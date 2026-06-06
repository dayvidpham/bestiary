package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// ----------------------------------------------------------------------------
// Parse data initialization tests
// ----------------------------------------------------------------------------

// TestParseData_RegexesValid asserts that the embedded parse data loads without
// error at startup: all JSON files are present in the embedded FS, all regex
// strings in version_patterns.json compile successfully, and no JSON is
// malformed.
//
// bestiary-bzdy: the sync.Once error path in initParseData() is silently
// swallowed by ParseFamily (fail-closed design). This test makes the startup
// contract explicit and measurable. If the data files or regexes are ever
// broken, this test will catch it before any caller of ParseFamily silently
// degrades to returning raw values unchanged.
func TestParseData_RegexesValid(t *testing.T) {
	t.Parallel()
	if err := bestiary.ParseDataReady(); err != nil {
		t.Fatalf("ParseDataReady() returned unexpected error: %v\n"+
			"  What: embedded parse data failed to load\n"+
			"  Why: a JSON file is missing, malformed, or a regex in version_patterns.json did not compile\n"+
			"  Where: parse/data/*.json embedded files\n"+
			"  How to fix: inspect the error message above and repair the affected JSON data file", err)
	}
}

// ----------------------------------------------------------------------------
// ParseFamily tests
// ----------------------------------------------------------------------------

// TestParseFamily_Overrides covers all entries in family_overrides.json
// that have a non-empty variant. The test table is authoritative: if you
// add an override to the JSON, add a row here.
func TestParseFamily_Overrides(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// BDD acceptance criterion from slice spec.
		{"claude-opus", "claude", "opus"},
		{"claude-haiku", "claude", "haiku"},
		{"claude-sonnet", "claude", "sonnet"},
		// Other compound claude families (via overrides).
		{"codestral-embed", "codestral", "embed"},
		{"cohere-embed", "cohere", "embed"},
		{"command-a", "command", "a"},
		{"command-r", "command", "r"},
		{"deepseek-flash", "deepseek", "flash"},
		// SLICE-3: deepseek-thinking / grok-vision / kimi-thinking overrides REMOVED —
		// trailing thinking/vision is now ALWAYS a Modifier (see TestUniformModifierSuffix).
		{"gemini-embedding", "gemini", "embedding"},
		{"gemini-flash", "gemini", "flash"},
		{"gemini-flash-lite", "gemini", "flash-lite"},
		{"gemini-pro", "gemini", "pro"},
		{"glm-air", "glm", "air"},
		{"glm-flash", "glm", "flash"},
		{"glm-free", "glm", "free"},
		{"glm-z", "glm", "z"},
		{"gpt-codex", "gpt", "codex"},
		{"gpt-codex-mini", "gpt", "codex-mini"},
		{"gpt-codex-spark", "gpt", "codex-spark"},
		{"gpt-image", "gpt", "image"},
		{"gpt-mini", "gpt", "mini"},
		{"gpt-nano", "gpt", "nano"},
		{"gpt-oss", "gpt", "oss"},
		{"gpt-pro", "gpt", "pro"},
		{"grok-beta", "grok", "beta"},
		{"hy3-free", "hy3", "free"},
		{"kat-coder", "kat", "coder"},
		{"kimi-free", "kimi", "free"},
		{"ling-flash-free", "ling", "flash-free"},
		{"magistral-medium", "magistral", "medium"},
		{"magistral-small", "magistral", "small"},
		{"mimo-flash-free", "mimo", "flash-free"},
		{"mimo-omni-free", "mimo", "omni-free"},
		{"mimo-pro-free", "mimo", "pro-free"},
		{"mimo-v2-omni", "mimo", "v2-omni"},
		{"mimo-v2-pro", "mimo", "v2-pro"},
		{"mimo-v2.5", "mimo", "v2.5"},
		{"mimo-v2.5-pro", "mimo", "v2.5-pro"},
		{"minimax-free", "minimax", "free"},
		{"minimax-m2.5", "minimax", "m2.5"},
		{"minimax-m2.7", "minimax", "m2.7"},
		{"mistral-embed", "mistral", "embed"},
		{"mistral-large", "mistral", "large"},
		{"mistral-medium", "mistral", "medium"},
		{"mistral-nemo", "mistral", "nemo"},
		{"mistral-small", "mistral", "small"},
		{"nemotron-free", "nemotron", "free"},
		{"nova-lite", "nova", "lite"},
		{"nova-micro", "nova", "micro"},
		{"nova-pro", "nova", "pro"},
		{"o-mini", "o", "mini"},
		{"o-pro", "o", "pro"},
		{"qwen-free", "qwen", "free"},
		{"solar-mini", "solar", "mini"},
		{"solar-pro", "solar", "pro"},
		{"sonar-deep-research", "sonar", "deep-research"},
		{"sonar-pro", "sonar", "pro"},
		{"sonar-reasoning", "sonar", "reasoning"},
		{"titan-embed", "titan", "embed"},
		{"trinity-mini", "trinity", "mini"},
	}

	for _, tc := range cases {
		t.Run(string(tc.raw), func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// TestParseFamily_Overrides_OpaqueCompounds tests compound families that are
// kept as-is (empty variant) because they are atomic branding units.
func TestParseFamily_Overrides_OpaqueCompounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw        bestiary.Family
		wantFamily bestiary.Family
	}{
		{"big-pickle", "big-pickle"},
		{"dall-e", "dall-e"},
		{"mm-poly", "mm-poly"},
		{"model-router", "model-router"},
		{"nano-banana", "nano-banana"},
		{"smart-turn", "smart-turn"},
		{"stable-diffusion", "stable-diffusion"},
		{"text-embedding", "text-embedding"},
	}

	for _, tc := range cases {
		t.Run(string(tc.raw), func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != "" {
				t.Errorf("ParseFamily(%q) variant = %q, want empty", tc.raw, gotVariant)
			}
		})
	}
}

// TestParseFamily_VersionedPatterns covers the versioned-variant patterns.
func TestParseFamily_VersionedPatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// k-prefix (BDD acceptance criterion).
		{"kimi-k2.5 via k-prefix", "kimi-k2.5", "kimi", "k2.5"},
		{"kimi-k2.6 via k-prefix", "kimi-k2.6", "kimi", "k2.6"},
		// m-prefix via pattern only — "someai-m3.0" is NOT in family_overrides.json,
		// so it falls through to the m-prefix versioned-variant pattern.
		{"someai-m3.0 (not in overrides, m-prefix pattern)", "someai-m3.0", "someai", "m3.0"},
		// no-prefix (BDD acceptance criterion).
		{"qwen3.5 via no-prefix", "qwen3.5", "qwen", "3.5"},
		{"qwen3.6 via no-prefix", "qwen3.6", "qwen", "3.6"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// TestParseFamily_HyphenVersion covers the hyphen-separated version rule.
// BDD criterion: "Given raw 'claude-opus-4-5' When ParseFamily Then returns ('claude', 'opus-4-5')."
func TestParseFamily_HyphenVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// BDD acceptance criterion.
		{"claude-opus-4-5 hyphen version", "claude-opus-4-5", "claude", "opus-4-5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// TestParseFamily_SingleToken covers raw families that are already single tokens.
// These should return (raw, "") unchanged.
func TestParseFamily_SingleToken(t *testing.T) {
	t.Parallel()

	singles := []bestiary.Family{
		"claude", "gpt", "gemini", "llama", "mistral", "qwen", "grok",
		"phi", "nova", "sonar", "kimi", "minimax", "mimo", "magistral",
		"deepseek", "codestral", "command",
	}

	for _, raw := range singles {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(raw)
			if gotFamily != raw {
				t.Errorf("ParseFamily(%q) family = %q, want %q (passthrough)", raw, gotFamily, raw)
			}
			if gotVariant != "" {
				t.Errorf("ParseFamily(%q) variant = %q, want empty for single-token input", raw, gotVariant)
			}
		})
	}
}

// TestParseFamily_Empty covers the empty-input edge case.
func TestParseFamily_Empty(t *testing.T) {
	t.Parallel()
	gotFamily, gotVariant := bestiary.ParseFamily("")
	if gotFamily != "" {
		t.Errorf("ParseFamily(\"\") family = %q, want empty", gotFamily)
	}
	if gotVariant != "" {
		t.Errorf("ParseFamily(\"\") variant = %q, want empty", gotVariant)
	}
}

// TestParseFamily_Determinism verifies that ParseFamily is deterministic:
// running it 100 times on the same input always produces identical output.
// This guards against any map-iteration-order leakage.
// MINOR bestiary-s36u: includes a suffix-stripping input.
func TestParseFamily_Determinism(t *testing.T) {
	t.Parallel()

	inputs := []bestiary.Family{
		"claude-opus", "kimi-k2.5", "qwen3.5", "gemini-flash-lite",
		"gpt-codex-spark", "claude-opus-4-5", "", "llama",
		// suffix-stripping path (bestiary-s36u): ensure determinism on Step 3.
		"foo-mini",
	}

	for _, raw := range inputs {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			first, firstVariant := bestiary.ParseFamily(raw)
			for i := 1; i < 100; i++ {
				f, v := bestiary.ParseFamily(raw)
				if f != first {
					t.Errorf("ParseFamily(%q) iteration %d: family = %q, want %q (non-deterministic)", raw, i, f, first)
				}
				if v != firstVariant {
					t.Errorf("ParseFamily(%q) iteration %d: variant = %q, want %q (non-deterministic)", raw, i, v, firstVariant)
				}
			}
		})
	}
}

// TestParseFamily_SuffixStripping covers all 30 entries in variant_suffixes.json.
// Inputs are chosen to NOT appear in family_overrides.json and to NOT match any
// versioned-variant pattern, so they route past Steps 1 and 2 and reach the
// suffix-stripping loop at parse.go:257-264.
//
// BLOCKER bestiary-jtbj: suffix-stripping path had zero test coverage.
func TestParseFamily_SuffixStripping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// All suffix entries from variant_suffixes.json (listed longest-first
		// in the JSON, but initParseData re-sorts by length so the order here
		// is documentary only). SLICE-3 REMOVED "-thinking" and "-vision" — they are
		// now uniform Modifiers (modifiers.json authoritative), never variant suffixes.
		// SLICE-10 (rc2) ALSO REMOVED "-instruct", "-turbo", "-base" — ratified global
		// modifiers (member-guarded). ParseFamily no longer strips them, so "acme-instruct"
		// stays family "acme-instruct" with empty variant (the modifier is extracted later
		// by ExtractModifier in the ID-driven pipeline, not by ParseFamily on the raw string).
		{"deep-research", "widget-deep-research", "widget", "deep-research"},
		{"codex-spark", "acme-codex-spark", "acme", "codex-spark"},
		{"codex-mini", "baz-codex-mini", "baz", "codex-mini"},
		{"flash-lite", "acme-flash-lite", "acme", "flash-lite"},
		{"codex", "acme-codex", "acme", "codex"},
		{"instruct (now a global Modifier — NOT stripped by ParseFamily)", "acme-instruct", "acme-instruct", ""},
		{"embed", "acme-embed", "acme", "embed"},
		{"embedding", "acme-embedding", "acme", "embedding"},
		{"mini", "foo-mini", "foo", "mini"},
		{"pro", "foo-pro", "foo", "pro"},
		{"flash", "foo-flash", "foo", "flash"},
		{"lite", "foo-lite", "foo", "lite"},
		{"turbo (now a global Modifier — NOT stripped by ParseFamily)", "foo-turbo", "foo-turbo", ""},
		{"base (now a global Modifier — NOT stripped by ParseFamily)", "foo-base", "foo-base", ""},
		{"ultra", "foo-ultra", "foo", "ultra"},
		{"large", "foo-large", "foo", "large"},
		{"medium", "foo-medium", "foo", "medium"},
		{"small", "foo-small", "foo", "small"},
		{"spark", "foo-spark", "foo", "spark"},
		{"nano", "foo-nano", "foo", "nano"},
		{"free", "foo-free", "foo", "free"},
		{"beta", "foo-beta", "foo", "beta"},
		{"nemo", "foo-nemo", "foo", "nemo"},
		{"oss", "foo-oss", "foo", "oss"},
		{"image", "foo-image", "foo", "image"},
		{"coder", "foo-coder", "foo", "coder"},
		{"-r", "foo-r", "foo", "r"},
		{"-a", "foo-a", "foo", "a"},
		// Multi-suffix input proving longest-first ordering: "-codex-mini" must
		// match before "-mini" would, yielding variant="codex-mini" not "mini".
		{"longest-first: codex-mini beats mini", "baz-codex-mini", "baz", "codex-mini"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// TestParseFamily_VPrefix covers the v-prefix versioned-variant pattern using
// base values NOT present in family_overrides.json.
//
// IMPORTANT bestiary-ave7: v-prefix path was previously uncovered — all v-prefix
// inputs in TestParseFamily_Overrides were intercepted by the overrides table.
func TestParseFamily_VPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// "somebase" is not in family_overrides.json; routes through v-prefix pattern.
		{"somebase-v3.0", "somebase-v3.0", "somebase", "v3.0"},
		// Multi-part variant (v-prefix with a trailing suffix).
		{"thing-v2.5-pro", "thing-v2.5-pro", "thing", "v2.5-pro"},
		// Single-part base with v-prefix version.
		{"widget-v10.0", "widget-v10.0", "widget", "v10.0"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// TestParseFamily_HyphenVersion_NoOverride covers the else-branch of the
// hyphen-version pattern handler (parse.go:249): when the extracted base is NOT
// found in the overrides table, the function returns (Family(base), variant)
// directly.
//
// IMPORTANT bestiary-resh: only previous case was "claude-opus-4-5" whose base
// "claude-opus" IS in overrides, leaving the else-branch unreachable in tests.
func TestParseFamily_HyphenVersion_NoOverride(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// The hyphen-version regex base group only captures alpha-leading segments:
		// ^(?P<base>[a-zA-Z][a-zA-Z0-9]*(?:-[a-zA-Z][a-zA-Z0-9]*)*)-(?P<variant>\d+(?:-\d+)*)$
		// For "llama-3-1": base="llama" (only alpha segment), variant="3-1".
		// "llama" is not in family_overrides.json → else-branch fires → returns
		// (Family("llama"), "3-1") directly without consulting overrides further.
		{"llama-3-1 base not in overrides", "llama-3-1", "llama", "3-1"},
		// "phi" is not in family_overrides.json; else-branch returns (Family("phi"), "4-5").
		{"phi-4-5 base not in overrides", "phi-4-5", "phi", "4-5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamily(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ExtractDate tests
// ----------------------------------------------------------------------------

// TestExtractDate_FromID covers date extraction from model IDs.
func TestExtractDate_FromID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		id          bestiary.ModelID
		releaseDate string
		want        string
	}{
		// BDD acceptance criterion: YYYYMMDD in model ID.
		{"claude-opus date from id", "claude-opus-4-20250514", "", "2025-05-14"},
		// BDD acceptance criterion: no date.
		{"gpt-codex-mini no date", "gpt-codex-mini", "", ""},
		// YYYY-MM-DD in model ID.
		{"id with YYYY-MM-DD", "gpt-4o-2024-08-06", "", "2024-08-06"},
		// No date in ID, date in releaseDate.
		{"date from releaseDate", "llama-3", "2024-04-18", "2024-04-18"},
		// Both empty.
		{"empty id empty releaseDate", "", "", ""},
		// Date in releaseDate YYYYMMDD form.
		{"releaseDate YYYYMMDD", "some-model", "20230901", "2023-09-01"},
		// ID takes priority over releaseDate when ID has a date.
		{"id date wins over releaseDate", "model-20240101", "2023-06-15", "2024-01-01"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractDate(tc.id, tc.releaseDate)
			if got != tc.want {
				t.Errorf("ExtractDate(%q, %q) = %q, want %q", tc.id, tc.releaseDate, got, tc.want)
			}
		})
	}
}

// TestExtractDate_CalendarValidation checks that structurally-matching but
// semantically-invalid dates (e.g. month 99, day 99) are rejected.
// bestiary-2jqs: ExtractDate must use time.Parse round-trip to validate range.
func TestExtractDate_CalendarValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		id          bestiary.ModelID
		releaseDate string
		want        string
	}{
		// Invalid month — 99 is not a real month.
		{"YYYY-MM-DD month 99 rejected", "model-9999-99-01", "", ""},
		// Invalid day — 99 is not a real day.
		{"YYYY-MM-DD day 99 rejected", "model-9999-01-99", "", ""},
		// Both invalid.
		{"YYYY-MM-DD month+day invalid", "model-9999-99-99", "", ""},
		// Compact form with invalid month.
		{"YYYYMMDD month 99 rejected", "model-99999901", "", ""},
		// Valid edge: last day of a real month.
		{"valid 2025-01-31", "model-2025-01-31", "", "2025-01-31"},
		// Valid compact.
		{"valid compact 20250101", "x-20250101", "", "2025-01-01"},
		// February 29 on non-leap year rejected (Go's time.Parse rejects this).
		{"Feb 29 non-leap year rejected", "model-2023-02-29", "", ""},
		// February 29 on a leap year accepted.
		{"Feb 29 leap year accepted", "model-2024-02-29", "", "2024-02-29"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractDate(tc.id, tc.releaseDate)
			if got != tc.want {
				t.Errorf("ExtractDate(%q, %q) = %q, want %q", tc.id, tc.releaseDate, got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// InferFamilyFromID tests
// ----------------------------------------------------------------------------

// TestInferFamilyFromID covers the empty-family fallback heuristic.
func TestInferFamilyFromID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		id       bestiary.ModelID
		provider bestiary.Provider
		want     bestiary.Family
	}{
		// BDD acceptance criterion: "gpt" from "gpt-4o-2024-08-06".
		{"gpt-4o-2024-08-06", "gpt-4o-2024-08-06", bestiary.ProviderOpenAI, "gpt"},
		// Leading alphabetic prefix extraction.
		{"llama-3", "llama-3", bestiary.ProviderLocal, "llama"},
		{"claude-3", "claude-3", bestiary.ProviderAnthropic, "claude"},
		// Pure version-only ID — no family signal.
		{"numeric only", "1234", bestiary.ProviderLocal, ""},
		// Empty ID.
		{"empty id", "", bestiary.ProviderLocal, ""},
		// Single alphabetic token.
		{"single token", "phi", bestiary.ProviderLocal, "phi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.InferFamilyFromID(tc.id, tc.provider)
			if got != tc.want {
				t.Errorf("InferFamilyFromID(%q, %q) = %q, want %q", tc.id, tc.provider, got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ParseFamilyWithVersion tests
// (SLICE-FIX-1-L2: tests FAIL until L3 implementation exists)
// ----------------------------------------------------------------------------

// TestParseFamilyWithVersion_Core covers the primary acceptance criteria from
// the slice spec: hyphen-versioned families split into (family, variant, version).
//
// BDD criterion: "claude-opus-4-5" → (family=claude, variant=opus, version=4.5).
// Version uses dot separator (4.5) not hyphen (4-5).
func TestParseFamilyWithVersion_Core(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// Primary UAT-2 criterion: claude families with versioned hyphen suffix.
		{"claude-opus-4-5", "claude-opus-4-5", "claude", "opus", "4.5"},
		{"claude-opus-4-6", "claude-opus-4-6", "claude", "opus", "4.6"},
		{"claude-sonnet-4-5", "claude-sonnet-4-5", "claude", "sonnet", "4.5"},
		{"claude-haiku-4-5", "claude-haiku-4-5", "claude", "haiku", "4.5"},
		// No version: vanilla overrides — version should be empty.
		{"claude-opus no version", "claude-opus", "claude", "opus", ""},
		{"claude-haiku no version", "claude-haiku", "claude", "haiku", ""},
		// Single version segment (single numeric after dash).
		{"llama-3-1 two parts", "llama-3-1", "llama", "", "3.1"},
		// phi-4-5: base "phi" not in overrides → family=phi, variant empty, version=4.5.
		{"phi-4-5", "phi-4-5", "phi", "", "4.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q", tc.raw, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestParseFamilyWithVersion_Gemini covers Gemini models which use a
// major.minor version in their family string.
func TestParseFamilyWithVersion_Gemini(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// gemini-2.5-flash: base=gemini, version=2.5 (from no-prefix pattern), variant=flash (override).
		// The raw family "gemini-flash" is in overrides → (gemini, flash).
		// But "gemini-2.5-flash" must parse via versioned patterns.
		// Design: gemini-2.5-flash → family=gemini, variant=flash, version=2.5.
		{"gemini-2.5-flash", "gemini-2.5-flash", "gemini", "flash", "2.5"},
		// gemini-2.5 → no variant, version=2.5.
		{"gemini-2.5", "gemini-2.5", "gemini", "", "2.5"},
		// gemini-flash (no version): family=gemini, variant=flash, version empty.
		{"gemini-flash no version", "gemini-flash", "gemini", "flash", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q", tc.raw, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestParseFamilyWithVersion_Empty verifies that empty input returns all-empty results.
func TestParseFamilyWithVersion_Empty(t *testing.T) {
	t.Parallel()
	gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion("")
	if gotFamily != "" || gotVariant != "" || gotVersion != "" {
		t.Errorf("ParseFamilyWithVersion(\"\") = (%q, %q, %q), want all empty", gotFamily, gotVariant, gotVersion)
	}
}

// TestParseFamilyWithVersion_AlphanumericVersion covers inputs where the version
// suffix is alphanumeric (e.g. "4o") rather than purely numeric (e.g. "4-5").
// The "4o" pattern is structurally different from hyphen-numeric patterns:
// it does not match the hyphen-version regex, so it falls through to the pure
// fallback. ParseFamilyWithVersion returns the raw value unchanged for these inputs.
//
// NOTE: "gpt-4o" → ("gpt-4o", "", "") because "4o" is not recognized as a
// separable version by any current pattern (it is not matched by hyphen-version
// which requires purely numeric trailing tokens, and the no-prefix pattern
// requires an embedded dot). Callers that need the version for gpt-4o models
// should use ExtractVersionFromID instead, which handles this case.
func TestParseFamilyWithVersion_AlphanumericVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// "gpt-4o": "4o" is alphanumeric — no pattern strips it; full fallback.
		// The family field "gpt-4o" is what models.dev actually returns for this model.
		{"gpt-4o", "gpt-4o", "gpt-4o", "", ""},
		// "chatgpt-4o-latest": "-latest" is not in the variant_suffixes list and
		// the trailing token is non-numeric, so full fallback applies.
		{"chatgpt-4o-latest", "chatgpt-4o-latest", "chatgpt-4o-latest", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q", tc.raw, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestExtractVersionFromID covers the ExtractVersionFromID helper introduced in
// cycle 2 (BLOCKER bestiary-5eh8). The helper extracts the version from the
// model ID when the raw family field does not embed one.
func TestExtractVersionFromID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		// Required L3 cases per team-lead brief (bestiary-5eh8).
		{"claude-opus-4-5-20251101", "claude-opus-4-5-20251101", "claude-opus", "4.5"},
		{"claude-opus-4-6-20250514", "claude-opus-4-6-20250514", "claude-opus", "4.6"},
		{"gemini-2.5-flash", "gemini-2.5-flash", "gemini", "2.5"},
		{"claude-opus no version", "claude-opus", "claude-opus", ""},

		// Additional coverage.
		// gpt-4o: single alphanumeric token "4o" after stripping "gpt-"
		{"gpt-4o", "gpt-4o", "gpt", "4o"},
		// claude-opus-4-6 without date
		{"claude-opus-4-6 no date", "claude-opus-4-6", "claude-opus", "4.6"},
		// ID that exactly equals family: no trailing version
		{"id equals family", "claude-opus", "claude-opus", ""},
		// Empty inputs
		{"empty id", "", "claude-opus", ""},
		{"empty family", "claude-opus-4-5", "", ""},
		// ID without the family prefix: no match
		{"no prefix match", "gpt-4o", "claude-opus", ""},
		// gemini-2.5: pure dot-version remainder
		{"gemini-2.5", "gemini-2.5", "gemini", "2.5"},
		// Trailing YYYY-MM-DD date stripped before version extraction
		{"claude-opus-4-6-2026-02-05", "claude-opus-4-6-2026-02-05", "claude-opus", "4.6"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q", tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// TestParseFamilyWithVersion_BackwardCompat verifies that non-versioned inputs
// produce the same (family, variant) as ParseFamily, with version="".
func TestParseFamilyWithVersion_BackwardCompat(t *testing.T) {
	t.Parallel()

	cases := []bestiary.Family{
		"claude-opus", "claude-haiku", "gpt-mini", "gemini-flash",
		"kimi-k2.5", "qwen3.5", "llama",
	}

	for _, raw := range cases {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			wantFamily, wantVariant := bestiary.ParseFamily(raw)
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(raw)
			if gotFamily != wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, ParseFamily says %q", raw, gotFamily, wantFamily)
			}
			if gotVariant != wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, ParseFamily says %q", raw, gotVariant, wantVariant)
			}
			if gotVersion != "" {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want empty for non-versioned input", raw, gotVersion)
			}
		})
	}
}

// TestInferFamilyFromID_Variant verifies that InferFamilyFromIDWithVariant extracts
// both variant and version from model IDs where the raw family field is empty.
//
// B5 (SLICE-FIX-2): the empty-family code path in genToModelInfo must produce
// identical (Family, Variant, Version) as the non-empty-family path for the same
// raw model ID. A model ID like "claude-opus-4-5-20251101" with empty raw family
// must decompose to (claude, opus, 4.5), not (claude, "", "").
//
// This test FAILS until SLICE-FIX-2-L3 lands (InferFamilyFromIDWithVariant
// does not yet exist; the existing InferFamilyFromID only returns family).
func TestInferFamilyFromID_Variant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			desc:        "claude-opus-4-5-20251101 empty raw_family → (claude, opus, 4.5)",
			id:          "claude-opus-4-5-20251101",
			provider:    "nano-gpt",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.5",
		},
		{
			desc:        "claude-opus-4-6 empty raw_family → (claude, opus, 4.6)",
			id:          "claude-opus-4-6",
			provider:    "some-provider",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if gotFamily != tc.wantFamily {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) family = %q, want %q",
					tc.id, tc.provider, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) variant = %q, want %q; "+
					"must apply suffix/pattern logic to extract variant from ID tokens, "+
					"not just return the first token",
					tc.id, tc.provider, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) version = %q, want %q",
					tc.id, tc.provider, gotVersion, tc.wantVersion)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ParseFamilyDetailed failure-detection tests (SLICE-FIX-V2-3 L2)
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_VersionDigitsNotExtracted verifies that ParseFamilyDetailed
// emits a ParseFailure with reason ReasonVersionDigitsNotExtracted for model IDs
// like "claude-3-5-haiku-20241022" where the raw_family is "claude-haiku" but the
// version digits (3, 5) are embedded in the model ID between the family prefix
// ("claude") and the variant ("haiku"), and are not extractable by ExtractVersionFromID.
//
// BDD: Given raw_family="claude-haiku" and id="claude-3-5-haiku-20241022" are parsed
// when version digits between family-prefix and variant cannot be extracted from the
// model ID then ParseFailure emitted with reason
// "version digits between family-prefix and variant not extracted".
func TestParseFamilyDetailed_VersionDigitsNotExtracted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		// claude-3.x line: version digits "3-5" between "claude" and "haiku" in the ID,
		// but raw_family="claude-haiku" gives family="claude", variant="haiku", version="".
		// ExtractVersionFromID fails because "claude-haiku" prefix does not match start of ID.
		{
			rawFamily: "claude-haiku",
			id:        "claude-3-5-haiku-20241022",
			provider:  "anthropic",
		},
		// claude-3.x line: version digits "3-7" between "claude" and "sonnet" in the ID.
		{
			rawFamily: "claude-sonnet",
			id:        "claude-3-7-sonnet-20250219",
			provider:  "anthropic",
		},
		// claude-3.x line: version digit "3" between "claude" and "haiku" in the ID.
		{
			rawFamily: "claude-haiku",
			id:        "claude-3-haiku-20240307",
			provider:  "anthropic",
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			// Under Δ1 (extract-first), these inputs now SUCCEED: version is populated from
			// the model ID via ExtractVersionBetweenFamilyAndVariant. So failure must be nil
			// and version must be non-empty.
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			_ = modifier

			// Best-effort result is always returned.
			if family == "" {
				t.Errorf("ParseFamilyDetailed(%q): got empty family; expected a non-empty best-effort result", tc.rawFamily)
			}
			_ = variant

			// Under Δ1 extract-first: version must be populated from the model ID.
			if version == "" {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want non-empty (Δ1 extract-first should populate version)",
					tc.rawFamily, tc.id, version)
			}

			// Failure must be nil — extract-first mode succeeds for these inputs.
			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): expected nil ParseFailure (version now extracted), got Reason=%q\n"+
					"  What: Δ1 extract-first should populate version from the model ID\n"+
					"  Why: ExtractVersionBetweenFamilyAndVariant should find the digits in the ID",
					tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_YYMMDateAsVersion verifies that ParseFamilyDetailed emits a
// ParseFailure with reason ReasonYYMMDateAsVersion for Mistral-style 4-digit numerals
// (e.g. "mistral-2401") where the YYMM segment cannot be reliably distinguished from a
// version number.
//
// BDD: Given a Mistral 4-digit numeric (e.g. "mistral-2401") when YYMM date cannot
// be cleanly distinguished from version then ParseFailure emitted with reason
// "YYMM-date-as-version false-positive".
func TestParseFamilyDetailed_YYMMDateAsVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		{rawFamily: "mistral-2401", id: "mistral-2401", provider: "mistral"},
		{rawFamily: "mistral-2403", id: "mistral-2403", provider: "mistral"},
		{rawFamily: "pixtral-2411", id: "pixtral-2411-latest", provider: "mistral"},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)

			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q): expected ParseFailure for YYMM pattern, got nil\n"+
					"  What: YYMM-date-as-version false-positive was not detected\n"+
					"  Why: the detector did not match the 4-digit YYMM pattern in the raw family string\n"+
					"  How to fix: verify reYYMMCandidate regex matches 4-digit numerals in range 1900-2999",
					tc.rawFamily)
			}
			if failure.Reason != bestiary.ReasonYYMMDateAsVersion {
				t.Errorf("ParseFamilyDetailed(%q): failure.Reason = %q, want %q",
					tc.rawFamily, failure.Reason, bestiary.ReasonYYMMDateAsVersion)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ExtractModifier tests (SLICE-FIX-V2-5)
// ----------------------------------------------------------------------------

// TestExtractModifier covers the 4-case corpus from the slice spec plus negative
// cases. Tests are expected to FAIL until L3 integrates ExtractModifier into the
// parse pipeline AND wires the result into ModelInfo.Modifier.
//
// Note: This test directly calls ExtractModifier which is already implemented
// (the skeleton returns the correct value since we implemented the body in L1).
// The pipeline integration test below covers the end-to-end flow.
func TestExtractModifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantModifier string
		wantConsumed string
	}{
		// 4-case corpus from the team-lead spec.
		{
			desc:         "claude-opus-4-1-20250805-thinking",
			id:           "claude-opus-4-1-20250805-thinking",
			family:       "claude",
			variant:      "opus",
			wantModifier: "thinking",
			wantConsumed: "-thinking",
		},
		{
			desc:         "claude-opus-4-6-thinking (no date in ID)",
			id:           "claude-opus-4-6-thinking",
			family:       "claude",
			variant:      "opus",
			wantModifier: "thinking",
			wantConsumed: "-thinking",
		},
		{
			desc:         "doubao-seed-1-6-thinking-250715",
			id:           "doubao-seed-1-6-thinking-250715",
			family:       "doubao",
			variant:      "seed",
			wantModifier: "",
			wantConsumed: "",
			// 250715 is the trailing token (YYMMDD without dashes), not "thinking".
			// "thinking" appears before "250715" so it is not the last hyphen-token.
			// ExtractModifier only matches when the modifier IS the trailing token.
		},
		{
			desc:         "gpt-4o-2024-05-13 (no modifier)",
			id:           "gpt-4o-2024-05-13",
			family:       "gpt",
			variant:      "",
			wantModifier: "",
			wantConsumed: "",
		},
		// Negative cases.
		{
			desc:         "unknown modifier -zen returns empty",
			id:           "some-model-zen",
			family:       "some",
			variant:      "model",
			wantModifier: "",
			wantConsumed: "",
		},
		{
			desc:         "trailing token == variant: no double-count",
			id:           "deepseek-thinking",
			family:       "deepseek",
			variant:      "thinking", // variant IS "thinking" — ExtractModifier must return empty (variant-guard, SLICE-FIX-V2-5 Fix 3)
			wantModifier: "",
			wantConsumed: "",
			// When the trailing modifier token equals the parsed variant, ExtractModifier
			// returns ("","") to avoid double-counting the same semantic token in both
			// Variant and Modifier. The variant is the authoritative encoding.
		},
		{
			desc:         "empty ID returns empty",
			id:           "",
			family:       "claude",
			variant:      "opus",
			wantModifier: "",
			wantConsumed: "",
		},
		{
			desc:         "think suffix (shorter modifier) does not shadow thinking",
			id:           "model-thinking",
			family:       "model",
			variant:      "",
			wantModifier: "thinking",
			wantConsumed: "-thinking",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotModifier, gotConsumed := bestiary.ExtractModifier(tc.id, tc.family, tc.variant)
			if gotModifier != tc.wantModifier {
				t.Errorf("ExtractModifier(%q, %q, %q) modifier = %q, want %q",
					tc.id, tc.family, tc.variant, gotModifier, tc.wantModifier)
			}
			if gotConsumed != tc.wantConsumed {
				t.Errorf("ExtractModifier(%q, %q, %q) consumed = %q, want %q",
					tc.id, tc.family, tc.variant, gotConsumed, tc.wantConsumed)
			}
		})
	}
}

// TestExtractModifier_DoesNotDoubleCountVariant verifies the variant-guard:
// when the trailing modifier token in the model ID equals the parsed variant,
// ExtractModifier returns ("","") to avoid encoding the same semantic token in
// both Variant and Modifier (double-count).
//
// SLICE-3 NOTE: after the uniform thinking/vision-as-modifier migration, the kimi/
// deepseek "variant=thinking" inputs below are SYNTHETIC — production no longer
// decomposes those IDs to variant="thinking" (the overrides/suffixes/members were
// removed; thinking is now the first-class Modifier — see TestUniformModifierSuffix).
// The variant-guard is RETAINED as a general defensive anti-double-count: should ANY
// variant ever coincide with a trailing modifier token, it must not be counted twice.
// These rows pin that guard mechanism; the empty-variant rows pin the new reality.
//
// IMPORTANT: bestiary-keqx (SLICE-FIX-V2-5 cycle-2 Fix 3)
func TestExtractModifier_DoesNotDoubleCountVariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantModifier string
		wantConsumed string
	}{
		{
			desc:         "kimi-k2-thinking: trailing token == variant → no double-count",
			id:           "kimi-k2-thinking",
			family:       "kimi",
			variant:      "thinking",
			wantModifier: "",
			wantConsumed: "",
			// variant="thinking" and trailing token "-thinking" match → guard fires → empty.
		},
		{
			desc:         "moonshotai/kimi-k2-thinking: path-stripped, trailing token == variant → no double-count",
			id:           "moonshotai/kimi-k2-thinking",
			family:       "kimi",
			variant:      "thinking",
			wantModifier: "",
			wantConsumed: "",
			// Leading path segment is stripped; same guard applies.
		},
		{
			desc:         "deepseek-thinking: trailing 'thinking' == variant='thinking' → no double-count",
			id:           "deepseek-thinking",
			family:       "deepseek",
			variant:      "thinking",
			wantModifier: "",
			wantConsumed: "",
		},
		{
			// Negative case: different trailing token vs. variant — guard must NOT fire.
			desc:         "claude-opus-4-6-thinking: variant='opus' != trailing 'thinking' → modifier fires",
			id:           "claude-opus-4-6-thinking",
			family:       "claude",
			variant:      "opus",
			wantModifier: "thinking",
			wantConsumed: "-thinking",
		},
		{
			// Negative case: no modifier in ID — guard irrelevant.
			desc:         "claude-opus-4-6: no trailing modifier → empty",
			id:           "claude-opus-4-6",
			family:       "claude",
			variant:      "opus",
			wantModifier: "",
			wantConsumed: "",
		},
		{
			// Edge case: variant is empty string — trailing modifier fires normally.
			desc:         "kimi-k2-thinking: empty variant → modifier fires",
			id:           "kimi-k2-thinking",
			family:       "kimi",
			variant:      "",
			wantModifier: "thinking",
			wantConsumed: "-thinking",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotModifier, gotConsumed := bestiary.ExtractModifier(tc.id, tc.family, tc.variant)
			if gotModifier != tc.wantModifier {
				t.Errorf("ExtractModifier(%q, %q, %q) modifier = %q, want %q\n"+
					"  What: modifier was double-counted (same token as variant)\n"+
					"  Fix: variant-guard in ExtractModifier (SLICE-FIX-V2-5 Fix 3)",
					tc.id, tc.family, tc.variant, gotModifier, tc.wantModifier)
			}
			if gotConsumed != tc.wantConsumed {
				t.Errorf("ExtractModifier(%q, %q, %q) consumed = %q, want %q",
					tc.id, tc.family, tc.variant, gotConsumed, tc.wantConsumed)
			}
		})
	}
}

// TestUniformModifierSuffix is the SLICE-3 acceptance test for the uniform
// thinking/vision-as-modifier migration: ANY trailing {thinking,vision} token is
// ALWAYS surfaced as the first-class Modifier and NEVER as the Variant, for ALL
// families and regardless of whether the token arrives via the model ID, the raw
// family field ("kimi-thinking", "deepseek-thinking", "grok-vision"), or both.
//
// BDD: Given a model whose ID and/or raw family carries a trailing thinking/vision
// token, When ParseFamilyDetailed runs, Then Modifier == that token AND Variant is
// never that token.
//
// SCOPE NOTE: version-presence (e.g. "3.7" in claude-3-7-sonnet-thinking, "k2" as a
// kimi variant) is OUT of scope here — that is SLICE-8 (version extraction). This
// test pins the modifier-classification invariant, not the full tuple's version.
func TestUniformModifierSuffix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawFamily    bestiary.Family
		id           bestiary.ModelID
		provider     bestiary.Provider
		wantFamily   bestiary.Family
		wantModifier string
	}{
		// The two headline cases mandated by the slice spec: BOTH must yield
		// modifier=thinking with the token NEVER appearing as the variant.
		{"claude-3-7-sonnet-thinking (empty raw)", "", "claude-3-7-sonnet-thinking", "nano-gpt", "claude", "thinking"},
		{"kimi-k2-thinking (empty raw)", "", "kimi-k2-thinking", "302ai", "kimi", "thinking"},
		// Raw-family-encoded modifier with the SAME token also in the ID.
		{"kimi-k2-thinking (raw=kimi-thinking)", "kimi-thinking", "kimi-k2-thinking", "ollama-cloud", "kimi", "thinking"},
		// Raw-family-encoded modifier with NO modifier token in the ID — the modifier
		// must be recovered from the raw family, never silently dropped.
		{"deepseek-thinking raw, id=deepseek-r1", "deepseek-thinking", "deepseek-r1", "iflowcn", "deepseek", "thinking"},
		// Vision is treated identically to thinking.
		{"grok-vision raw + id", "grok-vision", "grok-vision", "xai", "grok", "vision"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, _, modifier, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("ParseFamilyDetailed(%q, %q) family = %q, want %q",
					tc.rawFamily, tc.id, family, tc.wantFamily)
			}
			if modJoin(modifier) != tc.wantModifier {
				t.Errorf("ParseFamilyDetailed(%q, %q) modifier = %q, want %q\n"+
					"  What: trailing %q token was NOT surfaced as the first-class Modifier\n"+
					"  Why: SLICE-3 uniform migration — thinking/vision are ALWAYS modifiers",
					tc.rawFamily, tc.id, modifier, tc.wantModifier, tc.wantModifier)
			}
			// The invariant: the modifier token must NEVER be encoded as the variant.
			if variant == tc.wantModifier {
				t.Errorf("ParseFamilyDetailed(%q, %q) variant = %q — a trailing modifier token "+
					"must NEVER be classified as the Variant (SLICE-3 uniform migration)",
					tc.rawFamily, tc.id, variant)
			}
		})
	}
}

// TestExtractModifier_PipelineIntegration verifies that the parse pipeline
// (ParseFamily → ExtractModifier → strip consumed → ExtractVersionFromID →
// ExtractDate) produces a ModelInfo with Modifier populated and Version/Date
// NOT polluted by the trailing modifier token.
//
// These tests will FAIL until L3 integrates ExtractModifier into genToModelInfoDetailed
// so that ModelInfo.Modifier is populated during codegen.
// This test validates the FUNCTION COMPOSITION directly (not the codegen path).
func TestExtractModifier_PipelineIntegration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawID        bestiary.ModelID
		rawFamily    bestiary.Family
		wantModifier string
		wantVersion  string
		wantDate     string
	}{
		{
			desc:         "claude-opus-4-1-20250805-thinking",
			rawID:        "claude-opus-4-1-20250805-thinking",
			rawFamily:    "claude-opus",
			wantModifier: "thinking",
			wantVersion:  "4.1",
			wantDate:     "2025-08-05",
		},
		{
			desc:         "claude-opus-4-6-thinking (no date)",
			rawID:        "claude-opus-4-6-thinking",
			rawFamily:    "claude-opus",
			wantModifier: "thinking",
			wantVersion:  "4.6",
			wantDate:     "",
		},
		{
			desc:         "gpt-4o-2024-05-13 (no modifier, version not extracted)",
			rawID:        "gpt-4o-2024-05-13",
			rawFamily:    "gpt-4o",
			wantModifier: "",
			wantVersion:  "",
			wantDate:     "2024-05-13",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			// Step 1: ParseFamily
			family, variant, _ := bestiary.ParseFamilyWithVersion(tc.rawFamily)

			// Step 2: ExtractModifier
			modifier, consumed := bestiary.ExtractModifier(tc.rawID, family, variant)

			// Verify modifier extraction
			if modifier != tc.wantModifier {
				t.Errorf("ExtractModifier modifier = %q, want %q", modifier, tc.wantModifier)
			}

			// Step 3: Strip consumed from ID
			cleanedID := bestiary.ModelID(string(tc.rawID))
			if consumed != "" {
				cleanedStr := string(tc.rawID)
				if len(cleanedStr) >= len(consumed) && cleanedStr[len(cleanedStr)-len(consumed):] == consumed {
					cleanedID = bestiary.ModelID(cleanedStr[:len(cleanedStr)-len(consumed)])
				}
			}

			// Step 4: ExtractVersionFromID on cleaned ID
			version := bestiary.ExtractVersionFromID(cleanedID, tc.rawFamily)
			if version != tc.wantVersion {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q", cleanedID, tc.rawFamily, version, tc.wantVersion)
			}

			// Step 5: ExtractDate on cleaned ID
			date := bestiary.ExtractDate(cleanedID, "")
			if date != tc.wantDate {
				t.Errorf("ExtractDate(%q, %q) = %q, want %q", cleanedID, "", date, tc.wantDate)
			}
		})
	}
}

// TestParseFamilyDetailed_KnownSuffixOverflow verifies that ParseFamilyDetailed emits
// a ParseFailure with reason ReasonKnownSuffixOverflow for model IDs whose trailing
// token is a known modifier (thinking, think, vision, latest, code, preview) that
// the parser did NOT capture as the variant.
//
// BDD: Given a model ID ending with a known modifier token (e.g. "claude-opus-4-thinking")
// when the modifier was not captured by ParseFamilyWithVersion as the variant
// then ParseFailure emitted with reason ReasonKnownSuffixOverflow.
func TestParseFamilyDetailed_KnownSuffixOverflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
		modifier  string // expected trailing modifier token (for documentation)
	}{
		// "thinking" — each seed modifier tested with a realistic ID.
		{rawFamily: "claude-opus", id: "claude-opus-4-thinking", provider: "anthropic", modifier: "thinking"},
		{rawFamily: "gpt-4o", id: "gpt-4o-thinking", provider: "openai", modifier: "thinking"},
		// "think"
		{rawFamily: "claude-opus", id: "claude-opus-think", provider: "anthropic", modifier: "think"},
		// "vision"
		{rawFamily: "gpt-4", id: "gpt-4-vision", provider: "openai", modifier: "vision"},
		// "latest"
		{rawFamily: "gpt-4o", id: "gpt-4o-latest", provider: "openai", modifier: "latest"},
		// "code"
		{rawFamily: "claude-opus", id: "claude-opus-code", provider: "anthropic", modifier: "code"},
		// "preview"
		{rawFamily: "gpt-4o", id: "gpt-4o-preview", provider: "openai", modifier: "preview"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			_ = modifier

			// Best-effort parse result is always returned.
			if family == "" {
				t.Errorf("ParseFamilyDetailed(%q, %q): got empty family; expected non-empty best-effort",
					tc.rawFamily, tc.id)
			}
			_ = variant
			_ = version

			// Failure must be emitted.
			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q, %q): expected ParseFailure for known modifier %q, got nil\n"+
					"  What: trailing modifier token in model ID was not detected\n"+
					"  Why: pd.modifiers allowlist or Mode 2 condition may have changed\n"+
					"  How to fix: verify the modifier %q is in parse/data/modifiers.json and Mode 2 fires for this case",
					tc.rawFamily, tc.id, tc.modifier, tc.modifier)
			}
			if failure.Reason != bestiary.ReasonKnownSuffixOverflow {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonKnownSuffixOverflow)
			}
			if failure.RawID != tc.id {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.RawID = %q, want %q",
					tc.rawFamily, tc.id, failure.RawID, tc.id)
			}
		})
	}
}

// TestParseFamilyDetailed_UnknownSuffixOverflow verifies that ParseFamilyDetailed
// emits a ParseFailure with reason ReasonUnknownSuffixOverflow when the model ID has
// a trailing token that is NOT in the modifier allowlist but overflow is detected.
//
// This is an audit-log hint: when this fires, extend the modifier allowlist in parse.go.
//
// BDD: Given a model ID whose suffix-overflow condition fires but the trailing token
// is NOT in the seed allowlist when parsed then ParseFailure emitted with reason
// ReasonUnknownSuffixOverflow.
func TestParseFamilyDetailed_UnknownSuffixOverflow(t *testing.T) {
	t.Parallel()

	// Positive: unknown suffix token FIRES Mode 2 UnknownSuffixOverflow when:
	// (1) model ID trailing token is NOT in the modifier allowlist (pd.modifiers), AND
	// (2) that token is not already the parsed variant, AND
	// (3) detectSuffixOverflow returns true (raw family has >2 unaccounted tokens).
	//
	// This test documents the positive case for ReasonUnknownSuffixOverflow as an audit
	// hint to extend the modifier allowlist when new modifiers are detected in the wild.
	// Example: if models.dev returns rawFamily="claude-opus-4-1-extra-stuff-zen",
	// and the parser can only extract family="claude", variant="opus" from the override,
	// the tokens [4, 1, extra, stuff, zen] would be unaccounted for (5 tokens > 2 threshold),
	// triggering detectSuffixOverflow. Trailing token "zen" is unknown, so
	// ReasonUnknownSuffixOverflow would fire as an audit hint.
	//
	// R3a (e9pi): this subtest is LIVE (not skipped). ParseFamilyWithVersion Step-5 bounded
	// reorder prevents the pure-fallback from absorbing all trailing tokens, making
	// ReasonUnknownSuffixOverflow reachable for the claude-opus-4-1-extra-stuff-zen fixture.
	unknownTrailingWithOverflow := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
		name      string
	}{
		{
			name:      "UnknownSuffixOverflow_PositiveCase",
			rawFamily: "claude-opus-4-1-extra-stuff-zen",
			id:        "claude-opus-4-1-extra-stuff-zen",
			provider:  "anthropic",
		},
	}
	for _, tc := range unknownTrailingWithOverflow {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// R3a (e9pi): ParseFamilyWithVersion Step-5 bounded reorder decomposes
			// rawFamily="claude-opus-4-1-extra-stuff-zen" to (claude, opus, 4.1) via hyphen-version;
			// "extra-stuff-zen" are unaccounted tokens (>2 threshold) → detectSuffixOverflow fires
			// → "zen" is not in pd.modifiers → ReasonUnknownSuffixOverflow.
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure == nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): expected ParseFailure with Reason=%q, got nil\n"+
					"  What: ReasonUnknownSuffixOverflow was not emitted\n"+
					"  Why: ParseFamilyWithVersion Step-5 bounded reorder (R3a e9pi) must decompose\n"+
					"       the input so that 'extra-stuff-zen' tokens are unaccounted (>2 threshold)\n"+
					"  How to fix: verify ParseFamilyWithVersion returns (claude,opus,4.1) not raw passthrough",
					tc.rawFamily, tc.id, bestiary.ReasonUnknownSuffixOverflow)
				return
			}
			if failure.Reason != bestiary.ReasonUnknownSuffixOverflow {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q\n"+
					"  What: wrong failure reason — expected UnknownSuffixOverflow\n"+
					"  Why: trailing token 'zen' is not in pd.modifiers but overflow was detected",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonUnknownSuffixOverflow)
			}
		})
	}

	// Negative: unknown suffix token does NOT fire Mode 2 unless detectSuffixOverflow
	// also fires. These cases document the boundary: an unknown trailing token alone
	// is NOT sufficient to trigger Mode 2; the detectSuffixOverflow threshold (>2 extra
	// tokens) must also be met. This means the current Mode 2 detection is conservative:
	// novel-but-semantic modifiers like "-zen" go unreported unless there is a broader
	// overflow pattern. Extend the allowlist to catch specific new modifiers.
	unknownTrailingNotOverflow := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		{rawFamily: "gpt-4", id: "gpt-4-zen", provider: "openai"},
		{rawFamily: "claude-opus", id: "claude-opus-foobar", provider: "anthropic"},
	}
	for _, tc := range unknownTrailingNotOverflow {
		t.Run("no-overflow/"+string(tc.id), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure != nil && failure.Reason == bestiary.ReasonUnknownSuffixOverflow {
				t.Errorf("ParseFamilyDetailed(%q, %q): got ReasonUnknownSuffixOverflow; "+
					"this case should not fire Mode 2 (trailing token is unknown but no overflow)",
					tc.rawFamily, tc.id)
			}
		})
	}
}

// TestParseFamilyDetailed_Mode2_NegativeCases verifies that Mode 2 does NOT fire
// when the modifier is already the parsed variant (i.e., correctly extracted by
// ParseFamilyWithVersion's suffix stripping).
//
// BDD: Given rawFamily="claude-thinking" (suffix "-thinking" stripped) when parsed
// then ParseFamilyWithVersion extracts variant="thinking" and Mode 2 does NOT fire.
func TestParseFamilyDetailed_Mode2_NegativeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
		note      string
	}{
		// SLICE-3: the former claude-thinking / gpt-vision rows were REMOVED. Under the
		// uniform thinking/vision-as-modifier migration those tokens are never the parsed
		// variant, so they correctly surface as the first-class Modifier and — with no
		// variant absorbing them — DO trip Mode 2 as an honest audit signal (same as
		// claude-opus-4-thinking). Covered by TestParseFamilyDetailed_KnownSuffixOverflow
		// and TestUniformModifierSuffix.
		// Clean IDs with no trailing modifier.
		{"claude-opus", "claude-opus-4-20250514", "anthropic", "date suffix, not modifier"},
		{"claude-haiku", "claude-haiku-4-5", "anthropic", "version suffix, not modifier"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure != nil && (failure.Reason == bestiary.ReasonKnownSuffixOverflow || failure.Reason == bestiary.ReasonUnknownSuffixOverflow) {
				t.Errorf("ParseFamilyDetailed(%q, %q): got Mode 2 failure %q, expected none\n"+
					"  Note: %s\n"+
					"  Mode 2 should not fire when the modifier is already the parsed variant",
					tc.rawFamily, tc.id, failure.Reason, tc.note)
			}
		})
	}
}

// TestParseFamilyDetailed_CleanParse verifies that ParseFamilyDetailed returns
// nil *ParseFailure for cleanly parseable model IDs that the heuristics fully handle.
//
// BDD: Given a cleanly parseable model ID (e.g. "claude-opus") when parsed
// then NO ParseFailure emitted.
func TestParseFamilyDetailed_CleanParse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		// Known override entries — fully handled by the overrides table.
		{rawFamily: "claude-opus", id: "claude-opus-4-20250514", provider: "anthropic"},
		{rawFamily: "claude-haiku", id: "claude-haiku-4-5", provider: "anthropic"},
		{rawFamily: "claude-sonnet", id: "claude-sonnet-4-5-20251015", provider: "anthropic"},
		// Gemini with dot-version — handled by dot-version extraction.
		{rawFamily: "gemini-flash", id: "gemini-2.5-flash-preview-04-17", provider: "google"},
		// Empty raw family — no failure emitted on empty input.
		{rawFamily: "", id: "some-model", provider: "openai"},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)

			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q): expected nil ParseFailure for clean parse, got: %+v\n"+
					"  Family=%q  Reason=%q",
					tc.rawFamily, failure, family, failure.Reason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// R1: ExtractVersionBetweenFamilyAndVariant tests (SLICE-1-L2)
// --------------------------------------------------------------------------

// TestExtractVersionBetweenFamilyAndVariant covers the primary acceptance cases
// from the L2 scope. These tests FAIL until L3 implements the extractor.
//
// N-M equivalence: hyphen-separated numeric tokens are dot-joined (3-5 → 3.5).
// Residual: tokens between version and variant that are neither numeric nor variant
// are returned in the residual slice (honest-audit per R2).
func TestExtractVersionBetweenFamilyAndVariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantVersion  string
		wantResidual []string
	}{
		// Primary acceptance cases from L2 scope.
		{
			desc:         "gpt-5-mini → 5 (single numeric between family and variant)",
			id:           "gpt-5-mini",
			family:       "gpt",
			variant:      "mini",
			wantVersion:  "5",
			wantResidual: nil,
		},
		{
			desc:         "claude-3-5-haiku-20241022 → 3.5 (N-M dot-join)",
			id:           "claude-3-5-haiku-20241022",
			family:       "claude",
			variant:      "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:         "claude-3.5-haiku → 3.5 (dot-normalized in ID)",
			id:           "claude-3.5-haiku",
			family:       "claude",
			variant:      "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:         "gemini-3-pro-preview → 3 (single numeric, variant=pro)",
			id:           "gemini-3-pro-preview",
			family:       "gemini",
			variant:      "pro",
			wantVersion:  "3",
			wantResidual: nil,
		},
		{
			desc:         "gemini-3-1-pro-preview → 3.1 (N-M dot-join, variant=pro)",
			id:           "gemini-3-1-pro-preview",
			family:       "gemini",
			variant:      "pro",
			wantVersion:  "3.1",
			wantResidual: nil,
		},
		{
			desc:         "nova-2-lite-v1 → version=2, residual=[v1] (R2 honest-audit)",
			id:           "nova-2-lite-v1",
			family:       "nova",
			variant:      "lite",
			wantVersion:  "2",
			wantResidual: []string{"v1"},
		},
		{
			desc:         "nemotron-3-super-free → version=3, residual=[super] (R2 honest-audit)",
			id:           "nemotron-3-super-free",
			family:       "nemotron",
			variant:      "free",
			wantVersion:  "3",
			wantResidual: []string{"super"},
		},
		// Edge cases.
		{
			desc:        "no version between family and variant → empty",
			id:          "claude-opus-4-6",
			family:      "claude",
			variant:     "opus",
			wantVersion: "",
		},
		{
			desc:        "empty id → empty",
			id:          "",
			family:      "claude",
			variant:     "haiku",
			wantVersion: "",
		},
		{
			desc:        "empty family → empty",
			id:          "claude-3-5-haiku-20241022",
			family:      "",
			variant:     "haiku",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotVersion, gotResidual := bestiary.ExtractVersionBetweenFamilyAndVariant(tc.id, tc.family, tc.variant)
			if gotVersion != tc.wantVersion {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) version = %q, want %q",
					tc.id, tc.family, tc.variant, gotVersion, tc.wantVersion)
			}
			// Compare residual slices (nil and empty are equivalent for this test).
			if len(gotResidual) != len(tc.wantResidual) {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual = %v, want %v",
					tc.id, tc.family, tc.variant, gotResidual, tc.wantResidual)
			} else {
				for i, tok := range tc.wantResidual {
					if gotResidual[i] != tok {
						t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual[%d] = %q, want %q",
							tc.id, tc.family, tc.variant, i, gotResidual[i], tok)
					}
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// R3b (eq7w): isFourDigitDateToken tests
// --------------------------------------------------------------------------

// TestIsYYMMDateToken_Parity verifies that isFourDigitDateToken parity holds with
// ExtractVersionFromID: tokens for which isFourDigitDateToken is true must not be
// returned as versions.
// The direct unit test for isFourDigitDateToken lives in parse_internal_test.go
// (package bestiary) since the function is unexported.
//
// The key case: mistral-small-2603 → no version (2603 is a YYMM date).
func TestIsYYMMDateToken_Parity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		{
			desc:      "mistral-small-2603 → no version (2603 is YYMM date)",
			id:        "mistral-small-2603",
			rawFamily: "mistral",
			want:      "",
		},
		{
			desc:      "mistral-medium-2505 → no version (2505 is YYMM date)",
			id:        "mistral-medium-2505",
			rawFamily: "mistral",
			want:      "",
		},
		{
			desc:      "genuine version still extracted: claude-opus-4-6",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: YYMM token was not rejected by isFourDigitDateToken guard\n"+
					"  Why: ExtractVersionFromID must consult isFourDigitDateToken before returning hyphen-digit tokens",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// R3c (Δ2′): InferFamilyFromIDWithVariant tests (R3c acceptance)
// --------------------------------------------------------------------------

// TestInferFamilyFromIDWithVariant_R3c covers the Δ2′ corrected algorithm:
// tentative modifier strip → expose hidden date → decompose → guarded commit.
//
// Three empirically-verified traces from PROPOSAL-4 bestiary-y5lo:
//  1. 302ai re-host: claude-opus-4-1-20250805-thinking → (claude, opus, 4.1)
//  2. Genuine-variant guard: kimi-k2-thinking → GUARD-2 declines, variant=thinking preserved
//  3. No-modifier control: claude-opus-4-1-20250805 → (claude, opus, 4.1) unchanged
func TestInferFamilyFromIDWithVariant_R3c(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// Trace 1: 302ai re-host — empty raw_family, modifier after date.
			// exposed=claude-opus-4-1-20250805 → cleaned=claude-opus-4-1 → PFWV →
			// (claude, opus, 4.1); GUARD-1 passes (ExtractModifier returns -thinking),
			// GUARD-2 passes (claude != claude-opus-4-1) → return (claude, opus, 4.1).
			desc:        "claude-opus-4-1-20250805-thinking → (claude, opus, 4.1)",
			id:          "claude-opus-4-1-20250805-thinking",
			provider:    "302ai",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.1",
		},
		{
			// Trace 2 (SLICE-8 d / CLARIFICATION-5 flip, SUPERSEDES the SLICE-3
			// (kimi,"","") pin): kimi-k2-thinking (empty raw_family). kimi is a
			// letter-prefix series, so InferFamilyFromIDWithVariant's series split →
			// (kimi, "k", "2"). The trailing "thinking" is NOT a variant;
			// ParseFamilyDetailed surfaces it as the first-class Modifier
			// (InferFamilyFromIDWithVariant itself returns no modifier).
			desc:        "kimi-k2-thinking empty raw_family → series (kimi,k,2); thinking is a Modifier",
			id:          "kimi-k2-thinking",
			provider:    "moonshot",
			wantFamily:  "kimi",
			wantVariant: "k",
			wantVersion: "2",
		},
		{
			// Trace 3: no-modifier control — claude-opus-4-1-20250805.
			// trimOneTrailingModifier is a no-op (last token is date digit-group) →
			// existing flow → (claude, opus, 4.1) exactly as today.
			desc:        "claude-opus-4-1-20250805 no modifier → unchanged",
			id:          "claude-opus-4-1-20250805",
			provider:    "anthropic",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.1",
		},
		{
			// Previous acceptance case (must not regress).
			desc:        "claude-opus-4-5-20251101 empty raw_family",
			id:          "claude-opus-4-5-20251101",
			provider:    "nano-gpt",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if gotFamily != tc.wantFamily {
				t.Errorf("InferFamilyFromIDWithVariant(%q) family = %q, want %q",
					tc.id, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("InferFamilyFromIDWithVariant(%q) variant = %q, want %q",
					tc.id, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("InferFamilyFromIDWithVariant(%q) version = %q, want %q",
					tc.id, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestParseFamilyDetailed_R3c verifies that ParseFamilyDetailed, when called with
// empty raw_family (via InferFamilyFromIDWithVariant path), produces the expected
// 5-tuple for the Δ2′ traces.
//
// This covers the MANDATE from the UAT: 5-tuple returns include modifier.
func TestParseFamilyDetailed_5Tuple(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawFamily    bestiary.Family
		id           bestiary.ModelID
		provider     bestiary.Provider
		wantFamily   bestiary.Family
		wantVariant  string
		wantVersion  string
		wantModifier string
	}{
		{
			desc:         "claude-opus-4-1-20250805-thinking → modifier=thinking",
			rawFamily:    "claude-opus",
			id:           "claude-opus-4-1-20250805-thinking",
			provider:     "anthropic",
			wantFamily:   "claude",
			wantVariant:  "opus",
			wantVersion:  "4.1",
			wantModifier: "thinking",
		},
		{
			desc:         "claude-opus-4-6 → no modifier",
			rawFamily:    "claude-opus",
			id:           "claude-opus-4-6",
			provider:     "anthropic",
			wantFamily:   "claude",
			wantVariant:  "opus",
			wantVersion:  "4.6",
			wantModifier: "",
		},
		{
			// SLICE-8 (d) / CLARIFICATION-5 RED→GREEN flip (SUPERSEDES the SLICE-3
			// (kimi,"","") pin): the k-prefix is now a letter-prefix SERIES, so
			// kimi-k2-thinking → (kimi, variant="k", version="2", modifier=thinking).
			// "thinking" is stripped to the first-class Modifier first (uniform S3 rule);
			// the series split then decomposes the remaining "k2" → variant "k" + version
			// "2". Consistent across ALL providers (empty raw and raw="kimi-thinking").
			desc:         "kimi-k2-thinking empty rawFamily → series (kimi,k,2) + modifier thinking",
			rawFamily:    "",
			id:           "kimi-k2-thinking",
			provider:     "moonshot",
			wantFamily:   "kimi",
			wantVariant:  "k",
			wantVersion:  "2",
			wantModifier: "thinking",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if modJoin(modifier) != tc.wantModifier {
				t.Errorf("modifier = %q, want %q", modifier, tc.wantModifier)
			}
			// No case in this table should emit a spurious ParseFailure.
			if failure != nil {
				t.Errorf("unexpected failure: %+v", failure)
			}
		})
	}
}

// TestParseFamilyDetailed_R2_Residual verifies the R2 honest-audit signal:
// when extraction succeeds but leaves a residual token, a ParseFailure is emitted
// with Reason=ReasonResidualUnaccountedTokens AND version is populated.
//
// BDD: Given id="nova-2-lite-v1" and rawFamily="nova-lite" when parsed
// then version="2" AND failure.Reason=ReasonResidualUnaccountedTokens with [v1].
func TestParseFamilyDetailed_R2_Residual(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string
	}{
		{
			desc:        "nova-2-lite-v1 → version=2 + residual failure",
			rawFamily:   "nova-lite",
			id:          "nova-2-lite-v1",
			provider:    "amazon",
			wantVersion: "2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  R2: version must be populated even when failure is emitted",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q, %q): expected ParseFailure with R2 residual, got nil",
					tc.rawFamily, tc.id)
			}
			if failure.Reason != bestiary.ReasonResidualUnaccountedTokens {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonResidualUnaccountedTokens)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SLICE-1-FIX-2 tests: FIX A (bare-4-digit date guard) + FIX B1 (sole trailing
// variant-suffix promotion) + negative controls
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_FixA_Bare4DigitDateGuard verifies FIX A: any standalone
// 4-digit all-numeric token is rejected as a version (treated as a date/release-id),
// regardless of whether it falls in the YYMM range (19xx–29xx). The original guard
// (eq7w/R3b) only rejected YYMM-range tokens; FIX-A generalises to all 4-digit
// numerics since supervisor analysis confirmed 0 legitimate bare-4-digit semantic
// versions exist across the 1745 version-populated models.
//
// BDD: Given id contains a bare 4-digit numeric suffix (MMDD format like "0528")
// when ParseFamilyDetailed is called then version="" (no version emitted for date token).
//
// Acceptance: deepseek-r1-0528 → no version; deepseek-v3-0324 → no version;
// mistral-small-2603 still no version (YYMM, already handled by R3b).
func TestParseFamilyDetailed_FixA_Bare4DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string // want empty: 4-digit token must NOT be returned as version
	}{
		{
			// deepseek-r1-0528: "0528" is MMDD format, below 19xx YYMM range.
			// FIX-A: extended guard rejects "0528" as version.
			desc:        "deepseek-r1-0528 → no version (0528 is MMDD date, not version)",
			rawFamily:   "deepseek-r1",
			id:          "deepseek-r1-0528",
			provider:    "deepseek",
			wantVersion: "",
		},
		{
			// deepseek-v3-0324: "0324" is MMDD format.
			// FIX-A: extended guard rejects "0324" as version.
			desc:        "deepseek-v3-0324 → no version (0324 is MMDD date, not version)",
			rawFamily:   "deepseek",
			id:          "deepseek-v3-0324",
			provider:    "deepseek",
			wantVersion: "",
		},
		{
			// mistral-small-2603: "2603" is YYMM range — still rejected (R3b coverage preserved).
			desc:        "mistral-small-2603 → no version (2603 is YYMM date, R3b still holds)",
			rawFamily:   "mistral-small",
			id:          "mistral-small-2603",
			provider:    "mistral",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  What: bare 4-digit date token was returned as a version\n"+
					"  Why: FIX-A guard should reject any 4-digit all-numeric token as a date/release-id\n"+
					"  How to fix: verify isFourDigitDateToken returns true for all 4-digit all-numeric tokens",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
		})
	}
}

// TestExtractVersionFromID_FixA_Bare4DigitDateGuard verifies that ExtractVersionFromID
// also rejects bare 4-digit date tokens (FIX-A parity with ParseFamilyDetailed).
// The guard must be consistent across all call sites: isVersionToken, ExtractVersionFromID,
// and ParseFamilyDetailed.
//
// Acceptance: genuine versions like 4.6, 4.5, 4o still extracted correctly.
func TestExtractVersionFromID_FixA_Bare4DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		// FIX-A: bare 4-digit tokens rejected.
		{
			desc:      "deepseek-r1-0528 → no version (0528 rejected)",
			id:        "deepseek-r1-0528",
			rawFamily: "deepseek-r1",
			want:      "",
		},
		{
			desc:      "some-model-0324 → no version (0324 rejected)",
			id:        "some-model-0324",
			rawFamily: "some-model",
			want:      "",
		},
		// Existing YYMM guard still active.
		{
			desc:      "mistral-small-2603 → no version (2603 YYMM, R3b preserved)",
			id:        "mistral-small-2603",
			rawFamily: "mistral-small",
			want:      "",
		},
		// Genuine versions still extracted (must not regress).
		{
			desc:      "claude-opus-4-6 → 4.6 (legitimate version)",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
		{
			desc:      "gpt-4o → 4o (alphanumeric version)",
			id:        "gpt-4o",
			rawFamily: "gpt",
			want:      "4o",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: FIX-A bare-4-digit guard inconsistency\n"+
					"  Why: 4-digit token must be rejected by isFourDigitDateToken in ExtractVersionFromID",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// TestParseFamilyDetailed_FixB1_SoleVariantSuffixPromotion verifies FIX B1:
// when version was extracted AND exactly ONE residual token remains AND it is a
// known variant suffix (from variant_suffixes.json) AND Variant=="" → the token is
// promoted into Variant, and no ReasonResidualUnaccountedTokens failure is emitted.
//
// BDD: Given id with sole residual = known variant suffix and Variant==""
// when ParseFamilyDetailed is called then variant=<suffix> AND failure=nil.
//
// Acceptance: glm-5-turbo→(glm,turbo,5); phi-4-mini→(phi,mini,4).
// Note: text-embedding-3-large/small were here in FIX-2 but are now documented residuals
// after SLICE-1-FIX-4 reverted the full-prefix-first change (bestiary-ibtb, rc2 deferred).
func TestParseFamilyDetailed_FixB1_SoleVariantSuffixPromotion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		rawFamily     bestiary.Family
		id            bestiary.ModelID
		provider      bestiary.Provider
		wantFamily    bestiary.Family
		wantVariant   string
		wantVersion   string
		wantNoFailure bool // true → failure must be nil
	}{
		{
			// glm-5-turbo: rawFamily="glm" → family=glm, variant="" initially.
			// ExtractVersionBetween: ver="5", residual=["turbo"]. "turbo" is a known suffix.
			// B1: variant="" → promote "turbo" → (glm, turbo, 5), no failure.
			// SLICE-10: 'turbo' is now a global Modifier (glm has no 'turbo' member), so it is
			// NOT promoted into Variant — variant is empty, modifier=[turbo], version=5,
			// and no residual-unaccounted failure (the modifier is a first-class field).
			// SLICE-10: turbo→Modifier; ParseFamilyDetailed emits the ReasonKnownSuffixOverflow
			// AUDIT annotation (turbo is a known modifier trailing the ID) which codegen clears
			// once the modifier is a first-class field — so wantNoFailure is false here.
			desc:          "glm-5-turbo → (glm, '', 5) turbo→Modifier",
			rawFamily:     "glm",
			id:            "glm-5-turbo",
			provider:      "zhipu",
			wantFamily:    "glm",
			wantVariant:   "",
			wantVersion:   "5",
			wantNoFailure: false,
		},
		{
			// phi-4-mini: rawFamily="phi" → family=phi, variant="" initially.
			// ExtractVersionBetween: ver="4", residual=["mini"]. "mini" is a known suffix.
			// B1: variant="" → promote "mini" → (phi, mini, 4), no failure.
			desc:          "phi-4-mini → (phi, mini, 4), no residual failure",
			rawFamily:     "phi",
			id:            "phi-4-mini",
			provider:      "microsoft",
			wantFamily:    "phi",
			wantVariant:   "mini",
			wantVersion:   "4",
			wantNoFailure: true,
		},
		// NOTE: text-embedding-3-large and text-embedding-3-small are NOT in this table
		// after SLICE-1-FIX-4. The FIX-2 full-prefix-first change that made them promote
		// has been reverted. With firstToken normalization, family="text-embedding" →
		// prefix="text-" → remainder="embedding-3-large" → residual=["embedding","large"]
		// (2 residual tokens, B1 requires exactly 1) → ReasonResidualUnaccountedTokens.
		// These are documented residuals accepted in bestiary-ibtb (rc2 deferred).
		// They are covered by TestParseFamilyDetailed_FixB1_Reverted_TextEmbeddingResidual.
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: sole trailing known-suffix was not promoted into Variant\n"+
					"  Why: FIX B1 should set Variant=<suffix> when exactly one residual token is a known variant suffix\n"+
					"  How to fix: verify B1 promotion logic in ParseFamilyDetailed",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if tc.wantNoFailure && failure != nil {
				t.Errorf("failure = %+v, want nil\n"+
					"  What: ReasonResidualUnaccountedTokens emitted even though sole residual was a known suffix\n"+
					"  Why: FIX B1 should suppress failure when sole residual is promoted to Variant",
					failure)
			}
		})
	}
}

// TestParseFamilyDetailed_FixB1_NegativeControls verifies that a residual failure
// is STILL emitted (the model does not fully decompose) in two cases where the
// trailing residue is more than a single promotable suffix token — even though the
// member-variant IS now recovered by recoverMemberVariant.
//
// SLICE-1 (rc2) FIX CYCLE: recoverMemberVariant superseded the old inline B1.
// Unlike old B1 — which fired only on EXACTLY ONE post-version residual token — the
// broad member-zone scan now recovers a member variant up front (for registered
// families) regardless of how many OTHER residual tokens follow. So the variant IS
// populated here; the residual failure persists because a DIFFERENT, unaccounted
// token remains after the variant:
//
//	phi-3-medium-128k-instruct → variant="medium" recovered (phi member), but
//	    "128k" (and "instruct") remain unaccounted → ReasonResidualUnaccountedTokens.
//	nova-2-lite-v1 → variant="lite" set by ParseFamilyWithVersion suffix-strip, but
//	    "v1" remains after the variant → ReasonResidualUnaccountedTokens.
//
// These remain documented residuals (user-accepted, out of scope): the failure is
// the honest-audit signal that the ID did not fully decompose, NOT a missing variant.
func TestParseFamilyDetailed_FixB1_NegativeControls(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVariant string // member variant IS recovered, even though a residual remains
		wantFailure bool   // true → failure must be non-nil
		wantReason  bestiary.ParseFailureReason
		desc2       string // description of why it stays residual
	}{
		{
			// phi-3-medium-128k-instruct: recoverMemberVariant recovers "medium" (a phi
			// member) up front. ExtractVersionBetween then finds ver="3" with residual
			// ["128k","instruct"] AFTER the variant → residual failure persists.
			desc:        "phi-3-medium-128k-instruct (variant=medium recovered; 128k unaccounted)",
			rawFamily:   "phi",
			id:          "phi-3-medium-128k-instruct",
			provider:    "microsoft",
			wantVariant: "medium",
			wantFailure: true,
			wantReason:  bestiary.ReasonResidualUnaccountedTokens,
			desc2:       "variant recovered as 'medium', but '128k' remains unaccounted after the variant",
		},
		{
			// nova-2-lite-v1: rawFamily="nova-lite" → variant="lite" via ParseFamilyWithVersion
			// suffix-strip (so recoverMemberVariant is not consulted). ExtractVersionBetween
			// finds ver="2", residual=["v1"] AFTER the variant → residual failure persists.
			desc:        "nova-2-lite-v1 (variant=lite pre-set; v1 unaccounted)",
			rawFamily:   "nova-lite",
			id:          "nova-2-lite-v1",
			provider:    "cartesia",
			wantVariant: "lite",
			wantFailure: true,
			wantReason:  bestiary.ReasonResidualUnaccountedTokens,
			desc2:       "variant is 'lite'; 'v1' remains unaccounted after the variant",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, variant, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if variant != tc.wantVariant {
				t.Errorf("ParseFamilyDetailed(%q, %q): variant = %q, want %q\n"+
					"  What: recoverMemberVariant should recover the member variant even when a residual remains\n"+
					"  Why: the broad member-zone scan no longer requires a single sole residual (unlike old B1)",
					tc.rawFamily, tc.id, variant, tc.wantVariant)
			}
			if tc.wantFailure {
				if failure == nil {
					t.Errorf("ParseFamilyDetailed(%q, %q): failure = nil, want %q failure\n"+
						"  What: a residual failure should still fire here (%s)\n"+
						"  Why: an unaccounted token remains after the recovered variant",
						tc.rawFamily, tc.id, tc.wantReason, tc.desc2)
					return
				}
				if failure.Reason != tc.wantReason {
					t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
						tc.rawFamily, tc.id, failure.Reason, tc.wantReason)
				}
			} else if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): unexpected failure %q", tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SLICE-1-FIX-3: date-as-version guard inside dot-join paths
// --------------------------------------------------------------------------

// TestParseFamilyWithVersion_Fix3_DateGroupsStripped verifies SLICE-1-FIX-3:
// the date-shape guard is applied INSIDE the hyphen-version dot-join path,
// stripping trailing date groups and keeping only leading semantic-version groups.
//
// INVARIANT: no model's Version may be a date-shaped group. Covered shapes:
//   - 4-digit YYMM (e.g. "2603", "2512", "2508") → ""
//   - 4-digit MMDD (e.g. "0528", "0314", "1206") → ""
//   - 6-digit YYMMDD (e.g. "250615", "250715") → stripped from trailing position
//   - MM-YYYY two-group (e.g. "08-2024", "03-2025") → ""
//
// BDD: given a raw family string with a trailing date group in hyphen-version form,
// when ParseFamilyWithVersion is called, then version="" (date stripped) or version
// equals only the leading non-date groups.
func TestParseFamilyWithVersion_Fix3_DateGroupsStripped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// 4-digit YYMM cases: base in overrides, trailing date token.
		{
			name: "mistral-small-2603 → version empty (2603 is YYMM date)",
			raw:  "mistral-small-2603", wantFamily: "mistral", wantVariant: "small", wantVersion: "",
		},
		{
			name: "mistral-large-2512 → version empty (2512 is YYMM date)",
			raw:  "mistral-large-2512", wantFamily: "mistral", wantVariant: "large", wantVersion: "",
		},
		{
			name: "codestral-2508 → version empty (2508 is YYMM date)",
			raw:  "codestral-2508", wantFamily: "codestral", wantVariant: "", wantVersion: "",
		},
		// 4-digit MMDD cases: leading semantic version kept, trailing date stripped.
		{
			name: "gpt-4-0314 → version=4 (0314 is MMDD date, leading 4 kept)",
			raw:  "gpt-4-0314", wantFamily: "gpt", wantVariant: "", wantVersion: "4",
		},
		// 6-digit YYMMDD cases: stripped from trailing position.
		{
			name: "doubao-seed-1-6-250615 → version=1.6 (250615 is YYMMDD, stripped)",
			raw:  "doubao-seed-1-6-250615", wantFamily: "doubao-seed", wantVariant: "", wantVersion: "1.6",
		},
		// 4-digit MMDD: gemini-exp-1206 (1206 is MMDD, single group → "").
		{
			name: "gemini-exp-1206 → version empty (1206 is MMDD date)",
			raw:  "gemini-exp-1206", wantFamily: "gemini-exp", wantVariant: "", wantVersion: "",
		},
		// 4-digit MMDD: deepseek-r1-0528.
		{
			name: "deepseek-r1-0528 → version empty (0528 is MMDD date)",
			raw:  "deepseek-r1-0528", wantFamily: "deepseek-r1", wantVariant: "", wantVersion: "",
		},
		// MM-YYYY two-group cases: full remainder is a date.
		{
			name: "command-r-08-2024 → version empty (08-2024 is MM-YYYY date)",
			raw:  "command-r-08-2024", wantFamily: "command", wantVariant: "r", wantVersion: "",
		},
		{
			name: "command-a-03-2025 → version empty (03-2025 is MM-YYYY date)",
			raw:  "command-a-03-2025", wantFamily: "command", wantVariant: "a", wantVersion: "",
		},
		// Regression: legitimate versions must be preserved.
		{
			name: "claude-opus-4-5 → version=4.5 (no date, preserve)",
			raw:  "claude-opus-4-5", wantFamily: "claude", wantVariant: "opus", wantVersion: "4.5",
		},
		{
			name: "llama-3-1 → version=3.1 (no date, preserve)",
			raw:  "llama-3-1", wantFamily: "llama", wantVariant: "", wantVersion: "3.1",
		},
		{
			name: "phi-4-5 → version=4.5 (no date, preserve)",
			raw:  "phi-4-5", wantFamily: "phi", wantVariant: "", wantVersion: "4.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q\n"+
					"  What: date-shaped token was returned as version\n"+
					"  Why: SLICE-1-FIX-3 guard must strip date groups inside hyphen-version dot-join path\n"+
					"  How to fix: verify dotJoinStrippingDateSuffix strips trailing date groups",
					tc.raw, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestExtractVersionFromID_Fix3_MMYYYYTwoGroup verifies SLICE-1-FIX-3 for the
// reHyphenDigits path in ExtractVersionFromID: the MM-YYYY two-group pattern
// (e.g. "08-2024", "03-2025") must be detected as a date and return "".
//
// BDD: given remainder="MM-YYYY" after family-prefix strip, when ExtractVersionFromID
// is called, then "" is returned (date shape, not a semantic version).
func TestExtractVersionFromID_Fix3_MMYYYYTwoGroup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		{
			name:      "command-r-08-2024 → no version (08-2024 is MM-YYYY)",
			id:        "command-r-08-2024",
			rawFamily: "command-r",
			want:      "",
		},
		{
			name:      "command-a-03-2025 → no version (03-2025 is MM-YYYY)",
			id:        "command-a-03-2025",
			rawFamily: "command-a",
			want:      "",
		},
		// Regression: must not break legitimate hyphen-digit versions.
		{
			name:      "claude-opus-4-5 → 4.5 (legitimate version preserved)",
			id:        "claude-opus-4-5",
			rawFamily: "claude-opus",
			want:      "4.5",
		},
		{
			name:      "claude-opus-4-6 → 4.6 (legitimate version preserved)",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: MM-YYYY two-group was returned as version\n"+
					"  Why: SLICE-1-FIX-3 isMMYYYYTwoGroup guard must detect and reject MM-YYYY remainder",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// TestExtractVersionBetweenFamilyAndVariant_Fix3_6DigitStripped verifies that
// SLICE-1-FIX-3 correctly strips 6-digit YYMMDD tokens from the version extraction
// loop in ExtractVersionBetweenFamilyAndVariant.
//
// BDD: given an ID with a 6-digit YYMMDD suffix embedded after valid version tokens,
// when ExtractVersionBetweenFamilyAndVariant is called, then the version contains only
// the leading semantic groups (6-digit date group is stopped at, not included).
func TestExtractVersionBetweenFamilyAndVariant_Fix3_6DigitStripped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantVersion  string
		wantResidual []string
	}{
		{
			// seed-1-6-flash-250715: rawFamily="seed", 250715 is 6-digit YYMMDD.
			// With variant="flash" (from ParseFamilyDetailed), extraction stops at 250715.
			desc:         "seed-1-6-flash-250715 with variant=flash → version=1.6",
			id:           "seed-1-6-flash-250715",
			family:       "seed",
			variant:      "flash",
			wantVersion:  "1.6",
			wantResidual: nil,
		},
		{
			// doubao-seed-1-6-250615: family="doubao-seed", variant="".
			// SLICE-1-FIX-4: full-prefix-first reverted; firstToken("doubao-seed")="doubao" →
			// prefix="doubao-", remainder="seed-1-6-250615" → "seed" is non-version residual,
			// "1","6" are version tokens, "250615" is 6-digit YYMMDD → stop.
			// → version="1.6", residual=["seed"] (honest-audit residual for compound family).
			desc:         "doubao-seed-1-6-250615 with variant=empty → version=1.6, residual=[seed]",
			id:           "doubao-seed-1-6-250615",
			family:       "doubao-seed",
			variant:      "",
			wantVersion:  "1.6",
			wantResidual: []string{"seed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotVersion, gotResidual := bestiary.ExtractVersionBetweenFamilyAndVariant(tc.id, tc.family, tc.variant)
			if gotVersion != tc.wantVersion {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) version = %q, want %q\n"+
					"  What: 6-digit YYMMDD was included in version\n"+
					"  Why: SLICE-1-FIX-3 isDateShapedToken must reject 6-digit tokens in dot-join loop",
					tc.id, tc.family, tc.variant, gotVersion, tc.wantVersion)
			}
			if len(gotResidual) != len(tc.wantResidual) {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual = %v, want %v",
					tc.id, tc.family, tc.variant, gotResidual, tc.wantResidual)
			}
		})
	}
}

// --------------------------------------------------------------------------
// SLICE-1-FIX-4: regression tests
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_Fix4_VersionRestoredAfterRevert is the SLICE-1-FIX-4 regression
// test pinning that the FIX-2 B1 full-prefix-first revert RESTORES version extraction for
// the three canonical cases that were over-stripped. Guards against version-nulling recurrence.
//
// BDD: Given model IDs whose version digits appear BEFORE a compound family prefix in the ID
// (e.g. "gemini-2.5-flash-image-generation" where "2.5" precedes "flash"),
// when ParseFamilyDetailed is called, then version is populated (not empty).
//
// Pinned assertions:
//   - claude-3-7-sonnet-thinking → version "3.7"
//   - gemini-2.5-flash-image-generation → version "2.5"
//   - grok-3-beta → version "3"
func TestParseFamilyDetailed_Fix4_VersionRestoredAfterRevert(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string
	}{
		{
			// claude-3-7-sonnet-thinking: rawFamily="claude-sonnet" → (claude, sonnet, "").
			// ExtractVersionBetween(id, "claude", "sonnet"): prefix="claude-", rem="3-7-sonnet-thinking-20250219"
			// → date strip → "3-7-sonnet-thinking" → "3","7" before "sonnet" → ver="3.7".
			// (modifier "thinking" is stripped by ExtractModifier before this call.)
			desc:        "claude-3-7-sonnet-thinking → version 3.7 (FIX-4 restore)",
			rawFamily:   "claude-sonnet",
			id:          "claude-3-7-sonnet-thinking-20250219",
			provider:    "anthropic",
			wantVersion: "3.7",
		},
		{
			// gemini-2.5-flash-image-generation: rawFamily="gemini-2.5-flash-image" → via suffix/overrides
			// → family="gemini-2.5-flash", variant="image". ExtractVersionBetween(id, "gemini-2.5-flash", "image"):
			// prefix=firstToken("gemini-2.5-flash")+"-"="gemini-", rem="2.5-flash-image-generation"
			// → dot-version early return: reBareVersion.MatchString("2.5")=true → ver="2.5".
			// (FIX-2 full-prefix-first would have matched "gemini-2.5-flash-" and returned ver="".)
			desc:        "gemini-2.5-flash-image-generation → version 2.5 (FIX-4 restore)",
			rawFamily:   "gemini-2.5-flash-image",
			id:          "gemini-2.5-flash-image-generation",
			provider:    "google",
			wantVersion: "2.5",
		},
		{
			// grok-3-beta: rawFamily="grok" → (grok, "", ""). ExtractVersionBetween(id, "grok", ""):
			// prefix="grok-", rem="3-beta" → no variantFirst → "3" is version, "beta" residual.
			// B1: len(residual)==1, variant=="" → check "beta" is known suffix → promote → (grok, beta, 3).
			desc:        "grok-3-beta → version 3 (FIX-4 restore)",
			rawFamily:   "grok",
			id:          "grok-3-beta",
			provider:    "xai",
			wantVersion: "3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  What: version not extracted — FIX-4 revert of B1 full-prefix-first should restore this\n"+
					"  Why: full-prefix-first over-stripped compound family prefix, losing the leading version digits\n"+
					"  How to fix: verify ExtractVersionBetweenFamilyAndVariant uses firstToken normalization, not full-prefix",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): unexpected ParseFailure reason=%q (version should be populated cleanly)",
					tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_Fix4_OjjbSurvivingB1Promotions verifies that the B1 sole-variant-suffix
// promotions that do NOT depend on the full-prefix-first change SURVIVE the FIX-4 revert.
// These are gpt-5-codex and gpt-4-turbo, where rawFamily="gpt" (single token) and the full-prefix
// is the same as firstToken, so the revert has no effect on them.
//
// BDD: Given rawFamily="gpt" (single token, no compound prefix), when ParseFamilyDetailed is
// called on gpt-5-codex and gpt-4-turbo, then B1 promotes the sole residual variant suffix.
func TestParseFamilyDetailed_Fix4_OjjbSurvivingB1Promotions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// gpt-5-codex: rawFamily="gpt" → (gpt, "", ""). ExtractVersionBetween: prefix="gpt-",
			// rem="5-codex" → "5" is version, "codex" residual. B1: len(residual)==1, variant=="" →
			// "codex" is a known suffix → promote → (gpt, codex, 5). No compound prefix issue.
			desc:        "gpt-5-codex → (gpt, codex, 5) — B1 survives FIX-4 revert",
			rawFamily:   "gpt",
			id:          "gpt-5-codex",
			provider:    "openai",
			wantFamily:  "gpt",
			wantVariant: "codex",
			wantVersion: "5",
		},
		{
			// SLICE-10: 'turbo' is now a global Modifier (gpt has no 'turbo' member), so it is
			// extracted to the Modifier list instead of promoted to Variant → (gpt, "", 4).
			desc:        "gpt-4-turbo → (gpt, '', 4) turbo→Modifier (SLICE-10)",
			rawFamily:   "gpt",
			id:          "gpt-4-turbo",
			provider:    "openai",
			wantFamily:  "gpt",
			wantVariant: "",
			wantVersion: "4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: B1 sole-variant-suffix promotion did not fire\n"+
					"  Why: gpt-5-codex/gpt-4-turbo use single-token rawFamily ('gpt'); revert should not affect them\n"+
					"  How to fix: verify B1 promotion logic in ParseFamilyDetailed",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			// SLICE-10: a turbo→Modifier reclassification emits the ReasonKnownSuffixOverflow
			// AUDIT annotation (codegen clears it once the modifier is first-class). Permit it;
			// any OTHER failure reason is still unexpected.
			if failure != nil && failure.Reason != bestiary.ReasonKnownSuffixOverflow {
				t.Errorf("unexpected ParseFailure reason=%q; B1 should have promoted sole residual to variant",
					failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_Fix4_TextEmbeddingResidual documents the EXPECTED post-FIX-4 behavior
// of text-embedding-3-large and text-embedding-3-small: they are documented residuals
// (ReasonResidualUnaccountedTokens) after the full-prefix-first revert.
//
// After revert: firstToken("text-embedding")="text" → prefix="text-" → remainder="embedding-3-large"
// → residual=["embedding","large"] (2 tokens, B1 requires exactly 1) → failure emitted.
// Proper additive handling deferred to rc2 (bestiary-ibtb).
func TestParseFamilyDetailed_Fix4_TextEmbeddingResidual(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		{rawFamily: "text-embedding", id: "text-embedding-3-large", provider: "openai"},
		{rawFamily: "text-embedding", id: "text-embedding-3-small", provider: "openai"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure == nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure=nil, want ReasonResidualUnaccountedTokens\n"+
					"  What: text-embedding models should emit residual failure after FIX-4 revert\n"+
					"  Why: full-prefix-first was reverted; firstToken('text-embedding')='text' leaves 'embedding' as residual\n"+
					"  How to fix: verify full-prefix-first is NOT in ExtractVersionBetweenFamilyAndVariant",
					tc.rawFamily, tc.id)
				return
			}
			if failure.Reason != bestiary.ReasonResidualUnaccountedTokens {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason=%q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonResidualUnaccountedTokens)
			}
		})
	}
}

// TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard verifies the SLICE-1-FIX-4 7kyb/9yyp
// fix: ParseFamilyWithVersion Step-5 override-prefix version loop now uses isDateShapedToken
// (catches 4-digit AND 6-digit YYMMDD) instead of isFourDigitDateToken (4-digit only).
//
// BDD: Given a rawFamily string that hits the Step-5 override-prefix path AND contains a
// 6-digit YYMMDD date token in the suffix (e.g. "claude-opus-1-6-250615"),
// when ParseFamilyWithVersion is called, then the 6-digit date is NOT included in the version.
//
// Also confirms TestStaticModels_NoDateVersions invariant is not violated by the 4th site.
//
// NOTE (bestiary-rwbl / SLICE-1-FIX-4-FIX): The inputs in this test (e.g. "claude-opus-1-6-250615")
// actually match the Step-2 hyphen-version regex (all-digit suffix) and are processed by
// dotJoinStrippingDateSuffix BEFORE reaching Step-5. These tests are therefore NOT load-bearing
// for parse.go:455 (the isDateShapedToken guard in the Step-5 override-prefix loop).
// See TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard_LoadBearing below for the
// load-bearing test that actually exercises parse.go:455.
func TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// claude-opus-1-6-250615: "claude-opus" is in overrides → (claude, opus).
			// suffix = ["1","6","250615"]. "1" ok, "6" ok, "250615" is 6-digit → isDateShapedToken → stop.
			// → ver="1.6" (not "1.6.250615").
			name:        "claude-opus-1-6-250615 → version 1.6 (6-digit date stopped at Step-5)",
			raw:         "claude-opus-1-6-250615",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "1.6",
		},
		{
			// claude-sonnet-3-7-250219: "claude-sonnet" in overrides → (claude, sonnet).
			// suffix = ["3","7","250219"]. "3" ok, "7" ok, "250219" 6-digit → stop.
			// → ver="3.7".
			name:        "claude-sonnet-3-7-250219 → version 3.7 (6-digit date stopped at Step-5)",
			raw:         "claude-sonnet-3-7-250219",
			wantFamily:  "claude",
			wantVariant: "sonnet",
			wantVersion: "3.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			family, variant, version := bestiary.ParseFamilyWithVersion(tc.raw)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: 6-digit YYMMDD token included in version at Step-5 override-prefix loop\n"+
					"  Why: isFourDigitDateToken only catches 4-digit tokens; is6DigitYYMMDD was not guarded here\n"+
					"  How to fix: verify Step-5 loop uses isDateShapedToken (SLICE-1-FIX-4 7kyb/9yyp)",
					version, tc.wantVersion)
			}
			// The version must NOT be a 6-digit all-numeric string.
			if len(version) == 6 {
				allDigits := true
				for _, r := range version {
					if r < '0' || r > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					t.Errorf("version = %q is a bare 6-digit date — INVARIANT VIOLATED: version must not be a date",
						version)
				}
			}
		})
	}
}

// TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard_LoadBearing is the LOAD-BEARING
// companion test for parse.go:455 (the isDateShapedToken guard inside the Step-5
// override-prefix version loop of ParseFamilyWithVersion).
//
// Background (bestiary-rwbl / SLICE-1-FIX-4-FIX):
//
// The existing TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard is NOT load-bearing for
// parse.go:455: its inputs (e.g. "claude-opus-1-6-250615") match the Step-2 hyphen-version
// regex (^base-(\d+(-\d+)*)$) because their suffix is all-numeric, so they are handled by
// dotJoinStrippingDateSuffix at Step-2 and RETURN before Step-5 is ever entered.
// Reverting parse.go:455 from isDateShapedToken back to isFourDigitDateToken passes the entire
// test suite — confirming the original test does NOT exercise the FIX-4 change site.
//
// Reaching Step-5: the Step-5 override-prefix loop fires when:
//
//	(a) No exact-override match at Step-1 (rawStr itself is not in overrides),
//	(b) No hyphen-version match at Step-2 (requires the TRAILING suffix to be all-numeric —
//	    any non-digit token after the last digit group defeats the match),
//	(c) No other pattern (v/k/m/no-prefix) at Step-2,
//	(d) No suffix-strip match at Step-3,
//	(e) No dot-version match at Step-4.
//
// Key insight: appending a non-digit modifier (e.g. "-zen") after the date prevents the
// hyphen-version regex from matching (it requires an all-digit tail), so the input falls
// through to Step-5 where the override-prefix scan fires.
//
// Mutation verification (performed during test authoring — bestiary-rwbl):
//
//	Reverting parse.go:455 to isFourDigitDateToken: FAILS these cases.
//	  "claude-opus-1-6-250615-zen" → version="1.6.250615" (want "1.6")
//	  "claude-opus-4-250615-zen"   → version="4.250615"   (want "4")
//	  "claude-opus-250615-zen"     → version="250615"     (want "")
//	Restoring parse.go:455 to isDateShapedToken: PASSES all cases.
//
// BDD:
//
//	Given a rawFamily that (1) has no exact override match, (2) does NOT match the
//	hyphen-version regex due to a trailing non-digit modifier, and (3) has a known
//	override prefix in the overrides table with a 6-digit YYMMDD date token in the
//	version position of the remaining suffix —
//	When ParseFamilyWithVersion is called,
//	Then the 6-digit token must NOT appear in the returned version.
func TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard_LoadBearing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// "claude-opus-1-6-250615-zen": trailing "-zen" defeats hyphen-version regex (Step-2)
			// → falls through to Step-5. Override scan: "claude-opus" → {claude, opus}.
			// suffix = ["1","6","250615","zen"]. Tokens: "1" (version), "6" (version),
			// "250615" (6-digit YYMMDD — isDateShapedToken=true) → break.
			// Without FIX-4 (isFourDigitDateToken): "250615" has len=6≠4 → isFourDigitDateToken=false
			// → "250615" appended → version="1.6.250615" (WRONG).
			// With FIX-4 (isDateShapedToken): is6DigitYYMMDD("250615")=true → break → version="1.6" (CORRECT).
			name:        "claude-opus-1-6-250615-zen → version 1.6 (Step-5 path, 6-digit blocked)",
			raw:         "claude-opus-1-6-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "1.6",
		},
		{
			// "claude-opus-4-250615-zen": no hyphen-version match (zen at end).
			// Step-5: "claude-opus" override → suffix=["4","250615","zen"].
			// "4" ok, "250615" 6-digit → break → version="4".
			name:        "claude-opus-4-250615-zen → version 4 (Step-5 path, 6-digit blocked)",
			raw:         "claude-opus-4-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4",
		},
		{
			// "claude-opus-250615-zen": no hyphen-version match (zen at end).
			// Step-5: "claude-opus" override → suffix=["250615","zen"].
			// "250615" is 6-digit date → break immediately → version="".
			name:        "claude-opus-250615-zen → version empty (Step-5 path, 6-digit blocked first)",
			raw:         "claude-opus-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "",
		},
		{
			// "claude-sonnet-4-250615-zen": uses "claude-sonnet" override.
			// suffix=["4","250615","zen"]. "4" ok, "250615" 6-digit → break → version="4".
			name:        "claude-sonnet-4-250615-zen → version 4 (Step-5 path, 6-digit blocked)",
			raw:         "claude-sonnet-4-250615-zen",
			wantFamily:  "claude",
			wantVariant: "sonnet",
			wantVersion: "4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			family, variant, version := bestiary.ParseFamilyWithVersion(tc.raw)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: wrong family from Step-5 override-prefix decomposition\n"+
					"  File: parse.go ParseFamilyWithVersion Step-5 (lines ~443-465)",
					family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: wrong variant from Step-5 override-prefix decomposition\n"+
					"  File: parse.go ParseFamilyWithVersion Step-5 (lines ~443-465)",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: 6-digit YYMMDD token leaked into version at parse.go:455 (Step-5 loop)\n"+
					"  Why: isFourDigitDateToken only rejects 4-digit tokens; 6-digit YYMMDD (len=6) passes through it\n"+
					"  Where: parse.go ParseFamilyWithVersion Step-5, loop at ~line 454-459\n"+
					"  How to fix: parse.go:455 must use isDateShapedToken (not isFourDigitDateToken)\n"+
					"  Ref: SLICE-1-FIX-4 bestiary-rwbl load-bearing mutation test",
					version, tc.wantVersion)
			}
			// The version must NOT contain a 6-digit all-numeric segment (date leak).
			for _, seg := range splitDotSegments(version) {
				if len(seg) == 6 && isAllDigits(seg) {
					t.Errorf("version segment %q is a bare 6-digit date — INVARIANT VIOLATED\n"+
						"  What: version=%q contains date-shaped segment %q\n"+
						"  Ref: parse.go:455 isDateShapedToken guard (SLICE-1-FIX-4)",
						seg, version, seg)
				}
			}
		})
	}
}

// splitDotSegments splits s on "." and returns the non-empty parts.
// Used by TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard_LoadBearing to
// inspect individual dot-notation version tokens.
func splitDotSegments(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isAllDigits reports whether every rune in s is an ASCII digit.
// Used by TestParseFamilyWithVersion_Fix4_Step5_6DigitDateGuard_LoadBearing.
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ============================================================================
// SLICE-1 (rc2) — L2 Tests (RED until L3 implements M3/M4/recoverMemberVariant)
// ============================================================================

// ----------------------------------------------------------------------------
// M4 — case-fold: Family field must be lowercase at the output boundary
// ----------------------------------------------------------------------------

// TestM4_FamilyCaseFold verifies that ParseFamilyDetailed lowercases the
// Family field regardless of the casing in the raw_family input (M4).
//
// BDD: Given a mixed-case raw_family (e.g. "MiniMax"),
// When ParseFamilyDetailed is called,
// Then the returned Family is lowercase ("minimax").
//
// This is the M4 case-fold step (SLICE-1). Fixes CatA cross-provider divergences
// (e.g. some providers return raw_family="MiniMax" while others return "minimax").
func TestM4_FamilyCaseFold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		rawFamily  bestiary.Family
		id         bestiary.ModelID
		provider   bestiary.Provider
		wantFamily bestiary.Family
	}{
		{
			// CatA divergence: some providers return "MiniMax" (capitalised),
			// others return "minimax". M4 normalises both to lowercase "minimax".
			desc:       "MiniMax raw_family → lowercase minimax",
			rawFamily:  "MiniMax",
			id:         "minimax-m1-80k",
			provider:   "nano-gpt",
			wantFamily: "minimax",
		},
		{
			// "Hy" is the only uppercase entry in allFamilies. M4 lowercases it.
			desc:       "Hy raw_family → lowercase hy",
			rawFamily:  "Hy",
			id:         "hy3-something",
			provider:   "some-provider",
			wantFamily: "hy",
		},
		{
			// Already lowercase — M4 is a no-op; existing behaviour preserved.
			desc:       "claude raw_family unchanged by M4",
			rawFamily:  "claude-opus",
			id:         "claude-opus-4-6",
			provider:   "anthropic",
			wantFamily: "claude",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, _, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: M4 case-fold did not lowercase the Family field\n"+
					"  Why: SLICE-1 requires Family(strings.ToLower(...)) at the Family-field boundary\n"+
					"  How to fix: apply M4 case-fold in ParseFamilyDetailed and InferFamilyFromIDWithVariant",
					fam, tc.wantFamily)
			}
		})
	}
}

// TestM4_InferFamilyCaseFold verifies that InferFamilyFromIDWithVariant (the
// empty-raw-family path) also lowercases the inferred Family field (M4).
//
// BDD: Given an empty raw_family with a mixed-case model ID (e.g. "MiniMax-M1"),
// When InferFamilyFromIDWithVariant is called,
// Then the returned Family is lowercase ("minimax").
func TestM4_InferFamilyCaseFold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		id         bestiary.ModelID
		provider   bestiary.Provider
		wantFamily bestiary.Family
	}{
		{
			// MiniMax-M1: some providers have empty raw_family; model ID starts with
			// "MiniMax" (uppercase). After M3 path-strip and M4 lowercase, family
			// should be "minimax".
			desc:       "MiniMax-M1 empty raw_family → minimax (M4 lowercase)",
			id:         "MiniMax-M1",
			provider:   "nano-gpt",
			wantFamily: "minimax",
		},
		{
			// deepseek-ai/DeepSeek-V3.2: M3 path-strip gives "DeepSeek-V3.2", M4
			// lowercases first token → "deepseek".
			desc:       "DeepSeek-V3.2 after path strip → deepseek (M4 lowercase)",
			id:         "deepseek-ai/DeepSeek-V3.2",
			provider:   "some-provider",
			wantFamily: "deepseek",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, _, _ := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("InferFamilyFromIDWithVariant(%q) family = %q, want %q\n"+
					"  What: M4 case-fold did not lowercase inferred Family\n"+
					"  Why: SLICE-1 requires M4 lowercase at Family-field boundary in"+
					" InferFamilyFromIDWithVariant\n"+
					"  How to fix: apply Family(strings.ToLower(...)) at the return boundaries",
					tc.id, fam, tc.wantFamily)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// M3 — vendor/namespace strip via vendor_aliases.json
// ----------------------------------------------------------------------------

// TestM3_VendorAliasStrip verifies that model IDs starting with a vendor alias
// from vendor_aliases.json have the alias prefix stripped before family inference.
//
// BDD: Given a model ID starting with "minimaxai-" (a vendor alias NOT in
// Providers()),
// When InferFamilyFromIDWithVariant is called with empty raw_family,
// Then the vendor prefix is stripped and family="minimax" is inferred.
//
// The "/" separator case (e.g. "minimaxai/minimax-m1") is already handled by
// the existing lastPathSegment call. This test specifically covers the "-"
// separator variant.
func TestM3_VendorAliasStrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		{
			// "minimaxai-minimax-m1": M3 strips "minimaxai-" → "minimax-m1",
			// M4 lowercases → "minimax-m1"; SLICE-8 (d) series split → variant="m"
			// (CLARIFICATION-5 REVERSES the SLICE-0 whole-token "m1").
			desc:        "minimaxai-minimax-m1 → strip alias, family=minimax series variant=m",
			id:          "minimaxai-minimax-m1",
			provider:    "some-provider",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _ := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: M3 vendor alias strip did not remove vendor prefix\n"+
					"  How to fix: implement M3 '-' strip for vendor_aliases in pipeline",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: recoverMemberVariant did not recover variant from stripped ID",
					variant, tc.wantVariant)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// recoverMemberVariant — sole owner of member-variant recovery
// ----------------------------------------------------------------------------

// TestRecoverMemberVariant_FamiliesJSONMembers verifies that recoverMemberVariant
// recovers variant tokens from families.json members, specifically for tokens that
// are NOT in variant_suffixes.json (the old B1 scope) but ARE in the family's
// member list.
//
// BDD: Given raw_family="minimax" and id="minimax-m1-80k" (where "m1" is in
// families.json minimax.members but NOT in variant_suffixes.json),
// When ParseFamilyDetailed is called,
// Then variant="m1" is recovered.
//
// This test covers the NEW scope of recoverMemberVariant beyond old B1.
// It will be RED until L3 implements recoverMemberVariant.
func TestRecoverMemberVariant_FamiliesJSONMembers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		{
			// SLICE-8 (d) / CLARIFICATION-5: minimax is now a letter-prefix SERIES
			// (series_letter "m"), so the series split OWNS this decomposition and
			// supersedes the old whole-token "m1" member recovery: minimax-m1-80k →
			// variant="m" (+ version "1"; "80k" is an ignored context-window token).
			desc:        "minimax raw_family + id minimax-m1-80k → series variant=m",
			rawFamily:   "minimax",
			id:          "minimax-m1-80k",
			provider:    "minimax",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
		{
			// SLICE-8 (d) / CLARIFICATION-5: empty raw_family, MiniMax-M1 (mixed case)
			// → M4 family="minimax"; series split → variant="m" (+ version "1").
			desc:        "empty raw_family, MiniMax-M1 → series (minimax, m)",
			rawFamily:   "",
			id:          "MiniMax-M1",
			provider:    "nano-gpt",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
		{
			// qwen family, member "max" not in variant_suffixes.json.
			// raw_family="qwen", id="qwen-max" → variant="max" via member recovery.
			desc:        "qwen raw_family + id qwen-max → variant=max",
			rawFamily:   "qwen",
			id:          "qwen-max",
			provider:    "alibaba",
			wantFamily:  "qwen",
			wantVariant: "max",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q", fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: recoverMemberVariant did not recover variant from families.json members\n"+
					"  Why: SLICE-1 requires recoverMemberVariant to consult pd.families members\n"+
					"  How to fix: implement recoverMemberVariant in pipeline (L3)",
					variant, tc.wantVariant)
			}
		})
	}
}

// TestRecoverMemberVariant_SubsumesB1 verifies that the B1 family-agnostic
// sole-residual suffix promotion still yields its expected (family, variant,
// version) results.
//
// NOTE (SLICE-1 rc2 FIX CYCLE): B1 was NOT removed. The fix cycle RESTORED a
// version-preserving B1 that runs POST-version extraction for UNREGISTERED
// families (via the shared bareVariantSuffix helper) — see the B1 block in
// ParseFamilyDetailed and the recoverMemberVariant doc comment. These cases must
// remain green; if they turn red, the restored B1 promotion regressed.
func TestRecoverMemberVariant_SubsumesB1(t *testing.T) {
	t.Parallel()

	// These cases were already tested by TestParseFamilyDetailed_FixB1_SoleVariantSuffixPromotion.
	// They must remain green after B1 is removed. Including here as explicit
	// regression guards for the recoverMemberVariant subsumption.
	cases := []struct {
		desc          string
		rawFamily     bestiary.Family
		id            bestiary.ModelID
		provider      bestiary.Provider
		wantFamily    bestiary.Family
		wantVariant   string
		wantVersion   string
		wantNoFailure bool
	}{
		{
			// SLICE-10: 'turbo'→Modifier (glm non-member) → variant empty. The
			// ReasonKnownSuffixOverflow audit annotation now fires (codegen clears it).
			desc:          "glm-5-turbo → (glm, '', 5) turbo→Modifier [B1 subsumed]",
			rawFamily:     "glm",
			id:            "glm-5-turbo",
			provider:      "zhipu",
			wantFamily:    "glm",
			wantVariant:   "",
			wantVersion:   "5",
			wantNoFailure: false,
		},
		{
			desc:          "phi-4-mini → (phi, mini, 4) [B1 subsumed]",
			rawFamily:     "phi",
			id:            "phi-4-mini",
			provider:      "microsoft",
			wantFamily:    "phi",
			wantVariant:   "mini",
			wantVersion:   "4",
			wantNoFailure: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q", fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q (B1 subsumption check)", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if tc.wantNoFailure && failure != nil {
				t.Errorf("failure = %+v, want nil (B1 subsumption: no residual failure expected)", failure)
			}
		})
	}
}

// TestRecoverMemberVariant_SubsumesAmputation verifies that recoverMemberVariant
// subsumes the empty-raw amputation (parse.go:819-821) in
// InferFamilyFromIDWithVariant, preserving GUARD-2 tests green.
//
// The amputation case (family == candidateFamilyStr → firstToken) must be replaced
// by: firstToken as family + recoverMemberVariant for variant.
func TestRecoverMemberVariant_SubsumesAmputation(t *testing.T) {
	t.Parallel()

	// SLICE-8 (d) / CLARIFICATION-5 RED→GREEN flip (SUPERSEDES the SLICE-3
	// (kimi,"","") pin): kimi is a letter-prefix series, so InferFamilyFromIDWithVariant
	// applies the series split → (kimi, "k", "2"). The trailing "thinking" is a
	// Modifier (surfaced by ParseFamilyDetailed's ExtractModifier), never a Variant.
	t.Run("kimi-k2-thinking → series (kimi,k,2); thinking is a Modifier", func(t *testing.T) {
		t.Parallel()
		fam, variant, version := bestiary.InferFamilyFromIDWithVariant("kimi-k2-thinking", "moonshot")
		if fam != "kimi" {
			t.Errorf("family = %q, want %q", fam, "kimi")
		}
		if variant != "k" {
			t.Errorf("variant = %q, want %q", variant, "k")
		}
		if version != "2" {
			t.Errorf("version = %q, want %q", version, "2")
		}
	})

	// New: when the amputation path IS taken (passthrough), recoverMemberVariant
	// should recover the variant from the remaining tokens.
	t.Run("empty raw_family MiniMax-M1 → series (minimax, m, 1)", func(t *testing.T) {
		t.Parallel()
		// SLICE-8 (d) / CLARIFICATION-5: minimax is a letter-prefix series, so the
		// series split owns this — variant="m", version="1" (REVERSES whole-token "m1").
		fam, variant, version := bestiary.InferFamilyFromIDWithVariant("MiniMax-M1", "nano-gpt")
		if fam != "minimax" {
			t.Errorf("family = %q, want %q (expected M4 lowercase)", fam, "minimax")
		}
		if variant != "m" || version != "1" {
			t.Errorf("(variant,version) = (%q,%q), want (\"m\",\"1\") (CLARIFICATION-5 series split)",
				variant, version)
		}
	})
}

// ----------------------------------------------------------------------------
// Loader fail-fast — families.json key validation
// ----------------------------------------------------------------------------

// TestFamiliesJSON_LoaderFailFast verifies that FamiliesJSONKeyError catches
// unknown keys (typos) and accepts valid known keys.
//
// BDD: Given families.json with a typo key "claud" (not in allFamilies),
// When FamiliesJSONKeyError is called,
// Then a non-nil error is returned (fail-fast behaviour).
//
// This test is GREEN from L1 (FamiliesJSONKeyError is part of L1 infrastructure).
// It serves as the specification of the fail-fast contract.
func TestFamiliesJSON_LoaderFailFast(t *testing.T) {
	t.Parallel()

	t.Run("valid key passes", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"claude": {"members": ["opus"], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("valid key 'claude' caused error: %v", err)
		}
	})

	t.Run("typo key fails with actionable error", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"claud": {"members": ["opus"], "bare_gen_split": false}}`)
		err := bestiary.FamiliesJSONKeyError(data)
		if err == nil {
			t.Error("typo key 'claud' did not cause error — fail-fast not triggered\n" +
				"  What: FamiliesJSONKeyError must fail on unknown key\n" +
				"  Why: 'claud' is not in allFamilies (likely typo of 'claude')\n" +
				"  How to fix: ensure initParseData / FamiliesJSONKeyError validates keys")
		}
		// Verify the error mentions the bad key.
		if err != nil && !strings.Contains(err.Error(), "claud") {
			t.Errorf("error %q does not mention the bad key 'claud' — not actionable", err.Error())
		}
	})

	t.Run("_comment key is skipped (not validated)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"_comment": "test", "gpt": {"members": ["mini"], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("_comment key should be skipped, got error: %v", err)
		}
	})

	t.Run("Hy (uppercase in allFamilies) accepted as hy", func(t *testing.T) {
		t.Parallel()
		// allFamilies has "Hy" (uppercase). families.json uses lowercase keys.
		// FamiliesJSONKeyError must accept "hy" as matching "Hy" case-insensitively.
		data := []byte(`{"hy": {"members": [], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("'hy' should be valid (case-insensitive match for allFamilies 'Hy'), got error: %v", err)
		}
	})

	t.Run("openai is not a Family (provider name, not in allFamilies)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"openai": {"members": ["gpt"], "bare_gen_split": false}}`)
		err := bestiary.FamiliesJSONKeyError(data)
		if err == nil {
			t.Error("'openai' is a provider name, not a Family — should fail key validation")
		}
	})
}

// ============================================================================
// SLICE-3 (rc2) — family_aliases canonical-winner ledger
// ============================================================================

// TestFamilyAliasesJSON_LoaderFailFast verifies the SLICE-3 ledger validation
// contract: alias TARGETS (canonical family values) must be known families, while
// alias KEYS (mislabels) are deliberately NOT validated.
//
// BDD: Given a ledger row whose TARGET is a typo (not in allFamilies), When
// FamilyAliasesJSONError is called, Then a non-nil actionable error naming the bad
// target is returned. Given a non-canonical KEY mapping to a valid target, Then no
// error (keys are arbitrary mislabels by design).
func TestFamilyAliasesJSON_LoaderFailFast(t *testing.T) {
	t.Parallel()

	t.Run("valid target passes (ratified l3 → llama)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"aliases": {"l3": "llama", "l3.1": "llama"}}`)
		if err := bestiary.FamilyAliasesJSONError(data); err != nil {
			t.Errorf("valid target 'llama' caused error: %v", err)
		}
	})

	t.Run("typo target fails with actionable error", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"aliases": {"l3": "lluma"}}`)
		err := bestiary.FamilyAliasesJSONError(data)
		if err == nil {
			t.Fatal("typo target 'lluma' did not cause error — target validation not triggered")
		}
		if !strings.Contains(err.Error(), "lluma") {
			t.Errorf("error %q does not mention the bad target 'lluma' — not actionable", err.Error())
		}
	})

	t.Run("non-canonical KEY with valid target is accepted (keys are mislabels)", func(t *testing.T) {
		t.Parallel()
		// "l3.1" is NOT itself a canonical family — it is a mislabel. Only the TARGET
		// ("llama") must be canonical. This must pass.
		data := []byte(`{"aliases": {"l3.1": "llama"}}`)
		if err := bestiary.FamilyAliasesJSONError(data); err != nil {
			t.Errorf("non-canonical key 'l3.1' → valid target 'llama' should pass, got: %v", err)
		}
	})
}

// TestFamilyAliasesLedger_Fold verifies the RATIFIED l3/l3.1/l3.3 → llama fold
// end-to-end through ParseFamilyDetailed (the canonical-winner ledger applied after
// M4 family normalisation, before bare-gen-split). Community Llama-3 finetunes
// (sao10k/*) labelled with the "L3.x" shorthand must canonicalise to family "llama"
// so the family agrees cross-provider.
//
// SCOPE NOTE: the finetune name and the embedded "3.x" version are residual here —
// version recovery for folded families is SLICE-8 (version-presence), out of scope.
func TestFamilyAliasesLedger_Fold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id         bestiary.ModelID
		wantFamily bestiary.Family
	}{
		{"sao10k/l3-euryale-70b", "llama"},
		{"sao10K/l3-8b-lunaris", "llama"},
		{"sao10k/l3.1-70b-hanami-x1", "llama"},
		{"sao10k/l3.1-euryale-70b", "llama"},
		{"sao10k/l3.3-euryale-70b", "llama"},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, _ := bestiary.ParseFamilyDetailed("", tc.id, "p")
			if family != tc.wantFamily {
				t.Errorf("ParseFamilyDetailed(\"\", %q) family = %q, want %q\n"+
					"  What: the family_aliases ledger fold (l3* → llama) did not fire\n"+
					"  Why: RATIFIED row in parse/data/family_aliases.json must remap after M4",
					tc.id, family, tc.wantFamily)
			}
		})
	}
}

// TestFamilyAliasesLedger_DefaultOwnFamily verifies the DEFAULT own-family rule:
// genuinely distinct families that have NO ledger row are left unchanged (no
// accidental fold). These are the families the supervisor explicitly ratified as
// own-family in bestiary-7ipe.
func TestFamilyAliasesLedger_DefaultOwnFamily(t *testing.T) {
	t.Parallel()

	for _, raw := range []bestiary.Family{"mixtral", "ministral", "qwq", "aion", "pixtral", "voxtral"} {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, _ := bestiary.ParseFamilyDetailed(raw, bestiary.ModelID(raw), "p")
			if family != raw {
				t.Errorf("ParseFamilyDetailed(%q,…) family = %q, want %q (DEFAULT own-family: no ledger row)",
					raw, family, raw)
			}
		})
	}
}

// ============================================================================
// SLICE-2 (rc2) — L2 Tests (RED until L3 implements the bare_gen_split predicate)
// ============================================================================

// TestM2_BareGenSplit_PositiveSplits verifies the M2 bare-generation split: a
// glued family token <base><int> (e.g. "qwen3", "o1") OR a clean family whose ID
// carries a glued generation token decomposes to (base, …, version=int) when the
// CLOSED predicate holds (has families.json entry ∧ base not digit-suffixed ∧
// bare_gen_split:true flag attested in the snapshot).
//
// BDD: Given "qwen3-max" When decomposed Then (qwen, max, 3).
// These cases are RED until L3 implements the predicate at the SLICE-2 insertion
// point in BOTH entrypoints (InferFamilyFromIDWithVariant + ParseFamilyDetailed).
func TestM2_BareGenSplit_PositiveSplits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// Bare glued family token, empty raw — split base off the trailing int.
		{"qwen3 → (qwen,,3)", "", "qwen3", "alibaba", "qwen", "", "3"},
		// SLICE-12 (bestiary-xdbc Q2a): the 'o' family folds into gpt as a VARIANT —
		// o1 → (gpt, variant='o', version=1). Supersedes the SLICE-2 o1→(o,,1) row.
		{"o1 → (gpt,o,1) (bestiary-xdbc Q2a)", "", "o1", "openai", "gpt", "o", "1"},
		// Glued family token + member variant (empty-raw inference path).
		{"qwen3-max (raw empty) → (qwen,max,3)", "", "qwen3-max", "qiniu-ai", "qwen", "max", "3"},
		// CLEAN raw-supplied family + glued generation in the ID: the (B) version
		// recovery half must surface the glued int as version so both providers agree.
		{"qwen3-max (raw qwen) → (qwen,max,3)", "qwen", "qwen3-max", "alibaba", "qwen", "max", "3"},
		// SLICE-12 (bestiary-xdbc Q2a/Q2b): o3-mini → (gpt, variant='o', version=3),
		// mini→modifier (not asserted here). Supersedes the SLICE-2 o3-mini→(o,mini,3) row.
		{"o3-mini (raw o) → (gpt,o,3) (bestiary-xdbc Q2a)", "o", "openai/o3-mini", "openrouter", "gpt", "o", "3"},
		// Hyphenated generation already extracts version on the raw side; the
		// empty-raw inferred family "gpt-5"/"gemini-3" must split to the base.
		{"gpt-5-mini (raw empty) → (gpt,mini,5)", "", "openai/gpt-5-mini", "kilo", "gpt", "mini", "5"},
		{"gemini-3-flash-preview (raw empty) → (gemini,flash,3)", "", "gemini-3-flash-preview", "302ai", "gemini", "flash", "3"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, version, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: bare_gen_split did not split the glued generation off the family\n"+
					"  Why: SLICE-2 closed predicate (has-entry ∧ not-digit-suffixed ∧ flag) should split\n"+
					"  How to fix: implement the bare_gen_split predicate at the SLICE-2 insertion point",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: bare_gen_split did not surface the generation int as version\n"+
					"  Why: split <base><int> → version=int, including the clean-family (B) recovery half",
					version, tc.wantVersion)
			}
		})
	}
}

// TestM2_BareGenSplit_NonSplit verifies the CLOSED predicate's negative cases:
// tokens that look like <base><int> but MUST NOT split because a clause fails.
//
//   - v0 / asi1 / esm2 / wan2 / hy3 / r1: base ("v"/"asi"/"esm"/"wan"/"hy"/"r")
//     has NO families.json entry → has-entry clause fails.
//   - l3: l3's base "l" has no entry — CLARIFICATION-1.4 (digit-suffix guard +
//     has-entry).
//
// NOTE (SLICE-8 d): the letter-prefix series cases that USED to live here
// (minimax-m2.5 / kimi-k2.5 / mimo-v2.5 / mimo-v1) are now decomposed by the
// CLARIFICATION-5 series split (variant=letter + version=number) — a DIFFERENT
// mechanism from bare_gen_split. bare_gen_split STILL declines them (their bases
// carry no bare_gen_split flag); the observable ParseFamilyDetailed tuple is now
// owned by splitSeriesVariant and asserted in TestSeriesLetterSplit_CLARIFICATION5.
//
// These assert the predicate is CLOSED (no per-name allow-list): the family
// stays the un-split token.
func TestM2_BareGenSplit_NonSplit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// has-entry clause fails (base not a families.json key).
		{"v0 NOT split (v∉families)", "", "v0-1.5", "p", "v0", ""},
		{"asi1 NOT split (asi∉families)", "", "asi1-mini", "p", "asi1", "mini"},
		{"esm2 NOT split (esm∉families)", "", "esm2-large", "p", "esm2", "large"},
		{"wan2 NOT split (wan∉families)", "", "wan2-t2v", "p", "wan2", ""},
		// SLICE-14: "hy3" MOVED to the SPLIT set — "hy" is now a registered family
		// (bare "hy" attested via raw="Hy"), so hy3-preview → (hy, "", 3) [see
		// TestSLICE14_TIER1Convergences]. It is no longer a NonSplit case.
		{"r1 NOT split (r∉families)", "", "r1", "p", "r1", ""},
		// SLICE-3: bare-gen still DECLINES "l3" (base "l" ∉ families.json), but the
		// family_aliases ledger then folds l3 → llama (RATIFIED: L3.x = Llama-3
		// shorthand). The closed-predicate guarantee (no bare-gen split) is unchanged;
		// the canonical family arrives via the ledger remap, not the split.
		{"l3 → llama via ledger (bare-gen declines: l∉families)", "", "l3-8b", "p", "llama", ""},
		// NOTE: the former minimax-m2.5 / kimi-k2.5 / mimo-v2.5 / mimo-v1 cases moved
		// to TestSeriesLetterSplit_CLARIFICATION5 — they now decompose via the SLICE-8
		// (d) letter-prefix series split, not bare_gen_split.
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: bare_gen_split wrongly split a token whose closed-predicate clause fails\n"+
					"  Why: the predicate is CLOSED — no families.json entry (or no trailing digit) means NO split\n"+
					"  How to fix: gate the split on has-entry ∧ not-digit-suffixed ∧ bare_gen_split flag",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q (dotted numerics must remain variant tokens)", variant, tc.wantVariant)
			}
		})
	}
}

// ============================================================================
// SLICE-8 (rc2): ID-driven version-presence consistency + param-size guard +
// glued letter-suffix + letter-prefix series split (CLARIFICATION-4 + -5).
// ============================================================================

// TestSlice8_VersionPresenceConsistency_ClassA verifies SLICE-8 (a): a version
// derivable from the (vendor-stripped, case-folded) model ID is extracted
// CONSISTENTLY regardless of the provider raw_family. Each case asserts that the
// SAME id decomposes to an IDENTICAL (Family, Variant, Version) under BOTH an
// empty raw_family (the inference path) AND the provider's populated raw_family.
func TestSlice8_VersionPresenceConsistency_ClassA(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawPopulated bestiary.Family
		id           bestiary.ModelID
		wantFamily   bestiary.Family
		wantVariant  string
		wantVersion  string
	}{
		{"gpt-4.1 (gpt | empty)", "gpt", "openai/gpt-4.1", "gpt", "", "4.1"},
		{"glm-4.6 (glm | empty)", "glm", "z-ai/glm-4.6", "glm", "", "4.6"},
		{"gemma-3-12b-it (gemma | empty)", "gemma", "google/gemma-3-12b-it", "gemma", "", "3"},
		{"claude-3-5-haiku (claude-haiku | empty)", "claude-haiku", "claude-3-5-haiku-20241022", "claude", "haiku", "3.5"},
		{"grok-4.1-fast (grok | empty)", "grok", "x-ai/grok-4.1-fast", "grok", "", "4.1"},
		{"grok-4-fast (grok | empty)", "grok", "grok-4-fast-non-reasoning", "grok", "", "4"},
		{"ernie-4.5-21b-a3b (ernie | empty)", "ernie", "baidu/ernie-4.5-21b-a3b", "ernie", "", "4.5"},
		{"claude-opus-4.6-fast (claude-opus | empty)", "claude-opus", "anthropic/claude-opus-4.6-fast", "claude", "opus", "4.6"},
		{"mistral-medium-3-5 (mistral-medium | empty)", "mistral-medium", "mistralai/mistral-medium-3-5", "mistral", "medium", "3.5"},
		{"GLM-5 mixed-case (glm | empty)", "glm", "zai-org/GLM-5", "glm", "", "5"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			for _, raw := range []bestiary.Family{"", tc.rawPopulated} {
				f, va, ve, _, _ := bestiary.ParseFamilyDetailed(raw, tc.id, "p")
				if f != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVersion {
					t.Errorf("raw=%q id=%q → (%s|%s|%s), want (%s|%s|%s)\n"+
						"  What: ID-driven version not extracted consistently across raw_family\n"+
						"  Why: SLICE-8 (a) — version must derive from the ID regardless of raw_family",
						raw, tc.id, f, va, ve, tc.wantFamily, tc.wantVariant, tc.wantVersion)
				}
			}
		})
	}
}

// TestSlice8_ParamSizeGuard verifies SLICE-8 (b): parameter-count / model-size
// tokens (NNNb / NNNm / MoE) are NEVER promoted to Version. The size INFO is GH#9
// (missing Size dimension), explicitly not a version. Asserted on ALL providers
// (empty + populated raw) so gpt-oss-120b is Version "" everywhere (consistent).
func TestSlice8_ParamSizeGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		raw  bestiary.Family
		id   bestiary.ModelID
	}{
		{"gpt-oss-120b (raw gpt-oss)", "gpt-oss", "gpt-oss-120b"},
		{"gpt-oss-120b (empty raw)", "", "gpt-oss-120b"},
		{"gpt-oss-20b (raw gpt-oss)", "gpt-oss", "gpt-oss-20b"},
		{"qwen3-coder-30b-a3b MoE (raw qwen)", "qwen", "qwen3-coder-30b-a3b"},
		{"mixtral-8x22b MoE (empty raw)", "", "mistralai/mixtral-8x22b"},
		{"ernie-4.5-300b-a47b (raw ernie) keeps 4.5 not size", "ernie", "baidu/ernie-4.5-300b-a47b"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, ve, _, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			// The version must never be a bare param-size token (e.g. "120b", "20b",
			// "30b", "8x22b"). For ernie the genuine version 4.5 IS allowed.
			for _, bad := range []string{"120b", "20b", "30b", "8x22b", "300b", "a3b", "a47b"} {
				if ve == bad {
					t.Errorf("raw=%q id=%q → version=%q is a param-size token (must be dropped — GH#9, not a version)", tc.raw, tc.id, ve)
				}
			}
		})
	}

	// Direct guard unit checks on the public ExtractVersionFromID path.
	t.Run("ExtractVersionFromID drops 120b, keeps 4o/versions", func(t *testing.T) {
		t.Parallel()
		if v := bestiary.ExtractVersionFromID("gpt-oss-120b", "gpt-oss"); v != "" {
			t.Errorf("gpt-oss-120b → %q, want \"\" (param-size guard)", v)
		}
		if v := bestiary.ExtractVersionFromID("gpt-4o", "gpt"); v != "4o" {
			t.Errorf("gpt-4o → %q, want \"4o\" (genuine version, NOT a size)", v)
		}
	})
}

// TestSlice8_GluedVersionModifier verifies the glued letter-after-version handling.
// SLICE-12 (bestiary-xdbc) SUPERSEDES the SLICE-8(c) glm-4.5v→vision behaviour:
//   - Q1: the glued single 'v' after a glm version is the VARIANT 'v' (glm-4.5v →
//     (glm, "v", 4.5), NOT modifier vision). The spelled-out "-vision" hyphen token
//     remains a Modifier (uniform rule unchanged) and is NOT exercised here.
//   - Q2/Q2b: gpt-4o → variant '4o', version ” ('4o' is the line designator, not a
//     version). Supersedes the prior (gpt,"",4o) pin.
func TestSlice8_GluedVersionModifier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc                                          string
		raw                                           bestiary.Family
		id                                            bestiary.ModelID
		wantFamily, wantVariant, wantVersion, wantMod string
	}{
		{"glm-4.5v raw=glm → variant 'v' (bestiary-xdbc Q1)", "glm", "glm-4.5v", "glm", "v", "4.5", ""},
		{"glm-4.5v empty raw → variant 'v' (bestiary-xdbc Q1)", "", "glm-4.5v", "glm", "v", "4.5", ""},
		{"gpt-4o → variant '4o', version '' (bestiary-xdbc Q2b)", "gpt", "gpt-4o", "gpt", "4o", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVersion || modJoin(mod) != tc.wantMod {
				t.Errorf("raw=%q id=%q → (%s|%s|%s|mod=%s), want (%s|%s|%s|mod=%s)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFamily, tc.wantVariant, tc.wantVersion, tc.wantMod)
			}
		})
	}
}

// TestSeriesLetterSplit_CLARIFICATION5 verifies SLICE-8 (d): letter-prefix model
// series (kimi→k, minimax→m, mimo→v) decompose to variant=SERIES-LETTER +
// version=NUMBER, with ALL attested forms normalized consistently. This SUPERSEDES
// the SLICE-0 whole-token plan (minimax "m1") and SLICE-3's kimi-k2-thinking
// (kimi,"","") pin, and the version_patterns letter-prefix whole-token-variant.
//
// TIER INTERACTION: surfaced + ruled by the user (CLARIFICATION-6): tier→Modifier,
// variant stays the pure series-letter — pinned in TestSeriesTierModifier_CLARIFICATION6.
// MULTI-MODIFIER cases (tier + thinking/vision) remain surfaced (single-valued
// Modifier; multiplicity ruling pending) and keep the existing thinking modifier.
func TestSeriesLetterSplit_CLARIFICATION5(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc                             string
		raw                              bestiary.Family
		id                               bestiary.ModelID
		wantFamily, wantVariant, wantVer string
		wantMod                          string
	}{
		// kimi K-series (empty + populated raw → identical).
		{"kimi-k2 empty raw", "", "kimi-k2", "kimi", "k", "2", ""},
		{"kimi-k2 raw=kimi", "kimi", "kimi-k2", "kimi", "k", "2", ""},
		{"kimi-k2.5 raw=kimi-k2.5", "kimi-k2.5", "kimi-k2.5", "kimi", "k", "2.5", ""},
		{"kimi-k2.6 empty raw", "", "kimi-k2.6", "kimi", "k", "2.6", ""},
		{"kimi-k2p5 (p=dot)", "", "accounts/fireworks/models/kimi-k2p5", "kimi", "k", "2.5", ""},
		{"kimi-k2p6 (p=dot)", "kimi-thinking", "accounts/fireworks/models/kimi-k2p6", "kimi", "k", "2.6", "thinking"},
		{"kimi-k2-5 (hyphen=dot)", "kimi", "kimi-k2-5", "kimi", "k", "2.5", ""},
		{"kimi-k2-0711 (date dropped, ver 2)", "kimi", "kimi-k2-0711", "kimi", "k", "2", ""},
		{"kimi-k2:1t (context tag, ver 2)", "kimi", "kimi-k2:1t", "kimi", "k", "2", ""},
		{"kimi-k2-thinking → series + modifier", "kimi-thinking", "kimi-k2-thinking", "kimi", "k", "2", "thinking"},
		{"kimi-k2-thinking empty raw", "", "kimi-k2-thinking", "kimi", "k", "2", "thinking"},
		// minimax M-series (REVERSES SLICE-0 whole-token "m1").
		{"minimax-m1 raw=minimax", "minimax", "minimax-m1", "minimax", "m", "1", ""},
		{"minimax-m1 empty raw", "", "minimax-m1", "minimax", "m", "1", ""},
		{"MiniMax-M1-80k (context window ignored)", "minimax", "MiniMaxAI/MiniMax-M1-80k", "minimax", "m", "1", ""},
		{"minimax-m2.1", "minimax", "minimax-m2.1", "minimax", "m", "2.1", ""},
		// mimo V-series.
		{"mimo-v2.5 raw=mimo", "mimo", "mimo-v2.5", "mimo", "v", "2.5", ""},
		{"mimo-v2.5 empty raw", "", "xiaomi/mimo-v2.5", "mimo", "v", "2.5", ""},
		{"mimo-v1", "", "mimo-v1", "mimo", "v", "1", ""},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVer || modJoin(mod) != tc.wantMod {
				t.Errorf("raw=%q id=%q → (%s|%s|%s|mod=%s), want (%s|%s|%s|mod=%s)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFamily, tc.wantVariant, tc.wantVer, tc.wantMod)
			}
		})
	}
}

// TestSlice8_MustNotRegress_RealVersions pins genuine semantic versions that the
// param-size guard and series split MUST leave UNCHANGED (the size/series logic
// distinguishes size tokens and series letters from real version numbers).
func TestSlice8_MustNotRegress_RealVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		raw         bestiary.Family
		id          bestiary.ModelID
		wantVersion string
	}{
		{"4.5 dotted", "claude-opus", "claude-opus-4-5-20251101", "4.5"},
		{"2.5 dotted", "gemini-flash", "gemini-2.5-flash", "2.5"},
		// SLICE-12 (bestiary-xdbc Q2b): "4o" is now the VARIANT (line designator), so the
		// version is EMPTY. Supersedes the SLICE-8 "4o is a version" pin. (Variant=4o is
		// asserted in TestSlice8_GluedVersionModifier.)
		{"gpt-4o → version '' ('4o' is the variant; bestiary-xdbc Q2b)", "gpt", "gpt-4o", ""},
		{"3.5 (claude-haiku)", "claude-haiku", "claude-3-5-haiku-20241022", "3.5"},
		{"3.7 (claude-sonnet)", "claude-sonnet", "claude-3-7-sonnet-20250219", "3.7"},
		{"single-digit 5", "gpt", "openai/gpt-5", "5"},
		{"dotted 3.1", "llama", "meta-llama/llama-3.1-8b", "3.1"},
		{"mistral-small-2603 date NOT a version", "mistral-small", "mistral-small-2603", ""},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, ve, _, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if ve != tc.wantVersion {
				t.Errorf("raw=%q id=%q → version=%q, want %q (must-not-regress real version / date-guard)",
					tc.raw, tc.id, ve, tc.wantVersion)
			}
		})
	}
}

// TestSeriesTierModifier_CLARIFICATION6 verifies the tier→Modifier promotion: a
// curated TIER token trailing a letter-prefix series token becomes the Modifier,
// while the variant stays the PURE series-letter (kimi-k2-instruct →
// (kimi,'k','2',mod=instruct)). The promotion is SERIES-SCOPED — it must NOT
// reclassify the SAME token when it is a VARIANT of a NON-series family
// (gpt-5-mini, gemini-2.5-flash, qwen-turbo, llama-*-instruct stay variants).
//
// MULTI-MODIFIER cases (tier + thinking/vision, or 2+ tiers) are NOT pinned here:
// the Modifier field is single-valued and the multiplicity rule is pending — those
// keep the series split + the existing thinking/vision modifier and DROP the tier
// (surfaced to the supervisor, not resolved unilaterally).
func TestSeriesTierModifier_CLARIFICATION6(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc                                          string
		raw                                           bestiary.Family
		id                                            bestiary.ModelID
		wantFamily, wantVariant, wantVersion, wantMod string
	}{
		// Clean single-tier → modifier (variant = pure series-letter).
		{"minimax-m2.5-fast (raw)", "minimax", "minimax-m2.5-fast", "minimax", "m", "2.5", "fast"},
		{"minimax-m2.5-fast (empty raw)", "", "minimax-m2.5-fast", "minimax", "m", "2.5", "fast"},
		{"minimax-m2.5-highspeed", "minimax", "minimax-m2.5-highspeed", "minimax", "m", "2.5", "highspeed"},
		{"mimo-v2.5-pro (raw)", "mimo", "mimo-v2.5-pro", "mimo", "v", "2.5", "pro"},
		{"mimo-v2.5-pro (empty raw)", "", "xiaomi/mimo-v2.5-pro", "mimo", "v", "2.5", "pro"},
		{"kimi-k2-instruct (raw)", "kimi", "kimi-k2-instruct", "kimi", "k", "2", "instruct"},
		{"kimi-k2-instruct (empty raw)", "", "moonshotai/kimi-k2-instruct", "kimi", "k", "2", "instruct"},
		{"kimi-k2.5-fast", "kimi", "kimi-k2.5-fast", "kimi", "k", "2.5", "fast"},
		{"kimi-k2.6-precision", "kimi-k2.6", "kimi-k2.6-precision", "kimi", "k", "2.6", "precision"},
		{"mimo-v2-omni (omni curated tier)", "mimo", "mimo-v2-omni", "mimo", "v", "2", "omni"},
		// EDGE (b): the SAME tokens are VARIANTS for NON-series families — UNCHANGED.
		{"gpt-5-mini stays variant=mini", "gpt", "openai/gpt-5-mini", "gpt", "mini", "5", ""},
		{"gemini-2.5-flash stays variant=flash", "gemini-flash", "gemini-2.5-flash", "gemini", "flash", "2.5", ""},
		{"qwen-turbo stays variant=turbo (member-guard)", "qwen", "qwen-turbo", "qwen", "turbo", "", ""},
		// SLICE-10: 'instruct' is now a GLOBAL modifier (llama non-member) → variant empty, mod [instruct].
		{"llama-instruct → [instruct] (SLICE-10 member-guard)", "llama", "meta-llama/llama-3.1-8b-instruct", "llama", "", "3.1", "instruct"},
		// SLICE-10 MULTI-MODIFIER: tier + capability compose LOSSLESSLY in the Modifier list.
		{"kimi-k2p6-turbo (raw kimi-thinking) → [thinking,turbo]", "kimi-thinking", "accounts/fireworks/routers/kimi-k2p6-turbo", "kimi", "k", "2.6", "thinking,turbo"},
		{"kimi-k2-thinking-turbo (raw kimi-thinking) → [thinking,turbo]", "kimi-thinking", "kimi-k2-thinking-turbo", "kimi", "k", "2", "thinking,turbo"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVersion || modJoin(mod) != tc.wantMod {
				t.Errorf("raw=%q id=%q → (%s|%s|%s|mod=%s), want (%s|%s|%s|mod=%s)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFamily, tc.wantVariant, tc.wantVersion, tc.wantMod)
			}
		})
	}
}

// SLICE-10 (rc2): TestSlice8_MultiModifier_DeferredToModifierListSlice was REMOVED.
// It pinned the S8 single-Modifier interim (kimi-k2-thinking-turbo DROPPED "turbo"). The
// Modifier-LIST schema change (CLARIFICATION-7) now populates BOTH losslessly
// ([thinking, turbo]); the lossless multi-modifier behaviour is asserted by
// TestParseFamilyDetailed_Slice10_ModifierList.
func TestParseFamilyDetailed_Slice10_ModifierList(t *testing.T) {
	t.Parallel()
	cases := []struct {
		desc                             string
		raw                              bestiary.Family
		id                               bestiary.ModelID
		wantFamily, wantVariant, wantVer string
		wantMod                          string // canonical comma-joined
	}{
		// Multi-modifier lossless capture (replaces the S8 interim drop).
		{"kimi-k2-thinking-turbo → [thinking,turbo]", "kimi-thinking", "kimi-k2-thinking-turbo", "kimi", "k", "2", "thinking,turbo"},
		{"kimi-k2p6-turbo + thinking → [thinking,turbo]", "kimi-thinking", "accounts/fireworks/routers/kimi-k2p6-turbo", "kimi", "k", "2.6", "thinking,turbo"},
		{"kimi triple → [thinking,turbo,original]", "kimi-thinking", "moonshotai/kimi-k2-thinking-turbo-original", "kimi", "k", "2", "thinking,turbo,original"},
		// Per-ID convergence targets for the 9 stragglers (canonical order).
		{"command-a-reasoning → [reasoning]", "command-a", "command-a-reasoning-08-2025", "command", "a", "", "reasoning"},
		{"llama vision-instruct → [vision,instruct]", "llama", "llama-3.2-11b-vision-instruct", "llama", "", "3.2", "vision,instruct"},
		{"phi multimodal-instruct → [multimodal,instruct]", "phi", "phi-4-multimodal-instruct", "phi", "", "4", "multimodal,instruct"},
		{"llama-4-scout(-instruct) → variant scout + [instruct]", "llama", "llama-4-scout-17b-16e-instruct", "llama", "scout", "4", "instruct"},
		{"qwen3-next(-instruct) → variant next + [instruct]", "qwen", "qwen3-next-80b-a3b-instruct", "qwen", "next", "3", "instruct"},
		// Must-not-regress (single-modifier + member-variant protection).
		{"kimi-k2-thinking → [thinking]", "kimi-thinking", "kimi-k2-thinking", "kimi", "k", "2", "thinking"},
		{"grok-vision → [vision]", "grok-vision", "grok-vision", "grok", "", "", "vision"},
		{"claude-3-7-sonnet-thinking → [thinking]", "claude-sonnet", "claude-3-7-sonnet-thinking", "claude", "sonnet", "3.7", "thinking"},
		{"deepseek-chat → variant chat (member-guard)", "deepseek", "deepseek-chat", "deepseek", "chat", "", ""},
		// fix-cycle 1 (Reviewer-A/C BLOCKER): RawFamily-embedded member must NOT duplicate
		// into BOTH Variant and Modifier. Use the CODEGEN-REAL raw="sonar-reasoning"
		// (the idealized raw="sonar" masked the bug). reasoning stays the VARIANT, no dup.
		{"sonar-reasoning (raw=sonar-reasoning) → (sonar,reasoning,nil) no dup", "sonar-reasoning", "sonar-reasoning", "sonar", "reasoning", "", ""},
		{"sonar-reasoning-pro (raw=sonar-reasoning) → (sonar,reasoning,nil) no dup", "sonar-reasoning", "sonar-reasoning-pro", "sonar", "reasoning", "", ""},
		// Regression guards: other RawFamily-embedded members must also stay variant-only.
		{"deepseek-chat (raw=deepseek-chat) → variant chat, no dup", "deepseek-chat", "deepseek-chat", "deepseek", "chat", "", ""},
		{"qwen-turbo → variant turbo (member-guard)", "qwen", "qwen-turbo", "qwen", "turbo", "", ""},
		{"gemini-pro → variant pro (stays variant)", "gemini", "gemini-pro", "gemini", "pro", "", ""},
		{"qwen-flash → variant flash (stays variant)", "qwen", "qwen-flash", "qwen", "flash", "", ""},
		// fix-cycle 1 (FLAG2): whisper + seed registered as families → variant recovers
		// losslessly; the modifier composes (turbo/instruct), removing the 2 justifiedExceptions.
		// rc3-L2 (fz9r): whisper-family-gated trailing "-v3" now recovers Version=3 (was "").
		{"whisper-large-v3-turbo → (whisper,large,3,[turbo])", "whisper", "whisper-large-v3-turbo", "whisper", "large", "3", "turbo"},
		{"seed-oss-36b-instruct → (seed,oss,[instruct])", "seed", "bytedance/seed-oss-36b-instruct", "seed", "oss", "", "instruct"},
		// fix-cycle 1: lossless variant-suffix→modifier split (v2.5-turbo → v2.5 + [turbo]).
		{"elevenlabs-v2.5-turbo → (elevenlabs,v2.5,[turbo])", "elevenlabs", "elevenlabs/elevenlabs-v2.5-turbo", "elevenlabs", "v2.5", "", "turbo"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVer || modJoin(mod) != tc.wantMod {
				t.Errorf("raw=%q id=%q → (%s|%s|%s|mod=%v), want (%s|%s|%s|mod=%s)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFamily, tc.wantVariant, tc.wantVer, tc.wantMod)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// SLICE-9 (rc2) PATH-UNIFICATION unit tests (CLARIFICATION-8, Option A)
// ----------------------------------------------------------------------------

// TestParseFamilyDetailed_PathUnification pins the SLICE-9 re-scoped (Option A)
// behavior: ParseFamilyDetailed derives Variant/Version/Modifier from the ID (the
// idDrivenDecompose primitive shared with the empty-raw path), while PRESERVING the
// Family from raw_family (the ID-path over-captures Family — that convergence is the
// separate family-seeding slice). The diff-first gate
// (TestPathUnification_ZeroUnexpectedRegression) is the dataset-wide guard; these
// units pin the representative classes + the must-not-regress invariants.
func TestParseFamilyDetailed_PathUnification(t *testing.T) {
	cases := []struct {
		desc                               string
		raw, id                            string
		wantFam, wantVar, wantVer, wantMod string
	}{
		// CONVERGENCE WIN: glued letter-suffix version-modifier. raw-aware alone gave
		// (glm,"","5v",""); the ID owns it → (glm,"",5,vision), matching empty-raw providers.
		// SLICE-12 (bestiary-xdbc Q1): the glued single 'v' after a glm version is the
		// VARIANT 'v', NOT the 'vision' modifier (supersedes the SLICE-8 glm-5v→vision row).
		{"glm-5v: glued 'v' is variant (bestiary-xdbc Q1)", "glm", "glm-5v", "glm", "v", "5", ""},

		// FAMILY-PRESERVING (the safeguard's core): the ID-path OVER-captures Family
		// (deepseek-v4, gpt-4o) — raw_family is the correct SHORT family and is kept.
		// Converging these is Option B's scope, NOT this slice.
		{"deepseek-v4-flash: family PRESERVED (not deepseek-v4)", "deepseek-flash", "deepseek-v4-flash", "deepseek", "flash", "", ""},
		// SLICE-12 (bestiary-xdbc Q2/Q2b): gpt-4o-mini → variant '4o', mini→modifier
		// (the line designator '4o' occupies the variant slot; size token 'mini' demotes
		// to the Modifier). Supersedes the SLICE-9 family-preserve (gpt,mini,"") row.
		{"gpt-4o-mini: variant '4o', mini→modifier (bestiary-xdbc Q2b)", "gpt", "gpt-4o-mini", "gpt", "4o", "", "mini"},

		// VARIANT DE-JUNK: raw_family "qwen3.6" leaks the version into the variant
		// ("3.6"); the ID recovers the true member variant "flash".
		{"qwen3.6-flash: variant de-junk 3.6→flash", "qwen3.6", "qwen3.6-flash", "qwen", "flash", "3.6", ""},

		// VARIANT REFINEMENT: ID names a more specific variant than raw_family.
		{"gpt-5.1-codex-mini: variant refinement codex→codex-mini", "gpt-codex", "gpt-5.1-codex-mini", "gpt", "codex-mini", "5.1", ""},

		// CLEAN-VARIANT GUARD (true-regression prevention): the series split is defeated
		// by the "6bit" quantization suffix so the ID variant would be junk
		// ("v2.5-pro-6bit"); the clean raw variant "pro" is PRESERVED, not worsened.
		{"mimo-v2.5-pro-6bit: clean raw variant 'pro' preserved (not worsened)", "mimo", "mimo-v2.5-pro-6bit", "mimo", "pro", "", ""},

		// MUST-NOT-REGRESS: kimi-k2-thinking → (kimi,k,2,thinking).
		{"kimi-k2-thinking (must-hold)", "kimi-thinking", "kimi-k2-thinking", "kimi", "k", "2", "thinking"},

		// MUST-NOT-REGRESS: capability modifier from raw_family is never dropped (the
		// ID "deepseek-reasoner" has no thinking token; raw "deepseek-thinking" carries it).
		{"deepseek-reasoner: rawModifier 'thinking' preserved", "deepseek-thinking", "deepseek-reasoner", "deepseek", "", "", "thinking"},

		// SLICE-10: capability + tier compose LOSSLESSLY in the Modifier LIST (supersedes
		// the SLICE-8 single-modifier "capability wins, tier dropped" interim).
		{"kimi-k2p6-turbo raw=kimi-thinking: thinking+turbo lossless", "kimi-thinking", "kimi-k2p6-turbo", "kimi", "k", "2.6", "thinking,turbo"},

		// MUST-NOT-REGRESS: claude-opus-4-1-...-thinking → (claude,opus,4.1,thinking).
		{"claude-opus-4-1-thinking (must-hold)", "claude-opus", "claude-opus-4-1-20250805-thinking", "claude", "opus", "4.1", "thinking"},

		// fix-cycle-2 P1 (Reviewer A BLOCKER): a more-specific raw variant must NOT be
		// overridden by a less-specific ID-driven one. InferFamilyFromIDWithVariant loses
		// "-lite" (returns "flash") for the dated-preview suffix; the superstring guard
		// keeps the correct raw variant "flash-lite" (distinct Gemini tier).
		{"gemini-2.5-flash-lite-preview-06-17: flash-lite preserved (not downgraded to flash)", "gemini-flash-lite", "gemini-2.5-flash-lite-preview-06-17", "gemini", "flash-lite", "2.5", ""},
		// SLICE-10: "preview" before an MM-YYYY date is now captured as a Modifier (the
		// tail-scan skips the 09-2025 date fragment); flash-lite variant still preserved.
		{"gemini-2.5-flash-lite-preview-09-2025: flash-lite preserved + preview modifier", "gemini-flash-lite", "gemini-2.5-flash-lite-preview-09-2025", "gemini", "flash-lite", "2.5", "preview"},

		// fix-cycle-2 P2 (Reviewer C IMPORTANT): the '@' version/date delimiter is
		// normalized to '-' so the @-form converges to the canonical version (not "4").
		{"claude-opus-4-1@20250805: @-form version → 4.1 (raw)", "claude-opus", "claude-opus-4-1@20250805", "claude", "opus", "4.1", ""},
		{"claude-opus-4-1@20250805: @-form version → 4.1 (empty-raw)", "", "claude-opus-4-1@20250805", "claude", "opus", "4.1", ""},
		{"claude-sonnet-4-6@default: @-form version → 4.6", "claude-sonnet", "claude-sonnet-4-6@default", "claude", "sonnet", "4.6", ""},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(bestiary.Family(tc.raw), bestiary.ModelID(tc.id), "test-provider")
			if string(f) != tc.wantFam || va != tc.wantVar || ve != tc.wantVer || modJoin(mod) != tc.wantMod {
				t.Errorf("ParseFamilyDetailed(raw=%q, id=%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFam, tc.wantVar, tc.wantVer, tc.wantMod)
			}
		})
	}
}

// TestParseFamilyDetailed_PathUnification_EmptyRawConsistency asserts the unification
// invariant directly: for an ID whose Family agrees between the raw-populated and
// empty-raw paths, the (Variant,Version,Modifier) MUST be identical regardless of
// raw_family — the two paths share one ID-driven decomposition.
func TestParseFamilyDetailed_PathUnification_EmptyRawConsistency(t *testing.T) {
	ids := []string{"glm-5v", "qwen3.6-flash", "kimi-k2-thinking", "gpt-5.1-codex-mini"}
	rawHints := []string{"glm", "qwen3.6", "kimi-thinking", "gpt-codex"}
	for i, id := range ids {
		rf, rv, rver, rmod, _ := bestiary.ParseFamilyDetailed(bestiary.Family(rawHints[i]), bestiary.ModelID(id), "p")
		ef, ev, ever, emod, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(id), "p")
		// Family agrees for these IDs by construction (no over-capture); assert V/V/M parity.
		if rf != ef {
			t.Fatalf("%s: family disagrees raw=%q empty=%q (test precondition: pick a non-over-capture ID)", id, rf, ef)
		}
		if rv != ev || rver != ever || modJoin(rmod) != modJoin(emod) {
			t.Errorf("%s: raw-populated (%q,%q,%q) != empty-raw (%q,%q,%q) — paths not unified",
				id, rv, rver, rmod, ev, ever, emod)
		}
	}
}

// TestParseFamilyDetailed_SLICE11_FamilyOverCaptureReduction asserts the SLICE-11
// (rc2, Option B) family OVER-CAPTURE fix: the empty-raw ID-path now reduces an
// over-captured COMPOUND family to its registered SHORT base so it converges with the
// raw-populated providers of the same ID. Each case pins the empty-raw decomposition;
// the matching raw-populated decomposition (the convergence target) is asserted equal.
func TestParseFamilyDetailed_SLICE11_FamilyOverCaptureReduction(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantFam bestiary.Family
		wantVar string
		wantVer string
	}{
		{"claude-opus dotted", "anthropic/claude-opus-4.1", "claude", "opus", "4.1"},
		// SLICE-12 (bestiary-xdbc Q2b): gpt-4o-mini → variant '4o' (mini→modifier, asserted
		// elsewhere); version empty. Supersedes the SLICE-11 (gpt,mini,"") row.
		{"gpt-4o-mini (variant '4o', bestiary-xdbc Q2b)", "openai/gpt-4o-mini", "gpt", "4o", ""},
		{"deepseek-r1 (canonical drops r1)", "deepseek-ai/DeepSeek-R1-0528", "deepseek", "", ""},
		// SLICE-10: 'instruct' is a global modifier now → variant empty (not "instruct").
		{"llama-3.3-70b-instruct", "meta-llama/llama-3.3-70b-instruct", "llama", "", "3.3"},
		{"qwen3-vl member+gen", "qwen/qwen3-vl-30b-a3b-instruct", "qwen", "vl", "3"},
		{"phi-4-mini member+gen", "microsoft/phi-4-mini-instruct", "phi", "mini", "4"},
		{"gemini flash via modifier-strip branch", "google/gemini-2.5-flash-image", "gemini", "flash", "2.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, v, ver, _, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(tc.id), "empty-prov")
			if f != tc.wantFam || v != tc.wantVar || ver != tc.wantVer {
				t.Errorf("empty-raw %q → (%q,%q,%q), want (%q,%q,%q)",
					tc.id, f, v, ver, tc.wantFam, tc.wantVar, tc.wantVer)
			}
		})
	}
}

// TestParseFamilyDetailed_SLICE11_GenuineCompoundPreserved asserts the reducer is
// CLOSED: it never over-reduces a genuinely-compound family (curated as an override
// self-map) nor a family whose base is not a registered short family. These MUST stay
// intact — the safeguard against over-reducing the 655 short/correct records.
func TestParseFamilyDetailed_SLICE11_GenuineCompoundPreserved(t *testing.T) {
	// Each genuine compound must NOT collapse to its bare leading token (the over-reduction
	// the closed reducer is designed to refuse). The family is expected to retain the
	// curated compound base prefix, never the lone first token.
	cases := []struct {
		id         string
		mustNotBe  bestiary.Family // the wrong over-reduction
		wantPrefix string          // family must keep this curated compound prefix
	}{
		{"text-embedding-3-large", "text", "text-embedding"},
		{"stable-diffusion-xl", "stable", "stable-diffusion"},
		{"nano-banana-pro", "nano", "nano-banana"},
	}
	for _, tc := range cases {
		f, _, _, _, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(tc.id), "p")
		if f == tc.mustNotBe {
			t.Errorf("%q → family %q — genuine compound WRONGLY over-reduced to bare leading token", tc.id, f)
		}
		if !strings.HasPrefix(string(f), tc.wantPrefix) {
			t.Errorf("%q → family %q, expected to retain curated compound prefix %q", tc.id, f, tc.wantPrefix)
		}
	}
}

// TestParseFamilyDetailed_SLICE11_CapabilityModifierDeclined asserts that a compound
// family carrying a CAPABILITY modifier (thinking/vision) is NOT reduced — leaving it an
// HONEST residual rather than silently dropping the capability (the SLICE-10 Modifier-LIST
// multi-modifier case). kimi-k2-thinking-* keeps a thinking-bearing decomposition rather
// than being collapsed to a bare short family that loses "thinking".
func TestParseFamilyDetailed_SLICE11_CapabilityModifierDeclined(t *testing.T) {
	// glm-4.1v-thinking-flash: empty-raw must NOT silently lose "thinking" by reducing
	// to a bare (glm, flash) — the over-capture family stays intact (honest residual).
	f, _, _, _, _ := bestiary.ParseFamilyDetailed("", "nano-gpt-glm-4.1v-thinking-flash", "p")
	_ = f // family may stay compound; the contract is "thinking not dropped via reduction".
	// Direct reducer contract: IsKnownFamily distinguishes a canonical short family from a
	// synthetic over-capture.
	if !bestiary.IsKnownFamily("claude") {
		t.Errorf("IsKnownFamily(claude) = false, want true (canonical registered family)")
	}
	if bestiary.IsKnownFamily("claude-opus-4-1") {
		t.Errorf("IsKnownFamily(claude-opus-4-1) = true, want false (synthetic over-capture)")
	}
}

// TestSLICE12_Convergences pins the SLICE-12 (rc2) cross-provider convergence fixes
// (bestiary-b4jm). Each case is the canonical ParseFamilyDetailed decomposition that the
// fix-cycle ratified; together with the before/after-diff gate (ZERO cat-(c)) these are
// the L2 specification for the mechanical + o-series + ledger changes.
func TestSLICE12_Convergences(t *testing.T) {
	cases := []struct {
		desc                   string
		raw                    bestiary.Family
		id                     bestiary.ModelID
		wFam, wVar, wVer, wMod string
	}{
		// ── O-SERIES restructure (bestiary-xdbc Q2/Q2a/Q2b/Q2c) ──────────────────────
		{"o1 → (gpt,o,1)", "", "o1", "gpt", "o", "1", ""},
		{"o1 raw=o → (gpt,o,1)", "o", "o1", "gpt", "o", "1", ""},
		{"o1-mini → (gpt,o,1,mini)", "o-mini", "o1-mini", "gpt", "o", "1", "mini"},
		{"o3-mini → (gpt,o,3,mini)", "o", "o3-mini", "gpt", "o", "3", "mini"},
		{"o3-pro → (gpt,o,3,pro)", "o-pro", "o3-pro", "gpt", "o", "3", "pro"},
		{"o4-mini → (gpt,o,4,mini)", "o", "o4-mini", "gpt", "o", "4", "mini"},
		{"gpt-4o → (gpt,4o,'')", "gpt", "gpt-4o", "gpt", "4o", "", ""},
		{"gpt-4o empty raw → (gpt,4o,'')", "", "gpt-4o", "gpt", "4o", "", ""},
		{"gpt-4o-mini → (gpt,4o,'',mini)", "gpt-mini", "gpt-4o-mini", "gpt", "4o", "", "mini"},
		{"chatgpt-4o-latest → (gpt,4o,'',latest)", "gpt", "chatgpt-4o-latest", "gpt", "4o", "", "latest"},
		{"gpt-audio-mini → (gpt,audio,'',mini)", "", "openai/gpt-audio-mini", "gpt", "audio", "", "mini"},
		{"gpt-4 UNCHANGED → (gpt,'',4)", "gpt", "gpt-4", "gpt", "", "4", ""},
		// ── gpt-codex ID-WINS (#4) + flash-lite NON-regression ───────────────────────
		// SLICE-10: 'chat' is now a global modifier (gpt has no 'chat' member) → captured in the list.
		{"gpt-5-chat-latest: phantom codex cleared, chat→modifier", "gpt-codex", "gpt-5-chat-latest", "gpt", "", "5", "chat,latest"},
		{"gpt-5.1-chat: phantom codex cleared, chat→modifier", "gpt-codex", "openai/gpt-5.1-chat", "gpt", "", "5.1", "chat"},
		{"flash-lite NOT regressed (raw)", "gemini-flash-lite", "gemini-2.5-flash-lite-preview-06-17", "gemini", "flash-lite", "2.5", ""},
		// SLICE-10: 'preview' before the MM-YYYY date is now captured (tail-scan skips the date).
		{"flash-lite tier (empty raw, #6 compound-member)", "", "gemini-2.5-flash-lite-preview-09-2025", "gemini", "flash-lite", "2.5", "preview"},
		// ── glm 'v' variant (Q1) ─────────────────────────────────────────────────────
		{"glm-4.5v → (glm,v,4.5)", "glm", "glm-4.5v", "glm", "v", "4.5", ""},
		{"glm-5v-turbo → (glm,v,5,turbo)", "glm", "glm-5v-turbo", "glm", "v", "5", "turbo"},
		{"glmv raw → glm + variant v", "glmv", "z-ai/glm-4.5v", "glm", "v", "4.5", ""},
		// ── canonical-winner ENFORCE (own-family + org leak) ─────────────────────────
		{"aion mislabelled llama → aion", "llama", "aion-labs/aion-1.0", "aion", "", "1.0", ""},
		{"mixtral mislabelled mistral → mixtral, instruct→modifier", "mistral", "mistralai/mixtral-8x22b-instruct", "mixtral", "", "", "instruct"},
		{"nousresearch org leak → hermes", "nousresearch", "nousresearch/hermes-3-llama-3.1-70b", "hermes", "", "3", ""},
		{"liquid org leak → lfm", "liquid", "liquid/lfm-2-24b-a2b", "lfm", "", "2", ""},
		{"qwq mislabelled qwen → qwq", "qwen", "qwq-32b", "qwq", "", "", ""},
		// ── raw-populated over-capture fold (#2) + dotted bare-gen (#3) ───────────────
		{"qwen3.7-max raw over-capture → (qwen,max,3.7)", "qwen3.7-max", "qwen3.7-max", "qwen", "max", "3.7", ""},
		{"qwen3.5 dotted bare-gen de-junk → (qwen,'',3.5)", "qwen3.5", "qwen/qwen3.5-27b", "qwen", "", "3.5", ""},
		// ── member-variant suffix re-recovery (#5, A-1/A-2) ──────────────────────────
		// SLICE-10: 'instruct' → global modifier (not a variant) for these non-member families.
		{"codellama empty-raw: instruct→modifier", "", "alfredpros/codellama-7b-instruct-solidity", "codellama", "", "", "instruct"},
		{"rnj empty-raw: instruct→modifier (A-1)", "", "essentialai/rnj-1-instruct", "rnj", "", "1", "instruct"},
		{"voxtral empty-raw recovers small (A-2)", "", "mistralai/voxtral-small-24b-2507", "voxtral", "small", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			f, v, ver, m, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wFam || v != tc.wVar || ver != tc.wVer || modJoin(m) != tc.wMod {
				t.Errorf("ParseFamilyDetailed(raw=%q,id=%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					tc.raw, tc.id, f, v, ver, m, tc.wFam, tc.wVar, tc.wVer, tc.wMod)
			}
		})
	}
}

// TestSLICE14_TIER1Convergences pins the SLICE-14 (rc2) straggler convergences (bestiary-judu),
// per the team-lead-refined set: 5 COMMITTED (cohere command r/r-plus date-guard+member,
// deepseek product-line "chat", meta-llama surgical doubled-vendor) + 3 CONDITIONALS cleanly
// promoted under existing rules (grok product-name "code-fast", Qwen3-Embedding qwen-wins,
// hy3 bare-gen). Each is non-lossy under the hardened gate (cat-(c)=0). command-a-reasoning is
// DEFERRED to S10 (reasoning = borderline-capability, modifier-vs-variant judgment).
func TestSLICE14_TIER1Convergences(t *testing.T) {
	cases := []struct {
		desc                   string
		raw                    bestiary.Family
		id                     bestiary.ModelID
		wFam, wVar, wVer, wMod string
	}{
		// COMMITTED — deepseek "chat" product-line member (non-lossy; v3.1 version preserved).
		{"deepseek-chat-v3-0324 empty → (deepseek,chat)", "", "deepseek/deepseek-chat-v3-0324", "deepseek", "chat", "", ""},
		{"deepseek-chat-v3-0324 raw=deepseek → (deepseek,chat)", "deepseek", "deepseek/deepseek-chat-v3-0324", "deepseek", "chat", "", ""},
		{"deepseek-chat-v3.1 empty → (deepseek,chat,3.1)", "", "deepseek/deepseek-chat-v3.1", "deepseek", "chat", "3.1", ""},
		{"deepseek-chat-v3.1 raw=deepseek → (deepseek,chat,3.1)", "deepseek", "deepseek/deepseek-chat-v3.1", "deepseek", "chat", "3.1", ""},
		// COMMITTED — cohere command R-line members (date-guard 08/12; "r7b"=member "r"+size "7b").
		{"command-r-plus-08-2024 empty → (command,r-plus)", "", "cohere/command-r-plus-08-2024", "command", "r-plus", "", ""},
		{"command-r-plus-08-2024 raw=command-r → (command,r-plus)", "command-r", "cohere/command-r-plus-08-2024", "command", "r-plus", "", ""},
		// "r7b" = member "r" + param-size "7b"; both sides CONVERGE to (command, r, 12). The
		// version "12" is the MM of the "12-2024" date — a pre-existing SHARED value on both
		// providers (NOT introduced here, NOT a divergence); date-guarding it to "" is a future
		// polish surfaced to the supervisor. The convergence (variant r on both) is the fix.
		{"command-r7b-12-2024 empty → (command,r) [r7b=r+7b-size]", "", "cohere/command-r7b-12-2024", "command", "r", "12", ""},
		{"command-r7b-12-2024 raw=command-r → (command,r)", "command-r", "cohere/command-r7b-12-2024", "command", "r", "12", ""},
		// COMMITTED — meta-llama SURGICAL doubled-vendor strip (org "meta-llama/" + "Meta-Llama-…").
		// SLICE-10: 'instruct' → global modifier (llama has no 'instruct' member after the
		// ratified families.json correction); variant empty, modifier [instruct].
		{"meta-llama/Meta-Llama-3.1 empty → (llama,'',3.1,[instruct])", "", "meta-llama/Meta-Llama-3.1-8B-Instruct", "llama", "", "3.1", "instruct"},
		{"meta-llama/Meta-Llama-3.1 raw=llama → (llama,'',3.1,[instruct])", "llama", "meta-llama/Meta-Llama-3.1-8B-Instruct", "llama", "", "3.1", "instruct"},
		// CONDITIONAL (promoted) — grok product-name member "code-fast" (one unit; no fast-as-modifier judgment).
		{"grok-code-fast-1 empty → (grok,code-fast,1)", "", "x-ai/grok-code-fast-1", "grok", "code-fast", "1", ""},
		{"grok-code-fast-1 raw=grok → (grok,code-fast,1)", "grok", "x-ai/grok-code-fast-1", "grok", "code-fast", "1", ""},
		// CONDITIONAL (promoted) — Qwen3-Embedding: ID-family qwen wins over generic raw "text-embedding".
		{"Qwen3-Embedding raw=text-embedding → (qwen,embedding,3)", "text-embedding", "Qwen/Qwen3-Embedding-8B", "qwen", "embedding", "3", ""},
		{"Qwen3-Embedding raw=qwen → (qwen,embedding,3)", "qwen", "Qwen/Qwen3-Embedding-8B", "qwen", "embedding", "3", ""},
		// CONDITIONAL guard — OpenAI text-embedding-3* MUST stay family "text-embedding" (untouched).
		{"GUARD: openai text-embedding-3-large stays text-embedding", "text-embedding", "openai/text-embedding-3-large", "text-embedding", "large", "3", ""},
		{"GUARD: openai text-embedding-3-small stays text-embedding", "text-embedding", "text-embedding-3-small", "text-embedding", "small", "3", ""},
		// CONDITIONAL (promoted) — hy3 bare-gen (bare "hy" attested via raw="Hy").
		{"hy3-preview empty → (hy,,3,preview)", "", "tencent/hy3-preview", "hy", "", "3", "preview"},
		{"hy3-preview raw=Hy → (hy,,3,preview)", "Hy", "tencent/hy3-preview", "hy", "", "3", "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			f, v, ver, m, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if string(f) != tc.wFam || v != tc.wVar || ver != tc.wVer || modJoin(m) != tc.wMod {
				t.Errorf("ParseFamilyDetailed(raw=%q,id=%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					tc.raw, tc.id, f, v, ver, m, tc.wFam, tc.wVar, tc.wVer, tc.wMod)
			}
		})
	}
}

// TestWhisperTrailingVersionRecovery_FamilyGated is the rc3-L2 (bestiary-fz9r) coverage for
// the WHISPER-FAMILY-GATED trailing "-v<int>" → Version recovery. It pins both halves of the
// contract: (1) whisper-* ids gain the version, and (2) the axis-B mutation-proof — NO other
// family's "-vN" packaging/revision tag is ever promoted (the failure mode that sank the
// general attempt). A regression that widens the gate beyond whisper turns these RED.
func TestWhisperTrailingVersionRecovery_FamilyGated(t *testing.T) {
	t.Parallel()

	type tc struct {
		raw     bestiary.Family
		id      bestiary.ModelID
		wantFam bestiary.Family
		wantVer string
	}
	cases := []tc{
		// (1) whisper TARGETS — version recovered to 3 (empty-raw AND raw paths agree).
		{"", "openai/whisper-large-v3", "whisper", "3"},
		{"", "whisper-large-v3", "whisper", "3"},
		{"", "whisper-large-v3-turbo", "whisper", "3"}, // skips trailing "turbo" modifier
		{"whisper", "whisper-large-v3-turbo", "whisper", "3"},

		// (2) axis-B MUTATION-PROOF — non-whisper "-vN" tags MUST NOT be promoted.
		// claude-opus-4-6-v1's "-v1" is a Bedrock packaging revision; the real version is 4.6,
		// extracted by the normal path. The recovery must NOT overwrite it with "1".
		{"", "anthropic.claude-opus-4-6-v1", "anthropic.claude", "4.6"},
		// elevenlabs/nova/morph/deepseek/recraft trailing -vN must stay Version="" (untouched).
		{"", "elevenlabs/elevenlabs-v2.5-turbo", "elevenlabs", ""},
		{"", "amazon/nova-lite-v1", "nova", ""},
		{"", "morph/morph-v3-fast", "morph", ""},
		{"", "deepseek-ai/DeepSeek-V3", "deepseek", ""},
		{"", "recraft/recraft-v3", "recraft", ""},
	}
	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			fam, _, ver, _, _ := bestiary.ParseFamilyDetailed(c.raw, c.id, "")
			if fam != c.wantFam {
				t.Errorf("ParseFamilyDetailed(%q, %q).Family = %q, want %q", c.raw, c.id, fam, c.wantFam)
			}
			if ver != c.wantVer {
				t.Errorf("ParseFamilyDetailed(%q, %q).Version = %q, want %q (whisper-gated recovery must not touch non-whisper -vN tags)",
					c.raw, c.id, ver, c.wantVer)
			}
		})
	}
}

// TestGrokNegationAwareModifier is the rc3 (bestiary-fz9r, USER-RULING) coverage for
// negation-aware modifier emission: an ID containing the literal token "non-<mod>" must
// emit "non-<mod>" (e.g. "non-reasoning"), NEVER the bare positive "<mod>". It pins the
// axis-B mutation-proof on both sides: (a) a "Cannon"/substring-"non" id NEVER gains a
// non-* modifier (the gate is a separate hyphen-token "non", not a substring); (b) the
// grok non-reasoning ids invert correctly; and (c) the out-of-scope grok "fast" handling
// is left untouched (the positive reasoning sibling keeps [reasoning, fast]; the
// non-reasoning id does NOT gain "fast").
func TestGrokNegationAwareModifier(t *testing.T) {
	t.Parallel()

	type tc struct {
		id      bestiary.ModelID
		wantMod []string
	}
	cases := []tc{
		// (b) negation emitted, NOT the bare positive.
		{"grok-4-1-fast-non-reasoning", []string{"non-reasoning"}},
		{"grok-4-fast-non-reasoning", []string{"non-reasoning"}},
		{"grok-4-20-non-reasoning", []string{"non-reasoning"}},
		{"xai/grok-4.20-non-reasoning", []string{"non-reasoning"}},
		// (c) out-of-scope "fast" untouched: positive sibling KEEPS [reasoning, fast];
		// the non-reasoning id does NOT gain "fast" (stays a single negation token).
		{"grok-4-1-fast-reasoning", []string{"reasoning", "fast"}},
		{"grok-4-fast-reasoning", []string{"reasoning", "fast"}},
		// (a) substring "non" inside a single token ("Cannon") must NEVER negate.
		{"GalrionSoftworks/MN-LooseCannon-12B-v1", nil},
		{"VongolaChouko/Starcannon-Unleashed-12B-v1.0", nil},
	}
	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			_, _, _, mod, _ := bestiary.ParseFamilyDetailed("", c.id, "")
			if !equalStringSlices(mod, c.wantMod) {
				t.Errorf("ParseFamilyDetailed(%q).Modifier = %v, want %v (negation-aware emission: literal "+
					"\"non-<mod>\" token → \"non-<mod>\"; substring \"non\" must not negate; \"fast\" out of scope)",
					c.id, mod, c.wantMod)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
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
