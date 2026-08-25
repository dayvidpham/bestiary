package bestiary_test

// Embedded JSON case corpora for the budget-first fit calculator, plus the shared
// input/expected types. See TESTING.md for the corpus standard: each corpus is guarded
// by an exact case-count control, a value-based coverage assertion, and
// testcase.Corpus.Validate non-vacuity.
//
// Every corpus here is INJECTED: the fit assertions run over entities the test builds,
// not over the shipped registry, so a curation change to the catalog can never silently
// weaken a boundary assertion. The shipped registry is exercised separately, and only
// for identities that hold at any corpus size.

import (
	_ "embed"
)

//go:embed testdata/vram/fit_derived_weights_corpus.json
var fitDerivedWeightsCorpusJSON []byte

//go:embed testdata/vram/fit_null_shape_corpus.json
var fitNullShapeCorpusJSON []byte

//go:embed testdata/vram/fit_boundary_corpus.json
var fitBoundaryCorpusJSON []byte

// fitDerivedWeightsInput is the (attested total parameter count, quantization) pair fed
// to DerivedWeightsBytes.
type fitDerivedWeightsInput struct {
	TotalParams int64  `json:"total_params"`
	Quant       string `json:"quant"`
}

// fitDerivedWeightsExpected is the estimate and the honesty verdict: ok=false is a
// REFUSAL to publish a figure, and Bytes is 0 alongside it.
type fitDerivedWeightsExpected struct {
	Bytes int64 `json:"bytes"`
	OK    bool  `json:"ok"`
}

// fitNullShapeInput is the EntityRef of a shipped entity whose parameter-shape token
// carries no attested total.
type fitNullShapeInput struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

// fitNullShapeExpected is the fit outcome for a one-instance entity built from that ref:
// no rows at all, one entity counted excluded, none counted derived.
type fitNullShapeExpected struct {
	Rows     int `json:"rows"`
	Excluded int `json:"excluded"`
	Derived  int `json:"derived"`
}

// fitBoundaryInput is a single synthetic measured quant row plus the budget it is
// ranked against.
type fitBoundaryInput struct {
	WeightsBytes  int64 `json:"weights_bytes"`
	Layers        int   `json:"layers"`
	KVHeads       int   `json:"kv_heads"`
	HeadDim       int   `json:"head_dim"`
	ModelContext  int   `json:"model_context"`
	TotalBytes    int64 `json:"total_bytes"`
	HeadroomBytes int64 `json:"headroom_bytes"`
}

// fitBoundaryExpected is the max affordable context and the bound that produced it.
type fitBoundaryExpected struct {
	MaxContext int    `json:"max_context"`
	Bound      string `json:"bound"`
}
