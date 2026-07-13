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

// TestSchemaDefs_V025_DeepConformance mirrors TestSchemaDefs_V024_DeepConformance
// for the v0.2.5 additive struct $defs (ModelLink, BenchmarkResult,
// ReasoningOption, TierCost, CostTier, EntityMetadata). For each it marshals a
// populated production value and asserts:
//   - the schema $def declares EXACTLY the expected property set (a dropped or
//     renamed property fails; an undocumented extra property fails), and
//   - every declared property is present in the marshaled output with a JSON
//     kind matching the pinned schema "type".
//
// It also pins the load-bearing v0.2.5 wire-fidelity invariants:
//   - CostTier flattens the embedded TierCost cost fields to the tier level
//     (the schema $def lists ContextSize plus all seven cost props inline), and
//   - ModelLink.Type / ReasoningOption.Kind serialize as members of their
//     respective enum $defs (the $ref wiring, enforced behaviorally).
//
// oneOf cost properties (the nullable *float64 fields) carry no plain "type"
// node and are omitted from expectTypes (explicit allowlist), exactly as the
// V024 check allowlists the ProviderInstance cost fields. canonSchemaType is
// shared with schema_test.go (same test package).
func TestSchemaDefs_V025_DeepConformance(t *testing.T) {
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

	// LOCKED: Catalog is a parser return container, NOT a serialized output
	// document, so it must NOT appear as a schema $def (ratified amendment). A
	// Catalog $def creeping in would fail here.
	if _, ok := schemaDefs.Defs["Catalog"]; ok {
		t.Errorf("bestiary.schema.json declares a $defs.Catalog; it MUST NOT — Catalog is a parser return container, not a serialized output document. Remove it.")
	}

	// LOCKED: EntityMetadata must carry NO "status"/"Status" property. Status is
	// an api.json / instance-level fact and lives on ModelInfo only; a status
	// reappearing on EntityMetadata would re-introduce the sourcing defect.
	if emDef, ok := schemaDefs.Defs["EntityMetadata"]; ok {
		for prop := range emDef.Properties {
			if strings.EqualFold(prop, "status") {
				t.Errorf("bestiary.schema.json $defs.EntityMetadata declares a %q property; it MUST NOT — status is an api.json / instance-level fact carried on ModelInfo only.", prop)
			}
		}
	}

	linkTypeDef, ok := schemaDefs.Defs["LinkType"]
	if !ok || len(linkTypeDef.Enum) == 0 {
		t.Fatalf("bestiary.schema.json $defs.LinkType missing or has no enum")
	}
	rokDef, ok := schemaDefs.Defs["ReasoningOptionKind"]
	if !ok || len(rokDef.Enum) == 0 {
		t.Fatalf("bestiary.schema.json $defs.ReasoningOptionKind missing or has no enum")
	}

	type deepCheck struct {
		defName     string
		value       any
		expectProps []string
		expectTypes map[string]string
	}
	score := 85.5
	cost := 2.0
	checks := []deepCheck{
		{
			defName: "ModelLink",
			value: bestiary.ModelLink{
				Label: "Model card", URL: "https://example.com/card", Type: bestiary.LinkWeights,
			},
			expectProps: []string{"Label", "URL", "Type", "TypeRaw"},
			expectTypes: map[string]string{
				"Label":   "string",
				"URL":     "string",
				"Type":    "$ref",
				"TypeRaw": "string",
			},
		},
		{
			defName: "BenchmarkResult",
			value: bestiary.BenchmarkResult{
				Name: "MMLU", Version: "1.0", Variant: "5-shot", Dataset: "test",
				Harness: "lm-eval", Metric: "accuracy", Score: score, ScoreRaw: "",
				SourceURL: "https://example.com/blog", Date: "2026-01-01",
			},
			expectProps: []string{"Name", "Version", "Variant", "Dataset", "Harness", "Metric", "Score", "ScoreRaw", "SourceURL", "Date"},
			expectTypes: map[string]string{
				"Name":      "string",
				"Version":   "string",
				"Variant":   "string",
				"Dataset":   "string",
				"Harness":   "string",
				"Metric":    "string",
				"Score":     "number",
				"ScoreRaw":  "string",
				"SourceURL": "string",
				"Date":      "string",
			},
		},
		{
			defName: "ReasoningOption",
			value: bestiary.ReasoningOption{
				Kind: bestiary.ReasoningEffort, KindRaw: "", Values: []string{"low", "high"},
				MinTokens: 0, MaxTokens: 0,
			},
			expectProps: []string{"Kind", "KindRaw", "Values", "MinTokens", "MaxTokens"},
			expectTypes: map[string]string{
				"Kind":      "$ref",
				"KindRaw":   "string",
				"Values":    "array|null",
				"MinTokens": "integer",
				"MaxTokens": "integer",
			},
		},
		{
			defName: "TierCost",
			value: bestiary.TierCost{
				CostInputPerMTok: &cost,
			},
			expectProps: []string{
				"CostInputPerMTok", "CostOutputPerMTok", "CostReasoningPerMTok",
				"CostCacheReadPerMTok", "CostCacheWritePerMTok",
				"CostInputAudioPerMTok", "CostOutputAudioPerMTok",
			},
			// All seven props are oneOf{number,null} — no plain "type" node (allowlisted).
			expectTypes: map[string]string{},
		},
		{
			defName: "CostTier",
			value: bestiary.CostTier{
				ContextSize: 200000,
				TierCost:    bestiary.TierCost{CostOutputPerMTok: &cost},
			},
			expectProps: []string{
				"ContextSize",
				"CostInputPerMTok", "CostOutputPerMTok", "CostReasoningPerMTok",
				"CostCacheReadPerMTok", "CostCacheWritePerMTok",
				"CostInputAudioPerMTok", "CostOutputAudioPerMTok",
			},
			expectTypes: map[string]string{
				"ContextSize": "integer",
				// The seven flattened TierCost props are oneOf{number,null} (allowlisted).
			},
		},
		{
			defName: "EntityMetadata",
			value: bestiary.EntityMetadata{
				MetadataID:  "zhipuai/glm-4.6",
				Name:        "GLM 4.6",
				Description: "A capable model.",
				License:     "MIT",
				Links:       []bestiary.ModelLink{{Label: "Card", URL: "https://example.com", Type: bestiary.LinkModelCard}},
				Benchmarks:  []bestiary.BenchmarkResult{{Name: "MMLU", Metric: "accuracy", Score: score}},
				Source:      bestiary.DataSourceModelsDev,
				LastSynced:  "2026-07-12T00:00:00Z",
			},
			expectProps: []string{"MetadataID", "Name", "Description", "License", "Links", "Benchmarks", "Source", "LastSynced"},
			expectTypes: map[string]string{
				"MetadataID":  "string",
				"Name":        "string",
				"Description": "string",
				"License":     "string",
				"Links":       "array|null",
				"Benchmarks":  "array|null",
				"Source":      "string",
				"LastSynced":  "string",
			},
		},
	}

	for _, c := range checks {
		def, ok := schemaDefs.Defs[c.defName]
		if !ok || len(def.Properties) == 0 {
			t.Errorf("bestiary.schema.json $defs.%s missing or has no properties", c.defName)
			continue
		}
		// (a) EXACT prop set: every expected prop present, no undocumented extra.
		for _, want := range c.expectProps {
			if _, ok := def.Properties[want]; !ok {
				t.Errorf("bestiary.schema.json $defs.%s is missing the %q property;\n"+
					"  how to fix: add a %q property to $defs.%s (it is part of the v0.2.5 contract)",
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
		var out map[string]any
		dec := json.NewDecoder(bytes.NewReader(enc))
		dec.UseNumber()
		if err := dec.Decode(&out); err != nil {
			t.Errorf("could not unmarshal %s JSON: %v", c.defName, err)
			continue
		}
		for prop := range def.Properties {
			if _, ok := out[prop]; !ok {
				t.Errorf("%s JSON output is missing schema $defs.%s property %q;\n"+
					"  how to fix: ensure bestiary.%s has an exported field %q matching the schema $def",
					c.defName, c.defName, prop, c.defName, prop)
			}
		}
		// (b) Pin the declared schema "type" of every checked property, and
		// cross-check the marshaled value's JSON kind.
		for prop, want := range c.expectTypes {
			raw, ok := def.Properties[prop]
			if !ok {
				continue // a missing property is already reported above
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
					"  how to fix: set the \"type\" of $defs.%s.%s back to %q in bestiary.schema.json",
					c.defName, prop, got, want, c.defName, prop, want)
			}
			v, present := out[prop]
			if !present || v == nil {
				continue
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
		// Behavioral $ref checks: the enum-typed properties must serialize as a
		// member of the referenced enum $def.
		switch c.defName {
		case "ModelLink":
			if lv, ok := out["Type"].(string); ok {
				if !slices.Contains(linkTypeDef.Enum, lv) {
					t.Errorf("ModelLink.Type=%q is not a member of schema $defs.LinkType.enum %v", lv, linkTypeDef.Enum)
				}
			} else {
				t.Errorf("ModelLink.Type serialized as %T, want a JSON string (LinkType enum)", out["Type"])
			}
		case "ReasoningOption":
			if kv, ok := out["Kind"].(string); ok {
				if !slices.Contains(rokDef.Enum, kv) {
					t.Errorf("ReasoningOption.Kind=%q is not a member of schema $defs.ReasoningOptionKind.enum %v", kv, rokDef.Enum)
				}
			} else {
				t.Errorf("ReasoningOption.Kind serialized as %T, want a JSON string (ReasoningOptionKind enum)", out["Kind"])
			}
		}
	}
}

// TestSchemaDefs_V025Enums_EnumExactSet pins each new closed int enum's schema
// $def enum to be EXACTLY the set of canonical wire names produced by marshaling
// every member of the Go enum, in both directions (a dropped or phantom member
// fails). It mirrors TestSchemaDefs_Quantization_EnumExactSet and self-extends as
// the Go enums grow. It also asserts the marshaled zero value: ModelStatus is
// None-at-zero ("none"), while LinkType and ReasoningOptionKind are Other-at-zero
// ("other") — the deliberate scalar-vs-element convention divergence.
func TestSchemaDefs_V025Enums_EnumExactSet(t *testing.T) {
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

	marshal := func(m interface{ MarshalText() ([]byte, error) }) string {
		b, err := m.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() failed: %v", err)
		}
		return string(b)
	}

	// Build the authoritative Go wire-name set for each enum by iterating its
	// closed range. The first element is the zero value's wire name.
	var modelStatusNames []string
	for s := bestiary.StatusNone; s <= bestiary.StatusOther; s++ {
		modelStatusNames = append(modelStatusNames, marshal(s))
	}
	var linkTypeNames []string
	for l := bestiary.LinkOther; l <= bestiary.LinkWeights; l++ {
		linkTypeNames = append(linkTypeNames, marshal(l))
	}
	var reasoningKindNames []string
	for k := bestiary.ReasoningOptionOther; k <= bestiary.ReasoningBudgetTokens; k++ {
		reasoningKindNames = append(reasoningKindNames, marshal(k))
	}

	cases := []struct {
		defName      string
		goNames      []string
		wantZeroWire string
	}{
		{"ModelStatus", modelStatusNames, "none"},
		{"LinkType", linkTypeNames, "other"},
		{"ReasoningOptionKind", reasoningKindNames, "other"},
	}
	for _, tc := range cases {
		def, ok := schemaDefs.Defs[tc.defName]
		if !ok || len(def.Enum) == 0 {
			t.Errorf("bestiary.schema.json $defs.%s missing or has no enum", tc.defName)
			continue
		}
		// Zero-value convention: the FIRST Go wire name is the zero value's name.
		if tc.goNames[0] != tc.wantZeroWire {
			t.Errorf("%s zero value marshals as %q, want %q (zero-value convention)", tc.defName, tc.goNames[0], tc.wantZeroWire)
		}
		// Direction 1: every Go wire name must appear in the schema enum.
		for _, name := range tc.goNames {
			if !slices.Contains(def.Enum, name) {
				t.Errorf("schema $defs.%s.enum is MISSING Go wire name %q;\n"+
					"  how to fix: add %q to $defs.%s.enum in bestiary.schema.json",
					tc.defName, name, name, tc.defName)
			}
		}
		// Direction 2: every schema enum member must be produced by some Go value.
		for _, member := range def.Enum {
			if !slices.Contains(tc.goNames, member) {
				t.Errorf("schema $defs.%s.enum contains PHANTOM member %q with no Go counterpart;\n"+
					"  how to fix: remove %q from $defs.%s.enum, or add a matching Go constant",
					tc.defName, member, member, tc.defName)
			}
		}
		// Exact set: equal cardinality after both subset checks ⇒ set equality
		// (also catches a duplicated schema member).
		if len(def.Enum) != len(tc.goNames) {
			t.Errorf("schema $defs.%s.enum has %d members, the Go enum has %d;\n"+
				"  the schema enum must be EXACTLY the MarshalText set over the closed Go enum",
				tc.defName, len(def.Enum), len(tc.goNames))
		}
	}
}

// TestModelStatus_ParseAndBehavior exercises the exported ModelStatus surface:
// the None-at-zero scalar convention, IsKnown over the valid range, round-trip
// through MarshalText/UnmarshalText, StatusOther's "other" wire name, and the
// ParseModelStatus CLI path — empty is StatusNone, known tokens parse
// case-insensitively, and an unknown token (or the internal "other" sentinel)
// yields an actionable error naming the input.
func TestModelStatus_ParseAndBehavior(t *testing.T) {
	// None-at-zero: the zero value is StatusNone.
	var zero bestiary.ModelStatus
	if zero != bestiary.StatusNone {
		t.Errorf("zero-value ModelStatus = %v, want StatusNone (None-at-zero scalar convention)", zero)
	}

	// IsKnown over the valid range; an out-of-range value is not known.
	for s := bestiary.StatusNone; s <= bestiary.StatusOther; s++ {
		if !s.IsKnown() {
			t.Errorf("ModelStatus(%d).IsKnown() = false, want true", int(s))
		}
	}
	if bestiary.ModelStatus(999).IsKnown() {
		t.Errorf("ModelStatus(999).IsKnown() = true, want false (out of range)")
	}

	// Round-trip every member through MarshalText/UnmarshalText.
	for s := bestiary.StatusNone; s <= bestiary.StatusOther; s++ {
		b, err := s.MarshalText()
		if err != nil {
			t.Fatalf("ModelStatus(%d).MarshalText() error: %v", int(s), err)
		}
		var back bestiary.ModelStatus
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("ModelStatus.UnmarshalText(%q) error: %v", b, err)
		}
		if back != s {
			t.Errorf("ModelStatus round-trip: %q -> %v, want %v", b, back, s)
		}
	}
	if got := bestiary.StatusOther.String(); got != "other" {
		t.Errorf("StatusOther.String() = %q, want \"other\"", got)
	}

	// ParseModelStatus CLI path.
	parseOK := map[string]bestiary.ModelStatus{
		"":           bestiary.StatusNone,
		"alpha":      bestiary.StatusAlpha,
		"BETA":       bestiary.StatusBeta, // case-insensitive
		"deprecated": bestiary.StatusDeprecated,
	}
	for in, want := range parseOK {
		got, err := bestiary.ParseModelStatus(in)
		if err != nil {
			t.Errorf("ParseModelStatus(%q) error: %v, want %v", in, err, want)
			continue
		}
		if got != want {
			t.Errorf("ParseModelStatus(%q) = %v, want %v", in, got, want)
		}
	}
	// Unknown token and the internal "other" sentinel both error, naming the input.
	for _, bad := range []string{"bogus", "other", "ga"} {
		got, err := bestiary.ParseModelStatus(bad)
		if err == nil {
			t.Errorf("ParseModelStatus(%q) returned nil error (got %v), want an actionable error", bad, got)
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("ParseModelStatus(%q) error %q does not name the bad input", bad, err.Error())
		}
	}
}

// TestElementEnums_OtherAtZero_RoundTrip pins the Other-at-zero convention for
// the element enums LinkType and ReasoningOptionKind: the zero value marshals to
// "other", IsKnown holds over the valid range, and every member round-trips
// through MarshalText/UnmarshalText.
func TestElementEnums_OtherAtZero_RoundTrip(t *testing.T) {
	var zeroLink bestiary.LinkType
	if zeroLink != bestiary.LinkOther {
		t.Errorf("zero-value LinkType = %v, want LinkOther (Other-at-zero element convention)", zeroLink)
	}
	var zeroKind bestiary.ReasoningOptionKind
	if zeroKind != bestiary.ReasoningOptionOther {
		t.Errorf("zero-value ReasoningOptionKind = %v, want ReasoningOptionOther (Other-at-zero element convention)", zeroKind)
	}

	for l := bestiary.LinkOther; l <= bestiary.LinkWeights; l++ {
		if !l.IsKnown() {
			t.Errorf("LinkType(%d).IsKnown() = false, want true", int(l))
		}
		b, err := l.MarshalText()
		if err != nil {
			t.Fatalf("LinkType(%d).MarshalText() error: %v", int(l), err)
		}
		var back bestiary.LinkType
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("LinkType.UnmarshalText(%q) error: %v", b, err)
		}
		if back != l {
			t.Errorf("LinkType round-trip: %q -> %v, want %v", b, back, l)
		}
	}
	if got := bestiary.LinkOther.String(); got != "other" {
		t.Errorf("LinkOther.String() = %q, want \"other\"", got)
	}

	for k := bestiary.ReasoningOptionOther; k <= bestiary.ReasoningBudgetTokens; k++ {
		if !k.IsKnown() {
			t.Errorf("ReasoningOptionKind(%d).IsKnown() = false, want true", int(k))
		}
		b, err := k.MarshalText()
		if err != nil {
			t.Fatalf("ReasoningOptionKind(%d).MarshalText() error: %v", int(k), err)
		}
		var back bestiary.ReasoningOptionKind
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("ReasoningOptionKind.UnmarshalText(%q) error: %v", b, err)
		}
		if back != k {
			t.Errorf("ReasoningOptionKind round-trip: %q -> %v, want %v", b, back, k)
		}
	}
	if got := bestiary.ReasoningOptionOther.String(); got != "other" {
		t.Errorf("ReasoningOptionOther.String() = %q, want \"other\"", got)
	}
}

// TestSchemaDefs_ModelRefParamSize pins the additive ModelRef.ParamSize property
// (added in schema 0.3.0 alongside the #size identity work — ModelRef gained a
// ParamSize field so Resolve()'s ambiguity grouping over []ModelRef keeps sized
// siblings distinct). $defs.ModelRef must declare it as an optional string that
// is NEVER in required, and a marshaled ModelRef carrying a ParamSize must
// serialize it as that string.
func TestSchemaDefs_ModelRefParamSize(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}
	var schemaDefs struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}
	mr, ok := schemaDefs.Defs["ModelRef"]
	if !ok {
		t.Fatalf("bestiary.schema.json $defs.ModelRef missing")
	}
	// Declared as a string property.
	raw, ok := mr.Properties["ParamSize"]
	if !ok {
		t.Fatalf("bestiary.schema.json $defs.ModelRef is missing the \"ParamSize\" property;\n" +
			"  how to fix: add a \"ParamSize\" string property to $defs.ModelRef (added in schema 0.3.0)")
	}
	var node struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatalf("could not decode $defs.ModelRef.ParamSize: %v", err)
	}
	if got, ok := canonSchemaType(node.Type); !ok || got != "string" {
		t.Errorf("$defs.ModelRef.ParamSize declares type %q, want \"string\"", got)
	}
	// Additive: must NOT be required, so pre-0.3.0 ModelRef documents still validate.
	if slices.Contains(mr.Required, "ParamSize") {
		t.Errorf("$defs.ModelRef required[] contains \"ParamSize\"; it must stay OPTIONAL for backward compatibility")
	}
	// A marshaled ModelRef carrying a ParamSize serializes it as that string.
	ref := bestiary.ModelRef{ID: "llama-3.3-70b", ParamSize: "70b"}
	enc, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ModelRef) failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(enc, &out); err != nil {
		t.Fatalf("could not unmarshal ModelRef JSON: %v", err)
	}
	if v, ok := out["ParamSize"].(string); !ok || v != "70b" {
		t.Errorf("ModelRef.ParamSize serialized as %v (%T), want string \"70b\"", out["ParamSize"], out["ParamSize"])
	}
}

// TestSchema_BackwardCompat_V025Fields pins the additive invariant for the
// v0.2.5 fields: the new ModelInfo properties and the new Entity.Metadata
// property must NOT appear in any required[] array, so a 0.2.x-shaped record
// lacking them still validates.
func TestSchema_BackwardCompat_V025Fields(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}
	var schema struct {
		Required []string `json:"required"`
		Defs     map[string]struct {
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("could not unmarshal bestiary.schema.json: %v", err)
	}
	newModelInfoProps := []string{
		"Description", "Status", "StatusRaw", "ReasoningOptions",
		"CostInputAudioPerMTok", "CostOutputAudioPerMTok",
		"CostContextOver200k", "CostTiers",
	}
	for _, f := range newModelInfoProps {
		if slices.Contains(schema.Required, f) {
			t.Errorf("ModelInfo schema required[] contains %q; additive v0.2.5 fields must stay OPTIONAL for backward compatibility", f)
		}
	}
	if ent, ok := schema.Defs["Entity"]; ok {
		if slices.Contains(ent.Required, "Metadata") {
			t.Errorf("$defs.Entity required[] contains \"Metadata\"; it must stay OPTIONAL for backward compatibility")
		}
	}
}
