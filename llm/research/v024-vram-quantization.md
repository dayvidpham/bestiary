---
title: "v0.2.4 VRAM + Quantization — Domain Research"
date: "2026-06-09"
depth: "deep-dive"
request: "bestiary-5cbd7 (RESEARCH); parent bestiary-gycs2 (REQUEST); GH#12"
---

## Executive Summary

v0.2.4 adds **quantization** and **VRAM-usage** as PER-INSTANCE attributes (extend `ProviderInstance`, keyed by quant), settled as NOT part of `EntityRef` identity. Research confirms the design is sound and identifies the concrete data shapes:

- **Ollama registry is scrapeable and deterministic.** The Docker-Registry-v2 manifest API at `registry.ollama.ai/v2/...` is anonymous and returns, per tag, a config blob carrying `model_type` (param size), `file_type` (quant), `model_family`, and layer **file sizes** in bytes. It does **NOT** publish VRAM. Tag enumeration is the weak point: `/v2/.../tags/list` returns 404, so the tag set must come from scraping the `ollama.com/library/<model>/tags` HTML page (which also surfaces quant label, params, and download size).
- **Quantization should be a closed enum with a raw/Other escape**, mirroring the Provider-as-string precedent — but quant is a small, well-bounded set so a true int enum (like `DerivationKind`) fits better, with an `Other`/raw passthrough for unknown tokens. Authoritative bits-per-weight come from llama.cpp's own `quantize` README.
- **VRAM should be COMPUTED, not ingested** (Ollama doesn't publish it; computing from params × bpw + KV-cache + overhead is more deterministic for codegen than scraping flaky figures). Ingest the **file size** as ground truth for the weights term; compute KV-cache + overhead.
- **The hard problem is the models.dev ↔ Ollama ID-join** — IDs don't match 1:1 and need a family + param-size + quant decomposition/alias step, the same class of normalization rc2 already solved.

---

## Topic Area 1: Ollama Model Registry Format

### The registry v2 API (verified live, 2026-06-09)

Ollama's registry speaks the **Docker Distribution Registry v2** protocol, anonymously, over `https://registry.ollama.ai`. Verified live:

```
GET https://registry.ollama.ai/v2/library/<model>/manifests/<tag>
  Accept: application/vnd.docker.distribution.manifest.v2+json
```

Returns (real response for `library/llama3.2:1b`):

```json
{
  "schemaVersion": 2,
  "config": { "mediaType": "application/vnd.ollama.image.model",
              "digest": "sha256:4f659...", "size": 485 },
  "layers": [
    { "mediaType": "application/vnd.ollama.image.model",    "digest": "sha256:7470...", "size": 1321082688 },
    { "mediaType": "application/vnd.ollama.image.template",  "digest": "sha256:966d...", "size": 1429 },
    { "mediaType": "application/vnd.ollama.image.license",   "digest": "sha256:fcc5...", "size": 7711 },
    { "mediaType": "application/vnd.ollama.image.license",   "digest": "sha256:a70f...", "size": 6016 }
  ]
}
```

The `application/vnd.ollama.image.model` layer `size` is the **GGUF file size in bytes** (here 1.32 GB) — this is the on-disk weight footprint, the deterministic anchor for any VRAM estimate.

The config blob (`GET /v2/library/<model>/blobs/sha256:<digest>`, verified live) carries the structured metadata:

```json
{ "model_format": "gguf", "model_family": "llama", "model_families": ["llama"],
  "model_type": "1.2B", "file_type": "Q8_0", ... }
```

So per (model, tag) the registry deterministically yields: **param size (`model_type`), quantization (`file_type`), family, format, and weight file size**.

### Tag enumeration gap

`GET https://registry.ollama.ai/v2/library/llama3.2/tags/list` returns **404** — the registry does not expose the standard Docker tags-list endpoint. The list of available tags must be obtained by scraping the HTML at `https://ollama.com/library/<model>/tags`, which renders (verified for `llama3.3`):

| Tag | Quant | Params | Size |
|-----|-------|--------|------|
| `70b-instruct-q2_K` | q2_K | 70B | 26GB |
| `70b-instruct-q4_K_M` | q4_K_M | 70B | 43GB |
| `70b-instruct-q5_K_M` | q5_K_M | 70B | 50GB |
| `70b-instruct-q8_0` | q8_0 | 70B | 75GB |
| `70b-instruct-fp16` | fp16 | 70B | 141GB |
| … | … | … | … |

The page also shows context window (128K) and input modality but **no VRAM**.

### Assessment

| Aspect | Finding |
|--------|---------|
| Stable/deterministic | Manifest + config blob API: YES (content-addressed, immutable blobs). |
| Publishes VRAM? | **NO** — only on-disk file size. |
| Tag enumeration | NOT via API (`/tags/list` 404); requires HTML scrape of `ollama.com/library/<m>/tags` — the fragile bit. |
| Quant + params | YES, structured: `file_type` + `model_type` in config blob. |

**Recommendation: Adopt** the registry manifest+config-blob path for per-instance (quant, params, file-size). **Adapt** an HTML scrape (or a curated model list) for tag enumeration; treat scrape as the determinism risk — snapshot + sort + commit, never live in `go test`.

Sources:
- https://deepwiki.com/ollama/ollama/4.2-model-registry-and-layers
- https://medium.com/@dewasheesh.rana/inside-ollamas-model-storage-understanding-blobs-and-manifests-06f1620dd0b2
- https://ollama.com/library/llama3.3/tags
- Live API probes: `registry.ollama.ai/v2/library/llama3.2/manifests/1b` and its config blob (verified 2026-06-09)

---

## Topic Area 2: Quantization Namespace

### Authoritative bits-per-weight (from llama.cpp `tools/quantize/README.md`)

I-quants (importance-matrix, 1–4 bit):
IQ1_S 2.00, IQ1_M 2.15, IQ2_XXS 2.38, IQ2_XS 2.59, IQ2_S 2.74, IQ2_M 2.93, IQ3_XXS 3.25, IQ3_XS 3.50, IQ3_S 3.66, IQ3_M 3.76, IQ4_XS 4.46, IQ4_NL 4.68.

K-quants (super-block, 2–8 bit):
Q2_K_S 2.97, Q2_K 3.16, Q3_K_S 3.64, Q3_K_M 4.00, Q3_K_L 4.30, Q4_K_S 4.67, Q4_K_M 4.89, Q5_K_S 5.57, Q5_K_M 5.70, Q6_K 6.56.

Legacy block formats: Q4_0 ~4.5, Q4_1 ~5.0, Q5_0 ~5.5, Q5_1 ~6.0, Q8_0 8.50.

Float: F16/BF16 16.0, F32 32.0.

HF-ecosystem (non-GGUF): AWQ (4-bit), GPTQ (3/4/8-bit), bitsandbytes int8 / int4 / nf4 / fp4.

### Provenance / namespace split

| Format family | Ecosystem | Notes |
|---------------|-----------|-------|
| `Q*_0/_1` legacy, `Q*_K*`, `IQ*`, `F16/F32` | llama.cpp / GGUF (Ollama-native) | Ollama `file_type` uses these exact tokens (e.g. `Q4_K_M`, `Q8_0`, `fp16`). |
| AWQ, GPTQ | HF / vLLM / AutoGPTQ | Not Ollama; appear if HF is a later source. |
| int8/int4/nf4/fp4 (bitsandbytes) | HF transformers | Generic, runtime-applied. |
| BF16 | both | Ollama exposes as `bf16` / `fp16` interchangeably in places. |

### Recommended enum boundary

Quantization is a **small, closed, well-understood set** — closer to `DerivationKind` (int enum + Marshal/UnmarshalText + out-of-range guard) than to Provider-as-string. Recommend a `Quantization int` enum with a `QuantizationOther` escape + a raw string field on the instance for unknown tokens (lossless passthrough, audited like `parse_failures.json`). Carry a `BitsPerWeight()` method returning the authoritative bpw above (used by the VRAM estimator).

**Recommended enum members** (covers all Ollama `file_type` values + common HF):

```
QuantizationNone (zero value / unknown)
// GGUF float
QuantF16, QuantBF16, QuantF32
// GGUF legacy
QuantQ4_0, QuantQ4_1, QuantQ5_0, QuantQ5_1, QuantQ8_0
// GGUF k-quants
QuantQ2_K, QuantQ2_K_S, QuantQ3_K_S, QuantQ3_K_M, QuantQ3_K_L,
QuantQ4_K_S, QuantQ4_K_M, QuantQ5_K_S, QuantQ5_K_M, QuantQ6_K
// GGUF i-quants
QuantIQ1_S, QuantIQ1_M, QuantIQ2_XXS, QuantIQ2_XS, QuantIQ2_S, QuantIQ2_M,
QuantIQ3_XXS, QuantIQ3_XS, QuantIQ3_S, QuantIQ3_M, QuantIQ4_XS, QuantIQ4_NL
// HF ecosystem (defer until HF/Unsloth source lands)
QuantAWQ, QuantGPTQ, QuantInt8, QuantInt4
// escape
QuantizationOther  // + raw string on the instance
```

**Recommendation: Adopt** the int-enum + `Other`/raw-escape + `BitsPerWeight()`. The GGUF members are needed now (Ollama-native); AWQ/GPTQ/int* can be **deferred** until/if an HF source is added but should be reserved in the enum so the wire value is stable.

Sources:
- https://github.com/ggml-org/llama.cpp/blob/master/tools/quantize/README.md (bpw values)
- https://arxiv.org/html/2601.14277v1 (unified GGUF quant evaluation)
- https://kaitchup.substack.com/p/choosing-a-gguf-model-k-quants-i (k/i/legacy taxonomy)
- https://github.com/ggml-org/llama.cpp/discussions/5063

---

## Topic Area 3: VRAM Estimation Prior Art

### The formula (consensus across sources)

```
Total VRAM ≈ Weights + KV-cache + Activations + Overhead

Weights      = params × (bits_per_weight / 8)        [or: GGUF file size, ingested]
KV-cache     = 2 × layers × kv_heads × head_dim × ctx × kv_elem_bytes
               (head_dim = hidden / attn_heads; GQA: kv_heads << attn_heads)
Activations  ≈ 5–10% of total (inference)
Overhead     ≈ 500 MB – 2 GB framework/CUDA context
```

`whichllm` (the user-referenced baseline) computes VRAM **at runtime** in `engine/vram.py` as `weights + GQA KV cache + activation + overhead` (~500MB overhead), pulling model facts from the **HuggingFace API**, not Ollama. It applies per-quant multiplicative bpw discounts. HF "model memory calculator" and llama.cpp sizing use the same params × bpw + KV-cache decomposition.

### Ingest-data vs computed — recommendation

| Approach | Determinism | Source availability |
|----------|-------------|--------------------|
| Ingest VRAM figure | Low — Ollama publishes none; HF/blogs give ranges, not stable values | Poor |
| **Compute VRAM** | **High** — deterministic given (params, bpw, layers, kv_heads, head_dim, ctx, overhead constant) | Inputs are stable (config blob + GGUF KV metadata) |

**Recommendation: Compute.** Use the **ingested GGUF file size** (registry layer `size`) as the weights term (it already reflects the real quant), add a computed KV-cache term parameterized by `ContextWindow` (already on `ProviderInstance`) and a small set of architecture facts (layers, kv_heads, head_dim — available from GGUF KV metadata: `<arch>.block_count`, `<arch>.attention.head_count_kv`, `<arch>.embedding_length`), plus a fixed overhead constant. Computing is the only fully deterministic-for-codegen option; store the constants/formula version so estimates are reproducible. Represent VRAM as a computed `VRAMBytes` (uint64) on the per-(instance, quant) row, NOT a scraped figure.

Sources:
- https://www.spheron.network/blog/gpu-memory-requirements-llm/
- https://twm.me/posts/how-to-calculate-vram-requirement-local-llm-advanced/
- https://lyceum.technology/magazine/kv-cache-memory-calculation-llm/
- https://github.com/Andyyyy64/whichllm (engine/vram.py formula)
- https://www.whichllm.app/

---

## Topic Area 4: Unsloth as Secondary Source

Unsloth publishes **Dynamic 2.0 GGUF** quants on HuggingFace (`huggingface.co/unsloth/*-GGUF`). Key property: **per-layer mixed-bit quantization** — a single "Q4_K_XL"/"UD-*" file is NOT one uniform bpw; sensitive layers stay Q6/Q8 while others drop to Q2/Q3. They publish per-model "how to run locally" docs with approximate VRAM/RAM bands (e.g. 70B: Q4_K_M ~38–42 GB, Q5_K_M ~47–50 GB, Q8_0 ~72–75 GB), but as **prose ranges**, not a structured API.

Implications:
- Unsloth's dynamic quants **break the single-bpw assumption** of the estimator — their effective bpw must come from the actual file size, not a quant→bpw table.
- Metadata is HF-repo + docs prose, not a clean API; ingest is non-trivial and overlaps the HF-join problem.

**Recommendation: Defer (note-and-defer).** Unsloth is valuable as a coverage source for popular models but its dynamic quants need the file-size-as-weights approach (which we already recommend) and an HF ingest path that v0.2.4 should not block on. Reserve `QuantizationOther` + raw token to losslessly capture `UD-Q4_K_XL`-style labels if encountered, and revisit Unsloth as its own slice/epoch.

Sources:
- https://unsloth.ai/docs/basics/unsloth-dynamic-2.0-ggufs
- https://huggingface.co/collections/unsloth/unsloth-dynamic-20-quants
- https://www.spheron.network/blog/gguf-dynamic-quantization-gpu-cloud/

---

## Topic Area 5: The models.dev ↔ Ollama ID-Join (the hard problem)

models.dev IDs (e.g. `llama-3.3-70b-instruct`, decomposed by rc2 into Family/Variant/Version/Modifier) do NOT 1:1 match Ollama IDs (`llama3.3:70b-instruct-q4_K_M`). An alias/join step must decompose the Ollama tag into comparable axes:

```
Ollama tag:   llama3.3 : 70b - instruct - q4_K_M
              └family┘  └size┘ └modifier┘ └quant┘
```

Join key candidates: `(Family, Version, param-size, identity-modifiers)` → matches an `EntityRef`; the `(param-size, quant)` pair then keys the new per-instance VRAM/quant row. This is exactly the same normalization class as rc2's decomposition — but with two NEW axes Ollama exposes that models.dev does not split out: **param-size** (`70b`) and **quant** (`q4_K_M`). The param-size token (`70b`, `1.2B`) needs its own parser (and a units normalizer: `70b` vs `70B` vs `1.2B`).

Risks:
- Family-name mismatch (`llama3.3` vs `llama` + Version `3.3`) — needs an alias table.
- Not every models.dev entity has an Ollama presence (cloud-only providers) and vice-versa — the join is partial; many-to-one (one entity → many quant instances).
- Ollama `model_type` param size (`1.2B`) is a marketing/rounded value, not exact param count — fine for VRAM banding, risky as a join key if used alone.

**Recommendation: Adapt** the rc2 decomposition pipeline with a new Ollama-tag parser + a curated family-alias table (`parse/data/`-style), producing `(EntityRef-join-key, param-size, Quantization)`. Flag the join as the highest-risk slice; consider an explicit curated `ollama_aliases` table over heuristic matching for the first cut.

Sources:
- https://ollama.com/library/llama3.3/tags (tag naming)
- https://medium.com/@laurentkubaski/ollama-model-names-explained-a39460e0fab5 (`[NAME]:[SIZE]-[TYPE]-[QUANT]` convention)

---

## Summary

| Topic Area | Recommendation | Rationale |
|------------|---------------|-----------|
| Ollama registry ingest | Adopt manifest+config-blob; Adapt HTML tag-scrape | Structured quant/params/size via stable v2 API; tags need scrape (the risk). |
| Quantization enum | Adopt int-enum + Other/raw + BitsPerWeight() | Small closed set; DerivationKind precedent; lossless escape for unknowns. |
| VRAM representation | Adopt COMPUTED (file-size weights + computed KV) | Ollama publishes no VRAM; computing is deterministic for codegen. |
| Unsloth | Defer | HF-prose source, dynamic mixed-bpw; not worth blocking v0.2.4. |
| ID-join | Adapt rc2 decomposition + curated alias table | Highest-risk slice; new param-size + quant axes; partial many-to-one join. |

## Key Takeaways

### Adopt
- Ollama registry v2 manifest + config-blob path: deterministic `(quant=file_type, params=model_type, weight-bytes=layer size)`.
- `Quantization int` enum with `QuantizationOther`/raw escape + `BitsPerWeight()` (llama.cpp authoritative bpw).
- COMPUTED `VRAMBytes` per (instance, quant): file-size weights + KV-cache(ContextWindow, arch) + fixed overhead; version the formula.
- Add quant/VRAM as per-instance attributes on `ProviderInstance` (or a new `QuantInstance` keyed by quant); NOT on `EntityRef`.

### Adapt
- HTML scrape of `ollama.com/library/<m>/tags` for tag enumeration (no `/tags/list` API) — snapshot/sort/commit, network-gated out of `go test`.
- rc2 decomposition + curated Ollama family-alias table for the ID-join; new param-size parser.

### Defer
- Unsloth (HF prose source, dynamic mixed-bpw quants) — reserve `QuantizationOther` to capture `UD-*` labels.
- AWQ/GPTQ/int4/int8 enum members — reserve values now, wire up when an HF source lands.

### Skip
- Ingesting a published VRAM figure from Ollama — it publishes none; only file size.
- Treating Unsloth dynamic quants with a uniform bpw table — their per-layer bpw is non-uniform; use file size.

## Open Questions for the URE (Phase 2)

1. **Storage shape:** extend `ProviderInstance` with `[]QuantVRAM{Quant, FileSizeBytes, VRAMBytes}`, or a new `QuantInstance` type keyed by (instance, quant)? (Affects schema + JSON + ModelInfo.)
2. **VRAM context assumption:** compute VRAM at what context length — the instance `ContextWindow`, a fixed reference (e.g. 4K/8K), or emit a curve/band? Single number vs range?
3. **Architecture facts for KV-cache:** ingest layers/kv_heads/head_dim from GGUF KV metadata (requires reading the model blob's GGUF header, larger fetch), or curate them per family, or approximate from param-size?
4. **Tag-scrape determinism:** is an HTML scrape of `ollama.com/library` acceptable as a codegen source, or should v0.2.4 ship a curated allowlist of (model → quant tags) to keep ingest robust?
5. **ID-join policy:** curated `ollama_aliases` table (explicit, safe) vs heuristic family+size matching (broader coverage, drift risk) for the first cut?
6. **Coverage scope:** all ~115 providers, or just open-weight/local families that actually have Ollama entries? (Cloud-only entities have no quant/VRAM.)
7. **Param-size source of truth:** Ollama `model_type` is rounded marketing size; do we trust it, or require a models.dev param-count field (GH#9) first?
8. **Overhead constant:** fixed (e.g. 1 GB) or backend-dependent (CUDA vs Metal vs ROCm)? whichllm uses ~500 MB.
