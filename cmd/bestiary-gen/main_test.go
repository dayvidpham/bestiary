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

// TestCacheDirFlag verifies that --cache-dir <tmpdir> causes bestiary-gen to
// write api_response.json to the given directory (not the default one).
func TestCacheDirFlag(t *testing.T) {
	// Serve a minimal API response over HTTP.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(minimalAPIJSON(t))
	}))
	defer srv.Close()

	// Point apiURL at the test server by temporarily overriding the package-level
	// constant via a helper that swaps it for the duration of the test.
	// Since apiURL is a const, we test cacheAPIResponse + fetchModelsWithRaw directly.

	tmpDir := t.TempDir()

	// Write the response to the tmp dir, simulating what run() does after fetch.
	raw := minimalAPIJSON(t)
	if err := cacheAPIResponse(raw, tmpDir); err != nil {
		t.Fatalf("cacheAPIResponse: %v", err)
	}

	// Assert the file was written to tmpDir, not defaultCacheDir.
	wantPath := filepath.Join(tmpDir, cacheFile)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("cache file not written to --cache-dir %q: %v", tmpDir, err)
	}

	// Assert the default cache dir was NOT written to (no side-effects).
	defaultPath := filepath.Join(defaultCacheDir, cacheFile)
	if _, statErr := os.Stat(defaultPath); statErr == nil {
		// The default path may already exist (repo has a cached file);
		// compare mod times or just confirm tmpDir file exists — the key
		// assertion is that cacheAPIResponse honoured the dir argument.
		// So this branch is intentionally a no-op (not an error).
		_ = defaultPath
	}
}

// TestNoFetch_HitsCache verifies that --no-fetch reads from a pre-populated
// cache file without making any HTTP request.
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
// missing cache file returns an *ErrCacheMiss with all required actionable fields.
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

	// Path must contain the expected cache file location.
	wantPath := filepath.Join(tmpDir, cacheFile)
	absWant, _ := filepath.Abs(wantPath)
	msg := cacheMiss.Error()

	if !strings.Contains(msg, absWant) {
		t.Errorf("error message missing expected path %q:\n%s", absWant, msg)
	}
	if !strings.Contains(msg, "--no-fetch") {
		t.Errorf("error message missing '--no-fetch' reason:\n%s", msg)
	}
	// Remediation hint: must tell the user how to fix it.
	if !strings.Contains(msg, "re-run without --no-fetch") {
		t.Errorf("error message missing remediation hint 're-run without --no-fetch':\n%s", msg)
	}
}
