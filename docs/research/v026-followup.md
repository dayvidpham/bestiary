---
title: "v0.2.6 FOLLOWUP — MoE param-size shapes, mid-ID token extraction, VRAM presentation & fixtures — Domain Research"
date: "2026-07-15"
depth: "deep-dive"
request: "bestiary-flt9b (RESEARCH); epic bestiary-xds5d; children ol5mt/cuf4q/49lji/p0w6f"
---

## Executive Summary

The v0.2.6 epoch has four deferred work items. Research confirms all four are **low-novelty extensions of established bestiary precedents** — there is no new external standard bestiary must adopt wholesale, but two authoritative references anchor the design:

1. **The llama.cpp/ggml GGUF filename naming convention** is the one formal grammar for encoding parameter size, expert count, and MoE shape in a model identifier. It ratifies bestiary's already-chosen `#8x22b` (experts×per-expert) and gives the `A`-prefixed active-params notation that maps to `#Nb-aMb`. Adopt it as the canonical reference for the `#size` grammar (**ol5mt**).
2. **The LMCache `kvcache-view` KV formula is byte-for-byte identical to bestiary's KV term** — the single cleanest independent validation for the VRAM fixtures (**49lji**).

Per item:

- **ol5mt (MoE param-size):** the token *shapes* `8x22b` and `30b-a3b` are **already recognized** by `reParamSizeToken` and canonicalized by `ParseParamSize` (`parse.go:3059`). The genuinely new work is (a) additive `TotalParams`/`ActiveParams`/`ExpertCount` fields and (b) a decomposition function. **Key finding to escalate:** there are actually **four** shapes with **incompatible number-semantics** — the leading number is *total* in Qwen's `30b-a3b` but *active* in Llama-4's `17b-16e`, so decompose by suffix, never by position. The ratified `#30b-a3b`/`#8x22b` scope **omits** the Llama-4 `Nb-KE` (active+experts) form, which `reParamSizeToken` does not currently match, and the models.dev catalog drops the `-16E` suffix leaving a bare `17b` that looks dense but is *active* — both worth surfacing to the architect. `NxM` is nominal (Mixtral 8x7B is 46.7B total, not 56B) so never compute `Total=N×M`. Recommend `int64` raw counts + keep the canonical token as identity carrier. **Adapt** the GGUF convention.
- **cuf4q (VRAM typical-context):** strong cross-tool convention is **4K (4096)** — simultaneously Ollama's auto-default for consumer (<24 GiB) GPUs and LM Studio's in-app chat default. Recommend surfacing a **"typical (4K)"** figure beside the model-max headline (optionally a 4K/8K pair). Pure presentation via the existing `EstimateVRAM(ctx)`; no data-model change. **Adopt.**
- **49lji (VRAM fixtures):** frame as **method-validation, not ground-truth** — no measured machine-readable VRAM-at-context dataset exists (re-verified 2026-07-15). Validate the two formula *terms* independently: KV against LMCache `kvcache-view` (identical formula), weights against bartowski GGUF file-byte tables. **Adopt** the two-term validation design.
- **p0w6f (general mid-ID extraction):** `extractModifiers` structurally stops at the variant/version boundary (`parse.go:1213`), so mid-ID tokens (buried modifiers AND the bulk size population) are unreachable and today handled by ~26 curated exact-ID overrides (`idFamilyOverrides`, `parse.go:3523`). The general engine is one mid-ID scanner over two token domains (modifier + size). **Adapt** the existing tail-inward machinery inward-of-boundary, guarded by the census/up-to-date regression tests that already exist.

Upstream **models.dev does no structured size/MoE parsing at all** — it uses a hand-maintained family enum (`family.ts`) and substring matching (`describe.ts`). bestiary's canonical decomposition is already more capable than its data source; there is no upstream engine to borrow.

---

## Topic Area 1 (ol5mt): MoE parameter-size shape grammar

### Current state (local)

The param-size token grammar is already MoE-aware at the *recognition* layer. `reParamSizeToken` (`parse.go:3059`) matches three shapes:

```go
// parse.go:3059
var reParamSizeToken = regexp.MustCompile(`^(?i:\d+(?:\.\d+)?[bm]|\d+x\d+b|\d+b-a\d+b)$`)
```

- dense: `120b`, `7b`, `1.5b`, `560m`
- MoE experts×per-expert: `8x22b`, `8x7b`
- MoE total-active: `30b-a3b`, `235b-a22b`, `480b-a35b`

`ParseParamSize` (`parse.go:3091`) lowercases and validates against that regex, returning the canonical token (`"8x22B"` → `"8x22b"`). But the token is stored as an **opaque string** on `EntityRef.ParamSize` / `ModelInfo.ParamSize` (`entity.go:89`, `bestiary.go:61`) and rendered as the `#size` key segment (`entity.go:126`). There is **no numeric decomposition** — grep confirms no `TotalParams`/`ActiveParams`/`ExpertCount` fields exist anywhere.

The carrier chain today is **curated-only**: `param_size` in `quant_vram.json` → `ParamSizeFor(id)` (`quant_vram_data.go:401`) → `ModelInfo.ParamSize` → registry grouping → `EntityRef`. Only **4 models** carry a curated `param_size` today (`llama-3.3-70b-instruct`, `llama-3.3-8b-instruct`, `llama-3.2-3b-instruct`, `ollama/dracarys2-llama-3-70b-instruct`). This is exactly the ol5mt gap: bulk-extract the size from the ID rather than curate it per-model.

### Coverage opportunity (measured against the committed catalog snapshot)

Of **247** models in `parse/data/modelsdev/catalog.json`:

| Shape | Count | Examples |
|-------|-------|----------|
| total-active `NNb-aMMb` | **15** | `qwen3-235b-a22b`, `qwen3-coder-480b-a35b-instruct`, `qwen3-next-80b-a3b-instruct`, `nemotron-3-ultra-550b-a55b`, `gemma-4-26b-a4b-it` |
| dense `Nb`/`Nm` | **30** | `qwen3-32b`, `llama-4-scout-17b-instruct`, `pixtral-12b`, `ornith-1.0-397b`, `llama-3.1-nemotron-ultra-253b` |
| experts×per-expert `NxMb` | **0** in this snapshot | (Mixtral `8x7b`/`8x22b` not in the labs snapshot; real Ollama/HF convention — forward-looking) |

So **~45 IDs (18%)** carry an extractable size token today, vs 4 curated — an ~11× coverage increase from bulk extraction. The `NxMb` shape has zero hits in the committed snapshot but must stay supported: it is the canonical Mixtral/Ollama form and appears the moment a Mixtral-class model enters the catalog.

### The four shapes in the wild — and the number-semantics trap

The single most important design finding: there are **four** MoE size-name shapes, not three, and **the leading number means different things across them**. A parser that assumes "leading number = total params" mislabels every Llama-4 model.

| Shape | Grammar | Number semantics | Real IDs | Convention owner |
|-------|---------|------------------|----------|------------------|
| dense | `Nb`/`Nm` | total = active | `70b`, `0.5b`, `560m`, `qwen3:32b` | universal |
| experts×per-expert | `NxMb` | experts × *nominal* per-expert; **NOT a true total** | `mixtral:8x7b`, `mixtral:8x22b` | Mistral (formalized in GGUF) |
| total-active | `Nb-aMb` | **leading = TOTAL**, `a…` = ACTIVE | `qwen3:30b-a3b`, `qwen3:235b-a22b`, DeepSeek `671b-a37b` | Qwen |
| active + expert-count | `Nb-KE` | **leading = ACTIVE**, `…E` = expert count, **total unknown from ID** | `Llama-4-Scout-17B-16E`, `Llama-4-Maverick-17B-128E` | Meta (Llama 4) |

Disambiguator: **the suffix, not the leading number** — `-aNb` ⇒ leading is total; `-KE` ⇒ leading is active. No single model publishes two forms; each family commits to one. Match **case-insensitively** (Ollama lowercase `b`/`a` vs GGUF uppercase `B`/`A`/`E`).

**NxM is nominal, not a true total** (confirmed from Mistral's own numbers): Mixtral 8x7B nominal 8×7=56B but real total **46.7B** / active **12.9B**; 8x22B nominal 176B but real total **141B** / active **39B** (attention+embeddings are shared, only FFN blocks replicate). ⇒ for `NxM`, populate `ExpertCount=N` but **never** compute `TotalParams = N×M` (overshoots ~15–20%).

**Catalog trap (Llama-4):** the models.dev catalog carries `llama-4-maverick-17b-instruct` / `llama-4-scout-17b-instruct` — the `-16E`/`-128E` suffix is **dropped**, leaving a bare `17b` that *looks dense but is the ACTIVE count* (total is ~109B/~400B). Extracting `17b` as a dense total is silently wrong for these two IDs. Safe handling: treat a bare leading `Nb` as `ParamSize` (identity token) without asserting it is *total* unless a suffix/curation says so.

### External standard: the GGUF / llama.cpp filename naming convention

The one formal grammar (ggml `docs/gguf.md`, "GGUF Naming Convention"):

```
[<Sidecar>]<BaseName><SizeLabel><FineTune><Version><Encoding><Type><Shard>.gguf
SizeLabel = <expertCount>x<count><scale>   scale ∈ {Q:Quadrillion, T:Trillion, B:Billion, M:Million, K:Thousand}
            plus optional "-<attributes><count><scale>" appended "as needed"
```

Canonical example `Mixtral-8x7B-v0.1-KQ2.gguf`. **Key nuance:** the GGUF spec only formalizes the **`NxM` form**; the `A`/active notation (`30b-a3b`, Llama `17B`) is **not** a named GGUF field — it falls out as a generic "additional attribute" (`-A3B`). So `total-active` and `active+E` are **vendor conventions (Qwen, Meta) layered on GGUF**, not part of the standard. bestiary's `#Nb-aMb` is therefore a *bestiary* canonicalization of a vendor convention — correct to keep curated/deterministic rather than expecting a spec to define it.

### Structured metadata: where the counts actually live

- **GGUF keys** (namespaced by `general.architecture`): `[llm].expert_count` (total experts), `[llm].expert_used_count` (activated per token), plus `block_count`, `attention.head_count_kv`, `embedding_length`, `context_length`. These map cleanly to `ExpertCount`. **GGUF stores no scalar total/active *param* count** — those are summed from tensor shapes.
- **HF `config.json`** — the activated-experts key is **stable** (`num_experts_per_tok`, all three arches), but the **total-expert key is arch-specific**: Mixtral `num_local_experts`, Qwen3-MoE `num_experts`, DeepSeek `n_routed_experts` (+`n_shared_experts`). ⇒ a curated per-arch mapping is warranted, not a single field name.
- **safetensors** file metadata has only per-tensor dtype+shape (no total-param field); the HF Hub *API* aggregates `safetensors.total` server-side.

### Assessment & recommendation (ol5mt)

| Aspect | Finding |
|--------|---------|
| Token shapes | 3 of 4 already recognized in `reParamSizeToken`; the **Llama-4 `Nb-KE` (active+experts) form is NOT matched** — a real gap vs the ratified `#30b-a3b`/`#8x22b` scope. |
| Number semantics | Distinct per shape — decompose by **suffix branch**, never by leading-number position. |
| New fields | Additive `TotalParams`/`ActiveParams`/`ExpertCount`; populate only what the shape supports (`NxM`→ExpertCount only; `Nb-aMb`→Total+Active; `Nb-KE`→Active+ExpertCount, Total curated/unknown). |
| Numeric unit | `int64` raw parameter counts (so `560m` and `70b` share one unit); keep the canonical token string as identity carrier so the `#size` key stays byte-stable. |
| Identity impact | Additive fields → likely `BestiarySchemaVersion` bump, **no key churn**; dense stays `#Nb`. |

**Adoption recommendation:** **Adapt** the GGUF naming convention as the `NxM` reference; treat `Nb-aMb`/`Nb-KE` as curated vendor-convention branches. **Build** the additive `int64` fields + a suffix-branched decomposer that refuses to invent a total it cannot derive (NxM, Llama-4). **Surface to the architect** two scope items the ratified direction omits: (1) the Llama-4 `Nb-KE` shape + its leading=active semantics, and (2) the bare-`17b` catalog trap. Keep byte-stability of existing keys a hard invariant (v0.2.4 `#size` migration discipline).

---

## Topic Area 2 (cuf4q): VRAM typical-context presentation

### Current state (local)

The CLI `writeQuantRows` (`cmd/bestiary/main.go:1252`) prints one row per quant: `QUANT | WEIGHTS | VRAM | CTX | PARTIAL`, where `VRAM` is baked at **model-max** context (`QuantVRAM.VRAMContextTokens`). `(QuantVRAM).EstimateVRAM(ctx)` (`vram.go:56`) already recomputes at any caller-chosen context with no state change — so a second, typical-context figure is **pure presentation**, no data-model change (as the bead notes).

### Prior art: how tools present memory, and at what context

| Tool | Headline number | Factors context? | "Typical" context it uses |
|------|-----------------|------------------|---------------------------|
| Ollama (ollama.com) | GGUF file size (disk) | No on the page; runtime only (`ollama ps`) | **4K auto** for <24 GiB GPUs (24–48→32K, ≥48→256K) |
| LM Studio | estimated VRAM (weights+KV) | **Yes** (`lms load --estimate-only --context-length`) | **4096** (in-app chat default) |
| HF Model Memory Utility | weights-to-load only | No (context-free) | n/a; advises +~20% for inference |

The ollama.com "Size" that surprises users is **disk bytes ≈ WeightsBytes with zero KV**; the model-max headline adds the full KV term, which is why it looks so much larger. A typical-context figure directly closes that gap.

### Assessment & recommendation (cuf4q)

**Adopt.** Surface a **"typical (4K)"** VRAM figure beside the model-max headline — 4K is the strongest cross-tool convention (Ollama consumer auto-default *and* LM Studio chat default). If a pair is wanted, show **4K and 8K** (8K = common "comfortable chat" step). Label the columns explicitly as **typical vs model-max** so the semantics are unambiguous. Implement via `EstimateVRAM(4096)` (and `EstimateVRAM(8192)`); no schema/store change. Partial rows (weights-only) show the same value at both contexts (KV=0) — that is honest and should not be special-cased.

Sources: https://docs.ollama.com/context-length ; https://lmstudio.ai/docs/cli/local-models/load ; https://huggingface.co/spaces/hf-accelerate/model-memory-usage

---

## Topic Area 3 (49lji): VRAM benchmark / sanity fixtures

### Framing: method-validation, not ground-truth

Re-verified 2026-07-15: there is **still no canonical machine-readable MEASURED VRAM-at-context dataset**. The pinnable sources validate the two formula *terms* independently. bestiary computes `VRAMBytes = WeightsBytes + KVCache`, `KVCache = 2·layers·kvHeads·headDim·ctx·2` — the fixtures should assert the *method*, and explicitly document that real-GPU overhead is unvalidated (by design, `VRAMFormulaVersion = 2`, no overhead constant).

### Sources (verified current, 2026-07-15)

| Source | Status | Validates | URL |
|--------|--------|-----------|-----|
| LMCache `kvcache-view` | live, current | **KV term (exact-formula match)** | https://github.com/LMCache/kvcache-view |
| bartowski `*-GGUF` cards | live, current | **Weights term (ground-truth file bytes)** | https://huggingface.co/bartowski/Qwen_Qwen3-8B-GGUF |
| apxml calculator + blog | live, current | Method cross-check; blog publishes formula | https://apxml.com/tools/vram-calculator |
| HF Model Memory Utility | live | Weights-to-load only | https://huggingface.co/spaces/hf-accelerate/model-memory-usage |

**LMCache `kvcache-view` is the strongest KV cross-check** — its formula `KV = 2 × layers × tokens × kv_heads × (hidden_size/attention_heads) × dtype_size` is **identical** to bestiary's (`head_dim = hidden_size/attention_heads`; fp16 `dtype_size = 2`). It is GQA-aware and supports INT8/INT4 KV. **bartowski** publishes exact per-quant GGUF file sizes (e.g. Qwen3-8B: `Q4_K_M` 5.03 GB, `Q8_0` 8.71 GB, `BF16` 16.39 GB) — ground truth for `WeightsBytes`. **apxml**'s blog publishes `weights = P×(Q/8)×1.2` (a deliberate ~20% overhead) and the same per-token KV term — confirming our *method* is standard; the only divergence is their overhead vs our deliberate none.

New-in-2026 material is **ad-hoc blog benchmarks** (llama.cpp per-context tables; single-GPU roundups), usable as **loose non-strict sanity bands** but never as a pinnable dataset.

### Assessment & recommendation (49lji)

**Adopt** a two-term validation fixture set:
1. **KV-term conformance:** assert `EstimateVRAMBytes` KV component equals the LMCache formula on a few known architectures (Llama-3.1-8B, a Qwen3, a strongly-GQA model).
2. **Weights-term anchor:** assert `WeightsBytes` matches exact bartowski file sizes for those quants.
3. **Optional loose bands:** llama.cpp blog measured deltas as non-strict range checks.

Never label any fixture a real-GPU ground-truth measurement; document the unvalidated-overhead caveat inline.

Sources: https://github.com/LMCache/kvcache-view ; https://huggingface.co/bartowski/Qwen_Qwen3-8B-GGUF ; https://apxml.com/posts/how-to-calculate-vram-requirements-for-an-llm

---

## Topic Area 4 (p0w6f): general mid-ID token extraction

### Current state (local)

`extractModifiers` (`parse.go:1213`) scans the ID's tail tokens **from the end inward**, collecting modifiers and skipping transparent date/param/quant/context tokens, and **stops at the first real boundary token** (a variant/version/family token) — comment at `parse.go:1266`. Consequently:

- **Mid-ID modifiers** (a modifier token *before* the variant/version) are unreachable: `gemini-omni-flash-preview`, `qwen-omni-turbo`, `nemotron-3-nano-omni-30b-a3b-reasoning`, `gpt-realtime-2.1`.
- **The bulk size population** is unreachable as a captured field: `isModifierTransparentToken` (`parse.go:1181`) includes `isParamSizeToken`, so a size token is *seen and skipped* but never captured into `ParamSize`. This is the same structural gap as ol5mt — **size tokens are mid-ID too**, which is why the bead unifies the two into one engine.

Today these are handled by **~26 curated exact-ID overrides** in `idFamilyOverrides` (`parse.go:3523`), each pinning the full `(family, variant, version, modifiers)` tuple. Measured against the committed snapshot: **10** IDs carry a known modifier token in non-tail position (mid-ID candidates) and **2** carry dot-glued variant/version forms (`laguna-xs.2`, `laguna-m.1`).

### Sub-strands the bead carries

- **Dot-glued variant forms** (`laguna-xs.2`, `mimo-v2.5`): a variant/series letter glued to a `.N` version with no hyphen. `splitGluedVersionModifier` (`parse.go:3144`) handles the *trailing-letter* case (`glm-4.5v`); the *leading* dot-glued variant is curated-only today.
- **Family-aware fast/turbo reconciliation:** already surveyed and RESOLVED (report on `bestiary-iu71c`). The classification is intentionally family-aware — a global identity fail-safe with per-family attribute demotions (`claude`/`glm`/`kimi`/`deepseek`/`minimax`) in `modifier_class.json`. User nuance (verbatim): *"Some turbo models might indicate different entities, but fast usually doesn't."* Direction: **no wholesale class flip** — any future speed-tier change stays per-family. This strand is **report-only / no reclassification** for the epoch.

### External prior art

The GGUF filename grammar (`docs/gguf.md`, quoted in Topic Area 1) is the closest thing to a positional-token model for a model identifier: `<BaseName><SizeLabel><FineTune><Version><Encoding>…`. It confirms the token *classes* bestiary already splits (base/size/finetune-modifier/version/encoding) and that a size token can sit **mid-string** (before the version), exactly the case the tail-inward scanner cannot reach. It is a validation of the token taxonomy, not a drop-in engine.

Upstream **models.dev does not parse IDs positionally at all** — `family.ts` is a hand-maintained enum (with version baked into family names like `qwen3.5`, `glm-air`) and `describe.ts` does whole-string substring matching (`describeModel`, `family.ts:441 inferKimiFamily` is the sole special case). So there is **no upstream mid-ID extraction engine to adopt**; bestiary's decomposition already exceeds its source.

### Assessment & recommendation (p0w6f)

**Adapt** the existing tail-inward scanner to also extract a *known* mid-ID token (modifier or size) that precedes the variant/version boundary, then re-derive variant/version from the remainder — one engine, two token domains (modifier + size), replacing the bounded curated overrides with general machinery. Guardrails already exist and must gate the change: the real-input up-to-date regen test, the census guard (`TestMetadataCensus_SynthesizedStandalonesPinned`), and the byte-identity codegen test. Merge planning with ol5mt (identical extraction need). Keep the fast/turbo strand report-only.

---

## Summary

| Topic Area | Recommendation | Rationale |
|------------|---------------|-----------|
| ol5mt — MoE size grammar | **Adapt** (GGUF convention) + build additive fields | Shapes already recognized; add `int64` Total/Active/Expert + decomposer; dense stays `#Nb` |
| cuf4q — typical-context VRAM | **Adopt** 4K figure beside model-max | 4K = Ollama + LM Studio convention; `EstimateVRAM(ctx)` exists; presentation-only |
| 49lji — VRAM fixtures | **Adopt** two-term method-validation | No measured dataset; KV↔LMCache (identical), weights↔bartowski (exact bytes) |
| p0w6f — mid-ID extraction | **Adapt** tail scanner inward-of-boundary | One engine, two token domains; guarded by existing regression tests; fast/turbo report-only |

## Key Takeaways

### Adopt
- **4K typical-context VRAM** figure beside the model-max headline (Ollama consumer auto-default + LM Studio chat default), via `EstimateVRAM(4096)`; no schema change.
- **Two-term VRAM validation fixtures:** KV against LMCache `kvcache-view` (identical formula), weights against bartowski exact GGUF file bytes; framed as method-validation.

### Adapt
- The **GGUF/llama.cpp naming convention** as the reference grammar for `#8x22b`/`#Nb-aMb`; the three shapes are already recognized in `reParamSizeToken`.
- The **tail-inward modifier scanner** generalized to extract mid-ID tokens (modifier + size) before the variant/version boundary — one engine for ol5mt + p0w6f.

### Defer
- **True `TotalParams` for `NxM` and Llama-4 `Nb-KE`** — not derivable from the ID token (NxM nominal overshoots ~15–20%; Llama-4 leading `17b` is *active*, total ~109B/400B). Populate `ExpertCount`/`ActiveParams` from the ID; leave `TotalParams` to curation/GGUF metadata rather than inventing it.
- **Llama-4 `Nb-KE` (active+experts) shape** — outside the ratified `#30b-a3b`/`#8x22b` scope; flag to the architect, decide in/out for v0.2.6.
- Ingesting GGUF `expert_count`/HF `num_experts_per_tok` directly (per-arch config keys) — a curated per-arch mapping, deferred to when a GGUF/HF ingest path lands.
- Release-stage dimension for `preview`/`latest` (GH#13, already deferred).

### Skip
- Borrowing any upstream models.dev ID-parsing engine — it does none (hand-maintained family enum + substring matching).
- Ingesting a published VRAM figure or a "measured" dataset — none exists in machine-readable form; the fixtures are method-validation only.
