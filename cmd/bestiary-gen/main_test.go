package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// updateGolden is a test flag that causes TestDecompositionSnapshot to regenerate
// the golden file instead of comparing against it.
// To regenerate: go test ./cmd/bestiary-gen/... -run TestDecompositionSnapshot -update
var updateGolden = flag.Bool("update", false, "regenerate golden files instead of comparing")

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
// SLICE-FIX-V2-5 tests: Modifier slot in Model__ constants
// --------------------------------------------------------------------------

// TestNameForCanonical_ModifierSlot verifies that when a ModelInfo has a Modifier
// field set, nameForCanonicalWithMap emits the __Modifier__ slot between version
// and date in the constant name.
//
// These tests will FAIL until L3 updates nameForCanonicalWithMap to include the
// Modifier segment between version and date.
func TestNameForCanonical_ModifierSlot(t *testing.T) {
	cases := []struct {
		desc     string
		model    bestiary.ModelInfo
		wantName string
	}{
		{
			desc: "claude-opus-4-6-thinking (date not in ID, only in Date field)",
			model: bestiary.ModelInfo{
				ID:       "claude-opus-4-6-thinking",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.6",
				Date:     "2026-02-05",
				Modifier: "thinking",
			},
			// Date "2026-02-05" is NOT in the raw ID "claude-opus-4-6-thinking",
			// so dateFoundInID = false → no date suffix in constant.
			// Modifier slot "Thinking" appears between version "4_6" and end.
			// Expected: Model__Anthropic__Claude__Opus__4_6__Thinking
			wantName: "Model__Anthropic__Claude__Opus__4_6__Thinking",
		},
		{
			desc: "claude-opus-4-1-20250805-thinking with date in ID",
			model: bestiary.ModelInfo{
				ID:       "claude-opus-4-1-20250805-thinking",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.1",
				Date:     "2025-08-05",
				Modifier: "thinking",
			},
			// Compact date "20250805" IS in the raw ID → dateFoundInID = true.
			// Modifier "-thinking" is the trailing token, stripped before tokenizing.
			// After modifier+date strip: "claude-opus-4-1" → after version strip: "claude-opus"
			// Tokens: [Claude, Opus]; version: "4_1"; date: "20250805"; modifier: "Thinking"
			// Expected: Model__Anthropic__Claude__Opus__4_1__Thinking__20250805
			wantName: "Model__Anthropic__Claude__Opus__4_1__Thinking__20250805",
		},
		{
			desc: "model with modifier but no date",
			model: bestiary.ModelInfo{
				ID:       "claude-opus-4-6-thinking",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.6",
				Date:     "",
				Modifier: "thinking",
			},
			// No date → modifier becomes trailing segment.
			// Expected: Model__Anthropic__Claude__Opus__4_6__Thinking
			wantName: "Model__Anthropic__Claude__Opus__4_6__Thinking",
		},
		{
			desc: "gpt-4o-2024-05-13 (no modifier)",
			model: bestiary.ModelInfo{
				ID:       "gpt-4o-2024-05-13",
				Provider: "openai",
				Family:   "gpt",
				Variant:  "",
				Version:  "",
				Date:     "2024-05-13",
				Modifier: "",
			},
			// No modifier → no __Modifier__ slot (preserves current form).
			wantName: "Model__OpenAI__GPT__4o__20240513",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			got := nameForCanonicalWithMap(tc.model, testSlugToConst)
			if got != tc.wantName {
				t.Errorf("nameForCanonicalWithMap: got %q, want %q", got, tc.wantName)
			}
		})
	}
}

// TestTokenToConstPart_ModifierCasing verifies that modifier tokens receive the
// correct casing via tokenToConstPart (e.g. "thinking" → "Thinking").
func TestTokenToConstPart_ModifierCasing(t *testing.T) {
	cases := []struct {
		tok  string
		want string
	}{
		{"thinking", "Thinking"},
		{"vision", "Vision"},
		{"latest", "Latest"},
		{"code", "Code"},
		{"preview", "Preview"},
		{"think", "Think"},
	}
	for _, tc := range cases {
		got := tokenToConstPart(tc.tok)
		if got != tc.want {
			t.Errorf("tokenToConstPart(%q) = %q, want %q", tc.tok, got, tc.want)
		}
	}
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
// genToModelInfoDetailed fires when the model's family field is empty (~25% of real models).
// This exercises the else branch in genToModelInfoDetailed that the parse_test.go unit
// tests for InferFamilyFromID do not cover at the codegen integration layer.
func TestGenToModelInfo_EmptyFamily(t *testing.T) {
	wm := genWireModel{
		ID:     "claude-haiku-no-family",
		Name:   "Claude Haiku (no family)",
		Family: "", // empty — must trigger InferFamilyFromID
	}
	info, _ := genToModelInfoDetailed("anthropic", wm)

	if info.RawFamily != "" {
		t.Errorf("RawFamily: got %q, want empty (raw field was empty)", info.RawFamily)
	}
	// InferFamilyFromID("claude-haiku-no-family", "anthropic") must populate Family.
	if info.Family == "" {
		t.Errorf("Family: got empty; InferFamilyFromID should infer a non-empty family from ID %q", wm.ID)
	}
	// Variant may or may not be empty depending on InferFamilyFromID behavior.
	// The key property is that Family is populated (no silent no-op).
	t.Logf("genToModelInfoDetailed empty-family: Family=%q Variant=%q", info.Family, info.Variant)
}

// TestGenToModelInfo_CanonicalFields verifies that genToModelInfoDetailed correctly
// populates Family, Variant, and Date for models with known inputs.
// This guards against regressions in the genToModelInfoDetailed normalization splice path.
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
			info, _ := genToModelInfoDetailed(tc.providerSlug, tc.wm)

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

	_, _, _, _, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
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
	gotRaw, models, provMeta, _, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
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

	_, _, _, _, err := fetchModelsWithRaw(context.Background(), tmpDir, true)
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

// --------------------------------------------------------------------------
// SLICE-FIX-V2-3 tests: parse-failure audit log
// --------------------------------------------------------------------------

const parseFailuresFile = "parse_failures.json"

// failureAPIJSON returns a minimal API response with models that will produce
// parse failures at codegen time. Specifically, it includes models with
// raw_family values that trigger the YYMM-date-as-version false-positive
// (e.g. "mistral-2401") so that the failure count is predictable.
func failureAPIJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		// testprovider: a clean model with no parse failures
		"testprovider": map[string]any{
			"name": "Test Provider",
			"models": map[string]any{
				"test-model-clean": map[string]any{
					"id":     "test-model-clean",
					"name":   "Clean Test Model",
					"family": "claude-opus", // known override → no failure
				},
			},
		},
		// mistral: YYMM-date models that produce parse failures
		"mistral": map[string]any{
			"name": "Mistral",
			"models": map[string]any{
				"mistral-small-2401": map[string]any{
					"id":     "mistral-small-2401",
					"name":   "Mistral Small 2401",
					"family": "mistral-2401", // YYMM pattern → failure
				},
				"mistral-medium-2403": map[string]any{
					"id":     "mistral-medium-2403",
					"name":   "Mistral Medium 2403",
					"family": "mistral-2403", // YYMM pattern → failure
				},
			},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failureAPIJSON: marshal: %v", err)
	}
	return b
}

// TestWriteParseFailures_NonEmpty verifies that writeParseFailures writes a valid
// ParseFailuresEnvelope JSON file containing the given failures.
//
// BDD: Given bestiary-gen runs with N failures detected then
// file {cacheDir}/parse_failures.json exists with N records.
func TestWriteParseFailures_NonEmpty(t *testing.T) {
	cacheDir := t.TempDir()

	failures := []bestiary.ParseFailure{
		{
			RawID:     "mistral-2401",
			Provider:  "mistral",
			RawFamily: "mistral-2401",
			AttemptedParse: bestiary.ParseAttempt{
				Family:  "mistral",
				Variant: "",
				Version: "",
				Date:    "",
			},
			Reason: bestiary.ReasonYYMMDateAsVersion,
		},
		{
			RawID:     "mistral-2403",
			Provider:  "mistral",
			RawFamily: "mistral-2403",
			AttemptedParse: bestiary.ParseAttempt{
				Family:  "mistral",
				Variant: "",
				Version: "",
				Date:    "",
			},
			Reason: bestiary.ReasonYYMMDateAsVersion,
		},
	}

	if err := writeParseFailures(cacheDir, failures); err != nil {
		t.Fatalf("writeParseFailures: unexpected error: %v", err)
	}

	filePath := filepath.Join(cacheDir, parseFailuresFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("parse_failures.json not written: %v", err)
	}

	var envelope bestiary.ParseFailuresEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse_failures.json: invalid JSON: %v", err)
	}

	if envelope.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	if envelope.FailureCount != len(failures) {
		t.Errorf("FailureCount = %d, want %d", envelope.FailureCount, len(failures))
	}
	if len(envelope.Failures) != len(failures) {
		t.Errorf("len(Failures) = %d, want %d", len(envelope.Failures), len(failures))
	}
	if envelope.GeneratedAt.IsZero() {
		t.Error("GeneratedAt is zero; expected a valid timestamp")
	}
}

// TestWriteParseFailures_Empty verifies that writeParseFailures writes a valid
// ParseFailuresEnvelope with failure_count=0 and failures=[] when given an
// empty failures slice.
//
// BDD: Given bestiary-gen runs with zero failures then file exists with valid
// JSON envelope and failure_count: 0, failures: [].
func TestWriteParseFailures_Empty(t *testing.T) {
	cacheDir := t.TempDir()

	if err := writeParseFailures(cacheDir, nil); err != nil {
		t.Fatalf("writeParseFailures(nil): unexpected error: %v", err)
	}

	filePath := filepath.Join(cacheDir, parseFailuresFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("parse_failures.json not written for empty failures: %v", err)
	}

	var envelope bestiary.ParseFailuresEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse_failures.json (empty): invalid JSON: %v", err)
	}

	if envelope.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	if envelope.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", envelope.FailureCount)
	}
	if envelope.Failures == nil {
		// Per spec: must be an empty array, not null, in JSON.
		// encoding/json encodes nil slice as null. L3 must use []ParseFailure{} not nil.
		t.Errorf("Failures is nil (would encode as JSON null); want empty array []")
	}
	if len(envelope.Failures) != 0 {
		t.Errorf("len(Failures) = %d, want 0", len(envelope.Failures))
	}
}

// TestWriteParseFailures_OverwriteNotAppend verifies that calling writeParseFailures
// twice writes the SECOND run's failures — not the first run's combined with the second.
//
// BDD: Given bestiary-gen runs twice in succession then second run OVERWRITES
// (not appends) — file contents reflect ONLY the second run.
func TestWriteParseFailures_OverwriteNotAppend(t *testing.T) {
	cacheDir := t.TempDir()

	first := []bestiary.ParseFailure{
		{
			RawID:     "first-model",
			Provider:  "p1",
			RawFamily: "first-2401",
			AttemptedParse: bestiary.ParseAttempt{
				Family: "first",
			},
			Reason: bestiary.ReasonYYMMDateAsVersion,
		},
	}
	second := []bestiary.ParseFailure{
		{
			RawID:     "second-model",
			Provider:  "p2",
			RawFamily: "second-2403",
			AttemptedParse: bestiary.ParseAttempt{
				Family: "second",
			},
			Reason: bestiary.ReasonYYMMDateAsVersion,
		},
		{
			RawID:     "second-model-2",
			Provider:  "p2",
			RawFamily: "claude-haiku",
			AttemptedParse: bestiary.ParseAttempt{
				Family:  "claude",
				Variant: "haiku",
			},
			Reason: bestiary.ReasonVersionDigitsNotExtracted,
		},
	}

	// First write.
	if err := writeParseFailures(cacheDir, first); err != nil {
		t.Fatalf("writeParseFailures (first run): %v", err)
	}
	// Second write — must overwrite, not append.
	if err := writeParseFailures(cacheDir, second); err != nil {
		t.Fatalf("writeParseFailures (second run): %v", err)
	}

	filePath := filepath.Join(cacheDir, parseFailuresFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("parse_failures.json not found after second run: %v", err)
	}

	var envelope bestiary.ParseFailuresEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse_failures.json: invalid JSON: %v", err)
	}

	// Must reflect only the second run.
	if envelope.FailureCount != len(second) {
		t.Errorf("FailureCount = %d after second run, want %d (only second run's failures)", envelope.FailureCount, len(second))
	}
	// First run's model must NOT appear.
	for _, f := range envelope.Failures {
		if string(f.RawID) == "first-model" {
			t.Errorf("first run's entry 'first-model' found in second run's output (append bug)")
		}
	}
}

// TestWriteParseFailures_JSONRoundTrip verifies that the ParseFailuresEnvelope
// written by writeParseFailures round-trips through json.Marshal/Unmarshal
// with all fields preserved.
//
// BDD: Given the JSON file when re-parsed via json.Unmarshal into ParseFailuresEnvelope
// then round-trips equal.
func TestWriteParseFailures_JSONRoundTrip(t *testing.T) {
	cacheDir := t.TempDir()

	failures := []bestiary.ParseFailure{
		{
			RawID:     "claude-3-5-haiku-20241022",
			Provider:  "anthropic",
			RawFamily: "claude-haiku",
			AttemptedParse: bestiary.ParseAttempt{
				Family:  "claude",
				Variant: "haiku",
				Version: "",
				Date:    "2024-10-22",
			},
			Reason: bestiary.ReasonVersionDigitsNotExtracted,
		},
	}

	if err := writeParseFailures(cacheDir, failures); err != nil {
		t.Fatalf("writeParseFailures: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cacheDir, parseFailuresFile))
	if err != nil {
		t.Fatalf("read parse_failures.json: %v", err)
	}

	var envelope bestiary.ParseFailuresEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if len(envelope.Failures) != 1 {
		t.Fatalf("len(Failures) = %d, want 1", len(envelope.Failures))
	}
	f := envelope.Failures[0]
	if f.RawID != "claude-3-5-haiku-20241022" {
		t.Errorf("RawID = %q, want %q", f.RawID, "claude-3-5-haiku-20241022")
	}
	if f.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", f.Provider, "anthropic")
	}
	if f.RawFamily != "claude-haiku" {
		t.Errorf("RawFamily = %q, want %q", f.RawFamily, "claude-haiku")
	}
	if f.AttemptedParse.Family != "claude" {
		t.Errorf("AttemptedParse.Family = %q, want %q", f.AttemptedParse.Family, "claude")
	}
	if f.AttemptedParse.Variant != "haiku" {
		t.Errorf("AttemptedParse.Variant = %q, want %q", f.AttemptedParse.Variant, "haiku")
	}
	if f.AttemptedParse.Date != "2024-10-22" {
		t.Errorf("AttemptedParse.Date = %q, want %q", f.AttemptedParse.Date, "2024-10-22")
	}
	if f.Reason != bestiary.ReasonVersionDigitsNotExtracted {
		t.Errorf("Reason = %q, want %q", f.Reason, bestiary.ReasonVersionDigitsNotExtracted)
	}
}

// TestRun_WritesParseFailuresJSON verifies that run() writes parse_failures.json
// to cacheDir when it encounters models with parseable failures (e.g. YYMM-pattern
// raw_family values). This is the end-to-end integration test.
//
// BDD: Given bestiary-gen runs with N failures detected then file
// {cacheDir}/parse_failures.json exists.
func TestRun_WritesParseFailuresJSON(t *testing.T) {
	// Serve an API response with models that produce failures.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(failureAPIJSON(t))
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cacheDir := filepath.Join(tmpDir, "test-cache")

	if err := run([]string{"-cache-dir=" + cacheDir}); err != nil {
		t.Fatalf("run(): unexpected error: %v", err)
	}

	// parse_failures.json must exist.
	failuresPath := filepath.Join(cacheDir, parseFailuresFile)
	data, err := os.ReadFile(failuresPath)
	if err != nil {
		t.Fatalf("parse_failures.json not written to cacheDir %q: %v\n"+
			"  What: run() did not write parse_failures.json\n"+
			"  Why: the file-write step in run() may not be implemented yet (L3)\n"+
			"  How to fix: implement writeParseFailures call in run() (L3 task)",
			cacheDir, err)
	}

	// Must be valid JSON.
	var envelope bestiary.ParseFailuresEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse_failures.json: invalid JSON: %v\nContents: %s", err, data)
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	// failureAPIJSON injects exactly 2 YYMM-failure models (mistral-2401 + mistral-2403).
	// Asserting FailureCount guards against a regression where failure_count is always 0.
	const wantFailureCount = 2
	if envelope.FailureCount != wantFailureCount {
		t.Errorf("FailureCount = %d, want %d\n"+
			"  What: failure_count in parse_failures.json does not match expected 2 YYMM failures\n"+
			"  Why: the YYMM-date-as-version detector may have changed or FailureCount is not populated\n"+
			"  How to fix: verify ParseFamilyDetailed emits ReasonYYMMDateAsVersion for mistral-2401 and mistral-2403",
			envelope.FailureCount, wantFailureCount)
	}
	if len(envelope.Failures) != wantFailureCount {
		t.Errorf("len(Failures) = %d, want %d", len(envelope.Failures), wantFailureCount)
	}
}

// --------------------------------------------------------------------------
// SLICE-DET-1 tests: deterministic + reproducible codegen (R1, R3, R4)
// --------------------------------------------------------------------------

// normalizeWhitespace collapses all runs of whitespace in s to a single space.
// Used to compare generated Go source that may have tab-aligned columns against
// expected strings that use single spaces.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// containsNormalized reports whether the normalized form of s (whitespace collapsed)
// contains the normalized form of substr. Useful for matching against gofmt-aligned
// output where columns may have varying spaces.
func containsNormalized(s, substr string) bool {
	return strings.Contains(normalizeWhitespace(s), normalizeWhitespace(substr))
}

// reLastSynced matches a LastSynced field line in generated Go source.
// It covers both the fixture path (LastSynced: "") and the run()-stamped path
// (LastSynced: "2006-01-02T15:04:05Z"). The sentinel replaces the entire field
// value so that byte comparison is insensitive to the wall-clock timestamp.
var reLastSynced = regexp.MustCompile(`LastSynced:\s+"[^"]*"`)

const lastSyncedSentinel = `LastSynced: "__NORMALIZED__"`

// normalizeLastSynced replaces every LastSynced field value in src with a fixed
// sentinel string, making the output insensitive to the codegen wall-clock stamp.
// Use this on both sides of any byte comparison that should tolerate timestamp churn.
func normalizeLastSynced(src []byte) []byte {
	return reLastSynced.ReplaceAll(src, []byte(lastSyncedSentinel))
}

// deterministicFixtureJSON returns the hermetic fixture JSON for the reproducibility
// tests. It contains three collision groups:
//
//   - B (prefix/kilo): "openrouter/free" + "kilo-auto/free" → both produce
//     Model__Kilo__Free → resolved by raw-ID-ordered fallback (b)
//     → _1="kilo-auto/free", _2="openrouter/free"
//
//   - C (punctuation/cloudflare): "anthropic/claude-3.5-haiku" + "anthropic/claude-3-5-haiku"
//     → both produce Model__CloudflareAIGateway__Claude__3__5__Haiku
//     → resolved by raw-ID-ordered fallback (b)
//     → _1="anthropic/claude-3-5-haiku" ('-' < '.'), _2="anthropic/claude-3.5-haiku"
//
//   - E (version-pair / negative control): "gpt-5.1" + "gpt-5.2"
//     → extractVersionSegment yields distinct suffixes "5_1" / "5_2"
//     → resolved by version-suffix pass (a) — NOT the fallback
//     → constant names: Model__OpenAI__GPT__5_1 and Model__OpenAI__GPT__5_2
func deterministicFixtureJSON(t *testing.T) []byte {
	t.Helper()
	fixture := map[string]any{
		"cloudflare-ai-gateway": map[string]any{
			"name": "Cloudflare AI Gateway",
			"models": map[string]any{
				"anthropic/claude-3.5-haiku": map[string]any{
					"id":     "anthropic/claude-3.5-haiku",
					"name":   "Claude 3.5 Haiku",
					"family": "claude-haiku",
				},
				"anthropic/claude-3-5-haiku": map[string]any{
					"id":     "anthropic/claude-3-5-haiku",
					"name":   "Claude 3.5 Haiku (alt)",
					"family": "claude-haiku",
				},
			},
		},
		"kilo": map[string]any{
			"name": "Kilo",
			"models": map[string]any{
				"openrouter/free": map[string]any{
					"id":     "openrouter/free",
					"name":   "Free (OpenRouter)",
					"family": "free",
				},
				"kilo-auto/free": map[string]any{
					"id":     "kilo-auto/free",
					"name":   "Free (Kilo Auto)",
					"family": "free",
				},
			},
		},
		"openai": map[string]any{
			"name": "OpenAI",
			"models": map[string]any{
				"gpt-5.1": map[string]any{
					"id":     "gpt-5.1",
					"name":   "GPT-5.1",
					"family": "gpt",
				},
				"gpt-5.2": map[string]any{
					"id":     "gpt-5.2",
					"name":   "GPT-5.2",
					"family": "gpt",
				},
			},
		},
	}
	b, err := json.Marshal(fixture)
	if err != nil {
		t.Fatalf("deterministicFixtureJSON: marshal: %v", err)
	}
	return b
}

// runFixtureCodegen performs one full codegen cycle using the hermetic fixture:
// spin up an httptest.Server, override apiURL, call fetchModelsWithRaw, optionally
// stamp LastSynced (mirroring run() main.go:363-365), build slugToConst, and return
// both generated sources.
//
// lastSynced controls the LastSynced stamp applied to every model before source
// generation:
//   - "" (empty): no stamp — LastSynced stays "" (the pre-existing behaviour used by
//     tests that do not test the stamping path)
//   - any non-empty RFC3339 string: stamp models[i].LastSynced with that value,
//     exactly mirroring the run() pipeline path
//
// Each call re-randomizes the Go map iteration order, which is the nondeterminism
// source for bestiary-9lnq.
func runFixtureCodegen(t *testing.T, fixtureJSON []byte, lastSynced string) (staticSrc, constantsSrc []byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	_, models, provMeta, _, err := fetchModelsWithRaw(context.Background(), t.TempDir(), false)
	if err != nil {
		t.Fatalf("runFixtureCodegen: fetchModelsWithRaw: %v", err)
	}

	// Mirror run() main.go:363-365: stamp LastSynced on all models when a timestamp
	// is injected. Leaving lastSynced empty preserves the "" zero-value (fixture path).
	if lastSynced != "" {
		for i := range models {
			models[i].LastSynced = lastSynced
		}
	}

	allSlugs := make([]string, 0, len(provMeta))
	for slug := range provMeta {
		allSlugs = append(allSlugs, slug)
	}
	slugToConst := make(map[string]string, len(allSlugs))
	for _, slug := range allSlugs {
		meta := provMeta[slug]
		slugToConst[slug] = providerConstName(slug, meta.Name)
	}

	staticSrc, err = generateSource(models, slugToConst)
	if err != nil {
		t.Fatalf("runFixtureCodegen: generateSource: %v", err)
	}
	constantsSrc, err = generateConstantsSource(models, slugToConst)
	if err != nil {
		t.Fatalf("runFixtureCodegen: generateConstantsSource: %v", err)
	}
	return staticSrc, constantsSrc
}

// TestCodegen_Reproducible_ByteIdentical verifies that N=100 successive codegen
// runs over the same fixture data (each re-randomizing map iteration order via a
// fresh fetchModelsWithRaw) produce byte-identical output for BOTH generateSource
// and generateConstantsSource AFTER normalizing the LastSynced timestamp.
//
// LastSynced is injected via the same path as run() (main.go:363-365), using TWO
// DIFFERENT RFC3339 timestamps that alternate across iterations. This means:
//   - Odd iterations use tsA, even iterations use tsB.
//   - Without normalization, two differently-stamped runs WILL differ in the
//     LastSynced lines — this is proved by an explicit assertion below.
//   - After normalizeLastSynced(), the output must be byte-identical — proving
//     that LastSynced is the SOLE residual non-determinism.
//
// Additionally asserts that each raw model ID always receives the same _N suffix
// across all iterations (stable raw-ID-ordered assignment — R1 + raw-ID ordinal).
//
// Golden pins (from proposal/handoff spec):
//   - C: "anthropic/claude-3-5-haiku" always _1, "anthropic/claude-3.5-haiku" always _2
//   - B: "kilo-auto/free" always _1, "openrouter/free" always _2
//   - E (version-pair / negative control): exact constant names with NO doubled-ordinal variant
func TestCodegen_Reproducible_ByteIdentical(t *testing.T) {
	const N = 100
	// Two distinct RFC3339 timestamps used on alternating iterations to exercise the
	// run() LastSynced stamping path and prove sole-residual non-determinism.
	const tsA = "2000-01-01T00:00:00Z" // odd iterations (1, 3, 5, …)
	const tsB = "2099-12-31T23:59:59Z" // even iterations (0, 2, 4, …)

	fixtureJSON := deterministicFixtureJSON(t)

	// Run once (iteration 0 → tsB) to establish the reference output with a stamped
	// LastSynced. The reference uses the run()-equivalent stamping path.
	refStatic, refConstants := runFixtureCodegen(t, fixtureJSON, tsB)

	// Verify reference constants contain the expected golden pins.
	refStr := string(refConstants)
	// refNorm is the whitespace-normalized version for substring matching.
	refNorm := normalizeWhitespace(refStr)

	// C group pins: '-' (0x2D) < '.' (0x2E) means claude-3-5-haiku < claude-3.5-haiku.
	// With SLICE-1 parser active, version="3.5" is now extracted from both IDs
	// (family=claude, variant=haiku, version=3.5). Both map to the same base constant
	// Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5; collision suffix applies.
	if !strings.Contains(refNorm, `Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5_1 ModelID = "anthropic/claude-3-5-haiku"`) {
		t.Errorf("reference output: C group _1 pin mismatch; want anthropic/claude-3-5-haiku\nconstants:\n%s", refStr)
	}
	if !strings.Contains(refNorm, `Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5_2 ModelID = "anthropic/claude-3.5-haiku"`) {
		t.Errorf("reference output: C group _2 pin mismatch; want anthropic/claude-3.5-haiku\nconstants:\n%s", refStr)
	}
	// B group pins: kilo-auto/free < openrouter/free.
	if !strings.Contains(refNorm, `Model__Kilo__Free_1 ModelID = "kilo-auto/free"`) {
		t.Errorf("reference output: B group _1 pin mismatch; want kilo-auto/free\nconstants:\n%s", refStr)
	}
	if !strings.Contains(refNorm, `Model__Kilo__Free_2 ModelID = "openrouter/free"`) {
		t.Errorf("reference output: B group _2 pin mismatch; want openrouter/free\nconstants:\n%s", refStr)
	}
	// E control: version-suffix pass (a), not fallback. Exact constant names.
	if !strings.Contains(refNorm, `Model__OpenAI__GPT__5_1 ModelID = "gpt-5.1"`) {
		t.Errorf("reference output: E control Model__OpenAI__GPT__5_1 missing or wrong\nconstants:\n%s", refStr)
	}
	if !strings.Contains(refNorm, `Model__OpenAI__GPT__5_2 ModelID = "gpt-5.2"`) {
		t.Errorf("reference output: E control Model__OpenAI__GPT__5_2 missing or wrong\nconstants:\n%s", refStr)
	}
	// E control: assert NO fragment/doubled-ordinal variant (e.g. Model__OpenAI__GPT__5_1_1).
	// Note: Model__OpenAI__GPT__5_1 and Model__OpenAI__GPT__5_2 are distinct by version-suffix
	// pass (a), not the fallback collision suffix, so no _N suffix is appended.
	if strings.Contains(refNorm, "Model__OpenAI__GPT__5_1_") || strings.Contains(refNorm, "Model__OpenAI__GPT__5_2_") {
		t.Errorf("reference output: E control has doubled-ordinal variant (fragment suffix leaked)\nconstants:\n%s", refStr)
	}

	// Prove that the reference static file was actually stamped: it must contain tsB in a
	// LastSynced line. (The constants file does not contain LastSynced fields.)
	refStaticStr := string(refStatic)
	if !strings.Contains(refStaticStr, `LastSynced:`) || !strings.Contains(refStaticStr, tsB) {
		t.Errorf("reference static output: LastSynced stamp not found (expected %q in a LastSynced field)\n"+
			"  What: run() stamping path not exercised\n"+
			"  Why: runFixtureCodegen did not mirror run() main.go:363-365\n"+
			"  How to fix: verify that runFixtureCodegen stamps models[i].LastSynced when lastSynced != \"\"",
			tsB)
	}

	// Pre-compute normalized reference (LastSynced stripped) for byte-identity comparison.
	refStaticNorm := normalizeLastSynced(refStatic)
	refConstantsNorm := normalizeLastSynced(refConstants)

	// Build a per-rawID → constantName index from the reference for stability assertion.
	// Parse lines of the form: \t<ConstName>...<spaces>...ModelID = "<rawID>"
	// Use normalizeWhitespace per-line so the " ModelID = " split works despite gofmt alignment.
	refIDToConst := make(map[string]string)
	for _, line := range strings.Split(refStr, "\n") {
		norm := normalizeWhitespace(line)
		parts := strings.SplitN(norm, " ModelID = ", 2)
		if len(parts) != 2 {
			continue
		}
		constName := strings.TrimSpace(parts[0])
		rawID := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if constName != "" && rawID != "" {
			refIDToConst[rawID] = constName
		}
	}

	// Run N-1 more iterations and assert byte-equality after LastSynced normalization.
	// Odd iterations use tsA, even iterations use tsB (alternating across the full run).
	// At least one iteration with tsA WILL produce raw bytes that differ from the tsB
	// reference — we assert that below to prove sole-residual.
	foundDifferentRawStatic := false

	for i := 1; i < N; i++ {
		ts := tsB
		if i%2 == 1 {
			ts = tsA // odd iteration → different timestamp than reference (tsB)
		}
		staticSrc, constantsSrc := runFixtureCodegen(t, fixtureJSON, ts)

		// --- Sole-residual proof (first odd iteration) ---
		// Runs stamped with tsA must differ from the tsB reference in the raw bytes
		// of the static file (LastSynced lines differ), but ONLY in LastSynced lines.
		if ts == tsA && !foundDifferentRawStatic {
			if bytes.Equal(refStatic, staticSrc) {
				t.Errorf("sole-residual check: two runs with different timestamps produced identical raw bytes\n"+
					"  What: expected LastSynced lines to differ (tsA=%q vs tsB=%q)\n"+
					"  Why: LastSynced was not stamped by runFixtureCodegen\n"+
					"  How to fix: verify runFixtureCodegen stamps models[i].LastSynced",
					tsA, tsB)
			} else {
				foundDifferentRawStatic = true
				// Assert that every differing line contains "LastSynced" — proving it is the
				// sole source of non-determinism between two correctly-stamped runs.
				refLines := strings.Split(string(refStatic), "\n")
				iterLines := strings.Split(string(staticSrc), "\n")
				if len(refLines) == len(iterLines) {
					for lineIdx, refLine := range refLines {
						if refLine != iterLines[lineIdx] && !strings.Contains(refLine, "LastSynced") {
							t.Errorf("sole-residual check: line %d differs but does not contain 'LastSynced'\n"+
								"  What: a non-LastSynced line changed between runs with different timestamps\n"+
								"  Why: residual non-determinism beyond LastSynced exists\n"+
								"  ref:  %q\n  iter: %q",
								lineIdx+1, refLine, iterLines[lineIdx])
						}
					}
				}
			}
		}

		// --- Byte-identity after normalization ---
		staticNorm := normalizeLastSynced(staticSrc)
		constantsNorm := normalizeLastSynced(constantsSrc)

		if !bytes.Equal(refStaticNorm, staticNorm) {
			t.Fatalf("iteration %d (ts=%s): generateSource output differs from reference after LastSynced normalization\n"+
				"  What: the static model list changed between runs (beyond LastSynced)\n"+
				"  Why: R1 sort or fetchModelsWithRaw map-range is nondeterministic\n"+
				"  Where: fetchModelsWithRaw or generateSource\n"+
				"  How to fix: ensure sort.SliceStable(models, ...) runs before return in fetchModelsWithRaw",
				i+1, ts)
		}
		if !bytes.Equal(refConstantsNorm, constantsNorm) {
			t.Fatalf("iteration %d (ts=%s): generateConstantsSource output differs from reference after LastSynced normalization\n"+
				"  What: the constants file changed between runs\n"+
				"  Why: collision _N assignment is position-dependent (raw-ID ordinal not applied)\n"+
				"  Where: resolveCollisions fallback or final-uniqueness pass\n"+
				"  How to fix: replace sort.Ints(sortedPos) with raw-ID-keyed member sort in resolveCollisions",
				i+1, ts)
		}

		// Verify raw-ID → constant-name stability.
		iterStr := string(constantsSrc)
		for _, line := range strings.Split(iterStr, "\n") {
			norm := normalizeWhitespace(line)
			parts := strings.SplitN(norm, " ModelID = ", 2)
			if len(parts) != 2 {
				continue
			}
			constName := strings.TrimSpace(parts[0])
			rawID := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if constName == "" || rawID == "" {
				continue
			}
			if prev, ok := refIDToConst[rawID]; ok && prev != constName {
				t.Errorf("iteration %d: raw ID %q mapped to %q in iteration but %q in reference\n"+
					"  What: _N suffix for this raw ID changed between runs\n"+
					"  Why: raw-ID ordinal is not stable\n"+
					"  How to fix: verify resolveCollisions uses raw-ID-keyed sort",
					i+1, rawID, constName, prev)
			}
		}
	}

	// Ensure we actually hit the sole-residual check at least once.
	if !foundDifferentRawStatic {
		t.Error("sole-residual check: no odd iteration produced raw-byte differences in the static file\n" +
			"  What: the sole-residual proof never fired\n" +
			"  Why: N may be < 2 or the alternating timestamp logic is broken")
	}
}

// TestCodegen_UpToDate is the R4 up-to-date guard. It regenerates both source
// files from the hermetic fixture in-process and compares against committed golden
// excerpts in testdata/. Both sides are normalized with normalizeWhitespace
// (gofmt-alignment-insensitive). The golden files are excerpts of the expected
// output (not full files) and are substring-matched against the generated output.
//
// On mismatch: actionable error describing what differs, why it happened (forgot
// regen), and how to fix it (run `go run ./cmd/bestiary-gen --no-fetch && git add`).
func TestCodegen_UpToDate(t *testing.T) {
	// Load committed golden excerpts.
	constantsGoldenPath := filepath.Join("testdata", "expected_constants_excerpt.go.golden")
	staticGoldenPath := filepath.Join("testdata", "expected_static_excerpt.go.golden")

	constantsGoldenRaw, err := os.ReadFile(constantsGoldenPath)
	if err != nil {
		t.Fatalf("R4 guard: could not read constants golden %q: %v\n"+
			"  How to fix: ensure testdata/expected_constants_excerpt.go.golden is committed",
			constantsGoldenPath, err)
	}
	staticGoldenRaw, err := os.ReadFile(staticGoldenPath)
	if err != nil {
		t.Fatalf("R4 guard: could not read static golden %q: %v\n"+
			"  How to fix: ensure testdata/expected_static_excerpt.go.golden is committed",
			staticGoldenPath, err)
	}

	// Regenerate from the fixture.
	// Pass a representative injected timestamp to exercise the run() stamping path.
	// normalizeLastSynced is applied to both sides before comparison, so the guard
	// is insensitive to the codegen wall-clock (see bestiary-vq6k for true zero-diff).
	fixtureJSON := deterministicFixtureJSON(t)
	staticSrc, constantsSrc := runFixtureCodegen(t, fixtureJSON, "2000-01-01T00:00:00Z")

	// stripGenHeader strips the 2-line "// Code generated..." / "//go:generate..." header
	// from a generated Go file, then normalizes whitespace for comparison.
	stripGenHeader := func(src []byte) string {
		s := strings.TrimSpace(string(src))
		if strings.HasPrefix(s, "// Code generated") {
			idx := strings.Index(s, "\n")
			if idx >= 0 {
				s = strings.TrimSpace(s[idx+1:])
			}
		}
		if strings.HasPrefix(s, "//go:generate") {
			idx := strings.Index(s, "\n")
			if idx >= 0 {
				s = strings.TrimSpace(s[idx+1:])
			}
		}
		return normalizeWhitespace(s)
	}

	// normalizeAndStrip applies both normalizeLastSynced and stripGenHeader so that
	// both the wall-clock stamp and gofmt alignment are factored out.
	normalizeAndStrip := func(src []byte) string {
		return stripGenHeader(normalizeLastSynced(src))
	}

	// Normalize both sides: generated output (LastSynced stripped, header stripped) and
	// golden excerpt (LastSynced stripped). The golden file has LastSynced: "" because it
	// was produced by the fixture path; after normalization both map to the same sentinel.
	normConstants := normalizeAndStrip(constantsSrc)
	normConstantsGolden := normalizeWhitespace(string(normalizeLastSynced(constantsGoldenRaw)))

	// The golden excerpt must appear as a substring in the generated output.
	// Normalizing whitespace on both sides makes the comparison insensitive to
	// gofmt alignment and minor formatting differences.
	if !strings.Contains(normConstants, normConstantsGolden) {
		t.Errorf("R4 guard: constants file does not contain golden excerpt\n"+
			"  What: generateConstantsSource output differs from testdata/expected_constants_excerpt.go.golden\n"+
			"  Why: collision _N bindings may have changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen/main.go generateConstantsSource or resolveCollisions\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add models_constants_gen.go models_static_gen.go`\n"+
			"\nGolden excerpt (normalized, LastSynced stripped):\n%s\n\nGenerated (normalized, header+LastSynced stripped):\n%s",
			normConstantsGolden, normConstants)
	}

	normStatic := normalizeAndStrip(staticSrc)
	normStaticGolden := normalizeWhitespace(string(normalizeLastSynced(staticGoldenRaw)))

	// The golden excerpt must appear as a substring in the generated static output.
	if !strings.Contains(normStatic, normStaticGolden) {
		t.Errorf("R4 guard: static models file does not contain golden excerpt\n"+
			"  What: generateSource output differs from testdata/expected_static_excerpt.go.golden\n"+
			"  Why: model ordering changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen/main.go generateSource\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add models_constants_gen.go models_static_gen.go`\n"+
			"\nGolden excerpt (normalized, LastSynced stripped):\n%s\n\nGenerated (normalized, header+LastSynced stripped):\n%s",
			normStaticGolden, normStatic)
	}

	// Sanity-check: the constants golden must contain at least one expected binding.
	// This guards against an accidentally empty or truncated golden file.
	if !strings.Contains(string(constantsGoldenRaw), `ModelID = "anthropic/claude-3-5-haiku"`) {
		t.Errorf("R4 guard: constants golden file appears empty or truncated (missing expected binding)\n"+
			"  How to fix: ensure testdata/expected_constants_excerpt.go.golden is correctly committed")
	}
}

// TestCodegen_GoldenPins_C verifies the C group (cloudflare-ai-gateway punctuation
// collision): "anthropic/claude-3-5-haiku" → _1, "anthropic/claude-3.5-haiku" → _2.
// ASCII ordering: '-' (0x2D) < '.' (0x2E).
//
// With SLICE-1 parser active, both IDs parse to version="3.5" (family=claude,
// variant=haiku). The constant base becomes Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5;
// collision suffix _1/_2 still applies via raw-ID-ordered fallback.
func TestCodegen_GoldenPins_C(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5_1 ModelID = "anthropic/claude-3-5-haiku"`) {
		t.Errorf("C group _1 pin: expected anthropic/claude-3-5-haiku\nconstants:\n%s", string(constantsSrc))
	}
	if !strings.Contains(s, `Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5_2 ModelID = "anthropic/claude-3.5-haiku"`) {
		t.Errorf("C group _2 pin: expected anthropic/claude-3.5-haiku\nconstants:\n%s", string(constantsSrc))
	}
}

// TestCodegen_GoldenPins_B verifies the B group (kilo prefix collision):
// "kilo-auto/free" → _1, "openrouter/free" → _2.
func TestCodegen_GoldenPins_B(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Model__Kilo__Free_1 ModelID = "kilo-auto/free"`) {
		t.Errorf("B group _1 pin: expected kilo-auto/free\nconstants:\n%s", string(constantsSrc))
	}
	if !strings.Contains(s, `Model__Kilo__Free_2 ModelID = "openrouter/free"`) {
		t.Errorf("B group _2 pin: expected openrouter/free\nconstants:\n%s", string(constantsSrc))
	}
}

// TestCodegen_GoldenPins_E verifies the E group (openai version-pair negative control):
// "gpt-5.1" → Model__OpenAI__GPT__5_1, "gpt-5.2" → Model__OpenAI__GPT__5_2.
// The _1 and _2 here are VERSION DIGITS from pass (a), NOT collision suffixes from fallback (b).
// Asserts: exact names present; no fragment/doubled-ordinal variant.
func TestCodegen_GoldenPins_E(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Model__OpenAI__GPT__5_1 ModelID = "gpt-5.1"`) {
		t.Errorf("E control: Model__OpenAI__GPT__5_1 missing or wrong value\nconstants:\n%s", string(constantsSrc))
	}
	if !strings.Contains(s, `Model__OpenAI__GPT__5_2 ModelID = "gpt-5.2"`) {
		t.Errorf("E control: Model__OpenAI__GPT__5_2 missing or wrong value\nconstants:\n%s", string(constantsSrc))
	}
	// No doubled-ordinal or fragment variant (these would appear as Model__OpenAI__GPT__5_1_ in the raw form).
	rawStr := string(constantsSrc)
	if strings.Contains(rawStr, "Model__OpenAI__GPT__5_1_") {
		t.Errorf("E control: Model__OpenAI__GPT__5_1 has unexpected suffix (doubled ordinal or fragment)\nconstants:\n%s", rawStr)
	}
	if strings.Contains(rawStr, "Model__OpenAI__GPT__5_2_") {
		t.Errorf("E control: Model__OpenAI__GPT__5_2 has unexpected suffix (doubled ordinal or fragment)\nconstants:\n%s", rawStr)
	}
}

// TestCodegen_SortOrder verifies that fetchModelsWithRaw returns models sorted by
// (Provider, ID) after R1. Uses the fixture to check the expected ordering.
func TestCodegen_SortOrder(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	_, models, _, _, err := fetchModelsWithRaw(context.Background(), t.TempDir(), false)
	if err != nil {
		t.Fatalf("fetchModelsWithRaw: %v", err)
	}

	// Verify sorted order: (Provider, ID) ascending.
	for i := 1; i < len(models); i++ {
		pi := models[i-1]
		pj := models[i]
		if pi.Provider > pj.Provider {
			t.Errorf("sort order: model[%d] provider %q > model[%d] provider %q (not sorted)", i-1, pi.Provider, i, pj.Provider)
			continue
		}
		if pi.Provider == pj.Provider && pi.ID > pj.ID {
			t.Errorf("sort order: model[%d] ID %q > model[%d] ID %q within provider %q (not sorted)", i-1, pi.ID, i, pj.ID, pi.Provider)
		}
	}

	// Spot-check known order from fixture: cloudflare < kilo < openai.
	providers := make([]string, 0, len(models))
	seen := make(map[string]bool)
	for _, m := range models {
		if !seen[string(m.Provider)] {
			providers = append(providers, string(m.Provider))
			seen[string(m.Provider)] = true
		}
	}
	wantOrder := []string{"cloudflare-ai-gateway", "kilo", "openai"}
	if len(providers) != len(wantOrder) {
		t.Fatalf("expected %d providers, got %d: %v", len(wantOrder), len(providers), providers)
	}
	for i, want := range wantOrder {
		if providers[i] != want {
			t.Errorf("provider order[%d]: got %q, want %q", i, providers[i], want)
		}
	}
	// Within kilo: kilo-auto/free < openrouter/free.
	var kiloModels []string
	for _, m := range models {
		if m.Provider == "kilo" {
			kiloModels = append(kiloModels, string(m.ID))
		}
	}
	if !sort.StringsAreSorted(kiloModels) {
		t.Errorf("kilo models not sorted: %v", kiloModels)
	}
}

// --------------------------------------------------------------------------
// SLICE-2-L2 tests: decomposition snapshot + fixture R5d corpus + per-reason
// --------------------------------------------------------------------------

// decompositionSnapshotEntry records the parse decomposition for a single model.
// Sorted by (provider, model_id) for deterministic golden output.
type decompositionSnapshotEntry struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
	Family   string `json:"family"`
	Variant  string `json:"variant"`
	Version  string `json:"version"`
	Modifier string `json:"modifier"`
}

// fixtureAPIJSON returns the contents of testdata/fixture_api.json. This fixture
// contains the full R5d corpus (active-class, residual, empty-raw_family GUARD-2,
// YYMM) and is the hermetic input for TestDecompositionSnapshot.
func fixtureAPIJSON(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "fixture_api.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixtureAPIJSON: could not read %q: %v\n"+
			"  How to fix: ensure testdata/fixture_api.json is committed",
			path, err)
	}
	return data
}

// runFixtureAPICodegen is like runFixtureCodegen but uses fixtureAPIJSON (the full
// R5d corpus from testdata/fixture_api.json) instead of the collision-group-focused
// deterministicFixtureJSON. It returns all models + parse failures from the run.
func runFixtureAPICodegen(t *testing.T) (models []bestiary.ModelInfo, failures []bestiary.ParseFailure) {
	t.Helper()
	fixtureJSON := fixtureAPIJSON(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	_, models, _, failures, err := fetchModelsWithRaw(context.Background(), t.TempDir(), false)
	if err != nil {
		t.Fatalf("runFixtureAPICodegen: fetchModelsWithRaw: %v", err)
	}
	return models, failures
}

// TestDecompositionSnapshot is the R5a fixture-based decomposition snapshot test.
// It runs the full R5d corpus through fetchModelsWithRaw (which calls genToModelInfoDetailed
// → ParseFamilyDetailed) and compares the (Family, Variant, Version, Modifier) output
// per model against a committed golden file.
//
// The -update flag regenerates the golden file:
//
//	go test ./cmd/bestiary-gen/... -run TestDecompositionSnapshot -update
//
// This test is fixture-based only (NOT a real-data ==0 gate).
func TestDecompositionSnapshot(t *testing.T) {
	models, _ := runFixtureAPICodegen(t)

	// Collect decomposition entries, sorted by (provider, model_id) for determinism.
	entries := make([]decompositionSnapshotEntry, 0, len(models))
	for _, m := range models {
		entries = append(entries, decompositionSnapshotEntry{
			Provider: string(m.Provider),
			ModelID:  string(m.ID),
			Family:   string(m.Family),
			Variant:  m.Variant,
			Version:  m.Version,
			Modifier: m.Modifier,
		})
	}
	// Models from fetchModelsWithRaw are already sorted by (Provider, ID) via R1.

	got, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("TestDecompositionSnapshot: marshal entries: %v", err)
	}
	// Ensure trailing newline for consistency with golden files.
	got = append(got, '\n')

	goldenPath := filepath.Join("testdata", "decomposition_snapshot.golden.json")

	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("TestDecompositionSnapshot: write golden %q: %v", goldenPath, err)
		}
		t.Logf("Updated golden file: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("TestDecompositionSnapshot: could not read golden %q: %v\n"+
			"  How to fix: run `go test ./cmd/bestiary-gen/... -run TestDecompositionSnapshot -update` to generate",
			goldenPath, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("TestDecompositionSnapshot: decomposition mismatch\n"+
			"  What: fixture model decomposition changed vs golden\n"+
			"  Why: ParseFamilyDetailed output changed, or fixture_api.json updated without regen\n"+
			"  How to fix: run `go test ./cmd/bestiary-gen/... -run TestDecompositionSnapshot -update` to regenerate\n"+
			"\nGot:\n%s\n\nWant:\n%s",
			got, want)
	}
}

// TestDecompositionSnapshot_ActiveClassVersionPopulated asserts that each
// active-class model in the R5d fixture corpus has a non-empty Version field.
// This is the per-row version!="" check for the active class.
func TestDecompositionSnapshot_ActiveClassVersionPopulated(t *testing.T) {
	models, _ := runFixtureAPICodegen(t)

	// Active-class models: those expected to have version populated.
	// SLICE-1 parser correctly extracts version for these cases.
	// SLICE-1-FIX-2 B1: adds variant-promoted models (glm-5-turbo, phi-4-mini)
	// whose variant is now set from the sole trailing suffix after version extraction.
	// SLICE-1-FIX-4: text-embedding-3-large/small removed from active class —
	// the full-prefix-first change that enabled their B1 promotion was reverted.
	// They are now documented residuals (bestiary-ibtb, rc2 deferred).
	activeCases := map[string]struct {
		wantFamily  string
		wantVariant string
		wantVersion string
	}{
		// gpt-5-mini: raw_family=gpt-mini → family=gpt, variant=mini, version=5
		"gpt-5-mini": {wantFamily: "gpt", wantVariant: "mini", wantVersion: "5"},
		// claude-3-5-haiku: raw_family=claude-haiku → family=claude, variant=haiku, version=3.5
		"anthropic/claude-3-5-haiku": {wantFamily: "claude", wantVariant: "haiku", wantVersion: "3.5"},
		// claude-3.5-haiku: same family → same decomposition
		"anthropic/claude-3.5-haiku": {wantFamily: "claude", wantVariant: "haiku", wantVersion: "3.5"},
		// B1 promoted models surviving FIX-4 revert (single-token rawFamily, no compound prefix):
		// glm-5-turbo: raw_family=glm → family=glm, variant=turbo (B1), version=5
		"glm-5-turbo": {wantFamily: "glm", wantVariant: "turbo", wantVersion: "5"},
		// phi-4-mini: raw_family=phi → family=phi, variant=mini (B1), version=4
		"phi-4-mini": {wantFamily: "phi", wantVariant: "mini", wantVersion: "4"},
	}

	modelsByID := make(map[string]bestiary.ModelInfo, len(models))
	for _, m := range models {
		modelsByID[string(m.ID)] = m
	}

	for id, want := range activeCases {
		m, ok := modelsByID[id]
		if !ok {
			t.Errorf("active-class model %q not found in fixture output", id)
			continue
		}
		if m.Version == "" {
			t.Errorf("active-class model %q: Version is empty, want %q\n"+
				"  What: SLICE-1 parser should populate Version for active-class models\n"+
				"  Why: ParseFamilyDetailed may not be extracting version from model ID\n"+
				"  How to fix: verify ParseFamilyDetailed returns version for family=%q, id=%q",
				id, want.wantVersion, want.wantFamily, id)
		} else if m.Version != want.wantVersion {
			t.Errorf("active-class model %q: Version = %q, want %q", id, m.Version, want.wantVersion)
		}
		if string(m.Family) != want.wantFamily {
			t.Errorf("active-class model %q: Family = %q, want %q", id, m.Family, want.wantFamily)
		}
		if m.Variant != want.wantVariant {
			t.Errorf("active-class model %q: Variant = %q, want %q\n"+
				"  What: B1 promotion may not have fired (sole trailing suffix not promoted)\n"+
				"  Why: FIX B1 should set Variant=<suffix> when exactly one residual is a known variant suffix",
				id, m.Variant, want.wantVariant)
		}
	}
}

// TestDecompositionSnapshot_FixA_NoVersionForBare4Digit verifies FIX-A in the
// fixture corpus: deepseek-r1-0528 and deepseek-v3-0324 must have Version="" because
// "0528" and "0324" are bare 4-digit date tokens (MMDD format), not semantic versions.
//
// This is the per-row version=="" check for FIX-A models (fixture-based, not real-data).
func TestDecompositionSnapshot_FixA_NoVersionForBare4Digit(t *testing.T) {
	models, _ := runFixtureAPICodegen(t)

	modelsByID := make(map[string]bestiary.ModelInfo, len(models))
	for _, m := range models {
		modelsByID[string(m.ID)] = m
	}

	fixACases := map[string]struct {
		wantFamily  string
		wantVersion string // must be empty
	}{
		"deepseek-r1-0528": {wantFamily: "deepseek-r1", wantVersion: ""},
		"deepseek-v3-0324": {wantFamily: "deepseek", wantVersion: ""},
	}

	for id, want := range fixACases {
		m, ok := modelsByID[id]
		if !ok {
			t.Errorf("FIX-A model %q not found in fixture output", id)
			continue
		}
		if m.Version != want.wantVersion {
			t.Errorf("FIX-A model %q: Version = %q, want %q (bare 4-digit token must not be a version)\n"+
				"  What: 4-digit date-like token was extracted as a version\n"+
				"  Why: FIX-A extends isYYMMDateToken to reject any 4-digit all-numeric token\n"+
				"  How to fix: verify isYYMMDateToken returns true for \"0528\" and \"0324\"",
				id, m.Version, want.wantVersion)
		}
		if string(m.Family) != want.wantFamily {
			t.Errorf("FIX-A model %q: Family = %q, want %q", id, m.Family, want.wantFamily)
		}
	}
}

// --------------------------------------------------------------------------
// SLICE-2-L3 tests: version_duplicates.json + dot_form_audit.json + smoke check
// --------------------------------------------------------------------------

// TestRun_WritesVersionDuplicates verifies that run() writes version_duplicates.json
// to the cache directory when models share (provider, family, variant, version).
// Uses fixture_api.json which contains haiku models that both resolve to version="3.5"
// under cloudflare-ai-gateway (same provider/family/variant/version → duplicate group).
func TestRun_WritesVersionDuplicates(t *testing.T) {
	fixtureJSON := fixtureAPIJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cacheDir := filepath.Join(tmpDir, "test-cache")
	if err := run([]string{"-cache-dir=" + cacheDir}); err != nil {
		t.Fatalf("run(): unexpected error: %v", err)
	}

	// version_duplicates.json must exist.
	dupPath := filepath.Join(cacheDir, versionDuplicatesFile)
	data, err := os.ReadFile(dupPath)
	if err != nil {
		t.Fatalf("version_duplicates.json not written to cacheDir %q: %v\n"+
			"  How to fix: verify writeVersionDuplicates is called in run()",
			cacheDir, err)
	}

	// Must be valid JSON.
	var envelope VersionDuplicatesEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("version_duplicates.json: invalid JSON: %v\nContents: %s", err, data)
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("version_duplicates.json SchemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	// fixture_api.json has two haiku models under cloudflare-ai-gateway, both with
	// version="3.5" (family=claude, variant=haiku). They should form one duplicate group.
	if envelope.DuplicateCount == 0 {
		t.Errorf("version_duplicates.json DuplicateCount = 0, want > 0\n"+
			"  What: expected at least one duplicate group from haiku models\n"+
			"  Why: both claude-haiku models in fixture_api.json resolve to version=3.5\n"+
			"  How to fix: verify writeVersionDuplicates collects (provider,family,variant,version) groups")
	}
}

// TestRun_WritesDotFormAudit verifies that run() writes dot_form_audit.json with
// models whose Version contains a dot (dot-form populated).
func TestRun_WritesDotFormAudit(t *testing.T) {
	fixtureJSON := fixtureAPIJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := apiURL
	apiURL = srv.URL
	defer func() { apiURL = origURL }()

	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to tmpDir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cacheDir := filepath.Join(tmpDir, "test-cache")
	if err := run([]string{"-cache-dir=" + cacheDir}); err != nil {
		t.Fatalf("run(): unexpected error: %v", err)
	}

	// dot_form_audit.json must exist.
	auditPath := filepath.Join(cacheDir, dotFormAuditFile)
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("dot_form_audit.json not written to cacheDir %q: %v\n"+
			"  How to fix: verify writeDotFormAudit is called in run()",
			cacheDir, err)
	}

	// Must be valid JSON.
	var envelope DotFormAuditEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("dot_form_audit.json: invalid JSON: %v\nContents: %s", err, data)
	}
	if envelope.SchemaVersion != 1 {
		t.Errorf("dot_form_audit.json SchemaVersion = %d, want 1", envelope.SchemaVersion)
	}
	// fixture_api.json has: claude-3-5-haiku (version=3.5), claude-3.5-haiku (version=3.5),
	// gpt-5.1 (version=5.1), gpt-5.2 (version=5.2) — all with dot-form versions.
	if envelope.Count == 0 {
		t.Errorf("dot_form_audit.json Count = 0, want > 0\n"+
			"  What: expected models with dot-form versions (e.g. 3.5, 5.1)\n"+
			"  Why: fixture_api.json contains multiple models with dot-separated versions\n"+
			"  How to fix: verify writeDotFormAudit checks for Version containing '.'")
	}
}

// TestWriteVersionDuplicates_Unit is a unit test for the writeVersionDuplicates function.
func TestWriteVersionDuplicates_Unit(t *testing.T) {
	cacheDir := t.TempDir()
	models := []bestiary.ModelInfo{
		// Two models with same (provider, family, variant, version) → duplicate group.
		{ID: "claude-3-5-haiku", Provider: "anthropic", Family: "claude", Variant: "haiku", Version: "3.5"},
		{ID: "claude-3.5-haiku", Provider: "anthropic", Family: "claude", Variant: "haiku", Version: "3.5"},
		// One model with unique key → no duplicate.
		{ID: "gpt-5.1", Provider: "openai", Family: "gpt", Variant: "", Version: "5.1"},
		// Models with no version → skipped.
		{ID: "some-model", Provider: "provider", Family: "family", Variant: "", Version: ""},
	}
	if err := writeVersionDuplicates(cacheDir, models); err != nil {
		t.Fatalf("writeVersionDuplicates: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cacheDir, versionDuplicatesFile))
	if err != nil {
		t.Fatalf("read version_duplicates.json: %v", err)
	}
	var envelope VersionDuplicatesEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal version_duplicates.json: %v", err)
	}
	if envelope.DuplicateCount != 1 {
		t.Errorf("DuplicateCount = %d, want 1", envelope.DuplicateCount)
	}
	if len(envelope.Duplicates) != 1 {
		t.Fatalf("len(Duplicates) = %d, want 1", len(envelope.Duplicates))
	}
	g := envelope.Duplicates[0]
	if g.Key.Provider != "anthropic" || g.Key.Family != "claude" || g.Key.Variant != "haiku" || g.Key.Version != "3.5" {
		t.Errorf("duplicate group key = %+v, want {anthropic, claude, haiku, 3.5}", g.Key)
	}
	if len(g.ModelIDs) != 2 {
		t.Errorf("ModelIDs = %v, want 2 entries", g.ModelIDs)
	}
}

// TestWriteDotFormAudit_Unit is a unit test for the writeDotFormAudit function.
func TestWriteDotFormAudit_Unit(t *testing.T) {
	cacheDir := t.TempDir()
	models := []bestiary.ModelInfo{
		{ID: "claude-3.5-haiku", Provider: "anthropic", Version: "3.5"},  // dot-form
		{ID: "gpt-5.1", Provider: "openai", Version: "5.1"},               // dot-form
		{ID: "gpt-5-mini", Provider: "openai", Version: "5"},              // no dot — not in audit
		{ID: "nova-2-lite-v1", Provider: "cartesia", Version: "2"},        // no dot — not in audit
		{ID: "no-version", Provider: "test", Version: ""},                  // empty — not in audit
	}
	if err := writeDotFormAudit(cacheDir, models); err != nil {
		t.Fatalf("writeDotFormAudit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(cacheDir, dotFormAuditFile))
	if err != nil {
		t.Fatalf("read dot_form_audit.json: %v", err)
	}
	var envelope DotFormAuditEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("unmarshal dot_form_audit.json: %v", err)
	}
	if envelope.Count != 2 {
		t.Errorf("Count = %d, want 2 (claude-3.5-haiku + gpt-5.1)", envelope.Count)
	}
}

// TestFixturePerReasonCounts asserts per-reason FailureCount expectations over the
// full R5d fixture corpus. This mirrors the TestRun_WritesParseFailuresJSON pattern
// but uses fixture_api.json instead of failureAPIJSON.
//
// Expectations:
//   - ReasonVersionDigitsNotExtracted (active class) → 0: SLICE-1 now correctly
//     extracts version from IDs like claude-3-5-haiku; this failure should no longer fire.
//   - ReasonResidualUnaccountedTokens → at least 4 (SLICE-1-FIX-4 updated):
//     nova-2-lite-v1 (C: variant pre-set), phi-3-medium-128k-instruct (B2: multi-residual),
//     text-embedding-3-large + text-embedding-3-small (FIX-4 documented residuals —
//     full-prefix-first reverted; bestiary-ibtb tracks rc2 fix).
//   - ReasonYYMMDateAsVersion → at least 1: mistral-small-2603 (family mistral-2603)
//     triggers the YYMM false-positive detector.
//   - FIX-A confirmation: deepseek-r1-0528 / deepseek-v3-0324 produce NO failure
//     (bare 4-digit date tokens are now rejected as versions, not residual).
//   - FIX-B1 confirmation (SLICE-1-FIX-4 updated): glm-5-turbo / phi-4-mini produce NO failure
//     (single-token rawFamily; B1 still fires). text-embedding-3-* removed (now residual).
//
// This test is NOT a ==0 gate on real data — fixture-based only (Plan UAT decision).
func TestFixturePerReasonCounts(t *testing.T) {
	_, failures := runFixtureAPICodegen(t)

	// Count per-reason occurrences.
	counts := make(map[bestiary.ParseFailureReason]int)
	// Build per-model failure lookup for FIX-A/B1 spot checks.
	failsByID := make(map[string]bestiary.ParseFailureReason)
	for _, f := range failures {
		counts[f.Reason]++
		failsByID[string(f.RawID)] = f.Reason
	}

	// Active class: ReasonVersionDigitsNotExtracted must be 0.
	// With SLICE-1 parser, version is now correctly extracted for claude-3-5-haiku
	// and similar active-class models, so this failure reason must NOT appear.
	if n := counts[bestiary.ReasonVersionDigitsNotExtracted]; n != 0 {
		t.Errorf("ReasonVersionDigitsNotExtracted = %d, want 0\n"+
			"  What: SLICE-1 should suppress this failure for active-class models\n"+
			"  Why: ParseFamilyDetailed now extracts version via Δ1 extract-first path\n"+
			"  How to fix: verify ParseFamilyDetailed does not emit ReasonVersionDigitsNotExtracted for claude-3-5-haiku etc.",
			n)
	}

	// Residual: ReasonResidualUnaccountedTokens must be >= 4 after SLICE-1-FIX-4:
	// nova-2-lite-v1 (C: variant pre-set, "v1" residual after variant) +
	// phi-3-medium-128k-instruct (B2: multi-residual) +
	// text-embedding-3-large (FIX-4 documented residual: full-prefix-first reverted) +
	// text-embedding-3-small (same).
	// After FIX-B1, glm-5-turbo/phi-4-mini are promoted (single-token rawFamily, B1 applies).
	if n := counts[bestiary.ReasonResidualUnaccountedTokens]; n < 4 {
		t.Errorf("ReasonResidualUnaccountedTokens = %d, want >= 4\n"+
			"  What: nova-2-lite-v1 (C) + phi-3-medium-128k-instruct (B2) + text-embedding-3-large/small (FIX-4 residual)\n"+
			"    should produce residual failures\n"+
			"  Why: FIX-4 reverted full-prefix-first; text-embedding models now have compound residual tokens\n"+
			"  How to fix: verify fixture_api.json includes all four models",
			n)
	}

	// YYMM: ReasonYYMMDateAsVersion must be > 0 (mistral-small-2603 contributes).
	if n := counts[bestiary.ReasonYYMMDateAsVersion]; n == 0 {
		t.Errorf("ReasonYYMMDateAsVersion = 0, want > 0\n"+
			"  What: mistral-small-2603 (family mistral-2603) should produce a YYMM failure\n"+
			"  Why: ParseFamilyDetailed YYMM detector fires for families matching the YYMM pattern\n"+
			"  How to fix: verify fixture_api.json includes mistral-small-2603 under mistral provider")
	}

	// FIX-A spot check: deepseek-r1-0528 and deepseek-v3-0324 must NOT appear in
	// failures. Their bare 4-digit date tokens ("0528", "0324") are now rejected as
	// versions → no version extracted → no residual failure.
	for _, fixAID := range []string{"deepseek-r1-0528", "deepseek-v3-0324"} {
		if reason, found := failsByID[fixAID]; found {
			t.Errorf("FIX-A model %q produced a failure (reason=%q), want no failure\n"+
				"  What: bare 4-digit date token was not suppressed\n"+
				"  Why: FIX-A should extend isYYMMDateToken to reject 4-digit all-numeric tokens\n"+
				"  How to fix: verify isYYMMDateToken returns true for \"0528\" and \"0324\"",
				fixAID, reason)
		}
	}

	// FIX-B1 spot check (SLICE-1-FIX-4 updated): glm-5-turbo and phi-4-mini must NOT
	// appear in failures. Their single-token rawFamily ("glm", "phi") means B1 fires
	// correctly (sole trailing suffix promoted, no compound prefix issue).
	// text-embedding-3-large/small are removed from this check — they are now documented
	// residuals after SLICE-1-FIX-4 reverted the full-prefix-first change (bestiary-ibtb).
	for _, fixB1ID := range []string{"glm-5-turbo", "phi-4-mini"} {
		if reason, found := failsByID[fixB1ID]; found {
			t.Errorf("FIX-B1 model %q produced a failure (reason=%q), want no failure\n"+
				"  What: sole trailing known-suffix was not promoted into Variant\n"+
				"  Why: FIX-B1 should suppress ReasonResidualUnaccountedTokens when sole residual is a known suffix\n"+
				"  How to fix: verify B1 promotion logic in ParseFamilyDetailed",
				fixB1ID, reason)
		}
	}
}
