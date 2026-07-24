package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"github.com/dayvidpham/bestiary"
	datastar "github.com/starfederation/datastar-go/datastar"
)

// entityRoutePrefix is the ONE grammar shared by (a) the entity IRI the server mints for
// a link and (b) the route that dereferences it. EntityRef.IRI(entityRoutePrefix) yields
// exactly the path this route matches, because escapeIRISegment keeps '/' literal (RQ1):
// the key renders as a multi-segment path under this prefix. Changing this prefix changes
// both sides at once, which is the point — there is no second place to keep in sync.
const entityRoutePrefix = "/entity/"

// sampleLimit bounds how many entity links the index page and each SSE result set render.
// The full, paginated browser is slice-11; this foundation shows a representative slice.
const sampleLimit = 50

// Server is the offline bestiary web server. It reads ONLY the in-process static registry
// (bestiary.Entities()/StaticModels()) and an optional read-only SQLite cache — it never
// touches the network at serve time. The vendored datastar.js client (same origin) and
// the CDN-loaded fonts are the browser's own fetches, not server egress.
type Server struct {
	handler   http.Handler
	pages     map[string]*template.Template
	fragments *template.Template

	entities []bestiary.Entity
	byKey    map[string]bestiary.Entity
	links    []entityLink // precomputed, sorted by key — the index/SSE render source

	modelCount      int
	cacheModelCount int
}

// entityLink is the render view of one entity in a list: its canonical key, the
// same-origin route path (== EntityRef.IRI(entityRoutePrefix)), and its creator.
type entityLink struct {
	Key     string
	Path    string
	Family  string
	Creator string
}

// indexView is the data model for the index page.
type indexView struct {
	Title           string
	DatastarVersion string
	EntityCount     int
	ModelCount      int
	CacheModelCount int
	Sample          []entityLink
}

// entityView is the data model for an entity detail page.
type entityView struct {
	Title           string
	DatastarVersion string
	Entity          bestiary.Entity
	Nomina          []bestiary.Nomen
	Path            string
}

// entityJSON is the public JSON shape for an entity: the marshaled Entity plus its derived
// Nomina projection (a method, so it would not otherwise appear). It mirrors the CLI's
// `entities`/`show --by-entity` JSON so the content-negotiated JSON view and the CLI agree.
type entityJSON struct {
	bestiary.Entity
	Nomina []bestiary.Nomen
}

// NewServer builds a server over the given entities and an optional read-only cache.
// entities is normally bestiary.Entities(); cache is nil when no --cache path is
// configured (static-only). NewServer performs the cache READ (a count) up front so the
// serve path never has to — and so a test can assert the read path is exercised offline.
func NewServer(entities []bestiary.Entity, cache *bestiary.Store) (*Server, error) {
	pages, err := parseTemplates()
	if err != nil {
		return nil, fmt.Errorf("bestiary-web: parse page templates: %w", err)
	}
	frag, err := parseFragments()
	if err != nil {
		return nil, fmt.Errorf("bestiary-web: parse fragment templates: %w", err)
	}

	s := &Server{
		pages:      pages,
		fragments:  frag,
		entities:   entities,
		byKey:      make(map[string]bestiary.Entity, len(entities)),
		modelCount: len(bestiary.StaticModels()),
	}
	for _, e := range entities {
		key := e.Ref.String()
		s.byKey[key] = e
		s.links = append(s.links, entityLink{
			Key:     key,
			Path:    e.Ref.IRI(entityRoutePrefix),
			Family:  string(e.Ref.Family),
			Creator: string(e.Creator),
		})
	}
	sort.Slice(s.links, func(i, j int) bool { return s.links[i].Key < s.links[j].Key })

	if cache != nil {
		// Read-only cache path: count cached model rows. QueryModels(ctx, "") returns
		// every cached row across providers. This is an offline SQLite read (no network).
		models, err := cache.QueryModels(context.Background(), "")
		if err != nil {
			return nil, fmt.Errorf("bestiary-web: read SQLite cache: %w", err)
		}
		s.cacheModelCount = len(models)
	}

	s.routes()
	return s, nil
}

// routes registers the mux. Go 1.22 method+path patterns; the trailing-slash "/entity/"
// pattern is a subtree match, so a multi-segment key (llama/scout%404…) lands here.
//
// Route names deliberately diverge from the draft naming sketched in the early design
// notes ("/entities", "/static/*"): this implementation ships "/sse/entities" and
// "/assets/*" instead — "/sse/" names the transport (this is specifically the datastar
// SSE wiring seam, not a general entities collection endpoint one might expect to
// support other verbs/methods), and "/assets/" is the more conventional name for a
// same-origin static-asset mount. Later work builds on these shipped names — changing
// them now would be a breaking change to the route table, not a cosmetic rename.
func (s *Server) routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET "+entityRoutePrefix, s.handleEntity)
	mux.HandleFunc("GET /sse/entities", s.handleEntitiesSSE)
	mux.Handle("GET /assets/", http.FileServerFS(assetsFS))
	s.handler = mux
}

// ServeHTTP makes Server an http.Handler (so httptest can drive it with no port bind).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// handleIndex renders the catalog landing page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	sample := s.links
	if len(sample) > sampleLimit {
		sample = sample[:sampleLimit]
	}
	s.render(w, "index", indexView{
		Title:           "catalog",
		DatastarVersion: DatastarJSVersion,
		EntityCount:     len(s.entities),
		ModelCount:      s.modelCount,
		CacheModelCount: s.cacheModelCount,
		Sample:          sample,
	})
}

// handleEntity dereferences /entity/<multi-segment key>. The key is r.URL.Path with the
// prefix stripped: because '/' is literal in the grammar and Go has already percent-decoded
// %40/%23/%7B/%7D back to @/#/{/}, the stripped path IS the canonical entity key verbatim —
// the same string EntityRef.String() produced and EntityRef.IRI(entityRoutePrefix) minted.
//
// Query parameters are IDENTITY-IRRELEVANT (RQ1: query = non-identity view-state only), so
// they are ignored here entirely — ?quant/?ctx/?provider yield the same entity at defaults.
func (s *Server) handleEntity(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, entityRoutePrefix)
	if key == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	e, ok := s.byKey[key]
	if !ok {
		http.Error(w, fmt.Sprintf("entity %q not found", key), http.StatusNotFound)
		return
	}

	if negotiateJSON(r.Header.Get("Accept")) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entityJSON{Entity: e, Nomina: e.Nomina()}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.render(w, "entity", entityView{
		Title:           key,
		DatastarVersion: DatastarJSVersion,
		Entity:          e,
		Nomina:          e.Nomina(),
		Path:            entityRoutePrefix + key,
	})
}

// handleEntitiesSSE is the datastar wiring seam: it reads the `filter` signal, filters the
// entity links by a family/key substring, and PatchElements the results fragment into
// #entity-results. This is the single end-to-end proof that the datastar-go SDK
// (ReadSignals → NewSSE → PatchElements) and the vendored client agree; slice-11 grows the
// full browser on top of it. No network, no port assumptions.
func (s *Server) handleEntitiesSSE(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Filter string `json:"filter"`
	}
	// Tolerant read: absent/empty signals are not an error (the initial load has none).
	_ = datastar.ReadSignals(r, &sig)

	results := s.filterLinks(sig.Filter)
	var buf bytes.Buffer
	if err := s.fragments.ExecuteTemplate(&buf, "entity-results", results); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElements(
		buf.String(),
		datastar.WithSelector("#entity-results"),
		datastar.WithMode(datastar.ElementPatchModeInner),
	)
}

// filterLinks returns the entity links whose key contains filter (case-insensitive),
// capped at sampleLimit. An empty filter returns the leading sample.
func (s *Server) filterLinks(filter string) []entityLink {
	filter = strings.ToLower(strings.TrimSpace(filter))
	out := make([]entityLink, 0, sampleLimit)
	for _, l := range s.links {
		if filter != "" && !strings.Contains(strings.ToLower(l.Key), filter) {
			continue
		}
		out = append(out, l)
		if len(out) >= sampleLimit {
			break
		}
	}
	return out
}

// render executes a page template set's "layout" and writes it. On a template error it
// reports 500 rather than emitting a half-rendered page.
func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "unknown page "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// negotiateJSON is the content-negotiation seam: return the JSON entity shape only when the
// client explicitly asks for application/json and does NOT also accept text/html. A browser
// (Accept: text/html,…) and a default client (Accept: */*) both get HTML; an API client
// (Accept: application/json) gets JSON. This deliberately keeps the same URL serving both
// representations, differing only by Accept.
func negotiateJSON(accept string) bool {
	accept = strings.ToLower(accept)
	return strings.Contains(accept, "application/json") && !strings.Contains(accept, "text/html")
}
