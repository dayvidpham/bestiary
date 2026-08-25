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
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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
		return fmt.Errorf("usage: bestiary <list|show|providers|entities|series|sources|sync> [flags]")
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
	// --version (series only) selects a generation of the family named by the
	// positional, and is exactly equivalent to appending "-<value>" to it:
	// `series claude --version 4` ≡ `series claude-4` (the 4.x union) and
	// `series claude --version 4.8` ≡ `series claude-4.8` (the one line). It exists
	// because "which generation" is a dimension of the query, not part of the family's
	// name, and it is the spelling a script composes without string-building.
	seriesVersion := fs.String("version", "", "series: generation to select within the family (e.g. 4 for the 4.x union, 4.8 for one line)")
	// --input-format (series only) pins the grammar the selector is read under, for
	// scripting that must not depend on inference: canonical (the entity grammar,
	// no fallback), models.dev (a raw catalog id), or the default infer.
	seriesInput := fs.String("input-format", "", "series: selector grammar: canonical, models.dev, or infer (default)")

	if err := fs.Parse(reorderArgs(fs, args[1:])); err != nil {
		return err
	}

	// outputExplicit records whether the user actually passed --output, so the entity
	// views can pick a human-readable default (table) while still honouring an explicit
	// --output=json. The --output flag defaults to "json" for the model-oriented
	// commands; the entity views (show --by-entity and the plain-show entity fallback)
	// override that default to table only when the user left it unset. This mirrors the
	// flagWasSet discipline the series command uses for --db-path/--version.
	outputExplicit := flagWasSet(fs, "output")

	switch cmd {
	case "list":
		return runList(*provider, bestiary.OutputFormat(*output), *dbPath, *status)
	case "show":
		if *byEntity {
			if fs.NArg() < 1 {
				return fmt.Errorf("usage: bestiary show --by-entity <model-id | family[/variant][/version|@version]{identity-mods}> [--output=<json|table>] (default: table)")
			}
			// Human-readable default: the aggregate entity view renders as a table
			// unless the user explicitly asked for --output=json.
			entityFormat := bestiary.OutputFormat(*output)
			if !outputExplicit {
				entityFormat = bestiary.FormatTable
			}
			return runShowEntity(fs.Arg(0), entityFormat, *quant, *dbPath)
		}
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary show <model-id | entity-key> [--format=<peasant|huggingface|hf|purl|raw>] [--output=<json|yaml|table>] [flags]\n" +
				"  an entity-key input (e.g. llama@3.3#70b{instruct}) that is not a model id renders the entity view (default: table)")
		}
		return runShow(fs.Arg(0), bestiary.OutputFormat(*output), *dbPath, *inputFormat, *scheme, outputExplicit)
	case "providers":
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary providers <family>[/<variant>][/<version>|@<version>]{identity-mods} [--output=<json|table>]\n" +
				"  version may be given as a trailing /segment or as @version; the optional [attributes] filter is ignored in MVP")
		}
		return runProviders(fs.Arg(0), bestiary.OutputFormat(*output), *quant, *dbPath)
	case "entities":
		// entities takes no positional: it enumerates the whole registry so
		// metadata-only standalones (reachable only by exact key elsewhere) are
		// discoverable. --output selects json (Entity objects) or table (summary).
		return runEntities(bestiary.OutputFormat(*output), *dbPath)
	case "series":
		// series takes an OPTIONAL positional: with none it lists every line in the
		// registry, with one it details that line's releases and their entities.
		// --db-path is REJECTED rather than ignored: the view computes from the
		// compiled-in registry, so the flag cannot be honoured, and silently
		// accepting it would imply a cache read that never happens.
		// --provider/--quant/--status are real entity-level filters here (see
		// seriesFilter); --db-path remains rejected because the view is registry-static.
		if flagWasSet(fs, "db-path") {
			return errSeriesDBPath
		}
		// --version selects WITHIN a family, so it is meaningless without one: it is
		// rejected rather than silently ignored (or silently widened to the whole
		// registry), for the same reason --db-path is.
		if flagWasSet(fs, "version") {
			if strings.TrimSpace(*seriesVersion) == "" {
				return fmt.Errorf("--version was given no value\n" +
					"  What: `series --version` requires a generation to select\n" +
					"  Where: bestiary series\n" +
					"  How to fix: pass one, e.g. `bestiary series claude --version 4` (the 4.x union) " +
					"or `bestiary series claude --version 4.8` (that one line)")
			}
			if fs.NArg() < 1 {
				return fmt.Errorf("--version %s was given without a family\n"+
					"  What: `series --version` selects a generation WITHIN a family, so it needs one to select within\n"+
					"  Where: bestiary series\n"+
					"  Why: on its own it would name every %s-ish line in the registry across unrelated families\n"+
					"  How to fix: name the family, e.g. `bestiary series claude --version %s` "+
					"(equivalently `bestiary series claude-%s`)",
					*seriesVersion, *seriesVersion, *seriesVersion, *seriesVersion)
			}
		}
		// --input-format pins the selector grammar, so like --version it is
		// meaningless without a selector to read.
		if flagWasSet(fs, "input-format") && fs.NArg() < 1 {
			return fmt.Errorf("--input-format %s was given without a selector\n"+
				"  What: `series --input-format` pins the grammar the SELECTOR is read under, so it needs one\n"+
				"  Where: bestiary series\n"+
				"  How to fix: pass a selector, e.g. `bestiary series claude/opus@4 --input-format=canonical`, "+
				"or drop the flag to list every line",
				*seriesInput)
		}
		// Human-readable default: like `show --by-entity`, `series` renders a table
		// unless the user explicitly passed --output=json. `series` is the disambiguation
		// path the ambiguity guidance recommends ("browse the family: bestiary series
		// <family>"), so a raw-JSON default there would walk the user straight back into
		// the non-human-readable wall that motivated the human-readable default.
		seriesFormat := bestiary.OutputFormat(*output)
		if !outputExplicit {
			seriesFormat = bestiary.FormatTable
		}
		return runSeries(fs.Arg(0), seriesFormat, *provider, *quant, *status, *seriesVersion, *seriesInput)
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
		return fmt.Errorf("unknown command %q; supported commands: list, show, providers, entities, series, sources, sync", cmd)
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

// flagWasSet reports whether the named flag was EXPLICITLY given on the command
// line, as opposed to merely carrying its default. flag.FlagSet.Visit walks only
// the flags that were actually set, which is the distinction a command needs to
// reject a flag it cannot honour without also rejecting every invocation that
// simply inherited the shared flagset's default.
func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
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
// wantsOCIScheme reports whether the show flags select the OCI external-identifier
// scheme, honoring the same --format-over-legacy--scheme precedence runShow's
// resolve path uses: an explicit non-default --format wins, else the deprecated
// --scheme is consulted. A malformed flag value is left for the resolve path to
// reject with its own actionable parse error, so this returns false (not OCI) on a
// parse failure rather than swallowing the input.
func wantsOCIScheme(inputFormatFlag, schemeFlag string) bool {
	if inputFormatFlag != "" && inputFormatFlag != "peasant" {
		f, err := bestiary.ParseInputFormat(inputFormatFlag)
		return err == nil && f == bestiary.InputFormatOCI
	}
	if schemeFlag != "" {
		s, err := bestiary.ParseScheme(schemeFlag)
		return err == nil && s == bestiary.SchemeOCI
	}
	return false
}

// ociSchemeNotice is the actionable stderr message printed when `show` is asked for
// the OCI scheme (--format=oci / --scheme=oci) on a model or entity ref. OCI
// identity lives one altitude below a ref — at the per-quantization manifest digest
// — so a ref has no single OCI render; this explains WHY the result is empty and
// directs the user to the quant-level view that DOES carry OCI purls. It covers the
// two situations that both surface as empty: a bare ref that structurally has no
// digest, and an entity whose quant rows simply have no digest captured yet.
func ociSchemeNotice(input string) string {
	return fmt.Sprintf(
		errPrefix+"the `oci` scheme has no render at the model/ref altitude, so %q produces no output.\n"+
			errPrefix+"why: a pkg:oci identity is content-addressed by a per-quantization manifest digest\n"+
			errPrefix+"  (QuantVRAM.OCIDigest); a bare ref carries no single digest, so ModelRef.Format(oci)\n"+
			errPrefix+"  is \"\" by design. Two cases look the same here:\n"+
			errPrefix+"  (1) a bare ref has no digest at all — OCI identity lives on the quantization row, not the ref;\n"+
			errPrefix+"  (2) an entity's quant rows have no digest captured yet — digests land with the next\n"+
			errPrefix+"      offline `bestiary-ollama` refresh.\n"+
			errPrefix+"how to fix: use the quant-level view, e.g. `bestiary show --by-entity --output=json %s`,\n"+
			errPrefix+"  and read each instance's QuantVRAM.OCIDigest and the entity's oci/huggingface Nomina\n"+
			errPrefix+"  (with their attestations).\n",
		input, input,
	)
}

func runShow(input string, format bestiary.OutputFormat, dbPath string, inputFormatFlag string, schemeFlag string, outputExplicit bool) error {
	// The `oci` scheme has no rendering at the model/ref altitude: a pkg:oci identity
	// is content-addressed by a per-quantization manifest digest, so ModelRef.Format(
	// SchemeOCI) is "" BY DESIGN. Requesting it here would otherwise resolve to a
	// confusing empty/"not found". Short-circuit with an actionable explanation on
	// stderr and an empty stdout — this is an explained empty, not a failure, so it
	// returns nil (exit 0), matching the embeddedFallbackNotice / drift-warning
	// house convention for informational stderr notices.
	if wantsOCIScheme(inputFormatFlag, schemeFlag) {
		fmt.Fprint(os.Stderr, ociSchemeNotice(input))
		return nil
	}

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
			// Derive a concrete, copy-pasteable refinement from the FIRST candidate (the
			// deterministic-order representative) and the family to browse, so the
			// guidance names a REAL next command instead of the abstract "refine your
			// input". Both fall back to the raw input if the candidate list is somehow
			// empty (defensive: ErrAmbiguous always carries candidates).
			//
			// The example is the candidate's ENTITY KEY (EntityRef.String(), e.g.
			// "llama@3.3#70b{instruct}") shown via `show --by-entity`. This resolves BY
			// CONSTRUCTION: the entity view renders an entity key directly, without the
			// model-first resolution that produced this very ambiguity, so a candidate's
			// own key always renders. A plain `show <key>` would re-enter model
			// resolution and could re-ambiguate for a high-fanout entity (e.g. gpt/4o,
			// whose key names 20 date-differentiated model rows) — hence --by-entity,
			// which is exactly the grammar that path accepts.
			example := input
			family := input
			if len(ambig.Candidates) > 0 {
				if k := entityRefForRef(ambig.Candidates[0]).String(); k != "" {
					example = k
				}
				if f := string(ambig.Candidates[0].Family); f != "" {
					family = f
				}
			}
			return fmt.Errorf(
				"%q is under-specified: it matched %d distinct models (they differ by variant, version, or size), so `show` cannot pick one.\n"+
					"  The matching candidates are listed above. To narrow it:\n"+
					"    - show one directly:   bestiary show --by-entity %s\n"+
					"    - browse the family:   bestiary series %s\n"+
					"    - use an exact API id: bestiary show <raw-id> --format=raw",
				input, len(ambig.Candidates), example, family,
			)
		}
		// The model resolver could not parse/resolve the input. Before surfacing the
		// error, try the entity fallback: an entity-key input (a grammar the model
		// resolver rejects — e.g. one carrying #size or {identity-mods}) resolves as an
		// entity here. Model resolution stays FIRST, so this never shadows a real model.
		if handled, ferr := showEntityFallback(input, format, dbPath, outputExplicit); handled {
			return ferr
		}
		// Not a model and not an entity — surface the original resolver error.
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
		// Resolve produced refs but no ModelInfo backs them (registry/cache miss).
		// The same entity-key fallback applies: the input may be an entity identity
		// rather than a concrete model row.
		if handled, ferr := showEntityFallback(input, format, dbPath, outputExplicit); handled {
			return ferr
		}
		return &bestiary.ErrNotFound{What: "model", Key: input}
	}
	return bestiary.FormatModel(os.Stdout, best, format)
}

// showEntityFallback renders the aggregate entity view for input when input is an
// entity key rather than a concrete model id. It is the plain-`show` fallback F2
// adds: `show` resolves a MODEL first, and only when that misses does this attempt
// an entity-identity resolution over the store-overlaid entity set (the SAME path
// `show --by-entity` uses, so synced metadata and standalones surface identically).
//
// handled reports whether the input resolved as an entity: false means "not an
// entity either" and the caller should surface its original model-not-found error;
// true means the entity view was rendered (err carries any render/validation error).
//
// Output format honours F5's human-readable default: when the user did not pass an
// explicit --output (outputExplicit == false) the view renders as a table; an
// explicit --output=json is respected. validateEntityOutput rejects a format the
// entity view cannot render (e.g. yaml) with the same actionable error the
// --by-entity path raises.
func showEntityFallback(input string, format bestiary.OutputFormat, dbPath string, outputExplicit bool) (handled bool, err error) {
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	ent, ok := findEntityInSet(overlayEntities(store), input)
	if !ok {
		return false, nil
	}
	if !outputExplicit {
		format = bestiary.FormatTable
	}
	if verr := validateEntityOutput(format); verr != nil {
		return true, verr
	}
	if format == bestiary.FormatJSON {
		return true, writeJSON(os.Stdout, withNomina(ent))
	}
	writeEntityView(os.Stdout, ent)
	return true, nil
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

// entityRefForRef is the ModelRef analogue of entityRefForModel: it projects a
// resolver candidate (ErrAmbiguous.Candidates carries ModelRefs, not ModelInfos)
// onto its identity EntityRef via the same identity-class modifier projection.
// The derived key names the entity the candidate belongs to, so the ambiguity
// guidance can point `show --by-entity` at a key guaranteed to be in the
// registry's entity set.
func entityRefForRef(r bestiary.ModelRef) bestiary.EntityRef {
	return bestiary.EntityRef{
		Family:    r.Family,
		Variant:   r.Variant,
		Version:   r.Version,
		ParamSize: r.ParamSize,
		Modifier:  bestiary.EntityModifiers(r.Modifier, r.Family),
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
		// A redundant-modifier suppression entry makes the PREFERRED spelling shorter
		// than the key, and the preferred spelling is what the entity views print — so
		// it must resolve back here, or the CLI would render a name it cannot accept.
		// With no seed entry this is the same string and the assignment is a no-op.
		if preferred := ents[i].PreferredName(); preferred != ents[i].Ref.String() {
			if _, taken := index[preferred]; !taken {
				index[preferred] = i
			}
		}
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
	fmt.Fprintf(os.Stdout, "Entity: %s\n", ent.PreferredName())
	writeInstanceTable(os.Stdout, insts)
	return nil
}

// runEntities enumerates EVERY entity in the registry — the discoverability
// surface for identities (notably metadata-only standalones) that are otherwise
// reachable only by their exact key. It resolves entirely offline over the
// store-overlaid entity set (so synced metadata and synced-only standalones
// surface, and the one embedded-catalog notice fires on the zero-synced-rows
// path), sorts by entity key, and renders per --output: json emits the full Entity
// objects; table emits the ENTITY KEY | PROVIDERS | METADATA | BENCHMARKS summary.
func runEntities(format bestiary.OutputFormat, dbPath string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	store := openViewStore(dbPath)
	if store != nil {
		defer store.Close()
	}
	ents := overlayEntities(store)
	sort.Slice(ents, func(i, j int) bool { return ents[i].Ref.String() < ents[j].Ref.String() })
	if format == bestiary.FormatJSON {
		out := make([]entityJSON, len(ents))
		for i, e := range ents {
			out[i] = withNomina(e)
		}
		return writeJSON(os.Stdout, out)
	}
	writeEntitiesTable(os.Stdout, ents)
	return nil
}

// writeEntitiesTable renders the registry-wide entity summary: one row per entity
// with its key, provider/host instance count, whether provider-agnostic metadata is
// attached, and how many benchmark claims that metadata carries. An entity with no
// metadata shows "-" for METADATA and 0 for BENCHMARKS. Rows are emitted in the
// caller's order (sorted by key).
func writeEntitiesTable(w io.Writer, ents []bestiary.Entity) {
	fmt.Fprintf(w, "Entities (%d):\n", len(ents))
	fmt.Fprintf(w, "  %-48s %9s %8s %10s\n", "ENTITY KEY", "PROVIDERS", "METADATA", "BENCHMARKS")
	for _, e := range ents {
		metadata := "-"
		benchmarks := 0
		if e.Metadata != nil {
			metadata = "yes"
			benchmarks = len(e.Metadata.Benchmarks)
		}
		fmt.Fprintf(w, "  %-48s %9d %8s %10d\n", e.PreferredName(), len(e.Providers), metadata, benchmarks)
	}
}

// errSeriesDBPath is returned when `series` is given an explicit --db-path. The
// flag is registered on the shared flagset every subcommand parses, so it PARSES
// for series; rejecting it here is what keeps the CLI honest, because the series
// view computes from the compiled-in registry and never opens the cache. Accepting
// it silently would let a caller believe they had scoped the view to a synced
// database. No "bestiary:" prefix — main() prepends it.
var errSeriesDBPath = errors.New(
	"series computes from the compiled-in registry and does not read the cache; " +
		"--db-path has no effect here (use entities/show for cache-aware views)",
)

// seriesSummary is the JSON/table row of the registry-wide `series` listing: one
// line with its counts. Releases/Entities are counts, not the objects, so the
// listing stays a summary — `bestiary series <selector>` is the detail view.
type seriesSummary struct {
	Series     string
	Family     bestiary.Family
	Generation string
	Releases   int
	Entities   int
}

// seriesDetail is the JSON shape of the selected-line view: the line plus its
// releases, each with the canonical keys of its member entities.
type seriesDetail struct {
	Series     string
	Family     bestiary.Family
	Generation string
	Releases   []releaseDetail
}

// releaseDetail is one release within a seriesDetail. Name is empty for the
// bare (un-named) line; Release carries the display rendering either way.
type releaseDetail struct {
	Name     string
	Release  string
	Entities []string
}

// runSeries renders the computed Series/Release hierarchy.
//
// With no selector it lists every line in the registry (sorted by family, then
// generation) with its release and entity counts. With a selector it details the
// matching lines: each release and the canonical entity keys under it.
//
// # Selector semantics: a specificity ladder
//
// A Series is a family at one exact generation (claude-4.0 and claude-4.5 are two
// lines), and the selector chooses how much of that structure to ask for. Each rung
// is strictly narrower than the one above:
//
//	bestiary series claude              every claude line, all generations
//	bestiary series claude-4            every claude 4.x line (the MAJOR union)
//	bestiary series claude --version 4  identical to the line above
//	bestiary series claude-4.8          the one claude-4.8 line
//
// The major rung is a UNION, not a re-grouping: it returns several Series in the
// same multi-line output shape the family rung already produces, and the underlying
// hierarchy is untouched. Membership is a STRICT string rule (see
// generationInMajorUnion) — a generation belongs to major "4" iff it IS "4" or
// begins "4." — so a family that spells both a bare "4" and dotted siblings has the
// bare line included, and nothing is numerically normalized on the way in.
//
// versionFlag is the --version value and is exactly equivalent to appending
// "-<value>" to the positional, so the two spellings cannot drift apart.
//
// The view is OFFLINE and STATIC by construction: the hierarchy is computed from
// the compiled-in registry (SeriesAll/ReleasesOf/EntitiesOf), so — unlike the
// `entities` view — it never consults the SQLite cache and takes no --db-path. A
// synced row cannot introduce a line; that is a property of the taxonomy being a
// function of the baked catalog's key components, not a limitation of the cache.
//
// # Filters
//
// --provider, --quant and --status narrow the ENTITY list inside each release. They
// are per-entity predicates satisfied by an entity's INSTANCES:
//
//	--provider=<slug>   the entity has an instance served by that provider
//	--quant=<quant>     the entity has an instance carrying a matching QuantVRAM row
//	--status=<status>   the entity has an instance whose model has that release status
//
// Combined filters must be satisfied by ONE instance simultaneously, not by
// different instances each satisfying a different flag — see seriesFilter for why
// the per-dimension reading would report a provider/quant pairing that does not
// exist. An unknown --quant or --status value is rejected with an actionable error
// rather than silently matching nothing.
//
// The drops CASCADE: a release whose entities all filter away is omitted, and a
// line whose releases all empty is omitted from both views. The listing counts are
// post-filter, so `series --provider X` lists exactly the lines and counts that
// `series <line> --provider X` will then render. A selector that names real lines
// which the filters empty is a distinct, actionable error — not ErrNotFound, since
// the selector was good and the filter was what matched nothing.
func runSeries(selector string, format bestiary.OutputFormat, providerFlag, quantFlag, statusFlag, versionFlag, inputFormatFlag string) error {
	if err := validateEntityOutput(format); err != nil {
		return err
	}
	inputFormat, err := parseSeriesInputFormat(inputFormatFlag)
	if err != nil {
		return err
	}
	candidates, err := applyVersionFlag(selector, versionFlag, inputFormat)
	if err != nil {
		return err
	}
	// Parse the filters up front so an unknown --quant or --status value fails fast
	// with an actionable error, before any view is computed.
	filter, filterErr := newSeriesFilter(providerFlag, quantFlag, statusFlag)
	if filterErr != nil {
		return filterErr
	}

	all := bestiary.SeriesAll()
	if selector == "" {
		if format == bestiary.FormatJSON {
			return writeJSON(os.Stdout, seriesSummaries(all, filter))
		}
		writeSeriesTable(os.Stdout, seriesSummaries(all, filter))
		return nil
	}

	// Resolve every candidate spelling --version produced, unioning what they name so
	// the flag behaves identically on the ladder and canonical grammars.
	var selection seriesSelection
	var lastErr error
	for _, candidate := range candidates {
		got, err := resolveSeriesSelector(all, candidate, inputFormat)
		if err != nil {
			lastErr = err
			continue
		}
		selection = unionSeriesSelections(selection, got)
	}
	matches := selection.lines
	if len(matches) == 0 {
		// A hard grammar/lookup error from a RESTRICTED format is the useful message
		// (it says what was wrong with the spelling); otherwise the selector simply
		// named nothing.
		if lastErr != nil && inputFormat != seriesInputInfer {
			return lastErr
		}
		return &bestiary.ErrNotFound{What: "series", Key: selector}
	}

	// A provider-qualified selector feeds the ORDINARY entity filter rather than a
	// second filtering mechanism, so `anthropic/claude@4` and `claude@4
	// --provider anthropic` narrow identically. An explicit --provider that
	// disagrees is a contradiction, not a precedence question.
	if selection.hasProvider {
		if filter.byProvider && filter.provider != selection.provider {
			return fmt.Errorf(
				"selector %q and --provider %s disagree\n"+
					"  What: the selector is qualified to provider %q while --provider asks for %q\n"+
					"  Where: bestiary series\n"+
					"  Why: an entity cannot be narrowed to two different providers at once, and "+
					"silently preferring one would hide the contradiction\n"+
					"  How to fix: drop --provider to use the selector's %q, or drop the %q/ prefix "+
					"from the selector to use --provider %s",
				selector, filter.provider, selection.provider, filter.provider,
				selection.provider, selection.provider, filter.provider)
		}
		filter.provider, filter.byProvider = selection.provider, true
	}

	details := make([]seriesDetail, 0, len(matches))
	for _, s := range matches {
		d, ok := seriesDetailOf(s, filter)
		if !ok {
			continue
		}
		// A release-level cut ("claude/opus") keeps only the named release of each
		// line. A line that does not carry that release drops out entirely, under the
		// same cascade rule an emptying filter follows — a header printed over nothing
		// would claim the line has an opus release when it does not.
		if selection.hasRelease {
			d.Releases = releasesNamed(d.Releases, selection.release)
			if len(d.Releases) == 0 {
				continue
			}
		}
		details = append(details, d)
	}
	// The selector named real lines but the filters emptied every one of them. That is
	// a different outcome from "no such series" and says so: the caller's selector was
	// good and their filter was the thing that matched nothing.
	if len(details) == 0 {
		return fmt.Errorf(
			"series %q matched %d line(s) but %s left no entities\n"+
				"  What: every release of every matched line filtered away to empty\n"+
				"  Where: bestiary series (registry-static view)\n"+
				"  Why: no entity in those lines has an instance satisfying all the given filters at once\n"+
				"  How to fix: relax or drop a filter, or run `bestiary series %s` unfiltered to see the full lines",
			selector, len(matches), filter.describe(), selector)
	}
	if format == bestiary.FormatJSON {
		return writeJSON(os.Stdout, details)
	}
	writeSeriesDetailTable(os.Stdout, details)
	return nil
}

// seriesFilter is the entity-level predicate the series views apply. All three
// flags select ENTITIES by what their instances carry: an entity survives when at
// least one of its instances satisfies the filter.
//
// The conjunction is PER INSTANCE, not per dimension: with --provider and --quant
// both set, one single instance must satisfy both, rather than one instance
// matching the provider while a different one matches the quant. The per-dimension
// reading would answer "this line has an ollama instance somewhere, and a q4_k_m
// instance somewhere" — which reads as "ollama serves this at q4_k_m" and can be
// false. The per-instance reading cannot mislead that way.
//
// Status is a MODEL-level fact rather than an instance-level one, so it is reached
// through the instance's (Provider, ID) — the same pairing `show` uses to respect a
// provider choice — and mirrors the `list --status` semantics exactly: an exact
// match on the parsed status, with the zero value StatusNone being a real,
// selectable value rather than "unset".
type seriesFilter struct {
	provider   bestiary.Provider
	byProvider bool

	quant   bestiary.Quantization
	byQuant bool

	status   bestiary.ModelStatus
	byStatus bool
}

// newSeriesFilter parses the flag values into the predicate set, rejecting an
// unknown --quant or --status with the same actionable errors the other
// subcommands raise (never a silent no-match).
func newSeriesFilter(providerFlag, quantFlag, statusFlag string) (seriesFilter, error) {
	var f seriesFilter
	if providerFlag != "" {
		f.provider = bestiary.Provider(providerFlag)
		f.byProvider = true
	}
	quant, byQuant, err := parseQuantFilter(quantFlag)
	if err != nil {
		return seriesFilter{}, err
	}
	f.quant, f.byQuant = quant, byQuant
	if statusFlag != "" {
		s, err := bestiary.ParseModelStatus(statusFlag)
		if err != nil {
			return seriesFilter{}, err
		}
		f.status, f.byStatus = s, true
	}
	return f, nil
}

// active reports whether any filter is set. When none is, the views take their
// original unfiltered path and every entity is kept without inspection.
func (f seriesFilter) active() bool { return f.byProvider || f.byQuant || f.byStatus }

// describe renders the active filters for the actionable empty-result error.
func (f seriesFilter) describe() string {
	var parts []string
	if f.byProvider {
		parts = append(parts, fmt.Sprintf("--provider=%s", f.provider))
	}
	if f.byQuant {
		parts = append(parts, fmt.Sprintf("--quant=%s", f.quant))
	}
	if f.byStatus {
		parts = append(parts, fmt.Sprintf("--status=%s", f.status))
	}
	if len(parts) == 0 {
		return "no filter"
	}
	return strings.Join(parts, " ")
}

// keep reports whether the entity survives the filters: true when no filter is
// active, otherwise true when at least ONE instance satisfies every active
// predicate simultaneously.
func (f seriesFilter) keep(e bestiary.Entity) bool {
	if !f.active() {
		return true
	}
	for _, in := range e.Instances {
		if f.byProvider && in.Provider != f.provider {
			continue
		}
		if f.byQuant && !instanceHasQuant(in, f.quant) {
			continue
		}
		if f.byStatus && !instanceHasStatus(in, f.status) {
			continue
		}
		return true
	}
	return false
}

// instanceHasQuant reports whether the instance carries a QuantVRAM row for q.
// Mirrors filterInstancesByQuant's predicate, which selects instances by the
// presence of a matching row rather than pruning the rows themselves.
func instanceHasQuant(in bestiary.ProviderInstance, q bestiary.Quantization) bool {
	for _, qv := range in.QuantVRAM {
		if qv.Quant == q {
			return true
		}
	}
	return false
}

// instanceHasStatus reports whether the model behind this instance carries the
// given release status. The instance is a registry projection and does not carry
// Status, so the fact is read from the (Provider, ID) row it projects — the
// provider-respecting lookup, not the bare LookupModel, which would answer from
// whichever provider's row happened to come first. An instance whose row is absent
// (a cache-only overlay row) simply does not match.
func instanceHasStatus(in bestiary.ProviderInstance, want bestiary.ModelStatus) bool {
	m, ok := bestiary.LookupModelByProvider(in.Provider, string(in.ID))
	if !ok {
		return false
	}
	return m.Status == want
}

// filterEntities keeps the entities satisfying the filter, preserving order. The
// input slice is never mutated.
func filterEntities(ents []bestiary.Entity, f seriesFilter) []bestiary.Entity {
	if !f.active() {
		return ents
	}
	out := make([]bestiary.Entity, 0, len(ents))
	for _, e := range ents {
		if f.keep(e) {
			out = append(out, e)
		}
	}
	return out
}

// seriesInputFormat is the closed set of grammars `series` will read its selector
// under (--input-format). It is a typed enum rather than a bare string so an
// unrecognized value is rejected at parse time with the members listed, and so the
// three readings below can never be confused at a call site.
type seriesInputFormat int

const (
	// seriesInputInfer is the DEFAULT: try every reading and union the results,
	// falling back to a raw model ID only when nothing else matched.
	seriesInputInfer seriesInputFormat = iota
	// seriesInputCanonical restricts the selector to the canonical entity grammar,
	// with NO fallback — the spelling for a script that must fail loudly rather
	// than silently resolve some other way.
	seriesInputCanonical
	// seriesInputModelsDev restricts the selector to a raw models.dev model ID,
	// resolved through the ordinary catalog lookup to the line of its entity.
	seriesInputModelsDev
)

func (f seriesInputFormat) String() string {
	switch f {
	case seriesInputCanonical:
		return "canonical"
	case seriesInputModelsDev:
		return "models.dev"
	default:
		return "infer"
	}
}

// parseSeriesInputFormat maps the --input-format flag value onto the enum. An
// unrecognized value is an actionable error naming every member, never a silent
// fall-through to the default — a script that misspells "models.dev" must be told,
// not quietly given the inferring behaviour it was trying to avoid.
func parseSeriesInputFormat(s string) (seriesInputFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "infer":
		return seriesInputInfer, nil
	case "canonical":
		return seriesInputCanonical, nil
	case "models.dev", "modelsdev":
		return seriesInputModelsDev, nil
	}
	return 0, fmt.Errorf(
		"unsupported --input-format %q\n"+
			"  What: `series --input-format` accepts canonical, models.dev, or infer\n"+
			"  Where: bestiary series\n"+
			"  Why: the selector grammar decides how %q is read, so an unknown value cannot be guessed at\n"+
			"  How to fix: use --input-format=canonical (claude/opus@4), --input-format=models.dev "+
			"(a raw catalog id), or omit the flag for the default infer",
		s, s)
}

// applyVersionFlag folds the --version value into the positional selector and
// returns the candidate spellings to resolve. Folding it into the SELECTOR — rather
// than carrying it as a separate parameter through the resolution — is what makes
// `series claude --version 4` and `series claude-4` the same query rather than two
// implementations that must be kept in agreement.
//
// A version-less selector yields TWO candidates because the two grammars spell a
// version differently, and either may be what the user meant:
//
//	claude       --version 4  ->  "claude-4"  (ladder)  and  "claude@4"  (canonical)
//	claude/opus  --version 4  ->  "claude/opus-4"       and  "claude/opus@4"
//
// Only one of a pair ever resolves in practice; unioning them means the flag works
// identically on both grammars without the caller having to know which one they are
// writing. Under --input-format=canonical only the canonical spelling is offered,
// since that format promises no other reading.
//
// A selector that ALREADY pins a version is not extended: the flag must AGREE with
// it, and a disagreement is an actionable error rather than a silent win for one of
// them. Both grammars are fenced SYMMETRICALLY — the canonical "@version" always, and
// the ladder's dash-embedded version under infer — so `claude-4.8 --version 5` fails
// as loudly as `claude@4.8 --version 5` instead of silently building an unresolvable
// `claude-4.8-5` and surfacing a bare "series not found" for a line the user spelled
// correctly. An AGREEING redundant flag (`claude-4 --version 4`) is left intact and
// resolves. run() has already rejected a --version given without a positional or
// without a value.
func applyVersionFlag(selector, versionFlag string, format seriesInputFormat) ([]string, error) {
	sel := strings.TrimSpace(selector)
	version := strings.TrimSpace(versionFlag)
	if version == "" {
		return []string{sel}, nil
	}
	// Canonical "@version" — a syntactic pin, fenced under every format.
	if at := strings.LastIndex(sel, "@"); at >= 0 {
		if existing := sel[at+1:]; existing != version {
			return nil, seriesVersionDisagreeError(sel, "@", existing, version)
		}
		return []string{sel}, nil
	}
	// Ladder "family-version" — a dash-embedded version is only a reading under infer
	// (canonical spells versions with "@"; models.dev takes a raw id), and it is
	// recognised as a purely numeric, optionally-dotted trailing segment so a family
	// whose NAME contains a dash ("grok-build") is not mistaken for a pinned version.
	if format == seriesInputInfer {
		if dash := strings.LastIndex(sel, "-"); dash >= 0 {
			if existing := sel[dash+1:]; isLadderVersionToken(existing) {
				if existing != version {
					return nil, seriesVersionDisagreeError(sel, "-", existing, version)
				}
				return []string{sel}, nil
			}
		}
	}
	if format == seriesInputCanonical {
		return []string{sel + "@" + version}, nil
	}
	return []string{sel + "-" + version, sel + "@" + version}, nil
}

// seriesVersionDisagreeError is the actionable contradiction reported when a selector
// already pins a version and --version asks for a different one. It is shared by both
// grammars — sep is "@" for the canonical pin and "-" for the ladder's dash pin — so
// the two spellings the README advertises as equivalent fail identically (what / why /
// where / how), and the fix names the exact segment to drop.
func seriesVersionDisagreeError(sel, sep, existing, version string) error {
	return fmt.Errorf(
		"selector %q and --version %s disagree\n"+
			"  What: the selector already pins version %q while --version asks for %q\n"+
			"  Where: bestiary series\n"+
			"  Why: two different versions cannot both be selected, and silently preferring one "+
			"would hide the contradiction\n"+
			"  How to fix: drop --version to use the selector's %q, or drop the %s%s from the "+
			"selector to use --version %s",
		sel, version, existing, version, existing, sep, existing, version)
}

// isLadderVersionToken reports whether s is a purely numeric, optionally-dotted
// version token (4, 4.8, 0.1, 12.1, 4.8.1) — exactly the shape the major-union rule
// accepts and no other. A leading dot, a trailing dot, a doubled dot, or any
// non-digit character (so a family segment like "build" or a "4a" spelling) is
// rejected, which is what lets the dash split distinguish a pinned ladder version
// from a family name that merely contains a dash.
func isLadderVersionToken(s string) bool {
	if s == "" {
		return false
	}
	prevDot := true // also guards against a leading dot
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			prevDot = false
		case r == '.':
			if prevDot {
				return false
			}
			prevDot = true
		default:
			return false
		}
	}
	return !prevDot // a trailing dot leaves this set
}

// selectSeries resolves a positional selector to the lines it names, as the UNION
// of three readings — all deterministic, and the union so a selector can never
// silently hide a line the user could reasonably have meant. They form a specificity
// ladder, each rung narrower than the one above:
//
//   - the FAMILY reading: the selector equals a family name ("gemma"), which
//     selects every generation of that family;
//   - the MAJOR reading: the selector is "<family>-<version>" and the line's
//     generation falls in that version's union ("claude-4" → claude-4.0, -4.1, -4.5,
//     -4.6, -4.7, -4.8), so a user can ask for a whole generation without knowing
//     which point releases exist;
//   - the LINE reading: the selector equals a line's display rendering
//     ("llama-4", "claude-4.8"), which is exactly what the listing prints, so
//     copy-pasting a row from `bestiary series` always resolves.
//
// The major and line readings do not conflict: a fully-specified "claude-4.8" is its
// own union of one (nothing spells "4.8.x"), so the ladder narrows smoothly rather
// than needing a mode switch. The union across readings matters where a family name
// is also a bare line's rendering: "gemma" returns the un-versioned gemma line AND
// gemma-2/3/4, rather than the bare line alone.
//
// Matching is case-folded. The result keeps SeriesAll's ordering, so a union comes
// back sorted by generation exactly as the listing prints it.
func selectSeries(all []bestiary.Series, selector string) []bestiary.Series {
	want := strings.ToLower(strings.TrimSpace(selector))
	if want == "" {
		return nil
	}
	var out []bestiary.Series
	for _, s := range all {
		if strings.ToLower(s.String()) == want || strings.ToLower(string(s.Family)) == want ||
			seriesInMajorUnion(s, want) {
			out = append(out, s)
		}
	}
	return out
}

// seriesSelection is a resolved selector: which lines were named, plus the two
// narrowings a canonical selector can carry with it.
//
// release is the RELEASE-LEVEL cut ("claude/opus" → only the opus release of each
// line). provider is the provider qualification ("anthropic/claude@4" → only
// anthropic-served entities), which is fed into the ordinary seriesFilter rather
// than being a second filtering mechanism.
type seriesSelection struct {
	lines []bestiary.Series

	release    string
	hasRelease bool

	provider    bestiary.Provider
	hasProvider bool
}

// resolveSeriesSelector resolves a selector under the given input format.
//
// # Reading precedence
//
// Under INFER (the default) the readings are tried in this order and the first two
// are UNIONED, so a selector can never silently lose an interpretation:
//
//  1. the LADDER readings — family / major-union / line rendering (selectSeries);
//  2. the CANONICAL reading — [provider/]family[/variant][@version], which adds the
//     release cut and the provider qualification;
//  3. the RAW MODEL ID reading — tried ONLY when 1 and 2 both found nothing, so a
//     catalog id resolves to its entity's line without ever shadowing a line whose
//     rendering happens to look like an id.
//
// Under CANONICAL only reading 2 runs, and a selector that does not parse (or names
// nothing) is an error with no fallback. Under MODELS.DEV only reading 3 runs.
//
// Note the grammar's "@" is the entity VERSION here, exactly as in an entity key
// (claude/opus@4.5) — NOT the @-date form the `show` resolver accepts. Series live
// above entity keys, so they inherit the key grammar.
func resolveSeriesSelector(all []bestiary.Series, selector string, format seriesInputFormat) (seriesSelection, error) {
	switch format {
	case seriesInputCanonical:
		sel, err := canonicalSeriesSelection(all, selector)
		if err != nil {
			return seriesSelection{}, err
		}
		return sel, nil
	case seriesInputModelsDev:
		return rawIDSeriesSelection(all, selector)
	}

	// INFER: ladder ∪ canonical, then the raw-id fallback.
	sel := seriesSelection{lines: selectSeries(all, selector)}
	if canonical, err := canonicalSeriesSelection(all, selector); err == nil && len(canonical.lines) > 0 {
		sel = unionSeriesSelections(sel, canonical)
	}
	if len(sel.lines) > 0 {
		return sel, nil
	}
	byID, err := rawIDSeriesSelection(all, selector)
	if err != nil {
		// The raw-id reading is the LAST fallback, so its miss is reported as the
		// ordinary not-found for the selector as written rather than as an
		// id-specific error — the user did not necessarily mean an id at all.
		return seriesSelection{}, nil
	}
	return byID, nil
}

// unionSeriesSelections merges the ladder and canonical readings, keeping SeriesAll
// order and de-duplicating. The narrowings come from the canonical side (the ladder
// reading never sets one), so they are carried across verbatim.
func unionSeriesSelections(ladder, canonical seriesSelection) seriesSelection {
	seen := map[bestiary.Series]bool{}
	for _, s := range ladder.lines {
		seen[s] = true
	}
	out := ladder
	for _, s := range canonical.lines {
		if !seen[s] {
			seen[s] = true
			out.lines = append(out.lines, s)
		}
	}
	sort.Slice(out.lines, func(i, j int) bool {
		if out.lines[i].Family != out.lines[j].Family {
			return out.lines[i].Family < out.lines[j].Family
		}
		return out.lines[i].Generation < out.lines[j].Generation
	})
	out.release, out.hasRelease = canonical.release, canonical.hasRelease
	out.provider, out.hasProvider = canonical.provider, canonical.hasProvider
	return out
}

// canonicalSeriesSelection reads the selector as the canonical entity grammar and
// maps each component onto its SERIES-level meaning:
//
//	claude@4             the major-4 union (identical to the ladder's claude-4)
//	claude@4.5           the one claude-4.5 line
//	claude/opus          the opus RELEASE across every claude generation
//	claude/opus@4        the opus release within the major-4 union
//	anthropic/claude@4   the major-4 union, narrowed to anthropic-served entities
//
// The tuple is parsed by the SAME parseEntityTuple the entity commands use — there
// is deliberately no second parser — and the provider prefix is peeled off first,
// since parseEntityTuple would otherwise read "anthropic" as the family. A #size or
// {mods} segment parses without error but has no series-level meaning (a Series is a
// family and a generation), so it simply does not narrow anything.
func canonicalSeriesSelection(all []bestiary.Series, selector string) (seriesSelection, error) {
	raw := strings.TrimSpace(selector)
	if raw == "" {
		return seriesSelection{}, fmt.Errorf(
			"empty canonical selector\n" +
				"  What: --input-format=canonical needs a selector to parse\n" +
				"  Where: bestiary series\n" +
				"  How to fix: pass one, e.g. `bestiary series claude/opus@4 --input-format=canonical`")
	}

	var provider bestiary.Provider
	hasProvider := false
	// Peel a leading "<provider>/" ONLY when it names a known provider and something
	// follows it. Requiring IsKnown is what keeps "claude/opus" reading as
	// family/variant rather than as a provider called "claude".
	if slash := strings.Index(raw, "/"); slash > 0 {
		if p := bestiary.Provider(strings.ToLower(raw[:slash])); p.IsKnown() && slash+1 < len(raw) {
			provider, hasProvider = p, true
			raw = raw[slash+1:]
		}
	}

	fam, variant, version, _, _, err := parseEntityTuple(raw)
	if err != nil {
		return seriesSelection{}, fmt.Errorf(
			"selector %q is not the canonical grammar: %w\n"+
				"  What: expected [provider/]family[/variant][@version]\n"+
				"  Where: bestiary series --input-format=canonical\n"+
				"  How to fix: e.g. claude, claude@4, claude/opus, claude/opus@4, anthropic/claude@4",
			selector, err)
	}

	wantFamily := strings.ToLower(string(fam))
	sel := seriesSelection{provider: provider, hasProvider: hasProvider}
	if variant != "" {
		sel.release, sel.hasRelease = variant, true
	}
	for _, s := range all {
		if strings.ToLower(string(s.Family)) != wantFamily {
			continue
		}
		// No version given: every generation of the family, exactly like the family
		// rung of the ladder. A version given: the strict major-union rule, so
		// "@4" unions the 4.x lines and "@4.5" narrows to one.
		if version != "" && !generationInMajorUnion(strings.ToLower(s.Generation), strings.ToLower(version)) {
			continue
		}
		// A release cut selects only the lines that actually CARRY that release. This
		// is what makes the variant segment part of the selection rather than a
		// post-filter: "claude/opus" names the lines with an opus release, so a line
		// without one is not selected at all (and a variant no line carries selects
		// nothing, rather than matching every line and then emptying).
		if sel.hasRelease && !seriesHasRelease(s, sel.release) {
			continue
		}
		sel.lines = append(sel.lines, s)
	}
	// Naming nothing is reported here rather than left to the caller's generic
	// not-found, because the canonical reading knows WHICH component missed — and
	// under --input-format=canonical, where there is no fallback to soften it, that
	// distinction is the whole value of the strict mode.
	if len(sel.lines) == 0 {
		if sel.hasRelease {
			return seriesSelection{}, fmt.Errorf(
				"selector %q names no line\n"+
					"  What: no %s line carries a %q release\n"+
					"  Where: bestiary series (canonical grammar)\n"+
					"  How to fix: run `bestiary series %s` to see the releases that family does carry",
				selector, wantFamily, sel.release, wantFamily)
		}
		if version != "" {
			return seriesSelection{}, fmt.Errorf(
				"selector %q names no line\n"+
					"  What: family %q exists in the registry but spells no generation in the %q union\n"+
					"  Where: bestiary series (canonical grammar)\n"+
					"  How to fix: run `bestiary series %s` to see the generations that family does spell",
				selector, wantFamily, version, wantFamily)
		}
		return seriesSelection{}, fmt.Errorf(
			"selector %q names no line\n"+
				"  What: no family %q is in the compiled-in registry\n"+
				"  Where: bestiary series (canonical grammar)\n"+
				"  Why: the canonical grammar reads %q as [provider/]family[/variant][@version], so the "+
				"first segment must be a family — a raw catalog id is a different grammar\n"+
				"  How to fix: use a family (e.g. `claude/opus@4`), or pass "+
				"--input-format=models.dev to read the selector as a raw model id",
			selector, wantFamily, selector)
	}
	return sel, nil
}

// rawIDSeriesSelection reads the selector as a raw models.dev model ID and returns
// the line of the entity that id belongs to.
//
// It goes through the ordinary catalog lookup (lookupEntity — the same tuple-then-id
// path the entity commands use), so a provider-unqualified id gets the established
// canonical-provider preference rather than a second, divergent resolution rule.
func rawIDSeriesSelection(all []bestiary.Series, selector string) (seriesSelection, error) {
	id := strings.TrimSpace(selector)
	m, ok := bestiary.LookupModel(bestiary.ModelID(id))
	if !ok {
		return seriesSelection{}, &bestiary.ErrNotFound{What: "model id", Key: selector}
	}
	line := bestiary.SeriesOf(bestiary.EntityRef{
		Family:    m.Family,
		Variant:   m.Variant,
		Version:   m.Version,
		ParamSize: m.ParamSize,
		Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
	})
	for _, s := range all {
		if s == line {
			return seriesSelection{lines: []bestiary.Series{s}}, nil
		}
	}
	return seriesSelection{}, &bestiary.ErrNotFound{What: "series for model id", Key: selector}
}

// seriesInMajorUnion reports whether the case-folded selector reads as
// "<this line's family>-<version>" with this line's generation in that version's
// union.
//
// The family is what SPLITS the selector — not the last dash — because a family name
// may itself contain one: "grok-build-0.1" must split as (grok-build, 0.1), and
// testing each candidate line's own family is what gets that right without a parser.
// A bare line (empty generation) is never matched: it has no generation to select.
func seriesInMajorUnion(s bestiary.Series, want string) bool {
	if s.Generation == "" {
		return false
	}
	prefix := strings.ToLower(string(s.Family)) + "-"
	if !strings.HasPrefix(want, prefix) {
		return false
	}
	version := want[len(prefix):]
	if version == "" {
		return false
	}
	return generationInMajorUnion(strings.ToLower(s.Generation), version)
}

// generationInMajorUnion is the STRICT membership rule for a version selection: a
// generation belongs to version V iff it IS V, or begins with V followed by a DOT.
//
// The mandatory dot is the whole rule, and it is deliberately a string test with NO
// numeric normalization — which is what keeps the selection honest on the catalog's
// messier generation spellings:
//
//   - "4" selects 4, 4.0, 4.1, 4.5 … but NOT 42 or 4a (a bare prefix test would
//     wrongly swallow both);
//   - "5" does NOT select glm's "5p1"/"5p2" — those are GLM 5.1/5.2 spelled with a
//     "p" upstream, and repairing that spelling belongs in parse/ where the raw IDs
//     are, not in a selector that would have to guess;
//   - "1" does NOT select "1t" (a mis-parsed 1-trillion parameter size) or the
//     leading-zero "01"/"001" spellings;
//   - "0" DOES select 0.1, 0.2 and 0.3, so the sub-1.0 lines union under 0 like any
//     other generation — no special case is needed for them.
func generationInMajorUnion(generation, version string) bool {
	return generation == version || strings.HasPrefix(generation, version+".")
}

// seriesSummaries builds the listing rows for the given lines, preserving order.
//
// The counts are POST-FILTER, and the drop rules cascade the same way the detail
// view's do: a release whose entities all filter away contributes nothing, and a
// line left with no releases is omitted from the listing entirely. That keeps the
// listing and the detail view telling the same story — `series --provider X` lists
// exactly the lines `series <line> --provider X` will render, with exactly the
// counts it will show.
func seriesSummaries(all []bestiary.Series, f seriesFilter) []seriesSummary {
	out := make([]seriesSummary, 0, len(all))
	for _, s := range all {
		releases, entities := filteredReleaseCounts(s, f)
		if f.active() && releases == 0 {
			continue
		}
		out = append(out, seriesSummary{
			Series:     s.String(),
			Family:     s.Family,
			Generation: s.Generation,
			Releases:   releases,
			Entities:   entities,
		})
	}
	return out
}

// filteredReleaseCounts returns the number of releases of s that retain at least
// one entity under f, and the total surviving entities across them.
func filteredReleaseCounts(s bestiary.Series, f seriesFilter) (releases, entities int) {
	for _, r := range bestiary.ReleasesOf(s) {
		kept := filterEntities(bestiary.EntitiesOf(r), f)
		if len(kept) == 0 {
			continue
		}
		releases++
		entities += len(kept)
	}
	return releases, entities
}

// seriesHasRelease reports whether the line carries a release of that name
// (case-folded). It reads the registry rather than a filtered view, so the SELECTION
// is decided by what the line genuinely holds and not by what a --provider or
// --quant filter happens to leave behind.
func seriesHasRelease(s bestiary.Series, name string) bool {
	for _, r := range bestiary.ReleasesOf(s) {
		if strings.EqualFold(r.Name, name) {
			return true
		}
	}
	return false
}

// releasesNamed keeps the releases whose name matches (case-folded), which is how a
// canonical selector's variant segment becomes a RELEASE-LEVEL cut. It returns a
// fresh slice and never mutates its input.
//
// The empty name is matchable in principle (it is the real, un-named bare release),
// but a canonical selector cannot express it — "claude/" has no variant segment — so
// in practice this only ever selects named releases.
func releasesNamed(in []releaseDetail, name string) []releaseDetail {
	out := make([]releaseDetail, 0, 1)
	for _, r := range in {
		if strings.EqualFold(r.Name, name) {
			out = append(out, r)
		}
	}
	return out
}

// seriesDetailOf builds the detail view of one line: its releases in
// ReleasesOf order, each with its member entity keys in EntitiesOf order.
//
// Releases emptied by the filter are dropped, and the bool reports whether the line
// itself survived — false when every release emptied, so the caller can omit the
// line rather than print a header over nothing.
func seriesDetailOf(s bestiary.Series, f seriesFilter) (seriesDetail, bool) {
	d := seriesDetail{Series: s.String(), Family: s.Family, Generation: s.Generation}
	for _, r := range bestiary.ReleasesOf(s) {
		ents := filterEntities(bestiary.EntitiesOf(r), f)
		if len(ents) == 0 {
			continue
		}
		keys := make([]string, 0, len(ents))
		for _, e := range ents {
			keys = append(keys, e.Ref.String())
		}
		d.Releases = append(d.Releases, releaseDetail{Name: r.Name, Release: r.String(), Entities: keys})
	}
	return d, len(d.Releases) > 0
}

// writeSeriesTable renders the registry-wide line listing: one row per Series with
// its release and entity counts. An un-versioned line shows "-" for GENERATION.
func writeSeriesTable(w io.Writer, rows []seriesSummary) {
	fmt.Fprintf(w, "Series (%d):\n", len(rows))
	fmt.Fprintf(w, "  %-40s %-24s %-12s %9s %9s\n", "SERIES", "FAMILY", "GENERATION", "RELEASES", "ENTITIES")
	for _, r := range rows {
		generation := r.Generation
		if generation == "" {
			generation = "-"
		}
		fmt.Fprintf(w, "  %-40s %-24s %-12s %9d %9d\n", r.Series, string(r.Family), generation, r.Releases, r.Entities)
	}
}

// writeSeriesDetailTable renders the selected lines: each release under its line,
// with the member entity keys indented beneath it. The un-named bare release is
// labelled "(bare line)" so an empty name is never rendered as blank space.
func writeSeriesDetailTable(w io.Writer, details []seriesDetail) {
	for i, d := range details {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "Series %s (%d releases):\n", d.Series, len(d.Releases))
		for _, r := range d.Releases {
			name := r.Name
			if name == "" {
				name = "(bare line)"
			}
			fmt.Fprintf(w, "  %-24s %d entities\n", name, len(r.Entities))
			for _, key := range r.Entities {
				fmt.Fprintf(w, "      %s\n", key)
			}
		}
	}
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
	fmt.Fprintf(os.Stdout, "Entity: %s\n", ent.PreferredName())
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
// (KnownDataSources) — it is stable curated data with no store reader. The ingest
// history is the UNION of the store's ingest log and the curated seed rows,
// deduplicated on the exact (source_id, ingested_at) key: a live sync writes only
// its own source's rows to the store, so a store-only export would silently drop
// provenance that lives only in the curated seed (the offline Ollama ingest, and
// any models.dev seed row not re-synced this run). Unioning keeps the export
// complete and safely promotable into parse/data/datasources.json. Sources are
// ordered by curated file order; a source's ingest rows are ascending by ingest time.
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

	for _, ds := range sources {
		merged := mergeIngestRows(storeIngestHistory(store, ds.ID), bestiary.DatasetIngestHistoryFor(ds.ID))
		for _, di := range merged {
			doc.Ingested = append(doc.Ingested, datasetIngestedExport{
				SourceID:     string(di.SourceID),
				IngestedAt:   di.IngestedAt,
				ParserSchema: di.ParserSchema,
			})
		}
	}
	return doc
}

// storeIngestHistory returns a source's ingest history from the store, or nil when
// there is no store or the read fails (a fresh/absent store yields no rows, not an
// error). It isolates the best-effort store read so the union in buildSourcesExport
// degrades to curated-only rather than surfacing a store error on export.
func storeIngestHistory(store *bestiary.Store, id bestiary.DataSourceID) []bestiary.DatasetIngested {
	if store == nil {
		return nil
	}
	hist, err := store.QueryIngestHistory(context.Background(), id)
	if err != nil {
		return nil
	}
	return hist
}

// mergeIngestRows unions one source's store and curated ingest histories,
// deduplicating on the exact ingested_at key (the source is fixed per call, so
// ingested_at IS the (source_id, ingested_at) key) and returning the rows ascending
// by ingest time. The store row wins when a key exists in both — they describe the
// same ingest — while curated-only rows (provenance that never entered the store,
// e.g. the offline Ollama ingest) are preserved. Ascending ingested_at keys are
// unique after dedup, so the ordering is fully deterministic.
func mergeIngestRows(storeRows, curatedRows []bestiary.DatasetIngested) []bestiary.DatasetIngested {
	byTime := make(map[string]bestiary.DatasetIngested, len(storeRows)+len(curatedRows))
	for _, di := range storeRows {
		byTime[di.IngestedAt] = di
	}
	for _, di := range curatedRows {
		if _, ok := byTime[di.IngestedAt]; !ok {
			byTime[di.IngestedAt] = di
		}
	}
	out := make([]bestiary.DatasetIngested, 0, len(byTime))
	for _, di := range byTime {
		out = append(out, di)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IngestedAt < out[j].IngestedAt })
	return out
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
		return writeJSON(os.Stdout, withNomina(ent))
	}
	writeEntityView(os.Stdout, ent)
	return nil
}

// entityJSON augments a marshaled Entity with its derived Nomina projection so the
// read-only naming layer (canonical Preferred nomen, provider-ID Admitted nomina, and
// any curated claim-attributed alias) surfaces in the `show --by-entity` / `entities`
// JSON output. Nomina is a method on Entity, not a struct field, so it would not
// appear in a plain marshal; embedding Entity promotes its fields and this adds the
// Nomina array alongside them (matching $defs.Entity.Nomina).
type entityJSON struct {
	bestiary.Entity
	Nomina []bestiary.Nomen
}

// withNomina wraps e for JSON output, attaching e.Nomina() (the sorted, derived
// naming projection). It is the single seam the CLI JSON entity paths route through.
func withNomina(e bestiary.Entity) entityJSON {
	return entityJSON{Entity: e, Nomina: e.Nomina()}
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
	writeInstanceTableWithStatus(w, insts, instanceStatuses(insts), instanceStages(insts))
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

// instanceStages resolves the release STAGE of each instance from its ID via the
// pure DetectStageFromID scanner (index-aligned with insts). Stage is ID-derived and
// so needs no registry lookup — it is a property of the instance ID itself, distinct
// in provenance from the upstream-declared Status resolved by instanceStatuses.
func instanceStages(insts []bestiary.ProviderInstance) []bestiary.ReleaseStage {
	out := make([]bestiary.ReleaseStage, len(insts))
	for i, in := range insts {
		out[i], _ = bestiary.DetectStageFromID(in.ID)
	}
	return out
}

// writeInstanceTableWithStatus is the pure formatter behind writeInstanceTable: it
// renders the instance table and gains a trailing STATUS column when ANY instance
// carries a non-None status AND, INDEPENDENTLY, a trailing STAGE column when ANY
// instance carries a non-None stage. statuses and stages are index-aligned with
// insts; the separation of resolution (instanceStatuses / instanceStages) from
// formatting keeps the column logic unit-testable with synthetic values.
//
// STATUS and STAGE are DISTINCT columns by design: Status is the upstream-DECLARED
// lifecycle (api.json), Stage is the ID-DERIVED release stage. Rendering them under
// separate labels keeps the two provenance levels from blurring together (a model can
// carry one, both, or neither).
func writeInstanceTableWithStatus(w io.Writer, insts []bestiary.ProviderInstance, statuses []bestiary.ModelStatus, stages []bestiary.ReleaseStage) {
	showStatus := false
	for _, s := range statuses {
		if s != bestiary.StatusNone {
			showStatus = true
			break
		}
	}
	showStage := false
	for _, s := range stages {
		if s != bestiary.StageNone {
			showStage = true
			break
		}
	}

	fmt.Fprintf(w, "Instances (%d):\n", len(insts))
	header := fmt.Sprintf("  %-*s %-*s %-*s %12s %12s %10s %10s",
		instanceIDColWidth, "ID", instanceProviderColWidth, "PROVIDER",
		instanceHostColWidth, "HOST", "IN/MTok", "OUT/MTok", "CONTEXT", "MAXOUT")
	if showStatus {
		header += fmt.Sprintf(" %-12s", "STATUS")
	}
	if showStage {
		header += fmt.Sprintf(" %-12s", "STAGE")
	}
	fmt.Fprintln(w, header)

	// Cap the rendered rows: a heavily-multi-provider entity (dozens of rehosts,
	// each possibly carrying an inline quant sub-table) would otherwise render a
	// wall of output. The header still reports the true total; --output json is the
	// escape hatch for the full set. Mirrors nominaTableLimit / benchmarkTableLimit,
	// but sits higher because the instance table is the entity view's PRIMARY
	// content, not a secondary sub-table.
	shown := insts
	overflow := 0
	if len(insts) > instanceTableLimit {
		overflow = len(insts) - instanceTableLimit
		shown = insts[:instanceTableLimit]
	}
	for i, in := range shown {
		// Truncate the free-text ID/PROVIDER/HOST cells to their column widths so a
		// long value (e.g. the 24-char "azure-cognitive-services" provider slug) can
		// no longer overrun its column and shove every numeric column out of vertical
		// alignment. truncateCell keeps the cell exactly its width (ellipsis tail),
		// matching the benchmark NAME column's existing convention.
		idCell, _ := truncateCell(string(in.ID), instanceIDColWidth)
		provCell, _ := truncateCell(string(in.Provider), instanceProviderColWidth)
		hostCell, _ := truncateCell(fmtHost(in.Host), instanceHostColWidth)
		row := fmt.Sprintf("  %-*s %-*s %-*s %12s %12s %10d %10d",
			instanceIDColWidth, idCell, instanceProviderColWidth, provCell,
			instanceHostColWidth, hostCell,
			fmtPrice(in.CostInputPerMTok), fmtPrice(in.CostOutputPerMTok),
			in.ContextWindow, in.MaxOutput)
		if showStatus {
			status := bestiary.StatusNone
			if i < len(statuses) {
				status = statuses[i]
			}
			row += fmt.Sprintf(" %-12s", fmtStatus(status))
		}
		if showStage {
			stage := bestiary.StageNone
			if i < len(stages) {
				stage = stages[i]
			}
			row += fmt.Sprintf(" %-12s", fmtStage(stage))
		}
		fmt.Fprintln(w, row)
		writeQuantRows(w, in.QuantVRAM)
	}
	if overflow > 0 {
		fmt.Fprintf(w, "  … and %d more (use --output json)\n", overflow)
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

// fmtStage renders a ReleaseStage for a table cell, mapping the zero value
// (StageNone) to a dash so a bare "none" never clutters the column.
func fmtStage(s bestiary.ReleaseStage) string {
	if s == bestiary.StageNone {
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

// typContext4K is the "typical" context length at which writeQuantRows recomputes
// a per-quant VRAM figure (the TYP column). It answers the common ollama-size
// confusion — "what does this actually cost to run at a realistic context?" —
// separately from the baked VRAM figure, which is taken at the model's full
// maximum context.
const typContext4K = 4096

// typClampGlyph is rendered in a TYP column when the model's maximum context is
// smaller than that column's context length: the model cannot serve that many
// tokens, so a VRAM figure would be meaningless. It is an em dash, deliberately
// distinct from orDash's ASCII "-" (an absent field): here it means "context out
// of range", not "value absent".
const typClampGlyph = "—"

// typVRAMCell renders the "typical" VRAM figure for one quant row at a chosen
// context length. It returns typClampGlyph when the model's maximum context
// (q.VRAMContextTokens, the window the row was baked at) is below ctx — including
// the unknown-context case (VRAMContextTokens == 0), where no ceiling is known.
// Otherwise it returns (QuantVRAM).EstimateVRAM(ctx) recomputed from the row's
// stored arch-facts. For a PARTIAL row (arch-facts absent) EstimateVRAM adds no KV
// term, so the figure equals WeightsBytes — an honest weights-only lower bound
// that never implies a phantom KV delta.
func typVRAMCell(q bestiary.QuantVRAM, ctx int) string {
	if q.VRAMContextTokens < ctx {
		return typClampGlyph
	}
	return strconv.FormatInt(q.EstimateVRAM(ctx), 10)
}

// writeQuantRows prints the per-quantization VRAM sub-rows for one instance,
// indented beneath its main row. Nothing is printed when the instance carries no
// quant data. The TYP(4K) column shows the VRAM estimate recomputed at 4096 tokens
// via typVRAMCell — the clamp glyph when the model's max context is smaller than
// 4096, a weights-only figure on a partial row. The PARTIAL column flags a
// weights-only VRAM lower bound — true means the KV-cache term was excluded because
// architecture facts were absent, so VRAMBytes (baked at the model's max context)
// is not a full estimate.
func writeQuantRows(w io.Writer, rows []bestiary.QuantVRAM) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "      %-10s %15s %15s %15s %10s %8s\n",
		"QUANT", "WEIGHTS", "VRAM", "TYP(4K)", "CTX", "PARTIAL")
	for _, q := range rows {
		fmt.Fprintf(w, "      %-10s %15d %15d %15s %10d %8t\n",
			q.Quant.String(), q.WeightsBytes, q.VRAMBytes,
			typVRAMCell(q, typContext4K),
			q.VRAMContextTokens, q.VRAMEstimatePartial)
	}
}

// writeEntityView prints the human-readable aggregate entity view.
func writeEntityView(w io.Writer, e bestiary.Entity) {
	fmt.Fprintf(w, "Entity: %s\n", e.PreferredName())
	fmt.Fprintf(w, "  Family:        %s\n", string(e.Ref.Family))
	fmt.Fprintf(w, "  Variant:       %s\n", orDash(e.Ref.Variant))
	fmt.Fprintf(w, "  Version:       %s\n", orDash(e.Ref.Version))
	fmt.Fprintf(w, "  Identity-mods: %s\n", orDash(strings.Join(e.Ref.Modifier, ",")))
	// Creator is the lab/org that TRAINED the weights (SPDX "originator"), a derived
	// Family→Creator projection distinct from the Providers (SPDX suppliers) listed
	// below. CreatorNone (unmapped family) renders "-" — an honest blank, never a
	// guessed "unknown".
	fmt.Fprintf(w, "  Creator:       %s\n", orDash(string(e.Creator)))

	providers := make([]string, len(e.Providers))
	for i, p := range e.Providers {
		providers[i] = string(p)
	}
	hosts := make([]string, len(e.Hosts))
	for i, h := range e.Hosts {
		hosts[i] = fmtHost(h)
	}
	regions := make([]string, len(e.Regions))
	for i, r := range e.Regions {
		regions[i] = r.String()
	}
	fmt.Fprintf(w, "Providers (%d): %s\n", len(e.Providers), orDash(strings.Join(providers, ", ")))
	fmt.Fprintf(w, "Hosts (%d): %s\n", len(e.Hosts), orDash(strings.Join(hosts, ", ")))
	fmt.Fprintf(w, "Regions (%d): %s\n", len(e.Regions), orDash(strings.Join(regions, ", ")))

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

	writeNominaTable(w, e.Nomina())
}

// Instance-table column widths. The three free-text leading columns (ID,
// PROVIDER, HOST) are truncated to these widths so a long value cannot overrun
// its column and knock the numeric columns out of vertical alignment. The header
// verbs use the SAME widths, so labels and cells line up exactly.
const (
	instanceIDColWidth       = 40
	instanceProviderColWidth = 22
	instanceHostColWidth     = 12
)

// instanceTableLimit is the maximum number of provider instances the TABLE view
// renders before truncating with a "… and N more (use --output json)" footer —
// the same convention as nominaTableLimit / benchmarkTableLimit. It sits higher
// than those (which are secondary sub-tables) because the instance table is the
// entity view's PRIMARY content: the cap only trims the most extreme
// multi-provider walls (e.g. the 28-instance llama@3.3#70b{instruct}) while
// leaving typical entities rendered in full. JSON output always carries every
// instance.
const instanceTableLimit = 20

// nominaTableLimit is the maximum number of nomina the TABLE view renders in full
// (header line + attestation sub-table) before truncating with a "… and N more"
// footer — the same truncation convention writeBenchmarkTable uses
// (benchmarkTableLimit), for the same reason: a heavily-hosted entity's Nomina
// section is otherwise uncapped and can run to ~50 lines (e.g. 14 nomina, one
// attestation sub-table apiece, on llama@3.3#70b{instruct} — one of the catalog's
// most-attested entities). The JSON output always carries every nomen; the table
// is capped for readability.
const nominaTableLimit = 5

// writeNominaTable renders the entity's derived naming layer: each Nomen (a
// (Value, Scheme, Status) recorded naming) followed by its attestation set —
// the per-name provenance a name HAS-MANY of. A same-triple name attested by two
// distinct sources coalesces to ONE Nomen carrying BOTH attestations, so this view
// is where a dually-attested name (e.g. a curated claim + an HF-bot harvest of the
// same huggingface-scheme repo) shows its two legs (Source/Authority/Method) at
// once — the CLI-observable form of the dual-attestation visibility guarantee
// (a name attested by multiple sources shows every attestation). Nothing
// prints when the entity has no nomina. Nomina and each nomen's Attestations
// arrive deterministically sorted from the projection; this formatter does not re-sort
// that incoming order, EXCEPT for the display-only truncation reorder below.
func writeNominaTable(w io.Writer, nomina []bestiary.Nomen) {
	if len(nomina) == 0 {
		return
	}
	fmt.Fprintf(w, "Nomina (%d):\n", len(nomina))
	// Multiply-attested nomina (the dual-attestation visibility guarantee's dual
	// curated+huggingface leg) must survive the cap below, so they sort FIRST
	// via a stable sort on attestation count before truncating — otherwise a name that merely sorts
	// earlier by Value (single attestation) could push a dually-attested name
	// past nominaTableLimit and hide a leg. Ties preserve the incoming
	// (Value, Scheme, entity key) order from the projection (sortNomina/
	// lessNomen). This is a LOCAL COPY reorder for display only: it never
	// touches the caller's `nomina` slice, so the JSON output (which reads
	// the entity's untouched Nomina() projection) keeps its original order.
	shown := append([]bestiary.Nomen(nil), nomina...)
	sort.SliceStable(shown, func(i, j int) bool {
		return len(shown[i].Attestations) > len(shown[j].Attestations)
	})
	if len(shown) > nominaTableLimit {
		shown = shown[:nominaTableLimit]
	}
	for _, n := range shown {
		fmt.Fprintf(w, "  %s  (%s, %s)\n", n.Value, n.Scheme.String(), n.Status.String())
		// Each attestation is one independent piece of evidence for this naming:
		// WHICH ingest we read it from (Source), whose VOICE the document is
		// (Authority), HOW it entered (Method), and WHO asserts it (SourceURL, a
		// dash when self-minted with no claimant URL).
		fmt.Fprintf(w, "      %-12s %-10s %-12s %s\n",
			"SOURCE", "AUTHORITY", "METHOD", "SOURCE-URL")
		for _, at := range n.Attestations {
			fmt.Fprintf(w, "      %-12s %-10s %-12s %s\n",
				orDash(string(at.Source)), at.Authority.String(),
				at.Method.String(), orDash(at.SourceURL))
		}
	}
	if len(nomina) > nominaTableLimit {
		fmt.Fprintf(w, "  … and %d more (use --output json)\n", len(nomina)-nominaTableLimit)
	}
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

// benchmarkNameColWidth is the fixed display width of the NAME column. Names
// wider than this are shortened to a single trailing "…" so the SCORE/METRIC/…
// columns stay aligned regardless of an individual name's length (e.g.
// "Artificial Analysis Coding Index"). The full names remain available via
// --output json. The width matches the column's format verb below and is chosen
// to hold the common benchmark names without truncation while bounding the outliers.
const benchmarkNameColWidth = 24

// truncateCell shortens s to at most width display columns, replacing the tail
// with a single "…" rune when s would overflow, and reports whether truncation
// occurred. Runes are counted (not bytes), so a multi-byte name is measured by
// visible length; the returned truncated cell is exactly width runes wide (width-1
// content runes + the ellipsis), which the "%-*s" verb pads consistently with the
// untruncated (ASCII) cells so the columns line up. width must be ≥ 1.
func truncateCell(s string, width int) (string, bool) {
	if utf8.RuneCountInString(s) <= width {
		return s, false
	}
	return string([]rune(s)[:width-1]) + "…", true
}

// writeBenchmarkTable prints the lab-reported benchmark claims as a
// NAME|SCORE|METRIC|HARNESS|DATE|SOURCE table — fields kept in separate columns,
// never concatenated. At most benchmarkTableLimit rows render; when more exist a
// "… and N more (use --output json)" footer names the omitted count. Benchmark
// names wider than benchmarkNameColWidth are truncated to keep the columns
// aligned; when any rendered row was truncated a single note points at the full
// names in --output json. Nothing is printed when the entity has no benchmarks.
func writeBenchmarkTable(w io.Writer, benchmarks []bestiary.BenchmarkResult) {
	if len(benchmarks) == 0 {
		return
	}
	fmt.Fprintf(w, "Benchmarks (%d):\n", len(benchmarks))
	fmt.Fprintf(w, "  %-*s %12s %-14s %-18s %-12s %s\n",
		benchmarkNameColWidth, "NAME", "SCORE", "METRIC", "HARNESS", "DATE", "SOURCE")

	shown := benchmarks
	if len(shown) > benchmarkTableLimit {
		shown = shown[:benchmarkTableLimit]
	}
	nameTruncated := false
	for _, b := range shown {
		name, truncated := truncateCell(orDash(b.Name), benchmarkNameColWidth)
		nameTruncated = nameTruncated || truncated
		fmt.Fprintf(w, "  %-*s %12s %-14s %-18s %-12s %s\n",
			benchmarkNameColWidth, name, benchScoreCell(b), orDash(b.Metric),
			orDash(b.Harness), orDash(b.Date), orDash(b.SourceURL))
	}
	if len(benchmarks) > benchmarkTableLimit {
		fmt.Fprintf(w, "  … and %d more (use --output json)\n", len(benchmarks)-benchmarkTableLimit)
	}
	if nameTruncated {
		fmt.Fprintf(w, "  note: benchmark names truncated (use --output json for full names)\n")
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
//     It also registers the curated, huggingface and self-referential bestiary
//     dimension rows, whose committed ingest timestamps come from the seed: the
//     nomina persisted below carry those source_id FKs.
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
	// Also register the curated DataSource: the nomina persisted below include
	// curated third-party alias claims whose Source is DataSourceCurated (the honest
	// ingest — bestiary read them from its own committed claim files, not from
	// models.dev). The nomina.source_id foreign key references data_sources, so the
	// curated dimension row MUST exist before UpsertNomina. Its committed ingest
	// timestamp comes from the seed (a curation snapshot, not this sync's wall-clock).
	curatedDS, ok := bestiary.DataSourceByID(bestiary.DataSourceCurated)
	if !ok {
		curatedDS = bestiary.DataSource{
			ID:            bestiary.DataSourceCurated,
			URI:           "https://github.com/dayvidpham/bestiary/tree/main/parse/data",
			CanonicalName: "bestiary curated claim files",
		}
	}
	curatedIngest, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceCurated)
	if !ok {
		curatedIngest = bestiary.DatasetIngested{
			SourceID:     bestiary.DataSourceCurated,
			IngestedAt:   now,
			ParserSchema: modelsDevParserSchema,
		}
	}
	// Also register the huggingface DataSource: the nomina persisted below include
	// HARVESTED HuggingFace nomina (the embedded huggingface_nomina.json seed, folded
	// by MintNominaFromModels) whose Source is DataSourceHuggingFace. The
	// nomina.source_id foreign key references data_sources, so this dimension row MUST
	// exist before UpsertNomina. Its committed ingest timestamp comes from the seed
	// (the offline cmd/bestiary-hf run's snapshot, not this sync's wall-clock).
	hfDS, ok := bestiary.DataSourceByID(bestiary.DataSourceHuggingFace)
	if !ok {
		hfDS = bestiary.DataSource{
			ID:            bestiary.DataSourceHuggingFace,
			URI:           "https://huggingface.co/api/models",
			CanonicalName: "HuggingFace Hub",
		}
	}
	hfIngest, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceHuggingFace)
	if !ok {
		hfIngest = bestiary.DatasetIngested{
			SourceID:     bestiary.DataSourceHuggingFace,
			IngestedAt:   now,
			ParserSchema: modelsDevParserSchema,
		}
	}
	// Also register the self-referential bestiary DataSource: every SELF-MINTED
	// canonical nomen persisted below is attributed to DataSourceBestiary — bestiary
	// authored the key, so no upstream is the honest Source. The nomina.source_id
	// foreign key references data_sources, so this dimension row MUST exist before
	// UpsertNomina. Its committed ingest timestamp comes from the seed.
	selfDS, ok := bestiary.DataSourceByID(bestiary.DataSourceBestiary)
	if !ok {
		selfDS = bestiary.DataSource{
			ID:            bestiary.DataSourceBestiary,
			URI:           "https://github.com/dayvidpham/bestiary",
			CanonicalName: "bestiary (self-minted)",
		}
	}
	selfIngest, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceBestiary)
	if !ok {
		selfIngest = bestiary.DatasetIngested{
			SourceID:     bestiary.DataSourceBestiary,
			IngestedAt:   now,
			ParserSchema: modelsDevParserSchema,
		}
	}
	if err := store.UpsertDataSources(ctx,
		[]bestiary.DataSource{ds, curatedDS, hfDS, selfDS},
		[]bestiary.DatasetIngested{ingest, curatedIngest, hfIngest, selfIngest}); err != nil {
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

	// Persist the naming layer (v7): mint the provider-ID + canonical nomina from the
	// fetched models plus the curated alias claims, through the same shared joint the
	// registry uses, and upsert them. The nomina source_id FK is satisfied by the
	// models.dev DataSource written above.
	if err := store.UpsertNomina(ctx, bestiary.MintNominaFromModels(fetched)); err != nil {
		return fmt.Errorf("sync: persist nomina: %w", err)
	}

	// Persist the creators BCNF dimension (v8) from the curated creators.json seed — the
	// data_sources dimension-persistence precedent — so the cache is self-describing about
	// Family → Creator without recompiling. It is derived from the same seed the baked
	// static registry and the runtime Family.Creator projection use, so all three agree.
	if err := store.UpsertCreators(ctx); err != nil {
		return fmt.Errorf("sync: persist creators dimension: %w", err)
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
