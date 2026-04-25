# parse/data — data-only directory

This directory contains **JSON data files** that are embedded into the root
`bestiary` package at compile time via `//go:embed parse/data/*.json`.

## Important: no Go source files here

The directory is named `parse/` for readability, but it is **not** a Go
sub-package. Do not place `.go` files under `parse/` or `parse/data/`:

- Any `.go` file here would create a new `parse` package.
- The root `bestiary` package embeds from this directory; a sub-package under
  `parse/` that imports `bestiary` (directly or transitively) would create an
  import cycle.

The naming was explicitly chosen and ratified during design review. If you need
logic that processes these files, place it in the root `bestiary` package
(e.g. `parse.go`) alongside the `//go:embed` directive.

## Files

| File | Purpose |
|------|---------|
| `family_overrides.json` | Explicit `(raw_family → {family, variant})` mappings. Takes priority over pattern matching. |
| `variant_suffixes.json` | Suffix strings stripped to identify variants when no override or pattern matches. Order in the file is irrelevant — `initParseData()` re-sorts by length at runtime. |
| `version_patterns.json` | Named regex patterns for versioned-variant decomposition (v-prefix, k-prefix, m-prefix, hyphen-version, no-prefix). |
