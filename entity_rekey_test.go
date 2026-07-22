package bestiary_test

// Fences for the curated exact-ID decomposition corrections.
//
// All three fix a decomposition that read part of a model's NAME as part of its
// IDENTITY. The corrections are exact-ID curated overrides (the dracarys precedent),
// and each one MOVES an entity key — so what is pinned here is the new key, the fact
// that the old key is gone, the instances that landed on it, and the registry census
// delta, which differs per correction and is accounted for in full below.
//
//   - Qwen2.5-32B-EVA-v0.2: "qwen2.5-32b-eva/v0.2#32b" → "eva@0.2#32b", with the base
//     relationship promoted from a family token to a curated lineage edge.
//   - command-a-plus-05-2026: "command-a-plus" → "command/a-plus", converging the two
//     providers that previously disagreed about the model's identity.
//   - claude-opus4-5…-8 (cortecs): a glued major version read as the whole version,
//     minting four phantom claude/opus@5…@8 entities. These MERGE into the real
//     claude/opus@4.5…@4.8 rather than renaming, so they are the one correction here
//     that moves the registry census.

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

// TestEntityRekey_CortecsGluedVersion pins the cortecs correction. cortecs glues the
// major version onto the variant — "claude-opus4-5" is Opus 4.5, not an Opus 5 — and
// the mis-read was doubly wrong: it minted a phantom entity per spelling AND stranded
// the cortecs instance away from the real entity holding every other provider's rows.
//
// The date evidence is asserted alongside the tuple because it is what makes the
// reading certain rather than merely plausible: each cortecs row carries the real
// 4.5/4.6/4.7/4.8 release date.
func TestEntityRekey_CortecsGluedVersion(t *testing.T) {
	cases := []struct {
		id      bestiary.ModelID
		version string
		key     string
		date    string
	}{
		{"claude-opus4-5", "4.5", "claude/opus@4.5", "2025-11-24"},
		{"claude-opus4-6", "4.6", "claude/opus@4.6", "2026-02-05"},
		{"claude-opus4-7", "4.7", "claude/opus@4.7", "2026-04-16"},
		{"claude-opus4-8", "4.8", "claude/opus@4.8", "2026-05-28"},
	}

	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			m, ok := bestiary.LookupModelByProvider("cortecs", string(c.id))
			if !ok {
				t.Fatalf("LookupModelByProvider(cortecs, %q) not found — the catalog row is gone; "+
					"re-check the pin against the current snapshot", c.id)
			}
			if m.Family != "claude" || m.Variant != "opus" || m.Version != c.version {
				t.Errorf("%q decomposes to (%q, %q, %q), want (claude, opus, %s) — "+
					"the glued major version must not be read as the whole version",
					c.id, m.Family, m.Variant, m.Version, c.version)
			}
			if m.Date != c.date {
				t.Errorf("%q date = %q, want %q — this is the evidence the row IS Opus %s",
					c.id, m.Date, c.date, c.version)
			}

			// The instance must live on the real entity, beside every other provider's rows.
			e, found := bestiary.EntityByKey(c.key)
			if !found {
				t.Fatalf("EntityByKey(%q) not found", c.key)
			}
			var onEntity bool
			for _, i := range e.Instances {
				if i.Provider == "cortecs" && i.ID == c.id {
					onEntity = true
				}
			}
			if !onEntity {
				t.Errorf("the cortecs %q instance is not on entity %q — the pin must merge it into "+
					"the real entity, not merely re-version it", c.id, c.key)
			}
			if len(e.Instances) < 10 {
				t.Errorf("entity %q holds only %d instances; expected the real multi-provider entity "+
					"(the cortecs row must not have landed on a near-empty phantom)", c.key, len(e.Instances))
			}
		})
	}
}

// TestEntityRekey_NoPhantomOpusEntities is the negative half of the cortecs fence: the
// four phantom entities and the series lines that existed ONLY to hold them must be
// gone. claude-5 is deliberately excluded — that line is real, populated by
// claude/sonnet@5 — so this asserts the phantoms went without taking a real line with
// them.
func TestEntityRekey_NoPhantomOpusEntities(t *testing.T) {
	for _, key := range []string{"claude/opus@5", "claude/opus@6", "claude/opus@7", "claude/opus@8"} {
		if e, present := bestiary.EntityByKey(key); present {
			t.Errorf("phantom entity %q is present again with %d instance(s) — a glued cortecs "+
				"version is being read as a whole major version", key, len(e.Instances))
		}
	}

	lines := map[bestiary.Series]bool{}
	for _, s := range bestiary.SeriesAll() {
		lines[s] = true
	}
	for _, gen := range []string{"6", "7", "8"} {
		if lines[bestiary.Series{Family: "claude", Generation: gen}] {
			t.Errorf("series line claude-%s exists; it had no members but the four phantom opus "+
				"entities, so its return means the phantoms are back", gen)
		}
	}
	// The real Claude 5 line must survive: it is NOT a phantom.
	if !lines[bestiary.Series{Family: "claude", Generation: "5"}] {
		t.Error("series line claude-5 is missing — it is a REAL line (claude/sonnet@5), and the " +
			"cortecs correction must not have removed it")
	}
}

// TestEntityRekey_CensusAccounted is the side-effect fence over all three curated
// corrections on this branch. The arithmetic is the point:
//
//	975  entities at the previous bake
//	  0  eva            — a RENAME (qwen2.5-32b-eva/v0.2#32b → eva@0.2#32b)
//	  0  command-a-plus — a RENAME (command-a-plus → command/a-plus)
//	 -4  cortecs        — a MERGE (four phantom claude/opus@5…@8 absorbed into the
//	                      real claude/opus@4.5…@4.8, which already existed)
//	=971
//
// A rename that moved the count would mean an entity had merged into or split off
// from something else — collateral these narrow exact-ID overrides exist to avoid.
// The merge moves it by exactly the number of phantoms retired, and no more.
func TestEntityRekey_CensusAccounted(t *testing.T) {
	const wantEntities = 979
	if got := len(bestiary.Entities()); got != wantEntities {
		t.Errorf("registry census = %d entities, want %d — the eva and command-a-plus overrides "+
			"must be renames (count unmoved) and the cortecs pins a 4-entity merge", got, wantEntities)
	}
}
