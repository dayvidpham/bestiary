package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// nominaCensus tallies a nomen slice by scheme — the shared helper for the census
// pins below.
func nominaCensus(ns []bestiary.Nomen) map[bestiary.NomenScheme]int {
	m := map[bestiary.NomenScheme]int{}
	for _, n := range ns {
		m[n.Scheme]++
	}
	return m
}

// TestNomina_CensusExact pins the EXACT per-scheme census of the minted nomen set
// over the static registry (the "census literal pinned at bake"). The counts are
// derived from the committed models_static_gen.go: 975 canonical (one Preferred nomen
// per distinct entity key), 2792 provider-ID (one Admitted nomen per distinct instance
// ID spelling, deduped within an entity), and 1 alias (the grok-beta seed claim). On a
// models.dev snapshot refresh these move consciously, like the other census pins.
func TestNomina_CensusExact(t *testing.T) {
	const (
		wantCanonical  = 975
		wantProviderID = 2792
		wantAlias      = 1
		wantTotal      = wantCanonical + wantProviderID + wantAlias
	)
	all := bestiary.MintNomina(bestiary.Entities())
	if len(all) != wantTotal {
		t.Errorf("MintNomina total = %d, want %d", len(all), wantTotal)
	}
	c := nominaCensus(all)
	if c[bestiary.NomenSchemeCanonical] != wantCanonical {
		t.Errorf("canonical nomina = %d, want %d", c[bestiary.NomenSchemeCanonical], wantCanonical)
	}
	if c[bestiary.NomenSchemeProviderID] != wantProviderID {
		t.Errorf("provider-id nomina = %d, want %d", c[bestiary.NomenSchemeProviderID], wantProviderID)
	}
	if c[bestiary.NomenSchemeAlias] != wantAlias {
		t.Errorf("alias nomina = %d, want %d", c[bestiary.NomenSchemeAlias], wantAlias)
	}
	// The registry Nomina() convenience must agree with MintNomina(Entities()).
	if got := len(bestiary.Nomina()); got != wantTotal {
		t.Errorf("Nomina() total = %d, want %d", got, wantTotal)
	}
	// The from-models joint (sync path over fetched models) agrees on the
	// instance-bearing schemes with the from-entities joint: provider-id and alias are
	// IDENTICAL. Canonical differs by exactly the 4 metadata-only standalone entities
	// (the ornith rows synthesized by the metadata join) — those have no instances in
	// StaticModels, so the from-models joint mints no canonical nomen for them. This
	// documents the ONLY divergence between the two shared joints.
	const wantFromModelsCanonical = wantCanonical - 4
	fromModels := nominaCensus(bestiary.MintNominaFromModels(bestiary.StaticModels()))
	if fromModels[bestiary.NomenSchemeProviderID] != wantProviderID ||
		fromModels[bestiary.NomenSchemeAlias] != wantAlias {
		t.Errorf("MintNominaFromModels provider-id/alias census = %v, want %d/%d", fromModels, wantProviderID, wantAlias)
	}
	if fromModels[bestiary.NomenSchemeCanonical] != wantFromModelsCanonical {
		t.Errorf("MintNominaFromModels canonical = %d, want %d (975 entities minus 4 metadata-only standalones)",
			fromModels[bestiary.NomenSchemeCanonical], wantFromModelsCanonical)
	}
}

// TestNomina_DeterministicSortedEmission verifies INV3 for the mint output: the
// slice is sorted by (Value, Scheme, entity key) and two mint calls are byte-order
// identical (no reliance on map iteration order).
func TestNomina_DeterministicSortedEmission(t *testing.T) {
	a := bestiary.MintNomina(bestiary.Entities())
	b := bestiary.MintNomina(bestiary.Entities())
	if len(a) != len(b) {
		t.Fatalf("two mint calls differ in length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Value != b[i].Value || a[i].Scheme != b[i].Scheme ||
			a[i].ResolvesTo.String() != b[i].ResolvesTo.String() ||
			a[i].Status != b[i].Status || a[i].SourceURL != b[i].SourceURL || a[i].Source != b[i].Source {
			t.Fatalf("mint output nondeterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
		if i > 0 {
			prev, cur := a[i-1], a[i]
			if prev.Value > cur.Value {
				t.Fatalf("not sorted by value at %d: %q > %q", i, prev.Value, cur.Value)
			}
		}
	}
}

// TestNomina_ValidateNoConflict asserts the real minted set has no same-triple
// conflict — the positive half of the codegen guard.
func TestNomina_ValidateNoConflict(t *testing.T) {
	if err := bestiary.ValidateNomina(bestiary.MintNomina(bestiary.Entities())); err != nil {
		t.Fatalf("ValidateNomina over the real bake returned a conflict: %v", err)
	}
}

// TestNomina_SameTripleConflict_Loud is the NEGATIVE CONTROL: two nomina sharing the
// PK triple (value, scheme, entity_key) but disagreeing on status MUST fail the guard
// loudly (never last-write-wins). A crafted duplicate is required so the guard is not
// vacuously green.
func TestNomina_SameTripleConflict_Loud(t *testing.T) {
	ref := bestiary.EntityRef{Family: "grok", Version: "4.20", Modifier: []string{"reasoning"}}
	conflict := []bestiary.Nomen{
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityAdmitted, ResolvesTo: ref, SourceURL: "https://a", Source: bestiary.DataSourceModelsDev},
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityPreferred, ResolvesTo: ref, SourceURL: "https://b", Source: bestiary.DataSourceModelsDev},
	}
	err := bestiary.ValidateNomina(conflict)
	if err == nil {
		t.Fatal("ValidateNomina accepted a same-triple conflict; want a loud error")
	}
	if !strings.Contains(err.Error(), "same-triple") {
		t.Errorf("conflict error is not actionable about the triple: %v", err)
	}

	// An EXACT-duplicate triple (identical fields) is a harmless idempotent no-op and
	// must NOT error.
	dup := []bestiary.Nomen{conflict[0], conflict[0]}
	if err := bestiary.ValidateNomina(dup); err != nil {
		t.Errorf("ValidateNomina rejected an identical-duplicate triple (should be idempotent): %v", err)
	}
}

// TestNomenLookup_GrokBeta is the grok-beta worked example: the curated xAI alias
// claim resolves to the real grok@4.20{reasoning} entity, is Admitted, and carries
// claim attribution (SourceURL = the xAI page) DISTINCT from Source (the curated
// ingest we read it from) — the SourceURL-vs-Source discipline demonstrated end-to-end.
func TestNomenLookup_GrokBeta(t *testing.T) {
	matches, ok := bestiary.NomenLookup("grok-beta")
	if !ok || len(matches) != 1 {
		t.Fatalf("NomenLookup(grok-beta) = (%d rows, ok=%v), want exactly 1", len(matches), ok)
	}
	n := matches[0]
	if n.Scheme != bestiary.NomenSchemeAlias {
		t.Errorf("grok-beta scheme = %v, want alias", n.Scheme)
	}
	if n.Status != bestiary.AcceptabilityAdmitted {
		t.Errorf("grok-beta status = %v, want admitted", n.Status)
	}
	if got := n.ResolvesTo.String(); got != "grok@4.20{reasoning}" {
		t.Errorf("grok-beta resolves to %q, want grok@4.20{reasoning}", got)
	}
	if n.SourceURL == "" || !strings.Contains(n.SourceURL, "x.ai") {
		t.Errorf("grok-beta SourceURL = %q, want the xAI claimant page", n.SourceURL)
	}
	if n.Source != bestiary.DataSourceCurated {
		t.Errorf("grok-beta Source = %q, want curated (the honest ingest — read from bestiary's own claim file, distinct from the xAI claimant)", n.Source)
	}
	// The alias target must be a real entity, so the CLI can show it end-to-end.
	if _, exists := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning"); !exists {
		t.Error("grok-beta resolves to grok@4.20{reasoning}, which is not a real entity")
	}
}

// TestNomenLookup_HomonymyPositiveFence is the HOMONYMY POSITIVE FENCE: a spelling
// that names more than one distinct entity returns ALL of its rows (never a single
// "the nomen"). It scans for a real homonym and asserts NomenLookup returns every row
// the index holds for it.
func TestNomenLookup_HomonymyPositiveFence(t *testing.T) {
	idx := map[string]int{}
	entsPerValue := map[string]map[string]bool{}
	for _, n := range bestiary.MintNomina(bestiary.Entities()) {
		idx[n.Value]++
		if entsPerValue[n.Value] == nil {
			entsPerValue[n.Value] = map[string]bool{}
		}
		entsPerValue[n.Value][n.ResolvesTo.String()] = true
	}
	var homonym string
	for v, ents := range entsPerValue {
		if len(ents) > 1 {
			homonym = v
			break
		}
	}
	if homonym == "" {
		t.Skip("no homonymous spelling in the current bake; fence not exercisable")
	}
	matches, ok := bestiary.NomenLookup(homonym)
	if !ok {
		t.Fatalf("NomenLookup(%q) missing; want the homonym rows", homonym)
	}
	if len(matches) != idx[homonym] {
		t.Errorf("NomenLookup(%q) returned %d rows, want all %d persisted rows", homonym, len(matches), idx[homonym])
	}
	distinct := map[string]bool{}
	for _, n := range matches {
		distinct[n.ResolvesTo.String()] = true
	}
	if len(distinct) < 2 {
		t.Errorf("homonym %q resolved to %d distinct entities, want >= 2 (the fence)", homonym, len(distinct))
	}
}

// TestEntityNomina_CanonicalPreferredPlusAlias verifies the per-entity projection: the
// grok@4.20{reasoning} entity's Nomina() carries its canonical key as a Preferred
// nomen AND the grok-beta alias that resolves to it.
func TestEntityNomina_CanonicalPreferredPlusAlias(t *testing.T) {
	e, ok := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok@4.20{reasoning} entity not found")
	}
	var sawCanonicalPreferred, sawAlias bool
	for _, n := range e.Nomina() {
		if n.Scheme == bestiary.NomenSchemeCanonical {
			if n.Value != "grok@4.20{reasoning}" {
				t.Errorf("canonical nomen value = %q, want the entity key", n.Value)
			}
			if n.Status != bestiary.AcceptabilityPreferred {
				t.Errorf("canonical nomen status = %v, want preferred", n.Status)
			}
			sawCanonicalPreferred = true
		}
		if n.Scheme == bestiary.NomenSchemeAlias && n.Value == "grok-beta" {
			sawAlias = true
		}
	}
	if !sawCanonicalPreferred {
		t.Error("entity Nomina() missing its canonical Preferred nomen")
	}
	if !sawAlias {
		t.Error("entity Nomina() missing the grok-beta alias claim that resolves to it")
	}
}

// TestDesignationNomenConsistencyFence pins the Designations↔Nomen consistency fence:
// for a static model, the SchemeCanonical Designation rating equals the
// NomenSchemeCanonical status (both Preferred) — shared schemes agree on rating.
func TestDesignationNomenConsistencyFence(t *testing.T) {
	e, ok := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok@4.20{reasoning} entity not found")
	}
	// Canonical nomen status for this entity.
	var nomenStatus bestiary.AcceptabilityRating = -1
	for _, n := range e.Nomina() {
		if n.Scheme == bestiary.NomenSchemeCanonical {
			nomenStatus = n.Status
		}
	}
	if nomenStatus == -1 {
		t.Fatal("no canonical nomen for the entity")
	}
	// Canonical designation rating for an instance of the same entity.
	if len(e.Instances) == 0 {
		t.Fatal("entity has no instances")
	}
	m, ok := bestiary.LookupModel(e.Instances[0].ID)
	if !ok {
		t.Fatalf("LookupModel(%q) missing", e.Instances[0].ID)
	}
	var desigRating bestiary.AcceptabilityRating = -1
	for _, d := range m.Ref().Designations() {
		if d.Scheme == bestiary.SchemeCanonical {
			desigRating = d.Rating
		}
	}
	if desigRating != nomenStatus {
		t.Errorf("consistency fence broken: canonical Designation rating=%v but canonical Nomen status=%v (shared scheme must agree)", desigRating, nomenStatus)
	}
	if nomenStatus != bestiary.AcceptabilityPreferred {
		t.Errorf("canonical scheme rating=%v, want preferred (activation)", nomenStatus)
	}
}
