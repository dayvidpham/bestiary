package bestiary_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// The three fixtures below are hand-written to real upstream shape. They are
// deliberately shared so catalog.json is the EXACT composition of the api.json
// providers view and the models.json models view (see catalogFixtureJSON), which
// lets the composition-equivalence test assert
// ParseCatalogJSON == ParseAPIJSON + ParseModelsJSON.

// apiFixtureJSON is a models.dev api.json artifact (provider map). The "anthropic"
// provider carries two models exercising the v0.2.5 drift: description, an
// unknown status token, a discriminated reasoning_options array (effort /
// budget_tokens / toggle / an unknown kind), audio costs, a context_over_200k
// override, and a general cost tier.
const apiFixtureJSON = `{
  "anthropic": {
    "models": {
      "claude-omni-1": {
        "id": "claude-omni-1",
        "name": "Claude Omni 1",
        "family": "claude",
        "description": "An omni model.",
        "status": "experimental",
        "reasoning": true,
        "reasoning_options": [
          {"type": "effort", "values": ["low", "high"]},
          {"type": "budget_tokens", "min": 1024, "max": 32000},
          {"type": "toggle"},
          {"type": "wizardry"}
        ],
        "tool_call": true,
        "attachment": true,
        "temperature": true,
        "structured_output": true,
        "interleaved": false,
        "open_weights": false,
        "release_date": "2026-01-01",
        "knowledge": "2025-06",
        "experimental": {"modes": {"fast": {"cost": {"input": 1, "output": 2}}}},
        "provider": {"npm": "@ai-sdk/anthropic", "shape": "responses"},
        "cost": {
          "input": 3.0, "output": 15.0, "cache_read": 0.3, "cache_write": 3.75,
          "input_audio": 10.0, "output_audio": 20.0,
          "context_over_200k": {"input": 6.0, "output": 22.5},
          "tiers": [
            {"input": 5.0, "output": 25.0, "tier": {"type": "context", "size": 400000}}
          ]
        },
        "limit": {"context": 200000, "output": 64000},
        "modalities": {"input": ["text", "image", "audio"], "output": ["text"]}
      },
      "claude-lite-1": {
        "id": "claude-lite-1",
        "name": "Claude Lite 1",
        "family": "claude",
        "description": "A lite model.",
        "status": "beta",
        "reasoning": false,
        "tool_call": false,
        "attachment": false,
        "temperature": false,
        "open_weights": true,
        "release_date": "2026-02-01"
      }
    }
  }
}`

// modelsFixtureJSON is a models.dev models.json artifact (metadata map keyed by
// canonical <lab>/<model>). It exercises: a known link type, an unknown link type
// (→ LinkOther + raw), a folded weights[] row (→ LinkWeights), a numeric
// benchmark score, and a STRING benchmark score (→ ScoreRaw, row survives).
const modelsFixtureJSON = `{
  "anthropic/claude-omni-1": {
    "id": "anthropic/claude-omni-1",
    "name": "Claude Omni 1",
    "description": "An omni model.",
    "family": "claude",
    "license": "proprietary",
    "links": [
      {"label": "Docs", "url": "https://example.com/docs", "type": "docs"},
      {"label": "Mystery", "url": "https://example.com/x", "type": "sorcery"}
    ],
    "weights": [
      {"label": "GGUF", "url": "https://example.com/w.gguf", "format": "gguf", "quantization": "q4_k_m"}
    ],
    "benchmarks": [
      {"name": "MMLU", "score": 0.887, "metric": "accuracy", "harness": "lm-eval", "source": "https://example.com/blog", "date": "2026-01-02"},
      {"name": "GPQA", "score": "pass", "metric": "grade"}
    ]
  },
  "zhipuai/glm-5": {
    "id": "zhipuai/glm-5",
    "name": "GLM 5",
    "description": "A GLM model.",
    "license": "mit"
  }
}`

// catalogFixtureJSON is the catalog.json artifact: EXACTLY the composition of the
// two fixtures above, so ParseCatalogJSON must equal ParseAPIJSON + ParseModelsJSON.
var catalogFixtureJSON = `{"models":` + modelsFixtureJSON + `,"providers":` + apiFixtureJSON + `}`

func findModel(t *testing.T, ms []bestiary.ModelInfo, id bestiary.ModelID) bestiary.ModelInfo {
	t.Helper()
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("model %q not found among %d models", id, len(ms))
	return bestiary.ModelInfo{}
}

func findMeta(t *testing.T, ms []bestiary.EntityMetadata, id bestiary.MetadataID) bestiary.EntityMetadata {
	t.Helper()
	for _, m := range ms {
		if m.MetadataID == id {
			return m
		}
	}
	t.Fatalf("metadata %q not found among %d entries", id, len(ms))
	return bestiary.EntityMetadata{}
}

// TestParseAPIJSON_Drift verifies the api.json drift mapping: description, an
// unknown status token (StatusOther + raw), the reasoning_options discriminated
// union, audio costs, the context_over_200k override, and the cost tier.
func TestParseAPIJSON_Drift(t *testing.T) {
	models, err := bestiary.ParseAPIJSON([]byte(apiFixtureJSON))
	if err != nil {
		t.Fatalf("ParseAPIJSON: unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	omni := findModel(t, models, "claude-omni-1")

	if omni.Description != "An omni model." {
		t.Errorf("Description: got %q, want %q", omni.Description, "An omni model.")
	}
	// Unknown status token → StatusOther with raw preserved verbatim.
	if omni.Status != bestiary.StatusOther {
		t.Errorf("Status: got %v, want StatusOther", omni.Status)
	}
	if omni.StatusRaw != "experimental" {
		t.Errorf("StatusRaw: got %q, want %q", omni.StatusRaw, "experimental")
	}

	// reasoning_options discriminated union, order preserved.
	if len(omni.ReasoningOptions) != 4 {
		t.Fatalf("ReasoningOptions: got %d, want 4", len(omni.ReasoningOptions))
	}
	ro := omni.ReasoningOptions
	if ro[0].Kind != bestiary.ReasoningEffort {
		t.Errorf("ro[0].Kind: got %v, want ReasoningEffort", ro[0].Kind)
	}
	if !reflect.DeepEqual(ro[0].Values, []string{"low", "high"}) {
		t.Errorf("ro[0].Values: got %v, want [low high]", ro[0].Values)
	}
	if ro[1].Kind != bestiary.ReasoningBudgetTokens {
		t.Errorf("ro[1].Kind: got %v, want ReasoningBudgetTokens", ro[1].Kind)
	}
	if ro[1].MinTokens != 1024 || ro[1].MaxTokens != 32000 {
		t.Errorf("ro[1] budget: got min=%d max=%d, want 1024/32000", ro[1].MinTokens, ro[1].MaxTokens)
	}
	if ro[2].Kind != bestiary.ReasoningToggle {
		t.Errorf("ro[2].Kind: got %v, want ReasoningToggle", ro[2].Kind)
	}
	// Unknown reasoning-option kind → Other + raw.
	if ro[3].Kind != bestiary.ReasoningOptionOther {
		t.Errorf("ro[3].Kind: got %v, want ReasoningOptionOther", ro[3].Kind)
	}
	if ro[3].KindRaw != "wizardry" {
		t.Errorf("ro[3].KindRaw: got %q, want %q", ro[3].KindRaw, "wizardry")
	}

	// Audio costs.
	if omni.CostInputAudioPerMTok == nil || *omni.CostInputAudioPerMTok != 10.0 {
		t.Errorf("CostInputAudioPerMTok: got %v, want 10.0", omni.CostInputAudioPerMTok)
	}
	if omni.CostOutputAudioPerMTok == nil || *omni.CostOutputAudioPerMTok != 20.0 {
		t.Errorf("CostOutputAudioPerMTok: got %v, want 20.0", omni.CostOutputAudioPerMTok)
	}

	// context_over_200k override → CostContextOver200k *TierCost.
	if omni.CostContextOver200k == nil {
		t.Fatal("CostContextOver200k: expected non-nil")
	}
	if omni.CostContextOver200k.CostInputPerMTok == nil || *omni.CostContextOver200k.CostInputPerMTok != 6.0 {
		t.Errorf("CostContextOver200k.Input: got %v, want 6.0", omni.CostContextOver200k.CostInputPerMTok)
	}
	if omni.CostContextOver200k.CostOutputPerMTok == nil || *omni.CostContextOver200k.CostOutputPerMTok != 22.5 {
		t.Errorf("CostContextOver200k.Output: got %v, want 22.5", omni.CostContextOver200k.CostOutputPerMTok)
	}

	// General cost tier.
	if len(omni.CostTiers) != 1 {
		t.Fatalf("CostTiers: got %d, want 1", len(omni.CostTiers))
	}
	tier := omni.CostTiers[0]
	if tier.ContextSize != 400000 {
		t.Errorf("CostTiers[0].ContextSize: got %d, want 400000", tier.ContextSize)
	}
	if tier.CostInputPerMTok == nil || *tier.CostInputPerMTok != 5.0 {
		t.Errorf("CostTiers[0].Input: got %v, want 5.0", tier.CostInputPerMTok)
	}
	if tier.CostOutputPerMTok == nil || *tier.CostOutputPerMTok != 25.0 {
		t.Errorf("CostTiers[0].Output: got %v, want 25.0", tier.CostOutputPerMTok)
	}

	// Known status token maps to its constant, no raw.
	lite := findModel(t, models, "claude-lite-1")
	if lite.Status != bestiary.StatusBeta {
		t.Errorf("lite.Status: got %v, want StatusBeta", lite.Status)
	}
	if lite.StatusRaw != "" {
		t.Errorf("lite.StatusRaw: got %q, want empty", lite.StatusRaw)
	}
}

// TestParseModelsJSON_LinksBenchmarks verifies the models.json mapping: links
// with an unknown type (LinkOther + raw), a folded weights[] row (LinkWeights),
// a numeric benchmark score, and a STRING benchmark score preserved on ScoreRaw
// with the row still present.
func TestParseModelsJSON_LinksBenchmarks(t *testing.T) {
	meta, err := bestiary.ParseModelsJSON([]byte(modelsFixtureJSON))
	if err != nil {
		t.Fatalf("ParseModelsJSON: unexpected error: %v", err)
	}
	if len(meta) != 2 {
		t.Fatalf("expected 2 metadata entries, got %d", len(meta))
	}

	omni := findMeta(t, meta, "anthropic/claude-omni-1")
	if omni.Name != "Claude Omni 1" {
		t.Errorf("Name: got %q", omni.Name)
	}
	if omni.License != "proprietary" {
		t.Errorf("License: got %q, want proprietary", omni.License)
	}

	// links[] (2 rows) + folded weights[] (1 row) = 3 links.
	if len(omni.Links) != 3 {
		t.Fatalf("Links: got %d, want 3", len(omni.Links))
	}
	if omni.Links[0].Type != bestiary.LinkDocs || omni.Links[0].TypeRaw != "" {
		t.Errorf("Links[0]: got type=%v raw=%q, want docs/empty", omni.Links[0].Type, omni.Links[0].TypeRaw)
	}
	// Unknown link type → LinkOther + raw.
	if omni.Links[1].Type != bestiary.LinkOther || omni.Links[1].TypeRaw != "sorcery" {
		t.Errorf("Links[1]: got type=%v raw=%q, want other/sorcery", omni.Links[1].Type, omni.Links[1].TypeRaw)
	}
	// Folded weights row → LinkWeights, label preserved.
	if omni.Links[2].Type != bestiary.LinkWeights {
		t.Errorf("Links[2].Type: got %v, want LinkWeights", omni.Links[2].Type)
	}
	if omni.Links[2].Label != "GGUF" || omni.Links[2].URL != "https://example.com/w.gguf" {
		t.Errorf("Links[2]: got label=%q url=%q, want GGUF/…w.gguf", omni.Links[2].Label, omni.Links[2].URL)
	}

	// benchmarks: numeric + string score.
	if len(omni.Benchmarks) != 2 {
		t.Fatalf("Benchmarks: got %d, want 2", len(omni.Benchmarks))
	}
	mmlu := omni.Benchmarks[0]
	if mmlu.Name != "MMLU" || mmlu.Score != 0.887 || mmlu.ScoreRaw != "" {
		t.Errorf("MMLU: got name=%q score=%v raw=%q, want MMLU/0.887/empty", mmlu.Name, mmlu.Score, mmlu.ScoreRaw)
	}
	if mmlu.Metric != "accuracy" || mmlu.Harness != "lm-eval" {
		t.Errorf("MMLU apparatus: got metric=%q harness=%q", mmlu.Metric, mmlu.Harness)
	}
	if mmlu.SourceURL != "https://example.com/blog" || mmlu.Date != "2026-01-02" {
		t.Errorf("MMLU attribution: got source=%q date=%q", mmlu.SourceURL, mmlu.Date)
	}
	// String score → Score 0, ScoreRaw preserved, row NOT dropped.
	gpqa := omni.Benchmarks[1]
	if gpqa.Name != "GPQA" || gpqa.Score != 0 || gpqa.ScoreRaw != "pass" {
		t.Errorf("GPQA: got name=%q score=%v raw=%q, want GPQA/0/pass", gpqa.Name, gpqa.Score, gpqa.ScoreRaw)
	}

	// The no-links/no-benchmarks entry survives with empty slices.
	glm := findMeta(t, meta, "zhipuai/glm-5")
	if glm.License != "mit" {
		t.Errorf("glm License: got %q, want mit", glm.License)
	}
	if len(glm.Links) != 0 || len(glm.Benchmarks) != 0 {
		t.Errorf("glm: expected no links/benchmarks, got %d/%d", len(glm.Links), len(glm.Benchmarks))
	}
}

// TestParseCatalogJSON_CompositionEquivalence pins that ParseCatalogJSON equals
// the composition of ParseAPIJSON (over providers) and ParseModelsJSON (over
// models). Comparison is order-independent because all three flatten Go maps.
func TestParseCatalogJSON_CompositionEquivalence(t *testing.T) {
	cat, err := bestiary.ParseCatalogJSON([]byte(catalogFixtureJSON))
	if err != nil {
		t.Fatalf("ParseCatalogJSON: unexpected error: %v", err)
	}
	apiModels, err := bestiary.ParseAPIJSON([]byte(apiFixtureJSON))
	if err != nil {
		t.Fatalf("ParseAPIJSON: unexpected error: %v", err)
	}
	meta, err := bestiary.ParseModelsJSON([]byte(modelsFixtureJSON))
	if err != nil {
		t.Fatalf("ParseModelsJSON: unexpected error: %v", err)
	}

	// Models view equivalence (keyed by ID, order-independent).
	if len(cat.Models) != len(apiModels) {
		t.Fatalf("catalog Models len %d != api len %d", len(cat.Models), len(apiModels))
	}
	catByID := make(map[bestiary.ModelID]bestiary.ModelInfo, len(cat.Models))
	for _, m := range cat.Models {
		catByID[m.ID] = m
	}
	for _, want := range apiModels {
		got, ok := catByID[want.ID]
		if !ok {
			t.Errorf("catalog missing model %q", want.ID)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("catalog model %q != ParseAPIJSON result:\n got=%+v\nwant=%+v", want.ID, got, want)
		}
	}

	// Metadata view equivalence (keyed by MetadataID, order-independent).
	if len(cat.Metadata) != len(meta) {
		t.Fatalf("catalog Metadata len %d != models len %d", len(cat.Metadata), len(meta))
	}
	catByMeta := make(map[bestiary.MetadataID]bestiary.EntityMetadata, len(cat.Metadata))
	for _, m := range cat.Metadata {
		catByMeta[m.MetadataID] = m
	}
	for _, want := range meta {
		got, ok := catByMeta[want.MetadataID]
		if !ok {
			t.Errorf("catalog missing metadata %q", want.MetadataID)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("catalog metadata %q != ParseModelsJSON result:\n got=%+v\nwant=%+v", want.MetadataID, got, want)
		}
	}
}

// TestParsers_RowToleranceNeverFatal verifies that structurally-valid artifacts
// with individually-weird rows never fail the parse and never drop a row: a
// benchmark score that is neither number nor string is captured verbatim on
// ScoreRaw, and a link with a missing type maps to LinkOther with empty raw.
func TestParsers_RowToleranceNeverFatal(t *testing.T) {
	const weird = `{
  "lab/weird-1": {
    "id": "lab/weird-1",
    "name": "Weird 1",
    "description": "row-tolerance probe",
    "links": [
      {"label": "no type", "url": "https://example.com/n"}
    ],
    "benchmarks": [
      {"name": "ObjectScore", "score": {"nested": true}},
      {"name": "ArrayScore", "score": [1, 2, 3]},
      {"name": "Numeric", "score": 42}
    ]
  }
}`
	meta, err := bestiary.ParseModelsJSON([]byte(weird))
	if err != nil {
		t.Fatalf("ParseModelsJSON on weird-but-valid rows must NOT fail: %v", err)
	}
	m := findMeta(t, meta, "lab/weird-1")

	if len(m.Links) != 1 {
		t.Fatalf("Links: got %d, want 1", len(m.Links))
	}
	if m.Links[0].Type != bestiary.LinkOther || m.Links[0].TypeRaw != "" {
		t.Errorf("missing-type link: got type=%v raw=%q, want LinkOther/empty", m.Links[0].Type, m.Links[0].TypeRaw)
	}

	// All three benchmark rows survive; object/array scores land on ScoreRaw.
	if len(m.Benchmarks) != 3 {
		t.Fatalf("Benchmarks: got %d, want 3 (no row dropped)", len(m.Benchmarks))
	}
	if m.Benchmarks[0].Score != 0 || m.Benchmarks[0].ScoreRaw == "" {
		t.Errorf("object score: expected Score 0 + non-empty ScoreRaw, got %v/%q", m.Benchmarks[0].Score, m.Benchmarks[0].ScoreRaw)
	}
	if m.Benchmarks[1].Score != 0 || m.Benchmarks[1].ScoreRaw == "" {
		t.Errorf("array score: expected Score 0 + non-empty ScoreRaw, got %v/%q", m.Benchmarks[1].Score, m.Benchmarks[1].ScoreRaw)
	}
	if m.Benchmarks[2].Score != 42 || m.Benchmarks[2].ScoreRaw != "" {
		t.Errorf("numeric score: expected 42/empty, got %v/%q", m.Benchmarks[2].Score, m.Benchmarks[2].ScoreRaw)
	}
}

// TestParsers_MalformedArtifactErrors verifies that a structurally-invalid
// artifact (not the expected top-level shape) yields an actionable error rather
// than a panic or a silent empty result.
func TestParsers_MalformedArtifactErrors(t *testing.T) {
	if _, err := bestiary.ParseAPIJSON([]byte(`not json`)); err == nil {
		t.Error("ParseAPIJSON: expected error on non-JSON input")
	}
	if _, err := bestiary.ParseModelsJSON([]byte(`[1,2,3]`)); err == nil {
		t.Error("ParseModelsJSON: expected error on array-shaped input (want a map)")
	}
	if _, err := bestiary.ParseCatalogJSON([]byte(`42`)); err == nil {
		t.Error("ParseCatalogJSON: expected error on scalar input")
	}
}

// catalogTestServer returns an httptest.Server that serves the three fixtures by
// routing on the request path, plus a recorder of the paths it was asked for. A
// single server + WithBaseURL(<url>/api.json) covers all three fetch methods and
// proves the models.json/catalog.json URLs are derived correctly.
func catalogTestServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api.json":
			w.Write([]byte(apiFixtureJSON))
		case "/models.json":
			w.Write([]byte(modelsFixtureJSON))
		case "/catalog.json":
			w.Write([]byte(catalogFixtureJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := append([]string(nil), paths...)
		return out
	}
}

// TestFetchAllArtifacts_ViaDerivedURLs drives all three Client fetch methods
// against one server via WithBaseURL, proving the sibling-URL derivation routes
// FetchModelMetadata → /models.json and FetchCatalog → /catalog.json.
func TestFetchAllArtifacts_ViaDerivedURLs(t *testing.T) {
	srv, requested := catalogTestServer(t)
	defer srv.Close()

	// Base points at the api.json artifact; siblings derive from it. Fail fast so
	// a misrouted (404) request surfaces immediately rather than retrying.
	c := bestiary.NewClient(bestiary.WithBaseURL(srv.URL+"/api.json"), bestiary.WithRetries(0))
	ctx := context.Background()

	models, err := c.FetchModels(ctx)
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("FetchModels: got %d models, want 2", len(models))
	}

	meta, err := c.FetchModelMetadata(ctx)
	if err != nil {
		t.Fatalf("FetchModelMetadata: %v", err)
	}
	if len(meta) != 2 {
		t.Errorf("FetchModelMetadata: got %d entries, want 2", len(meta))
	}
	// The string-score row survives end-to-end through the fetch path.
	omni := findMeta(t, meta, "anthropic/claude-omni-1")
	if len(omni.Benchmarks) != 2 || omni.Benchmarks[1].ScoreRaw != "pass" {
		t.Errorf("FetchModelMetadata: string-score row not preserved: %+v", omni.Benchmarks)
	}

	cat, err := c.FetchCatalog(ctx)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(cat.Models) != 2 || len(cat.Metadata) != 2 {
		t.Errorf("FetchCatalog: got %d models / %d metadata, want 2/2", len(cat.Models), len(cat.Metadata))
	}

	// Confirm the derived paths were actually requested.
	got := requested()
	want := map[string]bool{"/api.json": false, "/models.json": false, "/catalog.json": false}
	for _, p := range got {
		if _, ok := want[p]; ok {
			want[p] = true
		}
	}
	for p, hit := range want {
		if !hit {
			t.Errorf("expected a request to %s (derived from base URL); requested paths were %v", p, got)
		}
	}
}

// TestFetchMetadata_RetryAndErrAPIUnavailable verifies the new metadata/catalog
// fetches share FetchModels' retry + structured-error semantics: a persistent
// 500 is retried retries+1 times and yields *ErrAPIUnavailable naming the derived
// URL.
func TestFetchMetadata_RetryAndErrAPIUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := bestiary.NewClient(
		bestiary.WithBaseURL(srv.URL+"/api.json"),
		bestiary.WithRetries(1),
	)
	_, err := c.FetchModelMetadata(context.Background())
	if err == nil {
		t.Fatal("FetchModelMetadata: expected error on persistent 500")
	}
	var apiErr *bestiary.ErrAPIUnavailable
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *ErrAPIUnavailable, got %T: %v", err, err)
	}
	if apiErr.Attempts != 2 {
		t.Errorf("Attempts: got %d, want 2", apiErr.Attempts)
	}
	if apiErr.URL == "" {
		t.Error("ErrAPIUnavailable.URL should name the derived models.json URL")
	}
}
