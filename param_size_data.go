package bestiary

import (
	"encoding/json"
	"fmt"
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
