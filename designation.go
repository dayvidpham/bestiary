package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
)

// AcceptabilityRating classifies a Designation by its acceptability status,
// following ISO 1087 terminology principles.
//
// All designations generated in this epoch default to AcceptabilityAdmitted.
// Promotion to AcceptabilityPreferred and assignment of AcceptabilityDeprecated
// are deferred to a follow-up curation epoch.
type AcceptabilityRating int

const (
	// AcceptabilityAdmitted is the default rating. The designation is
	// recognized and may be used, but is not the preferred form.
	AcceptabilityAdmitted AcceptabilityRating = iota

	// AcceptabilityPreferred marks the designation as the recommended form.
	// Currently no designations are promoted to Preferred in this epoch.
	AcceptabilityPreferred

	// AcceptabilityDeprecated marks the designation as no longer recommended.
	// Currently no designations are deprecated in this epoch.
	AcceptabilityDeprecated
)

// String returns a human-readable label for the rating.
func (r AcceptabilityRating) String() string {
	switch r {
	case AcceptabilityAdmitted:
		return "admitted"
	case AcceptabilityPreferred:
		return "preferred"
	case AcceptabilityDeprecated:
		return "deprecated"
	default:
		return fmt.Sprintf("AcceptabilityRating(%d)", int(r))
	}
}

// MarshalJSON serializes AcceptabilityRating as a JSON string (e.g. "admitted").
// This satisfies the bestiary.schema.json $defs/AcceptabilityRating enum contract,
// which declares the type as a string — not a number.
func (r AcceptabilityRating) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

// UnmarshalJSON deserializes a JSON string into an AcceptabilityRating.
// Accepted values: "admitted", "preferred", "deprecated" (case-insensitive).
// Returns an error if the string is not recognized.
func (r *AcceptabilityRating) UnmarshalJSON(b []byte) error {
	var raw string
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf(
			"bestiary: AcceptabilityRating.UnmarshalJSON: expected a JSON string, got %s;\n"+
				"  what went wrong: cannot unmarshal non-string JSON value into AcceptabilityRating\n"+
				"  why: AcceptabilityRating is serialized as a string enum (\"admitted\", \"preferred\", \"deprecated\")\n"+
				"  where: designation.go AcceptabilityRating.UnmarshalJSON\n"+
				"  how to fix: provide a JSON string value for AcceptabilityRating",
			b,
		)
	}
	parsed, err := parseRating(strings.ToLower(raw))
	if err != nil {
		return fmt.Errorf(
			"bestiary: AcceptabilityRating.UnmarshalJSON: unrecognized rating value %q;\n"+
				"  what went wrong: %w\n"+
				"  where: designation.go AcceptabilityRating.UnmarshalJSON\n"+
				"  how to fix: use one of \"admitted\", \"preferred\", \"deprecated\"",
			raw, err,
		)
	}
	*r = parsed
	return nil
}

// parseRating converts a lowercase string to an AcceptabilityRating.
// Returns an error for unrecognized values.
func parseRating(s string) (AcceptabilityRating, error) {
	switch s {
	case "admitted":
		return AcceptabilityAdmitted, nil
	case "preferred":
		return AcceptabilityPreferred, nil
	case "deprecated":
		return AcceptabilityDeprecated, nil
	default:
		return AcceptabilityAdmitted, fmt.Errorf(
			"bestiary: unrecognized acceptability rating %q\n"+
				"  what: %q is not a valid AcceptabilityRating\n"+
				"  why: only %q, %q, and %q are accepted\n"+
				"  where: parseRating\n"+
				"  how to fix: pass one of the accepted rating strings",
			s, s, "admitted", "preferred", "deprecated",
		)
	}
}

// Designation pairs a model identity string with its serialization scheme,
// hosting provider, and acceptability rating.
//
// A single ModelRef may have multiple Designations: a canonical form, a raw
// API ID form, and optionally provider-specific alias forms. Each carries an
// AcceptabilityRating. In this epoch all generated designations default to
// AcceptabilityAdmitted.
type Designation struct {
	// Value is the serialized model identifier under Scheme.
	Value string

	// Scheme is the serialization scheme used to produce Value.
	Scheme CanonicalScheme

	// Provider is the hosting provider associated with this designation.
	// For cross-scheme designations (e.g. SchemeRaw) the Provider is the
	// original model's hosting provider.
	Provider Provider

	// Rating classifies acceptability. All epoch-generated designations are
	// AcceptabilityAdmitted; promotion is deferred.
	Rating AcceptabilityRating
}
