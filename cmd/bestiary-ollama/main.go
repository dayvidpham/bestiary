// Command bestiary-ollama is the OFFLINE, network-gated Ollama refresh tool.
//
// A human runs this binary occasionally to refresh the curated per-quant
// weights/architecture data that codegen bakes into the bestiary static
// catalog. It is NOT part of `go test ./...` in any network sense: every live
// HTTP call lives behind run() (invoked only when the binary is executed), and
// the unit tests in main_test.go exercise the pure join/parse/output seams with
// canned fixtures and a fake clock/transport — never the network.
//
// What it does when a human runs it:
//  1. For each model in a curated, deterministically-ordered allowlist, fetch
//     the Ollama registry-v2 Docker-Distribution manifests + config blobs
//     (registry.ollama.ai/v2) and the ollama.com/library/<model>/tags HTML page
//     (the registry's /tags/list endpoint returns 404 — see the research report).
//     Every request goes through a POLITE-BOT seam: a descriptive User-Agent and
//     at least one second between requests (URD R9, user-stated hard constraint).
//  2. JOIN each Ollama tag onto a models.dev catalog ID (the epoch's named hard
//     problem): DetectQuantization strips the quant tag, the remainder is
//     decomposed through the production parse pipeline (ParseFamilyDetailed /
//     ParseParamSize / EntityModifiers) into an EntityRef key, and that key is
//     matched against bestiary.StaticModels(). ollama_aliases.json rescues
//     residuals the mechanical decomposition cannot match.
//  3. Community models that do not join are KEPT, never dropped: their base is
//     INFERRED (Ollama exposes no base_model marker) via decomposition + a curated
//     base table; a base-known finetune carries a base_ref, a base-unknown one
//     becomes a standalone entry AND is appended to a sorted ollama_unlinked.json
//     for visibility.
//  4. The joined + kept entries are written to parse/data/quant_vram.json (sorted,
//     replace-on-refresh, models.dev-keyed) and the ollama row of
//     parse/data/datasources.json gets its ingested_at stamped ONCE per run
//     (committed-snapshot design — codegen never stamps a wall-clock).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dayvidpham/bestiary"
)

// --------------------------------------------------------------------------
// Polite-bot constants (URD R9 — user-stated, verbatim)
// --------------------------------------------------------------------------

const (
	// userAgent identifies this project on every outbound request so the Ollama
	// operators can attribute (and contact about) the traffic. Dropping this is a
	// politeness violation; TestPoliteClient_SetsUserAgent pins it.
	userAgent = "bestiary-ollama/0.2.4 (+https://github.com/dayvidpham/bestiary; polite ingest bot)"

	// minRequestInterval is the minimum wall-clock gap the tool enforces between
	// two outbound requests. URD R9 mandates ">=1 second between requests".
	// TestPoliteClient_SleepsBetweenRequests pins it.
	minRequestInterval = 1 * time.Second

	// registryBase is the anonymous Docker-Distribution-v2 registry host.
	registryBase = "https://registry.ollama.ai"
	// libraryBase is the HTML site used for tag enumeration (the registry's
	// /v2/.../tags/list returns 404; see the research report).
	libraryBase = "https://ollama.com"

	// manifestAccept is the media type requested for a v2 manifest.
	manifestAccept = "application/vnd.docker.distribution.manifest.v2+json"
	// modelLayerMediaType marks the GGUF weights layer whose size is the on-disk
	// weight footprint (the deterministic anchor for the VRAM weights term).
	modelLayerMediaType = "application/vnd.ollama.image.model"

	// maxResponseBytes caps any single response body (defensive; manifests and
	// config blobs are tiny, tag pages are modest).
	maxResponseBytes = 8 << 20 // 8 MiB
)

// defaultAllowlist is the curated, deterministically-ordered set of Ollama
// library models the tool refreshes. It is the allowlist's home (the contract
// permits a tool-local list or a small config; this is the tool-local choice so
// the refresh set is reviewable in code). Kept sorted; run() iterates it in
// order so the fetch sequence is stable.
var defaultAllowlist = []string{
	"gemma2",
	"llama3.1",
	"llama3.2",
	"llama3.3",
	"mistral",
	"phi3.5",
	"qwen2.5",
}

// --------------------------------------------------------------------------
// Polite-bot request seam
// --------------------------------------------------------------------------

// doer is the minimal HTTP surface the polite client needs. *http.Client
// satisfies it; tests inject a canned transport so no socket is opened.
type doer interface {
	Do(*http.Request) (*http.Response, error)
}

// politeClient is the SINGLE outbound-request seam. Every fetch funnels through
// get(), which (a) enforces minRequestInterval since the previous request via an
// injectable clock+sleeper, and (b) sets the descriptive User-Agent. Routing all
// traffic through one seam is what makes the URD-R9 politeness guarantee
// structurally enforceable (and unit-testable without real time or sockets).
type politeClient struct {
	doer        doer
	ua          string
	minInterval time.Duration
	now         func() time.Time
	sleep       func(time.Duration)

	started bool
	last    time.Time
}

// newPoliteClient builds a production polite client backed by a real
// *http.Client, the real monotonic clock, and a real time.Sleep.
func newPoliteClient() *politeClient {
	return &politeClient{
		doer:        &http.Client{Timeout: 30 * time.Second},
		ua:          userAgent,
		minInterval: minRequestInterval,
		now:         time.Now,
		sleep:       time.Sleep,
	}
}

// get performs one polite GET. It sleeps to honor minInterval (skipped on the
// very first request), sets the User-Agent (and optional Accept) headers, and
// returns the response body (capped at maxResponseBytes) for a 2xx status.
func (c *politeClient) get(ctx context.Context, url, accept string) ([]byte, error) {
	if c.started {
		elapsed := c.now().Sub(c.last)
		if elapsed < c.minInterval {
			c.sleep(c.minInterval - elapsed)
		}
	}
	c.started = true
	c.last = c.now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary-ollama: build request for %q failed: %w\n"+
				"  What: net/http rejected the request URL\n"+
				"  Where: politeClient.get\n"+
				"  How to fix: verify the URL is well-formed", url, err)
	}
	req.Header.Set("User-Agent", c.ua)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := c.doer.Do(req)
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary-ollama: GET %q failed: %w\n"+
				"  What: the HTTP request did not complete\n"+
				"  Where: politeClient.get\n"+
				"  When: during the live Ollama refresh fetch\n"+
				"  How to fix: check network connectivity to %s", url, err, url)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary-ollama: read body of %q failed: %w\n"+
				"  Where: politeClient.get\n"+
				"  How to fix: retry; the response stream was truncated or reset", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"bestiary-ollama: GET %q returned HTTP %d\n"+
				"  What: a non-2xx status\n"+
				"  Where: politeClient.get\n"+
				"  Why: the model/tag may not exist or the registry rejected the request\n"+
				"  How to fix: verify the model name/tag and the Accept header", url, resp.StatusCode)
	}
	c.last = c.now()
	return body, nil
}

// --------------------------------------------------------------------------
// Registry manifest + config-blob shapes (Docker Distribution v2)
// --------------------------------------------------------------------------

// ollamaDescriptor is one content-addressed blob descriptor (config or layer).
type ollamaDescriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

// ollamaManifest is the registry-v2 manifest for one (model, tag).
type ollamaManifest struct {
	SchemaVersion int                `json:"schemaVersion"`
	Config        ollamaDescriptor   `json:"config"`
	Layers        []ollamaDescriptor `json:"layers"`
}

// ollamaConfigBlob is the structured metadata blob the manifest's config points
// at. model_type is the (rounded) param size; file_type is the quant token.
type ollamaConfigBlob struct {
	ModelFormat string `json:"model_format"`
	ModelFamily string `json:"model_family"`
	ModelType   string `json:"model_type"`
	FileType    string `json:"file_type"`
}

// parseManifest decodes a registry manifest, rejecting an empty/zero document.
func parseManifest(raw []byte) (ollamaManifest, error) {
	var m ollamaManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return ollamaManifest{}, fmt.Errorf(
			"bestiary-ollama: parse manifest failed: %w\n"+
				"  Where: parseManifest\n"+
				"  How to fix: confirm the response is a v2 manifest JSON document", err)
	}
	if len(m.Layers) == 0 {
		return ollamaManifest{}, fmt.Errorf(
			"bestiary-ollama: manifest has no layers\n" +
				"  What: a manifest with zero layers carries no weights blob\n" +
				"  Where: parseManifest\n" +
				"  How to fix: verify the model:tag exists in registry.ollama.ai")
	}
	return m, nil
}

// parseConfigBlob decodes a config blob.
func parseConfigBlob(raw []byte) (ollamaConfigBlob, error) {
	var c ollamaConfigBlob
	if err := json.Unmarshal(raw, &c); err != nil {
		return ollamaConfigBlob{}, fmt.Errorf(
			"bestiary-ollama: parse config blob failed: %w\n"+
				"  Where: parseConfigBlob\n"+
				"  How to fix: confirm the blob is the application/vnd.ollama.image.model config JSON", err)
	}
	return c, nil
}

// weightsBytes returns the size of the GGUF weights layer (the model-media-type
// layer) — the on-disk weight footprint. It returns 0 when no weights layer is
// present (the caller treats 0 as "skip this tag").
func (m ollamaManifest) weightsBytes() int64 {
	for _, l := range m.Layers {
		if l.MediaType == modelLayerMediaType {
			return l.Size
		}
	}
	return 0
}

// --------------------------------------------------------------------------
// The JOIN: Ollama ID -> models.dev catalog ID
// --------------------------------------------------------------------------

// reAlphaNumSplit matches a token that glues an alphabetic family prefix (>=2
// letters) to a numeric/dotted-numeric version, e.g. "llama3.3", "qwen2.5",
// "gemma2", "phi4". The >=2-letter guard deliberately EXCLUDES single-letter
// series tokens like "r1"/"k2"/"v3" (deepseek-r1, kimi-k2) which models.dev also
// keeps glued, so they must not be split.
var reAlphaNumSplit = regexp.MustCompile(`^([a-z]{2,})(\d+(?:\.\d+)?)$`)

// ollamaDecomposition is the normalized join key derived from an Ollama ID.
type ollamaDecomposition struct {
	Family    bestiary.Family
	Variant   string
	Version   string
	ParamSize string   // canonical token from the size segment (e.g. "70b")
	Modifiers []string // identity-class modifiers
	Quant     bestiary.Quantization
	QuantRaw  string
}

// joinKey renders the entity-identity key this decomposition maps to. It mirrors
// exactly how the registry builds entity keys (EntityModifiers projection +
// EntityRef.String), so a match against a catalog model's key is authoritative.
func (d ollamaDecomposition) joinKey() string {
	return bestiary.EntityRef{
		Family:    d.Family,
		Variant:   d.Variant,
		Version:   d.Version,
		ParamSize: d.ParamSize,
		Modifier:  bestiary.EntityModifiers(d.Modifiers, d.Family),
	}.String()
}

// normalizeOllamaName converts an Ollama colon-name (the part before ':', which
// glues family+version like "llama3.3") into a models.dev-style hyphenated form
// ("llama-3.3") so the production parse pipeline decomposes it the same way it
// decomposes catalog IDs. It splits per hyphen-token using reAlphaNumSplit; non
// family+version tokens (e.g. "nemo", "r1", "vision") are left untouched.
func normalizeOllamaName(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		low := strings.ToLower(p)
		if m := reAlphaNumSplit.FindStringSubmatch(low); m != nil {
			parts[i] = m[1] + "-" + m[2]
		} else {
			parts[i] = low
		}
	}
	return strings.Join(parts, "-")
}

// paramSizeFromID scans an ID's hyphen/colon/slash tokens for the first
// recognised parameter-size token (e.g. "70b", "8x22b"), returning its canonical
// form or "". It never splits on '.' so dotted versions ("3.3") are not mistaken
// for sizes.
func paramSizeFromID(id string) string {
	for _, tok := range strings.FieldsFunc(id, func(r rune) bool {
		return r == '-' || r == ':' || r == '/'
	}) {
		if ps, err := bestiary.ParseParamSize(tok); err == nil && ps != "" {
			return ps
		}
	}
	return ""
}

// decomposeOllamaID turns a full Ollama ID (e.g. "llama3.3:70b-instruct-q4_K_M")
// into its normalized join decomposition. The pipeline is exactly the contract's:
// DetectQuantization strips the quant tag, the colon-name is normalized + the tag
// re-glued into a models.dev-style ID, ParseFamilyDetailed decomposes
// family/variant/version/modifiers, and ParseParamSize lifts the size token.
func decomposeOllamaID(ollamaID string) ollamaDecomposition {
	quant, quantRaw, stripped := bestiary.DetectQuantization(bestiary.ModelID(ollamaID))
	s := string(stripped)

	name, tag, hadColon := strings.Cut(s, ":")
	normName := normalizeOllamaName(name)

	reconstructed := normName
	if hadColon && tag != "" {
		reconstructed = normName + "-" + tag
	}

	fam, variant, version, mods, _ := bestiary.ParseFamilyDetailed(
		bestiary.Family(""), bestiary.ModelID(reconstructed), bestiary.ProviderLocal)

	return ollamaDecomposition{
		Family:    fam,
		Variant:   variant,
		Version:   version,
		ParamSize: paramSizeFromID(s),
		Modifiers: mods,
		Quant:     quant,
		QuantRaw:  quantRaw,
	}
}

// ollamaAlias is one rescue tuple from ollama_aliases.json.
type ollamaAlias struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

// aliasFile is the on-disk shape of ollama_aliases.json.
type aliasFile struct {
	Comment       string                 `json:"_comment,omitempty"`
	SchemaVersion int                    `json:"schema_version"`
	Aliases       map[string]ollamaAlias `json:"aliases"`
}

// decomposition renders an alias tuple as a join decomposition (quant carried
// from the live tag, since the alias is identity-only).
func (a ollamaAlias) decomposition(quant bestiary.Quantization, quantRaw string) ollamaDecomposition {
	return ollamaDecomposition{
		Family:    bestiary.Family(a.Family),
		Variant:   a.Variant,
		Version:   a.Version,
		ParamSize: a.ParamSize,
		Modifiers: a.Modifier,
		Quant:     quant,
		QuantRaw:  quantRaw,
	}
}

// catalogJoinKey renders a catalog model's entity-identity key for join matching.
// It mirrors the registry's key construction; when ParamSize is not curated on
// the row it is recovered from the model ID so size-distinct Ollama tags still
// match the right size-distinct catalog entity.
func catalogJoinKey(m bestiary.ModelInfo) string {
	ps := m.ParamSize
	if ps == "" {
		ps = paramSizeFromID(string(m.ID))
	}
	return bestiary.EntityRef{
		Family:    m.Family,
		Variant:   m.Variant,
		Version:   m.Version,
		ParamSize: ps,
		Modifier:  bestiary.EntityModifiers(m.Modifier, m.Family),
	}.String()
}

// matchCatalog returns the models.dev catalog ID whose entity key equals key.
// When several catalog rows share the key (the same ID under several providers,
// or genuinely distinct IDs collapsing to one entity), the lexicographically
// smallest ID is chosen so the join is deterministic.
func matchCatalog(key string, catalog []bestiary.ModelInfo) (bestiary.ModelID, bool) {
	var best bestiary.ModelID
	found := false
	for i := range catalog {
		if catalogJoinKey(catalog[i]) == key {
			id := catalog[i].ID
			if !found || id < best {
				best = id
				found = true
			}
		}
	}
	return best, found
}

// joinResult is the outcome of joining one Ollama identity (a (name, size,
// modifier) group, quant-independent) onto the catalog.
type joinResult struct {
	OllamaID    string              // representative quant-stripped Ollama ID
	Decomp      ollamaDecomposition // identity decomposition (quant zeroed)
	Joined      bool                // matched a catalog entity
	ModelsDevID bestiary.ModelID    // set when Joined
	BaseRef     string              // set for a base-known community finetune
	Unlinked    bool                // base-unknown community model (-> ollama_unlinked.json)
}

// joinOllama joins a single quant-stripped Ollama identity onto the catalog.
// Order: mechanical decomposition -> catalog match; on miss, alias rescue ->
// catalog match; on miss, KEEP as a community model and INFER its base.
func joinOllama(
	ollamaIDStripped string,
	catalog []bestiary.ModelInfo,
	aliases map[string]ollamaAlias,
	bases map[string]string,
) joinResult {
	decomp := decomposeOllamaID(ollamaIDStripped)
	res := joinResult{OllamaID: ollamaIDStripped, Decomp: decomp}

	if id, ok := matchCatalog(decomp.joinKey(), catalog); ok {
		res.Joined = true
		res.ModelsDevID = id
		return res
	}

	// Alias rescue (residuals the mechanical decomposition cannot match).
	if alias, ok := lookupAlias(ollamaIDStripped, aliases); ok {
		ad := alias.decomposition(decomp.Quant, decomp.QuantRaw)
		if id, ok := matchCatalog(ad.joinKey(), catalog); ok {
			res.Decomp = ad
			res.Joined = true
			res.ModelsDevID = id
			return res
		}
	}

	// Community model: KEPT, never dropped. INFER the base.
	if base := inferBase(ollamaIDStripped, decomp, catalog, bases); base != "" {
		res.BaseRef = base
	} else {
		res.Unlinked = true
	}
	return res
}

// lookupAlias resolves an alias by the lowercased quant-stripped ID first, then
// the lowercased verbatim ID, so one alias entry rescues every quant variant.
func lookupAlias(id string, aliases map[string]ollamaAlias) (ollamaAlias, bool) {
	if aliases == nil {
		return ollamaAlias{}, false
	}
	_, _, stripped := bestiary.DetectQuantization(bestiary.ModelID(id))
	for _, k := range []string{strings.ToLower(string(stripped)), strings.ToLower(id)} {
		if a, ok := aliases[k]; ok {
			return a, true
		}
	}
	return ollamaAlias{}, false
}

// inferBase infers the base model of a community finetune (Ollama exposes no
// base_model marker). It consults the curated base table first (authoritative),
// then falls back to decomposition: drop the leading finetune-author token from
// the name and re-match the catalog at the same version/size/modifiers. Returns
// "" when the base cannot be determined.
func inferBase(
	ollamaIDStripped string,
	decomp ollamaDecomposition,
	catalog []bestiary.ModelInfo,
	bases map[string]string,
) string {
	name, _, _ := strings.Cut(strings.ToLower(ollamaIDStripped), ":")
	if bases != nil {
		if b, ok := bases[strings.ToLower(ollamaIDStripped)]; ok {
			return b
		}
		if b, ok := bases[name]; ok {
			return b
		}
	}

	// Decomposition fallback: a community finetune is conventionally
	// "<author>-<base-family>..."; dropping the leading author token may expose a
	// base that exists in the catalog at the same size/modifiers.
	parts := strings.Split(name, "-")
	if len(parts) >= 2 {
		trimmedName := strings.Join(parts[1:], "-")
		probe := trimmedName
		if decomp.ParamSize != "" {
			probe = trimmedName + "-" + decomp.ParamSize
		}
		bd := decomposeOllamaID(probe)
		// Preserve the finetune's identity modifiers (instruct, etc.) on the probe.
		bd.Modifiers = decomp.Modifiers
		if decomp.ParamSize != "" {
			bd.ParamSize = decomp.ParamSize
		}
		if id, ok := matchCatalog(bd.joinKey(), catalog); ok {
			return string(id)
		}
	}
	return ""
}

// --------------------------------------------------------------------------
// Output assembly (deterministic, sorted, models.dev-keyed)
// --------------------------------------------------------------------------

// fetchedTag is one (model, tag) the fetch step resolved to a quant + weight
// footprint. Multiple tags of the same identity (differing only by quant) group
// into one output entry with several rows.
type fetchedTag struct {
	OllamaID      string // full tag ID, incl. quant (e.g. "llama3.3:70b-instruct-q4_K_M")
	WeightsBytes  int64
	ContextWindow int
	Layers        int
	KVHeads       int
	HeadDim       int
}

// quantRowOut mirrors parse/data/quant_vram.json rows[].
type quantRowOut struct {
	Quant        string `json:"quant"`
	WeightsBytes int64  `json:"weights_bytes"`
	Layers       int    `json:"layers,omitempty"`
	KVHeads      int    `json:"kv_heads,omitempty"`
	HeadDim      int    `json:"head_dim,omitempty"`
}

// quantModelOut mirrors parse/data/quant_vram.json models[].
type quantModelOut struct {
	Comment       string        `json:"_comment,omitempty"`
	ModelID       string        `json:"model_id"`
	ParamSize     string        `json:"param_size,omitempty"`
	Source        string        `json:"source"`
	BaseRef       string        `json:"base_ref,omitempty"`
	ContextWindow int           `json:"context_window,omitempty"`
	Rows          []quantRowOut `json:"rows"`
}

// quantFileOut mirrors the top-level parse/data/quant_vram.json shape.
type quantFileOut struct {
	Comment       string          `json:"_comment,omitempty"`
	SchemaVersion int             `json:"schema_version"`
	Models        []quantModelOut `json:"models"`
}

const quantFileComment = "Generated by cmd/bestiary-ollama (offline refresh). model_id is the models.dev catalog ID for joined models, or an 'ollama/<id>' namespace form for community models with no models.dev presence. weights_bytes is the GGUF model-layer size from the Ollama registry; VRAMBytes/VRAMContextTokens are computed and baked by codegen. source is always 'ollama'; base_ref names an inferred finetune base when determinable. Sorted by model_id (rows sorted by quant) — replace-on-refresh, deterministic."

// quantVRAMSchemaVersion is the quant_vram.json schema the tool writes; it must
// match a version the bestiary loader (knownQuantVRAMSchemaVersions) accepts.
const quantVRAMSchemaVersion = 1

// buildOutput runs the full join + group + assembly over fetched tags. It is the
// pure core shared by production and tests: deterministic regardless of input
// order. Returns the quant_vram.json document and the sorted unlinked-ID list.
func buildOutput(
	tags []fetchedTag,
	catalog []bestiary.ModelInfo,
	aliases map[string]ollamaAlias,
	bases map[string]string,
) (quantFileOut, []string) {
	// Group tags by their quant-stripped identity key so all quants of one model
	// collapse into a single entry with multiple rows.
	type group struct {
		strippedID string
		join       joinResult
		rows       []quantRowOut
		ctx        int
	}
	groups := map[string]*group{}

	for _, ft := range tags {
		_, quantRaw, stripped := bestiary.DetectQuantization(bestiary.ModelID(ft.OllamaID))
		strippedID := string(stripped)
		g := groups[strippedID]
		if g == nil {
			g = &group{strippedID: strippedID, join: joinOllama(strippedID, catalog, aliases, bases)}
			groups[strippedID] = g
		}
		if ft.ContextWindow > g.ctx {
			g.ctx = ft.ContextWindow
		}
		g.rows = append(g.rows, quantRowOut{
			Quant:        strings.ToLower(quantRaw),
			WeightsBytes: ft.WeightsBytes,
			Layers:       ft.Layers,
			KVHeads:      ft.KVHeads,
			HeadDim:      ft.HeadDim,
		})
	}

	out := quantFileOut{
		Comment:       quantFileComment,
		SchemaVersion: quantVRAMSchemaVersion,
	}
	var unlinked []string

	for _, g := range groups {
		modelID := outputModelID(g.join, g.strippedID)

		rows := append([]quantRowOut(nil), g.rows...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].Quant < rows[j].Quant })

		out.Models = append(out.Models, quantModelOut{
			ModelID:       modelID,
			ParamSize:     g.join.Decomp.ParamSize,
			Source:        string(bestiary.DataSourceOllama),
			BaseRef:       g.join.BaseRef,
			ContextWindow: g.ctx,
			Rows:          rows,
		})

		if g.join.Unlinked {
			unlinked = append(unlinked, modelID)
		}
	}

	// EXPLICIT sort (not first-seen / map order) — the determinism invariant.
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].ModelID < out.Models[j].ModelID })
	sort.Strings(unlinked)

	return out, unlinked
}

// outputModelID is the models.dev catalog ID for a joined identity, else an
// 'ollama/<stripped-id>' namespace form that preserves the community model's
// size-distinct identity and never collides with a models.dev catalog key.
func outputModelID(j joinResult, strippedID string) string {
	if j.Joined {
		return string(j.ModelsDevID)
	}
	return "ollama/" + strings.ToLower(strippedID)
}

// unlinkedFileOut mirrors the parse/data/ollama_unlinked.json shape.
type unlinkedFileOut struct {
	Comment       string   `json:"_comment,omitempty"`
	SchemaVersion int      `json:"schema_version"`
	Unlinked      []string `json:"unlinked"`
}

const unlinkedFileComment = "Community Ollama models KEPT but with no determinable base (visibility list, never a drop path). Written sorted by the offline tool; each entry is also a standalone quant_vram.json entry."

// --------------------------------------------------------------------------
// datasources.json single stamp (committed-snapshot ingested_at)
// --------------------------------------------------------------------------

// dsSourceJSON / dsIngestedJSON / dsFileJSON mirror parse/data/datasources.json.
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

// stampOllamaIngestedAt sets the ingested_at of the single 'ollama' ingest row
// to snapshot, exactly once, preserving every other field and the file shape. It
// is the committed-snapshot write: codegen never stamps a wall-clock, so this
// pinned value is what keeps generated output byte-deterministic.
func stampOllamaIngestedAt(raw []byte, snapshot string) ([]byte, error) {
	var f dsFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf(
			"bestiary-ollama: parse datasources.json failed: %w\n"+
				"  Where: stampOllamaIngestedAt\n"+
				"  How to fix: validate parse/data/datasources.json syntax", err)
	}
	stamped := false
	for i := range f.Ingested {
		if f.Ingested[i].SourceID == string(bestiary.DataSourceOllama) {
			f.Ingested[i].IngestedAt = snapshot
			stamped = true
		}
	}
	if !stamped {
		return nil, fmt.Errorf(
			"bestiary-ollama: no 'ollama' ingest row in datasources.json\n"+
				"  What: stamping found no source_id==%q row to update\n"+
				"  Where: stampOllamaIngestedAt\n"+
				"  How to fix: add an 'ollama' entry to the 'ingested' array first",
			string(bestiary.DataSourceOllama))
	}
	return marshalJSON(f)
}

// --------------------------------------------------------------------------
// JSON write helpers (2-space indent, trailing newline — matches committed files)
// --------------------------------------------------------------------------

func marshalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("bestiary-ollama: marshal JSON failed: %w", err)
	}
	return append(b, '\n'), nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf(
			"bestiary-ollama: write %q failed: %w\n"+
				"  Where: writeFileAtomic\n"+
				"  How to fix: verify the directory exists and is writable", path, err)
	}
	return nil
}

// --------------------------------------------------------------------------
// Live fetch (human-run only; never exercised by go test)
// --------------------------------------------------------------------------

// reLibraryTag extracts "<lib>:<tag>" references from an ollama.com/library tags
// HTML page (the registry has no /tags/list endpoint). The tag href pattern is
// /library/<lib>:<tag>.
var reLibraryTag = regexp.MustCompile(`/library/([a-zA-Z0-9._-]+):([a-zA-Z0-9._-]+)`)

// fetchLibraryTags scrapes the tag list for one library model.
func fetchLibraryTags(ctx context.Context, c *politeClient, lib string) ([]string, error) {
	body, err := c.get(ctx, libraryBase+"/library/"+lib+"/tags", "text/html")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var tags []string
	for _, m := range reLibraryTag.FindAllStringSubmatch(string(body), -1) {
		if m[1] != lib {
			continue
		}
		full := lib + ":" + m[2]
		if _, dup := seen[full]; dup {
			continue
		}
		seen[full] = struct{}{}
		tags = append(tags, full)
	}
	sort.Strings(tags)
	return tags, nil
}

// fetchTag resolves one "<lib>:<tag>" to its weights footprint via the registry
// manifest + config blob.
func fetchTag(ctx context.Context, c *politeClient, lib, tag string) (fetchedTag, error) {
	full := lib + ":" + tag
	manRaw, err := c.get(ctx, registryBase+"/v2/library/"+lib+"/manifests/"+tag, manifestAccept)
	if err != nil {
		return fetchedTag{}, err
	}
	man, err := parseManifest(manRaw)
	if err != nil {
		return fetchedTag{}, err
	}
	weights := man.weightsBytes()
	if weights == 0 {
		return fetchedTag{}, fmt.Errorf(
			"bestiary-ollama: no weights layer for %q\n"+
				"  Where: fetchTag\n"+
				"  How to fix: confirm the manifest carries a %q layer", full, modelLayerMediaType)
	}
	// Fetch + parse the config blob for its authoritative file_type (quant) and
	// model_type (param size). Arch facts (layers/heads) are NOT in the blob, so
	// VRAM stays weights-only (partial) until curated. When the blob's file_type
	// names a quant the tag string omits, append it so DetectQuantization (which
	// groups rows downstream) sees the authoritative quant.
	if man.Config.Digest != "" {
		blobRaw, err := c.get(ctx, registryBase+"/v2/library/"+lib+"/blobs/"+man.Config.Digest, "")
		if err != nil {
			return fetchedTag{}, err
		}
		cfg, err := parseConfigBlob(blobRaw)
		if err != nil {
			return fetchedTag{}, err
		}
		if q, _, _ := bestiary.DetectQuantization(bestiary.ModelID(full)); q == bestiary.QuantizationNone && cfg.FileType != "" {
			full = full + "-" + strings.ToLower(cfg.FileType)
		}
	}
	return fetchedTag{OllamaID: full, WeightsBytes: weights}, nil
}

// --------------------------------------------------------------------------
// run / main
// --------------------------------------------------------------------------

func run(args []string) error {
	dataDir := "parse/data"
	snapshot := time.Now().UTC().Format(time.RFC3339)
	// Minimal flag handling (stdlib-only; this is a maintainer tool).
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--data-dir":
			if i+1 >= len(args) {
				return fmt.Errorf("bestiary-ollama: --data-dir requires a value")
			}
			dataDir = args[i+1]
			i++
		case "--snapshot":
			if i+1 >= len(args) {
				return fmt.Errorf("bestiary-ollama: --snapshot requires an RFC3339 value")
			}
			snapshot = args[i+1]
			i++
		default:
			return fmt.Errorf(
				"bestiary-ollama: unknown argument %q\n"+
					"  Usage: bestiary-ollama [--data-dir parse/data] [--snapshot RFC3339]", args[i])
		}
	}

	ctx := context.Background()
	c := newPoliteClient()

	aliases, err := loadAliasesFromDir(dataDir)
	if err != nil {
		return err
	}

	var tags []fetchedTag
	for _, lib := range defaultAllowlist {
		tagNames, err := fetchLibraryTags(ctx, c, lib)
		if err != nil {
			return fmt.Errorf("bestiary-ollama: enumerate tags for %q: %w", lib, err)
		}
		for _, full := range tagNames {
			_, tag, _ := strings.Cut(full, ":")
			ft, err := fetchTag(ctx, c, lib, tag)
			if err != nil {
				// A single bad tag must not abort the whole refresh; report and skip.
				fmt.Fprintf(os.Stderr, "bestiary-ollama: skip %q: %v\n", full, err)
				continue
			}
			tags = append(tags, ft)
		}
	}

	catalog := bestiary.StaticModels()
	out, unlinked := buildOutput(tags, catalog, aliases, communityBaseRefs)

	quantBytes, err := marshalJSON(out)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dataDir, "quant_vram.json"), quantBytes); err != nil {
		return err
	}

	unlinkedBytes, err := marshalJSON(unlinkedFileOut{
		Comment:       unlinkedFileComment,
		SchemaVersion: 1,
		Unlinked:      unlinked,
	})
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dataDir, "ollama_unlinked.json"), unlinkedBytes); err != nil {
		return err
	}

	dsPath := filepath.Join(dataDir, "datasources.json")
	dsRaw, err := os.ReadFile(dsPath)
	if err != nil {
		return fmt.Errorf("bestiary-ollama: read %q: %w", dsPath, err)
	}
	dsStamped, err := stampOllamaIngestedAt(dsRaw, snapshot)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(dsPath, dsStamped); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr,
		"bestiary-ollama: refreshed %d model entries (%d unlinked); stamped ollama ingested_at=%s\n",
		len(out.Models), len(unlinked), snapshot)
	return nil
}

// communityBaseRefs is the curated base-inference table for community finetunes
// (Ollama publishes no base_model marker). It maps a lowercased Ollama ID (or
// bare library name) to the inferred base reference written as base_ref.
var communityBaseRefs = map[string]string{}

// loadAliasesFromDir reads ollama_aliases.json from dir; a missing file degrades
// gracefully to an empty (no-rescue) table.
func loadAliasesFromDir(dir string) (map[string]ollamaAlias, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "ollama_aliases.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ollamaAlias{}, nil
		}
		return nil, fmt.Errorf("bestiary-ollama: read ollama_aliases.json: %w", err)
	}
	return parseAliases(raw)
}

// parseAliases decodes ollama_aliases.json bytes into a lowercased-key table.
func parseAliases(raw []byte) (map[string]ollamaAlias, error) {
	var f aliasFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf(
			"bestiary-ollama: parse ollama_aliases.json failed: %w\n"+
				"  Where: parseAliases\n"+
				"  How to fix: validate the alias-file JSON syntax", err)
	}
	out := make(map[string]ollamaAlias, len(f.Aliases))
	for k, v := range f.Aliases {
		out[strings.ToLower(k)] = v
	}
	return out, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-ollama: %v\n", err)
		os.Exit(1)
	}
}
