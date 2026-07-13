package bestiary

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
const currentSchemaVersion = 6

// schemaMetaSQL creates the schema_meta table used to track migration state.
// Safe to run on any existing database (CREATE TABLE IF NOT EXISTS).
const schemaMetaSQL = `CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL);`

// schemaSQL defines the current (v4) models table schema.
// Used only for fresh databases; existing databases go through migrateSchema.
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
    PRIMARY KEY (model_id, provider)
);`

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
const entityMetadataTableSQL = `CREATE TABLE IF NOT EXISTS entity_metadata (
    metadata_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    license     TEXT NOT NULL DEFAULT '',
    source_id   TEXT NOT NULL REFERENCES data_sources(data_source_id),
    last_synced TEXT NOT NULL DEFAULT ''
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

	// Step 2: add the three entity-metadata tables.
	if err = createMetadataTables(conn); err != nil {
		return fmt.Errorf("create entity-metadata tables: %w", err)
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
		last_synced
	) VALUES (
		?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8,
		?9, ?10,
		?11, ?12, ?13, ?14, ?15, ?16, ?17, ?18,
		?19, ?20, ?21, ?22, ?23,
		?24, ?25,
		?26, ?27,
		?28
	)`

	for i := range models {
		m := &models[i]
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
		metadata_id, name, description, license, source_id, last_synced
	) VALUES (?1, ?2, ?3, ?4, ?5, ?6)`
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
			Args: []any{mid, m.Name, m.Description, m.License, string(m.Source), m.LastSynced},
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
	const parentQuery = `SELECT metadata_id, name, description, license, source_id, last_synced
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
		query = `SELECT
			model_id, provider, display_name, raw_family, family, variant, version, date,
			context_window, max_output,
			reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
			cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
			release_date, knowledge,
			modalities_input, modalities_output,
			last_synced
		FROM models`
		args = nil
	} else {
		query = `SELECT
			model_id, provider, display_name, raw_family, family, variant, version, date,
			context_window, max_output,
			reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
			cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
			release_date, knowledge,
			modalities_input, modalities_output,
			last_synced
		FROM models
		WHERE provider = ?1`
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
	const query = `SELECT
		model_id, provider, display_name, raw_family, family, variant, version, date,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced
	FROM models
	WHERE model_id = ?1
	LIMIT 1`

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
	const query = `SELECT
		model_id, provider, display_name, raw_family, family, variant, version, date,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced
	FROM models
	WHERE model_id = ?1`

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

	query := `SELECT
		model_id, provider, display_name, raw_family, family, variant, version, date,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, interleaved_config, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced
	FROM models`

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
	}

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

	// Modalities: comma-separated text columns.
	m.Modalities = Modalities{
		Input:  modalitiesFromString(stmt.GetText("modalities_input")),
		Output: modalitiesFromString(stmt.GetText("modalities_output")),
	}

	return m
}
