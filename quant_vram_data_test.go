package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_Absent (VC5): a model ID that exists only in models.dev (no
// Ollama row) must return nil — no error, no panic. This is the graceful-degrade
// contract: the loader returns nil for a miss, never an error to the caller.
// ----------------------------------------------------------------------------

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

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_Llama33_70b (VC1): present model with arch facts. Verifies
// exact weights_bytes for Q4_K_M and Q8_0 from seed, Quant parsed correctly,
// Layers/KVHeads/HeadDim populated, and QuantRaw set appropriately.
// VRAMBytes/VRAMContextTokens/VRAMEstimatePartial are NOT checked here — they
// are computed by the codegen caller (EstimateVRAMBytes), not the loader.
// ----------------------------------------------------------------------------

func TestQuantVRAMFor_Llama33_70b(t *testing.T) {
	const id bestiary.ModelID = "llama3.3:70b-instruct"

	rows := bestiary.QuantVRAMFor(id)
	if len(rows) == 0 {
		t.Fatalf("QuantVRAMFor(%q) = nil, want %d rows", id, 3)
	}
	if len(rows) != 3 {
		t.Fatalf("QuantVRAMFor(%q): got %d rows, want 3", id, len(rows))
	}

	// Build a quick index for order-independent checks.
	byQuant := make(map[bestiary.Quantization]bestiary.QuantVRAM, len(rows))
	for _, r := range rows {
		byQuant[r.Quant] = r
	}

	// Q4_K_M row
	q4km, ok := byQuant[bestiary.QuantQ4_K_M]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no Q4_K_M row", id)
	}
	if q4km.WeightsBytes != 43033509888 {
		t.Errorf("Q4_K_M WeightsBytes = %d, want 43033509888 (~42.5 GB)", q4km.WeightsBytes)
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

	// Q8_0 row
	q8, ok := byQuant[bestiary.QuantQ8_0]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no Q8_0 row", id)
	}
	if q8.WeightsBytes != 75176521728 {
		t.Errorf("Q8_0 WeightsBytes = %d, want 75176521728 (~75 GB)", q8.WeightsBytes)
	}

	// F16 row
	f16, ok := byQuant[bestiary.QuantF16]
	if !ok {
		t.Fatalf("QuantVRAMFor(%q): no F16 row", id)
	}
	if f16.WeightsBytes != 141166166016 {
		t.Errorf("F16 WeightsBytes = %d, want 141166166016 (~141 GB)", f16.WeightsBytes)
	}
}

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_SmallModel (VC2): small model llama3.2:3b-instruct.
// Arch facts absent — exercises the partial path at the row level.
// ----------------------------------------------------------------------------

func TestQuantVRAMFor_SmallModel(t *testing.T) {
	const id bestiary.ModelID = "llama3.2:3b-instruct"

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

	// ParamSize round-trip
	ps := bestiary.ParamSizeFor(id)
	if ps != "3b" {
		t.Errorf("ParamSizeFor(%q) = %q, want %q", id, ps, "3b")
	}
}

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_Finetune: community finetune with base_ref. The loader must
// return rows for it (base_ref is stored in the JSON but does not affect QuantVRAMFor).
// ----------------------------------------------------------------------------

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
}

// ----------------------------------------------------------------------------
// TestParamSizeFor_Present / TestSourceFor_Present: hit + miss for both funcs.
// ----------------------------------------------------------------------------

func TestParamSizeFor_Present(t *testing.T) {
	cases := []struct {
		id   bestiary.ModelID
		want string
	}{
		{"llama3.3:70b-instruct", "70b"},
		{"llama3.2:3b-instruct", "3b"},
		{"qwen2.5:0.5b-instruct", "0.5b"},
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
		{"llama3.3:70b-instruct", bestiary.DataSourceOllama},
		{"llama3.2:3b-instruct", bestiary.DataSourceOllama},
		{"ollama/dracarys2-llama-3-70b-instruct", bestiary.DataSourceOllama},
	}
	for _, tc := range cases {
		got := bestiary.SourceFor(tc.id)
		if got != tc.want {
			t.Errorf("SourceFor(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

// ----------------------------------------------------------------------------
// TestValidateQuantVRAMTable_Green: the shipped file must pass validation.
// ----------------------------------------------------------------------------

func TestValidateQuantVRAMTable_Green(t *testing.T) {
	if err := bestiary.ValidateQuantVRAMTable(); err != nil {
		t.Fatalf("ValidateQuantVRAMTable() returned error on the shipped file: %v", err)
	}
}

// ----------------------------------------------------------------------------
// TestValidateQuantVRAMTable_RejectsBadInput: exercises bad-table rejection via
// the exported parseAndValidateQuantVRAMBytes seam (mirrors lineage test pattern).
// Each case exercises a distinct validation rule.
// ----------------------------------------------------------------------------

func TestValidateQuantVRAMTable_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "unknown_quant",
			json:    `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"notareal","weights_bytes":1000}]}]}`,
			wantErr: "unknown quant",
		},
		{
			name:    "zero_weights_bytes",
			json:    `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":0}]}]}`,
			wantErr: "weights_bytes",
		},
		{
			name:    "negative_weights_bytes",
			json:    `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":-1}]}]}`,
			wantErr: "weights_bytes",
		},
		{
			name:    "duplicate_model_id",
			json:    `{"schema_version":1,"models":[{"model_id":"dup","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000}]},{"model_id":"dup","param_size":"7b","source":"ollama","rows":[{"quant":"q8_0","weights_bytes":2000}]}]}`,
			wantErr: "duplicate",
		},
		{
			name:    "malformed_param_size",
			json:    `{"schema_version":1,"models":[{"model_id":"x","param_size":"notasize","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000}]}]}`,
			wantErr: "param_size",
		},
		{
			name:    "malformed_json",
			json:    `}{not valid`,
			wantErr: "unmarshal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := bestiary.ParseAndValidateQuantVRAMBytes([]byte(tc.json))
			if err == nil {
				t.Fatalf("ParseAndValidateQuantVRAMBytes accepted bad input (%s), want error containing %q", tc.name, tc.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.wantErr)) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_NoPanic: calling all exported funcs with the empty model ID
// must never panic (defensive against zero-value usage).
// ----------------------------------------------------------------------------

func TestQuantVRAMFor_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("QuantVRAMFor(\"\") panicked: %v", r)
		}
	}()
	_ = bestiary.QuantVRAMFor("")
	_ = bestiary.ParamSizeFor("")
	_ = bestiary.SourceFor("")
}

// ----------------------------------------------------------------------------
// TestQuantVRAMFor_QunatNotOther: every row loaded from the shipped file must
// not be QuantizationOther — the JSON curates known quant strings and validation
// must reject unknown tokens before they reach the table.
// ----------------------------------------------------------------------------

func TestQuantVRAMFor_QuantNotOther(t *testing.T) {
	knownIDs := []bestiary.ModelID{
		"llama3.3:70b-instruct",
		"llama3.2:3b-instruct",
		"qwen2.5:0.5b-instruct",
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
