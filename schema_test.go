package bestiary_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestJSONOutput_ConformsToSchema validates that the JSON produced by
// FormatModel contains exactly the fields declared in bestiary.schema.json —
// no more, no fewer.
//
// Validation strategy:
//  1. Read bestiary.schema.json from disk (os.ReadFile).
//  2. Unmarshal the schema and extract the top-level property names from
//     "properties".
//  3. Produce JSON output for a known ModelInfo fixture via FormatModel.
//  4. Unmarshal that output into map[string]any.
//  5. Assert every schema property key appears in the output.
//  6. Assert no extra keys in the output that are absent from the schema.
func TestJSONOutput_ConformsToSchema(t *testing.T) {
	// Step 1: read schema file.
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf(
			"could not read bestiary.schema.json;\n"+
				"  what went wrong: os.ReadFile returned error: %v\n"+
				"  why: the schema file may not exist or the test is run from the wrong directory\n"+
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
				"  how to fix: ensure bestiary.schema.json exists in the module root and tests are run from that directory",
			err,
		)
	}

	// Step 2: unmarshal schema and extract property names.
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf(
			"could not unmarshal bestiary.schema.json;\n"+
				"  what went wrong: json.Unmarshal returned error: %v\n"+
				"  why: the schema file may not be valid JSON\n"+
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
				"  how to fix: validate bestiary.schema.json with a JSON linter",
			err,
		)
	}
	if len(schema.Properties) == 0 {
		t.Fatalf(
			"bestiary.schema.json has no properties;\n"+
				"  what went wrong: schema.properties is empty or missing\n"+
				"  why: the schema file may be missing a \"properties\" key\n"+
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
				"  how to fix: add a \"properties\" object to bestiary.schema.json",
		)
	}

	// Step 3: build a comprehensive ModelInfo fixture and produce JSON.
	cost := 1.5
	fixture := bestiary.ModelInfo{
		ID:                    "test-schema-model",
		Provider:              "testprovider",
		DisplayName:           "Schema Test Model",
		Family:                "test-family",
		ContextWindow:         128000,
		MaxOutput:             4096,
		Reasoning:             true,
		ToolCall:              true,
		Attachment:            false,
		Temperature:           true,
		StructuredOutput:      true,
		Interleaved:           bestiary.Capability{Supported: true},
		OpenWeights:           false,
		CostInputPerMTok:      &cost,
		CostOutputPerMTok:     nil,
		CostReasoningPerMTok:  nil,
		CostCacheReadPerMTok:  nil,
		CostCacheWritePerMTok: nil,
		ReleaseDate:           "2024-01-01",
		Knowledge:             "2024-01",
		Modalities: bestiary.Modalities{
			Input:  []bestiary.Modality{bestiary.ModalityText},
			Output: []bestiary.Modality{bestiary.ModalityText},
		},
		LastSynced: "2024-01-01T00:00:00Z",
	}

	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, fixture, bestiary.FormatJSON); err != nil {
		t.Fatalf(
			"FormatModel(JSON) returned error;\n"+
				"  what went wrong: %v\n"+
				"  why: the formatter may have encountered an unexpected type or I/O error\n"+
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
				"  how to fix: check FormatModel in format.go",
			err,
		)
	}

	// Step 4: unmarshal JSON output.
	var output map[string]any
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf(
			"could not unmarshal FormatModel JSON output;\n"+
				"  what went wrong: json.Unmarshal returned error: %v\n"+
				"  why: FormatModel produced invalid JSON\n"+
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
				"  how to fix: check FormatModel in format.go for marshal errors",
			err,
		)
	}

	// Step 5: every schema property must exist as a key in the output.
	for prop := range schema.Properties {
		if _, ok := output[prop]; !ok {
			t.Errorf(
				"schema property %q is missing from FormatModel JSON output;\n"+
					"  what went wrong: key %q not found in output map\n"+
					"  why: ModelInfo field may have been removed or the schema property name does not match the Go field name\n"+
					"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
					"  how to fix: ensure ModelInfo has a field named %q and that it is exported (for json marshaling)",
				prop, prop, prop,
			)
		}
	}

	// Step 6: no extra keys in the output that are absent from the schema.
	for key := range output {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf(
				"FormatModel JSON output contains key %q not declared in schema;\n"+
					"  what went wrong: key %q is in output but not in schema.properties\n"+
					"  why: a new field was added to ModelInfo without updating bestiary.schema.json\n"+
					"  where: schema_test.go TestJSONOutput_ConformsToSchema\n"+
					"  how to fix: add property %q to bestiary.schema.json properties",
				key, key, key,
			)
		}
	}
}
