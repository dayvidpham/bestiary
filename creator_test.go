package bestiary_test

import (
	"encoding"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// creatorSeed is the UAT-confirmed Family→Creator mapping ("seed as listed"),
// asserted verbatim so a curation change to parse/data/creators.json that drops or
// re-points one of these nine labs is caught. It is deliberately NOT the whole table
// — the table has since grown a lab-derived group and a hand-curated group — it is
// the original UAT set, held fixed as a regression floor. VARIANT families (glm-air,
// gpt-mini, …) remain absent and resolve to CreatorNone (see
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
// seeded family still resolves through the curated creators.json to its expected
// Creator after every later expansion of the table.
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

// TestCreator_IsKnownAndCreators pins the size of the well-known Creator set, checks
// that Creators() returns a defensive copy, and checks that CreatorNone / an
// arbitrary token are NOT known.
//
// The pin is DERIVED, not copied forward. The set has three provenance groups and the
// count is their sum, measured from this repository state:
//
//	  9  hand-curated seed labs (meta, openai, anthropic, google, mistral, cohere,
//	     deepseek, alibaba, zhipu)
//	+ 14  tokens from the models.dev lab-prefix derivation. The catalog carries 24
//	     distinct lab prefixes; 8 of them (alibaba, anthropic, cohere, deepseek,
//	     google, meta, mistral, openai) are already seed labs, "zhipuai" is a
//	     spelling variant of the seeded "zhipu" and is deliberately NOT applied, and
//	     "thinkingmachines" is withheld with the "ling" family it would attribute —
//	     24 − 8 − 1 − 1 = 14
//	+ 18  tokens hand-curated for families the metadata join never reaches
//	     (01ai, ai21, amazon, baai, baichuan, baidu, blackforestlabs, bytedance,
//	     elevenlabs, ibm, ideogram, nousresearch, recraft, reka, runway,
//	     stabilityai, upstage, voyageai)
//	= 41
//
// A later curation slice splits the inkling/ling/kling collision and adds the
// withheld originator, which moves this pin again; re-derive it there rather than
// adjusting the literal.
func TestCreator_IsKnownAndCreators(t *testing.T) {
	const (
		seedLabs        = 9
		labDerived      = 14
		curatedUnreach  = 18
		wantCreatorsLen = seedLabs + labDerived + curatedUnreach // 41
	)
	all := bestiary.Creators()
	if len(all) != wantCreatorsLen {
		t.Fatalf("Creators() returned %d entries, want %d (%d seed + %d lab-derived + %d curated-unreached)",
			len(all), wantCreatorsLen, seedLabs, labDerived, curatedUnreach)
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
