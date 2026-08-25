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

- **`CreatorGroups()` / `SeriesGroups()` — a browsable Creator > Family > Series > entities
  projection.** A read-only view over relations that already exist (the curated
  `Family`→`Creator` seed and the computed Series/Release taxonomy); like the taxonomy it is
  computed on read and can never rename an entity. It exists because `SeriesAll()` alone is
  flat — several hundred lines with no organizing level above them — so the projection puts
  the two questions a reader actually arrives with, "whose models?" then "which line?", above
  the generation-level detail. The creator set is derived from the curated seed, so growing
  `creators.json` grows the tree with no code change.
- **The base hoist: no `(base)` node anywhere.** A series' un-named release is not a member
  of the line alongside the named ones — it *is* the line, un-named. Rendering it as a
  sibling node called `(base)` invented a level that does not exist and buried the entities
  of a line with no named releases one click deeper than the entities of a line that has
  them. Those entities are now hoisted onto the series itself (`SeriesGroup.Hoisted`), and
  only genuinely named releases remain as releases. `SeriesGroup.Shape()` reports which of
  `base-only` / `mixed` / `named-only` a line is, so a renderer can lay out the two levels
  correctly (a mixed line shows its hoisted entities above, and visually distinct from, its
  release disclosures). The hoist is a re-parenting, never a filter: a test asserts the
  projection is an exact partition of `Entities()`, so an implementation that dropped the
  un-named entities instead of lifting them fails loudly rather than rendering a plausible
  but smaller tree.
- Entities re-homed by the curated strays table (`series.json`) are shown under the line
  they were re-homed onto, which is the point of the table — so a stray can appear under a
  creator its own family token does not name. A test pins that this divergence is confined
  to strays and can never become a silent misattribution of a normal entity.

### Changed

- **Web: the front page is now a Creator > Family > Series > entities tree (`GET /`).** A
  reader arriving with no query in mind was previously handed a nine-hundred-row table; they
  now get a hierarchy to walk, starting from the lab that trained the weights. Attributed
  creators arrive expanded; families with no curated creator collect in a single collapsed
  group at the bottom. The tree renders whatever creators the curated seed carries, so it
  grows with `creators.json` and hard-codes no lab.
- **Web: the dense entity browser moved from `/` to `GET /entities`, unchanged.** Same seven
  signals, same `#entity-results` SSE patch target, same facets — only its address changed,
  and it is one click from the tree.
- **Web: `GET /families` absorbs the series/release explorer**, emitting the SAME per-series
  anchors, so every detail-page series link still resolves.
- **Web: no `(base)` node on any page.** Both hierarchy pages render one shared partial over
  the new hoisted projection, so the un-named release's entities attach directly to their
  line. On a line that has both, the hoisted entities render above the release disclosures
  with a legend and a rule marking them as a different level of the hierarchy — adjacency on
  screen must not be read as sameness of level. Tests assert the identity on the RENDERED
  document as well as on the projection: a template can drop a branch it never ranges over,
  entirely downstream of a correct projection.
- Four cross-page links were retargeted accordingly: the browser's "browse by series" and
  the entity detail page's "‹ catalog", "series" and series-section anchor links.

### Removed

- **`GET /series` is retired and now returns a hard 404** — the route, its handler and its
  template are deleted, not repointed. It is deliberately not a 301: an alias keeps a dead
  name alive in links, bookmarks and search results indefinitely, which is what retiring the
  name was meant to stop. No content was lost — `/families` absorbed it and emits the same
  anchors.

- **Web: a readability type scale governs every text size.** The `bestiary-web` stylesheet
  gained a six-step, rem-based type scale (`--fs-xs` … `--fs-xl`) and every `font-size` in
  the layout now resolves through it — there is no literal font-size value left in the
  stylesheet, and a test guard keeps it that way. Because the steps are rem-based, a reader
  who raises their browser's base font size now scales the whole page with it; the previous
  px literals ignored that preference outright. Each former size moves up exactly one step
  (12→13, 13→14, 14→15, 15→16 px at the browser default), so the dense entity grid keeps its
  density while the smallest text on the page stops being 12px. Headings, which previously
  carried no explicit size at all, are on the scale too. The steps are named `--fs-*` rather
  than `--text-*` because `--text` and `--text-muted` are already colour tokens. No selector
  was renamed.

## [0.2.9] - 2026-07-28

**Schema:** unchanged at `0.6.0`.

### Added

- Added `HarnessStrike` (`"strike"`) to the canonical known-harness set.

## [0.2.8] — 2026-07-24

**Schema:** `0.5.0` → `0.6.0` (additive). SQLite store schema `7` → `8`.

The **registry-ingest** epoch: names acquire real provenance, and a third source starts
attesting them. A `Nomen` no longer fuses its evidence into scalar columns — it carries a
set of `NomenAttestation`s, each with its own `Authority` (primary / secondary) and
`Method` (curated / harvested / self-minted), persisted in a `nomen_attestations` child
table. A new polite HuggingFace Hub bot (`cmd/bestiary-hf`) harvested 179 Hub repo names
into that layer, making the Hub the third `DataSource` alongside models.dev and Ollama, so
a name that curation already claimed and the Hub independently attests coalesces into one
nomen with two legs rather than two rows. Alongside it: a `Creator` dimension separating
who *trained* the weights from who *serves* them, a `pkg:oci` external-identifier scheme,
an offline `cmd/bestiary-web` catalog front-end, and a curation pass that drained the
models.dev unlinked report to zero.

### Added

- **Nomen multi-attestation** — `Nomen` **HAS-MANY** `NomenAttestation`
  `{SourceURL, Source, Authority, Method, IngestedAt}` (≥1 per nomen). Provenance moves off
  the nomen row, so the same name attested by two ingests is **one** nomen with two
  attestation legs, not two rows. Two new element enums, `AttestationAuthority`
  (unknown / primary / secondary) and `IngestMethod` (unknown / curated / harvested /
  self-minted), follow the `NomenScheme` codec precedent (`String` / `MarshalText` /
  `UnmarshalText`; an unknown token is an actionable error). `coalesceNomina` groups by the
  `(Value, Scheme, ResolvesTo)` triple and **unions** same-triple attestation sets, sorting
  by the total key `(Source, SourceURL, Authority, Method, IngestedAt)` so equal keys imply
  byte-identical, deterministic emission. `ValidateNomina` is **inverted**: a same-triple
  differing attester is now a legal append and only a differing `Status` is a loud conflict
  — and it is wired into `cmd/bestiary-gen`, so the bake aborts on conflict rather than the
  check being test-only. Runtime degrades to the raw sorted set on conflict, never panics.

- **`Creator` dimension** — the lab that **trained** a model's weights (SPDX *originator*),
  distinct from `Provider`, which serves it (SPDX *supplier*). `Creator` is an open string
  type mirroring `Provider` (`CreatorNone` + 9 well-known constants, `String` /
  `MarshalText` / `UnmarshalText` / `IsKnown` / `Creators`), and the Family→Creator mapping
  is a **curated, data-driven seed** (`parse/data/creators.json`) read through the
  graceful-degrade embed loader, never an in-code switch. `ModelInfo.Creator` and
  `Entity.Creator` are **derived join projections**, not stored columns — Family→Creator is
  a function, so a per-row column would be a transitive dependency. `ValidateCreatorTable()`
  is a loud codegen guard.

- **`pkg:oci` external-identifier scheme** — `SchemeOCI` appended to the `CanonicalScheme`
  iota tail (wire-stable) with every recognition arm plus `InputFormatOCI`.
  `formatOCIPurl` implements the purl-spec `oci` type: lowercased last path fragment as
  name, `sha256:<digest>` version with `:` percent-encoded, `repository_url` / `tag`
  qualifiers in canonical alphabetical order — and `""` when the digest is absent, because
  an OCI purl is **never** minted without one. `ModelRef.Format(SchemeOCI)` returns `""` via
  an **explicit** arm rather than falling through to a default that would leak the raw ID.
  `cmd/bestiary-ollama` now persists the manifest config digest it previously fetched and
  discarded, onto `QuantVRAM.OCIDigest`, emitted conditionally by codegen so the
  empty-digest majority stays byte-identical. `NomenSchemeOCI` mints one OCI nomen per
  digest-bearing quant row with an Ollama Harvested/Secondary attestation.

- **`cmd/bestiary-hf` — polite HuggingFace Hub bot + harvested nomen layer**: a
  network-gated tool on `internal/politebot` with its own versioned User-Agent, layering
  HTTP conditionals beneath the shared cadence seam — `If-None-Match`/`ETag` (a 304 keeps
  the existing row) and 429 backoff honoring `Retry-After`. A Hub id is `org/repo`, taken
  **1:1 with case preserved** as the nomen value, with **no decomposition**. The entity join
  is alias-first (`hf_aliases.json` override → mechanical decomposition through the
  production parse pipeline → keep-unlinked and report), so a repo is never silently
  dropped. `DataSourceHuggingFace` is the third source: an entity carrying an HF nomen
  dual-attests `{models.dev, huggingface}`, and `llama@3.3#70b{instruct}` now
  **triple**-attests `{huggingface, models.dev, ollama}`.

- **`cmd/bestiary-web` — offline web catalog (foundation)**: a read-only HTTP server over
  the in-process static registry (`Entities()`/`StaticModels()`) plus an optional read-only
  SQLite cache; it makes NO network request at serve time. Entity pages are addressed by the
  RQ1 multi-segment IRI grammar (`/entity/<key>`, `EntityRef.IRI(webRoot)` == route path) with
  a content-negotiation seam (`Accept: text/html` → page, `application/json` → the entity's
  public JSON shape) and query params treated as non-identity view-state (stripped for
  identity). The base `html/template` layout ships the approved "Phosphor Terminal" CSS in both
  color modes (`prefers-color-scheme` + an explicit `data-theme` toggle). The Datastar client
  is VENDORED via `go:embed` and served same-origin (v1.0.2, no CDN for JS); the two webfonts
  load from a CDN with a full offline fallback stack (fonts-only relaxation, RQ1). Server-side
  interactivity uses the `github.com/starfederation/datastar-go` SDK (SSE `PatchElements`).

- **`cmd/bestiary-web` views — entity browser, entity detail, series explorer**: the browser
  (`/`) is a dense sortable table over every entity with a server-side filter rail —
  family / creator / provider / region / modality exact-match facets plus free-text key
  search and a sort key, all driven through the `/sse/entities` Datastar seam. Modality is
  joined from `StaticModels()` at startup (an `(ID,Provider)→Modalities` map computed once,
  never affecting identity). The default order is family-then-key, fixed and tested so every
  SSE re-render is byte-stable; view-state signals select **which rows and in what order,
  never which entity a link denotes**. Entity detail (`/entity/<key>`) renders four sections
  — quants + VRAM (a `VRAMEstimatePartial` figure is labelled and its bar drawn hollow so it
  never implies precision the data lacks), pricing by provider over the `(ID,Provider)`
  instances, nomina + every attestation's source / authority / method / source-URL, and
  lineage + series membership — with an optional `?ctx` **display-only** VRAM recompute
  column via `(QuantVRAM).EstimateVRAM`. The series explorer (`/series`) is a native
  `<details>` disclosure tree over `SeriesAll`/`ReleasesOf`/`EntitiesOf` with stable anchors.

- **CLI naming/creator surface** — `show` and `list` gain a `Creator` column
  (`CreatorNone` renders `-`, never an invented "unknown") and `show --by-entity` gains a
  `Creator` line plus a **Nomina section**: each nomen's `(scheme, status)` with its
  attestation set indented beneath (Source / Authority / Method / SourceURL), so a
  dually-attested name shows **both** legs. `--scheme oci` / `--format oci` on a ref
  short-circuits with an actionable stderr notice directing to the quant-level view (OCI
  identity is per-quant-digest, so a ref has no render at that altitude) — an *explained
  empty* with exit 0, not an error.

- **Store schema `8`** — `nomen_attestations` child table + a `creators(family PK, creator)`
  BCNF dimension. `migrateToV8` runs in one transaction: create both tables, backfill each
  old `nomina` row's `(source_url, source_id)` into one attestation with authority/method
  derived per the defaults table, then recreate the `nomina` parent without the source
  columns (PK unchanged, so keys stay byte-identical). This removed all seven of the
  transitional v7 single-attestation bridge sites; the child table carries the **full**
  per-attestation set losslessly, including the curated-authored `Authority` the bridge
  could not round-trip. `UpsertNomina` is parent `OR IGNORE` + delete-then-insert children
  in one transaction (the `entity_metadata` replaceable-set precedent), with the child
  `source_id` FK rejecting orphans on full rollback.

### Changed

- **IRI output: `/` is now LITERAL** (`EntityRef.IRI` / `ModelRef.IRI`, BREAKING for the
  minted string). `escapeIRISegment` keeps the key grammar's `/` (family/variant) as a real
  path separator and percent-encodes only the remaining structural delimiters (`@`→`%40`,
  `#`→`%23`, `{`→`%7B`, `}`→`%7D`, and the ref-level `[`→`%5B`, `]`→`%5D`). An entity IRI is
  therefore a multi-segment, walkable path — `…/entity/llama/scout%404%2317b-16e%7Binstruct%7D`
  rather than the v0.2.7 single-segment `…/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D`.
  This is the RQ1-ratified grammar the new `cmd/bestiary-web` `/entity/<key>` routes
  dereference, so `EntityRef.IRI(webRoot)` equals the route path for the same entity (one
  grammar, pinned by a route-equality test). The round trip is unchanged: a whole-string
  `url.PathUnescape` still recovers the canonical key byte-identically (a literal `/` passes
  through; every `%40`/`%23`/`%7B`/`%7D`/`%5B`/`%5D` decodes back), so every IRI round-trip
  fence stays green — only the two golden-string assertions were re-pinned for the literal `/`.

- **`internal/politebot`** — the polite-HTTP seam is extracted out of
  `cmd/bestiary-ollama` into a compiler-private package: one `get()` request seam (≥1s
  inter-request cadence, descriptive versioned User-Agent, optional `Accept`,
  `io.LimitReader` body cap, non-2xx reject) with the injectable doer/clock/sleep
  offline-test hinge preserved via functional options. Both bots now share it; the per-bot
  User-Agent and call-site `Accept` stay bot-owned. Behavior-preserving — the root
  `bestiary` package is untouched and `internal/` is unimportable outside the module, so
  there is **zero public-API impact**.

- **`ModelInfo.Source` defaults to `DataSourceModelsDev`** — the semantics shift from "a
  further source beyond models.dev" to "the originating/attesting ingest source". Filled in
  at the **load** layer (the registry normalizes in-memory static models once) and on the
  store read path, so generated files stay byte-identical and `go generate` stays zero-diff.
  Non-empty carriers (Ollama) are untouched and the `EntitySource` join is unchanged.

- **Human-readable defaults on the entity views** — `show --by-entity`, the entity-key
  fallback, and `series` default to `--output table` when `--output` is not set explicitly;
  `--output=json` is still available by asking for it.

### Fixed

- **Ambiguity guidance is actionable and paste-back-resolvable** — the opaque "matched
  multiple canonicals" jargon is replaced with plain language plus concrete next steps
  derived from the actual candidates. The derived example is the first candidate's **entity
  key** shown via `show --by-entity`, which renders an entity key directly without the
  model-first resolution that produced the ambiguity, so it resolves for every family class
  (including high-fanout keys like `gpt/4o` that would re-ambiguate under a plain
  `show <key>`). `FormatAmbiguous` now lists candidate **entity forms**, not just provider
  slugs, for families without a canonical provider; the duplicated `bestiary:` preamble is
  dropped so only one wrapped error carries the prefix; and the `--format=raw` tip appears
  in exactly one place instead of twice in slightly different wording.

- **`show <input>` accepts an entity key without `--by-entity`** — model resolution stays
  first, and only on a model **miss** does it fall back to the entity view over the
  store-overlaid set. Ambiguous model input keeps the guidance path (no entity fallback on
  ambiguity).

- **Instance table alignment** — over-wide `ID` / `PROVIDER` / `HOST` cells truncate to
  their column widths so long provider slugs no longer break alignment, and rendered rows
  are capped with a `… and N more (use --output json)` footer, mirroring the
  nomina/benchmark convention.

- **HF bot hygiene** — dead RFC-5988 `Link` pagination is removed (the bot verifies known
  `org/repo` candidates with targeted GETs, never a listing endpoint, so pagination is
  inapplicable by design, now documented at the fetch loop); `hf_aliases.json` fails loud
  when two keys case-fold to the same value, since the case-insensitive lookup fallback
  would otherwise resolve by map iteration order; and `huggingface_unlinked.json` gains a
  `count` field per the unlinked-envelope precedent.

### Data

- **`modelsdev_unlinked` drained to 0** — all 11 remaining ids were served-entity join-key
  **disagreements** (the id-only ref mis-derives the family), not catalog-absent models, so
  none synthesizes a standalone and the census stays at the pinned 4 `ornith` rows. Ten are
  resolved by curated `modelsdev_aliases.json` rows mapping each metadata id onto its
  distinct served entity key; `command-a-translate-08-2025` needed `translate` added to
  `modifiers.json` as a peeled **identity** modifier so Command A Translate keys as
  `command/a{translate}` instead of overwriting base `command/a`'s metadata (a collateral
  scan found zero other tail-position `translate` tokens).

- **Evidence-gated parse-failure repairs** — of 286 audit signals, 285 are benign residual
  tokens (size/quant/serving leftovers with the version correct) and are **classified, not
  "fixed"**. The one genuine wrong-entity class is repaired via exact-id family overrides:
  `deepseek-v3-1` / `deepseek-ai/DeepSeek-V3-1` → `deepseek/v3.1` (14 dotted-sibling
  instances) and `deepseek-v3-2-exp` → `deepseek/v3.2-exp`. DeepSeek encodes point releases
  as a **variant** token, so a version-only fix would not merge — variant-pinned instead,
  retiring the phantom `deepseek@1` / `deepseek@2` entities.

- **Harvested HuggingFace seed** — one polite live `cmd/bestiary-hf` run over the
  models.dev-known open-weight `org/repo` candidates: 500 candidates fetched plus 4 forced
  aliases; **179 verified repos joined a catalog entity** and were seeded, 17 verified but
  unlinked (reported), 251 skipped (gated/private/4xx), 0 rate-limited, 0 unchanged. The 4
  pre-existing curated Hub repos are pinned by `hf_aliases.json` to their exact curated
  triples so each harvested attestation **coalesces** onto its curated claim — one nomen,
  two attestations with distinct Method and Source. The curated-layer archive fence is
  scoped to `Method=Curated`; harvested attestations carry live URLs by design.

- **Census re-pins (arithmetic, conscious)** — entities 958 → 957 (`command/a{translate}`
  +1; the two DeepSeek phantom merges −2), series 419 → 417, releases 671 → 669. Nomina
  3,797 → 3,796 from the entity merge, then → **3,971** with the HF harvest (the 4 curated
  repos coalesce with their harvested twins for +0; 175 distinct-triple harvested repos for
  +175). `huggingface`-scheme nomina go 4 → 179.

### Documentation

- **`docs/w3id-runbook.md`** — the user-executed w3id.org registration procedure
  (GitHub PR + Apache `.htaccess`), the content-negotiated HTML/JSON-LD redirect-target
  design, the default IRI base decision at the `EntityRef.IRI(base)` call site, and the RQ1
  URL-scheme ruling (literal `/` inside the canonical key, `@#{}` percent-encoded, query
  params as view-state only). **No registration performed.**

- **`docs/CONCEPTS.md` — attestation quality** — a new section grounding
  `AttestationAuthority` / `IngestMethod` in CIDOC CRM E13 *Attribute Assignment*, CRMinf
  I7 *Belief Adoption*, and Wikidata statement ranks. It also **corrects** the Grounding
  table's prior claim that `AcceptabilityRating` maps to an LRM-E9 "status" attribute — the
  verified LRM spec's Nomen attribute list has no such attribute, and "preferred" is
  expressed via the general *Category* attribute instead.

- **`docs/poetools-claude-code.md`** — live research into the models.dev catalog row
  `poetools/claude-code`, which decomposes as an ordinary Claude entity but is in fact Poe's
  own agent product built on the Claude Agent SDK. Used as the concrete case for the
  `harness.go` harness-vs-model classification question; three candidate relationships are
  recorded and **none chosen**.

- **Registry-ingest research report** and the `cmd/bestiary-web` visual-direction design
  note (retrofuturism, with the ratified gate rulings applied).

## [0.2.7] — 2026-07-23

**Schema:** `0.4.0` → `0.5.0` (additive). SQLite store schema `6` → `7`.

### Added
- **Nomen naming layer**: one queryable record for every way anything names a model
  entity — `Nomen{Value, Scheme, Status, ResolvesTo, SourceURL, Source}` with the
  `NomenScheme` classifier (canonical / provider-id / huggingface / purl / alias)
  and ISO 1087 `AcceptabilityRating` statuses. Minted by one shared production
  function over the entity index plus the curated `parse/data/nomen_claims.json`
  (3,797 nomina at release: 958 canonical Preferred, 2,834 provider-ID Admitted,
  4 huggingface, 1 curated
  alias claim — after the closing Impl-UAT batch below folded the entity census
  977 → 947). `Entity.Nomina()` and `NomenLookup()` (homonym-aware) are the
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
  be re-shaped without re-keying anything. Read APIs: `SeriesAll()` (419 lines, sorted
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
  selector it details that line's releases and their entity keys. The selector is a
  **specificity ladder**, case-folded, each rung narrower than the one above: a family
  name (`claude`) returns every generation of it; a **major version** (`claude-4`, or
  equivalently `claude --version 4`) returns every `claude-4.x` line as a UNION; and a
  full line rendering (`claude-4.8`) returns that one line. The major rung SELECTS, it
  does not re-group — it returns several Series in the same multi-line shape the family
  rung produces, and `claude-4.0`/`claude-4.5` remain distinct lines a narrower selector
  still addresses individually. Membership is a strict string rule (a generation belongs
  to version `4` iff it IS `4` or begins `4.`) with no numeric normalization, so `4`
  never swallows `42`, `1` never reaches `ling#1t` (whose `1t` is a param-size, not a
  version — see the closing batch below) or the leading-zero
  `gemini@001`. GLM's `5p1`/`5p2` spellings are decoded at PARSE into real `5.1`/`5.2`
  versions (see Fixed), so they join the `glm-5` union as ordinary dotted members —
  there is no `p`-awareness in the taxonomy. Sub-1.0
  generations need no special case (`mistral-0` unions `0.1` and `0.3`), and where a
  family spells both a bare `N` and dotted siblings the union includes the bare line.
  The new `--version` flag is exactly equivalent to appending `-<value>` to the
  positional, composes with `--provider`/`--quant`/`--status` (selection first, entity
  filters after), and is rejected with an actionable error when given without a family
  or without a value.

  The **canonical entity grammar** is accepted as a selector too, mapped to its
  series-level meaning — `claude@4` (the major union), `claude@4.5` (one line),
  `claude/opus` (a RELEASE-LEVEL cut: the opus release across every claude generation),
  `claude/opus@4`, and `anthropic/claude@4` (provider-qualified). The `@` is the entity
  VERSION, as in an entity key, not the `show` resolver's `@`-date form; series live
  above entity keys and inherit the key grammar. Parsing reuses the same
  `parseEntityTuple` the entity commands use — there is no second parser. A release cut
  SELECTS the lines carrying that release (a line without one drops out rather than
  rendering its other releases), and a provider prefix feeds the ordinary `--provider`
  machinery; a `--provider` or `--version` that contradicts the selector is an
  actionable error rather than a silent precedence win.

  The new `--input-format` flag pins the selector grammar for scripting: `canonical`
  (the entity grammar, NO fallback — a raw id fails loudly and is told which format
  would read it), `models.dev` (a raw catalog id resolved through the ordinary lookup
  to its entity's line), or the default `infer` (ladder and canonical readings unioned,
  with the raw-id reading as the FINAL fallback — the precedence is documented rather
  than emergent). Table and JSON output; no schema change, since this
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
  Minted census at release: 3,797 (958 canonical, 2,834 provider-ID, 4 huggingface, 1 alias).

### Changed
- **Designation layer activated**: `ModelRef.Designations()` now rates the
  canonical form `Preferred` (raw/HuggingFace/PURL stay `Admitted`) — the
  prerequisite for truthful `skos:prefLabel`/`altLabel` export (GH#24 ask 3).
- **Constants surface is now entity-level** (BREAKING). The ~5,650
  provider-flavored `Model__<Provider>__…` constants are replaced by one
  provider-agnostic `Entity__*` constant per model entity (958 after the closing
  Impl-UAT batch below), each valued by
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
  other key is byte-identical and that re-key leaves the entity census unchanged (a move,
  not a mint). Migration:

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
- **`p`-as-dot version decode** (`parse.go`). Some providers publish a version with a
  `p` where the dot belongs, because their id namespace disallows the dot — their own
  display names spell the dot. Reading it verbatim minted **phantom entities**:
  `glm@5p1` and `glm@5p2` sat stranded beside the real 50- and 65-instance `glm@5.1`
  and `glm@5.2`, so "the GLM 5 line" was silently incomplete. The decode is the SAME
  convention `parseSeriesNumber` has always applied inside the letter-prefix series
  split (`kimi-k2p6` → `k@2.6`, which is why the kimi/minimax spellings already
  resolved); generalizing that one rule to the ordinary version-token position — rather
  than adding a second, provider-gated mechanism for the same phenomenon — is what
  reaches families with no series letter. The shape is narrow on purpose (digits, a
  literal `p`, digits, whole-token), so `4o`, `120b` and any literal `p` not flanked by
  digits are untouched. **Census: 979 → 977 entities** (the two phantoms merge into
  their real siblings; two keys retired, none renamed), 421 → 419 series, 674 → 672
  releases, 3,775 → 3,773 nomina. The `5p1`/`5p2` id spellings survive as Admitted
  provider-ID nomina on the merged entities — decoding a spelling for *identity* never
  erases it from the record — and `series glm-5` now returns 5.1 and 5.2 as ordinary
  dotted members of the union, with no `p`-awareness anywhere in the taxonomy.

  The rule reaches **three positions**, sharing one definition: the ordinary
  version-token path, the letter-prefix series split (where it already lived), and the
  **glued** generation token — `qwen3p7-plus` had its version dropped entirely, because
  `splitBareGen`'s trailing digit/dot scan stops at the `p` and leaves the
  digit-suffixed base `qwen3`, which the not-digit-suffixed clause then rejects. It now
  decomposes to `qwen/plus@3.7` and joins the **pre-existing** entity of that key, so
  no key is created or retired and the census is unmoved (the base-side clauses are
  untouched — the new arm admits a *shape*, never a new permission).

  The one *residual* `k2p7` (kimi-for-coding) defect noted during the p-decode work is
  now **fixed in the closing Impl-UAT batch below**: it landed in the compound family
  `kimi-k2` with an empty version because — unlike its `k2p5`/`k2p6` siblings, which
  arrive with upstream family `kimi-thinking` and resolve — it arrives with the compound
  `kimi-k2`, the *(compound raw family + bare series-token id)* combination the shared
  family-recovery path does not close. Rather than a catalog-wide recovery-path change
  (too broad a blast radius across every kimi/minimax/mimo row), it is pinned with a
  narrow curated exact-id override so it now resolves to **`kimi/k@2.7`** and joins the
  `kimi-2.7` union alongside its `k2p5`/`k2p6` siblings (distinct from the fireworks
  `kimi/k@2.7{code}` coding router). The general recovery-path fix stays deferred.
- **Closing Impl-UAT batch — five user-ratified identity corrections** (BREAKING for
  38 `Entity__*` constants). The final acceptance pass folded five spelling/identity
  defects that had each split one model across two entities or stranded it under a junk
  key. Entity census **977 → 947** (canonical nomina 977 → 947, total 3,773 → 3,743;
  provider-ID **unchanged at 2,791** throughout — every fix MOVES instances, never drops
  one, so each corrected spelling survives as an Admitted provider-ID nomen); Series
  419 → 411 (a flat −8 versioned lines, bare unchanged at 204); Releases 672 → 659;
  sized-catalog entities 336 → 323.

  1. **C4 entity-level `N` → `N.0` fold** (`registry.go`, `NormalizeEntityVersion`). A
     family that spells BOTH a bare integer version `N` and the dotted `N.0` for the
     SAME (variant, param-size, identity-modifiers) folds the bare entity onto the
     dotted one — one canonical spelling per generation. A pure MERGE (never a rename):
     `llama@4`, with no `4.0` sibling, is untouched. Eight pairs fold. A bare-version
     EXPRESSION still resolves: `EntityByKey("claude/opus@4")` and
     `EntityByTuple(claude, opus, "4", "")` return the `claude/opus@4.0` entity. The
     specificity ladder is untouched — selector `claude-4` remains the 4.x union.
  2. **o-series dual-identity unified** (`parse.go`, `canonicalizeOpenAILine`). The same
     OpenAI o-series model was two identities by spelling: `openai/o1` → `gpt/o@1`, but
     digitalocean's hyphen-glued `openai-o1` stranded in a junk family `o`. Both now
     converge on `gpt/o@1` / `gpt/o@3` / `gpt/o@3{mini}`; family `o` empties.
  3. **Dot-lost version spellings repaired** (`parse.go`, `dotLostVersionOverrides`;
     26 dash-glued `qwen2-5-…`/`qwen3-6-…` + 7 dotless `minimax-m25`/`qwen35-…`/
     `mistral-small-31-…` ids) — a version-only curated exact-id override that corrects
     ONLY the version, so family/variant/size/modifier/stage are untouched. Each merges
     into (or re-keys onto) the heavily-attested dotted sibling.
  4. **`k2p7` routed to `kimi/k@2.7`** — the residual defect from the `p`-decode work
     above, fixed with a narrow curated exact-id override (see that entry).
  5. **`tts-1-hd` split to `tts@1{hd}`** — OpenAI documents tts-1-hd as a distinct
     higher-quality product, so `hd` is now an IDENTITY modifier (a new entity, split
     from `tts@1`).
  6. **`1t` routed to the param-size axis** (`parse.go`, trillion unit `t`). Ling-1T /
     Ring-1T are 1-trillion-parameter models, so `1t` is a SIZE not a version:
     `ling@1t` → `ling#1t`, and the `1t` in `ling-2.6-1t` rides beside version 2.6
     (`ling@2.6#1t`). The ollama `:1t` tag on `kimi-k2:1t` (suppress-pinned) and the
     token-internal `r1t2` (`deepseek-…-r1t2-chimera`) are unaffected.

  **Constant migration (38 removed; 8 of the replacements are NEW, the rest are the
  pre-existing dotted constants).** The full list is reproducible via
  `git diff <base>..HEAD -- entities_constants_gen.go`.

  | Old constant (gone) | New constant |
  |---|---|
  | `Entity__Claude__Opus__Version_4` | `Entity__Claude__Opus__Version_4_0` |
  | `Entity__Claude__Sonnet__Version_4` | `Entity__Claude__Sonnet__Version_4_0` |
  | `Entity__Gemini__Flash__Version_3` | `Entity__Gemini__Flash__Version_3_0` |
  | `Entity__Gemini__Pro__Version_3` | `Entity__Gemini__Pro__Version_3_0` |
  | `Entity__Imagen__Version_4` | `Entity__Imagen__Version_4_0` |
  | `Entity__Imagen__Version_4__Fast` | `Entity__Imagen__Version_4_0__Fast` |
  | `Entity__Imagen__Ultra__Version_4` | `Entity__Imagen__Ultra__Version_4_0` |
  | `Entity__Veo__Version_3` | `Entity__Veo__Version_3_0` |
  | `Entity__O` | `Entity__Gpt__O__Version_1`, `Entity__Gpt__O__Version_3` |
  | `Entity__O__Mini` | `Entity__Gpt__O__Version_3__Mini` |
  | `Entity__Minimax__M__Version_25` | `Entity__Minimax__M__Version_2_5` |
  | `Entity__Minimax__M__Version_27` | `Entity__Minimax__M__Version_2_7` |
  | `Entity__Mistral__Small__Version_31__Size_24b__Instruct` | `Entity__Mistral__Small__Version_3_1__Size_24b__Instruct` |
  | `Entity__Qwen__Coder__Version_2__Size_32b__Instruct` | `Entity__Qwen__Coder__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Coder__Version_2__Size_7b__Instruct` | `Entity__Qwen__Coder__Version_2_5__Size_7b__Instruct` **(new)** |
  | `Entity__Qwen__Plus__Version_3` | `Entity__Qwen__Plus__Version_3_5` / `__Version_3_6` / `__Version_3_7` |
  | `Entity__Qwen__Version_2__Size_14b__Instruct` | `Entity__Qwen__Version_2_5__Size_14b__Instruct` |
  | `Entity__Qwen__Version_2__Size_32b__Instruct` | `Entity__Qwen__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Version_2__Size_72b__Instruct` | `Entity__Qwen__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Version_2__Size_7b__Instruct` | `Entity__Qwen__Version_2_5__Size_7b__Instruct` |
  | `Entity__Qwen__Version_2__Size_7b__Omni` | `Entity__Qwen__Version_2_5__Size_7b__Omni` **(new)** |
  | `Entity__Qwen__Version_35__Size_122b_a10b` | `Entity__Qwen__Version_3_5__Size_122b_a10b` |
  | `Entity__Qwen__Version_35__Size_397b_a17b` | `Entity__Qwen__Version_3_5__Size_397b_a17b` |
  | `Entity__Qwen__Version_3__Size_122b_a10b` | `Entity__Qwen__Version_3_5__Size_122b_a10b` |
  | `Entity__Qwen__Version_3__Size_27b` | `Entity__Qwen__Version_3_5__Size_27b`, `Entity__Qwen__Version_3_6__Size_27b` |
  | `Entity__Qwen__Version_3__Size_35b_a3b` | `Entity__Qwen__Version_3_5__Size_35b_a3b` |
  | `Entity__Qwen__Version_3__Size_35b` | `Entity__Qwen__Version_3_6__Size_35b` **(new)** |
  | `Entity__Qwen__Version_3__Size_397b_a17b` | `Entity__Qwen__Version_3_5__Size_397b_a17b` |
  | `Entity__Qwen__Version_3__Size_9b` | `Entity__Qwen__Version_3_5__Size_9b` |
  | `Entity__Qwen__Vl__Version_25__Size_72b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_32b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_72b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_7b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_7b__Instruct` |
  | `Entity__Ling__Version_1t` | `Entity__Ling__Size_1t` **(new)** |
  | `Entity__Ling__Version_2_6` | `Entity__Ling__Version_2_6__Size_1t` **(new)** |
  | `Entity__Ring__Version_1t` | `Entity__Ring__Size_1t` **(new)** |
  | `Entity__Ring__Version_2_6` | `Entity__Ring__Version_2_6__Size_1t` **(new)** |
  | `Entity__Ring_1t__Free__Version_2_6` | `Entity__Ring__Version_2_6__Size_1t` |

  One constant is ADDED with no removal counterpart: `Entity__Tts__Version_1__Hd`
  (`"tts@1{hd}"`, item 5).
- **beta is ALWAYS a release stage, never an identity.** The two axes were already
  independent by construction (`DetectStageFromID` scans the ID *without* stripping), but one
  row asserted beta on both: vercel's `interfaze/interfaze-beta` arrives with an empty
  `raw_family`, so the leading-token pipeline promoted the trailing `beta` into the **variant**
  slot, giving the key `interfaze/beta` while the same record carried `Stage=beta`. That is
  contradictory rather than merely redundant — it splits a lab's beta and non-beta spellings of
  one artifact into two entities that the stage axis simultaneously calls the same model at
  different maturities. A curated exact-ID pin lands the row on the bare `interfaze` family and
  its stage still reads beta. This **reverses an earlier documented exception** that kept beta
  in that key while unifying only the grok line; the comment recording the old rule is
  rewritten. New LOUD codegen guard `ValidateNoBetaInIdentity` aborts the bake if any future
  decomposition puts beta into a key — either as the variant or as an identity modifier —
  naming the offending entity key and the model IDs that landed on it. **No allowlist**: the
  one exception was resolved by curation rather than exempted, and an allowlist would let the
  next one accumulate silently. A rename, so the entity census does not move.
- **`turbo` demoted to an attribute for kimi and minimax** (`parse/data/modifier_class.json`
  `family_overrides`, the glm precedent). Turbo stays IDENTITY globally — `gpt-4-turbo` is a
  different artifact from `gpt-4` — and is demoted only where curation established it names a
  serving speed tier over the *same* artifact. The evidence differs in strength between the two
  and the curated comment says so: **kimi** has repo-identity proof (moonshot serves
  `kimi-k2-thinking` and `kimi-k2-thinking-turbo` from the identical Kimi-K2-Thinking Hub repo,
  so the turbo spelling cannot denote different weights); **minimax** is graded *lower
  confidence* — no repo-identity proof, just the rev-2 URL census resolving the M2.7 /
  M2.5-highspeed serving names back to the plain repos plus lab-practice inference, flagged as
  the first row to revisit. Three entities fold into their plain siblings
  (`kimi/k@2{turbo}`, `kimi/k@2.6{turbo}`, `minimax/m@2.7{turbo}`); the turbo ID spellings
  survive as **Admitted provider-ID nomina** on the merged entities — a demotion changes what
  is *identity*, never what is *recorded*. Three `Entity__*` constants are removed and none is
  renamed, since the surviving siblings' keys never changed.
- **The z8w3 suppression seed still ships EMPTY, and its collision guard proved itself.** The
  first entry attempted (kimi turbo) was rejected at codegen: suppressing the modifier would
  have made `kimi/k@2{turbo}` and the pre-existing `kimi/k@2` both prefer the naming
  `kimi/k@2`, and the guard's own message diagnosed it — *"the modifier is evidently NOT
  redundant — it distinguishes them"*. That is what routed the change to a modifier-class
  demotion instead. The seed's optional `source_url` also picks up the curated-claims
  **archive policy**: present-but-live is now a loud load-time rejection, while a
  missing/corrupt seed still degrades to "no suppression".
- **`series` filter flags are real** (`--provider`, `--quant`, `--status`). They parse on the
  shared flagset for every subcommand, so `bestiary series --provider cohere` was accepted
  and then silently ignored — the worst shape for a filter, since the output looks like an
  answer. They now narrow the **entity list inside each release**, as per-entity predicates
  satisfied by an entity's *instances*: an instance from that provider, an instance carrying
  a matching `QuantVRAM` row, an instance whose model has that release status. Combined
  filters must be satisfied by **one instance simultaneously** (`--provider=X --quant=Y` means
  "X serves it at Y", never "X serves it *and* somebody serves it at Y" — the per-dimension
  reading can report a pairing that does not exist). The drops **cascade**: an emptied release
  is omitted, an emptied line is omitted from both views, and the listing's counts are
  post-filter, so the listing and the detail view can never disagree. An unknown `--quant` or
  `--status` is rejected with an actionable error before any view is computed (the
  `parseQuantFilter` precedent — never a silent empty result), and a selector naming a real
  line the filters empty gets its own actionable error rather than `ErrNotFound`, because the
  selector was good and the filter was what matched nothing. `--db-path` stays **rejected**:
  the view is still registry-static.
- **Canonical-provider preference applies to exact-ID lookups** (`resolve.go`). `bestiary show
  claude-sonnet-4-5-20250929` reported Provider `302ai` for a first-party Anthropic model. The
  preference carried an exact-ID carve-out justified as "those have deterministic
  cross-provider identity" — but that conflated the *identity* an input resolves to (one
  group, stable across runs, which the ID-based grouping already guarantees) with *which* of
  the co-hosting providers becomes the representative a single-model consumer renders. The
  latter was whichever row the static registry listed first: deterministic, but arbitrary.
  Every provider-unqualified canonical-form lookup now prefers the family's curated
  `CanonicalProvider()` when it is non-empty and present in the match set, else returns the
  full match set unchanged. Provider-qualified forms (PURL namespace, `LookupModelByProvider`),
  the multi-group `ErrAmbiguous` path, and `--format=raw` are untouched.
- **Curated claim `SourceURL`s are archive.org snapshots**, enforced at load. A claim is
  evidence of what a lab published, and the model cards and docs pages it cited are edited and
  deleted without notice, so a live URL silently stops attesting the claim. All five curated
  claims now cite a snapshot captured at claim time, and `parseNomenClaims` **rejects** a
  non-archive `source_url` with an actionable error. No new field: the snapshot embeds the
  original URL verbatim in its tail, so the live address stays recoverable. The file-level
  contract is unchanged — a missing or corrupt claims file still degrades gracefully to an
  empty table (the `lineage.go` precedent); a claim that is *present* and violates the policy
  is loud.
- **Decomposition corrections** (curated exact-ID overrides, the dracarys precedent):
  `Qwen2.5-32B-EVA-v0.2` was read as the compound family `qwen2.5-32b-eva` with EVA's own
  release in the variant slot — it is now `eva@0.2#32b`, with the base relationship promoted
  from a family token to an explicit `DerivationFinetune` edge to `qwen@2.5#32b`.
  `command-a-plus-05-2026` was **split across two entities** by a provider disagreement
  (cohere's `raw_family` mapped to variant `a`, dropping the `+`; nano-gpt's empty
  `raw_family` captured `command-a-plus` whole) — both rows now converge on `command/a-plus`,
  the sibling shape `command/r-plus` already had. cortecs glues the major version onto the
  *variant* (`claude-opus4-5` is Opus 4.5, not an Opus 5), which minted four phantom
  `claude/opus@5…@8` entities and stranded the cortecs instances away from the real ones;
  four curated pins merge them back. Entity census 975 → 971 — the first two are renames, the
  cortecs merge retires exactly the four phantoms.
- **The family-`o` junk bucket is emptied, without evicting the real o-series.** vercel labels
  a swathe of unrelated models with the upstream `raw_family` `"o"` — the OpenAI o-series
  family — and bestiary faithfully preserved the attestation, so Alibaba's Wan video models,
  OpenAI's TTS speech models, quiverai's arrow and Cohere's rerankers all decomposed into
  family `o` and shared one entity with the genuine o-series. The mislabel is **upstream's,
  not a mis-parse**: the vendored catalog carries `family="o"` verbatim on 17 vercel rows and
  2 digitalocean ones, and the ID-driven path already decomposed every one of them correctly
  when `raw_family` was empty — so the correction only stops a junk label from overriding an
  answer that was already right. 13 over-captured rows re-home to `wan` / `tts` / `arrow` /
  `rerank`, plus 2 vendor-leak rows (`voyage/rerank-2.5` and its lite sibling, labelled with
  the **org** rather than a family — the leak the enforce ledger exists to correct). The
  genuine o-series is untouched: `openai/o1` and `openai/o3` still resolve through the
  OpenAI-line canonicalization to `gpt/o@N`. (At the time of this entry digitalocean's
  hyphen-glued `openai-o1` / `openai-o3` / `openai-o3-mini` still kept family `o`; the
  **closing Impl-UAT batch below** finished the unification, converging those dashed
  spellings onto `gpt/o@1` / `gpt/o@3` / `gpt/o@3{mini}` so family `o` empties entirely.)
  Entity census 971 → 982: a bucket holding
  many distinct models becomes many distinct entities, so this is the branch's one correction
  that *adds* keys (15 new, 4 retired).
- **Codegen emitted no `ParamSize` on a lineage parent** (`lineageLiteral`). The first curated
  edge to name a sized parent exposed it: the runtime `lineage.json` path returned
  `qwen@2.5#32b` while the baked `ModelInfo.Lineage` said `qwen@2.5` — the same curated edge
  disagreeing with itself across the two paths.
- **`Family.CanonicalProvider()` had no mapping for `command`**, so provider-unqualified
  lookups of Cohere models fell through to the full match set. Cohere publishes the Command
  line; the mapping now says so (base family plus the `command-a` / `command-r` compound
  spellings the catalog emits).
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

### Added (naming policy)
- **Redundant-modifier suppression machinery** (ships with an **empty seed** —
  entries are curated case-by-case): a curated seed entry marks a modifier
  redundant for an entity, making the spelling with the modifier an `Admitted`
  nomen while the `Preferred` value omits it. The entity key is never changed
  and the policy is fully reversible; with the empty seed every rendered name
  is byte-identical to the key (census-fenced). Loud codegen validators reject
  malformed or colliding seed entries.

### Data
- **models.dev snapshot refreshed (2026-07-23)**: 5,654 → 5,765 models,
  162 → 170 providers, 247 → 263 metadata rows. Release-state censuses move
  accordingly (entities 947 → 958, nomina 3,743 → 3,797, series 411 → 419,
  releases 659 → 671, sized 323 → 319); the per-batch arithmetic in the entries
  below is stated relative to each batch's own base and still holds. Upstream
  retired the `kimi-for-coding` `k2p5/k2p6/k2p7` ids (their corpus rows and the
  `k2p7` curated override retire with them) and the `ollama-cloud` `kimi-k2:1t`
  row (that negative control retires; the r1t2 control remains).

### Added (tooling)
- **Concept & architecture docs**: `docs/CONCEPTS.md` (the working vocabulary —
  entity, nomen, appellation, canonicalized expression, series/release — with
  its ISO 1087 / IFLA-LRM/LRMoo grounding) and `docs/architecture.md` (the full
  architecture with ASCII diagrams: data pipeline, parse precedence, naming
  layer, provenance core, storage, test architecture, version axes), both
  cross-referenced from README and AGENTS. Also corrects two stale README
  claims (the designation layer is active, not deferred; generated constants
  are the entity-level `Entity__*` surface, not `Model__*`).
- **Release binaries**: the release tagger now pushes tags with a token minted
  from the release GitHub App, so tag pushes trigger the new `release-build`
  workflow — `bestiary` binaries for linux/darwin x amd64/arm64 (plus sha256
  sums) attach to each GitHub release from the next release onward.
- **Single go:generate directive**: the generator no longer emits a
  `//go:generate` directive into its own generated files (three of them carried
  one, so `go generate ./...` ran the generator three times per invocation);
  the one directive lives in hand-owned `bestiary.go`.
- **Makefile**: `make build` / `test` / `vet` / `fmt` / `generate` / `gates` /
  `install` encode the project's invocation discipline (`CGO_ENABLED=0
  GOWORK=off`) once; `make gates` is the full pre-commit suite including the
  regen-is-byte-clean check.

### Testing
- Fixture-extraction completion: corpus census 65 → 102 (every remaining
  targeted inline table migrated 1:1 under the three-guard discipline);
  unknown-suffix-overflow captured as a corpus; entity-projection probes on the
  two projection corpora; `TESTING.md` documents the census and what
  deliberately stays inline.
- Research deliverables (report-only, decisions user-gated): the turbo/fast
  per-family evidence report (URL-census methodology over the baked metadata
  links) and the general-beta evidence memo.
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

[Unreleased]: https://github.com/dayvidpham/bestiary/compare/v0.2.9...HEAD
[0.2.9]: https://github.com/dayvidpham/bestiary/compare/v0.2.8...v0.2.9
[0.2.8]: https://github.com/dayvidpham/bestiary/compare/v0.2.7...v0.2.8
[0.2.7]: https://github.com/dayvidpham/bestiary/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/dayvidpham/bestiary/compare/v0.2.5...v0.2.6
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
