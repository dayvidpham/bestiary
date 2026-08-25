package main

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// paletteSSE drives GET /sse/palette with one search signal and returns the SSE body. It
// goes through the SAME handler and mux the browser hits — there is no test-only entry
// point into the palette.
func paletteSSE(t *testing.T, s *Server, term string) string {
	t.Helper()
	q := url.Values{}
	q.Set("datastar", fmt.Sprintf(`{"paletteQuery":%q}`, term))
	rec := get(t, s, "/sse/palette?"+q.Encode(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sse/palette?paletteQuery=%q = %d, want 200", term, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	return rec.Body.String()
}

// TestServer_SSE_Palette drives the palette's wiring end to end: the search signal is read,
// the option list is rendered, and it is PatchElements-ed into #palette-results — the same
// SDK path the browser's results table uses, against a different target element.
func TestServer_SSE_Palette(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	body := paletteSSE(t, s, "llama")

	for _, want := range []string{
		"datastar-patch-elements",
		"#palette-results",
		`role="option"`,
		"llama",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("palette SSE body missing %q\nbody:\n%s", want, body)
		}
	}
	// The search is honored: a non-matching entity must not be offered.
	if strings.Contains(body, "deepseek") {
		t.Errorf("palette search=llama leaked a non-matching entity (deepseek)\nbody:\n%s", body)
	}
}

// TestServer_SSE_Palette_TargetsOnlyItsOwnContainer is the co-existence fence with the dense
// browser: the palette patches #palette-results and MUST NOT name #entity-results, or a
// keystroke in the dialog would rewrite the table behind it.
func TestServer_SSE_Palette_TargetsOnlyItsOwnContainer(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	body := paletteSSE(t, s, "llama")
	if strings.Contains(body, "#entity-results") {
		t.Errorf("palette SSE targeted the browser's results container (#entity-results)\nbody:\n%s", body)
	}
}

// TestServer_SSE_Palette_EmptyQuery pins the dialog's OPENING state: nothing typed offers
// nothing. Pre-loading arbitrary entities would put a row under Enter that the reader never
// chose, so the empty term must match zero entities — not "the first ten".
func TestServer_SSE_Palette_EmptyQuery(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	body := paletteSSE(t, s, "")
	if strings.Contains(body, `role="option"`) {
		t.Errorf("empty palette query offered options\nbody:\n%s", body)
	}
	if !strings.Contains(body, "type to search entities") {
		t.Errorf("empty palette query did not render the prompt state\nbody:\n%s", body)
	}
}

// TestServer_SSE_Palette_NoMatch distinguishes "nothing typed" from "nothing matches": the
// two states read differently to the reader and must render differently.
func TestServer_SSE_Palette_NoMatch(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	body := paletteSSE(t, s, "zzz-no-such-entity")
	if strings.Contains(body, `role="option"`) {
		t.Errorf("non-matching palette query offered options\nbody:\n%s", body)
	}
	if !strings.Contains(body, "no entities match") {
		t.Errorf("non-matching palette query did not render the no-match state\nbody:\n%s", body)
	}
	if strings.Contains(body, "type to search entities") {
		t.Errorf("non-matching palette query rendered the opening prompt instead of a no-match state")
	}
}

// TestPalette_EnterNavigatesToEntityRoute is the server-side half of the Enter contract:
// every option's data-path is the entity's own IRI under /entity/, and GETting it yields
// that entity's page. The browser-side handler does nothing but location.assign() this
// value, so a green here means Enter lands on the right page.
func TestPalette_EnterNavigatesToEntityRoute(t *testing.T) {
	entities := syntheticEntities()
	s := newTestServer(t, entities)

	v := s.palette(paletteQuery{PaletteQuery: "llama"})
	if len(v.Options) == 0 {
		t.Fatal("palette returned no options for llama")
	}
	for _, o := range v.Options {
		want := s.byKey[o.Key].Ref.IRI(entityRoutePrefix)
		if o.Path != want {
			t.Errorf("option %q path = %q, want the entity IRI %q", o.Key, o.Path, want)
		}
		if !strings.HasPrefix(o.Path, entityRoutePrefix) {
			t.Errorf("option %q path %q is not under %q", o.Key, o.Path, entityRoutePrefix)
		}
		if rec := get(t, s, o.Path, "text/html"); rec.Code != http.StatusOK {
			t.Errorf("GET %q (palette option %q) = %d, want 200", o.Path, o.Key, rec.Code)
		}
	}
}

// TestPalette_OptionIDsAreDenseAndUnique pins the aria-activedescendant contract: the input
// points at an option BY ID, and the browser-side handler walks the rendered options by
// position. Duplicate or sparse ids would make the pointer ambiguous.
func TestPalette_OptionIDsAreDenseAndUnique(t *testing.T) {
	s := newTestServer(t, bestiary.Entities())
	if len(s.rows) == 0 {
		t.Skip("no registry entities")
	}
	v := s.palette(paletteQuery{PaletteQuery: "a"})
	if len(v.Options) == 0 {
		t.Fatal("palette returned no options for a corpus-wide term")
	}
	seen := map[string]bool{}
	for i, o := range v.Options {
		if want := paletteOptionID(i); o.OptionID != want {
			t.Errorf("option %d id = %q, want the positional id %q", i, o.OptionID, want)
		}
		if seen[o.OptionID] {
			t.Errorf("duplicate option id %q: aria-activedescendant would be ambiguous", o.OptionID)
		}
		seen[o.OptionID] = true
	}
}

// TestPalette_RanksKeyPrefixFirst pins the relevance order: a term that PREFIXES a canonical
// key is what the reader almost always meant, so those options precede a mid-key match, and
// an attribution-only match (family/creator) comes last.
func TestPalette_RanksKeyPrefixFirst(t *testing.T) {
	rows := []entityRow{
		{Key: "zeta/alpha", Family: "zeta", Creator: "acme"}, // mid-key match on "alpha"
		{Key: "alpha@1", Family: "alpha", Creator: "acme"},   // key prefix
		{Key: "beta", Family: "beta", Creator: "alphacorp"},  // creator-only match
	}
	s := &Server{rows: rows}
	v := s.palette(paletteQuery{PaletteQuery: "alpha"})
	got := make([]string, 0, len(v.Options))
	for _, o := range v.Options {
		got = append(got, o.Key)
	}
	want := []string{"alpha@1", "zeta/alpha", "beta"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("palette rank order = %v, want %v (key prefix, then key substring, then attribution)", got, want)
	}
	if v.Total != 3 {
		t.Errorf("Total = %d, want 3", v.Total)
	}
}

// TestPalette_CapIsSurfacedNotSilent pins the no-silent-caps rule: the option list is capped
// at paletteMaxResults, and when it truncates the fragment says how many matched in total.
// A cap the reader cannot see reads as "that is everything" when it is not.
func TestPalette_CapIsSurfacedNotSilent(t *testing.T) {
	rows := make([]entityRow, 0, paletteMaxResults+5)
	for i := 0; i < paletteMaxResults+5; i++ {
		rows = append(rows, entityRow{Key: fmt.Sprintf("capfam@%d", i), Family: "capfam"})
	}
	s := &Server{rows: rows}

	v := s.palette(paletteQuery{PaletteQuery: "capfam"})
	if len(v.Options) != paletteMaxResults {
		t.Errorf("options = %d, want the cap %d", len(v.Options), paletteMaxResults)
	}
	if v.Total != paletteMaxResults+5 {
		t.Errorf("Total = %d, want the uncapped match count %d", v.Total, paletteMaxResults+5)
	}
	if !v.Truncated() {
		t.Fatal("Truncated() = false on a capped list")
	}

	frag, err := parseFragments()
	if err != nil {
		t.Fatalf("parseFragments: %v", err)
	}
	var sb strings.Builder
	if err := frag.ExecuteTemplate(&sb, "palette-options", v); err != nil {
		t.Fatalf("render palette-options: %v", err)
	}
	if want := fmt.Sprintf("showing %d of %d", paletteMaxResults, paletteMaxResults+5); !strings.Contains(sb.String(), want) {
		t.Errorf("truncated fragment did not state the cap (%q)\nfragment:\n%s", want, sb.String())
	}
}

// TestPalette_OptionsAreNotFocusable is the "never focus" half of the combobox contract: the
// pointer moves by aria-activedescendant while DOM focus stays in the input, which is what
// lets the reader keep typing while arrowing. A focusable option row would break that.
func TestPalette_OptionsAreNotFocusable(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	body := paletteSSE(t, s, "llama")
	if strings.Contains(body, "tabindex") {
		t.Errorf("palette options carry a tabindex: options must never take DOM focus\nbody:\n%s", body)
	}
	if strings.Contains(body, "<a ") || strings.Contains(body, "<button") {
		t.Errorf("palette options contain a natively focusable element\nbody:\n%s", body)
	}
}

// ---- layout guards: the dialog markup and its keyboard contract ----------------------------

// TestLayout_PaletteCombobox pins the accessible structure in the SHIPPED layout bytes: a
// native <dialog>, an ARIA combobox input that owns aria-activedescendant, and a listbox
// popup addressed as #palette-results.
func TestLayout_PaletteCombobox(t *testing.T) {
	src := layoutSource(t)
	for _, want := range []string{
		`<dialog id="palette"`,
		`id="palette-input"`,
		`role="combobox"`,
		`aria-controls="palette-results"`,
		`aria-autocomplete="list"`,
		`id="palette-results"`,
		`role="listbox"`,
		"aria-activedescendant",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("layout.html is missing the combobox construct %q", want)
		}
	}
}

// TestLayout_PaletteHotkey pins ⌘K / Ctrl-K as the opener, bound through the vendored
// client's own __window keydown hook — the reason the hotkey costs no new dependency.
func TestLayout_PaletteHotkey(t *testing.T) {
	src := layoutSource(t)
	for _, want := range []string{
		"data-on-keydown__window",
		"evt.metaKey",
		"evt.ctrlKey",
		"__paletteOpen()",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("layout.html is missing the ⌘K/Ctrl-K opener construct %q", want)
		}
	}
}

// TestLayout_PaletteKeysMovePointerNotFocus pins the arrow-key contract at the source: the
// handler answers ArrowUp/ArrowDown by rewriting aria-activedescendant (setActive) and must
// never call .focus() on an option row.
func TestLayout_PaletteKeysMovePointerNotFocus(t *testing.T) {
	src := layoutSource(t)
	for _, want := range []string{`"ArrowDown"`, `"ArrowUp"`, `"Enter"`, "setActive(", "location.assign("} {
		if !strings.Contains(src, want) {
			t.Errorf("palette keyboard handler is missing %q", want)
		}
	}
	// The only .focus() in the palette handler is the input's, when the dialog opens.
	focusCalls := regexp.MustCompile(`(\w+)\.focus\(\)`).FindAllStringSubmatch(src, -1)
	for _, m := range focusCalls {
		if m[1] != "inp" {
			t.Errorf("palette handler focuses %q; arrow keys must move aria-activedescendant, never focus", m[0])
		}
	}
}

// TestLayout_PaletteQueriesItsOwnSSERoute pins the wiring seam the dialog uses, and the fence
// against the browser's: the palette input queries /sse/palette and nothing else.
func TestLayout_PaletteQueriesItsOwnSSERoute(t *testing.T) {
	src := layoutSource(t)
	if !strings.Contains(src, "@get('/sse/palette')") {
		t.Error("palette input does not query /sse/palette")
	}
	if strings.Contains(src, "/sse/entities") {
		t.Error("the shared layout queries /sse/entities: that seam belongs to the browser page")
	}
}

// TestLayout_PaletteAddsNoBrowserDependency is AC-6's zero-new-dependency guard at the one
// place a web feature usually breaks it: the shipped layout must load exactly one external
// script, the vendored same-origin datastar client. A CDN <script> or a new module import
// would redden here even though go.mod stayed clean.
func TestLayout_PaletteAddsNoBrowserDependency(t *testing.T) {
	srcs := regexp.MustCompile(`<script[^>]*\ssrc="([^"]+)"`).FindAllStringSubmatch(layoutSource(t), -1)
	if len(srcs) != 1 {
		t.Fatalf("layout.html loads %d external scripts, want exactly 1 (the vendored datastar client): %v", len(srcs), srcs)
	}
	if got := srcs[0][1]; got != "/assets/datastar.js" {
		t.Errorf("layout.html loads external script %q, want the vendored /assets/datastar.js", got)
	}
}

// TestLayout_PaletteIsSearchAndNavigateOnly pins the scope fence: the palette offers entity
// rows only. A page-nav or view-action row would make Enter mean different things depending
// on what happens to be highlighted.
func TestLayout_PaletteIsSearchAndNavigateOnly(t *testing.T) {
	s := newTestServer(t, bestiary.Entities())
	if len(s.rows) == 0 {
		t.Skip("no registry entities")
	}
	v := s.palette(paletteQuery{PaletteQuery: "a"})
	for _, o := range v.Options {
		if _, ok := s.byKey[o.Key]; !ok {
			t.Errorf("palette offered %q, which is not an entity: the palette navigates to entities only", o.Key)
		}
	}
}

// TestPalette_RealRegistrySmoke exercises the palette over the committed registry: a term
// drawn from a real key must find that key, and every option must route.
func TestPalette_RealRegistrySmoke(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)
	term := s.rows[0].Key

	v := s.palette(paletteQuery{PaletteQuery: term})
	found := false
	for _, o := range v.Options {
		if o.Key == term {
			found = true
		}
		if rec := get(t, s, o.Path, "text/html"); rec.Code != http.StatusOK {
			t.Errorf("GET %q (palette option %q) = %d, want 200", o.Path, o.Key, rec.Code)
		}
	}
	if !found {
		t.Errorf("palette search for the exact key %q did not offer it", term)
	}
}

// kebabToCamel is the vendored client's own default key conversion (`-x` -> `X`), reproduced
// here so the guard below compares the signal name the BROWSER will compute, not the one the
// attribute happens to spell.
func kebabToCamel(s string) string {
	var b strings.Builder
	up := false
	for _, r := range s {
		switch {
		case r == '-':
			up = true
		case up:
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestLayout_PaletteSignalNameSurvivesTheParser closes a silent-failure gap that costs
// nothing to introduce and shows no error when introduced: the HTML parser LOWERCASES
// attribute names, so `data-signals-paletteQuery` reaches the client as
// `data-signals-palettequery` and binds a signal the server never reads — the palette would
// simply return the empty-query prompt forever, with no console error and no failing
// handler. Declaring the signal hyphenated is the only spelling that survives, because the
// client folds kebab back to camelCase. The guard walks the shipped bytes exactly that way
// and requires the result to equal the paletteQuery JSON tag the handler decodes.
func TestLayout_PaletteSignalNameSurvivesTheParser(t *testing.T) {
	src := layoutSource(t)

	want := "paletteQuery" // == the `json:"paletteQuery"` tag on paletteQuery.PaletteQuery
	decl := regexp.MustCompile(`data-(?:signals|bind)-(palette[a-zA-Z-]*)`).FindAllStringSubmatch(src, -1)
	if len(decl) == 0 {
		t.Fatal("no palette signal declaration found in layout.html")
	}
	for _, m := range decl {
		// Exactly what a browser does to an attribute name, then what the client does.
		got := kebabToCamel(strings.ToLower(m[1]))
		if got != want {
			t.Errorf("attribute %q binds signal %q after HTML lowercasing + kebab->camel; the handler reads %q",
				m[0], got, want)
		}
	}

	// The tag itself must not drift out from under the guard.
	var q paletteQuery
	if f, ok := reflect.TypeOf(q).FieldByName("PaletteQuery"); !ok || f.Tag.Get("json") != want {
		t.Fatalf("paletteQuery.PaletteQuery json tag = %q, want %q (the guard above pins this name)",
			f.Tag.Get("json"), want)
	}
}
