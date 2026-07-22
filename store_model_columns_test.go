package bestiary

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// fullColumnModel builds a ModelInfo carrying the v6 instance-level fields, for
// asserting a migrated / self-healed models table round-trips them.
func fullColumnModel(id string) ModelInfo {
	return ModelInfo{
		ID:          ModelID(id),
		Provider:    "p",
		DisplayName: "M " + id,
		Status:      StatusBeta,
		Description: "a description",
		ReasoningOptions: []ReasoningOption{
			{Kind: ReasoningEffort, Values: []string{"low", "high"}},
		},
		CostInputAudioPerMTok: f64(0.5),
		CostContextOver200k:   &TierCost{CostInputPerMTok: f64(6.0)},
		CostTiers:             []CostTier{{ContextSize: 200000, TierCost: TierCost{CostInputPerMTok: f64(6.0)}}},
		LastSynced:            "2026-01-01T00:00:00Z",
	}
}

// modelV6NewColumns is the set of instance-level columns v6 adds to the models
// table. Both the fresh DDL and the migrateToV6 ALTER pass must produce them.
var modelV6NewColumns = []string{
	"description", "status", "status_raw", "reasoning_options",
	"cost_input_audio", "cost_output_audio", "cost_context_over_200k", "cost_tiers",
}

// TestStoreV6_ModelsFreshShapeMatchesMigrated extends the fresh-vs-migrated shape
// guard to the models table: a fresh v6 database and one migrated up from v4 must
// have the SAME models column signature (names, types, NOT NULL, PK positions), so
// the migrateToV6 ALTER TABLE ADD COLUMN pass and the schemaSQL tail can never
// drift. v4Schema places `version` mid-table and ends at last_synced (matching
// schemaSQL), and the v6 columns append after last_synced in both, so the full
// ordered signatures are expected to be identical.
func TestStoreV6_ModelsFreshShapeMatchesMigrated(t *testing.T) {
	fresh, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:) fresh: %v", err)
	}
	defer fresh.Close()

	dbPath := filepath.Join(t.TempDir(), "v4.db")
	createV4DB(t, dbPath, nil)
	migrated, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore migrated: %v", err)
	}
	defer migrated.Close()

	freshSig := tableSignature(t, fresh.conn, "models")
	migSig := tableSignature(t, migrated.conn, "models")
	if !reflect.DeepEqual(freshSig, migSig) {
		t.Errorf("models table shape differs between fresh and migrated v6:\n  fresh    = %+v\n  migrated = %+v", freshSig, migSig)
	}

	// Both must actually carry the eight new columns (guards against a comparison
	// that passes because BOTH forgot them).
	assertHasColumns(t, freshSig, "fresh", modelV6NewColumns)
	assertHasColumns(t, migSig, "migrated", modelV6NewColumns)
}

// TestStoreV6_ModelsColumnTypes pins the declared types / nullability of the new
// columns: TEXT NOT NULL for the string + JSON columns, nullable REAL for the two
// audio-cost columns (matching the existing cost_* columns).
func TestStoreV6_ModelsColumnTypes(t *testing.T) {
	fresh, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer fresh.Close()

	byName := map[string]colSig{}
	for _, c := range tableSignature(t, fresh.conn, "models") {
		byName[c.name] = c
	}

	wantTextNotNull := []string{"description", "status", "status_raw", "reasoning_options", "cost_context_over_200k", "cost_tiers"}
	for _, name := range wantTextNotNull {
		c, ok := byName[name]
		if !ok {
			t.Errorf("models missing column %q", name)
			continue
		}
		if c.ctype != "TEXT" || c.notNull != 1 {
			t.Errorf("column %q = {type:%q notNull:%d}, want TEXT NOT NULL", name, c.ctype, c.notNull)
		}
	}
	for _, name := range []string{"cost_input_audio", "cost_output_audio"} {
		c, ok := byName[name]
		if !ok {
			t.Errorf("models missing column %q", name)
			continue
		}
		if c.ctype != "REAL" || c.notNull != 0 {
			t.Errorf("column %q = {type:%q notNull:%d}, want nullable REAL", name, c.ctype, c.notNull)
		}
	}
}

// assertHasColumns fails unless every wanted column name appears in sig.
func assertHasColumns(t *testing.T, sig []colSig, label string, want []string) {
	t.Helper()
	have := map[string]bool{}
	for _, c := range sig {
		have[c.name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Errorf("%s models table missing v6 column %q", label, name)
		}
	}
}

// assertModelRoundTrip upserts m and asserts its v6 instance-level fields survive
// a QueryModel — proving the migrated / self-healed models table has WORKING
// columns, not merely present ones.
func assertModelRoundTrip(t *testing.T, store *Store, m ModelInfo) {
	t.Helper()
	ctx := context.Background()
	if err := store.UpsertModels(ctx, []ModelInfo{m}); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	got, err := store.QueryModel(ctx, m.ID)
	if err != nil {
		t.Fatalf("QueryModel: %v", err)
	}
	if got.Status != m.Status {
		t.Errorf("Status = %v, want %v", got.Status, m.Status)
	}
	if got.Description != m.Description {
		t.Errorf("Description = %q, want %q", got.Description, m.Description)
	}
	if len(got.ReasoningOptions) != len(m.ReasoningOptions) {
		t.Errorf("ReasoningOptions len = %d, want %d", len(got.ReasoningOptions), len(m.ReasoningOptions))
	}
	if got.CostInputAudioPerMTok == nil || *got.CostInputAudioPerMTok != 0.5 {
		t.Errorf("CostInputAudioPerMTok = %v, want 0.5", got.CostInputAudioPerMTok)
	}
	if len(got.CostTiers) != 1 || got.CostTiers[0].ContextSize != 200000 {
		t.Errorf("CostTiers = %+v, want one 200000 tier", got.CostTiers)
	}
}

// TestStoreMigrate_V5toV6_ModelColumnsRoundTrip is the REAL v0.2.4-user path: a
// schema-v5 database on disk is opened, migrateToV6 adds the instance-level model
// columns, and a full-metadata model round-trips through QueryModels. This proves
// the migrated query path works (the chained-migration test checks schema shape but
// not the query round-trip), closing scenario (1) of the migration gap.
func TestStoreMigrate_V5toV6_ModelColumnsRoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v5.db")
	createV5DB(t, dbPath)

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v5→v6): %v", err)
	}
	defer store.Close()

	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("post-migration version = %d, want %d", v, currentSchemaVersion)
	}
	assertHasColumns(t, tableSignature(t, store.conn, "models"), "migrated-v5", modelV6NewColumns)
	assertModelRoundTrip(t, store, fullColumnModel("v5-migrated-model"))
}

// createV6NoModelColumnsDB writes a database that mimics an INTERMEDIATE-v6 dev
// cache: schema_meta records version 6 and the full v6 provenance + metadata
// tables exist, but the models table predates the eight instance-level columns
// (it is the v4/v5 models shape). Such a database was produced by earlier builds
// of this unreleased branch, before the columns were added to the fresh DDL.
func createV6NoModelColumnsDB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV6NoModelColumnsDB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: enable foreign_keys: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{6}}); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: insert schema version: %v", err)
	}
	// The OLD models shape (v4/v5) — deliberately missing the v6 instance-level columns.
	if err := sqlitex.ExecuteTransient(conn, v4Schema, nil); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: create models table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4IndexSQL, nil); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: create index: %v", err)
	}
	// The full v6 provenance + metadata tables (this cache DID reach v6 shape there).
	if err := createProvenanceTablesV6(conn); err != nil {
		t.Fatalf("createV6NoModelColumnsDB: create v6 provenance tables: %v", err)
	}
}

// TestStoreV6_SelfHealsMissingModelColumns is scenario (2): an intermediate-v6 dev
// cache whose schema_meta already reads 6 but whose models table lacks the eight
// instance-level columns. Because the version-gated migration never runs (6 == 6),
// only the OpenStore self-heal (ensureModelColumnsV6) can add them. Without it,
// UpsertModels/QueryModels error with "no such column"; with it, the columns are
// backfilled on open and the round-trip works, all while schema_meta stays 6.
func TestStoreV6_SelfHealsMissingModelColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6-nocols.db")
	createV6NoModelColumnsDB(t, dbPath)

	// Precondition: the fixture genuinely lacks the columns.
	pre, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	preCols, err := tableColumnSet(pre, "models")
	if err != nil {
		t.Fatalf("read fixture columns: %v", err)
	}
	pre.Close()
	if preCols["description"] {
		t.Fatalf("fixture already has the v6 model columns; the self-heal would be untested")
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v6 self-heal): %v", err)
	}
	defer store.Close()

	// Opening an intermediate-v6 cache now also applies the additive v6→v7 migration
	// (region columns + nomina table), so schema_meta advances to currentSchemaVersion;
	// the v6 column self-heal still backfills the missing v6 columns alongside it.
	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d (v6 cache migrates to current on open)", v, currentSchemaVersion)
	}
	// Columns backfilled, and the query path works.
	assertHasColumns(t, tableSignature(t, store.conn, "models"), "self-healed", modelV6NewColumns)
	assertModelRoundTrip(t, store, fullColumnModel("healed-model"))
}

// TestEnsureModelColumnsV6_Idempotent asserts the self-heal is a safe no-op on an
// already-complete models table (a fresh v6 store) — it can run on every OpenStore
// without error or effect.
func TestEnsureModelColumnsV6_Idempotent(t *testing.T) {
	store, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	defer store.Close()

	before := tableSignature(t, store.conn, "models")
	for i := range 3 {
		if err := ensureModelColumnsV6(store.conn); err != nil {
			t.Fatalf("ensureModelColumnsV6 (run %d): %v", i, err)
		}
	}
	after := tableSignature(t, store.conn, "models")
	if !reflect.DeepEqual(before, after) {
		t.Errorf("repeated ensureModelColumnsV6 changed the models shape:\n  before = %+v\n  after  = %+v", before, after)
	}
}
