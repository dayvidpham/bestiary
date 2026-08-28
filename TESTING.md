# Testing bestiary — the case-corpus standard

bestiary's parse/entity/quantization/VRAM suites are dominated by
**table-driven tests**: a fixed, hand-authored list of `{input, expected}` rows
driven through the production API. Historically each such table was an inline
`[]struct{...}` literal inside the `_test.go` file. This document describes the
**canonical corpus standard** those tables migrate onto, and the discipline that
keeps a migration behavior-preserving.

The standard mirrors the peasant-labs/schema `testcase` package, with one
deliberate adaptation to bestiary's dependency discipline (stdlib +
`zombiezen/sqlite` only): **corpora are JSON, decoded with `encoding/json`**,
never YAML. No external dependency is introduced.

## What is (and is not) a corpus

A **fixture corpus is for a FIXED, authored case list** — a table of
`{input, expected}` rows a human wrote down because each row pins a specific
real-world fact (a catalog id, a ruling on how a token classifies, an
arithmetic derivation). These extract to JSON.

A test that **derives its rows from the built catalog** (a census sweep over
`StaticModels()`, a property test, a regen/mutation fence, the codegen golden
machinery) stays inline Go code: its "cases" are not authored, they are computed
from live data, so there is nothing fixed to serialize.

## The three packages

| Package | Import | Carries `testing`? | Role |
|---|---|---|---|
| `testcase` | `github.com/dayvidpham/bestiary/testcase` | No | Pure data: generic `Case[I,E]`/`Corpus[I,E]`, closed-set `Classification`/`ProvenanceSource`, the JSON `LoadCorpus`, and the pure validators (`CheckMin`, `Validate`). |
| `testcase/assert` | `github.com/dayvidpham/bestiary/testcase/assert` | Yes | The `*testing.T` seam: `RequireMin` (wraps `CheckMin`) and `RequireValid` (wraps `Corpus.Validate`). |
| the corpus JSON | `testdata/<area>/*.json` | — | The authored rows, embedded into the **test binary** via `//go:embed` vars in a per-area `fixtures_<area>_test.go`. |

The pure/`testing` split is why `testcase` never drags `testing` into anything
that imports it, and why the size/validity logic can be negative-tested without
a `*testing.T` (see `testcase/testcase_test.go`).

### Why the embeds live in `_test.go` files, not a production `fixtures.go`

These corpora are test inputs; embedding them into the production `bestiary`
package (or the CLI binary) would bloat the shipped artifact with test data. So
each area's `//go:embed` vars live in a `fixtures_<area>_test.go` file, which
compiles the corpus **only** into that package's test binary. (One `_test.go`
embed file per area, rather than a single shared one, also keeps parallel edits
to different areas conflict-free.)

## The corpus schema

Each JSON file is a `{"cases": [...]}` document. Every case carries:

- `name` — a stable subtest name (derived from the old table's `name`/comment);
- `input` — the input, of the corpus's `I` type (a string, or a small struct);
- `expected` — the expected output, of the corpus's `E` type;
- `classification` — closed set: `must-pass` (the SUT must accept) or
  `must-fail` (the SUT must reject). Maps naturally onto the old Valid/Invalid
  table split;
- `provenance.source` — closed set: `requirement` (a BDD/spec clause), `bug`
  (a regression or coverage-gap audit finding), `enum` (generated from a closed
  enum or an authoritative curated table), `boundary` (an edge of a range or
  format), `manual` (hand-authored: a real catalog id it pins, or a
  hand-computed arithmetic derivation);
- `provenance.ref` — the concrete fact the case pins. **This is where the old
  inline comment goes.** For a hand-computed VRAM/anchor literal, the arithmetic
  derivation comment moves here verbatim — it *is* the provenance;
- `mutation.description` — the single change the case embodies (for a must-fail
  case, the mutation that makes a valid input invalid), so no case is vacuous.

`Corpus.Validate` rejects any case missing an in-set classification/source, a
non-empty `ref`, or a non-empty `description`.

## Migration safety (non-negotiable per migrated corpus)

A migration must be **behavior-preserving**: the same assertions run over the
same cases, just loaded from JSON. Three guards enforce this:

1. **Exact count control.** `if got := len(corpus.Cases); got != N { … }`, where
   `N` is the pre-migration inline row count. A min-floor would pass a silent
   drop that stays above the floor; the exact count catches a drop *or* a stray
   add. State each `N` in the migration commit message so a reviewer can
   diff-verify against the old table.
2. **Value-based coverage.** An exact count cannot see a **count-preserving
   swap** (drop a real case, add a filler, count unchanged). A lean check that
   the load-bearing inputs are still present *by value* catches it.
3. **Non-vacuity.** `corpus.Validate()` (or `assert.RequireValid`) asserts every
   case carries classification + provenance + mutation.

For a **growable** corpus (new rows expected over time) use
`assert.RequireMin(t, corpus, floor)` instead of the exact count.

## The idiom (worked shape)

```go
// fixtures_<area>_test.go
//go:embed testdata/<area>/<name>_corpus.json
var <name>CorpusJSON []byte

// <name>_test.go
type <name>Input struct {
    Raw string `json:"raw"`
}
type <name>Expected struct {
    Family  string `json:"family"`
    Variant string `json:"variant"`
}

func Test<Name>(t *testing.T) {
    corpus, err := testcase.LoadCorpus[<name>Input, <name>Expected](<name>CorpusJSON)
    if err != nil {
        t.Fatalf("load <name> corpus: %v", err)
    }
    if got := len(corpus.Cases); got != N { // exact count control
        t.Fatalf("<name> corpus has %d cases, want exactly N", got)
    }
    if err := corpus.Validate(); err != nil { // non-vacuity
        t.Fatalf("<name> corpus is under-populated: %v", err)
    }
    // value coverage: assert the load-bearing inputs are still present by value
    for _, c := range corpus.Cases {
        t.Run(c.Name, func(t *testing.T) {
            // drive the production API over c.Input, assert against c.Expected
            // and c.Classification
        })
    }
}
```

## The corpus census, and what is deliberately still inline

At the close of the naming-layer epoch the repository carries **128 corpora**:

| Area | Corpora | What lives there |
|---|---|---|
| `testdata/parse/` | 50 | family/variant/version decomposition, modifier extraction, param-size tokens, serving-host capture, the `ReasonUnknownSuffixOverflow` reachability capture, the vercel family-`o` over-capture (with its o-series slash negative controls + the dashed openai-o convergence rows), the "p"-as-dot version decode (unit shapes + every digit-p-digit catalog id), the dot-lost version repair (dotless + dash-glued qwen/minimax/mistral spellings), the v0.2.8 curation repairs (the deepseek variant-encoded dash-glued dot-lost merges + the Command A Translate identity-modifier split) and the `1t` trillion param-size routing (with the kimi-k2:1t / r1t2 negative controls), and the leading-token classification that decides whether an id's first token repeats a fact another axis carries or is the only place it appears (with the backend-host, product-namespace, constant-family-prefix and destroyed-remainder negative controls, and the one-family minimal pair that makes the creator rule's carrier check — the remainder's family must declare the very lab the token names — falsifiable without leaning on a downstream curated fixture), and the compound series-family recovery that reduces a BARE-INTEGER series compound on the raw-populated path (unit shapes + the catalog rows they re-key, with the curated genuine-compound self-map, wrong/absent series-letter and extra-token negative controls, and the control that an under-specified raw family never asserts a version the model ID does not state), and the GH#43 parser-conformance record: every string the top-traffic-lab sweep cites or measures, pinned to the entity key the production path produces for it TODAY, each with a closed-set verdict (conforming / defect / undecided) and, beside every confirmed defect, the conforming CONTROL that isolates its cause (the dotted version WITHOUT the leading `v`, the slash-separated `zai` namespace against the dash-glued `z-ai` one, the single-dash vendor prefix against the doubled one). An `undecided` case carries the `EXPECTED_TBD` marker instead of a want-key, because the sweep reports and never rules. The catalog-wide seed-token census that produced these rows is NOT a corpus and stays inline in `gh43_conformance_internal_test.go`: its rows are computed from the vendored catalog, not authored |
| `testdata/enum/` | 14 | closed-enum `String()`, `Parse*`, and JSON round-trip/reject tables (Modality, AcceptabilityRating, CanonicalScheme, Harness, Family, Provider) |
| `testdata/quant/` | 14 | the Quantization enum surface plus the curated quant/VRAM lookups |
| `testdata/entity/` | 8 | `EntityRef.String()` grammar, the llama-4 and laguna entity projections, Series/Release rendering, the suppression per-entry fence |
| `testdata/stage/` | 4 | the ReleaseStage axis |
| `testdata/midid/` | 3 | the internal mid-ID token engine |
| `testdata/metadata/` | 3 | the models.dev ingest detectors (status / link type / reasoning-option kind) |
| `testdata/resolve/` | 4 | the internal group-key helpers, the canonical-provider preference on provider-unqualified exact-ID lookups, and canonical segment binding at the peasant seam (the repaired variant-empty and provider-prefixed refs, the six must-not-widen falsifiers, the pinned entity-view guards, and the composition witness that is green only once BOTH the binding repair and the gpt tier re-key have landed) |
| `testdata/vram/` | 5 | the VRAM arithmetic anchors, plus the budget-first fit calculator: the context-boundary and bound-selection table, the derived-weights estimates with the six zero-bits-per-weight refusals, and the parameter-shape exclusion table (the NxM and Nb-Ke tokens that attest no total) |
| `cmd/bestiary/testdata/series/` | 4 | the `series` selector surface: end-to-end selector resolution (specificity ladder, canonical grammar, `--version`, `--input-format`, disagreement errors), the strict major-union membership rule, `--version` composition, and the `selectSeries` readings over a synthetic universe |
| `cmd/bestiary/testdata/retired/` | 8 | the retired-key migration records — six per-lever corpora, one per curation lever that retires entity keys (the global free-tier demotion, the ling/inkling/kling collision split, the mimo keyspace normalization, the cogito decomposition + variant pin, the gpt tier re-key + redundant leading-token strip, the series-compound family recovery), plus one release-wide corpus described below. Each case pins the two lookup seams for a retired key plus the instances it held and the live keys those instances re-homed onto, so the successor set is re-derived rather than claimed. The `show` seam is pinned PER KEY because it measurably has three outcomes, not one: not-found, the under-specified error when the family survives its bare key, and — recorded as a measured deviation — a successful resolution when the retired spelling remains a valid under-specified reference to exactly one live entity. `epoch_retired_keys_corpus.json` is NOT one of the per-lever records and does not follow their shape: it is the RELEASE-WIDE cumulative probe, taking every key present in the entity keyspace at the release baseline and absent at this tip (60 keys — the count is BASELINE-RELATIVE, so it can SHRINK: the 2026-08-28 catalog refresh gave `gpt/pro` and `ministral#8b{instruct}` real occupants again, un-retiring both, 62 -> 60) and pinning the outcome each one measurably has — a hard 404 at `EntityByKey` without exception, and a MEASURED split at the looser seams (7 keys answer `Resolve` with the typed under-specified error because the string keeps live children, and 2 still answer `show --by-entity`) rather than the two-outcome pin a per-lever corpus records. It carries its own exact-count control (the count IS the cumulative retired set, so it moves only with a measured key diff) and reconciles itself against the union of the per-lever corpora BY GLOB, so a new lever's corpus joins the reconciliation without being registered anywhere else. `refresh_2026_08_28_rekey_retired_keys_corpus.json` is the eighth file and is neither per-lever nor epoch-wide: it records the 19 keys the 2026-08-28 catalog refresh retired whose ARTIFACTS SURVIVED under a different key (the other 68 retirees are upstream deletions, for which a 404 is the whole truth and no migration record is owed). Its baseline is the PREVIOUS RELEASE bake rather than the epoch baseline, so most of its keys were minted after the epoch baseline and are correctly absent from the cumulative set; it is therefore declared out of the glob reconciliation by name, with its reason, in `nonEpochBaselineCorpora` (`epoch_retired_keys_test.go`), and that declaration is itself checked against the directory so a stale skip cannot silently drop a corpus. It re-derives each successor set from the key's own v0.2.10 instances, asserts every named successor is LIVE, and — because a refresh is not a conservation lever — requires any instance that re-homes nowhere to be declared in `departed_instances` |
| `cmd/bestiary/testdata/rehome/` | 1 | the instance-membership records for a curation lever that re-homes instances between keys that ALREADY EXIST, so no key is retired and no migration table applies. Each case names a live key and the exact set of provider instances it must hold, so a re-home stays distinguishable from a split and the instance total is checked as conserved |
| `cmd/bestiary-gen/testdata/gen/` | 9 | the identifier builders (`slugToIdentifier`, `providerConstName`, `styleSegment`, `entityConstName`, `splitComma`) |
| `cmd/bestiary-ollama/testdata/ollama/` | 1 | Ollama tag normalization |

### Committed baselines, which are NOT corpora

These are single committed artifacts, not case tables, so they are outside the count above.
They are listed here because the corpus map is where a curator looks first, and because each
one has an ownership rule that is easy to break by accident. `AGENTS.md`'s file-ownership
table is the authority; this is the index.

| File | Moves when | What breaks if it moves at the wrong time |
|---|---|---|
| `testdata/bake_identity_baseline.tsv` | ONLY when a release is tagged, moved by hand by the release engineer at the numbered bake-identity-baseline step of the release checklist in `AGENTS.md` | It is the PREVIOUS RELEASE's bake. Re-captured on a refresh, it freezes in any version loss the refresh itself introduced, and `TestBakeIdentity_NoUnjustifiedVersionLoss` then measures nothing. A stale one still PASSES, so nothing goes red to announce either mistake |
| `testdata/hf_archived_url_survivors.txt` | With a MEASURED harvest change, in the same commit | It is the pinned survivor set for `TestHFArchivedURL_NoErasureAmongSurvivors`. Edited to match a run, it stops naming the repo that lost its snapshot |
| `cmd/bestiary-gen/testdata/snapshot/decomp_baseline.tsv` | At a DECLARED capture point, via `BESTIARY_CAPTURE_BASELINE=1 go test ./cmd/bestiary-gen -run TestCaptureDecompositionBaseline` | It is the frozen BEFORE decomposition. Re-captured post-refactor, it masks the regressions it exists to catch |
| `cmd/bestiary-gen/testdata/snapshot/decomp_diff_report.json` | Under the same declared capture, in the commit that declares it | An ordinary run COMPARES it. A self-refreshing report agrees with every tree it is run on |

**Still inline, by the rule at the top of this document** — these are not stragglers,
they are tests whose "cases" are computed rather than authored, and extracting them
would be a category error:

- **census/property sweeps** over the built catalog (`StaticModels()` / `Entities()`
  walks, the injectivity guard, the empty-seed suppression sweep, the parse-failure
  audits, `TestHostSplit_EntityParity`, `TestVersionPresenceConsistency_ClassA`,
  `TestResolve_CanonicalProviderPreference_CatalogSweep`);
- **codegen determinism machinery** (`TestCodegen_Reproducible_ByteIdentical`,
  `TestCodegen_UpToDate`, the golden-excerpt fences, `TestDecompositionSnapshot`,
  and the upstream field-shape guards `TestModelsdevFieldCensus_NoDrift` /
  `_UpToDate` / `_EnvelopeContract`, whose rows are the catalog's own field paths);
- **store migration fixtures** (`store_migration_test.go`, `store_v5/v6/v7_test.go`) —
  the rows are *seed database state* for a migration, not `{input, expected}` cases;
- **graceful-degrade loader tables** (`TestSafe*_Degrades*`, `TestParse*_Rejects`) —
  each row is a distinct malformed *document*, and inlining keeps the corruption visible
  next to the assertion;
- **CLI argv/flag tables** (`cmd/bestiary/cli_warts_test.go`, and the
  `--version`/`--input-format` rejection tables in `series_cli_test.go`) — the rows are
  argv vectors exercising flag *mechanics* (a flag given without its required
  positional, or without a value), not data facts about the catalog. The selector→lines
  rows from the same file ARE data facts, and live in
  `cmd/bestiary/testdata/series/selector_resolution_corpus.json`;
- **`host_detect_test.go`'s curated-prefix table** — its rows are already pinned as data
  in `testdata/parse/azure_serving_host_corpus.json`; extracting the second copy would
  duplicate the same facts in two places.

## Running

```bash
GOWORK=off CGO_ENABLED=0 go test ./...          # full suite
GOWORK=off CGO_ENABLED=0 go test ./testcase/... # the harness + its negative controls
```
