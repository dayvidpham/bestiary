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
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dayvidpham/bestiary"
)

// brandCasing is the curated brand-casing table:
// lowercase token → preferred Go IDENTIFIER stylization. It is the single source the
// shared styleSegment seam consults per-segment, so Provider, Family, and Model__
// identifiers all stylize consistently. It ONLY affects generated SYMBOL names (and
// optionally DisplayName) — never the Family FIELD value, any runtime string, or the
// decomposition pipeline.
//
// Entries are RATIFIED or AUTO-APPLY (clearly-curated
// batch). An un-curated token defaults to title-case (incremental honest-audit). Any
// genuinely-ambiguous new casing must be SURFACED for user sign-off, never guessed here.
var brandCasing = map[string]string{
	// ── existing acronym/segment overrides (preserved) ──
	"ai":      "AI",
	"api":     "API",
	"chatgpt": "ChatGPT",
	"llm":     "LLM",
	"io":      "IO",
	"sap":     "SAP",
	"ovh":     "OVH",
	"cn":      "CN",
	"ams":     "AMS",
	"sgp":     "SGP",
	"aws":     "AWS",

	// ── RATIFIED casings ──
	"nvidia":     "Nvidia", // NOT NVIDIA
	"togetherai": "TogetherAI",
	"llmgateway": "LlmGateway", // NOT LLMGateway
	"iflowcn":    "iFlowCN",
	"nearai":     "NearAI",
	"gmicloud":   "GMICloud",

	// ── AUTO-APPLY (clearly-curated, user-confirmed batch) ──
	"openrouter":  "OpenRouter",
	"deepseek":    "DeepSeek",
	"minimax":     "MiniMax",
	"openai":      "OpenAI",
	"deepinfra":   "DeepInfra",
	"huggingface": "HuggingFace",
	"moonshotai":  "MoonshotAI",
	"xai":         "xAI", // was XAI; ratified brand is xAI
	"github":      "GitHub",
	"gitlab":      "GitLab",
	"gpt":         "GPT",
	"glm":         "GLM",
	"qwen":        "Qwen",
	"olmo":        "OLMo",
	"internlm":    "InternLM",
	"smollm":      "SmolLM",
	"wizardlm":    "WizardLM",
	"codellama":   "CodeLlama",
}

// styleSegment is the ONE shared per-segment identifier-styling seam. It
// consults the curated brandCasing table for the whole token, then (for a digit-leading
// token) for the alpha suffix, and otherwise title-cases.
//
// Returns (result, handled): handled=true when the result is DEFINITIVE for this segment
// — i.e. a curated brand entry applied, OR the token is digit-leading (whose styling is
// fully resolved here, matching the legacy order where digit handling preceded the
// name-hint fallback). handled=false for a plain (non-digit, un-curated) token, whose
// returned value is the default title-case form; a caller with an additional fallback
// (slugToIdentifier's API name-hint) may override it before settling on title-case.
//
// preserveDigitSuffix controls the un-curated alpha suffix of a digit-leading token:
// true keeps it verbatim ("4o" → "4o", the Model__ segment rule), false title-cases it
// ("302ab" → "302Ab", the slug identifier rule). A curated suffix (e.g. "ai"→"AI") wins
// either way ("302ai" → "302AI").
func styleSegment(tok string, preserveDigitSuffix bool) (string, bool) {
	if tok == "" {
		return "", true
	}
	lower := strings.ToLower(tok)
	if s, ok := brandCasing[lower]; ok {
		return s, true
	}
	if unicode.IsDigit(rune(tok[0])) {
		splitAt := -1
		for i, r := range tok {
			if !unicode.IsDigit(r) {
				splitAt = i
				break
			}
		}
		if splitAt < 0 {
			return tok, true // all digits
		}
		digitPart, alphaPart := tok[:splitAt], tok[splitAt:]
		if s, ok := brandCasing[strings.ToLower(alphaPart)]; ok {
			return digitPart + s, true
		}
		if preserveDigitSuffix {
			return digitPart + alphaPart, true
		}
		return digitPart + strings.ToUpper(alphaPart[:1]) + alphaPart[1:], true
	}
	// Plain un-curated token: title-case, NOT definitive (caller may apply a name-hint).
	return strings.ToUpper(lower[:1]) + lower[1:], false
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

		// 1+2. Shared seam: curated brand-casing (full token, then digit-suffix) and
		// digit-leading handling. preserveDigitSuffix=false → an un-curated digit suffix
		// is title-cased (the slug identifier rule). When styleSegment reports it handled
		// the segment (brand hit or digit-leading), that result is definitive.
		if styled, handled := styleSegment(tok, false); handled {
			sb.WriteString(styled)
			continue
		}

		// 3. Plain un-curated token: prefer an API display-name casing hint.
		if hint, ok := nameHintWords[lower]; ok {
			if styledHint, ok2 := brandCasing[strings.ToLower(hint)]; ok2 {
				sb.WriteString(styledHint)
			} else {
				sb.WriteString(strings.ToUpper(hint[:1]) + hint[1:])
			}
			continue
		}

		// 4. Default: title-case the token (styleSegment's non-definitive result).
		styled, _ := styleSegment(tok, false)
		sb.WriteString(styled)
	}
	return sb.String()
}

// providerConstName returns the Go identifier for a Provider constant given its slug.
// Examples: "anthropic" → "ProviderAnthropic", "302ai" → "Provider302AI",
// "xai" → "ProviderxAI", "amazon-bedrock" → "ProviderAmazonBedrock".
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
	outputPath            = "models_static_gen.go"
	outputProvidersPath   = "providers_gen.go"
	outputFamiliesPath    = "families_gen.go"
	defaultCacheDir       = ".bestiary-gen-cache"
	cacheFile             = "api_response.json"
	versionDuplicatesFile = "version_duplicates.json"
	dotFormAuditFile      = "dot_form_audit.json"
)

// VersionDuplicateKey identifies a group of models that share (provider, family,
// variant, version) but differ in date or other attributes. Written to
// version_duplicates.json as a work-list for the future duplicate collapse.
// Recognition only — duplicates remain two separate constants in the current epoch.
type VersionDuplicateKey struct {
	Provider string `json:"provider"`
	Family   string `json:"family"`
	Variant  string `json:"variant"`
	Version  string `json:"version"`
}

// VersionDuplicateGroup records all model IDs that share the same
// (provider, family, variant, version) key.
type VersionDuplicateGroup struct {
	Key      VersionDuplicateKey `json:"key"`
	ModelIDs []string            `json:"model_ids"`
}

// VersionDuplicatesEnvelope is the top-level JSON structure written to
// .bestiary-gen-cache/version_duplicates.json.
type VersionDuplicatesEnvelope struct {
	SchemaVersion  int                     `json:"schema_version"`
	GeneratedAt    time.Time               `json:"generated_at"`
	DuplicateCount int                     `json:"duplicate_count"` // number of groups with >1 model ID
	Duplicates     []VersionDuplicateGroup `json:"duplicates"`
}

// DotFormAuditEntry records a single model whose Version was newly populated via
// dot-form (N-M → N.M) recognition in ParseFamilyDetailed. Written to
// .bestiary-gen-cache/dot_form_audit.json so the regen delta is explicitly
// reviewable (embrace + audit-list).
type DotFormAuditEntry struct {
	ModelID  string `json:"model_id"`
	Provider string `json:"provider"`
	Version  string `json:"version"`
}

// DotFormAuditEnvelope is the top-level JSON structure written to
// .bestiary-gen-cache/dot_form_audit.json.
type DotFormAuditEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Count         int                 `json:"count"`
	Entries       []DotFormAuditEntry `json:"entries"`
}

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
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Family           string           `json:"family"`
	Reasoning        bool             `json:"reasoning"`
	ToolCall         bool             `json:"tool_call"`
	Attachment       bool             `json:"attachment"`
	Temperature      bool             `json:"temperature"`
	StructuredOutput bool             `json:"structured_output"`
	Interleaved      json.RawMessage  `json:"interleaved"`
	OpenWeights      bool             `json:"open_weights"`
	ReleaseDate      string           `json:"release_date"`
	Knowledge        string           `json:"knowledge"`
	Cost             *genWireCost     `json:"cost"`
	Limit            *genWireLimit    `json:"limit"`
	Modalities       *genWireModality `json:"modalities"`
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
			"-cache-dir value must not be empty\n"+
				"  What: -cache-dir was explicitly set to an empty string\n"+
				"  Why: an empty cache dir resolves to the current working directory, which is unintended\n"+
				"  Where: bestiary-gen flag parsing\n"+
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

	// Fail loudly on bad lineage curation (IP-4) BEFORE generating anything: an
	// unknown parent base family or a malformed entry is a curation bug that must
	// be caught at codegen, not silently degraded to "no lineage" at runtime.
	if err := bestiary.ValidateLineageTable(); err != nil {
		return fmt.Errorf("validate curated lineage table: %w", err)
	}

	rawJSON, models, providerMeta, parseFailures, err := fetchModelsWithRaw(ctx, flags.cacheDir, flags.noFetch)
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
	familyMeta := collectFamilies(models)

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

	// Write parse_failures.json to the cache directory.
	// Sort failures for stable output (parser output order is non-deterministic
	// due to map iteration order in the API response). Stable order means
	// consecutive codegen runs produce identical files when the data is unchanged.
	sort.Slice(parseFailures, func(i, j int) bool {
		li := string(parseFailures[i].Provider) + "/" + string(parseFailures[i].RawID)
		lj := string(parseFailures[j].Provider) + "/" + string(parseFailures[j].RawID)
		return li < lj
	})
	if err := writeParseFailures(flags.cacheDir, parseFailures); err != nil {
		// Non-fatal: log and continue. Failures file is a diagnostic aid;
		// a write error should not prevent the generated .go files from being used.
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write parse_failures.json: %v\n", err)
	}

	// Write version_duplicates.json — work-list for the future duplicate collapse.
	// Recognises models that share (provider, family, variant, version) but differ
	// in model ID. Recognition only; duplicates remain two separate constants.
	if err := writeVersionDuplicates(flags.cacheDir, models); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write version_duplicates.json: %v\n", err)
	}

	// Write dot_form_audit.json — models whose Version is populated via dot-form
	// (N-M ≡ N.M) recognition. The list makes the regen delta reviewable.
	if err := writeDotFormAudit(flags.cacheDir, models); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write dot_form_audit.json: %v\n", err)
	}

	// NON-GATING smoke check: log per-reason failure counts to stdout.
	// This is diagnostic only (by design: not a ==0 gate).
	logPerReasonCounts(parseFailures)

	fmt.Fprintf(os.Stdout,
		"bestiary-gen: wrote %s with %d models (%d providers), %s with %d constants, %s with %d constants, %s at %s; %d parse failures logged to %s\n",
		outputPath, len(filtered), countUniqueProviders(filtered),
		outputProvidersPath, len(allSlugs),
		outputFamiliesPath, len(familyMeta),
		outputConstantsPath,
		now,
		len(parseFailures),
		filepath.Join(flags.cacheDir, "parse_failures.json"),
	)
	return nil
}

// logPerReasonCounts logs a per-reason breakdown of parse failures to stdout.
// This is a NON-GATING smoke check — it never fails the codegen run regardless
// of the counts (by design: not a ==0 gate).
func logPerReasonCounts(failures []bestiary.ParseFailure) {
	counts := make(map[bestiary.ParseFailureReason]int)
	for _, f := range failures {
		counts[f.Reason]++
	}
	if len(counts) == 0 {
		fmt.Fprintln(os.Stdout, "bestiary-gen: parse-failure smoke check: 0 failures (all reasons)")
		return
	}
	// Collect and sort reason keys for deterministic output.
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, string(r))
	}
	sort.Strings(reasons)
	fmt.Fprintln(os.Stdout, "bestiary-gen: parse-failure smoke check (non-gating):")
	for _, r := range reasons {
		fmt.Fprintf(os.Stdout, "  %s: %d\n", r, counts[bestiary.ParseFailureReason(r)])
	}
}

// writeVersionDuplicates identifies models that share (provider, family, variant,
// version) but differ in model ID, and writes the result to
// {cacheDir}/version_duplicates.json. Only groups with version != "" are
// considered (models without version don't have a meaningful duplicate key).
//
// This is recognition-only: duplicates remain two separate constants. The file
// is the ready-made work-list for the future duplicate collapse.
func writeVersionDuplicates(cacheDir string, models []bestiary.ModelInfo) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf(
			"writeVersionDuplicates: create cache dir %q: %w\n"+
				"  How to fix: ensure the cache directory path is writable",
			cacheDir, err,
		)
	}

	// Build a map: key → []model_id, sorted for determinism.
	type groupKey struct {
		provider string
		family   string
		variant  string
		version  string
	}
	groups := make(map[groupKey][]string)
	for _, m := range models {
		if m.Version == "" {
			continue // no version → no meaningful duplicate key
		}
		k := groupKey{
			provider: string(m.Provider),
			family:   string(m.Family),
			variant:  m.Variant,
			version:  m.Version,
		}
		groups[k] = append(groups[k], string(m.ID))
	}

	// Collect groups with more than one model ID (actual duplicates).
	duplicates := make([]VersionDuplicateGroup, 0)
	for k, ids := range groups {
		if len(ids) <= 1 {
			continue
		}
		sort.Strings(ids)
		duplicates = append(duplicates, VersionDuplicateGroup{
			Key: VersionDuplicateKey{
				Provider: k.provider,
				Family:   k.family,
				Variant:  k.variant,
				Version:  k.version,
			},
			ModelIDs: ids,
		})
	}
	// Sort by (provider, family, variant, version) for deterministic output.
	sort.Slice(duplicates, func(i, j int) bool {
		ki := duplicates[i].Key
		kj := duplicates[j].Key
		if ki.Provider != kj.Provider {
			return ki.Provider < kj.Provider
		}
		if ki.Family != kj.Family {
			return ki.Family < kj.Family
		}
		if ki.Variant != kj.Variant {
			return ki.Variant < kj.Variant
		}
		return ki.Version < kj.Version
	})

	envelope := VersionDuplicatesEnvelope{
		SchemaVersion:  1,
		GeneratedAt:    time.Now().UTC(),
		DuplicateCount: len(duplicates),
		Duplicates:     duplicates,
	}
	if envelope.Duplicates == nil {
		envelope.Duplicates = []VersionDuplicateGroup{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("writeVersionDuplicates: marshal JSON: %w", err)
	}

	dst := filepath.Join(cacheDir, versionDuplicatesFile)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeVersionDuplicates: write %s: %w\n"+
				"  How to fix: ensure %s is writable",
			dst, err, cacheDir,
		)
	}
	return nil
}

// writeDotFormAudit collects all models whose Version contains a dot ("."),
// indicating that the version was populated via dot-form (N-M ≡ N.M) recognition
// in ParseFamilyDetailed. The list makes the regen delta explicitly reviewable.
// Written to {cacheDir}/dot_form_audit.json.
func writeDotFormAudit(cacheDir string, models []bestiary.ModelInfo) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf(
			"writeDotFormAudit: create cache dir %q: %w\n"+
				"  How to fix: ensure the cache directory path is writable",
			cacheDir, err,
		)
	}

	entries := make([]DotFormAuditEntry, 0)
	for _, m := range models {
		if strings.Contains(m.Version, ".") {
			entries = append(entries, DotFormAuditEntry{
				ModelID:  string(m.ID),
				Provider: string(m.Provider),
				Version:  m.Version,
			})
		}
	}
	// Sort by (provider, model_id) for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Provider != entries[j].Provider {
			return entries[i].Provider < entries[j].Provider
		}
		return entries[i].ModelID < entries[j].ModelID
	})

	envelope := DotFormAuditEnvelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		Count:         len(entries),
		Entries:       entries,
	}
	if envelope.Entries == nil {
		envelope.Entries = []DotFormAuditEntry{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("writeDotFormAudit: marshal JSON: %w", err)
	}

	dst := filepath.Join(cacheDir, dotFormAuditFile)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeDotFormAudit: write %s: %w\n"+
				"  How to fix: ensure %s is writable",
			dst, err, cacheDir,
		)
	}
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

// writeParseFailures marshals the given failures into a ParseFailuresEnvelope
// and writes it to {cacheDir}/parse_failures.json. The file is overwritten on
// every codegen run (full audit, not append). An empty failures slice produces a
// valid JSON envelope with failure_count=0 and failures=[].
//
// Per [C-actionable-errors]: the error message describes what failed, why, where
// the file lives, and how to recover.
func writeParseFailures(cacheDir string, failures []bestiary.ParseFailure) error {
	// Ensure the cache directory exists.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf(
			"writeParseFailures: create cache dir %q: %w\n"+
				"  What: could not create the cache directory for parse_failures.json\n"+
				"  Why: file system permission or path issue\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the parent directory exists and is writable, or use --cache-dir to choose a different location",
			cacheDir, err, cacheDir,
		)
	}

	// Use an empty (non-nil) slice so JSON encodes failures as [] not null.
	safeFailures := failures
	if safeFailures == nil {
		safeFailures = []bestiary.ParseFailure{}
	}

	envelope := bestiary.ParseFailuresEnvelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		FailureCount:  len(safeFailures),
		Failures:      safeFailures,
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf(
			"writeParseFailures: marshal JSON: %w\n"+
				"  What: could not serialize the parse failures envelope to JSON\n"+
				"  Why: the ParseFailuresEnvelope or its contents may contain non-serializable values\n"+
				"  Where: in-memory marshal step before writing to %s\n"+
				"  How to fix: inspect the ParseFailure records for unusual field values",
			err, filepath.Join(cacheDir, "parse_failures.json"),
		)
	}

	dst := filepath.Join(cacheDir, "parse_failures.json")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeParseFailures: write %s: %w\n"+
				"  What: could not write parse_failures.json to disk\n"+
				"  Why: file system permission or path issue\n"+
				"  Where: %s\n"+
				"  How to fix: ensure %s is writable, or use --cache-dir to choose a different location",
			dst, err, dst, cacheDir,
		)
	}
	return nil
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
// Returns the raw JSON body, the flat model slice, per-provider metadata, and
// any parse failures detected during model conversion via genToModelInfoDetailed.
// Parse failures are non-fatal — the model is still included in the output.
func fetchModelsWithRaw(ctx context.Context, dir string, noFetch bool) (rawJSON []byte, models []bestiary.ModelInfo, provMeta map[string]providerAPIMeta, failures []bestiary.ParseFailure, err error) {
	cachePath := filepath.Join(dir, cacheFile)

	if noFetch {
		// Load from cache; no network call.
		body, readErr := os.ReadFile(cachePath)
		if readErr != nil || len(body) == 0 {
			absPath, _ := filepath.Abs(cachePath)
			if absPath == "" {
				absPath = cachePath
			}
			return nil, nil, nil, nil, &ErrCacheMiss{Path: absPath}
		}
		rawJSON = body
	} else {
		// Fetch from the API over HTTP.
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
		if reqErr != nil {
			return nil, nil, nil, nil, fmt.Errorf(
				"create HTTP request for %s: %w\n"+
					"  What: failed to construct the API request\n"+
					"  How to fix: this is a programming error — report it",
				apiURL, reqErr,
			)
		}

		client := &http.Client{Timeout: 60 * time.Second}
		resp, doErr := client.Do(req)
		if doErr != nil {
			return nil, nil, nil, nil, fmt.Errorf(
				"HTTP GET %s: %w\n"+
					"  What: network request failed\n"+
					"  How to fix: check network connectivity and retry",
				apiURL, doErr,
			)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, nil, nil, nil, fmt.Errorf(
				"unexpected HTTP status %d from %s; expected 200 OK\n"+
					"  What: the API returned a non-success status\n"+
					"  How to fix: check the API endpoint and try again",
				resp.StatusCode, apiURL,
			)
		}

		const maxBodyBytes = 10 * 1024 * 1024
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if readErr != nil {
			return nil, nil, nil, nil, fmt.Errorf(
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
		return nil, nil, nil, nil, fmt.Errorf(
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
			info, failure := genToModelInfoDetailed(providerSlug, wm)
			models = append(models, info)
			if failure != nil {
				failures = append(failures, *failure)
			}
		}
	}

	// Determinism: sort the assembled model set by (Provider, ID) exactly once so
	// every downstream consumer observes a stable order regardless of API map-
	// iteration order.
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].ID < models[j].ID
	})

	return rawJSON, models, provMeta, failures, nil
}

// collectFamilies returns a deduplicated sorted list of unique non-empty raw API
// family values found across all models, together with a name hint for casing.
func collectFamilies(models []bestiary.ModelInfo) []string {
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

// genToModelInfoDetailed converts a genWireModel to (ModelInfo, *ParseFailure).
// The *ParseFailure is non-nil when ParseFamilyDetailed detects a known parsing
// deficiency (see bestiary.ParseFamilyDetailed for the three detected modes).
// Failure records are collected by fetchModelsWithRaw and written to
// parse_failures.json at the end of each codegen run.
func genToModelInfoDetailed(providerSlug string, wm genWireModel) (bestiary.ModelInfo, *bestiary.ParseFailure) {
	// Derive normalized family, variant, and version.
	rawFamily := bestiary.Family(wm.Family)
	id := bestiary.ModelID(wm.ID)
	provider := bestiary.Provider(providerSlug)

	// Single-ownership: consume the full 5-tuple from ParseFamilyDetailed.
	// ParseFamilyDetailed(raw="") delegates to InferFamilyFromIDWithVariant +
	// ExtractModifier, covering the empty-family case.
	// This is byte-equivalent to the former two-branch structure; the
	// decomposition snapshot test (TestDecompositionSnapshot) guards correctness.
	//
	// (family, variant, version, modifier) all come from ParseFamilyDetailed.
	// Codegen no longer calls ExtractVersionFromID directly (single-ownership).
	normFamily, normVariant, normVersion, normModifier, failure := bestiary.ParseFamilyDetailed(rawFamily, id, provider)

	// Serving-host attribute (IP-2). DetectHost surfaces a curated host prefix
	// (e.g. "azure-gpt-4o" → HostAzure) as a per-instance attribute; the same
	// strip is applied inside ParseFamilyDetailed so the decomposition above is
	// already host-independent. The full catalog ID is retained as info.ID below
	// — Host records the backend without mutating the record's identity.
	host, _ := bestiary.DetectHost(id)

	// Compute cleanID (modifier-stripped) for ExtractDate. The modifier consumed
	// value is a trailing suffix of the model ID; strip it to avoid date extraction
	// from tokens that are part of the modifier (e.g. "thinking", "preview").
	cleanID := id
	if len(normModifier) > 0 {
		// Modifier is now a LIST. Peel EVERY trailing modifier token (their
		// consumed suffixes are contiguous at the tail of the ID) so ExtractDate never
		// reads a date out of a modifier token (e.g. "...-thinking-turbo").
		for {
			_, modifierConsumed := bestiary.ExtractModifier(cleanID, normFamily, normVariant)
			if modifierConsumed == "" {
				break
			}
			trimmed, ok := strings.CutSuffix(string(cleanID), modifierConsumed)
			if !ok {
				break
			}
			cleanID = bestiary.ModelID(trimmed)
		}
	}

	// Derive normalized date from cleaned model ID (modifier stripped) or release date.
	normDate := bestiary.ExtractDate(cleanID, wm.ReleaseDate)

	// If a parse failure was detected, backfill the date into AttemptedParse.
	if failure != nil {
		failure.AttemptedParse.Date = normDate
		// Models where ExtractModifier extracts a known modifier no longer trip
		// ReasonKnownSuffixOverflow (the modifier is now a first-class field).
		// Clear the failure record for this case so the audit log shrinks as
		// expected per the V2-5 design.
		if failure.Reason == bestiary.ReasonKnownSuffixOverflow && len(normModifier) > 0 {
			failure = nil
		}
	}

	info := bestiary.ModelInfo{
		ID:               id,
		Provider:         provider,
		DisplayName:      wm.Name,
		RawFamily:        rawFamily,
		Host:             host,
		Family:           normFamily,
		Variant:          normVariant,
		Version:          normVersion,
		Date:             normDate,
		Modifier:         normModifier,
		Reasoning:        wm.Reasoning,
		ToolCall:         wm.ToolCall,
		Attachment:       wm.Attachment,
		Temperature:      wm.Temperature,
		StructuredOutput: wm.StructuredOutput,
		OpenWeights:      wm.OpenWeights,
		ReleaseDate:      wm.ReleaseDate,
		Knowledge:        wm.Knowledge,
		Interleaved:      parseCapabilityRaw(wm.Interleaved),
		LastSynced:       "",
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

	// Lineage (IP-4). Populate the derivation edges from the curated lineage
	// ledger (parse/data/lineage.json) for any catalog record whose ID matches a
	// curated child key. The ledger — not raw_family — is the authoritative
	// lineage source; nil (no edge) for the overwhelming majority of base models.
	info.Lineage = bestiary.LineageFor(id)

	return info, failure
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
// goStringSliceLiteral renders a []string as a compile-ready Go literal. A nil/empty
// slice renders as "nil" (the canonical "no modifiers" value), matching the
// empty→nil contract of ModelInfo.Modifier. Elements are emitted verbatim
// in their stored canonical order so codegen output is byte-stable.
func goStringSliceLiteral(ss []string) string {
	if len(ss) == 0 {
		return "nil"
	}
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

// derivationKindExpr renders a DerivationKind as its exported constant name so
// the generated source references the enum symbolically (e.g. DerivationFinetune)
// rather than by integer value. An out-of-range value (never produced by the
// curated table) falls back to DerivationNone defensively.
func derivationKindExpr(k bestiary.DerivationKind) string {
	switch k {
	case bestiary.DerivationFinetune:
		return "DerivationFinetune"
	case bestiary.DerivationMerge:
		return "DerivationMerge"
	case bestiary.DerivationDistillation:
		return "DerivationDistillation"
	case bestiary.DerivationQuantized:
		return "DerivationQuantized"
	case bestiary.DerivationAdapter:
		return "DerivationAdapter"
	default:
		return "DerivationNone"
	}
}

// lineageLiteral renders a []LineageEdge as a Go composite literal for the
// generated source, mirroring goStringSliceLiteral's empty→"nil" contract so the
// base-model majority emits a bare nil. The generated file is package bestiary,
// so LineageEdge / EntityRef / DerivationKind constants are referenced unqualified.
func lineageLiteral(edges []bestiary.LineageEdge) string {
	if len(edges) == 0 {
		return "nil"
	}
	parts := make([]string, len(edges))
	for i, e := range edges {
		parts[i] = fmt.Sprintf(
			"{Parent: EntityRef{Family: %q, Variant: %q, Version: %q, Modifier: %s}, Kind: %s}",
			string(e.Parent.Family), e.Parent.Variant, e.Parent.Version,
			goStringSliceLiteral(e.Parent.Modifier), derivationKindExpr(e.Kind),
		)
	}
	return "[]LineageEdge{" + strings.Join(parts, ", ") + "}"
}

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
		fmt.Fprintf(&buf, "\t\tModifier:              %s,\n", goStringSliceLiteral(m.Modifier))
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
		fmt.Fprintf(&buf, "\t\tHost:                  %q,\n", string(m.Host))
		fmt.Fprintf(&buf, "\t\tLineage:               %s,\n", lineageLiteral(m.Lineage))
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
// Model_ constants generation
// --------------------------------------------------------------------------

// outputConstantsPath is the file that generateConstantsSource writes.
const outputConstantsPath = "models_constants_gen.go"

// tokenToConstPart converts a single hyphen/dot-split token from a model ID into
// a constant-name segment via the shared styleSegment seam. Rules (in order):
//  1. curated brandCasing wins for the full token (e.g. "gpt" → "GPT", "deepseek" → "DeepSeek").
//  2. Digit-leading token: keep digit prefix verbatim; brandCasing or VERBATIM alpha suffix
//     ("4o" stays "4o", not "4O"/"4_o" — within-component characters preserved).
//  3. Otherwise: title-case. A multi-token value (e.g. the modifier "deep-research" passed
//     whole) is split on any non-alphanumeric separator, each sub-token styled, joined with
//     a single within-segment underscore ("deep-research" → "Deep_Research").
func tokenToConstPart(tok string) string {
	if tok == "" {
		return ""
	}
	lower := strings.ToLower(tok)

	// 1. Curated brand-casing for the FULL token (before any compound split).
	if s, ok := brandCasing[lower]; ok {
		return s
	}

	// 3. Compound (internal separator) → split, style each sub via the shared seam
	// (preserveDigitSuffix=true: Model__ segment rule), join with "_".
	subs := strings.FieldsFunc(tok, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(subs) > 1 {
		for i, s := range subs {
			styled, _ := styleSegment(s, true)
			subs[i] = styled
		}
		return strings.Join(subs, "_")
	}

	// 1+2. Single token: shared seam (brand → digit-leading [verbatim suffix] → title-case).
	styled, _ := styleSegment(tok, true)
	return styled
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

	// Strip modifier trailing token from raw ID before date/version extraction.
	// Modifier (e.g. "-thinking") appears as the trailing hyphen-separated token.
	// It must be stripped before date and version logic runs so it doesn't produce
	// spurious tokens in the constant name.
	// Modifier is a LIST. Greedily strip EVERY trailing modifier token from
	// the raw ID (they may appear in any order in the ID), then build the constant
	// segment from the stored CANONICAL order so the identifier is deterministic and
	// byte-stable (e.g. ["vision","instruct"] → "VisionInstruct").
	modifierSegment := ""
	if len(m.Modifier) > 0 {
		modSet := make(map[string]struct{}, len(m.Modifier))
		for _, mod := range m.Modifier {
			modSet[mod] = struct{}{}
		}
		// Peel trailing "-<modifier>" tokens until the tail is no longer a modifier.
		for {
			lastDash := strings.LastIndexByte(rawID, '-')
			if lastDash < 0 {
				break
			}
			tail := rawID[lastDash+1:]
			if _, ok := modSet[strings.ToLower(tail)]; !ok {
				break
			}
			rawID = strings.TrimRight(rawID[:lastDash], "-.")
		}
		// Cased segment in canonical order (m.Modifier is already canonical).
		for _, mod := range m.Modifier {
			modifierSegment += tokenToConstPart(mod)
		}
	}

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
	// Insert modifier segment between version and date.
	if modifierSegment != "" {
		name += "__" + modifierSegment
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
			// (b) Stable ordinal: order colliders by raw model ID so the _N binding is
			// reproducible regardless of slice order. (Belt-and-suspenders with the
			// deterministic (Provider,ID) model ordering's sort.)
			type member struct {
				pos   int
				rawID string
			}
			ms := make([]member, len(positions))
			for k, pos := range positions {
				ms[k] = member{pos, string(models[pos].ID)}
			}
			sort.Slice(ms, func(i, j int) bool { return ms[i].rawID < ms[j].rawID })
			for idx, m := range ms {
				result[m.pos] = baseName + "_" + strconv.Itoa(idx+1)
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
		// Stable ordinal: order by raw model ID so the _N binding is reproducible.
		type member struct {
			pos   int
			rawID string
		}
		ms := make([]member, len(positions))
		for k, pos := range positions {
			ms[k] = member{pos, string(models[pos].ID)}
		}
		sort.Slice(ms, func(i, j int) bool { return ms[i].rawID < ms[j].rawID })
		// Append _<n> to break the tie.
		for idx, m := range ms {
			result[m.pos] = result[m.pos] + "_" + strconv.Itoa(idx+1)
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
	buf.WriteString("// pipeline. The name diverges from the original spec (Models() []ModelID) to\n")
	buf.WriteString("// avoid clashing with registry.go:Models() []ModelInfo.\n")
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
