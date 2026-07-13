package bestiary

// resolve_wave3_internal_test.go — synthetic-registry replacements for two resolve
// tests that the July catalog refresh silently turned into permanent SKIPs (zero
// coverage). They now drive the full Resolve pipeline over a controlled registry
// (withSyntheticRegistry), so a future data refresh can NEVER disable them: the exemplar
// shapes are guaranteed present and the assertions are hard failures, not skips.
//
// Like the other withSyntheticRegistry tests, these mutate shared package state
// (staticModels + the entity index) and must NOT run in parallel.

import (
	"reflect"
	"strings"
	"testing"
)

// TestResolve_BracketSuffixStripping_RoundTrip verifies the canonical round-trip for a
// model carrying an IDENTITY modifier and a date: ModelRef.String() emits the
// "{modifier}" identity segment, and Resolve() must parse that canonical string back to
// a ref with the same (Family, Variant, Date, Modifier).
//
// Was data-pinned to "any Anthropic static model with Modifier and Date"; the July
// catalog has ZERO such models (every anthropic modifier is the attribute-class
// "thinking", which is projected out of the identity Modifier), so it skipped silently.
// A synthetic identity-modifier exemplar makes it immune to catalog drift.
func TestResolve_BracketSuffixStripping_RoundTrip(t *testing.T) {
	seed := ModelInfo{
		ID:       ModelID("meta/llama-chat-3.3-instruct"),
		Provider: Provider("meta"),
		Family:   Family("llama"),
		Variant:  "chat", // explicit variant so the canonical family/variant/version parse is unambiguous
		Version:  "3.3",
		Date:     "2025-01-01",
		Modifier: []string{"instruct"}, // identity-class → renders in the "{...}" segment
	}
	withSyntheticRegistry(t, []ModelInfo{seed}, func(t *testing.T) {
		ref := seed.Ref()
		canonical := ref.String()
		if canonical == "" {
			t.Fatalf("ModelRef.String() returned empty for %+v", ref)
		}
		// The canonical MUST carry the identity-modifier bracket segment — that is the
		// round-trip surface under test.
		if !strings.Contains(canonical, "{instruct}") {
			t.Fatalf("canonical %q is missing the {instruct} identity-modifier segment", canonical)
		}

		refs, err := Resolve(canonical)
		if err != nil {
			t.Fatalf("Resolve(%q): the canonical round-trip must succeed, got: %v", canonical, err)
		}
		if len(refs) == 0 {
			t.Fatalf("Resolve(%q) returned no refs; the round-trip must return at least one", canonical)
		}

		found := false
		for _, r := range refs {
			if r.Family == ref.Family && r.Variant == ref.Variant &&
				r.Date == ref.Date && reflect.DeepEqual(r.Modifier, ref.Modifier) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Resolve(%q): no ref matched seed (Family=%q Variant=%q Date=%q Modifier=%v); refs=%v",
				canonical, ref.Family, ref.Variant, ref.Date, ref.Modifier, refs)
		}
	})
}

// TestResolve_Reasoner_Distinct_FromThinking verifies that a reasoner model (IDENTITY
// modifier "reasoning", part of the entity key) and a thinking model (ATTRIBUTE modifier
// "thinking", projected OUT of the key) that are otherwise identical resolve to DISTINCT
// entities — never wrong-merged into one.
//
// Was data-pinned to claude-3-7-sonnet-reasoner + claude-3-7-sonnet-thinking:N; the July
// catalog has ZERO -reasoner models, so it skipped silently. The synthetic pair pins the
// group-key invariant directly (and is mutation-sensitive: reclassifying "reasoning" as
// attribute-class would collapse the two into one entity and fail here).
func TestResolve_Reasoner_Distinct_FromThinking(t *testing.T) {
	reasoner := ModelInfo{
		ID:       ModelID("vendor/claude-sonnet-reasoner"),
		Provider: Provider("vendor"),
		Family:   Family("claude"),
		Variant:  "sonnet",
		Version:  "3.7",
		Modifier: []string{"reasoning"}, // identity-class → in the entity key
	}
	thinking := ModelInfo{
		ID:       ModelID("vendor/claude-sonnet-thinking"),
		Provider: Provider("vendor"),
		Family:   Family("claude"),
		Variant:  "sonnet",
		Version:  "3.7",
		Modifier: []string{"thinking"}, // attribute-class → projected out of the entity key
	}
	withSyntheticRegistry(t, []ModelInfo{reasoner, thinking}, func(t *testing.T) {
		// The reasoner keys as {reasoning}; the thinker's attribute modifier is excluded,
		// so it keys with no identity modifier — two DISTINCT entities. (Entities() also
		// includes baked-metadata standalones, so we assert on the two specific synthetic
		// entities by tuple + instance, not on the total count.)
		rEnt, rok := EntityByTuple(Family("claude"), "sonnet", "3.7", "", "reasoning")
		if !rok {
			t.Fatal("reasoner entity (claude/sonnet@3.7{reasoning}) must exist")
		}
		tEnt, tok := EntityByTuple(Family("claude"), "sonnet", "3.7", "")
		if !tok {
			t.Fatal("thinking entity (claude/sonnet@3.7, no identity modifier) must exist")
		}
		if rEnt.Ref.String() == tEnt.Ref.String() {
			t.Fatalf("reasoner and thinking wrong-merged into one entity key %q — they must be distinct",
				rEnt.Ref.String())
		}
		// Each entity must be backed by its own synthetic model (confirming these are the
		// two models under test, not an incidental collision), with non-overlapping
		// instance IDs.
		if !entityHasInstance(rEnt, reasoner.ID) {
			t.Errorf("reasoner entity %q does not carry instance %q; instances=%v", rEnt.Ref.String(), reasoner.ID, instanceIDs(rEnt))
		}
		if !entityHasInstance(tEnt, thinking.ID) {
			t.Errorf("thinking entity %q does not carry instance %q; instances=%v", tEnt.Ref.String(), thinking.ID, instanceIDs(tEnt))
		}
		if entityHasInstance(rEnt, thinking.ID) || entityHasInstance(tEnt, reasoner.ID) {
			t.Errorf("reasoner and thinking instances leaked across entities (wrong-merge)")
		}

		// Each raw id resolves to itself, and their result sets do not overlap.
		reasonerIDs := map[ModelID]struct{}{}
		rRefs, err := Resolve(string(reasoner.ID), WithScheme(SchemeRaw))
		if err != nil {
			t.Fatalf("Resolve(%q, raw): %v", reasoner.ID, err)
		}
		for _, r := range rRefs {
			if r.ID != reasoner.ID {
				t.Errorf("Resolve(reasoner) returned ID=%q, want %q", r.ID, reasoner.ID)
			}
			reasonerIDs[r.ID] = struct{}{}
		}
		tRefs, err := Resolve(string(thinking.ID), WithScheme(SchemeRaw))
		if err != nil {
			t.Fatalf("Resolve(%q, raw): %v", thinking.ID, err)
		}
		for _, r := range tRefs {
			if r.ID != thinking.ID {
				t.Errorf("Resolve(thinking) returned ID=%q, want %q", r.ID, thinking.ID)
			}
			if _, overlap := reasonerIDs[r.ID]; overlap {
				t.Errorf("reasoner and thinking share ID %q — they must be distinct models", r.ID)
			}
		}
	})
}

// entityHasInstance reports whether e carries a provider instance with the given id.
func entityHasInstance(e Entity, id ModelID) bool {
	for _, inst := range e.Instances {
		if inst.ID == id {
			return true
		}
	}
	return false
}

// instanceIDs returns the ids of e's provider instances (for diagnostics).
func instanceIDs(e Entity) []ModelID {
	out := make([]ModelID, 0, len(e.Instances))
	for _, inst := range e.Instances {
		out = append(out, inst.ID)
	}
	return out
}
