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
