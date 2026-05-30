# bestiary

Go module and CLI for querying AI model metadata from [models.dev](https://models.dev), with a **canonical naming scheme** that gives every model a stable, cross-provider identity.

Provides strongly-typed providers and model IDs, a static model registry (~4,300 models across ~115 providers), entity normalization (Family / Variant / Version / Date / Modifier), an HTTP client with retry, and a local SQLite cache for offline queries.

## Install

**As a Go library:**

```sh
go get github.com/dayvidpham/bestiary@v0.2.0
```

**As a CLI tool:**

```sh
go install github.com/dayvidpham/bestiary/cmd/bestiary@latest
```

Requires Go 1.24+. Builds with `CGO_ENABLED=0` (no C compiler needed).

## The canonical representation

The same model shows up across many providers under inconsistent raw IDs. Anthropic's
Claude Opus 4.6 appears as `claude-opus-4-6` direct from Anthropic, as
`anthropic/claude-opus-4.6` on Vercel, as `claude-opus-4-6-thinking` on a reseller, and
as `eu.anthropic.claude-opus-4-6-v1` on a cloud gateway. The models.dev `family` field is
just as noisy — `claude-opus` conflates the family (`claude`) with the variant (`opus`),
and ~25% of entries have no family at all.

bestiary normalizes each model into a **canonical tuple** and renders it as a single
human-readable string:

```
provider / family / variant / version @ date [modifier]

anthropic / claude  / opus    / 4.6     @ 2026-02-05
```

| Field | Example | Meaning |
|-------|---------|---------|
| `Provider` | `anthropic` | Who hosts this manifestation of the model |
| `Family` | `claude` | The model line (provider-independent) |
| `Variant` | `opus` | The tier/branch within the family |
| `Version` | `4.6` | The version, **distinct from the date** — Opus 4.5 and 4.6 are different models |
| `Date` | `2026-02-05` | Release/snapshot date |
| `Modifier` | `thinking` | An optional mode suffix (`thinking`, `vision`, `latest`, …) |

The full canonical form for a modifier-bearing model:

```
alibaba-cn/kimi@2025-11-06[thinking]
```

**Why a tuple?** The tuple `(Family, Variant, Version, Date)` is the *canonical* identity;
the string forms are just convenience formatters over it. Two design properties fall out:

- **Cross-provider comparison.** `(Family, Variant, Version)` groups the *same* model
  across every provider that hosts it, so you can ask "who serves Claude Opus 4.6?" or
  "what does it cost on each?" without string-matching raw IDs.
- **Version ≠ date.** Splitting `Version` from `Date` is the whole point: a snapshot date
  alone can't tell Opus 4.5 from 4.6. The parser extracts the version from the model ID
  (which is authoritative) and falls back to the API family field.

Normalization is deterministic (suffix tables + curated overrides in `parse/data/`), so it's
auditable and easy to fix. Inputs the parser can't cleanly decompose are recorded to
`.bestiary-gen-cache/parse_failures.json` at codegen time rather than silently mangled.

> The design draws on ISO 1087 / IFLA-LRM terminology concepts (a concept vs. its
> designations); see [`docs/research/entity-normalization.md`](docs/research/entity-normalization.md)
> for the full rationale. Today every designation is rated *admitted*; promotion to
> *preferred* is deferred to a later curation pass.

## Demo

**Resolve a model by its canonical form** (`bestiary show` defaults to canonical/"peasant" input):

```sh
$ bestiary show 'anthropic/claude/opus/4.6@2026-02-05'
{
  "ID": "claude-opus-4-6",
  "Provider": "anthropic",
  "DisplayName": "Claude Opus 4.6",
  "RawFamily": "claude-opus",
  "Family": "claude",
  "Variant": "opus",
  "Version": "4.6",
  "Date": "2026-02-05",
  "Modifier": "",
  "ContextWindow": 1000000,
  "MaxOutput": 128000,
  "Reasoning": true,
  ...
}
```

**Bare or partial inputs are ambiguous** — many providers host the same model, so bestiary
lists the canonical provider (marked `*`) separately from the rehosts:

```sh
$ bestiary show claude
* = canonical provider

Canonical:
* anthropic/claude/opus/4.6@2026-02-05
* anthropic/claude/sonnet/4.6@2026-02-17
* anthropic/claude/haiku/4.5@2025-10-15
* anthropic/claude/opus/4.5@2025-11-24
* anthropic/claude/sonnet/4.5@2025-09-29
+9 more

Also rehosted by:
  deepinfra
  perplexity-agent
  azure-cognitive-services
  fastrouter
  nano-gpt
+24 more

To see all providers/variants: bestiary list   (or: bestiary list --provider <slug>)
To resolve an exact model ID:  bestiary show <raw-id> --format=raw
```

**Other input formats** are opt-in via `--format`. A Package-URL with a provider namespace
filters to that provider, falling back to a loose cross-provider match when the namespace
has no hit:

```sh
$ bestiary show --format purl 'pkg:huggingface/anthropic/claude-opus-4-5'
{ "ID": "claude-opus-4-5", "Provider": "anthropic", "Family": "claude", "Version": "4.5", ... }
```

**List models in a table:**

```sh
$ bestiary list --provider anthropic --output table
ID                                        Provider      Family              Context  MaxOutput  Reason  Tools   CostIn/MTok
----------------------------------------  ------------  ----------------  ---------  ---------  ------  -----  ------------
claude-3-5-haiku-20241022                 anthropic     claude               200000       8192      no    yes         $0.80
claude-haiku-4-5                          anthropic     claude               200000      64000     yes    yes         $1.00
claude-opus-4-6                           anthropic     claude              1000000     128000     yes    yes         $5.00
...
```

## CLI

```
bestiary <list|show|sync> [flags]
```

### Commands

**show** — resolve a single model and print it (offline). The argument is interpreted in the
canonical ("peasant") form by default; use `--format` to supply HuggingFace, PURL, or raw IDs.
If the input matches more than one model, an ambiguous-candidate listing is printed to stderr
and the command exits non-zero.

```sh
bestiary show 'anthropic/claude/opus/4.6@2026-02-05'      # canonical form (default)
bestiary show claude-opus-4-6 --format raw                # raw API model ID
bestiary show anthropic/claude-opus-4-6 --format hf       # HuggingFace repo-id
bestiary show pkg:huggingface/anthropic/claude-opus-4-5 --format purl
bestiary show 'anthropic/claude/opus/4.6@2026-02-05' --output yaml
```

**list** — query models from the static registry + local cache (offline).

```sh
bestiary list                                       # all models, JSON
bestiary list --provider anthropic --output table   # Anthropic models, table
bestiary list --output yaml                          # all models, YAML
```

**sync** — fetch models from the models.dev API and cache locally (online).

```sh
bestiary sync                                        # fetch all, print JSON
bestiary sync --provider anthropic --output table
```

After syncing, `list` and `show` merge static + cached data. When both sources have the same
`(ID, Provider)`, the most recently synced version wins.

### Flags

| Flag | Applies to | Default | Description |
|------|-----------|---------|-------------|
| `--output` | all | `json` | Output rendering: `json`, `yaml`, `table`. *(Was `--format` in v0.0.1.)* |
| `--format` | `show` | `peasant` | **Input** scheme for the model argument: `peasant` (canonical), `huggingface`/`hf`, `purl`, `raw`. No auto-detection — non-canonical inputs must select their format. |
| `--provider` | `list`, `sync` | (all) | Filter by provider slug (e.g. `anthropic`, `google`, `openai`). |
| `--db-path` | all | `$XDG_CACHE_HOME/bestiary/models.db` | SQLite cache location. |
| `--scheme` | `show` | — | **Deprecated** alias for `--format`; kept for v0.0.1 scripts. `--format` wins if both are set. |

## Library usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dayvidpham/bestiary"
)

func main() {
	// Static registry (compiled-in, no network)
	models := bestiary.StaticModels()
	fmt.Printf("%d models available\n", len(models))

	// Resolve any expression to canonical ModelRef(s).
	// Returns ErrAmbiguous (use errors.As) when the input matches multiple models.
	refs, err := bestiary.Resolve("anthropic/claude/opus/4.6@2026-02-05")
	if err == nil && len(refs) > 0 {
		r := refs[0]
		fmt.Println(r.Format(bestiary.SchemeCanonical))   // anthropic/claude/opus/4.6@2026-02-05
		fmt.Println(r.Format(bestiary.SchemeHuggingFace)) // anthropic/claude-opus-4-6
		fmt.Println(r.Format(bestiary.SchemePURL))        // pkg:huggingface/anthropic/claude-opus-4-6
		fmt.Println(r.Format(bestiary.SchemeRaw))         // claude-opus-4-6
	}

	// Opt into a non-canonical input scheme.
	refs, err = bestiary.Resolve(
		"pkg:huggingface/anthropic/claude-opus-4-5",
		bestiary.WithInputFormat(bestiary.InputFormatPURL),
	)

	// The canonical provider for a family (e.g. claude -> anthropic).
	fmt.Println(bestiary.Family("claude").CanonicalProvider())

	// Lookup / filter the static registry.
	if m, ok := bestiary.LookupModelByProvider(bestiary.ProviderAnthropic, "claude-opus-4-6"); ok {
		ref := m.Ref() // 8-field ModelRef
		fmt.Printf("%s v%s @ %s\n", ref.Family, ref.Version, ref.Date)
	}
	for _, m := range bestiary.ModelsByFamily("claude") {
		fmt.Println(m.ID)
	}
}
```

**Fetching live data:**

```go
ctx := context.Background()
client := bestiary.NewClient(
	bestiary.WithTimeout(10*time.Second),
	bestiary.WithRetries(3),
)
models, err := client.FetchModels(ctx)
// or: client.FetchModelsByProvider(ctx, bestiary.ProviderGoogle)
```

**Generated constants.** `go generate` emits a `Model__*` constant for every model, named
`Model__<Provider>__<Family>__<Variant>__<Version>__<Modifier>__<Date>` (double underscores
between components, single within), e.g. `Model__Anthropic__Claude__Opus__4_6__20260205`.

## Types

| Type | Description |
|------|-------------|
| `Provider` | String type with well-known constants (`ProviderAnthropic`, `ProviderGoogle`, `ProviderOpenAI`, `ProviderLocal`). Any models.dev slug is valid. |
| `ModelID` | String type for raw API model identifiers (e.g. `"claude-opus-4-6"`). |
| `ModelInfo` | Full model metadata: API fields + normalized `RawFamily`/`Family`/`Variant`/`Version`/`Date`/`Modifier`. |
| `ModelRef` | The 8-field canonical identity tuple `(ID, Provider, RawFamily, Family, Variant, Version, Date, Modifier)` with `Format(scheme)` and `String()`. |
| `CanonicalScheme` | Int enum: `SchemeCanonical`, `SchemeHuggingFace`, `SchemePURL`, `SchemeRaw`. |
| `InputFormat` | Parsed `--format` value: `InputFormatPeasant`, `InputFormatHuggingFace`, `InputFormatPURL`, `InputFormatRaw`. |
| `Designation` | A serialized identifier `(Value, Scheme, Provider, Rating)` — one model has many designations. |
| `AcceptabilityRating` | ISO-1087 rating: `AcceptabilityAdmitted` (default), `AcceptabilityPreferred`, `AcceptabilityDeprecated`. |
| `ErrAmbiguous` | Struct error (use `errors.As`) carrying the candidate `[]ModelRef`; returned by `Resolve` when an input matches multiple models. |
| `Modality` / `Modalities` | Int enum + `Input`/`Output` modality lists. |
| `Capability` | `Supported bool` + `Config map[string]string` for polymorphic fields (e.g. `Interleaved`). |

### Canonical string schemes

| Scheme | Output for Claude Opus 4.6 |
|--------|----------------------------|
| `SchemeCanonical` | `anthropic/claude/opus/4.6@2026-02-05` |
| `SchemeHuggingFace` | `anthropic/claude-opus-4-6` |
| `SchemePURL` | `pkg:huggingface/anthropic/claude-opus-4-6` |
| `SchemeRaw` | `claude-opus-4-6` |

## Schema versioning

bestiary tracks two versions (see `version.go`):

- **BestiarySchemaVersion** — semver for bestiary's public output contract, documented by the
  JSON Schema (`bestiary.schema.json`). The v0.0.1 → v0.0.2 changes (new normalized fields,
  added methods) were additive; see [`MIGRATION_v0.0.1_to_v0.0.2.md`](MIGRATION_v0.0.1_to_v0.0.2.md).
- **UpstreamSchemaVersion** — `<YYYY.MM.DD>-<sha256>` pinning the models.dev schema snapshot
  bestiary was built against, plus `UpstreamGitCommit` / `UpstreamGitRemote` for provenance.

(The module release tag — `v0.2.0` — is a separate axis from `BestiarySchemaVersion`: the Go
API grew substantially, while the JSON wire format stayed backward-compatible.)

## Updating static data

The static registry and `Model__*` constants are code-generated from the models.dev API:

```sh
go generate ./...
```

This runs `cmd/bestiary-gen`, which fetches `https://models.dev/api.json`, normalizes every
entry, and writes `models_static_gen.go` / `models_constants_gen.go`. Useful flags:

| Flag | Description |
|------|-------------|
| `--cache-dir <dir>` | Where the fetched API response and parse-failure log are written. |
| `--no-fetch` | Offline mode: reuse the cached `api_response.json` instead of hitting the network. |

Parse failures are written to `<cache-dir>/parse_failures.json` for review.

## Dependencies

- Go 1.24+
- `zombiezen.com/go/sqlite` (CGO-free SQLite driver)
- No other external dependencies

## License

MIT
