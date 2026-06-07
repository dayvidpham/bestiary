package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestModelConstants_Unique verifies that Model_* constant names are unique.
//
// Uniqueness of constant NAMES is a compile-time Go guarantee (duplicate const
// declarations are a compile error). This test verifies runtime properties of
// the constants:
//  1. ModelIDs() returns a non-empty slice (constants exist).
//  2. No empty string values are present (each constant maps to a real model ID).
//  3. The two-pass resolver ran (at least one constant is in the returned slice).
//
// Note: ModelID VALUES may be non-unique across the allModelConstants array
// because the same model ID is often hosted by multiple providers, each of
// which receives a distinct constant name (e.g. Model_Anthropic_Claude_Opus_4_20250514
// and Model_OpenRouter_Claude_Opus_4_20250514 both have value "claude-opus-4-20250514").
// That cross-provider hosting is intentional and correct.
func TestModelConstants_Unique(t *testing.T) {
	ids := bestiary.ModelIDs()
	if len(ids) == 0 {
		t.Fatal("ModelIDs() returned an empty slice; models_constants_gen.go may not have been generated")
	}

	// Each constant value must be a non-empty string.
	for i, id := range ids {
		if id == "" {
			t.Errorf("ModelIDs()[%d]: empty ModelID (should never occur in generated constants)", i)
		}
	}

	// The full list must be large enough to be credible (regression guard).
	// At time of writing (2026-04-25) the generated registry contains 4327 constants.
	// A floor of 4000 catches silent codegen collapses while allowing natural growth.
	const minExpected = 4000
	if len(ids) < minExpected {
		t.Errorf("ModelIDs() returned only %d constants; expected at least %d — "+
			"re-run go generate ./... to regenerate models_constants_gen.go", len(ids), minExpected)
	}
}

// TestModelConstants_RoundTrip verifies that for every Model_* constant the
// ModelID value is non-empty and appears in the static model registry (LookupModel).
//
// NOTE: This test depends on the static model registry (models_static_gen.go).
// It does NOT depend on the Resolve API. When Resolve is
// available, a richer round-trip test (Format → Resolve → check) can be added.
func TestModelConstants_RoundTrip(t *testing.T) {
	ids := bestiary.ModelIDs()
	if len(ids) == 0 {
		t.Skip("ModelIDs() returned empty; skipping — run go generate ./... first")
	}

	for _, id := range ids {
		if id == "" {
			t.Errorf("ModelIDs() contains empty ModelID value")
			continue
		}
		// Each constant must resolve to at least one entry in the static registry.
		_, found := bestiary.LookupModel(id)
		if !found {
			t.Errorf("Model constant %q not found in static registry via LookupModel", id)
		}
	}
}

// TestModelConstants_ValuesAreRawIDs verifies that values are raw API model IDs
// (e.g. "claude-opus-4-20250514"), not Go identifier strings — values must never
// start with "Model_".
//
// Note: Defensive copy is verified separately by TestModelIDs_DefensiveCopy.
// Codegen idempotency (re-running `go generate` produces the same output)
// is verified by the golden-file tests in cmd/bestiary-gen, which capture the
// full generated source. This test only checks runtime value format of ModelIDs().
func TestModelConstants_ValuesAreRawIDs(t *testing.T) {
	ids := bestiary.ModelIDs()

	if len(ids) == 0 {
		t.Skip("ModelIDs() returned empty; skipping — run go generate ./... first")
	}

	for _, id := range ids {
		if id == "" {
			t.Errorf("ModelIDs() contains empty ModelID value")
			continue
		}
		s := string(id)
		// ModelIDs() must return raw API model ID values (e.g. "claude-opus-4-20250514"),
		// NOT Go identifier strings (the constant names live in the generated source).
		// The constant names all start with "Model_"; the values never should.
		if strings.HasPrefix(s, "Model_") {
			t.Errorf("ModelIDs(): value %q looks like a constant name, not a model ID; "+
				"ModelIDs() should return raw API ID string values, not Go identifier strings", s)
		}
	}
}

// TestModelIDs_DefensiveCopy verifies that the slice returned by ModelIDs()
// is a defensive copy: mutating the returned slice does not affect subsequent calls.
func TestModelIDs_DefensiveCopy(t *testing.T) {
	ids1 := bestiary.ModelIDs()
	if len(ids1) == 0 {
		t.Skip("ModelIDs() returned empty; skipping — run go generate ./... first")
	}

	original := ids1[0]
	ids1[0] = "mutated"

	ids2 := bestiary.ModelIDs()
	if len(ids2) == 0 {
		t.Fatal("ModelIDs() returned empty on second call")
	}
	if ids2[0] != original {
		t.Errorf("ModelIDs(): not a defensive copy; second call returned %q, want %q", ids2[0], original)
	}
}
