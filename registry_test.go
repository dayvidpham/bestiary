package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestStaticModels_NonEmpty verifies that StaticModels returns a non-empty
// slice after codegen has produced models_static_gen.go.
// NOTE: This test will fail until go generate ./... has been run.
func TestStaticModels_NonEmpty(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Fatal("StaticModels: expected non-empty slice; got zero entries — run 'go generate ./...' to produce models_static_gen.go")
	}
}

// TestStaticModels_DefensiveCopy verifies that modifying the slice returned by
// StaticModels does not affect subsequent calls (defensive copy contract).
func TestStaticModels_DefensiveCopy(t *testing.T) {
	first := bestiary.StaticModels()
	if len(first) == 0 {
		t.Skip("skipping defensive-copy test: StaticModels returned empty slice (codegen not yet run)")
	}

	originalLen := len(first)
	// Truncate the returned slice — should not affect the registry.
	first = first[:0]

	second := bestiary.StaticModels()
	if len(second) != originalLen {
		t.Fatalf("StaticModels: defensive copy broken — after truncating first result, second call returned %d entries (expected %d)",
			len(second), originalLen)
	}
}

// TestLookupModel_Known verifies that a model known to be present in the
// static registry can be retrieved by ID.
// NOTE: This test will fail until go generate ./... has been run.
func TestLookupModel_Known(t *testing.T) {
	// Pick a model ID that is stable in the Anthropic catalogue.
	const wantID bestiary.ModelID = "claude-opus-4-6-20250514"

	info, ok := bestiary.LookupModel(wantID)
	if !ok {
		// Provide a more helpful message by listing available IDs if the
		// registry is non-empty but the model wasn't found.
		all := bestiary.StaticModels()
		if len(all) == 0 {
			t.Fatalf("LookupModel(%q): not found — static registry is empty; run 'go generate ./...' first", wantID)
		}
		t.Fatalf("LookupModel(%q): not found in static registry (%d models); check the generated model ID list", wantID, len(all))
	}
	if info.ID != wantID {
		t.Fatalf("LookupModel(%q): returned model has ID %q", wantID, info.ID)
	}
}

// TestLookupModel_Unknown verifies that looking up a non-existent model
// returns the zero value and false.
func TestLookupModel_Unknown(t *testing.T) {
	info, ok := bestiary.LookupModel("nonexistent-model")
	if ok {
		t.Fatalf("LookupModel(\"nonexistent-model\"): expected (zero, false); got (model=%+v, ok=true)", info)
	}
	// ModelInfo contains Modalities which holds slices — not directly comparable.
	// Check the discriminating fields that will be set on any real model.
	if info.ID != "" || info.Provider != "" || info.DisplayName != "" {
		t.Fatalf("LookupModel(\"nonexistent-model\"): expected zero ModelInfo; got non-zero fields: ID=%q Provider=%q DisplayName=%q",
			info.ID, info.Provider, info.DisplayName)
	}
}

// TestModelsByProvider verifies that ModelsByProvider(ProviderAnthropic)
// returns only Anthropic models, and that each result has the correct provider.
// NOTE: This test will fail until go generate ./... has been run.
func TestModelsByProvider(t *testing.T) {
	models := bestiary.ModelsByProvider(bestiary.ProviderAnthropic)
	if len(models) == 0 {
		t.Fatal("ModelsByProvider(ProviderAnthropic): expected non-empty slice; got zero entries — run 'go generate ./...' first")
	}
	for _, m := range models {
		if m.Provider != bestiary.ProviderAnthropic {
			t.Errorf("ModelsByProvider(ProviderAnthropic): got model %q with provider %q (want %q)",
				m.ID, m.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// TestModelsByProvider_Empty verifies that filtering by an unknown provider
// returns an empty (nil/zero-length) slice without panicking.
func TestModelsByProvider_Empty(t *testing.T) {
	models := bestiary.ModelsByProvider(bestiary.Provider("openrouter"))
	if len(models) != 0 {
		t.Fatalf("ModelsByProvider(\"openrouter\"): expected empty slice; got %d entries", len(models))
	}
}

// TestModelsByFamily verifies that ModelsByFamily returns only models with the
// given family string, and that all results match.
// NOTE: This test will fail until go generate ./... has been run.
func TestModelsByFamily(t *testing.T) {
	// "claude-opus" is a stable family in the Anthropic catalogue.
	const wantFamily = "claude-opus"

	models := bestiary.ModelsByFamily(wantFamily)
	if len(models) == 0 {
		all := bestiary.StaticModels()
		if len(all) == 0 {
			t.Fatalf("ModelsByFamily(%q): static registry is empty; run 'go generate ./...' first", wantFamily)
		}
		t.Fatalf("ModelsByFamily(%q): got zero results from non-empty registry (%d models); check the family name", wantFamily, len(all))
	}
	for _, m := range models {
		if m.Family != wantFamily {
			t.Errorf("ModelsByFamily(%q): got model %q with family %q", wantFamily, m.ID, m.Family)
		}
	}
}

// TestStaticModels_HaveLastSynced verifies that every model in the static
// registry has a non-empty LastSynced field.
// NOTE: This test will fail until go generate ./... has been run.
func TestStaticModels_HaveLastSynced(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Fatal("StaticModels: expected non-empty slice; run 'go generate ./...' first")
	}
	for _, m := range models {
		if m.LastSynced == "" {
			t.Errorf("model %q has empty LastSynced — codegen must set LastSynced to the generation timestamp", m.ID)
		}
	}
}
