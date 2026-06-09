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

### Added

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

[Unreleased]: https://github.com/dayvidpham/bestiary/compare/v0.2.3...HEAD
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
[#11]: https://github.com/dayvidpham/bestiary/issues/11
