package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
)

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

// MarshalJSON serializes CanonicalScheme as a JSON string (e.g. "canonical").
// This satisfies the bestiary.schema.json $defs/CanonicalScheme enum contract,
// which declares the type as a string — not a number.
func (s CanonicalScheme) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON deserializes a JSON string into a CanonicalScheme.
// Accepted values: "canonical", "huggingface", "purl", "raw" (case-insensitive).
// Returns an error if the string is not recognized.
func (s *CanonicalScheme) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf(
			"bestiary: CanonicalScheme.UnmarshalJSON: expected a JSON string, got %s;\n"+
				"  what went wrong: cannot unmarshal non-string JSON value into CanonicalScheme\n"+
				"  why: CanonicalScheme is serialized as a string enum (\"canonical\", \"huggingface\", \"purl\", \"raw\")\n"+
				"  where: canonical.go CanonicalScheme.UnmarshalJSON\n"+
				"  how to fix: provide a JSON string value for CanonicalScheme",
			b,
		)
	}
	parsed, err := ParseScheme(strings.ToLower(raw))
	if err != nil {
		return fmt.Errorf(
			"bestiary: CanonicalScheme.UnmarshalJSON: unrecognized scheme value %q;\n"+
				"  what went wrong: %w\n"+
				"  where: canonical.go CanonicalScheme.UnmarshalJSON\n"+
				"  how to fix: use one of \"canonical\", \"huggingface\", \"purl\", \"raw\"",
			raw, err,
		)
	}
	*s = parsed
	return nil
}

// ParseInputFormat converts a --format flag string to an InputFormat.
// Accepted values: "peasant", "huggingface", "hf", "purl", "raw" (case-insensitive).
// Returns an error if the string is not a recognized input format.
// Used by CLI flag parsing (--format flag in cmd/bestiary show).
func ParseInputFormat(s string) (InputFormat, error) {
	switch strings.ToLower(s) {
	case "peasant", "canonical":
		return InputFormatPeasant, nil
	case "huggingface", "hf":
		return InputFormatHuggingFace, nil
	case "purl":
		return InputFormatPURL, nil
	case "raw":
		return InputFormatRaw, nil
	default:
		return InputFormatPeasant, fmt.Errorf(
			"bestiary: unrecognized input format %q\n"+
				"  what: %q is not a valid input format\n"+
				"  why: only %q, %q, %q, and %q are accepted (\"hf\" is an alias for \"huggingface\")\n"+
				"  where: ParseInputFormat\n"+
				"  how to fix: pass one of the accepted format strings",
			s, s, "peasant", "huggingface", "purl", "raw",
		)
	}
}

// inputFormatToScheme maps an InputFormat to the corresponding CanonicalScheme
// for Resolve dispatch. Returns SchemeCanonical for InputFormatPeasant.
func inputFormatToScheme(f InputFormat) CanonicalScheme {
	switch f {
	case InputFormatHuggingFace:
		return SchemeHuggingFace
	case InputFormatPURL:
		return SchemePURL
	case InputFormatRaw:
		return SchemeRaw
	default:
		// InputFormatPeasant → SchemeCanonical
		return SchemeCanonical
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
				"  what: %q is not a valid CanonicalScheme\n"+
				"  why: only %q, %q, %q, and %q are accepted\n"+
				"  where: ParseScheme\n"+
				"  how to fix: pass one of the accepted scheme strings",
			s, s, "canonical", "huggingface", "purl", "raw",
		)
	}
}
