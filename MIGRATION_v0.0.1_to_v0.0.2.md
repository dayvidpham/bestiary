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
2. [ModelInfo new fields: Family, Variant, Version, Date (formerly NormalizedFamily etc.)](#2-modelinfo-new-fields)
3. [ModelRef shape: 7 fields (added ID + Version)](#3-modelref-shape-7-fields)
4. [NEW types: CanonicalScheme, Designation, AcceptabilityRating](#4-new-types)
5. [NEW Resolve API + ErrAmbiguous error type](#5-new-resolve-api)
6. [NEW parse package: ParseFamily, ExtractDate, InferFamilyFromID](#6-new-parse-package)
7. [SQLite v2→v3: column renames + idx_canonical index](#7-sqlite-v2v3-migration)
7b. [SQLite v3→v4: version column + idx_canonical rebuild](#7b-sqlite-v3v4-version-column--idx_canonical-rebuild)
8. [NEW Model_* constants (ModelIDs function)](#8-new-model_-constants)
9. [CLI: bestiary show --scheme flag; bestiary-gen --cache-dir and --no-fetch](#9-cli-changes)
10. [BREAKING: ModelInfo JSON field rename (drop Normalized prefix)](#10-modelinfo-json-field-rename)
11. [Resolve overhaul (PURL loose-fallback, ErrAmbiguous grouping, --format flag, canonical-provider preference)](#11-resolve-overhaul)
12. [Parse-failure audit log (.bestiary-gen-cache/parse_failures.json)](#12-parse-failure-audit-log)
12. [Parse-failure audit log (.bestiary-gen-cache/parse_failures.json)](#12-parse-failure-audit-log-bestiary-gen-cacheparsefailuresjson)

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

**Nature:** Additive (fields added in SLICE-FIX-1); then field rename (SLICE-FIX-V2-1, see Section 10 for breaking change details).

Five codegen-baked normalization fields are present in `ModelInfo` (`bestiary.go`).
They are populated at code-generation time by `cmd/bestiary-gen` invoking
`parse.ParseFamilyWithVersion`, `parse.ExtractDate`, `parse.InferFamilyFromID`,
and (since SLICE-FIX-1 cycle 2) `parse.ExtractVersionFromID`.
They are zero-value (`""`) for models loaded from a pre-v4 SQLite cache until a
`bestiary sync` is performed.

**Before (original ModelInfo, before SLICE-FIX-1):**
```go
type ModelInfo struct {
    ID          ModelID
    Provider    Provider
    DisplayName string
    Family      Family  // raw API family field (e.g. "claude-opus")
    ContextWindow int
    // ... remaining fields
}
```

**After SLICE-FIX-1 + SLICE-FIX-V2-1 (current shape):**
```go
type ModelInfo struct {
    ID          ModelID
    Provider    Provider
    DisplayName string
    RawFamily   Family  // raw API family field verbatim (e.g. "claude-opus")

    // Codegen-baked normalization (SLICE-FIX-1, renamed in SLICE-FIX-V2-1)
    Family  Family  // canonical family (e.g. "claude")
    Variant string  // variant suffix (e.g. "opus", "sonnet")
    Version string  // model version (e.g. "4.5", "4.6", "2.5"); see note below
    Date    string  // YYYY-MM-DD date from model ID or ReleaseDate

    ContextWindow int
    // ... remaining fields unchanged
}
```

**NOTE on Version:** The version is extracted from the model ID
(e.g. `"claude-opus-4-5-20251101"` → `"4.5"`) because the upstream models.dev
API family strings do not embed version numbers (`"claude-opus"` not
`"claude-opus-4-5"`). After SLICE-FIX-1 cycle 2, Version is populated
for approximately 636 of 4325 static models. Models whose IDs carry no separable
version component will have `Version: ""`.

**JSON wire impact (current):**
```json
{
  "RawFamily": "claude-opus",
  "Family":    "claude",
  "Variant":   "opus",
  "Version":   "4.5",
  "Date":      "2025-11-01"
}
```

All five fields are declared `required` in `bestiary.schema.json`.

**Fix-up steps:**
1. Update any JSON deserialization struct definitions to include all five fields with new names.
2. Update any JSON Schema validators — replace `NormalizedFamily/Variant/Version/Date` with
   `Family/Variant/Version/Date`, and replace the old `Family` property with `RawFamily`.
3. For models loaded from a pre-v4 SQLite cache, `Family/Variant/Version/Date`
   will be empty strings until `bestiary sync` is re-run (see Section 7b for v3→v4 migration; Section 7 for v2→v3).
4. See Section 10 for the full breaking-change details of the field rename.

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
1. Update any `ModelRef` struct literals — add `ID: m.ID` and `Version: m.Version`
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
generation time to bake `Family/Variant/Date` into
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

## 8. NEW Model__* constants (ModelIDs function)

**Nature:** Additive; generated constants file `models_constants_gen.go`.

`cmd/bestiary-gen` now emits a `Model__*` constant for every model in the static
registry. Constants follow the naming pattern:

```
Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
```

**Double underscores** separate top-level components (Provider, Family, Variant,
Version, Date). **Single underscores** appear only *within* a component — for
example, version `4.5` is encoded as `4_5`, preserving the dot-to-underscore
conversion inside the version segment.

where each component uses the same casing as the `Provider` and `Family` types
(PascalCase preserved). Casing overrides include `chatgpt → ChatGPT`, `gpt → GPT`,
`ai → AI`, and others.

**Examples:**
```go
const (
    // Anthropic claude-opus-4-5-20251101: family=Claude, variant=Opus,
    // version=4_5 (from Version "4.5"), date=20251101.
    Model__Anthropic__Claude__Opus__4_5__20251101 ModelID = "claude-opus-4-5-20251101"

    // OpenAI chatgpt-4o-latest: ChatGPT casing via casingOverrides entry.
    Model__OpenAI__ChatGPT__4o__Latest           ModelID = "chatgpt-4o-latest"

    // Google gemini-2.0-flash: no version or date in ID.
    Model__Google__Gemini__2__0__Flash           ModelID = "gemini-2.0-flash"

    // OpenAI gpt-4o: no date.
    Model__OpenAI__GPT__4o                       ModelID = "gpt-4o"
)
```

`ModelIDs() []ModelID` returns a defensive copy of all constant values (the
name differs from the PROPOSAL-3 spec `Models()` to avoid a clash with the
existing `registry.go:StaticModels()` function).

**Important:** `Provider`, `Family`, and `Harness` type constants are **not
renamed**. PascalCase is preserved — no breaking change to existing constant
references.

**`bestiary-gen` flag parser:** Both single-hyphen (`-flag`) and double-hyphen
(`--flag`) forms are accepted for all flags: `--no-fetch`, `--cache-dir`,
`--only-providers`, `--all-providers-except`.

**Fix-up steps:**
- These are new exports; no existing API changes.
- Update `go generate ./...` to regenerate `models_constants_gen.go` after any
  upstream API update (`bestiary-gen` fetches the current model list).
- Do not edit `models_constants_gen.go` by hand — it is owned by `cmd/bestiary-gen`.
- If you referenced `Model_Anthropic_*` constants from a previous version, update
  to the new `Model__Anthropic__*` form (double underscores between every segment).

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

## 10. BREAKING: ModelInfo JSON field rename (drop Normalized prefix)

**Nature:** Breaking; JSON wire format change.

**Audit trail:** FIX_IMPL_PLAN_V2 `bestiary-2xaf`, SLICE-FIX-V2-1 `bestiary-tel4`.

### What changed

In SLICE-FIX-1 (the initial normalization pipeline), `ModelInfo` was given five
codegen-baked fields. Four were named with a `Normalized` prefix, and the raw API
`Family` field kept its old name:

```go
// BEFORE (SLICE-FIX-1 shape — obsolete)
type ModelInfo struct {
    // ...
    Family           Family  // raw API value (e.g. "claude-opus")
    NormalizedFamily Family  // canonical (e.g. "claude")
    NormalizedVariant string  // variant suffix (e.g. "opus")
    NormalizedVersion string  // model version (e.g. "4.5")
    NormalizedDate   string  // YYYY-MM-DD
    // ...
}
```

In SLICE-FIX-V2-1 the prefix was dropped and the name collision resolved by
renaming the raw field to `RawFamily`:

```go
// AFTER (current shape)
type ModelInfo struct {
    // ...
    RawFamily Family  // raw API family field verbatim (e.g. "claude-opus")
    Family    Family  // canonical family (e.g. "claude")
    Variant   string  // variant suffix (e.g. "opus")
    Version   string  // model version (e.g. "4.5")
    Date      string  // YYYY-MM-DD
    // ...
}
```

### JSON wire impact

The JSON key names changed accordingly. `bestiary list --format=json` and
`bestiary show --format=json` output will now emit:

**Before (obsolete wire format):**
```json
{
  "Family":           "claude-opus",
  "NormalizedFamily": "claude",
  "NormalizedVariant": "opus",
  "NormalizedVersion": "4.5",
  "NormalizedDate":   "2025-11-01"
}
```

**After (current wire format):**
```json
{
  "RawFamily": "claude-opus",
  "Family":    "claude",
  "Variant":   "opus",
  "Version":   "4.5",
  "Date":      "2025-11-01"
}
```

All five fields remain `required` in `bestiary.schema.json`.

### SQL columns (unchanged)

The underlying SQLite column names were **not** changed. They were already correct
from the v2→v3 migration (Section 7) and remain:

| Column | Maps to Go field |
|---|---|
| `raw_family` | `RawFamily` |
| `family` | `Family` |
| `variant` | `Variant` |
| `version` | `Version` |
| `date` | `Date` |

No SQLite migration is required for this change.

### Fix-up steps for consumers

1. **Go callers using `ModelInfo` struct literals:** rename the five fields in any
   struct literal or field access:
   - `m.Family` (raw semantics) → `m.RawFamily`
   - `m.NormalizedFamily` → `m.Family`
   - `m.NormalizedVariant` → `m.Variant`
   - `m.NormalizedVersion` → `m.Version`
   - `m.NormalizedDate` → `m.Date`

2. **Go callers using `ModelsByFamily` or `ProvidersForFamily`:** these functions
   match on the raw API family value. Pass `RawFamily` values (e.g. `"claude-opus"`)
   as before — the function signature is unchanged.

3. **JSON consumers (downstream scripts, APIs, tests):** update any JSON key
   references from the old names to the new names shown in the wire format table
   above. There is no backward-compatibility shim; the change is a hard rename.

4. **JSON Schema validators:** replace the five property names in any schema
   definition that mirrors `bestiary.schema.json`. The `$schema` URI and `version`
   field in `bestiary.schema.json` have been updated to `"0.0.2"` (see Section 1).

5. **`ModelRef` consumers:** `ModelRef` already used the correct names
   (`RawFamily`, `Family`, `Variant`, `Version`, `Date`) prior to this rename.
   No change needed for callers that only access `ModelRef`.

---

## 11. Resolve overhaul (PURL loose-fallback, ErrAmbiguous grouping, --format flag, canonical-provider preference)

**Nature:** Breaking (CLI flag rename `--format` → `--output` for output; new `--format` for input). Behavioral change (canonical-provider preference, ErrAmbiguous output format).

**Slice:** SLICE-FIX-V2-2 (`bestiary-z2u7`)

**Audit trail:** FIX_IMPL_PLAN_V2 `bestiary-2xaf`, UAT-3 `bestiary-g9ci` (Component 3).

### 11.1 CLI flag rename: output `--format` → `--output`

The `bestiary` CLI had a naming collision: `--format` was used for both OUTPUT format (json/yaml/table) and (proposed) INPUT scheme selection. To use `--format` for input (as the user requested), the output flag was renamed.

**Before (v0.0.1):**
```
bestiary list --format=json
bestiary show <id> --format=yaml
bestiary sync --format=table
```

**After (v0.0.2):**
```
bestiary list --output=json        # renamed: --format → --output
bestiary show <id> --output=yaml
bestiary sync --output=table
```

The default output format remains `json` — scripts that rely on the default behavior are unaffected.

**Fix-up steps:**
- Replace `--format <json|yaml|table>` with `--output <json|yaml|table>` in all scripts and CI workflows.
- The `--format` flag now selects the INPUT scheme (see Section 11.2).

### 11.2 New `--format` input flag for `bestiary show`

`bestiary show` now defaults to canonical/peasant form ONLY. The previous auto-detect behavior (PURL via `pkg:` prefix, HuggingFace via `provider/id` form) is no longer applied by default.

**Default behavior (v0.0.2):** `bestiary show <input>` treats `<input>` as a bestiary canonical form:
```
[<provider>/]<family>[/<variant>[/<version>]][@<date>]
```

To use other input formats, specify `--format`:

| `--format` value | Input form accepted | Example |
|---|---|---|
| `peasant` (default) | Bestiary canonical | `claude/opus@2025-05-14` |
| `huggingface` or `hf` | HuggingFace Hub form | `anthropic/claude-opus-4-20250514` |
| `purl` | Package URL (PURL) | `pkg:huggingface/anthropic/claude-opus-4-20250514` |
| `raw` | Exact API model ID | `claude-opus-4-20250514` |

**Breaking changes for existing scripts:**
- `bestiary show pkg:huggingface/... ` → must add `--format purl`
- `bestiary show anthropic/<raw-id>` (HuggingFace form) → must add `--format huggingface` or `--format hf`
- `bestiary show <raw-id>` (exact ID without slashes) → still works in peasant mode, but bare exact IDs that match canonical family names may now trigger ErrAmbiguous instead of ErrNotFound.

**Legacy `--scheme` flag:** deprecated. Still accepted for backward compatibility. When `--format` is the default (peasant) and `--scheme` is set, the legacy `--scheme` value is honoured. `--format` takes precedence when explicitly set.

**Fix-up steps:**
1. Replace `bestiary show <purl>` with `bestiary show --format purl <purl>`.
2. Replace `bestiary show <provider>/<raw-id>` with `bestiary show --format hf <provider>/<raw-id>`.
3. Replace `bestiary show <raw-id>` (exact model ID) with `bestiary show --format raw <raw-id>` for unambiguous exact-ID lookup.
4. Update any `--scheme` usages to `--format` equivalents:
   - `--scheme canonical` → `--format peasant`
   - `--scheme huggingface` → `--format huggingface`
   - `--scheme purl` → `--format purl`
   - `--scheme raw` → `--format raw`

### 11.3 PURL loose-match fallback

When a PURL input (`pkg:huggingface/<namespace>/<id>`) has zero matches in the specified namespace (provider), Resolve now falls back to an all-provider search instead of returning `ErrNotFound`.

**Before (v0.0.1):**
```
Resolve("pkg:huggingface/unknown-ns/claude-opus-4-5") → ErrNotFound
```

**After (v0.0.2):**
```
Resolve("pkg:huggingface/unknown-ns/claude-opus-4-5") → *ErrAmbiguous
  Note: no matches in namespace "unknown-ns" — performing loose match across all providers
  Candidates: all providers that host claude-opus-4-5
```

The `ErrAmbiguous` struct gains a `PURLMissedNamespace string` field that carries the namespace that missed (empty for non-PURL ambiguity). The `ErrAmbiguous.Error()` message includes the diagnostic when set.

**Fix-up steps:**
- Callers that `errors.As(err, &ambig)` for PURL inputs may now receive `ErrAmbiguous` instead of `ErrNotFound`. Update any `ErrNotFound`-only handling paths to also check for `ErrAmbiguous` with a non-empty `PURLMissedNamespace`.

### 11.4 ErrAmbiguous output: grouping + truncation

`FormatAmbiguous` (written to stderr by `bestiary show` on ambiguous input) now:
1. Groups candidates by `(Family, Variant, Version)` tuple — one row per group. Collapses multiple providers hosting the same model into a single canonical row.
2. Truncates after N=10 groups with a `+M more` hint.

**Before (v0.0.1):** All matching ModelRefs shown, one row per provider (17+ rows for popular models).

**After (v0.0.2):** One row per distinct `(Family, Variant, Version)` group, at most 10 rows shown.

The footer hint was also updated from `--scheme=raw` to `--format=raw`.

**Fix-up steps:**
- Scripts that parse the `FormatAmbiguous` output (stderr) may need to be updated to handle the new format (grouped + truncated).
- The `ErrAmbiguous.Candidates` slice still contains all matching candidates (ungrouped, untruncated) — use the struct field for programmatic access if needed.

### 11.5 Canonical-provider preference in Resolve

When a canonical-form input (`family/variant@date`) matches multiple providers (cross-provider hosting), Resolve now prefers the originating canonical provider over re-hosts.

**Before (v0.0.1):**
```
Resolve("claude/opus@2025-05-14") → [302ai, anthropic, azure, ...] (alphabetical order, first used)
```

**After (v0.0.2):**
```
Resolve("claude/opus@2025-05-14") → [anthropic] (canonical provider preferred)
```

The preference is implemented via `Family.CanonicalProvider() Provider` in `family.go`. Well-known mappings:

| Family | Canonical Provider |
|---|---|
| `claude`, `claude-opus`, `claude-sonnet`, `claude-haiku` | `anthropic` |
| `gemini`, `gemma` | `google` |
| `gpt`, `o` (includes o1, o3, o4) | `openai` |
| `llama` | `local` |
| `mistral`, `codestral`, `devstral` | `mistral` |
| `deepseek` | `deepseek` |
| `qwen` | `alibaba` |
| All others | `""` (empty) → falls back to ErrAmbiguous |

The mapping for unrecognized families will be reviewed in followup `bestiary-1wy7`.

**Applies only to:** SchemeCanonical (peasant form) non-exact-ID inputs. Raw/HuggingFace/PURL forms and exact-ID lookups are unaffected.

**Fix-up steps:**
- Callers that iterated all cross-provider refs and selected the first may now receive a single ref (the canonical provider only). Update iteration logic if all-provider refs are needed (use `WithScheme(SchemeRaw)` or `WithInputFormat(InputFormatRaw)` for exact-ID all-provider lookups).
- `Family.CanonicalProvider()` is a new exported method — no breaking change.
- `ErrAmbiguous.PURLMissedNamespace` is a new field — no breaking change for existing code.

---

## 12. Parse-failure audit log (`.bestiary-gen-cache/parse_failures.json`)

**Nature:** Additive. New diagnostic output file written at codegen time. No API changes.

**Slice:** SLICE-FIX-V2-3 (`bestiary-wcvo`)

### What changed

`bestiary-gen` now writes a parse-failure audit log to
`.bestiary-gen-cache/parse_failures.json` at the end of every codegen run (i.e.
every `go generate ./...`). The file is **overwritten** on each run — it is a
full audit of the current generation, not an append log.

### File envelope (schema_version: 1)

```json
{
  "schema_version": 1,
  "generated_at": "2026-04-26T05:18:25Z",
  "failure_count": 2,
  "failures": [
    {
      "raw_id": "claude-3-5-haiku-20241022",
      "provider": "anthropic",
      "raw_family": "claude-haiku",
      "attempted_parse": {
        "family": "claude",
        "variant": "haiku",
        "version": "",
        "date": "2024-10-22"
      },
      "reason": "version digits between family-prefix and variant not extracted"
    }
  ]
}
```

When zero failures occur, the file is still written with `failure_count: 0` and
`failures: []`. It is never omitted.

### Failure modes detected

Three known failure modes are currently detected:

| Reason string | Description | Example |
|---|---|---|
| `"version digits between family-prefix and variant not extracted"` | The model ID contains version digits between the family prefix and variant name, but `ExtractVersionFromID` cannot reach them (e.g. `claude-haiku` prefix doesn't align with `claude-3-5-haiku-...` ID). | `claude-3-5-haiku-20241022` with `raw_family="claude-haiku"` |
| `"suffix overflow: extra segments after expected family/variant/version/date"` | More than 2 hyphen-separated segments remain in the raw family string after the parser accounts for the expected components. | Model IDs with unusual extra segments |
| `"YYMM-date-as-version false-positive"` | The raw family string contains a 4-digit numeric segment in the YYMM range (1900–2999) that the parser cannot reliably classify as a date vs. a version. | `mistral-2401`, `mistral-2403`, `pixtral-2411` |

### How to use the audit log

After running `go generate ./...`, inspect the file to identify models whose
canonical decomposition (`Family`, `Variant`, `Version`) may be inaccurate:

```bash
# Generate and inspect failures
go generate ./...
cat .bestiary-gen-cache/parse_failures.json | python3 -m json.tool

# Count failures by reason
cat .bestiary-gen-cache/parse_failures.json | \
  python3 -c "import json,sys; d=json.load(sys.stdin); \
  [print(f['reason']) for f in d['failures']]" | sort | uniq -c
```

The parse failures file is gitignored via `.bestiary-gen-cache/` in `.gitignore`.
It is a build-time diagnostic and should not be committed.

### API additions

New public types in `parse.go`:

```go
type ParseAttempt struct {
    Family  Family `json:"family"`
    Variant string `json:"variant"`
    Version string `json:"version"`
    Date    string `json:"date"`
}

type ParseFailure struct {
    RawID          ModelID      `json:"raw_id"`
    Provider       Provider     `json:"provider"`
    RawFamily      Family       `json:"raw_family"`
    AttemptedParse ParseAttempt `json:"attempted_parse"`
    Reason         string       `json:"reason"`
}

type ParseFailuresEnvelope struct {
    SchemaVersion int            `json:"schema_version"`
    GeneratedAt   time.Time      `json:"generated_at"`
    FailureCount  int            `json:"failure_count"`
    Failures      []ParseFailure `json:"failures"`
}

// New reason constants (use these for consistent phrasing):
const ReasonVersionDigitsNotExtracted = "version digits between family-prefix and variant not extracted"
const ReasonSuffixOverflow = "suffix overflow: extra segments after expected family/variant/version/date"
const ReasonYYMMDateAsVersion = "YYMM-date-as-version false-positive"

// New entry point (failure-aware companion to ParseFamilyWithVersion):
func ParseFamilyDetailed(raw Family, id ModelID, p Provider) (Family, string, string, *ParseFailure)
```

### Downstream impact

- No changes to `ModelInfo`, `ModelRef`, or any existing public types.
- No schema version bump (additive diagnostic output only).
- `.bestiary-gen-cache/parse_failures.json` is already gitignored.
- Callers of `go generate ./...` will see new output: `N parse failures logged to .bestiary-gen-cache/parse_failures.json`.

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
| FIX_IMPL_PLAN_V2 | `bestiary-2xaf` | Fix plan: drop Normalized prefix, reconcile ModelInfo fields |
| SLICE-FIX-V2-1 | `bestiary-tel4` | ModelInfo JSON field rename (this section's slice) |
| SLICE-FIX-V2-3 | `bestiary-wcvo` | Parse-failure audit log (Section 12) |
