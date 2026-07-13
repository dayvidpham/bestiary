package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// DataSourceID is the stable identifier for a data source that provides model
// records to the bestiary registry. The zero value (DataSourceNone, empty
// string) means "no source recorded" and is the correct default for any row
// that has not been assigned a source.
//
// This file is the home of the data-source provenance module: the DataSourceID
// type and well-known source constants; the BCNF provenance types (DataSource
// dimension, DatasetIngested fact, EntitySource join row); the curated loader for
// parse/data/datasources.json with its runtime degrade seam; the public lookups
// (KnownDataSources, DataSourceByID, DatasetIngestedFor, DatasetIngestHistoryFor,
// EntitySources); and the
// codegen FK guards (ValidateDataSourceTable, ValidateEntitySourceTable). The
// entity↔source join relation itself is built by the registry aggregate in
// registry.go (loadEntityIndex / buildEntitySourceRelation); EntitySources here
// reads that relation's per-entity projection.
type DataSourceID string

const (
	// DataSourceNone is the zero value: no source recorded for this row.
	DataSourceNone DataSourceID = ""

	// DataSourceModelsDev identifies the models.dev API as the originating
	// data source.
	DataSourceModelsDev DataSourceID = "models.dev"

	// DataSourceOllama identifies the Ollama registry as the originating
	// data source.
	DataSourceOllama DataSourceID = "ollama"
)

// --------------------------------------------------------------------------
// BCNF provenance types (the data-source dimension + its join table)
// --------------------------------------------------------------------------

// DataSource is the BCNF dimension row for a single originating data source. Its
// primary key is ID; URI is a second candidate key (each source has a distinct
// fetch endpoint), so ValidateDataSourceTable enforces URI uniqueness too.
//
//   - ID is the stable DataSourceID (e.g. DataSourceModelsDev).
//   - URI is the canonical fetch endpoint (e.g. https://models.dev/api.json).
//   - CanonicalName is the human-facing label for display.
type DataSource struct {
	ID            DataSourceID
	URI           string
	CanonicalName string
}

// DatasetIngested records one ingest of a data source. The URI is deliberately
// ABSENT: it is a transitive dependency (URI depends on SourceID via DataSource),
// so it is reached by joining to DataSource via SourceID rather than duplicated
// here — this is the BCNF normalization that removes the transitive dependency.
//
// A source may have MANY DatasetIngested rows: the ingest log is append-only, so a
// source accumulates one row per distinct ingest instant. The identity of a row is
// the composite (SourceID, IngestedAt); the CURRENT ingest for a source is the row
// with the maximum IngestedAt (DatasetIngestedFor), and the full ordered history is
// DatasetIngestHistoryFor.
//
//   - SourceID is the DataSourceID that was ingested (FK to DataSource).
//   - IngestedAt is a COMMITTED snapshot timestamp from datasources.json. It is
//     never computed at load or codegen time — pinning it in the data file is what
//     keeps generated output byte-deterministic across runs.
//   - ParserSchema is the curated-data schema version this ingest was parsed under.
type DatasetIngested struct {
	SourceID     DataSourceID
	IngestedAt   string
	ParserSchema int
}

// EntitySource is one row of the BCNF join table relating an entity to a data
// source that attests it. The primary key is the composite (EntityKey, SourceID);
// an entity attested by N sources has N rows. EntityKey is an EntityRef.String()
// value; SourceID is a FK to DataSource.
//
// Attestation rule (the join's existence condition, applied by the registry
// aggregate in registry.go's loadEntityIndex): every static row attests
// DataSourceModelsDev — a row whose Source carrier is empty (DataSourceNone) means
// the models.dev origin is implicit; a row whose Source names a further, distinct
// source (e.g. ollama) is a models.dev row ENRICHED with that source's data, so it
// DUAL-attests BOTH DataSourceModelsDev AND that source. Thus a pure models.dev
// entity has one row {models.dev} and an ollama-enriched entity has two rows
// {models.dev, ollama}. The same rule is stated at registry.go's aggregate comment
// and the parse/data/datasources.json _comment.
type EntitySource struct {
	EntityKey string
	SourceID  DataSourceID
}

// --------------------------------------------------------------------------
// Curated data-source table loading (parse/data/datasources.json) — go:embed +
// sync.Once, mirroring loadLineageTable / loadModifierClassTable determinism.
// --------------------------------------------------------------------------

// dataSourceJSON is one DataSource dimension row as curated in datasources.json.
type dataSourceJSON struct {
	ID            DataSourceID `json:"id"`
	URI           string       `json:"uri"`
	CanonicalName string       `json:"canonical_name"`
}

// datasetIngestedJSON is one current-ingest row as curated in datasources.json.
// It carries NO uri (reached via the DataSource join); IngestedAt is a committed
// snapshot, never a wall-clock stamp.
type datasetIngestedJSON struct {
	SourceID     DataSourceID `json:"source_id"`
	IngestedAt   string       `json:"ingested_at"`
	ParserSchema int          `json:"parser_schema"`
}

// dataSourceFileJSON is the top-level shape of parse/data/datasources.json.
type dataSourceFileJSON struct {
	Comment       string                `json:"_comment"`
	SchemaVersion int                   `json:"schema_version"`
	Sources       []dataSourceJSON      `json:"sources"`
	Ingested      []datasetIngestedJSON `json:"ingested"`
}

// dataSourceTable is the parsed, validated curated data-source dimension + its
// per-source ingest history.
//
//   - sources preserves the curated file order so KnownDataSources is deterministic.
//   - byID indexes the dimension rows by DataSourceID for O(1) FK resolution.
//   - ingested maps a DataSourceID to its ingest history: a slice of DatasetIngested
//     sorted ASCENDING by IngestedAt. The current ingest is the last element (the
//     maximum IngestedAt); DatasetIngestedFor returns it and DatasetIngestHistoryFor
//     returns the whole slice.
type dataSourceTable struct {
	sources  []DataSource
	byID     map[DataSourceID]DataSource
	ingested map[DataSourceID][]DatasetIngested
}

// emptyDataSourceTable is the degraded (load-failure) value: a non-nil table whose
// lookups all miss, so the public lookups return zero/false without ever panicking.
func emptyDataSourceTable() *dataSourceTable {
	return &dataSourceTable{
		sources:  nil,
		byID:     map[DataSourceID]DataSource{},
		ingested: map[DataSourceID][]DatasetIngested{},
	}
}

var (
	dataSourceOnce sync.Once
	dataSourceTbl  *dataSourceTable
	dataSourceErr  error
)

// loadDataSourceTable reads and validates parse/data/datasources.json from the
// embedded filesystem exactly once (sync.Once). The cached error is non-nil when
// the file is missing, malformed, or fails dimension validation (duplicate ID,
// duplicate URI, or an ingest whose source_id has no DataSource); the validate
// functions surface it so codegen can fail loudly on bad curation.
func loadDataSourceTable() (*dataSourceTable, error) {
	dataSourceOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/datasources.json")
		if err != nil {
			dataSourceErr = fmt.Errorf(
				"bestiary datasource: load datasources.json: %w\n"+
					"  What: cannot read the embedded data-source table\n"+
					"  Where: parse/data/datasources.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/datasources.json is present before building",
				err,
			)
			return
		}
		dataSourceTbl, dataSourceErr = parseDataSourceTable(raw)
	})
	return dataSourceTbl, dataSourceErr
}

// loadDataSourceTableSafe returns the cached table, or an empty (degraded) table
// when loading failed. It never returns nil and never panics — runtime data-source
// lookups degrade to "no sources" rather than aborting the program.
func loadDataSourceTableSafe() *dataSourceTable {
	return safeDataSourceTable(loadDataSourceTable())
}

// safeDataSourceTable is the testable degrade seam behind loadDataSourceTableSafe:
// it returns t when loading succeeded, or a non-nil EMPTY table when err is non-nil
// or t is nil. It is the runtime-degrade twin of the codegen Validate* hard-fail —
// at runtime a bad/missing table yields "no sources", never a panic.
func safeDataSourceTable(t *dataSourceTable, err error) *dataSourceTable {
	if err != nil || t == nil {
		return emptyDataSourceTable()
	}
	return t
}

// parseDataSourceTable parses and validates the curated data-source JSON. It is the
// testable seam behind loadDataSourceTable: it rejects a dimension with an empty ID,
// a duplicate ID, a duplicate URI (URI is a candidate key), or an ingest row whose
// source_id does not resolve to a curated DataSource — each with an actionable
// error. On success it returns a fully built table.
func parseDataSourceTable(raw []byte) (*dataSourceTable, error) {
	var file dataSourceFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary datasource: parse datasources.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/datasources.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := emptyDataSourceTable()
	uriSeen := map[string]DataSourceID{}
	for i, s := range file.Sources {
		if s.ID == "" {
			return nil, fmt.Errorf(
				"bestiary datasource: invalid source #%d: empty id\n"+
					"  What: a data-source dimension row has no id\n"+
					"  Where: parse/data/datasources.json sources[%d]\n"+
					"  Why: id is the primary key of the data-source dimension\n"+
					"  How to fix: set a non-empty id (e.g. \"models.dev\")",
				i, i,
			)
		}
		if _, dup := tbl.byID[s.ID]; dup {
			return nil, fmt.Errorf(
				"bestiary datasource: duplicate source id %q at #%d\n"+
					"  What: two data-source dimension rows share a primary key\n"+
					"  Where: parse/data/datasources.json sources[%d]\n"+
					"  Why: id is the primary key and must be unique\n"+
					"  How to fix: remove or rename the duplicate id",
				s.ID, i, i,
			)
		}
		if s.URI == "" {
			return nil, fmt.Errorf(
				"bestiary datasource: source %q (#%d): empty uri\n"+
					"  What: a data-source dimension row has no uri\n"+
					"  Where: parse/data/datasources.json sources[%d].uri\n"+
					"  Why: uri is a second candidate key; ingest rows reach it by joining on source_id\n"+
					"  How to fix: set the canonical fetch endpoint uri",
				s.ID, i, i,
			)
		}
		if prev, dup := uriSeen[s.URI]; dup {
			return nil, fmt.Errorf(
				"bestiary datasource: duplicate uri %q on sources %q and %q\n"+
					"  What: two data-source rows share a uri\n"+
					"  Where: parse/data/datasources.json sources[%d].uri\n"+
					"  Why: uri is a second candidate key and must be unique\n"+
					"  How to fix: give each source a distinct uri",
				s.URI, prev, s.ID, i,
			)
		}
		uriSeen[s.URI] = s.ID
		ds := DataSource{ID: s.ID, URI: s.URI, CanonicalName: s.CanonicalName}
		tbl.sources = append(tbl.sources, ds)
		tbl.byID[s.ID] = ds
	}

	// The ingest log is append-only history: a source MAY carry multiple rows. Only
	// an EXACT-duplicate (source_id, ingested_at) is rejected — that composite is the
	// append-only primary key, so a repeat is the same fact asserted twice.
	seenIngest := map[string]int{}
	for i, ing := range file.Ingested {
		if _, ok := tbl.byID[ing.SourceID]; !ok {
			return nil, fmt.Errorf(
				"bestiary datasource: ingest #%d references unknown source_id %q\n"+
					"  What: a dataset-ingested row names a source that is not in the dimension\n"+
					"  Where: parse/data/datasources.json ingested[%d].source_id\n"+
					"  Why: source_id is a foreign key into the sources table\n"+
					"  How to fix: add a sources[] row for %q, or correct the source_id",
				i, ing.SourceID, i, ing.SourceID,
			)
		}
		compositeKey := string(ing.SourceID) + "\x00" + ing.IngestedAt
		if prev, dup := seenIngest[compositeKey]; dup {
			return nil, fmt.Errorf(
				"bestiary datasource: duplicate ingest for source_id %q at ingested_at %q (rows #%d and #%d)\n"+
					"  What: two dataset-ingested rows share the same (source_id, ingested_at)\n"+
					"  Where: parse/data/datasources.json ingested[%d]\n"+
					"  Why: (source_id, ingested_at) is the composite primary key of the append-only ingest log; an exact duplicate is the same fact asserted twice\n"+
					"  How to fix: remove the duplicate ingested row, or correct its ingested_at",
				ing.SourceID, ing.IngestedAt, prev, i, i,
			)
		}
		seenIngest[compositeKey] = i
		tbl.ingested[ing.SourceID] = append(tbl.ingested[ing.SourceID], DatasetIngested{
			SourceID:     ing.SourceID,
			IngestedAt:   ing.IngestedAt,
			ParserSchema: ing.ParserSchema,
		})
	}

	// Sort each source's history ascending by IngestedAt so the current ingest is the
	// last element. Comparison is lexicographic on the RFC3339 string; committed
	// snapshots are UTC (Z-suffixed), so lexicographic order equals chronological.
	for id := range tbl.ingested {
		hist := tbl.ingested[id]
		sort.Slice(hist, func(a, b int) bool { return hist[a].IngestedAt < hist[b].IngestedAt })
		tbl.ingested[id] = hist
	}
	return tbl, nil
}

// --------------------------------------------------------------------------
// Public lookups
// --------------------------------------------------------------------------

// KnownDataSources returns the curated data-source dimension in curated file order.
// The result is a fresh slice (callers cannot mutate the cached table). On a
// load failure it returns nil (graceful degrade), never panicking.
func KnownDataSources() []DataSource {
	t := loadDataSourceTableSafe()
	if len(t.sources) == 0 {
		return nil
	}
	return append([]DataSource(nil), t.sources...)
}

// DataSourceByID returns the DataSource dimension row for id and whether it exists.
// It is the FK-join entry point used to resolve a SourceID (on a DatasetIngested or
// EntitySource) to its uri/canonical-name.
func DataSourceByID(id DataSourceID) (DataSource, bool) {
	ds, ok := loadDataSourceTableSafe().byID[id]
	return ds, ok
}

// DatasetIngestedFor returns the CURRENT DatasetIngested for the source id — the
// row with the maximum IngestedAt — and whether any ingest exists. The returned
// value carries NO uri; resolve the uri via DataSourceByID(id) (the BCNF join) when
// it is needed. Use DatasetIngestHistoryFor for the full ordered history.
func DatasetIngestedFor(id DataSourceID) (DatasetIngested, bool) {
	hist := loadDataSourceTableSafe().ingested[id]
	if len(hist) == 0 {
		return DatasetIngested{}, false
	}
	// The history is sorted ascending by IngestedAt, so the last element is current.
	return hist[len(hist)-1], true
}

// DatasetIngestHistoryFor returns the full ingest history for the source id, sorted
// ascending by IngestedAt (oldest first), or nil when the source has no ingest
// rows. The result is a fresh slice (callers cannot mutate the cached table). On a
// load failure it returns nil (graceful degrade), never panicking. It is the
// curated-seed counterpart of Store.QueryIngestHistory.
func DatasetIngestHistoryFor(id DataSourceID) []DatasetIngested {
	hist := loadDataSourceTableSafe().ingested[id]
	if len(hist) == 0 {
		return nil
	}
	return append([]DatasetIngested(nil), hist...)
}

// EntitySources returns the sorted, de-duplicated DataSourceIDs that attest the
// entity identified by entityKey (an EntityRef.String() value), or nil when none
// do. It is the join-table lookup behind Entity.Sources: the projection is read
// from the registry-built entity↔source relation and is always sorted ascending by
// DataSourceID for deterministic output.
func EntitySources(entityKey string) []DataSourceID {
	rel := loadEntitySourceRelation()
	src := rel.byEntity[entityKey]
	if len(src) == 0 {
		return nil
	}
	return append([]DataSourceID(nil), src...)
}

// --------------------------------------------------------------------------
// Codegen FK guards
// --------------------------------------------------------------------------

// ValidateDataSourceTable loads the curated data-source table and verifies its
// referential integrity: it returns any load/parse error, and additionally checks
// that every DatasetIngested.SourceID and every EntitySource.SourceID resolves to a
// DataSource (URI uniqueness is enforced at parse time in parseDataSourceTable).
//
// Codegen (cmd/bestiary-gen run()) calls this once, alongside ValidateLineageTable
// and ValidateQuantVRAMTable, and aborts on a non-nil result. Two FK invariants are
// caught at generation time rather than baking an orphan provenance row:
//   - the curated datasources.json is internally sound (no duplicate id/uri, every
//     ingest source_id present in the dimension); and
//   - no entity↔source attestation names a source absent from the dimension. The
//     attestation rows come from the registry COMPILED INTO THIS BINARY (the
//     previously generated staticModels), and their SourceIDs come from the
//     attestation rule (models.dev + each row's curated Source). datasources.json is
//     an INDEPENDENT curated file, so this cross-input check is genuinely
//     falsifiable: a Source added to the model curation without a matching
//     datasources.json row is rejected — on the next codegen run after that Source
//     bakes into staticModels.
func ValidateDataSourceTable() error {
	t, err := loadDataSourceTable()
	if err != nil {
		return err
	}
	return validateDataSourceFKs(t, loadEntitySourceRelation().rows)
}

// validateDataSourceFKs is the testable FK seam behind ValidateDataSourceTable: it
// checks that every DatasetIngested.SourceID and every EntitySource.SourceID in rows
// resolves to a DataSource in t. It takes its inputs explicitly so a constructed
// orphan (an ingest or attestation pointing at an absent source) can be falsified in
// a unit test without mutating the shipped data file. (URI uniqueness is enforced at
// parse time in parseDataSourceTable.)
func validateDataSourceFKs(t *dataSourceTable, rows []EntitySource) error {
	for id := range t.ingested {
		if _, ok := t.byID[id]; !ok {
			return fmt.Errorf(
				"bestiary datasource: dataset_ingested.source_id %q has no DataSource\n"+
					"  What: a current-ingest row references a source not in the dimension\n"+
					"  Where: parse/data/datasources.json ingested[].source_id\n"+
					"  Why: source_id is a foreign key into the sources table\n"+
					"  How to fix: add the missing sources[] row or correct the source_id",
				id,
			)
		}
	}
	for _, es := range rows {
		if _, ok := t.byID[es.SourceID]; !ok {
			return fmt.Errorf(
				"bestiary datasource: entity_source.source_id %q has no DataSource (entity %q)\n"+
					"  What: a join-table row attests an entity to a source not in the dimension\n"+
					"  Where: the registry entity↔source relation\n"+
					"  Why: source_id is a foreign key into the sources table\n"+
					"  How to fix: register the source in parse/data/datasources.json, or correct the attestation rule",
				es.SourceID, es.EntityKey,
			)
		}
	}
	return nil
}

// ValidateEntitySourceTable verifies that every EntityKey in the entity↔source join
// relation resolves to a real registry entity, and surfaces any data-source load
// error first via ValidateDataSourceTable.
//
// It is deliberately NOT wired into codegen. Unlike the lineage key-resolves guard
// (which cross-checks an INDEPENDENT curated file against the registry), the
// entity↔source relation is DERIVED from the registry: loadEntityIndex builds both
// the relation rows and the entity index from the same scan, so every row's
// EntityKey is a key of that index by construction and the resolver here cannot
// fail against the production relation — wiring it into codegen would assert a
// tautology and misleadingly imply the key FK is enforced at generation. The check
// is kept as a public, unit-tested guard (its seam, validateEntityKeyFKs, is
// falsified with a constructed orphan) so it is ready the moment entity↔source rows
// are sourced INDEPENDENTLY of the entity index (e.g. a curated join file or the
// SQLite store), at which point the key FK stops being tautological. Until then the
// genuine codegen FK guard is ValidateDataSourceTable, which cross-checks the
// independent datasources.json.
func ValidateEntitySourceTable() error {
	if err := ValidateDataSourceTable(); err != nil {
		return err
	}
	return validateEntityKeyFKs(loadEntitySourceRelation().rows, func(key string) bool {
		_, ok := entityIndexLookup(key)
		return ok
	})
}

// validateEntityKeyFKs is the testable seam behind ValidateEntitySourceTable: every
// EntityKey in rows must resolve via the supplied resolver (the production resolver
// is the registry entity index). It takes the resolver explicitly so a constructed
// orphan attestation — a row keyed to a non-existent entity — can be falsified in a
// unit test without injecting fake entities into the global registry.
func validateEntityKeyFKs(rows []EntitySource, resolves func(entityKey string) bool) error {
	for _, es := range rows {
		if !resolves(es.EntityKey) {
			return fmt.Errorf(
				"bestiary datasource: entity_source.entity_key %q resolves to no entity (source %q)\n"+
					"  What: a join-table row attests an entity key that is not in the registry\n"+
					"  Where: the registry entity↔source relation\n"+
					"  Why: entity_key is a foreign key into the entity index (EntityRef.String())\n"+
					"  How to fix: ensure the attestation key matches a real entity key, or correct the aggregation",
				es.EntityKey, es.SourceID,
			)
		}
	}
	return nil
}
