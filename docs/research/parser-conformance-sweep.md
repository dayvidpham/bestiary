# GH#43 — Parser conformance sweep for the top-traffic labs

Measured against the vendored catalog snapshot at
`parse/data/modelsdev/catalog.json`, on the branch tip that includes the
merged PR #40 (commit 4d66596).

The sweep changes NO entity key. `entities_constants_gen.go` is unchanged.
It reports only.

## Method

1. The universe is EVERY row in the vendored catalog: 7,430 rows in the
   provider (served) view and 361 rows in the models (lab) view — 7,791
   rows.
2. Each row's raw model id AND its raw upstream `family` string are
   grepped for the 19 seed lab tokens. 6,666 of the 7,791 rows match.
3. The matched rows are DE-DUPLICATED into records, by the counting rule
   below. 6,666 matched rows collapse to 3,105 records.
4. Every record is driven through the PRODUCTION parser. A served row is
   keyed by the registry's own entity index (the key the `Entity` actually
   carries, built by `bestiary-gen` from `ParseFamilyDetailed`). A lab row
   and an off-catalog usage spelling go through `metadataEntityRef`, the
   function the models.dev metadata join itself uses. No parsing is
   re-implemented.
5. The resulting key is compared against the correct key.

### The counting rule

Every figure in this report that is called a RECORD, including every
`Records` column and every count in the six fix issues, uses ONE unit:

> A **record** is one DISTINCT raw id string, WITHIN ONE catalog view,
> compared CASE-SENSITIVELY, among the rows the seed-token census matched.

Three parts of that sentence are load-bearing, and each was measured:

- **Per view.** The provider view and the models view are counted
  separately, so an id that appears in both counts twice. This is not
  double counting: the two views reach the parser by two different code
  paths, and the sweep is about what each path produces.
- **Distinct id, not row.** One id served by many providers is ONE record.
  This is step 3 above, and it is the whole gap between 6,666 and 3,105.
  A reader who counts rows gets a number about twice as large for the
  census and about three times as large for a busy key.
- **Case-sensitive.** `tencent/hy3` and `tencent/Hy3` are two records, even
  though the registry's serving index is case-INsensitive and both reach
  one entity. Casing variants of one id therefore inflate a count slightly.

Four figures in this report deliberately use a DIFFERENT unit or a SUBSET,
and each says so where it appears: the class 5 destination table prints
provider ROWS beside the record count, because the cited issue reported
rows; the boundary-rule exclusion table prints a Rows column beside its
Records column, because one dropped row belongs to a record that is not
dropped; and the class 6 doubled-dash and `duo-chat` figures are SUBSETS
of a key's records.

Every per-key record count stated below is PINNED in
`TestParserConformance_TokenCensus`, together with the 7,430 / 361 view split, the
7,791 / 6,666 / 3,105 figures, the class 5 table, the class 6 doubled-dash
and `duo-chat` subsets, and the boundary-rule exclusion table in both units.
A moved count fails the test and names the key, so no figure here is
unguarded prose.

### Boundary rule

A token matches when it is delimited by a NON-LETTER on both sides (or by
the start or the end of the string). Digits are allowed boundaries.

The rule is stated in LETTERS, not word characters, on purpose: a lab token
is routinely glued to a digit — `tencent/hy3`, `gpt-5`, `stepfun-ai/step3`,
`glm-4.6`. A word-character rule would miss all of them.

What the rule actually excludes was MEASURED, not imagined. Against this
snapshot, 17 distinct (view, id) records that a plain substring rule would
attribute are dropped by the letter rule, and NO record changes lab. The
same drop set is 18 catalog ROWS, and both units are printed because they
differ on one row:

| Excluded spelling | Token | Why | Records | Rows |
|---|---|---|---|---|
| the 5 `nanogpt/coding-router` ids and `fastgpt` | `gpt` | `gpt` is preceded by a letter | 6 | 6 |
| the `mistralai/` namespace: 2 `mixtral` ids, `Pixtral-12B-2409`, `Voxtral-Small-24B-2507` | `mistral` | `mistral` sits at index 0 and is FOLLOWED by the `a` of `mistralai` | 4 | 5 |
| `google-paligemma`, `medgemma-4b`, `diffusiongemma` | `gemma` | `gemma` is preceded by a letter | 3 | 3 |
| the 2 `autoglm-phone` ids | `glm` | `glm` is preceded by a letter | 2 | 2 |
| `deepclaude` | `claude` | `claude` is preceded by a letter | 1 | 1 |
| `stepfun-ai/gelab-zero-4b-preview` | `step` | `step` is followed by the letter `f` | 1 | 1 |

The one row where the units disagree is `mistral`. The catalog serves
`mistralai/mixtral-8x22b-instruct` three times: twice with the raw upstream
family `mistral`, which the letter rule matches, and once with an EMPTY raw
family. The record is therefore attributed, and only that third row is
dropped. A reader who counts rows gets 18 and must not print it under a
`Records` heading.

Note that the blocked spellings are not the ones an earlier draft named:
`mixtral`, `pixtral` and `voxtral` do not contain the substring `mistral`
at all, so they cannot block that token. The Mistral drops are the
`mistralai/` NAMESPACE, and the blocking letter is on the RIGHT.

Read that table as a declared COST, not as a validation. Several of those
rows are real products of the lab whose token was blocked: the four
`mistralai/` ids are Mistral's own, `medgemma` and `paligemma` are Google's,
`autoglm` is Zhipu's. The rule under-counts those labs, consistently and
visibly, and it buys a rule that never has to guess at a stem boundary. The last row is the sharp
edge: the `stepfun` NAMESPACE is itself excluded, so a StepFun row is
counted only when its own id or raw family carries a delimited `step`.

One further exclusion changes nothing and is recorded so it is not
re-discovered: in `cognitivecomputations/dolphin-mistral-24b-venice-edition`
the `phi` inside `dolphin` is letter-bounded on both sides and is blocked,
but the row matches `mistral` and is attributed to Mistral anyway.

The earlier draft of this section named `codellama`, `midjourney-steps` and
`sophia`-style ids. Those three shapes have ZERO rows in this snapshot; the
table above replaces them with what was measured.

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

The column counts RECORDS, by the counting rule above. The sum is 3,105,
which equals the distinct matched-record total. Attribution is disjoint, so
the sum and the total are two statements of one fact.

The row-level figure is 6,666: that many of the 7,791 catalog rows carry a
seed token before de-duplication. Both numbers are true and they answer
different questions. 3,105 is the number of distinct ids the parser was
driven over; 6,666 is the number of catalog rows those ids account for.

No match is skipped: every matched served row reaches an entity key (0
unkeyed), and every matched lab row is driven through the decomposition.

These numbers are pinned in `TestParserConformance_TokenCensus`. They are
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
| **TOTAL** | **31** |

The correct sibling `deepseek@3.2` holds 1 record.

The measured cause is the leading `v` token, not the version format. The
control proves it: `deepseek-3.2` (no `v`) keys `deepseek@3.2` correctly,
while `deepseek-v3.2` keys `deepseek/v3.2`.

The same cause has TWO more shapes the issue did not cite, and they are
much larger than the eight keys above:

- A BARE `v<major>` token is not misplaced, it is DESTROYED.
  `deepseek/deepseek-v4-pro` keys `deepseek/pro` — no version at all — and
  `deepseek-ai/deepseek-v4-flash` keys `deepseek/flash`. `deepseek/pro`
  holds 39 records and `deepseek/flash` holds 58, so 97 records state no
  version although their ids state one. The control `deepseek-4-flash`
  (no `v`) keys `deepseek/flash@4` correctly.
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

  The DISAGREEMENT is the measured fact, and the corpus pins BOTH keys, one
  case per path, so the disagreement itself goes red the moment either path
  moves. Where the two paths must MEET is a curation ruling this sweep does
  not make, so both cases carry the `EXPECTED_TBD` marker, exactly as class
  4 and the class 5 distill do. The served key is not automatically the
  answer: it states the version `v1.5` in the variant slot, which is the
  class 1 defect. A third candidate, `nemotron/super@3.3#49b`, reads `super`
  as the variant and drops `v1.5`; it is a candidate, not a verdict. Fix
  issue #49 states the ruling as an open question and does not presume the
  destination.

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

The slash-separated namespace isolates the cause: the SAME model as
`zai/glm-5v-turbo` strips the prefix with no `{z}` leak, keying `glm/v@5`.
That refutes the dash-prefix defect. But `glm/v@5` still misreads the vision
`v` as a variant (see the class 7 UAT follow-up), so its fully-correct key is
`glm@5{vision}`, and the corpus records this case as a defect on that residual.

A second split sits beside it: `glm-4-6v-flash` keys `glm/v@6{flash}`,
split off the `glm/v@4.6{flash}` line by the lost dot; its fully-correct key
also fixes the vision `v` and the flash variant, to `glm/flash@4.6{vision}`.

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

This class counts a DIFFERENT population from the census: every row
carrying the upstream label, not the seed-token matches. It is therefore
printed in BOTH units. The issue reported 96 rows. At this tip the label
carries 158 provider ROWS, which are 63 distinct served ids by the counting
rule, plus 6 lab ids. The label itself is correctly DISCARDED — no key
contains it — and identity comes from the id. The destinations:

| Destination key | Records (distinct ids) | Provider rows |
|---|---|---|
| `deepseek/pro` | 35 | 106 |
| `deepseek` | 19 | 40 |
| `deepseek#70b` | 3 | 6 |
| `deepseek#32b` | 2 | 2 |
| `deepseek/v3.2-exp` | 1 | 1 |
| `deepseek#8b` | 1 | 1 |
| `deepseek/v3.2` | 1 | 1 |
| `deepseek#14b` | 1 | 1 |
| **TOTAL** | **63** | **158** |

Both columns are pinned, and the table is TOTAL in both units: it accounts
for every id and every row the label carries, so no destination can be
dropped from this prose without the test going red.

The destination is therefore VERIFIED and is not itself the defect. What
it exposes is class 1: 106 of the 158 rows, 35 of the 63 ids, land on
`deepseek/pro`, a key that states no version, because their ids carry the
bare `v4` token that class 1 destroys. A further group are R1 distills of other labs' bases
(`deepseek-r1-distill-qwen-32b` keys `deepseek#32b`), where the R1 line
marker is dropped and the key states neither the line nor the base. The
four sized keys hold 7 ids and 10 rows between them. That last destination
is a curation ruling, and the corpus marks it `EXPECTED_TBD`.

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
`claude/sonnet@4.6` correctly, so the doubled dash is the cause.

Two figures follow, and they are different sizes on purpose:

| Figure | `claude/opus` | `claude/sonnet` |
|---|---|---|
| Records on the version-less key | 19 | 13 |
| Of those, records whose raw id carries the doubled dash | 6 | 6 |

So the doubled dash's blast radius is 6 and 6, not the key totals. The
other records lose the version for OTHER reasons, and this issue does not
repair them: `anthropic/claude-opus-latest`, the
`anthropic/claude-opus-4.6:thinking*` suffixed spellings, and the
`duo-chat-opus-*` ids. Both rows of the table are pinned in
`TestParserConformance_TokenCensus`.

An earlier draft of this section stated 14 and 10. Those figures counted
the same keys while silently dropping the family-matched `duo-chat-*` ids
(5 on opus, 3 on sonnet), which no other table in this report drops. The
counting rule above now applies here too, and the figures are 19 and 13.

## UAT follow-up: two further findings (classes 7 and 8)

User acceptance of this sweep raised two findings the original six classes
did not name, and ruled on the turbo and `claude-code` cases the sweep had
left `EXPECTED_TBD`. The corpus records all of them:

- **Class 7, the vision `v` suffix.** The trailing `v` on a GLM version
  (`glm-4.5v`, `glm-4.6v`, `glm-5v`, `glm-4.1v`) is the VISION modality, not
  a variant. The parser reads it as the variant, so `glm-4.5v` keys
  `glm/v@4.5` where the ruling wants `glm@4.5{vision}`. The vision `v` line
  currently splits under a `v` variant across the generations (`glm/v@4.6`
  holds 9 records, `glm/v@4.5` 7, `glm/v@5` 6).
- **Class 8, flash-variant uniformity.** `flash`/`flashx` is a
  distinct-weight VARIANT and must key as one uniformly. Gemini
  (`gemini/flash@2.5`, 24 records) and StepFun (`step/flash@3.5`, 5) already
  do; but `qwen3-coder-flash` DROPS `flash` and collides with `qwen3-coder`
  on `qwen/coder@3` (24 records), and the GLM vision-flash spellings invert
  both slots (`glm-4.1v-thinking-flash` keys `glm/v@4.1{flash}` where the
  ruling wants `glm/flash@4.1{vision}`; `flashx` is dropped entirely). The
  compound-variant rule applies: `qwen/coder-flash@3`.
- **Class 4 rulings.** `turbo` is a serving-speed ATTRIBUTE, off the key
  (Novita's 64k-context turbo endpoint on the same base weights), so
  `deepseek-r1-turbo` and `deepseek-v3-turbo` flip from `EXPECTED_TBD` to
  defects and split their `deepseek{turbo}` collision to `deepseek` and
  `deepseek@3`. Poe's `claude-code` is a harness, not model weights (0
  context / 0 cost), so it is EXCLUDED from the keyspace.

Classes 7 and 8 are carried into their own fix issues; the corpus flips its
`want_key`s in the same PR that fixes them (red until then).

## Committed deliverables

| Artifact | What it does |
|---|---|
| `testdata/parse/parser_conformance_corpus.json` | 52 authored cases: every cited string, the measured witnesses, and the conforming controls (including the class 7 vision-suffix and class 8 flash-variant cases the UAT raised) |
| `parser_conformance_internal_test.go` | `TestParserConformance_CitedStrings` (exact count 52, `RequireValid` non-vacuity, verdict consistency incl. the `EXCLUDED` marker, per-kind PREMISE guards, per-class coverage, value coverage, at-least-one-confirmed-defect) and `TestParserConformance_TokenCensus` (the census, its per-lab pins, the sum-equals-total mirror, the 7,791 / 6,666 / 3,105 unit figures, the per-key record counts every table above states, the class 6 doubled-dash and `duo-chat` subsets, the class 5 destination table in both units, and the boundary-rule exclusion table in both units) |
| `fixtures_parser_conformance_test.go` | the `//go:embed` of the corpus into the test binary only |
| `TESTING.md` | corpus table: `testdata/parse/` 49 -> 50, total 127 -> 128 |

## What this sweep deliberately did NOT do

- It changed no entity key, no curated data, and no generated file.
- It invented no ruling. Where the destination is still a curation decision
  (the nemotron dual-path pair, the class-5 distill destination), the corpus
  records the CURRENT key with the `EXPECTED_TBD` marker and the fix-issue
  draft frames the decision. The class 4 turbo cases and the `claude-code`
  case, once `EXPECTED_TBD`, are now ruled by UAT (attribute-off-key and
  keyspace-exclusion respectively).
- It priced no fix by grep. Every fix issue says the blast radius must be
  measured by regenerating and diffing the key set.
