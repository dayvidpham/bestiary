package bestiary_test

import (
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
		{"deepseek-thinking", "deepseek", "thinking"},
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
		{"grok-vision", "grok", "vision"},
		{"hy3-free", "hy3", "free"},
		{"kat-coder", "kat", "coder"},
		{"kimi-free", "kimi", "free"},
		{"kimi-thinking", "kimi", "thinking"},
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		raw := raw
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
		raw := raw
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
		// All 30 suffix entries from variant_suffixes.json (listed longest-first
		// in the JSON, but initParseData re-sorts by length so the order here
		// is documentary only).
		{"deep-research", "widget-deep-research", "widget", "deep-research"},
		{"codex-spark", "acme-codex-spark", "acme", "codex-spark"},
		{"codex-mini", "baz-codex-mini", "baz", "codex-mini"},
		{"flash-lite", "acme-flash-lite", "acme", "flash-lite"},
		{"codex", "acme-codex", "acme", "codex"},
		{"thinking", "acme-thinking", "acme", "thinking"},
		{"instruct", "acme-instruct", "acme", "instruct"},
		{"vision", "acme-vision", "acme", "vision"},
		{"embed", "acme-embed", "acme", "embed"},
		{"embedding", "acme-embedding", "acme", "embedding"},
		{"mini", "foo-mini", "foo", "mini"},
		{"pro", "foo-pro", "foo", "pro"},
		{"flash", "foo-flash", "foo", "flash"},
		{"lite", "foo-lite", "foo", "lite"},
		{"turbo", "foo-turbo", "foo", "turbo"},
		{"base", "foo-base", "foo", "base"},
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		tc := tc
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
		{"gemini-2.5-flash",         "gemini-2.5-flash",         "gemini",      "2.5"},
		{"claude-opus no version",   "claude-opus",              "claude-opus", ""},

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
		tc := tc
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
		raw := raw
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
		tc := tc
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
