package bestiary

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// The curated-claims ARCHIVE POLICY, enforced at load.
//
// Every source_url in nomen_claims.json must be an archive.org snapshot of the
// claimant page, captured when the claim was created. See NomenAttestation.SourceURL
// for the rationale; the short form is that a claim is evidence of what a lab published, and
// live model cards and docs pages are edited and deleted without notice.
//
// The fence binds THIS curated layer only. It is not a rule about the type: a
// harvested attestation's source_url is deliberately a live observation, and its
// snapshot (when the bot finds one) is recorded on ArchivedURL — see
// huggingface_nomina.go. Nothing below is relaxed, moved or duplicated by that.
//
// The failure disciplines here are deliberately split, matching the lineage.go
// precedent this file already follows: a MISSING or CORRUPT file degrades gracefully
// to an empty table (loadNomenClaimsSafe), because a build without curated claims is
// still a working library — but a claim that is PRESENT and violates the policy is
// LOUD, because silently minting a nomen whose evidence can rot is exactly the
// outcome the policy exists to prevent. Same split the empty-source_url rejection
// above already implements.
const archiveSnapshotURLPrefix = "https://web.archive.org/web/"

// archiveSnapshotURL is the snapshot shape: the prefix, a 14-digit capture
// timestamp, then the original claimant URL retained verbatim — which is why the
// CURATED layer needs no second archive_url field: its source_url already IS the
// snapshot, and the live address is recoverable from the snapshot's own tail. That
// is a statement about the curated layer alone. The HARVESTED layer cites a live
// observation (huggingface_nomina.go), so its snapshot cannot live in source_url
// and rides alongside on NomenAttestation.ArchivedURL instead.
var archiveSnapshotURL = regexp.MustCompile(`^https://web\.archive\.org/web/\d{14}/https?://.+$`)

// IsArchiveSnapshotURL reports whether url has the archive.org snapshot shape the
// curated archive policy requires: the web.archive.org prefix, a 14-digit capture
// timestamp, then the original URL retained verbatim.
//
// It is the ONE shared format check — the curated nomen-claim fence, the curated
// suppression-seed fence and the harvested layer's ArchivedURL all match against
// this single regexp rather than each copying the pattern, so the accepted shape
// can never drift between them. It is exported because the offline cmd/bestiary-hf
// bot (a separate package) validates the Wayback Availability API's answer with it
// before recording an ArchivedURL.
//
// It is a SHAPE check only: it does not fetch the URL and makes no claim that the
// snapshot exists or resolves.
func IsArchiveSnapshotURL(url string) bool { return archiveSnapshotURL.MatchString(url) }

// This file loads the curated third-party naming claims (parse/data/nomen_claims.json)
// and turns them into Alias/HuggingFace/PURL Nomina. It follows the lineage.go
// precedent exactly: go:embed + sync.Once, a LOUD validated parse (ValidateNomenClaims,
// used by codegen), and a graceful-degrade runtime accessor (loadNomenClaimsSafe) that
// returns a non-nil EMPTY table on any missing/corrupt file — so a bad claims file
// degrades to "no alias nomina", never a panic.
//
// The three curated-file roles the naming layer keeps separate (documented for the
// consolidation): the parse/data decomposition files (family_overrides, modifiers, …)
// are decomposition CONFIG; nomen_claims.json is the claim INPUTS layer (third-party
// naming assertions we could never derive mechanically); the minted Nomina are the
// queryable OUTPUT records. This file is the CONFIG→INPUT boundary; nomen.go mints the
// OUTPUT.

// nomenClaimRefJSON is the resolves_to entity-ref tuple in nomen_claims.json. It
// mirrors lineageRefJSON: family/variant/version/param_size plus identity-class
// modifiers, decomposed into the EntityRef key the registry produces.
type nomenClaimRefJSON struct {
	Family    Family   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size,omitempty"`
	Modifier  []string `json:"modifier,omitempty"`
}

// nomenClaimJSON is one curated naming claim.
//
//   - Value is the asserted naming spelling (e.g. "grok-beta").
//   - Scheme defaults to "alias" when omitted; any NomenScheme wire token is accepted.
//   - Status defaults to "admitted" when omitted (a third-party claim is admitted,
//     never preferred — the canonical key alone is preferred).
//   - SourceURL is WHO asserts the claim (the lab/vendor page). Required: a claim with
//     no claimant is a claim we cannot attribute, which defeats the purpose.
//   - SourceID is WHICH ingest we read it from (a DataSourceID). Defaults to
//     DataSourceCurated: bestiary read the claim from its OWN curated claim file, not
//     from models.dev or Ollama. This is the honest ingest provenance; it is DISTINCT
//     from SourceURL (who asserts the naming).
type nomenClaimJSON struct {
	Value     string            `json:"value"`
	Scheme    string            `json:"scheme,omitempty"`
	Status    string            `json:"status,omitempty"`
	ResolveTo nomenClaimRefJSON `json:"resolves_to"`
	SourceURL string            `json:"source_url"`
	SourceID  string            `json:"source_id,omitempty"`
	// Authority is whose VOICE the claimed evidence document is (primary/secondary).
	// It defaults to "primary" when omitted: a curated alias is typically the lab or
	// vendor declaring a naming for its own model (the grok-beta = xAI case). Any
	// AttestationAuthority wire token is accepted.
	Authority string `json:"authority,omitempty"`
}

// nomenClaimsFileJSON is the top-level shape of parse/data/nomen_claims.json.
type nomenClaimsFileJSON struct {
	Comment       string           `json:"_comment"`
	SchemaVersion int              `json:"schema_version"`
	Claims        []nomenClaimJSON `json:"claims"`
}

// nomenClaimsTable is the parsed, validated curated claim set as ready-to-use Nomina.
type nomenClaimsTable struct {
	claims []Nomen
}

func emptyNomenClaimsTable() *nomenClaimsTable {
	return &nomenClaimsTable{claims: nil}
}

var (
	nomenClaimsOnce sync.Once
	nomenClaimsTbl  *nomenClaimsTable
	nomenClaimsErr  error
)

// loadNomenClaims reads and validates parse/data/nomen_claims.json from the embedded
// FS exactly once. The cached error is non-nil when the file is missing, malformed,
// or fails validation; ValidateNomenClaims surfaces it so codegen fails loudly on bad
// curation.
func loadNomenClaims() (*nomenClaimsTable, error) {
	nomenClaimsOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/nomen_claims.json")
		if err != nil {
			nomenClaimsErr = fmt.Errorf(
				"bestiary nomen: load nomen_claims.json: %w\n"+
					"  What: cannot read the embedded nomen-claims table\n"+
					"  Where: parse/data/nomen_claims.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/nomen_claims.json is present before building",
				err,
			)
			return
		}
		nomenClaimsTbl, nomenClaimsErr = parseNomenClaims(raw)
	})
	return nomenClaimsTbl, nomenClaimsErr
}

// loadNomenClaimsSafe returns the cached table, or a non-nil EMPTY (degraded) table
// when loading failed. It never returns nil and never panics — runtime alias-nomen
// lookups degrade to "no claims" rather than aborting.
func loadNomenClaimsSafe() *nomenClaimsTable {
	t, err := loadNomenClaims()
	if err != nil || t == nil {
		return emptyNomenClaimsTable()
	}
	return t
}

// parseNomenClaims parses and validates the curated claims JSON into Nomina. It is
// the testable seam behind loadNomenClaims: it rejects a claim with an empty value,
// an empty source_url (unattributable), an unknown scheme/status token, or a
// resolves_to with an unknown base family, with an actionable error. Claims are sorted
// deterministically (lessNomen) before return so the table order is stable.
func parseNomenClaims(raw []byte) (*nomenClaimsTable, error) {
	var file nomenClaimsFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary nomen: parse nomen_claims.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/nomen_claims.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := &nomenClaimsTable{}
	for i, c := range file.Claims {
		value := strings.TrimSpace(c.Value)
		if value == "" {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid claim #%d: empty value\n"+
					"  What: a naming claim has no value spelling\n"+
					"  Where: parse/data/nomen_claims.json claims[%d].value\n"+
					"  How to fix: set a non-empty value (the asserted naming spelling)",
				i, i,
			)
		}
		sourceURL := strings.TrimSpace(c.SourceURL)
		if sourceURL == "" {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid claim #%d (value=%q): empty source_url\n"+
					"  What: a naming claim has no claimant attribution\n"+
					"  Where: parse/data/nomen_claims.json claims[%d].source_url\n"+
					"  Why: SourceURL records WHO asserts the naming; an unattributable claim defeats the purpose\n"+
					"  How to fix: set source_url to the asserting lab/vendor page",
				i, value, i,
			)
		}
		if !archiveSnapshotURL.MatchString(sourceURL) {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid claim #%d (value=%q): source_url %q is not an archive.org snapshot\n"+
					"  What: a curated naming claim cites a live page instead of an archived one\n"+
					"  Where: parse/data/nomen_claims.json claims[%d].source_url\n"+
					"  When: loading the curated claim table (parseNomenClaims), before any nomen is minted\n"+
					"  Why: a claim is evidence of what a lab published, and model cards and docs pages are\n"+
					"       edited and deleted without notice — a live URL silently stops attesting the claim\n"+
					"       it was cited for. The snapshot embeds the original URL in its tail, so the live\n"+
					"       address stays recoverable and this CURATED layer needs no second archive_url\n"+
					"       field (the harvested layer, which cites a live observation, carries its snapshot\n"+
					"       alongside on ArchivedURL instead)\n"+
					"  What it means for the caller: the claim is REJECTED; no nomen is minted from this file\n"+
					"  How to fix: capture the claimant page at web.archive.org, verify the snapshot loads,\n"+
					"       then use that URL — the form is %ssnapshot-timestamp/<original-url>",
				i, value, sourceURL, i, archiveSnapshotURLPrefix,
			)
		}

		scheme := NomenSchemeAlias
		if s := strings.TrimSpace(c.Scheme); s != "" {
			if err := scheme.UnmarshalText([]byte(s)); err != nil {
				return nil, fmt.Errorf(
					"bestiary nomen: invalid claim #%d (value=%q): %w\n"+
						"  Where: parse/data/nomen_claims.json claims[%d].scheme\n"+
						"  How to fix: use a known scheme token (alias, huggingface, purl, canonical, provider-id)",
					i, value, err, i,
				)
			}
		}

		status := AcceptabilityAdmitted
		if s := strings.TrimSpace(c.Status); s != "" {
			parsed, err := parseRating(strings.ToLower(s))
			if err != nil {
				return nil, fmt.Errorf(
					"bestiary nomen: invalid claim #%d (value=%q): %w\n"+
						"  Where: parse/data/nomen_claims.json claims[%d].status",
					i, value, err, i,
				)
			}
			status = parsed
		}

		if !c.ResolveTo.Family.IsKnown() {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid claim #%d (value=%q): unknown base family %q in resolves_to\n"+
					"  What: a claim resolves to a family that is not a known base family\n"+
					"  Where: parse/data/nomen_claims.json claims[%d].resolves_to.family\n"+
					"  Why: every claim must resolve to a known base family (Family.IsKnown)\n"+
					"  How to fix: correct the family, or register it in family.go (curatedBaseFamilies)",
				i, value, c.ResolveTo.Family, i,
			)
		}

		ref := EntityRef{
			Family:    c.ResolveTo.Family,
			Variant:   c.ResolveTo.Variant,
			Version:   c.ResolveTo.Version,
			ParamSize: c.ResolveTo.ParamSize,
			Modifier:  EntityModifiers(c.ResolveTo.Modifier, c.ResolveTo.Family),
		}

		source := DataSourceCurated
		if s := strings.TrimSpace(c.SourceID); s != "" {
			source = DataSourceID(s)
		}

		// Authority defaults to Primary (a curated alias is typically the lab
		// declaring a naming for its own model); any wire token overrides it.
		authority := AuthorityPrimary
		if s := strings.TrimSpace(c.Authority); s != "" {
			if err := authority.UnmarshalText([]byte(s)); err != nil {
				return nil, fmt.Errorf(
					"bestiary nomen: invalid claim #%d (value=%q): %w\n"+
						"  Where: parse/data/nomen_claims.json claims[%d].authority\n"+
						"  How to fix: use a known authority token (primary, secondary)",
					i, value, err, i,
				)
			}
		}

		// A curated claim is ONE attestation (§3.2): its evidence document is the
		// claimant SourceURL, read through the curated ingest, Method=Curated.
		tbl.claims = append(tbl.claims, Nomen{
			Value:      value,
			Scheme:     scheme,
			Status:     status,
			ResolvesTo: ref,
			Attestations: []NomenAttestation{{
				SourceURL: strings.TrimSpace(c.SourceURL),
				Source:    source,
				Authority: authority,
				Method:    IngestMethodCurated,
			}},
		})
	}

	sort.Slice(tbl.claims, func(i, j int) bool { return lessNomen(tbl.claims[i], tbl.claims[j]) })
	return tbl, nil
}

// -----------------------------------------------------------------------------
// Runtime lookup index + codegen conflict validation
// -----------------------------------------------------------------------------

var (
	nomenIndexOnce sync.Once
	nomenIndex     map[string][]Nomen
)

// nomenLookupIndex builds (once) the value→[]Nomen index over the full static-registry
// nomen set. It is the memoized backing store for NomenLookup. Each bucket is sorted
// (Nomina() is already sorted, and grouping preserves that order), so a homonymous
// spelling's matches come back in a stable order.
func nomenLookupIndex() map[string][]Nomen {
	nomenIndexOnce.Do(func() {
		all := Nomina()
		idx := make(map[string][]Nomen, len(all))
		for _, n := range all {
			idx[n.Value] = append(idx[n.Value], n)
		}
		nomenIndex = idx
	})
	return nomenIndex
}

// ValidateNomina is the LOUD codegen guard, with the v0.2.8 INVERTED semantics: a
// same-triple (value, scheme, entity_key) disagreement is a conflict ONLY when the
// records disagree on Status — bestiary's single editorial judgment per name. Two
// records sharing the triple but carrying DIFFERENT attesters (SourceURL / Source /
// Authority / Method) are LEGAL: they union into one Nomen's multi-attestation set
// (the v0.2.8 lift; formerly this was a conflict). This is exactly coalesceNomina's
// discipline, so the guard delegates the union-and-Status-check to it, then verifies
// the structural invariants each coalesced Nomen must satisfy:
//   - at least ONE attestation (a name with no evidence is a defect);
//   - no BYTE-IDENTICAL duplicate attestation (coalesce dedups, so a survivor is a
//     bug in the union path).
//
// Codegen calls this over MintNomina's output, aborting the bake on conflict; the
// runtime never does (it degrades via coalesceNominaOrRaw).
func ValidateNomina(nomina []Nomen) error {
	coalesced, err := coalesceNomina(nomina)
	if err != nil {
		return err
	}
	for _, n := range coalesced {
		if len(n.Attestations) == 0 {
			return fmt.Errorf(
				"bestiary nomen: nomen carries no attestation\n"+
					"  What: the nomen (value=%q, scheme=%q, entity_key=%q) has an empty Attestations set\n"+
					"  Where: ValidateNomina over the minted nomen set\n"+
					"  Why: every naming must carry >=1 piece of evidence (its provenance)\n"+
					"  How to fix: ensure the mint site or curated claim attaches at least one NomenAttestation",
				n.Value, n.Scheme.String(), n.ResolvesTo.String(),
			)
		}
		for i := 1; i < len(n.Attestations); i++ {
			if n.Attestations[i] == n.Attestations[i-1] {
				return fmt.Errorf(
					"bestiary nomen: duplicate attestation survived coalesce\n"+
						"  What: the nomen (value=%q, scheme=%q, entity_key=%q) carries a byte-identical duplicate attestation\n"+
						"  Where: ValidateNomina over the coalesced nomen set\n"+
						"  Why: coalesceNomina dedups byte-identical attestations, so a survivor indicates a union-path defect\n"+
						"  How to fix: investigate coalesceNomina/sortAndDedupAttestations",
					n.Value, n.Scheme.String(), n.ResolvesTo.String(),
				)
			}
		}
	}
	return nil
}
