// bestiary-gen fetches the models.dev API and writes three generated files
// into the bestiary package root:
//   - models_static_gen.go  — all ~4168 model records
//   - providers_gen.go      — one Provider constant per API slug + knownProviders
//   - families_gen.go       — one Family constant per unique API family value
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
	"ai":  "AI",
	"api": "API",
	"gpt": "GPT",
	"llm": "LLM",
	"io":  "IO",
	"sap": "SAP",
	"ovh": "OVH",
	"cn":  "CN",
	"ams": "AMS",
	"sgp": "SGP",
	"xai": "XAI",
	"aws": "AWS",
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

// CLI filter flags (set by parseFlags).
var (
	onlyProviders    []string // -only-providers: inclusion list (empty = all)
	excludeProviders []string // -all-providers-except: exclusion list (empty = none)
)

// Output paths are relative to the module root (where go generate is run from).
const (
	outputPath          = "models_static_gen.go"
	outputProvidersPath = "providers_gen.go"
	outputFamiliesPath  = "families_gen.go"
	defaultCacheDir     = ".bestiary-gen-cache"
	cacheFile           = "api_response.json"
)

const apiURL = "https://models.dev/api.json"

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
// Returns an error if both -only-providers and -all-providers-except are specified
// simultaneously (mutually exclusive).
func parseFlags(args []string) (flagResult, error) {
	var res flagResult
	res.cacheDir = defaultCacheDir // default: backward-compatible

	for i := 0; i < len(args); i++ {
		arg := args[i]
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

	fmt.Fprintf(os.Stdout,
		"bestiary-gen: wrote %s with %d models (%d providers), %s with %d constants, %s with %d constants at %s\n",
		outputPath, len(filtered), countUniqueProviders(filtered),
		outputProvidersPath, len(allSlugs),
		outputFamiliesPath, len(familyMeta),
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
			"  What: cached api_response.json missing or empty\n"+
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

// collectFamilies returns a deduplicated sorted list of unique non-empty family
// values found across all models, together with a name hint for casing.
func collectFamilies(models []bestiary.ModelInfo, provMeta map[string]providerAPIMeta) []string {
	seen := make(map[string]struct{})
	for _, m := range models {
		if m.Family != "" {
			seen[string(m.Family)] = struct{}{}
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
func genToModelInfo(providerSlug string, wm genWireModel) bestiary.ModelInfo {
	info := bestiary.ModelInfo{
		ID:               bestiary.ModelID(wm.ID),
		Provider:         bestiary.Provider(providerSlug),
		DisplayName:      wm.Name,
		Family:           bestiary.Family(wm.Family),
		Reasoning:        wm.Reasoning,
		ToolCall:         wm.ToolCall,
		Attachment:       wm.Attachment,
		Temperature:      wm.Temperature,
		StructuredOutput: wm.StructuredOutput,
		OpenWeights:      wm.OpenWeights,
		ReleaseDate:      wm.ReleaseDate,
		Knowledge:        wm.Knowledge,
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
		fmt.Fprintf(&buf, "\t\tFamily:                %q,\n", m.Family)
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}
