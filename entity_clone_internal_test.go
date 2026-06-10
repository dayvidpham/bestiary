package bestiary

// entity_clone_internal_test.go — internal tests that call the unexported
// clone helpers (cloneQuantVRAM, cloneInstances, cloneEntity) directly with
// POPULATED fixtures. These tests are mutation-sensitive: the following mutants
// must all cause failures:
//   - cloneQuantVRAM body replaced with `return in` (returns aliased slice)
//   - the `c.QuantVRAM = cloneQuantVRAM(...)` assignment deleted from cloneInstances
//   - the Sources copy block deleted from cloneEntity

import "testing"

// sampleQuantVRAMRows returns a two-element QuantVRAM slice used as a
// consistent populated fixture across clone tests.
func sampleQuantVRAMRows() []QuantVRAM {
	return []QuantVRAM{
		{
			Quant:               QuantQ4_K_M,
			QuantRaw:            "Q4_K_M",
			WeightsBytes:        42_500_000_000,
			VRAMBytes:           46_000_000_000,
			VRAMContextTokens:   131072,
			Layers:              80,
			KVHeads:             8,
			HeadDim:             128,
			VRAMEstimatePartial: false,
		},
		{
			Quant:               QuantQ8_0,
			QuantRaw:            "Q8_0",
			WeightsBytes:        74_000_000_000,
			VRAMBytes:           74_000_000_000,
			VRAMContextTokens:   32768,
			VRAMEstimatePartial: true, // arch facts absent
		},
	}
}

// TestCloneQuantVRAM_Internal_MutationIsolation calls cloneQuantVRAM directly
// and verifies bidirectional mutation isolation: mutating the clone does not
// affect the original, and mutating the original does not affect the clone.
//
// Mutant killers:
//   - `return in` body: clone shares the original's backing array, so
//     cloned[0].WeightsBytes mutation propagates back — original check fails.
func TestCloneQuantVRAM_Internal_MutationIsolation(t *testing.T) {
	original := sampleQuantVRAMRows()
	cloned := cloneQuantVRAM(original)

	if len(cloned) != len(original) {
		t.Fatalf("cloneQuantVRAM: len(cloned)=%d, want %d", len(cloned), len(original))
	}

	// Verify values match before any mutation.
	for i, row := range original {
		if cloned[i] != row {
			t.Errorf("cloneQuantVRAM: row[%d] value mismatch before mutation: got %+v, want %+v", i, cloned[i], row)
		}
	}

	// Mutate clone → original must be unchanged.
	cloned[0].WeightsBytes = 1
	cloned[1].VRAMEstimatePartial = false
	if original[0].WeightsBytes != 42_500_000_000 {
		t.Errorf("cloneQuantVRAM: mutating clone[0].WeightsBytes changed original[0].WeightsBytes to %d; clone must not alias original",
			original[0].WeightsBytes)
	}
	if !original[1].VRAMEstimatePartial {
		t.Error("cloneQuantVRAM: mutating clone[1].VRAMEstimatePartial changed original[1]; clone must not alias original")
	}

	// Mutate original → clone must be unchanged.
	original[0].VRAMBytes = 2
	if cloned[0].VRAMBytes != 46_000_000_000 {
		t.Errorf("cloneQuantVRAM: mutating original[0].VRAMBytes changed cloned[0].VRAMBytes to %d; original must not alias clone",
			cloned[0].VRAMBytes)
	}
}

// TestCloneQuantVRAM_Internal_NilPreserved verifies cloneQuantVRAM(nil) returns nil,
// matching the nil-means-absent convention.
func TestCloneQuantVRAM_Internal_NilPreserved(t *testing.T) {
	if got := cloneQuantVRAM(nil); got != nil {
		t.Errorf("cloneQuantVRAM(nil) = %v, want nil", got)
	}
}

// TestCloneInstances_Internal_QuantVRAMMutationIsolation calls cloneInstances
// directly with a populated QuantVRAM slice and verifies that mutating the
// clone's QuantVRAM rows does not affect the original slice.
//
// Mutant killers:
//   - deleting `c.QuantVRAM = cloneQuantVRAM(inst.QuantVRAM)` from cloneInstances:
//     clone and original share the same QuantVRAM backing array, so the
//     mutation check below fails.
func TestCloneInstances_Internal_QuantVRAMMutationIsolation(t *testing.T) {
	original := []ProviderInstance{
		{
			ID:        "test-model",
			Provider:  "testprovider",
			Source:    DataSourceOllama,
			QuantVRAM: sampleQuantVRAMRows(),
		},
	}
	cloned := cloneInstances(original)

	if len(cloned) != 1 {
		t.Fatalf("cloneInstances: len=%d, want 1", len(cloned))
	}
	if len(cloned[0].QuantVRAM) != 2 {
		t.Fatalf("cloneInstances: cloned[0].QuantVRAM len=%d, want 2", len(cloned[0].QuantVRAM))
	}

	// Mutate clone QuantVRAM → original must be unchanged.
	cloned[0].QuantVRAM[0].WeightsBytes = 999
	if original[0].QuantVRAM[0].WeightsBytes != 42_500_000_000 {
		t.Errorf("cloneInstances: mutating clone QuantVRAM[0].WeightsBytes changed original to %d; QuantVRAM clone must be independent",
			original[0].QuantVRAM[0].WeightsBytes)
	}

	// Mutate original QuantVRAM → clone must be unchanged.
	original[0].QuantVRAM[1].VRAMBytes = 888
	if cloned[0].QuantVRAM[1].VRAMBytes != 74_000_000_000 {
		t.Errorf("cloneInstances: mutating original QuantVRAM[1].VRAMBytes changed cloned to %d; original must not alias clone",
			cloned[0].QuantVRAM[1].VRAMBytes)
	}
}

// TestCloneEntity_Internal_SourcesMutationIsolation calls cloneEntity directly
// with a populated Sources slice and verifies bidirectional mutation isolation.
//
// Mutant killers:
//   - deleting the Sources copy block from cloneEntity: clone and original share
//     the same backing array, so the mutation check below fails.
func TestCloneEntity_Internal_SourcesMutationIsolation(t *testing.T) {
	original := Entity{
		Ref:     EntityRef{Family: "llama", Version: "3.3"},
		Sources: []DataSourceID{DataSourceModelsDev, DataSourceOllama},
	}
	cloned := cloneEntity(original)

	if len(cloned.Sources) != 2 {
		t.Fatalf("cloneEntity: cloned.Sources len=%d, want 2", len(cloned.Sources))
	}

	// Mutate clone Sources → original must be unchanged.
	cloned.Sources[0] = DataSourceNone
	if original.Sources[0] != DataSourceModelsDev {
		t.Errorf("cloneEntity: mutating cloned.Sources[0] changed original.Sources[0] to %q; clone must not alias original",
			original.Sources[0])
	}

	// Mutate original Sources → clone must be unchanged.
	original.Sources[1] = DataSourceNone
	if cloned.Sources[1] != DataSourceOllama {
		t.Errorf("cloneEntity: mutating original.Sources[1] changed cloned.Sources[1] to %q; original must not alias clone",
			cloned.Sources[1])
	}
}

// TestCloneEntity_Internal_SourcesNilPreserved verifies that cloneEntity
// preserves nil Sources (the zero value for entities without source data).
func TestCloneEntity_Internal_SourcesNilPreserved(t *testing.T) {
	e := Entity{Ref: EntityRef{Family: "llama"}}
	c := cloneEntity(e)
	if c.Sources != nil {
		t.Errorf("cloneEntity: nil Sources became %v after clone; must remain nil", c.Sources)
	}
}

// TestCloneInstances_Internal_QuantVRAMNilPreserved verifies cloneInstances
// preserves nil QuantVRAM slices on instances that carry none.
func TestCloneInstances_Internal_QuantVRAMNilPreserved(t *testing.T) {
	in := []ProviderInstance{
		{ID: "no-quant-model", Provider: "testprovider"},
	}
	out := cloneInstances(in)
	if len(out) != 1 {
		t.Fatalf("cloneInstances: len=%d, want 1", len(out))
	}
	if out[0].QuantVRAM != nil {
		t.Errorf("cloneInstances: nil QuantVRAM became %v after clone; must remain nil", out[0].QuantVRAM)
	}
}
