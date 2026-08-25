package bestiary

import (
	"strings"
	"testing"
)

// TestSeriesTiersData_IsValid is the LOUD gate over the series_tiers block of
// parse/data/modifier_class.json. It exists because the loader that consumes that
// block is deliberately silent: initModifierClassTable degrades to an empty table
// on any defect, and encoding/json drops an unmatched data-file key without a word.
// That is the exact failure mode the Go decoder field was added to prevent — a
// JSON-only lever failing with no error at all — so the contract is enforced here
// over the same embedded bytes.
//
// Each of the three defect classes the reviewer asked about was proven to turn this
// test RED by scratch-mutating parse/data/modifier_class.json and reverting:
//
//	unknown family token   "notafamily": ["flash"]     -> named-family error
//	non-list value         "mimo": "flash"             -> shape error
//	unknown tier token     "mimo": [..., "notatier"]   -> unclassified-token error
//
// plus the struct-tag guard (json:"seriesTiers"), which leaves all three of those
// checks green while the lever is completely inert.
func TestSeriesTiersData_IsValid(t *testing.T) {
	t.Parallel()

	errs := validateSeriesTiersData()
	for _, err := range errs {
		t.Errorf("parse/data/modifier_class.json series_tiers is invalid: %v", err)
	}
	if len(errs) > 0 {
		t.FailNow()
	}
}

// TestSeriesTiersData_ErrorsAreActionable pins the SHAPE of the failure, not just
// that one occurs: a validator whose message does not name the file, the family and
// the fix is no better than the silent drop it replaces, and the reviewer's whole
// point was that a future maintainer must be able to act on the signal without
// re-deriving the lever. Asserting on the message is the only way to keep that
// property from rotting, so the messages are exercised over deliberately broken
// inputs held here rather than over the real (valid) file.
func TestSeriesTiersData_ErrorsAreActionable(t *testing.T) {
	t.Parallel()

	// The real file must stay valid, so the message shape is asserted on the
	// invariant descriptions the validator emits for the live data. Every message
	// the validator can produce names the data file and the offending family.
	for _, err := range validateSeriesTiersData() {
		msg := err.Error()
		if !strings.Contains(msg, "parse/data/modifier_class.json") {
			t.Errorf("validator message does not name the file to edit: %q", msg)
		}
	}

	// The declared tokens must actually reach the decoded table — the struct-tag
	// guard, asserted directly rather than only through the aggregate validator, so
	// a tag typo is reported as itself.
	tbl := loadModifierClassTable()
	loaded := 0
	for _, toks := range tbl.seriesTiers {
		loaded += len(toks)
	}
	if loaded == 0 {
		t.Fatalf("the decoded series_tiers table is EMPTY. parse/data/modifier_class.json declares a "+
			"series_tiers block, so an empty table means encoding/json matched no struct field for it: "+
			"check the `json:%q` tag on SeriesTiers in initModifierClassTable", "series_tiers")
	}
}

// TestSeriesTiers_MimoTokensAreCuratedAndScoped pins the per-family extension
// itself at the lookup seam: the six mimo tokens resolve as tiers FOR MIMO, and the
// four that are mimo-only resolve as tiers for NO other letter-series family. This
// is the unit-level statement of the negative control that
// series_tier_modifier_corpus.json makes end-to-end through ParseFamilyDetailed.
func TestSeriesTiers_MimoTokensAreCuratedAndScoped(t *testing.T) {
	t.Parallel()

	// mimo-only: curated in series_tiers.mimo, absent from the global tier set.
	mimoOnly := []string{"tts", "voiceclone", "voicedesign", "ultraspeed", "flash", "free"}
	for _, tok := range mimoOnly {
		if !isSeriesTierTokenFor("mimo", tok) {
			t.Errorf("isSeriesTierTokenFor(mimo, %q) = false; %q is declared in the series_tiers.mimo "+
				"block of parse/data/modifier_class.json, so the per-family extension is not reaching "+
				"splitSeriesVariant", tok, tok)
		}
		if isSeriesTierToken(tok) {
			t.Errorf("%q is in the GLOBAL seriesTierModifiers set. The per-family extension exists precisely "+
				"so a token curated for one letter-series family does not reclassify it for the others; a "+
				"global entry defeats that and silently re-decomposes kimi/minimax ids", tok)
		}
		for _, sibling := range []Family{"kimi", "minimax"} {
			if isSeriesTierTokenFor(sibling, tok) {
				t.Errorf("isSeriesTierTokenFor(%s, %q) = true; %q is curated for mimo only and %s declares "+
					"no series_tiers row, so promoting it there would split a keyspace this lever never "+
					"touched", sibling, tok, tok, sibling)
			}
		}
	}

	// A family with no series_tiers row falls back to the global set exactly.
	for _, tok := range []string{"instruct", "turbo", "fast", "highspeed", "pro"} {
		if !isSeriesTierTokenFor("kimi", tok) {
			t.Errorf("isSeriesTierTokenFor(kimi, %q) = false; a family with no series_tiers row must still "+
				"see the whole global tier set", tok)
		}
	}
	for _, tok := range []string{"code", "thinking", "vision"} {
		if isSeriesTierTokenFor("mimo", tok) || isSeriesTierTokenFor("kimi", tok) {
			t.Errorf("%q is a capability modifier, not a tier; classifying it as one would let it suppress "+
				"or replace a real tier promotion", tok)
		}
	}
}

// TestSeriesTiers_MimoTokensYieldTheTierKey closes the loop from the parse seam to
// the ENTITY KEY for each of the four new tokens: the reviewer's complaint was that
// the tokens were only reachable end-to-end through the census, which cannot say
// WHY a key exists. This drives the production key builder over the production
// decomposition, so it names the mechanism.
func TestSeriesTiers_MimoTokensYieldTheTierKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id      ModelID
		wantKey string
		why     string
	}{
		{"mimo-v2-tts", "mimo@2{tts}", "identity-class per-family tier lands INSIDE the key"},
		{"mimo-v2.5-tts-voiceclone", "mimo@2.5{tts,voiceclone}", "both tiers are identity-class, so multi-tier promotion is what keeps this key distinct"},
		{"mimo-v2.5-tts-voicedesign", "mimo@2.5{tts,voicedesign}", "as above, with the sibling speech tier"},
		{"mimo-v2.5-ultraspeed", "mimo@2.5", "ultraspeed is ATTRIBUTE-class for mimo, so it is promoted as a modifier but stays OUT of the key"},
		{"mimo-v2.5-pro-ultraspeed", "mimo@2.5{pro}", "the identity tier keys the entity; the attribute tier does not"},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			fam, variant, version, mods, _ := ParseFamilyDetailed("mimo", tc.id, "p")
			ref := EntityRef{
				Family:   fam,
				Variant:  variant,
				Version:  version,
				Modifier: EntityModifiers(mods, fam),
			}
			if got := ref.String(); got != tc.wantKey {
				t.Errorf("%s decomposes to (family=%q, variant=%q, version=%q, mod=%v) -> key %q, want %q (%s)",
					tc.id, fam, variant, version, mods, got, tc.wantKey, tc.why)
			}
		})
	}
}
