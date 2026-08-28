---
title: "[P2] Compound family strings are not reduced, and the two ingest paths disagree"
labels: bug
---

## Problem

A compound family token must reduce to a family plus a variant. Some do not.
The sweep for #43 measured what is left after the earlier fable repair.

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
- `nvidia/llama-3.3-nemotron-super-49b-v1.5` produces the SAME key on both
  paths. A test asserts the equality directly.
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
`docs/research/gh43-parser-conformance-sweep.md`.
