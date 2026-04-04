//go:generate go run ./cmd/bestiary-gen

// bestiary-gen fetches the models.dev API and writes models_static_gen.go
// into the bestiary package root. Run via: go generate ./...
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dayvidpham/bestiary"
)

// targetProviders is the set of providers whose models will be included in the
// generated static registry. Aggregator providers (e.g. openrouter) are excluded.
var targetProviders = map[string]struct{}{
	"anthropic": {},
	"google":    {},
	"openai":    {},
}

// outputPath is relative to the module root (where go generate is run from).
const outputPath = "models_static_gen.go"

const apiURL = "https://models.dev/api.json"

// --------------------------------------------------------------------------
// Private wire types for JSON deserialization from models.dev
// These are defined here (not in the main package) so the codegen tool is
// self-contained and can handle API schema evolution without breaking the
// library's wire.go.
// --------------------------------------------------------------------------

type genWireResponse map[string]genWireProvider

type genWireProvider struct {
	Models map[string]genWireModel `json:"models"`
}

// genWireModel mirrors the models.dev model object.
// interleaved is stored as json.RawMessage because the field is polymorphic:
// some providers use bool (true) and others use an object ({field: "..."}).
// Since we only care about our three target providers (anthropic, google, openai)
// which do not use this field, we just skip it.
type genWireModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Family           string            `json:"family"`
	Reasoning        bool              `json:"reasoning"`
	ToolCall         bool              `json:"tool_call"`
	Attachment       bool              `json:"attachment"`
	Temperature      bool              `json:"temperature"`
	StructuredOutput bool              `json:"structured_output"`
	Interleaved      json.RawMessage   `json:"interleaved"`
	OpenWeights      bool              `json:"open_weights"`
	ReleaseDate      string            `json:"release_date"`
	Knowledge        string            `json:"knowledge"`
	Cost             *genWireCost      `json:"cost"`
	Limit            *genWireLimit     `json:"limit"`
	Modalities       *genWireModality  `json:"modalities"`
}

type genWireCost struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	Reasoning  *float64 `json:"reasoning"`
	CacheRead  *float64 `json:"cache_read"`
	CacheWrite *float64 `json:"cache_write"`
}

type genWireLimit struct {
	Context *int `json:"context"`
	Output  *int `json:"output"`
}

type genWireModality struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-gen: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)

	models, err := fetchModels(ctx)
	if err != nil {
		return err
	}

	// Filter to the three target providers and stamp LastSynced.
	var filtered []bestiary.ModelInfo
	for _, m := range models {
		if _, ok := targetProviders[string(m.Provider)]; ok {
			m.LastSynced = now
			filtered = append(filtered, m)
		}
	}

	if len(filtered) == 0 {
		return fmt.Errorf(
			"no models found for providers anthropic/google/openai after filtering %d total models\n"+
				"  What: the API returned no models for the expected providers\n"+
				"  Why: the API response may have changed schema or the filter is incorrect\n"+
				"  How to fix: inspect the raw API response at %s",
			len(models), apiURL,
		)
	}

	src, err := generateSource(filtered)
	if err != nil {
		return fmt.Errorf("generate Go source: %w", err)
	}

	if err := os.WriteFile(outputPath, src, 0o644); err != nil {
		return fmt.Errorf(
			"write %s: %w\n"+
				"  What: could not write the generated file\n"+
				"  Why: file system permission or path issue\n"+
				"  Where: %s\n"+
				"  How to fix: ensure the working directory is the module root and is writable",
			outputPath, err, outputPath,
		)
	}

	fmt.Fprintf(os.Stdout,
		"bestiary-gen: wrote %s with %d models (anthropic=%d google=%d openai=%d) at %s\n",
		outputPath, len(filtered),
		countByProvider(filtered, bestiary.ProviderAnthropic),
		countByProvider(filtered, bestiary.ProviderGoogle),
		countByProvider(filtered, bestiary.ProviderOpenAI),
		now,
	)
	return nil
}

// fetchModels fetches all models from the models.dev API using the local wire
// types (not via the bestiary.Client) so we can handle the polymorphic
// `interleaved` field that would break json.Unmarshal into bestiary's wire.go.
func fetchModels(ctx context.Context) ([]bestiary.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"create HTTP request for %s: %w\n"+
				"  What: failed to construct the API request\n"+
				"  How to fix: this is a programming error — report it",
			apiURL, err,
		)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"HTTP GET %s: %w\n"+
				"  What: network request failed\n"+
				"  How to fix: check network connectivity and retry",
			apiURL, err,
		)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"unexpected HTTP status %d from %s; expected 200 OK\n"+
				"  What: the API returned a non-success status\n"+
				"  How to fix: check the API endpoint and try again",
			resp.StatusCode, apiURL,
		)
	}

	const maxBodyBytes = 10 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf(
			"read response body from %s: %w\n"+
				"  What: failed to read the API response body\n"+
				"  How to fix: retry the operation",
			apiURL, err,
		)
	}

	var apiResp genWireResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf(
			"decode JSON from %s: %w\n"+
				"  What: the API response JSON could not be decoded\n"+
				"  Why: the API schema may have changed in a way not handled by json.RawMessage\n"+
				"  How to fix: inspect the API response and update the wire types in cmd/bestiary-gen/main.go",
			apiURL, err,
		)
	}

	var models []bestiary.ModelInfo
	for providerSlug, prov := range apiResp {
		for _, wm := range prov.Models {
			models = append(models, genToModelInfo(providerSlug, wm))
		}
	}
	return models, nil
}

// genToModelInfo converts a genWireModel to bestiary.ModelInfo.
// LastSynced is intentionally left empty — the caller stamps it.
func genToModelInfo(providerSlug string, wm genWireModel) bestiary.ModelInfo {
	info := bestiary.ModelInfo{
		ID:               bestiary.ModelID(wm.ID),
		Provider:         bestiary.Provider(providerSlug),
		DisplayName:      wm.Name,
		Family:           wm.Family,
		Reasoning:        wm.Reasoning,
		ToolCall:         wm.ToolCall,
		Attachment:       wm.Attachment,
		Temperature:      wm.Temperature,
		StructuredOutput: wm.StructuredOutput,
		OpenWeights:      wm.OpenWeights,
		ReleaseDate:      wm.ReleaseDate,
		Knowledge:        wm.Knowledge,
		// interleaved field: parse bool from raw JSON if present; default false.
		Interleaved: parseBoolRaw(wm.Interleaved),
		LastSynced:  "",
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
		info.Modalities = genToModalities(wm.Modalities.Input, wm.Modalities.Output)
	}

	return info
}

// parseBoolRaw extracts a bool from a json.RawMessage.
// Returns true only when the raw value is the JSON literal "true".
// Any other value (object, null, false, absent) returns false.
func parseBoolRaw(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false
	}
	return b
}

// genToModalities converts string slices from the API into the typed Modalities
// value. Unrecognised modality strings are silently skipped.
func genToModalities(input, output []string) bestiary.Modalities {
	parseList := func(ss []string) []bestiary.Modality {
		out := make([]bestiary.Modality, 0, len(ss))
		for _, s := range ss {
			var m bestiary.Modality
			if err := m.UnmarshalText([]byte(s)); err == nil {
				out = append(out, m)
			}
		}
		return out
	}
	return bestiary.Modalities{
		Input:  parseList(input),
		Output: parseList(output),
	}
}

func countByProvider(models []bestiary.ModelInfo, p bestiary.Provider) int {
	n := 0
	for _, m := range models {
		if m.Provider == p {
			n++
		}
	}
	return n
}

// --------------------------------------------------------------------------
// Source generation
// --------------------------------------------------------------------------

// generateSource renders the []ModelInfo slice as a valid Go source file and
// formats it with go/format so the result is gofmt-clean.
func generateSource(models []bestiary.ModelInfo) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
	buf.WriteString("//go:generate go run ./cmd/bestiary-gen\n")
	buf.WriteString("\n")
	buf.WriteString("package bestiary\n")
	buf.WriteString("\n")
	// f64 helper avoids taking addresses of numeric literals inline.
	buf.WriteString("// f64 is a code-generation helper that returns a pointer to a float64 literal.\n")
	buf.WriteString("func f64(v float64) *float64 { return &v }\n")
	buf.WriteString("\n")
	buf.WriteString("var staticModels = []ModelInfo{\n")

	for _, m := range models {
		buf.WriteString("\t{\n")
		fmt.Fprintf(&buf, "\t\tID:                    %q,\n", m.ID)
		fmt.Fprintf(&buf, "\t\tProvider:              %s,\n", providerExpr(m.Provider))
		fmt.Fprintf(&buf, "\t\tDisplayName:           %q,\n", m.DisplayName)
		fmt.Fprintf(&buf, "\t\tFamily:                %q,\n", m.Family)
		fmt.Fprintf(&buf, "\t\tContextWindow:         %d,\n", m.ContextWindow)
		fmt.Fprintf(&buf, "\t\tMaxOutput:             %d,\n", m.MaxOutput)
		fmt.Fprintf(&buf, "\t\tReasoning:             %v,\n", m.Reasoning)
		fmt.Fprintf(&buf, "\t\tToolCall:              %v,\n", m.ToolCall)
		fmt.Fprintf(&buf, "\t\tAttachment:            %v,\n", m.Attachment)
		fmt.Fprintf(&buf, "\t\tTemperature:           %v,\n", m.Temperature)
		fmt.Fprintf(&buf, "\t\tStructuredOutput:      %v,\n", m.StructuredOutput)
		fmt.Fprintf(&buf, "\t\tInterleaved:           %v,\n", m.Interleaved)
		fmt.Fprintf(&buf, "\t\tOpenWeights:           %v,\n", m.OpenWeights)
		fmt.Fprintf(&buf, "\t\tCostInputPerMTok:      %s,\n", float64PtrExpr(m.CostInputPerMTok))
		fmt.Fprintf(&buf, "\t\tCostOutputPerMTok:     %s,\n", float64PtrExpr(m.CostOutputPerMTok))
		fmt.Fprintf(&buf, "\t\tCostReasoningPerMTok:  %s,\n", float64PtrExpr(m.CostReasoningPerMTok))
		fmt.Fprintf(&buf, "\t\tCostCacheReadPerMTok:  %s,\n", float64PtrExpr(m.CostCacheReadPerMTok))
		fmt.Fprintf(&buf, "\t\tCostCacheWritePerMTok: %s,\n", float64PtrExpr(m.CostCacheWritePerMTok))
		fmt.Fprintf(&buf, "\t\tReleaseDate:           %q,\n", m.ReleaseDate)
		fmt.Fprintf(&buf, "\t\tKnowledge:             %q,\n", m.Knowledge)
		fmt.Fprintf(&buf, "\t\tModalities:            %s,\n", modalitiesExpr(m.Modalities))
		fmt.Fprintf(&buf, "\t\tLastSynced:            %q,\n", m.LastSynced)
		buf.WriteString("\t},\n")
	}

	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format failed: %w\n"+
				"  What: the generated Go source is not syntactically valid\n"+
				"  Why: a codegen template bug produced invalid Go\n"+
				"  How to fix: inspect the unformatted buffer for syntax errors\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err,
			truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}

// providerExpr returns the Go expression for a Provider value.
// Known providers use their named constant; others use a typed string literal.
func providerExpr(p bestiary.Provider) string {
	switch p {
	case bestiary.ProviderAnthropic:
		return "ProviderAnthropic"
	case bestiary.ProviderGoogle:
		return "ProviderGoogle"
	case bestiary.ProviderOpenAI:
		return "ProviderOpenAI"
	default:
		return fmt.Sprintf("Provider(%q)", string(p))
	}
}

// float64PtrExpr renders a *float64 as either "nil" or "f64(<value>)".
func float64PtrExpr(p *float64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("f64(%v)", *p)
}

// modalitiesExpr renders a Modalities value as a Go composite literal.
func modalitiesExpr(m bestiary.Modalities) string {
	var sb strings.Builder
	sb.WriteString("Modalities{")
	if len(m.Input) > 0 {
		sb.WriteString("Input: []Modality{")
		for i, mod := range m.Input {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(modalityExpr(mod))
		}
		sb.WriteString("}")
	}
	if len(m.Output) > 0 {
		if len(m.Input) > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("Output: []Modality{")
		for i, mod := range m.Output {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(modalityExpr(mod))
		}
		sb.WriteString("}")
	}
	sb.WriteString("}")
	return sb.String()
}

// modalityExpr returns the Go constant name for a Modality.
func modalityExpr(m bestiary.Modality) string {
	switch m {
	case bestiary.ModalityText:
		return "ModalityText"
	case bestiary.ModalityImage:
		return "ModalityImage"
	case bestiary.ModalityPDF:
		return "ModalityPDF"
	case bestiary.ModalityAudio:
		return "ModalityAudio"
	case bestiary.ModalityVideo:
		return "ModalityVideo"
	default:
		return fmt.Sprintf("Modality(%d)", int(m))
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}
