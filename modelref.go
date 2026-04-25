package bestiary

import "fmt"

// ModelRef represents the canonical identity of a model.
//
// The 7-field tuple (ID, Provider, RawFamily, Family, Variant, Version, Date) is the
// stable anchor for cross-provider queries, canonical formatting, and the
// normalization pipeline. ID is the original API model identifier (e.g.
// "claude-opus-4-20250514"). Family, Variant, and Version are populated at
// codegen time by the normalization pipeline in cmd/bestiary-gen.
type ModelRef struct {
	ID        ModelID  // Original API model ID (e.g. "claude-opus-4-20250514")
	Provider  Provider // Hosting provider
	RawFamily Family   // API family field verbatim (e.g., "claude-opus")
	Family    Family   // Canonical family (e.g., "claude"); empty if not yet normalized
	Variant   string   // Canonical variant (e.g., "opus"); empty if no variant
	Version   string   // Model version extracted from family (e.g., "4.5", "2.5"); empty if none
	Date      string   // Release date in YYYY-MM-DD format; empty if none
}

// Ref returns a ModelRef for this ModelInfo.
// All seven fields are populated: ID from the API model ID, Family, Variant,
// and Version from the codegen-baked normalization, and Date from the
// codegen-extracted release date.
func (m ModelInfo) Ref() ModelRef {
	return ModelRef{
		ID:        m.ID,
		Provider:  m.Provider,
		RawFamily: m.Family,
		Family:    m.NormalizedFamily,
		Variant:   m.NormalizedVariant,
		Version:   m.NormalizedVersion,
		Date:      m.NormalizedDate,
	}
}

// Format serializes the ModelRef according to the given CanonicalScheme.
//
//   - SchemeCanonical: "<provider>/<family>/<variant>@<date>" — the variant
//     segment is included only when non-empty; the "@<date>" suffix is included
//     only when date is non-empty. Falls back to "<provider>/<raw-id>" when both
//     Family and Variant are empty (e.g., provider-specific representation).
//   - SchemeHuggingFace: "<provider>/<raw-id>" (HuggingFace Hub form).
//   - SchemePURL: "pkg:huggingface/<provider>/<raw-id>" (purl-spec + ECMA-427).
//   - SchemeRaw: string(r.ID) — the original API model identifier verbatim.
func (r ModelRef) Format(s CanonicalScheme) string {
	switch s {
	case SchemeCanonical:
		return r.formatCanonical()
	case SchemeHuggingFace:
		return fmt.Sprintf("%s/%s", r.Provider, r.ID)
	case SchemePURL:
		return fmt.Sprintf("pkg:huggingface/%s/%s", r.Provider, r.ID)
	case SchemeRaw:
		return string(r.ID)
	default:
		// Unrecognized scheme: fall back to raw ID for safety.
		return string(r.ID)
	}
}

// formatCanonical produces the SchemeCanonical string.
//
// When Family is populated the form is:
//
//	<provider>/<family>/<variant>@<date>   (variant or date omitted if empty)
//
// When Family is empty the provider-specific raw form is used:
//
//	<provider>/<raw-id>
func (r ModelRef) formatCanonical() string {
	if r.Family == "" {
		// Fall back to provider-specific representation.
		return fmt.Sprintf("%s/%s", r.Provider, r.ID)
	}
	if r.Variant == "" && r.Date == "" {
		return fmt.Sprintf("%s/%s", r.Provider, r.Family)
	}
	if r.Variant == "" {
		return fmt.Sprintf("%s/%s@%s", r.Provider, r.Family, r.Date)
	}
	if r.Date == "" {
		return fmt.Sprintf("%s/%s/%s", r.Provider, r.Family, r.Variant)
	}
	return fmt.Sprintf("%s/%s/%s@%s", r.Provider, r.Family, r.Variant, r.Date)
}

// String implements fmt.Stringer.
// It returns Format(SchemeCanonical), the canonical slash-separated form.
func (r ModelRef) String() string {
	return r.Format(SchemeCanonical)
}

// Designations returns all string designations for this ModelRef.
// Every designation carries AcceptabilityAdmitted in this epoch.
// Promotion to Preferred is deferred to a follow-up curation epoch.
//
// The returned slice contains:
//  1. A SchemeRaw designation using the original API model ID.
//  2. A SchemeCanonical designation (the canonical slash-separated form).
//  3. A SchemeHuggingFace designation.
//  4. A SchemePURL designation.
func (r ModelRef) Designations() []Designation {
	return []Designation{
		{
			Value:    r.Format(SchemeRaw),
			Scheme:   SchemeRaw,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
		{
			Value:    r.Format(SchemeCanonical),
			Scheme:   SchemeCanonical,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
		{
			Value:    r.Format(SchemeHuggingFace),
			Scheme:   SchemeHuggingFace,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
		{
			Value:    r.Format(SchemePURL),
			Scheme:   SchemePURL,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
	}
}

// ProvidersForFamily returns the set of providers that host models with
// the given family string. The family parameter matches the raw API family
// field (e.g., "claude-opus", "gemini-flash"). The returned slice contains
// no duplicates. If no models match, a nil slice is returned.
func ProvidersForFamily(family Family) []Provider {
	seen := make(map[Provider]struct{})
	var out []Provider
	for _, m := range staticModels {
		if m.Family == family {
			if _, ok := seen[m.Provider]; !ok {
				seen[m.Provider] = struct{}{}
				out = append(out, m.Provider)
			}
		}
	}
	return out
}
