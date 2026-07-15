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

**Schema:** `0.3.0` → `0.4.0` (additive). SQLite store schema **unchanged** at `6`.

The **full-bulk `#size` re-key** epoch (GH#9): one shared enrichment now sizes every
model whose ID carries a parameter-size token, so a `#size` segment is no longer
confined to the handful of curated `quant_vram.json` entries.

### Added

- **Parameter-shape fields** on `ModelInfo` (`TotalParams`, `ActiveParams`,
  `PerExpertParams`, `ExpertCount`), decomposed from `ParamSize` — derived
  presentation facts, never entity-key material. Grouped along shape joints and
  never cross-computed (an NxM MoE token like `8x22b` sets `ExpertCount` +
  `PerExpertParams` but no total).
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
  standalone `beta` token appears, but its decomposition is untouched and its key is
  frozen (grok-4.20-beta keys stay `grok/beta@4.20{…}`). Re-keying beta out of the
  variant is deferred ([#13]).

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
