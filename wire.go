package bestiary

// Wire types for JSON deserialization from models.dev API.
// These types are unexported (package-internal); consumers use the public
// ModelInfo type returned by Client.FetchModels.

// wireResponse is the top-level API response — a map from provider slug to
// provider object.
type wireResponse map[string]wireProvider

// wireProvider holds the models map for a single provider.
type wireProvider struct {
	Models map[string]wireModel `json:"models"`
}

// wireModel represents a single model entry as returned by models.dev.
// All 17 fields are captured with JSON tags that match the API schema exactly.
type wireModel struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Family           string          `json:"family"`
	Reasoning        bool            `json:"reasoning"`
	ToolCall         bool            `json:"tool_call"`
	Attachment       bool            `json:"attachment"`
	Temperature      bool            `json:"temperature"`
	StructuredOutput bool            `json:"structured_output"`
	Interleaved      bool            `json:"interleaved"`
	OpenWeights      bool            `json:"open_weights"`
	ReleaseDate      string          `json:"release_date"`
	Knowledge        string          `json:"knowledge"`
	Cost             *wireCost       `json:"cost"`
	Limit            *wireLimit      `json:"limit"`
	Modalities       *wireModalities `json:"modalities"`
}

// wireCost holds per-token pricing information (USD per million tokens).
// All fields are pointers because any may be absent from the API response.
type wireCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	Reasoning  *float64 `json:"reasoning"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
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

// toModelInfo converts a wire-level model entry to the public ModelInfo type.
// providerSlug is the map key from wireResponse (e.g., "anthropic").
// LastSynced is intentionally left empty — callers set it on persist.
func toModelInfo(providerSlug string, wm wireModel) ModelInfo {
	info := ModelInfo{
		ID:               ModelID(wm.ID),
		Provider:         Provider(providerSlug),
		DisplayName:      wm.Name,
		Family:           wm.Family,
		Reasoning:        wm.Reasoning,
		ToolCall:         wm.ToolCall,
		Attachment:       wm.Attachment,
		Temperature:      wm.Temperature,
		StructuredOutput: wm.StructuredOutput,
		Interleaved:      wm.Interleaved,
		OpenWeights:      wm.OpenWeights,
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
