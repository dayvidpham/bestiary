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

## The link evidence: a URL-identity census

**Every repo-identity and repo-distinctness claim in this report was produced by
diffing the actual baked `Links[].URL` strings**, not by reading metadata IDs. That
distinction is load-bearing and was got wrong in the first revision of this report (see
the correction log at the end): a models.dev **metadata ID** such as
`minimax/MiniMax-M2.7-highspeed` is an upstream *key*, and it says nothing about which
repository the row points at. Only the URL does.

The method: group every baked metadata row by each of its link URLs and report each URL
claimed by two or more distinct metadata IDs. That is a total census — it cannot miss a
same-repo pair, and it cannot invent one.

**Speed-SKU same-repo pairs (the load-bearing findings), all URL-verified:**

| Plain row | Speed-SKU row | Shared URL |
|---|---|---|
| `moonshotai/kimi-k2-thinking` | `moonshotai/kimi-k2-thinking-**turbo**` | `https://huggingface.co/moonshotai/Kimi-K2-Thinking` |
| `moonshotai/kimi-k2.7-code` | `moonshotai/kimi-k2.7-code-**highspeed**` | `https://huggingface.co/moonshotai/Kimi-K2.7-Code` |
| `zhipuai/glm-4.7-flash` | `zhipuai/glm-4.7-**flashx**` | `https://huggingface.co/zai-org/GLM-4.7-Flash` |

**Speed-SKU rows that point at the PLAIN repo (no distinct repo exists), URL-verified:**

| Row | URL it actually points at |
|---|---|
| `minimax/MiniMax-M2.7-highspeed` | `https://huggingface.co/MiniMaxAI/MiniMax-M2.7` |
| `minimax/MiniMax-M2.5-highspeed` | `https://huggingface.co/MiniMaxAI/MiniMax-M2.5` |

There is **no** `MiniMax-M2.7-highspeed` repository. The `-highspeed` string appears only
in the upstream metadata key; the weights link resolves to the plain `MiniMax-M2.7`.

**The literal `turbo`/`fast` token census.** Across all 247 baked metadata rows, exactly
**one** row's ID carries the token `turbo` (`moonshotai/kimi-k2-thinking-turbo`) and
**zero** carry `fast` as a speed SKU. So no `LinkWeights` row exists for
`gpt-4-turbo`, `Llama-3.3-70B-Instruct-Turbo`, `whisper-large-v3-turbo`, `veo-3-fast`,
`grok-4-fast`, or any other turbo/fast spelling. **Absence of a link is not evidence of
sameness** — it is missing evidence, and the dispositions below say so plainly.

That token census is, however, the wrong frame on its own: labs spell the same idea
`turbo`, `highspeed`, `flashx`, `fast`. Every same-repo pair above is one lab shipping
**one set of weights under two serving SKUs**, and all three labs involved (Moonshot,
Z.ai, MiniMax) do it. Where a lab is shown to do this, that is evidence about **that
lab's naming practice**, and it is the strongest class of evidence available here.

---

## Per-family findings and proposed dispositions

| Family | Rows | Identity today | Link evidence | Proposed disposition (USER-GATED) |
|---|---|---|---|---|
| `gpt` | 46 | `turbo` IDENTITY (`gpt@3.5{turbo}`, `gpt@4{turbo}`, `gpt@3.5{turbo,instruct}`, `gpt@4{vision,turbo}`); `gpt/oss#120b{fast}` | No turbo/fast weights link | **KEEP IDENTITY.** `gpt-4-turbo` and `gpt-4` are separately documented OpenAI models with different context windows, cutoffs and pricing — the archetypal *distinct artifact* reading, and the case the global fail-safe exists for. The lone `openai/gpt-oss-120b-fast` is a different question (a provider's speed SKU of an open-weights model) → **insufficient evidence**, see below. |
| `kimi` | 16 | `turbo` IDENTITY, `fast` already ATTRIBUTE | **URL-verified, TWO same-repo pairs:** `kimi-k2-thinking` ≡ `-turbo` (`.../Kimi-K2-Thinking`) and `kimi-k2.7-code` ≡ `-highspeed` (`.../Kimi-K2.7-Code`) | **DEMOTE `turbo` → ATTRIBUTE.** The strongest evidence in the report: a direct `turbo` pair, corroborated by a second speed-SKU pair at a different version. Would fold `kimi/k@2{turbo}` and `kimi/k@2.6{turbo}` into their plain entities. Note the interaction: the metadata rows would then compete for one entity — check `modelsdev_aliases.json` before landing. |
| `glm` | 37 | both already ATTRIBUTE | Plain weights links for `glm@4.5/4.6/5.1/5.2`; no link for any turbo/fast spelling. **URL-verified adjacent pair:** `zhipuai/glm-4.7-flash` ≡ `zhipuai/glm-4.7-flashx` (`.../GLM-4.7-Flash`) | **KEEP AS IS (attribute).** The existing demotion is consistent with the evidence: Z.ai's turbo/fast spellings appear only as third-party host SKUs (`z-ai/`, `zai-org/`, fireworks routers), never as their own weights repo — and the flash/flashx pair shows Z.ai does ship two SKUs off one repo. |
| `claude` | 6 | `fast` already ATTRIBUTE | No `-fast` link; the `-fast` ids exist only under aggregators | **KEEP AS IS (attribute).** `claude-opus-4.x-fast` appears exclusively as an aggregator SKU of a model Anthropic itself does not publish under a `-fast` name. |
| `deepseek` | 3 | `fast` ATTRIBUTE, `turbo` IDENTITY | No link for any turbo/fast spelling. (The URL census does show `deepseek-chat` ≡ `deepseek-reasoner` on `.../DeepSeek-V3.2` — two API SKUs off one repo, but a reasoning-mode split, not a speed tier) | **INSUFFICIENT EVIDENCE for `turbo`.** `deepseek/deepseek-r1-turbo` and `-v3-turbo` are aggregator ids; nothing in the catalog says whether they re-quantize. Symmetry with the landed `fast` demotion *suggests* ATTRIBUTE, but suggestion is not evidence — leave IDENTITY pending a lab-page check. |
| `minimax` | 2 | `fast` ATTRIBUTE, `turbo` IDENTITY | **URL-verified:** `minimax/MiniMax-M2.7-highspeed` → `.../MiniMax-M2.7`; `minimax/MiniMax-M2.5-highspeed` → `.../MiniMax-M2.5`. No `-highspeed` repo exists; the token is in the metadata KEY only | **DEMOTE `turbo` → ATTRIBUTE — proposed, at lower confidence than `kimi`.** MiniMax ships speed SKUs off the **plain** repo, twice, at two different versions. Under this report's stated methodology (same repo = same weights = serving tier) that is direct evidence that MiniMax speed tokens are tiers, and it **confirms** rather than undermines the already-landed `fast` demotion. Confidence is lower than `kimi`'s only because the evidence is for `-highspeed`, not for the literal `turbo` spelling (`minimax/minimax-m2.7-turbo` has no metadata row); the inference is lab-practice, not a direct turbo pair. |
| `qwen` | 23 | `fast` IDENTITY (`qwen@3.5#397b{fast}`, …); `turbo` mostly absorbed into the family/variant (`qwen/turbo`) | Plain weights links only (`Qwen3.5-397B-A17B`, …) | **KEEP IDENTITY, but flag a parse residual.** `qwen-turbo` decomposes to variant `turbo` (`qwen/turbo`) while `Qwen3.5-397B-A17B-fast` keeps `{fast}` — two different treatments of the same commercial idea. The disposition question here is *not* really identity-vs-attribute; it is that `qwen@3.5#397b{fast}` and `qwen@3.5#397b-a17b{fast}` are the same model under two size spellings. Route to the size-axis work, not to a modifier demotion. |
| `grok` | 49 | `fast` IDENTITY (`grok@4{fast}`, `grok@4.1{fast}`, `grok@4{reasoning,fast}`, …) | No links | **KEEP IDENTITY.** xAI documents `grok-4-fast` as its own model with its own pricing and context window, and the catalog carries `grok-4-fast-reasoning` / `-non-reasoning` as separate rows. Collapsing would merge documented, separately-priced models. |
| `llama` | 6 | `turbo`/`fast` IDENTITY (`llama@3.3#70b{turbo,instruct}` vs `{fast,instruct}` vs plain `{instruct}`) | Plain `Llama-3.3-70B-Instruct` weights link only | **INSUFFICIENT EVIDENCE — but the highest-value candidate.** Together/Cloudflare `-Turbo` and `-fp8-fast` are near-certainly **quantization SKUs** of Meta's one release (`fp8` is in the id!). The right fix is probably **not** an attribute demotion but routing the quant tag to the existing `Quantization` axis. Explicitly out of scope here; recommend a dedicated ruling. |
| `veo`, `imagen`, `seed`, `runway`, `ideogram`, `elevenlabs`, `whisper`, `morph`, `flux` | 1–5 each | IDENTITY | No links | **KEEP IDENTITY (media/audio models).** Google documents `veo-3-fast` and `imagen-4-fast` as separate, separately-priced models, and `whisper-large-v3-turbo` is a genuinely distinct distilled checkpoint OpenAI released. This group is the clearest *keep*. |
| `ernie`, `baichuan4`, `qianfan`, `hunyuan`, `ling`, `o` (cohere rerank) | 1–3 each | IDENTITY | No metadata rows at all | **INSUFFICIENT EVIDENCE.** Long tail; each would need its own lab-page check for a one-model payoff. Recommend deferring as a batch. |
| `gemma` | 1 | `turbo` IDENTITY (`gemma@4#31b{turbo}`) | Google publishes `.../gemma-4-31B-it`, attached to the PLAIN entity; **no link for the turbo spelling** | **INSUFFICIENT EVIDENCE — but noted separately from the long tail, because it does have a plain-entity link.** The sole id is `google/gemma-4-31B-turbo-TEE`; `TEE` (Trusted Execution Environment) is a hosting/attestation property, which *suggests* the row is a serving configuration of `gemma-4-31B-it` rather than distinct weights. That is inference from the id, not link evidence, so it stays insufficient. |
| `fast` (the family literally named `fast`) | 1 | `fast{fast}` | — | **PARSE BUG, not a classification question.** A bare id `fast` decomposes to family `fast` + modifier `fast`. Route to the parse-residual backlog. |

---

## Summary of proposals (each requires its own Impl-UAT ruling)

1. **DEMOTE** `kimi.turbo` → attribute — **highest confidence**. Two URL-verified
   same-repo pairs, one of them a direct `turbo` pair.
2. **DEMOTE** `minimax.turbo` → attribute — **lower confidence, still positive**. Two
   URL-verified `-highspeed` rows pointing at the plain repo establish the lab's
   practice; the literal `turbo` spelling has no metadata row of its own, so this rests
   on lab-practice inference rather than a direct pair.
3. **KEEP IDENTITY** for `gpt`, `grok`, and the media group (`veo`, `imagen`, `seed`,
   `runway`, `ideogram`, `elevenlabs`, `whisper`, `morph`) — labs document these as
   distinct, separately-priced models.
4. **KEEP AS IS** for the already-demoted `glm` and `claude` — consistent with evidence;
   the `glm-4.7-flash` ≡ `flashx` pair independently supports the `glm` demotion.
5. **INSUFFICIENT EVIDENCE** for `deepseek.turbo`, `gemma.turbo`, the long tail, and the
   whole `llama` group.
6. **NOT A CLASSIFICATION QUESTION** — three findings that should leave this axis
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

---

## Correction log (append-only)

### Revision 2 — 2026-07-22: `minimax` row falsified; all link claims re-swept by URL diff

**What was wrong.** Revision 1 asserted that "MiniMax *does* publish a distinct
`-highspeed` repo", cited it as evidence *against* demoting `minimax.turbo`, and carried
that claim into the summary. It is **false**. The string `-highspeed` occurs in the
models.dev **metadata ID** (`minimax/MiniMax-M2.7-highspeed`); the row's actual
`Links[].URL` is `https://huggingface.co/MiniMaxAI/MiniMax-M2.7` — the **plain** repo. No
`-highspeed` repository exists in the data at all.

**Error class.** Reading a **metadata ID** as if it named a repository. The metadata ID
is an upstream key; only the URL identifies the artifact. Revision 1 read the ID for the
`minimax` row while (correctly, but by luck rather than method) reading URLs for the
`kimi` row.

**Why it mattered.** The mistake did not merely weaken a cell — it **inverted the
disposition**. Under this report's own stated methodology (identical repo = one set of
weights = a serving tier), the corrected evidence points *toward* demotion. The row now
reads DEMOTE at explicitly lower confidence than `kimi`, because the URL evidence covers
`-highspeed` rather than the literal `turbo` spelling.

**Re-sweep.** Every repo-identity and repo-distinctness claim in the report was
re-derived from the baked `Links[].URL` strings by a total census (group all 247 metadata
rows by URL; report every URL claimed by 2+ distinct IDs). Results:

| Claim (rev. 1) | Verdict | Action |
|---|---|---|
| `kimi-k2-thinking` ≡ `-turbo`, same repo | **CONFIRMED** | kept; a SECOND pair (`kimi-k2.7-code` ≡ `-highspeed`) was found and added |
| `minimax` has a distinct `-highspeed` repo | **FALSIFIED** | row rewritten; disposition re-derived to DEMOTE (lower confidence) |
| "No other family carries a `LinkWeights` row for a turbo/fast spelling at all" | **TRUE but misleading** | true for the literal tokens `turbo`/`fast`; it excluded `-highspeed`/`flashx`, which are the same phenomenon. Section rewritten to lead with the URL census, with the token census kept as a sub-point |
| `glm`: no turbo/fast link | **CONFIRMED** | kept; the URL census additionally found `glm-4.7-flash` ≡ `flashx`, added as supporting evidence |
| `claude`: no `-fast` link | **CONFIRMED** | unchanged |
| `deepseek`: plain links only | **CONFIRMED** for turbo/fast | refined: the census does show `deepseek-chat` ≡ `deepseek-reasoner`, a reasoning-mode split, not a speed tier |
| `qwen`: plain links only | **CONFIRMED** | unchanged |
| `gpt`, `grok`, `llama`, media group: no turbo/fast link | **CONFIRMED** | unchanged |
| `gemma` listed in the "no links" long tail | **IMPRECISE** | `gemma` does have a plain-entity link (`gemma-4-31B-it`); split into its own row, disposition unchanged (insufficient) |

Two same-repo pairs (`kimi-k2.7-code` ≡ `-highspeed`, `glm-4.7-flash` ≡ `flashx`) were
**missed entirely** by revision 1 and are new in revision 2. Both strengthen, rather than
weaken, dispositions that were already proposed.

**Standing rule for any future revision of this report:** a repo-identity or
repo-distinctness claim must cite the **URL**, never the metadata ID.
