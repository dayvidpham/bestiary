package bestiary_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModelInfo_QuantVRAM_JSONWireNames pins the exact JSON key names produced
// when marshaling a ModelInfo with populated QuantVRAM and Source fields.
// ModelInfo has no json struct tags, so fields marshal under their Go names.
// A rename, a json:"-" tag, or a dropped field on any QuantVRAM member must
// cause this test to fail.
//
// This test also verifies VRAMEstimatePartial=true serializes as boolean true
// (not false or absent) so a json:"-" mutant or zero-default is caught.
func TestModelInfo_QuantVRAM_JSONWireNames(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:       "test-wire",
		Provider: "testprovider",
		Source:   bestiary.DataSourceOllama,
		QuantVRAM: []bestiary.QuantVRAM{
			{
				Quant:               bestiary.QuantQ4_K_M,
				QuantRaw:            "Q4_K_M",
				WeightsBytes:        42_500_000_000,
				VRAMBytes:           46_000_000_000,
				VRAMContextTokens:   131072,
				Layers:              80,
				KVHeads:             8,
				HeadDim:             128,
				VRAMEstimatePartial: true,
			},
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal(ModelInfo) failed: %v", err)
	}
	raw := string(data)

	// Top-level ModelInfo wire keys must be present.
	for _, key := range []string{`"QuantVRAM"`, `"Source"`} {
		if !strings.Contains(raw, key) {
			t.Errorf("marshaled ModelInfo missing wire key %s;\n  JSON: %s", key, raw)
		}
	}

	// Source value must be the exact string "ollama".
	if !strings.Contains(raw, `"Source":"ollama"`) {
		t.Errorf("Source wire value is not \"ollama\";\n  JSON: %s", raw)
	}

	// QuantVRAM must be a JSON array.
	if !strings.Contains(raw, `"QuantVRAM":[`) {
		t.Errorf("QuantVRAM wire value is not a JSON array;\n  JSON: %s", raw)
	}

	// Every QuantVRAM field name must appear as a JSON key.
	for _, field := range []string{
		`"Quant"`,
		`"QuantRaw"`,
		`"WeightsBytes"`,
		`"VRAMBytes"`,
		`"VRAMContextTokens"`,
		`"Layers"`,
		`"KVHeads"`,
		`"HeadDim"`,
		`"VRAMEstimatePartial"`,
	} {
		if !strings.Contains(raw, field) {
			t.Errorf("QuantVRAM JSON missing field %s;\n  JSON: %s", field, raw)
		}
	}

	// VRAMEstimatePartial must be literal true (not false or missing).
	if !strings.Contains(raw, `"VRAMEstimatePartial":true`) {
		t.Errorf("VRAMEstimatePartial not serialized as true;\n  JSON: %s", raw)
	}

	// Quant must serialize as a lowercase string via TextMarshaler, not as an integer.
	if !strings.Contains(raw, `"Quant":"q4_k_m"`) {
		t.Errorf("Quant not serialized as string \"q4_k_m\";\n  JSON: %s", raw)
	}
}

// TestModelInfo_QuantVRAM_JSONRoundTrip verifies that a ModelInfo with a
// populated QuantVRAM slice marshal/unmarshal round-trips all field values
// correctly through JSON.
func TestModelInfo_QuantVRAM_JSONRoundTrip(t *testing.T) {
	m := bestiary.ModelInfo{
		ID:       "llama3.3:70b-instruct-q4_k_m",
		Provider: "ollama",
		Source:   bestiary.DataSourceOllama,
		QuantVRAM: []bestiary.QuantVRAM{
			{
				Quant:               bestiary.QuantQ4_K_M,
				QuantRaw:            "Q4_K_M",
				WeightsBytes:        42_500_000_000,
				VRAMBytes:           45_000_000_000,
				VRAMContextTokens:   131072,
				Layers:              80,
				KVHeads:             8,
				HeadDim:             128,
				VRAMEstimatePartial: false,
			},
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("json.Marshal(ModelInfo with QuantVRAM) failed: %v", err)
	}

	var got bestiary.ModelInfo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal into ModelInfo failed: %v", err)
	}

	if got.Source != bestiary.DataSourceOllama {
		t.Errorf("Source after round-trip = %q, want %q", got.Source, bestiary.DataSourceOllama)
	}
	if len(got.QuantVRAM) != 1 {
		t.Fatalf("QuantVRAM len after round-trip = %d, want 1", len(got.QuantVRAM))
	}

	row := got.QuantVRAM[0]
	if row.Quant != bestiary.QuantQ4_K_M {
		t.Errorf("QuantVRAM[0].Quant = %v, want QuantQ4_K_M", row.Quant)
	}
	if row.QuantRaw != "Q4_K_M" {
		t.Errorf("QuantVRAM[0].QuantRaw = %q, want %q", row.QuantRaw, "Q4_K_M")
	}
	if row.WeightsBytes != 42_500_000_000 {
		t.Errorf("QuantVRAM[0].WeightsBytes = %d, want 42500000000", row.WeightsBytes)
	}
	if row.VRAMBytes != 45_000_000_000 {
		t.Errorf("QuantVRAM[0].VRAMBytes = %d, want 45000000000", row.VRAMBytes)
	}
	if row.VRAMContextTokens != 131072 {
		t.Errorf("QuantVRAM[0].VRAMContextTokens = %d, want 131072", row.VRAMContextTokens)
	}
	if row.Layers != 80 {
		t.Errorf("QuantVRAM[0].Layers = %d, want 80", row.Layers)
	}
	if row.KVHeads != 8 {
		t.Errorf("QuantVRAM[0].KVHeads = %d, want 8", row.KVHeads)
	}
	if row.HeadDim != 128 {
		t.Errorf("QuantVRAM[0].HeadDim = %d, want 128", row.HeadDim)
	}
	if row.VRAMEstimatePartial {
		t.Errorf("QuantVRAM[0].VRAMEstimatePartial = true, want false")
	}
}

// TestCloneEntity_SourcesRegistryPath verifies that Entities() returns entities
// with a populated, sorted, clone-isolated Sources projection. Every entity in the
// static registry originates from the models.dev pipeline, so its projection
// contains at least DataSourceModelsDev (the registry attestation rule); the
// returned slice is an independent copy that callers may mutate without corrupting
// the registry.
func TestCloneEntity_SourcesRegistryPath(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("static registry is empty; cannot test clone isolation")
	}
	for _, e := range entities {
		if len(e.Sources) == 0 {
			t.Errorf("entity %q: Sources is empty, want at least the models.dev origin", e.Ref.String())
			continue
		}
		var hasModelsDev bool
		for _, s := range e.Sources {
			if s == bestiary.DataSourceModelsDev {
				hasModelsDev = true
			}
		}
		if !hasModelsDev {
			t.Errorf("entity %q: Sources = %v, want it to include models.dev", e.Ref.String(), e.Sources)
		}
	}

	// Clone-isolation probe, ONCE (not per-entity). Mutating a returned projection
	// must not corrupt the registry. Re-fetch and compare the SAME index the mutation
	// targeted (got[0]) — the previous per-entity form mutated e.Sources[0] for every
	// entity but only ever inspected got[0], so a leak from entity i>0 (visible at
	// got[i]) could never fire, and it re-cloned the whole registry O(n) times.
	if len(entities[0].Sources) > 0 {
		entities[0].Sources[0] = "mutated-by-test"
		if got := bestiary.Entities(); got[0].Sources[0] == "mutated-by-test" {
			t.Fatal("Entities() Sources slice is not clone-isolated: a caller mutation leaked into the registry")
		}
	}
}

// TestCloneInstances_QuantVRAMRegistryPath verifies the entity aggregate copies the
// per-row QuantVRAM and Source carriers onto each ProviderInstance, and that the
// returned instances are clone-isolated from the registry. The only curated source
// is ollama: a curated row carries its quant rows and Source ollama, while an
// uncurated row carries no quant rows and the zero (empty) Source — both directions
// are checked, registry-wide and on the curated llama-3.3-70b entity specifically.
func TestCloneInstances_QuantVRAMRegistryPath(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("static registry is empty; cannot test the instance carriers")
	}

	// Registry-wide both-directions invariant: an instance carries QuantVRAM rows
	// only if it is a curated row, and every curated row's Source is ollama; an
	// uncurated instance carries no quant rows and the zero Source.
	var populated, uncurated int
	for _, e := range entities {
		for j, inst := range e.Instances {
			if len(inst.QuantVRAM) > 0 {
				populated++
				if inst.Source != bestiary.DataSourceOllama {
					t.Errorf("entity %q instance[%d] (id %q) carries %d quant rows but Source = %q, want ollama",
						e.Ref.String(), j, inst.ID, len(inst.QuantVRAM), inst.Source)
				}
			} else if inst.Source == bestiary.DataSourceNone {
				uncurated++
			}
		}
	}
	if populated == 0 {
		t.Fatal("no ProviderInstance carries QuantVRAM: the aggregate does not copy ModelInfo.QuantVRAM onto instances")
	}
	if uncurated == 0 {
		t.Fatal("no uncurated (nil-quant, empty-source) instance found; expected the uncurated direction to exist")
	}

	// Targeted: the curated llama-3.3-70b entity's ollama instances carry exactly the
	// 3 curated quant rows (q4_k_m, q8_0, f16), in curated order.
	e, ok := bestiary.EntityByTuple("llama", "", "3.3", "70b", "instruct")
	if !ok {
		t.Fatal("curated entity llama@3.3#70b{instruct} not found in registry")
	}
	wantQuants := []bestiary.Quantization{bestiary.QuantQ4_K_M, bestiary.QuantQ8_0, bestiary.QuantF16}
	var sawOllama70b bool
	for j, inst := range e.Instances {
		if inst.Source != bestiary.DataSourceOllama {
			continue
		}
		sawOllama70b = true
		if len(inst.QuantVRAM) != len(wantQuants) {
			t.Errorf("70b instance[%d] (id %q): QuantVRAM has %d rows, want %d",
				j, inst.ID, len(inst.QuantVRAM), len(wantQuants))
			continue
		}
		for k, want := range wantQuants {
			if inst.QuantVRAM[k].Quant != want {
				t.Errorf("70b instance[%d].QuantVRAM[%d].Quant = %v, want %v", j, k, inst.QuantVRAM[k].Quant, want)
			}
		}
	}
	if !sawOllama70b {
		t.Error("curated 70b entity has no ollama-sourced instance: Source carrier not copied onto instances?")
	}

	// Clone-isolation through the registry path: mutating a returned instance's quant
	// row must not leak into the registry's cached copy.
	for j := range e.Instances {
		if len(e.Instances[j].QuantVRAM) > 0 {
			e.Instances[j].QuantVRAM[0].WeightsBytes = -1
			break
		}
	}
	refetched, _ := bestiary.EntityByTuple("llama", "", "3.3", "70b", "instruct")
	for k := range refetched.Instances {
		for _, row := range refetched.Instances[k].QuantVRAM {
			if row.WeightsBytes == -1 {
				t.Fatal("ProviderInstance.QuantVRAM is not clone-isolated: a caller mutation leaked into the registry")
			}
		}
	}
}

// TestProviderInstance_QuantVRAM_BuildSiteCloneIsolation falsifies the BUILD-SITE
// clone in the registry aggregate (registry.go loadEntityIndex:
// `QuantVRAM: cloneQuantVRAM(m.QuantVRAM)`). The lookup-path clone (cloneInstances on
// the way OUT of Entities/EntityByTuple) hides whether the CACHED instance aliases
// staticModels' backing — so TestCloneInstances_QuantVRAMRegistryPath's probe (which
// mutates an already-insulated lookup result) cannot observe the build-site copy.
//
// This reaches the shared backing directly: StaticModels() returns SHALLOW ModelInfo
// copies, so a returned row's QuantVRAM slice header points at the SAME backing array
// the registry built its cached instances from. After forcing the index to build, a
// sentinel written through that backing must NOT surface on any registry instance —
// true only if the build site took its own copy. Under the aliasing mutant
// (`QuantVRAM: m.QuantVRAM`) the cached instance shares the backing, the sentinel
// leaks through the lookup-path clone, and this test fails. The global mutation is
// restored via defer (the package's parallel tests resume only after this serial
// test returns).
func TestProviderInstance_QuantVRAM_BuildSiteCloneIsolation(t *testing.T) {
	// Force the entity index (and its cached instances) to materialize BEFORE we
	// perturb the backing, so the build-site copy has already run.
	_ = bestiary.Entities()

	ms := bestiary.StaticModels()
	idx := -1
	for i := range ms {
		if len(ms[i].QuantVRAM) > 0 {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("no curated row with QuantVRAM in StaticModels(); cannot exercise the build-site clone")
	}

	const sentinel int64 = -987654321
	orig := ms[idx].QuantVRAM[0].WeightsBytes
	if orig == sentinel {
		t.Fatalf("sentinel %d collides with a real WeightsBytes value; pick another", sentinel)
	}
	ms[idx].QuantVRAM[0].WeightsBytes = sentinel                // writes the shared backing array
	defer func() { ms[idx].QuantVRAM[0].WeightsBytes = orig }() // restore global state

	for _, e := range bestiary.Entities() {
		for j, inst := range e.Instances {
			for k, row := range inst.QuantVRAM {
				if row.WeightsBytes == sentinel {
					t.Fatalf("entity %q instance[%d].QuantVRAM[%d] shows the sentinel: the registry's cached "+
						"instance aliases the staticModels QuantVRAM backing — the build-site clone is missing",
						e.Ref.String(), j, k)
				}
			}
		}
	}
}

// TestSchema_QuantVRAMSource_Conformance extends the schema conformance fixture
// with populated QuantVRAM and Source fields (following the v0.2.3 Host/Lineage
// precedent at schema_test.go). It performs a deep check of the $defs.QuantVRAM
// definition: every declared property must be present in a marshaled QuantVRAM
// row, Quant must serialize as a string, and VRAMEstimatePartial=true must
// marshal as boolean true.
func TestSchema_QuantVRAMSource_Conformance(t *testing.T) {
	schemaBytes, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("could not read bestiary.schema.json: %v", err)
	}

	// Parse $defs to find QuantVRAM definition.
	var schemaDefs struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(schemaBytes, &schemaDefs); err != nil {
		t.Fatalf("could not unmarshal $defs from bestiary.schema.json: %v", err)
	}

	quantVRAMDef, ok := schemaDefs.Defs["QuantVRAM"]
	if !ok || len(quantVRAMDef.Properties) == 0 {
		t.Fatalf("bestiary.schema.json $defs.QuantVRAM missing or has no properties;\n" +
			"  how to fix: add a QuantVRAM $def to bestiary.schema.json")
	}

	// Marshal a populated QuantVRAM row with VRAMEstimatePartial=true.
	row := bestiary.QuantVRAM{
		Quant:               bestiary.QuantQ4_K_M,
		QuantRaw:            "Q4_K_M",
		WeightsBytes:        42_500_000_000,
		VRAMBytes:           46_000_000_000,
		VRAMContextTokens:   131072,
		Layers:              80,
		KVHeads:             8,
		HeadDim:             128,
		VRAMEstimatePartial: true,
	}
	rowJSON, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal(QuantVRAM) failed: %v", err)
	}
	var rowOut map[string]any
	if err := json.Unmarshal(rowJSON, &rowOut); err != nil {
		t.Fatalf("could not unmarshal QuantVRAM JSON: %v", err)
	}

	// Every $defs.QuantVRAM property must be present in the marshaled output.
	for prop := range quantVRAMDef.Properties {
		if _, ok := rowOut[prop]; !ok {
			t.Errorf("QuantVRAM JSON missing $defs.QuantVRAM property %q;\n"+
				"  how to fix: ensure bestiary.QuantVRAM has exported field %q matching the schema", prop, prop)
		}
	}

	// Quant must serialize as a string (TextMarshaler), not an integer.
	if qv, ok := rowOut["Quant"]; ok {
		if _, isStr := qv.(string); !isStr {
			t.Errorf("QuantVRAM.Quant serialized as %T, want a JSON string;\n"+
				"  how to fix: Quantization.MarshalText must be implemented and the field must be exported", qv)
		}
	}

	// VRAMEstimatePartial must appear as boolean true.
	if ev, ok := rowOut["VRAMEstimatePartial"]; ok {
		bv, isBool := ev.(bool)
		if !isBool || !bv {
			t.Errorf("QuantVRAM.VRAMEstimatePartial serialized as %v (%T), want boolean true", ev, ev)
		}
	} else {
		t.Error("QuantVRAM JSON missing VRAMEstimatePartial key")
	}

	// Top-level ModelInfo schema: verify QuantVRAM and Source are declared.
	var topSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schemaBytes, &topSchema); err != nil {
		t.Fatalf("could not unmarshal top-level schema properties: %v", err)
	}
	for _, prop := range []string{"QuantVRAM", "Source"} {
		if _, ok := topSchema.Properties[prop]; !ok {
			t.Errorf("bestiary.schema.json top-level properties missing %q;\n"+
				"  how to fix: add %q to the ModelInfo properties in bestiary.schema.json", prop, prop)
		}
	}

	// FormatModel with populated QuantVRAM+Source produces conformant JSON.
	// This mirrors the v0.2.3 Host/Lineage fixture in schema_test.go:96-105.
	cost := 1.5
	fixture := bestiary.ModelInfo{
		ID:               "test-quant-schema-model",
		Provider:         "testprovider",
		DisplayName:      "Quant Schema Test",
		RawFamily:        "test-family",
		Family:           "test",
		Variant:          "schema",
		Date:             "2024-01-01",
		ContextWindow:    131072,
		MaxOutput:        4096,
		Interleaved:      bestiary.Capability{Supported: false},
		CostInputPerMTok: &cost,
		ReleaseDate:      "2024-01-01",
		Knowledge:        "2024-01",
		Modalities: bestiary.Modalities{
			Input:  []bestiary.Modality{bestiary.ModalityText},
			Output: []bestiary.Modality{bestiary.ModalityText},
		},
		LastSynced: "2024-01-01T00:00:00Z",
		Source:     bestiary.DataSourceOllama,
		QuantVRAM: []bestiary.QuantVRAM{
			{
				Quant:               bestiary.QuantQ4_K_M,
				QuantRaw:            "Q4_K_M",
				WeightsBytes:        42_500_000_000,
				VRAMBytes:           46_000_000_000,
				VRAMContextTokens:   131072,
				Layers:              80,
				KVHeads:             8,
				HeadDim:             128,
				VRAMEstimatePartial: false,
			},
		},
	}

	var buf bytes.Buffer
	if err := bestiary.FormatModel(&buf, fixture, bestiary.FormatJSON); err != nil {
		t.Fatalf("FormatModel(JSON, populated QuantVRAM+Source) error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal FormatModel output: %v", err)
	}

	// The populated fixture must have QuantVRAM as a non-nil array.
	if qv, ok := out["QuantVRAM"]; !ok {
		t.Error("FormatModel JSON output missing \"QuantVRAM\" key with populated fixture")
	} else if arr, isArr := qv.([]any); !isArr || len(arr) == 0 {
		t.Errorf("FormatModel JSON QuantVRAM = %v (%T), want a non-empty array", qv, qv)
	}

	// Source must be the exact string.
	if src, ok := out["Source"]; !ok {
		t.Error("FormatModel JSON output missing \"Source\" key with populated fixture")
	} else if src != "ollama" {
		t.Errorf("FormatModel JSON Source = %v, want \"ollama\"", src)
	}

	// Verify the schema property set matches the output keys (same coverage as
	// TestJSONOutput_ConformsToSchema for the new fields).
	for prop := range topSchema.Properties {
		if _, ok := out[prop]; !ok {
			t.Errorf("FormatModel JSON missing schema property %q in populated fixture", prop)
		}
	}
	for key := range out {
		if _, ok := topSchema.Properties[key]; !ok {
			t.Errorf("FormatModel JSON has key %q not declared in schema", key)
		}
	}
}
