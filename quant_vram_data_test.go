package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestQuantVRAMFor_Absent: a model ID that exists only in models.dev (no
// Ollama row) must return nil — no error, no panic. The graceful-degrade
// contract: the loader returns nil for a miss, never panics.
func TestQuantVRAMFor_Absent(t *testing.T) {
	const absentID bestiary.ModelID = "claude-3-5-sonnet"
	rows := bestiary.QuantVRAMFor(absentID)
	if rows != nil {
		t.Errorf("QuantVRAMFor(%q) = %v, want nil for a model with no curated rows", absentID, rows)
	}
}

func TestParamSizeFor_Absent(t *testing.T) {
	const absentID bestiary.ModelID = "claude-3-5-sonnet"
	got := bestiary.ParamSizeFor(absentID)
	if got != "" {
		t.Errorf("ParamSizeFor(%q) = %q, want empty string for absent model", absentID, got)
	}
}

func TestSourceFor_Absent(t *testing.T) {
	const absentID bestiary.ModelID = "claude-3-5-sonnet"
	got := bestiary.SourceFor(absentID)
	if got != bestiary.DataSourceNone {
		t.Errorf("SourceFor(%q) = %q, want DataSourceNone for absent model", absentID, got)
	}
}

func TestContextWindowFor_Absent(t *testing.T) {
	const absentID bestiary.ModelID = "claude-3-5-sonnet"
	got := bestiary.ContextWindowFor(absentID)
	if got != 0 {
		t.Errorf("ContextWindowFor(%q) = %d, want 0 for absent model", absentID, got)
	}
}

func TestBaseRefFor_Absent(t *testing.T) {
	const absentID bestiary.ModelID = "claude-3-5-sonnet"
	got := bestiary.BaseRefFor(absentID)
	if got != "" {
		t.Errorf("BaseRefFor(%q) = %q, want empty string for absent model", absentID, got)
	}
}

// TestQuantVRAMFor_Llama33_70b: present model with arch facts. Verifies exact
// weights_bytes for each quant row from the seed file, Quant parsed correctly,
// Layers/KVHeads/HeadDim populated from the curated arch facts.
// VRAMBytes/VRAMContextTokens/VRAMEstimatePartial are not checked here — they
// are computed by the codegen caller (EstimateVRAMBytes), not the loader.
func TestQuantVRAMFor_Llama33_70b(t *testing.T) {
	const id bestiary.ModelID = "llama-3.3-70b-instruct"

	rows := bestiary.QuantVRAMFor(id)
	if len(rows) == 0 {
		t.Fatalf("QuantVRAMFor(%q) = nil, want 3 rows", id)
	}
	if len(rows) != 3 {
		t.Fatalf("QuantVRAMFor(%q): got %d rows, want 3", id, len(rows))
	}

	// Build a quick index for order-independent checks.
	byQuant := make(map[bestiary.Quantization]bestiary.QuantVRAM, len(rows))
	for _, r := range rows {
		byQuant[r.Quant] = r
	}

	// Q4_K_M row — ~42.5 GB, arch facts present.
	q4km, ok := byQuant[bestiary.QuantQ4_K_M]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no Q4_K_M row", id)
	}
	if q4km.WeightsBytes != 43033509888 {
		t.Errorf("Q4_K_M WeightsBytes = %d, want 43033509888", q4km.WeightsBytes)
	}
	if q4km.Layers != 80 {
		t.Errorf("Q4_K_M Layers = %d, want 80", q4km.Layers)
	}
	if q4km.KVHeads != 8 {
		t.Errorf("Q4_K_M KVHeads = %d, want 8", q4km.KVHeads)
	}
	if q4km.HeadDim != 128 {
		t.Errorf("Q4_K_M HeadDim = %d, want 128", q4km.HeadDim)
	}

	// Q8_0 row — ~75 GB.
	q8, ok := byQuant[bestiary.QuantQ8_0]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no Q8_0 row", id)
	}
	if q8.WeightsBytes != 75176521728 {
		t.Errorf("Q8_0 WeightsBytes = %d, want 75176521728", q8.WeightsBytes)
	}

	// F16 row — ~141 GB.
	f16, ok := byQuant[bestiary.QuantF16]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no F16 row", id)
	}
	if f16.WeightsBytes != 141166166016 {
		t.Errorf("F16 WeightsBytes = %d, want 141166166016", f16.WeightsBytes)
	}
}

// TestQuantVRAMFor_SmallModel: small 3B-parameter model with two quant rows.
// Arch facts are absent in the seed — exercises the partial-VRAM code path.
func TestQuantVRAMFor_SmallModel(t *testing.T) {
	const id bestiary.ModelID = "llama-3.2-3b-instruct"

	rows := bestiary.QuantVRAMFor(id)
	if len(rows) == 0 {
		t.Fatalf("QuantVRAMFor(%q) = nil, want rows", id)
	}
	if len(rows) != 2 {
		t.Fatalf("QuantVRAMFor(%q): got %d rows, want 2", id, len(rows))
	}

	for _, r := range rows {
		if r.WeightsBytes <= 0 {
			t.Errorf("row Quant=%s: WeightsBytes=%d, must be > 0", r.Quant, r.WeightsBytes)
		}
		// Arch facts absent: layers/kvheads/headdim must be zero.
		if r.Layers != 0 || r.KVHeads != 0 || r.HeadDim != 0 {
			t.Errorf("row Quant=%s: expected arch facts absent (all 0), got Layers=%d KVHeads=%d HeadDim=%d",
				r.Quant, r.Layers, r.KVHeads, r.HeadDim)
		}
	}

	// ParamSize round-trip.
	ps := bestiary.ParamSizeFor(id)
	if ps != "3b" {
		t.Errorf("ParamSizeFor(%q) = %q, want %q", id, ps, "3b")
	}
}

// TestQuantVRAMFor_Finetune: community finetune with base_ref. The loader must
// return rows and expose both base_ref and context_window via the codegen
// access functions.
func TestQuantVRAMFor_Finetune(t *testing.T) {
	const id bestiary.ModelID = "ollama/dracarys2-llama-3-70b-instruct"

	rows := bestiary.QuantVRAMFor(id)
	if len(rows) == 0 {
		t.Fatalf("QuantVRAMFor(%q) = nil, want rows for finetune entry", id)
	}
	if rows[0].WeightsBytes <= 0 {
		t.Errorf("finetune Q4_K_M WeightsBytes = %d, want > 0", rows[0].WeightsBytes)
	}
	// Arch facts present (copied from base for this entry).
	if rows[0].Layers == 0 {
		t.Errorf("finetune row: Layers = 0, want non-zero arch fact")
	}

	// base_ref must be accessible for codegen lineage inference.
	base := bestiary.BaseRefFor(id)
	if base == "" {
		t.Errorf("BaseRefFor(%q) = empty, want the curated base_ref", id)
	}

	// context_window must be accessible for codegen VRAM baking.
	ctx := bestiary.ContextWindowFor(id)
	if ctx == 0 {
		t.Errorf("ContextWindowFor(%q) = 0, want the curated context_window", id)
	}
}

// TestParamSizeFor_Present / TestSourceFor_Present: hit cases for the lookup
// functions, covering all seed models.
func TestParamSizeFor_Present(t *testing.T) {
	cases := []struct {
		id   bestiary.ModelID
		want string
	}{
		{"llama-3.3-70b-instruct", "70b"},
		{"llama-3.3-8b-instruct", "8b"},
		{"llama-3.2-3b-instruct", "3b"},
		{"ollama/dracarys2-llama-3-70b-instruct", "70b"},
	}
	for _, tc := range cases {
		got := bestiary.ParamSizeFor(tc.id)
		if got != tc.want {
			t.Errorf("ParamSizeFor(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestSourceFor_Present(t *testing.T) {
	cases := []struct {
		id   bestiary.ModelID
		want bestiary.DataSourceID
	}{
		{"llama-3.3-70b-instruct", bestiary.DataSourceOllama},
		{"llama-3.3-8b-instruct", bestiary.DataSourceOllama},
		{"llama-3.2-3b-instruct", bestiary.DataSourceOllama},
		{"ollama/dracarys2-llama-3-70b-instruct", bestiary.DataSourceOllama},
	}
	for _, tc := range cases {
		got := bestiary.SourceFor(tc.id)
		if got != tc.want {
			t.Errorf("SourceFor(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// TestContextWindowFor_Present: the curated context_window field is accessible
// for models that declare it.
func TestContextWindowFor_Present(t *testing.T) {
	cases := []struct {
		id   bestiary.ModelID
		want int
	}{
		{"llama-3.3-70b-instruct", 131072},
		{"llama-3.2-3b-instruct", 131072},
		{"ollama/dracarys2-llama-3-70b-instruct", 8192},
	}
	for _, tc := range cases {
		got := bestiary.ContextWindowFor(tc.id)
		if got != tc.want {
			t.Errorf("ContextWindowFor(%q) = %d, want %d", tc.id, got, tc.want)
		}
	}
}

// TestBaseRefFor_Present: the curated base_ref field is accessible for models
// that declare it.
func TestBaseRefFor_Present(t *testing.T) {
	got := bestiary.BaseRefFor("ollama/dracarys2-llama-3-70b-instruct")
	if got == "" {
		t.Errorf("BaseRefFor(dracarys2): got empty, want the curated base_ref")
	}
	// Non-finetune models must return empty.
	got2 := bestiary.BaseRefFor("llama-3.3-70b-instruct")
	if got2 != "" {
		t.Errorf("BaseRefFor(llama-3.3-70b-instruct base): got %q, want empty (not a finetune)", got2)
	}
}

// TestValidateQuantVRAMTable_Green: the shipped file must pass validation.
func TestValidateQuantVRAMTable_Green(t *testing.T) {
	if err := bestiary.ValidateQuantVRAMTable(); err != nil {
		t.Fatalf("ValidateQuantVRAMTable() returned error on the shipped file: %v", err)
	}
}

// TestQuantVRAMFor_BakeContractUnchanged: every row returned by QuantVRAMFor
// must have VRAMBytes==0, VRAMContextTokens==0, VRAMEstimatePartial==false.
// The loader does not compute these fields; that responsibility belongs to the
// codegen caller. An eager-computing loader mutant would fail this test.
func TestQuantVRAMFor_BakeContractUnchanged(t *testing.T) {
	ids := []bestiary.ModelID{
		"llama-3.3-70b-instruct",
		"llama-3.2-3b-instruct",
		"ollama/dracarys2-llama-3-70b-instruct",
	}
	for _, id := range ids {
		for _, r := range bestiary.QuantVRAMFor(id) {
			if r.VRAMBytes != 0 {
				t.Errorf("id=%q quant=%s: VRAMBytes=%d, want 0 (computed by codegen, not loader)",
					id, r.Quant, r.VRAMBytes)
			}
			if r.VRAMContextTokens != 0 {
				t.Errorf("id=%q quant=%s: VRAMContextTokens=%d, want 0 (computed by codegen)",
					id, r.Quant, r.VRAMContextTokens)
			}
			if r.VRAMEstimatePartial {
				t.Errorf("id=%q quant=%s: VRAMEstimatePartial=true, want false (set by codegen, not loader)",
					id, r.Quant)
			}
		}
	}
}

// TestQuantVRAMFor_NoPanic: calling all exported functions with the empty model
// ID must never panic (defensive against zero-value usage).
func TestQuantVRAMFor_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("exported functions panicked on empty ModelID: %v", r)
		}
	}()
	_ = bestiary.QuantVRAMFor("")
	_ = bestiary.ParamSizeFor("")
	_ = bestiary.SourceFor("")
	_ = bestiary.ContextWindowFor("")
	_ = bestiary.BaseRefFor("")
}

// TestQuantVRAMFor_QuantNotOther: every row loaded from the shipped file must
// not be QuantizationOther — the JSON curates known quant strings and validation
// must reject unknown tokens before they reach the table.
func TestQuantVRAMFor_QuantNotOther(t *testing.T) {
	knownIDs := []bestiary.ModelID{
		"llama-3.3-70b-instruct",
		"llama-3.2-3b-instruct",
		"ollama/dracarys2-llama-3-70b-instruct",
	}
	for _, id := range knownIDs {
		rows := bestiary.QuantVRAMFor(id)
		for _, r := range rows {
			if r.Quant == bestiary.QuantizationOther {
				t.Errorf("QuantVRAMFor(%q): row %v has Quant=QuantizationOther; curated data must use known quant strings", id, r)
			}
		}
	}
}

// TestLookup_CaseInsensitive: all five public lookup functions document
// case-insensitive matching. Verify it by querying with mixed-case IDs that
// differ in casing from the lowercased model_ids stored in the table.
func TestLookup_CaseInsensitive(t *testing.T) {
	// Mixed-case variant of a seed model (models.dev ID with different casing).
	const id bestiary.ModelID = "Llama-3.3-70B-Instruct"

	rows := bestiary.QuantVRAMFor(id)
	if len(rows) == 0 {
		t.Errorf("QuantVRAMFor(%q) = nil, want rows; lookup must be case-insensitive", id)
	}

	ps := bestiary.ParamSizeFor(id)
	if ps != "70b" {
		t.Errorf("ParamSizeFor(%q) = %q, want %q; lookup must be case-insensitive", id, ps, "70b")
	}

	src := bestiary.SourceFor(id)
	if src != bestiary.DataSourceOllama {
		t.Errorf("SourceFor(%q) = %q, want DataSourceOllama; lookup must be case-insensitive", id, src)
	}

	cw := bestiary.ContextWindowFor(id)
	if cw != 131072 {
		t.Errorf("ContextWindowFor(%q) = %d, want 131072; lookup must be case-insensitive", id, cw)
	}

	br := bestiary.BaseRefFor("Ollama/Dracarys2-Llama-3-70B-Instruct")
	if br == "" {
		t.Errorf("BaseRefFor (mixed-case finetune id) = empty, want non-empty; lookup must be case-insensitive")
	}
}

// TestQuantVRAMFor_QuantRawAlwaysPopulated: QuantRaw must be the verbatim
// curated token from the JSON file for every row, regardless of whether Quant
// is QuantizationOther. An empty QuantRaw on any loaded row is a contract
// violation — callers rely on it for lossless display and round-trip fidelity.
//
// Models with non-empty rows are checked; the 8b param-size-only entry has
// empty rows (QuantVRAMFor returns nil) so it is excluded from this check —
// the nil return is intentional and covered by TestParamSizeFor_Present.
func TestQuantVRAMFor_QuantRawAlwaysPopulated(t *testing.T) {
	ids := []bestiary.ModelID{
		"llama-3.3-70b-instruct",
		"llama-3.2-3b-instruct",
		"ollama/dracarys2-llama-3-70b-instruct",
	}
	for _, id := range ids {
		rows := bestiary.QuantVRAMFor(id)
		if len(rows) == 0 {
			t.Errorf("QuantVRAMFor(%q): expected rows, got nil", id)
			continue
		}
		for _, r := range rows {
			if r.QuantRaw == "" {
				t.Errorf("QuantVRAMFor(%q) quant=%s: QuantRaw is empty; must be the verbatim curated token", id, r.Quant)
			}
		}
	}
}
