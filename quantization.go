package bestiary

import (
	"fmt"
	"strings"
)

// Quantization identifies the numeric format / compression scheme applied to a
// model's weights. It is a closed int enum (like DerivationKind) because the
// set of formats served by Ollama + the reserved HF-ecosystem formats is small
// and well-understood. An open-ended passthrough is provided via
// QuantizationOther (+ a raw string carried on the instance) so that unknown
// tokens in ingested data are never silently dropped.
//
// The zero value is QuantizationNone (format not specified / unknown).
//
// Wire names are lowercase ASCII strings matching the llama.cpp / Ollama
// file_type field values (e.g. "q4_k_m", "f16"). MarshalText / UnmarshalText
// implement encoding.TextMarshaler / encoding.TextUnmarshaler for JSON and YAML
// round-trips.
type Quantization int

const (
	// QuantizationNone is the zero value: no quantization format specified.
	QuantizationNone Quantization = iota
	// QuantF16: 16-bit IEEE float (half precision).
	QuantF16
	// QuantBF16: 16-bit bfloat (brain float).
	QuantBF16
	// QuantF32: 32-bit IEEE float (single precision).
	QuantF32

	// Legacy GGUF block formats.

	// QuantQ4_0: legacy 4-bit block quant, scheme 0.
	QuantQ4_0
	// QuantQ4_1: legacy 4-bit block quant, scheme 1.
	QuantQ4_1
	// QuantQ5_0: legacy 5-bit block quant, scheme 0.
	QuantQ5_0
	// QuantQ5_1: legacy 5-bit block quant, scheme 1.
	QuantQ5_1
	// QuantQ8_0: legacy 8-bit block quant, scheme 0.
	QuantQ8_0

	// K-quants (super-block, 2–8 bit).

	// QuantQ2_K: 2-bit k-quant.
	QuantQ2_K
	// QuantQ2_K_S: 2-bit k-quant, small super-block.
	QuantQ2_K_S
	// QuantQ3_K_S: 3-bit k-quant, small super-block.
	QuantQ3_K_S
	// QuantQ3_K_M: 3-bit k-quant, medium super-block.
	QuantQ3_K_M
	// QuantQ3_K_L: 3-bit k-quant, large super-block.
	QuantQ3_K_L
	// QuantQ4_K_S: 4-bit k-quant, small super-block.
	QuantQ4_K_S
	// QuantQ4_K_M: 4-bit k-quant, medium super-block (most popular).
	QuantQ4_K_M
	// QuantQ5_K_S: 5-bit k-quant, small super-block.
	QuantQ5_K_S
	// QuantQ5_K_M: 5-bit k-quant, medium super-block.
	QuantQ5_K_M
	// QuantQ6_K: 6-bit k-quant.
	QuantQ6_K

	// I-quants (importance-matrix, 1–4 bit).

	// QuantIQ1_S: 1-bit i-quant, small.
	QuantIQ1_S
	// QuantIQ1_M: 1-bit i-quant, medium.
	QuantIQ1_M
	// QuantIQ2_XXS: 2-bit i-quant, extra-extra-small.
	QuantIQ2_XXS
	// QuantIQ2_XS: 2-bit i-quant, extra-small.
	QuantIQ2_XS
	// QuantIQ2_S: 2-bit i-quant, small.
	QuantIQ2_S
	// QuantIQ2_M: 2-bit i-quant, medium.
	QuantIQ2_M
	// QuantIQ3_XXS: 3-bit i-quant, extra-extra-small.
	QuantIQ3_XXS
	// QuantIQ3_XS: 3-bit i-quant, extra-small.
	QuantIQ3_XS
	// QuantIQ3_S: 3-bit i-quant, small.
	QuantIQ3_S
	// QuantIQ3_M: 3-bit i-quant, medium.
	QuantIQ3_M
	// QuantIQ4_XS: 4-bit i-quant, extra-small.
	QuantIQ4_XS
	// QuantIQ4_NL: 4-bit i-quant, non-linear (nearest-lookup).
	QuantIQ4_NL

	// Reserved: HF-ecosystem formats. Ingest deferred until an HF/Unsloth source
	// is added. Values are reserved here so the wire format is stable when that
	// source lands.

	// QuantAWQ: Activation-aware Weight Quantization (HF/vLLM, 4-bit).
	QuantAWQ
	// QuantGPTQ: GPTQ post-training quantization (HF/AutoGPTQ, 3/4/8-bit).
	QuantGPTQ
	// QuantInt8: bitsandbytes 8-bit integer (HF transformers).
	QuantInt8
	// QuantInt4: bitsandbytes 4-bit integer (HF transformers).
	QuantInt4

	// QuantizationOther is the lossless escape for any token that is present
	// in ingested data but not yet covered by the named constants above.  The
	// caller stores the raw string on the enclosing struct (e.g. QuantVRAM.QuantRaw).
	// ParseQuantization never silently maps an unrecognised token to this value;
	// DetectQuantization uses it for unrecognised tags found in model IDs.
	QuantizationOther
)

// quantNames is the canonical lowercase wire name for each Quantization value,
// index-aligned with the iota constants above.  It is the single source of truth
// for String / MarshalText / UnmarshalText.
var quantNames = [...]string{
	QuantizationNone:  "none",
	QuantF16:          "f16",
	QuantBF16:         "bf16",
	QuantF32:          "f32",
	QuantQ4_0:         "q4_0",
	QuantQ4_1:         "q4_1",
	QuantQ5_0:         "q5_0",
	QuantQ5_1:         "q5_1",
	QuantQ8_0:         "q8_0",
	QuantQ2_K:         "q2_k",
	QuantQ2_K_S:       "q2_k_s",
	QuantQ3_K_S:       "q3_k_s",
	QuantQ3_K_M:       "q3_k_m",
	QuantQ3_K_L:       "q3_k_l",
	QuantQ4_K_S:       "q4_k_s",
	QuantQ4_K_M:       "q4_k_m",
	QuantQ5_K_S:       "q5_k_s",
	QuantQ5_K_M:       "q5_k_m",
	QuantQ6_K:         "q6_k",
	QuantIQ1_S:        "iq1_s",
	QuantIQ1_M:        "iq1_m",
	QuantIQ2_XXS:      "iq2_xxs",
	QuantIQ2_XS:       "iq2_xs",
	QuantIQ2_S:        "iq2_s",
	QuantIQ2_M:        "iq2_m",
	QuantIQ3_XXS:      "iq3_xxs",
	QuantIQ3_XS:       "iq3_xs",
	QuantIQ3_S:        "iq3_s",
	QuantIQ3_M:        "iq3_m",
	QuantIQ4_XS:       "iq4_xs",
	QuantIQ4_NL:       "iq4_nl",
	QuantAWQ:          "awq",
	QuantGPTQ:         "gptq",
	QuantInt8:         "int8",
	QuantInt4:         "int4",
	QuantizationOther: "other",
}

// quantBitsPerWeight is the authoritative bits-per-weight for each Quantization,
// index-aligned with quantNames.  Values are from the llama.cpp quantize README
// (tools/quantize/README.md).  0 is used for QuantizationNone and
// QuantizationOther (undefined / model-specific) and for reserved HF-ecosystem
// members whose bpw is not a single well-defined value (AWQ / GPTQ / Int8 /
// Int4 vary by configuration).
var quantBitsPerWeight = [...]float64{
	QuantizationNone:  0,
	QuantF16:          16.0,
	QuantBF16:         16.0,
	QuantF32:          32.0,
	QuantQ4_0:         4.5,  // ~4.5 bpw (llama.cpp README: "approximately 4.5")
	QuantQ4_1:         5.0,  // ~5.0 bpw
	QuantQ5_0:         5.5,  // ~5.5 bpw
	QuantQ5_1:         6.0,  // ~6.0 bpw
	QuantQ8_0:         8.50, // 8.50 bpw (llama.cpp README exact)
	QuantQ2_K:         3.16, // k-quant bpw from llama.cpp README
	QuantQ2_K_S:       2.97,
	QuantQ3_K_S:       3.64,
	QuantQ3_K_M:       4.00,
	QuantQ3_K_L:       4.30,
	QuantQ4_K_S:       4.67,
	QuantQ4_K_M:       4.89,
	QuantQ5_K_S:       5.57,
	QuantQ5_K_M:       5.70,
	QuantQ6_K:         6.56,
	QuantIQ1_S:        2.00, // i-quant bpw from llama.cpp README
	QuantIQ1_M:        2.15,
	QuantIQ2_XXS:      2.38,
	QuantIQ2_XS:       2.59,
	QuantIQ2_S:        2.74,
	QuantIQ2_M:        2.93,
	QuantIQ3_XXS:      3.25,
	QuantIQ3_XS:       3.50,
	QuantIQ3_S:        3.66,
	QuantIQ3_M:        3.76,
	QuantIQ4_XS:       4.46,
	QuantIQ4_NL:       4.68,
	QuantAWQ:          0, // nominal ~4-bit but varies by config; unknown until ingest
	QuantGPTQ:         0, // nominal 3/4/8-bit but varies by config; unknown until ingest
	QuantInt8:         0, // nominal 8-bit but bitsandbytes config varies; unknown until ingest
	QuantInt4:         0, // nominal 4-bit but bitsandbytes config varies; unknown until ingest
	QuantizationOther: 0,
}

// String returns the canonical lowercase wire name of the quantization (e.g.
// "q4_k_m", "f16").  An out-of-range value renders as
// "quantization(<n>)" so that log messages never silently drop unexpected values.
func (q Quantization) String() string {
	if int(q) < 0 || int(q) >= len(quantNames) {
		return fmt.Sprintf("quantization(%d)", int(q))
	}
	return quantNames[q]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical
// lowercase wire name so Quantization serializes as a JSON/YAML string rather
// than an integer.  An out-of-range value is a programming error and returns an
// actionable error.
func (q Quantization) MarshalText() ([]byte, error) {
	if int(q) < 0 || int(q) >= len(quantNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal Quantization: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed"+
				" (only QuantizationNone..QuantizationOther constants are valid);"+
				" where: Quantization.MarshalText;"+
				" how to fix: assign one of the exported Quantization constants",
			int(q), len(quantNames),
		)
	}
	return []byte(quantNames[q]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical
// lowercase wire name back into a Quantization.  Parsing is case-insensitive so
// that Ollama's mixed-case file_type values (e.g. "Q4_K_M", "F16") round-trip
// correctly.  Aliases such as "fp16" are NOT accepted; ingest layers must
// normalise fp16→f16 before unmarshalling (see TestDetectQuantization_FP16Alias).
// An unrecognised token yields an actionable error listing valid examples.
func (q *Quantization) UnmarshalText(text []byte) error {
	s := strings.ToLower(string(text))
	for i, name := range quantNames {
		if name == s {
			*q = Quantization(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal Quantization from %q;"+
			" why: the token does not match any known quantization name;"+
			" where: Quantization.UnmarshalText;"+
			" valid examples: f16, bf16, q4_k_m, q8_0, iq4_nl, other;"+
			" how to fix: use one of the canonical wire names (full list: %v)",
		string(text), quantNames,
	)
}

// BitsPerWeight returns the authoritative bits-per-weight for this quantization
// as documented in the llama.cpp quantize README (tools/quantize/README.md).
// Returns 0 for QuantizationNone, QuantizationOther, and the reserved
// HF-ecosystem constants (QuantAWQ, QuantGPTQ, QuantInt8, QuantInt4) whose
// effective bpw is configuration-dependent and not yet ingested.
func (q Quantization) BitsPerWeight() float64 {
	if int(q) < 0 || int(q) >= len(quantBitsPerWeight) {
		return 0
	}
	return quantBitsPerWeight[q]
}

// IsKnown reports whether q is a named constant in this package (i.e. not an
// out-of-range integer).  QuantizationOther is considered known (it is a named
// member of the enum); only truly out-of-range integers return false.
func (q Quantization) IsKnown() bool {
	return int(q) >= 0 && int(q) < len(quantNames)
}

// DetectQuantization inspects a model ID for an embedded or trailing
// quantization tag in Ollama-style notation (e.g.
// "llama3.3:70b-instruct-q4_K_M") and returns:
//
//   - q: the matched Quantization constant (QuantizationNone when no tag is
//     found; QuantizationOther when a quant-looking tag is present but
//     unrecognised).
//   - raw: the raw tag string as it appears in the id, without case
//     normalisation (e.g. "q4_K_M").  Empty when no tag is found.
//   - stripped: the model ID with the quant tag (and its leading separator
//     "-") removed.  Equal to id when no tag is found.
//
// Matching is case-insensitive against quantNames so both "Q4_K_M" and
// "q4_k_m" resolve to QuantQ4_K_M.  The function never panics.
//
// This extends DetectHost's (value, stripped-id) shape with the raw matched
// tag as a third return value.
func DetectQuantization(id ModelID) (Quantization, string, ModelID) {
	s := string(id)

	// Split on ":" to separate model name from tag part
	// (e.g. "llama3.3:70b-instruct-q4_K_M" → base="llama3.3", tag part="70b-instruct-q4_K_M").
	// Only the tag part is searched; the family/version before the colon does
	// not carry quant tokens.
	base, tagPart, hadColon := strings.Cut(s, ":")
	if !hadColon {
		tagPart = s
	}

	// Walk segments separated by "-" from right to left.  Quant tokens are
	// conventionally the last dash-separated segment (underscores within the
	// token, e.g. "q4_K_M", are part of a single segment).
	segments := strings.Split(tagPart, "-")
	for i := len(segments) - 1; i >= 0; i-- {
		candidate := strings.Join(segments[i:], "-")
		lower := strings.ToLower(candidate)
		for j, name := range quantNames {
			// Skip the internal sentinels: "none" and "other" are not llama.cpp
			// or Ollama quant tags; matching them would corrupt the stripped ID.
			if j == int(QuantizationNone) || j == int(QuantizationOther) {
				continue
			}
			if name == lower {
				remaining := strings.Join(segments[:i], "-")
				return Quantization(j), candidate, rebuildStrippedID(base, hadColon, remaining)
			}
		}
	}

	// No known quant tag found; check whether the last dash-segment looks like
	// an unknown quant token (see looksLikeQuantTag) and return QuantizationOther
	// with it stripped.
	if len(segments) > 0 {
		last := segments[len(segments)-1]
		if looksLikeQuantTag(last) {
			remaining := strings.Join(segments[:len(segments)-1], "-")
			return QuantizationOther, last, rebuildStrippedID(base, hadColon, remaining)
		}
	}

	return QuantizationNone, "", id
}

// rebuildStrippedID reassembles a stripped ModelID from the base (the part
// before the colon, if any), the hadColon flag, and the remaining tag-part
// segments after the quant token has been removed.  Trailing dashes left by
// segment joining are trimmed before reassembly.
func rebuildStrippedID(base string, hadColon bool, remaining string) ModelID {
	remaining = strings.TrimRight(remaining, "-")
	if hadColon {
		if remaining == "" {
			return ModelID(base)
		}
		return ModelID(base + ":" + remaining)
	}
	return ModelID(remaining)
}

// looksLikeQuantTag returns true when s resembles an Ollama/llama.cpp quant
// tag token that is not in the known quantNames list.  It guards against
// stripping legitimate model-name segments (like "instruct" or "7b") as
// unknown quants.  The heuristic: s must start with one of the known quant
// prefixes (iq, bf, fp, q, f) followed immediately by a digit.  "fp" covers
// Ollama's fp16/fp32 aliases.  Prefix order is immaterial — the loop
// continues past any prefix whose next character is not a digit.
func looksLikeQuantTag(s string) bool {
	if len(s) == 0 {
		return false
	}
	lower := strings.ToLower(s)
	for _, pfx := range []string{"iq", "bf", "fp", "q", "f"} {
		if strings.HasPrefix(lower, pfx) {
			rest := lower[len(pfx):]
			if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
				return true
			}
		}
	}
	return false
}

// ParseQuantization parses a Quantization from a string using a
// case-insensitive exact match against the canonical wire names.  It is
// intended for CLI flag parsing and configuration input.
//
//   - "" returns (QuantizationNone, nil) — the empty string is treated as
//     "not specified" without error.
//   - A recognised name (e.g. "Q4_K_M", "f16") returns the corresponding
//     constant and nil.
//   - Any other non-empty string returns (QuantizationNone, error) with an
//     actionable message that lists what was received and valid examples.
//     Unlike DetectQuantization, ParseQuantization NEVER silently maps an
//     unrecognised token to QuantizationOther.
func ParseQuantization(s string) (Quantization, error) {
	if s == "" {
		return QuantizationNone, nil
	}
	lower := strings.ToLower(s)
	for i, name := range quantNames {
		if name == lower {
			return Quantization(i), nil
		}
	}
	return QuantizationNone, fmt.Errorf(
		"bestiary: ParseQuantization: unrecognised quantization %q;"+
			" why: the input does not match any known quantization name (case-insensitive);"+
			" where: ParseQuantization;"+
			" valid examples: f16, bf16, f32, q4_0, q8_0, q4_k_m, q5_k_m, iq4_nl, other;"+
			" how to fix: pass one of the canonical wire names listed above",
		s,
	)
}
