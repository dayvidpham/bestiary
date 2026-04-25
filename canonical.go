package bestiary

import "fmt"

// CanonicalScheme identifies the string serialization format for a ModelRef.
// Callers use it to select how a model identity is expressed as a string,
// e.g. for CLI output, SBOM generation, or HuggingFace Hub lookups.
type CanonicalScheme int

const (
	// SchemeCanonical is the default slash-separated canonical form:
	//   <provider>/<family>/<variant>@<date>
	// When no variant is present the variant segment is omitted.
	// Provider-specific representations (e.g. anthropic/claude-opus-4-20250514)
	// may be emitted where the canonical triple lacks sufficient granularity.
	SchemeCanonical CanonicalScheme = iota

	// SchemeHuggingFace produces the HuggingFace Hub form:
	//   <provider>/<raw-id>
	// This matches the repo-id used by the HuggingFace Hub API.
	SchemeHuggingFace

	// SchemePURL produces a Package URL (purl-spec + ECMA-427) form:
	//   pkg:huggingface/<provider>/<raw-id>
	SchemePURL

	// SchemeRaw returns the original API model ID verbatim (string(ModelRef.ID)).
	SchemeRaw
)

// String returns a human-readable name for the scheme.
// Used in debug output and error messages; not a serialized form.
func (s CanonicalScheme) String() string {
	switch s {
	case SchemeCanonical:
		return "canonical"
	case SchemeHuggingFace:
		return "huggingface"
	case SchemePURL:
		return "purl"
	case SchemeRaw:
		return "raw"
	default:
		return fmt.Sprintf("CanonicalScheme(%d)", int(s))
	}
}

// ParseScheme converts a string to a CanonicalScheme.
// Accepted values: "canonical", "huggingface", "purl", "raw".
// Returns an error if the string is not a recognized scheme.
// Used by CLI flag parsing (--scheme flag in cmd/bestiary).
func ParseScheme(s string) (CanonicalScheme, error) {
	switch s {
	case "canonical":
		return SchemeCanonical, nil
	case "huggingface":
		return SchemeHuggingFace, nil
	case "purl":
		return SchemePURL, nil
	case "raw":
		return SchemeRaw, nil
	default:
		return SchemeCanonical, fmt.Errorf(
			"bestiary: unrecognized scheme %q\n"+
				"  What: %q is not a valid CanonicalScheme\n"+
				"  Why: only %q, %q, %q, and %q are accepted\n"+
				"  Where: ParseScheme\n"+
				"  How to fix: pass one of the accepted scheme strings",
			s, s, "canonical", "huggingface", "purl", "raw",
		)
	}
}
