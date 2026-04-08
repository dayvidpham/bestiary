package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModelInfo_Ref verifies that Ref() extracts the correct 5-tuple from ModelInfo.
// Family and Variant must always be empty strings (deferred to normalization epoch).
func TestModelInfo_Ref(t *testing.T) {
	m := bestiary.ModelInfo{
		Provider:    bestiary.ProviderAnthropic,
		Family:      "claude-opus",
		ReleaseDate: "2025-05-14",
	}
	ref := m.Ref()

	if ref.Provider != bestiary.ProviderAnthropic {
		t.Errorf("Ref().Provider = %q, want %q", ref.Provider, bestiary.ProviderAnthropic)
	}
	if ref.RawFamily != "claude-opus" {
		t.Errorf("Ref().RawFamily = %q, want %q", ref.RawFamily, "claude-opus")
	}
	if ref.Family != "" {
		t.Errorf("Ref().Family = %q, want empty string (deferred to normalization epoch)", ref.Family)
	}
	if ref.Variant != "" {
		t.Errorf("Ref().Variant = %q, want empty string (deferred to normalization epoch)", ref.Variant)
	}
	if ref.Date != "2025-05-14" {
		t.Errorf("Ref().Date = %q, want %q", ref.Date, "2025-05-14")
	}
}

// TestModelInfo_Ref_EmptyFields verifies that Ref() handles zero-value ModelInfo fields
// gracefully, producing a ModelRef with empty strings and the zero Provider.
func TestModelInfo_Ref_EmptyFields(t *testing.T) {
	var m bestiary.ModelInfo
	ref := m.Ref()

	if ref.Provider != "" {
		t.Errorf("Ref().Provider = %q, want empty string for zero-value ModelInfo", ref.Provider)
	}
	if ref.RawFamily != "" {
		t.Errorf("Ref().RawFamily = %q, want empty string for zero-value ModelInfo", ref.RawFamily)
	}
	if ref.Family != "" {
		t.Errorf("Ref().Family = %q, want empty string", ref.Family)
	}
	if ref.Variant != "" {
		t.Errorf("Ref().Variant = %q, want empty string", ref.Variant)
	}
	if ref.Date != "" {
		t.Errorf("Ref().Date = %q, want empty string for zero-value ModelInfo", ref.Date)
	}
}

// TestProvidersForFamily_CrossProvider verifies that a known family returns at least
// the provider that hosts it. "claude-opus" must include ProviderAnthropic.
func TestProvidersForFamily_CrossProvider(t *testing.T) {
	providers := bestiary.ProvidersForFamily("claude-opus")

	if len(providers) == 0 {
		t.Fatal("ProvidersForFamily(\"claude-opus\") returned empty slice, want at least ProviderAnthropic")
	}

	found := false
	for _, p := range providers {
		if p == bestiary.ProviderAnthropic {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ProvidersForFamily(\"claude-opus\") = %v, want to include ProviderAnthropic", providers)
	}

	// Verify no duplicates in the returned slice.
	seen := make(map[bestiary.Provider]struct{}, len(providers))
	for _, p := range providers {
		if _, dup := seen[p]; dup {
			t.Errorf("ProvidersForFamily(\"claude-opus\") contains duplicate provider %q", p)
		}
		seen[p] = struct{}{}
	}
}

// TestProvidersForFamily_NotFound verifies that an unknown family returns a nil/empty slice.
func TestProvidersForFamily_NotFound(t *testing.T) {
	providers := bestiary.ProvidersForFamily("nonexistent-family")

	if len(providers) != 0 {
		t.Errorf("ProvidersForFamily(\"nonexistent-family\") = %v, want empty/nil slice", providers)
	}
}
