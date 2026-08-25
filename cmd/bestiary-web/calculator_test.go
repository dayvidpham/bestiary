package main

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// calcEntities is the injected corpus for the calculator page tests: one measured entity
// with full architecture facts, one measured entity with none (a weights-only lower
// bound), one sized entity with an attested total (the derived class), and one sized
// entity whose NxM token attests no total (the exclusion). The page assertions must hold
// for any corpus, so they are driven from entities the test owns rather than from
// whatever the committed registry happens to contain.
func calcEntities() []bestiary.Entity {
	measured := func(fam string, weights int64, layers, kvHeads, headDim, ctx int) bestiary.Entity {
		return bestiary.Entity{
			Ref: bestiary.EntityRef{Family: bestiary.Family(fam), Version: "1"},
			Instances: []bestiary.ProviderInstance{{
				ID:            bestiary.ModelID(fam),
				Provider:      "fixture",
				ContextWindow: ctx,
				QuantVRAM: []bestiary.QuantVRAM{{
					Quant:               bestiary.QuantQ4_K_M,
					QuantRaw:            "Q4_K_M",
					WeightsBytes:        weights,
					Layers:              layers,
					KVHeads:             kvHeads,
					HeadDim:             headDim,
					VRAMEstimatePartial: bestiary.VRAMEstimateIsPartial(layers, kvHeads, headDim),
				}},
			}},
		}
	}
	sized := func(fam, size string) bestiary.Entity {
		return bestiary.Entity{
			Ref: bestiary.EntityRef{Family: bestiary.Family(fam), Version: "1", ParamSize: size},
			Instances: []bestiary.ProviderInstance{{
				ID: bestiary.ModelID(fam), Provider: "fixture", ContextWindow: 32768,
			}},
		}
	}
	return []bestiary.Entity{
		measured("archfacts", 4<<30, 32, 8, 128, 131072),
		measured("noarchfacts", 3<<30, 0, 0, 0, 131072),
		sized("derivable", "7b"),
		sized("moe", "8x7b"),
	}
}

func calcServer(t *testing.T) *Server {
	t.Helper()
	return newTestServer(t, calcEntities())
}

// TestCalculator_Page renders /calculator and pins the page contract: the budget controls
// exist and are bound to signals, the vendored client is loaded, the results container is
// the one the SSE seam patches, and rows carry their weights and context cells.
func TestCalculator_Page(t *testing.T) {
	s := calcServer(t)
	rec := get(t, s, "/calculator", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /calculator = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="calc-results"`,
		"data-bind-budget",
		"data-bind-headroom",
		"data-bind-min-context",
		"@get('/sse/calculator')",
		`src="/assets/datastar.js"`,
		"available after headroom",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/calculator body missing %q", want)
		}
	}
	// The default budget is a preset, and the headroom preset is NON-ZERO: a fit verdict
	// computed with no slack over-promises, because the stored formula carries no
	// runtime-overhead term.
	if calcDefaultHeadroomGiB <= 0 {
		t.Error("the headroom preset is zero; the calculator would over-promise by default")
	}
	if !strings.Contains(body, `data-signals-headroom="'`+trimFloat(calcDefaultHeadroomGiB)+`'"`) {
		t.Errorf("/calculator did not seed the headroom signal with the %g GiB preset", float64(calcDefaultHeadroomGiB))
	}
}

// TestCalculator_SSE drives the datastar seam end to end: budget signals are read, the
// fit is recomputed, and the results are PatchElements-ed into #calc-results. It also
// pins that the pre-existing #entity-results seam is untouched by this endpoint.
func TestCalculator_SSE(t *testing.T) {
	s := calcServer(t)
	q := url.Values{}
	q.Set("datastar", `{"budget":"24","headroom":"2","minContext":"0"}`)
	rec := get(t, s, "/sse/calculator?"+q.Encode(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sse/calculator = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"datastar-patch-elements", "#calc-results", "archfacts"} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE body missing %q\nbody:\n%s", want, body)
		}
	}
	if strings.Contains(body, "#entity-results") {
		t.Error("the calculator seam patched the entity browser's container")
	}
}

// TestCalculator_BudgetFilters is the AC-4 core: only rows whose weights clear
// budget minus headroom are listed, and the headroom is load-bearing rather than
// decorative.
func TestCalculator_BudgetFilters(t *testing.T) {
	s := calcServer(t)
	// 5 GiB total with 2 GiB headroom leaves 3 GiB: the 4 GiB measured entity is out,
	// the 3 GiB one is in.
	view := s.calcView(calcQuery{Budget: "5", Headroom: "2"})
	keys := map[string]bool{}
	for _, r := range view.Rows {
		keys[r.Key] = true
	}
	if keys["archfacts@1"] {
		t.Error("a 4 GiB model was listed against a 3 GiB available budget")
	}
	if !keys["noarchfacts@1"] {
		t.Error("a 3 GiB model was not listed against a 3 GiB available budget")
	}
	// Drop the headroom and the same total budget admits the larger model.
	view = s.calcView(calcQuery{Budget: "5", Headroom: "0"})
	found := false
	for _, r := range view.Rows {
		if r.Key == "archfacts@1" {
			found = true
		}
	}
	if !found {
		t.Error("removing the headroom did not admit the 4 GiB model; the headroom is not load-bearing")
	}
}

// TestCalculator_ContextCells_NeverUnbounded pins the two non-fit readings and the third,
// real one. A row whose KV term is not computable reads "unknown"; a row whose computable
// KV budget is spent reads "no context budget remaining"; neither is ever an infinity, and
// neither is presented as an unqualified fit.
func TestCalculator_ContextCells_NeverUnbounded(t *testing.T) {
	s := calcServer(t)
	view := s.calcView(calcQuery{Budget: "24", Headroom: "0"})

	var unknown, real bool
	for _, r := range view.Rows {
		switch r.Key {
		case "noarchfacts@1":
			unknown = true
			if r.Context != "unknown" {
				t.Errorf("a row with no architecture facts reads %q, want %q", r.Context, "unknown")
			}
			if r.Bound != "" {
				t.Errorf("a non-computable row named a bound (%q); there is none to name", r.Bound)
			}
		case "archfacts@1":
			real = true
			if !strings.HasSuffix(r.Context, "tokens") {
				t.Errorf("a computable row reads %q, want a token count", r.Context)
			}
			if r.Bound == "" {
				t.Error("a computable row did not name which limit produced its context")
			}
		}
	}
	if !unknown || !real {
		t.Fatalf("fixture rows missing (unknown=%v computable=%v)", unknown, real)
	}
	for _, r := range view.Rows {
		for _, forbidden := range []string{"∞", "&infin;", "unlimited", "infinite"} {
			if strings.Contains(r.Context, forbidden) {
				t.Errorf("row %s reports %q as its context", r.Key, r.Context)
			}
		}
	}

	// A budget that clears the weights and nothing more: the KV budget is exhausted and
	// says so in words.
	exhausted := s.calcView(calcQuery{Budget: "4", Headroom: "0"})
	saw := false
	for _, r := range exhausted.Rows {
		if r.Key == "archfacts@1" {
			saw = true
			if r.Context != "no context budget remaining" {
				t.Errorf("an exhausted KV budget reads %q, want %q", r.Context, "no context budget remaining")
			}
		}
	}
	if !saw {
		t.Error("the exhausted row was dropped instead of being listed with its qualification")
	}
}

// TestCalculator_MinContext_ExcludesUnknownAndExhausted pins the AC-4 filter clause at the
// page level: a positive context floor drops BOTH the not-computable and the exhausted
// rows, because neither can promise the reader a token.
func TestCalculator_MinContext_ExcludesUnknownAndExhausted(t *testing.T) {
	s := calcServer(t)
	// 4 GiB available: archfacts (4 GiB) is exhausted, noarchfacts (3 GiB) is unknown.
	unfiltered := s.calcView(calcQuery{Budget: "4", Headroom: "0"})
	var sawExhausted, sawUnknown bool
	for _, r := range unfiltered.Rows {
		if r.Context == "no context budget remaining" {
			sawExhausted = true
		}
		if r.Context == "unknown" {
			sawUnknown = true
		}
	}
	if !sawExhausted || !sawUnknown {
		t.Fatalf("fixture did not produce both non-fit readings (exhausted=%v unknown=%v)", sawExhausted, sawUnknown)
	}

	filtered := s.calcView(calcQuery{Budget: "4", Headroom: "0", MinContext: "1"})
	for _, r := range filtered.Rows {
		if r.Context == "unknown" || r.Context == "no context budget remaining" {
			t.Errorf("row %s survived a context floor of 1 token with context %q", r.Key, r.Context)
		}
	}
	// The denominators describe the CORPUS, not the filter, so a context floor must not
	// move them.
	if filtered.Coverage != unfiltered.Coverage {
		t.Errorf("a context floor moved the coverage denominators: %+v -> %+v",
			unfiltered.Coverage, filtered.Coverage)
	}
}

// TestCalculator_DerivedBadge_NamesBothQualifications pins the AC-33 badge: a derived row
// says BOTH that its weights are derived and that the figure is weights-only, because a
// reader told only one of the two has been told half the truth.
func TestCalculator_DerivedBadge_NamesBothQualifications(t *testing.T) {
	s := calcServer(t)
	view := s.calcView(calcQuery{Budget: "64", Headroom: "0"})

	var derived, partialMeasured bool
	for _, r := range view.Rows {
		if r.Key == "derivable@1#7b" {
			derived = true
			if !r.Derived {
				t.Error("a row built from an attested parameter count is not typed derived")
			}
			if !strings.Contains(r.Badge, "derived") || !strings.Contains(r.Badge, "weights-only") {
				t.Errorf("derived badge %q must name BOTH derived and weights-only", r.Badge)
			}
			if r.Quant == "" {
				t.Error("a derived row lost its quantization")
			}
		}
		if r.Key == "noarchfacts@1" {
			partialMeasured = true
			if r.Derived {
				t.Error("an ingested row was typed derived")
			}
			if r.Badge != "weights-only" {
				t.Errorf("a measured partial row is badged %q, want %q", r.Badge, "weights-only")
			}
		}
	}
	if !derived || !partialMeasured {
		t.Fatalf("fixture rows missing (derived=%v measured-partial=%v)", derived, partialMeasured)
	}
	// The mixture-of-experts entity attests no total, so it must not appear at all.
	for _, r := range view.Rows {
		if strings.HasPrefix(r.Key, "moe@") {
			t.Errorf("an NxM entity produced a row: %+v", r)
		}
	}
	// And it is COUNTED excluded, so the statement can say why it is missing.
	if view.Coverage.Excluded != 1 {
		t.Errorf("Coverage.Excluded = %d, want 1", view.Coverage.Excluded)
	}
}

// TestCalculator_CoverageStatement_IsComputed is the AC-33 identity at the page: every
// number in the rendered sentence equals the corresponding FitResult field, and none is a
// literal in the template. The test recomputes the fields from the SAME injected corpus,
// so it stays true at any corpus size.
func TestCalculator_CoverageStatement_IsComputed(t *testing.T) {
	ents := calcEntities()
	s := newTestServer(t, ents)
	view := s.calcView(calcQuery{Budget: "64", Headroom: "0"})
	res := bestiary.FitOver(ents, calcQuery{Budget: "64", Headroom: "0"}.filter())

	if view.Coverage.Considered != res.EntitiesConsidered ||
		view.Coverage.Measured != res.EntitiesMeasured ||
		view.Coverage.Derived != res.EntitiesDerived ||
		view.Coverage.Excluded != res.EntitiesExcluded {
		t.Fatalf("view coverage %+v does not equal the FitResult fields (%d/%d/%d/%d)",
			view.Coverage, res.EntitiesConsidered, res.EntitiesMeasured, res.EntitiesDerived, res.EntitiesExcluded)
	}

	body := get(t, s, "/calculator", "text/html").Body.String()
	for _, want := range []string{
		fmt.Sprintf("<strong>%d of %d entities</strong>", res.EntitiesMeasured, res.EntitiesConsidered),
		fmt.Sprintf("<strong>%d entities</strong>", res.EntitiesDerived),
		fmt.Sprintf("<strong>%d mixture-of-experts entities are excluded</strong>", res.EntitiesExcluded),
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the rendered coverage statement does not carry %q", want)
		}
	}
	// The wording that qualifies every derived figure must be present verbatim: the
	// numbers alone do not tell a reader that a KV term is missing everywhere.
	for _, want := range []string{
		"architecture facts needed for a KV-cache term",
		"weights-only lower bound and real VRAM will be higher",
		"only an active count",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the coverage statement is missing the qualification %q", want)
		}
	}
	// Non-vacuity: the injected corpus really does populate all three classes, so the
	// identity above is not a comparison of zeros.
	if res.EntitiesMeasured == 0 || res.EntitiesDerived == 0 || res.EntitiesExcluded == 0 {
		t.Errorf("a coverage class is empty (%d/%d/%d); the identity is vacuous",
			res.EntitiesMeasured, res.EntitiesDerived, res.EntitiesExcluded)
	}
	// The denominators are not literals in the template: rendering the SAME page over a
	// corpus one entity shorter must move the considered count.
	shorter := newTestServer(t, ents[:len(ents)-1])
	if shorter.calcView(calcQuery{Budget: "64"}).Coverage.Considered == view.Coverage.Considered {
		t.Error("the considered count did not move with the corpus; it is not computed")
	}
}

// TestCalculator_MalformedSignals_DegradeToDefaults pins the tolerant read: a junk budget
// is the default view, never a 400 (the parseCtx precedent). A deliberate zero headroom is
// honoured, because a reader who set it has said something.
func TestCalculator_MalformedSignals_DegradeToDefaults(t *testing.T) {
	s := calcServer(t)
	for _, q := range []calcQuery{
		{Budget: "abc"}, {Budget: "-5"}, {Budget: ""}, {MinContext: "nope"}, {Headroom: "??"},
	} {
		view := s.calcView(q)
		if view.BudgetGiB != trimFloat(calcDefaultBudgetGiB) && q.Budget != "" && q.Budget != "abc" && q.Budget != "-5" {
			continue
		}
		if view.BudgetGiB != trimFloat(calcDefaultBudgetGiB) {
			t.Errorf("query %+v produced budget %q, want the %g GiB preset", q, view.BudgetGiB, float64(calcDefaultBudgetGiB))
		}
	}
	if got := s.calcView(calcQuery{Headroom: "0"}).HeadroomGiB; got != "0" {
		t.Errorf("an explicit zero headroom rendered as %q; a deliberate zero must be honoured", got)
	}
	rec := get(t, s, "/calculator?budget=abc&headroom=-1&minContext=x", "text/html")
	if rec.Code != http.StatusOK {
		t.Errorf("a malformed query returned %d; malformed view-state must degrade to the default view", rec.Code)
	}
}

// TestCalculator_NeverWritesBaked is the AC-4 must-not, at the page: rendering the
// calculator at any budget leaves every QuantVRAM row byte-identical and does not touch
// the shipped formula version.
func TestCalculator_NeverWritesBaked(t *testing.T) {
	ents := calcEntities()
	before := ents[0].Instances[0].QuantVRAM[0]
	s := newTestServer(t, ents)
	for _, b := range []string{"1", "8", "24", "512"} {
		_ = get(t, s, "/calculator?budget="+b, "text/html")
		_ = s.calcView(calcQuery{Budget: b, Headroom: "3", MinContext: "4096"})
	}
	if ents[0].Instances[0].QuantVRAM[0] != before {
		t.Error("rendering the calculator mutated a QuantVRAM row")
	}
	if bestiary.VRAMFormulaVersion != 2 {
		t.Errorf("VRAMFormulaVersion = %d, want 2", bestiary.VRAMFormulaVersion)
	}
	// The weights figure a row displays is the same string at every context floor.
	base := s.calcView(calcQuery{Budget: "64"})
	weights := map[string]string{}
	for _, r := range base.Rows {
		weights[r.Key+"|"+r.Quant] = r.Weights
	}
	for _, min := range []string{"1", "1024", "8192"} {
		for _, r := range s.calcView(calcQuery{Budget: "64", MinContext: min}).Rows {
			if w, ok := weights[r.Key+"|"+r.Quant]; ok && w != r.Weights {
				t.Errorf("minContext=%s changed %s's weights figure: %q -> %q", min, r.Key, w, r.Weights)
			}
		}
	}
}

// TestCalculator_TruncationIsStated pins the render cap's honesty: when the cap bites the
// page says so and names both numbers, rather than presenting a truncated table as if it
// were everything that fits.
func TestCalculator_TruncationIsStated(t *testing.T) {
	// One derivable entity yields one row per derivable quantization, per instance; a
	// handful of instances clears the cap without any dependence on the real catalog.
	inst := make([]bestiary.ProviderInstance, 0, 32)
	for i := 0; i < 32; i++ {
		inst = append(inst, bestiary.ProviderInstance{
			ID: bestiary.ModelID("m" + strconv.Itoa(i)), Provider: bestiary.Provider("p" + strconv.Itoa(i)),
			ContextWindow: 32768,
		})
	}
	e := bestiary.Entity{Ref: bestiary.EntityRef{Family: "many", Version: "1", ParamSize: "1b"}, Instances: inst}
	s := newTestServer(t, []bestiary.Entity{e})
	view := s.calcView(calcQuery{Budget: "64", Headroom: "0"})
	if !view.Truncated {
		t.Fatalf("the fixture produced %d rows, which did not exceed the %d-row cap", view.RowsTotal, calcMaxRows)
	}
	if view.RowsShown != calcMaxRows || view.RowsTotal <= view.RowsShown {
		t.Errorf("shown=%d total=%d, want shown == the cap (%d) and total greater", view.RowsShown, view.RowsTotal, calcMaxRows)
	}
	body := get(t, s, "/calculator?budget=64&headroom=0", "text/html").Body.String()
	want := fmt.Sprintf("Showing the largest %d of %d rows", view.RowsShown, view.RowsTotal)
	if !strings.Contains(body, want) {
		t.Errorf("the page does not state the truncation (%q missing)", want)
	}
}

// TestCalculator_RowsLinkToTheEntityRoute pins that a calculator row dereferences: its
// href is the SAME IRI the entity route matches, so a reader can go straight from a fit
// verdict to the entity it is about.
func TestCalculator_RowsLinkToTheEntityRoute(t *testing.T) {
	s := calcServer(t)
	view := s.calcView(calcQuery{Budget: "64", Headroom: "0"})
	if len(view.Rows) == 0 {
		t.Fatal("no rows to check")
	}
	for _, r := range view.Rows {
		if !strings.HasPrefix(r.Path, entityRoutePrefix) {
			t.Errorf("row %s links to %q, which the entity route does not match", r.Key, r.Path)
		}
		if rec := get(t, s, r.Path, "text/html"); rec.Code != http.StatusOK {
			t.Errorf("row %s links to %q, which returned %d", r.Key, r.Path, rec.Code)
		}
	}
}

// TestCommaInt pins the thousands grouping used by every token count on the page.
func TestCommaInt(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000", 32768: "32,768",
		131072: "131,072", 1000000: "1,000,000", -4096: "-4,096",
	} {
		if got := commaInt(in); got != want {
			t.Errorf("commaInt(%d) = %q, want %q", in, got, want)
		}
	}
}

// calcTemplateSource returns the shipped bytes of the calculator page template, read out
// of the SAME embedded FS the server renders from. Reading the template rather than a
// rendered page is deliberate: the signal DECLARATIONS are what this guard is about, and a
// rendered page would let a value substitution hide a bad attribute name.
func calcTemplateSource(t *testing.T) string {
	t.Helper()
	b, err := templatesFS.ReadFile("templates/calculator.html")
	if err != nil {
		t.Fatalf("read calculator template: %v", err)
	}
	return string(b)
}

// htmlCommentRE matches an HTML comment, including a multi-line one.
var htmlCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// signalNameAfterRoundTrip reproduces what actually happens to a signal attribute name
// between the server and the client: the HTML parser LOWERCASES the attribute name, and
// the Datastar client then folds kebab-case back to camelCase. Whatever comes out is the
// signal the browser really binds.
func signalNameAfterRoundTrip(attrSuffix string) string {
	parts := strings.Split(strings.ToLower(attrSuffix), "-")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// sortedSignalNames renders the handler's signal set for an error message in a stable
// order, so a failure reads the same way every run.
func sortedSignalNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestCalculator_SignalNamesSurviveTheParser closes a silent-failure gap that costs
// nothing to introduce and shows no error once introduced: a camelCase signal attribute
// (data-signals-minContext) reaches the client LOWERCASED as data-signals-mincontext and
// binds a signal the handler never decodes. The control would simply do nothing -- no
// console error, no failing handler, no red test -- and the page would quietly ignore its
// context floor. The hyphenated spelling is the only one that survives the round trip,
// because the client folds kebab back to camelCase.
//
// The guard walks the SHIPPED template bytes exactly that way and requires every signal
// name it finds to be one the handler actually reads, taken from the struct tags rather
// than from a second hand-written list that could drift.
func TestCalculator_SignalNamesSurviveTheParser(t *testing.T) {
	want := map[string]bool{}
	rt := reflect.TypeOf(calcQuery{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("calcQuery.%s has no json tag; the guard has nothing to pin against", rt.Field(i).Name)
		}
		want[tag] = true
	}
	if len(want) == 0 {
		t.Fatal("calcQuery declares no signals; the guard is vacuous")
	}

	// HTML comments are stripped first: this very file explains the hazard by SPELLING
	// the bad attribute name in prose, and a comment is not an attribute. Scanning the raw
	// bytes would make the explanation of the rule violate the rule.
	src := htmlCommentRE.ReplaceAllString(calcTemplateSource(t), "")
	decl := regexp.MustCompile(`data-(?:signals|bind)-([a-zA-Z][a-zA-Z-]*)`).FindAllStringSubmatch(src, -1)
	if len(decl) == 0 {
		t.Fatal("no signal declaration found in the calculator template")
	}
	seen := map[string]bool{}
	for _, m := range decl {
		got := signalNameAfterRoundTrip(m[1])
		if !want[got] {
			t.Errorf("attribute %q binds signal %q after HTML lowercasing + kebab->camel; "+
				"calcQuery reads %v -- declare it kebab-case, never camelCase",
				m[0], got, sortedSignalNames(want))
		}
		seen[got] = true
	}
	// Every signal the handler reads must actually be declared, or its control is missing
	// and the field is dead.
	for name := range want {
		if !seen[name] {
			t.Errorf("calcQuery reads signal %q, which no control in the template declares", name)
		}
	}
	// Non-vacuity: at least one signal name must be MULTI-WORD, since a single all-lowercase
	// word survives the round trip however it is spelled and would prove nothing.
	multiword := false
	for name := range want {
		if strings.ToLower(name) != name {
			multiword = true
		}
	}
	if !multiword {
		t.Error("no camelCase signal name remains; the guard no longer proves anything")
	}
}
