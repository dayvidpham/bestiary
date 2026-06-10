package bestiary

// DataSourceID is the stable identifier for a data source that provides model
// records to the bestiary registry. The zero value (DataSourceNone, empty
// string) means "no source recorded" and is the correct default for any row
// that has not been assigned a source.
//
// Constants are intentionally few here: the full BCNF provenance types
// (DataSource, DatasetIngested, EntitySource) and their lookup functions belong
// to a later slice that owns the rest of this file.
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
