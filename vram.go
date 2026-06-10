package bestiary

// VRAMKVElemBytes is the number of bytes per element in the KV cache.
// The KV cache is stored in fp16 (half-precision float), so each element
// occupies 2 bytes.
const VRAMKVElemBytes = 2

// VRAMFormulaVersion identifies the VRAM estimation formula in use.
// Version 2 uses ingested GGUF file size as the weights term (never derived
// from bits-per-weight) and fp16 KV cache with no overhead constant.
const VRAMFormulaVersion = 2

// EstimateVRAMBytes estimates the total VRAM requirement in bytes for a model
// given its weights footprint and architectural parameters.
//
// Formula:
//
//	total = weightsBytes + KV
//	KV    = 2 * layers * kvHeads * headDim * contextTokens * VRAMKVElemBytes
//
// The KV term accounts for both the K and V matrices (hence the leading 2),
// fp16 element size (VRAMKVElemBytes = 2), and is GQA-aware: kvHeads is the
// KV-head count, not the query-head count.
//
// The weights term is always the ingested GGUF file size passed in as
// weightsBytes — it is the ground-truth measurement, never derived from
// bits-per-weight arithmetic.
//
// KV = 0 (weights-only lower bound) when ANY of layers, kvHeads, headDim, or
// contextTokens is <= 0. When baking a QuantVRAM row, the caller sets
// VRAMEstimatePartial = VRAMEstimateIsPartial(layers, kvHeads, headDim).
// Note: a zero-context bake yields KV=0 WITHOUT partial — partial is reserved
// for structurally absent arch-facts; the caller chooses its bake context
// independently. See VRAMEstimateIsPartial for the exact predicate.
//
// Integer arithmetic is int64 throughout. The maximum realistic KV cache
// (e.g. 200 layers × 128 heads × 256 dim × 2 M context × 2 bytes ≈ 52 TB)
// is well within int64 range (~9.2 EB), so no overflow protection is needed
// for any plausible model at the time of writing.
func EstimateVRAMBytes(weightsBytes int64, contextTokens, layers, kvHeads, headDim int) int64 {
	if layers <= 0 || kvHeads <= 0 || headDim <= 0 || contextTokens <= 0 {
		return weightsBytes
	}
	kv := int64(2) * int64(layers) * int64(kvHeads) * int64(headDim) * int64(contextTokens) * VRAMKVElemBytes
	return weightsBytes + kv
}

// EstimateVRAM recomputes the total VRAM estimate from the arch-facts stored
// in the QuantVRAM row at a caller-supplied context length.
//
// This is a pure recompute: it calls EstimateVRAMBytes(q.WeightsBytes,
// contextTokens, q.Layers, q.KVHeads, q.HeadDim) with no additional state.
// It does NOT read or modify q.VRAMBytes, q.VRAMContextTokens, or
// q.VRAMEstimatePartial — those fields capture the baked-at-model-max snapshot;
// this method is for runtime recomputation at a different context length.
func (q QuantVRAM) EstimateVRAM(contextTokens int) int64 {
	return EstimateVRAMBytes(q.WeightsBytes, contextTokens, q.Layers, q.KVHeads, q.HeadDim)
}

// VRAMEstimateIsPartial reports whether the KV-cache term would be excluded
// from the VRAM estimate because one or more arch-facts are absent (zero).
//
// Caller contract: when baking a QuantVRAM row, the caller (codegen) sets
// VRAMEstimatePartial = VRAMEstimateIsPartial(layers, kvHeads, headDim) on the
// row. VRAMEstimatePartial = true means VRAMBytes is a weights-only lower bound
// and the KV-cache was NOT included. The contextTokens argument is NOT checked
// here because a zero context with valid arch-facts is a legitimate caller
// choice (weights-only result by intent); the partial flag is reserved for the
// case where arch-facts are structurally absent from the source data.
func VRAMEstimateIsPartial(layers, kvHeads, headDim int) bool {
	return layers <= 0 || kvHeads <= 0 || headDim <= 0
}
