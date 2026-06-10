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
// removed) must fail at least one row.
func TestEstimateVRAMBytes_ExactValues(t *testing.T) {
	t.Parallel()

	// Hand-computed expected values.
	//
	// Formula: KV = 2 * layers * kvHeads * headDim * contextTokens * 2
	//          total = weightsBytes + KV
	//
	// Row derivations:
	//   llama-3.3-70b class (layers=80, kvHeads=8, headDim=128, ctx=131072):
	//     KV = 2 * 80 * 8 * 128 * 131072 * 2 = 42,949,672,960
	//
	//   small model (layers=2, kvHeads=2, headDim=64, ctx=4096):
	//     KV = 2 * 2 * 2 * 64 * 4096 * 2 = 4,194,304
	tests := []struct {
		name          string
		weightsBytes  int64
		contextTokens int
		layers        int
		kvHeads       int
		headDim       int
		wantTotal     int64
	}{
		{
			name:          "llama-3.3-70b-class at 128K context",
			weightsBytes:  43_000_000_000,
			contextTokens: 131072,
			layers:        80,
			kvHeads:       8,
			headDim:       128,
			// 43,000,000,000 + 42,949,672,960 = 85,949,672,960
			wantTotal: 85_949_672_960,
		},
		{
			name:          "small model at 4K context",
			weightsBytes:  1_500_000_000,
			contextTokens: 4096,
			layers:        2,
			kvHeads:       2,
			headDim:       64,
			// 1,500,000,000 + 4,194,304 = 1,504,194,304
			wantTotal: 1_504_194_304,
		},
		// Weights-only lower-bound rows: KV term must be zero when ANY arch fact is 0.
		{
			name:          "weights-only: layers zero",
			weightsBytes:  5_000_000_000,
			contextTokens: 131072,
			layers:        0,
			kvHeads:       8,
			headDim:       128,
			wantTotal:     5_000_000_000,
		},
		{
			name:          "weights-only: kvHeads zero",
			weightsBytes:  5_000_000_000,
			contextTokens: 131072,
			layers:        80,
			kvHeads:       0,
			headDim:       128,
			wantTotal:     5_000_000_000,
		},
		{
			name:          "weights-only: headDim zero",
			weightsBytes:  5_000_000_000,
			contextTokens: 131072,
			layers:        80,
			kvHeads:       8,
			headDim:       0,
			wantTotal:     5_000_000_000,
		},
		{
			name:          "weights-only: contextTokens zero",
			weightsBytes:  5_000_000_000,
			contextTokens: 0,
			layers:        80,
			kvHeads:       8,
			headDim:       128,
			wantTotal:     5_000_000_000,
		},
		{
			name:          "all-zero arch facts with nonzero weights",
			weightsBytes:  5_000_000_000,
			contextTokens: 0,
			layers:        0,
			kvHeads:       0,
			headDim:       0,
			wantTotal:     5_000_000_000,
		},
		{
			name:          "all-zero including weights",
			weightsBytes:  0,
			contextTokens: 0,
			layers:        0,
			kvHeads:       0,
			headDim:       0,
			wantTotal:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.EstimateVRAMBytes(tt.weightsBytes, tt.contextTokens, tt.layers, tt.kvHeads, tt.headDim)
			if got != tt.wantTotal {
				t.Errorf("EstimateVRAMBytes(%d, %d, %d, %d, %d) = %d, want %d",
					tt.weightsBytes, tt.contextTokens, tt.layers, tt.kvHeads, tt.headDim, got, tt.wantTotal)
			}
		})
	}
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

	tests := []struct {
		name    string
		layers  int
		kvHeads int
		headDim int
		want    bool
	}{
		// All present — not partial
		{name: "all present", layers: 80, kvHeads: 8, headDim: 128, want: false},
		// Each fact absent individually — partial
		{name: "layers absent", layers: 0, kvHeads: 8, headDim: 128, want: true},
		{name: "kvHeads absent", layers: 80, kvHeads: 0, headDim: 128, want: true},
		{name: "headDim absent", layers: 80, kvHeads: 8, headDim: 0, want: true},
		// All absent — partial
		{name: "all absent", layers: 0, kvHeads: 0, headDim: 0, want: true},
		// Two absent — partial
		{name: "layers+kvHeads absent", layers: 0, kvHeads: 0, headDim: 128, want: true},
		{name: "layers+headDim absent", layers: 0, kvHeads: 8, headDim: 0, want: true},
		{name: "kvHeads+headDim absent", layers: 80, kvHeads: 0, headDim: 0, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.VRAMEstimateIsPartial(tt.layers, tt.kvHeads, tt.headDim)
			if got != tt.want {
				t.Errorf("VRAMEstimateIsPartial(%d, %d, %d) = %v, want %v",
					tt.layers, tt.kvHeads, tt.headDim, got, tt.want)
			}
		})
	}
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
