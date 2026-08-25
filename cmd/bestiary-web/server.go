package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
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

	// rows is the browser render source: one entityRow per entity, pre-sorted in the
	// DEFAULT order (Family then canonical key). The default order is fixed here so
	// every SSE re-render that does not choose an explicit sort returns the identical
	// sequence; the alternate sorts operate on a copy and never disturb this slice.
	rows   []entityRow
	facets facetOptions
	// maxVRAM is the largest per-entity VRAMBytes across the whole corpus; it is the
	// denominator that normalizes every row's mini-bar to a common scale.
	maxVRAM int64

	modelCount      int
	cacheModelCount int
}

// entityRow is the dense-table render view of one entity in the browser: its canonical
// key, the same-origin route path (== EntityRef.IRI(entityRoutePrefix)), and the aggregate
// facets the table displays and the filter rail filters on. The display strings and the
// mini-bar fraction are precomputed once at NewServer so the initial render and every SSE
// re-render agree byte-for-byte.
type entityRow struct {
	Key           string
	Path          string
	Family        string
	Creator       string
	Providers     []string // distinct provider tokens, for the provider facet
	ProviderCount int
	Regions       []string // distinct region tokens, for the region facet
	Modalities    []string // aggregated input modalities, for the modality facet
	VRAMBytes     int64    // max VRAMBytes across this entity's quant rows; 0 unknown
	VRAMPartial   bool     // the max-bearing quant row is a weights-only lower bound
	VRAMFrac      int      // 0..100, VRAMBytes as a fraction of the corpus maximum
	VRAMLabel     string   // "NN.N GB" or em-dash
	PriceLabel    string   // "$N.NN" min input price per MTok, or em-dash
}

// facetOptions holds the distinct, sorted values each filter-rail control offers. They are
// computed once from the corpus so the rail always offers exactly the values present.
type facetOptions struct {
	Families   []string
	Creators   []string
	Providers  []string
	Regions    []string
	Modalities []string
}

// browseQuery is the view-state the browser SSE endpoint reads from the Datastar signals:
// a free-text search over the key plus five exact-match facet filters and a sort key. Every
// field is optional — an empty value imposes no constraint. This is NON-IDENTITY view-state
// (RQ1): it selects which rows and in what order, never which entity a link denotes.
type browseQuery struct {
	Search   string `json:"search"`
	Family   string `json:"family"`
	Creator  string `json:"creator"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	Modality string `json:"modality"`
	Sort     string `json:"sort"`
}

// indexView is the data model for the browser (index) page.
type indexView struct {
	Title           string
	DatastarVersion string
	EntityCount     int
	ModelCount      int
	CacheModelCount int
	Rows            []entityRow
	Facets          facetOptions
}

// entityView is the data model for an entity detail page.
type entityView struct {
	Title           string
	DatastarVersion string
	Entity          bestiary.Entity
	Nomina          []bestiary.Nomen
	Path            string
	// MaxQuantVRAM is the largest VRAMBytes across THIS entity's quant rows; it
	// normalizes the per-quant mini-bars on the detail page to a common scale.
	MaxQuantVRAM int64
	// CtxTokens is the optional ?ctx view-state override (0 when absent). When
	// positive it drives the display-only VRAM recompute column; it NEVER changes
	// which entity is shown (RQ1).
	CtxTokens int
	// SeriesDisplay / SeriesAnchor locate this entity's release in the series
	// explorer: the human "Family-Gen / Release" breadcrumb and the same-page anchor
	// its "series" section links to.
	SeriesDisplay string
	SeriesAnchor  string
}

// entityJSON is the public JSON shape for an entity: the marshaled Entity plus its derived
// Nomina projection (a method, so it would not otherwise appear). It mirrors the CLI's
// `entities`/`show --by-entity` JSON so the content-negotiated JSON view and the CLI agree.
type entityJSON struct {
	bestiary.Entity
	Nomina []bestiary.Nomen
}

// treeView is the data model for the FRONT PAGE: the Creator > Family > Series >
// entities tree. It is the browsable entry point — a reader who has not yet
// decided what they are looking for starts from the lab that trained the weights,
// not from a nine-hundred-row table.
type treeView struct {
	Title           string
	DatastarVersion string
	Creators        []creatorNode
	CreatorCount    int
	FamilyCount     int
	SeriesCount     int
	EntityCount     int
}

// creatorNode is one lab at the root of the tree. Attributed is false for the
// single remainder group of families with no curated creator: it renders last and
// COLLAPSED, because "we do not know who trained these" is a footnote to the page,
// not its opening statement.
type creatorNode struct {
	Name        string
	Attributed  bool
	Families    []familyNode
	EntityCount int
}

// familyNode is one family beneath a creator, holding its versioned lines.
type familyNode struct {
	Name        string
	Series      []seriesNode
	EntityCount int
}

// familiesView is the data model for /families: the flat, creator-agnostic
// Series -> Release -> entities walk. It is the same substrate the front-page tree
// nests, rendered one level shallower for a reader who already knows the family
// and wants the whole line at once.
type familiesView struct {
	Title           string
	DatastarVersion string
	Series          []seriesNode
	SeriesCount     int
	EntityCount     int
}

// seriesNode is ONE versioned line with the base hoist applied, and it is shared
// by both pages so there is a single rendering of the hierarchy rather than two
// that could drift.
//
// Hoisted carries the un-named release's entities directly; Releases carries only
// NAMED releases. There is deliberately no "(base)" release here: the un-named
// release is not a member of the line alongside the named ones, it is the line
// itself, and giving it a node invented a level that does not exist.
//
// Anchor is emitted only where the page owns the anchor namespace (/families), so
// a detail page's series link always resolves to exactly one place.
type seriesNode struct {
	Display     string
	Anchor      string
	Hoisted     []entityRef
	Releases    []releaseNode
	EntityCount int
	// Mixed is true when the line has BOTH hoisted entities and named releases.
	// The two sit at different levels of the hierarchy while rendering adjacent
	// on screen, so the template must mark the hoisted group visually; a
	// base-only line has nothing to distinguish it from and is left plain.
	Mixed bool
}

// releaseNode is one NAMED release within a Series: its name and its entities.
type releaseNode struct {
	Name     string
	Entities []entityRef
}

// entityRef is a minimal entity link (key + route path) for the tree leaves.
type entityRef struct {
	Key  string
	Path string
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
	s.buildRows(entities)

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

// buildRows populates byKey, the browser rows, the facet option lists, and the corpus VRAM
// maximum. It performs the (ID,Provider)→Modalities join against StaticModels() ONCE here,
// so serve-time rendering never recomputes it and the aggregate never affects identity.
func (s *Server) buildRows(entities []bestiary.Entity) {
	modByInstance := modalityIndex()

	famSet := map[string]struct{}{}
	creatorSet := map[string]struct{}{}
	provSet := map[string]struct{}{}
	regionSet := map[string]struct{}{}
	modSet := map[string]struct{}{}

	s.rows = make([]entityRow, 0, len(entities))
	for _, e := range entities {
		key := e.Ref.String()
		s.byKey[key] = e

		providers := make([]string, 0, len(e.Providers))
		for _, p := range e.Providers {
			providers = append(providers, string(p))
			provSet[string(p)] = struct{}{}
		}
		regions := make([]string, 0, len(e.Regions))
		for _, r := range e.Regions {
			regions = append(regions, r.String())
			regionSet[r.String()] = struct{}{}
		}

		// Aggregate the distinct INPUT modalities across every instance of the entity
		// (what the entity can accept), joined from the flat model catalog.
		modAgg := map[bestiary.Modality]struct{}{}
		var maxVRAM int64
		var maxPartial bool
		for _, inst := range e.Instances {
			if mods, ok := modByInstance[instanceKey(inst.ID, inst.Provider)]; ok {
				for _, m := range mods.Input {
					modAgg[m] = struct{}{}
				}
			}
			for _, q := range inst.QuantVRAM {
				if q.VRAMBytes > maxVRAM {
					maxVRAM = q.VRAMBytes
					maxPartial = q.VRAMEstimatePartial
				}
			}
		}
		modalities := sortedModalities(modAgg)
		for _, m := range modalities {
			modSet[m] = struct{}{}
		}

		if maxVRAM > s.maxVRAM {
			s.maxVRAM = maxVRAM
		}
		if e.Ref.Family != "" {
			famSet[string(e.Ref.Family)] = struct{}{}
		}
		if e.Creator != "" {
			creatorSet[string(e.Creator)] = struct{}{}
		}

		s.rows = append(s.rows, entityRow{
			Key:           key,
			Path:          e.Ref.IRI(entityRoutePrefix),
			Family:        string(e.Ref.Family),
			Creator:       string(e.Creator),
			Providers:     providers,
			ProviderCount: len(e.Providers),
			Regions:       regions,
			Modalities:    modalities,
			VRAMBytes:     maxVRAM,
			VRAMPartial:   maxPartial,
			PriceLabel:    priceLabel(e.PriceInputRange[0]),
		})
	}

	// Second pass: normalize every mini-bar against the corpus maximum, now known.
	for i := range s.rows {
		s.rows[i].VRAMLabel = vramLabel(s.rows[i].VRAMBytes)
		s.rows[i].VRAMFrac = fracOf(s.rows[i].VRAMBytes, s.maxVRAM)
	}

	// DEFAULT ORDER: Family (ascending) then canonical key (ascending). Fixed here and
	// documented so every default SSE re-render is byte-stable.
	sort.SliceStable(s.rows, func(i, j int) bool { return lessDefaultRow(s.rows[i], s.rows[j]) })

	s.facets = facetOptions{
		Families:   sortedKeys(famSet),
		Creators:   sortedKeys(creatorSet),
		Providers:  sortedKeys(provSet),
		Regions:    sortedKeys(regionSet),
		Modalities: sortedKeys(modSet),
	}
}

// lessDefaultRow is the browser's DEFAULT total order: ascending Family, then ascending
// canonical key. It is the single ordering the un-sorted browse path uses, so an SSE
// re-render with no explicit sort is deterministic.
func lessDefaultRow(a, b entityRow) bool {
	if a.Family != b.Family {
		return a.Family < b.Family
	}
	return a.Key < b.Key
}

// modalityIndex builds the (ID,Provider) → Modalities join over the flat static catalog.
// Modalities are a per-model fact on ModelInfo, not carried on the rolled-up Entity, so the
// browser joins them back here at startup. Built once; never touched at serve time.
func modalityIndex() map[string]bestiary.Modalities {
	models := bestiary.StaticModels()
	out := make(map[string]bestiary.Modalities, len(models))
	for _, m := range models {
		out[instanceKey(m.ID, m.Provider)] = m.Modalities
	}
	return out
}

// instanceKey is the join key for the modality index: the (ID, Provider) tuple that is the
// composite identity of a catalog row, NUL-joined so no ID/provider token can collide.
func instanceKey(id bestiary.ModelID, p bestiary.Provider) string {
	return string(id) + "\x00" + string(p)
}

// sortedModalities renders a modality set as its stable lowercase tokens in Modality-value
// order (text, image, pdf, audio, video), so the display and facet lists are deterministic.
func sortedModalities(set map[bestiary.Modality]struct{}) []string {
	ms := make([]bestiary.Modality, 0, len(set))
	for m := range set {
		ms = append(ms, m)
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i] < ms[j] })
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.String()
	}
	return out
}

// sortedKeys returns the set's keys as an ascending, de-duplicated slice.
func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// vramLabel renders a VRAM byte count as a "NN.N GB" figure, or an em-dash when unknown.
func vramLabel(b int64) string {
	if b <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
}

// priceLabel renders a per-MTok input price as "$N.NN", or an em-dash when unknown (nil).
func priceLabel(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *p)
}

// fracOf clamps n/d to an integer percentage in [0,100]; a non-positive denominator or
// numerator yields 0 (an empty bar), never a divide-by-zero.
func fracOf(n, d int64) int {
	if d <= 0 || n <= 0 {
		return 0
	}
	p := int(n * 100 / d)
	if p > 100 {
		p = 100
	}
	return p
}

// routes registers the mux. Go 1.22 method+path patterns; the trailing-slash "/entity/"
// pattern is a subtree match, so a multi-segment key (llama/scout%404…) lands there.
//
// Route names deliberately diverge from the draft naming sketched in the early design
// notes ("/entities", "/static/*"): this implementation ships "/sse/entities" and
// "/assets/*" instead — "/sse/" names the transport (this is specifically the datastar
// SSE wiring seam, not a general entities collection endpoint one might expect to
// support other verbs/methods), and "/assets/" is the more conventional name for a
// same-origin static-asset mount. Later work builds on these shipped names — changing
// them now would be a breaking change to the route table, not a cosmetic rename.
//
// "/" is the Creator > Family > Series > entities tree and "/entities" is the dense
// browser that used to live at "/": a reader arriving with no query in mind needs a
// hierarchy to walk, not a nine-hundred-row table, while a reader who knows what they
// want still has the table one click away.
//
// "/calculator" is the budget-first fit page and "/sse/calculator" its patch seam, named
// on the same two conventions as the pair above: "/sse/" names the transport, and the page
// route names the question the page answers rather than the data it reads.
//
// "/series" is GONE, not redirected. Its content was absorbed by "/families", which
// emits the SAME series anchors, so the detail-page links that pointed into the old
// explorer resolve at the new path. A retired route returns a hard 404: an alias would
// keep a dead name alive in links and bookmarks indefinitely, which is the thing
// retiring it was meant to stop.
func (s *Server) routes() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleTree)
	mux.HandleFunc("GET /entities", s.handleEntities)
	mux.HandleFunc("GET /families", s.handleFamilies)
	mux.HandleFunc("GET "+entityRoutePrefix, s.handleEntity)
	mux.HandleFunc("GET /calculator", s.handleCalculator)
	mux.HandleFunc("GET /sse/entities", s.handleEntitiesSSE)
	mux.HandleFunc("GET /sse/calculator", s.handleCalculatorSSE)
	mux.HandleFunc("GET /sse/palette", s.handlePaletteSSE)
	mux.Handle("GET /assets/", http.FileServerFS(assetsFS))
	s.handler = mux
}

// ServeHTTP makes Server an http.Handler (so httptest can drive it with no port bind).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.handler.ServeHTTP(w, r) }

// handleEntities renders the entity browser: the dense sortable table over every entity,
// the filter rail, and the SSE-patched results container seeded with the default-ordered
// rows. It served "/" until the front page became the tree; the browser itself is
// unchanged, only its address.
func (s *Server) handleEntities(w http.ResponseWriter, r *http.Request) {
	s.render(w, "entities", indexView{
		Title:           "catalog",
		DatastarVersion: DatastarJSVersion,
		EntityCount:     len(s.entities),
		ModelCount:      s.modelCount,
		CacheModelCount: s.cacheModelCount,
		Rows:            s.rows,
		Facets:          s.facets,
	})
}

// handleEntity dereferences /entity/<multi-segment key>. The key is r.URL.Path with the
// prefix stripped: because '/' is literal in the grammar and Go has already percent-decoded
// %40/%23/%7B/%7D back to @/#/{/}, the stripped path IS the canonical entity key verbatim —
// the same string EntityRef.String() produced and EntityRef.IRI(entityRoutePrefix) minted.
//
// Query parameters are IDENTITY-IRRELEVANT (RQ1: query = non-identity view-state only), so
// they never select a different entity. The one honored view-state param is ?ctx, which
// only drives the display-only VRAM recompute COLUMN in the quants section — it never
// changes which entity is shown, and an entity with no quant rows renders byte-identically
// with or without it.
func (s *Server) handleEntity(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, entityRoutePrefix)
	if key == "" {
		// A bare /entity/ names no entity; send the caller to the browser, which is
		// the page that lists them, rather than to the tree.
		http.Redirect(w, r, "/entities", http.StatusSeeOther)
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

	rel := bestiary.ReleaseOf(e.Ref)
	s.render(w, "entity", entityView{
		Title:           key,
		DatastarVersion: DatastarJSVersion,
		Entity:          e,
		Nomina:          e.Nomina(),
		Path:            entityRoutePrefix + key,
		MaxQuantVRAM:    maxQuantVRAM(e),
		CtxTokens:       parseCtx(r.URL.Query().Get("ctx")),
		SeriesDisplay:   rel.String(),
		SeriesAnchor:    seriesAnchor(rel.Series),
	})
}

// parseCtx reads the optional ?ctx view-state override. A missing, non-numeric, or
// non-positive value yields 0 (meaning "no override; show baked figures"), never an error —
// a malformed view-state param must degrade to the default view, not a 400.
func parseCtx(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// maxQuantVRAM returns the largest VRAMBytes across all of an entity's quant rows; 0 when
// the entity has no quant data. It is the per-entity denominator for the detail mini-bars.
func maxQuantVRAM(e bestiary.Entity) int64 {
	var max int64
	for _, inst := range e.Instances {
		for _, q := range inst.QuantVRAM {
			if q.VRAMBytes > max {
				max = q.VRAMBytes
			}
		}
	}
	return max
}

// handleEntitiesSSE is the datastar wiring seam for the browser: it reads the view-state
// signals (search + five facets + sort), filters and sorts the entity rows, and
// PatchElements the results table into #entity-results. This is the single end-to-end proof
// that the datastar-go SDK (ReadSignals → NewSSE → PatchElements) and the vendored client
// agree; no network, no port assumptions.
func (s *Server) handleEntitiesSSE(w http.ResponseWriter, r *http.Request) {
	var q browseQuery
	// Tolerant read: absent/empty signals are not an error (the initial load has none).
	_ = datastar.ReadSignals(r, &q)

	rows := s.browse(q)
	var buf bytes.Buffer
	if err := s.fragments.ExecuteTemplate(&buf, "entity-rows", rows); err != nil {
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

// browse applies the view-state to the corpus: a case-insensitive substring match of the
// free-text search over the canonical key, exact-match filters on the five facets, and the
// chosen sort. It returns a fresh slice (a copy), so the alternate sorts never disturb the
// server's default-ordered rows. With no view-state it returns the rows in the DEFAULT order
// (Family then key, established in buildRows).
func (s *Server) browse(q browseQuery) []entityRow {
	search := strings.ToLower(strings.TrimSpace(q.Search))
	out := make([]entityRow, 0, len(s.rows))
	for _, r := range s.rows {
		if search != "" && !strings.Contains(strings.ToLower(r.Key), search) {
			continue
		}
		if q.Family != "" && r.Family != q.Family {
			continue
		}
		if q.Creator != "" && r.Creator != q.Creator {
			continue
		}
		if q.Provider != "" && !containsStr(r.Providers, q.Provider) {
			continue
		}
		if q.Region != "" && !containsStr(r.Regions, q.Region) {
			continue
		}
		if q.Modality != "" && !containsStr(r.Modalities, q.Modality) {
			continue
		}
		out = append(out, r)
	}
	sortRows(out, q.Sort)
	return out
}

// sortRows applies the chosen sort key IN PLACE. The empty/"family" key is the default
// (Family then key) — the slice already arrives in that order, so the stable sort is a
// no-op reorder that keeps it. Every branch breaks ties on the canonical key so the order
// is a total order (deterministic, never dependent on input position).
func sortRows(rows []entityRow, key string) {
	switch key {
	case "key":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	case "creator":
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Creator != rows[j].Creator {
				return rows[i].Creator < rows[j].Creator
			}
			return rows[i].Key < rows[j].Key
		})
	case "vram":
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].VRAMBytes != rows[j].VRAMBytes {
				return rows[i].VRAMBytes > rows[j].VRAMBytes // largest first
			}
			return rows[i].Key < rows[j].Key
		})
	case "providers":
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].ProviderCount != rows[j].ProviderCount {
				return rows[i].ProviderCount > rows[j].ProviderCount // most-hosted first
			}
			return rows[i].Key < rows[j].Key
		})
	default: // "" or "family": the default Family-then-key order
		sort.SliceStable(rows, func(i, j int) bool { return lessDefaultRow(rows[i], rows[j]) })
	}
}

// containsStr reports whether xs contains x.
func containsStr(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// ===== Command palette =====================================================================
//
// The palette is entity SEARCH AND NAVIGATE, and nothing else. It deliberately carries no
// page-navigation or view-action commands: a palette that also runs actions has to explain
// which of its rows change the page and which change the app, and the moment it does, a
// keystroke that used to open an entity can start doing something else. Every row here
// resolves to one entity URL, so Enter always means the same thing.
//
// It reuses the SSE fragment seam the browser already uses (ReadSignals -> NewSSE ->
// PatchElements); only the target element differs (#palette-results, not #entity-results).

// paletteMaxResults caps how many options one patch renders. A combobox popup is scanned,
// not paged, so a hundred rows would be noise; the cap is surfaced to the reader (the
// fragment states the total when it truncates) rather than silently swallowing matches.
const paletteMaxResults = 10

// paletteQuery is the view-state the palette SSE endpoint reads from the Datastar signals:
// the one free-text term the combobox input is bound to. Like the browser's signals this is
// NON-IDENTITY view-state — it selects which options are offered, never which entity a
// chosen option denotes.
type paletteQuery struct {
	PaletteQuery string `json:"paletteQuery"`
}

// paletteOption is one rendered combobox option: the canonical key the reader recognizes,
// the same-origin route Enter navigates to (== EntityRef.IRI(entityRoutePrefix), the exact
// path /entity/ dereferences), and the two attribution facets that disambiguate keys that
// read alike. OptionID is the DOM id the input's aria-activedescendant points at; it is
// minted server-side so the pointer target and the rendered row can never disagree.
type paletteOption struct {
	OptionID string
	Key      string
	Path     string
	Family   string
	Creator  string
}

// paletteView is the fragment's data model. Total is the match count BEFORE the cap, so a
// truncated list can say so; Query is echoed to distinguish "nothing typed yet" (the
// opening state of the dialog) from "nothing matches what you typed".
type paletteView struct {
	Query   string
	Options []paletteOption
	Total   int
}

// Truncated reports whether the cap hid matches, i.e. whether the fragment must tell the
// reader that Total is larger than what it listed.
func (v paletteView) Truncated() bool { return v.Total > len(v.Options) }

// handlePaletteSSE is the palette's wiring seam: read the one search signal, rank the
// matching entities, and PatchElements the option list into #palette-results. It is the
// same SDK path as handleEntitiesSSE against a different target, which is why the palette
// needed no new route KIND, no client router and no new dependency.
func (s *Server) handlePaletteSSE(w http.ResponseWriter, r *http.Request) {
	var q paletteQuery
	// Tolerant read: an absent/empty signal is the dialog's opening state, not an error.
	_ = datastar.ReadSignals(r, &q)

	var buf bytes.Buffer
	if err := s.fragments.ExecuteTemplate(&buf, "palette-options", s.palette(q)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElements(
		buf.String(),
		datastar.WithSelector("#palette-results"),
		datastar.WithMode(datastar.ElementPatchModeInner),
	)
}

// palette ranks the corpus against the search term and returns at most paletteMaxResults
// options, plus the uncapped match total.
//
// The rank is a relevance order, not a new identity: a term that PREFIXES a canonical key
// is what the reader almost always meant, so those come first; a term found anywhere else
// in the key comes next; a term that only matched the attribution (family or creator) comes
// last, because that reader typed a lab, not a model. Within a rank the corpus DEFAULT
// order (Family then key, established in buildRows) breaks the tie, so the same term always
// yields the same list.
//
// An empty term matches NOTHING: an opening palette that pre-loads ten arbitrary entities
// invites a reader to press Enter on a row they did not choose.
func (s *Server) palette(q paletteQuery) paletteView {
	term := strings.ToLower(strings.TrimSpace(q.PaletteQuery))
	v := paletteView{Query: strings.TrimSpace(q.PaletteQuery)}
	if term == "" {
		return v
	}

	type ranked struct {
		row  entityRow
		rank int
	}
	hits := make([]ranked, 0, paletteMaxResults)
	for _, r := range s.rows {
		rk, ok := paletteRank(r, term)
		if !ok {
			continue
		}
		v.Total++
		hits = append(hits, ranked{row: r, rank: rk})
	}
	// Stable: s.rows is already in the default order, so equal ranks keep it.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].rank < hits[j].rank })
	if len(hits) > paletteMaxResults {
		hits = hits[:paletteMaxResults]
	}

	v.Options = make([]paletteOption, 0, len(hits))
	for i, h := range hits {
		v.Options = append(v.Options, paletteOption{
			OptionID: paletteOptionID(i),
			Key:      h.row.Key,
			Path:     h.row.Path,
			Family:   h.row.Family,
			Creator:  h.row.Creator,
		})
	}
	return v
}

// paletteRank scores one row against an already-lowercased, non-empty term: 0 for a key
// prefix, 1 for a key substring, 2 for an attribution (family/creator) substring. ok is
// false when the row does not match at all.
func paletteRank(r entityRow, term string) (int, bool) {
	key := strings.ToLower(r.Key)
	switch {
	case strings.HasPrefix(key, term):
		return 0, true
	case strings.Contains(key, term):
		return 1, true
	case strings.Contains(strings.ToLower(r.Family), term),
		strings.Contains(strings.ToLower(r.Creator), term):
		return 2, true
	}
	return 0, false
}

// paletteOptionID mints the DOM id of the i-th option. The ids are positional and dense
// (0..n-1) because the combobox pointer moves by POSITION: the browser-side handler walks
// the rendered options and writes the chosen one's id into aria-activedescendant, so an id
// that encoded the entity key instead would buy nothing and would have to be escaped.
func paletteOptionID(i int) string { return fmt.Sprintf("palette-opt-%d", i) }

// handleTree renders the FRONT PAGE: the Creator > Family > Series > entities tree,
// walked from the root package's CreatorGroups() projection. It is a native <details>
// disclosure tree (no client router — a hierarchy walk is a nested list). Attributed
// creators render expanded; the single unattributed remainder renders collapsed at the
// bottom.
//
// The tree emits NO series anchors: /families owns that namespace, so a detail page's
// series link resolves to exactly one page rather than two that both claim the id.
func (s *Server) handleTree(w http.ResponseWriter, r *http.Request) {
	groups := bestiary.CreatorGroups()
	nodes := make([]creatorNode, 0, len(groups))
	var families, series, total int
	for _, cg := range groups {
		famNodes := make([]familyNode, 0, len(cg.Families))
		for _, fg := range cg.Families {
			serNodes := make([]seriesNode, 0, len(fg.Series))
			for _, sg := range fg.Series {
				serNodes = append(serNodes, newSeriesNode(sg, ""))
			}
			series += len(serNodes)
			famNodes = append(famNodes, familyNode{
				Name:        string(fg.Family),
				Series:      serNodes,
				EntityCount: fg.EntityCount,
			})
		}
		families += len(famNodes)
		total += cg.EntityCount
		name := string(cg.Creator)
		attributed := cg.Creator != bestiary.CreatorNone
		if !attributed {
			// The label reads under a text-transform:uppercase rule, where a
			// noun-first phrasing scans backwards; the bare adjective is what
			// a reader glancing at the collapsed group needs. The entity page
			// uses the same bare wording so the site stays consistent.
			name = "unattributed"
		}
		nodes = append(nodes, creatorNode{
			Name:        name,
			Attributed:  attributed,
			Families:    famNodes,
			EntityCount: cg.EntityCount,
		})
	}
	s.render(w, "tree", treeView{
		Title:           "bestiary",
		DatastarVersion: DatastarJSVersion,
		Creators:        nodes,
		CreatorCount:    len(nodes),
		FamilyCount:     families,
		SeriesCount:     series,
		EntityCount:     total,
	})
}

// handleFamilies renders the flat Series -> Release -> entities walk, absorbing what the
// retired /series explorer showed. It emits the same per-series anchors that explorer did,
// so every detail page's series link still resolves.
func (s *Server) handleFamilies(w http.ResponseWriter, r *http.Request) {
	groups := bestiary.SeriesGroups()
	nodes := make([]seriesNode, 0, len(groups))
	total := 0
	for _, sg := range groups {
		nodes = append(nodes, newSeriesNode(sg, seriesAnchor(sg.Series)))
		total += sg.EntityCount
	}
	s.render(w, "families", familiesView{
		Title:           "families",
		DatastarVersion: DatastarJSVersion,
		Series:          nodes,
		SeriesCount:     len(nodes),
		EntityCount:     total,
	})
}

// newSeriesNode converts one hoisted SeriesGroup into its render node. It is the SINGLE
// conversion both pages use, which is what keeps them from drifting: the base hoist has one
// implementation in the projection and one rendering here, so a "(base)" node cannot
// reappear on one page only.
//
// anchor is empty for a page that does not own the anchor namespace.
func newSeriesNode(sg bestiary.SeriesGroup, anchor string) seriesNode {
	n := seriesNode{
		Display:     sg.Series.String(),
		Anchor:      anchor,
		Hoisted:     entityRefs(sg.Hoisted),
		EntityCount: sg.EntityCount,
		Mixed:       sg.Shape() == bestiary.SeriesShapeMixed,
	}
	n.Releases = make([]releaseNode, 0, len(sg.Releases))
	for _, rg := range sg.Releases {
		n.Releases = append(n.Releases, releaseNode{
			Name:     rg.Release.Name,
			Entities: entityRefs(rg.Entities),
		})
	}
	return n
}

// entityRefs projects entities onto their (key, route path) link pairs. The path is minted
// with the same EntityRef.IRI(entityRoutePrefix) grammar the route dereferences, so a tree
// leaf and the page it opens can never disagree.
func entityRefs(ents []bestiary.Entity) []entityRef {
	out := make([]entityRef, 0, len(ents))
	for _, e := range ents {
		out = append(out, entityRef{
			Key:  e.Ref.String(),
			Path: e.Ref.IRI(entityRoutePrefix),
		})
	}
	return out
}

// seriesAnchor renders a stable, URL-fragment-safe same-page anchor for a Series. It is a
// DISPLAY slug (not a lookup key), so it need only be reproducible: the same Series always
// yields the same anchor on the explorer page and on any detail page that links to it. Every
// non-[a-z0-9] byte of the Series' display string folds to '-'.
func seriesAnchor(s bestiary.Series) string {
	var b strings.Builder
	b.WriteString("series-")
	for _, r := range strings.ToLower(s.String()) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
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

// ---- The budget-first fit calculator ----------------------------------------

// The calculator's presets. A budget the reader has not yet touched must still produce a
// page that answers something, and the headroom preset is deliberately NON-ZERO: the
// shipped VRAM formula carries no runtime-overhead term, so a fit verdict computed from
// a raw lower bound would say "yes" for models that run out of memory in practice. The
// slack is surfaced as a control the reader owns rather than smuggled into the datum.
const (
	calcDefaultBudgetGiB   = 24
	calcDefaultHeadroomGiB = 2
	// calcMaxRows caps what the page RENDERS, never what FitOver computes. The cap is
	// stated on the page whenever it bites (see calcView.Truncated) — a silently
	// truncated table reads as "this is everything", which is the one thing a coverage
	// statement exists to prevent.
	calcMaxRows = 200
	bytesPerGiB = int64(1) << 30
)

// calcQuery is the view-state the calculator SSE endpoint reads from the Datastar
// signals. Every field arrives as a string because that is what a bound form control
// produces; each is parsed tolerantly, so a malformed value degrades to the default view
// rather than to a 400 (the parseCtx precedent). This is NON-IDENTITY view-state: it
// selects which rows are listed, never which entity a link denotes.
type calcQuery struct {
	Budget     string `json:"budget"`
	Headroom   string `json:"headroom"`
	MinContext string `json:"minContext"`
}

// filter turns the raw signals into the library's FitFilter. Budget and headroom are read
// in GiB because that is the unit a reader knows their card in; the library works in
// bytes throughout and never sees the GiB figure.
func (q calcQuery) filter() bestiary.FitFilter {
	budget := parseGiB(q.Budget, calcDefaultBudgetGiB)
	headroom := parseGiB(q.Headroom, calcDefaultHeadroomGiB)
	return bestiary.FitFilter{
		Budget: bestiary.FitBudget{
			TotalBytes:    int64(budget * float64(bytesPerGiB)),
			HeadroomBytes: int64(headroom * float64(bytesPerGiB)),
		},
		MinContext: parseNonNegativeInt(q.MinContext),
	}
}

// parseGiB reads a GiB figure from a bound form control. A missing, non-numeric or
// negative value yields the preset; zero is honored, because a reader who deliberately
// sets the headroom to nothing has said something and must be believed.
func parseGiB(s string, preset float64) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return preset
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return preset
	}
	return v
}

// parseNonNegativeInt reads an optional token count. Anything unreadable is no
// constraint, never an error.
func parseNonNegativeInt(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// calcRow is the render view of one fit row: every cell precomputed, so the template
// makes no decisions and the same strings are asserted by the tests the reader sees.
type calcRow struct {
	Key      string
	Path     string
	Provider string
	Quant    string
	// Weights is the weights figure, e.g. "17.1 GB".
	Weights string
	// Context is the max-affordable-context cell. It is a token count, the literal
	// "unknown", or "no context budget remaining" -- never an unbounded figure.
	Context string
	// Bound names which limit produced Context; empty when Context carries no figure.
	Bound string
	// Badge is the honesty label, naming every qualification that applies. It is empty
	// only for a fully-measured row with a computable KV term.
	Badge string
	// BadgeTitle is the badge's long-form explanation.
	BadgeTitle string
	// Derived is true when the weights figure is an estimate rather than a file size.
	Derived bool
}

// fitCoverage carries the four denominators the coverage statement is rendered from. The
// sentence on the page interpolates these fields; it never contains a literal count,
// because a literal goes stale the first time the corpus moves.
type fitCoverage struct {
	Considered int
	Measured   int
	Derived    int
	Excluded   int
}

// calcView is the data model for the calculator page.
type calcView struct {
	Title           string
	DatastarVersion string
	// BudgetGiB / HeadroomGiB / MinContext seed the form controls with the values the
	// current render used, so the page and its signals never disagree on first paint.
	BudgetGiB   string
	HeadroomGiB string
	MinContext  string
	// Available is the budget actually spendable on weights, "NN.N GB".
	Available string
	Rows      []calcRow
	Coverage  fitCoverage
	// RowsTotal is how many rows fit the budget; RowsShown is how many are rendered.
	// They differ only when the render cap bites, which Truncated reports in words.
	RowsTotal int
	RowsShown int
	Truncated bool
}

// handleCalculator renders the budget-first calculator: the reader states a VRAM budget
// and an adjustable headroom, and the page lists what fits, largest first. It is the
// direction-reversal of the detail page's context recompute -- there you pick a model and
// ask how much VRAM; here you state the VRAM and ask what runs.
func (s *Server) handleCalculator(w http.ResponseWriter, r *http.Request) {
	q := calcQuery{
		Budget:     r.URL.Query().Get("budget"),
		Headroom:   r.URL.Query().Get("headroom"),
		MinContext: r.URL.Query().Get("minContext"),
	}
	s.render(w, "calculator", s.calcView(q))
}

// handleCalculatorSSE is the datastar wiring seam for the calculator: it reads the budget
// signals, re-runs the fit, and PatchElements the ranked table into #calc-results. The
// coverage statement is patched with it, because the denominators are part of the answer
// and must never lag the table they describe.
func (s *Server) handleCalculatorSSE(w http.ResponseWriter, r *http.Request) {
	var q calcQuery
	// Tolerant read: absent signals are not an error (the initial load has none).
	_ = datastar.ReadSignals(r, &q)

	var buf bytes.Buffer
	if err := s.fragments.ExecuteTemplate(&buf, "calc-results", s.calcView(q)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElements(
		buf.String(),
		datastar.WithSelector("#calc-results"),
		datastar.WithMode(datastar.ElementPatchModeInner),
	)
}

// calcView runs the fit and projects it for rendering. It is the ONE place the page's
// numbers come from, so the initial render and every SSE patch agree by construction.
func (s *Server) calcView(q calcQuery) calcView {
	f := q.filter()
	res := bestiary.FitOver(s.entities, f)

	shown := res.Rows
	if len(shown) > calcMaxRows {
		shown = shown[:calcMaxRows]
	}
	rows := make([]calcRow, 0, len(shown))
	for _, r := range shown {
		rows = append(rows, newCalcRow(r))
	}
	return calcView{
		Title:           "calculator",
		DatastarVersion: DatastarJSVersion,
		BudgetGiB:       trimFloat(float64(f.Budget.TotalBytes) / float64(bytesPerGiB)),
		HeadroomGiB:     trimFloat(float64(f.Budget.HeadroomBytes) / float64(bytesPerGiB)),
		MinContext:      strconv.Itoa(f.MinContext),
		Available:       vramLabel(f.Budget.Available()),
		Rows:            rows,
		Coverage: fitCoverage{
			Considered: res.EntitiesConsidered,
			Measured:   res.EntitiesMeasured,
			Derived:    res.EntitiesDerived,
			Excluded:   res.EntitiesExcluded,
		},
		RowsTotal: len(res.Rows),
		RowsShown: len(rows),
		Truncated: len(res.Rows) > len(rows),
	}
}

// newCalcRow projects one library row into display strings. Every honesty qualification
// the library typed is spelled out here in words: a derived figure says so, a weights-only
// bound says so, and the two say so TOGETHER when both apply, because a reader who sees
// only one of them has been told half the truth.
func newCalcRow(r bestiary.FitRow) calcRow {
	key := r.Ref.String()
	out := calcRow{
		Key:      key,
		Path:     r.Ref.IRI(entityRoutePrefix),
		Provider: string(r.Provider),
		Quant:    r.Quant.String(),
		Weights:  vramLabel(r.WeightsBytes),
		Derived:  r.WeightsBasis == bestiary.BasisDerived,
	}
	if r.QuantRaw != "" {
		out.Quant = r.QuantRaw
	}
	switch {
	case r.Bound == bestiary.ContextBoundUnknown:
		out.Context = "unknown"
	case r.NoContextBudget():
		out.Context = "no context budget remaining"
	default:
		out.Context = commaInt(int64(r.MaxContext)) + " tokens"
		out.Bound = r.Bound.String() + "-bound"
	}
	switch {
	case out.Derived:
		out.Badge = "derived · weights-only"
		out.BadgeTitle = "derived: weights estimated from the attested total parameter count " +
			"times bits-per-weight, not a measured file size. weights-only: the KV-cache term " +
			"is omitted because no architecture facts are published for this entity, so real " +
			"VRAM will be higher."
	case r.Partial:
		out.Badge = "weights-only"
		out.BadgeTitle = "weights-only lower bound: the KV-cache term is omitted because at " +
			"least one architecture fact is absent, so real VRAM will be higher."
	}
	return out
}

// trimFloat renders a GiB figure without a trailing ".0", so a whole-number budget seeds
// its form control as "24" rather than "24.0".
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// commaInt groups an integer with thousands separators. A six-figure token count is the
// kind of number a reader compares at a glance, and an ungrouped one defeats that.
func commaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}
