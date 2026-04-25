-- Migration: schema version 3 → 4
-- Bestiary entity normalization pipeline — Version field addition
--
-- PURPOSE: This file is a REFERENCE / EXTERNAL-DB-TOOL ONLY document.
-- The Go store performs all migration logic in store.go via migrateToV4().
-- Do NOT execute this file in place of the Go migration; it is provided
-- for documentation and for operators who need to run the migration
-- against an external SQLite database using standard SQL tooling.
--
-- CHANGES INTRODUCED IN v4:
--   1. Add NEW `version` TEXT column (NOT NULL DEFAULT '')
--      Holds the model version extracted from the family string
--      (e.g. "4.5" for claude-opus-4-5, "2.5" for gemini-2.5-flash).
--      Empty for models with no separable version.
--   2. Drop and recreate idx_canonical as (family, variant, version, provider)
--      The version axis is now part of the canonical key, making Opus 4.5
--      vs Opus 4.6 distinguishable without relying solely on the snapshot date.
--
-- SCHEMA_META: After this migration, set version = 4 in schema_meta:
--   DELETE FROM schema_meta;
--   INSERT INTO schema_meta (version) VALUES (4);
--
-- NOTE: SQLite supports ADD COLUMN via ALTER TABLE for simple cases (no PK,
-- no UNIQUE, no non-constant DEFAULT). The `version` column qualifies, so
-- we use ALTER TABLE directly rather than table-recreate.

BEGIN;

-- Step 1: Add the version column (defaults to '' for all existing rows).
ALTER TABLE models ADD COLUMN version TEXT NOT NULL DEFAULT '';

-- Step 2: Drop the old canonical index (covers family, variant, provider).
DROP INDEX IF EXISTS idx_canonical;

-- Step 3: Recreate the index to include version.
CREATE INDEX IF NOT EXISTS idx_canonical ON models(family, variant, version, provider);

-- Step 4: Backfill version values.
-- NOTE: This step requires application-level parsing (ExtractVersion).
-- The Go migration performs this in-process via migrateToV4().
-- For external tooling, populate the column after this transaction using
-- the Go CLI: `bestiary sync` will re-populate normalized fields on next sync,
-- or you may run custom SQL UPDATE statements using your own parsing logic.
--
-- Minimal stub for reference (leaves version empty, sync will correct it):
-- UPDATE models SET version = '' WHERE 1=1;

-- Step 5: Record the new schema version.
DELETE FROM schema_meta;
INSERT INTO schema_meta (version) VALUES (4);

COMMIT;
