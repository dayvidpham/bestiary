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
//	[0]=QUANT [1]=WEIGHTS [2]=VRAM [3]=TYP(4K) [4]=CTX [5]=PARTIAL
//
// so a test can make a precise per-column assertion (e.g. TYP(4K) == "—") rather
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
// TYP figure at 4K is a genuine weights+KV estimate strictly below VRAMBytes
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

// TestWriteQuantRows_TypHeaders pins that the quant table carries the single TYP
// column. Falsifier: dropping the column header.
func TestWriteQuantRows_TypHeaders(t *testing.T) {
	var buf bytes.Buffer
	writeQuantRows(&buf, []bestiary.QuantVRAM{fullArchRow()})
	out := buf.String()
	if !strings.Contains(out, "TYP(4K)") {
		t.Errorf("writeQuantRows header missing %q column; got:\n%s", "TYP(4K)", out)
	}
	if strings.Contains(out, "TYP(8K)") {
		t.Errorf("writeQuantRows header still carries dropped TYP(8K) column; got:\n%s", out)
	}
}

// TestWriteQuantRows_TypValues_FullArch pins that the TYP column renders
// (QuantVRAM).EstimateVRAM at 4096 for a full-arch row — a genuine weights+KV
// figure, above WeightsBytes and below the max-context VRAMBytes.
func TestWriteQuantRows_TypValues_FullArch(t *testing.T) {
	q := fullArchRow()
	want4k := q.EstimateVRAM(4096)

	// Guard the fixture: 4K sits between weights-only and the baked max-ctx VRAM.
	if !(q.WeightsBytes < want4k && want4k < q.VRAMBytes) {
		t.Fatalf("fixture invariant broken: weights=%d typ4k=%d vram=%d",
			q.WeightsBytes, want4k, q.VRAMBytes)
	}

	rows := quantDataRowFields(t, []bestiary.QuantVRAM{q})
	if len(rows) != 1 {
		t.Fatalf("want 1 data row, got %d", len(rows))
	}
	if got := rows[0][3]; got != strconv.FormatInt(want4k, 10) {
		t.Errorf("TYP(4K) = %q, want %d (EstimateVRAM(4096))", got, want4k)
	}
}

// TestWriteQuantRows_TypPartial_WeightsOnly pins the partial-row contract: a
// PARTIAL row (arch facts absent) renders a weights-only TYP figure that NEVER
// implies a KV delta. EstimateVRAM contributes no KV term without arch facts, so
// the TYP cell equals WeightsBytes. Falsifier: any implementation that fabricates
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
}

// TestWriteQuantRows_TypClamp_Below4K pins the clamp for a model whose max context
// is below 4096: the TYP column renders the clamp glyph.
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
}

// TestWriteQuantRows_TypClamp_BoundaryExact4K pins the inclusive boundary at 4096:
// a max context of exactly 4096 serves TYP(4K) (clamp is strict `<`, not `<=`).
func TestWriteQuantRows_TypClamp_BoundaryExact4K(t *testing.T) {
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
}

// TestWriteQuantRows_TypUnknownContext_Clamped pins that a row with an unknown max
// context (VRAMContextTokens == 0, e.g. neither curated nor upstream supplied one)
// renders the clamp glyph in the TYP column: without a known ceiling we cannot
// claim the model serves 4K tokens.
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
}

// TestRun_Providers_TypColumns_EndToEnd exercises the production CLI render path
// (`providers --output=table`) for the curated full-arch entity and asserts the
// TYP column carries the exact EstimateVRAM figure derived from the registry's own
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

	if !strings.Contains(out, "TYP(4K)") {
		t.Errorf("providers table missing %q header; got:\n%s", "TYP(4K)", out)
	}
	// The sample row's max context (131072) exceeds the TYP context, so the figure
	// renders (no clamp). Assert the exact recomputed value appears.
	want4k := strconv.FormatInt(sample.EstimateVRAM(4096), 10)
	if !strings.Contains(out, want4k) {
		t.Errorf("providers table missing TYP(4K) value %s for %s; got:\n%s", want4k, sample.Quant.String(), out)
	}
}
