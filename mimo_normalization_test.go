package bestiary_test

import (
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// mimoProKey is the single key the three former spellings of Xiaomi's MiMo 2.5 Pro
// collapse onto.
const mimoProKey = "mimo@2.5{pro}"

// TestMimo_SeriesLetterSurvivesAsNomen is the falsifier for the user requirement behind
// the mimo re-key: the "V" leaves the entity KEY but stays part of the model's NAME.
//
// It needs no mechanism. Nomina are minted from the provider ids, and the provider ids
// spell the V, so the V survives wherever a user actually types it. No NomenSchemeAlias
// claim and no nomen_claims.json row is authored for this — a second source of truth for
// a fact the existing machinery already carries would be a liability, and under the
// epoch's retired-key policy none may be authored for a retired key anyway.
//
// The assertion is deliberately on the LIBRARY seam (Nomina) rather than only on the CLI:
// the CLI's resolver has several fallbacks, so a CLI-only test could stay green while the
// naming layer had silently dropped the V spelling.
func TestMimo_SeriesLetterSurvivesAsNomen(t *testing.T) {
	if _, ok := bestiary.EntityByKey(mimoProKey); !ok {
		t.Fatalf("EntityByKey(%q) does not resolve; the three former spellings of the 2.5 Pro model "+
			"were supposed to collapse onto this key", mimoProKey)
	}

	// value -> the schemes that carry it, restricted to the entity under test.
	byValue := map[string][]bestiary.NomenScheme{}
	for _, n := range bestiary.Nomina() {
		if n.ResolvesTo.String() != mimoProKey {
			continue
		}
		byValue[string(n.Value)] = append(byValue[string(n.Value)], n.Scheme)
	}

	// The Hub repo name is the canonical V spelling and must be present on BOTH the
	// huggingface leg (the harvested repo) and the provider-id leg (the served id).
	const hubRepo = "XiaomiMiMo/MiMo-V2.5-Pro"
	schemes, ok := byValue[hubRepo]
	if !ok {
		t.Fatalf("%q is not a nomen of %s. The user's requirement is that the V survives as part of "+
			"the NAME after leaving the key; if this repo no longer names the entity, the re-key "+
			"took the V away from the user rather than out of the key.", hubRepo, mimoProKey)
	}
	for _, want := range []bestiary.NomenScheme{
		bestiary.NomenSchemeHuggingFace,
		bestiary.NomenSchemeProviderID,
	} {
		if !containsScheme(schemes, want) {
			t.Errorf("%q names %s on schemes %v, missing %v", hubRepo, mimoProKey, schemes, want)
		}
	}

	// The served provider spellings carry the V too — this is the form a user types.
	for _, want := range []string{"mimo-v2.5-pro", "xiaomi/mimo-v2.5-pro"} {
		if _, ok := byValue[want]; !ok {
			t.Errorf("%q is not a nomen of %s; the served id spelling must survive the re-key",
				want, mimoProKey)
		}
	}

	// No alias claim may be minted to prop this up. The alias leg exists for exactly one
	// unrelated curated row epoch-wide, so any growth here means someone reached for a
	// redirect the policy forbids.
	for value, ss := range byValue {
		if containsScheme(ss, bestiary.NomenSchemeAlias) {
			t.Errorf("%s carries an ALIAS nomen %q; the V survives through the provider ids with no "+
				"claim needed, and the retired-key policy forbids minting one", mimoProKey, value)
		}
	}
}

// TestMimo_OnlyTheCanonicalNomenLegMoves pins the shape of the re-key's effect on the
// naming layer: a key rename moves the CANONICAL nomen (one per entity, derived from the
// key) and must leave every other leg untouched, because the other legs are minted from
// values — provider ids, curated aliases, harvested Hub repos — that a key rename does
// not touch. The three re-keyed huggingface_nomina rows keep their VALUES and only have
// their ResolvesTo re-pointed, which is why the huggingface leg does not move either.
//
// The exact totals are pinned by the census test; what this test adds is the CLAIM about
// which leg is allowed to move, asserted per-entity so a compensating error (one leg up,
// another down) cannot hide inside an unchanged total.
func TestMimo_OnlyTheCanonicalNomenLegMoves(t *testing.T) {
	perScheme := map[bestiary.NomenScheme]int{}
	for _, n := range bestiary.Nomina() {
		if !mimoFamilyKey(n.ResolvesTo.String()) {
			continue
		}
		perScheme[n.Scheme]++
	}

	// Nine surviving mimo keys, one canonical nomen each.
	if got := perScheme[bestiary.NomenSchemeCanonical]; got != 9 {
		t.Errorf("mimo canonical nomina = %d, want 9 (one per surviving key)", got)
	}
	// The three harvested Hub repos were re-keyed, not removed.
	if got := perScheme[bestiary.NomenSchemeHuggingFace]; got != 3 {
		t.Errorf("mimo huggingface nomina = %d, want 3 — the re-key re-points ResolvesTo and must "+
			"not add or drop a harvested repo", got)
	}
	if got := perScheme[bestiary.NomenSchemeAlias]; got != 0 {
		t.Errorf("mimo alias nomina = %d, want 0", got)
	}
	// Every served spelling carries across; none is minted or lost by a rename.
	// 40 distinct served spellings across the 93 instances (nomina de-duplicate by value,
	// so the same id offered by several providers is one nomen). Measured UNCHANGED across
	// the re-key: 40 before, 40 after, while the canonical leg went 10 -> 9.
	if got := perScheme[bestiary.NomenSchemeProviderID]; got != 40 {
		t.Errorf("mimo provider-id nomina = %d, want 40 — a rename carries every served id spelling "+
			"across as an Admitted nomen; a change here means spellings were created or dropped", got)
	}
}

// TestMimo_MergedProKeyCarriesBothMetadataRows is the witness for the multi-metadata
// slot doing the job it was landed for. Two models.dev rows decompose to this one entity
// — the canonical `xiaomi/mimo-v2.5-pro` and the speed-tier `xiaomi/mimo-v2.5-pro-ultraspeed`
// that the re-key folds into it — and a single-valued metadata slot would have kept
// whichever arrived last, silently discarding the canonical row's description, its
// benchmark claims and its link.
//
// The benchmark figures are asserted by VALUE, not by count: a count assertion stays
// green if the surviving row is the wrong one, which is precisely the failure this test
// exists to catch.
func TestMimo_MergedProKeyCarriesBothMetadataRows(t *testing.T) {
	var e bestiary.Entity
	var found bool
	for _, cand := range bestiary.Entities() {
		if cand.Ref.String() == mimoProKey {
			e, found = cand, true
			break
		}
	}
	if !found {
		t.Fatalf("no entity %s in the registry", mimoProKey)
	}

	byID := map[bestiary.MetadataID]bestiary.EntityMetadata{}
	for _, m := range e.MetadataAll {
		byID[m.MetadataID] = m
	}
	if len(e.MetadataAll) != 2 {
		t.Fatalf("%s carries %d metadata row(s), want 2 — the canonical row and the ultraspeed row "+
			"both decompose to this key, and both must survive the merge. Present: %v",
			mimoProKey, len(e.MetadataAll), sortedMetadataIDs(byID))
	}

	const canonicalID bestiary.MetadataID = "xiaomi/mimo-v2.5-pro"
	const ultraspeedID bestiary.MetadataID = "xiaomi/mimo-v2.5-pro-ultraspeed"
	canonical, ok := byID[canonicalID]
	if !ok {
		t.Fatalf("%s is missing metadata row %q; present: %v", mimoProKey, canonicalID, sortedMetadataIDs(byID))
	}
	if _, ok := byID[ultraspeedID]; !ok {
		t.Errorf("%s is missing metadata row %q; present: %v", mimoProKey, ultraspeedID, sortedMetadataIDs(byID))
	}

	// The derived primary must be the canonical row (shortest id wins), so the CLI's
	// single-row rendering shows the lab's description rather than the speed tier's.
	if e.Metadata == nil {
		t.Fatalf("%s has no primary metadata projection", mimoProKey)
	}
	if e.Metadata.MetadataID != canonicalID {
		t.Errorf("%s primary metadata is %q, want %q — the primary is the shortest id, and the "+
			"canonical row is what a user should see first", mimoProKey, e.Metadata.MetadataID, canonicalID)
	}
	if canonical.Description == "" {
		t.Errorf("the canonical metadata row for %s has an empty description; the merge kept the row "+
			"but lost its content", mimoProKey)
	}

	wantScores := map[string]float64{
		"SWE-Bench Verified": 78.9,
		"SWE-Bench Pro":      57.2,
		"GPQA Diamond":       86.6,
	}
	got := map[string]float64{}
	for _, b := range canonical.Benchmarks {
		got[b.Name] = b.Score
	}
	for name, want := range wantScores {
		score, ok := got[name]
		if !ok {
			t.Errorf("the canonical metadata row for %s has no %q benchmark; the merge dropped a "+
				"lab-reported claim", mimoProKey, name)
			continue
		}
		if score != want {
			t.Errorf("%s benchmark %q = %v, want %v", mimoProKey, name, score, want)
		}
	}
}

// mimoFamilyKey reports whether an entity key belongs to the mimo family, matching on the
// family segment rather than a raw prefix.
func mimoFamilyKey(key string) bool {
	if len(key) < 4 || key[:4] != "mimo" {
		return false
	}
	if len(key) == 4 {
		return true
	}
	switch key[4] {
	case '/', '@', '#', '{', '[':
		return true
	default:
		return false
	}
}

func containsScheme(ss []bestiary.NomenScheme, want bestiary.NomenScheme) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func sortedMetadataIDs(m map[bestiary.MetadataID]bestiary.EntityMetadata) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	sort.Strings(out)
	return out
}
