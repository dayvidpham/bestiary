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
// HTTP-conditional seam (ETag/304, 429/Retry-After)
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

// --------------------------------------------------------------------------
// Wayback archive-snapshot enrichment — every case socket-free
// --------------------------------------------------------------------------

const (
	// hfLive is a stand-in live Hub repo page: the SourceURL a harvested nomen cites.
	hfLive = "https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct"
	// waybackHitBody is the documented HIT shape. Note the http:// scheme on the
	// archive host — the API answers with http, while the shape the codebase stores
	// (and bestiary.IsArchiveSnapshotURL accepts) is https, so the lookup must
	// normalize it. A test that hard-coded https here would not exercise that.
	waybackHitBody = `{"archived_snapshots":{"closest":{"available":true,` +
		`"url":"http://web.archive.org/web/20260715030540/https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct",` +
		`"timestamp":"20260715030540","status":"200"}}}`
	// waybackMissBody is the documented MISS shape: an empty object, with HTTP 200
	// and no error and no 404. This is the detail the API is easiest to get wrong on.
	waybackMissBody = `{"archived_snapshots":{}}`
	// wantSnapshot is the https-normalized snapshot the hit must yield.
	wantSnapshot = "https://web.archive.org/web/20260715030540/https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct"
)

// A HIT yields the snapshot, https-normalized, and in the shape the SHARED curated
// validator accepts — so what the bot writes is exactly what the loader will admit.
func TestLookupArchivedURL_Hit(t *testing.T) {
	c, _, sd, _ := newTestClient([]cannedResp{{status: 200, body: waybackHitBody}})
	got := lookupArchivedURL(context.Background(), c, hfLive)
	if got != wantSnapshot {
		t.Fatalf("lookupArchivedURL = %q, want %q (the http:// answer must be normalized to https://)", got, wantSnapshot)
	}
	if !bestiary.IsArchiveSnapshotURL(got) {
		t.Errorf("recorded snapshot %q is not accepted by the shared archive-snapshot shape check; the loader would reject it", got)
	}
	// The lookup went to the Availability API with the live URL query-escaped, and it
	// is a READ: never Save Page Now.
	if len(sd.reqs) != 1 {
		t.Fatalf("want exactly 1 request, got %d", len(sd.reqs))
	}
	req := sd.reqs[0]
	if req.Method != http.MethodGet {
		t.Errorf("method = %s, want GET (the archive is read-only for this project)", req.Method)
	}
	if got := req.URL.Query().Get("url"); got != hfLive {
		t.Errorf("query url = %q, want the live page %q", got, hfLive)
	}
	if !strings.HasPrefix(req.URL.String(), waybackAvailableBase) {
		t.Errorf("request URL %q does not target the Availability API", req.URL)
	}
	if strings.Contains(req.URL.String(), "save") {
		t.Error("the lookup must never touch Save Page Now")
	}
}

// The documented MISS shape — HTTP 200 with an empty archived_snapshots object — is
// a normal outcome: empty result, no error. Modelling it as an error (or reading a
// zero-value closest as a hit) is the single easiest way to get this API wrong.
func TestLookupArchivedURL_MissShapeIsNotAnError(t *testing.T) {
	c, _, _, _ := newTestClient([]cannedResp{{status: 200, body: waybackMissBody}})
	if got := lookupArchivedURL(context.Background(), c, hfLive); got != "" {
		t.Fatalf("lookupArchivedURL on the documented miss shape = %q, want \"\"", got)
	}
}

// Every not-a-usable-snapshot answer collapses to "" and never to an error or a
// fabricated URL: a non-2xx, a 5xx, available:false, an empty url, a malformed body,
// and a snapshot URL that fails the shared shape check.
func TestLookupArchivedURL_AllMissPaths(t *testing.T) {
	cases := map[string]cannedResp{
		"404 not found":       {status: 404, body: ""},
		"500 server error":    {status: 500, body: ""},
		"503 unavailable":     {status: 503, body: ""},
		"available false":     {status: 200, body: `{"archived_snapshots":{"closest":{"available":false,"url":"http://web.archive.org/web/20260715030540/` + hfLive + `"}}}`},
		"empty snapshot url":  {status: 200, body: `{"archived_snapshots":{"closest":{"available":true,"url":""}}}`},
		"unparseable body":    {status: 200, body: `not json at all`},
		"empty body":          {status: 200, body: ``},
		"non-snapshot url":    {status: 200, body: `{"archived_snapshots":{"closest":{"available":true,"url":"https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct"}}}`},
		"snapshot without ts": {status: 200, body: `{"archived_snapshots":{"closest":{"available":true,"url":"http://web.archive.org/web/https://huggingface.co/x"}}}`},
	}
	for name, resp := range cases {
		t.Run(name, func(t *testing.T) {
			c, _, _, _ := newTestClient([]cannedResp{resp})
			if got := lookupArchivedURL(context.Background(), c, hfLive); got != "" {
				t.Fatalf("lookupArchivedURL = %q, want \"\" (every non-usable answer is a MISS, never an error and never a fabricated URL)", got)
			}
		})
	}
}

// A 429 that survives hfDoer's Retry-After retry budget is a MISS, not a failure.
// The inherited backoff is exercised (the retries happen, honoring the header), and
// NO new backoff is added by the lookup: the only sleeps are the ones hfDoer and the
// politebot cadence already perform.
func TestLookupArchivedURL_PostRetry429IsAMiss(t *testing.T) {
	// hfDoer's budget is 3 retries; 5 consecutive 429s exhaust it (scriptedDoer
	// repeats its last entry, so the response stays 429 for every attempt).
	c, hd, sd, fc := newTestClient([]cannedResp{
		{status: 429, body: "", header: hdr("Retry-After", "2")},
	})
	got := lookupArchivedURL(context.Background(), c, hfLive)
	if got != "" {
		t.Fatalf("lookupArchivedURL on a post-retry 429 = %q, want \"\" (a rate-limited run records no snapshot and retries next refresh)", got)
	}
	if hd.lastStatus != http.StatusTooManyRequests {
		t.Errorf("lastStatus = %d, want 429 (the final response after the retry budget)", hd.lastStatus)
	}
	// The INHERITED Retry-After handling ran: hfDoer retried up to its budget and
	// slept the server-requested interval each time.
	if len(sd.reqs) != hd.maxRetry+1 {
		t.Errorf("transport calls = %d, want %d (hfDoer's existing retry budget, unchanged)", len(sd.reqs), hd.maxRetry+1)
	}
	if hd.count429 != hd.maxRetry {
		t.Errorf("count429 = %d, want %d", hd.count429, hd.maxRetry)
	}
	// No backoff beyond the inherited one: every sleep is either the 2s Retry-After
	// hfDoer performed or the politebot cadence. Nothing else sleeps.
	for _, slept := range fc.slept {
		if slept != 2*time.Second && slept > politebot.DefaultMinRequestInterval {
			t.Errorf("unexpected sleep of %v — the lookup must add NO backoff of its own; slept=%v", slept, fc.slept)
		}
	}
}

// AC-22's structural claim: the Wayback lookup and the Hub crawl share ONE
// politebot.Client, so the >=1s cadence is enforced ACROSS BOTH HOSTS. A second
// client would double the effective outbound rate against the archive — and the
// only way to see that is to interleave the two hosts through one client and check
// the gap between consecutive requests, which is what this does on a fake clock.
func TestWaybackAndHub_ShareOneClient_CadenceAcrossHosts(t *testing.T) {
	c, hd, sd, fc := newTestClient([]cannedResp{
		{status: 200, body: `{"id":"meta-llama/Llama-3.3-70B-Instruct"}`},
		{status: 200, body: waybackHitBody},
		{status: 200, body: `{"id":"BAAI/bge-m3"}`},
	})
	ctx := context.Background()
	start := fc.t

	// hd is the client's OWN hfDoer — the same instance the production run passes to
	// verifyRepo, so this exercises one seam end to end rather than a stand-in.
	if _, _, _, err := verifyRepo(ctx, c, hd, "meta-llama/Llama-3.3-70B-Instruct"); err != nil {
		t.Fatalf("verifyRepo: %v", err)
	}
	if got := lookupArchivedURL(ctx, c, hfLive); got != wantSnapshot {
		t.Fatalf("lookupArchivedURL = %q, want %q", got, wantSnapshot)
	}
	if _, _, _, err := verifyRepo(ctx, c, hd, "BAAI/bge-m3"); err != nil {
		t.Fatalf("second verifyRepo: %v", err)
	}

	if len(sd.reqs) != 3 {
		t.Fatalf("want 3 requests (hub, archive, hub) through the ONE client, got %d", len(sd.reqs))
	}
	// Two hosts appear, so the interleave is real and not an artifact.
	if !strings.Contains(sd.reqs[0].URL.Host, "huggingface.co") ||
		!strings.Contains(sd.reqs[1].URL.Host, "archive.org") ||
		!strings.Contains(sd.reqs[2].URL.Host, "huggingface.co") {
		t.Fatalf("hosts = %q/%q/%q, want hub/archive/hub interleaved",
			sd.reqs[0].URL.Host, sd.reqs[1].URL.Host, sd.reqs[2].URL.Host)
	}
	// Three requests through one cadence seam: the first is free, the two that follow
	// each wait the minimum interval — INCLUDING the hub request that follows the
	// archive one, which is only true because a single client tracks both.
	if want := 2 * politebot.DefaultMinRequestInterval; fc.t.Sub(start) < want {
		t.Errorf("elapsed fake time = %v across 3 requests on 2 hosts, want >= %v "+
			"(the cadence must be enforced across BOTH hosts by the one shared client)", fc.t.Sub(start), want)
	}
	// Every request carries the descriptive UA — the politeness identity is not lost
	// by routing a second host through the same seam.
	for i, r := range sd.reqs {
		if got := r.Header.Get("User-Agent"); got != testUA {
			t.Errorf("request %d User-Agent = %q, want %q", i, got, testUA)
		}
	}
}

// The seed carries the snapshot: a linked repo whose lookup hit writes archived_url,
// while one with no snapshot writes NO key at all (omitempty), so a seed of
// never-captured repos stays byte-identical to its pre-enrichment form.
func TestBuildHFOutput_ArchivedURL_EmittedAndOmitted(t *testing.T) {
	joined := []joinResult{
		{Repo: "meta-llama/Llama-3.3-70B-Instruct", Linked: true, ArchivedURL: wantSnapshot,
			Ref: hfRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}},
		{Repo: "BAAI/bge-m3", Linked: true, Ref: hfRef{Family: "bge"}},
	}
	out, _ := buildHFOutput(joined, hfFileOut{})
	byValue := map[string]hfNomenOut{}
	for _, n := range out.Nomina {
		byValue[n.Value] = n
	}
	if got := byValue["meta-llama/Llama-3.3-70B-Instruct"].ArchivedURL; got != wantSnapshot {
		t.Errorf("archived_url = %q, want %q", got, wantSnapshot)
	}
	if got := byValue["BAAI/bge-m3"].ArchivedURL; got != "" {
		t.Errorf("archived_url for the un-captured repo = %q, want \"\"", got)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"archived_url":"`+wantSnapshot+`"`) {
		t.Errorf("emitted seed does not carry the snapshot: %s", raw)
	}
	if strings.Contains(string(raw), `"archived_url":""`) {
		t.Error("an absent snapshot must emit NO archived_url key (omitempty), not an empty one")
	}
	// The live source_url is unchanged and still primary.
	if got := byValue["meta-llama/Llama-3.3-70B-Instruct"].SourceURL; got != hubRepoBase+"meta-llama/Llama-3.3-70B-Instruct" {
		t.Errorf("source_url = %q, want the unchanged live repo URL", got)
	}
}

// A MISS must not DELETE a snapshot an earlier run recorded. The archive does not
// un-capture a page, so "" from this run means "nothing recorded this run" (a rate
// limit, an outage) — overwriting durable evidence with it would silently destroy
// the citation on any throttled refresh. A NEW snapshot does replace an older one.
func TestBuildHFOutput_ArchivedURL_MissPreservesExisting(t *testing.T) {
	const older = "https://web.archive.org/web/20260101000000/https://huggingface.co/BAAI/bge-m3"
	const newer = "https://web.archive.org/web/20260801000000/https://huggingface.co/BAAI/bge-m3"
	existing := hfFileOut{SchemaVersion: 1, Nomina: []hfNomenOut{
		{Value: "BAAI/bge-m3", ResolveTo: hfRefOut{Family: "bge"}, SourceURL: hubRepoBase + "BAAI/bge-m3", ArchivedURL: older},
	}}

	// This run's lookup missed.
	out, _ := buildHFOutput([]joinResult{{Repo: "BAAI/bge-m3", Linked: true, Ref: hfRef{Family: "bge"}}}, existing)
	if len(out.Nomina) != 1 {
		t.Fatalf("got %d nomina, want 1", len(out.Nomina))
	}
	if got := out.Nomina[0].ArchivedURL; got != older {
		t.Errorf("archived_url after a MISS = %q, want the preserved %q — a miss is not a deletion", got, older)
	}

	// This run found a fresher snapshot: it wins.
	out2, _ := buildHFOutput([]joinResult{{Repo: "BAAI/bge-m3", Linked: true, ArchivedURL: newer, Ref: hfRef{Family: "bge"}}}, existing)
	if got := out2.Nomina[0].ArchivedURL; got != newer {
		t.Errorf("archived_url after a fresh HIT = %q, want the newer %q", got, newer)
	}
}

// normalizeArchiveScheme upgrades ONLY the leading archive-host scheme. The original
// URL embedded in the snapshot's tail keeps whatever scheme it was captured under —
// rewriting that would be falsifying what the archive recorded.
func TestNormalizeArchiveScheme(t *testing.T) {
	cases := map[string]string{
		"http://web.archive.org/web/20260715030540/http://docs.x.ai/models":  "https://web.archive.org/web/20260715030540/http://docs.x.ai/models",
		"https://web.archive.org/web/20260715030540/http://docs.x.ai/models": "https://web.archive.org/web/20260715030540/http://docs.x.ai/models",
		"http://web.archive.org/web/20260715030540/https://docs.x.ai/models": "https://web.archive.org/web/20260715030540/https://docs.x.ai/models",
		"https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct":           "https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct",
		"http://archive.ph/20260715030540/https://huggingface.co/x":          "http://archive.ph/20260715030540/https://huggingface.co/x",
	}
	for in, want := range cases {
		if got := normalizeArchiveScheme(in); got != want {
			t.Errorf("normalizeArchiveScheme(%q) = %q, want %q", in, got, want)
		}
	}
}
