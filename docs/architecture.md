# Architecture

How bestiary is built: the data pipeline from upstream artifacts to a queryable
identity layer, the runtime structure, storage, and the disciplines that keep
it deterministic. Vocabulary is defined in [`CONCEPTS.md`](CONCEPTS.md);
per-epoch design rationale lives in [`../AGENTS.md`](../AGENTS.md).

## The big picture

```
  UPSTREAM (network, occasional)                 COMMITTED (deterministic)
 ┌──────────────────────────────┐    vendor     ┌──────────────────────────────┐
 │ models.dev  api.json         │  ──────────►  │ parse/data/modelsdev/        │
 │             models.json      │  (manual,     │   catalog.json + SNAPSHOT    │
 │             catalog.json     │   1 request)  ├──────────────────────────────┤
 ├──────────────────────────────┤               │ parse/data/*.json            │
 │ registry.ollama.ai           │  ──────────►  │   quant_vram, datasources,   │
 │  (cmd/bestiary-ollama,       │  (polite bot, │   aliases, overrides,        │
 │   network-gated)             │   ≥1s/req)    │   lineage, series, claims,   │
 └──────────────────────────────┘               │   modifier_class, seeds      │
                                                └──────────────┬───────────────┘
                                                               │ go generate (offline)
                                                               ▼
                                                ┌──────────────────────────────┐
                                                │ cmd/bestiary-gen  (codegen)  │
                                                │  parse pipeline over every   │
                                                │  raw ID → canonical tuples   │
                                                └──────────────┬───────────────┘
                                                               │ bakes
                                                               ▼
 ┌───────────────────────────────────────────────────────────────────────────┐
 │ GENERATED  models_static_gen.go   (~5,650 ModelInfo rows, 162 providers)  │
 │            entities_constants_gen.go (one Entity__ const per entity)      │
 │            models_metadata_gen.go / families_gen.go / providers_gen.go    │
 └───────────────────────────────────┬───────────────────────────────────────┘
                                     │ compiled in
                                     ▼
 ┌───────────────────────────────────────────────────────────────────────────┐
 │ RUNTIME LIBRARY (package bestiary)                                        │
 │   registry → entity index → naming layer → taxonomy → resolve             │
 └───────┬───────────────────────────────────────────────────────┬───────────┘
         │                                                       │
         ▼                                                       ▼
 ┌───────────────────┐                                 ┌───────────────────────┐
 │ cmd/bestiary CLI  │                                 │ SQLite store (v7)     │
 │ list show entities│◄── offline reads ──────────────►│ cache + provenance +  │
 │ providers series  │        sync (online) ──────────►│ nomina + metadata     │
 │ sources sync      │                                 └───────────────────────┘
 └───────────────────┘
```

Two failure disciplines, deliberately distinct: at **codegen** a missing or
corrupt input is a **loud actionable error** (never bake an empty catalog); at
**runtime** the curated `go:embed` loaders **degrade gracefully** to empty,
non-nil values (never panic).

## The parse pipeline (raw ID → canonical tuple)

Every raw provider ID decomposes into the canonical tuple through one
deterministic pipeline, with a strict precedence order — curated always beats
mechanical:

```
   raw ID  "eu.anthropic.claude-sonnet-4-5-20250929-v1:0"
     │
     ▼
 ┌─ 1. pipeline alias files ──────────────┐  ollama_aliases.json /
 │    present? → SOLE identity, stop      │  modelsdev_aliases.json
 └────────────────┬───────────────────────┘
                  ▼
 ┌─ 2. curated exact-ID pins ─────────────┐  idFamilyOverrides (parse.go),
 │    family/variant/version/mods pins,   │  param_size_overrides.json,
 │    size-token pins, family_overrides   │  family_enforce.json
 └────────────────┬───────────────────────┘
                  ▼
 ┌─ 3. mechanical decomposition ──────────┐  vendor/namespace strip → host &
 │    strip order:                        │  region capture (Bedrock eu./us.)
 │    [attrs] → {mods} → #size →          │  → suffix tables → version
 │    @version → /variant                 │  patterns → p-as-dot decode →
 └────────────────┬───────────────────────┘  modifier classification
                  ▼
 ┌─ 4. normalization at identity ─────────┐  NormalizeEntityVersion:
 │    bare-N → N.0 where the family       │  one primitive, applied at every
 │    attests both (merge-only)           │  entity-key construction joint
 └────────────────┬───────────────────────┘
                  ▼
   (family, variant, version, #size, {mods})  +  Host, Region, Stage, attrs
```

Unparseable inputs are logged to `parse_failures.json` at codegen — never
silently mangled. Spelling repairs (dot-lost versions, glued p-notation,
vendor glue) live **here**, where the raw IDs are, so downstream layers never
guess.

## The entity index and the naming layer

```
        models_static_gen.go (flat ModelInfo rows, (ID, Provider)-keyed)
                       │  group by canonical tuple
                       ▼
 ┌──────────────────────────────────────────────────────────────────┐
 │ ENTITY INDEX (registry.go, sync.Once)                            │
 │                                                                  │
 │  EntityRef ──► Entity{ Ref, Instances[], Lineage[], Providers[], │
 │                        Hosts[], Regions[], ranges, Capabilities, │
 │                        Sources[] }                               │
 │                                                                  │
 │  lookups: EntityByKey (string) / EntityByTuple (parts)           │
 │  aggregates: ProvidersOf, EntityKeys, Entities                   │
 └───────────────┬───────────────────────────┬──────────────────────┘
                 │                           │
                 ▼                           ▼
 ┌───────────────────────────┐   ┌────────────────────────────────┐
 │ NAMING LAYER (nomen.go)   │   │ TAXONOMY (taxonomy.go)         │
 │  MintNomina(): ONE shared │   │  computed relations ABOVE keys │
 │  production function      │   │                                │
 │   canonical → Preferred   │   │  Series{Family, Generation}    │
 │   provider IDs → Admitted │   │    └─ Release{Series, Name}    │
 │   claims file → Alias/HF  │   │         └─ Entities            │
 │   suppression seed → demote   │  SeriesAll/ReleasesOf/         │
 │  NomenLookup (homonym-aware)  │  EntitiesOf; curated strays    │
 └───────────────────────────┘   │  in series.json                │
                                 └────────────────────────────────┘
```

The one-decode-path doctrine, twice over: `ParseAPIJSON`/`ParseModelsJSON`/
`ParseCatalogJSON` are the **single** wire conversions shared by codegen, the
live client, and tests; and `MintNomina()` is the **single** naming producer
shared by runtime lookups, `sync` persistence, and the codegen census — no
second copy exists to drift.

## Resolution (expression → thing)

```
  input expression
     │
     ├─ entity key grammar ─────► parseEntityKey → EntityByTuple ──► Entity
     │    family[/variant][@ver][#size]{mods}        (bare-version
     │                                                normalizes to N.0)
     ├─ canonical model form ───► Resolve() ─┬─ one match ──► ModelRef
     │    provider/family/variant@date       ├─ multi-provider, one group:
     │                                       │    prefer CanonicalProvider
     │                                       └─ multi-group ──► ErrAmbiguous
     ├─ raw ID / HF / purl ─────► scheme-selected lookup (opt-in --format)
     │
     └─ series selectors ───────► specificity ladder (cmd/bestiary):
          family → all generations         claude
          family-MAJOR → the N.x union     claude-4
          family-MAJOR.MINOR → one line    claude-4.5
          canonical grammar accepted       claude/opus@4, anthropic/claude@4
          --input-format pins the grammar  {canonical, models.dev, infer}
```

## Provenance (the relational core)

```
 ┌────────────────┐        ┌──────────────────────────┐
 │  data_sources  │◄───FK──│  dataset_ingested        │  append-only history:
 │  ID PK         │        │  PK(source_id,           │  current ingest =
 │  URI UNIQUE    │        │     ingested_at)         │  MAX(ingested_at)
 │  CanonicalName │        └──────────────────────────┘
 └───────┬────────┘
         │ FK                      many-to-many
 ┌───────▼────────┐        ┌──────────────────────────┐
 │  entity_source │        │  nomina                  │
 │  (EntityKey,   │        │  PK(value, scheme,       │  homonyms = N rows;
 │   SourceID)    │        │     entity_key)          │  same-triple conflict
 └────────────────┘        │  FK → data_sources       │  = LOUD codegen error
                           └──────────────────────────┘
        + entity_metadata / metadata_benchmarks / metadata_links
          (keyed by lab-scoped metadata_id — immune to entity re-keying)
```

Rules: an entity is attested by a source **iff** an `entity_source` row exists
(`Entity.Sources` is a derived projection, never authoritative); benchmark
scores are **lab-reported claims** with `SourceURL` (claimant) distinct from
`Source` (ingest); curated claim `SourceURL`s are archive.org snapshots by
policy, enforced loudly in the claims loader.

## Storage (SQLite, schema v7)

```
 OpenStore ──► PRAGMA foreign_keys = ON  (per-connection, BEFORE migrations)
          ──► schema_meta version check ──► presence-guarded self-heal
                                            migrations (v4→…→v7): add only
                                            what is missing, preserve data
 tables: models (region column) · the provenance core above ·
         entity_metadata + children · nomina
 offline: list/show/entities/series read static + cache (no network)
 online:  sync fetches, persists models + metadata + nomina + one real
          wall-clock ingest row (committed snapshots stay pinned timestamps)
```

## The CLI

```
 cmd/bestiary
   list       flat catalog rows (filter: --provider/--status)      offline
   show       one model (input --format) or --by-entity aggregate  offline
   providers  an entity's instances + per-quant VRAM               offline
   entities   the full entity census (incl. metadata standalones)  offline
   series     the Series/Release hierarchy (specificity ladder,    offline,
              canonical selectors, --input-format, filters)        registry-static
   sources    per-entity attestations; --history; --export         offline
   sync       fetch + persist + provenance                         ONLINE
```

`series` rejects `--db-path` (registry-static — it cannot honour the flag);
filters are per-instance conjunctive predicates; unknown filter values are
actionable errors, never silent no-ops.

## Codegen determinism

```
 INV3: two runs over the same committed input are byte-identical
       (LastSynced normalized). Enforced by:
   · one (Provider, ID) sort before any emission
   · explicit sort.Slice on every emitted literal set
   · TestCodegen_Reproducible_ByteIdentical (N=100)
   · TestCodegen_UpToDate(+_RealInput): regen-vs-committed diff guards
   · the injectivity guard: duplicate Entity__ identifier fails the bake
 regen lands as its own chore(gen): commit (atomic with its feature commit
 only when a split would be red — flagged, never silent)
```

## Test architecture

```
 authored facts                        computed properties
 ┌─────────────────────────────┐      ┌─────────────────────────────┐
 │ JSON corpora                │      │ inline Go tests             │
 │ testdata/<area>/*.json      │      │ census sweeps over the      │
 │ (111 corpora)               │      │ built catalog, invariant    │
 │  three guards each:         │      │ sweeps (injectivity, merge  │
 │   · exact-N count           │      │ tuple-invariant, partition),│
 │   · value coverage          │      │ codegen determinism, store  │
 │   · non-vacuity (Validate)  │      │ migration fixtures, argv    │
 │  cases carry classification │      │ mechanics                   │
 │  + provenance + mutation    │      └─────────────────────────────┘
 └─────────────────────────────┘
 The boundary rule (TESTING.md): if a human wrote the row to pin a fact,
 it is a corpus row; if the cases are computed from live data, they stay
 inline. `make gates` runs the full suite + regen-clean check.
```

## Version axes

```
 module tag        vX.Y.Z         what `go get` resolves; auto-tagged on
                                  release-PR merge (App-token push → the
                                  release-build workflow attaches binaries)
 BestiarySchemaVersion  0.5.0     the public JSON output contract
                                  (bestiary.schema.json); additive bumps
 store schema           v7        SQLite migrations (schema_meta)
 UpstreamSchemaVersion  pinned    the models.dev schema snapshot + commit
```

Distinct numbers; never conflate them.
