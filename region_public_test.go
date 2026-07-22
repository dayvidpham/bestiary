package bestiary_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestRegion_TextRoundTrip verifies MarshalText/UnmarshalText round-trips every member
// including the two absence spellings and the fail-safe. This is the JSON-contract
// round-trip that keeps the schema $defs.Region enum honest.
func TestRegion_TextRoundTrip(t *testing.T) {
	all := []bestiary.Region{
		bestiary.RegionNone, bestiary.RegionUS, bestiary.RegionEU, bestiary.RegionAPAC,
		bestiary.RegionGlobal, bestiary.RegionAU, bestiary.RegionJP, bestiary.RegionOther,
	}
	for _, r := range all {
		b, err := r.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", r, err)
		}
		var back bestiary.Region
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", b, err)
		}
		if back != r {
			t.Errorf("round-trip %v -> %q -> %v", r, b, back)
		}
	}
	// "unspecified" is emitted for RegionNone (never blank) and the empty string also
	// decodes to RegionNone.
	if b, _ := bestiary.RegionNone.MarshalText(); string(b) != "unspecified" {
		t.Errorf("RegionNone marshals to %q, want unspecified", b)
	}
	var r bestiary.Region = bestiary.RegionEU
	if err := r.UnmarshalText([]byte("")); err != nil || r != bestiary.RegionNone {
		t.Errorf("empty token should decode to RegionNone, got %v err=%v", r, err)
	}
}

// TestModelInfo_RegionJSON verifies a ModelInfo serializes Region as a lowercase token
// (not an int) and RegionRaw as a string, matching the schema.
func TestModelInfo_RegionJSON(t *testing.T) {
	m := bestiary.ModelInfo{ID: "us.anthropic.claude", Region: bestiary.RegionUS}
	enc, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(enc, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["Region"] != "us" {
		t.Errorf("JSON Region = %v, want \"us\"", out["Region"])
	}
	if _, ok := out["RegionRaw"]; !ok {
		t.Error("JSON missing RegionRaw key")
	}
}

// TestProviderInstance_RegionProjection is the registry-projection fence: a dotted
// Bedrock instance (us.anthropic.claude-sonnet-4-5-…) surfaces its Region on the rolled-up
// ProviderInstance. The entity claude/sonnet@4.5 must have at least one instance whose
// Region is a named member (not unspecified).
func TestProviderInstance_RegionProjection(t *testing.T) {
	e, ok := bestiary.EntityByTuple("claude", "sonnet", "4.5", "")
	if !ok {
		t.Fatal("claude/sonnet@4.5 entity not found")
	}
	var sawUS, sawRegioned bool
	for _, inst := range e.Instances {
		if inst.Region != bestiary.RegionNone {
			sawRegioned = true
		}
		if inst.Region == bestiary.RegionUS {
			sawUS = true
		}
	}
	if !sawRegioned {
		t.Error("no ProviderInstance surfaced a Bedrock region; the registry projection dropped it")
	}
	if !sawUS {
		t.Error("expected a us-region Bedrock instance (us.anthropic.claude-sonnet-4-5-...) to project RegionUS")
	}
}

// TestEntity_RegionsAggregate_Census pins the sorted-unique Entity.Regions aggregate
// for a real multi-region entity. claude/sonnet@4.5 is served in every Bedrock region
// AND plain (no prefix), so its aggregate is [unspecified, us, eu, global, au, jp] —
// sorted ASCENDING BY REGION ENUM VALUE (None<US<EU<Global<AU<JP), de-duplicated. This
// is the true bake value (verified against the committed catalog); it INCLUDES
// "unspecified" (the entity has a plain instance) and orders global before au/jp.
func TestEntity_RegionsAggregate_Census(t *testing.T) {
	e, ok := bestiary.EntityByTuple("claude", "sonnet", "4.5", "")
	if !ok {
		t.Fatal("claude/sonnet@4.5 entity not found")
	}
	want := []bestiary.Region{
		bestiary.RegionNone, bestiary.RegionUS, bestiary.RegionEU,
		bestiary.RegionGlobal, bestiary.RegionAU, bestiary.RegionJP,
	}
	if !reflect.DeepEqual(e.Regions, want) {
		got := make([]string, len(e.Regions))
		for i, r := range e.Regions {
			got[i] = r.String()
		}
		t.Errorf("claude/sonnet@4.5 Regions = %v, want [unspecified us eu global au jp]", got)
	}
	// Sorted-unique invariant: strictly ascending, no duplicates.
	for i := 1; i < len(e.Regions); i++ {
		if e.Regions[i] <= e.Regions[i-1] {
			t.Errorf("Regions not strictly ascending at %d: %v", i, e.Regions)
		}
	}
}
