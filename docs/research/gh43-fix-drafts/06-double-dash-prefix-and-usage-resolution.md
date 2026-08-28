---
title: "[P1] A doubled vendor dash destroys the version, and usage-side spellings do not resolve"
labels: bug
---

## Problem

#43 cited a set of tier-before-version claude spellings that the usage join
measured at 2.5% of all tokens. The sweep re-measured them. The result
SPLITS the report in two.

Part 1 - the cited spellings are NOT a parser defect. Every one decomposes
correctly:

| Cited spelling | Key produced |
|---|---|
| `anthropic/claude-4.6-sonnet` | `claude/sonnet@4.6` |
| `claude-4.8-opus` | `claude/opus@4.8` |
| `claude-4.7-opus` | `claude/opus@4.7` |
| `claude-5-fable` | `claude/fable@5` |
| `claude-4.6-opus` | `claude/opus@4.6` |
| `openai/gpt-5-mini-2025-08-07` | `gpt/mini@5` |
| `moonshotai/kimi-k2.5-0127` | `kimi/k@2.5` |

None of the seven is a catalog id. `Resolve` refuses each one with "model
not found", because `Resolve` matches catalog ids and no catalog row spells
the model this way. So the 2.5% gap is a RESOLUTION gap. The parser already
knows the destination; the lookup will not use it.

Part 2 - a REAL defect is in the catalog, and the cited spellings pointed
at it. A doubled dash after a vendor prefix destroys the version:

| Id | Key | Correct key |
|---|---|---|
| `anthropic--claude-4.6-sonnet` | `claude/sonnet` | `claude/sonnet@4.6` |
| `anthropic--claude-4.8-opus` | `claude/opus` | `claude/opus@4.8` |

The control isolates the cause: the SINGLE-dash spelling
`anthropic-claude-4.6-sonnet` keys `claude/sonnet@4.6` correctly. Only the
doubled dash fails.

Two figures follow, and they are different sizes on purpose. A **record**
is one distinct raw id string within one catalog view, compared
case-sensitively, among the rows the sweep's seed-token census matched.

| Figure | `claude/opus` | `claude/sonnet` |
|---|---|---|
| Records on the version-less key | 19 | 13 |
| Of those, records whose raw id carries the doubled dash | 6 | 6 |

The doubled dash's blast radius is therefore 6 and 6, NOT the key totals.
The other records lose the version for other reasons and this issue does
not repair them: `anthropic/claude-opus-latest`, the
`anthropic/claude-opus-4.6:thinking*` suffixed spellings, and the
`duo-chat-opus-*` ids. Both rows of the table are pinned in
`TestGH43Sweep_TokenCensus`.

## Scope

Changes:
- Repair the doubled-dash vendor prefix so it strips like the single-dash
  one. The version must survive.
- Give the lookup a decomposition fallback: when an input is not a catalog
  id, decompose it and look the resulting entity key up, instead of
  refusing. The fallback must be explicit and must say which path answered,
  so a caller can tell an exact id match from a decomposed match.
- Price the key change by regenerating and diffing the entity key set.

Non-changes:
- No curated alias per usage spelling. The decomposition already works; an
  alias table would hide that fact and would grow without end.
- No change to exact-id lookup precedence. An exact id must still win.

## Acceptance

- `anthropic--claude-4.6-sonnet` keys `claude/sonnet@4.6`, and reaches the
  SAME entity as `anthropic-claude-4.6-sonnet`.
- The 6 doubled-dash records on `claude/opus` and the 6 on `claude/sonnet`
  all carry their version. The remaining records on those two keys are NOT
  in scope, and the regen diff says so by naming what moved.
- The single-dash control stays green.
- Each of the seven cited usage spellings resolves to its entity, and the
  result states that it came from the decomposition fallback.
- A spelling that decomposes to a key with NO live entity still fails, with
  the typed not-found error. The fallback must not invent an answer.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
  Part 1: the lookup gap

    "claude-4.8-opus"  (a usage-side spelling, not a catalog id)
          |
          +-- parser --> claude/opus@4.8   <-- a LIVE entity key
          |
          +-- Resolve --> "model not found"   <-- refuses anyway
                           |
                           v
              wanted: exact id first, then decompose and look up the key

  Part 2: the doubled dash

    "anthropic-claude-4.6-sonnet"  --> claude/sonnet@4.6   (right)
    "anthropic--claude-4.6-sonnet" --> claude/sonnet       (version destroyed)
                  ^^
             one extra dash is the whole cause
```

## Blocked by & Blocks

Blocked by: nothing. The two parts are independent and can land in either
order.
Blocks: nothing measured.

## Related

#43, which measured this. The sweep report is
`docs/research/gh43-parser-conformance-sweep.md`. The usage measurement
that priced the gap at 2.5% of all tokens is the OpenRouter usage work.
