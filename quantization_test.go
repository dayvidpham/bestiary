package bestiary_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestQuantization_IotaOrder verifies that the iota assignments are exactly as
// specified in the ratified contract (IP-1).  Any reordering would break the
// wire format for existing serialised data.
func TestQuantization_IotaOrder(t *testing.T) {
	tests := []struct {
		q    bestiary.Quantization
		want int
		name string
	}{
		{bestiary.QuantizationNone, 0, "QuantizationNone"},
		{bestiary.QuantF16, 1, "QuantF16"},
		{bestiary.QuantBF16, 2, "QuantBF16"},
		{bestiary.QuantF32, 3, "QuantF32"},
		{bestiary.QuantQ4_0, 4, "QuantQ4_0"},
		{bestiary.QuantQ4_1, 5, "QuantQ4_1"},
		{bestiary.QuantQ5_0, 6, "QuantQ5_0"},
		{bestiary.QuantQ5_1, 7, "QuantQ5_1"},
		{bestiary.QuantQ8_0, 8, "QuantQ8_0"},
		{bestiary.QuantQ2_K, 9, "QuantQ2_K"},
		{bestiary.QuantQ2_K_S, 10, "QuantQ2_K_S"},
		{bestiary.QuantQ3_K_S, 11, "QuantQ3_K_S"},
		{bestiary.QuantQ3_K_M, 12, "QuantQ3_K_M"},
		{bestiary.QuantQ3_K_L, 13, "QuantQ3_K_L"},
		{bestiary.QuantQ4_K_S, 14, "QuantQ4_K_S"},
		{bestiary.QuantQ4_K_M, 15, "QuantQ4_K_M"},
		{bestiary.QuantQ5_K_S, 16, "QuantQ5_K_S"},
		{bestiary.QuantQ5_K_M, 17, "QuantQ5_K_M"},
		{bestiary.QuantQ6_K, 18, "QuantQ6_K"},
		{bestiary.QuantIQ1_S, 19, "QuantIQ1_S"},
		{bestiary.QuantIQ1_M, 20, "QuantIQ1_M"},
		{bestiary.QuantIQ2_XXS, 21, "QuantIQ2_XXS"},
		{bestiary.QuantIQ2_XS, 22, "QuantIQ2_XS"},
		{bestiary.QuantIQ2_S, 23, "QuantIQ2_S"},
		{bestiary.QuantIQ2_M, 24, "QuantIQ2_M"},
		{bestiary.QuantIQ3_XXS, 25, "QuantIQ3_XXS"},
		{bestiary.QuantIQ3_XS, 26, "QuantIQ3_XS"},
		{bestiary.QuantIQ3_S, 27, "QuantIQ3_S"},
		{bestiary.QuantIQ3_M, 28, "QuantIQ3_M"},
		{bestiary.QuantIQ4_XS, 29, "QuantIQ4_XS"},
		{bestiary.QuantIQ4_NL, 30, "QuantIQ4_NL"},
		{bestiary.QuantAWQ, 31, "QuantAWQ"},
		{bestiary.QuantGPTQ, 32, "QuantGPTQ"},
		{bestiary.QuantInt8, 33, "QuantInt8"},
		{bestiary.QuantInt4, 34, "QuantInt4"},
		{bestiary.QuantizationOther, 35, "QuantizationOther"},
	}
	for _, tt := range tests {
		if int(tt.q) != tt.want {
			t.Errorf("%s = %d, want %d", tt.name, int(tt.q), tt.want)
		}
	}
}

// TestQuantization_String verifies String() for known members and out-of-range values.
func TestQuantization_String(t *testing.T) {
	tests := []struct {
		q    bestiary.Quantization
		want string
	}{
		{bestiary.QuantizationNone, "none"},
		{bestiary.QuantF16, "f16"},
		{bestiary.QuantBF16, "bf16"},
		{bestiary.QuantF32, "f32"},
		{bestiary.QuantQ4_0, "q4_0"},
		{bestiary.QuantQ4_1, "q4_1"},
		{bestiary.QuantQ5_0, "q5_0"},
		{bestiary.QuantQ5_1, "q5_1"},
		{bestiary.QuantQ8_0, "q8_0"},
		{bestiary.QuantQ2_K, "q2_k"},
		{bestiary.QuantQ2_K_S, "q2_k_s"},
		{bestiary.QuantQ3_K_S, "q3_k_s"},
		{bestiary.QuantQ3_K_M, "q3_k_m"},
		{bestiary.QuantQ3_K_L, "q3_k_l"},
		{bestiary.QuantQ4_K_S, "q4_k_s"},
		{bestiary.QuantQ4_K_M, "q4_k_m"},
		{bestiary.QuantQ5_K_S, "q5_k_s"},
		{bestiary.QuantQ5_K_M, "q5_k_m"},
		{bestiary.QuantQ6_K, "q6_k"},
		{bestiary.QuantIQ1_S, "iq1_s"},
		{bestiary.QuantIQ1_M, "iq1_m"},
		{bestiary.QuantIQ2_XXS, "iq2_xxs"},
		{bestiary.QuantIQ2_XS, "iq2_xs"},
		{bestiary.QuantIQ2_S, "iq2_s"},
		{bestiary.QuantIQ2_M, "iq2_m"},
		{bestiary.QuantIQ3_XXS, "iq3_xxs"},
		{bestiary.QuantIQ3_XS, "iq3_xs"},
		{bestiary.QuantIQ3_S, "iq3_s"},
		{bestiary.QuantIQ3_M, "iq3_m"},
		{bestiary.QuantIQ4_XS, "iq4_xs"},
		{bestiary.QuantIQ4_NL, "iq4_nl"},
		{bestiary.QuantAWQ, "awq"},
		{bestiary.QuantGPTQ, "gptq"},
		{bestiary.QuantInt8, "int8"},
		{bestiary.QuantInt4, "int4"},
		{bestiary.QuantizationOther, "other"},
		// Out-of-range renders without panic.
		{bestiary.Quantization(9999), "quantization(9999)"},
		{bestiary.Quantization(-1), "quantization(-1)"},
	}
	for _, tt := range tests {
		got := tt.q.String()
		if got != tt.want {
			t.Errorf("Quantization(%d).String() = %q, want %q", int(tt.q), got, tt.want)
		}
	}
}

// TestQuantization_IsKnown verifies IsKnown() for all named members and
// out-of-range values.
func TestQuantization_IsKnown(t *testing.T) {
	// All named members should be known.
	known := []bestiary.Quantization{
		bestiary.QuantizationNone, bestiary.QuantF16, bestiary.QuantBF16, bestiary.QuantF32,
		bestiary.QuantQ4_0, bestiary.QuantQ4_1, bestiary.QuantQ5_0, bestiary.QuantQ5_1, bestiary.QuantQ8_0,
		bestiary.QuantQ2_K, bestiary.QuantQ2_K_S, bestiary.QuantQ3_K_S, bestiary.QuantQ3_K_M, bestiary.QuantQ3_K_L,
		bestiary.QuantQ4_K_S, bestiary.QuantQ4_K_M, bestiary.QuantQ5_K_S, bestiary.QuantQ5_K_M, bestiary.QuantQ6_K,
		bestiary.QuantIQ1_S, bestiary.QuantIQ1_M, bestiary.QuantIQ2_XXS, bestiary.QuantIQ2_XS,
		bestiary.QuantIQ2_S, bestiary.QuantIQ2_M, bestiary.QuantIQ3_XXS, bestiary.QuantIQ3_XS,
		bestiary.QuantIQ3_S, bestiary.QuantIQ3_M, bestiary.QuantIQ4_XS, bestiary.QuantIQ4_NL,
		bestiary.QuantAWQ, bestiary.QuantGPTQ, bestiary.QuantInt8, bestiary.QuantInt4,
		bestiary.QuantizationOther,
	}
	for _, q := range known {
		if !q.IsKnown() {
			t.Errorf("Quantization(%d).IsKnown() = false, want true", int(q))
		}
	}
	// Out-of-range should be unknown.
	if bestiary.Quantization(9999).IsKnown() {
		t.Error("Quantization(9999).IsKnown() = true, want false")
	}
	if bestiary.Quantization(-1).IsKnown() {
		t.Error("Quantization(-1).IsKnown() = true, want false")
	}
}

// TestQuantization_BitsPerWeight (VC-(c)) verifies spot-check bpw values
// against the authoritative llama.cpp README table.
func TestQuantization_BitsPerWeight(t *testing.T) {
	tests := []struct {
		q    bestiary.Quantization
		want float64
		name string
	}{
		{bestiary.QuantizationNone, 0, "None"},
		{bestiary.QuantF16, 16.0, "F16"},
		{bestiary.QuantBF16, 16.0, "BF16"},
		{bestiary.QuantF32, 32.0, "F32"},
		// Legacy
		{bestiary.QuantQ4_0, 4.5, "Q4_0"},
		{bestiary.QuantQ4_1, 5.0, "Q4_1"},
		{bestiary.QuantQ5_0, 5.5, "Q5_0"},
		{bestiary.QuantQ5_1, 6.0, "Q5_1"},
		{bestiary.QuantQ8_0, 8.50, "Q8_0"},
		// K-quants
		{bestiary.QuantQ2_K, 3.16, "Q2_K"},
		{bestiary.QuantQ2_K_S, 2.97, "Q2_K_S"},
		{bestiary.QuantQ3_K_S, 3.64, "Q3_K_S"},
		{bestiary.QuantQ3_K_M, 4.00, "Q3_K_M"},
		{bestiary.QuantQ3_K_L, 4.30, "Q3_K_L"},
		{bestiary.QuantQ4_K_S, 4.67, "Q4_K_S"},
		{bestiary.QuantQ4_K_M, 4.89, "Q4_K_M"},
		{bestiary.QuantQ5_K_S, 5.57, "Q5_K_S"},
		{bestiary.QuantQ5_K_M, 5.70, "Q5_K_M"},
		{bestiary.QuantQ6_K, 6.56, "Q6_K"},
		// I-quants
		{bestiary.QuantIQ1_S, 2.00, "IQ1_S"},
		{bestiary.QuantIQ1_M, 2.15, "IQ1_M"},
		{bestiary.QuantIQ2_XXS, 2.38, "IQ2_XXS"},
		{bestiary.QuantIQ2_XS, 2.59, "IQ2_XS"},
		{bestiary.QuantIQ2_S, 2.74, "IQ2_S"},
		{bestiary.QuantIQ2_M, 2.93, "IQ2_M"},
		{bestiary.QuantIQ3_XXS, 3.25, "IQ3_XXS"},
		{bestiary.QuantIQ3_XS, 3.50, "IQ3_XS"},
		{bestiary.QuantIQ3_S, 3.66, "IQ3_S"},
		{bestiary.QuantIQ3_M, 3.76, "IQ3_M"},
		{bestiary.QuantIQ4_XS, 4.46, "IQ4_XS"},
		{bestiary.QuantIQ4_NL, 4.68, "IQ4_NL"},
		// Reserved (unknown until ingest)
		{bestiary.QuantAWQ, 0, "AWQ"},
		{bestiary.QuantGPTQ, 0, "GPTQ"},
		{bestiary.QuantInt8, 0, "Int8"},
		{bestiary.QuantInt4, 0, "Int4"},
		// Other / out-of-range
		{bestiary.QuantizationOther, 0, "Other"},
		{bestiary.Quantization(9999), 0, "out-of-range"},
	}
	for _, tt := range tests {
		got := tt.q.BitsPerWeight()
		if got != tt.want {
			t.Errorf("%s BitsPerWeight() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestQuantization_MarshalText verifies that MarshalText emits the expected
// wire name for every named member and returns an error for out-of-range values.
func TestQuantization_MarshalText(t *testing.T) {
	// All named members must marshal without error.
	members := []struct {
		q    bestiary.Quantization
		want string
	}{
		{bestiary.QuantizationNone, "none"},
		{bestiary.QuantF16, "f16"},
		{bestiary.QuantBF16, "bf16"},
		{bestiary.QuantF32, "f32"},
		{bestiary.QuantQ4_0, "q4_0"},
		{bestiary.QuantQ4_1, "q4_1"},
		{bestiary.QuantQ5_0, "q5_0"},
		{bestiary.QuantQ5_1, "q5_1"},
		{bestiary.QuantQ8_0, "q8_0"},
		{bestiary.QuantQ2_K, "q2_k"},
		{bestiary.QuantQ2_K_S, "q2_k_s"},
		{bestiary.QuantQ3_K_S, "q3_k_s"},
		{bestiary.QuantQ3_K_M, "q3_k_m"},
		{bestiary.QuantQ3_K_L, "q3_k_l"},
		{bestiary.QuantQ4_K_S, "q4_k_s"},
		{bestiary.QuantQ4_K_M, "q4_k_m"},
		{bestiary.QuantQ5_K_S, "q5_k_s"},
		{bestiary.QuantQ5_K_M, "q5_k_m"},
		{bestiary.QuantQ6_K, "q6_k"},
		{bestiary.QuantIQ1_S, "iq1_s"},
		{bestiary.QuantIQ1_M, "iq1_m"},
		{bestiary.QuantIQ2_XXS, "iq2_xxs"},
		{bestiary.QuantIQ2_XS, "iq2_xs"},
		{bestiary.QuantIQ2_S, "iq2_s"},
		{bestiary.QuantIQ2_M, "iq2_m"},
		{bestiary.QuantIQ3_XXS, "iq3_xxs"},
		{bestiary.QuantIQ3_XS, "iq3_xs"},
		{bestiary.QuantIQ3_S, "iq3_s"},
		{bestiary.QuantIQ3_M, "iq3_m"},
		{bestiary.QuantIQ4_XS, "iq4_xs"},
		{bestiary.QuantIQ4_NL, "iq4_nl"},
		{bestiary.QuantAWQ, "awq"},
		{bestiary.QuantGPTQ, "gptq"},
		{bestiary.QuantInt8, "int8"},
		{bestiary.QuantInt4, "int4"},
		{bestiary.QuantizationOther, "other"},
	}
	for _, tt := range members {
		got, err := tt.q.MarshalText()
		if err != nil {
			t.Errorf("Quantization(%d).MarshalText() error: %v", int(tt.q), err)
			continue
		}
		if string(got) != tt.want {
			t.Errorf("Quantization(%d).MarshalText() = %q, want %q", int(tt.q), string(got), tt.want)
		}
	}

	// Out-of-range must return an actionable error.
	for _, bad := range []bestiary.Quantization{bestiary.Quantization(-1), bestiary.Quantization(9999)} {
		_, err := bad.MarshalText()
		if err == nil {
			t.Errorf("Quantization(%d).MarshalText() = nil error, want error", int(bad))
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "out of range") {
			t.Errorf("Quantization(%d).MarshalText() error %q does not mention 'out of range'", int(bad), msg)
		}
		if !strings.Contains(msg, "how to fix") {
			t.Errorf("Quantization(%d).MarshalText() error %q lacks 'how to fix'", int(bad), msg)
		}
	}
}

// TestQuantization_UnmarshalText verifies that UnmarshalText performs a lossless
// round-trip for every named member, is case-insensitive, and returns an
// actionable error for unrecognised tokens.
func TestQuantization_UnmarshalText(t *testing.T) {
	// Lossless round-trip for every named member.
	allMembers := []bestiary.Quantization{
		bestiary.QuantizationNone, bestiary.QuantF16, bestiary.QuantBF16, bestiary.QuantF32,
		bestiary.QuantQ4_0, bestiary.QuantQ4_1, bestiary.QuantQ5_0, bestiary.QuantQ5_1, bestiary.QuantQ8_0,
		bestiary.QuantQ2_K, bestiary.QuantQ2_K_S, bestiary.QuantQ3_K_S, bestiary.QuantQ3_K_M, bestiary.QuantQ3_K_L,
		bestiary.QuantQ4_K_S, bestiary.QuantQ4_K_M, bestiary.QuantQ5_K_S, bestiary.QuantQ5_K_M, bestiary.QuantQ6_K,
		bestiary.QuantIQ1_S, bestiary.QuantIQ1_M, bestiary.QuantIQ2_XXS, bestiary.QuantIQ2_XS,
		bestiary.QuantIQ2_S, bestiary.QuantIQ2_M, bestiary.QuantIQ3_XXS, bestiary.QuantIQ3_XS,
		bestiary.QuantIQ3_S, bestiary.QuantIQ3_M, bestiary.QuantIQ4_XS, bestiary.QuantIQ4_NL,
		bestiary.QuantAWQ, bestiary.QuantGPTQ, bestiary.QuantInt8, bestiary.QuantInt4,
		bestiary.QuantizationOther,
	}
	for _, orig := range allMembers {
		wire, err := orig.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%d) error: %v", int(orig), err)
			continue
		}
		var got bestiary.Quantization
		if err := got.UnmarshalText(wire); err != nil {
			t.Errorf("UnmarshalText(%q) error: %v", string(wire), err)
			continue
		}
		if got != orig {
			t.Errorf("round-trip Quantization(%d): got %d, want %d", int(orig), int(got), int(orig))
		}
	}

	// Case-insensitive: Ollama uses mixed-case file_type values.
	caseSuccess := []struct {
		input string
		want  bestiary.Quantization
	}{
		{"Q4_K_M", bestiary.QuantQ4_K_M},
		{"Q8_0", bestiary.QuantQ8_0},
		{"F16", bestiary.QuantF16},
		{"IQ4_NL", bestiary.QuantIQ4_NL},
		{"AWQ", bestiary.QuantAWQ},
		{"OTHER", bestiary.QuantizationOther},
	}
	for _, tt := range caseSuccess {
		var got bestiary.Quantization
		if err := got.UnmarshalText([]byte(tt.input)); err != nil {
			t.Errorf("UnmarshalText(%q) case-insensitive error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("UnmarshalText(%q) = %d, want %d", tt.input, int(got), int(tt.want))
		}
	}

	// "fp16" is NOT a canonical wire name (canonical is "f16"); must error.
	var fp16q bestiary.Quantization
	if err := fp16q.UnmarshalText([]byte("FP16")); err == nil {
		t.Error("UnmarshalText(\"FP16\") = nil error, want error (fp16 is not a canonical wire name)")
	}

	// Unknown token must return an actionable error.
	var q bestiary.Quantization
	err := q.UnmarshalText([]byte("completely_unknown_format"))
	if err == nil {
		t.Error("UnmarshalText(unknown) = nil error, want error")
	} else {
		msg := err.Error()
		if !strings.Contains(msg, "how to fix") {
			t.Errorf("UnmarshalText error %q lacks 'how to fix'", msg)
		}
		if !strings.Contains(msg, "completely_unknown_format") {
			t.Errorf("UnmarshalText error %q does not echo the bad input", msg)
		}
	}
}

// TestQuantization_JSONRoundTrip verifies that Quantization marshals as a JSON
// string (not integer) and round-trips correctly via encoding/json.
func TestQuantization_JSONRoundTrip(t *testing.T) {
	type wrapper struct {
		Q bestiary.Quantization `json:"q"`
	}
	tests := []struct {
		q        bestiary.Quantization
		wantJSON string
	}{
		{bestiary.QuantizationNone, `{"q":"none"}`},
		{bestiary.QuantQ4_K_M, `{"q":"q4_k_m"}`},
		{bestiary.QuantizationOther, `{"q":"other"}`},
	}
	for _, tt := range tests {
		b, err := json.Marshal(wrapper{Q: tt.q})
		if err != nil {
			t.Errorf("json.Marshal(Q=%d) error: %v", int(tt.q), err)
			continue
		}
		if string(b) != tt.wantJSON {
			t.Errorf("json.Marshal(Q=%d) = %s, want %s", int(tt.q), string(b), tt.wantJSON)
		}
		var got wrapper
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) error: %v", string(b), err)
			continue
		}
		if got.Q != tt.q {
			t.Errorf("JSON round-trip Q=%d: got %d", int(tt.q), int(got.Q))
		}
	}
}

// TestParseQuantization (VC-(A)) verifies ParseQuantization with known inputs,
// empty string, case-insensitive matching, and unknown tokens.
func TestParseQuantization(t *testing.T) {
	// Known values, exact case.
	known := []struct {
		input string
		want  bestiary.Quantization
	}{
		{"none", bestiary.QuantizationNone},
		{"f16", bestiary.QuantF16},
		{"bf16", bestiary.QuantBF16},
		{"f32", bestiary.QuantF32},
		{"q4_0", bestiary.QuantQ4_0},
		{"q8_0", bestiary.QuantQ8_0},
		{"q4_k_m", bestiary.QuantQ4_K_M},
		{"q5_k_m", bestiary.QuantQ5_K_M},
		{"iq4_nl", bestiary.QuantIQ4_NL},
		{"awq", bestiary.QuantAWQ},
		{"gptq", bestiary.QuantGPTQ},
		{"int8", bestiary.QuantInt8},
		{"int4", bestiary.QuantInt4},
		{"other", bestiary.QuantizationOther},
	}
	for _, tt := range known {
		got, err := bestiary.ParseQuantization(tt.input)
		if err != nil {
			t.Errorf("ParseQuantization(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseQuantization(%q) = %d, want %d", tt.input, int(got), int(tt.want))
		}
	}

	// Case-insensitive: upper-case inputs should resolve.
	caseInputs := []struct {
		input string
		want  bestiary.Quantization
	}{
		{"Q4_K_M", bestiary.QuantQ4_K_M},
		{"Q8_0", bestiary.QuantQ8_0},
		{"F16", bestiary.QuantF16},
		{"IQ4_NL", bestiary.QuantIQ4_NL},
		{"AWQ", bestiary.QuantAWQ},
	}
	for _, tt := range caseInputs {
		got, err := bestiary.ParseQuantization(tt.input)
		if err != nil {
			t.Errorf("ParseQuantization(%q) case-insensitive error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseQuantization(%q) = %d, want %d", tt.input, int(got), int(tt.want))
		}
	}

	// Empty string -> (None, nil).
	got, err := bestiary.ParseQuantization("")
	if err != nil {
		t.Errorf("ParseQuantization(\"\") error: %v", err)
	}
	if got != bestiary.QuantizationNone {
		t.Errorf("ParseQuantization(\"\") = %d, want QuantizationNone", int(got))
	}

	// Unknown non-empty -> (QuantizationNone, error).
	// NEVER silently maps to QuantizationOther.
	// Note: "int4" IS a valid (reserved) member — only truly unknown tokens error.
	unknown := []string{"fp16", "nf4", "UD-Q4_K_XL", "banana", "q4km"}
	for _, input := range unknown {
		got, err := bestiary.ParseQuantization(input)
		if err == nil {
			t.Errorf("ParseQuantization(%q) = nil error, want error for unknown input", input)
			continue
		}
		if got != bestiary.QuantizationNone {
			t.Errorf("ParseQuantization(%q) = %d, want QuantizationNone (not Other)", input, int(got))
		}
		msg := err.Error()
		// Error must be actionable: name what it got.
		if !strings.Contains(msg, input) {
			t.Errorf("ParseQuantization(%q) error %q does not mention the bad input", input, msg)
		}
		if !strings.Contains(msg, "how to fix") {
			t.Errorf("ParseQuantization(%q) error %q lacks 'how to fix'", input, msg)
		}
	}
}

// TestDetectQuantization_Known verifies DetectQuantization on real Ollama model
// IDs that are known to contain a quantization tag.
func TestDetectQuantization_Known(t *testing.T) {
	tests := []struct {
		id           bestiary.ModelID
		wantQ        bestiary.Quantization
		wantRaw      string
		wantStripped bestiary.ModelID
		name         string
	}{
		{
			id:           "llama3.3:70b-instruct-q4_K_M",
			wantQ:        bestiary.QuantQ4_K_M,
			wantRaw:      "q4_K_M",
			wantStripped: "llama3.3:70b-instruct",
			name:         "llama3.3 q4_K_M",
		},
		{
			id:           "qwen2.5:0.5b-instruct-q8_0",
			wantQ:        bestiary.QuantQ8_0,
			wantRaw:      "q8_0",
			wantStripped: "qwen2.5:0.5b-instruct",
			name:         "qwen2.5 q8_0",
		},
		{
			id:           "llama3.3:70b-instruct-f16",
			wantQ:        bestiary.QuantF16,
			wantRaw:      "f16",
			wantStripped: "llama3.3:70b-instruct",
			name:         "llama3.3 f16",
		},
		{
			id:           "mistral:7b-instruct-q5_K_M",
			wantQ:        bestiary.QuantQ5_K_M,
			wantRaw:      "q5_K_M",
			wantStripped: "mistral:7b-instruct",
			name:         "mistral q5_K_M",
		},
		{
			id:           "gemma2:9b-instruct-q2_K",
			wantQ:        bestiary.QuantQ2_K,
			wantRaw:      "q2_K",
			wantStripped: "gemma2:9b-instruct",
			name:         "gemma2 q2_K",
		},
		// Colon-free IDs (HF/GGUF-style) exercise the hadColon==false arm of
		// rebuildStrippedID.
		{
			id:           "llama-3-70b-q4_0",
			wantQ:        bestiary.QuantQ4_0,
			wantRaw:      "q4_0",
			wantStripped: "llama-3-70b",
			name:         "colon-free q4_0",
		},
	}
	for _, tt := range tests {
		q, raw, stripped := bestiary.DetectQuantization(tt.id)
		if q != tt.wantQ {
			t.Errorf("[%s] DetectQuantization: q = %d (%s), want %d (%s)",
				tt.name, int(q), q, int(tt.wantQ), tt.wantQ)
		}
		if raw != tt.wantRaw {
			t.Errorf("[%s] DetectQuantization: raw = %q, want %q", tt.name, raw, tt.wantRaw)
		}
		if stripped != tt.wantStripped {
			t.Errorf("[%s] DetectQuantization: stripped = %q, want %q", tt.name, stripped, tt.wantStripped)
		}
	}
}

// TestDetectQuantization_NoTag verifies that DetectQuantization returns
// (QuantizationNone, "", id) when the model ID carries no quantization tag.
func TestDetectQuantization_NoTag(t *testing.T) {
	tests := []struct {
		id bestiary.ModelID
	}{
		{"llama3.3:latest"},
		{"llama3.3:70b-instruct"},
		{"gpt-4o"},
		{"claude-opus-4-5-20251101"},
		{"mistral:7b"},
	}
	for _, tt := range tests {
		q, raw, stripped := bestiary.DetectQuantization(tt.id)
		if q != bestiary.QuantizationNone {
			t.Errorf("DetectQuantization(%q): q = %d, want QuantizationNone", tt.id, int(q))
		}
		if raw != "" {
			t.Errorf("DetectQuantization(%q): raw = %q, want \"\"", tt.id, raw)
		}
		if stripped != tt.id {
			t.Errorf("DetectQuantization(%q): stripped = %q, want id unchanged", tt.id, stripped)
		}
	}
}

// TestDetectQuantization_Unknown (VC4) verifies that an unknown-but-quant-looking
// tag results in (QuantizationOther, raw, stripped) without panicking.
// All three return values are pinned exactly to catch mutants that corrupt the
// raw case, truncate the raw, or mis-assemble the stripped ID.
func TestDetectQuantization_Unknown(t *testing.T) {
	tests := []struct {
		id           bestiary.ModelID
		wantQ        bestiary.Quantization
		wantRaw      string           // exact original-case raw tag
		wantStripped bestiary.ModelID // exact stripped ID
		name         string
	}{
		{
			id:           "somemodel:7b-instruct-q99_X",
			wantQ:        bestiary.QuantizationOther,
			wantRaw:      "q99_X",
			wantStripped: "somemodel:7b-instruct",
			name:         "unknown q-prefix tag",
		},
		{
			id:           "somemodel:7b-iq99_super",
			wantQ:        bestiary.QuantizationOther,
			wantRaw:      "iq99_super",
			wantStripped: "somemodel:7b",
			name:         "unknown iq-prefix tag",
		},
		{
			id:           "somemodel:7b-f99",
			wantQ:        bestiary.QuantizationOther,
			wantRaw:      "f99",
			wantStripped: "somemodel:7b",
			name:         "unknown f-prefix tag",
		},
		// Colon-free unknown-tag case exercises the hadColon==false arm of
		// rebuildStrippedID on the Other path.
		{
			id:           "somemodel-7b-q99_X",
			wantQ:        bestiary.QuantizationOther,
			wantRaw:      "q99_X",
			wantStripped: "somemodel-7b",
			name:         "colon-free unknown q-prefix tag",
		},
	}
	for _, tt := range tests {
		// Must not panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("[%s] DetectQuantization panicked: %v", tt.name, r)
				}
			}()
			q, raw, stripped := bestiary.DetectQuantization(tt.id)
			if q != tt.wantQ {
				t.Errorf("[%s] DetectQuantization: q = %d (%s), want QuantizationOther (%d)",
					tt.name, int(q), q, int(bestiary.QuantizationOther))
			}
			if raw != tt.wantRaw {
				t.Errorf("[%s] DetectQuantization: raw = %q, want %q", tt.name, raw, tt.wantRaw)
			}
			if stripped != tt.wantStripped {
				t.Errorf("[%s] DetectQuantization: stripped = %q, want %q", tt.name, stripped, tt.wantStripped)
			}
		}()
	}

	// Legitimate non-quant suffixes must NOT be stripped.
	noStrip := []bestiary.ModelID{
		"llama3.3:70b-instruct",
		"gpt-4o-mini",
		"claude-opus-4-5",
	}
	for _, id := range noStrip {
		q, _, _ := bestiary.DetectQuantization(id)
		if q == bestiary.QuantizationOther {
			t.Errorf("DetectQuantization(%q): got QuantizationOther, want not-Other (should not strip non-quant suffix)", id)
		}
	}

	// The sentinel name "other" must NOT be matched as a known tag; a model ID
	// ending in "-other" should pass through as (None, "", unchanged).
	otherSuffix := bestiary.ModelID("somemodel:7b-other")
	q, raw, stripped := bestiary.DetectQuantization(otherSuffix)
	if q != bestiary.QuantizationNone {
		t.Errorf("DetectQuantization(%q): q = %d (%s), want QuantizationNone (sentinel 'other' must not be matched as a quant tag)",
			otherSuffix, int(q), q)
	}
	if raw != "" {
		t.Errorf("DetectQuantization(%q): raw = %q, want \"\" (no quant tag in '-other' suffix)", otherSuffix, raw)
	}
	if stripped != otherSuffix {
		t.Errorf("DetectQuantization(%q): stripped = %q, want unchanged", otherSuffix, stripped)
	}
}

// TestDetectQuantization_NoPanic verifies that DetectQuantization never panics
// on edge-case inputs including empty string, only colon, only dash, etc.
func TestDetectQuantization_NoPanic(t *testing.T) {
	inputs := []bestiary.ModelID{
		"",
		":",
		"-",
		"a",
		"a:b",
		"a:-",
		":::",
		"model:tag-",
	}
	for _, id := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DetectQuantization(%q) panicked: %v", id, r)
				}
			}()
			bestiary.DetectQuantization(id)
		}()
	}
}

// TestDetectQuantization_FP16Alias documents the behavior for Ollama's "fp16"
// tag.  The canonical wire name for 16-bit float is "f16"; "fp16" is NOT in the
// quantNames table, so it is treated as an unknown-but-quant-looking tag and
// returned as QuantizationOther (not QuantF16).  Callers that need to normalise
// "fp16"→"f16" must do so before calling DetectQuantization.
func TestDetectQuantization_FP16Alias(t *testing.T) {
	id := bestiary.ModelID("llama3.3:70b-instruct-fp16")
	q, raw, stripped := bestiary.DetectQuantization(id)
	// fp16 is quant-like (f prefix + digit) so it must be detected as Other, not None.
	if q == bestiary.QuantizationNone && raw == "" {
		t.Errorf("DetectQuantization(%q): q=None raw=\"\", want QuantizationOther with raw=fp16", id)
	}
	if q != bestiary.QuantizationOther {
		t.Errorf("DetectQuantization(%q): q=%d (%s), want QuantizationOther", id, int(q), q)
	}
	if raw != "fp16" {
		t.Errorf("DetectQuantization(%q): raw=%q, want \"fp16\"", id, raw)
	}
	if stripped != "llama3.3:70b-instruct" {
		t.Errorf("DetectQuantization(%q): stripped=%q, want \"llama3.3:70b-instruct\"", id, stripped)
	}
}
