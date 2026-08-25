package bestiary

import "strings"

// QuantVRAM captures the per-quantization weight and VRAM footprint for a
// single quantization variant of a model.
//
// Field semantics:
//
//   - Quant is the parsed quantization constant; QuantRaw is the verbatim token
//     from the source data, always populated for every row (preserves original
//     casing, e.g. "Q4_K_M" from Ollama's file_type field).
//   - WeightsBytes is the ground-truth ingested GGUF file size in bytes.  It is
//     the primary measurement and is always sourced from the downloaded file, not
//     derived from bits-per-weight.
//   - VRAMBytes is the estimated total VRAM requirement: WeightsBytes plus the
//     KV-cache at VRAMContextTokens (the model's maximum context window).  When
//     arch-facts are absent (Layers/KVHeads/HeadDim all zero), VRAMBytes equals
//     WeightsBytes and VRAMEstimatePartial is set true.
//   - VRAMContextTokens is the context-window size (tokens) used to compute the
//     KV-cache term.  It is the model-max context, not a user-chosen value.
//   - Layers, KVHeads, HeadDim are the architectural parameters used for the
//     KV-cache computation.  HeadDim is the embedding width per attention head
//     (elements per head).  Zero when the source did not supply them.
//   - VRAMEstimatePartial is true when the KV-cache term was excluded from
//     VRAMBytes because one or more of Layers/KVHeads/HeadDim is zero.  A true
//     value means VRAMBytes is a weights-only lower bound, never a silent
//     under-estimate.
//
// Zero values mean unknown: int64 0 = unknown bytes, int 0 = unknown count.
// nil QuantVRAM slice on a ProviderInstance or ModelInfo means no quant data is
// available for that row.
type QuantVRAM struct {
	// Quant is the parsed quantization type.
	Quant Quantization
	// QuantRaw is the verbatim quant token from the source data, preserving the
	// original casing exactly as it appeared (e.g. "Q4_K_M" from an Ollama
	// file_type field, or "q4_k_m" as written in a curated JSON file). It is
	// populated for every row — not only when Quant is QuantizationOther —
	// so callers can use it for lossless display or round-trip fidelity without
	// a separate round-trip through Quant.String().
	QuantRaw string
	// WeightsBytes is the ingested GGUF file size in bytes — the ground-truth
	// weights footprint for this quantization variant. It is ALWAYS a measurement
	// and is NEVER derived from bits-per-weight arithmetic; nothing writes a
	// computed estimate here.
	//
	// A separately-typed DERIVED projection does exist for entities that carry an
	// attested total parameter count but no ingested file size: see WeightsBasis and
	// DerivedWeightsBytes in fit.go. That projection is computed at display time,
	// carries BasisDerived, is qualified as a weights-only lower bound, and is never
	// written into this field or into VRAMBytes. This paragraph is the amendment
	// that keeps the sentence above true: the invariant is about what is STORED, not
	// about what may ever be computed from a parameter count.
	WeightsBytes int64
	// VRAMBytes is weights + KV-cache at VRAMContextTokens; equals WeightsBytes
	// when VRAMEstimatePartial is true.
	VRAMBytes int64
	// VRAMContextTokens is the context window (tokens) used to compute VRAMBytes.
	VRAMContextTokens int
	// Layers is the number of transformer layers; 0 when unknown.
	Layers int
	// KVHeads is the number of KV-attention heads (GQA-aware); 0 when unknown.
	KVHeads int
	// HeadDim is the embedding width per attention head (elements per head); 0 when unknown.
	HeadDim int
	// VRAMEstimatePartial is true when the KV-cache term was omitted from
	// VRAMBytes because at least one of Layers, KVHeads, or HeadDim is zero.
	// Callers must check this flag before treating VRAMBytes as a full estimate.
	VRAMEstimatePartial bool
	// OCIDigest is the "sha256:<hex>" content-addressed manifest digest of this
	// quantization's Ollama-registry artifact — the value that makes a `pkg:oci`
	// purl (OCIPurl) uniquely identify the artifact. "" when absent (the digest is a
	// FETCH-OWNED field, captured by the offline cmd/bestiary-ollama refresh; it is
	// empty for every curated row until the next deliberate refresh harvests it).
	OCIDigest string
}

// OCIPurl renders this quantization's purl-spec `pkg:oci` package URL, passing the
// row's own OCIDigest as the content-addressed version. It returns "" when OCIDigest
// is empty — an OCI purl is never minted without a digest (the digest is what makes
// it identify an artifact). name is the repository name (the last fragment is
// lowercased per spec); tag and registry become the optional repository_url/tag
// qualifiers when non-empty. See formatOCIPurl (oci.go) for the full render contract.
func (q QuantVRAM) OCIPurl(name, tag, registry string) string {
	return formatOCIPurl(name, q.OCIDigest, tag, registry)
}

// EntityRef is the canonical IDENTITY of a model entity — the tuple that
// determines whether two provider/host instances are "the same model". It is the
// comparable map key for entity grouping (via EntityRef.String) and doubles as
// the parent reference in a lineage edge (see LineageEdge).
//
// Identity is (Family, Variant, Version, ParamSize) PLUS the identity-class
// modifiers in Modifier. Crucially:
//
//   - Version is the IDENTITY version (e.g. "4.5"), NOT a release date. EntityRef
//     deliberately has NO Date field: a date is a per-release attribute, not part
//     of identity. Do not conflate EntityRef's "@version" with formatCanonical's
//     "@date".
//   - ParamSize is the canonical parameter-size token (e.g. "70b", "8b"). It is
//     part of entity identity because "llama@3.3#70b{instruct}" and
//     "llama@3.3#8b{instruct}" are distinct models. Empty when size is unknown.
//   - Modifier holds ONLY identity-class modifiers (see EntityModifiers /
//     ClassifyModifier). Attribute-class modifiers and per-instance attributes
//     (host, price, quant, …) are NEVER part of EntityRef and NEVER appear in the
//     key string.
//
// Because Modifier is a slice, an EntityRef value is not itself comparable and
// cannot be used directly as a map key; use EntityRef.String() as the key.
type EntityRef struct {
	Family    Family
	Variant   string
	Version   string
	ParamSize string   // canonical parameter-size token, e.g. "70b"; empty when unknown
	Modifier  []string // identity-class modifiers only, canonical order
}

// String returns the canonical comparable key for this entity:
//
//	family[/variant][@version][#paramsize]{identity-mods}
//
// Rules:
//   - "/variant" is appended only when Variant is non-empty.
//   - "@version" is appended only when Version is non-empty (this is the IDENTITY
//     version, never a date).
//   - "#paramsize" is appended only when ParamSize is non-empty. It is placed
//     AFTER @version and BEFORE {identity-mods}. The '#' sentinel was chosen
//     because it does not collide with any existing segment character.
//     When ParamSize is empty, the segment is OMITTED ENTIRELY so every existing
//     key remains byte-identical to its pre-paramsize value.
//   - "{identity-mods}" is appended only when at least one identity modifier is
//     present; the tokens are de-duplicated and rendered in canonical order
//     (CanonicalizeModifiers), comma-separated. The braces are OMITTED entirely
//     when there are no identity modifiers.
//   - The "[attributes]" segment is NEVER part of this key (attributes do not
//     affect identity).
//
// Two EntityRefs whose Modifier slices are permutations of the same identity-mod
// set produce the IDENTICAL key.
func (r EntityRef) String() string {
	var b strings.Builder
	b.WriteString(string(r.Family))
	if r.Variant != "" {
		b.WriteByte('/')
		b.WriteString(r.Variant)
	}
	if r.Version != "" {
		b.WriteByte('@')
		b.WriteString(r.Version)
	}
	if r.ParamSize != "" {
		b.WriteByte('#')
		b.WriteString(r.ParamSize)
	}
	if key := modifierKey(r.Modifier); key != "" {
		b.WriteByte('{')
		b.WriteString(key)
		b.WriteByte('}')
	}
	return b.String()
}

// parseEntityKey is the inverse of EntityRef.String(): it decomposes a canonical
// entity-key string (family[/variant][@version][#size]{mod1,mod2,...}) back into an
// EntityRef. It strips the segments in the reverse of the render order — trailing
// {mods}, then #size, then @version, then /variant, leaving family — because those
// separators never appear inside a segment (version uses '.', not '/'). Modifiers are
// stored already-canonical in the key, so a split on ',' round-trips through
// EntityRef.String() byte-identically. It is used by the store (QueryNomina) to
// rebuild ResolvesTo from a persisted key; a caller that already holds the ref never
// needs it.
func parseEntityKey(key string) EntityRef {
	var ref EntityRef
	s := key
	if i := strings.LastIndexByte(s, '{'); i >= 0 && strings.HasSuffix(s, "}") {
		inner := s[i+1 : len(s)-1]
		s = s[:i]
		if inner != "" {
			ref.Modifier = strings.Split(inner, ",")
		}
	}
	if i := strings.LastIndexByte(s, '#'); i >= 0 {
		ref.ParamSize = s[i+1:]
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		ref.Version = s[i+1:]
		s = s[:i]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		ref.Variant = s[i+1:]
		s = s[:i]
	}
	ref.Family = Family(s)
	return ref
}

// LineageEdge is one directed derivation relationship: this model was derived
// from Parent via technique Kind. A model with multiple parents (e.g. a MERGE)
// carries multiple LineageEdges; Parent is a full EntityRef so a parent can be
// resolved to its own entity (and its own ancestors) for DAG traversal.
type LineageEdge struct {
	Parent EntityRef
	Kind   DerivationKind
}

// ProviderInstance is a single concrete offering of an entity: one (provider,
// host) serving of the model, with its instance-specific pricing and limits.
// Many ProviderInstances roll up into one Entity. The fields here are exactly the
// per-instance ATTRIBUTES — they vary across instances of the same entity and so
// are excluded from EntityRef.
type ProviderInstance struct {
	ID       ModelID
	Provider Provider
	Host     Host
	// Region is the serving jurisdiction of this instance (AWS Bedrock cross-region
	// inference profile), projected from ModelInfo.Region at registry roll-up. Like
	// Host it is a per-instance attribute, never part of identity; RegionNone when
	// the ID carries no region prefix (renders "unspecified"). RegionRaw carries the
	// verbatim token only for the fail-safe RegionOther.
	Region            Region
	RegionRaw         string
	CostInputPerMTok  *float64 // nil when unknown
	CostOutputPerMTok *float64 // nil when unknown
	ContextWindow     int
	MaxOutput         int
	// QuantVRAM is the per-quantization weight and VRAM footprint for this
	// instance. nil when no quantization data is available for this row.
	QuantVRAM []QuantVRAM
	// Source is the data source that provided this row. DataSourceNone (zero
	// value, empty string) when no source is recorded.
	Source DataSourceID
}

// CapabilityUnion is the aggregate capability view across all instances of an
// entity: each boolean is the OR over the corresponding per-instance capability
// (an entity "supports" a capability if ANY instance does). The zero value
// (all-false) is the identity-safe default for an entity with no instances.
type CapabilityUnion struct {
	Reasoning        bool
	ToolCall         bool
	Attachment       bool
	Temperature      bool
	StructuredOutput bool
	Interleaved      bool
	OpenWeights      bool
}

// Entity is a model identity (Ref) together with every provider/host instance
// that serves it and the aggregate views derived from those instances. It is the
// unit returned by Entities() / EntityByTuple().
//
// Range fields summarize the instances:
//   - PriceInputRange / PriceOutputRange: [min,max] over the NON-nil instance
//     costs only; when every instance cost is nil the range is {nil,nil} (no
//     nil-deref, no spurious zero). Indices: [0]=min, [1]=max.
//   - ContextRange / MaxOutputRange: [min,max] over instance context/max-output.
type Entity struct {
	Ref       EntityRef
	Instances []ProviderInstance
	Lineage   []LineageEdge
	Providers []Provider
	Hosts     []Host
	// Regions is the sorted (ascending Region value), de-duplicated aggregate of the
	// per-instance Region across every instance of this entity — the Providers/Hosts
	// aggregate pattern. An entity served in multiple jurisdictions (e.g. a Bedrock
	// claude offered us/eu/au/jp/global) surfaces all its regions here. It is nil only
	// on a hand-constructed Entity that never went through the registry aggregate; a
	// registry entity with no region prefix on any instance carries [RegionNone].
	Regions          []Region
	PriceInputRange  [2]*float64
	PriceOutputRange [2]*float64
	ContextRange     [2]int
	MaxOutputRange   [2]int
	Capabilities     CapabilityUnion
	// Sources is a DERIVED, sorted (ascending DataSourceID), de-duplicated read
	// projection of the entity↔source join relation. It is NOT a source of truth —
	// it is a convenience view over the EntitySource join table. For any entity
	// returned by the registry it is always populated (every registry entity attests
	// at least the models.dev origin); it is nil only on a hand-constructed Entity
	// value that never went through the registry aggregate.
	Sources []DataSourceID
	// Metadata is the PRIMARY provider-agnostic metadata row for this entity — a
	// DERIVED convenience projection of MetadataAll, not an independent slot. It is
	// a pointer because metadata is genuinely optional: nil when no EntityMetadata
	// was joined to this identity. When present it is owned by the Entity value and
	// is deep-copied on read alongside the other entity fields.
	//
	// The primary is the row with the SHORTEST MetadataID, ties broken lexicographic
	// ascending. Shortest-id is a naming rule, not a payload rule: a lab's canonical
	// id is its shortest one ("openai/gpt-5.5" over "openai/gpt-5.5-instant"), so the
	// primary is stable under re-ingest and independent of how many benchmark claims
	// or links a row happens to carry. Choosing by payload size would let a row's
	// claim count silently re-designate the entity's canonical naming, and inheriting
	// the incoming join order would make the choice depend on upstream iteration.
	Metadata *EntityMetadata
	// MetadataAll is EVERY provider-agnostic metadata row that joined to this
	// entity's identity, sorted ascending by MetadataID. Distinct lab ids routinely
	// decompose to one entity key (a dated alias and its floating alias; a base model
	// and a serving tier that is not a distinct artifact), and each such row carries
	// its OWN benchmark claims attributable to its OWN MetadataID.
	//
	// Metadata (the primary) is derived from this slice; MetadataAll is the complete
	// record. Benchmark claims from different rows are NEVER fused into one table:
	// a claim is attributable to the MetadataID under which the lab reported it, and
	// merging two rows' tables would fabricate an assessment record no lab published.
	// nil (never empty-non-nil) when no metadata joined to this identity, so the
	// nil-means-unjoined convention holds on both fields together.
	MetadataAll []EntityMetadata
	// Creator is the lab / organization that TRAINED this entity's models (the SPDX
	// originator), DISTINCT from the Providers that host it (the SPDX suppliers). It
	// is a DERIVED JOIN PROJECTION — the Entity.Sources / Entity.Regions precedent —
	// computed in loadEntityIndex from Ref.Family via the curated creators.json seed
	// (Family.Creator), NOT a stored column (Family → Creator is a function, so a
	// per-entity column would be a transitive dependency / BCNF violation). All
	// instances of an entity share its Family, so one value covers the whole entity.
	// CreatorNone (empty string) when the family has no curated mapping; it stays out
	// of EntityRef (never re-keys the entity). It is nil-free (a value type), so a
	// hand-constructed Entity that never went through the registry simply carries
	// CreatorNone until projected.
	Creator Creator
}

// Entities returns every model entity in the static registry, each with its
// instances and aggregate views populated. The slice is ordered deterministically
// by first-seen entity key.
//
// The result is a DEFENSIVE DEEP COPY: every returned Entity (and all of its
// slices and price pointers) is independent of the memoized registry index and
// of every other returned Entity. Mutating a returned value can never corrupt the
// registry or alias another entity.
func Entities() []Entity {
	cached := entityIndexAll()
	out := make([]Entity, len(cached))
	for i, e := range cached {
		out[i] = cloneEntity(e)
	}
	return out
}

// EntityByTuple looks up a single entity by its identity tuple: family, variant,
// version, paramSize, and any identity-class modifiers. The bool reports whether
// a matching entity exists. Lookup is by EntityRef.String() equality, so the
// modifier arguments are order-independent.
//
// paramSize is the canonical parameter-size token (e.g. "70b", "8b"), or "" when
// size is unknown. A non-empty paramSize produces a #-segment in the lookup key,
// distinguishing sized entities (e.g. "llama@3.3#70b{instruct}") from entities
// with no curated size ("llama@3.3{instruct}").
//
// The supplied modifiers are passed through EntityModifiers(_, family), the same
// identity-class projection used to build the index keys: attribute-class tokens
// are dropped and the remainder canonicalized, so a caller need not pre-filter.
// The returned Entity is a defensive deep copy (see Entities).
func EntityByTuple(family Family, variant, version, paramSize string, identityModifiers ...string) (Entity, bool) {
	ref := EntityRef{
		Family:    family,
		Variant:   variant,
		Version:   version,
		ParamSize: paramSize,
		Modifier:  EntityModifiers(identityModifiers, family),
	}
	e, ok := entityIndexLookup(ref.String())
	if !ok {
		return Entity{}, false
	}
	return cloneEntity(e), true
}

// cloneEntity returns a deep copy of e: a fresh backing array for every slice and
// a fresh *float64 for every non-nil price-range bound. The [2]int ranges and the
// CapabilityUnion are value types and copy by assignment. This is the single seam
// that enforces VC9 (defensive copy / no-wrong-merge) for all entity reads.
func cloneEntity(e Entity) Entity {
	c := e
	c.Ref = cloneRef(e.Ref)
	c.Instances = cloneInstances(e.Instances)
	c.Lineage = cloneLineage(e.Lineage)
	if e.Providers != nil {
		c.Providers = append([]Provider(nil), e.Providers...)
	}
	if e.Hosts != nil {
		c.Hosts = append([]Host(nil), e.Hosts...)
	}
	if e.Regions != nil {
		c.Regions = append([]Region(nil), e.Regions...)
	}
	if e.Sources != nil {
		c.Sources = append([]DataSourceID(nil), e.Sources...)
	}
	c.MetadataAll = cloneEntityMetadataAll(e.MetadataAll)
	c.Metadata = primaryEntityMetadata(c.MetadataAll)
	if c.MetadataAll == nil {
		// No multi-row record to derive from: carry the pointer through on its own.
		// This keeps a hand-constructed Entity (one that never went through the join)
		// byte-identical across a clone instead of silently dropping its metadata.
		c.Metadata = cloneEntityMetadata(e.Metadata)
	}
	c.PriceInputRange = cloneFloatPair(e.PriceInputRange)
	c.PriceOutputRange = cloneFloatPair(e.PriceOutputRange)
	return c
}

// cloneEntityMetadata deep-copies an *EntityMetadata: it duplicates the struct
// and gives its Links and Benchmarks slices fresh backing arrays. ModelLink and
// BenchmarkResult rows contain only value types (no nested pointers or slices),
// so a shallow element copy of each slice is a full deep copy. Returns nil for a
// nil input (the nil-means-unjoined convention), so a caller can never reach back
// into the registry-owned metadata through a returned Entity.
func cloneEntityMetadata(m *EntityMetadata) *EntityMetadata {
	if m == nil {
		return nil
	}
	c := *m
	if m.Links != nil {
		c.Links = append([]ModelLink(nil), m.Links...)
	}
	if m.Benchmarks != nil {
		c.Benchmarks = append([]BenchmarkResult(nil), m.Benchmarks...)
	}
	return &c
}

// cloneEntityMetadataAll deep-copies a MetadataAll slice ELEMENT-WISE: every row is
// cloned through cloneEntityMetadata so its Links and Benchmarks get fresh backing
// arrays. A bare append([]EntityMetadata(nil), in...) would copy the row structs but
// leave every row's Links/Benchmarks slice header aliasing the source, so a caller
// mutating a returned entity's benchmark table would reach back into registry-owned
// storage. Returns nil for a nil input (the nil-means-unjoined convention).
func cloneEntityMetadataAll(in []EntityMetadata) []EntityMetadata {
	if in == nil {
		return nil
	}
	out := make([]EntityMetadata, len(in))
	for i := range in {
		out[i] = *cloneEntityMetadata(&in[i])
	}
	return out
}

// primaryEntityMetadata returns a pointer to the PRIMARY row of a MetadataAll slice
// — the row with the shortest MetadataID, ties broken lexicographic ascending — or
// nil when the slice is empty. The returned pointer aliases INTO the supplied slice,
// so callers pass the already-cloned slice (cloneEntity does) and the entity's
// Metadata and MetadataAll[i] stay the same value rather than diverging copies.
//
// See Entity.Metadata for why identity length, not payload size or incoming order,
// selects the primary.
func primaryEntityMetadata(all []EntityMetadata) *EntityMetadata {
	if len(all) == 0 {
		return nil
	}
	best := 0
	for i := 1; i < len(all); i++ {
		if lessMetadataPrimary(all[i].MetadataID, all[best].MetadataID) {
			best = i
		}
	}
	return &all[best]
}

// lessMetadataPrimary reports whether a outranks b as the primary MetadataID:
// shorter id first, ties broken lexicographic ascending. It is a total order over
// distinct ids, so the primary is unique and deterministic.
func lessMetadataPrimary(a, b MetadataID) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

// cloneRef deep-copies an EntityRef, duplicating its Modifier slice.
func cloneRef(r EntityRef) EntityRef {
	c := r
	if r.Modifier != nil {
		c.Modifier = append([]string(nil), r.Modifier...)
	}
	return c
}

// cloneInstances deep-copies a ProviderInstance slice, including each instance's
// cost pointers and QuantVRAM slice, so a caller cannot reach back into
// registry-owned float64s or QuantVRAM rows.
func cloneInstances(in []ProviderInstance) []ProviderInstance {
	if in == nil {
		return nil
	}
	out := make([]ProviderInstance, len(in))
	for i, inst := range in {
		c := inst
		c.CostInputPerMTok = cloneFloatPtr(inst.CostInputPerMTok)
		c.CostOutputPerMTok = cloneFloatPtr(inst.CostOutputPerMTok)
		c.QuantVRAM = cloneQuantVRAM(inst.QuantVRAM)
		out[i] = c
	}
	return out
}

// cloneQuantVRAM deep-copies a QuantVRAM slice. QuantVRAM rows contain only
// value types (no pointers), so a shallow element copy suffices; the function
// allocates a fresh backing array so the caller cannot alias the source slice.
func cloneQuantVRAM(in []QuantVRAM) []QuantVRAM {
	if in == nil {
		return nil
	}
	out := make([]QuantVRAM, len(in))
	copy(out, in)
	return out
}

// cloneLineage deep-copies a LineageEdge slice, duplicating each parent ref's
// Modifier slice.
func cloneLineage(in []LineageEdge) []LineageEdge {
	if in == nil {
		return nil
	}
	out := make([]LineageEdge, len(in))
	for i, e := range in {
		c := e
		c.Parent = cloneRef(e.Parent)
		out[i] = c
	}
	return out
}

// cloneFloatPair returns a [2]*float64 with fresh pointers for each non-nil bound,
// preserving nils. The result shares no storage with the input.
func cloneFloatPair(p [2]*float64) [2]*float64 {
	return [2]*float64{cloneFloatPtr(p[0]), cloneFloatPtr(p[1])}
}

// cloneFloatPtr returns a fresh *float64 with the same value, or nil for nil.
func cloneFloatPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// Entity.Ancestors and Entity.Descendants — the cycle-safe DAG traversal over the
// curated lineage ledger — are implemented in lineage.go (IP-4).
