package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// syntheticEntities returns a small, deterministic set of entities whose keys exercise
// every structural delimiter the grammar produces — '/', '@', '#', '{', '}', ',' — so the
// route/IRI-equality and strip-params fences do not depend on which real keys happen to be
// in the committed registry.
func syntheticEntities() []bestiary.Entity {
	refs := []bestiary.EntityRef{
		{Family: "llama"},
		{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}},
		{Family: "llama", Version: "3.1", ParamSize: "405b", Modifier: []string{"turbo", "instruct"}},
		{Family: "deepseek", Variant: "v3.2"},
		{Family: "gemini", Version: "3.0"},
	}
	out := make([]bestiary.Entity, len(refs))
	for i, r := range refs {
		out[i] = bestiary.Entity{Ref: r}
	}
	return out
}

func newTestServer(t *testing.T, entities []bestiary.Entity) *Server {
	t.Helper()
	s, err := NewServer(entities, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s
}

func get(t *testing.T, s *Server, target string, accept string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestServer_Index(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	rec := get(t, s, "/", "text/html")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"5 entities",                            // len(syntheticEntities)
		`src="/assets/datastar.js"`,             // vendored client, same origin
		`data-on-input`,                         // datastar wiring present
		`class="theme-toggle"`,                  // approved chrome shipped (light/dark toggle)
		"datastar client v" + DatastarJSVersion, // pinned version surfaced in footer
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index body missing %q", want)
		}
	}
}

// TestServer_IRI_MatchesRoute is the one-grammar fence: for every entity, the IRI minted
// with the web root as base is EXACTLY the route path that dereferences it, and GETting
// that path resolves the same entity. If escapeIRISegment and the route ever diverged
// (e.g. '/' re-encoded to %2F on one side only) this reddens.
//
// Checks (a)-(c) alone do NOT catch a whole-string-PathEscape regression: net/http
// decodes %2F -> '/' in r.URL.Path before mux matching, so mint+route stay internally
// consistent regardless of which separator escapeIRISegment chooses — reverting the
// escaper to re-encode '/' did not redden this test before the (d)/(e) checks were
// added. (d) and (e) pin the RENDERED-STRING form instead of only the round-tripped
// behavior: the literal bytes minted and the literal bytes an href carries must contain
// an unencoded '/' and never "%2F".
func TestServer_IRI_MatchesRoute(t *testing.T) {
	entities := syntheticEntities()
	s := newTestServer(t, entities)
	for _, e := range entities {
		key := e.Ref.String()
		iri := e.Ref.IRI(entityRoutePrefix)

		// (a) the mint is literally the route prefix + escaped tail (one grammar).
		if !strings.HasPrefix(iri, entityRoutePrefix) {
			t.Fatalf("IRI %q lacks route prefix %q", iri, entityRoutePrefix)
		}
		// (b) GETting the minted IRI resolves the same entity (200 + key in page).
		rec := get(t, s, iri, "text/html")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %q (key %q) = %d, want 200", iri, key, rec.Code)
			continue
		}
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("GET %q did not render key %q", iri, key)
		}
		// (c) the decoded path tail equals the canonical key byte-for-byte.
		u, err := url.Parse(iri)
		if err != nil {
			t.Fatalf("parse %q: %v", iri, err)
		}
		if gotKey := strings.TrimPrefix(u.Path, entityRoutePrefix); gotKey != key {
			t.Errorf("route tail for %q decoded to %q", key, gotKey)
		}

		// (d) for a multi-segment entity, the RENDERED mint string itself carries a
		// literal '/' between family and variant and NEVER "%2F" — this is the pin
		// that a whole-string url.PathEscape regression would fail (%2F present,
		// literal '/' absent), independent of net/http's %2F-decoding behavior.
		if e.Ref.Variant != "" {
			wantSeg := string(e.Ref.Family) + "/" + e.Ref.Variant
			if !strings.Contains(iri, wantSeg) {
				t.Errorf("IRI %q does not contain literal-'/' segment %q", iri, wantSeg)
			}
			if strings.Contains(iri, "%2F") || strings.Contains(iri, "%2f") {
				t.Errorf("IRI %q was re-encoded to %%2F; want a literal '/'", iri)
			}
		}
	}

	// (e) the entity link href emitted in the index page HTML carries the same
	// literal-'/' form (not "%2F") for a multi-segment entity — pinning the rendered
	// document, not just the Go-level IRI() return value.
	multi := entities[1] // llama/scout@4#17b-16e{instruct}
	wantHref := `href="` + multi.Ref.IRI(entityRoutePrefix) + `"`
	indexBody := get(t, s, "/", "text/html").Body.String()
	if !strings.Contains(indexBody, wantHref) {
		t.Errorf("index HTML missing literal-'/' entity link href %q", wantHref)
	}
	if strings.Contains(indexBody, "llama%2Fscout") || strings.Contains(indexBody, "llama%2fscout") {
		t.Errorf("index HTML entity link was %%2F-encoded; want literal '/'")
	}
}

func TestServer_EntityDetail_JSON(t *testing.T) {
	entities := syntheticEntities()
	s := newTestServer(t, entities)
	ref := entities[1].Ref // llama/scout@4#17b-16e{instruct}
	rec := get(t, s, ref.IRI(entityRoutePrefix), "application/json")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET (json) = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got entityJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal JSON entity: %v\nbody: %s", err, rec.Body.String())
	}
	if got.Ref.String() != ref.String() {
		t.Errorf("JSON entity ref = %q, want %q", got.Ref.String(), ref.String())
	}
}

// TestServer_ContentNegotiation pins the seam: the SAME url yields HTML to a browser and
// JSON to an application/json client.
func TestServer_ContentNegotiation(t *testing.T) {
	entities := syntheticEntities()
	s := newTestServer(t, entities)
	path := entities[3].Ref.IRI(entityRoutePrefix) // deepseek/v3.2

	html := get(t, s, path, "text/html,application/xhtml+xml")
	if ct := html.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("browser Accept: Content-Type = %q, want text/html", ct)
	}
	jsn := get(t, s, path, "application/json")
	if ct := jsn.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("json Accept: Content-Type = %q, want application/json", ct)
	}
	// Default client (Accept: */*) and no Accept both fall back to HTML.
	for _, accept := range []string{"*/*", ""} {
		rec := get(t, s, path, accept)
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
			t.Errorf("Accept %q: Content-Type = %q, want text/html", accept, ct)
		}
	}
}

// TestServer_StripParams pins RQ1: query params are non-identity view-state, so any
// ?quant/?ctx/?provider off yields the SAME entity page at defaults.
func TestServer_StripParams(t *testing.T) {
	entities := syntheticEntities()
	s := newTestServer(t, entities)
	path := entities[1].Ref.IRI(entityRoutePrefix)

	base := get(t, s, path, "text/html").Body.String()
	withParams := get(t, s, path+"?quant=q4_K_M&ctx=8192&provider=anthropic", "text/html").Body.String()
	if base != withParams {
		t.Errorf("query params changed the entity page: identity must ignore view-state params")
	}
}

func TestServer_EntityNotFound(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	rec := get(t, s, "/entity/no-such-family", "text/html")
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET unknown entity = %d, want 404", rec.Code)
	}
}

// TestServer_Assets_Datastar verifies the vendored client is served from the same origin
// with the pinned version banner — no CDN for JS.
func TestServer_Assets_Datastar(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	rec := get(t, s, "/assets/datastar.js", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/datastar.js = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("Content-Type = %q, want a javascript type", ct)
	}
	if want := "// Datastar v" + DatastarJSVersion; !strings.HasPrefix(rec.Body.String(), want) {
		t.Errorf("vendored datastar.js does not start with %q (version drift?)", want)
	}
}

// TestServer_SSE_Entities drives the datastar wiring end to end: the filter signal is read,
// the results fragment is rendered, and it is PatchElements-ed into #entity-results as an
// SSE event. This is the single proof the datastar-go SDK path works, offline.
func TestServer_SSE_Entities(t *testing.T) {
	s := newTestServer(t, syntheticEntities())
	q := url.Values{}
	q.Set("datastar", `{"filter":"llama"}`)
	rec := get(t, s, "/sse/entities?"+q.Encode(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /sse/entities = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"datastar-patch-elements", "#entity-results", "llama"} {
		if !strings.Contains(body, want) {
			t.Errorf("SSE body missing %q\nbody:\n%s", want, body)
		}
	}
	// The filter is honored: a non-matching family must not appear.
	if strings.Contains(body, "deepseek") {
		t.Errorf("SSE filter=llama leaked a non-matching entity (deepseek)")
	}
}

// TestServer_CacheReadPath exercises the offline SQLite read path: a temp cache with one
// upserted model is opened read-only and its row count surfaces on the index. No network.
func TestServer_CacheReadPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache.db")
	st, err := bestiary.OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	m := bestiary.ModelInfo{ID: "test-model", Provider: bestiary.ProviderAnthropic}
	if err := st.UpsertModels(context.Background(), []bestiary.ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	s, err := NewServer(syntheticEntities(), st)
	if err != nil {
		t.Fatalf("NewServer with cache: %v", err)
	}
	if s.cacheModelCount != 1 {
		t.Errorf("cacheModelCount = %d, want 1 (read path not exercised)", s.cacheModelCount)
	}
	rec := get(t, s, "/", "text/html")
	if !strings.Contains(rec.Body.String(), "1 cached rows") {
		t.Errorf("index did not surface the cached row count")
	}
	_ = st.Close()
}

// TestServer_RealRegistrySmoke builds the server over the actual committed registry and
// GETs the first few entity links, confirming real keys route end to end (a corpus check
// on top of the synthetic delimiter cases).
func TestServer_RealRegistrySmoke(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Skip("no registry entities")
	}
	s := newTestServer(t, entities)
	n := 0
	for _, l := range s.links {
		rec := get(t, s, l.Path, "text/html")
		if rec.Code != http.StatusOK {
			t.Errorf("GET %q (key %q) = %d, want 200", l.Path, l.Key, rec.Code)
		}
		if n++; n >= 25 {
			break
		}
	}
}

// TestTemplates_RenderSmoke confirms the template sets parse and render without error.
func TestTemplates_RenderSmoke(t *testing.T) {
	if _, err := parseTemplates(); err != nil {
		t.Fatalf("parseTemplates: %v", err)
	}
	if _, err := parseFragments(); err != nil {
		t.Fatalf("parseFragments: %v", err)
	}
}
