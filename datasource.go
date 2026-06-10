package bestiary

import (
	"encoding/json"
	"fmt"
	"sync"
)

// DataSourceID is the stable identifier for a data source that provides model
// records to the bestiary registry. The zero value (DataSourceNone, empty
// string) means "no source recorded" and is the correct default for any row
// that has not been assigned a source.
//
// This file contains the DataSourceID type and the well-known source constants.
// The full BCNF provenance types (DataSource, DatasetIngested, EntitySource)
// and their registry lookup functions live alongside the registry aggregate.
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

// DatasetIngested records the single current ingest of a data source. The URI is
// deliberately ABSENT: it is a transitive (URI depends on SourceID via DataSource),
// so it is reached by joining to DataSource via SourceID rather than duplicated
// here — this is the BCNF normalization that removes the transitive dependency.
// The primary key is SourceID (one current ingest per source; the append-only
// ingest history is deferred to a later schema).
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
// current-ingest rows.
//
//   - sources preserves the curated file order so KnownDataSources is deterministic.
//   - byID indexes the dimension rows by DataSourceID for O(1) FK resolution.
//   - ingested indexes the single current ingest per source by DataSourceID.
type dataSourceTable struct {
	sources  []DataSource
	byID     map[DataSourceID]DataSource
	ingested map[DataSourceID]DatasetIngested
}

// emptyDataSourceTable is the degraded (load-failure) value: a non-nil table whose
// lookups all miss, so the public lookups return zero/false without ever panicking.
func emptyDataSourceTable() *dataSourceTable {
	return &dataSourceTable{
		sources:  nil,
		byID:     map[DataSourceID]DataSource{},
		ingested: map[DataSourceID]DatasetIngested{},
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
					"  Why: uri is a candidate key and the FK-join target for ingest rows\n"+
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
		if _, dup := tbl.ingested[ing.SourceID]; dup {
			return nil, fmt.Errorf(
				"bestiary datasource: duplicate ingest for source_id %q at #%d\n"+
					"  What: two dataset-ingested rows share a source_id\n"+
					"  Where: parse/data/datasources.json ingested[%d]\n"+
					"  Why: the current-ingest table has primary key source_id (single current ingest)\n"+
					"  How to fix: keep one ingest row per source",
				ing.SourceID, i, i,
			)
		}
		tbl.ingested[ing.SourceID] = DatasetIngested{
			SourceID:     ing.SourceID,
			IngestedAt:   ing.IngestedAt,
			ParserSchema: ing.ParserSchema,
		}
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

// DatasetIngestedFor returns the single current DatasetIngested for the source id
// and whether one exists. The returned value carries NO uri; resolve the uri via
// DataSourceByID(id) (the BCNF join) when it is needed.
func DatasetIngestedFor(id DataSourceID) (DatasetIngested, bool) {
	ing, ok := loadDataSourceTableSafe().ingested[id]
	return ing, ok
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
// DataSource, and that URIs are unique (the latter is enforced at parse time).
// Codegen calls this once and aborts on a non-nil result so a dangling source FK is
// caught at generation time rather than producing an orphan attestation at runtime.
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
// relation resolves to a real registry entity (generalizing the lineage key-resolves
// guard). Codegen calls this once and aborts on a non-nil result so an attestation
// keyed to a non-existent entity is caught at generation time. It also surfaces any
// data-source load error so a degraded relation never passes silently.
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
