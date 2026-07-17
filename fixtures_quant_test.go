package bestiary_test

// Embedded JSON case corpora for the quantization- and VRAM-package
// table-driven tests, plus the shared input/expected types and corpus-runner
// helpers. See TESTING.md for the corpus standard: each corpus is guarded by
// an exact case-count control, a value-based coverage assertion, and
// testcase.Corpus.Validate non-vacuity.

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// requireCoverage asserts each probed input is still present in the corpus
// with its expected value. It is the value-based coverage guard: a
// count-preserving swap that drops a load-bearing case (and adds a filler)
// reddens here even though the exact-count control cannot see it. Generic
// over any comparable (I, E) pair, it is shared across every quant/vram
// corpus rather than duplicated per type (cf. the per-type
// requireFamilyVariantCoverage/requireDateCoverage helpers in
// fixtures_parse_test.go, written before this package used generics this way).
func requireCoverage[I comparable, E comparable](t *testing.T, corpus testcase.Corpus[I, E], probes map[I]E) {
	t.Helper()
	got := map[I]E{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: case for input %+v is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: case %+v has %+v, want %+v", in, have, want)
		}
	}
}

// ---- Quantization corpora ---------------------------------------------

//go:embed testdata/quant/iota_order_corpus.json
var quantIotaOrderCorpusJSON []byte

//go:embed testdata/quant/string_corpus.json
var quantStringCorpusJSON []byte

//go:embed testdata/quant/bits_per_weight_corpus.json
var quantBitsPerWeightCorpusJSON []byte

//go:embed testdata/quant/marshal_text_corpus.json
var quantMarshalTextCorpusJSON []byte

//go:embed testdata/quant/unmarshal_text_corpus.json
var quantUnmarshalTextCorpusJSON []byte

//go:embed testdata/quant/json_roundtrip_corpus.json
var quantJSONRoundTripCorpusJSON []byte

//go:embed testdata/quant/parse_quantization_corpus.json
var quantParseQuantizationCorpusJSON []byte

//go:embed testdata/quant/detect_known_corpus.json
var quantDetectKnownCorpusJSON []byte

//go:embed testdata/quant/detect_no_tag_corpus.json
var quantDetectNoTagCorpusJSON []byte

//go:embed testdata/quant/detect_unknown_corpus.json
var quantDetectUnknownCorpusJSON []byte

//go:embed testdata/quant/detect_fp16_alias_corpus.json
var quantDetectFP16AliasCorpusJSON []byte

// quantDetectInput is the ModelID fed to DetectQuantization.
type quantDetectInput struct {
	ID string `json:"id"`
}

// quantDetectExpected is the (Quantization, raw tag, stripped ID) triple
// DetectQuantization returns, with Q carried as its underlying int (the
// corpus JSON schema has no direct way to reference a Go enum constant).
type quantDetectExpected struct {
	Q        int    `json:"q"`
	Raw      string `json:"raw"`
	Stripped string `json:"stripped"`
}

// runQuantIotaOrderCorpus drives bestiary.ParseQuantization over the wire
// name and asserts the resolved Quantization's int value against Expected —
// the data-driven equivalent of comparing a named Go constant to its pinned
// iota (IP-1 ratified contract).
func runQuantIotaOrderCorpus(t *testing.T, corpus testcase.Corpus[string, int]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			q, err := bestiary.ParseQuantization(c.Input)
			if err != nil {
				t.Fatalf("ParseQuantization(%q) error: %v", c.Input, err)
			}
			if int(q) != c.Expected {
				t.Errorf("ParseQuantization(%q) = %d, want %d", c.Input, int(q), c.Expected)
			}
		})
	}
}

// runQuantStringCorpus drives Quantization.String() over the raw int value.
func runQuantStringCorpus(t *testing.T, corpus testcase.Corpus[int, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.Quantization(c.Input).String()
			if got != c.Expected {
				t.Errorf("Quantization(%d).String() = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// runQuantBitsPerWeightCorpus drives Quantization.BitsPerWeight() over the raw int value.
func runQuantBitsPerWeightCorpus(t *testing.T, corpus testcase.Corpus[int, float64]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.Quantization(c.Input).BitsPerWeight()
			if got != c.Expected {
				t.Errorf("Quantization(%d).BitsPerWeight() = %v, want %v", c.Input, got, c.Expected)
			}
		})
	}
}

// runQuantMarshalTextCorpus drives Quantization.MarshalText() over the raw int value.
func runQuantMarshalTextCorpus(t *testing.T, corpus testcase.Corpus[int, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got, err := bestiary.Quantization(c.Input).MarshalText()
			if err != nil {
				t.Fatalf("Quantization(%d).MarshalText() error: %v", c.Input, err)
			}
			if string(got) != c.Expected {
				t.Errorf("Quantization(%d).MarshalText() = %q, want %q", c.Input, string(got), c.Expected)
			}
		})
	}
}

// runQuantUnmarshalTextCorpus drives Quantization.UnmarshalText() over the wire token.
func runQuantUnmarshalTextCorpus(t *testing.T, corpus testcase.Corpus[string, int]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			var got bestiary.Quantization
			if err := got.UnmarshalText([]byte(c.Input)); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", c.Input, err)
			}
			if int(got) != c.Expected {
				t.Errorf("UnmarshalText(%q) = %d, want %d", c.Input, int(got), c.Expected)
			}
		})
	}
}

// runQuantJSONRoundTripCorpus drives encoding/json over a wrapper struct
// carrying the Quantization at the raw int value, asserting the exact
// marshaled JSON and a lossless unmarshal round trip.
func runQuantJSONRoundTripCorpus(t *testing.T, corpus testcase.Corpus[int, string]) {
	t.Helper()
	type wrapper struct {
		Q bestiary.Quantization `json:"q"`
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			b, err := json.Marshal(wrapper{Q: bestiary.Quantization(c.Input)})
			if err != nil {
				t.Fatalf("json.Marshal(Q=%d) error: %v", c.Input, err)
			}
			if string(b) != c.Expected {
				t.Errorf("json.Marshal(Q=%d) = %s, want %s", c.Input, string(b), c.Expected)
			}
			var got wrapper
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error: %v", string(b), err)
			}
			if int(got.Q) != c.Input {
				t.Errorf("JSON round-trip Q=%d: got %d", c.Input, int(got.Q))
			}
		})
	}
}

// runQuantDetectCorpus drives bestiary.DetectQuantization over each case's
// ModelID and asserts the (Quantization, raw, stripped) triple. It never
// panics, matching the original inline tables' panic-guard discipline.
func runQuantDetectCorpus(t *testing.T, corpus testcase.Corpus[quantDetectInput, quantDetectExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("DetectQuantization(%q) panicked: %v", c.Input.ID, r)
				}
			}()
			q, raw, stripped := bestiary.DetectQuantization(bestiary.ModelID(c.Input.ID))
			if int(q) != c.Expected.Q {
				t.Errorf("DetectQuantization(%q): q = %d, want %d", c.Input.ID, int(q), c.Expected.Q)
			}
			if raw != c.Expected.Raw {
				t.Errorf("DetectQuantization(%q): raw = %q, want %q", c.Input.ID, raw, c.Expected.Raw)
			}
			if string(stripped) != c.Expected.Stripped {
				t.Errorf("DetectQuantization(%q): stripped = %q, want %q", c.Input.ID, string(stripped), c.Expected.Stripped)
			}
		})
	}
}

// ---- VRAM corpora -------------------------------------------------------

//go:embed testdata/vram/estimate_exact_values_corpus.json
var vramEstimateExactValuesCorpusJSON []byte

//go:embed testdata/vram/partial_truth_table_corpus.json
var vramPartialTruthTableCorpusJSON []byte

// vramExactInput is the (weightsBytes, contextTokens, layers, kvHeads,
// headDim) 5-tuple fed to bestiary.EstimateVRAMBytes.
type vramExactInput struct {
	WeightsBytes  int64 `json:"weights_bytes"`
	ContextTokens int   `json:"context_tokens"`
	Layers        int   `json:"layers"`
	KVHeads       int   `json:"kv_heads"`
	HeadDim       int   `json:"head_dim"`
}

// vramPartialInput is the (layers, kvHeads, headDim) triple fed to
// bestiary.VRAMEstimateIsPartial.
type vramPartialInput struct {
	Layers  int `json:"layers"`
	KVHeads int `json:"kv_heads"`
	HeadDim int `json:"head_dim"`
}

// runVRAMEstimateExactValuesCorpus drives bestiary.EstimateVRAMBytes over
// each case and asserts the exact total against literal expected values
// (see each case's provenance.ref for the hand-computed arithmetic
// derivation). t.Parallel() at both levels mirrors the pre-migration shape.
func runVRAMEstimateExactValuesCorpus(t *testing.T, corpus testcase.Corpus[vramExactInput, int64]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			in := c.Input
			got := bestiary.EstimateVRAMBytes(in.WeightsBytes, in.ContextTokens, in.Layers, in.KVHeads, in.HeadDim)
			if got != c.Expected {
				t.Errorf("EstimateVRAMBytes(%d, %d, %d, %d, %d) = %d, want %d",
					in.WeightsBytes, in.ContextTokens, in.Layers, in.KVHeads, in.HeadDim, got, c.Expected)
			}
		})
	}
}

// runVRAMPartialTruthTableCorpus drives bestiary.VRAMEstimateIsPartial over
// each (layers, kvHeads, headDim) case.
func runVRAMPartialTruthTableCorpus(t *testing.T, corpus testcase.Corpus[vramPartialInput, bool]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			in := c.Input
			got := bestiary.VRAMEstimateIsPartial(in.Layers, in.KVHeads, in.HeadDim)
			if got != c.Expected {
				t.Errorf("VRAMEstimateIsPartial(%d, %d, %d) = %v, want %v",
					in.Layers, in.KVHeads, in.HeadDim, got, c.Expected)
			}
		})
	}
}
