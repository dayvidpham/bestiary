package bestiary

// resolve_groupkey_internal_test.go — group-key invariant internal unit tests for
// the selectRepresentative and parseContextN seams.
//
// These tests live in package bestiary (not bestiary_test) because they exercise
// unexported functions. They verify the pure seam logic independently of
// StaticModels(), so they are green without depending on the
// parse.go decomposition fixes.

import (
	"testing"
)

// --- parseContextN ---

// TestParseContextN verifies that parseContextN extracts the digit-only ":N"
// suffix from a raw model ID, returning "" when absent or non-digit.
func TestParseContextN(t *testing.T) {
	corpus := loadInternalCorpus[string, string](t, internalParseContextNCorpusJSON, 16)
	internalRequireInputCoverage(t, corpus, map[string]string{
		"claude-3-7-sonnet-thinking:1024": "1024",
		"claude-3-7-sonnet-thinking":      "",
		"foo:1a2b":                        "",
		":42":                             "42",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := parseContextN(ModelID(c.Input)); got != c.Expected {
				t.Errorf("parseContextN(%q) = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// --- selectRepresentative ---

// mkRef builds a ModelRef with just the fields relevant for selectRepresentative.
func mkRef(id string, provider Provider, family Family) ModelRef {
	return ModelRef{
		ID:       ModelID(id),
		Provider: provider,
		Family:   family,
	}
}

// TestSelectRepresentative_SingleItem returns the sole item unchanged.
func TestSelectRepresentative_SingleItem(t *testing.T) {
	r := mkRef("claude-opus-4-20250514", ProviderAnthropic, FamilyClaude)
	got := selectRepresentative([]ModelRef{r})
	if got.ID != r.ID || got.Provider != r.Provider {
		t.Errorf("selectRepresentative([single]) = %+v, want %+v", got, r)
	}
}

// TestSelectRepresentative_CanonicalProviderWins verifies that the canonical
// provider wins the tiebreak when present in the group.
func TestSelectRepresentative_CanonicalProviderWins(t *testing.T) {
	// FamilyClaude.CanonicalProvider() == ProviderAnthropic
	rehost1 := mkRef("claude-opus-4-20250514", "rehost-a", FamilyClaude)
	canonical := mkRef("claude-opus-4-20250514", ProviderAnthropic, FamilyClaude)
	rehost2 := mkRef("claude-opus-4-20250514", "rehost-b", FamilyClaude)

	// Order: rehost first, canonical in the middle, rehost at end.
	group := []ModelRef{rehost1, canonical, rehost2}
	got := selectRepresentative(group)
	if got.Provider != ProviderAnthropic {
		t.Errorf("selectRepresentative: canonical provider preference not applied; got Provider=%q, want %q",
			got.Provider, ProviderAnthropic)
	}
}

// TestSelectRepresentative_CanonicalProviderWins_LastPosition verifies that
// canonical-provider preference applies even when the canonical row is last.
func TestSelectRepresentative_CanonicalProviderWins_LastPosition(t *testing.T) {
	group := []ModelRef{
		mkRef("m", "zz-rehost", FamilyClaude),
		mkRef("m", "aa-rehost", FamilyClaude),
		mkRef("m", ProviderAnthropic, FamilyClaude),
	}
	got := selectRepresentative(group)
	if got.Provider != ProviderAnthropic {
		t.Errorf("canonical in last position: got Provider=%q, want %q", got.Provider, ProviderAnthropic)
	}
}

// TestSelectRepresentative_LexicographicFallback verifies that when no canonical
// provider is in the group (unknown family → CanonicalProvider returns ""),
// the lexicographically-smallest Provider string is selected.
func TestSelectRepresentative_LexicographicFallback(t *testing.T) {
	// Use a family with no canonical provider mapping (empty string returned).
	unknownFamily := Family("totally-unknown-family-xyz")

	group := []ModelRef{
		mkRef("m", "zz-provider", unknownFamily),
		mkRef("m", "bb-provider", unknownFamily),
		mkRef("m", "aa-provider", unknownFamily), // lexicographically smallest
		mkRef("m", "mm-provider", unknownFamily),
	}
	got := selectRepresentative(group)
	if got.Provider != "aa-provider" {
		t.Errorf("lexicographic fallback: got Provider=%q, want %q", got.Provider, "aa-provider")
	}
}

// TestSelectRepresentative_OrderIndependent verifies that selectRepresentative
// returns the same result regardless of input slice order (slice-order independence).
//
// Note: this test proves slice-order independence only. End-to-end map-iteration
// determinism in Resolve (the byGroup map) is covered by the integration-level
// codegen reproducibility test (TestCodegen_Reproducible_ByteIdentical with -shuffle).
func TestSelectRepresentative_OrderIndependent(t *testing.T) {
	unknownFamily := Family("another-unknown-family")

	base := []ModelRef{
		mkRef("m", "delta", unknownFamily),
		mkRef("m", "alpha", unknownFamily),
		mkRef("m", "gamma", unknownFamily),
		mkRef("m", "beta", unknownFamily),
	}

	// Run with several permutations; all must return "alpha" (lexicographically smallest).
	permutations := [][]ModelRef{
		{base[0], base[1], base[2], base[3]},
		{base[3], base[2], base[1], base[0]},
		{base[1], base[3], base[0], base[2]},
		{base[2], base[0], base[3], base[1]},
	}
	for i, perm := range permutations {
		got := selectRepresentative(perm)
		if got.Provider != "alpha" {
			t.Errorf("perm[%d]: selectRepresentative returned Provider=%q, want %q (order-independent result violated)",
				i, got.Provider, "alpha")
		}
	}
}

// TestSelectRepresentative_CanonicalAbsent_LexicographicWins verifies the
// lexicographic tiebreak when the canonical provider IS known but NOT present
// in this group (e.g., a claude model rehosted only by non-Anthropic providers).
func TestSelectRepresentative_CanonicalAbsent_LexicographicWins(t *testing.T) {
	// FamilyClaude.CanonicalProvider() == ProviderAnthropic, but anthropic is absent.
	group := []ModelRef{
		mkRef("m", "zz-rehost", FamilyClaude),
		mkRef("m", "bb-rehost", FamilyClaude),
		mkRef("m", "aa-rehost", FamilyClaude),
	}
	got := selectRepresentative(group)
	if got.Provider != "aa-rehost" {
		t.Errorf("canonical absent → lexicographic tiebreak: got Provider=%q, want %q",
			got.Provider, "aa-rehost")
	}
}

// --- isBareIdentifier ---

// TestIsBareIdentifier_RejectsHash verifies that isBareIdentifier returns false
// for strings containing '#', so sized entity keys (e.g. "llama@3.3#70b{instruct}")
// are not treated as bare family names for the fallback matching path.
func TestIsBareIdentifier_RejectsHash(t *testing.T) {
	corpus := loadInternalCorpus[string, bool](t, internalIsBareIdentifierCorpusJSON, 8)
	internalRequireInputCoverage(t, corpus, map[string]bool{
		"llama":         true,
		"llama#70b":     false,
		"llama@3.3#70b": false,
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := isBareIdentifier(c.Input); got != c.Expected {
				t.Errorf("isBareIdentifier(%q) = %v, want %v", c.Input, got, c.Expected)
			}
		})
	}
}
