package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestQuantization_IotaOrder verifies that the iota assignments are exactly as
// specified in the ratified contract (IP-1).  Any reordering would break the
// wire format for existing serialised data.
func TestQuantization_IotaOrder(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, int](t, quantIotaOrderCorpusJSON, 36)
	requireCoverage(t, corpus, map[string]int{
		"none":   0,
		"f16":    1,
		"q4_k_m": 15,
		"other":  35,
	})
	runQuantIotaOrderCorpus(t, corpus)
}

// TestQuantization_String verifies String() for known members and out-of-range values.
func TestQuantization_String(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[int, string](t, quantStringCorpusJSON, 38)
	requireCoverage(t, corpus, map[int]string{
		1:    "f16",
		9999: "quantization(9999)",
		-1:   "quantization(-1)",
	})
	runQuantStringCorpus(t, corpus)
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
	t.Parallel()
	corpus := loadParseCorpus[int, float64](t, quantBitsPerWeightCorpusJSON, 37)
	requireCoverage(t, corpus, map[int]float64{
		8:    8.50,
		15:   4.89,
		9999: 0,
	})
	runQuantBitsPerWeightCorpus(t, corpus)
}

// TestQuantization_MarshalText verifies that MarshalText emits the expected
// wire name for every named member and returns an error for out-of-range values.
func TestQuantization_MarshalText(t *testing.T) {
	t.Parallel()
	// All named members must marshal without error.
	corpus := loadParseCorpus[int, string](t, quantMarshalTextCorpusJSON, 36)
	requireCoverage(t, corpus, map[int]string{
		0:  "none",
		15: "q4_k_m",
		35: "other",
	})
	runQuantMarshalTextCorpus(t, corpus)

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
	t.Parallel()
	// Lossless round-trip for every named member, plus mixed-case inputs
	// (Ollama uses mixed-case file_type values).
	corpus := loadParseCorpus[string, int](t, quantUnmarshalTextCorpusJSON, 42)
	requireCoverage(t, corpus, map[string]int{
		"f16":    1,
		"other":  35,
		"Q4_K_M": 15,
	})
	runQuantUnmarshalTextCorpus(t, corpus)

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
	t.Parallel()
	corpus := loadParseCorpus[int, string](t, quantJSONRoundTripCorpusJSON, 3)
	requireCoverage(t, corpus, map[int]string{
		0:  `{"q":"none"}`,
		15: `{"q":"q4_k_m"}`,
	})
	runQuantJSONRoundTripCorpus(t, corpus)
}

// TestParseQuantization (VC-(A)) verifies ParseQuantization with known inputs,
// empty string, case-insensitive matching, and unknown tokens.
func TestParseQuantization(t *testing.T) {
	t.Parallel()
	// Known values (exact case) + case-insensitive matching.
	corpus := loadParseCorpus[string, int](t, quantParseQuantizationCorpusJSON, 19)
	requireCoverage(t, corpus, map[string]int{
		"q4_k_m": 15,
		"other":  35,
		"Q8_0":   8,
	})
	runQuantIotaOrderCorpus(t, corpus) // same shape (string -> resolved int) as ParseQuantization drives directly

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
	t.Parallel()
	corpus := loadParseCorpus[quantDetectInput, quantDetectExpected](t, quantDetectKnownCorpusJSON, 6)
	requireCoverage(t, corpus, map[quantDetectInput]quantDetectExpected{
		{ID: "llama3.3:70b-instruct-q4_K_M"}: {Q: 15, Raw: "q4_K_M", Stripped: "llama3.3:70b-instruct"},
		{ID: "llama-3-70b-q4_0"}:             {Q: 4, Raw: "q4_0", Stripped: "llama-3-70b"},
	})
	runQuantDetectCorpus(t, corpus)
}

// TestDetectQuantization_NoTag verifies that DetectQuantization returns
// (QuantizationNone, "", id) when the model ID carries no quantization tag.
func TestDetectQuantization_NoTag(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[quantDetectInput, quantDetectExpected](t, quantDetectNoTagCorpusJSON, 5)
	requireCoverage(t, corpus, map[quantDetectInput]quantDetectExpected{
		{ID: "gpt-4o"}:                {Q: 0, Raw: "", Stripped: "gpt-4o"},
		{ID: "llama3.3:70b-instruct"}: {Q: 0, Raw: "", Stripped: "llama3.3:70b-instruct"},
	})
	runQuantDetectCorpus(t, corpus)
}

// TestDetectQuantization_Unknown (VC4) verifies that an unknown-but-quant-looking
// tag results in (QuantizationOther, raw, stripped) without panicking.
// All three return values are pinned exactly to catch mutants that corrupt the
// raw case, truncate the raw, or mis-assemble the stripped ID.
func TestDetectQuantization_Unknown(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[quantDetectInput, quantDetectExpected](t, quantDetectUnknownCorpusJSON, 4)
	requireCoverage(t, corpus, map[quantDetectInput]quantDetectExpected{
		{ID: "somemodel:7b-instruct-q99_X"}: {Q: 35, Raw: "q99_X", Stripped: "somemodel:7b-instruct"},
	})
	runQuantDetectCorpus(t, corpus)

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
	t.Parallel()
	corpus := loadParseCorpus[quantDetectInput, quantDetectExpected](t, quantDetectFP16AliasCorpusJSON, 1)
	requireCoverage(t, corpus, map[quantDetectInput]quantDetectExpected{
		{ID: "llama3.3:70b-instruct-fp16"}: {Q: 35, Raw: "fp16", Stripped: "llama3.3:70b-instruct"},
	})
	runQuantDetectCorpus(t, corpus)
}
