# ufn81 — `turbo` / `fast` per-family evidence report

**Status: REPORT ONLY.** No `parse/data/modifier_class.json` change is proposed *for
landing* in this slice. Every per-family disposition below is a **proposal that is
explicitly user-gated at Impl-UAT**; nothing here is self-executing.

**Scope.** Every family in the baked catalog carrying a `turbo` or `fast` token, the
evidence available from `EntityMetadata.Links[]` (the lab model pages models.dev
records) plus the catalog's own id census, and a proposed disposition per family.

**Method.** For each `ModelInfo` in `StaticModels()` whose decomposed `Modifier` list
carries `turbo`/`fast` (or whose raw id contains the token), the report records: the raw
ids, the *identity* modifiers `EntityModifiers` keeps, the resulting entity keys, and
every `EntityMetadata.Links[]` URL for entities of that family. `LinkWeights` rows are
the load-bearing evidence: a link to a **distinct weights repo** for the turbo/fast
spelling is evidence of a distinct artifact; a turbo/fast id whose metadata points at
the **same** weights repo as the plain spelling is evidence of a serving tier.

**Current classification at HEAD** (`modifier_class.json`): `turbo` and `fast` are
**global IDENTITY** (the fail-safe: never silently collapse two artifacts), with
per-family ATTRIBUTE demotions for `claude` (`fast`), `glm` (`turbo`, `fast`), `kimi`
(`fast`), `deepseek` (`fast`), `minimax` (`fast`).

---

## The decisive piece of link evidence

Only one family gives a **direct, same-repo** reading, and it is the strongest single
datum in this report:

```
LINK kimi/k@2       : https://huggingface.co/moonshotai/Kimi-K2-Thinking  [moonshotai/kimi-k2-thinking]
LINK kimi/k@2{turbo}: https://huggingface.co/moonshotai/Kimi-K2-Thinking  [moonshotai/kimi-k2-thinking-turbo]
```

Two metadata rows — `kimi-k2-thinking` and `kimi-k2-thinking-**turbo**` — point at the
**identical** weights repository. Moonshot publishes one set of weights and two serving
SKUs. On the evidence, `turbo` is a **serving tier** for `kimi`, not an artifact.

No other family in the catalog carries a `LinkWeights` row for a turbo/fast spelling at
all: the metadata join simply has no row for `gpt-4-turbo`, `Llama-3.3-70B-Instruct-Turbo`,
`whisper-large-v3-turbo`, `veo-3-fast`, etc. **Absence of a link is not evidence of
sameness** — it is missing evidence, and the dispositions below say so plainly.

---

## Per-family findings and proposed dispositions

| Family | Rows | Identity today | Link evidence | Proposed disposition (USER-GATED) |
|---|---|---|---|---|
| `gpt` | 46 | `turbo` IDENTITY (`gpt@3.5{turbo}`, `gpt@4{turbo}`, `gpt@3.5{turbo,instruct}`, `gpt@4{vision,turbo}`); `gpt/oss#120b{fast}` | No turbo/fast weights link | **KEEP IDENTITY.** `gpt-4-turbo` and `gpt-4` are separately documented OpenAI models with different context windows, cutoffs and pricing — the archetypal *distinct artifact* reading, and the case the global fail-safe exists for. The lone `openai/gpt-oss-120b-fast` is a different question (a provider's speed SKU of an open-weights model) → **insufficient evidence**, see below. |
| `kimi` | 16 | `turbo` IDENTITY, `fast` already ATTRIBUTE | **Same weights repo** for `kimi-k2-thinking` vs `-turbo` | **DEMOTE `turbo` → ATTRIBUTE.** The only family with direct same-repo evidence. Would fold `kimi/k@2{turbo}` and `kimi/k@2.6{turbo}` into their plain entities. Note the interaction: the metadata rows would then compete for one entity — check `modelsdev_aliases.json` before landing. |
| `glm` | 37 | both already ATTRIBUTE | Weights links exist for plain `glm@4.5/4.6/5.1/5.2`; none for a turbo/fast spelling | **KEEP AS IS (attribute).** The existing demotion is consistent with the evidence: Z.ai's turbo/fast spellings appear only as third-party host SKUs (`z-ai/`, `zai-org/`, fireworks routers), never as their own weights repo. |
| `claude` | 6 | `fast` already ATTRIBUTE | No `-fast` link; the `-fast` ids exist only under aggregators | **KEEP AS IS (attribute).** `claude-opus-4.x-fast` appears exclusively as an aggregator SKU of a model Anthropic itself does not publish under a `-fast` name. |
| `deepseek` | 3 | `fast` ATTRIBUTE, `turbo` IDENTITY | Plain weights links only | **INSUFFICIENT EVIDENCE for `turbo`.** `deepseek/deepseek-r1-turbo` and `-v3-turbo` are aggregator ids; nothing in the catalog says whether they re-quantize. Symmetry with the landed `fast` demotion *suggests* ATTRIBUTE, but suggestion is not evidence — leave IDENTITY pending a lab-page check. |
| `minimax` | 2 | `fast` ATTRIBUTE, `turbo` IDENTITY | `minimax/m@2.7` links to `MiniMax-M2.7-highspeed` weights | **INSUFFICIENT EVIDENCE for `turbo`.** MiniMax *does* publish a distinct `-highspeed` repo, which is evidence that MiniMax's speed variants can be genuinely different weights. That argues **against** demoting `turbo` here, and is a reason to re-examine the already-landed `fast` demotion. Flagged, not proposed. |
| `qwen` | 23 | `fast` IDENTITY (`qwen@3.5#397b{fast}`, …); `turbo` mostly absorbed into the family/variant (`qwen/turbo`) | Plain weights links only (`Qwen3.5-397B-A17B`, …) | **KEEP IDENTITY, but flag a parse residual.** `qwen-turbo` decomposes to variant `turbo` (`qwen/turbo`) while `Qwen3.5-397B-A17B-fast` keeps `{fast}` — two different treatments of the same commercial idea. The disposition question here is *not* really identity-vs-attribute; it is that `qwen@3.5#397b{fast}` and `qwen@3.5#397b-a17b{fast}` are the same model under two size spellings. Route to the size-axis work, not to a modifier demotion. |
| `grok` | 49 | `fast` IDENTITY (`grok@4{fast}`, `grok@4.1{fast}`, `grok@4{reasoning,fast}`, …) | No links | **KEEP IDENTITY.** xAI documents `grok-4-fast` as its own model with its own pricing and context window, and the catalog carries `grok-4-fast-reasoning` / `-non-reasoning` as separate rows. Collapsing would merge documented, separately-priced models. |
| `llama` | 6 | `turbo`/`fast` IDENTITY (`llama@3.3#70b{turbo,instruct}` vs `{fast,instruct}` vs plain `{instruct}`) | Plain `Llama-3.3-70B-Instruct` weights link only | **INSUFFICIENT EVIDENCE — but the highest-value candidate.** Together/Cloudflare `-Turbo` and `-fp8-fast` are near-certainly **quantization SKUs** of Meta's one release (`fp8` is in the id!). The right fix is probably **not** an attribute demotion but routing the quant tag to the existing `Quantization` axis. Explicitly out of scope here; recommend a dedicated ruling. |
| `veo`, `imagen`, `seed`, `runway`, `ideogram`, `elevenlabs`, `whisper`, `morph`, `flux` | 1–5 each | IDENTITY | No links | **KEEP IDENTITY (media/audio models).** Google documents `veo-3-fast` and `imagen-4-fast` as separate, separately-priced models, and `whisper-large-v3-turbo` is a genuinely distinct distilled checkpoint OpenAI released. This group is the clearest *keep*. |
| `ernie`, `baichuan4`, `qianfan`, `hunyuan`, `ling`, `o` (cohere rerank), `gemma` | 1–3 each | IDENTITY | No links | **INSUFFICIENT EVIDENCE.** Long tail; each would need its own lab-page check for a one-model payoff. Recommend deferring as a batch. |
| `fast` (the family literally named `fast`) | 1 | `fast{fast}` | — | **PARSE BUG, not a classification question.** A bare id `fast` decomposes to family `fast` + modifier `fast`. Route to the parse-residual backlog. |

---

## Summary of proposals (each requires its own Impl-UAT ruling)

1. **DEMOTE** `kimi.turbo` → attribute — the only family with direct same-weights evidence.
2. **KEEP IDENTITY** for `gpt`, `grok`, and the media group (`veo`, `imagen`, `seed`,
   `runway`, `ideogram`, `elevenlabs`, `whisper`, `morph`) — labs document these as
   distinct, separately-priced models.
3. **KEEP AS IS** for the already-demoted `glm` and `claude` — consistent with evidence.
4. **INSUFFICIENT EVIDENCE** for `deepseek.turbo`, `minimax.turbo`, the long tail, and
   the whole `llama` group; the `minimax` `-highspeed` weights repo is a live argument
   *against* speed-tier demotion for that lab.
5. **NOT A CLASSIFICATION QUESTION** — three findings that should leave this axis
   entirely: the `llama` `-Turbo`/`fp8-fast` quantization-SKU reading, the `qwen`
   `397b` vs `397b-a17b` size-spelling split, and the `fast{fast}` parse residual.

## Method caveats (read before acting on any row)

- `EntityMetadata.Links[]` is **sparse**: 247 baked metadata rows against 5,654 catalog
  models, so most turbo/fast spellings have no link at all. Every "no links" cell above
  means *no evidence*, never *evidence of sameness*.
- The links are models.dev's record of a lab page, not a bestiary measurement. Reading
  them is a **claim** (`SourceURL`-level provenance), not an observation.
- Fixing a modifier class is **not** free: it re-keys entities, which moves store keys,
  lineage edges, `Entity__` constants, and the census pins. Any accepted demotion needs
  the same before/after pinning discipline the maverick re-key used.
- Redundant-modifier suppression (GH#7, this slice) is the **non-re-keying alternative**
  for a subset of these: if a token is judged merely redundant rather than
  misclassified, a suppression seed entry changes the *preferred naming* while leaving
  the key — and every downstream consumer — untouched.
