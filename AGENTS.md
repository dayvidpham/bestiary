# Agent Guidelines for bestiary

## Commands

- **Test**: `CGO_ENABLED=0 go test ./...`
- **Test with race**: `go test -race ./...` (requires CGO_ENABLED=1)
- **Vet**: `go vet ./...`
- **Build CLI**: `CGO_ENABLED=0 go build ./cmd/bestiary`
- **Update static data**: `go generate ./...` (offline; reads the committed `parse/data/modelsdev/catalog.json`). To pull a newer upstream deploy, see "models.dev snapshot refresh".
- **Tidy deps**: `go mod tidy`
- **Commit**: `git agent-commit -m "..."` (never `git commit`)

## Architecture

```
bestiary/
├── bestiary.go              # Package doc, ModelInfo, ModelID, Capability types
├── canonical.go             # Canonical scheme parsing/formatting
├── client.go                # HTTP client with functional options, retry, 10 MB limit
├── datasource.go            # BCNF data-source provenance: DataSource/DatasetIngested/EntitySource + curated loader + FK guards
├── designation.go           # Designation type + AcceptabilityRating (ISO 1087)
├── entity.go                # Entity model: EntityRef (#size identity), EntityByTuple, QuantVRAM, Entity.Sources projection
├── errors.go                # ErrNotFound, ErrAmbiguous, ErrAPIUnavailable (struct errors, use errors.As)
├── families_gen.go          # GENERATED — Family type and constants from API
├── family.go                # Hand-curated Family methods (CanonicalProvider — popular families mapped, rest stubbed)
├── format.go                # JSON, YAML (internal serializer), table output
├── harness.go               # Harness type — identifies coding tool / dev environment
├── merge.go                 # MergeModels() — dedup by (ID, Provider), most-recent-wins
├── modality.go              # Modality int enum, Modalities struct
├── modelref.go              # ModelRef 8-field tuple + Ref()/Format() (RawFamily/Family/Variant/Version/Date/Modifier)
├── models_constants_gen.go  # GENERATED — Model__ string constants (~8650 entries, double-underscore fields)
├── models_static_gen.go     # GENERATED — ~4,300 ModelInfo structs from ~115 providers
├── parse.go                 # ParseFamily, ParseFamilyWithVersion, ExtractVersionFromID, ExtractModifier; parse-failure audit
├── provider.go              # Provider string type, IsKnown(), Providers()
├── providers_gen.go         # GENERATED — ~115 provider constants from API
├── quantization.go          # Quantization closed int enum + DetectQuantization/ParseQuantization/BitsPerWeight
├── quant_vram_data.go       # Curated loaders: QuantVRAMFor/ParamSizeFor/SourceFor + ValidateQuantVRAMTable (graceful-degrade)
├── registry.go              # StaticModels(), LookupModel(), LookupModelByProvider(), ModelsByProvider/Family(); entity index + entity↔source join relation build
├── resolve.go               # Resolve() with InputFormat selection (peasant default, no auto-detect) + canonical-provider preference; ErrAmbiguous candidate listing
├── store.go                 # SQLite cache (zombiezen driver), schema v5 migrations + BCNF provenance tables (real FK)
├── vram.go                  # EstimateVRAMBytes (weights + KV, no overhead) + (QuantVRAM).EstimateVRAM(ctx); VRAMFormulaVersion 2
├── version.go               # 4 provenance consts (schema + upstream versions)
├── wire.go                  # Internal JSON wire types for models.dev API deserialization
├── parse/data/*.json        # Curated codegen inputs (quant_vram.json, datasources.json, ollama_aliases.json, lineage.json, …)
├── bestiary.schema.json     # JSON Schema (draft-2020-12) for public output types
├── cmd/bestiary/main.go     # CLI entry point: list, show, providers, sources, sync
├── cmd/bestiary-gen/main.go # Codegen: fetches API, bakes static catalog (incl. QuantVRAM/Source/EntitySource)
└── cmd/bestiary-ollama/main.go # OFFLINE network-gated Ollama refresh tool (polite-bot; rebuilds quant_vram.json)
```

## Code style

- **Go version**: 1.24+, always `CGO_ENABLED=0`
- **Dependencies**: stdlib + `zombiezen.com/go/sqlite` only. Do not add external deps without discussion.
- **Types**: Prefer strongly-typed enums (Provider, Modality) over bare strings. Use zero values ("", 0) for always-present fields; use pointers (*float64) only for genuinely optional fields.
- **Errors**: Use struct error types (ErrNotFound, ErrAPIUnavailable) with actionable messages. Callers use `errors.As`, not `errors.Is`. Include what, why, where, and how-to-fix in error messages.
- **Context**: Accept `context.Context` on all client and store methods. Note: zombiezen/sqlite does not support per-operation context cancellation — ctx is accepted for API consistency but not threaded into SQLite calls.

## Testing conventions

- **Framework**: stdlib `testing` only. No testify, gomega, or external test frameworks.
- **Fixtures**: Shared `testModel()` / `testModels()` helpers for consistent test data.
- **SQLite tests**: Use `openMemStore(t)` for in-memory databases. Use `t.TempDir()` for filesystem path tests.
- **HTTP tests**: Use `net/http/httptest.Server` for mock API responses.
- **Environment**: Use `t.Setenv()` for XDG_CACHE_HOME and similar env var tests.
- **Assertions**: Check observable output (return values, stdout, error messages), not internal state.
- **Integration focus**: Prefer tests that exercise real code paths (real SQLite, real HTTP parsing) over mocks.

## Key design decisions

- **Provider as string type**: ~115 providers in the models.dev API. A closed int enum can't scale. String type with well-known constants (Anthropic, Google, OpenAI, Local) gives type safety at call sites with extensibility.
- **Canonical normalization**: every model decomposes to `(Family, Variant, Version, Date, Modifier)` via deterministic suffix tables + curated overrides in `parse/data/`. The tuple is canonical; `ModelRef.Format(scheme)` renders canonical/HuggingFace/PURL/raw strings. Version is distinct from Date (Opus 4.5 ≠ 4.6). Unparseable inputs are logged to `parse_failures.json` at codegen, never silently mangled.
- **Composite key (ModelID, Provider)**: Same model ID appears under multiple providers with different pricing. Store, merge, and registry all use the (ID, Provider) tuple.
- **Capability type for Interleaved**: The models.dev API returns `interleaved` as either `true` or `{"field": "reasoning_details"}`. Other boolean fields are always pure booleans. Only Interleaved uses the Capability struct.
- **Offline list/show, online sync**: `list` and `show` read static + SQLite cache (no network). `sync` fetches from the API and persists to SQLite.
- **Most-recent-wins merge**: When static and cached data overlap on (ID, Provider), the entry with the more recent LastSynced timestamp wins.
- **Schema migrations**: SQLite store uses a `schema_meta` version table. OpenStore() auto-migrates old schemas via table recreation with data preservation.
- **Internal YAML serializer**: Write-only, ~50 lines, no external yaml dependency. Handles the flat ModelInfo output case.

## v0.2.4 design decisions (VRAM, quantization & data-source provenance)

These are enduring facts about how the codebase models a model's memory footprint, its
identity at a given size, and where its data came from. The authoritative design record is the
handoff `bestiary-84su7` (the full ratified surface + implementation notes) and the URD
`bestiary-ipt4q` (R1–R11 + the Plan-UAT revision rounds = the *why* behind each decision).

- **Parameter size is part of entity identity** (`entity.go`). `EntityRef` carries a
  `ParamSize` field and `EntityRef.String()` renders it as a `#paramsize` segment:
  `family[/variant][@version][#paramsize]{identity-mods}`. A 70B and an 8B of one family have
  different weights, VRAM, and architecture, so they are genuinely **distinct entities** —
  param size belongs in the key, not as a per-instance attribute (quantization, by contrast,
  *is* per-instance: one entity, many quant rows). The `#` segment is **omitted when size is
  unknown**, which makes the migration byte-identical: every pre-v0.2.4 entity key is
  unchanged. The carrier chain is curated `param_size` → `ParamSizeFor(id)` →
  `ModelInfo.ParamSize` → registry grouping → `EntityRef`. Parser strip order is fixed:
  `[attrs]` → `{mods}` → `#size` → `@version` → `/`. `EntityByTuple` and the tuple parsers
  (`parseEntityTuple`, `matchCanonicalSegments`, `isBareIdentifier`) are all `#`-aware.

- **VRAM is computed, never ingested** (`vram.go`, `QuantVRAM` in `entity.go`). Ollama
  publishes a GGUF **file size** (download/disk size), not a VRAM figure, so `WeightsBytes` is
  the ground-truth ingested file size and `VRAMBytes` is *derived*:
  `VRAMBytes = WeightsBytes + KVCache`, with `KVCache = 2·layers·kvHeads·headDim·ctx·2`
  (fp16 KV, GQA-aware). Two deliberate choices, both ratified at Plan-UAT: VRAM is baked at
  the model's **maximum context** (`VRAMContextTokens` records which context was used — not a
  fixed 4096), and there is **no overhead constant** (`VRAMFormulaVersion = 2`; the earlier
  1 GiB overhead was removed). The weights term is *always* the ingested file size;
  `BitsPerWeight()` exists for sanity-checking only and never feeds the baked figure. When any
  architectural fact (layers / kvHeads / headDim) is absent the KV term is dropped (`KV = 0`)
  and `VRAMEstimatePartial` is set true, so `VRAMBytes` is an honest weights-only lower bound
  rather than a silent under-estimate. `(QuantVRAM).EstimateVRAM(ctx)` recomputes the figure
  from the stored inputs at any caller-chosen context.

- **Quantization is a closed int enum** (`quantization.go`). Following the `DerivationKind`
  precedent (not Provider-as-string), `Quantization` enumerates the GGUF/llama.cpp scheme
  names — `f16`/`bf16`/`f32`, the `q*` k-quants, the `iq*` i-quants — plus *reserved*
  HF-ecosystem members (`awq`/`gptq`/`int8`/`int4`, ingest deferred). `QuantizationNone` is the
  zero value and `QuantizationOther` is the fail-safe bucket so an unknown-but-recognized tag
  is never dropped (the raw token rides along on `QuantRaw`). `DetectQuantization(id)` strips a
  quant tag off an Ollama-style ID (unknown → `Other` + raw, never panics); `ParseQuantization(s)`
  is the CLI path (unknown non-empty → an actionable error, never a silent `Other`).

- **Curated data is a codegen input, not a live fetch** (`parse/data/*.json`,
  `quant_vram_data.go`). Per-quant weights/architecture (`quant_vram.json`) and source
  provenance (`datasources.json`) are committed JSON, auto-embedded via the `parse.go` glob and
  read by graceful-degrade loaders (the `lineage.go` precedent: never panic, never nil — a
  missing/corrupt file degrades to empty). `list` / `show` / `sources` therefore never touch
  the network. **Determinism invariant (INV3):** codegen output is byte-identical across runs
  except the `LastSynced` wall-clock. New literals (`QuantVRAM`, `EntitySource`, `DataSource`)
  must render via an **explicit `sort.Slice`** — the existing Providers/Hosts aggregate emits
  *first-seen* order (deterministic only because `staticModels` is pre-sorted, not because it
  sorts), so it is **not** a pattern to copy for the new emissions. `DatasetIngested.IngestedAt`
  is a **committed snapshot** read from `datasources.json`, never a codegen wall-clock, so it
  needs no normalization. Regen lands as a **separate `chore(gen):` commit**;
  `TestCodegen_Reproducible_ByteIdentical` (N=100) and `TestCodegen_UpToDate` must stay green.

- **Provenance is a BCNF + FK relational core** (`datasource.go`, `store.go`). Multiple
  sources now attest to a model (models.dev, Ollama, future Unsloth/HF), so provenance is
  first-class and normalized: `DataSource(ID PK, URI UNIQUE, CanonicalName)` is the dimension;
  `DatasetIngested(SourceID PK, IngestedAt, ParserSchema)` is the per-source ingest fact and
  carries **no URI** (that would be a transitive dependency — the URI is obtained by FK join to
  `DataSource`); `EntitySource(EntityKey, SourceID)` is the many-to-many **join table** and the
  single source of truth for entity↔source. `Entity.Sources []DataSourceID` is a **derived,
  sorted, denormalized read projection** of the join table — convenient, but not authoritative.
  The SQLite store persists all four tables (schema v5) with **real foreign keys**: `OpenStore`
  sets `PRAGMA foreign_keys = ON` per-connection *before* migrations (SQLite defaults FK
  enforcement off, so the FK clauses are decorative without it), and the fresh-DB path creates
  the BCNF tables too (not only the `migrateToV5` arm). Rule: a model is attested by a source
  iff there is an `EntitySource` row — dual attestation yields two rows and `Sources =
  [models.dev, ollama]`; never split one entity into two, and never treat the array as the
  source of truth.

- **The Ollama ingest is an offline, polite-bot tool** (`cmd/bestiary-ollama`). The hard
  models.dev↔Ollama ID-join lives in this network-gated binary, never in `go test`. It is
  **alias-first**: a curated `ollama_aliases.json` entry overrides the mechanical decomposition
  (curated > mechanical, matching the `parse/` override precedent); otherwise it strips the
  quant tag and decomposes the remainder through the production parse pipeline into an
  `EntityRef` key, matched against `StaticModels()` (retrying with an `instruct` modifier for
  bare size-only tags). Community finetunes are **kept, never dropped**: Ollama exposes no
  base-model marker, so lineage is **inferred** (decomposition + curated tables) — base-known
  finetunes carry a `base_ref` (→ a `DerivationFinetune` lineage edge), base-unknown ones become
  standalone entities and are appended to a sorted `ollama_unlinked.json` for visibility. On
  refresh, **field ownership** is explicit: fetch-owned fields (`weights_bytes`, the quant set,
  `param_size`) are overwritten while curation-owned fields (architecture facts,
  `context_window`, `base_ref`, `_comment`s) are preserved. The bot uses a descriptive
  User-Agent and waits **≥1 second** between requests (a hard project constraint).

- **Two version axes, both bump for v0.2.4.** `BestiarySchemaVersion` (the public JSON output
  contract in `bestiary.schema.json`) goes `0.1.0` → `0.2.0` — additive only; all new props
  (`ParamSize`, `QuantVRAM`, `Source`, `Entity.Sources`, the `DataSource`/`DatasetIngested`/
  `EntitySource`/`Quantization` `$defs`) are omitted from `required`. The SQLite store
  `currentSchemaVersion` (migrations) goes `4` → `5`. These are **distinct** numbers; do not
  conflate them. `TestJSONOutput_ConformsToSchema` enforces schema/type agreement.

## Schema versioning

When modifying public types (ModelInfo, Provider, Capability, Modalities):
1. Update `bestiary.schema.json` to match
2. Increment `BestiarySchemaVersion` in `version.go`
3. Run `TestJSONOutput_ConformsToSchema` to verify conformance

When updating wire types for upstream API changes:
1. Re-derive the SHA-256 hash: `sha256sum ~/codebases/models.dev/packages/core/src/schema.ts`
2. Get the commit: `cd ~/codebases/models.dev && git log --oneline -1`
3. Update `UpstreamSchemaVersion`, `UpstreamGitCommit` in `version.go`
4. Refresh the vendored snapshot + regenerate (see "models.dev snapshot refresh" below)

## models.dev snapshot refresh

Codegen consumes a **committed** snapshot of the models.dev catalog, not a live fetch:
`cmd/bestiary-gen` reads `parse/data/modelsdev/catalog.json` (the upstream `catalog.json`
artifact — both the `providers` and `models` views from a single deploy). `go generate ./...`
runs `go run ./cmd/bestiary-gen --no-fetch`, which reads that committed file and is fully
offline and deterministic; a **missing or corrupt** vendored catalog is a LOUD actionable
error — codegen never degrades to an empty catalog.

Refreshing the snapshot is a deliberate, occasional, manual act (mirrors the schema-hash
workflow above). To pull a newer upstream deploy:

1. **Fetch + re-vendor (one polite request).** From the module root run the generator in
   fetch mode (NO `--no-fetch`):
   ```
   go run ./cmd/bestiary-gen
   ```
   This does a single GET of `https://models.dev/catalog.json` (descriptive User-Agent),
   **overwrites** `parse/data/modelsdev/catalog.json` and its
   `parse/data/modelsdev/SNAPSHOT.json` manifest (`{artifact, fetched_at, etag,
   upstream_head_sha}` — informational provenance, never parsed into output), and regenerates
   the `*_gen.go` files from the freshly-fetched data.
2. **Append the ingest-history row.** Add one row to the `ingested` array in
   `parse/data/datasources.json` for `source_id: "models.dev"`: set `ingested_at` to the
   SNAPSHOT.json `fetched_at` value (a COMMITTED timestamp — never load-time wall-clock) and
   `parser_schema` to the current value (3). Leave the existing rows untouched — the log is
   append-only and the current ingest is the row with the maximum `ingested_at`.
3. **Deterministic regen.** Run `go generate ./...` (this is `--no-fetch`; it reads the
   just-vendored catalog). Review the diff like any curated-data change; the emitted
   `parse/data/modelsdev_unlinked.json` report lists join-disagreement metadata ids to triage
   into `parse/data/modelsdev_aliases.json`.
4. **Commit as a separate `chore(gen):` commit.** Land the regenerated `*_gen.go` files (and
   the vendored `catalog.json` / `SNAPSHOT.json`) in their own commit, after the feature
   commit, per the codegen-determinism regen workflow below. `TestCodegen_Reproducible_ByteIdentical`
   (N=100) and `TestCodegen_UpToDate` must stay green.

## Releases

Release tags are created automatically by `.github/workflows/tag-on-release-merge.yml` when a
release PR is merged. **Do not tag releases by hand** — drive them through the PR title:

1. Open the release PR into `main` with a title of the exact form `release(vX.Y.Z): <summary>`.
   The version is carried in the conventional-commit scope; pre-releases are supported
   (`release(v0.2.3-rc1): …`). A space after the colon is required.
2. On merge, the workflow validates the title and creates the annotated tag `vX.Y.Z` on the
   **resulting commit on `main`** (squash or merge commit), then pushes it.

The workflow only takes effect **once it has landed on `main`** — for `pull_request` triggers GitHub
runs the workflow from the base branch, so the PR that introduces it does not tag itself and any
release merged earlier must be tagged by hand. It is a no-op for any other PR title, and **fails
loudly if the tag already exists** (it never force-moves a published tag — so a duplicate or
mistyped release is caught). Tags pushed by its `GITHUB_TOKEN` do **not** trigger downstream
`on: push: tags` workflows — use a PAT or deploy key if a release-build job is later chained off the tag.

## File ownership

| File | Owner | Notes |
|------|-------|-------|
| `models_static_gen.go` | `cmd/bestiary-gen` | Never edit by hand. Regenerate with `go generate ./...` |
| `models_metadata_gen.go` | `cmd/bestiary-gen` | Never edit by hand. Baked models.dev metadata; regenerate with `go generate ./...` |
| `parse/data/modelsdev/catalog.json` + `SNAPSHOT.json` | `cmd/bestiary-gen` (fetch mode) | Vendored codegen input. Never edit by hand; refresh via "models.dev snapshot refresh" |
| `parse/data/modelsdev_unlinked.json` | `cmd/bestiary-gen` | Codegen-emitted join-disagreement report. Never edit by hand |
| `bestiary.schema.json` | Manual | Must stay in sync with Go types. Verified by `TestJSONOutput_ConformsToSchema` |
| `version.go` | Manual | Update on public type changes or upstream schema updates |
| All other `.go` files | Developer | Normal development workflow |

## Codegen determinism invariants

The codegen pipeline (`cmd/bestiary-gen`) is required to be fully deterministic: two successive runs over the same input MUST produce byte-identical output **modulo the `LastSynced` codegen timestamp** (wall-clock). Model ordering and collision `_N` assignment are fully deterministic. The `LastSynced` wall-clock stamp is the sole known residual non-determinism; making it deterministic is tracked in bestiary-vq6k (Epoch 2 / FOLLOWUP). See bestiary-9lnq for the original ordering bug.

1. **Model ordering (R1)**: `fetchModelsWithRaw` sorts the assembled model slice by `(Provider, ID)` — ascending lexicographic — immediately before returning. This single sort covers both the outer provider-map iteration and inner model-map iteration nondeterminism of the models.dev API response.

2. **Collision suffix ordering**: When two models share the same candidate constant name and the version-suffix pass (a) cannot distinguish them, the fallback assigns `_1`, `_2`, … by alphabetical raw model ID order (not slice position). This makes the `_N` binding stable regardless of insertion order. Meaningful naming of collision groups is deferred to bestiary-r66e (Epoch 2).

3. **Reproducibility test**: `TestCodegen_Reproducible_ByteIdentical` (N=100, `cmd/bestiary-gen/main_test.go`) verifies byte-identity across 100 fresh codegen runs using a hermetic fixture. The test exercises the `run()` LastSynced stamping path (mirroring `main.go:363-365`) with two alternating RFC3339 timestamps (tsA / tsB), normalizes the `LastSynced` value on both sides before comparing, and asserts that raw output from two differently-stamped runs differs **only** in `LastSynced` lines — confirming it is the sole residual non-determinism. Run this test locally before committing codegen changes.

4. **Up-to-date guard**: `TestCodegen_UpToDate` checks that the committed golden excerpts (`cmd/bestiary-gen/testdata/expected_*_excerpt.go.golden`) match what the current codegen logic would produce from the fixture. Both sides have `LastSynced` normalized before comparison, so the guard is insensitive to the codegen wall-clock. If this test fails after a logic change, re-run regen and commit the updated generated files.

5. **Regen workflow**: After any change to `cmd/bestiary-gen/main.go`, regenerate and commit the generated files as a **separate** `chore(gen):` commit:
   ```
   go run ./cmd/bestiary-gen --no-fetch
   git add models_static_gen.go models_constants_gen.go
   git agent-commit -m "chore(gen): regen after <change>"
   ```
   Note: a second `--no-fetch` run after committing will still show a diff in `LastSynced` lines (wall-clock stamp). This is expected under the current guarantee. True zero-diff regen (deterministic `LastSynced`) is tracked in bestiary-vq6k.
