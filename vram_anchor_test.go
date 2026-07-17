package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// ============================================================================
// VRAM anchor fixtures — METHOD-validation, NOT ground-truth measurement.
// ============================================================================
//
// PURPOSE
//
// These fixtures sanity-check the two independent TERMS of bestiary's VRAM
// method against external references, WITHOUT ever claiming a real-GPU
// ground-truth measurement:
//
//	VRAMBytes = WeightsBytes + KVCache
//	KVCache   = 2 * layers * kvHeads * headDim * ctx * 2   (fp16 K and V)
//
// Term 1 — KV cache (validated against LMCache `kvcache-view`):
//   The LMCache formula is
//       KV = 2 * layers * tokens * kv_heads * (hidden_size/attention_heads) * dtype_size
//   which is BYTE-FOR-BYTE IDENTICAL to ours (head_dim = hidden_size/attention_heads;
//   fp16 dtype_size = 2). Because the two formulas are identical, code-computing
//   BOTH sides would be a tautology — so every KV expected value below is a
//   HAND-COMPUTED integer literal (the arithmetic is spelled out inline), never
//   re-derived through any bestiary function. A mutated coefficient (the leading
//   factor-of-2 for K+V, or the fp16 element size) diverges from the literal and
//   fails.  Source: https://github.com/LMCache/kvcache-view
//
// Term 2 — weights (anchored to bartowski GGUF file-size tables):
//   bartowski publishes exact per-quant GGUF FILE sizes on its HF model cards.
//   Those figures are used here as SELF-CONTAINED fixture literals. They are
//   NOT joined against quant_vram.json — those rows are Ollama-ingested from a
//   DIFFERENT GGUF build and would be an apples-to-oranges comparison. The
//   assertion validates the METHOD property that our weights term is a pure
//   pass-through with ZERO overhead added (unlike apxml's deliberate ×1.2), so
//   it is exact regardless of the published figures' 2-decimal rounding.
//   Source: https://huggingface.co/bartowski/Qwen_Qwen3-8B-GGUF
//
// Soft cross-check (apxml) — DOCUMENTED, NEVER ASSERTED:
//   apxml's calculator publishes weights = P*(Q/8)*1.2 — the same per-token KV
//   term as ours plus a deliberate ~20% overhead. It confirms our METHOD is the
//   industry-standard one; the only divergence is their overhead vs our
//   deliberate none. apxml totals appear only as a soft ±band in comments below,
//   never as a Go assertion, because no measured machine-readable VRAM-at-context
//   dataset exists to pin an exact total to (re-verified 2026-07-15).
//   Source: https://apxml.com/posts/how-to-calculate-vram-requirements-for-an-llm
//
// UNVALIDATED BY DESIGN — real-GPU overhead:
//   bestiary's formula has NO overhead constant (VRAMFormulaVersion = 2; the
//   earlier 1 GiB overhead was removed). Real deployments incur allocator
//   fragmentation, CUDA context, activation buffers, and framework overhead that
//   these fixtures DO NOT and CANNOT validate — there is no canonical measured
//   dataset. Every number here validates the FORMULA's arithmetic, not a GPU's
//   actual peak footprint. Do not read any row as "this model needs exactly X on
//   a real card."
//
// Architecture facts (layers / kvHeads / headDim) below are published model
// config values; they only LABEL the anchors. The KV assertions are pure
// method-validation — the formula reproducing a hand-arithmetic result for the
// given inputs — so the tests remain valid even if a labeled config later drifts.

// ----------------------------------------------------------------------------
// Term 1: KV-cache conformance vs LMCache kvcache-view
// ----------------------------------------------------------------------------

// TestVRAMAnchor_KVTermMatchesLMCache pins the KV cache term against the LMCache
// `kvcache-view` formula (identical to ours) on real, well-known GQA
// architectures. weightsBytes is 0 so the returned value is the pure KV term.
//
// Expected values are HAND-COMPUTED literals (never code-derived from any
// bestiary formula — that would be a tautology given the formulas are identical).
func TestVRAMAnchor_KVTermMatchesLMCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		layers  int
		kvHeads int
		headDim int
		ctx     int
		// wantKV is hand-computed: 2 * layers * kvHeads * headDim * ctx * 2.
		wantKV int64
	}{
		{
			// Llama-3.1-8B: 32 layers, 32 q-heads, 8 kv-heads (GQA 4:1),
			// hidden 4096 -> head_dim 4096/32 = 128.
			// KV = 2 * 32 * 8 * 128 * 8192 * 2 = 1,073,741,824 (= 1 GiB).
			name:    "llama-3.1-8b @ 8K",
			layers:  32,
			kvHeads: 8,
			headDim: 128,
			ctx:     8192,
			wantKV:  1_073_741_824,
		},
		{
			// Same arch at native 128K context — proves ctx flows linearly
			// through the KV term (16x the 8K value).
			// KV = 2 * 32 * 8 * 128 * 131072 * 2 = 17,179,869,184 (= 16 GiB).
			name:    "llama-3.1-8b @ 128K",
			layers:  32,
			kvHeads: 8,
			headDim: 128,
			ctx:     131072,
			wantKV:  17_179_869_184,
		},
		{
			// Qwen3-8B: 36 layers, 32 q-heads, 8 kv-heads, head_dim 128.
			// KV = 2 * 36 * 8 * 128 * 8192 * 2 = 1,207,959,552.
			name:    "qwen3-8b @ 8K",
			layers:  36,
			kvHeads: 8,
			headDim: 128,
			ctx:     8192,
			wantKV:  1_207_959_552,
		},
		{
			// Qwen3-8B at native 32K context.
			// KV = 2 * 36 * 8 * 128 * 32768 * 2 = 4,831,838,208.
			name:    "qwen3-8b @ 32K",
			layers:  36,
			kvHeads: 8,
			headDim: 128,
			ctx:     32768,
			wantKV:  4_831_838_208,
		},
		{
			// Llama-3.1-70B: 80 layers, 64 q-heads, 8 kv-heads (STRONG GQA 8:1),
			// hidden 8192 -> head_dim 8192/64 = 128.
			// KV = 2 * 80 * 8 * 128 * 131072 * 2 = 42,949,672,960.
			name:    "llama-3.1-70b @ 128K (strong GQA 8:1)",
			layers:  80,
			kvHeads: 8,
			headDim: 128,
			ctx:     131072,
			wantKV:  42_949_672_960,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// weightsBytes = 0 isolates the KV term.
			got := bestiary.EstimateVRAMBytes(0, tt.ctx, tt.layers, tt.kvHeads, tt.headDim)
			if got != tt.wantKV {
				t.Errorf("KV term for %s = %d, want %d (hand-computed via LMCache formula "+
					"2*layers*kvHeads*headDim*ctx*2 with layers=%d kvHeads=%d headDim=%d ctx=%d)",
					tt.name, got, tt.wantKV, tt.layers, tt.kvHeads, tt.headDim, tt.ctx)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Term 2: weights anchor vs bartowski GGUF file sizes (no-overhead pass-through)
// ----------------------------------------------------------------------------

// TestVRAMAnchor_WeightsTermBartowski_NoOverhead anchors the weights term to
// bartowski's published Qwen3-8B GGUF file sizes and asserts the METHOD property
// that bestiary adds ZERO overhead to the weights figure (contrast apxml's
// deliberate ×1.2). Passing weightsBytes through with all arch facts zero must
// return the weights EXACTLY.
//
// The bartowski figures below are SELF-CONTAINED literals (NEVER joined against
// quant_vram.json — that is a different, Ollama-ingested GGUF build). They are
// bartowski's PUBLISHED, 2-decimal-rounded SI-GB figures (±~5 MB); the exact
// byte counts are not pinnable offline. That rounding does not weaken the test:
// the no-overhead identity holds for ANY literal, so this validates the formula's
// treatment of the weights term, not the file size to the byte.
//
// Source: https://huggingface.co/bartowski/Qwen_Qwen3-8B-GGUF
func TestVRAMAnchor_WeightsTermBartowski_NoOverhead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// weightsBytes: bartowski's published Qwen3-8B GGUF file size
		// (SI GB * 1e9, rounded to the published 2 decimals).
		weightsBytes int64
		// apxmlSoftBandGB: DOCUMENTED soft cross-check only, NOT asserted.
		// apxml weights = P*(Q/8)*1.2 with P~=8.19e9 -> our figure is ~their /1.2.
		apxmlSoftBandGB string
	}{
		{
			// bartowski Qwen3-8B Q4_K_M = 5.03 GB.
			// apxml (P*4.5/8*1.2) ~= 5.53 GB — ours is ~10% lower (no overhead).
			name:            "qwen3-8b Q4_K_M",
			weightsBytes:    5_030_000_000,
			apxmlSoftBandGB: "~5.5 GB apxml (×1.2 overhead) vs 5.03 GB ours",
		},
		{
			// bartowski Qwen3-8B Q8_0 = 8.71 GB.
			// apxml (P*8/8*1.2) ~= 9.83 GB — ours is ~13% lower (no overhead).
			name:            "qwen3-8b Q8_0",
			weightsBytes:    8_710_000_000,
			apxmlSoftBandGB: "~9.8 GB apxml (×1.2 overhead) vs 8.71 GB ours",
		},
		{
			// bartowski Qwen3-8B BF16 = 16.39 GB.
			// apxml (P*16/8*1.2) ~= 19.66 GB — ours is ~20% lower (no overhead).
			name:            "qwen3-8b BF16",
			weightsBytes:    16_390_000_000,
			apxmlSoftBandGB: "~19.7 GB apxml (×1.2 overhead) vs 16.39 GB ours",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// All arch facts zero -> weights-only lower bound == weights exactly.
			got := bestiary.EstimateVRAMBytes(tt.weightsBytes, 0, 0, 0, 0)
			if got != tt.weightsBytes {
				t.Errorf("weights term for %s = %d, want %d exactly "+
					"(overhead added? bestiary uses NO overhead constant; "+
					"apxml soft band for reference: %s)",
					tt.name, got, tt.weightsBytes, tt.apxmlSoftBandGB)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Both terms together: full-method composition anchor
// ----------------------------------------------------------------------------

// TestVRAMAnchor_FullMethodComposition ties the two independently-sourced terms
// into one total on a single real (model, quant, context) tuple, validating that
// they compose ADDITIVELY with no overhead:
//
//	total = bartowski weights (Term 2)  +  hand-computed LMCache KV (Term 1)
//
// A weights-inflation mutation (e.g. an apxml-style ×1.2 on weights) would make
// the returned total exceed weights+KV and fail here; a KV-coefficient mutation
// diverges from the hand literal and fails. Neither side of the total is
// code-computed by any bestiary formula, so this is not a tautology.
func TestVRAMAnchor_FullMethodComposition(t *testing.T) {
	t.Parallel()

	// Anchor: Qwen3-8B, Q8_0, at 8K context.
	//   Weights (bartowski Q8_0)              = 8,710,000,000  (published 8.71 GB)
	//   KV (LMCache: 2*36*8*128*8192*2)       = 1,207,959,552  (hand-computed)
	//   total                                 = 9,917,959,552
	// apxml soft band (documented, not asserted): weights ~9.8 GB + same KV ~=
	//   ~11.0 GB total — higher than ours purely because of apxml's ×1.2 overhead.
	const (
		weights   int64 = 8_710_000_000
		ctx             = 8192
		layers          = 36
		kvHeads         = 8
		headDim         = 128
		wantTotal int64 = 9_917_959_552
	)

	got := bestiary.EstimateVRAMBytes(weights, ctx, layers, kvHeads, headDim)
	if got != wantTotal {
		t.Errorf("qwen3-8b Q8_0 @ 8K total = %d, want %d "+
			"(bartowski weights %d + hand-computed LMCache KV 1,207,959,552; "+
			"no overhead by design, VRAMFormulaVersion 2)",
			got, wantTotal, weights)
	}
}
