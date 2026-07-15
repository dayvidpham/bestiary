package bestiary

import (
	"reflect"
	"strings"
	"testing"
)

// TestParseDataSourceTable_RejectsOrphanIngest is the negative FK gate behind
// ValidateDataSourceTable: an ingest row whose source_id has no sources[] row must
// be REJECTED at parse with an actionable error (an orphan attestation), never
// silently admitted. This is the injectable parse/validate seam that lets the
// orphan case be exercised without mutating the shipped data file.
func TestParseDataSourceTable_RejectsOrphanIngest(t *testing.T) {
	const bad = `{
	  "schema_version": 2,
	  "sources": [
	    {"id": "models.dev", "uri": "https://models.dev/api.json", "canonical_name": "models.dev"}
	  ],
	  "ingested": [
	    {"source_id": "ghost-source", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 2}
	  ]
	}`
	_, err := parseDataSourceTable([]byte(bad))
	if err == nil {
		t.Fatal("parseDataSourceTable accepted an ingest with an unknown source_id; want a rejection error")
	}
	if !strings.Contains(err.Error(), "ghost-source") {
		t.Errorf("error = %q, want it to name the orphan source_id", err.Error())
	}
}

// TestParseDataSourceTable_RejectsDuplicateURI guards the second candidate key: two
// dimension rows may not share a uri (URI is UNIQUE).
func TestParseDataSourceTable_RejectsDuplicateURI(t *testing.T) {
	const bad = `{
	  "schema_version": 2,
	  "sources": [
	    {"id": "a", "uri": "https://example.test", "canonical_name": "A"},
	    {"id": "b", "uri": "https://example.test", "canonical_name": "B"}
	  ]
	}`
	_, err := parseDataSourceTable([]byte(bad))
	if err == nil {
		t.Fatal("parseDataSourceTable accepted a duplicate uri; want a rejection (uri is a candidate key)")
	}
	if !strings.Contains(err.Error(), "uri") {
		t.Errorf("error = %q, want it to name the duplicate-uri violation", err.Error())
	}
}

// TestParseDataSourceTable_RejectsDuplicateID guards the primary key.
func TestParseDataSourceTable_RejectsDuplicateID(t *testing.T) {
	const bad = `{
	  "sources": [
	    {"id": "dup", "uri": "https://a.test", "canonical_name": "A"},
	    {"id": "dup", "uri": "https://b.test", "canonical_name": "B"}
	  ]
	}`
	if _, err := parseDataSourceTable([]byte(bad)); err == nil {
		t.Fatal("duplicate id accepted; want rejection (id is the primary key)")
	}
}

// TestParseDataSourceTable_MultiRowHistorySorted pins the v3 append-only history:
// a source MAY carry multiple ingested rows, and the loader must (1) accept them,
// (2) sort each source's history ASCENDING by ingested_at regardless of file order,
// and (3) expose the MAX as the current ingest via DatasetIngestedFor's seam. The
// fixture lists the rows out of order (newest first) so a dropped sort is falsified.
//
// It also drives the DatasetIngestedFor / DatasetIngestHistoryFor seams over the
// MULTI-ROW table (the shipped seed is single-row per source, so those public
// lookups have no MAX-vs-MIN teeth there): datasetIngestedFrom must return the
// MAX(ingested_at) row — a hist[0]/MIN mutant fails here — and
// datasetIngestHistoryFrom must return the full ascending, copy-isolated history.
func TestParseDataSourceTable_MultiRowHistorySorted(t *testing.T) {
	const src = `{
	  "schema_version": 3,
	  "sources": [
	    {"id": "models.dev", "uri": "https://models.dev/api.json", "canonical_name": "models.dev"}
	  ],
	  "ingested": [
	    {"source_id": "models.dev", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 3},
	    {"source_id": "models.dev", "ingested_at": "2026-01-01T00:00:00Z", "parser_schema": 2},
	    {"source_id": "models.dev", "ingested_at": "2026-03-15T00:00:00Z", "parser_schema": 5}
	  ]
	}`
	tbl, err := parseDataSourceTable([]byte(src))
	if err != nil {
		t.Fatalf("parseDataSourceTable rejected a valid multi-row history: %v", err)
	}
	hist := tbl.ingested[DataSourceModelsDev]
	if len(hist) != 3 {
		t.Fatalf("history length = %d, want 3 (all rows kept)", len(hist))
	}
	wantOrder := []string{"2026-01-01T00:00:00Z", "2026-03-15T00:00:00Z", "2026-06-09T00:00:00Z"}
	for i, w := range wantOrder {
		if hist[i].IngestedAt != w {
			t.Errorf("history[%d].IngestedAt = %q, want %q (ascending regardless of file order)", i, hist[i].IngestedAt, w)
		}
	}

	// DatasetIngestedFor seam: the CURRENT ingest is the MAX(ingested_at) row, not
	// the first (minimum) history row. hist[0] is 2026-01-01 (parser_schema 2), so a
	// hist[0]/MIN selection mutant is caught by BOTH the timestamp and the schema.
	cur, ok := datasetIngestedFrom(tbl, DataSourceModelsDev)
	if !ok {
		t.Fatal("datasetIngestedFrom(models.dev) missing over a 3-row history")
	}
	if cur.IngestedAt != "2026-06-09T00:00:00Z" || cur.ParserSchema != 3 {
		t.Errorf("current ingest = %+v, want the MAX row {2026-06-09T00:00:00Z, parser_schema 3}; a hist[0]/MIN selection returns 2026-01-01 (parser_schema 2)", cur)
	}
	if _, ok := datasetIngestedFrom(tbl, "no-such-source"); ok {
		t.Error("datasetIngestedFrom(unknown) reported ok; want false")
	}

	// DatasetIngestHistoryFor seam: full ascending history, last element == current,
	// and copy-isolated from the cached table.
	got := datasetIngestHistoryFrom(tbl, DataSourceModelsDev)
	if len(got) != len(wantOrder) {
		t.Fatalf("datasetIngestHistoryFrom length = %d, want %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].IngestedAt != w {
			t.Errorf("datasetIngestHistoryFrom[%d].IngestedAt = %q, want %q (ascending)", i, got[i].IngestedAt, w)
		}
	}
	if got[len(got)-1] != cur {
		t.Errorf("history last element %+v != current ingest %+v", got[len(got)-1], cur)
	}
	got[0].IngestedAt = "mutated-by-test"
	if again := datasetIngestHistoryFrom(tbl, DataSourceModelsDev); again[0].IngestedAt != "2026-01-01T00:00:00Z" {
		t.Error("datasetIngestHistoryFrom is not copy-isolated: a caller mutation leaked into the cached table")
	}
	if got := datasetIngestHistoryFrom(tbl, "no-such-source"); got != nil {
		t.Errorf("datasetIngestHistoryFrom(unknown) = %v, want nil", got)
	}
}

// TestParseDataSourceTable_RejectsExactDuplicateIngest pins the composite primary
// key of the append-only log: two rows sharing the SAME (source_id, ingested_at)
// are the same fact twice and must be rejected with an actionable error that names
// the timestamp. A different ingested_at for the same source is NOT a duplicate
// (that is the history case, covered above).
func TestParseDataSourceTable_RejectsExactDuplicateIngest(t *testing.T) {
	const bad = `{
	  "schema_version": 3,
	  "sources": [
	    {"id": "models.dev", "uri": "https://models.dev/api.json", "canonical_name": "models.dev"}
	  ],
	  "ingested": [
	    {"source_id": "models.dev", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 3},
	    {"source_id": "models.dev", "ingested_at": "2026-06-09T00:00:00Z", "parser_schema": 2}
	  ]
	}`
	_, err := parseDataSourceTable([]byte(bad))
	if err == nil {
		t.Fatal("parseDataSourceTable accepted an exact-duplicate (source_id, ingested_at); want a rejection")
	}
	if !strings.Contains(err.Error(), "2026-06-09T00:00:00Z") {
		t.Errorf("error = %q, want it to name the duplicate ingested_at", err.Error())
	}
}

// TestSafeDataSourceTable_DegradesToNoSources exercises the runtime degrade twin of
// the codegen Validate* hard-fail: when the table fails to load (parse error) or is
// nil, safeDataSourceTable must fall back to a non-nil EMPTY table so lookups miss
// ("no sources") and never nil-deref or panic. Mirrors safeLineageTable.
func TestSafeDataSourceTable_DegradesToNoSources(t *testing.T) {
	badTable, err := parseDataSourceTable([]byte("}{ not valid json"))
	if err == nil {
		t.Fatal("parseDataSourceTable accepted malformed JSON; expected a load error to drive the degrade path")
	}

	for _, tc := range []struct {
		name  string
		table *dataSourceTable
		err   error
	}{
		{"load error", badTable, err},
		{"nil table, nil error", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeDataSourceTable(tc.table, tc.err)
			if got == nil {
				t.Fatal("safeDataSourceTable returned nil; the degrade fallback must be non-nil")
			}
			if got.byID == nil || got.ingested == nil {
				t.Fatal("degraded table has a nil map; lookups would panic")
			}
			if _, ok := got.byID[DataSourceModelsDev]; ok {
				t.Error("degraded byID resolved a source; want a miss")
			}
			if _, ok := got.ingested[DataSourceModelsDev]; ok {
				t.Error("degraded ingested resolved a row; want a miss")
			}
		})
	}
}

// TestEmbeddedDataSourceTable_Valid confirms the shipped curated datasources.json
// loads and validates cleanly — the production-data counterpart of the negative
// parse tests above. It also asserts the loader leaves nothing nil.
func TestEmbeddedDataSourceTable_Valid(t *testing.T) {
	tbl, err := loadDataSourceTable()
	if err != nil {
		t.Fatalf("embedded datasources.json failed to load: %v", err)
	}
	if tbl == nil || tbl.byID == nil || tbl.ingested == nil {
		t.Fatal("loaded table or one of its maps is nil")
	}
	if err := ValidateDataSourceTable(); err != nil {
		t.Fatalf("embedded datasources.json failed FK validation: %v", err)
	}
	if err := ValidateEntitySourceTable(); err != nil {
		t.Fatalf("embedded entity↔source relation failed key-resolves validation: %v", err)
	}
}

// TestSortedSources_Sorts falsifies a dropped/no-op projection sort: given a
// REVERSED first-seen attestation order, sortedSources must return ascending order.
// The shipped data happens to attest models.dev first (lexically smallest), so a
// missing sort would survive a whole-registry test; this seam exercises the sort
// directly with input whose first-seen order differs from sorted order.
func TestSortedSources_Sorts(t *testing.T) {
	in := []DataSourceID{DataSourceOllama, DataSourceModelsDev}
	got := sortedSources(in)
	want := []DataSourceID{DataSourceModelsDev, DataSourceOllama}
	if len(got) != len(want) {
		t.Fatalf("sortedSources(%v) = %v, want %v", in, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortedSources(%v) = %v, want %v (ascending)", in, got, want)
		}
	}
	// The input must not be mutated (a fresh slice is returned).
	if in[0] != DataSourceOllama {
		t.Error("sortedSources mutated its input slice")
	}
	if sortedSources(nil) != nil {
		t.Error("sortedSources(nil) should return nil")
	}
}

// TestBuildEntitySourceRelation_SortsRegardlessOfAttestOrder falsifies the
// projection-sort WIRING, not just the sortedSources seam. It feeds the
// materializer (the single site that sorts the projection in production, consumed
// by both Entity.Sources and EntitySources) a key whose first-seen attestation
// order is REVERSED ([ollama, models.dev]) and asserts that BOTH the per-entity
// projection AND the flat rows come out ascending. The shipped corpus attests
// models.dev (lexically smallest) first, so first-seen order coincidentally equals
// sorted order — a call site that copied the first-seen list unsorted would pass
// the whole public suite on shipped data. Driving the materializer with adversarial
// order kills that bypass directly.
func TestBuildEntitySourceRelation_SortsRegardlessOfAttestOrder(t *testing.T) {
	const key = "adversarial@1#7b{}"
	firstSeen := map[string][]DataSourceID{
		key: {DataSourceOllama, DataSourceModelsDev}, // reversed vs sorted
	}
	rel := buildEntitySourceRelation([]string{key}, firstSeen)

	want := []DataSourceID{DataSourceModelsDev, DataSourceOllama}

	// byEntity is exactly the projection Entity.Sources holds and EntitySources copies.
	if proj := rel.byEntity[key]; !reflect.DeepEqual(proj, want) {
		t.Fatalf("byEntity[%q] = %v, want %v (ascending regardless of first-seen order)", key, proj, want)
	}

	// The flat rows for the key must also be ascending (the explicit total order).
	var rows []DataSourceID
	for _, r := range rel.rows {
		if r.EntityKey == key {
			rows = append(rows, r.SourceID)
		}
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rel.rows for %q = %v, want %v (explicit (EntityKey, SourceID) order)", key, rows, want)
	}
}

// TestValidateDataSourceFKs_RejectsOrphans falsifies a validator that skips its FK
// checks: a constructed table with an orphan INGEST row, and a constructed orphan
// ATTESTATION row, must each be rejected. This exercises the ingested-FK loop and
// the entity_source-source-FK loop independently of the parse-time guard.
func TestValidateDataSourceFKs_RejectsOrphans(t *testing.T) {
	good := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev, URI: "https://models.dev/api.json"}},
		ingested: map[DataSourceID][]DatasetIngested{DataSourceModelsDev: {{SourceID: DataSourceModelsDev}}},
	}
	if err := validateDataSourceFKs(good, []EntitySource{{EntityKey: "k", SourceID: DataSourceModelsDev}}); err != nil {
		t.Fatalf("validateDataSourceFKs rejected a sound table: %v", err)
	}

	orphanIngest := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev}},
		ingested: map[DataSourceID][]DatasetIngested{"ghost": {{SourceID: "ghost"}}},
	}
	if err := validateDataSourceFKs(orphanIngest, nil); err == nil {
		t.Error("validateDataSourceFKs accepted an ingest whose source_id has no DataSource; want rejection")
	}

	orphanAttest := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev}},
		ingested: map[DataSourceID][]DatasetIngested{},
	}
	if err := validateDataSourceFKs(orphanAttest, []EntitySource{{EntityKey: "k", SourceID: "ghost"}}); err == nil {
		t.Error("validateDataSourceFKs accepted an attestation to a source with no DataSource; want rejection")
	}
}

// TestValidateEntityKeyFKs_RejectsOrphan falsifies a key guard that always passes:
// an attestation keyed to an entity the resolver rejects must be reported.
func TestValidateEntityKeyFKs_RejectsOrphan(t *testing.T) {
	resolves := func(k string) bool { return k == "real-entity" }
	if err := validateEntityKeyFKs([]EntitySource{{EntityKey: "real-entity", SourceID: DataSourceModelsDev}}, resolves); err != nil {
		t.Fatalf("validateEntityKeyFKs rejected a resolvable key: %v", err)
	}
	if err := validateEntityKeyFKs([]EntitySource{{EntityKey: "ghost-entity", SourceID: DataSourceModelsDev}}, resolves); err == nil {
		t.Error("validateEntityKeyFKs accepted a key that resolves to no entity; want rejection")
	}
}

// TestEntitySourceRelation_RowOrderPinned asserts the in-memory join relation is
// sorted by (EntityKey, then SourceID) via the explicit sort — NOT first-seen — so
// every consumer (codegen emission included) observes a byte-deterministic order.
func TestEntitySourceRelation_RowOrderPinned(t *testing.T) {
	rel := loadEntitySourceRelation()
	if len(rel.rows) == 0 {
		t.Fatal("relation has no rows; expected the static registry to attest entities")
	}
	for i := 1; i < len(rel.rows); i++ {
		prev, cur := rel.rows[i-1], rel.rows[i]
		if prev.EntityKey > cur.EntityKey {
			t.Fatalf("rows not sorted by EntityKey at %d: %q > %q", i, prev.EntityKey, cur.EntityKey)
		}
		if prev.EntityKey == cur.EntityKey && prev.SourceID > cur.SourceID {
			t.Fatalf("rows not sorted by SourceID within key %q at %d: %q > %q",
				cur.EntityKey, i, prev.SourceID, cur.SourceID)
		}
	}
	// The per-entity projection must agree with the relation rows for the same key.
	for key, proj := range rel.byEntity {
		var fromRows []DataSourceID
		for _, r := range rel.rows {
			if r.EntityKey == key {
				fromRows = append(fromRows, r.SourceID)
			}
		}
		if len(fromRows) != len(proj) {
			t.Fatalf("entity %q: projection has %d sources, relation rows have %d", key, len(proj), len(fromRows))
		}
		for i := range proj {
			if proj[i] != fromRows[i] {
				t.Fatalf("entity %q: projection[%d]=%q != relation row %q", key, i, proj[i], fromRows[i])
			}
		}
	}
}
