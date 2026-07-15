package bestiary

// paramsize_wiring_internal_test.go — synthesized-registry tests that verify the
// ParamSize wiring end-to-end. These tests live in package bestiary (internal) so
// they can temporarily replace staticModels and reset the entity index, making the
// wiring in registry.go, EntityByTuple, and matchCanonicalSegments falsifiable with
// controlled fixtures rather than relying solely on production data where all sizes
// are currently empty.
//
// These tests must NOT be run in parallel with each other or with any test that
// reads entity index state, because withSyntheticRegistry mutates shared package
// variables (staticModels, entityIndex, entityKeys, entityIndexOnce).
//
// These tests are designed to be mutation-sensitive: any of the following changes
// would cause a failure:
//   - dropping ParamSize from the EntityRef literal in registry.go
//   - dropping ParamSize from the EntityByTuple lookup ref in entity.go
//   - removing or inverting the paramSizeFilter check in matchCanonicalSegments
//   - swapping the #size strip to after the @ strip in matchCanonicalSegments

import (
	"sync"
	"testing"
)

// withSyntheticRegistry replaces staticModels and resets the entity index for the
// duration of fn, then restores the originals. Not safe for concurrent calls
// against the same package state — call only from t.Run subtests that are NOT
// run in parallel.
//
// sync.Once cannot be copied (go vet: "assignment copies lock value"). Instead,
// we save/restore the index state directly and reset the Once by overwriting it
// with a fresh zero value (assign, never copy). The restore also re-resets the
// Once so the original index state will be lazily rebuilt on the next real call.
func withSyntheticRegistry(t *testing.T, models []ModelInfo, fn func(t *testing.T)) {
	t.Helper()

	origModels := staticModels
	origIndex := entityIndex
	origKeys := entityKeys

	staticModels = models
	entityIndexOnce = sync.Once{} // reset — assign a new zero value; go vet allows assignment, not copy
	entityIndex = nil
	entityKeys = nil

	t.Cleanup(func() {
		staticModels = origModels
		entityIndexOnce = sync.Once{} // reset again so next real call rebuilds from origModels
		entityIndex = origIndex
		entityKeys = origKeys
	})

	fn(t)
}

// syntheticLlamaModel returns a minimal ModelInfo for a llama-3.3 row with the
// given paramSize. Family/Variant/Version are set to match what the registry
// grouping code uses; Modifier is the identity projection of ["instruct"].
func syntheticLlamaModel(paramSize string) ModelInfo {
	return ModelInfo{
		ID:        ModelID("meta-llama/Llama-3.3-" + paramSize + "-Instruct"),
		Provider:  Provider("meta"),
		Family:    Family("llama"),
		Version:   "3.3",
		Modifier:  []string{"instruct"},
		ParamSize: paramSize,
	}
}

// TestCodegen_ParamSizePrecedence pins the presence-based precedence pin > mechanical
// > ParamSizeFor that the codegen bake and both runtime enrichment joints share. It
// replaces the pre-v0.2.6 "ParamSize only from the curated table" guard: the full-bulk
// re-key now sizes every ID with a mechanical token, so the enduring invariant is the
// PRECEDENCE, not a curated-only emission. The reachable tiers are exercised against
// the real embedded tables via EnrichedParamSize; the token-less-fallback and
// disagreement paths — which no current catalog ID reaches — are exercised against the
// pure resolveParamSizePrecedence helper with injected tiers.
func TestCodegen_ParamSizePrecedence(t *testing.T) {
	t.Run("pin wins over a divergent mechanical token (llama-4 seed)", func(t *testing.T) {
		const id = "llama-4-scout-17b-instruct"
		if mech, _ := ExtractParamSizeToken(id); mech != "17b" {
			t.Fatalf("precondition: mechanical token = %q, want %q (the pin must genuinely diverge)", mech, "17b")
		}
		got, err := EnrichedParamSize(id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "17b-16e" {
			t.Errorf("EnrichedParamSize(%q) = %q, want %q (pin must override the divergent mechanical token)", id, got, "17b-16e")
		}
	})

	t.Run("suppress-pin never falls through to mechanical (non-vacuity pinned)", func(t *testing.T) {
		const id = "qwen/qwen3-coder-next-fp8-1m"
		// Non-vacuity: the extractor HONESTLY returns ("1m", true), so the suppress-pin
		// is doing real work — without it the ID would key #1m. This pins the extractor
		// result so the suppress leg can never go dead.
		if mech, ok := ExtractParamSizeToken(id); mech != "1m" || !ok {
			t.Fatalf("precondition: ExtractParamSizeToken(%q) = (%q,%v), want (\"1m\",true) — else the suppress-pin is vacuous", id, mech, ok)
		}
		got, err := EnrichedParamSize(id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Errorf("EnrichedParamSize(%q) = %q, want \"\" (suppress-pin must not fall through to mechanical)", id, got)
		}
	})

	t.Run("mechanical fills an unpinned, uncurated ID", func(t *testing.T) {
		const id = "qwen/qwen3-30b-a3b"
		if got := ParamSizeFor(ModelID(id)); got != "" {
			t.Fatalf("precondition: ParamSizeFor(%q) = %q, want \"\" (this leg must exercise the mechanical tier, not the fallback)", id, got)
		}
		got, err := EnrichedParamSize(id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "30b-a3b" {
			t.Errorf("EnrichedParamSize(%q) = %q, want %q (mechanical tier)", id, got, "30b-a3b")
		}
	})

	// Precedence-decision legs via the pure helper with injected tiers.
	t.Run("ParamSizeFor fills only when there is no mechanical token", func(t *testing.T) {
		got, err := resolveParamSizePrecedence("some/quant-tagged-ollama-id", "", false, "", false, "13b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "13b" {
			t.Errorf("token-less fallback = %q, want %q (ParamSizeFor is the last tier)", got, "13b")
		}
	})

	t.Run("mechanical outranks ParamSizeFor when both agree", func(t *testing.T) {
		got, err := resolveParamSizePrecedence("id", "", false, "8b", true, "8b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "8b" {
			t.Errorf("got %q, want %q", got, "8b")
		}
	})

	t.Run("unpinned mechanical-vs-ParamSizeFor disagreement is a loud error", func(t *testing.T) {
		got, err := resolveParamSizePrecedence("bad/id", "", false, "70b", true, "72b")
		if err == nil {
			t.Fatalf("mechanical %q != fallback %q returned no error; codegen would silently bake a contested size", "70b", "72b")
		}
		if got != "70b" {
			t.Errorf("on disagreement the value = %q, want the mechanical token %q (precedence still resolves)", got, "70b")
		}
	})

	t.Run("a present pin resolves the disagreement without error", func(t *testing.T) {
		got, err := resolveParamSizePrecedence("id", "17b-16e", true, "70b", true, "72b")
		if err != nil {
			t.Fatalf("a pin must resolve a disagreement without error, got %v", err)
		}
		if got != "17b-16e" {
			t.Errorf("got %q, want %q (pin wins)", got, "17b-16e")
		}
	})
}

// TestEnrichModelInfo_ShapeInts verifies the shared read/decode enrichment populates
// ParamSize AND the four flat shape ints from the ID in one joint, honoring the
// presence-based precedence (pin > mechanical > ParamSizeFor) and each param-shape
// family. It pins the PerExpertParams NxM case (ExpertCount + PerExpertParams, NEVER a
// total) and the suppress-pin (no size, no ints) so a regression in either the joint
// wiring or the shape decomposition is caught.
func TestEnrichModelInfo_ShapeInts(t *testing.T) {
	cases := []struct {
		name, id, wantSize                   string
		wantTotal, wantActive, wantPerExpert int64
		wantExperts                          int
	}{
		{"dense", "meta-llama/Llama-3.3-70B-Instruct", "70b", 70_000_000_000, 0, 0, 0},
		{"decimal dense", "qwen/qwen3-embedding-0.6b", "0.6b", 600_000_000, 0, 0, 0},
		{"active MoE", "qwen/qwen3-30b-a3b", "30b-a3b", 30_000_000_000, 3_000_000_000, 0, 0},
		{"NxM MoE — PerExpertParams, no total", "mistralai/mixtral-8x22b", "8x22b", 0, 0, 22_000_000_000, 8},
		{"count-suffixed MoE via curated pin", "llama-4-scout-17b-instruct", "17b-16e", 0, 17_000_000_000, 0, 16},
		{"suppress-pin yields no size, no ints", "qwen/qwen3-coder-next-fp8-1m", "", 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := ModelInfo{ID: ModelID(tc.id)}
			enrichModelInfo(&m)
			if m.ParamSize != tc.wantSize {
				t.Errorf("ParamSize = %q, want %q", m.ParamSize, tc.wantSize)
			}
			if m.TotalParams != tc.wantTotal {
				t.Errorf("TotalParams = %d, want %d", m.TotalParams, tc.wantTotal)
			}
			if m.ActiveParams != tc.wantActive {
				t.Errorf("ActiveParams = %d, want %d", m.ActiveParams, tc.wantActive)
			}
			if m.PerExpertParams != tc.wantPerExpert {
				t.Errorf("PerExpertParams = %d, want %d", m.PerExpertParams, tc.wantPerExpert)
			}
			if m.ExpertCount != tc.wantExperts {
				t.Errorf("ExpertCount = %d, want %d", m.ExpertCount, tc.wantExperts)
			}
		})
	}
}

// TestParamSize_SizedMiss guards mutation (a) and (b): if ParamSize is dropped
// from either EntityByTuple's ref or from the registry grouping, a sized lookup
// against an unsized entity would silently hit instead of miss.
//
// Scenario: registry contains ONLY an unsized llama@3.3{instruct} entity.
// A sized lookup ("70b") must MISS; the unsized lookup must HIT.
func TestParamSize_SizedMiss(t *testing.T) {
	unsizedModel := syntheticLlamaModel("") // ParamSize=""
	withSyntheticRegistry(t, []ModelInfo{unsizedModel}, func(t *testing.T) {
		// Unsized lookup must HIT.
		e, ok := EntityByTuple(Family("llama"), "", "3.3", "", "instruct")
		if !ok {
			t.Fatal("unsized EntityByTuple should HIT an unsized registry entry, got miss")
		}
		wantKey := "llama@3.3{instruct}"
		if e.Ref.String() != wantKey {
			t.Errorf("unsized entity key = %q, want %q", e.Ref.String(), wantKey)
		}

		// Sized lookup must MISS — the registry only has the unsized variant.
		_, ok = EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if ok {
			t.Error("sized EntityByTuple(70b) must MISS when registry has only the unsized entity; got a hit — wrong-merge")
		}
	})
}

// TestParamSize_SizedHit guards mutation (a) and (b) from the other direction:
// if ParamSize is dropped from the registry grouping, the sized entity would be
// keyed the same as the unsized one, and distinct sizes would wrongly merge.
//
// Scenario: registry contains ONLY a sized llama@3.3#70b{instruct} entity.
// The sized lookup must HIT; the unsized lookup must MISS.
func TestParamSize_SizedHit(t *testing.T) {
	sizedModel := syntheticLlamaModel("70b") // ParamSize="70b"
	withSyntheticRegistry(t, []ModelInfo{sizedModel}, func(t *testing.T) {
		// Sized lookup must HIT.
		e, ok := EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if !ok {
			t.Fatal("sized EntityByTuple(70b) should HIT a sized registry entry, got miss")
		}
		wantKey := "llama@3.3#70b{instruct}"
		if e.Ref.String() != wantKey {
			t.Errorf("sized entity key = %q, want %q", e.Ref.String(), wantKey)
		}

		// Unsized lookup must MISS — the registry only has the 70b variant.
		_, ok = EntityByTuple(Family("llama"), "", "3.3", "", "instruct")
		if ok {
			t.Error("unsized EntityByTuple() must MISS when registry has only the sized entity; got a hit — wrong-merge")
		}
	})
}

// TestParamSize_TwoSizesDistinct verifies the core registry correctness: two
// models identical except ParamSize produce TWO distinct entities in the index,
// each resolvable only by its own sized key.
func TestParamSize_TwoSizesDistinct(t *testing.T) {
	model70 := syntheticLlamaModel("70b")
	model8 := syntheticLlamaModel("8b")
	withSyntheticRegistry(t, []ModelInfo{model70, model8}, func(t *testing.T) {
		e70, ok70 := EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if !ok70 {
			t.Fatal("EntityByTuple(70b) must HIT; got miss")
		}
		e8, ok8 := EntityByTuple(Family("llama"), "", "3.3", "8b", "instruct")
		if !ok8 {
			t.Fatal("EntityByTuple(8b) must HIT; got miss")
		}
		if e70.Ref.String() == e8.Ref.String() {
			t.Errorf("70b and 8b entities share key %q — they must be DISTINCT", e70.Ref.String())
		}
		if e70.Ref.ParamSize != "70b" {
			t.Errorf("70b entity has ParamSize=%q, want %q", e70.Ref.ParamSize, "70b")
		}
		if e8.Ref.ParamSize != "8b" {
			t.Errorf("8b entity has ParamSize=%q, want %q", e8.Ref.ParamSize, "8b")
		}

		// Cross-size lookups must miss: asking for a size that is not in the
		// registry must not silently return the other size's entity.
		if _, crossHit := EntityByTuple(Family("llama"), "", "3.3", "1b", "instruct"); crossHit {
			t.Error("EntityByTuple(1b) must MISS when registry has only 70b and 8b; got a hit — wrong-merge")
		}

		// The two entities must carry distinct instance IDs (not pointing at the
		// same underlying ModelInfo row).
		if e70.Instances[0].ID == e8.Instances[0].ID {
			t.Error("70b and 8b entities share the same instance ID — wrong-merge")
		}
	})
}

// TestMatchCanonicalSegments_ParamSizeFilter guards mutation (d): verifies that
// the paramSizeFilter clause in matchCanonicalSegments correctly distinguishes
// a sized model from its unsized sibling.
//
// A sized input "#70b" must match a ParamSize="70b" model and NOT a ParamSize=""
// model; an unsized input must match any ParamSize (including "").
func TestMatchCanonicalSegments_ParamSizeFilter(t *testing.T) {
	sized := ModelInfo{
		Family:    Family("llama"),
		Version:   "3.3",
		ParamSize: "70b",
		Modifier:  []string{"instruct"},
		Date:      "3.3", // matchCanonicalSegments uses @ as dateFilter
	}
	unsized := ModelInfo{
		Family:    Family("llama"),
		Version:   "3.3",
		ParamSize: "",
		Modifier:  []string{"instruct"},
		Date:      "3.3",
	}

	cases := []struct {
		name      string
		input     string
		model     ModelInfo
		wantMatch bool
	}{
		// Sized input, sized model: must match.
		{name: "sized input matches sized model", input: "llama@3.3#70b{instruct}", model: sized, wantMatch: true},
		// Sized input, unsized model: must NOT match (wrong-merge guard).
		{name: "sized input does not match unsized model", input: "llama@3.3#70b{instruct}", model: unsized, wantMatch: false},
		// Wrong size: must not match.
		{name: "wrong size does not match", input: "llama@3.3#8b{instruct}", model: sized, wantMatch: false},
		// Unsized input, sized model: must match (backward-compat: no # means no filter).
		{name: "unsized input matches sized model (no paramsize filter)", input: "llama@3.3{instruct}", model: sized, wantMatch: true},
		// Unsized input, unsized model: must match.
		{name: "unsized input matches unsized model", input: "llama@3.3{instruct}", model: unsized, wantMatch: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchCanonicalSegments(tc.model, tc.input)
			if got != tc.wantMatch {
				t.Errorf("matchCanonicalSegments(model.ParamSize=%q, input=%q) = %v, want %v",
					tc.model.ParamSize, tc.input, got, tc.wantMatch)
			}
		})
	}
}

// TestMatchCanonicalSegments_StripOrderGuard verifies that the # strip happens
// BEFORE the @ split. A strip-order swap (# after @) would cause the size token
// "70b" to be parsed as part of the date filter, which would then not match
// m.Date — making the match fail even for a correctly-sized model.
func TestMatchCanonicalSegments_StripOrderGuard(t *testing.T) {
	// Model where Date = "3.3" so the @ filter matches correctly only when
	// the # segment has already been stripped first.
	m := ModelInfo{
		Family:    Family("llama"),
		Version:   "3.3",
		Date:      "3.3",
		ParamSize: "70b",
		Modifier:  []string{"instruct"},
	}
	// Input: llama@3.3#70b{instruct}
	// Correct strip order:  [attrs] → {mods} → #70b → @3.3 → llama
	// Wrong strip order: if # were stripped after @, then @ would grab "3.3#70b"
	// as the date, which does not equal m.Date="3.3", so match would fail.
	if !matchCanonicalSegments(m, "llama@3.3#70b{instruct}") {
		t.Error("matchCanonicalSegments returned false; expected true — strip-order guard failed (# must be stripped BEFORE @)")
	}
}
