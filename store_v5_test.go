package bestiary

// v5 store tests live in the internal test package so they can reach the
// unexported conn field for raw-SQL FK falsification and read-back, plus the
// unexported helpers (tableExists, getSchemaVersion). They cover the v5 BCNF
// data-source provenance schema: the foreign_keys pragma actually biting, the
// additive v4→v5 migration, and the UpsertDataSources / UpsertEntitySources
// round-trips with their parents-before-children FK ordering.

import (
	"context"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// provenanceTables is the set of v5 BCNF tables every v5 database must carry.
var provenanceTables = []string{"data_sources", "dataset_ingested", "entities", "entity_source"}

// countRows returns the number of rows in table on conn.
func countRows(t *testing.T, conn *sqlite.Conn, table string) int {
	t.Helper()
	var n int
	err := sqlitex.Execute(conn, "SELECT COUNT(*) AS c FROM "+table, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			n = int(stmt.GetInt64("c"))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("countRows(%s): %v", table, err)
	}
	return n
}

// TestStoreV5_ForeignKeysEnforced (VC-NORM2) verifies, on a FRESH in-memory v5
// database, that (1) all four BCNF tables exist on the fresh-DB path, (2) the
// foreign_keys pragma actually bites — inserting an entity_source row whose
// data_source_id has no data_sources parent is REJECTED, and (3) a fully
// parented attestation inserts. The rejection arm is the guard that the pragma
// is set; without it SQLite silently accepts the orphan.
func TestStoreV5_ForeignKeysEnforced(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	// (1) Fresh-DB path must have created the four provenance tables.
	for _, tbl := range provenanceTables {
		exists, err := tableExists(store.conn, tbl)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", tbl, err)
		}
		if !exists {
			t.Errorf("fresh v5 DB is missing BCNF table %q; the fresh-DB migration path must create it", tbl)
		}
	}

	// (2) Orphan attestation: a valid parent entity exists, but the referenced
	// data source does not — the data_source_id FK must reject it. Insert the
	// entity parent first so ONLY the data_source FK can fire.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO entities (entity_key) VALUES ('llama@3.3#70b{instruct}')`, nil); err != nil {
		t.Fatalf("seed entities row: %v", err)
	}
	err = sqlitex.Execute(store.conn,
		`INSERT INTO entity_source (entity_key, data_source_id) VALUES ('llama@3.3#70b{instruct}', 'no-such-source')`, nil)
	if err == nil {
		t.Error(
			"orphan entity_source insert was ACCEPTED; foreign keys are not enforced.\n" +
				"  What: an entity_source row referencing a non-existent data_source_id was allowed\n" +
				"  Why: PRAGMA foreign_keys=ON is not set on the connection (or the FK clause is missing)\n" +
				"  Where: store.go OpenStore (pragma) / entitySourceTableSQL (FK clause)\n" +
				"  How to fix: set PRAGMA foreign_keys=ON before migrations and keep the entity_source FKs",
		)
	}

	// (3) A fully parented attestation must insert: register the source, then attest.
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO data_sources (data_source_id, uri, canonical_name)
		 VALUES ('ollama', 'https://ollama.com', 'Ollama')`, nil); err != nil {
		t.Fatalf("seed data_sources row: %v", err)
	}
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO entity_source (entity_key, data_source_id)
		 VALUES ('llama@3.3#70b{instruct}', 'ollama')`, nil); err != nil {
		t.Fatalf("valid entity_source insert was rejected: %v", err)
	}
	if got := countRows(t, store.conn, "entity_source"); got != 1 {
		t.Errorf("entity_source row count = %d, want 1 (the valid attestation)", got)
	}
}

// v4Schema is the v4 models schema: v3 columns plus the version column. Used to
// build a v4 database on disk that OpenStore must migrate additively to v5.
const v4Schema = `CREATE TABLE models (
    model_id          TEXT NOT NULL,
    provider          TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    raw_family        TEXT NOT NULL DEFAULT '',
    family            TEXT NOT NULL DEFAULT '',
    variant           TEXT NOT NULL DEFAULT '',
    version           TEXT NOT NULL DEFAULT '',
    date              TEXT NOT NULL DEFAULT '',
    context_window    INTEGER NOT NULL DEFAULT 0,
    max_output        INTEGER NOT NULL DEFAULT 0,
    reasoning         INTEGER NOT NULL DEFAULT 0,
    tool_call         INTEGER NOT NULL DEFAULT 0,
    attachment        INTEGER NOT NULL DEFAULT 0,
    temperature       INTEGER NOT NULL DEFAULT 0,
    structured_output INTEGER NOT NULL DEFAULT 0,
    interleaved       INTEGER NOT NULL DEFAULT 0,
    interleaved_config TEXT NOT NULL DEFAULT '',
    open_weights      INTEGER NOT NULL DEFAULT 0,
    cost_input        REAL,
    cost_output       REAL,
    cost_reasoning    REAL,
    cost_cache_read   REAL,
    cost_cache_write  REAL,
    release_date      TEXT NOT NULL DEFAULT '',
    knowledge         TEXT NOT NULL DEFAULT '',
    modalities_input  TEXT NOT NULL DEFAULT '',
    modalities_output TEXT NOT NULL DEFAULT '',
    last_synced       TEXT NOT NULL,
    PRIMARY KEY (model_id, provider)
)`

// v4IndexSQL is the v4 canonical index (family, variant, version, provider).
const v4IndexSQL = `CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, version, provider)`

// createV4DB writes a v4-schema SQLite database to path with schema_meta
// (version=4) and the given model rows. It deliberately creates NONE of the v5
// provenance tables, so OpenStore's v4→v5 migration is what must add them.
func createV4DB(t *testing.T, path string, rows []struct {
	modelID, provider, rawFamily, family, variant, version string
}) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV4DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV4DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{4}}); err != nil {
		t.Fatalf("createV4DB: insert schema version: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4Schema, nil); err != nil {
		t.Fatalf("createV4DB: create table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4IndexSQL, nil); err != nil {
		t.Fatalf("createV4DB: create v4 index: %v", err)
	}
	for _, r := range rows {
		err := sqlitex.Execute(conn,
			`INSERT INTO models (model_id, provider, display_name, raw_family, family, variant, version, last_synced)
            VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, '2026-01-01T00:00:00Z')`,
			&sqlitex.ExecOptions{Args: []any{
				r.modelID, r.provider, r.modelID + "-display",
				r.rawFamily, r.family, r.variant, r.version,
			}})
		if err != nil {
			t.Fatalf("createV4DB: insert row (%s, %s): %v", r.modelID, r.provider, err)
		}
	}
}

// TestStoreMigrate_V4toV5 (VC-migration) builds a v4 database with model rows,
// opens it with OpenStore, and asserts the migration is additive: the schema
// version becomes 5, the model rows are intact, and the four BCNF provenance
// tables now exist.
func TestStoreMigrate_V4toV5(t *testing.T) {
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
		t.Fatalf("OpenStore (v4→v5 migration): %v", err)
	}
	defer store.Close()

	// Schema version must be currentSchemaVersion (5) after migration.
	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("post-migration version = %d, want %d", version, currentSchemaVersion)
	}
	if currentSchemaVersion != 5 {
		t.Errorf("currentSchemaVersion = %d, want 5", currentSchemaVersion)
	}

	// Model rows must be intact (additive migration touches only new tables).
	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels after v4→v5: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 model rows preserved, got %d", len(all))
	}
	byID := make(map[ModelID]ModelInfo, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}
	if m, ok := byID["claude-opus-4-5-20251101"]; ok {
		if m.Family != "claude" || m.Variant != "opus" || m.Version != "4.5" {
			t.Errorf("claude row mangled by migration: family=%q variant=%q version=%q",
				m.Family, m.Variant, m.Version)
		}
	} else {
		t.Error("claude-opus-4-5-20251101 not found after v4→v5 migration")
	}

	// The four BCNF tables must now exist.
	for _, tbl := range provenanceTables {
		exists, err := tableExists(store.conn, tbl)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", tbl, err)
		}
		if !exists {
			t.Errorf("v4→v5 migration did not create BCNF table %q", tbl)
		}
	}
}

// TestUpsertDataSources_RoundTrip inserts data-source dimension rows and their
// current-ingest rows through the public API, then reads them back via the
// connection to pin insert-or-replace round-trip semantics. The DatasetIngested
// referencing a source in the SAME call proves the parents-before-children order
// (data_sources written before dataset_ingested).
func TestUpsertDataSources_RoundTrip(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sources := []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
		{ID: DataSourceOllama, URI: "https://ollama.com", CanonicalName: "Ollama"},
	}
	ingested := []DatasetIngested{
		{SourceID: DataSourceModelsDev, IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 2},
		{SourceID: DataSourceOllama, IngestedAt: "2026-06-02T00:00:00Z", ParserSchema: 2},
	}
	if err := store.UpsertDataSources(ctx, sources, ingested); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}

	if got := countRows(t, store.conn, "data_sources"); got != 2 {
		t.Errorf("data_sources row count = %d, want 2", got)
	}
	if got := countRows(t, store.conn, "dataset_ingested"); got != 2 {
		t.Errorf("dataset_ingested row count = %d, want 2", got)
	}

	// Read back a dimension row and its ingest fact (uri reached via the FK join).
	var uri, ingestedAt string
	var parserSchema int
	err = sqlitex.Execute(store.conn,
		`SELECT ds.uri AS uri, di.ingested_at AS ingested_at, di.parser_schema AS parser_schema
		 FROM dataset_ingested di JOIN data_sources ds ON ds.data_source_id = di.data_source_id
		 WHERE di.data_source_id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{string(DataSourceOllama)},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				uri = stmt.GetText("uri")
				ingestedAt = stmt.GetText("ingested_at")
				parserSchema = int(stmt.GetInt64("parser_schema"))
				return nil
			},
		})
	if err != nil {
		t.Fatalf("join read-back: %v", err)
	}
	if uri != "https://ollama.com" {
		t.Errorf("joined uri = %q, want %q", uri, "https://ollama.com")
	}
	if ingestedAt != "2026-06-02T00:00:00Z" {
		t.Errorf("ingested_at = %q, want %q", ingestedAt, "2026-06-02T00:00:00Z")
	}
	if parserSchema != 2 {
		t.Errorf("parser_schema = %d, want 2", parserSchema)
	}

	// Insert-or-replace: re-upserting the same id with a new canonical_name must
	// overwrite the existing row, not add a second.
	sources[1].CanonicalName = "Ollama Registry"
	if err := store.UpsertDataSources(ctx, sources[1:], nil); err != nil {
		t.Fatalf("UpsertDataSources (replace): %v", err)
	}
	if got := countRows(t, store.conn, "data_sources"); got != 2 {
		t.Errorf("after replace, data_sources row count = %d, want 2 (replace, not append)", got)
	}
	var name string
	err = sqlitex.Execute(store.conn,
		`SELECT canonical_name AS n FROM data_sources WHERE data_source_id = ?1`,
		&sqlitex.ExecOptions{
			Args: []any{string(DataSourceOllama)},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				name = stmt.GetText("n")
				return nil
			},
		})
	if err != nil {
		t.Fatalf("read canonical_name: %v", err)
	}
	if name != "Ollama Registry" {
		t.Errorf("canonical_name after replace = %q, want %q", name, "Ollama Registry")
	}
}

// TestUpsertDataSources_DuplicateURIRejected pins the UNIQUE(uri) candidate key:
// two distinct source ids that share a uri must be rejected.
func TestUpsertDataSources_DuplicateURIRejected(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	sources := []DataSource{
		{ID: "src-a", URI: "https://same.example", CanonicalName: "A"},
		{ID: "src-b", URI: "https://same.example", CanonicalName: "B"},
	}
	if err := store.UpsertDataSources(ctx, sources, nil); err == nil {
		t.Error("duplicate-uri UpsertDataSources was accepted; UNIQUE(uri) candidate key not enforced")
	}
}

// TestUpsertDataSources_IngestOrphanRejected proves the FK ordering bites across
// the dataset_ingested → data_sources edge: an ingest naming a source absent from
// both the call and the store is rejected.
func TestUpsertDataSources_IngestOrphanRejected(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	ingested := []DatasetIngested{
		{SourceID: "ghost-source", IngestedAt: "2026-06-01T00:00:00Z", ParserSchema: 2},
	}
	if err := store.UpsertDataSources(ctx, nil, ingested); err == nil {
		t.Error("dataset_ingested orphan was accepted; data_source_id FK not enforced")
	}
}

// TestUpsertEntitySources_RoundTrip writes attestations through the public API
// (after registering their source) and reads them back. It proves the two-pass
// parents-before-children order: the minimal entities row is created in pass 1 so
// the entity_source.entity_key FK resolves in pass 2. A dual-attested entity
// yields two join rows.
func TestUpsertEntitySources_RoundTrip(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// data_sources must exist first (entity_source.data_source_id FK).
	if err := store.UpsertDataSources(ctx, []DataSource{
		{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"},
		{ID: DataSourceOllama, URI: "https://ollama.com", CanonicalName: "Ollama"},
	}, nil); err != nil {
		t.Fatalf("UpsertDataSources: %v", err)
	}

	const key = "llama@3.3#70b{instruct}"
	attestations := []EntitySource{
		{EntityKey: key, SourceID: DataSourceModelsDev},
		{EntityKey: key, SourceID: DataSourceOllama},
		{EntityKey: "qwen@2.5#0.5b", SourceID: DataSourceModelsDev},
	}
	if err := store.UpsertEntitySources(ctx, attestations); err != nil {
		t.Fatalf("UpsertEntitySources: %v", err)
	}

	// Pass 1 must have created the minimal entities rows (2 distinct keys).
	if got := countRows(t, store.conn, "entities"); got != 2 {
		t.Errorf("entities row count = %d, want 2 (distinct keys)", got)
	}
	if got := countRows(t, store.conn, "entity_source"); got != 3 {
		t.Errorf("entity_source row count = %d, want 3", got)
	}

	// The dual-attested entity has two sorted distinct sources.
	var sources []string
	err = sqlitex.Execute(store.conn,
		`SELECT data_source_id AS s FROM entity_source WHERE entity_key = ?1 ORDER BY data_source_id`,
		&sqlitex.ExecOptions{
			Args: []any{key},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				sources = append(sources, stmt.GetText("s"))
				return nil
			},
		})
	if err != nil {
		t.Fatalf("read attestations: %v", err)
	}
	if len(sources) != 2 || sources[0] != string(DataSourceModelsDev) || sources[1] != string(DataSourceOllama) {
		t.Errorf("dual-attested sources = %v, want [models.dev ollama]", sources)
	}

	// Insert-or-replace: re-attesting an existing (key, source) pair does not duplicate.
	if err := store.UpsertEntitySources(ctx, attestations[:1]); err != nil {
		t.Fatalf("UpsertEntitySources (replace): %v", err)
	}
	if got := countRows(t, store.conn, "entity_source"); got != 3 {
		t.Errorf("after re-attest, entity_source row count = %d, want 3 (composite-key replace)", got)
	}
}

// TestUpsertEntitySources_UnknownSourceRejected proves the entity_source →
// data_sources FK bites through the public API: attesting an entity to a source
// that was never registered via UpsertDataSources is rejected.
func TestUpsertEntitySources_UnknownSourceRejected(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.UpsertEntitySources(ctx, []EntitySource{
		{EntityKey: "llama@3.3#70b", SourceID: "never-registered"},
	}); err == nil {
		t.Error("attestation to an unregistered source was accepted; entity_source.data_source_id FK not enforced")
	}
}
