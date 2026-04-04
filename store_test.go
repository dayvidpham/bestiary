package bestiary_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// testModel returns a ModelInfo suitable for round-trip testing.
func testModel(id string, provider bestiary.Provider) bestiary.ModelInfo {
	return bestiary.ModelInfo{
		ID:            bestiary.ModelID(id),
		Provider:      provider,
		DisplayName:   "Test " + id,
		Family:        "test-family",
		ContextWindow: 128000,
		MaxOutput:     4096,
		Reasoning:     true,
		ToolCall:      true,
		Modalities: bestiary.Modalities{
			Input:  []bestiary.Modality{bestiary.ModalityText, bestiary.ModalityImage},
			Output: []bestiary.Modality{bestiary.ModalityText},
		},
		LastSynced: "2026-01-01T00:00:00Z",
	}
}

// openMemStore opens an in-memory SQLite store and registers cleanup.
func openMemStore(t *testing.T) *bestiary.Store {
	t.Helper()
	s, err := bestiary.OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Store.Close: %v", err)
		}
	})
	return s
}

// TestOpenStore_CreatesTable verifies that OpenStore succeeds and Close returns nil.
func TestOpenStore_CreatesTable(t *testing.T) {
	_ = openMemStore(t)
}

// TestUpsertQueryModels_RoundTrip inserts 3 models and reads them all back.
func TestUpsertQueryModels_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	input := []bestiary.ModelInfo{
		testModel("model-a", bestiary.ProviderAnthropic),
		testModel("model-b", bestiary.ProviderGoogle),
		testModel("model-c", bestiary.ProviderOpenAI),
	}

	if err := s.UpsertModels(ctx, input); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("QueryModels returned %d models, want 3", len(got))
	}

	// Build a map for order-independent comparison.
	byID := make(map[bestiary.ModelID]bestiary.ModelInfo, len(got))
	for _, m := range got {
		byID[m.ID] = m
	}

	for _, want := range input {
		m, ok := byID[want.ID]
		if !ok {
			t.Errorf("model %s not found in results", want.ID)
			continue
		}
		if m.Provider != want.Provider {
			t.Errorf("model %s: Provider = %q, want %q", m.ID, m.Provider, want.Provider)
		}
		if m.DisplayName != want.DisplayName {
			t.Errorf("model %s: DisplayName = %q, want %q", m.ID, m.DisplayName, want.DisplayName)
		}
		if m.Family != want.Family {
			t.Errorf("model %s: Family = %q, want %q", m.ID, m.Family, want.Family)
		}
		if m.ContextWindow != want.ContextWindow {
			t.Errorf("model %s: ContextWindow = %d, want %d", m.ID, m.ContextWindow, want.ContextWindow)
		}
		if m.MaxOutput != want.MaxOutput {
			t.Errorf("model %s: MaxOutput = %d, want %d", m.ID, m.MaxOutput, want.MaxOutput)
		}
		if m.Reasoning != want.Reasoning {
			t.Errorf("model %s: Reasoning = %v, want %v", m.ID, m.Reasoning, want.Reasoning)
		}
		if m.ToolCall != want.ToolCall {
			t.Errorf("model %s: ToolCall = %v, want %v", m.ID, m.ToolCall, want.ToolCall)
		}
	}
}

// TestQueryModels_ProviderFilter verifies that filtering by provider returns only
// matching models, while an empty provider string returns all.
func TestQueryModels_ProviderFilter(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{
		testModel("claude-a", bestiary.ProviderAnthropic),
		testModel("claude-b", bestiary.ProviderAnthropic),
		testModel("gemini-a", bestiary.ProviderGoogle),
	}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	t.Run("anthropic filter", func(t *testing.T) {
		got, err := s.QueryModels(ctx, bestiary.ProviderAnthropic)
		if err != nil {
			t.Fatalf("QueryModels: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d models, want 2", len(got))
		}
		for _, m := range got {
			if m.Provider != bestiary.ProviderAnthropic {
				t.Errorf("expected anthropic model but got provider %q", m.Provider)
			}
		}
	})

	t.Run("empty provider returns all", func(t *testing.T) {
		got, err := s.QueryModels(ctx, "")
		if err != nil {
			t.Fatalf("QueryModels: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d models, want 3", len(got))
		}
	})
}

// TestQueryModel_ByID checks that the correct model is returned by ID.
func TestQueryModel_ByID(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	want := testModel("target-model", bestiary.ProviderOpenAI)
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{
		testModel("other-model", bestiary.ProviderGoogle),
		want,
	}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, bestiary.ModelID("target-model"))
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Provider != want.Provider {
		t.Errorf("Provider = %q, want %q", got.Provider, want.Provider)
	}
}

// TestQueryModel_NotFound verifies that querying a missing ID returns ErrNotFound.
func TestQueryModel_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	_, err := s.QueryModel(ctx, bestiary.ModelID("no-such-model"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var notFound *bestiary.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *ErrNotFound, got %T: %v", err, err)
	}
	if notFound.What != "model" {
		t.Errorf("ErrNotFound.What = %q, want %q", notFound.What, "model")
	}
	if notFound.Key != "no-such-model" {
		t.Errorf("ErrNotFound.Key = %q, want %q", notFound.Key, "no-such-model")
	}
}

// TestFloat64_NilRoundTrip verifies that nil *float64 cost fields survive a
// upsert + query cycle as nil.
func TestFloat64_NilRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	m := testModel("nil-costs", bestiary.ProviderAnthropic)
	// All cost fields are nil by default from testModel; confirm explicitly.
	m.CostInputPerMTok = nil
	m.CostOutputPerMTok = nil
	m.CostReasoningPerMTok = nil
	m.CostCacheReadPerMTok = nil
	m.CostCacheWritePerMTok = nil

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.CostInputPerMTok != nil {
		t.Errorf("CostInputPerMTok: got %v, want nil", *got.CostInputPerMTok)
	}
	if got.CostOutputPerMTok != nil {
		t.Errorf("CostOutputPerMTok: got %v, want nil", *got.CostOutputPerMTok)
	}
	if got.CostReasoningPerMTok != nil {
		t.Errorf("CostReasoningPerMTok: got %v, want nil", *got.CostReasoningPerMTok)
	}
	if got.CostCacheReadPerMTok != nil {
		t.Errorf("CostCacheReadPerMTok: got %v, want nil", *got.CostCacheReadPerMTok)
	}
	if got.CostCacheWritePerMTok != nil {
		t.Errorf("CostCacheWritePerMTok: got %v, want nil", *got.CostCacheWritePerMTok)
	}
}

// TestFloat64_NonNilRoundTrip verifies that non-nil *float64 cost fields survive
// a upsert + query cycle with the correct values.
func TestFloat64_NonNilRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	costIn := 3.00
	costOut := 15.00
	costReason := 12.50
	costCacheRead := 0.30
	costCacheWrite := 3.75

	m := testModel("priced-model", bestiary.ProviderAnthropic)
	m.CostInputPerMTok = &costIn
	m.CostOutputPerMTok = &costOut
	m.CostReasoningPerMTok = &costReason
	m.CostCacheReadPerMTok = &costCacheRead
	m.CostCacheWritePerMTok = &costCacheWrite

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	assertFloat := func(name string, want float64, got *float64) {
		t.Helper()
		if got == nil {
			t.Errorf("%s: got nil, want %v", name, want)
			return
		}
		if *got != want {
			t.Errorf("%s: got %v, want %v", name, *got, want)
		}
	}
	assertFloat("CostInputPerMTok", costIn, got.CostInputPerMTok)
	assertFloat("CostOutputPerMTok", costOut, got.CostOutputPerMTok)
	assertFloat("CostReasoningPerMTok", costReason, got.CostReasoningPerMTok)
	assertFloat("CostCacheReadPerMTok", costCacheRead, got.CostCacheReadPerMTok)
	assertFloat("CostCacheWritePerMTok", costCacheWrite, got.CostCacheWritePerMTok)
}

// TestModalities_RoundTrip verifies that input/output modality slices are
// serialised and parsed correctly through the comma-separated TEXT columns.
func TestModalities_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	m := testModel("modal-model", bestiary.ProviderGoogle)
	m.Modalities = bestiary.Modalities{
		Input:  []bestiary.Modality{bestiary.ModalityText, bestiary.ModalityImage, bestiary.ModalityPDF},
		Output: []bestiary.Modality{bestiary.ModalityText, bestiary.ModalityAudio},
	}

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	if len(got.Modalities.Input) != 3 {
		t.Errorf("Input modalities: got %v (len %d), want len 3", got.Modalities.Input, len(got.Modalities.Input))
	} else {
		if got.Modalities.Input[0] != bestiary.ModalityText {
			t.Errorf("Input[0] = %v, want text", got.Modalities.Input[0])
		}
		if got.Modalities.Input[1] != bestiary.ModalityImage {
			t.Errorf("Input[1] = %v, want image", got.Modalities.Input[1])
		}
		if got.Modalities.Input[2] != bestiary.ModalityPDF {
			t.Errorf("Input[2] = %v, want pdf", got.Modalities.Input[2])
		}
	}

	if len(got.Modalities.Output) != 2 {
		t.Errorf("Output modalities: got %v (len %d), want len 2", got.Modalities.Output, len(got.Modalities.Output))
	} else {
		if got.Modalities.Output[0] != bestiary.ModalityText {
			t.Errorf("Output[0] = %v, want text", got.Modalities.Output[0])
		}
		if got.Modalities.Output[1] != bestiary.ModalityAudio {
			t.Errorf("Output[1] = %v, want audio", got.Modalities.Output[1])
		}
	}
}

// TestUpsertModels_SecondUpsertUpdates verifies that upserting the same model ID
// twice results in the second version being stored.
func TestUpsertModels_SecondUpsertUpdates(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	first := testModel("updatable", bestiary.ProviderAnthropic)
	first.DisplayName = "Original Name"
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{first}); err != nil {
		t.Fatalf("first UpsertModels: %v", err)
	}

	second := testModel("updatable", bestiary.ProviderAnthropic)
	second.DisplayName = "Updated Name"
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{second}); err != nil {
		t.Fatalf("second UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, bestiary.ModelID("updatable"))
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.DisplayName != "Updated Name" {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Updated Name")
	}

	// Confirm there is only one row (not two).
	all, err := s.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("QueryModels returned %d rows after double-upsert, want 1", len(all))
	}
}
