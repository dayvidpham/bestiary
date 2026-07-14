package bestiary

import (
	"encoding/json"
	"strings"
)

// Wire types for JSON deserialization from the models.dev catalog artifacts.
// These types are unexported (package-internal); consumers use the public
// ModelInfo / EntityMetadata / Catalog types returned by the exported parsers
// (ParseAPIJSON / ParseModelsJSON / ParseCatalogJSON) and the Client Fetch*
// methods.
//
// The catalog has three artifacts, all derived from a single upstream deploy:
//   - api.json      → wireResponse (providers view: pricing + per-provider facts)
//   - models.json   → wireModelMetadataMap (provider-agnostic model facts)
//   - catalog.json  → wireCatalog ({models, providers}: both views in one payload)

// flexBool tolerates polymorphic JSON fields that are sometimes a boolean and
// sometimes an object or string in the models.dev API. When the value is a JSON
// boolean it is decoded normally; any other JSON type is silently treated as false.
type flexBool bool

func (fb *flexBool) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*fb = flexBool(b)
		return nil
	}
	// Non-boolean value (object, string, etc.) — treat as false.
	*fb = false
	return nil
}

// wireResponse is the top-level api.json response — a map from provider slug to
// provider object. It is also the shape of the catalog.json "providers" value
// (verified shape-identical), so the same decode path serves both.
type wireResponse map[string]wireProvider

// wireProvider holds the models map for a single provider.
type wireProvider struct {
	Models map[string]wireModel `json:"models"`
}

// wireModel represents a single model entry as returned on the api.json side of
// the models.dev catalog. Boolean capability fields use flexBool because the
// models.dev API occasionally returns objects or strings instead of booleans for
// some providers. Interleaved uses json.RawMessage because it is polymorphic:
// some providers send a bool and others send an object ({"field": "..."}).
//
// Experimental and Provider are captured for wire fidelity but not surfaced on
// the public ModelInfo type: no public field exists for them, so they are
// decoded and dropped (see the field comments).
type wireModel struct {
	ID               string                `json:"id"`
	Name             string                `json:"name"`
	Family           string                `json:"family"`
	Description      string                `json:"description"`
	Status           string                `json:"status"`
	Reasoning        flexBool              `json:"reasoning"`
	ReasoningOptions []wireReasoningOption `json:"reasoning_options"`
	ToolCall         flexBool              `json:"tool_call"`
	Attachment       flexBool              `json:"attachment"`
	Temperature      flexBool              `json:"temperature"`
	StructuredOutput flexBool              `json:"structured_output"`
	Interleaved      json.RawMessage       `json:"interleaved"`
	OpenWeights      flexBool              `json:"open_weights"`
	ReleaseDate      string                `json:"release_date"`
	Knowledge        string                `json:"knowledge"`
	Cost             *wireCost             `json:"cost"`
	Limit            *wireLimit            `json:"limit"`
	Modalities       *wireModalities       `json:"modalities"`
	// Experimental captures the upstream experimental-modes object verbatim. It is
	// intentionally NOT surfaced on ModelInfo (no public field); decoded only so an
	// unknown-but-present block never fails the parse.
	Experimental json.RawMessage `json:"experimental"`
	// Provider captures the per-model provider-routing config. npm/api/shape are
	// captured typed and body/headers as raw; none is surfaced on ModelInfo (there
	// is no public field for provider-routing config), so this is capture-only.
	Provider *wireModelProvider `json:"provider"`
}

// wireModelProvider is the per-model provider-routing config on the api.json
// side. It is captured for fidelity but not surfaced publicly.
type wireModelProvider struct {
	Npm     string          `json:"npm"`
	API     string          `json:"api"`
	Shape   string          `json:"shape"`
	Body    json.RawMessage `json:"body"`
	Headers json.RawMessage `json:"headers"`
}

// wireReasoningOption is one entry of the api.json reasoning_options[] array — a
// discriminated union keyed by Type ("toggle" | "effort" | "budget_tokens").
// Values is populated for "effort"; Min/Max for "budget_tokens" (Min may be -1).
type wireReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Min    *int     `json:"min"`
	Max    *int     `json:"max"`
}

// wireCostBase holds the base per-token pricing bundle (USD per million tokens).
// It matches the upstream Cost object, including audio input/output rates. All
// fields are pointers because any may be absent from the API response.
//
// wireCostBase is embedded (anonymously, so its fields flatten in JSON) by both
// wireCost (the OutputCost shape) and wireCostTier (the CostTier shape).
type wireCostBase struct {
	Input       *float64 `json:"input"`
	Output      *float64 `json:"output"`
	Reasoning   *float64 `json:"reasoning"`
	CacheRead   *float64 `json:"cache_read"`
	CacheWrite  *float64 `json:"cache_write"`
	InputAudio  *float64 `json:"input_audio"`
	OutputAudio *float64 `json:"output_audio"`
}

// wireCost is the api.json cost object (upstream OutputCost): the base per-token
// prices plus two context-conditional extensions. ContextOver200k is upstream's
// legacy fixed-threshold special case (a plain Cost bundle); Tiers is the general
// context-size-conditional tier mechanism.
type wireCost struct {
	wireCostBase
	ContextOver200k *wireCostBase  `json:"context_over_200k"`
	Tiers           []wireCostTier `json:"tiers"`
}

// wireCostTier is one entry of the api.json cost.tiers[] array: a base cost
// bundle (flattened) plus a tier discriminator carrying the context-size
// threshold.
type wireCostTier struct {
	wireCostBase
	Tier *wireTier `json:"tier"`
}

// wireTier is the tier discriminator on a cost tier. Upstream Type is always
// "context"; Size is the context-token threshold.
type wireTier struct {
	Type string `json:"type"`
	Size int    `json:"size"`
}

// wireLimit holds context/output window sizes in tokens.
// Fields are pointers because a model may not declare either limit.
type wireLimit struct {
	Context *int `json:"context"`
	Output  *int `json:"output"`
}

// wireModalities lists the input and output modality strings for a model.
type wireModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

// wireModelMetadataMap is the top-level models.json response — a map from the
// canonical <lab>/<model> metadata key to the provider-agnostic model facts. It
// is also the shape of the catalog.json "models" value.
type wireModelMetadataMap map[string]wireModelMetadata

// wireModelMetadata represents a single provider-agnostic model-facts entry on
// the models.json side of the catalog. It captures every upstream field for
// fidelity, but only a subset (id/name/description/license/links/weights/
// benchmarks) is surfaced on the public EntityMetadata type; the capability
// booleans, knowledge, dates, modalities, open_weights, and limit are captured
// but not surfaced (EntityMetadata deliberately omits them — those live on the
// api.json/ModelInfo side). There is NO status field: status is an api.json fact.
type wireModelMetadata struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Family      string             `json:"family"`
	Attachment  *bool              `json:"attachment"`
	Reasoning   *bool              `json:"reasoning"`
	ToolCall    *bool              `json:"tool_call"`
	Structured  *bool              `json:"structured_output"`
	Temperature *bool              `json:"temperature"`
	Knowledge   string             `json:"knowledge"`
	ReleaseDate string             `json:"release_date"`
	LastUpdated string             `json:"last_updated"`
	Modalities  *wireModalities    `json:"modalities"`
	OpenWeights *bool              `json:"open_weights"`
	Limit       *wireLimit         `json:"limit"`
	License     string             `json:"license"`
	Links       []wireModelLink    `json:"links"`
	Weights     []wireModelWeights `json:"weights"`
	Benchmarks  []wireBenchmark    `json:"benchmarks"`
}

// wireModelLink is one models.json links[] entry: a labeled URL with a type tag.
type wireModelLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	Type  string `json:"type"`
}

// wireModelWeights is one models.json weights[] entry. It folds into the public
// EntityMetadata.Links list with Type == LinkWeights; Format/Quantization are
// captured but not surfaced (they are ~unpopulated upstream and ModelLink has no
// field for them — revisit when real data appears).
type wireModelWeights struct {
	Label        string `json:"label"`
	URL          string `json:"url"`
	Format       string `json:"format"`
	Quantization string `json:"quantization"`
}

// wireBenchmark is one models.json benchmarks[] entry. Score is captured as raw
// JSON because upstream reports it as either a number or a string; the mapping
// decodes it tolerantly so a string score never fails the parse or drops the row
// (see decodeBenchmarkScore).
type wireBenchmark struct {
	Name    string          `json:"name"`
	Score   json.RawMessage `json:"score"`
	Metric  string          `json:"metric"`
	Harness string          `json:"harness"`
	Variant string          `json:"variant"`
	Dataset string          `json:"dataset"`
	Version string          `json:"version"`
	Source  string          `json:"source"`
	Date    string          `json:"date"`
}

// wireCatalog is the top-level catalog.json response: both catalog views from a
// single upstream deploy. The "providers" value is shape-identical to api.json
// (decoded through the same wireResponse path) and "models" to models.json.
type wireCatalog struct {
	Models    wireModelMetadataMap `json:"models"`
	Providers wireResponse         `json:"providers"`
}

// toModelInfo converts a wire-level api.json model entry to the public ModelInfo
// type. providerSlug is the map key from wireResponse (e.g., "anthropic").
// LastSynced is intentionally left empty — callers set it on persist.
// ParamSize is intentionally left "" — live sync rows are unsized; curated
// param-size data is baked at codegen time, not available from the live API.
// QuantVRAM is intentionally left nil — live sync rows carry no quant/VRAM data;
// curated VRAM is baked at codegen time, not available from the live API.
// Source is intentionally left DataSourceNone — live sync rows have no curated
// source tag; the models.dev source is implicit for live-sync rows.
func toModelInfo(providerSlug string, wm wireModel) ModelInfo {
	status, statusRaw := detectModelStatus(wm.Status)
	info := ModelInfo{
		ID:               ModelID(wm.ID),
		Provider:         Provider(providerSlug),
		DisplayName:      wm.Name,
		Family:           Family(wm.Family),
		Description:      wm.Description,
		Status:           status,
		StatusRaw:        statusRaw,
		Reasoning:        bool(wm.Reasoning),
		ReasoningOptions: toReasoningOptions(wm.ReasoningOptions),
		ToolCall:         bool(wm.ToolCall),
		Attachment:       bool(wm.Attachment),
		Temperature:      bool(wm.Temperature),
		StructuredOutput: bool(wm.StructuredOutput),
		Interleaved:      parseCapability(wm.Interleaved),
		OpenWeights:      bool(wm.OpenWeights),
		ReleaseDate:      wm.ReleaseDate,
		Knowledge:        wm.Knowledge,
		LastSynced:       "", // caller sets on persist
	}

	if wm.Cost != nil {
		info.CostInputPerMTok = wm.Cost.Input
		info.CostOutputPerMTok = wm.Cost.Output
		info.CostReasoningPerMTok = wm.Cost.Reasoning
		info.CostCacheReadPerMTok = wm.Cost.CacheRead
		info.CostCacheWritePerMTok = wm.Cost.CacheWrite
		info.CostInputAudioPerMTok = wm.Cost.InputAudio
		info.CostOutputAudioPerMTok = wm.Cost.OutputAudio
		if wm.Cost.ContextOver200k != nil {
			tc := toTierCost(*wm.Cost.ContextOver200k)
			info.CostContextOver200k = &tc
		}
		if len(wm.Cost.Tiers) > 0 {
			info.CostTiers = toCostTiers(wm.Cost.Tiers)
		}
	}

	if wm.Limit != nil {
		if wm.Limit.Context != nil {
			info.ContextWindow = *wm.Limit.Context
		}
		if wm.Limit.Output != nil {
			info.MaxOutput = *wm.Limit.Output
		}
	}

	if wm.Modalities != nil {
		info.Modalities = toModalities(wm.Modalities.Input, wm.Modalities.Output)
	}

	return info
}

// toTierCost converts a wire cost bundle to the public TierCost. Nil pointers
// pass through unchanged (a nil axis means "not overridden at this tier").
func toTierCost(c wireCostBase) TierCost {
	return TierCost{
		CostInputPerMTok:       c.Input,
		CostOutputPerMTok:      c.Output,
		CostReasoningPerMTok:   c.Reasoning,
		CostCacheReadPerMTok:   c.CacheRead,
		CostCacheWritePerMTok:  c.CacheWrite,
		CostInputAudioPerMTok:  c.InputAudio,
		CostOutputAudioPerMTok: c.OutputAudio,
	}
}

// toCostTiers converts the wire cost tiers to public CostTier values, preserving
// order. A tier with no discriminator object yields ContextSize 0.
func toCostTiers(ts []wireCostTier) []CostTier {
	out := make([]CostTier, 0, len(ts))
	for _, t := range ts {
		ct := CostTier{TierCost: toTierCost(t.wireCostBase)}
		if t.Tier != nil {
			ct.ContextSize = t.Tier.Size
		}
		out = append(out, ct)
	}
	return out
}

// toReasoningOptions converts the wire reasoning-option entries to public
// ReasoningOption values. An unknown discriminator maps to ReasoningOptionOther
// with the raw token preserved (never dropped); the payload fields are populated
// only for the kind that owns them.
func toReasoningOptions(wros []wireReasoningOption) []ReasoningOption {
	if len(wros) == 0 {
		return nil
	}
	out := make([]ReasoningOption, 0, len(wros))
	for _, wro := range wros {
		kind, raw := detectReasoningOptionKind(wro.Type)
		ro := ReasoningOption{Kind: kind, KindRaw: raw}
		switch kind {
		case ReasoningEffort:
			if len(wro.Values) > 0 {
				ro.Values = append([]string(nil), wro.Values...)
			}
		case ReasoningBudgetTokens:
			if wro.Min != nil {
				ro.MinTokens = *wro.Min
			}
			if wro.Max != nil {
				ro.MaxTokens = *wro.Max
			}
		}
		out = append(out, ro)
	}
	return out
}

// toEntityMetadata converts a wire-level models.json entry to the public
// EntityMetadata type. mapKey is the canonical <lab>/<model> map key; it is used
// as the MetadataID only when the entry's own id field is empty (they are the
// same upstream, but the map key is always present).
//
// Source and LastSynced are intentionally left at their zero values: the parser
// performs a pure wire→public mapping and never imputes ingest provenance or a
// timestamp — the caller (codegen bake or a sync) assigns those, mirroring how
// toModelInfo leaves Source/LastSynced empty on live rows.
func toEntityMetadata(mapKey string, wm wireModelMetadata) EntityMetadata {
	id := wm.ID
	if id == "" {
		id = mapKey
	}
	em := EntityMetadata{
		MetadataID:  MetadataID(id),
		Name:        wm.Name,
		Description: wm.Description,
		License:     wm.License,
		// The upstream models.json family verbatim — internal provenance carried so the
		// metadata<->entity join can key its family-presence gate off the same raw family
		// the catalog enrichment pipeline uses (see EntityMetadata.RawFamily).
		RawFamily: Family(wm.Family),
	}

	// links[] first, then folded weights[] (Type == LinkWeights), preserving order.
	if len(wm.Links) > 0 || len(wm.Weights) > 0 {
		links := make([]ModelLink, 0, len(wm.Links)+len(wm.Weights))
		for _, wl := range wm.Links {
			lt, rawT := detectLinkType(wl.Type)
			links = append(links, ModelLink{
				Label:   wl.Label,
				URL:     wl.URL,
				Type:    lt,
				TypeRaw: rawT,
			})
		}
		for _, ww := range wm.Weights {
			links = append(links, ModelLink{
				Label: ww.Label,
				URL:   ww.URL,
				Type:  LinkWeights,
			})
		}
		em.Links = links
	}

	if len(wm.Benchmarks) > 0 {
		bs := make([]BenchmarkResult, 0, len(wm.Benchmarks))
		for _, wb := range wm.Benchmarks {
			score, scoreRaw := decodeBenchmarkScore(wb.Score)
			bs = append(bs, BenchmarkResult{
				Name:      wb.Name,
				Version:   wb.Version,
				Variant:   wb.Variant,
				Dataset:   wb.Dataset,
				Harness:   wb.Harness,
				Metric:    wb.Metric,
				Score:     score,
				ScoreRaw:  scoreRaw,
				SourceURL: wb.Source,
				Date:      wb.Date,
			})
		}
		em.Benchmarks = bs
	}

	return em
}

// decodeBenchmarkScore tolerantly decodes an upstream benchmark score, which is
// either a JSON number or a JSON string. A number yields (score, ""); a string
// yields (0, verbatim) so the raw value rides on BenchmarkResult.ScoreRaw; an
// absent or otherwise unexpected value yields (0, raw-bytes) or (0, ""). It never
// returns an error, so a string score never fails the artifact parse nor drops
// the row.
func decodeBenchmarkScore(raw json.RawMessage) (score float64, scoreRaw string) {
	if len(raw) == 0 {
		return 0, ""
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return 0, s
	}
	// Neither number nor string (unexpected shape) — preserve the verbatim bytes
	// so nothing is silently dropped.
	return 0, strings.TrimSpace(string(raw))
}

// parseCapability converts a polymorphic JSON field to a Capability.
// The field may be:
//   - absent/null/empty → Capability{Supported: false}
//   - bool false → Capability{Supported: false}
//   - bool true → Capability{Supported: true}
//   - object (e.g. {"field": "reasoning_details"}) → Capability{Supported: true, Config: ...}
func parseCapability(raw json.RawMessage) Capability {
	if len(raw) == 0 {
		return Capability{}
	}
	// Try bool first.
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return Capability{Supported: b}
	}
	// Try object — an object means capability IS supported, with config.
	var cfg map[string]string
	if err := json.Unmarshal(raw, &cfg); err == nil {
		return Capability{Supported: true, Config: cfg}
	}
	return Capability{}
}

// toModalities converts string slices from the API into the typed Modalities
// value. Unrecognised modality strings are silently skipped to avoid breaking
// callers when the API adds new modality names in the future.
func toModalities(input, output []string) Modalities {
	parseList := func(ss []string) []Modality {
		out := make([]Modality, 0, len(ss))
		for _, s := range ss {
			var m Modality
			if err := m.UnmarshalText([]byte(s)); err == nil {
				out = append(out, m)
			}
		}
		return out
	}
	return Modalities{
		Input:  parseList(input),
		Output: parseList(output),
	}
}
