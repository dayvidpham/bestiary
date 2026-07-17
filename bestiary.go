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
	//
	// Release-stage tokens (preview/latest/original) REMAIN in this list as data —
	// the extraction still captures them — but they are excluded from entity keys
	// and from the canonical {mods}/[attrs] rendering: the Stage field carries that
	// axis (see ReleaseStage in stage.go), and EntityModifiers/attributeModifiers
	// route them out before classification.
	Modifier []string
	// ParamSize is the canonical parameter-size token for this model instance
	// (e.g. "70b", "8b", "0.5b"). Empty when the size is unknown or not applicable.
	// Populated at codegen time from curated data; participates in entity identity
	// via the #size segment of EntityRef.String().
	ParamSize string
	// TotalParams, ActiveParams, PerExpertParams, and ExpertCount are the flat
	// parameter-shape facts decomposed from ParamSize (see ParamShape and
	// ParseParamShape). They are DERIVED presentation facts, never entity-key
	// material — the identity carrier is ParamSize (the raw #size token). Each is an
	// in-domain NULLable integer under the ParamShapeNull sentinel contract:
	// ParamShapeNull (-1) means "not populated by the parser or curation" (the shape
	// does not carry that fact, or the size is unknown), a positive value is an
	// attested count, and a genuine 0 is reachable ONLY for ExpertCount (a dense
	// shape attests zero experts). All four are ParamShapeNull when ParamSize is
	// empty. They are grouped along parameter-shape joints, never collapsed: an NxM
	// MoE token ("8x22b") records ExpertCount and PerExpertParams but leaves
	// TotalParams and ActiveParams NULL (the product is deliberately not computed),
	// while an active-MoE token ("30b-a3b") records TotalParams and ActiveParams and
	// leaves PerExpertParams/ExpertCount NULL.
	// TotalParams is the total parameter count (e.g. 30_000_000_000 for "30b");
	// ParamShapeNull (-1) when the shape carries no total (NxM, count-suffixed) or
	// the size is unknown.
	TotalParams int64
	// ActiveParams is the active (per-forward-pass) parameter count of a MoE model
	// (e.g. 3_000_000_000 for "30b-a3b", 17_000_000_000 for "17b-16e").
	// ParamShapeNull (-1) for a dense model or when the size is unknown.
	ActiveParams int64
	// PerExpertParams is the per-expert parameter count of an NxM MoE token (e.g.
	// 22_000_000_000 for "8x22b"). ParamShapeNull (-1) unless the shape is NxM.
	PerExpertParams int64
	// ExpertCount is the number of experts of a MoE model (8 for "8x22b", 16 for
	// "17b-16e"). A genuine 0 for a dense model (it attests zero experts);
	// ParamShapeNull (-1) for a MoE shape that carries no count (active-MoE
	// "30b-a3b") or when the size is unknown.
	ExpertCount           int
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
	// QuantVRAM is the per-quantization weight and VRAM footprint for this model
	// instance. nil when no quantization data is available. Populated at codegen
	// time from curated data; live-sync rows carry nil (curated VRAM is not
	// available from the live API).
	QuantVRAM []QuantVRAM
	// Source is the data source that provided this model row. DataSourceNone
	// (zero value, empty string) on live-sync rows; populated at codegen time
	// from curated ingest data.
	Source DataSourceID

	// Instance-level facts from the api.json side of the models.dev catalog.

	// Description is the upstream model description. Empty when none is provided.
	Description string
	// Status is the upstream release status for this instance (StatusNone when
	// none is declared, which means generally available / stable). Status is an
	// instance-level fact — it is present on the api.json side only and never on
	// EntityMetadata.
	Status ModelStatus
	// StatusRaw carries the verbatim upstream status token, populated only when
	// Status is StatusOther (an unrecognized token); empty otherwise.
	StatusRaw string
	// Stage is the release stage DERIVED from this model's ID (preview / beta /
	// latest / original), distinct in provenance from Status: Status is the
	// upstream-DECLARED lifecycle from the api.json side, while Stage is read out
	// of the ID token stream by DetectStageFromID at the same enrichment joints as
	// ParamSize. StageNone (the zero value) when the ID carries no recognized stage
	// marker. Stage is a per-instance attribute and never participates in entity
	// identity (a "-beta"/"-latest" marker does not split the entity). Populated by
	// enrichModelInfo (pure function of the ID); see stage.go.
	Stage ReleaseStage
	// StageRaw carries the verbatim stage token, populated only when Stage is
	// StageOther — the RESERVED bucket for a future non-ID stage feeder. The
	// ID-detection path never yields StageOther, so StageRaw is empty for every
	// ID-derived stage this epoch (mirroring StatusRaw's Other-only convention).
	StageRaw string
	// ReasoningOptions are the reasoning-control options this model exposes
	// (toggle / effort / budget-tokens). nil when the model exposes none.
	ReasoningOptions []ReasoningOption
	// CostInputAudioPerMTok is the audio-input cost in USD per million tokens, or
	// nil when unknown.
	CostInputAudioPerMTok *float64
	// CostOutputAudioPerMTok is the audio-output cost in USD per million tokens,
	// or nil when unknown.
	CostOutputAudioPerMTok *float64
	// CostContextOver200k is the fixed context-over-200k-tokens pricing override,
	// or nil when the model declares none. It is upstream's legacy fixed-threshold
	// special case, kept distinct from CostTiers for wire fidelity.
	CostContextOver200k *TierCost
	// CostTiers are the general context-size-conditional price tiers. nil when the
	// model declares none.
	CostTiers []CostTier

	LastSynced string // RFC3339
}

// ParamShapeNull is the in-domain NULL sentinel for the four parameter-shape
// integer fields (ParamShape and the inline ModelInfo TotalParams / ActiveParams /
// PerExpertParams / ExpertCount). A field set to ParamShapeNull means "not populated
// by the parser or curation": the shape genuinely does not carry that fact (a dense
// token attests no active or per-expert count), or the size is entirely unknown. It
// is DISTINCT from a genuine 0, which under this contract is reachable only for
// ExpertCount (a dense shape attests exactly zero experts). Modelling the fields as a
// NULLable tabular domain — rather than overloading 0 for both "absent" and "zero" —
// lets a consumer tell an unpopulated field from an attested count. See
// ParseParamShape for the per-shape population contract.
const ParamShapeNull = -1

// ParamShape is the pure decomposition of a canonical parameter-size token into
// flat parameter-count facts. It is produced by ParseParamShape and carries the
// same four values that ModelInfo exposes inline (TotalParams / ActiveParams /
// PerExpertParams / ExpertCount).
//
// Each field is a NULLable integer under the ParamShapeNull (-1) sentinel contract:
// a field the shape does not carry is ParamShapeNull, never a masquerading 0. The
// fields are grouped along parameter-shape joints and are NEVER concatenated or
// cross-computed. In particular an NxM MoE token ("8x22b") sets ExpertCount and
// PerExpertParams but leaves TotalParams and ActiveParams NULL — the total is
// deliberately NOT N*M, because upstream does not publish it and inventing it would
// misstate the footprint. An active-MoE token ("30b-a3b") sets TotalParams and
// ActiveParams and leaves PerExpertParams/ExpertCount NULL; a count-suffixed MoE
// token ("17b-16e") sets ActiveParams and ExpertCount and leaves TotalParams and
// PerExpertParams NULL; a dense token ("30b", "560m", "10.7b") sets TotalParams,
// leaves ActiveParams and PerExpertParams NULL, and sets ExpertCount to a genuine 0
// (a dense model attests zero experts). The empty token leaves ALL FOUR NULL.
//
// Counts are exact int64 parameter counts (e.g. 30_000_000_000 for "30b"),
// computed with string-digit decimal arithmetic so a decimal token such as
// "10.7b" yields exactly 10_700_000_000 (never a float64 rounding of 10.7e9).
type ParamShape struct {
	TotalParams     int64
	ActiveParams    int64
	PerExpertParams int64
	ExpertCount     int
}
