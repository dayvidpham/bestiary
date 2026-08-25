package bestiary_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// quantFromName parses a canonical wire name through the production UnmarshalText seam,
// so a corpus row naming a quantization that no longer exists fails loudly here rather
// than silently resolving to the zero value (which is itself a meaningful member).
func quantFromName(t *testing.T, name string) bestiary.Quantization {
	t.Helper()
	var q bestiary.Quantization
	if err := q.UnmarshalText([]byte(name)); err != nil {
		t.Fatalf("corpus names quantization %q, which does not parse: %v", name, err)
	}
	return q
}

// measuredEntity builds a one-instance entity carrying exactly one ingested quant row.
// It is the injected stand-in for a measured catalog entity: the fit assertions must
// hold for any corpus, so they are driven from values the test owns.
func measuredEntity(key string, in fitBoundaryInput) bestiary.Entity {
	return bestiary.Entity{
		Ref: bestiary.EntityRef{Family: bestiary.Family(key), Version: "1"},
		Instances: []bestiary.ProviderInstance{{
			ID:            bestiary.ModelID(key),
			Provider:      bestiary.Provider("fixture"),
			ContextWindow: in.ModelContext,
			QuantVRAM: []bestiary.QuantVRAM{{
				Quant:               bestiary.QuantQ4_K_M,
				QuantRaw:            "Q4_K_M",
				WeightsBytes:        in.WeightsBytes,
				Layers:              in.Layers,
				KVHeads:             in.KVHeads,
				HeadDim:             in.HeadDim,
				VRAMEstimatePartial: bestiary.VRAMEstimateIsPartial(in.Layers, in.KVHeads, in.HeadDim),
			}},
		}},
	}
}

// sizedEntity builds a one-instance entity with a parameter-size token and NO measured
// quant row: the shape every derived (or excluded) row is computed from.
func sizedEntity(ref bestiary.EntityRef, contextWindow int) bestiary.Entity {
	return bestiary.Entity{
		Ref: ref,
		Instances: []bestiary.ProviderInstance{{
			ID:            bestiary.ModelID(ref.String()),
			Provider:      bestiary.Provider("fixture"),
			ContextWindow: contextWindow,
		}},
	}
}

// ---- The calculator agrees with the shipped formula -------------------------

// TestFitOver_ContextBoundary_Corpus is the context-boundary assertion, driven through
// FitOver over an INJECTED corpus.
//
// For every row with a computable KV term it asserts the defining property of "max
// affordable context" using the SHIPPED recompute (QuantVRAM.EstimateVRAM) on both
// sides: the estimate at m fits the available budget, and the estimate at m+1 does not
// — unless m is the model's own context window, where one more token is refused by the
// model rather than by the budget. The KV arithmetic is never re-implemented here; that
// is the whole point of driving the assertion through the production method.
func TestFitOver_ContextBoundary_Corpus(t *testing.T) {
	corpus, err := testcase.LoadCorpus[fitBoundaryInput, fitBoundaryExpected](fitBoundaryCorpusJSON)
	if err != nil {
		t.Fatalf("load fit boundary corpus: %v", err)
	}
	if got := len(corpus.Cases); got != 7 {
		t.Fatalf("fit boundary corpus has %d cases, want exactly 7", got)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("fit boundary corpus is under-populated: %v", err)
	}
	// Value-based coverage: the three load-bearing bounds must each still be present,
	// and the absent-arch-facts row must still be the one with no layer count. A
	// count-preserving swap that dropped any of them would redden here.
	seen := map[string]int{}
	unknownHasNoLayers := false
	for _, c := range corpus.Cases {
		seen[c.Expected.Bound]++
		if c.Expected.Bound == "unknown" && c.Input.Layers == 0 {
			unknownHasNoLayers = true
		}
	}
	for _, want := range []string{"budget", "model", "unknown"} {
		if seen[want] == 0 {
			t.Errorf("value coverage lost: no case expects the %q bound", want)
		}
	}
	if !unknownHasNoLayers {
		t.Error("value coverage lost: the unknown-bound case no longer omits an architecture fact")
	}

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			budget := bestiary.FitBudget{TotalBytes: c.Input.TotalBytes, HeadroomBytes: c.Input.HeadroomBytes}
			e := measuredEntity("fixture-model", c.Input)
			res := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: budget})
			if len(res.Rows) != 1 {
				t.Fatalf("got %d rows, want exactly 1 (the injected measured row)", len(res.Rows))
			}
			row := res.Rows[0]
			if row.MaxContext != c.Expected.MaxContext {
				t.Errorf("MaxContext = %d, want %d", row.MaxContext, c.Expected.MaxContext)
			}
			if got := row.Bound.String(); got != c.Expected.Bound {
				t.Errorf("Bound = %s, want %s", got, c.Expected.Bound)
			}
			if row.WeightsBasis != bestiary.BasisMeasured {
				t.Errorf("WeightsBasis = %s, want measured", row.WeightsBasis)
			}
			if row.WeightsBytes != c.Input.WeightsBytes {
				t.Errorf("WeightsBytes = %d, want the ingested file size %d exactly",
					row.WeightsBytes, c.Input.WeightsBytes)
			}

			if row.Bound == bestiary.ContextBoundUnknown {
				if row.MaxContext != 0 {
					t.Errorf("a non-computable KV term reported %d tokens; it must report none", row.MaxContext)
				}
				return
			}

			// The defining property, both sides computed by the SHIPPED formula.
			q := e.Instances[0].QuantVRAM[0]
			avail := budget.Available()
			m := row.MaxContext
			if m > 0 {
				if got := q.EstimateVRAM(m); got > avail {
					t.Errorf("EstimateVRAM(%d) = %d exceeds the available budget %d", m, got, avail)
				}
			}
			if m == row.ModelContext {
				return // one more token is refused by the model, not by the budget
			}
			if got := q.EstimateVRAM(m + 1); got <= avail {
				t.Errorf("EstimateVRAM(%d) = %d still fits the available budget %d, so %d is not the maximum",
					m+1, got, avail, m)
			}
		})
	}
}

// ---- The derived-weights class and its exclusions ---------------------------

// TestDerivedWeightsBytes_Corpus is the zero-bits-per-weight half of the exclusion
// table: all six members whose BitsPerWeight is 0 refuse a figure rather than returning
// a zero-byte one, and the derivable members return the hand-computed estimate.
func TestDerivedWeightsBytes_Corpus(t *testing.T) {
	corpus, err := testcase.LoadCorpus[fitDerivedWeightsInput, fitDerivedWeightsExpected](fitDerivedWeightsCorpusJSON)
	if err != nil {
		t.Fatalf("load derived-weights corpus: %v", err)
	}
	if got := len(corpus.Cases); got != 12 {
		t.Fatalf("derived-weights corpus has %d cases, want exactly 12", got)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("derived-weights corpus is under-populated: %v", err)
	}
	// Value-based coverage: every zero-bits-per-weight member must still be refused by
	// name. This is the list the exclusion table enumerates, and it is the list a reader
	// would be misled by if any member silently started producing rows.
	wantRefused := []string{"none", "awq", "gptq", "int8", "int4", "other"}
	refused := map[string]bool{}
	for _, c := range corpus.Cases {
		if !c.Expected.OK && c.Input.TotalParams > 0 {
			refused[c.Input.Quant] = true
		}
	}
	for _, q := range wantRefused {
		if !refused[q] {
			t.Errorf("value coverage lost: no case refuses a derived figure for quantization %q", q)
		}
	}

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			q := quantFromName(t, c.Input.Quant)
			gotBytes, gotOK := bestiary.DerivedWeightsBytes(c.Input.TotalParams, q)
			if gotOK != c.Expected.OK {
				t.Fatalf("DerivedWeightsBytes(%d, %s) ok = %v, want %v",
					c.Input.TotalParams, q, gotOK, c.Expected.OK)
			}
			if gotBytes != c.Expected.Bytes {
				t.Errorf("DerivedWeightsBytes(%d, %s) = %d bytes, want %d",
					c.Input.TotalParams, q, gotBytes, c.Expected.Bytes)
			}
		})
	}
}

// TestFitOver_NullShapes_ProduceNoDerivedRow is the parameter-shape half of the
// exclusion table: each shipped entity key whose size token carries no attested total
// produces NO row and is counted excluded, never derived.
func TestFitOver_NullShapes_ProduceNoDerivedRow(t *testing.T) {
	corpus, err := testcase.LoadCorpus[fitNullShapeInput, fitNullShapeExpected](fitNullShapeCorpusJSON)
	if err != nil {
		t.Fatalf("load null-shape corpus: %v", err)
	}
	if got := len(corpus.Cases); got != 11 {
		t.Fatalf("null-shape corpus has %d cases, want exactly 11", got)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("null-shape corpus is under-populated: %v", err)
	}
	// Value-based coverage: both dangerous shape families must still be represented by
	// their size tokens. They fail for different reasons (an NxM total is a refused
	// product; an Nb-Ke total was never published) and dropping either arm would leave
	// half the exclusion rule untested.
	tokens := map[string]bool{}
	for _, c := range corpus.Cases {
		tokens[c.Input.ParamSize] = true
	}
	for _, want := range []string{"8x7b", "8x22b", "17b-16e", "17b-128e"} {
		if !tokens[want] {
			t.Errorf("value coverage lost: no case carries the %q size token", want)
		}
	}

	// A budget large enough that nothing is filtered out for size: any row that appears
	// appeared because the shape was derived from, which is exactly what must not happen.
	budget := bestiary.FitBudget{TotalBytes: 1 << 50}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ref := bestiary.EntityRef{
				Family:    bestiary.Family(c.Input.Family),
				Variant:   c.Input.Variant,
				Version:   c.Input.Version,
				ParamSize: c.Input.ParamSize,
				Modifier:  c.Input.Modifier,
			}
			res := bestiary.FitOver([]bestiary.Entity{sizedEntity(ref, 32768)}, bestiary.FitFilter{Budget: budget})
			if got := len(res.Rows); got != c.Expected.Rows {
				t.Errorf("%s produced %d rows, want %d", ref.String(), got, c.Expected.Rows)
			}
			if res.EntitiesExcluded != c.Expected.Excluded {
				t.Errorf("%s: EntitiesExcluded = %d, want %d", ref.String(), res.EntitiesExcluded, c.Expected.Excluded)
			}
			if res.EntitiesDerived != c.Expected.Derived {
				t.Errorf("%s: EntitiesDerived = %d, want %d", ref.String(), res.EntitiesDerived, c.Expected.Derived)
			}
		})
	}
}

// TestFitOver_DerivedRow_IsTypedAndQualified pins the shape of a derived row: the typed
// basis, the always-true partial flag, the absent KV term, and the refusal to invent an
// upstream quant spelling.
func TestFitOver_DerivedRow_IsTypedAndQualified(t *testing.T) {
	ref := bestiary.EntityRef{Family: "fixture", Version: "1", ParamSize: "30b"}
	res := bestiary.FitOver(
		[]bestiary.Entity{sizedEntity(ref, 32768)},
		bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 1 << 50}},
	)
	if res.EntitiesDerived != 1 {
		t.Fatalf("EntitiesDerived = %d, want 1", res.EntitiesDerived)
	}
	if len(res.Rows) == 0 {
		t.Fatal("an attested total produced no derived rows")
	}
	for _, r := range res.Rows {
		if r.WeightsBasis != bestiary.BasisDerived {
			t.Errorf("%s row basis = %s, want derived", r.Quant, r.WeightsBasis)
		}
		if !r.Partial {
			t.Errorf("%s row is not marked partial; every derived row is a weights-only lower bound", r.Quant)
		}
		if r.KVBytesPerToken != 0 || r.Bound != bestiary.ContextBoundUnknown {
			t.Errorf("%s row claims a KV term (%d bytes/token, bound %s); no unmeasured entity carries architecture facts",
				r.Quant, r.KVBytesPerToken, r.Bound)
		}
		if r.QuantRaw != "" {
			t.Errorf("%s row carries an upstream quant spelling %q that nothing upstream published", r.Quant, r.QuantRaw)
		}
		if r.Quant.BitsPerWeight() == 0 {
			t.Errorf("%s has a zero bits-per-weight and must not produce a row at all", r.Quant)
		}
	}
	// One row per derivable quantization member, per instance.
	wantQuants := 0
	for q := bestiary.QuantizationNone; q <= bestiary.QuantizationOther; q++ {
		if q.BitsPerWeight() != 0 {
			wantQuants++
		}
	}
	if len(res.Rows) != wantQuants {
		t.Errorf("got %d derived rows for a one-instance entity, want one per derivable quantization (%d)",
			len(res.Rows), wantQuants)
	}
}

// TestFitOver_ZeroInstanceEntity_ProducesNoRow pins the instance gate: a metadata-only
// standalone that no provider serves gets no derived row, and is counted neither derived
// nor excluded — there is no deployment for a fit verdict to be about.
func TestFitOver_ZeroInstanceEntity_ProducesNoRow(t *testing.T) {
	e := bestiary.Entity{Ref: bestiary.EntityRef{Family: "fixture", Version: "1", ParamSize: "397b"}}
	res := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 1 << 50}})
	if len(res.Rows) != 0 {
		t.Errorf("a zero-instance entity produced %d rows, want 0", len(res.Rows))
	}
	if res.EntitiesDerived != 0 || res.EntitiesExcluded != 0 {
		t.Errorf("derived=%d excluded=%d, want 0/0: an unserved entity is neither",
			res.EntitiesDerived, res.EntitiesExcluded)
	}
	if res.EntitiesConsidered != 1 {
		t.Errorf("EntitiesConsidered = %d, want 1: it was still considered", res.EntitiesConsidered)
	}
}

// TestFitOver_MeasuredEntity_NeverAlsoDerives pins the mutual exclusion: an entity with
// any ingested quant row yields only measured rows, so one table never shows a file size
// and an estimate for the same artifact at the same quantization.
func TestFitOver_MeasuredEntity_NeverAlsoDerives(t *testing.T) {
	e := measuredEntity("fixture-model", fitBoundaryInput{
		WeightsBytes: 1 << 30, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 8192,
	})
	e.Ref.ParamSize = "30b" // an attested total is available, and must not be used
	res := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 1 << 50}})
	if res.EntitiesMeasured != 1 || res.EntitiesDerived != 0 {
		t.Fatalf("measured=%d derived=%d, want 1/0", res.EntitiesMeasured, res.EntitiesDerived)
	}
	for _, r := range res.Rows {
		if r.WeightsBasis != bestiary.BasisMeasured {
			t.Errorf("a measured entity produced a %s row", r.WeightsBasis)
		}
	}
}

// ---- Budget, headroom, and the two non-fit readings -------------------------

// TestFitBudget_Available pins the headroom arithmetic, including the floor that keeps a
// headroom larger than the budget from inverting every comparison.
func TestFitBudget_Available(t *testing.T) {
	cases := []struct {
		total, headroom, want int64
	}{
		{24 << 30, 2 << 30, 22 << 30},
		{24 << 30, 0, 24 << 30},
		{2 << 30, 4 << 30, 0},
		{0, 0, 0},
	}
	for _, c := range cases {
		got := bestiary.FitBudget{TotalBytes: c.total, HeadroomBytes: c.headroom}.Available()
		if got != c.want {
			t.Errorf("FitBudget{%d, %d}.Available() = %d, want %d", c.total, c.headroom, got, c.want)
		}
	}
}

// TestFitOver_WeightsOverBudget_AreNotListed pins the primary filter: a model whose
// weights do not fit is absent, not listed with a zero context.
func TestFitOver_WeightsOverBudget_AreNotListed(t *testing.T) {
	e := measuredEntity("fixture-model", fitBoundaryInput{
		WeightsBytes: 40 << 30, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 8192,
	})
	budget := bestiary.FitBudget{TotalBytes: 24 << 30, HeadroomBytes: 2 << 30}
	res := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: budget})
	if len(res.Rows) != 0 {
		t.Errorf("a 40 GiB model was listed against a 22 GiB available budget")
	}
	// The headroom is load-bearing: without it the same model fits.
	res = bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{
		Budget: bestiary.FitBudget{TotalBytes: 43 << 30, HeadroomBytes: 2 << 30},
	})
	if len(res.Rows) != 1 {
		t.Errorf("got %d rows, want 1 once the budget clears weights plus headroom", len(res.Rows))
	}
}

// TestFitOver_MinContext_ExcludesUnknownAndExhausted pins the context floor: a
// reader who asked for at least N tokens is not served by a row that cannot promise one,
// whether because the KV term is not computable or because the budget is spent. Both are
// excluded by the SAME filter, and neither is presented as an unqualified fit.
func TestFitOver_MinContext_ExcludesUnknownAndExhausted(t *testing.T) {
	kvPerToken := int64(2 * 32 * 8 * 128 * 2)
	weights := int64(4) << 30

	unknown := measuredEntity("no-arch-facts", fitBoundaryInput{
		WeightsBytes: weights, ModelContext: 32768, // layers/kvHeads/headDim absent
	})
	exhausted := measuredEntity("no-kv-budget", fitBoundaryInput{
		// Weights that consume the entire available budget: the model fits and not one
		// token of context does.
		WeightsBytes: weights + 100*kvPerToken, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 32768,
	})
	roomy := measuredEntity("plenty", fitBoundaryInput{
		WeightsBytes: 1 << 30, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 32768,
	})
	ents := []bestiary.Entity{unknown, exhausted, roomy}
	budget := bestiary.FitBudget{TotalBytes: weights + 100*kvPerToken}

	unfiltered := bestiary.FitOver(ents, bestiary.FitFilter{Budget: budget})
	if len(unfiltered.Rows) != 3 {
		t.Fatalf("got %d rows with no context floor, want all 3 listed", len(unfiltered.Rows))
	}
	var sawUnknown, sawExhausted bool
	for _, r := range unfiltered.Rows {
		switch {
		case r.Bound == bestiary.ContextBoundUnknown:
			sawUnknown = true
		case r.NoContextBudget():
			sawExhausted = true
		}
	}
	if !sawUnknown {
		t.Error("the no-architecture-facts row did not report an unknown bound")
	}
	if !sawExhausted {
		t.Error("the exhausted row did not report an empty context budget")
	}

	filtered := bestiary.FitOver(ents, bestiary.FitFilter{Budget: budget, MinContext: 1})
	if len(filtered.Rows) != 1 {
		t.Fatalf("got %d rows at MinContext=1, want only the row that can promise a token", len(filtered.Rows))
	}
	if filtered.Rows[0].MaxContext < 1 || filtered.Rows[0].Bound == bestiary.ContextBoundUnknown {
		t.Errorf("the surviving row is %+v, which cannot promise a token", filtered.Rows[0])
	}
	// The denominators are a statement about the CORPUS, not about the filter: a
	// context floor changes which rows are listed, never how many entities were
	// considered or measured.
	if filtered.EntitiesConsidered != unfiltered.EntitiesConsidered ||
		filtered.EntitiesMeasured != unfiltered.EntitiesMeasured {
		t.Error("a context floor moved the coverage denominators; they describe the corpus, not the filter")
	}
}

// TestFitOver_ContextNeverChangesAWeightsFigure pins the no-writeback rule: the weights figure
// a row is ranked on is the same number at every context floor, and no baked field is
// touched by ranking.
func TestFitOver_ContextNeverChangesAWeightsFigure(t *testing.T) {
	in := fitBoundaryInput{WeightsBytes: 1 << 30, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 32768}
	e := measuredEntity("fixture-model", in)
	before := e.Instances[0].QuantVRAM[0]

	budget := bestiary.FitBudget{TotalBytes: 24 << 30, HeadroomBytes: 2 << 30}
	base := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: budget})
	for _, min := range []int{0, 1, 1024, 8192} {
		res := bestiary.FitOver([]bestiary.Entity{e}, bestiary.FitFilter{Budget: budget, MinContext: min})
		for _, r := range res.Rows {
			if r.WeightsBytes != base.Rows[0].WeightsBytes {
				t.Errorf("MinContext=%d changed the weights figure: %d != %d",
					min, r.WeightsBytes, base.Rows[0].WeightsBytes)
			}
		}
	}
	if e.Instances[0].QuantVRAM[0] != before {
		t.Error("FitOver mutated a QuantVRAM row; the fit calculator writes nothing baked")
	}
	if bestiary.VRAMFormulaVersion != 2 {
		t.Errorf("VRAMFormulaVersion = %d, want 2: the calculator adds no overhead term",
			bestiary.VRAMFormulaVersion)
	}
}

// TestFitOver_IsPureAndDeterministic pins the two properties the page depends on: the
// same input yields the same ranked output, and the input order of the entities does not
// leak into the ranking.
func TestFitOver_IsPureAndDeterministic(t *testing.T) {
	mk := func(key string, w int64) bestiary.Entity {
		return measuredEntity(key, fitBoundaryInput{
			WeightsBytes: w, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 32768,
		})
	}
	a, b, c := mk("alpha", 1<<30), mk("bravo", 2<<30), mk("charlie", 1<<30)
	f := bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 24 << 30}}

	keys := func(res bestiary.FitResult) string {
		var sb strings.Builder
		for _, r := range res.Rows {
			fmt.Fprintf(&sb, "%s|%s|%s|%d;", r.Ref.String(), r.Provider, r.Quant, r.WeightsBytes)
		}
		return sb.String()
	}
	forward := keys(bestiary.FitOver([]bestiary.Entity{a, b, c}, f))
	reversed := keys(bestiary.FitOver([]bestiary.Entity{c, b, a}, f))
	if forward != reversed {
		t.Errorf("ranking depends on input order:\n forward  = %s\n reversed = %s", forward, reversed)
	}
	if again := keys(bestiary.FitOver([]bestiary.Entity{a, b, c}, f)); again != forward {
		t.Error("FitOver is not deterministic across identical calls")
	}
	// Largest weights first: the strongest thing the budget can run heads the list.
	rows := bestiary.FitOver([]bestiary.Entity{a, b, c}, f).Rows
	for i := 1; i < len(rows); i++ {
		if rows[i-1].WeightsBytes < rows[i].WeightsBytes {
			t.Fatalf("row %d (%d bytes) outranks row %d (%d bytes)",
				i, rows[i].WeightsBytes, i-1, rows[i-1].WeightsBytes)
		}
	}
}

// ---- The enums ---------------------------------------------------------------

// TestWeightsBasis_ZeroIsMeasured pins the enum-zero choice: a value that never saw this
// type reads as a measured, ingested file size, which is what every pre-existing
// QuantVRAM row is.
func TestWeightsBasis_ZeroIsMeasured(t *testing.T) {
	var zero bestiary.WeightsBasis
	if zero != bestiary.BasisMeasured {
		t.Errorf("the zero WeightsBasis is %v, want BasisMeasured", zero)
	}
	if got := zero.String(); got != "measured" {
		t.Errorf("zero WeightsBasis renders %q, want %q", got, "measured")
	}
	if got := bestiary.BasisDerived.String(); got != "derived" {
		t.Errorf("BasisDerived renders %q, want %q", got, "derived")
	}
	if got := bestiary.WeightsBasis(99).String(); got != "weightsbasis(99)" {
		t.Errorf("an out-of-range basis renders %q; it must never masquerade as measured", got)
	}
}

// TestContextBound_Strings pins the labels, including the out-of-range rendering that
// keeps an unexpected value from being displayed as a legitimate bound.
func TestContextBound_Strings(t *testing.T) {
	var zero bestiary.ContextBound
	if zero != bestiary.ContextBoundUnknown {
		t.Errorf("the zero ContextBound is %v, want ContextBoundUnknown", zero)
	}
	for bound, want := range map[bestiary.ContextBound]string{
		bestiary.ContextBoundUnknown: "unknown",
		bestiary.ContextBoundBudget:  "budget",
		bestiary.ContextBoundModel:   "model",
		bestiary.ContextBound(42):    "contextbound(42)",
	} {
		if got := bound.String(); got != want {
			t.Errorf("ContextBound(%d).String() = %q, want %q", int(bound), got, want)
		}
	}
}

// ---- The coverage identity ----------------------------------------------------

// TestFitOver_CoverageIdentity asserts the coverage identity over an injected corpus whose
// composition the test owns: each denominator equals the count of entities satisfying
// its own predicate, and the four classes partition the corpus with the unsized and
// unserved remainder accounted for. No count in this test is a literal read off the
// shipped catalog.
func TestFitOver_CoverageIdentity(t *testing.T) {
	arch := fitBoundaryInput{WeightsBytes: 1 << 30, Layers: 32, KVHeads: 8, HeadDim: 128, ModelContext: 32768}
	ents := []bestiary.Entity{
		measuredEntity("measured-one", arch),
		measuredEntity("measured-two", arch),
		sizedEntity(bestiary.EntityRef{Family: "d1", Version: "1", ParamSize: "7b"}, 32768),
		sizedEntity(bestiary.EntityRef{Family: "d2", Version: "1", ParamSize: "30b"}, 32768),
		sizedEntity(bestiary.EntityRef{Family: "d3", Version: "1", ParamSize: "70b"}, 32768),
		sizedEntity(bestiary.EntityRef{Family: "x1", Version: "1", ParamSize: "8x7b"}, 32768),
		sizedEntity(bestiary.EntityRef{Family: "x2", Version: "1", ParamSize: "17b-16e"}, 32768),
		sizedEntity(bestiary.EntityRef{Family: "unsized", Version: "1"}, 32768),
		{Ref: bestiary.EntityRef{Family: "unserved", Version: "1", ParamSize: "397b"}},
	}
	res := bestiary.FitOver(ents, bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 1 << 50}})

	// Each denominator recomputed from its own predicate over the SAME slice.
	wantConsidered := len(ents)
	wantMeasured, wantDerived, wantExcluded, wantNeither := 0, 0, 0, 0
	for _, e := range ents {
		measured := false
		for _, inst := range e.Instances {
			if len(inst.QuantVRAM) > 0 {
				measured = true
			}
		}
		total, attested := int64(0), false
		if e.Ref.ParamSize != "" {
			if shape, err := bestiary.ParseParamShape(e.Ref.ParamSize); err == nil &&
				shape.TotalParams != bestiary.ParamShapeNull && shape.TotalParams > 0 {
				total, attested = shape.TotalParams, true
			}
		}
		_ = total
		switch {
		case measured:
			wantMeasured++
		case len(e.Instances) == 0:
			wantNeither++
		case attested:
			wantDerived++
		case e.Ref.ParamSize != "":
			wantExcluded++
		default:
			wantNeither++
		}
	}
	if res.EntitiesConsidered != wantConsidered {
		t.Errorf("EntitiesConsidered = %d, want %d", res.EntitiesConsidered, wantConsidered)
	}
	if res.EntitiesMeasured != wantMeasured {
		t.Errorf("EntitiesMeasured = %d, want %d", res.EntitiesMeasured, wantMeasured)
	}
	if res.EntitiesDerived != wantDerived {
		t.Errorf("EntitiesDerived = %d, want %d", res.EntitiesDerived, wantDerived)
	}
	if res.EntitiesExcluded != wantExcluded {
		t.Errorf("EntitiesExcluded = %d, want %d", res.EntitiesExcluded, wantExcluded)
	}
	// The partition: the four classes plus the unsized/unserved remainder are the whole
	// corpus, so the published sentence can never describe more entities than existed.
	if sum := res.EntitiesMeasured + res.EntitiesDerived + res.EntitiesExcluded + wantNeither; sum != res.EntitiesConsidered {
		t.Errorf("the coverage classes sum to %d, want the considered total %d", sum, res.EntitiesConsidered)
	}
}

// TestFit_ShippedRegistry_CoverageIsAnIdentity runs the same identity over the SHIPPED
// registry. It asserts no literal count: only that each denominator equals its own
// predicate over Entities(), that the classes never over-count, and that every excluded
// entity really is one the parser refused a total for. It therefore stays true at any
// corpus size.
func TestFit_ShippedRegistry_CoverageIsAnIdentity(t *testing.T) {
	ents := bestiary.Entities()
	res := bestiary.Fit(bestiary.FitFilter{Budget: bestiary.FitBudget{TotalBytes: 24 << 30, HeadroomBytes: 2 << 30}})

	if res.EntitiesConsidered != len(ents) {
		t.Errorf("EntitiesConsidered = %d, want len(Entities()) = %d", res.EntitiesConsidered, len(ents))
	}
	measured, derived, excluded := 0, 0, 0
	for _, e := range ents {
		hasQuant := false
		for _, inst := range e.Instances {
			if len(inst.QuantVRAM) > 0 {
				hasQuant = true
			}
		}
		if hasQuant {
			measured++
			continue
		}
		if len(e.Instances) == 0 || e.Ref.ParamSize == "" {
			continue
		}
		shape, err := bestiary.ParseParamShape(e.Ref.ParamSize)
		if err == nil && shape.TotalParams != bestiary.ParamShapeNull && shape.TotalParams > 0 {
			derived++
			continue
		}
		excluded++
	}
	if res.EntitiesMeasured != measured {
		t.Errorf("EntitiesMeasured = %d, want %d recomputed from Entities()", res.EntitiesMeasured, measured)
	}
	if res.EntitiesDerived != derived {
		t.Errorf("EntitiesDerived = %d, want %d recomputed from Entities()", res.EntitiesDerived, derived)
	}
	if res.EntitiesExcluded != excluded {
		t.Errorf("EntitiesExcluded = %d, want %d recomputed from Entities()", res.EntitiesExcluded, excluded)
	}
	if res.EntitiesMeasured+res.EntitiesDerived+res.EntitiesExcluded > res.EntitiesConsidered {
		t.Error("the coverage classes over-count the corpus")
	}
	// Non-vacuity: the shipped corpus really does exercise all three classes, so a
	// future change that silently emptied one would be caught here rather than passing
	// a set of trivially-equal zeros.
	if res.EntitiesMeasured == 0 || res.EntitiesDerived == 0 || res.EntitiesExcluded == 0 {
		t.Errorf("a coverage class is empty in the shipped corpus (measured=%d derived=%d excluded=%d); the identity is vacuous",
			res.EntitiesMeasured, res.EntitiesDerived, res.EntitiesExcluded)
	}
}
