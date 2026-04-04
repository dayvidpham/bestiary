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
