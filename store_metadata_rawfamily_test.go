package bestiary

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// oldEntityMetadataTableSQL is the pre-raw_family entity_metadata shape: an
// intermediate-v6 dev cache created before the raw_family provenance column was
// added. The self-heal must backfill the column on such a cache.
const oldEntityMetadataTableSQL = `CREATE TABLE IF NOT EXISTS entity_metadata (
    metadata_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    license     TEXT NOT NULL DEFAULT '',
    source_id   TEXT NOT NULL REFERENCES data_sources(data_source_id),
    last_synced TEXT NOT NULL DEFAULT ''
);`

// createV6OldEntityMetadataDB writes a v6-schema database whose entity_metadata
// table predates the raw_family column (schema_meta reads 6, but the version-gated
// migration never runs) — the exact intermediate-v6 cache the self-heal targets.
func createV6OldEntityMetadataDB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV6OldEntityMetadataDB: open %s: %v", path, err)
	}
	defer conn.Close()

	exec := func(what, sql string) {
		if err := sqlitex.ExecuteTransient(conn, sql, nil); err != nil {
			t.Fatalf("createV6OldEntityMetadataDB: %s: %v", what, err)
		}
	}
	exec("foreign_keys", `PRAGMA foreign_keys = ON;`)
	exec("schema_meta", schemaMetaSQL)
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{6}}); err != nil {
		t.Fatalf("createV6OldEntityMetadataDB: insert schema version: %v", err)
	}
	// A current-shape models table (isolates the entity_metadata heal from the models heal).
	exec("models", schemaSQL)
	// Provenance dimension + the FK target for entity_metadata.source_id.
	exec("data_sources", dataSourcesTableSQL)
	exec("entities", entitiesTableSQL)
	exec("dataset_ingested", datasetIngestedV6TableSQL)
	exec("entity_source", entitySourceTableSQL)
	// The OLD entity_metadata (no raw_family) + its child tables.
	exec("entity_metadata (old)", oldEntityMetadataTableSQL)
	exec("metadata_benchmarks", metadataBenchmarksTableSQL)
	exec("metadata_links", metadataLinksTableSQL)
}

// TestStoreV6_SelfHealsMissingEntityMetadataRawFamily is the entity_metadata twin of
// TestStoreV6_SelfHealsMissingModelColumns: an intermediate-v6 cache whose
// entity_metadata lacks raw_family. Because schema_meta already reads 6 the migration
// never runs, so only the OpenStore self-heal can add the column. Without it,
// UpsertEntityMetadata/QueryEntityMetadata would error with "no such column"; with it,
// the column is backfilled on open and the round-trip works while schema_meta stays 6.
func TestStoreV6_SelfHealsMissingEntityMetadataRawFamily(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6-old-entity-metadata.db")
	createV6OldEntityMetadataDB(t, dbPath)

	// Precondition: the fixture genuinely lacks raw_family.
	pre, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	preCols, err := tableColumnSet(pre, "entity_metadata")
	pre.Close()
	if err != nil {
		t.Fatalf("read fixture columns: %v", err)
	}
	if preCols["raw_family"] {
		t.Fatalf("fixture already has raw_family; the self-heal would be untested")
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (entity_metadata self-heal): %v", err)
	}
	defer store.Close()

	// Opening an intermediate-v6 cache now also applies the additive v6→v7 migration,
	// so schema_meta advances to currentSchemaVersion; the entity_metadata self-heal
	// still backfills raw_family alongside it.
	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d (v6 cache migrates to current on open)", v, currentSchemaVersion)
	}
	// Column backfilled.
	postCols, err := tableColumnSet(store.conn, "entity_metadata")
	if err != nil {
		t.Fatalf("read healed columns: %v", err)
	}
	if !postCols["raw_family"] {
		t.Fatalf("raw_family not backfilled by the OpenStore self-heal")
	}

	// And the round-trip works (including RawFamily) on the healed table.
	ctx := context.Background()
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
	}, nil); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	want := []EntityMetadata{testMetadataRow()}
	if err := store.UpsertEntityMetadata(ctx, want); err != nil {
		t.Fatalf("UpsertEntityMetadata on healed table: %v", err)
	}
	got, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata on healed table: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("healed round-trip mismatch:\n  got  = %+v\n  want = %+v", got, want)
	}
	if len(got) == 1 && got[0].RawFamily != "glm" {
		t.Errorf("healed round-trip RawFamily = %q, want %q", got[0].RawFamily, "glm")
	}
}

// TestEnsureEntityMetadataColumnsV6_Idempotent asserts the entity_metadata self-heal
// is a safe no-op on an already-complete table (a fresh v6 store): running it
// repeatedly neither errors nor changes the table shape.
func TestEnsureEntityMetadataColumnsV6_Idempotent(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	before, err := tableColumnSet(store.conn, "entity_metadata")
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	for i := range 3 {
		if err := ensureEntityMetadataColumnsV6(store.conn); err != nil {
			t.Fatalf("ensureEntityMetadataColumnsV6 (run %d): %v", i, err)
		}
	}
	after, err := tableColumnSet(store.conn, "entity_metadata")
	if err != nil {
		t.Fatalf("read columns: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("repeated ensureEntityMetadataColumnsV6 changed the entity_metadata shape:\n  before = %+v\n  after = %+v", before, after)
	}
}

// TestAfterSyncOverlay_CensusStaysPinned is the end-to-end guard for the disclosed
// after-sync gap: the three family-present-no-clean-entity rows
// (gemini-omni-flash-preview, gpt-realtime-2.1, laguna-xs-2.1), once round-tripped
// through the store as a SYNC would do, must NOT resurface as synthesized standalones
// in the CLI overlay join. Because a synced cached row wins the most-recent-wins merge
// over the baked row, the merged row's RawFamily is the STORE's — so the family-presence
// gate stays apples-to-apples only if raw_family persists. Before that column
// round-tripped, the cached rows lost RawFamily and the join re-synthesized three
// standalones; this pins the overlay census, which is empty at this tip (see the note
// on wantStandalone).
func TestAfterSyncOverlay_CensusStaysPinned(t *testing.T) {
	// 4 ornith rows -> empty with the 2026-08-28 models.dev catalog refresh. Premise
	// change, not a relaxed pin: ornith was a genuine catalog absence (zero serving
	// entries) and is now served eight times over (inferx "Ornith-1.0-35B-FP8", six
	// nano-gpt "ornith-ai/ornith-1.5-*" rows, runinfra "ornith-ai/Ornith-1.5-35B-A3B"),
	// so the presence gate correctly stops synthesizing for it. Same cause and same
	// measurement as the curated set in TestMetadataCensus_SynthesizedStandalonesPinned.
	//
	// The census this test exists to pin is the AFTER-SYNC one, and it stays fully in
	// force: the load-bearing assertion is that the three round-tripped rows below do NOT
	// resurface as standalones once raw_family has been through the store. An empty
	// expected set makes that assertion strictly stronger, not weaker.
	wantStandalone := map[string]bool{}
	unaliased := map[MetadataID]bool{
		"google/gemini-omni-flash-preview": true,
		"openai/gpt-realtime-2.1":          true,
		"poolside/laguna-xs-2.1":           true,
	}

	// The served entity set (the join's left input): every registry entity that has
	// at least one provider instance (i.e. excluding the folded-in standalones).
	var served []Entity
	for _, e := range Entities() {
		if len(e.Instances) > 0 {
			served = append(served, e)
		}
	}

	// Simulate a sync of the three unaliased rows: take their baked rows (which carry
	// RawFamily), stamp a NEWER LastSynced so the cached copy wins the merge, and
	// round-trip them through the store.
	var toSync []EntityMetadata
	for _, m := range staticEntityMetadata() {
		if unaliased[m.MetadataID] {
			m.LastSynced = "2099-01-01T00:00:00Z" // newer than the baked "" so cache wins
			toSync = append(toSync, m)
		}
	}
	if len(toSync) != 3 {
		t.Fatalf("expected 3 unaliased baked rows to sync, got %d", len(toSync))
	}

	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
	}, nil); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	if err := store.UpsertEntityMetadata(ctx, toSync); err != nil {
		t.Fatalf("UpsertEntityMetadata: %v", err)
	}
	cached, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata: %v", err)
	}

	// The exact CLI-overlay composition: merge baked + cached (cache wins on recency),
	// then run the join over the served entities.
	merged := MergeEntityMetadata(staticEntityMetadata(), cached)
	_, _, standalone := JoinEntityMetadata(served, merged)

	got := make(map[string]bool, len(standalone))
	for _, s := range standalone {
		got[s.Ref.String()] = true
	}
	for k := range got {
		if !wantStandalone[k] {
			t.Errorf("after-sync overlay synthesized an UNEXPECTED standalone %q\n"+
				"  What: a synced/cached metadata row re-synthesized a metadata-only entity for a served family\n"+
				"  Why: the store round-trip likely dropped RawFamily, so the presence gate fell back to the\n"+
				"       over-captured mechanical family (the exact after-sync gap raw_family persistence closes)\n"+
				"  How to fix: ensure UpsertEntityMetadata/QueryEntityMetadata persist and read raw_family", k)
		}
	}
	for k := range wantStandalone {
		if !got[k] {
			t.Errorf("after-sync overlay is MISSING expected standalone %q (ornith is a genuine catalog absence)", k)
		}
	}
}
