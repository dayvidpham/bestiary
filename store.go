package bestiary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

const schemaSQL = `CREATE TABLE IF NOT EXISTS models (
    model_id          TEXT PRIMARY KEY,
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
    last_synced       TEXT NOT NULL
);`

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
// The models table is created if it does not already exist.
// Caller must call Close when done.
func OpenStore(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("bestiary: OpenStore: create parent dirs for %s: %w", path, err)
	}

	conn, err := sqlite.OpenConn(path)
	if err != nil {
		return nil, fmt.Errorf("bestiary: OpenStore: open %s: %w", path, err)
	}

	if err := sqlitex.ExecuteTransient(conn, schemaSQL, nil); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("bestiary: OpenStore: create schema in %s: %w", path, err)
	}

	return &Store{conn: conn, path: path}, nil
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
func (s *Store) UpsertModels(ctx context.Context, models []ModelInfo) error {
	endFn := sqlitex.Transaction(s.conn)

	var err error
	defer endFn(&err)

	now := time.Now().UTC().Format(time.RFC3339)

	const upsertSQL = `INSERT OR REPLACE INTO models (
		model_id, provider, display_name, family,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced
	) VALUES (
		?1, ?2, ?3, ?4,
		?5, ?6,
		?7, ?8, ?9, ?10, ?11, ?12, ?13,
		?14, ?15, ?16, ?17, ?18,
		?19, ?20,
		?21, ?22,
		?23
	)`

	for i := range models {
		m := &models[i]
		err = sqlitex.Execute(s.conn, upsertSQL, &sqlitex.ExecOptions{
			Args: []any{
				string(m.ID),
				string(m.Provider),
				m.DisplayName,
				m.Family,
				m.ContextWindow,
				m.MaxOutput,
				boolToInt(m.Reasoning),
				boolToInt(m.ToolCall),
				boolToInt(m.Attachment),
				boolToInt(m.Temperature),
				boolToInt(m.StructuredOutput),
				boolToInt(m.Interleaved),
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
func (s *Store) QueryModels(ctx context.Context, provider Provider) ([]ModelInfo, error) {
	var (
		query string
		args  []any
	)

	if provider == "" {
		query = `SELECT
			model_id, provider, display_name, family,
			context_window, max_output,
			reasoning, tool_call, attachment, temperature, structured_output, interleaved, open_weights,
			cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
			release_date, knowledge,
			modalities_input, modalities_output,
			last_synced
		FROM models`
		args = nil
	} else {
		query = `SELECT
			model_id, provider, display_name, family,
			context_window, max_output,
			reasoning, tool_call, attachment, temperature, structured_output, interleaved, open_weights,
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

// QueryModel returns the single model with the given ID, or ErrNotFound if
// no model with that ID exists in the store.
func (s *Store) QueryModel(ctx context.Context, id ModelID) (ModelInfo, error) {
	const query = `SELECT
		model_id, provider, display_name, family,
		context_window, max_output,
		reasoning, tool_call, attachment, temperature, structured_output, interleaved, open_weights,
		cost_input, cost_output, cost_reasoning, cost_cache_read, cost_cache_write,
		release_date, knowledge,
		modalities_input, modalities_output,
		last_synced
	FROM models
	WHERE model_id = ?1`

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

// --- helpers ---

// boolToInt converts a bool to 0 or 1 for SQLite INTEGER storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
// Column order must match the SELECT in QueryModels / QueryModel.
func scanModelInfo(stmt *sqlite.Stmt) ModelInfo {
	m := ModelInfo{
		ID:               ModelID(stmt.GetText("model_id")),
		Provider:         Provider(stmt.GetText("provider")),
		DisplayName:      stmt.GetText("display_name"),
		Family:           stmt.GetText("family"),
		ContextWindow:    int(stmt.GetInt64("context_window")),
		MaxOutput:        int(stmt.GetInt64("max_output")),
		Reasoning:        stmt.GetBool("reasoning"),
		ToolCall:         stmt.GetBool("tool_call"),
		Attachment:       stmt.GetBool("attachment"),
		Temperature:      stmt.GetBool("temperature"),
		StructuredOutput: stmt.GetBool("structured_output"),
		Interleaved:      stmt.GetBool("interleaved"),
		OpenWeights:      stmt.GetBool("open_weights"),
		ReleaseDate:      stmt.GetText("release_date"),
		Knowledge:        stmt.GetText("knowledge"),
		LastSynced:       stmt.GetText("last_synced"),
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
