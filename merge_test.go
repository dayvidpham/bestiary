package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// helper: create a minimal ModelInfo with just ID and LastSynced set.
func mkModel(id, lastSynced string) bestiary.ModelInfo {
	return bestiary.ModelInfo{
		ID:         bestiary.ModelID(id),
		LastSynced: lastSynced,
	}
}

// helper: create a minimal ModelInfo with ID, Provider, and LastSynced set.
func mkModelWithProvider(id, provider, lastSynced string) bestiary.ModelInfo {
	return bestiary.ModelInfo{
		ID:         bestiary.ModelID(id),
		Provider:   bestiary.Provider(provider),
		LastSynced: lastSynced,
	}
}

func TestMergeModels_CachedWins(t *testing.T) {
	static := []bestiary.ModelInfo{mkModel("a", "2024-01-01T00:00:00Z")}
	cached := []bestiary.ModelInfo{mkModel("a", "2024-06-01T00:00:00Z")}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	if result[0].LastSynced != "2024-06-01T00:00:00Z" {
		t.Errorf("expected cached (newer) to win, got LastSynced=%q", result[0].LastSynced)
	}
}

func TestMergeModels_StaticWins(t *testing.T) {
	static := []bestiary.ModelInfo{mkModel("a", "2024-06-01T00:00:00Z")}
	cached := []bestiary.ModelInfo{mkModel("a", "2024-01-01T00:00:00Z")}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 1 {
		t.Fatalf("expected 1 model, got %d", len(result))
	}
	if result[0].LastSynced != "2024-06-01T00:00:00Z" {
		t.Errorf("expected static (newer) to win, got LastSynced=%q", result[0].LastSynced)
	}
}

func TestMergeModels_EmptyStatic(t *testing.T) {
	static := []bestiary.ModelInfo{}
	cached := []bestiary.ModelInfo{
		mkModel("a", "2024-01-01T00:00:00Z"),
		mkModel("b", "2024-02-01T00:00:00Z"),
	}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 2 {
		t.Fatalf("expected 2 models from cached, got %d", len(result))
	}
}

func TestMergeModels_EmptyCached(t *testing.T) {
	static := []bestiary.ModelInfo{
		mkModel("a", "2024-01-01T00:00:00Z"),
		mkModel("b", "2024-02-01T00:00:00Z"),
	}
	cached := []bestiary.ModelInfo{}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 2 {
		t.Fatalf("expected 2 models from static, got %d", len(result))
	}
}

func TestMergeModels_BothEmpty(t *testing.T) {
	result := bestiary.MergeModels(nil, nil)

	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d models", len(result))
	}
}

func TestMergeModels_NoOverlap(t *testing.T) {
	static := []bestiary.ModelInfo{mkModel("a", "2024-01-01T00:00:00Z")}
	cached := []bestiary.ModelInfo{mkModel("b", "2024-01-01T00:00:00Z")}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 2 {
		t.Fatalf("expected 2 models (no overlap), got %d", len(result))
	}
	ids := make(map[bestiary.ModelID]bool)
	for _, m := range result {
		ids[m.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("expected both IDs present, got %v", ids)
	}
}

func TestMergeModels_TimestampTie(t *testing.T) {
	ts := "2024-01-01T00:00:00Z"
	static := []bestiary.ModelInfo{mkModel("a", ts)}
	cached := []bestiary.ModelInfo{mkModel("a", ts)}

	result := bestiary.MergeModels(static, cached)

	// Exactly one entry expected — either wins is acceptable.
	if len(result) != 1 {
		t.Fatalf("expected 1 model on timestamp tie, got %d", len(result))
	}
	if result[0].ID != "a" {
		t.Errorf("expected model ID 'a', got %q", result[0].ID)
	}
}

// TestMergeModels_SameIDDifferentProvider verifies that two models sharing the
// same model ID but belonging to different providers are treated as distinct
// entries and both survive the merge (no deduplication across providers).
func TestMergeModels_SameIDDifferentProvider(t *testing.T) {
	ts := "2024-01-01T00:00:00Z"
	static := []bestiary.ModelInfo{
		mkModelWithProvider("shared-model", "anthropic", ts),
	}
	cached := []bestiary.ModelInfo{
		mkModelWithProvider("shared-model", "openai", ts),
	}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 2 {
		t.Fatalf("expected 2 models (same ID, different providers), got %d", len(result))
	}
	providers := make(map[bestiary.Provider]bool)
	for _, m := range result {
		if m.ID != "shared-model" {
			t.Errorf("unexpected model ID %q, want 'shared-model'", m.ID)
		}
		providers[m.Provider] = true
	}
	if !providers["anthropic"] {
		t.Error("expected provider 'anthropic' in result")
	}
	if !providers["openai"] {
		t.Error("expected provider 'openai' in result")
	}
}

// TestMergeModels_SameIDSameProviderDedup verifies that two models with the
// same (ID, Provider) pair are deduplicated, with the more recent one winning.
func TestMergeModels_SameIDSameProviderDedup(t *testing.T) {
	static := []bestiary.ModelInfo{
		mkModelWithProvider("m", "anthropic", "2024-01-01T00:00:00Z"),
	}
	cached := []bestiary.ModelInfo{
		mkModelWithProvider("m", "anthropic", "2024-06-01T00:00:00Z"),
	}

	result := bestiary.MergeModels(static, cached)

	if len(result) != 1 {
		t.Fatalf("expected 1 model (same ID+provider deduped), got %d", len(result))
	}
	if result[0].LastSynced != "2024-06-01T00:00:00Z" {
		t.Errorf("expected cached (newer) to win, got LastSynced=%q", result[0].LastSynced)
	}
}
