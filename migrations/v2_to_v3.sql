-- Migration: schema version 2 → 3
-- Bestiary entity normalization pipeline (deliverable 11)
--
-- PURPOSE: This file is a REFERENCE / EXTERNAL-DB-TOOL ONLY document.
-- The Go store performs all migration logic in store.go via migrateToV3().
-- Do NOT execute this file in place of the Go migration; it is provided
-- for documentation and for operators who need to run the migration
-- against an external SQLite database using standard SQL tooling.
--
-- CHANGES INTRODUCED IN v3:
--   1. Rename existing `family` column to `raw_family`
--      (preserves the raw API family value from models.dev).
--   2. Add NEW `family` TEXT column
--      (parsed canonical family, populated by parse.ParseFamily(raw_family)).
--   3. Add NEW `variant` TEXT column
--      (variant suffix, e.g. "opus", "flash", populated by parse.ParseFamily).
--   4. Add NEW `date` TEXT column in YYYY-MM-DD format
--      (extracted from model_id or release_date by parse.ExtractDate).
--   5. Create idx_canonical index on (family, variant, provider)
--      for efficient QueryByCanonical lookups.
--
-- SCHEMA_META: After this migration, set version = 3 in schema_meta:
--   DELETE FROM schema_meta;
--   INSERT INTO schema_meta (version) VALUES (3);
--
-- NOTE: SQLite supports column rename via ALTER TABLE ... RENAME COLUMN
-- only from version 3.25.0 (2018-09-15). The Go migration instead uses
-- the table-recreate pattern (create new + copy + drop + rename) for
-- broader compatibility. This SQL file shows both approaches; use the
-- one appropriate for your SQLite version.

-- =============================================================================
-- APPROACH A: Table-recreate (compatible with SQLite < 3.25, recommended)
-- =============================================================================

BEGIN;

-- Step 1: Create new table with v3 schema.
CREATE TABLE IF NOT EXISTS models_new (
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
);

-- Step 2: Copy rows; map old family → raw_family; new columns default to ''.
INSERT OR IGNORE INTO models_new
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
    FROM models;

-- Step 3: Replace old table with new.
DROP TABLE models;
ALTER TABLE models_new RENAME TO models;

-- Step 4: Backfill family/variant/date.
-- NOTE: This step requires application-level logic (parse.ParseFamily,
-- parse.ExtractDate). The Go migration performs this in-process.
-- For external tooling, populate the columns after this transaction using
-- the Go CLI: `bestiary sync` will re-populate normalized fields on next sync,
-- or you may run custom SQL UPDATE statements using your own parsing logic.
--
-- Minimal stub for reference (replace with real parsed values):
-- UPDATE models SET family = raw_family, variant = '', date = '' WHERE 1=1;

-- Step 5: Create the canonical index.
CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, provider);

-- Step 6: Record the new schema version.
DELETE FROM schema_meta;
INSERT INTO schema_meta (version) VALUES (3);

COMMIT;


-- =============================================================================
-- APPROACH B: ALTER TABLE (requires SQLite >= 3.25.0)
-- =============================================================================
-- This approach is NOT used by the Go migration but is provided for reference.
-- It is shorter but less portable. If using this approach, also add the new
-- columns and index manually as shown.

-- BEGIN;
--
-- ALTER TABLE models RENAME COLUMN family TO raw_family;
-- ALTER TABLE models ADD COLUMN family            TEXT NOT NULL DEFAULT '';
-- ALTER TABLE models ADD COLUMN variant           TEXT NOT NULL DEFAULT '';
-- ALTER TABLE models ADD COLUMN date              TEXT NOT NULL DEFAULT '';
--
-- -- Backfill: see note above; this stub sets family = raw_family as a placeholder.
-- UPDATE models SET family = raw_family, variant = '', date = '';
--
-- CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, provider);
--
-- DELETE FROM schema_meta;
-- INSERT INTO schema_meta (version) VALUES (3);
--
-- COMMIT;
