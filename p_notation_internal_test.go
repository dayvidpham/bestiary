package bestiary

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
)

// TestPNotationVersion_Corpus drives the "p"-as-dot version decode at both levels
// from one corpus: the unit rows probe decodePNotationVersion directly, and the
// catalog rows pin every digit-p-digit model id in the committed registry to the
// entity key it resolves to.
//
// Both levels matter and neither substitutes for the other. The unit rows fence the
// SHAPE (what may and may not decode); the catalog rows fence the OUTCOME (that the
// fireworks GLM rows land in the real glm@5.1 / glm@5.2 entities rather than minting
// phantoms beside them), which is what the ruling actually asked for.
func TestPNotationVersion_Corpus(t *testing.T) {
	corpus := loadInternalCorpus[pNotationInput, pNotationExpected](t, internalPNotationVersionCorpusJSON, 26)

	// Value coverage: the two rows the ruling is ABOUT, the two known residual
	// defects, and the synthetic literal-p hazard must all still be present.
	requirePNotationCoverage(t, corpus, []pNotationInput{
		{ID: "accounts/fireworks/models/glm-5p1", Provider: "fireworks-ai"},
		{ID: "accounts/fireworks/models/glm-5p2", Provider: "fireworks-ai"},
		{ID: "k2p7", Provider: "kimi-for-coding"},
		{ID: "accounts/fireworks/models/qwen3p7-plus", Provider: "fireworks-ai"},
		{ID: "qwen3-coder", Provider: "helicone"},
		{Token: "3px"},
		{Token: "120b"},
	})

	units, catalog := 0, 0
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Input.Token != "" {
				units++
				got, ok := decodePNotationVersion(c.Input.Token)
				wantOK := c.Classification == testcase.MustPass
				if ok != wantOK {
					t.Fatalf("decodePNotationVersion(%q) ok = %v, want %v", c.Input.Token, ok, wantOK)
				}
				if got != c.Expected.Decoded {
					t.Errorf("decodePNotationVersion(%q) = %q, want %q", c.Input.Token, got, c.Expected.Decoded)
				}
				return
			}
			catalog++
			m, ok := LookupModelByProvider(Provider(c.Input.Provider), c.Input.ID)
			if !ok {
				t.Fatalf("LookupModelByProvider(%q, %q) = false; the pinned catalog row is gone",
					c.Input.Provider, c.Input.ID)
			}
			ref := EntityRef{
				Family:    m.Family,
				Variant:   m.Variant,
				Version:   m.Version,
				ParamSize: m.ParamSize,
				Modifier:  EntityModifiers(m.Modifier, m.Family),
			}
			if got := ref.String(); got != c.Expected.EntityKey {
				t.Errorf("%q (%s) resolves to entity %q, want %q",
					c.Input.ID, c.Input.Provider, got, c.Expected.EntityKey)
			}
			// The entity must really exist in the registry, so a row cannot pass by
			// computing a key nothing is filed under.
			if _, ok := EntityByKey(c.Expected.EntityKey); !ok {
				t.Errorf("entity %q is absent from the registry", c.Expected.EntityKey)
			}
		})
	}
	if units == 0 || catalog == 0 {
		t.Errorf("corpus lost a whole level: %d unit rows, %d catalog rows", units, catalog)
	}
}

// requirePNotationCoverage asserts each probe input is still present in the corpus by
// value — the guard the exact case count cannot provide, since a count-preserving swap
// could drop one of the ruled rows and add a filler.
func requirePNotationCoverage(t *testing.T, corpus testcase.Corpus[pNotationInput, pNotationExpected], probes []pNotationInput) {
	t.Helper()
	for _, p := range probes {
		found := false
		for _, c := range corpus.Cases {
			if c.Input == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("value coverage lost: no corpus case for %+v", p)
		}
	}
}

// reDigitPDigitID matches a digit-p-digit sequence anywhere in a model id — the
// shape the decode reacts to.
var reDigitPDigitID = regexp.MustCompile(`[0-9]p[0-9]`)

// TestPNotation_CatalogCensus is the CONFINEMENT sweep over the built registry: it
// counts every digit-p-digit model id and asserts the set is exactly the size and the
// provider scope the corpus pins.
//
// Its rows are computed from the catalog rather than authored, so it stays inline per
// TESTING.md. Its job is the one the corpus cannot do: catch a NEW p-notation id
// appearing (from these providers or, more importantly, from a third one) that no
// corpus row covers, so the decode's blast radius can never widen unnoticed.
func TestPNotation_CatalogCensus(t *testing.T) {
	const (
		wantIDs             = 14
		wantFireworks       = 11
		wantKimiForCoding   = 3
		wantUndecodedInKeys = 0
	)
	byProvider := map[Provider]int{}
	total := 0
	for _, m := range StaticModels() {
		if !reDigitPDigitID.MatchString(string(m.ID)) {
			continue
		}
		total++
		byProvider[m.Provider]++
		// The decoded version must never carry the p forward into identity.
		if reDigitPDigitID.MatchString(m.Version) {
			t.Errorf("model %q (%s) kept an undecoded p-notation version %q",
				m.ID, m.Provider, m.Version)
		}
	}
	if total != wantIDs {
		t.Errorf("catalog holds %d digit-p-digit ids, want exactly %d — a new one appeared or "+
			"one vanished; add or retire its corpus row in the same commit", total, wantIDs)
	}
	if byProvider["fireworks-ai"] != wantFireworks || byProvider["kimi-for-coding"] != wantKimiForCoding {
		t.Errorf("p-notation ids by provider = %v, want fireworks-ai=%d kimi-for-coding=%d",
			byProvider, wantFireworks, wantKimiForCoding)
	}
	// CONFINEMENT: no THIRD provider may ship the spelling without this failing, which
	// is what keeps the decode's reach reviewable.
	for p, n := range byProvider {
		if p != "fireworks-ai" && p != "kimi-for-coding" {
			t.Errorf("provider %q now ships %d digit-p-digit id(s); the spelling has spread beyond "+
				"the two reviewed providers and needs a fresh look", p, n)
		}
	}

	// No ENTITY KEY may carry an undecoded p-notation version — the phantom-entity
	// failure mode the fix exists to remove, asserted over the whole registry rather
	// than only the ids the corpus names.
	stranded := 0
	for _, e := range Entities() {
		if reDigitPDigitID.MatchString(e.Ref.Version) {
			stranded++
			t.Errorf("entity %q carries an undecoded p-notation version", e.Ref.String())
		}
	}
	if stranded != wantUndecodedInKeys {
		t.Errorf("%d entities carry a p-notation version, want %d", stranded, wantUndecodedInKeys)
	}
}

// TestPNotation_LiteralPTokensUntouched is the negative control over the real
// catalog: ids containing a "p" that is NOT flanked by digits must be unaffected by
// the decode. It samples the two reviewed providers' own non-p-notation ids, so the
// control is drawn from exactly the population most at risk.
func TestPNotation_LiteralPTokensUntouched(t *testing.T) {
	checked := 0
	for _, m := range StaticModels() {
		if m.Provider != "fireworks-ai" && m.Provider != "kimi-for-coding" {
			continue
		}
		id := strings.ToLower(string(m.ID))
		if !strings.Contains(id, "p") || reDigitPDigitID.MatchString(id) {
			continue
		}
		checked++
		if strings.Contains(m.Version, ".") && !strings.Contains(id, ".") && !strings.Contains(id, "-") {
			t.Errorf("id %q has no digit-p-digit shape yet gained a dotted version %q", m.ID, m.Version)
		}
	}
	if checked == 0 {
		t.Fatal("no literal-p control ids found among the two providers; the control is vacuous")
	}
}
