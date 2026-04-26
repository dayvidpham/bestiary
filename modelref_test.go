package bestiary_test

import (
	"encoding/json"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModelInfo_Ref verifies that Ref() populates all 7 fields correctly.
// Family, Variant, and Version come from the canonical fields; Date from Date.
func TestModelInfo_Ref(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:          "claude-opus-4-20250514",
		Provider:    bestiary.ProviderAnthropic,
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Version:     "",
		Date:        "2025-05-14",
		ReleaseDate: "2025-05-14",
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
	if ref.Version != "" {
		t.Errorf("Ref().Version = %q, want empty string", ref.Version)
	}
	if ref.Date != "2025-05-14" {
		t.Errorf("Ref().Date = %q, want %q", ref.Date, "2025-05-14")
	}
}

// TestModelInfo_Ref_WithVersion verifies that Ref() propagates Version.
func TestModelInfo_Ref_WithVersion(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:          "claude-opus-4-5-20251101",
		Provider:    bestiary.ProviderAnthropic,
		RawFamily:   "claude-opus-4-5",
		Family:      "claude",
		Variant:     "opus",
		Version:     "4.5",
		Date:        "2025-11-01",
		ReleaseDate: "2025-11-01",
	}
	ref := m.Ref()

	if ref.Variant != "opus" {
		t.Errorf("Ref().Variant = %q, want %q", ref.Variant, "opus")
	}
	if ref.Version != "4.5" {
		t.Errorf("Ref().Version = %q, want %q", ref.Version, "4.5")
	}
	if ref.Date != "2025-11-01" {
		t.Errorf("Ref().Date = %q, want %q", ref.Date, "2025-11-01")
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
	if ref.Version != "" {
		t.Errorf("Ref().Version = %q, want empty string for zero-value ModelInfo", ref.Version)
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
		Version:  "",
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
		Version:  "",
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
		Version:  "",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemePURL)
	want := "pkg:huggingface/anthropic/claude-opus-4-20250514"
	if got != want {
		t.Errorf("Format(SchemePURL) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_WithVariantAndDate verifies the full
// "<provider>/<family>/<variant>@<date>" form (no Version).
func TestFormatCanonical_WithVariantAndDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Version:  "",
		Date:     "2025-05-14",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus@2025-05-14"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_WithVariantNoDate verifies "<provider>/<family>/<variant>"
// when Date and Version are empty.
func TestFormatCanonical_WithVariantNoDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Version:  "",
		Date:     "",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// TestFormatCanonical_FamilyOnly verifies "<provider>/<family>"
// when Variant, Version, and Date are all empty.
func TestFormatCanonical_FamilyOnly(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "gpt-4",
		Provider: bestiary.ProviderOpenAI,
		Family:   "gpt",
		Variant:  "",
		Version:  "",
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
		Version:  "",
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
		Version:  "",
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
		Version:  "",
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
		Version:  "",
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
// when Variant and Version are empty but Date is not.
func TestFormatCanonical_WithFamilyAndDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "gpt-4-2024-08-06",
		Provider: bestiary.ProviderOpenAI,
		Family:   "gpt",
		Variant:  "",
		Version:  "",
		Date:     "2024-08-06",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "openai/gpt@2024-08-06"
	if got != want {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Tests for Version-bearing canonical formatting (B4 from slice spec).
// These tests FAIL until L3 (formatCanonical) is implemented.
// ---------------------------------------------------------------------------

// TestFormatCanonical_WithVersionAndDate verifies
// "<provider>/<family>/<variant>/<version>@<date>" — the primary UAT-2 case.
func TestFormatCanonical_WithVersionAndDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-5-20251101",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Version:  "4.5",
		Date:     "2025-11-01",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus/4.5@2025-11-01"
	if got != want {
		t.Errorf("Format(SchemeCanonical) with Version+Date = %q, want %q", got, want)
	}
}

// TestFormatCanonical_WithVersionNoDate verifies
// "<provider>/<family>/<variant>/<version>" when Date is empty.
func TestFormatCanonical_WithVersionNoDate(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-5",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Version:  "4.5",
		Date:     "",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "anthropic/claude/opus/4.5"
	if got != want {
		t.Errorf("Format(SchemeCanonical) with Version no Date = %q, want %q", got, want)
	}
}

// TestFormatCanonical_VersionOnlyNoVariant verifies
// "<provider>/<family>/<version>@<date>" when Variant is empty.
func TestFormatCanonical_VersionOnlyNoVariant(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "gemini-2.5-20251101",
		Provider: "google",
		Family:   "gemini",
		Variant:  "",
		Version:  "2.5",
		Date:     "2025-11-01",
	}
	got := ref.Format(bestiary.SchemeCanonical)
	want := "google/gemini/2.5@2025-11-01"
	if got != want {
		t.Errorf("Format(SchemeCanonical) Version-only no Variant = %q, want %q", got, want)
	}
}

// TestFormatCanonical_AllCombinations exercises the full combinatorial matrix
// for Version presence/absence with Variant and Date.
func TestFormatCanonical_AllCombinations(t *testing.T) {
	cases := []struct {
		name    string
		ref     bestiary.ModelRef
		want    string
	}{
		{
			// Variant set, Version set, Date set
			name: "variant+version+date",
			ref: bestiary.ModelRef{
				ID: "claude-opus-4-5-20251101", Provider: bestiary.ProviderAnthropic,
				Family: "claude", Variant: "opus", Version: "4.5", Date: "2025-11-01",
			},
			want: "anthropic/claude/opus/4.5@2025-11-01",
		},
		{
			// Variant set, Version set, no Date
			name: "variant+version-nodate",
			ref: bestiary.ModelRef{
				ID: "claude-opus-4-5", Provider: bestiary.ProviderAnthropic,
				Family: "claude", Variant: "opus", Version: "4.5", Date: "",
			},
			want: "anthropic/claude/opus/4.5",
		},
		{
			// Variant set, no Version, Date set (existing behaviour preserved)
			name: "variant-noversion+date",
			ref: bestiary.ModelRef{
				ID: "claude-opus-4-20250514", Provider: bestiary.ProviderAnthropic,
				Family: "claude", Variant: "opus", Version: "", Date: "2025-05-14",
			},
			want: "anthropic/claude/opus@2025-05-14",
		},
		{
			// Variant set, no Version, no Date
			name: "variant-noversion-nodate",
			ref: bestiary.ModelRef{
				ID: "claude-opus", Provider: bestiary.ProviderAnthropic,
				Family: "claude", Variant: "opus", Version: "", Date: "",
			},
			want: "anthropic/claude/opus",
		},
		{
			// No Variant, Version set, Date set
			name: "novariant+version+date",
			ref: bestiary.ModelRef{
				ID: "gemini-2.5-20251101", Provider: "google",
				Family: "gemini", Variant: "", Version: "2.5", Date: "2025-11-01",
			},
			want: "google/gemini/2.5@2025-11-01",
		},
		{
			// No Variant, Version set, no Date
			name: "novariant+version-nodate",
			ref: bestiary.ModelRef{
				ID: "gemini-2.5", Provider: "google",
				Family: "gemini", Variant: "", Version: "2.5", Date: "",
			},
			want: "google/gemini/2.5",
		},
		{
			// No Variant, no Version, Date set (existing behaviour preserved)
			name: "novariant-noversion+date",
			ref: bestiary.ModelRef{
				ID: "gpt-4-2024-08-06", Provider: bestiary.ProviderOpenAI,
				Family: "gpt", Variant: "", Version: "", Date: "2024-08-06",
			},
			want: "openai/gpt@2024-08-06",
		},
		{
			// No Variant, no Version, no Date
			name: "family-only",
			ref: bestiary.ModelRef{
				ID: "gpt-4", Provider: bestiary.ProviderOpenAI,
				Family: "gpt", Variant: "", Version: "", Date: "",
			},
			want: "openai/gpt",
		},
		{
			// Empty Family: fall back to raw-ID
			name: "empty-family-fallback",
			ref: bestiary.ModelRef{
				ID: "opaque-model", Provider: "custom",
				Family: "", Variant: "", Version: "", Date: "",
			},
			want: "custom/opaque-model",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.ref.Format(bestiary.SchemeCanonical)
			if got != tc.want {
				t.Errorf("Format(SchemeCanonical) = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestFormatCanonical_StaticRegistry_Claude_Opus_4_5 is an integration test
// that pulls claude-opus-4-5-20251101 from the static registry (populated by
// go generate ./... via codegen) and asserts the canonical form includes the
// version "4.5". This verifies the end-to-end production code path:
// codegen → models_static_gen.go Version → Ref() → formatCanonical.
//
// Before cycle-2 fix: formatted as "anthropic/claude/opus@2025-11-01" (no version).
// After cycle-2 fix: formatted as "anthropic/claude/opus/4.5@2025-11-01".
//
// This test asserts the BLOCKER resolution from bestiary-5eh8.
func TestFormatCanonical_StaticRegistry_Claude_Opus_4_5(t *testing.T) {
	const targetID = "claude-opus-4-5-20251101"
	const wantCanonical = "anthropic/claude/opus/4.5@2025-11-01"

	m, found := bestiary.LookupModelByProvider(bestiary.ProviderAnthropic, targetID)
	if !found {
		// The model may only be present under a third-party provider in this
		// epoch. Fall back to any provider that carries this ID.
		m2, found2 := bestiary.LookupModel(bestiary.ModelID(targetID))
		if !found2 {
			t.Skipf("model %q not found in static registry — run go generate ./... to refresh", targetID)
		}
		m = m2
	}

	ref := m.Ref()
	if ref.Version == "" {
		t.Errorf("Ref().Version is empty for %q — Version not populated by codegen; want version extracted from model ID", targetID)
	}
	if ref.Version != "4.5" {
		t.Errorf("Ref().Version = %q, want %q", ref.Version, "4.5")
	}
	got := ref.Format(bestiary.SchemeCanonical)
	if got != wantCanonical {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", got, wantCanonical)
	}
}

// ----------------------------------------------------------------------------
// Modifier field tests on ModelRef (SLICE-FIX-V2-5)
// ----------------------------------------------------------------------------

// TestModelRef_Modifier_MarshalUnmarshal verifies that ModelRef with a populated
// Modifier field round-trips through JSON marshal/unmarshal correctly.
func TestModelRef_Modifier_MarshalUnmarshal(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:        "claude-opus-4-6-thinking",
		Provider:  "anthropic",
		RawFamily: "claude-opus",
		Family:    "claude",
		Variant:   "opus",
		Version:   "4.6",
		Date:      "2026-02-05",
		Modifier:  "thinking",
	}

	enc, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ModelRef with Modifier) failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(enc, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	// All 8 fields must be present.
	required := []string{"ID", "Provider", "RawFamily", "Family", "Variant", "Version", "Date", "Modifier"}
	for _, field := range required {
		if _, ok := got[field]; !ok {
			t.Errorf("ModelRef JSON missing required field %q", field)
		}
	}

	// Modifier must be "thinking".
	if modVal, ok := got["Modifier"]; !ok || modVal != "thinking" {
		t.Errorf("ModelRef JSON Modifier = %v, want \"thinking\"", modVal)
	}
}

// TestModelInfo_Ref_Modifier verifies that Ref() propagates Modifier from ModelInfo to ModelRef.
func TestModelInfo_Ref_Modifier(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:          "claude-opus-4-6-thinking",
		Provider:    bestiary.ProviderAnthropic,
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Version:     "4.6",
		Date:        "2026-02-05",
		Modifier:    "thinking",
	}

	ref := m.Ref()
	if ref.Modifier != "thinking" {
		t.Errorf("Ref().Modifier = %q, want %q", ref.Modifier, "thinking")
	}
}

// TestModelInfo_Ref_EmptyModifier verifies that zero-value Modifier propagates correctly.
func TestModelInfo_Ref_EmptyModifier(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:       "gpt-4o-2024-05-13",
		Provider: "openai",
		Family:   "gpt",
		Modifier: "",
	}

	ref := m.Ref()
	if ref.Modifier != "" {
		t.Errorf("Ref().Modifier = %q, want empty string for zero-value Modifier", ref.Modifier)
	}
}
