package bestiary

// ModelRef represents the canonical identity of a model.
// Family and Variant are empty strings until the normalization epoch,
// when a decomposition pipeline will split RawFamily into structured fields.
//
// The 5-field tuple (Provider, RawFamily, Family, Variant, Date) provides a
// stable anchor for cross-provider queries and future normalization work.
type ModelRef struct {
	Provider  Provider // Hosting provider
	RawFamily string   // API family field verbatim (e.g., "claude-opus")
	Family    string   // Empty until normalization epoch
	Variant   string   // Empty until normalization epoch
	Date      string   // Release date from ModelInfo.ReleaseDate (e.g., "2025-05-14")
}

// Ref returns a ModelRef for this ModelInfo.
// RawFamily is set from the API family field. Family and Variant are always
// empty strings — decomposition is deferred to the normalization epoch.
func (m ModelInfo) Ref() ModelRef {
	return ModelRef{
		Provider:  m.Provider,
		RawFamily: m.Family,
		Family:    "",
		Variant:   "",
		Date:      m.ReleaseDate,
	}
}

// ProvidersForFamily returns the set of providers that host models with
// the given family string. The family parameter matches the raw API family
// field (e.g., "claude-opus", "gemini-flash"). The returned slice contains
// no duplicates. If no models match, a nil slice is returned.
func ProvidersForFamily(family string) []Provider {
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
