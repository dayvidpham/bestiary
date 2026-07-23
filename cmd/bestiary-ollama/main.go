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
//     (registry.ollama.ai/v2) and the ollama.com/library/<model>/tags HTML page.
//     The registry's /v2/<model>/tags/list endpoint returns 404 (verified
//     2026-06), so the tag set is enumerated from the HTML page. Every request
//     goes through a polite-bot seam: a descriptive User-Agent and at least one
//     second between requests (a project hard constraint, GH#12).
//  2. JOIN each Ollama tag onto a models.dev catalog ID. This is the hard problem
//     of this dataset: Ollama IDs and models.dev IDs do not match 1:1. A curated
//     alias (ollama_aliases.json) is consulted FIRST and OVERRIDES the mechanical
//     decomposition (curated > mechanical, matching the parse/ curated-overrides
//     precedent); otherwise DetectQuantization strips the quant tag, the remainder
//     is decomposed through the production parse pipeline (ParseFamilyDetailed /
//     ParseParamSize / EntityModifiers) into an EntityRef key, and that key is
//     matched against bestiary.StaticModels(). For a bare size-only Ollama tag
//     (Ollama's default tags are instruction-tuned but omit the modifier) the join
//     retries with an "instruct" modifier when the bare key misses.
//  3. Community models that do not join are KEPT, never dropped: their base is
//     inferred (Ollama exposes no base_model marker) via decomposition + a curated
//     base table; a base-known finetune carries a base_ref, a base-unknown one
//     becomes a standalone entry AND is appended to a sorted ollama_unlinked.json.
//  4. The result is MERGED into parse/data/quant_vram.json: fetch-owned fields
//     (weights_bytes, the quant set, param_size from tags) refresh, while
//     curation-owned fields (architecture facts, context_window, base_ref,
//     provenance _comments) are preserved from the existing file. The ollama row
//     of parse/data/datasources.json gets its ingested_at stamped once per run
//     (a committed snapshot — codegen never stamps a wall-clock).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/internal/politebot"
)

// --------------------------------------------------------------------------
// Polite-bot constants (a user-stated hard constraint, GH#12)
// --------------------------------------------------------------------------

const (
	// userAgent identifies this project on every outbound request so the Ollama
	// operators can attribute (and contact about) the traffic. Dropping this is a
	// politeness violation. It is passed into politebot.New; the polite seam sets
	// it on every request. The ≥1s inter-request cadence is owned by politebot
	// (politebot.DefaultMinRequestInterval).
	userAgent = "bestiary-ollama/0.2.4 (+https://github.com/dayvidpham/bestiary; polite ingest bot)"

	// registryBase is the anonymous Docker-Distribution-v2 registry host.
	registryBase = "https://registry.ollama.ai"
	// libraryBase is the HTML site used for tag enumeration (the registry's
	// /v2/.../tags/list returns 404).
	libraryBase = "https://ollama.com"

	// manifestAccept is the media type requested for a v2 manifest.
	manifestAccept = "application/vnd.docker.distribution.manifest.v2+json"
	// modelLayerMediaType marks the GGUF weights layer whose size is the on-disk
	// weight footprint (the deterministic anchor for the VRAM weights term).
	modelLayerMediaType = "application/vnd.ollama.image.model"
)

// defaultAllowlist is the curated, deterministically-ordered set of Ollama
// library models the tool refreshes. It is the allowlist's home (a tool-local
// list so the refresh set is reviewable in code). Kept sorted; run() iterates it
// in order so the fetch sequence is stable.
//
// Join disposition of each head against the current models.dev catalog (the
// catalog is compiled in; see TestRealCatalog_AllowlistDisposition):
//   - llama3.1, llama3.2, llama3.3: bare size tags collide with a base or
//     non-instruct / community catalog row, so an alias pins each to the instruct
//     entity (see ollama_aliases.json). llama3.2's 3b and 1b tags both hit base
//     entities (llama-3.2-3b / meta/llama-3.2-1b) bare.
//   - qwen2.5: bare key misses; the instruct fallback reaches the catalog entity.
//   - mistral: bare key reaches the canonical open-weights entity.
//   - gemma2, phi3.5: models.dev carries no joinable catalog entity for these
//     sizes, so they are correctly KEPT as standalone community entries (and
//     listed in ollama_unlinked.json). They are retained in the allowlist so the
//     tool records their footprint rather than silently ignoring them.
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
// keeps glued, so they must not be split. TestNormalizeOllamaName pins both.
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

// withInstruct returns a copy of the decomposition with an "instruct" identity
// modifier added — the default-tag interpretation for a bare Ollama size tag.
func (d ollamaDecomposition) withInstruct() ollamaDecomposition {
	out := d
	out.Modifiers = append(append([]string(nil), d.Modifiers...), "instruct")
	return out
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

// paramSizeFromID resolves an ID's canonical parameter-size token (e.g. "70b",
// "8x22b", "235b-a22b"), returning its canonical form or "". It delegates to the
// shared ExtractParamSizeToken grammar authority (longest whole-window match over
// [-:/] only) so this site decomposes sizes identically to the library and never
// re-implements a greedy per-token scan; '.' is never a separator, so dotted
// versions ("3.3") are not mistaken for sizes.
func paramSizeFromID(id string) string {
	if tok, ok := bestiary.ExtractParamSizeToken(id); ok {
		return tok
	}
	return ""
}

// decomposeOllamaID turns a full Ollama ID (e.g. "llama3.3:70b-instruct-q4_K_M")
// into its normalized join decomposition: DetectQuantization strips the quant
// tag, the colon-name is normalized + the tag re-glued into a models.dev-style
// ID, ParseFamilyDetailed decomposes family/variant/version/modifiers, and
// ParseParamSize lifts the size token.
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

// matchCatalog returns the preferred models.dev catalog ID whose entity key
// equals key. Several catalog rows commonly share a key (the same model under
// many providers, plus community variants that decompose to the same identity);
// the representative is chosen by a deterministic PREFERENCE ORDER, not by
// lexicographic-smallest (which lands on uppercase community-merge junk —
// "Llama-3.3-70B-Anthrobomination" sorts before "llama-3.3-70b-instruct"):
//
//	rank 1: ID already keyed in the curated quant_vram.json (the refresh target)
//	rank 2: ID with no provider-namespace prefix (no '/')
//	rank 3: ID that is entirely lowercase (canonical models.dev form, not a
//	        CamelCase upstream/HF repo name)
//	rank 4: shorter ID (prefers the plain entity over "-maas"/"-fp8" suffixes)
//	rank 5: lexicographic (final total-order tiebreak)
//
// Canonical-provider preference was considered but is unreliable here: the Llama
// family's canonical provider is "local" (no catalog rows), and other families'
// canonical-provider rows are often the CamelCase HF-repo IDs rank 3 demotes.
func matchCatalog(key string, catalog []bestiary.ModelInfo, curated map[string]bool) (bestiary.ModelID, bool) {
	var best bestiary.ModelID
	found := false
	for i := range catalog {
		if catalogJoinKey(catalog[i]) != key {
			continue
		}
		id := catalog[i].ID
		if !found || preferCatalogID(id, best, curated) {
			best = id
			found = true
		}
	}
	return best, found
}

// preferCatalogID reports whether candidate a is a better representative than the
// incumbent b under the matchCatalog preference order.
func preferCatalogID(a, b bestiary.ModelID, curated map[string]bool) bool {
	ra, rb := catalogIDRank(a, curated), catalogIDRank(b, curated)
	for i := range ra {
		if ra[i] != rb[i] {
			return ra[i] < rb[i]
		}
	}
	// All rank components equal (incl. length): break the final tie lexicographically.
	return a < b
}

// catalogIDRank is the comparable rank vector for an ID (lower is better): not
// curated, has namespace slash, not all-lowercase, then length.
func catalogIDRank(id bestiary.ModelID, curated map[string]bool) [4]int {
	s := string(id)
	notCurated := 1
	if curated[strings.ToLower(s)] {
		notCurated = 0
	}
	hasSlash := 0
	if strings.Contains(s, "/") {
		hasSlash = 1
	}
	notLower := 0
	if s != strings.ToLower(s) {
		notLower = 1
	}
	return [4]int{notCurated, hasSlash, notLower, len(s)}
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
// Precedence (curated > mechanical):
//  1. a curated alias OVERRIDES the mechanical decomposition (an alias is needed
//     precisely where the mechanical key matches the WRONG catalog row, e.g. a
//     bare default-instruct tag colliding with a community-merge or non-instruct
//     row, so it cannot be a mere miss-rescue);
//  2. the mechanical decomposition's natural key;
//  3. for a bare size-only tag with no identity modifiers, a retry with the
//     "instruct" modifier (Ollama's default size tags are instruction-tuned);
//  4. otherwise KEEP as a community model and infer its base.
func joinOllama(
	ollamaIDStripped string,
	catalog []bestiary.ModelInfo,
	aliases map[string]ollamaAlias,
	bases map[string]string,
	curated map[string]bool,
) joinResult {
	decomp := decomposeOllamaID(ollamaIDStripped)
	res := joinResult{OllamaID: ollamaIDStripped, Decomp: decomp}

	// 1. Curated alias OVERRIDE.
	if alias, ok := lookupAlias(ollamaIDStripped, aliases); ok {
		ad := alias.decomposition(decomp.Quant, decomp.QuantRaw)
		if id, ok := matchCatalog(ad.joinKey(), catalog, curated); ok {
			res.Decomp = ad
			res.Joined = true
			res.ModelsDevID = id
			return res
		}
	}

	// 2. Mechanical natural key.
	if id, ok := matchCatalog(decomp.joinKey(), catalog, curated); ok {
		res.Joined = true
		res.ModelsDevID = id
		return res
	}

	// 3. Default-tag instruct fallback (bare size-only tag).
	if decomp.ParamSize != "" && len(bestiary.EntityModifiers(decomp.Modifiers, decomp.Family)) == 0 {
		instr := decomp.withInstruct()
		if id, ok := matchCatalog(instr.joinKey(), catalog, curated); ok {
			res.Decomp = instr
			res.Joined = true
			res.ModelsDevID = id
			return res
		}
	}

	// 4. Community model: KEPT, never dropped. Infer the base.
	if base := inferBase(ollamaIDStripped, decomp, catalog, bases, curated); base != "" {
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
	curated map[string]bool,
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
		bd.Modifiers = decomp.Modifiers
		if decomp.ParamSize != "" {
			bd.ParamSize = decomp.ParamSize
		}
		if id, ok := matchCatalog(bd.joinKey(), catalog, curated); ok {
			return string(id)
		}
	}
	return ""
}

// --------------------------------------------------------------------------
// Output assembly (deterministic, sorted, models.dev-keyed, merge-on-refresh)
// --------------------------------------------------------------------------

// fetchedTag is one (model, tag) the fetch step resolved. The fetch supplies ONLY
// the OllamaID and the GGUF weights footprint — the Ollama config blob carries no
// architecture facts (layers/kv_heads/head_dim) or context window, so those are
// CURATION-owned and merged in from the existing file, never sourced here.
type fetchedTag struct {
	OllamaID     string // full tag ID, incl. quant (e.g. "llama3.3:70b-instruct-q4_K_M")
	WeightsBytes int64
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

const quantFileComment = "Per-model per-quant weights and architecture facts for VRAM estimation. FIELD OWNERSHIP: the offline Ollama tool (cmd/bestiary-ollama) owns weights_bytes, the quant set, param_size, and source — it refreshes these from the Ollama registry on each run. Curation owns layers/kv_heads/head_dim (absent from the Ollama registry), context_window, base_ref, and the per-entry _comment — the tool PRESERVES these across a refresh (merge-on-refresh), never clobbering them. model_id is the models.dev catalog ID for joined models, or an 'ollama/<id>' namespace form for community models with no models.dev presence. VRAMBytes/VRAMContextTokens are computed and baked by codegen. Sorted by model_id (rows by quant) — deterministic."

// quantVRAMSchemaVersion is the quant_vram.json schema the tool writes; it must
// match a version the bestiary loader (knownQuantVRAMSchemaVersions) accepts.
const quantVRAMSchemaVersion = 1

// buildOutput runs the full join + group + merge over fetched tags. It is the
// pure core shared by production and tests: deterministic regardless of input
// order. fetch-owned fields refresh from tags; curation-owned fields are
// preserved from existing (the current quant_vram.json). curated is the set of
// existing model_id keys (lowercased), used as the strongest catalog-preference
// signal. Returns the merged document and the sorted unlinked-ID list.
func buildOutput(
	tags []fetchedTag,
	catalog []bestiary.ModelInfo,
	aliases map[string]ollamaAlias,
	bases map[string]string,
	curated map[string]bool,
	existing quantFileOut,
) (quantFileOut, []string) {
	// Index existing entries (and their per-quant arch facts) for merge lookups.
	existingByID := map[string]quantModelOut{}
	for _, m := range existing.Models {
		existingByID[strings.ToLower(m.ModelID)] = m
	}

	// Group fetched tags by quant-stripped identity so all quants of one model
	// collapse into a single entry with multiple rows.
	type group struct {
		strippedID string
		join       joinResult
		rows       []quantRowOut
	}
	groups := map[string]*group{}
	var groupOrder []string

	for _, ft := range tags {
		_, quantRaw, stripped := bestiary.DetectQuantization(bestiary.ModelID(ft.OllamaID))
		strippedID := string(stripped)
		g := groups[strippedID]
		if g == nil {
			g = &group{strippedID: strippedID, join: joinOllama(strippedID, catalog, aliases, bases, curated)}
			groups[strippedID] = g
			groupOrder = append(groupOrder, strippedID)
		}
		g.rows = append(g.rows, quantRowOut{
			Quant:        strings.ToLower(quantRaw),
			WeightsBytes: ft.WeightsBytes,
		})
	}

	// Start the output from a copy of every existing entry so curation that was
	// not re-fetched this run is never dropped.
	out := quantFileOut{Comment: quantFileComment, SchemaVersion: quantVRAMSchemaVersion}
	refreshed := map[string]bool{}
	for _, sid := range groupOrder {
		g := groups[sid]
		modelID := outputModelID(g.join, g.strippedID)
		prev, hasPrev := existingByID[strings.ToLower(modelID)]
		refreshed[strings.ToLower(modelID)] = true
		out.Models = append(out.Models, mergeEntry(modelID, g.join, g.rows, prev, hasPrev))
	}
	for _, m := range existing.Models {
		if !refreshed[strings.ToLower(m.ModelID)] {
			out.Models = append(out.Models, m)
		}
	}

	var unlinked []string
	for _, sid := range groupOrder {
		if groups[sid].join.Unlinked {
			unlinked = append(unlinked, outputModelID(groups[sid].join, sid))
		}
	}

	// EXPLICIT sort (not first-seen / map order) — the determinism invariant.
	sort.Slice(out.Models, func(i, j int) bool { return out.Models[i].ModelID < out.Models[j].ModelID })
	sort.Strings(unlinked)
	return out, unlinked
}

// mergeEntry assembles one output entry: fetch-owned fields from the fresh rows,
// curation-owned fields (arch facts per quant, context_window, base_ref, comment)
// preserved from the previous entry when present.
func mergeEntry(modelID string, j joinResult, freshRows []quantRowOut, prev quantModelOut, hasPrev bool) quantModelOut {
	prevArch := map[string]quantRowOut{}
	if hasPrev {
		for _, r := range prev.Rows {
			prevArch[strings.ToLower(r.Quant)] = r
		}
	}

	rows := make([]quantRowOut, 0, len(freshRows))
	for _, r := range freshRows {
		if pr, ok := prevArch[strings.ToLower(r.Quant)]; ok {
			// Curation owns the arch facts; refresh only the fetch-owned weights.
			r.Layers, r.KVHeads, r.HeadDim = pr.Layers, pr.KVHeads, pr.HeadDim
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, k int) bool { return rows[i].Quant < rows[k].Quant })

	paramSize := j.Decomp.ParamSize
	if paramSize == "" && hasPrev {
		paramSize = prev.ParamSize
	}
	baseRef := j.BaseRef
	contextWindow := 0
	comment := ""
	if hasPrev {
		if prev.BaseRef != "" {
			baseRef = prev.BaseRef // curation wins for an explicitly curated base
		}
		contextWindow = prev.ContextWindow
		comment = prev.Comment
	}

	return quantModelOut{
		Comment:       comment,
		ModelID:       modelID,
		ParamSize:     paramSize,
		Source:        string(bestiary.DataSourceOllama),
		BaseRef:       baseRef,
		ContextWindow: contextWindow,
		Rows:          rows,
	}
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

// writeFileAtomic writes data to path atomically: it writes to a temp file in the
// SAME directory (so os.Rename is a same-filesystem atomic swap) and renames it
// over path. A crash mid-write leaves either the old file or the new one, never a
// truncated file. The temp file is removed on any error before the rename.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bestiary-ollama-*.tmp")
	if err != nil {
		return fmt.Errorf(
			"bestiary-ollama: create temp file in %q failed: %w\n"+
				"  Where: writeFileAtomic\n"+
				"  How to fix: verify the directory exists and is writable", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("bestiary-ollama: write temp file %q failed: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("bestiary-ollama: close temp file %q failed: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf(
			"bestiary-ollama: rename %q -> %q failed: %w\n"+
				"  Where: writeFileAtomic\n"+
				"  How to fix: ensure the temp and target are on the same filesystem", tmpName, path, err)
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
func fetchLibraryTags(ctx context.Context, c *politebot.Client, lib string) ([]string, error) {
	body, err := c.Get(ctx, libraryBase+"/library/"+lib+"/tags", "text/html")
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
func fetchTag(ctx context.Context, c *politebot.Client, lib, tag string) (fetchedTag, error) {
	full := lib + ":" + tag
	manRaw, err := c.Get(ctx, registryBase+"/v2/library/"+lib+"/manifests/"+tag, manifestAccept)
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
	// The config blob's file_type is the authoritative quant; when the tag string
	// omits it, append it so DetectQuantization (which groups rows downstream)
	// sees the real quant. Architecture facts are NOT in the blob, so VRAM stays
	// weights-only until curation supplies them (merge-on-refresh preserves them).
	if man.Config.Digest != "" {
		blobRaw, err := c.Get(ctx, registryBase+"/v2/library/"+lib+"/blobs/"+man.Config.Digest, "")
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
	c := politebot.New(userAgent)

	aliases, err := loadAliasesFromDir(dataDir)
	if err != nil {
		return err
	}
	existing, curated, err := loadExistingQuantVRAM(filepath.Join(dataDir, "quant_vram.json"))
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
	out, unlinked := buildOutput(tags, catalog, aliases, communityBaseRefs, curated, existing)

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
// bare library name) to the inferred base reference written as base_ref. It is
// empty today (no shipped community finetune needs a curated override beyond the
// decomposition fallback) but is the documented seam for future curation.
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

// loadExistingQuantVRAM reads the current quant_vram.json for merge-on-refresh. A
// missing file degrades to an empty document (first-ever run). It returns the
// parsed document and the set of lowercased model_id keys (the catalog-preference
// signal).
func loadExistingQuantVRAM(path string) (quantFileOut, map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return quantFileOut{SchemaVersion: quantVRAMSchemaVersion}, map[string]bool{}, nil
		}
		return quantFileOut{}, nil, fmt.Errorf("bestiary-ollama: read %q: %w", path, err)
	}
	var f quantFileOut
	if err := json.Unmarshal(raw, &f); err != nil {
		return quantFileOut{}, nil, fmt.Errorf(
			"bestiary-ollama: parse %q failed: %w\n"+
				"  How to fix: validate the quant_vram.json syntax before refreshing", path, err)
	}
	curated := make(map[string]bool, len(f.Models))
	for _, m := range f.Models {
		curated[strings.ToLower(m.ModelID)] = true
	}
	return f, curated, nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "bestiary-ollama: %v\n", err)
		os.Exit(1)
	}
}
