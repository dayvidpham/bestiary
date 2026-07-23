package main

import "testing"

// Brand-casing helper unit tests. Separate file from
// main_test.go (avoids colliding with the gate edits). Covers: curated brand
// stylization, un-curated title-case default, preserved multi-token acronyms, and the
// per-segment digit-handling difference between the two identifier builders.

// TestStyleSegment_Curated asserts the shared seam applies the curated brand table.
func TestStyleSegment_Curated(t *testing.T) {
	corpus := loadGenCorpus[string, string](t, genStyleSegmentCuratedCorpusJSON, 26)
	genRequireInputCoverage(t, corpus, map[string]string{
		"xai":      "xAI",
		"olmo":     "OLMo",
		"XAI":      "xAI",
		"DeepSeek": "DeepSeek",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, handled := styleSegment(c.Input, false)
			if got != c.Expected {
				t.Errorf("styleSegment(%q) = %q, want %q", c.Input, got, c.Expected)
			}
			if !handled {
				t.Errorf("styleSegment(%q) handled=false, want true (curated brand entries are definitive)", c.Input)
			}
		})
	}
}

// TestStyleSegment_DefaultTitleCase asserts an un-curated token defaults to title-case
// and is reported NON-definitive (so slugToIdentifier may apply its name-hint).
func TestStyleSegment_DefaultTitleCase(t *testing.T) {
	corpus := loadGenCorpus[string, string](t, genStyleSegmentDefaultTitleCaseCorpusJSON, 5)
	genRequireInputCoverage(t, corpus, map[string]string{
		"anthropic": "Anthropic",
		"foobar":    "Foobar",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, handled := styleSegment(c.Input, false)
			if got != c.Expected {
				t.Errorf("styleSegment(%q) = %q, want %q (title-case default)", c.Input, got, c.Expected)
			}
			if handled {
				t.Errorf("styleSegment(%q) handled=true, want false (un-curated token is not definitive)", c.Input)
			}
		})
	}
}

// TestStyleSegment_DigitLeading covers the per-segment digit-prefix rule, including the
// preserveDigitSuffix difference between the Model__ segment rule (verbatim "4o") and the
// slug identifier rule (title-case suffix).
func TestStyleSegment_DigitLeading(t *testing.T) {
	corpus := loadGenCorpus[genStyleSegmentInput, string](t, genStyleSegmentDigitLeadingCorpusJSON, 6)
	genRequireInputCoverage(t, corpus, map[genStyleSegmentInput]string{
		{Token: "4o", Preserve: true}:  "4o",
		{Token: "4o", Preserve: false}: "4O",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got, handled := styleSegment(c.Input.Token, c.Input.Preserve)
			if got != c.Expected {
				t.Errorf("styleSegment(%q, preserve=%v) = %q, want %q", c.Input.Token, c.Input.Preserve, got, c.Expected)
			}
			if !handled {
				t.Errorf("styleSegment(%q) handled=false, want true (digit-leading is definitive)", c.Input.Token)
			}
		})
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
