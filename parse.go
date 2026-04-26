package bestiary

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

//go:embed parse/data/*.json
var parseDataFS embed.FS

// familyOverride holds the explicit decomposition of a raw API family value.
type familyOverride struct {
	Family  Family `json:"family"`
	Variant string `json:"variant"`
}

// versionPattern holds a named regex pattern for versioned-variant decomposition.
type versionPattern struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Regex       string `json:"regex"`
}

// parseData holds all loaded parse configuration.
type parseData struct {
	// overrides maps raw family string → (Family, variant) override.
	overrides map[Family]familyOverride

	// suffixes is the ordered list of variant suffixes to strip (longest-first).
	suffixes []string

	// patterns is the ordered list of compiled versioned-variant regex patterns.
	patterns []*compiledPattern
}

// compiledPattern is a versionPattern with its compiled regexp.
type compiledPattern struct {
	versionPattern
	re         *regexp.Regexp
	baseIdx    int // index of "base" submatch
	variantIdx int // index of "variant" submatch
}

var (
	parseOnce sync.Once
	parsed    *parseData
	parseErr  error
)

// loadParseData loads and parses the embedded JSON data files once.
// Subsequent calls return the cached result. Not safe for concurrent
// initialization — sync.Once guarantees single execution.
func loadParseData() (*parseData, error) {
	parseOnce.Do(func() {
		parsed, parseErr = initParseData()
	})
	return parsed, parseErr
}

// initParseData reads and unmarshals all three JSON data files from the
// embedded filesystem. Called exactly once by loadParseData.
func initParseData() (*parseData, error) {
	// Load family_overrides.json.
	rawOverrides, err := parseDataFS.ReadFile("parse/data/family_overrides.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load family_overrides.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/family_overrides.json\n"+
				"  Why: file missing from embedded FS (should not happen in production build)\n"+
				"  How to fix: ensure parse/data/*.json files are present before running go generate",
			err,
		)
	}

	// The JSON object has a top-level "_comment" key plus family-string keys.
	// We unmarshal into map[string]json.RawMessage to skip the comment key.
	var rawOverridesMap map[string]json.RawMessage
	if err := json.Unmarshal(rawOverrides, &rawOverridesMap); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse family_overrides.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/family_overrides.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}

	overrides := make(map[Family]familyOverride, len(rawOverridesMap))
	for key, raw := range rawOverridesMap {
		if key == "_comment" {
			continue
		}
		var ov familyOverride
		if err := json.Unmarshal(raw, &ov); err != nil {
			return nil, fmt.Errorf(
				"bestiary parse: parse family_overrides.json entry %q: %w",
				key, err,
			)
		}
		overrides[Family(key)] = ov
	}

	// Load variant_suffixes.json.
	rawSuffixes, err := parseDataFS.ReadFile("parse/data/variant_suffixes.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load variant_suffixes.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/variant_suffixes.json",
			err,
		)
	}

	var suffixFile struct {
		Comment  string   `json:"_comment"`
		Suffixes []string `json:"suffixes"`
	}
	if err := json.Unmarshal(rawSuffixes, &suffixFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse variant_suffixes.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/variant_suffixes.json",
			err,
		)
	}

	// Ensure suffixes are sorted longest-first for correct greedy matching.
	suffixes := make([]string, len(suffixFile.Suffixes))
	copy(suffixes, suffixFile.Suffixes)
	sort.Slice(suffixes, func(i, j int) bool {
		return len(suffixes[i]) > len(suffixes[j])
	})

	// Load version_patterns.json.
	rawPatterns, err := parseDataFS.ReadFile("parse/data/version_patterns.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load version_patterns.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/version_patterns.json",
			err,
		)
	}

	var patternFile struct {
		Comment  string           `json:"_comment"`
		Patterns []versionPattern `json:"patterns"`
	}
	if err := json.Unmarshal(rawPatterns, &patternFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse version_patterns.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/version_patterns.json",
			err,
		)
	}

	compiled := make([]*compiledPattern, 0, len(patternFile.Patterns))
	for _, p := range patternFile.Patterns {
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			return nil, fmt.Errorf(
				"bestiary parse: compile version pattern %q: %w\n"+
					"  Where: parse/data/version_patterns.json pattern %q\n"+
					"  Regex: %s",
				p.Name, err, p.Name, p.Regex,
			)
		}
		cp := &compiledPattern{
			versionPattern: p,
			re:             re,
		}
		// Locate named subgroup indices.
		for i, name := range re.SubexpNames() {
			switch name {
			case "base":
				cp.baseIdx = i
			case "variant":
				cp.variantIdx = i
			}
		}
		compiled = append(compiled, cp)
	}

	return &parseData{
		overrides: overrides,
		suffixes:  suffixes,
		patterns:  compiled,
	}, nil
}

// ParseDataReady returns the error (if any) from the one-time initialization of
// the embedded parse data (JSON files + regex compilation). In a correct build
// the return value is always nil, because the data files are embedded at compile
// time and the regexes are validated before any release.
//
// This function is primarily useful for startup self-checks and tests. Production
// code does not need to call it — ParseFamily degrades gracefully when the load
// fails (see the fail-closed comment inside ParseFamily).
func ParseDataReady() error {
	_, err := loadParseData()
	return err
}

// ParseFamily takes a raw API family value and returns (Family, variant).
//
// Resolution order (first match wins):
//  1. family_overrides table — explicit (raw → Family, variant) mappings.
//  2. Versioned-variant patterns — v/k/m/no-prefix and hyphen-separated versions.
//     For hyphen-version matches, the non-numeric prefix is itself resolved via overrides.
//  3. Suffix-stripping table — strip the longest matching suffix to identify variant.
//  4. Fallback — return (raw, "") unchanged.
//
// ParseFamily is deterministic: the same input always produces the same output.
// Empty raw returns ("", "").
func ParseFamily(raw Family) (Family, string) {
	if raw == "" {
		return "", ""
	}

	pd, err := loadParseData()
	if err != nil {
		// Fail closed: if embedded data cannot be loaded, return the raw value
		// unchanged with an empty variant. In a correct build this path is
		// unreachable because the JSON files are embedded at compile time and the
		// regexes in version_patterns.json are validated once at startup. The
		// silent degradation is intentional — ParseFamily has a 2-return signature
		// by design (see PROPOSAL-3) and callers cannot handle an error return.
		// TestParseData_RegexesValid asserts that this path is never taken in a
		// normal test run, providing startup-time validation coverage.
		return raw, ""
	}

	// Step 1: Check explicit overrides table.
	if ov, ok := pd.overrides[raw]; ok {
		return ov.Family, ov.Variant
	}

	rawStr := string(raw)

	// Step 2: Try versioned-variant patterns (in order).
	for _, cp := range pd.patterns {
		m := cp.re.FindStringSubmatch(rawStr)
		if m == nil {
			continue
		}
		base := m[cp.baseIdx]
		variant := m[cp.variantIdx]

		// For hyphen-version pattern, look up the base in overrides.
		// e.g. "claude-opus-4-5": base="claude-opus", version="4-5"
		//   → overrides["claude-opus"] = {family:"claude", variant:"opus"}
		//   → result: family="claude", variant="opus-4-5"
		if cp.Name == "hyphen-version" {
			if ov, ok := pd.overrides[Family(base)]; ok {
				combined := variant
				if ov.Variant != "" {
					combined = ov.Variant + "-" + variant
				}
				return ov.Family, combined
			}
			// No override for the base; return base as family.
			return Family(base), variant
		}

		return Family(base), variant
	}

	// Step 3: Suffix stripping (longest-first is already ensured by initParseData).
	for _, suffix := range pd.suffixes {
		if strings.HasSuffix(rawStr, suffix) {
			base := rawStr[:len(rawStr)-len(suffix)]
			variant := suffix[1:] // strip leading "-"
			return Family(base), variant
		}
	}

	// Step 4: Fallback — return raw unchanged with empty variant.
	return raw, ""
}

// ParseFamilyWithVersion takes a raw API family value and returns
// (Family, variant, version) — a three-way decomposition that separates the
// semantic model version (e.g. "4.5") from the variant (e.g. "opus").
//
// This is an additive companion to ParseFamily. For inputs that ParseFamily
// already handles correctly (overrides, v/k/m/no-prefix patterns, suffix
// stripping), the family and variant return values are identical to ParseFamily.
// The new third return value extracts the numeric version component when the
// raw family string embeds one via the hyphen-version pattern or a dot-version
// tail (e.g. "gemini-2.5-flash").
//
// Resolution order (first match wins):
//  1. family_overrides table — no version for override entries.
//  2. hyphen-version pattern — converts hyphenated digits to dot notation:
//     "claude-opus-4-5" → (claude, opus, 4.5); "llama-3-1" → (llama, "", 3.1).
//  3. Other versioned patterns (v/k/m/no-prefix) — version stays in variant, version="".
//  4. Suffix stripping + dot-version detection:
//     "gemini-2.5-flash" → suffix strip yields base="gemini-2.5"; detect "-N.M"
//     tail → (gemini, flash, 2.5).
//  5. Dot-version fallback: "gemini-2.5" → (gemini, "", 2.5).
//  6. Pure fallback — same as ParseFamily, version="".
//
// ParseFamilyWithVersion is deterministic. Empty raw returns ("", "", "").
func ParseFamilyWithVersion(raw Family) (Family, string, string) {
	if raw == "" {
		return "", "", ""
	}

	pd, err := loadParseData()
	if err != nil {
		// Fail closed: same rationale as ParseFamily.
		return raw, "", ""
	}

	// Step 1: Check explicit overrides table. No version for override entries —
	// overrides encode stable (family, variant) pairs without a version component.
	if ov, ok := pd.overrides[raw]; ok {
		return ov.Family, ov.Variant, ""
	}

	rawStr := string(raw)

	// Step 2: Try versioned-variant patterns.
	for _, cp := range pd.patterns {
		m := cp.re.FindStringSubmatch(rawStr)
		if m == nil {
			continue
		}
		base := m[cp.baseIdx]
		variantStr := m[cp.variantIdx]

		if cp.Name == "hyphen-version" {
			// Convert hyphen-separated digit tokens to dot notation.
			// e.g. "4-5" → "4.5"; "3-1" → "3.1"; "4" → "4".
			version := strings.ReplaceAll(variantStr, "-", ".")

			if ov, ok := pd.overrides[Family(base)]; ok {
				// The base has a known decomposition; the numeric version is extracted
				// separately from the override's variant.
				// e.g. "claude-opus-4-5": base="claude-opus" → (claude, opus), version="4.5"
				return ov.Family, ov.Variant, version
			}
			// Base not in overrides; treat base as the family directly.
			// e.g. "llama-3-1": base="llama" → (llama, "", "3.1")
			return Family(base), "", version
		}

		// For all other patterns (v-prefix, k-prefix, m-prefix, no-prefix):
		// the version-like string (e.g. "k2.5", "3.5") stays in the variant field
		// as ParseFamily returns it. These encode version in their own notation and
		// separating them from the "variant" concept adds no value at this time.
		// version remains "".
		return Family(base), variantStr, ""
	}

	// Step 3: Suffix stripping + dot-version detection.
	// "gemini-2.5-flash": suffix "-flash" → base="gemini-2.5" → extractDotVersion → (gemini, "2.5")
	for _, suffix := range pd.suffixes {
		if strings.HasSuffix(rawStr, suffix) {
			trimmedBase := rawStr[:len(rawStr)-len(suffix)]
			variantSuffix := suffix[1:] // strip leading "-"
			if baseWithoutVer, ver := extractDotVersion(trimmedBase); ver != "" {
				return Family(baseWithoutVer), variantSuffix, ver
			}
			return Family(trimmedBase), variantSuffix, ""
		}
	}

	// Step 4: Dot-version fallback.
	// "gemini-2.5" → (gemini, "", "2.5")
	if baseWithoutVer, ver := extractDotVersion(rawStr); ver != "" {
		return Family(baseWithoutVer), "", ver
	}

	// Step 5: Pure fallback — return raw unchanged, no version.
	return raw, "", ""
}

// extractDotVersion detects a trailing "-N.M" version suffix in s and splits
// it off. Returns (base, "N.M") when found, or (s, "") when not.
//
// Examples:
//
//	"gemini-2.5" → ("gemini", "2.5")
//	"somemodel-10.3" → ("somemodel", "10.3")
//	"gpt-4o" → ("gpt-4o", "")  (no dot in the version token)
func extractDotVersion(s string) (string, string) {
	m := reDotVersionSuffix.FindStringSubmatch(s)
	if m == nil {
		return s, ""
	}
	return m[1], m[2]
}

// ExtractVersionFromID extracts a numeric version (e.g. "4.5", "4.6", "2.5",
// "4o") from a model ID after stripping the known family prefix. Returns ""
// if no version-like token follows the family prefix.
//
// This is the ID-as-authoritative-source companion to ParseFamilyWithVersion.
// It is called by codegen (genToModelInfo) when ParseFamilyWithVersion on the
// raw family field yields an empty version — which is the common case because
// the upstream models.dev API family strings do not embed version numbers
// ("claude-opus" not "claude-opus-4-5"). The model ID is where the version
// lives ("claude-opus-4-5-20251101", "claude-opus-4-6").
//
// Algorithm:
//  1. Strip "<rawFamily>-" from the start of id. If the ID does not begin
//     with that prefix, return "".
//  2. Strip any trailing compact date (YYYYMMDD or YYYY-MM-DD) from the
//     remainder, since those are not version tokens.
//  3. From the remaining string, attempt version extraction:
//     a. All-hyphen-separated-digit tokens (e.g. "4-5") → dot-join → "4.5"
//     b. Dot-version suffix (e.g. "2.5") → return directly
//     c. Single alphanumeric-suffix token (e.g. "4o") → return as-is
//     d. Otherwise → return ""
//
// Examples:
//
//	ExtractVersionFromID("claude-opus-4-5-20251101", "claude-opus") → "4.5"
//	ExtractVersionFromID("claude-opus-4-6-20250514", "claude-opus") → "4.6"
//	ExtractVersionFromID("claude-opus-4-6",          "claude-opus") → "4.6"
//	ExtractVersionFromID("gemini-2.5-flash",         "gemini")      → "2.5"
//	ExtractVersionFromID("gpt-4o",                   "gpt")         → "4o"
//	ExtractVersionFromID("claude-opus",              "claude-opus") → ""
//	ExtractVersionFromID("claude-3-5-sonnet-20241022","claude")     → ""  (non-version interleaved tokens)
func ExtractVersionFromID(id ModelID, rawFamily Family) string {
	if id == "" || rawFamily == "" {
		return ""
	}
	idStr := string(id)
	prefix := string(rawFamily) + "-"
	if !strings.HasPrefix(idStr, prefix) {
		return ""
	}
	// remainder: everything after the "<family>-" prefix.
	remainder := idStr[len(prefix):]
	if remainder == "" {
		return ""
	}

	// Strip trailing compact date (YYYYMMDD or YYYY-MM-DD) from the remainder.
	// We do this on the full remainder string so the date suffix does not
	// contaminate version token detection.
	remainder = stripTrailingDate(remainder)
	if remainder == "" {
		return ""
	}

	// Path (a): all tokens are purely numeric → hyphen-separated digits → dot-join.
	// e.g. "4-5" → "4.5"; "4-6" → "4.6"; "3-1" → "3.1"
	if reHyphenDigits.MatchString(remainder) {
		return strings.ReplaceAll(remainder, "-", ".")
	}

	// Path (b): dot-version — the remainder is itself a "N.M" string (after date strip).
	// e.g. "2.5" left by "gemini-2.5-flash" after prefix strip and no date present.
	// We only have the remainder after <family>- so this handles single dot-version tokens.
	if reBareVersion.MatchString(remainder) {
		return remainder
	}

	// Path (c): single alphanumeric-suffix token (e.g. "4o" from "gpt-4o").
	// Must start with a digit and contain only alphanumeric characters (no hyphens).
	// Must not be a pure-alpha word (which would be a variant, not a version).
	if reAlphaNumVersion.MatchString(remainder) {
		return remainder
	}

	// Path (d): multi-segment remainder with a dot-version prefix.
	// e.g. "2.5-flash" from "gemini-2.5-flash" after stripping "gemini-"
	// Extract the leading dot-version segment.
	if idx := strings.Index(remainder, "-"); idx > 0 {
		lead := remainder[:idx]
		if reBareVersion.MatchString(lead) {
			return lead
		}
	}

	return ""
}

// stripTrailingDate removes a trailing compact (YYYYMMDD) or dash-separated
// (YYYY-MM-DD) date from s. Returns s unchanged if no date is found at the end.
func stripTrailingDate(s string) string {
	// Try YYYY-MM-DD at the end: last three hyphen-joined segments totalling 10 chars.
	if m := reTrailingDashDate.FindStringIndex(s); m != nil {
		trimmed := s[:m[0]]
		return strings.TrimRight(trimmed, "-")
	}
	// Try compact YYYYMMDD at the end.
	if m := reTrailingCompactDate.FindStringIndex(s); m != nil {
		trimmed := s[:m[0]]
		return strings.TrimRight(trimmed, "-")
	}
	return s
}

// reHyphenDigits matches strings that are entirely hyphen-separated digit groups.
// e.g. "4-5", "3-1", "4-6", "10-3" — but NOT "4o", "2.5", "flash"
var reHyphenDigits = regexp.MustCompile(`^\d+(?:-\d+)*$`)

// reBareVersion matches a bare "N.M" dot-version with optional additional segments.
// e.g. "2.5", "10.3" — but NOT "4o", "4-5", "flash"
var reBareVersion = regexp.MustCompile(`^\d+\.\d+$`)

// reAlphaNumVersion matches a single token that starts with a digit and contains
// only alphanumeric characters (letters and digits). This captures version
// suffixes like "4o" (gpt-4o) that are not purely numeric and have no separators.
// Must not be purely alphabetic (that would be a variant word, not a version).
var reAlphaNumVersion = regexp.MustCompile(`^\d[a-zA-Z0-9]*$`)

// reTrailingDashDate matches a YYYY-MM-DD date at the end of a string,
// optionally preceded by a hyphen.
var reTrailingDashDate = regexp.MustCompile(`-?\d{4}-\d{2}-\d{2}$`)

// reTrailingCompactDate matches a compact YYYYMMDD date at the end of a string,
// optionally preceded by a hyphen.
var reTrailingCompactDate = regexp.MustCompile(`-?\d{8}$`)

// reDotVersionSuffix matches a string of the form "<base>-<MAJOR>.<MINOR>"
// where base is one or more alphanumeric segments separated by hyphens.
// The version must end the string (no trailing suffix).
var reDotVersionSuffix = regexp.MustCompile(`^(.+)-(\d+\.\d+)$`)

// ExtractDate extracts a date string from a model ID or release date field,
// normalizing to the YYYY-MM-DD form.
//
// Matching priority:
//  1. id is scanned for a YYYYMMDD or YYYY-MM-DD substring.
//  2. If id has no match, releaseDate is scanned.
//
// Returns "" when no date is found in either field.
// The returned string always uses the YYYY-MM-DD format (hyphens added for YYYYMMDD).
func ExtractDate(id ModelID, releaseDate string) string {
	if d := extractDateFromString(string(id)); d != "" {
		return d
	}
	return extractDateFromString(releaseDate)
}

// extractDateFromString scans s for a YYYY-MM-DD or YYYYMMDD date pattern.
// Returns normalized YYYY-MM-DD on match, or "" when no date is found.
// YYYY-MM-DD is tried first (higher precision/readability); YYYYMMDD second.
//
// Calendar validation: the regex narrows candidates to structurally valid
// digit sequences, but time.Parse("2006-01-02", ...) is used as the final
// gate. Inputs like "9999-99-99" (invalid month/day) are rejected and ""
// is returned. Only dates parseable by the standard library are accepted.
func extractDateFromString(s string) string {
	if s == "" {
		return ""
	}
	// Try YYYY-MM-DD first (it's unambiguous and common in model IDs).
	if m := reYYYYDashMMDashDD.FindStringSubmatch(s); m != nil {
		candidate := m[1] + "-" + m[2] + "-" + m[3]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	// Try compact YYYYMMDD (e.g. "claude-opus-4-20250514").
	if m := reYYYYMMDD.FindStringSubmatch(s); m != nil {
		candidate := m[1] + "-" + m[2] + "-" + m[3]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// InferFamilyFromIDWithVariant is the extended empty-family fallback for models
// whose API family field is empty (~25% of models). Unlike InferFamilyFromID,
// it extracts (Family, Variant, Version) by:
//  1. Inferring the family from the first token of the model ID.
//  2. Deriving the raw family string from the inferred family + remaining tokens
//     (treating the ID after the family prefix as a family-like string for parsing).
//  3. Applying ParseFamilyWithVersion on the derived family string to extract
//     variant and version using the same suffix/pattern logic as the non-empty
//     family path in genToModelInfo.
//
// This ensures (NormalizedFamily, NormalizedVariant, NormalizedVersion) is
// consistent across providers regardless of whether raw_family is empty or populated.
//
// Examples:
//
//	InferFamilyFromIDWithVariant("claude-opus-4-5-20251101", "nano-gpt") → ("claude", "opus", "4.5")
//	InferFamilyFromIDWithVariant("claude-opus-4-6", "some-provider")    → ("claude", "opus", "4.6")
//	InferFamilyFromIDWithVariant("gpt-4o", "openai")                    → ("gpt", "", "4o")
//
// The provider parameter is reserved for future provider-specific heuristics
// and is not currently used.
func InferFamilyFromIDWithVariant(id ModelID, p Provider) (Family, string, string) {
	if id == "" {
		return "", "", ""
	}
	idStr := string(id)

	// Step 1: strip trailing date tokens so they don't contaminate family inference.
	stripped := stripTrailingDate(idStr)
	if stripped == "" {
		stripped = idStr
	}

	tokens := strings.Split(stripped, "-")
	if len(tokens) == 0 {
		return "", "", ""
	}
	// Take only the first alphabetic-leading token as the family seed.
	first := tokens[0]
	if first == "" || !unicode.IsLetter(rune(first[0])) {
		return "", "", ""
	}

	// Step 2: reconstruct a "raw family" string from first token + remaining tokens
	// (excluding trailing purely-numeric tokens which are version components).
	// Then run ParseFamilyWithVersion on it to get (family, variant, version).
	//
	// Example: "claude-opus-4-5" (date already stripped from "claude-opus-4-5-20251101")
	//   → tokens = ["claude", "opus", "4", "5"]
	//   → strip trailing numeric tokens: ["claude", "opus", "4", "5"]
	//     but we feed the whole thing as a family string to ParseFamilyWithVersion.
	//
	// Build the candidate family string: all tokens (no date stripping already done above).
	candidateFamilyStr := stripped // e.g. "claude-opus-4-5" or "claude-opus-4-6"

	family, variant, version := ParseFamilyWithVersion(Family(candidateFamilyStr))

	// If ParseFamilyWithVersion returns the raw string unchanged (no pattern matched),
	// it means the entire string is treated as a family with no variant or version.
	// Fall back to InferFamilyFromID behaviour: use only the first token.
	if family == Family(candidateFamilyStr) {
		return Family(first), "", ""
	}

	// If version is still empty, try ExtractVersionFromID.
	if version == "" && family != "" {
		version = ExtractVersionFromID(id, family)
	}

	return family, variant, version
}

// InferFamilyFromID is the empty-family fallback for models whose API family field
// is empty (~25% of models). It uses the model ID as a heuristic signal.
//
// Algorithm:
//  1. Split id on "-".
//  2. Consume trailing tokens that are purely version-like (all digits, or match
//     a version pattern) — these are noise, not signal.
//  3. Take the first remaining token as the inferred family.
//  4. Return "" when no alphabetic-leading token is found.
//
// The provider parameter is reserved for future provider-specific heuristics
// and is not currently used.
func InferFamilyFromID(id ModelID, p Provider) Family {
	if id == "" {
		return ""
	}
	tokens := splitAndStripVersionTail(string(id))
	if len(tokens) == 0 {
		return ""
	}
	// Take the first token as the family, but only if it begins with a letter.
	first := tokens[0]
	if first == "" || !unicode.IsLetter(rune(first[0])) {
		return ""
	}
	return Family(first)
}

// splitAndStripVersionTail splits s on "-" and removes trailing tokens that are
// purely numeric (version-like). Returns remaining tokens.
// Used by InferFamilyFromID to discard version suffixes from model IDs.
func splitAndStripVersionTail(s string) []string {
	tokens := strings.Split(s, "-")
	// Walk from the end and drop purely-numeric or date-like tokens.
	for len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		if isVersionToken(last) {
			tokens = tokens[:len(tokens)-1]
		} else {
			break
		}
	}
	return tokens
}

// isVersionToken returns true when tok is a purely-numeric token (all digits).
// Used to strip trailing version components from model IDs.
func isVersionToken(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// reYYYYMMDD matches an 8-digit date string not preceded or followed by a digit.
// Captured as YYYY, MM, DD in groups 1, 2, 3.
var reYYYYMMDD = regexp.MustCompile(`(?:^|[^0-9])(\d{4})(\d{2})(\d{2})(?:$|[^0-9])`)

// reYYYYDashMMDashDD matches YYYY-MM-DD date strings.
var reYYYYDashMMDashDD = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)
