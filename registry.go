package bestiary

import "sync"

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
var (
	entityIndexOnce sync.Once
	entityIndex     map[string]Entity
	entityKeys      []string
)

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

	lineage []LineageEdge
	linSeen map[string]struct{}

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
			Family:   m.Family,
			Variant:  m.Variant,
			Version:  m.Version,
			Modifier: EntityModifiers(m.Modifier, m.Family),
		}
		key := ref.String()

		a := aggs[key]
		if a == nil {
			a = &entityAgg{
				ref:      ref,
				provSeen: make(map[Provider]struct{}),
				hostSeen: make(map[Host]struct{}),
				linSeen:  make(map[string]struct{}),
			}
			aggs[key] = a
			order = append(order, key)
		}

		a.instances = append(a.instances, ProviderInstance{
			ID:                m.ID,
			Provider:          m.Provider,
			Host:              m.Host,
			CostInputPerMTok:  m.CostInputPerMTok,
			CostOutputPerMTok: m.CostOutputPerMTok,
			ContextWindow:     m.ContextWindow,
			MaxOutput:         m.MaxOutput,
		})

		if _, dup := a.provSeen[m.Provider]; !dup {
			a.provSeen[m.Provider] = struct{}{}
			a.providers = append(a.providers, m.Provider)
		}
		if _, dup := a.hostSeen[m.Host]; !dup {
			a.hostSeen[m.Host] = struct{}{}
			a.hosts = append(a.hosts, m.Host)
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
	for _, key := range order {
		a := aggs[key]
		ent := Entity{
			Ref:            a.ref,
			Instances:      a.instances,
			Lineage:        a.lineage,
			Providers:      a.providers,
			Hosts:          a.hosts,
			ContextRange:   [2]int{a.ctxMin, a.ctxMax},
			MaxOutputRange: [2]int{a.moMin, a.moMax},
			Capabilities:   a.caps,
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
// See ModelIDs() (in models_constants_gen.go) for the canonical Model_* constant slice.
func Models() []ModelInfo {
	return StaticModels()
}
