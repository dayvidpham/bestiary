package bestiary

// Migration tests live in the internal test package so they can access
// unexported helpers (getSchemaVersion, migrateSchema) and the conn field.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// TestMigration_FreshDB verifies that opening a brand-new database results in
// schema version 2 and a functional store.
func TestMigration_FreshDB(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("fresh DB version = %d, want %d", version, currentSchemaVersion)
	}

	// Confirm the store is usable — upsert and query back.
	ctx := context.Background()
	m := ModelInfo{
		ID:          ModelID("m1"),
		Provider:    ProviderAnthropic,
		DisplayName: "Test m1",
		LastSynced:  "2026-01-01T00:00:00Z",
	}
	if err := store.UpsertModels(ctx, []ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels after fresh migration: %v", err)
	}
	got, err := store.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel after fresh migration: %v", err)
	}
	if got.ID != m.ID || got.Provider != m.Provider {
		t.Errorf("round-trip mismatch: got (%s, %s), want (%s, %s)",
			got.ID, got.Provider, m.ID, m.Provider)
	}
}

// v1Schema is the original schema: single-column PRIMARY KEY, no interleaved_config.
const v1Schema = `CREATE TABLE models (
    model_id          TEXT PRIMARY KEY,
    provider          TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    family            TEXT NOT NULL DEFAULT '',
    context_window    INTEGER NOT NULL DEFAULT 0,
    max_output        INTEGER NOT NULL DEFAULT 0,
    reasoning         INTEGER NOT NULL DEFAULT 0,
    tool_call         INTEGER NOT NULL DEFAULT 0,
    attachment        INTEGER NOT NULL DEFAULT 0,
    temperature       INTEGER NOT NULL DEFAULT 0,
    structured_output INTEGER NOT NULL DEFAULT 0,
    interleaved       INTEGER NOT NULL DEFAULT 0,
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
    last_synced       TEXT NOT NULL
)`

// createV1DB writes a v1-schema SQLite database to path and inserts one row.
func createV1DB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV1DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, v1Schema, nil); err != nil {
		t.Fatalf("createV1DB: create table: %v", err)
	}
	const insertSQL = `INSERT INTO models
        (model_id, provider, display_name, last_synced)
        VALUES ('m1', 'anthropic', 'Test Model', '2026-01-01T00:00:00Z')`
	if err := sqlitex.ExecuteTransient(conn, insertSQL, nil); err != nil {
		t.Fatalf("createV1DB: insert row: %v", err)
	}
}

// TestMigration_V1toV2 creates a v1 database on disk, then opens it with
// OpenStore and verifies:
//   - The version is bumped to 2.
//   - The existing row survives.
//   - The composite primary key is enforced (same model_id + different provider → 2 rows).
//   - The interleaved_config column exists and defaults to ''.
func TestMigration_V1toV2(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV1DB(t, dbPath)

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v1→v2 migration): %v", err)
	}
	defer store.Close()

	// Version must be currentSchemaVersion (3) after migration.
	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion after migration: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("post-migration version = %d, want %d", version, currentSchemaVersion)
	}

	ctx := context.Background()

	// The original row must be preserved.
	got, err := store.QueryModel(ctx, ModelID("m1"))
	if err != nil {
		t.Fatalf("QueryModel after migration: %v", err)
	}
	if got.ID != "m1" {
		t.Errorf("preserved row ID = %q, want %q", got.ID, "m1")
	}
	if got.Provider != ProviderAnthropic {
		t.Errorf("preserved row Provider = %q, want %q", got.Provider, ProviderAnthropic)
	}
	// interleaved_config defaults to '' → Capability.Config should be nil.
	if got.Interleaved.Config != nil {
		t.Errorf("interleaved_config after migration: got %v, want nil", got.Interleaved.Config)
	}

	// Composite key: insert same model_id under a different provider → must succeed.
	m2 := ModelInfo{
		ID:          ModelID("m1"),
		Provider:    ProviderOpenAI,
		DisplayName: "Test Model (OpenAI)",
		LastSynced:  "2026-01-01T00:00:00Z",
	}
	if err := store.UpsertModels(ctx, []ModelInfo{m2}); err != nil {
		t.Fatalf("UpsertModels second provider after migration: %v", err)
	}
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels after migration: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 rows after composite-key upsert, got %d", len(all))
	}
}

// TestMigration_V2Idempotent opens the same v2 database twice and verifies
// that no error occurs, the version remains 2, and data is preserved.
func TestMigration_V2Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// First open — creates fresh v2 DB and writes a row.
	{
		store, err := OpenStore(dbPath)
		if err != nil {
			t.Fatalf("first OpenStore: %v", err)
		}
		ctx := context.Background()
		m := ModelInfo{
			ID:          ModelID("m1"),
			Provider:    ProviderAnthropic,
			DisplayName: "Idempotent Model",
			LastSynced:  "2026-01-01T00:00:00Z",
		}
		if err := store.UpsertModels(ctx, []ModelInfo{m}); err != nil {
			t.Fatalf("first UpsertModels: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
	}

	// Verify the file exists before reopening.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("DB file missing after first open: %v", err)
	}

	// Second open — must not error and must see currentSchemaVersion and the existing row.
	{
		store, err := OpenStore(dbPath)
		if err != nil {
			t.Fatalf("second OpenStore (idempotent): %v", err)
		}
		defer store.Close()

		version, err := getSchemaVersion(store.conn)
		if err != nil {
			t.Fatalf("getSchemaVersion on second open: %v", err)
		}
		if version != currentSchemaVersion {
			t.Errorf("version after second open = %d, want %d", version, currentSchemaVersion)
		}

		ctx := context.Background()
		got, err := store.QueryModel(ctx, ModelID("m1"))
		if err != nil {
			t.Fatalf("QueryModel on second open: %v", err)
		}
		if got.DisplayName != "Idempotent Model" {
			t.Errorf("DisplayName = %q, want %q", got.DisplayName, "Idempotent Model")
		}
	}
}
