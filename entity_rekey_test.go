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
	// 979 → 977 with the "p"-as-dot version decode: the phantom glm@5p1 and glm@5p2 keys
	// merge into the real glm@5.1 and glm@5.2. Like the cortecs pins this is a MERGE (two
	// keys retired, none renamed), so the count moves by exactly the number of phantoms
	// retired and no more.
	//
	// 977 → 978 with the tts-1-hd identity split: "hd" becomes an IDENTITY modifier so
	// tts@1{hd} splits off from tts@1 (a SPLIT: one key added, none renamed).
	//
	// 978 → 976 with the o-series dual-identity fix: openai-o1 / openai-o3 / openai-o3-mini
	// canonicalize onto the existing gpt/o entities, vacating the two junk family-"o" keys
	// (a MERGE: two keys retired, none renamed).
	//
	// 976 → 955 with the dot-lost version repair + 1t param-size routing: dotless/dash-glued
	// qwen/minimax/mistral spellings fold onto their dotted entities and ling@1t/ring@1t
	// re-key to #1t (ring-2.6-1t-free merges into ring@2.6#1t). Net −21 (mostly merges).
	//
	// 955 → 947 with the entity-level MERGE-only N→N.0 fold (C4): 8 bare-N entities fold
	// onto their N.0 siblings (claude/opus, claude/sonnet, gemini/flash, gemini/pro,
	// imagen, imagen{fast}, imagen/ultra, veo). A pure MERGE, none renamed.
	// 947 -> 958 with the 2026-07-23 snapshot refresh: +11 upstream additions on top of the
	// closing-batch arithmetic below (which still holds relative to its own base).
	// 958 -> 957 with the v0.2.8 curation slice: command/a{translate} splits out (+1, "translate"
	// now a peeled identity modifier so Command A Translate stops collapsing onto base command/a)
	// and the deepseek dash-glued dot-lost pins merge phantom deepseek@1 / deepseek@2 onto the
	// dotted deepseek/v3.1 and deepseek/v3.2-exp entities (−2). Net −1.
	//
	// 957 -> 940 with the global free demotion: "free" leaves the entity key entirely, so
	// 17 free-tier keys retire (a pure MERGE — 0 added, instance total conserved) as their
	// instances re-home onto the surviving sibling. ling/flash-free@2.6 is carved out by an
	// exact-ID pin and is NOT among them.
	//
	// 940 -> 939 with the qwen3-coder-next suppress-pin extended to the unprefixed spelling:
	// qwen/coder@3#1m retires (1 key, 0 added, also a pure MERGE) because its '1m' is a
	// 1M-context tier marker rather than a parameter size; the InferX instance rejoins
	// qwen/coder@3.
	//
	// 939 -> 947 with the ling/inkling/kling collision split. This one is NOT a pure merge:
	// the upstream catalog stamps raw_family "ling" on all 14 rows of two unrelated product
	// lines — Thinking Machines' 6 Inkling instances and vercel's 8 klingai video rows — so
	// both were folded onto inclusionAI's Ling family. Splitting them RETIRES 2 keys and ADDS
	// 10: bare `ling` empties (its only occupants were the 6 mislabelled Inkling instances,
	// which leave for the new `inkling` key) and the phantom `kling-v2@6` is re-keyed to
	// `kling@2.6`, while the 8 klingai rows split off into 8 `kling/v*` keys of their own.
	// -2 + 10 = +8. inclusionAI's five surviving ling keys are untouched.
	//
	// 947 -> 946 with the keyspace-wide mimo normalization. This one is a rename block
	// with a single merge inside it: the mimo series letter stops keying the entity, so
	// all ten mimo keys are rewritten and nine of them map one-to-one onto their new
	// spelling (mimo/v@2.5 -> mimo@2.5, mimo/v2.5-tts -> mimo@2.5{tts}, and so on). The
	// tenth, mimo/pro, held the single xiaomi/mimo-v2.5-pro-ultraspeed instance: once
	// "ultraspeed" is curated as an attribute-class tier that instance rejoins
	// mimo@2.5{pro}, which is the only key count actually lost. -10 + 9 = -1, with all
	// 93 mimo instances conserved.
	//
	// 945 -> 942 with the gpt tier re-key. luna/sol/terra move from being FAMILIES of
	// their own (gpt-luna, gpt-sol, gpt-terra) to being VARIANTS of family gpt, so all
	// twelve tier keys are rewritten: gpt-<tier> -> gpt/<tier>, gpt-<tier>@5.6 ->
	// gpt/<tier>@5.6, gpt-<tier>/pro@5.6 -> gpt/<tier>@5.6{pro}. That is nine renames.
	// The remaining three, gpt-<tier>/pro, have no successor spelling with an occupant:
	// their only instances were venice's squashed-version ids (openai-gpt-56-<tier>-pro),
	// and those are pinned to 5.6 here, so every -pro instance is dated and the undated
	// {pro} key empties. -12 + 9 = -3, with all 76 tier instances conserved.
	const wantEntities = 942
	if got := len(bestiary.Entities()); got != wantEntities {
		t.Errorf("registry census = %d entities, want %d — this literal is the running total of "+
			"every curated key retirement (see the arithmetic above it); update it in the same "+
			"commit if the entity count changed intentionally", got, wantEntities)
	}
}

// TestEntityMerge_NToN0_MergeOnly fences the C4 entity-level MERGE-only N->N.0 fold: a
// family that spells BOTH a bare integer version N and the dotted N.0 for the SAME
// (variant, param-size, identity-modifiers) folds the bare entity onto the dotted one.
//
// It pins the EXACT set of 8 merge pairs (the whole authored table), asserts each fold is
// a pure MERGE (the bare key is gone from Entities(), a bare EXPRESSION resolves through
// the alias to the dotted entity, and the dotted entity's instance count EQUALS THE SUM of
// both spellings' pre-fold instances — so a merge that silently DROPPED an instance is
// caught, not just a mis-key), and guards the negative control: llama@4 has no 4.0 sibling
// anywhere in the family, so it is NEVER touched — the fold is a merge, never a rename that
// would mint a phantom @4.0 for it.
func TestEntityMerge_NToN0_MergeOnly(t *testing.T) {
	// The complete authored merge table: bare key -> (dotted key it folds into, the SUM of
	// both spellings' pre-fold instance counts). The wantInst figures are the pre-merge
	// bare+dotted sums measured on the catalog immediately before the fold:
	//   opus 21+1, sonnet 26+1, flash 28+1, pro 24+2, imagen 1+3, imagen{fast} 1+1,
	//   ultra 1+1, veo 1+2.
	type merge struct {
		dotted   string
		wantInst int
	}
	merges := map[string]merge{
		"claude/opus@4":   {"claude/opus@4.0", 22},
		"claude/sonnet@4": {"claude/sonnet@4.0", 27},
		// 29 -> 28: an upstream rehost row left at the 2026-07-23 refresh.
		"gemini/flash@3": {"gemini/flash@3.0", 28},
		"gemini/pro@3":   {"gemini/pro@3.0", 26},
		"imagen@4":       {"imagen@4.0", 4},
		"imagen@4{fast}": {"imagen@4.0{fast}", 2},
		"imagen/ultra@4": {"imagen/ultra@4.0", 2},
		"veo@3":          {"veo@3.0", 3},
	}

	// The bare keys must be ABSENT from the entity set (they folded away, never renamed).
	present := map[string]bool{}
	for _, e := range bestiary.Entities() {
		present[e.Ref.String()] = true
	}
	for bare, m := range merges {
		if present[bare] {
			t.Errorf("bare key %q still exists as a distinct entity; it must fold into %q", bare, m.dotted)
		}
		if !present[m.dotted] {
			t.Errorf("dotted merge target %q is absent from the registry", m.dotted)
		}
		// A bare EXPRESSION must resolve through the fold to the dotted entity.
		e, ok := bestiary.EntityByKey(bare)
		if !ok {
			t.Errorf("EntityByKey(%q) = false; the bare spelling must resolve to the merged entity", bare)
			continue
		}
		if got := e.Ref.String(); got != m.dotted {
			t.Errorf("EntityByKey(%q) resolved to %q, want the merged entity %q", bare, got, m.dotted)
		}
		// PURE MERGE: the merged entity must carry EVERY instance of both spellings — the
		// bare+dotted pre-fold sum. A regression that dropped a source row during re-keying
		// would still land on the right key but fail here.
		if got := len(e.Instances); got != m.wantInst {
			t.Errorf("merged entity %q has %d instances, want %d (the bare+dotted pre-fold sum) — "+
				"the fold must not drop or duplicate an instance", m.dotted, got, m.wantInst)
		}
	}

	// Negative control: llama@4 has no llama@4.0 sibling, so it stays exactly llama@4 —
	// a pure merge never renames a lone bare-N line.
	if _, ok := bestiary.EntityByKey("llama@4.0"); ok {
		t.Error("llama@4.0 exists; the MERGE-only fold must not mint a dotted phantom for a lone bare-N line")
	}
	e, ok := bestiary.EntityByKey("llama@4")
	if !ok {
		t.Fatal("EntityByKey(llama@4) = false; the lone bare-N line must be untouched by the fold")
	}
	if got := e.Ref.String(); got != "llama@4" {
		t.Errorf("llama@4 was moved to %q; a lone bare-N line must be untouched", got)
	}
}

// TestEntityMerge_NToN0_TupleInvariant is the general, catalog-guarding sweep of the
// NormalizeEntityVersion invariant — distinct from the 8-pin integration table above and
// from the coarser Series-level (Family, Generation) census. It asserts the full
// entity-identity tuple property directly: NO published entity may carry a bare-integer
// version N while ANOTHER entity exists with the IDENTICAL (family, variant, param-size,
// identity-modifiers) tuple and version exactly "N.0". If both survived, the MERGE-only
// fold failed to collapse them — the exact failure a future catalog change (a new variant
// that spells both N and N.0) could introduce that the 8 fixed pins would not cover.
func TestEntityMerge_NToN0_TupleInvariant(t *testing.T) {
	ents := bestiary.Entities()
	present := make(map[string]bool, len(ents))
	for _, e := range ents {
		present[e.Ref.String()] = true
	}
	for _, e := range ents {
		v := e.Ref.Version
		if v == "" {
			continue
		}
		bareInt := true
		for _, r := range v {
			if r < '0' || r > '9' {
				bareInt = false
				break
			}
		}
		if !bareInt {
			continue
		}
		// Build the dotted-sibling key for the identical tuple.
		dottedRef := e.Ref
		dottedRef.Version = v + ".0"
		if present[dottedRef.String()] {
			t.Errorf("entity %q coexists with its dotted sibling %q — the MERGE-only N->N.0 fold "+
				"must have collapsed them into one entity", e.Ref.String(), dottedRef.String())
		}
	}
}
