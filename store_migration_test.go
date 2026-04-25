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

	if ver, _ := getSchemaVersion(store.conn); ver != 3 {
		t.Errorf("schema version = %d, want 3", ver)
	}

	ctx := context.Background()
	all, err := store.QueryModels(ctx, "")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows preserved, got %d", len(all))
	}

	// raw_family must be preserved from the old v2 family column.
	byID := make(map[ModelID]ModelInfo, len(all))
	for _, m := range all {
		byID[m.ID] = m
	}
	if m, ok := byID["claude-opus-4-20250514"]; ok {
		if m.Family != "claude-opus" {
			t.Errorf("claude-opus-4: Family (raw_family) = %q, want %q", m.Family, "claude-opus")
		}
	} else {
		t.Error("claude-opus-4-20250514 not found after migration")
	}
	if m, ok := byID["gemini-pro"]; ok {
		if m.Family != "gemini-pro" {
			t.Errorf("gemini-pro: Family (raw_family) = %q, want %q", m.Family, "gemini-pro")
		}
	} else {
		t.Error("gemini-pro not found after migration")
	}
}

// TestMigration_V2toV3_Backfill creates a v2 database with a row where family="claude-opus",
// migrates to v3, and asserts NormalizedFamily/NormalizedVariant/NormalizedDate are backfilled
// from parse.ParseFamily and parse.ExtractDate.
func TestMigration_V2toV3_Backfill(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	createV2DB(t, dbPath, []struct{ modelID, provider, family, releaseDate string }{
		// claude-opus-4-20250514: ParseFamily("claude-opus") = ("claude","opus")
		// ExtractDate("claude-opus-4-20250514", "") = "2025-05-14"
		{"claude-opus-4-20250514", "anthropic", "claude-opus", ""},
	})

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (backfill test): %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	got, err := store.QueryModel(ctx, ModelID("claude-opus-4-20250514"))
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}

	// raw_family preserved
	if got.Family != "claude-opus" {
		t.Errorf("Family (raw_family) = %q, want %q", got.Family, "claude-opus")
	}
	// NormalizedFamily backfilled via ParseFamily("claude-opus")
	if got.NormalizedFamily != "claude" {
		t.Errorf("NormalizedFamily = %q, want %q", got.NormalizedFamily, "claude")
	}
	// NormalizedVariant backfilled
	if got.NormalizedVariant != "opus" {
		t.Errorf("NormalizedVariant = %q, want %q", got.NormalizedVariant, "opus")
	}
	// NormalizedDate extracted from model_id "claude-opus-4-20250514"
	if got.NormalizedDate != "2025-05-14" {
		t.Errorf("NormalizedDate = %q, want %q", got.NormalizedDate, "2025-05-14")
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
		if version != 3 {
			t.Errorf("version after second open = %d, want 3", version)
		}

		ctx := context.Background()
		got, err := store.QueryModel(ctx, ModelID("gpt-4o-2024-08-06"))
		if err != nil {
			t.Fatalf("QueryModel on second open: %v", err)
		}
		if got.Family != "gpt-4o" {
			t.Errorf("Family (raw_family) = %q, want %q", got.Family, "gpt-4o")
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

	// Empty family case: NormalizedFamily should be "" (no override, no suffix match)
	if m, ok := byID["some-model-20251201"]; ok {
		if m.Family != "" {
			t.Errorf("empty-family row: Family = %q, want %q", m.Family, "")
		}
		// date extracted from model_id "some-model-20251201"
		if m.NormalizedDate != "2025-12-01" {
			t.Errorf("empty-family row: NormalizedDate = %q, want %q", m.NormalizedDate, "2025-12-01")
		}
	} else {
		t.Error("some-model-20251201 not found")
	}

	// Single-token family "gpt" → no override, no pattern, no suffix → fallback
	if m, ok := byID["gpt"]; ok {
		if m.Family != "gpt" {
			t.Errorf("gpt row: Family (raw_family) = %q, want %q", m.Family, "gpt")
		}
		// ParseFamily("gpt") → ("gpt","") because no pattern/suffix matches a single token
		if m.NormalizedFamily != "gpt" {
			t.Errorf("gpt row: NormalizedFamily = %q, want %q", m.NormalizedFamily, "gpt")
		}
		if m.NormalizedVariant != "" {
			t.Errorf("gpt row: NormalizedVariant = %q, want %q", m.NormalizedVariant, "")
		}
	} else {
		t.Error("gpt not found")
	}

	// gemini row: date extracted from model_id
	if m, ok := byID["gemini-2-0-flash-20250205"]; ok {
		if m.NormalizedDate != "2025-02-05" {
			t.Errorf("gemini row: NormalizedDate = %q, want %q", m.NormalizedDate, "2025-02-05")
		}
	} else {
		t.Error("gemini-2-0-flash-20250205 not found")
	}
}

// TestMigration_V2toV3_IndexUsed verifies that EXPLAIN QUERY PLAN for a query
// filtering on (family, variant, provider) references the idx_canonical index.
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

	// Run EXPLAIN QUERY PLAN and collect detail lines.
	var planLines []string
	err = sqlitex.Execute(store.conn,
		`EXPLAIN QUERY PLAN SELECT * FROM models WHERE family = ?1 AND variant = ?2 AND provider = ?3`,
		&sqlitex.ExecOptions{
			Args: []any{"claude", "opus", "anthropic"},
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
