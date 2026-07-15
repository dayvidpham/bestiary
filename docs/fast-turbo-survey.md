# `fast` / `turbo` classification survey

**Status: report-only.** This document audits how the `fast` and `turbo` modifier
tokens are classified across families in the current catalog. It changes **no**
classification. Any future change to a speed-tier token's class stays **per-family**
(never a wholesale flip) and is a separate, curated act — this survey only assembles the
evidence a curator would weigh.

## Governing principle

The user framed the target behaviour directly:

> "Some turbo models might indicate different entities, but fast usually doesn't."

So the two tokens are **not** symmetric. `fast` is usually a serving/speed tier (safe to
treat as a per-instance attribute), while `turbo` is more often part of a model's actual
identity (`gpt-3.5-turbo`, `whisper-large-v3-turbo`), so it should be demoted far more
conservatively.

## How classification works today

`ClassifyModifier(token, family)` (`modifierclass.go`) partitions each trailing modifier
into one of two roles via the curated table `parse/data/modifier_class.json`:

- **IDENTITY** — part of the entity key; renders in the `{…}` segment. Two IDs that
  differ only by an identity modifier are **distinct entities**.
- **ATTRIBUTE** — excluded from the key; renders in the `[…]` segment. An attribute
  modifier folds its ID onto the base entity as a per-instance runtime tier.

The table has two layers. The **global** default for the ambiguous speed tokens is the
fail-safe **IDENTITY** (never silently collapse two artifacts). A **per-family override**
then demotes a specific token to ATTRIBUTE where curation has established it is a
speed/runtime tier for that family:

| Layer | `fast` | `turbo` |
|-------|--------|---------|
| global default | IDENTITY | IDENTITY |
| `claude` | ATTRIBUTE | — |
| `glm` | ATTRIBUTE | **ATTRIBUTE** |
| `kimi` | ATTRIBUTE | — |
| `deepseek` | ATTRIBUTE | — |
| `minimax` | ATTRIBUTE | — |

Two observations already visible in the table:

1. `fast` is demoted for five families; `turbo` for only one (`glm`). That asymmetry is
   the encoded form of the governing principle.
2. Within `kimi` / `deepseek` / `minimax`, `fast` is demoted but `turbo` is **not** — the
   same principle applied token-by-token inside a family.

### The concrete effect of a demotion

Decomposing representative IDs shows what the class does to the key
(`identityMods` = the subset that enters the entity key):

| ID | modifiers | identity modifiers | resulting entity |
|----|-----------|--------------------|------------------|
| `glm-5-turbo` | `[turbo]` | `[]` (demoted) | folds onto `glm@5` |
| `glm-5` | `[]` | `[]` | `glm@5` |
| `claude-opus-4-8-fast` | `[fast]` | `[]` (demoted) | folds onto `claude/opus@4.8` |
| `grok-4-fast` | `[fast]` | `[fast]` (global identity) | distinct `grok@4{fast}` |
| `grok-4` | `[]` | `[]` | `grok@4` |

Demotion **merges** the tier variant onto the base entity; the global IDENTITY default
**keeps** them split.

## Method

Occurrences were enumerated over the full baked catalog (`StaticModels()`, 5,654 rows /
162 providers). A row counts as a **surfaced** occurrence when the decomposed
`Modifier` list carries the token — that is the form `ClassifyModifier` actually sees.
Counts below are **distinct model IDs** (the same ID served by many providers is counted
once). Tokens that appear in the raw ID but are absorbed into the variant/family and never
surface as a trailing modifier are reported separately as "raw-only".

## Census — `fast`

### Demoted to ATTRIBUTE (per-family override) — 20 distinct IDs

| Family | distinct IDs | representative IDs |
|--------|--------------|--------------------|
| `kimi` | 7 | `kimi-k2.5-fast`, `kimi-k2.6-fast`, `kimi-k2-instruct-fast`, `kimi-k2p6-fast` |
| `glm` | 6 | `glm-5.2-fast`, `glm-5.2-short-fast`, `glm-5p1-fast`, `glm-5p2-fast` |
| `claude` | 5 | `claude-opus-4.6-fast`, `claude-opus-4.7-fast`, `claude-opus-4.8-fast` |
| `deepseek` | 1 | `DeepSeek-V3.2-fast` |
| `minimax` | 1 | `MiniMax-M2.5-fast` |

### Left IDENTITY (global default) — 32 distinct IDs

| Family | distinct IDs | representative IDs | reads as |
|--------|--------------|--------------------|----------|
| `grok` | 10 | `grok-4-fast`, `grok-4.1-fast`, `grok-4.2-fast`, `grok-4-fast-reasoning` | distinct lab model ("Grok 4 Fast") |
| `qwen` | 6 | `qwen3.5-397b-fast`, `qwen3.6-35b-fast`, `qwen2.5-coder-7b-fast` | distinct serving line |
| `imagen` | 3 | `imagen-3-fast`, `imagen-4-fast`, `imagen-4.0-fast` | distinct fast image model |
| `veo` | 3 | `veo-3-fast`, `veo-3.1-fast` | distinct fast video model |
| `morph` | 2 | `morph-v3-fast` | distinct product tier |
| `llama` | 2 | `llama-3.3-70b-instruct-fp8-fast` | serving/quant packaging tag |
| `seed` | 2 | `seedance-2.0-fast`, `seedance-v1.0-pro-fast` | distinct fast video model |
| `gpt` | 1 | `gpt-oss-120b-fast` | serving tag |
| `qianfan` | 1 | `qianfan-ocr-fast` | distinct OCR tier |
| `o` | 1 | `cohere/rerank-v4-fast` | (family mis-parse, see notes) |
| `fast` | 1 | `fast` | placeholder/junk ID (see notes) |

Raw-only `fast` (token present, not surfaced as a trailing modifier): `grok` 12
(`grok-4-fast-non-reasoning`, `grok-code-fast-1` — `fast` sits mid-ID before another
token), plus single/low counts under `flux`, `imagen`, `stable-diffusion`, `veo`, `glm`.

## Census — `turbo`

### Demoted to ATTRIBUTE (per-family override) — 12 distinct IDs

| Family | distinct IDs | representative IDs |
|--------|--------------|--------------------|
| `glm` | 12 | `glm-5-turbo`, `glm-5v-turbo`, `GLM-4.6-turbo`, `GLM-5V-Turbo` |

### Left IDENTITY (global default) — 36 distinct IDs

| Family | distinct IDs | representative IDs | reads as |
|--------|--------------|--------------------|----------|
| `gpt` | 16 | `gpt-3.5-turbo`, `gpt-4-turbo`, `gpt-4-turbo-vision` | turbo **is** the model identity |
| `kimi` | 5 | `kimi-k2-turbo-preview`, `kimi-k2-thinking-turbo`, `kimi-k2p6-turbo` | serving speed tier (candidate) |
| `llama` | 3 | `Llama-3.3-70B-Instruct-Turbo`, `Meta-Llama-3.1-405B-Instruct-Turbo` | serving-optimized packaging |
| `deepseek` | 2 | `deepseek-r1-turbo`, `deepseek-v3-turbo` | serving speed tier (candidate) |
| `ernie` | 2 | `ernie-4.5-turbo-128k`, `ernie-x1-turbo-32k` | distinct lab line |
| `whisper` | 2 | `whisper-large-v3-turbo` | distinct distilled model |
| `baichuan4` | 1 | `Baichuan4-Turbo` | distinct lab line |
| `elevenlabs` | 1 | `elevenlabs-v2.5-turbo` | distinct fast TTS model |
| `gemma` | 1 | `gemma-4-31B-turbo-TEE` | serving/enclave tag |
| `ideogram` | 1 | `ideogram-v2a-turbo` | distinct fast image model |
| `minimax` | 1 | `minimax-m2.7-turbo` | serving speed tier (candidate) |
| `runway` | 1 | `runway-gen-4-turbo` | distinct fast video model |

Raw-only `turbo` (absorbed into the variant/family, never surfaced as a modifier):
`qwen` 9 (`qwen-turbo`, `qwen-doc-turbo`, `qwen-math-turbo`, `qwen-mt-turbo`,
`Qwen2.5-7B-Instruct-Turbo` — here `turbo` is part of Alibaba's product line name, so it
belongs to the variant, not the modifier), `ling` 2 (kling video turbo variants),
`ernie` 1.

## Assessment

**The existing per-family demotions are well-founded.** Every demoted `fast` family
(`claude`/`glm`/`kimi`/`deepseek`/`minimax`) and the single demoted `turbo` family
(`glm`) shows the tier attached to a base model that is also served without it — the
attribute reading (one entity, a faster serving row) matches how those labs describe the
tier. No demotion in the current table collapses two IDs that read as genuinely different
artifacts.

**The global IDENTITY default is correct where the tier names a real artifact.** The
`turbo` IDENTITY set is dominated by cases where the token is inseparable from the model:
`gpt-3.5-turbo` / `gpt-4-turbo` (turbo *is* the OpenAI line), `whisper-large-v3-turbo`
(a distinct distilled checkpoint), and distinct image/video/TTS "turbo" models
(`ideogram`, `runway`, `elevenlabs`). Likewise the `fast` IDENTITY set is dominated by
labs that ship a marketed "Fast" model (`grok-4-fast`, `qwen*-fast`) or a distinct fast
media model (`imagen`/`veo`/`seedance`). Demoting any of these would wrongly merge two
distinct entities — exactly the failure the fail-safe default prevents.

**Candidates a future per-family review might weigh (not this epoch).** Three families
carry a `turbo` that reads like `glm`'s already-demoted serving tier rather than a
distinct artifact:

- `deepseek` — `deepseek-r1-turbo`, `deepseek-v3-turbo` (served by an inference router as
  a faster serving of the same base; the base `r1`/`v3` are present separately).
- `kimi` — `kimi-k2-turbo-preview`, `kimi-k2p6-turbo` (Moonshot's higher-throughput
  serving of `k2`; base present separately). Note `kimi` already demotes `fast`.
- `minimax` — `minimax-m2.7-turbo` (single occurrence; base line present). `minimax`
  already demotes `fast`.

Each would be a **per-family `turbo → attribute`** override mirroring `glm`, and each
needs a curator to confirm from the lab's own description that the turbo row is the same
weights served faster (not a separately-trained model) before flipping. This is precisely
the "some turbo models might indicate different entities" caution — so the safe default
until then is to leave them IDENTITY (over-split, never wrong-merge).

**Against any `fast` change.** No family in the `fast` IDENTITY set is a demotion
candidate: `grok` and `qwen` `fast` rows are marketed distinct models, and the media
families (`imagen`/`veo`/`seed`) ship "fast" as a separate model. `fast` demotion should
stay confined to the current five families.

## Recommendation

1. **Make no classification change this epoch.** `parse/data/modifier_class.json` stays
   as-is; the survey is report-only.
2. **Keep the current five `fast` demotions and the single `glm` `turbo` demotion.** The
   evidence supports each.
3. **Record `deepseek` / `kimi` / `minimax` `turbo` as the only forward-looking
   demotion candidates**, gated on per-family curator confirmation (same-weights serving
   tier vs. distinct model). Any such change is a per-family override, never a global
   flip.
4. **Leave every `fast` IDENTITY family and the identity-bearing `turbo` families
   (`gpt`/`whisper`/media labs) unchanged** — demoting them would merge distinct
   entities.

## Notes

- The `identityMods=[]` vs `[fast]` split above comes from `EntityModifiers` /
  `ClassifyModifier`; it is the exact test a curator can re-run against the catalog.
- Two rows are data-quality curiosities unrelated to speed-tier classification and out of
  scope here: `cohere/rerank-v4-fast` decomposes to family `o` (an o-series over-capture),
  and a lone placeholder ID `fast` parses to family `fast`. Neither affects the `fast`
  classification decision.
- A dedicated size/param axis (`mini`/`pro`/`flash` reclassification) is deferred to
  GH#9 and is out of scope for this speed-tier survey.
