---
title: "[P1] The leading `v` version token is misplaced or destroyed"
labels: bug
---

## Problem

The parser does not decode a leading `v` on a version token. The token then
goes to the wrong slot, or it disappears. The sweep for #43 measured three
shapes of one cause.

A **record**, here and in the sweep report, is one DISTINCT raw id string
within one catalog view, compared case-sensitively, among the rows the
seed-token census matched. The counting rule is stated once in
`docs/research/gh43-parser-conformance-sweep.md`, and every count below is
pinned in `TestGH43Sweep_TokenCensus`.

Shape A - the version goes to the VARIANT slot. Eight keys, 31 records:

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

The correct sibling `deepseek@3.2` holds 1 record. The same product is
therefore on two keys.

Shape B - a BARE `v<major>` token is destroyed. `deepseek/deepseek-v4-pro`
keys `deepseek/pro`, which states no version. That key holds 39 records.
`deepseek-ai/deepseek-v4-flash` keys `deepseek/flash`, 58 records. Together
this is 97 records that state no version although their ids state one.

Shape C - the dash-glued dot-lost spelling misreads the version.
`deepseek-v3-2` keys `deepseek@2`. The parser takes only the last segment.

The cause is the `v` character, not the version format. The control proves
it: `deepseek-3.2` keys `deepseek@3.2` correctly, and `deepseek-4-flash`
keys `deepseek/flash@4` correctly. Remove the `v` and the same token
reaches the `@version` slot.

This defect is also what makes #43's class 5 look like a labelling problem.
The upstream family `deepseek-thinking` carries 158 provider ROWS, which
are 63 distinct served ids by the counting rule above. 106 of those rows,
35 of those ids, land on `deepseek/pro` - the version-less key of shape B.

## Scope

Changes:
- Decode a leading `v` on a version token in the production decomposition,
  for all three shapes: `v3.2`, `v4`, and `v3-2`.
- Keep the `v` out of the key. `deepseek-v3.2` must key `deepseek@3.2`.
- Price the change by regenerating and diffing the entity key set. Do NOT
  price it by grep.
- Record every retired key in a migration corpus, with the live key each
  instance re-homes onto.

Non-changes:
- No curated data entry per model id. The repair is mechanical, or it is
  not the repair.
- No change to the dot-lost repair that already exists for spellings
  without a `v`.

## Acceptance

- `deepseek-v3.2`, `deepseek-v3-2` and `deepseek-3.2` all key
  `deepseek@3.2`.
- `deepseek-v4-pro` keys `deepseek/pro@4`; `deepseek-v4-flash` keys
  `deepseek/flash@4`.
- The eight `deepseek/v3.x` keys no longer exist. A migration corpus names
  each retired key and its successor, and asserts every successor is live.
- The GH#43 conformance corpus is updated: the shape-A, shape-B and shape-C
  cases flip from `defect` to `conforming`, and their controls stay green.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
  raw id "deepseek-v3.2-exp"
        |
        v
  token split -> [deepseek] [v3.2] [exp]
        |
        +-- today --> family=deepseek  variant="v3.2-exp"   ==> deepseek/v3.2-exp
        |
        +-- wanted -> strip leading "v" from a version-shaped token
                         |
                         v
                  family=deepseek  version=3.2  modifier=[exp]
                                                  ==> deepseek@3.2{exp}
```

## Blocked by & Blocks

Blocked by: nothing. The evidence is measured and committed.
Blocks: the class 4 modifier ruling, which cannot separate
`deepseek/deepseek-r1-turbo` from `deepseek/deepseek-v3-turbo` until each
one carries its own version again.

## Related

#43, which measured this. The sweep report is
`docs/research/gh43-parser-conformance-sweep.md`. The witnesses are in
`testdata/parse/gh43_conformance_corpus.json`.
