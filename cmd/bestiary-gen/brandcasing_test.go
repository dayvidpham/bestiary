package main

import "testing"

// Brand-casing helper unit tests. Separate file from
// main_test.go (avoids colliding with the gate edits). Covers: curated brand
// stylization, un-curated title-case default, preserved multi-token acronyms, and the
// per-segment digit-handling difference between the two identifier builders.

// TestStyleSegment_Curated asserts the shared seam applies the curated brand table.
func TestStyleSegment_Curated(t *testing.T) {
	cases := []struct {
		tok  string
		want string
	}{
		// RATIFIED casings.
		{"nvidia", "Nvidia"},
		{"togetherai", "TogetherAI"},
		{"llmgateway", "LlmGateway"},
		{"iflowcn", "iFlowCN"},
		{"nearai", "NearAI"},
		{"gmicloud", "GMICloud"},
		// AUTO-APPLY batch.
		{"openrouter", "OpenRouter"},
		{"deepseek", "DeepSeek"},
		{"minimax", "MiniMax"},
		{"openai", "OpenAI"},
		{"deepinfra", "DeepInfra"},
		{"huggingface", "HuggingFace"},
		{"moonshotai", "MoonshotAI"},
		{"xai", "xAI"},
		{"github", "GitHub"},
		{"gitlab", "GitLab"},
		{"gpt", "GPT"},
		{"glm", "GLM"},
		{"qwen", "Qwen"},
		{"olmo", "OLMo"},
		{"internlm", "InternLM"},
		{"smollm", "SmolLM"},
		{"wizardlm", "WizardLM"},
		{"codellama", "CodeLlama"},
		// Case-insensitive on input.
		{"DeepSeek", "DeepSeek"},
		{"XAI", "xAI"},
	}
	for _, c := range cases {
		got, handled := styleSegment(c.tok, false)
		if got != c.want {
			t.Errorf("styleSegment(%q) = %q, want %q", c.tok, got, c.want)
		}
		if !handled {
			t.Errorf("styleSegment(%q) handled=false, want true (curated brand entries are definitive)", c.tok)
		}
	}
}

// TestStyleSegment_DefaultTitleCase asserts an un-curated token defaults to title-case
// and is reported NON-definitive (so slugToIdentifier may apply its name-hint).
func TestStyleSegment_DefaultTitleCase(t *testing.T) {
	cases := []struct{ tok, want string }{
		{"anthropic", "Anthropic"},
		{"google", "Google"},
		{"mistral", "Mistral"},
		{"foobar", "Foobar"},
		{"claude", "Claude"},
	}
	for _, c := range cases {
		got, handled := styleSegment(c.tok, false)
		if got != c.want {
			t.Errorf("styleSegment(%q) = %q, want %q (title-case default)", c.tok, got, c.want)
		}
		if handled {
			t.Errorf("styleSegment(%q) handled=true, want false (un-curated token is not definitive)", c.tok)
		}
	}
}

// TestStyleSegment_DigitLeading covers the per-segment digit-prefix rule, including the
// preserveDigitSuffix difference between the Model__ segment rule (verbatim "4o") and the
// slug identifier rule (title-case suffix).
func TestStyleSegment_DigitLeading(t *testing.T) {
	cases := []struct {
		tok      string
		preserve bool
		want     string
	}{
		{"302ai", false, "302AI"},        // curated suffix override wins regardless of preserve
		{"302ai", true, "302AI"},         //
		{"4o", true, "4o"},               // Model__ segment rule: verbatim suffix
		{"4o", false, "4O"},              // slug rule: title-case suffix
		{"123", true, "123"},             // all digits → verbatim
		{"3deepseek", true, "3DeepSeek"}, // curated alpha suffix after digit
	}
	for _, c := range cases {
		got, handled := styleSegment(c.tok, c.preserve)
		if got != c.want {
			t.Errorf("styleSegment(%q, preserve=%v) = %q, want %q", c.tok, c.preserve, got, c.want)
		}
		if !handled {
			t.Errorf("styleSegment(%q) handled=false, want true (digit-leading is definitive)", c.tok)
		}
	}
}

// TestTokenToConstPart_BrandAndCompound asserts the Model__ segment builder applies the
// brand table and preserves the compound-split underscore-join + verbatim digit suffix.
func TestTokenToConstPart_BrandAndCompound(t *testing.T) {
	cases := []struct{ tok, want string }{
		{"deepseek", "DeepSeek"},
		{"openrouter", "OpenRouter"},
		{"gpt", "GPT"},
		{"glm", "GLM"},
		{"4o", "4o"},                       // verbatim within-segment
		{"302ai", "302AI"},                 // curated suffix
		{"deep-research", "Deep_Research"}, // compound → underscore-join
		{"flash", "Flash"},                 // un-curated → title
	}
	for _, c := range cases {
		if got := tokenToConstPart(c.tok); got != c.want {
			t.Errorf("tokenToConstPart(%q) = %q, want %q", c.tok, got, c.want)
		}
	}
}

// TestSlugToIdentifier_BrandAndAcronyms asserts the Provider/Family symbol builder applies
// the brand table per-segment, preserves the existing multi-token acronyms (AIHubMix /
// AlibabaCN / AmazonBedrock), and keeps the name-hint fallback for un-curated tokens.
func TestSlugToIdentifier_BrandAndAcronyms(t *testing.T) {
	cases := []struct {
		slug, nameHint, want string
	}{
		// Curated single-token brands (no name-hint needed).
		{"togetherai", "", "TogetherAI"},
		{"huggingface", "", "HuggingFace"},
		{"deepinfra", "", "DeepInfra"},
		{"llmgateway", "", "LlmGateway"},
		{"iflowcn", "", "iFlowCN"},
		{"gmicloud", "", "GMICloud"},
		{"xai", "", "xAI"},
		{"nvidia", "", "Nvidia"},
		{"deepseek", "", "DeepSeek"},
		// PRESERVED multi-token acronyms (subsumed, not regressed).
		{"alibaba-cn", "Alibaba CN", "AlibabaCN"},
		{"amazon-bedrock", "Amazon Bedrock", "AmazonBedrock"},
		// Un-curated token → name-hint casing, else title-case.
		{"anthropic", "Anthropic", "Anthropic"},
		{"some-new-provider", "Some New Provider", "SomeNewProvider"},
	}
	for _, c := range cases {
		if got := slugToIdentifier(c.slug, c.nameHint); got != c.want {
			t.Errorf("slugToIdentifier(%q, %q) = %q, want %q", c.slug, c.nameHint, got, c.want)
		}
	}
}
