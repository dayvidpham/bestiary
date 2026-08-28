package bestiary

import (
	"context"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// v8NomenAttestationsTableSQL is the PRE-v9 nomen_attestations shape, retained here
// (and only here) so the migration fixture is a genuine v8 table rather than the
// current production DDL with a column removed. It is byte-identical to the shipped
// v8 constant apart from the absent archived_url column.
const v8NomenAttestationsTableSQL = `CREATE TABLE IF NOT EXISTS nomen_attestations (
    value       TEXT NOT NULL,
    scheme      TEXT NOT NULL,
    entity_key  TEXT NOT NULL,
    source_url  TEXT NOT NULL DEFAULT '',
    source_id   TEXT NOT NULL REFERENCES data_sources(data_source_id),
    authority   TEXT NOT NULL DEFAULT 'unknown',
    method      TEXT NOT NULL DEFAULT 'unknown',
    ingested_at TEXT NOT NULL DEFAULT ''
);`

// v8Attestation is one seeded pre-v9 attestation row, kept as an explicit record so
// the post-migration assertions can compare every column value it was written with.
type v8Attestation struct {
	value, scheme, entity, sourceURL, sourceID, authority, method, ingestedAt string
}

// v8FixtureAttestations are the seeded rows: one per §3.2 provenance branch — a
// self-minted canonical, a harvested provider-id, a curated alias carrying an
// archive-snapshot source_url, and a harvested HuggingFace nomen whose source_url is
// the LIVE repo page (the exact row the v9 column exists to enrich). Every one of
// them must survive the migration byte-identical, with an EMPTY ArchivedURL.
var v8FixtureAttestations = []v8Attestation{
	{"grok@4.20", "canonical", "grok@4.20", "", "models.dev", "primary", "self-minted", ""},
	{"grok-4.20-0309", "provider-id", "grok@4.20", "", "models.dev", "secondary", "harvested", ""},
	{"grok-beta", "alias", "grok@4.20", "https://web.archive.org/web/20260101000000/https://docs.x.ai/docs/models", "curated", "primary", "curated", "2026-07-20T00:00:00Z"},
	{"xai-org/grok-1", "huggingface", "grok@4.20", "https://huggingface.co/xai-org/grok-1", "huggingface", "primary", "harvested", ""},
}

// createV8DB writes a genuine v8-schema SQLite database to path: schema_meta
// (version=8), the full v8 models table, the v6 provenance + metadata tables, the
// v8-shaped nomina parent, the PRE-v9 nomen_attestations child (no archived_url) and
// the creators dimension, three seeded data sources, one models row, and one nomina
// parent + attestation row per fixture branch. It deliberately has NO archived_url
// column — that is exactly what the v8→v9 migration must add. It uses the production
// DDL for everything except that one table, so the fixture is faithful.
func createV8DB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV8DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		t.Fatalf("createV8DB: enable foreign_keys: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV8DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{8}}); err != nil {
		t.Fatalf("createV8DB: insert schema version: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaSQL, nil); err != nil {
		t.Fatalf("createV8DB: create models table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, indexSQL, nil); err != nil {
		t.Fatalf("createV8DB: create index: %v", err)
	}
	if err := createProvenanceTablesV6(conn); err != nil {
		t.Fatalf("createV8DB: create v6 provenance tables: %v", err)
	}
	if err := createNominaTableV8(conn); err != nil {
		t.Fatalf("createV8DB: create v8 nomina table: %v", err)
	}
	// The PRE-v9 child table: the whole point of the fixture.
	if err := sqlitex.ExecuteTransient(conn, v8NomenAttestationsTableSQL, nil); err != nil {
		t.Fatalf("createV8DB: create v8 nomen_attestations table: %v", err)
	}
	if err := createCreatorsTable(conn); err != nil {
		t.Fatalf("createV8DB: create creators table: %v", err)
	}

	sources := []struct{ id, uri, name string }{
		{"models.dev", "https://models.dev/api.json", "models.dev"},
		{"curated", "https://github.com/dayvidpham/bestiary/tree/main/parse/data", "bestiary curated claim files"},
		{"huggingface", "https://huggingface.co/api/models", "HuggingFace Hub"},
	}
	for _, src := range sources {
		if err := sqlitex.Execute(conn,
			`INSERT INTO data_sources (data_source_id, uri, canonical_name) VALUES (?1, ?2, ?3)`,
			&sqlitex.ExecOptions{Args: []any{src.id, src.uri, src.name}}); err != nil {
			t.Fatalf("createV8DB: seed %q data_source: %v", src.id, err)
		}
	}
	if err := sqlitex.Execute(conn,
		`INSERT INTO models (model_id, provider, display_name, last_synced) VALUES ('m1','p1','M1','2026-01-01T00:00:00Z')`, nil); err != nil {
		t.Fatalf("createV8DB: seed models row: %v", err)
	}

	for _, a := range v8FixtureAttestations {
		if err := sqlitex.Execute(conn,
			`INSERT INTO nomina (value, scheme, entity_key, status) VALUES (?1, ?2, ?3, 'admitted')`,
			&sqlitex.ExecOptions{Args: []any{a.value, a.scheme, a.entity}}); err != nil {
			t.Fatalf("createV8DB: seed nomina parent %q: %v", a.value, err)
		}
		if err := sqlitex.Execute(conn,
			`INSERT INTO nomen_attestations (value, scheme, entity_key, source_url, source_id, authority, method, ingested_at)
			 VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8)`,
			&sqlitex.ExecOptions{Args: []any{a.value, a.scheme, a.entity, a.sourceURL, a.sourceID, a.authority, a.method, a.ingestedAt}}); err != nil {
			t.Fatalf("createV8DB: seed nomen_attestations row %q: %v", a.value, err)
		}
	}
}

// readRawAttestations reads every nomen_attestations row back through raw SQL,
// keyed by value. It deliberately bypasses QueryNomina so the assertions see the
// stored columns themselves, not a reconstructed projection.
func readRawAttestations(t *testing.T, conn *sqlite.Conn, withArchived bool) map[string]v8Attestation {
	t.Helper()
	out := map[string]v8Attestation{}
	archived := map[string]string{}
	query := `SELECT value, scheme, entity_key, source_url, source_id, authority, method, ingested_at FROM nomen_attestations`
	if withArchived {
		query = `SELECT value, scheme, entity_key, source_url, source_id, authority, method, ingested_at, archived_url FROM nomen_attestations`
	}
	err := sqlitex.Execute(conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			row := v8Attestation{
				value:      stmt.GetText("value"),
				scheme:     stmt.GetText("scheme"),
				entity:     stmt.GetText("entity_key"),
				sourceURL:  stmt.GetText("source_url"),
				sourceID:   stmt.GetText("source_id"),
				authority:  stmt.GetText("authority"),
				method:     stmt.GetText("method"),
				ingestedAt: stmt.GetText("ingested_at"),
			}
			out[row.value] = row
			if withArchived {
				archived[row.value] = stmt.GetText("archived_url")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("readRawAttestations: %v", err)
	}
	if withArchived {
		for value, got := range archived {
			if got != "" {
				t.Errorf("migrated attestation %q has archived_url = %q, want \"\" "+
					"(the v9 column is added with an empty default; the migration must not invent a snapshot)", value, got)
			}
		}
	}
	return out
}

// TestStoreMigrate_V8toV9 is the v8→v9 migration + zero-data-loss fence: opening a
// genuine v8 database with a v0.2.10 build advances schema_meta to 9, ADDS the
// nomen_attestations.archived_url column via the presence-guarded self-heal, and
// leaves every pre-existing row byte-identical with an EMPTY ArchivedURL. The
// migration is additive by construction — no table is dropped, recreated or
// reordered — which this test pins by comparing the full pre-migration column list
// against the post-migration one (the v9 column appended, nothing else moved).
func TestStoreMigrate_V8toV9(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v8.db")
	createV8DB(t, dbPath)

	// Precondition: the fixture genuinely is a v8 shape (child table present, but no
	// archived_url column). Without this the test could pass against a v9 fixture.
	pre, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	if has, _ := tableExists(pre, "nomen_attestations"); !has {
		t.Fatal("fixture lacks nomen_attestations; not a genuine v8 fixture")
	}
	if has, _ := columnExists(pre, "nomen_attestations", "archived_url"); has {
		t.Fatal("fixture already has nomen_attestations.archived_url; not a genuine v8 fixture")
	}
	preCols := tableColumnOrder(t, pre, "nomen_attestations")
	preRows := readRawAttestations(t, pre, false)
	preCount := countRows(t, pre, "nomen_attestations")
	preNomina := countRows(t, pre, "nomina")
	pre.Close()

	if len(preRows) != len(v8FixtureAttestations) {
		t.Fatalf("fixture seeded %d attestation rows, want %d", len(preRows), len(v8FixtureAttestations))
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v8→v9): %v", err)
	}
	defer store.Close()

	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	if has, _ := columnExists(store.conn, "nomen_attestations", "archived_url"); !has {
		t.Fatal("v8→v9 migration did not add nomen_attestations.archived_url")
	}

	// The column list is the pre-migration list with archived_url APPENDED: nothing
	// dropped, nothing reordered (SQLite ADD COLUMN appends, a recreate would not).
	postCols := tableColumnOrder(t, store.conn, "nomen_attestations")
	wantCols := append(append([]string{}, preCols...), "archived_url")
	if len(postCols) != len(wantCols) {
		t.Fatalf("nomen_attestations columns after migration = %v, want %v", postCols, wantCols)
	}
	for i := range wantCols {
		if postCols[i] != wantCols[i] {
			t.Errorf("nomen_attestations column %d = %q, want %q (the migration must APPEND archived_url, never reorder or recreate);\n got  %v\n want %v",
				i, postCols[i], wantCols[i], postCols, wantCols)
		}
	}

	// Zero row loss on both parent and child.
	if got := countRows(t, store.conn, "nomen_attestations"); got != preCount {
		t.Errorf("nomen_attestations row count = %d after migration, want %d (zero loss)", got, preCount)
	}
	if got := countRows(t, store.conn, "nomina"); got != preNomina {
		t.Errorf("nomina row count = %d after migration, want %d (zero loss)", got, preNomina)
	}

	// Every pre-existing row is byte-identical on every pre-v9 column, and its new
	// archived_url is empty (asserted inside readRawAttestations).
	postRows := readRawAttestations(t, store.conn, true)
	for value, want := range preRows {
		got, ok := postRows[value]
		if !ok {
			t.Errorf("attestation %q lost in the v8→v9 migration", value)
			continue
		}
		if got != want {
			t.Errorf("attestation %q changed across the v8→v9 migration:\n got  %+v\n want %+v (byte-identical)", value, got, want)
		}
	}

	// The Go-level read path agrees: every migrated attestation carries an empty
	// ArchivedURL, and the rest of its provenance round-trips unchanged.
	out, err := store.QueryNomina(context.Background())
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	if len(out) != len(v8FixtureAttestations) {
		t.Fatalf("QueryNomina returned %d nomina, want %d", len(out), len(v8FixtureAttestations))
	}
	for _, n := range out {
		if len(n.Attestations) != 1 {
			t.Errorf("migrated nomen %q carries %d attestations, want 1", n.Value, len(n.Attestations))
			continue
		}
		if got := n.Attestations[0].ArchivedURL; got != "" {
			t.Errorf("migrated nomen %q has ArchivedURL = %q, want \"\"", n.Value, got)
		}
	}
	// The harvested HF row keeps its LIVE source_url untouched — the v9 column sits
	// beside it, it does not replace or rewrite it.
	byValue := map[string]Nomen{}
	for _, n := range out {
		byValue[n.Value] = n
	}
	hf, ok := byValue["xai-org/grok-1"]
	if !ok {
		t.Fatal("harvested HuggingFace nomen lost in the migration")
	}
	if got := hf.Attestations[0].SourceURL; got != "https://huggingface.co/xai-org/grok-1" {
		t.Errorf("harvested SourceURL = %q, want the unchanged live repo URL", got)
	}

	// The seeded models row survives.
	if m, err := store.QueryModel(context.Background(), "m1"); err != nil || m.ID != "m1" {
		t.Errorf("seeded models row lost after v8→v9: m=%+v err=%v", m, err)
	}
}

// TestStoreMigrate_V8toV9_IdempotentRerun re-runs the v9 self-heal over an
// already-migrated database and asserts it is a no-op: the column list, the row
// count and every row value are unchanged, and no error is raised. This is the
// intermediate-cache case the presence guard exists for — a database whose
// schema_meta already reads 9 must not be re-ALTERed (a second ADD COLUMN is a hard
// SQLite error, so an unguarded self-heal would make every subsequent open fail).
func TestStoreMigrate_V8toV9_IdempotentRerun(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v8.db")
	createV8DB(t, dbPath)

	first, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("first OpenStore (v8→v9): %v", err)
	}
	wantCols := tableColumnOrder(t, first.conn, "nomen_attestations")
	wantRows := readRawAttestations(t, first.conn, true)
	wantCount := countRows(t, first.conn, "nomen_attestations")
	// The self-heal called directly a second time on the SAME connection: the
	// tightest form of the idempotence claim (no reopen to hide behind).
	if err := ensureNomenAttestationColumnsV9(first.conn); err != nil {
		t.Fatalf("ensureNomenAttestationColumnsV9 re-run on a complete v9 table: %v", err)
	}
	first.Close()

	second, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("second OpenStore (already v9): %v", err)
	}
	defer second.Close()

	if v, _ := getSchemaVersion(second.conn); v != currentSchemaVersion {
		t.Errorf("schema version after reopen = %d, want %d", v, currentSchemaVersion)
	}
	gotCols := tableColumnOrder(t, second.conn, "nomen_attestations")
	if len(gotCols) != len(wantCols) {
		t.Errorf("column list changed on rerun: got %v, want %v", gotCols, wantCols)
	} else {
		for i := range wantCols {
			if gotCols[i] != wantCols[i] {
				t.Errorf("column %d changed on rerun: got %q, want %q", i, gotCols[i], wantCols[i])
			}
		}
	}
	if got := countRows(t, second.conn, "nomen_attestations"); got != wantCount {
		t.Errorf("row count changed on rerun: got %d, want %d", got, wantCount)
	}
	gotRows := readRawAttestations(t, second.conn, true)
	for value, want := range wantRows {
		if got, ok := gotRows[value]; !ok || got != want {
			t.Errorf("row %q changed on rerun:\n got  %+v (present=%v)\n want %+v", value, got, ok, want)
		}
	}
}

// TestStoreV9_IntermediateCacheSelfHeals covers the specific defect the presence
// guard is FOR: a cache whose schema_meta already reads 9 (so the version-gated
// migration arm never runs) but whose nomen_attestations table predates the column,
// as an intermediate build of this branch would leave it. Opening it must backfill
// the column rather than fail the next read with "no such column".
func TestStoreV9_IntermediateCacheSelfHeals(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "intermediate-v9.db")
	createV8DB(t, dbPath)

	// Stamp it as v9 while it is still v8-shaped.
	conn, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	if err := setSchemaVersion(conn, currentSchemaVersion); err != nil {
		t.Fatalf("stamp schema_meta=%d: %v", currentSchemaVersion, err)
	}
	conn.Close()

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (intermediate v9 cache): %v", err)
	}
	defer store.Close()

	if has, _ := columnExists(store.conn, "nomen_attestations", "archived_url"); !has {
		t.Fatal("intermediate-v9 cache was not self-healed; nomen_attestations.archived_url still absent")
	}
	// And the read path works rather than erroring on the missing column.
	if _, err := store.QueryNomina(context.Background()); err != nil {
		t.Errorf("QueryNomina after self-heal: %v", err)
	}
}

// TestStoreV9_FreshTablesPresent verifies the fresh-DB path creates the v9 shape
// directly: a fresh database must never be left recording schema_meta=9 with a
// pre-v9 nomen_attestations table (the fresh-arm discipline the v5/v6/v8 tables
// follow).
func TestStoreV9_FreshTablesPresent(t *testing.T) {
	store := openMemStoreInternal(t)
	if has, err := columnExists(store.conn, "nomen_attestations", "archived_url"); err != nil {
		t.Fatalf("columnExists: %v", err)
	} else if !has {
		t.Error("fresh v9 DB is missing nomen_attestations.archived_url; the fresh-DB path must create the v9 shape")
	}
	if v, _ := getSchemaVersion(store.conn); v != 9 {
		t.Errorf("fresh DB schema version = %d, want 9", v)
	}
}

// TestStoreV9_ArchivedURLRoundTrip is the write-side fence: an attestation carrying
// an ArchivedURL persists and reads back byte-identical alongside its unchanged
// SourceURL, and two attestations that differ ONLY in ArchivedURL are kept as two
// distinct rows (they are distinct evidence records; collapsing them would lose
// one). The empty case round-trips as empty — an absent snapshot is data, not a
// write error.
func TestStoreV9_ArchivedURLRoundTrip(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()
	seedModelsDevSource(t, store)

	const live = "https://huggingface.co/xai-org/grok-1"
	const snap = "https://web.archive.org/web/20260101000000/https://huggingface.co/xai-org/grok-1"

	ref := EntityRef{Family: "grok", Version: "4.20"}
	in := []Nomen{{
		Value: "xai-org/grok-1", Scheme: NomenSchemeHuggingFace, Status: AcceptabilityAdmitted, ResolvesTo: ref,
		Attestations: []NomenAttestation{
			{SourceURL: live, ArchivedURL: snap, Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodHarvested},
			{SourceURL: live, ArchivedURL: "", Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodHarvested},
		},
	}}
	if err := store.UpsertNomina(ctx, in); err != nil {
		t.Fatalf("UpsertNomina: %v", err)
	}
	out, err := store.QueryNomina(ctx)
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("QueryNomina returned %d nomina, want 1", len(out))
	}
	got := out[0].Attestations
	if len(got) != 2 {
		t.Fatalf("round-tripped %d attestations, want 2 (two rows differing only in ArchivedURL are DISTINCT evidence records): %+v", len(got), got)
	}
	// sortAndDedupAttestations orders on the TOTAL key, so "" sorts before the snapshot.
	if got[0].ArchivedURL != "" {
		t.Errorf("attestation[0].ArchivedURL = %q, want \"\"", got[0].ArchivedURL)
	}
	if got[1].ArchivedURL != snap {
		t.Errorf("attestation[1].ArchivedURL = %q, want %q", got[1].ArchivedURL, snap)
	}
	for i, a := range got {
		if a.SourceURL != live {
			t.Errorf("attestation[%d].SourceURL = %q, want the unchanged live URL %q", i, a.SourceURL, live)
		}
	}

	// Re-upserting the identical set stays a replaceable set: the raw child table
	// carries exactly the two rows, never four.
	if err := store.UpsertNomina(ctx, in); err != nil {
		t.Fatalf("UpsertNomina (2nd): %v", err)
	}
	if n := countRows(t, store.conn, "nomen_attestations"); n != 2 {
		t.Errorf("raw nomen_attestations row count = %d after two identical upserts, want 2", n)
	}
}

// tableColumnOrder returns table's column names in declaration order (pragma
// table_info's cid order), so a test can assert a migration APPENDED a column rather
// than recreating the table with a different layout.
func tableColumnOrder(t *testing.T, conn *sqlite.Conn, table string) []string {
	t.Helper()
	var cols []string
	err := sqlitex.Execute(conn, `SELECT name FROM pragma_table_info(?1) ORDER BY cid`, &sqlitex.ExecOptions{
		Args: []any{table},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			cols = append(cols, stmt.GetText("name"))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("tableColumnOrder(%q): %v", table, err)
	}
	return cols
}
