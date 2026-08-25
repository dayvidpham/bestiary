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
//	430 raw (family, identity-version) pairs over the 982 registry entities
//	 -6 bare/dotted generation folds (the N + N.0 sibling collapses; see
//	    TestSeries_GenerationNormalization_CensusExact)
//	 -3 curated strays folded into an existing line (parse/data/series.json)
//	=421 Series
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
//
// 418 → 421 with the family-"o" over-capture fix, +4 versioned / -1 bare -> +3 net:
//   - arrow-1.1, rerank-2.5, tts-1 (three NEW versioned lines)
//   - wan (a new BARE line; the wan rows carry variants, not identity versions)
//   - voyage (its only two entities were voyage-labelled rerankers, which the rerank
//     enforce entry re-homes onto the rerank line — the vendor-leak correction)
//
// The rerank BARE line already existed (nvidia/rerank-qa-mistral-4b), so it is not new.
//
// 419 → 418 with the o-series dual-identity fix (-1 bare / versioned unchanged): the
// digitalocean openai-o1 / openai-o3 / openai-o3-mini rows canonicalize to the EXISTING
// gpt/o@1, gpt/o@3, gpt/o@3{mini} entities, which vacates the family-"o" bare line (its
// only occupants). No versioned line is added (the gpt/o generations already existed).
//
// 418 → 411 with the dot-lost version repair + 1t param-size routing (-8 versioned / +1
// bare): the dot-lost merges fold whole versioned qwen/minimax lines (e.g. the dotless
// minimax/m@25, qwen@35 lines) into their dotted siblings, retiring 8 versioned lines;
// 1t routing empties the ling@1t/ring@1t VERSIONS (1t is now a size), so ling and ring
// become bare lines — ring already had a bare presence, ling adds one, net +1 bare.
// 417 → 415 with the global free demotion (−2 bare / versioned UNCHANGED): of the 17
// retired free-tier keys, 15 shared a family with a surviving sibling, so their lines
// stayed populated. Two families existed ONLY through their free key — deepseek-flash
// (deepseek-flash/free) and minimax-m3 (minimax-m3/free) — and their bare lines empty out
// with them. Every versioned line touched (glm@4.7 / @5 / @5.2, hy@3, laguna-s@2.1,
// nemotron@3) keeps other entities, so no versioned line retires.
// 415 → 417 with the ling/inkling/kling collision split (+2 bare / versioned UNCHANGED):
// two new bare lines appear — `inkling` (Thinking Machines' 6 instances, no identity
// version) and `kling` (the 8 klingai video keys, whose shape token lives in the VARIANT
// slot, not the version) — while `ling` keeps its bare line through the surviving ling#1t.
// The versioned side nets to zero: the phantom kling-v2@6 line retires and the kling@2.6
// line replaces it one-for-one.
// 417 -> 416 with the keyspace-wide mimo normalization (-1 bare / versioned UNCHANGED):
// the release name is the variant, and mimo's variant is now empty on every key, so the
// six bare mimo keys that each carried their own name (mimo, mimo/flash, mimo/pro and the
// three v2.5-tts* speech keys) all move onto a versioned line — their version came out of
// the variant slot the letter had been sharing. The bare `mimo` line therefore empties.
// The two versioned mimo lines (gen 2 and gen 2.5) both already existed and simply absorb
// the arrivals, so no versioned line is added or retired.
// 416 -> 416 with the cogito decomposition repair (+1 versioned / -1 bare): the fused
// variant "v2.1-671b" carried no version, so the artifact sat on a BARE cogito line that
// it was the sole occupant of; un-fusing it into variant "v" + version "2.1" moves it to
// a cogito gen-2.1 line that did not exist before. One bare line empties, one versioned
// line appears, and the total is unchanged.
func TestSeriesAll_CensusExact(t *testing.T) {
	const (
		wantSeries        = 416 // 411 -> 419: 2026-07-23 refresh (+4 versioned incl. gemini-3.6, +4 bare); 419 -> 417: v0.2.8 slice — the deepseek dot-lost merges retire the two phantom versioned lines deepseek gen-1 / gen-2 (command/a{translate} joins the existing command/a line, adding none); 417 -> 415: the global free demotion empties the deepseek-flash and minimax-m3 bare lines; 415 -> 417: the ling/inkling/kling split adds the bare `inkling` and `kling` lines (the kling-v2 versioned line is replaced one-for-one by kling@2.6); 417 -> 416: the keyspace-wide mimo normalization empties the bare `mimo` line (all six of its keys move onto the two existing versioned mimo lines)
		wantVersionLines  = 210 // lines with a non-empty generation (207 -> 211 at the 2026-07-23 refresh; 211 -> 209 as deepseek gen-1 / gen-2 retire in the v0.2.8 slice; UNCHANGED by the free demotion — every versioned line it touches keeps other entities; 209 -> 210 as the cogito decomposition mints the cogito gen-2.1 line)
		wantBareLines     = 206 // lines whose entities carry no identity version (204 -> 208 at the 2026-07-23 refresh; UNCHANGED by the v0.2.8 slice; 208 -> 206 as the free demotion empties the deepseek-flash and minimax-m3 lines; 206 -> 208 as the ling/inkling/kling split adds the bare inkling and kling lines). 208 -> 207 as the mimo normalization empties the bare mimo line; 207 -> 206 as the cogito decomposition moves its sole bare occupant onto the new gen-2.1 line. 210 + 206 = 416.
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
// 672 → 670 with the o-series dual-identity fix: vacating the family-"o" line retires
// its two releases (the bare "o" release and the "mini" release); the o1/o3 rows land on
// releases that already existed under gpt/o, so none is added.
//
// 670 → 659 with the dot-lost version repair + 1t param-size routing: the dot-lost merges
// retire the releases carried by the folded dotless/dash lines, and the 1t re-keys move
// ling/ring onto releases that already existed; net −11.
// 669 → 652 with the global free demotion: each of the 17 retired keys carried a
// free-tier release name of its own on its line (free, flash-free, omni-free, pro-free,
// v2.5, v2.5-free, v2.5-pro), none of which any surviving entity shares, so the release
// count falls by exactly the 17 retired keys.
// 652 → 661 with the ling/inkling/kling collision split (+9): the 8 klingai keys each carry
// a distinct shape token in the variant slot (v2.5-turbo-i2v … v3.0-t2v), so each is its own
// named release on the new bare kling line (+8); `inkling` and `kling@2.6` open a bare
// release on each of their new lines (+2); the phantom kling-v2@6 line takes its sole bare
// release with it (−1); and retiring bare `ling` costs NOTHING, because ling#1t already
// shares that line's un-named release. 8 + 2 − 1 = +9.
// 661 -> 655 with the keyspace-wide mimo normalization (-6): a release is named by the
// VARIANT, and after the normalization no mimo key has one. Before, mimo carried EIGHT
// releases — six on the bare line (the un-named bare `mimo`, plus flash, pro, v2.5-tts,
// v2.5-tts-voiceclone, v2.5-tts-voicedesign) and one named "v" on each of the two
// versioned lines. After, it carries TWO: one un-named release on gen 2 and one on gen
// 2.5. The six speech/tier distinctions are not lost; they moved out of the release name
// and into the identity-modifier segment of the key. 8 - 2 = -6.
func TestReleases_CensusExact(t *testing.T) {
	const wantReleases = 655 // 659 -> 671: 2026-07-23 refresh (+12 releases on the new lines); 671 -> 669: v0.2.8 slice — the two phantom deepseek gen-1 / gen-2 lines retire their bare releases (command/a{translate} shares command/a's existing release; a modifier is not a distinct release name); 669 -> 652: the global free demotion retires 17 keys, each the sole occupant of its release name (−17); 652 -> 661: the ling/inkling/kling split adds 8 named kling shape releases plus the bare inkling and kling@2.6 releases and retires the sole kling-v2@6 release (+9); 661 -> 655: the mimo normalization empties the variant slot on every mimo key, collapsing mimo's eight named releases to the two un-named ones on gen 2 and gen 2.5

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

// TestSeries_GenerationNormalization_Gemini is the ratified gemini ruling, now realized
// at ENTITY IDENTITY level (the C4 MERGE-only N->N.0 fold): a family that spells both a
// bare "3" and "3.0" for the same variant no longer carries two entities — gemini/flash@3
// MERGES into gemini/flash@3.0. So the bare spelling is not a separate entity at all: the
// bare EXPRESSION resolves through the merge to the single dotted entity, and the series
// view sees one line, Series{gemini, "3.0"}. (The series-level generation fold in
// taxonomy.go is retained as a safety net, but the entity merge now subsumes it — there
// is no longer a bare-"3" entity for it to fold.)
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

	// Both spellings resolve to the SAME single merged entity gemini/flash@3.0: the bare
	// key is an alias, the dotted key is the entity itself.
	dotted := "gemini/flash@3.0"
	for _, key := range []string{"gemini/flash@3", "gemini/flash@3.0"} {
		e, ok := bestiary.EntityByKey(key)
		if !ok {
			t.Fatalf("EntityByKey(%q) = false; the merged entity is missing from the registry", key)
		}
		// The resolved entity is the DOTTED one regardless of which spelling was asked for
		// — the merge moved the bare spelling onto it.
		if got := e.Ref.String(); got != dotted {
			t.Errorf("EntityByKey(%q) resolved to %q, want the merged entity %q", key, got, dotted)
		}
		if got := bestiary.SeriesOf(e.Ref); got != normalized {
			t.Errorf("SeriesOf(%q) = %+v, want %+v", key, got, normalized)
		}
	}

	// The flash release of the normalized line holds the ONE merged entity (the two
	// spellings are no longer two entities).
	flash := bestiary.Release{Series: normalized, Name: "flash"}
	wantKeys := []string{"gemini/flash@3.0"}
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
