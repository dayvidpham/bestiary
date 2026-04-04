# bestiary

Go module and CLI for querying AI model metadata from [models.dev](https://models.dev).

Provides strongly-typed providers, model IDs, a static model registry (~110 models), an HTTP client with retry, and a local SQLite cache for offline queries.

## Install

**As a Go library:**

```sh
go get github.com/dayvidpham/bestiary
```

**As a CLI tool:**

```sh
go install github.com/dayvidpham/bestiary/cmd/bestiary@latest
```

Requires Go 1.24+. Builds with `CGO_ENABLED=0` (no C compiler needed).

## Library usage

```go
package main

import (
    "fmt"
    "github.com/dayvidpham/bestiary"
)

func main() {
    // Static registry (compiled-in, no network)
    models := bestiary.StaticModels()
    fmt.Printf("%d models available\n", len(models))

    // Lookup by ID
    if m, ok := bestiary.LookupModel("claude-opus-4-6"); ok {
        fmt.Printf("%s: %s (%s, %d ctx)\n",
            m.ID, m.DisplayName, m.Provider, m.ContextWindow)
    }

    // Filter by provider
    for _, m := range bestiary.ModelsByProvider(bestiary.ProviderAnthropic) {
        fmt.Println(m.ID)
    }

    // Filter by family
    for _, m := range bestiary.ModelsByFamily("claude-opus") {
        fmt.Println(m.ID)
    }
}
```

**Fetching live data:**

```go
ctx := context.Background()
client := bestiary.NewClient(
    bestiary.WithTimeout(10 * time.Second),
    bestiary.WithRetries(3),
)

models, err := client.FetchModels(ctx)
// or filter by provider:
models, err := client.FetchModelsByProvider(ctx, bestiary.ProviderGoogle)
```

## CLI

```
bestiary <list|show|sync> [flags]
```

### Commands

**list** -- Query models from the static registry + local cache (offline).

```sh
bestiary list                                    # all models, JSON
bestiary list --provider anthropic --format table  # Anthropic models, table
bestiary list --format yaml                      # all models, YAML
```

**show** -- Show a single model by ID (offline).

```sh
bestiary show claude-opus-4-6 --format json
bestiary show gemini-2.0-flash --format table
```

**sync** -- Fetch models from the models.dev API and cache locally (online).

```sh
bestiary sync                                    # fetch all, print JSON
bestiary sync --provider anthropic --format table  # fetch Anthropic, table
bestiary sync --provider google --format yaml    # fetch Google, YAML
```

After syncing, `list` and `show` merge static + cached data. When both sources have the same model, the most recently synced version wins.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--provider` | (all) | Filter by provider slug (e.g. `anthropic`, `google`, `openai`) |
| `--format` | `json` | Output format: `json`, `yaml`, `table` |
| `--db-path` | `$XDG_CACHE_HOME/bestiary/models.db` | SQLite cache location |

### Example output

```
$ bestiary list --provider anthropic --format table

ID                          Provider   Family          Context  MaxOutput  Reason  Tools  CostIn/MTok
--------------------------  ---------  --------------  -------  ---------  ------  -----  -----------
claude-3-5-haiku-20241022   anthropic  claude-haiku    200000       8192      no    yes        $0.80
claude-opus-4-6             anthropic  claude-opus    1000000     128000     yes    yes        $5.00
claude-sonnet-4-6           anthropic  claude-sonnet  1000000      64000     yes    yes        $3.00
...
```

## Types

| Type | Description |
|------|-------------|
| `Provider` | String type with well-known constants (`ProviderAnthropic`, `ProviderGoogle`, `ProviderOpenAI`, `ProviderLocal`). Any models.dev provider slug is valid. |
| `ModelID` | String type for model identifiers (e.g. `"claude-opus-4-6"`). |
| `ModelInfo` | Canonical model metadata: all 17 API fields including capabilities, pricing, modalities. |
| `Modality` | Int enum: `ModalityText`, `ModalityImage`, `ModalityPDF`, `ModalityAudio`, `ModalityVideo`. |
| `Modalities` | `Input []Modality` and `Output []Modality`. |
| `Capability` | `Supported bool` + `Config map[string]string` for polymorphic fields (e.g. `Interleaved`). |

## Schema versioning

bestiary tracks two schema versions (see `version.go`):

- **BestiarySchemaVersion** -- Semver for bestiary's public Go types. The JSON Schema (`bestiary.schema.json`) documents this contract.
- **UpstreamSchemaVersion** -- `<YYYY.MM.DD>-<sha256>` pinning the models.dev schema snapshot bestiary was built against.
- **UpstreamGitCommit** / **UpstreamGitRemote** -- Git provenance for the upstream schema source.

## Updating static data

The static model registry is code-generated from the models.dev API:

```sh
go generate ./...
```

This runs `cmd/bestiary-gen` which fetches `https://models.dev/api.json`, filters to anthropic/google/openai, and writes `models_static_gen.go`.

## Dependencies

- Go 1.24+
- `zombiezen.com/go/sqlite` (CGO-free SQLite driver)
- No other external dependencies

## License

MIT
