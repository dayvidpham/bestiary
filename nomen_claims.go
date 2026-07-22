package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

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
//     "models.dev" (a curated alias is layered on the models.dev catalog we decomposed).
type nomenClaimJSON struct {
	Value     string            `json:"value"`
	Scheme    string            `json:"scheme,omitempty"`
	Status    string            `json:"status,omitempty"`
	ResolveTo nomenClaimRefJSON `json:"resolves_to"`
	SourceURL string            `json:"source_url"`
	SourceID  string            `json:"source_id,omitempty"`
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
		if strings.TrimSpace(c.SourceURL) == "" {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid claim #%d (value=%q): empty source_url\n"+
					"  What: a naming claim has no claimant attribution\n"+
					"  Where: parse/data/nomen_claims.json claims[%d].source_url\n"+
					"  Why: SourceURL records WHO asserts the naming; an unattributable claim defeats the purpose\n"+
					"  How to fix: set source_url to the asserting lab/vendor page",
				i, value, i,
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

		source := DataSourceModelsDev
		if s := strings.TrimSpace(c.SourceID); s != "" {
			source = DataSourceID(s)
		}

		tbl.claims = append(tbl.claims, Nomen{
			Value:      value,
			Scheme:     scheme,
			Status:     status,
			ResolvesTo: ref,
			SourceURL:  strings.TrimSpace(c.SourceURL),
			Source:     source,
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

// ValidateNomina is the LOUD codegen guard: it fails if two nomina share the same
// primary-key triple (value, scheme, entity_key) but disagree on their other fields
// (status, source_url, source). A same-triple CONFLICT is a curation error that must
// never be resolved by last-write-wins — the store's PK would silently collapse the
// rows, so the bake refuses. An exact-duplicate triple with identical fields is a
// harmless no-op (idempotent) and is allowed. Codegen calls this over MintNomina's
// output; the runtime never does (it degrades).
func ValidateNomina(nomina []Nomen) error {
	type pk struct {
		value  string
		scheme NomenScheme
		entity string
	}
	seen := make(map[pk]Nomen, len(nomina))
	for _, n := range nomina {
		key := pk{value: n.Value, scheme: n.Scheme, entity: n.ResolvesTo.String()}
		prev, dup := seen[key]
		if !dup {
			seen[key] = n
			continue
		}
		if prev.Status != n.Status || prev.SourceURL != n.SourceURL || prev.Source != n.Source {
			return fmt.Errorf(
				"bestiary nomen: same-triple claim conflict\n"+
					"  What: two nomina share the PK triple (value=%q, scheme=%q, entity_key=%q) but disagree\n"+
					"  First:  status=%q source_url=%q source=%q\n"+
					"  Second: status=%q source_url=%q source=%q\n"+
					"  Where: MintNomina output (nomen_claims.json curation and/or the minted provider-ID/canonical set)\n"+
					"  Why: the store PK (value,scheme,entity_key) would silently collapse these rows (last-write-wins), losing a distinct assertion\n"+
					"  How to fix: reconcile the conflicting claim in parse/data/nomen_claims.json, or split it onto a distinct entity",
				key.value, key.scheme.String(), key.entity,
				prev.Status.String(), prev.SourceURL, prev.Source,
				n.Status.String(), n.SourceURL, n.Source,
			)
		}
	}
	return nil
}
