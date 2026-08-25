package bestiary

import (
	"sort"
	"strconv"
)

// FitBudget is a reader's hardware VRAM budget together with the headroom they want
// left unspent. Both are PRESENTATION-LAYER view state: they are never persisted,
// never baked into a QuantVRAM row, and never reach VRAMBytes. VRAMFormulaVersion is
// unaffected by anything in this file.
//
// Headroom exists because the shipped formula deliberately carries NO runtime-overhead
// term (see vram.go: the 1 GiB constant was removed at v0.2.4 because an invented
// constant is not a fact). A fit verdict computed from a lower bound would answer "yes"
// for models that run out of memory in practice, so the slack is exposed as a number the
// reader owns and can see, rather than smuggled into the stored datum.
type FitBudget struct {
	// TotalBytes is the reader's total VRAM in bytes.
	TotalBytes int64
	// HeadroomBytes is the slack the reader wants left unspent, in bytes.
	HeadroomBytes int64
}

// Available returns the bytes a model may actually occupy: TotalBytes minus
// HeadroomBytes, floored at 0. A headroom larger than the budget yields 0 (nothing
// fits) rather than a negative capacity that would silently invert every comparison.
func (b FitBudget) Available() int64 {
	avail := b.TotalBytes - b.HeadroomBytes
	if avail < 0 {
		return 0
	}
	return avail
}

// ContextBound names WHICH limit produced a row's max affordable context. It is
// reported alongside MaxContext because "32,768 tokens" means two different things
// depending on whether the budget or the model ran out first: the first is improved by
// buying VRAM, the second is not.
type ContextBound int

const (
	// ContextBoundUnknown means the KV-cache term is not computable for this row
	// because at least one architecture fact (layers, KV heads, head dim) is absent.
	// A row in this state reports NO context figure at all — never an unbounded one:
	// an absent fact is not an infinite budget.
	ContextBoundUnknown ContextBound = iota
	// ContextBoundBudget means the VRAM budget ran out before the model's own context
	// window did.
	ContextBoundBudget
	// ContextBoundModel means the model's own context window ran out before the budget
	// did: the row can afford every token the model accepts.
	ContextBoundModel
)

// String returns a short lowercase label for the bound. An out-of-range value renders
// as "contextbound(<n>)" so an unexpected value is never silently displayed as a
// legitimate one.
func (c ContextBound) String() string {
	switch c {
	case ContextBoundUnknown:
		return "unknown"
	case ContextBoundBudget:
		return "budget"
	case ContextBoundModel:
		return "model"
	}
	return "contextbound(" + strconv.Itoa(int(c)) + ")"
}

// WeightsBasis names WHERE a row's weights figure came from.
//
// BasisMeasured sits at zero deliberately (the enum-zero-is-the-safe-default
// convention used elsewhere in this package): a QuantVRAM row that was deserialized
// without ever seeing this type IS a measured, ingested file size, so the zero value
// tells the truth about existing data.
//
// This enum is the instrument that lets a DERIVED weights figure exist without
// weakening the ingested one: see the invariant amendment recorded in vram.go and
// entity.go. A derived figure is never written into QuantVRAM and never feeds
// VRAMBytes.
type WeightsBasis int

const (
	// BasisMeasured is the ingested GGUF file size — a ground-truth measurement.
	BasisMeasured WeightsBasis = iota
	// BasisDerived is TotalParams x BitsPerWeight(quant) / 8 — an ESTIMATE computed at
	// display time from an attested total parameter count. It is never a file size and
	// is never stored.
	BasisDerived
)

// String returns a short lowercase label for the basis. An out-of-range value renders
// as "weightsbasis(<n>)" rather than masquerading as "measured".
func (w WeightsBasis) String() string {
	switch w {
	case BasisMeasured:
		return "measured"
	case BasisDerived:
		return "derived"
	}
	return "weightsbasis(" + strconv.Itoa(int(w)) + ")"
}

// FitRow is one (entity, provider, quantization) candidate that fits a budget, together
// with the largest context it can afford and which limit produced that context.
//
// A row exists ONLY when its weights fit: WeightsBytes <= FitBudget.Available(). The
// context figure is secondary — a model whose weights do not fit cannot be rescued by
// shortening the prompt.
type FitRow struct {
	// Ref is the entity this row belongs to.
	Ref EntityRef
	// Provider is the provider instance the row was computed from. A derived row
	// attaches per instance exactly as a measured row does, because a basis is a
	// per-row fact: the same artifact on the same entity is measured on some instances
	// and unmeasured on others.
	Provider Provider
	// Quant is the quantization this row is priced at.
	Quant Quantization
	// QuantRaw is the verbatim upstream quant token for a MEASURED row, preserving the
	// original casing. It is empty on a DERIVED row: nothing upstream spelled that
	// token, and inventing a spelling would fabricate provenance.
	QuantRaw string
	// WeightsBytes is the weights footprint this row was ranked on. Read it together
	// with WeightsBasis: it is an ingested file size or an estimate, never ambiguously
	// either.
	WeightsBytes int64
	// WeightsBasis names where WeightsBytes came from.
	WeightsBasis WeightsBasis
	// KVBytesPerToken is the per-token KV-cache cost, 0 when any architecture fact is
	// absent. 0 is the sole cause of ContextBoundUnknown.
	KVBytesPerToken int64
	// ModelContext is the model's own context window on this instance, in tokens; 0
	// when the instance publishes none. It is the second term of the MaxContext
	// minimum and is carried so a reader can see WHY a model-bound row stopped where
	// it did without a second lookup.
	ModelContext int
	// MaxContext is the largest context (tokens) the row can afford. It is 0 when
	// Bound is ContextBoundUnknown (not computable) AND when a computable KV budget is
	// exhausted; the two cases are distinguished by Bound, never by the number.
	MaxContext int
	// Bound names which limit produced MaxContext.
	Bound ContextBound
	// Partial is true when the KV-cache term was omitted, so the VRAM figure is a
	// weights-only lower bound. It mirrors QuantVRAM.VRAMEstimatePartial on a measured
	// row and is ALWAYS true on a derived row.
	Partial bool
}

// NoContextBudget reports whether the row has a computable KV term whose budget is
// exhausted: the weights fit, but not one token of context does. This is a materially
// different answer from ContextBoundUnknown ("we cannot say") and neither is a fit.
func (r FitRow) NoContextBudget() bool {
	return r.Bound != ContextBoundUnknown && r.MaxContext <= 0
}

// FitFilter is the reader's query: a budget plus an optional floor on usable context.
type FitFilter struct {
	// Budget is the hardware budget and its headroom.
	Budget FitBudget
	// MinContext is a floor on MaxContext in tokens; 0 imposes no constraint. A
	// positive value excludes BOTH the not-computable rows (ContextBoundUnknown) and
	// the exhausted ones: a reader who asked for at least N tokens has not been served
	// by a row that cannot promise one.
	MinContext int
}

// FitResult is the ranked rows plus the four coverage denominators.
//
// The denominators exist so the page can STATE its own coverage instead of implying
// completeness it does not have. The published sentence is rendered from these fields
// at request time; it never carries a literal count, because a literal goes stale the
// first time the corpus moves.
type FitResult struct {
	// Rows is the ranked, filtered candidate set.
	Rows []FitRow
	// EntitiesConsidered is the number of entities the query ran over.
	EntitiesConsidered int
	// EntitiesMeasured is the number of considered entities carrying at least one
	// QuantVRAM row on at least one instance.
	EntitiesMeasured int
	// EntitiesDerived is the number of considered entities that produced at least one
	// DERIVED row: no measured row anywhere, at least one instance, and an attested
	// TotalParams.
	EntitiesDerived int
	// EntitiesExcluded is the number of considered entities that carry a parameter-size
	// token but whose TotalParams is ParamShapeNull, so no honest total exists to
	// derive from. These are the mixture-of-experts shapes upstream publishes without a
	// total: the NxM tokens (whose product parse.go deliberately refuses to compute)
	// and the Nb-Ke tokens (which carry only an active count).
	EntitiesExcluded int
}

// Fit ranks the whole static registry against a budget. It is the thin convenience
// wrapper; FitOver is the testable seam.
func Fit(f FitFilter) FitResult {
	return FitOver(Entities(), f)
}

// FitOver ranks the entities it is GIVEN against a budget. It is pure: it reads only
// its arguments, mutates nothing (not the entities, not any QuantVRAM row), and returns
// the same result for the same input.
//
// Per entity:
//   - An entity with at least one QuantVRAM row anywhere is MEASURED: every one of its
//     quant rows becomes a candidate, with WeightsBytes the ingested file size exactly.
//   - Otherwise the entity may be DERIVED: it needs at least one instance (a derived
//     figure hanging off an entity no provider serves would describe nothing a reader
//     could run) and an attested TotalParams on its own size token. Each instance then
//     yields one candidate per quantization with a non-zero BitsPerWeight.
//   - A sized entity whose TotalParams is ParamShapeNull is EXCLUDED, not zeroed: the
//     sentinel says upstream published no total, and deriving from ActiveParams instead
//     understates residency by more than an order of magnitude.
//
// A candidate is kept only when its weights fit the available budget and it satisfies
// MinContext.
func FitOver(entities []Entity, f FitFilter) FitResult {
	avail := f.Budget.Available()
	res := FitResult{EntitiesConsidered: len(entities)}

	for _, e := range entities {
		switch {
		case entityIsMeasured(e):
			res.EntitiesMeasured++
			res.Rows = appendKept(res.Rows, measuredRows(e), avail, f.MinContext)
		case len(e.Instances) == 0:
			// A metadata-only synthesized standalone: no provider serves it, so there
			// is no deployment for a fit verdict to be about. It is neither derived nor
			// excluded-for-shape; it is simply not a candidate.
		default:
			shape, ok := attestedTotalParams(e.Ref.ParamSize)
			switch {
			case ok:
				rows := derivedRows(e, shape)
				if len(rows) > 0 {
					res.EntitiesDerived++
				}
				res.Rows = appendKept(res.Rows, rows, avail, f.MinContext)
			case e.Ref.ParamSize != "":
				res.EntitiesExcluded++
			}
		}
	}

	sortFitRows(res.Rows)
	return res
}

// attestedTotalParams reports the total parameter count a size token attests, and
// whether one exists at all. A token the parser could not read, an empty token, and the
// ParamShapeNull sentinel all return ok=false — the three ways of saying "upstream did
// not publish a total", which is a refusal to derive, not a zero.
func attestedTotalParams(sizeToken string) (int64, bool) {
	if sizeToken == "" {
		return 0, false
	}
	shape, err := ParseParamShape(sizeToken)
	if err != nil {
		return 0, false
	}
	if shape.TotalParams == ParamShapeNull || shape.TotalParams <= 0 {
		return 0, false
	}
	return shape.TotalParams, true
}

// entityIsMeasured reports whether any instance of e carries an ingested quant row.
// Measurement is checked at ENTITY level even though a basis is a per-row fact: an
// entity with real file sizes on some instances must not also sprout estimates on the
// others, because the two would sit side by side in one table describing the same
// artifact at the same quantization with different numbers.
func entityIsMeasured(e Entity) bool {
	for _, inst := range e.Instances {
		if len(inst.QuantVRAM) > 0 {
			return true
		}
	}
	return false
}

// measuredRows builds one candidate per ingested quant row. WeightsBytes is carried
// through verbatim — this function performs no weights arithmetic of any kind.
func measuredRows(e Entity) []FitRow {
	var out []FitRow
	for _, inst := range e.Instances {
		for _, q := range inst.QuantVRAM {
			out = append(out, FitRow{
				Ref:             e.Ref,
				Provider:        inst.Provider,
				Quant:           q.Quant,
				QuantRaw:        q.QuantRaw,
				WeightsBytes:    q.WeightsBytes,
				WeightsBasis:    BasisMeasured,
				KVBytesPerToken: kvBytesPerToken(q.Layers, q.KVHeads, q.HeadDim),
				ModelContext:    inst.ContextWindow,
				Partial:         q.VRAMEstimatePartial,
			})
		}
	}
	return out
}

// derivedRows builds one candidate per (instance, derivable quantization) for an entity
// with an attested total parameter count and no measured row anywhere.
//
// Every derived row is Partial with KVBytesPerToken 0. That is not a shortcut: no
// unmeasured entity in the corpus carries layers / KV heads / head dim from any source,
// because every architecture fact present comes from the same curated file that carries
// the measured file sizes. The omitted KV term is routinely larger than a small model's
// entire weights, so the qualification is load-bearing, not decorative.
func derivedRows(e Entity, totalParams int64) []FitRow {
	var out []FitRow
	for _, inst := range e.Instances {
		for _, q := range derivableQuants() {
			bytes, ok := DerivedWeightsBytes(totalParams, q)
			if !ok {
				continue
			}
			out = append(out, FitRow{
				Ref:          e.Ref,
				Provider:     inst.Provider,
				Quant:        q,
				WeightsBytes: bytes,
				WeightsBasis: BasisDerived,
				ModelContext: inst.ContextWindow,
				Partial:      true,
			})
		}
	}
	return out
}

// DerivedWeightsBytes estimates a weights footprint from an attested TOTAL parameter
// count and a quantization's bits-per-weight.
//
// It returns ok=false rather than a wrong number whenever the figure would be
// dishonest: when totalParams is ParamShapeNull or non-positive (upstream published no
// total), or when BitsPerWeight is 0 — the six members none, awq, gptq, int8, int4 and
// other, whose effective bits-per-weight is configuration-dependent and not ingested. A
// zero-byte row would read as "fits in any budget", which is the opposite of what an
// absent fact means.
//
// It NEVER writes to QuantVRAM and never feeds VRAMBytes; see the invariant amendment
// in vram.go.
func DerivedWeightsBytes(totalParams int64, q Quantization) (int64, bool) {
	if totalParams == ParamShapeNull || totalParams <= 0 {
		return 0, false
	}
	bpw := q.BitsPerWeight()
	if bpw == 0 {
		return 0, false
	}
	return int64(float64(totalParams) * bpw / 8.0), true
}

// derivableQuants returns the quantization members with a non-zero bits-per-weight, in
// enum order. It is DERIVED from the enum and its bits-per-weight table rather than
// hand-listed, so a member added or re-costed upstream is picked up (or excluded) by
// the same fact that documents it, with no second list to forget.
func derivableQuants() []Quantization {
	out := make([]Quantization, 0, int(QuantizationOther)+1)
	for q := QuantizationNone; q <= QuantizationOther; q++ {
		if q.BitsPerWeight() != 0 {
			out = append(out, q)
		}
	}
	return out
}

// kvBytesPerToken returns the KV-cache cost of ONE context token, or 0 when any
// architecture fact is absent. It is the per-token factor of the shipped
// EstimateVRAMBytes KV term, extracted so the two cannot drift: KV at n tokens is
// exactly n * kvBytesPerToken, which is what the round-trip test asserts against
// EstimateVRAMBytes itself.
func kvBytesPerToken(layers, kvHeads, headDim int) int64 {
	if layers <= 0 || kvHeads <= 0 || headDim <= 0 {
		return 0
	}
	return int64(2) * int64(layers) * int64(kvHeads) * int64(headDim) * VRAMKVElemBytes
}

// appendKept filters candidates against the budget and the context floor and appends
// the survivors.
func appendKept(dst []FitRow, rows []FitRow, avail int64, minContext int) []FitRow {
	for _, r := range rows {
		if r.WeightsBytes <= 0 || r.WeightsBytes > avail {
			continue
		}
		r.MaxContext, r.Bound = maxAffordableContext(r.WeightsBytes, r.KVBytesPerToken, avail, r.ModelContext)
		if minContext > 0 && (r.Bound == ContextBoundUnknown || r.MaxContext < minContext) {
			continue
		}
		dst = append(dst, r)
	}
	return dst
}

// maxAffordableContext computes the largest context a row can afford and names which
// limit produced it.
//
// With no computable KV term the answer is ContextBoundUnknown with a zero figure: an
// absent architecture fact is not an unbounded context budget, and reporting one would
// turn missing data into a promise.
//
// Otherwise the figure is min(budget tokens, the model's own window). The budget term
// is floor((available - weights) / kvBytesPerToken), which is exactly the largest m
// with weights + m*kvBytesPerToken <= available. A zero result with a computable term
// is a real answer -- the weights fit and no context does -- and is reported as such
// rather than filtered away silently.
func maxAffordableContext(weights, kvPerToken, avail int64, modelContext int) (int, ContextBound) {
	if kvPerToken <= 0 {
		return 0, ContextBoundUnknown
	}
	budgetTokens := (avail - weights) / kvPerToken
	if budgetTokens < 0 {
		budgetTokens = 0
	}
	if modelContext > 0 && int64(modelContext) <= budgetTokens {
		return modelContext, ContextBoundModel
	}
	return int(budgetTokens), ContextBoundBudget
}

// sortFitRows imposes the ranking: largest weights first, so the strongest thing the
// budget can actually run is at the top, then a total tie-break on the row's identity
// (entity key, provider, quantization, basis). The tie-break is total, so the order is
// deterministic for any input and does not depend on the order entities arrived in.
func sortFitRows(rows []FitRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.WeightsBytes != b.WeightsBytes {
			return a.WeightsBytes > b.WeightsBytes
		}
		ak, bk := a.Ref.String(), b.Ref.String()
		if ak != bk {
			return ak < bk
		}
		if a.Provider != b.Provider {
			return a.Provider < b.Provider
		}
		if a.Quant != b.Quant {
			return a.Quant < b.Quant
		}
		return a.WeightsBasis < b.WeightsBasis
	})
}
