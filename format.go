package bestiary

import "io"

// OutputFormat specifies how models are rendered for display.
type OutputFormat string

const (
	FormatJSON  OutputFormat = "json"
	FormatYAML  OutputFormat = "yaml"
	FormatTable OutputFormat = "table"
)

// FormatModels writes a list of models to w in the specified format.
func FormatModels(w io.Writer, models []ModelInfo, format OutputFormat) error {
	return nil // stub — implemented in L3
}

// FormatModel writes a single model to w in the specified format.
func FormatModel(w io.Writer, model ModelInfo, format OutputFormat) error {
	return nil // stub — implemented in L3
}
