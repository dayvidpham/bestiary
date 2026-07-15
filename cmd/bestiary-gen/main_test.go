package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
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
		// Single-token brand-casing.
		{"xai", "xAI", "xAI"},
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
		{"xai", "xAI", "ProviderxAI"},
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
// tests: nameForCanonical, resolveCollisions, generateConstantsSource
// --------------------------------------------------------------------------

// testSlugToConst is a minimal slugToConst map for tests, providing the correct
// provider constant names (with proper casing) for the providers used in golden examples.
var testSlugToConst = map[string]string{
	"anthropic":     "ProviderAnthropic",
	"openai":        "ProviderOpenAI",
	"google":        "ProviderGoogle",
	"google-vertex": "ProviderGoogleVertex",
	"openrouter":    "ProviderOpenRouter",
}

// TestNameForCanonical_KnownExamples verifies that nameForCanonicalWithMap produces
// the expected constant names for the spec-defined golden examples.
// Updated to new double-underscore template: Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
// (double underscores between components, single underscores within components).
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
				ID:       "claude-opus-4-1",
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
	// Both produce "Model__Anthropic__Claude__Opus" as the naive name (double-underscore).
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
	// Must contain the expected constant names (double-underscore between components).
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
// Tests: Modifier slot in Model__ constants
// --------------------------------------------------------------------------

// TestNameForCanonical_ModifierSlot verifies that when a ModelInfo has a Modifier
// field set, nameForCanonicalWithMap emits the __Modifier__ slot between version
// and date in the constant name.
//
// These tests will FAIL until nameForCanonicalWithMap is updated to include the
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
				Modifier: []string{"thinking"},
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
				Modifier: []string{"thinking"},
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
				Modifier: []string{"thinking"},
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
				Modifier: nil,
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

// genBase constructs the library-shaped base ModelInfo that ParseCatalogJSON would
// hand enrichModelInfo: the RAW family lives on the Family field (RawFamily unset), and
// the api.json-side facts are already decoded. It mirrors the toModelInfo contract
// enrichModelInfo depends on.
func genBase(providerSlug, id, name, rawFamily, releaseDate string) bestiary.ModelInfo {
	return bestiary.ModelInfo{
		ID:          bestiary.ModelID(id),
		Provider:    bestiary.Provider(providerSlug),
		DisplayName: name,
		Family:      bestiary.Family(rawFamily), // library sets Family = raw family
		ReleaseDate: releaseDate,
	}
}

// TestGenToModelInfo_EmptyFamily verifies that the InferFamilyFromID code path in
// enrichModelInfo fires when the model's family field is empty (~25% of real models).
// This exercises the else branch in enrichModelInfo that the parse_test.go unit tests
// for InferFamilyFromID do not cover at the codegen integration layer.
func TestGenToModelInfo_EmptyFamily(t *testing.T) {
	base := genBase("anthropic", "claude-haiku-no-family", "Claude Haiku (no family)", "", "")
	info, _ := enrichModelInfo(base)

	if info.RawFamily != "" {
		t.Errorf("RawFamily: got %q, want empty (raw field was empty)", info.RawFamily)
	}
	// InferFamilyFromID("claude-haiku-no-family", "anthropic") must populate Family.
	if info.Family == "" {
		t.Errorf("Family: got empty; InferFamilyFromID should infer a non-empty family from ID %q", base.ID)
	}
	// Variant may or may not be empty depending on InferFamilyFromID behavior.
	// The key property is that Family is populated (no silent no-op).
	t.Logf("enrichModelInfo empty-family: Family=%q Variant=%q", info.Family, info.Variant)
}

// TestGenToModelInfo_CanonicalFields verifies that enrichModelInfo correctly populates
// Family, Variant, and Date for models with known inputs.
// This guards against regressions in the enrichModelInfo normalization splice path.
func TestGenToModelInfo_CanonicalFields(t *testing.T) {
	cases := []struct {
		desc             string
		base             bestiary.ModelInfo
		wantFamily       string
		wantVariant      string
		wantDateContains string // substring of Date (may be empty for no-date models)
	}{
		{
			desc:             "claude-opus-4-20250514: family=claude-opus, date in ID",
			base:             genBase("anthropic", "claude-opus-4-20250514", "Claude Opus 4", "claude-opus", "2025-05-14"),
			wantFamily:       "claude",
			wantVariant:      "opus",
			wantDateContains: "2025-05-14",
		},
		{
			desc: "gpt-4o-2024-08-06: family=gpt, variant=4o, date in ID",
			base: genBase("openai", "gpt-4o-2024-08-06", "GPT-4o", "gpt-4o", "2024-08-06"),
			// gpt-4o decomposes to family "gpt" with the
			// line designator "4o" as the VARIANT (version empty); Date still from the ID.
			wantFamily:       "gpt",
			wantVariant:      "4o",
			wantDateContains: "2024-08-06",
		},
		{
			desc:             "empty family: Family inferred from ID",
			base:             genBase("anthropic", "claude-haiku-no-family", "Claude Haiku", "", ""),
			wantFamily:       "claude", // InferFamilyFromID should infer "claude"
			wantDateContains: "",       // no date
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			info, _ := enrichModelInfo(tc.base)

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

// minimalCatalogJSON returns a minimal valid catalog.json body ({models, providers})
// for tests: one provider ("testprovider") with one model, and an empty metadata view.
func minimalCatalogJSON(t *testing.T) []byte {
	t.Helper()
	payload := map[string]any{
		"providers": map[string]any{
			"testprovider": map[string]any{
				"name": "Test Provider",
				"models": map[string]any{
					"test-model-1": map[string]any{
						"id":   "test-model-1",
						"name": "Test Model 1",
					},
				},
			},
		},
		"models": map[string]any{},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("minimalCatalogJSON: marshal: %v", err)
	}
	return b
}

// withVendoredCatalog points vendoredCatalogPath at a fresh temp file (or, when body is
// nil, at a path that does not exist) and restores it on cleanup. It returns the path.
func withVendoredCatalog(t *testing.T, body []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	if body != nil {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("withVendoredCatalog: write %q: %v", path, err)
		}
	}
	orig := vendoredCatalogPath
	vendoredCatalogPath = path
	t.Cleanup(func() { vendoredCatalogPath = orig })
	return path
}

// TestNoFetch_CorruptVendoredCatalog verifies that --no-fetch with a corrupt committed
// catalog.json returns a LOUD actionable error naming the path and the refresh workflow
// — codegen NEVER degrades to an empty catalog. Exercises the ParseCatalogJSON error
// path in fetchModelsWithRaw.
func TestNoFetch_CorruptVendoredCatalog(t *testing.T) {
	path := withVendoredCatalog(t, []byte("{not valid json"))

	_, _, _, _, _, err := fetchModelsWithRaw(context.Background(), true)
	if err == nil {
		t.Fatal("fetchModelsWithRaw(noFetch=true, corrupt vendored catalog): expected error, got nil")
	}
	msg := err.Error()
	// The error must reference the decode failure of the vendored catalog.
	if !strings.Contains(msg, "catalog.json") {
		t.Errorf("error for corrupt vendored catalog does not mention catalog.json:\n%s", msg)
	}
	// It must never claim to have degraded to an empty catalog — it must point at the fix.
	if !strings.Contains(msg, "never degrades to an empty catalog") {
		t.Errorf("error must state it never degrades to an empty catalog:\n%s", msg)
	}
	// Actionable: name the refresh workflow so a curator knows how to recover.
	if !strings.Contains(msg, "snapshot refresh") {
		t.Errorf("error must name the snapshot refresh workflow:\n%s", msg)
	}
	// It must name the offending path.
	absPath, _ := filepath.Abs(path)
	if !strings.Contains(msg, absPath) {
		t.Errorf("error must name the corrupt file path %q:\n%s", absPath, msg)
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

// TestCacheDirFlag verifies that a fetch-mode run rewrites the vendored catalog.json +
// SNAPSHOT.json and routes diagnostics to --cache-dir (not the default
// .bestiary-gen-cache). It exercises the full run() code path end-to-end via a catalogURL
// override, chdir'd into a temp dir so the vendored rewrite and generated files stay
// isolated from the repo.
func TestCacheDirFlag(t *testing.T) {
	// Serve a minimal catalog.json over HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", "\"test-etag\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(minimalCatalogJSON(t))
	}))
	defer srv.Close()

	// Override the package-level catalogURL var so run() fetches from the test server.
	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

	// run() writes generated .go files AND the vendored snapshot to paths relative to
	// the working directory; use a temp dir as cwd so we don't pollute the repo root.
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

	// Call run() with --cache-dir pointing to our custom directory (fetch mode).
	if err := run([]string{"-cache-dir=" + cacheDir}); err != nil {
		t.Fatalf("run(-cache-dir=%s): unexpected error: %v", cacheDir, err)
	}

	// The fetch REWRITES the vendored catalog.json + SNAPSHOT.json (relative paths →
	// under tmpDir). Both must exist and be non-empty.
	for _, rel := range []string{vendoredCatalogPath, snapshotManifestPath} {
		info, statErr := os.Stat(filepath.Join(tmpDir, rel))
		if statErr != nil {
			t.Fatalf("vendored file %q not written on fetch: %v", rel, statErr)
		}
		if info.Size() == 0 {
			t.Fatalf("vendored file %q is empty; expected non-empty content", rel)
		}
	}

	// Diagnostics (parse_failures.json) must go to --cache-dir, not the default.
	if _, statErr := os.Stat(filepath.Join(cacheDir, "parse_failures.json")); statErr != nil {
		t.Fatalf("parse_failures.json not written to --cache-dir %q: %v", cacheDir, statErr)
	}
	defaultPath := filepath.Join(tmpDir, defaultCacheDir)
	if _, statErr := os.Stat(defaultPath); statErr == nil {
		t.Errorf("default cache dir %q was created; run() should only write diagnostics to --cache-dir", defaultPath)
	}
}

// TestNoFetch_ReadsVendoredCatalog verifies that --no-fetch reads the committed
// catalog.json without making any HTTP request. A guard server wired into catalogURL
// trips the contacted flag if fetchModelsWithRaw ever dials out.
func TestNoFetch_ReadsVendoredCatalog(t *testing.T) {
	withVendoredCatalog(t, minimalCatalogJSON(t))

	// A test server that fails the test if contacted — no HTTP should happen.
	contacted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		http.Error(w, "should not be called", http.StatusInternalServerError)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

	gotRaw, models, _, provMeta, _, err := fetchModelsWithRaw(context.Background(), true)
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

// TestNoFetch_MissingVendoredCatalog_ActionableError verifies that --no-fetch with a
// missing committed catalog.json returns an *ErrVendoredCatalogMissing carrying all six
// actionable fields: What, Why, Where, When, what-it-means, how-to-fix (naming the
// refresh workflow). Codegen must never degrade to an empty catalog.
func TestNoFetch_MissingVendoredCatalog_ActionableError(t *testing.T) {
	// Point at a path that does NOT exist (body == nil).
	path := withVendoredCatalog(t, nil)

	_, _, _, _, _, err := fetchModelsWithRaw(context.Background(), true)
	if err == nil {
		t.Fatal("fetchModelsWithRaw(noFetch=true, missing vendored catalog): expected error, got nil")
	}

	var missing *ErrVendoredCatalogMissing
	if !errors.As(err, &missing) {
		t.Fatalf("expected *ErrVendoredCatalogMissing, got %T: %v", err, err)
	}

	absWant, _ := filepath.Abs(path)
	msg := missing.Error()

	// (1) What: the missing/empty committed input.
	if !strings.Contains(msg, "vendored models.dev catalog.json missing or empty") {
		t.Errorf("error message missing 'What' field:\n%s", msg)
	}
	// (2) Why: --no-fetch skipped the fetch.
	if !strings.Contains(msg, "--no-fetch") {
		t.Errorf("error message missing 'Why' field (--no-fetch):\n%s", msg)
	}
	// (3) Where: the missing path.
	if !strings.Contains(msg, absWant) {
		t.Errorf("error message missing 'Where' field (path %q):\n%s", absWant, msg)
	}
	// (4) When: the load step.
	if !strings.Contains(msg, "fetchModelsWithRaw") {
		t.Errorf("error message missing 'When' field (fetchModelsWithRaw):\n%s", msg)
	}
	// (5) What it means: never degrades to an empty catalog.
	if !strings.Contains(msg, "never degrades to an empty catalog") {
		t.Errorf("error message missing 'what-it-means' field:\n%s", msg)
	}
	// (6) How to fix: the snapshot-refresh workflow.
	if !strings.Contains(msg, "snapshot refresh") {
		t.Errorf("error message missing 'how-to-fix' field (snapshot refresh):\n%s", msg)
	}
}

// --------------------------------------------------------------------------
// Cross-provider decomposition consistency tests
// --------------------------------------------------------------------------

// crossProviderJustifiedResidual is the ENUMERATED justified-residual
// ledger for cross-provider (Family,Variant,Version) divergences over the COMMITTED
// snapshot. The hardened gate asserts SET-EQUALITY: the divergent-ID set produced by the
// production pipeline must equal EXACTLY this set. Each row carries a one-line
// justification. The only justified residual is the embedded-family nemotron
// (the ID leads with "llama" but the canonical family is "nemotron"; GH-followup).
// SET-equality (not count) catches a DIFFERENT id going divergent while nemotron converges
// — count would stay 1, the set would change. Do NOT pad this map to force green: every
// row must be independently justified; an unexpected divergence is a STOP-and-surface.
// Now EMPTY. The sole prior residual
// (nvidia/llama-3.3-nemotron-super-49b-v1.5) was FOLDED to family nemotron via the curated
// idFamilyOverrides entry — both providers converge on (nemotron,v1.5,3.3). Cross-provider
// (Family,Variant,Version) divergence is now ZERO, so the divergent-ID SET is empty and this
// justified-residual ledger is empty too (SET-equality holds at the empty set).
var crossProviderJustifiedResidual = map[string]string{}

// crossProviderResidualUnaccountedCeiling pins the at-scale count of
// ReasonResidualUnaccountedTokens over the committed snapshot.
// Today only the non-gating stdout smoke (main.go) sees this; pinning it catches a
// non-fixture-family residual regression (the seed-flash class) that would otherwise slip
// every gate. Currently measured = 243; assert ≤ ceiling (tighten-only; a legitimate
// reduction passes, a regression that re-drops sole-residual/member coverage trips it).
const crossProviderResidualUnaccountedCeiling = 243

// crossProviderPopulatedVersionFloor pins the at-scale count of snapshot records whose
// production decomposition yields a NON-EMPTY Version (landing pin; supersedes the
// stale 1681/293 figures). Currently measured = 3401 over 4979 records. Assert ≥ floor
// (loosen-only: more version coverage passes; a regression that drops version-presence
// — the inverse of the residual-ceiling guard — trips it). Pinned alongside the residual
// ceiling so both the "version populated" and "tokens unaccounted" at-scale counts are gated.
const crossProviderPopulatedVersionFloor = 3401

// TestStaticDataset_CrossProviderConsistency is the HARDENED cross-provider
// consistency GATE. It REPLACES the earlier heuristic gate that carried 5 escape
// hatches (namespaced-ID `/.@:` skip; no-empty-raw skip; no-populated-raw skip;
// populated-providers-disagree skip; Infer-consensus-can't-derive skip) — those hid
// 331/388 of the original divergences.
//
// It decomposes the COMMITTED full snapshot (testdata/snapshot/models_api.json) via the
// PRODUCTION pipeline (ParseFamilyDetailed) — the SAME data + pipeline that
// TestSnapshotAnalysis_CrossProviderDivergences uses, so the two gates MUST agree — groups
// by model ID across providers, and asserts the cross-provider (Family,Variant,Version)
// divergent-ID SET == crossProviderJustifiedResidual (SET-equality, no escape hatches).
// It also pins the ReasonResidualUnaccountedTokens count ≤ ceiling.
//
// The Modifier list is compared ORDER-INDEPENDENTLY everywhere else (resolve
// the group-key invariant, path_unification cmp(), TestPathUnification_ModifierSetIndependence);
// this gate's PRIMARY tuple is (Family,Variant,Version) per the ratified consistency metric,
// so a permuted-modifier pair across providers structurally cannot register here.
func TestStaticDataset_CrossProviderConsistency(t *testing.T) {
	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("TestStaticDataset_CrossProviderConsistency: LoadSnapshotRecords: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("committed snapshot is empty — cannot run the cross-provider gate")
	}

	type tuple3 struct {
		Family  bestiary.Family
		Variant string
		Version string
	}
	byID := make(map[string]map[tuple3]struct{})
	residualUnaccounted := 0
	populatedVersion := 0
	for _, r := range records {
		fam, variant, version, _, failure := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		id := string(r.ID)
		if byID[id] == nil {
			byID[id] = make(map[tuple3]struct{})
		}
		byID[id][tuple3{fam, variant, version}] = struct{}{}
		if failure != nil && failure.Reason == bestiary.ReasonResidualUnaccountedTokens {
			residualUnaccounted++
		}
		if version != "" {
			populatedVersion++
		}
	}

	// Compute the divergent-ID SET: multi-provider IDs whose (Family,Variant,Version)
	// tuples are NOT all identical.
	divergent := make(map[string]struct{})
	for id, tuples := range byID {
		if len(tuples) >= 2 {
			divergent[id] = struct{}{}
		}
	}

	// SET-EQUALITY against the enumerated justified-residual ledger.
	// (a) every divergent ID must be a justified residual — else STOP + surface.
	for id := range divergent {
		if _, ok := crossProviderJustifiedResidual[id]; !ok {
			// Dump the conflicting tuples for diagnosis.
			var tups []string
			for tp := range byID[id] {
				tups = append(tups, fmt.Sprintf("(family=%q,variant=%q,version=%q)", tp.Family, tp.Variant, tp.Version))
			}
			sort.Strings(tups)
			t.Errorf("UNEXPECTED cross-provider divergence for ID %q (NOT in the justified-residual ledger):\n"+
				"  tuples: %s\n"+
				"  This gate uses the SAME snapshot + production pipeline as TestSnapshotAnalysis_CrossProviderDivergences,\n"+
				"  so both must agree on the divergent set. A new unexplained divergence is a GATE-LOGIC or pipeline\n"+
				"  regression — ROOT-CAUSE and SURFACE it; do NOT pad the ledger to force green.",
				id, strings.Join(tups, " | "))
		}
	}
	// (b) every ledger row must STILL be divergent — else it converged (prune the row)
	// or a DIFFERENT id took its place (caught by (a)).
	for id, justification := range crossProviderJustifiedResidual {
		if _, ok := divergent[id]; !ok {
			t.Errorf("justified-residual ledger row %q (%q) is NO LONGER divergent — it converged;\n"+
				"  remove the stale row so the ledger SET stays exactly the live residual set.",
				id, justification)
		}
	}

	// GATE-AGREEMENT cross-check: this hardened gate and TestSnapshotAnalysis decompose
	// identical data via the identical pipeline, so the divergent count must match the
	// pinned divergenceExact (0). A mismatch means the two gates DISAGREE — a gate-logic bug.
	if len(divergent) != len(crossProviderJustifiedResidual) {
		t.Errorf("divergent-ID count = %d, justified-residual ledger size = %d — SET-equality broken (see per-ID errors above)",
			len(divergent), len(crossProviderJustifiedResidual))
	}

	// RESIDUAL-COUNT PIN: catch a non-fixture-family residual regression.
	if residualUnaccounted > crossProviderResidualUnaccountedCeiling {
		t.Errorf("ReasonResidualUnaccountedTokens count = %d, exceeds pinned ceiling %d;\n"+
			"  a non-fixture-family residual regression (the seed-flash class) re-dropped sole-residual/member coverage.\n"+
			"  Investigate ParseFamilyDetailed; if the increase is intentional, bump the ceiling with justification.",
			residualUnaccounted, crossProviderResidualUnaccountedCeiling)
	}

	// POPULATED-VERSION FLOOR PIN: catch a version-presence regression.
	if populatedVersion < crossProviderPopulatedVersionFloor {
		t.Errorf("populated-version count = %d, below pinned floor %d;\n"+
			"  a regression dropped Version-presence coverage (the inverse of the residual-ceiling guard).\n"+
			"  Investigate ParseFamilyDetailed/idDrivenVersion; if the decrease is intentional, lower the floor with justification.",
			populatedVersion, crossProviderPopulatedVersionFloor)
	}
}

// --------------------------------------------------------------------------
// Double-hyphen flag support, ChatGPT casing, double-underscore tests
// --------------------------------------------------------------------------

// TestParseFlags_DoubleHyphen verifies that all flags accept BOTH single-hyphen
// and double-hyphen forms (e.g. --no-fetch is equivalent to -no-fetch).
// This test covers the single-/double-hyphen flag criteria from the slice spec.
//
// These tests will FAIL until double-hyphen prefix support is added to parseFlags.
func TestParseFlags_DoubleHyphen(t *testing.T) {
	cases := []struct {
		desc  string
		args  []string
		check func(t *testing.T, got flagResult)
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
// applied correctly: chatgpt → ChatGPT.
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
// Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
//
// When Version is non-empty, the version "4.5" is encoded as a single
// segment "4_5" (dot→underscore). The raw ID version tokens are replaced by this
// single compact segment so that "4_5" uses single underscores within.
//
// These tests will FAIL until the join separator is changed and version-segment logic is added.
func TestNameForCanonical_DoubleUnderscoreTemplate(t *testing.T) {
	cases := []struct {
		desc     string
		model    bestiary.ModelInfo
		wantName string
	}{
		{
			desc: "claude-opus-4-5 with Version on Anthropic (golden)",
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
			desc: "gpt-4o without version or date on OpenAI (golden)",
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
			desc: "chatgpt model uses ChatGPT casing",
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
// Tests: parse-failure audit log
// --------------------------------------------------------------------------

const parseFailuresFile = "parse_failures.json"

// failureCatalogJSON returns a minimal catalog.json ({providers, models}) whose
// providers view carries models that produce parse failures at codegen time.
// Specifically it includes raw_family values that trigger the YYMM-date-as-version
// false-positive (e.g. "mistral-2401") so the failure count is predictable.
func failureCatalogJSON(t *testing.T) []byte {
	t.Helper()
	providers := map[string]any{
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
	b, err := json.Marshal(map[string]any{"providers": providers, "models": map[string]any{}})
	if err != nil {
		t.Fatalf("failureCatalogJSON: marshal: %v", err)
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
		// encoding/json encodes nil slice as null. The impl must use []ParseFailure{} not nil.
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
		_, _ = w.Write(failureCatalogJSON(t))
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

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
			"  Why: the file-write step in run() may not be implemented yet\n"+
			"  How to fix: implement writeParseFailures call in run()",
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
	// failureCatalogJSON injects exactly 2 YYMM-failure models (mistral-2401 + mistral-2403).
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

// TestRun_AbortsOnDataSourceValidationError pins the codegen data-source FK guard
// wiring: run() must invoke the data-source validator BEFORE fetching/generating and
// abort (returning the wrapped error) when it fails. The guard is swapped for a
// failing stub via the validateCuratedDataSourceTable seam so the abort path is
// exercised without mutating the embedded datasources.json. If the validation call
// were dropped from run(), run() would proceed past it and the returned error would
// not wrap the sentinel — killing that drop mutant.
func TestRun_AbortsOnDataSourceValidationError(t *testing.T) {
	orig := validateCuratedDataSourceTable
	defer func() { validateCuratedDataSourceTable = orig }()

	sentinel := errors.New("sentinel: bad data-source curation")
	validateCuratedDataSourceTable = func() error { return sentinel }

	// -no-fetch with an empty cache dir would itself fail at the fetch step; the
	// data-source guard runs strictly before fetch, so a correctly-wired run() returns
	// the sentinel-wrapped error and never reaches fetch.
	err := run([]string{"-no-fetch", "-cache-dir=" + filepath.Join(t.TempDir(), "cache")})
	if err == nil {
		t.Fatal("run(): expected abort on data-source validation error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("run(): error does not wrap the data-source validation failure (guard not wired before fetch?): %v", err)
	}
	if !strings.Contains(err.Error(), "validate curated data-source table") {
		t.Fatalf("run(): error missing the data-source guard context: %v", err)
	}
}

// TestValidateCuratedDataSourceTable_DefaultIsRealGuard pins the seam's DEFAULT
// BINDING to the real guard. TestRun_AbortsOnDataSourceValidationError falsifies the
// CALL (run() invokes the seam and wraps its error) but not the binding: a refactor
// typo or a leftover stub (e.g. `= func() error { return nil }`) would silently
// disarm the data-source FK guard while the suite stayed green. Comparing func-
// pointer identity kills that no-op-binding mutant. It is safe under -shuffle: the
// abort test restores the binding via defer, so the package-var holds its default
// here regardless of test order.
func TestValidateCuratedDataSourceTable_DefaultIsRealGuard(t *testing.T) {
	got := reflect.ValueOf(validateCuratedDataSourceTable).Pointer()
	want := reflect.ValueOf(bestiary.ValidateDataSourceTable).Pointer()
	if got != want {
		t.Fatal("validateCuratedDataSourceTable default binding is not bestiary.ValidateDataSourceTable: " +
			"the codegen data-source FK guard is disconnected (a no-op stub would disarm it silently)")
	}
}

// --------------------------------------------------------------------------
// Tests: deterministic + reproducible codegen (ordering, collision-suffix, up-to-date guard)
// --------------------------------------------------------------------------

// normalizeWhitespace collapses all runs of whitespace in s to a single space.
// Used to compare generated Go source that may have tab-aligned columns against
// expected strings that use single spaces.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
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

// deterministicFixtureJSON returns the hermetic CATALOG.json fixture ({models,
// providers}) for the reproducibility tests. The "providers" view carries three
// collision groups:
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
//
// The "models" view carries THREE metadata entries (each with populated benchmarks and
// links), DELIBERATELY inserted out of MetadataID order so the metadata bake's
// sort.Slice is load-bearing: without it, Go map iteration would reorder the emitted
// rows run-to-run and the N=100 byte-identity assertion would fail. One benchmark
// carries a STRING score to exercise the ScoreRaw tolerant-decode path.
func deterministicFixtureJSON(t *testing.T) []byte {
	t.Helper()
	providers := map[string]any{
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
		// Curated-join models. Both IDs match entries in parse/data/quant_vram.json
		// by exact model-ID, so the curated quant/VRAM/ParamSize/Source bake in
		// enrichModelInfo fires for them. The provider slug "vllm" sorts
		// after every other fixture provider, so these two append to the end of the
		// generated slice and the golden excerpt can extend contiguously.
		//
		// llama-3.3-70b-instruct carries an upstream context window (128000) that
		// DELIBERATELY differs from the curated context_window (131072). Because the
		// curated value wins the bake-context precedence, the baked VRAMContextTokens
		// is 131072 and the VRAMBytes are computed at 131072 — pinning the precedence
		// chain. Its three rows have arch facts present, so VRAMEstimatePartial=false.
		//
		// llama-3.2-3b-instruct joins an arch-absent curated entry: every row has
		// KV=0, so VRAMBytes==WeightsBytes and VRAMEstimatePartial=true.
		// The two vllm models ALSO carry the instance-level api.json field groups so the
		// static golden / UpToDate / N=100 cover the ModelInfo emitter for every new
		// field: a mutant that drops any group's emission fails those tests.
		//   - llama-3.3-70b-instruct: description, status "deprecated" (known enum),
		//     reasoning_options (effort + budget_tokens kinds), audio costs,
		//     context_over_200k, and a general cost tier.
		//   - llama-3.2-3b-instruct: status "experimental" (unknown → StatusOther +
		//     StatusRaw), covering the fail-safe status path.
		"vllm": map[string]any{
			"name": "vLLM",
			"models": map[string]any{
				"llama-3.3-70b-instruct": map[string]any{
					"id":          "llama-3.3-70b-instruct",
					"name":        "Llama 3.3 70B Instruct",
					"family":      "llama",
					"description": "Meta Llama 3.3 70B, instruction-tuned.",
					"status":      "deprecated",
					"reasoning_options": []any{
						map[string]any{"type": "effort", "values": []any{"low", "high"}},
						map[string]any{"type": "budget_tokens", "min": 1024, "max": 32000},
					},
					"limit": map[string]any{
						"context": 128000,
					},
					"cost": map[string]any{
						"input":             0.5,
						"output":            1.5,
						"input_audio":       2,
						"output_audio":      3,
						"context_over_200k": map[string]any{"input": 1, "output": 3},
						"tiers": []any{
							map[string]any{
								"tier":   map[string]any{"type": "context", "size": 200000},
								"input":  0.8,
								"output": 2.4,
							},
						},
					},
				},
				"llama-3.2-3b-instruct": map[string]any{
					"id":     "llama-3.2-3b-instruct",
					"name":   "Llama 3.2 3B Instruct",
					"family": "llama",
					"status": "experimental",
				},
			},
		},
	}

	// models.json view: ≥3 metadata entries, benchmarks + links, keys inserted
	// out of sorted order (zhipuai > openai > anthropic) — the emitter must sort.
	models := map[string]any{
		"zhipuai/glm-4.6": map[string]any{
			"id":          "zhipuai/glm-4.6",
			"family":      "glm",
			"name":        "GLM-4.6",
			"description": "Zhipu AI GLM-4.6 general model.",
			"license":     "MIT",
			"links": []any{
				map[string]any{"label": "Model card", "url": "https://huggingface.co/zai-org/GLM-4.6", "type": "model_card"},
			},
			"benchmarks": []any{
				// String score → exercises the ScoreRaw tolerant-decode path.
				map[string]any{"name": "SWE-bench", "score": "pass", "metric": "resolved", "source": "https://z.ai/glm-4-6"},
			},
		},
		"openai/gpt-5.1": map[string]any{
			"id":          "openai/gpt-5.1",
			"family":      "gpt",
			"name":        "GPT-5.1",
			"description": "OpenAI GPT-5.1 flagship model.",
			"license":     "proprietary",
			"links": []any{
				map[string]any{"label": "Announcement", "url": "https://openai.com/gpt-5-1", "type": "announcement"},
			},
			"benchmarks": []any{
				map[string]any{"name": "MMLU", "score": 91.2, "metric": "accuracy", "source": "https://openai.com/gpt-5-1"},
			},
		},
		"anthropic/claude-haiku-3.5": map[string]any{
			"id":          "anthropic/claude-haiku-3.5",
			"family":      "claude-haiku",
			"name":        "Claude Haiku 3.5",
			"description": "Anthropic Claude Haiku 3.5.",
			"license":     "proprietary",
			"links": []any{
				map[string]any{"label": "Docs", "url": "https://docs.anthropic.com/claude-haiku", "type": "docs"},
			},
			"benchmarks": []any{
				map[string]any{"name": "GPQA", "score": 41.6, "metric": "accuracy", "source": "https://anthropic.com/claude-haiku"},
			},
		},
	}

	b, err := json.Marshal(map[string]any{"providers": providers, "models": models})
	if err != nil {
		t.Fatalf("deterministicFixtureJSON: marshal: %v", err)
	}
	return b
}

// runFixtureCodegen performs one full codegen cycle using the hermetic catalog
// fixture: spin up an httptest.Server, override catalogURL, call fetchModelsWithRaw,
// optionally stamp LastSynced (mirroring run()'s stamping path), build slugToConst, and
// return the three generated sources (static, constants, metadata).
//
// lastSynced controls the LastSynced stamp applied to every model before source
// generation:
//   - "" (empty): no stamp — LastSynced stays "" (the pre-existing behaviour used by
//     tests that do not test the stamping path)
//   - any non-empty RFC3339 string: stamp models[i].LastSynced with that value,
//     exactly mirroring the run() pipeline path
//
// The metadata source is generated from the fixture's models.json view; its rows are
// NOT stamped (baked metadata keeps LastSynced="") so the metadata output is a fixed
// function of the (map-randomized) metadata input — which is exactly what makes the
// missing-sort bug observable under N=100.
//
// Each call re-randomizes the Go map iteration order (both the providers models and the
// metadata map), which is the nondeterminism source the codegen ordering guarantees
// defend against.
func runFixtureCodegen(t *testing.T, fixtureJSON []byte, lastSynced string) (staticSrc, constantsSrc, metadataSrc []byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

	_, models, metadata, provMeta, _, err := fetchModelsWithRaw(context.Background(), false)
	if err != nil {
		t.Fatalf("runFixtureCodegen: fetchModelsWithRaw: %v", err)
	}

	// Mirror run()'s stamping path: stamp LastSynced on all models when a timestamp is
	// injected. Leaving lastSynced empty preserves the "" zero-value (fixture path).
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
	metadataSrc, err = generateMetadataSource(metadata)
	if err != nil {
		t.Fatalf("runFixtureCodegen: generateMetadataSource: %v", err)
	}
	return staticSrc, constantsSrc, metadataSrc
}

// TestCodegen_Reproducible_ByteIdentical verifies that N=100 successive codegen runs
// over the same fixture data (each re-randomizing map iteration order via a fresh
// fetchModelsWithRaw) produce FULLY byte-identical output for generateSource,
// generateConstantsSource, and generateMetadataSource — with NO normalization.
//
// The codegen LastSynced stamp is now DETERMINISTIC (bestiary-vq6k): every run stamps the
// SAME value — the current models.dev ingest instant from the committed datasources.json
// (codegenLastSynced) — so the wall-clock is no longer a residual source of diff. This
// guard therefore asserts RAW byte-identity and catches ANY divergence, including a stray
// time.Now() creeping back into a baked field. (The obsolete alternating-timestamp /
// sole-residual machinery it replaces is gone; the normalizeLastSynced helper stays for the
// deliberately wall-clock-injecting tests that still need it.)
//
// Additionally asserts that each raw model ID always receives the same _N suffix
// across all iterations (stable raw-ID-ordered assignment — deterministic ordering + raw-ID ordinal).
//
// Golden pins (from the spec):
//   - C: "anthropic/claude-3-5-haiku" always _1, "anthropic/claude-3.5-haiku" always _2
//   - B: "kilo-auto/free" always _1, "openrouter/free" always _2
//   - E (version-pair / negative control): exact constant names with NO doubled-ordinal variant
func TestCodegen_Reproducible_ByteIdentical(t *testing.T) {
	const N = 100

	// The single deterministic stamp used on EVERY iteration — the production codegen path.
	ts, err := codegenLastSynced()
	if err != nil {
		t.Fatalf("codegenLastSynced: %v", err)
	}
	// Pin the stamp to the models.dev current-ingest instant (source of truth), non-empty —
	// tying this byte-identity guard to the real datasources.json without a brittle literal,
	// so a snapshot refresh that appends a newer ingest instant moves both sides in lockstep.
	curIngest, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceModelsDev)
	if !ok || curIngest.IngestedAt == "" {
		t.Fatalf("expected a models.dev current ingest in the committed datasources.json")
	}
	if ts != curIngest.IngestedAt {
		t.Fatalf("codegen LastSynced stamp %q != models.dev current ingest instant %q\n"+
			"  Why: codegenLastSynced must return DatasetIngestedFor(DataSourceModelsDev).IngestedAt",
			ts, curIngest.IngestedAt)
	}

	fixtureJSON := deterministicFixtureJSON(t)

	// Reference run (iteration 0). Every run uses the SAME deterministic stamp `ts`.
	refStatic, refConstants, refMetadata := runFixtureCodegen(t, fixtureJSON, ts)

	// The metadata bake must emit rows in ascending MetadataID order regardless of the
	// map-iteration order the fixture's models.json view arrived in. Assert the sorted
	// order directly: anthropic/… < openai/… < zhipuai/… (the fixture inserts them in
	// the reverse-ish order zhipuai, openai, anthropic). If the sort is dropped, this
	// pin AND the N=100 byte-identity check below fail.
	refMetaStr := string(refMetadata)
	iA := strings.Index(refMetaStr, `MetadataID:  "anthropic/claude-haiku-3.5"`)
	iO := strings.Index(refMetaStr, `MetadataID:  "openai/gpt-5.1"`)
	iZ := strings.Index(refMetaStr, `MetadataID:  "zhipuai/glm-4.6"`)
	if iA < 0 || iO < 0 || iZ < 0 {
		t.Fatalf("reference metadata: expected all three baked MetadataIDs present\nmetadata:\n%s", refMetaStr)
	}
	if !(iA < iO && iO < iZ) {
		t.Errorf("reference metadata: rows not in ascending MetadataID order (anthropic<openai<zhipuai)\n"+
			"  positions: anthropic=%d openai=%d zhipuai=%d\n"+
			"  Why: generateMetadataSource dropped the sort.Slice on MetadataID\nmetadata:\n%s",
			iA, iO, iZ, refMetaStr)
	}
	// Baked rows must carry Source=DataSourceModelsDev and empty LastSynced, and the
	// string benchmark score must ride on ScoreRaw (Score 0).
	if !strings.Contains(refMetaStr, `Source:      "models.dev"`) {
		t.Errorf("reference metadata: baked Source is not models.dev\nmetadata:\n%s", refMetaStr)
	}
	if !strings.Contains(refMetaStr, `ScoreRaw: "pass"`) {
		t.Errorf("reference metadata: string benchmark score not captured on ScoreRaw\nmetadata:\n%s", refMetaStr)
	}

	// Verify reference constants contain the expected golden pins.
	refStr := string(refConstants)
	// refNorm is the whitespace-normalized version for substring matching.
	refNorm := normalizeWhitespace(refStr)

	// C group pins: '-' (0x2D) < '.' (0x2E) means claude-3-5-haiku < claude-3.5-haiku.
	// With parser active, version="3.5" is now extracted from both IDs
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

	// Prove that the reference static file was actually stamped with the deterministic value:
	// it must contain `ts` in a LastSynced line. (The constants file has no LastSynced fields.)
	refStaticStr := string(refStatic)
	if !strings.Contains(refStaticStr, `LastSynced:`) || !strings.Contains(refStaticStr, ts) {
		t.Errorf("reference static output: deterministic LastSynced stamp %q not found\n"+
			"  What: run()'s stamping path was not exercised\n"+
			"  Why: runFixtureCodegen did not stamp models[i].LastSynced with the deterministic value\n"+
			"  How to fix: verify that runFixtureCodegen stamps models[i].LastSynced when lastSynced != \"\"",
			ts)
	}

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

	// Run N-1 more iterations and assert FULL byte-identity (NO normalization). Because the
	// LastSynced stamp is deterministic, ANY diff — including a LastSynced line — is a real
	// non-determinism regression, so this now catches a stray wall-clock as well as the
	// original map-order/collision-ordinal bugs.
	for i := 1; i < N; i++ {
		staticSrc, constantsSrc, metadataSrc := runFixtureCodegen(t, fixtureJSON, ts)

		if !bytes.Equal(refStatic, staticSrc) {
			t.Fatalf("iteration %d: generateSource output is not byte-identical to the reference\n"+
				"  What: the static model list changed between runs\n"+
				"  Why: nondeterminism in the fetchModelsWithRaw map-range, the ordering sort, or a wall-clock in a baked field\n"+
				"  Where: fetchModelsWithRaw or generateSource\n"+
				"  How to fix: ensure the model slice is sorted before return and no baked field uses time.Now()",
				i+1)
		}
		if !bytes.Equal(refConstants, constantsSrc) {
			t.Fatalf("iteration %d: generateConstantsSource output is not byte-identical to the reference\n"+
				"  What: the constants file changed between runs\n"+
				"  Why: collision _N assignment is position-dependent (raw-ID ordinal not applied)\n"+
				"  Where: resolveCollisions fallback or final-uniqueness pass\n"+
				"  How to fix: replace sort.Ints(sortedPos) with a raw-ID-keyed member sort in resolveCollisions",
				i+1)
		}

		// The fixture's models.json view (≥3 unsorted entries) arrives in random map order
		// each iteration; the emitted metadata source must be byte-identical to the reference,
		// which is what proves the explicit sort.Slice on MetadataID. Metadata rows are never
		// stamped, so this needs no normalization either.
		if !bytes.Equal(refMetadata, metadataSrc) {
			t.Fatalf("iteration %d: generateMetadataSource output is not byte-identical to the reference\n"+
				"  What: the baked metadata order/content changed between runs\n"+
				"  Why: the metadata bake did not impose a deterministic MetadataID order\n"+
				"  Where: generateMetadataSource sort.Slice(baked, ...) on MetadataID\n"+
				"  How to fix: ensure generateMetadataSource sorts by MetadataID before emitting",
				i+1)
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
}

// TestEmit_VendoredCatalog_CarriesInstanceFields is the END-TO-END seam guard for the
// ModelInfo emitter over REAL data. It spans the full codegen pipeline —
// vendored catalog.json -> ParseCatalogJSON (wire parse) -> enrichModelInfo (bake) ->
// generateSource (emit) — and asserts that the instance-level api.json field GROUPS
// (status, description, reasoning options, tier/audio costs) survive all the way to the
// emitted static source. This is the exact seam a prior version silently dropped, which
// made `list --status deprecated` return an empty set even though 100+ models are
// tagged deprecated upstream: the wire parse populated the fields, but the emitter never
// rendered them, so StaticModels() lost them. Keying the check to the committed vendored
// snapshot (not a fixture) means a future emitter change that drops any field group over
// real data fails here, not just in the hand-built golden.
func TestEmit_VendoredCatalog_CarriesInstanceFields(t *testing.T) {
	// `go test` runs with the package dir as CWD, but vendoredCatalogPath is relative
	// to the module root; resolve it from this test file's location (two levels up).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the module root")
	}
	catalogPath := filepath.Join(filepath.Dir(thisFile), "..", "..", vendoredCatalogPath)
	raw, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read vendored catalog %q: %v\n"+
			"  How to fix: run the models.dev snapshot refresh (see AGENTS.md)", catalogPath, err)
	}
	cat, err := bestiary.ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("ParseCatalogJSON(vendored): %v", err)
	}

	models := make([]bestiary.ModelInfo, 0, len(cat.Models))
	var deprecated, withReasoning, withTiers int
	for _, base := range cat.Models {
		info, _ := enrichModelInfo(base)
		models = append(models, info)
		if info.Status == bestiary.StatusDeprecated {
			deprecated++
		}
		if len(info.ReasoningOptions) > 0 {
			withReasoning++
		}
		if len(info.CostTiers) > 0 {
			withTiers++
		}
	}

	// Sanity: the vendored snapshot really does carry these groups (so the emitter
	// assertions below are not vacuous). If the parse/bake seam drops them, this fires
	// first with a precise message.
	if deprecated == 0 {
		t.Fatalf("no StatusDeprecated models after ParseCatalogJSON+enrichModelInfo over the vendored catalog — the status field group is dropped at the parse/bake seam")
	}

	src, err := generateSource(models, map[string]string{})
	if err != nil {
		t.Fatalf("generateSource(vendored): %v", err)
	}
	s := string(src)

	// The emitted static source MUST render each instance-level field group. These
	// markers are gofmt-alignment-insensitive (constant/type names, not aligned
	// colons). A missing marker means the emitter is dropping that group.
	for _, want := range []string{
		"StatusDeprecated",      // ModelStatus enum emission (statusExpr)
		"Description:",          // description scalar
		"[]ReasoningOption{",    // reasoning-options literal
		"[]CostTier{",           // general cost-tier literal
		"&TierCost{",            // context_over_200k pointer literal
		"CostInputAudioPerMTok", // audio-cost field
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated static source is MISSING %q — the emitter is dropping an instance-level field group\n"+
				"  What: the vendored-catalog -> bake -> emit seam lost a field group\n"+
				"  Why: generateSource does not render this field (the `list --status`-empty regression)\n"+
				"  How to fix: extend the generateSource field emission in cmd/bestiary-gen/main.go",
				want)
		}
	}
	t.Logf("vendored bake: %d models; %d deprecated, %d with reasoning options, %d with cost tiers",
		len(models), deprecated, withReasoning, withTiers)
}

// TestCodegen_UpToDate is the up-to-date guard. It regenerates both source
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

	metadataGoldenPath := filepath.Join("testdata", "expected_metadata_excerpt.go.golden")

	constantsGoldenRaw, err := os.ReadFile(constantsGoldenPath)
	if err != nil {
		t.Fatalf("up-to-date guard: could not read constants golden %q: %v\n"+
			"  How to fix: ensure testdata/expected_constants_excerpt.go.golden is committed",
			constantsGoldenPath, err)
	}
	staticGoldenRaw, err := os.ReadFile(staticGoldenPath)
	if err != nil {
		t.Fatalf("up-to-date guard: could not read static golden %q: %v\n"+
			"  How to fix: ensure testdata/expected_static_excerpt.go.golden is committed",
			staticGoldenPath, err)
	}
	metadataGoldenRaw, err := os.ReadFile(metadataGoldenPath)
	if err != nil {
		t.Fatalf("up-to-date guard: could not read metadata golden %q: %v\n"+
			"  How to fix: ensure testdata/expected_metadata_excerpt.go.golden is committed",
			metadataGoldenPath, err)
	}

	// Regenerate from the fixture. The codegen LastSynced stamp is now deterministic
	// (bestiary-vq6k), so this guard no longer injects a timestamp and normalizes it out:
	// the golden excerpts carry LastSynced: "" and the fixture path (lastSynced "") emits
	// exactly that, so the comparison is an EXACT content match with no LastSynced masking.
	// Keeping the golden's stamp empty also decouples this content/ordering guard from the
	// committed ingest instant, so a snapshot refresh never forces a golden re-cut here.
	fixtureJSON := deterministicFixtureJSON(t)
	staticSrc, constantsSrc, metadataSrc := runFixtureCodegen(t, fixtureJSON, "")

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

	// normalizeAndStrip strips the generated-file header and normalizes whitespace. It no
	// longer strips LastSynced: the stamp is deterministic and the fixture path emits the
	// golden's empty LastSynced verbatim, so no masking is needed (masking would weaken this
	// to a content-only guard).
	normalizeAndStrip := func(src []byte) string {
		return stripGenHeader(src)
	}

	// Both sides: generated output (header stripped, whitespace normalized) and golden
	// excerpt (whitespace normalized). The golden excerpt carries LastSynced: "" and the
	// fixture path emits exactly that, so this is an EXACT match on content INCLUDING the
	// empty stamp.
	normConstants := normalizeAndStrip(constantsSrc)
	normConstantsGolden := normalizeWhitespace(string(constantsGoldenRaw))

	// The golden excerpt must appear as a substring in the generated output.
	// Normalizing whitespace on both sides makes the comparison insensitive to
	// gofmt alignment and minor formatting differences.
	if !strings.Contains(normConstants, normConstantsGolden) {
		t.Errorf("up-to-date guard: constants file does not contain golden excerpt\n"+
			"  What: generateConstantsSource output differs from testdata/expected_constants_excerpt.go.golden\n"+
			"  Why: collision _N bindings may have changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen/main.go generateConstantsSource or resolveCollisions\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add models_constants_gen.go models_static_gen.go`\n"+
			"\nGolden excerpt (normalized):\n%s\n\nGenerated (normalized, header stripped):\n%s",
			normConstantsGolden, normConstants)
	}

	normStatic := normalizeAndStrip(staticSrc)
	normStaticGolden := normalizeWhitespace(string(staticGoldenRaw))

	// The golden excerpt must appear as a substring in the generated static output.
	if !strings.Contains(normStatic, normStaticGolden) {
		t.Errorf("up-to-date guard: static models file does not contain golden excerpt\n"+
			"  What: generateSource output differs from testdata/expected_static_excerpt.go.golden\n"+
			"  Why: model ordering changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen/main.go generateSource\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add models_constants_gen.go models_static_gen.go`\n"+
			"\nGolden excerpt (normalized):\n%s\n\nGenerated (normalized, header stripped):\n%s",
			normStaticGolden, normStatic)
	}

	normMetadata := normalizeAndStrip(metadataSrc)
	normMetadataGolden := normalizeWhitespace(string(metadataGoldenRaw))

	// The golden excerpt must appear as a substring in the generated metadata output.
	if !strings.Contains(normMetadata, normMetadataGolden) {
		t.Errorf("up-to-date guard: metadata file does not contain golden excerpt\n"+
			"  What: generateMetadataSource output differs from testdata/expected_metadata_excerpt.go.golden\n"+
			"  Why: the baked metadata rows/order changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen/main.go generateMetadataSource\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add models_metadata_gen.go`\n"+
			"\nGolden excerpt (normalized):\n%s\n\nGenerated (normalized, header stripped):\n%s",
			normMetadataGolden, normMetadata)
	}

	// Sanity-check: the constants golden must contain at least one expected binding.
	// This guards against an accidentally empty or truncated golden file.
	if !strings.Contains(string(constantsGoldenRaw), `ModelID = "anthropic/claude-3-5-haiku"`) {
		t.Errorf("up-to-date guard: constants golden file appears empty or truncated (missing expected binding)\n" +
			"  How to fix: ensure testdata/expected_constants_excerpt.go.golden is correctly committed")
	}
}

// TestCodegen_GoldenPins_C verifies the C group (cloudflare-ai-gateway punctuation
// collision): "anthropic/claude-3-5-haiku" → _1, "anthropic/claude-3.5-haiku" → _2.
// ASCII ordering: '-' (0x2D) < '.' (0x2E).
//
// With parser active, both IDs parse to version="3.5" (family=claude,
// variant=haiku). The constant base becomes Model__CloudflareAIGateway__Claude__3__5__Haiku__3_5;
// collision suffix _1/_2 still applies via raw-ID-ordered fallback.
func TestCodegen_GoldenPins_C(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
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
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
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
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
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
// (Provider, ID) after the deterministic ordering. Uses the fixture to check the expected ordering.
func TestCodegen_SortOrder(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

	_, models, _, _, _, err := fetchModelsWithRaw(context.Background(), false)
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

	// Spot-check known order from fixture: cloudflare < kilo < openai < vllm.
	// The curated-join provider "vllm" sorts last, so its two models append to the
	// end of the slice — which is what lets the static golden excerpt extend
	// contiguously to cover them.
	providers := make([]string, 0, len(models))
	seen := make(map[string]bool)
	for _, m := range models {
		if !seen[string(m.Provider)] {
			providers = append(providers, string(m.Provider))
			seen[string(m.Provider)] = true
		}
	}
	wantOrder := []string{"cloudflare-ai-gateway", "kilo", "openai", "vllm"}
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
// Decomposition snapshot + fixture corpus + per-reason tests
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

// fixtureCatalogJSON returns the contents of testdata/fixture_catalog.json. This
// catalog.json-shaped fixture ({providers, models}) contains the full providers corpus
// (active-class, residual, empty-raw_family passthrough-guard, YYMM) plus a small
// metadata view; it is the hermetic input for TestDecompositionSnapshot and the run()
// diagnostics tests.
func fixtureCatalogJSON(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join("testdata", "fixture_catalog.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixtureCatalogJSON: could not read %q: %v\n"+
			"  How to fix: ensure testdata/fixture_catalog.json is committed",
			path, err)
	}
	return data
}

// runFixtureCatalogCodegen is like runFixtureCodegen but uses fixtureCatalogJSON (the
// full providers corpus from testdata/fixture_catalog.json) instead of the
// collision-group-focused deterministicFixtureJSON. It returns all models + parse
// failures from the run.
func runFixtureCatalogCodegen(t *testing.T) (models []bestiary.ModelInfo, failures []bestiary.ParseFailure) {
	t.Helper()
	fixtureJSON := fixtureCatalogJSON(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

	_, models, _, _, failures, err := fetchModelsWithRaw(context.Background(), false)
	if err != nil {
		t.Fatalf("runFixtureAPICodegen: fetchModelsWithRaw: %v", err)
	}
	return models, failures
}

// TestDecompositionSnapshot is the fixture-based decomposition snapshot test.
// It runs the full fixture corpus through fetchModelsWithRaw (which calls genToModelInfoDetailed
// → ParseFamilyDetailed) and compares the (Family, Variant, Version, Modifier) output
// per model against a committed golden file.
//
// The -update flag regenerates the golden file:
//
//	go test ./cmd/bestiary-gen/... -run TestDecompositionSnapshot -update
//
// This test is fixture-based only (NOT a real-data ==0 gate).
func TestDecompositionSnapshot(t *testing.T) {
	models, _ := runFixtureCatalogCodegen(t)

	// Collect decomposition entries, sorted by (provider, model_id) for determinism.
	entries := make([]decompositionSnapshotEntry, 0, len(models))
	for _, m := range models {
		entries = append(entries, decompositionSnapshotEntry{
			Provider: string(m.Provider),
			ModelID:  string(m.ID),
			Family:   string(m.Family),
			Variant:  m.Variant,
			Version:  m.Version,
			Modifier: modKey(m.Modifier),
		})
	}
	// Models from fetchModelsWithRaw are already sorted by (Provider, ID) via the deterministic ordering.

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
// active-class model in the fixture corpus has a non-empty Version field.
// This is the per-row version!="" check for the active class.
func TestDecompositionSnapshot_ActiveClassVersionPopulated(t *testing.T) {
	models, _ := runFixtureCatalogCodegen(t)

	// Active-class models: those expected to have version populated.
	// parser correctly extracts version for these cases.
	// Sole-residual promotion adds variant-promoted models (glm-5-turbo, phi-4-mini)
	// whose variant is now set from the sole trailing suffix after version extraction.
	// text-embedding-3-large/small removed from active class —
	// the full-prefix-first change that enabled their sole-residual promotion was reverted.
	// They are now documented residuals.
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
		// Sole-residual-promoted models surviving the full-prefix-first revert (single-token rawFamily, no compound prefix):
		// glm-5-turbo: — 'turbo' is now a GLOBAL modifier (not a glm member), so it
		// reclassifies variant→modifier: (glm, "", 5, [turbo]). Version 5 still populated.
		"glm-5-turbo": {wantFamily: "glm", wantVariant: "", wantVersion: "5"},
		// phi-4-mini: raw_family=phi → family=phi, variant=mini (sole-residual promotion, still a variant suffix), version=4
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
				"  What: the parser should populate Version for active-class models\n"+
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
				"  What: the sole-residual promotion may not have fired (sole trailing suffix not promoted)\n"+
				"  Why: the sole-residual suffix promotion should set Variant=<suffix> when exactly one residual is a known variant suffix",
				id, m.Variant, want.wantVariant)
		}
	}
}

// TestDecompositionSnapshot_NoVersionForBare4Digit verifies the bare-4-digit-date guard in the
// fixture corpus: deepseek-r1-0528 and deepseek-v3-0324 must have Version="" because
// "0528" and "0324" are bare 4-digit date tokens (MMDD format), not semantic versions.
//
// This is the per-row version=="" check for the bare-4-digit-date guard models (fixture-based, not real-data).
func TestDecompositionSnapshot_NoVersionForBare4Digit(t *testing.T) {
	models, _ := runFixtureCatalogCodegen(t)

	modelsByID := make(map[string]bestiary.ModelInfo, len(models))
	for _, m := range models {
		modelsByID[string(m.ID)] = m
	}

	fixACases := map[string]struct {
		wantFamily  string
		wantVersion string // must be empty
	}{
		// raw_family "deepseek-r1" is a RAW-POPULATED over-capture; it now
		// reduces to the short base "deepseek" (the SAME reduction applied to empty-raw),
		// making it consistent with deepseek-v3-0324. The bare-4-digit date guard (the focus
		// of this test) is unchanged — Version stays "".
		"deepseek-r1-0528": {wantFamily: "deepseek", wantVersion: ""},
		"deepseek-v3-0324": {wantFamily: "deepseek", wantVersion: ""},
	}

	for id, want := range fixACases {
		m, ok := modelsByID[id]
		if !ok {
			t.Errorf("the bare-4-digit-date guard model %q not found in fixture output", id)
			continue
		}
		if m.Version != want.wantVersion {
			t.Errorf("the bare-4-digit-date guard model %q: Version = %q, want %q (bare 4-digit token must not be a version)\n"+
				"  What: 4-digit date-like token was extracted as a version\n"+
				"  Why: the bare-4-digit-date guard extends isFourDigitDateToken to reject any 4-digit all-numeric token\n"+
				"  How to fix: verify isFourDigitDateToken returns true for \"0528\" and \"0324\"",
				id, m.Version, want.wantVersion)
		}
		if string(m.Family) != want.wantFamily {
			t.Errorf("the bare-4-digit-date guard model %q: Family = %q, want %q", id, m.Family, want.wantFamily)
		}
	}
}

// --------------------------------------------------------------------------
// version_duplicates.json + dot_form_audit.json + smoke check tests
// --------------------------------------------------------------------------

// TestRun_WritesVersionDuplicates verifies that run() writes version_duplicates.json
// to the cache directory when models share (provider, family, variant, version).
// Uses fixture_api.json which contains haiku models that both resolve to version="3.5"
// under cloudflare-ai-gateway (same provider/family/variant/version → duplicate group).
func TestRun_WritesVersionDuplicates(t *testing.T) {
	fixtureJSON := fixtureCatalogJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

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
		t.Errorf("version_duplicates.json DuplicateCount = 0, want > 0\n" +
			"  What: expected at least one duplicate group from haiku models\n" +
			"  Why: both claude-haiku models in fixture_api.json resolve to version=3.5\n" +
			"  How to fix: verify writeVersionDuplicates collects (provider,family,variant,version) groups")
	}
}

// TestRun_WritesDotFormAudit verifies that run() writes dot_form_audit.json with
// models whose Version contains a dot (dot-form populated).
func TestRun_WritesDotFormAudit(t *testing.T) {
	fixtureJSON := fixtureCatalogJSON(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(fixtureJSON)
	}))
	defer srv.Close()

	origURL := catalogURL
	catalogURL = srv.URL
	defer func() { catalogURL = origURL }()

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
		t.Errorf("dot_form_audit.json Count = 0, want > 0\n" +
			"  What: expected models with dot-form versions (e.g. 3.5, 5.1)\n" +
			"  Why: fixture_api.json contains multiple models with dot-separated versions\n" +
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
		{ID: "claude-3.5-haiku", Provider: "anthropic", Version: "3.5"}, // dot-form
		{ID: "gpt-5.1", Provider: "openai", Version: "5.1"},             // dot-form
		{ID: "gpt-5-mini", Provider: "openai", Version: "5"},            // no dot — not in audit
		{ID: "nova-2-lite-v1", Provider: "cartesia", Version: "2"},      // no dot — not in audit
		{ID: "no-version", Provider: "test", Version: ""},               // empty — not in audit
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
// full fixture corpus. This mirrors the TestRun_WritesParseFailuresJSON pattern
// but uses fixture_catalog.json instead of failureCatalogJSON.
//
// Expectations:
//   - ReasonVersionDigitsNotExtracted (active class) → 0: now correctly
//     extracts version from IDs like claude-3-5-haiku; this failure should no longer fire.
//   - ReasonResidualUnaccountedTokens → at least 4:
//     nova-2-lite-v1 (C: variant pre-set), phi-3-medium-128k-instruct (multi-residual),
//     text-embedding-3-large + text-embedding-3-small (the full-prefix-first revert documented residuals —
//     full-prefix-first reverted; deferred fix).
//   - ReasonYYMMDateAsVersion → at least 1: mistral-small-2603 (family mistral-2603)
//     triggers the YYMM false-positive detector.
//   - the bare-4-digit-date guard confirmation: deepseek-r1-0528 / deepseek-v3-0324 produce NO failure
//     (bare 4-digit date tokens are now rejected as versions, not residual).
//   - the sole-residual suffix promotion confirmation: glm-5-turbo / phi-4-mini produce NO failure
//     (single-token rawFamily; the sole-residual promotion still fires). text-embedding-3-* removed (now residual).
//
// This test is NOT a ==0 gate on real data — fixture-based only (by design).
func TestFixturePerReasonCounts(t *testing.T) {
	_, failures := runFixtureCatalogCodegen(t)

	// Count per-reason occurrences.
	counts := make(map[bestiary.ParseFailureReason]int)
	// Build per-model failure lookup for the bare-4-digit-date guard / sole-residual promotion spot checks.
	failsByID := make(map[string]bestiary.ParseFailureReason)
	for _, f := range failures {
		counts[f.Reason]++
		failsByID[string(f.RawID)] = f.Reason
	}

	// Active class: ReasonVersionDigitsNotExtracted must be 0.
	// With parser, version is now correctly extracted for claude-3-5-haiku
	// and similar active-class models, so this failure reason must NOT appear.
	if n := counts[bestiary.ReasonVersionDigitsNotExtracted]; n != 0 {
		t.Errorf("ReasonVersionDigitsNotExtracted = %d, want 0\n"+
			"  What: the parser should suppress this failure for active-class models\n"+
			"  Why: ParseFamilyDetailed now extracts version via Δ1 extract-first path\n"+
			"  How to fix: verify ParseFamilyDetailed does not emit ReasonVersionDigitsNotExtracted for claude-3-5-haiku etc.",
			n)
	}

	// Residual: ReasonResidualUnaccountedTokens must be >= 4:
	// nova-2-lite-v1 (C: variant pre-set, "v1" residual after variant) +
	// phi-3-medium-128k-instruct (multi-residual) +
	// text-embedding-3-large (documented residual of the full-prefix-first revert) +
	// text-embedding-3-small (same).
	// After the sole-residual suffix promotion, glm-5-turbo/phi-4-mini are promoted (single-token rawFamily, the sole-residual promotion applies).
	if n := counts[bestiary.ReasonResidualUnaccountedTokens]; n < 4 {
		t.Errorf("ReasonResidualUnaccountedTokens = %d, want >= 4\n"+
			"  What: nova-2-lite-v1 (C) + phi-3-medium-128k-instruct (multi-residual) + text-embedding-3-large/small (the full-prefix-first revert residual)\n"+
			"    should produce residual failures\n"+
			"  Why: the full-prefix-first change was reverted; text-embedding models now have compound residual tokens\n"+
			"  How to fix: verify fixture_api.json includes all four models",
			n)
	}

	// YYMM: ReasonYYMMDateAsVersion must be > 0 (mistral-small-2603 contributes).
	if n := counts[bestiary.ReasonYYMMDateAsVersion]; n == 0 {
		t.Errorf("ReasonYYMMDateAsVersion = 0, want > 0\n" +
			"  What: mistral-small-2603 (family mistral-2603) should produce a YYMM failure\n" +
			"  Why: ParseFamilyDetailed YYMM detector fires for families matching the YYMM pattern\n" +
			"  How to fix: verify fixture_api.json includes mistral-small-2603 under mistral provider")
	}

	// the bare-4-digit-date guard spot check: deepseek-r1-0528 and deepseek-v3-0324 must NOT appear in
	// failures. Their bare 4-digit date tokens ("0528", "0324") are now rejected as
	// versions → no version extracted → no residual failure.
	for _, fixAID := range []string{"deepseek-r1-0528", "deepseek-v3-0324"} {
		if reason, found := failsByID[fixAID]; found {
			t.Errorf("the bare-4-digit-date guard model %q produced a failure (reason=%q), want no failure\n"+
				"  What: bare 4-digit date token was not suppressed\n"+
				"  Why: the bare-4-digit-date guard should extend isFourDigitDateToken to reject 4-digit all-numeric tokens\n"+
				"  How to fix: verify isFourDigitDateToken returns true for \"0528\" and \"0324\"",
				fixAID, reason)
		}
	}

	// the sole-residual suffix promotion spot check: glm-5-turbo and phi-4-mini must NOT
	// appear in failures. Their single-token rawFamily ("glm", "phi") means the sole-residual promotion fires
	// correctly (sole trailing suffix promoted, no compound prefix issue).
	// text-embedding-3-large/small are removed from this check — they are now documented
	// residuals after reverted the full-prefix-first change.
	for _, fixB1ID := range []string{"glm-5-turbo", "phi-4-mini"} {
		if reason, found := failsByID[fixB1ID]; found {
			t.Errorf("the sole-residual suffix promotion model %q produced a failure (reason=%q), want no failure\n"+
				"  What: sole trailing known-suffix was not promoted into Variant\n"+
				"  Why: the sole-residual suffix promotion should suppress ReasonResidualUnaccountedTokens when sole residual is a known suffix\n"+
				"  How to fix: verify the sole-residual promotion logic in ParseFamilyDetailed",
				fixB1ID, reason)
		}
	}
}

// --------------------------------------------------------------------------
// Codegen wiring — QuantVRAM baking + determinism tests
// --------------------------------------------------------------------------

// TestQuantVRAM_Llama33_70b is the 70B anchor: llama-3.3-70b-instruct bakes
// VRAMBytes = weights + KV at context 131072 with partial=false for all three quants.
//
// Expected values (hand-computed):
//
//	KV = 2 * 80 * 8 * 128 * 131072 * 2 = 42,949,672,960 bytes
//	q4_k_m: VRAMBytes = 43,033,509,888 + 42,949,672,960 = 85,983,182,848
//	q8_0:   VRAMBytes = 75,176,521,728 + 42,949,672,960 = 118,126,194,688
//	f16:    VRAMBytes = 141,166,166,016 + 42,949,672,960 = 184,115,838,976
func TestQuantVRAM_Llama33_70b(t *testing.T) {
	const modelID = bestiary.ModelID("llama-3.3-70b-instruct")
	const bakeCtx = 131072 // curated context_window from quant_vram.json

	type wantRow struct {
		quant         bestiary.Quantization
		weightsBytes  int64
		vramBytes     int64
		vramCtxTokens int
		partial       bool
	}

	want := []wantRow{
		{
			quant:         bestiary.QuantQ4_K_M,
			weightsBytes:  43_033_509_888,
			vramBytes:     85_983_182_848,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
		{
			quant:         bestiary.QuantQ8_0,
			weightsBytes:  75_176_521_728,
			vramBytes:     118_126_194_688,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
		{
			quant:         bestiary.QuantF16,
			weightsBytes:  141_166_166_016,
			vramBytes:     184_115_838_976,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
	}

	// Obtain the raw (unbaked) rows from the curated table.
	rawRows := bestiary.QuantVRAMFor(modelID)
	if rawRows == nil {
		t.Fatalf("QuantVRAMFor(%q) returned nil; expected curated rows from quant_vram.json", modelID)
	}
	if len(rawRows) != len(want) {
		t.Fatalf("QuantVRAMFor(%q): got %d rows, want %d", modelID, len(rawRows), len(want))
	}

	// Bake each row using EstimateVRAMBytes at the curated bake context.
	for i, row := range rawRows {
		baked := row
		baked.VRAMBytes = bestiary.EstimateVRAMBytes(row.WeightsBytes, bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
		baked.VRAMContextTokens = bakeCtx
		baked.VRAMEstimatePartial = bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)

		w := want[i]
		if baked.Quant != w.quant {
			t.Errorf("row %d: Quant = %v, want %v", i, baked.Quant, w.quant)
		}
		if baked.WeightsBytes != w.weightsBytes {
			t.Errorf("row %d (%v): WeightsBytes = %d, want %d", i, baked.Quant, baked.WeightsBytes, w.weightsBytes)
		}
		if baked.VRAMBytes != w.vramBytes {
			t.Errorf("row %d (%v): VRAMBytes = %d, want %d\n"+
				"  What: baked VRAM does not match expected weights+KV\n"+
				"  Why: KV = 2*layers*kvHeads*headDim*ctx*2; bakeCtx=%d, layers=%d, kvHeads=%d, headDim=%d\n"+
				"  How to fix: verify EstimateVRAMBytes formula or quant_vram.json weights_bytes",
				i, baked.Quant, baked.VRAMBytes, w.vramBytes,
				bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
		}
		if baked.VRAMContextTokens != w.vramCtxTokens {
			t.Errorf("row %d (%v): VRAMContextTokens = %d, want %d", i, baked.Quant, baked.VRAMContextTokens, w.vramCtxTokens)
		}
		if baked.VRAMEstimatePartial != w.partial {
			t.Errorf("row %d (%v): VRAMEstimatePartial = %v, want %v\n"+
				"  What: arch facts (layers=%d, kvHeads=%d, headDim=%d) are all present; partial must be false\n"+
				"  How to fix: verify VRAMEstimateIsPartial predicate",
				i, baked.Quant, baked.VRAMEstimatePartial, w.partial,
				row.Layers, row.KVHeads, row.HeadDim)
		}
	}

	// ParamSize check.
	if ps := bestiary.ParamSizeFor(modelID); ps != "70b" {
		t.Errorf("ParamSizeFor(%q) = %q, want %q", modelID, ps, "70b")
	}
	// Source check.
	if src := bestiary.SourceFor(modelID); src != bestiary.DataSourceOllama {
		t.Errorf("SourceFor(%q) = %q, want %q", modelID, src, bestiary.DataSourceOllama)
	}
}

// TestQuantVRAM_SmallModel covers small models where arch facts are absent
// (exercises partial path) and ParamSize is populated.
//
// llama-3.2-3b-instruct: two quant rows, arch facts absent.
// llama-3.3-8b-instruct: param-size-only entry (empty rows array); QuantVRAMFor
// returns nil but ParamSizeFor returns "8b" — demonstrates the (Family,Version,
// Modifier) wrong-merge split without fabricated GGUF weights.
func TestQuantVRAM_SmallModel(t *testing.T) {
	t.Run("llama-3.2-3b-instruct", func(t *testing.T) {
		const modelID bestiary.ModelID = "llama-3.2-3b-instruct"
		rows := bestiary.QuantVRAMFor(modelID)
		if rows == nil {
			t.Fatalf("QuantVRAMFor(%q) returned nil; expected curated rows", modelID)
		}
		if len(rows) != 2 {
			t.Fatalf("QuantVRAMFor(%q): got %d rows, want 2", modelID, len(rows))
		}

		// Arch facts should be absent (all zero) for this model.
		for i, row := range rows {
			if row.Layers != 0 || row.KVHeads != 0 || row.HeadDim != 0 {
				t.Errorf("row %d: expected arch facts to be absent (0), got layers=%d kvHeads=%d headDim=%d",
					i, row.Layers, row.KVHeads, row.HeadDim)
			}
			// With arch absent, partial flag must be true after baking.
			partial := bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)
			if !partial {
				t.Errorf("row %d: VRAMEstimateIsPartial = false with absent arch facts; want true", i)
			}
			if row.WeightsBytes <= 0 {
				t.Errorf("row %d: WeightsBytes = %d; must be > 0", i, row.WeightsBytes)
			}
		}

		if ps := bestiary.ParamSizeFor(modelID); ps != "3b" {
			t.Errorf("ParamSizeFor(%q) = %q, want %q", modelID, ps, "3b")
		}
		if src := bestiary.SourceFor(modelID); src != bestiary.DataSourceOllama {
			t.Errorf("SourceFor(%q) = %q, want %q", modelID, src, bestiary.DataSourceOllama)
		}
	})

	t.Run("llama-3.3-8b-instruct param-size-only", func(t *testing.T) {
		// This entry has an empty rows array: no GGUF weights curated yet.
		// QuantVRAMFor must return nil (correct for empty rows), but ParamSizeFor
		// must return "8b" so codegen splits llama@3.3#8b{instruct} from the 70b entity.
		const modelID bestiary.ModelID = "llama-3.3-8b-instruct"
		rows := bestiary.QuantVRAMFor(modelID)
		if rows != nil {
			t.Errorf("QuantVRAMFor(%q) = %v, want nil for a param-size-only entry with no curated weights", modelID, rows)
		}
		if ps := bestiary.ParamSizeFor(modelID); ps != "8b" {
			t.Errorf("ParamSizeFor(%q) = %q, want %q", modelID, ps, "8b")
		}
		if src := bestiary.SourceFor(modelID); src != bestiary.DataSourceOllama {
			t.Errorf("SourceFor(%q) = %q, want %q", modelID, src, bestiary.DataSourceOllama)
		}
	})
}

// TestQuantVRAM_PartialWhenArchAbsent verifies the baking rule that
// arch-absent rows produce VRAMBytes==WeightsBytes AND VRAMEstimatePartial==true,
// while rows with complete arch facts produce partial=false. Tests both sides of
// the predicate using the curated seed data.
func TestQuantVRAM_PartialWhenArchAbsent(t *testing.T) {
	t.Run("arch_absent_yields_partial", func(t *testing.T) {
		// llama-3.2-3b-instruct has no arch facts (exercises partial-VRAM path).
		rows := bestiary.QuantVRAMFor("llama-3.2-3b-instruct")
		if rows == nil {
			t.Fatal("QuantVRAMFor(llama-3.2-3b-instruct) returned nil; need curated rows")
		}
		for i, row := range rows {
			bakeCtx := bestiary.ContextWindowFor("llama-3.2-3b-instruct")
			vram := bestiary.EstimateVRAMBytes(row.WeightsBytes, bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
			partial := bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)

			if vram != row.WeightsBytes {
				t.Errorf("row %d: VRAMBytes=%d with absent arch, want WeightsBytes=%d (weights-only lower bound)",
					i, vram, row.WeightsBytes)
			}
			if !partial {
				t.Errorf("row %d: VRAMEstimatePartial=false with absent arch facts (layers=%d kvHeads=%d headDim=%d); want true",
					i, row.Layers, row.KVHeads, row.HeadDim)
			}
		}
	})

	t.Run("arch_present_yields_not_partial", func(t *testing.T) {
		// llama-3.3-70b-instruct has full arch facts.
		rows := bestiary.QuantVRAMFor("llama-3.3-70b-instruct")
		if rows == nil {
			t.Fatal("QuantVRAMFor(llama-3.3-70b-instruct) returned nil; need curated rows")
		}
		for i, row := range rows {
			partial := bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)
			if partial {
				t.Errorf("row %d: VRAMEstimatePartial=true with arch facts present (layers=%d kvHeads=%d headDim=%d); want false",
					i, row.Layers, row.KVHeads, row.HeadDim)
			}
		}
	})
}

// TestCodegen_ParamSizeOnlyFromCuratedTable asserts that ParamSize is emitted
// in the generated static source if and only if a model joins a curated
// quant_vram.json entry — the carrier of the #size identity dimension. A model
// WITHOUT a curated entry must emit no ParamSize line, so its EntityRef key stays
// byte-identical to the pre-paramsize baseline (no migration drift); a model WITH
// a curated entry must emit exactly the curated param_size token.
//
// The hermetic fixture contains exactly two curated-joining IDs
// (llama-3.2-3b-instruct → "3b", llama-3.3-70b-instruct → "70b") and four
// uncurated IDs. So the generated source must contain exactly two ParamSize lines
// with those two values and no others. This makes both directions falsifiable:
// dropping the join leaves zero ParamSize lines; a stray ParamSize on an
// uncurated model adds an unexpected line.
func TestCodegen_ParamSizeOnlyFromCuratedTable(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	staticSrc, _, _ := runFixtureCodegen(t, fixtureJSON, "")
	src := string(staticSrc)

	reParamSize := regexp.MustCompile(`ParamSize:\s+"([^"]*)"`)
	got := reParamSize.FindAllStringSubmatch(src, -1)

	gotValues := make([]string, 0, len(got))
	for _, m := range got {
		gotValues = append(gotValues, m[1])
	}
	sort.Strings(gotValues)

	want := []string{"3b", "70b"}
	if len(gotValues) != len(want) || gotValues[0] != want[0] || gotValues[1] != want[1] {
		t.Errorf("ParamSize emission mismatch: got %v, want %v\n"+
			"  What: ParamSize must be emitted only for fixture models that join a curated quant_vram.json entry\n"+
			"  Why: ParamSize carries the #size identity dimension; an uncurated model emitting it would drift its entity key, "+
			"and a curated model NOT emitting it would silently disconnect the join\n"+
			"  Where: genToModelInfoDetailed (info.ParamSize = bestiary.ParamSizeFor(id)) + generateSource ParamSize emission\n"+
			"  How to fix: ensure the join fires for curated IDs and ParamSize stays empty (unemitted) for the rest",
			gotValues, want)
	}
}

// TestCodegen_IngestedAt_Deterministic verifies that IngestedAt
// lines in generated source are byte-identical across runs WITHOUT normalization.
// IngestedAt is committed-snapshot input from datasources.json, never a codegen
// wall-clock stamp — so two runs always produce the identical value. The test
// currently only asserts the fixture-based output is stable (no IngestedAt lines
// are emitted from the fixture, which has no datasource data) as a structural
// guard. When DatasetIngested emission is wired in the provenance-core work, this
// test will extend to verify the emitted IngestedAt lines are byte-identical.
func TestCodegen_IngestedAt_Deterministic(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)

	// Run twice with DIFFERENT LastSynced stamps to confirm that IngestedAt
	// (if present) is unaffected by the wall-clock stamp.
	src1, _, _ := runFixtureCodegen(t, fixtureJSON, "2000-01-01T00:00:00Z")
	src2, _, _ := runFixtureCodegen(t, fixtureJSON, "2099-12-31T23:59:59Z")

	// Normalize LastSynced (the known residual) from both sides.
	n1 := string(normalizeLastSynced(src1))
	n2 := string(normalizeLastSynced(src2))

	if n1 != n2 {
		t.Errorf("IngestedAt determinism: generated source differs after LastSynced normalization\n"+
			"  What: non-determinism beyond LastSynced detected in static codegen output\n"+
			"  Why: a field other than LastSynced varies between runs (possible new time.Now() call)\n"+
			"  Where: cmd/bestiary-gen generateSource\n"+
			"  How to fix: ensure no codegen field other than LastSynced uses a wall-clock value\n"+
			"  Diff context: src1 len=%d, src2 len=%d",
			len(src1), len(src2))
	}

	// Structural guard: the hermetic fixture carries no datasource data, so no
	// IngestedAt field may be emitted. Asserting its absence keeps this guard from
	// going vacuously green — if DatasetIngested emission ever fires for the
	// fixture without intent, this fails loudly. When DatasetIngested emission is
	// wired in the provenance-core work, flip this to assert the emitted IngestedAt
	// lines are present and byte-identical without normalization.
	if strings.Contains(n1, "IngestedAt:") {
		t.Errorf("fixture codegen emitted an IngestedAt field; expected none (the fixture has no datasource data)\n" +
			"  What: an IngestedAt line appeared in fixture-based output that carries no datasource records\n" +
			"  Why: either DatasetIngested emission was wired without updating this guard, or a stray field leaked\n" +
			"  Where: cmd/bestiary-gen generateSource\n" +
			"  How to fix: if DatasetIngested emission is now intended, replace this absence check with a " +
			"byte-identity assertion over the emitted IngestedAt lines (committed snapshot, never time.Now())",
		)
	}
}

// TestEntitySource_Deterministic is a structural guard that verifies the
// fixture-based output is byte-identical across same-timestamp runs. When
// EntitySource emission is wired in the provenance-core work it will be extended
// to assert sorted (EntityKey, SourceID) order and byte-identity across two runs.
// The test currently acts as a compile-and-run guard that the fixture codegen
// still produces valid, deterministic output.
func TestEntitySource_Deterministic(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	src1, _, _ := runFixtureCodegen(t, fixtureJSON, "2000-01-01T00:00:00Z")
	src2, _, _ := runFixtureCodegen(t, fixtureJSON, "2000-01-01T00:00:00Z")

	// Both runs with the same timestamp must be byte-identical (no residual).
	if !bytes.Equal(src1, src2) {
		t.Errorf("EntitySource determinism: same-timestamp runs produced different output\n" +
			"  What: codegen is non-deterministic even with a fixed timestamp\n" +
			"  How to fix: eliminate all non-deterministic sources (map iteration, time.Now, etc.)")
	}
}

// TestCodegen_BaseRef_LineageEdge exercises the finetune-lineage wiring directly
// through appendFinetuneLineage (the helper genToModelInfoDetailed calls), pinning
// the full parent EntityRef, the append-not-replace semantics, and the defensive
// copy that prevents aliasing the curated ledger's backing slice. It also pins the
// parseBaseRef decomposition (Family/Version/ParamSize/Modifier) and a
// non-decomposable base_ref's behavior.
func TestCodegen_BaseRef_LineageEdge(t *testing.T) {
	// parseBaseRef must decompose the curated Ollama-style base ref into the FULL
	// parent EntityRef — Modifier and ParamSize included. The parent ref is the
	// finetune edge's key, so a dropped modifier would silently point the edge at
	// the wrong entity (llama@3#70b instead of llama@3#70b{instruct}).
	t.Run("parseBaseRef_full_ref", func(t *testing.T) {
		ref := parseBaseRef("llama3:70b-instruct")
		if got, want := ref.String(), "llama@3#70b{instruct}"; got != want {
			t.Errorf("parseBaseRef(%q).String() = %q, want %q\n"+
				"  What: base_ref decomposition lost a field (Family/Version/ParamSize/Modifier)\n"+
				"  Where: parseBaseRef tag scan + EntityModifiers projection\n"+
				"  How to fix: ensure the param-size token and the identity-class modifier are both carried",
				"llama3:70b-instruct", got, want)
		}
		// Pin each component individually so a regression names the lost field.
		if string(ref.Family) != "llama" {
			t.Errorf("Family = %q, want %q", ref.Family, "llama")
		}
		if ref.Variant != "" {
			t.Errorf("Variant = %q, want %q", ref.Variant, "")
		}
		if ref.Version != "3" {
			t.Errorf("Version = %q, want %q", ref.Version, "3")
		}
		if ref.ParamSize != "70b" {
			t.Errorf("ParamSize = %q, want %q", ref.ParamSize, "70b")
		}
		if len(ref.Modifier) != 1 || ref.Modifier[0] != "instruct" {
			t.Errorf("Modifier = %v, want [instruct]", ref.Modifier)
		}
	})

	// appendFinetuneLineage must append exactly one DerivationFinetune edge whose
	// parent is the decomposed base ref. Deleting the append (the production-wiring
	// mutant) leaves Lineage empty and fails here.
	t.Run("append_edge_from_base_ref", func(t *testing.T) {
		info := bestiary.ModelInfo{}
		appendFinetuneLineage(&info, "llama3:70b-instruct")
		if len(info.Lineage) != 1 {
			t.Fatalf("len(Lineage) = %d, want 1 (the appended finetune edge)", len(info.Lineage))
		}
		got := info.Lineage[0]
		if got.Kind != bestiary.DerivationFinetune {
			t.Errorf("Kind = %v, want DerivationFinetune", got.Kind)
		}
		if got.Parent.String() != "llama@3#70b{instruct}" {
			t.Errorf("Parent.String() = %q, want %q", got.Parent.String(), "llama@3#70b{instruct}")
		}
	})

	// An empty base_ref is a no-op: models without a curated finetune base keep nil
	// Lineage (the base-model majority).
	t.Run("empty_base_ref_is_noop", func(t *testing.T) {
		info := bestiary.ModelInfo{}
		appendFinetuneLineage(&info, "")
		if info.Lineage != nil {
			t.Errorf("Lineage = %v, want nil for an empty base_ref", info.Lineage)
		}
	})

	// A non-decomposable base_ref (no colon, no glued version, no tag tokens) still
	// appends an edge — parseBaseRef never panics and always returns a non-zero
	// EntityRef. "123" has no colon and no leading-alpha prefix so the version regex
	// does not match; strings.Cut gives model="123", tag="". ParseFamilyWithVersion
	// treats "123" as a family pass-through, so Family="123" and all other fields are
	// empty. The edge is still recorded rather than dropped.
	t.Run("non_decomposable_base_ref_still_appends", func(t *testing.T) {
		info := bestiary.ModelInfo{}
		appendFinetuneLineage(&info, "123")
		if len(info.Lineage) != 1 {
			t.Fatalf("len(Lineage) = %d, want 1 (edge appended even for a degenerate base_ref)", len(info.Lineage))
		}
		if info.Lineage[0].Kind != bestiary.DerivationFinetune {
			t.Errorf("Kind = %v, want DerivationFinetune", info.Lineage[0].Kind)
		}
		if got := string(info.Lineage[0].Parent.Family); got != "123" {
			t.Errorf("Parent.Family = %q, want %q (degenerate input passed through as family)", got, "123")
		}
	})

	// Append-not-replace + no-aliasing: when LineageFor already supplied ledger
	// edges, the base_ref edge is appended after them, and the append must NOT write
	// into the caller's backing array. We seed a 1-element slice with spare capacity
	// (cap 4) — a naive append into that array would be observable here.
	t.Run("append_preserves_existing_and_does_not_alias", func(t *testing.T) {
		existing := make([]bestiary.LineageEdge, 1, 4)
		existing[0] = bestiary.LineageEdge{
			Parent: bestiary.EntityRef{Family: "gemma", Version: "2"},
			Kind:   bestiary.DerivationDistillation,
		}
		info := bestiary.ModelInfo{Lineage: existing}

		appendFinetuneLineage(&info, "llama3:70b-instruct")

		if len(info.Lineage) != 2 {
			t.Fatalf("len(Lineage) = %d, want 2 (existing ledger edge + appended finetune edge)", len(info.Lineage))
		}
		if info.Lineage[0].Kind != bestiary.DerivationDistillation {
			t.Errorf("first edge Kind = %v, want DerivationDistillation (existing edge must be preserved, not replaced)", info.Lineage[0].Kind)
		}
		if info.Lineage[1].Kind != bestiary.DerivationFinetune ||
			info.Lineage[1].Parent.String() != "llama@3#70b{instruct}" {
			t.Errorf("second edge = %+v, want DerivationFinetune to llama@3#70b{instruct}", info.Lineage[1])
		}
		// Aliasing guard: the caller's original backing array (len 1, cap 4) must
		// not have been written past index 0. A naive append into the shared array
		// would have placed the finetune edge at existing[0:2]'s underlying [1].
		aliased := existing[:2]
		if aliased[1].Kind == bestiary.DerivationFinetune {
			t.Errorf("appendFinetuneLineage wrote into the caller's backing array (aliasing)\n"+
				"  What: the appended edge landed in the shared LineageFor slice storage\n"+
				"  Why: append reused spare capacity instead of copying first\n"+
				"  Where: appendFinetuneLineage\n"+
				"  How to fix: copy info.Lineage into a fresh slice before appending; got aliased[1]=%+v", aliased[1])
		}
	})
}

// TestCodegen_QuantVRAMLiteral_Deterministic verifies that quantVRAMLiteral
// produces byte-identical output across repeated calls with the same input,
// using the curated llama-3.3-70b-instruct rows as the baked anchor.
func TestCodegen_QuantVRAMLiteral_Deterministic(t *testing.T) {
	// Obtain and bake the llama-3.3-70b rows.
	const modelID = bestiary.ModelID("llama-3.3-70b-instruct")
	const bakeCtx = 131072

	rawRows := bestiary.QuantVRAMFor(modelID)
	if rawRows == nil {
		t.Fatal("QuantVRAMFor(llama-3.3-70b-instruct) returned nil")
	}
	baked := make([]bestiary.QuantVRAM, len(rawRows))
	for i, row := range rawRows {
		row.VRAMBytes = bestiary.EstimateVRAMBytes(row.WeightsBytes, bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
		row.VRAMContextTokens = bakeCtx
		row.VRAMEstimatePartial = bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)
		baked[i] = row
	}

	// quantVRAMLiteral must be pure and deterministic.
	lit1 := quantVRAMLiteral(baked)
	lit2 := quantVRAMLiteral(baked)
	if lit1 != lit2 {
		t.Errorf("quantVRAMLiteral produced different output on repeated calls with the same input\n" +
			"  What: non-deterministic literal generation\n" +
			"  How to fix: ensure quantVRAMLiteral does not use map iteration or time.Now()")
	}

	// Sanity: the literal must contain the expected Quant constant names.
	if !strings.Contains(lit1, "QuantQ4_K_M") {
		t.Errorf("quantVRAMLiteral: missing QuantQ4_K_M in output\nliteral: %s", lit1)
	}
	if !strings.Contains(lit1, "QuantQ8_0") {
		t.Errorf("quantVRAMLiteral: missing QuantQ8_0 in output\nliteral: %s", lit1)
	}
	if !strings.Contains(lit1, "QuantF16") {
		t.Errorf("quantVRAMLiteral: missing QuantF16 in output\nliteral: %s", lit1)
	}
	// VRAMEstimatePartial must be false for these arch-complete rows.
	if strings.Contains(lit1, "VRAMEstimatePartial: true") {
		t.Errorf("quantVRAMLiteral: unexpected VRAMEstimatePartial: true for arch-complete rows\nliteral: %s", lit1)
	}
}

// TestCodegen_UpToDate_RealInput is the real-input regen up-to-date guard. It
// regenerates every codegen-owned source IN MEMORY from the COMMITTED vendored
// snapshot (parse/data/modelsdev/catalog.json) via the exact generation sequence
// run() uses, then byte-compares each fresh source against the COMMITTED
// *_gen.go file on disk — with LastSynced lines normalized on both sides (the
// same reLastSynced/normalizeLastSynced machinery TestCodegen_Reproducible_ByteIdentical
// uses). It FAILS if any generated file is stale relative to the vendored
// snapshot plus the current emitter logic.
//
// This is the guard whose absence let a stale regen ship invisibly: the
// hermetic TestCodegen_UpToDate checks only golden EXCERPTS from a fixture, and
// TestCodegen_Reproducible_ByteIdentical proves run-to-run stability but not
// agreement with what is committed. Only a full committed-vs-fresh comparison
// over the real 5654-model catalog catches a generated file that was hand-edited,
// left un-regenerated after an emitter or data change, or captured in a
// non-canonical form. The comparison is generation-only (the generate* functions
// return bytes; nothing is written to disk), so the committed files are never
// mutated by the test.
//
// Not parallel: it temporarily repoints the vendoredCatalogPath global at the
// module-root-resolved committed snapshot.
func TestCodegen_UpToDate_RealInput(t *testing.T) {
	// `go test` runs with the package dir as CWD, but the vendored catalog and the
	// committed generated files live at the module root; resolve both from this
	// test file's location (two levels up).
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate the module root")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// Point fetchModelsWithRaw at the committed snapshot (absolute, so it resolves
	// regardless of the test CWD).
	orig := vendoredCatalogPath
	vendoredCatalogPath = filepath.Join(moduleRoot, vendoredCatalogPath)
	t.Cleanup(func() { vendoredCatalogPath = orig })

	_, models, metadata, providerMeta, _, err := fetchModelsWithRaw(context.Background(), true)
	if err != nil {
		t.Fatalf("fetchModelsWithRaw(noFetch=true) over the committed vendored catalog: %v\n"+
			"  How to fix: run the models.dev snapshot refresh (see AGENTS.md)", err)
	}

	// Reproduce run()'s generation sequence exactly (main.go run()):
	//   allSlugs (sorted) -> collectFamilies -> applyFilter(no flags) -> the five
	//   generate* emitters, with slugToConst built from providerConstName.
	// LastSynced is intentionally NOT stamped here: normalizeLastSynced neutralizes
	// the field on both sides, so the on-disk wall-clock stamp is irrelevant.
	allSlugs := make([]string, 0, len(providerMeta))
	for slug := range providerMeta {
		allSlugs = append(allSlugs, slug)
	}
	sort.Strings(allSlugs)

	familyMeta := collectFamilies(models)
	filtered := applyFilter(models, nil, nil)

	slugToConst := make(map[string]string, len(allSlugs))
	for _, slug := range allSlugs {
		slugToConst[slug] = providerConstName(slug, providerMeta[slug].Name)
	}

	providersSrc, err := generateProvidersSource(allSlugs, providerMeta)
	if err != nil {
		t.Fatalf("generateProvidersSource: %v", err)
	}
	familiesSrc, err := generateFamiliesSource(familyMeta)
	if err != nil {
		t.Fatalf("generateFamiliesSource: %v", err)
	}
	staticSrc, err := generateSource(filtered, slugToConst)
	if err != nil {
		t.Fatalf("generateSource: %v", err)
	}
	constantsSrc, err := generateConstantsSource(models, slugToConst)
	if err != nil {
		t.Fatalf("generateConstantsSource: %v", err)
	}
	metadataSrc, err := generateMetadataSource(metadata)
	if err != nil {
		t.Fatalf("generateMetadataSource: %v", err)
	}

	fresh := []struct {
		relPath string
		src     []byte
	}{
		{outputPath, staticSrc},
		{outputConstantsPath, constantsSrc},
		{outputMetadataPath, metadataSrc},
		{outputFamiliesPath, familiesSrc},
		{outputProvidersPath, providersSrc},
	}

	for _, f := range fresh {
		committedPath := filepath.Join(moduleRoot, f.relPath)
		committed, readErr := os.ReadFile(committedPath)
		if readErr != nil {
			t.Errorf("read committed generated file %q: %v\n"+
				"  How to fix: run `go generate ./...` to (re)create it", committedPath, readErr)
			continue
		}

		freshNorm := normalizeLastSynced(f.src)
		committedNorm := normalizeLastSynced(committed)
		if bytes.Equal(freshNorm, committedNorm) {
			continue
		}

		// Report the first divergent line to make the staleness reviewable without
		// dumping the whole (multi-MB) file.
		freshLines := strings.Split(string(freshNorm), "\n")
		commLines := strings.Split(string(committedNorm), "\n")
		firstDiff, ln, freshLn, commLn := -1, 0, "", ""
		for ln < len(freshLines) || ln < len(commLines) {
			var fl, cl string
			if ln < len(freshLines) {
				fl = freshLines[ln]
			}
			if ln < len(commLines) {
				cl = commLines[ln]
			}
			if fl != cl {
				firstDiff, freshLn, commLn = ln+1, fl, cl
				break
			}
			ln++
		}

		t.Errorf(
			"committed %s is STALE relative to the vendored snapshot + current emitter logic\n"+
				"  What: a fresh --no-fetch regen of this file (LastSynced normalized) does not match what is committed\n"+
				"  Why: the generated file was hand-edited, or left un-regenerated after an emitter or curated-data change\n"+
				"  Where: %s (committed) vs in-memory regen from %s\n"+
				"  First divergent line (%d):\n"+
				"    committed: %q\n"+
				"    fresh    : %q\n"+
				"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch` and commit the regenerated files as a chore(gen) commit",
			f.relPath, f.relPath, vendoredCatalogPath, firstDiff, commLn, freshLn,
		)
	}
}
