package bestiary

import (
	"fmt"
	"strings"
)

// This file is the home of the models.dev entity-metadata module: the
// provider-agnostic model facts that models.dev publishes on the models.json
// side of its catalog (description, license, benchmark claims, links) plus the
// instance-level extensions that ride on ModelInfo (status, reasoning options,
// audio and context-tier pricing).
//
// Zero-value conventions across the enums declared here are DELIBERATELY not
// uniform, and the divergence is documented on each type:
//
//   - ModelStatus is a SCALAR field on ModelInfo. A model with no declared
//     status is meaningfully "generally available / stable", so the absence
//     carries information: StatusNone sits at the zero value, and StatusOther is
//     the fail-safe bucket for an unknown-but-present token.
//   - LinkType and ReasoningOptionKind are ELEMENT types: every element of a
//     list always arrives with its discriminating tag (a link always has a
//     type, a reasoning option always has a kind), so absence is impossible and
//     a zero value can only mean "the upstream tag was unrecognized". For those
//     the fail-safe bucket therefore sits AT the zero value (LinkOther,
//     ReasoningOptionOther).
//
// Each closed int enum follows the DerivationKind / Quantization precedent: a
// name table is the single source of truth for String / MarshalText /
// UnmarshalText, an ingest-path detector maps an unknown-but-present token to
// the Other bucket (never dropping it — the raw token is carried alongside),
// and — where a CLI flag consumes the value — a Parse* function returns an
// actionable error instead of silently bucketing an unknown token.

// MetadataID is the lab-scoped models.dev metadata key for a model's
// provider-agnostic facts (e.g. "zhipuai/glm-4.6"). It is the stable upstream
// key for an EntityMetadata row and is immune to entity re-keying, so the store
// and the join adapter key metadata by it rather than by an EntityRef.String().
type MetadataID string

// --------------------------------------------------------------------------
// ModelStatus — instance-level release status (api.json side)
// --------------------------------------------------------------------------

// ModelStatus classifies the release status an upstream provider declares for a
// model instance (from the api.json status field). It is a closed int enum
// (like DerivationKind and Quantization) because the set of statuses is small
// and well-understood.
//
// The zero value is StatusNone: on this SCALAR field absence is meaningful — a
// model with no declared status is generally available / stable — so None sits
// at zero and StatusOther is the fail-safe bucket for an unknown-but-present
// token (the raw token is carried on ModelInfo.StatusRaw). This None-at-zero
// convention deliberately differs from the Other-at-zero convention on the
// element enums LinkType and ReasoningOptionKind; see the file-level comment.
//
// Wire names are lowercase ASCII strings; MarshalText / UnmarshalText implement
// encoding.TextMarshaler / encoding.TextUnmarshaler so a ModelStatus serializes
// as a JSON/YAML string rather than an integer.
type ModelStatus int

const (
	// StatusNone is the zero value: no status declared (generally available / stable).
	StatusNone ModelStatus = iota
	// StatusAlpha: an early, unstable preview.
	StatusAlpha
	// StatusBeta: a later preview, more stable than alpha but not yet GA.
	StatusBeta
	// StatusDeprecated: scheduled for or already past end-of-life.
	StatusDeprecated
	// StatusOther is the fail-safe for a status token present in the upstream
	// data but not covered by the named constants above. The raw token is carried
	// on ModelInfo.StatusRaw. detectModelStatus maps an unrecognized token here;
	// ParseModelStatus never does (it returns an actionable error instead).
	StatusOther
)

// modelStatusNames is the canonical lowercase wire name for each ModelStatus,
// index-aligned with the iota constants. It is the single source of truth for
// String / MarshalText / UnmarshalText.
var modelStatusNames = [...]string{
	StatusNone:       "none",
	StatusAlpha:      "alpha",
	StatusBeta:       "beta",
	StatusDeprecated: "deprecated",
	StatusOther:      "other",
}

// String returns the canonical lowercase wire name of the status. An
// out-of-range value renders as "modelstatus(<n>)" so logs never silently drop
// an unexpected value.
func (s ModelStatus) String() string {
	if int(s) < 0 || int(s) >= len(modelStatusNames) {
		return fmt.Sprintf("modelstatus(%d)", int(s))
	}
	return modelStatusNames[s]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical wire
// name (so ModelStatus serializes as a JSON string, not an integer). An
// out-of-range value is a programming error and yields an actionable error.
func (s ModelStatus) MarshalText() ([]byte, error) {
	if int(s) < 0 || int(s) >= len(modelStatusNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal ModelStatus: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed"+
				" (only the StatusNone..StatusOther constants are valid);"+
				" where: ModelStatus.MarshalText;"+
				" how to fix: assign one of the exported ModelStatus constants",
			int(s), len(modelStatusNames),
		)
	}
	return []byte(modelStatusNames[s]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical wire
// name back into a ModelStatus. Parsing is case-insensitive so mixed-case
// upstream tokens round-trip. An unrecognized token yields an actionable error.
func (s *ModelStatus) UnmarshalText(text []byte) error {
	lower := strings.ToLower(string(text))
	for i, name := range modelStatusNames {
		if name == lower {
			*s = ModelStatus(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal ModelStatus from %q;"+
			" why: the token does not match any known status;"+
			" where: ModelStatus.UnmarshalText;"+
			" how to fix: use one of %v",
		string(text), modelStatusNames,
	)
}

// IsKnown reports whether s is a named constant in this package (i.e. not an
// out-of-range integer). StatusOther is considered known — it is a named member
// of the enum; only truly out-of-range integers return false.
func (s ModelStatus) IsKnown() bool {
	return int(s) >= 0 && int(s) < len(modelStatusNames)
}

// ParseModelStatus parses a ModelStatus from a string using a case-insensitive
// exact match against the canonical wire names. It is the CLI path (e.g.
// list --status): the empty string returns StatusNone with no error, a
// recognized name returns its constant, and any other non-empty string returns
// an actionable error that names what was received and lists the valid values.
// Unlike detectModelStatus, ParseModelStatus never silently maps an
// unrecognized token to StatusOther, and it rejects the internal "other"
// sentinel (it is not a user-selectable status).
func ParseModelStatus(s string) (ModelStatus, error) {
	if s == "" {
		return StatusNone, nil
	}
	lower := strings.ToLower(s)
	for i, name := range modelStatusNames {
		if i == int(StatusOther) {
			continue // "other" is an internal sentinel, never user-selectable
		}
		if name == lower {
			return ModelStatus(i), nil
		}
	}
	return StatusNone, fmt.Errorf(
		"bestiary: ParseModelStatus: unrecognized status %q;"+
			" why: the input does not match any known model status (case-insensitive);"+
			" where: ParseModelStatus;"+
			" valid values: none, alpha, beta, deprecated;"+
			" how to fix: pass one of the valid values listed above",
		s,
	)
}

// detectModelStatus is the ingest path (api.json status field): the empty (or
// whitespace-only) token yields StatusNone; a recognized token yields its
// constant; and an unknown-but-present token yields (StatusOther, raw) so it is
// never dropped — the caller stores raw on ModelInfo.StatusRaw. The returned raw
// string is non-empty only when the result is StatusOther, preserving the
// verbatim upstream token (original casing). The function never panics.
func detectModelStatus(s string) (ModelStatus, string) {
	if strings.TrimSpace(s) == "" {
		return StatusNone, ""
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	for i, name := range modelStatusNames {
		if i == int(StatusOther) {
			continue // "other" is an internal sentinel, not an upstream token
		}
		if name == lower {
			return ModelStatus(i), ""
		}
	}
	return StatusOther, s
}

// --------------------------------------------------------------------------
// LinkType — element type for a model reference link (models.json side)
// --------------------------------------------------------------------------

// LinkType classifies a model reference link (the type tag on a models.json
// links[] row, and the synthetic tag for a folded-in weights[] row). It is a
// closed int enum.
//
// The zero value is LinkOther: on this ELEMENT type the upstream type tag is
// always present (every link arrives with a type), so absence is impossible and
// a zero value can only mean "the upstream tag was unrecognized". The fail-safe
// bucket therefore sits at the zero value, and the raw token is carried on
// ModelLink.TypeRaw. This Other-at-zero convention deliberately differs from
// ModelStatus's None-at-zero convention; see the file-level comment.
type LinkType int

const (
	// LinkOther is the zero value and fail-safe: an unrecognized link type. The
	// raw token is carried on ModelLink.TypeRaw.
	LinkOther LinkType = iota
	// LinkAnnouncement: a launch or announcement post.
	LinkAnnouncement
	// LinkBlog: a blog article.
	LinkBlog
	// LinkDocs: product or API documentation.
	LinkDocs
	// LinkLicense: the model's license text.
	LinkLicense
	// LinkModelCard: a model card (e.g. on a model hub).
	LinkModelCard
	// LinkPaper: a research paper or technical report.
	LinkPaper
	// LinkWeights: a downloadable-weights reference (a folded-in weights[] row).
	LinkWeights
)

// linkTypeNames is the canonical lowercase wire name for each LinkType,
// index-aligned with the iota constants. It is the single source of truth for
// String / MarshalText / UnmarshalText.
var linkTypeNames = [...]string{
	LinkOther:        "other",
	LinkAnnouncement: "announcement",
	LinkBlog:         "blog",
	LinkDocs:         "docs",
	LinkLicense:      "license",
	LinkModelCard:    "model_card",
	LinkPaper:        "paper",
	LinkWeights:      "weights",
}

// String returns the canonical lowercase wire name of the link type. An
// out-of-range value renders as "linktype(<n>)" so logs never silently drop an
// unexpected value.
func (t LinkType) String() string {
	if int(t) < 0 || int(t) >= len(linkTypeNames) {
		return fmt.Sprintf("linktype(%d)", int(t))
	}
	return linkTypeNames[t]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical wire
// name (so LinkType serializes as a JSON string, not an integer). An
// out-of-range value is a programming error and yields an actionable error.
func (t LinkType) MarshalText() ([]byte, error) {
	if int(t) < 0 || int(t) >= len(linkTypeNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal LinkType: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed"+
				" (only the LinkOther..LinkWeights constants are valid);"+
				" where: LinkType.MarshalText;"+
				" how to fix: assign one of the exported LinkType constants",
			int(t), len(linkTypeNames),
		)
	}
	return []byte(linkTypeNames[t]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical wire
// name back into a LinkType. Parsing is case-insensitive. An unrecognized token
// yields an actionable error.
func (t *LinkType) UnmarshalText(text []byte) error {
	lower := strings.ToLower(string(text))
	for i, name := range linkTypeNames {
		if name == lower {
			*t = LinkType(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal LinkType from %q;"+
			" why: the token does not match any known link type;"+
			" where: LinkType.UnmarshalText;"+
			" how to fix: use one of %v",
		string(text), linkTypeNames,
	)
}

// IsKnown reports whether t is a named constant in this package. LinkOther is
// considered known (a named member of the enum); only out-of-range integers
// return false.
func (t LinkType) IsKnown() bool {
	return int(t) >= 0 && int(t) < len(linkTypeNames)
}

// detectLinkType is the ingest path for a link type tag: a recognized token
// (case-insensitive) yields its constant with an empty raw; an unknown-but-
// present token yields (LinkOther, raw) so it is never dropped — the caller
// stores raw on ModelLink.TypeRaw. Because LinkOther is the zero value, the
// empty token also maps to (LinkOther, "") and a missing tag is indistinguishable
// from an unrecognized one, which is the intended element-enum semantics. The
// returned raw is non-empty only when the token was present but unrecognized.
func detectLinkType(s string) (LinkType, string) {
	if strings.TrimSpace(s) == "" {
		return LinkOther, ""
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	for i, name := range linkTypeNames {
		if i == int(LinkOther) {
			continue // "other" is an internal sentinel, not an upstream token
		}
		if name == lower {
			return LinkType(i), ""
		}
	}
	return LinkOther, s
}

// ModelLink is one reference link for a model: a labeled URL with a classified
// type. Upstream weights[] rows fold in here with Type == LinkWeights.
type ModelLink struct {
	// Label is the human-facing link text.
	Label string
	// URL is the link target.
	URL string
	// Type is the classified link type.
	Type LinkType
	// TypeRaw carries the verbatim upstream type token, populated only when Type
	// is LinkOther (an unrecognized tag); empty otherwise.
	TypeRaw string
}

// --------------------------------------------------------------------------
// BenchmarkResult — one lab-reported benchmark claim (models.json side)
// --------------------------------------------------------------------------

// BenchmarkResult is one benchmark claim as reported by the publishing
// organization. Its fields are grouped along the assessment-provenance joints —
// criterion identity, apparatus, assessment value, and claim attribution — and
// are NEVER concatenated into a single string, so a future canonical benchmark
// dimension can join on the separated fields.
//
// These scores are attributed claims by the lab that published the model (a
// blog post or model card), not independent third-party measurements; callers
// must treat them as such.
type BenchmarkResult struct {
	// Criterion identity — what was assessed. A future canonical Benchmark
	// dimension joins here.

	// Name is the benchmark name (required upstream, e.g. "MMLU", "GPQA").
	Name string
	// Version is the benchmark version, when the source distinguishes one.
	Version string
	// Variant is the benchmark variant / subtask, when the source distinguishes one.
	Variant string
	// Dataset is the evaluation dataset, when the source names one.
	Dataset string

	// Apparatus — how it was assessed.

	// Harness is the evaluation harness / framework used, when the source names one.
	Harness string

	// Assessment value — the reported result.

	// Metric is the reported metric name (e.g. "accuracy", "pass@1").
	Metric string
	// Score is the numeric score, or 0 when the upstream score is non-numeric
	// (in which case the verbatim value is preserved on ScoreRaw).
	Score float64
	// ScoreRaw is the verbatim upstream score when it is non-numeric (e.g. a
	// string), and empty when Score is numeric. Upstream reports scores as either
	// a number or a string; capturing the raw form here keeps a string score from
	// dropping the row or failing the parse.
	ScoreRaw string

	// Claim attribution — who asserted it and when.

	// SourceURL is the URL of the original claimant (the lab blog or model card).
	// It is named SourceURL rather than Source to avoid colliding with the
	// DataSourceID-typed Source fields on ModelInfo and EntityMetadata: a claim
	// attribution (who reported the score) and an ingest attestation (which data
	// source bestiary read the row from) are different provenance levels and must
	// not share a field name.
	SourceURL string
	// Date is the claim date in YYYY-MM-DD format, when the source gives one.
	Date string
}

// --------------------------------------------------------------------------
// ReasoningOption — a discriminated reasoning-control option (api.json side)
// --------------------------------------------------------------------------

// ReasoningOptionKind discriminates the kind of a reasoning-control option an
// upstream model exposes. It is a closed int enum.
//
// The zero value is ReasoningOptionOther: on this ELEMENT type the upstream
// discriminating tag is always present, so a zero value can only mean "the tag
// was unrecognized". The fail-safe bucket therefore sits at the zero value (the
// raw token is carried on ReasoningOption.KindRaw), the Other-at-zero convention
// shared with LinkType and deliberately distinct from ModelStatus; see the
// file-level comment.
type ReasoningOptionKind int

const (
	// ReasoningOptionOther is the zero value and fail-safe: an unrecognized
	// reasoning-option kind. The raw token is carried on ReasoningOption.KindRaw.
	ReasoningOptionOther ReasoningOptionKind = iota
	// ReasoningToggle: a plain on/off reasoning switch.
	ReasoningToggle
	// ReasoningEffort: a discrete effort selector; the choices are on Values.
	ReasoningEffort
	// ReasoningBudgetTokens: a token-budget control; the bounds are on
	// MinTokens / MaxTokens.
	ReasoningBudgetTokens
)

// reasoningOptionKindNames is the canonical lowercase wire name for each
// ReasoningOptionKind, index-aligned with the iota constants. It is the single
// source of truth for String / MarshalText / UnmarshalText.
var reasoningOptionKindNames = [...]string{
	ReasoningOptionOther:  "other",
	ReasoningToggle:       "toggle",
	ReasoningEffort:       "effort",
	ReasoningBudgetTokens: "budget_tokens",
}

// String returns the canonical lowercase wire name of the kind. An out-of-range
// value renders as "reasoningoptionkind(<n>)" so logs never silently drop an
// unexpected value.
func (k ReasoningOptionKind) String() string {
	if int(k) < 0 || int(k) >= len(reasoningOptionKindNames) {
		return fmt.Sprintf("reasoningoptionkind(%d)", int(k))
	}
	return reasoningOptionKindNames[k]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical wire
// name (so ReasoningOptionKind serializes as a JSON string, not an integer). An
// out-of-range value is a programming error and yields an actionable error.
func (k ReasoningOptionKind) MarshalText() ([]byte, error) {
	if int(k) < 0 || int(k) >= len(reasoningOptionKindNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal ReasoningOptionKind: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed"+
				" (only the ReasoningOptionOther..ReasoningBudgetTokens constants are valid);"+
				" where: ReasoningOptionKind.MarshalText;"+
				" how to fix: assign one of the exported ReasoningOptionKind constants",
			int(k), len(reasoningOptionKindNames),
		)
	}
	return []byte(reasoningOptionKindNames[k]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical wire
// name back into a ReasoningOptionKind. Parsing is case-insensitive. An
// unrecognized token yields an actionable error.
func (k *ReasoningOptionKind) UnmarshalText(text []byte) error {
	lower := strings.ToLower(string(text))
	for i, name := range reasoningOptionKindNames {
		if name == lower {
			*k = ReasoningOptionKind(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal ReasoningOptionKind from %q;"+
			" why: the token does not match any known reasoning-option kind;"+
			" where: ReasoningOptionKind.UnmarshalText;"+
			" how to fix: use one of %v",
		string(text), reasoningOptionKindNames,
	)
}

// IsKnown reports whether k is a named constant in this package. ReasoningOptionOther
// is considered known; only out-of-range integers return false.
func (k ReasoningOptionKind) IsKnown() bool {
	return int(k) >= 0 && int(k) < len(reasoningOptionKindNames)
}

// detectReasoningOptionKind is the ingest path for a reasoning-option kind tag:
// a recognized token (case-insensitive) yields its constant with an empty raw;
// an unknown-but-present token yields (ReasoningOptionOther, raw) so it is never
// dropped — the caller stores raw on ReasoningOption.KindRaw. The empty token
// also maps to (ReasoningOptionOther, ""), matching the element-enum semantics.
// The returned raw is non-empty only when the token was present but unrecognized.
func detectReasoningOptionKind(s string) (ReasoningOptionKind, string) {
	if strings.TrimSpace(s) == "" {
		return ReasoningOptionOther, ""
	}
	lower := strings.ToLower(strings.TrimSpace(s))
	for i, name := range reasoningOptionKindNames {
		if i == int(ReasoningOptionOther) {
			continue // "other" is an internal sentinel, not an upstream token
		}
		if name == lower {
			return ReasoningOptionKind(i), ""
		}
	}
	return ReasoningOptionOther, s
}

// ReasoningOption is one reasoning-control option a model exposes, as a
// strongly-typed discriminated union keyed by Kind. Only the fields relevant to
// Kind carry data: ReasoningEffort populates Values; ReasoningBudgetTokens
// populates MinTokens / MaxTokens; ReasoningToggle carries neither.
type ReasoningOption struct {
	// Kind discriminates which of the payload fields below are meaningful.
	Kind ReasoningOptionKind
	// KindRaw carries the verbatim upstream kind token, populated only when Kind
	// is ReasoningOptionOther (an unrecognized tag); empty otherwise.
	KindRaw string
	// Values holds the discrete choices for a ReasoningEffort option; nil otherwise.
	Values []string
	// MinTokens is the lower budget bound for a ReasoningBudgetTokens option; 0 otherwise.
	MinTokens int
	// MaxTokens is the upper budget bound for a ReasoningBudgetTokens option; 0 otherwise.
	MaxTokens int
}

// --------------------------------------------------------------------------
// Cost tiers — context-conditional pricing (api.json side)
// --------------------------------------------------------------------------

// TierCost is one bundle of cost overrides. Every axis is optional: a nil
// pointer means "not overridden at this tier". The per-million-token costs
// mirror the flat cost fields on ModelInfo and add audio input/output rates.
type TierCost struct {
	// CostInputPerMTok is the input cost in USD per million tokens, or nil when unset.
	CostInputPerMTok *float64
	// CostOutputPerMTok is the output cost in USD per million tokens, or nil when unset.
	CostOutputPerMTok *float64
	// CostReasoningPerMTok is the reasoning cost in USD per million tokens, or nil when unset.
	CostReasoningPerMTok *float64
	// CostCacheReadPerMTok is the cache-read cost in USD per million tokens, or nil when unset.
	CostCacheReadPerMTok *float64
	// CostCacheWritePerMTok is the cache-write cost in USD per million tokens, or nil when unset.
	CostCacheWritePerMTok *float64
	// CostInputAudioPerMTok is the audio-input cost in USD per million tokens, or nil when unset.
	CostInputAudioPerMTok *float64
	// CostOutputAudioPerMTok is the audio-output cost in USD per million tokens, or nil when unset.
	CostOutputAudioPerMTok *float64
}

// CostTier is a context-size-conditional price tier: the TierCost overrides
// apply once the request context reaches ContextSize tokens. Upstream, a tier's
// type is always "context", so only the token threshold is modeled here (as
// ContextSize) and the TierCost bundle is embedded so its cost fields flatten to
// the tier level in JSON.
//
// The fixed context_over_200k override on ModelInfo and this general tiers list
// BOTH express context-conditional pricing: context_over_200k is upstream's
// legacy fixed-threshold special case and CostTier is the general mechanism.
// They are kept separate for wire fidelity — unifying them would invent data
// upstream does not assert.
type CostTier struct {
	// ContextSize is the token threshold at which this tier's overrides apply.
	ContextSize int
	// TierCost is embedded so its cost fields flatten to the tier level in JSON.
	TierCost
}

// --------------------------------------------------------------------------
// EntityMetadata — provider-agnostic model facts (models.json side)
// --------------------------------------------------------------------------

// EntityMetadata holds the provider-agnostic model facts models.dev publishes
// on its models.json side: description, license, reference links, and
// lab-reported benchmark claims, keyed by the stable MetadataID.
//
// It carries NO status field: status is not present in models.json — it is an
// api.json / instance-level fact and lives on ModelInfo only. A single
// EntityMetadata attaches to at most one Entity (Entity.Metadata); the join is
// computed at load time by the metadata↔entity adapter.
type EntityMetadata struct {
	// MetadataID is the stable models.dev metadata key (e.g. "zhipuai/glm-4.6").
	MetadataID MetadataID
	// Name is the display name.
	Name string
	// Description is the model description.
	Description string
	// License is the license identifier or name.
	License string
	// Links are the model's reference links; upstream weights[] rows fold in with
	// Type == LinkWeights. nil when the source lists none.
	Links []ModelLink
	// Benchmarks are the lab-reported benchmark claims. nil when the source lists none.
	Benchmarks []BenchmarkResult
	// Source is the ingest attestation — the data source this metadata was read
	// from (DataSourceModelsDev). It is DataSourceNone on a value that has not
	// been assigned a source.
	Source DataSourceID
	// LastSynced is an RFC3339 timestamp; empty on baked rows until a sync occurs.
	LastSynced string
}

// Catalog is the parsed catalog.json artifact: the two views models.dev
// publishes from a single upstream deploy. Models is the providers view (the
// api.json shape) and Metadata is the models view (the models.json shape).
//
// Catalog is a parser return container, not a serialized public output
// document, so it is intentionally NOT a $def in bestiary.schema.json; the Go
// type exists only to carry the two parsed views together.
type Catalog struct {
	// Models is the providers view (api.json shape).
	Models []ModelInfo
	// Metadata is the models view (models.json shape).
	Metadata []EntityMetadata
}
