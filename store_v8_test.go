package bestiary

import (
	"context"
	"path/filepath"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// createV7DB writes a genuine v7-schema SQLite database to path: schema_meta (version=7),
// the full v7 models table (v4 shape + the eight v6 instance-level columns + the two v7
// region columns), the v6 provenance + metadata tables, the v7-shaped nomina table (with
// the fused source_url/source_id columns), two seeded data sources, one models row, and a
// set of nomina rows exercising each §3.2 derivation branch. It deliberately has NO
// nomen_attestations table and NO creators table — those are exactly what the v7→v8
// migration must add. It uses the production DDL constants so the fixture is faithful.
func createV7DB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV7DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		t.Fatalf("createV7DB: enable foreign_keys: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV7DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{7}}); err != nil {
		t.Fatalf("createV7DB: insert schema version: %v", err)
	}
	// v7 models shape: v4 table + the eight v6 columns + the two v7 region columns.
	if err := sqlitex.ExecuteTransient(conn, v4Schema, nil); err != nil {
		t.Fatalf("createV7DB: create models table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4IndexSQL, nil); err != nil {
		t.Fatalf("createV7DB: create index: %v", err)
	}
	for _, col := range modelV6Columns {
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			t.Fatalf("createV7DB: add v6 column %q: %v", col.name, err)
		}
	}
	for _, col := range modelV7Columns {
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			t.Fatalf("createV7DB: add v7 column %q: %v", col.name, err)
		}
	}
	if err := createProvenanceTablesV6(conn); err != nil {
		t.Fatalf("createV7DB: create v6 provenance tables: %v", err)
	}
	// v7 nomina table (with the fused source columns) — created via the production v7 DDL.
	if err := createNominaTable(conn); err != nil {
		t.Fatalf("createV7DB: create v7 nomina table: %v", err)
	}
	// Seed two data sources (models.dev + curated) so the nomina source_id FK resolves.
	if err := sqlitex.Execute(conn,
		`INSERT INTO data_sources (data_source_id, uri, canonical_name) VALUES ('models.dev','https://models.dev/api.json','models.dev')`, nil); err != nil {
		t.Fatalf("createV7DB: seed models.dev data_source: %v", err)
	}
	if err := sqlitex.Execute(conn,
		`INSERT INTO data_sources (data_source_id, uri, canonical_name) VALUES ('curated','https://github.com/dayvidpham/bestiary/tree/main/parse/data','bestiary curated claim files')`, nil); err != nil {
		t.Fatalf("createV7DB: seed curated data_source: %v", err)
	}
	if err := sqlitex.Execute(conn,
		`INSERT INTO models (model_id, provider, display_name, last_synced) VALUES ('m1','p1','M1','2026-01-01T00:00:00Z')`, nil); err != nil {
		t.Fatalf("createV7DB: seed models row: %v", err)
	}
	// Seed v7 nomina rows, one per §3.2 derivation branch: a canonical (models.dev →
	// Primary/SelfMinted), a provider-id (models.dev → Secondary/Harvested), and a
	// curated alias with a source_url (curated → Primary/Curated).
	nomina := []struct{ value, scheme, entity, status, sourceURL, sourceID string }{
		{"grok@4.20", "canonical", "grok@4.20", "preferred", "", "models.dev"},
		{"grok-4.20-0309", "provider-id", "grok@4.20", "admitted", "", "models.dev"},
		{"grok-beta", "alias", "grok@4.20", "admitted", "https://web.archive.org/web/2026/https://docs.x.ai", "curated"},
	}
	for _, n := range nomina {
		if err := sqlitex.Execute(conn,
			`INSERT INTO nomina (value, scheme, entity_key, status, source_url, source_id) VALUES (?1,?2,?3,?4,?5,?6)`,
			&sqlitex.ExecOptions{Args: []any{n.value, n.scheme, n.entity, n.status, n.sourceURL, n.sourceID}}); err != nil {
			t.Fatalf("createV7DB: seed nomina row %q: %v", n.value, err)
		}
	}
}

// TestStoreMigrate_V7toV8 is the v7→v8 migration + zero-data-loss fence: opening a genuine
// v7 database advances schema_meta to 8, recreates the nomina parent WITHOUT the fused
// source columns, backfills each old row's (source_url, source_id) into ONE
// nomen_attestations row with authority/method DERIVED per §3.2 (ingested_at ”), and
// creates the creators dimension — all with zero data loss on the seeded rows.
func TestStoreMigrate_V7toV8(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v7.db")
	createV7DB(t, dbPath)

	// Precondition: the fixture genuinely is a v7 shape (fused nomina source columns, no
	// child/creators tables).
	pre, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	nominaCols, _ := tableColumnSet(pre, "nomina")
	if !nominaCols["source_url"] || !nominaCols["source_id"] {
		t.Fatal("fixture nomina lacks the fused v7 source columns; not a genuine v7 fixture")
	}
	if has, _ := tableExists(pre, "nomen_attestations"); has {
		t.Fatal("fixture already has nomen_attestations; not a genuine v7 fixture")
	}
	if has, _ := tableExists(pre, "creators"); has {
		t.Fatal("fixture already has creators; not a genuine v7 fixture")
	}
	preNomina := countRows(t, pre, "nomina")
	pre.Close()

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v7→v8): %v", err)
	}
	defer store.Close()

	// Schema advanced to v8.
	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	// The nomina parent no longer carries the fused source columns.
	cols, _ := tableColumnSet(store.conn, "nomina")
	if cols["source_url"] || cols["source_id"] {
		t.Error("v7→v8 migration did not drop the fused nomina source columns")
	}
	// The new tables exist.
	if has, _ := tableExists(store.conn, "nomen_attestations"); !has {
		t.Error("nomen_attestations table not created by the v7→v8 migration")
	}
	if has, _ := tableExists(store.conn, "creators"); !has {
		t.Error("creators table not created by the v7→v8 migration")
	}

	// Zero data loss on the parent: same row count, one attestation backfilled per row.
	if got := countRows(t, store.conn, "nomina"); got != preNomina {
		t.Errorf("nomina row count = %d after migration, want %d (zero loss)", got, preNomina)
	}
	if got := countRows(t, store.conn, "nomen_attestations"); got != preNomina {
		t.Errorf("nomen_attestations row count = %d, want %d (one per migrated nomen)", got, preNomina)
	}

	// Content + derived provenance survive: read the nomina back and check each branch.
	out, err := store.QueryNomina(context.Background())
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	byValue := map[string]Nomen{}
	for _, n := range out {
		byValue[n.Value] = n
	}

	// Canonical (models.dev) → Primary/SelfMinted, empty source_url, ingested_at "".
	assertMigratedAttestation(t, byValue, "grok@4.20", AcceptabilityPreferred, NomenAttestation{
		SourceURL: "", Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodSelfMinted, IngestedAt: "",
	})
	// Provider-id (models.dev) → Secondary/Harvested.
	assertMigratedAttestation(t, byValue, "grok-4.20-0309", AcceptabilityAdmitted, NomenAttestation{
		SourceURL: "", Source: DataSourceModelsDev, Authority: AuthoritySecondary, Method: IngestMethodHarvested, IngestedAt: "",
	})
	// Curated alias → Primary/Curated, preserving the claim source_url.
	assertMigratedAttestation(t, byValue, "grok-beta", AcceptabilityAdmitted, NomenAttestation{
		SourceURL: "https://web.archive.org/web/2026/https://docs.x.ai", Source: DataSourceCurated, Authority: AuthorityPrimary, Method: IngestMethodCurated, IngestedAt: "",
	})

	// The seeded models row survives the chained migration.
	if m, err := store.QueryModel(context.Background(), "m1"); err != nil || m.ID != "m1" {
		t.Errorf("seeded models row lost after v7→v8: m=%+v err=%v", m, err)
	}
}

// assertMigratedAttestation checks a migrated nomen carries exactly one attestation equal
// to want and the expected editorial status.
func assertMigratedAttestation(t *testing.T, byValue map[string]Nomen, value string, wantStatus AcceptabilityRating, want NomenAttestation) {
	t.Helper()
	n, ok := byValue[value]
	if !ok {
		t.Errorf("migrated nomen %q missing after v7→v8", value)
		return
	}
	if n.Status != wantStatus {
		t.Errorf("%q status = %v, want %v", value, n.Status, wantStatus)
	}
	if len(n.Attestations) != 1 {
		t.Errorf("%q carries %d attestations, want 1 (single-attestation backfill)", value, len(n.Attestations))
		return
	}
	if got := n.Attestations[0]; got != want {
		t.Errorf("%q attestation derived wrong on migration:\n got  %+v\n want %+v", value, got, want)
	}
}

// TestStoreV8_FreshTablesPresent verifies the fresh-DB v8 path creates the full v8 naming
// shape: a v8-shaped nomina parent (no fused source columns), the nomen_attestations child,
// and the creators dimension — so a fresh v8 database is never left with schema_meta=8 but
// missing tables.
func TestStoreV8_FreshTablesPresent(t *testing.T) {
	store := openMemStoreInternal(t)
	for _, tbl := range []string{"nomina", "nomen_attestations", "creators"} {
		if has, err := tableExists(store.conn, tbl); err != nil {
			t.Fatalf("tableExists(%q): %v", tbl, err)
		} else if !has {
			t.Errorf("fresh v8 DB is missing the %q table; the fresh-DB path must create it", tbl)
		}
	}
	cols, _ := tableColumnSet(store.conn, "nomina")
	if cols["source_url"] || cols["source_id"] {
		t.Error("fresh v8 nomina carries the fused v7 source columns; the fresh path must create the v8 shape")
	}
}

// TestStore_CreatorsRoundTrip persists the curated creator dimension from the embedded seed
// and reads it back, asserting the persisted store record agrees with the running binary's
// Family.Creator projection (they share the seed) and that a known mapping is present.
func TestStore_CreatorsRoundTrip(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()

	// Fresh dimension is empty.
	if got, err := store.QueryCreators(ctx); err != nil {
		t.Fatalf("QueryCreators (empty): %v", err)
	} else if len(got) != 0 {
		t.Errorf("fresh creators dimension has %d rows, want 0", len(got))
	}

	if err := store.UpsertCreators(ctx); err != nil {
		t.Fatalf("UpsertCreators: %v", err)
	}
	got, err := store.QueryCreators(ctx)
	if err != nil {
		t.Fatalf("QueryCreators: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("creators dimension empty after UpsertCreators; the seed did not persist")
	}
	// A known mapping from creators.json is present (llama → Meta).
	if got["llama"] != CreatorMeta {
		t.Errorf("persisted creator for llama = %q, want %q", got["llama"], CreatorMeta)
	}
	// The persisted store record agrees with the running binary's projection (same seed).
	for family, creator := range got {
		if proj := family.Creator(); proj != creator {
			t.Errorf("persisted creator for %q = %q, disagrees with Family.Creator projection %q", family, creator, proj)
		}
	}

	// Re-upsert is idempotent (INSERT OR REPLACE over the same seed): row count unchanged.
	if err := store.UpsertCreators(ctx); err != nil {
		t.Fatalf("UpsertCreators (2nd): %v", err)
	}
	got2, _ := store.QueryCreators(ctx)
	if len(got2) != len(got) {
		t.Errorf("after re-upsert creators has %d rows, want idempotent %d", len(got2), len(got))
	}
}

// seedCuratedSource writes the curated DataSource so a nomen attestation whose Source is
// DataSourceCurated satisfies the nomen_attestations source_id FK.
func seedCuratedSource(t *testing.T, store *Store) {
	t.Helper()
	ds := DataSource{ID: DataSourceCurated, URI: "https://github.com/dayvidpham/bestiary/tree/main/parse/data", CanonicalName: "bestiary curated claim files"}
	ingest := DatasetIngested{SourceID: DataSourceCurated, IngestedAt: "2026-07-20T00:00:00Z", ParserSchema: 3}
	if err := store.UpsertDataSources(context.Background(), []DataSource{ds}, []DatasetIngested{ingest}); err != nil {
		t.Fatalf("seed curated source: %v", err)
	}
}

// TestStore_NomenAttestationsRawRowCountIdempotent upserts the SAME nomen 3 times and
// asserts the RAW nomen_attestations row count via direct SQL — bypassing QueryNomina's
// read-side dedup (sortAndDedupAttestations), which would otherwise mask a broken
// delete-then-insert step: QueryNomina always reads back a deduplicated set even if the
// underlying table has silently accumulated duplicate rows across re-upserts. Step (2) of
// UpsertNomina (delete the prior replaceable-attestation-set rows for the triple) must run
// on every upsert, so the raw row count after N upserts of an unchanged attestation set
// must equal the attestation-set size, never N times it.
func TestStore_NomenAttestationsRawRowCountIdempotent(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()
	seedModelsDevSource(t, store)
	seedCuratedSource(t, store)

	ref := EntityRef{Family: "grok", Version: "4.20"}
	attestations := []NomenAttestation{
		{Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodSelfMinted},
		{SourceURL: "https://docs.x.ai/docs/models", Source: DataSourceCurated, Authority: AuthoritySecondary, Method: IngestMethodCurated, IngestedAt: "2026-07-20T00:00:00Z"},
	}
	in := []Nomen{
		{Value: "grok@4.20", Scheme: NomenSchemeCanonical, Status: AcceptabilityPreferred, ResolvesTo: ref, Attestations: attestations},
	}

	for i := 0; i < 3; i++ {
		if err := store.UpsertNomina(ctx, in); err != nil {
			t.Fatalf("UpsertNomina (upsert #%d): %v", i+1, err)
		}
	}

	// The QueryNomina-side view still reports the correct, deduplicated set — this alone
	// does NOT prove the delete step ran; it is exactly what the masked bug looked like.
	out, err := store.QueryNomina(ctx)
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	if len(out) != 1 || len(out[0].Attestations) != len(attestations) {
		t.Fatalf("QueryNomina after 3 upserts = %+v, want 1 nomen with %d attestations", out, len(attestations))
	}

	// The falsifiable assertion: the RAW table, read directly via SQL, must carry exactly
	// len(attestations) rows — not 3x that — regardless of what QueryNomina's dedup reports.
	if got := countRows(t, store.conn, "nomen_attestations"); got != len(attestations) {
		t.Errorf("raw nomen_attestations row count = %d after 3 identical upserts, want %d "+
			"(the delete-then-insert replace-set step must clear prior rows on every upsert, "+
			"not just accumulate new ones)", got, len(attestations))
	}
}

// TestStoreV8_NomenAttestationsForeignKeyEnforced pins the nomen_attestations.source_id
// foreign key into data_sources directly at the SQL layer (the TestStoreV5_ForeignKeysEnforced
// / TestStoreV7_NominaForeignKeyEnforced precedent), independent of the UpsertNomina Go-API
// wrapper: a raw INSERT naming an unregistered source_id must be rejected by SQLite with
// PRAGMA foreign_keys=ON, and must leave the table with zero rows (no partial write).
func TestStoreV8_NomenAttestationsForeignKeyEnforced(t *testing.T) {
	store := openMemStoreInternal(t)

	err := sqlitex.Execute(store.conn,
		`INSERT INTO nomen_attestations (value, scheme, entity_key, source_id) VALUES (?1, ?2, ?3, ?4)`,
		&sqlitex.ExecOptions{Args: []any{"grok-beta", "alias", "grok@4.20", "no-such-source"}})
	if err == nil {
		t.Error(
			"orphan nomen_attestations insert was ACCEPTED; foreign keys are not enforced.\n" +
				"  What: a nomen_attestations row referencing a non-existent source_id was allowed\n" +
				"  Why: PRAGMA foreign_keys=ON is not set on the connection (or the FK clause is missing)\n" +
				"  Where: store.go OpenStore (pragma) / nomenAttestationsTableSQL (source_id FK clause)\n" +
				"  How to fix: set PRAGMA foreign_keys=ON before migrations and keep the nomen_attestations source_id FK",
		)
	}
	if got := countRows(t, store.conn, "nomen_attestations"); got != 0 {
		t.Errorf("after a rejected FK insert, nomen_attestations has %d rows, want 0", got)
	}

	// A fully parented attestation must insert once its source is registered.
	seedModelsDevSource(t, store)
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO nomina (value, scheme, entity_key, status) VALUES ('grok-beta', 'alias', 'grok@4.20', 'admitted')`, nil); err != nil {
		t.Fatalf("seed nomina parent row: %v", err)
	}
	if err := sqlitex.Execute(store.conn,
		`INSERT INTO nomen_attestations (value, scheme, entity_key, source_id) VALUES (?1, ?2, ?3, ?4)`,
		&sqlitex.ExecOptions{Args: []any{"grok-beta", "alias", "grok@4.20", string(DataSourceModelsDev)}}); err != nil {
		t.Fatalf("valid nomen_attestations insert (registered source) was rejected: %v", err)
	}
	if got := countRows(t, store.conn, "nomen_attestations"); got != 1 {
		t.Errorf("nomen_attestations row count = %d, want 1 after the parented insert", got)
	}
}
