# Agent Guidelines for bestiary

## Commands

- **Test**: `CGO_ENABLED=0 go test ./...`
- **Test with race**: `go test -race ./...` (requires CGO_ENABLED=1)
- **Vet**: `go vet ./...`
- **Build CLI**: `CGO_ENABLED=0 go build ./cmd/bestiary`
- **Update static data**: `go generate ./...` (requires network)
- **Tidy deps**: `go mod tidy`
- **Commit**: `git agent-commit -m "..."` (never `git commit`)

## Architecture

```
bestiary/
├── bestiary.go              # Package doc, ModelInfo, ModelID, Capability types
├── canonical.go             # Canonical scheme parsing/formatting
├── client.go                # HTTP client with functional options, retry, 10 MB limit
├── designation.go           # Designation type + AcceptabilityRating (ISO 1087)
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
├── registry.go              # StaticModels(), LookupModel(), LookupModelByProvider(), ModelsByProvider/Family()
├── resolve.go               # Resolve() with InputFormat selection (peasant default, no auto-detect) + canonical-provider preference; ErrAmbiguous candidate listing
├── store.go                 # SQLite cache (zombiezen driver), schema migrations
├── version.go               # 4 provenance consts (schema + upstream versions)
├── wire.go                  # Internal JSON wire types for models.dev API deserialization
├── bestiary.schema.json     # JSON Schema (draft-2020-12) for public output types
├── cmd/bestiary/main.go     # CLI entry point: list, show, sync
└── cmd/bestiary-gen/main.go # Codegen: fetches API, writes generated files
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

## Schema versioning

When modifying public types (ModelInfo, Provider, Capability, Modalities):
1. Update `bestiary.schema.json` to match
2. Increment `BestiarySchemaVersion` in `version.go`
3. Run `TestJSONOutput_ConformsToSchema` to verify conformance

When updating wire types for upstream API changes:
1. Re-derive the SHA-256 hash: `sha256sum ~/codebases/models.dev/packages/core/src/schema.ts`
2. Get the commit: `cd ~/codebases/models.dev && git log --oneline -1`
3. Update `UpstreamSchemaVersion`, `UpstreamGitCommit` in `version.go`
4. Run `go generate ./...` to refresh static data

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
