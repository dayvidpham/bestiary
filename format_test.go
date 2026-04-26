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
			ID:            "test-model-1",
			Provider:      "testprovider",
			DisplayName:   "Test Model 1",
			Family:        "test-family",
			ContextWindow: 128000,
			MaxOutput:     4096,
			CostInputPerMTok: &cost,
			LastSynced:    "2024-01-01T00:00:00Z",
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
// candidate list after N=10 and emits a "+M more" hint.
//
// Fix #2 (SLICE-FIX-V2-2): "Truncate after N (e.g. 10) with '+M more' hint"
func TestFormatAmbiguous_Truncation(t *testing.T) {
	// Create 17 distinct candidates (one per distinct (Family, Variant, Version) tuple).
	candidates := makeAmbiguousRefs(17, false)
	e := &bestiary.ErrAmbiguous{
		Input:      "test-input",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Must contain "+7 more" (17 - 10 = 7) or equivalent.
	if !strings.Contains(output, "+7 more") && !strings.Contains(output, "7 more") {
		t.Errorf("FormatAmbiguous(17 candidates): output should contain '+7 more' hint; got:\n%s", output)
	}
	// Must NOT list all 17 candidates (only first 10).
	// Check that "model-10" through "model-16" are NOT in the output.
	for i := 10; i < 17; i++ {
		if strings.Contains(output, fmt.Sprintf("model-%d", i)) {
			t.Errorf("FormatAmbiguous: candidate model-%d should NOT appear in truncated output; got:\n%s", i, output)
		}
	}
}

// TestFormatAmbiguous_NoTruncation_ExactlyN verifies that exactly N=10 candidates
// does NOT emit a truncation hint ("+M more" pattern).
func TestFormatAmbiguous_NoTruncation_ExactlyN(t *testing.T) {
	candidates := makeAmbiguousRefs(10, false)
	e := &bestiary.ErrAmbiguous{
		Input:      "test-input",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// No "+M more" truncation hint for exactly 10 candidates.
	// (The footer may contain "more" in "more specific" — check for "+N more" pattern.)
	if strings.Contains(output, "+") && strings.Contains(output, "more\n") {
		t.Errorf("FormatAmbiguous(10 candidates): should NOT have '+N more' truncation hint; got:\n%s", output)
	}
}

// TestFormatAmbiguous_Grouping verifies that FormatAmbiguous groups candidates
// by (Family, Variant, Version) tuple and shows ONE canonical row per group.
//
// Fix #2 (SLICE-FIX-V2-2): "Group by NormalizedFamily/Variant; show one canonical per group"
func TestFormatAmbiguous_Grouping(t *testing.T) {
	// Create candidates where the same (family, variant, version) appears with
	// multiple providers — simulating the 17+ rehost scenario for claude/opus.
	candidates := []bestiary.ModelRef{
		{ID: "model-opus-1", Provider: bestiary.Provider("provider-a"), Family: "myfamily", Variant: "alpha", Version: "1", Date: "2025-01-01"},
		{ID: "model-opus-1", Provider: bestiary.Provider("provider-b"), Family: "myfamily", Variant: "alpha", Version: "1", Date: "2025-01-01"},
		{ID: "model-opus-1", Provider: bestiary.Provider("provider-c"), Family: "myfamily", Variant: "alpha", Version: "1", Date: "2025-01-01"},
		{ID: "model-beta-1", Provider: bestiary.Provider("provider-a"), Family: "myfamily", Variant: "beta", Version: "1", Date: "2025-01-01"},
		{ID: "model-beta-1", Provider: bestiary.Provider("provider-b"), Family: "myfamily", Variant: "beta", Version: "1", Date: "2025-01-01"},
	}

	e := &bestiary.ErrAmbiguous{
		Input:      "myfamily",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// Should show exactly 2 data rows (one per unique (family, variant, version) group),
	// not 5 data rows (one per candidate).
	// Count by splitting on newlines and counting non-header, non-separator data rows.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var dataLines []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		// Skip header, separator (all dashes), footer, and empty lines.
		if l == "" || strings.HasPrefix(l, "bestiary:") ||
			strings.HasPrefix(l, "use --format") || strings.HasPrefix(l, "+") ||
			strings.HasPrefix(l, "Canonical") || strings.HasPrefix(l, "----") {
			continue
		}
		dataLines = append(dataLines, l)
	}
	if len(dataLines) != 2 {
		t.Errorf("FormatAmbiguous grouping: expected 2 data rows (one per group), got %d; output:\n%s\ndata lines: %v",
			len(dataLines), output, dataLines)
	}

	// Both variant names should appear exactly once in the output lines
	// (grouping collapsed the 3+2 providers into 1+1 rows).
	alphaCount := strings.Count(output, "/alpha/")
	betaCount := strings.Count(output, "/beta/")
	if alphaCount != 1 {
		t.Errorf("FormatAmbiguous grouping: '/alpha/' appears %d times in canonical column, want 1 (grouped); output:\n%s", alphaCount, output)
	}
	if betaCount != 1 {
		t.Errorf("FormatAmbiguous grouping: '/beta/' appears %d times in canonical column, want 1 (grouped); output:\n%s", betaCount, output)
	}
}

// TestFormatAmbiguous_GroupingAndTruncation verifies that grouping is applied
// BEFORE truncation — i.e., if 17+ rehost rows collapse to 10 groups, no
// truncation hint is emitted. If they collapse to >10 groups, truncation applies.
func TestFormatAmbiguous_GroupingAndTruncation(t *testing.T) {
	// Create 15 distinct canonical groups, each with 2 providers (30 candidates total).
	// After grouping: 15 groups → exceeds N=10 → should truncate with "+5 more".
	candidates := make([]bestiary.ModelRef, 0, 30)
	for i := 0; i < 15; i++ {
		for j := 0; j < 2; j++ {
			candidates = append(candidates, bestiary.ModelRef{
				ID:       bestiary.ModelID(fmt.Sprintf("model-%d-p%d", i, j)),
				Provider: bestiary.Provider(fmt.Sprintf("provider-%d", j)),
				Family:   bestiary.Family(fmt.Sprintf("family-%d", i)),
				Variant:  fmt.Sprintf("variant-%d", i),
				Version:  fmt.Sprintf("1.%d", i),
				Date:     "2025-01-01",
			})
		}
	}

	e := &bestiary.ErrAmbiguous{
		Input:      "test",
		Scheme:     bestiary.SchemeCanonical,
		Candidates: candidates,
	}

	var buf bytes.Buffer
	bestiary.FormatAmbiguous(&buf, e)
	output := buf.String()

	// After grouping 30 candidates into 15 groups, then truncating at 10:
	// "+5 more" hint must appear as "+5 more".
	if !strings.Contains(output, "+5 more") {
		t.Errorf("FormatAmbiguous(30 candidates, 15 groups): should truncate to 10+'+5 more'; got:\n%s", output)
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
