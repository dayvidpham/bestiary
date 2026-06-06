package bestiary

// Migration tests live in the internal test package so they can access
// unexported helpers (getSchemaVersion, migrateSchema) and the conn field.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// TestMigration_FreshDB_IndexCreated verifies that a brand-new (fresh-install)
// database has the idx_canonical index — i.e., the index is not only created
// by migrateToV3 (upgrade path) but also by the fresh-DB path in migrateSchema.
func TestMigration_FreshDB_IndexCreated(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	var found bool
	err = sqlitex.Execute(store.conn,
		`SELECT 1 FROM sqlite_master WHERE type='index' AND name='idx_canonical'`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				found = true
				return nil
			},
		})
	if err != nil {
		t.Fatalf("query sqlite_master for idx_canonical: %v", err)
	}
	if !found {
		t.Error("idx_canonical index not found in fresh database; fresh-DB path must create the index")
	}
}

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
//   - The interleaved_config column exists and defaults to ”.
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

// v2Schema is the v2 schema: composite primary key, has interleaved_config, no canonical columns.
const v2Schema = `CREATE TABLE models (
    model_id          TEXT NOT NULL,
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

// createV2DB writes a v2-schema SQLite database to path with schema_meta (version=2)
// and inserts the given rows. The rows slice contains (model_id, provider, family,
// release_date) tuples for test data.
func createV2DB(t *testing.T, path string, rows []struct{ modelID, provider, family, releaseDate string }) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV2DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV2DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{2}}); err != nil {
		t.Fatalf("createV2DB: insert schema version: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v2Schema, nil); err != nil {
		t.Fatalf("createV2DB: create table: %v", err)
	}
	for _, r := range rows {
		err := sqlitex.Execute(conn,
			`INSERT INTO models (model_id, provider, display_name, family, release_date, last_synced)
            VALUES (?1, ?2, ?3, ?4, ?5, '2026-01-01T00:00:00Z')`,
			&sqlitex.ExecOptions{Args: []any{r.modelID, r.provider, r.modelID + "-display", r.family, r.releaseDate}})
		if err != nil {
			t.Fatalf("createV2DB: insert row (%s, %s): %v", r.modelID, r.provider, err)
		}
	}
}

// TestMigration_FreshDB_Idempotent opens the same fresh (v3) database twice and
// verifies that the second open does not re-migrate, the schema version remains
// currentSchemaVersion, and previously written data is preserved.
func TestMigration_FreshDB_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// First open — creates a fresh v3 DB and writes a row.
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

// TestMigration_V2Idempotent opens the same v2 database (created by createV2DB)
// twice and verifies that the second open does not re-migrate, the schema
// version remains currentSchemaVersion, and data is preserved.
// This exercises the v2→v3→v4 migration path specifically for idempotency: the
// first open migrates, the second open must be a no-op.
func TestMigration_V2Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		{"m1", "anthropic", "claude", ""},
	})

	// First open — migrates v2 → v3.
	{
		store, err := OpenStore(dbPath)
		if err != nil {
			t.Fatalf("first OpenStore (v2→v3): %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
	}

	// Second open — must not error and must still see currentSchemaVersion and data.
	{
		store, err := OpenStore(dbPath)
		if err != nil {
			t.Fatalf("second OpenStore (v2 idempotent): %v", err)
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
		if got.Provider != ProviderAnthropic {
			t.Errorf("Provider = %q, want %q", got.Provider, ProviderAnthropic)
		}
	}
}

// TestMigration_V2toV3_PreservesData creates a v2 database with two rows, migrates
// to v3 via OpenStore, and asserts both rows are present with correct non-canonical fields.
func TestMigration_V2toV3_PreservesData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		{"claude-opus-4-20250514", "anthropic", "claude-opus", "2025-05-14"},
		{"gemini-pro", "google", "gemini-pro", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v2→v3 migration): %v", err)
	}
	defer store.Close()

	if ver, _ := getSchemaVersion(store.conn); ver != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d", ver, currentSchemaVersion)
	}

	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows preserved, got %d", len(all))
	}

	// raw_family must be preserved from the old v2 family column;
	// canonical fields must be backfilled by the migration.
	byID := make(map[ModelID]ModelInfo, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}
	if m, ok := byID["claude-opus-4-20250514"]; ok {
		if m.RawFamily != "claude-opus" {
			t.Errorf("claude-opus-4: RawFamily (raw_family) = %q, want %q", m.RawFamily, "claude-opus")
		}
		// ParseFamily("claude-opus") = ("claude", "opus")
		if m.Family != "claude" {
			t.Errorf("claude-opus-4: Family = %q, want %q", m.Family, "claude")
		}
		if m.Variant != "opus" {
			t.Errorf("claude-opus-4: Variant = %q, want %q", m.Variant, "opus")
		}
		// ExtractDate("claude-opus-4-20250514", "2025-05-14") = "2025-05-14" (from model_id)
		if m.Date != "2025-05-14" {
			t.Errorf("claude-opus-4: Date = %q, want %q", m.Date, "2025-05-14")
		}
	} else {
		t.Error("claude-opus-4-20250514 not found after migration")
	}
	if m, ok := byID["gemini-pro"]; ok {
		if m.RawFamily != "gemini-pro" {
			t.Errorf("gemini-pro: RawFamily (raw_family) = %q, want %q", m.RawFamily, "gemini-pro")
		}
		// ParseFamily("gemini-pro") = ("gemini", "pro")
		if m.Family != "gemini" {
			t.Errorf("gemini-pro: Family = %q, want %q", m.Family, "gemini")
		}
		if m.Variant != "pro" {
			t.Errorf("gemini-pro: Variant = %q, want %q", m.Variant, "pro")
		}
		// ExtractDate("gemini-pro", "") = "" (no date in model_id, no release_date)
		if m.Date != "" {
			t.Errorf("gemini-pro: Date = %q, want %q", m.Date, "")
		}
	} else {
		t.Error("gemini-pro not found after migration")
	}
}

// TestMigration_V2toV3_Backfill creates a v2 database and migrates to v3,
// asserting Family/Variant/Date are backfilled from ParseFamily and ExtractDate.
// Two rows cover both ExtractDate branches:
//   - date embedded in model_id, no release_date
//   - no date in model_id, date taken from release_date column
func TestMigration_V2toV3_Backfill(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		// Row 1: date embedded in model_id; release_date is empty.
		// ParseFamily("claude-opus") = ("claude","opus")
		// ExtractDate("claude-opus-4-20250514", "") = "2025-05-14" (from model_id)
		{"claude-opus-4-20250514", "anthropic", "claude-opus", ""},
		// Row 2: model_id has no embedded date; release_date is non-empty.
		// ParseFamily("gemini-pro") = ("gemini-pro","") or similar single-token result.
		// ExtractDate("gemini-pro", "2024-06-01") = "2024-06-01" (from release_date)
		{"gemini-pro", "google", "gemini-pro", "2024-06-01"},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (backfill test): %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// --- Row 1: date from model_id ---
	got1, err := store.QueryModel(ctx, ModelID("claude-opus-4-20250514"))
	if err != nil {
		t.Fatalf("QueryModel (row 1): %v", err)
	}
	if got1.RawFamily != "claude-opus" {
		t.Errorf("row1: RawFamily (raw_family) = %q, want %q", got1.RawFamily, "claude-opus")
	}
	if got1.Family != "claude" {
		t.Errorf("row1: Family = %q, want %q", got1.Family, "claude")
	}
	if got1.Variant != "opus" {
		t.Errorf("row1: Variant = %q, want %q", got1.Variant, "opus")
	}
	// Date must come from the model_id, not release_date (which is empty).
	if got1.Date != "2025-05-14" {
		t.Errorf("row1: Date = %q, want %q", got1.Date, "2025-05-14")
	}

	// --- Row 2: date from release_date (model_id has no embedded date) ---
	got2, err := store.QueryModel(ctx, ModelID("gemini-pro"))
	if err != nil {
		t.Fatalf("QueryModel (row 2): %v", err)
	}
	if got2.RawFamily != "gemini-pro" {
		t.Errorf("row2: RawFamily (raw_family) = %q, want %q", got2.RawFamily, "gemini-pro")
	}
	// Date must come from release_date "2024-06-01" because model_id has no date.
	if got2.Date != "2024-06-01" {
		t.Errorf("row2: Date = %q, want %q (should come from release_date)", got2.Date, "2024-06-01")
	}
}

// TestMigration_V2toV3_Idempotent opens a v3 database twice and asserts that
// the second open does not re-migrate and the schema version remains 3.
func TestMigration_V2toV3_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		{"gpt-4o-2024-08-06", "openai", "gpt-4o", "2024-08-06"},
	})

	// First open: migrates v2 → v3.
	{
		store, err := OpenStore(dbPath)
		if err != nil {
			t.Fatalf("first OpenStore: %v", err)
		}
		if err := store.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
	}

	// Second open: must not error; must see version 3; data unchanged.
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
		got, err := store.QueryModel(ctx, ModelID("gpt-4o-2024-08-06"))
		if err != nil {
			t.Fatalf("QueryModel on second open: %v", err)
		}
		if got.RawFamily != "gpt-4o" {
			t.Errorf("RawFamily (raw_family) = %q, want %q", got.RawFamily, "gpt-4o")
		}
	}
}

// TestMigration_V2toV3_EdgeCases covers edge cases in backfill:
//   - empty family: ParseFamily("") → ("",""); ExtractDate uses model_id
//   - NULL release_date (empty string in v2): ExtractDate uses model_id only
//   - single-token raw_family: ParseFamily returns (raw,"")
func TestMigration_V2toV3_EdgeCases(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		// Empty family: fallback to InferFamilyFromID logic (returns "" from parse)
		{"some-model-20251201", "local", "", ""},
		// Single-token raw_family: "gpt" → ParseFamily returns ("gpt", "")
		{"gpt", "openai", "gpt", ""},
		// family with date in model_id
		{"gemini-2-0-flash-20250205", "google", "gemini", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (edge cases): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(all))
	}

	byID := make(map[ModelID]ModelInfo, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	// Empty family case: Family should be "" (no override, no suffix match)
	if m, ok := byID["some-model-20251201"]; ok {
		if m.Family != "" {
			t.Errorf("empty-family row: Family = %q, want %q", m.Family, "")
		}
		// date extracted from model_id "some-model-20251201"
		if m.Date != "2025-12-01" {
			t.Errorf("empty-family row: Date = %q, want %q", m.Date, "2025-12-01")
		}
	} else {
		t.Error("some-model-20251201 not found")
	}

	// Single-token raw_family "gpt" → no override, no pattern, no suffix → fallback
	if m, ok := byID["gpt"]; ok {
		if m.RawFamily != "gpt" {
			t.Errorf("gpt row: RawFamily (raw_family) = %q, want %q", m.RawFamily, "gpt")
		}
		// ParseFamily("gpt") → ("gpt","") because no pattern/suffix matches a single token
		if m.Family != "gpt" {
			t.Errorf("gpt row: Family = %q, want %q", m.Family, "gpt")
		}
		if m.Variant != "" {
			t.Errorf("gpt row: Variant = %q, want %q", m.Variant, "")
		}
	} else {
		t.Error("gpt not found")
	}

	// gemini row: date extracted from model_id
	if m, ok := byID["gemini-2-0-flash-20250205"]; ok {
		if m.Date != "2025-02-05" {
			t.Errorf("gemini row: Date = %q, want %q", m.Date, "2025-02-05")
		}
	} else {
		t.Error("gemini-2-0-flash-20250205 not found")
	}
}

// TestMigration_V2toV3_IndexUsed verifies that EXPLAIN QUERY PLAN for the
// actual QueryByCanonical predicate shape (family, variant, date) references
// the idx_canonical index. The index covers (family, variant, version, provider);
// SQLite uses the (family, variant, version) prefix as a range scan and treats
// date as a residual filter. This test asserts that the index is reachable from
// the migrated-DB code path (separate from the fresh-DB path tested elsewhere).
func TestMigration_V2toV3_IndexUsed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		{"claude-opus-4-20250514", "anthropic", "claude-opus", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	// Run EXPLAIN QUERY PLAN using the same predicate shape as QueryByCanonical
	// (family, variant, date) — not provider — so the plan reflects actual usage.
	var planLines []string
	err = sqlitex.Execute(store.conn,
		`EXPLAIN QUERY PLAN SELECT * FROM models WHERE family = ?1 AND variant = ?2 AND date = ?3`,
		&sqlitex.ExecOptions{
			Args: []any{"claude", "opus", "2025-05-14"},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				// EXPLAIN QUERY PLAN columns: id, parent, notused, detail
				detail := stmt.GetText("detail")
				planLines = append(planLines, detail)
				return nil
			},
		})
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}

	// The plan must reference idx_canonical somewhere.
	var found bool
	for _, line := range planLines {
		if strings.Contains(line, "idx_canonical") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("idx_canonical not referenced in query plan; plan:\n%s",
			strings.Join(planLines, "\n"))
	}
}

// ---------------------------------------------------------------------------
// v3 → v4 migration tests
// (SLICE-FIX-1-L2: tests FAIL until L3 implements migrateToV4 + version column wiring)
// ---------------------------------------------------------------------------

// v3Schema is the v3 schema: has raw_family, family, variant, date columns but
// no version column. Used to create test databases for v3→v4 migration testing.
const v3Schema = `CREATE TABLE models (
    model_id          TEXT NOT NULL,
    provider          TEXT NOT NULL,
    display_name      TEXT NOT NULL,
    raw_family        TEXT NOT NULL DEFAULT '',
    family            TEXT NOT NULL DEFAULT '',
    variant           TEXT NOT NULL DEFAULT '',
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

// v3IndexSQL is the v3 canonical index (family, variant, provider) — without version.
const v3IndexSQL = `CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, provider)`

// createV3DB writes a v3-schema SQLite database to path with schema_meta (version=3)
// and inserts rows with the given fields. The rows slice contains
// (model_id, provider, raw_family, family, variant, date) tuples.
func createV3DB(t *testing.T, path string, rows []struct {
	modelID, provider, rawFamily, family, variant, date string
}) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV3DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV3DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{3}}); err != nil {
		t.Fatalf("createV3DB: insert schema version: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v3Schema, nil); err != nil {
		t.Fatalf("createV3DB: create table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v3IndexSQL, nil); err != nil {
		t.Fatalf("createV3DB: create v3 index: %v", err)
	}
	for _, r := range rows {
		err := sqlitex.Execute(conn,
			`INSERT INTO models (model_id, provider, display_name, raw_family, family, variant, date, last_synced)
            VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, '2026-01-01T00:00:00Z')`,
			&sqlitex.ExecOptions{Args: []any{
				r.modelID, r.provider, r.modelID + "-display",
				r.rawFamily, r.family, r.variant, r.date,
			}})
		if err != nil {
			t.Fatalf("createV3DB: insert row (%s, %s): %v", r.modelID, r.provider, err)
		}
	}
}

// TestMigration_V3toV4_PreservesData creates a v3 database with two rows,
// migrates to v4 via OpenStore, and asserts:
//   - Schema version is 4 (currentSchemaVersion).
//   - Both rows are preserved.
//   - The new `version` column exists with default ”.
//   - Existing canonical fields (family, variant, date) are unchanged.
func TestMigration_V3toV4_PreservesData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v3.db")

	createV3DB(t, dbPath, []struct {
		modelID, provider, rawFamily, family, variant, date string
	}{
		{"claude-opus-4-20250514", "anthropic", "claude-opus", "claude", "opus", "2025-05-14"},
		{"gemini-flash", "google", "gemini-flash", "gemini", "flash", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v3→v4 migration): %v", err)
	}
	defer store.Close()

	// Schema version must be 4.
	version, err := getSchemaVersion(store.conn)
	if err != nil {
		t.Fatalf("getSchemaVersion: %v", err)
	}
	if version != currentSchemaVersion {
		t.Errorf("post-migration version = %d, want %d", version, currentSchemaVersion)
	}

	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels after v3→v4: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows preserved, got %d", len(all))
	}

	byID := make(map[ModelID]ModelInfo, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}

	// Check claude row: existing fields preserved, version defaults to ''.
	if m, ok := byID["claude-opus-4-20250514"]; ok {
		if m.Family != "claude" {
			t.Errorf("claude: Family = %q, want %q", m.Family, "claude")
		}
		if m.Variant != "opus" {
			t.Errorf("claude: Variant = %q, want %q", m.Variant, "opus")
		}
		if m.Date != "2025-05-14" {
			t.Errorf("claude: Date = %q, want %q", m.Date, "2025-05-14")
		}
		// Version column defaults to '' for migrated rows.
		if m.Version != "" {
			t.Errorf("claude: Version = %q, want '' (default from migration)", m.Version)
		}
	} else {
		t.Error("claude-opus-4-20250514 not found after v3→v4 migration")
	}

	// Check gemini row.
	if m, ok := byID["gemini-flash"]; ok {
		if m.Family != "gemini" {
			t.Errorf("gemini: Family = %q, want %q", m.Family, "gemini")
		}
		if m.Version != "" {
			t.Errorf("gemini: Version = %q, want '' (default from migration)", m.Version)
		}
	} else {
		t.Error("gemini-flash not found after v3→v4 migration")
	}
}

// TestMigration_V3toV4_VersionColumnExists verifies that the `version` column
// actually exists in the models table after migration (schema-level assertion).
func TestMigration_V3toV4_VersionColumnExists(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v3.db")

	createV3DB(t, dbPath, []struct {
		modelID, provider, rawFamily, family, variant, date string
	}{
		{"m1", "anthropic", "claude-opus", "claude", "opus", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	exists, err := columnExists(store.conn, "models", "version")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if !exists {
		t.Error(
			"version column missing from models table after v3→v4 migration;\n" +
				"  What: v3→v4 migration did not add the version column\n" +
				"  Why: migrateToV4 was not called or did not execute the ALTER TABLE\n" +
				"  Where: store.go migrateToV4 / migrateSchema\n" +
				"  How to fix: ensure migrateSchema calls migrateToV4 when fromVersion < 4",
		)
	}
}

// TestMigration_V3toV4_IndexRebuilt verifies that after migration the
// idx_canonical index covers (family, variant, version, provider) — i.e., the
// new index is wider than the v3 index which only had (family, variant, provider).
//
// We check this by inspecting EXPLAIN QUERY PLAN for a query that predicates on
// version; if the index is used, it covers the version column.
func TestMigration_V3toV4_IndexRebuilt(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "v3.db")

	createV3DB(t, dbPath, []struct {
		modelID, provider, rawFamily, family, variant, date string
	}{
		{"claude-opus-4-5", "anthropic", "claude-opus-4-5", "claude", "opus", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	// EXPLAIN QUERY PLAN with version in the predicate.
	var planLines []string
	err = sqlitex.Execute(store.conn,
		`EXPLAIN QUERY PLAN SELECT * FROM models WHERE family = ?1 AND variant = ?2 AND version = ?3`,
		&sqlitex.ExecOptions{
			Args: []any{"claude", "opus", "4.5"},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				planLines = append(planLines, stmt.GetText("detail"))
				return nil
			},
		})
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}

	var found bool
	for _, line := range planLines {
		if strings.Contains(line, "idx_canonical") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("idx_canonical not referenced in query plan after v3→v4 migration;\n"+
			"  plan:\n%s\n"+
			"  What: idx_canonical does not cover the version column\n"+
			"  Why: v3→v4 migration did not drop and recreate the index\n"+
			"  How to fix: ensure migrateToV4 drops idx_canonical and recreates with (family,variant,version,provider)",
			strings.Join(planLines, "\n"))
	}
}

// TestVersion_RoundTrip verifies that Version survives a UpsertModels + QueryModel round-trip.
func TestVersion_RoundTrip(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	m := ModelInfo{
		ID:          ModelID("claude-opus-4-5-20251101"),
		Provider:    ProviderAnthropic,
		DisplayName: "Claude Opus 4.5",
		RawFamily:   Family("claude-opus-4-5"),
		Family:      Family("claude"),
		Variant:     "opus",
		Version:     "4.5",
		Date:        "2025-11-01",
		LastSynced:  "2026-01-01T00:00:00Z",
	}

	if err := store.UpsertModels(ctx, []ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}

	got, err := store.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	if got.Version != "4.5" {
		t.Errorf(
			"Version round-trip failed: got %q, want %q\n"+
				"  What: Version not persisted or not scanned\n"+
				"  Why: upsert SQL or scanModelInfo does not handle the version column\n"+
				"  Where: store.go UpsertModels / scanModelInfo\n"+
				"  How to fix: add version column to the INSERT + scan in store.go",
			got.Version, "4.5",
		)
	}
	if got.Family != "claude" {
		t.Errorf("Family = %q, want %q", got.Family, "claude")
	}
	if got.Variant != "opus" {
		t.Errorf("Variant = %q, want %q", got.Variant, "opus")
	}
}
