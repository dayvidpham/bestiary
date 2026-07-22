package bestiary

import (
	"sort"
	"sync"
)

// staticModels is declared and populated in the generated models_static_gen.go.
// It is referenced here by the registry query functions below.

// entityIndex is the memoized grouping of the static registry into model
// entities, keyed by EntityRef.String(). It is built exactly once (sync.Once)
// from staticModels and then served read-only; callers receive defensive copies
// via Entities()/EntityByTuple(), so the cached values here are never mutated.
//
// entityKeys preserves the first-seen key order so Entities() returns a
// deterministic slice (staticModels is itself sorted by (Provider, ID) at
// codegen, making first-seen order stable across runs).
//
// entitySourceRel is the entity↔source join relation built alongside the index
// under the same sync.Once; it is grouped with the index state it is born with.
var (
	entityIndexOnce sync.Once
	entityIndex     map[string]Entity
	entityKeys      []string
	entitySourceRel *entitySourceRelation
)

// entitySourceRelation is the in-memory BCNF join relation between entities and the
// data sources that attest them, built once alongside entityIndex (same sync.Once)
// from the static registry's per-row Source carrier.
//
//   - rows is the full set of EntitySource join rows, sorted by (EntityKey, then
//     SourceID) via an EXPLICIT sort.Slice. This pinned order is what makes the
//     generated EntitySource emission and any relation iteration byte-deterministic;
//     it deliberately does NOT reuse the providers/hosts first-seen aggregate order,
//     which has no sort of its own.
//   - byEntity is the sorted, de-duplicated DataSourceID projection per entity key
//     (the read view materialized into Entity.Sources and returned by EntitySources).
type entitySourceRelation struct {
	rows     []EntitySource
	byEntity map[string][]DataSourceID
}

// loadEntitySourceRelation returns the memoized entity↔source relation, building the
// entity index (and the relation alongside it) on first use.
func loadEntitySourceRelation() *entitySourceRelation {
	entityIndexOnce.Do(loadEntityIndex)
	return entitySourceRel
}

// entityAgg accumulates the per-entity aggregate state while scanning the
// registry. Price min/max are tracked as plain float64 + a found flag so the
// stored range pointers never alias a ModelInfo's cost pointer (T9 nil-cost
// rule: ranges cover NON-nil costs only; all-nil collapses to {nil, nil}).
type entityAgg struct {
	ref       EntityRef
	instances []ProviderInstance

	providers []Provider
	provSeen  map[Provider]struct{}
	hosts     []Host
	hostSeen  map[Host]struct{}

	// regions accumulates the de-duplicated per-instance Region values in first-seen
	// order; the SORTED, deterministic Entity.Regions output is produced by an explicit
	// sort in loadEntityIndex (the aggregate is sorted by ascending Region value, NOT
	// first-seen — mirroring the Sources determinism discipline, not the Providers/Hosts
	// first-seen order).
	regions    []Region
	regionSeen map[Region]struct{}

	lineage []LineageEdge
	linSeen map[string]struct{}

	// sources accumulates the de-duplicated DataSourceIDs that attest this entity,
	// in first-seen order. It is the raw projection; the SORTED, deterministic
	// output is produced by an explicit sort.Slice in loadEntityIndex (NOT first-seen
	// order — the providers/hosts aggregate is first-seen and must NOT be mirrored
	// here; see attestation rule below).
	sources []DataSourceID
	srcSeen map[DataSourceID]struct{}

	caps CapabilityUnion

	ctxSet         bool
	ctxMin, ctxMax int
	moSet          bool
	moMin, moMax   int

	piFound      bool
	piMin, piMax float64
	poFound      bool
	poMin, poMax float64
}

// loadEntityIndex builds entityIndex/entityKeys from staticModels. It runs under
// entityIndexOnce, so it executes at most once per process.
func loadEntityIndex() {
	aggs := make(map[string]*entityAgg)
	var order []string

	for i := range staticModels {
		m := staticModels[i]

		// CRITICAL: entity identity uses the IDENTITY-class projection of the
		// raw modifiers (EntityModifiers), never the raw modifier list. Stuffing
		// an attribute-class token into the key would split or merge entities
		// incorrectly because EntityRef.String() trusts Modifier to be
		// identity-only.
		ref := EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: m.ParamSize,
			Modifier:  EntityModifiers(m.Modifier, m.Family),
		}
		key := ref.String()

		a := aggs[key]
		if a == nil {
			a = &entityAgg{
				ref:        ref,
				provSeen:   make(map[Provider]struct{}),
				hostSeen:   make(map[Host]struct{}),
				regionSeen: make(map[Region]struct{}),
				linSeen:    make(map[string]struct{}),
				srcSeen:    make(map[DataSourceID]struct{}),
			}
			aggs[key] = a
			order = append(order, key)
		}

		a.instances = append(a.instances, ProviderInstance{
			ID:                m.ID,
			Provider:          m.Provider,
			Host:              m.Host,
			Region:            m.Region,
			RegionRaw:         m.RegionRaw,
			CostInputPerMTok:  m.CostInputPerMTok,
			CostOutputPerMTok: m.CostOutputPerMTok,
			ContextWindow:     m.ContextWindow,
			MaxOutput:         m.MaxOutput,
			// QuantVRAM is the per-row quant/VRAM carrier and Source is the per-row
			// ingest carrier; both are copied onto the instance so a curated row's
			// quantization footprint and originating source travel with it. QuantVRAM
			// is deep-copied (cloneQuantVRAM) because the cached instance must never
			// alias a staticModels row's backing slice; Source is a value type.
			QuantVRAM: cloneQuantVRAM(m.QuantVRAM),
			Source:    m.Source,
		})

		if _, dup := a.provSeen[m.Provider]; !dup {
			a.provSeen[m.Provider] = struct{}{}
			a.providers = append(a.providers, m.Provider)
		}
		if _, dup := a.hostSeen[m.Host]; !dup {
			a.hostSeen[m.Host] = struct{}{}
			a.hosts = append(a.hosts, m.Host)
		}
		if _, dup := a.regionSeen[m.Region]; !dup {
			a.regionSeen[m.Region] = struct{}{}
			a.regions = append(a.regions, m.Region)
		}

		// Attestation rule (BCNF entity↔source join). Every static row originates
		// from the models.dev pipeline, so each row attests DataSourceModelsDev —
		// including rows whose Source carrier is empty (DataSourceNone), which means
		// "the models.dev origin is implicit". A row whose Source names a further,
		// distinct source (e.g. an ollama-enriched row carries Source=ollama) is a
		// models.dev row ENRICHED with that source's data, so it DUAL-attests: it
		// contributes BOTH DataSourceModelsDev and that source. Net effect:
		//   - a pure models.dev row (Source=="")      → {models.dev}
		//   - an ollama-enriched row (Source=="ollama") → {models.dev, ollama}
		// so a multi-source entity (e.g. the curated llama-3.3-70b) carries
		// [models.dev, ollama]. The per-source set is de-duplicated here in
		// first-seen order; the deterministic output order is imposed by an explicit
		// sort.Slice when the projection and relation are materialized below.
		attest := func(src DataSourceID) {
			if _, dup := a.srcSeen[src]; dup {
				return
			}
			a.srcSeen[src] = struct{}{}
			a.sources = append(a.sources, src)
		}
		attest(DataSourceModelsDev)
		if m.Source != DataSourceNone && m.Source != DataSourceModelsDev {
			attest(m.Source)
		}

		for _, e := range m.Lineage {
			ek := e.Parent.String() + "\x00" + e.Kind.String()
			if _, dup := a.linSeen[ek]; !dup {
				a.linSeen[ek] = struct{}{}
				a.lineage = append(a.lineage, e)
			}
		}

		// Capability union: OR each per-instance capability.
		a.caps.Reasoning = a.caps.Reasoning || m.Reasoning
		a.caps.ToolCall = a.caps.ToolCall || m.ToolCall
		a.caps.Attachment = a.caps.Attachment || m.Attachment
		a.caps.Temperature = a.caps.Temperature || m.Temperature
		a.caps.StructuredOutput = a.caps.StructuredOutput || m.StructuredOutput
		a.caps.Interleaved = a.caps.Interleaved || m.Interleaved.Supported
		a.caps.OpenWeights = a.caps.OpenWeights || m.OpenWeights

		// Integer ranges cover every instance (zero is a legitimate value, not a
		// "missing" sentinel — unlike nil costs).
		if !a.ctxSet {
			a.ctxMin, a.ctxMax, a.ctxSet = m.ContextWindow, m.ContextWindow, true
		} else {
			if m.ContextWindow < a.ctxMin {
				a.ctxMin = m.ContextWindow
			}
			if m.ContextWindow > a.ctxMax {
				a.ctxMax = m.ContextWindow
			}
		}
		if !a.moSet {
			a.moMin, a.moMax, a.moSet = m.MaxOutput, m.MaxOutput, true
		} else {
			if m.MaxOutput < a.moMin {
				a.moMin = m.MaxOutput
			}
			if m.MaxOutput > a.moMax {
				a.moMax = m.MaxOutput
			}
		}

		// Price ranges cover NON-nil costs only (T9). A copy of the dereferenced
		// value is tracked so the materialized pointers never alias the registry.
		if m.CostInputPerMTok != nil {
			v := *m.CostInputPerMTok
			if !a.piFound {
				a.piMin, a.piMax, a.piFound = v, v, true
			} else {
				if v < a.piMin {
					a.piMin = v
				}
				if v > a.piMax {
					a.piMax = v
				}
			}
		}
		if m.CostOutputPerMTok != nil {
			v := *m.CostOutputPerMTok
			if !a.poFound {
				a.poMin, a.poMax, a.poFound = v, v, true
			} else {
				if v < a.poMin {
					a.poMin = v
				}
				if v > a.poMax {
					a.poMax = v
				}
			}
		}
	}

	entityIndex = make(map[string]Entity, len(order))
	entityKeys = order

	// Collect the first-seen attestation list per entity, then materialize the
	// sorted projection + flat join relation in one place (buildEntitySourceRelation).
	// Routing both Entity.Sources and EntitySources through that single materializer
	// keeps the public projection and the relation rows in lockstep and concentrates
	// the determinism-imposing sort at one testable site.
	firstSeen := make(map[string][]DataSourceID, len(order))
	for _, key := range order {
		firstSeen[key] = aggs[key].sources
	}
	rel := buildEntitySourceRelation(order, firstSeen)

	for _, key := range order {
		a := aggs[key]

		ent := Entity{
			Ref:            a.ref,
			Instances:      a.instances,
			Lineage:        a.lineage,
			Providers:      a.providers,
			Hosts:          a.hosts,
			Regions:        sortedRegions(a.regions),
			ContextRange:   [2]int{a.ctxMin, a.ctxMax},
			MaxOutputRange: [2]int{a.moMin, a.moMax},
			Capabilities:   a.caps,
			Sources:        rel.byEntity[key],
		}
		if a.piFound {
			lo, hi := a.piMin, a.piMax
			ent.PriceInputRange = [2]*float64{&lo, &hi}
		}
		if a.poFound {
			lo, hi := a.poMin, a.poMax
			ent.PriceOutputRange = [2]*float64{&lo, &hi}
		}
		entityIndex[key] = ent
	}

	entitySourceRel = rel

	// Attach compiled-in models.dev metadata and fold in any metadata-only standalone
	// entity it synthesizes. This is a no-op until the codegen slice bakes real
	// metadata (the accessor returns nil today), so the index — and every determinism
	// guarantee that rests on its first-seen key order — is unchanged for the current
	// corpus.
	attachBakedMetadataToIndex()
}

// attachBakedMetadataToIndex runs the metadata<->entity join over the just-built
// entity index using the compiled-in baked metadata (staticEntityMetadata): it writes
// each matched entity's Metadata back into the index and appends any synthesized
// metadata-only standalone entity to the index in a deterministic position.
//
// It returns immediately when no metadata is baked in (the current state: the
// accessor returns nil until the codegen slice emits the generated metadata file), so
// entityIndex/entityKeys stay byte-identical to the pre-metadata build until real
// baked data lands.
//
// Synthesized standalones are appended to entityKeys in ascending MetadataID order so
// their position in Entities() is deterministic and independent of map iteration; a
// synthesized key that collides with a real entity is dropped (a real entity is never
// overwritten by a standalone). Each appended standalone is a metadata-only entity
// attested by models.dev, so it MUST also contribute its (entity_key, source) rows to
// the entity↔source relation (entitySourceRel): the ratified invariant is "a model is
// attested by a source iff there is an EntitySource row", and a standalone's derived
// Entity.Sources projection would otherwise disagree with the relation. Because the
// standalone is synthesized AFTER buildEntitySourceRelation already ran over the static
// rows, this function extends the relation in lockstep and re-imposes its
// (EntityKey, SourceID) total order.
func attachBakedMetadataToIndex() {
	meta := staticEntityMetadata()
	if len(meta) == 0 {
		return
	}

	ents := make([]Entity, len(entityKeys))
	for i, key := range entityKeys {
		ents[i] = entityIndex[key]
	}

	attached, _, standalone := JoinEntityMetadata(ents, meta)

	// Write attached metadata back into the index (attached[i] <-> entityKeys[i]).
	for i, key := range entityKeys {
		if attached[i].Metadata == nil {
			continue
		}
		e := entityIndex[key]
		e.Metadata = attached[i].Metadata
		entityIndex[key] = e
	}

	// Append synthesized standalones deterministically (ascending MetadataID).
	sort.Slice(standalone, func(i, j int) bool {
		return standaloneMetadataID(standalone[i]) < standaloneMetadataID(standalone[j])
	})
	relExtended := false
	for _, s := range standalone {
		key := s.Ref.String()
		if _, exists := entityIndex[key]; exists {
			continue // never overwrite a real entity with a standalone
		}
		// Attest the standalone in the relation FIRST, then read the entity's Sources
		// projection back from it, so the projection stays a faithful derived view of
		// the relation — exactly the contract real entities get from
		// buildEntitySourceRelation. sortedSources yields the ascending, de-duplicated
		// DataSourceID set (a standalone carries [DataSourceModelsDev]).
		srcs := sortedSources(s.Sources)
		entitySourceRel.byEntity[key] = srcs
		for _, src := range srcs {
			entitySourceRel.rows = append(entitySourceRel.rows, EntitySource{EntityKey: key, SourceID: src})
			relExtended = true
		}
		s.Sources = srcs
		entityIndex[key] = s
		entityKeys = append(entityKeys, key)
	}

	// Re-impose the pinned (EntityKey, SourceID) total order on the relation rows so
	// the join relation (and any deterministic consumer of it) stays byte-stable after
	// the standalone extension.
	if relExtended {
		sort.Slice(entitySourceRel.rows, func(i, j int) bool {
			return lessEntitySource(entitySourceRel.rows[i], entitySourceRel.rows[j])
		})
	}
}

// lessEntitySource is the canonical total order on entity↔source relation rows —
// ascending by EntityKey, then by SourceID. It is the SINGLE source of truth for the
// relation's deterministic, byte-stable row order: both the initial build
// (buildEntitySourceRelation) and any later extension (the standalone attestation in
// attachBakedMetadataToIndex) sort through it, so the canonical order can never drift
// between the two sites.
func lessEntitySource(a, b EntitySource) bool {
	if a.EntityKey != b.EntityKey {
		return a.EntityKey < b.EntityKey
	}
	return a.SourceID < b.SourceID
}

// standaloneMetadataID returns the MetadataID a synthesized standalone entity carries,
// or "" when it has none (defensive — a synthesized standalone always carries one).
func standaloneMetadataID(e Entity) MetadataID {
	if e.Metadata == nil {
		return ""
	}
	return e.Metadata.MetadataID
}

// buildEntitySourceRelation materializes the BCNF entity↔source join relation from
// the per-entity first-seen attestation lists. For each key (in the supplied order)
// it sorts the attestation set ascending by DataSourceID — that sorted slice is the
// per-entity projection exposed as Entity.Sources and returned by EntitySources —
// then flattens the projections into rows and imposes the EXPLICIT (EntityKey,
// SourceID) total order so relation iteration (and the generated EntitySource
// emission that consumes it) is byte-deterministic regardless of map/first-seen
// order.
//
// It is the pure, testable seam behind loadEntityIndex: the projection sort is
// applied here, not at the call site, so feeding it a key whose first-seen order is
// reversed proves the projection AND rows come out ascending. The shipped corpus
// attests models.dev (lexically smallest) first, so a bypass that copied first-seen
// order unsorted would pass the whole public suite on shipped data — this seam lets
// that bypass be falsified directly with adversarial input.
func buildEntitySourceRelation(order []string, firstSeen map[string][]DataSourceID) *entitySourceRelation {
	rel := &entitySourceRelation{byEntity: make(map[string][]DataSourceID, len(order))}
	for _, key := range order {
		rel.byEntity[key] = sortedSources(firstSeen[key])
	}
	for _, key := range order {
		for _, src := range rel.byEntity[key] {
			rel.rows = append(rel.rows, EntitySource{EntityKey: key, SourceID: src})
		}
	}
	sort.Slice(rel.rows, func(i, j int) bool {
		return lessEntitySource(rel.rows[i], rel.rows[j])
	})
	return rel
}

// sortedSources returns a fresh slice holding the elements of in in ascending
// DataSourceID order. It is the testable projection-sort seam: the registry feeds
// it the first-seen attestation order, and the explicit sort is what makes the
// Entity.Sources / EntitySources output deterministic regardless of the order in
// which a source first attested an entity. An empty input returns nil.
func sortedSources(in []DataSourceID) []DataSourceID {
	if len(in) == 0 {
		return nil
	}
	out := append([]DataSourceID(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// sortedRegions returns a fresh slice holding the elements of in sorted ascending
// by Region enum value. Like sortedSources it is the deterministic projection-sort
// seam: the registry feeds it the first-seen per-instance Region order, and the
// explicit sort makes Entity.Regions output independent of instance order. Sorting
// by the int enum value (not the String token) is the natural total order for a
// closed int enum and keeps the aggregate stable across bakes. An empty input
// returns nil.
func sortedRegions(in []Region) []Region {
	if len(in) == 0 {
		return nil
	}
	out := append([]Region(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// entityIndexLookup returns the cached entity for the given EntityRef key and
// whether it exists. The returned Entity is the cached value (NOT a copy);
// callers that hand it to external code MUST clone it first.
func entityIndexLookup(key string) (Entity, bool) {
	entityIndexOnce.Do(loadEntityIndex)
	e, ok := entityIndex[key]
	return e, ok
}

// entityIndexAll returns the cached entities in deterministic key order (NOT
// copies — callers MUST clone before exposing).
func entityIndexAll() []Entity {
	entityIndexOnce.Do(loadEntityIndex)
	out := make([]Entity, 0, len(entityKeys))
	for _, key := range entityKeys {
		out = append(out, entityIndex[key])
	}
	return out
}

// StaticModels returns a defensive copy of the compiled-in model data.
// Modifying the returned slice does not affect the registry.
func StaticModels() []ModelInfo {
	out := make([]ModelInfo, len(staticModels))
	copy(out, staticModels)
	return out
}

// LookupModel searches the static registry for a model by its ID.
// It returns the model and true if found, or the zero value and false otherwise.
func LookupModel(id ModelID) (ModelInfo, bool) {
	for _, m := range staticModels {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// ModelsByProvider returns all static models from the given provider.
func ModelsByProvider(p Provider) []ModelInfo {
	var out []ModelInfo
	for _, m := range staticModels {
		if m.Provider == p {
			out = append(out, m)
		}
	}
	return out
}

// ModelsByFamily returns all static models with the given raw API family string.
// The family parameter matches the RawFamily field (verbatim API value, e.g.
// "claude-opus", "gemini-flash").
func ModelsByFamily(family Family) []ModelInfo {
	var out []ModelInfo
	for _, m := range staticModels {
		if m.RawFamily == family {
			out = append(out, m)
		}
	}
	return out
}

// ProvidersOf returns the distinct providers that serve the entity identified by
// ref, sorted ascending by provider string. It is the API-level replacement for the
// provider-flavored Model__ constants removed in the entity-constants hard cut: rather
// than one constant per (entity, provider) pair, an entity has a single Entity__
// constant and its serving providers are queried here.
//
// The lookup keys off ref.String() (the canonical entity key), so the ref need only
// carry the identity tuple; the returned slice is a fresh copy the caller may mutate.
// An entity absent from the static registry yields nil (no error — a ref for an
// unknown entity simply has no providers), so a caller distinguishes "unknown entity"
// from "known entity, no providers" by the entity's presence elsewhere, not by this
// result.
func ProvidersOf(ref EntityRef) []Provider {
	e, ok := entityIndexLookup(ref.String())
	if !ok {
		return nil
	}
	return sortedProviders(e.Providers)
}

// ProvidersOfModel returns the distinct providers that serve the entity the given
// model ID belongs to, sorted ascending. It is the instance-level convenience over
// ProvidersOf: it resolves the model to its entity identity (the same
// identity-class projection the registry index uses) and delegates. An ID absent from
// the static registry yields nil.
//
// Note a single raw ID can be served by several providers under one entity; this
// returns ALL of that entity's providers, not only the first-matched instance's.
func ProvidersOfModel(id ModelID) []Provider {
	m, ok := LookupModel(id)
	if !ok {
		return nil
	}
	ref := EntityRef{
		Family:    m.Family,
		Variant:   m.Variant,
		Version:   m.Version,
		ParamSize: m.ParamSize,
		Modifier:  EntityModifiers(m.Modifier, m.Family),
	}
	return ProvidersOf(ref)
}

// sortedProviders returns a fresh slice holding the elements of in in ascending
// provider-string order. The registry's Entity.Providers aggregate is de-duplicated
// but in first-seen order; this imposes the deterministic ascending order ProvidersOf
// promises (the sortedSources / sortedRegions projection-sort discipline). An empty
// input returns nil.
func sortedProviders(in []Provider) []Provider {
	if len(in) == 0 {
		return nil
	}
	out := append([]Provider(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// LookupModelByProvider searches the static registry for a model matching both
// the given provider and name (model ID string). It returns the model and true
// if found, or the zero value and false otherwise.
func LookupModelByProvider(p Provider, name string) (ModelInfo, bool) {
	for _, m := range staticModels {
		if m.Provider == p && string(m.ID) == name {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// Models returns all available models. It delegates to StaticModels and returns
// a defensive copy so callers cannot mutate the registry. This is the preferred
// API for external callers; StaticModels is an implementation detail.
//
// See EntityKeys() (in entities_constants_gen.go) for the canonical Entity__* entity-key
// constant slice, and ProvidersOf to enumerate an entity's serving providers.
func Models() []ModelInfo {
	return StaticModels()
}
