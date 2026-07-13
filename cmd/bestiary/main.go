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
	"time"

	"github.com/dayvidpham/bestiary"
)

// errPrefix is the single namespace prefix the CLI guarantees on every error
// line it prints to stderr.
const errPrefix = "bestiary: "

// modelsDevParserSchema is the curated-data schema version stamped on the
// dataset_ingested row `sync` appends for a models.dev fetch. It tracks
// parse/data/datasources.json's schema_version (3): the ingest row records "the
// curated-data schema version this ingest was parsed under", so a runtime sync
// row and the committed seed rows share the same current schema number. Bump this
// in lockstep with the datasources.json schema_version on the next schema change.
const modelsDevParserSchema = 3

// driftWarningThreshold is the number of live models.dev model IDs that may be
// absent from the embedded (vendored) catalog before `sync` warns that the
// vendored snapshot is materially behind upstream.
//
// The vendored catalog.json is refreshed on-demand (see the "models.dev snapshot
// refresh" workflow in AGENTS.md), so a handful of newly-added upstream models
// between refreshes is expected and harmless — those models still sync into the
// local cache. Only when MORE than this many live model IDs are missing from the
// embedded catalog is the snapshot stale enough to warrant a regen.
//
// A COUNT threshold is deliberately chosen over a ratio or a snapshot-age
// heuristic: it is trivially testable (inject N synthetic new models and assert
// the warning fires only at N > threshold) and avoids a brittle heuristic soup.
// The value is a judgement call justified as "about a provider's worth" of new
// models — a drift that large means several upstream releases have accumulated
// unvendored.
const driftWarningThreshold = 50

// embeddedFallbackNotice is the single stderr line an entity-view command prints
// when it cannot open the SQLite cache and falls back to the embedded (baked)
// catalog. It is emitted at most once per command invocation (see openViewStore).
const embeddedFallbackNotice = errPrefix + "using embedded catalog (run 'bestiary sync' to refresh metadata)"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, renderError(err))
		os.Exit(1)
	}
}

// renderError formats err for the CLI's stderr line with EXACTLY one
// "bestiary: " prefix.
//
// The bestiary package is also a library: its structured errors (ErrNotFound,
// ErrAmbiguous, ErrAPIUnavailable, the ParseQuantization error, …) deliberately
// namespace themselves with "bestiary: " in their Error() string, which is the
// correct, self-describing form for any library consumer. The CLI must not
// double that prefix, so it prints an already-namespaced error verbatim. The
// inline errors raised in this command package (usage strings, unknown-command,
// unsupported-output) carry no prefix on purpose — the CLI supplies the sole one
// here. Either way the user sees one prefix, never "bestiary: bestiary:".
func renderError(err error) string {
	msg := err.Error()
	// A library error already namespaces itself with a LEADING "bestiary: "
	// (ErrNotFound, ErrAmbiguous, the ParseQuantization error, …): print it
	// verbatim — it carries exactly one prefix, at the front.
	if strings.HasPrefix(msg, errPrefix) {
		return msg
	}
	// A context-wrapped library error carries the namespace token in the MIDDLE,
	// after a bare wrapper prefix (e.g. runSync's
	// "sync: open store at <dir>: bestiary: OpenStore: …"). Stripping every
	// embedded "bestiary: " and hoisting a single one to the front collapses the
	// redundancy to exactly one leading prefix, so no path ever renders the
	// doubled "bestiary: bestiary:". A bare inline error contains no token to
	// strip and simply gains the sole prefix.
	if strings.Contains(msg, errPrefix) {
		msg = strings.ReplaceAll(msg, errPrefix, "")
	}
	return errPrefix + msg
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
	// --status filters `list` output to models carrying the given release status
	// (alpha, beta, deprecated, none). It is parsed via bestiary.ParseModelStatus,
	// which rejects an unrecognised value with an actionable error rather than
	// silently matching nothing. Empty (the default) leaves list output unchanged.
	status := fs.String("status", "", "filter list output by release status: none, alpha, beta, deprecated")
	// --history (sources only) prints the full append-only ingest history for every
	// data source, ascending by ingest time, instead of the per-entity provenance view.
	history := fs.Bool("history", false, "sources: print the full ingest history per source (ascending)")
	// --export (sources only) emits the store's ingest provenance as a datasources.json
	// v3 document. The optional positional argument is the output path; with none (or
	// "-") it writes to stdout. When the store is empty or absent it falls back to the
	// curated table.
	export := fs.Bool("export", false, "sources: export ingest provenance as datasources.json v3 (positional path, or stdout)")

	if err := fs.Parse(reorderArgs(fs, args[1:])); err != nil {
		return err
	}

	switch cmd {
	case "list":
		return runList(*provider, bestiary.OutputFormat(*output), *dbPath, *status)
	case "show":
		if *byEntity {
			if fs.NArg() < 1 {
				return fmt.Errorf("usage: bestiary show --by-entity <model-id | family[/variant][/version|@version]{identity-mods}> [--output=<json|table>]")
			}
			return runShowEntity(fs.Arg(0), bestiary.OutputFormat(*output), *quant, *dbPath)
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
		return runProviders(fs.Arg(0), bestiary.OutputFormat(*output), *quant, *dbPath)
	case "sources":
		// --history and --export are catalog-wide ingest-log views; they take no
		// entity positional. The default sources view still requires an entity key.
		if *history {
			return runSourcesHistory(bestiary.OutputFormat(*output), *dbPath)
		}
		if *export {
			return runSourcesExport(*dbPath, fs.Arg(0))
		}
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary sources <family>[/<variant>][@<version>][#<paramsize>]{identity-mods} [--output=<json|table>]\n" +
				"  prints the per-source ingest provenance (uri via FK join, ingest date, parser schema) that attests the entity, sorted by source\n" +
				"  bestiary sources --history [--output=<json|table>]   full append-only ingest history per source (ascending)\n" +
				"  bestiary sources --export [path]                     export ingest provenance as datasources.json v3 (stdout when path omitted)")
		}
		return runSources(fs.Arg(0), bestiary.OutputFormat(*output), *dbPath)
	case "sync":
		return runSync(*provider, bestiary.OutputFormat(*output), *dbPath)
	default:
		return fmt.Errorf("unknown command %q; supported commands: list, show, providers, sources, sync", cmd)
	}
}

// reorderArgs makes flag parsing position-independent. Go's flag package stops
// scanning at the first non-flag argument, so a flag written AFTER the
// positional (e.g. `show KEY --by-entity`) would otherwise be silently dropped.
// This helper partitions args into flags and positionals — preserving the order
// within each group — and returns the flags first, followed by a "--" terminator
// and the positionals, so flag.Parse sees every flag regardless of where the
// user placed it relative to the positional.
//
// Value-bearing flags are handled in both spellings: the joined "--name=value"
// form is moved as a single token, and the separated "--name value" form pulls
// the following token along as its value. Boolean flags (detected via the flag
// package's IsBoolFlag contract) consume no value. An unknown flag is treated as
// value-bearing, which leaves the eventual flag.Parse to reject it with its
// standard unknown-flag error rather than this reordering swallowing it silently.
// A lone "-" and everything after an explicit "--" are treated as positionals.
//
// A value-bearing flag is only ever satisfied by a token that FOLLOWS it; a
// positional typed BEFORE the flag (e.g. `providers KEY --quant`) cannot be its
// value. Such a trailing value-bearing flag with nothing after it is "dangling"
// and must raise the same "flag needs an argument" error the flags-first form
// produces — so it is emitted LAST, with no "--" terminator after it that
// flag.Parse could otherwise greedily consume as a bogus value. Because that
// parse fails before any positional is read, the positionals are intentionally
// dropped from the dangling result: the command errors identically regardless.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, positionals []string
	dangling := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			// A lone "-" (len 1) or any non-dash token is a positional.
			positionals = append(positionals, arg)
			continue
		}
		// arg is a flag token.
		name := strings.TrimLeft(arg, "-")
		if strings.IndexByte(name, '=') >= 0 {
			// "--name=value": value is already attached to this token.
			flags = append(flags, arg)
			continue
		}
		if flagIsBool(fs, name) {
			// Boolean flag: consumes no following token.
			flags = append(flags, arg)
			continue
		}
		// "--name": a value-bearing flag consumes the following token, if one
		// follows it in the original order.
		if i+1 < len(args) {
			flags = append(flags, arg, args[i+1])
			i++
			continue
		}
		// Nothing follows: a dangling value-bearing flag missing its value.
		dangling = arg
	}
	if dangling != "" {
		// Emit the dangling flag last with nothing after it so flag.Parse raises
		// "flag needs an argument: -<name>", matching the flags-first form.
		return append(flags, dangling)
	}
	if len(positionals) == 0 {
		return flags
	}
	// The "--" terminator guards any positional that itself begins with "-".
	return append(append(flags, "--"), positionals...)
}

// flagIsBool reports whether the named flag is a registered boolean flag (one
// that takes no value), using the same IsBoolFlag contract the flag package uses
// internally. An unregistered name reports false so reorderArgs defers the
// rejection of unknown flags to flag.Parse.
func flagIsBool(fs *flag.FlagSet, name string) bool {
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
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
// Gracefully falls back to static-only if the store cannot be opened. When
// statusFlag is non-empty the merged set is filtered to models whose release
// status matches it exactly (parsed via ParseModelStatus, which rejects an
// unknown value with an actionable error); an empty statusFlag leaves the
// default output unchanged.
func runList(provider string, format bestiary.OutputFormat, dbPath, statusFlag string) error {
	// Parse the status filter up front so an unknown value fails fast with an
	// actionable error before any work is done.
	statusFilter := bestiary.StatusNone
	filterStatus := false
	if statusFlag != "" {
		s, err := bestiary.ParseModelStatus(statusFlag)
		if err != nil {
			return err
		}
		statusFilter = s
		filterStatus = true
	}

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
	if filterStatus {
		merged = filterModelsByStatus(merged, statusFilter)
	}
	return bestiary.FormatModels(os.Stdout, merged, format)
}

// filterModelsByStatus returns the models whose Status equals want. The input is
// never mutated; the result is a freshly allocated slice (possibly empty). It is
// an exact-match filter — `--status none` selects models with no declared status.
func filterModelsByStatus(models []bestiary.ModelInfo, want bestiary.ModelStatus) []bestiary.ModelInfo {
	out := make([]bestiary.ModelInfo, 0, len(models))
	for _, m := range models {
		if m.Status == want {
			out = append(out, m)
		}
	}
	return out
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

// openViewStore opens the SQLite metadata cache best-effort for a view command,
// mirroring runList's discipline: resolveDBPath → OpenStore. It returns the store
// (the caller closes it) or nil when the path cannot be resolved or opened. It is
// SILENT: the embedded-catalog fallback notice is NOT decided here — a store that
// opens can still contribute zero synced rows (the fresh-empty, never-synced case),
// so the notice decision belongs to the overlay that actually reads the cache.
func openViewStore(dbPath string) *bestiary.Store {
	if path, err := resolveDBPath(dbPath); err == nil {
		if store, oerr := bestiary.OpenStore(path); oerr == nil {
			return store
		}
	}
	return nil
}

// overlayEntities returns the full entity set with synced (store) metadata
// overlaid on top of the baked catalog, and prints the SINGLE embedded-catalog
// fallback notice exactly when the store contributes ZERO synced metadata rows.
//
// This honors the sync-discoverability intent: the notice fires whenever a view
// shows baked-only metadata — the store is absent (nil), auto-created fresh/empty
// (never synced), or unreadable — and stays SILENT once a sync has populated the
// cache. OpenStore auto-creates an empty DB for a never-synced user, so keying the
// notice on "open failed" would miss that primary audience; keying it on "zero
// synced rows" catches every baked-only path with one notice per command.
//
// The overlay runs over the FULL entity set (before any tuple filtering) so
// metadata-only standalones and re-attached rows both surface. When synced metadata
// is present it is merged over the baked layer (synced wins per MetadataID via
// LastSynced; baked-only rows survive) and re-attached across every entity.
func overlayEntities(store *bestiary.Store) []bestiary.Entity {
	ents := bestiary.Entities()

	var cached []bestiary.EntityMetadata
	if store != nil {
		if rows, err := store.QueryEntityMetadata(context.Background()); err == nil {
			cached = rows
		}
	}

	if len(cached) == 0 {
		// Store absent, fresh-empty, or unreadable — the view falls back to the
		// baked catalog (which already carries baked metadata). Emit the one
		// sync-discoverability notice.
		fmt.Fprintln(os.Stderr, embeddedFallbackNotice)
		return ents
	}

	baked := bakedEntityMetadataFromEntities(ents)
	meta := bestiary.MergeEntityMetadata(baked, cached)
	return bestiary.AttachEntityMetadata(ents, meta)
}

// bakedEntityMetadataFromEntities gathers the baked metadata rows currently
// attached to the static entity set. The registry attaches baked models.dev
// metadata to entities (and synthesizes baked standalone entities) at load, so the
// set of non-nil Entity.Metadata over Entities() IS the baked metadata surfaced in
// views. It is the base layer MergeEntityMetadata overlays synced metadata onto;
// there is no exported baked-slice accessor and this reconstruction needs none
// (unlinked baked rows are intentionally never surfaced in views anyway).
func bakedEntityMetadataFromEntities(ents []bestiary.Entity) []bestiary.EntityMetadata {
	var out []bestiary.EntityMetadata
	for i := range ents {
		if ents[i].Metadata != nil {
			out = append(out, *ents[i].Metadata)
		}
	}
	return out
}

// entityRefForModel builds the identity EntityRef for a model exactly the way the
// registry aggregate (loadEntityIndex) builds entity keys: the identity-class
// projection of the raw modifiers (EntityModifiers), never the raw list. It is the
// single derivation used both to attest synced models (entitySourcesForModels) and
// to locate an entity within an overlaid set (findEntityInSet), so the CLI and the
// registry always agree on an entity's key.
func entityRefForModel(m bestiary.ModelInfo) bestiary.EntityRef {
	return bestiary.EntityRef{
		Family:    m.Family,
		Variant:   m.Variant,
		Version:   m.Version,
		ParamSize: m.ParamSize,
		Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
	}
}

// findEntityInSet resolves arg to an entity within the provided (possibly
// overlaid) set. It mirrors lookupEntity's two-path resolution — an identity tuple
// first, then a concrete-model-ID fallback — but searches the given slice instead
// of the registry, so a synced-only standalone entity (present only after the
// store overlay) is found. The set is indexed by EntityRef.String(); the returned
// entity is the slice element (already a defensive deep copy from Entities()).
func findEntityInSet(ents []bestiary.Entity, arg string) (bestiary.Entity, bool) {
	index := make(map[string]int, len(ents))
	for i := range ents {
		index[ents[i].Ref.String()] = i
	}
	// Tuple path: parse the identity tuple and build its key.
	if fam, variant, version, paramSize, mods, err := parseEntityTuple(arg); err == nil {
		ref := bestiary.EntityRef{
			Family:    fam,
			Variant:   variant,
			Version:   version,
			ParamSize: paramSize,
			Modifier:  bestiary.EntityModifiers(mods, fam),
		}
		if i, ok := index[ref.String()]; ok {
			return ents[i], true
		}
	}
	// Fallback: arg may be a concrete model ID rather than a tuple.
	if m, ok := bestiary.LookupModel(bestiary.ModelID(arg)); ok {
		if i, ok := index[entityRefForModel(m).String()]; ok {
			return ents[i], true
		}
	}
	return bestiary.Entity{}, false
}

// runProviders lists every provider/host instance of the entity identified by the
// given tuple (or model ID). When quantFlag is non-empty the instance set is
// filtered to those carrying a QuantVRAM row matching that quantization; an
// unrecognised quantFlag is rejected with an actionable error (never silently
// ignored or mapped to QuantizationOther). The entity is resolved over the
// store-overlaid entity set so synced metadata and standalones surface.
func runProviders(arg string, format bestiary.OutputFormat, quantFlag, dbPath string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	quant, filter, err := parseQuantFilter(quantFlag)
	if err != nil {
		return err
	}
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	ent, ok := findEntityInSet(overlayEntities(store), arg)
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
func runSources(arg string, format bestiary.OutputFormat, dbPath string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	ent, ok := findEntityInSet(overlayEntities(store), arg)
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

// runSourcesHistory prints the full append-only ingest history for every data
// source, ascending by ingest time. It opens the store best-effort: when the store
// opens its dataset_ingested log is read (Store.QueryIngestHistory per source);
// when it does not, the command falls back to the curated ingest history
// (DatasetIngestHistoryFor). Either way the source dimension (id/uri/canonical
// name) is resolved via the curated DataSourceByID FK join, and rows are grouped by
// source (ascending source id) then by ingest time (ascending).
func runSourcesHistory(format bestiary.OutputFormat, dbPath string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	rows := ingestHistoryRows(store)
	if format == bestiary.FormatJSON {
		return writeJSON(os.Stdout, rows)
	}
	writeHistoryTable(os.Stdout, rows)
	return nil
}

// ingestHistoryRows builds the full per-source ingest history as joined provenance
// rows. For each curated data source it reads the ingest history from the store
// (when store is non-nil and the read succeeds) or the curated table otherwise,
// joins the source dimension via DataSourceByID, and emits one row per ingest.
// Sources are visited in ascending id order and each source's rows stay ascending
// by ingest time (the reader contract), so the result is fully deterministic.
func ingestHistoryRows(store *bestiary.Store) []sourceProvenance {
	sources := bestiary.KnownDataSources()
	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })

	var rows []sourceProvenance
	for _, ds := range sources {
		var hist []bestiary.DatasetIngested
		if store != nil {
			if h, err := store.QueryIngestHistory(context.Background(), ds.ID); err == nil && len(h) > 0 {
				hist = h
			}
		}
		if hist == nil {
			hist = bestiary.DatasetIngestHistoryFor(ds.ID)
		}
		for _, di := range hist {
			rows = append(rows, sourceProvenance{
				DataSource:   ds,
				IngestedAt:   di.IngestedAt,
				ParserSchema: di.ParserSchema,
			})
		}
	}
	return rows
}

// runSourcesExport emits the store's ingest provenance as a datasources.json v3
// document — the SAME schema the curated seed loader parses, so an export is
// diffable against / promotable into parse/data/datasources.json. It writes to
// outPath, or to stdout when outPath is empty or "-". When the store is empty or
// absent the export falls back to the curated table (documented), so the document
// is always complete and round-trippable.
func runSourcesExport(dbPath, outPath string) error {
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	doc := buildSourcesExport(store)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("sources --export: marshal datasources document: %w", err)
	}
	data = append(data, '\n')

	if outPath == "" || outPath == "-" {
		_, err = os.Stdout.Write(data)
		if err != nil {
			return fmt.Errorf("sources --export: write to stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return fmt.Errorf(
			"sources --export: write datasources document to %s: %w;"+
				" why: the output file could not be created or written;"+
				" where: runSourcesExport;"+
				" how to fix: pass a writable path, or omit the path to write to stdout",
			outPath, err,
		)
	}
	return nil
}

// datasourcesExportDoc is the datasources.json v3 export shape. Its JSON tags match
// the curated-seed loader's dataSourceFileJSON EXACTLY (schema_version / sources /
// ingested with id,uri,canonical_name and source_id,ingested_at,parser_schema), so
// the emitted document round-trips through that loader with content equality.
type datasourcesExportDoc struct {
	SchemaVersion int                     `json:"schema_version"`
	Sources       []datasourceExportRow   `json:"sources"`
	Ingested      []datasetIngestedExport `json:"ingested"`
}

type datasourceExportRow struct {
	ID            string `json:"id"`
	URI           string `json:"uri"`
	CanonicalName string `json:"canonical_name"`
}

type datasetIngestedExport struct {
	SourceID     string `json:"source_id"`
	IngestedAt   string `json:"ingested_at"`
	ParserSchema int    `json:"parser_schema"`
}

// datasourcesExportSchemaVersion is the schema_version the export document carries.
// It matches parse/data/datasources.json's committed schema_version (3).
const datasourcesExportSchemaVersion = 3

// buildSourcesExport assembles the datasources.json v3 export document. The source
// dimension (id/uri/canonical name) always comes from the curated table
// (KnownDataSources) — it is stable curated data with no store reader — while the
// ingest history comes from the store when it holds any ingest rows, else from the
// curated table (the documented store-empty/absent fallback). Sources are ordered
// by curated file order; a source's ingest rows stay ascending by ingest time.
func buildSourcesExport(store *bestiary.Store) datasourcesExportDoc {
	doc := datasourcesExportDoc{SchemaVersion: datasourcesExportSchemaVersion}

	sources := bestiary.KnownDataSources()
	for _, ds := range sources {
		doc.Sources = append(doc.Sources, datasourceExportRow{
			ID:            string(ds.ID),
			URI:           ds.URI,
			CanonicalName: ds.CanonicalName,
		})
	}

	// Prefer the store's ingest log when it holds any rows; otherwise fall back to
	// the curated ingest table so the export is never empty when curated data exists.
	useStore := false
	if store != nil {
		if cur, err := store.QueryCurrentIngests(context.Background()); err == nil && len(cur) > 0 {
			useStore = true
		}
	}
	for _, ds := range sources {
		var hist []bestiary.DatasetIngested
		if useStore {
			hist, _ = store.QueryIngestHistory(context.Background(), ds.ID)
		} else {
			hist = bestiary.DatasetIngestHistoryFor(ds.ID)
		}
		for _, di := range hist {
			doc.Ingested = append(doc.Ingested, datasetIngestedExport{
				SourceID:     string(di.SourceID),
				IngestedAt:   di.IngestedAt,
				ParserSchema: di.ParserSchema,
			})
		}
	}
	return doc
}

// runShowEntity renders the aggregate view of one entity: its identity, rolled-up
// provider/host lists, price/context/max-output ranges, capability union, lineage
// edges, and the underlying instances.
func runShowEntity(arg string, format bestiary.OutputFormat, quantFlag, dbPath string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	quant, filter, err := parseQuantFilter(quantFlag)
	if err != nil {
		return err
	}
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	ent, ok := findEntityInSet(overlayEntities(store), arg)
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

// sourceProvenance is the sources subcommand's per-source output record: the
// DataSource dimension row (ID/URI/CanonicalName — reached by the FK join on the
// source id; the uri is obtained ONLY via this join and is never duplicated onto
// the ingest row) composed with the two scalar facts of its DatasetIngested
// current ingest (IngestedAt/ParserSchema). The source id appears once, as the
// embedded DataSource.ID primary key — DatasetIngested.SourceID would be the same
// value, so it is omitted to avoid a redundant key.
//
// WIRE-SHAPE CONSTRAINT: this record marshals with the published 0.2.0 $defs field
// names (PascalCase: ID, URI, CanonicalName, IngestedAt, ParserSchema) — the same
// spelling every sibling subcommand (list/show/providers) emits and the spelling
// the schema tests pin. It deliberately embeds the production bestiary.DataSource
// type so any rename of those fields propagates here automatically. Do NOT add
// snake_case json tags: the snake_case shapes elsewhere in the repo are INGEST
// wire types, not output types, and a divergent output casing would be a breaking
// change for CLI consumers.
type sourceProvenance struct {
	bestiary.DataSource
	IngestedAt   string
	ParserSchema int
}

// sourceProvenanceRows builds the joined per-source provenance view for an
// entity's attesting sources. For each source id it resolves the DataSource
// dimension (id/uri/canonical-name) via the DataSourceByID FK join and the
// DatasetIngested current ingest via DatasetIngestedFor. The result is sorted
// ascending by source id so output ordering is deterministic regardless of the
// order in which Entity.Sources was supplied. A source that fails to resolve (a
// graceful-degrade load failure) keeps its id with empty join fields rather than
// being dropped.
func sourceProvenanceRows(sources []bestiary.DataSourceID) []sourceProvenance {
	rows := make([]sourceProvenance, 0, len(sources))
	for _, id := range sources {
		// Seed ID so a degraded (FK-miss) record still carries the source id.
		sp := sourceProvenance{DataSource: bestiary.DataSource{ID: id}}
		if ds, ok := bestiary.DataSourceByID(id); ok {
			sp.DataSource = ds
		}
		if di, ok := bestiary.DatasetIngestedFor(id); ok {
			sp.IngestedAt = di.IngestedAt
			sp.ParserSchema = di.ParserSchema
		}
		rows = append(rows, sp)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].ID < rows[j].ID
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
			string(r.ID), orDash(r.URI), orDash(r.IngestedAt), r.ParserSchema)
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

// writeInstanceTable prints a fixed-width table of provider instances, resolving
// each instance's release status from the static registry (ProviderInstance is a
// rolled-up view and carries no status of its own; status is an api.json /
// instance-level fact on ModelInfo, reached here by LookupModelByProvider).
func writeInstanceTable(w io.Writer, insts []bestiary.ProviderInstance) {
	writeInstanceTableWithStatus(w, insts, instanceStatuses(insts))
}

// instanceStatuses resolves the release status of each instance by looking up its
// backing ModelInfo in the static registry (by provider + id). An instance with no
// registry match — e.g. a synced-only standalone — resolves to StatusNone. The
// returned slice is index-aligned with insts.
func instanceStatuses(insts []bestiary.ProviderInstance) []bestiary.ModelStatus {
	out := make([]bestiary.ModelStatus, len(insts))
	for i, in := range insts {
		if m, ok := bestiary.LookupModelByProvider(in.Provider, string(in.ID)); ok {
			out[i] = m.Status
		}
	}
	return out
}

// writeInstanceTableWithStatus is the pure formatter behind writeInstanceTable: it
// renders the instance table and, when ANY instance carries a non-None status,
// gains a trailing STATUS column. statuses is index-aligned with insts; the
// separation of resolution (instanceStatuses) from formatting keeps the column
// logic unit-testable with synthetic statuses. Status is instance-level data, so
// it renders here on instance rows and never on the entity-metadata block.
func writeInstanceTableWithStatus(w io.Writer, insts []bestiary.ProviderInstance, statuses []bestiary.ModelStatus) {
	showStatus := false
	for _, s := range statuses {
		if s != bestiary.StatusNone {
			showStatus = true
			break
		}
	}

	fmt.Fprintf(w, "Instances (%d):\n", len(insts))
	if showStatus {
		fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10s %10s %-12s\n",
			"ID", "PROVIDER", "HOST", "IN/MTok", "OUT/MTok", "CONTEXT", "MAXOUT", "STATUS")
	} else {
		fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10s %10s\n",
			"ID", "PROVIDER", "HOST", "IN/MTok", "OUT/MTok", "CONTEXT", "MAXOUT")
	}
	for i, in := range insts {
		if showStatus {
			status := bestiary.StatusNone
			if i < len(statuses) {
				status = statuses[i]
			}
			fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10d %10d %-12s\n",
				string(in.ID), string(in.Provider), fmtHost(in.Host),
				fmtPrice(in.CostInputPerMTok), fmtPrice(in.CostOutputPerMTok),
				in.ContextWindow, in.MaxOutput, fmtStatus(status))
		} else {
			fmt.Fprintf(w, "  %-40s %-22s %-12s %12s %12s %10d %10d\n",
				string(in.ID), string(in.Provider), fmtHost(in.Host),
				fmtPrice(in.CostInputPerMTok), fmtPrice(in.CostOutputPerMTok),
				in.ContextWindow, in.MaxOutput)
		}
		writeQuantRows(w, in.QuantVRAM)
	}
}

// fmtStatus renders a ModelStatus for a table cell, mapping the zero value
// (StatusNone) to a dash so a bare "none" never clutters the column.
func fmtStatus(s bestiary.ModelStatus) string {
	if s == bestiary.StatusNone {
		return "-"
	}
	return s.String()
}

// writeHistoryTable prints the full ingest history as a SOURCE|URI|INGESTED|PARSER
// table (the same columns as the per-entity sources view). Each row is one ingest;
// the row count reflects total ingests across all sources.
func writeHistoryTable(w io.Writer, rows []sourceProvenance) {
	fmt.Fprintf(w, "Ingest history (%d):\n", len(rows))
	fmt.Fprintf(w, "  %-12s %-34s %-22s %8s\n", "SOURCE", "URI", "INGESTED", "PARSER")
	for _, r := range rows {
		fmt.Fprintf(w, "  %-12s %-34s %-22s %8d\n",
			string(r.ID), orDash(r.URI), orDash(r.IngestedAt), r.ParserSchema)
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

	writeEntityMetadata(w, e.Metadata)

	fmt.Fprintf(w, "Lineage (%d):\n", len(e.Lineage))
	for _, edge := range e.Lineage {
		fmt.Fprintf(w, "  -> %s %s\n", edge.Kind.String(), edge.Parent.String())
	}

	writeInstanceTable(w, e.Instances)
}

// writeEntityMetadata renders the models.dev entity metadata (provider-agnostic
// facts) attached to an entity: its description, license, and a benchmark table.
// It is a no-op when no metadata is joined (m == nil), so an entity with no
// metadata renders exactly as before. Description and license are entity-level
// facts (models.json side); status is deliberately absent here — it is
// instance-level and renders only on the instance table.
func writeEntityMetadata(w io.Writer, m *bestiary.EntityMetadata) {
	if m == nil {
		return
	}
	fmt.Fprintf(w, "Description: %s\n", orDash(m.Description))
	fmt.Fprintf(w, "License:     %s\n", orDash(m.License))
	writeBenchmarkTable(w, m.Benchmarks)
}

// benchmarkTableLimit is the maximum number of benchmark rows the TABLE view
// renders before truncating with a "… and N more" footer. The JSON output always
// carries every row; the table is capped for readability.
const benchmarkTableLimit = 5

// writeBenchmarkTable prints the lab-reported benchmark claims as a
// NAME|SCORE|METRIC|HARNESS|DATE|SOURCE table — fields kept in separate columns,
// never concatenated. At most benchmarkTableLimit rows render; when more exist a
// "… and N more (use --output json)" footer names the omitted count. Nothing is
// printed when the entity has no benchmarks.
func writeBenchmarkTable(w io.Writer, benchmarks []bestiary.BenchmarkResult) {
	if len(benchmarks) == 0 {
		return
	}
	fmt.Fprintf(w, "Benchmarks (%d):\n", len(benchmarks))
	fmt.Fprintf(w, "  %-24s %12s %-14s %-18s %-12s %s\n",
		"NAME", "SCORE", "METRIC", "HARNESS", "DATE", "SOURCE")

	shown := benchmarks
	if len(shown) > benchmarkTableLimit {
		shown = shown[:benchmarkTableLimit]
	}
	for _, b := range shown {
		fmt.Fprintf(w, "  %-24s %12s %-14s %-18s %-12s %s\n",
			orDash(b.Name), benchScoreCell(b), orDash(b.Metric),
			orDash(b.Harness), orDash(b.Date), orDash(b.SourceURL))
	}
	if len(benchmarks) > benchmarkTableLimit {
		fmt.Fprintf(w, "  … and %d more (use --output json)\n", len(benchmarks)-benchmarkTableLimit)
	}
}

// benchScoreCell renders the SCORE cell for a benchmark row: the verbatim upstream
// value (ScoreRaw) when the score was non-numeric, otherwise the numeric Score.
// This guarantees the cell is never blank — a string score ("pass", an em-dash,
// etc.) rides through on ScoreRaw rather than collapsing to a bare 0.
func benchScoreCell(b bestiary.BenchmarkResult) string {
	if b.ScoreRaw != "" {
		return b.ScoreRaw
	}
	return fmt.Sprintf("%g", b.Score)
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

// syncNow returns the RFC3339 (second-precision, UTC) timestamp stamped on the
// dataset_ingested row `sync` appends and on the synced metadata's LastSynced. It
// is a package var so tests can install a deterministic clock; production uses the
// wall clock. Second precision (NOT RFC3339Nano) is deliberate: it matches the
// committed datasources.json snapshot timestamps so store rows and seed rows sort
// consistently under lexicographic MAX(ingested_at).
var syncNow = func() string { return time.Now().UTC().Format(time.RFC3339) }

// runSync fetches live model data from the API, persists to store, and prints
// results. Unlike list/show, sync requires a functional store (no graceful
// fallback). It delegates to runSyncClient with the production client; tests
// exercise the SAME code path with a WithBaseURL client pointed at httptest.
func runSync(provider string, format bestiary.OutputFormat, dbPath string) error {
	return runSyncClient(bestiary.NewClient(), provider, format, dbPath)
}

// runSyncClient is the testable core of `sync`: it fetches the api.json models and
// the models.json entity metadata through the given client, warns on large drift
// versus the embedded catalog, then persists everything in the store —
//
//   - UpsertModels: the fetched api.json models (existing behavior).
//   - UpsertDataSources: registers the models.dev DataSource dimension row and
//     APPENDS one dataset_ingested row stamped with the sync wall-clock (a runtime
//     ingest is a genuine event, so a wall-clock RFC3339 timestamp is correct here
//     — this is NOT the committed-snapshot kind that must stay byte-deterministic).
//   - UpsertEntityMetadata: the fetched metadata, attributed to models.dev and
//     stamped with the same sync timestamp (its parent row's source_id is an FK, so
//     the DataSource must be registered first — hence the ordering above).
//   - UpsertEntitySources: one models.dev attestation per distinct synced entity
//     key, derived the SAME way the registry aggregate builds entity keys.
func runSyncClient(client *bestiary.Client, provider string, format bestiary.OutputFormat, dbPath string) error {
	ctx := context.Background()

	path, err := resolveDBPath(dbPath)
	if err != nil {
		return fmt.Errorf("sync: %w", err)
	}

	var fetched []bestiary.ModelInfo
	if provider != "" {
		fetched, err = client.FetchModelsByProvider(ctx, bestiary.Provider(provider))
	} else {
		fetched, err = client.FetchModels(ctx)
	}
	if err != nil {
		return fmt.Errorf("sync: fetch models: %w", err)
	}

	metadata, err := client.FetchModelMetadata(ctx)
	if err != nil {
		return fmt.Errorf("sync: fetch model metadata: %w", err)
	}

	// Drift warning: a materially stale vendored snapshot warrants a regen.
	if n := driftedModelCount(fetched, bestiary.StaticModels()); n > driftWarningThreshold {
		fmt.Fprintf(os.Stderr,
			"%swarning: %d live model IDs are absent from the embedded catalog;"+
				" the vendored models.dev snapshot is stale — refresh it and regenerate"+
				" (see AGENTS.md \"models.dev snapshot refresh\")\n",
			errPrefix, n)
	}

	store, err := bestiary.OpenStore(path)
	if err != nil {
		return fmt.Errorf("sync: open store at %s: %w", path, err)
	}
	defer store.Close()

	if err := store.UpsertModels(ctx, fetched); err != nil {
		return fmt.Errorf("sync: persist models: %w", err)
	}

	// Provenance timestamp for this sync. A runtime ingest is a real event, so a
	// wall-clock RFC3339 (UTC) stamp is correct — unlike the committed datasources.json
	// snapshot timestamps, which are pinned to keep generated output deterministic.
	now := syncNow()

	// Register the models.dev DataSource dimension row (reusing the curated
	// id/uri/canonical-name when available so provenance stays consistent with the
	// seed) and append one ingest-history row. UpsertEntityMetadata's parent FK
	// references data_sources(data_source_id), so this MUST precede it.
	ds, ok := bestiary.DataSourceByID(bestiary.DataSourceModelsDev)
	if !ok {
		// The curated seed (parse/data/datasources.json) normally supplies the
		// models.dev dimension row; this literal is a graceful fallback for a
		// degraded seed so the ingest row's FK still resolves.
		ds = bestiary.DataSource{
			ID:            bestiary.DataSourceModelsDev,
			URI:           "https://models.dev/api.json",
			CanonicalName: string(bestiary.DataSourceModelsDev),
		}
	}
	ingest := bestiary.DatasetIngested{
		SourceID:     bestiary.DataSourceModelsDev,
		IngestedAt:   now,
		ParserSchema: modelsDevParserSchema,
	}
	if err := store.UpsertDataSources(ctx, []bestiary.DataSource{ds}, []bestiary.DatasetIngested{ingest}); err != nil {
		return fmt.Errorf("sync: persist data source + ingest row: %w", err)
	}

	// Attribute the fetched metadata to models.dev and stamp it, then persist.
	for i := range metadata {
		metadata[i].Source = bestiary.DataSourceModelsDev
		metadata[i].LastSynced = now
	}
	if err := store.UpsertEntityMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("sync: persist entity metadata: %w", err)
	}

	// Attest every distinct synced entity to models.dev.
	if err := store.UpsertEntitySources(ctx, entitySourcesForModels(fetched)); err != nil {
		return fmt.Errorf("sync: persist entity attestations: %w", err)
	}

	return bestiary.FormatModels(os.Stdout, fetched, format)
}

// driftedModelCount reports how many fetched (live) models are absent from the
// embedded catalog, keyed by the composite (Provider, ID) that identifies a model
// row. It is the falsifiable core of the drift warning: it depends only on its two
// slice arguments, so it can be unit-tested with synthetic data independent of the
// threshold.
func driftedModelCount(fetched, embedded []bestiary.ModelInfo) int {
	have := make(map[string]struct{}, len(embedded))
	for _, m := range embedded {
		have[string(m.Provider)+"\x00"+string(m.ID)] = struct{}{}
	}
	n := 0
	for _, m := range fetched {
		if _, ok := have[string(m.Provider)+"\x00"+string(m.ID)]; !ok {
			n++
		}
	}
	return n
}

// entitySourcesForModels derives the models.dev entity→source attestations for a
// set of synced models. Each model's entity key is built via entityRefForModel —
// the SAME identity-class derivation the registry aggregate uses — and duplicate
// keys (many models share one entity) collapse to a single attestation row. Every
// returned row attests DataSourceModelsDev; the sync writes them with
// UpsertEntitySources (INSERT OR REPLACE per (entity_key, source), so a re-sync is
// idempotent and never duplicates an attestation).
func entitySourcesForModels(models []bestiary.ModelInfo) []bestiary.EntitySource {
	seen := make(map[string]struct{}, len(models))
	var out []bestiary.EntitySource
	for _, m := range models {
		key := entityRefForModel(m).String()
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, bestiary.EntitySource{EntityKey: key, SourceID: bestiary.DataSourceModelsDev})
	}
	return out
}
