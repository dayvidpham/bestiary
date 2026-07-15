package bestiary

import (
	"strings"
	"testing"
)

// TestParseParamSizeOverrides_Presence pins the PRESENCE-based semantics of the
// override parser: a normal pin round-trips its token, a SUPPRESS entry
// (param_size "") is PRESERVED in the map (found, value "") rather than dropped,
// and both the ID key and the token value are lowercased.
func TestParseParamSizeOverrides_Presence(t *testing.T) {
	raw := []byte(`{
      "schema_version": 1,
      "entries": [
        {"id": "Llama-4-Scout", "param_size": "17B-16E"},
        {"id": "qwen/qwen3-coder-next-fp8-1m", "param_size": ""},
        {"id": "", "param_size": "70b"}
      ]
    }`)

	m, err := parseParamSizeOverrides(raw)
	if err != nil {
		t.Fatalf("parseParamSizeOverrides returned unexpected error: %v", err)
	}

	// Normal pin: key + value both lowercased.
	if got, ok := m["llama-4-scout"]; !ok || got != "17b-16e" {
		t.Errorf("pin llama-4-scout = (%q, %v), want (\"17b-16e\", true)", got, ok)
	}

	// Suppress pin: present with empty value (NOT dropped).
	if got, ok := m["qwen/qwen3-coder-next-fp8-1m"]; !ok || got != "" {
		t.Errorf("suppress pin = (%q, %v), want (\"\", true) — a suppress entry must be PRESENT", got, ok)
	}

	// Empty ID entry is skipped (cannot be matched).
	if _, ok := m[""]; ok {
		t.Errorf("an entry with an empty id must not be inserted")
	}
}

// TestParseParamSizeOverrides_MalformedDegrades verifies the parser returns an
// actionable error on malformed JSON (the runtime loader swallows this and
// degrades to an empty map — never panicking, per the lineage.go precedent).
func TestParseParamSizeOverrides_MalformedDegrades(t *testing.T) {
	if _, err := parseParamSizeOverrides([]byte("{ this is not json")); err == nil {
		t.Fatal("parseParamSizeOverrides(malformed) = nil error, want an actionable parse error")
	} else if !strings.Contains(err.Error(), "param_size_overrides.json") {
		t.Errorf("error should name the data file, got: %q", err.Error())
	}
}

// TestParamSizePin_Seed exercises paramSizePin against the REAL embedded seed
// (parse/data/param_size_overrides.json): a llama-4 census pin, a suppress pin, an
// absent ID, and case-insensitive matching. It anchors the curated seed entries so
// a future re-pin can't silently drop these load-bearing rows.
func TestParamSizePin_Seed(t *testing.T) {
	cases := []struct {
		name      string
		id        string
		wantTok   string
		wantFound bool
	}{
		{"scout size-less → 17b-16e", "llama-4-scout", "17b-16e", true},
		{"maverick size-less → 17b-128e", "llama-4-maverick", "17b-128e", true},
		{"scout bare-17b → 17b-16e", "llama-4-scout-17b-instruct", "17b-16e", true},
		{"Bedrock dotted scout → 17b-16e", "us.meta.llama4-scout-17b-instruct-v1:0", "17b-16e", true},
		{"Bedrock dotted maverick → 17b-128e", "meta.llama4-maverick-17b-instruct-v1:0", "17b-128e", true},
		{"underscore solar → 10.7b", "upstage/solar-10_7b-instruct", "10.7b", true},
		{"suppress fp8-1m → \"\" found", "qwen/qwen3-coder-next-fp8-1m", "", true},
		{"case-insensitive scout", "LLAMA-4-SCOUT", "17b-16e", true},
		{"absent id → not found", "claude-opus-4-5", "", false},
		{"sized llama-4 form is NOT pinned (extracts mechanically)", "llama-4-scout-17b-16e-instruct", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTok, gotFound := paramSizePin(tc.id)
			if gotFound != tc.wantFound || gotTok != tc.wantTok {
				t.Errorf("paramSizePin(%q) = (%q, %v), want (%q, %v)",
					tc.id, gotTok, gotFound, tc.wantTok, tc.wantFound)
			}
		})
	}
}

// TestParamSizePin_SuppressNeverFallsThrough is the load-bearing guard for the
// suppress semantics: for the fp8-1m ID the MECHANICAL extractor honestly returns
// a token ("1m", true), yet the curated pin is present-with-empty. A correct
// enrichment path branches on the pin's presence, so the mechanical "1m" is
// overridden. This test pins both facts so the suppress pin can never go dead.
func TestParamSizePin_SuppressNeverFallsThrough(t *testing.T) {
	const id = "qwen/qwen3-coder-next-fp8-1m"

	// The pin is present and suppresses (empty value).
	tok, found := paramSizePin(id)
	if !found || tok != "" {
		t.Fatalf("paramSizePin(%q) = (%q, %v), want (\"\", true)", id, tok, found)
	}

	// Non-vacuity: the mechanical extractor really would size this as "1m", so the
	// suppress pin is doing real work (not covering an already-empty case).
	mechTok, mechOK := ExtractParamSizeToken(id)
	if !mechOK || mechTok != "1m" {
		t.Fatalf("ExtractParamSizeToken(%q) = (%q, %v), want (\"1m\", true) — the suppress pin must override a REAL mechanical size",
			id, mechTok, mechOK)
	}
}
