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
	for prop := range modelRefDef.Properties {
		if _, ok := refOut[prop]; !ok {
			t.Errorf("ModelRef JSON output missing schema $defs.ModelRef property %q", prop)
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

		// Designations() from a ModelRef always uses AcceptabilityAdmitted in this epoch.
		// Verify Designations() returns 4 entries and all have rating "admitted".
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
			if len(designations) != 4 {
				t.Errorf(
					"ModelRef.Designations() returned %d designations, want 4;\n"+
						"  what: expected Raw, Canonical, HuggingFace, and PURL designations\n"+
						"  where: schema_test.go TestDesignation_AllAcceptabilityRatings",
					len(designations),
				)
			}
			for i, dg := range designations {
				if dg.Rating != bestiary.AcceptabilityAdmitted {
					t.Errorf(
						"Designation[%d].Rating = %v, want AcceptabilityAdmitted;\n"+
							"  what: all epoch-generated designations must default to admitted\n"+
							"  why: promotion to preferred is deferred\n"+
							"  where: schema_test.go TestDesignation_AllAcceptabilityRatings",
						i, dg.Rating,
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
