---
title: "[P2] Compound family strings are not reduced, and the two ingest paths disagree"
labels: bug
---

## Problem

A compound family token must reduce to a family plus a variant. Some do not.
The sweep for #43 measured what is left after the earlier fable repair.

A **record**, here and in the sweep report, is one DISTINCT raw id string
within one catalog view, compared case-sensitively, among the rows the
seed-token census matched. The counting rule is stated once in
`docs/research/parser-conformance-sweep.md`, and every count below is
pinned in `TestParserConformance_TokenCensus`.

Confirmed, still open:

| Key | Records | What it should be |
|---|---|---|
| `deepseek-ocr@2` | 3 | `deepseek/ocr@2` |
| `claude-mythos@5` | 2 | `claude/mythos@5` |

`deepseek-ocr` also SPLITS its own line. The same product spelled
`deepseek-ai/DeepSeek-OCR` keys plain `deepseek`, because the `ocr` token
is dropped instead of becoming the variant. One product, two keys, and
neither is right.

`claude-mythos` is the same defect that was fixed for `claude-fable`. That
fix added one curated variant entry. A curated entry does not generalise,
so each new Anthropic tier repeats the defect until the reduction is
mechanical.

Confirmed, and worse: the two ingest paths disagree about ONE model. The
id `nvidia/llama-3.3-nemotron-super-49b-v1.5` decomposes differently on
each side.

| Path | Key |
|---|---|
| lab (metadata) row | `llama-3.3-nemotron-super-49b/v1.5@3.3#49b` |
| served row | `nemotron/v1.5@3.3#49b` |

On the lab path the WHOLE id becomes the family. The lab path has no
upstream family string to lean on, so an id-only decomposition takes
everything it cannot classify.

The DISAGREEMENT is what the sweep measured, and the conformance corpus
pins BOTH keys, one case per path, so it goes red the moment either path
moves. WHERE the two paths must meet is a curation ruling the sweep does
not make, so both cases carry the `EXPECTED_TBD` marker, the same treatment
the class 4 modifiers and the class 5 distill get. The served key is not
automatically the answer: `nemotron/v1.5@3.3#49b` states the version `v1.5`
in the variant slot, which is the separate `v`-token defect. A third
candidate, `nemotron/super@3.3#49b`, reads `super` as the variant and drops
`v1.5`. This issue asks for the ruling; it does not presume it.

Refuted at this tip, and recorded so the fix is not repeated:
`claude-fable`, `claude-fable@5` and `glm-4.1v-thinking/flash` all key
correctly now.

## Scope

Changes:
- Reduce a compound family mechanically: when the first token names a known
  family and the remainder is a single clean token, the remainder becomes
  the variant.
- Make the lab path and the served path agree on one id. The disagreement
  is the bug, whichever key wins.
- Price the change by regenerating and diffing the entity key set.

Non-changes:
- No new per-tier curated variant entry. A curated entry is what failed to
  generalise.
- No change to the fable and glm repairs that already hold.

## Acceptance

- `deepseek-ocr-2` keys `deepseek/ocr@2`, and `DeepSeek-OCR` keys
  `deepseek/ocr`. The two spellings share one line.
- `anthropic/claude-mythos-5` keys `claude/mythos@5`.
- A NEW, unseen Anthropic tier name reduces without a curated entry. A test
  states this with a synthetic tier, so the generalisation is falsifiable.
- The destination is RULED, in writing, with its reason: the served key
  `nemotron/v1.5@3.3#49b`, the lab key
  `llama-3.3-nemotron-super-49b/v1.5@3.3#49b`, or a third key such as
  `nemotron/super@3.3#49b`. Naming the winner is part of closing this issue.
- `nvidia/llama-3.3-nemotron-super-49b-v1.5` then produces THAT key on both
  paths. A test asserts the equality directly, and names the key.
- The two `EXPECTED_TBD` nemotron cases in the GH#43 conformance corpus are
  replaced by real expectations carrying the ruled key.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
  raw family "deepseek-ocr"          raw id "nvidia/llama-3.3-nemotron-super-49b-v1.5"
        |                                       |
        |                            +----------+----------+
        v                            |                     |
  today: family="deepseek-ocr"    lab path             served path
        |                         (id only)          (id + raw family)
        v                            |                     |
  ==> deepseek-ocr@2                 v                     v
                            llama-3.3-nemotron-      nemotron/v1.5@3.3#49b
  wanted: first token is a  super-49b/v1.5@3.3#49b
  known family, remainder            |                     |
  is one clean token                 +--------- must be ---+
        |                                     the same key
        v
  family=deepseek variant=ocr ==> deepseek/ocr@2
```

## Blocked by & Blocks

Blocked by: nothing.
Blocks: nothing measured.

## Related

#43, which measured this. The sweep report is
`docs/research/parser-conformance-sweep.md`.
