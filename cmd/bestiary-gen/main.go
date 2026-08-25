// bestiary-gen consumes the models.dev catalog and writes five generated files
// into the bestiary package root:
//   - models_static_gen.go    — all model records (api.json / providers view)
//   - providers_gen.go        — one Provider constant per API slug + knownProviders
//   - families_gen.go         — one Family constant per unique API family value
//   - models_constants_gen.go — one Model_* constant per eligible (ID, Provider) pair
//   - models_metadata_gen.go  — baked EntityMetadata (models.json view), populated
//     into bakedEntityMetadata via init()
//
// Input: the committed catalog.json snapshot at parse/data/modelsdev/catalog.json
// (the models.dev catalog.json artifact — both the providers and models views from a
// single upstream deploy). Two modes:
//   - --no-fetch (the deterministic default for `go generate`): reads the committed
//     catalog.json. A missing or corrupt snapshot is a LOUD actionable error — codegen
//     NEVER degrades to an empty catalog.
//   - fetch (manual snapshot refresh): a single polite GET of the live catalog.json,
//     which REWRITES the committed catalog.json + SNAPSHOT.json manifest, then regens.
//
// Decoding is delegated to the library's ParseCatalogJSON (the single wire-decode path
// shared with the runtime client) so the generator never carries a second copy of the
// models.dev wire schema; the generator owns only the post-decode decomposition and the
// codegen emission.
//
// Run the deterministic regen via: go generate ./...
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
	"regexp"
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
	versionDuplicatesFile = "version_duplicates.json"
	dotFormAuditFile      = "dot_form_audit.json"
)

// codegenUserAgent is the descriptive User-Agent the fetch mode sends on its single
// polite request to the live models.dev catalog (the polite-bot precedent from
// cmd/bestiary-ollama). It identifies the tool and its purpose so upstream can
// attribute the request.
const codegenUserAgent = "bestiary-gen/0.2.5 (+https://github.com/dayvidpham/bestiary; models.dev snapshot refresh)"

// modelsdevUnlinkedFile is the codegen-emitted disagreement report: metadata ids whose
// decomposed family IS present among the catalog entities but whose full tuple matched
// no entity (a curator resolves each with a modelsdev_aliases.json entry). It mirrors
// the parse_failures.json / ollama_unlinked.json report precedent and lives beside the
// alias table it feeds.
const modelsdevUnlinkedFile = "parse/data/modelsdev_unlinked.json"

// creatorProvidersUnservedFile is the committed coverage report of curated
// Creator→Provider pairs that serve no instance of any of that creator's families.
const creatorProvidersUnservedFile = "parse/data/creator_providers_unserved.json"

// creatorsLabDisagreementsFile is the committed report of families whose models.dev
// lab evidence was NOT auto-applied to the Family→Creator dimension.
const creatorsLabDisagreementsFile = "parse/data/creators_lab_disagreements.json"

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

// catalogURL is the live models.dev catalog.json endpoint the fetch mode GETs.
// Declared as a var (not const) so tests can override it to point at an
// httptest.Server without build tags or dual code paths.
var catalogURL = "https://models.dev/catalog.json"

// vendoredCatalogPath is the committed codegen input: the models.dev catalog.json
// snapshot. --no-fetch reads it; fetch mode rewrites it. Declared as a var so the
// loud-error tests can point it at a controlled temp path. Relative to the module
// root (where go generate runs).
var vendoredCatalogPath = "parse/data/modelsdev/catalog.json"

// snapshotManifestPath is the committed provenance sidecar for the vendored catalog.
// It is informational only — it is NEVER parsed into generated output — and is
// rewritten alongside the catalog on a live fetch. Declared as a var so tests can
// redirect it.
var snapshotManifestPath = "parse/data/modelsdev/SNAPSHOT.json"

// snapshotManifest is the on-disk shape of parse/data/modelsdev/SNAPSHOT.json. It
// records the provenance of the vendored catalog.json snapshot. FetchedAt is the
// real wall-clock fetch time (written ONLY on a live refresh, never on a --no-fetch
// regen, so it does not perturb deterministic regeneration). UpstreamHeadSHA is
// best-effort: it is left empty when it cannot be determined from the fetch (the
// catalog.json artifact does not carry the models.dev repo HEAD).
type snapshotManifest struct {
	Comment         string `json:"_comment"`
	Artifact        string `json:"artifact"`
	FetchedAt       string `json:"fetched_at"`
	ETag            string `json:"etag"`
	UpstreamHeadSHA string `json:"upstream_head_sha"`
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

// validateCuratedDataSourceTable is the codegen data-source FK guard, indirected
// through a package var so the run() abort-on-bad-curation wiring is falsifiable in
// a test (swap it for a failing stub and assert run() returns the wrapped error)
// without mutating the embedded datasources.json. Production always uses the real
// guard.
var validateCuratedDataSourceTable = bestiary.ValidateDataSourceTable

// codegenLastSynced returns the DETERMINISTIC timestamp stamped onto every generated
// ModelInfo.LastSynced: the CURRENT (maximum) models.dev ingest instant from the
// COMMITTED parse/data/datasources.json (DatasetIngestedFor(DataSourceModelsDev)), NOT a
// wall-clock. Pinning the codegen stamp to a committed snapshot instant is what makes a
// `go run ./cmd/bestiary-gen --no-fetch` regen byte-deterministic: the wall-clock stamp
// was previously the sole residual non-determinism in the generated output, so with it
// pinned TestCodegen_Reproducible_ByteIdentical asserts FULL byte-identity.
//
// This is CODEGEN-ONLY. The runtime `sync` path deliberately keeps a real UTC wall-clock
// (correct precisely because a sync is a real event); being later than this committed
// instant, a synced row consistently wins the store's most-recent-wins merge over a baked
// static row (see the stamp site in run()).
//
// A missing models.dev ingest row is a curation bug, so this is a LOUD actionable error at
// codegen (the codegen-loud / runtime-graceful discipline), never a silent empty stamp.
func codegenLastSynced() (string, error) {
	ingest, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceModelsDev)
	if !ok || ingest.IngestedAt == "" {
		return "", fmt.Errorf(
			"codegen LastSynced: no models.dev ingest instant found\n"+
				"  What: DatasetIngestedFor(%q) returned no current ingest\n"+
				"  Why: parse/data/datasources.json has no `ingested` row for source_id %q (or its ingested_at is empty)\n"+
				"  Where: cmd/bestiary-gen codegenLastSynced (the deterministic LastSynced stamp source)\n"+
				"  How to fix: add a models.dev ingest row to parse/data/datasources.json per the models.dev snapshot-refresh workflow",
			bestiary.DataSourceModelsDev, bestiary.DataSourceModelsDev)
	}
	return ingest.IngestedAt, nil
}

func run(args []string) error {
	flags, err := parseFlags(args)
	if err != nil {
		return err
	}

	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	// Fail loudly on bad lineage curation BEFORE generating anything: an
	// unknown parent base family or a malformed entry is a curation bug that must
	// be caught at codegen, not silently degraded to "no lineage" at runtime.
	if err := bestiary.ValidateLineageTable(); err != nil {
		return fmt.Errorf("validate curated lineage table: %w", err)
	}

	// Fail loudly on bad quant/VRAM curation BEFORE generating anything: an unknown
	// quant token, zero weights_bytes, duplicate model_id, invalid param_size, or
	// unknown source is a curation bug that must abort codegen rather than silently
	// baking wrong VRAM estimates or provenance into the generated static data.
	if err := bestiary.ValidateQuantVRAMTable(); err != nil {
		return fmt.Errorf("validate curated quant-VRAM table: %w", err)
	}

	// Fail loudly on a non-canonical param-size pin BEFORE generating anything: a
	// typo'd size token in param_size_overrides.json would otherwise flow verbatim
	// into #size entity-key material. The runtime loader degrades gracefully; this
	// codegen-time check fences the pin file.
	if err := bestiary.ValidateParamSizePins(); err != nil {
		return fmt.Errorf("validate curated param-size pins: %w", err)
	}

	// Fail loudly on bad redundant-modifier suppression curation BEFORE generating
	// anything: an unknown family, a missing rationale, or a modifier the entity does
	// not carry would otherwise degrade at runtime to "no suppression", silently
	// reverting the curated naming policy instead of reporting the bad entry. (The
	// entity-relative existence/collision checks need the built entity set and run
	// later, once the models are decomposed — see ValidateSuppression.)
	if err := bestiary.ValidateSuppressionSeed(); err != nil {
		return fmt.Errorf("validate curated suppression seed: %w", err)
	}

	// Fail loudly on bad data-source curation BEFORE generating anything: a duplicate
	// source id/uri, an ingest source_id absent from the dimension, or an entity↔source
	// attestation naming a source absent from the curated datasources.json is a curation
	// bug that must abort codegen rather than baking an orphan provenance row. (The
	// sibling entity-key FK is NOT guarded here: the entity↔source relation is derived
	// from the registry, so that key check is tautological at codegen — see
	// ValidateEntitySourceTable.)
	if err := validateCuratedDataSourceTable(); err != nil {
		return fmt.Errorf("validate curated data-source table: %w", err)
	}

	// Fail loudly on bad creator curation BEFORE generating anything: an unknown
	// family, a duplicate family (Family → Creator must be a function), or an empty
	// creator in creators.json is a curation bug that must abort codegen rather than
	// silently baking a wrong or missing Creator projection. The runtime loader
	// degrades gracefully to CreatorNone; this codegen-time check fences the seed so
	// the baked ModelInfo.Creator and the persisted store creators dimension agree.
	if err := bestiary.ValidateCreatorTable(); err != nil {
		return fmt.Errorf("validate curated creator table: %w", err)
	}

	// Same discipline for the Creator→[]Provider distribution relation: an unknown
	// creator, an unknown provider slug, a duplicate creator row, an empty provider
	// list, or a provider repeated within a row is curation that can never match a
	// served instance, so creator-first selection would silently never fire for it.
	// Fail here rather than emitting a report that quietly lists the whole row as
	// serving nothing.
	if err := bestiary.ValidateCreatorProviderTable(); err != nil {
		return fmt.Errorf("validate curated creator-provider table: %w", err)
	}

	// Fail loudly on a bad harvested HuggingFace seed BEFORE generating anything: an
	// empty/non-org-repo value, a source_url that is not the live Hub URL for the
	// value (the case-preservation cross-check), an unknown resolves_to family, or a
	// duplicate in huggingface_nomina.json is a bot/curation bug that must abort
	// codegen rather than baking a case-mangled or orphan HF naming. The runtime
	// loader degrades gracefully to "no harvested nomina"; this fences the seed.
	if err := bestiary.ValidateHFNomina(); err != nil {
		return fmt.Errorf("validate harvested huggingface seed: %w", err)
	}

	rawJSON, models, metadata, providerMeta, parseFailures, err := fetchModelsWithRaw(ctx, flags.noFetch)
	if err != nil {
		return err
	}

	// On a live fetch, REWRITE the committed catalog.json snapshot + its SNAPSHOT.json
	// manifest so the vendored input matches the just-fetched deploy. This is the only
	// place the vendored files are mutated; --no-fetch never touches them, so a
	// deterministic regen leaves them byte-identical. A rewrite failure is fatal: a
	// stale-but-committed catalog paired with freshly-generated code would silently
	// diverge, so we refuse to proceed.
	if !flags.noFetch {
		if wErr := rewriteVendoredSnapshot(rawJSON, lastFetchMeta, now); wErr != nil {
			return wErr
		}
	}

	// Stamp the DETERMINISTIC codegen LastSynced on every model — the current models.dev
	// ingest instant from the committed datasources.json (see codegenLastSynced), NOT the
	// wall-clock `now`. `now` remains the real fetch time for the SNAPSHOT.json provenance
	// rewrite above and the stdout summary; only the baked LastSynced is pinned to the
	// committed instant, which is what makes a --no-fetch regen byte-deterministic.
	//
	// Merge tie-break implication: every baked static row now carries this STABLE committed
	// timestamp instead of a per-regen wall-clock. The runtime `sync` path stamps a real UTC
	// wall-clock (a sync is a real event), which is later than this committed instant, so the
	// store's most-recent-wins merge (MergeModels: on an (ID, Provider) collision the higher
	// LastSynced wins) CONSISTENTLY prefers a fresher synced row over the baked static row —
	// no longer depending on whenever the generated files were last regenerated.
	lastSynced, err := codegenLastSynced()
	if err != nil {
		return err
	}
	for i := range models {
		models[i].LastSynced = lastSynced
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
			flags.only, len(models), catalogURL,
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

	// Generate entities_constants_gen.go — one Entity__ constant per model ENTITY
	// (provider-agnostic canonical key), derived through the shared MintNomina path over
	// the full (unfiltered) model set. Uses the full set so every entity is covered
	// regardless of the --only/--except filter. A name collision fails the bake loudly
	// (injectivity guard).
	entitiesConstSrc, err := generateEntitiesConstantsSource(models, metadata)
	if err != nil {
		return fmt.Errorf("generate entity constants source: %w", err)
	}
	if err := writeFile(outputEntitiesConstantsPath, entitiesConstSrc); err != nil {
		return err
	}

	// Generate models_metadata_gen.go — the baked models.dev entity-metadata catalog
	// (models.json view). It populates bakedEntityMetadata via init(); the emission
	// sets Source=DataSourceModelsDev and LastSynced="" and orders rows by an explicit
	// sort on MetadataID (never the first-seen aggregate order).
	metadataSrc, err := generateMetadataSource(metadata)
	if err != nil {
		return fmt.Errorf("generate metadata source: %w", err)
	}
	if err := writeFile(outputMetadataPath, metadataSrc); err != nil {
		return err
	}

	// Emit modelsdev_unlinked.json — the join-disagreement report. It runs the
	// metadata↔entity join over the freshly decomposed entity set (built from the
	// models just generated, NOT the compiled-in registry) so the report reflects
	// THIS run's data. A write failure is non-fatal (diagnostic aid, parse_failures
	// precedent); it never blocks the generated .go files.
	if err := writeModelsdevUnlinked(models, metadata); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write %s: %v\n", modelsdevUnlinkedFile, err)
	}

	// Emit the two creator-dimension reports. Both are diagnostic aids on the same
	// non-fatal footing as the unlinked report: a write failure never blocks the
	// generated .go files. The curation they describe was already fenced loudly above
	// by ValidateCreatorTable / ValidateCreatorProviderTable, so a failure here can
	// only be a filesystem problem, never bad data slipping through.
	if err := writeCreatorProvidersUnserved(models); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write %s: %v\n", creatorProvidersUnservedFile, err)
	}
	if err := writeCreatorsLabDisagreements(metadata); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: warning: could not write %s: %v\n", creatorsLabDisagreementsFile, err)
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
		"bestiary-gen: wrote %s with %d models (%d providers), %s with %d constants, %s with %d constants, %s with %d metadata rows, %s at %s; %d parse failures logged to %s\n",
		outputPath, len(filtered), countUniqueProviders(filtered),
		outputProvidersPath, len(allSlugs),
		outputFamiliesPath, len(familyMeta),
		outputMetadataPath, len(metadata),
		outputEntitiesConstantsPath,
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

// fetchMeta captures provenance from the most recent live catalog fetch (currently
// the HTTP ETag) for the SNAPSHOT.json manifest. It is written only by the fetch path
// of fetchModelsWithRaw and read only by run() immediately afterwards; it is
// package-level solely to avoid threading a response-meta value through the shared
// decode pipeline that the direct-call tests also exercise.
type fetchMeta struct {
	etag string
}

// lastFetchMeta holds the fetchMeta of the most recent live fetch. It is meaningful
// only in the fetch branch of run(), immediately after fetchModelsWithRaw returns.
var lastFetchMeta fetchMeta

// rewriteVendoredSnapshot overwrites the committed catalog.json snapshot with the
// just-fetched bytes and rewrites its SNAPSHOT.json manifest. It is invoked ONLY on a
// live fetch (never on --no-fetch), so a deterministic regen never perturbs the
// vendored files. fetchedAt is the RFC3339 fetch time (the run's `now`), recorded as
// committed provenance.
func rewriteVendoredSnapshot(rawJSON []byte, meta fetchMeta, fetchedAt string) error {
	dir := filepath.Dir(vendoredCatalogPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(
			"rewriteVendoredSnapshot: create %q: %w\n"+
				"  What: could not create the vendored snapshot directory\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the module root is writable",
			dir, err, dir,
		)
	}
	if err := os.WriteFile(vendoredCatalogPath, rawJSON, 0o644); err != nil {
		return fmt.Errorf(
			"rewriteVendoredSnapshot: write %q: %w\n"+
				"  What: could not write the vendored catalog.json snapshot\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the module root is writable",
			vendoredCatalogPath, err, vendoredCatalogPath,
		)
	}

	manifest := snapshotManifest{
		Comment: "Provenance sidecar for the committed models.dev catalog.json snapshot. " +
			"Informational only — NEVER parsed into generated output. Rewritten alongside " +
			"catalog.json on a live `go run ./cmd/bestiary-gen` fetch; untouched by --no-fetch " +
			"regen. upstream_head_sha is best-effort (the catalog.json artifact does not carry " +
			"the models.dev repo HEAD); leave empty when not determinable.",
		Artifact:        "catalog.json",
		FetchedAt:       fetchedAt,
		ETag:            meta.etag,
		UpstreamHeadSHA: "",
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("rewriteVendoredSnapshot: marshal SNAPSHOT.json: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(snapshotManifestPath, data, 0o644); err != nil {
		return fmt.Errorf(
			"rewriteVendoredSnapshot: write %q: %w\n"+
				"  What: could not write the SNAPSHOT.json manifest\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the module root is writable",
			snapshotManifestPath, err, snapshotManifestPath,
		)
	}
	return nil
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

// ErrVendoredCatalogMissing is returned by fetchModelsWithRaw when --no-fetch is set
// and the committed catalog.json snapshot is absent or empty. It is the loud-fail
// precedent for the codegen input: codegen NEVER degrades to an empty catalog.
type ErrVendoredCatalogMissing struct {
	Path string // full resolved path that was missing
}

func (e *ErrVendoredCatalogMissing) Error() string {
	return fmt.Sprintf(
		"vendored models.dev catalog.json missing or empty\n"+
			"  What: the committed codegen input is absent or zero-length\n"+
			"  Why: --no-fetch was specified; the live HTTP fetch was skipped\n"+
			"  Where: %s (read in fetchModelsWithRaw during the --no-fetch load step)\n"+
			"  When: codegen input load step\n"+
			"  What it means: bestiary-gen cannot proceed and never degrades to an empty catalog\n"+
			"  How to fix: run the models.dev snapshot refresh — `go run ./cmd/bestiary-gen` "+
			"WITHOUT --no-fetch — to fetch and vendor %s + SNAPSHOT.json, then commit them "+
			"(see the \"models.dev snapshot refresh\" section in AGENTS.md)",
		e.Path, e.Path,
	)
}

// fetchModelsWithRaw loads the models.dev catalog (the committed snapshot when noFetch
// is true, otherwise a single live GET), decodes it through the LIBRARY's
// ParseCatalogJSON — the single wire-decode path — and runs the codegen decomposition
// over the parsed models.
//
//   - noFetch=true: read the committed vendoredCatalogPath. A missing/empty file yields
//     *ErrVendoredCatalogMissing; a corrupt file yields a loud decode error. It never
//     degrades to an empty catalog.
//   - noFetch=false: a single polite GET of catalogURL (descriptive User-Agent). The
//     caller (run) is responsible for rewriting the vendored files from rawJSON.
//
// Returns the raw catalog bytes, the decomposed model slice (sorted by (Provider, ID)),
// the baked metadata rows (models.json view, unsorted here — the metadata emitter
// imposes the MetadataID order), per-provider display-name metadata, and any parse
// failures from the decomposition (non-fatal — the model is still included).
func fetchModelsWithRaw(ctx context.Context, noFetch bool) (rawJSON []byte, models []bestiary.ModelInfo, metadata []bestiary.EntityMetadata, provMeta map[string]providerAPIMeta, failures []bestiary.ParseFailure, err error) {
	if noFetch {
		body, readErr := os.ReadFile(vendoredCatalogPath)
		if readErr != nil || len(body) == 0 {
			absPath, _ := filepath.Abs(vendoredCatalogPath)
			if absPath == "" {
				absPath = vendoredCatalogPath
			}
			return nil, nil, nil, nil, nil, &ErrVendoredCatalogMissing{Path: absPath}
		}
		rawJSON = body
	} else {
		body, fetchErr := fetchLiveCatalog(ctx)
		if fetchErr != nil {
			return nil, nil, nil, nil, nil, fetchErr
		}
		rawJSON = body
	}

	// Single decode path: the library's ParseCatalogJSON. The generator never carries
	// a second copy of the models.dev wire schema.
	cat, parseErr := bestiary.ParseCatalogJSON(rawJSON)
	if parseErr != nil {
		return nil, nil, nil, nil, nil, catalogDecodeError(parseErr, noFetch)
	}

	// Decompose each parsed model. The library gives each ModelInfo its raw family on
	// the Family field (RawFamily unset); enrichModelInfo runs the production
	// decomposition and the curated bakes over it.
	models = make([]bestiary.ModelInfo, 0, len(cat.Models))
	for _, base := range cat.Models {
		info, failure, enrichErr := enrichModelInfo(base)
		if enrichErr != nil {
			return nil, nil, nil, nil, nil, enrichErr
		}
		models = append(models, info)
		if failure != nil {
			failures = append(failures, *failure)
		}
	}
	metadata = cat.Metadata

	// Determinism: sort the assembled model set by (Provider, ID) exactly once so
	// every downstream consumer observes a stable order regardless of catalog map-
	// iteration order.
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].ID < models[j].ID
	})

	// Provider display names: the library's decode drops the per-provider "name"
	// (ModelInfo carries no provider display name), so extract it from the same bytes
	// with a thin name probe. This is not a second wire-schema copy — it reads only the
	// stable provider "name" the casing heuristics use as a hint.
	provMeta, err = extractProviderNames(rawJSON)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}

	return rawJSON, models, metadata, provMeta, failures, nil
}

// fetchLiveCatalog performs the single polite GET of the live catalog.json artifact
// and returns its body (10 MB-limited). It sets a descriptive User-Agent (polite-bot
// precedent) and records the response ETag on lastFetchMeta for the snapshot manifest.
func fetchLiveCatalog(ctx context.Context) ([]byte, error) {
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if reqErr != nil {
		return nil, fmt.Errorf(
			"create HTTP request for %s: %w\n"+
				"  What: failed to construct the catalog request\n"+
				"  How to fix: this is a programming error — report it",
			catalogURL, reqErr,
		)
	}
	req.Header.Set("User-Agent", codegenUserAgent)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, doErr := client.Do(req)
	if doErr != nil {
		return nil, fmt.Errorf(
			"HTTP GET %s: %w\n"+
				"  What: network request failed\n"+
				"  How to fix: check network connectivity and retry",
			catalogURL, doErr,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"unexpected HTTP status %d from %s; expected 200 OK\n"+
				"  What: the API returned a non-success status\n"+
				"  How to fix: check the API endpoint and try again",
			resp.StatusCode, catalogURL,
		)
	}

	const maxBodyBytes = 10 * 1024 * 1024
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if readErr != nil {
		return nil, fmt.Errorf(
			"read response body from %s: %w\n"+
				"  What: failed to read the catalog response body\n"+
				"  How to fix: retry the operation",
			catalogURL, readErr,
		)
	}
	lastFetchMeta = fetchMeta{etag: resp.Header.Get("ETag")}
	return body, nil
}

// catalogDecodeError wraps a ParseCatalogJSON failure in an actionable message. In
// --no-fetch mode it names the vendored path and the refresh workflow (a corrupt
// committed snapshot must never silently degrade to an empty catalog); in fetch mode it
// points at the upstream schema.
func catalogDecodeError(cause error, noFetch bool) error {
	if noFetch {
		absPath, _ := filepath.Abs(vendoredCatalogPath)
		if absPath == "" {
			absPath = vendoredCatalogPath
		}
		return fmt.Errorf(
			"decode vendored models.dev catalog.json: %w\n"+
				"  What: the committed codegen input is not valid catalog.json ({models, providers})\n"+
				"  Why: the vendored snapshot is corrupt or truncated\n"+
				"  Where: %s\n"+
				"  When: codegen input decode step (--no-fetch)\n"+
				"  What it means: bestiary-gen cannot proceed and never degrades to an empty catalog\n"+
				"  How to fix: re-run the models.dev snapshot refresh — `go run ./cmd/bestiary-gen` "+
				"WITHOUT --no-fetch — to re-vendor a clean catalog.json (see AGENTS.md)",
			cause, absPath,
		)
	}
	return fmt.Errorf(
		"decode models.dev catalog.json from %s: %w\n"+
			"  What: the live catalog response could not be decoded as catalog.json\n"+
			"  Why: the upstream schema may have changed\n"+
			"  How to fix: inspect the response and update wire.go / ParseCatalogJSON in the library",
		catalogURL, cause,
	)
}

// extractProviderNames reads only the provider display names from the catalog bytes.
// The library's decode intentionally drops the per-provider "name" (there is no
// ModelInfo field for it), but codegen uses it as a casing hint for provider/family
// identifiers, so a thin name probe recovers it without duplicating the model wire
// schema.
func extractProviderNames(rawJSON []byte) (map[string]providerAPIMeta, error) {
	var probe struct {
		Providers map[string]struct {
			Name string `json:"name"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rawJSON, &probe); err != nil {
		return nil, fmt.Errorf(
			"extractProviderNames: decode catalog providers view: %w\n"+
				"  What: could not read the providers map from the catalog bytes\n"+
				"  How to fix: verify the bytes are the catalog.json artifact ({models, providers})",
			err,
		)
	}
	out := make(map[string]providerAPIMeta, len(probe.Providers))
	for slug, p := range probe.Providers {
		out[slug] = providerAPIMeta{Name: p.Name}
	}
	return out, nil
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

// enrichModelInfo runs the codegen decomposition + curated bakes over a base
// ModelInfo the library's ParseCatalogJSON produced. On entry, `base` already carries
// every api.json-side fact the library decoded (id, provider, display name, costs,
// limits, modalities, interleaved, description, status, reasoning options, …) with its
// RAW family on the Family field (RawFamily unset). enrichModelInfo overwrites the
// identity axes (RawFamily/Family/Variant/Version/Date/Modifier), attaches the serving
// host, and bakes the curated lineage/param-size/source/quant-VRAM data keyed by ID.
//
// It returns (ModelInfo, *ParseFailure, error). The *ParseFailure is non-nil when
// ParseFamilyDetailed detects a known parsing deficiency (collected by
// fetchModelsWithRaw and written to parse_failures.json). The error is a LOUD,
// FATAL codegen failure — currently a param-size disagreement surfaced by
// EnrichedParamSize (mechanical vs curated ParamSizeFor) — that must abort the bake
// rather than silently baking a contested size.
func enrichModelInfo(base bestiary.ModelInfo) (bestiary.ModelInfo, *bestiary.ParseFailure, error) {
	// The library set Family = the raw family string; that is the codegen raw family.
	rawFamily := base.Family
	id := base.ID
	provider := base.Provider

	// Single-ownership: consume the full 5-tuple from ParseFamilyDetailed.
	// ParseFamilyDetailed(raw="") delegates to InferFamilyFromIDWithVariant +
	// ExtractModifier, covering the empty-family case.
	// (family, variant, version, modifier) all come from ParseFamilyDetailed.
	normFamily, normVariant, normVersion, normModifier, failure := bestiary.ParseFamilyDetailed(rawFamily, id, provider)

	// Serving-host attribute. DetectHost surfaces a curated host prefix
	// (e.g. "azure-gpt-4o" → HostAzure) as a per-instance attribute; the same
	// strip is applied inside ParseFamilyDetailed so the decomposition above is
	// already host-independent. The full catalog ID is retained as info.ID below
	// — Host records the backend without mutating the record's identity.
	host, _ := bestiary.DetectHost(id)

	// Serving-jurisdiction attribute. DetectRegion surfaces an AWS Bedrock
	// cross-region inference-profile prefix (e.g. "eu.anthropic.claude-..." → RegionEU)
	// as a per-instance attribute; the same prefix is stripped inside
	// ParseFamilyDetailed so the decomposition above is already region-independent.
	// Like Host, Region records the jurisdiction without mutating identity.
	region, regionRaw := bestiary.DetectRegion(id)

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
	normDate := bestiary.ExtractDate(cleanID, base.ReleaseDate)

	// If a parse failure was detected, backfill the date into AttemptedParse.
	if failure != nil {
		failure.AttemptedParse.Date = normDate
		// Models where ExtractModifier extracts a known modifier no longer trip
		// ReasonKnownSuffixOverflow (the modifier is now a first-class field).
		// Clear the failure record for this case so the audit log shrinks as
		// expected now that the modifier is captured separately.
		if failure.Reason == bestiary.ReasonKnownSuffixOverflow && len(normModifier) > 0 {
			failure = nil
		}
	}

	// Start from the library-decoded base (costs, limits, modalities, interleaved, and
	// the new api.json-side facts are already populated) and overwrite the identity
	// axes plus the codegen-baked fields.
	info := base
	info.RawFamily = rawFamily
	info.Host = host
	info.Region = region
	info.RegionRaw = regionRaw
	info.Family = normFamily
	info.Variant = normVariant
	info.Version = normVersion
	info.Date = normDate
	info.Modifier = normModifier
	info.LastSynced = ""

	// Lineage. Populate the derivation edges from the curated lineage
	// ledger (parse/data/lineage.json) for any catalog record whose ID matches a
	// curated child key. The ledger — not raw_family — is the authoritative
	// lineage source; nil (no edge) for the overwhelming majority of base models.
	info.Lineage = bestiary.LineageFor(id)

	// Parameter-size enrichment (folded into the shared library precedence pin >
	// mechanical > ParamSizeFor). The size is baked here so the static entity keys
	// re-key in full bulk: a mechanical ExtractParamSizeToken token now sizes the
	// majority of catalog IDs that carry no curated quant_vram.json param_size. A
	// disagreement between the mechanical token and the fetch-owned ParamSizeFor
	// fallback is a LOUD, FATAL codegen error (a curator must add a pin) rather than
	// a silently baked contested size. The flat shape ints are decomposed from the
	// resolved token via ParseParamShape so the baked static row carries them without
	// going through a runtime joint.
	enrichedSize, sizeErr := bestiary.EnrichedParamSize(string(id))
	if sizeErr != nil {
		return info, failure, fmt.Errorf("codegen param-size enrichment for %q: %w", id, sizeErr)
	}
	info.ParamSize = enrichedSize
	// ParseParamShape returns the all-NULL (ParamShapeNull) shape for an empty token
	// AND on any decomposition error, so the assignment is unconditional: an unsized
	// or unparseable row bakes the four NULL sentinels, never a masquerading 0. The
	// shapeErr is intentionally discarded — enrichedSize is a canonical-or-empty token
	// (EnrichedParamSize already surfaced any disagreement as a fatal error above), so
	// a shape error here would be a grammar-drift bug whose NULL fallback is still the
	// correct bake.
	shape, _ := bestiary.ParseParamShape(enrichedSize)
	info.TotalParams = shape.TotalParams
	info.ActiveParams = shape.ActiveParams
	info.PerExpertParams = shape.PerExpertParams
	info.ExpertCount = shape.ExpertCount

	// Release-stage enrichment. Stage/StageRaw are derived from the ID by the same
	// pure DetectStageFromID the runtime joints use (wire decode, store read), so
	// the baked static row and a live-sync row of the same ID carry an identical
	// stage. Stage is a per-instance attribute and never touches the entity key, so
	// this bakes a new field without any re-key.
	info.Stage, info.StageRaw = bestiary.DetectStageFromID(id)

	// Curated Source and QuantVRAM from the curated quant_vram.json table. QuantVRAM
	// rows are BAKED here: each row's VRAMBytes and VRAMContextTokens are filled in by
	// calling EstimateVRAMBytes at the model's maximum context window. The bake-context
	// precedence is: (1) curated context_window from quant_vram.json (most specific);
	// (2) ModelInfo.ContextWindow from the upstream models.dev catalog; (3) 0
	// (weights-only lower bound). VRAMEstimatePartial is set when the arch facts
	// (Layers, KVHeads, HeadDim) are absent, per VRAMEstimateIsPartial.
	info.Source = bestiary.SourceFor(id)

	rawRows := bestiary.QuantVRAMFor(id)
	if len(rawRows) > 0 {
		// Determine the bake context using the curated precedence chain.
		bakeCtx := bestiary.ContextWindowFor(id)
		if bakeCtx == 0 {
			bakeCtx = info.ContextWindow
		}
		// bakeCtx may still be 0 if neither curated nor upstream supplied it;
		// EstimateVRAMBytes handles 0 contextTokens as weights-only (KV=0).

		baked := make([]bestiary.QuantVRAM, len(rawRows))
		for i, row := range rawRows {
			row.VRAMBytes = bestiary.EstimateVRAMBytes(row.WeightsBytes, bakeCtx, row.Layers, row.KVHeads, row.HeadDim)
			row.VRAMContextTokens = bakeCtx
			row.VRAMEstimatePartial = bestiary.VRAMEstimateIsPartial(row.Layers, row.KVHeads, row.HeadDim)
			baked[i] = row
		}
		info.QuantVRAM = baked
	}

	// Curated finetune base reference. When quant_vram.json records a
	// base_ref for this model, append a DerivationFinetune edge to Lineage. This
	// path is production-dormant until a curated finetune joins a models.dev
	// catalog ID: the only base_ref entry today is a non-models.dev community
	// finetune that never joins during codegen.
	appendFinetuneLineage(&info, bestiary.BaseRefFor(id))

	return info, failure, nil
}

// appendFinetuneLineage appends a DerivationFinetune edge to info.Lineage when
// baseRef is non-empty, with the parent EntityRef decomposed from baseRef by
// parseBaseRef. An empty baseRef is a no-op (info.Lineage is left untouched).
//
// This handles community Ollama finetunes whose base is known from curated
// metadata but not yet in the lineage.json ledger. If the ledger already supplied
// edges (LineageFor returned non-nil), the base_ref edge is still appended so the
// full parent set is recorded; callers must be aware of this when consuming
// Lineage.
//
// info.Lineage on entry is the slice returned by bestiary.LineageFor, which is
// the curated table's stored backing slice (not a copy). A plain append could
// write into that shared array if it has spare capacity, so this copies the
// existing edges into a fresh slice before appending — the appended edge can
// never alias the curated ledger's storage.
func appendFinetuneLineage(info *bestiary.ModelInfo, baseRef string) {
	if baseRef == "" {
		return
	}
	edges := make([]bestiary.LineageEdge, 0, len(info.Lineage)+1)
	edges = append(edges, info.Lineage...)
	edges = append(edges, bestiary.LineageEdge{
		Parent: parseBaseRef(baseRef),
		Kind:   bestiary.DerivationFinetune,
	})
	info.Lineage = edges
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

// reBaseRefModelVersion matches a trailing version number (integer or simple
// dotted, e.g. "3", "3.1", "3.3") appended directly to a family name in an
// Ollama-style base ref (e.g. "llama3", "llama3.1"). This is distinct from a
// full model ID like "llama-3.3-70b-instruct" — the base-ref model part uses
// no hyphen between the family and version digit.
var reBaseRefModelVersion = regexp.MustCompile(`^([a-z][a-z0-9\-]*)(\d+(?:\.\d+)?)$`)

// parseBaseRef decomposes a curated base-ref string (as stored in
// parse/data/quant_vram.json's base_ref field) into a bestiary.EntityRef. The
// format used in the curated file is an Ollama-style reference:
//
//	"<model>[:<tag>]"
//
// where <model> is a model-family name with an optional bare version digit
// glued to it (e.g. "llama3", "llama3.1") and <tag> is a hyphen-separated
// sequence of param-size and modifier tokens (e.g. "70b-instruct"). The
// decomposition uses the production parse pipeline plus a dedicated step for
// the bare-version-glued-to-family form:
//
//  1. Split on the first ':' to get model and tag.
//  2. Detect and strip a trailing version number from the model part (e.g.
//     "llama3" → model="llama", version="3"). This handles the common Ollama
//     naming pattern where the generation digit is glued directly to the family
//     name without a hyphen. After stripping, apply ParseFamilyWithVersion to
//     the clean family string.
//  3. Heuristic tag scan: the first token that passes ParseParamSize is the
//     param-size; remaining hyphen-separated tokens are treated as modifiers
//     and passed through EntityModifiers to project only the identity-class subset.
//
// Unknown tokens (tokens that are neither a valid param-size nor a recognised
// modifier) are silently ignored — curated base refs are always small, and
// ignoring noise is better than aborting codegen on an unrecognised suffix.
// The function never panics and always returns a non-zero EntityRef.
func parseBaseRef(baseRef string) bestiary.EntityRef {
	// Split on the first ':' to separate the model part from the tag.
	model, tag, _ := strings.Cut(baseRef, ":")

	// Detect "llama3", "llama3.1", "qwen2", etc.: family name with a bare
	// version number glued to the end (no hyphen separator). When matched,
	// strip the version from the model string and carry it forward.
	var gluedVersion string
	if m := reBaseRefModelVersion.FindStringSubmatch(model); m != nil {
		// m[1] (the family prefix) is non-empty by construction: the regex's
		// leading [a-z] requirement excludes pure-numeric/degenerate inputs, which
		// never match at all (FindStringSubmatch returns nil for those).
		model = m[1]        // e.g. "llama"
		gluedVersion = m[2] // e.g. "3", "3.1"
	}

	// Decompose the (now clean) model part using ParseFamilyWithVersion.
	normFamily, normVariant, normVersion := bestiary.ParseFamilyWithVersion(bestiary.Family(model))

	// If ParseFamilyWithVersion didn't extract a version but we found a glued
	// version above, use the glued version. If ParseFamilyWithVersion did
	// extract one (e.g. from a hyphen-version pattern), prefer that.
	if normVersion == "" && gluedVersion != "" {
		normVersion = gluedVersion
	}

	var paramSize string
	var modifiers []string

	if tag != "" {
		// Delegate the size lift to the shared ExtractParamSizeToken grammar authority
		// (longest whole-window match), so a compound token like "17b-16e" or
		// "235b-a22b" is captured WHOLE rather than clipped to its first "17b"/"235b"
		// by a greedy per-token scan. The remaining tokens (the tag with the matched
		// size window removed) are candidate modifiers; EntityModifiers projects only
		// identity-class tokens for the resolved family. Lowercasing first makes the
		// canonical size token a literal substring of the tag so it can be excised.
		tagLower := strings.ToLower(tag)
		rest := tagLower
		if ps, ok := bestiary.ExtractParamSizeToken(tagLower); ok {
			paramSize = ps
			rest = strings.Replace(tagLower, ps, "", 1)
		}
		for _, tok := range strings.Split(rest, "-") {
			if tok == "" {
				continue
			}
			modifiers = append(modifiers, tok)
		}
	}

	return bestiary.EntityRef{
		Family:    normFamily,
		Variant:   normVariant,
		Version:   normVersion,
		ParamSize: paramSize,
		Modifier:  bestiary.EntityModifiers(modifiers, normFamily),
	}
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
//
// Every EntityRef field the curated ledger can set must be emitted here, ParamSize
// included: a parent may name a specific size (a 32B finetune derives from the 32B
// base, not from the whole line), and an emitter that skips the field silently
// widens that edge to the size-agnostic parent in the BAKED data while the runtime
// lineage.json path keeps the size — the two would then disagree about the same
// curated edge. The field went unemitted until the first curated edge used it.
func lineageLiteral(edges []bestiary.LineageEdge) string {
	if len(edges) == 0 {
		return "nil"
	}
	parts := make([]string, len(edges))
	for i, e := range edges {
		parts[i] = fmt.Sprintf(
			"{Parent: EntityRef{Family: %q, Variant: %q, Version: %q, ParamSize: %q, Modifier: %s}, Kind: %s}",
			string(e.Parent.Family), e.Parent.Variant, e.Parent.Version, e.Parent.ParamSize,
			goStringSliceLiteral(e.Parent.Modifier), derivationKindExpr(e.Kind),
		)
	}
	return "[]LineageEdge{" + strings.Join(parts, ", ") + "}"
}

// quantExpr renders a Quantization value as its exported constant name so the
// generated source references the enum symbolically (e.g. QuantQ4_K_M) rather
// than by integer value. Mirrors derivationKindExpr exactly. An out-of-range
// value falls back to QuantizationNone defensively.
func quantExpr(q bestiary.Quantization) string {
	switch q {
	case bestiary.QuantF16:
		return "QuantF16"
	case bestiary.QuantBF16:
		return "QuantBF16"
	case bestiary.QuantF32:
		return "QuantF32"
	case bestiary.QuantQ4_0:
		return "QuantQ4_0"
	case bestiary.QuantQ4_1:
		return "QuantQ4_1"
	case bestiary.QuantQ5_0:
		return "QuantQ5_0"
	case bestiary.QuantQ5_1:
		return "QuantQ5_1"
	case bestiary.QuantQ8_0:
		return "QuantQ8_0"
	case bestiary.QuantQ2_K:
		return "QuantQ2_K"
	case bestiary.QuantQ2_K_S:
		return "QuantQ2_K_S"
	case bestiary.QuantQ3_K_S:
		return "QuantQ3_K_S"
	case bestiary.QuantQ3_K_M:
		return "QuantQ3_K_M"
	case bestiary.QuantQ3_K_L:
		return "QuantQ3_K_L"
	case bestiary.QuantQ4_K_S:
		return "QuantQ4_K_S"
	case bestiary.QuantQ4_K_M:
		return "QuantQ4_K_M"
	case bestiary.QuantQ5_K_S:
		return "QuantQ5_K_S"
	case bestiary.QuantQ5_K_M:
		return "QuantQ5_K_M"
	case bestiary.QuantQ6_K:
		return "QuantQ6_K"
	case bestiary.QuantIQ1_S:
		return "QuantIQ1_S"
	case bestiary.QuantIQ1_M:
		return "QuantIQ1_M"
	case bestiary.QuantIQ2_XXS:
		return "QuantIQ2_XXS"
	case bestiary.QuantIQ2_XS:
		return "QuantIQ2_XS"
	case bestiary.QuantIQ2_S:
		return "QuantIQ2_S"
	case bestiary.QuantIQ2_M:
		return "QuantIQ2_M"
	case bestiary.QuantIQ3_XXS:
		return "QuantIQ3_XXS"
	case bestiary.QuantIQ3_XS:
		return "QuantIQ3_XS"
	case bestiary.QuantIQ3_S:
		return "QuantIQ3_S"
	case bestiary.QuantIQ3_M:
		return "QuantIQ3_M"
	case bestiary.QuantIQ4_XS:
		return "QuantIQ4_XS"
	case bestiary.QuantIQ4_NL:
		return "QuantIQ4_NL"
	case bestiary.QuantAWQ:
		return "QuantAWQ"
	case bestiary.QuantGPTQ:
		return "QuantGPTQ"
	case bestiary.QuantInt8:
		return "QuantInt8"
	case bestiary.QuantInt4:
		return "QuantInt4"
	case bestiary.QuantizationOther:
		return "QuantizationOther"
	default:
		return "QuantizationNone"
	}
}

// quantVRAMLiteral renders a []QuantVRAM as a Go composite literal for the
// generated source, mirroring lineageLiteral's empty→"nil" contract so the
// models-with-no-quant-data majority emits a bare nil. The generated file is
// package bestiary, so QuantVRAM and Quantization constants are referenced
// unqualified. Row order is the curated file order (the loader preserves
// insertion order), which is deterministic and never subject to map iteration.
func quantVRAMLiteral(rows []bestiary.QuantVRAM) string {
	if len(rows) == 0 {
		return "nil"
	}
	parts := make([]string, len(rows))
	for i, r := range rows {
		fields := fmt.Sprintf(
			"Quant: %s, QuantRaw: %q, WeightsBytes: %d, VRAMBytes: %d,"+
				" VRAMContextTokens: %d, Layers: %d, KVHeads: %d, HeadDim: %d,"+
				" VRAMEstimatePartial: %v",
			quantExpr(r.Quant), r.QuantRaw,
			r.WeightsBytes, r.VRAMBytes, r.VRAMContextTokens,
			r.Layers, r.KVHeads, r.HeadDim,
			r.VRAMEstimatePartial,
		)
		// OCIDigest is emitted only when present: the empty-digest majority (every
		// curated row until an Ollama refresh harvests digests) stays byte-identical to
		// its pre-OCIDigest bake, so this field adds no codegen diff today. The
		// condition is on the deterministic baked data, so INV3 (byte-identical regen)
		// holds. Row order is the curated file order (never map iteration).
		if r.OCIDigest != "" {
			fields += fmt.Sprintf(", OCIDigest: %q", r.OCIDigest)
		}
		parts[i] = "{" + fields + "}"
	}
	return "[]QuantVRAM{" + strings.Join(parts, ", ") + "}"
}

// statusExpr renders a ModelStatus as its exported constant name so the generated
// source references the enum symbolically (e.g. StatusDeprecated). Mirrors
// derivationKindExpr/quantExpr. StatusNone (the zero value) is the default.
func statusExpr(s bestiary.ModelStatus) string {
	switch s {
	case bestiary.StatusAlpha:
		return "StatusAlpha"
	case bestiary.StatusBeta:
		return "StatusBeta"
	case bestiary.StatusDeprecated:
		return "StatusDeprecated"
	case bestiary.StatusOther:
		return "StatusOther"
	default:
		return "StatusNone"
	}
}

// stageExpr renders a ReleaseStage as its exported constant name so the generated
// source references the enum symbolically (e.g. StageBeta). Mirrors statusExpr.
// StageNone (the zero value) is the default.
func stageExpr(s bestiary.ReleaseStage) string {
	switch s {
	case bestiary.StageStable:
		return "StageStable"
	case bestiary.StagePreview:
		return "StagePreview"
	case bestiary.StageBeta:
		return "StageBeta"
	case bestiary.StageAlpha:
		return "StageAlpha"
	case bestiary.StageExperimental:
		return "StageExperimental"
	case bestiary.StageLatest:
		return "StageLatest"
	case bestiary.StageOriginal:
		return "StageOriginal"
	case bestiary.StageOther:
		return "StageOther"
	default:
		return "StageNone"
	}
}

// regionExpr renders a Region as its exported constant name so the generated source
// references the enum symbolically (e.g. RegionEU). Mirrors statusExpr/stageExpr.
// RegionNone (the zero value) is the default and is never emitted (the caller guards
// on it), but is returned here for completeness.
func regionExpr(r bestiary.Region) string {
	switch r {
	case bestiary.RegionUS:
		return "RegionUS"
	case bestiary.RegionEU:
		return "RegionEU"
	case bestiary.RegionAPAC:
		return "RegionAPAC"
	case bestiary.RegionGlobal:
		return "RegionGlobal"
	case bestiary.RegionAU:
		return "RegionAU"
	case bestiary.RegionJP:
		return "RegionJP"
	case bestiary.RegionOther:
		return "RegionOther"
	default:
		return "RegionNone"
	}
}

// creatorExpr renders a Creator as its exported constant name so the generated
// source references the well-known set symbolically (e.g. CreatorAnthropic), mirroring
// providerExpr. Creator is an OPEN string type, so an unmapped-but-present creator (a
// future huggingface-ingest originator with no constant) falls back to a Creator("token")
// conversion rather than a broken symbol. CreatorNone (the zero value) is never emitted —
// the caller guards on it (the Region compact-omit precedent).
func creatorExpr(c bestiary.Creator) string {
	switch c {
	case bestiary.CreatorMeta:
		return "CreatorMeta"
	case bestiary.CreatorOpenAI:
		return "CreatorOpenAI"
	case bestiary.CreatorAnthropic:
		return "CreatorAnthropic"
	case bestiary.CreatorGoogle:
		return "CreatorGoogle"
	case bestiary.CreatorMistral:
		return "CreatorMistral"
	case bestiary.CreatorCohere:
		return "CreatorCohere"
	case bestiary.CreatorDeepSeek:
		return "CreatorDeepSeek"
	case bestiary.CreatorAlibaba:
		return "CreatorAlibaba"
	case bestiary.CreatorZhipu:
		return "CreatorZhipu"
	case bestiary.CreatorDeepReinforce:
		return "CreatorDeepReinforce"
	case bestiary.CreatorMeituan:
		return "CreatorMeituan"
	case bestiary.CreatorMicrosoft:
		return "CreatorMicrosoft"
	case bestiary.CreatorMiniMax:
		return "CreatorMiniMax"
	case bestiary.CreatorMoonshotAI:
		return "CreatorMoonshotAI"
	case bestiary.CreatorNvidia:
		return "CreatorNvidia"
	case bestiary.CreatorPerplexity:
		return "CreatorPerplexity"
	case bestiary.CreatorPoolside:
		return "CreatorPoolside"
	case bestiary.CreatorSakana:
		return "CreatorSakana"
	case bestiary.CreatorSarvam:
		return "CreatorSarvam"
	case bestiary.CreatorStepFun:
		return "CreatorStepFun"
	case bestiary.CreatorTencent:
		return "CreatorTencent"
	case bestiary.CreatorThinkingMachines:
		return "CreatorThinkingMachines"
	case bestiary.CreatorXAI:
		return "CreatorXAI"
	case bestiary.CreatorXiaomi:
		return "CreatorXiaomi"
	case bestiary.Creator01AI:
		return "Creator01AI"
	case bestiary.CreatorAI21:
		return "CreatorAI21"
	case bestiary.CreatorAmazon:
		return "CreatorAmazon"
	case bestiary.CreatorBAAI:
		return "CreatorBAAI"
	case bestiary.CreatorBaichuan:
		return "CreatorBaichuan"
	case bestiary.CreatorBaidu:
		return "CreatorBaidu"
	case bestiary.CreatorBlackForestLabs:
		return "CreatorBlackForestLabs"
	case bestiary.CreatorByteDance:
		return "CreatorByteDance"
	case bestiary.CreatorElevenLabs:
		return "CreatorElevenLabs"
	case bestiary.CreatorIBM:
		return "CreatorIBM"
	case bestiary.CreatorIdeogram:
		return "CreatorIdeogram"
	case bestiary.CreatorInclusionAI:
		return "CreatorInclusionAI"
	case bestiary.CreatorNousResearch:
		return "CreatorNousResearch"
	case bestiary.CreatorRecraft:
		return "CreatorRecraft"
	case bestiary.CreatorReka:
		return "CreatorReka"
	case bestiary.CreatorRunway:
		return "CreatorRunway"
	case bestiary.CreatorStabilityAI:
		return "CreatorStabilityAI"
	case bestiary.CreatorUpstage:
		return "CreatorUpstage"
	case bestiary.CreatorVoyageAI:
		return "CreatorVoyageAI"
	default:
		return fmt.Sprintf("Creator(%q)", string(c))
	}
}

// reasoningOptionKindExpr renders a ReasoningOptionKind as its exported constant name.
// ReasoningOptionOther (the zero value) is the default fail-safe.
func reasoningOptionKindExpr(k bestiary.ReasoningOptionKind) string {
	switch k {
	case bestiary.ReasoningToggle:
		return "ReasoningToggle"
	case bestiary.ReasoningEffort:
		return "ReasoningEffort"
	case bestiary.ReasoningBudgetTokens:
		return "ReasoningBudgetTokens"
	default:
		return "ReasoningOptionOther"
	}
}

// reasoningOptionsLiteral renders a []ReasoningOption as a Go composite literal,
// preserving the upstream array order (deterministic — no map iteration). Empty→"nil".
func reasoningOptionsLiteral(opts []bestiary.ReasoningOption) string {
	if len(opts) == 0 {
		return "nil"
	}
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = fmt.Sprintf(
			"{Kind: %s, KindRaw: %q, Values: %s, MinTokens: %d, MaxTokens: %d}",
			reasoningOptionKindExpr(o.Kind), o.KindRaw, goStringSliceLiteral(o.Values), o.MinTokens, o.MaxTokens,
		)
	}
	return "[]ReasoningOption{" + strings.Join(parts, ", ") + "}"
}

// tierCostLiteral renders a TierCost value as a Go composite literal, emitting only the
// non-nil per-million-token cost pointers in a FIXED field order (deterministic and
// compact — an all-nil TierCost renders as "TierCost{}"). Used for CostContextOver200k
// and each CostTier's embedded bundle.
func tierCostLiteral(tc bestiary.TierCost) string {
	var parts []string
	add := func(name string, p *float64) {
		if p != nil {
			parts = append(parts, name+": "+float64PtrExpr(p))
		}
	}
	add("CostInputPerMTok", tc.CostInputPerMTok)
	add("CostOutputPerMTok", tc.CostOutputPerMTok)
	add("CostReasoningPerMTok", tc.CostReasoningPerMTok)
	add("CostCacheReadPerMTok", tc.CostCacheReadPerMTok)
	add("CostCacheWritePerMTok", tc.CostCacheWritePerMTok)
	add("CostInputAudioPerMTok", tc.CostInputAudioPerMTok)
	add("CostOutputAudioPerMTok", tc.CostOutputAudioPerMTok)
	return "TierCost{" + strings.Join(parts, ", ") + "}"
}

// tierCostPtrExpr renders a *TierCost as "nil" or "&TierCost{...}" (the address of a
// composite literal), for the CostContextOver200k pointer field.
func tierCostPtrExpr(tc *bestiary.TierCost) string {
	if tc == nil {
		return "nil"
	}
	return "&" + tierCostLiteral(*tc)
}

// costTiersLiteral renders a []CostTier as a Go composite literal, preserving the
// upstream tier order (deterministic). Empty→"nil". CostTier embeds TierCost, so the
// bundle is emitted under the embedded field name.
func costTiersLiteral(tiers []bestiary.CostTier) string {
	if len(tiers) == 0 {
		return "nil"
	}
	parts := make([]string, len(tiers))
	for i, t := range tiers {
		parts[i] = fmt.Sprintf("{ContextSize: %d, TierCost: %s}", t.ContextSize, tierCostLiteral(t.TierCost))
	}
	return "[]CostTier{" + strings.Join(parts, ", ") + "}"
}

func generateSource(models []bestiary.ModelInfo, slugToConst map[string]string) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
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
		// Region (AWS Bedrock cross-region inference profile) is an int enum, emitted
		// CONDITIONALLY — only when a prefix was detected — matching the Stage/Status
		// precedent so the unregioned majority stays compact. RegionRaw is the reserved
		// Other-bucket carrier and is empty for every named-member region.
		if m.Region != bestiary.RegionNone {
			fmt.Fprintf(&buf, "\t\tRegion:                %s,\n", regionExpr(m.Region))
		}
		if m.RegionRaw != "" {
			fmt.Fprintf(&buf, "\t\tRegionRaw:             %q,\n", m.RegionRaw)
		}
		fmt.Fprintf(&buf, "\t\tLineage:               %s,\n", lineageLiteral(m.Lineage))
		// ParamSize: only emit when non-empty, matching how other optional string
		// fields (Variant, Version, Date) are handled — zero value omitted entirely
		// so the output stays compact for the unsized majority.
		if m.ParamSize != "" {
			fmt.Fprintf(&buf, "\t\tParamSize:             %q,\n", m.ParamSize)
		}
		// Parameter-shape ints: DERIVED from ParamSize by ParseParamShape and baked so
		// static rows carry them without a runtime joint. Emitted UNCONDITIONALLY (all
		// four, every row) under the ParamShapeNull sentinel contract: an omitted field
		// would default to the Go zero 0, but 0 is a MEANINGFUL value here (a dense
		// shape's ExpertCount) that must be distinguished from the NULL sentinel -1, so
		// every field is written explicitly. This makes the bake byte-identical to the
		// runtime enrichModelInfo decomposition of the same ID.
		fmt.Fprintf(&buf, "\t\tTotalParams:           %d,\n", m.TotalParams)
		fmt.Fprintf(&buf, "\t\tActiveParams:          %d,\n", m.ActiveParams)
		fmt.Fprintf(&buf, "\t\tPerExpertParams:       %d,\n", m.PerExpertParams)
		fmt.Fprintf(&buf, "\t\tExpertCount:           %d,\n", m.ExpertCount)
		// Source: always emit; DataSourceNone ("") is the correct zero value for
		// live-sync rows and is emitted explicitly so the field is self-documenting.
		fmt.Fprintf(&buf, "\t\tSource:                %q,\n", string(m.Source))
		// Creator: the DERIVED Family→Creator projection, baked from the curated
		// creators.json seed (validated loudly above via ValidateCreatorTable). Emitted
		// CONDITIONALLY — only when the family maps to a creator — matching the
		// Region/ParamSize compact-omit precedent; an unmapped family carries the
		// CreatorNone ("") zero value and is omitted. Baking the family-derived value
		// keeps the compiled registry and the store creators dimension in agreement by
		// construction (the mapping is a codegen input, not a hand-entered per-row fact).
		if c := m.Family.Creator(); c != bestiary.CreatorNone {
			fmt.Fprintf(&buf, "\t\tCreator:               %s,\n", creatorExpr(c))
		}
		fmt.Fprintf(&buf, "\t\tQuantVRAM:             %s,\n", quantVRAMLiteral(m.QuantVRAM))
		// Instance-level facts from the api.json side (description, status, reasoning
		// options, audio/tier costs). Emitted CONDITIONALLY — only when non-zero —
		// matching the ParamSize precedent, so the unset majority stays compact. The
		// wire parse populates these and enrichModelInfo carries them through; dropping
		// them here would make (e.g.) `list --status deprecated` vacuously empty.
		if m.Description != "" {
			fmt.Fprintf(&buf, "\t\tDescription:           %q,\n", m.Description)
		}
		if m.Status != bestiary.StatusNone {
			fmt.Fprintf(&buf, "\t\tStatus:                %s,\n", statusExpr(m.Status))
		}
		if m.StatusRaw != "" {
			fmt.Fprintf(&buf, "\t\tStatusRaw:             %q,\n", m.StatusRaw)
		}
		// Release stage (ID-derived, distinct from the api.json Status above).
		// Emitted CONDITIONALLY — only when a stage was detected — matching the
		// Status precedent so the unmarked majority stays compact. StageRaw is the
		// reserved Other-bucket companion and is empty for every ID-derived stage.
		if m.Stage != bestiary.StageNone {
			fmt.Fprintf(&buf, "\t\tStage:                 %s,\n", stageExpr(m.Stage))
		}
		if m.StageRaw != "" {
			fmt.Fprintf(&buf, "\t\tStageRaw:              %q,\n", m.StageRaw)
		}
		if len(m.ReasoningOptions) > 0 {
			fmt.Fprintf(&buf, "\t\tReasoningOptions:      %s,\n", reasoningOptionsLiteral(m.ReasoningOptions))
		}
		if m.CostInputAudioPerMTok != nil {
			fmt.Fprintf(&buf, "\t\tCostInputAudioPerMTok:  %s,\n", float64PtrExpr(m.CostInputAudioPerMTok))
		}
		if m.CostOutputAudioPerMTok != nil {
			fmt.Fprintf(&buf, "\t\tCostOutputAudioPerMTok: %s,\n", float64PtrExpr(m.CostOutputAudioPerMTok))
		}
		if m.CostContextOver200k != nil {
			fmt.Fprintf(&buf, "\t\tCostContextOver200k:   %s,\n", tierCostPtrExpr(m.CostContextOver200k))
		}
		if len(m.CostTiers) > 0 {
			fmt.Fprintf(&buf, "\t\tCostTiers:             %s,\n", costTiersLiteral(m.CostTiers))
		}
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
// Metadata generation (models_metadata_gen.go) + join-disagreement report
// --------------------------------------------------------------------------

// outputMetadataPath is the file generateMetadataSource writes: the baked models.dev
// entity-metadata catalog (models.json view). It populates bakedEntityMetadata via
// init(), mirroring how models_static_gen.go owns staticModels.
const outputMetadataPath = "models_metadata_gen.go"

// generateMetadataSource renders the baked models.dev metadata rows as
// models_metadata_gen.go. It is self-contained about the bake conventions: every row's
// Source is set to DataSourceModelsDev and LastSynced to "" (a baked row always loses a
// most-recent-wins merge to any synced row), and rows are ordered by an EXPLICIT
// sort.Slice on MetadataID.
//
// The MetadataID sort is the determinism guard: the metadata arrives from a Go map
// (non-deterministic iteration), so a missing sort would make the emission vary run to
// run. The generated file assigns bakedEntityMetadata inside init() rather than
// re-declaring it (the var is declared, empty, in metadata_join.go).
func generateMetadataSource(meta []bestiary.EntityMetadata) ([]byte, error) {
	baked := make([]bestiary.EntityMetadata, len(meta))
	copy(baked, meta)
	for i := range baked {
		baked[i].Source = bestiary.DataSourceModelsDev
		baked[i].LastSynced = ""
	}
	// Determinism: explicit MetadataID order — NOT a first-seen aggregate order.
	sort.Slice(baked, func(i, j int) bool {
		return baked[i].MetadataID < baked[j].MetadataID
	})

	var buf bytes.Buffer
	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
	buf.WriteString("\n")
	buf.WriteString("package bestiary\n\n")
	buf.WriteString("// init populates the compiled-in models.dev entity-metadata catalog. The\n")
	buf.WriteString("// bakedEntityMetadata var is declared (empty) in metadata_join.go and owned\n")
	buf.WriteString("// here, exactly as models_static_gen.go owns staticModels. Rows are ordered by\n")
	buf.WriteString("// MetadataID; Source is DataSourceModelsDev and LastSynced is empty so a baked\n")
	buf.WriteString("// row always loses a most-recent-wins merge to a synced row.\n")
	buf.WriteString("func init() {\n")
	buf.WriteString("\tbakedEntityMetadata = []EntityMetadata{\n")
	for _, m := range baked {
		buf.WriteString("\t\t{\n")
		fmt.Fprintf(&buf, "\t\t\tMetadataID:  %q,\n", string(m.MetadataID))
		fmt.Fprintf(&buf, "\t\t\tName:        %q,\n", m.Name)
		fmt.Fprintf(&buf, "\t\t\tDescription: %q,\n", m.Description)
		fmt.Fprintf(&buf, "\t\t\tLicense:     %q,\n", m.License)
		// RawFamily: internal upstream-family provenance used by the join's family-presence
		// gate. Always emit (empty is a valid zero value) so the bake is deterministic.
		fmt.Fprintf(&buf, "\t\t\tRawFamily:   %q,\n", string(m.RawFamily))
		if len(m.Links) > 0 {
			fmt.Fprintf(&buf, "\t\t\tLinks:       %s,\n", modelLinksLiteral(m.Links))
		}
		if len(m.Benchmarks) > 0 {
			fmt.Fprintf(&buf, "\t\t\tBenchmarks:  %s,\n", benchmarksLiteral(m.Benchmarks))
		}
		fmt.Fprintf(&buf, "\t\t\tSource:      %q,\n", string(m.Source))
		fmt.Fprintf(&buf, "\t\t\tLastSynced:  %q,\n", m.LastSynced)
		buf.WriteString("\t\t},\n")
	}
	buf.WriteString("\t}\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format models_metadata_gen.go: %w\n"+
				"  What: the generated metadata source is not syntactically valid\n"+
				"  Why: a codegen template bug produced invalid Go\n"+
				"  How to fix: inspect the unformatted buffer for syntax errors\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err, truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}

// linkTypeExpr renders a LinkType as its exported constant name so the generated
// source references the enum symbolically (e.g. LinkBlog) rather than by integer
// value. Mirrors derivationKindExpr/quantExpr. An out-of-range value falls back to
// LinkOther defensively (the Other-at-zero fail-safe for this element enum).
func linkTypeExpr(t bestiary.LinkType) string {
	switch t {
	case bestiary.LinkAnnouncement:
		return "LinkAnnouncement"
	case bestiary.LinkBlog:
		return "LinkBlog"
	case bestiary.LinkDocs:
		return "LinkDocs"
	case bestiary.LinkLicense:
		return "LinkLicense"
	case bestiary.LinkModelCard:
		return "LinkModelCard"
	case bestiary.LinkPaper:
		return "LinkPaper"
	case bestiary.LinkWeights:
		return "LinkWeights"
	default:
		return "LinkOther"
	}
}

// modelLinksLiteral renders a []ModelLink as a Go composite literal, preserving the
// upstream (links then folded weights) order the parser produced. The generated file
// is package bestiary, so ModelLink and LinkType constants are referenced unqualified.
func modelLinksLiteral(links []bestiary.ModelLink) string {
	parts := make([]string, len(links))
	for i, l := range links {
		parts[i] = fmt.Sprintf(
			"{Label: %q, URL: %q, Type: %s, TypeRaw: %q}",
			l.Label, l.URL, linkTypeExpr(l.Type), l.TypeRaw,
		)
	}
	return "[]ModelLink{" + strings.Join(parts, ", ") + "}"
}

// benchmarksLiteral renders a []BenchmarkResult as a Go composite literal, preserving
// the upstream array order. Score is emitted via scoreLiteral (a %g-canonical float
// literal); a non-numeric upstream score rides on ScoreRaw with Score == 0.
func benchmarksLiteral(bs []bestiary.BenchmarkResult) string {
	parts := make([]string, len(bs))
	for i, b := range bs {
		parts[i] = fmt.Sprintf(
			"{Name: %q, Version: %q, Variant: %q, Dataset: %q, Harness: %q,"+
				" Metric: %q, Score: %s, ScoreRaw: %q, SourceURL: %q, Date: %q}",
			b.Name, b.Version, b.Variant, b.Dataset, b.Harness,
			b.Metric, scoreLiteral(b.Score), b.ScoreRaw, b.SourceURL, b.Date,
		)
	}
	return "[]BenchmarkResult{" + strings.Join(parts, ", ") + "}"
}

// scoreLiteral renders a float64 benchmark score as a canonical, byte-stable Go
// float literal (shortest %g round-trip, e.g. 0, 88.5, 91.2). Using strconv.FormatFloat
// with 'g'/-1 avoids locale/format drift across codegen runs.
func scoreLiteral(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// buildEntitySet builds the minimal entity set the join needs from the freshly
// decomposed models: one Entity per distinct identity key (Family, Variant, Version,
// ParamSize, identity-class Modifier), constructed EXACTLY as registry.loadEntityIndex
// builds its keys. Only Ref matters for the unlinked computation (JoinEntityMetadata
// keys entities by Ref.String() and tests family presence by Ref.Family), so instances
// and aggregates are intentionally omitted. This mirrors the registry's ref
// construction without depending on the compiled-in staticModels.
func buildEntitySet(models []bestiary.ModelInfo) []bestiary.Entity {
	// Pre-pass: the raw (pre-fold) entity-key set, so the MERGE-only N->N.0 version fold
	// can ask whether a bare-integer version's dotted sibling exists — the SAME condition
	// the runtime registry (loadEntityIndex) uses, via the SAME shared primitive
	// (bestiary.NormalizeEntityVersion), so the generated Entity__ constants can never
	// drift from the entities Entities() exposes.
	rawKeys := make(map[string]struct{}, len(models))
	for _, m := range models {
		ref := bestiary.EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: m.ParamSize,
			Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
		}
		rawKeys[ref.String()] = struct{}{}
	}

	seen := make(map[string]struct{}, len(models))
	var ents []bestiary.Entity
	for _, m := range models {
		ref := bestiary.EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: m.ParamSize,
			Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
		}
		if dotted, merged := bestiary.NormalizeEntityVersion(ref, rawKeys); merged {
			ref = dotted
		}
		key := ref.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ents = append(ents, bestiary.Entity{Ref: ref})
	}
	return ents
}

// ModelsdevUnlinkedEnvelope is the committed shape of parse/data/modelsdev_unlinked.json:
// the sorted list of metadata ids whose decomposed family IS present among the catalog
// entities but whose full tuple matched no entity (a curator resolves each with a
// modelsdev_aliases.json entry). It carries NO wall-clock timestamp so the committed
// report is byte-deterministic across regens (unlike the cache-dir parse_failures.json).
type ModelsdevUnlinkedEnvelope struct {
	Comment       string   `json:"_comment"`
	SchemaVersion int      `json:"schema_version"`
	Count         int      `json:"count"`
	Unlinked      []string `json:"unlinked"`
}

// writeModelsdevUnlinked runs the metadata↔entity join over the freshly decomposed
// entity set and writes the sorted disagreement report to modelsdevUnlinkedFile. It is
// the codegen-emitted companion to the (empty-by-default) modelsdev_aliases.json: each
// listed id is a join miss a curator triages into an alias. The report is deterministic
// (sorted, no timestamp) so a clean regen never churns it.
func writeModelsdevUnlinked(models []bestiary.ModelInfo, meta []bestiary.EntityMetadata) error {
	ents := buildEntitySet(models)
	_, unlinked, _ := bestiary.JoinEntityMetadata(ents, meta)

	ids := make([]string, 0, len(unlinked))
	for _, id := range unlinked {
		ids = append(ids, string(id))
	}
	sort.Strings(ids)

	envelope := ModelsdevUnlinkedEnvelope{
		Comment: "Codegen-emitted models.dev join-disagreement report (DO NOT EDIT). Each id is a " +
			"metadata row whose decomposed family IS present in the catalog but whose full identity " +
			"tuple matched no entity; a curator resolves it with a parse/data/modelsdev_aliases.json " +
			"entry. Regenerated by `go generate ./...`; sorted and timestamp-free for byte-stability. " +
			"CAUTION before aliasing a row here: verify the target is a DISTINCT entity — some ids collapse " +
			"under the current decomposition onto a COARSE entity already shared by other lab models, and " +
			"aliasing those would OVERWRITE another row's metadata or misattribute it to a whole family. " +
			"See the modelsdev_aliases.json _comment for the collision-hazard rule.",
		SchemaVersion: 1,
		Count:         len(ids),
		Unlinked:      ids,
	}
	if envelope.Unlinked == nil {
		envelope.Unlinked = []string{}
	}

	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("writeModelsdevUnlinked: marshal JSON: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(modelsdevUnlinkedFile, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeModelsdevUnlinked: write %s: %w\n"+
				"  How to fix: ensure %s is writable",
			modelsdevUnlinkedFile, err, filepath.Dir(modelsdevUnlinkedFile),
		)
	}
	return nil
}

// --------------------------------------------------------------------------
// Creator-dimension committed emissions.
//
// Both reports follow the writeModelsdevUnlinked contract (INV3, AGENTS.md:138):
// NO wall-clock timestamp, an EXPLICIT sort in output position, and a non-nil empty
// slice rather than a JSON null, so a clean regen never churns the committed bytes.
// Each is split into a PURE build* function returning the exact bytes and a thin
// write* wrapper, so the codegen reproducibility harness can exercise the emission
// itself — the two .go-only codegen guards cannot reach a file writer.
// --------------------------------------------------------------------------

// CreatorProviderUnservedRow is one curated Creator→Provider distribution pair that
// matched no served instance in this run's catalog.
type CreatorProviderUnservedRow struct {
	Creator  string `json:"creator"`
	Provider string `json:"provider"`
}

// CreatorProvidersUnservedEnvelope is the committed shape of
// parse/data/creator_providers_unserved.json.
type CreatorProvidersUnservedEnvelope struct {
	Comment       string                       `json:"_comment"`
	SchemaVersion int                          `json:"schema_version"`
	Count         int                          `json:"count"`
	Unserved      []CreatorProviderUnservedRow `json:"unserved"`
}

// buildCreatorProvidersUnserved is the pure emission: it joins the curated
// Creator→[]Provider relation against THIS run's models and returns the report bytes
// for every curated pair that serves no instance of any family belonging to that
// creator.
//
// It sweeps the MODELS, not buildEntitySet's entities: a codegen-side entity carries
// only its Ref (the constants emitter needs nothing else), so its Providers slice is
// always empty and joining against it would report every curated pair as unserved.
// A ModelInfo is the provider-scoped row, so (m.Family, m.Provider) is exactly the
// served pair this report is about.
//
// A listed pair is not automatically wrong — a lab may have a real hosting surface
// this catalog snapshot happens not to cover — but it IS curation that can have no
// effect on resolution, so it must be visible rather than silent.
func buildCreatorProvidersUnserved(models []bestiary.ModelInfo) ([]byte, error) {
	served := make(map[bestiary.Creator]map[bestiary.Provider]struct{})
	for _, m := range models {
		c := m.Family.Creator()
		if c == bestiary.CreatorNone || m.Provider == "" {
			continue
		}
		if served[c] == nil {
			served[c] = make(map[bestiary.Provider]struct{})
		}
		served[c][m.Provider] = struct{}{}
	}

	rows := make([]CreatorProviderUnservedRow, 0)
	for _, c := range bestiary.Creators() {
		for _, p := range c.Providers() {
			if _, ok := served[c][p]; ok {
				continue
			}
			rows = append(rows, CreatorProviderUnservedRow{Creator: string(c), Provider: string(p)})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Creator != rows[j].Creator {
			return rows[i].Creator < rows[j].Creator
		}
		return rows[i].Provider < rows[j].Provider
	})

	envelope := CreatorProvidersUnservedEnvelope{
		Comment: "Codegen-emitted creator-distribution coverage report (DO NOT EDIT). Each row is a " +
			"curated parse/data/creator_providers.json pair whose provider serves NO instance of any " +
			"family this creator is mapped to, so the pair cannot influence creator-first resolution. " +
			"A row is a prompt to check the curation, not proof it is wrong: a lab may operate a real " +
			"surface this catalog snapshot does not cover. Regenerated by `go generate ./...`; sorted " +
			"and timestamp-free for byte-stability. An EMPTY list is the healthy steady state.",
		SchemaVersion: 1,
		Count:         len(rows),
		Unserved:      rows,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("buildCreatorProvidersUnserved: marshal JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// writeCreatorProvidersUnserved writes buildCreatorProvidersUnserved's bytes to
// creatorProvidersUnservedFile.
func writeCreatorProvidersUnserved(models []bestiary.ModelInfo) error {
	data, err := buildCreatorProvidersUnserved(models)
	if err != nil {
		return err
	}
	if err := os.WriteFile(creatorProvidersUnservedFile, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeCreatorProvidersUnserved: write %s: %w\n"+
				"  How to fix: ensure %s is writable",
			creatorProvidersUnservedFile, err, filepath.Dir(creatorProvidersUnservedFile),
		)
	}
	return nil
}

// CreatorsLabDisagreementsEnvelope is the committed shape of
// parse/data/creators_lab_disagreements.json.
type CreatorsLabDisagreementsEnvelope struct {
	Comment       string                            `json:"_comment"`
	SchemaVersion int                               `json:"schema_version"`
	Count         int                               `json:"count"`
	Disagreements []bestiary.CreatorLabDisagreement `json:"disagreements"`
}

// buildCreatorsLabDisagreements is the pure emission for the models.dev lab-prefix
// derivation: it returns the report bytes listing every family whose lab evidence was
// NOT auto-applied to the Family→Creator dimension, with the conflict class and the
// reason.
//
// It is deliberately NON-FATAL. A disagreement is the normal, expected output of
// running a mechanical derivation over a catalog that genuinely disagrees with
// itself; aborting codegen on one would make an ordinary upstream re-publication
// (say, a lab's weights appearing under a second lab's prefix) break the build.
// DeriveCreatorLabDisagreements does the sorting, so no ordering pass is needed here.
func buildCreatorsLabDisagreements(meta []bestiary.EntityMetadata) ([]byte, error) {
	rows := bestiary.DeriveCreatorLabDisagreements(meta)
	if rows == nil {
		rows = []bestiary.CreatorLabDisagreement{}
	}
	envelope := CreatorsLabDisagreementsEnvelope{
		Comment: "Codegen-emitted models.dev lab-derivation disagreement report (DO NOT EDIT). Each row " +
			"is a family whose lab evidence was NOT applied to parse/data/creators.json, with the class " +
			"of conflict: 'multi-org' (more than one lab prefix reaches the family), 'spelling-variant' " +
			"(the lab prefix and the curated creator are prefix-related spellings of one organization), " +
			"'divergent' (they name materially different organizations) or 'withheld' (a deliberate, " +
			"explained deferral listed in the creators.json 'withheld' array). Rows are a triage queue " +
			"for a curator, NOT a build failure: a catalog that disagrees with itself is the normal case. " +
			"Regenerated by `go generate ./...`; sorted by family and timestamp-free for byte-stability.",
		SchemaVersion: 1,
		Count:         len(rows),
		Disagreements: rows,
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("buildCreatorsLabDisagreements: marshal JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// writeCreatorsLabDisagreements writes buildCreatorsLabDisagreements's bytes to
// creatorsLabDisagreementsFile.
func writeCreatorsLabDisagreements(meta []bestiary.EntityMetadata) error {
	data, err := buildCreatorsLabDisagreements(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(creatorsLabDisagreementsFile, data, 0o644); err != nil {
		return fmt.Errorf(
			"writeCreatorsLabDisagreements: write %s: %w\n"+
				"  How to fix: ensure %s is writable",
			creatorsLabDisagreementsFile, err, filepath.Dir(creatorsLabDisagreementsFile),
		)
	}
	return nil
}
