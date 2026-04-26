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
├── family.go                # Hand-curated Family methods (e.g., upcoming CanonicalProvider)
├── format.go                # JSON, YAML (internal serializer), table output
├── harness.go               # Harness type — identifies coding tool / dev environment
├── merge.go                 # MergeModels() — dedup by (ID, Provider), most-recent-wins
├── modality.go              # Modality int enum, Modalities struct
├── modelref.go              # ModelRef struct + Ref() method (RawFamily/Family/Variant/Version/Date)
├── models_constants_gen.go  # GENERATED — Model__ string constants (~8654 entries)
├── models_static_gen.go     # GENERATED — ~110 ModelInfo structs from 3 providers
├── parse.go                 # ParseFamily, ParseFamilyWithVersion, ExtractVersionFromID
├── provider.go              # Provider string type, IsKnown(), Providers()
├── providers_gen.go         # GENERATED — ~110 provider constants from API
├── registry.go              # StaticModels(), LookupModel(), ModelsByProvider/Family()
├── resolve.go               # Resolve() with auto-detect (canonical/PURL/HF/raw)
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

- **Provider as string type**: 109 providers in the models.dev API. A closed int enum can't scale. String type with well-known constants (Anthropic, Google, OpenAI, Local) gives type safety at call sites with extensibility.
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

## File ownership

| File | Owner | Notes |
|------|-------|-------|
| `models_static_gen.go` | `cmd/bestiary-gen` | Never edit by hand. Regenerate with `go generate ./...` |
| `bestiary.schema.json` | Manual | Must stay in sync with Go types. Verified by `TestJSONOutput_ConformsToSchema` |
| `version.go` | Manual | Update on public type changes or upstream schema updates |
| All other `.go` files | Developer | Normal development workflow |
