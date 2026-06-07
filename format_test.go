package bestiary_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// testModels returns a small, deterministic list of models for formatting tests.
func testModels() []bestiary.ModelInfo {
	cost := 15.0
	return []bestiary.ModelInfo{
		{
			ID:               "test-model-1",
			Provider:         "testprovider",
			DisplayName:      "Test Model 1",
			Family:           "test-family",
			ContextWindow:    128000,
			MaxOutput:        4096,
			CostInputPerMTok: &cost,
			LastSynced:       "2024-01-01T00:00:00Z",
			Modalities: bestiary.Modalities{
				Input:  []bestiary.Modality{bestiary.ModalityText, bestiary.ModalityImage},
				Output: []bestiary.Modality{bestiary.ModalityText},
			},
		},
		{
			ID:          "test-model-2",
			Provider:    "testprovider",
			DisplayName: "Test Model 2",
			Family:      "test-family",
			LastSynced:  "2024-02-01T00:00:00Z",
		},
	}
}

func TestFormatModels_JSON(t *testing.T) {
	var buf bytes.Buffer
	models := testModels()

	err := bestiary.FormatModels(&buf, models, bestiary.FormatJSON)
	if err != nil {
		t.Fatalf("FormatModels(JSON) returned error: %v", err)
	}

	output := buf.Bytes()
	if !json.Valid(output) {
		t.Errorf("FormatModels(JSON) produced invalid JSON:\n%s", output)
	}
}

func TestFormatModel_JSON(t *testing.T) {
	var buf bytes.Buffer
	model := testModels()[0]

	err := bestiary.FormatModel(&buf, model, bestiary.FormatJSON)
	if err != nil {
		t.Fatalf("FormatModel(JSON) returned error: %v", err)
	}

	output := buf.Bytes()
	if !json.Valid(output) {
		t.Errorf("FormatModel(JSON) produced invalid JSON:\n%s", output)
	}

	// Verify expected fields are present.
	s := string(output)
	for _, field := range []string{"test-model-1", "testprovider", "test-family"} {
		if !strings.Contains(s, field) {
			t.Errorf("FormatModel(JSON) output missing expected value %q", field)
		}
	}

	// Verify structural keys are present in the JSON object.
	// ModelInfo has no json tags so field names match the struct field names exactly.
	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("FormatModel(JSON): unmarshal for structural check: %v", err)
	}
	requiredKeys := []string{
		"ID",
		"Provider",
		"DisplayName",
		"Family",
		"ContextWindow",
		"MaxOutput",
		"LastSynced",
	}
	for _, key := range requiredKeys {
		if _, ok := decoded[key]; !ok {
			t.Errorf("FormatModel(JSON): expected structural key %q not found in output; present keys: %v",
				key, jsonKeysOf(decoded))
		}
	}
}

// jsonKeysOf returns the keys of m as a slice (for diagnostic messages).
func jsonKeysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestFormatModels_YAML(t *testing.T) {
	var buf bytes.Buffer
	models := testModels()

	err := bestiary.FormatModels(&buf, models, bestiary.FormatYAML)
	if err != nil {
		t.Fatalf("FormatModels(YAML) returned error: %v", err)
	}

	output := string(buf.Bytes())
	// Verify expected field names appear in the output.
	for _, field := range []string{"test-model-1", "testprovider", "ContextWindow", "LastSynced"} {
		if !strings.Contains(output, field) {
			t.Errorf("FormatModels(YAML) output missing expected field/value %q\nGot:\n%s", field, output)
		}
	}
}

func TestFormatModel_Table(t *testing.T) {
	var buf bytes.Buffer
	model := testModels()[0]

	err := bestiary.FormatModel(&buf, model, bestiary.FormatTable)
	if err != nil {
		t.Fatalf("FormatModel(Table) returned error: %v", err)
	}

	output := string(buf.Bytes())
	// Must have a header row.
	if !strings.Contains(output, "ID") {
		t.Errorf("FormatModel(Table) output missing header 'ID'\nGot:\n%s", output)
	}
	// Must contain the model ID.
	if !strings.Contains(output, "test-model-1") {
		t.Errorf("FormatModel(Table) output missing model ID 'test-model-1'\nGot:\n%s", output)
	}
}

// TestFormatModel_Table_ReasoningToolCallColumns verifies that the Reason and
// Tools columns appear in the table header and that their values are rendered
// as "yes" or "no" for boolean fields.
func TestFormatModel_Table_ReasoningToolCallColumns(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:        "reason-tool-model",
		Provider:  "testprovider",
		Reasoning: true,
		ToolCall:  false,
	}

	err := bestiary.FormatModel(&buf, model, bestiary.FormatTable)
	if err != nil {
		t.Fatalf("FormatModel(Table) returned error: %v", err)
	}

	output := string(buf.Bytes())

	// Header must include both new column names.
	if !strings.Contains(output, "Reason") {
		t.Errorf("table header missing 'Reason' column\nGot:\n%s", output)
	}
	if !strings.Contains(output, "Tools") {
		t.Errorf("table header missing 'Tools' column\nGot:\n%s", output)
	}

	// Row values: Reasoning=true → "yes", ToolCall=false → "no".
	if !strings.Contains(output, "yes") {
		t.Errorf("table row missing 'yes' for Reasoning=true\nGot:\n%s", output)
	}
	if !strings.Contains(output, "no") {
		t.Errorf("table row missing 'no' for ToolCall=false\nGot:\n%s", output)
	}
}

// TestFormatModels_Table_BothBoolColumns verifies that FormatModels renders
// Reason/Tools columns for each row in a multi-model table.
func TestFormatModels_Table_BothBoolColumns(t *testing.T) {
	var buf bytes.Buffer
	models := []bestiary.ModelInfo{
		{ID: "m1", Provider: "p", Reasoning: true, ToolCall: true},
		{ID: "m2", Provider: "p", Reasoning: false, ToolCall: false},
	}

	err := bestiary.FormatModels(&buf, models, bestiary.FormatTable)
	if err != nil {
		t.Fatalf("FormatModels(Table) returned error: %v", err)
	}

	output := string(buf.Bytes())
	if !strings.Contains(output, "Reason") {
		t.Errorf("table missing 'Reason' header\nGot:\n%s", output)
	}
	if !strings.Contains(output, "Tools") {
		t.Errorf("table missing 'Tools' header\nGot:\n%s", output)
	}
	// Both "yes" (from m1) and "no" (from m2) should appear.
	if !strings.Contains(output, "yes") {
		t.Errorf("table missing 'yes' value\nGot:\n%s", output)
	}
	if !strings.Contains(output, "no") {
		t.Errorf("table missing 'no' value\nGot:\n%s", output)
	}
}

func TestFormatModels_InvalidFormat(t *testing.T) {
	var buf bytes.Buffer
	models := testModels()

	err := bestiary.FormatModels(&buf, models, bestiary.OutputFormat("toml"))
	if err == nil {
		t.Error("FormatModels with unknown format should return an error, got nil")
	}
}

// TestFormatModel_YAML_Capability verifies that a Capability with config
// is rendered with sub-fields in YAML output.
func TestFormatModel_YAML_Capability(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          "cap-model",
		Provider:    "testprovider",
		DisplayName: "Cap Model",
		Family:      "test",
		Interleaved: bestiary.Capability{
			Supported: true,
			Config:    map[string]string{"field": "reasoning_details"},
		},
		LastSynced: "2024-01-01T00:00:00Z",
	}

	err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML)
	if err != nil {
		t.Fatalf("FormatModel(YAML) returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Interleaved:") {
		t.Errorf("YAML output missing Interleaved key\nGot:\n%s", output)
	}
	if !strings.Contains(output, "supported: true") {
		t.Errorf("YAML output missing 'supported: true'\nGot:\n%s", output)
	}
	if !strings.Contains(output, "reasoning_details") {
		t.Errorf("YAML output missing 'reasoning_details'\nGot:\n%s", output)
	}
}

// TestFormatModel_YAML_CapabilityBoolFalse verifies that a Capability with no
// config renders as a plain bool in YAML output.
func TestFormatModel_YAML_CapabilityBoolFalse(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          "cap-false-model",
		Provider:    "testprovider",
		DisplayName: "Cap False Model",
		Family:      "test",
		Interleaved: bestiary.Capability{Supported: false},
		LastSynced:  "2024-01-01T00:00:00Z",
	}

	err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML)
	if err != nil {
		t.Fatalf("FormatModel(YAML) returned error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Interleaved: false") {
		t.Errorf("YAML output should contain 'Interleaved: false'\nGot:\n%s", output)
	}
}

// TestFormatModel_YAML validates that FormatModel with FormatYAML produces output
// containing the expected top-level YAML keys for a ModelInfo fixture.
// Validation is string-based only (no yaml.v3 dependency).
func TestFormatModel_YAML(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          bestiary.ModelID("yaml-test-model"),
		Provider:    "testprovider",
		DisplayName: "YAML Test Model",
		Family:      "yaml-family",
		LastSynced:  "2024-03-01T00:00:00Z",
	}

	err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML)
	if err != nil {
		t.Fatalf("FormatModel(YAML) returned unexpected error: %v", err)
	}

	output := buf.String()
	for _, wantKey := range []string{"ID:", "Provider:", "DisplayName:", "Family:"} {
		if !strings.Contains(output, wantKey) {
			t.Errorf("FormatModel(YAML) output missing expected key %q\nGot:\n%s", wantKey, output)
		}
	}

	// Verify the model values appear in the output.
	for _, wantVal := range []string{"yaml-test-model", "testprovider", "YAML Test Model", "yaml-family"} {
		if !strings.Contains(output, wantVal) {
			t.Errorf("FormatModel(YAML) output missing expected value %q\nGot:\n%s", wantVal, output)
		}
	}
}

// TestFormatModel_YAML_CanonicalFields verifies that the YAML serializer emits
// Family, Variant, and Date alongside the other fields, matching the JSON path.
func TestFormatModel_YAML_CanonicalFields(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          bestiary.ModelID("claude-opus-4-20250514"),
		Provider:    bestiary.ProviderAnthropic,
		DisplayName: "Claude Opus 4",
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Date:        "2025-05-14",
		LastSynced:  "2025-05-14T00:00:00Z",
	}

	err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML)
	if err != nil {
		t.Fatalf("FormatModel(YAML) returned error: %v", err)
	}

	output := buf.String()
	for _, want := range []string{
		"RawFamily:", "Family:", "Variant:", "Date:",
		"claude-opus", "claude", "opus", "2025-05-14",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("FormatModel(YAML) output missing %q\nGot:\n%s", want, output)
		}
	}
}

// TestFormatModel_JSON_Version verifies that a ModelInfo with a non-empty Version
// field round-trips correctly through JSON serialization.
func TestFormatModel_JSON_Version(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          "claude-opus-4-5-20251101",
		Provider:    bestiary.ProviderAnthropic,
		DisplayName: "Claude Opus 4.5",
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Version:     "4.5",
		Date:        "2025-11-01",
		LastSynced:  "2025-11-01T00:00:00Z",
	}

	if err := bestiary.FormatModel(&buf, model, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(JSON) returned error: %v", err)
	}

	output := buf.Bytes()
	if !json.Valid(output) {
		t.Errorf("FormatModel(JSON) produced invalid JSON:\n%s", output)
	}

	var decoded map[string]any
	if err := json.Unmarshal(output, &decoded); err != nil {
		t.Fatalf("FormatModel(JSON): unmarshal failed: %v", err)
	}

	v, ok := decoded["Version"]
	if !ok {
		t.Fatalf("FormatModel(JSON): Version missing from output; present keys: %v", jsonKeysOf(decoded))
	}
	if s, ok := v.(string); !ok || s != "4.5" {
		t.Errorf("FormatModel(JSON): Version = %v (%T), want %q", v, v, "4.5")
	}
}

// --- Fix #2: ErrAmbiguous grouping + truncation ---

// makeAmbiguousRefs creates a list of ModelRefs for use in FormatAmbiguous tests.
// Generates n refs with distinct (Family, Variant, Version) tuples by default,
// unless duplicateTuples is true in which case some tuples are repeated to test grouping.
func makeAmbiguousRefs(n int, duplicateTuples bool) []bestiary.ModelRef {
	refs := make([]bestiary.ModelRef, n)
	for i := 0; i < n; i++ {
		family := fmt.Sprintf("family-%d", i)
		variant := fmt.Sprintf("variant-%d", i)
		version := fmt.Sprintf("1.%d", i)
		if duplicateTuples && i > 0 && i%3 == 0 {
			// Repeat the previous (family, variant, version) with a different provider.
			family = fmt.Sprintf("family-%d", i-1)
			variant = fmt.Sprintf("variant-%d", i-1)
			version = fmt.Sprintf("1.%d", i-1)
		}
		refs[i] = bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("model-%d", i)),
			Provider: bestiary.Provider(fmt.Sprintf("provider-%d", i)),
			Family:   bestiary.Family(family),
			Variant:  variant,
			Version:  version,
			Date:     "2025-01-01",
		}
	}
	return refs
}

// TestFormatAmbiguous_Truncation verifies that FormatAmbiguous truncates the
// canonical candidate list after N=5 and emits a "+M more" hint.
//
// Updated for  two-section layout: canonical cap is now 5.
// Uses canonical Anthropic refs (Provider==CanonicalProvider) so Section 1 is populated.
func TestFormatAmbiguous_Truncation(t *testing.T) {
	// Create 8 canonical anthropic/claude refs — after cap-5, "+3 more" expected.
	candidates := make([]bestiary.ModelRef, 8)
	for i := 0; i < 8; i++ {
		candidates[i] = bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("claude-model-%d", i)),
			Provider: bestiary.ProviderAnthropic,
			Family:   "claude",
			Variant:  fmt.Sprintf("variant-%d", i),
			Version:  "1",
			Date:     "2025-01-01",
		}
	}
	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Must contain "+3 more" (8 - 5 = 3).
	if !strings.Contains(output, "+3 more") {
		t.Errorf("FormatAmbiguous(8 canonical candidates): output should contain '+3 more' hint; got:\n%s", output)
	}
	// Must NOT list variant-5 through variant-7 (beyond cap-5).
	for i := 5; i < 8; i++ {
		if strings.Contains(output, fmt.Sprintf("variant-%d", i)) {
			t.Errorf("FormatAmbiguous: canonical candidate variant-%d should NOT appear in truncated output; got:\n%s", i, output)
		}
	}
}

// TestFormatAmbiguous_NoTruncation_ExactlyN verifies that exactly N=5 canonical
// candidates does NOT emit a truncation hint ("+M more" pattern).
//
// Updated for  two-section layout: canonical cap is now 5.
func TestFormatAmbiguous_NoTruncation_ExactlyN(t *testing.T) {
	// Exactly 5 canonical anthropic/claude refs — no truncation expected.
	candidates := make([]bestiary.ModelRef, 5)
	for i := 0; i < 5; i++ {
		candidates[i] = bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("claude-model-%d", i)),
			Provider: bestiary.ProviderAnthropic,
			Family:   "claude",
			Variant:  fmt.Sprintf("variant-%d", i),
			Version:  "1",
			Date:     "2025-01-01",
		}
	}
	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// No "+N more" truncation hint for exactly 5 canonical candidates.
	// The footer lines don't contain "+N more" patterns, so a simple check is safe.
	if strings.Contains(output, "+0 more") {
		t.Errorf("FormatAmbiguous(5 canonical candidates): should NOT have '+0 more'; got:\n%s", output)
	}
	// Specifically: no canonical overflow hint.
	lines := strings.Split(output, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "+") && strings.Contains(l, "more") {
			t.Errorf("FormatAmbiguous(5 canonical candidates): unexpected '+N more' hint: %q\nFull output:\n%s", l, output)
		}
	}
}

// TestFormatAmbiguous_Grouping verifies that FormatAmbiguous groups canonical candidates
// by (Family, Variant, Version) tuple and shows ONE row per group in Section 1.
//
// Updated for  two-section layout: grouping applies to canonical-provider
// rows. Non-canonical rows in Candidates are excluded from Section 1.
func TestFormatAmbiguous_Grouping(t *testing.T) {
	// Two distinct canonical groups (alpha and beta), with no duplicate (family,variant,version).
	// Anthropic is the canonical provider for "claude" family.
	candidates := []bestiary.ModelRef{
		{ID: "claude-alpha-1", Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "alpha", Version: "1", Date: "2025-01-01"},
		{ID: "claude-beta-1", Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "beta", Version: "1", Date: "2025-01-01"},
	}

	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Both variant names should appear in Section 1.
	// Section 1 rows are canonical form strings prefixed with "* ".
	if !strings.Contains(output, "/alpha/") {
		t.Errorf("FormatAmbiguous grouping: '/alpha/' missing from output;\nGot:\n%s", output)
	}
	if !strings.Contains(output, "/beta/") {
		t.Errorf("FormatAmbiguous grouping: '/beta/' missing from output;\nGot:\n%s", output)
	}

	// Each variant should appear exactly once (no duplicates).
	alphaCount := strings.Count(output, "/alpha/")
	betaCount := strings.Count(output, "/beta/")
	if alphaCount != 1 {
		t.Errorf("FormatAmbiguous grouping: '/alpha/' appears %d times, want 1; output:\n%s", alphaCount, output)
	}
	if betaCount != 1 {
		t.Errorf("FormatAmbiguous grouping: '/beta/' appears %d times, want 1; output:\n%s", betaCount, output)
	}
}

// TestFormatAmbiguous_GroupingAndTruncation verifies that grouping is applied
// BEFORE truncation — canonical rows with same (Family,Variant,Version) are
// deduped, then capped at 5 with "+N more".
//
// Updated for  two-section layout: canonical cap is now 5.
func TestFormatAmbiguous_GroupingAndTruncation(t *testing.T) {
	// 8 canonical anthropic/claude rows with distinct (variant, version) tuples.
	// After dedup: 8 distinct groups → cap 5 → "+3 more".
	candidates := make([]bestiary.ModelRef, 0, 8)
	for i := 0; i < 8; i++ {
		candidates = append(candidates, bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("claude-model-%d", i)),
			Provider: bestiary.ProviderAnthropic,
			Family:   "claude",
			Variant:  fmt.Sprintf("variant-%d", i),
			Version:  fmt.Sprintf("1.%d", i),
			Date:     "2025-01-01",
		})
	}

	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// After dedup (all distinct) and cap at 5: "+3 more" must appear.
	if !strings.Contains(output, "+3 more") {
		t.Errorf("FormatAmbiguous(8 canonical groups): should truncate to 5+'+3 more'; got:\n%s", output)
	}
}

// TestFormatAmbiguous_RemedHintUpdated verifies that the remediation hint in
// FormatAmbiguous now references --format=raw instead of the deprecated --scheme=raw.
func TestFormatAmbiguous_RemedHintUpdated(t *testing.T) {
	candidates := makeAmbiguousRefs(3, false)
	e := &bestiary.ErrAmbiguous{
		Input:      "test",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// The hint should reference --format=raw, not the deprecated --scheme=raw.
	if strings.Contains(output, "--scheme=raw") {
		t.Errorf("FormatAmbiguous: output should not reference deprecated --scheme=raw; got:\n%s", output)
	}
	if !strings.Contains(output, "--format") && !strings.Contains(output, "raw") {
		t.Errorf("FormatAmbiguous: output should reference --format=raw or 'raw'; got:\n%s", output)
	}
}

// ----------------------------------------------------------------------------
// Modifier field format tests
// ----------------------------------------------------------------------------

// TestFormatModel_JSON_ModifierField verifies that FormatModel(JSON) emits the
// Modifier field when non-empty.
//
// These tests will FAIL (Modifier will be missing) until ModelInfo gains the
// Modifier field AND json serialization uses it. The field is
// auto-serialized since it's an exported string field with no explicit json tag.
// Actually this test should PASS once the field is added, since Go's json.Marshal
// picks up exported string fields by their field name.
func TestFormatModel_JSON_ModifierField(t *testing.T) {
	// Test that Modifier is emitted when non-empty.
	m := bestiary.ModelInfo{
		ID:          "claude-opus-4-6-thinking",
		Provider:    "anthropic",
		DisplayName: "Claude Opus 4.6 Thinking",
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Version:     "4.6",
		Date:        "2026-02-05",
		Modifier:    []string{"thinking"},
		LastSynced:  "2026-02-05T00:00:00Z",
	}

	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, m, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(JSON) error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Unmarshal JSON output error: %v", err)
	}

	val, ok := got["Modifier"]
	if !ok {
		t.Fatal("JSON output missing 'Modifier' key; add Modifier to ModelInfo and schema")
	}
	// Modifier is a JSON array now.
	if arr, ok := val.([]interface{}); !ok || len(arr) != 1 || arr[0] != "thinking" {
		t.Errorf("Modifier = %v (%T), want [\"thinking\"]", val, val)
	}

	// Test that Modifier appears when empty (as empty string, not omitted,
	// since ModelInfo.Modifier has no `json:",omitempty"` tag).
	mEmpty := bestiary.ModelInfo{
		ID:          "gpt-4o-2024-05-13",
		Provider:    "openai",
		DisplayName: "GPT-4o",
		RawFamily:   "gpt-4o",
		Family:      "gpt",
		Variant:     "",
		Version:     "4o",
		Date:        "2024-05-13",
		Modifier:    nil,
		LastSynced:  "2024-05-13T00:00:00Z",
	}

	var buf2 bytes.Buffer
	if err := bestiary.FormatModel(&buf2, mEmpty, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(JSON) empty Modifier error: %v", err)
	}

	var got2 map[string]any
	if err := json.Unmarshal(buf2.Bytes(), &got2); err != nil {
		t.Fatalf("Unmarshal empty-modifier JSON error: %v", err)
	}
	// Modifier key should still be present (empty string "" since no omitempty).
	if _, ok := got2["Modifier"]; !ok {
		t.Error("JSON output missing 'Modifier' key even when empty; ensure Modifier field is in ModelInfo")
	}
}

// TestFormatModel_YAML_ModifierField verifies that the YAML serializer emits
// a Modifier line when Modifier is non-empty.
//
// These tests will FAIL until modelToYAML in format.go is updated to include a
// Modifier line.
func TestFormatModel_YAML_ModifierField(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:          "claude-opus-4-6-thinking",
		Provider:    "anthropic",
		DisplayName: "Claude Opus 4.6 Thinking",
		RawFamily:   "claude-opus",
		Family:      "claude",
		Variant:     "opus",
		Version:     "4.6",
		Date:        "2026-02-05",
		Modifier:    []string{"thinking"},
		LastSynced:  "2026-02-05T00:00:00Z",
	}

	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, m, bestiary.FormatYAML); err != nil {
		t.Fatalf("FormatModel(YAML) error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Modifier") {
		t.Errorf("YAML output missing 'Modifier' field;\nGot:\n%s", output)
	}
	if !strings.Contains(output, "thinking") {
		t.Errorf("YAML output missing modifier value 'thinking';\nGot:\n%s", output)
	}
}

// TestFormatModels_Table validates that FormatModels with FormatTable produces a
// table with a header row containing column names and data rows for each model.
func TestFormatModels_Table(t *testing.T) {
	var buf bytes.Buffer
	models := []bestiary.ModelInfo{
		{
			ID:            bestiary.ModelID("table-model-1"),
			Provider:      "providerA",
			DisplayName:   "Table Model 1",
			Family:        "table-family",
			ContextWindow: 32000,
			MaxOutput:     2048,
		},
		{
			ID:       bestiary.ModelID("table-model-2"),
			Provider: "providerB",
			Family:   "other-family",
		},
	}

	err := bestiary.FormatModels(&buf, models, bestiary.FormatTable)
	if err != nil {
		t.Fatalf("FormatModels(Table) returned unexpected error: %v", err)
	}

	output := buf.String()

	// Header must contain the expected column names.
	for _, header := range []string{"Provider", "Family"} {
		if !strings.Contains(output, header) {
			t.Errorf("FormatModels(Table) output missing header column %q\nGot:\n%s", header, output)
		}
	}

	// Model data must appear in the rows.
	for _, val := range []string{"table-model-1", "providerA", "table-model-2", "providerB"} {
		if !strings.Contains(output, val) {
			t.Errorf("FormatModels(Table) output missing expected value %q\nGot:\n%s", val, output)
		}
	}
}

// ----------------------------------------------------------------------------
// Fix 1: Canonical-provider distinction in the ErrAmbiguous table
//
// ----------------------------------------------------------------------------

// makeClaudeAmbiguousRefs builds a synthetic ErrAmbiguous candidate list that
// mirrors the real "bestiary show claude" scenario:
//   - One canonical-provider row per variant (Provider="anthropic", Family="claude")
//   - numRehostGroups rehost groups (each with Provider != "anthropic") with
//     distinct (Family,Variant,Version) tuples so they pass the grouping dedup.
//
// The list is ordered with rehosts first, then the canonical anthropic row,
// to guarantee the test catches ordering bugs (canonical must surface at top
// even when it appears last in input).
func makeClaudeAmbiguousRefs(numRehostGroups int) []bestiary.ModelRef {
	rehostProviders := []string{
		"deepinfra", "perplexity-agent", "azure-cognitive-services",
		"nano-gpt", "together-ai", "fireworks", "groq", "anyscale",
		"openrouter", "replicate", "modal", "lambda-labs", "lepton",
	}
	refs := make([]bestiary.ModelRef, 0, numRehostGroups+1)
	for i := 0; i < numRehostGroups; i++ {
		prov := rehostProviders[i%len(rehostProviders)]
		refs = append(refs, bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("claude-rehost-%d", i)),
			Provider: bestiary.Provider(prov + fmt.Sprintf("-%d", i)),
			Family:   "claude",
			Variant:  fmt.Sprintf("rehost-variant-%d", i),
			Version:  "",
			Date:     "2025-01-01",
		})
	}
	// Canonical anthropic row — appended LAST so canonical-sort logic must lift it.
	refs = append(refs, bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic, // "anthropic"
		Family:   "claude",
		Variant:  "opus",
		Version:  "4",
		Date:     "2025-05-14",
	})
	return refs
}

// TestFormatAmbiguous_CanonicalRowPresent verifies that when the candidate list
// contains an anthropic (canonical) row mixed with rehosts, FormatAmbiguous
// surfaces the anthropic row in the output.
//
// Fix 1 — this test FAILS before the impl sorts canonical rows
// to the top (when >10 groups exist, canonical gets truncated away).
func TestFormatAmbiguous_CanonicalRowPresent(t *testing.T) {
	// 12 rehost groups + 1 canonical = 13 groups total → exceeds N=10.
	// Before the fix, the canonical anthropic row (added last) is cut by truncation.
	candidates := makeClaudeAmbiguousRefs(12)
	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// The canonical provider "anthropic" must appear in the output.
	if !strings.Contains(output, string(bestiary.ProviderAnthropic)) {
		t.Errorf("FormatAmbiguous: canonical provider %q missing from output;\nGot:\n%s",
			bestiary.ProviderAnthropic, output)
	}
}

// TestFormatAmbiguous_CanonicalRowMarked verifies that the canonical row is
// visually distinguished from rehost rows in the FormatAmbiguous output.
//
// Fix 1 — this test FAILS before the impl adds a visual marker
// to canonical rows.
func TestFormatAmbiguous_CanonicalRowMarked(t *testing.T) {
	candidates := makeClaudeAmbiguousRefs(3) // 3 rehosts + 1 canonical, well within N=10
	e := &bestiary.ErrAmbiguous{
		Input:      "claude",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// The canonical anthropic row must carry a visual marker.
	// The impl uses a "*" PREFIX at the start of each canonical row in Section 1.
	// Check that the line containing "anthropic" also contains the marker "*".
	lines := strings.Split(output, "\n")
	var canonicalLine string
	for _, l := range lines {
		if strings.Contains(l, string(bestiary.ProviderAnthropic)) {
			canonicalLine = l
			break
		}
	}
	if canonicalLine == "" {
		t.Fatalf("FormatAmbiguous: no line containing canonical provider %q found in output;\nGot:\n%s",
			bestiary.ProviderAnthropic, output)
	}
	if !strings.Contains(canonicalLine, "*") {
		t.Errorf("FormatAmbiguous: canonical row missing '*' marker;\ncanonical line: %q\nFull output:\n%s",
			canonicalLine, output)
	}
}

// TestFormatAmbiguous_CanonicalSortedToTop verifies that the canonical section
// (Section 1) appears BEFORE the rehost section (Section 2) in FormatAmbiguous output.
//
// Updated for  two-section layout: Section 1 ("Canonical:") always
// precedes Section 2 ("Also rehosted by:"). The canonical provider appears in Section 1
// (from Candidates), while rehost names appear in Section 2 (from RehostProviders).
func TestFormatAmbiguous_CanonicalSortedToTop(t *testing.T) {
	candidates := makeClaudeAmbiguousRefs(3) // 3 rehosts + 1 canonical anthropic row
	// Populate RehostProviders from the non-canonical candidates.
	rehostProviders := make([]bestiary.Provider, 3)
	for i := 0; i < 3; i++ {
		rehostProviders[i] = bestiary.Provider("deepinfra-" + fmt.Sprintf("%d", i))
	}
	e := &bestiary.ErrAmbiguous{
		Input:           "claude",
		Scheme:          bestiary.SchemeCanonical,
		Candidates:      candidates,
		RehostProviders: rehostProviders,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// "Canonical:" section must appear before "Also rehosted by:".
	canonicalPos := strings.Index(output, "Canonical:")
	rehostPos := strings.Index(output, "Also rehosted by:")

	if canonicalPos < 0 {
		t.Fatalf("FormatAmbiguous: 'Canonical:' section not found;\nGot:\n%s", output)
	}
	if rehostPos < 0 {
		t.Fatalf("FormatAmbiguous: 'Also rehosted by:' section not found;\nGot:\n%s", output)
	}
	if canonicalPos > rehostPos {
		t.Errorf("FormatAmbiguous: 'Canonical:' section should appear BEFORE 'Also rehosted by:';\n"+
			"Canonical: at offset %d, Also rehosted by: at offset %d\nFull output:\n%s",
			canonicalPos, rehostPos, output)
	}

	// Canonical provider "anthropic" must appear in the output (Section 1).
	if !strings.Contains(output, string(bestiary.ProviderAnthropic)) {
		t.Errorf("FormatAmbiguous: canonical provider %q missing from output;\nGot:\n%s",
			bestiary.ProviderAnthropic, output)
	}
}

// TestFormatAmbiguous_CanonicalSurvivesTruncation verifies that even with >5
// canonical groups, the canonical section renders exactly 5 rows and the overflow
// hint is emitted.
//
// Updated for  two-section layout: canonical cap is now 5.
func TestFormatAmbiguous_CanonicalSurvivesTruncation(t *testing.T) {
	// 1 canonical + 12 rehosts in Candidates; RehostProviders for Section 2.
	// The canonical anthropic row must appear in Section 1.
	candidates := makeClaudeAmbiguousRefs(12) // 12 rehosts + 1 anthropic
	// Build a realistic RehostProviders list.
	rehostProviders := make([]bestiary.Provider, 7)
	for i := 0; i < 7; i++ {
		rehostProviders[i] = bestiary.Provider(fmt.Sprintf("rehost-%d", i))
	}
	e := &bestiary.ErrAmbiguous{
		Input:           "claude",
		Scheme:          bestiary.SchemeCanonical,
		Candidates:      candidates,
		RehostProviders: rehostProviders,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Canonical provider "anthropic" must appear in Section 1.
	if !strings.Contains(output, string(bestiary.ProviderAnthropic)) {
		t.Errorf("FormatAmbiguous: canonical provider %q not in output;\nGot:\n%s",
			bestiary.ProviderAnthropic, output)
	}
	// Rehost overflow: 7 - 5 = 2 → "+2 more" for Section 2.
	if !strings.Contains(output, "+2 more") {
		t.Errorf("FormatAmbiguous: expected '+2 more' for 7 rehost providers (cap 5);\nGot:\n%s", output)
	}
}

// ----------------------------------------------------------------------------
// Fix 2: PURL loose-fallback missed-namespace note
//
// ----------------------------------------------------------------------------

// TestFormatAmbiguous_PURLMissedNamespaceNote verifies that when
// ErrAmbiguous.PURLMissedNamespace is non-empty, FormatAmbiguous prints a note
// above the candidate table naming the missed namespace.
//
// Fix 2 — this test FAILS before the impl adds the note.
func TestFormatAmbiguous_PURLMissedNamespaceNote(t *testing.T) {
	candidates := makeAmbiguousRefs(3, false)
	e := &bestiary.ErrAmbiguous{
		Input:               "claude-opus-4-5",
		Scheme:              bestiary.SchemePURL,
		Candidates:          candidates,
		PURLMissedNamespace: "nonexistent",
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Must contain the missed-namespace note.
	wantNote := `no matches in namespace "nonexistent"`
	if !strings.Contains(output, wantNote) {
		t.Errorf("FormatAmbiguous: missing missed-namespace note %q;\nGot:\n%s", wantNote, output)
	}
}

// TestFormatAmbiguous_PURLMissedNamespaceNote_AbsentWhenEmpty verifies that when
// ErrAmbiguous.PURLMissedNamespace is empty, no missed-namespace note is printed.
//
// This is the negative case — the note must NOT appear for normal ambiguous errors.
func TestFormatAmbiguous_PURLMissedNamespaceNote_AbsentWhenEmpty(t *testing.T) {
	candidates := makeAmbiguousRefs(3, false)
	e := &bestiary.ErrAmbiguous{
		Input:               "test-input",
		Scheme:              bestiary.SchemeCanonical,
		Candidates:          candidates,
		PURLMissedNamespace: "", // empty — note must not appear
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	if strings.Contains(output, "no matches in namespace") {
		t.Errorf("FormatAmbiguous: missed-namespace note should be absent when PURLMissedNamespace is empty;\nGot:\n%s", output)
	}
}

// TestFormatAmbiguous_PURLMissedNamespaceNote_BeforeTable verifies that the
// missed-namespace note appears BEFORE the candidate table header.
//
// Fix 2 — the note must be above the table per spec.
// Uses a canonical anthropic ref so the Canonical: section is present in output.
func TestFormatAmbiguous_PURLMissedNamespaceNote_BeforeTable(t *testing.T) {
	// Use makeClaudeAmbiguousRefs (includes one canonical anthropic row) so that
	// the Canonical: section appears in output and ordering can be verified.
	candidates := makeClaudeAmbiguousRefs(2)
	e := &bestiary.ErrAmbiguous{
		Input:               "claude-opus-4-5",
		Scheme:              bestiary.SchemePURL,
		Candidates:          candidates,
		PURLMissedNamespace: "nonexistent",
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	notePos := strings.Index(output, `no matches in namespace "nonexistent"`)
	tableHeaderPos := strings.Index(output, "Canonical")

	if notePos < 0 {
		t.Fatalf("FormatAmbiguous: missed-namespace note not found in output;\nGot:\n%s", output)
	}
	if tableHeaderPos < 0 {
		t.Fatalf("FormatAmbiguous: table header 'Canonical' not found in output;\nGot:\n%s", output)
	}
	if notePos > tableHeaderPos {
		t.Errorf("FormatAmbiguous: missed-namespace note must appear BEFORE the table header;\n"+
			"note at offset %d, table header at offset %d\nFull output:\n%s",
			notePos, tableHeaderPos, output)
	}
}

// ----------------------------------------------------------------------------
// Two-section layout (prefix marker, legend, rehost names, footer)
// ----------------------------------------------------------------------------

// makeAmbiguousRefsWithRehosts builds an ErrAmbiguous suitable for two-section layout tests.
// It constructs numCanonical canonical refs (provider="anthropic", Family="claude") and
// numRehosts rehost providers in RehostProviders. Each canonical ref has a distinct variant
// so they appear as distinct canonical rows. RehostProviders is populated directly.
func makeAmbiguousWithRehosts(numCanonical int, rehostProviders []bestiary.Provider) *bestiary.ErrAmbiguous {
	candidates := make([]bestiary.ModelRef, numCanonical)
	for i := 0; i < numCanonical; i++ {
		candidates[i] = bestiary.ModelRef{
			ID:       bestiary.ModelID(fmt.Sprintf("claude-canonical-%d", i)),
			Provider: bestiary.ProviderAnthropic,
			Family:   "claude",
			Variant:  fmt.Sprintf("variant-%d", i),
			Version:  "1",
			Date:     "2025-01-01",
		}
	}
	return &bestiary.ErrAmbiguous{
		Input:           "claude",
		Scheme:          bestiary.SchemeCanonical,
		Candidates:      candidates,
		RehostProviders: rehostProviders,
	}
}

// TestFormatAmbiguous_V4_LegendPresent verifies the legend line "* = canonical provider"
// is present in FormatAmbiguous output.
//
// This test FAILS before the new layout is implemented.
func TestFormatAmbiguous_V4_LegendPresent(t *testing.T) {
	e := makeAmbiguousWithRehosts(2, []bestiary.Provider{"deepinfra", "azure-cognitive-services"})

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	const legend = "* = canonical provider"
	if !strings.Contains(output, legend) {
		t.Errorf("FormatAmbiguous: missing legend line %q;\nGot:\n%s", legend, output)
	}
}

// TestFormatAmbiguous_V4_CanonicalRowsHavePrefixStar verifies that canonical rows
// START with "* " (prefix), NOT end with " *" (old suffix format).
//
// This test FAILS before the new layout since the old impl uses a suffix marker.
func TestFormatAmbiguous_V4_CanonicalRowsHavePrefixStar(t *testing.T) {
	e := makeAmbiguousWithRehosts(3, []bestiary.Provider{"deepinfra"})

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// The output must contain at least one line that starts with "* " (prefix).
	lines := strings.Split(output, "\n")
	foundPrefixStar := false
	for _, l := range lines {
		if strings.HasPrefix(l, "* ") {
			foundPrefixStar = true
			break
		}
	}
	if !foundPrefixStar {
		t.Errorf("FormatAmbiguous: no line starts with '* ' (prefix marker absent);\nGot:\n%s", output)
	}

	// No canonical line should end with " *" (that was the old suffix format).
	for _, l := range lines {
		if strings.HasSuffix(l, " *") {
			t.Errorf("FormatAmbiguous: found line with old suffix marker ' *': %q\nFull output:\n%s", l, output)
		}
	}
}

// TestFormatAmbiguous_V4_CanonicalSection_Cap5 verifies that at most 5 canonical rows
// are displayed, with a "+N more" hint when >5 canonical candidates exist.
//
// This test FAILS before the new layout since the old impl caps at 10.
func TestFormatAmbiguous_V4_CanonicalSection_Cap5(t *testing.T) {
	// 8 canonical candidates — should display 5 and emit "+3 more".
	e := makeAmbiguousWithRehosts(8, nil)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Count lines that start with "* " and are inside the Canonical: section
	// (exclude the legend line "* = canonical provider").
	lines := strings.Split(output, "\n")
	prefixLines := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "* ") && !strings.HasPrefix(l, "* =") {
			prefixLines++
		}
	}
	if prefixLines > 5 {
		t.Errorf("FormatAmbiguous: expected <=5 canonical rows with '* ' prefix, got %d;\nGot:\n%s", prefixLines, output)
	}

	// Must emit "+3 more" hint for 8-5=3 overflow canonical rows.
	if !strings.Contains(output, "+3 more") {
		t.Errorf("FormatAmbiguous: expected '+3 more' hint for 8 canonical candidates (cap 5); got:\n%s", output)
	}

	// Candidates variant-5 through variant-7 must NOT appear in the output.
	for i := 5; i < 8; i++ {
		variantStr := fmt.Sprintf("variant-%d", i)
		if strings.Contains(output, variantStr) {
			t.Errorf("FormatAmbiguous: canonical candidate %q should NOT appear after cap-5 truncation; got:\n%s", variantStr, output)
		}
	}
}

// TestFormatAmbiguous_V4_RehostSection_PresentWhenNonEmpty verifies Section 2
// "Also rehosted by:" is present when RehostProviders is non-empty, and lists
// distinct rehost provider NAMES (not full canonical rows).
//
// This test FAILS before the new layout is implemented.
func TestFormatAmbiguous_V4_RehostSection_PresentWhenNonEmpty(t *testing.T) {
	rehosts := []bestiary.Provider{"deepinfra", "azure-cognitive-services", "nano-gpt"}
	e := makeAmbiguousWithRehosts(2, rehosts)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	const sectionHeader = "Also rehosted by:"
	if !strings.Contains(output, sectionHeader) {
		t.Errorf("FormatAmbiguous: missing Section 2 header %q when RehostProviders non-empty;\nGot:\n%s", sectionHeader, output)
	}

	// Each rehost provider name must appear in the output.
	for _, prov := range rehosts {
		if !strings.Contains(output, string(prov)) {
			t.Errorf("FormatAmbiguous: rehost provider %q missing from Section 2;\nGot:\n%s", prov, output)
		}
	}
}

// TestFormatAmbiguous_V4_RehostSection_AbsentWhenEmpty verifies Section 2 is
// entirely omitted (header AND content) when RehostProviders is empty.
//
// This test FAILS before the new layout is implemented.
func TestFormatAmbiguous_V4_RehostSection_AbsentWhenEmpty(t *testing.T) {
	// RehostProviders explicitly empty — section must be absent.
	e := makeAmbiguousWithRehosts(3, nil)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	const sectionHeader = "Also rehosted by:"
	if strings.Contains(output, sectionHeader) {
		t.Errorf("FormatAmbiguous: Section 2 header %q must be ABSENT when RehostProviders is empty;\nGot:\n%s", sectionHeader, output)
	}
}

// TestFormatAmbiguous_V4_RehostSection_Cap5 verifies that at most 5 rehost provider
// names are displayed, with a "+N more" hint when RehostProviders has >5 entries.
//
// This test FAILS before the new layout is implemented.
func TestFormatAmbiguous_V4_RehostSection_Cap5(t *testing.T) {
	rehosts := []bestiary.Provider{
		"deepinfra", "azure-cognitive-services", "nano-gpt",
		"together-ai", "fireworks", "groq", "anyscale",
	}
	e := makeAmbiguousWithRehosts(2, rehosts)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// "+2 more" for 7-5=2 overflow rehost providers.
	if !strings.Contains(output, "+2 more") {
		t.Errorf("FormatAmbiguous: expected '+2 more' hint for 7 rehost providers (cap 5);\nGot:\n%s", output)
	}

	// Providers groq and anyscale (index 5,6) must NOT appear.
	for _, hidden := range []bestiary.Provider{"groq", "anyscale"} {
		if strings.Contains(output, string(hidden)) {
			t.Errorf("FormatAmbiguous: rehost provider %q should NOT appear after cap-5;\nGot:\n%s", hidden, output)
		}
	}

	// First 5 providers must appear.
	for _, shown := range rehosts[:5] {
		if !strings.Contains(output, string(shown)) {
			t.Errorf("FormatAmbiguous: rehost provider %q should appear in first-5;\nGot:\n%s", shown, output)
		}
	}
}

// TestFormatAmbiguous_V4_RehostSection_NamesOnly verifies that Section 2 lists
// provider NAMES only (not full canonical model strings like "anthropic/claude/opus/...").
//
// This test FAILS before the new layout if full canonical rows appear.
func TestFormatAmbiguous_V4_RehostSection_NamesOnly(t *testing.T) {
	rehosts := []bestiary.Provider{"deepinfra", "nano-gpt"}
	e := makeAmbiguousWithRehosts(2, rehosts)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Locate section 2 content after the "Also rehosted by:" header.
	idx := strings.Index(output, "Also rehosted by:")
	if idx < 0 {
		t.Fatalf("FormatAmbiguous: Section 2 header 'Also rehosted by:' not found;\nGot:\n%s", output)
	}
	section2 := output[idx:]

	// The section 2 area must NOT contain a full canonical path like "claude/variant-0"
	// (which would indicate full model rows were rendered instead of provider names).
	if strings.Contains(section2, "claude/variant-0") || strings.Contains(section2, "claude/variant-1") {
		t.Errorf("FormatAmbiguous Section 2 must list provider names only, not full canonical model strings;\nSection2:\n%s", section2)
	}
}

// TestFormatAmbiguous_V4_RehostSection_OnePerLine verifies that Section 2 renders
// each rehost provider name on its own line rather than as a comma-separated list.
//
// The rehost-names rendering change (one-per-line).
func TestFormatAmbiguous_V4_RehostSection_OnePerLine(t *testing.T) {
	rehosts := []bestiary.Provider{"deepinfra", "azure-cognitive-services", "nano-gpt"}
	e := makeAmbiguousWithRehosts(2, rehosts)

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Locate Section 2 content starting from its header.
	idx := strings.Index(output, "Also rehosted by:")
	if idx < 0 {
		t.Fatalf("FormatAmbiguous: Section 2 header 'Also rehosted by:' not found;\nGot:\n%s", output)
	}
	section2 := output[idx:]

	// Each provider must appear on its own line — confirmed by checking that the
	// provider name is preceded by a newline (possibly with leading whitespace) and
	// not joined to another provider name with a comma on the same line.
	for _, prov := range rehosts {
		provStr := string(prov)
		// The comma-joined form would have "deepinfra, azure-cognitive-services" on one line.
		// Assert the provider appears on a line by itself (no comma following it on the same line).
		lines := strings.Split(section2, "\n")
		found := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == provStr {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FormatAmbiguous: rehost provider %q must appear on its own line in Section 2;\nSection2:\n%s", provStr, section2)
		}
	}

	// No line in Section 2 must contain two provider names joined by ", ".
	lines := strings.Split(section2, "\n")
	for _, line := range lines {
		for i, p1 := range rehosts {
			for _, p2 := range rehosts[i+1:] {
				if strings.Contains(line, string(p1)+", "+string(p2)) || strings.Contains(line, string(p2)+", "+string(p1)) {
					t.Errorf("FormatAmbiguous: rehost providers must be one-per-line, not comma-joined;\nOffending line: %q\nSection2:\n%s",
						line, section2)
				}
			}
		}
	}
}

// TestFormatAmbiguous_V4_FooterInstructions verifies that the footer contains
// the two real instruction strings: "bestiary list" and "--format=raw".
//
// This test FAILS before the new layout is implemented.
func TestFormatAmbiguous_V4_FooterInstructions(t *testing.T) {
	e := makeAmbiguousWithRehosts(2, []bestiary.Provider{"deepinfra"})

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	if !strings.Contains(output, "bestiary list") {
		t.Errorf("FormatAmbiguous footer: missing 'bestiary list' instruction;\nGot:\n%s", output)
	}
	if !strings.Contains(output, "--format=raw") {
		t.Errorf("FormatAmbiguous footer: missing '--format=raw' instruction;\nGot:\n%s", output)
	}
}

// TestFormatAmbiguous_V4_PURLNote_StillPresent verifies that the PURLMissedNamespace
// note is still printed when set, and appears before the Canonical section.
//
// Regression guard for existing Fix 2 behavior.
// Uses canonical anthropic refs so the Canonical: section is present in output.
func TestFormatAmbiguous_V4_PURLNote_StillPresent(t *testing.T) {
	// Use makeClaudeAmbiguousRefs (includes one canonical anthropic row) so that
	// the Canonical: section appears in output and ordering can be verified.
	e := &bestiary.ErrAmbiguous{
		Input:               "claude-opus-4-5",
		Scheme:              bestiary.SchemePURL,
		Candidates:          makeClaudeAmbiguousRefs(2),
		PURLMissedNamespace: "nonexistent-v4",
		RehostProviders:     nil,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	wantNote := `no matches in namespace "nonexistent-v4"`
	if !strings.Contains(output, wantNote) {
		t.Errorf("FormatAmbiguous V4: PURL missed-namespace note %q absent;\nGot:\n%s", wantNote, output)
	}

	// Note must appear before the Canonical: section.
	notePos := strings.Index(output, wantNote)
	canonicalPos := strings.Index(output, "Canonical:")
	if canonicalPos < 0 {
		t.Fatalf("FormatAmbiguous V4: 'Canonical:' section header not found;\nGot:\n%s", output)
	}
	if notePos > canonicalPos {
		t.Errorf("FormatAmbiguous V4: PURL note must appear before 'Canonical:' section;\n"+
			"note at %d, Canonical at %d\nFull output:\n%s", notePos, canonicalPos, output)
	}
}

// ----------------------------------------------------------------------------
// Empty-canonical suppression
// ----------------------------------------------------------------------------

// TestFormatAmbiguous_EmptyCanonical_NoBareHeader verifies that when no candidates
// match the canonical provider (unmapped family, e.g. "minimax"), FormatAmbiguous
// does NOT print a bare empty "Canonical:" header or an orphaned "* = canonical provider"
// legend. Instead, those elements are omitted entirely, producing coherent output.
//
// Before the fix, "bestiary show minimax" rendered:
//
//   - = canonical provider
//     Canonical:
//     Also rehosted by: ...
//
// The empty section with orphaned legend was confusing. After the fix, both are omitted
// when there are zero canonical rows. The Section 2 (rehost names) and footer still appear.
func TestFormatAmbiguous_EmptyCanonical_NoBareHeader(t *testing.T) {
	// Build candidates with no canonical provider — all providers are synthetic
	// names that do not map to any known CanonicalProvider().
	candidates := makeAmbiguousRefs(3, false)
	rehosts := []bestiary.Provider{"minimax", "deepinfra", "ollama-cloud"}
	e := &bestiary.ErrAmbiguous{
		Input:           "minimax",
		Scheme:          bestiary.SchemeCanonical,
		Candidates:      candidates,
		RehostProviders: rehosts,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// The bare "Canonical:" header must NOT appear when there are no canonical rows.
	if strings.Contains(output, "Canonical:") {
		t.Errorf("FormatAmbiguous: bare 'Canonical:' header must be ABSENT when no canonical rows exist;\nGot:\n%s", output)
	}

	// The orphaned "* = canonical provider" legend must NOT appear.
	if strings.Contains(output, "* = canonical provider") {
		t.Errorf("FormatAmbiguous: orphaned '* = canonical provider' legend must be ABSENT when no canonical rows exist;\nGot:\n%s", output)
	}

	// Section 2 (rehost names) must still be present.
	if !strings.Contains(output, "Also rehosted by:") {
		t.Errorf("FormatAmbiguous: 'Also rehosted by:' section must still appear when RehostProviders non-empty;\nGot:\n%s", output)
	}
	for _, p := range rehosts {
		if !strings.Contains(output, string(p)) {
			t.Errorf("FormatAmbiguous: rehost provider %q missing from Section 2;\nGot:\n%s", p, output)
		}
	}

	// Footer must still appear.
	if !strings.Contains(output, "bestiary list") {
		t.Errorf("FormatAmbiguous: footer 'bestiary list' must still appear;\nGot:\n%s", output)
	}
}
