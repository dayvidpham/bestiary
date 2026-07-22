package bestiary_test

// Fences for the two curated compound-family re-keys.
//
// Both fix the same failure mode: a model whose ID leads with something other than
// its own family name, which the leading-token decomposition swallowed whole as a
// compound family. The corrections are exact-ID curated overrides (the dracarys
// precedent), and each one MOVES an entity key — so what is pinned here is the new
// key, the fact that the old key is gone, the instances that landed on it, and the
// registry census that must not move as a side effect.
//
//   - Qwen2.5-32B-EVA-v0.2: "qwen2.5-32b-eva/v0.2#32b" → "eva@0.2#32b", with the base
//     relationship promoted from a family token to a curated lineage edge.
//   - command-a-plus-05-2026: "command-a-plus" → "command/a-plus", converging the two
//     providers that previously disagreed about the model's identity.

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestEntityRekey_Eva pins EVA's split from its base model. EVA-UNIT-01 named its
// finetune after what it was trained FROM, so the decomposition read "qwen2.5-32b-eva"
// as the family and EVA's own release ("v0.2") as a variant. The override makes the
// family EVA's own and the version EVA's own release line, keeps the 32B size read
// mechanically off the ID, and moves the base relationship into lineage where a
// derivation belongs.
func TestEntityRekey_Eva(t *testing.T) {
	const key = "eva@0.2#32b"

	if bestiary.Entity__Eva__Version_0_2__Size_32b != key {
		t.Errorf("Entity__Eva__Version_0_2__Size_32b = %q, want %q",
			bestiary.Entity__Eva__Version_0_2__Size_32b, key)
	}

	e, ok := bestiary.EntityByKey(key)
	if !ok {
		t.Fatalf("EntityByKey(%q) not found — the curated eva override is missing or was dropped", key)
	}
	if got := e.Ref.String(); got != key {
		t.Errorf("entity renders key %q, want %q", got, key)
	}
	if e.Ref.Family != "eva" || e.Ref.Version != "0.2" || e.Ref.ParamSize != "32b" || e.Ref.Variant != "" {
		t.Errorf("entity ref = %+v, want family eva / version 0.2 / size 32b / no variant", e.Ref)
	}

	// The one catalog row that must have landed here.
	if len(e.Instances) != 1 || string(e.Instances[0].ID) != "Qwen2.5-32B-EVA-v0.2" {
		var ids []string
		for _, i := range e.Instances {
			ids = append(ids, string(i.ID))
		}
		t.Errorf("entity instances = %v, want exactly [Qwen2.5-32B-EVA-v0.2]", ids)
	}

	// The compound family the override retired must be gone: if it comes back, the
	// override stopped applying and the finetune has re-merged with its base name.
	if _, gone := bestiary.EntityByKey("qwen2.5-32b-eva/v0.2#32b"); gone {
		t.Error("the retired compound key \"qwen2.5-32b-eva/v0.2#32b\" is present again — " +
			"the curated eva override is no longer taking effect")
	}

	// The derivation is now STATED, not smuggled into a family token. It must be a
	// finetune of the 32B Qwen2.5 specifically — a size-agnostic parent would claim
	// every Qwen2.5 size as the base.
	if len(e.Lineage) != 1 {
		t.Fatalf("entity lineage = %+v, want exactly one curated edge", e.Lineage)
	}
	edge := e.Lineage[0]
	if edge.Kind != bestiary.DerivationFinetune {
		t.Errorf("lineage kind = %v, want finetune", edge.Kind)
	}
	if got, want := edge.Parent.String(), "qwen@2.5#32b"; got != want {
		t.Errorf("lineage parent = %q, want %q (the sized base, carried through the bake)", got, want)
	}
}

// TestEntityRekey_CommandAPlus pins Cohere's Command A+ onto the command family as
// the a-plus variant — the exact shape its command/r-plus sibling already had.
//
// This one also repaired a split: the two providers of the SAME model ID disagreed
// at the source (cohere's raw_family mapped to variant "a", dropping the "+"; nano-gpt
// sent an empty raw_family, leaving the whole "command-a-plus" captured as a family),
// so one model occupied two entities. Being provider-agnostic, the exact-ID override
// converges both.
func TestEntityRekey_CommandAPlus(t *testing.T) {
	const key = "command/a-plus"

	if bestiary.Entity__Command__A_plus != key {
		t.Errorf("Entity__Command__A_plus = %q, want %q", bestiary.Entity__Command__A_plus, key)
	}
	// The near-twin: both spellings now sit under the same Entity__Command__ prefix,
	// so the constants read as siblings rather than as unrelated families.
	if bestiary.Entity__Command__R_plus != "command/r-plus" {
		t.Errorf("Entity__Command__R_plus = %q, want %q", bestiary.Entity__Command__R_plus, "command/r-plus")
	}

	e, ok := bestiary.EntityByKey(key)
	if !ok {
		t.Fatalf("EntityByKey(%q) not found — the curated command-a-plus override is missing or was dropped", key)
	}
	if e.Ref.Family != "command" || e.Ref.Variant != "a-plus" {
		t.Errorf("entity ref = %+v, want family command / variant a-plus", e.Ref)
	}

	// Both provider rows of the one ID must have converged here.
	byProvider := map[bestiary.Provider]bestiary.ModelID{}
	for _, i := range e.Instances {
		byProvider[i.Provider] = i.ID
	}
	for _, p := range []bestiary.Provider{"cohere", "nano-gpt"} {
		if id, present := byProvider[p]; !present || string(id) != "command-a-plus-05-2026" {
			t.Errorf("provider %q instance = %q (present=%v), want command-a-plus-05-2026 — "+
				"the override must converge every provider of the id onto one entity", p, id, present)
		}
	}

	if _, gone := bestiary.EntityByKey("command-a-plus"); gone {
		t.Error("the retired compound key \"command-a-plus\" is present again — " +
			"the curated command-a-plus override is no longer taking effect")
	}

	// The override pins family/variant only. Each provider's own release date must
	// still come from the date pipeline, so the MM-YYYY month-leak guard is untouched:
	// the two rows genuinely carry different dates upstream.
	dates := map[bestiary.Provider]string{}
	for _, m := range bestiary.StaticModels() {
		if string(m.ID) == "command-a-plus-05-2026" {
			dates[m.Provider] = m.Date
		}
	}
	if dates["cohere"] != "2026-05-20" || dates["nano-gpt"] != "2026-05-22" {
		t.Errorf("per-provider dates = %v, want cohere 2026-05-20 / nano-gpt 2026-05-22 — "+
			"the family override must not disturb date extraction", dates)
	}
}

// TestEntityRekey_CensusUnmoved is the side-effect fence: both corrections RENAME an
// entity, they do not create or destroy one, so the registry census must be exactly
// what it was. A move that changed the count would mean an entity merged into or split
// off from something else — the collateral these narrow, exact-ID overrides exist to
// avoid.
func TestEntityRekey_CensusUnmoved(t *testing.T) {
	const wantEntities = 975
	if got := len(bestiary.Entities()); got != wantEntities {
		t.Errorf("registry census = %d entities, want %d — the curated re-keys must be renames, "+
			"not merges or splits", got, wantEntities)
	}
}
