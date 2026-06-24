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

## v0.2.4 — VRAM, quantization & data-source provenance

v0.2.4 answers "what will this model cost to *run*, at which quantization, and where did
the data come from?" It adds three things on top of the canonical entity model:
**parameter size as part of identity**, **per-quantization weights + computed VRAM**, and a
**data-source provenance** core that records every source that attests to a model.

> All CLI examples below write the flag **before** the positional argument
> (`show --by-entity <key>`). Flags are accepted in any position — `show <key> --by-entity`
> works too — but the flag-first form is the one shown here for consistency.

### Parameter size is part of identity

`EntityRef.String()` gains an optional `#paramsize` segment:

```
family[/variant][@version][#paramsize]{identity-mods}
```

So a 70B and an 8B of the same family are **distinct entities** with distinct keys — they
have different weights, different VRAM, and different architectures, so they are not the same
thing served at two sizes:

```sh
$ bestiary show --by-entity 'llama@3.3#70b{instruct}'
{
  "Ref": {
    "Family": "llama",
    "Variant": "",
    "Version": "3.3",
    "ParamSize": "70b",
    "Modifier": ["instruct"]
  },
  ...
}

$ bestiary show --by-entity 'llama@3.3#8b{instruct}'
{
  "Ref": { "Family": "llama", "Version": "3.3", "ParamSize": "8b", "Modifier": ["instruct"] },
  ...
}
```

The `#` segment is **omitted when size is unknown**, so every pre-v0.2.4 entity key is
byte-identical — sizing is purely additive. Today only a small curated set carries sizes;
broader coverage is incremental.

### Per-quantization weights vs. VRAM

Each provider instance of a curated local model carries a `QuantVRAM` row per quantization.
The table view is the most compact way to read it:

```sh
$ bestiary providers --output table 'llama@3.3#70b{instruct}'
Entity: llama@3.3#70b{instruct}
Instances (8):
  ID                                       PROVIDER               HOST              IN/MTok     OUT/MTok    CONTEXT     MAXOUT
  llama-3.3-70b-instruct                   azure                  -                  0.7100       0.7100     128000      32768
      QUANT              WEIGHTS            VRAM        CTX  PARTIAL
      q4_k_m         43033509888     85983182848     131072    false
      q8_0           75176521728    118126194688     131072    false
      f16           141166166016    184115838976     131072    false
  ...
```

Two numbers matter, and they are deliberately **not** the same:

- **`WEIGHTS`** is the ground-truth GGUF **file size** ingested from the Ollama registry
  (the q4_k_m download is ≈40 GiB = `43,033,509,888` bytes). This is the number Ollama lists
  on a model's tags page — it is a *download/disk* size, **not** a VRAM figure.
- **`VRAM`** is *computed* by bestiary: `weights + KV-cache`, **baked at the model's maximum
  context** (`CTX` = 131072 here), with **no overhead constant** (`VRAMFormulaVersion 2`).

For the 70B at q4_k_m, `VRAM − WEIGHTS = 85,983,182,848 − 43,033,509,888 = 42,949,672,960` =
exactly **40 GiB of fp16 KV-cache** at 128K context (≈320 KiB/token). That is why the VRAM
figure is roughly **2× the file size at full context** — it is physically correct, not a bug:
the KV-cache at a 128K window is as large as the quantized weights themselves. At a smaller
working context the figure is far closer to the file size; `(QuantVRAM).EstimateVRAM(ctx)`
recomputes it from the stored inputs at any context you choose.

When the architectural facts (layers / KV-heads / head-dim) are **absent**, the KV term is
excluded and the row is flagged `PARTIAL true` — `VRAM` then equals `WEIGHTS`, an honest
weights-only **lower bound**, never a silent under-estimate:

```sh
$ bestiary providers --output table 'llama@3.2#3b{instruct}'
Entity: llama@3.2#3b{instruct}
Instances (1):
  ...
      QUANT              WEIGHTS            VRAM        CTX  PARTIAL
      q4_k_m          2019139072      2019139072     131072     true
      q8_0            3419799040      3419799040     131072     true
```

### Filtering by quantization

`--quant` keeps only the instances that carry a matching quantization row (it applies to
`providers` and `show --by-entity`):

```sh
$ bestiary providers --output table --quant f16 'llama@3.3#70b{instruct}'   # 8 instances, all carry f16
$ bestiary providers --output table --quant f16 'llama@3.2#3b{instruct}'    # Instances (0): 3b has no f16 row
```

An unrecognized quant is rejected with an actionable error rather than silently ignored:

```sh
$ bestiary providers --quant nope 'llama@3.3#70b{instruct}'
bestiary: ParseQuantization: unrecognised quantization "nope"; why: the input does not
match any known quantization name (case-insensitive); ... how to fix: pass one of the
canonical wire names (f16, bf16, f32, q4_0, q8_0, q4_k_m, q5_k_m, iq4_nl, other).
```

### Where the data came from (`sources`)

Every entity records which data sources attest to it. `bestiary sources <key>` joins the
provenance tables and prints one row per source — its URI, ingest date, and parser-schema
version:

```sh
$ bestiary sources --output table 'llama@3.3#70b{instruct}'
Entity: llama@3.3#70b{instruct}
Sources (2):
  SOURCE       URI                                INGESTED                 PARSER
  models.dev   https://models.dev/api.json        2026-06-09T00:00:00Z          2
  ollama       https://registry.ollama.ai         2026-06-09T00:00:00Z          2
```

The 70B is **dual-attested** (`[models.dev, ollama]`): models.dev knows it as a hosted API
model, and the Ollama ingest contributed its per-quant weights. A model only models.dev knows
about reports a single source:

```sh
$ bestiary sources --output table 'claude/opus@4.5'
Entity: claude/opus@4.5
Sources (1):
  SOURCE       URI                                INGESTED                 PARSER
  models.dev   https://models.dev/api.json        2026-06-09T00:00:00Z          2
```

The same provenance is available as JSON (`bestiary sources <key>`, no `--output`), and the
`Entity.Sources` array is included on every `show --by-entity` / `providers` JSON document.

### Refreshing the Ollama data (`cmd/bestiary-ollama`)

The per-quant weights/architecture data lives in a committed curated file
(`parse/data/quant_vram.json`) that codegen bakes into the static catalog — `list` / `show`
/ `sources` never touch the network. To refresh that file from the live Ollama registry, a
human runs the **offline, network-gated** `cmd/bestiary-ollama` tool. It is a polite bot
(descriptive User-Agent, ≥1 s between requests), joins each Ollama tag onto a models.dev
catalog ID (alias table first, then mechanical decomposition), **keeps** community finetunes
rather than dropping them, and merges fetch-owned fields into the curated file while
preserving hand-curated architecture facts. It is **not** part of `go test ./...`.

## CLI

```
bestiary <list|show|providers|sources|sync> [flags]
```

### Commands

**show** — resolve a single model and print it (offline). The argument is interpreted in the
canonical ("peasant") form by default; use `--format` to supply HuggingFace, PURL, or raw IDs.
If the input matches more than one model, an ambiguous-candidate listing is printed to stderr
and the command exits non-zero. Pass `--by-entity` to resolve a `#size`-aware **entity key**
(`family[/variant][@version][#paramsize]{mods}`) and print the aggregated entity — its
instances, per-quant VRAM, capability union, and source list — instead of a single row.

```sh
bestiary show 'anthropic/claude/opus/4.6@2026-02-05'      # canonical form (default)
bestiary show claude-opus-4-6 --format raw                # raw API model ID
bestiary show anthropic/claude-opus-4-6 --format hf       # HuggingFace repo-id
bestiary show pkg:huggingface/anthropic/claude-opus-4-5 --format purl
bestiary show 'anthropic/claude/opus/4.6@2026-02-05' --output yaml
bestiary show --by-entity 'llama@3.3#70b{instruct}'       # aggregated entity by #size key
```

**providers** — resolve an entity key and list every provider instance that serves it,
including per-quantization weights/VRAM rows in the table view.

```sh
bestiary providers --output table 'llama@3.3#70b{instruct}'
bestiary providers --output table --quant q4_k_m 'llama@3.3#70b{instruct}'
```

**sources** — resolve an entity key and print its data-source provenance (one row per
attesting source: URI, ingest date, parser-schema version). Offline.

```sh
bestiary sources --output table 'llama@3.3#70b{instruct}'   # dual-attested: models.dev + ollama
bestiary sources 'claude/opus@4.5'                          # JSON; models.dev-only
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
| `--by-entity` | `show` | `false` | Resolve the argument as a `#size`-aware entity key and print the aggregated entity instead of a single model row. |
| `--quant` | `providers`, `show --by-entity` | (all) | Keep only instances carrying a matching quantization row (e.g. `q4_k_m`, `f16`). An unrecognized value is rejected with an actionable error. |
| `--provider` | `list`, `sync` | (all) | Filter by provider slug (e.g. `anthropic`, `google`, `openai`). |
| `--db-path` | all | `$XDG_CACHE_HOME/bestiary/models.db` | SQLite cache location. |
| `--scheme` | `show` | — | **Deprecated** alias for `--format`; kept for v0.0.1 scripts. `--format` wins if both are set. |

Flags are positional-order-independent: `bestiary show --by-entity <key>` and
`bestiary show <key> --by-entity` are equivalent.

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

## Releases

Release tags are created automatically when a **release PR is merged**. To cut a release:

1. Open a PR into `main` whose **title** is `release(vX.Y.Z): <summary>` — the version lives in the
   conventional-commit scope, e.g. `release(v0.2.3): lineage + entity linking`. Pre-releases are
   supported: `release(v0.2.3-rc1): …`. A space after the `):` is required (`release(v0.2.3):x` is
   not recognized).
2. Merge it. The [`tag-on-release-merge`](.github/workflows/tag-on-release-merge.yml) workflow
   validates the title, then creates the annotated tag `vX.Y.Z` on the **resulting commit on `main`**
   (whether you squash or create a merge commit) and pushes it.

Notes:

- **Only active once it has landed on `main`.** Because the trigger is `pull_request`, GitHub runs
  the workflow from the copy on the base branch — so the PR that *introduces* the workflow does not
  tag itself, and any release merged before it reaches `main` must be tagged manually.
- Any PR whose title is not a strict `release(vX.Y.Z): …` is ignored (a silent no-op).
- If the tag already exists, the workflow **fails loudly** (it never force-moves a published tag),
  so a duplicate or mistyped release PR is caught rather than silently doing nothing.
- A tag pushed by the workflow's `GITHUB_TOKEN` does **not** trigger downstream `on: push: tags`
  workflows; use a PAT or deploy key if you later chain a release-build job off the tag.

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
