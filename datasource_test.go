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

// TestEntitySources_Deterministic asserts EntitySources returns a copy-isolated,
// stable projection. The earlier two-consecutive-reads form was vacuous: both reads
// resolve through the same sync.Once-memoized relation, so they aliased one backing
// slice and could not detect a missing defensive copy. This form mutates the first
// read and asserts the second read is unaffected — killing a "return the internal
// slice without copying" mutant in EntitySources — then checks the relation-wide
// sort.
func TestEntitySources_Deterministic(t *testing.T) {
	key := "llama@3.3#70b{instruct}"
	first := bestiary.EntitySources(key)
	if len(first) == 0 {
		t.Fatalf("EntitySources(%q) returned no sources; expected the dual-attested 70b", key)
	}
	// Mutating the returned slice must not leak into the memoized relation.
	first[0] = "mutated-by-test"
	second := bestiary.EntitySources(key)
	if second[0] == "mutated-by-test" {
		t.Fatalf("EntitySources(%q) is not copy-isolated: a caller mutation leaked into the memoized relation", key)
	}
	want := []bestiary.DataSourceID{bestiary.DataSourceModelsDev, bestiary.DataSourceOllama}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("EntitySources(%q) = %v, want %v (sorted dual attestation)", key, second, want)
	}
	// And every entity's projection is independently sorted (relation-wide pin).
	for _, e := range bestiary.Entities() {
		s := e.Sources
		if !sort.SliceIsSorted(s, func(i, j int) bool { return s[i] < s[j] }) {
			t.Fatalf("entity %q projection not sorted: %v", e.Ref.String(), s)
		}
	}
}

// TestDatasetIngestHistoryFor_SeedAndCurrent asserts the v3 public history lookup
// over the shipped seed: DatasetIngestHistoryFor returns the source's ingest rows
// ascending, its final (maximum) element equals DatasetIngestedFor (the current
// ingest), and an unknown source yields nil. The shipped seed carries one committed
// snapshot row per source, so the single-element history is the current ingest.
func TestDatasetIngestHistoryFor_SeedAndCurrent(t *testing.T) {
	hist := bestiary.DatasetIngestHistoryFor(bestiary.DataSourceModelsDev)
	if len(hist) == 0 {
		t.Fatal("DatasetIngestHistoryFor(models.dev) returned no rows; expected the committed seed row")
	}
	// Ascending by IngestedAt.
	for i := 1; i < len(hist); i++ {
		if hist[i-1].IngestedAt > hist[i].IngestedAt {
			t.Errorf("history not ascending at %d: %q > %q", i, hist[i-1].IngestedAt, hist[i].IngestedAt)
		}
	}
	// Every row carries the queried source id and a committed (non-empty) timestamp.
	for _, di := range hist {
		if di.SourceID != bestiary.DataSourceModelsDev {
			t.Errorf("history row SourceID = %q, want %q", di.SourceID, bestiary.DataSourceModelsDev)
		}
		if di.IngestedAt == "" {
			t.Error("history row has empty IngestedAt; expected a committed snapshot timestamp")
		}
	}
	// The current ingest (DatasetIngestedFor) is the maximum = last history element.
	cur, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceModelsDev)
	if !ok {
		t.Fatal("DatasetIngestedFor(models.dev) missing")
	}
	last := hist[len(hist)-1]
	if cur != last {
		t.Errorf("DatasetIngestedFor = %+v, want the maximum history row %+v", cur, last)
	}

	// Mutating the returned history must not leak into the memoized table.
	hist[0].IngestedAt = "mutated-by-test"
	again := bestiary.DatasetIngestHistoryFor(bestiary.DataSourceModelsDev)
	if again[0].IngestedAt == "mutated-by-test" {
		t.Error("DatasetIngestHistoryFor is not copy-isolated: a caller mutation leaked into the cached table")
	}

	// An unknown source has no history.
	if got := bestiary.DatasetIngestHistoryFor("no-such-source"); got != nil {
		t.Errorf("DatasetIngestHistoryFor(unknown) = %v, want nil", got)
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
