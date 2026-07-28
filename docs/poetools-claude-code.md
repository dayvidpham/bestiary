# `poetools/claude-code`: what it is, and the harness-vs-model design question

A ledger note (R7, the v0.2.8 registry-ingest/creator-dimension proposal §9):
investigate what
`poetools/claude-code` — a real row in the ingested models.dev catalog — actually
routes to, and use it as the concrete case for the design question `harness.go`
already poses but leaves deliberately unwired: **how should a harness identifier
relate to bestiary's model entities, if at all?**

This is a design record, not an implementation. No code wiring lands with it.

## 1. What the catalog row actually is

`models_static_gen.go` carries this row (Provider `poe`, i.e. `ProviderPoe`):

```go
ID:         "poetools/claude-code",
Provider:   ProviderPoe,
DisplayName: "claude-code",
Family:     "claude",
Modifier:   []string{"code"},
Date:       "2025-11-27",
```

The upstream models.dev description for it: *"Claude model for careful reasoning,
writing, coding, and tool use"* — generic Claude marketing copy, giving no signal
that this ID names anything other than a bare model.

Live research (Poe's own creator documentation, current as of this writing)
shows it is not a bare model at all:

- **"Poe Tools" (`poetools`)** is Poe's own first-party creator account. It
  publishes several official bots, one of which is named **`Claude-Code`**:
  *"A powerful assistant that can read, write, and analyze files across many
  formats" that "can also delegate to other Poe bots to handle complex,
  multi-step tasks. Built on the Claude Agent SDK from Anthropic."*
- Two other Poe Tools bots (`Script-Bot-Creator`, `Canvas-Creator`) are
  themselves described as **"powered by Claude Code"** — i.e. they are built
  *on top of* this bot, not on top of a bare Claude model.
- Separately, and confusingly reusing the same name: Anthropic's own **Claude
  Code CLI** (the tool this repository's contributors run) can be pointed at
  Poe's infrastructure by setting `ANTHROPIC_BASE_URL=https://api.poe.com`. In
  that mode Claude Code speaks Poe's Anthropic-Messages-API-compatible
  endpoint, and the model actually served is selected by Claude Code's own
  model aliases (`Sonnet`/`Opus`/`Haiku`), which Poe maps to real Claude model
  versions and bills against the user's Poe subscription.

So there are **two distinct "Claude Code" things** live on Poe right now, and
the models.dev catalog row conflates both into one model-shaped entry:

1. `poetools`'s **Claude-Code bot** — an agent product (file I/O, delegation to
   other bots), built on the Claude Agent SDK. This is what `poetools/claude-code`
   actually names.
2. Anthropic's **Claude Code CLI**, optionally routed through Poe as a
   transport, itself selecting an underlying Claude model by alias.

Neither is "a Claude model" in the sense every other row in the catalog is.

## 2. Why this matters to bestiary

bestiary's entity model assumes every catalog row names a **weights identity**
— `(Family, Variant, Version, Date, Modifier)` decomposing to one artifact with
one architecture. `poetools/claude-code` decomposes cleanly through the parser
(`Family: claude`, `Modifier: [code]`) and sits in the registry as if it were a
Claude variant named "code" — exactly the shape a genuine model-family member
would have (compare `claude/opus@4.5`, `claude/haiku@4.5`). Nothing in the
current pipeline can tell the difference between "a real distinct Claude
variant" and "an agent product wrapping whichever Claude model the operator
configured," because the ID and the decomposition look identical either way.

This is not a parsing bug to fix — the decomposition is doing exactly what it's
designed to do (turn an ID into a canonical tuple). It's a data-quality ceiling:
**the source data itself does not distinguish a harness from a model**, and
bestiary ingests only what upstream publishes (per the "three-artifact
ingestion" design in `CLAUDE.md`'s v0.2.5 section — no independent judgment is
applied at ingest time beyond the parse/curation pipeline). A curator reviewing
`parse_failures.json` or the unlinked report would have no signal, from the row
alone, to flag it as anything other than a normal Claude entity.

## 3. `harness.go`: what exists, and why it stays unwired

`harness.go` defines `Harness` (`HarnessClaudeCode`, `HarnessGeminiCLI`,
`HarnessCodex`, `HarnessOpenCode`, `HarnessCursor`, `HarnessAntigravity`) — a
closed set of coding-tool/dev-environment identifiers, with the same
`String`/`MarshalText`/`UnmarshalText`/`IsKnown` shape every other bestiary
enum uses. It has never been attached to `Entity`, `ModelInfo`, or any store
table. That is deliberate, not an oversight: a harness is a **consumer** of a
model (the CLI/IDE/agent driving requests to it), not a fact about the model's
weights or serving. Attaching it to the entity model would conflate "what this
artifact is" with "what tool happened to call it," which is the same axis
confusion `poetools/claude-code` demonstrates from the opposite direction —
here a harness-shaped *product* got ingested as if it were a model-shaped
*artifact*.

## 4. The design question, stated precisely

Given a harness exists as a typed identifier and a real catalog row shows a
harness product masquerading as a model row, there are three candidate
relationships between `Harness` and the entity model — recorded here for a
future epoch to pick up, not decided by this note:

1. **No relationship (status quo).** `Harness` stays a free-standing
   identifier for describing *bestiary's own* callers (e.g. a future
   `bestiary show --harness claude-code` filtering CLI telemetry, or a
   provenance field on how a *query* was made) and never touches `Entity`.
   Rows like `poetools/claude-code` remain ordinary — if wrong — catalog
   entries; the fix, if any, is curation (an alias/override that reclassifies
   or excludes the row), not a new type relationship.
2. **Harness as a modifier-class disambiguator at ingest.** A curated
   override table (the `idFamilyOverrideEntry`/`modelsdev_aliases.json`
   precedent) recognizes known harness-product IDs from known
   harness-hosting providers (Poe's `poetools/*`, and any equivalent on other
   providers) and either excludes them from the entity registry or tags them
   with a distinct, non-identity fact (e.g. an instance-level "delivered via
   harness X" attribute) rather than letting them decompose into a phantom
   model variant. This keeps `Harness` out of `EntityRef` entirely — it would
   be a `ModelInfo`/`ProviderInstance`-level fact at most, matching how
   `Host`/`Region` already sit off to the side of identity.
3. **Harness as its own dimension, unrelated to any specific entity.**
   Since a harness (Claude Code, Cursor, etc.) can call *any* number of
   models across *any* number of providers, a `Harness` fact is arguably
   never about one entity at all — it belongs on the request/session level
   bestiary doesn't model today (bestiary is a catalog, not a request log).
   Under this reading `poetools/claude-code`'s only bestiary-relevant
   property is that it is *not* a model, and the correct fix is exclusion,
   full stop — `Harness` answers a question bestiary isn't in the business
   of answering yet.

No option is chosen here. The recommendation, if this is picked up: option 2's
narrow slice (curated exclusion/reclassification of known harness-product
rows) is the cheapest fix for the concrete data-quality problem
(`poetools/claude-code` sitting in the registry as a phantom Claude variant)
without committing to a full harness dimension; options 1 and 3 are both
compatible with deferring the general question indefinitely.
