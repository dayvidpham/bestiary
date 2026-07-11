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
	// weights footprint for this quantization variant.
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
	ID                ModelID
	Provider          Provider
	Host              Host
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
	Ref              EntityRef
	Instances        []ProviderInstance
	Lineage          []LineageEdge
	Providers        []Provider
	Hosts            []Host
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
	if e.Sources != nil {
		c.Sources = append([]DataSourceID(nil), e.Sources...)
	}
	c.PriceInputRange = cloneFloatPair(e.PriceInputRange)
	c.PriceOutputRange = cloneFloatPair(e.PriceOutputRange)
	return c
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
