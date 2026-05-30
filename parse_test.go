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
		desc             string
		id               bestiary.ModelID
		family           bestiary.Family
		variant          string
		wantModifier     string
		wantConsumed     string
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
// Real-world case: moonshotai/kimi-k2-thinking with RawFamily="kimi-thinking".
// ParseFamily("kimi-thinking") → variant="thinking". The ID ends with "-thinking".
// Without the guard, ExtractModifier would return modifier="thinking" — double-count.
// With the guard, it returns ("","") because the trailing token equals the variant.
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
		desc            string
		rawID           bestiary.ModelID
		rawFamily       bestiary.Family
		wantModifier    string
		wantVersion     string
		wantDate        string
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
	// (1) model ID trailing token is NOT in knownModifierTokens, AND
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
	// NOTE: This positive-case subtest is currently SKIPPED because the code path is
	// unreachable by construction (see Reviewer 2's analysis in SLICE-FIX-V2-3 cycle-3).
	// Re-enable when FOLLOWUP bestiary-e9pi (parser reorder under FOLLOWUP_SLICE-1 bestiary-wi36) lands.
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
			// R3a (e9pi): this test is RE-ENABLED after the Step-5 bounded reorder in
			// ParseFamilyWithVersion. The reorder prevents the pure-fallback from absorbing
			// all trailing tokens, making ReasonUnknownSuffixOverflow reachable.
			// Input: rawFamily="claude-opus-4-1-extra-stuff-zen" — ParseFamilyWithVersion
			// now decomposes (claude, opus, 4.1) via hyphen-version; "extra-stuff-zen"
			// are unaccounted tokens (>2 threshold) → detectSuffixOverflow fires → "zen"
			// is unknown → ReasonUnknownSuffixOverflow.
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
		// Variant IS the modifier — suffix stripping already handled it.
		{"claude-thinking", "claude-thinking", "anthropic", "thinking is the parsed variant"},
		{"gpt-vision", "gpt-vision", "openai", "vision is the parsed variant"},
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
		desc           string
		id             bestiary.ModelID
		family         bestiary.Family
		variant        string
		wantVersion    string
		wantResidual   []string
	}{
		// Primary acceptance cases from L2 scope.
		{
			desc:    "gpt-5-mini → 5 (single numeric between family and variant)",
			id:      "gpt-5-mini",
			family:  "gpt",
			variant: "mini",
			wantVersion:  "5",
			wantResidual: nil,
		},
		{
			desc:    "claude-3-5-haiku-20241022 → 3.5 (N-M dot-join)",
			id:      "claude-3-5-haiku-20241022",
			family:  "claude",
			variant: "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:    "claude-3.5-haiku → 3.5 (dot-normalized in ID)",
			id:      "claude-3.5-haiku",
			family:  "claude",
			variant: "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:    "gemini-3-pro-preview → 3 (single numeric, variant=pro)",
			id:      "gemini-3-pro-preview",
			family:  "gemini",
			variant: "pro",
			wantVersion:  "3",
			wantResidual: nil,
		},
		{
			desc:    "gemini-3-1-pro-preview → 3.1 (N-M dot-join, variant=pro)",
			id:      "gemini-3-1-pro-preview",
			family:  "gemini",
			variant: "pro",
			wantVersion:  "3.1",
			wantResidual: nil,
		},
		{
			desc:    "nova-2-lite-v1 → version=2, residual=[v1] (R2 honest-audit)",
			id:      "nova-2-lite-v1",
			family:  "nova",
			variant: "lite",
			wantVersion:  "2",
			wantResidual: []string{"v1"},
		},
		{
			desc:    "nemotron-3-super-free → version=3, residual=[super] (R2 honest-audit)",
			id:      "nemotron-3-super-free",
			family:  "nemotron",
			variant: "free",
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
// R3b (eq7w): isYYMMDateToken tests
// --------------------------------------------------------------------------

// TestIsYYMMDateToken_Parity verifies that isYYMMDateToken parity holds with
// ExtractVersionFromID: tokens for which isYYMMDateToken is true must not be
// returned as versions.
// The direct unit test for isYYMMDateToken lives in parse_internal_test.go
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
					"  What: YYMM token was not rejected by isYYMMDateToken guard\n"+
					"  Why: ExtractVersionFromID must consult isYYMMDateToken before returning hyphen-digit tokens",
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
			// Trace 2: genuine-variant guard — kimi-k2-thinking (hypothetical empty raw_family).
			// exposed=kimi-k2 → cleaned=kimi-k2 → PFWV → (kimi-k2,"","") passthrough →
			// GUARD-2 declines (fProv == cleaned) → existing flow → variant=thinking preserved.
			// Key: the variant MUST be "thinking", not "". Family is "kimi-k2" (no over-strip).
			desc:        "kimi-k2-thinking-style passthrough → variant=thinking",
			id:          "kimi-k2-thinking",
			provider:    "moonshot",
			wantFamily:  "kimi-k2",
			wantVariant: "thinking",
			wantVersion: "",
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
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
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
			// GUARD-2 passthrough: empty rawFamily, id="kimi-k2-thinking".
			// InferFamilyFromIDWithVariant path: exposed="kimi-k2" (after trimOneTrailingModifier
			// strips -thinking), cleaned="kimi-k2" → PFWV → (kimi-k2,"","") passthrough →
			// GUARD-2 declines (fProv == cleaned "kimi-k2") → existing flow preserves variant=thinking.
			// ParseFamilyDetailed with rawFamily="" must preserve variant=thinking (not over-strip).
			desc:         "kimi-k2-thinking empty rawFamily → GUARD-2 preserves variant=thinking",
			rawFamily:    "",
			id:           "kimi-k2-thinking",
			provider:     "moonshot",
			wantFamily:   "kimi-k2",
			wantVariant:  "thinking",
			wantVersion:  "",
			wantModifier: "",
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
			if modifier != tc.wantModifier {
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
		desc      string
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
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
