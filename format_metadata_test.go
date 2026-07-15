package bestiary_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestFormatModel_YAML_MetadataScalars asserts the minimal YAML serializer emits
// the two v0.2.5 flat metadata scalars — Description and Status — alongside the
// existing fields. Status renders as its canonical wire name.
func TestFormatModel_YAML_MetadataScalars(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{
		ID:          bestiary.ModelID("yaml-meta-model"),
		Provider:    "testprovider",
		Description: "a flat description scalar",
		Status:      bestiary.StatusBeta,
	}
	if err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML); err != nil {
		t.Fatalf("FormatModel(YAML): %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Description:") || !strings.Contains(out, "a flat description scalar") {
		t.Errorf("YAML missing Description scalar; got:\n%s", out)
	}
	// Status renders as the canonical wire name, quoted by the string writer.
	if !strings.Contains(out, `Status: "beta"`) {
		t.Errorf("YAML missing Status: \"beta\" scalar; got:\n%s", out)
	}
}

// TestFormatModel_YAML_StatusNone asserts a model with no declared status still
// emits the Status scalar as "none" (the serializer is flat and unconditional).
func TestFormatModel_YAML_StatusNone(t *testing.T) {
	var buf bytes.Buffer
	model := bestiary.ModelInfo{ID: "no-status", Provider: "p"}
	if err := bestiary.FormatModel(&buf, model, bestiary.FormatYAML); err != nil {
		t.Fatalf("FormatModel(YAML): %v", err)
	}
	if !strings.Contains(buf.String(), `Status: "none"`) {
		t.Errorf("YAML for a status-less model should emit Status: \"none\"; got:\n%s", buf.String())
	}
}
