package bestiary

import "strings"

// ResolveOption configures a Resolve call.
// Options are applied in order; later options override earlier ones.
type ResolveOption func(*resolveConfig)

// resolveConfig holds the resolved configuration for a Resolve call.
type resolveConfig struct {
	// scheme is the explicit CanonicalScheme to use for matching.
	// When nil, Resolve auto-detects the scheme from the input string.
	scheme *CanonicalScheme
}

// WithScheme pins the CanonicalScheme for a Resolve call.
// When not specified, Resolve auto-detects the scheme from the input prefix:
//   - "pkg:huggingface/" prefix → SchemePURL
//   - "<word>/<word>" two-segment form (no "pkg:" prefix) → SchemeHuggingFace
//   - Otherwise → SchemeRaw (exact model ID lookup)
func WithScheme(s CanonicalScheme) ResolveOption {
	return func(c *resolveConfig) {
		c.scheme = &s
	}
}

// Resolve returns the set of ModelRefs that match the given input string.
//
// # Disambiguation rule (Reviewer C-N1)
//
// Cross-provider hosting: if all matches share the same Canonical triple
// (NormalizedFamily, NormalizedVariant, NormalizedDate) — meaning the same
// conceptual model is hosted by multiple providers — Resolve returns a non-nil
// []ModelRef with err == nil. The caller can iterate by Provider.
//
// Multiple distinct canonicals: if the input matches models that resolve to
// two or more distinct Canonical triples (e.g., "claude" matches claude/opus,
// claude/sonnet, and claude/haiku), Resolve returns nil, *ErrAmbiguous with
// the candidate list. The caller should refine the input or use
// WithScheme(SchemeRaw) with an exact API model ID.
//
// Zero matches: returns nil, *ErrNotFound.
//
// # Scheme auto-detection
//
//   - "pkg:huggingface/<provider>/<id>" → SchemePURL: strip prefix, match raw ID
//   - "<provider>/<id>" (two slash segments, no "pkg:" prefix) → SchemeHuggingFace:
//     strip provider prefix, match raw ID
//   - Otherwise → SchemeRaw: exact model ID match
//
// Use WithScheme to override auto-detection.
func Resolve(input string, opts ...ResolveOption) ([]ModelRef, error) {
	cfg := &resolveConfig{}
	for _, o := range opts {
		o(cfg)
	}

	var scheme CanonicalScheme
	var matchInput string

	if cfg.scheme != nil {
		scheme = *cfg.scheme
		matchInput = normalizeInput(input, scheme)
	} else {
		scheme, matchInput = detectScheme(input)
	}

	matches := matchModels(matchInput, scheme)
	if len(matches) == 0 {
		return nil, &ErrNotFound{What: "model", Key: input}
	}

	// Group matches by their canonical key.
	//
	// For SchemeRaw (exact ID match), group by model ID: all providers hosting
	// the same raw API ID are considered cross-provider hosting of a single model.
	// This avoids false ErrAmbiguous when normalization differences across
	// providers produce slightly different (Family, Variant) tuples for the same ID.
	//
	// For SchemeCanonical and others, group by Canonical triple
	// (NormalizedFamily, NormalizedVariant, NormalizedDate).
	type groupKey struct {
		id      ModelID // non-empty for SchemeRaw grouping
		family  Family
		variant string
		date    string
	}
	byGroup := make(map[groupKey][]ModelRef)
	var order []groupKey // preserve insertion order for deterministic output

	for _, ref := range matches {
		var key groupKey
		if scheme == SchemeRaw || scheme == SchemeHuggingFace || scheme == SchemePURL {
			// Group by model ID: cross-provider hosting of the same raw ID.
			key = groupKey{id: ref.ID}
		} else {
			// Group by Canonical triple for SchemeCanonical.
			key = groupKey{family: ref.Family, variant: ref.Variant, date: ref.Date}
		}
		if _, exists := byGroup[key]; !exists {
			order = append(order, key)
		}
		byGroup[key] = append(byGroup[key], ref)
	}

	if len(byGroup) == 1 {
		// All matches share the same group: cross-provider hosting.
		// Return the flat slice; caller can filter by Provider.
		return matches, nil
	}

	// Multiple distinct groups: ambiguous input.
	// Build a representative candidate per distinct group.
	candidates := make([]ModelRef, 0, len(order))
	for _, key := range order {
		// Use the first match for each group as the representative.
		candidates = append(candidates, byGroup[key][0])
	}
	return nil, &ErrAmbiguous{
		Input:      input,
		Scheme:     scheme,
		Candidates: candidates,
	}
}

// detectScheme infers the CanonicalScheme from the input string and returns
// the effective match string (with any scheme-specific prefixes stripped).
func detectScheme(input string) (CanonicalScheme, string) {
	// SchemePURL: starts with "pkg:huggingface/"
	if strings.HasPrefix(input, "pkg:huggingface/") {
		stripped := strings.TrimPrefix(input, "pkg:huggingface/")
		// Strip "<provider>/" prefix if present.
		if idx := strings.Index(stripped, "/"); idx >= 0 {
			stripped = stripped[idx+1:]
		}
		return SchemePURL, stripped
	}

	// SchemeHuggingFace: two slash-separated segments with no "pkg:" prefix.
	// e.g. "anthropic/claude-opus-4-20250514"
	slashCount := strings.Count(input, "/")
	if slashCount == 1 && !strings.HasPrefix(input, "pkg:") {
		// Strip "<provider>/" prefix to get the raw model ID.
		idx := strings.Index(input, "/")
		return SchemeHuggingFace, input[idx+1:]
	}

	// Default: SchemeRaw — treat input as a raw model ID or substring.
	return SchemeRaw, input
}

// normalizeInput strips scheme-specific prefixes so the result is the raw
// model identifier for matching purposes.
func normalizeInput(input string, scheme CanonicalScheme) string {
	switch scheme {
	case SchemePURL:
		stripped := strings.TrimPrefix(input, "pkg:huggingface/")
		if idx := strings.Index(stripped, "/"); idx >= 0 {
			stripped = stripped[idx+1:]
		}
		return stripped
	case SchemeHuggingFace:
		if idx := strings.Index(input, "/"); idx >= 0 {
			return input[idx+1:]
		}
		return input
	default:
		return input
	}
}

// matchModels returns the ModelRefs from the static registry that match the
// given matchInput under the active scheme. For SchemeRaw the match is an
// exact model ID comparison. For all other schemes that have already been
// normalized to a raw ID (by detectScheme / normalizeInput), the match is
// also an exact model ID comparison.
//
// SchemeCanonical falls back to substring matching on Family when the input
// is not an exact ID, enabling lookups like "claude" to return multiple
// candidates (triggering ErrAmbiguous).
func matchModels(matchInput string, scheme CanonicalScheme) []ModelRef {
	var out []ModelRef
	for _, m := range staticModels {
		if modelMatches(m, matchInput, scheme) {
			out = append(out, m.Ref())
		}
	}
	return out
}

// modelMatches reports whether model m matches matchInput under scheme.
func modelMatches(m ModelInfo, matchInput string, scheme CanonicalScheme) bool {
	switch scheme {
	case SchemeRaw:
		// Exact model ID match.
		return string(m.ID) == matchInput
	case SchemeHuggingFace, SchemePURL:
		// Input was normalized to raw ID; exact match.
		return string(m.ID) == matchInput
	case SchemeCanonical:
		// Try exact ID first for full IDs like "claude-opus-4-20250514".
		if string(m.ID) == matchInput {
			return true
		}
		// Then try matching on NormalizedFamily (substring of the family).
		// This allows inputs like "claude" to match all claude-family models.
		if string(m.NormalizedFamily) == matchInput {
			return true
		}
		return false
	default:
		return string(m.ID) == matchInput
	}
}
