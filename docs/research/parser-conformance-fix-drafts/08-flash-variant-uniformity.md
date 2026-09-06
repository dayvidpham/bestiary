---
title: "[P2] flash/flashx must be a distinct-weight variant uniformly"
labels: bug
---

## Problem

A `flash` (or `flashx`) model is typically entirely different model weights
from its base, sometimes a different architecture. It is a VARIANT, and it
must key as one uniformly. Today the parser is inconsistent: two labs key it
correctly, one lab DROPS it, and the GLM vision line demotes it to a modifier.

A **record**, here and in the sweep report, is one DISTINCT raw id string
within one catalog view, compared case-sensitively, among the rows the
seed-token census matched. The counting rule is stated once in
`docs/research/parser-conformance-sweep.md`, and every count below is
pinned in `TestParserConformance_TokenCensus`.

The sweep for #43, corrected at UAT, measured these ids:

| Id | Key today | Wanted key | Note |
|---|---|---|---|
| `gemini-2.5-flash` | `gemini/flash@2.5` | `gemini/flash@2.5` | correct control (24 records) |
| `step-3.5-flash` | `step/flash@3.5` | `step/flash@3.5` | correct control (5 records) |
| `qwen3-coder` | `qwen/coder@3` | `qwen/coder@3` | correct base (24 records) |
| `qwen3-coder-flash` | `qwen/coder@3` | `qwen/coder-flash@3` | flash DROPPED, collides with the base |
| `glm-4.1v-thinking-flash` | `glm/v@4.1{flash}` | `glm/flash@4.1{vision}` | flash demoted to a modifier |
| `glm-4.6v-flash` | `glm/v@4.6{flash}` | `glm/flash@4.6{vision}` | flash demoted to a modifier |

Two distinct failures:

- `qwen3-coder-flash` DROPS the `flash` specializer and collapses onto
  `qwen/coder@3`, the same key as the `qwen3-coder` base. A reader cannot tell
  the two apart from the key. The repair keeps `flash` as part of a COMPOUND
  variant: `coder-flash`. A specializer glued to an existing variant extends
  it, it does not replace or vanish.
- On the GLM vision line, `flash` becomes an identity MODIFIER while the
  vision `v` wrongly takes the variant slot. The vision half is repaired under
  the vision-suffix fix; this fix is responsible for `flash` reaching the
  variant slot.

The two conforming controls (`gemini`, `step`) show the target shape is
already met where the id is well formed, so the defect is the drop and the
demotion, not `flash` itself.

## Scope

Changes:
- `flash` and `flashx` reach the VARIANT slot for every lab, never dropped
  and never demoted to a modifier.
- A `flash` specializer glued to an existing variant forms a COMPOUND variant
  (`coder-flash`), so it does not collide with the bare variant base.
- Price the change by regenerating and diffing the entity key set. The blast
  radius is cross-lab (gemini, step, qwen, glm at least) and is knowable only
  by regen-and-diff, not by grep.

Non-changes:
- No change to `gemini`/`step`, which already key `flash` as the variant. The
  controls must stay green.
- The vision `v` is named here but repaired under the vision-suffix fix.

## Acceptance

- `qwen3-coder-flash` keys `qwen/coder-flash@3` and no longer collides with
  `qwen3-coder` on `qwen/coder@3`.
- The GLM vision-flash cases key `flash` in the variant slot
  (`glm/flash@4.1{vision}`, `glm/flash@4.6{vision}`).
- `gemini-2.5-flash` and `step-3.5-flash` stay green, so the repair did not
  move the path that already worked.
- The corpus flash cases (class 8) flip from `defect` to `conforming` in the
  same PR that lands the fix.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
   "gemini-2.5-flash"     "qwen3-coder-flash"      "glm-4.1v-thinking-flash"
         |                        |                          |
   flash -> variant        flash DROPPED              flash -> modifier,
   (right)                 collides with base         'v' -> variant
         |                        |                          |
         v                        v                          v
   gemini/flash@2.5        qwen/coder@3  (wrong)      glm/v@4.1{flash} (wrong)

   wanted: flash is always the variant; glued to a variant it compounds
         |                        |                          |
         v                        v                          v
   (unchanged)             qwen/coder-flash@3         glm/flash@4.1{vision}
```

## Blocked by & Blocks

Blocked by: nothing.
Blocks: nothing measured. Shares the GLM vision-flash ids with the
vision-suffix fix, which carries the `{vision}` modifier half.

## Related

#43, which measured this, corrected at UAT. The sweep report is
`docs/research/parser-conformance-sweep.md`; the witnesses are the class 8
cases in `testdata/parse/parser_conformance_corpus.json`.
