package bestiary_test

import (
	"encoding"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// creatorSeed is the UAT-confirmed Family→Creator mapping ("seed as listed"),
// asserted verbatim so a curation change to parse/data/creators.json that drops or
// re-points one of the nine labs is caught. VARIANT families (glm-air, gpt-mini, …)
// are deliberately absent from the seed and resolve to CreatorNone (see
// TestFamilyCreator_UnmappedIsNone).
var creatorSeed = map[bestiary.Family]bestiary.Creator{
	"llama":         bestiary.CreatorMeta,
	"claude":        bestiary.CreatorAnthropic,
	"claude-haiku":  bestiary.CreatorAnthropic,
	"claude-opus":   bestiary.CreatorAnthropic,
	"claude-sonnet": bestiary.CreatorAnthropic,
	"gemini":        bestiary.CreatorGoogle,
	"gemma":         bestiary.CreatorGoogle,
	"gpt":           bestiary.CreatorOpenAI,
	"o":             bestiary.CreatorOpenAI,
	"mistral":       bestiary.CreatorMistral,
	"codestral":     bestiary.CreatorMistral,
	"devstral":      bestiary.CreatorMistral,
	"command":       bestiary.CreatorCohere,
	"command-a":     bestiary.CreatorCohere,
	"command-r":     bestiary.CreatorCohere,
	"deepseek":      bestiary.CreatorDeepSeek,
	"qwen":          bestiary.CreatorAlibaba,
	"glm":           bestiary.CreatorZhipu,
}

// TestFamilyCreator_SeedMappings pins the nine UAT-confirmed lab mappings: each
// seeded family resolves through the curated creators.json to its expected Creator.
func TestFamilyCreator_SeedMappings(t *testing.T) {
	for fam, want := range creatorSeed {
		if got := fam.Creator(); got != want {
			t.Errorf("Family(%q).Creator() = %q, want %q", fam, got, want)
		}
	}
}

// TestFamilyCreator_UnmappedIsNone asserts an unmapped family — a variant family
// deliberately left out of the seed, and a nonsense family — resolves to the honest
// CreatorNone rather than a wrong-but-plausible guess.
func TestFamilyCreator_UnmappedIsNone(t *testing.T) {
	for _, fam := range []bestiary.Family{"glm-air", "gpt-mini", "gemini-flash", "qwen3.5", "totally-not-a-family", ""} {
		if got := fam.Creator(); got != bestiary.CreatorNone {
			t.Errorf("Family(%q).Creator() = %q, want CreatorNone", fam, got)
		}
	}
}

// TestValidateCreatorTable_RealSeedIsClean asserts the shipped creators.json passes
// the loud codegen guard: every family is IsKnown, no duplicates, no empty creator.
func TestValidateCreatorTable_RealSeedIsClean(t *testing.T) {
	if err := bestiary.ValidateCreatorTable(); err != nil {
		t.Fatalf("ValidateCreatorTable() on the shipped seed: %v", err)
	}
}

// TestCreator_TextCodecRoundTrips asserts Creator satisfies the text codec contract
// (MarshalText/UnmarshalText) and round-trips through its lowercase token, mirroring
// Provider. Unknown-but-non-empty tokens survive the round trip (open type); IsKnown
// classifies the well-known set.
func TestCreator_TextCodecRoundTrips(t *testing.T) {
	var _ encoding.TextMarshaler = bestiary.CreatorMeta
	var _ encoding.TextUnmarshaler = new(bestiary.Creator)

	for _, c := range append(bestiary.Creators(), bestiary.Creator("some-new-lab")) {
		b, err := c.MarshalText()
		if err != nil {
			t.Fatalf("Creator(%q).MarshalText: %v", c, err)
		}
		if string(b) != c.String() {
			t.Errorf("MarshalText = %q, want %q", b, c.String())
		}
		var back bestiary.Creator
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", b, err)
		}
		if back != c {
			t.Errorf("round trip = %q, want %q", back, c)
		}
	}
}

// TestCreator_IsKnownAndCreators asserts the well-known set is exactly the nine
// seeded labs, that Creators() returns a defensive copy, and that CreatorNone / an
// arbitrary token are NOT known.
func TestCreator_IsKnownAndCreators(t *testing.T) {
	all := bestiary.Creators()
	if len(all) != 9 {
		t.Fatalf("Creators() returned %d entries, want 9", len(all))
	}
	for _, c := range all {
		if !c.IsKnown() {
			t.Errorf("Creators() member %q reports !IsKnown()", c)
		}
	}
	// Defensive copy: mutating the result must not affect a second call.
	all[0] = bestiary.Creator("mutated")
	if bestiary.Creators()[0] == bestiary.Creator("mutated") {
		t.Error("Creators() did not return a defensive copy")
	}
	if bestiary.CreatorNone.IsKnown() {
		t.Error("CreatorNone.IsKnown() = true, want false")
	}
	if bestiary.Creator("unheard-of-lab").IsKnown() {
		t.Error("an arbitrary token reports IsKnown() = true, want false")
	}
}

// TestEntityCreator_ProjectionMatchesFamily asserts Entity.Creator is a faithful
// DERIVED projection: for every registry entity, Entity.Creator equals
// Ref.Family.Creator(). This is a runtime projection (loadEntityIndex), independent
// of the baked ModelInfo.Creator emission, so it holds regardless of codegen state.
func TestEntityCreator_ProjectionMatchesFamily(t *testing.T) {
	var sawMapped bool
	for _, e := range bestiary.Entities() {
		want := e.Ref.Family.Creator()
		if e.Creator != want {
			t.Errorf("entity %q: Creator = %q, want %q (= Ref.Family.Creator())", e.Ref.String(), e.Creator, want)
		}
		if want != bestiary.CreatorNone {
			sawMapped = true
		}
	}
	if !sawMapped {
		t.Error("no registry entity resolved to a non-None Creator; the projection or seed is not wired")
	}
}

// TestModelInfoCreator_BakedMatchesFamily asserts the baked ModelInfo.Creator agrees
// with the family-derived value for every static row — the codegen creatorExpr
// emission (L3) baking the derived value so the compiled registry and the store's
// creators dimension agree by construction. Before the L3 regen this fails for
// mapped families (baked Creator is empty); after regen it is green.
func TestModelInfoCreator_BakedMatchesFamily(t *testing.T) {
	for _, m := range bestiary.StaticModels() {
		if want := m.Family.Creator(); m.Creator != want {
			t.Errorf("static model %q (family %q): baked Creator = %q, want %q", m.ID, m.Family, m.Creator, want)
		}
	}
}
