package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestSlugToIdentifier verifies the slug-to-PascalCase conversion, including
// digit-leading slugs, casing overrides, and hyphen-separated tokens.
func TestSlugToIdentifier(t *testing.T) {
	cases := []struct {
		slug     string
		nameHint string
		want     string
	}{
		// Digit-leading slug: "302" stays verbatim; "ai" → "AI" via casingOverrides.
		{"302ai", "302AI", "302AI"},
		// Single-token casing override.
		{"xai", "xAI", "XAI"},
		// Multi-token with two overrides (SAP + AI).
		{"sap-ai-core", "SAP AI Core", "SAPAICore"},
		// Hyphenated without overrides — title-case each token.
		{"amazon-bedrock", "Amazon Bedrock", "AmazonBedrock"},
		// Simple single token.
		{"anthropic", "Anthropic", "Anthropic"},
		{"google", "Google", "Google"},
		// Multi-token with AI override.
		{"cloudflare-ai-gateway", "Cloudflare AI Gateway", "CloudflareAIGateway"},
		// AWS override.
		{"aws", "AWS", "AWS"},
		// openrouter — name hint "OpenRouter" provides the display casing.
		{"openrouter", "OpenRouter", "OpenRouter"},
	}

	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			got := slugToIdentifier(tc.slug, tc.nameHint)
			if got != tc.want {
				t.Errorf("slugToIdentifier(%q, %q) = %q, want %q", tc.slug, tc.nameHint, got, tc.want)
			}
		})
	}
}

// TestSlugToIdentifier_DigitLeadingVariants covers digit-alpha combinations.
func TestSlugToIdentifier_DigitLeadingVariants(t *testing.T) {
	cases := []struct {
		slug string
		name string
		want string
	}{
		{"302ai", "302AI", "302AI"},
		{"3ai", "3AI", "3AI"},
	}
	for _, tc := range cases {
		got := slugToIdentifier(tc.slug, tc.name)
		if got != tc.want {
			t.Errorf("slugToIdentifier(%q, %q) = %q, want %q", tc.slug, tc.name, got, tc.want)
		}
	}
}

// TestProviderConstName verifies that providerConstName produces valid Go identifiers.
func TestProviderConstName(t *testing.T) {
	cases := []struct {
		slug string
		name string
		want string
	}{
		{"302ai", "302AI", "Provider302AI"},
		{"xai", "xAI", "ProviderXAI"},
		{"sap-ai-core", "SAP AI Core", "ProviderSAPAICore"},
		{"amazon-bedrock", "Amazon Bedrock", "ProviderAmazonBedrock"},
		{"anthropic", "Anthropic", "ProviderAnthropic"},
		{"google", "Google", "ProviderGoogle"},
		{"cloudflare-ai-gateway", "Cloudflare AI Gateway", "ProviderCloudflareAIGateway"},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			got := providerConstName(tc.slug, tc.name)
			if got != tc.want {
				t.Errorf("providerConstName(%q, %q) = %q, want %q", tc.slug, tc.name, got, tc.want)
			}
		})
	}
}

// TestFilterFlags_MutualExclusivity verifies that providing both -only-providers
// and -all-providers-except returns an actionable error.
func TestFilterFlags_MutualExclusivity(t *testing.T) {
	_, err := parseFlags([]string{
		"-only-providers=anthropic",
		"-all-providers-except=openrouter",
	})
	if err == nil {
		t.Fatal("parseFlags: expected error for mutually exclusive flags, got nil")
	}
	msg := err.Error()
	// Error must be actionable: mention both flags and explain what to do.
	if !strings.Contains(msg, "-only-providers") {
		t.Errorf("error message %q missing '-only-providers'", msg)
	}
	if !strings.Contains(msg, "-all-providers-except") {
		t.Errorf("error message %q missing '-all-providers-except'", msg)
	}
	if !strings.Contains(msg, "mutually exclusive") {
		t.Errorf("error message %q missing 'mutually exclusive'", msg)
	}
}

// TestFilterFlags_ProviderInclusion verifies that -only-providers filters model
// data while leaving the provider constant list unaffected (tested via applyFilter).
//
// Filter asymmetry: in run(), allSlugs is populated from the full (unfiltered)
// model set and passed to generateProvidersSource BEFORE applyFilter is called.
// applyFilter only narrows the model data passed to generateSource (the static
// model list). The constant generation path is therefore independent of any
// filter; TestProviders_MinimumCount in provider_test.go asserts that all 110+
// provider constants are present regardless of any filter applied here.
func TestFilterFlags_ProviderInclusion(t *testing.T) {
	flags, err := parseFlags([]string{"-only-providers=anthropic,google"})
	if err != nil {
		t.Fatalf("parseFlags: unexpected error: %v", err)
	}
	if len(flags.only) != 2 {
		t.Fatalf("parseFlags: only = %v, want [anthropic google]", flags.only)
	}
	if len(flags.except) != 0 {
		t.Fatalf("parseFlags: except = %v, want []", flags.except)
	}
	if flags.only[0] != "anthropic" || flags.only[1] != "google" {
		t.Errorf("parseFlags: only = %v, want [anthropic google]", flags.only)
	}

	// Verify applyFilter actually excludes non-listed providers from model data.
	models := makeTestModels()
	filtered := applyFilter(models, flags.only, flags.except)
	for _, m := range filtered {
		slug := string(m.Provider)
		if slug != "anthropic" && slug != "google" {
			t.Errorf("applyFilter: model with provider %q passed inclusion filter [anthropic google]", slug)
		}
	}
	// Verify included providers are present.
	seen := make(map[string]bool)
	for _, m := range filtered {
		seen[string(m.Provider)] = true
	}
	for _, p := range flags.only {
		if !seen[p] {
			t.Errorf("applyFilter: provider %q missing from filtered results", p)
		}
	}
}

// TestFilterFlags_ProviderExclusion verifies that -all-providers-except removes
// the listed providers from model data but keeps them in the constants list.
func TestFilterFlags_ProviderExclusion(t *testing.T) {
	flags, err := parseFlags([]string{"-all-providers-except=openrouter,vercel"})
	if err != nil {
		t.Fatalf("parseFlags: unexpected error: %v", err)
	}
	if len(flags.only) != 0 {
		t.Fatalf("parseFlags: only = %v, want []", flags.only)
	}
	if len(flags.except) != 2 {
		t.Fatalf("parseFlags: except = %v, want [openrouter vercel]", flags.except)
	}

	models := makeTestModels()
	filtered := applyFilter(models, flags.only, flags.except)
	for _, m := range filtered {
		slug := string(m.Provider)
		if slug == "openrouter" || slug == "vercel" {
			t.Errorf("applyFilter: excluded provider %q appeared in filtered results", slug)
		}
	}
	// Non-excluded providers are still present.
	seen := make(map[string]bool)
	for _, m := range filtered {
		seen[string(m.Provider)] = true
	}
	if !seen["anthropic"] {
		t.Error("applyFilter: 'anthropic' should not be excluded but was missing")
	}
}

// TestFilterFlags_NoFlags verifies that with no flags, all models are returned.
func TestFilterFlags_NoFlags(t *testing.T) {
	flags, err := parseFlags(nil)
	if err != nil {
		t.Fatalf("parseFlags(nil): unexpected error: %v", err)
	}
	models := makeTestModels()
	filtered := applyFilter(models, flags.only, flags.except)
	if len(filtered) != len(models) {
		t.Errorf("applyFilter with no flags: got %d models, want %d", len(filtered), len(models))
	}
}

// TestSplitComma verifies the comma-splitting helper.
func TestSplitComma(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"anthropic,google", []string{"anthropic", "google"}},
		{"anthropic", []string{"anthropic"}},
		{"", nil},
		{"anthropic, google", []string{"anthropic", "google"}},
	}
	for _, tc := range cases {
		got := splitComma(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("splitComma(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i, g := range got {
			if g != tc.want[i] {
				t.Errorf("splitComma(%q)[%d] = %q, want %q", tc.in, i, g, tc.want[i])
			}
		}
	}
}

// --------------------------------------------------------------------------
// SLICE-4 tests: nameForCanonical, resolveCollisions, generateConstantsSource
// --------------------------------------------------------------------------

// testSlugToConst is a minimal slugToConst map for tests, providing the correct
// provider constant names (with proper casing) for the providers used in golden examples.
var testSlugToConst = map[string]string{
	"anthropic":    "ProviderAnthropic",
	"openai":       "ProviderOpenAI",
	"google":       "ProviderGoogle",
	"google-vertex": "ProviderGoogleVertex",
	"openrouter":   "ProviderOpenRouter",
}

// TestNameForCanonical_KnownExamples verifies that nameForCanonicalWithMap produces
// the expected constant names for the spec-defined golden examples from UAT-1 / PROPOSAL-3.
// Updated to new double-underscore template: Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
// (B5: double underscores between components, single underscores within components).
//
// The naming uses double underscores between EVERY token from the raw ID (after date strip),
// plus the provider prefix and date suffix. Tokens from the raw ID (hyphen/dot split) each
// become a separate __-separated component. The Version field produces a single
// underscore-within-component segment when it is non-empty (e.g. "4.5" → "4_5").
func TestNameForCanonical_KnownExamples(t *testing.T) {
	cases := []struct {
		desc     string
		model    bestiary.ModelInfo
		wantName string
	}{
		{
			desc: "claude-opus-4-20250514 on Anthropic",
			model: bestiary.ModelInfo{
				ID:       "claude-opus-4-20250514",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Date:     "2025-05-14",
			},
			// Tokens after date strip: [claude→Claude, opus→Opus, 4→4]
			// Double-underscore join + provSuffix + date.
			wantName: "Model__Anthropic__Claude__Opus__4__20250514",
		},
		{
			desc: "claude-opus-4-1 on Anthropic (date not in ID, from release field)",
			model: bestiary.ModelInfo{
				ID:      "claude-opus-4-1",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				// Date comes from release field, NOT from ID content.
				// The ID "claude-opus-4-1" has no YYYYMMDD/YYYY-MM-DD date.
				// So date should NOT be appended to the constant name.
				Date: "2025-08-05",
			},
			// Tokens: [Claude, Opus, 4, 1]; date not in ID → no date suffix.
			wantName: "Model__Anthropic__Claude__Opus__4__1",
		},
		{
			desc: "gpt-4o-2024-08-06 on OpenAI",
			model: bestiary.ModelInfo{
				ID:       "gpt-4o-2024-08-06",
				Provider: "openai",
				Family:   "gpt",
				Variant:  "",
				Date:     "2024-08-06",
			},
			// Tokens after date strip: [gpt→GPT, 4o→4o]
			wantName: "Model__OpenAI__GPT__4o__20240806",
		},
		{
			desc: "gemini-2.5-flash-lite-preview-06-17 on GoogleVertex (MM-DD date form)",
			model: bestiary.ModelInfo{
				ID:       "gemini-2.5-flash-lite-preview-06-17",
				Provider: "google-vertex",
				Family:   "gemini",
				Variant:  "flash-lite",
				Date:     "2025-06-17",
			},
			// ID has "06-17" which is the MM-DD form of Date "2025-06-17".
			// stripDateFromID strips "06-17", leaving "gemini-2.5-flash-lite-preview".
			// Tokens: [Gemini, 2, 5, Flash, Lite, Preview] — each becomes own __ segment.
			wantName: "Model__GoogleVertex__Gemini__2__5__Flash__Lite__Preview__20250617",
		},
		{
			desc: "model with no date",
			model: bestiary.ModelInfo{
				ID:       "claude-haiku",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "haiku",
				Date:     "",
			},
			wantName: "Model__Anthropic__Claude__Haiku",
		},
		{
			desc: "provider-prefixed ID (openrouter style)",
			model: bestiary.ModelInfo{
				ID:       "anthropic/claude-opus-4-20250514",
				Provider: "openrouter",
				Family:   "claude",
				Variant:  "opus",
				Date:     "2025-05-14",
			},
			wantName: "Model__OpenRouter__Claude__Opus__4__20250514",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := nameForCanonicalWithMap(tc.model, testSlugToConst)
			if got != tc.wantName {
				t.Errorf("nameForCanonicalWithMap: got %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestSkipEmptyFamily verifies that nameForCanonical returns "" when Family is empty.
func TestSkipEmptyFamily(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:       "some-model-123",
		Provider: "anthropic",
		Family:   "", // empty → skip
		Variant:  "",
		Date:     "2025-01-01",
	}
	got := nameForCanonical(m)
	if got != "" {
		t.Errorf("nameForCanonical: expected empty string for empty Family, got %q", got)
	}
}

// TestResolveCollisions_VersionSuffix verifies that when two models share the
// same naive name but have distinguishable version segments in their raw IDs,
// the version segment is used as a disambiguator (pass (a)).
func TestResolveCollisions_VersionSuffix(t *testing.T) {
	// Two models that produce the same naive name Model__Anthropic__Claude__Opus
	// but whose IDs have different version tokens (4 vs 3_5).
	models := []bestiary.ModelInfo{
		{
			ID:       "claude-opus-4",
			Provider: "anthropic",
			Family:   "claude",
			Variant:  "opus",
			Date:     "",
		},
		{
			ID:       "claude-opus-3-5",
			Provider: "anthropic",
			Family:   "claude",
			Variant:  "opus",
			Date:     "",
		},
	}
	// Both produce "Model__Anthropic__Claude__Opus" as the naive name (double-underscore, B5).
	names := []string{
		"Model__Anthropic__Claude__Opus",
		"Model__Anthropic__Claude__Opus",
	}

	resolved := resolveCollisions(names, models)
	if len(resolved) != 2 {
		t.Fatalf("resolveCollisions: want 2 results, got %d", len(resolved))
	}

	// Both must be non-empty and distinct.
	if resolved[0] == "" || resolved[1] == "" {
		t.Errorf("resolveCollisions: got empty string in result: %v", resolved)
	}
	if resolved[0] == resolved[1] {
		t.Errorf("resolveCollisions: not unique: both = %q", resolved[0])
	}

	// Version-suffix disambiguation (pass a) must produce the exact expected names.
	// claude-opus-4 → version segment "4"; claude-opus-3-5 → version segment "3_5".
	// Under the new template the separator between the naive name and the version suffix is "__".
	want0 := "Model__Anthropic__Claude__Opus__4"
	want1 := "Model__Anthropic__Claude__Opus__3_5"
	if (resolved[0] != want0 || resolved[1] != want1) && (resolved[0] != want1 || resolved[1] != want0) {
		t.Errorf("resolveCollisions: unexpected version-suffix results:\n  got  [%q, %q]\n  want [%q, %q] (either order)",
			resolved[0], resolved[1], want0, want1)
	}
}

// TestResolveCollisions_SequentialSuffix verifies that when version-suffix
// disambiguation fails (no distinct version), sequential _<n> suffixes are appended.
func TestResolveCollisions_SequentialSuffix(t *testing.T) {
	// Two models with the same naive name and same (or indistinguishable) version.
	// We force this by using models with matching version tokens.
	models := []bestiary.ModelInfo{
		{
			ID:       "mystery-model",
			Provider: "anthropic",
			Family:   "mystery",
			Variant:  "",
			Date:     "",
		},
		{
			ID:       "mystery-model",
			Provider: "anthropic",
			Family:   "mystery",
			Variant:  "",
			Date:     "",
		},
	}
	names := []string{
		"Model__Anthropic__Mystery__Model",
		"Model__Anthropic__Mystery__Model",
	}

	resolved := resolveCollisions(names, models)
	if len(resolved) != 2 {
		t.Fatalf("resolveCollisions: want 2 results, got %d", len(resolved))
	}
	if resolved[0] == resolved[1] {
		t.Errorf("resolveCollisions: sequential fallback failed; both = %q", resolved[0])
	}
	// Must have numeric suffix (sequential disambiguator appended with "_" within the suffix).
	if !strings.Contains(resolved[0], "_1") && !strings.Contains(resolved[0], "_2") {
		t.Errorf("resolveCollisions: expected numeric suffix in %q", resolved[0])
	}
	if !strings.Contains(resolved[1], "_1") && !strings.Contains(resolved[1], "_2") {
		t.Errorf("resolveCollisions: expected numeric suffix in %q", resolved[1])
	}
}

// TestGenerateConstantsSource_Compiles verifies that generateConstantsSource
// returns valid Go source that passes go/format for a small set of test models.
func TestGenerateConstantsSource_Compiles(t *testing.T) {
	models := []bestiary.ModelInfo{
		{
			ID:       "claude-opus-4-20250514",
			Provider: "anthropic",
			Family:   "claude",
			Variant:  "opus",
			Date:     "2025-05-14",
		},
		{
			ID:       "gpt-4o-2024-08-06",
			Provider: "openai",
			Family:   "gpt",
			Variant:  "",
			Date:     "2024-08-06",
		},
		{
			// Skip-rule: empty family.
			ID:       "unknown-xyz",
			Provider: "some-provider",
			Family:   "",
			Variant:  "",
			Date:     "",
		},
	}

	src, err := generateConstantsSource(models, testSlugToConst)
	if err != nil {
		t.Fatalf("generateConstantsSource: unexpected error: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("generateConstantsSource: returned empty source")
	}
	// Must contain the expected constant names (double-underscore between components, B5).
	srcStr := string(src)
	if !strings.Contains(srcStr, "Model__Anthropic__Claude__Opus__4__20250514") {
		t.Errorf("generated source missing Model__Anthropic__Claude__Opus__4__20250514:\n%s", srcStr[:min(500, len(srcStr))])
	}
	if !strings.Contains(srcStr, "Model__OpenAI__GPT__4o__20240806") {
		t.Errorf("generated source missing Model__OpenAI__GPT__4o__20240806:\n%s", srcStr[:min(500, len(srcStr))])
	}
	// Must NOT contain a constant for the skip-rule model.
	if strings.Contains(srcStr, "unknown-xyz") {
		t.Errorf("generated source should not contain skip-rule model 'unknown-xyz'")
	}
	// Must contain ModelIDs() function (named to avoid clash with registry.go:Models() []ModelInfo).
	if !strings.Contains(srcStr, "func ModelIDs()") {
		t.Errorf("generated source missing ModelIDs() function")
	}
}

// min is a helper for older Go versions that don't have built-in min for integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// makeTestModels returns a small []bestiary.ModelInfo spanning four providers,
// sufficient to verify inclusion/exclusion filter behaviour.
func makeTestModels() []bestiary.ModelInfo {
	providers := []string{"anthropic", "google", "openrouter", "vercel"}
	var out []bestiary.ModelInfo
	for _, p := range providers {
		out = append(out, bestiary.ModelInfo{
			ID:       bestiary.ModelID("test-model-" + p),
			Provider: bestiary.Provider(p),
		})
	}
	return out
}

// minimalAPIJSON returns a minimal valid api_response.json body for tests.
// One provider ("testprovider") with one model.
func minimalAPIJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"testprovider": map[string]any{
			"name": "Test Provider",
			"models": map[string]any{
				"test-model-1": map[string]any{
					"id":   "test-model-1",
					"name": "Test Model 1",
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("minimalAPIJSON: marshal: %v", err)
	}
	return b
}

// normalizationAPIJSON returns a minimal api_response.json body with models that
// exercise the normalization code paths: one model with a non-empty family field
// and one model with an empty family field (triggering InferFamilyFromID).
func normalizationAPIJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"anthropic": map[string]any{
			"name": "Anthropic",
			"models": map[string]any{
				// Model with a non-empty family — exercises ParseFamily path.
				"claude-opus-4-20250514": map[string]any{
					"id":           "claude-opus-4-20250514",
					"name":         "Claude Opus 4",
					"family":       "claude-opus",
					"release_date": "2025-05-14",
				},
				// Model with empty family — exercises InferFamilyFromID path (~25% of real models).
				"claude-haiku-no-family": map[string]any{
					"id":     "claude-haiku-no-family",
					"name":   "Claude Haiku (no family)",
					"family": "",
				},
			},
		},
		"openai": map[string]any{
			"name": "OpenAI",
			"models": map[string]any{
				// GPT model with date in ID.
				"gpt-4o-2024-08-06": map[string]any{
					"id":           "gpt-4o-2024-08-06",
					"name":         "GPT-4o",
					"family":       "gpt-4o",
					"release_date": "2024-08-06",
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("normalizationAPIJSON: marshal: %v", err)
	}
	return b
}

// TestGenToModelInfo_EmptyFamily verifies that the InferFamilyFromID code path in
// genToModelInfo fires when the model's family field is empty (~25% of real models).
// This exercises the else branch in genToModelInfo that the parse_test.go unit tests
// for InferFamilyFromID do not cover at the codegen integration layer.
func TestGenToModelInfo_EmptyFamily(t *testing.T) {
	wm := genWireModel{
		ID:     "claude-haiku-no-family",
		Name:   "Claude Haiku (no family)",
		Family: "", // empty — must trigger InferFamilyFromID
	}
	info := genToModelInfo("anthropic", wm)

	if info.RawFamily != "" {
		t.Errorf("RawFamily: got %q, want empty (raw field was empty)", info.RawFamily)
	}
	// InferFamilyFromID("claude-haiku-no-family", "anthropic") must populate Family.
	if info.Family == "" {
		t.Errorf("Family: got empty; InferFamilyFromID should infer a non-empty family from ID %q", wm.ID)
	}
	// Variant may or may not be empty depending on InferFamilyFromID behavior.
	// The key property is that Family is populated (no silent no-op).
	t.Logf("genToModelInfo empty-family: Family=%q Variant=%q", info.Family, info.Variant)
}

// TestGenToModelInfo_CanonicalFields verifies that genToModelInfo correctly populates
// Family, Variant, and Date for models with known inputs.
// This guards against regressions in the genToModelInfo normalization splice path.
func TestGenToModelInfo_CanonicalFields(t *testing.T) {
	cases := []struct {
		desc             string
		providerSlug     string
		wm               genWireModel
		wantFamily       string
		wantVariant      string
		wantDateContains string // substring of Date (may be empty for no-date models)
	}{
		{
			desc:         "claude-opus-4-20250514: family=claude-opus, date in ID",
			providerSlug: "anthropic",
			wm: genWireModel{
				ID:          "claude-opus-4-20250514",
				Name:        "Claude Opus 4",
				Family:      "claude-opus",
				ReleaseDate: "2025-05-14",
			},
			wantFamily:       "claude",
			wantVariant:      "opus",
			wantDateContains: "2025-05-14",
		},
		{
			desc:         "gpt-4o-2024-08-06: family=gpt-4o, date in ID",
			providerSlug: "openai",
			wm: genWireModel{
				ID:          "gpt-4o-2024-08-06",
				Name:        "GPT-4o",
				Family:      "gpt-4o",
				ReleaseDate: "2024-08-06",
			},
			// ParseFamily("gpt-4o") returns ("gpt-4o", "") when no override matches;
			// the exact result depends on parse data but the key property is that
			// Family is non-empty and Date is populated from the ID.
			wantFamily:       "gpt-4o",
			wantVariant:      "",
			wantDateContains: "2024-08-06",
		},
		{
			desc:         "empty family: Family inferred from ID",
			providerSlug: "anthropic",
			wm: genWireModel{
				ID:     "claude-haiku-no-family",
				Name:   "Claude Haiku",
				Family: "",
			},
			wantFamily:       "claude", // InferFamilyFromID should infer "claude"
			wantDateContains: "",       // no date
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			info := genToModelInfo(tc.providerSlug, tc.wm)

			if string(info.Family) != tc.wantFamily {
				t.Errorf("Family: got %q, want %q", info.Family, tc.wantFamily)
			}
			if tc.wantVariant != "" && info.Variant != tc.wantVariant {
				t.Errorf("Variant: got %q, want %q", info.Variant, tc.wantVariant)
			}
			if tc.wantDateContains != "" && !strings.Contains(info.Date, tc.wantDateContains) {
				t.Errorf("Date: got %q, want it to contain %q", info.Date, tc.wantDateContains)
			}
			if tc.wantDateContains == "" && info.Date != "" {
				// Some models may extract a date from the release field even when wantDateContains is "".
				// Just log it; don't fail — release-date fallback is valid behavior.
				t.Logf("Date: got %q (non-empty); extracted from release field or ID", info.Date)
			}
		})
	}
}

// TestNoFetch_MalformedCache verifies that --no-fetch with a corrupt cache file
// returns an actionable error describing the JSON decode failure.
// This exercises the json.Unmarshal error path in fetchModelsWithRaw.
func TestNoFetch_MalformedCache(t *testing.T) {
	tmpDir := t.TempDir()
	cachePath := filepath.Join(tmpDir, cacheFile)

	// Write invalid JSON to the cache file.
	if err := os.WriteFile(cachePath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("setup: write malformed cache file: %v", err)
	}

	_, _, _, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
	if err == nil {
		t.Fatal("fetchModelsWithRaw(noFetch=true, malformed cache): expected error, got nil")
	}
	msg := err.Error()
	// The error must reference the decode failure (not a generic "operation failed").
	if !strings.Contains(msg, "decode JSON") && !strings.Contains(msg, "json") && !strings.Contains(msg, "JSON") {
		t.Errorf("error for malformed cache does not mention JSON decode failure:\n%s", msg)
	}
}

// TestCacheDirFlag_EmptyValue verifies that parseFlags rejects -cache-dir= (empty value)
// with an actionable error instead of silently setting cacheDir to "".
func TestCacheDirFlag_EmptyValue(t *testing.T) {
	_, err := parseFlags([]string{"-cache-dir="})
	if err == nil {
		t.Fatal("parseFlags(-cache-dir=): expected error for empty value, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "-cache-dir") {
		t.Errorf("error message %q does not mention -cache-dir", msg)
	}
	if !strings.Contains(msg, "empty") {
		t.Errorf("error message %q does not mention 'empty'", msg)
	}
}

// TestCacheDirFlag verifies that bestiary-gen --cache-dir <tmpdir> writes
// api_response.json into the given directory (not the default .bestiary-gen-cache).
// This test exercises the full run() code path end-to-end via apiURL override.
func TestCacheDirFlag(t *testing.T) {
	// Serve a minimal API response over HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(minimalAPIJSON(t))
	}))
	defer srv.Close()

	// Override the package-level apiURL var so run() fetches from the test server.
	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	// run() writes generated .go files to the working directory; use a temp dir
	// as cwd so we don't pollute the repo root.
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cacheDir := filepath.Join(tmpDir, "my-custom-cache")

	// Call run() with --cache-dir pointing to our custom directory.
	if err := run([]string{"-cache-dir=" + cacheDir}); err != nil {
		t.Fatalf("run(-cache-dir=%s): unexpected error: %v", cacheDir, err)
	}

	// Assert api_response.json was written to cacheDir (not defaultCacheDir).
	wantPath := filepath.Join(cacheDir, cacheFile)
	info, statErr := os.Stat(wantPath)
	if statErr != nil {
		t.Fatalf("cache file not written to --cache-dir %q: %v", cacheDir, statErr)
	}
	if info.Size() == 0 {
		t.Fatalf("cache file at %q is empty; expected non-empty JSON", wantPath)
	}

	// Assert the default cache dir was NOT created (no side-effects).
	defaultPath := filepath.Join(tmpDir, defaultCacheDir)
	if _, statErr := os.Stat(defaultPath); statErr == nil {
		t.Errorf("default cache dir %q was created; run() should only write to --cache-dir", defaultPath)
	}
}

// TestNoFetch_HitsCache verifies that --no-fetch reads from a pre-populated
// cache file without making any HTTP request.
// The httptest.Server is wired into apiURL so that if fetchModelsWithRaw ever
// makes an outbound request via apiURL, the contacted flag trips.
//
// Note: contacted trips only if a regression calls apiURL. A regression that
// uses a hardcoded URL or a separate http.Get would not be caught by this guard.
func TestNoFetch_HitsCache(t *testing.T) {
	tmpDir := t.TempDir()
	raw := minimalAPIJSON(t)

	// Pre-populate the cache.
	cachePath := filepath.Join(tmpDir, cacheFile)
	if err := os.WriteFile(cachePath, raw, 0o644); err != nil {
		t.Fatalf("setup: write cache file: %v", err)
	}

	// A test server that fails the test if contacted — no HTTP should happen.
	contacted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Override apiURL so any accidental HTTP fetch would hit our guard server.
	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	// fetchModelsWithRaw with noFetch=true must read from cache, not contact srv.
	gotRaw, models, provMeta, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
	if err != nil {
		t.Fatalf("fetchModelsWithRaw(noFetch=true): unexpected error: %v", err)
	}
	if contacted {
		t.Error("fetchModelsWithRaw(noFetch=true): made an HTTP request when it should not have")
	}
	if len(models) == 0 {
		t.Error("fetchModelsWithRaw(noFetch=true): returned no models; expected at least one")
	}
	if len(provMeta) == 0 {
		t.Error("fetchModelsWithRaw(noFetch=true): returned no provider metadata")
	}
	if len(gotRaw) == 0 {
		t.Error("fetchModelsWithRaw(noFetch=true): returned empty rawJSON")
	}
}

// TestNoFetch_MissingCache_ActionableError verifies that --no-fetch with a
// missing cache file returns an *ErrCacheMiss with all 6 required actionable
// fields: What, Why, Where, When, what-it-means, how-to-fix.
func TestNoFetch_MissingCache_ActionableError(t *testing.T) {
	tmpDir := t.TempDir()
	// Deliberately do NOT create api_response.json in tmpDir.

	_, _, _, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
	if err == nil {
		t.Fatal("fetchModelsWithRaw(noFetch=true, missing cache): expected error, got nil")
	}

	// Must be an *ErrCacheMiss.
	var cacheMiss *ErrCacheMiss
	if !errors.As(err, &cacheMiss) {
		t.Fatalf("expected *ErrCacheMiss, got %T: %v", err, err)
	}

	// All 6 actionability fields must be present in the error message.
	wantPath := filepath.Join(tmpDir, cacheFile)
	absWant, _ := filepath.Abs(wantPath)
	msg := cacheMiss.Error()

	// (1) What: describes what went wrong — the missing/empty cache file.
	if !strings.Contains(msg, "cached api_response.json missing or empty") {
		t.Errorf("error message missing 'What' field (cached api_response.json missing or empty):\n%s", msg)
	}
	// (2) Why: explains the cause — --no-fetch was set.
	if !strings.Contains(msg, "--no-fetch") {
		t.Errorf("error message missing 'Why' field (--no-fetch):\n%s", msg)
	}
	// (3) Where: the file path that was missing.
	if !strings.Contains(msg, absWant) {
		t.Errorf("error message missing 'Where' field (path %q):\n%s", absWant, msg)
	}
	// (4) When: the step at which it failed.
	if !strings.Contains(msg, "fetchModelsWithRaw") {
		t.Errorf("error message missing 'When' field (fetchModelsWithRaw):\n%s", msg)
	}
	// (5) What it means: consequence for the caller.
	if !strings.Contains(msg, "cannot proceed without API data") {
		t.Errorf("error message missing 'what-it-means' field (cannot proceed without API data):\n%s", msg)
	}
	// (6) How to fix: actionable remediation.
	if !strings.Contains(msg, "re-run without --no-fetch") {
		t.Errorf("error message missing 'how-to-fix' field (re-run without --no-fetch):\n%s", msg)
	}
}

// --------------------------------------------------------------------------
// SLICE-FIX-2 tests: cross-provider decomposition consistency
// --------------------------------------------------------------------------

// TestStaticDataset_CrossProviderConsistency verifies that for model IDs present
// under multiple providers in the static dataset, providers that have an empty
// raw_family field produce the same (Family, Variant, Version) as providers with
// a populated raw_family, when all populated-raw_family providers agree on the
// same decomposition.
//
// B6 (SLICE-FIX-2): codegen must produce consistent decompositions regardless of
// whether the raw_family field is empty or populated. The primary documented
// regression was: Nano-GPT and 302ai (empty raw_family) producing
// Variant="" for claude-opus-4-5-20251101 while Anthropic/QiHangAI
// (raw_family="claude-opus") produce Variant="opus".
//
// SCOPE BOUNDARY (documented findings for FOLLOWUP_SLICE-1 / bestiary-wi36):
//
// Some divergences are NOT caused by the empty-raw_family bug and are deferred:
//
//  1. Different raw_family values across providers (upstream data inconsistency):
//     Some providers use different raw_family for the same model ID, leading to
//     fundamentally different canonical results. Example: alibaba uses raw_family=""
//     for qwen3-* IDs while other providers use "qwen3-*". These are upstream API
//     inconsistencies, not a codegen bug. Deferred to bestiary-wi36.
//
//  2. Complex claude-3.x IDs (claude-3-5-haiku-*, claude-3-7-sonnet-*):
//     InferFamilyFromIDWithVariant("claude-3-5-haiku-20241022") extracts
//     family="claude-3-5" (since "3-5" is a version component in the ID), while
//     providers with raw_family="claude-haiku" yield family="claude". The correct
//     decomposition requires parser fixes for claude-3.x family IDs, deferred to
//     bestiary-wi36.
//
// This test constrains its check to: groups where ALL populated-raw_family providers
// agree on the same (family, variant, version) AND at least one empty-raw_family
// provider disagrees. This targets the original documented bug precisely.
func TestStaticDataset_CrossProviderConsistency(t *testing.T) {
	models := bestiary.StaticModels()

	// Build a per-ID map with the raw_family, normalized decomp, and provider.
	type entry struct {
		RawFamily string
		Family    string
		Variant   string
		Version   string
		Provider  string
	}
	byID := make(map[string][]entry)
	for _, m := range models {
		id := string(m.ID)
		// Skip non-standard ID formats deferred to FOLLOWUP_SLICE-1 (bestiary-wi36).
		if strings.Contains(id, "/") || strings.Contains(id, ".") ||
			strings.HasPrefix(id, "@") || strings.HasPrefix(id, ":") {
			continue
		}
		byID[id] = append(byID[id], entry{
			RawFamily: string(m.RawFamily),
			Family:    string(m.Family),
			Variant:   m.Variant,
			Version:   m.Version,
			Provider:  string(m.Provider),
		})
	}

	for id, entries := range byID {
		if len(entries) < 2 {
			continue
		}

		// Split entries into populated-raw_family and empty-raw_family groups.
		var populated []entry
		var empty []entry
		for _, e := range entries {
			if e.RawFamily != "" {
				populated = append(populated, e)
			} else {
				empty = append(empty, e)
			}
		}

		// Skip this ID if there are no empty-raw_family providers
		// (divergence can't be caused by the documented bug).
		if len(empty) == 0 {
			continue
		}
		// Skip this ID if there are no populated-raw_family providers
		// (no reference to compare against).
		if len(populated) == 0 {
			continue
		}

		// Check if all populated-raw_family providers agree on the same decomposition.
		// If they disagree, it's an upstream data inconsistency — skip (FOLLOWUP).
		consensusFamily := populated[0].Family
		consensusVariant := populated[0].Variant
		consensusVersion := populated[0].Version
		populatedAgree := true
		for _, p := range populated[1:] {
			if p.Family != consensusFamily || p.Variant != consensusVariant || p.Version != consensusVersion {
				populatedAgree = false
				break
			}
		}
		if !populatedAgree {
			// Upstream data inconsistency across populated providers — deferred to bestiary-wi36.
			continue
		}

		// Compute what InferFamilyFromIDWithVariant returns for this ID.
		// This is the reference point for "what the current parser can derive
		// from the ID alone, without any raw_family data".
		// If InferFamilyFromIDWithVariant returns the same as the consensus,
		// then an empty-raw_family provider that differs is a genuine bug.
		// If InferFamilyFromIDWithVariant cannot derive the consensus, the
		// case is deferred to FOLLOWUP_SLICE-1 (bestiary-wi36) — it requires
		// parser enhancements beyond the scope of SLICE-FIX-2.
		inferredFamily, inferredVariant, inferredVersion := bestiary.InferFamilyFromIDWithVariant(
			bestiary.ModelID(id),
			bestiary.Provider(empty[0].Provider),
		)

		// Skip if InferFamilyFromIDWithVariant can't derive the consensus.
		// These are FOLLOWUP_SLICE-1 cases (parser enhancement needed).
		if inferredFamily != bestiary.Family(consensusFamily) ||
			inferredVariant != consensusVariant ||
			inferredVersion != consensusVersion {
			continue
		}

		// InferFamilyFromIDWithVariant CAN derive the consensus — so all
		// empty-raw_family providers must match. Flag any that don't.
		for _, e := range empty {
			if e.Family == consensusFamily && e.Variant == consensusVariant && e.Version == consensusVersion {
				continue
			}
			t.Errorf("cross-provider decomposition divergence for ID %q:\n"+
				"  populated-raw_family consensus (provider %q) → (family=%q, variant=%q, version=%q)\n"+
				"  empty-raw_family provider %q → (family=%q, variant=%q, version=%q)\n"+
				"  InferFamilyFromIDWithVariant returns (%q, %q, %q) — the regen must use it.\n"+
				"  Fix: re-run go generate ./... to bake the updated decomposition into models_static_gen.go.",
				id,
				populated[0].Provider, consensusFamily, consensusVariant, consensusVersion,
				e.Provider, e.Family, e.Variant, e.Version,
				inferredFamily, inferredVariant, inferredVersion)
		}
	}
}

// --------------------------------------------------------------------------
// SLICE-FIX-3 tests: double-hyphen flag support, ChatGPT casing, double-underscore
// --------------------------------------------------------------------------

// TestParseFlags_DoubleHyphen verifies that all flags accept BOTH single-hyphen
// and double-hyphen forms (e.g. --no-fetch is equivalent to -no-fetch).
// This test covers B1-B3 from the slice spec.
//
// These tests will FAIL until L3 adds double-hyphen prefix support to parseFlags.
func TestParseFlags_DoubleHyphen(t *testing.T) {
	cases := []struct {
		desc    string
		args    []string
		check   func(t *testing.T, got flagResult)
	}{
		{
			desc: "--no-fetch sets noFetch=true",
			args: []string{"--no-fetch"},
			check: func(t *testing.T, got flagResult) {
				if !got.noFetch {
					t.Errorf("parseFlags([\"--no-fetch\"]): noFetch = false, want true")
				}
			},
		},
		{
			desc: "--cache-dir=<value> (equals form) sets cacheDir",
			args: []string{"--cache-dir=/tmp/foo"},
			check: func(t *testing.T, got flagResult) {
				if got.cacheDir != "/tmp/foo" {
					t.Errorf("parseFlags([\"--cache-dir=/tmp/foo\"]): cacheDir = %q, want /tmp/foo", got.cacheDir)
				}
			},
		},
		{
			desc: "--cache-dir <value> (space form) sets cacheDir",
			args: []string{"--cache-dir", "/tmp/bar"},
			check: func(t *testing.T, got flagResult) {
				if got.cacheDir != "/tmp/bar" {
					t.Errorf("parseFlags([\"--cache-dir\", \"/tmp/bar\"]): cacheDir = %q, want /tmp/bar", got.cacheDir)
				}
			},
		},
		{
			desc: "--only-providers=a,b (equals form) sets only",
			args: []string{"--only-providers=a,b"},
			check: func(t *testing.T, got flagResult) {
				if len(got.only) != 2 || got.only[0] != "a" || got.only[1] != "b" {
					t.Errorf("parseFlags([\"--only-providers=a,b\"]): only = %v, want [a b]", got.only)
				}
			},
		},
		{
			desc: "--only-providers <value> (space form) sets only",
			args: []string{"--only-providers", "x,y"},
			check: func(t *testing.T, got flagResult) {
				if len(got.only) != 2 || got.only[0] != "x" || got.only[1] != "y" {
					t.Errorf("parseFlags([\"--only-providers\", \"x,y\"]): only = %v, want [x y]", got.only)
				}
			},
		},
		{
			desc: "--all-providers-except=z (equals form) sets except",
			args: []string{"--all-providers-except=z"},
			check: func(t *testing.T, got flagResult) {
				if len(got.except) != 1 || got.except[0] != "z" {
					t.Errorf("parseFlags([\"--all-providers-except=z\"]): except = %v, want [z]", got.except)
				}
			},
		},
		{
			desc: "--all-providers-except <value> (space form) sets except",
			args: []string{"--all-providers-except", "p"},
			check: func(t *testing.T, got flagResult) {
				if len(got.except) != 1 || got.except[0] != "p" {
					t.Errorf("parseFlags([\"--all-providers-except\", \"p\"]): except = %v, want [p]", got.except)
				}
			},
		},
		{
			desc: "-no-fetch (single-hyphen) still sets noFetch=true (regression)",
			args: []string{"-no-fetch"},
			check: func(t *testing.T, got flagResult) {
				if !got.noFetch {
					t.Errorf("parseFlags([\"-no-fetch\"]): noFetch = false, want true (regression)")
				}
			},
		},
		{
			desc: "-cache-dir=<value> (single-hyphen equals form) still works (regression)",
			args: []string{"-cache-dir=/tmp/baz"},
			check: func(t *testing.T, got flagResult) {
				if got.cacheDir != "/tmp/baz" {
					t.Errorf("parseFlags([\"-cache-dir=/tmp/baz\"]): cacheDir = %q, want /tmp/baz", got.cacheDir)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseFlags(tc.args)
			if err != nil {
				t.Fatalf("parseFlags(%v): unexpected error: %v", tc.args, err)
			}
			tc.check(t, got)
		})
	}
}

// TestSlugToIdentifier_ChatGPT verifies that the chatgpt casing override is
// applied correctly. B4: chatgpt → ChatGPT.
func TestSlugToIdentifier_ChatGPT(t *testing.T) {
	cases := []struct {
		slug     string
		nameHint string
		want     string
	}{
		// Full slug "chatgpt" → "ChatGPT" (single-token casing override).
		{"chatgpt", "ChatGPT", "ChatGPT"},
		// "chatgpt-4o" splits into ["chatgpt", "4o"]:
		// chatgpt → ChatGPT via casing override.
		// 4o: digit-leading, alpha "o" has no casing override → title-cased to "O"
		// (slugToIdentifier uppercases single-char alpha suffixes; see tokenToConstPart for
		// the model-ID tokenization path that preserves them).
		{"chatgpt-4o", "ChatGPT-4o", "ChatGPT4O"},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			got := slugToIdentifier(tc.slug, tc.nameHint)
			if got != tc.want {
				t.Errorf("slugToIdentifier(%q, %q) = %q, want %q", tc.slug, tc.nameHint, got, tc.want)
			}
		})
	}
}

// TestNameForCanonical_DoubleUnderscoreTemplate verifies the new Model__ naming
// convention (double underscores between field components, single underscores
// within a component, e.g. version "4.5" → "4_5").
//
// B5: Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
//
// When Version is non-empty, the version "4.5" is encoded as a single
// segment "4_5" (dot→underscore). The raw ID version tokens are replaced by this
// single compact segment so that "4_5" uses single underscores within.
//
// These tests will FAIL until L3 changes the join separator and adds version-segment logic.
func TestNameForCanonical_DoubleUnderscoreTemplate(t *testing.T) {
	cases := []struct {
		desc     string
		model    bestiary.ModelInfo
		wantName string
	}{
		{
			desc: "claude-opus-4-5 with Version on Anthropic (B5 golden)",
			model: bestiary.ModelInfo{
				ID:       "claude-opus-4-5-20251101",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.5",
				Date:     "2025-11-01",
			},
			// Version "4.5" → segment "4_5" (single underscores within, double between).
			// Raw version tokens ("4","5") replaced by the Version segment.
			wantName: "Model__Anthropic__Claude__Opus__4_5__20251101",
		},
		{
			desc: "gpt-4o without version or date on OpenAI (B5 golden)",
			model: bestiary.ModelInfo{
				ID:       "gpt-4o",
				Provider: "openai",
				Family:   "gpt",
				Variant:  "",
				Version:  "",
				Date:     "",
			},
			// No Version → raw ID tokens: [GPT, 4o]; joined with __.
			wantName: "Model__OpenAI__GPT__4o",
		},
		{
			desc: "chatgpt model uses ChatGPT casing (B4)",
			model: bestiary.ModelInfo{
				ID:       "chatgpt-4o",
				Provider: "openai",
				Family:   "chatgpt",
				Variant:  "",
				Version:  "",
				Date:     "",
			},
			// chatgpt → ChatGPT via casingOverrides; 4o from raw ID.
			wantName: "Model__OpenAI__ChatGPT__4o",
		},
		{
			desc: "claude-haiku no date, double underscore between provider and family",
			model: bestiary.ModelInfo{
				ID:       "claude-haiku",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "haiku",
				Version:  "",
				Date:     "",
			},
			wantName: "Model__Anthropic__Claude__Haiku",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got := nameForCanonicalWithMap(tc.model, testSlugToConst)
			if got != tc.wantName {
				t.Errorf("nameForCanonicalWithMap: got %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestValidateGeneratedFamilyType verifies that validateGeneratedFamilyType
// accepts a file with the correct named-type declaration and rejects files
// that are missing the declaration or use the alias form.
func TestValidateGeneratedFamilyType(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr bool
		errFrag string // substring expected in error message
	}{
		{
			name: "passing: named type present, alias absent",
			content: `// Code generated by bestiary-gen. DO NOT EDIT.
package bestiary

type Family string

const (
	FamilyClaude Family = "claude"
)
`,
			wantErr: false,
		},
		{
			name: "failing: named type missing (empty file content)",
			content: `// Code generated by bestiary-gen. DO NOT EDIT.
package bestiary
`,
			wantErr: true,
			errFrag: "named-type declaration not found",
		},
		{
			name: "failing: alias form present",
			content: `// Code generated by bestiary-gen. DO NOT EDIT.
package bestiary

type Family = string
`,
			wantErr: true,
			// Only the first condition fires: the alias form ("type Family = string") does NOT
			// contain the named-type declaration string ("type Family string", without "="),
			// so the namedDecl check reports the missing declaration.
			errFrag: "named-type declaration not found",
		},
		{
			name: "failing: both named and alias forms present",
			content: `// Code generated by bestiary-gen. DO NOT EDIT.
package bestiary

type Family string
type Family = string
`,
			wantErr: true,
			// Both the named form AND the alias form are present simultaneously.
			// The named-type check passes; the alias-detection check fires second.
			errFrag: "alias declaration found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "families_gen.go")
			if err := os.WriteFile(path, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("setup: write synthetic file: %v", err)
			}

			err := validateGeneratedFamilyType(path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateGeneratedFamilyType(%q): expected error, got nil", tc.name)
				}
				if tc.errFrag != "" && !strings.Contains(err.Error(), tc.errFrag) {
					t.Errorf("error message %q does not contain expected fragment %q", err.Error(), tc.errFrag)
				}
			} else {
				if err != nil {
					t.Fatalf("validateGeneratedFamilyType(%q): unexpected error: %v", tc.name, err)
				}
			}
		})
	}
}
