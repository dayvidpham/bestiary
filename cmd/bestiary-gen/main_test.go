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
// makes an outbound request, the contacted flag trips.
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
