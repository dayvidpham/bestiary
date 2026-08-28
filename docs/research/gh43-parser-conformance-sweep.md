# GH#43 — Parser conformance sweep for the top-traffic labs

Measured against the vendored catalog snapshot at
`parse/data/modelsdev/catalog.json`, on the branch tip that includes the
merged PR #40 (commit 4d66596).

The sweep changes NO entity key. `entities_constants_gen.go` is unchanged.
It reports only.

## Method

1. The universe is EVERY record in the vendored catalog: 7,430 rows in the
   provider (served) view and 361 rows in the models (lab) view — 7,791
   records.
2. Each record's raw model id AND its raw upstream `family` string are
   grepped for the 19 seed lab tokens.
3. Every match is driven through the PRODUCTION parser. A served row is
   keyed by the registry's own entity index (the key the `Entity` actually
   carries, built by `bestiary-gen` from `ParseFamilyDetailed`). A lab row
   and an off-catalog usage spelling go through `metadataEntityRef`, the
   function the models.dev metadata join itself uses. No parsing is
   re-implemented.
4. The resulting key is compared against the correct key.

### Boundary rule

A token matches when it is delimited by a NON-LETTER on both sides (or by
the start or the end of the string). Digits are allowed boundaries.

The rule is stated in LETTERS, not word characters, on purpose: a lab token
is routinely glued to a digit — `tencent/hy3`, `gpt-5`, `stepfun-ai/step3`,
`glm-4.6`. A word-character rule would miss all of them.

Excluded false positives, by this rule:

| Excluded | Token | Why |
|---|---|---|
| `midjourney-steps`-style ids | `step` | `step` is followed by the letter `s` |
| `codellama` spellings | `llama` | `llama` is preceded by a letter |
| `sophia`-style ids | `phi` | `phi` is preceded and followed by letters |

The `codellama` exclusion is a deliberate, declared cost of the rule: it is
consistent, and no `codellama` row in the current catalog changes any
verdict below.

### Attribution rule

A record can match more than one lab (`nvidia/llama-3.3-nemotron-super-49b-v1`
matches both `llama` and `nemotron`). The census attributes each record to
the FIRST lab that matches, in the seed order the issue lists. Attribution
is therefore DISJOINT, and the per-lab counts sum to the distinct
matched-record total — the accounting the issue's acceptance clause asks
for.

## Per-lab match count

| Lab | Token(s) | Matches |
|---|---|---|
| DeepSeek | deepseek | 211 |
| Moonshot | kimi | 145 |
| Zhipu | glm | 277 |
| MiniMax | minimax | 96 |
| Xiaomi | mimo | 55 |
| Alibaba | qwen | 552 |
| Anthropic | claude | 318 |
| OpenAI | gpt | 415 |
| Meta | llama | 195 |
| Meta | muse | 22 |
| Poolside | laguna | 14 |
| Google | gemini | 224 |
| Google | gemma | 93 |
| xAI | grok | 122 |
| NVIDIA | nemotron | 101 |
| Mistral | mistral, ministral, devstral, codestral, magistral | 192 |
| Microsoft | phi | 12 |
| Tencent | hy, hunyuan | 30 |
| StepFun | step | 31 |
| **TOTAL** | | **3,105** |

The sum of the column is 3,105, which equals the distinct matched-record
total. No match is skipped: every matched served row reaches an entity key
(0 unkeyed), and every matched lab row is driven through the decomposition.

These numbers are pinned in `TestGH43Sweep_TokenCensus`. They are
SNAPSHOT-RELATIVE: a re-vendored catalog moves them, and the test then goes
red so the sweep is re-measured instead of quietly going stale.

## Verdict per cited defect class

### Class 1 — version in the variant slot: CONFIRMED, and larger than cited

All eight cited keys reproduce, except `deepseek/v3.1-nex-n1`, whose
upstream row is gone; the spelling still produces the cited wrong key when
driven through the parser off-catalog, so the defect survives the row's
removal. One key the issue did not cite has joined: `deepseek/v3.2-maas`.

| Key | Records |
|---|---|
| `deepseek/v3.2` | 12 |
| `deepseek/v3.2-exp` | 6 |
| `deepseek/v3.1` | 5 |
| `deepseek/v3.1-terminus` | 4 |
| `deepseek/v3.2-speciale` | 1 |
| `deepseek/v3.2-maas` | 1 |
| `deepseek/v3.2-251201` | 1 |
| `deepseek/v3.1-maas` | 1 |

The correct sibling `deepseek@3.2` holds 1 record.

The measured cause is the leading `v` token, not the version format. The
control proves it: `deepseek-3.2` (no `v`) keys `deepseek@3.2` correctly,
while `deepseek-v3.2` keys `deepseek/v3.2`.

The same cause has TWO more shapes the issue did not cite, and they are
much larger than the eight keys above:

- A BARE `v<major>` token is not misplaced, it is DESTROYED.
  `deepseek/deepseek-v4-pro` keys `deepseek/pro` — no version at all — and
  `deepseek-ai/deepseek-v4-flash` keys `deepseek/flash`. `deepseek/pro`
  holds 39 records and `deepseek/flash` holds 58. The control
  `deepseek-4-flash` (no `v`) keys `deepseek/flash@4` correctly.
- The dash-glued dot-lost spelling MISREADS the version: `deepseek-v3-2`
  keys `deepseek@2`, taking only the trailing segment.

### Class 2 — compound families not reduced: PARTLY REFUTED, partly confirmed

- `claude-fable` and `claude-fable@5`: REFUTED at this tip. PR #40's
  curated fable variant reduces them; `anthropic/claude-fable-5` now keys
  `claude/fable@5` (12 records).
- `glm-4.1v-thinking/flash`: REFUTED at this tip. The re-split routes
  `glm-4.1v-thinking-flash` to `glm/v@4.1{flash}`.
- `deepseek-ocr@2`: CONFIRMED, 3 records. It also SPLITS the line — the
  same product spelled `deepseek-ai/DeepSeek-OCR` keys plain `deepseek`,
  where the `ocr` token is dropped entirely.
- NEW at this tip: `claude-mythos@5` (2 records) repeats the very defect
  PR #40 fixed for fable, on a newer Anthropic tier. The fix was a curated
  variant entry, so it does not generalise, and the next tier will repeat
  it again.
- NEW at this tip, and worse: the lab-side decomposition of
  `nvidia/llama-3.3-nemotron-super-49b-v1.5` makes the WHOLE id the family
  — `llama-3.3-nemotron-super-49b/v1.5@3.3#49b` — while the SERVED row for
  the identical id keys `nemotron/v1.5@3.3#49b`. The metadata join and the
  registry disagree about one model.

### Class 3 — split encodings: CONFIRMED

The cause is the dash-glued `z-ai` serving namespace. It is read as model
content, and it lands in a DIFFERENT slot depending on what follows it:

| Id | Key | What went wrong |
|---|---|---|
| `z-ai-glm-5-3` | `glm/z` | prefix becomes the VARIANT; version 5.3 destroyed |
| `z-ai-glm-5v-turbo` | `glm/v@5{z}` | prefix becomes an identity MODIFIER |
| `THUDM/GLM-Z1-9B-0414` | `glm/z#9b` | a GENUINE Zhipu Z1 model, colliding with the prefix artifact above |

The cited `glm/z#32b` is measured as `glm/z#9b` at this tip. `glm/z` holds
3 records and `glm/z#9b` holds 1.

The control isolates the cause: the SAME model under the slash-separated
namespace, `zai/glm-5v-turbo`, keys `glm/v@5` correctly.

A second split sits beside it: `glm-4-6v-flash` keys `glm/v@6{flash}`,
split off the 5-record `glm/v@4.6{flash}` line by the lost dot.

### Class 4 — modifier questions: CONFIRMED as observed, destination UNDECIDED

`deepseek{turbo}` (2 records) and `claude{code}` (1 record) both reproduce.

The sweep does not rule on where they belong. It does record one fact the
issue did not: `deepseek{turbo}` is a COLLISION. Two different models —
`deepseek/deepseek-r1-turbo` and `deepseek/deepseek-v3-turbo` — land on the
identical key, because each loses its line marker (`r1`, `v3`) as well. A
ruling on `turbo` alone does not separate them; the class-1 version repair
must land too.

The corpus records these three cases with the `EXPECTED_TBD` marker.

### Class 5 — upstream family `deepseek-thinking`: CONFIRMED, destination measured

The issue reported 96 rows. At this tip the upstream label carries 158
served rows and 6 lab rows. The label itself is correctly DISCARDED — no
key contains it — and identity comes from the id. The destinations:

| Destination key | Served rows |
|---|---|
| `deepseek/pro` | 106 |
| `deepseek` | 40 |
| `deepseek#70b` | 6 |
| `deepseek#32b` | 2 |
| `deepseek/v3.2-exp` | 1 |
| `deepseek#8b` | 1 |
| `deepseek/v3.2` | 1 |
| `deepseek#14b` | 1 |

The destination is therefore VERIFIED and is not itself the defect. What
it exposes is class 1: 106 of the 158 rows land on `deepseek/pro`, a key
that states no version, because their ids carry the bare `v4` token that
class 1 destroys. A further group are R1 distills of other labs' bases
(`deepseek-r1-distill-qwen-32b` keys `deepseek#32b`), where the R1 line
marker is dropped and the key states neither the line nor the base. That
last destination is a curation ruling, and the corpus marks it
`EXPECTED_TBD`.

### Class 6 — tier before version: REFUTED as a parser defect; a DIFFERENT defect confirmed

Every cited spelling DECOMPOSES CORRECTLY at this tip:

| Cited spelling | Key produced |
|---|---|
| `anthropic/claude-4.6-sonnet` | `claude/sonnet@4.6` |
| `claude-4.8-opus` | `claude/opus@4.8` |
| `claude-4.7-opus` | `claude/opus@4.7` |
| `claude-5-fable` | `claude/fable@5` |
| `claude-4.6-opus` | `claude/opus@4.6` |
| `openai/gpt-5-mini-2025-08-07` | `gpt/mini@5` |
| `moonshotai/kimi-k2.5-0127` | `kimi/k@2.5` |

None of these seven strings is a catalog id. They are USAGE-side
spellings. `Resolve` refuses each one with "model not found" because
`Resolve` matches catalog ids, and no catalog row carries these spellings.
So the 2.5%-of-tokens gap the usage join measured is a RESOLUTION gap, not
a decomposition gap: the parser already knows where each string belongs.

The real class-6 defect the cited strings pointed at is the DOUBLE-DASH
vendor prefix, and it IS in the catalog:

| Id | Key | Correct key |
|---|---|---|
| `anthropic--claude-4.6-sonnet` | `claude/sonnet` | `claude/sonnet@4.6` |
| `anthropic--claude-4.8-opus` | `claude/opus` | `claude/opus@4.8` |

The single-dash control `anthropic-claude-4.6-sonnet` keys
`claude/sonnet@4.6` correctly, so the doubled dash is the cause. Fourteen
records sit on the version-less `claude/opus` key and ten on
`claude/sonnet`.

## Committed deliverables

| Artifact | What it does |
|---|---|
| `testdata/parse/gh43_conformance_corpus.json` | 41 authored cases: every cited string, the measured witnesses, and the conforming controls |
| `gh43_conformance_internal_test.go` | `TestGH43Conformance_CitedStrings` (exact count 41, `RequireValid` non-vacuity, verdict consistency, per-class coverage, value coverage, at-least-one-confirmed-defect) and `TestGH43Sweep_TokenCensus` (the census, its per-lab pins, and the sum-equals-total mirror) |
| `fixtures_gh43_test.go` | the `//go:embed` of the corpus into the test binary only |
| `TESTING.md` | corpus table: `testdata/parse/` 49 -> 50, total 127 -> 128 |

## What this sweep deliberately did NOT do

- It changed no entity key, no curated data, and no generated file.
- It invented no ruling. Where the destination is a curation decision
  (class 4 in full, the class-5 distill destination), the corpus records
  the CURRENT key with the `EXPECTED_TBD` marker and the fix-issue draft
  frames the decision for the user.
- It priced no fix by grep. Every fix issue says the blast radius must be
  measured by regenerating and diffing the key set.
