// Command bestiary-hf is the OFFLINE, network-gated HuggingFace Hub refresh tool.
//
// A human runs this binary occasionally to refresh the harvested HuggingFace
// naming seed (parse/data/huggingface_nomina.json) that codegen folds into the
// bestiary nomen set. It is NOT part of `go test ./...` in any network sense:
// every live HTTP call lives behind run() (invoked only when the binary is
// executed), and the unit tests in main_test.go exercise the pure
// join/parse/output/HTTP-conditional seams with canned fixtures and a fake
// clock/transport — never the network.
//
// What it does when a human runs it:
//  1. Gather candidate Hub org/repo paths from the models.dev-known OPEN-WEIGHT
//     catalog rows (bestiary.StaticModels()): a HuggingFace id is org/repo, 1:1,
//     so the candidates are the open-weight raw ids of that shape. The id CASE IS
//     PRESERVED throughout — meta-llama/Llama-3.3-70B-Instruct is not
//     .../llama-3.3-70b-instruct (a lowercase would be a different, non-existent
//     repo).
//  2. VERIFY each candidate against the Hub API
//     (https://huggingface.co/api/models/<org>/<repo>) through a polite seam: a
//     descriptive versioned User-Agent and at least one second between requests
//     (the project hard constraint, GH#12), owned by internal/politebot. Layered
//     over that seam the tool adds the Hub-specific HTTP conditionals it needs:
//     an If-None-Match/ETag conditional request (a 304 means "unchanged — keep
//     the existing entry"), RFC-5988 Link-header pagination (follow rel="next",
//     never compute a page URL), and 429 backoff honoring Retry-After. A 404
//     means the candidate is not a real Hub repo and is skipped.
//  3. JOIN each verified repo onto a bestiary entity with the ALIAS-FIRST
//     precedence engine (curated hf_aliases.json OVERRIDES the mechanical
//     decomposition — the parse/ curated-overrides precedent — otherwise the repo
//     name is decomposed through the production parse pipeline into an EntityRef
//     key matched against the catalog). A repo that joins nothing is KEPT in a
//     sorted huggingface_unlinked.json report, never silently dropped.
//  4. EMIT parse/data/huggingface_nomina.json (the harvested seed). Field
//     ownership: the repo SET is fetch-owned (refreshed from the verified set);
//     entries not re-verified this run (e.g. a hand-added repo) are curation-owned
//     and PRESERVED (merge-on-refresh). The huggingface row of
//     parse/data/datasources.json gets its ingested_at stamped once per run (a
//     committed snapshot — codegen never stamps a wall-clock).
//
// Reserved-quant producer (§7): a Hub repo name may carry an awq/gptq/int8/int4
// quant tag. bestiary.DetectQuantization already recognizes these reserved
// HF-ecosystem members; the tool surfaces them but NEVER guesses a
// bits-per-weight (Quantization.BitsPerWeight stays 0 for the reserved four), so
// no fabricated bpw ever feeds VRAM math (URE Q9 caveat).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/internal/politebot"
)

// --------------------------------------------------------------------------
// Polite-bot identity + Hub endpoints
// --------------------------------------------------------------------------

const (
	// userAgent identifies this project on every outbound request so the Hub
	// operators can attribute (and contact about) the traffic. It is passed into
	// politebot.New; the polite seam sets it on every request and enforces the ≥1s
	// inter-request cadence. This is the HF bot's OWN descriptive versioned UA
	// (distinct from cmd/bestiary-ollama's).
	userAgent = "bestiary-hf/0.2.8 (+https://github.com/dayvidpham/bestiary; polite ingest bot)"

	// hubAPIModelBase is the per-repo metadata endpoint prefix. A GET of
	// hubAPIModelBase+"<org>/<repo>" returns 200 for a real repo, 404 otherwise.
	hubAPIModelBase = "https://huggingface.co/api/models/"

	// hubRepoBase is the live human-facing repo URL prefix. The harvested nomen's
	// SourceURL is hubRepoBase+"<org>/<repo>" (the live observation, case-preserved).
	hubRepoBase = "https://huggingface.co/"

	// jsonAccept is the Accept media type for the Hub JSON API.
	jsonAccept = "application/json"

	// defaultCandidateLimit bounds a single live run's request count so the polite
	// visitation stays well under the anonymous ceiling. Overridable with --limit.
	defaultCandidateLimit = 500

	// defaultRetryAfter is the fallback backoff when a 429 carries no parseable
	// Retry-After. It is deliberately >= the ≥1s cadence floor.
	defaultRetryAfter = 5 * time.Second

	// maxRetryAfter caps a pathological Retry-After so a hostile/misconfigured
	// header cannot stall the tool indefinitely.
	maxRetryAfter = 5 * time.Minute
)

// --------------------------------------------------------------------------
// hfDoer: the HTTP-conditional layer beneath politebot's cadence/UA seam
// --------------------------------------------------------------------------

// hfDoer wraps an inner Doer to add the header-level HTTP behaviors that
// politebot.Client.Get does not itself expose (it returns a body only): a
// conditional If-None-Match request from a per-URL ETag cache, RFC-5988 Link
// capture (for pagination), and 429/Retry-After backoff+retry. The division of
// labor is deliberate: internal/politebot owns the ONE politeness policy (the ≥1s
// cadence + descriptive UA, tested once), and hfDoer owns the Hub-specific
// conditionals. politebot.Client.Get funnels through this Doer, so the cadence
// guarantee is preserved while the bot reads the captured status/Link after each
// Get. It is NOT safe for concurrent use — the tool fetches sequentially (as the
// ≥1s cadence requires).
type hfDoer struct {
	inner    politebot.Doer
	etags    map[string]string   // url -> last-seen ETag (conditional cache)
	sleep    func(time.Duration) // Retry-After backoff (injectable for tests)
	maxRetry int                 // 429 retry budget per request

	// Captured from the most recent response for the caller to read after Get.
	lastStatus int
	lastLink   string
}

func newHFDoer(inner politebot.Doer, sleep func(time.Duration)) *hfDoer {
	return &hfDoer{inner: inner, etags: map[string]string{}, sleep: sleep, maxRetry: 3}
}

// Do sends one request, adding the conditional If-None-Match header (when an ETag
// is cached for this URL), retrying on 429 (sleeping the server-requested
// Retry-After, honored as backpressure distinct from the baseline cadence), and
// capturing the response status, Link header, and any new ETag. It returns the
// final response (2xx, 304, 404, or a still-429 after the retry budget) to
// politebot.Client.Get, which reads the body and applies its non-2xx reject.
func (d *hfDoer) Do(req *http.Request) (*http.Response, error) {
	url := req.URL.String()
	if et, ok := d.etags[url]; ok && et != "" {
		req.Header.Set("If-None-Match", et)
	}

	for attempt := 0; ; attempt++ {
		resp, err := d.inner.Do(req)
		if err != nil {
			return resp, err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < d.maxRetry {
			wait := parseRetryAfter(resp.Header.Get("Retry-After"))
			if resp.Body != nil {
				resp.Body.Close()
			}
			d.sleep(wait)
			continue
		}
		d.lastStatus = resp.StatusCode
		d.lastLink = resp.Header.Get("Link")
		if et := resp.Header.Get("ETag"); et != "" {
			d.etags[url] = et
		}
		return resp, nil
	}
}

// notModified reports whether the last response was a 304 (the conditional
// request matched — the resource is unchanged since the cached ETag).
func (d *hfDoer) notModified() bool { return d.lastStatus == http.StatusNotModified }

// notFound reports whether the last response was a 404 (the candidate is not a
// real Hub repo).
func (d *hfDoer) notFound() bool { return d.lastStatus == http.StatusNotFound }

// reLinkNext extracts the URL of a `rel="next"` link from an RFC-5988 Link header
// value: `<https://.../?cursor=...>; rel="next"`. Following the server-supplied
// next URL (never computing a page offset) is the pagination contract.
var reLinkNext = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

// parseLinkNext returns the rel="next" URL from a Link header value, or "" when
// there is no next page. A Link header may carry several comma-separated links;
// only the one whose rel is "next" is followed.
func parseLinkNext(link string) string {
	for _, part := range strings.Split(link, ",") {
		if m := reLinkNext.FindStringSubmatch(strings.TrimSpace(part)); m != nil {
			return m[1]
		}
	}
	return ""
}

// parseRetryAfter interprets a Retry-After header. It accepts the common
// delta-seconds form (an integer) and falls back to defaultRetryAfter for an
// absent or unparseable value (including the HTTP-date form, which the Hub does
// not use for this endpoint). The result is clamped to [1s, maxRetryAfter] so the
// backoff always honors the cadence floor and can never stall indefinitely.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return defaultRetryAfter
	}
	if secs, err := strconv.Atoi(h); err == nil {
		d := time.Duration(secs) * time.Second
		if d < time.Second {
			return time.Second
		}
		if d > maxRetryAfter {
			return maxRetryAfter
		}
		return d
	}
	return defaultRetryAfter
}

// --------------------------------------------------------------------------
// The JOIN: Hub org/repo -> bestiary entity (alias-first, joinOllama precedent)
// --------------------------------------------------------------------------

// hfRef is the entity-identity tuple a verified repo resolves to. It is exactly
// the resolves_to shape huggingface_nomina.json carries, so the join result
// serializes directly into the seed.
type hfRef struct {
	Family    bestiary.Family
	Variant   string
	Version   string
	ParamSize string
	Modifier  []string
}

// key renders the entity-identity key this ref maps to, mirroring how the registry
// builds entity keys (EntityModifiers projection + EntityRef.String), so a match
// against a catalog model's key is authoritative.
func (r hfRef) key() string {
	return bestiary.EntityRef{
		Family:    r.Family,
		Variant:   r.Variant,
		Version:   r.Version,
		ParamSize: r.ParamSize,
		Modifier:  bestiary.EntityModifiers(r.Modifier, r.Family),
	}.String()
}

// hfAlias is one curated join override from hf_aliases.json: it pins a repo path
// (case-preserved key) to the precise entity tuple, used where the mechanical
// decomposition would land on the wrong (usually coarser) entity — e.g.
// "deepseek-ai/DeepSeek-V3.2" decomposes to the coarse "deepseek" bucket while the
// repo names the "deepseek/v3.2" entity every other provider serves.
type hfAlias struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

func (a hfAlias) ref() hfRef {
	return hfRef{
		Family:    bestiary.Family(a.Family),
		Variant:   a.Variant,
		Version:   a.Version,
		ParamSize: a.ParamSize,
		Modifier:  a.Modifier,
	}
}

// aliasFile is the on-disk shape of hf_aliases.json (case-preserved repo keys).
type aliasFile struct {
	Comment       string             `json:"_comment,omitempty"`
	SchemaVersion int                `json:"schema_version"`
	Aliases       map[string]hfAlias `json:"aliases"`
}

// decomposeHFRepo mechanically decomposes a Hub repo path into its entity ref. The
// ORG namespace (before the '/') is dropped — it is a Hub account, not family
// material — and the repo NAME is decomposed through the production parse pipeline
// exactly as the registry decomposes a catalog id. A trailing quant tag
// (awq/gptq/int8/int4/q*) is stripped first (the reserved-quant producer: the tag
// is recognized, never guessed into a bpw). The repo value passed to the seed is
// the ORIGINAL path, case-preserved; only this join key is derived from it.
func decomposeHFRepo(repo string) hfRef {
	_, _, stripped := bestiary.DetectQuantization(bestiary.ModelID(repo))
	s := string(stripped)
	name := s
	if _, after, ok := strings.Cut(s, "/"); ok {
		name = after
	}
	fam, variant, version, mods, _ := bestiary.ParseFamilyDetailed(
		bestiary.Family(""), bestiary.ModelID(name), bestiary.ProviderHuggingFace)
	ps := ""
	if tok, ok := bestiary.ExtractParamSizeToken(name); ok {
		ps = tok
	}
	return hfRef{Family: fam, Variant: variant, Version: version, ParamSize: ps, Modifier: mods}
}

// joinResult is the outcome of joining one verified Hub repo onto the catalog.
type joinResult struct {
	Repo     string // the org/repo path, case-preserved
	Ref      hfRef  // the resolved entity tuple (valid when Linked)
	Linked   bool   // matched a catalog entity
	Unlinked bool   // no catalog entity (-> huggingface_unlinked.json)
}

// joinHF joins one verified Hub repo onto the catalog. Precedence (curated >
// mechanical), mirroring cmd/bestiary-ollama's joinOllama:
//  1. a curated hf_aliases.json entry OVERRIDES the mechanical decomposition (an
//     alias is needed precisely where the mechanical key matches the WRONG catalog
//     entity, so it is not a mere miss-rescue);
//  2. the mechanical decomposition's natural key;
//  3. otherwise KEEP the repo as unlinked (reported, never dropped).
func joinHF(repo string, catalog map[string]struct{}, aliases map[string]hfAlias) joinResult {
	res := joinResult{Repo: repo}

	// 1. Curated alias OVERRIDE (keyed case-preserved, then case-insensitively as a
	// convenience so a curator need not match case exactly).
	if a, ok := lookupAlias(repo, aliases); ok {
		r := a.ref()
		if _, hit := catalog[r.key()]; hit {
			res.Ref, res.Linked = r, true
			return res
		}
	}

	// 2. Mechanical natural key.
	r := decomposeHFRepo(repo)
	if _, hit := catalog[r.key()]; hit {
		res.Ref, res.Linked = r, true
		return res
	}

	// 3. Unlinked: KEPT, never dropped.
	res.Unlinked = true
	return res
}

// lookupAlias resolves an alias for repo: exact (case-preserved) first, then a
// lowercased-key convenience match.
func lookupAlias(repo string, aliases map[string]hfAlias) (hfAlias, bool) {
	if aliases == nil {
		return hfAlias{}, false
	}
	if a, ok := aliases[repo]; ok {
		return a, true
	}
	if a, ok := aliases[strings.ToLower(repo)]; ok {
		return a, true
	}
	return hfAlias{}, false
}

// --------------------------------------------------------------------------
// Candidate gathering + output assembly
// --------------------------------------------------------------------------

// reOrgRepo matches an org/repo Hub path: exactly one '/', both sides non-empty of
// the Hub-permitted character class. It deliberately rejects a bare id (no '/') and
// a multi-segment provider path.
var reOrgRepo = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// looksLikeOrgRepo reports whether id has the org/repo shape of a Hub model path.
func looksLikeOrgRepo(id string) bool { return reOrgRepo.MatchString(id) }

// gatherCandidates returns the sorted, de-duplicated set of open-weight catalog raw
// ids of org/repo shape — the models.dev-known candidates a live run verifies
// against the Hub. Case is preserved. Only OpenWeights rows are included (a
// closed-weights model has no public Hub repo to harvest).
func gatherCandidates(catalog []bestiary.ModelInfo) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, m := range catalog {
		if !m.OpenWeights {
			continue
		}
		id := string(m.ID)
		if !looksLikeOrgRepo(id) {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// catalogKeySet renders the set of catalog entity-identity keys for join matching.
func catalogKeySet(catalog []bestiary.ModelInfo) map[string]struct{} {
	keys := make(map[string]struct{}, len(catalog))
	for _, m := range catalog {
		ps := m.ParamSize
		if ps == "" {
			if tok, ok := bestiary.ExtractParamSizeToken(string(m.ID)); ok {
				ps = tok
			}
		}
		key := bestiary.EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: ps,
			Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
		}.String()
		keys[key] = struct{}{}
	}
	return keys
}

// hfRefOut mirrors huggingface_nomina.json nomina[].resolves_to.
type hfRefOut struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant,omitempty"`
	Version   string   `json:"version,omitempty"`
	ParamSize string   `json:"param_size,omitempty"`
	Modifier  []string `json:"modifier,omitempty"`
}

// hfNomenOut mirrors one huggingface_nomina.json nomina[] entry.
type hfNomenOut struct {
	Value     string   `json:"value"`
	ResolveTo hfRefOut `json:"resolves_to"`
	SourceURL string   `json:"source_url"`
}

// hfFileOut mirrors the top-level parse/data/huggingface_nomina.json shape.
type hfFileOut struct {
	Comment       string       `json:"_comment,omitempty"`
	SchemaVersion int          `json:"schema_version"`
	Nomina        []hfNomenOut `json:"nomina"`
}

const hfFileComment = "HARVESTED HuggingFace Hub naming seed (v0.2.8, GH#25/R1). Each entry records a Hub org/repo path an open-weight entity's WEIGHTS live at, as OBSERVED by the offline cmd/bestiary-hf bot fetching the Hub API (https://huggingface.co/api/models). This is the harvested-layer twin of nomen_claims.json's curated claims, with two deliberate differences: (1) NO archive-snapshot fence — source_url is the LIVE repo URL (https://huggingface.co/<org>/<repo>), because a harvested nomen cites the live observation the bot made, not durable third-party evidence (the archive-pin policy binds the curated layer ONLY); (2) CASE IS PRESERVED — an HF id is org/repo, 1:1, case-significant (meta-llama/Llama-3.3-70B-Instruct is not .../llama-3.3-70b-instruct), so value and source_url are stored verbatim and the loader cross-checks source_url == 'https://huggingface.co/' + value. Each nomen mints a NomenSchemeHuggingFace naming with attestation {SourceURL: live repo URL, Source: huggingface, Authority: primary (the Hub is authoritative for the huggingface scheme), Method: harvested}. FIELD OWNERSHIP: the bot owns the repo set (fetch-owned, refreshed each run); curation owns any hand override (preserved on refresh). An entity carrying an HF nomen DUAL-attests {models.dev, huggingface}. Graceful-degrade at runtime, LOUD at codegen (ValidateHFNomina). Sorted deterministically; regenerated by cmd/bestiary-hf, never hand-edited except curated overrides."

const hfNominaSchemaVersion = 1

// buildHFOutput assembles the merged seed from the join results of this run plus
// the existing file. It is the pure core shared by production and tests:
// deterministic regardless of input order. Field ownership: the repo set is
// fetch-owned — a linked repo verified this run refreshes (or adds) its entry;
// existing entries whose value was NOT re-verified this run are curation-owned and
// PRESERVED (merge-on-refresh, the ollama precedent). Unlinked repos never enter
// the seed (they go to the report). Returns the merged document and the sorted
// unlinked-repo list.
func buildHFOutput(joined []joinResult, existing hfFileOut) (hfFileOut, []string) {
	refreshed := map[string]hfNomenOut{}
	var unlinked []string
	for _, j := range joined {
		if j.Unlinked {
			unlinked = append(unlinked, j.Repo)
			continue
		}
		if !j.Linked {
			continue
		}
		refreshed[j.Value()] = hfNomenOut{
			Value: j.Repo,
			ResolveTo: hfRefOut{
				Family:    string(j.Ref.Family),
				Variant:   j.Ref.Variant,
				Version:   j.Ref.Version,
				ParamSize: j.Ref.ParamSize,
				Modifier:  j.Ref.Modifier,
			},
			SourceURL: hubRepoBase + j.Repo,
		}
	}

	out := hfFileOut{Comment: hfFileComment, SchemaVersion: hfNominaSchemaVersion}
	for _, n := range refreshed {
		out.Nomina = append(out.Nomina, n)
	}
	// Preserve existing entries not refreshed this run (curation-owned).
	for _, n := range existing.Nomina {
		if _, done := refreshed[n.Value]; done {
			continue
		}
		out.Nomina = append(out.Nomina, n)
	}

	sort.Slice(out.Nomina, func(i, j int) bool { return out.Nomina[i].Value < out.Nomina[j].Value })
	sort.Strings(unlinked)
	dedupSortedStrings(&unlinked)
	return out, unlinked
}

// Value is the seed value for a join result (the case-preserved repo path).
func (j joinResult) Value() string { return j.Repo }

// dedupSortedStrings removes adjacent duplicates from a sorted slice in place.
func dedupSortedStrings(s *[]string) {
	in := *s
	out := in[:0]
	for i, v := range in {
		if i > 0 && v == in[i-1] {
			continue
		}
		out = append(out, v)
	}
	*s = out
}

// unlinkedFileOut mirrors parse/data/huggingface_unlinked.json.
type unlinkedFileOut struct {
	Comment       string   `json:"_comment,omitempty"`
	SchemaVersion int      `json:"schema_version"`
	Unlinked      []string `json:"unlinked"`
}

const unlinkedFileComment = "Verified HuggingFace repos KEPT but joining no models.dev entity (visibility list, never a drop path). Written sorted by the offline tool; each is a real Hub repo that decomposed to no catalog entity — a curator either adds an hf_aliases.json override to link it, or confirms it is genuinely absent from the catalog."

// --------------------------------------------------------------------------
// datasources.json single stamp (committed-snapshot ingested_at)
// --------------------------------------------------------------------------

type dsSourceJSON struct {
	ID            string `json:"id"`
	URI           string `json:"uri"`
	CanonicalName string `json:"canonical_name"`
}

type dsIngestedJSON struct {
	SourceID     string `json:"source_id"`
	IngestedAt   string `json:"ingested_at"`
	ParserSchema int    `json:"parser_schema"`
}

type dsFileJSON struct {
	Comment       string           `json:"_comment,omitempty"`
	SchemaVersion int              `json:"schema_version"`
	Sources       []dsSourceJSON   `json:"sources"`
	Ingested      []dsIngestedJSON `json:"ingested"`
}

// stampHFIngestedAt sets the ingested_at of the single 'huggingface' ingest row to
// snapshot, exactly once, preserving every other field and the file shape. It is
// the committed-snapshot write (codegen never stamps a wall-clock).
func stampHFIngestedAt(raw []byte, snapshot string) ([]byte, error) {
	var f dsFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf(
			"bestiary-hf: parse datasources.json failed: %w\n"+
				"  Where: stampHFIngestedAt\n"+
				"  How to fix: validate parse/data/datasources.json syntax", err)
	}
	stamped := false
	for i := range f.Ingested {
		if f.Ingested[i].SourceID == string(bestiary.DataSourceHuggingFace) {
			f.Ingested[i].IngestedAt = snapshot
			stamped = true
		}
	}
	if !stamped {
		return nil, fmt.Errorf(
			"bestiary-hf: no 'huggingface' ingest row in datasources.json\n"+
				"  What: stamping found no source_id==%q row to update\n"+
				"  Where: stampHFIngestedAt\n"+
				"  How to fix: add a 'huggingface' entry to the 'ingested' array first",
			string(bestiary.DataSourceHuggingFace))
	}
	return marshalJSON(f)
}

// --------------------------------------------------------------------------
// JSON write helpers (2-space indent, trailing newline — matches committed files)
// --------------------------------------------------------------------------

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bestiary-hf: marshal JSON failed: %w", err)
	}
	return append(b, '\n'), nil
}

// writeFileAtomic writes data to path atomically (temp file in the same dir +
// rename), so a crash mid-write leaves either the old file or the new one.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bestiary-hf-*.tmp")
	if err != nil {
		return fmt.Errorf(
			"bestiary-hf: create temp file in %q failed: %w\n"+
				"  Where: writeFileAtomic\n"+
				"  How to fix: verify the directory exists and is writable", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("bestiary-hf: write temp file %q failed: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("bestiary-hf: close temp file %q failed: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf(
			"bestiary-hf: rename %q -> %q failed: %w\n"+
				"  Where: writeFileAtomic\n"+
				"  How to fix: ensure the temp and target are on the same filesystem", tmpName, path, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Curated-input + existing-seed loading (graceful degrade)
// --------------------------------------------------------------------------

// loadAliasesFromDir reads hf_aliases.json from dir; a missing file degrades to an
// empty (no-override) table.
func loadAliasesFromDir(dir string) (map[string]hfAlias, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "hf_aliases.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]hfAlias{}, nil
		}
		return nil, fmt.Errorf("bestiary-hf: read hf_aliases.json: %w", err)
	}
	var f aliasFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf(
			"bestiary-hf: parse hf_aliases.json failed: %w\n"+
				"  Where: loadAliasesFromDir\n"+
				"  How to fix: validate the alias-file JSON syntax", err)
	}
	return f.Aliases, nil
}

// loadExistingHFNomina reads the current huggingface_nomina.json for
// merge-on-refresh. A missing file degrades to an empty document (first run).
func loadExistingHFNomina(path string) (hfFileOut, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return hfFileOut{SchemaVersion: hfNominaSchemaVersion}, nil
		}
		return hfFileOut{}, fmt.Errorf("bestiary-hf: read %q: %w", path, err)
	}
	var f hfFileOut
	if err := json.Unmarshal(raw, &f); err != nil {
		return hfFileOut{}, fmt.Errorf(
			"bestiary-hf: parse %q failed: %w\n"+
				"  How to fix: validate the huggingface_nomina.json syntax before refreshing", path, err)
	}
	return f, nil
}

// --------------------------------------------------------------------------
// Live fetch (human-run only; never exercised by go test)
// --------------------------------------------------------------------------

// hfModelResp is the subset of the Hub /api/models/<id> response the tool reads.
// The `id` is the case-authoritative canonical repo path.
type hfModelResp struct {
	ID string `json:"id"`
}

// verifyRepo verifies one candidate against the Hub per-repo API. It returns:
//   - exists=true + the case-authoritative repo id, when the repo is present (200);
//   - exists=false when the candidate is not a Hub repo (404) or unchanged (304);
//   - a non-nil error for any other failure (which the caller reports and skips).
//
// A 304 (unchanged since the cached ETag) is reported as exists=false with
// notModified=true so the caller keeps the existing entry unmodified.
func verifyRepo(ctx context.Context, c *politebot.Client, d *hfDoer, repo string) (id string, exists, notModified bool, err error) {
	body, gerr := c.Get(ctx, hubAPIModelBase+repo, jsonAccept)
	if gerr != nil {
		switch {
		case d.notModified():
			return "", false, true, nil
		case d.notFound():
			return "", false, false, nil
		default:
			return "", false, false, gerr
		}
	}
	var m hfModelResp
	if uerr := json.Unmarshal(body, &m); uerr != nil {
		return "", false, false, fmt.Errorf("bestiary-hf: parse Hub response for %q: %w", repo, uerr)
	}
	id = m.ID
	if id == "" {
		id = repo // defensive: fall back to the requested path (case-preserved)
	}
	return id, true, false, nil
}

// --------------------------------------------------------------------------
// run / main
// --------------------------------------------------------------------------

func run(args []string) error {
	dataDir := "parse/data"
	snapshot := time.Now().UTC().Format(time.RFC3339)
	limit := defaultCandidateLimit
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--data-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("bestiary-hf: --data-dir requires a value")
			}
			dataDir = args[i+1]
			i++
		case "--snapshot":
			if i+1 >= len(args) {
				return fmt.Errorf("bestiary-hf: --snapshot requires an RFC3339 value")
			}
			snapshot = args[i+1]
			i++
		case "--limit":
			if i+1 >= len(args) {
				return fmt.Errorf("bestiary-hf: --limit requires an integer value")
			}
			n, perr := strconv.Atoi(args[i+1])
			if perr != nil || n <= 0 {
				return fmt.Errorf("bestiary-hf: --limit must be a positive integer, got %q", args[i+1])
			}
			limit = n
			i++
		default:
			return fmt.Errorf(
				"bestiary-hf: unknown argument %q\n"+
					"  Usage: bestiary-hf [--data-dir parse/data] [--snapshot RFC3339] [--limit N]", args[i])
		}
	}

	ctx := context.Background()
	inner := &http.Client{Timeout: 30 * time.Second}
	d := newHFDoer(inner, time.Sleep)
	c := politebot.New(userAgent, politebot.WithDoer(d))

	aliases, err := loadAliasesFromDir(dataDir)
	if err != nil {
		return err
	}
	existingPath := filepath.Join(dataDir, "huggingface_nomina.json")
	existing, err := loadExistingHFNomina(existingPath)
	if err != nil {
		return err
	}

	catalog := bestiary.StaticModels()
	candidates := gatherCandidates(catalog)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	keys := catalogKeySet(catalog)

	var joined []joinResult
	verified, missing, unchanged := 0, 0, 0
	for _, repo := range candidates {
		id, exists, notModified, verr := verifyRepo(ctx, c, d, repo)
		if verr != nil {
			fmt.Fprintf(os.Stderr, "bestiary-hf: skip %q: %v\n", repo, verr)
			continue
		}
		if notModified {
			unchanged++
			continue
		}
		if !exists {
			missing++
			continue
		}
		verified++
		joined = append(joined, joinHF(id, keys, aliases))
	}

	out, unlinked := buildHFOutput(joined, existing)

	seedBytes, err := marshalJSON(out)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(existingPath, seedBytes); err != nil {
		return err
	}

	unlinkedBytes, err := marshalJSON(unlinkedFileOut{
		Comment:       unlinkedFileComment,
		SchemaVersion: hfNominaSchemaVersion,
		Unlinked:      unlinked,
	})
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dataDir, "huggingface_unlinked.json"), unlinkedBytes); err != nil {
		return err
	}

	dsPath := filepath.Join(dataDir, "datasources.json")
	dsRaw, err := os.ReadFile(dsPath)
	if err != nil {
		return fmt.Errorf("bestiary-hf: read %q: %w", dsPath, err)
	}
	dsStamped, err := stampHFIngestedAt(dsRaw, snapshot)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(dsPath, dsStamped); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr,
		"bestiary-hf: %d candidates; %d verified (%d linked, %d unlinked), %d absent, %d unchanged; stamped huggingface ingested_at=%s\n",
		len(candidates), verified, len(out.Nomina), len(unlinked), missing, unchanged, snapshot)
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-hf: %v\n", err)
		os.Exit(1)
	}
}
