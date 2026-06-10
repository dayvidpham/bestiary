package bestiary

import (
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

// TestValidateDataSourceFKs_RejectsOrphans falsifies a validator that skips its FK
// checks: a constructed table with an orphan INGEST row, and a constructed orphan
// ATTESTATION row, must each be rejected. This exercises the ingested-FK loop and
// the entity_source-source-FK loop independently of the parse-time guard.
func TestValidateDataSourceFKs_RejectsOrphans(t *testing.T) {
	good := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev, URI: "https://models.dev/api.json"}},
		ingested: map[DataSourceID]DatasetIngested{DataSourceModelsDev: {SourceID: DataSourceModelsDev}},
	}
	if err := validateDataSourceFKs(good, []EntitySource{{EntityKey: "k", SourceID: DataSourceModelsDev}}); err != nil {
		t.Fatalf("validateDataSourceFKs rejected a sound table: %v", err)
	}

	orphanIngest := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev}},
		ingested: map[DataSourceID]DatasetIngested{"ghost": {SourceID: "ghost"}},
	}
	if err := validateDataSourceFKs(orphanIngest, nil); err == nil {
		t.Error("validateDataSourceFKs accepted an ingest whose source_id has no DataSource; want rejection")
	}

	orphanAttest := &dataSourceTable{
		byID:     map[DataSourceID]DataSource{DataSourceModelsDev: {ID: DataSourceModelsDev}},
		ingested: map[DataSourceID]DatasetIngested{},
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
