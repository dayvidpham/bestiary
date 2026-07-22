package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestEntityConstants_Unique verifies runtime properties of the generated Entity__*
// constants (the provider-agnostic successor to the removed Model__* surface).
//
// Uniqueness of constant NAMES is a compile-time Go guarantee (a duplicate const
// declaration is a compile error) AND a codegen-time guarantee (the injectivity guard
// fails the bake on any duplicate name). This test verifies the runtime shape:
//  1. EntityKeys() returns a non-empty slice (constants exist).
//  2. No empty-string values are present (each constant maps to a real entity key).
//
// Unlike the old Model__ surface, Entity__ VALUES are unique: one constant per entity,
// no provider flavoring. The value-uniqueness itself is pinned in
// TestEntityConstants_ValuesAreCanonicalKeys.
func TestEntityConstants_Unique(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Fatal("EntityKeys() returned an empty slice; entities_constants_gen.go may not have been generated")
	}

	for i, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys()[%d]: empty entity key (should never occur in generated constants)", i)
		}
	}

	// Exact census: the hard-cut bake emits one Entity__ constant per registry entity.
	// At this bake the registry holds 975 entities. This is an EXACT pin (not a floor):
	// a change to the entity count is a deliberate act that must move this literal in the
	// same commit, so a silent drift is caught.
	const wantEntityCount = 975
	if len(keys) != wantEntityCount {
		t.Errorf("EntityKeys() returned %d constants; expected exactly %d — "+
			"re-run go generate ./... and update this census literal if the entity count changed intentionally",
			len(keys), wantEntityCount)
	}

	// Floor guard (defense-in-depth against a silent codegen collapse that also edits the
	// census literal): the count must never fall below a credible floor.
	const minExpected = 800
	if len(keys) < minExpected {
		t.Errorf("EntityKeys() returned only %d constants; expected at least %d — "+
			"re-run go generate ./... to regenerate entities_constants_gen.go", len(keys), minExpected)
	}
}

// TestEntityConstants_RoundTrip verifies that every Entity__* constant value is a real
// entity key: it appears among the entities the static registry exposes via Entities().
// This is the entity-level analog of the old Model__/LookupModel round-trip (the
// constant set is derived from the SAME registry index, so every value must round-trip).
func TestEntityConstants_RoundTrip(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	registryKeys := make(map[string]bool)
	for _, e := range bestiary.Entities() {
		registryKeys[e.Ref.String()] = true
	}

	for _, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys() contains empty value")
			continue
		}
		if !registryKeys[k] {
			t.Errorf("Entity constant value %q not found among registry entities (Entities())", k)
		}
	}
}

// TestProvidersOf_ScoutCensus pins the ProvidersOf census for the scout entity and
// exercises a generated Entity__ constant end-to-end (the BDD case: the constant
// compiles, resolves to an entity, and ProvidersOf returns 11 distinct providers across
// its 13 instances — no provider-flavored entity constant remains).
func TestProvidersOf_ScoutCensus(t *testing.T) {
	// The generated constant compiles and carries the canonical key.
	const scoutKey = bestiary.Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct
	if scoutKey != "llama/scout@4#17b-16e{instruct}" {
		t.Fatalf("Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct = %q; want %q", scoutKey, "llama/scout@4#17b-16e{instruct}")
	}

	ent, ok := bestiary.EntityByTuple("llama", "scout", "4", "17b-16e", "instruct")
	if !ok {
		t.Fatalf("EntityByTuple did not find the scout entity %q", scoutKey)
	}
	if len(ent.Instances) != 13 {
		t.Errorf("scout instances = %d; want 13", len(ent.Instances))
	}

	provs := bestiary.ProvidersOf(ent.Ref)
	if len(provs) != 11 {
		t.Errorf("ProvidersOf(scout) = %d providers; want 11 (across 13 instances)", len(provs))
	}
	// The result must be sorted ascending and de-duplicated.
	for i := 1; i < len(provs); i++ {
		if !(provs[i-1] < provs[i]) {
			t.Errorf("ProvidersOf(scout) not strictly ascending/deduped at index %d: %q then %q", i, provs[i-1], provs[i])
		}
	}
}

// TestEntityConstants_ValuesAreCanonicalKeys verifies that the constant VALUES are
// canonical entity-key strings (e.g. "llama/scout@4#17b-16e{instruct}"), NOT the Go
// identifier names — a value must never start with "Entity_". It also pins value
// uniqueness (one entity per constant). This is the successor to the old
// ValuesAreRawIDs check, renamed because the values are now canonical keys.
func TestEntityConstants_ValuesAreCanonicalKeys(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys() contains empty value")
			continue
		}
		if strings.HasPrefix(k, "Entity_") {
			t.Errorf("EntityKeys(): value %q looks like a constant name, not an entity key; "+
				"EntityKeys() must return canonical key string values, not Go identifier strings", k)
		}
		if _, dup := seen[k]; dup {
			t.Errorf("EntityKeys(): duplicate value %q — Entity__ constants must be one-per-entity (no provider flavoring)", k)
		}
		seen[k] = struct{}{}
	}
}

// TestEntityKeys_DefensiveCopy verifies that the slice returned by EntityKeys() is a
// defensive copy: mutating it does not affect subsequent calls.
func TestEntityKeys_DefensiveCopy(t *testing.T) {
	k1 := bestiary.EntityKeys()
	if len(k1) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	original := k1[0]
	k1[0] = "mutated"

	k2 := bestiary.EntityKeys()
	if len(k2) == 0 {
		t.Fatal("EntityKeys() returned empty on second call")
	}
	if k2[0] != original {
		t.Errorf("EntityKeys(): not a defensive copy; second call returned %q, want %q", k2[0], original)
	}
}
