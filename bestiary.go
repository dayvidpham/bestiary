// Package bestiary provides a thin wrapper and CLI interface for the models.dev API.
// It exposes types for AI model metadata and a local SQLite cache for offline use.
package bestiary

// ModelID is the canonical identifier for an AI model (e.g., "claude-3-5-sonnet-20241022").
type ModelID string

// ModelInfo holds metadata for a single AI model as returned by the models.dev API.
type ModelInfo struct {
	ID                    ModelID
	Provider              Provider
	DisplayName           string
	Family                string
	ContextWindow         int
	MaxOutput             int
	Reasoning             bool
	ToolCall              bool
	Attachment            bool
	Temperature           bool
	StructuredOutput      bool
	Interleaved           bool
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
