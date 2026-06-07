package bestiary_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// findEdge returns the first lineage edge with a parent of the given family, and
// whether it was found — a small helper so assertions read by intent.
func findEdge(edges []bestiary.LineageEdge, parentFamily bestiary.Family) (bestiary.LineageEdge, bool) {
	for _, e := range edges {
		if e.Parent.Family == parentFamily {
			return e, true
		}
	}
	return bestiary.LineageEdge{}, false
}

// TestLineage_RealCatalogEdges (VC3+) pins the REAL finetune edges: the child
// records exist in the models.dev catalog and codegen populated their
// ModelInfo.Lineage from the curated ledger. dracarys and hermes-2-pro are both
// llama finetunes.
func TestLineage_RealCatalogEdges(t *testing.T) {
	cases := []struct {
		name      string
		id        bestiary.ModelID
		parentFam bestiary.Family
		parentVer string
		kind      bestiary.DerivationKind
	}{
		{
			name:      "dracarys-llama-3.1-70b finetune of llama",
			id:        "abacusai/dracarys-llama-3_1-70b-instruct",
			parentFam: bestiary.FamilyLlama,
			parentVer: "3.1",
			kind:      bestiary.DerivationFinetune,
		},
		{
			name:      "hermes-2-pro-llama-3-8b finetune of llama",
			id:        "nousresearch/hermes-2-pro-llama-3-8b",
			parentFam: bestiary.FamilyLlama,
			parentVer: "3",
			kind:      bestiary.DerivationFinetune,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The catalog record carries the populated lineage (codegen-baked).
			m, ok := bestiary.LookupModel(tc.id)
			if !ok {
				t.Fatalf("LookupModel(%q): not in catalog — the REAL fixture must exist", tc.id)
			}
			edge, ok := findEdge(m.Lineage, tc.parentFam)
			if !ok {
				t.Fatalf("ModelInfo.Lineage for %q = %+v, want an edge with parent family %q", tc.id, m.Lineage, tc.parentFam)
			}
			if edge.Parent.Version != tc.parentVer {
				t.Errorf("parent version = %q, want %q", edge.Parent.Version, tc.parentVer)
			}
			if edge.Kind != tc.kind {
				t.Errorf("derivation kind = %v, want %v", edge.Kind, tc.kind)
			}

			// The curated record is flagged REAL (attested catalog child).
			rec, ok := bestiary.LineageRecordFor(tc.id)
			if !ok {
				t.Fatalf("LineageRecordFor(%q): no curated record", tc.id)
			}
			if !rec.Real {
				t.Errorf("LineageRecordFor(%q).Real = false, want true (child is in the catalog)", tc.id)
			}
		})
	}
}

// TestLineage_SyntheticCatalogAbsent (VC3+) pins the SYNTHETIC nous-hermes-2
// edges to Solar / Yi. These children are NOT in the 4,979-record catalog (only
// their parents are), so the curated record must be flagged Real=false and the
// child must be absent from the static registry — never mistaken for attested.
func TestLineage_SyntheticCatalogAbsent(t *testing.T) {
	cases := []struct {
		name      string
		childID   bestiary.ModelID
		parentFam bestiary.Family
		parentVer string
	}{
		{"nous-hermes-2 10.7B finetune of solar", "nous-hermes-2-solar-10.7b", bestiary.FamilySolar, "10.7b"},
		{"nous-hermes-2 34B finetune of yi", "nous-hermes-2-yi-34b", bestiary.FamilyYi, "34b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, ok := bestiary.LineageRecordFor(tc.childID)
			if !ok {
				t.Fatalf("LineageRecordFor(%q): no curated record (synthetic edge missing)", tc.childID)
			}
			if rec.Real {
				t.Errorf("LineageRecordFor(%q).Real = true, want false (catalog-absent synthetic fixture)", tc.childID)
			}
			edge, ok := findEdge(rec.Edges, tc.parentFam)
			if !ok {
				t.Fatalf("edges = %+v, want a parent with family %q", rec.Edges, tc.parentFam)
			}
			if edge.Parent.Version != tc.parentVer {
				t.Errorf("parent version = %q, want %q", edge.Parent.Version, tc.parentVer)
			}
			if edge.Kind != bestiary.DerivationFinetune {
				t.Errorf("kind = %v, want finetune", edge.Kind)
			}
			// The synthetic child must NOT be in the catalog.
			if _, inCatalog := bestiary.LookupModel(tc.childID); inCatalog {
				t.Errorf("LookupModel(%q) found a record; the synthetic child must be catalog-absent", tc.childID)
			}
			// The parent base family must be recognized (registered for validation).
			if !tc.parentFam.IsKnown() {
				t.Errorf("parent base family %q must be a known family (registered base)", tc.parentFam)
			}
		})
	}
}

// TestLineage_MergeFixture (VC3+/VC10) pins the >=2-parent MERGE case: the REAL
// mythomax-l2-13b is a merge of two llama-2 parents.
func TestLineage_MergeFixture(t *testing.T) {
	const id = bestiary.ModelID("gryphe/mythomax-l2-13b")
	m, ok := bestiary.LookupModel(id)
	if !ok {
		t.Fatalf("LookupModel(%q): the REAL merge fixture must exist in the catalog", id)
	}
	if len(m.Lineage) < 2 {
		t.Fatalf("merge must carry >= 2 parents; got %d: %+v", len(m.Lineage), m.Lineage)
	}
	for i, e := range m.Lineage {
		if e.Kind != bestiary.DerivationMerge {
			t.Errorf("edge[%d].Kind = %v, want merge", i, e.Kind)
		}
		// Parents are standalone merge families (mythologic, huginn) — see VC13's
		// TestLineage_MergeParentsAreStandaloneFamilies for the per-family pins.
		if e.Parent.Family == bestiary.FamilyLlama {
			t.Errorf("edge[%d].Parent.Family = llama; merge parents must be standalone families", i)
		}
	}
	rec, ok := bestiary.LineageRecordFor(id)
	if !ok || !rec.Real {
		t.Errorf("LineageRecordFor(%q): want (real=true, ok=true), got real=%v ok=%v", id, rec.Real, ok)
	}
}

// TestLineage_MergeParentsAreStandaloneFamilies (VC13) pins that the mythomax
// merge parents are modeled as their OWN base families (mythologic, huginn) — not
// as llama-variants — and that both are registered as known base families so
// lineage parent-validation accepts them. VC10's ≥2-parent merge invariant holds.
func TestLineage_MergeParentsAreStandaloneFamilies(t *testing.T) {
	m, ok := bestiary.LookupModel("gryphe/mythomax-l2-13b")
	if !ok {
		t.Fatal("LookupModel(gryphe/mythomax-l2-13b): merge fixture must exist")
	}
	if len(m.Lineage) < 2 {
		t.Fatalf("VC10: merge must carry >= 2 parents; got %d", len(m.Lineage))
	}
	wantParents := map[bestiary.Family]bool{
		bestiary.FamilyMythologic: false,
		bestiary.FamilyHuginn:     false,
	}
	for _, e := range m.Lineage {
		if e.Kind != bestiary.DerivationMerge {
			t.Errorf("parent %q kind = %v, want merge", e.Parent.Family, e.Kind)
		}
		// The parent must be a STANDALONE family, never a llama-variant.
		if e.Parent.Family == bestiary.FamilyLlama {
			t.Errorf("merge parent modeled as llama (variant %q); want a standalone family", e.Parent.Variant)
		}
		if _, expected := wantParents[e.Parent.Family]; expected {
			wantParents[e.Parent.Family] = true
		}
		// Every parent base family must be registered/known for validation.
		if !e.Parent.Family.IsKnown() {
			t.Errorf("merge parent family %q is not a known base family (must be registered)", e.Parent.Family)
		}
	}
	for fam, seen := range wantParents {
		if !seen {
			t.Errorf("expected standalone merge parent %q not found in lineage", fam)
		}
	}
}

// TestLineage_IDFamilyOverride_FoldToLlamaDerivatives (VC14) pins the exact-ID
// family overrides that rescue derivatives a provider mislabeled with the base
// family ("llama"): the record must regain its derivative family AND its curated
// lineage, and the previously case-split MythoMax must link to the one mythomax
// entity across providers.
func TestLineage_IDFamilyOverride_FoldToLlamaDerivatives(t *testing.T) {
	// Dracarys-72B: provider tagged it raw_family=llama → must become dracarys
	// with a finetune-from-llama edge.
	d, ok := bestiary.LookupModel("abacusai/Dracarys-72B-Instruct")
	if !ok {
		t.Fatal("LookupModel(abacusai/Dracarys-72B-Instruct): record must exist")
	}
	if d.Family != bestiary.Family("dracarys") {
		t.Errorf("Dracarys-72B Family = %q, want dracarys (override must beat raw_family=llama)", d.Family)
	}
	if edge, ok := findEdge(d.Lineage, bestiary.FamilyLlama); !ok || edge.Kind != bestiary.DerivationFinetune {
		t.Errorf("Dracarys-72B Lineage = %+v, want a llama finetune edge", d.Lineage)
	}

	// MythoMax-L2-13b (nano-gpt, raw_family=llama) → mythomax + merge lineage.
	mm, ok := bestiary.LookupModel("Gryphe/MythoMax-L2-13b")
	if !ok {
		t.Fatal("LookupModel(Gryphe/MythoMax-L2-13b): record must exist")
	}
	if mm.Family != bestiary.Family("mythomax") {
		t.Errorf("MythoMax-L2-13b Family = %q, want mythomax (override must beat raw_family=llama)", mm.Family)
	}
	if len(mm.Lineage) < 2 {
		t.Errorf("MythoMax-L2-13b Lineage = %+v, want the >=2-parent merge", mm.Lineage)
	}

	// Cross-provider linking: the mythomax entity must now aggregate BOTH the
	// nano-gpt instance (previously split off as family=llama) and the providers
	// that always served it as mythomax — one entity, multiple providers.
	ent, ok := bestiary.EntityByTuple(bestiary.Family("mythomax"), "", "")
	if !ok {
		t.Fatal("EntityByTuple(mythomax): the mythomax entity must exist")
	}
	var providers = map[bestiary.Provider]bool{}
	for _, inst := range ent.Instances {
		providers[inst.Provider] = true
	}
	if !providers[bestiary.ProviderNanoGPT] {
		t.Errorf("mythomax entity instances = %v providers; want the nano-gpt MythoMax linked in (case-split fixed)", providers)
	}
	if len(providers) < 2 {
		t.Errorf("mythomax entity spans %d providers, want >= 2 (cross-provider linking)", len(providers))
	}
}

// TestLineage_JSONRoundTrip (VC3+) confirms a populated ModelInfo.Lineage
// round-trips through JSON: the DerivationKind serializes as its lowercase wire
// string and the edges survive marshal→unmarshal unchanged.
func TestLineage_JSONRoundTrip(t *testing.T) {
	m, ok := bestiary.LookupModel("gryphe/mythomax-l2-13b")
	if !ok || len(m.Lineage) < 2 {
		t.Fatalf("merge fixture missing or under-populated: ok=%v lineage=%+v", ok, m.Lineage)
	}

	enc, err := json.Marshal(m.Lineage)
	if err != nil {
		t.Fatalf("json.Marshal(Lineage) error: %v", err)
	}
	// Kind must be a string, not an integer (encoding.TextMarshaler).
	var generic []map[string]any
	if err := json.Unmarshal(enc, &generic); err != nil {
		t.Fatalf("json.Unmarshal generic error: %v", err)
	}
	if generic[0]["Kind"] != "merge" {
		t.Errorf("Lineage[0].Kind JSON = %v (%T), want string \"merge\"", generic[0]["Kind"], generic[0]["Kind"])
	}

	var back []bestiary.LineageEdge
	if err := json.Unmarshal(enc, &back); err != nil {
		t.Fatalf("json.Unmarshal([]LineageEdge) error: %v", err)
	}
	if !reflect.DeepEqual(back, m.Lineage) {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", back, m.Lineage)
	}
}

// TestLineage_Ancestors_RealCatalog (VC3+) exercises Entity.Ancestors over the
// populated (acyclic) curated DAG: a finetune child's ancestors are exactly its
// base parent. Cycle-termination of the DFS is covered directly against an
// injected cycle in the internal tests (TestLineageAncestors_CycleSafe).
func TestLineage_Ancestors_RealCatalog(t *testing.T) {
	// Construct the dracarys entity with its codegen-populated edges.
	edges := bestiary.LineageFor("abacusai/dracarys-llama-3_1-70b-instruct")
	if len(edges) == 0 {
		t.Fatal("LineageFor(dracarys) returned no edges")
	}
	e := bestiary.Entity{
		Ref:     bestiary.EntityRef{Family: bestiary.FamilyLlama, Variant: "dracarys", Version: "3.1"},
		Lineage: edges,
	}
	anc := e.Ancestors()
	if len(anc) != 1 {
		t.Fatalf("Ancestors() = %+v, want exactly [llama@3.1]", anc)
	}
	if anc[0].Family != bestiary.FamilyLlama || anc[0].Version != "3.1" {
		t.Errorf("Ancestors()[0] = %q, want llama@3.1", anc[0].String())
	}

	// Fallback seed: an entity with no inline Lineage still resolves via the
	// curated forward index keyed by its Ref.
	e2 := bestiary.Entity{Ref: bestiary.EntityRef{Family: bestiary.FamilyLlama, Variant: "dracarys", Version: "3.1"}}
	if got := e2.Ancestors(); len(got) != 1 || got[0].String() != "llama@3.1" {
		t.Errorf("Ancestors() via forward-index seed = %+v, want [llama@3.1]", got)
	}
}

// TestLineage_Descendants pins the reverse traversal: the llama@3.1 base entity's
// descendants include the dracarys child derived from it.
func TestLineage_Descendants(t *testing.T) {
	base := bestiary.Entity{Ref: bestiary.EntityRef{Family: bestiary.FamilyLlama, Version: "3.1"}}
	desc := base.Descendants()
	found := false
	for _, d := range desc {
		if d.String() == "llama/dracarys@3.1" {
			found = true
		}
	}
	if !found {
		t.Errorf("Descendants(llama@3.1) = %+v, want to include llama/dracarys@3.1", desc)
	}
}

// TestLineage_BaseModelHasNoLineage confirms a plain base model carries no
// curated lineage (nil), so lineage is a strictly additive, opt-in axis.
func TestLineage_BaseModelHasNoLineage(t *testing.T) {
	if edges := bestiary.LineageFor("some-totally-unknown-model-xyz"); edges != nil {
		t.Errorf("LineageFor(unknown) = %+v, want nil", edges)
	}
	if _, ok := bestiary.LineageRecordFor("some-totally-unknown-model-xyz"); ok {
		t.Error("LineageRecordFor(unknown) ok = true, want false")
	}
}
