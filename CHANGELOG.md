# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for its **Go module tags** (`vX.Y.Z`).

> **Two version axes.** The module tag (`vX.Y.Z`, what `go get` resolves) is
> distinct from `BestiarySchemaVersion` (the version of the public JSON output
> schema in `bestiary.schema.json`). Each release below notes both. The schema
> version only changes when the public output types change; several module
> releases share one schema version.

## [Unreleased]

**Schema:** `0.4.0` → `0.5.0` (additive). SQLite store schema `6` → `7`.

### Added
- **Nomen naming layer**: one queryable record for every way anything names a model
  entity — `Nomen{Value, Scheme, Status, ResolvesTo, SourceURL, Source}` with the
  `NomenScheme` classifier (canonical / provider-id / huggingface / purl / alias)
  and ISO 1087 `AcceptabilityRating` statuses. Minted by one shared production
  function over the entity index plus the curated `parse/data/nomen_claims.json`
  (3,768 nomina: 975 canonical Preferred, 2,792 provider-ID Admitted, 1 curated
  alias claim). `Entity.Nomina()` and `NomenLookup()` (homonym-aware) are the
  read APIs; claim attribution keeps *who asserts* (`SourceURL`) distinct from
  *which ingest we read* (`Source` — curated claims attribute the new `curated`
  data source, never models.dev).
- **Region axis**: `Region` closed int enum (`region.go`) capturing the
  geographic/jurisdictional boundary an instance's serving is scoped to (Amazon
  Bedrock cross-region inference profiles: `us.`/`eu.`/`au.`/`jp.`/`global.`).
  Members follow ISO 3166-1 alpha-2 where applicable; `RegionNone` renders
  `"unspecified"`; unknown tokens land in `RegionOther` with the raw token
  preserved. Public surface: `ModelInfo.Region`/`RegionRaw`, per-instance
  `ProviderInstance.Region`, and the `Entity.Regions` jurisdiction aggregate
  (e.g. Bedrock-served entities report `[unspecified, us, eu, global, au, jp]`),
  rendered in table/YAML/JSON. Not part of entity identity.
- **Series/Release hierarchy** (`taxonomy.go`): a computed, read-only grouping above
  entity keys. `Series{Family, Generation}` is the versioned line (`llama-4`,
  `gemini-3.0`) and `Release{Series, Name}` is a named member of it (`scout`,
  `maverick`, `flash`) — version above variant. Both are COMPUTED from key components
  already on `EntityRef`, never stored and never fed back into keys: the hierarchy can
  be re-shaped without re-keying anything. Read APIs: `SeriesAll()` (422 lines, sorted
  by family then generation), `ReleasesOf(Series)`, `EntitiesOf(Release)`, plus
  `SeriesOf(EntityRef)`/`ReleaseOf(EntityRef)` for the entity → line direction; all
  orders are explicit sorts and all results are defensive copies. Two refinements keep
  one line from splitting on a spelling accident: a bare-integer generation `N` folds
  into `N.0` when the same family also spells `N.0` (so `gemini@3` and `gemini@3.0`
  share `gemini-3.0`, while `llama-4` — which has no dotted sibling — keeps its bare
  generation), and the curated `parse/data/series.json` re-homes the few families whose
  line the computation cannot derive (`gemma4` and `gemma-4-31b-larkspur` onto
  `gemma-4`, `gemini-exp` onto `gemini`). The curated file is graceful-degrade: missing
  or malformed, the taxonomy falls back to pure computation. **Entity keys are
  untouched by both.**
- **`bestiary series` subcommand** (read-only, offline, static registry — it never reads
  the SQLite cache and *rejects* `--db-path` with an actionable error rather than
  accepting a flag it cannot honour): bare, it lists every line with its release and entity counts; with a
  selector it details that line's releases and their entity keys. The selector reads as
  a line rendering (`llama-4`) or a family name (`gemma`, every generation of it) —
  the union of both, case-folded. Table and JSON output; no schema change, since this
  renders Go relations rather than a new public JSON document type.
- **Store v7**: `nomina` table (PK `(value, scheme, entity_key)`, FK →
  `data_sources` — enforcement regression-tested) + `region` column, with
  presence-guarded self-heal from v6 on both paths and zero data loss.
- **IRI minting** (`iri.go`): `EntityRef.IRI(base)` and `ModelRef.IRI(base)` render an
  identity as a dereferenceable RFC 3987 name — the caller-supplied namespace base with
  the percent-encoded canonical string appended as exactly ONE path segment. The base is
  a **parameter by design and is never defaulted**: bestiary owns no public namespace, so
  the consumer supplies its own (`https://w3id.org/…`, an internal https namespace, a
  `urn:` prefix); it is used verbatim and the only rejected value is the empty string
  (which would yield a relative reference, not an IRI — that returns `""`). Escaping is
  `url.PathEscape` (the path-segment escaper — `url.QueryEscape` would render a space as
  `+`, which does not decode back in a path position) plus an explicit `@` → `%40` pass,
  because `@` is a legal `pchar` that `PathEscape` leaves raw while the grammar uses it as
  the version delimiter. Every grammar delimiter is therefore encoded — `#` most
  importantly, since raw it would start a URI *fragment* and silently truncate the
  identifier at the size segment. Additive Go API only: no schema, store or output change.
  `llama/scout@4#17b-16e{instruct}` →
  `https://w3id.org/bestiary/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D`, and the
  round trip back through `url.PathUnescape` is fenced over a delimiter torture set and
  over every entity and model ref in the committed registry.
- **Curated HuggingFace nomina** (`parse/data/nomen_claims.json`): four
  `NomenSchemeHuggingFace` seed claims mapping flagship open-weight entities to the Hub
  org/repo their weights live at — `meta-llama/Llama-4-Scout-17B-16E-Instruct` →
  `llama/scout@4#17b-16e{instruct}` (an entity served by 13 providers), plus
  `meta-llama/Llama-3.3-70B-Instruct`, `Qwen/Qwen3-Coder-480B-A35B-Instruct` and
  `deepseek-ai/DeepSeek-V3.2`. Each repo path is verified verbatim against the committed
  catalog's raw model IDs (fenced), each is `Admitted` with `Source = curated` and a
  claimant `SourceURL` pointing at the Hub page, and all surface through
  `Entity.Nomina()` / `NomenLookup()`. This is the durable, **entity-level** external
  identifier: the Hub name holds regardless of which provider is serving the entity.
  Minted census moves to 3,772 (975 canonical, 2,792 provider-ID, 4 huggingface, 1 alias).

### Changed
- **Designation layer activated**: `ModelRef.Designations()` now rates the
  canonical form `Preferred` (raw/HuggingFace/PURL stay `Admitted`) — the
  prerequisite for truthful `skos:prefLabel`/`altLabel` export (GH#24 ask 3).
- **Constants surface is now entity-level** (BREAKING). The ~5,650
  provider-flavored `Model__<Provider>__…` constants are replaced by one
  provider-agnostic `Entity__*` constant per model entity (975), each valued by
  its canonical entity key (e.g.
  `Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct = "llama/scout@4#17b-16e{instruct}"`).
  Names follow a word-sentinel grammar — `Entity__<Family>[__<Variant>][__Version_<v>][__Size_<s>][__<Mod>…]`,
  plain-Pascal segments with a separator-preserving sanitizer (every
  non-alphanumeric character → `_`, never camel-folded) — kept injective by a
  loud codegen guard that fails the bake on any duplicate identifier. Provider
  information moves to the API: `ProvidersOf(ref EntityRef) []Provider` and
  `ProvidersOfModel(id ModelID) []Provider` return an entity's serving providers
  (sorted, de-duplicated). `ModelIDs()` → `EntityKeys()`.

- **`llama-4 maverick` is a named release** (BREAKING for two entity keys). `maverick`
  joins `scout` as a curated `llama` variant member, so the 23 maverick instance rows
  key under `/maverick` as siblings of scout instead of collapsing into the
  variant-less `llama@4` line. This is the epoch's ONE deliberate entity re-key; every
  other key is byte-identical and the entity census is unchanged at 975 (a move, not a
  mint). Migration:

  | Old key (gone) | New key |
  |---|---|
  | `llama@4#17b-128e` | `llama/maverick@4#17b-128e` |
  | `llama@4#17b-128e{instruct}` | `llama/maverick@4#17b-128e{instruct}` |

  | Old constant (gone) | New constant |
  |---|---|
  | `Entity__Llama__Version_4__Size_17b_128e` | `Entity__Llama__Maverick__Version_4__Size_17b_128e` |
  | `Entity__Llama__Version_4__Size_17b_128e__Instruct` | `Entity__Llama__Maverick__Version_4__Size_17b_128e__Instruct` |

  The four spellings whose form puts the maverick token out of the mechanical member
  scan's reach — the dotted Bedrock forms (`meta.llama4-maverick-…`,
  `us.meta.llama4-maverick-…`) and the aggregator provider-prefixed forms
  (`cerebras-…`, `groq-…`) — carry an exact-ID variant pin so all 23 rows stay in one
  artifact rather than fragmenting across two keys.

### Removed
- **`Model__*` constants + `ModelIDs()`** (BREAKING). Migration:

  | Old (removed) | New |
  |---|---|
  | `Model__<Provider>__…` (an entity served by one provider) | `Entity__<…>` (the entity) + `ProvidersOf(ref)` to list its providers |
  | `bestiary.ModelIDs()` (all provider-flavored IDs) | `bestiary.EntityKeys()` (all canonical entity keys) + `EntityByKey(key)` — enumerate-then-lookup works again: `for _, key := range EntityKeys() { e, _ := EntityByKey(key); … }` |
  | constant → a specific provider's instance | `LookupModelByProvider(provider, id)` (or `LookupModel(id)`) for instance-level fields |
  | constant → the entity's identity | `Entity__<…>` constant, then `EntityByKey(value)` — or `EntityByTuple(family, variant, version, paramSize, mods…)` from a decomposed tuple |

  **Caveat — entity keys are NOT `ModelID`s.** `EntityKeys()`/`Entity__*` values are
  canonical ENTITY keys (grammar `family[/variant][@version][#size]{mods}`, where `@` is
  the identity VERSION), a different grammar from raw catalog `ModelID`s and from
  `Resolve`'s `<provider>/<family>/<variant>@<date>` form (where `@` is a DATE). Do NOT
  pass an entity key to `LookupModel` / `LookupModelByProvider` / `Resolve` — they take
  provider-ID grammar and will silently miss. Use `EntityByKey(key)` (or `EntityByTuple`)
  for entity-key lookups; use `LookupModel(id)` for raw-ID instance lookups.

### Fixed
- **PURL render restricted to HuggingFace** (behavior change; the previous output was
  spec-invalid). `ModelRef.Format(SchemePURL)` used to interpolate the *serving provider*
  as the purl namespace — `pkg:huggingface/anthropic/claude-opus-4-1` for a model that has
  no HuggingFace repo at all, and `pkg:huggingface/huggingface/meta-llama/Llama-3.3-70B-Instruct`
  (double-`huggingface`) for HuggingFace's own rows. A purl is a foreign key into someone
  else's registry, so it is now minted only where the artifact's registry home is known:
  the ~51 huggingface-provider instances, whose raw ID already *is* the Hub org/repo path,
  render `pkg:huggingface/<org>/<repo>`; every other provider renders `""`, and
  `Designations()` **drops** the purl entry rather than emitting an empty-valued
  designation (3 designations for a non-HF ref, 4 for an HF one — canonical stays
  `Preferred`). The **input** side is deliberately unchanged (Postel): `Resolve` and
  `--scheme purl` still accept the legacy `pkg:huggingface/<provider>/<raw-id>` spelling,
  since downstream SBOMs may hold strings bestiary itself emitted. The provider-independent
  identifier story is the entity-level Hub nomen above; this render is the stopgap.
- **Empty-raw claude version recovery**: `claude-3.5-haiku` / `claude-3-5-haiku`
  empty-raw forms now decompose to `(claude, haiku, 3.5)` instead of dropping the
  version.
- **MM-YYYY month leak**: a trailing `MM-YYYY` date no longer leaks its month into
  `Version` (`command-r7b-12-2024` → version `""`, date `2024-12`; also
  `command-r7b-arabic-02-2025`, `command-a-plus-05-2026`).
- **Command R7B identity**: `command-r7b*` ids keep the variant whole and carry the
  size — entity key `command/r7b#7b` ("Command R7B" is Cohere's own model name;
  `command-r` and `command-r-plus` unchanged).
- **Bedrock dotted-namespace convergence**: `us./eu./au./jp./global.` vendor-dotted
  Bedrock ids now recover the same version as their plain siblings and share one
  entity (previously they split into version-less sibling entities).

### Testing
- IRI round-trip fences: a delimiter torture set (`/`, `@`, `#`, `{`, `}`, `,`, the
  ref-level `[attributes]` brackets) plus every entity and every model ref in the
  committed registry, each asserted to decode back byte-identically; the escaping
  contract itself and the empty-base/verbatim-base rules are pinned separately.
- PURL fences: a real catalog HF row renders the repo-path purl, every huggingface-provider
  row is checked against double-namespacing, a non-HF ref renders `""`, no designation is
  ever emitted empty-valued corpus-wide, and the lenient legacy purl *input* still resolves.
- Parse-residual capture corpora: 59 → 65 corpora (azure serving-host,
  meta-llama no-slash census, namespace suffix transparency, text-embedding
  sole-variant, grok documented-residual, region capture), all under the
  three-guard discipline.

## [0.2.6] — 2026-07-16

**Schema:** `0.3.0` → `0.4.0` (additive). SQLite store schema **unchanged** at `6`.

The **full-bulk `#size` re-key** epoch (GH#9): one shared enrichment now sizes every
model whose ID carries a parameter-size token, so a `#size` segment is no longer
confined to the handful of curated `quant_vram.json` entries.

### Added

- **Parameter-shape fields** on `ModelInfo` (`TotalParams`, `ActiveParams`,
  `PerExpertParams`, `ExpertCount`), decomposed from `ParamSize` — derived
  presentation facts, never entity-key material. Grouped along shape joints and
  never cross-computed (an NxM MoE token like `8x22b` sets `ExpertCount` +
  `PerExpertParams` but no total). Each field is an **in-domain NULLable integer**
  under the new `ParamShapeNull` (`-1`) sentinel contract: `-1` means "not populated
  by parser or curation" (the shape carries no such fact, or the size is unknown), a
  positive value is an attested count, and a genuine `0` is reachable **only** for
  `ExpertCount` (a dense shape attests zero experts). An unsized model bakes all four
  as `-1`. The schema pins `minimum: -1` on each. Every row now emits all four fields
  explicitly (a `0` is meaningful and must be distinguished from the NULL sentinel).
- **`EnrichedParamSize(id)`** — the single param-size precedence authority
  (pin > mechanical > `ParamSizeFor`), shared by the two runtime enrichment joints
  (`toModelInfo`, `scanModelInfo`) and the codegen bake, plus `ValidateParamSizePins`
  (a codegen-time guard that rejects a non-canonical curated pin token).
- **Release-stage axis** (`stage.go`, [#13]): a closed int enum `ReleaseStage`
  (`StageNone`/`StageStable`/`StagePreview`/`StageBeta`/`StageAlpha`/
  `StageExperimental`/`StageLatest`/`StageOriginal`/`StageOther`) with
  `ParseReleaseStage` (CLI/config parse), `DetectReleaseStage` (ID-token
  detection, known-members-only), and `DetectStageFromID` (the shared ID scanner).
  `ModelInfo` gains `Stage`/`StageRaw`, derived from the ID at the SAME enrichment
  joints as `ParamSize` (so a live-sync row and its baked static row always agree).
  Stage is DELIBERATELY separate from `ModelStatus`: `Status` is upstream-declared
  lifecycle, `Stage` is ID-derived — `show`'s instance table renders them under
  distinct `STATUS` / `STAGE` columns. `StageOther` is a RESERVED bucket for a
  future non-ID feeder (Quantization precedent); the ID path never produces it.
  `StageLatest`/`StageOriginal` name a moving target, not a fixed artifact property.
  Schema `0.4.0` gains additive `Stage`/`StageRaw` properties and a `ReleaseStage`
  `$def` (neither required).
- **`TYP(4K)` quant-table column** (`cmd/bestiary`): the per-quantization VRAM
  sub-rows gain one typical-context column — `(QuantVRAM).EstimateVRAM(4096)`
  recomputed from the row's stored arch-facts — alongside the max-context `VRAM`
  figure, so a realistic-run cost is readable at a glance. Renders an em dash
  (`—`) when the model's maximum context is below 4096 or unknown (a figure at a
  context the model cannot serve would be meaningless), and stays weights-only on
  a `PARTIAL` row (no phantom KV delta). A companion `TYP(8K)` column was
  considered and dropped at acceptance — the ruling is a single 4K column (the
  8K delta added noise, not signal).
- **New `testcase` / `testcase/assert` packages** — a pure-data JSON case-corpus
  harness (stdlib-only; classification + provenance + mutation metadata with
  non-vacuity validation) — plus `TESTING.md` documenting the corpus standard
  the parse/entity/quant/VRAM suites migrated onto.

### Changed — MIGRATION NOTE (entity keys)

- **Full-bulk `#size` re-key.** Entity keys now carry a `#size` segment for every
  model whose ID yields a size token (mechanical `ExtractParamSizeToken`), or that
  matches a curated pin. This re-keys roughly a third of catalog entities
  (`llama-3.1-8b-instruct` → `llama@3.1#8b{instruct}`; `qwen3-30b-a3b` →
  `qwen@3#30b-a3b`; `mixtral-8x22b` → `…#8x22b`). Callers that pinned pre-v0.2.6
  unsized keys (e.g. `dracarys` → `dracarys#72b`, `mythomax` → `mythomax#13b`) must
  update to the sized key. Curated `llama-4` scout/maverick IDs pin to their full
  expert shape (`#17b-16e` / `#17b-128e`) so every spelling of one artifact keys to
  ONE entity, and a suppress-pin keeps a context-tier token out of the key
  (`qwen3-coder-next-fp8-1m` stays unsized — `1m` is a context marker, not params).
- **No store migration.** The size is a pure function of the ID re-derived at both
  read joints, so the SQLite store stays at schema `6` (no `param_size` column).
- Live-sync rows are now sized identically to the baked static rows for the same ID,
  so the most-recent-wins merge can never de-size an `(ID, Provider)`.
- **What stays stable:** entity keys for models whose ID carries no size token and no
  curated pin are byte-identical to v0.2.5 (test-pinned by the successor invariant's
  enrichment-consistency sweep). Curated lineage needs zero churn: lineage lookups
  resolve exact-key-first with a size-stripped fallback, so an unsized curated edge is
  inherited by every newly sized sibling without re-keying `lineage.json`.
- **Stage-token vocabulary migration (no entity-key change).** `preview`, `latest`,
  and `original` left the modifier-class attribute set — they now populate the
  `Stage` axis instead of rendering as `[preview]`/`[latest]` attributes. They are
  routed out of both render segments AND the entity key BEFORE the modifier
  identity fail-safe, so the key is unchanged (they were attribute-class =
  key-excluded before, stage-routed = key-excluded after). The tokens STAY in the
  `Modifier` data field (so constant names and the `[attr]` resolve filter are
  byte-stable). `beta` is detect-without-strip: `Stage=StageBeta` is set wherever a
  standalone `beta` token appears, independent of the key. For the
  **grok-4.20 line**, curated exact-ID overrides now **unify** the beta-alias spellings
  onto their non-beta entity — `grok-4.20-beta-0309-reasoning` and the `…-reasoning-beta`
  / dashed / multi-agent variants key `grok@4.20{…}`, the same entity as the official
  `grok-4.20-0309-reasoning`, while still carrying `Stage=StageBeta`. The **general beta
  freeze stays for non-grok names** (e.g. `interfaze-beta` keeps `beta` in its key);
  a wholesale beta re-key is deferred ([#13]).
- **Version-less `llama-4` `@4` unification.** The `llama-4` scout/maverick spellings
  that omit the `-4-` version token — the AWS Bedrock dotted forms
  (`meta.llama4-scout-17b-instruct-v1:0`, `us.meta.llama4-…`) and the aggregator
  provider-prefixed forms (`cerebras-…`, `groq-…`) — now carry a curated `@4` version
  pin (exact-ID override) so they merge into the existing `@4` entity their canonical
  siblings key (`llama/scout@4#17b-16e{instruct}`, `llama@4#17b-128e{instruct}`).
  This removes the pre-existing version-presence split. (Note: `scout` is a curated
  `llama` variant member so it keys under `/scout`, but `maverick` is not, so the
  official maverick entity is `llama@4` — the pins set variant `""` to merge, not mint
  a new entity.)

### Changed — BREAKING (Go API: exported constant renames)

- **Meaningful collision-group constant names** (`models_constants_gen.go`, r66e).
  Same-base-name model constants that previously fell back to opaque ordinals
  (`Model__…__5_1` / `_2` — visually indistinguishable from a `5.1` version segment)
  are now disambiguated by their backend-route path prefix, the axis along which the
  collisions actually differ (the same model re-served under a `TEE/`, `Pro/`,
  `stealth/`, `openrouter/`, or vendor-path route). Renames include
  `Model__Kilo__Free_1`/`_2` → `Model__Kilo__Free__KiloAuto`/`__OpenRouter`,
  `Model__NanoGPT__GLM__5_1`/`_2` → `Model__NanoGPT__GLM__5__Tee`/`__ZaiOrg`, and 56
  others (58 constants total). The alphabetical-raw-ID ordinal fallback is retained
  only for a genuine tie no discriminator separates (e.g. the same route + date under
  a punctuation-only ID difference). Any code referencing a renamed `Model__…_N`
  constant must update to the new route-suffixed name.

## [0.2.5] — 2026-07-15

**Schema:** `0.2.0` → `0.3.0` (additive). SQLite store schema `5` → `6`.

The **models.dev harmonization + provenance history** epoch: ingest all three
models.dev JSON artifacts (api.json, models.json, catalog.json), bake the new
provider-agnostic metadata dimension (benchmarks, licenses, links), refresh the
static catalog from a vendored July snapshot, make the ingest log an append-only
history with a round-trip export, and land the `#size` lookup prerequisites.
Relates [#9] (size prereqs) and [#13] (stage tokens remain a deferred dimension).

### Added

- **Three-artifact ingestion** (`wire.go`, `client.go`): exported
  `ParseAPIJSON`/`ParseModelsJSON`/`ParseCatalogJSON` plus
  `Client.FetchModelMetadata`/`FetchCatalog` on one shared decode path; wire
  types refreshed for the upstream schema drift (`reasoning_options`, `status`,
  `description`, cost tiers/audio/`context_over_200k`); benchmark scores
  tolerate upstream `number|string` (`ScoreRaw`).
- **Entity metadata dimension** (`metadata.go`): `EntityMetadata`
  (description, license, typed `Links`, `Benchmarks`) attached at the entity
  level via an alias-first join with a two-tier miss policy (unlinked report /
  genuine-absence standalones); `BenchmarkResult` keeps criterion identity,
  harness, score, and claim attribution (`SourceURL`, lab-reported) as separate
  fields; closed enums `ModelStatus`, `LinkType`, `ReasoningOptionKind`;
  `ModelInfo` gains description/status/reasoning-options/cost-tier fields.
- **CLI**: new `entities` subcommand (enumerates every entity, including
  metadata-only standalones); `list --status`; `sources --history` and
  `sources --export` (union of store history and the curated seed, promotable
  into `datasources.json`); one stderr notice when views serve embedded-only
  metadata; sync drift warning; benchmark table renders top-5 with a count
  footer and ellipsis-truncated names.
- **Vendored codegen input** (`parse/data/modelsdev/`): committed
  `catalog.json` + `SNAPSHOT.json` provenance sidecar; codegen is fully offline
  and fails loudly on a missing/corrupt snapshot; new `models_metadata_gen.go`
  bake; `modelsdev_unlinked.json` join report + `modelsdev_aliases.json`
  curation; a real-input regen up-to-date guard.
- **Store v6** (`store.go`): `dataset_ingested` becomes an append-only history
  (composite primary key, `INSERT OR IGNORE`, current = latest); new
  `entity_metadata`/`metadata_benchmarks`/`metadata_links` tables (including
  `raw_family`); the `models` table persists the new instance-level fields;
  presence-guarded self-heals for intermediate v6 caches; sync now persists
  provenance (ingest rows, metadata, attestations).
- **`#size` prerequisites**: lineage keys carry `param_size` with a
  size-agnostic fallback (unsized edges apply to every sized sibling;
  size-specific edges win); `Resolve()` ambiguity grouping and `ModelRef` gain
  the size axis.
- **Stage/mode identity granularity**: `omni`/`livetranslate` are
  identity-class modifiers, `realtime` is an attribute-class serving tier, and
  the laguna `xs`/`m` product lines are curated variants — distinct lab models
  no longer collapse into shared entities.

### Changed

- Static catalog refreshed from the July models.dev snapshot: 162 providers,
  5,654 models, 810 entities (April baseline: 138 providers, ~4,300 models).
- `UpstreamSchemaVersion` pinned to the July schema; ingest `parser_schema`
  is now `3`; `datasources.json` is schema v3 (multi-row ingest history).

### Fixed

- Metadata join family-presence gate no longer synthesizes standalone entities
  for models the catalog actually serves (the presence check now uses the same
  family canonicalization as the enrichment pipeline).
- Sized entities no longer silently lose lineage; `gpt-realtime-*` versions are
  no longer swallowed by the `realtime` token; the laguna metadata collision is
  resolved; parse-time suffix/modifier tie-breaks are total-ordered
  (determinism by construction).

## [0.2.4] — 2026-07-11

**Schema:** `0.1.0` → `0.2.0` (additive). SQLite store schema `4` → `5`.

The **VRAM + quantization + provenance** epoch: model the memory footprint of a
model at each quantization, make parameter size part of entity identity, ingest
the Ollama registry, and record where every model row came from. Addresses the
roadmap VRAM/quantization issue [#12] (and the size/param+quant strand [#9]).

### Added

- **Quantization enum** (`quantization.go`): closed `Quantization` int enum over
  the GGUF/llama.cpp scheme names (`f16`/`bf16`/`f32`, the `q*` k-quants, the
  `iq*` i-quants) plus reserved HF-ecosystem members (`awq`/`gptq`/`int8`/`int4`),
  with `none` as the unquantized zero value and `other` as the fail-safe bucket
  for a recognized-but-unmapped tag. `String`/`MarshalText`/`UnmarshalText`
  serialize the canonical lowercase wire name (case-insensitive on the way in);
  `BitsPerWeight()` returns the authoritative llama.cpp bits-per-weight;
  `DetectQuantization(id)` and `ParseQuantization(s)` extract a quant from a model
  ID / CLI argument (unknown → `other` on detection, an actionable error on parse).
- **Per-quantization VRAM** (`vram.go`, `QuantVRAM` on `ProviderInstance` and
  `ModelInfo`): each quant row carries the ground-truth ingested `WeightsBytes`
  (GGUF file size) and an estimated `VRAMBytes` = weights + KV-cache **baked at the
  model's maximum context**, with **no overhead constant** (`VRAMFormulaVersion 2`).
  When the architectural facts (layers / KV-heads / head-dim) are absent, the KV
  term is excluded and `VRAMEstimatePartial` is set true so `VRAMBytes` is a
  weights-only lower bound, never a silent under-estimate. `(QuantVRAM).EstimateVRAM(ctx)`
  recomputes the figure at a caller-chosen context.
- **Parameter size as identity** (`EntityRef.ParamSize`, `ModelInfo.ParamSize`):
  `EntityRef.String()` gains a `#paramsize` segment —
  `family[/variant][@version][#paramsize]{identity-mods}` — so `llama@3.3#70b{instruct}`
  and `llama@3.3#8b{instruct}` are distinct entities. The segment is omitted when
  size is unknown, so every existing entity key stays byte-identical.
  `EntityByTuple` and the tuple parsers are `#`-aware.
- **Curated Ollama ingest** (`cmd/bestiary-ollama`, `parse/data/quant_vram.json`):
  a network-gated, polite-bot offline tool that joins the Ollama registry to the
  entity model. Community finetunes are **kept**, never dropped; lineage to a base
  is **inferred** (Ollama exposes no base-model marker) via tuple decomposition and
  curated alias tables — base-known finetunes carry an inferred finetune lineage
  edge, base-unknown ones become standalone entities.
- **BCNF data-source provenance** (`datasource.go`, `parse/data/datasources.json`):
  a normalized `DataSource` / `DatasetIngested` / `EntitySource` core (the join
  table carries the entity↔source many-to-many relation) persisted with real
  foreign keys in the SQLite store. `Entity.Sources` is a derived, sorted read
  projection of the join table — not a source of truth. `DatasetIngested` carries
  no URI (a transitive dependency obtained by FK join to `DataSource`); its ingest
  timestamp is a committed snapshot, never a codegen wall-clock stamp.
- **CLI**: `bestiary sources <key>` lists the per-source provenance for an entity
  (joined ingest date and URI, sorted by source); `show`/`providers` JSON carries
  `Entity.Sources`. A `--quant` filter selects a quantization where applicable.
- This `CHANGELOG.md`.

---

## [0.2.3] — 2026-06-08

**Schema:** `0.0.3` → `0.1.0` (additive). Module PR [#20]. Merge commit `e636b2d`.

The **entity model** epoch: deduplicate and link models across providers by their
canonical identity, track derivation lineage, and separate serving-host from model
identity. Resolves the three coupled roadmap issues [#11] (lineage), [#16]
(serving-host), and [#18] (entity-linking).

### Added

- **Entity layer** (`entity.go`): `EntityRef` — the canonical identity tuple
  `(Family, Variant, Version, identity-modifiers)`, keyed by `EntityRef.String()`
  as `family[/variant][@version]{identity-mods}`. `Entity` aggregates every
  provider/host instance of one identity, with `ProviderInstance` carrying the
  per-instance attributes (host, price, context, max-output). `CapabilityUnion`
  ORs capabilities across instances. New APIs: `Entities()` and
  `EntityByTuple(family, variant, version, identityModifiers...)`, both returning
  defensive deep copies so callers cannot corrupt the registry index.
- **Modifier classification** (`modifierclass.go`, `parse/data/modifier_class.json`):
  `ModifierClass` enum (`Identity` / `Attribute`) with `ClassifyModifier(token, family)`.
  A curated global table plus per-family overrides decide whether a trailing
  modifier is part of identity (renders in `{...}`) or a per-instance attribute
  (renders in `[...]`). Unknown tokens default to **Identity** (fail-safe: never
  silently collapse two artifacts into one entity). `EntityModifiers` /
  `attributeModifiers` project a modifier set onto each class.
- **Serving-host dimension** (`host.go`, `host_detect.go`, `parse/data/hosts.json`):
  `Host` string type (`HostNone`/`HostAzure`/`HostAWS`/`HostGCP`/`HostCloudflare`)
  with `DetectHost(id)`. Detection is curated ID-prefix-only and **never consults
  `Provider`**; namespaced IDs (containing `/`) are never split. Host is a
  per-instance attribute, never part of entity identity. Guards against the
  v0.2.2 blanket provider-prefix-strip bug.
- **Lineage DAG** (`lineage.go`, `derivation.go`, `parse/data/lineage.json`):
  `DerivationKind` enum, `LineageEdge` / `LineageRecord`, and cycle-safe
  `Ancestors` / `Descendants` traversal over a curated derivation ledger
  (fine-tunes, distillations, multi-parent merges). `real=false` flags synthetic
  catalog-absent fixtures. Seeded with the Dracarys/Hermes/MythoMax/Solar/Yi cases.
- **CLI**: `bestiary providers <tuple>` lists every provider serving a given
  identity tuple; `bestiary show --by-entity` groups output by entity. The tuple
  parser accepts the `{identity-mods}` segment.

### Changed

- `ModelRef.formatCanonical()` is now modifier-class-aware: identity modifiers
  render in `{...}`, attribute modifiers in `[...]`. Attribute-only models render
  byte-identical to v0.2.2.
- `family.go`: registered `solar`, `yi`, `mythologic`, `huginn` as base families;
  added ID-family overrides for Dracarys-72B and MythoMax.
- `.gitignore`: the `bestiary` binary ignore is now root-anchored (`/bestiary`)
  so it no longer masks same-named nested paths.

### Fixed

- `fast` is demoted from a global identity modifier to a per-family **attribute**
  for tiered families (claude/glm/kimi/deepseek/minimax) after profiling showed it
  is a speed-tier label there, while remaining identity-bearing for families like
  grok/imagen/veo where it denotes a distinct model.
- The 70B Dracarys lineage `child_ref` is aligned to its actual decomposed entity
  key (`dracarys{instruct}`, version empty) so the edge resolves to the real node.

---

## [0.2.2] — 2026-06-06

**Schema:** `0.0.2` → `0.0.3`. Module PR [#15]. Tag `v0.2.2`. Released via a
release-candidate cycle (`-rc1` / `-rc2` / `-rc3`).

Epoch 2 — **cross-provider decomposition consistency**. The headline outcome:
the canonical `(Family, Variant, Version)` decomposition was driven to **zero
divergence** across providers — the *same* model now decomposes to the *same*
identity tuple regardless of which provider's ID spelling it arrives under
(starting from 388 divergent triples, reduced 68 → 18 → 0 over the rc cycle).
This is the precondition that made v0.2.3 tuple-keyed entity linking possible.
Along the way the project ratified a large set of canonical-representation
decisions for specific model families; they are recorded below because they
define how IDs are *interpreted*, not just how the code is structured.

### Canonical representation & serialization

- **The decomposition tuple is canonical.** Every model decomposes to
  `(Family, Variant, Version, Date, Modifier)`. `Version` is distinct from
  `Date` (an identity version vs. a release stamp). The **cross-provider
  consistency metric** is the 3-tuple `(Family, Variant, Version)` *excluding*
  modifier; that exclusive metric is what reached and is gated at **0**.
- **Four serialization schemes** are the supported renderings of the tuple
  (`CanonicalScheme`):
  - `canonical` (CLI alias **`peasant`**) — `provider/family/variant/version@date[modifier]`
  - `huggingface` (`hf`) — `provider/raw-id`
  - `purl` — `pkg:huggingface/provider/raw-id`
  - `raw` — the original API model ID, verbatim
- **`Modifier` became a list** (`string` → `[]string`): multiple trailing
  modifiers now compose losslessly under a ratified modifier taxonomy. Tokens are
  matched **longest-first** (so `think` cannot shadow `thinking`) and rendered in
  a fixed `canonicalModifierOrder`.

### Family-resolution decision layers (curated data)

The decomposition is governed by a layered set of **embedded, curated JSON
tables** under `parse/data/` (data-only directory — no Go files, to avoid an
import cycle; this was explicitly ratified). The precedence chain:

- `family_overrides.json` — explicit `(raw_family → {family, variant})` mappings;
  highest priority, beats all pattern matching.
- `version_patterns.json` — ordered regexes that split a versioned-variant raw
  family (v-/k-/m-prefix, hyphen-version, no-prefix); first match wins.
- `variant_suffixes.json` — suffix strings stripped to identify a variant when no
  override/pattern matches (re-sorted longest-first at load).
- `family_aliases.json` — the **canonical-winner ledger**: maps a
  mislabel/shorthand family to its canonical family, applied after case-fold and
  before bare-generation split, in *both* parse entrypoints.
- `family_enforce.json` — the **canonical-winner ENFORCE set**: a closed list of
  distinct families that WIN over a disagreeing `raw_family` when the model ID
  itself names the family.
- `families.json` — per-family member lists driving `recoverMemberVariant` and
  the **per-family member-guard**: a `variant → modifier` reclassification fires
  *only* when the token is NOT a curated member of the resolved family — so
  `deepseek-chat`, `sonar-reasoning`, `qwen-turbo` keep the token as a product-line
  variant rather than demoting it to a modifier.
- `vendor_aliases.json` — residual non-provider vendor/namespace prefixes (not in
  `Providers()`) stripped from leading ID segments.

### Ratified per-family canonicalization rulings

- **`meta-llama` / `nemotron` folds:** no-slash `meta-llama-*` folds to the
  `llama` family with its version preserved; `nvidia/llama-3.3-nemotron-*`
  decomposes to the `nemotron` family (was an over-capture under the empty-`raw_family`
  provider).
- **`azure-*` folds → upstream family:** `azure-gpt-*` / `azure-o*` resolve to the
  `gpt` / o-series families. Critically, the earlier **blanket azure
  provider-prefix *strip* was removed** — it destroyed a backend-host label
  (NanoGPT's `azure-` prefix). The host signal was deferred to a dedicated
  serving-host dimension, which became the v0.2.3 `Host` type.
- **o-series restructure** for the OpenAI `o1`/`o3`/`o4`-style reasoning line.
- **Whisper:** a family-gated trailing `-v<int>` is recovered as `Version`
  (e.g. `whisper-v3`) instead of being treated as a modifier.
- **Grok:** negation-aware modifier handling emits an explicit `non-reasoning`
  modifier; the `grok-3-mini-fast-beta` member-guard suppresses a false
  `non-reasoning` negation.
- **Brand casing:** stylized `Provider` / `Family` / `Model__` constants (correct
  vendor capitalization in generated identifiers).

### Three sanctioned non-defect residuals (USER-RATIFIED)

After convergence, exactly **three** decompositions were ratified as intentional
non-defects (not divergence bugs), each feeding later roadmap work:

- **dracarys** — a llama fine-tune whose lineage is lost by folding → motivated
  GH#11 (delivered in v0.2.3).
- **solar** — register-or-accept as its own base family.
- **grok-beta** — `beta` as a release stage → motivated GH#13 (release-stage axis).

### Parsing correctness fixes

- Fixed the `raw_family` **version-extraction gap** (`gpt-mini` ⇒ `gpt-5-mini`):
  version digits sitting between the family prefix and the variant are now
  recovered, clearing the `ReasonVersionDigitsNotExtracted` class from the codegen
  `parse_failures.json` audit.
- ID-driven version-presence consistency, a param-size guard (so a parameter count
  like `7b` is not read as a version), and date-guards for 6-digit / `YYMM` forms.
- `Resolve` ambiguity: `ErrAmbiguous` now renders a two-section listing (canonical
  vs. rehosts) with canonical-provider preference; variant-aware bare-family
  shorthand restores `claude-opus → ErrAmbiguous`.

### Determinism

- A committed cross-provider snapshot + a `divergence=0` gate, plus a
  network-gated **drift smoke** test and snapshot goldens, keep the decomposition
  stable across regens.

---

## [0.2.1] — 2026-05-30

**Schema:** `0.0.2` (unchanged). Tag `v0.2.1`.

**Deterministic & reproducible codegen.** `cmd/bestiary-gen` output is now
reproducible:

- Models sorted by `(Provider, ID)` once after assembly (fixes static-file
  reshuffle between regens).
- Collision `Model__*_N` suffixes assigned by raw-ID alphabetical order (stops
  random `_1`/`_2` flipping).
- `models_constants_gen.go` is byte-identical across regens; `models_static_gen.go`
  is identical modulo the `LastSynced` per-run timestamp.
- Reproducibility guard `TestCodegen_Reproducible_ByteIdentical` (N=100) and an
  up-to-date golden guard added.

---

## [0.2.0] — 2026-05-29

**Schema:** `0.0.1` → `0.0.2`. Tag `v0.2.0`.

**Entity normalization pipeline** — canonical model identity:

- `ModelRef` gains `Version` + `Modifier` (the 8-field tuple).
- New `parse` package: family / variant / version / modifier extraction from the
  API `raw_family` plus the model ID, with embedded curated override data.
- Canonical scheme `provider/family/variant/version@date[modifier]`.
- `Resolve` with `--format {peasant,huggingface|hf,purl,raw}`, PURL namespace
  filter with loose fallback, and `ErrAmbiguous` two-section output (canonical vs
  rehosts) with canonical-provider preference.
- `Model__` constants use double-underscore field separators.

---

## [0.1.1] — 2026-04-08

Tag `v0.1.1`. Ignore the `bestiary-gen` build artifact; regenerate
`models_static_gen.go` with the fixed codegen template (named `Family` type).

---

## [0.1.0] — 2026-04-04

**Schema:** `0.0.1`. Tag `v0.1.0`. Adds `LookupModelByProvider` and the `Models`
registry lookup APIs.

---

## [0.0.2]

Tag `v0.0.2`. The original entity-normalization epoch groundwork:

- New `parse` package: `ParseFamily`, `ExtractDate`, `InferFamilyFromID` with
  embedded JSON data.
- `ModelInfo` gains `NormalizedFamily` / `Variant` / `Date` (codegen-baked).
- `ModelRef` refactored to 6 fields including `ID`; `Format(scheme)` dispatch over
  the `CanonicalScheme` enum.
- New types: `Designation`, `AcceptabilityRating`.

[Unreleased]: https://github.com/dayvidpham/bestiary/compare/v0.2.5...HEAD
[0.2.5]: https://github.com/dayvidpham/bestiary/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/dayvidpham/bestiary/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/dayvidpham/bestiary/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/dayvidpham/bestiary/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dayvidpham/bestiary/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dayvidpham/bestiary/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/dayvidpham/bestiary/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/dayvidpham/bestiary/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/dayvidpham/bestiary/releases/tag/v0.0.2
[#20]: https://github.com/dayvidpham/bestiary/pull/20
[#18]: https://github.com/dayvidpham/bestiary/issues/18
[#16]: https://github.com/dayvidpham/bestiary/issues/16
[#15]: https://github.com/dayvidpham/bestiary/pull/15
[#13]: https://github.com/dayvidpham/bestiary/issues/13
[#12]: https://github.com/dayvidpham/bestiary/issues/12
[#11]: https://github.com/dayvidpham/bestiary/issues/11
[#9]: https://github.com/dayvidpham/bestiary/issues/9
