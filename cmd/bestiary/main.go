package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

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
		return fmt.Errorf("usage: bestiary <list|show|sync> [flags]")
	}

	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	provider := fs.String("provider", "", "filter by provider slug")
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

	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	switch cmd {
	case "list":
		return runList(*provider, bestiary.OutputFormat(*output), *dbPath)
	case "show":
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: bestiary show <model-id> [--format=<peasant|huggingface|hf|purl|raw>] [--output=<json|yaml|table>] [flags]")
		}
		return runShow(fs.Arg(0), bestiary.OutputFormat(*output), *dbPath, *inputFormat, *scheme)
	case "sync":
		return runSync(*provider, bestiary.OutputFormat(*output), *dbPath)
	default:
		return fmt.Errorf("unknown command %q; supported commands: list, show, sync", cmd)
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
