---
title: "[P3] Decide where a cross-lab distill belongs in the keyspace"
labels: bug
---

## Problem

The sweep for #43 verified the destination of the upstream family
`deepseek-thinking`. The label itself is correctly discarded - no key
contains it.

This class counts a DIFFERENT population from the sweep census: every row
carrying the label, not the seed-token matches. So it is printed in BOTH
units. The label carries 158 provider ROWS, which are 63 distinct served
ids by the sweep's counting rule (one distinct raw id per catalog view,
compared case-sensitively), plus 6 lab ids. Both columns are pinned in
`TestParserConformance_TokenCensus`, and the table is total in both units.

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

Most of the wrong keys here are the `v` version-token defect, which has its
own issue. One group is NOT, and it needs a ruling.

The R1 distills key by SIZE alone. `deepseek-r1-distill-qwen-32b` keys
`deepseek#32b`. The key states neither the R1 line that produced it nor the
qwen base it was distilled from. A reader cannot tell it from any other
32-billion deepseek row. The four sized keys above hold 7 distinct ids and
10 provider rows between them.

The repository already carries a lineage ledger for derivation edges. A
distill is exactly such an edge. What is undecided is whether the KEY must
also carry the fact, and if so, which axis carries it.

## Scope

The decision, stated as questions for the user:

1. Does a distill get its own entity, or is it an instance of the base it
   was distilled from?
2. If it gets its own entity, which axis names the distill - a variant, a
   modifier, or the lineage edge alone?
3. Does the R1 line marker belong in the key, and if so, is it a version or
   a variant?

Changes, after the ruling:
- Apply the ruling to the distill rows.
- Record the derivation edge in the lineage ledger either way, because the
  edge is a fact whatever the key says.
- Price the change by regenerating and diffing the entity key set.

Non-changes:
- No ruling is invented by the sweep. The conformance corpus records the
  distill case with the `EXPECTED_TBD` marker.
- The `deepseek-thinking` label itself needs no change. Discarding it is
  correct.

## Acceptance

- The ruling is written down with its reason, next to the lineage ledger.
- Every R1 distill row reaches the destination the ruling names.
- The lineage edge from each distill to its base is present and is
  asserted by a test.
- The `EXPECTED_TBD` distill case in the GH#43 conformance corpus is
  replaced by a real expectation.

## Diagram

```
  upstream family "deepseek-thinking"  (158 served rows)
        |
        +--> 106 rows -> deepseek/pro      <-- the `v` version defect, own issue
        +-->  40 rows -> deepseek          <-- correct: alias endpoints, no version
        +-->  10 rows -> deepseek#<size>   <-- THIS issue (7 distinct ids)
                           |
                           v
              "deepseek-r1-distill-qwen-32b"
                   R1 line marker: dropped
                   qwen base:      dropped
                   size:           kept
                           |
                           v
                   ruling needed: which axis carries the distill?
```

## Blocked by & Blocks

Blocked by: the `v` version-token repair, which removes 106 of the 158 rows
from the question and leaves only the rows this ruling is about.
Blocks: nothing measured.

## Related

#43, which measured this. The sweep report is
`docs/research/parser-conformance-sweep.md`.
