---
title: "[P2] Decide where `turbo` on deepseek and `code` on claude belong"
labels: bug
---

## Problem

Two tokens are kept as IDENTITY modifiers. Each one makes a key that names
something the lab does not sell as a distinct artifact. The sweep for #43
measured them and did NOT rule on them, because the destination is a
curation decision.

A **record**, here and in the sweep report, is one DISTINCT raw id string
within one catalog view, compared case-sensitively, among the rows the
seed-token census matched. The counting rule is stated once in
`docs/research/parser-conformance-sweep.md`, and every count below is
pinned in `TestParserConformance_TokenCensus`.

| Key | Records | Ids |
|---|---|---|
| `deepseek{turbo}` | 2 | `deepseek/deepseek-r1-turbo`, `deepseek/deepseek-v3-turbo` |
| `claude{code}` | 1 | `poetools/claude-code` |

`deepseek{turbo}` is also a COLLISION. Two DIFFERENT models sit on it. R1
and V3 are not the same artifact, but each one lost its line marker as
well as gaining `{turbo}`, so the two keys fell together.

`claude{code}` reads a PRODUCT name (Claude Code) as a property of a model.
The repository already treats `turbo` as attribute-class for kimi and
minimax, with archived lab evidence in each case. No such evidence exists
yet for deepseek, and none exists for `code` on claude.

## Scope

The decision, stated as questions for the user:

1. Does DeepSeek publish separate `-turbo` weights, or is `turbo` a serving
   speed tier of the same artifact? If it is a tier, `turbo` becomes
   attribute-class for deepseek, following the kimi and minimax precedent.
2. Is `poetools/claude-code` a distinct model, a product namespace on an
   ordinary claude model, or a row that should not mint an entity at all?

Changes, after the ruling:
- Add the ruling to the modifier-class table, with archived lab evidence
  for the reason, as the existing kimi and minimax entries do.
- Price the change by regenerating and diffing the entity key set.

Non-changes:
- No ruling is invented by the sweep. Until the user rules, the conformance
  corpus records these three cases with the `EXPECTED_TBD` marker.

## Acceptance

- The modifier-class table carries a reasoned entry for each token, with an
  archived source URL.
- The `deepseek{turbo}` collision is gone: the R1 row and the V3 row reach
  DIFFERENT keys, each stating its own version.
- The `claude{code}` case reaches the destination the ruling names.
- The three `EXPECTED_TBD` cases in the GH#43 conformance corpus are
  replaced by real expectations.

## Diagram

```
  deepseek/deepseek-r1-turbo ---+
                                +--> deepseek{turbo}   <-- ONE key, TWO models
  deepseek/deepseek-v3-turbo ---+

  the collision has two causes, and BOTH must be repaired:
     1. the version repair restores "r1" and "v3" to the key
     2. this ruling decides whether "turbo" stays in the key at all

  poetools/claude-code -------> claude{code}
     ruling: distinct model? product namespace? or no entity?
```

## Blocked by & Blocks

Blocked by: the `v` version-token repair. Without it the two deepseek rows
stay collided whatever this ruling says.
Blocks: nothing measured.

## Related

#43, which measured this. The sweep report is
`docs/research/parser-conformance-sweep.md`. Prior art for the
evidence standard: the kimi and minimax `turbo` demotions in
`parse/data/modifier_class.json`.
