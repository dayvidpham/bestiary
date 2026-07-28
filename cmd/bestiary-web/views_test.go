package main

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// sseGet drives the /sse/entities seam with a JSON signal payload and returns the patched
// fragment body. It is the offline (httptest, no port bind) analogue of the browser's
// data-on-input/change round-trip.
func sseGet(t *testing.T, s *Server, signals string) string {
	t.Helper()
	q := url.Values{}
	q.Set("datastar", signals)
	rec := get(t, s, "/sse/entities?"+q.Encode(), "")
	if rec.Code != 200 {
		t.Fatalf("GET /sse/entities = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// TestBrowser_ListsRealCorpus is the R9 "browser lists the 958" case: over the actual
// committed registry the index renders the dense table with every entity's row and the
// filter rail with real facet options. Offline; no network.
func TestBrowser_ListsRealCorpus(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)

	// Every entity has a precomputed row (nothing dropped from the corpus).
	if len(s.rows) != len(entities) {
		t.Fatalf("rows = %d, want one per entity (%d)", len(s.rows), len(entities))
	}

	body := get(t, s, "/", "text/html").Body.String()
	if want := fmt.Sprintf("%d entities", len(entities)); !strings.Contains(body, want) {
		t.Errorf("index missing entity count %q", want)
	}
	// The filter rail ships real facet controls.
	for _, want := range []string{
		`aria-label="filter entities by family"`,
		`aria-label="filter entities by creator"`,
		`aria-label="filter entities by provider"`,
		`aria-label="filter entities by region"`,
		`aria-label="filter entities by modality"`,
		`aria-label="sort entities"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index filter rail missing %q", want)
		}
	}
	// The (ID,Provider)->Modalities startup join surfaced real modality facets and the
	// corpus has priced/VRAM/creator facets too.
	if len(s.facets.Modalities) == 0 {
		t.Error("no modality facets: the StaticModels() modality join did not surface any")
	}
	if len(s.facets.Families) == 0 || len(s.facets.Creators) == 0 || len(s.facets.Providers) == 0 {
		t.Error("expected non-empty family/creator/provider facets over the real corpus")
	}
}

// TestBrowser_DefaultSortDeterministic pins the browser's default order (Family then
// canonical key) and that an SSE re-render with no view-state is byte-stable — the
// stability the SSE seam depends on (team steer: a documented, tested default sort).
func TestBrowser_DefaultSortDeterministic(t *testing.T) {
	s := newTestServer(t, bestiary.Entities())
	if len(s.rows) < 2 {
		t.Skip("need >= 2 rows")
	}
	// s.rows is sorted Family-then-key.
	for i := 1; i < len(s.rows); i++ {
		a, b := s.rows[i-1], s.rows[i]
		if a.Family > b.Family || (a.Family == b.Family && a.Key > b.Key) {
			t.Fatalf("default order violated at %d: (%q,%q) before (%q,%q)", i, a.Family, a.Key, b.Family, b.Key)
		}
	}
	// Two empty-signal SSE renders are identical (deterministic re-render).
	first := sseGet(t, s, `{}`)
	second := sseGet(t, s, `{}`)
	if first != second {
		t.Error("empty-signal SSE re-render is not byte-stable")
	}
}

// TestBrowser_FacetFilter is the R9 "filters" case: a family-facet signal narrows the
// result set to that family only. Uses two real families so the exact-match filter is
// exercised over live data.
func TestBrowser_FacetFilter(t *testing.T) {
	entities := bestiary.Entities()
	s := newTestServer(t, entities)

	// Pick two distinct families with a representative key each.
	byFam := map[string]string{}
	for _, r := range s.rows {
		if r.Family != "" {
			byFam[r.Family] = r.Key
		}
	}
	if len(byFam) < 2 {
		t.Skip("need >= 2 families")
	}
	var famA, keyA, famB, keyB string
	for f, k := range byFam {
		if famA == "" {
			famA, keyA = f, k
			continue
		}
		famB, keyB = f, k
		break
	}

	body := sseGet(t, s, fmt.Sprintf(`{"family":%q}`, famA))
	if !strings.Contains(body, keyA) {
		t.Errorf("family=%q filter dropped its own entity %q", famA, keyA)
	}
	if strings.Contains(body, ">"+keyB+"<") {
		t.Errorf("family=%q filter leaked a %q-family entity %q", famA, famB, keyB)
	}

	// A non-existent facet value yields the empty-state row, not a leak of everything.
	empty := sseGet(t, s, `{"family":"__no_such_family__"}`)
	if !strings.Contains(empty, "no entities match") {
		t.Errorf("impossible filter did not yield the empty state")
	}
}

// detailFixture is a hand-built entity exercising every RQ2 detail section deterministically:
// two quant rows (one full estimate, one PARTIAL weights-only lower bound), instances that
// mint provider-id nomina with attestations, and a family/version that computes to a Series.
func detailFixture() bestiary.Entity {
	return bestiary.Entity{
		Ref: bestiary.EntityRef{Family: "llama", Version: "3.1", ParamSize: "8b", Modifier: []string{"instruct"}},
		Instances: []bestiary.ProviderInstance{{
			ID:       "llama3.1:8b",
			Provider: bestiary.Provider("ollama"),
			QuantVRAM: []bestiary.QuantVRAM{
				{QuantRaw: "Q4_K_M", WeightsBytes: 5_000_000_000, VRAMBytes: 6_000_000_000, VRAMContextTokens: 8192},
				{QuantRaw: "Q8_0", WeightsBytes: 9_000_000_000, VRAMBytes: 9_000_000_000, VRAMContextTokens: 8192, VRAMEstimatePartial: true},
			},
		}},
	}
}

// TestDetail_AllFourSections is the R9 "detail shows an entity's quants+nomina+
// attestations+series" case, plus the team steer that a PARTIAL VRAM estimate is rendered
// visually distinct AND labelled. Fully offline over a hand-built fixture.
func TestDetail_AllFourSections(t *testing.T) {
	e := detailFixture()
	s := newTestServer(t, []bestiary.Entity{e})
	body := get(t, s, e.Ref.IRI(entityRoutePrefix), "text/html").Body.String()

	for _, want := range []string{
		"quants &amp; vram", // section 1
		"Q4_K_M", "Q8_0",    // quant rows
		"pricing by provider",               // section 2
		"llama3.1:8b",                       // the (ID,Provider) instance
		"nomina &amp; attestations",         // section 3
		"authority", "method", "source-url", // attestation columns (multi-attestation showcase)
		"secondary", "harvested", // the provider-id nomen's attestation legs
		"series",    // section 4b
		"llama-3.1", // computed Series display
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail page missing %q", want)
		}
	}

	// PARTIAL honesty: the weights-only row carries both the "partial" label and the
	// hollow/dashed bar class; a page with a partial row must show both.
	if !strings.Contains(body, `class="badge warn"`) {
		t.Error("partial VRAM row is not labelled")
	}
	if !strings.Contains(body, "vram-bar partial") {
		t.Error("partial VRAM row bar is not rendered visually distinct (missing .partial)")
	}
}

// TestDetail_CtxRecompute pins the ?ctx display-only VRAM recompute (in-scope §17.5 display,
// NOT the deferred v0.2.9 calculator): the param adds a recomputed column but never changes
// which entity is shown.
func TestDetail_CtxRecompute(t *testing.T) {
	e := detailFixture()
	s := newTestServer(t, []bestiary.Entity{e})
	path := e.Ref.IRI(entityRoutePrefix)

	base := get(t, s, path, "text/html").Body.String()
	if strings.Contains(base, "vram @") {
		t.Error("recompute column shown without a ?ctx override")
	}
	withCtx := get(t, s, path+"?ctx=4096", "text/html").Body.String()
	if !strings.Contains(withCtx, "vram @4096") {
		t.Error("?ctx=4096 did not add the recomputed VRAM column")
	}
	// Identity is unchanged: still the same entity key title.
	if !strings.Contains(withCtx, e.Ref.String()) {
		t.Error("?ctx changed which entity is shown (identity must ignore view-state)")
	}
}

// TestDetail_StripParams_NoQuant confirms the RQ1 byte-identity fence still holds for an
// entity with no quant rows: ?ctx has nothing to recompute, so the page is byte-identical —
// the ctx view-state only ever affects the quants section, never identity.
func TestDetail_StripParams_NoQuant(t *testing.T) {
	e := bestiary.Entity{Ref: bestiary.EntityRef{Family: "deepseek", Variant: "v3.2"}}
	s := newTestServer(t, []bestiary.Entity{e})
	path := e.Ref.IRI(entityRoutePrefix)
	base := get(t, s, path, "text/html").Body.String()
	withCtx := get(t, s, path+"?ctx=8192", "text/html").Body.String()
	if base != withCtx {
		t.Error("?ctx changed a quant-less entity page: it must only touch the quants section")
	}
}

// TestSeriesExplorer_WalksTree is the R9 "explorer walks series→releases→entities" case: the
// /series page renders the disclosure tree over SeriesAll()/ReleasesOf()/EntitiesOf(), and a
// concrete series→release→entity path is present with a navigable entity link. Offline.
func TestSeriesExplorer_WalksTree(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)
	rec := get(t, s, "/series", "text/html")
	if rec.Code != 200 {
		t.Fatalf("GET /series = %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// Walk the same relations the page walks and assert a concrete leaf is present.
	all := bestiary.SeriesAll()
	if len(all) == 0 {
		t.Fatal("SeriesAll() empty over a non-empty registry")
	}
	var walked bool
	for _, ser := range all {
		anchor := seriesAnchor(ser)
		if !strings.Contains(body, `id="`+anchor+`"`) {
			t.Errorf("series %q missing its anchor %q on the explorer page", ser.String(), anchor)
		}
		for _, rel := range bestiary.ReleasesOf(ser) {
			ents := bestiary.EntitiesOf(rel)
			if len(ents) == 0 {
				continue
			}
			key := ents[0].Ref.String()
			href := `href="` + ents[0].Ref.IRI(entityRoutePrefix) + `"`
			if !strings.Contains(body, href) {
				t.Errorf("explorer missing entity link %q (series %q)", href, ser.String())
			}
			if !strings.Contains(body, ">"+key+"<") {
				t.Errorf("explorer missing entity key %q text", key)
			}
			walked = true
			break
		}
		if walked {
			break
		}
	}
	if !walked {
		t.Error("could not walk any series→release→entity path")
	}
}

// TestSeriesLink_DetailToExplorer pins the cross-view link integrity: the anchor a detail
// page links to (/series#<anchor>) is exactly an anchor the explorer emits, so the "series"
// section on a detail page always resolves to a real spot in the tree.
func TestSeriesLink_DetailToExplorer(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)

	e := entities[0]
	detail := get(t, s, e.Ref.IRI(entityRoutePrefix), "text/html").Body.String()
	anchor := seriesAnchor(bestiary.SeriesOf(e.Ref))
	wantLink := `href="/series#` + anchor + `"`
	if !strings.Contains(detail, wantLink) {
		t.Errorf("detail page missing series link %q", wantLink)
	}
	explorer := get(t, s, "/series", "text/html").Body.String()
	if !strings.Contains(explorer, `id="`+anchor+`"`) {
		t.Errorf("explorer missing the anchor %q the detail page links to", anchor)
	}
}
