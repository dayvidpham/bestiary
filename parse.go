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

// familyInfo holds the member list and bare-gen-split flag for a single family.
// Populated from parse/data/families.json (SLICE-1). Keys in families.json are
// lowercase and validated against allFamilies at load.
//
// Members is the curated list of variant tokens for this family (e.g. ["opus",
// "sonnet", "haiku"] for "claude"). Used by recoverMemberVariant to identify
// variant tokens in model IDs.
//
// BareGenSplit drives the M2 bare-generation split predicate (SLICE-2). Set to
// provisional values by SLICE-1; SLICE-2 finalizes attested flags.
//
// SeriesLetter (SLICE-8 d, CLARIFICATION-5) marks a letter-prefix model series:
// a single lowercase letter (e.g. "k" for kimi, "m" for minimax, "v" for mimo)
// such that an ID token of the form "<letter><number>" (kimi-k2, minimax-m1,
// mimo-v2.5) decomposes to variant=<letter> + version=<number>, instead of the
// whole token becoming the variant. Empty when the family has no letter series.
type familyInfo struct {
	Members      []string `json:"members"`
	BareGenSplit bool     `json:"bare_gen_split"`
	SeriesLetter string   `json:"series_letter,omitempty"`
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

	// modifiers is the sorted longest-first list of known modifier tokens
	// (e.g. "thinking", "vision"). Populated from parse/data/modifiers.json.
	modifiers []string

	// families maps lowercase Family → familyInfo (members + bare_gen_split flag).
	// Populated from parse/data/families.json. Keys validated against allFamilies
	// at load; fail-fast on unknown key.
	families map[Family]familyInfo

	// vendorAliases is the list of lowercase vendor alias names (non-provider
	// vendors) from parse/data/vendor_aliases.json. Used for M3 vendor strip:
	// model IDs starting with <alias>/ or <alias>- have the prefix stripped.
	vendorAliases []string

	// familyAliases is the canonical-winner ledger (SLICE-3): mislabel/shorthand
	// family key → canonical family. Populated from parse/data/family_aliases.json.
	// Keys are lowercase mislabels (need NOT be canonical); VALUES are canonical
	// families validated against allFamilies at load (fail-fast). Applied after M4
	// family normalisation and before bare-gen-split in both parse entrypoints.
	familyAliases map[Family]Family

	// enforceFamilies is the SLICE-12 CLOSED canonical-winner ENFORCE set
	// (parse/data/family_enforce.json). When the ID-driven family is in this set and
	// DISAGREES with raw_family, the ID-driven decomposition wins (reconcileIDDriven).
	// Covers the own-family-enforce ledger (distinct model mislabelled as its parent)
	// and the vendor/org-namespace leak (raw_family is the ID's org, not a family).
	enforceFamilies map[Family]struct{}
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

	// Load modifiers.json.
	rawModifiers, err := parseDataFS.ReadFile("parse/data/modifiers.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load modifiers.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/modifiers.json\n"+
				"  Why: file missing from embedded FS (should not happen in production build)\n"+
				"  How to fix: ensure parse/data/modifiers.json is present before running go build",
			err,
		)
	}

	var modifierFile struct {
		Comment   string   `json:"_comment"`
		SchemaVer int      `json:"schema_version"`
		Modifiers []string `json:"modifiers"`
	}
	if err := json.Unmarshal(rawModifiers, &modifierFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse modifiers.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/modifiers.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}

	// Ensure modifiers are sorted longest-first for greedy matching
	// (prevents "think" from shadowing "thinking" when both are in the list).
	modifiers := make([]string, len(modifierFile.Modifiers))
	copy(modifiers, modifierFile.Modifiers)
	sort.Slice(modifiers, func(i, j int) bool {
		return len(modifiers[i]) > len(modifiers[j])
	})

	// Load families.json (SLICE-1).
	rawFamilies, err := parseDataFS.ReadFile("parse/data/families.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load families.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/families.json\n"+
				"  Why: file missing from embedded FS (should not happen in production build)\n"+
				"  How to fix: ensure parse/data/families.json is present before running go build",
			err,
		)
	}

	// Unmarshal into map[string]json.RawMessage so we can skip _comment.
	var rawFamiliesMap map[string]json.RawMessage
	if err := json.Unmarshal(rawFamilies, &rawFamiliesMap); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse families.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/families.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}

	families := make(map[Family]familyInfo, len(rawFamiliesMap))
	for key, raw := range rawFamiliesMap {
		if key == "_comment" {
			continue
		}
		if !familyKeyKnown(key) {
			return nil, fmt.Errorf(
				"bestiary parse: families.json key %q is not a known family\n"+
					"  What: unrecognised family key in parse/data/families.json\n"+
					"  Where: families.json entry %q\n"+
					"  Why: key does not match (case-insensitively) any value in allFamilies"+
					" (families_gen.go:182-357)\n"+
					"  How to fix: check for typos; valid keys must match lowercase allFamilies"+
					" values (e.g. \"claude\", \"gpt\", \"qwen\")",
				key, key,
			)
		}
		var info familyInfo
		if err := json.Unmarshal(raw, &info); err != nil {
			return nil, fmt.Errorf(
				"bestiary parse: parse families.json entry %q: %w\n"+
					"  How to fix: validate JSON syntax for this entry",
				key, err,
			)
		}
		// Store under the lowercase key so recoverMemberVariant lookups use M4-normalised
		// family values.
		families[Family(strings.ToLower(key))] = info
	}

	// Load vendor_aliases.json (SLICE-1).
	rawVendorAliases, err := parseDataFS.ReadFile("parse/data/vendor_aliases.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load vendor_aliases.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/vendor_aliases.json\n"+
				"  Why: file missing from embedded FS (should not happen in production build)\n"+
				"  How to fix: ensure parse/data/vendor_aliases.json is present before running go build",
			err,
		)
	}

	var vendorAliasFile struct {
		Comment string   `json:"_comment"`
		Vendors []string `json:"vendors"`
	}
	if err := json.Unmarshal(rawVendorAliases, &vendorAliasFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse vendor_aliases.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/vendor_aliases.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}
	// Normalise to lowercase for case-insensitive prefix matching at runtime.
	vendorAliases := make([]string, len(vendorAliasFile.Vendors))
	for i, v := range vendorAliasFile.Vendors {
		vendorAliases[i] = strings.ToLower(v)
	}

	// Load family_aliases.json (SLICE-3 canonical-winner ledger).
	rawAliases, err := parseDataFS.ReadFile("parse/data/family_aliases.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load family_aliases.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/family_aliases.json\n"+
				"  Why: file missing from embedded FS (should not happen in production build)\n"+
				"  How to fix: ensure parse/data/family_aliases.json is present before running go build",
			err,
		)
	}
	if err := FamilyAliasesJSONError(rawAliases); err != nil {
		return nil, err
	}
	var aliasFile struct {
		Comment string            `json:"_comment"`
		Aliases map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(rawAliases, &aliasFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse family_aliases.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/family_aliases.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}
	// Normalise keys to lowercase (M4 boundary); values are canonical families.
	familyAliases := make(map[Family]Family, len(aliasFile.Aliases))
	for key, target := range aliasFile.Aliases {
		familyAliases[Family(strings.ToLower(key))] = Family(strings.ToLower(target))
	}

	// Load family_enforce.json (SLICE-12 canonical-winner enforce set).
	rawEnforce, err := parseDataFS.ReadFile("parse/data/family_enforce.json")
	if err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: load family_enforce.json: %w\n"+
				"  What: cannot read embedded parse data file\n"+
				"  Where: parse/data/family_enforce.json\n"+
				"  How to fix: ensure parse/data/family_enforce.json is present before running go build",
			err,
		)
	}
	var enforceFile struct {
		Comment  string   `json:"_comment"`
		Families []string `json:"families"`
	}
	if err := json.Unmarshal(rawEnforce, &enforceFile); err != nil {
		return nil, fmt.Errorf(
			"bestiary parse: parse family_enforce.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/family_enforce.json\n"+
				"  How to fix: validate JSON syntax in the data file",
			err,
		)
	}
	enforceFamilies := make(map[Family]struct{}, len(enforceFile.Families))
	for _, f := range enforceFile.Families {
		enforceFamilies[Family(strings.ToLower(f))] = struct{}{}
	}

	return &parseData{
		overrides:       overrides,
		suffixes:        suffixes,
		patterns:        compiled,
		modifiers:       modifiers,
		families:        families,
		vendorAliases:   vendorAliases,
		familyAliases:   familyAliases,
		enforceFamilies: enforceFamilies,
	}, nil
}

// familyKeyKnown reports whether key (case-insensitive) matches an entry in
// allFamilies. Used by initParseData and FamiliesJSONKeyError to validate
// families.json keys. Reusable by SLICE-3's family_aliases.json validator.
func familyKeyKnown(key string) bool {
	lower := strings.ToLower(key)
	for _, f := range allFamilies {
		if strings.ToLower(string(f)) == lower {
			return true
		}
	}
	return false
}

// IsKnownFamily reports whether f is a CANONICAL registered family — a value present
// (case-insensitively) in the generated allFamilies registry (families_gen.go), which
// is derived directly from the upstream models.dev family field. Synthetic OVER-CAPTURE
// family strings the ID-path may transiently produce (e.g. "claude-opus", "qwen3-vl-72b",
// "phi-4-mini") are NOT in that registry. Exposed for the SLICE-11 before/after-diff gate,
// which uses "after ∈ registry ∧ before ∉ registry" as an INDEPENDENT, data-grounded
// signal that a family change reduced an over-capture to its canonical short base (and so
// is a genuine fix, not a regression) — the registry is curated upstream data, not the
// reducer's own logic, so the check does not merely rubber-stamp the implementation.
func IsKnownFamily(f Family) bool {
	return familyKeyKnown(string(f))
}

// IsEnforcedCanonicalFamily reports whether f is in the SLICE-12 CLOSED canonical-winner
// ENFORCE set (parse/data/family_enforce.json) — a curated list of DISTINCT model families
// that win over a disagreeing raw_family parent/org mislabel (aion, hermes, mixtral, qwq,
// …). Exposed for the before/after-diff gate (cmd/bestiary-gen): a lateral family change
// whose AFTER family is in this set is a SANCTIONED ledger correction (the ID-canonical
// distinct family beat a parent/org mislabel), not a regression — analogous to the SLICE-11
// "after ∈ registry" signal but for lateral (non-reduction) corrections. The set is curated
// data tied to the rc2 ledger + bestiary-xdbc Q3, NOT the categorizer's own logic, so this
// does not rubber-stamp an arbitrary family rewrite.
func IsEnforcedCanonicalFamily(f Family) bool {
	pd, err := loadParseData()
	if err != nil {
		return false
	}
	_, ok := pd.enforceFamilies[Family(strings.ToLower(string(f)))]
	return ok
}

// FamiliesJSONKeyError validates that every non-comment key in the raw
// families.json-style bytes is a known Family (case-insensitive match against
// allFamilies). Returns nil when all keys are valid, or an actionable error on
// the first unrecognised key.
//
// Used by tests to verify the validation contract without modifying the
// embedded data, and by SLICE-3 to validate family_aliases.json target keys.
//
// BDD: Given families.json with a typo key "claud" (should be "claude"),
// When FamiliesJSONKeyError is called, Then a non-nil error is returned
// mentioning the bad key.
func FamiliesJSONKeyError(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf(
			"bestiary parse: validate families JSON: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  How to fix: validate JSON syntax in the data",
			err,
		)
	}
	for key := range m {
		if key == "_comment" {
			continue
		}
		if !familyKeyKnown(key) {
			return fmt.Errorf(
				"bestiary parse: families JSON key %q is not a known family\n"+
					"  What: unrecognised family key\n"+
					"  Why: key does not match (case-insensitively) any value in allFamilies"+
					" (families_gen.go:182-357)\n"+
					"  How to fix: check for typos; valid keys must match allFamilies values"+
					" (e.g. \"claude\", \"gpt\", \"qwen\")",
				key,
			)
		}
	}
	return nil
}

// FamilyAliasesJSONError validates the SLICE-3 canonical-winner ledger bytes
// (family_aliases.json shape: {"_comment": ..., "aliases": {key: target, ...}}).
// Each alias TARGET (the canonical family value) must be a known Family
// (case-insensitive match against allFamilies). Alias KEYS are mislabels and are
// deliberately NOT validated — they need not be canonical. Returns nil when every
// target is valid, or an actionable error on the first unknown target.
//
// Reuses familyKeyKnown (SLICE-1) so the ledger and families.json share one
// fail-fast validation contract. Used by initParseData (load-time fail-fast) and
// by tests to verify the contract without mutating embedded data.
//
// BDD: Given a ledger row {"l3": "lluma"} (typo target), When FamilyAliasesJSONError
// is called, Then a non-nil error naming the bad target is returned.
func FamilyAliasesJSONError(data []byte) error {
	var m struct {
		Aliases map[string]string `json:"aliases"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf(
			"bestiary parse: validate family_aliases.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/family_aliases.json\n"+
				"  How to fix: validate JSON syntax; expected {\"aliases\": {key: target}}",
			err,
		)
	}
	for key, target := range m.Aliases {
		if !familyKeyKnown(target) {
			return fmt.Errorf(
				"bestiary parse: family_aliases.json target %q (for key %q) is not a known family\n"+
					"  What: unrecognised canonical-family TARGET in the SLICE-3 ledger\n"+
					"  Where: family_aliases.json alias %q → %q\n"+
					"  Why: the target does not match (case-insensitively) any value in allFamilies"+
					" (families_gen.go:182-357)\n"+
					"  How to fix: the alias VALUE must be a canonical family (e.g. \"llama\", \"qwen\");"+
					" check for typos. Alias KEYS may be arbitrary mislabels, but TARGETS must be canonical",
				target, key, key, target,
			)
		}
	}
	return nil
}

// remapFamilyAlias applies the SLICE-3 canonical-winner ledger to family: if a
// ledger row maps family (compared case-insensitively) to a canonical target, the
// target is returned; otherwise family is returned unchanged (DEFAULT own-family).
// Called at the SLICE-3 insertion point (after M4 normalisation, before
// bare-gen-split) in both parse entrypoints.
func remapFamilyAlias(pd *parseData, family Family) Family {
	if pd == nil || family == "" {
		return family
	}
	if target, ok := pd.familyAliases[Family(strings.ToLower(string(family)))]; ok {
		return target
	}
	return family
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
			// Convert hyphen-separated digit tokens to dot notation, stripping any
			// trailing date-shaped groups (SLICE-1-FIX-3).
			// RULE: dot-join only the LEADING semantic-version groups; stop (discard)
			// at the first date-shaped group. Date shapes: 4-digit (YYMM/MMDD),
			// 6-digit (YYMMDD), or a full MM-YYYY two-group remainder.
			// e.g. "4-5" → "4.5"; "4-0314" → "4"; "2603" → ""; "1-6-250615" → "1.6";
			//      "08-2024" (MM-YYYY) → "".
			version := dotJoinStrippingDateSuffix(variantStr)

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

	// Step 5: Bounded reorder — try to find a known-override prefix before pure passthrough.
	// R3a (e9pi): without this step, a string like "claude-opus-4-1-extra-stuff-zen" would
	// return the whole string as the family (passthrough), causing detectSuffixOverflow to
	// find 0 unaccounted tokens (all tokens are "consumed" by the family) and thus making
	// ReasonUnknownSuffixOverflow unreachable.
	//
	// Algorithm: scan hyphen-segmented prefixes of rawStr from longest to shortest. If a
	// prefix is found in the overrides table, use that override's (family, variant) as the
	// decomposition and attempt to extract a version from the remaining numeric tokens.
	// This ensures that "claude-opus-4-1-extra-stuff-zen" decomposes to (claude, opus, 4.1)
	// rather than returning the whole string as the family, making the overflow tokens
	// (extra, stuff, zen) visible to detectSuffixOverflow.
	rawTokens5 := strings.Split(rawStr, "-")
	for prefixLen := len(rawTokens5) - 1; prefixLen >= 1; prefixLen-- {
		candidate := strings.Join(rawTokens5[:prefixLen], "-")
		if ov, ok := pd.overrides[Family(candidate)]; ok {
			// Found an override prefix. The remaining tokens are the suffix.
			suffix := rawTokens5[prefixLen:]

			// Collect leading purely-numeric suffix tokens as version.
			// SLICE-1-FIX-4 (7kyb/9yyp): use isDateShapedToken (4-digit OR 6-digit YYMMDD)
			// to guard the 4th date-guard site. Previously isYYMMDateToken (4-digit only)
			// was used, leaving 6-digit YYMMDD tokens (e.g. "250615") unguarded here.
			var versionTokens []string
			for _, tok := range suffix {
				if isVersionToken(tok) && !isDateShapedToken(tok) {
					versionTokens = append(versionTokens, tok)
				} else {
					break
				}
			}

			version5 := strings.Join(versionTokens, ".")
			return ov.Family, ov.Variant, version5
		}
	}

	// Step 5 fallback: return raw unchanged, no version.
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
	// SLICE-8 (a): strip the vendor/path namespace so the SAME ID yields the same
	// version regardless of how the provider prefixes it (e.g. "openai/gpt-4.1" and
	// "gpt-4.1" must both match the "gpt-" family prefix). Without this the literal
	// HasPrefix check below fails on namespaced IDs and the version is silently
	// dropped on those providers only — a cross-provider version-presence divergence.
	// SLICE-8 (a): case-fold the prefix match so an uppercase ID (e.g. "GLM-5",
	// "MiniMax-M2") yields the SAME version as its lowercase sibling. The resolved
	// family is already lowercase (M4), so without folding the literal prefix check
	// fails on mixed-case IDs and the version drops on those providers only.
	idStr := strings.ToLower(stripVendorNamespace(string(id)))
	prefix := strings.ToLower(string(rawFamily)) + "-"
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
	// R3b (eq7w): reject a single YYMM token (e.g. "2603") to prevent Mistral-style
	// 4-digit date numerals from being returned as versions.
	// SLICE-1-FIX-3: also strip trailing date groups from multi-group remainders, and
	// detect the MM-YYYY two-group pattern (e.g. "08-2024", "03-2025") as a full date.
	if reHyphenDigits.MatchString(remainder) {
		if isYYMMDateToken(remainder) {
			return ""
		}
		// Detect MM-YYYY two-group remainder (e.g. "08-2024", "03-2025").
		if isMMYYYYTwoGroup(remainder) {
			return ""
		}
		// Strip trailing date groups and dot-join the leading semantic-version groups.
		return dotJoinStrippingDateSuffix(remainder)
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
		// SLICE-8 (b): param-size guard. Reject parameter-count / model-size tokens
		// (e.g. "120b" from gpt-oss-120b, "20b", "7m") so they are NEVER promoted to
		// Version. This makes gpt-oss-120b → Version "" on ALL providers (consistent);
		// the size INFO is GH#9 (missing Size dimension), not a version. Genuine
		// alphanumeric versions like "4o" are NOT matched (no b/m unit suffix).
		if isParamSizeToken(remainder) {
			return ""
		}
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

// ExtractModifier returns the modifier suffix found at the trailing end of id
// (after family and variant are resolved) and the literal substring of id that
// was consumed (including the leading hyphen).
//
// Resolution: after family + variant are known, the function scans the model ID
// for a trailing "-<modifier>" token where modifier is in the allowlist from
// parse/data/modifiers.json. Matching is longest-suffix-first so that "think"
// does not shadow "thinking" when both are seeded.
//
// Return values:
//   - modifier: the bare modifier token (e.g. "thinking"), or "" when none found.
//   - modifierConsumed: the substring removed from id (e.g. "-thinking"), or "" when none found.
//
// The caller should strip modifierConsumed from the model ID before passing to
// ExtractVersionFromID and ExtractDate, so that the modifier token does not
// pollute version/date heuristics.
//
// ExtractModifier does not modify ModelInfo or ModelRef fields — it is a pure
// function that returns values for the caller to wire. Pipeline order:
//
//  1. ParseFamily (raw → family + variant)
//  2. ExtractModifier (id, family, variant) → modifier, consumed
//  3. Strip consumed from id
//  4. ExtractVersionFromID on cleaned id
//  5. ExtractDate on cleaned id
func ExtractModifier(id ModelID, family Family, variant string) (modifier string, modifierConsumed string) {
	if id == "" {
		return "", ""
	}

	pd, err := loadParseData()
	if err != nil {
		// Fail closed: if embedded data cannot be loaded, return empty.
		// In a correct build this path is unreachable because the JSON files
		// are embedded at compile time.
		return "", ""
	}

	idStr := string(id)

	// Strip any leading path segment (e.g. "anthropic/claude-opus-4-6-thinking" → use last segment).
	if idx := strings.LastIndexByte(idStr, '/'); idx >= 0 {
		idStr = idStr[idx+1:]
	}
	// SLICE-10: match case-INSENSITIVELY (model IDs spell modifiers mixed-case, e.g.
	// "Llama-3.1-8B-Instruct", "Qwen-Turbo"). The bare modifier token returned is the
	// lowercase canonical form; the consumed suffix is the ACTUAL-case tail substring so
	// callers can strip it from the original ID by exact match.
	idLower := strings.ToLower(idStr)

	// Check modifiers longest-first.
	for _, mod := range pd.modifiers {
		suffix := "-" + mod
		if strings.HasSuffix(idLower, suffix) {
			// Variant-guard (SLICE-FIX-V2-5 Fix 3): if the trailing modifier token is
			// already encoded as the variant, do NOT double-count it. This prevents
			// models like kimi-k2-thinking (RawFamily="kimi-thinking" → variant="thinking")
			// from also getting Modifier="thinking". The variant is the authoritative
			// source for that token when they are the same.
			if mod == strings.ToLower(variant) {
				return "", ""
			}
			// SLICE-10 PER-FAMILY MEMBER-GUARD (ratified taxonomy, URD bestiary-o54x):
			// a token reclassifies variant→modifier ONLY IF it is NOT a curated member
			// of the resolved family. A member token is that family's product LINE (the
			// variant), never a modifier — e.g. deepseek-chat (chat ∈ deepseek.members),
			// sonar-reasoning (reasoning ∈ sonar.members), qwen-turbo (turbo ∈ qwen.members)
			// keep the token as the variant. This is what lets `instruct`/`reasoning`/etc.
			// be GLOBAL modifiers for non-member families while staying variants for the
			// families that genuinely productize them.
			if isFamilyMemberToken(pd, family, mod) {
				return "", ""
			}
			// Return the actual-case tail substring as consumed (same length as suffix).
			return mod, idStr[len(idStr)-len(suffix):]
		}
	}
	return "", ""
}

// isFamilyMemberToken reports whether tok is a curated member of family in
// families.json. Used by the SLICE-10 member-guard so a per-family product LINE
// (deepseek "chat", sonar "reasoning", qwen "turbo", llama "scout"/qwen "next")
// is never reclassified from variant to a global modifier. Case-insensitive on the
// family key; member tokens are stored lowercase.
func isFamilyMemberToken(pd *parseData, family Family, tok string) bool {
	if pd == nil || family == "" || tok == "" {
		return false
	}
	info, ok := pd.families[Family(strings.ToLower(string(family)))]
	if !ok {
		return false
	}
	lt := strings.ToLower(tok)
	for _, m := range info.Members {
		if m == lt {
			return true
		}
	}
	return false
}

// quantizationTokens is the curated set of quantization / serving-format tail tokens
// that are TRANSPARENT to modifier extraction: they are neither version, variant, nor
// modifier — they sit between a buried modifier and the tail (e.g.
// "llama-3.3-70b-instruct-fp8-fast" → fp8 sits between instruct and fast). The Size/
// Quantization dimension is a documented GH#9 residual, never a first-class field.
var quantizationTokens = map[string]struct{}{
	"fp8": {}, "fp16": {}, "fp4": {}, "bf16": {}, "int8": {}, "int4": {},
	"awq": {}, "gptq": {}, "gguf": {}, "tee": {}, "w8a8": {}, "w4a16": {},
	// SLICE-10: serving-platform / finetune tail words that sit AFTER a buried modifier
	// (e.g. "...-instruct-maas", "...-instruct-fp8-dynamic", "...-Instruct-abliterated").
	// They are neither version, variant, nor modifier — transparent to the modifier scan.
	"dynamic": {}, "maas": {}, "tput": {}, "abliterated": {}, "hf": {}, "raw": {},
	"tpu": {}, "dynamicfp8": {},
}

// reVersionMarkerToken matches a "v"-prefixed version-marker tail token (v0.1, v03,
// v1, v3.5) — SLICE-10 transparent to modifier scanning so a modifier buried before it
// (e.g. "mistral-7b-instruct-v0.1") is recovered. A BARE numeric ("1" in
// "grok-code-fast-1") is deliberately NOT matched, so it still stops the scan.
var reVersionMarkerToken = regexp.MustCompile(`^v[0-9]+(\.[0-9]+)?$`)

// isModifierTransparentToken reports whether a tail token should be SKIPPED (consumed
// but not collected) while scanning for buried modifiers: a date fragment, a param-size
// token, a context-window token, a quantization/serving-format token, or a 4+ digit
// pure-numeric date-like token (e.g. "2507", "0905"). These never carry modifier or
// (primary) version semantics, so scanning PAST them to recover a buried modifier
// (instruct/turbo/…) cannot drop real version/variant data.
func isModifierTransparentToken(tok string) bool {
	if tok == "" {
		return true
	}
	if isDateShapedToken(tok) || isParamSizeToken(tok) || reContextWindow.MatchString(tok) {
		return true
	}
	if _, ok := quantizationTokens[tok]; ok {
		return true
	}
	if reVersionMarkerToken.MatchString(tok) {
		return true // "v"-prefixed version marker (v0.1, v03) — not a bare digit
	}
	if len(tok) >= 4 && isAllDigits(tok) {
		return true // 4+ digit pure number → a date (YYMM/MMDD/YYYYMMDD), never a version
	}
	return false
}

// extractModifiers is the SLICE-10 multi-modifier companion to ExtractModifier. It
// scans the model ID's tail tokens (after the vendor/path strip, '@'→'-' normalized)
// from the END inward, collecting EVERY modifier token (member-guarded, case-insensitive)
// and SKIPPING transparent date/param/quant/context tokens, stopping at the first real
// boundary token (a variant/version/family token). This recovers modifiers buried behind
// quantization or date suffixes (e.g. "Llama-3.3-70B-Instruct-FP8-Fast" → [fast, instruct])
// that a strictly-trailing match would miss.
//
// Returns (mods, consumed): mods are lowercase canonical tokens in tail-inward PEEL order
// (the caller passes them through CanonicalizeModifiers); consumed is the full trailing
// substring spanned by the scan (modifiers + skipped transparent tokens), suitable for
// stripping from the ID before version/date logic. The member-guard and the variant-guard
// (token == resolved variant) both apply so a per-family product LINE token is never peeled.
func extractModifiers(id ModelID, family Family, variant string) (mods []string, consumed string) {
	pd, err := loadParseData()
	if err != nil {
		return nil, ""
	}
	idStr := string(id)
	if idx := strings.LastIndexByte(idStr, '/'); idx >= 0 {
		idStr = idStr[idx+1:]
	}
	idStr = strings.ReplaceAll(idStr, "@", "-")
	toks := strings.Split(idStr, "-")
	lowVariant := strings.ToLower(variant)

	consumedTokens := 0 // number of trailing tokens consumed (modifiers + transparent)
	prevAfterYear := false
	for i := len(toks) - 1; i >= 0; i-- {
		t := strings.ToLower(toks[i])
		if j := strings.IndexByte(t, ':'); j >= 0 {
			t = t[:j] // strip ":N" context-window tag for the test
		}
		// MM-of-MM-YYYY date fragment: a 1-2 digit numeric immediately PRECEDING (in
		// tail order) a 4-digit year is the month half of an "MM-YYYY" date — transparent
		// (e.g. "08" in "command-a-reasoning-08-2025"). Lets the scan reach a buried modifier.
		isMonthFragment := prevAfterYear && len(t) <= 2 && isAllDigits(t)
		switch {
		case isKnownModifierToken(pd, t) && t != lowVariant && !isFamilyMemberToken(pd, family, t):
			mods = append(mods, t)
			consumedTokens = len(toks) - i
			prevAfterYear = false
		case isMonthFragment || isModifierTransparentToken(t):
			// skip but keep scanning inward; consumedTokens only advances on a modifier hit.
			prevAfterYear = len(t) == 4 && isAllDigits(t)
			continue
		default:
			// real boundary token (variant/version/family) → stop.
			i = -1
		}
	}
	if consumedTokens > 0 {
		// Rebuild the consumed trailing substring (covers the skipped transparent tokens
		// interleaved before the innermost collected modifier).
		consumed = "-" + strings.Join(toks[len(toks)-consumedTokens:], "-")
	}
	return mods, consumed
}

// promoteVariantModifier enforces the SLICE-10 taxonomy invariant on the VARIANT slot:
// a global modifier token must NEVER occupy the primary-variant slot. If the resolved
// variant is a known modifier token AND is NOT a curated member of the resolved family
// (member-guard), it is moved into the modifier LIST and the variant is cleared. This
// catches residual cases where an empty-raw / unregistered-family inference left a
// modifier (e.g. "instruct") in the variant slot. Member product-LINE tokens
// (deepseek "chat", sonar "reasoning", qwen "turbo") are protected by the member-guard
// and stay variants.
func promoteVariantModifier(pd *parseData, family Family, variant string, mods []string) (string, []string) {
	if pd == nil || variant == "" {
		return variant, mods
	}
	lv := strings.ToLower(variant)
	if isKnownModifierToken(pd, lv) && !isFamilyMemberToken(pd, family, variant) {
		return "", CanonicalizeModifiers(append(append([]string{}, mods...), lv))
	}
	return variant, mods
}

// InferFamilyFromIDWithVariant is the extended empty-family fallback for models
// whose API family field is empty (~25% of models). Unlike InferFamilyFromID,
// it extracts (Family, Variant, Version) by:
//  1. Attempting the Δ2′ modifier-strip path (R3c): tentatively strip a trailing
//     modifier to expose a hidden date, then decompose and apply two commit guards.
//  2. Existing flow: strip trailing date, feed ID to ParseFamilyWithVersion, then
//     fall back to first-token-only if no decomposition found.
//
// This ensures (Family, Variant, Version) is consistent across providers
// regardless of whether raw_family is empty or populated.
//
// Examples:
//
//	InferFamilyFromIDWithVariant("claude-opus-4-5-20251101", "nano-gpt") → ("claude", "opus", "4.5")
//	InferFamilyFromIDWithVariant("claude-opus-4-6", "some-provider")    → ("claude", "opus", "4.6")
//	InferFamilyFromIDWithVariant("gpt-4o", "openai")                    → ("gpt", "", "4o")
//	InferFamilyFromIDWithVariant("claude-opus-4-1-20250805-thinking", "302ai") → ("claude", "opus", "4.1")
//
// The provider parameter is reserved for future provider-specific heuristics
// and is not currently used.
//
// SLICE-8 (d): this is a thin wrapper that applies the letter-prefix series split
// (CLARIFICATION-5) to the inner inference result, so the empty-raw primitive is
// SELF-CONSISTENT with the canonical ParseFamilyDetailed path (kimi-k2 → (kimi,k,2),
// minimax-m1 → (minimax,m,1), and the compound-family recovery kimi-k2-0905 → kimi).
func InferFamilyFromIDWithVariant(id ModelID, p Provider) (Family, string, string) {
	family, variant, version := inferFamilyFromIDWithVariantBase(id, p)
	if pd, pdErr := loadParseData(); pdErr == nil {
		if base, ok := seriesBaseFamily(pd, family); ok {
			family = base
		}
		// tierMod is ignored here: InferFamilyFromIDWithVariant has no Modifier slot;
		// the tier→Modifier promotion (CLARIFICATION-6) is applied by ParseFamilyDetailed.
		if sv, svv, _, ok := splitSeriesVariant(pd, family, string(id)); ok {
			variant, version = sv, svv
		}
	}
	return family, variant, version
}

// inferFamilyFromIDWithVariantBase is the inner empty-family inference (pre
// series-split). It owns the M3/M4 + ledger + bare-gen + member-recovery flow; the
// SLICE-8 (d) series override is applied by the InferFamilyFromIDWithVariant wrapper.
func inferFamilyFromIDWithVariantBase(id ModelID, p Provider) (Family, string, string) {
	if id == "" {
		return "", "", ""
	}

	// ── M3: vendor/namespace strip (shared head) ─────────────────────────────
	// stripVendorNamespace strips the "<org>/" path segment then any residual
	// "<vendor_alias>-" / "<vendor_alias>/" prefix (e.g. "minimaxai-minimax-m1" →
	// "minimax-m1"). The SAME helper is called by ParseFamilyDetailed (jvpa symmetry).
	idStr := stripVendorNamespace(string(id))

	// R3c (Δ2′): a trailing modifier (e.g. "-thinking") can hide a trailing date
	// ("-20250805-thinking"), blocking stripTrailingDate and corrupting decomposition.
	// Algorithm: tentatively strip modifier → expose date → stripTrailingDate → provisional
	// decompose → GUARD-1 (variant-guard: ExtractModifier returns non-empty consumed) +
	// GUARD-2 (passthrough-guard: fProv != Family(cleaned)) → commit.
	if exposed := trimOneTrailingModifier(idStr); exposed != idStr {
		// A trailing pd.modifiers token was present; now the date (if any) is exposed.
		cleaned := orSelf(stripTrailingDate(exposed), exposed)
		fProv, vProv, verProv := ParseFamilyWithVersion(Family(cleaned))

		// GUARD-1 (variant-guard): ExtractModifier(id, fProv, vProv) must return a
		// non-empty consumed string. This confirms the trailing modifier token is a
		// genuine modifier (not the same as the provisional variant), preventing
		// over-stripping of real variants like "thinking" in "kimi-k2-thinking".
		_, consumed := ExtractModifier(id, fProv, vProv)

		// GUARD-2 (passthrough-guard): fProv must differ from the cleaned string.
		// If they are equal, ParseFamilyWithVersion returned a pure passthrough (no
		// decomposition found), meaning the strip was not semantically meaningful.
		if consumed != "" && fProv != Family(cleaned) {
			version := verProv
			if version == "" && fProv != "" {
				version = ExtractVersionFromID(ModelID(cleaned), fProv)
			}
			if version == "" && vProv != "" {
				if v, _ := ExtractVersionBetweenFamilyAndVariant(ModelID(cleaned), fProv, vProv); v != "" {
					version = v
				}
			}
			// M4: lowercase Family field at the output boundary.
			outFamily := Family(strings.ToLower(string(fProv)))
			// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before
			// bare-gen-split). This modifier-stripping branch returns early, so the
			// ledger must be applied here too for symmetry. DEFAULT own-family → no-op.
			if pd, pdErr := loadParseData(); pdErr == nil {
				outFamily = remapFamilyAlias(pd, outFamily)
			}
			// SLICE-2 bare_gen_split: this modifier-stripping branch returns early
			// (before the pipeline insertion point below), so apply the split here too
			// — e.g. "gemini-3-flash-preview" (preview stripped) leaves family
			// "gemini-3" which must split to (gemini, …, 3). Closed predicate.
			outVariant := vProv
			if pd, pdErr := loadParseData(); pdErr == nil {
				if base, ver, ok := splitBareGen(pd, string(outFamily)); ok {
					outFamily = base
					if version == "" {
						version = ver
					}
				}
				// SLICE-11 (rc2) Option B: reduce an OVER-CAPTURED compound family here too
				// — this modifier-strip branch returns early (bypassing the main-path
				// reduction), so IDs carrying a trailing modifier (e.g.
				// gemini-3.1-flash-image-preview, gpt-4o-mini-search-preview) would otherwise
				// keep the compound family. Re-derive variant/version against the short base,
				// mirroring the main path, so they converge with the raw-populated providers.
				if short, vhint, ok := reduceOverCapturedFamily(pd, outFamily); ok {
					outFamily = short
					outVariant = vhint
					version = ""
					lowID := strings.ToLower(stripVendorNamespace(string(cleaned)))
					leadTok := lastPathSegment(lowID)
					if i := strings.IndexByte(leadTok, '-'); i >= 0 {
						leadTok = leadTok[:i]
					}
					if b, ver, okb := splitBareGen(pd, leadTok); okb && b == outFamily {
						version = ver
					}
					if version == "" && outVariant != "" {
						if v, _ := ExtractVersionBetweenFamilyAndVariant(ModelID(orSelf(stripTrailingDate(lowID), lowID)), outFamily, outVariant); v != "" {
							version = v
						}
					}
					if outVariant == "" {
						normFamPrefix := strings.ToLower(firstToken(string(outFamily))) + "-"
						zone := lowID
						if strings.HasPrefix(zone, normFamPrefix) {
							zone = zone[len(normFamPrefix):]
						}
						outVariant = recoverMemberVariant(strings.Split(zone, "-"), outFamily)
					}
				}
			}
			return outFamily, outVariant, version // committed: modifier handled
		}
	}

	// ── Existing flow (no hidden modifier, or commit declined by either guard) ──

	// Strip trailing date tokens so they don't contaminate family inference.
	stripped := orSelf(stripTrailingDate(idStr), idStr)

	tokens := strings.Split(stripped, "-")
	if len(tokens) == 0 {
		return "", "", ""
	}
	// Take only the first alphabetic-leading token as the family seed.
	first := tokens[0]
	if first == "" || !unicode.IsLetter(rune(first[0])) {
		return "", "", ""
	}

	// Build the candidate family string and run ParseFamilyWithVersion on it.
	candidateFamilyStr := stripped

	family, variant, version := ParseFamilyWithVersion(Family(candidateFamilyStr))

	// ── Pipeline skeleton ──────────────────────────────────────────────────
	// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before bare-gen-split)
	// SLICE-2 INSERTION POINT: bare_gen_split closed predicate (before recoverMemberVariant)

	// If ParseFamilyWithVersion returns the raw string unchanged (no pattern matched),
	// it means the entire string is treated as a family with no variant or version.
	// SLICE-1 replacement of the empty-raw amputation: take firstToken as family, then
	// recoverMemberVariant on remaining tokens to find variant from families.json members.
	if family == Family(candidateFamilyStr) {
		baseFamily := Family(strings.ToLower(firstToken(stripped)))
		// Remaining tokens after the first (family) token are candidate member zones.
		remainingTokens := tokens[1:]
		// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before
		// bare-gen-split). Folds an inferred mislabel/shorthand seed to its canonical
		// family (e.g. "l3.1" → "llama" for community Llama-3 finetunes) so the family
		// agrees cross-provider; recoverMemberVariant then runs against the canonical
		// family's member list. DEFAULT own-family → no-op.
		if pd, pdErr := loadParseData(); pdErr == nil {
			baseFamily = remapFamilyAlias(pd, baseFamily)
		}
		// SLICE-2 bare_gen_split: when the family seed is a glued <base><int> token
		// (e.g. "qwen3", "o1"), split it so the generation surfaces as version. Gated
		// on the closed predicate (has-entry ∧ not-digit-suffixed ∧ bare_gen_split).
		bareVersion := ""
		if pd, pdErr := loadParseData(); pdErr == nil {
			if base, ver, ok := splitBareGen(pd, string(baseFamily)); ok {
				baseFamily = base
				bareVersion = ver
			}
		}
		recoveredVariant := recoverMemberVariant(remainingTokens, baseFamily)
		// M4: baseFamily is already lowercased above.
		return baseFamily, recoveredVariant, bareVersion
	}

	// M4: lowercase Family field at the output boundary.
	family = Family(strings.ToLower(string(family)))

	// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before bare-gen-split).
	// DEFAULT own-family → no-op; a ledger row remaps a mislabel to its canonical family.
	if pd, pdErr := loadParseData(); pdErr == nil {
		family = remapFamilyAlias(pd, family)
	}

	// SLICE-2 INSERTION POINT: bare_gen_split closed predicate (before recoverMemberVariant)
	// Split a glued/hyphenated <base><int> family token (e.g. "qwen3", "gpt-5") into
	// its base family + generation version. Closed predicate (splitBareGen). Only
	// fills an empty version — never clobbers a version ParseFamilyWithVersion found.
	if pd, pdErr := loadParseData(); pdErr == nil {
		if base, ver, ok := splitBareGen(pd, string(family)); ok {
			family = base
			if version == "" {
				version = ver
			}
		}
	}

	// SLICE-11 (rc2) Option B: reduce an OVER-CAPTURED compound family to its registered
	// SHORT base BEFORE member/version recovery, so the recovery below runs against the
	// short family and reproduces the SAME (variant, version) the raw-populated providers
	// derive (e.g. empty-raw "qwen3-vl-30b-a3b-instruct" → variant "vl"+version "3", NOT
	// suffix-variant "instruct"). variant/version captured against the COMPOUND family are
	// reset so they re-derive against the short base; the variant hint is used only as a
	// last-resort fallback when member recovery finds nothing.
	if pd, pdErr := loadParseData(); pdErr == nil {
		if short, vhint, ok := reduceOverCapturedFamily(pd, family); ok {
			// Preserve a VERSION-SHAPED pre-reset variant (e.g. "v3.1" for deepseek-chat-v3.1,
			// where ParseFamilyWithVersion's v-prefix pattern put the version in the variant
			// slot of the over-captured family). Reducing the family must NOT drop that real
			// version — recover it below if the post-reduction re-derivation finds none.
			origVariantVer := ""
			if isVersionShaped(variant) {
				origVariantVer = strings.TrimLeft(strings.ToLower(variant), "abcdefghijklmnopqrstuvwxyz")
			}
			family = short
			variant = vhint
			version = ""
			// Re-derive the generation version against the SHORT family directly from the
			// ID — mirroring the raw-populated providers' recovery — so the empty-raw record
			// converges FULLY (family AND version), not just on family. Two steps mirror
			// ParseFamilyDetailed's raw path: (1) bare-gen split of the leading ID token
			// (qwen2.5→2.5, qwen3→3); (2) version between the family and the recovered
			// member variant (phi-4-mini → 4, mistral-small-3.1 → 3.1). The generic
			// extractors below remain as a final fallback.
			lowID := strings.ToLower(stripVendorNamespace(string(id)))
			leadTok := lastPathSegment(lowID)
			if i := strings.IndexByte(leadTok, '-'); i >= 0 {
				leadTok = leadTok[:i]
			}
			if b, ver, okb := splitBareGen(pd, leadTok); okb && b == family {
				version = ver
			}
			if version == "" && variant != "" {
				if v, _ := ExtractVersionBetweenFamilyAndVariant(ModelID(orSelf(stripTrailingDate(lowID), lowID)), family, variant); v != "" {
					version = v
				}
			}
			// Final fallback: the version that the v-prefix pattern stashed in the variant
			// slot of the compound family (deepseek-chat-v3.1 → "3.1"), recovered only if no
			// generation version was derived above, so a reduction never drops a real version.
			if version == "" && origVariantVer != "" {
				version = origVariantVer
			}
		}
	}

	// recoverMemberVariant: if variant is still empty after ParseFamilyWithVersion,
	// attempt to recover it from the model ID tokens (families.json members + suffixes).
	if variant == "" {
		normFamPrefix := strings.ToLower(firstToken(string(family))) + "-"
		lowStripped := strings.ToLower(stripped)
		memberZone := lowStripped
		if strings.HasPrefix(memberZone, normFamPrefix) {
			memberZone = memberZone[len(normFamPrefix):]
		}
		variant = recoverMemberVariant(strings.Split(memberZone, "-"), family)
	}

	// If version is still empty, try ExtractVersionFromID.
	if version == "" && family != "" {
		version = ExtractVersionFromID(id, family)
	}
	// Final fallback: try the between-family-and-variant extractor.
	if version == "" && variant != "" {
		if v, _ := ExtractVersionBetweenFamilyAndVariant(ModelID(stripped), family, variant); v != "" {
			version = v
		}
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

// isVersionToken returns true when tok is a purely-numeric token (all digits)
// AND is not a YYMM-date token (as detected by isYYMMDateToken).
// Used to strip trailing version components from model IDs.
//
// R3b (eq7w): YYMM tokens (e.g. "2603", "2512") are rejected so that
// mistral-small-2603 → no version.
func isVersionToken(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	// R3b guard: reject YYMM date tokens (e.g. 2603, 2512, 2411).
	return !isYYMMDateToken(tok)
}

// reYYYYMMDD matches an 8-digit date string not preceded or followed by a digit.
// Captured as YYYY, MM, DD in groups 1, 2, 3.
var reYYYYMMDD = regexp.MustCompile(`(?:^|[^0-9])(\d{4})(\d{2})(\d{2})(?:$|[^0-9])`)

// reYYYYDashMMDashDD matches YYYY-MM-DD date strings.
var reYYYYDashMMDashDD = regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`)

// --------------------------------------------------------------------------
// Parse-failure audit types (SLICE-FIX-V2-3)
// --------------------------------------------------------------------------

// ParseAttempt records the partial result produced when parse heuristics
// could not fully decompose a raw family string. Fields mirror ModelInfo
// canonical fields (Family, Variant, Version, Date) to aid comparison.
type ParseAttempt struct {
	Family  Family `json:"family"`
	Variant string `json:"variant"`
	Version string `json:"version"`
	Date    string `json:"date"`
}

// ParseFailureReason is a typed string identifying the class of parse failure.
// Using a named type rather than bare string prevents accidental mixing of
// reason strings and enables exhaustive case analysis.
type ParseFailureReason string

// ParseFailure records a single parsing failure detected during family-string
// decomposition. It is produced by ParseFamilyDetailed when the parser's
// best-effort result is known to be incomplete or ambiguous.
//
// JSON field names match the locked per-record format from SLICE-FIX-V2-3:
//
//	{
//	  "raw_id":         "claude-3-5-haiku-20241022",
//	  "provider":       "anthropic",
//	  "raw_family":     "claude-haiku",
//	  "attempted_parse": {"family":"claude","variant":"haiku","version":"","date":"2024-10-22"},
//	  "reason":         "version digits between family-prefix and variant not extracted"
//	}
type ParseFailure struct {
	RawID          ModelID            `json:"raw_id"`
	Provider       Provider           `json:"provider"`
	RawFamily      Family             `json:"raw_family"`
	AttemptedParse ParseAttempt       `json:"attempted_parse"`
	Reason         ParseFailureReason `json:"reason"`
}

// ParseFailuresEnvelope is the top-level JSON structure written by bestiary-gen
// to .bestiary-gen-cache/parse_failures.json after each codegen run.
// The file is overwritten on every run (full audit, not append).
type ParseFailuresEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	FailureCount  int            `json:"failure_count"`
	Failures      []ParseFailure `json:"failures"`
}

// Reason constants for the known failure modes (use these constants verbatim
// to ensure consistent reason phrasing across all callers).
const (
	// ReasonVersionDigitsNotExtracted is used when version digits appear between
	// the family prefix and the variant (e.g. "claude-3-5-haiku-20241022" where
	// the "3-5" version component is not extracted by the parse heuristics).
	ReasonVersionDigitsNotExtracted ParseFailureReason = "version digits between family-prefix and variant not extracted"

	// ReasonKnownSuffixOverflow is used when the trailing segment of the model ID
	// matches a known modifier token (thinking, vision, latest, code, preview, think).
	// The modifier is semantically meaningful but not yet extracted as a first-class
	// field. Extend the modifier allowlist in parse/data/modifiers.json when new
	// tokens are discovered.
	ReasonKnownSuffixOverflow ParseFailureReason = "suffix overflow: trailing token is a known modifier"

	// ReasonUnknownSuffixOverflow is used when the trailing segment of the model ID
	// does not match any known modifier token. This is an audit-log hint that the
	// modifier allowlist in parse/data/modifiers.json should be extended.
	ReasonUnknownSuffixOverflow ParseFailureReason = "suffix overflow: trailing token is an unknown modifier (extend allowlist)"

	// ReasonYYMMDateAsVersion is used for Mistral-style 4-digit numerals (e.g. 2401,
	// 2403) where the parser cannot reliably distinguish a YYMM date from a version.
	ReasonYYMMDateAsVersion ParseFailureReason = "YYMM-date-as-version false-positive"

	// ReasonResidualUnaccountedTokens is used when version extraction succeeds but
	// leaves one or more tokens in the model ID unaccounted for (e.g. nova-2-lite-v1
	// yields version="2" but token "v1" is not explained by family/variant/version/date).
	// This is an honest-audit signal: the version is populated but there is residual
	// information the parser did not fully account for.
	ReasonResidualUnaccountedTokens ParseFailureReason = "residual unaccounted tokens after extraction"
)

// idDrivenDecompose is the SINGLE ID-driven decomposition primitive (SLICE-9
// path-unification). It derives (Family, Variant, Version, Modifier) purely from
// the model ID via the S1-S8 pipeline (InferFamilyFromIDWithVariant → ExtractModifier
// → idDrivenVersion → tier→Modifier), with NO reference to raw_family. It is the
// body the raw=="" branch of ParseFamilyDetailed used to inline; extracting it lets
// the raw!="" branch consult the IDENTICAL decomposition so the two paths can never
// disagree on the fields the ID owns.
//
// SLICE-9 (rc2) re-scope (Option A, authorized): this primitive drives Variant/
// Version/Modifier in the unified path. It deliberately does NOT own Family for the
// raw-populated path — the diff-first safeguard proved the ID-path OVER-captures the
// Family for the common <family>-<gen><size>-<variant> ID shape (deepseek-v4-flash →
// "deepseek-v4", gpt-4o-mini → "gpt-4o"), where raw_family carries the correct SHORT
// family. Converging those 107 family-over-capture divergences to the short family is
// the dedicated family-seeding slice (Option B), not this one.
func idDrivenDecompose(id ModelID, p Provider) (Family, string, string, []string) {
	// SLICE-9 fix-cycle-2 (P2, Reviewer C IMPORTANT): some providers separate the
	// version/date tag with '@' instead of '-' (e.g. "claude-opus-4-1@20250805",
	// "claude-sonnet-4-6@default"). The '@' defeats every hyphen-based extractor
	// (InferFamilyFromIDWithVariant, idDrivenVersion, splitSeriesVariant), leaving
	// version "4" instead of the canonical "4.1"/"4.6" — a NEW same-model cross-form
	// version-VALUE divergence (the epic's exact target, newly introduced). Normalize
	// '@'→'-' ONCE here, at the head of the ID-driven primitive, so the @-form CONVERGES
	// to the canonical hyphen-form decomposition. Date extraction (ExtractDate, codegen)
	// runs on the ORIGINAL id and matches the embedded 8-digit date regardless of the
	// '@' delimiter, so Date is unaffected.
	id = ModelID(strings.ReplaceAll(string(id), "@", "-"))
	family, variant, version := InferFamilyFromIDWithVariant(id, p)
	// SLICE-10: peel ALL trailing modifiers (member-guarded) so multi-modifier IDs
	// (kimi-k2-thinking-turbo → [thinking, turbo]) are captured losslessly.
	modifiers, _ := extractModifiers(id, family, variant)
	// ID-driven version consistency (SLICE-8 a/c): recover a version the ID carries
	// but ParseFamilyWithVersion's passthrough missed; also the glued letter-suffix
	// version-modifier (glm-4.5v → 4.5 + vision).
	if version == "" && family != "" {
		if v, gmod := idDrivenVersion(id, family, variant); v != "" {
			version = v
			if gmod != "" {
				modifiers = append(modifiers, gmod)
			}
		}
	}
	// Tier→Modifier promotion (CLARIFICATION-6) for series families. The single-tier
	// promotion still applies; with the Modifier LIST it composes atop any capability
	// modifier (CanonicalizeModifiers dedups against tiers already peeled above).
	if pd, pdErr := loadParseData(); pdErr == nil {
		if _, _, tierMod, ok := splitSeriesVariant(pd, family, string(id)); ok && tierMod != "" {
			modifiers = append(modifiers, tierMod)
		}
		// SLICE-10: a global modifier left in the variant slot by the empty-family
		// inference (e.g. "instruct") is moved to the modifier list here, BEFORE the
		// raw/ID reconcile, so it cannot override a genuine raw variant (e.g. "oss").
		variant, modifiers = promoteVariantModifier(pd, family, variant, modifiers)
	}
	return family, variant, version, CanonicalizeModifiers(modifiers)
}

// reconcileIDDriven is the SLICE-9 (rc2) Option A unification step. Given the
// raw-aware decomposition (the family-preserving base + fallback) and the model ID,
// it produces the unified canonical (Family, Variant, Version, Modifier):
//
//   - FAMILY is PRESERVED from the raw-aware path. raw_family is the reliable short
//     family; the ID-driven path over-captures it (see idDrivenDecompose). Family is
//     NEVER changed here → ZERO family regression by construction.
//   - VARIANT / VERSION / MODIFIER are taken from the ID-driven decomposition, but
//     ONLY when the ID-driven path resolved the SAME family (idFam == rawFam). When
//     they agree, the ID owns the field split and every provider of that ID (raw or
//     empty) converges on the identical value — this is the divergence-fixing win for
//     the field-only divergent IDs (glm-5v → (glm,"",5,vision); version-presence).
//   - An ID-driven field is adopted only when NON-EMPTY: a populated raw field is
//     never CLEARED by an empty ID-driven field (monotonic; preserves the rawModifier
//     fallback such as raw="deepseek-thinking", id="deepseek-reasoner" → modifier
//     "thinking", and avoids dropping a raw variant the ID does not re-derive).
//   - When the families DISAGREE (the over-capture / ledger class), the raw-aware
//     result is kept verbatim — that convergence belongs to the family-seeding slice.
func reconcileIDDriven(rawFam Family, rawVar, rawVer string, rawMod []string, id ModelID, p Provider) (Family, string, string, []string) {
	// Family-preserving: the family is always the raw-aware family.
	family, variant, version, modifier := rawFam, rawVar, rawVer, rawMod

	if id == "" {
		return family, variant, version, modifier
	}

	idFam, idVar, idVer, idMod := idDrivenDecompose(id, p)
	if idFam != rawFam {
		// SLICE-12 CANONICAL-WINNER ENFORCE: when the ID-driven family is a DISTINCT
		// family in the closed enforce set (family_enforce.json) and raw_family disagrees,
		// the ID is authoritative — raw is either a parent-family mislabel (aion/magnum
		// tagged "llama", mixtral/pixtral/voxtral tagged "mistral", intellect tagged "glm",
		// qwq tagged "qwen", wizardlm/inflection tagged "gpt", weaver/owl tagged "alpha")
		// or an org/namespace leak (raw is the ID's org: nousresearch→hermes, allenai→olmo,
		// liquid→lfm). Adopt the ID-driven decomposition wholesale so the mislabelling
		// provider converges onto the ID-derived canonical family + variant/version. The
		// enforce set is CLOSED (curated), and this only fires when the ID's OWN family
		// resolves to one of those distinct names — never a blanket family rewrite.
		if pd, pdErr := loadParseData(); pdErr == nil && idFam != "" {
			if _, enforced := pd.enforceFamilies[Family(strings.ToLower(string(idFam)))]; enforced {
				// Monotonic adoption: take the ID-driven family, but FALL BACK to the
				// raw-aware Variant/Version/Modifier when the ID-driven field is empty, so
				// a populated raw field is never CLEARED (e.g. qwq-plus: idDriven yields no
				// variant for the unregistered family "qwq", so keep raw variant "plus").
				v, ver, mod := idVar, idVer, idMod
				if v == "" {
					v = rawVar
				}
				if ver == "" {
					ver = rawVer
				}
				// SLICE-10: union raw + ID modifiers (lossless); empty→nil.
				mod = CanonicalizeModifiers(append(append([]string{}, mod...), rawMod...))
				return idFam, v, ver, mod
			}
		}
		// SLICE-12 (#2): a RAW-POPULATED OVER-CAPTURE family (raw_family is itself a
		// compound that REDUCES to the same short base the ID-path derived — e.g.
		// raw="qwen3.7-max" → qwen) is a provider over-capture, the mirror of the empty-raw
		// case S11 already reduces. When reduceOverCapturedFamily(rawFam) lands on idFam,
		// adopt the ID-driven decomposition so the over-capturing provider converges with its
		// short-family siblings. CLOSED: declines (and keeps raw) when rawFam carries an
		// unrecognised token (qwen3-next-80b-a3b — "next" blocks the reduction; honest residual).
		// Guard: only adopt when raw carries NO populated variant of its own (rawVar==""),
		// so this never SWAPS a real raw variant for a different ID-driven one (e.g.
		// deepseek-flash-free's access-tag "free" must not be laterally replaced). qwen3.7-max
		// (rawVar="") is the intended target; the swap cases keep the raw result.
		if rawVar == "" {
			if pd, pdErr := loadParseData(); pdErr == nil {
				if short, _, ok := reduceOverCapturedFamily(pd, rawFam); ok && short == idFam {
					ver, mod := idVer, idMod
					if ver == "" {
						ver = rawVer
					}
					mod = CanonicalizeModifiers(append(append([]string{}, mod...), rawMod...))
					return idFam, idVar, ver, mod
				}
			}
		}
		// SLICE-14 (TIER-1 conditional): raw_family "text-embedding" is a GENERIC descriptor
		// (a self-map override), not a product family. When the ID itself names a real registered
		// family (Qwen/Qwen3-Embedding-8B → ID-family "qwen"), that family WINS. GUARD: OpenAI's
		// own "text-embedding-3-large/small" — whose ID literally IS "text-embedding" — derives
		// idFam=="text-embedding", so idFam==rawFam there and this branch never fires (no collateral).
		if strings.EqualFold(string(rawFam), "text-embedding") && idFam != "" &&
			!strings.EqualFold(string(idFam), "text-embedding") && familyKeyKnown(string(idFam)) {
			return idFam, idVar, idVer, CanonicalizeModifiers(append(append([]string{}, idMod...), rawMod...))
		}
		// Family disagreement (ID-path over-capture or a genuine ledger mislabel):
		// keep the raw-aware result verbatim. Converging these is Option B's job.
		return family, variant, version, modifier
	}

	// Families agree → the ID owns the field decomposition. Adopt each non-empty
	// ID-driven field, but conservatively so a currently-correct decomposition is
	// never WORSENED (the CLARIFICATION-8 zero-regression gate):
	//
	//  - VARIANT: fill an empty variant freely (enrichment + convergence with the
	//    empty-raw providers). Only OVERRIDE a populated raw variant when the ID-driven
	//    variant is a CLEAN token — no '.' and no digit — so we never replace a clean
	//    raw variant (e.g. "pro") with a version/quant-polluted ID token (e.g.
	//    "v2.5-pro-6bit" for mimo-v2.5-pro-6bit, where the series split was defeated by
	//    the "6bit" quantization suffix). De-junking (raw variant "3.6" → "flash") and
	//    refinement (raw "codex" → "codex-mini") both pass this guard.
	//
	//    SLICE-9 fix-cycle-2 (P1, Reviewer A BLOCKER): additionally REJECT the override
	//    when the populated raw variant is a more-specific SUPERSTRING of idVar — i.e.
	//    the ID-driven variant is LESS specific (a prefix). This prevents the
	//    gemini-2.5-flash-lite-preview-* regression where InferFamilyFromIDWithVariant
	//    loses the "-lite" tier and returns "flash", which would otherwise overwrite the
	//    correct raw variant "flash-lite" (conflating two distinct Gemini tiers). The
	//    empty-raw providers of those IDs still mis-derive "flash" — that is a REAL
	//    pre-existing ID-path residual surfaced for a future family/tier-seeding fix,
	//    NOT hidden by downgrading the correct raw data.
	if idVar != "" && (variant == "" || (isCleanVariantToken(idVar) && !strings.HasPrefix(variant, idVar))) {
		variant = idVar
	}
	//    SLICE-12 (#4) gpt-codex ID-WINS-on-variant-conflict: raw_family "gpt-codex"
	//    (override → variant "codex"/"codex-mini"/"codex-spark") MISLABELS the
	//    gpt-5/5.1/5.2-chat[-latest] chat models, whose IDs contain NO "codex". When the
	//    codex variant is ABSENT from the ID it is a provider phantom — drop it in favor of
	//    the ID-driven variant (idVar, possibly empty). SCOPED to the codex family on
	//    purpose: a blanket "raw variant absent from ID → clear" wrongly drops legitimate
	//    variants whose ID spells them differently (kimi-k2.6 vs "Kimi-K2_6") or names a
	//    different one (raw "kat-coder" vs ID "KAT-Dev"). The S9 flash-lite superstring win
	//    is untouched (flash-lite ∉ codex family AND is present in its ID).
	if strings.HasPrefix(variant, "codex") &&
		!strings.Contains(strings.ToLower(stripVendorNamespace(string(id))), "codex") {
		variant = idVar
	}
	//  - VERSION: fill an empty version, or override with a NUMERIC/dotted ID-driven
	//    version (versions are low-ambiguity). The override path is guarded by
	//    isVersionShaped so a populated version is only ever replaced by another
	//    version-shaped value, never by junk.
	if idVer != "" && (version == "" || isVersionShaped(idVer)) {
		version = idVer
	}
	//  - MODIFIER: SLICE-10 — UNION the raw and ID modifier sets (lossless). The
	//    Modifier field is now a LIST, so a capability modifier carried by raw_family
	//    (e.g. "thinking") and a tier/capability the ID carries (e.g. "turbo") COMPOSE
	//    rather than one silently shadowing the other (kimi-k2p6-turbo raw="kimi-thinking"
	//    → [thinking, turbo]). CanonicalizeModifiers dedups + orders.
	modifier = CanonicalizeModifiers(append(append([]string{}, modifier...), idMod...))
	// SLICE-12 (#3) dotted bare-gen de-junk: a raw variant that is a PURE dotted/numeric
	// generation token (e.g. "3.5"/"3.6" from raw_family "qwen3.5"/"qwen3.6") is the family
	// GENERATION leaked into the variant slot by the version_patterns "no-prefix" rule. Move
	// it to the (empty) version and clear the variant, so the raw-populated provider converges
	// with the empty-raw providers (which carry version 3.5, variant ""). A pure numeric/dotted
	// token is never a legitimate variant (real variants are words or single series letters),
	// so this cannot drop a real variant.
	if isPureDottedGen(variant) {
		if version == "" {
			version = variant
		}
		variant = ""
	}
	return family, variant, version, modifier
}

// isPureDottedGen reports whether s is a pure numeric generation token — digits with an
// optional single dotted minor (e.g. "3", "3.5", "3.6") and nothing else (no letters, no
// extra separators). Used to detect a family generation that leaked into the Variant slot.
func isPureDottedGen(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.':
		default:
			return false
		}
	}
	return hasDigit
}

// reOSeriesLine matches an OpenAI o-series leading ID token: "o" glued to a generation
// integer (o1, o3, o4). The capture is the generation number.
var reOSeriesLine = regexp.MustCompile(`^o([0-9]+)$`)

// canonicalizeOpenAILine applies the bestiary-xdbc (Q2/Q2a/Q2b/Q2c) o-series taxonomy
// restructure: ALL of OpenAI's gpt line decomposes under family=gpt, with the LINE
// DESIGNATOR carried in the VARIANT slot and SIZE tokens (mini/pro/…) demoted to the
// Modifier:
//
//   - o1 / o3 / o4              → (gpt, variant="o",     version=1/3/4)
//   - o1-mini                   → (gpt, variant="o",     version=1, modifier="mini")
//   - gpt-4o                    → (gpt, variant="4o",    version="")   ("4o" is the variant
//                                                                       NOT version 4)
//   - gpt-4o-mini[-DATE]        → (gpt, variant="4o",    version="", modifier="mini")
//   - chatgpt-4o-latest         → (gpt, variant="4o",    version="", modifier="latest")
//   - gpt-audio[-mini]          → (gpt, variant="audio", version="" [, modifier="mini"])
//   - gpt-4 / gpt-5 / gpt-oss…  → UNCHANGED (no o-series line designator in the ID)
//
// It is ID-DRIVEN (the designator comes from the model ID, not raw_family) so every provider
// of the same ID — however it mislabels raw_family ("gpt"/"o"/"o-mini"/"gpt-mini"/"gpt-audio")
// — converges on the IDENTICAL canonical tuple. Scoped to family ∈ {gpt, o, gpt-audio} so it
// can only ever touch OpenAI's line; a non-designator gpt model (gpt-4, gpt-5-chat, gpt-oss,
// gpt-image, gpt-codex) returns unchanged. This INTENTIONALLY supersedes the pre-S12 pinned
// BDD (o1-mini→(o,mini,1)); the change is sanctioned by bestiary-xdbc and gated by the
// reviewed o-series allowlist.
func canonicalizeOpenAILine(family Family, variant, version string, modifier []string, id ModelID) (Family, string, string, []string) {
	lf := strings.ToLower(string(family))
	if lf != "gpt" && lf != "o" && lf != "gpt-audio" {
		return family, variant, version, modifier
	}
	clean := strings.ToLower(lastPathSegment(stripVendorNamespace(string(id))))
	clean = strings.ReplaceAll(clean, "@", "-")
	toks := strings.Split(clean, "-")
	if len(toks) == 0 {
		return family, variant, version, modifier
	}

	designator := ""
	newVer := version
	switch {
	case reOSeriesLine.MatchString(toks[0]):
		designator = "o"
		newVer = reOSeriesLine.FindStringSubmatch(toks[0])[1] // the generation digits
	case containsToken(toks, "4o"):
		designator = "4o"
		newVer = "" // "4o" is the variant; version is empty
	case lf == "gpt-audio" || (containsToken(toks, "audio") && (toks[0] == "gpt" || toks[0] == "chatgpt")):
		designator = "audio"
		newVer = ""
	default:
		// gpt-4, gpt-5, gpt-oss, gpt-image, gpt-codex, gpt-3.5-turbo, … — not an o-line ID.
		return family, variant, version, modifier
	}

	// Size/finetune tokens (the OLD variant, e.g. mini/pro/deep-research) move to the
	// Modifier LIST when the variant slot is occupied by the line designator. The old
	// variant ADDS to any ID-extracted modifiers (CanonicalizeModifiers dedups) so no
	// information is dropped (SLICE-10).
	newMod := modifier
	if variant != "" && variant != designator {
		newMod = CanonicalizeModifiers(append(append([]string{}, modifier...), variant))
	}
	return Family("gpt"), designator, newVer, newMod
}

// reGlmV matches a glm version token with a glued trailing single 'v' (glm-4.5v → "4.5";
// glm-5v-turbo → "5"). The 'v' must be at a token boundary after the numeric version.
var reGlmV = regexp.MustCompile(`(?:^|-)(\d+(?:\.\d+)?)v(?:-|$)`)

// canonicalizeGlmV applies the bestiary-xdbc Q1 ruling: for the GLM family ONLY, a single
// letter 'v' GLUED to a glm version (glm-4.5v, glm-5v-turbo) is a VARIANT letter, NOT the
// 'vision' modifier — glm-4.5v → (glm, variant="v", version=4.5); glm-5v-turbo → (glm,
// variant="v", version=5, modifier="turbo"). Scoped to family=glm so the uniform
// vision-as-MODIFIER rule is UNCHANGED for the spelled-out "vision" token and for every
// other family (grok-vision, etc. — those carry "-vision" as a separate hyphen token and
// never reach here). The mis-derived "vision" modifier (produced by the generic glued-letter
// split for the glm 'v') is dropped; a tier in the old variant slot (turbo) is demoted to
// the Modifier.
func canonicalizeGlmV(family Family, variant, version string, modifier []string, id ModelID) (Family, string, string, []string) {
	if strings.ToLower(string(family)) != "glm" {
		return family, variant, version, modifier
	}
	clean := strings.ToLower(lastPathSegment(stripVendorNamespace(string(id))))
	m := reGlmV.FindStringSubmatch(clean)
	if m == nil {
		return family, variant, version, modifier
	}
	// Drop the mis-derived "vision" modifier: the glued 'v' is the variant, not vision.
	newMod := make([]string, 0, len(modifier))
	for _, mod := range modifier {
		if mod == "vision" {
			continue
		}
		newMod = append(newMod, mod)
	}
	// Demote a tier left in the variant slot (e.g. "turbo" for glm-5v-turbo) to the Modifier.
	if variant != "" && variant != "v" {
		newMod = append(newMod, variant)
	}
	return family, "v", m[1], CanonicalizeModifiers(newMod)
}

// containsToken reports whether toks contains tok exactly.
func containsToken(toks []string, tok string) bool {
	for _, t := range toks {
		if t == tok {
			return true
		}
	}
	return false
}

// isCleanVariantToken reports whether a variant token is a clean word-form variant
// (no '.' and no digit) — safe to OVERRIDE a populated raw variant with. Tokens
// carrying version/quantization junk (e.g. "v2.5-pro-6bit") are rejected so the
// unification never worsens a clean raw variant.
func isCleanVariantToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '.' || (r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// isVersionShaped reports whether s looks like a version (digits, optionally with
// '.'/'-' separators, and at most a trailing letter like "4o"). Used to guard
// version overrides so a populated version is only replaced by another version.
func isVersionShaped(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.' || r == '-':
			// separator
		case r >= 'a' && r <= 'z':
			// allow trailing/embedded letters (e.g. "4o")
		default:
			return false
		}
	}
	return hasDigit
}

// ParseFamilyDetailed is the failure-aware companion to ParseFamilyWithVersion.
// It returns the same three-way decomposition (Family, variant, version) plus
// an optional *ParseFailure when the parser detects a known incomplete result.
//
// Failure detection covers three modes:
//  1. Version digits trapped between family-prefix and variant:
//     "claude-3-5-haiku-20241022" → the 3-5 version digits are not extracted.
//  2. Suffix overflow: trailing token beyond expected family/variant/version/date.
//     Sub-cases: ReasonKnownSuffixOverflow (token in modifier allowlist) and
//     ReasonUnknownSuffixOverflow (token not in allowlist — extend allowlist).
//  3. YYMM-date-as-version false-positive: a 4-digit numeric segment that looks
//     like a YYMM date (1900–2999 range) but is treated as part of the family/version.
//
// The returned *ParseFailure is an annotation, NOT an error. The function always
// returns its best-effort family/variant/version values regardless of whether a
// failure was detected. Callers who need only the parse result can discard the
// *ParseFailure with _. Callers building an audit log should check failure != nil
// and accumulate.
//
// Parameters id and p (model ID and provider) are used to populate the failure
// record fields and are not used in the parse logic itself.
//
// IP-1: The return order is (family, variant, version, modifier, *ParseFailure).
// SLICE-2 (codegen wiring) depends on this exact 5-tuple shape — do NOT reorder.
func ParseFamilyDetailed(raw Family, id ModelID, p Provider) (Family, string, string, []string, *ParseFailure) {
	// rc3-L1 (bestiary-xfo0, USER-RATIFIED Impl-UAT 2gxu) — curated ID-keyed family
	// override. A CLOSED, exact-model-ID map for the embedded-family case the leading-token
	// pipeline cannot reach without a general embedded-detect (the Path-B trap: 16/249
	// llama-nemotron IDs have attestation conflicts, so a broad nemotron/llama match would
	// REGRESS them). Keyed to the EXACT id only → zero collateral. Applied to ALL providers
	// of that id (provider-agnostic) so the empty-raw and raw-populated forms CONVERGE on the
	// identical tuple. It corrects ONLY the FAMILY to the attested allFamilies target,
	// preserving the pipeline-derived variant/version so no ID-present field is dropped
	// (enforce-blessed family-only correction; cat-(c)=0).
	if ov, ok := idFamilyOverrides[strings.ToLower(string(id))]; ok {
		return ov.family, ov.variant, ov.version, nil, nil
	}

	// SLICE-3 (uniform thinking/vision-as-modifier migration): a trailing
	// {thinking,vision,…} token embedded in the RAW family is ALWAYS a modifier,
	// never a variant. models.dev encodes the modifier in the family field for some
	// providers (e.g. "deepseek-thinking", "kimi-thinking", "grok-vision"). Strip it
	// BEFORE decomposition so the family normalises (kimi-thinking → kimi); the
	// stripped token is surfaced as the modifier below (rawModifier) and takes effect
	// only when the model ID does not itself carry a trailing modifier that
	// ExtractModifier(id, …) can find (e.g. raw="deepseek-thinking", id="deepseek-r1"
	// — the "thinking" lives ONLY in the raw family).
	cleanRaw := raw
	rawModifier := ""
	if trimmed := Family(trimOneTrailingModifier(string(raw))); trimmed != raw {
		rawModifier = strings.TrimPrefix(string(raw)[len(trimmed):], "-")
		cleanRaw = trimmed
	}

	family, variant, version := ParseFamilyWithVersion(cleanRaw)

	// M4: lowercase Family field at the initial parse boundary.
	// Applied to all raw_family inputs including empty string (no-op on "").
	// For raw=="" the InferFamilyFromIDWithVariant path below applies its own M4.
	family = Family(strings.ToLower(string(family)))

	// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before bare-gen-split).
	// DEFAULT own-family → no-op; a ledger row remaps a mislabel to its canonical family.
	if pd, pdErr := loadParseData(); pdErr == nil {
		family = remapFamilyAlias(pd, family)
	}

	// No failure annotation when the input is empty: delegate to
	// InferFamilyFromIDWithVariant so that GUARD-2 passthrough cases (e.g.
	// kimi-k2-thinking) are handled correctly. The modifier is then extracted from
	// the inferred family+variant context. M4 is applied inside InferFamilyFromIDWithVariant.
	if raw == "" {
		// Empty raw_family: the decomposition is fully ID-driven (no family hint to
		// preserve). SLICE-9: this is the same idDrivenDecompose primitive the
		// raw-populated path now consults, so the two paths share one decomposition.
		family, variant, version, modifier := idDrivenDecompose(id, p)
		family, variant, version, modifier = canonicalizeOpenAILine(family, variant, version, modifier, id)
		family, variant, version, modifier = canonicalizeGlmV(family, variant, version, modifier, id)
		if pd, pdErr := loadParseData(); pdErr == nil {
			variant, modifier = promoteVariantModifier(pd, family, variant, modifier)
		}
		return family, variant, version, modifier, nil
	}

	// SLICE-9 (rc2) Option A unification: EVERY raw-populated return path funnels its
	// (family,variant,version,modifier) through reconcileIDDriven (family-preserving,
	// ID-driven Variant/Version/Modifier) before returning. The *ParseFailure audit
	// annotation is computed from the raw-path attempt and passed through unchanged.
	recon := func(f Family, v, ver string, m []string, fail *ParseFailure) (Family, string, string, []string, *ParseFailure) {
		rf, rv, rver, rmod := reconcileIDDriven(f, v, ver, m, id, p)
		rf, rv, rver, rmod = canonicalizeOpenAILine(rf, rv, rver, rmod, id)
		rf, rv, rver, rmod = canonicalizeGlmV(rf, rv, rver, rmod, id)
		if pd, pdErr := loadParseData(); pdErr == nil {
			rv, rmod = promoteVariantModifier(pd, rf, rv, rmod)
		}
		return rf, rv, rver, rmod, fail
	}

	// ── Δ1 extract-first: attempt to populate version from the model ID ───────
	// Extract modifier(s) first so they don't pollute version/date extraction.
	// SLICE-10: peel ALL trailing modifiers (member-guarded) into a LIST.
	modifierList, consumed := extractModifiers(id, family, variant)
	// SLICE-3: when the modifier was encoded in the RAW family (not the ID),
	// ExtractModifier(id, …) finds nothing — fall back to the raw-family modifier so
	// it is never silently dropped (e.g. raw="deepseek-thinking", id="deepseek-r1").
	// SLICE-10: rawModifier COMPOSES with any ID modifiers (lossless union) — BUT it is
	// MEMBER-GUARDED: a rawModifier token that is a curated member of the resolved family
	// is the product-LINE VARIANT, not a modifier (recoverMemberVariant restores it as the
	// variant below), so it must NOT be appended — otherwise a RawFamily-embedded member
	// (raw="sonar-reasoning" → "reasoning") duplicates into BOTH Variant and Modifier
	// (Reviewer-A/C BLOCKER, fix-cycle 1).
	if rawModifier != "" {
		if pd, pdErr := loadParseData(); pdErr != nil || !isFamilyMemberToken(pd, family, rawModifier) {
			modifierList = append(modifierList, rawModifier)
		}
	}
	modifier := CanonicalizeModifiers(modifierList)
	cleanedID := id
	if consumed != "" {
		cleanedStr := string(id)
		if len(cleanedStr) >= len(consumed) && cleanedStr[len(cleanedStr)-len(consumed):] == consumed {
			cleanedID = ModelID(cleanedStr[:len(cleanedStr)-len(consumed)])
		}
	}

	// ── Pipeline skeleton (SLICE-1) ──────────────────────────────────────────
	// M3 (vendor/namespace strip) is applied here at the member-recovery boundary
	// via stripVendorNamespace — the SAME helper InferFamilyFromIDWithVariant uses
	// at its head (Reviewer A jvpa: one M3 helper, called in BOTH entrypoints).
	// We deliberately scope M3 to the member-recovery memberZone rather than
	// pre-stripping the ID fed to the version extractors: ExtractVersionBetween…
	// already calls lastPathSegment internally, and pre-stripping the "<org>/"
	// segment ahead of ExtractVersionFromID would expose a SEPARATE latent
	// version-extraction issue (e.g. "gpt-oss-120b" → version "120b" param-count,
	// "gpt-4o" → version "4o") that is out of SLICE-1 scope (version-presence is
	// SLICE-2). Scoping M3 to member recovery keeps version extraction unchanged.
	//
	// SLICE-3 INSERTION POINT: family_aliases ledger remap (after M4, before bare-gen-split)
	// SLICE-2 INSERTION POINT: bare_gen_split closed predicate (before recoverMemberVariant)
	// Two halves, both gated on the SAME closed predicate (splitBareGen):
	//  (A) family-token split: when the raw family is itself a glued <base><int>
	//      (e.g. raw="qwen3"), split it to base family + generation version.
	//  (B) clean-family version recovery: when the raw family is already a bare_gen
	//      base (e.g. "qwen", "o") but the ID carries the glued generation token
	//      (e.g. "qwen3-max", "o3-mini" — NO hyphen between base and int, so the
	//      version extractors below cannot see it), recover the int as version so the
	//      clean-family path AGREES with the empty-raw inference path. Only ever
	//      FILLS an empty version (never clobbers a version PFWV already found), and
	//      the (B) ID token's base must equal the resolved family (no cross-family
	//      promotion). Param-count/size tokens (e.g. "120b" in gpt-oss-120b) are
	//      immune: splitBareGen requires a registered, bare_gen_split base, and size
	//      tokens are never the leading family token.
	if pd, pdErr := loadParseData(); pdErr == nil {
		if base, ver, ok := splitBareGen(pd, strings.ToLower(string(family))); ok {
			family = base
			if version == "" {
				version = ver
			}
		} else if version == "" {
			firstTok := lastPathSegment(strings.ToLower(stripVendorNamespace(string(cleanedID))))
			if idx := strings.IndexByte(firstTok, '-'); idx >= 0 {
				firstTok = firstTok[:idx]
			}
			if b, ver2, ok2 := splitBareGen(pd, firstTok); ok2 && b == family {
				version = ver2
			}
		}
	}
	//
	// recoverMemberVariant: recover variant from model ID member tokens (families.json
	// members + curated suffixes) for REGISTERED families when ParseFamilyWithVersion
	// did not populate it. Called BEFORE version extraction so the recovered variant is
	// available as the boundary token for ExtractVersionBetweenFamilyAndVariant. The
	// family-agnostic sole-residual suffix promotion (B1) for UNREGISTERED families runs
	// AFTER version extraction (see the B1 block below) so it preserves a version that
	// follows the suffix in the ID.
	if variant == "" {
		normFamPrefix := strings.ToLower(firstToken(string(family))) + "-"
		lowID := strings.ToLower(stripVendorNamespace(string(cleanedID)))
		memberZone := lowID
		if strings.HasPrefix(memberZone, normFamPrefix) {
			memberZone = memberZone[len(normFamPrefix):]
		}
		variant = recoverMemberVariant(strings.Split(memberZone, "-"), family)
	}

	// SLICE-8 (d): letter-prefix series split (CLARIFICATION-5). Runs BEFORE generic
	// version extraction so it OWNS the (variant, version) decomposition for series
	// families (kimi/minimax/mimo) and takes precedence over both the version_patterns
	// whole-token variant and the residual-unaccounted early-return below (e.g.
	// "kimi-k2-6" must become ('k','2.6'), not version "6" + residual "k2"). A trailing
	// curated TIER token is promoted to the Modifier (CLARIFICATION-6) — but ONLY when
	// no other modifier (thinking/vision from the ID or raw family) is already present;
	// multi-modifier cases keep the existing modifier and drop the tier (surfaced).
	if pd, pdErr := loadParseData(); pdErr == nil {
		if sv, svv, tierMod, ok := splitSeriesVariant(pd, family, string(cleanedID)); ok {
			variant, version = sv, svv
			// SLICE-10: the tier COMPOSES into the modifier LIST (CanonicalizeModifiers
			// dedups against any already-peeled tier/capability), no longer dropped.
			if tierMod != "" {
				modifier = CanonicalizeModifiers(append(append([]string{}, modifier...), tierMod))
			}
		}
	}

	// If ParseFamilyWithVersion did not extract a version, attempt extraction from
	// the model ID using the canonical family prefix.
	if version == "" && family != "" && cleanedID != "" {
		if v, residual := ExtractVersionBetweenFamilyAndVariant(cleanedID, family, variant); v != "" {
			version = v
			// B1 sole-residual promotion (family-agnostic). When exactly ONE residual
			// token remains AND it is a known variant suffix AND Variant is still empty,
			// promote it into Variant instead of emitting ReasonResidualUnaccountedTokens.
			// Handles e.g. seed-1-6-flash-250715 → (seed,flash,1.6), reka-flash-3 →
			// (reka,flash,3), text-embedding-005 → (text-embedding,embedding,005).
			//
			// This MUST run post-version: the version is already captured here, so the
			// promotion preserves a version that follows the suffix in the ID (e.g. the
			// "3" in reka-flash-3). recoverMemberVariant (called pre-version, above) owns
			// member recovery for REGISTERED families; this narrow sole-residual suffix
			// promotion covers UNREGISTERED families that recoverMemberVariant skips.
			// >1 residual (B2) or a non-suffix sole token (C) remain documented residuals.
			if len(residual) == 1 && variant == "" {
				if pd, pdErr := loadParseData(); pdErr == nil {
					if bare, ok := bareVariantSuffix(pd, strings.ToLower(residual[0])); ok {
						variant = bare
						residual = nil
					}
				}
			}
			// R2: any residual still remaining after promotion is genuinely unaccounted —
			// emit an honest-audit failure WITH version populated.
			if len(residual) > 0 {
				attempted := ParseAttempt{Family: family, Variant: variant, Version: version, Date: ""}
				return recon(family, variant, version, modifier, &ParseFailure{
					RawID:          id,
					Provider:       p,
					RawFamily:      raw,
					AttemptedParse: attempted,
					Reason:         ReasonResidualUnaccountedTokens,
				})
			}
		}

		// Fallback: try the direct ID-prefix extractor with the raw family first
		// (more specific prefix), then with the extracted family. The raw family
		// (e.g. "claude-opus") gives a better prefix match than extracted family
		// (e.g. "claude") for IDs like "claude-opus-4-6".
		if version == "" {
			version = ExtractVersionFromID(cleanedID, raw)
		}
		if version == "" {
			version = ExtractVersionFromID(cleanedID, family)
		}
		// SLICE-8 (c): glued version-modifier fallback (glm-4.5v → version 4.5 +
		// modifier vision). The trailing "v" is glued to the version so neither
		// ExtractModifier (separate hyphen-token) nor the extractors above catch it.
		// Only fills an empty version and only sets the modifier when still empty.
		if version == "" {
			if v, gmod := idDrivenVersion(cleanedID, family, variant); v != "" {
				version = v
				if gmod != "" {
					modifier = CanonicalizeModifiers(append(append([]string{}, modifier...), gmod))
				}
			}
		}
	}

	// Build the attempted parse for potential failure records.
	attempted := ParseAttempt{
		Family:  family,
		Variant: variant,
		Version: version,
		// Date is populated by ExtractDate separately; pass empty string here
		// since this function does not have access to the release date field.
		Date: "",
	}

	rawStr := string(raw)

	// ── Failure mode 3: YYMM-date-as-version false-positive ──────────────────
	// Detect 4-digit date-like numerals (e.g. "mistral-2401", "mistral-0528")
	// where a 4-digit segment could be mistaken for a version. Originally this
	// only caught the YYMM century range (19xx–29xx) via reYYMMCandidate; FIX-A
	// generalizes to ANY standalone 4-digit numeric segment via
	// reBareAnyFourDigitCandidate, covering non-YYMM-range tokens like "0528"
	// and "0324" in raw family strings. These appear in the raw family string,
	// not as a separate date field.
	if reBareAnyFourDigitCandidate.MatchString(rawStr) {
		return recon(family, variant, version, modifier, &ParseFailure{
			RawID:          id,
			Provider:       p,
			RawFamily:      raw,
			AttemptedParse: attempted,
			Reason:         ReasonYYMMDateAsVersion,
		})
	}

	// ── Failure mode 2: Suffix overflow ──────────────────────────────────────
	// Detect cases where the model ID ends with a trailing modifier token that
	// the parser did NOT capture as the variant. This covers IDs like
	// "claude-opus-4-thinking" where "thinking" is a modifier but rawFamily
	// "claude-opus-4" parses to variant="" (no variant extracted).
	//
	// We classify the trailing token into two sub-cases:
	//
	//   ReasonKnownSuffixOverflow   — trailing token is in the modifier allowlist
	//                                 (thinking, think, vision, latest, code, preview)
	//   ReasonUnknownSuffixOverflow — trailing token is NOT in the allowlist
	//                                 (audit-log hint to extend the allowlist)
	//
	// Condition: fires when the model ID's trailing token is a modifier AND
	// that token is NOT the already-parsed variant (to avoid double-reporting
	// cases where suffix stripping correctly extracted the modifier as variant).
	//
	// Note: this is intentionally separate from ExtractModifier (added by
	// SLICE-FIX-V2-5), which extracts the modifier as a first-class field.
	// After V2-5 lands, most ReasonKnownSuffixOverflow cases will be pre-empted
	// by ExtractModifier; this block catches residuals.
	if string(id) != "" {
		pd, pdErr := loadParseData()
		if pdErr == nil {
			idTrailing := extractTrailingToken(string(id))
			// Build a fast lookup set from pd.modifiers.
			isKnownModifier := false
			for _, mod := range pd.modifiers {
				if mod == idTrailing {
					isKnownModifier = true
					break
				}
			}
			// Only fire if trailing token is a modifier AND is not already the parsed
			// variant AND the cleaned ID (with modifier stripped) does not end with a
			// date token. The variant check avoids double-reporting when the parser
			// correctly extracted the modifier as the variant. The date check suppresses
			// spurious overflow when a modifier legitimately follows a release date
			// (e.g. "claude-opus-4-1-20250805-thinking" — the modifier is expected after
			// the date and is fully accounted for by ExtractModifier).
			cleanedEndsWithDate := stripTrailingDate(string(cleanedID)) != string(cleanedID)
			if idTrailing != variant && !cleanedEndsWithDate && (isKnownModifier || detectSuffixOverflow(rawStr, family, variant, version)) {
				var reason ParseFailureReason
				if isKnownModifier {
					reason = ReasonKnownSuffixOverflow
				} else {
					reason = ReasonUnknownSuffixOverflow
				}
				return recon(family, variant, version, modifier, &ParseFailure{
					RawID:          id,
					Provider:       p,
					RawFamily:      raw,
					AttemptedParse: attempted,
					Reason:         reason,
				})
			}
		}
	}

	return recon(family, variant, version, modifier, nil)
}

// reYYMMCandidate matches a 4-digit segment in a hyphen-separated raw family
// string that falls in the YYMM range (1900–2999). These are characteristic of
// Mistral versioning (e.g. "mistral-2401", "pixtral-2411").
// The segment must be at a word boundary within the hyphenated string.
var reYYMMCandidate = regexp.MustCompile(`(?:^|-)(?:19|20|21|22|23|24|25|26|27|28|29)\d{2}(?:-|$)`)

// reBareAnyFourDigitCandidate matches any standalone 4-digit all-numeric segment
// in a hyphen-separated string. This is the FIX-A generalization of reYYMMCandidate:
// it catches 4-digit tokens like "0528", "0324", "0905" in addition to YYMM-range
// tokens like "2603", "2512". Used by ParseFamilyDetailed parity check.
var reBareAnyFourDigitCandidate = regexp.MustCompile(`(?:^|-)\d{4}(?:-|$)`)

// extractTrailingToken returns the last hyphen-separated token of s,
// or the whole string if there is no hyphen.
func extractTrailingToken(s string) string {
	if idx := strings.LastIndexByte(s, '-'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// detectVersionDigitsInID returns true when the model ID contains one or more
// purely-numeric hyphen-separated tokens between the canonical family prefix and
// the variant name. This identifies cases like:
//
//	id="claude-3-5-haiku-20241022", family="claude", variant="haiku"
//	→ after stripping "claude-" prefix and trailing date, "3-5-haiku" remains
//	→ tokens ["3","5","haiku"]: digits appear before the variant → true
//
//	id="claude-opus-4-20250514", family="claude", variant="opus"
//	→ after stripping "claude-" prefix and trailing date, "opus-4" remains
//	→ tokens ["opus","4"]: variant appears before digits → false (no overflow digits ahead of variant)
//
// The function strips trailing YYYYMMDD/YYYY-MM-DD dates from the ID before
// token inspection to avoid misclassifying date segments as version digits.
func detectVersionDigitsInID(id ModelID, family Family, variant string) bool {
	if family == "" || variant == "" || id == "" {
		return false
	}

	idStr := string(id)

	// Strip any leading path segments (multi-segment provider IDs).
	if idx := strings.LastIndexByte(idStr, '/'); idx >= 0 {
		idStr = idStr[idx+1:]
	}

	// Build the expected family prefix for stripping (e.g. "claude-").
	familyPrefix := string(family) + "-"
	if !strings.HasPrefix(idStr, familyPrefix) {
		return false
	}

	// Strip the family prefix, then strip any trailing date.
	remainder := idStr[len(familyPrefix):]
	remainder = stripTrailingDate(remainder)
	if remainder == "" {
		return false
	}

	// Tokenize the remainder on hyphens.
	tokens := strings.Split(remainder, "-")

	// Check if there are purely-numeric tokens BEFORE the first variant token.
	// The variant may be multi-token (e.g. "flash-lite"), so we look for ANY
	// variant token among the leading tokens.
	variantTokens := strings.Split(variant, "-")
	variantFirst := variantTokens[0]

	for i, tok := range tokens {
		if tok == variantFirst {
			// Variant token found. If any earlier token was purely numeric, it's a version miss.
			for _, prev := range tokens[:i] {
				if isVersionToken(prev) {
					return true
				}
			}
			return false
		}
	}
	// Variant token not found in remaining ID tokens — no version-between-variant pattern.
	return false
}

// detectSuffixOverflow returns true when the raw family string contains more
// hyphen-separated segments than can be accounted for by the parsed
// family/variant/version tokens. This catches overflow cases where the heuristics
// consume only a prefix and silently drop the rest.
//
// Algorithm: rebuild the "known tokens" from family + variant + version (using
// their hyphen-split forms), then count how many tokens in rawStr are not
// accounted for by the known set. If >0 extra tokens exist beyond a threshold,
// it is likely overflow.
func detectSuffixOverflow(rawStr string, family Family, variant string, version string) bool {
	// Tokenize rawStr.
	rawTokens := strings.Split(rawStr, "-")

	// Build the set of tokens that the parser "consumed".
	knownTokens := make(map[string]struct{})
	for _, t := range strings.Split(string(family), "-") {
		if t != "" {
			knownTokens[t] = struct{}{}
		}
	}
	for _, t := range strings.Split(variant, "-") {
		if t != "" {
			knownTokens[t] = struct{}{}
		}
	}
	for _, t := range strings.Split(strings.ReplaceAll(version, ".", "-"), "-") {
		if t != "" {
			knownTokens[t] = struct{}{}
		}
	}

	// Count tokens not accounted for.
	extra := 0
	for _, tok := range rawTokens {
		if tok == "" {
			continue
		}
		if _, known := knownTokens[tok]; !known {
			extra++
		}
	}

	// Overflow threshold: more than 2 unaccounted tokens suggests overflow.
	// The threshold of 2 avoids false positives for models with minor extra tokens.
	return extra > 2
}

// --------------------------------------------------------------------------
// SLICE-1: M3 vendor-strip helpers
// --------------------------------------------------------------------------

// stripVendorAliasPrefix strips a leading "<alias>/" or "<alias>-" prefix from
// id when the first "/" or "-"-separated token (lowercased) matches one of the
// provided vendor aliases. Used for M3 vendor/namespace strip in addition to the
// path-based lastPathSegment call.
//
// The "/" case is already handled by lastPathSegment (in the same M3 step) but
// is also attempted here for robustness. The "-" case handles IDs like
// "minimaxai-minimax-m1" where the vendor alias is the first hyphen token.
//
// If no alias matches, id is returned unchanged.
func stripVendorAliasPrefix(id string, vendorAliases []string) string {
	if len(vendorAliases) == 0 {
		return id
	}
	lowID := strings.ToLower(id)
	for _, alias := range vendorAliases {
		// "/" separator: e.g. "minimaxai/model-name" → "model-name"
		slashPrefix := alias + "/"
		if strings.HasPrefix(lowID, slashPrefix) {
			return id[len(slashPrefix):]
		}
		// "-" separator: e.g. "minimaxai-model-name" → "model-name"
		hyphenPrefix := alias + "-"
		if strings.HasPrefix(lowID, hyphenPrefix) {
			return id[len(hyphenPrefix):]
		}
	}
	return id
}

// stripVendorNamespace applies the M3 vendor/namespace strip to a model ID: first
// the leading "<org>/" path segment (lastPathSegment), then any residual
// non-provider "<vendor_alias>-" / "<vendor_alias>/" prefix (stripVendorAliasPrefix,
// consulting vendor_aliases.json). Returns the stripped ID.
//
// This is the SINGLE M3 pipeline head shared by BOTH decomposition entrypoints
// (InferFamilyFromIDWithVariant and ParseFamilyDetailed), so the strip is applied
// symmetrically. Resolves Reviewer A finding jvpa: the vendor-alias strip
// previously lived only in InferFamilyFromIDWithVariant, leaving ParseFamilyDetailed's
// raw!="" path without it.
func stripVendorNamespace(id string) string {
	// SLICE-14 (TIER-1): SURGICAL doubled-vendor strip. Some providers prefix the model name
	// with the org AND repeat it: org "meta-llama/" + name "Meta-Llama-3.1-8B-Instruct". After
	// the org segment is stripped, the leading "Meta-" makes the family derive "meta" instead
	// of "llama". Strip the redundant leading "meta-" ONLY when the ORG segment is literally
	// "meta-llama" (the doubled signal) — NOT a broad "meta" vendor alias, which caused cat-(c)
	// collateral on odd-format IDs (Meta-Llama-3-1-…-FP8, meta-llama-3_3-70b with no such org).
	if i := strings.IndexByte(id, '/'); i > 0 {
		org := strings.ToLower(id[:i])
		rest := id[i+1:]
		if org == "meta-llama" && strings.HasPrefix(strings.ToLower(rest), "meta-") {
			id = rest[len("meta-"):] // "Meta-Llama-3.1-…" → "Llama-3.1-…"
		}
	}
	idStr := lastPathSegment(id)
	if pd, err := loadParseData(); err == nil {
		idStr = stripVendorAliasPrefix(idStr, pd.vendorAliases)
	}
	return idStr
}

// --------------------------------------------------------------------------
// SLICE-1: recoverMemberVariant (sole owner)
// --------------------------------------------------------------------------

// bareVariantSuffix reports whether lowTok (lowercase, no leading "-") matches a
// curated variant suffix in pd.suffixes, returning the bare suffix on match.
// pd.suffixes entries carry a leading "-"; this strips it before comparison. It is
// the single source of truth for "is this token a known variant suffix", shared by
// recoverMemberVariant (member-zone scan) and the B1 sole-residual promotion in
// ParseFamilyDetailed.
func bareVariantSuffix(pd *parseData, lowTok string) (string, bool) {
	for _, sfx := range pd.suffixes {
		bare := sfx
		if len(bare) > 0 && bare[0] == '-' {
			bare = bare[1:]
		}
		if lowTok == bare {
			return bare, true
		}
	}
	return "", false
}

// --------------------------------------------------------------------------
// SLICE-2: bare_gen_split closed predicate
// --------------------------------------------------------------------------

// splitBareGen applies the M2 bare-generation split CLOSED predicate to a single
// lowercase token. It splits a glued/hyphen-joined family-and-generation token
// <base><int> or <base>-<int> (e.g. "qwen3", "o1", "gpt-5", "gemini-3") into its
// base family and the trailing generation int, returning (base, version, true)
// ONLY when ALL THREE clauses hold:
//
//  1. has-entry-in-families.json: base is a known family key (pd.families[base]
//     exists). This is the clause that lets the negatives v0/asi1/esm2/wan2/hy3/r1
//     (bases v/asi/esm/wan/hy/r) and l3 (base l) fall out — none are family keys.
//  2. base-name-not-digit-suffixed: the alphabetic base does not itself end in a
//     digit (defends against double-promoting an already-versioned base).
//  3. bare_gen_split flag: pd.families[base].BareGenSplit is true. The flag is the
//     CURATION-TIME record that the split form is attested in the committed
//     snapshot (set in parse/data/families.json), keeping the predicate CLOSED —
//     no per-name runtime allow-list.
//
// The trailing generation is the maximal run of [0-9.] at the end of the token
// (so "qwen3" → "3", "qwen3.5" → "3.5"); a single separating hyphen between base
// and int is consumed ("gpt-5" → base "gpt"). Tokens with no trailing digit
// (e.g. "mimo") and all-numeric tokens return ok=false. The predicate never sees
// letter-prefixed dotted variant tokens (m2.5/k2.5/v2.5): those are variants, not
// family tokens, and their families carry no bare_gen_split flag regardless.
func splitBareGen(pd *parseData, tok string) (Family, string, bool) {
	if pd == nil || tok == "" {
		return "", "", false
	}
	// Maximal trailing run of digits and dots = the generation version.
	end := len(tok)
	i := end
	for i > 0 {
		c := tok[i-1]
		if (c >= '0' && c <= '9') || c == '.' {
			i--
			continue
		}
		break
	}
	if i == end {
		return "", "", false // no trailing digit/dot run
	}
	version := tok[i:]
	base := tok[:i]
	// Consume a single separating hyphen ("gpt-5" → base "gpt").
	base = strings.TrimSuffix(base, "-")
	// version must contain at least one digit (guard against a lone "." tail).
	if base == "" || !strings.ContainsAny(version, "0123456789") {
		return "", "", false
	}
	// Clause 2: base-name-not-digit-suffixed.
	if last := base[len(base)-1]; last >= '0' && last <= '9' {
		return "", "", false
	}
	// Clause 1 (has-entry) + Clause 3 (bare_gen_split flag attested in snapshot).
	info, ok := pd.families[Family(base)]
	if !ok || !info.BareGenSplit {
		return "", "", false
	}
	return Family(base), version, true
}

// recoverMemberVariant recovers a variant token from idTokens (the hyphen-split
// tokens of a model ID AFTER the family prefix is stripped) by scanning for the
// first token that matches a families.json member (most specific) or a curated
// variant suffix (pd.suffixes). Token comparison is case-insensitive. Returns the
// bare variant token (lowercase, no leading "-"), or "" if none matches.
//
// SCOPE: recovery is limited to REGISTERED families (present in families.json).
// Unregistered/compound families (e.g. "text-embedding") return "" here, because
// firstToken-prefix stripping is imprecise for them and a broad multi-token scan
// would over-recover a family sub-token ("embedding", "large") as a variant. The
// narrow, version-preserving sole-residual suffix promotion for UNREGISTERED
// families (the original "B1", e.g. seed-1-6-flash-250715 → flash) is restored in
// ParseFamilyDetailed AFTER version extraction — see the B1 block there. It must run
// post-version: promoting a suffix that precedes the version (e.g. reka-flash-3)
// pre-version would make ExtractVersionBetweenFamilyAndVariant stop at the variant
// boundary and drop the version.
//
// This function subsumes the empty-raw amputation (firstToken fallback in
// InferFamilyFromIDWithVariant) for registered families.
//
// Precondition: ExtractModifier has already been called and modifier tokens
// stripped from the ID before computing idTokens. This ensures modifier tokens
// (e.g. "-thinking") are not misidentified as variant members.
func recoverMemberVariant(idTokens []string, family Family) string {
	pd, err := loadParseData()
	if err != nil {
		return ""
	}
	info, hasFamilyInfo := pd.families[Family(strings.ToLower(string(family)))]
	if !hasFamilyInfo {
		// SLICE-12 (#5) generic suffix-variant recovery for UNREGISTERED families
		// (codellama, rnj, mixtral, voxtral, lyria — the enforce-set distinct families and
		// over-capture short bases that carry no families.json member list). Recover a
		// trailing GENERIC variant word (instruct/pro/mini/large/small/medium) so the
		// empty-raw path matches the raw-populated sibling's variant (A-1/A-2 member-variant
		// residual). Scoped to a small curated word set (NOT the full suffix list) so it
		// cannot over-recover ambiguous tokens (-r/-a/-oss) for unregistered families.
		for i, tok := range idTokens {
			if _, ok := genericVariantWords[strings.ToLower(tok)]; !ok {
				continue
			}
			// Skip a generic word immediately followed by a PLAIN version token (e.g.
			// "devstral-small-2-…": recovering "small" as the variant would strip the "2"
			// version boundary). A following size/date token does not block recovery.
			if i+1 < len(idTokens) {
				nxt := idTokens[i+1]
				if isVersionShaped(nxt) && !isParamSizeToken(nxt) && !isDateShapedToken(nxt) {
					continue
				}
			}
			return strings.ToLower(tok)
		}
		return ""
	}
	// SLICE-12 (#6) flash-lite tier: prefer a COMPOUND (multi-token) member that appears
	// as a contiguous hyphen-delimited run in the ID over a shorter single-token member —
	// e.g. "gemini-2.5-flash-lite-preview" must recover "flash-lite", NOT the bare "flash"
	// that the single-token scan below would match first. Longest compound member wins.
	joined := strings.ToLower(strings.Join(idTokens, "-"))
	bestCompound := ""
	for _, m := range info.Members {
		if !strings.Contains(m, "-") {
			continue
		}
		if joined == m || strings.HasPrefix(joined, m+"-") || strings.HasSuffix(joined, "-"+m) || strings.Contains(joined, "-"+m+"-") {
			if len(m) > len(bestCompound) {
				bestCompound = m
			}
		}
	}
	if bestCompound != "" {
		return bestCompound
	}
	for _, tok := range idTokens {
		if tok == "" {
			continue
		}
		lowTok := strings.ToLower(tok)
		// 1. Family members (most specific — curated in families.json).
		for _, member := range info.Members {
			if lowTok == member {
				return member
			}
			// SLICE-14: a member GLUED to a trailing param-size ("r7b" = member "r" + size
			// "7b" for command-r7b-…) recovers the bare member — the param-size is GH#9 noise
			// dropped from the canonical tuple. Both pieces are attested (member ∈ families.json,
			// size ∈ isParamSizeToken), so this is mechanical, not a speculative add.
			if rest, ok := strings.CutPrefix(lowTok, member); ok && rest != "" && isParamSizeToken(rest) {
				return member
			}
		}
		// 2. Curated variant suffix fallback (registered families only).
		if bare, ok := bareVariantSuffix(pd, lowTok); ok {
			return bare
		}
	}
	return ""
}

// genericVariantWords is the small CLOSED set of family-agnostic variant words recovered
// for UNREGISTERED families (SLICE-12 #5). These are unambiguous tier/finetune words that
// are a VARIANT for whatever family carries them (codellama-…-instruct, lyria-…-pro,
// voxtral-small). Deliberately excludes short/ambiguous suffixes (r/a/oss/base) that would
// over-recover for an unregistered family with no curated member list to disambiguate.
var genericVariantWords = map[string]struct{}{
	"instruct": {}, "pro": {}, "mini": {}, "large": {}, "small": {}, "medium": {},
}

// --------------------------------------------------------------------------
// Small string helpers (R3c / Δ2′ support)
// --------------------------------------------------------------------------

// lastPathSegment returns the substring after the last '/' in s, or s itself
// when no '/' is present. Used to strip leading provider path segments from
// model IDs (e.g. "anthropic/claude-opus-4-6" → "claude-opus-4-6").
func lastPathSegment(s string) string {
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// orSelf returns candidate when it is non-empty, otherwise falls back to s.
// Used in InferFamilyFromIDWithVariant to guard no-op stripTrailingDate calls.
func orSelf(candidate, s string) string {
	if candidate != "" {
		return candidate
	}
	return s
}

// firstToken returns the first hyphen-separated token of s, or s itself when
// no hyphen is present.
func firstToken(s string) string {
	if idx := strings.IndexByte(s, '-'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// --------------------------------------------------------------------------
// isYYMMDateToken (R3b / eq7w → FIX-A generalization)
// --------------------------------------------------------------------------

// isYYMMDateToken returns true when tok is a 4-digit all-numeric string.
// This is the FIX-A generalization of the original YYMM guard (eq7w):
// the original guard only rejected tokens in the YYMM century range (19xx–29xx),
// but supervisor analysis confirmed that ALL bare-4-digit tokens in the 1745
// version-populated models are date/release-ids (MMDD or YYMM format), with ZERO
// legitimate bare-4-digit semantic versions. Therefore the guard is extended to
// reject ANY 4-digit numeric token (e.g. "0528", "0324", "0905" in addition to
// the existing "2603", "2512").
//
// Examples of now-rejected tokens: "0528" (deepseek-r1-0528), "0324" (deepseek-v3-0324),
// "2603" (mistral-small-2603), "2512" (existing YYMM range).
//
// Parity contract: any tok for which isYYMMDateToken returns true must NOT be
// treated as a version by isVersionToken or ExtractVersionFromID. The same guard
// is also applied in ParseFamilyDetailed via reBareAnyFourDigitCandidate, which
// matches any 4-digit segment in the raw family string (parity across all three
// call sites).
func isYYMMDateToken(tok string) bool {
	if len(tok) != 4 {
		return false
	}
	// Any 4-digit purely-numeric token is treated as a date/release-id, not a
	// semantic version. Check all-digits first (fast path); no range restriction.
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// --------------------------------------------------------------------------
// SLICE-1-FIX-3: date-shape guards for dot-join paths
// --------------------------------------------------------------------------

// is6DigitYYMMDD returns true when tok is exactly 6 all-numeric digits.
// These appear as compact YYMMDD date suffixes in model IDs (e.g. "250615",
// "251215", "250715") and must not be treated as semantic version components.
// Examples: "250615" (doubao-seed-1-6-250615), "250715" (seed-1-6-flash-250715).
func is6DigitYYMMDD(tok string) bool {
	if len(tok) != 6 {
		return false
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isMMYYYYTwoGroup returns true when s is a two-hyphen-separated group with
// the form MM-YYYY where MM is 01-12 and YYYY starts with 19 or 20.
// This detects date strings like "08-2024", "03-2025", "12-2024" that appear
// as the entire remainder after stripping a family prefix in model IDs such as
// "command-r-08-2024" or "command-a-03-2025".
// Precondition: s must already be confirmed to match reHyphenDigits (all-digit groups).
func isMMYYYYTwoGroup(s string) bool {
	// Must be exactly "NN-NNNN" (7 chars with hyphen at position 2).
	if len(s) != 7 || s[2] != '-' {
		return false
	}
	mm, yyyy := s[:2], s[3:]
	// MM must be purely numeric (already guaranteed by reHyphenDigits precondition).
	// Range check: 01–12.
	mm0, mm1 := mm[0]-'0', mm[1]-'0'
	if mm0 == 0 && mm1 == 0 {
		return false // "00" is not a valid month
	}
	if mm0 > 1 {
		return false // month > 19 is invalid
	}
	if mm0 == 1 && mm1 > 2 {
		return false // month > 12 is invalid
	}
	// YYYY must start with "19" or "20".
	if (yyyy[0] != '1' || yyyy[1] != '9') && (yyyy[0] != '2' || yyyy[1] != '0') {
		return false
	}
	return true
}

// isDateShapedToken returns true when tok is a date-shaped digit group that must
// NOT be included as part of a semantic version. Covers all date shapes defined by
// SLICE-1-FIX-3: 4-digit (YYMM/MMDD), 6-digit (YYMMDD).
// MM-YYYY two-group detection requires two tokens and is handled separately by
// isMMYYYYTwoGroup on the full remainder.
func isDateShapedToken(tok string) bool {
	return isYYMMDateToken(tok) || is6DigitYYMMDD(tok)
}

// --------------------------------------------------------------------------
// SLICE-8 (b): param-size guard (mirrors the date guard)
// --------------------------------------------------------------------------

// reParamSizeToken matches a parameter-count / model-size token that must NEVER
// be promoted to Version. The size INFO is a documented residual (GH#9, missing
// Size/Quantization dimension) — explicitly NOT a version and NOT a cross-provider
// divergence. Mirrors the YYMM/date guard pattern.
//
// Shapes covered (case-insensitive; the b/m unit suffix is the discriminator):
//   - dense param count:  "120b", "20b", "7b", "1.5b", "560m", "7m"
//   - MoE "NxNNb":        "8x22b", "8x7b"
//   - MoE active-params:  "30b-a3b", "300b-a47b", "235b-a22b", "480b-a35b"
//
// Genuine version tokens are NOT matched because they have no b/m unit suffix:
// "4o" (ends "o"), "4.5", "5", "3.1", "2603" (date, also guarded separately).
var reParamSizeToken = regexp.MustCompile(`^(?i:\d+(?:\.\d+)?[bm]|\d+x\d+b|\d+b-a\d+b)$`)

// isParamSizeToken reports whether tok is a parameter-count / model-size token
// (e.g. "120b", "7m", "8x22b", "30b-a3b"). Such tokens are dropped from version
// extraction by the SLICE-8 (b) param-size guard so that, e.g., gpt-oss-120b
// decomposes to Version "" on ALL providers (consistent), with the size INFO left
// to GH#9 rather than masquerading as a version.
func isParamSizeToken(tok string) bool {
	if tok == "" {
		return false
	}
	return reParamSizeToken.MatchString(tok)
}

// --------------------------------------------------------------------------
// SLICE-8 (c): glued letter-suffix on a version (the glmv / glm-4.5v case)
// --------------------------------------------------------------------------

// gluedVersionModifierLetters maps a recognized modifier-letter that may be GLUED
// to the trailing end of a numeric version token (e.g. the "v" in "glm-4.5v") to
// its canonical modifier name. Only letters in this curated map are split off; any
// other trailing letter (e.g. the "o" in "gpt-4o") is left intact so genuine
// alphanumeric version tokens are never mangled.
var gluedVersionModifierLetters = map[byte]string{
	'v': "vision",
}

// reGluedVersionModifier matches a numeric version (integer or N.M dotted) with a
// SINGLE trailing ASCII letter glued to it: e.g. "4.5v", "5v". The leading group is
// a genuine numeric version so that "4o" (caught by the alpha-num version path) and
// param-size tokens ("120b") are excluded — those are filtered by the modifier-letter
// allowlist (gluedVersionModifierLetters) regardless.
var reGluedVersionModifier = regexp.MustCompile(`^(\d+(?:\.\d+)?)([a-zA-Z])$`)

// splitGluedVersionModifier inspects the LAST hyphen-token of id for a numeric
// version with a glued recognized modifier-letter (SLICE-8 (c), the glm-4.5v case).
// On match it returns (cleanedID, modifier, true) where cleanedID has the trailing
// letter removed (so version extraction sees the bare numeric version) and modifier
// is the expanded name (e.g. "vision"). On no match it returns (id, "", false).
//
// SCOPE-NOTE (SLICE-8 (c) / S3→S8 hand-off): glm-4.5v decomposes to
// (glm, "", 4.5, modifier=vision). The trailing "v" is GLUED to the version so the
// SLICE-3 trailing-{vision}-token modifier rule (which matches "-vision" as a
// separate hyphen token) does NOT catch it; this glued-suffix split is the dedicated
// owner. Only recognized modifier-letters (v=vision) are split — "4o" stays "4o".
// CRITICAL: this is the TRAILING-letter case (modifier). The LEADING series-letter
// case (mimo-v2.5 → variant "v") is SLICE-8 (d); position is the discriminator.
func splitGluedVersionModifier(id string) (string, string, bool) {
	if id == "" {
		return id, "", false
	}
	last := id
	if idx := strings.LastIndexByte(id, '-'); idx >= 0 {
		last = id[idx+1:]
	}
	m := reGluedVersionModifier.FindStringSubmatch(last)
	if m == nil {
		return id, "", false
	}
	letter := m[2][0]
	// Normalise the letter to lowercase for the allowlist lookup.
	if letter >= 'A' && letter <= 'Z' {
		letter += 'a' - 'A'
	}
	modifier, ok := gluedVersionModifierLetters[letter]
	if !ok {
		return id, "", false
	}
	// Drop exactly the trailing letter, leaving the numeric version glued to the
	// preceding family/version context (e.g. "glm-4.5v" → "glm-4.5").
	return id[:len(id)-1], modifier, true
}

// --------------------------------------------------------------------------
// SLICE-8 (d): letter-prefix model-series split (CLARIFICATION-5)
// --------------------------------------------------------------------------

// reSeriesDotP matches the within-token version part of a series token AFTER the
// series letter, with a "." or "p"(=dot) separator: "2.5", "2p5", "2p6" → N.M.
var reSeriesDotP = regexp.MustCompile(`^(\d+)[.p](\d+)$`)

// reContextWindow matches a context-window token like "80k", "16k" that lives only
// in the ID (a discriminator, NOT a version, NOT a tier). Skipped by the series split.
var reContextWindow = regexp.MustCompile(`^\d+k$`)

// isAllDigits reports whether s is non-empty and every rune is an ASCII digit.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// isKnownModifierToken reports whether tok is in the modifier allowlist (pd.modifiers).
func isKnownModifierToken(pd *parseData, tok string) bool {
	if pd == nil {
		return false
	}
	for _, m := range pd.modifiers {
		if m == tok {
			return true
		}
	}
	return false
}

// parseSeriesNumber parses the version part of a series token (the substring AFTER
// the series letter, e.g. "2", "2.5", "2p5", "25"). Returns (version, hadSep, ok)
// where hadSep is true when an explicit "." / "p" separator was present (so a later
// hyphen-as-dot merge is NOT applied). A bare date-shaped run (e.g. "2603") is
// rejected (ok=false) so a date is never mistaken for a series version.
func parseSeriesNumber(rest string) (version string, hadSep bool, ok bool) {
	if rest == "" {
		return "", false, false
	}
	if m := reSeriesDotP.FindStringSubmatch(rest); m != nil {
		return m[1] + "." + m[2], true, true
	}
	if isAllDigits(rest) {
		if isDateShapedToken(rest) {
			return "", false, false
		}
		return rest, false, true
	}
	return "", false, false
}

// splitSeriesVariant implements the SLICE-8 (d) / CLARIFICATION-5 letter-prefix
// model-series decomposition. For a family carrying a series_letter (kimi→"k",
// minimax→"m", mimo→"v"), it finds the "<letter><number>" token in the model ID
// and returns (variant=<letter>, version=<number>, ok=true). It normalizes ALL
// attested forms consistently:
//
//	kimi-k2        → ("k", "2")     minimax-m1   → ("m", "1")
//	kimi-k2.5      → ("k", "2.5")   mimo-v2.5    → ("v", "2.5")
//	kimi-k2p5      → ("k", "2.5")   (p = dot)    kimi-k2p6 → ("k", "2.6")
//	kimi-k2-5      → ("k", "2.5")   (hyphen as dot, across two tokens)
//	kimi-k2-0711   → ("k", "2")     (trailing date dropped; not a version)
//	kimi-k2:1t     → ("k", "2")     (":1t" context discriminator stripped)
//	minimax-m1-80k → ("m", "1")     ("80k" context window ignored)
//
// ⚠ TIER INTERACTION (MUST-SURFACE, SLICE-8 d): when a NON-numeric, non-modifier,
// non-size/context tier token follows the series token (kimi-k2-instruct,
// kimi-k2p6-turbo, mimo-v2.5-pro, minimax-m2.5-fast), the canonical placement of
// the tier is AMBIGUOUS (variant is a single field). This was surfaced to the
// supervisor for a ruling (recommended Option A: compound "<letter>-<tier>"). Until
// ratified, this function DECLINES those cases (returns ok=false) and leaves the
// current decomposition unchanged — it never picks a tier placement unilaterally.
// Modifiers (thinking/vision) are stripped from the ID BEFORE this runs, so they do
// not trigger the tier decline.
// seriesBaseFamily recovers the base series family when the empty-raw inference
// swallowed the series token into a COMPOUND family token. e.g. "kimi-k2" (from
// "kimi-k2-0905") → "kimi". It returns (base, true) only when family is exactly
// "<base>-<letter><digit>…" where base carries a series_letter that matches the
// glued token's leading letter — keeping the recovery narrowly scoped to the three
// series families (kimi/minimax/mimo); every other family is left untouched.
// This is the family-side half of the SLICE-8 (d) normalization (so kimi-k2-0711
// and kimi-k2-0905 resolve to family "kimi", per CLARIFICATION-5).
func seriesBaseFamily(pd *parseData, family Family) (Family, bool) {
	if pd == nil {
		return "", false
	}
	fs := strings.ToLower(string(family))
	idx := strings.IndexByte(fs, '-')
	if idx <= 0 {
		return "", false
	}
	base, tok := fs[:idx], fs[idx+1:]
	info, has := pd.families[Family(base)]
	if !has || info.SeriesLetter == "" {
		return "", false
	}
	// tok must be exactly the series token "<letter><number>" (no further hyphens).
	if strings.IndexByte(tok, '-') >= 0 {
		return "", false
	}
	if len(tok) < 2 || tok[0] != info.SeriesLetter[0] {
		return "", false
	}
	if _, _, okv := parseSeriesNumber(tok[1:]); !okv {
		return "", false
	}
	return Family(base), true
}

// --------------------------------------------------------------------------
// SLICE-11 (rc2): family OVER-CAPTURE reduction (Option B / CLARIFICATION-9)
// --------------------------------------------------------------------------

// isFamilyResidueToken reports whether tok (a single lowercase token TRAILING the
// short base of a COMPOUND, over-captured family string) is decomposition RESIDUE —
// a member / variant / version / param-size / date / context-window / modifier token
// that legitimately belongs to a model's variant/version split, NOT a distinct family.
//
// reduceOverCapturedFamily collapses a compound family to its short base ONLY when
// EVERY trailing token is residue. A single UNRECOGNISED token blocks the reduction
// (the compound is left intact as an HONEST residual) — the reducer never guesses that
// an unknown token is "just noise", which is what keeps it from over-reducing a
// genuinely-compound or genuinely-mislabelled family (e.g. ministral, lfm, intellect,
// a leaked vendor namespace, or a community finetune slug).
func isFamilyResidueToken(pd *parseData, base Family, tok string) bool {
	if pd == nil {
		return false
	}
	if tok == "" {
		return true
	}
	if info, ok := pd.families[Family(strings.ToLower(string(base)))]; ok {
		// Curated member of the short base (families.json members list).
		for _, m := range info.Members {
			if tok == m {
				return true
			}
		}
		// Series-letter token ("k2", "m1", "v2") for a series family.
		if info.SeriesLetter != "" && len(tok) >= 2 && tok[0] == info.SeriesLetter[0] {
			if _, _, okv := parseSeriesNumber(tok[1:]); okv {
				return true
			}
		}
	}
	// Curated variant suffix (registered-family variant token, e.g. "flash", "mini").
	if _, ok := bareVariantSuffix(pd, tok); ok {
		return true
	}
	// Version-shaped (4o, 3.3, v4, r1, 4.1), param-size (70b, 30b-a3b, 8x22b),
	// date-shaped (0528, 250715), context-window (80k), or a known capability modifier
	// (thinking, vision, instruct, preview, …). All are variant/version residue.
	if isVersionShaped(tok) || isParamSizeToken(tok) || isDateShapedToken(tok) ||
		reContextWindow.MatchString(tok) || isKnownModifierToken(pd, tok) {
		return true
	}
	return false
}

// reduceOverCapturedFamily collapses an ID-seeded COMPOUND family to its registered
// SHORT base family (SLICE-11 Option B). It is the inverse of SLICE-9 Option-A's
// family-PRESERVE workaround: where the empty-raw ID-path OVER-captures the family
// (claude-opus-4.1 → "claude-opus", gpt-4o-mini → "gpt-4o", deepseek-v4-pro →
// "deepseek-v4", llama-3.3-70b-instruct → "llama-3.3-70b", qwen3-vl-30b-a3b →
// "qwen3-vl-30b-a3b"), this reduces the family to the short registered base
// ("claude"/"gpt"/"deepseek"/"llama"/"qwen") so the empty-raw and raw-populated
// providers of the SAME model ID converge on the SAME short family + variant/version.
//
// It is CLOSED and conservative (never invents a reduction):
//   - A family explicitly SELF-MAPPED in family_overrides.json (text-embedding,
//     stable-diffusion, nano-banana, model-router, dall-e, mm-poly, big-pickle,
//     smart-turn) is a CURATED genuine compound → DECLINED (never reduced).
//   - A family with a REDUCING override (claude-opus→claude, gpt-mini→gpt, …) reduces
//     to that override's (family, variant).
//   - Otherwise the leading token must resolve (directly, or via bare-gen split such
//     as qwen3→qwen) to a REGISTERED short family (familyKeyKnown / allFamilies) AND
//     EVERY remaining token must be decomposition RESIDUE (isFamilyResidueToken). A
//     single unrecognised token DECLINES the reduction (honest residual).
//
// Returns (shortFamily, variantHint, ok). variantHint is the first member/suffix token
// recovered from the residue (used only when the caller has no better variant). When ok,
// the caller RE-DECOMPOSES variant/version against shortFamily so the empty-raw result
// matches the raw-populated providers' member-recovery output exactly.
func reduceOverCapturedFamily(pd *parseData, family Family) (Family, string, bool) {
	if pd == nil {
		return "", "", false
	}
	s := strings.ToLower(string(family))
	if s == "" {
		return "", "", false
	}

	// Override table: explicit curated decomposition, or a self-map = curated genuine
	// compound that must be PRESERVED.
	if ov, ok := pd.overrides[Family(s)]; ok {
		if ov.Family != Family(s) {
			return ov.Family, ov.Variant, true
		}
		return "", "", false
	}

	idx := strings.IndexByte(s, '-')
	if idx <= 0 {
		// Single-token family (short, or a glued form handled by splitBareGen/glued
		// helpers elsewhere) — nothing to reduce here.
		return "", "", false
	}
	leading, rest := s[:idx], s[idx+1:]

	// Resolve the short base: split a glued generation off the leading token
	// (qwen3 → qwen) when attested by the closed bare-gen predicate, else the leading
	// token directly.
	base := Family(leading)
	if b, _, ok := splitBareGen(pd, leading); ok {
		base = b
	}
	// The base MUST be a registered short family — never synthesize one.
	if !familyKeyKnown(string(base)) {
		return "", "", false
	}
	// Must be a STRICT reduction (a real shortening of the compound).
	if base == family || strings.EqualFold(string(base), s) {
		return "", "", false
	}

	// SLICE-14: the ENTIRE remainder is a single registered COMPOUND member of the base
	// (grok-code-fast → grok + member "code-fast"). Taking it as one product-name variant
	// unit avoids a per-token residue decision on "fast" (which would be a modifier-vs-variant
	// judgment reserved for S10) — the member is curated in families.json, so this is
	// attested product-name recovery, not a speculative split.
	if info, ok := pd.families[base]; ok {
		for _, m := range info.Members {
			if rest == m {
				return base, m, true
			}
		}
	}

	// Every remaining token must be decomposition residue; the first member/suffix
	// token becomes the variant hint.
	variantHint := ""
	for _, tok := range strings.Split(rest, "-") {
		// DECLINE when a CAPABILITY modifier (thinking/think/vision) is glued into the
		// over-captured family token. Reducing + re-decomposing such a family risks
		// dropping the capability modifier (single Modifier field), and the multi-modifier
		// cases (e.g. kimi-k2-thinking-turbo: thinking AND tier) are the KNOWN S8 residual
		// explicitly deferred to the SLICE-10 Modifier-LIST change. Leaving these compound
		// families intact keeps them an HONEST residual rather than silently losing the
		// capability — never masking the deferred multi-modifier limitation.
		if tok == "thinking" || tok == "think" || tok == "vision" {
			return "", "", false
		}
		if !isFamilyResidueToken(pd, base, tok) {
			return "", "", false
		}
		if variantHint == "" {
			if info, ok := pd.families[Family(strings.ToLower(string(base)))]; ok {
				for _, m := range info.Members {
					if tok == m {
						variantHint = m
						break
					}
				}
			}
			if variantHint == "" {
				if bare, ok := bareVariantSuffix(pd, tok); ok {
					variantHint = bare
				}
			}
		}
	}
	return base, variantHint, true
}

// seriesTierModifiers is the curated set of TIER/finetune tokens that, when they
// trail a letter-prefix series token, are promoted to the Modifier field
// (CLARIFICATION-6: tier→modifier, variant stays the pure series-letter). This set
// is consulted ONLY inside splitSeriesVariant — i.e. ONLY within the letter-prefix
// series decomposition (kimi/minimax/mimo). It is deliberately NOT added to the
// global modifiers.json: every one of these tokens is also a VARIANT for some
// NON-series family (turbo=qwen member + gpt-3.5-turbo; pro=gemini/mimo member;
// instruct=llama member; fast=grok-4-fast; flash=qwen/glm/gemini member), so a
// global promotion would wrongly reclassify gpt-5-mini / gemini-2.5-flash /
// qwen-turbo. Series scoping satisfies the CLARIFICATION-6 edge-(b) constraint.
var seriesTierModifiers = map[string]struct{}{
	"instruct":  {},
	"turbo":     {},
	"fast":      {},
	"highspeed": {},
	"lightning": {},
	"precision": {},
	"pro":       {},
	// "omni": the only remaining unknown-tier token that still caused a cross-provider
	// series divergence after the initial tier→modifier wiring (mimo-v2-omni). Confirmed
	// by the supervisor's residual-categorization analysis; added per CLARIFICATION-6.
	"omni": {},
}

// idFamilyOverrideEntry is the curated (family, variant, version) an exact model ID maps to.
type idFamilyOverrideEntry struct {
	family  Family
	variant string
	version string
}

// idFamilyOverrides is the rc3-L1 (USER-RATIFIED Impl-UAT bestiary-2gxu) CLOSED, exact-
// model-ID family-override map. It exists ONLY for the embedded-family case the leading-
// token decomposition cannot reach safely: the model ID leads with one family token but the
// canonical family is embedded later, and a general embedded-detect would regress sibling
// IDs (the Path-B trap). Each entry is keyed to ONE exact (lowercase) model ID, corrects
// the FAMILY to an attested allFamilies target, and preserves the pipeline-derived
// variant/version (no field dropped). It drives the sole remaining cross-provider divergence
// to 0 by converging the empty-raw and raw="nemotron" forms of the same id on one tuple.
//
//   - nvidia/llama-3.3-nemotron-super-49b-v1.5 (kilo raw="" over-captures family
//     "llama-3.3-nemotron-super-49b"; openrouter raw="nemotron" gives "nemotron") →
//     both converge on (nemotron, v1.5, 3.3). nemotron ∈ allFamilies + family_enforce.json.
var idFamilyOverrides = map[string]idFamilyOverrideEntry{
	"nvidia/llama-3.3-nemotron-super-49b-v1.5": {family: "nemotron", variant: "v1.5", version: "3.3"},
}

func isSeriesTierToken(tok string) bool {
	_, ok := seriesTierModifiers[tok]
	return ok
}

// splitSeriesVariant implements the SLICE-8 (d) series split. It returns
// (variant=series-letter, version, tierMod, ok). tierMod is the curated series-tier
// token promoted to a Modifier (CLARIFICATION-6) when EXACTLY ONE tier trails the
// series token AND no thinking/vision modifier also trails it; otherwise tierMod is
// "" (multi-modifier cases keep the series split but leave the tier uncaptured,
// pending the Modifier-multiplicity ruling — surfaced to the supervisor). A trailing
// token that is neither date/context/size, a known modifier, nor a curated tier is
// an UNKNOWN finetune token → DECLINE (ok=false), leaving current behavior intact.
func splitSeriesVariant(pd *parseData, family Family, idStr string) (variant, version, tierMod string, ok bool) {
	if pd == nil {
		return "", "", "", false
	}
	info, has := pd.families[Family(strings.ToLower(string(family)))]
	if !has || info.SeriesLetter == "" {
		return "", "", "", false
	}
	letter := info.SeriesLetter[0]

	toks := strings.Split(strings.ToLower(stripVendorNamespace(idStr)), "-")

	si := -1
	var ver string
	var hadSep bool
	for i, t := range toks {
		tt := t
		if j := strings.IndexByte(tt, ':'); j >= 0 {
			tt = tt[:j]
		}
		if len(tt) < 2 || tt[0] != letter {
			continue
		}
		if v, sep, okv := parseSeriesNumber(tt[1:]); okv {
			si, ver, hadSep = i, v, sep
			break
		}
	}
	if si < 0 {
		return "", "", "", false
	}

	rest := toks[si+1:]

	// Hyphen-as-dot merge: an immediately-following bare integer with no internal
	// separator on the series token (kimi-k2-5 → 2.5). A date-shaped following token
	// (kimi-k2-0711) is consumed but dropped from the version (it is a Date).
	idx := 0
	if len(rest) > 0 {
		n := rest[0]
		if j := strings.IndexByte(n, ':'); j >= 0 {
			n = n[:j]
		}
		if isAllDigits(n) {
			if isDateShapedToken(n) {
				idx = 1 // date suffix: consumed, version unchanged
			} else if !hadSep {
				ver = ver + "." + n
				idx = 1
			} else {
				idx = 1 // already dotted; extra numeric is noise, consume it
			}
		}
	}

	// Partition the trailing tokens (after the series token + numeric merge):
	//   - date / context-window / param-size      → skipped (not version, not tier)
	//   - known modifier (thinking/vision/preview) → counted (multi-modifier signal)
	//   - curated series-tier (instruct/turbo/…)   → tier candidate
	//   - anything else                            → UNKNOWN → DECLINE the series split
	var tiers []string
	knownMods := 0
	for _, t := range rest[idx:] {
		tt := t
		if j := strings.IndexByte(tt, ':'); j >= 0 {
			tt = tt[:j]
		}
		switch {
		case tt == "" || isDateShapedToken(tt) || reContextWindow.MatchString(tt) || isParamSizeToken(tt):
			// not a tier, not a modifier — ignored residual (context/size/date).
		case isKnownModifierToken(pd, tt):
			knownMods++
		case isSeriesTierToken(tt):
			tiers = append(tiers, tt)
		default:
			// Unknown finetune/provider token (omni/maas/original/fp4/tts/…): DECLINE
			// the series split entirely (leave current behavior; surfaced as residual).
			return "", "", "", false
		}
	}

	// CLARIFICATION-6: promote a single trailing tier to the Modifier ONLY when there
	// is exactly one tier and NO co-occurring thinking/vision modifier. Multi-modifier
	// cases (tier + thinking/vision, or 2+ tiers) keep the series split but leave the
	// tier uncaptured — the Modifier field is single-valued and the multiplicity rule
	// is pending (surfaced to the supervisor); never picked here unilaterally.
	if len(tiers) == 1 && knownMods == 0 {
		tierMod = tiers[0]
	}

	return string(letter), ver, tierMod, true
}

// idDrivenVersion is the consolidated ID-driven version extractor (SLICE-8 a/c).
// It extracts the canonical version for a resolved (family, variant) directly from
// the model ID so the SAME ID yields the SAME version regardless of provider
// raw_family. Resolution order:
//
//  1. Strip the vendor/path namespace (so "openai/gpt-4.1" and "gpt-4.1" agree).
//  2. SLICE-8 (c): if the trailing token glues a recognized modifier-letter to a
//     numeric version (e.g. "glm-4.5v"), split it — extract the version off the
//     cleaned token and surface the modifier (e.g. "vision"). The glued modifier is
//     only returned when the split actually yields a version (no false positives).
//  3. ExtractVersionBetweenFamilyAndVariant (inter-token + bare dot-version).
//  4. ExtractVersionFromID (family-prefix extractor; bare dot, "4o", param-size guard).
//
// Returns (version, gluedModifier). gluedModifier is "" unless step 2 fired.
// Date and param-size guards live inside the two extractors (single ownership).
func idDrivenVersion(id ModelID, family Family, variant string) (string, string) {
	if id == "" || family == "" {
		return "", ""
	}
	cleaned := stripVendorNamespace(string(id))

	// Step 2: glued version-modifier split (glm-4.5v). Only commit the modifier when
	// the trimmed form actually produces a version, so a stray trailing letter never
	// invents a modifier without a corresponding version.
	if trimmed, mod, ok := splitGluedVersionModifier(cleaned); ok {
		if v := extractVersionFromCleanID(trimmed, family, variant); v != "" {
			return v, mod
		}
	}

	return extractVersionFromCleanID(cleaned, family, variant), ""
}

// extractVersionFromCleanID runs the two ID-driven version extractors (between
// family/variant, then family-prefix) on an ALREADY vendor-stripped id string.
// Shared by idDrivenVersion's glued and non-glued paths.
func extractVersionFromCleanID(cleaned string, family Family, variant string) string {
	if v, _ := ExtractVersionBetweenFamilyAndVariant(ModelID(cleaned), family, variant); v != "" {
		return v
	}
	if v := ExtractVersionFromID(ModelID(cleaned), family); v != "" {
		return v
	}
	// SLICE-8 (a): the version may appear AFTER the variant in the ID (e.g.
	// "claude-opus-4.6-fast", "mistral-small-3.2-24b-..."). When raw_family is empty
	// the family alone ("claude"/"mistral") cannot reach the post-variant numeric, so
	// try the family+variant compound as the prefix — making the empty-raw path agree
	// with the raw-populated path (which uses raw="claude-opus" / "mistral-small").
	if variant != "" {
		compound := Family(firstToken(string(family)) + "-" + variant)
		if v := ExtractVersionFromID(ModelID(cleaned), compound); v != "" {
			return v
		}
	}
	return ""
}

// dotJoinStrippingDateSuffix converts a hyphen-separated digit string (as captured
// by the hyphen-version regex) into a dot-notation version, stripping any TRAILING
// date-shaped groups.
//
// RULE (SLICE-1-FIX-3): iterate groups left-to-right; stop (discard the current and
// all remaining groups) at the first date-shaped group (4-digit YYMM/MMDD or 6-digit
// YYMMDD). Dot-join the leading semantic-version groups only. Return "" when no
// leading non-date groups remain.
//
// Additionally, a two-group remainder that is entirely a MM-YYYY date (e.g. "08-2024")
// returns "" directly.
//
// Examples:
//
//	"4-5"      → "4.5"   (both groups are semantic)
//	"2603"     → ""      (single 4-digit date)
//	"4-0314"   → "4"     (leading "4" kept, trailing "0314" date stripped)
//	"1-6-250615" → "1.6" (leading "1-6" kept, trailing 6-digit "250615" stripped)
//	"08-2024"  → ""      (full MM-YYYY two-group remainder)
//	"1-6"      → "1.6"   (semantic version, no date)
func dotJoinStrippingDateSuffix(s string) string {
	// Fast-path: single token.
	if !strings.Contains(s, "-") {
		if isDateShapedToken(s) {
			return ""
		}
		return s
	}

	// Check if the whole string is a MM-YYYY two-group date.
	if isMMYYYYTwoGroup(s) {
		return ""
	}

	// Iterate groups left-to-right; stop at first date-shaped group.
	parts := strings.Split(s, "-")
	var keep []string
	for _, p := range parts {
		if isDateShapedToken(p) {
			break // stop: discard this and all subsequent groups
		}
		keep = append(keep, p)
	}
	return strings.Join(keep, ".")
}

// --------------------------------------------------------------------------
// trimOneTrailingModifier (R3c / Δ2′ — tentative only)
// --------------------------------------------------------------------------

// trimOneTrailingModifier removes exactly ONE trailing "-<mod>" token from s
// where mod is in pd.modifiers (longest-first). Returns s unchanged when no
// trailing modifier is found.
//
// IMPORTANT: this function is TENTATIVE and used ONLY inside
// InferFamilyFromIDWithVariant to expose a hidden date. The actual commit to
// the decomposition is always gated by the two guards in that caller (GUARD-1
// variant-guard and GUARD-2 passthrough-guard). trimOneTrailingModifier itself
// never commits anything — it only strips.
//
// Data lifecycle: pd.modifiers is populated via loadParseData/sync.Once from
// parse/data/modifiers.json (embedded FS). The list is sorted longest-first at
// load time so that "thinking" cannot be shadowed by "think". trimOneTrailingModifier
// relies on that ordering to strip the longest matching modifier. In the event
// loadParseData returns an error, trimOneTrailingModifier returns s unchanged
// (fail-closed, matching the existing ParseFamily behavior).
func trimOneTrailingModifier(s string) string {
	pd, err := loadParseData()
	if err != nil {
		return s
	}
	for _, mod := range pd.modifiers {
		suffix := "-" + mod
		if strings.HasSuffix(s, suffix) {
			return s[:len(s)-len(suffix)]
		}
	}
	return s
}

// --------------------------------------------------------------------------
// ExtractVersionBetweenFamilyAndVariant (R1)
// --------------------------------------------------------------------------

// ExtractVersionBetweenFamilyAndVariant extracts the numeric version component
// from a model ID that embeds version digits BETWEEN the normalized family prefix
// and the variant name. This handles the common case where raw_family does not
// embed the version but the model ID does:
//
//	id="claude-3-5-haiku-20241022", family="claude", variant="haiku" → "3.5"
//	id="claude-3.5-haiku",          family="claude", variant="haiku" → "3.5"
//	id="gpt-5-mini",                family="gpt",    variant="mini"  → "5"
//
// Returns (version, residual) where:
//   - version is the dot-joined leading numeric tokens found between family and variant.
//   - residual contains any tokens between the version and variant that are neither
//     numeric nor the variant first-token (honest-audit signal per R2).
//
// N-M equivalence: hyphen-separated pure-digit tokens are dot-joined so that
// "3-5" → "3.5" and "4-6" → "4.6". This brings parity with ParseFamilyWithVersion.
//
// Parity contract: ExtractVersionBetweenFamilyAndVariant fires if and only if
// detectVersionDigitsInID (parse.go) would also fire on the same (id, family, variant).
// This invariant is enforced by TestExtractVersionBetweenFamilyAndVariant_Parity
// (parse_internal_test.go), which asserts: detector fires ⟺ extractor returns
// non-empty version OR non-empty residual.
//
// Algorithm:
//  1. Normalize the family to a canonical single-word form (first alphabetic token
//     of family for multi-token families like "claude-opus" → "claude").
//  2. Strip "<normalizedFamily>-" prefix from the ID. Return ("","") when absent.
//  3. Strip any trailing compact/dash date from the remainder.
//  4. Tokenize on "-". Collect leading purely-numeric tokens up to the first
//     variant-first-token (or end of tokens if variant is empty).
//  5. Dot-join the numeric tokens → version. Tokens after the numeric run that
//     are neither the variant first-token nor purely-numeric are residual.
func ExtractVersionBetweenFamilyAndVariant(id ModelID, family Family, variant string) (version string, residual []string) {
	if id == "" || family == "" {
		return "", nil
	}

	// SLICE-8 (a): case-fold so mixed-case IDs ("GLM-5", "MiniMax-M2") agree with
	// their lowercase siblings; the resolved family/variant are already lowercase.
	idStr := strings.ToLower(lastPathSegment(string(id)))

	// Normalize family: use only the first hyphen-token so that "claude-opus" → "claude".
	// SLICE-1-FIX-4: full-prefix-first was reverted (FIX-2 B1 over-stripped compound families
	// like "gemini-2.5-flash-image-generation" by matching "gemini-2.5-flash-" as prefix and
	// leaving "image-generation" as remainder with no numeric leading token, losing version "2.5").
	// Proper additive handling for compound families (text-embedding, etc.) is deferred to rc2.
	normalizedFamily := strings.ToLower(firstToken(string(family)))
	prefix := normalizedFamily + "-"
	if !strings.HasPrefix(idStr, prefix) {
		return "", nil
	}

	// Strip the family prefix.
	remainder := idStr[len(prefix):]
	if remainder == "" {
		return "", nil
	}

	// Strip trailing date so date tokens are not included in version or residual.
	remainder = stripTrailingDate(remainder)
	if remainder == "" {
		return "", nil
	}

	// SLICE-8 (a): bare dot-version remainder with NO trailing hyphen segment.
	// e.g. "gpt-4.1" (family "gpt") → remainder "4.1"; "glm-4.6" → "4.6". Without
	// this, the tokenizer below treats "4.1" as a non-numeric residual (it has a dot)
	// and drops the version, so the SAME ID extracts a version on the empty-raw path
	// (which routes through ParseFamilyWithVersion's dot-version fallback) but NOT on
	// the raw-populated path — a cross-provider version-presence divergence.
	if reBareVersion.MatchString(remainder) {
		return remainder, nil
	}

	// Handle the case where the ID uses a dot-version before the first hyphen segment.
	// e.g. "3.5-haiku" — the first token "3.5" is a dot-version, not a hyphen-digit.
	// Extract it by checking for a bare dot-version segment at the start of the remainder.
	if idx := strings.Index(remainder, "-"); idx >= 0 {
		lead := remainder[:idx]
		if reBareVersion.MatchString(lead) {
			// Dot-version token at the start: e.g. "3.5-haiku" → lead="3.5"
			// Treat this as the version (no residual within this context — the suffix
			// after the variant may have a date which is already stripped).
			return lead, nil
		}
	}

	tokens := strings.Split(remainder, "-")

	// Determine the first token of the variant (for boundary detection).
	variantFirst := ""
	if variant != "" {
		variantFirst = firstToken(variant)
	}

	// Build a set of "accounted" tokens (family first-token is already stripped;
	// variant tokens will be accounted during the scan).
	variantTokens := make(map[string]struct{})
	if variant != "" {
		for _, vt := range strings.Split(variant, "-") {
			if vt != "" {
				variantTokens[vt] = struct{}{}
			}
		}
	}

	// Collect leading purely-numeric tokens between family and variant.
	// SLICE-1-FIX-3: also reject 6-digit YYMMDD tokens (is6DigitYYMMDD) in addition
	// to the existing 4-digit guard (isYYMMDateToken via isVersionToken). Stop at the
	// first date-shaped token so trailing date groups are not included in the version.
	var numericTokens []string
	variantStart := -1
	for i, tok := range tokens {
		if tok == "" {
			continue
		}
		// Stop at the variant boundary.
		if variantFirst != "" && tok == variantFirst {
			variantStart = i
			break
		}
		if isVersionToken(tok) && !isDateShapedToken(tok) {
			numericTokens = append(numericTokens, tok)
		} else if !isVersionToken(tok) {
			// Non-numeric, non-variant token — treat it as residual if before variant.
			residual = append(residual, tok)
		} else {
			// Date-shaped token (6-digit YYMMDD or 4-digit, caught by isDateShapedToken):
			// stop collecting version tokens (dates go to Date via ExtractDate, not here).
			break
		}
	}

	if len(numericTokens) == 0 {
		return "", nil
	}

	version = strings.Join(numericTokens, ".")

	// Collect residual tokens AFTER the variant (e.g. "v1" in "nova-2-lite-v1").
	// These are tokens that are not date-like, not numeric version digits, not part of
	// the variant itself, and not known modifier/suffix tokens (which have their own
	// semantic meaning and are not "unaccounted").
	if variantStart >= 0 {
		// Build known-modifier lookup from pd.modifiers for fast checks.
		pd, pdErr := loadParseData()
		isKnownMod := func(tok string) bool {
			if pdErr != nil {
				return false
			}
			for _, mod := range pd.modifiers {
				if mod == tok {
					return true
				}
			}
			return false
		}

		// Skip variant tokens.
		afterVariant := variantStart
		for _, vt := range strings.Split(variant, "-") {
			if vt != "" && afterVariant < len(tokens) && tokens[afterVariant] == vt {
				afterVariant++
			}
		}
		// Any tokens remaining after the variant are residual IF they are not known
		// modifiers (which are semantically accounted for by the modifier extraction
		// pipeline) and not purely numeric.
		// SLICE-1-FIX-3: also skip 6-digit YYMMDD date tokens (they go to Date, not residual).
		for i := afterVariant; i < len(tokens); i++ {
			tok := tokens[i]
			if tok == "" {
				continue
			}
			// Skip purely-numeric tokens and date-shaped tokens.
			if isVersionToken(tok) && !isDateShapedToken(tok) {
				continue
			}
			// Skip date-shaped tokens (4-digit YYMM/MMDD and 6-digit YYMMDD).
			if isDateShapedToken(tok) {
				continue
			}
			// Skip known modifiers (they are extracted by ExtractModifier, not residual).
			if isKnownMod(tok) {
				continue
			}
			residual = append(residual, tok)
		}
	}

	return version, residual
}
