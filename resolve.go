package bestiary

import "strings"

// ResolveOption configures a Resolve call.
// Options are applied in order; later options override earlier ones.
type ResolveOption func(*resolveConfig)

// resolveConfig holds the resolved configuration for a Resolve call.
type resolveConfig struct {
	// scheme is the explicit CanonicalScheme to use for matching.
	// When nil, Resolve auto-detects the scheme from the input string
	// (unless inputFormat is set, which takes precedence over auto-detect).
	scheme *CanonicalScheme

	// inputFormat is the explicit InputFormat to use for matching.
	// When non-nil, Resolve dispatches directly to the matching scheme
	// without auto-detect. inputFormat takes precedence over scheme when both are set.
	inputFormat *InputFormat

	// providerHint is the provider extracted from the input string during scheme
	// detection (e.g., the "anthropic" segment in "pkg:huggingface/anthropic/...").
	// When non-empty, Resolve filters match results to this provider only.
	// This field is set internally by detectScheme; callers cannot set it directly.
	providerHint Provider
}

// WithInputFormat pins the InputFormat for a Resolve call.
//
// When InputFormatPeasant is specified (the default from the CLI), Resolve
// dispatches as SchemeCanonical and does NOT auto-detect from the input prefix.
// A PURL or HuggingFace input passed with InputFormatPeasant will fail to match
// (ErrNotFound) — this is intentional. Pass the matching --format flag explicitly.
//
// For huggingface/hf, purl, and raw, dispatches directly to the corresponding
// scheme without auto-detect.
func WithInputFormat(f InputFormat) ResolveOption {
	return func(c *resolveConfig) {
		c.inputFormat = &f
	}
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
// # Disambiguation rule
//
// Cross-provider hosting: if all matches share the same Canonical triple
// (Family, Variant, Date) — meaning the same conceptual model is hosted by
// multiple providers — Resolve returns a non-nil []ModelRef with err == nil.
// The caller can iterate by Provider.
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
// Variant-aware bare-family fallback: when the Family-exact
// retry also yields zero matches and the bare input is a hyphenated
// "<family>-<variant>" shorthand whose leading token is a registered Family and
// trailing token names a Variant within it (e.g. "claude-opus"), Resolve matches
// that variant group and returns *ErrAmbiguous with the variant's candidates. See
// matchBareFamilyVariant for the conservative matching rule.
//
// Use WithScheme to override auto-detection.
func Resolve(input string, opts ...ResolveOption) ([]ModelRef, error) {
	cfg := &resolveConfig{}
	for _, o := range opts {
		o(cfg)
	}

	var scheme CanonicalScheme
	var matchInput string

	if cfg.inputFormat != nil {
		// Explicit --format flag: dispatch directly without auto-detect.
		scheme = inputFormatToScheme(*cfg.inputFormat)
		matchInput = normalizeInput(input, scheme)
		// For PURL, also extract the provider hint.
		if scheme == SchemePURL && strings.HasPrefix(input, "pkg:huggingface/") {
			stripped := strings.TrimPrefix(input, "pkg:huggingface/")
			if idx := strings.Index(stripped, "/"); idx >= 0 {
				cfg.providerHint = Provider(stripped[:idx])
			}
		}
	} else if cfg.scheme != nil {
		scheme = *cfg.scheme
		matchInput = normalizeInput(input, scheme)
	} else {
		scheme, matchInput, cfg.providerHint = detectSchemeWithHint(input)
	}

	matches := matchModels(matchInput, scheme)

	// Apply provider hint filter (set by PURL and SchemeCanonical with leading provider segment).
	// Fix #1 (PURL loose-match fallback): when providerHint filter yields zero results,
	// fall back to the full match set (all-provider loose match) and return ErrAmbiguous
	// with a diagnostic message naming the missed namespace.
	var purlLooseFallback bool
	var purlMissedNamespace Provider
	if cfg.providerHint != "" {
		filtered := filterByProvider(matches, cfg.providerHint)
		if len(filtered) == 0 && len(matches) > 0 && scheme == SchemePURL {
			// Fix #1: namespace (provider hint) had zero matches but model found in other providers.
			// Record the miss and fall back to all matches as loose candidates.
			purlLooseFallback = true
			purlMissedNamespace = cfg.providerHint
			// Do not set matches = filtered; keep the full match set for loose fallback.
		} else {
			// Normal path: apply the filter.
			matches = filtered
		}
	}

	// Bare-family fallback: when SchemeRaw produces zero matches and the input
	// looks like a bare family name (no slashes, no "@", no special characters),
	// retry with SchemeCanonical to surface ErrAmbiguous instead of ErrNotFound.
	if len(matches) == 0 && scheme == SchemeRaw && isBareIdentifier(input) {
		canonicalMatches := matchModels(input, SchemeCanonical)
		if len(canonicalMatches) > 0 {
			matches = canonicalMatches
			scheme = SchemeCanonical
		} else if variantMatches := matchBareFamilyVariant(input); len(variantMatches) > 0 {
			// Variant-aware bare-family fallback: a bare
			// hyphenated "<family>-<variant>" shorthand (e.g. "claude-opus") has no
			// exact Family match, but the leading token is a registered
			// Family and the trailing token names a Variant within it. Surface the
			// matching variant group as ErrAmbiguous (via SchemeCanonical grouping)
			// rather than ErrNotFound.
			matches = variantMatches
			scheme = SchemeCanonical
		}
	}

	if len(matches) == 0 {
		return nil, &ErrNotFound{What: "model", Key: input}
	}

	// Fix #1 (PURL loose fallback): when the PURL namespace yielded zero matches
	// but other providers host the model, emit ErrAmbiguous with all candidates
	// and a diagnostic message that names the missed namespace.
	if purlLooseFallback {
		// Build a deduplicated candidate list from all matches (group by ID).
		// Fix: prefer the canonical provider as the
		// per-ID representative. When iterating matches, if the current match is the
		// canonical provider for its family, upgrade the stored representative (even
		// if we've already seen this ID). This mirrors the multi-group logic at
		// resolve.go:267-276 so FormatAmbiguous Section 1 is never empty for a model
		// that IS hosted by its canonical provider.
		candidateMap := make(map[ModelID]ModelRef)
		var candidateOrder []ModelID
		for _, m := range matches {
			existing, seen := candidateMap[m.ID]
			if !seen {
				candidateOrder = append(candidateOrder, m.ID)
				candidateMap[m.ID] = m
			} else {
				// Upgrade to the canonical provider when the stored rep is not canonical
				// and the current match is the canonical provider for this family.
				canonProv := m.Family.CanonicalProvider()
				if canonProv != "" && m.Provider == canonProv && existing.Provider != canonProv {
					candidateMap[m.ID] = m
				}
			}
		}
		candidates := make([]ModelRef, 0, len(candidateOrder))
		for _, id := range candidateOrder {
			candidates = append(candidates, candidateMap[id])
		}
		return nil, &ErrAmbiguous{
			Input:               input,
			Scheme:              scheme,
			Candidates:          candidates,
			PURLMissedNamespace: string(purlMissedNamespace),
			RehostProviders:     collectRehostProviders(matches),
		}
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
	// "claude-opus-4-20250514" with WithScheme(SchemeCanonical), the Variant field
	// across providers can produce divergent tuples for what is semantically one
	// model (e.g., Family="claude"/Variant="opus" from providers with a family
	// field vs. Family="claude"/Variant="" from providers without one). Grouping
	// by model ID instead collapses these spurious differences.
	//
	// For SchemeCanonical with a non-exact-ID input (e.g., "claude" to match
	// multiple family members), group by Canonical triple so that genuinely
	// distinct models (claude/opus, claude/sonnet) remain distinct and trigger
	// ErrAmbiguous as intended.
	//
	// exactIDInput is true when every match returned by matchModels has the same
	// model ID as the matchInput — which is the case for an exact static ID lookup.
	exactIDInput := scheme == SchemeCanonical && isExactIDInput(matchInput, matches)

	// groupKey is the canonical-identity key used to bucket matches into groups.
	// Cross-provider hosting of the same conceptual model collapses into one group;
	// genuinely distinct models (different variants, versions, context windows, etc.)
	// land in separate groups.
	//
	// FIX-B: The key now carries Version, Modifier, and a locally-parsed
	// ":N" context-window discriminator (parseContextN). This prevents context-window
	// variants (e.g. claude-3-7-sonnet-thinking:1024 vs :128000) from being silently
	// collapsed into a single representative — they share identical canonical fields
	// but differ only in the raw ":N" suffix.
	type groupKey struct {
		id       ModelID // non-empty for ID-based grouping
		family   Family
		variant  string
		version  string
		modifier string
		date     string
		contextN string // parsed ":N" from ID (e.g., "1024", "128000", "")
	}
	byGroup := make(map[groupKey][]ModelRef)
	var order []groupKey // preserve insertion order for deterministic output

	for _, ref := range matches {
		var key groupKey
		if scheme == SchemeRaw || scheme == SchemeHuggingFace || scheme == SchemePURL || exactIDInput {
			// Group by model ID: cross-provider hosting of the same raw ID.
			key = groupKey{id: ref.ID}
		} else {
			// Group by extended canonical identity for SchemeCanonical non-exact inputs.
			// Version, Modifier, and contextN distinguish sub-variants that share the
			// same (Family, Variant, Date) triple.
			key = groupKey{
				family:  ref.Family,
				variant: ref.Variant,
				version: ref.Version,
				// the Modifier component is the ORDER-INDEPENDENT canonical
				// key (modifierKey), so [thinking,turbo] and [turbo,thinking] never
				// split a group; the ":N" context-window still discriminates per FIX-B.
				modifier: modifierKey(ref.Modifier),
				date:     ref.Date,
				contextN: parseContextN(ref.ID),
			}
		}
		if _, exists := byGroup[key]; !exists {
			order = append(order, key)
		}
		byGroup[key] = append(byGroup[key], ref)
	}

	if len(byGroup) == 1 {
		// All matches share the same group: cross-provider hosting.
		// Fix #4 (canonical-provider preference): when resolving in canonical form
		// (peasant/SchemeCanonical) and not an exact-ID lookup, prefer the canonical
		// originating provider over rehosts.
		//
		// Applies when:
		//   1. Scheme is SchemeCanonical (canonical-form input, not raw/HF/PURL)
		//   2. Not an exact-ID lookup (exactIDInput = false), since those have
		//      deterministic cross-provider identity
		//   3. CanonicalProvider() returns a non-empty Provider
		//   4. That Provider is present in the match set
		if scheme == SchemeCanonical && !exactIDInput && len(matches) > 0 {
			canonicalProv := matches[0].Family.CanonicalProvider()
			if canonicalProv != "" {
				filtered := filterByProvider(matches, canonicalProv)
				if len(filtered) > 0 {
					return filtered, nil
				}
				// Canonical provider not in match set — fall through to return all matches.
			}
		}
		// Return the flat slice; caller can filter by Provider.
		return matches, nil
	}

	// Multiple distinct groups: ambiguous input.
	// Build a representative candidate per distinct group using selectRepresentative.
	// Fix (FIX-B): selectRepresentative prefers the
	// canonical provider row and falls back to lexicographic Provider order —
	// ensuring "anthropic" appears as the representative for claude groups rather
	// than an arbitrary rehost, and guaranteeing determinism independent of
	// static registry ordering.
	candidates := make([]ModelRef, 0, len(order))
	for _, key := range order {
		candidates = append(candidates, selectRepresentative(byGroup[key]))
	}
	return nil, &ErrAmbiguous{
		Input:           input,
		Scheme:          scheme,
		Candidates:      candidates,
		RehostProviders: collectRehostProviders(matches),
	}
}

// collectRehostProviders returns the distinct providers from refs that are NOT
// the canonical/originating provider for their family. Providers are deduplicated
// in stable first-seen order. This is used to populate ErrAmbiguous.RehostProviders.
func collectRehostProviders(refs []ModelRef) []Provider {
	seen := make(map[Provider]struct{})
	var out []Provider
	for _, m := range refs {
		if m.Provider == m.Family.CanonicalProvider() {
			continue
		}
		if _, dup := seen[m.Provider]; dup {
			continue
		}
		seen[m.Provider] = struct{}{}
		out = append(out, m.Provider)
	}
	return out
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
//     1-segment inputs ending with "@date". Returns the input unchanged with
//     no provider hint — provider preference for canonical-form input matching
//     multiple providers is applied later via Family.CanonicalProvider() at the
//     grouping step (see resolve.go:228-252).
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
		// Canonical form matching uses the full input string; family/variant/version/date
		// decomposition is handled in matchCanonicalSegments (no provider prefix stripping).
		return SchemeCanonical, input, ""
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

// matchBareFamilyVariant implements the variant-aware bare-family fallback for a
// bare hyphenated "<family>-<variant>" shorthand such as "claude-opus". It is
// invoked only when an exact-ID and exact-Family lookup have both already failed.
//
// The match is deliberately CLOSED/conservative to avoid false positives on
// arbitrary hyphenated junk: it splits input at each hyphen, and accepts a split
// only when BOTH (1) the leading token is a registered Family (IsKnownFamily) and
// (2) the trailing token equals the Variant of one or more models in that Family.
// The first hyphen position that satisfies both yields the candidate set; if no
// split qualifies, it returns nil (caller then returns ErrNotFound). Because the
// trailing token must match a real model Variant, a genuinely-unknown input like
// "claude-banana" or "foo-bar" matches nothing.
//
// The returned refs are grouped by the caller under SchemeCanonical, so a variant
// spanning multiple distinct canonicals (e.g. opus 4, opus 4.1, opus 3) surfaces
// as ErrAmbiguous with the full candidate list.
func matchBareFamilyVariant(input string) []ModelRef {
	for i := 0; i < len(input); i++ {
		if input[i] != '-' {
			continue
		}
		famTok, varTok := input[:i], input[i+1:]
		if famTok == "" || varTok == "" || !IsKnownFamily(Family(famTok)) {
			continue
		}
		var out []ModelRef
		for _, m := range staticModels {
			if string(m.Family) == famTok && m.Variant == varTok {
				out = append(out, m.Ref())
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

// parseContextN extracts the context-window ":N" token from a raw model ID.
// The ":N" suffix (e.g. ":1024", ":128000") is used by some providers (notably
// NanoGPT) to expose distinct context-window variants of the same model under
// separate IDs. These variants share identical canonical fields (Family, Variant,
// Version, Modifier, Date) and can only be distinguished by the raw ":N" suffix.
//
// Returns the digit-only suffix after the last colon, or "" when absent or when
// the suffix contains non-digit characters (e.g. ":thinking:low" → "").
func parseContextN(id ModelID) string {
	s := string(id)
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return ""
	}
	suffix := s[i+1:]
	if len(suffix) == 0 {
		return ""
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return ""
		}
	}
	return suffix
}

// selectRepresentative picks the most representative ModelRef from a group of
// cross-provider-hosted models (all sharing the same canonical identity).
//
// Tiebreak order:
//  1. Prefer the row whose Provider equals Family.CanonicalProvider() for the
//     family (as defined in family.go). This ensures the originating publisher
//     appears as the representative rather than an arbitrary rehost.
//  2. When no canonical provider is present in the group (or CanonicalProvider
//     returns ""), fall back to the lexicographically-smallest Provider string.
//     This guarantees a deterministic result regardless of slice order or map
//     iteration order.
//
// Panics on an empty group (caller invariant: groups always contain ≥1 ref).
func selectRepresentative(group []ModelRef) ModelRef {
	if len(group) == 0 {
		panic("bestiary: selectRepresentative called with empty group")
	}
	if len(group) == 1 {
		return group[0]
	}
	// Prefer canonical provider row.
	canonProv := group[0].Family.CanonicalProvider()
	if canonProv != "" {
		for _, r := range group {
			if r.Provider == canonProv {
				return r
			}
		}
	}
	// Lexicographic tiebreak: smallest Provider string for determinism.
	rep := group[0]
	for _, r := range group[1:] {
		if r.Provider < rep.Provider {
			rep = r
		}
	}
	return rep
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
		// Try matching on Family (exact canonical family name match).
		// This allows inputs like "claude" to match all claude-family models.
		if string(m.Family) == matchInput {
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
// "claude/opus@2025-11-01", "claude/opus/4.5@2025-11-01[thinking]", or
// "anthropic/claude/haiku@2024-10-22[latest]") and checks whether the model m
// matches the parsed (family, variant, version, date, modifier) tuple.
//
// Parsing rules:
//  1. Strip "[modifier]" bracket suffix if present.
//  2. Strip "@date" suffix if present.
//  3. Split remaining segments on "/".
//  4. Provider-prefix detection: when 4 segments remain (provider/family/variant/version),
//     segment[0] is treated as a provider prefix and skipped.
//     Similarly for 3 segments (provider/family/variant) when segment[0] does not match
//     the model's Family but segment[1] does — the provider prefix is skipped.
//  5. Segment[0] = family; segment[1] = variant (if present); segment[2] = version (if present).
//
// Matching rules:
//   - family must match Family (required).
//   - variant must match Variant when specified.
//   - version must match Version when specified.
//   - date must match Date when specified.
//   - modifier must match Modifier when specified (non-empty bracket suffix).
func matchCanonicalSegments(m ModelInfo, matchInput string) bool {
	// Extract "[modifier]" bracket suffix.
	// Must be done BEFORE stripping "@date" so the bracket is not confused with
	// the date field when the date is absent.
	var modifierFilter string
	if lb := strings.LastIndex(matchInput, "["); lb >= 0 {
		if rb := strings.LastIndex(matchInput, "]"); rb == len(matchInput)-1 && rb > lb {
			modifierFilter = matchInput[lb+1 : rb]
			matchInput = matchInput[:lb]
		}
	}

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

	// Provider-prefix handling: when 4 segments are present
	// (provider/family/variant/version), segment[0] is the provider.
	// When 3 segments are present (provider/family/variant or family/variant/version),
	// try segment[0] as provider: if it does not match the model's Family but segment[1]
	// does, skip segment[0] as a provider prefix.
	if len(segments) == 4 {
		// 4 segments: always treat segment[0] as provider.
		segments = segments[1:]
	} else if len(segments) == 3 && string(m.Family) != segments[0] && string(m.Family) == segments[1] {
		// 3 segments: first is provider (doesn't match Family), second is family.
		segments = segments[1:]
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
	if string(m.Family) != familyFilter {
		return false
	}
	// Variant filter: when specified, must match.
	if variantFilter != "" && m.Variant != variantFilter {
		return false
	}
	// Version filter: when specified, must match.
	if versionFilter != "" && m.Version != versionFilter {
		return false
	}
	// Date filter: when specified, must match.
	if dateFilter != "" && m.Date != dateFilter {
		return false
	}
	// Modifier filter: when specified (bracket suffix present), must match the
	// model's order-independent canonical modifier key. The bracket
	// suffix renders the same canonical comma-joined form (ModelRef.Format), so a
	// round-tripped "[vision,instruct]" matches a model with Modifier
	// ["instruct","vision"].
	if modifierFilter != "" && modifierKey(m.Modifier) != modifierFilter {
		return false
	}
	return true
}
