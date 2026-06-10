package bestiary_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// findMultiSourceEntity returns the key of an entity attested by both models.dev
// AND ollama (the curated-join case), or "" if none exists.
func findMultiSourceEntity(t *testing.T) string {
	t.Helper()
	for _, e := range bestiary.Entities() {
		var hasMD, hasOllama bool
		for _, s := range e.Sources {
			switch s {
			case bestiary.DataSourceModelsDev:
				hasMD = true
			case bestiary.DataSourceOllama:
				hasOllama = true
			}
		}
		if hasMD && hasOllama {
			return e.Ref.String()
		}
	}
	return ""
}

// TestEntitySource_FKResolves (VC-NORM1) asserts the join relation is referentially
// sound: every attested SourceID resolves to a real DataSource, and every attested
// EntityKey resolves to a real registry entity. ValidateEntitySourceTable /
// ValidateDataSourceTable are the codegen FK guards that enforce this.
func TestEntitySource_FKResolves(t *testing.T) {
	if err := bestiary.ValidateDataSourceTable(); err != nil {
		t.Fatalf("ValidateDataSourceTable: %v", err)
	}
	if err := bestiary.ValidateEntitySourceTable(); err != nil {
		t.Fatalf("ValidateEntitySourceTable: %v", err)
	}

	for _, e := range bestiary.Entities() {
		// Every entity's source projection must resolve to a real DataSource.
		for _, sid := range e.Sources {
			if _, ok := bestiary.DataSourceByID(sid); !ok {
				t.Errorf("entity %q attests source %q with no DataSource dimension row", e.Ref.String(), sid)
			}
		}
		// And EntitySources(key) is the join lookup for the same key — it must agree.
		joined := bestiary.EntitySources(e.Ref.String())
		if !reflect.DeepEqual(joined, e.Sources) {
			t.Errorf("entity %q: EntitySources()=%v != Entity.Sources=%v", e.Ref.String(), joined, e.Sources)
		}
	}
}

// TestEntitySources_ProjectionAgrees (VC-NORM3) asserts Entity.Sources equals the
// sorted, distinct set of attesting SourceIDs for that key — the projection is a
// faithful, de-duplicated, sorted view of the join relation.
func TestEntitySources_ProjectionAgrees(t *testing.T) {
	for _, e := range bestiary.Entities() {
		got := e.Sources

		// Sorted ascending.
		if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
			t.Errorf("entity %q: Sources not sorted ascending: %v", e.Ref.String(), got)
		}
		// Distinct (no duplicate SourceID).
		seen := map[bestiary.DataSourceID]bool{}
		for _, s := range got {
			if seen[s] {
				t.Errorf("entity %q: duplicate source %q in Sources %v", e.Ref.String(), s, got)
			}
			seen[s] = true
		}
		// Agrees with the join lookup (the relation's own projection for this key).
		want := bestiary.EntitySources(e.Ref.String())
		if !reflect.DeepEqual(got, want) {
			t.Errorf("entity %q: Sources=%v != EntitySources()=%v", e.Ref.String(), got, want)
		}
	}
}

// TestEntitySource_MultiSource (VC-NORM4) asserts the curated llama-3.3-70b entity
// is attested by BOTH models.dev and ollama (two attestations, sorted), and that
// each attesting source's DatasetIngested resolves via the FK join. This is the
// multi-source case the BCNF normalization exists for: a single entity, two join
// rows, NOT two entities and NOT an array-valued source-of-truth.
func TestEntitySource_MultiSource(t *testing.T) {
	e, ok := bestiary.EntityByTuple("llama", "", "3.3", "70b", "instruct")
	if !ok {
		t.Fatal("curated entity llama@3.3#70b{instruct} not found in registry")
	}
	want := []bestiary.DataSourceID{bestiary.DataSourceModelsDev, bestiary.DataSourceOllama}
	if !reflect.DeepEqual(e.Sources, want) {
		t.Fatalf("70b entity Sources = %v, want %v (sorted dual attestation)", e.Sources, want)
	}
	for _, sid := range e.Sources {
		di, ok := bestiary.DatasetIngestedFor(sid)
		if !ok {
			t.Errorf("no DatasetIngested for attesting source %q", sid)
			continue
		}
		if di.SourceID != sid {
			t.Errorf("DatasetIngestedFor(%q).SourceID = %q, want %q", sid, di.SourceID, sid)
		}
	}

	// Cross-check the generic finder reaches the same conclusion (the curated 70b is
	// the multi-source entity we expect).
	if got := findMultiSourceEntity(t); got != e.Ref.String() {
		t.Logf("first multi-source entity found = %q (expected at least %q)", got, e.Ref.String())
		if got == "" {
			t.Fatal("no multi-source entity found; expected the curated 70b to be dual-attested")
		}
	}
}

// TestDatasetIngested_NoURI (VC-NORM5) asserts DatasetIngested carries NO uri field
// (it was normalized out as a transitive dependency) and that the uri is instead
// obtained by the FK join to DataSource via DataSourceByID(SourceID).
func TestDatasetIngested_NoURI(t *testing.T) {
	// Reflectively assert the struct has no "URI"/"Uri" field — the BCNF guarantee.
	rt := reflect.TypeOf(bestiary.DatasetIngested{})
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		if name == "URI" || name == "Uri" {
			t.Fatalf("DatasetIngested has a %q field; uri must be reached via the DataSource join, not duplicated here", name)
		}
	}

	di, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceOllama)
	if !ok {
		t.Fatal("no DatasetIngested for ollama")
	}
	if di.IngestedAt == "" {
		t.Error("DatasetIngested.IngestedAt is empty; expected the committed snapshot timestamp")
	}
	// The uri is obtained by joining to the DataSource dimension.
	ds, ok := bestiary.DataSourceByID(di.SourceID)
	if !ok {
		t.Fatalf("FK join failed: DataSourceByID(%q) missing", di.SourceID)
	}
	if ds.URI == "" {
		t.Errorf("joined DataSource(%q).URI is empty; the join must yield the fetch endpoint", di.SourceID)
	}
}

// TestEntitySources_Deterministic asserts the projection and relation iteration
// order are pinned: two independent reads of the same key return byte-equal slices
// (the explicit sort, NOT first-seen, guarantees this).
func TestEntitySources_Deterministic(t *testing.T) {
	key := "llama@3.3#70b{instruct}"
	first := bestiary.EntitySources(key)
	second := bestiary.EntitySources(key)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("EntitySources(%q) not stable: %v vs %v", key, first, second)
	}
	// And every entity's projection is independently sorted (relation-wide pin).
	for _, e := range bestiary.Entities() {
		s := e.Sources
		if !sort.SliceIsSorted(s, func(i, j int) bool { return s[i] < s[j] }) {
			t.Fatalf("entity %q projection not sorted: %v", e.Ref.String(), s)
		}
	}
}

// TestKnownDataSources_SeedPresent asserts the shipped seed contains both the
// models.dev and ollama dimension rows with their candidate-key uris.
func TestKnownDataSources_SeedPresent(t *testing.T) {
	byID := map[bestiary.DataSourceID]bestiary.DataSource{}
	for _, ds := range bestiary.KnownDataSources() {
		byID[ds.ID] = ds
	}
	md, ok := byID[bestiary.DataSourceModelsDev]
	if !ok {
		t.Fatal("KnownDataSources missing models.dev")
	}
	if md.URI == "" {
		t.Error("models.dev source has an empty uri")
	}
	ol, ok := byID[bestiary.DataSourceOllama]
	if !ok {
		t.Fatal("KnownDataSources missing ollama")
	}
	if ol.URI == "" {
		t.Error("ollama source has an empty uri")
	}
}
