---
title: "[P2] The version-glued vision 'v' suffix is read as a variant"
labels: bug
---

## Problem

Zhipu names its vision models with a `v` glued to the version: `glm-4.5v`,
`glm-4.6v`, `glm-5v`, `glm-4.1v`. That `v` is the VISION modality. The parser
reads it as the VARIANT, so the whole GLM vision line keys under a `v`
variant and the vision fact is lost from the key.

A **record**, here and in the sweep report, is one DISTINCT raw id string
within one catalog view, compared case-sensitively, among the rows the
seed-token census matched. The counting rule is stated once in
`docs/research/parser-conformance-sweep.md`, and every count below is
pinned in `TestParserConformance_TokenCensus`.

The sweep for #43, corrected at UAT, measured these ids:

| Id | Key today | Wanted key |
|---|---|---|
| `glm-4.5v` | `glm/v@4.5` | `glm@4.5{vision}` |
| `glm-4.6v` | `glm/v@4.6` | `glm@4.6{vision}` |
| `glm-5v-turbo` | `glm/v@5` | `glm@5{vision}` |
| `glm-4.1v-thinking-flash` | `glm/v@4.1{flash}` | `glm/flash@4.1{vision}` |
| `glm-4.1v-thinking-flashx` | `glm/v@4.1` | `glm/flashx@4.1{vision}` |
| `glm-4.6v-flash` | `glm/v@4.6{flash}` | `glm/flash@4.6{vision}` |

The vision line splits across generations under the wrong `v` variant:
`glm/v@4.6` holds 9 records, `glm/v@4.5` holds 7, and `glm/v@5` holds 6. Two
further defects ride on the same ids:

- The `flash`/`flashx` token is a distinct-weight VARIANT (see the
  flash-variant-uniformity fix). Where it appears, `v` takes the variant slot
  and `flash` is demoted to a modifier, the exact inversion of both rulings.
- The `thinking` token is DROPPED (for example `glm-4.1v-thinking-flash`).

The `turbo` token on `glm-5v-turbo` is already OFF the key (dropped, not kept
as a modifier), so the vision `v` is the sole remaining defect there.

## Scope

Changes:
- Read a version-glued `v` on a GLM version as the `{vision}` MODIFIER, not
  the variant. The rule is curated and explicit for the GLM vision line, not
  a heuristic on any trailing letter.
- Where a real variant is also present (`flash`, `flashx`), keep the variant
  in the variant slot and vision in the modifier slot.
- Price the change by regenerating and diffing the entity key set.

Non-changes:
- No blanket rule that any trailing `v` is vision. That would eat real model
  content.
- The `thinking` drop and the `flash` variant are named here but repaired
  under their own work (the flash-variant-uniformity fix carries `flash`).

## Acceptance

- `glm-4.5v` keys `glm@4.5{vision}`, `glm-4.6v` keys `glm@4.6{vision}`, and
  `glm-5v-turbo` keys `glm@5{vision}`.
- No `glm/v@...` key remains for a vision id.
- The corpus vision cases (class 7) flip from `defect` to `conforming` in the
  same PR that lands the fix.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
   "glm-4.5v"          "glm-4.1v-thinking-flash"
        |                        |
   'v' -> variant           'v' -> variant, flash -> modifier,
        |                    thinking dropped
        v                        v
   glm/v@4.5  (wrong)       glm/v@4.1{flash}  (wrong)

   wanted: 'v' -> {vision} modifier; a real variant stays the variant
        |                        |
        v                        v
   glm@4.5{vision}          glm/flash@4.1{vision}
```

## Blocked by & Blocks

Blocked by: nothing.
Blocks: nothing measured. Shares the GLM vision-flash ids with the
flash-variant-uniformity fix, which carries the `flash` variant half.

## Related

#43, which measured this, corrected at UAT. The sweep report is
`docs/research/parser-conformance-sweep.md`; the witnesses are the class 7
cases in `testdata/parse/parser_conformance_corpus.json`.
