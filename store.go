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
const currentSchemaVersion = 4

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
	} else if fromVersion < 2 {
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
//   - Adds `version TEXT NOT NULL DEFAULT ''` column for the model version
//     extracted from the family string (e.g. "4.5" for claude-opus-4-5).
//   - Drops the v3 idx_canonical (family, variant, provider) index and
//     recreates it as (family, variant, version, provider) so that version
//     is a first-class lookup axis.
//
// SQLite supports ADD COLUMN via ALTER TABLE for NOT NULL columns with a
// constant DEFAULT value, so table-recreate is not required here.
// The new column defaults to '' for all existing rows; a subsequent sync
// operation will backfill NormalizedVersion from the parser.
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
				string(m.Family),
				string(m.NormalizedFamily),
				m.NormalizedVariant,
				m.NormalizedVersion,
				m.NormalizedDate,
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
		ID:                ModelID(stmt.GetText("model_id")),
		Provider:          Provider(stmt.GetText("provider")),
		DisplayName:       stmt.GetText("display_name"),
		Family:            Family(stmt.GetText("raw_family")),
		NormalizedFamily:  Family(stmt.GetText("family")),
		NormalizedVariant: stmt.GetText("variant"),
		NormalizedVersion: stmt.GetText("version"),
		NormalizedDate:    stmt.GetText("date"),
		ContextWindow:     int(stmt.GetInt64("context_window")),
		MaxOutput:         int(stmt.GetInt64("max_output")),
		Reasoning:         stmt.GetBool("reasoning"),
		ToolCall:          stmt.GetBool("tool_call"),
		Attachment:        stmt.GetBool("attachment"),
		Temperature:       stmt.GetBool("temperature"),
		StructuredOutput:  stmt.GetBool("structured_output"),
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
