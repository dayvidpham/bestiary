package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// --------------------------------------------------------------------------
// Curated parameter-size override table (parse/data/param_size_overrides.json)
// — go:embed (via parseDataFS in parse.go) + sync.Once, mirroring the
// loadModelsdevAliases / loadLineageTable graceful-degrade precedent.
// --------------------------------------------------------------------------
//
// The override table is the TOP precedence tier for a model's parameter size:
// an exact-ID pin overrides the mechanical ExtractParamSizeToken decomposition
// and the fetch-owned ParamSizeFor fallback (pin > mechanical > ParamSizeFor).
// It exists for two families of ID that the mechanical extractor cannot size
// correctly:
//
//   - Under-specified identity forms whose size lives only in a sibling spelling.
//     Every llama-4 scout/maverick ID that lacks the expert-count suffix (a bare
//     "llama-4-scout", a "…-17b-instruct" without "-16e", or a dotted Bedrock
//     form) is pinned to its full-shape token ("17b-16e"/"17b-128e") so every
//     variant of the artifact keys to ONE entity.
//   - Size-shaped tokens that are not sizes. A SUPPRESS-pin (param_size "") means
//     NO size is extracted for that ID: no #size segment, no shape ints. This is
//     for IDs whose only size-looking token is really something else, e.g.
//     "qwen/qwen3-coder-next-fp8-1m" where "1m" is a 1M-context tier marker.
//
// The pin is PRESENCE-based: paramSizePin returns (token, found). A found "" is a
// deliberate suppression and must NOT fall through to the mechanical extractor —
// callers branch on found, never on the token being non-empty.

// paramSizeOverride is one curated exact-ID -> canonical size-token pin. param_size
// is the canonical token verbatim (e.g. "17b-16e", "10.7b"); an EMPTY param_size is
// a SUPPRESS-pin (no size for this ID). _comment is curator provenance and is not
// consumed by the loader.
type paramSizeOverride struct {
	ID        string `json:"id"`
	ParamSize string `json:"param_size"`
	Comment   string `json:"_comment,omitempty"`
}

// paramSizeOverrideFile is the on-disk shape of parse/data/param_size_overrides.json.
type paramSizeOverrideFile struct {
	Comment       string              `json:"_comment,omitempty"`
	SchemaVersion int                 `json:"schema_version"`
	Entries       []paramSizeOverride `json:"entries"`
}

var (
	paramSizeOverrideOnce sync.Once
	paramSizeOverrideMap  map[string]string
)

// loadParamSizeOverrides returns the curated exact-ID -> size-token pin map, loaded
// from the embedded file exactly once (sync.Once). It is a graceful-degrade loader:
// a missing or malformed file yields an EMPTY (non-nil) map, so enrichment simply
// proceeds with the mechanical extractor — it never panics and never returns nil
// (the loadModelsdevAliases / loadLineageTableSafe precedent). The map value may be
// "" for a suppress-pin; presence in the map is what matters, so callers use the
// paramSizePin (token, found) accessor rather than reading the map directly.
func loadParamSizeOverrides() map[string]string {
	paramSizeOverrideOnce.Do(func() {
		paramSizeOverrideMap = map[string]string{}
		raw, err := parseDataFS.ReadFile("parse/data/param_size_overrides.json")
		if err != nil {
			return
		}
		if m, err := parseParamSizeOverrides(raw); err == nil {
			paramSizeOverrideMap = m
		}
	})
	return paramSizeOverrideMap
}

// parseParamSizeOverrides is the testable seam behind loadParamSizeOverrides: it
// unmarshals the override file and lowercases every ID key AND size-token value
// (IDs and canonical size tokens are both matched/emitted lowercase). It returns an
// actionable error on malformed JSON so a codegen or test caller can surface the
// problem; the runtime loader above swallows the error and degrades to empty.
//
// A "" value is preserved verbatim as a suppress-pin — it is NOT dropped, because
// presence, not the value, is what suppresses the mechanical size.
func parseParamSizeOverrides(raw []byte) (map[string]string, error) {
	var file paramSizeOverrideFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary: parse param_size_overrides.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/param_size_overrides.json\n"+
				"  How to fix: validate the JSON syntax; expected"+
				" {\"schema_version\": N, \"entries\": [{\"id\": ..., \"param_size\": ...}]}",
			err,
		)
	}
	out := make(map[string]string, len(file.Entries))
	for _, e := range file.Entries {
		id := strings.ToLower(strings.TrimSpace(e.ID))
		if id == "" {
			continue // an entry with no ID cannot be matched; skip rather than fail.
		}
		out[id] = strings.ToLower(strings.TrimSpace(e.ParamSize))
	}
	return out, nil
}

// paramSizePin returns the curated size-token pin for id and whether one exists.
// It is PRESENCE-based: a returned found==true with token=="" is a deliberate
// SUPPRESS-pin (no size for this ID) and callers must honor it — they branch on
// found, never on the token being non-empty, so a suppress-pin can never fall
// through to the mechanical extractor. Lookup is case-insensitive on the full ID.
func paramSizePin(id string) (token string, found bool) {
	token, found = loadParamSizeOverrides()[strings.ToLower(strings.TrimSpace(id))]
	return token, found
}

// EnrichedParamSize resolves the canonical parameter-size token for a model ID via
// the PRESENCE-based precedence pin > mechanical > ParamSizeFor:
//
//   - a curated pin (param_size_overrides.json) is authoritative when PRESENT, even
//     when its token is "" — a suppress-pin deliberately yields NO size and must
//     never fall through to the mechanical extractor (presence, not value, decides);
//   - else the mechanical ExtractParamSizeToken decomposition of the ID;
//   - else the fetch-owned ParamSizeFor fallback (curated quant_vram.json), which
//     ranks LAST so an Ollama-bot refresh of a fetch-owned param_size can never flip
//     a stable ID-derived entity key.
//
// The returned err is a DISAGREEMENT signal — non-nil only when the ID is UNPINNED
// and BOTH the mechanical token and the ParamSizeFor fallback are present yet differ.
// That is a curation gap a human must resolve (add a pin). The returned token is
// still valid (the mechanical token wins per precedence), so runtime callers ignore
// the error and degrade gracefully; codegen treats it as a LOUD, actionable failure.
func EnrichedParamSize(id string) (token string, err error) {
	pinToken, pinned := paramSizePin(id)
	mech, mechOK := ExtractParamSizeToken(id)
	fallback := ParamSizeFor(ModelID(id))
	return resolveParamSizePrecedence(id, pinToken, pinned, mech, mechOK, fallback)
}

// resolveParamSizePrecedence applies the presence-based precedence pin > mechanical >
// ParamSizeFor to already-resolved tier values. It is separated from the table
// lookups so the precedence itself is unit-testable with injected tiers — in
// particular the token-less-fallback and disagreement paths that no current catalog
// ID reaches (all four quant_vram.json param_sizes agree with the mechanical token
// today, and every disagreeing ID is pinned). Callers pass the three tiers pre-fetched
// from paramSizePin / ExtractParamSizeToken / ParamSizeFor.
func resolveParamSizePrecedence(id, pinToken string, pinned bool, mech string, mechOK bool, fallback string) (string, error) {
	if pinned {
		return pinToken, nil // a PRESENT pin wins outright; "" suppresses all tiers.
	}
	if mechOK && fallback != "" && mech != fallback {
		return mech, fmt.Errorf(
			"bestiary: param-size disagreement for %q: mechanical token %q != curated ParamSizeFor %q\n"+
				"  What: the ID-derived size token and the fetch-owned quant_vram.json param_size differ\n"+
				"  Why: a curated fallback and the mechanical decomposition point at different sizes\n"+
				"  Where: bestiary.EnrichedParamSize (codegen bake / runtime enrichment joint)\n"+
				"  How to fix: add a curated pin in parse/data/param_size_overrides.json for %q resolving the size, "+
				"or correct the quant_vram.json param_size so it matches the ID",
			id, mech, fallback, id,
		)
	}
	if mechOK {
		return mech, nil
	}
	return fallback, nil
}

// enrichModelInfo is the shared per-row enrichment applied at BOTH runtime joints
// (wire decode toModelInfo and store read scanModelInfo). It derives ParamSize from
// the model ID via EnrichedParamSize and decomposes it into the flat shape ints
// (TotalParams/ActiveParams/PerExpertParams/ExpertCount) via ParseParamShape, and it
// derives the release Stage/StageRaw from the same ID via DetectStageFromID.
//
// It is a pure function of (ID, embedded curated data): NOTHING is persisted, so the
// SQLite store needs no param_size or stage column and stays schema v6 — a cached row
// is re-enriched from its ID on read. A disagreement or an unparseable size degrades
// gracefully (the derived token is still used; ParseParamShape returns the all-NULL
// shape so the shape ints read ParamShapeNull, not a masquerading 0), so a runtime
// joint never fails on data; the codegen bake path surfaces the same disagreement as
// a LOUD error instead. Stage is derived from the ID (not from the decomposed
// Modifier list) so it is symmetric across all three joints — the wire-decode joint
// has no decomposition yet, so a live-sync row and its baked static row still agree.
func enrichModelInfo(m *ModelInfo) {
	size, _ := EnrichedParamSize(string(m.ID))
	m.ParamSize = size
	shape, _ := ParseParamShape(size)
	m.TotalParams = shape.TotalParams
	m.ActiveParams = shape.ActiveParams
	m.PerExpertParams = shape.PerExpertParams
	m.ExpertCount = shape.ExpertCount
	m.Stage, m.StageRaw = DetectStageFromID(m.ID)
}

// ValidateParamSizePins checks that every curated pin token in
// param_size_overrides.json is CANONICAL: a non-empty token must round-trip through
// ParseParamSize to itself (a suppress-pin "" is allowed and skipped). It returns an
// actionable error naming every offending id -> token pair so a typo (e.g.
// "17b-16ee") is caught at CODEGEN, before a non-canonical token can ever flow into
// #size key material. The runtime loader stays graceful (a bad file degrades to an
// empty map); this is the codegen-time discipline that fences the pin file.
func ValidateParamSizePins() error {
	return validateParamSizePinsIn(loadParamSizeOverrides())
}

// validateParamSizePinsIn is the testable seam behind ValidateParamSizePins: it runs
// the canonical-token check over any pin map, so the rejection path is falsifiable
// with injected bad pins — the embedded seed is expected to always pass, which would
// otherwise leave the rejection arm unreachable from tests.
func validateParamSizePinsIn(pins map[string]string) error {
	var bad []string
	for id, tok := range pins {
		if tok == "" {
			continue // suppress-pin: absence of a size is the intended, canonical state.
		}
		canon, perr := ParseParamSize(tok)
		if perr != nil || canon != tok {
			bad = append(bad, fmt.Sprintf("%q -> %q", id, tok))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf(
		"bestiary: non-canonical param-size pin(s) in param_size_overrides.json: %s\n"+
			"  What: a curated pin token is not a canonical size (ParseParamSize rejected it or normalized it differently)\n"+
			"  Why: a typo'd or non-normalized token would flow verbatim into #size entity-key material\n"+
			"  Where: parse/data/param_size_overrides.json\n"+
			"  How to fix: correct each token to its canonical form (e.g. \"17b-16e\", \"10.7b\"); use \"\" for a suppress-pin",
		strings.Join(bad, ", "),
	)
}
