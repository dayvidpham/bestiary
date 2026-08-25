package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// This file loads the HARVESTED HuggingFace Hub naming seed
// (parse/data/huggingface_nomina.json) and turns it into NomenSchemeHuggingFace
// Nomina. It is the harvested-layer twin of nomen_claims.go's curated-claims
// loader — same go:embed + sync.Once + LOUD-at-codegen / graceful-at-runtime
// discipline (the lineage.go precedent) — with two deliberate differences that
// follow from §3.2/§6 of the naming design:
//
//   - NO archive-snapshot fence ON source_url. A curated claim (nomen_claims.json)
//     cites an archive.org snapshot because it is durable third-party EVIDENCE a
//     human recorded, and a live page rots. A harvested HF nomen is a LIVE
//     observation the offline cmd/bestiary-hf bot made of the Hub itself: its
//     SourceURL is the live repo URL (https://huggingface.co/<org>/<repo>),
//     Method=Harvested, Authority=Primary (the Hub is authoritative for the
//     huggingface scheme), Source=huggingface. The archive-pin policy
//     (nomen_claims.go) binds the CURATED layer only, never this one — subjecting a
//     harvested observation to the curated fence would be a category error.
//
//     A harvested nomen's live page can still rot, so the snapshot is recorded
//     BESIDE the observation rather than in place of it: the optional archived_url
//     field carries an archive.org snapshot OF source_url onto
//     NomenAttestation.ArchivedURL. It is OPTIONAL (absent means "none recorded",
//     which is the normal case — the Wayback lookup is best-effort) but, when
//     present, must satisfy the SAME shared shape check the curated fence uses
//     (IsArchiveSnapshotURL), so the two layers can never disagree about what an
//     archive URL looks like.
//   - CASE IS PRESERVED. An HF id is org/repo, 1:1, and case-significant
//     (meta-llama/Llama-3.3-70B-Instruct is NOT .../llama-3.3-70b-instruct). The
//     value is stored and rendered VERBATIM; lowercasing it is a defect the loader
//     structurally cannot introduce (it never lower-cases the value) and the
//     source_url guard cross-checks (the live URL tail must equal the value).
//
// The three curated-file roles the naming layer keeps separate still hold: this
// file is a claim INPUTS layer (harvested naming observations we could never
// derive mechanically — a repo path is the lab's own choice), nomen.go mints the
// OUTPUT records, and the parse/data decomposition files are CONFIG. The seed is
// FIELD-OWNED by the bot: the repo set is fetch-owned (refreshed each run); any
// hand override is curation-owned (preserved). See cmd/bestiary-hf.

// hfLiveURLPrefix is the live Hub URL prefix a harvested SourceURL must carry. It
// is deliberately the LIVE address (not an archive.org snapshot): a harvested
// nomen records what the bot observed on the Hub, and the live repo URL is the
// self-describing citation (its tail is the org/repo value itself).
const hfLiveURLPrefix = "https://huggingface.co/"

// hfNomenRefJSON is the resolves_to entity-ref tuple in huggingface_nomina.json.
// It mirrors nomenClaimRefJSON: family/variant/version/param_size plus identity-
// class modifiers, decomposed into the EntityRef key the registry produces.
type hfNomenRefJSON struct {
	Family    Family   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size,omitempty"`
	Modifier  []string `json:"modifier,omitempty"`
}

// hfNomenJSON is one harvested HuggingFace naming observation.
//
//   - Value is the Hub org/repo path, CASE-PRESERVED (e.g.
//     "meta-llama/Llama-3.3-70B-Instruct"). It is the nomen value verbatim — never
//     decomposed to form the value, never lowercased.
//   - ResolveTo is the entity the repo names (the JOIN result the bot computed).
//   - SourceURL is the LIVE Hub repo URL (https://huggingface.co/<value>) the bot
//     observed. It is a harvested attestation's SourceURL, NOT a curated archive
//     snapshot (the fence in nomen_claims.go does not apply here).
//   - ArchivedURL is an archive.org snapshot OF SourceURL, when the bot's Wayback
//     lookup found one. OMITTED (the omitempty spelling) when none is recorded —
//     the overwhelmingly common case on a first harvest, and never an error.
type hfNomenJSON struct {
	Value       string         `json:"value"`
	ResolveTo   hfNomenRefJSON `json:"resolves_to"`
	SourceURL   string         `json:"source_url"`
	ArchivedURL string         `json:"archived_url,omitempty"`
}

// hfNominaFileJSON is the top-level shape of parse/data/huggingface_nomina.json.
type hfNominaFileJSON struct {
	Comment       string        `json:"_comment"`
	SchemaVersion int           `json:"schema_version"`
	Nomina        []hfNomenJSON `json:"nomina"`
}

// hfNominaTable is the parsed, validated harvested seed as ready-to-use Nomina.
type hfNominaTable struct {
	nomina []Nomen
	// keys is the set of entity keys (EntityRef.String()) an HF nomen resolves to.
	// It is the registry attestation input: an entity in this set DUAL-attests
	// {models.dev, huggingface}.
	keys map[string]struct{}
}

func emptyHFNominaTable() *hfNominaTable {
	return &hfNominaTable{nomina: nil, keys: map[string]struct{}{}}
}

var (
	hfNominaOnce sync.Once
	hfNominaTbl  *hfNominaTable
	hfNominaErr  error
)

// loadHFNomina reads and validates parse/data/huggingface_nomina.json from the
// embedded FS exactly once. The cached error is non-nil when the file is missing,
// malformed, or fails validation; ValidateHFNomina surfaces it so codegen fails
// loudly on a bad seed.
func loadHFNomina() (*hfNominaTable, error) {
	hfNominaOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/huggingface_nomina.json")
		if err != nil {
			hfNominaErr = fmt.Errorf(
				"bestiary nomen: load huggingface_nomina.json: %w\n"+
					"  What: cannot read the embedded harvested HuggingFace seed\n"+
					"  Where: parse/data/huggingface_nomina.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/huggingface_nomina.json is present before building",
				err,
			)
			return
		}
		hfNominaTbl, hfNominaErr = parseHFNomina(raw)
	})
	return hfNominaTbl, hfNominaErr
}

// loadHFNominaSafe returns the cached table, or a non-nil EMPTY (degraded) table
// when loading failed. It never returns nil and never panics — runtime HF-nomen
// lookups degrade to "no harvested nomina" rather than aborting.
func loadHFNominaSafe() *hfNominaTable {
	t, err := loadHFNomina()
	if err != nil || t == nil {
		return emptyHFNominaTable()
	}
	return t
}

// parseHFNomina parses and validates the harvested HF seed JSON into Nomina. It is
// the testable seam behind loadHFNomina. It rejects an entry with an empty value, a
// value that is not an org/repo path (no "/"), an empty/ill-formed source_url, a
// source_url that is not the LIVE Hub URL for exactly this value (case-preserved
// cross-check), or a resolves_to with an unknown base family — each with an
// actionable error. Nomina are sorted deterministically (lessNomen) before return.
//
// CASE PRESERVATION is load-bearing and asserted here: the source_url must equal
// hfLiveURLPrefix+value byte-for-byte, so a lowercased value (or a lowercased URL)
// is rejected rather than silently minting a case-mangled Hub name.
func parseHFNomina(raw []byte) (*hfNominaTable, error) {
	var file hfNominaFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary nomen: parse huggingface_nomina.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/huggingface_nomina.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := &hfNominaTable{keys: map[string]struct{}{}}
	seen := map[string]struct{}{}
	for i, n := range file.Nomina {
		// Value is CASE-PRESERVED: trim surrounding whitespace only, never lowercase.
		value := strings.TrimSpace(n.Value)
		if value == "" {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid huggingface nomen #%d: empty value\n"+
					"  What: a harvested HF observation has no org/repo value\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].value\n"+
					"  How to fix: set the Hub org/repo path (case-preserved)",
				i, i,
			)
		}
		if !strings.Contains(value, "/") {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid huggingface nomen #%d (value=%q): not an org/repo path\n"+
					"  What: a HuggingFace id is org/repo (1:1); this value has no '/'\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].value\n"+
					"  How to fix: use the full Hub path, e.g. \"meta-llama/Llama-3.3-70B-Instruct\"",
				i, value, i,
			)
		}
		if _, dup := seen[value]; dup {
			return nil, fmt.Errorf(
				"bestiary nomen: duplicate huggingface nomen #%d (value=%q)\n"+
					"  What: the same org/repo appears twice in the harvested seed\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].value\n"+
					"  Why: the repo set is fetch-owned and de-duplicated at emit; a duplicate is a bot bug\n"+
					"  How to fix: remove the duplicate entry",
				i, value, i,
			)
		}
		seen[value] = struct{}{}

		sourceURL := strings.TrimSpace(n.SourceURL)
		wantURL := hfLiveURLPrefix + value
		if sourceURL != wantURL {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid huggingface nomen #%d (value=%q): source_url %q is not the live Hub URL for this value\n"+
					"  What: a harvested HF nomen's source_url must be exactly %q (the live repo URL, case-preserved)\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].source_url\n"+
					"  Why: the harvested layer cites the LIVE observation (not an archive snapshot — that fence binds the curated layer only);\n"+
					"       the URL tail is the org/repo value itself, so a mismatch means a lowercased/altered value or a wrong URL\n"+
					"  How to fix: set source_url to %q",
				i, value, sourceURL, wantURL, i, wantURL,
			)
		}

		// archived_url is OPTIONAL — a harvest with no snapshot is the normal case, and
		// treating its absence as an error would make the best-effort lookup load-bearing.
		// But a PRESENT value must be a real archive.org snapshot: it is recorded as durable
		// evidence, so a malformed one is a bot/curation defect that would quietly ship an
		// unusable citation. The check reuses the SAME IsArchiveSnapshotURL shape the curated
		// fence applies — one shared validator, so the accepted shape cannot drift between the
		// two layers.
		archivedURL := strings.TrimSpace(n.ArchivedURL)
		if archivedURL != "" && !IsArchiveSnapshotURL(archivedURL) {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid huggingface nomen #%d (value=%q): archived_url %q is not an archive.org snapshot\n"+
					"  What: a harvested nomen records a snapshot URL that is not in the archive.org snapshot shape\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].archived_url\n"+
					"  When: loading the harvested seed, before any nomen is minted\n"+
					"  Why: archived_url exists so a harvested citation survives its live page rotting; a value\n"+
					"       that is not a snapshot cannot serve that purpose, and shipping it would present an\n"+
					"       unusable citation as durable evidence. The field is OPTIONAL — omitting it is valid\n"+
					"  What it means for the caller: the whole seed is REJECTED; no harvested nomen is minted\n"+
					"  How to fix: omit archived_url, or set it to a %ssnapshot-timestamp/<original-url> URL\n"+
					"       (cmd/bestiary-hf writes this field; a hand-edit is the likely source of a bad value)",
				i, value, archivedURL, i, archiveSnapshotURLPrefix,
			)
		}

		// The harvested layer resolves to WHATEVER catalog entity the repo joins to —
		// not necessarily a curated BASE family (unlike nomen_claims.json). A community
		// finetune's repo legitimately names an entity whose family is real in the
		// catalog but not in curatedBaseFamilies (e.g. codellama, hermes, qwq). So the
		// load-time check is only that the family is NON-EMPTY (an entity key needs a
		// family); the meaningful guard — that the nomen resolves to a REAL entity — is
		// the codegen ValidateHFNomina entity FK check (which has the registry), the
		// datasource.go FK-guard precedent. Keep-never-drop: a nomen resolving to no
		// entity still mints (NomenLookup returns it) but attests nothing.
		if strings.TrimSpace(string(n.ResolveTo.Family)) == "" {
			return nil, fmt.Errorf(
				"bestiary nomen: invalid huggingface nomen #%d (value=%q): empty resolves_to.family\n"+
					"  What: a harvested nomen has no family to key its entity by\n"+
					"  Where: parse/data/huggingface_nomina.json nomina[%d].resolves_to.family\n"+
					"  How to fix: set the family the Hub repo's weights belong to",
				i, value, i,
			)
		}

		ref := EntityRef{
			Family:    n.ResolveTo.Family,
			Variant:   n.ResolveTo.Variant,
			Version:   n.ResolveTo.Version,
			ParamSize: n.ResolveTo.ParamSize,
			Modifier:  EntityModifiers(n.ResolveTo.Modifier, n.ResolveTo.Family),
		}

		// A harvested HF observation is ONE attestation (§3.2 defaults table): the
		// Hub is the Primary authority for the huggingface scheme, the SourceURL is
		// the live repo the bot observed, read through the huggingface ingest,
		// Method=Harvested. ArchivedURL carries the archive.org snapshot of that live
		// page when one was recorded, "" otherwise. IngestedAt is left "" (honest): the
		// committed ingest instant lives on the datasources.json ingest row, not per-nomen.
		tbl.nomina = append(tbl.nomina, Nomen{
			Value:      value,
			Scheme:     NomenSchemeHuggingFace,
			Status:     AcceptabilityAdmitted,
			ResolvesTo: ref,
			Attestations: []NomenAttestation{{
				SourceURL:   sourceURL,
				ArchivedURL: archivedURL,
				Source:      DataSourceHuggingFace,
				Authority:   AuthorityPrimary,
				Method:      IngestMethodHarvested,
			}},
		})
		tbl.keys[ref.String()] = struct{}{}
	}

	sort.Slice(tbl.nomina, func(i, j int) bool { return lessNomen(tbl.nomina[i], tbl.nomina[j]) })
	return tbl, nil
}

// hfNominaClaims returns a fresh copy of the harvested HF Nomina for folding into
// the mint set (the loadNomenClaimsSafe().claims precedent). It degrades to nil on
// a load failure — never panics.
func hfNominaClaims() []Nomen {
	t := loadHFNominaSafe()
	if len(t.nomina) == 0 {
		return nil
	}
	return append([]Nomen(nil), t.nomina...)
}

// ValidateHFNomina is the LOUD codegen guard over the harvested HF seed: it
// surfaces any load/parse error (empty value, non-org/repo value, source_url that
// is not the live Hub URL for the value, a PRESENT archived_url that is not an
// archive.org snapshot, empty family, duplicate), AND checks the
// entity FK — every harvested nomen must resolve to a REAL registry entity (the
// meaningful integrity guard for the harvested layer, the datasource.go FK-guard
// precedent). Codegen (cmd/bestiary-gen run()) calls it alongside the other
// Validate* guards and aborts the bake on a non-nil result, so a malformed,
// case-mangled, or orphan-resolving harvested seed never bakes into the static
// catalog. The runtime loader (loadHFNominaSafe) degrades gracefully instead.
func ValidateHFNomina() error {
	t, err := loadHFNomina()
	if err != nil {
		return err
	}
	return validateHFEntityFKs(t.nomina, func(key string) bool {
		_, ok := EntityByKey(key)
		return ok
	})
}

// validateHFEntityFKs is the testable seam behind ValidateHFNomina's entity FK
// check: every harvested nomen's ResolvesTo must resolve via the supplied resolver
// (the production resolver is the registry entity index). It takes the resolver
// explicitly so a constructed orphan — a harvested nomen keyed to a non-existent
// entity — can be falsified in a unit test without injecting fake entities into
// the global registry (the validateEntityKeyFKs precedent).
func validateHFEntityFKs(nomina []Nomen, resolves func(entityKey string) bool) error {
	for _, n := range nomina {
		key := n.ResolvesTo.String()
		if !resolves(key) {
			return fmt.Errorf(
				"bestiary nomen: harvested huggingface nomen %q resolves to no entity (key %q)\n"+
					"  What: a harvested HF naming names an entity that is not in the registry\n"+
					"  Where: parse/data/huggingface_nomina.json (the entity FK check)\n"+
					"  Why: a harvested nomen must name a REAL catalog entity; the bot only writes\n"+
					"       repos whose decomposition matched a catalog key, so an orphan means the\n"+
					"       seed was hand-edited or the catalog changed under it\n"+
					"  How to fix: re-run cmd/bestiary-hf to refresh the seed, add an hf_aliases.json\n"+
					"       override pinning the repo to a real entity, or remove the stale entry",
				n.Value, key,
			)
		}
	}
	return nil
}
