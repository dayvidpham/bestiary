# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html)
for its **Go module tags** (`vX.Y.Z`).

> **Two version axes.** The module tag (`vX.Y.Z`, what `go get` resolves) is
> distinct from `BestiarySchemaVersion` (the version of the public JSON output
> schema in `bestiary.schema.json`). Each release below notes both. The schema
> version only changes when the public output types change; several module
> releases share one schema version.

## [Unreleased]

The v0.2.11 release record below preserves the catalog-refresh corrections for
`laguna-s/free@2.1`, `gpt/pro`, and `ministral#8b{instruct}`. The first now re-homes to
`laguna`; the latter two are live again. Their v0.2.10 migration rows remain historically
correct for users of that release.

## [0.2.11] — 2026-09-06

**Schema:** unchanged at `0.7.0`. SQLite store schema unchanged at `9`.

This release includes the work merged since v0.2.10: the Wayback capture
([#39](https://github.com/dayvidpham/bestiary/pull/39)), models.dev refresh and
curation ([#40](https://github.com/dayvidpham/bestiary/pull/40)), OpenRouter usage
research ([#47](https://github.com/dayvidpham/bestiary/pull/47)), and parser-conformance
evidence ([#54](https://github.com/dayvidpham/bestiary/pull/54)), plus the Pi harness
identity below. Research findings and fixture expectations are not parser fixes.

### Changed

- **First Wayback snapshot capture for the harvested HuggingFace nomina.** One live
  `cmd/bestiary-hf --data-dir parse/data` run against the Internet Archive **Availability
  API**, exercising the read-only Wayback arm shipped in 0.2.10 for the first time.
  `parse/data/huggingface_nomina.json` goes from **0 to 159 entries carrying
  `archived_url`**; **25** of the 184 seed entries still have none, an honest "no snapshot
  recorded" rather than an error. **Zero** `429`/`Retry-After` backoff events fired. The
  documented guarantees held as written and were verified entry-by-entry against the
  released tip: **no** entry was removed, **no** entry lost a snapshot, and **no**
  pre-existing entry changed in any field other than gaining `archived_url`. The capture is
  visible end-to-end in the public output, not only in the seed: nomina carrying
  `Attestations[].ArchivedURL` go **0 to 159**.
  - Of 500 candidates, 250 verified and **250 returned HTTP 401** (gated Hub repos). Those
    401s are reported here separately because they are *not* counted anywhere in the tool's
    own summary line — see the known defect below.
  - The run also stamps `parse/data/datasources.json` (`huggingface` `ingested_at`) and
    rewrites `parse/data/huggingface_unlinked.json` (17 to 14). Both are documented outputs
    of the single documented invocation; the fresh ingest stamp is honest provenance for a
    genuinely new live ingest.

- **Harvested nomen census re-pinned 179 to 184** (`TestNomina_CensusExact`), with the cause
  proven rather than assumed. The documented invocation has **no Wayback-only mode** — the
  snapshot lookup rides inside the full harvest — so the capture run necessarily also
  refreshed the fetch-owned repo set, and that refresh is the entire cause of the +5: four
  nvidia repos moved unlinked to linked (`NVIDIA-Nemotron-3-Nano-30B-A3B-BF16`,
  `NVIDIA-Nemotron-3-Super-120B-A12B-FP8`, `NVIDIA-Nemotron-3-Ultra-550B-A55B-BF16`,
  `NVIDIA-Nemotron-Nano-9B-v2`) and `moonshotai/Kimi-K3` is newly present. Total nomina
  4211 to 4216. **Only the harvested leg moves**: the entity census is unmoved at **930**
  with **0** keys added or removed, `go generate ./...` leaves every generated file
  **byte-identical**, and canonical/provider-id/alias/oci re-measure UNCHANGED at
  930/2834/1/267. `archived_url` is attestation *data*, not identity, so the 159 snapshots
  add no nomina on their own.

- **models.dev catalog re-vendored (2026-08-28) — the largest upstream jump this project
  has ingested.** One polite `go run ./cmd/bestiary-gen` fetch of
  `https://models.dev/catalog.json` (`fetched_at` `2026-08-28T07:54:37Z`, etag
  `W/"cd4c3f129e5221534bd799eff950aad0"`), replacing the 2026-07-23 snapshot. UNIT:
  vendored catalog records; AXIS: the three views the artifact carries; CONFIGURATION:
  `parse/data/modelsdev/catalog.json` at this tree.

  | view | before | after |
  |---|---|---|
  | providers | 170 | **204** |
  | provider model rows | 5,765 | **7,430** |
  | models view (lab rows) | 263 | **361** |

  `github-models` was **removed** upstream; its row was dropped from
  `parse/data/creator_providers.json` (the FK gate is loud at codegen, so a retired
  provider slug aborts the bake rather than degrading).

- **Upstream field shape did not move, and that is now a machine-checked fact rather than
  an assertion.** The new census (below) measures **71 field paths** across the provider,
  model and models-view scopes before and after: **0 added, 0 removed**. The whole jump is
  more rows, not new shape.

- **Entity keyspace 930 to 989** (`TestEntityConstants_ExactCensus`, `EntityKeys()`).
  UNIT: canonical entity keys; AXIS: the generated `Entity__*` constant set;
  CONFIGURATION: this tree's regenerated bake. **87 retired, 146 minted, net +59**
  (930 - 87 + 146 = 989). The add set concentrates where upstream grew — gpt (21),
  qwen (16), seed (6), hy (6), glm (6), gemini (5), nemotron (4), claude (4) — and the
  retire set is dominated by product lines upstream dropped outright: **phi (6:
  Microsoft retired the entire Phi-3/3.5 line, leaving only Phi-4)**, doubao (6),
  ernie (3), bge (3), aion (3), and `codellama` in full. The complete removed and added
  lists are in the PR description.

- **Nomina census 4,216 to 4,993** (`TestNomina_CensusExact`). UNIT: minted nomina;
  AXIS: per scheme; CONFIGURATION: this tree's bake.

  | scheme | before | after | why |
  |---|---|---|---|
  | canonical | 930 | **989** | exactly one per entity, so it tracks the keyspace |
  | provider-id | 2,834 | **3,562** | 1,665 new upstream rows carry new ID spellings |
  | alias | 1 | 1 | unchanged |
  | huggingface | 184 | **174** | see below |
  | OCI | 267 | 267 | unchanged (no Ollama refresh in this slice) |
  | **total** | **4,216** | **4,993** | |

  The canonical and total cells are refresh figures and are corrected further down by the two
  round-2 review pins, exactly as the keyspace bullet above is: canonical **989 to 987** and
  the total **4,993 to 4,991**.

- **Ten harvested HuggingFace nomina removed, because upstream retired the entities they
  named.** The catalog now carries **zero** phi-3 rows and **zero** codellama rows, so
  `AlfredPros/CodeLlama-7b-Instruct-Solidity` and nine `microsoft/Phi-3*` repos resolved to
  entity keys that no longer exist and failed the loud codegen FK gate. There is no honest
  alias target — the surviving Phi family is Phi-4, a different generation — so the entries
  were removed, which is exactly what `cmd/bestiary-hf` would itself emit on its next run
  under that file's fetch-owned field-ownership rule. **Every surviving snapshot was
   preserved**: of the 159 `archived_url` values captured after v0.2.10, the 153 belonging to
  surviving repos are byte-identical before and after, and all 174 surviving records are
  unchanged in every field. The 6 lost snapshots belong to the retired repos alone.

  This deviates from the slice brief, which asked for `159+` `archived_url` values before
  and after; the deviation was ACCEPTED at the 2026-08-28 merge-gate ruling, which is
  recorded in the PR. The reasoning offered for it, now the accepted rationale, is that a
  literal floor over a corpus upstream has legitimately shrunk is the wrong rule. The
  invariant asserted in its place — ZERO ERASURES AMONG SURVIVORS — is no longer prose: it
  is enforced by `TestHFArchivedURL_CensusExact` (both counts with their arithmetic,
  184→174 records and 159→153 snapshots), `TestHFArchivedURL_NoErasureAmongSurvivors`
  (the survivor SET is pinned in `testdata/hf_archived_url_survivors.txt`, so a repo that
  loses its snapshot fails BY NAME rather than hiding behind an addition), and
  `TestHFArchivedURL_EveryArchivedRecordIsWellFormed` (a present-but-empty value is an
  erasure wearing the shape of a live field, invisible to both a count and a set check).

- **Four codegen gates fired on this refresh and each was resolved by curation, not by
  loosening the gate.**
  - `solar` and `ornith` are now emitted by upstream as bare families, colliding with the
    hand-curated `FamilySolar` / `FamilyOrnith` supplements — the tree did not compile.
    Both curated declarations retire; the generated constants take over unchanged.
  - `ValidateNoBetaInIdentity` aborted the bake on four `grok/beta@4.20*` entities. Vercel
    re-namespaced its beta aliases `xai/` to `spacexai/`, breaking three exact-ID pins;
    requesty added `grok-4.2-beta`; and the new `llmgateway-providers` prefixes its rows by
    backend host. Six pins added; **beta stays a release stage and never an identity**.
  - The `github-models` FK break above.
  - The HuggingFace seed FK break above.

- **Curation repairs the refresh exposed.** Each is a real defect, measured rather than
  assumed:
  - **Claude Fable was decomposing as a compound family** — `claude-fable` and
    `claude-fable@5`, sitting outside the `claude` family entirely. Anthropic publishes it
    with raw family `claude-fable` exactly as it publishes `claude-opus` / `claude-sonnet`
    / `claude-haiku`, but `fable` was missing from both `families.json` members and
    `family_overrides.json`. It now keys `claude/fable` / `claude/fable@5` like every
    sibling tier. This wart pre-dates the refresh: both compound keys were live at the
    v0.2.10 baseline.
  - **Inkling was folded back onto inclusionAI's Ling family.** The Thinking Machines line
    grew from 6 upstream rows to about 40 and gained `:free` / `:thinking` /
    `:peft:262144` spellings whose suffix defeats the ID-driven read, re-minting the bare
    `ling` key a prior epoch had retired. Three pins; `ling` is retired again.
  - **`trendyol-asure-12b` was keyed as Google Gemma.** llmtr publishes it with raw family
    `gemma`, and `gemma#12b` held that one row alone — an entire entity that was a
    misattribution. It now keys `asure#12b`, and the lab's own models-view row joins it.
  - **MiMo v2.5 Pro lost its version** through nano-gpt's `-crof` backend label (`crof` is
    itself a provider in this catalog), re-minting the undated `mimo/pro`.
  - **NVIDIA Nemotron 3.5 Lightning lost its version** in four spellings that put the tier
    before the version, re-minting the undated `nemotron#30b-a3b`. Pinned to the reading
    the dozen sibling spellings already produce.
  - **Llama-4 Scout/Maverick lost their MoE shapes** on five new `llmgateway-providers`
    backend-host spellings, keying `17b` instead of `17b-16e` / `17b-128e`.
  - **The family-`o` junk bucket was crediting OpenAI with Fish Audio's speech models.**
    Vercel publishes eight `fish-audio/*` rows under raw family `o` — the OpenAI o-series
    bucket — and the keys `o` and `o/pro` held those eight rows and **nothing else**, while
    `creators.json` maps family `o` to OpenAI. This is the same upstream defect the
    `family_enforce` ledger already corrects for vercel's wan / tts / arrow rows, but after
    the vendor namespace is stripped these ids are just `s1` / `s2-pro` / `transcribe-1`,
    carrying no family token to enforce against — so they are pinned exactly. `o` and
    `o/pro` are gone; `fish-audio/s@1`, `fish-audio/s@2{pro}`, `fish-audio/s@2.1{pro}` and
    `fish-audio/transcribe@1` replace them. The `-free` spellings share their paid
    sibling's tuple, per the free-demotion ruling.
  - **A phantom `gpt@5.6` was hijacking resolution.** kilo published
    `openai/gpt-5.6-sol-discounted`, the only `-discounted` id in the catalog; the
    unrecognised token defeated the tier scan, lost the `sol` tier, and minted a bare
    `gpt@5.6` holding that one instance. `bestiary show gpt/5.6` then answered with that
    single discounted reseller listing instead of spanning the six real GPT-5.6 tier
    entities. Pinned to `(gpt, sol, 5.6)` — a pricing label is an attribute of one
    provider's offer, never identity — and `gpt@5.6` retires.
  - **A dot-lost `mimo@25`.** inferx's new `mimo-v25` spelling names a version Xiaomi never
    published; the row sat alone on a phantom line while ~40 siblings serve `mimo@2.5`.
  - **The Dracarys 70B finetune edge was silently dropped.** Upstream re-spelled
    `abacusai/dracarys-llama-3_1-70b-instruct` with a dot; `lineage.json` still keyed the
    underscore form, so neither the full-id nor the post-`/` lookup matched and the baked
    `Lineage` went empty. Re-keyed to the dotted spelling; the `DerivationFinetune` edge to
    `llama@3.1` is baked again.
  - The `eva@0.2#32b` override was **re-keyed**: upstream dropped the base-leading
    `Qwen2.5-32B-EVA-v0.2` spelling and now serves only `EVA-UNIT-01/EVA-Qwen2.5-32B-v0.2`,
    which unpinned decomposed to the bare `qwen` bucket.

- **Corrections to the v0.2.10 migration record. The published `[0.2.10]` stanza is left
  BYTE-VERBATIM; every correction lives here.** Retirement and seam class are measured
  against a BASELINE keyspace, so both move when upstream moves, and a released stanza
  states what that release shipped rather than what is true today. A reader on v0.2.10
  needs its own figures to still be there; a reader on this tree needs the corrections.
  Both are now true. Four things changed:

  - **Two previously-retired keys are legitimately live again, and the retired-key corpora
    say so.** `gpt/pro` (edenai's rolling `openai/gpt-pro-latest`) and
    `ministral#8b{instruct}` (pioneer's original `mistralai/Ministral-8B-Instruct-2410`,
    whose 2410 is a date rather than a version) each gained a genuinely undated occupant.
    Their rows were deleted from the epoch corpus (**62 to 60**) and the gpt-tier corpus
    (**26 to 24**) in the same commit as the refresh that un-retired them — a corpus row
    asserting a hard 404 for a key upstream has re-occupied would be asserting a falsehood.
    Four other keys that came back did so through version LOSS on new spellings and were
    repaired with the pins above instead, so they stay retired.
  - **Two rows of the v0.2.10 gpt-tier migration table no longer apply on this tree**, and
    they are exactly the two above: `gpt/pro` (was → `gpt/pro@5.2`, `gpt/pro@5.4`,
    `gpt/pro@5.5`) and `ministral#8b{instruct}` (was → `ministral@3#8b{instruct}`). On
    v0.2.10 those rows were correct and a user on that build still needs them, which is why
    they remain in the released table. On this tree both keys resolve on their own, so
    following the migration would send a user AWAY from a live key. The other twenty-four
    rows are unchanged, and neither lever was reverted.
  - **`mistral/large#675b{instruct}` moved seam class**, from RESOLVED to under-specified.
    Its successor `mistral/large@3#675b{instruct}` is still ONE live entity and still the
    right successor, but it went from one provider row to three (nano-gpt and nvidia joined
    amazon-bedrock) which group into two date-differentiated candidates, so the retired
    spelling no longer names exactly one of them. On this tree the v0.2.10 `show`-seam split
    reads **45 not-found / 11 under-specified / 4 resolved** over the 60 surviving retirees,
    where the release shipped 45 / 12 / 5 over 62.
  - **One row of the v0.2.10 free-demotion migration table moved its successor.**
    `laguna-s/free@2.1` re-homes to `laguna` on this tree, not to `laguna-s@2.1` as it did
    when the lever landed. Its single pinned row, `vercel|poolside/laguna-s-2.1-free`, is
    still live and still spelled the same way; upstream changed what it stamps on that row,
    so it now decomposes onto the bare `laguna` key. `laguna-s@2.1` is still a live key — it
    simply no longer holds this instance. That relabel also cost the row its version, which
    is recorded with its reasoning in the historical bake-identity record below.

- **`modelsdev_unlinked.json` is 12, not 0, and the drained-to-zero gate is now a justified
  ledger.** This deviates from the slice brief, which asked for a drained-to-zero report,
  and the deviation was ACCEPTED at the 2026-08-28 merge-gate ruling, which is recorded in
  the PR. The reasoning offered for it is now the accepted rationale.
  `TestModelsdevUnlinked_MatchesJustifiedLedger`
  replaces `TestModelsdevUnlinked_IsDrained` and asserts SET EQUALITY against
  `unlinkedJustifiedExceptions`. Set equality is not strictly stronger than the zero gate it
  replaces — a zero gate rejects all twelve rows this ledger admits, so on the admit-an-orphan
  axis the old gate was stricter and the two are otherwise incomparable. What is true, and is
  the reason for the change: given that a non-empty justified set is accepted, set equality is
  the strongest gate available over it, and it adds a direction a count never had — a NEW
  orphan fails because it is not listed, and a listed row that starts joining again also fails
  as dead curation. Six rows were drained with honest aliases
  (`seed{vision}`, `gemini/deep-research`, `gemini/pro@3.5{translate}`, `granite/small@4`,
  `sakana-namazu`, plus `asure#12b` via the pin above). The remaining 12 are lab models no
  provider serves at that exact tier — models.dev's models view is a LAB catalogue while
  the provider rows are a SERVING catalogue — or models served only under a coarse key that
  already holds other lab models. Each carries its reason in the ledger; aliasing them
  anyway would point one lab's card at a different artifact, which is precisely the
  collision hazard `modelsdev_aliases.json` documents.

- **Declared path-unification corpus refresh.** `models_api.json` re-vendored through the
  documented `jq .providers` form (5,765 to **7,430** records, 170 to **204** providers),
  `decomp_baseline.tsv` re-captured in the same commit, and `snapshot_meta.json` restamped
  to the 2026-08-28 capture. The cross-provider justified-residual ledger goes **4 rows to
  12**, with a per-row citation each: one row LEFT (vercel corrected its `aura` mislabel on
  `sakana/fugu-ultra`) and nine joined, being three disagreements rather than nine — the
  ByteDance Seed line published under three raw-family spellings (5 ids, deferred to a
  curation slice because converging it moves entity keys and `family_aliases.json` requires
  sign-off for a fold of that shape), two glued-tier member-variant reads, and two genuine
  mislabels. At-scale pins re-measured: residual-unaccounted ceiling **303 to 353** (the
  RATE improved, 5.26% to 4.75%) and populated-version floor **4,229 to 5,433**.
  `upstream_git_commit` and `upstream_schema_version` are deliberately unchanged — the
  artifact does not carry the repo HEAD, and the field census proves the schema did not
  move.

- **Two further identity defects the refresh introduced, found in review and pinned.** Both
  are the same class the refresh already fixes twice, so the same lever applies. Keyspace
  **989 to 987**; both keys were minted BY the refresh and neither existed at the 930-key
  release baseline, so the refresh link restates as 930 − 87 + 144 = 987 and no key retires
  that did not already retire. The canonical nomina leg tracks the keyspace exactly — one
  canonical nomen per entity — so it moves 989 to 987 with it, and the nomina total moves
  **4,993 to 4,991** (987 + 3,562 + 1 + 174 + 267 = 4,991). The other four legs are
  re-measured UNCHANGED: neither pinned id is a harvested HuggingFace repo, and a re-keyed
  instance carries its own id spelling across as an Admitted nomen. These are the figures
  `TestNomina_CensusExact` and `TestEntityConstants_ExactCensus` pin at this tip.
  - `claude/opus#31b` **retires.** nano-gpt's `Gemma-4-31B-Claude-4.6-Opus-Reasoning-Distilled`
    is a Google Gemma 4 31B model DISTILLED FROM Claude Opus 4.6, and the new catalog stamps
    `raw_family: "claude"` on it where v0.2.10 carried `null`. The row keyed the flagship Opus
    line and reported **Creator anthropic** — a Google open-weights model credited to
    Anthropic at a 31B size Anthropic has never published, shown as a sibling of Opus
    4.5/4.6/4.7 by `show claude/opus`. The distillation TEACHER is not the artifact's family.
    Pinned to `(gemma, 4)`: the row rejoins `gemma@4#31b`, the 31B size is read mechanically
    off the id, and the `4` the label had hidden is recovered.
  - `inkling@256k` **retires.** requesty spells the SERVED CONTEXT LENGTH into the id, and
    `256k` landed in the version slot — a release Thinking Machines never published, and the
    only context-length-shaped version anywhere in the keyspace. The requesty row and llmtr's
    bare `thinkingmachines/inkling` carry an identical 262144 window at an identical
    1.8700 / 4.6800 price. This is the rule the `:peft:262144` pin in the same lever already
    applies. Pinned to the bare family.

- **Nine surviving rows lost a populated Version in this refresh, and the cost is now
  written down rather than implied.** All nine are upstream FAMILY RELABELS: six ByteDance
  `doubao-seed`→`seed` rows, `learnlm`→`gemini`, and the two poolside `laguna-s`→`laguna`
  rows. The ByteDance fold stays DEFERRED to its own curation slice because it moves entity
  keys across three families — but the deferral note described the cost as coexisting
  sibling keys, and the measured cost is larger. Each row retains its reason in the
  historical loss record beside `bakeIdentityVersionLosses` in `bake_identity_test.go`,
  including which are correct readings of a relabel (`learnlm`, whose
  1.5 was LearnLM's release line and not Gemini's) and which are OPEN (`laguna`, which needs
  the series-letter reading the mimo lever established but has no sibling evidence).

- **Release-only bake-identity baseline capture, not a curation fix.** The v0.2.11
  baseline was captured from production `Models()` at commit `7c85400` as required by
  the release checklist. The active `bakeIdentityVersionLosses` map is empty because
  this baseline already contains the refresh's losses and agrees with the live bake.
  Its dead-row gate forbids carrying old-baseline waivers forward. All nine historical
  row identities and rationales remain visible beside the map; the six ByteDance losses
  stay DEFERRED, LearnLM stays a correct loss, and both Laguna losses stay OPEN. The
  capture changes no model identities and does not resolve those curation questions.

### Added

- **Pi Coding Agent harness identity.** `HarnessPi` names the canonical `pi` wire token,
  `NewHarness` validates harness strings at input boundaries, and the authoritative
  harness inventory includes Pi while preserving every existing identifier. Named fixtures
  pin constructor, text, and exact unique inventory behavior. This identifies the coding
  agent and is unrelated to the Inflection Pi model catalog.

- **OpenRouter usage capture and concentration research.** `scripts/openrouter_usage.py`
  adds explicit `capture` and `analyze` commands for the `rankings-daily` dataset, with
  creator/family attribution through the Bestiary CLI and an optional per-day CSV export.
  The committed report and CSV cover **2026-07-29 through 2026-08-27**, using the refreshed
  2026-08-28 catalog. The report reaches at least 90% of recorded tokens with **16 creator
  groups** (including the unattributed `ox-alpha` group) and **17 families**; **6.0%** is
  the daily top-50 truncation's `other` bucket and **2.7%** is unmatched permaslugs.
  These are bounded historical measurements, not a live ranking or a parser repair.
  Raw API responses stay in the uncommitted cache. The source data is **CC BY 4.0**;
  `docs/research/openrouter-usage-concentration-2026-08.md` carries the OpenRouter
  attribution and links its companion `openrouter-rankings-daily_2026-07-29_2026-08-27.csv`.

- **Parser-conformance sweep for the top-traffic labs** ([#43](https://github.com/dayvidpham/bestiary/issues/43), closed by this sweep). Every
  catalog record carrying one of the **19** seed lab tokens was driven through the
  production parser and its entity key asserted. UNIT: records (one distinct raw id string,
  per catalog view, case-sensitive); AXIS: entity-key conformance per defect class;
  CONFIGURATION: the committed `parse/data/modelsdev/catalog.json` (**7,430** provider rows
  + **361** lab rows = **7,791**; **6,666** matched rows; **3,105** records; **0** unkeyed).
  - `testdata/parse/parser_conformance_corpus.json`: **52** cases pinning the evidence for
    **eight** defect classes — GH#43's original six plus two UAT-raised follow-ups (class 7,
    the GLM vision `v` suffix read as a variant instead of a `{vision}` modifier; class 8,
    flash/flashx must be a distinct-weight variant uniformly). **3** cases carry
    `EXPECTED_TBD` where the destination is still a curation ruling (the nemotron dual-path
    pair in [#49](https://github.com/dayvidpham/bestiary/issues/49) and the class 5 distill in [#52](https://github.com/dayvidpham/bestiary/issues/52)); **1** case is `EXCLUDED` (Poe's
    `claude-code` is a harness, not model weights). The UAT rulings also flip the two
    `deepseek*-turbo` cases to defects (turbo is a serving-speed attribute, off the key,
    splitting the r1/v3 collision) and the `glm-4.1v-thinking-flash` case to a defect
    (`glm/flash@4.1{vision}`).
  - `TestParserConformance_TokenCensus` pins the counting rule end to end: all **19** per-lab
    counts, **27** per-key record counts, the class 5 destination table in both units, the
    class 6 doubled-dash and `duo-chat` subsets, the view split, and the boundary-rule
    exclusion table in both units (**17** records / **18** rows). Premise guards on all
    three id kinds keep a refutation from silently going stale.
  - Verdicts: class 1 CONFIRMED, the leading `v` misplaces or destroys the version ([#48](https://github.com/dayvidpham/bestiary/issues/48));
    class 2 PARTLY REFUTED, two compound families remain and the two ingest paths disagree
    about one model ([#49](https://github.com/dayvidpham/bestiary/issues/49)); class 3 CONFIRMED, the dash-glued `z-ai` namespace is read as
    model content and collides with GLM-Z1 ([#50](https://github.com/dayvidpham/bestiary/issues/50)); class 4 CONFIRMED,
    with turbo ruled a serving attribute in the expected keys ([#51](https://github.com/dayvidpham/bestiary/issues/51)); class 5 VERIFIED, the `deepseek-thinking` label is
    correctly discarded and the rows mostly re-expose class 1 ([#52](https://github.com/dayvidpham/bestiary/issues/52)); class 6 REFUTED as a
    parser defect, the cited spellings decompose correctly and fail at resolution, while a
    doubled vendor dash in the catalog does destroy the version ([#53](https://github.com/dayvidpham/bestiary/issues/53)).
  - The sweep only reports: **no** entity key changes, `entities_constants_gen.go` is
    byte-identical, and a `--no-fetch` regen leaves a zero diff. Each fix lands in its own
    issue, priced by regen-and-diff. Full method and figures:
    `docs/research/parser-conformance-sweep.md`.
  - The final corpus has **16 conforming, 32 defect, 3 undecided, and 1 excluded** cases.
    Its `want_key` values record the accepted destinations, not newly shipped parser
    behavior: vision `v` belongs in `{vision}` ([#56](https://github.com/dayvidpham/bestiary/issues/56));
    `flash`/`flashx` belong in distinct-weight variants, including `qwen/coder-flash@3`
    ([#57](https://github.com/dayvidpham/bestiary/issues/57)); and the two DeepSeek turbo
    spellings must lose the serving attribute from identity ([#51](https://github.com/dayvidpham/bestiary/issues/51)).
    The class-3 expectations also honor those rulings; its conforming slash-prefix
    control uses the non-vision `zai/glm-5.3` → `glm@5.3`, not a still-defective vision
    spelling. Excluding Poe's `claude-code` from model-conformance expectations does
    **not** remove that row from the production catalog. These parser and catalog
    defects remain unresolved here; the sweep supplies evidence and fix-issue drafts.

- **`parse/data/modelsdev_field_census.json` — a committed census of the UPSTREAM field
  shape, with a loud drift guard.** Every field path the vendored catalog publishes, with
  the number of records filling it: provider level, per-provider model rows, the models
  view, and one level of object nesting in each. **71 paths** on this snapshot. UNIT: field
  paths; AXIS: fill count per path; CONFIGURATION: the committed
  `parse/data/modelsdev/catalog.json`.

  A snapshot refresh was previously reviewable only on counts. Row and provider totals are
  obvious in a diff; a field **added** upstream (nothing consumes it yet, so nothing fails)
  or **removed** upstream (something downstream silently reads a zero value forever) was
  invisible. `TestModelsdevFieldCensus_NoDrift` recomputes the census from the vendored
  catalog and fails **naming the added and removed paths**, with a fix procedure per
  direction; `_UpToDate` catches a fill count that moved while the path set held; and
  `_EnvelopeContract` pins the committed-emission invariants (count agrees with the list,
  explicit sort, empty-not-null, no wall clock, all three scopes non-vacuous).

  The emission follows the established committed-report pattern: a pure
  `buildModelsdevFieldCensus` returning bytes, wired into `runFixtureCodegenArtifacts` and
  the **N=100** `TestCodegen_Reproducible_ByteIdentical` byte-identity loop — which is the
  only place an omitted sort could surface, since the census walks JSON objects whose key
  order Go randomizes on every decode. `TestBuildModelsdevFieldCensus_DetectsAnAddedField`
  is the falsifier: it injects a synthetic field into an in-memory copy of the catalog and
  asserts the census reports exactly that path and its nested subkey, so an emitter that
  returned a constant path set cannot pass.

- **Behavioural regression tests for the 29 curated exact-id pins, the archived_url survivor
  invariant, the previous-release bake identity, and the re-keyed retirees.** Four guards,
  each closing a gap where a claim was made in prose or defended only by a determinism
  check.

  - `curated_id_pins_internal_test.go` pins all **29** exact-id overrides to the behaviour
    each defends: the BEFORE tuple the unpinned pipeline produces, the AFTER tuple, the
    entity key, the Creator attribution at stake, and the user-visible defect that returns.
    The load-bearing arm re-runs each id through `ParseFamilyDetailed`, so it goes RED THE
    MOMENT A PIN LEAVES `parse.go` — before any regeneration. The only previous guard was
    the stale-bake check, whose printed remedy after a pin loss is "regenerate and commit",
    which produces a green tree with the defect restored. Twelve phantom and junk-bucket
    keys are pinned ABSENT, and `show gpt/5.6` is asserted to stay under-specified over the
    six real tiers rather than resolving to one reseller's discounted rehost.
  - `nomen_archived_url_test.go` enforces the survivor invariant described above.
  - `bake_identity_test.go` compares the PREVIOUS RELEASE's baked
    `(ID, Provider) → (Family, Variant, Version)` tuples against the current bake. This is
    the one measurement the declared baseline re-capture structurally removes: the
    path-unification baseline is re-captured on every refresh, so a decomposition change
    introduced BY the refresh is frozen into it before the gate measures anything, and
    `(c)REGRESSION=0` cannot mean what it appears to. `testdata/bake_identity_baseline.tsv`
    is deliberately NOT re-captured on a refresh — it moves only during release PR
    preparation, as described in the release-only capture record above.
  - `refresh_2026_08_28_rekey_retired_keys_corpus.json` records the migration for the **19**
    retired keys whose ARTIFACTS SURVIVED under a different key, discriminated by measuring
    each retired key's v0.2.10 instance ids against the refreshed catalog's id set (19
    re-keys, 68 genuine deletions, which are owed no record). `claude-fable@5` held 24
    instances and RESOLVED at the release; its successor `claude/fable@5` was named nowhere.
    Every successor set is re-derived from the key's own instances and asserted LIVE.

- **The field-shape census is now a LOUD codegen failure.** Both arms of the emission were
  warnings, so a build or write failure left the previous, stale census committed and the
  drift gate then failed far from the cause. `modelsdev_unlinked.json` and
  `parse_failures.json` are diagnostic AIDS; the census is a committed ARTIFACT a gate
  reads, which puts it on the vendored-catalog footing.

- **The path-unification diff report no longer re-emits itself.** It used to rewrite on any
  passing run, so a dropped pin that left `(c)` at zero silently updated the committed
  evidence in the working tree beside a red test elsewhere. Refreshing it is now the same
  declared, env-gated act as re-capturing the baseline, and an ordinary run COMPARES.

### Known defects (observed during the HuggingFace Wayback capture; not fixed here)

- **`cmd/bestiary-hf`'s summary line under-reports gated repos.** It prints a counter
  labelled `absent(404/401)`, but `verifyRepo` maps only `d.notFound()` to absent; an HTTP
  `401` falls through to the default error branch and is logged as a `skip ...` line on
  stderr **without incrementing any counter**. In this run that hid 250 of 500 candidates
  behind a printed `0 absent(404/401)`. Reporting defect only — no harvested data is wrong.
- **`mistralai/Mistral-Large-3-675B-Instruct-2512` appears in BOTH the seed and the unlinked
  report.** Its seed entry is a curation-owned hand-repair pointing at
  `mistral/large@3#675b{instruct}` (an entity that does exist), but the *mechanical* join no
  longer reproduces that target, so this run's join dropped it to the unlinked report while
  merge-on-refresh correctly preserved the curated entry. Consequence: it can never gain an
  `archived_url`, because the snapshot lookup enriches mechanically-linked repos only.

## [0.2.10] — 2026-08-28

**Schema:** `0.6.0` → `0.7.0` (additive). SQLite store schema `8` → `9`.

### Changed

- **Two dead curation rows are swept.** Both were reachable only through data this
  release itself removed, and both are deleted with a measured proof of deadness: a full
  `go generate ./...` with and without each row is **byte-identical in every generated
  file** — no key moves, no instance moves, no report row changes.

  - `parse/data/creators.json`: the `{"family": "kimi-k2", "creator": "moonshotai"}` row.
    The `kimi-k2` family was retired by the general series-compound recovery, so the row
    named a family that no longer exists. Moonshot keeps its attribution through the
    surviving `kimi` row, and `Creators()` is unmoved at **43**.
  - `parse/data/family_overrides.json`: the `mimo-v2.5` row. It is subsumed by
    `splitSeriesVariant`, which re-decomposes a letter-prefix series directly off the model
    ID: `ParseFamilyDetailed(raw=mimo, id=mimo-v2.5)` already yields
    `(mimo, "", "2.5", "")` without it, which is what the canonical seam and therefore every
    entity key uses. The row only affected the raw-only `ParseFamily` primitive, whose
    whole-token output (`v2.5`) is documented as SUPERSEDED downstream
    (`parse/data/version_patterns.json`). Its case leaves
    `testdata/parse/family_overrides_corpus.json` with it, since that corpus enumerates the
    override rows. The sibling `mimo-v2.5-pro` row is **not** dead and stays.

- **Kling's eight video keys state their version in the version slot.** The klingai rows
  are spelled `klingai/kling-v<version>[-turbo]-<modality>` upstream, and the leading-token
  decomposition read the whole remainder as ONE variant token, so the keys rendered
  `kling/v2.5-turbo-i2v`, `kling/v3.0-motion-control` and so on — a flattened string
  carrying three different KINDS of fact in one slot, sitting directly beside `kling@2.6`,
  which spells the same kind of version in the version slot. Both shapes could not be
  right. This was deferred when the collision split landed; it is pulled in here.

  **Which axis carries which token, and why.**

  | token | axis | reason |
  |---|---|---|
  | `v` | none — it leaves the key | A version PREFIX: it introduces the number it is glued to and names no sibling line. Same reading this release gives the mimo series letter and the cogito release letter. The v-carrying spelling survives verbatim as a provider-id nomen. |
  | `2.5` / `2.6` / `3.0` | **version** | This is what makes the set coherent with `kling@2.6`, which already keys its version this way. |
  | `i2v` / `t2v` / `motion-control` | **variant** (identity) | Image-to-video and text-to-video are genuinely different artifacts taking different inputs, not a serving tier. The variant slot is where this release puts a named member of a line (the gpt tiers, the mimo speech members), and a modality is exactly that. |
  | `turbo` | **identity modifier** `{turbo}` | The repo's fast/turbo rule is a global identity fail-safe with per-family ATTRIBUTE demotions, each curated against evidence that the token names a speed tier of a base the catalog also serves. Kling has no such evidence — there is **no** non-turbo 2.5 row anywhere in the catalog — so demoting would key these two rows onto a `kling@2.5` base nothing attests. |

  **A typed modality axis is still the better long-term home, and is deliberately NOT minted
  here.** One vendor's three tokens are not enough evidence to design a keyspace-wide axis,
  and the variant slot is reversible into one later. What is fixed now is the incoherence
  between `kling@2.6` and `kling/v2.6-i2v`.

  **Measured key diff** (unit: entity keys; axis: the constant set in
  `entities_constants_gen.go`; configuration: this lever alone, on the release tip). Eight
  renames, nothing else moves — **census 930 → 930**:

  | old spelling | new spelling |
  |---|---|
  | `kling/v2.5-turbo-i2v` | `kling/i2v@2.5{turbo}` |
  | `kling/v2.5-turbo-t2v` | `kling/t2v@2.5{turbo}` |
  | `kling/v2.6-i2v` | `kling/i2v@2.6` |
  | `kling/v2.6-motion-control` | `kling/motion-control@2.6` |
  | `kling/v2.6-t2v` | `kling/t2v@2.6` |
  | `kling/v3.0-i2v` | `kling/i2v@3.0` |
  | `kling/v3.0-motion-control` | `kling/motion-control@3.0` |
  | `kling/v3.0-t2v` | `kling/t2v@3.0` |

  **This table is not a migration table, and no key is retired by it.** Every one of the
  eight old spellings was minted *inside this same unreleased release* by the collision
  split and **never shipped**: measured against the release baseline, none of them appears
  on either side of the cumulative key diff, which stays at **62 retired / 35 added**. There
  is nothing for a released consumer to migrate from; the table is here so a reader of the
  collision-split entry below is not left with the old spellings.

  Series lines 409 → 410 (versioned 208 → 210, bare 201 → 200): the eight keys leave the
  bare `kling` line — which held nothing else, so it empties — for generation lines, two of
  which (`kling` gen-2.5 and gen-3.0) are new while gen-2.6 already existed. Releases,
  instance totals and every nomen count are unmoved. The path-unification gate reports
  **`(c)=0`** and needs **no new justified-exception row**, exactly as the deferral priced it.

- **The entity view lists the lab's own providers first.** `bestiary show <ref>
  --by-entity` rendered `Providers (N):` as one flat, effectively alphabetical run, so
  the organisation that TRAINED the model sat wherever its name happened to fall —
  Zhipu's own `zhipuai` was 41st of 42 on `glm@5`, one line below a `Creator: zhipu`
  field. The line now reads in three groups: the creator's own hosted surfaces in the
  curated order (which encodes primacy — `zhipuai` ahead of the international `zai`
  brand), then the family's `CanonicalProvider` when it is not already among them, then
  every remaining provider alphabetically. A ` | ` separates the preferred group from
  the rest, so the boundary is visible without knowing the creator table by heart; the
  bar is omitted when either side is empty.

  The **`Instances` table follows the same order**. That is the half with teeth: the
  table truncates at 20 rows, so on a heavily-rehosted entity the lab's own offering
  could be cut from the view entirely while twenty rehosts were shown.

  Presentation only. No key moves, no entity or instance is added or dropped, the
  printed provider count is unchanged (the list is a permutation), and `--output json`
  is untouched — it still carries the registry's own `Providers` and `Instances` order.

- **The GPT 5.6 tiers are variants of `gpt`, not families of their own.** Luna, Sol and
  Terra are tiers of one release, exactly as Claude's Haiku, Opus and Sonnet are, and the
  curated `parse/data/family_overrides.json` table already holds that precedent. Three
  rows map upstream family `gpt-<tier>` onto `(family gpt, variant <tier>)` and cover all
  **76** catalog rows carrying such a family — a different population from the **75** ids
  matching the `gpt-*{luna,sol,terra}` id pattern, and neither is a typo for the other.
  The keys now read `gpt/luna@5.6`, `gpt/luna@5.6{pro}` and the same for `sol` and `terra`.
  - The `parse/data/modelsdev_aliases.json` rows are retargeted in the same change.
    Omitting that step was measured to synthesize three phantom zero-instance standalone
    entities.
  - Six exact-ID pins carry the `-pro` rows. With the tier in the variant slot, `pro` has
    nowhere mechanical to go — it is not in `modifiers.json`, so nothing can peel it — and
    the six `-pro` rows were measured to CONFLATE into their non-pro siblings, a silent
    data conflation. Teaching `pro` globally was priced and rejected: it re-keys ~30
    unrelated families **and still does not fix these rows**, because `pro` is a curated
    `gpt` member and the member guard holds.

- **Venice's dot-less version spelling is read as the version it means, for all fourteen
  of its GPT ids.** Venice publishes `openai-gpt-56-luna` and `openai-gpt-55-pro` — GPT
  5.6 Luna and GPT 5.5 Pro with the dot squashed out. This is a **curated reading of
  fourteen specific ids, not a parser change**, and it is deliberately all-or-nothing:
  a partial pin would scatter one aggregator's own rows across dated and undated keys.

  **What this means for you as a reader.** Venice's rows now sit **under the dated keys**
  — `gpt/luna@5.6`, `gpt/luna@5.6{pro}` and so on — where before they would have attached
  to the undated `gpt/<tier>` and `gpt/<tier>{pro}` and been invisible to anyone filtering
  on the dated key. The consequence of pinning is that the undated `{pro}` key per tier
  now has **no occupant at all** and is retired: every `-pro` instance is dated. The bare
  `gpt/<tier>` keys DO survive, holding one row each — gitlab's, for the reason in the
  next entry.

- **A model id no longer loses its version to a leading token that repeats what another
  axis already records.** An id whose first token is the serving provider's own slug
  (`databricks-gpt-5-6-luna`) or the lab that trained the model
  (`openai-gpt-5.6-luna`, `openai.gpt-5.6-luna`) pushed the version scan one token late,
  so the artifact keyed an **undated sibling** of the entity it belongs to. Measured, 165
  records were affected and 120 changed their decomposition.

  The rule is a classification, not a prefix list (`ClassifyIDPrefix`, `id_prefix.go`): a
  leading token may be dropped **only when a DIFFERENT carrier already holds the fact it
  names**, and the strip is additionally refused unless the remainder still names a known
  family. Two carriers license it — the `Provider` field, and the `Creator` axis when the
  remainder's family declares that exact lab — and everything else is left byte-identical.

  **What is deliberately NOT stripped, and why the distinction is the whole point:**
  - A **backend-host label**. nano-gpt's `azure-gpt-4o` is served by nano-gpt, not Azure,
    so `azure` is the only place that routing fact appears. A blanket provider-name strip
    deleted exactly this label once; here it simply fails both carrier tests.
  - A **product-surface namespace**. gitlab prefixes all 22 of its ids with `duo-chat-`,
    which is neither its provider slug nor a lab, and no axis records which of a
    provider's surfaces served a model. Stripping it would delete a fact rather than
    repeat one — so gitlab's three tier rows stay on the undated `gpt/<tier>` keys, and
    that is the measured cost of the rule being honest rather than tidy.
  - A **family token that happens to be constant across a provider's catalog**. Measured,
    28 providers prefix every one of their ids with a single token, and for most of them
    (`claude-`, `grok-`, `glm-`, `kimi-`, `mimo-`, `solar-`) that token IS the family.
    Constancy is not a carrier test.
  - A **lab token the Creator axis spells differently**. Bedrock's `zai.glm-…` and
    `moonshot.kimi-…` are declined, because the curated creators are `zhipu` and
    `moonshotai`; the carrier does not hold the value the id spells.

  Two smaller consequences are worth naming. The Bedrock grammar
  `[<region>.]<vendor>.<model>[-v<N>:<M>]` now has BOTH arms normalized: the routing tail
  goes with a dotted strip, because leaving it swallows the release date behind it. And
  two Bedrock rows whose dotted lab segment was being read as the FAMILY — Mistral's
  Voxtral Mini and Small, keyed as `mistral/mini` and `mistral/small` — land on `voxtral`,
  the line Mistral actually published them under.

- **A compound series family is recovered generally, not one spelling at a time.** A
  provider that reports a COMPOUND series family as its raw family (`kimi-k2`, `kimi-k3`)
  kept that compound verbatim whenever the version-pattern table missed it. That table
  matches only a DOTTED series number (`kimi-k2.7` → `kimi` + `k2.7`), so every
  BARE-INTEGER series compound fell through to passthrough and stranded its models on a
  compound-family key of their own, split off from the short-family siblings carrying the
  same series. `kimi-for-coding`'s bare id `k3` — tagged with raw family `kimi-k3`, with
  the series token living ONLY in the family field — was invisible to the `kimi` series
  entirely, sitting alone on a `kimi-k3` key.

  The empty-raw inference already recovered this shape; the raw-populated path did not.
  Wiring the SAME closed predicate into the raw-populated path is a general reduction over
  series families and series numbers: it accepts a family only when it is exactly
  `<base>-<letter><number>` and `<base>` carries that series letter, so a future series
  number recovers automatically and **no exact-id entry is added for `k3`**. A family
  self-mapped in `family_overrides.json` is a curated genuine compound and is declined,
  as are a wrong series letter, a series letter with no number, and any compound carrying
  an extra unrecognised token.

  ONLY the family is reduced. The `(variant, version)` split stays the letter-prefix
  seam's job, run against the model ID. Seeding it from the consumed family token instead
  was measured and rejected: a provider that tags a K2.5 model with the coarser raw family
  `kimi-k2` (`moonshotai/Kimi-K2.5-TEE` and siblings) would then be asserted onto the
  `kimi/k@2` key and silently merged with genuine K2 models. An under-specified model
  belongs on the honest bare-family line, never on a confidently wrong version key — so
  those rows stay exactly where they were, and that is pinned as a negative control.

- **Two defects in the offline Ollama bot, both invisible until it was run against the
  real registry.** (1) `model_id` is the corpus's join key — codegen matches rows onto the
  catalog by exact model id — but two distinct Ollama identities routinely resolve to one
  catalog id (the bare size tag `llama3.1:405b` and the explicit `llama3.1:405b-instruct`
  are the same model), and the bot emitted an entry per identity: a real refresh wrote the
  same `model_id` **two and three times**, each with a different subset of the quants, and
  codegen would have kept whichever it read last. Identities are now coalesced by output
  model id — the strongest join arm owns the entry and any quant they disagree on, while
  every other identity still contributes the quants it alone measured, so coalescing never
  costs a measurement. (2) The bot wrote the RAW Ollama quant token, and Ollama spells
  16-bit float `fp16` — a spelling the `Quantization` enum deliberately rejects, leaving
  normalisation to the ingest layer. That produced a corpus the loader refuses outright,
  and it orphaned curation: a curated `f16` row's architecture facts vanished when the
  refresh reported the same quant as `fp16`. Rows now carry the canonical enum name, so
  the refreshed row IS the curated row; a token with no canonical name is dropped with an
  actionable message rather than written.

- **The offline Ollama refresh bot now names the version that actually made the request.**
  Its `User-Agent` was a hand-spelled literal and sat at `bestiary-ollama/0.2.4` for three
  releases, misreporting the build to the registry operators whose logs carry it. The
  version segment is now DERIVED from a new `bestiary.ReleaseVersion` constant — a THIRD
  version axis, distinct from both `BestiarySchemaVersion` (the public JSON output
  contract) and the SQLite store schema number — so it reads `bestiary-ollama/0.2.10`
  and cannot drift again. `AGENTS.md`'s release procedure gains the bump step, and a pin
  test fails until the bump happens.

  The bot's outbound guarantees are now asserted at the tool's OWN seam, not only inside
  the shared polite-bot package: one constructor (`newPoliteClient`) builds the client
  `run()` uses, and the offline tests drive that same constructor with an injected
  transport and a fake clock to pin the current-version `User-Agent`, the ≥1 s gap between
  consecutive requests (first request: no sleep; second: ≥1 s), and that every outbound
  path funnels through the injected transport — a client whose transport refuses to dial
  produces an error rather than a silent socket. `go test ./cmd/bestiary-ollama` was
  additionally run inside a disabled network namespace (`unshare -rn`) and is green:
  zero network requests.

- **The measured-weights corpus is refreshed from the live Ollama registry, and the
  calculator's measured coverage grows from 2 entities to 19.** One network-gated run of
  the offline bot over its seven-model allowlist takes `parse/data/quant_vram.json` from
  **4 hand-written entries to 47** (43 new, none removed).

  **What you gain as a reader.** Measured over the shipped catalog with
  `FitOver(Entities(), FitFilter{})` — UNIT: entities; AXIS: the `FitResult`
  denominators; CONFIGURATION: this tree's own regenerated bake:

  | denominator | before | after |
  |---|---|---|
  | `EntitiesConsidered` | 930 | 930 |
  | `EntitiesMeasured` | 2 | **19** |
  | `EntitiesDerived` | 297 | 281 |
  | `EntitiesExcluded` | 11 | 11 |

  Seventeen entities become measured. **Sixteen were previously DERIVED** — they had an
  estimate inferred from an attested parameter count, and now carry real per-quant
  footprints instead: `gemma@2#2b`, `gemma@2#27b`, `llama@3.1#8b`, `llama@3.1#8b{instruct}`,
  `llama@3.1#70b`, `llama@3.1#70b{instruct}`, `llama@3.1#405b{instruct}`,
  `llama@3.2#1b{instruct}`, `llama@3.2#3b`, `mistral#7b`, `mistral#7b{instruct}`,
  `mistral@0.3#7b{instruct}`, `qwen@2.5#7b{instruct}`, `qwen@2.5#14b{instruct}`,
  `qwen@2.5#32b{instruct}`, `qwen@2.5#72b{instruct}`. **One is newly covered outright**:
  the bare `mistral` key had no attested total, so before this refresh the calculator
  could say nothing about it at all. **No new architecture facts**: `layers`/`kv_heads`/
  `head_dim` are curation-owned and the registry's config blob does not publish them, so
  the newly measured rows are weights-only (`VRAMEstimatePartial: true`) until a curator
  supplies them.

  **The entity keyspace does not move: 930 keys before, 930 after — zero renames, zero
  fissions, zero retirements, and no `param_size` introduced.** The refresh writes
  `Source` and `QuantVRAM` onto 63 catalog rows and touches no other generated field.

  **Every curation-owned field is byte-identical afterwards**, verified by diffing the
  four pre-existing entries field by field: per-entry `_comment`, `context_window`,
  `base_ref`, and the per-quant `layers`/`kv_heads`/`head_dim` on all three arch-curated
  quants of the 70B anchor. The fetch-owned fields move as designed — the quant set
  widens to what the registry publishes (the 70B model goes from 3 curated quants to 12),
  and the weights update to the current manifests, which corrects the seed's 70B `q4_k_m`
  estimate from 43,033,509,888 down to the measured 42,520,398,528 bytes.

  **One text field DID change, and it is the file's own header, not curation.** The
  top-level `_comment` of `parse/data/quant_vram.json` — distinct from the per-entry
  `_comment`s above, which are byte-identical — is the tool's own constant and is
  rewritten unconditionally on every run. It goes from the old `KEYING CONTRACT`
  paragraph to a `FIELD OWNERSHIP` paragraph naming which fields the bot owns
  (`weights_bytes`, digest, quant set, `param_size`, `source`) and which curation owns
  and the bot preserves. This is a documentation rewrite of the file header only; no
  entry data moves with it.

  **`ollama_unlinked.json` did not exist before this run; it now lists 26 entries** — the
  base-unknown community models the bot keeps rather than drops (`gemma2`, `phi3.5` and
  `qwen2.5` tags with no joinable models.dev row, plus the `:latest` and version-only
  Mistral tags). They ship as standalone entries, so nothing measured is discarded.

  **The OCI naming scheme now carries data.** The refresh is the first run to capture the
  per-quant OCI manifest digest, so the shipped registry mints **267 OCI nomina** where it
  minted 0 — 262 distinct digests across 19 entities, counted as 267 (digest, entity)
  pairs because three digests are published under more than one catalog ID. The total
  nomen census moves 3,944 → 4,211; the canonical (930), provider-id (2,834), alias (1)
  and huggingface (179) legs are re-measured **unchanged**, since the refresh adds no
  entity, no instance and no ID spelling.

  One tag was skipped: `llama3.1:70b-instruct-q3_K_S` answered HTTP 503. The bot reports
  and continues rather than abandoning a whole refresh for one tag.

### Removed

- **Four more entity keys are retired by the series-compound recovery**, and one is
  added (`kimi/coder`, for the two `umans-coder` rows whose id names no series token at
  all, so the letter-prefix seam correctly declines and the residual token becomes the
  variant). No alias is minted, no redirect is added and no successor is listed at the
  tool: this table is the migration record, and the only pointer a user gets.

  | retired key (series-compound recovery) | instances re-home to |
  |---|---|
  | `kimi-k2` | `kimi/coder`, `kimi/k@2.7` |
  | `kimi-k2{instruct}` | `kimi/k@2{instruct}` |
  | `kimi-k3` | `kimi/k@3` |
  | `kimi{instruct}` | `kimi/k@2{instruct}` |

  `kimi-k2` SPLITS — it is not a fold onto one successor — because two of its four rows
  name no series token and two carry a dotted `2.7`. Every row is re-derived from the
  instances the retired key actually held, checked against the live registry on each run,
  and cross-checked against this table in BOTH directions
  (`cmd/bestiary/testdata/retired/compound_recovery_retired_keys_corpus.json`).

  Two of these four are a MEASURED DEVIATION from the epoch's uniform-404 reading, and
  they are recorded rather than repaired. `kimi-k2` and `kimi-k3` are the upstream
  raw_family spellings, and both are still LIVE concrete model ids, so both CLI seams still
  answer them: `bestiary show` finds the model directly, and `bestiary show --by-entity`
  finds it through its concrete-model-id arm and renders the owning entity (`kimi/k@2`,
  `kimi/k@3`). The hard 404 that admits no exception is at the EXACT-key seam
  (`bestiary.EntityByKey`), and it holds for all four — and for all 62 keys this release
  retires. Making either CLI seam fail would mean breaking a live lookup because the
  spelling happens to match a retired key — an under-specified reference at `bestiary show`,
  an exact concrete-model-id hit at `show --by-entity`.

- **The retired-key rule, stated per seam.** This is the release's single normative
  statement of what a retired key does, and it governs every per-key stanza below. An
  earlier draft of some stanzas below described `bestiary show --by-entity` as "the
  exact-key seam" and read the hard 404 as covering it; that wording conflated two
  different exact lookups, and because it never shipped it is rewritten in place rather
  than appended to — in the CHANGELOG and in every retired-key test docblock. The rule:

  > A retired key is not found at the exact-key seams: `EntityByKey`,
  > `GET /entity/<key>`. The CLI resolver keeps its short-reference fallback. An old
  > spelling that is still a valid short reference can resolve or show candidates.

  So the invariant that admits no exception is the pair of EXACT-key seams — the library
  call `bestiary.EntityByKey` and the web route `GET /entity/<key>` — and it holds for
  all **62** keys this release retires. Neither CLI seam is one of those two, and the two
  do not behave alike. `bestiary show --by-entity` is an exact match over the
  store-overlaid entity index, accepting the entity key, the entity preferred name or a
  concrete model id; it has NO short-reference path, so it never returns the
  under-specified error. `bestiary show` runs the input through the model resolver, which
  keeps its short-reference (under-specified) fallback, so a retired spelling that is
  still a valid short reference answers there or lists candidates. Measured at the
  shipped `bestiary show` seam, the 62 split **45 not-found / 12 under-specified /
  5 resolved**; `show --by-entity` differs from `show` for a further four keys. The
  per-key record is `cmd/bestiary/testdata/retired/epoch_retired_keys_corpus.json`,
  probed by `TestEpochRetiredKeys_MeasuredPolicySplit`. No alias is minted, no redirect
  is added and no successor is listed at the tool: the migration tables below are the
  only pointer a user gets.

- **Twenty-six entity keys are retired by the two changes above.** Twelve come from the
  tier re-key and fourteen from the leading-token strip. No alias is minted, no redirect
  is added and no successor is listed at the tool: this table is the migration record, and
  the only pointer a user gets.

  | retired key (tier re-key + prefix strip) | instances re-home to |
  |---|---|
  | `agi` | `agi@01` |
  | `devstral#123b` | `devstral@2#123b` |
  | `gemma#12b` | `gemma@3#12b` |
  | `gemma#26b-a4b` | `gemma@4#26b-a4b` |
  | `gemma#4b` | `gemma@3#4b` |
  | `gpt-luna` | `gpt/luna`, `gpt/luna@5.6` |
  | `gpt-luna/pro` | `gpt/luna@5.6{pro}` |
  | `gpt-luna/pro@5.6` | `gpt/luna@5.6{pro}` |
  | `gpt-luna@5.6` | `gpt/luna@5.6` |
  | `gpt-sol` | `gpt/sol`, `gpt/sol@5.6` |
  | `gpt-sol/pro` | `gpt/sol@5.6{pro}` |
  | `gpt-sol/pro@5.6` | `gpt/sol@5.6{pro}` |
  | `gpt-sol@5.6` | `gpt/sol@5.6` |
  | `gpt-terra` | `gpt/terra`, `gpt/terra@5.6` |
  | `gpt-terra/pro` | `gpt/terra@5.6{pro}` |
  | `gpt-terra/pro@5.6` | `gpt/terra@5.6{pro}` |
  | `gpt-terra@5.6` | `gpt/terra@5.6` |
  | `gpt/pro` | `gpt/pro@5.2`, `gpt/pro@5.4`, `gpt/pro@5.5` |
  | `kimi-k2{code}` | `kimi/k@2.7{code}` |
  | `ministral#3b{instruct}` | `ministral@3#3b{instruct}` |
  | `ministral#8b{instruct}` | `ministral@3#8b{instruct}` |
  | `mistral/large#675b{instruct}` | `mistral/large@3#675b{instruct}` |
  | `mistral/mini#3b` | `voxtral/mini#3b` |
  | `mistral/small#24b` | `voxtral/small#24b` |
  | `nemotron#120b` | `nemotron@3#120b` |
  | `nemotron#30b-a3b` | `nemotron@2#30b-a3b` |

  Every row is re-derived from the instances the retired key actually held, checked against
  the live registry on each run, and cross-checked against this table
  (`cmd/bestiary/testdata/retired/gpt_tier_rekey_retired_keys_corpus.json`), so the three
  copies of the record cannot drift apart. Two rows are why the record cannot be written
  from assumption: `gpt-luna` (and its `sol`/`terra` twins) SPLITS, because gitlab's row is
  deliberately not stripped, and `gpt/pro` splits three ways because its rows are dated by
  two different mechanisms.

  **Three keys deviate from the epoch-wide `show`-seam expectation, and the deviation is
  recorded rather than repaired.** `ministral#3b{instruct}`, `mistral/large#675b{instruct}`
  and `nemotron#120b` still RESOLVE at plain `bestiary show`, because each remains a valid
  **under-specified reference** to exactly one live entity: the successor carries a version
  the retired key did not, so a ref omitting the version still names one model. Nothing was
  added to let a retired key resolve: the exact-key seams (`bestiary.EntityByKey` and
  `GET /entity/<key>`) are a hard 404 for all **26**, and making these fail would mean
  breaking ordinary under-specified lookups whenever they happen to match a retired
  spelling. `show --by-entity` is a different lookup again — an exact match over the
  store-overlaid entity index (entity key, entity preferred name or concrete model id),
  with no short-reference path — and it reports not-found for all 26. Nine further keys
  report the under-specified error because their FAMILY survives them, exactly as `show gpt`
  and `show claude` always have; the remaining fourteen are 404 on both seams.

  **Library consumers get a compile break, which is louder than a 404.**
  `entities_constants_gen.go` loses **26** `Entity__` declarations and gains **14**,
  counted from the file:

  removed — `Entity__Agi`, `Entity__Devstral__Size_123b`, `Entity__Gemma__Size_12b`,
  `Entity__Gemma__Size_26b_a4b`, `Entity__Gemma__Size_4b`, `Entity__Gpt__Pro`,
  `Entity__Gpt_luna`, `Entity__Gpt_luna__Pro`, `Entity__Gpt_luna__Pro__Version_5_6`,
  `Entity__Gpt_luna__Version_5_6`, `Entity__Gpt_sol`, `Entity__Gpt_sol__Pro`,
  `Entity__Gpt_sol__Pro__Version_5_6`, `Entity__Gpt_sol__Version_5_6`,
  `Entity__Gpt_terra`, `Entity__Gpt_terra__Pro`, `Entity__Gpt_terra__Pro__Version_5_6`,
  `Entity__Gpt_terra__Version_5_6`, `Entity__Kimi_k2__Code`,
  `Entity__Ministral__Size_3b__Instruct`, `Entity__Ministral__Size_8b__Instruct`,
  `Entity__Mistral__Large__Size_675b__Instruct`, `Entity__Mistral__Mini__Size_3b`,
  `Entity__Mistral__Small__Size_24b`, `Entity__Nemotron__Size_120b`,
  `Entity__Nemotron__Size_30b_a3b`;

  added — `Entity__Agi__Version_01`, `Entity__Devstral__Version_2__Size_123b`,
  `Entity__Gpt__Luna`, `Entity__Gpt__Luna__Version_5_6`,
  `Entity__Gpt__Luna__Version_5_6__Pro`, `Entity__Gpt__Sol`,
  `Entity__Gpt__Sol__Version_5_6`, `Entity__Gpt__Sol__Version_5_6__Pro`,
  `Entity__Gpt__Terra`, `Entity__Gpt__Terra__Version_5_6`,
  `Entity__Gpt__Terra__Version_5_6__Pro`,
  `Entity__Ministral__Version_3__Size_14b__Instruct`,
  `Entity__Ministral__Version_3__Size_3b__Instruct`,
  `Entity__Nemotron__Version_3__Size_120b`.

  The name shape also flips for the tier keys, `__Pro__Version_5_6` becoming
  `__Version_5_6__Pro`, because `pro` moves from a path segment to an identity modifier.

  Census effect, measured over this slice's own two runs: the tier re-key alone takes
  entities **945 → 942** (12 out, 9 in), series lines 415 → 410, versioned lines 209 → 207,
  bare lines 206 → 203, releases 654 → 648, canonical nomina 945 → 942 (total 3,959 →
  3,956). The leading-token strip then takes entities **942 → 933** (14 out, 5 in), series
  lines 410 → 411 (the one new `agi` gen-01 line; bare lines unchanged), releases 648 →
  646, canonical nomina 942 → 933 (total 3,956 → 3,947), sized catalog entities 317 → 310.
  Provider-id (**2,834**), alias (**1**) and HuggingFace (**179**) nomina are re-measured
  UNCHANGED across both: a re-keyed instance carries its own id spelling across as an
  Admitted nomen, and the one harvested Hub repo whose entity moved keeps its value while
  its `ResolvesTo` is re-pointed.

### Added

- **A budget-first VRAM fit calculator at `/calculator`, with a typed weights basis.**
  The detail page answers "I have this model, what does it cost?"; the calculator
  reverses the direction and answers "I have this much VRAM, what runs?". State a budget
  and an adjustable headroom, and the page lists only rows whose weights clear
  `budget − headroom`, largest first, each with the greatest context it can afford and
  **which limit produced that figure** — the budget or the model's own window. The
  arithmetic lives in the root package (`fit.go`: `FitOver`/`Fit`, `FitBudget`,
  `FitFilter`, `FitRow`, `FitResult`, `DerivedWeightsBytes`), so it is unit-testable
  without an HTTP server and reusable by the CLI later; `cmd/bestiary-web` only renders
  it. `FitOver` is pure over the entities it is given.
  - **Headroom is presentation-layer view state, and its preset is deliberately
    non-zero.** The shipped formula carries no runtime-overhead term
    (`VRAMFormulaVersion` stays **2**), which is correct for a stored datum but means a
    fit verdict computed from it alone over-promises. The slack is a control the reader
    owns and can see, never a constant smuggled back into the data: nothing on this path
    writes to `QuantVRAM`, `WeightsBytes` or `VRAMBytes`, and no context setting changes
    any displayed VRAM figure.
  - **Two non-fits are named rather than rounded off.** A row whose KV-cache term is not
    computable reads **unknown** — never an unbounded context, because an absent
    architecture fact is not an infinite budget — and a row whose computable KV budget is
    spent reads **no context budget remaining**. A positive minimum-context filter
    excludes both, since neither can promise the reader a token.
  - **`WeightsBasis` types where a weights figure came from**, with `BasisMeasured` at
    enum zero so any value that never saw the type reads as the ingested file size it is.
    A **derived** row — an entity with an attested `TotalParams`, at least one instance,
    and no ingested quant row anywhere — estimates weights as parameter count ×
    bits-per-weight and is badged `derived · weights-only`, naming **both**
    qualifications: the figure is an estimate, and its KV term is missing. No unmeasured
    entity in the catalog publishes layers / KV heads / head dim, so every derived figure
    is a lower bound and the page says so.
  - **The exclusions are structural, not a runtime policy.** An entity whose parameter
    shape returns the `ParamShapeNull` sentinel — the `NxM` tokens, whose product
    `parse.go` deliberately refuses to compute, and the `Nb-Ke` tokens, which publish only
    an active count — produces **no** derived row and is counted excluded. Deriving from
    `ActiveParams` instead would understate residency by up to 26.7× and would render
    Llama 4 Scout and Maverick identically. The six quantization members whose
    `BitsPerWeight()` is 0 (`none`, `awq`, `gptq`, `int8`, `int4`, `other`) produce **no**
    row rather than a zero-byte one, which would read as fitting any budget. An entity no
    provider serves gets no row at all.
  - **The coverage statement is computed, never written.** The sentence heading the
    table interpolates `FitResult.EntitiesConsidered` / `EntitiesMeasured` /
    `EntitiesDerived` / `EntitiesExcluded` at request time; the tests assert the
    **identity** between the rendered numbers and those fields, and recompute each field
    from its own predicate over the same entity slice. No count is a literal anywhere on
    the page or in its tests. The render cap is stated in words whenever it bites, so a
    truncated table never reads as everything that fits.
  - Patches arrive over `GET /sse/calculator` into `#calc-results` through the vendored
    Datastar client; the entity browser's `#entity-results` seam is untouched.

- **Creator-first resolution, layered above `CanonicalProvider` rather than replacing
  it.** A new curated `Creator → [Provider]` distribution relation
  (`parse/data/creator_providers.json`, 24 rows / 52 pairs) records the hosting
  surfaces each lab operates for its OWN models, and `Creator.Providers()` exposes them
  in **curation order** — the lab's primacy order, which is load-bearing: Zhipu leads
  with its own `zhipuai` API ahead of the international `zai` brand. All five
  provider-preference sites (`resolve.go` ×4, `format.go` ×1) now consult one shared
  authority that ranks a creator surface above the canonical provider above a rehost.
  `Family.CanonicalProvider` is unchanged and still consulted in full; it is the layer
  beneath, and a family with no creator, no curated distribution row, or no
  creator-hosted candidate resolves exactly as before. **77 distinct exact model IDs
  change their rendered provider**, every one of them from a rehost or router to the
  lab's own surface — `llama-3.3-70b-instruct` reports `llama` instead of `azure`,
  `glm-4.6` reports `zhipuai` instead of `302ai`, and the `claude-*` line reports
  `google-vertex-anthropic` instead of the generic `google-vertex`. Multi-lab hubs
  (`modelscope`, `huggingface`) are deliberately NOT distribution surfaces: they host
  many labs on the same footing, so listing one would rank a hub above a genuine rehost
  while saying nothing about first-party hosting.
- **The ambiguity listing names both axes separately.** `FormatAmbiguous` renders a
  `Creator:` section (rows marked `+`) before the existing `Canonical:` section (rows
  marked `*`), each suppressed independently when empty, with each row assigned to at
  most one section so a provider that satisfies both — Anthropic creates AND hosts
  Claude — is listed once. This closes the v0.2.8 Impl-UAT finding that the user-facing
  message conflated "there is one canonical creator (Meta)" with "there is no canonical
  provider". `Also rehosted by:` now excludes BOTH axes, so a lab's own surface is no
  longer listed as a rehost of the lab's own weights.
- **Lab-prefix derivation for the creator dimension.** Every models.dev metadata id is
  lab-scoped, so `DeriveCreatorLabDisagreements` projects that assertion onto the family
  the JOIN'S OWN decomposition maps the row to (a curated `modelsdev_aliases.json` entry
  is the sole identity; otherwise `stripMetadataLab` + `ParseFamilyDetailed`). The
  catalog carries **24 distinct lab prefixes** across 263 metadata rows — 20 of the 24
  are also Provider tokens, 4 are not — reaching **40 families**, 38 by exactly one lab.
  The derivation is **report-only, never self-applying**: a new committed emission
  `parse/data/creators_lab_disagreements.json` lists every family whose evidence
  conflicts, classified `multi-org` / `spelling-variant` / `divergent` / `withheld`.
  It currently carries **4 rows**, and auto-applying any of the 3 mechanical ones would
  have recorded a WRONG creator: `llama` and `mistral` are claimed by both their own lab
  and NVIDIA's re-publications, and `glm`'s lab spells itself `zhipuai` against the
  curated `zhipu`. The fourth, `ling`, is **withheld** through a new `withheld` array in
  `creators.json` — its only lab-scoped row reaches it through an alias retargeting
  `thinkingmachines/inkling`, so seeding it would attribute InclusionAI's line to the
  wrong lab; the withholding carries its reason and is re-reported on every regen so it
  cannot decay into an unexplained gap.
- **Curated `Creator → [Provider]` coverage report.** A second committed emission
  `parse/data/creator_providers_unserved.json` lists every curated pair whose provider
  serves no instance of any of that creator's families, so aspirational or stale
  curation is visible rather than silent. It is **empty** at this commit — the healthy
  steady state. Both new emissions follow the INV3 contract (explicit `sort.Slice`, no
  wall clock, empty list rather than null) and are now covered by the codegen
  reproducibility harness: `runFixtureCodegen` was widened to a `codegenArtifacts`
  struct so `TestCodegen_Reproducible_ByteIdentical` (N=100) compares them alongside the
  three generated `.go` sources. Neither `.go`-only codegen guard could reach a JSON
  report before, so an emission built from a map range was previously unguarded.
- **A ⌘K command palette on every page — an ARIA combobox in a native `<dialog>`, with
  zero new dependencies.** `⌘K` / `Ctrl-K` (and a chrome button, replacing the disabled
  placeholder search box) opens a modal palette; type-ahead patches `#palette-results`
  through a new `GET /sse/palette`, `↑`/`↓` move `aria-activedescendant` while DOM focus
  stays in the input, and `Enter` navigates to `/entity/<key>`. The scope is entity
  **search and navigate ONLY**: there are deliberately no page-navigation or view-action
  rows, so `Enter` means the same thing whatever is highlighted. Nothing was added to
  `go.mod` and the vendored `assets/datastar.js` is untouched — the global hotkey is the
  client's own `data-on-keydown__window` binding, the modal scrim / focus trap /
  Esc-to-dismiss are the platform's `<dialog>`, and the option rows are server-rendered
  through the SAME SSE fragment seam the entity browser already used (`ReadSignals` →
  `NewSSE` → `PatchElements`), differing only in target element. Matching is ranked —
  canonical-key prefix, then key substring, then an attribution (family/creator) match —
  capped at 10 options, and the cap is **stated in the popup** (`showing 10 of N`) rather
  than silently swallowing matches. An empty query offers nothing at all, so a reader who
  has typed nothing cannot press `Enter` onto a row they never chose. `templates/palette.html`
  is parsed into BOTH the page sets and the SSE fragment set, so the dialog's opening state
  and the server's patched state are one rendering rather than two that could drift.

### Changed

- **The v0.2.4 weights invariant is amended in the open, not quietly widened.** Three
  shipped doc comments said, in effect, that a weights figure is never derived from
  bits-per-weight: `QuantVRAM.WeightsBytes` (`entity.go`), the weights-term paragraph in
  `vram.go`, and `Quantization.BitsPerWeight` (`quantization.go`). Each is rescoped in
  the same commit as the enum that makes a derived figure possible: the invariant governs
  what is **stored**, and the separately-typed display-time projection that now exists is
  never written back. `VRAMFormulaVersion` stays **2**.

- **The curated `Family → Creator` seed grows from 18 rows to 75, and `Creators()` from
  9 to 41.** Rows are grouped by provenance in the data file: the 18 original
  UAT-confirmed rows, 27 rows applied from the lab derivation, and 30 hand-curated rows
  for families the metadata join never reaches (a family with catalog entities but no
  `models.json` row has no lab prefix to derive from). Curated rows WIN over lab-derived
  values. The 41 tokens are `9 seed + 14 lab-derived + 18 curated-unreached`, where the
  14 is `24 lab prefixes − 8 already-seeded labs − 1 spelling variant (zhipuai) − 1
  withheld (thinkingmachines)`. This collapses the front-page creator grouping from
  **251 top-level groups to 226** (unit: distinct non-empty `Creator` values plus
  families that remain unattributed, over the 254 distinct families of the 957-entity
  catalog). The long tail is deliberately left unattributed rather than guessed at: 152
  of the 254 families carry exactly one entity and many of those tokens are decomposition
  artifacts (`free`, `cheap`, `coder`).
- Adding a NEW creator token is documented on `Creators()` as costing **five** authoring
  parts that must move together — the `creators.json` row, the `Creator` constant, the
  `knownCreators` entry, the `Creators()` length pin, and the `creatorExpr` case in
  `cmd/bestiary-gen` (omit the last and codegen silently bakes the untyped fallback
  `Creator("token")` instead of the constant). Two new consistency guards enforce both
  directions: every creator in `creators.json` satisfies `Creator.IsKnown()`, and every
  well-known `Creator` is referenced by at least one row, so the set cannot accumulate
  dead tokens.
- Six creator rows map families no catalog entity currently carries (`claude-haiku`,
  `claude-opus`, `claude-sonnet`, `command-a`, `command-r`, `o`). Their disposition is
  **RETAIN**, on the rationale `family.go` already gives for keeping `FamilyO` in
  `CanonicalProvider`: each is still a real `raw_family` value the upstream catalog
  emits, so a residual row resolves to its lab rather than falling through. A test pins
  both halves — that the rows still resolve, and that they are still at zero entities —
  so "dead" stays measured rather than assumed.
- `curatedBaseFamilies` gains `c4ai`, `ornith`, `qwq` and the lowercase `hy`. All four
  carry real catalog entities but are absent from the generated family set, and the
  creator table's FK gate requires `Family.IsKnown`. `hy` is registered as a literal
  because the generated set already binds the identifier `FamilyHy` to a DIFFERENT
  Family VALUE — the upstream mixed-case `"Hy"` — while every entity the decomposition
  produces carries lowercase `"hy"`; family comparison is byte-exact, so the two are
  distinct families and reconciling them is a family-set repair, not a creator one.
- Reground the `cmd/bestiary-gen` decomposition test corpus on the vendored codegen
  catalog (`parse/data/modelsdev/catalog.json`), replacing a fixture that had fallen
  786 records behind it: 4,979 → 5,765 records over 170 providers. The frozen
  decomposition baseline was re-captured in the same commit, so the path-unification
  diff reads `records=5765 changed=0` — no entity key moves and no census pin changes.
  The refresh surfaced four **real** cross-provider divergences the stale fixture had
  hidden (`text-embedding-3-small`, `text-embedding-3-large`, `poolside/laguna-s-2.1`,
  `sakana/fugu-ultra`); they are carried as enumerated, individually justified residuals
  rather than curated away.
- **Plan amendment — `Creator.Providers()` is deterministic, not alphabetical.** The
  implementation plan specified the accessor return a "sorted, deterministic" slice;
  it returns **curation order** instead, and that is a deliberate amendment rather
  than an oversight. Determinism is satisfied by the committed
  `parse/data/creator_providers.json` (same file → same slice on every call and every
  build); alphabetising it would not be a cleanup but a behaviour change, because
  creator-first selection uses the slice INDEX as its primacy tie-break, so a sort
  would silently change which of a lab's surfaces a model resolves to. Emissions a
  reader would expect sorted keep their own explicit sorts:
  `parse/data/creator_providers_unserved.json` sorts on `(creator, provider)` and
  `parse/data/creators_lab_disagreements.json` sorts on `family`.

### Fixed

- **NVIDIA's Nemotron Super 49B v1.5 is served from one key, and the bare 49B key stops
  holding two different models.** nano-gpt spells that artifact with underscores
  (`nvidia/Llama-3_3-Nemotron-Super-49B-v1_5`) where every other provider uses dots.
  Underscores are not a separator the decomposition splits on, so neither `3_3` nor `v1_5`
  was reachable as a token: the row arrived with an empty variant and version and keyed the
  bare `nemotron#49b` line — which already held the genuinely different Super-49B **v1**.
  An exact-id pin puts it on the `(nemotron, v1.5, 3.3)` tuple its dotted siblings already
  converge on.

  **This moves one instance and retires no entity key, so it carries no migration table and
  falls outside the retired-key policy entirely.** Both keys already existed at the baseline
  and both survive; measured, the entity key set is byte-identical before and after
  (`entities_constants_gen.go` does not change), and the whole effect is an instance diff:
  `nemotron#49b` goes 2 → 1 instance (keeping the v1 row alone) and
  `nemotron/v1.5@3.3#49b` goes 2 → 3. It is **not** a split — `nemotron#49b` stays live —
  and nothing is orphaned: the instance total across the pair is conserved at 4. A committed
  test pins the exact instance membership of BOTH keys, because asserting only the arrival
  would leave a retirement of the bare key indistinguishable from a re-home
  (`cmd/bestiary/testdata/rehome/nemotron_rehome_corpus.json`). Census unchanged: entities
  945, series 415, releases 654, canonical nomina 945.

- `synthesizeStandaloneEntity` never projected `Entity.Creator`, so a metadata-only
  standalone reported an empty creator even when its family was mapped. The invariant
  "`Entity.Creator == Ref.Family.Creator()` for every entity" held only by accident —
  no synthesized family had a curated creator until the `ornith` rows gained one.
- **Self-referential `bestiary` data source.** `DataSourceBestiary` (`"bestiary"`) joins the
  BCNF data-source dimension as a fifth curated row
  (`parse/data/datasources.json`, uri `https://github.com/dayvidpham/bestiary`). It is the
  honest `Source` for anything bestiary **authors** rather than reads from an upstream, and it
  is deliberately distinct from `curated` — `curated` is the ingest a *third-party* claim was
  transcribed from, `bestiary` is a claim with no third party at all. `sources --export`
  carries the new row and round-trips it, so the export stays promotable straight back into the
  curated seed.

### Changed

- **Self-minted canonical nomina are attributed to `bestiary`, not `models.dev`.** Both shared
  mint joints — `MintNomina` (from entities) and `MintNominaFromModels` (the sync path), which
  previously hard-coded `DataSourceModelsDev` at their canonical call sites — now emit
  `Source = bestiary` on every canonical attestation. A canonical key is rendered by bestiary's
  own parse + key pipeline, so naming an upstream credited it with a claim it never made.
  `canonicalAttestation` takes no source argument any more, which makes the FK
  impossible to get wrong at a call site. `Authority` (`primary`) and `Method` (`self-minted`)
  are **unchanged**, and no nomen **count** changes (957 canonical / 2834 provider-id / 1 alias
  / 179 huggingface, unchanged; the from-models joint stays at canonical − 4).
- `sync` now registers the `bestiary` dimension row alongside `models.dev`, `curated` and
  `huggingface` before persisting nomina, since `nomen_attestations.source_id` is a real
  foreign key.

### Notes

- **A SQLite cache written by a pre-v0.2.10 build keeps its old `models.dev` FK** on
  canonical-nomen attestations until it is re-synced. This is deliberate, not merely tolerated:
  the FK records *which ingest we read a naming from*, and for a cached row that ingest genuinely
  was the pre-v0.2.10 pipeline — rewriting it would back-date a claim the old build never made,
  which is exactly the provenance dishonesty this row exists to fix. Run `bestiary sync` to
  re-mint the attestations; the value corrects itself.
- **`NomenAttestation.ArchivedURL` — an archive.org snapshot for harvested namings.**
  A curated naming claim already had to cite a snapshot, because a lab's model card is
  edited and deleted without notice. A *harvested* naming could not: it cites the LIVE
  page the bot observed, which is precisely the thing that rots. The snapshot now rides
  **beside** that citation instead of replacing it — `SourceURL` stays primary and
  unchanged, and `ArchivedURL` carries the archive.org capture of it. Empty means "no
  snapshot recorded", an honest unknown rather than an error, and it is always empty on a
  curated claim (whose `SourceURL` already *is* the snapshot). The curated archive-only
  fence is untouched — not relaxed, moved or duplicated.
  - `cmd/bestiary-hf` looks the snapshot up from the Internet Archive **Availability
    API** (read-only; never Save Page Now) through the **same** `politebot.Client` as the
    Hub crawl, so the ≥1 s cadence is enforced across both hosts by one seam and no new
    backoff is added — the existing `Retry-After` handling is inherited as-is. Every
    not-a-snapshot outcome is a **miss, never an error**: the documented
    `{"archived_snapshots":{}}` shape, any final non-2xx (a post-retry 429 included), an
    unparseable answer, or a URL that fails the shared archive-snapshot shape check. A
    miss also never **erases** a snapshot an earlier run recorded — the archive does not
    un-capture a page, so a throttled refresh must not destroy durable evidence.
  - The seed's `archived_url` field is optional and omitted when absent, so a repo the
    archive has never captured leaves the committed file byte-identical.
  - `IsArchiveSnapshotURL` is exported as the ONE shared shape check, so the curated
    fence, the suppression fence and the harvested layer cannot drift apart on what an
    archive URL looks like.

**Schema:** `0.6.0` → `0.7.0` (additive). SQLite store schema `8` → `9`.

- `bestiary.schema.json`: `ArchivedURL` joins `$defs.NomenAttestation` `properties` **and**
  `required` (all six — a `NomenAttestation` carries no json tags, so every field always
  serializes). `BestiarySchemaVersion` is `0.7.0`, the single bump of this epoch.
- SQLite store `8` → `9`: `nomen_attestations` gains `archived_url TEXT NOT NULL DEFAULT ''`
  via a presence-guarded `ALTER TABLE ADD COLUMN` self-heal read from `pragma_table_info`.
  The migration is purely additive — no table is dropped, recreated or reordered — so every
  pre-existing row survives byte-identical with an empty `ArchivedURL`, and re-running it is
  a no-op.

| store schema | table | change |
|---|---|---|
| `8` → `9` | `nomen_attestations` | `+ archived_url TEXT NOT NULL DEFAULT ''` (appended; presence-guarded self-heal) |
### Fixed

- **Canonical segment binding no longer mis-reads a version token as a variant.** A
  canonical ref is bound to `(family, variant, version)` by slot position, and a provider
  prefix was consumed by shifting the segment slice left — but the residue was then re-read
  positionally with no memory of which slot the un-stripped form implied. A trailing
  version token therefore landed in the *variant* slot and could never match a
  variant-empty entity, so `ling/2.6` and `ant/ling/2.6` were both `model not found`, and a
  variant-empty ref could not be addressed with a provider prefix at all. The repair adds
  three rules — a two-segment provider strip guarded on the candidate's own `Provider`
  field, a variant-empty version rebind written as an if/else (never an abort, which would
  drop thousands of resolvable refs such as `302ai/claude/haiku@4.5`), and a
  date-to-version rebind gated on a provider segment having actually been stripped.
  `ling/2.6` and `ant/ling/2.6` now resolve uniquely to `ling@2.6#1t`; `openai/gpt@5.1`
  returns a scoped `ErrAmbiguous` over exactly its two openai-served candidates, and
  `gpt/5.1` / `openai/gpt/5.1` over their groups.

  Every new rule lives in a **second pass that runs only when the first pass matched
  nothing anywhere in the registry**, so "nothing matched" is a property of the match
  *set*, never of one model: a ref that already resolved is untouched by construction. A
  further **base-first preference** keeps a rebound version segment on the variant-empty
  artifact whenever one exists, and only lets it reach variant-carrying siblings when the
  catalog holds no variant-empty artifact at that version.

  The `providerStripped` gate on the date-to-version rebind is load-bearing and was found
  by measurement, not by reasoning: without it, **486 of 957 entity keys** silently lose
  their `bestiary show` aggregate entity view, because `show` reaches that view only when
  model resolution *misses* — a resolver-only sweep stays green while the CLI regresses.
  Guards now run at **both seams**: an invariant sweep over every entity key at the
  resolver, and a full-census sweep at the `show` seam.

### Added

- `testdata/resolve/segment_binding_corpus.json`, driven at the peasant seam
  (`Resolve(ref, WithInputFormat(InputFormatPeasant))` — what `bestiary show` passes, and
  where the must-not-widen refs are actually reachable). Each row pins its **candidate
  set** rather than its outcome class: the entity keys the candidates span, plus the
  provider-level ref set wherever a widening would be invisible at the identity level. It
  carries the repaired refs, the six known falsifiers, the pinned entity-view guards, and
  one held-open composition-witness slot.
- **`Entity.MetadataAll` — every metadata row an entity is named by.** Distinct
  models.dev identifiers routinely decompose to one entity key (a dated alias and its
  floating alias; a serving tier that is not a distinct artifact), and `Entity.Metadata`
  was a single pointer, so the join kept only the row it visited last. Measured over the
  baked corpus at this change's baseline (unit: metadata rows / benchmark claims / links;
  axis: the whole registry; configuration: the committed
  `parse/data/modelsdev/catalog.json` snapshot, offline, no store overlay): **39 of 263
  rows were unreachable, taking 103 benchmark claims and 15 links with them** — 224
  distinct entities carried metadata. All 263 rows (508 claims, 105 links) are now
  reachable. The witness is `gpt@5.5`, named by both `openai/gpt-5.5` and
  `openai/gpt-5.5-instant`: the **31 claims** reported under `openai/gpt-5.5` previously
  rendered nowhere, because the instant row won the single pointer.
  `MetadataAll` is sorted ascending by `MetadataID`; `Entity.Metadata` becomes a derived
  projection of it — the shortest `MetadataID`, ties lexicographic ascending. That is a
  naming rule, not a payload rule: a lab's canonical identifier is its shortest one, so
  the primary is stable across re-ingest and independent of how many claims a row carries
  or the order rows arrive in.
- **Per-identifier claim attribution** in `show --by-entity` and on the web entity page:
  every joined row is listed (the primary marked) and each benchmark table is headed by
  the `MetadataID` the claims were reported under. Claims from different identifiers are
  never merged into one table — a score is a **lab-reported claim** attributable to the
  identifier the lab published it under, and fusing two rows would present an assessment
  record no lab actually published. `docs/CONCEPTS.md` gains this framing.

### Changed

- `JoinEntityMetadata` is now **idempotent** as well as pure: `MetadataAll` is
  cleared-then-accumulated per entity touched in the call, so re-joining an
  already-joined set replaces the record instead of doubling it, while an entity no row
  lands on keeps the record it arrived with. Entity clones copy `MetadataAll`
  **element-wise**, so a returned row's benchmark table never aliases registry-owned
  storage. Two metadata rows sharing one absent-family key now accumulate onto a single
  synthesized standalone rather than producing a duplicate entity.
- The sync overlay's baked base layer is rebuilt from every metadata row rather than one
  row per entity. `MergeEntityMetadata` unions the base against synced rows per
  `MetadataID`, so a row missing from the base could not survive as a baked-only row
  after a sync. The `entities` table's `BENCHMARKS` column likewise sums claims across
  every joined row.
- **Schema:** `bestiary.schema.json` `$defs.Entity` gains `MetadataAll` (an array of
  `EntityMetadata`, additive and **not** required). No SQLite store migration: the
  `entity_metadata` table is already keyed by the stable `metadata_id`, one row per lab
  identifier, so the multi-row record is a join-layer property the existing schema
  already carries — proven by a full-corpus store round-trip test.

### Added

- **`CreatorGroups()` / `SeriesGroups()` — a browsable Creator > Family > Series > entities
  projection.** A read-only view over relations that already exist (the curated
  `Family`→`Creator` seed and the computed Series/Release taxonomy); like the taxonomy it is
  computed on read and can never rename an entity. It exists because `SeriesAll()` alone is
  flat — several hundred lines with no organizing level above them — so the projection puts
  the two questions a reader actually arrives with, "whose models?" then "which line?", above
  the generation-level detail. The creator set is derived from the curated seed, so growing
  `creators.json` grows the tree with no code change.
- **The base hoist: no `(base)` node anywhere.** A series' un-named release is not a member
  of the line alongside the named ones — it *is* the line, un-named. Rendering it as a
  sibling node called `(base)` invented a level that does not exist and buried the entities
  of a line with no named releases one click deeper than the entities of a line that has
  them. Those entities are now hoisted onto the series itself (`SeriesGroup.Hoisted`), and
  only genuinely named releases remain as releases. `SeriesGroup.Shape()` reports which of
  `base-only` / `mixed` / `named-only` a line is, so a renderer can lay out the two levels
  correctly (a mixed line shows its hoisted entities above, and visually distinct from, its
  release disclosures). The hoist is a re-parenting, never a filter: a test asserts the
  projection is an exact partition of `Entities()`, so an implementation that dropped the
  un-named entities instead of lifting them fails loudly rather than rendering a plausible
  but smaller tree.
- Entities re-homed by the curated strays table (`series.json`) are shown under the line
  they were re-homed onto, which is the point of the table — so a stray can appear under a
  creator its own family token does not name. A test pins that this divergence is confined
  to strays and can never become a silent misattribution of a normal entity.

### Changed

- **Web: the front page is now a Creator > Family > Series > entities tree (`GET /`).** A
  reader arriving with no query in mind was previously handed a nine-hundred-row table; they
  now get a hierarchy to walk, starting from the lab that trained the weights. Attributed
  creators arrive expanded; families with no curated creator collect in a single collapsed
  group at the bottom. The tree renders whatever creators the curated seed carries, so it
  grows with `creators.json` and hard-codes no lab.
- **Web: the dense entity browser moved from `/` to `GET /entities`, unchanged.** Same seven
  signals, same `#entity-results` SSE patch target, same facets — only its address changed,
  and it is one click from the tree.
- **Web: `GET /families` absorbs the series/release explorer**, emitting the SAME per-series
  anchors, so every detail-page series link still resolves.
- **Web: no `(base)` node on any page.** Both hierarchy pages render one shared partial over
  the new hoisted projection, so the un-named release's entities attach directly to their
  line. On a line that has both, the hoisted entities render above the release disclosures
  with a legend and a rule marking them as a different level of the hierarchy — adjacency on
  screen must not be read as sameness of level. Tests assert the identity on the RENDERED
  document as well as on the projection: a template can drop a branch it never ranges over,
  entirely downstream of a correct projection.
- Four cross-page links were retargeted accordingly: the browser's "browse by series" and
  the entity detail page's "‹ catalog", "series" and series-section anchor links.

### Removed

- **`GET /series` is retired and now returns a hard 404** — the route, its handler and its
  template are deleted, not repointed. It is deliberately not a 301: an alias keeps a dead
  name alive in links, bookmarks and search results indefinitely, which is what retiring the
  name was meant to stop. No content was lost — `/families` absorbed it and emits the same
  anchors.

- **Web: a readability type scale governs every text size.** The `bestiary-web` stylesheet
  gained a six-step, rem-based type scale (`--fs-xs` … `--fs-xl`) and every `font-size` in
  the layout now resolves through it — there is no literal font-size value left in the
  stylesheet, and a test guard keeps it that way. Because the steps are rem-based, a reader
  who raises their browser's base font size now scales the whole page with it; the previous
  px literals ignored that preference outright. Each former size moves up exactly one step
  (12→13, 13→14, 14→15, 15→16 px at the browser default), so the dense entity grid keeps its
  density while the smallest text on the page stops being 12px. Headings, which previously
  carried no explicit size at all, are on the scale too. The steps are named `--fs-*` rather
  than `--text-*` because `--text` and `--text-muted` are already colour tokens. No selector
  was renamed.

- **Entity key retired: `qwen/coder@3#1m`.** The unprefixed spelling `qwen3-coder-next-fp8-1m` (provider InferX)
  escaped the 1M-context suppress-pin that already covered the `qwen/`-prefixed spelling, so its `1m` context marker
  was keying off a phantom `#1m` size entity holding a `TotalParams` of 1,000,000. Both spellings are now pinned;
  the instance rejoins `qwen/coder@3`. Measured at this commit (unit: entity keys; axis: the full constant set in
  `entities_constants_gen.go`; configuration: this pin alone, applied on top of the free-demotion baseline in the
  section below): 940 → 939, one key retired, none renamed, none added. Retired-key policy: hard 404 on
  `bestiary show` and `GET /entity/`, no alias.

  | old key | new home | removed constants |
  |---|---|---|
  | `qwen/coder@3#1m` | instance rejoins `qwen/coder@3` | `Entity__Qwen__Coder__Version_3__Size_1m` (1 declaration) |

- **Three entity keys were one: the `ling` / `inkling` / `kling` collision is split — 939 → 947 keys,
  2 retired and 10 added.** The vendored models.dev catalog stamps `"family": "ling"` on **all 14**
  rows of two product lines that have nothing to do with inclusionAI's Ling: Thinking Machines'
  6 Inkling instances and vercel's 8 `klingai/kling-v*` video models. Bare `ling` was therefore not an
  inclusionAI entity at all — it held those 14 mislabelled rows and none of inclusionAI's own. Two
  curated `family_enforce.json` ledger rows (`inkling`, `kling`) now let the ID-driven decomposition
  win over the upstream label, splitting all 14 with **no exact-ID pins**, and the
  `parse/data/modelsdev_aliases.json` row for `thinkingmachines/inkling` is retargeted from `ling` to
  `inkling` so its metadata follows the entity.

  **The root cause is upstream's label, not our parser.** The earlier reading — "vercel drops the
  leading `k`" — is **false**: vercel spells its rows `klingai/kling-v2.5-turbo-i2v`, leading `k`
  intact, and every other provider's id is equally correct. Our decomposition was right all along and
  was simply being overruled by a wrong `raw_family`. That is why the fix is a ledger entry rather
  than 14 pins.

  A fifteenth row needed a different lever. qiniu-ai's `kling-v2-6` carries **no** upstream family, so
  the key was entirely our own: the leading-token pipeline glued the dash-spelled major onto the family
  token and read the orphan as the whole version, producing `kling-v2@6` — a phantom family at a version
  the vendor never published. An exact-ID `idFamilyOverrides` row corrects family *and* version together
  to `kling@2.6`. A `dotLostVersionOverrides` entry would **not** have worked: by construction it
  corrects `Version` only, leaving `kling-v2@2.6` with the corrupted family standing.

  **inclusionAI's ling keyspace is untouched.** `ling#1t`, `ling@2.6#1t`, `ling/flash@2.0`,
  `ling/flash@2.6` and `ling/flash-free@2.6` are byte-identical before and after, each holding exactly
  the instances it held before (2, 4, 2, 4 and 1), asserted by value in a committed test. The `ling`
  **family** survives with those five children; only the bare `ling` **key** retires.

  The 8 klingai shape tokens are **normalized** — this was deferred when the split landed and has since
  been pulled in; see the dedicated entry at the top of this release for the axis-by-axis reasoning and
  the measured diff. The successor column below therefore names the normalized keys.

  **Migration table (old → new).** Each row is DERIVED, not assumed: every retired key's pre-split
  instances are pinned in a companion corpus, the successor set is re-derived from them against the
  live registry on every test run, and this table is compared against that derivation.

  | retired collision key | instances re-home to |
  |---|---|
  | `kling-v2@6` | `kling@2.6` |
  | `ling` | `inkling`, `kling/i2v@2.5{turbo}`, `kling/t2v@2.5{turbo}`, `kling/i2v@2.6`, `kling/motion-control@2.6`, `kling/t2v@2.6`, `kling/i2v@3.0`, `kling/motion-control@3.0`, `kling/t2v@3.0` |

  **`ling` is this epoch's widest split — nine successors, and it does NOT fold onto its family line.**
  Looking for your model under bare `ling` will not find it: six of its rows are Inkling and eight are
  Kling video models, and the surviving `ling` children are unrelated inclusionAI weights.

  **Retired-key behaviour, measured — the two keys differ, and that is correct.** `kling-v2@6` is a
  uniform hard 404: `ErrNotFound` on both `bestiary show kling-v2@6` and
  `bestiary show kling-v2@6 --by-entity`. Bare `ling` returns `ErrNotFound` on the
  `--by-entity` seam but **`ErrAmbiguous`** on the looser `show` seam — not because it
  split, but because its **family outlives the key** and the bare family token still has
  five live children, exactly as `show gpt`, `show claude` and `show mimo` behave. The
  asymmetry is in the seams, not in the key: `--by-entity` is an exact match over the
  store-overlaid entity index (entity key, entity preferred name or concrete model id)
  with no short-reference path, so it cannot come back ambiguous, while plain `show` runs
  the input through the model resolver, which keeps that fallback. The exact-key seams are
  `bestiary.EntityByKey` and `GET /entity/<key>`. That reading is pinned as measured and
  must not be "corrected" into a 404. No alias is minted and no successor is listed by the
  tool on either seam; this table is the pointer.

  **Compile break for library consumers — 2 `Entity__` constants removed, 10 added**, counted from
  `entities_constants_gen.go`. Removed: `Entity__Ling`, `Entity__Kling_v2__Version_6`. Added:
  `Entity__Inkling`, `Entity__Kling__I2v__Version_2_5__Turbo`, `Entity__Kling__T2v__Version_2_5__Turbo`, `Entity__Kling__I2v__Version_2_6`, `Entity__Kling__Motion_control__Version_2_6`, `Entity__Kling__T2v__Version_2_6`, `Entity__Kling__I2v__Version_3_0`, `Entity__Kling__Motion_control__Version_3_0`, `Entity__Kling__T2v__Version_3_0`, `Entity__Kling__Version_2_6` (the eight video constants are named for the keys they carry at the RELEASE tip, after the variant-shape normalization below; the pre-normalization spellings never shipped). The generated file holds
  939 → 947 constant declarations over 939 → 947 distinct key values — a bijection, one declaration
  per key, so the constant break and the key diff are the same either way.

  **Creator attribution follows the split.** The `ling` withhold — deferred precisely because a curated
  alias pointed Inkling at the wrong family — is discharged and the withhold list is now empty. The lab
  derivation reaches `inkling` unambiguously, so `thinkingmachines` is applied; `ling` is left with no
  lab-scoped metadata row at all, so `inclusionai` is authored by hand. The well-known `Creator` set
  moves **41 → 43** and the codegen lab-disagreement report **4 → 3 rows**. `kling` is deliberately left
  unattributed: naming its lab is a separate curation decision.

  Downstream census, all re-measured from this regeneration: canonical nomina 939 → 947 (provider-ID
  2834, alias 1 and huggingface 179 all unchanged, so the nomen total moves 3953 → 3961 — no instance is
  created or destroyed, all 15 that move carry their id spellings across as Admitted nomina); series
  lines 415 → 417 (bare 206 → 208 for the new `inkling` and `kling` lines; versioned unchanged at 209,
  since `kling@2.6` replaces the `kling-v2@6` line one-for-one); releases 652 → 661 (+8 named kling
  shapes, +2 bare on the new lines, −1 for the retired `kling-v2@6` line; retiring bare `ling` costs
  nothing, because `ling#1t` already shares that line's un-named release).

  The codegen-emitted `parse/data/modelsdev_unlinked.json` join-disagreement report gains a
  **`count == 0` guard**, which it never had. It is added here because this is the first curation slice
  that can break it: measured, leaving the `thinkingmachines/inkling` alias pointed at `ling` while the
  entity moves to `inkling` drives the report 0 → 1 and silently orphans that row's description,
  license and benchmarks.

### Changed

- **The `free` tier leaves entity identity: 957 → 940 entity keys, 17 retired and 0 added.**
  A `-free` suffix names a pricing/serving tier a provider offers for an existing model, not
  a different weights artifact, so it now classifies as an ATTRIBUTE modifier and renders in
  the `[…]` segment instead of the key. Three curated sites carry the change —
  `parse/data/modifier_class.json` gains `global.free = "attribute"` (it was absent, so the
  unknown→IDENTITY fail-safe had been promoting it), `parse/data/modifiers.json` gains `free`
  so the tail scan peels it, and `parse/data/families.json` drops the `free` member from
  `glm`, `kimi`, `minimax` and `nemotron` so the per-family member-guard stops re-promoting
  the already-resolved variant. `qwen` keeps its `free` member and is unaffected. No instance
  is lost: the instance total is conserved and every retired key's rows re-home onto a
  surviving sibling. `claude/haiku`, `claude/sonnet` and `north/mini` gain and lose no key —
  their instances merely re-home, because `-free` had been blocking version extraction.

  **`ling/flash-free@2.6` is deliberately exempt and SURVIVES**, with `ling/flash@2.6`
  unchanged at 4 instances. It is preserved by a new exact-ID row in `parse.go`'s
  `idFamilyOverrides` (`ling-2.6-flash-free`), which is the only seam that can reach it: once
  `free` is a peelable modifier the trailing-modifier trim rewrites the raw family
  `ling-flash-free` → `ling-flash` *before* the `family_overrides.json` lookup runs, so a
  curated override row there is already dead. It is the one `*free*`-bearing key besides the
  standalone `free` and `cobuddy:free` entities to survive.

  **Migration table (old → new).** Each retired key is a hard 404 on both lookup seams —
  `bestiary show <old>` and `bestiary show <old> --by-entity` both return `ErrNotFound`, for
  17 of 17, verified and pinned as a committed test. No alias is minted, no redirect is
  added, and no successor is listed by the tool; this table is the pointer. Each row is
  DERIVED, not assumed: every retired key's pre-demotion instances are pinned in the
  companion corpus, the successor set is re-derived from them against the live registry on
  every test run, and this table is compared against that derivation — so a row cannot
  quietly name a key its rows never landed on.

  | retired key | instances re-home to |
  |---|---|
  | `deepseek-flash/free` | `deepseek/flash` |
  | `glm/free` | `glm` |
  | `glm/free@4.7` | `glm@4.7` |
  | `glm/free@5` | `glm@5` |
  | `glm/free@5.2` | `glm@5.2` |
  | `hy/free@3` | `hy@3` |
  | `kimi/free` | `kimi/k@2.5`, `kimi/k@2.7{code}`, `kimi/k@3` |
  | `laguna-s/free@2.1` | `laguna-s@2.1` |
  | `mimo/flash-free` | `mimo@2{flash}` |
  | `mimo/omni-free` | `mimo@2{omni}` |
  | `mimo/pro-free` | `mimo@2{pro}` |
  | `mimo/v2.5` | `mimo@2.5` |
  | `mimo/v2.5-free` | `mimo@2.5` |
  | `mimo/v2.5-pro` | `mimo@2.5{pro}` |
  | `minimax/free` | `minimax/m@2.1`, `minimax/m@2.5`, `minimax/m@2.7` |
  | `minimax-m3/free` | `minimax/m@3` |
  | `nemotron/free@3` | `nemotron@3` |

  Two of these were the *only* key their family had, so the `deepseek-flash` and
  `minimax-m3` families disappear entirely — which is what they always were, phantom
  families minted by a fused pricing suffix. The five `mimo/*` and `minimax-m3` rows re-home
  to a *re-keyed* target rather than a plain parent: peeling `free` exposes a version that
  the fused suffix had been hiding, so `variant="v2.5-free", version=""` resolves as
  `variant="", version="2.5"`. Those six `mimo/*` targets are stated at their FINAL spelling:
  the keyspace-wide mimo normalization below landed in the same release and re-keyed every
  mimo entity, and a migration table may only point at a key that is live when the release
  ships.

  **Two rows SPLIT rather than fold.** `kimi/free` and `minimax/free` were each a single
  key holding rows from three *different* model versions, because a fused `-free` suffix had
  been blocking version extraction on all of them: `kimi/free` held `kimi-k2.5-free`,
  `moonshotai/kimi-k2.7-code-free` and `moonshotai/kimi-k3-free`, and `minimax/free` held
  `coding-minimax-m2.7-free`, `minimax-m2.1-free` and `minimax-m2.5-free`. Peeling `free`
  separates them, so each key has three successors and **neither folds onto its bare family
  line** — bare `kimi` and bare `minimax` are live tokens with unrelated children, and
  looking there will not find these models.

  **Compile break for library consumers — 17 `Entity__` constants removed, 0 added**, counted
  from `entities_constants_gen.go`: `Entity__Deepseek_flash__Free`, `Entity__Glm__Free`,
  `Entity__Glm__Free__Version_4_7`, `Entity__Glm__Free__Version_5`,
  `Entity__Glm__Free__Version_5_2`, `Entity__Hy__Free__Version_3`, `Entity__Kimi__Free`,
  `Entity__Laguna_s__Free__Version_2_1`, `Entity__Mimo__Flash_free`,
  `Entity__Mimo__Omni_free`, `Entity__Mimo__Pro_free`, `Entity__Mimo__V2_5`,
  `Entity__Mimo__V2_5_free`, `Entity__Mimo__V2_5_pro`, `Entity__Minimax__Free`,
  `Entity__Minimax_m3__Free`, `Entity__Nemotron__Free__Version_3`. The generated file holds
  957 → 940 constant declarations over 957 → 940 distinct key values — a bijection, one
  declaration per key, so the constant break and the key diff are the same 17 either way.

  Downstream census, all re-measured from this regeneration: canonical nomina 957 → 940
  (provider-ID 2834, alias 1 and huggingface 179 all unchanged, so the nomen total moves
  3971 → 3954); series lines 417 → 415, entirely from the two vanished families (versioned
  lines stay at 209, bare lines 208 → 206); releases 669 → 652, one per retired key, each
  having been the sole occupant of its release name.

- **The modifier-class inventory guard derives its token set from the curated file.**
  `TestVC6_InventoryTokensPinned` previously restated the inventory as a hand-maintained
  token list with a `total != 21` assert, and had silently gone stale by four — the curated
  file held 25 global tokens while the test pinned 21, gated by nothing. It now reads
  `parse/data/modifier_class.json` and checks set equality in both directions, so adding,
  removing or reclassifying a global token needs no edit there and the guard cannot drift. A
  small explicitly-scoped floor of load-bearing tokens stays in code so that silently
  deleting a curated row is still caught.

### Changed

- **The mimo keyspace is normalized to `mimo@<version>{modifiers}` — the series letter
  leaves the entity key, and the ten mimo keys become nine.** `mimo/v2.5-pro`,
  `mimo/v@2.5{pro}` and `mimo/pro` were three spellings of one model, Xiaomi's MiMo 2.5
  Pro, and they now all render **`mimo@2.5{pro}`**. No `mimo/v*` key survives anywhere.

  The lever is not "delete the series letter" — that was measured and it destroys the
  keyspace, because the letter is load-bearing for version extraction and residue
  detection (dropping it reaches `mimo@2.5` zero times and mints `mimo@5`, `mimo/pro@5`
  and worse). Instead a family record can now declare **`series_letter_in_key: false`**
  (`parse/data/families.json`): the letter is still consumed to extract the version, but
  it no longer occupies the variant slot. The field is a pointer, so absent means true and
  every other letter-series family — `kimi` (`k`), `minimax` (`m`) — is unchanged by
  construction.

  Two supporting changes were each measured to be **required**, not cosmetic:
  - `parse/data/modifier_class.json` gains a **`series_tiers`** block: a per-family
    extension of the curated series-tier token set, giving mimo `flash`, `tts`,
    `voiceclone`, `voicedesign`, `free` and `ultraspeed`. The scoping is the point — an
    earlier attempt added these to the *global* set, which is shared with kimi and
    minimax, and the blast radius was six times the intended one. None of them may go in
    `modifiers.json` either: `tts` is already a family key, so a global promotion would
    collide with it.
  - the trailing-tier promotion now returns a **list**, and for a family whose series
    letter is NOT in the key the two restrictions that used to discard tiers are gone.
    Previously a tier was promoted only when there was exactly one of them and no
    capability modifier alongside it, so `mimo-v2.5-tts-voiceclone` lost its second tier
    and `xiaomi/mimo-v2-flash-thinking` lost `flash` outright. The widening is **scoped to
    mimo**, exactly like the token set above: `kimi` and `minimax` keep the letter in
    their keys and keep the original one-tier, no-co-occurring-modifier rule, so no kimi
    or minimax record changes its decomposition. The same predicate also orders the
    classification, because a token that is both a curated tier and a global modifier is
    bucketed differently depending on which test runs first.

  The seven mimo rows in `parse/data/family_overrides.json` are **retained and rewritten
  to an empty variant** rather than deleted — deleting them was measured to mint four
  malformed keys (`mimo-flash/free`, `mimo-omni-free{omni}`, `mimo-pro/free`,
  `mimo-v2/pro`). The three `parse/data/huggingface_nomina.json` mimo rows are re-keyed;
  the now-redundant `xiaomi/mimo-v2.5-pro-ultraspeed` row is dropped from
  `parse/data/modelsdev_aliases.json`, since `ultraspeed` no longer declines.

  **The "V" survives where it belongs — in the name.** It leaves the key, not the model's
  identity: `bestiary show mimo-v2.5-pro` still resolves, and `XiaomiMiMo/MiMo-V2.5-Pro`
  is still a nomen of `mimo@2.5{pro}` on both the huggingface and provider-id legs. That
  needed no mechanism, because nomina are minted from the provider ids and the ids spell
  the V. Measured over the mimo family, **only the canonical leg moved**: canonical
  10 → 9, provider-id 40 → 40, huggingface 3 → 3, alias 0 → 0. No `NomenSchemeAlias` claim
  and no `nomen_claims.json` row was authored.

  **Surviving keyspace (9 keys, 93 instances — the instance total is conserved exactly):**
  `mimo@2.5`, `mimo@2.5{pro}`, `mimo@2.5{tts}`, `mimo@2.5{tts,voiceclone}`,
  `mimo@2.5{tts,voicedesign}`, `mimo@2{flash}`, `mimo@2{omni}`, `mimo@2{pro}`,
  `mimo@2{tts}`.

- **The merged `mimo@2.5{pro}` keeps BOTH of its models.dev rows.** Two metadata rows
  decompose to that one key — the canonical `xiaomi/mimo-v2.5-pro` and the
  `xiaomi/mimo-v2.5-pro-ultraspeed` speed tier the merge folds in — and the multi-metadata
  slot carries both, with the canonical row as the derived primary. Its description, its
  link and its three benchmark claims (SWE-Bench Verified 78.9, SWE-Bench Pro 57.2, GPQA
  Diamond 86.6) survive the merge intact, asserted by value in a committed test.

### Removed

- **Ten mimo entity keys are retired by the normalization above, and each is a hard 404.**
  Nine are pure renames and one (`mimo/pro`) is a genuine merge. No alias is minted, no
  redirect is added and no successor is listed at the tool: this table is the migration
  record, and the pointer a user gets.

  | retired mimo key | instances re-home to |
  |---|---|
  | `mimo` | `mimo@2{tts}` |
  | `mimo/flash` | `mimo@2{flash}` |
  | `mimo/pro` | `mimo@2.5{pro}` |
  | `mimo/v2.5-tts` | `mimo@2.5{tts}` |
  | `mimo/v2.5-tts-voiceclone` | `mimo@2.5{tts,voiceclone}` |
  | `mimo/v2.5-tts-voicedesign` | `mimo@2.5{tts,voicedesign}` |
  | `mimo/v@2.5` | `mimo@2.5` |
  | `mimo/v@2.5{pro}` | `mimo@2.5{pro}` |
  | `mimo/v@2{omni}` | `mimo@2{omni}` |
  | `mimo/v@2{pro}` | `mimo@2{pro}` |

  Every row is re-derived from the instances the retired key actually held, checked against
  the live registry on each run, and cross-checked against this table
  (`cmd/bestiary/testdata/retired/mimo_normalization_retired_keys_corpus.json`), so the
  three copies of the record cannot drift apart.

  **Bare `mimo` is the one row whose two seams differ, and that is correct.**
  `bestiary show mimo --by-entity` returns not-found like every other retired key, but
  plain `bestiary show mimo` still reports the lookup as under-specified — not because the
  key split, but because the FAMILY survives the retirement of its bare key and still has
  nine live children, exactly as `show gpt` and `show claude` behave. It must not be
  "fixed" into a 404.

  **Library consumers get a compile break, which is louder than a 404.**
  `entities_constants_gen.go` loses **10** `Entity__` declarations and gains **9**, counted
  from the file:

  removed — `Entity__Mimo`, `Entity__Mimo__Flash`, `Entity__Mimo__Pro`,
  `Entity__Mimo__V2_5_tts`, `Entity__Mimo__V2_5_tts_voiceclone`,
  `Entity__Mimo__V2_5_tts_voicedesign`, `Entity__Mimo__V__Version_2_5`,
  `Entity__Mimo__V__Version_2_5__Pro`, `Entity__Mimo__V__Version_2__Omni`,
  `Entity__Mimo__V__Version_2__Pro`;

  added — `Entity__Mimo__Version_2_5`, `Entity__Mimo__Version_2_5__Pro`,
  `Entity__Mimo__Version_2_5__Tts`, `Entity__Mimo__Version_2_5__Tts__Voiceclone`,
  `Entity__Mimo__Version_2_5__Tts__Voicedesign`, `Entity__Mimo__Version_2__Flash`,
  `Entity__Mimo__Version_2__Omni`, `Entity__Mimo__Version_2__Pro`,
  `Entity__Mimo__Version_2__Tts`.

  Census effect of this lever alone, measured on its own run: entities 947 → 946, series
  lines 417 → 416 (bare 208 → 207, versioned unchanged), releases 661 → 655, canonical
  nomina 947 → 946 (total 3,961 → 3,960).

- **Two cogito entity keys are retired, and each is a hard 404.** Deep Cogito publishes one
  artifact — Cogito v2.1 671B — and the catalog was carrying it under two keys, neither of
  them right. The dotted spelling `deepcogito/cogito-v2.1-671b` decomposed with the whole id
  remainder `v2.1-671b` in the variant slot and an empty version, so the 671b parameter size
  was stated **twice**: once as an identity token inside the variant and once again as the
  mechanically recovered `#size` segment. The dash-glued togetherai spelling
  `deepcogito/cogito-v2-1-671b` lost its dot, so only the orphaned trailing integer was read
  as the version, minting a phantom "Cogito v1" line for a release that does not exist.

  Both are repaired to the same shape — an EMPTY variant and the dotted point release as the
  version — so all three serving instances (kilo, openrouter, togetherai) now key
  `cogito@2.1#671b`, the only live cogito key. **The `v` is a version PREFIX, not a variant:**
  it introduces the number it is glued to and names no sibling line this release is
  distinguished from, which is the reading the mimo normalization already applies to its
  series letter. The v-carrying spellings survive as provider-id nomina
  (`deepcogito/cogito-v2.1-671b`, `deepcogito/cogito-v2-1-671b`), so
  `bestiary show deepcogito/cogito-v2.1-671b --format=raw` still finds the entity. It lands in **two** steps, in that order: the
  decomposition first, then the merge. Merging into the malformed key first would have moved
  the togetherai instance onto a key that then had to be renamed underneath it.

  No alias is minted, no redirect is added and no successor is listed at the tool: this table
  is the migration record, and the pointer a user gets.

  | retired cogito key | instances re-home to |
  |---|---|
  | `cogito/v2.1-671b#671b` | `cogito@2.1#671b` |
  | `cogito@1#671b` | `cogito@2.1#671b` |

  **Correction, recorded rather than silently rewritten.** An earlier draft of this stanza
  read the glued `v` as the VARIANT and published `cogito/v@2.1#671b` as the successor. That
  spelling **never shipped** — it existed only in-tree between two commits of this same
  unreleased stanza — and it contradicted the mimo ruling this release already applied. The
  successor column above is the corrected one; there is nothing for a released consumer to
  migrate from, because no release ever carried `cogito/v@2.1#671b`. The release's cumulative
  retired set is therefore **unchanged at 62** and its added set unchanged at 35: measured
  against the release baseline, `cogito/v@2.1#671b` is absent from BOTH sides of the diff.

  Every row is re-derived from the instances the retired key actually held, checked against
  the live registry on each run, and cross-checked against this table
  (`cmd/bestiary/testdata/retired/cogito_decomposition_retired_keys_corpus.json`), so the
  three copies of the record cannot drift apart.

  **Both keys 404 on both seams**, measured: `bestiary show <key> --by-entity` and plain
  `bestiary show <key>` each return not-found for `cogito/v2.1-671b#671b` and for
  `cogito@1#671b`. Neither is a bare family token, so neither reaches the under-specified
  branch that bare `mimo` and bare `ling` reach.

  **Library consumers get a compile break, which is louder than a 404.**
  `entities_constants_gen.go` loses **2** `Entity__` declarations and gains **1**, counted
  from the file:

  removed — `Entity__Cogito__V2_1_671b__Size_671b`, `Entity__Cogito__Version_1__Size_671b`;

  added — `Entity__Cogito__Version_2_1__Size_671b`.

  Census effect, measured per commit on its own run (unit: entity keys; axis: the constant
  set in `entities_constants_gen.go`; configuration: each lever alone, chained on the
  in-tree baseline). The decomposition is a pure **rename**: entities 946 → 946, series lines
  416 → 416 (versioned 209 → 210, bare 207 → 206 — the artifact leaves the bare cogito line
  it was the sole occupant of and mints a cogito gen-2.1 line), releases unchanged at 655,
  canonical nomina unchanged at 946. The variant pin is a genuine **merge**: entities
  946 → 945, series lines 416 → 415 (versioned 210 → 209 as the phantom gen-1 line empties,
  bare unchanged at 206), releases 655 → 654, canonical nomina 946 → 945 (total 3,960 →
  3,959), sized catalog entities 318 → 317. Across both, the provider-ID nomen count is
  unchanged at 2,834 — every re-keyed instance carries its own id spelling across as an
  admitted nomen.


### Changed

- **The MiniMax `turbo` demotion is re-verified against the lab's own sources, and its
  evidence grade is upgraded.** `turbo` is IDENTITY by global default and is demoted to an
  ATTRIBUTE for only a few curated families. For MiniMax that demotion had rested on
  inference alone — it was recorded as the lower-confidence row, the first one to revisit —
  and it rides on **exactly one** catalog instance, nanogpt's `minimax/minimax-m2.7-turbo`.
  It was re-checked on **2026-08-25** against
  [the MiniMax M2.7 weights repo](https://huggingface.co/MiniMaxAI/MiniMax-M2.7) and
  [MiniMax's own text-generation docs](https://platform.minimax.io/docs/guides/text-generation).

  **Outcome: the demotion stands, now on repo-identity evidence.** MiniMaxAI publishes a
  **single** M2.7 weights repo — there is no `MiniMax-M2.7-Turbo` and no
  `MiniMax-M2.7-highspeed` repo — so every fast-serving spelling of the version resolves
  back to one artifact. The lab documents its speed tier in its own words as *"M2.7
  Highspeed: Same performance, faster and more agile (output speed approximately 100 tps)"*
  against roughly 60 tps for the standard tier: an inference-layer tier of the same model,
  priced at 2x. The catalog census agrees, and it agrees *inside the
  entity*: `minimax/m@2.7` carries **54 instances**, **15** of which are M2.7 `-highspeed`
  spellings (**25** `-highspeed` ids across the family's M2.5 and M2.7 lines together), and
  the single `turbo` row sits right beside them — matching the seven canonical M2.7
  `-highspeed` rows (fastrouter, llmgateway, merge-gateway, minimax, minimax-cn, novita-ai,
  orcarouter) **exactly** on every axis: **0.6 / 2.4** per MTok, **204,800** context,
  **131,072** max output, against the plain M2.7's **0.3 / 1.2** — the same doubling the
  lab charges for its own speed tier. MiniMax's own first-party endpoints name that tier
  `highspeed`, never `turbo`, and **no** provider ships both spellings for one version — the
  aggregators that carry the tier all copy the lab's word. So `turbo` cannot be a
  distinct artifact standing beside `highspeed`; it is nanogpt's own label for it. `highspeed`
  is already attribute-class globally, so this is the consistent reading.

  **Nothing you depend on moves.** This is a documentation and evidence change only:
  `minimax/m@2.7` keeps its instances, no `minimax/m@2.7{turbo}` key is minted, and
  regeneration was measured **byte-identical** — not one generated file changed. No key is
  retired, so there is no migration to make.

## [0.2.9] - 2026-07-28

**Schema:** unchanged at `0.6.0`.

### Added

- Added `HarnessStrike` (`"strike"`) to the canonical known-harness set.

## [0.2.8] — 2026-07-24

**Schema:** `0.5.0` → `0.6.0` (additive). SQLite store schema `7` → `8`.

The **registry-ingest** epoch: names acquire real provenance, and a third source starts
attesting them. A `Nomen` no longer fuses its evidence into scalar columns — it carries a
set of `NomenAttestation`s, each with its own `Authority` (primary / secondary) and
`Method` (curated / harvested / self-minted), persisted in a `nomen_attestations` child
table. A new polite HuggingFace Hub bot (`cmd/bestiary-hf`) harvested 179 Hub repo names
into that layer, making the Hub the third `DataSource` alongside models.dev and Ollama, so
a name that curation already claimed and the Hub independently attests coalesces into one
nomen with two legs rather than two rows. Alongside it: a `Creator` dimension separating
who *trained* the weights from who *serves* them, a `pkg:oci` external-identifier scheme,
an offline `cmd/bestiary-web` catalog front-end, and a curation pass that drained the
models.dev unlinked report to zero.

### Added

- **Nomen multi-attestation** — `Nomen` **HAS-MANY** `NomenAttestation`
  `{SourceURL, Source, Authority, Method, IngestedAt}` (≥1 per nomen). Provenance moves off
  the nomen row, so the same name attested by two ingests is **one** nomen with two
  attestation legs, not two rows. Two new element enums, `AttestationAuthority`
  (unknown / primary / secondary) and `IngestMethod` (unknown / curated / harvested /
  self-minted), follow the `NomenScheme` codec precedent (`String` / `MarshalText` /
  `UnmarshalText`; an unknown token is an actionable error). `coalesceNomina` groups by the
  `(Value, Scheme, ResolvesTo)` triple and **unions** same-triple attestation sets, sorting
  by the total key `(Source, SourceURL, Authority, Method, IngestedAt)` so equal keys imply
  byte-identical, deterministic emission. `ValidateNomina` is **inverted**: a same-triple
  differing attester is now a legal append and only a differing `Status` is a loud conflict
  — and it is wired into `cmd/bestiary-gen`, so the bake aborts on conflict rather than the
  check being test-only. Runtime degrades to the raw sorted set on conflict, never panics.

- **`Creator` dimension** — the lab that **trained** a model's weights (SPDX *originator*),
  distinct from `Provider`, which serves it (SPDX *supplier*). `Creator` is an open string
  type mirroring `Provider` (`CreatorNone` + 9 well-known constants, `String` /
  `MarshalText` / `UnmarshalText` / `IsKnown` / `Creators`), and the Family→Creator mapping
  is a **curated, data-driven seed** (`parse/data/creators.json`) read through the
  graceful-degrade embed loader, never an in-code switch. `ModelInfo.Creator` and
  `Entity.Creator` are **derived join projections**, not stored columns — Family→Creator is
  a function, so a per-row column would be a transitive dependency. `ValidateCreatorTable()`
  is a loud codegen guard.

- **`pkg:oci` external-identifier scheme** — `SchemeOCI` appended to the `CanonicalScheme`
  iota tail (wire-stable) with every recognition arm plus `InputFormatOCI`.
  `formatOCIPurl` implements the purl-spec `oci` type: lowercased last path fragment as
  name, `sha256:<digest>` version with `:` percent-encoded, `repository_url` / `tag`
  qualifiers in canonical alphabetical order — and `""` when the digest is absent, because
  an OCI purl is **never** minted without one. `ModelRef.Format(SchemeOCI)` returns `""` via
  an **explicit** arm rather than falling through to a default that would leak the raw ID.
  `cmd/bestiary-ollama` now persists the manifest config digest it previously fetched and
  discarded, onto `QuantVRAM.OCIDigest`, emitted conditionally by codegen so the
  empty-digest majority stays byte-identical. `NomenSchemeOCI` mints one OCI nomen per
  digest-bearing quant row with an Ollama Harvested/Secondary attestation.

- **`cmd/bestiary-hf` — polite HuggingFace Hub bot + harvested nomen layer**: a
  network-gated tool on `internal/politebot` with its own versioned User-Agent, layering
  HTTP conditionals beneath the shared cadence seam — `If-None-Match`/`ETag` (a 304 keeps
  the existing row) and 429 backoff honoring `Retry-After`. A Hub id is `org/repo`, taken
  **1:1 with case preserved** as the nomen value, with **no decomposition**. The entity join
  is alias-first (`hf_aliases.json` override → mechanical decomposition through the
  production parse pipeline → keep-unlinked and report), so a repo is never silently
  dropped. `DataSourceHuggingFace` is the third source: an entity carrying an HF nomen
  dual-attests `{models.dev, huggingface}`, and `llama@3.3#70b{instruct}` now
  **triple**-attests `{huggingface, models.dev, ollama}`.

- **`cmd/bestiary-web` — offline web catalog (foundation)**: a read-only HTTP server over
  the in-process static registry (`Entities()`/`StaticModels()`) plus an optional read-only
  SQLite cache; it makes NO network request at serve time. Entity pages are addressed by the
  RQ1 multi-segment IRI grammar (`/entity/<key>`, `EntityRef.IRI(webRoot)` == route path) with
  a content-negotiation seam (`Accept: text/html` → page, `application/json` → the entity's
  public JSON shape) and query params treated as non-identity view-state (stripped for
  identity). The base `html/template` layout ships the approved "Phosphor Terminal" CSS in both
  color modes (`prefers-color-scheme` + an explicit `data-theme` toggle). The Datastar client
  is VENDORED via `go:embed` and served same-origin (v1.0.2, no CDN for JS); the two webfonts
  load from a CDN with a full offline fallback stack (fonts-only relaxation, RQ1). Server-side
  interactivity uses the `github.com/starfederation/datastar-go` SDK (SSE `PatchElements`).

- **`cmd/bestiary-web` views — entity browser, entity detail, series explorer**: the browser
  (`/`) is a dense sortable table over every entity with a server-side filter rail —
  family / creator / provider / region / modality exact-match facets plus free-text key
  search and a sort key, all driven through the `/sse/entities` Datastar seam. Modality is
  joined from `StaticModels()` at startup (an `(ID,Provider)→Modalities` map computed once,
  never affecting identity). The default order is family-then-key, fixed and tested so every
  SSE re-render is byte-stable; view-state signals select **which rows and in what order,
  never which entity a link denotes**. Entity detail (`/entity/<key>`) renders four sections
  — quants + VRAM (a `VRAMEstimatePartial` figure is labelled and its bar drawn hollow so it
  never implies precision the data lacks), pricing by provider over the `(ID,Provider)`
  instances, nomina + every attestation's source / authority / method / source-URL, and
  lineage + series membership — with an optional `?ctx` **display-only** VRAM recompute
  column via `(QuantVRAM).EstimateVRAM`. The series explorer (`/series`) is a native
  `<details>` disclosure tree over `SeriesAll`/`ReleasesOf`/`EntitiesOf` with stable anchors.

- **CLI naming/creator surface** — `show` and `list` gain a `Creator` column
  (`CreatorNone` renders `-`, never an invented "unknown") and `show --by-entity` gains a
  `Creator` line plus a **Nomina section**: each nomen's `(scheme, status)` with its
  attestation set indented beneath (Source / Authority / Method / SourceURL), so a
  dually-attested name shows **both** legs. `--scheme oci` / `--format oci` on a ref
  short-circuits with an actionable stderr notice directing to the quant-level view (OCI
  identity is per-quant-digest, so a ref has no render at that altitude) — an *explained
  empty* with exit 0, not an error.

- **Store schema `8`** — `nomen_attestations` child table + a `creators(family PK, creator)`
  BCNF dimension. `migrateToV8` runs in one transaction: create both tables, backfill each
  old `nomina` row's `(source_url, source_id)` into one attestation with authority/method
  derived per the defaults table, then recreate the `nomina` parent without the source
  columns (PK unchanged, so keys stay byte-identical). This removed all seven of the
  transitional v7 single-attestation bridge sites; the child table carries the **full**
  per-attestation set losslessly, including the curated-authored `Authority` the bridge
  could not round-trip. `UpsertNomina` is parent `OR IGNORE` + delete-then-insert children
  in one transaction (the `entity_metadata` replaceable-set precedent), with the child
  `source_id` FK rejecting orphans on full rollback.

### Changed

- **IRI output: `/` is now LITERAL** (`EntityRef.IRI` / `ModelRef.IRI`, BREAKING for the
  minted string). `escapeIRISegment` keeps the key grammar's `/` (family/variant) as a real
  path separator and percent-encodes only the remaining structural delimiters (`@`→`%40`,
  `#`→`%23`, `{`→`%7B`, `}`→`%7D`, and the ref-level `[`→`%5B`, `]`→`%5D`). An entity IRI is
  therefore a multi-segment, walkable path — `…/entity/llama/scout%404%2317b-16e%7Binstruct%7D`
  rather than the v0.2.7 single-segment `…/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D`.
  This is the RQ1-ratified grammar the new `cmd/bestiary-web` `/entity/<key>` routes
  dereference, so `EntityRef.IRI(webRoot)` equals the route path for the same entity (one
  grammar, pinned by a route-equality test). The round trip is unchanged: a whole-string
  `url.PathUnescape` still recovers the canonical key byte-identically (a literal `/` passes
  through; every `%40`/`%23`/`%7B`/`%7D`/`%5B`/`%5D` decodes back), so every IRI round-trip
  fence stays green — only the two golden-string assertions were re-pinned for the literal `/`.

- **`internal/politebot`** — the polite-HTTP seam is extracted out of
  `cmd/bestiary-ollama` into a compiler-private package: one `get()` request seam (≥1s
  inter-request cadence, descriptive versioned User-Agent, optional `Accept`,
  `io.LimitReader` body cap, non-2xx reject) with the injectable doer/clock/sleep
  offline-test hinge preserved via functional options. Both bots now share it; the per-bot
  User-Agent and call-site `Accept` stay bot-owned. Behavior-preserving — the root
  `bestiary` package is untouched and `internal/` is unimportable outside the module, so
  there is **zero public-API impact**.

- **`ModelInfo.Source` defaults to `DataSourceModelsDev`** — the semantics shift from "a
  further source beyond models.dev" to "the originating/attesting ingest source". Filled in
  at the **load** layer (the registry normalizes in-memory static models once) and on the
  store read path, so generated files stay byte-identical and `go generate` stays zero-diff.
  Non-empty carriers (Ollama) are untouched and the `EntitySource` join is unchanged.

- **Human-readable defaults on the entity views** — `show --by-entity`, the entity-key
  fallback, and `series` default to `--output table` when `--output` is not set explicitly;
  `--output=json` is still available by asking for it.

### Fixed

- **Ambiguity guidance is actionable and paste-back-resolvable** — the opaque "matched
  multiple canonicals" jargon is replaced with plain language plus concrete next steps
  derived from the actual candidates. The derived example is the first candidate's **entity
  key** shown via `show --by-entity`, which renders an entity key directly without the
  model-first resolution that produced the ambiguity, so it resolves for every family class
  (including high-fanout keys like `gpt/4o` that would re-ambiguate under a plain
  `show <key>`). `FormatAmbiguous` now lists candidate **entity forms**, not just provider
  slugs, for families without a canonical provider; the duplicated `bestiary:` preamble is
  dropped so only one wrapped error carries the prefix; and the `--format=raw` tip appears
  in exactly one place instead of twice in slightly different wording.

- **`show <input>` accepts an entity key without `--by-entity`** — model resolution stays
  first, and only on a model **miss** does it fall back to the entity view over the
  store-overlaid set. Ambiguous model input keeps the guidance path (no entity fallback on
  ambiguity).

- **Instance table alignment** — over-wide `ID` / `PROVIDER` / `HOST` cells truncate to
  their column widths so long provider slugs no longer break alignment, and rendered rows
  are capped with a `… and N more (use --output json)` footer, mirroring the
  nomina/benchmark convention.

- **HF bot hygiene** — dead RFC-5988 `Link` pagination is removed (the bot verifies known
  `org/repo` candidates with targeted GETs, never a listing endpoint, so pagination is
  inapplicable by design, now documented at the fetch loop); `hf_aliases.json` fails loud
  when two keys case-fold to the same value, since the case-insensitive lookup fallback
  would otherwise resolve by map iteration order; and `huggingface_unlinked.json` gains a
  `count` field per the unlinked-envelope precedent.

### Data

- **`modelsdev_unlinked` drained to 0** — all 11 remaining ids were served-entity join-key
  **disagreements** (the id-only ref mis-derives the family), not catalog-absent models, so
  none synthesizes a standalone and the census stays at the pinned 4 `ornith` rows. Ten are
  resolved by curated `modelsdev_aliases.json` rows mapping each metadata id onto its
  distinct served entity key; `command-a-translate-08-2025` needed `translate` added to
  `modifiers.json` as a peeled **identity** modifier so Command A Translate keys as
  `command/a{translate}` instead of overwriting base `command/a`'s metadata (a collateral
  scan found zero other tail-position `translate` tokens).

- **Evidence-gated parse-failure repairs** — of 286 audit signals, 285 are benign residual
  tokens (size/quant/serving leftovers with the version correct) and are **classified, not
  "fixed"**. The one genuine wrong-entity class is repaired via exact-id family overrides:
  `deepseek-v3-1` / `deepseek-ai/DeepSeek-V3-1` → `deepseek/v3.1` (14 dotted-sibling
  instances) and `deepseek-v3-2-exp` → `deepseek/v3.2-exp`. DeepSeek encodes point releases
  as a **variant** token, so a version-only fix would not merge — variant-pinned instead,
  retiring the phantom `deepseek@1` / `deepseek@2` entities.

- **Harvested HuggingFace seed** — one polite live `cmd/bestiary-hf` run over the
  models.dev-known open-weight `org/repo` candidates: 500 candidates fetched plus 4 forced
  aliases; **179 verified repos joined a catalog entity** and were seeded, 17 verified but
  unlinked (reported), 251 skipped (gated/private/4xx), 0 rate-limited, 0 unchanged. The 4
  pre-existing curated Hub repos are pinned by `hf_aliases.json` to their exact curated
  triples so each harvested attestation **coalesces** onto its curated claim — one nomen,
  two attestations with distinct Method and Source. The curated-layer archive fence is
  scoped to `Method=Curated`; harvested attestations carry live URLs by design.

- **Census re-pins (arithmetic, conscious)** — entities 958 → 957 (`command/a{translate}`
  +1; the two DeepSeek phantom merges −2), series 419 → 417, releases 671 → 669. Nomina
  3,797 → 3,796 from the entity merge, then → **3,971** with the HF harvest (the 4 curated
  repos coalesce with their harvested twins for +0; 175 distinct-triple harvested repos for
  +175). `huggingface`-scheme nomina go 4 → 179.

### Documentation

- **`docs/w3id-runbook.md`** — the user-executed w3id.org registration procedure
  (GitHub PR + Apache `.htaccess`), the content-negotiated HTML/JSON-LD redirect-target
  design, the default IRI base decision at the `EntityRef.IRI(base)` call site, and the RQ1
  URL-scheme ruling (literal `/` inside the canonical key, `@#{}` percent-encoded, query
  params as view-state only). **No registration performed.**

- **`docs/CONCEPTS.md` — attestation quality** — a new section grounding
  `AttestationAuthority` / `IngestMethod` in CIDOC CRM E13 *Attribute Assignment*, CRMinf
  I7 *Belief Adoption*, and Wikidata statement ranks. It also **corrects** the Grounding
  table's prior claim that `AcceptabilityRating` maps to an LRM-E9 "status" attribute — the
  verified LRM spec's Nomen attribute list has no such attribute, and "preferred" is
  expressed via the general *Category* attribute instead.

- **`docs/poetools-claude-code.md`** — live research into the models.dev catalog row
  `poetools/claude-code`, which decomposes as an ordinary Claude entity but is in fact Poe's
  own agent product built on the Claude Agent SDK. Used as the concrete case for the
  `harness.go` harness-vs-model classification question; three candidate relationships are
  recorded and **none chosen**.

- **Registry-ingest research report** and the `cmd/bestiary-web` visual-direction design
  note (retrofuturism, with the ratified gate rulings applied).

## [0.2.7] — 2026-07-23

**Schema:** `0.4.0` → `0.5.0` (additive). SQLite store schema `6` → `7`.

### Added
- **Nomen naming layer**: one queryable record for every way anything names a model
  entity — `Nomen{Value, Scheme, Status, ResolvesTo, SourceURL, Source}` with the
  `NomenScheme` classifier (canonical / provider-id / huggingface / purl / alias)
  and ISO 1087 `AcceptabilityRating` statuses. Minted by one shared production
  function over the entity index plus the curated `parse/data/nomen_claims.json`
  (3,797 nomina at release: 958 canonical Preferred, 2,834 provider-ID Admitted,
  4 huggingface, 1 curated
  alias claim — after the closing Impl-UAT batch below folded the entity census
  977 → 947). `Entity.Nomina()` and `NomenLookup()` (homonym-aware) are the
  read APIs; claim attribution keeps *who asserts* (`SourceURL`) distinct from
  *which ingest we read* (`Source` — curated claims attribute the new `curated`
  data source, never models.dev).
- **Region axis**: `Region` closed int enum (`region.go`) capturing the
  geographic/jurisdictional boundary an instance's serving is scoped to (Amazon
  Bedrock cross-region inference profiles: `us.`/`eu.`/`au.`/`jp.`/`global.`).
  Members follow ISO 3166-1 alpha-2 where applicable; `RegionNone` renders
  `"unspecified"`; unknown tokens land in `RegionOther` with the raw token
  preserved. Public surface: `ModelInfo.Region`/`RegionRaw`, per-instance
  `ProviderInstance.Region`, and the `Entity.Regions` jurisdiction aggregate
  (e.g. Bedrock-served entities report `[unspecified, us, eu, global, au, jp]`),
  rendered in table/YAML/JSON. Not part of entity identity.
- **Series/Release hierarchy** (`taxonomy.go`): a computed, read-only grouping above
  entity keys. `Series{Family, Generation}` is the versioned line (`llama-4`,
  `gemini-3.0`) and `Release{Series, Name}` is a named member of it (`scout`,
  `maverick`, `flash`) — version above variant. Both are COMPUTED from key components
  already on `EntityRef`, never stored and never fed back into keys: the hierarchy can
  be re-shaped without re-keying anything. Read APIs: `SeriesAll()` (419 lines, sorted
  by family then generation), `ReleasesOf(Series)`, `EntitiesOf(Release)`, plus
  `SeriesOf(EntityRef)`/`ReleaseOf(EntityRef)` for the entity → line direction; all
  orders are explicit sorts and all results are defensive copies. Two refinements keep
  one line from splitting on a spelling accident: a bare-integer generation `N` folds
  into `N.0` when the same family also spells `N.0` (so `gemini@3` and `gemini@3.0`
  share `gemini-3.0`, while `llama-4` — which has no dotted sibling — keeps its bare
  generation), and the curated `parse/data/series.json` re-homes the few families whose
  line the computation cannot derive (`gemma4` and `gemma-4-31b-larkspur` onto
  `gemma-4`, `gemini-exp` onto `gemini`). The curated file is graceful-degrade: missing
  or malformed, the taxonomy falls back to pure computation. **Entity keys are
  untouched by both.**
- **`bestiary series` subcommand** (read-only, offline, static registry — it never reads
  the SQLite cache and *rejects* `--db-path` with an actionable error rather than
  accepting a flag it cannot honour): bare, it lists every line with its release and entity counts; with a
  selector it details that line's releases and their entity keys. The selector is a
  **specificity ladder**, case-folded, each rung narrower than the one above: a family
  name (`claude`) returns every generation of it; a **major version** (`claude-4`, or
  equivalently `claude --version 4`) returns every `claude-4.x` line as a UNION; and a
  full line rendering (`claude-4.8`) returns that one line. The major rung SELECTS, it
  does not re-group — it returns several Series in the same multi-line shape the family
  rung produces, and `claude-4.0`/`claude-4.5` remain distinct lines a narrower selector
  still addresses individually. Membership is a strict string rule (a generation belongs
  to version `4` iff it IS `4` or begins `4.`) with no numeric normalization, so `4`
  never swallows `42`, `1` never reaches `ling#1t` (whose `1t` is a param-size, not a
  version — see the closing batch below) or the leading-zero
  `gemini@001`. GLM's `5p1`/`5p2` spellings are decoded at PARSE into real `5.1`/`5.2`
  versions (see Fixed), so they join the `glm-5` union as ordinary dotted members —
  there is no `p`-awareness in the taxonomy. Sub-1.0
  generations need no special case (`mistral-0` unions `0.1` and `0.3`), and where a
  family spells both a bare `N` and dotted siblings the union includes the bare line.
  The new `--version` flag is exactly equivalent to appending `-<value>` to the
  positional, composes with `--provider`/`--quant`/`--status` (selection first, entity
  filters after), and is rejected with an actionable error when given without a family
  or without a value.

  The **canonical entity grammar** is accepted as a selector too, mapped to its
  series-level meaning — `claude@4` (the major union), `claude@4.5` (one line),
  `claude/opus` (a RELEASE-LEVEL cut: the opus release across every claude generation),
  `claude/opus@4`, and `anthropic/claude@4` (provider-qualified). The `@` is the entity
  VERSION, as in an entity key, not the `show` resolver's `@`-date form; series live
  above entity keys and inherit the key grammar. Parsing reuses the same
  `parseEntityTuple` the entity commands use — there is no second parser. A release cut
  SELECTS the lines carrying that release (a line without one drops out rather than
  rendering its other releases), and a provider prefix feeds the ordinary `--provider`
  machinery; a `--provider` or `--version` that contradicts the selector is an
  actionable error rather than a silent precedence win.

  The new `--input-format` flag pins the selector grammar for scripting: `canonical`
  (the entity grammar, NO fallback — a raw id fails loudly and is told which format
  would read it), `models.dev` (a raw catalog id resolved through the ordinary lookup
  to its entity's line), or the default `infer` (ladder and canonical readings unioned,
  with the raw-id reading as the FINAL fallback — the precedence is documented rather
  than emergent). Table and JSON output; no schema change, since this
  renders Go relations rather than a new public JSON document type.
- **Store v7**: `nomina` table (PK `(value, scheme, entity_key)`, FK →
  `data_sources` — enforcement regression-tested) + `region` column, with
  presence-guarded self-heal from v6 on both paths and zero data loss.
- **IRI minting** (`iri.go`): `EntityRef.IRI(base)` and `ModelRef.IRI(base)` render an
  identity as a dereferenceable RFC 3987 name — the caller-supplied namespace base with
  the percent-encoded canonical string appended as exactly ONE path segment. The base is
  a **parameter by design and is never defaulted**: bestiary owns no public namespace, so
  the consumer supplies its own (`https://w3id.org/…`, an internal https namespace, a
  `urn:` prefix); it is used verbatim and the only rejected value is the empty string
  (which would yield a relative reference, not an IRI — that returns `""`). Escaping is
  `url.PathEscape` (the path-segment escaper — `url.QueryEscape` would render a space as
  `+`, which does not decode back in a path position) plus an explicit `@` → `%40` pass,
  because `@` is a legal `pchar` that `PathEscape` leaves raw while the grammar uses it as
  the version delimiter. Every grammar delimiter is therefore encoded — `#` most
  importantly, since raw it would start a URI *fragment* and silently truncate the
  identifier at the size segment. Additive Go API only: no schema, store or output change.
  `llama/scout@4#17b-16e{instruct}` →
  `https://w3id.org/bestiary/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D`, and the
  round trip back through `url.PathUnescape` is fenced over a delimiter torture set and
  over every entity and model ref in the committed registry.
- **Curated HuggingFace nomina** (`parse/data/nomen_claims.json`): four
  `NomenSchemeHuggingFace` seed claims mapping flagship open-weight entities to the Hub
  org/repo their weights live at — `meta-llama/Llama-4-Scout-17B-16E-Instruct` →
  `llama/scout@4#17b-16e{instruct}` (an entity served by 13 providers), plus
  `meta-llama/Llama-3.3-70B-Instruct`, `Qwen/Qwen3-Coder-480B-A35B-Instruct` and
  `deepseek-ai/DeepSeek-V3.2`. Each repo path is verified verbatim against the committed
  catalog's raw model IDs (fenced), each is `Admitted` with `Source = curated` and a
  claimant `SourceURL` pointing at the Hub page, and all surface through
  `Entity.Nomina()` / `NomenLookup()`. This is the durable, **entity-level** external
  identifier: the Hub name holds regardless of which provider is serving the entity.
  Minted census at release: 3,797 (958 canonical, 2,834 provider-ID, 4 huggingface, 1 alias).

### Changed
- **Designation layer activated**: `ModelRef.Designations()` now rates the
  canonical form `Preferred` (raw/HuggingFace/PURL stay `Admitted`) — the
  prerequisite for truthful `skos:prefLabel`/`altLabel` export (GH#24 ask 3).
- **Constants surface is now entity-level** (BREAKING). The ~5,650
  provider-flavored `Model__<Provider>__…` constants are replaced by one
  provider-agnostic `Entity__*` constant per model entity (958 after the closing
  Impl-UAT batch below), each valued by
  its canonical entity key (e.g.
  `Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct = "llama/scout@4#17b-16e{instruct}"`).
  Names follow a word-sentinel grammar — `Entity__<Family>[__<Variant>][__Version_<v>][__Size_<s>][__<Mod>…]`,
  plain-Pascal segments with a separator-preserving sanitizer (every
  non-alphanumeric character → `_`, never camel-folded) — kept injective by a
  loud codegen guard that fails the bake on any duplicate identifier. Provider
  information moves to the API: `ProvidersOf(ref EntityRef) []Provider` and
  `ProvidersOfModel(id ModelID) []Provider` return an entity's serving providers
  (sorted, de-duplicated). `ModelIDs()` → `EntityKeys()`.

- **`llama-4 maverick` is a named release** (BREAKING for two entity keys). `maverick`
  joins `scout` as a curated `llama` variant member, so the 23 maverick instance rows
  key under `/maverick` as siblings of scout instead of collapsing into the
  variant-less `llama@4` line. This is the epoch's ONE deliberate entity re-key; every
  other key is byte-identical and that re-key leaves the entity census unchanged (a move,
  not a mint). Migration:

  | Old key (gone) | New key |
  |---|---|
  | `llama@4#17b-128e` | `llama/maverick@4#17b-128e` |
  | `llama@4#17b-128e{instruct}` | `llama/maverick@4#17b-128e{instruct}` |

  | Old constant (gone) | New constant |
  |---|---|
  | `Entity__Llama__Version_4__Size_17b_128e` | `Entity__Llama__Maverick__Version_4__Size_17b_128e` |
  | `Entity__Llama__Version_4__Size_17b_128e__Instruct` | `Entity__Llama__Maverick__Version_4__Size_17b_128e__Instruct` |

  The four spellings whose form puts the maverick token out of the mechanical member
  scan's reach — the dotted Bedrock forms (`meta.llama4-maverick-…`,
  `us.meta.llama4-maverick-…`) and the aggregator provider-prefixed forms
  (`cerebras-…`, `groq-…`) — carry an exact-ID variant pin so all 23 rows stay in one
  artifact rather than fragmenting across two keys.

### Removed
- **`Model__*` constants + `ModelIDs()`** (BREAKING). Migration:

  | Old (removed) | New |
  |---|---|
  | `Model__<Provider>__…` (an entity served by one provider) | `Entity__<…>` (the entity) + `ProvidersOf(ref)` to list its providers |
  | `bestiary.ModelIDs()` (all provider-flavored IDs) | `bestiary.EntityKeys()` (all canonical entity keys) + `EntityByKey(key)` — enumerate-then-lookup works again: `for _, key := range EntityKeys() { e, _ := EntityByKey(key); … }` |
  | constant → a specific provider's instance | `LookupModelByProvider(provider, id)` (or `LookupModel(id)`) for instance-level fields |
  | constant → the entity's identity | `Entity__<…>` constant, then `EntityByKey(value)` — or `EntityByTuple(family, variant, version, paramSize, mods…)` from a decomposed tuple |

  **Caveat — entity keys are NOT `ModelID`s.** `EntityKeys()`/`Entity__*` values are
  canonical ENTITY keys (grammar `family[/variant][@version][#size]{mods}`, where `@` is
  the identity VERSION), a different grammar from raw catalog `ModelID`s and from
  `Resolve`'s `<provider>/<family>/<variant>@<date>` form (where `@` is a DATE). Do NOT
  pass an entity key to `LookupModel` / `LookupModelByProvider` / `Resolve` — they take
  provider-ID grammar and will silently miss. Use `EntityByKey(key)` (or `EntityByTuple`)
  for entity-key lookups; use `LookupModel(id)` for raw-ID instance lookups.

### Fixed
- **`p`-as-dot version decode** (`parse.go`). Some providers publish a version with a
  `p` where the dot belongs, because their id namespace disallows the dot — their own
  display names spell the dot. Reading it verbatim minted **phantom entities**:
  `glm@5p1` and `glm@5p2` sat stranded beside the real 50- and 65-instance `glm@5.1`
  and `glm@5.2`, so "the GLM 5 line" was silently incomplete. The decode is the SAME
  convention `parseSeriesNumber` has always applied inside the letter-prefix series
  split (`kimi-k2p6` → `k@2.6`, which is why the kimi/minimax spellings already
  resolved); generalizing that one rule to the ordinary version-token position — rather
  than adding a second, provider-gated mechanism for the same phenomenon — is what
  reaches families with no series letter. The shape is narrow on purpose (digits, a
  literal `p`, digits, whole-token), so `4o`, `120b` and any literal `p` not flanked by
  digits are untouched. **Census: 979 → 977 entities** (the two phantoms merge into
  their real siblings; two keys retired, none renamed), 421 → 419 series, 674 → 672
  releases, 3,775 → 3,773 nomina. The `5p1`/`5p2` id spellings survive as Admitted
  provider-ID nomina on the merged entities — decoding a spelling for *identity* never
  erases it from the record — and `series glm-5` now returns 5.1 and 5.2 as ordinary
  dotted members of the union, with no `p`-awareness anywhere in the taxonomy.

  The rule reaches **three positions**, sharing one definition: the ordinary
  version-token path, the letter-prefix series split (where it already lived), and the
  **glued** generation token — `qwen3p7-plus` had its version dropped entirely, because
  `splitBareGen`'s trailing digit/dot scan stops at the `p` and leaves the
  digit-suffixed base `qwen3`, which the not-digit-suffixed clause then rejects. It now
  decomposes to `qwen/plus@3.7` and joins the **pre-existing** entity of that key, so
  no key is created or retired and the census is unmoved (the base-side clauses are
  untouched — the new arm admits a *shape*, never a new permission).

  The one *residual* `k2p7` (kimi-for-coding) defect noted during the p-decode work is
  now **fixed in the closing Impl-UAT batch below**: it landed in the compound family
  `kimi-k2` with an empty version because — unlike its `k2p5`/`k2p6` siblings, which
  arrive with upstream family `kimi-thinking` and resolve — it arrives with the compound
  `kimi-k2`, the *(compound raw family + bare series-token id)* combination the shared
  family-recovery path does not close. Rather than a catalog-wide recovery-path change
  (too broad a blast radius across every kimi/minimax/mimo row), it is pinned with a
  narrow curated exact-id override so it now resolves to **`kimi/k@2.7`** and joins the
  `kimi-2.7` union alongside its `k2p5`/`k2p6` siblings (distinct from the fireworks
  `kimi/k@2.7{code}` coding router). The general recovery-path fix stays deferred.
- **Closing Impl-UAT batch — five user-ratified identity corrections** (BREAKING for
  38 `Entity__*` constants). The final acceptance pass folded five spelling/identity
  defects that had each split one model across two entities or stranded it under a junk
  key. Entity census **977 → 947** (canonical nomina 977 → 947, total 3,773 → 3,743;
  provider-ID **unchanged at 2,791** throughout — every fix MOVES instances, never drops
  one, so each corrected spelling survives as an Admitted provider-ID nomen); Series
  419 → 411 (a flat −8 versioned lines, bare unchanged at 204); Releases 672 → 659;
  sized-catalog entities 336 → 323.

  1. **C4 entity-level `N` → `N.0` fold** (`registry.go`, `NormalizeEntityVersion`). A
     family that spells BOTH a bare integer version `N` and the dotted `N.0` for the
     SAME (variant, param-size, identity-modifiers) folds the bare entity onto the
     dotted one — one canonical spelling per generation. A pure MERGE (never a rename):
     `llama@4`, with no `4.0` sibling, is untouched. Eight pairs fold. A bare-version
     EXPRESSION still resolves: `EntityByKey("claude/opus@4")` and
     `EntityByTuple(claude, opus, "4", "")` return the `claude/opus@4.0` entity. The
     specificity ladder is untouched — selector `claude-4` remains the 4.x union.
  2. **o-series dual-identity unified** (`parse.go`, `canonicalizeOpenAILine`). The same
     OpenAI o-series model was two identities by spelling: `openai/o1` → `gpt/o@1`, but
     digitalocean's hyphen-glued `openai-o1` stranded in a junk family `o`. Both now
     converge on `gpt/o@1` / `gpt/o@3` / `gpt/o@3{mini}`; family `o` empties.
  3. **Dot-lost version spellings repaired** (`parse.go`, `dotLostVersionOverrides`;
     26 dash-glued `qwen2-5-…`/`qwen3-6-…` + 7 dotless `minimax-m25`/`qwen35-…`/
     `mistral-small-31-…` ids) — a version-only curated exact-id override that corrects
     ONLY the version, so family/variant/size/modifier/stage are untouched. Each merges
     into (or re-keys onto) the heavily-attested dotted sibling.
  4. **`k2p7` routed to `kimi/k@2.7`** — the residual defect from the `p`-decode work
     above, fixed with a narrow curated exact-id override (see that entry).
  5. **`tts-1-hd` split to `tts@1{hd}`** — OpenAI documents tts-1-hd as a distinct
     higher-quality product, so `hd` is now an IDENTITY modifier (a new entity, split
     from `tts@1`).
  6. **`1t` routed to the param-size axis** (`parse.go`, trillion unit `t`). Ling-1T /
     Ring-1T are 1-trillion-parameter models, so `1t` is a SIZE not a version:
     `ling@1t` → `ling#1t`, and the `1t` in `ling-2.6-1t` rides beside version 2.6
     (`ling@2.6#1t`). The ollama `:1t` tag on `kimi-k2:1t` (suppress-pinned) and the
     token-internal `r1t2` (`deepseek-…-r1t2-chimera`) are unaffected.

  **Constant migration (38 removed; 8 of the replacements are NEW, the rest are the
  pre-existing dotted constants).** The full list is reproducible via
  `git diff <base>..HEAD -- entities_constants_gen.go`.

  | Old constant (gone) | New constant |
  |---|---|
  | `Entity__Claude__Opus__Version_4` | `Entity__Claude__Opus__Version_4_0` |
  | `Entity__Claude__Sonnet__Version_4` | `Entity__Claude__Sonnet__Version_4_0` |
  | `Entity__Gemini__Flash__Version_3` | `Entity__Gemini__Flash__Version_3_0` |
  | `Entity__Gemini__Pro__Version_3` | `Entity__Gemini__Pro__Version_3_0` |
  | `Entity__Imagen__Version_4` | `Entity__Imagen__Version_4_0` |
  | `Entity__Imagen__Version_4__Fast` | `Entity__Imagen__Version_4_0__Fast` |
  | `Entity__Imagen__Ultra__Version_4` | `Entity__Imagen__Ultra__Version_4_0` |
  | `Entity__Veo__Version_3` | `Entity__Veo__Version_3_0` |
  | `Entity__O` | `Entity__Gpt__O__Version_1`, `Entity__Gpt__O__Version_3` |
  | `Entity__O__Mini` | `Entity__Gpt__O__Version_3__Mini` |
  | `Entity__Minimax__M__Version_25` | `Entity__Minimax__M__Version_2_5` |
  | `Entity__Minimax__M__Version_27` | `Entity__Minimax__M__Version_2_7` |
  | `Entity__Mistral__Small__Version_31__Size_24b__Instruct` | `Entity__Mistral__Small__Version_3_1__Size_24b__Instruct` |
  | `Entity__Qwen__Coder__Version_2__Size_32b__Instruct` | `Entity__Qwen__Coder__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Coder__Version_2__Size_7b__Instruct` | `Entity__Qwen__Coder__Version_2_5__Size_7b__Instruct` **(new)** |
  | `Entity__Qwen__Plus__Version_3` | `Entity__Qwen__Plus__Version_3_5` / `__Version_3_6` / `__Version_3_7` |
  | `Entity__Qwen__Version_2__Size_14b__Instruct` | `Entity__Qwen__Version_2_5__Size_14b__Instruct` |
  | `Entity__Qwen__Version_2__Size_32b__Instruct` | `Entity__Qwen__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Version_2__Size_72b__Instruct` | `Entity__Qwen__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Version_2__Size_7b__Instruct` | `Entity__Qwen__Version_2_5__Size_7b__Instruct` |
  | `Entity__Qwen__Version_2__Size_7b__Omni` | `Entity__Qwen__Version_2_5__Size_7b__Omni` **(new)** |
  | `Entity__Qwen__Version_35__Size_122b_a10b` | `Entity__Qwen__Version_3_5__Size_122b_a10b` |
  | `Entity__Qwen__Version_35__Size_397b_a17b` | `Entity__Qwen__Version_3_5__Size_397b_a17b` |
  | `Entity__Qwen__Version_3__Size_122b_a10b` | `Entity__Qwen__Version_3_5__Size_122b_a10b` |
  | `Entity__Qwen__Version_3__Size_27b` | `Entity__Qwen__Version_3_5__Size_27b`, `Entity__Qwen__Version_3_6__Size_27b` |
  | `Entity__Qwen__Version_3__Size_35b_a3b` | `Entity__Qwen__Version_3_5__Size_35b_a3b` |
  | `Entity__Qwen__Version_3__Size_35b` | `Entity__Qwen__Version_3_6__Size_35b` **(new)** |
  | `Entity__Qwen__Version_3__Size_397b_a17b` | `Entity__Qwen__Version_3_5__Size_397b_a17b` |
  | `Entity__Qwen__Version_3__Size_9b` | `Entity__Qwen__Version_3_5__Size_9b` |
  | `Entity__Qwen__Vl__Version_25__Size_72b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_32b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_32b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_72b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_72b__Instruct` |
  | `Entity__Qwen__Vl__Version_2__Size_7b__Instruct` | `Entity__Qwen__Vl__Version_2_5__Size_7b__Instruct` |
  | `Entity__Ling__Version_1t` | `Entity__Ling__Size_1t` **(new)** |
  | `Entity__Ling__Version_2_6` | `Entity__Ling__Version_2_6__Size_1t` **(new)** |
  | `Entity__Ring__Version_1t` | `Entity__Ring__Size_1t` **(new)** |
  | `Entity__Ring__Version_2_6` | `Entity__Ring__Version_2_6__Size_1t` **(new)** |
  | `Entity__Ring_1t__Free__Version_2_6` | `Entity__Ring__Version_2_6__Size_1t` |

  One constant is ADDED with no removal counterpart: `Entity__Tts__Version_1__Hd`
  (`"tts@1{hd}"`, item 5).
- **beta is ALWAYS a release stage, never an identity.** The two axes were already
  independent by construction (`DetectStageFromID` scans the ID *without* stripping), but one
  row asserted beta on both: vercel's `interfaze/interfaze-beta` arrives with an empty
  `raw_family`, so the leading-token pipeline promoted the trailing `beta` into the **variant**
  slot, giving the key `interfaze/beta` while the same record carried `Stage=beta`. That is
  contradictory rather than merely redundant — it splits a lab's beta and non-beta spellings of
  one artifact into two entities that the stage axis simultaneously calls the same model at
  different maturities. A curated exact-ID pin lands the row on the bare `interfaze` family and
  its stage still reads beta. This **reverses an earlier documented exception** that kept beta
  in that key while unifying only the grok line; the comment recording the old rule is
  rewritten. New LOUD codegen guard `ValidateNoBetaInIdentity` aborts the bake if any future
  decomposition puts beta into a key — either as the variant or as an identity modifier —
  naming the offending entity key and the model IDs that landed on it. **No allowlist**: the
  one exception was resolved by curation rather than exempted, and an allowlist would let the
  next one accumulate silently. A rename, so the entity census does not move.
- **`turbo` demoted to an attribute for kimi and minimax** (`parse/data/modifier_class.json`
  `family_overrides`, the glm precedent). Turbo stays IDENTITY globally — `gpt-4-turbo` is a
  different artifact from `gpt-4` — and is demoted only where curation established it names a
  serving speed tier over the *same* artifact. The evidence differs in strength between the two
  and the curated comment says so: **kimi** has repo-identity proof (moonshot serves
  `kimi-k2-thinking` and `kimi-k2-thinking-turbo` from the identical Kimi-K2-Thinking Hub repo,
  so the turbo spelling cannot denote different weights); **minimax** is graded *lower
  confidence* — no repo-identity proof, just the rev-2 URL census resolving the M2.7 /
  M2.5-highspeed serving names back to the plain repos plus lab-practice inference, flagged as
  the first row to revisit. Three entities fold into their plain siblings
  (`kimi/k@2{turbo}`, `kimi/k@2.6{turbo}`, `minimax/m@2.7{turbo}`); the turbo ID spellings
  survive as **Admitted provider-ID nomina** on the merged entities — a demotion changes what
  is *identity*, never what is *recorded*. Three `Entity__*` constants are removed and none is
  renamed, since the surviving siblings' keys never changed.
- **The z8w3 suppression seed still ships EMPTY, and its collision guard proved itself.** The
  first entry attempted (kimi turbo) was rejected at codegen: suppressing the modifier would
  have made `kimi/k@2{turbo}` and the pre-existing `kimi/k@2` both prefer the naming
  `kimi/k@2`, and the guard's own message diagnosed it — *"the modifier is evidently NOT
  redundant — it distinguishes them"*. That is what routed the change to a modifier-class
  demotion instead. The seed's optional `source_url` also picks up the curated-claims
  **archive policy**: present-but-live is now a loud load-time rejection, while a
  missing/corrupt seed still degrades to "no suppression".
- **`series` filter flags are real** (`--provider`, `--quant`, `--status`). They parse on the
  shared flagset for every subcommand, so `bestiary series --provider cohere` was accepted
  and then silently ignored — the worst shape for a filter, since the output looks like an
  answer. They now narrow the **entity list inside each release**, as per-entity predicates
  satisfied by an entity's *instances*: an instance from that provider, an instance carrying
  a matching `QuantVRAM` row, an instance whose model has that release status. Combined
  filters must be satisfied by **one instance simultaneously** (`--provider=X --quant=Y` means
  "X serves it at Y", never "X serves it *and* somebody serves it at Y" — the per-dimension
  reading can report a pairing that does not exist). The drops **cascade**: an emptied release
  is omitted, an emptied line is omitted from both views, and the listing's counts are
  post-filter, so the listing and the detail view can never disagree. An unknown `--quant` or
  `--status` is rejected with an actionable error before any view is computed (the
  `parseQuantFilter` precedent — never a silent empty result), and a selector naming a real
  line the filters empty gets its own actionable error rather than `ErrNotFound`, because the
  selector was good and the filter was what matched nothing. `--db-path` stays **rejected**:
  the view is still registry-static.
- **Canonical-provider preference applies to exact-ID lookups** (`resolve.go`). `bestiary show
  claude-sonnet-4-5-20250929` reported Provider `302ai` for a first-party Anthropic model. The
  preference carried an exact-ID carve-out justified as "those have deterministic
  cross-provider identity" — but that conflated the *identity* an input resolves to (one
  group, stable across runs, which the ID-based grouping already guarantees) with *which* of
  the co-hosting providers becomes the representative a single-model consumer renders. The
  latter was whichever row the static registry listed first: deterministic, but arbitrary.
  Every provider-unqualified canonical-form lookup now prefers the family's curated
  `CanonicalProvider()` when it is non-empty and present in the match set, else returns the
  full match set unchanged. Provider-qualified forms (PURL namespace, `LookupModelByProvider`),
  the multi-group `ErrAmbiguous` path, and `--format=raw` are untouched.
- **Curated claim `SourceURL`s are archive.org snapshots**, enforced at load. A claim is
  evidence of what a lab published, and the model cards and docs pages it cited are edited and
  deleted without notice, so a live URL silently stops attesting the claim. All five curated
  claims now cite a snapshot captured at claim time, and `parseNomenClaims` **rejects** a
  non-archive `source_url` with an actionable error. No new field: the snapshot embeds the
  original URL verbatim in its tail, so the live address stays recoverable. The file-level
  contract is unchanged — a missing or corrupt claims file still degrades gracefully to an
  empty table (the `lineage.go` precedent); a claim that is *present* and violates the policy
  is loud.
- **Decomposition corrections** (curated exact-ID overrides, the dracarys precedent):
  `Qwen2.5-32B-EVA-v0.2` was read as the compound family `qwen2.5-32b-eva` with EVA's own
  release in the variant slot — it is now `eva@0.2#32b`, with the base relationship promoted
  from a family token to an explicit `DerivationFinetune` edge to `qwen@2.5#32b`.
  `command-a-plus-05-2026` was **split across two entities** by a provider disagreement
  (cohere's `raw_family` mapped to variant `a`, dropping the `+`; nano-gpt's empty
  `raw_family` captured `command-a-plus` whole) — both rows now converge on `command/a-plus`,
  the sibling shape `command/r-plus` already had. cortecs glues the major version onto the
  *variant* (`claude-opus4-5` is Opus 4.5, not an Opus 5), which minted four phantom
  `claude/opus@5…@8` entities and stranded the cortecs instances away from the real ones;
  four curated pins merge them back. Entity census 975 → 971 — the first two are renames, the
  cortecs merge retires exactly the four phantoms.
- **The family-`o` junk bucket is emptied, without evicting the real o-series.** vercel labels
  a swathe of unrelated models with the upstream `raw_family` `"o"` — the OpenAI o-series
  family — and bestiary faithfully preserved the attestation, so Alibaba's Wan video models,
  OpenAI's TTS speech models, quiverai's arrow and Cohere's rerankers all decomposed into
  family `o` and shared one entity with the genuine o-series. The mislabel is **upstream's,
  not a mis-parse**: the vendored catalog carries `family="o"` verbatim on 17 vercel rows and
  2 digitalocean ones, and the ID-driven path already decomposed every one of them correctly
  when `raw_family` was empty — so the correction only stops a junk label from overriding an
  answer that was already right. 13 over-captured rows re-home to `wan` / `tts` / `arrow` /
  `rerank`, plus 2 vendor-leak rows (`voyage/rerank-2.5` and its lite sibling, labelled with
  the **org** rather than a family — the leak the enforce ledger exists to correct). The
  genuine o-series is untouched: `openai/o1` and `openai/o3` still resolve through the
  OpenAI-line canonicalization to `gpt/o@N`. (At the time of this entry digitalocean's
  hyphen-glued `openai-o1` / `openai-o3` / `openai-o3-mini` still kept family `o`; the
  **closing Impl-UAT batch below** finished the unification, converging those dashed
  spellings onto `gpt/o@1` / `gpt/o@3` / `gpt/o@3{mini}` so family `o` empties entirely.)
  Entity census 971 → 982: a bucket holding
  many distinct models becomes many distinct entities, so this is the branch's one correction
  that *adds* keys (15 new, 4 retired).
- **Codegen emitted no `ParamSize` on a lineage parent** (`lineageLiteral`). The first curated
  edge to name a sized parent exposed it: the runtime `lineage.json` path returned
  `qwen@2.5#32b` while the baked `ModelInfo.Lineage` said `qwen@2.5` — the same curated edge
  disagreeing with itself across the two paths.
- **`Family.CanonicalProvider()` had no mapping for `command`**, so provider-unqualified
  lookups of Cohere models fell through to the full match set. Cohere publishes the Command
  line; the mapping now says so (base family plus the `command-a` / `command-r` compound
  spellings the catalog emits).
- **PURL render restricted to HuggingFace** (behavior change; the previous output was
  spec-invalid). `ModelRef.Format(SchemePURL)` used to interpolate the *serving provider*
  as the purl namespace — `pkg:huggingface/anthropic/claude-opus-4-1` for a model that has
  no HuggingFace repo at all, and `pkg:huggingface/huggingface/meta-llama/Llama-3.3-70B-Instruct`
  (double-`huggingface`) for HuggingFace's own rows. A purl is a foreign key into someone
  else's registry, so it is now minted only where the artifact's registry home is known:
  the ~51 huggingface-provider instances, whose raw ID already *is* the Hub org/repo path,
  render `pkg:huggingface/<org>/<repo>`; every other provider renders `""`, and
  `Designations()` **drops** the purl entry rather than emitting an empty-valued
  designation (3 designations for a non-HF ref, 4 for an HF one — canonical stays
  `Preferred`). The **input** side is deliberately unchanged (Postel): `Resolve` and
  `--scheme purl` still accept the legacy `pkg:huggingface/<provider>/<raw-id>` spelling,
  since downstream SBOMs may hold strings bestiary itself emitted. The provider-independent
  identifier story is the entity-level Hub nomen above; this render is the stopgap.
- **Empty-raw claude version recovery**: `claude-3.5-haiku` / `claude-3-5-haiku`
  empty-raw forms now decompose to `(claude, haiku, 3.5)` instead of dropping the
  version.
- **MM-YYYY month leak**: a trailing `MM-YYYY` date no longer leaks its month into
  `Version` (`command-r7b-12-2024` → version `""`, date `2024-12`; also
  `command-r7b-arabic-02-2025`, `command-a-plus-05-2026`).
- **Command R7B identity**: `command-r7b*` ids keep the variant whole and carry the
  size — entity key `command/r7b#7b` ("Command R7B" is Cohere's own model name;
  `command-r` and `command-r-plus` unchanged).
- **Bedrock dotted-namespace convergence**: `us./eu./au./jp./global.` vendor-dotted
  Bedrock ids now recover the same version as their plain siblings and share one
  entity (previously they split into version-less sibling entities).

### Added (naming policy)
- **Redundant-modifier suppression machinery** (ships with an **empty seed** —
  entries are curated case-by-case): a curated seed entry marks a modifier
  redundant for an entity, making the spelling with the modifier an `Admitted`
  nomen while the `Preferred` value omits it. The entity key is never changed
  and the policy is fully reversible; with the empty seed every rendered name
  is byte-identical to the key (census-fenced). Loud codegen validators reject
  malformed or colliding seed entries.

### Data
- **models.dev snapshot refreshed (2026-07-23)**: 5,654 → 5,765 models,
  162 → 170 providers, 247 → 263 metadata rows. Release-state censuses move
  accordingly (entities 947 → 958, nomina 3,743 → 3,797, series 411 → 419,
  releases 659 → 671, sized 323 → 319); the per-batch arithmetic in the entries
  below is stated relative to each batch's own base and still holds. Upstream
  retired the `kimi-for-coding` `k2p5/k2p6/k2p7` ids (their corpus rows and the
  `k2p7` curated override retire with them) and the `ollama-cloud` `kimi-k2:1t`
  row (that negative control retires; the r1t2 control remains).

### Added (tooling)
- **Concept & architecture docs**: `docs/CONCEPTS.md` (the working vocabulary —
  entity, nomen, appellation, canonicalized expression, series/release — with
  its ISO 1087 / IFLA-LRM/LRMoo grounding) and `docs/architecture.md` (the full
  architecture with ASCII diagrams: data pipeline, parse precedence, naming
  layer, provenance core, storage, test architecture, version axes), both
  cross-referenced from README and AGENTS. Also corrects two stale README
  claims (the designation layer is active, not deferred; generated constants
  are the entity-level `Entity__*` surface, not `Model__*`).
- **Release binaries**: the release tagger now pushes tags with a token minted
  from the release GitHub App, so tag pushes trigger the new `release-build`
  workflow — `bestiary` binaries for linux/darwin x amd64/arm64 (plus sha256
  sums) attach to each GitHub release from the next release onward.
- **Single go:generate directive**: the generator no longer emits a
  `//go:generate` directive into its own generated files (three of them carried
  one, so `go generate ./...` ran the generator three times per invocation);
  the one directive lives in hand-owned `bestiary.go`.
- **Makefile**: `make build` / `test` / `vet` / `fmt` / `generate` / `gates` /
  `install` encode the project's invocation discipline (`CGO_ENABLED=0
  GOWORK=off`) once; `make gates` is the full pre-commit suite including the
  regen-is-byte-clean check.

### Testing
- Fixture-extraction completion: corpus census 65 → 102 (every remaining
  targeted inline table migrated 1:1 under the three-guard discipline);
  unknown-suffix-overflow captured as a corpus; entity-projection probes on the
  two projection corpora; `TESTING.md` documents the census and what
  deliberately stays inline.
- Research deliverables (report-only, decisions user-gated): the turbo/fast
  per-family evidence report (URL-census methodology over the baked metadata
  links) and the general-beta evidence memo.
- IRI round-trip fences: a delimiter torture set (`/`, `@`, `#`, `{`, `}`, `,`, the
  ref-level `[attributes]` brackets) plus every entity and every model ref in the
  committed registry, each asserted to decode back byte-identically; the escaping
  contract itself and the empty-base/verbatim-base rules are pinned separately.
- PURL fences: a real catalog HF row renders the repo-path purl, every huggingface-provider
  row is checked against double-namespacing, a non-HF ref renders `""`, no designation is
  ever emitted empty-valued corpus-wide, and the lenient legacy purl *input* still resolves.
- Parse-residual capture corpora: 59 → 65 corpora (azure serving-host,
  meta-llama no-slash census, namespace suffix transparency, text-embedding
  sole-variant, grok documented-residual, region capture), all under the
  three-guard discipline.

## [0.2.6] — 2026-07-16

**Schema:** `0.3.0` → `0.4.0` (additive). SQLite store schema **unchanged** at `6`.

The **full-bulk `#size` re-key** epoch (GH#9): one shared enrichment now sizes every
model whose ID carries a parameter-size token, so a `#size` segment is no longer
confined to the handful of curated `quant_vram.json` entries.

### Added

- **Parameter-shape fields** on `ModelInfo` (`TotalParams`, `ActiveParams`,
  `PerExpertParams`, `ExpertCount`), decomposed from `ParamSize` — derived
  presentation facts, never entity-key material. Grouped along shape joints and
  never cross-computed (an NxM MoE token like `8x22b` sets `ExpertCount` +
  `PerExpertParams` but no total). Each field is an **in-domain NULLable integer**
  under the new `ParamShapeNull` (`-1`) sentinel contract: `-1` means "not populated
  by parser or curation" (the shape carries no such fact, or the size is unknown), a
  positive value is an attested count, and a genuine `0` is reachable **only** for
  `ExpertCount` (a dense shape attests zero experts). An unsized model bakes all four
  as `-1`. The schema pins `minimum: -1` on each. Every row now emits all four fields
  explicitly (a `0` is meaningful and must be distinguished from the NULL sentinel).
- **`EnrichedParamSize(id)`** — the single param-size precedence authority
  (pin > mechanical > `ParamSizeFor`), shared by the two runtime enrichment joints
  (`toModelInfo`, `scanModelInfo`) and the codegen bake, plus `ValidateParamSizePins`
  (a codegen-time guard that rejects a non-canonical curated pin token).
- **Release-stage axis** (`stage.go`, [#13]): a closed int enum `ReleaseStage`
  (`StageNone`/`StageStable`/`StagePreview`/`StageBeta`/`StageAlpha`/
  `StageExperimental`/`StageLatest`/`StageOriginal`/`StageOther`) with
  `ParseReleaseStage` (CLI/config parse), `DetectReleaseStage` (ID-token
  detection, known-members-only), and `DetectStageFromID` (the shared ID scanner).
  `ModelInfo` gains `Stage`/`StageRaw`, derived from the ID at the SAME enrichment
  joints as `ParamSize` (so a live-sync row and its baked static row always agree).
  Stage is DELIBERATELY separate from `ModelStatus`: `Status` is upstream-declared
  lifecycle, `Stage` is ID-derived — `show`'s instance table renders them under
  distinct `STATUS` / `STAGE` columns. `StageOther` is a RESERVED bucket for a
  future non-ID feeder (Quantization precedent); the ID path never produces it.
  `StageLatest`/`StageOriginal` name a moving target, not a fixed artifact property.
  Schema `0.4.0` gains additive `Stage`/`StageRaw` properties and a `ReleaseStage`
  `$def` (neither required).
- **`TYP(4K)` quant-table column** (`cmd/bestiary`): the per-quantization VRAM
  sub-rows gain one typical-context column — `(QuantVRAM).EstimateVRAM(4096)`
  recomputed from the row's stored arch-facts — alongside the max-context `VRAM`
  figure, so a realistic-run cost is readable at a glance. Renders an em dash
  (`—`) when the model's maximum context is below 4096 or unknown (a figure at a
  context the model cannot serve would be meaningless), and stays weights-only on
  a `PARTIAL` row (no phantom KV delta). A companion `TYP(8K)` column was
  considered and dropped at acceptance — the ruling is a single 4K column (the
  8K delta added noise, not signal).
- **New `testcase` / `testcase/assert` packages** — a pure-data JSON case-corpus
  harness (stdlib-only; classification + provenance + mutation metadata with
  non-vacuity validation) — plus `TESTING.md` documenting the corpus standard
  the parse/entity/quant/VRAM suites migrated onto.

### Changed — MIGRATION NOTE (entity keys)

- **Full-bulk `#size` re-key.** Entity keys now carry a `#size` segment for every
  model whose ID yields a size token (mechanical `ExtractParamSizeToken`), or that
  matches a curated pin. This re-keys roughly a third of catalog entities
  (`llama-3.1-8b-instruct` → `llama@3.1#8b{instruct}`; `qwen3-30b-a3b` →
  `qwen@3#30b-a3b`; `mixtral-8x22b` → `…#8x22b`). Callers that pinned pre-v0.2.6
  unsized keys (e.g. `dracarys` → `dracarys#72b`, `mythomax` → `mythomax#13b`) must
  update to the sized key. Curated `llama-4` scout/maverick IDs pin to their full
  expert shape (`#17b-16e` / `#17b-128e`) so every spelling of one artifact keys to
  ONE entity, and a suppress-pin keeps a context-tier token out of the key
  (`qwen3-coder-next-fp8-1m` stays unsized — `1m` is a context marker, not params).
- **No store migration.** The size is a pure function of the ID re-derived at both
  read joints, so the SQLite store stays at schema `6` (no `param_size` column).
- Live-sync rows are now sized identically to the baked static rows for the same ID,
  so the most-recent-wins merge can never de-size an `(ID, Provider)`.
- **What stays stable:** entity keys for models whose ID carries no size token and no
  curated pin are byte-identical to v0.2.5 (test-pinned by the successor invariant's
  enrichment-consistency sweep). Curated lineage needs zero churn: lineage lookups
  resolve exact-key-first with a size-stripped fallback, so an unsized curated edge is
  inherited by every newly sized sibling without re-keying `lineage.json`.
- **Stage-token vocabulary migration (no entity-key change).** `preview`, `latest`,
  and `original` left the modifier-class attribute set — they now populate the
  `Stage` axis instead of rendering as `[preview]`/`[latest]` attributes. They are
  routed out of both render segments AND the entity key BEFORE the modifier
  identity fail-safe, so the key is unchanged (they were attribute-class =
  key-excluded before, stage-routed = key-excluded after). The tokens STAY in the
  `Modifier` data field (so constant names and the `[attr]` resolve filter are
  byte-stable). `beta` is detect-without-strip: `Stage=StageBeta` is set wherever a
  standalone `beta` token appears, independent of the key. For the
  **grok-4.20 line**, curated exact-ID overrides now **unify** the beta-alias spellings
  onto their non-beta entity — `grok-4.20-beta-0309-reasoning` and the `…-reasoning-beta`
  / dashed / multi-agent variants key `grok@4.20{…}`, the same entity as the official
  `grok-4.20-0309-reasoning`, while still carrying `Stage=StageBeta`. The **general beta
  freeze stays for non-grok names** (e.g. `interfaze-beta` keeps `beta` in its key);
  a wholesale beta re-key is deferred ([#13]).
- **Version-less `llama-4` `@4` unification.** The `llama-4` scout/maverick spellings
  that omit the `-4-` version token — the AWS Bedrock dotted forms
  (`meta.llama4-scout-17b-instruct-v1:0`, `us.meta.llama4-…`) and the aggregator
  provider-prefixed forms (`cerebras-…`, `groq-…`) — now carry a curated `@4` version
  pin (exact-ID override) so they merge into the existing `@4` entity their canonical
  siblings key (`llama/scout@4#17b-16e{instruct}`, `llama@4#17b-128e{instruct}`).
  This removes the pre-existing version-presence split. (Note: `scout` is a curated
  `llama` variant member so it keys under `/scout`, but `maverick` is not, so the
  official maverick entity is `llama@4` — the pins set variant `""` to merge, not mint
  a new entity.)

### Changed — BREAKING (Go API: exported constant renames)

- **Meaningful collision-group constant names** (`models_constants_gen.go`, r66e).
  Same-base-name model constants that previously fell back to opaque ordinals
  (`Model__…__5_1` / `_2` — visually indistinguishable from a `5.1` version segment)
  are now disambiguated by their backend-route path prefix, the axis along which the
  collisions actually differ (the same model re-served under a `TEE/`, `Pro/`,
  `stealth/`, `openrouter/`, or vendor-path route). Renames include
  `Model__Kilo__Free_1`/`_2` → `Model__Kilo__Free__KiloAuto`/`__OpenRouter`,
  `Model__NanoGPT__GLM__5_1`/`_2` → `Model__NanoGPT__GLM__5__Tee`/`__ZaiOrg`, and 56
  others (58 constants total). The alphabetical-raw-ID ordinal fallback is retained
  only for a genuine tie no discriminator separates (e.g. the same route + date under
  a punctuation-only ID difference). Any code referencing a renamed `Model__…_N`
  constant must update to the new route-suffixed name.

## [0.2.5] — 2026-07-15

**Schema:** `0.2.0` → `0.3.0` (additive). SQLite store schema `5` → `6`.

The **models.dev harmonization + provenance history** epoch: ingest all three
models.dev JSON artifacts (api.json, models.json, catalog.json), bake the new
provider-agnostic metadata dimension (benchmarks, licenses, links), refresh the
static catalog from a vendored July snapshot, make the ingest log an append-only
history with a round-trip export, and land the `#size` lookup prerequisites.
Relates [#9] (size prereqs) and [#13] (stage tokens remain a deferred dimension).

### Added

- **Three-artifact ingestion** (`wire.go`, `client.go`): exported
  `ParseAPIJSON`/`ParseModelsJSON`/`ParseCatalogJSON` plus
  `Client.FetchModelMetadata`/`FetchCatalog` on one shared decode path; wire
  types refreshed for the upstream schema drift (`reasoning_options`, `status`,
  `description`, cost tiers/audio/`context_over_200k`); benchmark scores
  tolerate upstream `number|string` (`ScoreRaw`).
- **Entity metadata dimension** (`metadata.go`): `EntityMetadata`
  (description, license, typed `Links`, `Benchmarks`) attached at the entity
  level via an alias-first join with a two-tier miss policy (unlinked report /
  genuine-absence standalones); `BenchmarkResult` keeps criterion identity,
  harness, score, and claim attribution (`SourceURL`, lab-reported) as separate
  fields; closed enums `ModelStatus`, `LinkType`, `ReasoningOptionKind`;
  `ModelInfo` gains description/status/reasoning-options/cost-tier fields.
- **CLI**: new `entities` subcommand (enumerates every entity, including
  metadata-only standalones); `list --status`; `sources --history` and
  `sources --export` (union of store history and the curated seed, promotable
  into `datasources.json`); one stderr notice when views serve embedded-only
  metadata; sync drift warning; benchmark table renders top-5 with a count
  footer and ellipsis-truncated names.
- **Vendored codegen input** (`parse/data/modelsdev/`): committed
  `catalog.json` + `SNAPSHOT.json` provenance sidecar; codegen is fully offline
  and fails loudly on a missing/corrupt snapshot; new `models_metadata_gen.go`
  bake; `modelsdev_unlinked.json` join report + `modelsdev_aliases.json`
  curation; a real-input regen up-to-date guard.
- **Store v6** (`store.go`): `dataset_ingested` becomes an append-only history
  (composite primary key, `INSERT OR IGNORE`, current = latest); new
  `entity_metadata`/`metadata_benchmarks`/`metadata_links` tables (including
  `raw_family`); the `models` table persists the new instance-level fields;
  presence-guarded self-heals for intermediate v6 caches; sync now persists
  provenance (ingest rows, metadata, attestations).
- **`#size` prerequisites**: lineage keys carry `param_size` with a
  size-agnostic fallback (unsized edges apply to every sized sibling;
  size-specific edges win); `Resolve()` ambiguity grouping and `ModelRef` gain
  the size axis.
- **Stage/mode identity granularity**: `omni`/`livetranslate` are
  identity-class modifiers, `realtime` is an attribute-class serving tier, and
  the laguna `xs`/`m` product lines are curated variants — distinct lab models
  no longer collapse into shared entities.

### Changed

- Static catalog refreshed from the July models.dev snapshot: 162 providers,
  5,654 models, 810 entities (April baseline: 138 providers, ~4,300 models).
- `UpstreamSchemaVersion` pinned to the July schema; ingest `parser_schema`
  is now `3`; `datasources.json` is schema v3 (multi-row ingest history).

### Fixed

- Metadata join family-presence gate no longer synthesizes standalone entities
  for models the catalog actually serves (the presence check now uses the same
  family canonicalization as the enrichment pipeline).
- Sized entities no longer silently lose lineage; `gpt-realtime-*` versions are
  no longer swallowed by the `realtime` token; the laguna metadata collision is
  resolved; parse-time suffix/modifier tie-breaks are total-ordered
  (determinism by construction).

## [0.2.4] — 2026-07-11

**Schema:** `0.1.0` → `0.2.0` (additive). SQLite store schema `4` → `5`.

The **VRAM + quantization + provenance** epoch: model the memory footprint of a
model at each quantization, make parameter size part of entity identity, ingest
the Ollama registry, and record where every model row came from. Addresses the
roadmap VRAM/quantization issue [#12] (and the size/param+quant strand [#9]).

### Added

- **Quantization enum** (`quantization.go`): closed `Quantization` int enum over
  the GGUF/llama.cpp scheme names (`f16`/`bf16`/`f32`, the `q*` k-quants, the
  `iq*` i-quants) plus reserved HF-ecosystem members (`awq`/`gptq`/`int8`/`int4`),
  with `none` as the unquantized zero value and `other` as the fail-safe bucket
  for a recognized-but-unmapped tag. `String`/`MarshalText`/`UnmarshalText`
  serialize the canonical lowercase wire name (case-insensitive on the way in);
  `BitsPerWeight()` returns the authoritative llama.cpp bits-per-weight;
  `DetectQuantization(id)` and `ParseQuantization(s)` extract a quant from a model
  ID / CLI argument (unknown → `other` on detection, an actionable error on parse).
- **Per-quantization VRAM** (`vram.go`, `QuantVRAM` on `ProviderInstance` and
  `ModelInfo`): each quant row carries the ground-truth ingested `WeightsBytes`
  (GGUF file size) and an estimated `VRAMBytes` = weights + KV-cache **baked at the
  model's maximum context**, with **no overhead constant** (`VRAMFormulaVersion 2`).
  When the architectural facts (layers / KV-heads / head-dim) are absent, the KV
  term is excluded and `VRAMEstimatePartial` is set true so `VRAMBytes` is a
  weights-only lower bound, never a silent under-estimate. `(QuantVRAM).EstimateVRAM(ctx)`
  recomputes the figure at a caller-chosen context.
- **Parameter size as identity** (`EntityRef.ParamSize`, `ModelInfo.ParamSize`):
  `EntityRef.String()` gains a `#paramsize` segment —
  `family[/variant][@version][#paramsize]{identity-mods}` — so `llama@3.3#70b{instruct}`
  and `llama@3.3#8b{instruct}` are distinct entities. The segment is omitted when
  size is unknown, so every existing entity key stays byte-identical.
  `EntityByTuple` and the tuple parsers are `#`-aware.
- **Curated Ollama ingest** (`cmd/bestiary-ollama`, `parse/data/quant_vram.json`):
  a network-gated, polite-bot offline tool that joins the Ollama registry to the
  entity model. Community finetunes are **kept**, never dropped; lineage to a base
  is **inferred** (Ollama exposes no base-model marker) via tuple decomposition and
  curated alias tables — base-known finetunes carry an inferred finetune lineage
  edge, base-unknown ones become standalone entities.
- **BCNF data-source provenance** (`datasource.go`, `parse/data/datasources.json`):
  a normalized `DataSource` / `DatasetIngested` / `EntitySource` core (the join
  table carries the entity↔source many-to-many relation) persisted with real
  foreign keys in the SQLite store. `Entity.Sources` is a derived, sorted read
  projection of the join table — not a source of truth. `DatasetIngested` carries
  no URI (a transitive dependency obtained by FK join to `DataSource`); its ingest
  timestamp is a committed snapshot, never a codegen wall-clock stamp.
- **CLI**: `bestiary sources <key>` lists the per-source provenance for an entity
  (joined ingest date and URI, sorted by source); `show`/`providers` JSON carries
  `Entity.Sources`. A `--quant` filter selects a quantization where applicable.
- This `CHANGELOG.md`.

---

## [0.2.3] — 2026-06-08

**Schema:** `0.0.3` → `0.1.0` (additive). Module PR [#20]. Merge commit `e636b2d`.

The **entity model** epoch: deduplicate and link models across providers by their
canonical identity, track derivation lineage, and separate serving-host from model
identity. Resolves the three coupled roadmap issues [#11] (lineage), [#16]
(serving-host), and [#18] (entity-linking).

### Added

- **Entity layer** (`entity.go`): `EntityRef` — the canonical identity tuple
  `(Family, Variant, Version, identity-modifiers)`, keyed by `EntityRef.String()`
  as `family[/variant][@version]{identity-mods}`. `Entity` aggregates every
  provider/host instance of one identity, with `ProviderInstance` carrying the
  per-instance attributes (host, price, context, max-output). `CapabilityUnion`
  ORs capabilities across instances. New APIs: `Entities()` and
  `EntityByTuple(family, variant, version, identityModifiers...)`, both returning
  defensive deep copies so callers cannot corrupt the registry index.
- **Modifier classification** (`modifierclass.go`, `parse/data/modifier_class.json`):
  `ModifierClass` enum (`Identity` / `Attribute`) with `ClassifyModifier(token, family)`.
  A curated global table plus per-family overrides decide whether a trailing
  modifier is part of identity (renders in `{...}`) or a per-instance attribute
  (renders in `[...]`). Unknown tokens default to **Identity** (fail-safe: never
  silently collapse two artifacts into one entity). `EntityModifiers` /
  `attributeModifiers` project a modifier set onto each class.
- **Serving-host dimension** (`host.go`, `host_detect.go`, `parse/data/hosts.json`):
  `Host` string type (`HostNone`/`HostAzure`/`HostAWS`/`HostGCP`/`HostCloudflare`)
  with `DetectHost(id)`. Detection is curated ID-prefix-only and **never consults
  `Provider`**; namespaced IDs (containing `/`) are never split. Host is a
  per-instance attribute, never part of entity identity. Guards against the
  v0.2.2 blanket provider-prefix-strip bug.
- **Lineage DAG** (`lineage.go`, `derivation.go`, `parse/data/lineage.json`):
  `DerivationKind` enum, `LineageEdge` / `LineageRecord`, and cycle-safe
  `Ancestors` / `Descendants` traversal over a curated derivation ledger
  (fine-tunes, distillations, multi-parent merges). `real=false` flags synthetic
  catalog-absent fixtures. Seeded with the Dracarys/Hermes/MythoMax/Solar/Yi cases.
- **CLI**: `bestiary providers <tuple>` lists every provider serving a given
  identity tuple; `bestiary show --by-entity` groups output by entity. The tuple
  parser accepts the `{identity-mods}` segment.

### Changed

- `ModelRef.formatCanonical()` is now modifier-class-aware: identity modifiers
  render in `{...}`, attribute modifiers in `[...]`. Attribute-only models render
  byte-identical to v0.2.2.
- `family.go`: registered `solar`, `yi`, `mythologic`, `huginn` as base families;
  added ID-family overrides for Dracarys-72B and MythoMax.
- `.gitignore`: the `bestiary` binary ignore is now root-anchored (`/bestiary`)
  so it no longer masks same-named nested paths.

### Fixed

- `fast` is demoted from a global identity modifier to a per-family **attribute**
  for tiered families (claude/glm/kimi/deepseek/minimax) after profiling showed it
  is a speed-tier label there, while remaining identity-bearing for families like
  grok/imagen/veo where it denotes a distinct model.
- The 70B Dracarys lineage `child_ref` is aligned to its actual decomposed entity
  key (`dracarys{instruct}`, version empty) so the edge resolves to the real node.

---

## [0.2.2] — 2026-06-06

**Schema:** `0.0.2` → `0.0.3`. Module PR [#15]. Tag `v0.2.2`. Released via a
release-candidate cycle (`-rc1` / `-rc2` / `-rc3`).

Epoch 2 — **cross-provider decomposition consistency**. The headline outcome:
the canonical `(Family, Variant, Version)` decomposition was driven to **zero
divergence** across providers — the *same* model now decomposes to the *same*
identity tuple regardless of which provider's ID spelling it arrives under
(starting from 388 divergent triples, reduced 68 → 18 → 0 over the rc cycle).
This is the precondition that made v0.2.3 tuple-keyed entity linking possible.
Along the way the project ratified a large set of canonical-representation
decisions for specific model families; they are recorded below because they
define how IDs are *interpreted*, not just how the code is structured.

### Canonical representation & serialization

- **The decomposition tuple is canonical.** Every model decomposes to
  `(Family, Variant, Version, Date, Modifier)`. `Version` is distinct from
  `Date` (an identity version vs. a release stamp). The **cross-provider
  consistency metric** is the 3-tuple `(Family, Variant, Version)` *excluding*
  modifier; that exclusive metric is what reached and is gated at **0**.
- **Four serialization schemes** are the supported renderings of the tuple
  (`CanonicalScheme`):
  - `canonical` (CLI alias **`peasant`**) — `provider/family/variant/version@date[modifier]`
  - `huggingface` (`hf`) — `provider/raw-id`
  - `purl` — `pkg:huggingface/provider/raw-id`
  - `raw` — the original API model ID, verbatim
- **`Modifier` became a list** (`string` → `[]string`): multiple trailing
  modifiers now compose losslessly under a ratified modifier taxonomy. Tokens are
  matched **longest-first** (so `think` cannot shadow `thinking`) and rendered in
  a fixed `canonicalModifierOrder`.

### Family-resolution decision layers (curated data)

The decomposition is governed by a layered set of **embedded, curated JSON
tables** under `parse/data/` (data-only directory — no Go files, to avoid an
import cycle; this was explicitly ratified). The precedence chain:

- `family_overrides.json` — explicit `(raw_family → {family, variant})` mappings;
  highest priority, beats all pattern matching.
- `version_patterns.json` — ordered regexes that split a versioned-variant raw
  family (v-/k-/m-prefix, hyphen-version, no-prefix); first match wins.
- `variant_suffixes.json` — suffix strings stripped to identify a variant when no
  override/pattern matches (re-sorted longest-first at load).
- `family_aliases.json` — the **canonical-winner ledger**: maps a
  mislabel/shorthand family to its canonical family, applied after case-fold and
  before bare-generation split, in *both* parse entrypoints.
- `family_enforce.json` — the **canonical-winner ENFORCE set**: a closed list of
  distinct families that WIN over a disagreeing `raw_family` when the model ID
  itself names the family.
- `families.json` — per-family member lists driving `recoverMemberVariant` and
  the **per-family member-guard**: a `variant → modifier` reclassification fires
  *only* when the token is NOT a curated member of the resolved family — so
  `deepseek-chat`, `sonar-reasoning`, `qwen-turbo` keep the token as a product-line
  variant rather than demoting it to a modifier.
- `vendor_aliases.json` — residual non-provider vendor/namespace prefixes (not in
  `Providers()`) stripped from leading ID segments.

### Ratified per-family canonicalization rulings

- **`meta-llama` / `nemotron` folds:** no-slash `meta-llama-*` folds to the
  `llama` family with its version preserved; `nvidia/llama-3.3-nemotron-*`
  decomposes to the `nemotron` family (was an over-capture under the empty-`raw_family`
  provider).
- **`azure-*` folds → upstream family:** `azure-gpt-*` / `azure-o*` resolve to the
  `gpt` / o-series families. Critically, the earlier **blanket azure
  provider-prefix *strip* was removed** — it destroyed a backend-host label
  (NanoGPT's `azure-` prefix). The host signal was deferred to a dedicated
  serving-host dimension, which became the v0.2.3 `Host` type.
- **o-series restructure** for the OpenAI `o1`/`o3`/`o4`-style reasoning line.
- **Whisper:** a family-gated trailing `-v<int>` is recovered as `Version`
  (e.g. `whisper-v3`) instead of being treated as a modifier.
- **Grok:** negation-aware modifier handling emits an explicit `non-reasoning`
  modifier; the `grok-3-mini-fast-beta` member-guard suppresses a false
  `non-reasoning` negation.
- **Brand casing:** stylized `Provider` / `Family` / `Model__` constants (correct
  vendor capitalization in generated identifiers).

### Three sanctioned non-defect residuals (USER-RATIFIED)

After convergence, exactly **three** decompositions were ratified as intentional
non-defects (not divergence bugs), each feeding later roadmap work:

- **dracarys** — a llama fine-tune whose lineage is lost by folding → motivated
  GH#11 (delivered in v0.2.3).
- **solar** — register-or-accept as its own base family.
- **grok-beta** — `beta` as a release stage → motivated GH#13 (release-stage axis).

### Parsing correctness fixes

- Fixed the `raw_family` **version-extraction gap** (`gpt-mini` ⇒ `gpt-5-mini`):
  version digits sitting between the family prefix and the variant are now
  recovered, clearing the `ReasonVersionDigitsNotExtracted` class from the codegen
  `parse_failures.json` audit.
- ID-driven version-presence consistency, a param-size guard (so a parameter count
  like `7b` is not read as a version), and date-guards for 6-digit / `YYMM` forms.
- `Resolve` ambiguity: `ErrAmbiguous` now renders a two-section listing (canonical
  vs. rehosts) with canonical-provider preference; variant-aware bare-family
  shorthand restores `claude-opus → ErrAmbiguous`.

### Determinism

- A committed cross-provider snapshot + a `divergence=0` gate, plus a
  network-gated **drift smoke** test and snapshot goldens, keep the decomposition
  stable across regens.

---

## [0.2.1] — 2026-05-30

**Schema:** `0.0.2` (unchanged). Tag `v0.2.1`.

**Deterministic & reproducible codegen.** `cmd/bestiary-gen` output is now
reproducible:

- Models sorted by `(Provider, ID)` once after assembly (fixes static-file
  reshuffle between regens).
- Collision `Model__*_N` suffixes assigned by raw-ID alphabetical order (stops
  random `_1`/`_2` flipping).
- `models_constants_gen.go` is byte-identical across regens; `models_static_gen.go`
  is identical modulo the `LastSynced` per-run timestamp.
- Reproducibility guard `TestCodegen_Reproducible_ByteIdentical` (N=100) and an
  up-to-date golden guard added.

---

## [0.2.0] — 2026-05-29

**Schema:** `0.0.1` → `0.0.2`. Tag `v0.2.0`.

**Entity normalization pipeline** — canonical model identity:

- `ModelRef` gains `Version` + `Modifier` (the 8-field tuple).
- New `parse` package: family / variant / version / modifier extraction from the
  API `raw_family` plus the model ID, with embedded curated override data.
- Canonical scheme `provider/family/variant/version@date[modifier]`.
- `Resolve` with `--format {peasant,huggingface|hf,purl,raw}`, PURL namespace
  filter with loose fallback, and `ErrAmbiguous` two-section output (canonical vs
  rehosts) with canonical-provider preference.
- `Model__` constants use double-underscore field separators.

---

## [0.1.1] — 2026-04-08

Tag `v0.1.1`. Ignore the `bestiary-gen` build artifact; regenerate
`models_static_gen.go` with the fixed codegen template (named `Family` type).

---

## [0.1.0] — 2026-04-04

**Schema:** `0.0.1`. Tag `v0.1.0`. Adds `LookupModelByProvider` and the `Models`
registry lookup APIs.

---

## [0.0.2]

Tag `v0.0.2`. The original entity-normalization epoch groundwork:

- New `parse` package: `ParseFamily`, `ExtractDate`, `InferFamilyFromID` with
  embedded JSON data.
- `ModelInfo` gains `NormalizedFamily` / `Variant` / `Date` (codegen-baked).
- `ModelRef` refactored to 6 fields including `ID`; `Format(scheme)` dispatch over
  the `CanonicalScheme` enum.
- New types: `Designation`, `AcceptabilityRating`.

[Unreleased]: https://github.com/dayvidpham/bestiary/compare/v0.2.11...HEAD
[0.2.11]: https://github.com/dayvidpham/bestiary/compare/v0.2.10...v0.2.11
[0.2.10]: https://github.com/dayvidpham/bestiary/compare/v0.2.9...v0.2.10
[0.2.9]: https://github.com/dayvidpham/bestiary/compare/v0.2.8...v0.2.9
[0.2.8]: https://github.com/dayvidpham/bestiary/compare/v0.2.7...v0.2.8
[0.2.7]: https://github.com/dayvidpham/bestiary/compare/v0.2.6...v0.2.7
[0.2.6]: https://github.com/dayvidpham/bestiary/compare/v0.2.5...v0.2.6
[0.2.5]: https://github.com/dayvidpham/bestiary/compare/v0.2.4...v0.2.5
[0.2.4]: https://github.com/dayvidpham/bestiary/compare/v0.2.3...v0.2.4
[0.2.3]: https://github.com/dayvidpham/bestiary/compare/v0.2.2...v0.2.3
[0.2.2]: https://github.com/dayvidpham/bestiary/compare/v0.2.1...v0.2.2
[0.2.1]: https://github.com/dayvidpham/bestiary/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/dayvidpham/bestiary/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/dayvidpham/bestiary/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/dayvidpham/bestiary/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/dayvidpham/bestiary/releases/tag/v0.0.2
[#20]: https://github.com/dayvidpham/bestiary/pull/20
[#18]: https://github.com/dayvidpham/bestiary/issues/18
[#16]: https://github.com/dayvidpham/bestiary/issues/16
[#15]: https://github.com/dayvidpham/bestiary/pull/15
[#13]: https://github.com/dayvidpham/bestiary/issues/13
[#12]: https://github.com/dayvidpham/bestiary/issues/12
[#11]: https://github.com/dayvidpham/bestiary/issues/11
[#9]: https://github.com/dayvidpham/bestiary/issues/9
