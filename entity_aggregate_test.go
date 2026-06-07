package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// findEntity returns the first entity in Entities() satisfying pred, failing the
// test when none matches. Selecting entities by predicate (rather than hardcoding
// a model id) keeps these tests resilient to registry regeneration.
func findEntity(t *testing.T, pred func(bestiary.Entity) bool) bestiary.Entity {
	t.Helper()
	for _, e := range bestiary.Entities() {
		if pred(e) {
			return e
		}
	}
	t.Fatal("no entity in the static registry matched the test predicate")
	return bestiary.Entity{}
}

// distinctProviders returns the set of distinct providers across the instances.
func distinctProviders(insts []bestiary.ProviderInstance) map[bestiary.Provider]struct{} {
	set := make(map[bestiary.Provider]struct{})
	for _, in := range insts {
		set[in.Provider] = struct{}{}
	}
	return set
}

// TestVC2_MultiProviderEntity_RangesAndInstances verifies the core entity
// aggregation contract: a model served by N providers resolves to exactly ONE
// entity carrying every instance, with the provider list equal to the distinct
// provider set and the integer context/max-output ranges equal to the true
// min/max over the instances.
func TestVC2_MultiProviderEntity_RangesAndInstances(t *testing.T) {
	// Pick a genuinely multi-provider entity so the N-provider rollup is exercised.
	e := findEntity(t, func(e bestiary.Entity) bool {
		return len(distinctProviders(e.Instances)) >= 2
	})

	// EntityByTuple must resolve the same identity to one entity with the same
	// instances (one entity, all instances).
	got, ok := bestiary.EntityByTuple(e.Ref.Family, e.Ref.Variant, e.Ref.Version, e.Ref.Modifier...)
	if !ok {
		t.Fatalf("EntityByTuple(%s) returned ok=false, want the entity to resolve", e.Ref.String())
	}
	if got.Ref.String() != e.Ref.String() {
		t.Fatalf("EntityByTuple returned a different identity: got %q, want %q", got.Ref.String(), e.Ref.String())
	}
	if len(got.Instances) != len(e.Instances) {
		t.Fatalf("instance count mismatch between Entities() and EntityByTuple(): %d vs %d", len(e.Instances), len(got.Instances))
	}

	// Provider list == distinct provider set (no duplicates, none missing).
	wantProviders := distinctProviders(got.Instances)
	if len(got.Providers) != len(wantProviders) {
		t.Errorf("Providers length = %d, want %d distinct providers", len(got.Providers), len(wantProviders))
	}
	seen := make(map[bestiary.Provider]struct{})
	for _, p := range got.Providers {
		if _, dup := seen[p]; dup {
			t.Errorf("Providers contains duplicate %q", p)
		}
		seen[p] = struct{}{}
		if _, ok := wantProviders[p]; !ok {
			t.Errorf("Providers contains %q not present in any instance", p)
		}
	}

	// Integer ranges equal the true min/max over instances.
	ctxMin, ctxMax := got.Instances[0].ContextWindow, got.Instances[0].ContextWindow
	moMin, moMax := got.Instances[0].MaxOutput, got.Instances[0].MaxOutput
	for _, in := range got.Instances {
		if in.ContextWindow < ctxMin {
			ctxMin = in.ContextWindow
		}
		if in.ContextWindow > ctxMax {
			ctxMax = in.ContextWindow
		}
		if in.MaxOutput < moMin {
			moMin = in.MaxOutput
		}
		if in.MaxOutput > moMax {
			moMax = in.MaxOutput
		}
	}
	if got.ContextRange != [2]int{ctxMin, ctxMax} {
		t.Errorf("ContextRange = %v, want %v", got.ContextRange, [2]int{ctxMin, ctxMax})
	}
	if got.MaxOutputRange != [2]int{moMin, moMax} {
		t.Errorf("MaxOutputRange = %v, want %v", got.MaxOutputRange, [2]int{moMin, moMax})
	}
}

// TestVC8_NilCostAggregation sweeps every entity and verifies the T9 nil-cost
// rule holds universally: the price range covers only NON-nil instance costs;
// when every instance cost is nil the range is {nil, nil}; and aggregation never
// nil-derefs (the sweep itself would panic if it did). The sweep is a stronger
// statement than a single fixture because it proves the invariant for the entire
// generated catalog.
func TestVC8_NilCostAggregation(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("Entities() returned no entities; cannot validate nil-cost aggregation")
	}

	allNilSeen, mixedSeen := 0, 0
	for _, e := range entities {
		checkPriceRange(t, e.Ref.String(), "input", e.PriceInputRange, func(in bestiary.ProviderInstance) *float64 {
			return in.CostInputPerMTok
		}, e.Instances, &allNilSeen, &mixedSeen)
		checkPriceRange(t, e.Ref.String(), "output", e.PriceOutputRange, func(in bestiary.ProviderInstance) *float64 {
			return in.CostOutputPerMTok
		}, e.Instances, nil, nil)
	}
	// Observability: a silent "0 all-nil entities" would mean the {nil,nil} arm was
	// never actually exercised by real data. Log the coverage either way.
	t.Logf("VC8 coverage over %d entities: input-cost all-nil=%d, mixed=%d", len(entities), allNilSeen, mixedSeen)

	// Self-guard the coverage: both the all-nil ({nil,nil}) arm and the mixed
	// (range-over-non-nil) arm MUST have been exercised by real data. A future
	// regen that eliminated either case would otherwise leave that arm silently
	// untested.
	if allNilSeen == 0 {
		t.Error("VC8 all-nil arm never exercised: no entity had every input cost nil; the {nil,nil} path is untested")
	}
	if mixedSeen == 0 {
		t.Error("VC8 mixed arm never exercised: no entity mixed nil and non-nil input costs; range-over-non-nil is untested")
	}
}

func checkPriceRange(
	t *testing.T,
	key, which string,
	rng [2]*float64,
	cost func(bestiary.ProviderInstance) *float64,
	insts []bestiary.ProviderInstance,
	allNilSeen, mixedSeen *int,
) {
	t.Helper()
	var min, max float64
	found := false
	nilCount := 0
	for _, in := range insts {
		c := cost(in)
		if c == nil {
			nilCount++
			continue
		}
		if !found {
			min, max, found = *c, *c, true
		} else {
			if *c < min {
				min = *c
			}
			if *c > max {
				max = *c
			}
		}
	}

	if !found {
		// All instance costs nil → range must be {nil, nil}.
		if rng[0] != nil || rng[1] != nil {
			t.Errorf("entity %s %s-price: all costs nil but range = {%v, %v}, want {nil, nil}", key, which, rng[0], rng[1])
		}
		if allNilSeen != nil {
			*allNilSeen++
		}
		return
	}

	if rng[0] == nil || rng[1] == nil {
		t.Errorf("entity %s %s-price: have non-nil costs but range bound is nil: {%v, %v}", key, which, rng[0], rng[1])
		return
	}
	if *rng[0] != min || *rng[1] != max {
		t.Errorf("entity %s %s-price range = {%v, %v}, want {%v, %v}", key, which, *rng[0], *rng[1], min, max)
	}
	if mixedSeen != nil && nilCount > 0 {
		*mixedSeen++
	}
}

// TestVC9_DefensiveCopy_NoWrongMerge verifies that values returned by the entity
// layer are deep copies: mutating a returned Entity (its slices, its Ref, its
// price-range pointers) cannot corrupt the registry or alias another entity, and
// two distinct entities never share backing arrays.
func TestVC9_DefensiveCopy_NoWrongMerge(t *testing.T) {
	pred := func(e bestiary.Entity) bool {
		return len(e.Instances) >= 2 && e.PriceInputRange[0] != nil
	}
	e := findEntity(t, pred)
	fam, variant, version, mods := e.Ref.Family, e.Ref.Variant, e.Ref.Version, e.Ref.Modifier

	// Two independent reads of the SAME entity must not share backing storage.
	a, ok := bestiary.EntityByTuple(fam, variant, version, mods...)
	if !ok {
		t.Fatalf("EntityByTuple(%s) ok=false", e.Ref.String())
	}
	b, ok := bestiary.EntityByTuple(fam, variant, version, mods...)
	if !ok {
		t.Fatalf("EntityByTuple(%s) second read ok=false", e.Ref.String())
	}
	if &a.Instances[0] == &b.Instances[0] {
		t.Error("two EntityByTuple reads share the same Instances backing array (aliasing)")
	}
	if a.PriceInputRange[0] == b.PriceInputRange[0] {
		t.Error("two EntityByTuple reads share the same PriceInputRange pointer (aliasing)")
	}

	// Record originals from an untouched read.
	origInstanceID := b.Instances[0].ID
	origProvider := b.Providers[0]
	origPriceLo := *b.PriceInputRange[0]
	hadInstanceCost := b.Instances[0].CostInputPerMTok != nil

	// Mutate every mutable surface of `a`.
	a.Instances[0].ID = bestiary.ModelID("MUTATED-id")
	a.Instances[0].Provider = bestiary.Provider("MUTATED-prov")
	if a.Instances[0].CostInputPerMTok != nil {
		*a.Instances[0].CostInputPerMTok = -999
	}
	a.Providers[0] = bestiary.Provider("MUTATED-prov")
	*a.PriceInputRange[0] = -1234
	if len(a.Ref.Modifier) > 0 {
		a.Ref.Modifier[0] = "MUTATED-mod"
	}

	// A fresh read must be entirely unaffected (registry not corrupted).
	c, ok := bestiary.EntityByTuple(fam, variant, version, mods...)
	if !ok {
		t.Fatalf("EntityByTuple(%s) post-mutation read ok=false", e.Ref.String())
	}
	if c.Instances[0].ID != origInstanceID {
		t.Errorf("registry corrupted: instance ID = %q, want %q", c.Instances[0].ID, origInstanceID)
	}
	if c.Instances[0].Provider == bestiary.Provider("MUTATED-prov") {
		t.Error("registry corrupted: instance Provider leaked the mutation")
	}
	if c.Providers[0] != origProvider {
		t.Errorf("registry corrupted: Providers[0] = %q, want %q", c.Providers[0], origProvider)
	}
	if *c.PriceInputRange[0] != origPriceLo {
		t.Errorf("registry corrupted: PriceInputRange[0] = %v, want %v", *c.PriceInputRange[0], origPriceLo)
	}
	if hadInstanceCost && *c.Instances[0].CostInputPerMTok == -999 {
		t.Error("registry corrupted: instance CostInputPerMTok leaked the mutation")
	}

	// The sibling read `b` taken BEFORE the mutation must also be unaffected
	// (distinct entities never share backing arrays).
	if b.Instances[0].ID != origInstanceID {
		t.Errorf("sibling entity aliased: instance ID = %q, want %q", b.Instances[0].ID, origInstanceID)
	}
	if *b.PriceInputRange[0] != origPriceLo {
		t.Errorf("sibling entity aliased: PriceInputRange[0] = %v, want %v", *b.PriceInputRange[0], origPriceLo)
	}
}

// TestVC9_DistinctEntitiesDoNotShareStorage verifies two DIFFERENT entities never
// share backing arrays: mutating one entity's slices leaves the other intact.
func TestVC9_DistinctEntitiesDoNotShareStorage(t *testing.T) {
	all := bestiary.Entities()
	if len(all) < 2 {
		t.Skip("need at least two entities to compare cross-entity aliasing")
	}
	// Find two distinct entities that both have at least one instance.
	var first, second bestiary.Entity
	first = all[0]
	for _, e := range all[1:] {
		if e.Ref.String() != first.Ref.String() && len(e.Instances) > 0 && len(first.Instances) > 0 {
			second = e
			break
		}
	}
	if len(first.Instances) == 0 || len(second.Instances) == 0 {
		t.Skip("could not find two distinct non-empty entities")
	}
	if &first.Instances[0] == &second.Instances[0] {
		t.Fatal("distinct entities share an Instances backing array")
	}
	secondOrigID := second.Instances[0].ID
	first.Instances[0].ID = bestiary.ModelID("MUTATED")
	if second.Instances[0].ID != secondOrigID {
		t.Errorf("mutating one entity changed another: second instance ID = %q, want %q", second.Instances[0].ID, secondOrigID)
	}
}

// TestEntityByTuple_Miss verifies a non-existent identity tuple returns
// (zero, false) without panicking.
func TestEntityByTuple_Miss(t *testing.T) {
	e, ok := bestiary.EntityByTuple("no-such-family", "no-variant", "no-version")
	if ok {
		t.Errorf("EntityByTuple(bogus) ok=true, want false; got %s", e.Ref.String())
	}
}

// TestEntityIndex_AttributeModifierFoldsIntoBase proves the INDEX-BUILD side of
// the identity projection: a model whose DECOMPOSED ModelInfo.Modifier carries a
// real attribute-class token (e.g. ["thinking"]) is folded into its BASE entity
// (empty Ref.Modifier) — its instance appears in that base entity's Instances —
// because the index keys on EntityModifiers(m.Modifier, m.Family), not raw
// m.Modifier.
//
// This guard is independent of EntityByTuple's lookup-side projection: it asserts
// the concrete instance is PRESENT in the base entity. If the index build used
// raw m.Modifier, that instance would key into a separate "{thinking}" entity and
// be ABSENT from the base — so this test goes RED under that regression (verified
// by flipping registry.go's EntityModifiers(...) → m.Modifier).
func TestEntityIndex_AttributeModifierFoldsIntoBase(t *testing.T) {
	// Locate a registry model whose raw decomposed modifiers are non-empty but
	// project to NO identity modifiers (i.e. every raw token is attribute-class),
	// so its identity is the bare base tuple. The dash-form "...-thinking" Claude
	// IDs decompose to Modifier=["thinking"] and qualify.
	var probe bestiary.ModelInfo
	found := false
	for _, m := range bestiary.StaticModels() {
		if len(m.Modifier) == 0 {
			continue
		}
		if len(bestiary.EntityModifiers(m.Modifier, m.Family)) == 0 {
			probe = m
			found = true
			break
		}
	}
	if !found {
		t.Skip("no registry model with a non-empty all-attribute modifier set; cannot exercise index-side folding")
	}
	t.Logf("probe instance: ID=%s family=%s variant=%s version=%s rawMod=%v", probe.ID, probe.Family, probe.Variant, probe.Version, probe.Modifier)

	// The probe's BASE identity is its tuple with NO modifiers.
	base, ok := bestiary.EntityByTuple(probe.Family, probe.Variant, probe.Version)
	if !ok {
		t.Fatalf("base entity %s/%s@%s not found; the attribute-modifier instance did not fold into a base entity", probe.Family, probe.Variant, probe.Version)
	}
	if len(base.Ref.Modifier) != 0 {
		t.Fatalf("base entity unexpectedly carries identity modifiers %v; key=%q", base.Ref.Modifier, base.Ref.String())
	}

	// The probe instance MUST be present in the base entity's Instances. Under a
	// raw-modifier index it would instead live under "<base>{thinking}" and be
	// absent here.
	present := false
	for _, in := range base.Instances {
		if in.ID == probe.ID && in.Provider == probe.Provider {
			present = true
			break
		}
	}
	if !present {
		t.Errorf("index-side projection failed: instance %s (provider %s, rawMod %v) is NOT in base entity %q's instances — the attribute token leaked into the entity key and split it off its base",
			probe.ID, probe.Provider, probe.Modifier, base.Ref.String())
	}
}
