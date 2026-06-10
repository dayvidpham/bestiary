package bestiary

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
