package bestiary_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
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
			"bestiary.schema.json has no properties;\n" +
				"  what went wrong: schema.properties is empty or missing\n" +
				"  why: the schema file may be missing a \"properties\" key\n" +
				"  where: schema_test.go TestJSONOutput_ConformsToSchema\n" +
				"  how to fix: add a \"properties\" object to bestiary.schema.json",
		)
	}

	// Step 3: build a comprehensive ModelInfo fixture with all canonical fields
	// populated and produce JSON. Non-empty Family/Variant/Date are used
	// to exercise the codegen-baked normalization path.
	cost := 1.5
	fixture := bestiary.ModelInfo{
		ID:                    "test-schema-model-20240101",
		Provider:              "testprovider",
		DisplayName:           "Schema Test Model",
		RawFamily:             "test-family",
		Family:                "test",
		Variant:               "schema",
		Date:                  "2024-01-01",
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
		// v0.2.3 additive fields (schema 0.1.0): exercise the populated
		// serialization path for Host (string) and Lineage ([]LineageEdge,
		// with DerivationKind serialized as text).
		Host: bestiary.HostAzure,
		Lineage: []bestiary.LineageEdge{
			{
				Parent: bestiary.EntityRef{Family: "test", Variant: "schema", Version: "0"},
				Kind:   bestiary.DerivationFinetune,
			},
		},
		// v0.2.4 additive fields (schema 0.2.0): exercise the populated
		// serialization path for ParamSize (string), QuantVRAM ([]QuantVRAM, with
		// Quant serialized as a Quantization enum string), and Source (DataSourceID).
		ParamSize: "70b",
		QuantVRAM: []bestiary.QuantVRAM{
			{
				Quant:        bestiary.QuantQ4_K_M,
				QuantRaw:     "Q4_K_M",
				WeightsBytes: 42_000_000_000,
				VRAMBytes:    45_000_000_000,
			},
		},
		Source: bestiary.DataSourceOllama,
		// v0.2.5 additive fields (schema 0.3.0): exercise the populated
		// serialization path for the instance-level models.dev harmonization
		// fields — Description (string), Status (ModelStatus enum string),
		// ReasoningOptions ([]ReasoningOption with a Kind enum string), the audio
		// cost pointers, CostContextOver200k (*TierCost), and CostTiers ([]CostTier
		// with the embedded TierCost fields flattened).
		Description: "A schema test model.",
		Status:      bestiary.StatusBeta,
		ReasoningOptions: []bestiary.ReasoningOption{
			{Kind: bestiary.ReasoningEffort, Values: []string{"low", "high"}},
		},
		CostInputAudioPerMTok:  &cost,
		CostOutputAudioPerMTok: nil,
		CostContextOver200k:    &bestiary.TierCost{CostInputPerMTok: &cost},
		CostTiers: []bestiary.CostTier{
			{ContextSize: 200000, TierCost: bestiary.TierCost{CostOutputPerMTok: &cost}},
		},
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

	// Step 7: the ModelRef $defs sub-schema MUST also be
	// validated — the uncaught B finding was that ModelRef.Modifier stayed "type":"string"
	// while the Go field is []string. Parse $defs.ModelRef, marshal a real ModelRef with a
	// MULTI-modifier list, assert every declared property is present AND that the Modifier
	// field serializes as a JSON ARRAY of strings (the array-type fix) — not a bare string.
	var schemaDefs struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Type any `json:"type"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}
	modelRefDef, ok := schemaDefs.Defs["ModelRef"]
	if !ok || len(modelRefDef.Properties) == 0 {
		t.Fatalf("bestiary.schema.json $defs.ModelRef missing or has no properties")
	}

	ref := bestiary.ModelRef{
		ID:        "llama-3.2-11b-vision-instruct",
		Provider:  "testprovider",
		RawFamily: "llama",
		Family:    "llama",
		Variant:   "",
		Version:   "3.2",
		Date:      "",
		Modifier:  []string{"vision", "instruct"},
	}
	refJSON, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ModelRef) failed: %v", err)
	}
	var refOut map[string]any
	if err := json.Unmarshal(refJSON, &refOut); err != nil {
		t.Fatalf("could not unmarshal ModelRef JSON: %v", err)
	}
	// Bi-directional EXACT-prop conformance for $defs.ModelRef, mirroring the
	// V024/V025 deep-conformance pattern. The forward arm (schema -> output)
	// catches a declared property the Go type no longer marshals; the reverse arm
	// (output -> schema) catches a marshaled Go field the schema fails to declare.
	// The reverse arm is what a one-directional check lacked: a Go ModelRef field
	// added without a matching $def property (e.g. ParamSize) marshaled into the
	// output yet stayed GREEN. Now any such drift fails the suite.
	for prop := range modelRefDef.Properties {
		if _, ok := refOut[prop]; !ok {
			t.Errorf("ModelRef JSON output missing schema $defs.ModelRef property %q;\n"+
				"  how to fix: ensure bestiary.ModelRef has an exported field %q, or remove the property from $defs.ModelRef", prop, prop)
		}
	}
	for key := range refOut {
		if _, ok := modelRefDef.Properties[key]; !ok {
			t.Errorf("ModelRef JSON output key %q is not declared in schema $defs.ModelRef;\n"+
				"  what went wrong: a marshaled ModelRef field has no matching $def property (Go/schema drift)\n"+
				"  how to fix: add property %q to $defs.ModelRef in bestiary.schema.json", key, key)
		}
	}
	// The crux of the fix: Modifier MUST be an array, not a string.
	if mv, ok := refOut["Modifier"]; ok {
		arr, isArr := mv.([]any)
		if !isArr {
			t.Errorf("ModelRef.Modifier serialized as %T, want JSON array (schema $defs.ModelRef.Modifier must be array, not string)", mv)
		} else if len(arr) != 2 || arr[0] != "vision" || arr[1] != "instruct" {
			t.Errorf("ModelRef.Modifier = %v, want [vision instruct] in canonical order", arr)
		}
	} else {
		t.Error("ModelRef JSON output missing 'Modifier'")
	}

	// Also assert a POPULATED ModelInfo.Modifier serializes as an array (top-level schema).
	infoMM := fixture
	infoMM.Modifier = []string{"thinking", "turbo"}
	var bufMM bytes.Buffer
	if err := bestiary.FormatModel(&bufMM, infoMM, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(JSON) multi-modifier error: %v", err)
	}
	var outMM map[string]any
	if err := json.Unmarshal(bufMM.Bytes(), &outMM); err != nil {
		t.Fatalf("unmarshal multi-modifier JSON: %v", err)
	}
	if arr, ok := outMM["Modifier"].([]any); !ok || len(arr) != 2 {
		t.Errorf("ModelInfo.Modifier = %v (%T), want a 2-element JSON array", outMM["Modifier"], outMM["Modifier"])
	}
}

// TestSchemaDefs_EntityLineage_DeepConformance mirrors the Step-7 ModelRef deep
// check for the v0.2.3 additive $defs: it marshals a populated LineageEdge and a
// populated EntityRef and asserts (a) every property declared in the schema's
// $defs.LineageEdge / $defs.EntityRef is present in the marshaled output, (b)
// LineageEdge.Kind serializes as the DerivationKind enum STRING (e.g. "finetune")
// matching the schema's $defs.DerivationKind enum — never an integer, and (c) the
// newly-added $defs.ModelRef.Host property is declared in the schema.
func TestSchemaDefs_EntityLineage_DeepConformance(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}

	var schemaDefs struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Enum       []string                   `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}

	// (c) $defs.ModelRef.Host must be a declared property.
	modelRefDef, ok := schemaDefs.Defs["ModelRef"]
	if !ok {
		t.Fatalf("bestiary.schema.json $defs.ModelRef missing")
	}
	if _, ok := modelRefDef.Properties["Host"]; !ok {
		t.Errorf("bestiary.schema.json $defs.ModelRef is missing the new \"Host\" property;\n" +
			"  how to fix: add a \"Host\" property to $defs.ModelRef in bestiary.schema.json (added in schema 0.1.0)")
	}

	// $defs.EntityRef deep check.
	entityRefDef, ok := schemaDefs.Defs["EntityRef"]
	if !ok || len(entityRefDef.Properties) == 0 {
		t.Fatalf("bestiary.schema.json $defs.EntityRef missing or has no properties")
	}
	entRef := bestiary.EntityRef{
		Family:   "llama",
		Variant:  "",
		Version:  "3.1",
		Modifier: []string{"instruct"},
	}
	entRefJSON, err := json.Marshal(entRef)
	if err != nil {
		t.Fatalf("json.Marshal(EntityRef) failed: %v", err)
	}
	var entRefOut map[string]any
	if err := json.Unmarshal(entRefJSON, &entRefOut); err != nil {
		t.Fatalf("could not unmarshal EntityRef JSON: %v", err)
	}
	for prop := range entityRefDef.Properties {
		if _, ok := entRefOut[prop]; !ok {
			t.Errorf("EntityRef JSON output missing schema $defs.EntityRef property %q;\n"+
				"  how to fix: ensure bestiary.EntityRef has an exported field %q matching the schema", prop, prop)
		}
	}

	// $defs.LineageEdge deep check + Kind-as-enum-string assertion.
	lineageEdgeDef, ok := schemaDefs.Defs["LineageEdge"]
	if !ok || len(lineageEdgeDef.Properties) == 0 {
		t.Fatalf("bestiary.schema.json $defs.LineageEdge missing or has no properties")
	}
	edge := bestiary.LineageEdge{
		Parent: bestiary.EntityRef{Family: "llama", Version: "3"},
		Kind:   bestiary.DerivationFinetune,
	}
	edgeJSON, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("json.Marshal(LineageEdge) failed: %v", err)
	}
	var edgeOut map[string]any
	if err := json.Unmarshal(edgeJSON, &edgeOut); err != nil {
		t.Fatalf("could not unmarshal LineageEdge JSON: %v", err)
	}
	for prop := range lineageEdgeDef.Properties {
		if _, ok := edgeOut[prop]; !ok {
			t.Errorf("LineageEdge JSON output missing schema $defs.LineageEdge property %q;\n"+
				"  how to fix: ensure bestiary.LineageEdge has an exported field %q matching the schema", prop, prop)
		}
	}
	// The crux: Kind MUST be the DerivationKind enum STRING, not an integer.
	kindVal, ok := edgeOut["Kind"]
	if !ok {
		t.Fatal("LineageEdge JSON output missing 'Kind'")
	}
	kindStr, isStr := kindVal.(string)
	if !isStr {
		t.Errorf("LineageEdge.Kind serialized as %T, want a JSON string (DerivationKind enum); a float/integer means MarshalText was bypassed", kindVal)
	} else {
		if kindStr != "finetune" {
			t.Errorf("LineageEdge.Kind = %q, want \"finetune\"", kindStr)
		}
		// And it must be a member of the schema's $defs.DerivationKind enum.
		dkDef, ok := schemaDefs.Defs["DerivationKind"]
		if !ok || len(dkDef.Enum) == 0 {
			t.Fatalf("bestiary.schema.json $defs.DerivationKind missing or has no enum")
		}
		if !slices.Contains(dkDef.Enum, kindStr) {
			t.Errorf("LineageEdge.Kind=%q is not a member of schema $defs.DerivationKind.enum %v", kindStr, dkDef.Enum)
		}
	}
}

// TestSchema_BackwardCompat_ZeroValueFields pins INV2: the v0.2.3 additive fields
// (Host, Lineage) are backward-compatible. A v0.2.2-shaped record (Host:"" /
// Lineage:nil) still conforms to schema 0.1.0, and crucially Host/Lineage are NOT
// listed in any required[] array (top-level ModelInfo or $defs.ModelRef) — so a
// document predating these fields validates without them. This was previously
// true only by inspection; this test makes it a regression gate.
func TestSchema_BackwardCompat_ZeroValueFields(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}

	// Parse top-level required + properties, and $defs required arrays.
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
		Defs       map[string]struct {
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("could not unmarshal bestiary.schema.json: %v", err)
	}

	// (a) Host/Lineage must NOT be in the top-level ModelInfo required[].
	for _, f := range []string{"Host", "Lineage"} {
		if slices.Contains(schema.Required, f) {
			t.Errorf("ModelInfo schema required[] contains %q; additive v0.2.3 fields must stay OPTIONAL for backward compatibility (INV2)", f)
		}
	}
	// (b) Host must NOT be in $defs.ModelRef.required[].
	if mr, ok := schema.Defs["ModelRef"]; ok {
		if slices.Contains(mr.Required, "Host") {
			t.Errorf("$defs.ModelRef required[] contains \"Host\"; it must stay OPTIONAL for backward compatibility (INV2)")
		}
	}

	// (c) A v0.2.2-shaped record (no Host/Lineage values) still conforms: every
	// schema property appears in output and no extra keys appear. Host:"" and
	// Lineage:nil are the zero values a pre-0.1.0 record would carry.
	cost := 1.5
	v022 := bestiary.ModelInfo{
		ID:               "legacy-model-20240101",
		Provider:         "testprovider",
		DisplayName:      "Legacy Model",
		RawFamily:        "legacy",
		Family:           "legacy",
		Variant:          "",
		Date:             "2024-01-01",
		ContextWindow:    8000,
		MaxOutput:        2048,
		Interleaved:      bestiary.Capability{Supported: false},
		CostInputPerMTok: &cost,
		ReleaseDate:      "2024-01-01",
		Knowledge:        "2024-01",
		Modalities: bestiary.Modalities{
			Input:  []bestiary.Modality{bestiary.ModalityText},
			Output: []bestiary.Modality{bestiary.ModalityText},
		},
		LastSynced: "2024-01-01T00:00:00Z",
		// Host and Lineage deliberately left at zero value (Host:"" / Lineage:nil).
	}
	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, v022, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(legacy record) error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal legacy record JSON: %v", err)
	}
	for prop := range schema.Properties {
		if _, ok := out[prop]; !ok {
			t.Errorf("legacy record missing schema property %q", prop)
		}
	}
	for key := range out {
		if _, ok := schema.Properties[key]; !ok {
			t.Errorf("legacy record has key %q not declared in schema", key)
		}
	}
	// The zero values must serialize as expected for a backward-compatible record.
	if out["Host"] != "" {
		t.Errorf("legacy record Host = %v, want \"\" (zero value)", out["Host"])
	}
	if out["Lineage"] != nil {
		t.Errorf("legacy record Lineage = %v, want null (zero value)", out["Lineage"])
	}
}

// TestJSONOutput_CanonicalFields_Populated verifies that a ModelInfo fixture with
// Family, Variant, Version, and Date set to non-empty values round-trips correctly
// through JSON marshaling.
//
// This exercises the codegen-baked normalization path.
func TestJSONOutput_CanonicalFields_Populated(t *testing.T) {
	cost := 2.5
	fixture := bestiary.ModelInfo{
		ID:                    "claude-opus-4-5-20251101",
		Provider:              "anthropic",
		DisplayName:           "Claude Opus 4.5",
		RawFamily:             "claude-opus",
		Family:                "claude",
		Variant:               "opus",
		Version:               "4.5",
		Date:                  "2025-11-01",
		ContextWindow:         200000,
		MaxOutput:             32000,
		Reasoning:             true,
		ToolCall:              true,
		Attachment:            true,
		Temperature:           true,
		StructuredOutput:      true,
		Interleaved:           bestiary.Capability{Supported: false},
		OpenWeights:           false,
		CostInputPerMTok:      &cost,
		CostOutputPerMTok:     &cost,
		CostReasoningPerMTok:  nil,
		CostCacheReadPerMTok:  nil,
		CostCacheWritePerMTok: nil,
		ReleaseDate:           "2025-11-01",
		Knowledge:             "2025-01",
		Modalities: bestiary.Modalities{
			Input:  []bestiary.Modality{bestiary.ModalityText, bestiary.ModalityImage},
			Output: []bestiary.Modality{bestiary.ModalityText},
		},
		LastSynced: "2025-11-01T12:00:00Z",
	}

	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, fixture, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	checks := map[string]string{
		"Family":  "claude",
		"Variant": "opus",
		"Version": "4.5",
		"Date":    "2025-11-01",
	}
	for field, want := range checks {
		v, ok := got[field]
		if !ok {
			t.Errorf("field %q missing from JSON output;\n  how to fix: ensure ModelInfo.%s is exported and marshaled", field, field)
			continue
		}
		if got, ok := v.(string); !ok || got != want {
			t.Errorf("field %q: got %v (%T), want %q;\n  why: canonical fields must be string values", field, v, v, want)
		}
	}
}

// TestModelRef_AllFields_Present validates that ModelRef can be JSON-marshaled
// with all 7 fields present and round-trips correctly.
//
// ModelRef is documented in the $defs/ModelRef section of bestiary.schema.json.
func TestModelRef_AllFields_Present(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:        "claude-opus-4-20250514",
		Provider:  "anthropic",
		RawFamily: "claude-opus",
		Family:    "claude",
		Variant:   "opus",
		Version:   "",
		Date:      "2025-05-14",
	}

	enc, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ModelRef) failed: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(enc, &got); err != nil {
		t.Fatalf("json.Unmarshal(ModelRef) failed: %v", err)
	}

	// All 7 schema fields must be present.
	required := []string{"ID", "Provider", "RawFamily", "Family", "Variant", "Version", "Date"}
	for _, field := range required {
		if _, ok := got[field]; !ok {
			t.Errorf(
				"ModelRef JSON missing required field %q;\n"+
					"  what went wrong: field absent from marshaled output\n"+
					"  why: ModelRef.%s may be unexported or missing\n"+
					"  where: schema_test.go TestModelRef_AllFields_Present\n"+
					"  how to fix: ensure ModelRef.%s is exported and present in bestiary.go/modelref.go",
				field, field, field,
			)
		}
	}
}

// TestDesignation_AllAcceptabilityRatings validates that each AcceptabilityRating
// constant serializes to the expected JSON string value, matching the
// $defs/AcceptabilityRating enum in bestiary.schema.json.
//
// Accepted values: "admitted", "preferred", "deprecated".
// Also validates that Scheme serializes to its expected string wire value,
// matching the $defs/CanonicalScheme enum.
func TestDesignation_AllAcceptabilityRatings(t *testing.T) {
	cases := []struct {
		rating     bestiary.AcceptabilityRating
		want       string
		scheme     bestiary.CanonicalScheme
		wantScheme string
	}{
		{bestiary.AcceptabilityAdmitted, "admitted", bestiary.SchemeRaw, "raw"},
		{bestiary.AcceptabilityPreferred, "preferred", bestiary.SchemeCanonical, "canonical"},
		{bestiary.AcceptabilityDeprecated, "deprecated", bestiary.SchemeHuggingFace, "huggingface"},
	}

	for _, tc := range cases {
		d := bestiary.Designation{
			Value:    "test-model",
			Scheme:   tc.scheme,
			Provider: "testprovider",
			Rating:   tc.rating,
		}

		enc, err := json.Marshal(d)
		if err != nil {
			t.Fatalf("json.Marshal(Designation{Rating:%v}) failed: %v", tc.rating, err)
		}

		var got map[string]any
		if err := json.Unmarshal(enc, &got); err != nil {
			t.Fatalf("json.Unmarshal(Designation) failed: %v", err)
		}

		// Assert Rating wire value is a string matching the schema enum.
		ratingWire, ok := got["Rating"]
		if !ok {
			t.Errorf(
				"Designation JSON missing \"Rating\" field for rating %v;\n"+
					"  what went wrong: Rating key absent from marshaled output\n"+
					"  why: AcceptabilityRating.MarshalJSON may not be implemented\n"+
					"  where: schema_test.go TestDesignation_AllAcceptabilityRatings\n"+
					"  how to fix: implement MarshalJSON on AcceptabilityRating to emit a string",
				tc.rating,
			)
		} else if ratingStr, ok := ratingWire.(string); !ok || ratingStr != tc.want {
			t.Errorf(
				"Designation[Rating] wire value = %v (%T), want string %q;\n"+
					"  what went wrong: AcceptabilityRating serializes as non-string or wrong value\n"+
					"  why: MarshalJSON must call String() and emit the result as a JSON string\n"+
					"  where: schema_test.go TestDesignation_AllAcceptabilityRatings\n"+
					"  how to fix: ensure AcceptabilityRating.MarshalJSON returns json.Marshal(r.String())",
				ratingWire, ratingWire, tc.want,
			)
		}

		// Assert Scheme wire value is a string matching the schema enum.
		schemeWire, ok := got["Scheme"]
		if !ok {
			t.Errorf(
				"Designation JSON missing \"Scheme\" field for scheme %v;\n"+
					"  what went wrong: Scheme key absent from marshaled output\n"+
					"  why: CanonicalScheme.MarshalJSON may not be implemented\n"+
					"  where: schema_test.go TestDesignation_AllAcceptabilityRatings\n"+
					"  how to fix: implement MarshalJSON on CanonicalScheme to emit a string",
				tc.scheme,
			)
		} else if schemeStr, ok := schemeWire.(string); !ok || schemeStr != tc.wantScheme {
			t.Errorf(
				"Designation[Scheme] wire value = %v (%T), want string %q;\n"+
					"  what went wrong: CanonicalScheme serializes as non-string or wrong value\n"+
					"  why: MarshalJSON must call String() and emit the result as a JSON string\n"+
					"  where: schema_test.go TestDesignation_AllAcceptabilityRatings\n"+
					"  how to fix: ensure CanonicalScheme.MarshalJSON returns json.Marshal(s.String())",
				schemeWire, schemeWire, tc.wantScheme,
			)
		}

		// Designations() from a ModelRef now carries ACTIVE acceptability: the
		// canonical designation is Preferred, the others are Admitted. This ref is
		// anthropic-hosted, so it carries THREE designations — the purl entry is
		// minted only for a ref whose registry home is known (HuggingFace-hosted),
		// and is dropped rather than emitted empty-valued for every other provider.
		if tc.rating == bestiary.AcceptabilityAdmitted {
			ref := bestiary.ModelRef{
				ID:       "claude-opus-4-20250514",
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "",
				Date:     "2025-05-14",
			}
			designations := ref.Designations()
			if len(designations) != 3 {
				t.Errorf(
					"ModelRef.Designations() returned %d designations, want 3;\n"+
						"  what: expected Raw, Canonical and HuggingFace designations (no PURL for a non-HuggingFace provider)\n"+
						"  where: schema_test.go TestDesignation_AllAcceptabilityRatings",
					len(designations),
				)
			}
			for i, dg := range designations {
				want := bestiary.AcceptabilityAdmitted
				if dg.Scheme == bestiary.SchemeCanonical {
					want = bestiary.AcceptabilityPreferred
				}
				if dg.Rating != want {
					t.Errorf(
						"Designation[%d] (scheme %v).Rating = %v, want %v;\n"+
							"  what: the canonical designation is Preferred; raw/HF/PURL stay Admitted\n"+
							"  where: schema_test.go TestDesignation_AllAcceptabilityRatings",
						i, dg.Scheme, dg.Rating, want,
					)
				}
			}
		}
	}
}

// TestResolve_ErrAmbiguous validates that Resolve returns *ErrAmbiguous when
// an input matches multiple distinct canonical models (e.g. a family name
// shared by several variants). This exercises the ErrAmbiguous error type
// documented in bestiary.schema.json (see package errors.go).
//
// The static registry must contain at least two models with the same
// canonical Family but different variants for this test to be meaningful.
func TestResolve_ErrAmbiguous(t *testing.T) {
	// "claude" matches claude/opus, claude/sonnet, claude/haiku, etc. in the
	// static registry. This should trigger ErrAmbiguous because multiple distinct
	// canonical triples (Family+Variant+Date) match a non-exact-ID input.
	_, err := bestiary.Resolve("claude", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err == nil {
		// The static registry must always contain multiple claude variants (opus, sonnet, haiku, etc.).
		// If Resolve returns nil here, the registry has shrunk below the expected threshold — surface
		// this as a hard failure so registry regressions are caught immediately.
		t.Fatal(
			"Resolve(\"claude\", SchemeCanonical) returned nil error, want *ErrAmbiguous;\n" +
				"  what went wrong: no ambiguity detected for a family name that matches many claude variants\n" +
				"  why: the static registry must contain at least 2 distinct canonical triples for 'claude' (opus, sonnet, haiku, ...)\n" +
				"  where: schema_test.go TestResolve_ErrAmbiguous\n" +
				"  how to fix: check that models_static_gen.go still contains claude-opus, claude-sonnet, and claude-haiku entries; re-run go generate ./... if the static data shrank",
		)
	}

	var ambig *bestiary.ErrAmbiguous
	if !isErrAmbiguous(err, &ambig) {
		t.Fatalf(
			"Resolve(\"claude\") returned non-*ErrAmbiguous error: %T %v;\n"+
				"  what went wrong: expected *ErrAmbiguous for an ambiguous prefix input\n"+
				"  why: the static registry may have changed or Resolve disambiguation logic changed\n"+
				"  where: schema_test.go TestResolve_ErrAmbiguous\n"+
				"  how to fix: check Resolve in resolve.go and ensure >1 canonical matches \"claude\"",
			err, err,
		)
	}

	// ErrAmbiguous must carry structured payload.
	if ambig.Input == "" {
		t.Error("ErrAmbiguous.Input is empty; want the original query string")
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf(
			"ErrAmbiguous.Candidates has %d entry(ies), want >=2;\n"+
				"  what: ambiguous resolution must carry at least 2 candidate ModelRefs\n"+
				"  why: ErrAmbiguous is only returned when >1 distinct canonical is matched\n"+
				"  where: schema_test.go TestResolve_ErrAmbiguous",
			len(ambig.Candidates),
		)
	}
	// Each candidate must be a valid ModelRef with non-empty ID and Provider.
	for i, c := range ambig.Candidates {
		if string(c.ID) == "" {
			t.Errorf("ErrAmbiguous.Candidates[%d].ID is empty", i)
		}
		if string(c.Provider) == "" {
			t.Errorf("ErrAmbiguous.Candidates[%d].Provider is empty", i)
		}
	}
}

// isErrAmbiguous reports whether err is or wraps *bestiary.ErrAmbiguous.
// It is used instead of errors.As because ErrAmbiguous has no Unwrap method
// and this call site must avoid importing "errors" in the test file.
func isErrAmbiguous(err error, target **bestiary.ErrAmbiguous) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*bestiary.ErrAmbiguous); ok {
		if target != nil {
			*target = e
		}
		return true
	}
	return false
}

// TestSchemaDefs_V024_DeepConformance mirrors the Step-7 ModelRef / EntityRef /
// LineageEdge deep checks for the v0.2.4 additive $defs. For each new type it
// marshals a populated Go value (the actual production type, not a hand-rolled
// shape) and asserts every property the schema $def declares is present in the
// marshaled output — locking the schema to the Go contract. It also pins the
// load-bearing v0.2.4 invariants:
//   - $defs.EntityRef.ParamSize is a declared property (the #size identity carrier).
//   - $defs.QuantVRAM.Quant references the Quantization enum and serializes as a
//     member of that enum's string values.
//   - $defs.DatasetIngested carries NO "uri" property (BCNF transitive-dep removal).
func TestSchemaDefs_V024_DeepConformance(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}

	var schemaDefs struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
			Enum       []string                   `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}

	// (1) LOCKED: $defs.EntityRef.ParamSize must be a declared property — the
	// #size identity carrier. A revert of the ParamSize addition must fail here.
	entityRefDef, ok := schemaDefs.Defs["EntityRef"]
	if !ok {
		t.Fatalf("bestiary.schema.json $defs.EntityRef missing")
	}
	if _, ok := entityRefDef.Properties["ParamSize"]; !ok {
		t.Errorf("bestiary.schema.json $defs.EntityRef is missing the \"ParamSize\" property;\n" +
			"  how to fix: add a \"ParamSize\" property to $defs.EntityRef (added in schema 0.2.0; the #size identity carrier)")
	}

	// (2) LOCKED: $defs.DatasetIngested must carry NO "uri" property. The URI is a
	// transitive dependency obtained by FK join to DataSource; a uri reappearing
	// here would re-introduce the BCNF normalization defect.
	dsiDef, ok := schemaDefs.Defs["DatasetIngested"]
	if !ok || len(dsiDef.Properties) == 0 {
		t.Fatalf("bestiary.schema.json $defs.DatasetIngested missing or has no properties")
	}
	for prop := range dsiDef.Properties {
		if strings.EqualFold(prop, "uri") {
			t.Errorf("bestiary.schema.json $defs.DatasetIngested declares a %q property; it MUST NOT — the URI is a transitive dependency reached by FK join to DataSource (BCNF). Remove it.", prop)
		}
	}

	// (3) $defs.Quantization is a string enum whose values are the canonical
	// lowercase wire names. Assert it exists, is non-empty, and that a marshaled
	// Quantization (via QuantVRAM.Quant) is a member of it.
	quantDef, ok := schemaDefs.Defs["Quantization"]
	if !ok || len(quantDef.Enum) == 0 {
		t.Fatalf("bestiary.schema.json $defs.Quantization missing or has no enum")
	}

	// Per-$def deep check: marshal a populated production value and assert
	//   (a) the schema $def declares EXACTLY the expected property set
	//       (catches a dropped or renamed schema property — the mutation guard), and
	//   (b) every declared property is present in the marshaled production output
	//       (catches a Go field that drifted away from the schema).
	//
	// expectTypes pins each property's DECLARED schema "type" against the Go
	// contract so a type-flip in the schema (e.g. DatasetIngested.ParserSchema
	// integer->string) is falsifiable. Values are the canonical type rendering
	// used by canonSchemaType: a scalar keyword ("string"/"integer"/"number"/
	// "boolean"/"array"), a sorted "|"-joined union for nullable arrays
	// ("array|null"), or the sentinel "$ref" for $ref-valued properties.
	// Polymorphic oneOf properties (the nullable-cost numbers) carry no plain
	// "type" node and are deliberately omitted (explicit allowlist).
	type deepCheck struct {
		defName     string
		value       any
		expectProps []string
		expectTypes map[string]string
		// projectionProps are schema properties that are DERIVED projections (a method
		// on the Go type, surfaced by a CLI wrapper) rather than struct fields, so they
		// are legitimately absent from the plain-value marshal. They must still be
		// declared in the $def (the expectProps arms above enforce that), but are
		// exempt from the marshaled-output presence check.
		projectionProps []string
	}
	cost := 2.0
	checks := []deepCheck{
		{
			defName: "QuantVRAM",
			value: bestiary.QuantVRAM{
				Quant:               bestiary.QuantQ4_K_M,
				QuantRaw:            "Q4_K_M",
				WeightsBytes:        42_000_000_000,
				VRAMBytes:           45_000_000_000,
				VRAMContextTokens:   131072,
				Layers:              80,
				KVHeads:             8,
				HeadDim:             128,
				VRAMEstimatePartial: false,
			},
			expectProps: []string{"Quant", "QuantRaw", "WeightsBytes", "VRAMBytes", "VRAMContextTokens", "Layers", "KVHeads", "HeadDim", "VRAMEstimatePartial"},
			expectTypes: map[string]string{
				"Quant":               "$ref",
				"QuantRaw":            "string",
				"WeightsBytes":        "integer",
				"VRAMBytes":           "integer",
				"VRAMContextTokens":   "integer",
				"Layers":              "integer",
				"KVHeads":             "integer",
				"HeadDim":             "integer",
				"VRAMEstimatePartial": "boolean",
			},
		},
		{
			defName: "ProviderInstance",
			value: bestiary.ProviderInstance{
				ID:                "llama-3.3-70b",
				Provider:          "ollama",
				Host:              bestiary.HostNone,
				CostInputPerMTok:  &cost,
				CostOutputPerMTok: nil,
				ContextWindow:     131072,
				MaxOutput:         8192,
				QuantVRAM: []bestiary.QuantVRAM{
					{Quant: bestiary.QuantQ4_K_M, QuantRaw: "Q4_K_M", WeightsBytes: 42_000_000_000},
				},
				Source: bestiary.DataSourceOllama,
			},
			expectProps: []string{"ID", "Provider", "Host", "Region", "RegionRaw", "CostInputPerMTok", "CostOutputPerMTok", "ContextWindow", "MaxOutput", "QuantVRAM", "Source"},
			expectTypes: map[string]string{
				"ID":       "string",
				"Provider": "string",
				"Host":     "string",
				"Region":   "$ref",
				// RegionRaw is a plain string sibling (the RegionOther carrier).
				"RegionRaw": "string",
				// CostInputPerMTok / CostOutputPerMTok: oneOf{number,null} — no plain "type" node (allowlisted).
				"ContextWindow": "integer",
				"MaxOutput":     "integer",
				"QuantVRAM":     "array|null",
				"Source":        "string",
			},
		},
		{
			defName: "CapabilityUnion",
			value: bestiary.CapabilityUnion{
				Reasoning: true, ToolCall: true, OpenWeights: true,
			},
			expectProps: []string{"Reasoning", "ToolCall", "Attachment", "Temperature", "StructuredOutput", "Interleaved", "OpenWeights"},
			expectTypes: map[string]string{
				"Reasoning":        "boolean",
				"ToolCall":         "boolean",
				"Attachment":       "boolean",
				"Temperature":      "boolean",
				"StructuredOutput": "boolean",
				"Interleaved":      "boolean",
				"OpenWeights":      "boolean",
			},
		},
		{
			defName: "EntityRef",
			value: bestiary.EntityRef{
				Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"},
			},
			expectProps: []string{"Family", "Variant", "Version", "ParamSize", "Modifier"},
			expectTypes: map[string]string{
				"Family":    "string",
				"Variant":   "string",
				"Version":   "string",
				"ParamSize": "string",
				"Modifier":  "array|null",
			},
		},
		{
			defName: "DataSource",
			value: bestiary.DataSource{
				ID: bestiary.DataSourceOllama, URI: "https://ollama.com", CanonicalName: "Ollama",
			},
			expectProps: []string{"ID", "URI", "CanonicalName"},
			expectTypes: map[string]string{
				"ID":            "string",
				"URI":           "string",
				"CanonicalName": "string",
			},
		},
		{
			defName: "DatasetIngested",
			value: bestiary.DatasetIngested{
				SourceID: bestiary.DataSourceOllama, IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 1,
			},
			expectProps: []string{"SourceID", "IngestedAt", "ParserSchema"},
			expectTypes: map[string]string{
				"SourceID":     "string",
				"IngestedAt":   "string",
				"ParserSchema": "integer",
			},
		},
		{
			defName: "EntitySource",
			value: bestiary.EntitySource{
				EntityKey: "llama@3.3#70b{instruct}", SourceID: bestiary.DataSourceOllama,
			},
			expectProps: []string{"EntityKey", "SourceID"},
			expectTypes: map[string]string{
				"EntityKey": "string",
				"SourceID":  "string",
			},
		},
		{
			defName: "Entity",
			value: bestiary.Entity{
				Ref:     bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
				Sources: []bestiary.DataSourceID{bestiary.DataSourceModelsDev, bestiary.DataSourceOllama},
			},
			expectProps: []string{"Ref", "Instances", "Lineage", "Providers", "Hosts", "Regions", "Nomina", "PriceInputRange", "PriceOutputRange", "ContextRange", "MaxOutputRange", "Capabilities", "Sources", "Metadata", "Creator"},
			expectTypes: map[string]string{
				"Ref":              "$ref",
				"Instances":        "array|null",
				"Lineage":          "array|null",
				"Providers":        "array|null",
				"Hosts":            "array|null",
				"Regions":          "array|null",
				"PriceInputRange":  "array",
				"PriceOutputRange": "array",
				"ContextRange":     "array",
				"MaxOutputRange":   "array",
				"Capabilities":     "$ref",
				"Sources":          "array|null",
				// Creator is a DERIVED join projection surfaced as a plain struct
				// field ($ref #/$defs/Creator; a hand-constructed Entity carries the
				// CreatorNone zero value). Added in schema 0.6.0.
				"Creator": "$ref",
				// Metadata is oneOf{$ref EntityMetadata, null} — no plain "type"
				// node and no direct "$ref" (it is inside the oneOf), so it is
				// allowlisted from the type cross-check (added in schema 0.3.0).
				// Nomina is a DERIVED PROJECTION (Entity.Nomina() method, surfaced by
				// the CLI wrapper), not a struct field, so it is intentionally absent
				// from the plain-Entity marshal and allowlisted from the presence +
				// type cross-check via projectionProps below (added in schema 0.5.0).
			},
			projectionProps: []string{"Nomina"},
		},
	}

	for _, c := range checks {
		def, ok := schemaDefs.Defs[c.defName]
		if !ok || len(def.Properties) == 0 {
			t.Errorf("bestiary.schema.json $defs.%s missing or has no properties", c.defName)
			continue
		}
		// (a) the schema $def must declare EXACTLY the expected property set. A
		// dropped property fails the first arm; an undocumented extra property
		// fails the second. This is what makes "remove a $def property" a falsifier.
		for _, want := range c.expectProps {
			if _, ok := def.Properties[want]; !ok {
				t.Errorf("bestiary.schema.json $defs.%s is missing the %q property;\n"+
					"  how to fix: add a %q property to $defs.%s (it is part of the v0.2.4 contract)",
					c.defName, want, want, c.defName)
			}
		}
		for got := range def.Properties {
			if !slices.Contains(c.expectProps, got) {
				t.Errorf("bestiary.schema.json $defs.%s declares an unexpected property %q;\n"+
					"  how to fix: remove it or add it to the expected set in this test if it is intended",
					c.defName, got)
			}
		}
		enc, err := json.Marshal(c.value)
		if err != nil {
			t.Errorf("json.Marshal(%s) failed: %v", c.defName, err)
			continue
		}
		// Decode with UseNumber so numeric JSON values arrive as json.Number,
		// letting the integer/number type check below distinguish "42" from "42.0".
		var out map[string]any
		dec := json.NewDecoder(bytes.NewReader(enc))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			t.Errorf("could not unmarshal %s JSON: %v", c.defName, err)
			continue
		}
		for prop := range def.Properties {
			if slices.Contains(c.projectionProps, prop) {
				continue // a derived-projection property is not in the plain-value marshal
			}
			if _, ok := out[prop]; !ok {
				t.Errorf("%s JSON output is missing schema $defs.%s property %q;\n"+
					"  how to fix: ensure bestiary.%s has an exported field %q matching the schema $def",
					c.defName, c.defName, prop, c.defName, prop)
			}
		}
		// (c) Pin the DECLARED schema "type" of every checked property against the
		// Go contract, and cross-check the marshaled value's JSON kind. This is what
		// makes a type-flip (e.g. ParserSchema integer->string) a falsifier: the
		// presence-only checks above never read the declared "type".
		for prop, want := range c.expectTypes {
			raw, ok := def.Properties[prop]
			if !ok {
				continue // a missing property is already reported by the presence check above
			}
			var node struct {
				Type json.RawMessage `json:"type"`
				Ref  string          `json:"$ref"`
			}
			if err := json.Unmarshal(raw, &node); err != nil {
				t.Errorf("could not decode $defs.%s.%s for type check: %v", c.defName, prop, err)
				continue
			}
			if want == "$ref" {
				if node.Ref == "" {
					t.Errorf("$defs.%s.%s expected to be a $ref property, but no \"$ref\" is declared;\n"+
						"  how to fix: restore the \"$ref\" for $defs.%s.%s in bestiary.schema.json",
						c.defName, prop, c.defName, prop)
				}
				continue
			}
			got, ok := canonSchemaType(node.Type)
			if !ok {
				t.Errorf("$defs.%s.%s has no decodable \"type\" node (expected %q);\n"+
					"  how to fix: declare \"type\": %q on $defs.%s.%s",
					c.defName, prop, want, want, c.defName, prop)
				continue
			}
			if got != want {
				t.Errorf("$defs.%s.%s declares type %q, want %q;\n"+
					"  what went wrong: the schema property type drifted from the Go contract\n"+
					"  why: the published 0.2.0 schema type no longer matches what the production type marshals\n"+
					"  impact: a conforming validator would REJECT valid bestiary records carrying this field\n"+
					"  how to fix: set the \"type\" of $defs.%s.%s back to %q in bestiary.schema.json",
					c.defName, prop, got, want, c.defName, prop, want)
			}
			// Cross-check the marshaled production value's JSON kind for this property,
			// when present and non-null. Catches Go-side drift (a field whose Go type
			// no longer marshals to the declared schema type). json.Number lets the
			// integer branch assert the value carries no decimal point.
			v, present := out[prop]
			if !present || v == nil {
				continue // null branch of a nullable union, or an absent optional — schema-side check above stands
			}
			switch want {
			case "string":
				if _, ok := v.(string); !ok {
					t.Errorf("$defs.%s.%s: marshaled value is %T, want a JSON string", c.defName, prop, v)
				}
			case "boolean":
				if _, ok := v.(bool); !ok {
					t.Errorf("$defs.%s.%s: marshaled value is %T, want a JSON boolean", c.defName, prop, v)
				}
			case "integer":
				n, ok := v.(json.Number)
				if !ok {
					t.Errorf("$defs.%s.%s: marshaled value is %T, want a JSON integer", c.defName, prop, v)
				} else if strings.ContainsAny(n.String(), ".eE") {
					t.Errorf("$defs.%s.%s: marshaled value %q is not an integer (schema declares \"integer\")", c.defName, prop, n.String())
				}
			case "number":
				if _, ok := v.(json.Number); !ok {
					t.Errorf("$defs.%s.%s: marshaled value is %T, want a JSON number", c.defName, prop, v)
				}
			case "array", "array|null":
				if _, ok := v.([]any); !ok {
					t.Errorf("$defs.%s.%s: marshaled value is %T, want a JSON array", c.defName, prop, v)
				}
			}
		}
		// Where the marshaled value carries a Quant token, assert it is a member of
		// the Quantization enum (the $ref wiring is enforced behaviorally here).
		if c.defName == "QuantVRAM" {
			if qv, ok := out["Quant"].(string); ok {
				if !slices.Contains(quantDef.Enum, qv) {
					t.Errorf("QuantVRAM.Quant=%q is not a member of schema $defs.Quantization.enum %v", qv, quantDef.Enum)
				}
			} else {
				t.Errorf("QuantVRAM.Quant serialized as %T, want a JSON string (Quantization enum)", out["Quant"])
			}
		}
	}
}

// canonSchemaType renders a JSON-Schema "type" node — which may be a single
// string ("integer") or an array of strings (["array","null"]) — as a stable,
// comparable canonical string. Array forms are sorted and "|"-joined so member
// order in the schema document is immaterial (["array","null"] and
// ["null","array"] both render "array|null"). It returns ok=false when the raw
// node is empty or not a string/[]string (e.g. a property that uses oneOf or
// $ref instead of a plain "type").
func canonSchemaType(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single, true
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil && len(many) > 0 {
		s := append([]string(nil), many...)
		slices.Sort(s)
		return strings.Join(s, "|"), true
	}
	return "", false
}

// TestSchemaDefs_Quantization_EnumExactSet pins the published
// $defs.Quantization.enum to be EXACTLY the set of canonical wire names produced
// by marshaling every member of the closed Go enum (QuantizationNone ..
// QuantizationOther). It asserts set equality in BOTH directions and self-extends
// as the Go enum grows — adding a new Quantization constant automatically widens
// the expected set with no test edit.
//
// This closes the gap where the prior deep-check only verified the ONE marshaled
// value it exercised (q4_k_m) was a member: 35 of 36 members could silently
// vanish from the schema, or any number of phantom members could be added, with
// the suite still green. Both are now falsifiers:
//   - a member dropped from the schema (e.g. iq2_xxs) fails direction 1 — a
//     conforming validator would reject valid CLI output quantized at that format;
//   - a phantom member added to the schema (e.g. q9_zz_bogus) fails direction 2 —
//     a validator would falsely admit a token Quantization.UnmarshalText refuses.
func TestSchemaDefs_Quantization_EnumExactSet(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}
	var schemaDefs struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}
	quantDef, ok := schemaDefs.Defs["Quantization"]
	if !ok || len(quantDef.Enum) == 0 {
		t.Fatalf("bestiary.schema.json $defs.Quantization missing or has no enum")
	}

	// Authoritative set: marshal every member of the closed Go enum. Iterating
	// QuantizationNone..QuantizationOther makes this self-extend when the enum grows.
	goNames := make([]string, 0, int(bestiary.QuantizationOther)+1)
	for q := bestiary.QuantizationNone; q <= bestiary.QuantizationOther; q++ {
		b, err := q.MarshalText()
		if err != nil {
			t.Fatalf("Quantization(%d).MarshalText() failed: %v", int(q), err)
		}
		goNames = append(goNames, string(b))
	}

	// Direction 1: every Go wire name must appear in the schema enum.
	for _, name := range goNames {
		if !slices.Contains(quantDef.Enum, name) {
			t.Errorf("schema $defs.Quantization.enum is MISSING Go wire name %q;\n"+
				"  what went wrong: the closed Go enum marshals %q but the published schema enum omits it\n"+
				"  why: a member was dropped from (or never added to) $defs.Quantization.enum\n"+
				"  impact: a conforming validator would REJECT valid CLI output quantized at %q\n"+
				"  how to fix: add %q to $defs.Quantization.enum in bestiary.schema.json",
				name, name, name, name)
		}
	}

	// Direction 2: every schema enum member must be produced by some Go value.
	for _, member := range quantDef.Enum {
		if !slices.Contains(goNames, member) {
			t.Errorf("schema $defs.Quantization.enum contains PHANTOM member %q with no Go counterpart;\n"+
				"  what went wrong: the schema enum lists %q but no Quantization constant marshals to it\n"+
				"  why: a spurious member was added to $defs.Quantization.enum\n"+
				"  impact: a validator would falsely ADMIT %q, which Quantization.UnmarshalText then refuses\n"+
				"  how to fix: remove %q from $defs.Quantization.enum, or add a matching Go constant",
				member, member, member, member)
		}
	}

	// Exact set: equal cardinality after both subset checks ⇒ set equality, and
	// also catches a duplicated schema member (which both subset checks would miss).
	if len(quantDef.Enum) != len(goNames) {
		t.Errorf("schema $defs.Quantization.enum has %d members, the Go enum has %d;\n"+
			"  the schema enum must be EXACTLY the MarshalText set over QuantizationNone..QuantizationOther\n"+
			"  (a count mismatch with all members present indicates a duplicate member in the schema enum)",
			len(quantDef.Enum), len(goNames))
	}
}

// TestDataSource_JSONSchemaRoundTrip verifies that the BCNF provenance types
// (DataSource, DatasetIngested-with-no-uri, EntitySource) and the Entity.Sources
// projection round-trip through JSON and conform to their schema $defs. It marshals
// each production value, round-trips it back through Unmarshal, asserts field
// fidelity, and re-asserts the key invariants against the schema document.
func TestDataSource_JSONSchemaRoundTrip(t *testing.T) {
	// DataSource round-trip.
	ds := bestiary.DataSource{
		ID:            bestiary.DataSourceModelsDev,
		URI:           "https://models.dev/api.json",
		CanonicalName: "models.dev",
	}
	dsEnc, err := json.Marshal(ds)
	if err != nil {
		t.Fatalf("json.Marshal(DataSource) failed: %v", err)
	}
	var dsBack bestiary.DataSource
	if err := json.Unmarshal(dsEnc, &dsBack); err != nil {
		t.Fatalf("json.Unmarshal(DataSource) failed: %v", err)
	}
	if dsBack != ds {
		t.Errorf("DataSource round-trip mismatch: got %+v, want %+v", dsBack, ds)
	}

	// DatasetIngested round-trip + NO uri in the serialized form.
	di := bestiary.DatasetIngested{
		SourceID:     bestiary.DataSourceOllama,
		IngestedAt:   "2026-06-01T00:00:00Z",
		ParserSchema: 1,
	}
	diEnc, err := json.Marshal(di)
	if err != nil {
		t.Fatalf("json.Marshal(DatasetIngested) failed: %v", err)
	}
	var diMap map[string]any
	if err := json.Unmarshal(diEnc, &diMap); err != nil {
		t.Fatalf("could not unmarshal DatasetIngested JSON: %v", err)
	}
	for k := range diMap {
		if strings.EqualFold(k, "uri") {
			t.Errorf("DatasetIngested serialized a %q key; it MUST carry no URI (transitive dep reached via FK join to DataSource)", k)
		}
	}
	var diBack bestiary.DatasetIngested
	if err := json.Unmarshal(diEnc, &diBack); err != nil {
		t.Fatalf("json.Unmarshal(DatasetIngested) failed: %v", err)
	}
	if diBack != di {
		t.Errorf("DatasetIngested round-trip mismatch: got %+v, want %+v", diBack, di)
	}

	// EntitySource round-trip.
	es := bestiary.EntitySource{
		EntityKey: "llama@3.3#70b{instruct}",
		SourceID:  bestiary.DataSourceOllama,
	}
	esEnc, err := json.Marshal(es)
	if err != nil {
		t.Fatalf("json.Marshal(EntitySource) failed: %v", err)
	}
	var esBack bestiary.EntitySource
	if err := json.Unmarshal(esEnc, &esBack); err != nil {
		t.Fatalf("json.Unmarshal(EntitySource) failed: %v", err)
	}
	if esBack != es {
		t.Errorf("EntitySource round-trip mismatch: got %+v, want %+v", esBack, es)
	}

	// Entity.Sources projection: an Entity carrying a sorted []DataSourceID
	// projection round-trips and the Sources field serializes as a JSON array of
	// source-id strings.
	ent := bestiary.Entity{
		Ref:     bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
		Sources: []bestiary.DataSourceID{bestiary.DataSourceModelsDev, bestiary.DataSourceOllama},
	}
	entEnc, err := json.Marshal(ent)
	if err != nil {
		t.Fatalf("json.Marshal(Entity) failed: %v", err)
	}
	var entMap map[string]any
	if err := json.Unmarshal(entEnc, &entMap); err != nil {
		t.Fatalf("could not unmarshal Entity JSON: %v", err)
	}
	srcArr, ok := entMap["Sources"].([]any)
	if !ok {
		t.Fatalf("Entity.Sources serialized as %T, want a JSON array", entMap["Sources"])
	}
	if len(srcArr) != 2 || srcArr[0] != "models.dev" || srcArr[1] != "ollama" {
		t.Errorf("Entity.Sources = %v, want [models.dev ollama] (sorted projection)", srcArr)
	}
}

// TestJSONOutput_NegativeConformance verifies that a synthesized JSON object
// that violates the bestiary.schema.json specification is detectable — i.e.,
// the schema does NOT accept a wrong type for Date.
//
// This test does not invoke a live JSON Schema validator library (no external
// deps); instead it directly asserts the detection logic — a Date field
// containing an integer would be rejected by type: string in the schema.
// The test constructs such an invalid object and verifies it cannot be parsed
// into a ModelInfo via a strict decoder that mirrors schema validation intent.
func TestJSONOutput_NegativeConformance(t *testing.T) {
	// Construct a JSON object with Date as integer (schema violation).
	// The real schema says Date must be type: string.
	invalidJSON := `{
		"ID": "bad-model",
		"Provider": "test",
		"DisplayName": "Bad Model",
		"RawFamily": "test",
		"Family": "test",
		"Variant": "",
		"Date": 20240101,
		"ContextWindow": 1000,
		"MaxOutput": 100,
		"Reasoning": false,
		"ToolCall": false,
		"Attachment": false,
		"Temperature": false,
		"StructuredOutput": false,
		"Interleaved": {"Supported": false, "Config": null},
		"OpenWeights": false,
		"CostInputPerMTok": null,
		"CostOutputPerMTok": null,
		"CostReasoningPerMTok": null,
		"CostCacheReadPerMTok": null,
		"CostCacheWritePerMTok": null,
		"ReleaseDate": "2024-01-01",
		"Knowledge": "2024-01",
		"Modalities": {"Input": ["text"], "Output": ["text"]},
		"LastSynced": "2024-01-01T00:00:00Z"
	}`

	// Strict JSON decode into ModelInfo: Date is a string field in Go.
	// json.Decoder with DisallowUnknownFields will fail on type mismatch.
	var m bestiary.ModelInfo
	dec := json.NewDecoder(bytes.NewBufferString(invalidJSON))
	dec.DisallowUnknownFields()
	err := dec.Decode(&m)
	if err == nil {
		t.Errorf(
			"expected decode error for Date=integer, got nil;\n" +
				"  what went wrong: a JSON integer was accepted where a string is required\n" +
				"  why: the schema declares Date as type: string\n" +
				"  where: schema_test.go TestJSONOutput_NegativeConformance\n" +
				"  how to fix: ModelInfo.Date must be typed as string in Go so " +
				"JSON decode rejects non-string values",
		)
	}
}
