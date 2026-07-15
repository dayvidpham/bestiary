package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// ----------------------------------------------------------------------------
// EstimateVRAMBytes — exact-value table tests
// ----------------------------------------------------------------------------

// TestEstimateVRAMBytes_ExactValues verifies the formula against literal
// expected values computed by hand (not re-derived via the same formula).
// Any change to a formula coefficient (e.g. VRAMKVElemBytes 2→1, factor 2
// removed) must fail at least one row. Each case's provenance.ref carries the
// hand-computed arithmetic derivation verbatim.
func TestEstimateVRAMBytes_ExactValues(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[vramExactInput, int64](t, vramEstimateExactValuesCorpusJSON, 12)
	requireCoverage(t, corpus, map[vramExactInput]int64{
		{WeightsBytes: 43_000_000_000, ContextTokens: 131072, Layers: 80, KVHeads: 8, HeadDim: 128}: 85_949_672_960,
		{WeightsBytes: 1_500_000_000, ContextTokens: 4096, Layers: 2, KVHeads: 2, HeadDim: 64}:      1_504_194_304,
	})
	runVRAMEstimateExactValuesCorpus(t, corpus)
}

// TestEstimateVRAMBytes_NoOverhead asserts that EstimateVRAMBytes(W, 0, 0, 0,
// 0) == W exactly.  A reintroduced overhead constant must fail this test.
func TestEstimateVRAMBytes_NoOverhead(t *testing.T) {
	t.Parallel()

	const weights int64 = 43_000_000_000
	got := bestiary.EstimateVRAMBytes(weights, 0, 0, 0, 0)
	if got != weights {
		t.Errorf("EstimateVRAMBytes(%d, 0, 0, 0, 0) = %d; want exactly %d (overhead constant introduced?)",
			weights, got, weights)
	}
}

// ----------------------------------------------------------------------------
// (QuantVRAM).EstimateVRAM — recompute parity + context sensitivity
// ----------------------------------------------------------------------------

// TestQuantVRAM_EstimateVRAM_Parity verifies that (QuantVRAM).EstimateVRAM
// produces the same result as calling EstimateVRAMBytes with the struct fields.
func TestQuantVRAM_EstimateVRAM_Parity(t *testing.T) {
	t.Parallel()

	q := bestiary.QuantVRAM{
		WeightsBytes: 43_000_000_000,
		Layers:       80,
		KVHeads:      8,
		HeadDim:      128,
	}
	const ctx = 131072

	want := bestiary.EstimateVRAMBytes(q.WeightsBytes, ctx, q.Layers, q.KVHeads, q.HeadDim)
	got := q.EstimateVRAM(ctx)
	if got != want {
		t.Errorf("QuantVRAM.EstimateVRAM(%d) = %d, want %d (EstimateVRAMBytes parity broken)", ctx, got, want)
	}
}

// TestQuantVRAM_EstimateVRAM_ContextSensitive verifies that changing contextTokens
// produces a different KV contribution, confirming ctx flows through.
func TestQuantVRAM_EstimateVRAM_ContextSensitive(t *testing.T) {
	t.Parallel()

	q := bestiary.QuantVRAM{
		WeightsBytes: 43_000_000_000,
		Layers:       80,
		KVHeads:      8,
		HeadDim:      128,
	}

	at128k := q.EstimateVRAM(131072)
	at4k := q.EstimateVRAM(4096)

	if at128k <= at4k {
		t.Errorf("EstimateVRAM(131072)=%d should be > EstimateVRAM(4096)=%d (context not flowing through KV formula)",
			at128k, at4k)
	}
}

// TestQuantVRAM_EstimateVRAM_WeightsOnly verifies that when arch facts are
// absent (zero), EstimateVRAM returns exactly WeightsBytes regardless of ctx.
func TestQuantVRAM_EstimateVRAM_WeightsOnly(t *testing.T) {
	t.Parallel()

	q := bestiary.QuantVRAM{
		WeightsBytes: 43_000_000_000,
		// Layers, KVHeads, HeadDim intentionally zero
	}

	got := q.EstimateVRAM(131072)
	if got != q.WeightsBytes {
		t.Errorf("EstimateVRAM with zero arch facts = %d, want exactly %d (weights-only lower bound)",
			got, q.WeightsBytes)
	}
}

// ----------------------------------------------------------------------------
// VRAMEstimateIsPartial — truth table
// ----------------------------------------------------------------------------

// TestVRAMEstimateIsPartial_TruthTable verifies the partial predicate for
// all single-absent and all-present combinations.
func TestVRAMEstimateIsPartial_TruthTable(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[vramPartialInput, bool](t, vramPartialTruthTableCorpusJSON, 11)
	requireCoverage(t, corpus, map[vramPartialInput]bool{
		{Layers: 80, KVHeads: 8, HeadDim: 128}: false,
		{Layers: 0, KVHeads: 0, HeadDim: 0}:    true,
	})
	runVRAMPartialTruthTableCorpus(t, corpus)
}

// ----------------------------------------------------------------------------
// Mutation-resistance: coefficient spot-checks
// ----------------------------------------------------------------------------

// TestEstimateVRAMBytes_KVCoefficients pins the exact KV contribution for
// a fully-specified row, ensuring neither the leading factor-of-2 (for both
// the K and V matrices) nor VRAMKVElemBytes (fp16 = 2 bytes) can be silently
// changed.
func TestEstimateVRAMBytes_KVCoefficients(t *testing.T) {
	t.Parallel()

	// Pure KV case: zero weights, full arch.
	// KV = 2 * 1 * 1 * 1 * 1 * 2 = 4  (layers=1, kvHeads=1, headDim=1, ctx=1)
	got := bestiary.EstimateVRAMBytes(0, 1, 1, 1, 1)
	const wantKVUnit int64 = 4 // 2 * VRAMKVElemBytes = 2 * 2
	if got != wantKVUnit {
		t.Errorf("unit KV (all dims=1): got %d, want %d — VRAMKVElemBytes or leading factor changed", got, wantKVUnit)
	}
}

// TestVRAMFormulaVersion pins the exported formula version constant so an
// accidental bump is caught.
func TestVRAMFormulaVersion(t *testing.T) {
	t.Parallel()

	if bestiary.VRAMFormulaVersion != 2 {
		t.Errorf("VRAMFormulaVersion = %d, want 2", bestiary.VRAMFormulaVersion)
	}
}

// TestVRAMKVElemBytes pins the element-size constant (fp16 = 2 bytes).
func TestVRAMKVElemBytes(t *testing.T) {
	t.Parallel()

	if bestiary.VRAMKVElemBytes != 2 {
		t.Errorf("VRAMKVElemBytes = %d, want 2", bestiary.VRAMKVElemBytes)
	}
}
