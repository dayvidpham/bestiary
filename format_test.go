package bestiary_test

import (
	"bytes"
	"encoding/json"
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
