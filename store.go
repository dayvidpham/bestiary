package bestiary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// currentSchemaVersion is the schema version this build expects.
// Bump this whenever a migration is added.
//
// NOTE: this is the SQLite store migration version (models cache + BCNF
// provenance tables). It is DISTINCT from BestiarySchemaVersion in version.go,
// which versions the public JSON output schema; do not conflate the two.
const currentSchemaVersion = 9

// schemaMetaSQL creates the schema_meta table used to track migration state.
// Safe to run on any existing database (CREATE TABLE IF NOT EXISTS).
const schemaMetaSQL = `CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL);`

// schemaSQL defines the current (v6) models table schema. Used only for fresh
// databases; existing databases go through migrateSchema. The instance-level
// models.dev fields at the tail (description … cost_tiers) are appended AFTER
// last_synced so the fresh column order matches the migrateToV6 ALTER TABLE ADD
// COLUMN order (SQLite appends new columns), keeping fresh == migrated.
const schemaSQL = `CREATE TABLE IF NOT EXISTS models (
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
    description       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT '',
    status_raw        TEXT NOT NULL DEFAULT '',
    reasoning_options TEXT NOT NULL DEFAULT '',
    cost_input_audio  REAL,
    cost_output_audio REAL,
    cost_context_over_200k TEXT NOT NULL DEFAULT '',
    cost_tiers        TEXT NOT NULL DEFAULT '',
    region            TEXT NOT NULL DEFAULT '',
    region_raw        TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (model_id, provider)
);`

// modelV6Column is one instance-level column v6 adds to the models table: its
// NAME (for presence checks) paired with the ALTER TABLE ADD COLUMN that creates
// it. Carrying the name lets ensureModelColumnsV6 add ONLY the columns a table is
// missing, which makes the addition idempotent and self-healing.
type modelV6Column struct {
	name string
	sql  string
}

// modelV6Columns is the ordered list of the eight instance-level models.dev
// columns v6 adds to the models table. Every column is either a NOT NULL TEXT with
// a constant empty-string default or a nullable REAL, both of which SQLite ADD
// COLUMN accepts. The order matches the schemaSQL tail so a migrated models table
// is column-order-identical to a fresh one.
var modelV6Columns = []modelV6Column{
	{"description", `ALTER TABLE models ADD COLUMN description TEXT NOT NULL DEFAULT ''`},
	{"status", `ALTER TABLE models ADD COLUMN status TEXT NOT NULL DEFAULT ''`},
	{"status_raw", `ALTER TABLE models ADD COLUMN status_raw TEXT NOT NULL DEFAULT ''`},
	{"reasoning_options", `ALTER TABLE models ADD COLUMN reasoning_options TEXT NOT NULL DEFAULT ''`},
	{"cost_input_audio", `ALTER TABLE models ADD COLUMN cost_input_audio REAL`},
	{"cost_output_audio", `ALTER TABLE models ADD COLUMN cost_output_audio REAL`},
	{"cost_context_over_200k", `ALTER TABLE models ADD COLUMN cost_context_over_200k TEXT NOT NULL DEFAULT ''`},
	{"cost_tiers", `ALTER TABLE models ADD COLUMN cost_tiers TEXT NOT NULL DEFAULT ''`},
}

// ensureModelColumnsV6 adds any of the eight v6 instance-level models columns that
// the models table is currently missing, ALTERing only the absent ones (presence
// is read from pragma_table_info). It is idempotent and self-healing: it is safe
// to run when all columns already exist (a no-op), when only some exist (an
// interrupted multi-ALTER migration — the group is NOT atomic across process
// death), and when NONE exist (a v5→v6 upgrade or an intermediate-v6 dev cache
// whose schema_meta reads 6 but whose models table predates these columns). It
// does nothing when the models table is absent, leaving table creation to the
// fresh/migration paths. It manages no transaction of its own, so it composes both
// inside migrateToV6's transaction and standalone from OpenStore.
func ensureModelColumnsV6(conn *sqlite.Conn) error {
	existing, err := tableColumnSet(conn, "models")
	if err != nil {
		return fmt.Errorf("read models columns: %w", err)
	}
	if len(existing) == 0 {
		// No models table yet — nothing to heal.
		return nil
	}
	for _, col := range modelV6Columns {
		if existing[col.name] {
			continue
		}
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			return fmt.Errorf("add missing models column %q: %w\n"+
				"  What: adding an instance-level models column failed\n"+
				"  Why: ALTER TABLE ADD COLUMN was rejected on a models table missing the column\n"+
				"  Where: store.go ensureModelColumnsV6\n"+
				"  How to fix: inspect the models table schema; delete the cache to rebuild it if corrupt",
				col.name, err)
		}
	}
	return nil
}

// entityMetadataV6Columns is the ordered list of entity_metadata columns that a
// LATER v6 revision adds to a table an earlier v6 build already created. Today it is
// just raw_family (the upstream family provenance). It mirrors modelV6Columns so the
// entity_metadata dimension self-heals with the same discipline as the models table.
var entityMetadataV6Columns = []modelV6Column{
	{"raw_family", `ALTER TABLE entity_metadata ADD COLUMN raw_family TEXT NOT NULL DEFAULT ''`},
}

// ensureEntityMetadataColumnsV6 adds any entity_metadata column in
// entityMetadataV6Columns that the table is currently missing, ALTERing only the
// absent ones (presence read from pragma_table_info). It is the entity_metadata
// sibling of ensureModelColumnsV6: idempotent and self-healing, so it is a no-op
// when the column already exists (a fresh v6 table, or a re-run) and backfills it on
// an intermediate-v6 dev cache whose schema_meta reads 6 but whose entity_metadata
// table predates the column. It does nothing when the entity_metadata table is
// absent, leaving creation to the fresh/migration paths, and manages no transaction
// of its own so it composes inside migrateToV6's transaction and standalone from
// OpenStore.
func ensureEntityMetadataColumnsV6(conn *sqlite.Conn) error {
	existing, err := tableColumnSet(conn, "entity_metadata")
	if err != nil {
		return fmt.Errorf("read entity_metadata columns: %w", err)
	}
	if len(existing) == 0 {
		// No entity_metadata table yet — nothing to heal.
		return nil
	}
	for _, col := range entityMetadataV6Columns {
		if existing[col.name] {
			continue
		}
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			return fmt.Errorf("add missing entity_metadata column %q: %w\n"+
				"  What: adding an entity_metadata provenance column failed\n"+
				"  Why: ALTER TABLE ADD COLUMN was rejected on an entity_metadata table missing the column\n"+
				"  Where: store.go ensureEntityMetadataColumnsV6\n"+
				"  How to fix: inspect the entity_metadata table schema; delete the cache to rebuild it if corrupt",
				col.name, err)
		}
	}
	return nil
}

// modelV7Columns is the ordered list of the two per-instance region columns v7 adds
// to the models table: region (the Region String() token — the enum-column-as-TEXT
// precedent already used for status) and region_raw (the fail-safe RegionOther
// carrier). Both are NOT NULL TEXT with an empty-string default, which SQLite ADD
// COLUMN accepts. The order matches the schemaSQL tail so a migrated models table is
// column-order-identical to a fresh one.
var modelV7Columns = []modelV6Column{
	{"region", `ALTER TABLE models ADD COLUMN region TEXT NOT NULL DEFAULT ''`},
	{"region_raw", `ALTER TABLE models ADD COLUMN region_raw TEXT NOT NULL DEFAULT ''`},
}

// ensureModelColumnsV7 adds any of the v7 region columns the models table is missing,
// ALTERing only the absent ones (presence read from pragma_table_info). It is the v7
// sibling of ensureModelColumnsV6: idempotent and self-healing, so it is a no-op when
// the columns already exist (a fresh v7 table, or a re-run), backfills them on a
// v6→v7 upgrade, and heals an intermediate-v7 dev cache whose schema_meta reads 7 but
// whose models table predates the columns. It does nothing when the models table is
// absent, and manages no transaction of its own so it composes inside migrateToV7's
// transaction and standalone from OpenStore.
func ensureModelColumnsV7(conn *sqlite.Conn) error {
	existing, err := tableColumnSet(conn, "models")
	if err != nil {
		return fmt.Errorf("read models columns: %w", err)
	}
	if len(existing) == 0 {
		// No models table yet — nothing to heal.
		return nil
	}
	for _, col := range modelV7Columns {
		if existing[col.name] {
			continue
		}
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			return fmt.Errorf("add missing models column %q: %w\n"+
				"  What: adding a per-instance region column failed\n"+
				"  Why: ALTER TABLE ADD COLUMN was rejected on a models table missing the column\n"+
				"  Where: store.go ensureModelColumnsV7\n"+
				"  How to fix: inspect the models table schema; delete the cache to rebuild it if corrupt",
				col.name, err)
		}
	}
	return nil
}

// tableColumnSet returns the set of column names of table via pragma_table_info.
// An absent table yields an empty (non-nil) set, so callers distinguish it from a
// present-but-column-short table.
func tableColumnSet(conn *sqlite.Conn, table string) (map[string]bool, error) {
	cols := map[string]bool{}
	err := sqlitex.Execute(conn, `SELECT name FROM pragma_table_info(?1)`, &sqlitex.ExecOptions{
		Args: []any{table},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			cols[stmt.GetText("name")] = true
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	return cols, nil
}

// modelColumns is the ordered column list shared by every models SELECT so the
// five read paths (QueryModels, QueryModel, QueryModelsByID, QueryByCanonical)
// can never drift when a column is added. scanModelInfo reads by column NAME, so
// this order is independent of the table's physical column order.
const modelColumns = `model_id, provider, display_name, raw_family, family, variant, version, date,
	context_window, max_output,
	reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
	cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
	release_date, knowledge,
	modalities_input, modalities_output,
	last_synced,
	description, status, status_raw, reasoning_options,
	cost_input_audio, cost_output_audio, cost_context_over_200k, cost_tiers,
	region, region_raw`

// indexSQL creates the canonical lookup index used by QueryByCanonical.
// The (family, variant, version) prefix is used for all non-empty canonical
// axis predicates; provider is included to support composite-key scan pruning.
// Safe to run on any database that already has the models table (v4+).
const indexSQL = `CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, version, provider);`

// indexV3SQL is the v3 canonical index, used only by migrateToV3 to create
// a temporary (family, variant, provider) index on databases being upgraded
// from v2 to v3. The subsequent migrateToV4 call will drop this index and
// recreate it as indexSQL (adding version).
const indexV3SQL = `CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, provider);`

// --- v5 BCNF data-source provenance schema ---
//
// Four additive tables form the data-source provenance core. They are created
// on both the fresh-DB path and the v4→v5 upgrade path via createProvenanceTables
// so a fresh v5 database is never left with schema_meta=5 but no provenance
// tables. Foreign keys are enforced only because OpenStore sets
// `PRAGMA foreign_keys = ON` per-connection before migrations; without that
// pragma these FK clauses would be decorative.

// dataSourcesTableSQL is the BCNF dimension of originating data sources.
// uri is a second candidate key (each source has a distinct fetch endpoint),
// pinned UNIQUE so a duplicate-endpoint row is rejected.
const dataSourcesTableSQL = `CREATE TABLE IF NOT EXISTS data_sources (
    data_source_id TEXT PRIMARY KEY,
    uri            TEXT NOT NULL,
    canonical_name TEXT NOT NULL,
    UNIQUE(uri)
);`

// datasetIngestedTableSQL is the v5 shape of dataset_ingested: it records a
// single current ingest per source (primary key data_source_id). It carries NO
// uri (a transitive dependency reached via the data_sources FK join), which is
// the BCNF normalization. This constant is retained for the v4 to v5 migration
// path (createProvenanceTables) so a v5 database is faithfully reproduced before
// migrateToV6 widens the primary key; fresh v6 databases use
// datasetIngestedV6TableSQL instead.
const datasetIngestedTableSQL = `CREATE TABLE IF NOT EXISTS dataset_ingested (
    data_source_id TEXT PRIMARY KEY,
    ingested_at    TEXT NOT NULL,
    parser_schema  INTEGER NOT NULL,
    FOREIGN KEY (data_source_id) REFERENCES data_sources(data_source_id)
);`

// entitiesTableSQL is the entity dimension that entity_source.entity_key
// references. The decomposed columns default to the empty string so a minimal
// FK-target row (entity_key only) is valid.
const entitiesTableSQL = `CREATE TABLE IF NOT EXISTS entities (
    entity_key TEXT PRIMARY KEY,
    family     TEXT NOT NULL DEFAULT '',
    variant    TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    param_size TEXT NOT NULL DEFAULT ''
);`

// entitySourceTableSQL is the BCNF join table relating an entity to each data
// source that attests it. The composite primary key (entity_key, data_source_id)
// allows an entity attested by N sources to hold N rows; both columns are
// foreign keys, so an orphan attestation (missing entity or missing source) is
// rejected when foreign_keys is ON.
const entitySourceTableSQL = `CREATE TABLE IF NOT EXISTS entity_source (
    entity_key     TEXT NOT NULL,
    data_source_id TEXT NOT NULL,
    PRIMARY KEY (entity_key, data_source_id),
    FOREIGN KEY (entity_key) REFERENCES entities(entity_key),
    FOREIGN KEY (data_source_id) REFERENCES data_sources(data_source_id)
);`

// --- v6 append-only ingest log + entity-metadata schema ---
//
// v6 widens dataset_ingested from a single current ingest per source to an
// append-only history (composite primary key), and adds three entity-metadata
// tables for the provider-agnostic models.dev model facts. All string columns
// default to the empty string so a minimal row is valid; those defaults are
// written as prose in these comments (a doubled straight quote in a comment is
// rewritten by gofmt, so the literal glyph is avoided).

// datasetIngestedV6TableSQL is the v6 shape of dataset_ingested: an append-only
// ingest history keyed by the composite primary key (data_source_id,
// ingested_at). A source therefore carries one row per distinct ingest instant
// instead of a single mutable current row; the current ingest is the row with the
// maximum ingested_at. It still carries NO uri (reached via the data_sources FK
// join). Fresh v6 databases create this shape directly; upgrading databases reach
// it through migrateToV6's dedicated table-recreate.
const datasetIngestedV6TableSQL = `CREATE TABLE IF NOT EXISTS dataset_ingested (
    data_source_id TEXT NOT NULL REFERENCES data_sources(data_source_id),
    ingested_at    TEXT NOT NULL,
    parser_schema  INTEGER NOT NULL,
    PRIMARY KEY (data_source_id, ingested_at)
);`

// entityMetadataTableSQL is the entity-metadata dimension: the provider-agnostic
// model facts from the models.dev models.json view, keyed by the stable
// metadata_id. It carries NO status column (status is an api.json / instance-level
// fact, absent from models.json). source_id is a foreign key into data_sources
// (the ingest attestation). name / description / license / last_synced default to
// the empty string.
// raw_family is the upstream models.json family verbatim (EntityMetadata.RawFamily),
// persisted so a synced/cached row round-trips the same raw-family signal the baked
// rows carry — the metadata<->entity join keys its family-presence gate off it, so a
// row loaded from the store must not lose it and degrade to the mechanical family. It
// is LAST in the column list to match the ALTER TABLE ADD COLUMN position the
// self-heal (ensureEntityMetadataColumnsV6) uses on an intermediate-v6 cache, so a
// fresh and a healed table are column-order-identical.
const entityMetadataTableSQL = `CREATE TABLE IF NOT EXISTS entity_metadata (
    metadata_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    license     TEXT NOT NULL DEFAULT '',
    source_id   TEXT NOT NULL REFERENCES data_sources(data_source_id),
    last_synced TEXT NOT NULL DEFAULT '',
    raw_family  TEXT NOT NULL DEFAULT ''
);`

// metadataBenchmarksTableSQL is the benchmark-claims child table of
// entity_metadata. It deliberately has NO primary key: the benchmark rows are a
// replaceable set owned by their parent metadata_id, refreshed by delete-then-
// insert inside the UpsertEntityMetadata transaction, so a row's identity is its
// insertion order (rowid) within a parent. score defaults to 0; every string
// column except name defaults to the empty string.
const metadataBenchmarksTableSQL = `CREATE TABLE IF NOT EXISTS metadata_benchmarks (
    metadata_id TEXT NOT NULL REFERENCES entity_metadata(metadata_id),
    name        TEXT NOT NULL,
    version     TEXT NOT NULL DEFAULT '',
    variant     TEXT NOT NULL DEFAULT '',
    dataset     TEXT NOT NULL DEFAULT '',
    harness     TEXT NOT NULL DEFAULT '',
    metric      TEXT NOT NULL DEFAULT '',
    score       REAL NOT NULL DEFAULT 0,
    score_raw   TEXT NOT NULL DEFAULT '',
    source_url  TEXT NOT NULL DEFAULT '',
    date        TEXT NOT NULL DEFAULT ''
);`

// metadataLinksTableSQL is the reference-links child table of entity_metadata. It
// also has NO primary key for the same replaceable-set reason as
// metadata_benchmarks. The type column stores the LinkType string form; type_raw
// carries the verbatim upstream token only when the type is the other bucket.
// label / type / type_raw default to the empty string; url is required.
const metadataLinksTableSQL = `CREATE TABLE IF NOT EXISTS metadata_links (
    metadata_id TEXT NOT NULL REFERENCES entity_metadata(metadata_id),
    label       TEXT NOT NULL DEFAULT '',
    url         TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT '',
    type_raw    TEXT NOT NULL DEFAULT ''
);`

// nominaTableSQL is the v7 naming table: one row per recorded naming of an entity,
// keyed by the composite primary key (value, scheme, entity_key) so a single spelling
// may resolve to several entities (homonymy — the PK admits N rows for one value). It
// carries claim attribution split into two provenance levels: source_url (WHO asserts
// the naming) distinct from source_id (WHICH ingest we read it from, a foreign key
// into data_sources). entity_key is NOT an FK — the entities table is a stub dimension
// and a minted canonical/provider-id nomen may name an entity never written to it;
// this is the documented deviation the plan ratified. status stores the
// AcceptabilityRating token; scheme stores the NomenScheme token.
const nominaTableSQL = `CREATE TABLE IF NOT EXISTS nomina (
    value       TEXT NOT NULL,
    scheme      TEXT NOT NULL,
    entity_key  TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'admitted',
    source_url  TEXT NOT NULL DEFAULT '',
    source_id   TEXT NOT NULL REFERENCES data_sources(data_source_id),
    PRIMARY KEY (value, scheme, entity_key)
);`

// createNominaTable creates the v7 nomina table. It is CREATE TABLE IF NOT EXISTS, so
// it is idempotent and safe on the fresh-DB v7 path and from migrateToV7 alike. It
// references data_sources, so callers create it AFTER the provenance dimensions.
//
// The v7 shape (single fused source_url/source_id pair) is retained ONLY so the
// v6→v7→v8 migration chain can build the v7 table before migrateToV8 recreates it
// without those columns. Fresh v8 databases skip it and create the v8 shape directly.
func createNominaTable(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, nominaTableSQL, nil); err != nil {
		return fmt.Errorf("create nomina table: %w", err)
	}
	return nil
}

// nominaV8TableSQL is the v8 nomina PARENT table: the multi-attestation lift moves
// provenance out of the fused source_url/source_id columns into the nomen_attestations
// child table, so the parent keeps ONLY the naming identity (value, scheme, entity_key)
// and bestiary's single editorial judgment (status). The primary key is UNCHANGED from
// v7 — (value, scheme, entity_key) — so every pre-v8 nomen key is byte-identical. It no
// longer references data_sources (the FK moves to the child); entity_key is still NOT an
// FK (the entities table is a stub dimension a minted nomen may name without a row).
const nominaV8TableSQL = `CREATE TABLE IF NOT EXISTS nomina (
    value       TEXT NOT NULL,
    scheme      TEXT NOT NULL,
    entity_key  TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'admitted',
    PRIMARY KEY (value, scheme, entity_key)
);`

// nomenAttestationsTableSQL is the v8 nomen_attestations CHILD table: the evidence set
// of a Nomen, one row per NomenAttestation (the entity_metadata→child-tables precedent,
// a replaceable set delete-then-inserted by its parent's triple). It carries the
// per-attestation provenance the fused v7 columns could not: source_url (WHO asserts),
// source_id (WHICH ingest, the FK into data_sources that moved here from the parent),
// authority (whose VOICE), method (HOW it entered), and ingested_at (committed snapshot).
// It has NO primary key — the set is owned by (value, scheme, entity_key) and joined back
// to nomina, so two sources asserting one name are two rows, never a collision.
//
// archived_url (v9) is LAST in the declaration order deliberately: SQLite ALTER TABLE
// ADD COLUMN appends, so a fresh v9 table and a migrated v8 one are column-order
// identical (the schemaSQL/modelV6Columns discipline). It is a durability aid for
// source_url, not a provenance level of its own — see NomenAttestation.ArchivedURL.
const nomenAttestationsTableSQL = `CREATE TABLE IF NOT EXISTS nomen_attestations (
    value        TEXT NOT NULL,
    scheme       TEXT NOT NULL,
    entity_key   TEXT NOT NULL,
    source_url   TEXT NOT NULL DEFAULT '',
    source_id    TEXT NOT NULL REFERENCES data_sources(data_source_id),
    authority    TEXT NOT NULL DEFAULT 'unknown',
    method       TEXT NOT NULL DEFAULT 'unknown',
    ingested_at  TEXT NOT NULL DEFAULT '',
    archived_url TEXT NOT NULL DEFAULT ''
);`

// nomenAttestationV9Columns is the ordered list of the one column v9 adds to the
// nomen_attestations table: archived_url, the archive.org snapshot of a harvested
// attestation's live source_url. It is NOT NULL TEXT with an empty-string default,
// which SQLite ADD COLUMN accepts on a populated table — so every pre-existing row
// keeps its values and gains an empty snapshot, an honest "none recorded".
var nomenAttestationV9Columns = []modelV6Column{
	{"archived_url", `ALTER TABLE nomen_attestations ADD COLUMN archived_url TEXT NOT NULL DEFAULT ''`},
}

// ensureNomenAttestationColumnsV9 adds any of the v9 columns the nomen_attestations
// table is missing, ALTERing only the absent ones (presence read from
// pragma_table_info). It is the v9 sibling of ensureModelColumnsV6/V7: idempotent and
// self-healing, so it is a no-op when the column already exists (a fresh v9 table, or
// a re-run — an unguarded second ADD COLUMN is a hard SQLite error, which is exactly
// why the guard is not optional), backfills it on a v8→v9 upgrade, and heals an
// intermediate-v9 cache whose schema_meta reads 9 but whose child table predates the
// column. It does nothing when the table is absent, leaving creation to the
// fresh/migration paths, and manages no transaction of its own so it composes inside
// migrateToV9's flow and standalone from OpenStore.
func ensureNomenAttestationColumnsV9(conn *sqlite.Conn) error {
	existing, err := tableColumnSet(conn, "nomen_attestations")
	if err != nil {
		return fmt.Errorf("read nomen_attestations columns: %w", err)
	}
	if len(existing) == 0 {
		// No nomen_attestations table yet — nothing to heal.
		return nil
	}
	for _, col := range nomenAttestationV9Columns {
		if existing[col.name] {
			continue
		}
		if err := sqlitex.ExecuteTransient(conn, col.sql, nil); err != nil {
			return fmt.Errorf("add missing nomen_attestations column %q: %w\n"+
				"  What: adding the archive-snapshot column failed\n"+
				"  Why: ALTER TABLE ADD COLUMN was rejected on a nomen_attestations table missing the column\n"+
				"  Where: store.go ensureNomenAttestationColumnsV9\n"+
				"  How to fix: inspect the nomen_attestations table schema; delete the cache to rebuild it if corrupt",
				col.name, err)
		}
	}
	return nil
}

// migrateToV9 upgrades a v8 database to v9. It is PURELY ADDITIVE — one presence-
// guarded ALTER TABLE ADD COLUMN on nomen_attestations, no table dropped, recreated or
// reordered (the migrateToV7 additive precedent, not the migrateToV6/V8 recreate one),
// so it is zero-data-loss by construction: every pre-existing row keeps every value and
// gains an empty archived_url.
func migrateToV9(conn *sqlite.Conn) error {
	if err := ensureNomenAttestationColumnsV9(conn); err != nil {
		return fmt.Errorf("v8→v9: add nomen_attestations archive column: %w", err)
	}
	return nil
}

// creatorsTableSQL is the v8 creators BCNF dimension: Family → Creator, keyed by family.
// Because Creator is functionally dependent on Family (Family → Creator), storing it on
// a models or entities row would replicate the same fact on every row (a transitive
// dependency, a BCNF violation), so it lives in its own dimension keyed by family. It is
// populated from the curated creators.json seed at sync (UpsertCreators), the data_sources
// dimension precedent — the persisted record of what the syncing binary knew.
const creatorsTableSQL = `CREATE TABLE IF NOT EXISTS creators (
    family  TEXT PRIMARY KEY,
    creator TEXT NOT NULL
);`

// createNominaTableV8 creates the v8 nomina parent table. CREATE TABLE IF NOT EXISTS, so
// idempotent and safe on the fresh-DB v8 path.
func createNominaTableV8(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, nominaV8TableSQL, nil); err != nil {
		return fmt.Errorf("create v8 nomina table: %w", err)
	}
	return nil
}

// createNomenAttestationsTable creates the v8 nomen_attestations child table. It
// references data_sources, so callers create it AFTER the provenance dimensions.
// CREATE TABLE IF NOT EXISTS, so idempotent and shared by the fresh-DB path and migrateToV8.
func createNomenAttestationsTable(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, nomenAttestationsTableSQL, nil); err != nil {
		return fmt.Errorf("create nomen_attestations table: %w", err)
	}
	return nil
}

// createCreatorsTable creates the v8 creators dimension table. CREATE TABLE IF NOT
// EXISTS, so idempotent and shared by the fresh-DB path and migrateToV8.
func createCreatorsTable(conn *sqlite.Conn) error {
	if err := sqlitex.ExecuteTransient(conn, creatorsTableSQL, nil); err != nil {
		return fmt.Errorf("create creators table: %w", err)
	}
	return nil
}

// migrateToV7 upgrades a v6 database to v7: it backfills the two per-instance region
// columns on the models table (ensureModelColumnsV7, presence-guarded so a partial
// intermediate-v7 cache heals idempotently) and creates the nomina naming table. Both
// steps are additive — no table recreation, so it is zero-data-loss. It runs inside
// migrateSchema's flow; the region self-heal and nomina creation are also reachable
// standalone from OpenStore for an intermediate-v7 cache.
func migrateToV7(conn *sqlite.Conn) error {
	if err := ensureModelColumnsV7(conn); err != nil {
		return fmt.Errorf("v6→v7: add region columns: %w", err)
	}
	if err := createNominaTable(conn); err != nil {
		return fmt.Errorf("v6→v7: create nomina table: %w", err)
	}
	return nil
}

// v7NominaAttestationKind derives the (Authority, Method) of a v7 nomina row from its
// (scheme, source) per the §3.2 defaults table. It is the migration-local carrier of the
// exact mapping the removed v7 transitional read-path used to reconstruct these on read:
// the v7 nomina row has no authority/method columns, so its (source_url, source_id)
// pair migrates into ONE nomen_attestations row whose authority/method are reconstructed
// here where derivable, else left at their Unknown zero — never guessed. This is a
// one-time best-effort backfill of pre-v8 rows; every nomen minted AFTER v8 persists its
// real per-attestation Authority/Method losslessly through the child table.
func v7NominaAttestationKind(scheme NomenScheme, source DataSourceID) (AttestationAuthority, IngestMethod) {
	// A curated ingest is Method=Curated regardless of scheme (an alias or a
	// huggingface-scheme name can both arrive through the curated layer).
	if source == DataSourceCurated {
		return AuthorityPrimary, IngestMethodCurated
	}
	switch scheme {
	case NomenSchemeCanonical:
		return AuthorityPrimary, IngestMethodSelfMinted
	case NomenSchemeProviderID:
		return AuthoritySecondary, IngestMethodHarvested
	case NomenSchemeHuggingFace:
		return AuthorityPrimary, IngestMethodHarvested
	default:
		return AuthorityUnknown, IngestMethodUnknown
	}
}

// migrateToV8 upgrades a v7 database to v8 (migrateToV6 table-recreate precedent). It
// lifts nomen provenance out of the fused nomina columns into the nomen_attestations
// child table and adds the creators BCNF dimension, all inside one transaction so a
// failure rolls back cleanly:
//
//  1. Create nomen_attestations + creators (CREATE TABLE IF NOT EXISTS — idempotent, so a
//     re-run over a partially-migrated intermediate-v8 cache is safe).
//  2. Copy each existing nomina row's (source_url, source_id) into ONE nomen_attestations
//     row, reconstructing authority/method via v7NominaAttestationKind (the §3.2 defaults,
//     the same mapping the deleted bridge used) and setting ingested_at ” — ZERO data
//     loss on the provenance the v7 columns actually held.
//  3. Recreate the nomina parent WITHOUT source_url/source_id (SQLite cannot DROP COLUMN
//     under an old engine, and the recreate mirrors migrateToV6's dataset_ingested swap),
//     copying value/scheme/entity_key/status — the primary key is unchanged, so keys stay
//     byte-identical.
//
// Step 2 MUST precede step 3: it reads the source columns off the OLD nomina table before
// the recreate drops them.
func migrateToV8(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	// Step 1: additive new tables. nomen_attestations references data_sources, which a v7
	// database already has; creators is a standalone dimension.
	if err = createNomenAttestationsTable(conn); err != nil {
		return err
	}
	if err = createCreatorsTable(conn); err != nil {
		return err
	}

	// Step 2: backfill the child table from the old fused columns BEFORE the recreate
	// drops them. Read every old row, derive its single attestation, insert it. INSERT is
	// plain (not OR IGNORE): the child set is empty on a genuine v7→v8 upgrade, and a
	// duplicate would signal a corrupt fixture worth surfacing rather than silently eating.
	type v7Row struct {
		value, scheme, entityKey, sourceURL, sourceID string
	}
	var rows []v7Row
	err = sqlitex.Execute(conn,
		`SELECT value, scheme, entity_key, source_url, source_id FROM nomina`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rows = append(rows, v7Row{
					value:     stmt.GetText("value"),
					scheme:    stmt.GetText("scheme"),
					entityKey: stmt.GetText("entity_key"),
					sourceURL: stmt.GetText("source_url"),
					sourceID:  stmt.GetText("source_id"),
				})
				return nil
			},
		})
	if err != nil {
		return fmt.Errorf("v7→v8: read v7 nomina rows for attestation backfill: %w", err)
	}
	const insAttSQL = `INSERT INTO nomen_attestations (
		value, scheme, entity_key, source_url, source_id, authority, method, ingested_at
	) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, '')`
	for i := range rows {
		r := &rows[i]
		var scheme NomenScheme
		if e := scheme.UnmarshalText([]byte(r.scheme)); e != nil {
			scheme = NomenSchemeOther
		}
		authority, method := v7NominaAttestationKind(scheme, DataSourceID(r.sourceID))
		err = sqlitex.Execute(conn, insAttSQL, &sqlitex.ExecOptions{
			Args: []any{
				r.value, r.scheme, r.entityKey, r.sourceURL, r.sourceID,
				authority.String(), method.String(),
			},
		})
		if err != nil {
			return fmt.Errorf("v7→v8: backfill attestation for nomen (value=%q, scheme=%q, entity=%q): %w",
				r.value, r.scheme, r.entityKey, err)
		}
	}

	// Step 3: recreate the nomina parent without the source columns (migrateToV6 swap
	// precedent). The new table is created under a temporary name, populated with the four
	// retained columns, then swapped in.
	const createNominaNewSQL = `CREATE TABLE nomina_new (
    value       TEXT NOT NULL,
    scheme      TEXT NOT NULL,
    entity_key  TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'admitted',
    PRIMARY KEY (value, scheme, entity_key)
)`
	if err = sqlitex.ExecuteTransient(conn, createNominaNewSQL, nil); err != nil {
		return fmt.Errorf("v7→v8: create nomina_new: %w", err)
	}
	const copyNominaSQL = `INSERT INTO nomina_new (value, scheme, entity_key, status)
        SELECT value, scheme, entity_key, status FROM nomina`
	if err = sqlitex.ExecuteTransient(conn, copyNominaSQL, nil); err != nil {
		return fmt.Errorf("v7→v8: copy nomina rows: %w", err)
	}
	if err = sqlitex.ExecuteTransient(conn, `DROP TABLE nomina`, nil); err != nil {
		return fmt.Errorf("v7→v8: drop old nomina: %w", err)
	}
	if err = sqlitex.ExecuteTransient(conn, `ALTER TABLE nomina_new RENAME TO nomina`, nil); err != nil {
		return fmt.Errorf("v7→v8: rename nomina_new to nomina: %w", err)
	}
	return nil
}

// ensureNominaV8 is the presence-guarded v8 self-heal for the nomina naming layer: it
// makes an intermediate-v8 cache (schema_meta already reads 8, but built by a build that
// predates part of the v8 shape) converge on the full v8 tables. It creates
// nomen_attestations + creators if absent (CREATE TABLE IF NOT EXISTS) and, if the nomina
// parent still carries the v7 source_url column, runs the migrateToV8 recreate+backfill.
// Idempotent — a no-op for an already-complete v8 database.
func ensureNominaV8(conn *sqlite.Conn) error {
	hasNomina, err := tableExists(conn, "nomina")
	if err != nil {
		return fmt.Errorf("v8 self-heal: check nomina table: %w", err)
	}
	if !hasNomina {
		// No nomina table at all (should not happen post-migration); create the full v8
		// shape so a downstream read does not fail with "no such table".
		if err := createNominaTableV8(conn); err != nil {
			return err
		}
		if err := createNomenAttestationsTable(conn); err != nil {
			return err
		}
		return createCreatorsTable(conn)
	}
	hasSourceURL, err := columnExists(conn, "nomina", "source_url")
	if err != nil {
		return fmt.Errorf("v8 self-heal: check nomina.source_url column: %w", err)
	}
	if hasSourceURL {
		// A v7-shaped nomina under a schema_meta=8 cache: run the full recreate+backfill.
		return migrateToV8(conn)
	}
	// nomina is already v8-shaped; ensure the two new tables exist (a partially-built
	// intermediate-v8 cache may have recreated nomina but not yet the child/dimension).
	if err := createNomenAttestationsTable(conn); err != nil {
		return err
	}
	return createCreatorsTable(conn)
}

// createProvenanceTables creates the four v5 BCNF provenance tables in FK
// dependency order (parents before children): data_sources and entities are
// dimensions; dataset_ingested and entity_source reference them. Each statement
// is CREATE TABLE IF NOT EXISTS, so the helper is idempotent and safe to call on
// the fresh-DB path and from migrateToV5 alike.
//
// It creates the v5 (single current-ingest) shape of dataset_ingested. The
// fresh-DB v6 path uses createProvenanceTablesV6 instead; this helper is retained
// for the v4 to v5 upgrade arm so migrateToV6's table-recreate has a faithful v5
// table to migrate.
func createProvenanceTables(conn *sqlite.Conn) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"data_sources", dataSourcesTableSQL},
		{"entities", entitiesTableSQL},
		{"dataset_ingested", datasetIngestedTableSQL},
		{"entity_source", entitySourceTableSQL},
	}
	for _, s := range stmts {
		if err := sqlitex.ExecuteTransient(conn, s.sql, nil); err != nil {
			return fmt.Errorf("create %s table: %w", s.name, err)
		}
	}
	return nil
}

// createMetadataTables creates the three v6 entity-metadata tables (the parent
// entity_metadata dimension before its metadata_benchmarks / metadata_links child
// tables). Each statement is CREATE TABLE IF NOT EXISTS, so the helper is
// idempotent and shared by the fresh-DB v6 path and migrateToV6.
func createMetadataTables(conn *sqlite.Conn) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"entity_metadata", entityMetadataTableSQL},
		{"metadata_benchmarks", metadataBenchmarksTableSQL},
		{"metadata_links", metadataLinksTableSQL},
	}
	for _, s := range stmts {
		if err := sqlitex.ExecuteTransient(conn, s.sql, nil); err != nil {
			return fmt.Errorf("create %s table: %w", s.name, err)
		}
	}
	return nil
}

// createProvenanceTablesV6 creates the full v6 provenance + metadata shape for a
// fresh database in FK dependency order: the data_sources / entities dimensions,
// the append-only dataset_ingested history (composite primary key), the
// entity_source join, then the three entity-metadata tables. It exists so a fresh
// v6 database is created directly with the target shape rather than being built at
// v5 and then migrated — the same fresh-arm discipline the v5 tables already
// follow.
func createProvenanceTablesV6(conn *sqlite.Conn) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"data_sources", dataSourcesTableSQL},
		{"entities", entitiesTableSQL},
		{"dataset_ingested", datasetIngestedV6TableSQL},
		{"entity_source", entitySourceTableSQL},
	}
	for _, s := range stmts {
		if err := sqlitex.ExecuteTransient(conn, s.sql, nil); err != nil {
			return fmt.Errorf("create %s table: %w", s.name, err)
		}
	}
	return createMetadataTables(conn)
}

// CanonicalFilter selects models by their parsed canonical axes.
// Empty fields act as wildcards: an empty Family matches any family, an
// empty Variant matches any variant, an empty Version matches any version,
// and an empty Date matches any date.
// This is the parameter type for Store.QueryByCanonical.
type CanonicalFilter struct {
	Family  Family
	Variant string
	Version string
	Date    string
}

// Store is a SQLite-backed cache for AI model metadata.
// Use OpenStore to create, and Close when done.
type Store struct {
	conn *sqlite.Conn
	path string
}

// DefaultDBPath returns the default path for the models database.
// It uses $XDG_CACHE_HOME/bestiary/models.db, falling back to
// ~/.cache/bestiary/models.db when XDG_CACHE_HOME is not set.
func DefaultDBPath() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("bestiary: DefaultDBPath: resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "bestiary", "models.db"), nil
}

// OpenStore opens (or creates) the SQLite database at path.
// It applies any pending schema migrations before returning.
// Caller must call Close when done.
func OpenStore(path string) (*Store, error) {
	// ":memory:" has no directory component; os.MkdirAll(".", …) is harmless but
	// we skip it for in-memory databases to avoid creating a stray directory.
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("bestiary: OpenStore: create parent dirs for %s: %w", path, err)
		}
	}

	conn, err := sqlite.OpenConn(path)
	if err != nil {
		return nil, fmt.Errorf("bestiary: OpenStore: open %s: %w", path, err)
	}

	// Enforce foreign keys on this connection BEFORE any migration runs. SQLite
	// defaults FK enforcement OFF, so the v5 BCNF tables' FK clauses are decorative
	// unless this pragma is set; it must be set outside a transaction, which is why
	// it precedes migrateSchema. PRAGMA foreign_keys is per-connection, so it is set
	// once here for the lifetime of the Store's single connection.
	if err := sqlitex.ExecuteTransient(conn, `PRAGMA foreign_keys = ON;`, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: enable foreign_keys on %s: %w", path, err)
	}

	// Ensure schema_meta exists — safe on fresh and existing DBs.
	if err := sqlitex.ExecuteTransient(conn, schemaMetaSQL, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: create schema_meta in %s: %w", path, err)
	}

	// Read current schema version (0 if no row exists).
	version, err := getSchemaVersion(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: read schema version from %s: %w", path, err)
	}

	// Apply any pending migrations.
	if version < currentSchemaVersion {
		if err := migrateSchema(conn, version); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("bestiary: OpenStore: migrate %s from v%d: %w", path, version, err)
		}
	}

	// Self-heal the v6 instance-level models columns even when schema_meta already
	// reads currentSchemaVersion. A database created by an intermediate build of
	// this (unreleased) v6 branch records schema_meta=6 but its models table
	// predates these columns, so the version-gated migration above never runs and a
	// query would fail with "no such column". This presence-guarded, idempotent step
	// backfills the missing columns on open; it is a no-op for an already-complete
	// models table.
	if err := ensureModelColumnsV6(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: ensure v6 model columns on %s: %w", path, err)
	}

	// Self-heal the entity_metadata provenance columns for the same
	// intermediate-v6-cache reason: a database created before raw_family was added
	// records schema_meta=6 but its entity_metadata table lacks the column, so the
	// version-gated migration never runs and a metadata read/write would fail. This
	// presence-guarded, idempotent step backfills it on open; a no-op otherwise.
	if err := ensureEntityMetadataColumnsV6(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: ensure v6 entity_metadata columns on %s: %w", path, err)
	}

	// Self-heal the v7 per-instance region columns for the same intermediate-cache
	// reason: a database created by an intermediate v7 build records schema_meta=7 but
	// its models table may predate the region columns, so the version-gated migration
	// never runs. Presence-guarded and idempotent — a no-op for an already-complete table.
	if err := ensureModelColumnsV7(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: ensure v7 region columns on %s: %w", path, err)
	}

	// Self-heal the v8 naming layer: an intermediate-v8 cache records schema_meta=8 (so
	// the version-gated migration above never runs) but may still carry a v7-shaped nomina
	// (fused source columns) or lack the nomen_attestations / creators tables. ensureNominaV8
	// is presence-guarded and idempotent: it recreates a v7-shaped nomina into the v8 child
	// model and creates the two new tables if absent — a no-op for a complete v8 database.
	// It supersedes the prior v7 nomina self-heal (it also handles an absent nomina table).
	if err := ensureNominaV8(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: ensure v8 naming tables on %s: %w", path, err)
	}

	// Self-heal the v9 archive-snapshot column for the same intermediate-cache reason:
	// a database built by an intermediate v9 build records schema_meta=9 (so the
	// version-gated migration above never runs) but its nomen_attestations table may
	// predate archived_url, and a read would then fail with "no such column". Presence-
	// guarded and idempotent — a no-op for an already-complete v9 table. It runs AFTER
	// ensureNominaV8, which may itself have just created the table.
	if err := ensureNomenAttestationColumnsV9(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: ensure v9 attestation columns on %s: %w", path, err)
	}

	return &Store{conn: conn, path: path}, nil
}

// getSchemaVersion reads the stored schema version from schema_meta.
// Returns 0 if the table is empty (legacy DB or brand-new DB).
func getSchemaVersion(conn *sqlite.Conn) (int, error) {
	var version int
	var found bool
	err := sqlitex.Execute(conn, "SELECT version FROM schema_meta LIMIT 1", &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			version = int(stmt.GetInt64("version"))
			found = true
			return nil
		},
	})
	if err != nil {
		return 0, fmt.Errorf("bestiary: getSchemaVersion: %w", err)
	}
	if !found {
		return 0, nil // No version row → treat as version 0.
	}
	return version, nil
}

// setSchemaVersion replaces the single schema_meta row with version.
func setSchemaVersion(conn *sqlite.Conn, version int) error {
	if err := sqlitex.ExecuteTransient(conn, "DELETE FROM schema_meta", nil); err != nil {
		return fmt.Errorf("bestiary: setSchemaVersion: clear schema_meta: %w", err)
	}
	return sqlitex.Execute(conn, "INSERT INTO schema_meta (version) VALUES (?1)",
		&sqlitex.ExecOptions{Args: []any{version}})
}

// tableExists reports whether a table with name exists in the database.
func tableExists(conn *sqlite.Conn, name string) (bool, error) {
	var exists bool
	err := sqlitex.Execute(conn,
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name=?1",
		&sqlitex.ExecOptions{
			Args: []any{name},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				exists = true
				return nil
			},
		})
	return exists, err
}

// columnExists reports whether column exists in table.
func columnExists(conn *sqlite.Conn, table, column string) (bool, error) {
	var exists bool
	err := sqlitex.Execute(conn,
		fmt.Sprintf("SELECT 1 FROM pragma_table_info('%s') WHERE name=?1", table),
		&sqlitex.ExecOptions{
			Args: []any{column},
			ResultFunc: func(stmt *sqlite.Stmt) error {
				exists = true
				return nil
			},
		})
	return exists, err
}

// migrateSchema applies all migrations needed to bring the database from
// fromVersion to currentSchemaVersion, then records the new version.
func migrateSchema(conn *sqlite.Conn, fromVersion int) error {
	hasModels, err := tableExists(conn, "models")
	if err != nil {
		return fmt.Errorf("bestiary: migrateSchema: check models table: %w", err)
	}

	if !hasModels {
		// Fresh database — create the current schema directly.
		if err := sqlitex.ExecuteTransient(conn, schemaSQL, nil); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create models table: %w", err)
		}
		// Create the canonical index on fresh DBs; upgrade paths handle their
		// own index creation inside each migrateToVN function.
		if err := sqlitex.ExecuteTransient(conn, indexSQL, nil); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create idx_canonical: %w", err)
		}
		// Fresh DBs must ALSO get the full v6 provenance + metadata shape here: this
		// branch skips the migrateToVN arms below, so without this call a fresh
		// database would record schema_meta=6 with no provenance tables. The v6
		// creator makes the append-only dataset_ingested history and the three
		// entity-metadata tables directly (not the v5 shape followed by a migrate).
		if err := createProvenanceTablesV6(conn); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create provenance tables: %w", err)
		}
		// v8 naming layer: created on the fresh path in the target v8 shape (the parent
		// WITHOUT the fused source columns, plus the nomen_attestations child and the
		// creators dimension) so a fresh v8 database is never left with schema_meta=8 but a
		// v7-shaped nomina or missing child/dimension tables — the same fresh-arm discipline
		// the v5/v6 tables follow. The models table already carries the v7 region columns
		// via schemaSQL.
		if err := createNominaTableV8(conn); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create nomina table: %w", err)
		}
		if err := createNomenAttestationsTable(conn); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create nomen_attestations table: %w", err)
		}
		if err := createCreatorsTable(conn); err != nil {
			return fmt.Errorf("bestiary: migrateSchema: create creators table: %w", err)
		}
	} else {
		if fromVersion < 2 {
			// Existing database with v0/v1 schema needs migration to v2.
			// SQLite cannot ALTER PRIMARY KEY, so we recreate the table.
			if err := migrateToV2(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v%d→v2: %w", fromVersion, err)
			}
			// Fall through: v2 DB still needs migration to v3 then v4.
			if err := migrateToV3(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v2→v3: %w", err)
			}
			if err := migrateToV4(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v3→v4: %w", err)
			}
		} else if fromVersion < 3 {
			// v2 database needs migration to v3, then v4.
			if err := migrateToV3(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v2→v3: %w", err)
			}
			if err := migrateToV4(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v3→v4: %w", err)
			}
		} else if fromVersion < 4 {
			// v3 database needs migration to v4.
			if err := migrateToV4(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v3→v4: %w", err)
			}
		}
		// All upgrade arms above converge on the v4 models schema. The v4→v5 step
		// is purely additive (new BCNF tables, models untouched), so it applies to
		// every existing database that predates v5.
		if fromVersion < 5 {
			if err := migrateToV5(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v4→v5: %w", err)
			}
		}
		// The v5→v6 step recreates dataset_ingested with a composite primary key
		// (append-only history) and adds the three entity-metadata tables. It
		// applies to every existing database that predates v6, including one just
		// brought to v5 by the arm above (chained v4→v5→v6).
		if fromVersion < 6 {
			if err := migrateToV6(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v5→v6: %w", err)
			}
		}
		// The v6→v7 step adds the per-instance region columns to the models table and
		// creates the nomina naming table. Both are additive (no table recreation), so
		// it applies to every existing database that predates v7, including one just
		// brought to v6 by the arm above (chained …→v6→v7).
		if fromVersion < 7 {
			if err := migrateToV7(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v6→v7: %w", err)
			}
		}
		// The v7→v8 step lifts nomen provenance into the nomen_attestations child table
		// (recreating the nomina parent without the fused source columns) and adds the
		// creators BCNF dimension. It applies to every existing database that predates v8,
		// including one just brought to v7 by the arm above (chained …→v7→v8).
		if fromVersion < 8 {
			if err := migrateToV8(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v7→v8: %w", err)
			}
		}
		// The v8→v9 step adds the archive-snapshot column to nomen_attestations. It is
		// additive (ALTER TABLE ADD COLUMN — no recreation), so it applies to every
		// existing database that predates v9, including one just brought to v8 by the arm
		// above (chained …→v8→v9).
		if fromVersion < 9 {
			if err := migrateToV9(conn); err != nil {
				return fmt.Errorf("bestiary: migrateSchema: v8→v9: %w", err)
			}
		}
	}

	if err := setSchemaVersion(conn, currentSchemaVersion); err != nil {
		return fmt.Errorf("bestiary: migrateSchema: set version: %w", err)
	}
	return nil
}

// tableRecreate applies the standard SQLite table-recreation migration pattern:
//  1. Execute createSQL to create models_new with the target schema.
//  2. Execute copySQL to populate models_new from the old models table.
//  3. Drop the old models table.
//  4. Rename models_new to models.
//
// Both createSQL and copySQL run inside the caller's transaction.
// This helper is used by migrateToV2 and migrateToV3 to avoid duplicating
// the identical 3-step pattern.
func tableRecreate(conn *sqlite.Conn, createSQL, copySQL string) error {
	if err := sqlitex.ExecuteTransient(conn, createSQL, nil); err != nil {
		return fmt.Errorf("create models_new: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, copySQL, nil); err != nil {
		return fmt.Errorf("copy data to models_new: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `DROP TABLE models`, nil); err != nil {
		return fmt.Errorf("drop old models table: %w", err)
	}
	if err := sqlitex.ExecuteTransient(conn, `ALTER TABLE models_new RENAME TO models`, nil); err != nil {
		return fmt.Errorf("rename models_new to models: %w", err)
	}
	return nil
}

// migrateToV2 upgrades an existing models table to the v2 schema:
//   - Adds interleaved_config column (if missing).
//   - Changes PRIMARY KEY from (model_id) to (model_id, provider).
//
// SQLite does not support altering a primary key in place, so the migration
// creates a new table, copies data, and renames.
func migrateToV2(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	const createNewSQL = `CREATE TABLE IF NOT EXISTS models_new (
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

	// Determine whether the old table already has the interleaved_config column.
	hasConfig, err := columnExists(conn, "models", "interleaved_config")
	if err != nil {
		return fmt.Errorf("check interleaved_config column: %w", err)
	}

	// Copy rows from old table, supplying '' for interleaved_config if absent.
	var copySQL string
	if hasConfig {
		copySQL = `INSERT OR IGNORE INTO models_new SELECT * FROM models`
	} else {
		copySQL = `INSERT OR IGNORE INTO models_new
            SELECT model_id, provider, display_name, family,
                context_window, max_output,
                reasoning, tool_call, attachment, temperature, structured_output, interleaved, '' ,
                open_weights,
                cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
                release_date, knowledge,
                modalities_input, modalities_output,
                last_synced
            FROM models`
	}

	if err = tableRecreate(conn, createNewSQL, copySQL); err != nil {
		return err
	}
	return nil
}

// migrateToV3 upgrades an existing v2 models table to the v3 schema:
//   - Renames existing `family` column to `raw_family`.
//   - Adds NEW `family` (parsed canonical family), `variant`, and `date` columns.
//   - Backfills family/variant/date by re-running ParseFamily and ExtractDate on each row.
//   - Creates idx_canonical index on (family, variant, provider) for QueryByCanonical.
//
// SQLite does not support renaming columns prior to 3.25, so the migration
// creates a new table, copies data, renames, and then backfills.
func migrateToV3(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	const createNewSQL = `CREATE TABLE IF NOT EXISTS models_new (
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

	// Copy rows from old v2 table: map old family → raw_family; new columns default to ''.
	const copySQL = `INSERT OR IGNORE INTO models_new
        SELECT model_id, provider, display_name,
            family AS raw_family, '' AS family, '' AS variant, '' AS date,
            context_window, max_output,
            reasoning, tool_call, attachment, temperature, structured_output,
            interleaved, interleaved_config,
            open_weights,
            cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
            release_date, knowledge,
            modalities_input, modalities_output,
            last_synced
        FROM models`

	if err = tableRecreate(conn, createNewSQL, copySQL); err != nil {
		return err
	}

	// Backfill: read each row and re-parse family/variant/date.
	// Two-pass: zombiezen/sqlite does not allow issuing new statements on conn
	// while a ResultFunc cursor is open, so we collect all keys first then UPDATE.
	type rowKey struct {
		modelID     string
		provider    string
		rawFamily   string
		releaseDate string
	}
	var rows []rowKey
	err = sqlitex.Execute(conn, `SELECT model_id, provider, raw_family, release_date FROM models`,
		&sqlitex.ExecOptions{
			ResultFunc: func(stmt *sqlite.Stmt) error {
				rows = append(rows, rowKey{
					modelID:     stmt.GetText("model_id"),
					provider:    stmt.GetText("provider"),
					rawFamily:   stmt.GetText("raw_family"),
					releaseDate: stmt.GetText("release_date"),
				})
				return nil
			},
		})
	if err != nil {
		return fmt.Errorf("read rows for backfill: %w", err)
	}

	const backfillSQL = `UPDATE models SET family = ?1, variant = ?2, date = ?3
        WHERE model_id = ?4 AND provider = ?5`
	for _, r := range rows {
		parsedFamily, variant := ParseFamily(Family(r.rawFamily))
		date := ExtractDate(ModelID(r.modelID), r.releaseDate)
		err = sqlitex.Execute(conn, backfillSQL, &sqlitex.ExecOptions{
			Args: []any{
				string(parsedFamily),
				variant,
				date,
				r.modelID,
				r.provider,
			},
		})
		if err != nil {
			return fmt.Errorf("backfill row (%s, %s): %w", r.modelID, r.provider, err)
		}
	}

	// Create the v3 canonical index (family, variant, provider).
	// migrateToV4 will subsequently drop and recreate this as (family, variant, version, provider).
	err = sqlitex.ExecuteTransient(conn, indexV3SQL, nil)
	if err != nil {
		return fmt.Errorf("create idx_canonical (v3): %w", err)
	}

	return nil
}

// migrateToV4 upgrades an existing v3 models table to the v4 schema:
//   - Adds the version column (TEXT NOT NULL, empty-string default) for the model
//     version extracted from the family string (e.g. "4.5" for claude-opus-4-5).
//   - Drops the v3 idx_canonical (family, variant, provider) index and
//     recreates it as (family, variant, version, provider) so that version
//     is a first-class lookup axis.
//
// SQLite supports ADD COLUMN via ALTER TABLE for NOT NULL columns with a
// constant DEFAULT value, so table-recreate is not required here.
// The new column defaults to the empty string for all existing rows; a subsequent
// sync operation will backfill Version from the parser.
func migrateToV4(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	// Step 1: Add the version column (defaults to '' for all existing rows).
	err = sqlitex.ExecuteTransient(conn,
		`ALTER TABLE models ADD COLUMN version TEXT NOT NULL DEFAULT ''`, nil)
	if err != nil {
		return fmt.Errorf("add version column: %w\n"+
			"  What: v3→v4 migration failed to add the version column\n"+
			"  Why: ALTER TABLE rejected — column may already exist or schema is corrupt\n"+
			"  Where: store.go migrateToV4\n"+
			"  How to fix: inspect the database schema; if already on v4, this is a version mismatch bug",
			err)
	}

	// Step 2: Drop the v3 idx_canonical (covers family, variant, provider).
	err = sqlitex.ExecuteTransient(conn, `DROP INDEX IF EXISTS idx_canonical`, nil)
	if err != nil {
		return fmt.Errorf("drop old idx_canonical: %w", err)
	}

	// Step 3: Recreate idx_canonical with version as a key column.
	err = sqlitex.ExecuteTransient(conn, indexSQL, nil)
	if err != nil {
		return fmt.Errorf("create new idx_canonical (v4): %w", err)
	}

	return nil
}

// migrateToV5 upgrades an existing v4 database to the v5 schema by adding the
// four BCNF data-source provenance tables (data_sources, dataset_ingested,
// entities, entity_source). The migration is purely additive: the models table
// is untouched and no data is copied or recreated. Because each statement is
// CREATE TABLE IF NOT EXISTS, re-running is harmless.
func migrateToV5(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	if err = createProvenanceTables(conn); err != nil {
		return fmt.Errorf("create BCNF provenance tables: %w", err)
	}
	return nil
}

// migrateToV6 upgrades an existing v5 database to the v6 schema:
//   - Widens dataset_ingested's primary key from (data_source_id) to the composite
//     (data_source_id, ingested_at) so a source records an append-only ingest
//     history instead of a single current row.
//   - Adds the three entity-metadata tables (entity_metadata + its
//     metadata_benchmarks / metadata_links children).
//
// SQLite cannot ALTER a primary key in place, so the dataset_ingested widening
// uses a DEDICATED table-recreate sequence (create the new table, copy every
// existing row, drop the old table, rename) rather than the models-specific
// tableRecreate helper, whose hard-coded models/models_new names do not fit here.
// dataset_ingested is a leaf (no other table references it), so dropping and
// renaming it does not disturb any foreign key.
func migrateToV6(conn *sqlite.Conn) error {
	endFn := sqlitex.Transaction(conn)
	var err error
	defer endFn(&err)

	// Step 1: recreate dataset_ingested with the composite primary key. The new
	// table is created under a temporary name, populated from the old table, then
	// swapped in. INSERT OR IGNORE on the copy is defensive: a v5 table has one row
	// per source (its own primary key), so no composite-key collision can occur, but
	// OR IGNORE keeps the copy total even if a hand-edited source somehow held two.
	const createIngestedNewSQL = `CREATE TABLE dataset_ingested_new (
    data_source_id TEXT NOT NULL REFERENCES data_sources(data_source_id),
    ingested_at    TEXT NOT NULL,
    parser_schema  INTEGER NOT NULL,
    PRIMARY KEY (data_source_id, ingested_at)
)`
	if err = sqlitex.ExecuteTransient(conn, createIngestedNewSQL, nil); err != nil {
		return fmt.Errorf("create dataset_ingested_new: %w", err)
	}
	const copyIngestedSQL = `INSERT OR IGNORE INTO dataset_ingested_new (data_source_id, ingested_at, parser_schema)
        SELECT data_source_id, ingested_at, parser_schema FROM dataset_ingested`
	if err = sqlitex.ExecuteTransient(conn, copyIngestedSQL, nil); err != nil {
		return fmt.Errorf("copy dataset_ingested rows: %w", err)
	}
	if err = sqlitex.ExecuteTransient(conn, `DROP TABLE dataset_ingested`, nil); err != nil {
		return fmt.Errorf("drop old dataset_ingested: %w", err)
	}
	if err = sqlitex.ExecuteTransient(conn, `ALTER TABLE dataset_ingested_new RENAME TO dataset_ingested`, nil); err != nil {
		return fmt.Errorf("rename dataset_ingested_new to dataset_ingested: %w", err)
	}

	// Step 2: add the three entity-metadata tables. createMetadataTables is
	// CREATE TABLE IF NOT EXISTS, so on an intermediate-v6 cache whose entity_metadata
	// predates raw_family it leaves the older table untouched; the presence-guarded
	// self-heal below backfills the column so both the fresh-create and
	// already-present paths converge on the same shape.
	if err = createMetadataTables(conn); err != nil {
		return fmt.Errorf("create entity-metadata tables: %w", err)
	}
	if err = ensureEntityMetadataColumnsV6(conn); err != nil {
		return fmt.Errorf("add v6 entity_metadata columns: %w", err)
	}

	// Step 3: add the eight instance-level models.dev columns to the models table.
	// Each is a defaulted non-PK column, so ALTER TABLE ADD COLUMN is SQLite-safe
	// (no table recreate). This goes through the presence-guarded ensureModelColumnsV6
	// so a re-run over a partially-migrated table adds only what is missing rather
	// than erroring on a duplicate column.
	if err = ensureModelColumnsV6(conn); err != nil {
		return fmt.Errorf("add v6 models columns: %w", err)
	}
	return nil
}

// Close closes the underlying SQLite connection.
func (s *Store) Close() error {
	if s.conn == nil {
		return nil
	}
	err := s.conn.Close()
	s.conn = nil
	return err
}

// UpsertModels inserts or replaces the given models in the store.
// It sets LastSynced to the current UTC time in RFC3339 format for each model.
// All upserts run inside a single transaction.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support per-operation context cancellation.
func (s *Store) UpsertModels(ctx context.Context, models []ModelInfo) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	now := time.Now().UTC().Format(time.RFC3339)

	const upsertSQL = `INSERT OR REPLACE INTO models (
		model_id, provider, display_name, raw_family, family, variant, version, date,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced,
		description, status, status_raw, reasoning_options,
		cost_input_audio, cost_output_audio, cost_context_over_200k, cost_tiers,
		region, region_raw
	) VALUES (
		?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8,
		?9, ?10,
		?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18,
		?19, ?20, ?21, ?22, ?23,
		?24, ?25,
		?26, ?27,
		?28,
		?29, ?30, ?31, ?32,
		?33, ?34, ?35, ?36,
		?37, ?38
	)`

	for i := range models {
		m := &models[i]
		statusStr, statusRaw := modelStatusToStore(m.Status, m.StatusRaw)
		err = sqlitex.Execute(s.conn, upsertSQL, &sqlitex.ExecOptions{
			Args: []any{
				string(m.ID),
				string(m.Provider),
				m.DisplayName,
				string(m.RawFamily),
				string(m.Family),
				m.Variant,
				m.Version,
				m.Date,
				m.ContextWindow,
				m.MaxOutput,
				boolToInt(m.Reasoning),
				boolToInt(m.ToolCall),
				boolToInt(m.Attachment),
				boolToInt(m.Temperature),
				boolToInt(m.StructuredOutput),
				boolToInt(m.Interleaved.Supported),
				capabilityConfigToString(m.Interleaved.Config),
				boolToInt(m.OpenWeights),
				derefFloat64(m.CostInputPerMTok),
				derefFloat64(m.CostOutputPerMTok),
				derefFloat64(m.CostReasoningPerMTok),
				derefFloat64(m.CostCacheReadPerMTok),
				derefFloat64(m.CostCacheWritePerMTok),
				m.ReleaseDate,
				m.Knowledge,
				modalitiesToString(m.Modalities.Input),
				modalitiesToString(m.Modalities.Output),
				now,
				m.Description,
				statusStr,
				statusRaw,
				reasoningOptionsToString(m.ReasoningOptions),
				derefFloat64(m.CostInputAudioPerMTok),
				derefFloat64(m.CostOutputAudioPerMTok),
				tierCostPtrToString(m.CostContextOver200k),
				costTiersToString(m.CostTiers),
				m.Region.String(),
				m.RegionRaw,
			},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertModels: upsert model %s: %w", m.ID, err)
		}
	}

	return nil
}

// UpsertDataSources writes the data-source dimension rows and appends their
// ingest rows in a single transaction. Rows are written parents-before-children to
// satisfy the dataset_ingested.data_source_id foreign key: every DataSource is
// written first (insert-or-update on the id primary key), then every
// DatasetIngested. A DatasetIngested whose SourceID names a DataSource absent from
// BOTH this call and the existing data_sources table is rejected by the foreign
// key (when foreign_keys is ON).
//
// The ingest pass is APPEND-ONLY: it uses INSERT OR IGNORE against the composite
// primary key (data_source_id, ingested_at). A new (source, timestamp) appends a
// history row; an identical (source, timestamp) is the same ingest fact re-
// asserted, so OR IGNORE keeps the original untouched — it can never MUTATE an
// existing row (unlike OR REPLACE, which would overwrite parser_schema). The
// current ingest for a source is the row with the maximum ingested_at, surfaced by
// QueryCurrentIngests / DatasetIngestedFor.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) UpsertDataSources(ctx context.Context, sources []DataSource, ingested []DatasetIngested) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	// Pass 1 (parent dimension): data_sources. The upsert targets the
	// data_source_id primary key ONLY (ON CONFLICT DO UPDATE), so re-ingesting a
	// source by id refreshes its uri/canonical_name — but a DIFFERENT id claiming
	// an already-owned uri violates the UNIQUE(uri) candidate key and is rejected,
	// rather than silently REPLACE-deleting the incumbent row (which plain
	// INSERT OR REPLACE would do on the secondary UNIQUE).
	const sourceSQL = `INSERT INTO data_sources (
		data_source_id, uri, canonical_name
	) VALUES (?1, ?2, ?3)
	ON CONFLICT(data_source_id) DO UPDATE SET
		uri = excluded.uri,
		canonical_name = excluded.canonical_name`
	for i := range sources {
		ds := &sources[i]
		err = sqlitex.Execute(s.conn, sourceSQL, &sqlitex.ExecOptions{
			Args: []any{string(ds.ID), ds.URI, ds.CanonicalName},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertDataSources: upsert data_source %q (uri %q): %w\n"+
				"  What: writing a data-source dimension row failed\n"+
				"  Why: a constraint was violated — most likely the UNIQUE(uri) candidate key (another source already owns this uri)\n"+
				"  Where: store.go UpsertDataSources, data_sources insert\n"+
				"  How to fix: give each data source a distinct uri, or correct the duplicate id",
				ds.ID, ds.URI, err)
		}
	}

	// Pass 2 (child fact): dataset_ingested references data_sources.data_source_id.
	// INSERT OR IGNORE is append-only: a duplicate (data_source_id, ingested_at) is
	// silently skipped (the original history row is retained), while a genuine
	// foreign-key violation (unknown data_source_id) still errors — OR IGNORE
	// suppresses only the listed constraint algorithms, never a foreign-key abort.
	const ingestSQL = `INSERT OR IGNORE INTO dataset_ingested (
		data_source_id, ingested_at, parser_schema
	) VALUES (?1, ?2, ?3)`
	for i := range ingested {
		di := &ingested[i]
		err = sqlitex.Execute(s.conn, ingestSQL, &sqlitex.ExecOptions{
			Args: []any{string(di.SourceID), di.IngestedAt, di.ParserSchema},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertDataSources: append dataset_ingested for source %q: %w\n"+
				"  What: writing an ingest-history row failed\n"+
				"  Why: the foreign key data_source_id has no matching data_sources row in this call or the store\n"+
				"  Where: store.go UpsertDataSources, dataset_ingested insert\n"+
				"  How to fix: include the DataSource for %q in the sources argument before its DatasetIngested",
				di.SourceID, err, di.SourceID)
		}
	}

	return nil
}

// UpsertEntitySources inserts or replaces the entity↔source join rows in a single
// transaction. Rows are written parents-before-children to satisfy the
// entity_source.entity_key foreign key: pass 1 ensures a minimal entities
// dimension row exists for each distinct EntityKey (INSERT OR IGNORE so a richer
// pre-existing row is never clobbered; the decomposed columns default to the
// empty string — the store's entities table is a foreign-key target, not the authoritative entity
// decomposition, which lives in the generated registry), then pass 2 writes the
// join rows. The entity_source.data_source_id foreign key is NOT auto-satisfied
// here: callers must have populated data_sources first via UpsertDataSources, so an
// EntitySource naming an unknown source is rejected (when foreign_keys is ON).
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) UpsertEntitySources(ctx context.Context, sources []EntitySource) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	// Pass 1 (parent dimension): minimal entities rows so the entity_key FK resolves.
	const entitySQL = `INSERT OR IGNORE INTO entities (entity_key) VALUES (?1)`
	for i := range sources {
		es := &sources[i]
		err = sqlitex.Execute(s.conn, entitySQL, &sqlitex.ExecOptions{
			Args: []any{es.EntityKey},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertEntitySources: ensure entity %q: %w\n"+
				"  What: writing the minimal entities dimension row failed\n"+
				"  Why: the entities insert was rejected unexpectedly\n"+
				"  Where: store.go UpsertEntitySources, entities insert\n"+
				"  How to fix: verify the v5 entities table exists (OpenStore migration)",
				es.EntityKey, err)
		}
	}

	// Pass 2 (child join): entity_source references entities and data_sources.
	const joinSQL = `INSERT OR REPLACE INTO entity_source (
		entity_key, data_source_id
	) VALUES (?1, ?2)`
	for i := range sources {
		es := &sources[i]
		err = sqlitex.Execute(s.conn, joinSQL, &sqlitex.ExecOptions{
			Args: []any{es.EntityKey, string(es.SourceID)},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertEntitySources: upsert attestation (entity %q, source %q): %w\n"+
				"  What: writing an entity↔source join row failed\n"+
				"  Why: the data_source_id foreign key has no matching data_sources row\n"+
				"  Where: store.go UpsertEntitySources, entity_source insert\n"+
				"  How to fix: call UpsertDataSources with the DataSource for %q before attesting entities to it",
				es.EntityKey, es.SourceID, err, es.SourceID)
		}
	}

	return nil
}

// UpsertNomina writes the naming rows into the v8 nomina parent table and their
// evidence sets into the nomen_attestations child table in a single transaction. Per
// nomen:
//
//   - The parent row is a per-triple IDEMPOTENT insert keyed by the primary key
//     (value, scheme, entity_key): INSERT OR IGNORE, so re-persisting the same triple
//     is a no-op that never MUTATES an existing status (unlike OR REPLACE). A same-triple
//     Status CONFLICT is not reconciled here by last-write-wins — the codegen guard
//     ValidateNomina rejects a conflicting mint before it ever reaches the store, and OR
//     IGNORE keeps the incumbent so a stray duplicate at sync cannot silently overwrite.
//   - The attestation set is a REPLACEABLE SET owned by the triple (the entity_metadata
//     child-table precedent): the prior nomen_attestations rows for the triple are DELETEd
//     and the current Attestations re-inserted, so a re-sync with a changed evidence set
//     converges by content and re-syncing an identical set is idempotent.
//
// The nomen_attestations.source_id foreign key into data_sources is NOT auto-satisfied:
// callers must have populated data_sources first (via UpsertDataSources), so an
// attestation naming an unknown source is rejected when foreign_keys is ON and the whole
// transaction rolls back (no partial write). entity_key is deliberately NOT an FK (the
// entities table is a stub dimension; a minted canonical/provider-id nomen may name an
// entity never written to it).
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) UpsertNomina(ctx context.Context, nomina []Nomen) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	const parentSQL = `INSERT OR IGNORE INTO nomina (
		value, scheme, entity_key, status
	) VALUES (?1, ?2, ?3, ?4)`
	const delChildSQL = `DELETE FROM nomen_attestations WHERE value = ?1 AND scheme = ?2 AND entity_key = ?3`
	const insChildSQL = `INSERT INTO nomen_attestations (
		value, scheme, entity_key, source_url, source_id, authority, method, ingested_at, archived_url
	) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)`

	for i := range nomina {
		n := &nomina[i]
		schemeStr := n.Scheme.String()
		entityKey := n.ResolvesTo.String()

		// (1) Parent row (identity + editorial status only).
		err = sqlitex.Execute(s.conn, parentSQL, &sqlitex.ExecOptions{
			Args: []any{n.Value, schemeStr, entityKey, n.Status.String()},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertNomina: upsert nomen (value=%q, scheme=%q, entity=%q): %w\n"+
				"  What: writing a naming parent row failed\n"+
				"  Why: an unexpected SQLite error on the nomina insert\n"+
				"  Where: store.go UpsertNomina, nomina insert\n"+
				"  How to fix: verify the v8 nomina table exists (OpenStore migration)",
				n.Value, schemeStr, entityKey, err)
		}

		// (2) Clear the replaceable attestation set for this triple.
		if err = sqlitex.Execute(s.conn, delChildSQL, &sqlitex.ExecOptions{Args: []any{n.Value, schemeStr, entityKey}}); err != nil {
			return fmt.Errorf("bestiary: UpsertNomina: clear attestations for nomen (value=%q, scheme=%q, entity=%q): %w\n"+
				"  What: deleting the prior attestation rows failed\n"+
				"  Why: an unexpected SQLite error during the replace-set refresh\n"+
				"  Where: store.go UpsertNomina, nomen_attestations delete\n"+
				"  How to fix: verify the v8 nomen_attestations table exists (OpenStore migration)",
				n.Value, schemeStr, entityKey, err)
		}

		// (3) Insert the current attestations. source_id is the FK into data_sources.
		for j := range n.Attestations {
			at := &n.Attestations[j]
			err = sqlitex.Execute(s.conn, insChildSQL, &sqlitex.ExecOptions{
				Args: []any{
					n.Value, schemeStr, entityKey,
					at.SourceURL, string(at.Source), at.Authority.String(), at.Method.String(), at.IngestedAt,
					at.ArchivedURL,
				},
			})
			if err != nil {
				return fmt.Errorf("bestiary: UpsertNomina: insert attestation for nomen (value=%q, scheme=%q, entity=%q, source=%q): %w\n"+
					"  What: writing an attestation child row failed\n"+
					"  Why: most likely the source_id foreign key has no matching data_sources row\n"+
					"  Where: store.go UpsertNomina, nomen_attestations insert\n"+
					"  How to fix: call UpsertDataSources with the DataSource for %q before persisting nomina attributed to it",
					n.Value, schemeStr, entityKey, at.Source, err, at.Source)
			}
		}
	}

	return nil
}

// QueryNomina reads every persisted naming row back as []Nomen, sorted
// deterministically (lessNomen) so a round-trip is order-stable. It LEFT-JOINs the
// nomen_attestations child rows back into each Nomen's Attestations set (LEFT so a
// parent with no attestations still yields a Nomen), grouping child rows by the parent
// triple and sorting each set by the TOTAL lessAttestation key (sortAndDedupAttestations)
// — so the full per-attestation provenance (SourceURL, ArchivedURL, Source, Authority,
// Method, IngestedAt) round-trips losslessly, INCLUDING a curated-authored Authority that differs
// from the scheme/source default (the case the deleted v7 single-attestation bridge could
// not carry). The ResolvesTo EntityRef is reconstructed by PARSING the stored entity_key
// back into its tuple (parseEntityKey); scheme/status/authority/method tokens decode via
// their permissive parsers.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) QueryNomina(ctx context.Context) ([]Nomen, error) {
	type tripleKey struct{ value, scheme, entity string }
	index := make(map[tripleKey]*Nomen)
	var order []tripleKey

	const query = `SELECT n.value, n.scheme, n.entity_key, n.status,
		a.source_url, a.source_id, a.authority, a.method, a.ingested_at, a.archived_url
		FROM nomina n
		LEFT JOIN nomen_attestations a
		  ON n.value = a.value AND n.scheme = a.scheme AND n.entity_key = a.entity_key`
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			k := tripleKey{stmt.GetText("value"), stmt.GetText("scheme"), stmt.GetText("entity_key")}
			n, ok := index[k]
			if !ok {
				var scheme NomenScheme
				if e := scheme.UnmarshalText([]byte(k.scheme)); e != nil {
					scheme = NomenSchemeOther
				}
				status, _ := parseRating(strings.ToLower(stmt.GetText("status")))
				n = &Nomen{
					Value:      k.value,
					Scheme:     scheme,
					Status:     status,
					ResolvesTo: parseEntityKey(k.entity),
				}
				index[k] = n
				order = append(order, k)
			}
			// A LEFT-JOIN row with no matching child has a NULL source_id (GetText → "").
			// A real attestation always carries a non-empty source_id (NOT NULL + FK to
			// data_sources), so an empty source_id marks "no attestation" — skip it.
			sourceID := stmt.GetText("source_id")
			if sourceID == "" {
				return nil
			}
			var authority AttestationAuthority
			if e := authority.UnmarshalText([]byte(stmt.GetText("authority"))); e != nil {
				authority = AuthorityUnknown
			}
			var method IngestMethod
			if e := method.UnmarshalText([]byte(stmt.GetText("method"))); e != nil {
				method = IngestMethodUnknown
			}
			n.Attestations = append(n.Attestations, NomenAttestation{
				SourceURL:   stmt.GetText("source_url"),
				ArchivedURL: stmt.GetText("archived_url"),
				Source:      DataSourceID(sourceID),
				Authority:   authority,
				Method:      method,
				IngestedAt:  stmt.GetText("ingested_at"),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryNomina: read nomina: %w", err)
	}

	out := make([]Nomen, 0, len(order))
	for _, k := range order {
		n := index[k]
		n.Attestations = sortAndDedupAttestations(n.Attestations)
		out = append(out, *n)
	}
	sortNomina(out)
	return out, nil
}

// UpsertCreators populates the v8 creators BCNF dimension from the curated creators.json
// seed (the data_sources dimension-persistence precedent). The seed is the single source
// of truth for Family → Creator, so each row is an INSERT OR REPLACE keyed by family — a
// full replace over the seed is correct and idempotent (re-syncing the same seed yields
// identical rows). Rows are written in family-sorted order so the write is deterministic
// (INV3). Only families whose creator is non-empty and whose family Family.IsKnown() are
// written — the same FK-style consistency the datasource guard enforces; an unmapped
// family is expressed by the ABSENCE of a row (Family.Creator returns CreatorNone), never
// a row with an empty creator.
//
// The persisted table is the STORE's record of what the syncing binary knew; it is read
// back by QueryCreators. It is deliberately distinct from the running binary's Family.Creator
// projection (embedded seed) — see the scanModelInfo skew note.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) UpsertCreators(ctx context.Context) error {
	tbl := loadCreatorTableSafe()
	families := make([]Family, 0, len(tbl.byFamily))
	for f := range tbl.byFamily {
		families = append(families, f)
	}
	sort.Slice(families, func(i, j int) bool { return families[i] < families[j] })

	endFn := sqlitex.Transaction(s.conn)
	var err error
	defer endFn(&err)

	const upsertSQL = `INSERT OR REPLACE INTO creators (family, creator) VALUES (?1, ?2)`
	for _, f := range families {
		creator := tbl.byFamily[f]
		if creator == CreatorNone || !f.IsKnown() {
			// Defensive: parseCreatorTable already rejects both at codegen, but a
			// degraded runtime seed must never persist an empty or unrecognized mapping.
			continue
		}
		err = sqlitex.Execute(s.conn, upsertSQL, &sqlitex.ExecOptions{
			Args: []any{string(f), string(creator)},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertCreators: upsert creator (family=%q, creator=%q): %w\n"+
				"  What: writing a creator dimension row failed\n"+
				"  Why: an unexpected SQLite error on the creators insert\n"+
				"  Where: store.go UpsertCreators, creators insert\n"+
				"  How to fix: verify the v8 creators table exists (OpenStore migration)",
				f, creator, err)
		}
	}
	return nil
}

// QueryCreators reads the persisted v8 creators dimension back as a Family → Creator map.
// It is the JOIN-queryable, self-describing read of the store's own creator record (the
// Entity.Sources projection precedent at the dimension level): an external SQL consumer or
// a downstream read that wants the store's persisted mapping reads it here, distinct from
// the running binary's Family.Creator projection over the embedded seed. Returns an empty
// (non-nil) map when the dimension is unpopulated.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) QueryCreators(ctx context.Context) (map[Family]Creator, error) {
	out := make(map[Family]Creator)
	const query = `SELECT family, creator FROM creators`
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			out[Family(stmt.GetText("family"))] = Creator(stmt.GetText("creator"))
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryCreators: read creators: %w", err)
	}
	return out, nil
}

// UpsertEntityMetadata writes each EntityMetadata and its benchmark / link
// children in a single transaction. The children of a metadata_id are a
// REPLACEABLE SET owned by their parent: per row the method (1) INSERT OR REPLACE
// the parent entity_metadata row, (2) DELETE the existing metadata_benchmarks and
// metadata_links for that metadata_id, then (3) INSERT the current children.
//
// This order is foreign-key safe under foreign_keys=ON: INSERT OR REPLACE keeps
// the same metadata_id, so at the end of that statement any pre-existing children
// still resolve their parent (the FK is checked at statement end, and a parent
// with the same key exists). The subsequent delete-then-insert then refreshes the
// child set. Re-syncing identical metadata is idempotent by CONTENT — the row
// counts and values are unchanged.
//
// Enum-typed columns persist their STRING forms: a link's type column stores
// LinkType.String() and type_raw carries the verbatim upstream token (populated
// only when the type is the other bucket). entity_metadata has no status column
// (status is not present on the models.json side). The parent's source_id is a
// foreign key into data_sources, so callers must register the DataSource (via
// UpsertDataSources) before attaching metadata attributed to it.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) UpsertEntityMetadata(ctx context.Context, rows []EntityMetadata) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	const parentSQL = `INSERT OR REPLACE INTO entity_metadata (
		metadata_id, name, description, license, source_id, last_synced, raw_family
	) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7)`
	const delBenchSQL = `DELETE FROM metadata_benchmarks WHERE metadata_id = ?1`
	const delLinkSQL = `DELETE FROM metadata_links WHERE metadata_id = ?1`
	const insBenchSQL = `INSERT INTO metadata_benchmarks (
		metadata_id, name, version, variant, dataset, harness, metric, score, score_raw, source_url, date
	) VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9, ?10, ?11)`
	const insLinkSQL = `INSERT INTO metadata_links (
		metadata_id, label, url, type, type_raw
	) VALUES (?1, ?2, ?3, ?4, ?5)`

	for i := range rows {
		m := &rows[i]
		mid := string(m.MetadataID)

		// (1) Parent row. source_id is a FK into data_sources.
		err = sqlitex.Execute(s.conn, parentSQL, &sqlitex.ExecOptions{
			Args: []any{mid, m.Name, m.Description, m.License, string(m.Source), m.LastSynced, string(m.RawFamily)},
		})
		if err != nil {
			return fmt.Errorf("bestiary: UpsertEntityMetadata: upsert entity_metadata %q: %w\n"+
				"  What: writing the entity-metadata parent row failed\n"+
				"  Why: the source_id foreign key has no matching data_sources row, or a constraint was violated\n"+
				"  Where: store.go UpsertEntityMetadata, entity_metadata insert\n"+
				"  How to fix: register the DataSource for %q via UpsertDataSources before attaching its metadata",
				mid, err, m.Source)
		}

		// (2) Clear the replaceable child set for this metadata_id.
		if err = sqlitex.Execute(s.conn, delBenchSQL, &sqlitex.ExecOptions{Args: []any{mid}}); err != nil {
			return fmt.Errorf("bestiary: UpsertEntityMetadata: clear metadata_benchmarks for %q: %w\n"+
				"  What: deleting the prior benchmark rows failed\n"+
				"  Why: an unexpected SQLite error during the replace-set refresh\n"+
				"  Where: store.go UpsertEntityMetadata, metadata_benchmarks delete\n"+
				"  How to fix: verify the v6 metadata tables exist (OpenStore migration)",
				mid, err)
		}
		if err = sqlitex.Execute(s.conn, delLinkSQL, &sqlitex.ExecOptions{Args: []any{mid}}); err != nil {
			return fmt.Errorf("bestiary: UpsertEntityMetadata: clear metadata_links for %q: %w\n"+
				"  What: deleting the prior link rows failed\n"+
				"  Why: an unexpected SQLite error during the replace-set refresh\n"+
				"  Where: store.go UpsertEntityMetadata, metadata_links delete\n"+
				"  How to fix: verify the v6 metadata tables exist (OpenStore migration)",
				mid, err)
		}

		// (3) Insert the current children in slice order (their rowid becomes the
		// stable read-back order QueryEntityMetadata reassembles).
		for j := range m.Benchmarks {
			b := &m.Benchmarks[j]
			err = sqlitex.Execute(s.conn, insBenchSQL, &sqlitex.ExecOptions{
				Args: []any{
					mid, b.Name, b.Version, b.Variant, b.Dataset,
					b.Harness, b.Metric, b.Score, b.ScoreRaw, b.SourceURL, b.Date,
				},
			})
			if err != nil {
				return fmt.Errorf("bestiary: UpsertEntityMetadata: insert benchmark %q for %q: %w\n"+
					"  What: writing a benchmark child row failed\n"+
					"  Why: the metadata_id foreign key has no parent, or a constraint was violated\n"+
					"  Where: store.go UpsertEntityMetadata, metadata_benchmarks insert\n"+
					"  How to fix: ensure the parent entity_metadata row was written first (it is, within this txn)",
					b.Name, mid, err)
			}
		}
		for j := range m.Links {
			l := &m.Links[j]
			err = sqlitex.Execute(s.conn, insLinkSQL, &sqlitex.ExecOptions{
				Args: []any{mid, l.Label, l.URL, l.Type.String(), l.TypeRaw},
			})
			if err != nil {
				return fmt.Errorf("bestiary: UpsertEntityMetadata: insert link %q for %q: %w\n"+
					"  What: writing a link child row failed\n"+
					"  Why: the metadata_id foreign key has no parent, or a constraint was violated\n"+
					"  Where: store.go UpsertEntityMetadata, metadata_links insert\n"+
					"  How to fix: ensure the parent entity_metadata row was written first (it is, within this txn)",
					l.URL, mid, err)
			}
		}
	}

	return nil
}

// QueryCurrentIngests returns the CURRENT ingest of every source: the row with the
// maximum ingested_at per data_source_id, sorted ascending by data_source_id. It
// reads the append-only dataset_ingested history and collapses it to one current
// row per source (the ingest-log analogue of DatasetIngestedFor's MAX, across all
// sources at once).
//
// The query relies on SQLite's documented bare-column rule: with a single MAX()
// aggregate and a GROUP BY, the bare parser_schema column takes its value from the
// same row that supplied the maximum ingested_at, so the returned parser_schema is
// the current ingest's, not an arbitrary row's.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) QueryCurrentIngests(ctx context.Context) ([]DatasetIngested, error) {
	const query = `SELECT data_source_id, MAX(ingested_at) AS ingested_at, parser_schema
		FROM dataset_ingested
		GROUP BY data_source_id
		ORDER BY data_source_id`

	var out []DatasetIngested
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			out = append(out, DatasetIngested{
				SourceID:     DataSourceID(stmt.GetText("data_source_id")),
				IngestedAt:   stmt.GetText("ingested_at"),
				ParserSchema: int(stmt.GetInt64("parser_schema")),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryCurrentIngests: %w", err)
	}
	return out, nil
}

// QueryIngestHistory returns every ingest row for a single source, sorted ascending
// by ingested_at (oldest first). It is the per-source history view behind
// `sources --history`; an empty slice (not an error) is returned when the source
// has no ingest rows.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) QueryIngestHistory(ctx context.Context, id DataSourceID) ([]DatasetIngested, error) {
	const query = `SELECT data_source_id, ingested_at, parser_schema
		FROM dataset_ingested
		WHERE data_source_id = ?1
		ORDER BY ingested_at ASC`

	var out []DatasetIngested
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		Args: []any{string(id)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			out = append(out, DatasetIngested{
				SourceID:     DataSourceID(stmt.GetText("data_source_id")),
				IngestedAt:   stmt.GetText("ingested_at"),
				ParserSchema: int(stmt.GetInt64("parser_schema")),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryIngestHistory(%q): %w", string(id), err)
	}
	return out, nil
}

// QueryEntityMetadata returns all cached entity metadata with its benchmark and
// link children reassembled onto each parent. The order is deterministic: parents
// ascending by metadata_id, and within a parent the children in insertion order
// (rowid). Enum-typed columns are decoded back from their stored string forms.
//
// The reassembly is three sequential passes (parents, then benchmarks, then
// links): zombiezen.com/go/sqlite does not permit issuing a new statement while a
// result cursor is open, so nested per-parent child queries are not possible; the
// flat child passes are keyed back onto their parent by metadata_id.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support
// per-operation context cancellation.
func (s *Store) QueryEntityMetadata(ctx context.Context) ([]EntityMetadata, error) {
	// Pass 1: parents, ordered by metadata_id. Build the slice and an index from
	// metadata_id to its position so the child passes can append in place.
	var out []EntityMetadata
	idx := map[MetadataID]int{}
	const parentQuery = `SELECT metadata_id, name, description, license, source_id, last_synced, raw_family
		FROM entity_metadata
		ORDER BY metadata_id`
	err := sqlitex.Execute(s.conn, parentQuery, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			mid := MetadataID(stmt.GetText("metadata_id"))
			idx[mid] = len(out)
			out = append(out, EntityMetadata{
				MetadataID:  mid,
				Name:        stmt.GetText("name"),
				Description: stmt.GetText("description"),
				License:     stmt.GetText("license"),
				Source:      DataSourceID(stmt.GetText("source_id")),
				LastSynced:  stmt.GetText("last_synced"),
				RawFamily:   Family(stmt.GetText("raw_family")),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryEntityMetadata: read parents: %w", err)
	}

	// Pass 2: benchmarks, ordered by (metadata_id, rowid) so each parent's set
	// reassembles in insertion order.
	const benchQuery = `SELECT metadata_id, name, version, variant, dataset, harness, metric, score, score_raw, source_url, date
		FROM metadata_benchmarks
		ORDER BY metadata_id, rowid`
	err = sqlitex.Execute(s.conn, benchQuery, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			mid := MetadataID(stmt.GetText("metadata_id"))
			pos, ok := idx[mid]
			if !ok {
				// A child with no parent cannot occur under the FK, but skip rather
				// than panic if the store was hand-edited with foreign_keys off.
				return nil
			}
			out[pos].Benchmarks = append(out[pos].Benchmarks, BenchmarkResult{
				Name:      stmt.GetText("name"),
				Version:   stmt.GetText("version"),
				Variant:   stmt.GetText("variant"),
				Dataset:   stmt.GetText("dataset"),
				Harness:   stmt.GetText("harness"),
				Metric:    stmt.GetText("metric"),
				Score:     stmt.GetFloat("score"),
				ScoreRaw:  stmt.GetText("score_raw"),
				SourceURL: stmt.GetText("source_url"),
				Date:      stmt.GetText("date"),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryEntityMetadata: read benchmarks: %w", err)
	}

	// Pass 3: links, same (metadata_id, rowid) order. The stored type string decodes
	// back to a LinkType; an unexpected token degrades to LinkOther rather than
	// failing the read (the verbatim token is preserved in type_raw regardless).
	const linkQuery = `SELECT metadata_id, label, url, type, type_raw
		FROM metadata_links
		ORDER BY metadata_id, rowid`
	err = sqlitex.Execute(s.conn, linkQuery, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			mid := MetadataID(stmt.GetText("metadata_id"))
			pos, ok := idx[mid]
			if !ok {
				return nil
			}
			var lt LinkType
			if uerr := lt.UnmarshalText([]byte(stmt.GetText("type"))); uerr != nil {
				lt = LinkOther
			}
			out[pos].Links = append(out[pos].Links, ModelLink{
				Label:   stmt.GetText("label"),
				URL:     stmt.GetText("url"),
				Type:    lt,
				TypeRaw: stmt.GetText("type_raw"),
			})
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryEntityMetadata: read links: %w", err)
	}

	return out, nil
}

// QueryModels returns all cached models. If provider is non-empty, results are
// filtered to only models from that provider. An empty provider string returns
// ALL models regardless of provider.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support per-operation context cancellation.
func (s *Store) QueryModels(ctx context.Context, provider Provider) ([]ModelInfo, error) {
	var (
		query string
		args  []any
	)

	if provider == "" {
		query = `SELECT ` + modelColumns + ` FROM models`
		args = nil
	} else {
		query = `SELECT ` + modelColumns + ` FROM models WHERE provider = ?1`
		args = []any{string(provider)}
	}

	var models []ModelInfo
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			m := scanModelInfo(stmt)
			models = append(models, m)
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryModels(provider=%q): %w", string(provider), err)
	}
	return models, nil
}

// QueryModel returns the first model found with the given ID, or ErrNotFound
// if no model with that ID exists in the store.
// Note: with the composite (model_id, provider) primary key, multiple rows may
// share the same model_id across different providers. Use QueryModelsByID to
// retrieve all provider variants for a given model ID.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support per-operation context cancellation.
func (s *Store) QueryModel(ctx context.Context, id ModelID) (ModelInfo, error) {
	const query = `SELECT ` + modelColumns + ` FROM models WHERE model_id = ?1 LIMIT 1`

	var found bool
	var result ModelInfo
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		Args: []any{string(id)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			result = scanModelInfo(stmt)
			found = true
			return nil
		},
	})
	if err != nil {
		return ModelInfo{}, fmt.Errorf("bestiary: QueryModel(%q): %w", string(id), err)
	}
	if !found {
		return ModelInfo{}, &ErrNotFound{What: "model", Key: string(id)}
	}
	return result, nil
}

// QueryModelsByID returns all cached models with the given ID across all
// providers. Returns an empty slice (not an error) if none are found.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support per-operation context cancellation.
func (s *Store) QueryModelsByID(ctx context.Context, id ModelID) ([]ModelInfo, error) {
	const query = `SELECT ` + modelColumns + ` FROM models WHERE model_id = ?1`

	var models []ModelInfo
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		Args: []any{string(id)},
		ResultFunc: func(stmt *sqlite.Stmt) error {
			models = append(models, scanModelInfo(stmt))
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryModelsByID(%q): %w", string(id), err)
	}
	return models, nil
}

// QueryByCanonical returns ModelInfo entries matching the canonical axes in f.
// Cross-provider results are returned as a slice. Empty fields in f act as
// wildcards: an empty Family matches any family, an empty Variant matches any
// variant, and an empty Date matches any date.
// Returns an empty slice (not an error) when no matching models are found.
//
// The query uses the (family, variant) prefix of idx_canonical for efficient
// lookup when f.Family is non-empty.
//
// ctx is accepted for API compatibility; zombiezen.com/go/sqlite does not support per-operation context cancellation.
func (s *Store) QueryByCanonical(ctx context.Context, f CanonicalFilter) ([]ModelInfo, error) {
	// Build a dynamic WHERE clause: only include predicates for non-empty fields.
	var conditions []string
	var args []any
	paramIdx := 1

	if f.Family != "" {
		conditions = append(conditions, fmt.Sprintf("family = ?%d", paramIdx))
		args = append(args, string(f.Family))
		paramIdx++
	}
	if f.Variant != "" {
		conditions = append(conditions, fmt.Sprintf("variant = ?%d", paramIdx))
		args = append(args, f.Variant)
		paramIdx++
	}
	if f.Version != "" {
		conditions = append(conditions, fmt.Sprintf("version = ?%d", paramIdx))
		args = append(args, f.Version)
		paramIdx++
	}
	if f.Date != "" {
		conditions = append(conditions, fmt.Sprintf("date = ?%d", paramIdx))
		args = append(args, f.Date)
		paramIdx++
	}

	query := `SELECT ` + modelColumns + ` FROM models`

	if len(conditions) > 0 {
		query += "\n\tWHERE " + strings.Join(conditions, " AND ")
	}

	var models []ModelInfo
	err := sqlitex.Execute(s.conn, query, &sqlitex.ExecOptions{
		Args: args,
		ResultFunc: func(stmt *sqlite.Stmt) error {
			models = append(models, scanModelInfo(stmt))
			return nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("bestiary: QueryByCanonical(family=%q, variant=%q, version=%q, date=%q): %w",
			string(f.Family), f.Variant, f.Version, f.Date, err)
	}
	return models, nil
}

// --- helpers ---

// boolToInt converts a bool to 0 or 1 for SQLite INTEGER storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// capabilityConfigToString serialises a Capability.Config map to a JSON string
// for TEXT column storage. Returns "" when cfg is nil or empty.
func capabilityConfigToString(cfg map[string]string) string {
	if len(cfg) == 0 {
		return ""
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// configFromString deserialises a JSON string back to a map[string]string.
// Returns nil for an empty string. Malformed JSON is silently ignored (returns nil).
func configFromString(s string) map[string]string {
	if s == "" {
		return nil
	}
	var cfg map[string]string
	_ = json.Unmarshal([]byte(s), &cfg)
	return cfg
}

// derefFloat64 converts *float64 to any: nil → nil (SQL NULL), non-nil → float64 value.
func derefFloat64(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// modalitiesToString serialises a []Modality slice to a comma-separated string
// (e.g., "text,image"). An empty slice returns "".
func modalitiesToString(ms []Modality) string {
	if len(ms) == 0 {
		return ""
	}
	parts := make([]string, len(ms))
	for i, m := range ms {
		parts[i] = m.String()
	}
	return strings.Join(parts, ",")
}

// modalitiesFromString parses a comma-separated modality string back to
// []Modality. Unknown modality names are silently skipped.
func modalitiesFromString(s string) []Modality {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]Modality, 0, len(parts))
	for _, p := range parts {
		var m Modality
		if err := m.UnmarshalText([]byte(p)); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// scanModelInfo reads a ModelInfo from the current prepared statement row.
// Column order must match the SELECT in QueryModels / QueryModel / QueryByCanonical.
func scanModelInfo(stmt *sqlite.Stmt) ModelInfo {
	m := ModelInfo{
		ID:               ModelID(stmt.GetText("model_id")),
		Provider:         Provider(stmt.GetText("provider")),
		DisplayName:      stmt.GetText("display_name"),
		RawFamily:        Family(stmt.GetText("raw_family")),
		Family:           Family(stmt.GetText("family")),
		Variant:          stmt.GetText("variant"),
		Version:          stmt.GetText("version"),
		Date:             stmt.GetText("date"),
		ContextWindow:    int(stmt.GetInt64("context_window")),
		MaxOutput:        int(stmt.GetInt64("max_output")),
		Reasoning:        stmt.GetBool("reasoning"),
		ToolCall:         stmt.GetBool("tool_call"),
		Attachment:       stmt.GetBool("attachment"),
		Temperature:      stmt.GetBool("temperature"),
		StructuredOutput: stmt.GetBool("structured_output"),
		Interleaved: Capability{
			Supported: stmt.GetBool("interleaved"),
			Config:    configFromString(stmt.GetText("interleaved_config")),
		},
		OpenWeights: stmt.GetBool("open_weights"),
		ReleaseDate: stmt.GetText("release_date"),
		Knowledge:   stmt.GetText("knowledge"),
		LastSynced:  stmt.GetText("last_synced"),
		Description: stmt.GetText("description"),
	}

	// Instance-level release status: the enum string form decodes back to a
	// ModelStatus, and the StatusOther fail-safe carries its verbatim raw token.
	m.Status, m.StatusRaw = modelStatusFromStore(stmt.GetText("status"), stmt.GetText("status_raw"))

	// Reasoning options and cost tiers: JSON-encoded TEXT columns.
	m.ReasoningOptions = reasoningOptionsFromString(stmt.GetText("reasoning_options"))
	m.CostContextOver200k = tierCostPtrFromString(stmt.GetText("cost_context_over_200k"))
	m.CostTiers = costTiersFromString(stmt.GetText("cost_tiers"))

	// Per-instance region: the Region String() token decodes back via UnmarshalText
	// (permissive — accepts every token the store can hold, including "other" and the
	// empty/"unspecified" spellings of RegionNone). A malformed token degrades to
	// RegionNone rather than failing the scan. RegionRaw carries the RegionOther token.
	m.Region = regionFromStore(stmt.GetText("region"))
	m.RegionRaw = stmt.GetText("region_raw")

	// Creator is the DERIVED join projection of Family → Creator (the Entity.Sources /
	// registry.go precedent): it is NOT stored on the models row (Family → Creator is a
	// functional dependency, so a models.creator column would be a transitive-dependency
	// BCNF violation), so it is projected here from the row's own Family via the curated
	// seed. CreatorNone when the family has no mapping.
	//
	// SKEW NOTE (the constraint a store user must know): this projection reads THIS
	// binary's EMBEDDED seed, whereas the persisted creators dimension (QueryCreators)
	// records what the SYNCING binary knew. They agree by construction when the reading
	// and syncing binaries share a seed, but an OLD binary reading a NEWER cache can
	// disagree until upgraded. Authority is role-split: the creators table is the STORE's
	// persisted record (read it via QueryCreators); this projection is the RUNNING binary's
	// current view. Neither is "wrong" — they answer different questions.
	m.Creator = m.Family.Creator()

	// Source is the ORIGINATING ingest source of the row. The models cache table has
	// no source column: it is populated exclusively by `sync`, which reads the
	// models.dev catalog, so every persisted row originates from models.dev. A row
	// therefore reads back as DataSourceModelsDev (equivalently: a not-persisted /
	// empty carrier defaults to models.dev), matching the load-layer fill-in the
	// static registry applies (registry.go init) so the store and static paths agree
	// on the implicit origin rather than surfacing an empty Source to the caller.
	m.Source = DataSourceModelsDev

	// Nullable REAL fields.
	if !stmt.IsNull("cost_input") {
		v := stmt.GetFloat("cost_input")
		m.CostInputPerMTok = &v
	}
	if !stmt.IsNull("cost_output") {
		v := stmt.GetFloat("cost_output")
		m.CostOutputPerMTok = &v
	}
	if !stmt.IsNull("cost_reasoning") {
		v := stmt.GetFloat("cost_reasoning")
		m.CostReasoningPerMTok = &v
	}
	if !stmt.IsNull("cost_cache_read") {
		v := stmt.GetFloat("cost_cache_read")
		m.CostCacheReadPerMTok = &v
	}
	if !stmt.IsNull("cost_cache_write") {
		v := stmt.GetFloat("cost_cache_write")
		m.CostCacheWritePerMTok = &v
	}
	if !stmt.IsNull("cost_input_audio") {
		v := stmt.GetFloat("cost_input_audio")
		m.CostInputAudioPerMTok = &v
	}
	if !stmt.IsNull("cost_output_audio") {
		v := stmt.GetFloat("cost_output_audio")
		m.CostOutputAudioPerMTok = &v
	}

	// Modalities: comma-separated text columns.
	m.Modalities = Modalities{
		Input:  modalitiesFromString(stmt.GetText("modalities_input")),
		Output: modalitiesFromString(stmt.GetText("modalities_output")),
	}

	// Size enrichment joint (READ-path only): the models table has no param_size
	// column, so ParamSize + the shape ints are re-derived from the ID here, exactly
	// as the wire decode joint does. This keeps the store at schema v6 — enrichment
	// is a pure function of the ID, never persisted state.
	enrichModelInfo(&m)

	return m
}

// modelStatusToStore encodes a (Status, StatusRaw) pair for persistence: the
// status column stores the enum's canonical string form (e.g. "none", "beta",
// "other") and status_raw carries the verbatim upstream token, which is
// meaningful only for StatusOther. An out-of-range status (a programming error)
// degrades to the empty string so the write never fails.
func modelStatusToStore(status ModelStatus, statusRaw string) (string, string) {
	if !status.IsKnown() {
		return "", ""
	}
	if status == StatusOther {
		return status.String(), statusRaw
	}
	// Named statuses (including StatusNone) carry no raw token.
	return status.String(), ""
}

// modelStatusFromStore decodes the stored status/status_raw columns back to a
// (ModelStatus, StatusRaw) pair. StatusOther is reconstructed with its verbatim
// raw token; named statuses (and the empty default from a pre-v6 migrated row)
// round-trip via ParseModelStatus. An unrecognized token degrades to StatusNone
// rather than failing the read.
func modelStatusFromStore(statusStr, statusRaw string) (ModelStatus, string) {
	if statusStr == StatusOther.String() {
		return StatusOther, statusRaw
	}
	if st, err := ParseModelStatus(statusStr); err == nil {
		return st, ""
	}
	return StatusNone, ""
}

// reasoningOptionsToString / reasoningOptionsFromString serialise the
// ReasoningOptions slice to/from a JSON TEXT column. An empty slice stores the
// empty string (keeping the column's empty-string default) rather than a JSON null.
// The ReasoningOptionKind discriminant round-trips as its wire name via its
// Text(Un)Marshaler, so ReasoningOptionOther + KindRaw survive faithfully.
func reasoningOptionsToString(opts []ReasoningOption) string {
	if len(opts) == 0 {
		return ""
	}
	b, _ := json.Marshal(opts)
	return string(b)
}

func reasoningOptionsFromString(s string) []ReasoningOption {
	if s == "" {
		return nil
	}
	var out []ReasoningOption
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// costTiersToString / costTiersFromString serialise the CostTiers slice to/from a
// JSON TEXT column, with the same empty-slice-stores-empty-string convention. The
// embedded TierCost's *float64 axes round-trip as JSON null/number.
func costTiersToString(tiers []CostTier) string {
	if len(tiers) == 0 {
		return ""
	}
	b, _ := json.Marshal(tiers)
	return string(b)
}

func costTiersFromString(s string) []CostTier {
	if s == "" {
		return nil
	}
	var out []CostTier
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

// tierCostPtrToString / tierCostPtrFromString serialise the optional
// context_over_200k tier to/from a JSON TEXT column: a nil pointer stores the
// empty string (matching the empty-string default), a non-nil one stores the
// marshalled TierCost.
func tierCostPtrToString(t *TierCost) string {
	if t == nil {
		return ""
	}
	b, _ := json.Marshal(t)
	return string(b)
}

func tierCostPtrFromString(s string) *TierCost {
	if s == "" {
		return nil
	}
	var t TierCost
	if err := json.Unmarshal([]byte(s), &t); err != nil {
		return nil
	}
	return &t
}
