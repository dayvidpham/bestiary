package bestiary_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dayvidpham/bestiary"
)

// testModel returns a ModelInfo suitable for round-trip testing.
// NormalizedFamily, NormalizedVariant, NormalizedVersion, and NormalizedDate are
// set to non-zero values so that round-trip tests prove these fields survive
// persistence.
func testModel(id string, provider bestiary.Provider) bestiary.ModelInfo {
	return bestiary.ModelInfo{
		ID:                bestiary.ModelID(id),
		Provider:          provider,
		DisplayName:       "Test " + id,
		Family:            "test-family",
		NormalizedFamily:  "test",
		NormalizedVariant: "family",
		NormalizedVersion: "",
		NormalizedDate:    "2026-01-01",
		ContextWindow:     128000,
		MaxOutput:         4096,
		Reasoning:         true,
		ToolCall:          true,
		Interleaved:       bestiary.Capability{Supported: false},
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

// TestNormalizedFields_RoundTrip verifies that NormalizedFamily, NormalizedVariant,
// and NormalizedDate survive a UpsertModels + QueryModel round-trip with non-zero
// values. This test exists because these fields were added in v3 and the base
// testModel fixture intentionally sets them; this assertion proves they are wired
// through the full persistence path.
func TestNormalizedFields_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	want := testModel("norm-model", bestiary.ProviderAnthropic)
	// Confirm testModel provides non-zero values (guard against accidental zeroing).
	if want.NormalizedFamily == "" || want.NormalizedVariant == "" || want.NormalizedDate == "" {
		t.Fatal("testModel must return non-zero NormalizedFamily/Variant/Date for this test to be meaningful")
	}

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{want}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryModel(ctx, want.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	if got.NormalizedFamily != want.NormalizedFamily {
		t.Errorf("NormalizedFamily = %q, want %q", got.NormalizedFamily, want.NormalizedFamily)
	}
	if got.NormalizedVariant != want.NormalizedVariant {
		t.Errorf("NormalizedVariant = %q, want %q", got.NormalizedVariant, want.NormalizedVariant)
	}
	if got.NormalizedDate != want.NormalizedDate {
		t.Errorf("NormalizedDate = %q, want %q", got.NormalizedDate, want.NormalizedDate)
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

// TestCapability_RoundTrip verifies that Capability values (both plain bool and
// object-with-config form) survive a upsert + query cycle unchanged.
func TestCapability_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	// Model with interleaved supported=false, no config.
	mFalse := testModel("cap-false", bestiary.ProviderAnthropic)
	mFalse.Interleaved = bestiary.Capability{Supported: false}

	// Model with interleaved supported=true, no config.
	mTrue := testModel("cap-true", bestiary.ProviderAnthropic)
	mTrue.Interleaved = bestiary.Capability{Supported: true}

	// Model with interleaved supported=true, config present.
	mConfig := testModel("cap-config", bestiary.ProviderAnthropic)
	mConfig.Interleaved = bestiary.Capability{
		Supported: true,
		Config:    map[string]string{"field": "reasoning_details"},
	}

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{mFalse, mTrue, mConfig}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	t.Run("false no config", func(t *testing.T) {
		got, err := s.QueryModel(ctx, bestiary.ModelID("cap-false"))
		if err != nil {
			t.Fatalf("QueryModel: %v", err)
		}
		if got.Interleaved.Supported {
			t.Error("Interleaved.Supported: expected false")
		}
		if got.Interleaved.Config != nil {
			t.Errorf("Interleaved.Config: expected nil, got %v", got.Interleaved.Config)
		}
	})

	t.Run("true no config", func(t *testing.T) {
		got, err := s.QueryModel(ctx, bestiary.ModelID("cap-true"))
		if err != nil {
			t.Fatalf("QueryModel: %v", err)
		}
		if !got.Interleaved.Supported {
			t.Error("Interleaved.Supported: expected true")
		}
		if got.Interleaved.Config != nil {
			t.Errorf("Interleaved.Config: expected nil, got %v", got.Interleaved.Config)
		}
	})

	t.Run("true with config", func(t *testing.T) {
		got, err := s.QueryModel(ctx, bestiary.ModelID("cap-config"))
		if err != nil {
			t.Fatalf("QueryModel: %v", err)
		}
		if !got.Interleaved.Supported {
			t.Error("Interleaved.Supported: expected true")
		}
		if got.Interleaved.Config == nil {
			t.Fatal("Interleaved.Config: expected non-nil map")
		}
		if v := got.Interleaved.Config["field"]; v != "reasoning_details" {
			t.Errorf("Interleaved.Config[\"field\"]: got %q, want \"reasoning_details\"", v)
		}
	})
}

// TestCompositeKey_SameIDDifferentProviders verifies that inserting the same
// model_id under two different providers produces two distinct rows in the
// store, and both are retrievable via QueryModels and QueryModelsByID.
func TestCompositeKey_SameIDDifferentProviders(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	anthropicModel := testModel("shared-model", bestiary.ProviderAnthropic)
	anthropicModel.DisplayName = "Shared Model (Anthropic)"

	openaiModel := testModel("shared-model", bestiary.ProviderOpenAI)
	openaiModel.DisplayName = "Shared Model (OpenAI)"

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{anthropicModel, openaiModel}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	// QueryModels with no filter should return both rows.
	all, err := s.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("QueryModels returned %d rows, want 2 (one per provider)", len(all))
	}

	// QueryModelsByID should return all providers for the given ID.
	variants, err := s.QueryModelsByID(ctx, bestiary.ModelID("shared-model"))
	if err != nil {
		t.Fatalf("QueryModelsByID: %v", err)
	}
	if len(variants) != 2 {
		t.Fatalf("QueryModelsByID returned %d rows, want 2", len(variants))
	}
	providers := make(map[bestiary.Provider]string)
	for _, m := range variants {
		providers[m.Provider] = m.DisplayName
	}
	if providers[bestiary.ProviderAnthropic] != "Shared Model (Anthropic)" {
		t.Errorf("anthropic variant: DisplayName = %q, want %q",
			providers[bestiary.ProviderAnthropic], "Shared Model (Anthropic)")
	}
	if providers[bestiary.ProviderOpenAI] != "Shared Model (OpenAI)" {
		t.Errorf("openai variant: DisplayName = %q, want %q",
			providers[bestiary.ProviderOpenAI], "Shared Model (OpenAI)")
	}
}

// TestQueryModelsByID_NotFound verifies that QueryModelsByID returns an empty
// slice (not an error) when no model with the given ID is cached.
func TestQueryModelsByID_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	variants, err := s.QueryModelsByID(ctx, bestiary.ModelID("no-such-model"))
	if err != nil {
		t.Fatalf("QueryModelsByID returned unexpected error: %v", err)
	}
	if len(variants) != 0 {
		t.Errorf("QueryModelsByID: got %d results, want 0", len(variants))
	}
}

// TestUpsertModels_EmptySlice verifies that calling UpsertModels with an empty
// slice is a no-op: it returns no error and leaves the store unchanged.
func TestUpsertModels_EmptySlice(t *testing.T) {
	ctx := context.Background()

	// Use a file-backed store in a temp dir to ensure isolation.
	dir := t.TempDir()
	path := dir + "/models.db"
	s, err := bestiary.OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Store.Close: %v", err)
		}
	})

	// Insert a known model so we can verify the store is unchanged afterwards.
	initial := []bestiary.ModelInfo{testModel("pre-existing", bestiary.ProviderAnthropic)}
	if err := s.UpsertModels(ctx, initial); err != nil {
		t.Fatalf("UpsertModels (initial): %v", err)
	}

	// Upsert with empty slice must not error.
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{}); err != nil {
		t.Fatalf("UpsertModels(empty): expected no error, got: %v", err)
	}

	// Store must still contain exactly the one pre-existing model.
	got, err := s.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("QueryModels after empty upsert: got %d models, want 1", len(got))
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

// TestDefaultDBPath verifies that DefaultDBPath returns an XDG-based path when
// XDG_CACHE_HOME is set, and falls back to ~/.cache/bestiary/models.db otherwise.
func TestDefaultDBPath(t *testing.T) {
	t.Run("XDG_CACHE_HOME set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)

		got, err := bestiary.DefaultDBPath()
		if err != nil {
			t.Fatalf("DefaultDBPath() returned unexpected error: %v", err)
		}
		// Path must be rooted under the XDG_CACHE_HOME we set.
		if !strings.HasPrefix(got, tmpDir) {
			t.Errorf("DefaultDBPath() = %q; want path rooted under XDG_CACHE_HOME %q", got, tmpDir)
		}
		// Path must end with the expected relative components.
		if !strings.HasSuffix(got, "bestiary/models.db") {
			t.Errorf("DefaultDBPath() = %q; want suffix %q", got, "bestiary/models.db")
		}
	})

	t.Run("XDG_CACHE_HOME unset falls back to home cache", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "")

		got, err := bestiary.DefaultDBPath()
		if err != nil {
			t.Fatalf("DefaultDBPath() returned unexpected error: %v", err)
		}
		// The fallback path must still end with the canonical sub-path.
		if !strings.HasSuffix(got, "bestiary/models.db") {
			t.Errorf("DefaultDBPath() fallback = %q; want suffix %q", got, "bestiary/models.db")
		}
		// Fallback path must pass through .cache.
		if !strings.Contains(got, ".cache") {
			t.Errorf("DefaultDBPath() fallback = %q; expected path to contain %q", got, ".cache")
		}
	})
}

// testCanonicalModel returns a ModelInfo with the given canonical normalization fields set.
// It uses testModel as a base and overrides NormalizedFamily, NormalizedVariant, NormalizedDate.
func testCanonicalModel(id string, provider bestiary.Provider, rawFamily, normFamily, variant, date string) bestiary.ModelInfo {
	m := testModel(id, provider)
	m.Family = bestiary.Family(rawFamily)
	m.NormalizedFamily = bestiary.Family(normFamily)
	m.NormalizedVariant = variant
	m.NormalizedVersion = ""
	m.NormalizedDate = date
	return m
}

// TestQueryByCanonical_CrossProvider inserts the same canonical (claude, opus, 2025-05-14)
// under 3 providers and asserts QueryByCanonical returns all 3 rows.
func TestQueryByCanonical_CrossProvider(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	models := []bestiary.ModelInfo{
		testCanonicalModel("claude-opus-4-20250514", bestiary.ProviderAnthropic, "claude-opus", "claude", "opus", "2025-05-14"),
		testCanonicalModel("claude-opus-4-20250514", bestiary.Provider("amazon"), "claude-opus", "claude", "opus", "2025-05-14"),
		testCanonicalModel("claude-opus-4-20250514", bestiary.Provider("azure"), "claude-opus", "claude", "opus", "2025-05-14"),
		// Different canonical: should NOT be returned
		testCanonicalModel("claude-sonnet-4-20250514", bestiary.ProviderAnthropic, "claude-sonnet", "claude", "sonnet", "2025-05-14"),
	}
	if err := s.UpsertModels(ctx, models); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{Family: "claude", Variant: "opus", Date: "2025-05-14"})
	if err != nil {
		t.Fatalf("QueryByCanonical: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("QueryByCanonical: got %d results, want 3", len(got))
	}
	// All results must have the same canonical.
	for _, m := range got {
		if m.NormalizedFamily != "claude" {
			t.Errorf("NormalizedFamily = %q, want %q", m.NormalizedFamily, "claude")
		}
		if m.NormalizedVariant != "opus" {
			t.Errorf("NormalizedVariant = %q, want %q", m.NormalizedVariant, "opus")
		}
		if m.NormalizedDate != "2025-05-14" {
			t.Errorf("NormalizedDate = %q, want %q", m.NormalizedDate, "2025-05-14")
		}
	}
}

// TestQueryByCanonical_NotFound verifies that a query for a non-existent canonical
// returns an empty slice and no error.
func TestQueryByCanonical_NotFound(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{
		testCanonicalModel("claude-opus-4-20250514", bestiary.ProviderAnthropic, "claude-opus", "claude", "opus", "2025-05-14"),
	}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{Family: "nonexistent", Variant: "v1", Date: "2099-01-01"})
	if err != nil {
		t.Fatalf("QueryByCanonical returned unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("QueryByCanonical: got %d results, want 0", len(got))
	}
}

// TestQueryByCanonical_PartialMatch verifies that empty params act as wildcards:
// empty variant matches all variants for a given family.
func TestQueryByCanonical_PartialMatch(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{
		testCanonicalModel("claude-opus-4-20250514", bestiary.ProviderAnthropic, "claude-opus", "claude", "opus", "2025-05-14"),
		testCanonicalModel("claude-sonnet-4-20250815", bestiary.ProviderAnthropic, "claude-sonnet", "claude", "sonnet", "2025-08-15"),
		testCanonicalModel("claude-haiku-4-20250307", bestiary.ProviderAnthropic, "claude-haiku", "claude", "haiku", "2025-03-07"),
		testCanonicalModel("gpt-4o", bestiary.ProviderOpenAI, "gpt-4o", "gpt", "4o", ""),
	}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	// Empty variant + empty date: match all claude models.
	got, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{Family: "claude"})
	if err != nil {
		t.Fatalf("QueryByCanonical(claude, empty, empty): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("QueryByCanonical(claude): got %d results, want 3", len(got))
	}
	for _, m := range got {
		if m.NormalizedFamily != "claude" {
			t.Errorf("unexpected family %q in results", m.NormalizedFamily)
		}
	}

	// Empty family + specific variant: matches any family with that variant.
	got2, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{Variant: "opus"})
	if err != nil {
		t.Fatalf("QueryByCanonical(empty, opus, empty): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("QueryByCanonical(empty, opus, empty): got %d, want 1", len(got2))
	}
	if got2[0].NormalizedVariant != "opus" {
		t.Errorf("variant = %q, want %q", got2[0].NormalizedVariant, "opus")
	}

	// All empty: returns all models.
	all, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{})
	if err != nil {
		t.Fatalf("QueryByCanonical(empty, empty, empty): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("QueryByCanonical all-empty: got %d, want 4", len(all))
	}
}

// TestQueryByCanonical_NormalDataIntegrity verifies that values returned by
// QueryByCanonical match exactly what was written by UpsertModels.
func TestQueryByCanonical_NormalDataIntegrity(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	want := testCanonicalModel("gemini-2-5-flash-preview-04-17", bestiary.ProviderGoogle,
		"gemini-2-5-flash", "gemini", "flash", "2025-04-17")
	want.DisplayName = "Gemini 2.5 Flash Preview"
	want.ContextWindow = 1048576
	want.MaxOutput = 65536

	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{want}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	results, err := s.QueryByCanonical(ctx, bestiary.CanonicalFilter{Family: "gemini", Variant: "flash", Date: "2025-04-17"})
	if err != nil {
		t.Fatalf("QueryByCanonical: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	got := results[0]

	if got.ID != want.ID {
		t.Errorf("ID = %q, want %q", got.ID, want.ID)
	}
	if got.Provider != want.Provider {
		t.Errorf("Provider = %q, want %q", got.Provider, want.Provider)
	}
	if got.DisplayName != want.DisplayName {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, want.DisplayName)
	}
	if got.Family != want.Family {
		t.Errorf("Family (raw_family) = %q, want %q", got.Family, want.Family)
	}
	if got.NormalizedFamily != want.NormalizedFamily {
		t.Errorf("NormalizedFamily = %q, want %q", got.NormalizedFamily, want.NormalizedFamily)
	}
	if got.NormalizedVariant != want.NormalizedVariant {
		t.Errorf("NormalizedVariant = %q, want %q", got.NormalizedVariant, want.NormalizedVariant)
	}
	if got.NormalizedDate != want.NormalizedDate {
		t.Errorf("NormalizedDate = %q, want %q", got.NormalizedDate, want.NormalizedDate)
	}
	if got.ContextWindow != want.ContextWindow {
		t.Errorf("ContextWindow = %d, want %d", got.ContextWindow, want.ContextWindow)
	}
	if got.MaxOutput != want.MaxOutput {
		t.Errorf("MaxOutput = %d, want %d", got.MaxOutput, want.MaxOutput)
	}
}

// TestUpsertModels_StampsLastSynced verifies that UpsertModels sets the
// LastSynced field to the current UTC time, and that the value is a valid
// RFC3339 timestamp within the last 60 seconds.
func TestUpsertModels_StampsLastSynced(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	before := time.Now().UTC().Add(-time.Second)

	m := testModel("ts-model", bestiary.ProviderAnthropic)
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	after := time.Now().UTC().Add(time.Second)

	got, err := s.QueryModel(ctx, bestiary.ModelID("ts-model"))
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	if got.LastSynced == "" {
		t.Fatal("LastSynced is empty; expected RFC3339 timestamp")
	}

	ts, err := time.Parse(time.RFC3339, got.LastSynced)
	if err != nil {
		t.Fatalf("LastSynced %q is not a valid RFC3339 timestamp: %v", got.LastSynced, err)
	}

	if ts.Before(before) || ts.After(after) {
		t.Errorf("LastSynced %q is not within the expected time window [%s, %s]",
			got.LastSynced, before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
}
