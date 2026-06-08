package bestiary

import "strings"

// EntityRef is the canonical IDENTITY of a model entity — the tuple that
// determines whether two provider/host instances are "the same model". It is the
// comparable map key for entity grouping (via EntityRef.String) and doubles as
// the parent reference in a lineage edge (see LineageEdge).
//
// Identity is (Family, Variant, Version) PLUS the identity-class modifiers in
// Modifier. Crucially:
//
//   - Version is the IDENTITY version (e.g. "4.5"), NOT a release date. EntityRef
//     deliberately has NO Date field: a date is a per-release attribute, not part
//     of identity. Do not conflate EntityRef's "@version" with formatCanonical's
//     "@date".
//   - Modifier holds ONLY identity-class modifiers (see EntityModifiers /
//     ClassifyModifier). Attribute-class modifiers and per-instance attributes
//     (host, price, quant, …) are NEVER part of EntityRef and NEVER appear in the
//     key string.
//
// Because Modifier is a slice, an EntityRef value is not itself comparable and
// cannot be used directly as a map key; use EntityRef.String() as the key.
type EntityRef struct {
	Family   Family
	Variant  string
	Version  string
	Modifier []string // identity-class modifiers only, canonical order
}

// String returns the canonical comparable key for this entity:
//
//	family[/variant][@version]{identity-mods}
//
// Rules:
//   - "/variant" is appended only when Variant is non-empty.
//   - "@version" is appended only when Version is non-empty (this is the IDENTITY
//     version, never a date).
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
// version, and any identity-class modifiers. The bool reports whether a matching
// entity exists. Lookup is by EntityRef.String() equality, so the modifier
// arguments are order-independent.
//
// The supplied modifiers are passed through EntityModifiers(_, family), the same
// identity-class projection used to build the index keys: attribute-class tokens
// are dropped and the remainder canonicalized, so a caller need not pre-filter.
// The returned Entity is a defensive deep copy (see Entities).
func EntityByTuple(family Family, variant, version string, identityModifiers ...string) (Entity, bool) {
	ref := EntityRef{
		Family:   family,
		Variant:  variant,
		Version:  version,
		Modifier: EntityModifiers(identityModifiers, family),
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
// cost pointers, so a caller cannot reach back into registry-owned float64s.
func cloneInstances(in []ProviderInstance) []ProviderInstance {
	if in == nil {
		return nil
	}
	out := make([]ProviderInstance, len(in))
	for i, inst := range in {
		c := inst
		c.CostInputPerMTok = cloneFloatPtr(inst.CostInputPerMTok)
		c.CostOutputPerMTok = cloneFloatPtr(inst.CostOutputPerMTok)
		out[i] = c
	}
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
