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

	// providerHint is the provider extracted from the input string during scheme
	// detection (e.g., the "anthropic" segment in "pkg:huggingface/anthropic/...").
	// When non-empty, Resolve filters match results to this provider only.
	// This field is set internally by detectScheme; callers cannot set it directly.
	providerHint Provider
}

// WithScheme pins the CanonicalScheme for a Resolve call.
// When not specified, Resolve auto-detects the scheme from the input prefix:
//   - "pkg:huggingface/<provider>/<id>" → SchemePURL: strip prefix, retain provider hint
//   - "<word>/<word>" two-segment form (no "pkg:" prefix, no "@" or versioned token) → SchemeHuggingFace:
//     strip provider prefix, match raw ID
//   - "<family>/<variant>[@date]" or "<provider>/<family>/<variant>[@date]" form → SchemeCanonical
//   - Otherwise → SchemeRaw (exact model ID lookup)
//
// SchemeCanonical is auto-detected when the input contains 1–3 "/" separators AND
// at least one of: an "@" date suffix, or a versioned token (e.g. "4.5", "2.5").
// Use WithScheme to override auto-detection.
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
//   - "pkg:huggingface/<provider>/<id>" → SchemePURL: strip prefix, apply provider filter
//   - "<family>/<variant>[@date]" or multi-segment form with "@" or versioned token → SchemeCanonical
//   - "<provider>/<id>" (two slash segments, no "pkg:" prefix, no canonical signals) → SchemeHuggingFace:
//     strip provider prefix, match raw ID
//   - Otherwise → SchemeRaw: exact model ID match
//
// Bare-family fallback: when SchemeRaw produces zero matches and the input
// contains no slashes or special characters, Resolve retries with SchemeCanonical
// family-only matching. If multiple distinct canonical triples match, *ErrAmbiguous
// is returned. If a single group matches, refs are returned. This surfaces
// *ErrAmbiguous for inputs like "claude" instead of ErrNotFound.
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
		scheme, matchInput, cfg.providerHint = detectSchemeWithHint(input)
	}

	matches := matchModels(matchInput, scheme)

	// Apply provider hint filter (set by PURL and SchemeCanonical with leading provider segment).
	if cfg.providerHint != "" {
		matches = filterByProvider(matches, cfg.providerHint)
	}

	// Bare-family fallback: when SchemeRaw produces zero matches and the input
	// looks like a bare family name (no slashes, no "@", no special characters),
	// retry with SchemeCanonical to surface ErrAmbiguous instead of ErrNotFound.
	if len(matches) == 0 && scheme == SchemeRaw && isBareIdentifier(input) {
		canonicalMatches := matchModels(input, SchemeCanonical)
		if len(canonicalMatches) > 0 {
			matches = canonicalMatches
			scheme = SchemeCanonical
		}
	}

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
	// For SchemeCanonical with an exact-ID input: use the same ID-based grouping
	// as SchemeRaw. When the caller supplies an exact model ID like
	// "claude-opus-4-20250514" with WithScheme(SchemeCanonical), normalizing the
	// NormalizedVariant across providers can produce divergent tuples for what is
	// semantically one model (e.g., Family="claude"/Variant="opus" from providers
	// with a family field vs. Family="claude"/Variant="" from providers without
	// one). Grouping by model ID instead collapses these spurious differences.
	//
	// For SchemeCanonical with a non-exact-ID input (e.g., "claude" to match
	// multiple family members), group by Canonical triple so that genuinely
	// distinct models (claude/opus, claude/sonnet) remain distinct and trigger
	// ErrAmbiguous as intended.
	//
	// exactIDInput is true when every match returned by matchModels has the same
	// model ID as the matchInput — which is the case for an exact static ID lookup.
	exactIDInput := scheme == SchemeCanonical && isExactIDInput(matchInput, matches)

	type groupKey struct {
		id      ModelID // non-empty for ID-based grouping
		family  Family
		variant string
		date    string
	}
	byGroup := make(map[groupKey][]ModelRef)
	var order []groupKey // preserve insertion order for deterministic output

	for _, ref := range matches {
		var key groupKey
		if scheme == SchemeRaw || scheme == SchemeHuggingFace || scheme == SchemePURL || exactIDInput {
			// Group by model ID: cross-provider hosting of the same raw ID.
			key = groupKey{id: ref.ID}
		} else {
			// Group by Canonical triple for SchemeCanonical non-exact inputs.
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

// detectSchemeWithHint infers the CanonicalScheme from the input string and
// returns the effective match string, scheme, and any provider hint extracted
// from the input.
//
// Detection order:
//  1. SchemePURL: starts with "pkg:huggingface/" — strips the PURL prefix;
//     retains the provider segment as a hint for filtering.
//  2. SchemeCanonical: contains 2–4 slash-separated segments AND either an "@"
//     date suffix OR a versioned token (e.g. "2.5", "4.5"). Also detects
//     1-segment inputs ending with "@date". Provider hint extracted when the
//     leading segment matches a known provider pattern (contains no digit tokens
//     that would indicate a model family). Returns the canonicalized match input.
//  3. SchemeHuggingFace: exactly one "/" with no "pkg:" prefix and no canonical signals.
//  4. SchemeRaw: default for plain model IDs with no slashes.
func detectSchemeWithHint(input string) (CanonicalScheme, string, Provider) {
	// SchemePURL: starts with "pkg:huggingface/"
	if strings.HasPrefix(input, "pkg:huggingface/") {
		stripped := strings.TrimPrefix(input, "pkg:huggingface/")
		var providerHint Provider
		// Retain "<provider>/" as a filter hint before stripping it.
		if idx := strings.Index(stripped, "/"); idx >= 0 {
			providerHint = Provider(stripped[:idx])
			stripped = stripped[idx+1:]
		}
		return SchemePURL, stripped, providerHint
	}

	slashCount := strings.Count(input, "/")

	// SchemeCanonical detection: input with slashes that carries an "@" date
	// or a versioned token (N.M, N-M digit sequences, or lone digit-alphanumeric).
	// Valid canonical forms include:
	//   "family/variant@date"                 (1 slash)
	//   "provider/family/variant@date"        (2 slashes)
	//   "provider/family/variant/version@date" (3 slashes)
	//   "family@date"                         (0 slashes, "@")
	if slashCount >= 1 && slashCount <= 3 && isCanonicalForm(input) {
		// Extract provider hint from leading segment when present.
		// A leading segment is a provider hint when all remaining segments after
		// it contain the expected canonical family/variant/version components.
		providerHint, matchInput := extractCanonicalProviderHint(input)
		return SchemeCanonical, matchInput, providerHint
	}

	// "@date" with no slashes: also canonical form.
	if slashCount == 0 && strings.Contains(input, "@") {
		return SchemeCanonical, input, ""
	}

	// SchemeHuggingFace: two slash-separated segments with no "pkg:" prefix and no canonical signals.
	// e.g. "anthropic/claude-opus-4-20250514"
	if slashCount == 1 && !strings.HasPrefix(input, "pkg:") {
		// Strip "<provider>/" prefix to get the raw model ID.
		idx := strings.Index(input, "/")
		return SchemeHuggingFace, input[idx+1:], ""
	}

	// Default: SchemeRaw — treat input as a raw model ID.
	return SchemeRaw, input, ""
}

// isCanonicalForm reports whether input looks like a canonical model reference.
// A canonical form is identified by at least one of:
//   - Contains "@" (date suffix)
//   - Contains a versioned segment: "N.M" dot-notation or "N-M" hyphen-digit pair
//
// This distinguishes "claude/opus@2025-11-01" (canonical) from
// "anthropic/claude-opus-4-20250514" (HuggingFace form).
func isCanonicalForm(input string) bool {
	if strings.Contains(input, "@") {
		return true
	}
	// Check for versioned token in any segment: N.M or N-M
	segments := strings.Split(input, "/")
	for _, seg := range segments {
		if looksLikeVersionedSegment(seg) {
			return true
		}
	}
	return false
}

// looksLikeVersionedSegment returns true when seg contains a dot-version
// pattern ("N.M") that is characteristic of canonical version tokens.
// This avoids false positives on date-like patterns such as "2025-11-01".
func looksLikeVersionedSegment(seg string) bool {
	// Dot-version: "4.5", "2.5", "3.1" — a digit, dot, digit pattern.
	// We require the segment to be ONLY the version token (no surrounding text)
	// to avoid matching partial strings inside model IDs.
	if reBareVersion.MatchString(seg) {
		return true
	}
	return false
}

// extractCanonicalProviderHint inspects a canonical-form input with slashes and
// attempts to determine whether the first segment is a provider name (vs. a family name).
//
// Heuristic: a segment is treated as a provider hint when it contains only
// lowercase alpha characters with optional hyphens and does not look like a
// model family name (i.e., it does not begin with a known family prefix that
// would be followed by a variant segment).
//
// When a provider hint is found, matchInput is the remaining path after stripping it.
// When no provider hint is found, matchInput equals the full input.
//
// This is a best-effort heuristic. For unambiguous provider-filtered resolution,
// callers should use the PURL form or WithScheme(SchemePURL).
func extractCanonicalProviderHint(input string) (Provider, string) {
	// For canonical forms, we return the input as-is for matching (SchemeCanonical
	// matching in modelMatches handles family/variant decomposition).
	// Provider hint extraction from canonical form is not applied for now —
	// the canonical matching already constrains results sufficiently.
	// Full provider-hint extraction from canonical form is a future enhancement.
	return "", input
}

// filterByProvider returns only those refs whose Provider matches hint.
func filterByProvider(refs []ModelRef, hint Provider) []ModelRef {
	var out []ModelRef
	for _, r := range refs {
		if r.Provider == hint {
			out = append(out, r)
		}
	}
	return out
}

// isBareIdentifier reports whether s is a simple bare identifier with no slashes,
// "@" characters, "pkg:" prefix, or other special characters. Used to determine
// whether the bare-family fallback should be attempted.
func isBareIdentifier(s string) bool {
	if strings.Contains(s, "/") || strings.Contains(s, "@") || strings.Contains(s, ":") {
		return false
	}
	return true
}

// detectScheme infers the CanonicalScheme from the input string and returns
// the effective match string (with any scheme-specific prefixes stripped).
// This is the legacy form; new code uses detectSchemeWithHint.
func detectScheme(input string) (CanonicalScheme, string) {
	scheme, matchInput, _ := detectSchemeWithHint(input)
	return scheme, matchInput
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

// isExactIDInput reports whether every ref in matches has a model ID equal to
// matchInput. This is used to detect an exact-ID lookup in SchemeCanonical mode
// so that cross-provider normalization divergence does not produce a false
// ErrAmbiguous. See the grouping comment in Resolve for the full rationale.
func isExactIDInput(matchInput string, matches []ModelRef) bool {
	if len(matches) == 0 {
		return false
	}
	for _, ref := range matches {
		if string(ref.ID) != matchInput {
			return false
		}
	}
	return true
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
		// Try matching on NormalizedFamily (exact family name match).
		// This allows inputs like "claude" to match all claude-family models.
		if string(m.NormalizedFamily) == matchInput {
			return true
		}
		// Try canonical segment matching: "family/variant@date" form.
		// Parse the matchInput into (family, variant, date) segments.
		if matchCanonicalSegments(m, matchInput) {
			return true
		}
		return false
	default:
		return string(m.ID) == matchInput
	}
}

// matchCanonicalSegments parses a canonical-form matchInput (e.g.
// "claude/opus@2025-11-01" or "claude/opus/4.5@2025-11-01") and checks whether
// the model m matches the parsed (family, variant, version, date) tuple.
//
// Parsing rules:
//  1. Strip "@date" suffix if present.
//  2. Split remaining segments on "/".
//  3. Segment[0] = family; segment[1] = variant (if present); segment[2] = version (if present).
//
// Matching rules:
//   - family must match NormalizedFamily (required).
//   - variant must match NormalizedVariant when specified.
//   - version must match NormalizedVersion when specified.
//   - date must match NormalizedDate when specified.
func matchCanonicalSegments(m ModelInfo, matchInput string) bool {
	// Extract "@date" suffix.
	var dateFilter string
	if at := strings.LastIndex(matchInput, "@"); at >= 0 {
		dateFilter = matchInput[at+1:]
		matchInput = matchInput[:at]
	}

	segments := strings.Split(matchInput, "/")
	if len(segments) == 0 || segments[0] == "" {
		return false
	}

	familyFilter := segments[0]
	var variantFilter string
	var versionFilter string

	if len(segments) >= 2 {
		variantFilter = segments[1]
	}
	if len(segments) >= 3 {
		versionFilter = segments[2]
	}

	// Family must match.
	if string(m.NormalizedFamily) != familyFilter {
		return false
	}
	// Variant filter: when specified, must match.
	if variantFilter != "" && m.NormalizedVariant != variantFilter {
		return false
	}
	// Version filter: when specified, must match.
	if versionFilter != "" && m.NormalizedVersion != versionFilter {
		return false
	}
	// Date filter: when specified, must match.
	if dateFilter != "" && m.NormalizedDate != dateFilter {
		return false
	}
	return true
}
