package bestiary_test

// Fences for canonical segment binding at the peasant seam.
//
// The defect the repair closes: a canonical ref is bound to (family, variant,
// version) BY SLOT POSITION, and a provider prefix is consumed by shifting the
// segment slice left. The residue is then re-read positionally with no memory of
// which slot the un-stripped form implied, so a trailing version token lands in
// the VARIANT slot and can never match a variant-empty entity. `ling/2.6` and
// `ant/ling/2.6` were both model-not-found for exactly that reason, and a
// variant-empty ref could not be addressed with a provider prefix at all.
//
// Every repair rule lives in a second pass that runs only when the first pass
// matched nothing ANYWHERE in the registry, so a ref that already resolved is
// untouched by construction — which is why the six must-not-widen rows below come
// back byte-identical rather than merely "still resolving".

import (
	_ "embed"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/resolve/segment_binding_corpus.json
var resolveSegmentBindingCorpusJSON []byte

// segmentBindingInput is one probed ref plus the role that decides which domain
// precondition the runner enforces before believing the row.
type segmentBindingInput struct {
	Ref  string `json:"ref"`
	Role string `json:"role"`
}

// The closed set of corpus roles. A row carrying anything else is a corpus
// authoring error and fails loudly rather than skipping its precondition.
const (
	roleRepair             = "repair"
	roleMustNotWiden       = "must-not-widen"
	roleEntityViewGuard    = "entity-view-guard"
	roleCompositionWitness = "composition-witness"
)

// segmentBindingExpected pins the row's CANDIDATE SET, not merely its outcome
// class: Entities is the sorted set of entity keys the candidates span (the
// identity-level candidate set), and Refs the sorted provider|id set. Both are
// comma-joined plain strings so the corpus stays inside the value-based coverage
// guard, which needs a comparable expected type.
type segmentBindingExpected struct {
	Outcome  string `json:"outcome"`
	Entities string `json:"entities"`
	Refs     string `json:"refs"`
}

const (
	outcomeResolved  = "resolved"
	outcomeAmbiguous = "ambiguous"
	outcomeNotFound  = "not-found"
)

// entityKeyForRef projects a ModelRef onto the entity key it belongs to, through
// the same exported EntityRef/EntityModifiers construction the CLI uses, so the
// pinned candidate sets are stated in the identity vocabulary the acceptance
// criterion is written in rather than in raw ids.
func entityKeyForRef(r bestiary.ModelRef) string {
	return bestiary.EntityRef{
		Family:    r.Family,
		Variant:   r.Variant,
		Version:   r.Version,
		ParamSize: r.ParamSize,
		Modifier:  bestiary.EntityModifiers(r.Modifier, r.Family),
	}.String()
}

// joinSortedSet renders a set as the sorted comma-joined form the corpus pins.
func joinSortedSet(set map[string]bool) string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

// segmentBindingObserved is what one peasant-seam resolution actually produced.
type segmentBindingObserved struct {
	outcome  string
	refs     []bestiary.ModelRef
	entities string
	refSet   string
}

// resolveAtPeasantSeam drives the production entry point in the configuration
// `bestiary show <ref>` uses. An ErrAmbiguous is not an error case here: its
// candidate list IS the observation the corpus pins.
func resolveAtPeasantSeam(ref string) segmentBindingObserved {
	refs, err := bestiary.Resolve(ref, bestiary.WithInputFormat(bestiary.InputFormatPeasant))
	obs := segmentBindingObserved{outcome: outcomeResolved}
	var ambig *bestiary.ErrAmbiguous
	switch {
	case errors.As(err, &ambig):
		obs.outcome, refs = outcomeAmbiguous, ambig.Candidates
	case err != nil:
		obs.outcome, refs = outcomeNotFound, nil
	}
	obs.refs = refs
	ents, rs := map[string]bool{}, map[string]bool{}
	for _, r := range refs {
		ents[entityKeyForRef(r)] = true
		rs[string(r.Provider)+"|"+string(r.ID)] = true
	}
	obs.entities, obs.refSet = joinSortedSet(ents), joinSortedSet(rs)
	return obs
}

// entityKeyIsLive reports whether key names an entity in the built registry.
func entityKeyIsLive(key string) bool {
	for _, e := range bestiary.Entities() {
		if e.Ref.String() == key {
			return true
		}
	}
	return false
}

// entityKeysAtFamilyVersion counts the distinct entity identities in the built
// catalog sharing one (family, version). It is the independent measurement the
// repair rows are checked against: a repaired ref whose (family, version) names
// only ONE identity had nothing to mis-bind to, so its row would pass vacuously.
func entityKeysAtFamilyVersion(family bestiary.Family, version string) []string {
	var out []string
	for _, e := range bestiary.Entities() {
		if e.Ref.Family == family && e.Ref.Version == version {
			out = append(out, e.Ref.String())
		}
	}
	sort.Strings(out)
	return out
}

// TestResolve_SegmentBinding_Corpus drives every corpus row through the
// production Resolve entry point at the peasant seam — the exact configuration
// `bestiary show <ref>` passes — and asserts the pinned candidate set.
//
// Driving this at a bare Resolve() instead would be vacuous: auto-detect routes
// five of the six must-not-widen refs to a non-canonical scheme where they are
// NOTFOUND, so the pins would record NOTFOUND -> NOTFOUND and could not see a
// widening at all.
func TestResolve_SegmentBinding_Corpus(t *testing.T) {
	corpus := loadParseCorpus[segmentBindingInput, segmentBindingExpected](t, resolveSegmentBindingCorpusJSON, 18)

	requireInputCoverage(t, corpus, map[segmentBindingInput]segmentBindingExpected{
		// the amended criterion's unique-resolution arm, both spellings, pinned by value
		{Ref: "ling/2.6", Role: roleRepair}:     {Outcome: outcomeResolved, Entities: "ling@2.6#1t"},
		{Ref: "ant/ling/2.6", Role: roleRepair}: {Outcome: outcomeResolved, Entities: "ling@2.6#1t"},
		// the scoped-ambiguity arm: the provider-equality guard is what keeps this
		// from listing every host of the family
		{Ref: "openai/gpt@5.1", Role: roleRepair}: {
			Outcome: outcomeAmbiguous, Entities: "gpt@5.1,gpt@5.1{chat}",
			Refs: "openai|gpt-5.1,openai|gpt-5.1-chat-latest",
		},
		// one must-not-widen falsifier, with its ref set
		{Ref: "mistral/codestral", Role: roleMustNotWiden}: {
			Outcome: outcomeResolved, Entities: "codestral", Refs: "vercel|mistral/codestral",
		},
		// one entity-view guard: this key must stay model-not-found so `show` reaches
		// its aggregate entity view
		{Ref: "llama@3.3#70b", Role: roleEntityViewGuard}: {Outcome: outcomeNotFound},
		// the composition witness: green only when the tier re-key AND the binding
		// repair have both landed, pinned by value so neither can regress silently
		{Ref: "openai/gpt/5.6", Role: roleCompositionWitness}: {
			Outcome:  outcomeAmbiguous,
			Entities: "gpt/luna@5.6,gpt/luna@5.6{pro},gpt/sol@5.6,gpt/sol@5.6{pro},gpt/terra@5.6,gpt/terra@5.6{pro}",
			Refs:     "openai|gpt-5.6,openai|gpt-5.6-luna,openai|gpt-5.6-terra,openrouter|openai/gpt-5.6-luna-pro,openrouter|openai/gpt-5.6-sol-pro,openrouter|openai/gpt-5.6-terra-pro",
		},
	})

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			obs := resolveAtPeasantSeam(c.Input.Ref)

			switch c.Input.Role {
			case roleRepair:
				// Domain precondition: the (family, version) this ref names must be
				// served by MORE THAN ONE entity identity in the current catalog.
				// Otherwise the ref had exactly one place to land and the row proves
				// nothing about binding.
				if len(obs.refs) == 0 {
					t.Fatalf("repair row %q resolved to nothing at the peasant seam; the segment-binding "+
						"repair is the whole point of this row", c.Input.Ref)
				}
				siblings := entityKeysAtFamilyVersion(obs.refs[0].Family, obs.refs[0].Version)
				if len(siblings) < 2 {
					t.Fatalf("catalog precondition lost: family %q at version %q now names %v — want >= 2 "+
						"distinct entity identities, or the ref had nothing to mis-bind to and this row "+
						"passes vacuously; re-pick the case against the current catalog",
						obs.refs[0].Family, obs.refs[0].Version, siblings)
				}
			case roleMustNotWiden:
				// Domain precondition: the row must pin its ref set, since a widening
				// that keeps the entity key but adds provider rows is invisible at the
				// identity level.
				if c.Expected.Refs == "" {
					t.Fatalf("corpus authoring error: must-not-widen row %q pins no ref set; "+
						"an identity-level pin alone cannot see a provider-level widening", c.Input.Ref)
				}
			case roleEntityViewGuard:
				// Domain precondition: the probed string must still BE a live entity
				// key. Guarding a key the registry no longer mints protects nothing.
				if !entityKeyIsLive(c.Input.Ref) {
					t.Fatalf("catalog precondition lost: %q is no longer a live entity key, so pinning it "+
						"model-not-found no longer protects an entity view; re-pick against the current catalog",
						c.Input.Ref)
				}
			case roleCompositionWitness:
				// Domain precondition 1: the composition witness must never be read as a
				// claim that a variant-empty artifact exists at this version. It is
				// measured that none does and that no upstream row would produce one; if
				// one ever appears, the row's reasoning has changed and must be revisited
				// rather than left to pass on a different fact.
				if entityKeyIsLive("gpt@5.6") {
					t.Fatalf("a base `gpt@5.6` entity now exists; this witness was authored on the " +
						"measured fact that none does and that no upstream row would produce one — " +
						"revisit the row rather than letting it pass")
				}
				// Domain precondition 2: the witness is a COMPOSITION of the tier re-key
				// and the binding repair, so it is only meaningful while the ref it probes
				// spans more than one identity. A single-identity span would mean the ref
				// had nowhere to mis-bind and the ambiguity pin would be vacuous.
				if len(obs.refs) < 2 {
					t.Fatalf("composition witness %q resolved to %d candidate(s) at the peasant seam; the "+
						"row pins a SCOPED AMBIGUITY, so fewer than two candidates means either the tier "+
						"re-key or the binding repair is absent", c.Input.Ref, len(obs.refs))
				}
			default:
				t.Fatalf("corpus authoring error: row %q carries unknown role %q", c.Name, c.Input.Role)
			}

			if obs.outcome != c.Expected.Outcome {
				t.Fatalf("Resolve(%q, peasant) outcome = %q, want %q (candidates: %s)",
					c.Input.Ref, obs.outcome, c.Expected.Outcome, obs.refSet)
			}
			if obs.entities != c.Expected.Entities {
				t.Errorf("Resolve(%q, peasant) candidate set spans entities %q, want %q",
					c.Input.Ref, obs.entities, c.Expected.Entities)
			}
			// A pinned ref set is the provider-level candidate pin. Every must-not-widen
			// row carries one, because a widening that keeps the entity key but adds
			// provider rows is invisible at the identity level; a repair row carries one
			// when the criterion names an exact candidate count (the provider-scoped
			// ambiguity), which likewise cannot be seen from the entity keys alone.
			if c.Expected.Refs != "" && obs.refSet != c.Expected.Refs {
				t.Errorf("Resolve(%q, peasant) candidate refs = %q, want %q — the row pins its candidate "+
					"set at the provider level, so a rule that widens WHICH rows come back reddens here "+
					"even when the entity keys are unchanged",
					c.Input.Ref, obs.refSet, c.Expected.Refs)
			}
		})
	}
}
