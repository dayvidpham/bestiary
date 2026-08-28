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
	corpus := loadGenCorpus[genSlugInput, string](t, genSlugToIdentifierCorpusJSON, 9)
	genRequireInputCoverage(t, corpus, map[genSlugInput]string{
		{Slug: "302ai", NameHint: "302AI"}:                                 "302AI",
		{Slug: "sap-ai-core", NameHint: "SAP AI Core"}:                     "SAPAICore",
		{Slug: "cloudflare-ai-gateway", NameHint: "Cloudflare AI Gateway"}: "CloudflareAIGateway",
	})
	runGenSlugCorpus(t, corpus)
}

// TestSlugToIdentifier_DigitLeadingVariants covers digit-alpha combinations.
func TestSlugToIdentifier_DigitLeadingVariants(t *testing.T) {
	corpus := loadGenCorpus[genSlugInput, string](t, genSlugToIdentifierDigitLeadingCorpusJSON, 2)
	genRequireInputCoverage(t, corpus, map[genSlugInput]string{
		{Slug: "3ai", NameHint: "3AI"}: "3AI",
	})
	runGenSlugCorpus(t, corpus)
}

// TestProviderConstName verifies that providerConstName produces valid Go identifiers.
func TestProviderConstName(t *testing.T) {
	corpus := loadGenCorpus[genSlugInput, string](t, genProviderConstNameCorpusJSON, 7)
	genRequireInputCoverage(t, corpus, map[genSlugInput]string{
		{Slug: "302ai", NameHint: "302AI"}: "Provider302AI",
		{Slug: "xai", NameHint: "xAI"}:     "ProviderxAI",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := providerConstName(c.Input.Slug, c.Input.NameHint); got != c.Expected {
				t.Errorf("providerConstName(%q, %q) = %q, want %q", c.Input.Slug, c.Input.NameHint, got, c.Expected)
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
	corpus := loadGenCorpus[string, []string](t, genSplitCommaCorpusJSON, 4)
	genRequireNameCoverage(t, corpus, "two-values", "empty-is-nil", "padded-values")
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := splitComma(c.Input)
			if len(got) != len(c.Expected) {
				t.Fatalf("splitComma(%q) = %v, want %v", c.Input, got, c.Expected)
			}
			for i, g := range got {
				if g != c.Expected[i] {
					t.Errorf("splitComma(%q)[%d] = %q, want %q", c.Input, i, g, c.Expected[i])
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// tests: entityConstName, buildEntityConstEntries, generateEntitiesConstantsSource
// --------------------------------------------------------------------------

// canonNomen builds a Canonical-scheme Nomen for ref — the shape
// buildEntityConstEntries consumes when deriving Entity__ constants.
func canonNomen(ref bestiary.EntityRef) bestiary.Nomen {
	return bestiary.Nomen{Value: ref.String(), Scheme: bestiary.NomenSchemeCanonical, ResolvesTo: ref}
}

// TestEntityConstName_PinnedExamples pins the Entity__ word-sentinel grammar on the
// ratified worked examples (Plan-UAT: __Version_/__Size_ word sentinels replace __At/__S).
// The version-vs-variant pins (deepseek@3.2 vs deepseek/v3.2) and the version-sanitize
// pins (qwen@3.5 vs qwen@35) are the injectivity-critical cases.
func TestEntityConstName_PinnedExamples(t *testing.T) {
	corpus := loadGenCorpus[genEntityRefInput, string](t, genEntityConstNamePinnedCorpusJSON, 6)
	genRequireNameCoverage(t, corpus,
		"deepseek-version-3-2", "deepseek-variant-v3-2",
		"qwen-version-3-5", "qwen-version-35",
		"llama-scout-full-tuple",
	)
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ref := bestiary.EntityRef{
				Family:    bestiary.Family(c.Input.Family),
				Variant:   c.Input.Variant,
				Version:   c.Input.Version,
				ParamSize: c.Input.ParamSize,
				Modifier:  c.Input.Modifier,
			}
			if got := entityConstName(ref); got != c.Expected {
				t.Errorf("entityConstName(%q) = %q, want %q", ref.String(), got, c.Expected)
			}
		})
	}
}

// TestEntityConstName_SeparatorPreserving verifies the sanitizer is separator-preserving
// (no camel-fold): "3.2" -> "3_2" is DISTINCT from "32" -> "32", and a hyphenated family
// keeps each separator as its own underscore (no fold to camelCase).
func TestEntityConstName_SeparatorPreserving(t *testing.T) {
	if a, b := entityConstName(bestiary.EntityRef{Family: "qwen", Version: "3.5"}), entityConstName(bestiary.EntityRef{Family: "qwen", Version: "35"}); a == b {
		t.Errorf("version 3.5 and 35 must render distinctly, both = %q", a)
	}
	if got, want := entityConstName(bestiary.EntityRef{Family: "text-embedding", Version: "3"}), "Entity__Text_embedding__Version_3"; got != want {
		t.Errorf("hyphenated family: got %q, want %q", got, want)
	}
}

// TestBuildEntityConstEntries_RealPairsPass verifies the injectivity-critical real
// collision pairs (deepseek version-vs-variant, qwen version-sanitize) PASS the guard:
// they render to DISTINCT names, so the bake succeeds and emits all four.
func TestBuildEntityConstEntries_RealPairsPass(t *testing.T) {
	refs := []bestiary.EntityRef{
		{Family: "deepseek", Version: "3.2"},
		{Family: "deepseek", Variant: "v3.2"},
		{Family: "qwen", Version: "3.5"},
		{Family: "qwen", Version: "35"},
	}
	var nomina []bestiary.Nomen
	keySet := map[string]bool{}
	for _, r := range refs {
		nomina = append(nomina, canonNomen(r))
		keySet[r.String()] = true
	}
	entries, err := buildEntityConstEntries(nomina, keySet)
	if err != nil {
		t.Fatalf("buildEntityConstEntries: unexpected collision error on distinct real pairs: %v", err)
	}
	if len(entries) != len(refs) {
		t.Fatalf("buildEntityConstEntries: got %d entries, want %d", len(entries), len(refs))
	}
}

// TestBuildEntityConstEntries_InjectivityGuard_NegativeControl is the guard's negative
// control: two DISTINCT entity keys crafted to render the SAME identifier (a variant
// "bar" and a modifier "bar" both become the "__Bar" segment) MUST fail the bake loudly.
// Without this control, the real-data pairs pass whether or not the guard is live.
func TestBuildEntityConstEntries_InjectivityGuard_NegativeControl(t *testing.T) {
	refVariant := bestiary.EntityRef{Family: "foo", Variant: "bar"}
	refMod := bestiary.EntityRef{Family: "foo", Modifier: []string{"bar"}}
	if refVariant.String() == refMod.String() {
		t.Fatalf("test setup: keys must be distinct, both = %q", refVariant.String())
	}
	if entityConstName(refVariant) != entityConstName(refMod) {
		t.Fatalf("test setup: the crafted refs must collide on name; got %q vs %q", entityConstName(refVariant), entityConstName(refMod))
	}
	nomina := []bestiary.Nomen{canonNomen(refVariant), canonNomen(refMod)}
	keySet := map[string]bool{refVariant.String(): true, refMod.String(): true}
	if _, err := buildEntityConstEntries(nomina, keySet); err == nil {
		t.Fatal("buildEntityConstEntries: expected a LOUD collision error on the crafted duplicate-name pair, got nil")
	} else if !strings.Contains(err.Error(), "collision") || !strings.Contains(err.Error(), refVariant.String()) || !strings.Contains(err.Error(), refMod.String()) {
		t.Errorf("buildEntityConstEntries: collision error must name both colliding keys; got: %v", err)
	}
}

// TestBuildEntityConstEntries_FiltersNonCanonical verifies only Preferred (Canonical)
// nomina become constants — ProviderID/Alias nomina are ignored — and a Canonical nomen
// whose key is absent from keySet (a claim for a non-catalog entity) is dropped.
func TestBuildEntityConstEntries_FiltersNonCanonical(t *testing.T) {
	ref := bestiary.EntityRef{Family: "deepseek", Version: "3.2"}
	nomina := []bestiary.Nomen{
		canonNomen(ref),
		{Value: "deepseek-v3.2", Scheme: bestiary.NomenSchemeProviderID, ResolvesTo: ref},
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, ResolvesTo: bestiary.EntityRef{Family: "grok", Version: "4.20"}},
		{Value: "ghost@9", Scheme: bestiary.NomenSchemeCanonical, ResolvesTo: bestiary.EntityRef{Family: "ghost", Version: "9"}},
	}
	keySet := map[string]bool{ref.String(): true}
	entries, err := buildEntityConstEntries(nomina, keySet)
	if err != nil {
		t.Fatalf("buildEntityConstEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].key != ref.String() {
		t.Fatalf("buildEntityConstEntries: want exactly the one Canonical in-set entry, got %+v", entries)
	}
}

// TestGenerateEntitiesConstantsSource_Compiles verifies the emitter returns valid Go
// source (passes go/format) carrying the expected Entity__ constant, the
// allEntityConstants backing array, and the EntityKeys() accessor.
func TestGenerateEntitiesConstantsSource_Compiles(t *testing.T) {
	models := []bestiary.ModelInfo{
		{ID: "claude-opus-4-20250514", Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4"},
		{ID: "gpt-4o", Provider: "openai", Family: "gpt", Variant: "4o"},
	}
	src, err := generateEntitiesConstantsSource(models, nil)
	if err != nil {
		t.Fatalf("generateEntitiesConstantsSource: unexpected error: %v", err)
	}
	if len(src) == 0 {
		t.Fatal("generateEntitiesConstantsSource: returned empty source")
	}
	srcNorm := normalizeWhitespace(string(src))
	for _, want := range []string{
		`Entity__Claude__Opus__Version_4 = "claude/opus@4"`,
		`Entity__Gpt__4o = "gpt/4o"`,
		"func EntityKeys()",
		"var allEntityConstants",
	} {
		if !strings.Contains(srcNorm, normalizeWhitespace(want)) {
			t.Errorf("generated entity-constants source missing %q\n%s", want, string(src)[:min(800, len(src))])
		}
	}
}

// min is a small helper kept local for error-truncation slices in this package's tests.
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
	info, _, _ := enrichModelInfo(base)

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
			info, _, _ := enrichModelInfo(tc.base)

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
// justification.
// SET-equality (not count) catches a DIFFERENT id going divergent while another converges
// — the count could stay put while the set changes. Do NOT pad this map to force green: every
// row must be independently justified; an unexpected divergence is a STOP-and-surface.
//
// The prior residual (nvidia/llama-3.3-nemotron-super-49b-v1.5) was FOLDED to family
// nemotron via the curated idFamilyOverrides entry — both providers converge on
// (nemotron,v1.5,3.3) — so it is gone from the set.
//
// Every row is a REAL upstream disagreement about the raw family a provider publishes for
// a shared model ID. None is a pipeline regression: each row's tuples are exactly what the
// two upstream raw-family spellings decompose to, and they are carried as honest residuals
// rather than papered over with curation (curating them would move entity keys, which a
// corpus refresh must not do in the same act).
//
// 4 -> 12 with the 2026-08-28 models.dev catalog refresh and its declared corpus
// re-capture (7,430 records, up from 5,765). One row LEFT the set — sakana/fugu-ultra
// converged, because vercel corrected its "aura" mislabel to "fugu" upstream — and nine
// joined it. The nine are three distinct disagreements, not nine independent ones:
//
//   - The ByteDance Seed line (5 ids) is published under THREE raw-family spellings:
//     "seed", "doubao-seed" and "doubao". volcengine says "doubao-seed", its resellers
//     say "seed". This one has already leaked into the keyspace — doubao-seed@1.6,
//     doubao-seed/pro@2.0, doubao-seed/lite@2.0, doubao-seed/mini@2.0 and four siblings
//     now sit beside the seed/* keys for the same product line. A family_aliases.json row
//     (doubao-seed -> seed) would converge all of them, but that file's own contract
//     requires user sign-off for a fold of this shape, and it would move entity keys, so
//     it is deliberately DEFERRED to a curation slice rather than smuggled into a refresh.
//   - Two member-variant reads (z-ai/glm-4.7-flashx, upstage/solar-pro4): one provider
//     resolves a glued tier token ("flashx", "pro4") to the member and the other leaves it
//     on the bare family. Category C in the divergence report.
//   - Two genuine mislabels (qvq-max, openai/gpt-latest): a provider files Alibaba's QVQ
//     visual-reasoning line under "qwen", and files OpenAI's rolling gpt-latest under the
//     "sol" tier. Category D.
var crossProviderJustifiedResidual = map[string]string{
	"text-embedding-3-small": "REAL UPSTREAM DIVERGENCE (hidden by the stale fixture): openai/azure/azure-cognitive-services publish raw family \"text-embedding\" → (text-embedding,small,3), while sap-ai-core publishes NO raw family, so the ID-driven path reads the family token as \"text-embedding-3\" → (text-embedding-3,small,3). Two spellings of one model, not a decomposition regression.",
	"text-embedding-3-large": "REAL UPSTREAM DIVERGENCE (hidden by the stale fixture): same empty-raw-vs-populated-raw split as text-embedding-3-small — openai/azure/azure-cognitive-services say \"text-embedding\", sap-ai-core says nothing and the ID yields \"text-embedding-3\".",
	"poolside/laguna-s-2.1":  "REAL UPSTREAM DIVERGENCE: poolside labels its own model raw family \"laguna\" → (laguna,\"\",\"\"), while openrouter and vercel label it \"laguna-s\" → (laguna-s,\"\",2.1). The lab and its resellers disagree on where the line designator ends.",

	// The ByteDance Seed line, published under three raw-family spellings. Deferred to a
	// curation slice (family_aliases.json doubao-seed -> seed) because converging it moves
	// entity keys and that file requires sign-off for a fold of this shape.
	"volcengine/doubao-seed-2.0-pro":  "REAL UPSTREAM DIVERGENCE: volcengine publishes raw family \"doubao-seed\" → (doubao-seed,pro,2.0); its resellers publish \"seed\" → (seed,pro,\"\"). One product line, two vendor spellings of the family token.",
	"volcengine/doubao-seed-2.0-lite": "REAL UPSTREAM DIVERGENCE: same doubao-seed↔seed spelling split as doubao-seed-2.0-pro, on the lite tier.",
	"volcengine/doubao-seed-2.0-mini": "REAL UPSTREAM DIVERGENCE: same doubao-seed↔seed spelling split as doubao-seed-2.0-pro, on the mini tier.",
	"volcengine/doubao-seed-2.0-code": "REAL UPSTREAM DIVERGENCE: same doubao-seed↔seed spelling split as doubao-seed-2.0-pro, with no tier token.",
	"doubao-seed-1-6-vision-250815":   "REAL UPSTREAM DIVERGENCE: the THIRD spelling of the same family — volcengine publishes \"doubao\" → (doubao,\"\",1.6) while ofox publishes \"seed\" → (seed,\"\",\"\"). Same deferral as its 2.0 siblings.",

	// Member-variant reads: a glued tier token that one provider resolves to the member
	// and the other leaves on the bare family. Category C in the divergence report.
	"z-ai/glm-4.7-flashx": "REAL UPSTREAM DIVERGENCE: one provider reads the glued \"flashx\" tier as the flash member → (glm,flash,4.7), another leaves it on the bare family → (glm,\"\",4.7). Curating it would have to rule on whether FlashX is the Flash tier or its own; left as an honest residual.",
	"upstage/solar-pro4":  "REAL UPSTREAM DIVERGENCE: \"pro4\" is read as the pro member by one provider → (solar,pro,\"\") and left on the bare family by another → (solar,\"\",\"\"). The glued generation digit is what defeats agreement.",

	// Genuine family mislabels. Category D.
	"qvq-max":           "REAL UPSTREAM MISLABEL: QVQ is Alibaba's visual-reasoning line and is published as raw family \"qvq\" → (qvq,\"\",\"\"), but one provider files it under \"qwen\" → (qwen,max,\"\"). qvq is a DISTINCT family (it is in family_enforce.json), so this is the provider's error, not a decomposition one.",
	"openai/gpt-latest": "REAL UPSTREAM MISLABEL: one provider publishes OpenAI's rolling gpt-latest under the \"sol\" tier → (gpt,sol,\"\") while the rest leave it on the bare family → (gpt,\"\",\"\"). sol is a real GPT tier, so the mislabel is not detectable without knowing which artifact gpt-latest currently points at — exactly the moving-target problem StageLatest records.",
}

// crossProviderResidualUnaccountedCeiling pins the at-scale count of
// ReasonResidualUnaccountedTokens over the committed snapshot.
// Today only the non-gating stdout smoke (main.go) sees this; pinning it catches a
// non-fixture-family residual regression (the seed-flash class) that would otherwise slip
// every gate. Measured = 282 parse failures over the 5,765-record refreshed snapshot
// (was 243 over the 4,979-record stale fixture — the +39 are new upstream ids, not a
// pipeline regression: the diff gate reads changed=0, so no record's decomposition moved).
// Assert ≤ ceiling (tighten-only; a legitimate reduction passes, a regression that
// re-drops sole-residual/member coverage trips it).
//
// 282 -> 303 with the redundant leading-token strip. This ceiling counts tokens the
// decomposition did not ACCOUNT FOR, and a stripped prefix is by construction one of
// them: the whole point of the strip is that the token's fact lives on a DIFFERENT axis
// (Provider, or the Creator the family declares), so the (family, variant, version,
// modifier) tuple does not and should not absorb it. The +21 is therefore a measurement
// of the strip's reach, not a loss of coverage — and the companion floor below moves the
// other way in the same run, from 4,064 to 4,229 populated versions, which is the fact
// the strip was made for. A residual regression of the kind this ceiling exists to catch
// would raise the residual WITHOUT raising the version floor.
//
// 303 -> 353 with the 2026-08-28 catalog refresh and its declared corpus re-capture. This
// is a ceiling on an ABSOLUTE count over a corpus that grew 29% (5,765 -> 7,430 records),
// so the raw number had to move; what matters is whether coverage got WORSE, and it got
// BETTER: the residual RATE fell from 303/5,765 = 5.26% to 353/7,430 = 4.75%. The
// companion floor below moves the right way in the same run (4,229 -> 5,433 populated
// versions, 73.4% -> 73.1% of records, flat). Two curation pins in this refresh account
// for most of the improvement — the claude-fable family override, which stopped a whole
// new Anthropic tier decomposing as a compound family, and the Nemotron 3.5 Lightning
// version pins. A genuine residual regression is the case this ceiling exists to catch
// and looks different: the residual rate RISES while the version floor does not follow.
// Pinned at the MEASURED value, not padded — this ceiling is tighten-only by contract.
const crossProviderResidualUnaccountedCeiling = 353

// crossProviderPopulatedVersionFloor pins the at-scale count of snapshot records whose
// production decomposition yields a NON-EMPTY Version (landing pin; supersedes the
// stale 1681/293 figures). Measured = 4,229 records with a populated Version over the
// 5,765-record refreshed snapshot (was 4,064 before the redundant leading-token strip,
// and 3,401 over the older 4,979-record fixture). The +165 is the strip's yield: an id
// that repeats its provider's or its lab's name in front of the model name pushed the
// version scan one token late, so the artifact keyed an UNDATED sibling of the entity it
// belongs to; with the prefix gone the scan lands on the version the vendor published. Assert ≥ floor
// (loosen-only: more version coverage passes; a regression that drops version-presence
// — the inverse of the residual-ceiling guard — trips it). Pinned alongside the residual
// ceiling so both the "version populated" and "tokens unaccounted" at-scale counts are gated.
//
// 4,229 -> 5,433 with the 2026-08-28 catalog refresh and its declared corpus re-capture
// (5,765 -> 7,430 records). As a share of the corpus this is flat — 73.4% -> 73.1% — so
// the floor tracks corpus growth rather than any change in version coverage. Raising it
// keeps the guard as tight as it was: left at 4,229 it would have been satisfied by a
// decomposition that lost version presence on a thousand records.
const crossProviderPopulatedVersionFloor = 5433

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
	// pinned divergenceExact (4). A mismatch means the two gates DISAGREE — a gate-logic bug.
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

	// At-scale census log: the three figures this gate pins, each with its unit and
	// corpus size, so a refresh can re-pin them from the run rather than by guesswork.
	t.Logf("at-scale census over %d snapshot records: residual-unaccounted=%d (ceiling %d), populated-version=%d (floor %d)",
		len(records), residualUnaccounted, crossProviderResidualUnaccountedCeiling,
		populatedVersion, crossProviderPopulatedVersionFloor)

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
	corpus := loadGenCorpus[genSlugInput, string](t, genSlugToIdentifierChatGPTCorpusJSON, 2)
	genRequireInputCoverage(t, corpus, map[genSlugInput]string{
		{Slug: "chatgpt-4o", NameHint: "ChatGPT-4o"}: "ChatGPT4O",
	})
	runGenSlugCorpus(t, corpus)
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
// providers}) for the reproducibility tests. Under the entity-constants hard cut the
// three groups exercise convergence vs. distinctness of the Entity__ surface:
//
//   - B (prefix/kilo): "openrouter/free" + "kilo-auto/free" both decompose to the entity
//     "free" → they CONVERGE onto the single Entity__Free constant (no backend-route flavor).
//
//   - C (punctuation/cloudflare): "anthropic/claude-3.5-haiku" + "anthropic/claude-3-5-haiku"
//     both decompose to the entity claude/haiku@3.5 → they CONVERGE onto the single
//     Entity__Claude__Haiku__Version_3_5 constant (no provider flavor, no _N ordinal).
//
//   - E (version-pair / negative control): "gpt-5.1" + "gpt-5.2" decompose to DISTINCT
//     entities gpt@5.1 / gpt@5.2 → distinct Entity__Gpt__Version_5_1 / Entity__Gpt__Version_5_2
//     constants (the __Version_ word sentinel keeps them apart; injectivity guard never fires).
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
// codegenArtifacts is EVERY output one fixture codegen run produces — the three
// generated .go sources AND the committed non-.go emissions. It exists because the
// reproducibility guards below could previously only see the .go sources, so a
// committed JSON report emitted from a map range (or stamped with a wall clock) would
// have gone unguarded no matter how many iterations they ran.
//
// A slice that adds a new committed emission extends this struct and the byte-identity
// loop that consumes it; the emission itself must therefore be reachable as a PURE
// build* function returning bytes, never only as a file writer.
type codegenArtifacts struct {
	// staticSrc, constantsSrc and metadataSrc are the generated .go sources.
	staticSrc    []byte
	constantsSrc []byte
	metadataSrc  []byte
	// creatorProvidersUnserved and creatorsLabDisagreements are the committed
	// creator-dimension JSON reports (parse/data/creator_providers_unserved.json and
	// parse/data/creators_lab_disagreements.json).
	creatorProvidersUnserved []byte
	creatorsLabDisagreements []byte
	// modelsdevFieldCensus is the committed upstream field-shape census
	// (parse/data/modelsdev_field_census.json). It is built from the RAW catalog bytes
	// rather than the decomposed models, so it is the one artifact here that proves the
	// emitter is stable against JSON map-iteration order at the WIRE level.
	modelsdevFieldCensus []byte
}

// runFixtureCodegen returns just the three generated .go sources, for the many callers
// that only assert on those. It is a thin shim over runFixtureCodegenArtifacts.
func runFixtureCodegen(t *testing.T, fixtureJSON []byte, lastSynced string) (staticSrc, constantsSrc, metadataSrc []byte) {
	t.Helper()
	a := runFixtureCodegenArtifacts(t, fixtureJSON, lastSynced)
	return a.staticSrc, a.constantsSrc, a.metadataSrc
}

func runFixtureCodegenArtifacts(t *testing.T, fixtureJSON []byte, lastSynced string) codegenArtifacts {
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

	rawJSON, models, metadata, provMeta, _, err := fetchModelsWithRaw(context.Background(), false)
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

	var out codegenArtifacts
	out.staticSrc, err = generateSource(models, slugToConst)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: generateSource: %v", err)
	}
	out.constantsSrc, err = generateEntitiesConstantsSource(models, metadata)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: generateEntitiesConstantsSource: %v", err)
	}
	out.metadataSrc, err = generateMetadataSource(metadata)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: generateMetadataSource: %v", err)
	}
	// The committed non-.go emissions, built through the SAME pure functions the
	// writers call, so what this harness proves byte-identical is exactly what lands
	// on disk.
	out.creatorProvidersUnserved, err = buildCreatorProvidersUnserved(models)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: buildCreatorProvidersUnserved: %v", err)
	}
	out.creatorsLabDisagreements, err = buildCreatorsLabDisagreements(metadata)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: buildCreatorsLabDisagreements: %v", err)
	}
	out.modelsdevFieldCensus, err = buildModelsdevFieldCensus(rawJSON)
	if err != nil {
		t.Fatalf("runFixtureCodegenArtifacts: buildModelsdevFieldCensus: %v", err)
	}
	return out
}

// TestCodegen_Reproducible_ByteIdentical verifies that N=100 successive codegen runs
// over the same fixture data (each re-randomizing map iteration order via a fresh
// fetchModelsWithRaw) produce FULLY byte-identical output for generateSource,
// generateEntitiesConstantsSource, and generateMetadataSource — with NO normalization.
//
// The codegen LastSynced stamp is DETERMINISTIC: every run stamps the SAME value — the
// current models.dev ingest instant from the committed datasources.json
// (codegenLastSynced) — so the wall-clock is no longer a residual source of diff and this
// guard asserts RAW byte-identity across iterations with no masking.
//
// Fencing boundary: this test injects the stamp VALUE via runFixtureCodegen, so it
// proves the EMISSION is a pure function of (input, stamp) — it does not itself
// exercise run()'s stamp SOURCE. That source-side fence is now CLOSED by
// TestCodegen_UpToDate_RealInput: the committed *_gen.go files carry the
// deterministic stamp and that guard byte-compares a fresh regen (stamped through
// the same codegenLastSynced path run() uses) against them EXACTLY, with no
// LastSynced masking — so a regression of the stamp back to a wall-clock fails
// there. (The obsolete alternating-timestamp / sole-residual machinery is gone; the
// normalizeLastSynced helper stays for the deliberately wall-clock-injecting tests
// that still need it.)
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
	refArtifacts := runFixtureCodegenArtifacts(t, fixtureJSON, ts)
	refStatic, refConstants, refMetadata := refArtifacts.staticSrc, refArtifacts.constantsSrc, refArtifacts.metadataSrc

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

	// Verify reference constants contain the expected golden pins. In the entity-constants
	// world the provider leak is GONE: IDs that used to yield distinct provider-flavored
	// Model__ constants now CONVERGE onto one Entity__ constant per identity, and a name
	// collision is a loud bake error (never a silent _N ordinal).
	refStr := string(refConstants)
	// refNorm is the whitespace-normalized version for substring matching.
	refNorm := normalizeWhitespace(refStr)

	// C group (convergence): both cloudflare-ai-gateway spellings — "anthropic/claude-3-5-haiku"
	// and "anthropic/claude-3.5-haiku" — decompose to the SAME entity claude/haiku@3.5, so they
	// collapse to ONE constant (no _1/_2 collision ordinal, no provider flavoring).
	if !strings.Contains(refNorm, `Entity__Claude__Haiku__Version_3_5 = "claude/haiku@3.5"`) {
		t.Errorf("reference output: C group convergence pin missing; want the single claude/haiku@3.5 entity\nconstants:\n%s", refStr)
	}
	if strings.Contains(refNorm, "CloudflareAIGateway") || strings.Contains(refNorm, "Version_3_5_1") || strings.Contains(refNorm, "Version_3_5_2") {
		t.Errorf("reference output: C group must not carry a provider-flavored or _N-ordinal constant\nconstants:\n%s", refStr)
	}
	// B group (convergence): "kilo-auto/free" and "openrouter/free" both decompose to the entity
	// "free" — one constant, no backend-route disambiguator (that was a Model__-surface concern).
	if !strings.Contains(refNorm, `Entity__Free = "free"`) {
		t.Errorf("reference output: B group convergence pin missing; want the single free entity\nconstants:\n%s", refStr)
	}
	// E control: distinct versions stay distinct entities. Exact constant names.
	if !strings.Contains(refNorm, `Entity__Gpt__Version_5_1 = "gpt@5.1"`) {
		t.Errorf("reference output: E control Entity__Gpt__Version_5_1 missing or wrong\nconstants:\n%s", refStr)
	}
	if !strings.Contains(refNorm, `Entity__Gpt__Version_5_2 = "gpt@5.2"`) {
		t.Errorf("reference output: E control Entity__Gpt__Version_5_2 missing or wrong\nconstants:\n%s", refStr)
	}
	// E control: assert NO doubled-ordinal fragment (e.g. Entity__Gpt__Version_5_1_1). Distinct
	// versions render distinctly by the sentinel grammar, so the injectivity guard never fires
	// and no _N suffix is ever appended.
	if strings.Contains(refNorm, "Entity__Gpt__Version_5_1_") || strings.Contains(refNorm, "Entity__Gpt__Version_5_2_") {
		t.Errorf("reference output: E control has a doubled-ordinal fragment (should never happen)\nconstants:\n%s", refStr)
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

	// Build a per-constantName → entity-key index from the reference for stability
	// assertion. Parse const-declaration lines of the form: \t<ConstName> = "<entity-key>"
	// Use normalizeWhitespace per-line so the " = " split works despite gofmt alignment.
	// (The allEntityConstants array lines end in "," with no " = " and are skipped.)
	refConstToKey := make(map[string]string)
	for _, line := range strings.Split(refStr, "\n") {
		norm := normalizeWhitespace(line)
		if !strings.HasPrefix(norm, "Entity__") {
			continue
		}
		parts := strings.SplitN(norm, " = ", 2)
		if len(parts) != 2 {
			continue
		}
		constName := strings.TrimSpace(parts[0])
		key := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if constName != "" && key != "" {
			refConstToKey[constName] = key
		}
	}

	// Run N-1 more iterations and assert FULL byte-identity (NO normalization). The stamp
	// is a constant here, so ANY diff between iterations — including a LastSynced line —
	// is a real non-determinism regression in the emission (a generator consulting a clock,
	// map-order leakage, or an unstable collision ordinal). run()'s stamp SOURCE is outside
	// this test's reach (see the fencing-boundary note above).
	for i := 1; i < N; i++ {
		iterArtifacts := runFixtureCodegenArtifacts(t, fixtureJSON, ts)
		staticSrc, constantsSrc, metadataSrc := iterArtifacts.staticSrc, iterArtifacts.constantsSrc, iterArtifacts.metadataSrc

		if !bytes.Equal(refStatic, staticSrc) {
			t.Fatalf("iteration %d: generateSource output is not byte-identical to the reference\n"+
				"  What: the static model list changed between runs\n"+
				"  Why: nondeterminism in the fetchModelsWithRaw map-range, the ordering sort, or a wall-clock in a baked field\n"+
				"  Where: fetchModelsWithRaw or generateSource\n"+
				"  How to fix: ensure the model slice is sorted before return and no baked field uses time.Now()",
				i+1)
		}
		if !bytes.Equal(refConstants, constantsSrc) {
			t.Fatalf("iteration %d: generateEntitiesConstantsSource output is not byte-identical to the reference\n"+
				"  What: the entity-constants file changed between runs\n"+
				"  Why: entity grouping or the ascending-name sort is order-dependent (map-iteration leakage)\n"+
				"  Where: generateEntitiesConstantsSource / buildEntityConstEntries\n"+
				"  How to fix: ensure the const entries are sorted by name before emitting (they are — investigate any new map range)",
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

		// The committed non-.go emissions. Both are built from map ranges (the
		// creator→provider served set; the family→lab evidence accumulation), so an
		// omitted sort in output position shows up here and NOWHERE else: neither
		// TestCodegen_Reproducible_ByteIdentical's .go comparisons nor
		// TestCodegen_UpToDate's golden excerpts can reach a JSON report.
		if !bytes.Equal(refArtifacts.creatorProvidersUnserved, iterArtifacts.creatorProvidersUnserved) {
			t.Fatalf("iteration %d: buildCreatorProvidersUnserved output is not byte-identical to the reference\n"+
				"  What: the curated creator→provider coverage report changed between runs\n"+
				"  Why: the report consulted a wall clock, or emitted rows in map-iteration order\n"+
				"  Where: buildCreatorProvidersUnserved\n"+
				"  How to fix: keep the explicit sort.Slice on (creator, provider) and emit no timestamp",
				i+1)
		}
		if !bytes.Equal(refArtifacts.creatorsLabDisagreements, iterArtifacts.creatorsLabDisagreements) {
			t.Fatalf("iteration %d: buildCreatorsLabDisagreements output is not byte-identical to the reference\n"+
				"  What: the models.dev lab-derivation disagreement report changed between runs\n"+
				"  Why: the derivation emitted families in map-iteration order, or a row's labs list was left unsorted\n"+
				"  Where: bestiary.DeriveCreatorLabDisagreements / buildCreatorsLabDisagreements\n"+
				"  How to fix: keep the sort.Slice on family and the sort.Strings on each row's labs",
				i+1)
		}
		if !bytes.Equal(refArtifacts.modelsdevFieldCensus, iterArtifacts.modelsdevFieldCensus) {
			t.Fatalf("iteration %d: buildModelsdevFieldCensus output is not byte-identical to the reference\n"+
				"  What: the upstream field-shape census changed between runs over identical input\n"+
				"  Why: the census walks JSON objects, whose key order Go randomizes on every decode,\n"+
				"       so an omitted sort in output position surfaces HERE and nowhere else\n"+
				"  Where: buildModelsdevFieldCensus\n"+
				"  How to fix: keep the explicit sort.Slice on Path and emit no timestamp",
				i+1)
		}

		// Verify constant-name → entity-key stability.
		iterStr := string(constantsSrc)
		for _, line := range strings.Split(iterStr, "\n") {
			norm := normalizeWhitespace(line)
			if !strings.HasPrefix(norm, "Entity__") {
				continue
			}
			parts := strings.SplitN(norm, " = ", 2)
			if len(parts) != 2 {
				continue
			}
			constName := strings.TrimSpace(parts[0])
			key := strings.Trim(strings.TrimSpace(parts[1]), `"`)
			if constName == "" || key == "" {
				continue
			}
			if prev, ok := refConstToKey[constName]; ok && prev != key {
				t.Errorf("iteration %d: constant %q mapped to key %q in iteration but %q in reference\n"+
					"  What: an entity constant's value changed between runs\n"+
					"  Why: entity grouping is not stable across map-iteration order\n"+
					"  How to fix: verify buildEntitySet/MintNomina impose a deterministic order",
					i+1, constName, key, prev)
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
		info, _, _ := enrichModelInfo(base)
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

	// Regenerate from the fixture. The codegen LastSynced stamp is deterministic (pinned
	// to the committed models.dev ingest instant, not a wall-clock), so this guard no
	// longer injects a timestamp and normalizes it out: the golden excerpts carry
	// LastSynced: "" and the fixture path (lastSynced "") emits exactly that, so the
	// comparison is an EXACT content match with no LastSynced masking.
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
		t.Errorf("up-to-date guard: entity-constants file does not contain golden excerpt\n"+
			"  What: generateEntitiesConstantsSource output differs from testdata/expected_constants_excerpt.go.golden\n"+
			"  Why: the entity set or the Entity__ grammar changed, or codegen logic was modified without re-running regen\n"+
			"  Where: cmd/bestiary-gen entities_constants.go generateEntitiesConstantsSource\n"+
			"  How to fix: run `go run ./cmd/bestiary-gen --no-fetch && git add entities_constants_gen.go models_static_gen.go`\n"+
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
	if !strings.Contains(string(constantsGoldenRaw), `= "claude/haiku@3.5"`) {
		t.Errorf("up-to-date guard: constants golden file appears empty or truncated (missing expected binding)\n" +
			"  How to fix: ensure testdata/expected_constants_excerpt.go.golden is correctly committed")
	}
}

// TestCodegen_GoldenPins_C verifies the C group under the entity-constants hard cut: the
// two cloudflare-ai-gateway spellings "anthropic/claude-3-5-haiku" and
// "anthropic/claude-3.5-haiku" both decompose to the SAME entity claude/haiku@3.5, so the
// provider-flavored/_N-collision Model__ pair is gone — they CONVERGE onto ONE Entity__
// constant. (The old _1/_2 ordinal was a provider-leak artifact the cut eliminates.)
func TestCodegen_GoldenPins_C(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Entity__Claude__Haiku__Version_3_5 = "claude/haiku@3.5"`) {
		t.Errorf("C group convergence: expected the single claude/haiku@3.5 entity\nconstants:\n%s", string(constantsSrc))
	}
	if strings.Contains(s, "CloudflareAIGateway") || strings.Contains(s, "Version_3_5_1") || strings.Contains(s, "Version_3_5_2") {
		t.Errorf("C group: no provider-flavored or _N-ordinal constant may survive the hard cut\nconstants:\n%s", string(constantsSrc))
	}
}

// TestCodegen_GoldenPins_B verifies the B group under the hard cut: "kilo-auto/free" and
// "openrouter/free" both decompose to the entity "free", so — like the C group — they
// CONVERGE onto ONE Entity__ constant. The backend-route disambiguator that the old
// Model__ surface needed is no longer relevant (provider/route is queried via the API).
func TestCodegen_GoldenPins_B(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Entity__Free = "free"`) {
		t.Errorf("B group convergence: expected the single free entity\nconstants:\n%s", string(constantsSrc))
	}
	if strings.Contains(s, "KiloAuto") || strings.Contains(s, "__OpenRouter") {
		t.Errorf("B group: no backend-route-flavored constant may survive the hard cut\nconstants:\n%s", string(constantsSrc))
	}
}

// TestCodegen_GoldenPins_E verifies the E group (version-pair negative control) under the
// hard cut: "gpt-5.1" → Entity__Gpt__Version_5_1, "gpt-5.2" → Entity__Gpt__Version_5_2.
// Distinct versions render distinctly via the __Version_ word sentinel, so the injectivity
// guard never fires — asserts exact names present and NO doubled-ordinal fragment.
func TestCodegen_GoldenPins_E(t *testing.T) {
	fixtureJSON := deterministicFixtureJSON(t)
	_, constantsSrc, _ := runFixtureCodegen(t, fixtureJSON, "")
	s := normalizeWhitespace(string(constantsSrc))

	if !strings.Contains(s, `Entity__Gpt__Version_5_1 = "gpt@5.1"`) {
		t.Errorf("E control: Entity__Gpt__Version_5_1 missing or wrong value\nconstants:\n%s", string(constantsSrc))
	}
	if !strings.Contains(s, `Entity__Gpt__Version_5_2 = "gpt@5.2"`) {
		t.Errorf("E control: Entity__Gpt__Version_5_2 missing or wrong value\nconstants:\n%s", string(constantsSrc))
	}
	rawStr := string(constantsSrc)
	if strings.Contains(rawStr, "Entity__Gpt__Version_5_1_") {
		t.Errorf("E control: Entity__Gpt__Version_5_1 has an unexpected suffix (doubled ordinal or fragment)\nconstants:\n%s", rawStr)
	}
	if strings.Contains(rawStr, "Entity__Gpt__Version_5_2_") {
		t.Errorf("E control: Entity__Gpt__Version_5_2 has an unexpected suffix (doubled ordinal or fragment)\nconstants:\n%s", rawStr)
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
// VRAMBytes = weights + KV at context 131072 with partial=false on each quant
// that carries the curated architecture facts.
//
// The quant SET is fetch-owned — the offline refresh writes every quant the
// registry publishes — while the architecture facts are curation-owned and exist
// for three of them. So the anchor asserts by quant, not by position: the three
// arch-curated quants carry hand-computable VRAM, and every other row is a
// measured weight with no arch facts and therefore a partial estimate.
//
// Expected values (hand-computed):
//
//	KV = 2 * 80 * 8 * 128 * 131072 * 2 = 42,949,672,960 bytes
//	q4_k_m: VRAMBytes = 42,520,398,528 + 42,949,672,960 = 85,470,071,488
//	q8_0:   VRAMBytes = 74,975,054,528 + 42,949,672,960 = 117,924,727,488
//	f16:    VRAMBytes = 141,117,917,888 + 42,949,672,960 = 184,067,590,848
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

	want := map[bestiary.Quantization]wantRow{
		bestiary.QuantQ4_K_M: {
			quant:         bestiary.QuantQ4_K_M,
			weightsBytes:  42_520_398_528,
			vramBytes:     85_470_071_488,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
		bestiary.QuantQ8_0: {
			quant:         bestiary.QuantQ8_0,
			weightsBytes:  74_975_054_528,
			vramBytes:     117_924_727_488,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
		bestiary.QuantF16: {
			quant:         bestiary.QuantF16,
			weightsBytes:  141_117_917_888,
			vramBytes:     184_067_590_848,
			vramCtxTokens: bakeCtx,
			partial:       false,
		},
	}

	// Obtain the raw (unbaked) rows from the curated table.
	rawRows := bestiary.QuantVRAMFor(modelID)
	if rawRows == nil {
		t.Fatalf("QuantVRAMFor(%q) returned nil; expected curated rows from quant_vram.json", modelID)
	}
	const wantRows = 12 // the registry's published quant set for this model
	if len(rawRows) != wantRows {
		t.Fatalf("QuantVRAMFor(%q): got %d rows, want %d", modelID, len(rawRows), wantRows)
	}

	// Bake each row using EstimateVRAMBytes at the curated bake context.
	seen := 0
	for i, row := range rawRows {
		baked := row
		baked.VRAMBytes = bestiary.EstimateVRAMBytes(row.WeightsBytes, bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
		baked.VRAMContextTokens = bakeCtx
		baked.VRAMEstimatePartial = bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)

		w, curated := want[baked.Quant]
		if !curated {
			// A measured quant with no curated arch facts: weights-only, partial.
			if baked.VRAMBytes != baked.WeightsBytes || !baked.VRAMEstimatePartial {
				t.Errorf("row %d (%v): arch-absent row baked VRAMBytes=%d partial=%v, want %d and true",
					i, baked.Quant, baked.VRAMBytes, baked.VRAMEstimatePartial, baked.WeightsBytes)
			}
			continue
		}
		seen++
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
	if seen != len(want) {
		t.Errorf("matched %d arch-curated quants, want %d; the curated arch facts must survive every refresh", seen, len(want))
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
// llama-3.2-3b-instruct: every quant the registry publishes, arch facts absent
// on all of them (the quant set is fetch-owned, the arch facts curation-owned).
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
		if len(rows) != 15 {
			t.Fatalf("QuantVRAMFor(%q): got %d rows, want 15", modelID, len(rows))
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
		// llama-3.3-70b-instruct carries curated arch facts on the three quants that
		// were curated by hand; the refresh added measured weights for the rest of the
		// registry's quant set, which have none. Both classes must be present, and each
		// must land on its own side of the predicate — a corpus where one class vanished
		// would pass a one-sided assertion vacuously.
		rows := bestiary.QuantVRAMFor("llama-3.3-70b-instruct")
		if rows == nil {
			t.Fatal("QuantVRAMFor(llama-3.3-70b-instruct) returned nil; need curated rows")
		}
		archComplete, archAbsent := 0, 0
		for i, row := range rows {
			partial := bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)
			if row.Layers != 0 && row.KVHeads != 0 && row.HeadDim != 0 {
				archComplete++
				if partial {
					t.Errorf("row %d: VRAMEstimatePartial=true with arch facts present (layers=%d kvHeads=%d headDim=%d); want false",
						i, row.Layers, row.KVHeads, row.HeadDim)
				}
				continue
			}
			archAbsent++
			if !partial {
				t.Errorf("row %d: VRAMEstimatePartial=false with arch facts absent (layers=%d kvHeads=%d headDim=%d); want true",
					i, row.Layers, row.KVHeads, row.HeadDim)
			}
		}
		if archComplete != 3 {
			t.Errorf("arch-complete rows = %d, want 3 (the curated q4_k_m/q8_0/f16 facts)", archComplete)
		}
		if archAbsent != 9 {
			t.Errorf("arch-absent rows = %d, want 9 (the refresh's measured quants)", archAbsent)
		}
	})
}

// The presence-based param-size precedence (pin > mechanical > ParamSizeFor) that the
// codegen bake shares with the runtime enrichment joints is pinned by
// TestCodegen_ParamSizePrecedence in the library package (paramsize_wiring_internal_test.go),
// where it can reach both the exported EnrichedParamSize and the unexported precedence
// helper for the token-less-fallback and disagreement legs no catalog ID reaches. It
// replaces the pre-v0.2.6 curated-only ParamSize emission guard, which the full-bulk
// re-key retired.

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

	// parseBaseRef must decompose a COMPOUND base_ref whose size is a multi-part MoE
	// shape token into the FULL parent EntityRef, capturing the WHOLE size window
	// (family + version + #size(compound) + identity modifier). This is the payoff of
	// delegating to ExtractParamSizeToken: the pre-delegation greedy per-token scan
	// would clip "235b-a22b" to "235b" (and drop "a22b" as a stray modifier), pointing
	// the finetune edge at the wrong parent entity.
	t.Run("parseBaseRef_compound_moe_size", func(t *testing.T) {
		cases := []struct {
			baseRef, want, wantSize string
		}{
			{"qwen3:235b-a22b-instruct", "qwen@3#235b-a22b{instruct}", "235b-a22b"}, // active MoE — whole window, not "235b"
			{"mixtral:8x22b-instruct", "mixtral#8x22b{instruct}", "8x22b"},          // NxM MoE shape
			{"llama4:17b-16e-instruct", "llama@4#17b-16e{instruct}", "17b-16e"},     // count-suffixed MoE
		}
		for _, tc := range cases {
			ref := parseBaseRef(tc.baseRef)
			if got := ref.String(); got != tc.want {
				t.Errorf("parseBaseRef(%q).String() = %q, want %q\n"+
					"  What: a compound MoE size window was clipped or a modifier was lost\n"+
					"  Where: parseBaseRef ExtractParamSizeToken delegation + modifier remainder scan\n"+
					"  How to fix: the whole size window must be captured and the remaining tokens treated as modifiers",
					tc.baseRef, got, tc.want)
			}
			if ref.ParamSize != tc.wantSize {
				t.Errorf("parseBaseRef(%q).ParamSize = %q, want %q (whole-window size, not a clipped prefix)", tc.baseRef, ref.ParamSize, tc.wantSize)
			}
			if len(ref.Modifier) != 1 || ref.Modifier[0] != "instruct" {
				t.Errorf("parseBaseRef(%q).Modifier = %v, want [instruct] (remainder after excising the size window)", tc.baseRef, ref.Modifier)
			}
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
	// The partial flag must render both ways, tracking each row's arch facts: the
	// three curated quants are complete (false), the refresh's measured quants have
	// no curated arch facts (true). A literal carrying only one of the two would mean
	// the flag stopped tracking the row it describes.
	if !strings.Contains(lit1, "VRAMEstimatePartial: false") {
		t.Errorf("quantVRAMLiteral: no VRAMEstimatePartial: false row; the arch-curated quants must bake complete\nliteral: %s", lit1)
	}
	if !strings.Contains(lit1, "VRAMEstimatePartial: true") {
		t.Errorf("quantVRAMLiteral: no VRAMEstimatePartial: true row; the arch-absent measured quants must bake partial\nliteral: %s", lit1)
	}
}

// TestCodegen_QuantVRAMLiteral_OCIDigest exercises the conditional OCIDigest-emission
// branch in quantVRAMLiteral directly, with a digest-bearing fixture. quantVRAMLiteral
// emits the OCIDigest field only when r.OCIDigest != "" (the empty-digest majority stays
// byte-identical to the pre-OCIDigest bake); this test is the fixture that actually
// exercises the true arm of that condition, rather than relying on baked curated data
// (which today never carries a digest).
func TestCodegen_QuantVRAMLiteral_OCIDigest(t *testing.T) {
	withDigest := []bestiary.QuantVRAM{
		{
			Quant:        bestiary.QuantQ4_K_M,
			QuantRaw:     "Q4_K_M",
			WeightsBytes: 4_000_000_000,
			VRAMBytes:    4_500_000_000,
			OCIDigest:    "sha256:abc123def456",
		},
	}
	withoutDigest := []bestiary.QuantVRAM{
		{
			Quant:        bestiary.QuantQ4_K_M,
			QuantRaw:     "Q4_K_M",
			WeightsBytes: 4_000_000_000,
			VRAMBytes:    4_500_000_000,
			OCIDigest:    "",
		},
	}

	// (a) a digest-bearing fixture renders the OCIDigest field, correctly quoted.
	litWith := quantVRAMLiteral(withDigest)
	if !strings.Contains(litWith, `OCIDigest: "sha256:abc123def456"`) {
		t.Errorf("quantVRAMLiteral: digest-bearing fixture missing rendered OCIDigest field\n"+
			"  what: expected literal to contain OCIDigest: %q\n"+
			"  where: quantVRAMLiteral (cmd/bestiary-gen/main.go)\n"+
			"  literal: %s", "sha256:abc123def456", litWith)
	}

	// (b) a digest-less fixture omits the OCIDigest field entirely.
	litWithout := quantVRAMLiteral(withoutDigest)
	if strings.Contains(litWithout, "OCIDigest") {
		t.Errorf("quantVRAMLiteral: digest-less fixture unexpectedly emitted an OCIDigest field\n"+
			"  what: OCIDigest must be omitted when r.OCIDigest == \"\"\n"+
			"  where: quantVRAMLiteral (cmd/bestiary-gen/main.go)\n"+
			"  literal: %s", litWithout)
	}

	// (c) emission is deterministic across two runs on the same digest-bearing input
	// (INV3 — byte-identical regen).
	litWithAgain := quantVRAMLiteral(withDigest)
	if litWith != litWithAgain {
		t.Errorf("quantVRAMLiteral: OCIDigest-bearing emission is non-deterministic across repeated calls\n"+
			"  what: two calls with the identical digest-bearing input produced different output\n"+
			"  why: violates INV3 (codegen output must be byte-identical across runs)\n"+
			"  where: quantVRAMLiteral (cmd/bestiary-gen/main.go)\n"+
			"  how to fix: ensure quantVRAMLiteral does not use map iteration or time.Now()\n"+
			"  got:  %s\n  want: %s", litWithAgain, litWith)
	}
}

// TestCodegen_UpToDate_RealInput is the real-input regen up-to-date guard. It
// regenerates every codegen-owned source IN MEMORY from the COMMITTED vendored
// snapshot (parse/data/modelsdev/catalog.json) via the exact generation sequence
// run() uses — INCLUDING the deterministic LastSynced stamp — then byte-compares each
// fresh source against the COMMITTED *_gen.go file on disk with NO masking. Because the
// baked LastSynced is now deterministic (the committed models.dev ingest instant, not a
// wall-clock), the comparison is exact; a wall-clock leaking back into the stamp would
// fail here. It FAILS if any generated file is stale relative to the vendored snapshot
// plus the current emitter logic.
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
	//   stamp the deterministic LastSynced -> allSlugs (sorted) -> collectFamilies ->
	//   applyFilter(no flags) -> the five generate* emitters, with slugToConst built from
	//   providerConstName.
	//
	// LastSynced IS stamped here with the same deterministic codegenLastSynced value run()
	// uses (the committed models.dev ingest instant, NOT a wall-clock), so the comparison
	// below is an EXACT byte match with NO LastSynced masking. This permanently fences
	// run()'s stamp source: if a wall-clock ever leaks back into the baked LastSynced, the
	// fresh regen's deterministic stamp would diverge from the committed files and fail
	// here — which masking would have hidden.
	lastSynced, err := codegenLastSynced()
	if err != nil {
		t.Fatalf("codegenLastSynced: %v", err)
	}
	for i := range models {
		models[i].LastSynced = lastSynced
	}

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
	constantsSrc, err := generateEntitiesConstantsSource(models, metadata)
	if err != nil {
		t.Fatalf("generateEntitiesConstantsSource: %v", err)
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
		{outputEntitiesConstantsPath, constantsSrc},
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

		if bytes.Equal(f.src, committed) {
			continue
		}

		// Report the first divergent line to make the staleness reviewable without
		// dumping the whole (multi-MB) file.
		freshLines := strings.Split(string(f.src), "\n")
		commLines := strings.Split(string(committed), "\n")
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
				"  What: a fresh --no-fetch regen of this file (deterministic LastSynced, no masking) does not match what is committed\n"+
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
