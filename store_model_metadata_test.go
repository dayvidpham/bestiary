package bestiary_test

import (
	"context"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// ptrF64 is a small helper for building *float64 test values.
func ptrF64(v float64) *float64 { return &v }

// fullMetadataModel returns a ModelInfo carrying ALL the v0.2.5 instance-level
// fields (description, status, reasoning options, audio costs, context-tier
// pricing) at non-zero values, so a store round-trip proves every one persists.
func fullMetadataModel(id string) bestiary.ModelInfo {
	m := testModel(id, bestiary.ProviderAnthropic)
	m.Description = "a full model description"
	m.Status = bestiary.StatusBeta
	m.ReasoningOptions = []bestiary.ReasoningOption{
		{Kind: bestiary.ReasoningToggle},
		{Kind: bestiary.ReasoningEffort, Values: []string{"low", "medium", "high"}},
		{Kind: bestiary.ReasoningBudgetTokens, MinTokens: 1024, MaxTokens: 32000},
	}
	m.CostInputAudioPerMTok = ptrF64(0.5)
	m.CostOutputAudioPerMTok = ptrF64(1.5)
	m.CostContextOver200k = &bestiary.TierCost{
		CostInputPerMTok:  ptrF64(6.0),
		CostOutputPerMTok: ptrF64(22.5),
	}
	m.CostTiers = []bestiary.CostTier{
		{ContextSize: 200000, TierCost: bestiary.TierCost{CostInputPerMTok: ptrF64(6.0), CostOutputPerMTok: ptrF64(22.5)}},
		{ContextSize: 1000000, TierCost: bestiary.TierCost{CostInputPerMTok: ptrF64(9.0)}},
	}
	return m
}

// TestModelMetadataFields_RoundTrip is the falsifiable proof that the new
// instance-level ModelInfo fields survive UpsertModels -> QueryModel unchanged.
// Before the store gained columns for them, every one of these assertions fails
// (the queried model carries the zero value).
func TestModelMetadataFields_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	m := fullMetadataModel("meta-fields-model")
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	if got.Description != m.Description {
		t.Errorf("Description = %q, want %q", got.Description, m.Description)
	}
	if got.Status != bestiary.StatusBeta {
		t.Errorf("Status = %v, want beta", got.Status)
	}
	if got.StatusRaw != "" {
		t.Errorf("StatusRaw = %q, want empty for a named status", got.StatusRaw)
	}

	// Reasoning options: kind discriminant + payload fields.
	if len(got.ReasoningOptions) != 3 {
		t.Fatalf("ReasoningOptions len = %d, want 3", len(got.ReasoningOptions))
	}
	if got.ReasoningOptions[0].Kind != bestiary.ReasoningToggle {
		t.Errorf("ReasoningOptions[0].Kind = %v, want toggle", got.ReasoningOptions[0].Kind)
	}
	if got.ReasoningOptions[1].Kind != bestiary.ReasoningEffort ||
		len(got.ReasoningOptions[1].Values) != 3 || got.ReasoningOptions[1].Values[2] != "high" {
		t.Errorf("ReasoningOptions[1] = %+v, want effort with [low medium high]", got.ReasoningOptions[1])
	}
	if got.ReasoningOptions[2].Kind != bestiary.ReasoningBudgetTokens ||
		got.ReasoningOptions[2].MinTokens != 1024 || got.ReasoningOptions[2].MaxTokens != 32000 {
		t.Errorf("ReasoningOptions[2] = %+v, want budget_tokens 1024..32000", got.ReasoningOptions[2])
	}

	// Audio costs (nullable REAL).
	if got.CostInputAudioPerMTok == nil || *got.CostInputAudioPerMTok != 0.5 {
		t.Errorf("CostInputAudioPerMTok = %v, want 0.5", got.CostInputAudioPerMTok)
	}
	if got.CostOutputAudioPerMTok == nil || *got.CostOutputAudioPerMTok != 1.5 {
		t.Errorf("CostOutputAudioPerMTok = %v, want 1.5", got.CostOutputAudioPerMTok)
	}

	// context_over_200k tier.
	if got.CostContextOver200k == nil {
		t.Fatalf("CostContextOver200k = nil, want a tier")
	}
	if got.CostContextOver200k.CostInputPerMTok == nil || *got.CostContextOver200k.CostInputPerMTok != 6.0 {
		t.Errorf("CostContextOver200k.CostInputPerMTok = %v, want 6.0", got.CostContextOver200k.CostInputPerMTok)
	}
	if got.CostContextOver200k.CostOutputPerMTok == nil || *got.CostContextOver200k.CostOutputPerMTok != 22.5 {
		t.Errorf("CostContextOver200k.CostOutputPerMTok = %v, want 22.5", got.CostContextOver200k.CostOutputPerMTok)
	}

	// Cost tiers.
	if len(got.CostTiers) != 2 {
		t.Fatalf("CostTiers len = %d, want 2", len(got.CostTiers))
	}
	if got.CostTiers[0].ContextSize != 200000 ||
		got.CostTiers[0].CostInputPerMTok == nil || *got.CostTiers[0].CostInputPerMTok != 6.0 {
		t.Errorf("CostTiers[0] = %+v, want ContextSize 200000 / input 6.0", got.CostTiers[0])
	}
	if got.CostTiers[1].ContextSize != 1000000 ||
		got.CostTiers[1].CostInputPerMTok == nil || *got.CostTiers[1].CostInputPerMTok != 9.0 {
		t.Errorf("CostTiers[1] = %+v, want ContextSize 1000000 / input 9.0", got.CostTiers[1])
	}
	// The 1M tier's other axes were unset — they must remain nil, not 0.
	if got.CostTiers[1].CostOutputPerMTok != nil {
		t.Errorf("CostTiers[1].CostOutputPerMTok = %v, want nil (unset)", got.CostTiers[1].CostOutputPerMTok)
	}
}

// TestModelStatusOther_RoundTrip proves the StatusOther fail-safe survives with
// its verbatim raw token — the whole reason StatusRaw exists (an unknown-but-
// present upstream status must never be dropped or downgraded to StatusNone).
func TestModelStatusOther_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	m := testModel("status-other-model", bestiary.ProviderOpenAI)
	m.Status = bestiary.StatusOther
	m.StatusRaw = "experimental-preview"
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.Status != bestiary.StatusOther {
		t.Errorf("Status = %v, want other", got.Status)
	}
	if got.StatusRaw != "experimental-preview" {
		t.Errorf("StatusRaw = %q, want %q (verbatim upstream token preserved)", got.StatusRaw, "experimental-preview")
	}
}

// TestModelStatusNone_RoundTrip proves the default status (StatusNone) round-trips
// as none with no spurious raw token.
func TestModelStatusNone_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	m := testModel("status-none-model", bestiary.ProviderOpenAI) // Status left zero
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	got, err := s.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.Status != bestiary.StatusNone || got.StatusRaw != "" {
		t.Errorf("Status/StatusRaw = %v/%q, want none/empty", got.Status, got.StatusRaw)
	}
}

// TestMergeModels_SyncedRoundTripKeepsStatus is the integration guard for the bug
// this fix addresses: a synced row round-tripped through the store must NOT lose
// its status, because being fresher it wins MergeModels — so a lost status would
// make `list --status` degrade to empty after any sync. It seeds a baked row with
// a status, round-trips a synced copy through the store (which stamps a fresher
// LastSynced), merges them, and asserts the fresher (cached) winner keeps status.
func TestMergeModels_SyncedRoundTripKeepsStatus(t *testing.T) {
	ctx := context.Background()
	s := openMemStore(t)

	// Baked/static row with a status and an OLD LastSynced.
	static := testModel("merge-status-model", bestiary.ProviderAnthropic)
	static.Status = bestiary.StatusBeta
	static.LastSynced = "2020-01-01T00:00:00Z"

	// Persist a synced copy — UpsertModels stamps a fresh (newer) LastSynced.
	synced := static
	if err := s.UpsertModels(ctx, []bestiary.ModelInfo{synced}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	cached, err := s.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}

	merged := bestiary.MergeModels([]bestiary.ModelInfo{static}, cached)

	var found *bestiary.ModelInfo
	for i := range merged {
		if merged[i].ID == static.ID && merged[i].Provider == static.Provider {
			found = &merged[i]
		}
	}
	if found == nil {
		t.Fatalf("merged result missing %s/%s", static.ID, static.Provider)
	}
	// The cached (fresher) row wins the merge; it must STILL carry the status.
	if found.Status != bestiary.StatusBeta {
		t.Errorf("merged Status = %v, want beta — a synced row must not erase status on the store round-trip", found.Status)
	}
	if found.LastSynced <= static.LastSynced {
		t.Errorf("expected the fresher cached row to win the merge (LastSynced %q should exceed %q)", found.LastSynced, static.LastSynced)
	}
}
