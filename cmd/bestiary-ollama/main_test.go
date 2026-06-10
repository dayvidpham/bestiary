package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dayvidpham/bestiary"
)

// --------------------------------------------------------------------------
// Canned fixtures (NO network)
// --------------------------------------------------------------------------

// cannedCatalog is a small, hand-built stand-in for bestiary.StaticModels(). The
// join code accepts a catalog slice precisely so tests inject this instead of
// touching the real (generated) catalog or the network.
func cannedCatalog() []bestiary.ModelInfo {
	return []bestiary.ModelInfo{
		// Plain-join target: size + identity modifier both present.
		{ID: "llama-3.3-70b-instruct", Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
		// Alias-rescue target: ParamSize uncurated (recovered from ID), carries the
		// instruct modifier that Ollama's bare "llama3.1:8b" tag omits.
		{ID: "meta-llama/Meta-Llama-3.1-8B-Instruct", Family: "llama", Version: "3.1", Modifier: []string{"instruct"}},
		// Base for the lineage-linked finetune case.
		{ID: "mixtral-8x7b-instruct", Family: "mixtral", ParamSize: "8x7b", Modifier: []string{"instruct"}},
	}
}

// --------------------------------------------------------------------------
// Polite-bot seam: User-Agent + >=1s rate limit (URD R9)
// --------------------------------------------------------------------------

// recordingDoer captures the requests it receives and returns a canned 200 body.
type recordingDoer struct {
	reqs []*http.Request
	body string
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.reqs = append(d.reqs, req)
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(d.body)),
		Header:     make(http.Header),
	}, nil
}

// fakeClock is a controllable monotonic clock + sleeper. sleep advances the clock
// and records each slept duration, so the rate-limit gap is observable without
// real time.
type fakeClock struct {
	t     time.Time
	slept []time.Duration
}

func (c *fakeClock) now() time.Time { return c.t }
func (c *fakeClock) doSleep(d time.Duration) {
	c.slept = append(c.slept, d)
	c.t = c.t.Add(d)
}

func newTestClient(body string) (*politeClient, *recordingDoer, *fakeClock) {
	rd := &recordingDoer{body: body}
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := &politeClient{
		doer:        rd,
		ua:          userAgent,
		minInterval: minRequestInterval,
		now:         fc.now,
		sleep:       fc.doSleep,
	}
	return c, rd, fc
}

func TestPoliteClient_SetsUserAgent(t *testing.T) {
	c, rd, _ := newTestClient(`{}`)
	if _, err := c.get(context.Background(), "https://registry.ollama.ai/v2/library/x/manifests/y", manifestAccept); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(rd.reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(rd.reqs))
	}
	if got := rd.reqs[0].Header.Get("User-Agent"); got != userAgent {
		t.Fatalf("User-Agent = %q, want %q (politeness: a descriptive UA is mandatory)", got, userAgent)
	}
	if got := rd.reqs[0].Header.Get("Accept"); got != manifestAccept {
		t.Fatalf("Accept = %q, want %q", got, manifestAccept)
	}
}

func TestPoliteClient_SleepsBetweenRequests(t *testing.T) {
	c, _, fc := newTestClient(`{}`)
	ctx := context.Background()
	// First request: no sleep (nothing precedes it).
	if _, err := c.get(ctx, "https://x/1", ""); err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("first request must not sleep, slept=%v", fc.slept)
	}
	// Second request immediately after: must sleep the full minInterval.
	if _, err := c.get(ctx, "https://x/2", ""); err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if len(fc.slept) != 1 {
		t.Fatalf("second request must sleep exactly once, slept=%v", fc.slept)
	}
	if fc.slept[0] < minRequestInterval {
		t.Fatalf("rate-limit sleep = %v, want >= %v (URD R9)", fc.slept[0], minRequestInterval)
	}
}

// --------------------------------------------------------------------------
// Manifest + config-blob parsing (canned JSON from the research report)
// --------------------------------------------------------------------------

const cannedManifest = `{
  "schemaVersion": 2,
  "config": { "mediaType": "application/vnd.ollama.image.model", "digest": "sha256:cfg", "size": 485 },
  "layers": [
    { "mediaType": "application/vnd.ollama.image.model", "digest": "sha256:w", "size": 1321082688 },
    { "mediaType": "application/vnd.ollama.image.template", "digest": "sha256:t", "size": 1429 }
  ]
}`

const cannedConfigBlob = `{ "model_format": "gguf", "model_family": "llama", "model_type": "1.2B", "file_type": "Q8_0" }`

func TestParseManifest_WeightsLayer(t *testing.T) {
	m, err := parseManifest([]byte(cannedManifest))
	if err != nil {
		t.Fatalf("parseManifest: %v", err)
	}
	if got := m.weightsBytes(); got != 1321082688 {
		t.Fatalf("weightsBytes = %d, want 1321082688 (the model-layer size)", got)
	}
}

func TestParseManifest_RejectsNoLayers(t *testing.T) {
	if _, err := parseManifest([]byte(`{"schemaVersion":2,"layers":[]}`)); err == nil {
		t.Fatalf("want error for a manifest with no layers")
	}
}

func TestParseConfigBlob(t *testing.T) {
	c, err := parseConfigBlob([]byte(cannedConfigBlob))
	if err != nil {
		t.Fatalf("parseConfigBlob: %v", err)
	}
	if c.FileType != "Q8_0" || c.ModelType != "1.2B" {
		t.Fatalf("blob = %+v, want file_type=Q8_0 model_type=1.2B", c)
	}
}

// --------------------------------------------------------------------------
// THE JOIN
// --------------------------------------------------------------------------

func TestJoin_PlainDecompositionJoins(t *testing.T) {
	r := joinOllama("llama3.3:70b-instruct", cannedCatalog(), nil, nil)
	if !r.Joined {
		t.Fatalf("expected join, got decomp key %q (no catalog match)", r.Decomp.joinKey())
	}
	if r.ModelsDevID != "llama-3.3-70b-instruct" {
		t.Fatalf("ModelsDevID = %q, want llama-3.3-70b-instruct", r.ModelsDevID)
	}
	if r.Decomp.ParamSize != "70b" {
		t.Fatalf("ParamSize = %q, want 70b", r.Decomp.ParamSize)
	}
}

// VC6: a community finetune that does NOT join is KEPT (never dropped); with no
// determinable base it becomes a standalone entry AND lands in the unlinked list.
func TestOllamaCommunity_FinetuneKept(t *testing.T) {
	tags := []fetchedTag{
		{OllamaID: "wizardlm-uncensored:13b-q4_K_M", WeightsBytes: 7865000000},
	}
	out, unlinked := buildOutput(tags, cannedCatalog(), nil, nil)

	var kept *quantModelOut
	for i := range out.Models {
		if strings.HasPrefix(out.Models[i].ModelID, "ollama/wizardlm-uncensored") {
			kept = &out.Models[i]
		}
	}
	if kept == nil {
		t.Fatalf("community finetune was DROPPED — must be kept; models=%+v", out.Models)
	}
	if kept.BaseRef != "" {
		t.Fatalf("base should be unknown here, got base_ref=%q", kept.BaseRef)
	}
	if len(kept.Rows) != 1 || kept.Rows[0].Quant != "q4_k_m" {
		t.Fatalf("kept entry rows = %+v, want one q4_k_m row", kept.Rows)
	}
	found := false
	for _, u := range unlinked {
		if u == kept.ModelID {
			found = true
		}
	}
	if !found {
		t.Fatalf("base-unknown community model %q must appear in ollama_unlinked.json list %v", kept.ModelID, unlinked)
	}
}

// VC-R11b: a finetune whose base IS determinable (curated base table) is KEPT and
// carries an inferred base_ref — and is NOT unlinked.
func TestOllamaCommunity_LineageLinked(t *testing.T) {
	bases := map[string]string{"dolphin-mixtral": "mixtral-8x7b-instruct"}
	r := joinOllama("dolphin-mixtral:8x7b", cannedCatalog(), nil, bases)
	if r.Joined {
		t.Fatalf("a community finetune must not join a catalog entity directly")
	}
	if r.BaseRef != "mixtral-8x7b-instruct" {
		t.Fatalf("BaseRef = %q, want mixtral-8x7b-instruct (inferred from curated table)", r.BaseRef)
	}
	if r.Unlinked {
		t.Fatalf("base-known finetune must NOT be marked unlinked")
	}
}

// Base inference falls back to decomposition: stripping the leading author token
// exposes a base that exists in the catalog.
func TestInferBase_DecompositionFallback(t *testing.T) {
	r := joinOllama("myauthor-llama3.3:70b-instruct", cannedCatalog(), nil, nil)
	if r.Joined {
		t.Fatalf("the prefixed finetune should not join directly")
	}
	if r.BaseRef != "llama-3.3-70b-instruct" {
		t.Fatalf("BaseRef = %q, want llama-3.3-70b-instruct (decomposition fallback)", r.BaseRef)
	}
}

// Alias rescue: Ollama's bare "llama3.1:8b" omits the instruct modifier
// models.dev carries, so the mechanical decomposition does NOT match — the alias
// re-adds instruct and the join succeeds. Also pins the mutation: WITHOUT the
// alias there is no join.
func TestJoin_AliasRescue(t *testing.T) {
	// Mutation guard: no alias -> no join.
	bare := joinOllama("llama3.1:8b", cannedCatalog(), nil, nil)
	if bare.Joined {
		t.Fatalf("without an alias, llama3.1:8b must NOT join (missing instruct modifier)")
	}

	aliases := map[string]ollamaAlias{
		"llama3.1:8b": {Family: "llama", Version: "3.1", ParamSize: "8b", Modifier: []string{"instruct"}},
	}
	r := joinOllama("llama3.1:8b", cannedCatalog(), aliases, nil)
	if !r.Joined {
		t.Fatalf("alias rescue failed: llama3.1:8b should join via alias")
	}
	if r.ModelsDevID != "meta-llama/Meta-Llama-3.1-8B-Instruct" {
		t.Fatalf("ModelsDevID = %q, want meta-llama/Meta-Llama-3.1-8B-Instruct", r.ModelsDevID)
	}
}

// --------------------------------------------------------------------------
// Output determinism (sorted, byte-identical regardless of input order)
// --------------------------------------------------------------------------

func TestBuildOutput_Deterministic(t *testing.T) {
	a := []fetchedTag{
		{OllamaID: "llama3.3:70b-instruct-q8_0", WeightsBytes: 75176521728},
		{OllamaID: "llama3.3:70b-instruct-q4_K_M", WeightsBytes: 43033509888},
		{OllamaID: "wizardlm-uncensored:13b-q4_K_M", WeightsBytes: 7865000000},
	}
	// Same tags, different order.
	b := []fetchedTag{
		{OllamaID: "wizardlm-uncensored:13b-q4_K_M", WeightsBytes: 7865000000},
		{OllamaID: "llama3.3:70b-instruct-q4_K_M", WeightsBytes: 43033509888},
		{OllamaID: "llama3.3:70b-instruct-q8_0", WeightsBytes: 75176521728},
	}
	out1, ul1 := buildOutput(a, cannedCatalog(), nil, nil)
	out2, ul2 := buildOutput(b, cannedCatalog(), nil, nil)

	j1, _ := marshalJSON(out1)
	j2, _ := marshalJSON(out2)
	if string(j1) != string(j2) {
		t.Fatalf("buildOutput is not deterministic across input orderings:\n--- a ---\n%s\n--- b ---\n%s", j1, j2)
	}
	if !reflect.DeepEqual(ul1, ul2) {
		t.Fatalf("unlinked list not deterministic: %v vs %v", ul1, ul2)
	}

	// Within the joined llama entry, the two quant rows must be sorted by quant.
	var llama *quantModelOut
	for i := range out1.Models {
		if out1.Models[i].ModelID == "llama-3.3-70b-instruct" {
			llama = &out1.Models[i]
		}
	}
	if llama == nil {
		t.Fatalf("expected joined llama entry, got %+v", out1.Models)
	}
	if len(llama.Rows) != 2 || llama.Rows[0].Quant != "q4_k_m" || llama.Rows[1].Quant != "q8_0" {
		t.Fatalf("rows not sorted by quant: %+v", llama.Rows)
	}
}

// --------------------------------------------------------------------------
// datasources.json single stamp (committed-snapshot ingested_at)
// --------------------------------------------------------------------------

func TestStampOllamaIngestedAt(t *testing.T) {
	const in = `{
  "_comment": "x",
  "schema_version": 2,
  "sources": [
    { "id": "models.dev", "uri": "https://models.dev/api.json", "canonical_name": "models.dev" },
    { "id": "ollama", "uri": "https://registry.ollama.ai", "canonical_name": "Ollama Registry" }
  ],
  "ingested": [
    { "source_id": "models.dev", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 2 },
    { "source_id": "ollama", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 2 }
  ]
}`
	got, err := stampOllamaIngestedAt([]byte(in), "2026-07-01T12:00:00Z")
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	var f dsFileJSON
	if err := json.Unmarshal(got, &f); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	var ollamaAt, modelsDevAt string
	for _, ig := range f.Ingested {
		switch ig.SourceID {
		case "ollama":
			ollamaAt = ig.IngestedAt
		case "models.dev":
			modelsDevAt = ig.IngestedAt
		}
	}
	if ollamaAt != "2026-07-01T12:00:00Z" {
		t.Fatalf("ollama ingested_at = %q, want the stamped snapshot", ollamaAt)
	}
	if modelsDevAt != "2026-06-09T00:00:00Z" {
		t.Fatalf("models.dev ingested_at must be untouched, got %q", modelsDevAt)
	}
	if f.SchemaVersion != 2 || len(f.Sources) != 2 {
		t.Fatalf("file shape not preserved: %+v", f)
	}
}

func TestStampOllamaIngestedAt_MissingRow(t *testing.T) {
	const in = `{"schema_version":2,"sources":[],"ingested":[{"source_id":"models.dev","ingested_at":"x","parser_schema":2}]}`
	if _, err := stampOllamaIngestedAt([]byte(in), "2026-07-01T12:00:00Z"); err == nil {
		t.Fatalf("want error when no ollama ingest row exists")
	}
}

// --------------------------------------------------------------------------
// Committed-file shape compatibility
// --------------------------------------------------------------------------

// The tool's output structs must model the committed quant_vram.json exactly, so
// codegen's loader round-trips the tool's writes. Parsing the committed file with
// the tool's own structs proves the shapes agree.
func TestQuantVRAMShape_ParsesCommittedFile(t *testing.T) {
	raw, err := os.ReadFile("../../parse/data/quant_vram.json")
	if err != nil {
		t.Skipf("committed quant_vram.json not found: %v", err)
	}
	var f quantFileOut
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("tool structs cannot parse committed quant_vram.json: %v", err)
	}
	if f.SchemaVersion != quantVRAMSchemaVersion {
		t.Fatalf("committed schema_version = %d, tool writes %d", f.SchemaVersion, quantVRAMSchemaVersion)
	}
	if len(f.Models) == 0 {
		t.Fatalf("expected committed models")
	}
}

func TestAliasSeed_Parses(t *testing.T) {
	raw, err := os.ReadFile("../../parse/data/ollama_aliases.json")
	if err != nil {
		t.Skipf("seed not found: %v", err)
	}
	aliases, err := parseAliases(raw)
	if err != nil {
		t.Fatalf("parse seed: %v", err)
	}
	if _, ok := aliases["llama3.1:8b"]; !ok {
		t.Fatalf("seed must carry the llama3.1:8b rescue entry, got keys %v", keysOf(aliases))
	}
}

func keysOf(m map[string]ollamaAlias) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
