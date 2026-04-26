// bestiary-gen fetches the models.dev API and writes four generated files
// into the bestiary package root:
//   - models_static_gen.go    — all ~4168 model records
//   - providers_gen.go        — one Provider constant per API slug + knownProviders
//   - families_gen.go         — one Family constant per unique API family value
//   - models_constants_gen.go — one Model_* constant per eligible (ID, Provider) pair
//
// Run via: go generate ./...
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/dayvidpham/bestiary"
)

// casingOverrides maps lowercase token → preferred uppercase form.
// Applied when blending the API name field does not yield a recognisable token.
var casingOverrides = map[string]string{
	"ai":      "AI",
	"api":     "API",
	"chatgpt": "ChatGPT",
	"gpt":     "GPT",
	"llm":     "LLM",
	"io":      "IO",
	"sap":     "SAP",
	"ovh":     "OVH",
	"cn":      "CN",
	"ams":     "AMS",
	"sgp":     "SGP",
	"xai":     "XAI",
	"aws":     "AWS",
}

// slugToIdentifier converts a provider/family slug (e.g. "amazon-bedrock", "xai",
// "302ai") into a Go PascalCase identifier suffix (e.g. "AmazonBedrock", "XAI",
// "302AI").
//
// Algorithm:
//  1. Split on hyphens and dots to get tokens (dots appear in some family names).
//  2. For each token, check casingOverrides first.
//  3. If not in overrides and the token starts with a digit, keep the digit part
//     verbatim and apply overrides to the trailing alpha part.
//  4. Otherwise use the API display name as a casing hint, falling back to title-case.
func slugToIdentifier(slug string, nameHint string) string {
	if slug == "" {
		return ""
	}
	// Split on both hyphens and dots; dots appear in family slugs like "gpt-4.5".
	tokens := strings.FieldsFunc(slug, func(r rune) bool {
		return r == '-' || r == '.'
	})

	// Build a lookup from lowercase name-hint words for casing hints.
	nameHintWords := make(map[string]string) // lowercase → display form
	for _, w := range strings.Fields(nameHint) {
		lower := strings.ToLower(w)
		nameHintWords[lower] = w
	}

	var sb strings.Builder
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		lower := strings.ToLower(tok)

		// 1. Check casing overrides.
		if override, ok := casingOverrides[lower]; ok {
			sb.WriteString(override)
			continue
		}

		// 2. If the token starts with a digit, keep the digit part verbatim.
		// Apply casing overrides to any alpha suffix.
		// e.g. "302ai" → "302" + "ai" → "302" + "AI"
		if unicode.IsDigit(rune(tok[0])) {
			digitPart := ""
			alphaPart := ""
			for i, r := range tok {
				if !unicode.IsDigit(r) {
					digitPart = tok[:i]
					alphaPart = tok[i:]
					break
				}
			}
			if alphaPart == "" {
				digitPart = tok
			}
			sb.WriteString(digitPart)
			if alphaPart != "" {
				alphaLower := strings.ToLower(alphaPart)
				if override, ok := casingOverrides[alphaLower]; ok {
					sb.WriteString(override)
				} else {
					sb.WriteString(strings.ToUpper(alphaPart[:1]) + alphaPart[1:])
				}
			}
			continue
		}

		// 3. Check name hint map for a display-form casing hint.
		if hint, ok := nameHintWords[lower]; ok {
			hintLower := strings.ToLower(hint)
			if override, ok2 := casingOverrides[hintLower]; ok2 {
				sb.WriteString(override)
			} else {
				sb.WriteString(strings.ToUpper(hint[:1]) + hint[1:])
			}
			continue
		}

		// 4. Default: title-case the token.
		sb.WriteString(strings.ToUpper(lower[:1]) + lower[1:])
	}
	return sb.String()
}

// providerConstName returns the Go identifier for a Provider constant given its slug.
// Examples: "anthropic" → "ProviderAnthropic", "302ai" → "Provider302AI",
// "xai" → "ProviderXAI", "amazon-bedrock" → "ProviderAmazonBedrock".
func providerConstName(slug string, nameHint string) string {
	return "Provider" + slugToIdentifier(slug, nameHint)
}

// familyConstName returns the Go identifier for a Family constant given its raw value.
// Examples: "claude-opus" → "FamilyClaudeOpus", "gpt-4o" → "FamilyGPT4o".
func familyConstName(slug string, nameHint string) string {
	return "Family" + slugToIdentifier(slug, nameHint)
}

// Output paths are relative to the module root (where go generate is run from).
const (
	outputPath          = "models_static_gen.go"
	outputProvidersPath = "providers_gen.go"
	outputFamiliesPath  = "families_gen.go"
	defaultCacheDir     = ".bestiary-gen-cache"
	cacheFile           = "api_response.json"
)

// apiURL is the endpoint bestiary-gen fetches from. Declared as a var (not const)
// so tests can override it to point at an httptest.Server without build tags or
// dual code paths.
var apiURL = "https://models.dev/api.json"

// --------------------------------------------------------------------------
// Private wire types for JSON deserialization from models.dev
// These are defined here (not in the main package) so the codegen tool is
// self-contained and can handle API schema evolution without breaking the
// library's wire.go.
// --------------------------------------------------------------------------

type genWireResponse map[string]genWireProvider

type genWireProvider struct {
	Name   string                  `json:"name"`
	Models map[string]genWireModel `json:"models"`
}

// genWireModel mirrors the models.dev model object.
// interleaved is stored as json.RawMessage because the field is polymorphic:
// some providers use bool (true) and others use an object ({field: "..."}).
// Since we only care about our three target providers (anthropic, google, openai)
// which do not use this field, we just skip it.
type genWireModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Family           string            `json:"family"`
	Reasoning        bool              `json:"reasoning"`
	ToolCall         bool              `json:"tool_call"`
	Attachment       bool              `json:"attachment"`
	Temperature      bool              `json:"temperature"`
	StructuredOutput bool              `json:"structured_output"`
	Interleaved      json.RawMessage   `json:"interleaved"`
	OpenWeights      bool              `json:"open_weights"`
	ReleaseDate      string            `json:"release_date"`
	Knowledge        string            `json:"knowledge"`
	Cost             *genWireCost      `json:"cost"`
	Limit            *genWireLimit     `json:"limit"`
	Modalities       *genWireModality  `json:"modalities"`
}

type genWireCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	Reasoning  *float64 `json:"reasoning"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type genWireLimit struct {
	Context *int `json:"context"`
	Output  *int `json:"output"`
}

type genWireModality struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: %v\n", err)
		os.Exit(1)
	}
}

// flagResult holds all parsed CLI flags.
type flagResult struct {
	only     []string // -only-providers: inclusion list (empty = all)
	except   []string // -all-providers-except: exclusion list (empty = none)
	cacheDir string   // -cache-dir: override default cache directory
	noFetch  bool     // -no-fetch: skip HTTP, load from cache
}

// parseFlags parses os.Args[1:] (or a provided slice) for all supported flags.
// Both single-hyphen (-flag) and double-hyphen (--flag) forms are accepted for
// all flags. Returns an error if both -only-providers and -all-providers-except
// are specified simultaneously (mutually exclusive).
func parseFlags(args []string) (flagResult, error) {
	var res flagResult
	res.cacheDir = defaultCacheDir // default: backward-compatible

	// normalizeFlag strips a leading double-hyphen to a single hyphen so that
	// "--flag" is treated identically to "-flag" throughout the switch below.
	normalizeFlag := func(s string) string {
		if strings.HasPrefix(s, "--") {
			return s[1:] // "--foo" → "-foo"
		}
		return s
	}

	for i := 0; i < len(args); i++ {
		arg := normalizeFlag(args[i])
		var val string
		switch {
		case strings.HasPrefix(arg, "-only-providers="):
			val = strings.TrimPrefix(arg, "-only-providers=")
			res.only = splitComma(val)
		case arg == "-only-providers" && i+1 < len(args):
			i++
			res.only = splitComma(args[i])
		case strings.HasPrefix(arg, "-all-providers-except="):
			val = strings.TrimPrefix(arg, "-all-providers-except=")
			res.except = splitComma(val)
		case arg == "-all-providers-except" && i+1 < len(args):
			i++
			res.except = splitComma(args[i])
		case strings.HasPrefix(arg, "-cache-dir="):
			res.cacheDir = strings.TrimPrefix(arg, "-cache-dir=")
		case arg == "-cache-dir" && i+1 < len(args):
			i++
			res.cacheDir = args[i]
		case arg == "-no-fetch":
			res.noFetch = true
		}
	}
	if len(res.only) > 0 && len(res.except) > 0 {
		return flagResult{}, fmt.Errorf(
			"flags -only-providers and -all-providers-except are mutually exclusive\n" +
				"  What: both inclusion and exclusion filters were specified\n" +
				"  Why: these flags represent opposite filtering strategies and cannot be combined\n" +
				"  Where: bestiary-gen flag parsing\n" +
				"  How to fix: use either -only-providers=<slugs> OR -all-providers-except=<slugs>, not both",
		)
	}
	if res.cacheDir == "" {
		return flagResult{}, fmt.Errorf(
			"-cache-dir value must not be empty\n" +
				"  What: -cache-dir was explicitly set to an empty string\n" +
				"  Why: an empty cache dir resolves to the current working directory, which is unintended\n" +
				"  Where: bestiary-gen flag parsing\n" +
				"  How to fix: omit -cache-dir to use the default (%s), or provide a non-empty path",
			defaultCacheDir,
		)
	}
	return res, nil
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// applyFilter returns only the models that pass the inclusion/exclusion filter.
// Constants are always generated for ALL providers; this filter only affects model data.
func applyFilter(models []bestiary.ModelInfo, only, except []string) []bestiary.ModelInfo {
	if len(only) == 0 && len(except) == 0 {
		return models
	}
	onlySet := make(map[string]struct{}, len(only))
	for _, p := range only {
		onlySet[p] = struct{}{}
	}
	exceptSet := make(map[string]struct{}, len(except))
	for _, p := range except {
		exceptSet[p] = struct{}{}
	}

	var out []bestiary.ModelInfo
	for _, m := range models {
		slug := string(m.Provider)
		if len(onlySet) > 0 {
			if _, ok := onlySet[slug]; !ok {
				continue
			}
		}
		if len(exceptSet) > 0 {
			if _, ok := exceptSet[slug]; ok {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

func run(args []string) error {
	flags, err := parseFlags(args)
	if err != nil {
		return err
	}

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	rawJSON, models, providerMeta, err := fetchModelsWithRaw(ctx, flags.cacheDir, flags.noFetch)
	if err != nil {
		return err
	}

	// Cache the raw API JSON for offline analysis (only when we actually fetched).
	if !flags.noFetch {
		if cacheErr := cacheAPIResponse(rawJSON, flags.cacheDir); cacheErr != nil {
			// Non-fatal: log and continue.
			fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not cache API response: %v\n", cacheErr)
		}
	}

	// Stamp LastSynced on all models.
	for i := range models {
		models[i].LastSynced = now
	}

	// Collect all unique provider slugs from the API (for constant generation).
	allSlugs := make([]string, 0, len(providerMeta))
	for slug := range providerMeta {
		allSlugs = append(allSlugs, slug)
	}
	sort.Strings(allSlugs)

	// Collect all unique family values from all models (before data filter).
	familyMeta := collectFamilies(models, providerMeta)

	// Apply model data filter (constants are always generated for all providers).
	filtered := applyFilter(models, flags.only, flags.except)

	if len(filtered) == 0 && len(flags.only) > 0 {
		return fmt.Errorf(
			"no models found after applying -only-providers filter %v from %d total models\n"+
				"  What: the inclusion filter matched no models\n"+
				"  Why: the specified provider slugs may be incorrect or absent from the API\n"+
				"  Where: bestiary-gen model filter\n"+
				"  How to fix: check slug spelling against the API at %s or remove the filter",
			flags.only, len(models), apiURL,
		)
	}

	// Generate providers_gen.go (all provider constants, regardless of filter).
	providersSrc, err := generateProvidersSource(allSlugs, providerMeta)
	if err != nil {
		return fmt.Errorf("generate providers source: %w", err)
	}
	if err := writeFile(outputProvidersPath, providersSrc); err != nil {
		return err
	}

	// Generate families_gen.go.
	familiesSrc, err := generateFamiliesSource(familyMeta)
	if err != nil {
		return fmt.Errorf("generate families source: %w", err)
	}
	if err := writeFile(outputFamiliesPath, familiesSrc); err != nil {
		return err
	}

	// Generate models_static_gen.go (uses slug→const map for providerExpr).
	// Build slug→constName map for all providers.
	slugToConst := make(map[string]string, len(allSlugs))
	for _, slug := range allSlugs {
		meta := providerMeta[slug]
		slugToConst[slug] = providerConstName(slug, meta.Name)
	}

	src, err := generateSource(filtered, slugToConst)
	if err != nil {
		return fmt.Errorf("generate Go source: %w", err)
	}
	if err := writeFile(outputPath, src); err != nil {
		return err
	}

	// Post-condition: verify families_gen.go contains a named Family type (not an alias).
	// This guards against a regression where the codegen template accidentally emits
	// "type Family = string" (alias) instead of "type Family string" (named type).
	if err := validateGeneratedFamilyType(outputFamiliesPath); err != nil {
		return err
	}

	// Generate models_constants_gen.go — Model_* constants for all eligible models.
	// Uses the full (unfiltered) model set so that constants cover all providers.
	constantsSrc, err := generateConstantsSource(models, slugToConst)
	if err != nil {
		return fmt.Errorf("generate constants source: %w", err)
	}
	if err := writeFile(outputConstantsPath, constantsSrc); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout,
		"bestiary-gen: wrote %s with %d models (%d providers), %s with %d constants, %s with %d constants, %s at %s\n",
		outputPath, len(filtered), countUniqueProviders(filtered),
		outputProvidersPath, len(allSlugs),
		outputFamiliesPath, len(familyMeta),
		outputConstantsPath,
		now,
	)
	return nil
}

func writeFile(path string, src []byte) error {
	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf(
			"write %s: %w\n"+
				"  What: could not write the generated file\n"+
				"  Why: file system permission or path issue\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the working directory is the module root and is writable",
			path, err, path,
		)
	}
	return nil
}

func cacheAPIResponse(raw []byte, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir %s: %w", dir, err)
	}
	dst := filepath.Join(dir, cacheFile)
	return os.WriteFile(dst, raw, 0o644)
}

// providerAPIMeta holds per-provider metadata extracted from the API for codegen.
type providerAPIMeta struct {
	Name string // display name from API (e.g. "Amazon Bedrock", "XAI")
}

// ErrCacheMiss is returned by fetchModelsWithRaw when --no-fetch is set and the
// cache file does not exist or is empty.
type ErrCacheMiss struct {
	Path string // full resolved path that was missing
}

func (e *ErrCacheMiss) Error() string {
	return fmt.Sprintf(
		"cached api_response.json missing or empty\n"+
			"  Why: --no-fetch was specified; HTTP fetch was skipped\n"+
			"  Where: %s (during cache load step in fetchModelsWithRaw)\n"+
			"  When: during cache load step in fetchModelsWithRaw\n"+
			"  What it means: bestiary-gen cannot proceed without API data\n"+
			"  How to fix: re-run without --no-fetch (HTTP fetch enabled), OR place a previously-cached api_response.json at %s",
		e.Path, e.Path,
	)
}

// fetchModelsWithRaw fetches all models from the models.dev API (or loads from
// the local cache when noFetch is true).
//
//   - dir: cache directory to write/read api_response.json (default: defaultCacheDir).
//   - noFetch: when true, skip HTTP and read from dir/api_response.json instead.
//     Returns *ErrCacheMiss if the cache file is absent or empty.
//
// Returns the raw JSON body, the flat model slice, and per-provider metadata.
func fetchModelsWithRaw(ctx context.Context, dir string, noFetch bool) (rawJSON []byte, models []bestiary.ModelInfo, provMeta map[string]providerAPIMeta, err error) {
	cachePath := filepath.Join(dir, cacheFile)

	if noFetch {
		// Load from cache; no network call.
		body, readErr := os.ReadFile(cachePath)
		if readErr != nil || len(body) == 0 {
			absPath, _ := filepath.Abs(cachePath)
			if absPath == "" {
				absPath = cachePath
			}
			return nil, nil, nil, &ErrCacheMiss{Path: absPath}
		}
		rawJSON = body
	} else {
		// Fetch from the API over HTTP.
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if reqErr != nil {
			return nil, nil, nil, fmt.Errorf(
				"create HTTP request for %s: %w\n"+
					"  What: failed to construct the API request\n"+
					"  How to fix: this is a programming error — report it",
				apiURL, reqErr,
			)
		}

		client := &http.Client{Timeout: 60 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			return nil, nil, nil, fmt.Errorf(
				"HTTP GET %s: %w\n"+
					"  What: network request failed\n"+
					"  How to fix: check network connectivity and retry",
				apiURL, doErr,
			)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, nil, nil, fmt.Errorf(
				"unexpected HTTP status %d from %s; expected 200 OK\n"+
					"  What: the API returned a non-success status\n"+
					"  How to fix: check the API endpoint and try again",
				resp.StatusCode, apiURL,
			)
		}

		const maxBodyBytes = 10 * 1024 * 1024
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if readErr != nil {
			return nil, nil, nil, fmt.Errorf(
				"read response body from %s: %w\n"+
					"  What: failed to read the API response body\n"+
					"  How to fix: retry the operation",
				apiURL, readErr,
			)
		}
		rawJSON = body
	}

	var apiResp genWireResponse
	if err := json.Unmarshal(rawJSON, &apiResp); err != nil {
		return nil, nil, nil, fmt.Errorf(
			"decode JSON from %s: %w\n"+
				"  What: the API response JSON could not be decoded\n"+
				"  Why: the API schema may have changed in a way not handled by json.RawMessage\n"+
				"  How to fix: inspect the API response and update the wire types in cmd/bestiary-gen/main.go",
			apiURL, err,
		)
	}

	provMeta = make(map[string]providerAPIMeta, len(apiResp))
	for providerSlug, prov := range apiResp {
		provMeta[providerSlug] = providerAPIMeta{Name: prov.Name}
		for _, wm := range prov.Models {
			models = append(models, genToModelInfo(providerSlug, wm))
		}
	}
	return rawJSON, models, provMeta, nil
}

// collectFamilies returns a deduplicated sorted list of unique non-empty raw API
// family values found across all models, together with a name hint for casing.
func collectFamilies(models []bestiary.ModelInfo, provMeta map[string]providerAPIMeta) []string {
	seen := make(map[string]struct{})
	for _, m := range models {
		if m.RawFamily != "" {
			seen[string(m.RawFamily)] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// parseCapabilityRaw converts a polymorphic JSON field to a bestiary.Capability.
// The field may be: absent/null/empty → {false}, bool → {b}, object → {true, config}.
func parseCapabilityRaw(raw json.RawMessage) bestiary.Capability {
	if len(raw) == 0 {
		return bestiary.Capability{}
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return bestiary.Capability{Supported: b}
	}
	var cfg map[string]string
	if err := json.Unmarshal(raw, &cfg); err == nil {
		return bestiary.Capability{Supported: true, Config: cfg}
	}
	return bestiary.Capability{}
}

// genToModelInfo converts a genWireModel to bestiary.ModelInfo.
// LastSynced is intentionally left empty — the caller stamps it.
//
// Canonical fields (Family, Variant, Version, Date) are populated at this
// stage by invoking bestiary.ParseFamilyWithVersion, bestiary.ExtractVersionFromID
// (primary source for Version when the raw family field does not embed a version),
// bestiary.ExtractDate, and bestiary.InferFamilyFromIDWithVariant (for models with
// an empty raw family field) so that models_static_gen.go carries baked
// normalization data at compile time with consistent (Family, Variant, Version)
// across providers regardless of whether raw_family is empty or populated
// (SLICE-FIX-2, B5/B6).
func genToModelInfo(providerSlug string, wm genWireModel) bestiary.ModelInfo {
	// Derive normalized family, variant, and version.
	rawFamily := bestiary.Family(wm.Family)
	var normFamily bestiary.Family
	var normVariant string
	var normVersion string
	if rawFamily != "" {
		normFamily, normVariant, normVersion = bestiary.ParseFamilyWithVersion(rawFamily)
		if normVersion == "" {
			// The raw family field (e.g. "claude-opus") does not embed a version.
			// Fall back to extracting the version from the model ID, which is the
			// authoritative source for version numbers per team-lead arbitration
			// (bestiary-5eh8). Example: "claude-opus-4-5-20251101" with family
			// "claude-opus" yields version "4.5".
			normVersion = bestiary.ExtractVersionFromID(bestiary.ModelID(wm.ID), rawFamily)
		}
	} else {
		// ~25% of models have an empty Family field — infer from the model ID.
		// InferFamilyFromIDWithVariant applies the same suffix/pattern logic as
		// ParseFamilyWithVersion, ensuring consistent (Family, Variant, Version)
		// across providers that have empty vs. populated raw_family for the same
		// model ID (SLICE-FIX-2, B5/B6).
		normFamily, normVariant, normVersion = bestiary.InferFamilyFromIDWithVariant(
			bestiary.ModelID(wm.ID),
			bestiary.Provider(providerSlug),
		)
	}

	// Derive normalized date from model ID (primary) or release date (fallback).
	normDate := bestiary.ExtractDate(bestiary.ModelID(wm.ID), wm.ReleaseDate)

	info := bestiary.ModelInfo{
		ID:          bestiary.ModelID(wm.ID),
		Provider:    bestiary.Provider(providerSlug),
		DisplayName: wm.Name,
		RawFamily:   rawFamily,
		Family:      normFamily,
		Variant:     normVariant,
		Version:     normVersion,
		Date:        normDate,
		Reasoning:         wm.Reasoning,
		ToolCall:          wm.ToolCall,
		Attachment:        wm.Attachment,
		Temperature:       wm.Temperature,
		StructuredOutput:  wm.StructuredOutput,
		OpenWeights:       wm.OpenWeights,
		ReleaseDate:       wm.ReleaseDate,
		Knowledge:         wm.Knowledge,
		// interleaved field: polymorphic bool or object.
		Interleaved: parseCapabilityRaw(wm.Interleaved),
		LastSynced:  "",
	}

	if wm.Cost != nil {
		info.CostInputPerMTok = wm.Cost.Input
		info.CostOutputPerMTok = wm.Cost.Output
		info.CostReasoningPerMTok = wm.Cost.Reasoning
		info.CostCacheReadPerMTok = wm.Cost.CacheRead
		info.CostCacheWritePerMTok = wm.Cost.CacheWrite
	}

	if wm.Limit != nil {
		if wm.Limit.Context != nil {
			info.ContextWindow = *wm.Limit.Context
		}
		if wm.Limit.Output != nil {
			info.MaxOutput = *wm.Limit.Output
		}
	}

	if wm.Modalities != nil {
		info.Modalities = genToModalities(wm.Modalities.Input, wm.Modalities.Output)
	}

	return info
}

// genToModalities converts string slices from the API into the typed Modalities
// value. Unrecognised modality strings are silently skipped.
func genToModalities(input, output []string) bestiary.Modalities {
	parseList := func(ss []string) []bestiary.Modality {
		out := make([]bestiary.Modality, 0, len(ss))
		for _, s := range ss {
			var m bestiary.Modality
			if err := m.UnmarshalText([]byte(s)); err == nil {
				out = append(out, m)
			}
		}
		return out
	}
	return bestiary.Modalities{
		Input:  parseList(input),
		Output: parseList(output),
	}
}

// --------------------------------------------------------------------------
// Source generation
// --------------------------------------------------------------------------

// generateSource renders the []ModelInfo slice as a valid Go source file and
// formats it with go/format so the result is gofmt-clean.
// slugToConst maps provider slug → Go constant name (e.g. "anthropic" → "ProviderAnthropic").
func generateSource(models []bestiary.ModelInfo, slugToConst map[string]string) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
	buf.WriteString("//go:generate go run ./cmd/bestiary-gen\n")
	buf.WriteString("\n")
	buf.WriteString("package bestiary\n")
	buf.WriteString("\n")
	// f64 helper avoids taking addresses of numeric literals inline.
	buf.WriteString("// f64 is a code-generation helper that returns a pointer to a float64 literal.\n")
	buf.WriteString("func f64(v float64) *float64 { return &v }\n")
	buf.WriteString("\n")
	buf.WriteString("var staticModels = []ModelInfo{\n")

	for _, m := range models {
		buf.WriteString("\t{\n")
		fmt.Fprintf(&buf, "\t\tID:                    %q,\n", m.ID)
		fmt.Fprintf(&buf, "\t\tProvider:              %s,\n", providerExpr(m.Provider, slugToConst))
		fmt.Fprintf(&buf, "\t\tDisplayName:           %q,\n", m.DisplayName)
		fmt.Fprintf(&buf, "\t\tRawFamily:             %q,\n", m.RawFamily)
		fmt.Fprintf(&buf, "\t\tFamily:                %q,\n", m.Family)
		fmt.Fprintf(&buf, "\t\tVariant:               %q,\n", m.Variant)
		fmt.Fprintf(&buf, "\t\tVersion:               %q,\n", m.Version)
		fmt.Fprintf(&buf, "\t\tDate:                  %q,\n", m.Date)
		fmt.Fprintf(&buf, "\t\tContextWindow:         %d,\n", m.ContextWindow)
		fmt.Fprintf(&buf, "\t\tMaxOutput:             %d,\n", m.MaxOutput)
		fmt.Fprintf(&buf, "\t\tReasoning:             %v,\n", m.Reasoning)
		fmt.Fprintf(&buf, "\t\tToolCall:              %v,\n", m.ToolCall)
		fmt.Fprintf(&buf, "\t\tAttachment:            %v,\n", m.Attachment)
		fmt.Fprintf(&buf, "\t\tTemperature:           %v,\n", m.Temperature)
		fmt.Fprintf(&buf, "\t\tStructuredOutput:      %v,\n", m.StructuredOutput)
		fmt.Fprintf(&buf, "\t\tInterleaved:           %s,\n", capabilityExpr(m.Interleaved))
		fmt.Fprintf(&buf, "\t\tOpenWeights:           %v,\n", m.OpenWeights)
		fmt.Fprintf(&buf, "\t\tCostInputPerMTok:      %s,\n", float64PtrExpr(m.CostInputPerMTok))
		fmt.Fprintf(&buf, "\t\tCostOutputPerMTok:     %s,\n", float64PtrExpr(m.CostOutputPerMTok))
		fmt.Fprintf(&buf, "\t\tCostReasoningPerMTok:  %s,\n", float64PtrExpr(m.CostReasoningPerMTok))
		fmt.Fprintf(&buf, "\t\tCostCacheReadPerMTok:  %s,\n", float64PtrExpr(m.CostCacheReadPerMTok))
		fmt.Fprintf(&buf, "\t\tCostCacheWritePerMTok: %s,\n", float64PtrExpr(m.CostCacheWritePerMTok))
		fmt.Fprintf(&buf, "\t\tReleaseDate:           %q,\n", m.ReleaseDate)
		fmt.Fprintf(&buf, "\t\tKnowledge:             %q,\n", m.Knowledge)
		fmt.Fprintf(&buf, "\t\tModalities:            %s,\n", modalitiesExpr(m.Modalities))
		fmt.Fprintf(&buf, "\t\tLastSynced:            %q,\n", m.LastSynced)
		buf.WriteString("\t},\n")
	}

	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format failed: %w\n"+
				"  What: the generated Go source is not syntactically valid\n"+
				"  Why: a codegen template bug produced invalid Go\n"+
				"  How to fix: inspect the unformatted buffer for syntax errors\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err,
			truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}

// generateProvidersSource generates providers_gen.go with one Provider constant
// per API slug plus a knownProviders array and Providers() function.
// allSlugs must be sorted alphabetically.
func generateProvidersSource(allSlugs []string, provMeta map[string]providerAPIMeta) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n\n")
	buf.WriteString("package bestiary\n\n")
	buf.WriteString("const (\n")
	for _, slug := range allSlugs {
		meta := provMeta[slug]
		constName := providerConstName(slug, meta.Name)
		fmt.Fprintf(&buf, "\t%s Provider = %q\n", constName, slug)
	}
	buf.WriteString(")\n\n")

	// knownProviders: all API providers alphabetically, then ProviderLocal last.
	buf.WriteString("// knownProviders contains all Provider constants from the models.dev API\n")
	buf.WriteString("// plus ProviderLocal. Used by IsKnown() and Providers().\n")
	buf.WriteString("var knownProviders = [...]Provider{\n")
	for _, slug := range allSlugs {
		meta := provMeta[slug]
		constName := providerConstName(slug, meta.Name)
		fmt.Fprintf(&buf, "\t%s,\n", constName)
	}
	buf.WriteString("\tProviderLocal, // bestiary-specific, always last\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format providers_gen.go: %w\n"+
				"  What: the generated providers source is not syntactically valid\n"+
				"  How to fix: inspect slugToIdentifier output for invalid identifiers\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err, truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}

// generateFamiliesSource generates families_gen.go with one Family constant
// per unique non-empty family value found in the API response.
func generateFamiliesSource(families []string) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n\n")
	buf.WriteString("package bestiary\n\n")
	buf.WriteString("// Family identifies the model family from the models.dev API.\n")
	buf.WriteString("// It is a named string type for type safety, following the same pattern as Provider.\n")
	buf.WriteString("type Family string\n\n")

	if len(families) > 0 {
		buf.WriteString("const (\n")
		for _, fam := range families {
			constName := familyConstName(fam, "")
			fmt.Fprintf(&buf, "\t%s Family = %q\n", constName, fam)
		}
		buf.WriteString(")\n\n")
	}

	// Families() function returning a defensive copy.
	buf.WriteString("// allFamilies is the complete list of family values from the models.dev API.\n")
	buf.WriteString("var allFamilies = [...]Family{\n")
	for _, fam := range families {
		fmt.Fprintf(&buf, "\t%q,\n", fam)
	}
	buf.WriteString("}\n\n")
	buf.WriteString("// Families returns all known Family values as a defensive copy.\n")
	buf.WriteString("func Families() []Family {\n")
	buf.WriteString("\tout := make([]Family, len(allFamilies))\n")
	buf.WriteString("\tcopy(out, allFamilies[:])\n")
	buf.WriteString("\treturn out\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format families_gen.go: %w\n"+
				"  What: the generated families source is not syntactically valid\n"+
				"  How to fix: inspect slugToIdentifier output for invalid identifiers\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err, truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}

// providerExpr returns the Go expression for a Provider value.
// Uses the slug→const map; falls back to a typed string literal for unknown providers.
func providerExpr(p bestiary.Provider, slugToConst map[string]string) string {
	if constName, ok := slugToConst[string(p)]; ok {
		return constName
	}
	return fmt.Sprintf("Provider(%q)", string(p))
}

func countUniqueProviders(models []bestiary.ModelInfo) int {
	seen := make(map[bestiary.Provider]struct{})
	for _, m := range models {
		seen[m.Provider] = struct{}{}
	}
	return len(seen)
}

// capabilityExpr renders a bestiary.Capability as a Go composite literal.
// When Config is nil it emits: Capability{Supported: <bool>}
// When Config is non-nil it emits: Capability{Supported: true, Config: map[string]string{...}}
func capabilityExpr(c bestiary.Capability) string {
	if len(c.Config) == 0 {
		return fmt.Sprintf("Capability{Supported: %v}", c.Supported)
	}
	var sb strings.Builder
	sb.WriteString("Capability{Supported: true, Config: map[string]string{")
	i := 0
	for k, v := range c.Config {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%q: %q", k, v)
		i++
	}
	sb.WriteString("}}")
	return sb.String()
}

// float64PtrExpr renders a *float64 as either "nil" or "f64(<value>)".
func float64PtrExpr(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("f64(%v)", *p)
}

// modalitiesExpr renders a Modalities value as a Go composite literal.
func modalitiesExpr(m bestiary.Modalities) string {
	var sb strings.Builder
	sb.WriteString("Modalities{")
	if len(m.Input) > 0 {
		sb.WriteString("Input: []Modality{")
		for i, mod := range m.Input {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(modalityExpr(mod))
		}
		sb.WriteString("}")
	}
	if len(m.Output) > 0 {
		if len(m.Input) > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("Output: []Modality{")
		for i, mod := range m.Output {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(modalityExpr(mod))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

// modalityExpr returns the Go constant name for a Modality.
func modalityExpr(m bestiary.Modality) string {
	switch m {
	case bestiary.ModalityText:
		return "ModalityText"
	case bestiary.ModalityImage:
		return "ModalityImage"
	case bestiary.ModalityPDF:
		return "ModalityPDF"
	case bestiary.ModalityAudio:
		return "ModalityAudio"
	case bestiary.ModalityVideo:
		return "ModalityVideo"
	default:
		return fmt.Sprintf("Modality(%d)", int(m))
	}
}

// validateGeneratedFamilyType reads the generated file at path and asserts that it
// contains a named Family type declaration ("type Family string") and does NOT
// contain a type alias declaration ("type Family = string").
//
// This post-condition guards against a regression where the codegen template
// accidentally emits an alias instead of a named type, which would break the
// Family methods defined in family.go (methods cannot be attached to aliases of
// built-in types defined in another package).
//
// Returns a detailed actionable error when either assertion fails.
func validateGeneratedFamilyType(path string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf(
			"validateGeneratedFamilyType: read %s: %w\n"+
				"  What: could not read the generated families file\n"+
				"  Why: the file may not have been written yet or is inaccessible\n"+
				"  Where: %s\n"+
				"  How to fix: ensure generateFamiliesSource wrote the file before this validation runs",
			path, err, path,
		)
	}
	namedDecl := []byte("type Family string")
	aliasDecl := []byte("type Family = string")

	if !bytes.Contains(src, namedDecl) {
		return fmt.Errorf(
			"validateGeneratedFamilyType: named-type declaration not found in %s\n"+
				"  What: expected %q but did not find it\n"+
				"  Why: the generateFamiliesSource template may have changed\n"+
				"  Where: %s\n"+
				"  How to fix: ensure generateFamiliesSource emits \"type Family string\" (no '=' sign)",
			path, string(namedDecl), path,
		)
	}
	if bytes.Contains(src, aliasDecl) {
		return fmt.Errorf(
			"validateGeneratedFamilyType: alias declaration found in %s\n"+
				"  What: found %q — this is a type alias, not a named type\n"+
				"  Why: the generateFamiliesSource template emitted an alias instead of a named type\n"+
				"  Where: %s\n"+
				"  How to fix: change the template to emit \"type Family string\" (remove the '=' sign)",
			path, string(aliasDecl), path,
		)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}

// --------------------------------------------------------------------------
// Model_ constants generation (SLICE-4)
// --------------------------------------------------------------------------

// outputConstantsPath is the file that generateConstantsSource writes.
const outputConstantsPath = "models_constants_gen.go"

// tokenToConstPart converts a single hyphen/dot-split token from a model ID into
// a constant-name segment. Rules (in priority order):
//  1. casingOverrides wins (e.g. "gpt" → "GPT", "ai" → "AI").
//  2. Token leading with a digit: keep digit prefix verbatim; apply overrides to alpha suffix.
//  3. Otherwise: title-case the token.
//
// Within-component characters are preserved ("4o" → "4o", not "4_o").
func tokenToConstPart(tok string) string {
	if tok == "" {
		return ""
	}
	lower := strings.ToLower(tok)

	// 1. Casing override for the full token.
	if override, ok := casingOverrides[lower]; ok {
		return override
	}

	// 2. Digit-leading token: split at first alpha character.
	// "4o" → "4o" (within-component characters preserved — no casing change).
	// "302ai" → "302AI" (casingOverride applies to multi-char alpha suffix).
	if unicode.IsDigit(rune(tok[0])) {
		// Find split point between digit prefix and alpha suffix.
		splitAt := -1
		for i, r := range tok {
			if !unicode.IsDigit(r) {
				splitAt = i
				break
			}
		}
		if splitAt < 0 {
			// All digits — return as-is.
			return tok
		}
		digitPart := tok[:splitAt]
		alphaPart := tok[splitAt:]
		alphaLower := strings.ToLower(alphaPart)
		if override, ok := casingOverrides[alphaLower]; ok {
			return digitPart + override
		}
		// Preserve the alpha part exactly as-is (spec: "4o stays 4o, not 4_o").
		return digitPart + alphaPart
	}

	// 3. Default: title-case.
	return strings.ToUpper(lower[:1]) + lower[1:]
}

// nameForCanonical derives the Model__* constant name for a single ModelInfo.
//
// The slugToConst map (slug → Go constant name, e.g. "openai" → "ProviderOpenAI")
// is used to resolve the provider suffix with the correct display casing.
// Pass nil to fall back to slugToIdentifier without a name hint.
//
// Algorithm:
//  1. Provider segment: strip "Provider" prefix from the constant name.
//  2. Strip any provider prefix from the raw ID (anything up to and including "/").
//  3. Determine if the Date is embedded in the raw ID (all forms:
//     YYYYMMDD, YYYY-MM-DD, MM-YYYY, MM-DD). Strip that form from the end of the ID.
//  4. If Version is non-empty, compute a version segment ("4.5" → "4_5")
//     and strip the version tokens from the end of the ID (before the date).
//  5. Split remaining raw ID on non-alphanumeric characters; map each token through
//     tokenToConstPart.
//  6. Join all parts with "__" (double underscore), prefix "Model__<ProviderSuffix>__".
//  7. If a version segment was produced, append "__<version>".
//  8. Append "__YYYYMMDD" when the date was found in the raw ID.
//
// Naming: Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
// Double underscores separate top-level segments; single underscores appear only
// within a segment (e.g. version "4.5" → "4_5", date always YYYYMMDD).
//
// Returns "" when the skip rule applies (Family == "").
func nameForCanonical(m bestiary.ModelInfo) string {
	return nameForCanonicalWithMap(m, nil)
}

// nameForCanonicalWithMap is the full implementation of nameForCanonical with
// an optional slugToConst map for correct provider casing.
func nameForCanonicalWithMap(m bestiary.ModelInfo, slugToConst map[string]string) string {
	if m.Family == "" {
		return "" // skip rule: no family, no extractable family
	}

	// Derive provider suffix.
	provSuffix := ""
	if slugToConst != nil {
		if constName, ok := slugToConst[string(m.Provider)]; ok {
			provSuffix = strings.TrimPrefix(constName, "Provider")
		}
	}
	if provSuffix == "" {
		provSuffix = slugToIdentifier(string(m.Provider), "")
	}
	if provSuffix == "" {
		provSuffix = "Unknown"
	}

	rawID := string(m.ID)

	// Strip any leading path segments from the raw ID.
	// Some providers use multi-segment paths in model IDs:
	//   - "accounts/fireworks/models/deepseek-v3p1" → strip "accounts/fireworks/models/"
	//   - "workers-ai/@cf/meta/llama-3.1-8b-instruct" → strip "workers-ai/@cf/meta/"
	//   - "@cf/meta/llama-3.1-8b-instruct" → strip "@cf/meta/"
	//   - "anthropic/claude-opus-4-20250514" → strip "anthropic/"
	// We strip everything up to and including the last "/" to get just the model name.
	if idx := strings.LastIndexByte(rawID, '/'); idx >= 0 {
		rawID = rawID[idx+1:]
	}
	// Strip a leading "@" if present (e.g. "@cf/..." already stripped to "..." above,
	// but catch remaining "@" characters in ID suffixes).
	rawID = strings.TrimLeft(rawID, "@")

	// Compute date segment (compact form, no hyphens).
	dateCompact := strings.ReplaceAll(m.Date, "-", "") // YYYYMMDD or ""

	// Strip the date from the raw ID and remember if we found it
	// (we only append the date constant if the ID actually contains the date).
	// Note: Google Vertex Anthropic uses "@" as date separator (e.g. "claude-opus-4@20250514").
	// We must also strip the "@YYYYMMDD" form.
	rawIDForDate := strings.ReplaceAll(rawID, "@", "-")
	rawIDStripped, dateFoundInID := stripDateFromID(rawIDForDate, m.Date, dateCompact)

	// Compute version segment: Version "4.5" → "4_5".
	// If Version is non-empty, strip the version tokens from the end of
	// rawIDStripped (they appear immediately before the date in the raw ID).
	versionSegment := ""
	if m.Version != "" {
		// Convert "4.5" → "4_5" (dots to underscores; no hyphens expected in Version).
		versionSegment = strings.ReplaceAll(m.Version, ".", "_")

		// Strip the version portion from rawIDStripped. The version appears in the
		// raw ID as hyphen-separated digits/tokens (e.g. "4-5" for version "4.5").
		// We try the hyphenated form and the dotted form as suffixes.
		versionHyphen := strings.ReplaceAll(m.Version, ".", "-")
		for _, vSuffix := range []string{versionHyphen, m.Version} {
			if vSuffix == "" {
				continue
			}
			if strings.HasSuffix(rawIDStripped, "-"+vSuffix) {
				rawIDStripped = strings.TrimSuffix(rawIDStripped, "-"+vSuffix)
				rawIDStripped = strings.TrimRight(rawIDStripped, "-.")
				break
			}
			if strings.HasSuffix(rawIDStripped, "."+vSuffix) {
				rawIDStripped = strings.TrimSuffix(rawIDStripped, "."+vSuffix)
				rawIDStripped = strings.TrimRight(rawIDStripped, "-.")
				break
			}
		}
	}

	// Split on any non-alphanumeric character to produce clean identifier tokens.
	// This handles all separator styles: hyphens, dots, colons, underscores, slashes,
	// at-signs, etc. found in the wild across various provider model ID formats.
	tokens := strings.FieldsFunc(rawIDStripped, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var parts []string
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		p := tokenToConstPart(tok)
		if p != "" {
			parts = append(parts, p)
		}
	}

	// Join parts with double underscores (between-component separator).
	// Single underscores appear only within a segment (e.g. version "4_5").
	name := "Model__" + provSuffix + "__" + strings.Join(parts, "__")
	if versionSegment != "" {
		name += "__" + versionSegment
	}
	if dateFoundInID && dateCompact != "" {
		name += "__" + dateCompact
	}
	return name
}

// stripDateFromID strips the date portion from the end of a model ID string.
// It recognizes several date forms (in priority order):
//  1. YYYYMMDD compact (e.g. "20250514" in "claude-opus-4-20250514")
//  2. YYYY-MM-DD hyphenated (e.g. "2024-08-06" in "gpt-4o-2024-08-06")
//  3. MM-YYYY (e.g. "09-2025" in "gemini-2.5-flash-lite-preview-09-2025")
//  4. MM-DD   (e.g. "06-17" in "gemini-2.5-flash-lite-preview-06-17")
//
// Returns (strippedID, true) when any form is found, or (rawID, false) when no
// date form is found in the ID.
func stripDateFromID(rawID, normalizedDate, dateCompact string) (string, bool) {
	if normalizedDate == "" {
		return rawID, false
	}

	// Form 1: compact YYYYMMDD suffix.
	if strings.HasSuffix(rawID, dateCompact) {
		stripped := strings.TrimSuffix(rawID, dateCompact)
		stripped = strings.TrimRight(stripped, "-.")
		return stripped, true
	}

	// Form 2: YYYY-MM-DD suffix.
	if strings.HasSuffix(rawID, normalizedDate) {
		stripped := strings.TrimSuffix(rawID, normalizedDate)
		stripped = strings.TrimRight(stripped, "-.")
		return stripped, true
	}

	// Forms 3 and 4 require parsing YYYY-MM-DD into parts.
	parts := strings.SplitN(normalizedDate, "-", 3)
	if len(parts) != 3 {
		return rawID, false
	}
	// yyyy, mm, dd := parts[0], parts[1], parts[2]
	mm, dd, yyyy := parts[1], parts[2], parts[0]

	// Form 3: MM-YYYY (e.g. "09-2025").
	mmYYYY := mm + "-" + yyyy
	if strings.HasSuffix(rawID, mmYYYY) {
		stripped := strings.TrimSuffix(rawID, mmYYYY)
		stripped = strings.TrimRight(stripped, "-.")
		return stripped, true
	}

	// Form 4: MM-DD (e.g. "06-17").
	mmDD := mm + "-" + dd
	if strings.HasSuffix(rawID, mmDD) {
		stripped := strings.TrimSuffix(rawID, mmDD)
		stripped = strings.TrimRight(stripped, "-.")
		return stripped, true
	}

	return rawID, false
}

// resolveCollisions takes a slice of candidate constant names (parallel to
// models) and returns a slice of final, unique names. The input name "" means
// "skip this model" — the output preserves "" at those positions.
//
// Two-pass algorithm:
//
//	Pass 1: detect all positions that share the same candidate name (collisions).
//	Pass 2: for each collision group, apply disambiguators in priority order:
//	  (a) Append a version segment extracted from the raw model ID.
//	  (b) Append "_2", "_3", … (sequential suffix, last resort).
func resolveCollisions(names []string, models []bestiary.ModelInfo) []string {
	result := make([]string, len(names))
	copy(result, names)

	// Pass 1: group positions by their candidate name.
	nameToPositions := make(map[string][]int, len(names))
	for i, n := range names {
		if n == "" {
			continue // skip-rule models have no constant
		}
		nameToPositions[n] = append(nameToPositions[n], i)
	}

	// Pass 2: resolve each collision group.
	for baseName, positions := range nameToPositions {
		if len(positions) < 2 {
			continue // no collision — keep as-is
		}

		// (a) Try to append a version segment extracted from the model ID.
		// Version segment: the part of the raw ID that is between the date-stripped
		// common prefix and the date. We use the tokenized ID minus the common tokens.
		// Simplified heuristic: extract version-like tokens not present in other models.
		type candidate struct {
			pos     int
			vSuffix string // "" means no usable version suffix was found
		}
		cands := make([]candidate, len(positions))
		for k, pos := range positions {
			cands[k] = candidate{pos: pos, vSuffix: extractVersionSegment(models[pos])}
		}

		// Check if all version suffixes are distinct and non-empty.
		vSuffixSeen := make(map[string]int)
		for _, c := range cands {
			if c.vSuffix != "" {
				vSuffixSeen[c.vSuffix]++
			}
		}
		allDistinct := true
		for _, c := range cands {
			if c.vSuffix == "" || vSuffixSeen[c.vSuffix] > 1 {
				allDistinct = false
				break
			}
		}

		if allDistinct {
			// Version suffix disambiguates all colliders.
			// Use "__" to separate the version disambiguation suffix from the base name
			// (consistent with the Model__<Provider>__<...> double-underscore template).
			for _, c := range cands {
				result[c.pos] = baseName + "__" + c.vSuffix
			}
		} else {
			// (b) Sequential suffix fallback.
			// Sort positions for deterministic ordering.
			sortedPos := make([]int, len(positions))
			copy(sortedPos, positions)
			sort.Ints(sortedPos)
			for idx, pos := range sortedPos {
				if idx == 0 {
					result[pos] = baseName + "_1"
				} else {
					result[pos] = baseName + "_" + fmt.Sprintf("%d", idx+1)
				}
			}
		}
	}

	// Final uniqueness pass: if version-suffix disambiguation introduced new collisions
	// (rare), fall back to sequential suffixes for remaining groups.
	finalSeen := make(map[string][]int)
	for i, n := range result {
		if n == "" {
			continue
		}
		finalSeen[n] = append(finalSeen[n], i)
	}
	for _, positions := range finalSeen {
		if len(positions) < 2 {
			continue
		}
		sortedPos := make([]int, len(positions))
		copy(sortedPos, positions)
		sort.Ints(sortedPos)
		// Append _<n> to break the tie.
		for idx, pos := range sortedPos {
			result[pos] = result[pos] + fmt.Sprintf("_%d", idx+1)
		}
	}

	return result
}

// extractVersionSegment returns a short string that uniquely identifies the
// version of a model within its family+variant. It is used as a tie-breaker
// in collision pass (a).
//
// Strategy:
//  1. Strip provider prefix from raw ID.
//  2. Strip date suffix from raw ID (using stripDateFromID for all date forms).
//  3. Strip the Family prefix from the remaining ID tokens.
//  4. Strip the Variant prefix from the remaining tokens.
//  5. Whatever is left is the version segment (joined with "_").
//
// Returns "" when no distinct version can be extracted.
func extractVersionSegment(m bestiary.ModelInfo) string {
	rawID := string(m.ID)

	// Strip any leading path segments (same logic as nameForCanonicalWithMap).
	if idx := strings.LastIndexByte(rawID, '/'); idx >= 0 {
		rawID = rawID[idx+1:]
	}
	rawID = strings.TrimLeft(rawID, "@")

	dateCompact := strings.ReplaceAll(m.Date, "-", "")

	// Normalize "@" date separator (Google Vertex Anthropic style).
	rawIDForDate := strings.ReplaceAll(rawID, "@", "-")

	// Strip date from end using the same logic as nameForCanonical.
	rawIDStripped, _ := stripDateFromID(rawIDForDate, m.Date, dateCompact)

	// Tokenize — split on any non-alphanumeric character.
	tokens := strings.FieldsFunc(rawIDStripped, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	// Strip known family tokens.
	// NOTE: Family and Variant only use hyphens and dots as separators
	// (models.dev normalized fields). Using the narrow hyphen-dot splitter
	// here is intentional and matches all real data. If a future provider introduces
	// underscores or other separators in a normalized family slug, unify this with the
	// universal non-alphanumeric splitter used above for rawIDStripped.
	familyTokens := strings.FieldsFunc(string(m.Family), func(r rune) bool {
		return r == '-' || r == '.'
	})
	variantTokens := strings.FieldsFunc(m.Variant, func(r rune) bool {
		return r == '-' || r == '.'
	})

	// Build a set of "known" tokens (family + variant) to skip.
	knownTokens := make(map[string]struct{}, len(familyTokens)+len(variantTokens))
	for _, t := range familyTokens {
		knownTokens[strings.ToLower(t)] = struct{}{}
	}
	for _, t := range variantTokens {
		knownTokens[strings.ToLower(t)] = struct{}{}
	}

	var versionParts []string
	for _, tok := range tokens {
		if _, known := knownTokens[strings.ToLower(tok)]; !known {
			p := tokenToConstPart(tok)
			if p != "" {
				versionParts = append(versionParts, p)
			}
		}
	}

	return strings.Join(versionParts, "_")
}

// generateConstantsSource generates models_constants_gen.go containing one
// Model__* constant per eligible model in the static data, plus a ModelIDs()
// function returning a defensive copy.
//
// slugToConst maps provider slug → Go constant name (e.g. "openai" → "ProviderOpenAI").
// It is used for correct provider casing in the generated constant names.
// Pass nil to use the slug directly with slugToIdentifier (less accurate casing).
//
// Eligibility: Family must be non-empty.
// Naming: Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
// Double underscores separate top-level segments; single underscores appear only
// within a segment (e.g. version "4.5" → "4_5").
// Collision resolution: two-pass (version suffix → sequential suffix).
func generateConstantsSource(models []bestiary.ModelInfo, slugToConst map[string]string) ([]byte, error) {
	// Build candidate names.
	candidateNames := make([]string, len(models))
	for i, m := range models {
		candidateNames[i] = nameForCanonicalWithMap(m, slugToConst)
	}

	// Resolve collisions.
	resolvedNames := resolveCollisions(candidateNames, models)

	// Build the sorted list of (constName, modelID) pairs for output.
	// Skip entries where constName == "" (skip-rule models).
	type constEntry struct {
		constName string
		modelID   bestiary.ModelID
	}
	var entries []constEntry
	for i, name := range resolvedNames {
		if name == "" {
			continue
		}
		entries = append(entries, constEntry{constName: name, modelID: models[i].ID})
	}

	// Sort by constant name for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].constName < entries[j].constName
	})

	var buf bytes.Buffer
	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
	buf.WriteString("//go:generate go run ./cmd/bestiary-gen\n")
	buf.WriteString("\n")
	buf.WriteString("package bestiary\n\n")

	if len(entries) > 0 {
		buf.WriteString("// Model__* constants provide compile-time references to every eligible model\n")
		buf.WriteString("// in the static model registry. Names follow the pattern:\n")
		buf.WriteString("//   Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?\n")
		buf.WriteString("// where each component uses the same casing rules as Provider and Family\n")
		buf.WriteString("// constants. Double underscores separate top-level components; single\n")
		buf.WriteString("// underscores appear only within a component (e.g. version \"4.5\" → \"4_5\",\n")
		buf.WriteString("// \"4o\" stays \"4o\" within a component).\n")
		buf.WriteString("const (\n")
		for _, e := range entries {
			fmt.Fprintf(&buf, "\t%s ModelID = %q\n", e.constName, e.modelID)
		}
		buf.WriteString(")\n\n")
	}

	// allModelConstants: backing array for ModelIDs().
	buf.WriteString("// allModelConstants is the complete list of generated Model__* constants.\n")
	buf.WriteString("var allModelConstants = [...]ModelID{\n")
	for _, e := range entries {
		fmt.Fprintf(&buf, "\t%s,\n", e.constName)
	}
	buf.WriteString("}\n\n")

	// ModelIDs() defensive copy.
	// NOTE: Models() in registry.go returns []ModelInfo (full metadata). This function
	// returns []ModelID (the constant values only). The distinct name avoids a compile-
	// time conflict with the existing registry.go:Models() function.
	buf.WriteString("// ModelIDs returns the canonical Model_<...> constant values from the codegen\n")
	buf.WriteString("// pipeline. The name diverges from PROPOSAL-3's spec (Models() []ModelID) to\n")
	buf.WriteString("// avoid clashing with registry.go:Models() []ModelInfo. See bestiary-p6l5.\n")
	buf.WriteString("//\n")
	buf.WriteString("// The returned slice is a defensive copy; mutating it does not affect future calls.\n")
	buf.WriteString("// See Models() in registry.go for the full ModelInfo slice (metadata + constants).\n")
	buf.WriteString("func ModelIDs() []ModelID {\n")
	buf.WriteString("\tout := make([]ModelID, len(allModelConstants))\n")
	buf.WriteString("\tcopy(out, allModelConstants[:])\n")
	buf.WriteString("\treturn out\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format models_constants_gen.go: %w\n"+
				"  What: the generated constants source is not syntactically valid\n"+
				"  Why: a codegen template bug produced invalid Go\n"+
				"  How to fix: inspect the unformatted buffer for syntax errors\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err, truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}
