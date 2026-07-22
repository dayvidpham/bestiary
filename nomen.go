package bestiary

import (
	"fmt"
	"sort"
	"strings"
)

// NomenScheme classifies a Nomen by the KIND of naming it records — a provenance
// classifier, NOT a rendering codec.
//
// It is deliberately distinct from canonical.go's CanonicalScheme: CanonicalScheme
// is a RENDER CODEC (given a ModelRef, produce a string in some serialization), while
// NomenScheme is a RECORDED-FACT CLASSIFIER (given an observed naming, say what kind
// of naming it is — a provider's raw ID spelling, the canonical key itself, a
// third-party-asserted alias, …). The two never collide because their constants are
// type-prefixed (NomenScheme*) and live on different types.
//
// It follows the ELEMENT-enum convention (the Quantization / ReasoningOptionKind
// precedent): every nomen is minted with a KNOWN scheme, so absence is
// uninhabitable — the zero value NomenSchemeOther is the fail-safe bucket, sitting
// AT zero, never a "none" sentinel.
type NomenScheme int

const (
	// NomenSchemeOther is the fail-safe bucket (zero value): a recorded naming whose
	// kind is not one of the known schemes. No mint path produces it today; it exists
	// so a future feeder never has to drop an unclassifiable naming.
	NomenSchemeOther NomenScheme = iota
	// NomenSchemeCanonical is the entity key itself — the canonical decomposed
	// identity (EntityRef.String()). It is the PREFERRED designation of an entity.
	NomenSchemeCanonical
	// NomenSchemeProviderID is a provider's verbatim model-ID spelling (the raw
	// catalog ID). It is an ADMITTED designation: recognized and usable, not preferred.
	NomenSchemeProviderID
	// NomenSchemeHuggingFace is a HuggingFace Hub org/repo naming (reserved for the
	// external-identifier slice; seeded via curated claims).
	NomenSchemeHuggingFace
	// NomenSchemePURL is a Package-URL naming (reserved for the external-identifier
	// slice).
	NomenSchemePURL
	// NomenSchemeAlias is a third-party-asserted alias — a naming a lab or vendor
	// declares for a model (e.g. xAI publishing "grok-beta" as an alias). It carries
	// claim attribution (Nomen.SourceURL = who asserted it) and is ADMITTED.
	NomenSchemeAlias
)

// nomenSchemeNames is the single source of truth for String / MarshalText /
// UnmarshalText, indexed by the NomenScheme int value. The tokens are stable
// lowercase wire names used in the public JSON contract ($defs.Nomen.Scheme).
var nomenSchemeNames = []string{
	NomenSchemeOther:       "other",
	NomenSchemeCanonical:   "canonical",
	NomenSchemeProviderID:  "provider-id",
	NomenSchemeHuggingFace: "huggingface",
	NomenSchemePURL:        "purl",
	NomenSchemeAlias:       "alias",
}

// String renders the NomenScheme as its canonical lowercase wire token. An
// out-of-range value degrades to "other" (never a panic).
func (s NomenScheme) String() string {
	if int(s) < 0 || int(s) >= len(nomenSchemeNames) {
		return "other"
	}
	return nomenSchemeNames[s]
}

// MarshalText implements encoding.TextMarshaler so a NomenScheme serializes as its
// lowercase token in JSON/YAML (the ModelStatus precedent), not as its underlying
// int. It never errors — String covers every value.
func (s NomenScheme) MarshalText() ([]byte, error) {
	return []byte(s.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of MarshalText: it
// maps a lowercase wire token back to its NomenScheme. An unrecognized token is an
// actionable error (the ModelStatus precedent), never a silent NomenSchemeOther.
func (s *NomenScheme) UnmarshalText(text []byte) error {
	lower := strings.ToLower(strings.TrimSpace(string(text)))
	for i, name := range nomenSchemeNames {
		if name == lower {
			*s = NomenScheme(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal NomenScheme from %q\n"+
			"  What: the token does not match any known naming scheme\n"+
			"  Why: only %v are accepted\n"+
			"  Where: bestiary.NomenScheme.UnmarshalText (nomen.go)\n"+
			"  How to fix: use one of the accepted scheme tokens",
		string(text), nomenSchemeNames,
	)
}

// Nomen is one recorded NAMING of an entity: a value string classified by scheme,
// carrying its acceptability status, the entity it resolves to, and — crucially —
// its provenance split into two distinct levels.
//
// The two provenance fields must NEVER be conflated (the BenchmarkResult
// SourceURL-vs-Source discipline):
//   - SourceURL is WHO ASSERTS the naming (claim attribution) — e.g. the xAI model
//     page that declares "grok-beta" as an alias. It is a claimant, a URL to the
//     party making the naming assertion. Empty for bestiary-minted nomina
//     (provider-ID spellings and canonical keys assert themselves).
//   - Source is WHICH INGEST we read the naming from (a DataSourceID FK) — the
//     catalog/pipeline through which the naming reached bestiary. A curated alias
//     claim layered on the models.dev catalog carries Source = DataSourceModelsDev
//     even though its SourceURL points at the asserting lab: a *who reported it* and
//     a *which source we read it from* are different provenance levels.
//
// An Entity HAS-MANY Nomina; the canonical key IS the entity's Preferred nomen
// (scheme NomenSchemeCanonical). Designation (designation.go) is the READ PROJECTION
// of the same underlying facts — a Nomen recorded at the entity level, a Designation
// rendered at the ref level.
type Nomen struct {
	// Value is the naming string (a raw model ID, a canonical key, an alias spelling).
	Value string
	// Scheme classifies the KIND of naming (provenance classifier, not a render codec).
	Scheme NomenScheme
	// Status is the ISO-1087 acceptability rating. Canonical nomina are Preferred;
	// provider-ID and alias nomina are Admitted.
	Status AcceptabilityRating
	// ResolvesTo is the entity this naming denotes. One spelling may resolve to
	// several entities (homonymy); NomenLookup returns all matches.
	ResolvesTo EntityRef
	// SourceURL is WHO asserts this naming (claim attribution). Empty for
	// bestiary-minted nomina.
	//
	// Curated-claims archive policy: every SourceURL carried by a claim in
	// parse/data/nomen_claims.json is an archive.org snapshot URL
	// (https://web.archive.org/web/<timestamp>/<original-url>) captured when the
	// claim was created, never the live claimant page. A claim is evidence of what
	// a lab published, and model cards and docs pages are edited and deleted
	// without notice — a live URL quietly stops attesting the claim it was cited
	// for. There is deliberately NO second "archive_url" field: the snapshot URL
	// embeds the original claimant URL verbatim in its tail, so the live address
	// stays recoverable from the value itself.
	//
	// The policy binds the CURATED claims layer. It says nothing about SourceURLs
	// that arrive through an upstream ingest (e.g. BenchmarkResult.SourceURL from
	// the models.dev catalog), which are recorded exactly as upstream published
	// them.
	SourceURL string
	// Source is WHICH ingest we read this naming from (a DataSourceID FK).
	Source DataSourceID
}

// -----------------------------------------------------------------------------
// Minting — the single shared production function (interpretation ratified by the
// supervisor): codegen (census assertion), sync (store persistence), and the
// runtime projections (Entity.Nomina / NomenLookup) all mint through THIS code so
// they can never disagree. Nothing is baked to a *_gen.go: the provider-ID and
// canonical nomina are fully derivable from the entity index already baked in
// models_static_gen.go, and the alias nomina come from the embedded, graceful-degrade
// nomen_claims.json.
// -----------------------------------------------------------------------------

// mintEntityNomina mints the nomina intrinsic to a single entity: one Canonical
// nomen (the entity key, Preferred) plus one ProviderID nomen per DISTINCT instance
// ID spelling (Admitted), deduplicated within the entity. It does NOT include alias
// claims — those are folded in by the callers that have the claim table. The result
// is deterministically sorted (lessNomen). The entity's own Sources drive the
// per-nomen Source: a nomen's Source is the ingest that attests the entity
// (DataSourceModelsDev for every registry entity).
//
// It reads the curated redundant-modifier suppression seed through the shared
// mintEntityNominaWith seam so the production path and the fences exercise ONE
// implementation of the policy.
func mintEntityNomina(e Entity) []Nomen {
	return mintEntityNominaWith(e, loadSuppressionSafe())
}

// mintEntityNominaWith is the suppression-aware minting core: the single place the
// redundant-modifier naming policy is applied to the canonical nomina. The table is a
// parameter (dependency injection) rather than a package global read inline, so a
// fence can drive the SAME production function over a synthetic seed — there is no
// test-only mint path.
//
// The policy, in full:
//   - The PREFERRED canonical nomen's value is PreferredNomenValue(e.Ref) — the key
//     with every seed-suppressed modifier omitted. With no applicable entry this is
//     byte-identically e.Ref.String().
//   - When suppression applies, the KEY spelling is ALSO minted, as an ADMITTED
//     canonical nomen: the fuller spelling is recorded, resolvable, never dropped.
//   - ResolvesTo is e.Ref in both cases: the entity key is untouched.
func mintEntityNominaWith(e Entity, suppression *suppressionTable) []Nomen {
	src := entitySourceForNomen(e)
	out := make([]Nomen, 0, len(e.Instances)+2)

	// Canonical nomina: the preferred naming (key minus any redundant modifier) is the
	// Preferred designation; when they differ, the key spelling stays Admitted.
	key := e.Ref.String()
	preferred := preferredNomenValueWith(e.Ref, suppression)
	out = append(out, Nomen{
		Value:      preferred,
		Scheme:     NomenSchemeCanonical,
		Status:     AcceptabilityPreferred,
		ResolvesTo: e.Ref,
		Source:     src,
	})
	if preferred != key {
		out = append(out, Nomen{
			Value:      key,
			Scheme:     NomenSchemeCanonical,
			Status:     AcceptabilityAdmitted,
			ResolvesTo: e.Ref,
			Source:     src,
		})
	}

	// ProviderID nomina: every distinct instance ID spelling is an Admitted naming.
	// Deduped by the raw ID within this entity (the same ID served by two providers
	// resolves to the same entity → one nomen; the PK (value,scheme,entity_key) makes
	// this idempotent at the store as well).
	seen := make(map[string]struct{}, len(e.Instances))
	for _, inst := range e.Instances {
		id := string(inst.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, Nomen{
			Value:      id,
			Scheme:     NomenSchemeProviderID,
			Status:     AcceptabilityAdmitted,
			ResolvesTo: e.Ref,
			Source:     src,
		})
	}

	sortNomina(out)
	return out
}

// entitySourceForNomen picks the ingest DataSourceID a minted nomen is attributed
// to: the models.dev origin every registry entity attests. It reads the entity's
// derived Sources projection and prefers DataSourceModelsDev (always present for a
// registry entity); it falls back to the first source, or DataSourceModelsDev when
// the entity carries none (a hand-built value).
func entitySourceForNomen(e Entity) DataSourceID {
	for _, s := range e.Sources {
		if s == DataSourceModelsDev {
			return DataSourceModelsDev
		}
	}
	if len(e.Sources) > 0 {
		return e.Sources[0]
	}
	return DataSourceModelsDev
}

// Nomina returns the nomina for THIS entity: its Canonical (Preferred) nomen, one
// ProviderID (Admitted) nomen per distinct instance ID spelling, and any curated
// alias claim (nomen_claims.json) that resolves to this entity's key. The slice is a
// sorted, derived projection (deterministic order via lessNomen). It is safe to
// mutate — every element is a value and the slice is freshly built.
func (e Entity) Nomina() []Nomen {
	out := mintEntityNomina(e)
	key := e.Ref.String()
	for _, c := range loadNomenClaimsSafe().claims {
		if c.ResolvesTo.String() == key {
			out = append(out, c)
		}
	}
	sortNomina(out)
	return out
}

// MintNomina mints the FULL nomen set over the given entities plus every curated
// alias claim: the union of each entity's mintEntityNomina and the claim table. It
// is the shared production entry point for codegen (census pins), sync (store
// persistence), and NomenLookup's index. The result is deterministically sorted
// (INV3: the explicit sort makes the output order independent of entity/instance
// iteration order even though nothing is emitted to a *_gen.go). A claim whose
// ResolvesTo names an entity absent from the input is STILL included (a curated
// alias is authoritative curation, not gated on catalog presence) — the same
// keep-never-drop discipline the lineage synthetic-fixture flag uses.
func MintNomina(entities []Entity) []Nomen {
	var out []Nomen
	for _, e := range entities {
		out = append(out, mintEntityNomina(e)...)
	}
	out = append(out, loadNomenClaimsSafe().claims...)
	sortNomina(out)
	return out
}

// MintNominaFromModels mints the full nomen set directly from a flat []ModelInfo —
// the SHARED DECODE JOINT for the sync path, which holds freshly-fetched models but
// has not built the registry index. It groups models into entities by the SAME
// identity-class EntityRef key the registry aggregate uses (EntityModifiers
// projection), mints one Canonical (Preferred) nomen per entity key and one
// ProviderID (Admitted) nomen per distinct instance ID spelling within that entity,
// then folds in the curated alias claims. The result is deterministically sorted
// (INV3). It agrees row-for-row with MintNomina(Entities()) over the same models, so
// codegen/sync/cache stay in lockstep: they mint through equivalent joints.
func MintNominaFromModels(models []ModelInfo) []Nomen {
	type group struct {
		ref    EntityRef
		ids    []string
		idSeen map[string]struct{}
	}
	groups := make(map[string]*group)
	var order []string
	for i := range models {
		m := models[i]
		ref := EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: m.ParamSize,
			Modifier:  EntityModifiers(m.Modifier, m.Family),
		}
		key := ref.String()
		g := groups[key]
		if g == nil {
			g = &group{ref: ref, idSeen: make(map[string]struct{})}
			groups[key] = g
			order = append(order, key)
		}
		id := string(m.ID)
		if id == "" {
			continue
		}
		if _, dup := g.idSeen[id]; dup {
			continue
		}
		g.idSeen[id] = struct{}{}
		g.ids = append(g.ids, id)
	}

	suppression := loadSuppressionSafe()
	var out []Nomen
	for _, key := range order {
		g := groups[key]
		// Same suppression policy as mintEntityNominaWith: preferred value omits any
		// seed-suppressed modifier, the key spelling stays admitted when they differ.
		preferred := preferredNomenValueWith(g.ref, suppression)
		out = append(out, Nomen{
			Value:      preferred,
			Scheme:     NomenSchemeCanonical,
			Status:     AcceptabilityPreferred,
			ResolvesTo: g.ref,
			Source:     DataSourceModelsDev,
		})
		if preferred != key {
			out = append(out, Nomen{
				Value:      key,
				Scheme:     NomenSchemeCanonical,
				Status:     AcceptabilityAdmitted,
				ResolvesTo: g.ref,
				Source:     DataSourceModelsDev,
			})
		}
		for _, id := range g.ids {
			out = append(out, Nomen{
				Value:      id,
				Scheme:     NomenSchemeProviderID,
				Status:     AcceptabilityAdmitted,
				ResolvesTo: g.ref,
				Source:     DataSourceModelsDev,
			})
		}
	}
	out = append(out, loadNomenClaimsSafe().claims...)
	sortNomina(out)
	return out
}

// Nomina (package-level) mints the full nomen set over the static registry entity
// index — the runtime convenience wrapper for MintNomina(Entities()). It is what the
// sync path and NomenLookup build on.
func Nomina() []Nomen {
	return MintNomina(Entities())
}

// NomenLookup returns EVERY nomen whose Value equals value, and whether at least one
// exists. It returns ALL matches — a single spelling may resolve to several distinct
// entities (homonymy: the ErrAmbiguous reality), so a scalar "the nomen" would be a
// lie. The HOMONYMY POSITIVE FENCE rests on this: N persisted rows for one spelling
// yield N results here. The comparison is exact (case-sensitive) on the raw value.
func NomenLookup(value string) ([]Nomen, bool) {
	idx := nomenLookupIndex()
	matches, ok := idx[value]
	if !ok {
		return nil, false
	}
	out := append([]Nomen(nil), matches...)
	return out, true
}

// sortNomina imposes the deterministic total order used everywhere nomina are
// emitted (INV3). It sorts in place by (Value, Scheme, resolved entity key), which
// is a total order because the PK triple (value, scheme, entity_key) is unique.
func sortNomina(ns []Nomen) {
	sort.Slice(ns, func(i, j int) bool { return lessNomen(ns[i], ns[j]) })
}

// lessNomen is the strict weak ordering behind sortNomina: (Value, then Scheme int,
// then the resolved entity key). It orders on exactly the PK columns so equal-PK
// rows compare equal (neither less-than) and the sort is stable across bakes.
func lessNomen(a, b Nomen) bool {
	if a.Value != b.Value {
		return a.Value < b.Value
	}
	if a.Scheme != b.Scheme {
		return a.Scheme < b.Scheme
	}
	return a.ResolvesTo.String() < b.ResolvesTo.String()
}
