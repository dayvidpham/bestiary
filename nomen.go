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
	// claim attribution (a NomenAttestation whose SourceURL is who asserted it) and
	// is ADMITTED.
	NomenSchemeAlias
	// NomenSchemeOCI is a `pkg:oci` purl naming minted per digest-bearing Ollama quant
	// row (the content-addressed manifest digest is what makes each OCI purl unique).
	// APPENDED to the iota tail (wire stability). It is ADMITTED and carries an Ollama
	// Harvested/Secondary attestation. A digest rotates on any re-publish
	// (requantization / template fixes), not only a new release, and the old-digest
	// OCI nomen PERSISTS — names are never erased.
	NomenSchemeOCI
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
	NomenSchemeOCI:         "oci",
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

// AttestationAuthority records WHOSE VOICE an attestation's evidence document is:
// the namespace owner/creator speaking for itself (Primary) or an aggregator
// relaying it (Secondary). It is a per-attestation fact — the same name can be
// attested Primary by its creator and Secondary by an aggregator, so the axis
// cannot live on the DataSource dimension.
//
// It follows the ELEMENT-enum convention (the NomenScheme / Quantization
// precedent): every attestation arrives with a value, so AuthorityUnknown sits AT
// zero as the fail-safe (never a "none" sentinel).
type AttestationAuthority int

const (
	// AuthorityUnknown is the fail-safe bucket (zero value): an attestation whose
	// voice is not yet recognized.
	AuthorityUnknown AttestationAuthority = iota
	// AuthorityPrimary is the namespace owner or creator speaking for itself (the
	// Hub is authoritative for the huggingface scheme; bestiary is authoritative
	// for its own canonical scheme).
	AuthorityPrimary
	// AuthoritySecondary is an aggregator or relayer reporting the naming (a
	// catalog's raw-ID spelling, an Ollama tag).
	AuthoritySecondary
)

// attestationAuthorityNames is the single source of truth for String / MarshalText
// / UnmarshalText, indexed by the AttestationAuthority int value. The tokens are the
// stable lowercase wire names.
var attestationAuthorityNames = []string{
	AuthorityUnknown:   "unknown",
	AuthorityPrimary:   "primary",
	AuthoritySecondary: "secondary",
}

// String renders the AttestationAuthority as its lowercase wire token. An
// out-of-range value degrades to "unknown" (never a panic).
func (a AttestationAuthority) String() string {
	if int(a) < 0 || int(a) >= len(attestationAuthorityNames) {
		return "unknown"
	}
	return attestationAuthorityNames[a]
}

// MarshalText implements encoding.TextMarshaler (the NomenScheme precedent): the
// enum serializes as its lowercase token, never its underlying int. It never errors.
func (a AttestationAuthority) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of MarshalText. An
// unrecognized token is an actionable error (the NomenScheme precedent), never a
// silent AuthorityUnknown.
func (a *AttestationAuthority) UnmarshalText(text []byte) error {
	lower := strings.ToLower(strings.TrimSpace(string(text)))
	for i, name := range attestationAuthorityNames {
		if name == lower {
			*a = AttestationAuthority(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal AttestationAuthority from %q\n"+
			"  What: the token does not match any known authority\n"+
			"  Why: only %v are accepted\n"+
			"  Where: bestiary.AttestationAuthority.UnmarshalText (nomen.go)\n"+
			"  How to fix: use one of the accepted authority tokens",
		string(text), attestationAuthorityNames,
	)
}

// IngestMethod records HOW a naming record entered the system: from a committed
// curated seed (Curated), bot-fetched from an external registry (Harvested), or
// bestiary-authored (SelfMinted). It is a per-attestation fact symmetric with
// Authority — models.dev is simultaneously the Source of self-minted canonical
// nomina AND of harvested provider-id nomina, so the axis cannot live on the
// DataSource dimension.
//
// Element-enum convention: IngestMethodUnknown sits AT zero as the fail-safe.
type IngestMethod int

const (
	// IngestMethodUnknown is the fail-safe bucket (zero value).
	IngestMethodUnknown IngestMethod = iota
	// IngestMethodCurated is a naming from a committed curated seed (nomen_claims.json).
	IngestMethodCurated
	// IngestMethodHarvested is a naming bot-fetched from an external registry
	// (models.dev / HuggingFace / Ollama).
	IngestMethodHarvested
	// IngestMethodSelfMinted is a bestiary-authored naming (the canonical key, a
	// purl derivation).
	IngestMethodSelfMinted
)

// ingestMethodNames is the single source of truth for String / MarshalText /
// UnmarshalText, indexed by the IngestMethod int value.
var ingestMethodNames = []string{
	IngestMethodUnknown:    "unknown",
	IngestMethodCurated:    "curated",
	IngestMethodHarvested:  "harvested",
	IngestMethodSelfMinted: "self-minted",
}

// String renders the IngestMethod as its lowercase wire token. An out-of-range
// value degrades to "unknown" (never a panic).
func (m IngestMethod) String() string {
	if int(m) < 0 || int(m) >= len(ingestMethodNames) {
		return "unknown"
	}
	return ingestMethodNames[m]
}

// MarshalText implements encoding.TextMarshaler (the NomenScheme precedent).
func (m IngestMethod) MarshalText() ([]byte, error) {
	return []byte(m.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, the inverse of MarshalText. An
// unrecognized token is an actionable error, never a silent IngestMethodUnknown.
func (m *IngestMethod) UnmarshalText(text []byte) error {
	lower := strings.ToLower(strings.TrimSpace(string(text)))
	for i, name := range ingestMethodNames {
		if name == lower {
			*m = IngestMethod(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal IngestMethod from %q\n"+
			"  What: the token does not match any known ingest method\n"+
			"  Why: only %v are accepted\n"+
			"  Where: bestiary.IngestMethod.UnmarshalText (nomen.go)\n"+
			"  How to fix: use one of the accepted ingest-method tokens",
		string(text), ingestMethodNames,
	)
}

// NomenAttestation is one independent piece of evidence for a Nomen (the Wikidata
// per-claim-reference model). Each attestation stands on its own; two sources
// asserting the same name append two attestations, never overwrite. A Nomen HAS-MANY
// attestations — the v0.2.8 multi-attestation lift of the former fused
// SourceURL+Source columns.
//
// The two provenance fields must NEVER be conflated (the BenchmarkResult
// SourceURL-vs-Source discipline):
//   - SourceURL is WHO ASSERTS the naming (claim attribution) — e.g. the xAI model
//     page that declares "grok-beta" as an alias. Empty for bestiary-self-minted
//     nomina (provider-ID spellings and canonical keys assert themselves).
//   - Source is WHICH INGEST we read the naming from (a DataSourceID FK) — the
//     catalog/pipeline through which the naming reached bestiary.
//
// Curated-claims archive policy: a SourceURL carried by a claim in
// parse/data/nomen_claims.json is an archive.org snapshot URL captured when the
// claim was created, never the live claimant page (see nomen_claims.go). The policy
// binds the CURATED claims layer only.
type NomenAttestation struct {
	// SourceURL is WHO asserts this naming (claim attribution). "" for self-minted.
	SourceURL string
	// Source is WHICH ingest we read this naming from (a DataSourceID FK).
	Source DataSourceID
	// Authority is whose VOICE the evidence document is (per-attestation).
	Authority AttestationAuthority
	// Method is HOW this record entered the system (per-attestation).
	Method IngestMethod
	// IngestedAt is a committed snapshot RFC3339 timestamp; "" honest when unknown.
	IngestedAt string
}

// Nomen is one recorded NAMING of an entity: a value string classified by scheme,
// carrying its acceptability status, the entity it resolves to, and — the v0.2.8
// multi-attestation lift — a set of independent attestations (the evidence set). A
// name HAS-MANY attestations: provenance is no longer FUSED into the name row, so
// two sources asserting the same name append two attestations rather than
// conflicting. Status remains bestiary's single editorial judgment, exactly once per
// name.
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
	// provider-ID and alias nomina are Admitted. It is ONE editorial judgment per
	// name — a same-triple Status disagreement is a LOUD codegen conflict.
	Status AcceptabilityRating
	// ResolvesTo is the entity this naming denotes. One spelling may resolve to
	// several entities (homonymy); NomenLookup returns all matches.
	ResolvesTo EntityRef
	// Attestations is the evidence set (>=1): each element is an independent
	// assertion of this naming. coalesceNomina unions same-triple attestations from
	// distinct sources; the slice is kept deterministically sorted (lessAttestation).
	Attestations []NomenAttestation
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

// canonicalAttestation is the single self-minted attestation a canonical nomen
// carries (§3.2 defaults table): bestiary is the Primary authority for its own
// canonical scheme, and the key is bestiary-authored, so Method is SelfMinted. The
// SourceURL is empty (a self-minted key asserts itself); Source is the ingest that
// attests the entity.
func canonicalAttestation(src DataSourceID) []NomenAttestation {
	return []NomenAttestation{{
		Source:    src,
		Authority: AuthorityPrimary,
		Method:    IngestMethodSelfMinted,
	}}
}

// providerIDAttestation is the single attestation a provider-id nomen carries (§3.2
// defaults table): a raw catalog spelling is an aggregator's voice (Secondary),
// bot-fetched from the catalog (Harvested). SourceURL is empty; Source is the ingest.
func providerIDAttestation(src DataSourceID) []NomenAttestation {
	return []NomenAttestation{{
		Source:    src,
		Authority: AuthoritySecondary,
		Method:    IngestMethodHarvested,
	}}
}

// ociAttestation is the single attestation an OCI nomen carries (§3.2 defaults table):
// the digest was bot-harvested from the Ollama registry (Source ollama, Method
// Harvested), and Ollama is an aggregator/distributor relaying an upstream model — its
// voice is Secondary, not the model creator's. sourceURL is the Ollama claimant page
// for the naming (WHO asserts).
func ociAttestation(sourceURL string) []NomenAttestation {
	return []NomenAttestation{{
		SourceURL: sourceURL,
		Source:    DataSourceOllama,
		Authority: AuthoritySecondary,
		Method:    IngestMethodHarvested,
	}}
}

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
		Value:        preferred,
		Scheme:       NomenSchemeCanonical,
		Status:       AcceptabilityPreferred,
		ResolvesTo:   e.Ref,
		Attestations: canonicalAttestation(src),
	})
	if preferred != key {
		out = append(out, Nomen{
			Value:        key,
			Scheme:       NomenSchemeCanonical,
			Status:       AcceptabilityAdmitted,
			ResolvesTo:   e.Ref,
			Attestations: canonicalAttestation(src),
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
			Value:        id,
			Scheme:       NomenSchemeProviderID,
			Status:       AcceptabilityAdmitted,
			ResolvesTo:   e.Ref,
			Attestations: providerIDAttestation(src),
		})
	}

	// OCI nomina: one per DIGEST-BEARING quant row across this entity's instances. The
	// content-addressed manifest digest is what makes a `pkg:oci` purl uniquely name an
	// artifact, so a row with no OCIDigest yields NO nomen (OCIPurl returns "" and is
	// skipped). Deduped by purl value within the entity (two instances of the same
	// Ollama artifact share the same digest → one nomen). The tag qualifier is OMITTED
	// at this altitude: the full Ollama tag is not reconstructable from a quant row
	// alone, and the digest already makes the purl unique, so an omitted tag is honest
	// rather than a partial-and-misleading one. Registry is the fixed Ollama library
	// namespace. On shipped data ZERO rows carry a digest, so this emits nothing today;
	// the set grows only with the next deliberate cmd/bestiary-ollama refresh.
	ociSeen := make(map[string]struct{})
	for _, inst := range e.Instances {
		name := string(inst.ID)
		for _, q := range inst.QuantVRAM {
			purl := q.OCIPurl(name, "", ociOllamaRegistry)
			if purl == "" {
				continue
			}
			if _, dup := ociSeen[purl]; dup {
				continue
			}
			ociSeen[purl] = struct{}{}
			out = append(out, Nomen{
				Value:        purl,
				Scheme:       NomenSchemeOCI,
				Status:       AcceptabilityAdmitted,
				ResolvesTo:   e.Ref,
				Attestations: ociAttestation(ociSourceURL(name)),
			})
		}
	}

	sortNomina(out)
	return out
}

// ociSourceURL builds the Ollama claimant page URL for an OCI nomen's attestation
// (WHO asserts the naming): the ollama.com library page for the repository. name is
// reduced to its lowercased last path fragment to match the purl name and the Ollama
// library slug (e.g. "ollama/llama3.1" → "llama3.1").
func ociSourceURL(name string) string {
	n := strings.ToLower(name)
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		n = n[i+1:]
	}
	if n == "" {
		return ""
	}
	return "https://ollama.com/library/" + n
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
	// Fold in any harvested HuggingFace nomen resolving to this entity (the same
	// keep-never-drop fold as the curated claims; the harvested seed is a separate
	// layer — see huggingface_nomina.go). coalesceNomina (applied by the callers
	// building the full set) unions a curated + harvested same-triple HF name; at
	// the single-entity projection level a duplicate triple simply coalesces.
	for _, c := range hfNominaClaims() {
		if c.ResolvesTo.String() == key {
			out = append(out, c)
		}
	}
	out = coalesceNominaOrRaw(out)
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
	out = append(out, hfNominaClaims()...)
	return coalesceNominaOrRaw(out)
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
		ref     EntityRef
		ids     []string
		idSeen  map[string]struct{}
		oci     []Nomen             // OCI nomina (one per digest-bearing quant row)
		ociSeen map[string]struct{} // dedup by purl value within the entity
	}
	// Raw key set for the MERGE-only N->N.0 fold (the SAME shared primitive the registry
	// and codegen use, NormalizeEntityVersion), so the from-models nomina grouping folds a
	// bare-N spelling onto its dotted sibling exactly as Entities() does — otherwise the
	// from-models and from-entities canonical censuses would diverge by the merged pairs.
	rawKeys := make(map[string]struct{}, len(models))
	for i := range models {
		m := models[i]
		ref := EntityRef{
			Family:    m.Family,
			Variant:   m.Variant,
			Version:   m.Version,
			ParamSize: m.ParamSize,
			Modifier:  EntityModifiers(m.Modifier, m.Family),
		}
		rawKeys[ref.String()] = struct{}{}
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
		if dotted, merged := NormalizeEntityVersion(ref, rawKeys); merged {
			ref = dotted
		}
		key := ref.String()
		g := groups[key]
		if g == nil {
			g = &group{ref: ref, idSeen: make(map[string]struct{}), ociSeen: make(map[string]struct{})}
			groups[key] = g
			order = append(order, key)
		}
		// OCI nomina from this model's digest-bearing quant rows (parity with
		// mintEntityNominaWith: the digest is the fetch-owned field the entity-level
		// mint also reads). Deduped by purl value within the entity; skipped when the
		// row carries no digest. models.dev-sourced sync models never carry a digest,
		// so this fires only over baked models that harvested one from an Ollama refresh.
		for _, q := range m.QuantVRAM {
			purl := q.OCIPurl(string(m.ID), "", ociOllamaRegistry)
			if purl == "" {
				continue
			}
			if _, dup := g.ociSeen[purl]; dup {
				continue
			}
			g.ociSeen[purl] = struct{}{}
			g.oci = append(g.oci, Nomen{
				Value:        purl,
				Scheme:       NomenSchemeOCI,
				Status:       AcceptabilityAdmitted,
				ResolvesTo:   g.ref,
				Attestations: ociAttestation(ociSourceURL(string(m.ID))),
			})
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
			Value:        preferred,
			Scheme:       NomenSchemeCanonical,
			Status:       AcceptabilityPreferred,
			ResolvesTo:   g.ref,
			Attestations: canonicalAttestation(DataSourceModelsDev),
		})
		if preferred != key {
			out = append(out, Nomen{
				Value:        key,
				Scheme:       NomenSchemeCanonical,
				Status:       AcceptabilityAdmitted,
				ResolvesTo:   g.ref,
				Attestations: canonicalAttestation(DataSourceModelsDev),
			})
		}
		for _, id := range g.ids {
			out = append(out, Nomen{
				Value:        id,
				Scheme:       NomenSchemeProviderID,
				Status:       AcceptabilityAdmitted,
				ResolvesTo:   g.ref,
				Attestations: providerIDAttestation(DataSourceModelsDev),
			})
		}
		out = append(out, g.oci...)
	}
	out = append(out, loadNomenClaimsSafe().claims...)
	out = append(out, hfNominaClaims()...)
	return coalesceNominaOrRaw(out)
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

// lessAttestation is the TOTAL strict weak ordering over a Nomen's attestation set:
// (Source, SourceURL, Authority, Method, IngestedAt) — EVERY field of
// NomenAttestation. Totality is load-bearing for determinism (INV3): a sort key that
// omitted IngestedAt (or any field) would leave two attestations equal on the
// compared fields yet not byte-identical, so they would neither dedup nor order
// stably — a nondeterministic map-group fallback that breaks N=100. With every field
// in the key, equal-key ⇒ byte-identical ⇒ deduped.
func lessAttestation(a, b NomenAttestation) bool {
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.SourceURL != b.SourceURL {
		return a.SourceURL < b.SourceURL
	}
	if a.Authority != b.Authority {
		return a.Authority < b.Authority
	}
	if a.Method != b.Method {
		return a.Method < b.Method
	}
	return a.IngestedAt < b.IngestedAt
}

// sortAndDedupAttestations sorts an attestation slice by the TOTAL lessAttestation
// key and removes byte-identical duplicates (adjacent after the sort, since the key
// is every field). It mutates and returns the slice.
func sortAndDedupAttestations(as []NomenAttestation) []NomenAttestation {
	sort.Slice(as, func(i, j int) bool { return lessAttestation(as[i], as[j]) })
	out := as[:0]
	for i, a := range as {
		if i > 0 && a == as[i-1] {
			continue // exact duplicate (all five fields equal) — idempotent no-op
		}
		out = append(out, a)
	}
	return out
}

// coalesceNomina groups nomina by the triple (Value, Scheme, ResolvesTo) and UNIONS
// each group's attestation sets into ONE Nomen per triple — the v0.2.8 multi-
// attestation lift: two sources asserting the same name append their attestations
// rather than conflicting. Within a group the union is deduped (byte-identical
// attestations collapse) and sorted by the TOTAL lessAttestation key, so the output
// is deterministic (INV3) regardless of input order.
//
// A group whose members disagree on Status is a LOUD conflict: Status is bestiary's
// single editorial judgment per name, so a same-triple Status disagreement is a
// curation error that must never be resolved by last-write-wins. Differing attesters
// (SourceURL/Source/Authority/Method) are NOT a conflict — that is exactly the union
// the lift enables.
//
// It is pure: it deep-copies each group's attestation slice and never mutates the
// input nomina.
func coalesceNomina(nomina []Nomen) ([]Nomen, error) {
	type tripleKey struct {
		value  string
		scheme NomenScheme
		entity string
	}
	groups := make(map[tripleKey]*Nomen, len(nomina))
	for i := range nomina {
		n := nomina[i]
		k := tripleKey{value: n.Value, scheme: n.Scheme, entity: n.ResolvesTo.String()}
		g, ok := groups[k]
		if !ok {
			cp := n
			cp.Attestations = append([]NomenAttestation(nil), n.Attestations...)
			groups[k] = &cp
			continue
		}
		if g.Status != n.Status {
			return nil, fmt.Errorf(
				"bestiary nomen: same-triple Status conflict\n"+
					"  What: two nomina share the PK triple (value=%q, scheme=%q, entity_key=%q) but disagree on Status\n"+
					"  First:  status=%q\n"+
					"  Second: status=%q\n"+
					"  Where: coalesceNomina over the minted nomen set (nomen_claims.json curation and/or the minted provider-ID/canonical set)\n"+
					"  Why: Status is bestiary's single editorial judgment per name; a same-triple disagreement would be resolved by last-write-wins, silently losing a verdict\n"+
					"  How to fix: reconcile the conflicting Status in parse/data/nomen_claims.json, or split the claim onto a distinct entity",
				k.value, k.scheme.String(), k.entity,
				g.Status.String(), n.Status.String(),
			)
		}
		g.Attestations = append(g.Attestations, n.Attestations...)
	}

	out := make([]Nomen, 0, len(groups))
	for _, g := range groups {
		g.Attestations = sortAndDedupAttestations(g.Attestations)
		out = append(out, *g)
	}
	sortNomina(out)
	return out, nil
}

// coalesceNominaOrRaw is the mint-path wrapper: it coalesces, and on a Status
// conflict — a curation bug the codegen ValidateNomina guard catches LOUD — it
// degrades at RUNTIME to the raw sorted set rather than panicking or dropping
// nomina (the loadNomenClaimsSafe never-nil/never-panic discipline). The conflict
// path is unreachable on shipped data, which ValidateNomina fences at bake time.
func coalesceNominaOrRaw(raw []Nomen) []Nomen {
	coalesced, err := coalesceNomina(raw)
	if err != nil {
		sortNomina(raw)
		return raw
	}
	return coalesced
}
