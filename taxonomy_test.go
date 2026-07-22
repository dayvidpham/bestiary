package bestiary_test

import (
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// seriesSet renders a Series slice as a lookup set keyed by the (family, generation)
// pair — the identity of a line — for membership assertions below.
func seriesSet(in []bestiary.Series) map[bestiary.Series]bool {
	out := map[bestiary.Series]bool{}
	for _, s := range in {
		out[s] = true
	}
	return out
}

// releaseNames returns the release names of a Series in the order ReleasesOf
// returned them (ascending by name; the bare-line "" first).
func releaseNames(in []bestiary.Release) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		out = append(out, r.Name)
	}
	return out
}

// entityKeysOf renders the canonical keys of an Entity slice in returned order.
func entityKeysOf(in []bestiary.Entity) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		out = append(out, e.Ref.String())
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSeriesAll_CensusExact pins the EXACT Series census over the static registry,
// with the plan's floor as defense-in-depth.
//
// The count is the ALL-PAIRS census: every distinct (family, generation) line,
// INCLUDING the lines whose generation is empty (families whose entities carry no
// identity version). It is reconciled from the raw pair count so a future drift is
// attributable rather than merely "different":
//
//	427 raw (family, identity-version) pairs over the 971 registry entities
//	 -6 bare/dotted generation folds (the N + N.0 sibling collapses; see
//	    TestSeries_GenerationNormalization_CensusExact)
//	 -3 curated strays folded into an existing line (parse/data/series.json)
//	=418 Series
//
// This is an exact pin, not a floor: a change to the line count is a deliberate act
// (a catalog refresh, a stray row, a normalization change) that must move this
// literal in the same commit, so silent drift is caught.
//
// 422 → 421 when the curated eva and command-a-plus overrides landed. Both retired a
// compound-family line, and only one of them created a new one, so the split moved
// -2 bare / +1 versioned:
//   - "qwen2.5-32b-eva" (bare line, its only entity re-keyed) → "eva" generation 0.2,
//     a NEW versioned line;
//   - "command-a-plus" (bare line) → absorbed into the existing "command" line as the
//     a-plus variant, adding no line.
//
// 421 → 418 when the curated cortecs pins landed, all three from the versioned side
// (217 → 214, bare unchanged). The pins retired FOUR phantom entities but only THREE
// lines: claude/opus@5…@8 were the sole occupants of the claude-6, claude-7 and
// claude-8 lines, which vanish with them — but claude-5 SURVIVES, because the real
// Claude 5 line is populated by claude/sonnet@5. Entity count and line count move by
// different amounts here, which is exactly why both are pinned.
func TestSeriesAll_CensusExact(t *testing.T) {
	const (
		wantSeries        = 418
		wantVersionLines  = 214 // lines with a non-empty generation
		wantBareLines     = 204 // lines whose entities carry no identity version
		minExpectedSeries = 300 // the ratified floor
	)
	all := bestiary.SeriesAll()
	if len(all) != wantSeries {
		t.Errorf("SeriesAll() = %d series, want exactly %d — update this census literal "+
			"in the same commit if the line count changed intentionally", len(all), wantSeries)
	}
	if len(all) < minExpectedSeries {
		t.Errorf("SeriesAll() = %d series, below the ratified floor of %d", len(all), minExpectedSeries)
	}
	var versioned, bare int
	for _, s := range all {
		if s.Generation == "" {
			bare++
		} else {
			versioned++
		}
	}
	if versioned != wantVersionLines || bare != wantBareLines {
		t.Errorf("SeriesAll() split = %d versioned / %d bare, want %d / %d",
			versioned, bare, wantVersionLines, wantBareLines)
	}
}

// TestReleases_CensusExact pins the EXACT Release census — the second half of the
// hierarchy's size, sibling to the Series literal above.
//
// A Release is one (line, variant-name) pair, so the total counts every named
// member of every line PLUS each line's un-named bare release. It is measured two
// independent ways here (summing ReleasesOf over SeriesAll, and counting the
// distinct releases the entities themselves map to via ReleaseOf) because the two
// disagree if the release index and the entity mapping ever drift apart — a single
// summation would pin the number without proving it consistent.
//
// The literal is exact for the same reason the Series literal is: it moves only by
// a deliberate act. Note it is sensitive to re-keys that the Series count is NOT —
// making an entity a named member (as the maverick member-ize did) adds a Release
// without adding a Series, since the line already existed.
func TestReleases_CensusExact(t *testing.T) {
	const wantReleases = 664

	summed := 0
	for _, s := range bestiary.SeriesAll() {
		summed += len(bestiary.ReleasesOf(s))
	}
	if summed != wantReleases {
		t.Errorf("sum of ReleasesOf over SeriesAll() = %d, want exactly %d — "+
			"update this census literal in the same commit if the release count changed intentionally",
			summed, wantReleases)
	}

	// Independent method: the distinct releases reached from the entity side.
	distinct := map[bestiary.Release]bool{}
	for _, e := range bestiary.Entities() {
		distinct[bestiary.ReleaseOf(e.Ref)] = true
	}
	if len(distinct) != wantReleases {
		t.Errorf("distinct ReleaseOf over Entities() = %d, want %d — the release index and the "+
			"entity mapping disagree", len(distinct), wantReleases)
	}

	// The llama-4 line carries three releases (bare, maverick, scout); the maverick
	// one exists only because of the member-ize re-key, and is the difference between
	// this census and its pre-re-key value.
	if got := len(bestiary.ReleasesOf(bestiary.Series{Family: "llama", Generation: "4"})); got != 3 {
		t.Errorf("llama-4 has %d releases, want 3 (bare, maverick, scout)", got)
	}
}

// TestSeriesAll_SortedDeduplicatedDeterministic verifies the ordering contract:
// strictly ascending by (Family, Generation) — which also proves de-duplication —
// and byte-identical across two calls (no map-iteration order leaking out).
func TestSeriesAll_SortedDeduplicatedDeterministic(t *testing.T) {
	a := bestiary.SeriesAll()
	b := bestiary.SeriesAll()
	if len(a) != len(b) {
		t.Fatalf("two SeriesAll() calls differ in length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("SeriesAll() nondeterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
		if i == 0 {
			continue
		}
		prev, cur := a[i-1], a[i]
		if prev.Family > cur.Family || (prev.Family == cur.Family && prev.Generation >= cur.Generation) {
			t.Fatalf("SeriesAll() not strictly ascending at %d: %+v then %+v", i, prev, cur)
		}
	}
}

// TestTaxonomy_DefensiveCopies verifies that mutating any returned slice cannot
// corrupt the memoized taxonomy index — the Entities()/ProvidersOf copy discipline.
func TestTaxonomy_DefensiveCopies(t *testing.T) {
	first := bestiary.SeriesAll()
	if len(first) == 0 {
		t.Fatal("SeriesAll() is empty")
	}
	want := first[0]
	first[0] = bestiary.Series{Family: "mutated", Generation: "9"}
	if got := bestiary.SeriesAll()[0]; got != want {
		t.Errorf("SeriesAll() leaked its backing array: after mutation got %+v, want %+v", got, want)
	}

	line := bestiary.Series{Family: "llama", Generation: "4"}
	rels := bestiary.ReleasesOf(line)
	if len(rels) == 0 {
		t.Fatal("ReleasesOf(llama-4) is empty")
	}
	wantRel := rels[0]
	rels[0] = bestiary.Release{Series: line, Name: "mutated"}
	if got := bestiary.ReleasesOf(line)[0]; got != wantRel {
		t.Errorf("ReleasesOf leaked its backing array: got %+v, want %+v", got, wantRel)
	}

	scout := bestiary.Release{Series: line, Name: "scout"}
	ents := bestiary.EntitiesOf(scout)
	if len(ents) == 0 {
		t.Fatal("EntitiesOf(llama-4/scout) is empty")
	}
	wantKey := ents[0].Ref.String()
	ents[0].Ref.Family = "mutated"
	ents[0].Providers = nil
	if got := bestiary.EntitiesOf(scout)[0].Ref.String(); got != wantKey {
		t.Errorf("EntitiesOf returned an aliased entity: got %q, want %q", got, wantKey)
	}
}

// TestSeries_GenerationNormalization_Gemini is the ratified gemini ruling: the raw
// versions "3" and "3.0" are one generation, so both spellings map into the single
// Series{gemini, "3.0"} — and the ENTITY KEYS ARE UNTOUCHED on both sides.
func TestSeries_GenerationNormalization_Gemini(t *testing.T) {
	set := seriesSet(bestiary.SeriesAll())
	normalized := bestiary.Series{Family: "gemini", Generation: "3.0"}
	raw := bestiary.Series{Family: "gemini", Generation: "3"}
	if !set[normalized] {
		t.Errorf("SeriesAll() is missing the normalized line %+v", normalized)
	}
	if set[raw] {
		t.Errorf("SeriesAll() still exposes the un-normalized line %+v; '3' must fold into '3.0'", raw)
	}
	if got := bestiary.ReleasesOf(raw); got != nil {
		t.Errorf("ReleasesOf(gemini-3) = %v, want nil (the line folded into gemini-3.0)", got)
	}

	// Both spellings compute to the SAME Series...
	for _, key := range []string{"gemini/flash@3", "gemini/flash@3.0"} {
		e, ok := bestiary.EntityByKey(key)
		if !ok {
			t.Fatalf("EntityByKey(%q) = false; the fixture entity is missing from the registry", key)
		}
		if got := bestiary.SeriesOf(e.Ref); got != normalized {
			t.Errorf("SeriesOf(%q) = %+v, want %+v", key, got, normalized)
		}
		// ...and neither key moved: normalization is a SERIES-level view only.
		if got := e.Ref.String(); got != key {
			t.Errorf("entity key changed under normalization: %q != %q", got, key)
		}
	}

	// The flash release of the normalized line holds BOTH spellings' entities.
	flash := bestiary.Release{Series: normalized, Name: "flash"}
	wantKeys := []string{"gemini/flash@3", "gemini/flash@3.0"}
	if got := entityKeysOf(bestiary.EntitiesOf(flash)); !equalStrings(got, wantKeys) {
		t.Errorf("EntitiesOf(gemini-3.0/flash) = %v, want %v", got, wantKeys)
	}
}

// TestSeries_GenerationNormalization_CensusExact pins the FULL set of bare/dotted
// generation folds at this bake — the gemini ruling generalized to its rule: a bare
// "N" folds into "N.0" only when the same family also spells "N.0".
//
// Pinning the complete set (not just gemini) is what makes the rule falsifiable: a
// change in scope — a new collision appearing, or an existing one silently ceasing
// to fold — moves this literal.
func TestSeries_GenerationNormalization_CensusExact(t *testing.T) {
	// Every (family, bare) pair that folds; the target is always bare + ".0".
	wantFolds := []bestiary.Series{
		{Family: "claude", Generation: "4"},
		{Family: "gemini", Generation: "2"},
		{Family: "gemini", Generation: "3"},
		{Family: "imagen", Generation: "4"},
		{Family: "qwen", Generation: "2"},
		{Family: "veo", Generation: "3"},
	}
	set := seriesSet(bestiary.SeriesAll())
	for _, folded := range wantFolds {
		if set[folded] {
			t.Errorf("Series %+v should have folded into generation %q", folded, folded.Generation+".0")
		}
		target := bestiary.Series{Family: folded.Family, Generation: folded.Generation + ".0"}
		if !set[target] {
			t.Errorf("fold target %+v is missing from SeriesAll()", target)
		}
	}

	// NEGATIVE CONTROL: llama spells only "4" (no "4.0" sibling), so it must NOT be
	// renamed — an unconditional N -> N.0 rule would break the ratified "llama-4".
	if !set[bestiary.Series{Family: "llama", Generation: "4"}] {
		t.Error("Series{llama, 4} is missing: a family with no dotted sibling must keep its bare generation")
	}
	if set[bestiary.Series{Family: "llama", Generation: "4.0"}] {
		t.Error("Series{llama, 4.0} exists: the fold must be conditional on a real dotted sibling")
	}

	// Count the folds structurally too, so a NEW collision cannot appear unnoticed.
	byFamily := map[bestiary.Family]map[string]bool{}
	for _, s := range set2slice(set) {
		if byFamily[s.Family] == nil {
			byFamily[s.Family] = map[string]bool{}
		}
		byFamily[s.Family][s.Generation] = true
	}
	for fam, gens := range byFamily {
		for g := range gens {
			if isDigits(g) && gens[g+".0"] {
				t.Errorf("family %q still exposes both %q and %q: the fold did not apply", fam, g, g+".0")
			}
		}
	}
}

func set2slice(in map[bestiary.Series]bool) []bestiary.Series {
	out := make([]bestiary.Series, 0, len(in))
	for s := range in {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Family != out[j].Family {
			return out[i].Family < out[j].Family
		}
		return out[i].Generation < out[j].Generation
	})
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// TestSeries_CuratedStrays verifies the three curated stray rows re-home their
// families onto the right line — and that re-homing never touched an entity key.
func TestSeries_CuratedStrays(t *testing.T) {
	gemma4 := bestiary.Series{Family: "gemma", Generation: "4"}
	gemini := bestiary.Series{Family: "gemini", Generation: ""}
	cases := []struct {
		entityKey   string
		wantSeries  bestiary.Series
		wantRelease string
	}{
		{"gemma4#31b", gemma4, ""},
		{"gemma-4-31b-larkspur/v0.5@4#31b", gemma4, "larkspur"},
		{"gemini-exp", gemini, "exp"},
	}
	for _, tc := range cases {
		e, ok := bestiary.EntityByKey(tc.entityKey)
		if !ok {
			t.Errorf("EntityByKey(%q) = false; the stray fixture is missing from the registry", tc.entityKey)
			continue
		}
		if got := bestiary.SeriesOf(e.Ref); got != tc.wantSeries {
			t.Errorf("SeriesOf(%q) = %+v, want %+v", tc.entityKey, got, tc.wantSeries)
		}
		if got := bestiary.ReleaseOf(e.Ref).Name; got != tc.wantRelease {
			t.Errorf("ReleaseOf(%q).Name = %q, want %q", tc.entityKey, got, tc.wantRelease)
		}
		// The stray family must NOT survive as a line of its own.
		if seriesSet(bestiary.SeriesAll())[bestiary.Series{Family: e.Ref.Family, Generation: e.Ref.Version}] &&
			e.Ref.Family != tc.wantSeries.Family {
			t.Errorf("stray family %q still forms its own Series", e.Ref.Family)
		}
		// And the key itself is untouched by the re-homing.
		if got := e.Ref.String(); got != tc.entityKey {
			t.Errorf("stray entity key changed: %q != %q", got, tc.entityKey)
		}
	}
	// phi needs no curated row: its generations decompose correctly.
	for _, g := range []string{"3", "3.5", "4"} {
		if !seriesSet(bestiary.SeriesAll())[bestiary.Series{Family: "phi", Generation: g}] {
			t.Errorf("Series{phi, %q} missing: phi must compute without a curated stray row", g)
		}
	}
}

// TestTaxonomy_PartitionsEveryEntity is the structural fence over the WHOLE registry:
// the Series/Release hierarchy is a partition — every entity belongs to exactly one
// release, that release is listed under its series, that series is in SeriesAll, and
// EntitiesOf finds the entity again. A taxonomy that silently dropped or duplicated
// entities fails here rather than in a census literal.
func TestTaxonomy_PartitionsEveryEntity(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("Entities() is empty")
	}
	series := seriesSet(bestiary.SeriesAll())

	covered := 0
	for _, s := range bestiary.SeriesAll() {
		for _, r := range bestiary.ReleasesOf(s) {
			if r.Series != s {
				t.Errorf("ReleasesOf(%+v) returned a release of a different series: %+v", s, r.Series)
			}
			covered += len(bestiary.EntitiesOf(r))
		}
	}
	if covered != len(entities) {
		t.Errorf("releases cover %d entities, want %d (the hierarchy must partition the registry)",
			covered, len(entities))
	}

	for _, e := range entities {
		rel := bestiary.ReleaseOf(e.Ref)
		if !series[rel.Series] {
			t.Fatalf("entity %q maps to Series %+v which is absent from SeriesAll()", e.Ref.String(), rel.Series)
		}
		found := false
		for _, r := range bestiary.ReleasesOf(rel.Series) {
			if r == rel {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("entity %q maps to Release %+v which is absent from ReleasesOf(%+v)",
				e.Ref.String(), rel, rel.Series)
		}
		if e.Ref.String() != mustFindKey(bestiary.EntitiesOf(rel), e.Ref.String()) {
			t.Fatalf("entity %q is not returned by EntitiesOf(%+v)", e.Ref.String(), rel)
		}
	}
}

func mustFindKey(in []bestiary.Entity, key string) string {
	for _, e := range in {
		if e.Ref.String() == key {
			return key
		}
	}
	return ""
}

// TestTaxonomy_UnknownLookups verifies the absent-match contract: an unknown Series
// or Release is a normal negative (nil), never an error or a panic.
func TestTaxonomy_UnknownLookups(t *testing.T) {
	unknown := bestiary.Series{Family: "definitely-not-a-family", Generation: "99"}
	if got := bestiary.ReleasesOf(unknown); got != nil {
		t.Errorf("ReleasesOf(unknown) = %v, want nil", got)
	}
	if got := bestiary.EntitiesOf(bestiary.Release{Series: unknown, Name: "nope"}); got != nil {
		t.Errorf("EntitiesOf(unknown) = %v, want nil", got)
	}
	// An unregistered ref still COMPUTES its line (the taxonomy is a function of the
	// key components, not of registry membership).
	ref := bestiary.EntityRef{Family: "definitely-not-a-family", Variant: "x", Version: "1"}
	if got, want := bestiary.SeriesOf(ref), (bestiary.Series{Family: "definitely-not-a-family", Generation: "1"}); got != want {
		t.Errorf("SeriesOf(unregistered) = %+v, want %+v", got, want)
	}
	if got, want := bestiary.ReleaseOf(ref).Name, "x"; got != want {
		t.Errorf("ReleaseOf(unregistered).Name = %q, want %q", got, want)
	}
}

// TestSeriesRelease_StringRendering pins the display renderings, including the
// bare-line and un-named-release cases.
func TestSeriesRelease_StringRendering(t *testing.T) {
	corpus := loadParseCorpus[entReleaseInput, string](t, entSeriesReleaseStringCorpusJSON, 5)
	requireInputCoverage(t, corpus, map[entReleaseInput]string{
		{Family: "llama", Generation: "4", Name: "scout"}:    "llama-4/scout",
		{Family: "gemini", Generation: "3.0", Name: "flash"}: "gemini-3.0/flash",
		{Family: "gemma"}: "gemma",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			rel := bestiary.Release{
				Series: bestiary.Series{Family: bestiary.Family(c.Input.Family), Generation: c.Input.Generation},
				Name:   c.Input.Name,
			}
			if got := rel.String(); got != c.Expected {
				t.Errorf("Release.String() = %q, want %q", got, c.Expected)
			}
		})
	}
}
