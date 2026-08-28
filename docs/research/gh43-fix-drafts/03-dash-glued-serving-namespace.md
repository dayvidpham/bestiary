---
title: "[P1] A dash-glued serving namespace is read as model content"
labels: bug
---

## Problem

Some providers glue their namespace to the model id with a dash instead of
a slash. The parser strips a SLASH namespace. It does not strip a DASH one,
so the namespace becomes model content — and it lands in a different slot
depending on what follows it.

The sweep for #43 measured the `z-ai` prefix on Zhipu ids:

| Id | Key | What went wrong |
|---|---|---|
| `z-ai-glm-5-3` | `glm/z` | the prefix became the VARIANT, and version 5.3 was destroyed |
| `z-ai-glm-5v-turbo` | `glm/v@5{z}` | the prefix became an identity MODIFIER |
| `THUDM/GLM-Z1-9B-0414` | `glm/z#9b` | a GENUINE Zhipu Z1 model, now sharing a family and variant with the prefix artifact above |

`glm/z` holds 3 records and `glm/z#9b` holds 1. So a real product line
(GLM-Z1) now shares its variant with an artifact of a provider's naming
habit, and a reader cannot tell them apart from the key.

The control isolates the cause. The SAME model under the slash-separated
namespace, `zai/glm-5v-turbo`, keys `glm/v@5` correctly. Only the dash
spelling fails.

A second split sits beside it and has the same smell: `glm-4-6v-flash`
keys `glm/v@6{flash}`, split off the 5-record `glm/v@4.6{flash}` line
because the dot was lost.

## Scope

Changes:
- Strip a dash-glued serving namespace before decomposition, the way a
  slash-glued one is already stripped. The namespace list is curated and
  explicit, never a heuristic on any leading token.
- Keep the stripped namespace as a per-instance attribute if it names a
  real serving fact. It must never reach the entity key.
- Price the change by regenerating and diffing the entity key set.

Non-changes:
- No change to the genuine GLM-Z1 line beyond giving it a key it does not
  share with an artifact.
- No blanket rule that any leading token before a known family is a
  namespace. That rule would eat real product names.

## Acceptance

- `z-ai-glm-5-3` keys `glm@5.3`.
- `z-ai-glm-5v-turbo` keys the same entity as `zai/glm-5v-turbo`.
- The `{z}` identity modifier no longer appears on any key.
- `THUDM/GLM-Z1-9B-0414` keys a Z1 line that no prefix artifact shares.
- The slash control `zai/glm-5v-turbo` stays green, so the repair did not
  move the path that already worked.
- The regen diff is stated as a measured count of added, retired and moved
  keys.

## Diagram

```
  "zai/glm-5v-turbo"            "z-ai-glm-5v-turbo"        "z-ai-glm-5-3"
        |                              |                        |
   slash namespace              dash namespace           dash namespace
   IS stripped                  is NOT stripped          is NOT stripped
        |                              |                        |
        v                              v                        v
   glm/v@5   (right)           glm/v@5{z}  (wrong)        glm/z  (wrong,
                                                          version destroyed)

   wanted: strip a curated dash-glued namespace before decomposition
        |
        v
   all three ==> the key the slash spelling already produces
```

## Blocked by & Blocks

Blocked by: nothing.
Blocks: nothing measured.

## Related

#43, which measured this. The sweep report is
`docs/research/gh43-parser-conformance-sweep.md`.
