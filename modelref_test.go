package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModelInfo_Ref verifies that Ref() populates all 6 fields correctly.
// Family and Variant come from the normalized fields; Date from NormalizedDate.
func TestModelInfo_Ref(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:                "claude-opus-4-20250514",
		Provider:          bestiary.ProviderAnthropic,
		Family:            "claude-opus",
		NormalizedFamily:  "claude",
		NormalizedVariant: "opus",
		NormalizedDate:    "2025-05-14",
		ReleaseDate:       "2025-05-14",
	}
	ref := m.Ref()

	if ref.ID != "claude-opus-4-20250514" {
		t.Errorf("Ref().ID = %q, want %q", ref.ID, "claude-opus-4-20250514")
	}
	if ref.Provider != bestiary.ProviderAnthropic {
		t.Errorf("Ref().Provider = %q, want %q", ref.Provider, bestiary.ProviderAnthropic)
	}
	if ref.RawFamily != "claude-opus" {
		t.Errorf("Ref().RawFamily = %q, want %q", ref.RawFamily, "claude-opus")
	}
	if ref.Family != "claude" {
		t.Errorf("Ref().Family = %q, want %q (normalized family)", ref.Family, "claude")
	}
	if ref.Variant != "opus" {
		t.Errorf("Ref().Variant = %q, want %q (normalized variant)", ref.Variant, "opus")
	}
	if ref.Date != "2025-05-14" {
		t.Errorf("Ref().Date = %q, want %q", ref.Date, "2025-05-14")
	}
}

// TestModelInfo_Ref_EmptyFields verifies that Ref() handles zero-value ModelInfo
// gracefully, producing a ModelRef with empty strings and the zero Provider.
func TestModelInfo_Ref_EmptyFields(t *testing.T) {
	var m bestiary.ModelInfo
	ref := m.Ref()

	if ref.ID != "" {
		t.Errorf("Ref().ID = %q, want empty string for zero-value ModelInfo", ref.ID)
	}
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

// TestModelRef_HasID verifies that a static model's Ref() populates ID correctly.
func TestModelRef_HasID(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Fatal("StaticModels() returned empty slice")
	}
	// Check every model; all should have non-empty IDs.
	for _, m := range models {
		ref := m.Ref()
		if ref.ID != m.ID {
			t.Errorf("model %q: Ref().ID = %q, want %q", m.ID, ref.ID, m.ID)
		}
	}
}

// TestFormatRaw_UsesID verifies that Format(SchemeRaw) returns string(r.ID).
func TestFormatRaw_UsesID(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemeRaw)
	if got != "claude-opus-4-20250514" {
		t.Errorf("Format(SchemeRaw) = %q, want %q", got, "claude-opus-4-20250514")
	}
}

// TestFormatHuggingFace verifies that Format(SchemeHuggingFace) produces
// "<provider>/<raw-id>".
func TestFormatHuggingFace(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemeHuggingFace)
	want := "anthropic/claude-opus-4-20250514"
	if got != want {
		t.Errorf("Format(SchemeHuggingFace) = %q, want %q", got, want)
	}
}

// TestFormatPURL verifies that Format(SchemePURL) produces
// "pkg:huggingface/<provider>/<raw-id>".
func TestFormatPURL(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemePURL)
	want := "pkg:huggingface/anthropic/claude-opus-4-20250514"
	if got != want {
		t.Errorf("Format(SchemePURL) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_WithVariantAndDate verifies the full
// "<provider>/<family>/<variant>@<date>" form.
func TestFormatCanonical_WithVariantAndDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus@2025-05-14"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_WithVariantNoDate verifies "<provider>/<family>/<variant>"
// when Date is empty.
func TestFormatCanonical_WithVariantNoDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_FamilyOnly verifies "<provider>/<family>"
// when both Variant and Date are empty.
func TestFormatCanonical_FamilyOnly(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "gpt-4",
		Provider: bestiary.ProviderOpenAI,
		Family:   "gpt",
		Variant:  "",
		Date:     "",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "openai/gpt"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_EmptyFamily_FallsBackToRawID verifies that when Family
// is empty the canonical form falls back to "<provider>/<raw-id>".
func TestFormatCanonical_EmptyFamily_FallsBackToRawID(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "some-opaque-model-id",
		Provider: "custom-provider",
		Family:   "",
		Variant:  "",
		Date:     "",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "custom-provider/some-opaque-model-id"
	if got != want {
		t.Errorf("Format(SchemeCanonical) with empty Family = %q, want %q", got, want)
	}
}

// TestModelRef_String verifies that String() delegates to Format(SchemeCanonical).
func TestModelRef_String(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	if ref.String() != ref.Format(bestiary.SchemeCanonical) {
		t.Errorf("String() = %q, Format(SchemeCanonical) = %q, want equal",
			ref.String(), ref.Format(bestiary.SchemeCanonical))
	}
}

// TestModelRef_Designations_AllAdmitted verifies that every Designation
// returned by Designations() has Rating == AcceptabilityAdmitted.
func TestModelRef_Designations_AllAdmitted(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	designations := ref.Designations()
	if len(designations) == 0 {
		t.Fatal("Designations() returned empty slice")
	}
	for _, d := range designations {
		if d.Rating != bestiary.AcceptabilityAdmitted {
			t.Errorf("designation %q has Rating=%v, want AcceptabilityAdmitted",
				d.Value, d.Rating)
		}
	}
}

// TestModelRef_Designations_CoversSchemes verifies that all four CanonicalSchemes
// are represented in the Designations() output.
func TestModelRef_Designations_CoversSchemes(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	designations := ref.Designations()
	schemesSeen := make(map[bestiary.CanonicalScheme]bool)
	for _, d := range designations {
		schemesSeen[d.Scheme] = true
	}
	required := []bestiary.CanonicalScheme{
		bestiary.SchemeRaw,
		bestiary.SchemeCanonical,
		bestiary.SchemeHuggingFace,
		bestiary.SchemePURL,
	}
	for _, s := range required {
		if !schemesSeen[s] {
			t.Errorf("Designations() missing scheme %v", s)
		}
	}
}

// TestModelRef_Designations_StaticModels verifies the per-model-info invariant:
// every Designation from a static model's Ref().Designations() has Rating == Admitted.
func TestModelRef_Designations_StaticModels(t *testing.T) {
	models := bestiary.StaticModels()
	for _, m := range models {
		for _, d := range m.Ref().Designations() {
			if d.Rating != bestiary.AcceptabilityAdmitted {
				t.Errorf("model %q designation %q: Rating=%v, want AcceptabilityAdmitted",
					m.ID, d.Value, d.Rating)
			}
		}
	}
}

// TestProvidersForFamily_CrossProvider verifies that a known family returns at
// least the provider that hosts it.
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

// TestFormatCanonical_WithFamilyAndDate verifies "<provider>/<family>@<date>"
// when Variant is empty but Date is not.
func TestFormatCanonical_WithFamilyAndDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "gpt-4-2024-08-06",
		Provider: bestiary.ProviderOpenAI,
		Family:   "gpt",
		Variant:  "",
		Date:     "2024-08-06",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	// Family only + date: "openai/gpt@2024-08-06"
	if !strings.Contains(got, "openai") || !strings.Contains(got, "gpt") || !strings.Contains(got, "2024-08-06") {
		t.Errorf("Format(SchemeCanonical) = %q; want to contain provider, family, and date", got)
	}
}
