package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestParseEntityTuple is a table-driven check of the canonical entity-tuple
// parser: the family[/variant][@version]{identity-mods}[attributes] grammar plus
// the lenient 3-segment family/variant/version form and the discarded trailing
// [attributes] segment.
func TestParseEntityTuple(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantFam     bestiary.Family
		wantVariant string
		wantVersion string
		wantMods    []string
		wantErr     bool
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fam, variant, version, mods, err := parseEntityTuple(tc.input)
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
			if !equalStrings(mods, tc.wantMods) {
				t.Errorf("mods = %v, want %v", mods, tc.wantMods)
			}
		})
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
