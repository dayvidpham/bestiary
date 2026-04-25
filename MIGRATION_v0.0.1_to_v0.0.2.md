# Migration Guide: bestiary v0.0.1 → v0.0.2

This document describes every change introduced in the v0.0.2 schema epoch
(entity normalization pipeline). Each section lists: the nature of the change
(additive / breaking / data-migration), before/after snippets where applicable,
and concrete steps for downstream consumers (e.g., the `provenance` module or
any tool that imports `github.com/dayvidpham/bestiary`).

**Audit trail:** IMPL_PLAN `bestiary-0ip`, SLICE-7 `bestiary-0ug0`,
URD `bestiary-rjf`, PROPOSAL-3 `bestiary-1oq`.

---

## Table of Contents

1. [Schema version bump 0.0.1 → 0.0.2](#1-schema-version-bump-001--002)
2. [ModelInfo new fields: NormalizedFamily, NormalizedVariant, NormalizedVersion, NormalizedDate](#2-modelinfo-new-fields)
3. [ModelRef shape: 7 fields (added ID + Version)](#3-modelref-shape-7-fields)
4. [NEW types: CanonicalScheme, Designation, AcceptabilityRating](#4-new-types)
5. [NEW Resolve API + ErrAmbiguous error type](#5-new-resolve-api)
6. [NEW parse package: ParseFamily, ExtractDate, InferFamilyFromID](#6-new-parse-package)
7. [SQLite v2→v3: column renames + idx_canonical index](#7-sqlite-v2v3-migration)
7b. [SQLite v3→v4: version column + idx_canonical rebuild](#7b-sqlite-v3v4-version-column--idx_canonical-rebuild)
8. [NEW Model_* constants (ModelIDs function)](#8-new-model_-constants)
9. [CLI: bestiary show --scheme flag; bestiary-gen --cache-dir and --no-fetch](#9-cli-changes)

---

## 1. Schema version bump 0.0.1 → 0.0.2

**Nature:** Additive; JSON wire compatible.

The `BestiarySchemaVersion` constant in `version.go` has been bumped from
`"0.0.1"` to `"0.0.2"`. The `bestiary.schema.json` `$id` and `version` fields
have been updated accordingly.

**Before (`version.go`):**
```go
const BestiarySchemaVersion = "0.0.1"
```

**After (`version.go`):**
```go
const BestiarySchemaVersion = "0.0.2"
```

**bestiary.schema.json** `$id` and `version`:
```json
// Before
"$id": "https://github.com/dayvidpham/bestiary/bestiary.schema.json",
"version": "1.0.0"

// After (bare $id — version carried by the "version" field, not the URI)
"$id": "https://github.com/dayvidpham/bestiary/bestiary.schema.json",
"version": "0.0.2"
```

The `@0.0.2` URI suffix used during development was non-idiomatic: the `@`
character is not a valid URI path character and confuses URI resolvers (ajv,
jsonschema-rs, VS Code). The `"version"` field is the canonical location for
the schema version string; `$id` remains a stable bare URL.

**Fix-up steps for downstream consumers:**
- If you pin `BestiarySchemaVersion == "0.0.1"` anywhere, update to `"0.0.2"`.
- Update any stored `$id` references to the schema URL.
- Re-run `go get github.com/dayvidpham/bestiary@v0.0.2` and `go mod tidy`.

---

## 2. ModelInfo new fields

**Nature:** Additive; no existing fields removed or renamed.

Four codegen-baked normalization fields have been added to `ModelInfo`
(`bestiary.go`). They are populated at code-generation time by
`cmd/bestiary-gen` invoking `parse.ParseFamilyWithVersion`, `parse.ExtractDate`,
`parse.InferFamilyFromID`, and (since SLICE-FIX-1 cycle 2) `parse.ExtractVersionFromID`.
They are zero-value (`""`) for models loaded from a pre-v4 SQLite cache until a
`bestiary sync` is performed.

**Before (`ModelInfo`):**
```go
type ModelInfo struct {
    ID          ModelID
    Provider    Provider
    DisplayName string
    Family      Family
    ContextWindow int
    // ... remaining fields
}
```

**After (`ModelInfo`):**
```go
type ModelInfo struct {
    ID          ModelID
    Provider    Provider
    DisplayName string
    Family      Family

    // Codegen-baked normalization (SLICE-FIX-1)
    NormalizedFamily  Family  // canonical family (e.g. "claude")
    NormalizedVariant string  // variant suffix (e.g. "opus", "sonnet")
    NormalizedVersion string  // model version (e.g. "4.5", "4.6", "2.5"); see note below
    NormalizedDate    string  // YYYY-MM-DD date from model ID or ReleaseDate

    ContextWindow int
    // ... remaining fields unchanged
}
```

**NOTE on NormalizedVersion:** The version is extracted from the model ID
(e.g. `"claude-opus-4-5-20251101"` → `"4.5"`) because the upstream models.dev
API family strings do not embed version numbers (`"claude-opus"` not
`"claude-opus-4-5"`). After SLICE-FIX-1 cycle 2, NormalizedVersion is populated
for approximately 636 of 4325 static models. Models whose IDs carry no separable
version component will have `NormalizedVersion: ""`.

**JSON wire impact:**
```json
// New fields appear in all JSON outputs (FormatModel, FormatModels):
{
  "NormalizedFamily":  "claude",
  "NormalizedVariant": "opus",
  "NormalizedVersion": "4.5",
  "NormalizedDate":    "2025-11-01"
}
```

All four new fields are declared `required` in `bestiary.schema.json`. Consumers
that previously used `additionalProperties: false` validation will need to accept
these four new keys.

**Fix-up steps:**
1. Update any JSON deserialization struct definitions to include all four new fields.
2. Update any JSON Schema validators that use `additionalProperties: false` on
   `ModelInfo`-shaped objects — add the four new property declarations.
3. For models loaded from a pre-v4 SQLite cache, `NormalizedFamily/Variant/Version/Date`
   will be empty strings until `bestiary sync` is re-run (see Section 7b for v3→v4 migration; Section 7 for v2→v3).

---

## 3. ModelRef shape: 7 fields

**Nature:** Additive; `ID` and `Version` fields are new. Other 5 fields
(`Provider`, `RawFamily`, `Family`, `Variant`, `Date`) were present in the
previous shape.

`ModelRef` (`modelref.go`) now carries a 7-field tuple. The `ID` field stores
the original API model identifier (e.g. `"claude-opus-4-5-20251101"`), and
`Version` carries the extracted model version (e.g. `"4.5"`), providing
the full context needed to distinguish `claude-opus-4-5` from `claude-opus-4-6`.

**Before (`ModelRef`):**
```go
type ModelRef struct {
    Provider  Provider
    RawFamily Family
    Family    Family
    Variant   string
    Date      string
}
```

**After (`ModelRef`):**
```go
type ModelRef struct {
    ID        ModelID  // NEW — original API model identifier
    Provider  Provider
    RawFamily Family   // API family verbatim
    Family    Family   // canonical family after normalization
    Variant   string   // canonical variant suffix
    Version   string   // NEW — model version (e.g. "4.5", "4.6", "2.5"); "" if none
    Date      string   // YYYY-MM-DD release date
}
```

`ModelInfo.Ref()` populates all 7 fields from the static registry. The
`Format(SchemeCanonical)` method now includes Version in the path when present:
- With Version: `<provider>/<family>/<variant>/<version>@<date>`
  e.g. `anthropic/claude/opus/4.5@2025-11-01`
- Without Version: `<provider>/<family>/<variant>@<date>` (unchanged)
  e.g. `anthropic/claude/opus@2025-05-14`

`Format(SchemeRaw)` returns `string(r.ID)`.

**Fix-up steps:**
1. Update any `ModelRef` struct literals — add `ID: m.ID` and `Version: m.NormalizedVersion`
   when constructing manually.
2. Update code that accesses `ModelRef` fields to use `r.ID` instead of
   indirect lookups through `RawFamily` when the raw model ID is needed.
3. Update any canonical-form string comparisons to expect the Version segment
   when the model carries a version (see `formatCanonical` in `modelref.go`).

---

## 4. NEW types: CanonicalScheme, Designation, AcceptabilityRating

**Nature:** Additive; new public types, no existing types changed.

### 4.1 CanonicalScheme (`canonical.go`)

`CanonicalScheme` is a `int`-based enum with four constants. It controls the
serialization format for `ModelRef.Format(s CanonicalScheme) string`.

```go
type CanonicalScheme int

const (
    SchemeCanonical  CanonicalScheme = iota  // "<provider>/<family>/<variant>@<date>"
    SchemeHuggingFace                         // "<provider>/<raw-id>"
    SchemePURL                                // "pkg:huggingface/<provider>/<raw-id>"
    SchemeRaw                                 // string(ModelRef.ID)
)
```

`ParseScheme(s string) (CanonicalScheme, error)` converts CLI flag strings
(`"canonical"`, `"huggingface"`, `"purl"`, `"raw"`) to the enum.

### 4.2 AcceptabilityRating (`designation.go`)

`AcceptabilityRating` is an `int`-based enum following ISO 1087 terminology.
All designations generated in this epoch default to `AcceptabilityAdmitted`.

```go
type AcceptabilityRating int

const (
    AcceptabilityAdmitted    AcceptabilityRating = iota  // default
    AcceptabilityPreferred                                // deferred
    AcceptabilityDeprecated                               // deferred
)
```

`AcceptabilityRating.String()` returns `"admitted"`, `"preferred"`, or
`"deprecated"` — matching the `$defs/AcceptabilityRating` enum values in
`bestiary.schema.json`.

### 4.3 Designation (`designation.go`)

`Designation` pairs a model identity string with its scheme, provider, and
acceptability rating.

```go
type Designation struct {
    Value    string              // serialized model ID under Scheme
    Scheme   CanonicalScheme     // serialization scheme
    Provider Provider            // hosting provider
    Rating   AcceptabilityRating // acceptability (all "admitted" in this epoch)
}
```

`ModelRef.Designations() []Designation` returns 4 designations per model
(SchemeRaw, SchemeCanonical, SchemeHuggingFace, SchemePURL), all rated
`AcceptabilityAdmitted`.

**Fix-up steps:**
- These are new types; no existing API is broken.
- Import `github.com/dayvidpham/bestiary` to access `CanonicalScheme`,
  `AcceptabilityRating`, and `Designation`.
- Use `ParseScheme` to convert CLI `--scheme` flag values.

---

## 5. NEW Resolve API + ErrAmbiguous error type

**Nature:** Additive; new public function and error type.

`Resolve(input string, opts ...ResolveOption) ([]ModelRef, error)` (`resolve.go`)
maps a model identity string (raw ID, canonical path, HuggingFace form, or
PURL) to one or more `ModelRef` values. Scheme is auto-detected from the input
prefix or can be pinned via `WithScheme(s CanonicalScheme)`.

```go
// Zero matches → *ErrNotFound
// Multiple distinct canonicals → *ErrAmbiguous
// One canonical (possibly multiple providers) → []ModelRef, nil
refs, err := bestiary.Resolve("claude-opus-4-20250514")
refs, err := bestiary.Resolve("anthropic/claude-opus-4-20250514")
refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514")
refs, err := bestiary.Resolve("claude", bestiary.WithScheme(bestiary.SchemeCanonical))
```

**ErrAmbiguous** (`errors.go`) is returned when the input matches models with
two or more distinct canonical triples:

```go
type ErrAmbiguous struct {
    Input      string       // original query string
    Scheme     CanonicalScheme
    Candidates []ModelRef   // one representative per distinct canonical
}
```

Use `errors.As` to extract it:

```go
var ambig *bestiary.ErrAmbiguous
if errors.As(err, &ambig) {
    // ambig.Candidates lists all matching canonicals
    bestiary.FormatAmbiguous(os.Stderr, ambig)
}
```

**Fix-up steps:**
- These are new exports; no existing API changes.
- Add `errors.As` handling for `*ErrAmbiguous` in any code that calls `Resolve`.
- Do not confuse with `*ErrNotFound` (zero matches) — both are distinct error types.

---

## 6. NEW parse package

**Nature:** Additive; new package embedded in the `bestiary` module root.

Three top-level functions in `parse.go` (package `bestiary`) power the
normalization pipeline. They are also called by `cmd/bestiary-gen` at code
generation time to bake `NormalizedFamily/Variant/Date` into
`models_static_gen.go`.

### 6.1 ParseFamily

```go
func ParseFamily(raw Family) (Family, string)
```

Decomposes a raw API family value into a canonical `(Family, variant)` pair.
Resolution order:
1. `parse/data/family_overrides.json` — explicit mappings (e.g. `"claude-opus"` → `{family:"claude", variant:"opus"}`).
2. Versioned-variant regex patterns from `parse/data/version_patterns.json`.
3. Suffix-stripping from `parse/data/variant_suffixes.json`.
4. Fallback: return `(raw, "")`.

Data files are embedded via `//go:embed parse/data/*.json`.

### 6.2 ExtractDate

```go
func ExtractDate(id ModelID, releaseDate string) string
```

Extracts a `YYYY-MM-DD` date from a model ID (e.g. `"claude-opus-4-20250514"` →
`"2025-05-14"`) or falls back to the `releaseDate` field. Returns `""` when no
date is found.

### 6.3 InferFamilyFromID

```go
func InferFamilyFromID(id ModelID, p Provider) Family
```

Fallback for models with an empty `Family` field (~25% of the API corpus).
Splits the model ID on `-`, strips trailing version tokens, and returns the
first alphabetic-leading token as the inferred family.

**Fix-up steps:**
- These are new exports; no existing API changes.
- `ParseFamily`, `ExtractDate`, and `InferFamilyFromID` are called automatically
  by `go generate ./...` via `cmd/bestiary-gen`. Downstream consumers do not
  need to call them unless implementing a custom normalization pipeline.

---

## 7. SQLite v2→v3: column renames + idx_canonical index

**Nature:** Data migration; automatic on first `OpenStore` call.

The SQLite `models` table schema was upgraded from v2 to v3.

| Column / Index | v2 | v3 | Notes |
|---|---|---|---|
| `family` | raw API family | renamed to `raw_family` | preserves original API value |
| `family` | — | NEW `TEXT NOT NULL DEFAULT ''` | parsed canonical family |
| `variant` | — | NEW `TEXT NOT NULL DEFAULT ''` | parsed canonical variant |
| `date` | — | NEW `TEXT NOT NULL DEFAULT ''` | YYYY-MM-DD release date |
| `idx_canonical` | — | NEW index on `(family, variant, provider)` | powers QueryByCanonical |

The Go migration (`store.go` → `migrateToV3()`) uses the table-recreate pattern
for SQLite compatibility (< 3.25.0). A reference SQL file is available at
`migrations/v2_to_v3.sql`.

**Migration is automatic:** `OpenStore(path)` detects the current schema version
from `schema_meta` and runs `migrateToV3()` when version < 3. No manual action
is required on upgrade.

**After migration, `family`/`variant`/`date` columns are empty strings** until
`bestiary sync` is run. A sync re-populates all three normalized columns from
the API response using `ParseFamily` and `ExtractDate`.

**New `QueryByCanonical` method:**
```go
func (s *Store) QueryByCanonical(ctx context.Context, family Family, variant string, provider Provider) ([]ModelInfo, error)
```

**Fix-up steps:**
1. No manual migration needed; upgrade is automatic.
2. After upgrading the Go module, run `bestiary sync` to populate the new columns.
3. If you manage external SQLite databases (not via `OpenStore`), apply
   `migrations/v2_to_v3.sql` using Approach A (table-recreate) for broad
   SQLite version compatibility.
4. Update any raw SQL queries targeting the `family` column — it is now
   `raw_family`. Use the new `family` column for normalized lookups.

---

## 7b. SQLite v3→v4: version column + idx_canonical rebuild

**Nature:** Data migration; automatic on first `OpenStore` call after upgrading.

The SQLite `models` table schema was upgraded from v3 to v4 as part of SLICE-FIX-1.

| Column / Index | v3 | v4 | Notes |
|---|---|---|---|
| `version` | — | NEW `TEXT NOT NULL DEFAULT ''` | extracted model version (e.g. "4.5") |
| `idx_canonical` | `(family, variant, provider)` | rebuilt as `(family, variant, version, provider)` | powers QueryByCanonical with version lookup |

The Go migration (`store.go` → `migrateToV4()`) uses the `ALTER TABLE ADD COLUMN`
approach (appropriate for adding a single `TEXT NOT NULL DEFAULT ''` column in
SQLite ≥ 3.0). A reference SQL file is available at `migrations/v3_to_v4.sql`.

**Migration is automatic:** `OpenStore(path)` detects the current schema version
from `schema_meta` and runs `migrateToV4()` when version < 4. No manual action
is required on upgrade.

**After migration, `version` column is empty string** until `bestiary sync` is
run. A sync re-populates all four normalized columns (including the new `version`)
from the API response using `ParseFamilyWithVersion` and `ExtractVersionFromID`.

**Fix-up steps:**
1. No manual migration needed; upgrade is automatic.
2. After upgrading the Go module, run `bestiary sync` to populate the new `version` column.
3. If you manage external SQLite databases (not via `OpenStore`), apply
   `migrations/v3_to_v4.sql` to add the `version` column and rebuild `idx_canonical`.
4. Update any raw SQL queries that use `idx_canonical` for ordering/filtering —
   the index now covers `(family, variant, version, provider)`.

---

## 8. NEW Model_* constants (ModelIDs function)

**Nature:** Additive; generated constants file `models_constants_gen.go`.

`cmd/bestiary-gen` now emits a `Model_*` constant for every model in the static
registry. Constants follow the naming pattern:

```
Model_<Provider>_<Family>_<Variant>_<Date>?
```

where each component uses the same casing as the `Provider` and `Family` types
(PascalCase preserved). Underscores separate components; within-component
characters are preserved.

**Examples:**
```go
const (
    Model_Anthropic_Claude_Opus_20250514 ModelID = "claude-opus-4-20250514"
    Model_Google_Gemini_Flash            ModelID = "gemini-2.0-flash"
    Model_OpenAI_Gpt_4o                  ModelID = "gpt-4o"
)
```

`ModelIDs() []ModelID` returns a defensive copy of all constant values (the
name differs from the PROPOSAL-3 spec `Models()` to avoid a clash with the
existing `registry.go:StaticModels()` function).

**Important:** `Provider`, `Family`, and `Harness` type constants are **not
renamed**. PascalCase is preserved — no breaking change to existing constant
references.

**Fix-up steps:**
- These are new exports; no existing API changes.
- Update `go generate ./...` to regenerate `models_constants_gen.go` after any
  upstream API update (`bestiary-gen` fetches the current model list).
- Do not edit `models_constants_gen.go` by hand — it is owned by `cmd/bestiary-gen`.

---

## 9. CLI changes

**Nature:** Additive flag for `bestiary show`; additive flags for `bestiary-gen`.

### 9.1 `bestiary show` — new `--scheme` flag

The `bestiary show` subcommand gains an optional `--scheme` flag controlling
how the model identity is displayed.

```
bestiary show <model-id> [--scheme=<canonical|huggingface|purl|raw>] [flags]
```

**Default behavior (unchanged):** When `--scheme` is omitted, `show` performs
auto-detection from the input string prefix:
- `"pkg:huggingface/..."` → `SchemePURL`
- `"<word>/..."` (two segments, no `"pkg:"` prefix) → `SchemeHuggingFace`
- Otherwise → `SchemeRaw` (exact model ID lookup)

**New flag values:**
| Value | Behavior |
|---|---|
| `canonical` | `<provider>/<family>/<variant>@<date>` |
| `huggingface` | `<provider>/<raw-id>` |
| `purl` | `pkg:huggingface/<provider>/<raw-id>` |
| `raw` | original API model ID verbatim |

**Fix-up steps:**
- The flag is optional with backward-compatible auto-detect default.
- Existing scripts calling `bestiary show <id>` are unaffected.

### 9.2 `bestiary-gen` — new `--cache-dir` and `--no-fetch` flags

`bestiary-gen` (code generator) gains two flags for CI/offline use:

```
bestiary-gen --cache-dir=<path>  # directory for caching API responses
bestiary-gen --no-fetch          # use cached response only, skip network fetch
```

`--cache-dir` defaults to `$(go env GOPATH)/pkg/mod/cache/bestiary-gen` (or
`$XDG_CACHE_HOME/bestiary-gen` if set). `--no-fetch` is `false` by default.

**Fix-up steps:**
- These are additive flags; existing `go generate ./...` invocations are unaffected.
- For offline/CI builds, set `--no-fetch` and pre-populate the cache directory.

---

## Appendix: Beads audit trail

| Reference | ID | Role |
|---|---|---|
| IMPL_PLAN | `bestiary-0ip` | Entity normalization pipeline implementation plan |
| SLICE-7 | `bestiary-0ug0` | Schema + version + migration docs (this document's slice) |
| SLICE-2b | `bestiary-hf4` | ModelInfo NormalizedX fields + codegen splice |
| SLICE-3 | `bestiary-99m` | ModelRef refactor + canonical + designation + Resolve |
| SLICE-4 | `bestiary-ktj` | Model_* constants generation |
| SLICE-5 | `bestiary-est` | SQLite v2→v3 migration + QueryByCanonical |
| SLICE-6b | — | bestiary-gen --cache-dir + --no-fetch |
| URD | `bestiary-rjf` | User requirements document (R10 normalization epoch) |
| PROPOSAL-3 | `bestiary-1oq` | Architecture proposal |
