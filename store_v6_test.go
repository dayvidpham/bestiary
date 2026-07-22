package bestiary

// v6 store tests live in the internal test package so they can reach the
// unexported conn field, the schema constants, and the same-package helpers
// (tableExists, getSchemaVersion, countRows, createV4DB, the v4/v5 DDL). They
// cover the v6 append-only ingest log (composite-PK dataset_ingested) and the
// three entity-metadata tables: the chained v4→v5→v6 and v5→v6 migrations, the
// fresh-DB v6 shape matching the migrated shape, re-open idempotency, INSERT OR
// IGNORE append + discriminator semantics, UpsertEntityMetadata replace-set
// round-trips, and the entity-metadata FK guards.

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// metadataTables is the set of v6 entity-metadata tables every v6 database carries
// in addition to the four v5 BCNF provenance tables.
var metadataTables = []string{"entity_metadata", "metadata_benchmarks", "metadata_links"}

// colSig is a structural signature of one table column: name, declared type, the
// NOT NULL flag, and the 1-based position in the primary key (0 when not part of
// it). Comparing the ordered colSig slice of two tables detects a primary-key or
// column-shape divergence without depending on the exact CREATE TABLE text.
type colSig struct {
	name    string
	ctype   string
	notNull int
	pk      int
}

// tableSignature reads the ordered column signatures of table via
// pragma_table_info. It is the robust structural comparison behind the fresh-vs-
// migrated shape assertion (the stored CREATE TABLE text differs — IF NOT EXISTS,
// the rename from dataset_ingested_new — so the raw SQL cannot be compared).
func tableSignature(t *testing.T, conn *sqlite.Conn, table string) []colSig {
	t.Helper()
	var sigs []colSig
	err := sqlitex.Execute(conn,
		`SELECT name, type AS ctype, "notnull" AS nn, pk FROM pragma_table_info(?1) ORDER BY cid`,
		&sqlitex.ExecOptions{
			Args: []any{table},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				sigs = append(sigs, colSig{
					name:    stmt.GetText("name"),
					ctype:   stmt.GetText("ctype"),
					notNull: int(stmt.GetInt64("nn")),
					pk:      int(stmt.GetInt64("pk")),
				})
				return nil
			},
		})
	if err != nil {
		t.Fatalf("tableSignature(%s): %v", table, err)
	}
	return sigs
}

// createV5DB writes a v5-schema SQLite database to path: schema_meta (version=5),
// the v4 models table + index, the four v5 BCNF provenance tables (with the
// SINGLE-PK dataset_ingested), and a seeded data source plus one v5 current-ingest
// row. It uses the production DDL constants so the fixture is a faithful v5
// database; OpenStore's v5→v6 migration is what must widen dataset_ingested and add
// the metadata tables while preserving the seeded ingest row.
func createV5DB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV5DB: open %s: %v", path, err)
	}
	defer conn.Close()

	// Enforce FKs so the seeded ingest row's data_source_id FK is genuinely checked.
	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		t.Fatalf("createV5DB: enable foreign_keys: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV5DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{5}}); err != nil {
		t.Fatalf("createV5DB: insert schema version: %v", err)
	}
	// Models table (v4 == v5 models shape) so the migrated DB has a models table.
	if err := sqlitex.ExecuteTransient(conn, v4Schema, nil); err != nil {
		t.Fatalf("createV5DB: create models table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4IndexSQL, nil); err != nil {
		t.Fatalf("createV5DB: create v4 index: %v", err)
	}
	// The four v5 BCNF tables, with the SINGLE-PK dataset_ingested.
	for _, sql := range []string{dataSourcesTableSQL, entitiesTableSQL, datasetIngestedTableSQL, entitySourceTableSQL} {
		if err := sqlitex.ExecuteTransient(conn, sql, nil); err != nil {
			t.Fatalf("createV5DB: create BCNF table: %v", err)
		}
	}
	// Seed a data source and its single v5 current-ingest row.
	if err := sqlitex.Execute(conn,
		`INSERT INTO data_sources (data_source_id, uri, canonical_name) VALUES ('models.dev', 'https://models.dev/api.json', 'models.dev')`, nil); err != nil {
		t.Fatalf("createV5DB: seed data_sources: %v", err)
	}
	if err := sqlitex.Execute(conn,
		`INSERT INTO dataset_ingested (data_source_id, ingested_at, parser_schema) VALUES ('models.dev', '2026-06-09T00:00:00Z', 2)`, nil); err != nil {
		t.Fatalf("createV5DB: seed dataset_ingested: %v", err)
	}
}

// assertV6Tables fails unless all four v5 BCNF tables and all three v6 metadata
// tables exist on conn.
func assertV6Tables(t *testing.T, conn *sqlite.Conn) {
	t.Helper()
	for _, tbl := range append(append([]string{}, provenanceTables...), metadataTables...) {
		exists, err := tableExists(conn, tbl)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", tbl, err)
		}
		if !exists {
			t.Errorf("v6 database is missing table %q", tbl)
		}
	}
}

// TestStoreMigrate_V5toV6 builds a faithful v5 database with a seeded current-ingest
// row, opens it, and asserts the v5→v6 migration (1) reaches currentSchemaVersion,
// (2) PRESERVES the seeded ingest row through the dataset_ingested table-recreate,
// (3) creates the three metadata tables, and (4) leaves dataset_ingested with the
// composite primary key (a second ingest at a new timestamp now APPENDS).
func TestStoreMigrate_V5toV6(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v5.db")
	createV5DB(t, dbPath)

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v5→v6 migration): %v", err)
	}
	defer store.Close()

	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("post-migration version = %d, want %d", version, currentSchemaVersion)
	}
	if currentSchemaVersion != 7 {
		t.Errorf("currentSchemaVersion = %d, want 7", currentSchemaVersion)
	}

	assertV6Tables(t, store.conn)

	// The seeded v5 ingest row must survive the recreate, values intact.
	ctx := context.Background()
	hist, err := store.QueryIngestHistory(ctx, DataSourceModelsDev)
	if err != nil {
		t.Fatalf("QueryIngestHistory: %v", err)
	}
	if len(hist) != 1 {
		t.Fatalf("preserved ingest rows = %d, want 1", len(hist))
	}
	if hist[0].IngestedAt != "2026-06-09T00:00:00Z" || hist[0].ParserSchema != 2 {
		t.Errorf("preserved ingest row = %+v, want ingested_at 2026-06-09T00:00:00Z parser_schema 2", hist[0])
	}

	// Composite PK now in effect: a second ingest at a NEW timestamp appends.
	if err := store.UpsertDataSources(ctx, nil, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-07-01T00:00:00Z", ParserSchema: 3},
	}); err != nil {
		t.Fatalf("append second ingest: %v", err)
	}
	if got := countRows(t, store.conn, "dataset_ingested"); got != 2 {
		t.Errorf("dataset_ingested row count = %d, want 2 (append-only history)", got)
	}
}

// TestStoreMigrate_V4toV5toV6_Chained builds a v4 database with model rows and
// asserts the full chained migration reaches v6: model rows preserved, all seven
// provenance+metadata tables present, and dataset_ingested carrying the composite
// primary key (append works, exact duplicate ignored).
func TestStoreMigrate_V4toV5toV6_Chained(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v4.db")
	createV4DB(t, dbPath, []struct {
		modelID, provider, rawFamily, family, variant, version string
	}{
		{"claude-opus-4-5-20251101", "anthropic", "claude-opus-4-5", "claude", "opus", "4.5"},
		{"gemini-2-0-flash", "google", "gemini-2-0-flash", "gemini", "flash", "2.0"},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v4→v5→v6 migration): %v", err)
	}
	defer store.Close()

	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("chained-migration version = %d, want %d", version, currentSchemaVersion)
	}

	// Model rows preserved through both migration steps.
	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels after chained migration: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 model rows preserved, got %d", len(all))
	}

	assertV6Tables(t, store.conn)

	// Composite PK: register the source, then append + exact-duplicate.
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
	}, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 3},
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 3}, // exact dup, ignored
	}); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}
	if got := countRows(t, store.conn, "dataset_ingested"); got != 1 {
		t.Errorf("after append+dup, dataset_ingested row count = %d, want 1 (composite PK dedup)", got)
	}
}

// TestStoreV6_FreshShapeMatchesMigrated pins the v0.2.4 fresh-arm lesson: a fresh v6
// database must have the SAME dataset_ingested and entity-metadata table shapes as a
// database migrated up from v4. It compares the structural signature (columns +
// primary-key positions) of each v6 table between a fresh in-memory store and a
// migrated on-disk store, so a fresh arm that forgot the composite PK or a metadata
// table is caught.
func TestStoreV6_FreshShapeMatchesMigrated(t *testing.T) {
	fresh, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:) fresh: %v", err)
	}
	defer fresh.Close()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v4.db")
	createV4DB(t, dbPath, nil)
	migrated, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore migrated: %v", err)
	}
	defer migrated.Close()

	for _, tbl := range append([]string{"dataset_ingested"}, metadataTables...) {
		freshSig := tableSignature(t, fresh.conn, tbl)
		migSig := tableSignature(t, migrated.conn, tbl)
		if !reflect.DeepEqual(freshSig, migSig) {
			t.Errorf("table %q shape differs between fresh and migrated v6:\n  fresh    = %+v\n  migrated = %+v",
				tbl, freshSig, migSig)
		}
	}

	// Sanity: dataset_ingested must have a TWO-column composite primary key.
	var pkCols int
	for _, c := range tableSignature(t, fresh.conn, "dataset_ingested") {
		if c.pk > 0 {
			pkCols++
		}
	}
	if pkCols != 2 {
		t.Errorf("fresh dataset_ingested primary-key columns = %d, want 2 (composite (data_source_id, ingested_at))", pkCols)
	}
}

// TestStoreV6_ReopenIdempotent asserts a v6 database can be closed and reopened
// without error or version drift, and that seeded rows survive the reopen. The
// second OpenStore reads schema_meta=6 and applies no migration.
func TestStoreV6_ReopenIdempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v6.db")

	ctx := context.Background()
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (fresh v6): %v", err)
	}
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
	}, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-09T00:00:00Z", ParserSchema: 3},
	}); err != nil {
		t.Fatalf("seed UpsertDataSources: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (reopen v6): %v", err)
	}
	defer reopened.Close()

	version, err := getSchemaVersion(reopened.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion after reopen: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("reopened version = %d, want %d", version, currentSchemaVersion)
	}
	assertV6Tables(t, reopened.conn)
	hist, err := reopened.QueryIngestHistory(ctx, DataSourceModelsDev)
	if err != nil {
		t.Fatalf("QueryIngestHistory after reopen: %v", err)
	}
	if len(hist) != 1 || hist[0].IngestedAt != "2026-06-09T00:00:00Z" {
		t.Errorf("seeded ingest row not preserved across reopen: %+v", hist)
	}
}

// TestUpsertDataSources_AppendOnlyHistory pins the two-syncs semantics: ingesting a
// source at t1 then t2 (t1 != t2) yields TWO rows, QueryCurrentIngests reports the
// MAX (t2) with the t2 row's parser_schema, and QueryIngestHistory returns both rows
// ascending.
func TestUpsertDataSources_AppendOnlyHistory(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	src := []DataSource{{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}}

	if err := store.UpsertDataSources(ctx, src, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 2},
	}); err != nil {
		t.Fatalf("UpsertDataSources (t1): %v", err)
	}
	if err := store.UpsertDataSources(ctx, src, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-09T00:00:00Z", ParserSchema: 3},
	}); err != nil {
		t.Fatalf("UpsertDataSources (t2): %v", err)
	}

	if got := countRows(t, store.conn, "dataset_ingested"); got != 2 {
		t.Errorf("dataset_ingested row count = %d, want 2 (append-only, not replace)", got)
	}

	// Current = MAX(ingested_at) = t2, carrying the t2 row's parser_schema.
	current, err := store.QueryCurrentIngests(ctx)
	if err != nil {
		t.Fatalf("QueryCurrentIngests: %v", err)
	}
	if len(current) != 1 {
		t.Fatalf("QueryCurrentIngests returned %d rows, want 1 (one source)", len(current))
	}
	if current[0].SourceID != DataSourceModelsDev ||
		current[0].IngestedAt != "2026-06-09T00:00:00Z" ||
		current[0].ParserSchema != 3 {
		t.Errorf("current ingest = %+v, want {models.dev 2026-06-09T00:00:00Z 3}", current[0])
	}

	// History = both rows ascending.
	hist, err := store.QueryIngestHistory(ctx, DataSourceModelsDev)
	if err != nil {
		t.Fatalf("QueryIngestHistory: %v", err)
	}
	wantTS := []string{"2026-06-01T00:00:00Z", "2026-06-09T00:00:00Z"}
	if len(hist) != 2 {
		t.Fatalf("history length = %d, want 2", len(hist))
	}
	for i, w := range wantTS {
		if hist[i].IngestedAt != w {
			t.Errorf("history[%d].IngestedAt = %q, want %q (ascending)", i, hist[i].IngestedAt, w)
		}
	}
}

// TestUpsertDataSources_IngestDiscriminator is the OR-IGNORE-vs-OR-REPLACE falsifier:
// re-ingesting the SAME (source, ingested_at) with a DIFFERENT parser_schema must
// leave the ORIGINAL row unchanged. Under an OR REPLACE regression the parser_schema
// would flip to the new value; under append-only OR IGNORE the original is retained.
func TestUpsertDataSources_IngestDiscriminator(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	src := []DataSource{{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}}

	if err := store.UpsertDataSources(ctx, src, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-09T00:00:00Z", ParserSchema: 3},
	}); err != nil {
		t.Fatalf("UpsertDataSources (original): %v", err)
	}
	// Re-ingest the SAME (source, ingested_at) with a DIFFERENT parser_schema.
	if err := store.UpsertDataSources(ctx, src, []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-09T00:00:00Z", ParserSchema: 99},
	}); err != nil {
		t.Fatalf("UpsertDataSources (re-ingest): %v", err)
	}

	if got := countRows(t, store.conn, "dataset_ingested"); got != 1 {
		t.Errorf("dataset_ingested row count = %d, want 1 (exact-duplicate composite key ignored)", got)
	}
	hist, err := store.QueryIngestHistory(ctx, DataSourceModelsDev)
	if err != nil {
		t.Fatalf("QueryIngestHistory: %v", err)
	}
	if len(hist) != 1 || hist[0].ParserSchema != 3 {
		t.Errorf("after re-ingest, row = %+v, want original parser_schema 3 (OR IGNORE retains original; an OR REPLACE regression would show 99)", hist)
	}
}

// testMetadataRow builds an EntityMetadata with two benchmarks (one with a string
// score) and two links (one with an unrecognized type carrying a raw token) so a
// round-trip exercises every child column, both enum encodings, and slice ordering.
func testMetadataRow() EntityMetadata {
	return EntityMetadata{
		MetadataID:  "zhipuai/glm-4.6",
		Name:        "GLM 4.6",
		Description: "A capable open-weights model.",
		License:     "MIT",
		Links: []ModelLink{
			{Label: "Model card", URL: "https://example.test/card", Type: LinkModelCard},
			{Label: "Custom", URL: "https://example.test/other", Type: LinkOther, TypeRaw: "press-release"},
		},
		Benchmarks: []BenchmarkResult{
			{Name: "MMLU", Metric: "accuracy", Score: 0.873, Harness: "lm-eval", Date: "2026-05-01"},
			{Name: "GPQA", Metric: "pass@1", ScoreRaw: "N/A", Variant: "diamond", SourceURL: "https://example.test/blog"},
		},
		Source:     DataSourceModelsDev,
		LastSynced: "2026-06-09T00:00:00Z",
		// RawFamily is the upstream family provenance; persisting it is what keeps the
		// after-sync join's family-presence gate working. A non-empty value here makes
		// TestUpsertEntityMetadata_RoundTrip's DeepEqual cover the raw_family column
		// (it was "" -> mismatch before the column round-tripped).
		RawFamily: "glm",
	}
}

// TestUpsertEntityMetadata_RoundTrip pins content equality through the store: an
// EntityMetadata with benchmarks and links written by UpsertEntityMetadata reads
// back identically via QueryEntityMetadata (parents ascending, children in insertion
// order, enum columns decoded). Re-syncing the same row leaves the child row counts
// unchanged (the replace-set never duplicates children).
func TestUpsertEntityMetadata_RoundTrip(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	// entity_metadata.source_id FK requires the data source to exist first.
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
	}, nil); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}

	rows := []EntityMetadata{testMetadataRow()}
	if err := store.UpsertEntityMetadata(ctx, rows); err != nil {
		t.Fatalf("UpsertEntityMetadata: %v", err)
	}

	got, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata: %v", err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Errorf("round-trip mismatch:\n  got  = %+v\n  want = %+v", got, rows)
	}

	// Re-sync the IDENTICAL metadata: child counts must be unchanged (replace-set,
	// not append).
	benchBefore := countRows(t, store.conn, "metadata_benchmarks")
	linkBefore := countRows(t, store.conn, "metadata_links")
	if err := store.UpsertEntityMetadata(ctx, rows); err != nil {
		t.Fatalf("UpsertEntityMetadata (re-sync): %v", err)
	}
	if got := countRows(t, store.conn, "metadata_benchmarks"); got != benchBefore {
		t.Errorf("after re-sync, metadata_benchmarks = %d, want %d (no duplication)", got, benchBefore)
	}
	if got := countRows(t, store.conn, "metadata_links"); got != linkBefore {
		t.Errorf("after re-sync, metadata_links = %d, want %d (no duplication)", got, linkBefore)
	}
	if got := countRows(t, store.conn, "entity_metadata"); got != 1 {
		t.Errorf("after re-sync, entity_metadata = %d, want 1 (parent replaced, not duplicated)", got)
	}

	// And the content is still identical after the re-sync.
	got2, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata (post re-sync): %v", err)
	}
	if !reflect.DeepEqual(got2, rows) {
		t.Errorf("post re-sync mismatch:\n  got  = %+v\n  want = %+v", got2, rows)
	}
}

// TestUpsertEntityMetadata_ReplaceSetShrinks proves the child set is genuinely
// REPLACED, not merged: writing a row with two benchmarks then re-writing it with
// one benchmark leaves exactly one benchmark row (the delete-then-insert removed the
// stale child).
func TestUpsertEntityMetadata_ReplaceSetShrinks(t *testing.T) {
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

	full := testMetadataRow()
	if err := store.UpsertEntityMetadata(ctx, []EntityMetadata{full}); err != nil {
		t.Fatalf("UpsertEntityMetadata (full): %v", err)
	}

	shrunk := testMetadataRow()
	shrunk.Benchmarks = shrunk.Benchmarks[:1] // keep one
	shrunk.Links = nil                        // drop all links
	if err := store.UpsertEntityMetadata(ctx, []EntityMetadata{shrunk}); err != nil {
		t.Fatalf("UpsertEntityMetadata (shrunk): %v", err)
	}

	if got := countRows(t, store.conn, "metadata_benchmarks"); got != 1 {
		t.Errorf("metadata_benchmarks = %d, want 1 (stale child removed by replace-set)", got)
	}
	if got := countRows(t, store.conn, "metadata_links"); got != 0 {
		t.Errorf("metadata_links = %d, want 0 (all links dropped by replace-set)", got)
	}
	got, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata: %v", err)
	}
	if !reflect.DeepEqual(got, []EntityMetadata{shrunk}) {
		t.Errorf("shrunk round-trip mismatch:\n  got  = %+v\n  want = %+v", got, []EntityMetadata{shrunk})
	}
}

// TestUpsertEntityMetadata_OrphanChildRejected is the FK positive control for the
// metadata tables: it first proves a properly parented child inserts (FK is enabled
// and not over-restrictive), then proves an orphan benchmark and an orphan link (a
// metadata_id with no entity_metadata parent) are each REJECTED. Without the parent
// insert succeeding, the rejection could be a false positive from an unrelated error.
func TestUpsertEntityMetadata_OrphanChildRejected(t *testing.T) {
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
	// A real parent for the positive control.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO entity_metadata (metadata_id, source_id) VALUES ('real/parent', 'models.dev')`, nil); err != nil {
		t.Fatalf("seed entity_metadata parent: %v", err)
	}

	// Positive control: a parented benchmark inserts.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO metadata_benchmarks (metadata_id, name) VALUES ('real/parent', 'MMLU')`, nil); err != nil {
		t.Fatalf("parented benchmark insert was rejected (FK over-restrictive or misconfigured): %v", err)
	}

	// Orphan benchmark: unknown metadata_id → FK reject.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO metadata_benchmarks (metadata_id, name) VALUES ('ghost/model', 'MMLU')`, nil); err == nil {
		t.Error("orphan metadata_benchmarks insert was ACCEPTED; the metadata_id FK is not enforced")
	}
	// Orphan link: unknown metadata_id → FK reject.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO metadata_links (metadata_id, url) VALUES ('ghost/model', 'https://example.test')`, nil); err == nil {
		t.Error("orphan metadata_links insert was ACCEPTED; the metadata_id FK is not enforced")
	}
}

// TestUpsertEntityMetadata_OrphanParentSourceRejected proves the entity_metadata
// source_id FK bites through the public writer: attaching metadata whose Source names
// a data source that was never registered is rejected.
func TestUpsertEntityMetadata_OrphanParentSourceRejected(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	err = store.UpsertEntityMetadata(ctx, []EntityMetadata{
		{MetadataID: "zhipuai/glm-4.6", Source: "never-registered"},
	})
	if err == nil {
		t.Error("UpsertEntityMetadata to an unregistered source was accepted; entity_metadata.source_id FK not enforced")
	}
}
