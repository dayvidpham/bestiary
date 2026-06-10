package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/dayvidpham/bestiary"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bestiary <list|show|providers|sources|sync> [flags]")
	}

	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	provider := fs.String("provider", "", "filter by provider slug")
	// --by-entity (show only) switches `show` from per-model rendering to the
	// aggregate entity view: all provider/host instances of one model identity
	// rolled up with their price/context/capability ranges and lineage.
	byEntity := fs.Bool("by-entity", false, "show the aggregate entity view (show command only)")
	// --output selects the output rendering format (json, yaml, table).
	// NOTE: formerly --format in v0.0.1; renamed to --output in v0.0.2 to
	// free --format for the input-scheme selection. See MIGRATION Section 11.
	output := fs.String("output", "json", "output format: json, yaml, table")
	dbPath := fs.String("db-path", "", "SQLite database path (default: XDG_CACHE_HOME/bestiary/models.db)")
	// --format selects the input scheme for model ID parsing (show command only).
	// Default is "peasant" (bestiary canonical form). Other forms require explicit selection.
	// Accepted values: peasant, huggingface, hf, purl, raw.
	inputFormat := fs.String("format", "peasant", "input format for model ID: peasant (default), huggingface (hf), purl, raw")
	// --scheme is kept for backward compatibility with v0.0.1 scripts.
	// When --scheme is set and --format is not explicitly set, --scheme takes effect.
	// --format takes precedence over --scheme when both are provided.
	scheme := fs.String("scheme", "", "DEPRECATED: use --format instead; scheme for model ID resolution: canonical, huggingface, purl, raw")
	// --quant filters the entity instance views (providers, show --by-entity) to the
	// instances that carry a per-quantization VRAM row matching the given quant
	// (e.g. q4_k_m, f16). It is parsed via bestiary.ParseQuantization, which rejects
	// an unrecognised value with an actionable error rather than silently ignoring it.
	quant := fs.String("quant", "", "filter instances by quantization (e.g. q4_k_m, f16); applies to providers and show --by-entity")

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	switch cmd {
	case "list":
		return runList(*provider, bestiary.OutputFormat(*output), *dbPath)
	case "show":
		if *byEntity {
			if fs.NArg() < 1 {
				return fmt.Errorf("usage: bestiary show --by-entity <model-id | family[/variant][/version|@version]{identity-mods}> [--output=<json|table>]")
			}
			return runShowEntity(fs.Arg(0), bestiary.OutputFormat(*output), *quant)
		}
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary show <model-id> [--format=<peasant|huggingface|hf|purl|raw>] [--output=<json|yaml|table>] [flags]")
		}
		return runShow(fs.Arg(0), bestiary.OutputFormat(*output), *dbPath, *inputFormat, *scheme)
	case "providers":
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary providers <family>[/<variant>][/<version>|@<version>]{identity-mods} [--output=<json|table>]\n" +
				"  version may be given as a trailing /segment or as @version; the optional [attributes] filter is ignored in MVP")
		}
		return runProviders(fs.Arg(0), bestiary.OutputFormat(*output), *quant)
	case "sources":
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary sources <family>[/<variant>][@<version>][#<paramsize>]{identity-mods} [--output=<json|table>]\n" +
				"  prints the per-source ingest provenance (uri via FK join, ingest date, parser schema) that attests the entity, sorted by source")
		}
		return runSources(fs.Arg(0), bestiary.OutputFormat(*output))
	case "sync":
		return runSync(*provider, bestiary.OutputFormat(*output), *dbPath)
	default:
		return fmt.Errorf("unknown command %q; supported commands: list, show, providers, sources, sync", cmd)
	}
}

// resolveDBPath returns dbPath if non-empty, otherwise calls DefaultDBPath().
func resolveDBPath(dbPath string) (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	path, err := bestiary.DefaultDBPath()
	if err != nil {
		return "", fmt.Errorf("resolve default DB path: %w", err)
	}
	return path, nil
}

// runList lists models from static registry merged with any cached models.
// Gracefully falls back to static-only if the store cannot be opened.
func runList(provider string, format bestiary.OutputFormat, dbPath string) error {
	// Fetch static models, optionally filtered by provider.
	var static []bestiary.ModelInfo
	if provider != "" {
		static = bestiary.ModelsByProvider(bestiary.Provider(provider))
	} else {
		static = bestiary.StaticModels()
	}

	// Attempt to open store for cached models — fall back gracefully on error.
	var cached []bestiary.ModelInfo
	path, err := resolveDBPath(dbPath)
	if err == nil {
		store, err := bestiary.OpenStore(path)
		if err == nil {
			defer store.Close()
			cached, err = store.QueryModels(context.Background(), bestiary.Provider(provider))
			if err != nil {
				return fmt.Errorf("query cached models: %w", err)
			}
		}
		// If store can't be opened, cached remains nil — static-only is fine.
	}

	merged := bestiary.MergeModels(static, cached)
	return bestiary.FormatModels(os.Stdout, merged, format)
}

// runShow resolves a model by input string and prints it in the requested format.
//
// In the default (peasant/canonical) format the input mirrors canonical output:
// "<provider>/<family>[/<variant>][/<version>][@<date>]{identity-mods}[attributes]".
// The "{identity-mods}" brace segment and the optional trailing "[attributes]"
// bracket segment are both consumed by Resolve (matchCanonicalSegments); the union
// of their tokens must equal the model's modifier set, so a class-aware render such
// as "openai/gpt/4o{instruct}[turbo]" round-trips back to its model.
//
// Three Resolve outcomes are handled:
//
//   - Single canonical (cross-provider OK): print the best (most-recent) entry.
//   - *ErrAmbiguous: print a candidate table to stderr and return non-zero.
//   - *ErrNotFound: return the error directly.
//
// The static registry is authoritative for scheme-based lookups; the SQLite
// cache is consulted for most-recent-wins selection. Falls back to static-only
// when the store cannot be opened.
//
// inputFormatFlag: value of --format flag (peasant/huggingface/hf/purl/raw).
// schemeFlag: value of deprecated --scheme flag; used only when inputFormatFlag is "peasant" (default).
func runShow(input string, format bestiary.OutputFormat, dbPath string, inputFormatFlag string, schemeFlag string) error {
	// Build Resolve options from flags.
	// --format takes precedence. If --format is explicitly non-peasant, use it.
	// If --format is "peasant" (default) and --scheme is set, honour legacy --scheme.
	var resolveOpts []bestiary.ResolveOption

	if inputFormatFlag != "" && inputFormatFlag != "peasant" {
		// Explicit non-default --format: parse and dispatch directly.
		ifmt, err := bestiary.ParseInputFormat(inputFormatFlag)
		if err != nil {
			return err
		}
		resolveOpts = append(resolveOpts, bestiary.WithInputFormat(ifmt))
	} else if schemeFlag != "" {
		// Legacy --scheme flag (deprecated): translate to WithScheme.
		s, err := bestiary.ParseScheme(schemeFlag)
		if err != nil {
			return err
		}
		resolveOpts = append(resolveOpts, bestiary.WithScheme(s))
	} else {
		// Default: peasant (canonical) form only — no auto-detect.
		resolveOpts = append(resolveOpts, bestiary.WithInputFormat(bestiary.InputFormatPeasant))
	}

	refs, resolveErr := bestiary.Resolve(input, resolveOpts...)
	if resolveErr != nil {
		var ambig *bestiary.ErrAmbiguous
		if errors.As(resolveErr, &ambig) {
			// Print a candidate table to stderr; do not pollute stdout.
			bestiary.FormatAmbiguous(os.Stderr, ambig)
			return fmt.Errorf("ambiguous input %q matched %d canonicals — use --format=raw or refine to a more specific canonical form", input, len(ambig.Candidates))
		}
		// ErrNotFound or other errors pass through directly.
		return resolveErr
	}

	// Resolve returned one or more refs (cross-provider hosting of same canonical).
	// Gather full ModelInfo for each ref from static registry and/or cache.
	// Pick the best entry: prefer the one with the most-recent LastSynced.
	//
	// Try to open the store for cached data; fall back gracefully on error.
	// Use QueryModel (per-ID lookup) instead of QueryModels("") (load-all) to
	// avoid loading the full cache into memory for a single-model show operation.
	var store *bestiary.Store
	path, dbErr := resolveDBPath(dbPath)
	if dbErr == nil {
		if s, openErr := bestiary.OpenStore(path); openErr == nil {
			store = s
			defer store.Close()
		}
	}

	var best bestiary.ModelInfo
	found := false
	ctx := context.Background()
	for _, ref := range refs {
		// Look up by (Provider, ID) to respect the canonical-provider preference
		// applied by Resolve. Using LookupModel(ID) alone would return the first
		// model in the registry with that ID, ignoring the provider filter.
		staticModel, inStatic := bestiary.LookupModelByProvider(ref.Provider, string(ref.ID))

		var cachedModel bestiary.ModelInfo
		inCached := false
		if store != nil {
			if m, qErr := store.QueryModel(ctx, ref.ID); qErr == nil {
				// Filter cached model by provider as well.
				if m.Provider == ref.Provider {
					cachedModel = m
					inCached = true
				}
			}
		}

		var candidate bestiary.ModelInfo
		switch {
		case inStatic && inCached:
			if cachedModel.LastSynced > staticModel.LastSynced {
				candidate = cachedModel
			} else {
				candidate = staticModel
			}
		case inStatic:
			candidate = staticModel
		case inCached:
			candidate = cachedModel
		default:
			continue
		}

		if !found || candidate.LastSynced > best.LastSynced {
			best = candidate
			found = true
		}
	}

	if !found {
		return &bestiary.ErrNotFound{What: "model", Key: input}
	}
	return bestiary.FormatModel(os.Stdout, best, format)
}

// parseEntityTuple parses an entity identity tuple of the canonical form
//
//	family[/variant][@version][#paramsize]{identity-mods}[attributes]
//
// returning the (family, variant, version, paramSize, identity-modifiers) components.
// This mirrors EntityRef.String()'s rendering so that a key printed by the entity
// layer round-trips back through this parser. The optional trailing "[attributes]"
// bracket segment is recognized and discarded (attributes never affect identity,
// and the MVP entity lookup ignores them). The "{identity-mods}" brace tokens are
// split on commas and passed through verbatim; EntityByTuple re-projects them via
// EntityModifiers, so attribute-class tokens supplied here are dropped at lookup.
//
// Strip order: [attrs] -> {mods} -> #size -> @version -> /variant
// The #size strip happens BEFORE the @-LastIndex version split so that a '#'
// in the size token never confuses the version parser.
//
// It returns an error only when the family segment is empty.
func parseEntityTuple(input string) (fam bestiary.Family, variant, version, paramSize string, mods []string, err error) {
	s := input

	// Strip the trailing "[attributes]" segment (ignored in MVP) before anything
	// else so its contents cannot be confused with a brace/version segment.
	if lb := strings.LastIndex(s, "["); lb >= 0 {
		if rb := strings.LastIndex(s, "]"); rb == len(s)-1 && rb > lb {
			s = s[:lb]
		}
	}

	// Strip and capture the "{identity-mods}" segment.
	if lb := strings.LastIndex(s, "{"); lb >= 0 {
		if rb := strings.LastIndex(s, "}"); rb == len(s)-1 && rb > lb {
			for _, t := range strings.Split(s[lb+1:rb], ",") {
				if t = strings.TrimSpace(t); t != "" {
					mods = append(mods, t)
				}
			}
			s = s[:lb]
		}
	}

	// Strip and capture the "#paramsize" segment. Must be done BEFORE the
	// @-LastIndex version split — the size strip is intentionally ordered here
	// so a '#' token never reaches the version parser.
	// Canonicalize to lowercase via ParseParamSize so that "llama@3.3#70B" and
	// "llama@3.3#70b" resolve to the same entity key. Non-size tokens (empty,
	// unrecognized shapes) are left as-is; the lookup will simply miss, which
	// is the correct behavior for a malformed size token.
	if hash := strings.LastIndex(s, "#"); hash >= 0 {
		raw := s[hash+1:]
		s = s[:hash]
		if canonical, err := bestiary.ParseParamSize(raw); err == nil {
			paramSize = canonical
		} else {
			paramSize = raw // pass through verbatim; lookup will miss
		}
	}

	// Strip and capture the "@version" segment (identity version, not a date).
	if at := strings.LastIndex(s, "@"); at >= 0 {
		version = s[at+1:]
		s = s[:at]
	}

	segs := strings.Split(s, "/")
	if len(segs) == 0 || segs[0] == "" {
		return "", "", "", "", nil, fmt.Errorf(
			"parse entity tuple %q: empty family segment; expected family[/variant][@version][#paramsize]{identity-mods}",
			input,
		)
	}
	fam = bestiary.Family(segs[0])
	if len(segs) >= 2 {
		variant = segs[1]
	}
	// A third path segment is accepted as the version for leniency, but only when
	// no explicit @version was given (EntityRef renders version via @).
	if len(segs) >= 3 && version == "" {
		version = segs[2]
	}
	return fam, variant, version, paramSize, mods, nil
}

// lookupEntity resolves the show/providers argument to an entity. It first tries
// to parse the argument as an identity tuple; on a miss it falls back to treating
// the argument as a concrete model ID (deriving that model's identity tuple), so
// both `claude/opus@4.5` and `claude-opus-4-5-20251101` resolve to the same
// entity.
func lookupEntity(arg string) (bestiary.Entity, bool) {
	if fam, variant, version, paramSize, mods, err := parseEntityTuple(arg); err == nil {
		if e, ok := bestiary.EntityByTuple(fam, variant, version, paramSize, mods...); ok {
			return e, true
		}
	}
	// Fallback: the argument may be a concrete model ID rather than a tuple.
	// Use m.ParamSize so sized model rows resolve to their sized entity key.
	if m, ok := bestiary.LookupModel(bestiary.ModelID(arg)); ok {
		return bestiary.EntityByTuple(m.Family, m.Variant, m.Version, m.ParamSize, m.Modifier...)
	}
	return bestiary.Entity{}, false
}

// validateEntityOutput restricts the entity commands to the output formats they
// can actually render (json or table). Unlike the model commands, there is no
// YAML serializer for Entity, so any other value — including a typo such as
// "tabel" or an unsupported "yaml" — is rejected with an actionable error rather
// than silently falling through to the table renderer.
func validateEntityOutput(format bestiary.OutputFormat) error {
	switch format {
	case bestiary.FormatJSON, bestiary.FormatTable:
		return nil
	default:
		// No "bestiary:" prefix here — main() already prepends "bestiary: %v",
		// so embedding it would render a doubled prefix. The sibling inline errors
		// in this file (usage strings, unknown-command) omit it for the same reason.
		return fmt.Errorf(
			"unsupported --output %q for entity commands; supported formats: json, table",
			string(format),
		)
	}
}

// runProviders lists every provider/host instance of the entity identified by the
// given tuple (or model ID). When quantFlag is non-empty the instance set is
// filtered to those carrying a QuantVRAM row matching that quantization; an
// unrecognised quantFlag is rejected with an actionable error (never silently
// ignored or mapped to QuantizationOther).
func runProviders(arg string, format bestiary.OutputFormat, quantFlag string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	quant, filter, err := parseQuantFilter(quantFlag)
	if err != nil {
		return err
	}
	ent, ok := lookupEntity(arg)
	if !ok {
		return &bestiary.ErrNotFound{What: "entity", Key: arg}
	}
	insts := ent.Instances
	if filter {
		insts = filterInstancesByQuant(insts, quant)
	}
	if format == bestiary.FormatJSON {
		return writeJSON(os.Stdout, insts)
	}
	fmt.Fprintf(os.Stdout, "Entity: %s\n", ent.Ref.String())
	writeInstanceTable(os.Stdout, insts)
	return nil
}

// runSources prints the per-source ingest provenance attesting the entity
// identified by arg: one record per attesting data source (from Entity.Sources),
// each JOINED across the BCNF provenance tables — uri + canonical-name from the
// DataSource dimension (reached by FK on the source id, never duplicated onto the
// ingest row) and ingested-at + parser-schema from the DatasetIngested current
// ingest. Records are sorted ascending by source id. This is the subcommand's own
// SOURCE|URI|INGESTED|PARSER view; it deliberately does NOT add a SOURCE column to
// the show/list instance tables (deferred). An unknown key yields an actionable
// ErrNotFound.
func runSources(arg string, format bestiary.OutputFormat) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	ent, ok := lookupEntity(arg)
	if !ok {
		return &bestiary.ErrNotFound{What: "entity", Key: arg}
	}
	rows := sourceProvenanceRows(ent.Sources)
	if format == bestiary.FormatJSON {
		return writeJSON(os.Stdout, rows)
	}
	fmt.Fprintf(os.Stdout, "Entity: %s\n", ent.Ref.String())
	writeSourceTable(os.Stdout, rows)
	return nil
}

// runShowEntity renders the aggregate view of one entity: its identity, rolled-up
// provider/host lists, price/context/max-output ranges, capability union, lineage
// edges, and the underlying instances.
func runShowEntity(arg string, format bestiary.OutputFormat, quantFlag string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	quant, filter, err := parseQuantFilter(quantFlag)
	if err != nil {
		return err
	}
	ent, ok := lookupEntity(arg)
	if !ok {
		return &bestiary.ErrNotFound{What: "entity", Key: arg}
	}
	if filter {
		ent.Instances = filterInstancesByQuant(ent.Instances, quant)
	}
	if format == bestiary.FormatJSON {
		return writeJSON(os.Stdout, ent)
	}
	writeEntityView(os.Stdout, ent)
	return nil
}

// parseQuantFilter interprets the --quant flag value. An empty string means "no
// filter" and returns filter=false. A non-empty value is parsed via
// bestiary.ParseQuantization, which NEVER silently maps an unrecognised token to
// QuantizationOther — an unknown value returns an actionable error so the caller
// learns their filter was rejected rather than silently matching nothing.
func parseQuantFilter(flag string) (q bestiary.Quantization, filter bool, err error) {
	if flag == "" {
		return bestiary.QuantizationNone, false, nil
	}
	q, err = bestiary.ParseQuantization(flag)
	if err != nil {
		return bestiary.QuantizationNone, false, err
	}
	return q, true, nil
}

// filterInstancesByQuant keeps only the instances that carry at least one
// QuantVRAM row whose Quant equals q; instances with no matching quant row are
// dropped entirely. The returned slice is freshly allocated and the matched
// instances retain their full QuantVRAM lists (the filter selects instances, it
// does not prune their rows). The input slice is never mutated.
func filterInstancesByQuant(insts []bestiary.ProviderInstance, q bestiary.Quantization) []bestiary.ProviderInstance {
	out := make([]bestiary.ProviderInstance, 0, len(insts))
	for _, in := range insts {
		for _, qv := range in.QuantVRAM {
			if qv.Quant == q {
				out = append(out, in)
				break
			}
		}
	}
	return out
}

// sourceProvenance is one attesting data source for an entity, joined across the
// BCNF provenance tables. URI and CanonicalName come from the DataSource dimension
// (reached by FK on Source) — the uri is obtained ONLY via this join and is never
// duplicated onto the ingest row. IngestedAt and ParserSchema come from the
// DatasetIngested current-ingest fact for the same source.
type sourceProvenance struct {
	Source        bestiary.DataSourceID `json:"source"`
	URI           string                `json:"uri"`
	CanonicalName string                `json:"canonical_name"`
	IngestedAt    string                `json:"ingested_at"`
	ParserSchema  int                   `json:"parser_schema"`
}

// sourceProvenanceRows builds the joined per-source provenance view for an
// entity's attesting sources. For each source id it resolves the DataSource
// dimension (uri/canonical-name) via the DataSourceByID FK join and the
// DatasetIngested current ingest via DatasetIngestedFor. The result is sorted
// ascending by source id so output ordering is deterministic regardless of the
// order in which Entity.Sources was supplied. A source that fails to resolve (a
// graceful-degrade load failure) keeps its id with empty join fields rather than
// being dropped.
func sourceProvenanceRows(sources []bestiary.DataSourceID) []sourceProvenance {
	rows := make([]sourceProvenance, 0, len(sources))
	for _, id := range sources {
		sp := sourceProvenance{Source: id}
		if ds, ok := bestiary.DataSourceByID(id); ok {
			sp.URI = ds.URI
			sp.CanonicalName = ds.CanonicalName
		}
		if di, ok := bestiary.DatasetIngestedFor(id); ok {
			sp.IngestedAt = di.IngestedAt
			sp.ParserSchema = di.ParserSchema
		}
		rows = append(rows, sp)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Source < rows[j].Source
	})
	return rows
}

// writeSourceTable prints the sources subcommand's SOURCE|URI|INGESTED|PARSER
// table. This is the subcommand's own provenance view and is distinct from the
// deferred show/list instance-table SOURCE column.
func writeSourceTable(w io.Writer, rows []sourceProvenance) {
	fmt.Fprintf(w, "Sources (%d):\n", len(rows))
	fmt.Fprintf(w, "  %-12s %-34s %-22s %8s\n", "SOURCE", "URI", "INGESTED", "PARSER")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-12s %-34s %-22s %8d\n",
			string(r.Source), orDash(r.URI), orDash(r.IngestedAt), r.ParserSchema)
	}
}

// writeJSON marshals v as indented JSON to w.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// fmtPrice renders a *float64 price (per-MTok) as a fixed-precision string, or a
// dash when the value is nil/unknown.
func fmtPrice(p *float64) string {
	if p == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *p)
}

// fmtHost renders a Host, mapping the zero value (HostNone) to a dash.
func fmtHost(h bestiary.Host) string {
	if h == bestiary.HostNone {
		return "-"
	}
	return string(h)
}

// writeInstanceTable prints a fixed-width table of provider instances.
func writeInstanceTable(w io.Writer, insts []bestiary.ProviderInstance) {
	fmt.Fprintf(w, "Instances (%d):\n", len(insts))
	fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10s %10s\n",
		"ID", "PROVIDER", "HOST", "IN/MTok", "OUT/MTok", "CONTEXT", "MAXOUT")
	for _, in := range insts {
		fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10d %10d\n",
			string(in.ID), string(in.Provider), fmtHost(in.Host),
			fmtPrice(in.CostInputPerMTok), fmtPrice(in.CostOutputPerMTok),
			in.ContextWindow, in.MaxOutput)
		writeQuantRows(w, in.QuantVRAM)
	}
}

// writeQuantRows prints the per-quantization VRAM sub-rows for one instance,
// indented beneath its main row. Nothing is printed when the instance carries no
// quant data. The PARTIAL column flags a weights-only VRAM lower bound — true
// means the KV-cache term was excluded because architecture facts were absent, so
// VRAMBytes is not a full estimate.
func writeQuantRows(w io.Writer, rows []bestiary.QuantVRAM) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "      %-10s %15s %15s %10s %8s\n",
		"QUANT", "WEIGHTS", "VRAM", "CTX", "PARTIAL")
	for _, q := range rows {
		fmt.Fprintf(w, "      %-10s %15d %15d %10d %8t\n",
			q.Quant.String(), q.WeightsBytes, q.VRAMBytes, q.VRAMContextTokens, q.VRAMEstimatePartial)
	}
}

// writeEntityView prints the human-readable aggregate entity view.
func writeEntityView(w io.Writer, e bestiary.Entity) {
	fmt.Fprintf(w, "Entity: %s\n", e.Ref.String())
	fmt.Fprintf(w, "  Family:        %s\n", string(e.Ref.Family))
	fmt.Fprintf(w, "  Variant:       %s\n", orDash(e.Ref.Variant))
	fmt.Fprintf(w, "  Version:       %s\n", orDash(e.Ref.Version))
	fmt.Fprintf(w, "  Identity-mods: %s\n", orDash(strings.Join(e.Ref.Modifier, ",")))

	providers := make([]string, len(e.Providers))
	for i, p := range e.Providers {
		providers[i] = string(p)
	}
	hosts := make([]string, len(e.Hosts))
	for i, h := range e.Hosts {
		hosts[i] = fmtHost(h)
	}
	fmt.Fprintf(w, "Providers (%d): %s\n", len(e.Providers), orDash(strings.Join(providers, ", ")))
	fmt.Fprintf(w, "Hosts (%d): %s\n", len(e.Hosts), orDash(strings.Join(hosts, ", ")))

	fmt.Fprintf(w, "Price input  /MTok: %s\n", fmtRangePtr(e.PriceInputRange))
	fmt.Fprintf(w, "Price output /MTok: %s\n", fmtRangePtr(e.PriceOutputRange))
	fmt.Fprintf(w, "Context window:     %s\n", fmtRangeInt(e.ContextRange))
	fmt.Fprintf(w, "Max output:         %s\n", fmtRangeInt(e.MaxOutputRange))
	fmt.Fprintf(w, "Capabilities: %s\n", orDash(strings.Join(capList(e.Capabilities), ", ")))

	fmt.Fprintf(w, "Lineage (%d):\n", len(e.Lineage))
	for _, edge := range e.Lineage {
		fmt.Fprintf(w, "  -> %s %s\n", edge.Kind.String(), edge.Parent.String())
	}

	writeInstanceTable(w, e.Instances)
}

// orDash returns s, or "-" when s is empty.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// fmtRangePtr renders a [min,max] price range, collapsing the all-nil case to a
// dash and a single-valued range to one number.
func fmtRangePtr(r [2]*float64) string {
	if r[0] == nil && r[1] == nil {
		return "-"
	}
	if r[0] != nil && r[1] != nil && *r[0] == *r[1] {
		return fmtPrice(r[0])
	}
	return fmt.Sprintf("[%s, %s]", fmtPrice(r[0]), fmtPrice(r[1]))
}

// fmtRangeInt renders a [min,max] integer range, collapsing equal bounds.
func fmtRangeInt(r [2]int) string {
	if r[0] == r[1] {
		return fmt.Sprintf("%d", r[0])
	}
	return fmt.Sprintf("[%d, %d]", r[0], r[1])
}

// capList returns the names of the capabilities that the union reports as
// supported, in a stable declaration order.
func capList(c bestiary.CapabilityUnion) []string {
	var out []string
	if c.Reasoning {
		out = append(out, "reasoning")
	}
	if c.ToolCall {
		out = append(out, "tool-call")
	}
	if c.Attachment {
		out = append(out, "attachment")
	}
	if c.Temperature {
		out = append(out, "temperature")
	}
	if c.StructuredOutput {
		out = append(out, "structured-output")
	}
	if c.Interleaved {
		out = append(out, "interleaved")
	}
	if c.OpenWeights {
		out = append(out, "open-weights")
	}
	return out
}

// runSync fetches live model data from the API, persists to store, and prints results.
// Unlike list/show, sync requires a functional store (no graceful fallback).
func runSync(provider string, format bestiary.OutputFormat, dbPath string) error {
	ctx := context.Background()

	path, err := resolveDBPath(dbPath)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	client := bestiary.NewClient()

	var fetched []bestiary.ModelInfo
	if provider != "" {
		fetched, err = client.FetchModelsByProvider(ctx, bestiary.Provider(provider))
	} else {
		fetched, err = client.FetchModels(ctx)
	}
	if err != nil {
		return fmt.Errorf("sync: fetch models: %w", err)
	}

	store, err := bestiary.OpenStore(path)
	if err != nil {
		return fmt.Errorf("sync: open store at %s: %w", path, err)
	}
	defer store.Close()

	if err := store.UpsertModels(ctx, fetched); err != nil {
		return fmt.Errorf("sync: persist models: %w", err)
	}

	return bestiary.FormatModels(os.Stdout, fetched, format)
}
