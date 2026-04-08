package bestiary

import "encoding/json"

// Wire types for JSON deserialization from models.dev API.
// These types are unexported (package-internal); consumers use the public
// ModelInfo type returned by Client.FetchModels.

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

// wireResponse is the top-level API response — a map from provider slug to
// provider object.
type wireResponse map[string]wireProvider

// wireProvider holds the models map for a single provider.
type wireProvider struct {
	Models map[string]wireModel `json:"models"`
}

// wireModel represents a single model entry as returned by models.dev.
// All 17 fields are captured with JSON tags that match the API schema exactly.
// Boolean capability fields use flexBool because the models.dev API occasionally
// returns objects or strings instead of booleans for some providers.
// Interleaved uses json.RawMessage because it is polymorphic: some providers
// send a bool and others send an object ({"field": "reasoning_details"}).
type wireModel struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Family           string          `json:"family"`
	Reasoning        flexBool        `json:"reasoning"`
	ToolCall         flexBool        `json:"tool_call"`
	Attachment       flexBool        `json:"attachment"`
	Temperature      flexBool        `json:"temperature"`
	StructuredOutput flexBool        `json:"structured_output"`
	Interleaved      json.RawMessage `json:"interleaved"`
	OpenWeights      flexBool        `json:"open_weights"`
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

// toModelInfo converts a wire-level model entry to the public ModelInfo type.
// providerSlug is the map key from wireResponse (e.g., "anthropic").
// LastSynced is intentionally left empty — callers set it on persist.
func toModelInfo(providerSlug string, wm wireModel) ModelInfo {
	info := ModelInfo{
		ID:               ModelID(wm.ID),
		Provider:         Provider(providerSlug),
		DisplayName:      wm.Name,
		Family:           Family(wm.Family),
		Reasoning:        bool(wm.Reasoning),
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
