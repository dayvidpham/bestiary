package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// These tests assert against the production typClampGlyph constant (defined in
// main.go) so the em-dash clamp glyph has a single source of truth.

// quantDataRowFields renders rows via the production writeQuantRows formatter and
// returns each data row's whitespace-separated fields (the QUANT header line and
// any leading blank lines are dropped). The field layout is:
//
//	[0]=QUANT [1]=WEIGHTS [2]=VRAM [3]=TYP(4K) [4]=TYP(8K) [5]=CTX [6]=PARTIAL
//
// so a test can make a precise per-column assertion (e.g. TYP(8K) == "—") rather
// than a fragile substring match over the whole line.
func quantDataRowFields(t *testing.T, rows []bestiary.QuantVRAM) [][]string {
	t.Helper()
	var buf bytes.Buffer
	writeQuantRows(&buf, rows)
	var out [][]string
	for _, line := range strings.Split(buf.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "QUANT" { // header
			continue
		}
		out = append(out, fields)
	}
	return out
}

// fullArchRow is a synthetic QuantVRAM row with complete architecture facts baked
// at a 131072-token maximum context (mirrors the curated llama-3.3-70b rows). Its
// TYP figures at 4K/8K are genuine weights+KV estimates strictly below VRAMBytes
// (which is baked at the larger 131072 context).
func fullArchRow() bestiary.QuantVRAM {
	q := bestiary.QuantVRAM{
		Quant:             bestiary.QuantQ4_K_M,
		QuantRaw:          "Q4_K_M",
		WeightsBytes:      43033509888,
		VRAMContextTokens: 131072,
		Layers:            80,
		KVHeads:           8,
		HeadDim:           128,
	}
	q.VRAMBytes = q.EstimateVRAM(q.VRAMContextTokens)
	return q
}

// TestWriteQuantRows_TypHeaders pins that the quant table gained the two TYP
// columns. Falsifier: dropping either column header.
func TestWriteQuantRows_TypHeaders(t *testing.T) {
	var buf bytes.Buffer
	writeQuantRows(&buf, []bestiary.QuantVRAM{fullArchRow()})
	out := buf.String()
	for _, want := range []string{"TYP(4K)", "TYP(8K)"} {
		if !strings.Contains(out, want) {
			t.Errorf("writeQuantRows header missing %q column; got:\n%s", want, out)
		}
	}
}

// TestWriteQuantRows_TypValues_FullArch pins that the TYP columns render
// (QuantVRAM).EstimateVRAM at 4096 and 8192 for a full-arch row — a genuine
// weights+KV figure, above WeightsBytes and below the max-context VRAMBytes.
func TestWriteQuantRows_TypValues_FullArch(t *testing.T) {
	q := fullArchRow()
	want4k := q.EstimateVRAM(4096)
	want8k := q.EstimateVRAM(8192)

	// Guard the fixture: 4K < 8K < baked VRAM, and both exceed weights-only.
	if !(q.WeightsBytes < want4k && want4k < want8k && want8k < q.VRAMBytes) {
		t.Fatalf("fixture invariant broken: weights=%d typ4k=%d typ8k=%d vram=%d",
			q.WeightsBytes, want4k, want8k, q.VRAMBytes)
	}

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got := rows[0][3]; got != strconv.FormatInt(want4k, 10) {
		t.Errorf("TYP(4K) = %q, want %d (EstimateVRAM(4096))", got, want4k)
	}
	if got := rows[0][4]; got != strconv.FormatInt(want8k, 10) {
		t.Errorf("TYP(8K) = %q, want %d (EstimateVRAM(8192))", got, want8k)
	}
}

// TestWriteQuantRows_TypPartial_WeightsOnly pins the partial-row contract: a
// PARTIAL row (arch facts absent) renders a weights-only TYP figure that NEVER
// implies a KV delta. EstimateVRAM contributes no KV term without arch facts, so
// both TYP cells equal WeightsBytes. Falsifier: any implementation that fabricates
// a KV delta for a partial row would push TYP above WeightsBytes.
func TestWriteQuantRows_TypPartial_WeightsOnly(t *testing.T) {
	q := bestiary.QuantVRAM{
		Quant:               bestiary.QuantQ4_K_M,
		QuantRaw:            "Q4_K_M",
		WeightsBytes:        2019139072,
		VRAMBytes:           2019139072, // baked partial == weights-only
		VRAMContextTokens:   131072,     // max ctx large enough to not clamp
		VRAMEstimatePartial: true,
	}
	weights := strconv.FormatInt(q.WeightsBytes, 10)

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got := rows[0][3]; got != weights {
		t.Errorf("partial TYP(4K) = %q, want weights-only %s (no phantom KV delta)", got, weights)
	}
	if got := rows[0][4]; got != weights {
		t.Errorf("partial TYP(8K) = %q, want weights-only %s (no phantom KV delta)", got, weights)
	}
}

// TestWriteQuantRows_TypClamp_Below8K pins the clamp: a model whose max context is
// exactly 4096 serves TYP(4K) but NOT TYP(8K) (8192 > 4096), so TYP(8K) is the
// clamp glyph while TYP(4K) still shows a figure. The 4096 boundary is inclusive
// (clamp is strict `<`).
func TestWriteQuantRows_TypClamp_Below8K(t *testing.T) {
	q := fullArchRow()
	q.VRAMContextTokens = 4096
	q.VRAMBytes = q.EstimateVRAM(4096)

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got, want := rows[0][3], strconv.FormatInt(q.EstimateVRAM(4096), 10); got != want {
		t.Errorf("TYP(4K) at max ctx 4096 = %q, want %s (4096 boundary is inclusive)", got, want)
	}
	if got := rows[0][4]; got != typClampGlyph {
		t.Errorf("TYP(8K) at max ctx 4096 = %q, want clamp %q (max ctx < 8192)", got, typClampGlyph)
	}
}

// TestWriteQuantRows_TypClamp_Below4K pins the clamp for a model whose max context
// is below 4096: BOTH TYP columns render the clamp glyph.
func TestWriteQuantRows_TypClamp_Below4K(t *testing.T) {
	q := fullArchRow()
	q.VRAMContextTokens = 2048
	q.VRAMBytes = q.EstimateVRAM(2048)

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got := rows[0][3]; got != typClampGlyph {
		t.Errorf("TYP(4K) at max ctx 2048 = %q, want clamp %q", got, typClampGlyph)
	}
	if got := rows[0][4]; got != typClampGlyph {
		t.Errorf("TYP(8K) at max ctx 2048 = %q, want clamp %q", got, typClampGlyph)
	}
}

// TestWriteQuantRows_TypClamp_BoundaryExact8K pins the inclusive boundary at 8192:
// a max context of exactly 8192 serves TYP(8K) (clamp is strict `<`, not `<=`).
func TestWriteQuantRows_TypClamp_BoundaryExact8K(t *testing.T) {
	q := fullArchRow()
	q.VRAMContextTokens = 8192
	q.VRAMBytes = q.EstimateVRAM(8192)

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got, want := rows[0][4], strconv.FormatInt(q.EstimateVRAM(8192), 10); got != want {
		t.Errorf("TYP(8K) at max ctx 8192 = %q, want %s (8192 boundary is inclusive)", got, want)
	}
}

// TestWriteQuantRows_TypUnknownContext_Clamped pins that a row with an unknown max
// context (VRAMContextTokens == 0, e.g. neither curated nor upstream supplied one)
// renders the clamp glyph in both TYP columns: without a known ceiling we cannot
// claim the model serves 4K or 8K tokens.
func TestWriteQuantRows_TypUnknownContext_Clamped(t *testing.T) {
	q := fullArchRow()
	q.VRAMContextTokens = 0
	q.VRAMBytes = q.WeightsBytes // weights-only when bake ctx is 0

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got := rows[0][3]; got != typClampGlyph {
		t.Errorf("TYP(4K) at unknown max ctx = %q, want clamp %q", got, typClampGlyph)
	}
	if got := rows[0][4]; got != typClampGlyph {
		t.Errorf("TYP(8K) at unknown max ctx = %q, want clamp %q", got, typClampGlyph)
	}
}

// TestRun_Providers_TypColumns_EndToEnd exercises the production CLI render path
// (`providers --output=table`) for the curated full-arch entity and asserts the
// TYP columns carry the exact EstimateVRAM figures derived from the registry's own
// QuantVRAM rows. This is the integration proof that writeQuantRows is wired into
// the command output; the unit tests above pin the clamp/partial edge behavior.
func TestRun_Providers_TypColumns_EndToEnd(t *testing.T) {
	ent := requireSizedEntity(t)

	var sample bestiary.QuantVRAM
	found := false
	for _, in := range ent.Instances {
		if len(in.QuantVRAM) > 0 {
			sample = in.QuantVRAM[0]
			found = true
			break
		}
	}
	if !found {
		t.Skip("sized entity carries no QuantVRAM rows")
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", "--output=table", sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run providers --output=table %q: %v", sizedCuratedKey, runErr)
	}

	for _, want := range []string{"TYP(4K)", "TYP(8K)"} {
		if !strings.Contains(out, want) {
			t.Errorf("providers table missing %q header; got:\n%s", want, out)
		}
	}
	// The sample row's max context (131072) exceeds both TYP contexts, so both
	// figures render (no clamp). Assert the exact recomputed values appear.
	want4k := strconv.FormatInt(sample.EstimateVRAM(4096), 10)
	want8k := strconv.FormatInt(sample.EstimateVRAM(8192), 10)
	if !strings.Contains(out, want4k) {
		t.Errorf("providers table missing TYP(4K) value %s for %s; got:\n%s", want4k, sample.Quant.String(), out)
	}
	if !strings.Contains(out, want8k) {
		t.Errorf("providers table missing TYP(8K) value %s for %s; got:\n%s", want8k, sample.Quant.String(), out)
	}
}
