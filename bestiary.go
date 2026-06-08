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
// Canonical fields (Family, Variant, Version, Date) are populated at codegen time
// by the bestiary-gen tool invoking bestiary.ParseFamily, bestiary.ExtractDate, and
// bestiary.InferFamilyFromID. They are zero-value for models loaded from the SQLite
// cache (pre-normalization epoch) until a sync is performed.
//
// RawFamily is the raw API family value verbatim (e.g. "claude-opus", "gemini-flash").
// Family is the canonical/normalized family (e.g. "claude", "gemini").
type ModelInfo struct {
	ID          ModelID
	Provider    Provider
	DisplayName string
	RawFamily   Family // raw API family field verbatim (e.g. "claude-opus")

	// Codegen-baked normalization

	// Family is the canonical family identifier extracted from RawFamily
	// (or inferred from ID when RawFamily is empty). Populated at codegen time.
	Family Family
	// Variant is the variant suffix extracted from RawFamily (e.g. "opus",
	// "pro", "flash-lite"). Empty when the model has no variant. Populated at codegen time.
	Variant string
	// Version is the model version extracted from the model ID
	// (primary source, e.g. "claude-opus-4-5-20251101" → "4.5") or, when the
	// family string itself carries a version component, from the family string
	// (fallback, e.g. "gemini-2.5-flash" → "2.5"). Empty when no separable
	// version is found. Populated at codegen time.
	Version string
	// Date is the release date extracted from the model ID or ReleaseDate
	// field, in YYYY-MM-DD format. Empty when no date is found. Populated at codegen time.
	Date string
	// Modifier is the LIST of known trailing tokens extracted from the model ID
	// that carry semantic meaning beyond family/variant/version/date (e.g.
	// ["thinking"], ["vision", "instruct"]). nil when no known modifier is found.
	// The list is stored in deterministic CANONICAL ORDER (see CanonicalizeModifiers
	// in modifier.go): capability > speed > format/stage, with an alphabetical
	// fallback. Populated by the parse pipeline at codegen time.
	// widened string → []string for lossless
	// multi-modifier capture (kimi-k2-thinking-turbo → [thinking, turbo]).
	Modifier              []string
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

	// Host is the serving host / backend infrastructure that runs this model
	// instance, distinct from Provider. It is a per-instance ATTRIBUTE and never
	// participates in entity identity. HostNone (zero value) when unknown or when
	// the provider serves the model directly. Populated by the host-split slice.
	Host Host
	// Lineage is the set of derivation edges from this model to its parent
	// model(s) (finetune, merge, distillation, …). nil when the model is a base
	// model or no curated lineage is known. Populated at codegen time from the
	// curated lineage table by the lineage slice.
	Lineage []LineageEdge

	LastSynced string // RFC3339
}
