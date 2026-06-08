package bestiary

import "testing"

// modifierclass_internal_test.go — internal unit tests for the graceful-degrade
// contract of the modifier-class table. These exercise the unexported classify
// seam directly so the load-failure path (initModifierClassTable returns an
// empty-but-non-nil table) can be pinned without touching the package-level
// sync.Once.

// TestModifierClassTable_DegradeEmptyAndNil verifies the load-failure contract:
// when the table is empty (the exact value initModifierClassTable returns on any
// read/parse error) OR nil, EVERY token — including curated identity tokens,
// curated attribute tokens, and family-overridden tokens — degrades to
// ModifierClassIdentity, and classify never panics.
func TestModifierClassTable_DegradeEmptyAndNil(t *testing.T) {
	// Tokens spanning all three buckets: a curated attribute, a curated identity,
	// the family-overridden ambiguous token, and an unknown token.
	probes := []struct {
		token string
		fam   Family
	}{
		{"thinking", ""},        // curated attribute in the real table
		{"instruct", "llama"},   // curated identity in the real table
		{"turbo", "glm"},        // demoted to attribute by override in the real table
		{"totally-unknown", ""}, // never curated
		{"", ""},                // empty token
	}

	empty := &modifierClassTable{
		global:    map[string]ModifierClass{},
		perFamily: map[Family]map[string]ModifierClass{},
	}
	var nilTable *modifierClassTable

	for _, p := range probes {
		if got := empty.classify(p.token, p.fam); got != ModifierClassIdentity {
			t.Errorf("empty table classify(%q, %q) = %v, want ModifierClassIdentity (degrade)", p.token, p.fam, got)
		}
		if got := nilTable.classify(p.token, p.fam); got != ModifierClassIdentity {
			t.Errorf("nil table classify(%q, %q) = %v, want ModifierClassIdentity (degrade)", p.token, p.fam, got)
		}
	}
}

// TestModifierClassTable_LoadedHasContent guards the positive case: the embedded
// table actually loaded (so the degrade test above is testing degrade, not the
// same empty state the production path would hit on a real load failure).
func TestModifierClassTable_LoadedHasContent(t *testing.T) {
	tbl := loadModifierClassTable()
	if tbl == nil {
		t.Fatal("loadModifierClassTable() returned nil; must never be nil")
	}
	if len(tbl.global) == 0 {
		t.Error("loaded global table is empty; embedded modifier_class.json failed to load")
	}
	// The per-family override seed must be present and demote glm:turbo.
	if got := tbl.classify("turbo", "glm"); got != ModifierClassAttribute {
		t.Errorf("loaded table classify(turbo, glm) = %v, want ModifierClassAttribute", got)
	}
}
