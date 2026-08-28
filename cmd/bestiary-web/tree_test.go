package main

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// realServer builds the server over the actual committed registry, skipping when the
// registry is empty so the suite stays runnable against a stripped corpus.
func realServer(t *testing.T) *Server {
	t.Helper()
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	return newTestServer(t, entities)
}

// TestSeries_Retired_HardNotFound pins the retirement of /series. The page was not
// redirected, aliased, or left serving a successor listing — it is GONE, and the route,
// its handler and its template were deleted rather than repointed.
//
// A hard 404 is the deliberate choice over a 301: an alias keeps a dead name alive in
// links, bookmarks and search results indefinitely, which is precisely what retiring the
// name was meant to stop. The content itself was not lost — /families absorbed it and
// emits the same anchors — so nothing a reader wanted is unreachable, only the old spelling.
func TestSeries_Retired_HardNotFound(t *testing.T) {
	s := realServer(t)
	for _, target := range []string{"/series", "/series#series-llama-4"} {
		rec := get(t, s, target, "text/html")
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %q = %d, want 404 (the retired route must not be aliased or redirected)", target, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "" {
			t.Errorf("GET %q emitted a redirect to %q; the retirement is a hard 404", target, loc)
		}
	}
	// The page name is gone from the template registry too, not merely unrouted.
	if _, ok := pageFiles["series"]; ok {
		t.Error(`pageFiles still carries a "series" entry; the page was retired, not repointed`)
	}
	if _, ok := s.pages["series"]; ok {
		t.Error(`the parsed page set still carries "series"`)
	}
}

// TestTree_FrontPage_WalksCreatorHierarchy is the front-page contract: "/" renders the
// Creator > Family > Series > entities disclosure tree, attributed creators arrive
// expanded, and the unattributed remainder is a single collapsed group at the bottom.
func TestTree_FrontPage_WalksCreatorHierarchy(t *testing.T) {
	s := realServer(t)
	rec := get(t, s, "/", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	groups := bestiary.CreatorGroups()
	if len(groups) == 0 {
		t.Fatal("CreatorGroups() empty over a non-empty registry")
	}
	for _, cg := range groups {
		if cg.Creator == bestiary.CreatorNone {
			continue
		}
		if !strings.Contains(body, ">"+string(cg.Creator)+"<") {
			t.Errorf("front page missing creator %q", cg.Creator)
		}
		for _, fg := range cg.Families {
			if !strings.Contains(body, ">"+string(fg.Family)+"<") {
				t.Errorf("front page missing family %q under creator %q", fg.Family, cg.Creator)
			}
		}
	}

	// Every level of the hierarchy is actually rendered as a disclosure node.
	for _, want := range []string{`class="creator-node"`, `class="family-node"`, `class="series-node"`, `class="release-node"`} {
		if !strings.Contains(body, want) {
			t.Errorf("front page missing tree level %s", want)
		}
	}
	// Attributed creators open by default; the unattributed remainder does not.
	if !strings.Contains(body, `<details class="creator-node" open>`) {
		t.Error("no creator group is expanded on arrival: the page reads as a row of closed boxes")
	}
	if !strings.Contains(body, ">unattributed<") {
		t.Error("front page does not surface the unattributed remainder group")
	}
	openIdx := strings.Index(body, `<details class="creator-node" open>`)
	remIdx := strings.Index(body, ">unattributed<")
	if openIdx >= 0 && remIdx >= 0 && remIdx < openIdx {
		t.Error("the unattributed remainder renders before an attributed creator; it belongs last")
	}
	if collapsed := strings.LastIndex(body[:remIdx], `<details class="creator-node">`); collapsed < 0 {
		t.Error("the unattributed remainder group is not collapsed")
	}
}

// TestTree_RenderedReachabilityIdentity asserts the reachability identity on the RENDERED
// DOCUMENT rather than on the projection: every entity in Entities() is linked from the
// front-page tree exactly once, and the tree links nothing that is not an entity.
//
// The projection-level identity already holds (see the root package's reachability test),
// but that is not the same claim: a template can drop a branch it never ranges over, or
// emit one twice, entirely downstream of a correct projection. This test is what makes
// "every entity appears exactly once in the rendered tree" true of the page a reader
// actually loads.
//
// It counts exact `href="<path>"` occurrences rather than bare keys because one entity key
// is frequently a substring of another (llama@3.1 inside llama@3.1#405b); the closing quote
// makes each link match exactly one entity.
func TestTree_RenderedReachabilityIdentity(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)
	body := get(t, s, "/", "text/html").Body.String()

	var missing, duplicated []string
	total := 0
	for _, e := range entities {
		href := `href="` + e.Ref.IRI(entityRoutePrefix) + `"`
		n := strings.Count(body, href)
		total += n
		switch {
		case n == 1:
		case n == 0:
			missing = append(missing, e.Ref.String())
		default:
			duplicated = append(duplicated, e.Ref.String())
		}
	}
	sort.Strings(missing)
	sort.Strings(duplicated)

	if len(missing) != 0 {
		show := missing
		if len(show) > 10 {
			show = show[:10]
		}
		t.Errorf("%d of %d entities are NOT linked from the rendered tree (a hoist that drops "+
			"instead of re-parents renders a plausible but smaller tree); first: %v",
			len(missing), len(entities), show)
	}
	if len(duplicated) != 0 {
		show := duplicated
		if len(show) > 10 {
			show = show[:10]
		}
		t.Errorf("%d entities are linked more than once from the rendered tree; first: %v", len(duplicated), show)
	}
	// Nothing extra: the entity links on the page account for exactly the corpus.
	if got := strings.Count(body, `href="`+entityRoutePrefix); got != total {
		t.Errorf("tree emits %d entity links but only %d are accounted for by Entities()", got, total)
	}
}

// TestFamilies_RenderedReachabilityIdentity holds the same rendered identity for /families,
// the other page built on the hoisted substrate.
func TestFamilies_RenderedReachabilityIdentity(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)
	body := get(t, s, "/families", "text/html").Body.String()

	var missing, duplicated int
	for _, e := range entities {
		switch strings.Count(body, `href="`+e.Ref.IRI(entityRoutePrefix)+`"`) {
		case 1:
		case 0:
			missing++
		default:
			duplicated++
		}
	}
	if missing != 0 || duplicated != 0 {
		t.Errorf("/families rendered reachability broken: %d entities missing, %d duplicated (of %d)",
			missing, duplicated, len(entities))
	}
}

// TestHoist_NoBaseNodeOnAnyPage is the user-visible half of the base hoist: the string
// "(base)" appears on no page. The pre-hoist explorer rendered the un-named release as a
// node with that literal name; after the hoist there is no such node anywhere, on either
// page that walks the hierarchy.
func TestHoist_NoBaseNodeOnAnyPage(t *testing.T) {
	s := realServer(t)
	for _, page := range []string{"/", "/families"} {
		body := get(t, s, page, "text/html").Body.String()
		if strings.Contains(body, "(base)") {
			t.Errorf("%s still renders a \"(base)\" node; the un-named release must be hoisted", page)
		}
	}
}

// TestHoist_PerShapeRendering pins what each series shape renders, using a concrete series
// of each shape drawn from the real corpus. It is the rendering half of the projection's
// per-shape invariants.
//
//   - a BASE-ONLY line attaches its entities directly, with no release disclosure and no
//     legend (there is nothing to distinguish them from);
//   - a MIXED line renders its hoisted entities marked as a distinct level — the legend and
//     the .mixed rule — because they sit adjacent to the release disclosures on screen while
//     belonging above them in the hierarchy;
//   - a NAMED-ONLY line has no hoisted list at all.
func TestHoist_PerShapeRendering(t *testing.T) {
	s := realServer(t)
	body := get(t, s, "/families", "text/html").Body.String()

	samples := map[bestiary.SeriesShape]bestiary.SeriesGroup{}
	for _, sg := range bestiary.SeriesGroups() {
		if _, ok := samples[sg.Shape()]; !ok {
			samples[sg.Shape()] = sg
		}
	}
	for _, shape := range []bestiary.SeriesShape{
		bestiary.SeriesShapeBaseOnly, bestiary.SeriesShapeMixed, bestiary.SeriesShapeNamedOnly,
	} {
		sg, ok := samples[shape]
		if !ok {
			t.Errorf("corpus has no %s series: its rendering path is untested", shape)
			continue
		}
		// Isolate this series' markup by its anchored <details> block.
		open := strings.Index(body, `id="`+seriesAnchor(sg.Series)+`"`)
		if open < 0 {
			t.Errorf("series %q (%s) missing from /families", sg.Series.String(), shape)
			continue
		}
		end := strings.Index(body[open:], "</details>")
		if end < 0 {
			t.Fatalf("unterminated series node for %q", sg.Series.String())
		}
		block := body[open : open+end]

		switch shape {
		case bestiary.SeriesShapeBaseOnly:
			if strings.Contains(block, `class="release-node"`) {
				t.Errorf("base-only series %q rendered a release disclosure", sg.Series.String())
			}
			if strings.Contains(block, "hoisted-legend") {
				t.Errorf("base-only series %q rendered the mixed-level legend; it has nothing to distinguish", sg.Series.String())
			}
			if !strings.Contains(block, `class="hoisted"`) {
				t.Errorf("base-only series %q did not attach its entities directly", sg.Series.String())
			}
		case bestiary.SeriesShapeMixed:
			if !strings.Contains(block, `class="hoisted mixed"`) {
				t.Errorf("mixed series %q did not mark its hoisted entities as a distinct level", sg.Series.String())
			}
			if !strings.Contains(block, "hoisted-legend") {
				t.Errorf("mixed series %q rendered no legend for its hoisted entities", sg.Series.String())
			}
			// The hoisted entities render ABOVE the release disclosures.
			h, r := strings.Index(block, `class="hoisted mixed"`), strings.Index(block, `class="release-node"`)
			if h >= 0 && r >= 0 && h > r {
				t.Errorf("mixed series %q renders its hoisted entities below its releases", sg.Series.String())
			}
		case bestiary.SeriesShapeNamedOnly:
			if strings.Contains(block, `class="hoisted`) {
				t.Errorf("named-only series %q rendered a hoisted list", sg.Series.String())
			}
		}
	}
}

// TestFamilies_SeriesAnchorParity is the anchor contract: /families emits the SAME
// anchor for every series that the retired explorer did, because seriesAnchor is unchanged.
// This is what lets the detail-page links be a pure path retarget rather than a re-linking.
func TestFamilies_SeriesAnchorParity(t *testing.T) {
	s := realServer(t)
	body := get(t, s, "/families", "text/html").Body.String()
	all := bestiary.SeriesAll()
	if len(all) == 0 {
		t.Fatal("SeriesAll() empty over a non-empty registry")
	}
	for _, ser := range all {
		anchor := seriesAnchor(ser)
		if n := strings.Count(body, `id="`+anchor+`"`); n != 1 {
			t.Errorf("series %q anchor %q appears %d times on /families, want exactly 1", ser.String(), anchor, n)
		}
	}
	// The tree deliberately does NOT claim the anchor namespace, so a detail-page series
	// link resolves to exactly one page.
	tree := get(t, s, "/", "text/html").Body.String()
	if strings.Contains(tree, `id="series-`) {
		t.Error("the front-page tree emits series anchors; /families owns that namespace")
	}
}

// TestLinkRetargets pins each of the four cross-page links this refactor moved, by asserting
// on the RENDERED href rather than on the template source — a link is only retargeted if the
// page actually emits the new path AND no longer emits the old one.
func TestLinkRetargets(t *testing.T) {
	s := realServer(t)
	entity := bestiary.Entities()[0]
	detail := get(t, s, entity.Ref.IRI(entityRoutePrefix), "text/html").Body.String()
	browser := get(t, s, "/entities", "text/html").Body.String()

	cases := []struct {
		name, body, want string
	}{
		{"entity browser: 'browse by series' now points at /families", browser, `href="/families"`},
		{"entity detail: the '‹ catalog' link now points at the browser, not the tree", detail, `href="/entities"`},
		{"entity detail: the 'series' link now points at /families", detail, `href="/families"`},
		{"entity detail: the series-section anchor link now points into /families", detail,
			`href="/families#` + seriesAnchor(bestiary.SeriesOf(entity.Ref)) + `"`},
	}
	for _, c := range cases {
		if !strings.Contains(c.body, c.want) {
			t.Errorf("%s: rendered page missing %s", c.name, c.want)
		}
	}
	// No page still emits a link into the retired route.
	for _, page := range []string{"/", "/entities", "/families", entity.Ref.IRI(entityRoutePrefix)} {
		body := get(t, s, page, "text/html").Body.String()
		if strings.Contains(body, `href="/series"`) || strings.Contains(body, `href="/series#`) {
			t.Errorf("%s still links to the retired /series route", page)
		}
	}
}
