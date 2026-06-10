package bestiary

import (
	"strings"
	"testing"
)

// TestParseQuantVRAMTable_RejectsUnknownQuant: a curated row with a quant token
// that does not match any named Quantization constant must be rejected at load.
func TestParseQuantVRAMTable_RejectsUnknownQuant(t *testing.T) {
	const bad = `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"notareal","weights_bytes":1000}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted unknown quant token; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unknown quant") {
		t.Errorf("error = %q, want it to mention unknown quant", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsQuantizationOther: "other" is a valid wire
// name for UnmarshalText but must be rejected in curated data — it signals a
// curation gap, not a lossless escape.
func TestParseQuantVRAMTable_RejectsQuantizationOther(t *testing.T) {
	const bad = `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"other","weights_bytes":1000}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted quant=\"other\" in curated data; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "other") {
		t.Errorf("error = %q, want it to mention QuantizationOther / \"other\"", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsZeroWeightsBytes: weights_bytes must be > 0.
func TestParseQuantVRAMTable_RejectsZeroWeightsBytes(t *testing.T) {
	const bad = `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":0}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted weights_bytes=0; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "weights_bytes") {
		t.Errorf("error = %q, want it to mention weights_bytes", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsNegativeWeightsBytes: weights_bytes < 0 must
// also be rejected.
func TestParseQuantVRAMTable_RejectsNegativeWeightsBytes(t *testing.T) {
	const bad = `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":-1}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted negative weights_bytes; want rejection")
	}
}

// TestParseQuantVRAMTable_RejectsDuplicateModelID: two entries sharing a
// model_id (case-insensitive) must be rejected.
func TestParseQuantVRAMTable_RejectsDuplicateModelID(t *testing.T) {
	const bad = `{"schema_version":1,"models":[` +
		`{"model_id":"dup","param_size":"7b","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000}]},` +
		`{"model_id":"dup","param_size":"7b","source":"ollama","rows":[{"quant":"q8_0","weights_bytes":2000}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted duplicate model_id; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		t.Errorf("error = %q, want it to mention duplicate", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsMalformedParamSize: a param_size that does not
// pass ParseParamSize must be rejected at load.
func TestParseQuantVRAMTable_RejectsMalformedParamSize(t *testing.T) {
	const bad = `{"schema_version":1,"models":[{"model_id":"x","param_size":"notasize","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000}]}]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted malformed param_size; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "param_size") {
		t.Errorf("error = %q, want it to mention param_size", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsMalformedJSON: syntactically invalid JSON must
// be rejected with an error mentioning unmarshal.
func TestParseQuantVRAMTable_RejectsMalformedJSON(t *testing.T) {
	_, err := parseQuantVRAMTable([]byte(`}{not valid`))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted malformed JSON; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "unmarshal") {
		t.Errorf("error = %q, want it to mention unmarshal", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsUnknownSchemaVersion: a schema_version not in
// the known set must be rejected with an actionable error.
func TestParseQuantVRAMTable_RejectsUnknownSchemaVersion(t *testing.T) {
	const bad = `{"schema_version":999,"models":[]}`
	_, err := parseQuantVRAMTable([]byte(bad))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted unknown schema_version; want rejection")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "schema_version") {
		t.Errorf("error = %q, want it to mention schema_version", err.Error())
	}
}

// TestParseQuantVRAMTable_RejectsUnknownSource: a source field that is not a
// known DataSourceID must be rejected.
func TestParseQuantVRAMTable_RejectsUnknownSource(t *testing.T) {
	for _, badSrc := range []string{"", "huggingface", "unsloth"} {
		bad := `{"schema_version":1,"models":[{"model_id":"x","param_size":"7b","source":"` + badSrc + `","rows":[{"quant":"q4_k_m","weights_bytes":1000}]}]}`
		_, err := parseQuantVRAMTable([]byte(bad))
		if err == nil {
			t.Errorf("parseQuantVRAMTable accepted unknown source %q; want rejection", badSrc)
		}
	}
}

// TestParseQuantVRAMTable_RejectsNegativeArchFacts: negative layers, kv_heads,
// or head_dim values must be rejected — they are physically nonsensical.
func TestParseQuantVRAMTable_RejectsNegativeArchFacts(t *testing.T) {
	cases := []struct {
		name string
		json string
	}{
		{
			name: "negative_layers",
			json: `{"schema_version":1,"models":[{"model_id":"x","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000,"layers":-1}]}]}`,
		},
		{
			name: "negative_kv_heads",
			json: `{"schema_version":1,"models":[{"model_id":"x","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000,"kv_heads":-1}]}]}`,
		},
		{
			name: "negative_head_dim",
			json: `{"schema_version":1,"models":[{"model_id":"x","source":"ollama","rows":[{"quant":"q4_k_m","weights_bytes":1000,"head_dim":-1}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQuantVRAMTable([]byte(tc.json))
			if err == nil {
				t.Fatalf("parseQuantVRAMTable accepted %s; want rejection", tc.name)
			}
		})
	}
}

// TestParseQuantVRAMTable_QuantCaseInsensitive: quant tokens in the JSON are
// matched case-insensitively. A token written as "Q4_K_M" (uppercase, as Ollama
// file_type values are mixed-case) must resolve to QuantQ4_K_M, not be rejected.
func TestParseQuantVRAMTable_QuantCaseInsensitive(t *testing.T) {
	const input = `{"schema_version":1,"models":[{"model_id":"x","source":"ollama","rows":[{"quant":"Q4_K_M","weights_bytes":1000}]}]}`
	tbl, err := parseQuantVRAMTable([]byte(input))
	if err != nil {
		t.Fatalf("parseQuantVRAMTable rejected uppercase quant token Q4_K_M: %v", err)
	}
	rows := tbl.rows["x"]
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Quant != QuantQ4_K_M {
		t.Errorf("Quant = %v, want QuantQ4_K_M", rows[0].Quant)
	}
	// QuantRaw must be the verbatim token from the file, preserving the casing.
	if rows[0].QuantRaw != "Q4_K_M" {
		t.Errorf("QuantRaw = %q, want %q (verbatim token from file)", rows[0].QuantRaw, "Q4_K_M")
	}
}

// TestEmbeddedQuantVRAMTable_Valid: the shipped curated file loads and validates
// cleanly — the production-data counterpart of the negative tests above.
func TestEmbeddedQuantVRAMTable_Valid(t *testing.T) {
	if err := ValidateQuantVRAMTable(); err != nil {
		t.Fatalf("ValidateQuantVRAMTable failed on the embedded quant_vram.json: %v", err)
	}
}

// TestSafeQuantVRAMTable_DegradesToEmpty: when the table fails to load (parse
// error) or is nil, safeQuantVRAMTable must fall back to a non-nil EMPTY table
// so all lookups return zero values — nil/""/ DataSourceNone — and never panic.
// Mirrors TestSafeLineageTable_DegradesToNoLineage exactly.
func TestSafeQuantVRAMTable_DegradesToEmpty(t *testing.T) {
	badTable, err := parseQuantVRAMTable([]byte("}{ not valid json"))
	if err == nil {
		t.Fatal("parseQuantVRAMTable accepted malformed JSON; expected a load error to drive the degrade path")
	}

	for _, tc := range []struct {
		name  string
		table *quantVRAMTable
		err   error
	}{
		{"load error", badTable, err},
		{"nil table, nil error", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeQuantVRAMTable(tc.table, tc.err)
			if got == nil {
				t.Fatal("safeQuantVRAMTable returned nil; the degrade fallback must be non-nil")
			}
			// All lookups must return zero values on the degraded table.
			if rows := got.rows["llama3.3:70b-instruct"]; rows != nil {
				t.Errorf("degraded rows lookup returned %v, want nil", rows)
			}
			if ps := got.paramSize["llama3.3:70b-instruct"]; ps != "" {
				t.Errorf("degraded paramSize lookup returned %q, want empty", ps)
			}
			if src, ok := got.source["llama3.3:70b-instruct"]; ok || src != DataSourceNone {
				t.Errorf("degraded source lookup returned (%q, %v), want (\"\", false)", src, ok)
			}
			if cw := got.contextWindow["llama3.3:70b-instruct"]; cw != 0 {
				t.Errorf("degraded contextWindow lookup returned %d, want 0", cw)
			}
			if br := got.baseRef["llama3.3:70b-instruct"]; br != "" {
				t.Errorf("degraded baseRef lookup returned %q, want empty", br)
			}
		})
	}
}
