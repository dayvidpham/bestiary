package bestiary

import (
	"path/filepath"
	"reflect"
	"testing"
)

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
