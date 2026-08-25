package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// This file is the home of the models.dev metadata<->entity JOIN: the adapter that
// maps a provider-agnostic EntityMetadata row (keyed by its lab-scoped MetadataID,
// e.g. "zhipuai/glm-4.6") onto the model entity it describes, plus the pure
// merge/attach helpers the CLI overlay and codegen consume.
//
// Resolution order for one metadata id (curated > mechanical, mirroring the
// parse/ curated-override and ollama_aliases precedents):
//
//  1. Curated alias: a row in parse/data/modelsdev_aliases.json maps the (lowercased)
//     MetadataID to an explicit entity tuple. When present it is tried FIRST.
//  2. Mechanical: strip the "<lab>/" prefix and decompose the remainder through the
//     PRODUCTION parse pipeline (ParseFamilyDetailed) — the same decomposition the
//     registry and ollama paths use — lifting a parameter-size token (ParseParamSize)
//     exactly as the registry builds entity keys. The resulting EntityRef key is
//     matched against the provided entity set.
//
// A metadata row is never silently dropped. On a miss the two-tier policy applies:
//
//   - family PRESENT among the provided entities but no tuple match -> the id is a
//     *disagreement*, collected into the returned unlinked list (the codegen slice
//     emits it as a sorted modelsdev_unlinked.json report; the parse_failures /
//     ollama_unlinked precedent) so a curator can add an alias.
//   - family ABSENT entirely -> the model is *genuinely* not in the catalog, so a
//     metadata-only STANDALONE entity is synthesized (Ref from the decomposition,
//     empty Instances, Sources = [models.dev], Metadata attached).
//
// The join is pure: JoinEntityMetadata / AttachEntityMetadata never touch the store
// and never mutate their inputs — every returned entity is a defensive deep copy and
// every attached *EntityMetadata is a fresh clone, so a returned Entity can never
// alias the caller's slice or the registry-owned metadata.

// --------------------------------------------------------------------------
// Curated alias table (parse/data/modelsdev_aliases.json) — go:embed + sync.Once,
// mirroring loadModifierClassTable / loadDataSourceTable graceful-degrade.
// --------------------------------------------------------------------------

// modelsdevAlias is one curated MetadataID -> entity-tuple override row. It mirrors
// the ollamaAlias shape used by the offline Ollama refresh tool.
type modelsdevAlias struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

// entityRef renders the alias tuple as an EntityRef, projecting the curated modifier
// list through EntityModifiers (identity-class only) so the key it produces is built
// exactly the way the registry builds entity keys.
func (a modelsdevAlias) entityRef() EntityRef {
	fam := Family(a.Family)
	return EntityRef{
		Family:    fam,
		Variant:   a.Variant,
		Version:   a.Version,
		ParamSize: a.ParamSize,
		Modifier:  EntityModifiers(a.Modifier, fam),
	}
}

// modelsdevAliasFile is the on-disk shape of parse/data/modelsdev_aliases.json.
type modelsdevAliasFile struct {
	Comment       string                    `json:"_comment,omitempty"`
	SchemaVersion int                       `json:"schema_version"`
	Aliases       map[string]modelsdevAlias `json:"aliases"`
}

var (
	modelsdevAliasOnce sync.Once
	modelsdevAliasMap  map[string]modelsdevAlias
)

// loadModelsdevAliases returns the curated MetadataID->tuple alias map, loaded from
// the embedded file exactly once (sync.Once). It is a graceful-degrade loader: a
// missing or malformed file yields an EMPTY (non-nil) map, so the mechanical join
// simply proceeds without curated overrides — it never panics and never returns nil
// (the loadLineageTable / loadDataSourceTableSafe precedent).
func loadModelsdevAliases() map[string]modelsdevAlias {
	modelsdevAliasOnce.Do(func() {
		modelsdevAliasMap = map[string]modelsdevAlias{}
		raw, err := parseDataFS.ReadFile("parse/data/modelsdev_aliases.json")
		if err != nil {
			return
		}
		if m, err := parseModelsdevAliases(raw); err == nil {
			modelsdevAliasMap = m
		}
	})
	return modelsdevAliasMap
}

// parseModelsdevAliases is the testable seam behind loadModelsdevAliases: it
// unmarshals the alias file and lowercases every key (MetadataID lookups are
// case-insensitive). It returns an actionable error on malformed JSON so a codegen
// or test caller can surface the problem; the runtime loader above swallows the
// error and degrades to empty.
func parseModelsdevAliases(raw []byte) (map[string]modelsdevAlias, error) {
	var file modelsdevAliasFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary metadata: parse modelsdev_aliases.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/modelsdev_aliases.json\n"+
				"  How to fix: validate the JSON syntax; expected"+
				" {\"schema_version\": N, \"aliases\": {\"<lab>/<model>\": {\"family\": ...}}}",
			err,
		)
	}
	out := make(map[string]modelsdevAlias, len(file.Aliases))
	for k, v := range file.Aliases {
		out[strings.ToLower(k)] = v
	}
	return out, nil
}

// metadataAliasRef resolves a curated alias EntityRef for the given MetadataID and
// whether one exists. Lookup is case-insensitive on the MetadataID.
func metadataAliasRef(id MetadataID) (EntityRef, bool) {
	a, ok := loadModelsdevAliases()[strings.ToLower(string(id))]
	if !ok {
		return EntityRef{}, false
	}
	return a.entityRef(), true
}

// --------------------------------------------------------------------------
// Mechanical decomposition (MetadataID -> EntityRef)
// --------------------------------------------------------------------------

// stripMetadataLab strips the leading "<lab>/" segment of a models.dev metadata id
// ("zhipuai/glm-4.6" -> "glm-4.6"). A models.dev metadata key is always lab-scoped;
// when no slash is present the id is returned unchanged.
func stripMetadataLab(id string) string {
	if _, rest, found := strings.Cut(id, "/"); found {
		return rest
	}
	return id
}

// metadataParamSize resolves the canonical parameter-size token for a lab-stripped
// metadata remainder. It applies the curated pin FIRST — the remainder-pin rule: a
// param_size_overrides.json pin keyed byte-equal to the stripped remainder wins, so
// the metadata join agrees with the entity key by construction (a pinned llama-4
// scout keys #17b-16e on both sides, not a mechanical #17b, and a suppress-pin yields
// no size). Otherwise it delegates to the shared ExtractParamSizeToken grammar
// authority (longest whole-window match over [-:/] only), so this site never
// re-implements a greedy scan and never splits on '.' (a dotted version "4.6" is
// never mistaken for a size).
func metadataParamSize(remainder string) string {
	if pinToken, pinned := paramSizePin(remainder); pinned {
		return pinToken // PRESENT pin wins; "" is a deliberate suppress.
	}
	if tok, ok := ExtractParamSizeToken(remainder); ok {
		return tok
	}
	return ""
}

// metadataEntityRef decomposes a MetadataID into the entity-identity EntityRef it
// maps to under the MECHANICAL join: it strips the "<lab>/" prefix and decomposes the
// remainder through the production parse pipeline (ParseFamilyDetailed with an empty
// raw-family and no provider — the decomposition is fully id-driven and
// provider-agnostic), then lifts a parameter-size token the same way the registry
// and ollama paths build entity keys. The identity-class modifier projection
// (EntityModifiers) makes the resulting key byte-identical to a matching catalog
// entity's key.
func metadataEntityRef(id MetadataID) EntityRef {
	remainder := stripMetadataLab(string(id))
	fam, variant, version, mods, _ := ParseFamilyDetailed(Family(""), ModelID(remainder), Provider(""))
	return EntityRef{
		Family:    fam,
		Variant:   variant,
		Version:   version,
		ParamSize: metadataParamSize(remainder),
		Modifier:  EntityModifiers(mods, fam),
	}
}

// metadataPresenceFamily returns the CANONICAL family the catalog enrichment
// pipeline would derive for this metadata row, used only for the join's
// family-presence gate. Unlike metadataEntityRef (which is id-only and
// provider/raw-family-agnostic, so it over-captures compound families), this feeds
// the row's own upstream models.json family into ParseFamilyDetailed together with
// the FULL lab-scoped id — exactly the (rawFamily, id) inputs enrichModelInfo uses
// for a serving model — so the presence gate agrees with the served entity's
// family. The entity KEY itself is deliberately NOT changed by this (that stays the
// mechanical decomposition + curated aliases); only the standalone-vs-unlinked
// decision consults it.
func metadataPresenceFamily(id MetadataID, rawFamily Family) Family {
	fam, _, _, _, _ := ParseFamilyDetailed(rawFamily, ModelID(id), Provider(""))
	return fam
}

// --------------------------------------------------------------------------
// Merge (most-recent-wins per MetadataID) — MergeModels mirror
// --------------------------------------------------------------------------

// MergeEntityMetadata merges baked (static) and cached entity-metadata lists,
// deduplicating by MetadataID. When both sources carry the same MetadataID the row
// with the more recent LastSynced timestamp wins; since LastSynced is an RFC3339
// string, lexicographic comparison correctly orders recency and a baked row (empty
// LastSynced) always loses to any synced row.
//
// The result has UNION semantics (the MergeModels precedent): a MetadataID present
// in only one source is always retained — in particular a BAKED-ONLY row (never
// re-synced) SURVIVES a sync of a disjoint set. Output is sorted ascending by
// MetadataID for deterministic ordering.
func MergeEntityMetadata(static, cached []EntityMetadata) []EntityMetadata {
	seen := make(map[MetadataID]EntityMetadata, len(static)+len(cached))

	for _, m := range static {
		seen[m.MetadataID] = m
	}

	for _, m := range cached {
		if existing, ok := seen[m.MetadataID]; ok {
			// RFC3339 timestamps sort lexicographically — the later timestamp wins.
			if m.LastSynced > existing.LastSynced {
				seen[m.MetadataID] = m
			}
		} else {
			seen[m.MetadataID] = m
		}
	}

	out := make([]EntityMetadata, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MetadataID < out[j].MetadataID
	})
	return out
}

// --------------------------------------------------------------------------
// Join + attach (pure)
// --------------------------------------------------------------------------

// JoinEntityMetadata runs the metadata<->entity join over ents with meta and returns
// three results:
//
//   - attached: every provided entity (deep-copied), carrying EVERY metadata row that
//     matched its identity on MetadataAll (sorted ascending by MetadataID) with
//     Metadata set to the derived primary (shortest MetadataID, ties lexicographic
//     ascending). Entities with no matching metadata carry their original Metadata
//     and MetadataAll unchanged.
//   - unlinked: the MetadataIDs whose decomposed family IS present among the provided
//     entities but whose full tuple matched no entity — disagreements a curator
//     resolves with an alias (the codegen slice emits them as a sorted report).
//   - standalone: newly synthesized metadata-only entities for metadata whose family
//     is absent entirely (empty Instances, Sources = [models.dev], Metadata attached).
//
// The join is PURE: the caller's ents slice and its elements are never mutated (every
// returned entity is a fresh deep copy), and every attached *EntityMetadata is a fresh
// clone of the source row (never aliasing the meta slice).
//
// Re-attach, never re-create: a metadata row whose identity key already matches a
// provided entity — including a standalone synthesized on an earlier pass and fed
// back in — is RE-ATTACHED onto that entity rather than duplicated, so repeated joins
// over the same metadata set are idempotent (no growing standalone set).
//
// Accumulation is CLEAR-THEN-ACCUMULATE: the first row to land on an entity in THIS
// call clears whatever MetadataAll that entity arrived with (tracked by the touched
// set) before appending, so re-joining an already-joined set REPLACES the record
// rather than doubling it. An entity no row lands on is never cleared, so a
// hand-attached or previously-joined record on an unmatched entity survives.
func JoinEntityMetadata(ents []Entity, meta []EntityMetadata) (attached []Entity, unlinked []MetadataID, standalone []Entity) {
	// Deep-copy every input entity up front so the join is pure and so re-attachment
	// mutates only the copies. Index the copies by their identity key, and record the
	// set of families present for the two-tier miss policy.
	attached = make([]Entity, len(ents))
	byKey := make(map[string]int, len(ents))
	// touched records which attached entities have received a row in THIS call, so the
	// first landing row clears any pre-existing MetadataAll exactly once (idempotence)
	// and an untouched entity is left byte-identical to its input.
	touched := make(map[int]struct{}, len(ents))
	// standaloneByKey indexes synthesized standalones by entity key so two metadata
	// rows sharing one absent-family key accumulate onto a single standalone entity.
	standaloneByKey := make(map[string]int)
	keySet := make(map[string]struct{}, len(ents))
	familyPresent := make(map[Family]struct{}, len(ents))
	for i := range ents {
		attached[i] = cloneEntity(ents[i])
		k := attached[i].Ref.String()
		byKey[k] = i
		keySet[k] = struct{}{}
		familyPresent[attached[i].Ref.Family] = struct{}{}
	}

	for i := range meta {
		m := meta[i]

		// Resolution order, curated > mechanical: a curated alias FULLY overrides the
		// mechanical decomposition. When an alias exists it is the SOLE identity, used
		// both for matching and for the two-tier miss policy — there is no fallback to
		// the mechanical ref. Falling back would let a row whose alias target is absent
		// attach to a DIFFERENT entity the mechanical decomposition happens to hit,
		// which is exactly the wrong-attach an alias exists to prevent; instead an
		// absent alias target flows through the same miss policy on the curated family.
		identity := metadataEntityRef(m.MetadataID)
		aliased := false
		if aliasRef, ok := metadataAliasRef(m.MetadataID); ok {
			identity = aliasRef
			aliased = true
		}

		// MERGE-only N->N.0 fold: a metadata row (or a curated alias) may decompose to a
		// bare-integer version key ("claude/opus@4") whose entity folded into its dotted
		// sibling ("claude/opus@4.0"); resolve the identity through the SAME shared fold so
		// the lab metadata still attaches to the merged entity instead of being reported as
		// an unlinked disagreement. A no-op for any key without a dotted sibling.
		if dotted, merged := NormalizeEntityVersion(identity, keySet); merged {
			identity = dotted
		}

		if idx, ok := byKey[identity.String()]; ok {
			if _, seen := touched[idx]; !seen {
				touched[idx] = struct{}{}
				attached[idx].MetadataAll = nil // clear-then-accumulate (see doc above)
			}
			attached[idx].MetadataAll = append(attached[idx].MetadataAll, *cloneEntityMetadata(&m))
			continue
		}

		// Miss. Two-tier: family known -> unlinked; family absent -> standalone.
		// The presence gate keys off the CANONICAL family the catalog enrichment
		// pipeline would derive for this row (metadataPresenceFamily), not the
		// mechanical id-only decomposition. The id-only path OVER-captures a compound
		// family (e.g. "gemini-omni-flash-preview" -> "gemini-omni") that is absent
		// from the catalog even though its short family ("gemini") is served — which
		// wrongly synthesized a standalone. Feeding the upstream raw family makes the
		// gate apples-to-apples with enrichModelInfo, so such a row routes to the
		// unlinked report instead. A curated alias supersedes this (its target family
		// is authoritative). RawFamily is carried on both the baked rows and the
		// store round-trip (entity_metadata.raw_family), so it is empty only for a value
		// that never had one — a legacy pre-column cache row until re-synced, or a
		// hand-constructed EntityMetadata — in which case the gate degrades to the
		// mechanical family.
		presenceFamily := identity.Family
		if !aliased && m.RawFamily != "" {
			presenceFamily = metadataPresenceFamily(m.MetadataID, m.RawFamily)
		}
		if _, known := familyPresent[presenceFamily]; known {
			unlinked = append(unlinked, m.MetadataID)
			continue
		}
		// Two metadata rows can decompose to the SAME absent-family key; they are one
		// entity with two rows, not two entities. Index the standalones by key so the
		// second row accumulates onto the first rather than synthesizing a duplicate —
		// the same "one entity, many rows" rule the matched path above follows.
		key := identity.String()
		if si, ok := standaloneByKey[key]; ok {
			standalone[si].MetadataAll = append(standalone[si].MetadataAll, *cloneEntityMetadata(&m))
			continue
		}
		standaloneByKey[key] = len(standalone)
		standalone = append(standalone, synthesizeStandaloneEntity(identity, m))
	}

	// Impose the MetadataAll contract on every entity a row landed on: sort ascending
	// by MetadataID with an EXPLICIT sort.Slice (never an incidental insertion order),
	// then derive the primary pointer from the sorted slice.
	for idx := range touched {
		sortMetadataAll(attached[idx].MetadataAll)
		attached[idx].Metadata = primaryEntityMetadata(attached[idx].MetadataAll)
	}
	for i := range standalone {
		sortMetadataAll(standalone[i].MetadataAll)
		standalone[i].Metadata = primaryEntityMetadata(standalone[i].MetadataAll)
	}
	return attached, unlinked, standalone
}

// sortMetadataAll imposes the MetadataAll ordering contract in place: ascending by
// MetadataID via an explicit sort.Slice. MetadataIDs are distinct within one entity
// (they key the upstream rows), so the order is total and the sort is deterministic
// regardless of the order rows arrived in.
func sortMetadataAll(all []EntityMetadata) {
	sort.Slice(all, func(i, j int) bool {
		return all[i].MetadataID < all[j].MetadataID
	})
}

// AttachEntityMetadata runs the same join over the provided entities and metadata set
// and returns the attached entities followed by any newly synthesized standalone
// entities. It is pure (no store access; inputs never mutated) and re-attaches
// existing standalones instead of duplicating them, so a second call with the same
// metadata set yields the same result with no growing standalone tail.
//
// The unlinked disagreements are intentionally not returned here — this is the CLI
// overlay entry point, where an unmatched-but-family-known metadata id is simply not
// surfaced. Callers that need the unlinked report (codegen) call JoinEntityMetadata.
func AttachEntityMetadata(ents []Entity, meta []EntityMetadata) []Entity {
	attached, _, standalone := JoinEntityMetadata(ents, meta)
	if len(standalone) == 0 {
		return attached
	}
	return append(attached, standalone...)
}

// synthesizeStandaloneEntity builds a metadata-only standalone entity for a metadata
// row whose family is absent from the catalog: identity from the decomposition (or
// curated alias), empty (non-nil) Instances, Sources attested to models.dev, and a
// fresh clone of the metadata attached as the standalone's sole MetadataAll row (its
// Metadata primary derives from that slice). The Ref and metadata are cloned so the
// standalone shares no storage with the join's inputs.
func synthesizeStandaloneEntity(ref EntityRef, m EntityMetadata) Entity {
	e := Entity{
		Ref:         cloneRef(ref),
		Instances:   []ProviderInstance{},
		Sources:     []DataSourceID{DataSourceModelsDev},
		MetadataAll: []EntityMetadata{*cloneEntityMetadata(&m)},
	}
	e.Metadata = primaryEntityMetadata(e.MetadataAll)
	return e
}

// --------------------------------------------------------------------------
// Baked-metadata accessor — declared here, populated by codegen
// --------------------------------------------------------------------------

// bakedEntityMetadata is the compiled-in models.dev entity-metadata catalog. It is
// declared here (empty) and POPULATED by the generated metadata file via an init()
// assignment, exactly mirroring how staticModels is owned by the generated
// models_static_gen.go. Until the codegen slice emits that file this stays nil, so
// every baked-metadata path degrades to "no metadata" without panicking.
var bakedEntityMetadata []EntityMetadata

// staticEntityMetadata returns the compiled-in baked models.dev metadata rows. It is
// the internal baked-metadata accessor consumed by the registry hook (loadEntityIndex)
// to attach baked metadata and synthesize metadata-only standalone entities. The generated
// metadata file populates bakedEntityMetadata; this stub returns whatever is baked in
// (nil until then), so the accessor is safe to call before codegen has run.
func staticEntityMetadata() []EntityMetadata {
	return bakedEntityMetadata
}
