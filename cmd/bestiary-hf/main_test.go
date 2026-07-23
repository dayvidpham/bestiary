package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/internal/politebot"
)

const testUA = "bestiary-hf-test/0.0.0 (+offline)"

// cannedResp is one scripted HTTP response.
type cannedResp struct {
	status int
	body   string
	header http.Header
}

// scriptedDoer returns a queued sequence of canned responses and records every
// request (headers cloned at Do time, so an injected If-None-Match is observable).
// It is the offline transport the polite seam funnels through — no socket opened.
type scriptedDoer struct {
	seq  []cannedResp
	idx  int
	reqs []*http.Request
}

func (d *scriptedDoer) Do(req *http.Request) (*http.Response, error) {
	rec := req.Clone(req.Context())
	rec.Header = req.Header.Clone()
	d.reqs = append(d.reqs, rec)

	r := d.seq[d.idx]
	if d.idx < len(d.seq)-1 {
		d.idx++
	}
	h := r.header
	if h == nil {
		h = make(http.Header)
	}
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     h,
	}, nil
}

// fakeClock is the injectable monotonic clock + sleeper (the politebot test
// precedent): doSleep advances the clock instead of blocking.
type fakeClock struct {
	t     time.Time
	slept []time.Duration
}

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) doSleep(d time.Duration) { c.slept = append(c.slept, d); c.t = c.t.Add(d) }

// newTestClient wires a scriptedDoer through the SAME exported seam production
// uses: hfDoer (conditionals) beneath politebot.New (cadence + UA), fake clock.
func newTestClient(seq []cannedResp) (*politebot.Client, *hfDoer, *scriptedDoer, *fakeClock) {
	sd := &scriptedDoer{seq: seq}
	fc := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	hd := newHFDoer(sd, fc.doSleep)
	c := politebot.New(testUA, politebot.WithDoer(hd), politebot.WithClock(fc.now), politebot.WithSleep(fc.doSleep))
	return c, hd, sd, fc
}

func hdr(kv ...string) http.Header {
	h := make(http.Header)
	for i := 0; i+1 < len(kv); i += 2 {
		h.Set(kv[i], kv[i+1])
	}
	return h
}

// --------------------------------------------------------------------------
// HTTP-conditional seam (ETag/304, Link pagination, 429/Retry-After)
// --------------------------------------------------------------------------

// A first 200 that carries an ETag must cause the NEXT request to the same URL to
// carry If-None-Match: the conditional-request contract.
func TestHFDoer_SendsIfNoneMatch(t *testing.T) {
	c, _, sd, _ := newTestClient([]cannedResp{
		{status: 200, body: `{"id":"a/b"}`, header: hdr("ETag", `"v1"`)},
		{status: 200, body: `{"id":"a/b"}`},
	})
	ctx := context.Background()
	url := hubAPIModelBase + "a/b"
	if _, err := c.Get(ctx, url, jsonAccept); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if got := sd.reqs[0].Header.Get("If-None-Match"); got != "" {
		t.Fatalf("first request must not send If-None-Match, got %q", got)
	}
	if _, err := c.Get(ctx, url, jsonAccept); err != nil {
		t.Fatalf("second Get: %v", err)
	}
	if got := sd.reqs[1].Header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("second request If-None-Match = %q, want %q (ETag not replayed)", got, `"v1"`)
	}
}

// A 304 is surfaced to the caller as notModified (politebot rejects the non-2xx,
// hfDoer captures the status): the "unchanged, keep existing" path.
func TestHFDoer_NotModified304(t *testing.T) {
	c, hd, _, _ := newTestClient([]cannedResp{{status: 304, body: ""}})
	_, err := c.Get(context.Background(), hubAPIModelBase+"a/b", jsonAccept)
	if err == nil {
		t.Fatalf("want politebot non-2xx error for a 304")
	}
	if !hd.notModified() {
		t.Fatalf("hfDoer.notModified() = false, want true for a 304")
	}
	if hd.notFound() {
		t.Fatalf("hfDoer.notFound() = true, want false for a 304")
	}
}

// A 429 with a Retry-After is retried after the backoff sleep, then succeeds. The
// backoff sleep is asserted via the fake clock — no real time elapses.
func TestHFDoer_429_BackoffThenSuccess(t *testing.T) {
	c, hd, sd, fc := newTestClient([]cannedResp{
		{status: 429, body: "", header: hdr("Retry-After", "2")},
		{status: 200, body: `{"id":"a/b"}`},
	})
	body, err := c.Get(context.Background(), hubAPIModelBase+"a/b", jsonAccept)
	if err != nil {
		t.Fatalf("Get after 429 backoff: %v", err)
	}
	if !strings.Contains(string(body), `"a/b"`) {
		t.Fatalf("body = %q, want the 200 payload after retry", body)
	}
	if hd.lastStatus != 200 {
		t.Fatalf("lastStatus = %d, want 200 (429 must be retried past)", hd.lastStatus)
	}
	if len(sd.reqs) != 2 {
		t.Fatalf("want 2 transport calls (429 then retry), got %d", len(sd.reqs))
	}
	// The 429 backoff sleep (2s) must have been honored.
	var sawBackoff bool
	for _, s := range fc.slept {
		if s == 2*time.Second {
			sawBackoff = true
		}
	}
	if !sawBackoff {
		t.Fatalf("Retry-After=2s backoff not observed, slept=%v", fc.slept)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Errorf("parseRetryAfter(3) = %v, want 3s", got)
	}
	if got := parseRetryAfter("0"); got != time.Second {
		t.Errorf("parseRetryAfter(0) = %v, want clamp to 1s", got)
	}
	if got := parseRetryAfter(""); got != defaultRetryAfter {
		t.Errorf("parseRetryAfter(empty) = %v, want default %v", got, defaultRetryAfter)
	}
	if got := parseRetryAfter("Wed, 21 Oct 2026 07:28:00 GMT"); got != defaultRetryAfter {
		t.Errorf("parseRetryAfter(http-date) = %v, want default fallback", got)
	}
	if got := parseRetryAfter("100000"); got != maxRetryAfter {
		t.Errorf("parseRetryAfter(huge) = %v, want clamp to %v", got, maxRetryAfter)
	}
}

// --------------------------------------------------------------------------
// Case preservation (MUST-FAIL: lowercasing an HF org/repo)
// --------------------------------------------------------------------------

// The seam preserves an org/repo's case verbatim: the requested URL, the recorded
// nomen value, and the source_url all carry the original mixed case. A lowercase
// would be a different, non-existent repo.
func TestCasePreserved_EndToEnd(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	c, hd, sd, _ := newTestClient([]cannedResp{{status: 200, body: `{"id":"` + repo + `"}`}})
	id, exists, _, err := verifyRepo(context.Background(), c, hd, repo)
	if err != nil || !exists {
		t.Fatalf("verifyRepo(%q): exists=%v err=%v", repo, exists, err)
	}
	if id != repo {
		t.Errorf("verifyRepo returned id %q, want case-preserved %q", id, repo)
	}
	if got := sd.reqs[0].URL.String(); got != hubAPIModelBase+repo {
		t.Fatalf("request URL = %q, want case-preserved %q", got, hubAPIModelBase+repo)
	}
	// buildOutput preserves case in both value and source_url.
	j := joinResult{Repo: repo, Ref: hfRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}, Linked: true}
	out, _ := buildHFOutput([]joinResult{j}, hfFileOut{})
	if len(out.Nomina) != 1 {
		t.Fatalf("want 1 nomen, got %d", len(out.Nomina))
	}
	if out.Nomina[0].Value != repo {
		t.Errorf("nomen value = %q, want case-preserved %q", out.Nomina[0].Value, repo)
	}
	if want := hubRepoBase + repo; out.Nomina[0].SourceURL != want {
		t.Errorf("source_url = %q, want %q", out.Nomina[0].SourceURL, want)
	}
}

// --------------------------------------------------------------------------
// The JOIN (alias-first, unlinked report)
// --------------------------------------------------------------------------

func TestJoinHF_MechanicalLink(t *testing.T) {
	// A repo whose decomposition key is present in the catalog set links mechanically.
	repo := "meta-llama/Llama-3.3-70B-Instruct"
	key := decomposeHFRepo(repo).key()
	catalog := map[string]struct{}{key: {}}
	res := joinHF(repo, catalog, nil)
	if !res.Linked || res.Unlinked {
		t.Fatalf("joinHF(%q) = %+v, want linked", repo, res)
	}
}

func TestJoinHF_AliasOverridesMechanical(t *testing.T) {
	// The mechanical decomposition of "deepseek-ai/DeepSeek-V3.2" lands on a coarse
	// bucket; a curated alias pins the precise entity. The alias key must be the one
	// the catalog contains, proving the alias (not the decomposition) drove the link.
	repo := "deepseek-ai/DeepSeek-V3.2"
	alias := hfAlias{Family: "deepseek", Variant: "v3.2"}
	aliasKey := alias.ref().key()
	mechKey := decomposeHFRepo(repo).key()
	if aliasKey == mechKey {
		t.Skip("alias and mechanical keys coincide for this fixture; alias precedence not exercised")
	}
	catalog := map[string]struct{}{aliasKey: {}} // ONLY the alias key present
	res := joinHF(repo, catalog, map[string]hfAlias{repo: alias})
	if !res.Linked {
		t.Fatalf("joinHF(%q) with alias = %+v, want linked via alias", repo, res)
	}
	if res.Ref.key() != aliasKey {
		t.Errorf("joined ref key = %q, want alias key %q (mechanical would be %q)", res.Ref.key(), aliasKey, mechKey)
	}
}

func TestJoinHF_UnlinkedKeptNotDropped(t *testing.T) {
	repo := "some-org/totally-unknown-model-xyz"
	res := joinHF(repo, map[string]struct{}{}, nil)
	if !res.Unlinked || res.Linked {
		t.Fatalf("joinHF(%q) = %+v, want unlinked (kept, not dropped)", repo, res)
	}
	// It surfaces in the unlinked report, never in the seed.
	out, unlinked := buildHFOutput([]joinResult{res}, hfFileOut{})
	if len(out.Nomina) != 0 {
		t.Errorf("unlinked repo leaked into the seed: %+v", out.Nomina)
	}
	if len(unlinked) != 1 || unlinked[0] != repo {
		t.Errorf("unlinked report = %v, want [%q]", unlinked, repo)
	}
}

// --------------------------------------------------------------------------
// Output merge (field ownership) + determinism
// --------------------------------------------------------------------------

func TestBuildHFOutput_MergePreservesCuration_AndSorts(t *testing.T) {
	// A hand-added existing entry NOT re-verified this run is preserved (curation-
	// owned); a repo verified this run refreshes/adds. Output is sorted by value.
	existing := hfFileOut{
		SchemaVersion: hfNominaSchemaVersion,
		Nomina: []hfNomenOut{
			{Value: "aaa/curated-only", ResolveTo: hfRefOut{Family: "llama"}, SourceURL: hubRepoBase + "aaa/curated-only"},
			{Value: "zzz/refresh-me", ResolveTo: hfRefOut{Family: "qwen"}, SourceURL: hubRepoBase + "zzz/refresh-me"},
		},
	}
	joined := []joinResult{
		{Repo: "zzz/refresh-me", Ref: hfRef{Family: "qwen", Version: "3"}, Linked: true},
		{Repo: "mmm/new-repo", Ref: hfRef{Family: "mistral"}, Linked: true},
	}
	out, _ := buildHFOutput(joined, existing)
	got := make([]string, len(out.Nomina))
	for i, n := range out.Nomina {
		got[i] = n.Value
	}
	want := []string{"aaa/curated-only", "mmm/new-repo", "zzz/refresh-me"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("merged values = %v, want %v (sorted; curation preserved; refresh applied)", got, want)
	}
	// The refreshed entry took the fetch-derived version (3), not the stale existing.
	for _, n := range out.Nomina {
		if n.Value == "zzz/refresh-me" && n.ResolveTo.Version != "3" {
			t.Errorf("refreshed entry version = %q, want fetch-owned 3", n.ResolveTo.Version)
		}
	}
}

func TestBuildHFOutput_Deterministic(t *testing.T) {
	joined := []joinResult{
		{Repo: "b/two", Ref: hfRef{Family: "llama"}, Linked: true},
		{Repo: "a/one", Ref: hfRef{Family: "qwen"}, Linked: true},
		{Repo: "c/unl", Unlinked: true},
	}
	a1, u1 := buildHFOutput(joined, hfFileOut{})
	a2, u2 := buildHFOutput(joined, hfFileOut{})
	b1, _ := json.Marshal(a1)
	b2, _ := json.Marshal(a2)
	if string(b1) != string(b2) {
		t.Fatalf("buildHFOutput not deterministic:\n%s\n%s", b1, b2)
	}
	if strings.Join(u1, ",") != strings.Join(u2, ",") {
		t.Fatalf("unlinked not deterministic: %v vs %v", u1, u2)
	}
}

// --------------------------------------------------------------------------
// Candidate gathering
// --------------------------------------------------------------------------

func TestGatherCandidates_OrgRepoOpenWeightsOnly(t *testing.T) {
	catalog := []bestiary.ModelInfo{
		{ID: "meta-llama/Llama-3.3-70B-Instruct", OpenWeights: true},
		{ID: "anthropic/claude-opus-4", OpenWeights: false},          // closed weights → excluded
		{ID: "gpt-4o", OpenWeights: true},                            // no '/' → excluded
		{ID: "meta-llama/Llama-3.3-70B-Instruct", OpenWeights: true}, // dup → collapsed
		{ID: "Qwen/Qwen3-Coder-480B-A35B-Instruct", OpenWeights: true},
	}
	got := gatherCandidates(catalog)
	want := []string{"Qwen/Qwen3-Coder-480B-A35B-Instruct", "meta-llama/Llama-3.3-70B-Instruct"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("candidates = %v, want %v (org/repo + open-weights + sorted + deduped; case preserved)", got, want)
	}
}

func TestSelectCandidates_AlwaysIncludesAliasesExemptFromCap(t *testing.T) {
	base := []string{"a/1", "b/2", "c/3", "d/4"}
	aliases := map[string]hfAlias{"zzz/pinned": {Family: "llama"}}
	// limit 2 would drop c/3, d/4 AND zzz/pinned (which sorts last) — but the alias
	// must survive the cap.
	got := selectCandidates(base, aliases, 2)
	set := map[string]bool{}
	for _, c := range got {
		set[c] = true
	}
	if !set["zzz/pinned"] {
		t.Fatalf("aliased repo dropped by the cap: %v", got)
	}
	if !set["a/1"] || !set["b/2"] {
		t.Errorf("capped base candidates missing: %v", got)
	}
	// Sorted + deduped.
	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Errorf("not sorted/deduped: %v", got)
		}
	}
}

func TestLooksLikeOrgRepo(t *testing.T) {
	yes := []string{"meta-llama/Llama-3.3-70B-Instruct", "Qwen/Qwen3", "a/b"}
	no := []string{"gpt-4o", "a/b/c", "/b", "a/", ""}
	for _, s := range yes {
		if !looksLikeOrgRepo(s) {
			t.Errorf("looksLikeOrgRepo(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if looksLikeOrgRepo(s) {
			t.Errorf("looksLikeOrgRepo(%q) = true, want false", s)
		}
	}
}

// --------------------------------------------------------------------------
// Reserved-quant producer (§7): recognized, but NO guessed bpw
// --------------------------------------------------------------------------

func TestReservedQuant_RecognizedButNoBPW(t *testing.T) {
	for _, repo := range []string{"TheBloke/Model-AWQ", "org/model-gptq", "org/model-int8", "org/model-int4"} {
		q, raw, _ := bestiary.DetectQuantization(bestiary.ModelID(repo))
		if q == bestiary.QuantizationNone {
			t.Errorf("DetectQuantization(%q) found no quant; a reserved tag must be recognized", repo)
		}
		if bpw := q.BitsPerWeight(); bpw != 0 {
			t.Errorf("BitsPerWeight for %q (raw %q) = %v, want 0 (no guessed bpw feeds VRAM)", repo, raw, bpw)
		}
	}
}

// --------------------------------------------------------------------------
// datasources.json stamp
// --------------------------------------------------------------------------

func TestStampHFIngestedAt(t *testing.T) {
	raw := []byte(`{
  "schema_version": 3,
  "sources": [{"id":"huggingface","uri":"https://huggingface.co/api/models","canonical_name":"HuggingFace Hub"}],
  "ingested": [{"source_id":"huggingface","ingested_at":"2020-01-01T00:00:00Z","parser_schema":3}]
}`)
	out, err := stampHFIngestedAt(raw, "2026-07-23T12:00:00Z")
	if err != nil {
		t.Fatalf("stampHFIngestedAt: %v", err)
	}
	var f dsFileJSON
	if err := json.Unmarshal(out, &f); err != nil {
		t.Fatalf("unmarshal stamped: %v", err)
	}
	if f.Ingested[0].IngestedAt != "2026-07-23T12:00:00Z" {
		t.Errorf("ingested_at = %q, want the stamped snapshot", f.Ingested[0].IngestedAt)
	}
}

func TestStampHFIngestedAt_NoRowIsLoud(t *testing.T) {
	raw := []byte(`{"schema_version":3,"sources":[],"ingested":[]}`)
	if _, err := stampHFIngestedAt(raw, "2026-07-23T12:00:00Z"); err == nil {
		t.Fatalf("want a loud error when no huggingface ingest row exists")
	}
}

// --------------------------------------------------------------------------
// hf_aliases.json case-fold collision guard
// --------------------------------------------------------------------------

func TestLoadAliasesFromDir_CaseFoldCollisionIsLoud(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "schema_version": 1,
  "aliases": {
    "Org/Repo": {"family":"llama","variant":"","version":"","param_size":"","modifier":[]},
    "org/repo": {"family":"qwen","variant":"","version":"","param_size":"","modifier":[]}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "hf_aliases.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write hf_aliases.json: %v", err)
	}
	_, err := loadAliasesFromDir(dir)
	if err == nil {
		t.Fatalf("want a loud error when two alias keys case-fold to the same value")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Org/Repo") || !strings.Contains(msg, "org/repo") {
		t.Errorf("error %q does not name both colliding keys", msg)
	}
}

func TestLoadAliasesFromDir_NoCollisionOK(t *testing.T) {
	dir := t.TempDir()
	raw := `{
  "schema_version": 1,
  "aliases": {
    "Org/Repo": {"family":"llama","variant":"","version":"","param_size":"","modifier":[]},
    "Other/Repo": {"family":"qwen","variant":"","version":"","param_size":"","modifier":[]}
  }
}`
	if err := os.WriteFile(filepath.Join(dir, "hf_aliases.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write hf_aliases.json: %v", err)
	}
	aliases, err := loadAliasesFromDir(dir)
	if err != nil {
		t.Fatalf("loadAliasesFromDir: %v", err)
	}
	if len(aliases) != 2 {
		t.Errorf("len(aliases) = %d, want 2", len(aliases))
	}
}

// --------------------------------------------------------------------------
// huggingface_unlinked.json envelope shape (count field, modelsdev_unlinked.json precedent)
// --------------------------------------------------------------------------

func TestUnlinkedFileOut_CarriesCount(t *testing.T) {
	out := unlinkedFileOut{
		Comment:       unlinkedFileComment,
		SchemaVersion: hfNominaSchemaVersion,
		Count:         2,
		Unlinked:      []string{"a/b", "c/d"},
	}
	b, err := marshalJSON(out)
	if err != nil {
		t.Fatalf("marshalJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	count, ok := got["count"].(float64)
	if !ok {
		t.Fatalf("emitted JSON has no numeric \"count\" field: %s", b)
	}
	if int(count) != len(out.Unlinked) {
		t.Errorf("count = %v, want len(unlinked) = %d", count, len(out.Unlinked))
	}
}
