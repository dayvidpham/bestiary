package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestMain points XDG_CACHE_HOME at a throwaway directory for the whole test
// binary. The entity-view commands (providers, show --by-entity, sources) now
// best-effort open the default SQLite store when no --db-path is given; without
// this redirection those commands would create ~/.cache/bestiary/models.db during
// tests. A per-binary temp cache keeps every test hermetic.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bestiary-cli-test-cache")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: MkdirTemp: %v\n", err)
		os.Exit(1)
	}
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// tempDBPath returns a fresh SQLite path under the test's temp dir.
func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "models.db")
}

// -------------------------------------------------------------------------
// list --status
// -------------------------------------------------------------------------

// TestList_Status_UnknownValue_ActionableError asserts an unrecognised --status
// value is rejected with an actionable error naming the value and the valid set —
// never a silent empty result.
func TestList_Status_UnknownValue_ActionableError(t *testing.T) {
	err := run([]string{"list", "--status", "bogus", "--db-path", tempDBPath(t)})
	if err == nil {
		t.Fatalf("list --status bogus returned nil error; want an actionable rejection")
	}
	msg := err.Error()
	for _, want := range []string{"bogus", "valid values", "alpha", "beta", "deprecated"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q missing %q; want it to name the bad value and the valid set", msg, want)
		}
	}
}

// TestList_Status_None_KeepsModels asserts `--status none` (the status every baked
// model currently carries) filters to a non-empty set, while a status no baked
// model carries filters to the empty set — proving the filter is applied, not
// ignored.
func TestList_Status_None_KeepsModels(t *testing.T) {
	db := tempDBPath(t)

	var noneOut string
	if s := captureStdout(t, func() {
		if err := run([]string{"list", "--status", "none", "--output", "json", "--db-path", db}); err != nil {
			t.Fatalf("list --status none: %v", err)
		}
	}); true {
		noneOut = s
	}
	var noneModels []map[string]any
	if err := json.Unmarshal([]byte(noneOut), &noneModels); err != nil {
		t.Fatalf("list --status none json parse: %v", err)
	}
	if len(noneModels) == 0 {
		t.Fatalf("list --status none returned 0 models; the baked catalog should carry status-none models")
	}

	// The baked catalog carries a NON-EMPTY set of deprecated models (the vendored
	// models.dev catalog bakes them), and `--status deprecated` must surface them —
	// a strict, non-empty subset of the full set. Asserting len > 0 here is the
	// end-to-end guard that a status drop ANYWHERE on the vendored-catalog → bake →
	// StaticModels → merge → filter path is caught: without it, a filter that dropped
	// every non-none match (deprecated → empty) would still satisfy a bare
	// "deprecated < none" check and pass silently.
	depOut := captureStdout(t, func() {
		if err := run([]string{"list", "--status", "deprecated", "--output", "json", "--db-path", db}); err != nil {
			t.Fatalf("list --status deprecated: %v", err)
		}
	})
	var depModels []map[string]any
	if err := json.Unmarshal([]byte(depOut), &depModels); err != nil {
		t.Fatalf("list --status deprecated json parse: %v", err)
	}
	if len(depModels) == 0 {
		t.Errorf("list --status deprecated returned 0 models; the baked catalog bakes deprecated models, so a status drop on the CLI merge/query/filter path is the likely cause")
	}
	if len(depModels) >= len(noneModels) {
		t.Errorf("status filter not applied: deprecated=%d, none=%d (deprecated should be a strict subset)",
			len(depModels), len(noneModels))
	}
}

// TestList_NoStatus_DefaultUnchanged asserts that omitting --status leaves the
// default output (all merged models) intact.
func TestList_NoStatus_DefaultUnchanged(t *testing.T) {
	db := tempDBPath(t)
	out := captureStdout(t, func() {
		if err := run([]string{"list", "--output", "json", "--db-path", db}); err != nil {
			t.Fatalf("list: %v", err)
		}
	})
	var models []map[string]any
	if err := json.Unmarshal([]byte(out), &models); err != nil {
		t.Fatalf("list json parse: %v", err)
	}
	if len(models) < len(bestiary.StaticModels()) {
		t.Errorf("default list returned %d models, want >= %d (the full static catalog)",
			len(models), len(bestiary.StaticModels()))
	}
}

// -------------------------------------------------------------------------
// Entity-view metadata overlay + embedded-fallback notice
// -------------------------------------------------------------------------

// syncedStandaloneID is a metadata id whose family is absent from the catalog, so
// the join synthesizes a metadata-only standalone entity whose key is the bare
// family "zzfakemodel". Seeding this into the store lets an overlay test assert
// synced metadata surfaces in a view without depending on any baked entity.
const (
	syncedStandaloneID  = "zztestlab/zzfakemodel"
	syncedStandaloneKey = "zzfakemodel"
)

// seedMetadataStore opens (creating) a store at db and writes one models.dev
// metadata row with the given description, registering the models.dev data source
// first so the parent FK resolves.
func seedMetadataStore(t *testing.T, db, metadataID, description, lastSynced string) {
	t.Helper()
	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore(%q): %v", db, err)
	}
	defer store.Close()
	ctx := context.Background()

	ds := bestiary.DataSource{ID: bestiary.DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}
	di := bestiary.DatasetIngested{SourceID: bestiary.DataSourceModelsDev, IngestedAt: lastSynced, ParserSchema: modelsDevParserSchema}
	if err := store.UpsertDataSources(ctx, []bestiary.DataSource{ds}, []bestiary.DatasetIngested{di}); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	row := bestiary.EntityMetadata{
		MetadataID:  bestiary.MetadataID(metadataID),
		Description: description,
		License:     "MIT",
		Source:      bestiary.DataSourceModelsDev,
		LastSynced:  lastSynced,
	}
	if err := store.UpsertEntityMetadata(ctx, []bestiary.EntityMetadata{row}); err != nil {
		t.Fatalf("UpsertEntityMetadata: %v", err)
	}
}

// TestOverlay_SyncedMetadata_Surfaces asserts the store overlay makes synced
// metadata visible in an entity view (show --by-entity), and that WITHOUT the
// synced data the same identity is not found (static-only fallback), proving the
// overlay is what surfaces it.
func TestOverlay_SyncedMetadata_Surfaces(t *testing.T) {
	seeded := tempDBPath(t)
	seedMetadataStore(t, seeded, syncedStandaloneID, "SYNCED-DESCRIPTION-XYZ", "2026-07-12T00:00:05Z")

	out := captureStdout(t, func() {
		if err := run([]string{"show", "--by-entity", "--output", "table", "--db-path", seeded, syncedStandaloneKey}); err != nil {
			t.Fatalf("show --by-entity (seeded store): %v", err)
		}
	})
	if !strings.Contains(out, "SYNCED-DESCRIPTION-XYZ") {
		t.Errorf("overlaid view did not surface synced description; got:\n%s", out)
	}

	// Empty store → the standalone does not exist → ErrNotFound (static-only).
	empty := tempDBPath(t)
	err := run([]string{"show", "--by-entity", "--db-path", empty, syncedStandaloneKey})
	if err == nil {
		t.Errorf("show --by-entity with an empty store found %q; static-only fallback must not surface synced-only standalones", syncedStandaloneKey)
	}
}

// TestOverlay_JSON_CarriesMetadata asserts the JSON entity view emits the overlaid
// metadata (auto via marshal), pinning the synced description on Entity.Metadata.
func TestOverlay_JSON_CarriesMetadata(t *testing.T) {
	seeded := tempDBPath(t)
	seedMetadataStore(t, seeded, syncedStandaloneID, "JSON-SYNC-DESC", "2026-07-12T00:00:05Z")

	out := captureStdout(t, func() {
		if err := run([]string{"show", "--by-entity", "--output", "json", "--db-path", seeded, syncedStandaloneKey}); err != nil {
			t.Fatalf("show --by-entity json: %v", err)
		}
	})
	var ent struct {
		Metadata *struct {
			Description string
			License     string
		}
	}
	if err := json.Unmarshal([]byte(out), &ent); err != nil {
		t.Fatalf("entity json parse: %v\n%s", err, out)
	}
	if ent.Metadata == nil || ent.Metadata.Description != "JSON-SYNC-DESC" {
		t.Errorf("entity json Metadata.Description = %+v, want JSON-SYNC-DESC", ent.Metadata)
	}
}

// noticeCount runs an entity-view command and returns how many times the
// embedded-catalog fallback notice was printed to stderr, failing if the command
// itself errored (a command that errors before the overlay would give a misleading
// zero).
func noticeCount(t *testing.T, args ...string) int {
	t.Helper()
	var stderr string
	var runErr error
	_ = captureStdout(t, func() {
		stderr = captureStderr(t, func() { runErr = run(args) })
	})
	if runErr != nil {
		t.Fatalf("run %v returned error: %v", args, runErr)
	}
	return strings.Count(stderr, "using embedded catalog")
}

// TestEntityView_FallbackNotice_OnZeroSyncedRows asserts the ONE embedded-catalog
// stderr notice fires exactly once on EVERY zero-synced-rows path — a store absent
// / auto-created fresh-empty (the never-synced user), and a genuine open failure —
// and stays SILENT once a sync has populated the cache with metadata. This is the
// sync-discoverability intent: a view that shows baked-only metadata always hints
// at `sync`, and a view backed by synced metadata never nags.
func TestEntityView_FallbackNotice_OnZeroSyncedRows(t *testing.T) {
	// (1) Fresh, never-synced store: OpenStore auto-creates an empty DB that
	// contributes zero synced rows → notice exactly once.
	if n := noticeCount(t, "providers", "--db-path", tempDBPath(t), sizedCuratedKey); n != 1 {
		t.Errorf("fresh-empty store: notice appeared %d times, want exactly 1", n)
	}

	// (2) Genuine open failure (db path under a regular file → MkdirAll fails): still
	// zero synced rows → notice exactly once.
	fileAsDir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	badDB := filepath.Join(fileAsDir, "models.db")
	if n := noticeCount(t, "providers", "--db-path", badDB, sizedCuratedKey); n != 1 {
		t.Errorf("open-failure store: notice appeared %d times, want exactly 1", n)
	}

	// (3) A store populated with synced metadata → SILENT (synced data is present).
	seeded := tempDBPath(t)
	seedMetadataStore(t, seeded, syncedStandaloneID, "synced-desc", "2026-07-12T00:00:05Z")
	if n := noticeCount(t, "providers", "--db-path", seeded, sizedCuratedKey); n != 0 {
		t.Errorf("synced store: notice appeared %d times, want 0 (silent once synced)", n)
	}
}

// TestViewCommands_Offline is the offline proof for the entity-view commands: they
// resolve entirely from local data (baked catalog + SQLite cache) and construct no
// HTTP client, so they cannot dial out. Each command completes with only local
// state — a network failure could never affect them.
func TestViewCommands_Offline(t *testing.T) {
	db := tempDBPath(t)
	cases := [][]string{
		{"list", "--output", "json", "--db-path", db},
		{"providers", "--db-path", db, sizedCuratedKey},
		{"show", "--by-entity", "--db-path", db, sizedCuratedKey},
		{"sources", "--db-path", db, sizedCuratedKey},
		{"sources", "--history", "--db-path", db},
	}
	for _, args := range cases {
		t.Run(strings.Join(args[:2], "_"), func(t *testing.T) {
			_ = captureStderr(t, func() {
				_ = captureStdout(t, func() {
					if err := run(args); err != nil {
						t.Fatalf("offline view command %v failed: %v", args, err)
					}
				})
			})
		})
	}
}

// -------------------------------------------------------------------------
// sources --history
// -------------------------------------------------------------------------

// TestSourcesHistory_Store_Ascending seeds the store's ingest log with two models.dev
// rows (t1 < t2) and asserts --history lists both, ascending, joined to the source uri.
func TestSourcesHistory_Store_Ascending(t *testing.T) {
	db := tempDBPath(t)
	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ctx := context.Background()
	ds := bestiary.DataSource{ID: bestiary.DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}
	t1, t2 := "2026-07-10T00:00:00Z", "2026-07-11T00:00:00Z"
	ing := []bestiary.DatasetIngested{
		{SourceID: bestiary.DataSourceModelsDev, IngestedAt: t1, ParserSchema: 3},
		{SourceID: bestiary.DataSourceModelsDev, IngestedAt: t2, ParserSchema: 3},
	}
	if err := store.UpsertDataSources(ctx, []bestiary.DataSource{ds}, ing); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--history", "--output", "json", "--db-path", db}); err != nil {
			t.Fatalf("sources --history: %v", err)
		}
	})
	var rows []struct {
		ID         string `json:"ID"`
		URI        string `json:"URI"`
		IngestedAt string `json:"IngestedAt"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("history json parse: %v\n%s", err, out)
	}

	var mdRows []string
	for _, r := range rows {
		if r.ID == string(bestiary.DataSourceModelsDev) {
			mdRows = append(mdRows, r.IngestedAt)
			if r.URI != "https://models.dev/api.json" {
				t.Errorf("models.dev row URI = %q, want the FK-joined uri", r.URI)
			}
		}
	}
	if len(mdRows) != 2 || mdRows[0] != t1 || mdRows[1] != t2 {
		t.Errorf("models.dev history = %v, want ascending [%s %s]", mdRows, t1, t2)
	}
}

// TestSourcesHistory_CuratedFallback asserts that with an empty store, --history
// falls back to the curated ingest table (the committed datasources.json rows).
func TestSourcesHistory_CuratedFallback(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--history", "--output", "json", "--db-path", tempDBPath(t)}); err != nil {
			t.Fatalf("sources --history (curated fallback): %v", err)
		}
	})
	var rows []struct {
		ID         string `json:"ID"`
		IngestedAt string `json:"IngestedAt"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("history json parse: %v\n%s", err, out)
	}
	// The curated seed carries at least the models.dev row.
	found := false
	for _, r := range rows {
		if r.ID == string(bestiary.DataSourceModelsDev) {
			if want := bestiary.DatasetIngestHistoryFor(bestiary.DataSourceModelsDev); len(want) > 0 && r.IngestedAt == want[0].IngestedAt {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("curated --history fallback did not surface the committed models.dev ingest row; got:\n%s", out)
	}
}

// -------------------------------------------------------------------------
// sources --export round-trip
// -------------------------------------------------------------------------

// v3Doc mirrors the datasources.json v3 loader shape (same json tags) so both the
// export output and the committed seed file decode into it for content comparison.
type v3Doc = datasourcesExportDoc

// ingestBySource groups a v3 document's ingest rows by source id.
func ingestBySource(doc v3Doc) map[string][]datasetIngestedExport {
	m := map[string][]datasetIngestedExport{}
	for _, i := range doc.Ingested {
		m[i.SourceID] = append(m[i.SourceID], i)
	}
	return m
}

// TestSourcesExport_CuratedFallback_RoundTrip asserts that with an empty store the
// export reproduces the committed datasources.json content (round-trip content
// equality against the curated seed on disk).
func TestSourcesExport_CuratedFallback_RoundTrip(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", tempDBPath(t)}); err != nil {
			t.Fatalf("sources --export (curated fallback): %v", err)
		}
	})
	var got v3Doc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("export json parse: %v\n%s", err, out)
	}
	if got.SchemaVersion != 3 {
		t.Errorf("export schema_version = %d, want 3", got.SchemaVersion)
	}

	seed, err := os.ReadFile("../../parse/data/datasources.json")
	if err != nil {
		t.Fatalf("read committed datasources.json: %v", err)
	}
	var want v3Doc
	if err := json.Unmarshal(seed, &want); err != nil {
		t.Fatalf("parse committed datasources.json: %v", err)
	}

	// Sources: same id→(uri,canonical_name) set.
	wantSrc := map[string]datasourceExportRow{}
	for _, s := range want.Sources {
		wantSrc[s.ID] = s
	}
	gotSrc := map[string]datasourceExportRow{}
	for _, s := range got.Sources {
		gotSrc[s.ID] = s
	}
	if len(gotSrc) != len(wantSrc) {
		t.Errorf("export has %d sources, committed seed has %d", len(gotSrc), len(wantSrc))
	}
	for id, ws := range wantSrc {
		if gs, ok := gotSrc[id]; !ok || gs != ws {
			t.Errorf("export source %q = %+v, want %+v", id, gs, ws)
		}
	}

	// Ingested: same per-source rows (order-independent).
	wantIng, gotIng := ingestBySource(want), ingestBySource(got)
	for id, wrows := range wantIng {
		grows := gotIng[id]
		if len(grows) != len(wrows) {
			t.Errorf("export ingested for %q has %d rows, seed has %d", id, len(grows), len(wrows))
			continue
		}
		wset := map[datasetIngestedExport]bool{}
		for _, w := range wrows {
			wset[w] = true
		}
		for _, g := range grows {
			if !wset[g] {
				t.Errorf("export ingested row %+v for %q not present in committed seed", g, id)
			}
		}
	}
}

// TestSourcesExport_Store_RoundTrip seeds the store's ingest log and asserts the
// export reflects the store history (content equality against the store), and that
// the document is a valid v3 shape (schema_version 3, source dimension present).
func TestSourcesExport_Store_RoundTrip(t *testing.T) {
	db := tempDBPath(t)
	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ctx := context.Background()
	ds := bestiary.DataSource{ID: bestiary.DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}
	t1, t2 := "2026-07-10T00:00:00Z", "2026-07-11T00:00:00Z"
	ing := []bestiary.DatasetIngested{
		{SourceID: bestiary.DataSourceModelsDev, IngestedAt: t1, ParserSchema: 3},
		{SourceID: bestiary.DataSourceModelsDev, IngestedAt: t2, ParserSchema: 3},
	}
	if err := store.UpsertDataSources(ctx, []bestiary.DataSource{ds}, ing); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", db}); err != nil {
			t.Fatalf("sources --export (store): %v", err)
		}
	})
	var got v3Doc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("export json parse: %v\n%s", err, out)
	}
	if got.SchemaVersion != 3 {
		t.Errorf("export schema_version = %d, want 3", got.SchemaVersion)
	}
	// The source dimension is present (from the curated table).
	hasModelsDev := false
	for _, s := range got.Sources {
		if s.ID == string(bestiary.DataSourceModelsDev) {
			hasModelsDev = true
		}
	}
	if !hasModelsDev {
		t.Errorf("export sources missing models.dev dimension row")
	}
	// Ingested reflects the store history (t1, t2) for models.dev.
	md := ingestBySource(got)[string(bestiary.DataSourceModelsDev)]
	if len(md) != 2 {
		t.Fatalf("export models.dev ingested = %d rows, want 2 (the seeded store history)", len(md))
	}
	seen := map[string]bool{}
	for _, r := range md {
		seen[r.IngestedAt] = true
	}
	if !seen[t1] || !seen[t2] {
		t.Errorf("export models.dev ingested = %+v, want to contain %s and %s", md, t1, t2)
	}
}

// TestSourcesExport_WritesFile asserts --export to a path writes the document to
// that file (not stdout).
func TestSourcesExport_WritesFile(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "export.json")
	stdout := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", tempDBPath(t), outPath}); err != nil {
			t.Fatalf("sources --export <path>: %v", err)
		}
	})
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("export to a file also wrote to stdout: %q", stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	var doc v3Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("exported file is not valid v3 json: %v", err)
	}
	if doc.SchemaVersion != 3 {
		t.Errorf("exported file schema_version = %d, want 3", doc.SchemaVersion)
	}
}

// -------------------------------------------------------------------------
// sync — httptest, metadata + ingest log + attestations
// -------------------------------------------------------------------------

// syncTestServer serves a minimal api.json and models.json fixture, routing on the
// request path so a single WithBaseURL(base+"/api.json") client reaches both.
func syncTestServer(t *testing.T, apiJSON, modelsJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api.json"):
			fmt.Fprint(w, apiJSON)
		case strings.HasSuffix(r.URL.Path, "/models.json"):
			fmt.Fprint(w, modelsJSON)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

const minimalAPIJSON = `{"testprov":{"models":{"synced-model-1":{"id":"synced-model-1","name":"M1","family":"testfam"}}}}`
const minimalModelsJSON = `{"testlab/synced-model":{"id":"testlab/synced-model","name":"Synced","description":"a synced description","license":"Apache-2.0"}}`

// TestSync_PersistsMetadataAndIngestLog drives runSyncClient against httptest with
// a deterministic clock, twice at t1≠t2, and asserts: the metadata is persisted
// (Source=models.dev, LastSynced=t2); the append-only ingest log holds BOTH rows
// (current=MAX=t2); and the attestations are written (the sync succeeds, which
// requires the models.dev DataSource to be registered before the FK-checked
// entity_source rows).
func TestSync_PersistsMetadataAndIngestLog(t *testing.T) {
	srv := syncTestServer(t, minimalAPIJSON, minimalModelsJSON)
	client := bestiary.NewClient(bestiary.WithBaseURL(srv.URL + "/api.json"))
	db := tempDBPath(t)

	// Deterministic clock: two distinct timestamps across two syncs.
	t1, t2 := "2026-07-12T00:00:01Z", "2026-07-12T00:00:02Z"
	stamps := []string{t1, t2}
	orig := syncNow
	i := 0
	syncNow = func() string {
		s := stamps[i]
		if i < len(stamps)-1 {
			i++
		}
		return s
	}
	t.Cleanup(func() { syncNow = orig })

	for _, sync := range []int{0, 1} {
		i = sync
		_ = captureStdout(t, func() {
			if err := runSyncClient(client, "", bestiary.FormatJSON, db); err != nil {
				t.Fatalf("runSyncClient (sync %d): %v", sync, err)
			}
		})
	}

	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Metadata persisted, attributed + stamped with the latest sync.
	meta, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata: %v", err)
	}
	var got *bestiary.EntityMetadata
	for i := range meta {
		if meta[i].MetadataID == "testlab/synced-model" {
			got = &meta[i]
		}
	}
	if got == nil {
		t.Fatalf("synced metadata not persisted; got %d rows", len(meta))
	}
	if got.Description != "a synced description" || got.Source != bestiary.DataSourceModelsDev || got.LastSynced != t2 {
		t.Errorf("persisted metadata = {desc:%q src:%q synced:%q}, want {…, models.dev, %s}",
			got.Description, got.Source, got.LastSynced, t2)
	}

	// Two syncs at t1≠t2 → two ingest rows; current = MAX = t2.
	hist, err := store.QueryIngestHistory(ctx, bestiary.DataSourceModelsDev)
	if err != nil {
		t.Fatalf("QueryIngestHistory: %v", err)
	}
	if len(hist) != 2 || hist[0].IngestedAt != t1 || hist[1].IngestedAt != t2 {
		t.Fatalf("ingest history = %+v, want two rows ascending [%s %s]", hist, t1, t2)
	}
	cur, err := store.QueryCurrentIngests(ctx)
	if err != nil {
		t.Fatalf("QueryCurrentIngests: %v", err)
	}
	found := false
	for _, c := range cur {
		if c.SourceID == bestiary.DataSourceModelsDev {
			found = true
			if c.IngestedAt != t2 {
				t.Errorf("current models.dev ingest = %s, want MAX %s", c.IngestedAt, t2)
			}
		}
	}
	if !found {
		t.Errorf("QueryCurrentIngests has no models.dev row")
	}
}

// TestEntitySourcesForModels_DerivesAndDedups asserts the attestation derivation
// builds one models.dev row per DISTINCT entity key (many models → one entity
// collapse) using the same identity projection the registry uses.
func TestEntitySourcesForModels_DerivesAndDedups(t *testing.T) {
	models := []bestiary.ModelInfo{
		{ID: "a", Provider: "p1", Family: "fam", Version: "1"},
		{ID: "b", Provider: "p2", Family: "fam", Version: "1"}, // same entity key as a
		{ID: "c", Provider: "p3", Family: "other", Version: "2"},
	}
	got := entitySourcesForModels(models)
	keys := map[string]bool{}
	for _, es := range got {
		if es.SourceID != bestiary.DataSourceModelsDev {
			t.Errorf("attestation source = %q, want models.dev", es.SourceID)
		}
		if keys[es.EntityKey] {
			t.Errorf("duplicate attestation for entity key %q", es.EntityKey)
		}
		keys[es.EntityKey] = true
	}
	if len(keys) != 2 {
		t.Errorf("derived %d distinct entity keys, want 2 (fam@1 collapses two models)", len(keys))
	}
	want := entityRefForModel(models[0]).String()
	if !keys[want] {
		t.Errorf("missing expected entity key %q in %v", want, keys)
	}
}

// -------------------------------------------------------------------------
// drift warning
// -------------------------------------------------------------------------

// TestDriftedModelCount counts live models absent from the embedded set.
func TestDriftedModelCount(t *testing.T) {
	embedded := []bestiary.ModelInfo{
		{ID: "known", Provider: "p"},
	}
	fetched := []bestiary.ModelInfo{
		{ID: "known", Provider: "p"}, // present → not drift
		{ID: "new1", Provider: "p"},  // absent → drift
		{ID: "new2", Provider: "q"},  // absent → drift
		{ID: "known", Provider: "q"}, // same id, different provider → drift
	}
	if n := driftedModelCount(fetched, embedded); n != 3 {
		t.Errorf("driftedModelCount = %d, want 3", n)
	}
}

// TestSync_DriftWarning drives sync with more than driftWarningThreshold synthetic
// new models and asserts the ONE stderr warning fires; and with a small fixture
// asserts it does NOT.
func TestSync_DriftWarning(t *testing.T) {
	// Over-threshold: driftWarningThreshold+1 synthetic models, none in the catalog.
	var sb strings.Builder
	sb.WriteString(`{"zzdriftprov":{"models":{`)
	for i := 0; i <= driftWarningThreshold; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `"zz-drift-%d":{"id":"zz-drift-%d","name":"D","family":"zzdrift"}`, i, i)
	}
	sb.WriteString(`}}}`)
	bigSrv := syncTestServer(t, sb.String(), minimalModelsJSON)
	bigClient := bestiary.NewClient(bestiary.WithBaseURL(bigSrv.URL + "/api.json"))

	stderr := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runSyncClient(bigClient, "", bestiary.FormatJSON, tempDBPath(t)); err != nil {
				t.Fatalf("sync (over-threshold): %v", err)
			}
		})
	})
	if n := strings.Count(stderr, "vendored models.dev snapshot is stale"); n != 1 {
		t.Errorf("drift warning appeared %d times over-threshold, want exactly 1; stderr:\n%s", n, stderr)
	}

	// Under-threshold: a single new model → no warning.
	smallSrv := syncTestServer(t, minimalAPIJSON, minimalModelsJSON)
	smallClient := bestiary.NewClient(bestiary.WithBaseURL(smallSrv.URL + "/api.json"))
	stderr2 := captureStderr(t, func() {
		_ = captureStdout(t, func() {
			if err := runSyncClient(smallClient, "", bestiary.FormatJSON, tempDBPath(t)); err != nil {
				t.Fatalf("sync (under-threshold): %v", err)
			}
		})
	})
	if strings.Contains(stderr2, "vendored models.dev snapshot is stale") {
		t.Errorf("drift warning fired under threshold; stderr:\n%s", stderr2)
	}
}

// -------------------------------------------------------------------------
// benchmark table rendering
// -------------------------------------------------------------------------

// TestBenchmarkTable_Top5_Footer_ScoreRaw asserts the benchmark table shows at
// most 5 rows with a "… and N more" footer, and that the SCORE cell renders
// ScoreRaw for a non-numeric score and the numeric Score otherwise.
func TestBenchmarkTable_Top5_Footer_ScoreRaw(t *testing.T) {
	var benches []bestiary.BenchmarkResult
	// Bench0: non-numeric score (ScoreRaw). Bench1: numeric. Bench2..6: filler.
	benches = append(benches, bestiary.BenchmarkResult{Name: "Bench0", Metric: "acc", ScoreRaw: "PASS-RAW"})
	benches = append(benches, bestiary.BenchmarkResult{Name: "Bench1", Metric: "acc", Score: 87.5})
	for i := 2; i < 7; i++ {
		benches = append(benches, bestiary.BenchmarkResult{Name: fmt.Sprintf("Bench%d", i), Metric: "acc", Score: float64(i)})
	}

	var sb strings.Builder
	writeBenchmarkTable(&sb, benches)
	out := sb.String()

	for _, want := range []string{"NAME", "SCORE", "METRIC", "HARNESS", "DATE", "SOURCE", "Benchmarks (7)"} {
		if !strings.Contains(out, want) {
			t.Errorf("benchmark table missing %q; got:\n%s", want, out)
		}
	}
	// Top 5 shown, last 2 hidden behind the footer.
	for _, shown := range []string{"Bench0", "Bench1", "Bench2", "Bench3", "Bench4"} {
		if !strings.Contains(out, shown) {
			t.Errorf("benchmark %q should be in the top-5 rows; got:\n%s", shown, out)
		}
	}
	for _, hidden := range []string{"Bench5", "Bench6"} {
		if strings.Contains(out, hidden) {
			t.Errorf("benchmark %q should be beyond the top-5 cap; got:\n%s", hidden, out)
		}
	}
	if !strings.Contains(out, "… and 2 more (use --output json)") {
		t.Errorf("missing top-5 truncation footer; got:\n%s", out)
	}
	// SCORE cell: raw for non-numeric, numeric otherwise; never blank.
	if !strings.Contains(out, "PASS-RAW") {
		t.Errorf("non-numeric score cell should render ScoreRaw; got:\n%s", out)
	}
	if !strings.Contains(out, "87.5") {
		t.Errorf("numeric score cell should render Score; got:\n%s", out)
	}
}

// TestBenchmarkTable_Empty_NoOutput asserts nothing is written when there are no
// benchmarks (an entity with no metadata renders exactly as before).
func TestBenchmarkTable_Empty_NoOutput(t *testing.T) {
	var sb strings.Builder
	writeBenchmarkTable(&sb, nil)
	if sb.Len() != 0 {
		t.Errorf("empty benchmark set wrote output: %q", sb.String())
	}
}

// TestInstanceTable_StatusColumn asserts the instance table gains a STATUS column
// only when an instance carries a non-None status, and renders the status name.
func TestInstanceTable_StatusColumn(t *testing.T) {
	insts := []bestiary.ProviderInstance{
		{ID: "m1", Provider: "p1"},
		{ID: "m2", Provider: "p2"},
	}

	// All None → no STATUS column.
	var noStatus strings.Builder
	writeInstanceTableWithStatus(&noStatus, insts, []bestiary.ModelStatus{bestiary.StatusNone, bestiary.StatusNone})
	if strings.Contains(noStatus.String(), "STATUS") {
		t.Errorf("STATUS column present when no instance carries a status:\n%s", noStatus.String())
	}

	// One Beta → STATUS column with the status name.
	var withStatus strings.Builder
	writeInstanceTableWithStatus(&withStatus, insts, []bestiary.ModelStatus{bestiary.StatusBeta, bestiary.StatusNone})
	out := withStatus.String()
	if !strings.Contains(out, "STATUS") {
		t.Errorf("STATUS column missing when an instance carries a status:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("STATUS column should render the status name 'beta':\n%s", out)
	}
}
