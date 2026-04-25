package bestiary

import "fmt"

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
