package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dayvidpham/bestiary"
)

// --------------------------------------------------------------------------
// Canned fixtures (NO network)
// --------------------------------------------------------------------------

// cannedCatalog is a small, hand-built stand-in for bestiary.StaticModels() for
// the unit-level join tests. The load-bearing join correctness check runs against
// the REAL compiled-in catalog (TestRealCatalog_AllowlistDisposition); these
// fixtures isolate individual behaviors.
func cannedCatalog() []bestiary.ModelInfo {
	return []bestiary.ModelInfo{
		{ID: "llama-3.3-70b-instruct", Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
		{ID: "meta-llama/Meta-Llama-3.1-8B-Instruct", Family: "llama", Version: "3.1", Modifier: []string{"instruct"}},
		{ID: "mixtral-8x7b-instruct", Family: "mixtral", ParamSize: "8x7b", Modifier: []string{"instruct"}},
	}
}

// emptyCurated is the no-prior-curation set (first-ever run).
var emptyCurated = map[string]bool{}

// emptyExisting is the no-prior-file document.
var emptyExisting = quantFileOut{SchemaVersion: quantVRAMSchemaVersion}

// --------------------------------------------------------------------------
// Polite-bot seam: User-Agent + >=1s rate limit
// --------------------------------------------------------------------------

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
		t.Fatalf("User-Agent = %q, want %q (a descriptive UA is mandatory)", got, userAgent)
	}
	if got := rd.reqs[0].Header.Get("Accept"); got != manifestAccept {
		t.Fatalf("Accept = %q, want %q", got, manifestAccept)
	}
}

func TestPoliteClient_SleepsBetweenRequests(t *testing.T) {
	c, _, fc := newTestClient(`{}`)
	ctx := context.Background()
	if _, err := c.get(ctx, "https://x/1", ""); err != nil {
		t.Fatalf("get 1: %v", err)
	}
	if len(fc.slept) != 0 {
		t.Fatalf("first request must not sleep, slept=%v", fc.slept)
	}
	if _, err := c.get(ctx, "https://x/2", ""); err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if len(fc.slept) != 1 {
		t.Fatalf("second request must sleep exactly once, slept=%v", fc.slept)
	}
	if fc.slept[0] < minRequestInterval {
		t.Fatalf("rate-limit sleep = %v, want >= %v (>= 1 second between requests)", fc.slept[0], minRequestInterval)
	}
}

// --------------------------------------------------------------------------
// Manifest + config-blob parsing (canned JSON captured from registry responses)
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
// normalizeOllamaName — family/version split, single-letter series guard
// --------------------------------------------------------------------------

// Pins both the positive splits AND the single-letter guard. Relaxing
// reAlphaNumSplit from {2,} to + must split "deepseek-r1" into "deepseek-r-1"
// and fail this table.
func TestNormalizeOllamaName(t *testing.T) {
	corpus := loadOllamaCorpus[string, string](t, ollamaNormalizeNameCorpusJSON, 8)
	ollamaRequireInputCoverage(t, corpus, map[string]string{
		"llama3.3":    "llama-3.3",
		"deepseek-r1": "deepseek-r1",
		"kimi-k2":     "kimi-k2",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := normalizeOllamaName(c.Input); got != c.Expected {
				t.Errorf("normalizeOllamaName(%q) = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// --------------------------------------------------------------------------
// THE JOIN — fixture-level behaviors
// --------------------------------------------------------------------------

func TestJoin_PlainDecompositionJoins(t *testing.T) {
	r := joinOllama("llama3.3:70b-instruct", cannedCatalog(), nil, nil, emptyCurated)
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

// A community finetune that does NOT join is KEPT (never dropped); with no
// determinable base it becomes a standalone entry AND lands in the unlinked list.
func TestOllamaCommunity_FinetuneKept(t *testing.T) {
	tags := []fetchedTag{
		{OllamaID: "wizardlm-uncensored:13b-q4_K_M", WeightsBytes: 7865000000},
	}
	out, unlinked := buildOutput(tags, cannedCatalog(), nil, nil, emptyCurated, emptyExisting)

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
		t.Fatalf("base-unknown community model %q must appear in the unlinked list %v", kept.ModelID, unlinked)
	}
}

// A finetune whose base IS determinable (curated base table) is KEPT and carries
// an inferred base_ref — and is NOT unlinked.
func TestOllamaCommunity_LineageLinked(t *testing.T) {
	bases := map[string]string{"dolphin-mixtral": "mixtral-8x7b-instruct"}
	r := joinOllama("dolphin-mixtral:8x7b", cannedCatalog(), nil, bases, emptyCurated)
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
	r := joinOllama("myauthor-llama3.3:70b-instruct", cannedCatalog(), nil, nil, emptyCurated)
	if r.Joined {
		t.Fatalf("the prefixed finetune should not join directly")
	}
	if r.BaseRef != "llama-3.3-70b-instruct" {
		t.Fatalf("BaseRef = %q, want llama-3.3-70b-instruct (decomposition fallback)", r.BaseRef)
	}
}

// Alias OVERRIDES the mechanical match — even when the mechanical bare key DOES
// match a (wrong) catalog row. Here the bare key llama@3.1#8b matches the
// non-instruct "cerebras/..." row, so the instruct fallback never fires; only an
// alias with precedence can redirect to the instruct entity. The mutation guard:
// inverting precedence (mechanical-first) lands on the wrong cerebras row.
func TestJoin_AliasOverride(t *testing.T) {
	catalog := []bestiary.ModelInfo{
		// Bare key llama@3.1#8b (no instruct) — a non-instruct provider variant.
		{ID: "cerebras/llama-3.1-8b-cs", Family: "llama", Version: "3.1", ParamSize: "8b"},
		// The instruct entity the Ollama default tag actually serves.
		{ID: "llama-3.1-8b-instruct", Family: "llama", Version: "3.1", ParamSize: "8b", Modifier: []string{"instruct"}},
	}

	// Without an alias, the bare mechanical key wins — the WRONG (non-instruct) row.
	bare := joinOllama("llama3.1:8b", catalog, nil, nil, emptyCurated)
	if !bare.Joined || bare.ModelsDevID != "cerebras/llama-3.1-8b-cs" {
		t.Fatalf("bare mechanical join = (%v, %q), want join to cerebras/llama-3.1-8b-cs", bare.Joined, bare.ModelsDevID)
	}

	// With the alias (precedence over mechanical), the join is redirected.
	aliases := map[string]ollamaAlias{
		"llama3.1:8b": {Family: "llama", Version: "3.1", ParamSize: "8b", Modifier: []string{"instruct"}},
	}
	r := joinOllama("llama3.1:8b", catalog, aliases, nil, emptyCurated)
	if !r.Joined {
		t.Fatalf("alias override failed: llama3.1:8b should join via alias")
	}
	if r.ModelsDevID != "llama-3.1-8b-instruct" {
		t.Fatalf("ModelsDevID = %q, want llama-3.1-8b-instruct (alias overrides the bare match)", r.ModelsDevID)
	}
}

// --------------------------------------------------------------------------
// REAL-CATALOG join (load-bearing): bestiary.StaticModels() is compiled in — no
// network. This is the test that catches mis-keying against the actual catalog.
// It pins the disposition of every model in defaultAllowlist (each head's sizes),
// and dies under the alias-precedence-inversion mutant and the matchCatalog
// lexicographic mutant; removing any llama alias entry also fails it.
// --------------------------------------------------------------------------

func TestRealCatalog_AllowlistDisposition(t *testing.T) {
	catalog := bestiary.StaticModels()
	aliases, err := loadAliasesFromDir("../../parse/data")
	if err != nil {
		t.Fatalf("load aliases: %v", err)
	}
	_, curated, err := loadExistingQuantVRAM("../../parse/data/quant_vram.json")
	if err != nil {
		t.Fatalf("load curated: %v", err)
	}

	type want struct {
		joined   bool
		modelsID string // when joined
	}
	cases := map[string]want{
		// Alias OVERRIDE pins the instruct entity (bare key would mis-match a
		// community-merge, a non-instruct provider variant, or a base entity).
		"llama3.3:70b": {joined: true, modelsID: "llama-3.3-70b-instruct"},
		"llama3.1:8b":  {joined: true, modelsID: "llama-3.1-8b-instruct"},
		"llama3.2:3b":  {joined: true, modelsID: "llama-3.2-3b-instruct"},
		"llama3.2:1b":  {joined: true, modelsID: "meta/llama-3.2-1b-instruct"},
		// Default-tag instruct FALLBACK (bare key misses, instruct hits).
		"qwen2.5:7b": {joined: true, modelsID: "qwen/qwen2.5-7b-instruct"},
		// Bare mechanical match to the canonical open-weights entity.
		"mistral:7b": {joined: true, modelsID: "open-mistral-7b"},
		// No joinable catalog entity at these sizes -> correctly KEPT (community).
		"gemma2:9b":   {joined: false},
		"phi3.5:3.8b": {joined: false},
	}

	for head, w := range cases {
		r := joinOllama(head, catalog, aliases, communityBaseRefs, curated)
		if r.Joined != w.joined {
			t.Errorf("%s: Joined = %v, want %v (key=%q, modelsID=%q)", head, r.Joined, w.joined, r.Decomp.joinKey(), r.ModelsDevID)
			continue
		}
		if w.joined && string(r.ModelsDevID) != w.modelsID {
			t.Errorf("%s: ModelsDevID = %q, want %q", head, r.ModelsDevID, w.modelsID)
		}
		if !w.joined && r.Joined {
			t.Errorf("%s: expected community (kept), got join to %q", head, r.ModelsDevID)
		}
	}
}

// --------------------------------------------------------------------------
// MERGE-ON-REFRESH: a refresh preserves curated arch facts + context_window +
// base_ref + _comment while refreshing fetch-owned weights. The wholesale-rewrite
// mutant (ignore existing) loses the arch facts and fails here.
// --------------------------------------------------------------------------

func TestBuildOutput_MergePreservesCuration(t *testing.T) {
	curated := map[string]bool{"llama-3.3-70b-instruct": true}
	existing := quantFileOut{
		SchemaVersion: quantVRAMSchemaVersion,
		Models: []quantModelOut{
			{
				Comment:       "curated note",
				ModelID:       "llama-3.3-70b-instruct",
				ParamSize:     "70b",
				Source:        "ollama",
				ContextWindow: 131072,
				Rows: []quantRowOut{
					{Quant: "q4_k_m", WeightsBytes: 43033509888, Layers: 80, KVHeads: 8, HeadDim: 128},
				},
			},
			// A curated entry NOT re-fetched this run must survive untouched.
			{ModelID: "llama-3.2-3b-instruct", ParamSize: "3b", Source: "ollama", Rows: []quantRowOut{{Quant: "q4_k_m", WeightsBytes: 2019139072}}},
		},
	}
	// Fresh fetch: same model:quant, NEW weights, NO arch facts.
	tags := []fetchedTag{{OllamaID: "llama3.3:70b-instruct-q4_K_M", WeightsBytes: 99999999999}}

	out, _ := buildOutput(tags, bestiary.StaticModels(), nil, nil, curated, existing)

	var e *quantModelOut
	for i := range out.Models {
		if out.Models[i].ModelID == "llama-3.3-70b-instruct" {
			e = &out.Models[i]
		}
	}
	if e == nil {
		t.Fatalf("refreshed entry missing; models=%v", modelIDs(out))
	}
	if len(e.Rows) != 1 {
		t.Fatalf("rows = %+v, want 1", e.Rows)
	}
	row := e.Rows[0]
	if row.WeightsBytes != 99999999999 {
		t.Errorf("weights_bytes = %d, want refreshed 99999999999", row.WeightsBytes)
	}
	if row.Layers != 80 || row.KVHeads != 8 || row.HeadDim != 128 {
		t.Errorf("curated arch facts LOST: layers=%d kv=%d hd=%d, want 80/8/128", row.Layers, row.KVHeads, row.HeadDim)
	}
	if e.ContextWindow != 131072 {
		t.Errorf("context_window = %d, want curated 131072 preserved", e.ContextWindow)
	}
	if e.Comment != "curated note" {
		t.Errorf("_comment = %q, want curated note preserved", e.Comment)
	}
	// The un-refetched curated entry must still be present.
	if !slices.Contains(modelIDs(out), "llama-3.2-3b-instruct") {
		t.Errorf("un-refetched curated entry llama-3.2-3b-instruct was dropped; got %v", modelIDs(out))
	}
}

func modelIDs(f quantFileOut) []string {
	out := make([]string, 0, len(f.Models))
	for _, m := range f.Models {
		out = append(out, m.ModelID)
	}
	return out
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
	b := []fetchedTag{
		{OllamaID: "wizardlm-uncensored:13b-q4_K_M", WeightsBytes: 7865000000},
		{OllamaID: "llama3.3:70b-instruct-q4_K_M", WeightsBytes: 43033509888},
		{OllamaID: "llama3.3:70b-instruct-q8_0", WeightsBytes: 75176521728},
	}
	out1, ul1 := buildOutput(a, cannedCatalog(), nil, nil, emptyCurated, emptyExisting)
	out2, ul2 := buildOutput(b, cannedCatalog(), nil, nil, emptyCurated, emptyExisting)

	j1, _ := marshalJSON(out1)
	j2, _ := marshalJSON(out2)
	if string(j1) != string(j2) {
		t.Fatalf("buildOutput is not deterministic across input orderings:\n--- a ---\n%s\n--- b ---\n%s", j1, j2)
	}
	if !reflect.DeepEqual(ul1, ul2) {
		t.Fatalf("unlinked list not deterministic: %v vs %v", ul1, ul2)
	}

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
// writeFileAtomic — content correctness + no leftover temp file
// --------------------------------------------------------------------------

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	if err := writeFileAtomic(path, []byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("content = %q, want %q", got, "hello\n")
	}
	// Overwrite an existing file (rename-over).
	if err := writeFileAtomic(path, []byte("world\n")); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	got, _ = os.ReadFile(path)
	if string(got) != "world\n" {
		t.Fatalf("overwrite content = %q, want world", got)
	}
	// No temp residue left behind.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".bestiary-ollama-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
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
	for _, k := range []string{"llama3.1:8b", "llama3.2:1b", "llama3.2:3b", "llama3.3:70b"} {
		if _, ok := aliases[k]; !ok {
			t.Fatalf("seed must carry the %q override entry, got keys %v", k, keysOf(aliases))
		}
	}
}

func keysOf(m map[string]ollamaAlias) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
