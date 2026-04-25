package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

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
		// m-prefix.
		{"minimax-m2.5 (not in overrides, fallthrough to pattern)", "minimax-m2.5", "minimax", "m2.5"},
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
func TestParseFamily_Determinism(t *testing.T) {
	t.Parallel()

	inputs := []bestiary.Family{
		"claude-opus", "kimi-k2.5", "qwen3.5", "gemini-flash-lite",
		"gpt-codex-spark", "claude-opus-4-5", "", "llama",
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
