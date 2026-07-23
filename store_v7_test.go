package bestiary

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// createV6DB writes a genuine v6-schema SQLite database to path: schema_meta
// (version=6), the full v6 models table (v4 shape + the eight v6 instance-level
// columns, but NO region columns), the v6 provenance + metadata tables, a seeded
// data source, and one models row. It deliberately has NO nomina table and NO region
// columns — those are exactly what the v6→v7 migration must add. It uses the
// production DDL constants so the fixture is a faithful v6 database.
func createV6DB(t *testing.T, path string) {
	t.Helper()
	conn, err := sqlite.OpenConn(path)
	if err != nil {
		t.Fatalf("createV6DB: open %s: %v", path, err)
	}
	defer conn.Close()

	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		t.Fatalf("createV6DB: enable foreign_keys: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		t.Fatalf("createV6DB: create schema_meta: %v", err)
	}
	if err := sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{6}}); err != nil {
		t.Fatalf("createV6DB: insert schema version: %v", err)
	}
	// v6 models shape: the v4 table plus the eight v6 instance-level columns, but NOT
	// the v7 region columns.
	if err := sqlitex.ExecuteTransient(conn, v4Schema, nil); err != nil {
		t.Fatalf("createV6DB: create models table: %v", err)
	}
	if err := sqlitex.ExecuteTransient(conn, v4IndexSQL, nil); err != nil {
		t.Fatalf("createV6DB: create index: %v", err)
	}
	for _, col := range modelV6Columns {
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			t.Fatalf("createV6DB: add v6 column %q: %v", col.name, err)
		}
	}
	if err := createProvenanceTablesV6(conn); err != nil {
		t.Fatalf("createV6DB: create v6 provenance tables: %v", err)
	}
	// Seed a data source and one models row (no region column exists yet).
	if err := sqlitex.Execute(conn,
		`INSERT INTO data_sources (data_source_id, uri, canonical_name) VALUES ('models.dev','https://models.dev/api.json','models.dev')`, nil); err != nil {
		t.Fatalf("createV6DB: seed data_sources: %v", err)
	}
	if err := sqlitex.Execute(conn,
		`INSERT INTO models (model_id, provider, display_name, last_synced) VALUES ('m1','p1','M1','2026-01-01T00:00:00Z')`, nil); err != nil {
		t.Fatalf("createV6DB: seed models row: %v", err)
	}
}

// TestStoreMigrate_V6toV7 is the v6→v7 migration + self-heal fence: opening a genuine
// v6 database advances schema_meta to 7, adds the region columns (an existing row
// backfills to RegionNone), and creates the nomina naming table — all with zero data
// loss on the seeded row.
func TestStoreMigrate_V6toV7(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v6.db")
	createV6DB(t, dbPath)

	// Precondition: the fixture genuinely lacks the v7 additions.
	pre, err := sqlite.OpenConn(dbPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	preCols, _ := tableColumnSet(pre, "models")
	if preCols["region"] {
		t.Fatal("fixture already has a region column; not a genuine v6 fixture")
	}
	if hasNomina, _ := tableExists(pre, "nomina"); hasNomina {
		t.Fatal("fixture already has a nomina table; not a genuine v6 fixture")
	}
	pre.Close()

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore (v6→v7): %v", err)
	}
	defer store.Close()

	if v, _ := getSchemaVersion(store.conn); v != currentSchemaVersion {
		t.Errorf("schema version = %d, want %d", v, currentSchemaVersion)
	}
	cols, _ := tableColumnSet(store.conn, "models")
	if !cols["region"] || !cols["region_raw"] {
		t.Error("region columns not added by the v6→v7 migration")
	}
	if hasNomina, _ := tableExists(store.conn, "nomina"); !hasNomina {
		t.Error("nomina table not created by the v6→v7 migration")
	}
	// Zero data loss: the seeded row survives and backfills to RegionNone.
	m, err := store.QueryModel(context.Background(), "m1")
	if err != nil {
		t.Fatalf("QueryModel(m1): %v", err)
	}
	if m.ID != "m1" || m.Region != RegionNone {
		t.Errorf("migrated row = {ID:%q Region:%v}, want {m1 unspecified}", m.ID, m.Region)
	}
}

// TestStore_RegionRoundTrip verifies the region column persists and reads back through
// UpsertModels / QueryModel, for a named member and for the fail-safe RegionOther+raw.
func TestStore_RegionRoundTrip(t *testing.T) {
	store := openMemStoreInternal(t)
	models := []ModelInfo{
		{ID: "eu.anthropic.claude", Provider: "anthropic", DisplayName: "c", Region: RegionEU},
		{ID: "ca.anthropic.claude", Provider: "anthropic", DisplayName: "c2", Region: RegionOther, RegionRaw: "ca"},
		{ID: "plain-model", Provider: "anthropic", DisplayName: "c3"},
	}
	if err := store.UpsertModels(context.Background(), models); err != nil {
		t.Fatalf("UpsertModels: %v", err)
	}
	got, err := store.QueryModels(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("QueryModels: %v", err)
	}
	byID := map[ModelID]ModelInfo{}
	for _, m := range got {
		byID[m.ID] = m
	}
	if byID["eu.anthropic.claude"].Region != RegionEU {
		t.Errorf("eu region not round-tripped: %v", byID["eu.anthropic.claude"].Region)
	}
	if r := byID["ca.anthropic.claude"]; r.Region != RegionOther || r.RegionRaw != "ca" {
		t.Errorf("RegionOther+raw not round-tripped: region=%v raw=%q", r.Region, r.RegionRaw)
	}
	if byID["plain-model"].Region != RegionNone {
		t.Errorf("plain model region = %v, want unspecified", byID["plain-model"].Region)
	}
}

// TestStore_NominaRoundTrip persists a minted nomen set and reads it back through the v8
// schema. It is the SOLE falsifiability guard for §5a's "zero data loss" under the v8
// nomen_attestations child table: it asserts the FULL per-attestation set
// (SourceURL+Source+Authority+Method+IngestedAt) survives an UpsertNomina→QueryNomina
// round-trip — INCLUDING (a) a curated-authored Authority that differs from the
// scheme/source default (the case the removed v7 single-attestation bridge could NOT
// carry — it always read the default back) and (b) a multi-attestation nomen (two
// independent attesters on one triple, which the single fused v7 column pair could not
// hold at all). The source_id FK requires both DataSources to be registered first.
func TestStore_NominaRoundTrip(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()
	seedModelsDevSource(t, store)
	seedCuratedSource(t, store)

	ref := EntityRef{Family: "grok", Version: "4.20", Modifier: []string{"reasoning"}}

	// The lossy-edge attestation: authored with Authority=Secondary, which is NOT the
	// DataSourceCurated scheme/source default (Primary). The v7 bridge derived authority
	// from (scheme, source) on read and would have discarded this override, reading back
	// Primary; the v8 child table persists the exact authored value.
	lossyEdge := NomenAttestation{
		SourceURL:  "https://web.archive.org/web/2026/https://docs.x.ai/docs/models",
		Source:     DataSourceCurated,
		Authority:  AuthoritySecondary,
		Method:     IngestMethodCurated,
		IngestedAt: "2026-07-20T00:00:00Z",
	}
	// A genuinely multi-attested canonical nomen: two independent attesters on ONE triple
	// (both from models.dev but differing in every other field, so neither dedups nor
	// collapses). The v7 fused columns could hold only one; v8 round-trips both.
	multi := []NomenAttestation{
		{Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodSelfMinted},
		{SourceURL: "https://docs.x.ai/docs/models", Source: DataSourceModelsDev, Authority: AuthoritySecondary, Method: IngestMethodHarvested, IngestedAt: "2026-07-19T00:00:00Z"},
	}
	in := []Nomen{
		{Value: "grok@4.20{reasoning}", Scheme: NomenSchemeCanonical, Status: AcceptabilityPreferred, ResolvesTo: ref, Attestations: multi},
		{Value: "grok-4.20-0309-reasoning", Scheme: NomenSchemeProviderID, Status: AcceptabilityAdmitted, ResolvesTo: ref, Attestations: []NomenAttestation{{Source: DataSourceModelsDev}}},
		{Value: "grok-beta", Scheme: NomenSchemeAlias, Status: AcceptabilityAdmitted, ResolvesTo: ref, Attestations: []NomenAttestation{lossyEdge}},
	}
	if err := store.UpsertNomina(ctx, in); err != nil {
		t.Fatalf("UpsertNomina: %v", err)
	}
	out, err := store.QueryNomina(ctx)
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("QueryNomina returned %d rows, want %d", len(out), len(in))
	}
	// Re-persisting the same set is idempotent (parent OR IGNORE + delete-then-insert
	// children): no duplicate parent rows, no duplicate attestations.
	if err := store.UpsertNomina(ctx, in); err != nil {
		t.Fatalf("UpsertNomina (2nd): %v", err)
	}
	out2, _ := store.QueryNomina(ctx)
	if len(out2) != len(in) {
		t.Errorf("after re-upsert QueryNomina = %d rows, want idempotent %d", len(out2), len(in))
	}

	byValue := map[string]Nomen{}
	for _, n := range out {
		byValue[n.Value] = n
	}

	// (a) The lossy-edge case: grok-beta's single curated attestation survives EXACTLY,
	// including the non-default Authority=Secondary the v7 bridge could not carry.
	gb, ok := byValue["grok-beta"]
	if !ok {
		t.Fatal("grok-beta claim missing after round-trip")
	}
	if len(gb.Attestations) != 1 {
		t.Fatalf("grok-beta carries %d attestations, want 1", len(gb.Attestations))
	}
	if got := gb.Attestations[0]; got != lossyEdge {
		t.Errorf("grok-beta attestation not round-tripped losslessly:\n got  %+v\n want %+v", got, lossyEdge)
	}
	if gb.ResolvesTo.String() != "grok@4.20{reasoning}" {
		t.Errorf("grok-beta ResolvesTo not reconstructed: %q", gb.ResolvesTo.String())
	}

	// (b) The multi-attestation case: the canonical nomen round-trips BOTH attesters,
	// sorted by the total key (sortAndDedupAttestations). Compare as sets.
	can, ok := byValue["grok@4.20{reasoning}"]
	if !ok {
		t.Fatal("canonical nomen missing after round-trip")
	}
	if len(can.Attestations) != 2 {
		t.Fatalf("canonical nomen carries %d attestations, want 2", len(can.Attestations))
	}
	wantSet := map[NomenAttestation]bool{multi[0]: true, multi[1]: true}
	for _, at := range can.Attestations {
		if !wantSet[at] {
			t.Errorf("canonical nomen has an unexpected attestation after round-trip: %+v", at)
		}
		delete(wantSet, at)
	}
	if len(wantSet) != 0 {
		t.Errorf("canonical nomen dropped attestation(s) on round-trip: %+v", wantSet)
	}
}

// TestStore_NominaHomonymyPositiveFence is the STORE-side homonymy positive fence: N
// rows sharing one Value but resolving to DISTINCT entities are all persisted (the PK
// admits them) and all read back — the store never collapses a homonym.
func TestStore_NominaHomonymyPositiveFence(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()
	seedModelsDevSource(t, store)

	const value = "shared-spelling"
	in := []Nomen{
		{Value: value, Scheme: NomenSchemeProviderID, Status: AcceptabilityAdmitted, ResolvesTo: EntityRef{Family: "grok", Version: "1"}, Attestations: []NomenAttestation{{Source: DataSourceModelsDev}}},
		{Value: value, Scheme: NomenSchemeProviderID, Status: AcceptabilityAdmitted, ResolvesTo: EntityRef{Family: "grok", Version: "2"}, Attestations: []NomenAttestation{{Source: DataSourceModelsDev}}},
		{Value: value, Scheme: NomenSchemeProviderID, Status: AcceptabilityAdmitted, ResolvesTo: EntityRef{Family: "claude", Variant: "opus"}, Attestations: []NomenAttestation{{Source: DataSourceModelsDev}}},
	}
	if err := store.UpsertNomina(ctx, in); err != nil {
		t.Fatalf("UpsertNomina: %v", err)
	}
	out, err := store.QueryNomina(ctx)
	if err != nil {
		t.Fatalf("QueryNomina: %v", err)
	}
	n := 0
	entities := map[string]bool{}
	for _, row := range out {
		if row.Value == value {
			n++
			entities[row.ResolvesTo.String()] = true
		}
	}
	if n != 3 {
		t.Errorf("homonym %q persisted %d rows, want all 3", value, n)
	}
	if len(entities) != 3 {
		t.Errorf("homonym %q resolved to %d distinct entities, want 3", value, len(entities))
	}
}

// TestStoreV7_NominaForeignKeyEnforced verifies, on a FRESH in-memory v7 database,
// that (1) the nomina table exists on the fresh-DB path, (2) the source_id
// foreign_keys pragma actually bites — UpsertNomina of a nomen whose Source has no
// data_sources parent is REJECTED with the wrapped FK error and no partial write — and
// (3) the same nomen inserts once its DataSource is registered. The rejection arm is
// the guard that PRAGMA foreign_keys=ON is set and the nominaTableSQL REFERENCES clause
// is intact; without it SQLite silently accepts the orphan and corrupts nomen
// provenance. Sibling of TestStoreV5_ForeignKeysEnforced (entity_source).
func TestStoreV7_NominaForeignKeyEnforced(t *testing.T) {
	store := openMemStoreInternal(t)
	ctx := context.Background()

	// (1) Fresh-DB path must have created the nomina table.
	if exists, err := tableExists(store.conn, "nomina"); err != nil {
		t.Fatalf("tableExists(nomina): %v", err)
	} else if !exists {
		t.Fatal("fresh v7 DB is missing the nomina table; the fresh-DB migration path must create it")
	}

	ref := EntityRef{Family: "grok", Version: "4.20", Modifier: []string{"reasoning"}}

	// (2) Orphan source: the referenced DataSource is NOT registered (deliberately no
	// seed), so the source_id FK must reject the write.
	orphan := []Nomen{{
		Value: "grok-beta", Scheme: NomenSchemeAlias, Status: AcceptabilityAdmitted,
		ResolvesTo: ref, Attestations: []NomenAttestation{{SourceURL: "https://docs.x.ai/docs/models", Source: DataSourceID("no-such-source")}},
	}}
	err := store.UpsertNomina(ctx, orphan)
	if err == nil {
		t.Error(
			"orphan nomina insert was ACCEPTED; foreign keys are not enforced.\n" +
				"  What: a nomina row referencing a non-existent source_id was allowed\n" +
				"  Why: PRAGMA foreign_keys=ON is not set on the connection (or the FK clause is missing)\n" +
				"  Where: store.go OpenStore (pragma) / nominaTableSQL (source_id FK clause)\n" +
				"  How to fix: set PRAGMA foreign_keys=ON before migrations and keep the nomina source_id FK",
		)
	} else if !strings.Contains(err.Error(), "no-such-source") {
		t.Errorf("FK rejection error is not actionable about the offending source: %v", err)
	}
	// The rejected write must leave the table empty (no partial/silent insert).
	if got := countRows(t, store.conn, "nomina"); got != 0 {
		t.Errorf("after a rejected FK insert, nomina has %d rows, want 0", got)
	}

	// (3) A fully parented nomen must insert: register the source, then persist a nomen
	// whose Source resolves.
	seedModelsDevSource(t, store)
	valid := []Nomen{{
		Value: "grok-beta", Scheme: NomenSchemeAlias, Status: AcceptabilityAdmitted,
		ResolvesTo: ref, Attestations: []NomenAttestation{{SourceURL: "https://docs.x.ai/docs/models", Source: DataSourceModelsDev}},
	}}
	if err := store.UpsertNomina(ctx, valid); err != nil {
		t.Fatalf("valid nomina insert (registered source) was rejected: %v", err)
	}
	if got := countRows(t, store.conn, "nomina"); got != 1 {
		t.Errorf("nomina row count = %d, want 1 after the parented insert", got)
	}
}

// openMemStoreInternal is the internal-package twin of the external openMemStore.
func openMemStoreInternal(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatalf("OpenStore(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedModelsDevSource writes the models.dev DataSource so nomina/entity FKs resolve.
func seedModelsDevSource(t *testing.T, store *Store) {
	t.Helper()
	ds := DataSource{ID: DataSourceModelsDev, URI: "https://models.dev/api.json", CanonicalName: "models.dev"}
	ingest := DatasetIngested{SourceID: DataSourceModelsDev, IngestedAt: "2026-07-20T00:00:00Z", ParserSchema: 3}
	if err := store.UpsertDataSources(context.Background(), []DataSource{ds}, []DatasetIngested{ingest}); err != nil {
		t.Fatalf("seed models.dev source: %v", err)
	}
}
