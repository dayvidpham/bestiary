// Package bestiary provides a thin wrapper and CLI interface for the models.dev API.
// It exposes types for AI model metadata and a local SQLite cache for offline use.
package bestiary

// ModelID is the canonical identifier for an AI model (e.g., "claude-3-5-sonnet-20241022").
type ModelID string

// Capability represents a model capability that may carry additional configuration.
// For most capabilities, Supported is the only relevant field. For interleaved,
// Config may hold additional details (e.g., {"field": "reasoning_details"}).
type Capability struct {
	Supported bool
	Config    map[string]string // nil when no extra config
}

// ModelInfo holds metadata for a single AI model as returned by the models.dev API.
//
// Normalized fields (NormalizedFamily, NormalizedVariant, NormalizedDate) are
// populated at codegen time by the bestiary-gen tool invoking parse.ParseFamily,
// parse.ExtractDate, and parse.InferFamilyFromID. They are zero-value for models
// loaded from the SQLite cache (pre-normalization epoch) until a sync is performed.
type ModelInfo struct {
	ID                    ModelID
	Provider              Provider
	DisplayName           string
	Family                Family
	// NormalizedFamily is the canonical family identifier extracted from Family
	// (or inferred from ID when Family is empty). Populated at codegen time.
	NormalizedFamily  Family
	// NormalizedVariant is the variant suffix extracted from Family (e.g. "opus-4",
	// "pro", "flash"). Empty when the model has no variant. Populated at codegen time.
	NormalizedVariant string
	// NormalizedDate is the release date extracted from the model ID or ReleaseDate
	// field, in YYYY-MM-DD format. Empty when no date is found. Populated at codegen time.
	NormalizedDate    string
	ContextWindow         int
	MaxOutput             int
	Reasoning             bool
	ToolCall              bool
	Attachment            bool
	Temperature           bool
	StructuredOutput      bool
	Interleaved           Capability
	OpenWeights           bool
	CostInputPerMTok      *float64
	CostOutputPerMTok     *float64
	CostReasoningPerMTok  *float64
	CostCacheReadPerMTok  *float64
	CostCacheWritePerMTok *float64
	ReleaseDate           string
	Knowledge             string
	Modalities            Modalities
	LastSynced            string // RFC3339
}
