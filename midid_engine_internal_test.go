package bestiary

import (
	"strings"
	"testing"
)

// midid_engine_internal_test.go — internal pins for the mid-ID token engine: the
// phase-B stage/mode harvest in extractModifiers, which recovers identity/attribute
// modifiers buried before the variant/version boundary. Internal package so the tests can
// (a) call the unexported extractModifiers directly and (b) read/temporarily remove an
// idFamilyOverrides entry.
//
// The stage/mode overrides the engine subsumed have been RETIRED from idFamilyOverrides.
// Each retired ID keeps a retained decomposition pin here: a literal (family, variant,
// version, modifiers) tuple that the mechanical decomposition of the bare ID must
// reproduce, plus an assertion that its override entry is gone. A few dotted-version
// gpt-realtime IDs were NOT retired — the engine harvests their realtime modifier but a
// dotted version glued behind that token is not yet mechanically recovered, so the
// override still owns the version; those are pinned separately with a promotion tripwire.

// decompStr renders a decomposition tuple for readable failures.
func decompStr(f Family, v, ver string, mods []string) string {
	return string(f) + "|" + v + "|" + ver + "|{" + strings.Join(mods, ",") + "}"
}

// modsContain reports whether want is present in mods (order-independent).
func modsContain(mods []string, want string) bool {
	for _, m := range mods {
		if strings.EqualFold(m, want) {
			return true
		}
	}
	return false
}

// decomposeWithoutOverride removes the exact-ID override for id (if any), runs the FULL
// public decomposition through ParseFamilyDetailed, then restores the override BEFORE
// returning so a later assertion failure can never leave the map mutated. raw="" exercises
// the mechanical (empty-family) path the override converges. Provider is irrelevant to the
// parse logic (documented on ParseFamilyDetailed) so "" is used.
func decomposeWithoutOverride(id string) (Family, string, string, []string) {
	key := strings.ToLower(id)
	ov, had := idFamilyOverrides[key]
	if had {
		delete(idFamilyOverrides, key)
	}
	f, v, ver, mods, _ := ParseFamilyDetailed("", ModelID(id), "")
	if had {
		idFamilyOverrides[key] = ov
	}
	return f, v, ver, mods
}

// retiredStageModeOverride is a stage/mode exact-ID entry that was RETIRED from
// idFamilyOverrides once the mid-ID engine derived it mechanically. Each row carries the
// exact decomposition the entry used to pin, retained here as a literal so the pin
// survives the entry's deletion. mods is the pre-canonicalization modifier list.
type retiredStageModeOverride struct {
	id      string
	family  Family
	variant string
	version string
	mods    []string
}

// retiredStageModeOverrides is the full set of stage/mode overrides retired in favor of
// the mid-ID engine. For each, the buried omni/livetranslate/realtime token — previously
// unreachable behind the variant/version/size boundary — is now harvested mechanically,
// and family/variant/version already agree, so the mechanical decomposition of the bare
// ID reproduces the retained tuple with no override present.
var retiredStageModeOverrides = []retiredStageModeOverride{
	{"gemini-omni-flash-preview", "gemini", "flash", "", []string{"omni", "preview"}},
	{"google/gemini-omni-flash-preview", "gemini", "flash", "", []string{"omni", "preview"}},
	{"openai/gpt-realtime-2", "gpt", "", "2", []string{"realtime"}},
	{"openai/gpt-realtime-mini", "gpt", "mini", "", []string{"realtime"}},
	{"qwen-omni-turbo", "qwen", "turbo", "", []string{"omni"}},
	{"qwen-omni-turbo-realtime", "qwen", "turbo", "", []string{"omni", "realtime"}},
	{"qwen3-omni-flash", "qwen", "flash", "3", []string{"omni"}},
	{"qwen3-omni-flash-realtime", "qwen", "flash", "3", []string{"omni", "realtime"}},
	{"qwen3.5-omni-flash", "qwen", "flash", "3.5", []string{"omni"}},
	{"qwen3.5-omni-plus", "qwen", "plus", "3.5", []string{"omni"}},
	{"qwen/qwen3-omni-30b-a3b-instruct", "qwen", "", "3", []string{"omni", "instruct"}},
	{"qwen/qwen3-omni-30b-a3b-thinking", "qwen", "", "3", []string{"omni", "thinking"}},
	{"qwen3-livetranslate-flash-realtime", "qwen", "flash", "3", []string{"livetranslate", "realtime"}},
	{"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning", "nemotron", "nano", "3", []string{"omni", "reasoning"}},
	{"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning-bf16", "nemotron", "nano", "3", []string{"omni", "reasoning"}},
	{"nvidia/nemotron-3-nano-omni-30b-a3b-reasoning:free", "nemotron", "nano", "3", []string{"omni", "reasoning"}},
}

// TestMidIDEngine_StageOverrideEquivalence_FullyDerivable is the retained decomposition
// pin for the retired stage/mode overrides. For each retired ID it asserts (1) the
// override entry is GONE from idFamilyOverrides (the retirement actually happened) and
// (2) the mechanical decomposition of the bare ID reproduces the retained tuple exactly.
// A failure means either the mid-ID harvest regressed or an entry was retired without an
// equivalent mechanical derivation.
func TestMidIDEngine_StageOverrideEquivalence_FullyDerivable(t *testing.T) {
	for _, c := range retiredStageModeOverrides {
		if _, ok := idFamilyOverrides[strings.ToLower(c.id)]; ok {
			t.Errorf("%s: still has an idFamilyOverrides entry — it was retired because the mid-ID engine derives it mechanically", c.id)
		}
		f, v, ver, mods, _ := ParseFamilyDetailed("", ModelID(c.id), "")
		got := decompStr(f, v, ver, mods)
		want := decompStr(c.family, c.variant, c.version, CanonicalizeModifiers(c.mods))
		if got != want {
			t.Errorf("%s: mechanical decomposition = %s, want (retained pin) = %s", c.id, got, want)
		}
	}
}

// TestMidIDEngine_StageOverrideEquivalence_RealtimeModifierOnly pins the boundary of the
// engine's reach: the dotted-version gpt-realtime IDs. The engine DOES harvest the mid-ID
// realtime token (its job) and resolves family=gpt, but a SEPARATE version-extraction gap
// (a dotted version sitting behind the mid-ID realtime token) leaves the version
// unrecovered, so these are NOT yet fully retirement-ready — the override still owns the
// version. Pinning the exact current mechanical version documents the gap: if the version
// extractor later closes it, this test flags the ID for promotion into
// stageModeFullyDerivable.
func TestMidIDEngine_StageOverrideEquivalence_RealtimeModifierOnly(t *testing.T) {
	cases := []struct {
		id          string
		wantVersion string // current mechanical version (the documented gap)
	}{
		{"gpt-realtime-2.1", ""},
		{"openai/gpt-realtime-2.1", ""},
		{"openai/gpt-realtime-1.5", ""},
	}
	for _, c := range cases {
		ov := idFamilyOverrides[strings.ToLower(c.id)]
		f, v, ver, mods := decomposeWithoutOverride(c.id)
		// Engine's contribution: realtime harvested, family + variant agree with the pin.
		if !modsContain(mods, "realtime") {
			t.Errorf("%s: mid-ID realtime not harvested; mods=%v", c.id, mods)
		}
		if f != ov.family || v != ov.variant {
			t.Errorf("%s: family/variant = (%q,%q), want (%q,%q)", c.id, f, v, ov.family, ov.variant)
		}
		if strings.Join(CanonicalizeModifiers(mods), ",") != strings.Join(CanonicalizeModifiers(ov.modifiers), ",") {
			t.Errorf("%s: modifiers = %v, want %v", c.id, mods, ov.modifiers)
		}
		// Documented gap: version not yet mechanically recovered (override still needed).
		if ver != c.wantVersion {
			t.Errorf("%s: mechanical version = %q, want %q (version-extraction gap changed — "+
				"if now == pinned %q, retire this override and move the ID into retiredStageModeOverrides)",
				c.id, ver, c.wantVersion, ov.version)
		}
	}
}

// TestMidIDEngine_ExtractModifiers_MidIDHarvest exercises the phase-B harvest directly and
// pins the `consumed` invariant: a mid-ID modifier is added to the modifier list but NEVER
// grows `consumed` (it is not a trailing substring, so the version/date extractors must
// still see it in place). Phase-A trailing modifiers still populate `consumed` as before.
func TestMidIDEngine_ExtractModifiers_MidIDHarvest(t *testing.T) {
	cases := []struct {
		name         string
		id           ModelID
		family       Family
		variant      string
		wantMods     []string // must all be present (order-independent)
		wantConsumed string   // exact
	}{
		{
			name: "gemini omni before variant, preview trails",
			id:   "gemini-omni-flash-preview", family: "gemini", variant: "flash",
			wantMods: []string{"omni", "preview"}, wantConsumed: "-preview",
		},
		{
			name: "gpt realtime before bare version, nothing trails",
			id:   "gpt-realtime-2.1", family: "gpt", variant: "",
			wantMods: []string{"realtime"}, wantConsumed: "",
		},
		{
			name: "livetranslate before variant, realtime trails",
			id:   "qwen3-livetranslate-flash-realtime", family: "qwen", variant: "flash",
			wantMods: []string{"livetranslate", "realtime"}, wantConsumed: "-realtime",
		},
		{
			name: "omni behind a split MoE size, instruct trails",
			id:   "qwen/qwen3-omni-30b-a3b-instruct", family: "qwen", variant: "",
			wantMods: []string{"omni", "instruct"}, wantConsumed: "-instruct",
		},
		{
			name: "realtime before variant (mini)",
			id:   "openai/gpt-realtime-mini", family: "gpt", variant: "mini",
			wantMods: []string{"realtime"}, wantConsumed: "",
		},
	}
	for _, c := range cases {
		mods, consumed := extractModifiers(c.id, c.family, c.variant)
		for _, want := range c.wantMods {
			if !modsContain(mods, want) {
				t.Errorf("%s (%s): mods=%v missing %q", c.name, c.id, mods, want)
			}
		}
		if consumed != c.wantConsumed {
			t.Errorf("%s (%s): consumed=%q, want %q (mid-ID token must NOT grow consumed)",
				c.name, c.id, consumed, c.wantConsumed)
		}
	}
}

// TestMidIDEngine_Guards pins the guards that keep the mid-ID harvest from over-capturing:
// the variant-guard (incl. compound variants), the family-token guard, and the closed
// token set. These are the invariants that prevent regressions on the rest of the catalog.
func TestMidIDEngine_Guards(t *testing.T) {
	t.Run("compound-variant component is not double-harvested", func(t *testing.T) {
		// mimo-v2-omni-free carries the upstream compound variant "omni-free"; "omni" is
		// already a component, so the harvest must emit nothing (regression pin: without the
		// component-guard this flipped Modifier nil→[omni] and broke codegen byte-identity).
		mods, consumed := extractModifiers("mimo-v2-omni-free", "mimo", "omni-free")
		if len(mods) != 0 {
			t.Errorf("mimo-v2-omni-free: mods=%v, want none (omni is a variant component)", mods)
		}
		if consumed != "" {
			t.Errorf("mimo-v2-omni-free: consumed=%q, want \"\"", consumed)
		}
	})
	t.Run("stage/mode token that IS the variant is not harvested", func(t *testing.T) {
		mods, _ := extractModifiers("qwen-omni", "qwen", "omni")
		if modsContain(mods, "omni") {
			t.Errorf("qwen-omni (variant=omni): omni wrongly harvested; mods=%v", mods)
		}
	})
	t.Run("stage/mode token that IS the family is not harvested", func(t *testing.T) {
		// Synthetic: a family literally named "omni". The family-guard blocks the head token;
		// the trailing preview is still recovered by phase A.
		mods, _ := extractModifiers("omni-1-preview", "omni", "")
		if modsContain(mods, "omni") {
			t.Errorf("omni-1-preview (family=omni): family token wrongly harvested; mods=%v", mods)
		}
		if !modsContain(mods, "preview") {
			t.Errorf("omni-1-preview: trailing preview lost; mods=%v", mods)
		}
	})
	t.Run("only the closed stage/mode set is reached mid-ID", func(t *testing.T) {
		// A trailing-class modifier (instruct) buried BEFORE a variant is NOT harvested mid-ID
		// (phase B is restricted to omni/livetranslate/realtime) — instruct only rides when it
		// trails, exactly as before the engine.
		mods, _ := extractModifiers("acme-instruct-flash", "acme", "flash")
		if modsContain(mods, "instruct") {
			t.Errorf("acme-instruct-flash: instruct wrongly harvested mid-ID; mods=%v", mods)
		}
	})
}

// TestMidIDEngine_NonOverrideUnaffected pins that non-override catalog IDs carrying a
// stage/mode token decompose UNCHANGED — their token either trails or sits behind only
// transparent tokens, so phase A already reached it and phase B adds nothing. This is the
// regression guard behind the byte-identical codegen (no regen).
func TestMidIDEngine_NonOverrideUnaffected(t *testing.T) {
	// nemotron-3-nano-omni: omni trails → phase A harvests it (unchanged).
	if mods, _ := extractModifiers("nemotron-3-nano-omni", "nemotron", "nano"); !modsContain(mods, "omni") {
		t.Errorf("nemotron-3-nano-omni: omni should still be harvested (trailing); mods=%v", mods)
	}
	// qwen2-5-omni-7b: omni sits behind the transparent size 7b → phase A harvests it.
	if mods, consumed := extractModifiers("qwen2-5-omni-7b", "qwen", ""); !modsContain(mods, "omni") || consumed != "-omni-7b" {
		t.Errorf("qwen2-5-omni-7b: mods=%v consumed=%q, want omni present + consumed=-omni-7b", mods, consumed)
	}
}
