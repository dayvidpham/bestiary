# bestiary

Go module and CLI for querying AI model metadata from [models.dev](https://models.dev), with a **canonical naming scheme** that gives every model a stable, cross-provider identity.

Provides strongly-typed providers and model IDs, a static model registry (~5,650 models across ~162 providers), entity normalization (Family / Variant / Version / Date / Modifier), an HTTP client with retry, and a local SQLite cache for offline queries.

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

## Canonical entity keys

The section above names *one provider's* copy of a model. Collapse every provider that
serves the same underlying model into a single cross-provider identity and you get an
**entity** — the canonical representation bestiary has converged on for a model. An entity
is written as one canonical key:

```
family[/variant][@version][#paramsize]{identity-mods}
```

A trailing `[attributes]` segment is accepted as **input / filter syntax only** — it is
never rendered into a key, because attributes are per-*instance* data, not part of an
entity's identity.

| Segment | Meaning |
|---------|---------|
| `family` | The canonical, provider-independent model line (`llama`, `gemini`, `gpt`). |
| `/variant` | The product-line variant/tier within the family (`flash`, `xs`). Omitted when the family has none. |
| `@version` | The version — **distinct from the release date** (Opus 4.5 ≠ 4.6). |
| `#paramsize` | Parameter size, which **is** identity: a 70B and an 8B are different models (different weights, VRAM, architecture). **Omitted when the size is unknown**, so keys without a known size are unchanged. |
| `{identity-mods}` | Identity-class modifiers (`instruct`, `thinking`, `omni`, `livetranslate`, …), emitted in canonical order. These name genuinely different artifacts, so they are **part of the key**. |
| `[attributes]` | Attribute-class tokens: serving tiers (`realtime`, `fast`) and release-stage tokens (`preview`, `latest`). These are per-instance runtime knobs — **filterable, but never part of the key**. |

Every key below is real — resolve any of them with `bestiary show --by-entity '<key>'`:

| Entity key | What it demonstrates |
|------------|----------------------|
| `llama@3.3#70b{instruct}` | Size lives in the key: a *distinct* entity from `llama@3.3#8b{instruct}`. |
| `gemini/flash{omni}` vs `gemini/flash` | An identity mode token (`omni`) splits one variant into two entities. |
| `gpt@2.1` | `realtime` rode in as an **attribute** (stripped off `gpt-realtime-2.1`), so it stays out of the key and the version `2.1` is preserved. |
| `qwen/flash@3{livetranslate}` | A stage/mode token carried as an identity modifier. |
| `laguna/xs@2.1` | The plain shape: family, variant, version, no modifiers. |
| `ornith@1.0#9b` | A **metadata-only standalone** — zero providers serve it, but models.dev still publishes facts, so it is synthesized as its own `#size`-keyed entity rather than dropped. |

The rationale for each rule — why parameter size is identity, why `omni`/`livetranslate`
are identity-class while `realtime`/`preview` are attributes, and how metadata-only
standalones are synthesized — is recorded in the design-decisions sections of
[`AGENTS.md`](AGENTS.md): "Parameter size is part of entity identity", "Stage/mode identity
granularity", and "Alias-first join with a two-tier miss policy".

## Spelling unification & provenance

One model reaches bestiary under many raw spellings — different provider prefixes, casing,
punctuation, size suffixes, quant tags, and even a missing version segment. Unifying those
`N` spellings onto **one** entity key is not magic string-munging: every spelling arrives at
its key through a **deterministic, auditable** pipeline, and the grouping it produces is
itself queryable. Four mechanisms do the work, in precedence order:

1. **Mechanical canonical decomposition.** Every raw ID is decomposed into the canonical
   tuple `(family, variant, version, #paramsize, {identity-mods})` by the same suffix tables
   and version patterns described above (`parse.go`, `parse/data/variant_suffixes.json`,
   `parse/data/version_patterns.json`). Spellings that differ only in provider prefix, casing,
   or a stripped quant/attribute tag collapse here, for free.
2. **Curated exact-ID pins.** Where the mechanical path can't reach the truth, a curated
   entry overrides it — **size-token pins** (`parse/data/param_size_overrides.json`) and
   **raw-family → family/variant overrides** (`parse/data/family_overrides.json`), each
   JSON entry carrying a `_comment` recording *why* the pin exists, plus exact-ID
   **family/variant/version pins** in the curated `idFamilyOverrides` table in `parse.go`
   (provenance recorded in Go comments). A pin is the top precedence tier: it can never be
   flipped by a mechanical scan or a data refresh.
3. **Pipeline alias files.** The two ingest pipelines each keep a curated alias map that is
   consulted *before* mechanical decomposition — `parse/data/ollama_aliases.json` for the
   Ollama refresh and `parse/data/modelsdev_aliases.json` for the models.dev metadata join.
   A present alias is the sole identity for that spelling (curated > mechanical).
4. **The `Instances` list is the grouping.** Once unified, every raw spelling that resolved to
   a key hangs off that entity as one row in its `Instances` list. That list is the
   **queryable** evidence of the grouping — `bestiary providers '<key>'` (or `show --by-entity`)
   prints exactly which raw IDs, under which providers, folded into the entity.

**Worked example — Llama 4 Scout (13 spellings → 1 entity).** All of these key to
`llama/scout@4#17b-16e{instruct}`:

```
llama-4-scout                                     meta.llama4-scout-17b-instruct-v1:0
llama-4-scout-17b-16e-instruct                    us.meta.llama4-scout-17b-instruct-v1:0
llama-4-scout-17b-16e-instruct-fp8                cerebras-llama-4-scout-17b-16e-instruct
meta/llama-4-scout-17b-16e-instruct               meta-llama/Llama-4-Scout-17B-16E-Instruct
@cf/meta/llama-4-scout-17b-16e-instruct           workers-ai/@cf/meta/llama-4-scout-17b-16e-instruct
```

Three unification steps stack: the provider-prefix / casing / `-fp8` spread collapses
mechanically; the size-token pins in `param_size_overrides.json` fold the size-less
(`llama-4-scout`) and bare-`17b` (`…-17b-instruct`, including the two **dotted Bedrock** forms
`meta.llama4-scout-…v1:0` where the dot is token-internal) spellings up to the full `17b-16e`
shape; and curated **`@4` version pins** in `parse.go`'s exact-ID `idFamilyOverrides` table
join the three version-less spellings (the two Bedrock forms + `cerebras-…`, whose IDs
simply omit the `4`) to the ten that already carry it. Without the pins those three would strand as a separate
`llama/scout#17b-16e{instruct}` entity — provenance-honest, but not what a reader wants.

**Worked example — Grok 4.20 beta (aliases folded into one entity).** The two `-beta-`
spellings

```
grok-4.20-beta-0309-reasoning      grok-4-20-beta-0309-reasoning
```

now key to `grok@4.20{reasoning}`, alongside the non-beta reasoning spellings
(`grok-4.20-0309-reasoning`, `xai/grok-4.20-reasoning`, `grok-4-20-reasoning`, …). A curated
ruling reclassified Grok's `beta` from an identity **variant** into a release-stage
**attribute**: the stage signal is still delivered (a resolved model reports `Stage: beta`),
but `beta` no longer forks the key, so the `-beta-` aliases stop stranding as a separate
`grok/beta@4.20{reasoning}` entity. This is a *curated, family-scoped* reclassification —
`beta` is still frozen as key material for other families where no such ruling exists.

> **Boundary — traceability is derivation-based today, not yet an alias data model.** A
> spelling reaches its entity by *decomposition + curated pins/aliases*, and the `Instances`
> list is the evidence of that grouping. What does **not** yet exist is a first-class alias
> **edge**: an explicit record, per accepted spelling, of *who* asserted the equivalence and
> *on what basis* (claim attribution). Those alias edges are a planned follow-up, not a
> shipped data model — for now the curated `_comment` fields in `parse/data/*` (and the Go
> comments on `parse.go`'s curated override tables) are the human-readable provenance trail.

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

## VRAM, quantization & data-source provenance

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
    "Modifier": [
      "instruct"
    ]
  },
  ...                          # (excerpt — the "Ref" object; Instances/Sources/etc. elided)
}

$ bestiary show --by-entity 'llama@3.3#8b{instruct}'
{
  "Ref": {
    "Family": "llama",
    "Variant": "",
    "Version": "3.3",
    "ParamSize": "8b",
    "Modifier": [
      "instruct"
    ]
  },
  ...                          # (excerpt)
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
      QUANT              WEIGHTS            VRAM         TYP(4K)        CTX  PARTIAL
      q4_k_m         43033509888     85983182848     44375687168     131072    false
      q8_0           75176521728    118126194688     76518699008     131072    false
      f16           141166166016    184115838976    142508343296     131072    false
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

- **`TYP(4K)`** is that recomputation made visible: the same VRAM estimate baked at a
  *typical* 4096-token working context instead of the full window, so you can size a
  realistic run at a glance (the 70B q4_k_m needs ≈44.4 GB at 4K versus ≈86 GB at its 131K
  max). It renders an em dash (`—`) when the model's maximum context is below 4096 — or
  unknown — since a figure at a context the model cannot serve would be meaningless, and on a
  `PARTIAL` row it stays weights-only (no phantom KV delta), exactly like `VRAM`.

When the architectural facts (layers / KV-heads / head-dim) are **absent**, the KV term is
excluded and the row is flagged `PARTIAL true` — `VRAM` then equals `WEIGHTS`, an honest
weights-only **lower bound**, never a silent under-estimate:

```sh
$ bestiary providers --output table 'llama@3.2#3b{instruct}'
Entity: llama@3.2#3b{instruct}
Instances (1):
  ...
      QUANT              WEIGHTS            VRAM         TYP(4K)        CTX  PARTIAL
      q4_k_m          2019139072      2019139072      2019139072     131072     true
      q8_0            3419799040      3419799040      3419799040     131072     true
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
bestiary: ParseQuantization: unrecognised quantization "nope"; why: the input does not match any known quantization name (case-insensitive); where: ParseQuantization; valid examples: f16, bf16, f32, q4_0, q8_0, q4_k_m, q5_k_m, iq4_nl, other; how to fix: pass one of the canonical wire names listed above
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

## Model metadata & ingest provenance

v0.2.5 harmonizes bestiary with the full models.dev catalog and turns provenance
into a first-class, queryable history. It ingests all three models.dev JSON
artifacts (api.json, models.json, catalog.json), bakes a **provider-agnostic
metadata dimension** (descriptions, licenses, benchmark claims, links) that
attaches at the *entity* level, adds an `entities` census and a `--status`
filter, and makes the ingest log an **append-only history** with a round-trip
export. The static catalog is refreshed from a vendored July snapshot: **162
providers, 5,654 models, 810 entities** (up from the April baseline of ~138
providers / ~4,300 models).

### Enumerating every entity (`entities`)

`bestiary entities` walks the whole registry and prints one row per canonical
entity — including **metadata-only standalones** that no provider currently
serves but that models.dev still publishes facts about, which are otherwise only
reachable by their exact key:

```sh
$ bestiary entities --output table
Entities (810):
  ENTITY KEY                                       PROVIDERS METADATA BENCHMARKS
  ...
  claude/opus@4.7                                         32      yes         19
  ...
  glm/air@4.5                                             20      yes          3
  ...
  ornith@1.0#35b                                           0      yes          7
  ...
  whisper/large@3{turbo}                                   2      yes          0
  ...
```

`PROVIDERS` counts the serving instances, `METADATA` marks whether the entity
carries a models.dev metadata row, and `BENCHMARKS` counts its lab-reported
benchmark claims. The `ornith@1.0#35b` row is a **metadata-only standalone**:
`PROVIDERS 0` (no instance serves it) yet `METADATA yes` with 7 benchmarks — the
join synthesizes it as its own `#size`-keyed entity rather than dropping the
facts. When the SQLite cache has never been synced, entity-view commands print a
single notice to **stderr** and read the embedded catalog:

```
bestiary: using embedded catalog (run 'bestiary sync' to refresh metadata)
```

### Entity metadata: description, license & benchmarks

`show --by-entity --output table` now renders the provider-agnostic metadata
block — description, license, and the benchmark-claims table — under the entity
header (Providers/Hosts/Instances elided here):

```sh
$ bestiary show glm-4.6 --by-entity --output table
Entity: glm@4.6
  Family:        glm
  Variant:       -
  Version:       4.6
  Identity-mods: -
Providers (24): 302ai, abacus, deepinfra, …
...
Capabilities: reasoning, tool-call, attachment, temperature, structured-output, interleaved, open-weights
Description: Late GLM-4 workhorse for coding agents, reasoning, and structured tasks
License:     -
Benchmarks (4):
  NAME                            SCORE METRIC         HARNESS            DATE         SOURCE
  Artificial Analysis Cod…         29.5 index          -                  2026-05-22   https://openrouter.ai/z-ai/glm-4.6/benchmarks
  SciCode                          38.4 percent correct -                  2026-05-22   https://openrouter.ai/z-ai/glm-4.6/benchmarks
  Terminal-Bench Hard                25 success rate   -                  2026-05-22   https://openrouter.ai/z-ai/glm-4.6/benchmarks
  SWE-Bench Pro                    9.67 resolve rate   -                  -            https://labs.scale.com/leaderboard/swe_bench_pro_public
  note: benchmark names truncated (use --output json for full names)
Lineage (0):
Instances (28):
  ...
```

The benchmark table keeps every claim in a **separate column** (name, score,
metric, harness, date, source) — never concatenated — so a future canonical
benchmark dimension can join on the parts. Two readability rules apply, and both
announce themselves:

- **Names truncate at 24 columns.** `Artificial Analysis Coding Index` renders as
  `Artificial Analysis Cod…`; the `note: benchmark names truncated` line points at
  `--output json` for the full names.
- **The table caps at the top 5 rows.** glm-4.6 has 4, so all show. An entity with
  more prints a `… and N more` footer — e.g. `claude/opus@4.7` (19 benchmarks)
  renders five rows then `… and 14 more (use --output json)`.

The full, untruncated set — every benchmark row, both score forms, all links — is
always available via `--output json`.

### Filtering by release status (`list --status`)

`list --status` keeps only the models carrying a given models.dev release status
(`none`, `alpha`, `beta`, `deprecated`):

```sh
$ bestiary list --status deprecated --output table
ID                                        Provider      Family              Context  MaxOutput  Reason  Tools   CostIn/MTok
----------------------------------------  ------------  ----------------  ---------  ---------  ------  -----  ------------
claude-opus-4-1                           anthropic     claude               200000      32000     yes    yes        $15.00
mistralai/devstral-2512                   anyapi        devstral             262144     262144      no    yes             —
MiniMaxAI/MiniMax-M2.5                    baseten       minimax              204000     204000     yes    yes         $0.30
...                                                                          # 119 deprecated models
```

An unrecognized status is rejected with an actionable error rather than silently
matching nothing:

```sh
$ bestiary list --status bogus
bestiary: ParseModelStatus: unrecognized status "bogus"; why: the input does not match any known model status (case-insensitive); where: ParseModelStatus; valid values: none, alpha, beta, deprecated; how to fix: pass one of the valid values listed above
```

### Provenance history & export (`sources --history` / `--export`)

The ingest log is now an **append-only history** — a source carries one row per
distinct ingest instant, and the *current* ingest is simply the row with the
latest timestamp. A fresh, never-synced cache serves the embedded catalog (the
stderr notice above); `bestiary sync` then fetches api.json + models.json live,
persists the models, metadata, and attestations, and **appends** one ingest row
stamped with the real UTC wall-clock time. A materially stale vendored snapshot
also warns when more than 50 live model IDs are missing from the embedded
catalog (shape shown — `sync` requires the network, and `<N>` is the live
missing-model count):

```sh
$ bestiary sync
bestiary: warning: <N> live model IDs are absent from the embedded catalog; the vendored models.dev snapshot is stale — refresh it and regenerate (see AGENTS.md "models.dev snapshot refresh")
...
```

`sources --history` prints the whole log, ascending by ingest time, offline from
the committed seed (no entity argument):

```sh
$ bestiary sources --history --output table
Ingest history (3):
  SOURCE       URI                                INGESTED                 PARSER
  models.dev   https://models.dev/api.json        2026-06-09T00:00:00Z          3
  models.dev   https://models.dev/api.json        2026-07-13T02:11:52Z          3
  ollama       https://registry.ollama.ai         2026-06-09T00:00:00Z          3
```

`sources --export` emits the store's ingest provenance as a `datasources.json`
**v3 document** — the **union** of the store's synced history and the curated
seed, so the curated `ollama` row rides along even when only models.dev was
synced. The output is round-trippable and promotable straight back into
`parse/data/datasources.json`:

```sh
$ bestiary sources --export
{
  "schema_version": 3,
  "sources": [
    {
      "id": "models.dev",
      "uri": "https://models.dev/api.json",
      "canonical_name": "models.dev"
    },
    {
      "id": "ollama",
      "uri": "https://registry.ollama.ai",
      "canonical_name": "Ollama Registry"
    }
  ],
  "ingested": [
    {
      "source_id": "models.dev",
      "ingested_at": "2026-06-09T00:00:00Z",
      "parser_schema": 3
    },
    {
      "source_id": "models.dev",
      "ingested_at": "2026-07-13T02:11:52Z",
      "parser_schema": 3
    },
    {
      "source_id": "ollama",
      "ingested_at": "2026-06-09T00:00:00Z",
      "parser_schema": 3
    }
  ]
}
```

### Vendored codegen snapshot

Codegen no longer fetches the catalog at build time. `go generate ./...` reads a
**committed** snapshot — `parse/data/modelsdev/catalog.json` plus its
`SNAPSHOT.json` provenance sidecar — so it is fully offline and deterministic, and
a missing or corrupt snapshot is a **loud** error (codegen never bakes an empty
catalog). Refreshing that snapshot from a newer upstream deploy is a deliberate,
occasional manual step — see the **"models.dev snapshot refresh"** workflow in
[`AGENTS.md`](AGENTS.md).

## CLI

```
bestiary <list|show|providers|entities|series|sources|sync> [flags]
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

**entities** — enumerate every canonical entity in the registry (offline; no argument).
Each row carries its provider count, whether it has models.dev metadata, and its
benchmark-claim count; metadata-only standalones (no serving provider) are included so
they are discoverable.

```sh
bestiary entities --output table   # summary table (ENTITY KEY / PROVIDERS / METADATA / BENCHMARKS)
bestiary entities                  # full Entity objects, JSON
```

**series** — browse the computed **Series/Release** hierarchy above entity keys (offline;
static registry only — it never reads the SQLite cache, and **rejects** `--db-path` with an
actionable error rather than accepting a flag it cannot honour). With no argument it lists every versioned
line — `Series{Family, Generation}` such as `llama-4` or `gemini-3.0` — with its release and
entity counts. With a selector it details that line: each **Release** (a named member such as
`scout`, `maverick`, `flash`, plus the un-named bare line) and the canonical entity keys under
it.

The selector is a **specificity ladder** — ask for as much or as little as you know:

| Selector | Returns |
|---|---|
| `bestiary series claude` | every claude line, all generations |
| `bestiary series claude-4` | every claude **4.x** line (the major union) |
| `bestiary series claude --version 4` | identical to the row above |
| `bestiary series claude-4.8` | the one `claude-4.8` line |

The **canonical entity grammar** is accepted too, mapped to its series-level meaning. Note the
`@` here is the entity **version**, exactly as in an entity key (`claude/opus@4.5`) — not the
`@`-date form the `show` resolver accepts; series live above entity keys, so they inherit the
key grammar.

| Selector | Returns |
|---|---|
| `bestiary series claude@4` | the major-4 union (identical to `claude-4`) |
| `bestiary series claude@4.5` | the one `claude-4.5` line |
| `bestiary series claude/opus` | the **opus release across every claude generation** |
| `bestiary series claude/opus@4` | the opus release within the 4.x lines |
| `bestiary series anthropic/claude@4` | the 4.x lines, narrowed to anthropic-served entities |

A variant segment is a **release-level cut**: it selects the lines that actually carry that
release and shows only that release in each, so a line without an opus release drops out rather
than appearing with the rest of its releases intact. A leading `<provider>/` is peeled only when
it names a *known* provider (so `claude/opus` still reads as family/variant), and it feeds the
ordinary `--provider` machinery rather than a second filter — an explicit `--provider` that
disagrees with the prefix is an actionable error, as is a `--version` that disagrees with the
selector's `@version`.

`--input-format` pins the grammar for scripting that must not depend on inference:

| Value | Behaviour |
|---|---|
| `infer` (default) | ladder and canonical readings are tried and **unioned**; a raw model ID is the **final** fallback, used only when both find nothing |
| `canonical` | the selector must be `[provider/]family[/variant][@version]` — **no fallback**; a raw ID fails loudly and is told which format would read it |
| `models.dev` | the selector is a raw catalog ID, resolved through the ordinary lookup to its entity's line (`claude-sonnet-4-5-20250929` → `claude-4.5`) |

The major rung is a **union, not a re-grouping**: it returns several Series in the same
multi-line output shape the family rung already produces, and the hierarchy itself is
untouched — `claude-4.0` and `claude-4.5` remain distinct lines that a narrower selector still
addresses individually. Membership is a **strict string rule**: a generation belongs to version
`4` iff it *is* `4` or begins `4.`. Nothing is numerically normalized, so `4` never swallows
`42`, `1` never reaches the mis-parsed `ling@1t` or the leading-zero `gemini@001`. Upstream
`p`-for-dot spellings *are* repaired — at **parse** time, not in the selector: `glm-5p1`/`glm-5p2`
decode to the real `glm@5.1`/`glm@5.2`, so `series glm-5` returns them as ordinary dotted union
members. That repair lives in `parse/`, where the raw IDs are, which is also why the spellings it
does *not* yet reach — a compound-family case like `k2p7`, which decomposes to a version-less
`kimi-k2` rather than `kimi/k@2.7` — stay out of any union until `parse/` fixes them too, never a
selector that would have to guess. Sub-1.0 generations need no special case: `mistral-0` unions `mistral-0.1` and
`mistral-0.3` like any other version. Where a family spells both a bare `N` and dotted
siblings (`claude-3`, `glm-4`, `gpt-5`, …), the union **includes the bare line**. `--version`
is exactly equivalent to appending `-<value>` to the positional, and is rejected with an
actionable error when given without a family — it selects *within* one. Matching is
case-folded, and the filters below apply *after* the selection.

Two refinements keep one line from splitting on a spelling accident: a bare generation `N`
folds into `N.0` when the same family also spells `N.0` (so `gemini@3` and `gemini@3.0` share
`gemini-3.0`, while `llama-4` — with no dotted sibling — keeps its bare generation), and the
curated `parse/data/series.json` re-homes the few families whose line cannot be derived
(`gemma4` → `gemma-4`). **Neither affects entity keys**: the hierarchy is computed *above*
them and never feeds back into identity.

`--provider`, `--quant` and `--status` narrow the **entity list inside each release**. Each is
a per-entity predicate satisfied by the entity's *instances*: `--provider` keeps entities with
an instance served by that provider, `--quant` those with an instance carrying a matching
`QuantVRAM` row, `--status` those with an instance whose model has that release status.
Combined filters must be satisfied by **one instance at once** — `--provider=X --quant=Y`
means "X serves it at Y", not "X serves it *and* somebody serves it at Y". The drops
**cascade**: an emptied release is omitted, an emptied line is omitted from both views, and
the listing's counts are post-filter, so `series --provider X` lists exactly the lines and
counts `series <line> --provider X` will then render. An unknown `--quant` or `--status` value
is rejected with an actionable error rather than silently matching nothing, and a selector
that names a real line the filters empty is its own actionable error (not "not found" — the
line exists; the filter is what matched nothing).

```sh
bestiary series --output table            # every line: SERIES / FAMILY / GENERATION / RELEASES / ENTITIES
bestiary series llama-4 --output table    # that line's releases (bare, maverick, scout) + entity keys
bestiary series gemma                     # every gemma generation, JSON
bestiary series claude-4                  # every claude 4.x line: 4.0, 4.1, 4.5, 4.6, 4.7, 4.8
bestiary series claude --version 4        # identical to the line above
bestiary series claude-4.8                # just that line
bestiary series claude/opus               # the opus release across every claude generation
bestiary series anthropic/claude@4        # the 4.x lines, anthropic-served entities only
bestiary series claude-sonnet-4-5-20250929 --input-format models.dev   # a raw id -> its line
bestiary series --provider cohere         # only lines cohere serves, with post-filter counts
bestiary series --quant q4_k_m            # only lines with a q4_k_m-quantized instance
bestiary series --status beta             # only lines with a beta-status instance
bestiary series llama-3.3 --quant q4_k_m  # detail view, entity list narrowed the same way
```

**sources** — resolve an entity key and print its data-source provenance (one row per
attesting source: URI, ingest date, parser-schema version). The `--history` and `--export`
views take no entity argument and read the catalog-wide ingest log. Offline.

```sh
bestiary sources --output table 'llama@3.3#70b{instruct}'   # dual-attested: models.dev + ollama
bestiary sources 'claude/opus@4.5'                          # JSON; models.dev-only
bestiary sources --history --output table                   # full append-only ingest log (ascending)
bestiary sources --export                                   # datasources.json v3 to stdout
bestiary sources --export sources.json                      # ...or to a file
```

**list** — query models from the static registry + local cache (offline).

```sh
bestiary list                                       # all models, JSON
bestiary list --provider anthropic --output table   # Anthropic models, table
bestiary list --status deprecated --output table    # only deprecated models
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
| `--status` | `list` | (all) | Keep only models with the given release status: `none`, `alpha`, `beta`, `deprecated`. An unrecognized value is rejected with an actionable error. |
| `--provider` | `list`, `sync` | (all) | Filter by provider slug (e.g. `anthropic`, `google`, `openai`). |
| `--history` | `sources` | `false` | Print the full append-only ingest log per source (ascending). Takes no entity argument. |
| `--export` | `sources` | `false` | Export the ingest provenance as a `datasources.json` v3 document (optional positional path; stdout otherwise). |
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
