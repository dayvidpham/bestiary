# a7glm — is `beta` always a release-stage attribute? (evidence memo)

**Status: EVIDENCE MEMO. No code change this epoch.** This memo assembles what the
catalog and the curated alias-edge data actually say, and ends with the **named
Impl-UAT ruling question** it exists to support.

## What the code does today

`beta` is a member of the dedicated **`ReleaseStage` axis** (`stage.go`), introduced in
v0.2.6 as "Design B′". Stage tokens (`preview`, `beta`, `latest`, `original`, …) are
routed **out of both modifier segments** by `EntityModifiers` / `attributeModifiers`
*before* `modifier_class.json`'s identity fail-safe is consulted — so a `-beta` token
can never be promoted into an entity key through the modifier path. It is recorded on
`ModelInfo.Stage`, a per-instance attribute.

## Census: every `beta` spelling in the baked catalog

Two families, ten distinct ids.

**`grok` (xAI) — 8 ids, all stage-only:**

| id | Stage | Entity key |
|---|---|---|
| `grok-4-20-beta-0309-reasoning` | `beta` | `grok@4.20{reasoning}` |
| `grok-4.20-beta-0309-reasoning` | `beta` | `grok@4.20{reasoning}` |
| `xai/grok-4.20-reasoning-beta` | `beta` | `grok@4.20{reasoning}` |
| `grok-4-20-beta-0309-non-reasoning` | `beta` | `grok@4.20{non-reasoning}` |
| `grok-4.20-beta-0309-non-reasoning` | `beta` | `grok@4.20{non-reasoning}` |
| `xai/grok-4.20-non-reasoning-beta` | `beta` | `grok@4.20{non-reasoning}` |
| `grok-4.20-multi-agent-beta-0309` | `beta` | `grok@4.20` |
| `xai/grok-4.20-multi-agent-beta` | `beta` | `grok@4.20` |

The stage treatment is **doing real work**: three id spellings of the *same* model
(hyphen-version, dot-version, and suffix-ordered) converge onto one key precisely
because `beta` is routed off the key. Under any identity treatment these would fragment
into three or more entities per model.

**`interfaze` — 1 id, and the sole counter-example:**

| id | Stage | Entity key |
|---|---|---|
| `interfaze/interfaze-beta` | `beta` | `interfaze/beta` |

Here `beta` is detected as the stage **and** survives into the key as a **variant**. It
is not a modifier-class escape — it is the *sole-variant promotion* path: after the
family `interfaze` is stripped, `beta` is the only token left, so the variant recovery
promotes it. This is the single row in the whole catalog where a `beta` token is part of
an identity.

## The alias-edge data (S2)

The curated claim table (`parse/data/nomen_claims.json`) carries exactly **one** entry,
and it is a `beta` entry:

```json
{ "value": "grok-beta", "scheme": "alias", "status": "admitted",
  "resolves_to": { "family": "grok", "version": "4.20", "modifier": ["reasoning"] },
  "source_url": "https://docs.x.ai/docs/models" }
```

This is the load-bearing datum for the ruling. **xAI itself declares `grok-beta` as an
alias** of a concrete versioned model — i.e. the lab treats the `beta` spelling as
another *name for the same thing*, not as a name of a different thing. The naming layer
already records exactly that: `grok-beta` is an **Admitted** nomen resolving to
`grok@4.20{reasoning}`, whose **Preferred** nomen is the version-bearing canonical key.

## Reading of the evidence

1. **Where a lab declares it, `beta` is an alias, not an artifact.** One lab-attested
   data point (`grok-beta` → `grok@4.20{reasoning}`), zero counter-attestations.
2. **The stage treatment is load-bearing for convergence.** Eight `grok` ids collapse to
   three entities because `beta` is off the key. Reversing that would fragment them.
3. **The one counter-example is not a `beta` ruling at all.** `interfaze/beta` reaches
   the key through *sole-variant promotion*, a different mechanism entirely. It is
   better read as "the catalog has an entity whose only distinguishing token happens to
   be the word beta" — a **degenerate naming**, not a stage-vs-identity disagreement.
4. **Nothing in the catalog demonstrates a lab shipping `X-beta` and `X` as
   simultaneously-served, materially different artifacts.** That is the fact pattern
   that would defeat the blanket rule, and it is absent — but absent from a **10-row**
   census, which is thin.
5. **`beta` differs from `preview` in one respect worth naming:** `preview` spellings
   frequently *co-exist* with their GA counterpart in the catalog (`gpt-4-turbo-preview`
   alongside `gpt-4-turbo`), whereas every `beta` spelling here is the **only** spelling
   of its model. So the "two things served at once" hazard, if it exists anywhere,
   exists for `preview` before it exists for `beta`.

## Cost of each answer

| Ruling | Consequence |
|---|---|
| **Yes — `beta` is always a release-stage attribute** | Zero code change; the current behaviour is ratified and the one-line rule can be documented as a settled invariant. Risk: a future lab that ships `X-beta` and `X` as different artifacts silently collapses them into one entity, and the collapse is *silent* (no unlinked report fires). |
| **No — allow a per-family identity override** | Requires re-opening the stage router to a family-override layer (the `modifier_class.json` `family_overrides` shape). Cost: a second override surface on an axis deliberately built without one, for **zero** currently-known beneficiaries. |
| **Yes, with a guard** | Ratify the blanket rule *and* add a census fence that fails if a `-beta` id and its stage-stripped sibling are ever both present under the same provider — turning the silent-collapse risk into a loud one. Cost: one census test. |

## THE NAMED IMPL-UAT RULING QUESTION

> **Is `beta` ALWAYS a release-stage attribute — never part of a model's identity?**
>
> The evidence says yes for every row in today's catalog: xAI publishes `grok-beta` as
> an *alias* of `grok@4.20{reasoning}` (already recorded as an Admitted nomen), and the
> stage treatment is what lets eight `grok` id spellings converge onto three entities.
> The single row where `beta` reaches a key (`interfaze/beta`) gets there through
> sole-variant promotion, not through the stage axis.
>
> **Sub-question (only if the answer is "yes"):** should the ruling ship with the
> **guard** — a census fence that fails loudly if a `-beta` id and its stage-stripped
> sibling ever appear together under one provider — so that a future lab shipping both
> as distinct artifacts is caught rather than silently collapsed?
>
> **Sub-question (independent of the ruling):** `interfaze/beta` is a degenerate entity
> whose whole identity is the word "beta". Leave it, or curate it as a family-only
> entity (`interfaze`)?
