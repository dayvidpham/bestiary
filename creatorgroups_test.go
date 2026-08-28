package bestiary_test

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// walkGroupKeys returns every entity key the tree yields, in walk order, WITH
// duplicates preserved. Preserving duplicates is the point: a set would silently
// absorb a double-parented entity, which is exactly one of the two failures the
// reachability identity exists to catch.
func walkGroupKeys(groups []bestiary.CreatorGroup) []string {
	var keys []string
	for _, cg := range groups {
		for _, fg := range cg.Families {
			for _, sg := range fg.Series {
				for _, e := range sg.Hoisted {
					keys = append(keys, e.Ref.String())
				}
				for _, rg := range sg.Releases {
					for _, e := range rg.Entities {
						keys = append(keys, e.Ref.String())
					}
				}
			}
		}
	}
	return keys
}

// countBy tallies occurrences of each key.
func countBy(keys []string) map[string]int {
	out := make(map[string]int, len(keys))
	for _, k := range keys {
		out[k]++
	}
	return out
}

// TestCreatorGroups_ReachabilityIdentity is the DERIVED identity that guards the
// base hoist: the tree is a PARTITION of the corpus — every entity in Entities()
// appears in it exactly once, and it yields nothing Entities() does not.
//
// It is stated as an identity against Entities() rather than as a literal count
// on purpose. The corpus census moves whenever a curation lever lands, so a
// pinned number here would be re-pinned (and could be re-pinned WRONG) by every
// unrelated slice; the identity holds at any corpus size and needs no upkeep.
//
// This is the test that makes a delete-instead-of-hoist regression fail loudly.
// Hoisting is a re-parenting: the un-named release's entities move up a level.
// An implementation that DROPPED them instead would still render a perfectly
// plausible tree — every remaining node correct, nothing visibly broken — and
// would silently lose the several hundred entities whose lines have no named
// release at all. Here that is an immediate, enumerated failure.
func TestCreatorGroups_ReachabilityIdentity(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	seen := countBy(walkGroupKeys(bestiary.CreatorGroups()))

	var missing, duplicated []string
	for _, e := range entities {
		key := e.Ref.String()
		switch n := seen[key]; {
		case n == 1:
		case n == 0:
			missing = append(missing, key)
		default:
			duplicated = append(duplicated, fmt.Sprintf("%s (x%d)", key, n))
		}
		delete(seen, key)
	}
	// Whatever is left was yielded by the tree but is not in the corpus.
	var extra []string
	for key := range seen {
		extra = append(extra, key)
	}

	for label, got := range map[string][]string{
		"missing from the tree (a hoist that DROPS instead of re-parents)": missing,
		"appearing more than once in the tree":                             duplicated,
		"yielded by the tree but absent from Entities()":                   extra,
	} {
		if len(got) == 0 {
			continue
		}
		sort.Strings(got)
		if len(got) > 10 {
			t.Errorf("%d entities %s; first 10: %v", len(got), label, got[:10])
			continue
		}
		t.Errorf("%d entities %s: %v", len(got), label, got)
	}
}

// TestSeriesGroups_ReachabilityIdentity holds the same partition property for the
// flat, creator-agnostic view. Both views are built from one hoist implementation,
// so this also pins that the nesting step neither adds nor loses an entity.
func TestSeriesGroups_ReachabilityIdentity(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}

	var flat []string
	for _, sg := range bestiary.SeriesGroups() {
		for _, e := range sg.Hoisted {
			flat = append(flat, e.Ref.String())
		}
		for _, rg := range sg.Releases {
			for _, e := range rg.Entities {
				flat = append(flat, e.Ref.String())
			}
		}
	}
	nested := walkGroupKeys(bestiary.CreatorGroups())

	if len(flat) != len(entities) {
		t.Errorf("flat SeriesGroups walk yielded %d entity slots, want one per entity (%d)", len(flat), len(entities))
	}
	sort.Strings(flat)
	sort.Strings(nested)
	if len(flat) != len(nested) {
		t.Fatalf("flat walk yielded %d slots, nested walk %d: the two views disagree", len(flat), len(nested))
	}
	for i := range flat {
		if flat[i] != nested[i] {
			t.Fatalf("flat and nested walks disagree at %d: %q vs %q", i, flat[i], nested[i])
		}
	}
}

// TestSeriesGroups_NoBaseNode pins the user-visible half of the hoist: no group
// carries a release whose name is empty or the literal "(base)" placeholder the
// pre-hoist explorer rendered. The un-named release is reachable ONLY as Hoisted.
func TestSeriesGroups_NoBaseNode(t *testing.T) {
	for _, sg := range bestiary.SeriesGroups() {
		for _, rg := range sg.Releases {
			if rg.Release.Name == "" {
				t.Errorf("series %q kept an un-named release as a node; it must be hoisted", sg.Series.String())
			}
			if rg.Release.Name == "(base)" {
				t.Errorf("series %q emitted a literal \"(base)\" release node", sg.Series.String())
			}
		}
	}
}

// TestSeriesGroups_PerShapeInvariants exercises all three real shapes over the
// real corpus and asserts what each one promises a renderer, so a layout can rely
// on Shape() alone. It also confirms the corpus actually CONTAINS all three —
// otherwise the per-shape rendering paths would be asserted against nothing.
func TestSeriesGroups_PerShapeInvariants(t *testing.T) {
	groups := bestiary.SeriesGroups()
	if len(groups) == 0 {
		t.Skip("no registry series")
	}

	counts := map[bestiary.SeriesShape]int{}
	entitiesByShape := map[bestiary.SeriesShape]int{}
	for _, sg := range groups {
		shape := sg.Shape()
		counts[shape]++
		entitiesByShape[shape] += sg.EntityCount

		switch shape {
		case bestiary.SeriesShapeBaseOnly:
			// Nothing to disclose: the entities attach directly.
			if len(sg.Releases) != 0 {
				t.Errorf("base-only series %q carries %d releases", sg.Series.String(), len(sg.Releases))
			}
			if len(sg.Hoisted) == 0 {
				t.Errorf("base-only series %q has no hoisted entities", sg.Series.String())
			}
		case bestiary.SeriesShapeMixed:
			// Both levels present: a renderer must show hoisted entities above
			// the release disclosures and distinguish them visually.
			if len(sg.Hoisted) == 0 || len(sg.Releases) == 0 {
				t.Errorf("mixed series %q is not actually mixed: %d hoisted, %d releases",
					sg.Series.String(), len(sg.Hoisted), len(sg.Releases))
			}
		case bestiary.SeriesShapeNamedOnly:
			if len(sg.Hoisted) != 0 {
				t.Errorf("named-only series %q carries %d hoisted entities", sg.Series.String(), len(sg.Hoisted))
			}
			if len(sg.Releases) == 0 {
				t.Errorf("named-only series %q has no releases", sg.Series.String())
			}
		default:
			t.Errorf("series %q has shape %s: an emitted group must never be empty", sg.Series.String(), shape)
		}

		// EntityCount is a derived sum and must agree with the contents it
		// summarizes — a renderer labels collapsed nodes from it without walking.
		want := len(sg.Hoisted)
		for _, rg := range sg.Releases {
			want += len(rg.Entities)
		}
		if sg.EntityCount != want {
			t.Errorf("series %q EntityCount = %d, want %d (derived sum disagrees with contents)",
				sg.Series.String(), sg.EntityCount, want)
		}
	}

	for _, shape := range []bestiary.SeriesShape{
		bestiary.SeriesShapeBaseOnly, bestiary.SeriesShapeMixed, bestiary.SeriesShapeNamedOnly,
	} {
		if counts[shape] == 0 {
			t.Errorf("corpus contains no %s series: the %s rendering path is untested", shape, shape)
		}
	}
	// The shapes partition the series, and the hoist covers a real share of the
	// corpus — not a rounding error a broken hoist could hide inside.
	if got := counts[bestiary.SeriesShapeBaseOnly] + counts[bestiary.SeriesShapeMixed] + counts[bestiary.SeriesShapeNamedOnly]; got != len(groups) {
		t.Errorf("shape counts sum to %d, want %d series", got, len(groups))
	}
	hoisted := 0
	for _, sg := range groups {
		hoisted += len(sg.Hoisted)
	}
	if hoisted == 0 {
		t.Error("no entities are hoisted anywhere: the base hoist is not doing anything")
	}
	t.Logf("shape census (unit: series; axis: release composition; configuration: static registry): base-only=%d mixed=%d named-only=%d total=%d",
		counts[bestiary.SeriesShapeBaseOnly], counts[bestiary.SeriesShapeMixed],
		counts[bestiary.SeriesShapeNamedOnly], len(groups))
	t.Logf("hoist coverage (unit: entities; axis: un-named release membership; configuration: static registry): hoisted=%d of %d",
		hoisted, len(bestiary.Entities()))
}

// TestSeriesShape_ZeroValueIsNone pins the enum's zero-value convention: an empty
// group reports SeriesShapeNone, not a silent "named-only". The distinction
// matters because a renderer switches on Shape(), and a zero value that claimed a
// real shape would send an empty group down a rendering path that assumes content.
func TestSeriesShape_ZeroValueIsNone(t *testing.T) {
	var zero bestiary.SeriesGroup
	if got := zero.Shape(); got != bestiary.SeriesShapeNone {
		t.Errorf("zero SeriesGroup.Shape() = %s, want %s", got, bestiary.SeriesShapeNone)
	}
	if got := bestiary.SeriesShapeNone.String(); got != "none" {
		t.Errorf("SeriesShapeNone.String() = %q, want %q", got, "none")
	}
	// A group whose only release is empty of entities is still None: shape is
	// derived from CONTENT, not from the presence of a release node.
	hollow := bestiary.SeriesGroup{Releases: []bestiary.ReleaseGroup{{Release: bestiary.Release{Name: "scout"}}}}
	if got := hollow.Shape(); got != bestiary.SeriesShapeNone {
		t.Errorf("group with an entity-less release reported %s, want %s", got, bestiary.SeriesShapeNone)
	}
}

// TestCreatorGroups_DerivedFromCuratedSeed_NotHardCoded is the creator-dimension
// pin. The tree must read whatever creators the curated seed carries, so that the
// creator-dimension slice can grow creators.json (9 seeded today, more later)
// WITHOUT touching this projection. Asserting a literal creator count here would
// defeat that: it would make an unrelated seed expansion redden this test, and the
// natural "fix" would be to re-pin the number rather than to notice.
//
// So the assertions are relational: every creator the tree emits is either a
// curated known creator or the CreatorNone remainder, every family the corpus
// carries gets a group under the creator its OWN curated mapping names, and no
// creator token is written down in this file.
func TestCreatorGroups_DerivedFromCuratedSeed_NotHardCoded(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	groups := bestiary.CreatorGroups()

	emitted := map[bestiary.Creator]int{}
	for _, cg := range groups {
		if _, dup := emitted[cg.Creator]; dup {
			t.Errorf("creator %q emitted as two separate groups", cg.Creator)
		}
		emitted[cg.Creator] = cg.EntityCount
		if cg.Creator != bestiary.CreatorNone && !cg.Creator.IsKnown() {
			t.Errorf("tree emitted creator %q, which is not in the curated seed", cg.Creator)
		}
		// Every family sits under the creator its own curated mapping names —
		// the seed is the authority, not anything written here.
		for _, fg := range cg.Families {
			if got := fg.Family.Creator(); got != cg.Creator {
				t.Errorf("family %q is grouped under creator %q but its curated mapping says %q",
					fg.Family, cg.Creator, got)
			}
		}
	}

	// Ground truth: the set of creators the curated seed attributes the corpus'
	// SERIES families to. Derived, never enumerated.
	attributed := map[bestiary.Creator]bool{}
	for _, sg := range bestiary.SeriesGroups() {
		attributed[sg.Series.Family.Creator()] = true
	}
	for creator := range attributed {
		if _, ok := emitted[creator]; !ok {
			t.Errorf("the corpus attributes families to creator %q but the tree emits no group for it", creator)
		}
	}
	if len(emitted) != len(attributed) {
		t.Errorf("tree emits %d creator groups, the curated seed attributes corpus families to %d creators",
			len(emitted), len(attributed))
	}
	// A creator seeded but unused by the corpus correctly yields no group; a
	// seeded creator the corpus DOES use must be present.
	for _, known := range bestiary.Creators() {
		if attributed[known] && emitted[known] == 0 {
			t.Errorf("curated creator %q has corpus families but no group in the tree", known)
		}
	}
	t.Logf("creator census (unit: groups; axis: Series.Family -> curated creator; configuration: static registry + curated creators.json): %d groups over %d seeded creators",
		len(emitted), len(bestiary.Creators()))
}

// curatedStrayRowJSON is the minimal shape of one parse/data/series.json row this
// test needs: the family token the curation re-homes, and the line it re-homes it
// onto. The production loader reads the same file (taxonomy.go), but its table is
// unexported and, more to the point, deriving the expectation from the FILE rather
// than from the loader keeps the test an independent statement about the curation
// instead of a restatement of the code under test.
type curatedStrayRowJSON struct {
	Family string `json:"family"`
	Series struct {
		Family string `json:"family"`
	} `json:"series"`
}

// loadCuratedStrayLines reads parse/data/series.json and returns each re-homed
// family token mapped to the family of the line it is re-homed onto, both
// case-folded to match the production loader's case-insensitive keying.
func loadCuratedStrayLines(t *testing.T) map[bestiary.Family]bestiary.Family {
	t.Helper()
	const path = "parse/data/series.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the curated strays table %s: %v\n"+
			"  What it means: this test derives its expectation from that file and cannot run without it.\n"+
			"  How to fix: run the test from the module root, or restore the file.", path, err)
	}
	var file struct {
		Strays []curatedStrayRowJSON `json:"strays"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("cannot parse the curated strays table %s: %v\n"+
			"  How to fix: correct the JSON; the production loader degrades to no strays on a malformed file, "+
			"which would silently empty this test's expectation.", path, err)
	}
	out := make(map[bestiary.Family]bestiary.Family, len(file.Strays))
	for _, row := range file.Strays {
		if row.Family == "" || row.Series.Family == "" {
			continue
		}
		out[bestiary.Family(strings.ToLower(row.Family))] = bestiary.Family(strings.ToLower(row.Series.Family))
	}
	return out
}

// TestCreatorGroups_CreatorDivergenceIsOnlyStrays pins the ONE way an entity can
// appear under a creator its own Entity.Creator projection does not name.
//
// The tree groups by Series.Family so that the curated strays table is honoured:
// series.json re-homes gemma4 onto the gemma-4 line, and the tree shows it there.
// (At the 2026-08-28 catalog refresh all three curated strays lost their upstream rows,
// so the derived expectation is empty; see the note in the body — the mechanism is
// unchanged and the assertion is strictly stronger while that holds.)
// But Entity.Creator is a function of the entity's OWN family token, which knows
// nothing of that re-homing — so a stray's own creator can be CreatorNone while
// the line it was re-homed onto is attributed.
//
// That divergence is intended, but it must stay CONFINED to strays, and to
// EXACTLY the strays the curation actually re-homes across a creator boundary.
// So the expectation is DERIVED from parse/data/series.json: a row whose own
// family token maps to a different (or missing) creator than the line it is
// re-homed onto MUST show up as a divergence, and nothing else may. The
// derivation is the pin — no literal count or token is written down here, so a
// curation change moves the expectation and the corpus together, while a change
// in the GROUPING silently ceases to match and reddens.
//
// Two failures are called out by name rather than folded into the set diff,
// because both are misattributed authorship — a much more serious claim than a
// re-homing: an entity whose own family IS its series' family landing under a
// foreign creator, and an entity whose family appears in no curated row at all.
func TestCreatorGroups_CreatorDivergenceIsOnlyStrays(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	rehomed := loadCuratedStrayLines(t)

	present := make(map[bestiary.Family]bool, len(entities))
	for _, e := range entities {
		present[e.Ref.Family] = true
	}

	// EXPECTED, derived: a curated row diverges exactly when the creator of the
	// line it re-homes onto differs from the creator of its own family token. A
	// row for a family the corpus does not carry is correctly invisible here.
	want := map[bestiary.Family]bool{}
	for own, line := range rehomed {
		if !present[own] {
			continue
		}
		if own.Creator() != line.Creator() {
			want[own] = true
		}
	}
	// Non-vacuity. The derivation IS empty at this tip, and the reason is documented
	// and asserted rather than assumed: the 2026-08-28 models.dev catalog refresh
	// retired the upstream rows behind all three curated strays, so gemma4,
	// gemma-4-31b-larkspur and gemini-exp no longer name any corpus entity and a row
	// for a family the corpus does not carry is correctly invisible here.
	//
	// The check is NOT deleted and NOT weakened. With an empty derivation the set
	// equality below becomes STRICTLY STRONGER — it now says no entity anywhere in the
	// tree may sit under a foreign creator — and the two named misattribution failures
	// still sweep every entity. What an empty `want` would genuinely make vacuous is a
	// tree walk that visits nothing, so that, and the documented cause of the emptiness,
	// are what is asserted here instead of a bare Fatal.
	if len(want) == 0 {
		for own := range rehomed {
			if present[own] {
				t.Errorf("derived expectation is empty, but curated stray family %q IS carried by the corpus\n"+
					"  What it means: the emptiness no longer has its documented cause (every stray family absent from the corpus).\n"+
					"  How to fix: re-derive — either the stray now re-homes within one creator, or the derivation broke.", own)
			}
		}
		t.Logf("derived expectation is empty: every curated stray in parse/data/series.json (%d rows) names a family "+
			"the corpus no longer carries, so no re-homing crosses a creator boundary. The set equality below "+
			"consequently asserts that NO entity sits under a foreign creator.", len(rehomed))
	}

	got := map[bestiary.Family]bool{}
	var divergences []string
	visited := 0
	for _, cg := range bestiary.CreatorGroups() {
		for _, fg := range cg.Families {
			for _, sg := range fg.Series {
				var all []bestiary.Entity
				all = append(all, sg.Hoisted...)
				for _, rg := range sg.Releases {
					all = append(all, rg.Entities...)
				}
				for _, e := range all {
					visited++
					if e.Creator == cg.Creator {
						continue
					}
					if e.Ref.Family == sg.Series.Family {
						t.Errorf("entity %q (own family %q, own creator %q) is grouped under creator %q "+
							"but is NOT a re-homed stray: the tree is misattributing authorship",
							e.Ref.String(), e.Ref.Family, e.Creator, cg.Creator)
						continue
					}
					if _, curated := rehomed[e.Ref.Family]; !curated {
						t.Errorf("entity %q (own family %q, own creator %q) is grouped under creator %q "+
							"but family %q appears in no row of parse/data/series.json: the tree is misattributing "+
							"authorship through a re-homing nobody curated",
							e.Ref.String(), e.Ref.Family, e.Creator, cg.Creator, e.Ref.Family)
						continue
					}
					got[e.Ref.Family] = true
					divergences = append(divergences, fmt.Sprintf("%s (family %s -> series %s, own creator %q, shown under %q)",
						e.Ref.String(), e.Ref.Family, sg.Series.String(), e.Creator, cg.Creator))
				}
			}
		}
	}

	// Tree-walk non-vacuity: the set equality only means something if the walk actually
	// visited the corpus. This is the guard that matters now that the derivation is
	// empty — a CreatorGroups() that returned nothing would otherwise pass silently.
	// The floor is deliberately far below the ~989-entity registry so ordinary catalog
	// churn never trips it.
	const minVisited = 500
	if visited < minVisited {
		t.Errorf("the creator-group walk visited only %d entities, want at least %d\n"+
			"  What it means: the set equality above compared against an essentially empty tree and asserted nothing.\n"+
			"  How to fix: check CreatorGroups() — a collapsed tree, not a curation change, is the likely cause.",
			visited, minVisited)
	}

	// Set equality against the derivation, in both directions.
	var unexpected, absent []string
	for fam := range got {
		if !want[fam] {
			unexpected = append(unexpected, string(fam))
		}
	}
	for fam := range want {
		if !got[fam] {
			absent = append(absent, string(fam))
		}
	}
	sort.Strings(unexpected)
	sort.Strings(absent)
	if len(unexpected) > 0 {
		t.Errorf("families shown under a foreign creator that the curation does not re-home across a creator boundary: %v\n"+
			"  How to fix: either the grouping changed, or parse/data/series.json changed and the divergence is now expected.",
			unexpected)
	}
	if len(absent) > 0 {
		t.Errorf("curated cross-creator re-homings that produced NO divergence in the tree: %v\n"+
			"  What it means: the tree is no longer grouping those entities by their re-homed line, so the curation is not being honoured.",
			absent)
	}

	sort.Strings(divergences)
	t.Logf("curated-stray creator divergences (unit: entities; axis: own family vs series family; configuration: static registry + series.json): %d entities over %d curated families\n  %s",
		len(divergences), len(got), strings.Join(divergences, "\n  "))
}

// TestCreatorGroups_Ordering pins the two orderings a reader depends on: groups
// ascend by creator token, and the unattributed remainder is LAST so it can be
// collapsed at the bottom rather than leading the page with an empty name.
func TestCreatorGroups_Ordering(t *testing.T) {
	groups := bestiary.CreatorGroups()
	if len(groups) < 2 {
		t.Skip("need >= 2 creator groups")
	}
	for i, cg := range groups {
		if cg.Creator != bestiary.CreatorNone {
			continue
		}
		if i != len(groups)-1 {
			t.Errorf("the unattributed (CreatorNone) group is at index %d of %d; it must sort last", i, len(groups))
		}
	}
	for i := 1; i < len(groups); i++ {
		prev, cur := groups[i-1].Creator, groups[i].Creator
		if prev == bestiary.CreatorNone {
			t.Errorf("a group follows the CreatorNone remainder at index %d", i)
		}
		if cur != bestiary.CreatorNone && prev >= cur {
			t.Errorf("creator groups out of order at %d: %q before %q", i, prev, cur)
		}
	}
	// Families ascend within a creator, series ascend within a family, and every
	// counter is the derived sum of the level below it.
	for _, cg := range groups {
		sum := 0
		for i, fg := range cg.Families {
			if i > 0 && cg.Families[i-1].Family >= fg.Family {
				t.Errorf("creator %q families out of order at %d: %q before %q", cg.Creator, i, cg.Families[i-1].Family, fg.Family)
			}
			famSum := 0
			for j, sg := range fg.Series {
				if j > 0 {
					p := fg.Series[j-1].Series
					if p.Family > sg.Series.Family || (p.Family == sg.Series.Family && p.Generation > sg.Series.Generation) {
						t.Errorf("family %q series out of order at %d: %q before %q", fg.Family, j, p.String(), sg.Series.String())
					}
				}
				famSum += sg.EntityCount
			}
			if fg.EntityCount != famSum {
				t.Errorf("family %q EntityCount = %d, want %d", fg.Family, fg.EntityCount, famSum)
			}
			sum += fg.EntityCount
		}
		if cg.EntityCount != sum {
			t.Errorf("creator %q EntityCount = %d, want %d", cg.Creator, cg.EntityCount, sum)
		}
	}
}

// TestCreatorGroups_DefensiveCopy confirms the projection hands back copies, the
// Entities()/EntitiesOf() precedent: mutating a returned entity can never corrupt
// the memoized registry or alias another caller's result.
func TestCreatorGroups_DefensiveCopy(t *testing.T) {
	first := bestiary.CreatorGroups()
	if len(first) == 0 {
		t.Skip("no creator groups")
	}
	var target *bestiary.Entity
	for i := range first {
		for j := range first[i].Families {
			for k := range first[i].Families[j].Series {
				if h := first[i].Families[j].Series[k].Hoisted; len(h) > 0 {
					target = &h[0]
				}
			}
		}
	}
	if target == nil {
		t.Skip("no hoisted entity to mutate")
	}
	key := target.Ref.String()
	target.Ref.Family = bestiary.Family("__mutated__")

	for _, cg := range bestiary.CreatorGroups() {
		for _, fg := range cg.Families {
			for _, sg := range fg.Series {
				for _, e := range sg.Hoisted {
					if e.Ref.Family == bestiary.Family("__mutated__") {
						t.Fatalf("mutating a returned entity (%q) corrupted the registry projection", key)
					}
				}
			}
		}
	}
}
