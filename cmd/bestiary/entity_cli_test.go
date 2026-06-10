package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestParseEntityTuple is a table-driven check of the canonical entity-tuple
// parser: the family[/variant][@version][#paramsize]{identity-mods}[attributes]
// grammar plus the lenient 3-segment family/variant/version form and the
// discarded trailing [attributes] segment.
func TestParseEntityTuple(t *testing.T) {
	cases := []struct {
		name          string
		input         string
		wantFam       bestiary.Family
		wantVariant   string
		wantVersion   string
		wantParamSize string
		wantMods      []string
		wantErr       bool
	}{
		{name: "family only", input: "llama", wantFam: "llama"},
		{name: "family + variant", input: "claude/opus", wantFam: "claude", wantVariant: "opus"},
		{name: "family + @version", input: "llama@3.1", wantFam: "llama", wantVersion: "3.1"},
		{name: "family + variant + @version", input: "claude/opus@4.5", wantFam: "claude", wantVariant: "opus", wantVersion: "4.5"},
		{name: "lenient 3-segment version", input: "claude/opus/4.5", wantFam: "claude", wantVariant: "opus", wantVersion: "4.5"},
		{name: "single identity-mod", input: "llama@3.1{instruct}", wantFam: "llama", wantVersion: "3.1", wantMods: []string{"instruct"}},
		{name: "multiple identity-mods", input: "kimi@k2{thinking,turbo}", wantFam: "kimi", wantVersion: "k2", wantMods: []string{"thinking", "turbo"}},
		{name: "trailing attributes discarded", input: "claude/opus@4.5[thinking]", wantFam: "claude", wantVariant: "opus", wantVersion: "4.5"},
		{name: "identity-mods kept, attributes discarded", input: "doubao@1.6{vision}[turbo]", wantFam: "doubao", wantVersion: "1.6", wantMods: []string{"vision"}},
		{name: "explicit @version wins over 3rd segment", input: "claude/opus/x@4.5", wantFam: "claude", wantVariant: "opus", wantVersion: "4.5"},
		{name: "empty family errors", input: "", wantErr: true},
		{name: "missing family before @ errors", input: "@4.5", wantErr: true},
		// #paramsize grammar (new)
		{name: "#paramsize only", input: "llama#70b", wantFam: "llama", wantParamSize: "70b"},
		{name: "@version + #paramsize", input: "llama@3.3#70b", wantFam: "llama", wantVersion: "3.3", wantParamSize: "70b"},
		{name: "@version + #paramsize + {mods}", input: "llama@3.3#70b{instruct}", wantFam: "llama", wantVersion: "3.3", wantParamSize: "70b", wantMods: []string{"instruct"}},
		{name: "family/variant@version#paramsize{mods}", input: "qwen/coder@2.5#7b{instruct}", wantFam: "qwen", wantVariant: "coder", wantVersion: "2.5", wantParamSize: "7b", wantMods: []string{"instruct"}},
		{name: "#paramsize with [attrs] discarded", input: "llama@3.3#70b{instruct}[thinking]", wantFam: "llama", wantVersion: "3.3", wantParamSize: "70b", wantMods: []string{"instruct"}},
		{name: "no #paramsize produces empty paramSize", input: "llama@3.3{instruct}", wantFam: "llama", wantVersion: "3.3", wantMods: []string{"instruct"}},
		// Adversarial # inputs: the parser uses LastIndex so a double-# takes only the
		// rightmost segment as size; the prefix up to that # (including any embedded #)
		// lands in the family segment as verbatim text (no second parse pass).
		// This is pinned passthrough behavior — such inputs will produce a lookup miss.
		{name: "double # uses last segment as paramsize (prefix becomes family)", input: "llama#70b#8b", wantFam: "llama#70b", wantParamSize: "8b"},
		// Trailing # produces empty paramsize (unrecognized shape passthrough).
		{name: "trailing # produces empty paramsize", input: "llama#", wantFam: "llama", wantParamSize: ""},
		// Leading # means empty family segment (errors).
		{name: "leading # errors (empty family)", input: "#70b", wantErr: true},
		// Uppercase size token: canonicalized to lowercase at parse boundary.
		{name: "uppercase size token canonicalized", input: "llama@3.3#70B", wantFam: "llama", wantVersion: "3.3", wantParamSize: "70b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fam, variant, version, paramSize, mods, err := parseEntityTuple(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseEntityTuple(%q) err = nil, want an error", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEntityTuple(%q) unexpected error: %v", tc.input, err)
			}
			if fam != tc.wantFam {
				t.Errorf("family = %q, want %q", fam, tc.wantFam)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if paramSize != tc.wantParamSize {
				t.Errorf("paramSize = %q, want %q", paramSize, tc.wantParamSize)
			}
			if !equalStrings(mods, tc.wantMods) {
				t.Errorf("mods = %v, want %v", mods, tc.wantMods)
			}
		})
	}
}

// TestEntityKey_SizedRoundTrip verifies the full round-trip property: for both
// sized and unsized EntityRefs, String() produces a key that parses back to
// identical components, and re-calling String() on the reconstructed EntityRef
// produces a byte-identical key.
func TestEntityKey_SizedRoundTrip(t *testing.T) {
	cases := []struct {
		ref  bestiary.EntityRef
		want string
	}{
		{
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
			want: "llama@3.3#70b{instruct}",
		},
		{
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "8b", Modifier: []string{"instruct"}},
			want: "llama@3.3#8b{instruct}",
		},
		{
			ref:  bestiary.EntityRef{Family: "qwen", Variant: "coder", Version: "2.5", ParamSize: "7b"},
			want: "qwen/coder@2.5#7b",
		},
		// Unsized round-trip: no #size segment must survive the parse and re-render.
		{
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"},
			want: "claude/opus@4.5",
		},
		{
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3", Modifier: []string{"instruct"}},
			want: "llama@3.3{instruct}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			key := tc.ref.String()
			if key != tc.want {
				t.Fatalf("EntityRef.String() = %q, want %q", key, tc.want)
			}

			// Parse the key back to its components.
			fam, variant, version, paramSize, mods, err := parseEntityTuple(key)
			if err != nil {
				t.Fatalf("parseEntityTuple(%q) unexpected error: %v", key, err)
			}

			// Reconstruct an EntityRef from parsed components.
			reconstructed := bestiary.EntityRef{
				Family:    fam,
				Variant:   variant,
				Version:   version,
				ParamSize: paramSize,
				Modifier:  mods,
			}
			rekey := reconstructed.String()

			// The re-computed key must be byte-identical.
			if rekey != key {
				t.Errorf("round-trip failed: original=%q re-computed=%q", key, rekey)
			}
		})
	}
}

// TestParseEntityTuple_ParamSizeCanonicalization pins the canonicalization rule at
// the CLI parse boundary: uppercase size tokens are lowercased so that
// "llama@3.3#70B" resolves to the same entity key as "llama@3.3#70b". The
// canonical form is always lowercase, matching EntityRef.ParamSize storage.
func TestParseEntityTuple_ParamSizeCanonicalization(t *testing.T) {
	cases := []struct {
		input         string
		wantParamSize string
	}{
		// Uppercase B is lowercased at parse boundary.
		{"llama@3.3#70B", "70b"},
		{"llama@3.3#8B", "8b"},
		// Already lowercase: unchanged.
		{"llama@3.3#70b", "70b"},
		// MoE with uppercase: lowercased.
		{"qwen#8X22B", "8x22b"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			_, _, _, paramSize, _, err := parseEntityTuple(tc.input)
			if err != nil {
				t.Fatalf("parseEntityTuple(%q) unexpected error: %v", tc.input, err)
			}
			if paramSize != tc.wantParamSize {
				t.Errorf("paramSize = %q, want %q", paramSize, tc.wantParamSize)
			}
		})
	}
}

// TestLookupEntity_TuplePath_SizedMiss guards lookupEntity's tuple path (mutation c):
// a sized key must MISS against the real static registry, which contains only unsized
// entities. If the paramSize were dropped from the EntityByTuple call in lookupEntity,
// the sized tuple would resolve to the unsized entity — a wrong-merge.
func TestLookupEntity_TuplePath_SizedMiss(t *testing.T) {
	// Pick any real entity from the static registry; use its key with an appended
	// size segment. The sized variant does not exist in static data, so it must miss.
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("static registry empty; cannot exercise lookupEntity tuple path")
	}
	realEntity := entities[0]
	// Build a sized key by appending "#99b" — no static entity has this size.
	sizedKey := realEntity.Ref.String() + "#99b"

	_, ok := lookupEntity(sizedKey)
	if ok {
		t.Errorf("lookupEntity(%q) = (entity, true), want miss: sized key must not resolve to unsized entity", sizedKey)
	}
}

// TestLookupEntity_TuplePath_UnsizedHit verifies the tuple path resolves a real
// unsized entity key from the registry.
func TestLookupEntity_TuplePath_UnsizedHit(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("static registry empty")
	}
	realEntity := entities[0]
	key := realEntity.Ref.String()

	got, ok := lookupEntity(key)
	if !ok {
		t.Fatalf("lookupEntity(%q) = miss, want a hit for a real entity key", key)
	}
	if got.Ref.String() != key {
		t.Errorf("lookupEntity(%q) returned entity key %q, want %q", key, got.Ref.String(), key)
	}
}

// TestLookupEntity_FallbackPath_ParamSizeConsistency exercises lookupEntity's
// model-ID fallback path and verifies that the resolved entity's ParamSize is
// consistent with the looked-up model's ParamSize.
//
// Constraint: all current static models carry ParamSize="" (sized model rows are
// baked at codegen time, not yet present in the static data). Therefore this test
// cannot falsify the specific mutant of replacing m.ParamSize with "" at main.go:341
// — both produce the same result when all models are unsized. The synthesized-registry
// tests in paramsize_wiring_internal_test.go guard the EntityByTuple carrier semantics
// (that ParamSize participates correctly in entity keying), but they never execute the
// main.go:341 call site; the argument binding at that call site remains unfalsifiable
// from this package until sized static data ships. A sized-fallback test is deferred
// to when sized model rows are present in the static registry.
func TestLookupEntity_FallbackPath_ParamSizeConsistency(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Skip("static registry empty")
	}
	// Find a model that successfully resolves via the fallback path.
	found := false
	for _, m := range models {
		e, ok := lookupEntity(string(m.ID))
		if !ok {
			continue
		}
		found = true
		// The entity's ParamSize must equal the model's ParamSize.
		// With current static data both are "" so this assertion cannot distinguish
		// m.ParamSize from a hardcoded "": a mutant replacing m.ParamSize with ""
		// still resolves the same unsized entity and passes. The assertion documents
		// the required invariant and will hold correctly once unsized data remains
		// unsized, but it does not falsify the hardcoded-"" mutant today.
		if e.Ref.ParamSize != m.ParamSize {
			t.Errorf("model %q: lookupEntity fallback returned entity.ParamSize=%q, want %q (m.ParamSize); "+
				"the fallback path must thread m.ParamSize to EntityByTuple, not a hardcoded empty string",
				m.ID, e.Ref.ParamSize, m.ParamSize)
		}
		break
	}
	if !found {
		t.Fatal("no static model resolved via the fallback path; cannot verify ParamSize consistency")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// pickMultiProviderEntity returns a stable, genuinely multi-provider entity from
// the registry so the CLI end-to-end tests assert against real rolled-up data
// without hardcoding a model id.
func pickMultiProviderEntity(t *testing.T) bestiary.Entity {
	t.Helper()
	for _, e := range bestiary.Entities() {
		seen := map[bestiary.Provider]struct{}{}
		for _, in := range e.Instances {
			seen[in.Provider] = struct{}{}
		}
		if len(seen) >= 2 {
			return e
		}
	}
	t.Fatal("no multi-provider entity found in the registry")
	return bestiary.Entity{}
}

// TestRun_Providers_Table drives `providers <tuple> --output=table` end-to-end and
// asserts the entity header, the instance table header, and a real provider name
// render to stdout.
func TestRun_Providers_Table(t *testing.T) {
	e := pickMultiProviderEntity(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", "--output=table", e.Ref.String()})
	})
	if runErr != nil {
		t.Fatalf("run providers %q returned error: %v", e.Ref.String(), runErr)
	}
	if !strings.Contains(out, "Entity: "+e.Ref.String()) {
		t.Errorf("output missing entity header for %q; got:\n%s", e.Ref.String(), out)
	}
	if !strings.Contains(out, "Instances (") {
		t.Errorf("output missing instance table header; got:\n%s", out)
	}
	if !strings.Contains(out, string(e.Providers[0])) {
		t.Errorf("output missing provider %q; got:\n%s", e.Providers[0], out)
	}
}

// TestRun_Providers_JSON drives the default (json) output and asserts the rendered
// instance array parses and carries one entry per instance.
func TestRun_Providers_JSON(t *testing.T) {
	e := pickMultiProviderEntity(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", e.Ref.String()})
	})
	if runErr != nil {
		t.Fatalf("run providers %q returned error: %v", e.Ref.String(), runErr)
	}
	var insts []map[string]any
	if err := json.Unmarshal([]byte(out), &insts); err != nil {
		t.Fatalf("providers json output did not parse: %v\noutput:\n%s", err, out)
	}
	if len(insts) != len(e.Instances) {
		t.Errorf("json instance count = %d, want %d", len(insts), len(e.Instances))
	}
}

// TestRun_ShowByEntity_Table drives `show --by-entity <tuple> --output=table` and
// asserts the aggregate view renders (identity, provider rollup, capabilities).
func TestRun_ShowByEntity_Table(t *testing.T) {
	e := pickMultiProviderEntity(t)
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--output=table", e.Ref.String()})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity %q returned error: %v", e.Ref.String(), runErr)
	}
	for _, want := range []string{"Entity: " + e.Ref.String(), "Providers (", "Capabilities:", "Instances ("} {
		if !strings.Contains(out, want) {
			t.Errorf("entity view missing %q; got:\n%s", want, out)
		}
	}
}

// TestRun_ShowByEntity_ModelIDFallback verifies the lookupEntity fallback: passing
// a concrete model ID (not a tuple) resolves to that model's entity.
func TestRun_ShowByEntity_ModelIDFallback(t *testing.T) {
	// Choose an entity and one of its concrete instance IDs.
	e := pickMultiProviderEntity(t)
	instID := string(e.Instances[0].ID)

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--output=table", instID})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity %q (model-id fallback) returned error: %v", instID, runErr)
	}
	if !strings.Contains(out, "Entity: "+e.Ref.String()) {
		t.Errorf("model-id %q did not resolve to entity %q; got:\n%s", instID, e.Ref.String(), out)
	}
}

// TestRun_Entity_UnsupportedOutput verifies the entity commands reject an output
// format they cannot render (yaml, or a typo) with an actionable error rather than
// silently falling through to the table renderer.
func TestRun_Entity_UnsupportedOutput(t *testing.T) {
	e := pickMultiProviderEntity(t)
	for _, bad := range []string{"yaml", "tabel"} {
		t.Run(bad, func(t *testing.T) {
			err := run([]string{"providers", "--output=" + bad, e.Ref.String()})
			if err == nil {
				t.Fatalf("run providers --output=%s returned nil error, want an unsupported-format error", bad)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "unsupported") || !strings.Contains(msg, "json, table") {
				t.Errorf("error = %q; want it to flag the unsupported format and list json, table", err.Error())
			}
			// The error string itself must NOT carry a "bestiary:" prefix — main()
			// prepends exactly one. Asserting on the raw string here, AND on the
			// rendered boundary below, locks the single-prefix convention so a
			// doubled "bestiary: bestiary:" can't regress.
			if strings.HasPrefix(err.Error(), "bestiary:") {
				t.Errorf("validateEntityOutput error %q must not embed the 'bestiary:' prefix; main() adds it", err.Error())
			}
			// Replicate main()'s rendering ("bestiary: %v") and assert exactly one
			// "bestiary:" prefix appears in what the user would see on stderr.
			rendered := "bestiary: " + err.Error()
			if n := strings.Count(rendered, "bestiary:"); n != 1 {
				t.Errorf("rendered error %q has %d 'bestiary:' prefixes, want exactly 1", rendered, n)
			}
			// show --by-entity must reject the same way.
			if err := run([]string{"show", "--by-entity", "--output=" + bad, e.Ref.String()}); err == nil {
				t.Errorf("run show --by-entity --output=%s returned nil error, want an unsupported-format error", bad)
			}
		})
	}
}

// TestRun_Providers_NotFound verifies a bogus tuple yields an actionable
// not-found error.
func TestRun_Providers_NotFound(t *testing.T) {
	err := run([]string{"providers", "no-such-family/no-variant@no-version"})
	if err == nil {
		t.Fatal("run providers <bogus> returned nil error, want a not-found error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("error = %q, want it to mention 'not found'", err.Error())
	}
}
