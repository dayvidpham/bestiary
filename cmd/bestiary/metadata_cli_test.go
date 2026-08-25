package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
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
		{"entities", "--output", "json", "--db-path", db},
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
// entities (registry-wide enumeration)
// -------------------------------------------------------------------------

// entitiesRowFields returns the whitespace-split cells of the `entities` table row
// whose ENTITY KEY equals key, or nil when no such row exists. Splitting on
// whitespace makes the assertion robust to the column padding.
func entitiesRowFields(out, key string) []string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 1 && f[0] == key {
			return f
		}
	}
	return nil
}

// TestEntities_Table_ListsMetadataOnlyStandalone asserts the table enumeration
// surfaces a metadata-only standalone (a synced identity with no provider
// instances) with PROVIDERS 0 and METADATA yes — the discoverability the command
// exists to provide, since such an entity is otherwise reachable only by exact key.
func TestEntities_Table_ListsMetadataOnlyStandalone(t *testing.T) {
	db := tempDBPath(t)
	seedMetadataStore(t, db, syncedStandaloneID, "STANDALONE-DESC", "2026-07-12T00:00:05Z")

	var out string
	_ = captureStderr(t, func() {
		out = captureStdout(t, func() {
			if err := run([]string{"entities", "--output", "table", "--db-path", db}); err != nil {
				t.Fatalf("entities table: %v", err)
			}
		})
	})

	fields := entitiesRowFields(out, syncedStandaloneKey)
	if fields == nil {
		t.Fatalf("entities table missing metadata-only standalone %q; got:\n%s", syncedStandaloneKey, out)
	}
	// ENTITY KEY | PROVIDERS | METADATA | BENCHMARKS
	if len(fields) != 4 || fields[1] != "0" || fields[2] != "yes" || fields[3] != "0" {
		t.Errorf("standalone row = %v, want [%s 0 yes 0]", fields, syncedStandaloneKey)
	}
}

// TestEntities_JSON_RoundTrips_SortedByKey asserts the json enumeration emits the
// full Entity objects, sorted ascending by entity key, and that the metadata-only
// standalone round-trips (its metadata and empty provider set both survive).
func TestEntities_JSON_RoundTrips_SortedByKey(t *testing.T) {
	db := tempDBPath(t)
	seedMetadataStore(t, db, syncedStandaloneID, "RT-STANDALONE-DESC", "2026-07-12T00:00:05Z")

	var out string
	_ = captureStderr(t, func() {
		out = captureStdout(t, func() {
			if err := run([]string{"entities", "--output", "json", "--db-path", db}); err != nil {
				t.Fatalf("entities json: %v", err)
			}
		})
	})

	var ents []bestiary.Entity
	if err := json.Unmarshal([]byte(out), &ents); err != nil {
		t.Fatalf("entities json parse: %v\n%s", err, out)
	}
	if len(ents) == 0 {
		t.Fatal("entities json is empty")
	}

	// Sorted ascending by entity key (pinned order).
	for i := 1; i < len(ents); i++ {
		if ents[i].Ref.String() < ents[i-1].Ref.String() {
			t.Errorf("entities not sorted by key: %q before %q", ents[i-1].Ref.String(), ents[i].Ref.String())
		}
	}

	// The metadata-only standalone round-trips: present, metadata preserved, no instances.
	found := false
	for _, e := range ents {
		if e.Ref.String() == syncedStandaloneKey {
			found = true
			if e.Metadata == nil || e.Metadata.Description != "RT-STANDALONE-DESC" {
				t.Errorf("standalone metadata = %+v, want description RT-STANDALONE-DESC", e.Metadata)
			}
			if len(e.Providers) != 0 {
				t.Errorf("standalone Providers = %d, want 0", len(e.Providers))
			}
		}
	}
	if !found {
		t.Errorf("entities json missing metadata-only standalone %q", syncedStandaloneKey)
	}
}

// TestEntities_UsageMentionsCommand asserts the top-level usage and the
// unknown-command error both advertise the new `entities` subcommand.
func TestEntities_UsageMentionsCommand(t *testing.T) {
	if err := run(nil); err == nil || !strings.Contains(err.Error(), "entities") {
		t.Errorf("empty-args usage should mention 'entities'; got %v", err)
	}
	if err := run([]string{"definitely-not-a-command"}); err == nil || !strings.Contains(err.Error(), "entities") {
		t.Errorf("unknown-command error should list 'entities'; got %v", err)
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

// seedV3Doc loads the committed curated datasources.json seed as a v3 document so
// union tests assert against the on-disk curated provenance without hardcoding its
// (refreshable) timestamps.
func seedV3Doc(t *testing.T) v3Doc {
	t.Helper()
	seed, err := os.ReadFile("../../parse/data/datasources.json")
	if err != nil {
		t.Fatalf("read committed datasources.json: %v", err)
	}
	var doc v3Doc
	if err := json.Unmarshal(seed, &doc); err != nil {
		t.Fatalf("parse committed datasources.json: %v", err)
	}
	return doc
}

// TestSourcesExport_Union_StoreAndCurated seeds the store with models.dev ingest
// rows absent from the curated seed and asserts the export is the UNION of the
// store history and the curated seed: both the seeded store rows and the curated
// models.dev rows appear, and the curated-only Ollama provenance survives.
// Dropping the curated arm of the union would drop the Ollama rows, so this test
// goes RED — the regression the fix exists to prevent.
func TestSourcesExport_Union_StoreAndCurated(t *testing.T) {
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
			t.Fatalf("sources --export (union): %v", err)
		}
	})
	var got v3Doc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("export json parse: %v\n%s", err, out)
	}
	if got.SchemaVersion != 3 {
		t.Errorf("export schema_version = %d, want 3", got.SchemaVersion)
	}

	curated := ingestBySource(seedV3Doc(t))
	gotIng := ingestBySource(got)

	// models.dev: the union is the curated seed rows plus the two seeded store rows.
	wantMD := map[datasetIngestedExport]bool{}
	for _, r := range curated[string(bestiary.DataSourceModelsDev)] {
		wantMD[r] = true
	}
	wantMD[datasetIngestedExport{SourceID: string(bestiary.DataSourceModelsDev), IngestedAt: t1, ParserSchema: 3}] = true
	wantMD[datasetIngestedExport{SourceID: string(bestiary.DataSourceModelsDev), IngestedAt: t2, ParserSchema: 3}] = true

	gotMD := map[datasetIngestedExport]bool{}
	for _, r := range gotIng[string(bestiary.DataSourceModelsDev)] {
		gotMD[r] = true
	}
	if len(gotMD) != len(wantMD) {
		t.Errorf("union models.dev has %d distinct rows, want %d; got=%+v", len(gotMD), len(wantMD), gotIng[string(bestiary.DataSourceModelsDev)])
	}
	for r := range wantMD {
		if !gotMD[r] {
			t.Errorf("union models.dev missing expected row %+v", r)
		}
	}

	// Curated-only source: Ollama provenance lives only in the curated seed (a live
	// sync never writes it to the store), so it MUST survive the union.
	ollama := string(bestiary.DataSourceOllama)
	if len(curated[ollama]) == 0 {
		t.Fatalf("curated seed unexpectedly has no ollama ingest rows")
	}
	gotOllama := map[datasetIngestedExport]bool{}
	for _, r := range gotIng[ollama] {
		gotOllama[r] = true
	}
	for _, r := range curated[ollama] {
		if !gotOllama[r] {
			t.Errorf("union dropped curated-only ollama row %+v (curated arm missing?)", r)
		}
	}
}

// TestSourcesExport_Union_DedupExactKey seeds the store with a models.dev row whose
// exact (source_id, ingested_at) key already exists in the curated seed and asserts
// the union carries that key exactly once — no duplicate ingest row.
func TestSourcesExport_Union_DedupExactKey(t *testing.T) {
	mdCurated := ingestBySource(seedV3Doc(t))[string(bestiary.DataSourceModelsDev)]
	if len(mdCurated) == 0 {
		t.Fatalf("curated seed unexpectedly has no models.dev ingest rows")
	}
	dup := mdCurated[0] // a real curated (source_id, ingested_at) to collide on

	db := tempDBPath(t)
	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	ds := bestiary.DataSource{ID: bestiary.DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}
	ing := []bestiary.DatasetIngested{
		{SourceID: bestiary.DataSourceModelsDev, IngestedAt: dup.IngestedAt, ParserSchema: dup.ParserSchema},
	}
	if err := store.UpsertDataSources(context.Background(), []bestiary.DataSource{ds}, ing); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	store.Close()

	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", db}); err != nil {
			t.Fatalf("sources --export (dedup): %v", err)
		}
	})
	var got v3Doc
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("export json parse: %v\n%s", err, out)
	}

	n := 0
	for _, r := range got.Ingested {
		if r.SourceID == string(bestiary.DataSourceModelsDev) && r.IngestedAt == dup.IngestedAt {
			n++
		}
	}
	if n != 1 {
		t.Errorf("dedup: models.dev row at %s appears %d times in the union, want exactly 1", dup.IngestedAt, n)
	}
}

// TestSourcesExport_BestiarySelfReferentialRow is the export oracle for the
// self-referential bestiary source row: that source is a first-class dimension
// row, so `sources --export` emits it with the id/uri/canonical-name reached by the FK join, and the
// emitted document round-trips — re-decoding the export and re-exporting from it
// yields the same bestiary row, which is what makes the export promotable straight
// back into parse/data/datasources.json.
//
// It matters as its own case (rather than riding on the generic seed comparison)
// because this row is the FK target of every self-minted canonical nomen: an export
// that omitted it would produce a seed whose promotion breaks the nomina FK.
func TestSourcesExport_BestiarySelfReferentialRow(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", tempDBPath(t)}); err != nil {
			t.Fatalf("sources --export: %v", err)
		}
	})
	var doc v3Doc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("export json parse: %v\n%s", err, out)
	}

	want, ok := bestiary.DataSourceByID(bestiary.DataSourceBestiary)
	if !ok {
		t.Fatalf("DataSourceByID(%q) missed; the curated seed must carry the self-referential row", bestiary.DataSourceBestiary)
	}
	var got []datasourceExportRow
	for _, r := range doc.Sources {
		if r.ID == string(bestiary.DataSourceBestiary) {
			got = append(got, r)
		}
	}
	if len(got) != 1 {
		t.Fatalf("export carries %d rows for source %q, want exactly 1 (it is the dimension primary key); sources: %+v",
			len(got), bestiary.DataSourceBestiary, doc.Sources)
	}
	if got[0].URI != want.URI || got[0].CanonicalName != want.CanonicalName {
		t.Errorf("exported bestiary row = {uri:%q name:%q}, want {uri:%q name:%q} (must come from the DataSourceByID join)",
			got[0].URI, got[0].CanonicalName, want.URI, want.CanonicalName)
	}
	ingest := ingestBySource(doc)
	if len(ingest[string(bestiary.DataSourceBestiary)]) == 0 {
		t.Errorf("export carries no ingest row for source %q; the seed row must survive the union",
			bestiary.DataSourceBestiary)
	}

	// Round-trip: write the export to a file, re-export, and compare the bestiary
	// row and its ingest rows. A lossy export would diverge on the second pass.
	outPath := filepath.Join(t.TempDir(), "datasources.json")
	if err := os.WriteFile(outPath, []byte(out), 0o644); err != nil {
		t.Fatalf("write exported doc: %v", err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read exported doc: %v", err)
	}
	var reread v3Doc
	if err := json.Unmarshal(data, &reread); err != nil {
		t.Fatalf("re-decode exported doc: %v", err)
	}
	second := captureStdout(t, func() {
		if err := run([]string{"sources", "--export", "--db-path", tempDBPath(t)}); err != nil {
			t.Fatalf("sources --export (second pass): %v", err)
		}
	})
	var doc2 v3Doc
	if err := json.Unmarshal([]byte(second), &doc2); err != nil {
		t.Fatalf("second export json parse: %v", err)
	}
	if !reflect.DeepEqual(reread.Sources, doc2.Sources) {
		t.Errorf("export sources are not round-trip stable:\n first: %+v\nsecond: %+v", reread.Sources, doc2.Sources)
	}
	if !reflect.DeepEqual(ingestBySource(reread)[string(bestiary.DataSourceBestiary)],
		ingestBySource(doc2)[string(bestiary.DataSourceBestiary)]) {
		t.Errorf("bestiary ingest rows are not round-trip stable:\n first: %+v\nsecond: %+v",
			ingestBySource(reread)[string(bestiary.DataSourceBestiary)],
			ingestBySource(doc2)[string(bestiary.DataSourceBestiary)])
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

// TestSync_KeyIdentity_ReDerivesSizeAndSources drives runSyncClient against httptest
// with a size-bearing model ID and asserts the sync/cache round-trip re-derives the
// SIZE dimension identically to the static decomposition: the persisted row carries no
// param_size column, so a correct read-back proves the wire-decode joint (on the way
// in) and the store-scan joint (on the way out) agree with the codegen bake. Because
// ParamSize is a pure function of the ID, a synced (ID, Provider) can never be de-sized
// by the most-recent-wins merge — the static and synced rows resolve the same size. It
// also pins the entity key's #size segment and the [models.dev] Sources projection.
// (The Family/Version decomposition is deliberately NOT asserted here: full family
// decomposition is a codegen-time step, so a live-sync row keeps its raw family — a
// pre-existing codegen-vs-runtime difference outside the #size re-key.)
func TestSync_KeyIdentity_ReDerivesSizeAndSources(t *testing.T) {
	const sizedAPIJSON = `{"testprov":{"models":{"qwen3-30b-a3b":{"id":"qwen3-30b-a3b","name":"Q","family":"qwen"},"grok-4.20-beta-0309-reasoning":{"id":"grok-4.20-beta-0309-reasoning","name":"G","family":"grok"}}}}`
	srv := syncTestServer(t, sizedAPIJSON, "{}")
	client := bestiary.NewClient(bestiary.WithBaseURL(srv.URL + "/api.json"))
	db := tempDBPath(t)

	_ = captureStdout(t, func() {
		if err := runSyncClient(client, "", bestiary.FormatJSON, db); err != nil {
			t.Fatalf("runSyncClient: %v", err)
		}
	})

	store, err := bestiary.OpenStore(db)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	models, err := store.QueryModels(context.Background(), "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	var m *bestiary.ModelInfo
	for i := range models {
		if models[i].ID == "qwen3-30b-a3b" {
			m = &models[i]
		}
	}
	if m == nil {
		t.Fatalf("synced model not found; got %d cached rows", len(models))
	}

	// ParamSize + shape ints re-derived on the cache read path, identical to the static
	// decomposition of the same ID.
	wantSize, _ := bestiary.EnrichedParamSize("qwen3-30b-a3b")
	wantShape, _ := bestiary.ParseParamShape(wantSize)
	if m.ParamSize != wantSize || m.ParamSize != "30b-a3b" {
		t.Errorf("synced ParamSize = %q, want %q (re-derived from ID; merge must not de-size)", m.ParamSize, "30b-a3b")
	}
	if m.TotalParams != wantShape.TotalParams || m.ActiveParams != wantShape.ActiveParams {
		t.Errorf("synced shape ints = {total:%d active:%d}, want {%d %d}",
			m.TotalParams, m.ActiveParams, wantShape.TotalParams, wantShape.ActiveParams)
	}

	// The entity key carries the re-derived #size segment, and the Sources projection is
	// exactly one models.dev attestation keyed by that entity.
	key := entityRefForModel(*m).String()
	if !strings.Contains(key, "#30b-a3b") {
		t.Errorf("synced entity key = %q, want a #30b-a3b segment", key)
	}
	att := entitySourcesForModels([]bestiary.ModelInfo{*m})
	if len(att) != 1 || att[0].SourceID != bestiary.DataSourceModelsDev || att[0].EntityKey != key {
		t.Errorf("attestations = %+v, want one models.dev row for entity key %q", att, key)
	}

	// Stage/StageRaw re-derived on the same cache read path, identical to the static
	// decomposition. The sized (qwen) row carries no stage marker; the beta row
	// re-derives Stage=StageBeta from its ID. This proves the stage axis rides the
	// same enrichment joint as ParamSize (a synced-then-cached row's stage matches the
	// baked static row).
	if m.Stage != bestiary.StageNone || m.StageRaw != "" {
		t.Errorf("synced qwen Stage/StageRaw = {%v %q}, want {none \"\"}", m.Stage, m.StageRaw)
	}
	var beta *bestiary.ModelInfo
	for i := range models {
		if models[i].ID == "grok-4.20-beta-0309-reasoning" {
			beta = &models[i]
		}
	}
	if beta == nil {
		t.Fatalf("synced beta model not found; got %d cached rows", len(models))
	}
	if beta.Stage != bestiary.StageBeta {
		t.Errorf("synced beta Stage = %v, want StageBeta (re-derived from the -beta ID on read)", beta.Stage)
	}
	if beta.StageRaw != "" {
		t.Errorf("synced beta StageRaw = %q, want \"\" (reserved for the Other path)", beta.StageRaw)
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

// TestBenchmarkTable_LongName_Truncated_Note_Aligned asserts that a benchmark
// name wider than the NAME column is truncated with a trailing "…", that the
// columns stay aligned (the NAME cell occupies exactly benchmarkNameColWidth
// display runes so the SCORE column starts at a fixed offset on every row), and
// that a single truncation note is printed after the table.
func TestBenchmarkTable_LongName_Truncated_Note_Aligned(t *testing.T) {
	const longName = "Artificial Analysis Coding Index" // 32 runes > benchmarkNameColWidth
	benches := []bestiary.BenchmarkResult{
		{Name: longName, Metric: "acc", Score: 42},
		{Name: "MMLU", Metric: "acc", Score: 87.5},
	}

	var sb strings.Builder
	writeBenchmarkTable(&sb, benches)
	out := sb.String()

	// Mutation guard: unbounded rendering would leak the full name verbatim.
	if strings.Contains(out, longName) {
		t.Errorf("long benchmark name should be truncated, not rendered in full; got:\n%s", out)
	}
	// The truncated cell is (benchmarkNameColWidth-1) content runes + "…".
	wantCell := string([]rune(longName)[:benchmarkNameColWidth-1]) + "…"
	if !strings.Contains(out, wantCell) {
		t.Errorf("expected truncated NAME cell %q; got:\n%s", wantCell, out)
	}
	// Mutation guard: the note must be present exactly once when truncation occurred.
	const note = "note: benchmark names truncated (use --output json for full names)"
	if n := strings.Count(out, note); n != 1 {
		t.Errorf("expected exactly one truncation note, got %d; out:\n%s", n, out)
	}

	// Alignment: on the header row and every data row, the rune two-space indent +
	// benchmarkNameColWidth is the column separator (a space). Unbounded rendering
	// of the long name would push a name character into that position.
	const indent = 2 // "  " row prefix
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") { // the "Benchmarks (N):" title is not a column row
			continue
		}
		if strings.HasPrefix(line, "  … and ") || strings.Contains(line, note) {
			continue // footers are not column rows
		}
		runes := []rune(line)
		sep := indent + benchmarkNameColWidth
		if len(runes) <= sep {
			t.Errorf("row too short to hold the NAME column; line:\n%q", line)
			continue
		}
		if runes[sep] != ' ' {
			t.Errorf("NAME column not %d wide (misaligned): rune at boundary is %q; line:\n%q",
				benchmarkNameColWidth, string(runes[sep]), line)
		}
	}
}

// TestBenchmarkTable_ShortNames_NoTruncation_NoNote asserts that when every name
// fits the NAME column, nothing is truncated and no note is printed. Kept under
// benchmarkTableLimit rows so the "… and N more" footer (also an ellipsis) never
// appears and the "no ellipsis" assertion is unambiguous.
func TestBenchmarkTable_ShortNames_NoTruncation_NoNote(t *testing.T) {
	benches := []bestiary.BenchmarkResult{
		{Name: "MMLU", Metric: "acc", Score: 87.5},
		{Name: "GPQA Diamond", Metric: "acc", Score: 50},
	}

	var sb strings.Builder
	writeBenchmarkTable(&sb, benches)
	out := sb.String()

	if strings.Contains(out, "…") {
		t.Errorf("short names should not truncate (no ellipsis); got:\n%s", out)
	}
	if strings.Contains(out, "note: benchmark names truncated") {
		t.Errorf("no truncation note expected when all names fit; got:\n%s", out)
	}
	for _, want := range []string{"MMLU", "GPQA Diamond"} {
		if !strings.Contains(out, want) {
			t.Errorf("short name %q should render in full; got:\n%s", want, out)
		}
	}
}

// TestBenchmarkTable_JSON_FullNames asserts the JSON output path (what
// `show --by-entity --output json` emits) is unaffected by table truncation: the
// full benchmark name survives and no truncation note/ellipsis leaks in.
func TestBenchmarkTable_JSON_FullNames(t *testing.T) {
	const longName = "Artificial Analysis Coding Index"
	ent := bestiary.Entity{
		Metadata: &bestiary.EntityMetadata{
			Benchmarks: []bestiary.BenchmarkResult{{Name: longName, Metric: "acc", Score: 42}},
		},
	}

	var sb strings.Builder
	if err := writeJSON(&sb, ent); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	out := sb.String()

	if !strings.Contains(out, longName) {
		t.Errorf("JSON output must carry the full benchmark name; got:\n%s", out)
	}
	if strings.Contains(out, "truncated") || strings.Contains(out, "…") {
		t.Errorf("JSON output must not carry table truncation artifacts; got:\n%s", out)
	}
}

// TestInstanceTable_StatusColumn asserts the instance table gains a STATUS column
// only when an instance carries a non-None status, and renders the status name.
func TestInstanceTable_StatusColumn(t *testing.T) {
	insts := []bestiary.ProviderInstance{
		{ID: "m1", Provider: "p1"},
		{ID: "m2", Provider: "p2"},
	}

	noStages := []bestiary.ReleaseStage{bestiary.StageNone, bestiary.StageNone}

	// All None → no STATUS column.
	var noStatus strings.Builder
	writeInstanceTableWithStatus(&noStatus, insts, []bestiary.ModelStatus{bestiary.StatusNone, bestiary.StatusNone}, noStages)
	if strings.Contains(noStatus.String(), "STATUS") {
		t.Errorf("STATUS column present when no instance carries a status:\n%s", noStatus.String())
	}

	// One Beta → STATUS column with the status name.
	var withStatus strings.Builder
	writeInstanceTableWithStatus(&withStatus, insts, []bestiary.ModelStatus{bestiary.StatusBeta, bestiary.StatusNone}, noStages)
	out := withStatus.String()
	if !strings.Contains(out, "STATUS") {
		t.Errorf("STATUS column missing when an instance carries a status:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("STATUS column should render the status name 'beta':\n%s", out)
	}
}

// TestInstanceTable_StageColumn asserts the instance table gains a STAGE column —
// distinct from STATUS — only when an instance carries a non-None release stage, and
// that the two columns are independent (stage present, status absent renders STAGE
// but not STATUS).
func TestInstanceTable_StageColumn(t *testing.T) {
	insts := []bestiary.ProviderInstance{
		{ID: "m1", Provider: "p1"},
		{ID: "m2", Provider: "p2"},
	}
	noStatuses := []bestiary.ModelStatus{bestiary.StatusNone, bestiary.StatusNone}

	// All None → no STAGE column.
	var noStage strings.Builder
	writeInstanceTableWithStatus(&noStage, insts, noStatuses, []bestiary.ReleaseStage{bestiary.StageNone, bestiary.StageNone})
	if strings.Contains(noStage.String(), "STAGE") {
		t.Errorf("STAGE column present when no instance carries a stage:\n%s", noStage.String())
	}

	// One Beta stage, no status → STAGE column present, STATUS column absent (independent columns).
	var withStage strings.Builder
	writeInstanceTableWithStatus(&withStage, insts, noStatuses, []bestiary.ReleaseStage{bestiary.StageBeta, bestiary.StageNone})
	out := withStage.String()
	if !strings.Contains(out, "STAGE") {
		t.Errorf("STAGE column missing when an instance carries a stage:\n%s", out)
	}
	if strings.Contains(out, "STATUS") {
		t.Errorf("STATUS column must be absent when no instance carries a status (columns are independent):\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("STAGE column should render the stage name 'beta':\n%s", out)
	}
}
