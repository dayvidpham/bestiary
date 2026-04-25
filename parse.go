package bestiary

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	panic("not yet implemented — bodies land in L4")
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
	panic("not yet implemented — bodies land in L4")
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
	panic("not yet implemented — bodies land in L4")
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

// isVersionToken returns true when tok is a purely-numeric token or looks like
// a date component (8-digit YYYYMMDD or 4-digit year).
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
