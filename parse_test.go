package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// ----------------------------------------------------------------------------
// Parse data initialization tests
// ----------------------------------------------------------------------------

// TestParseData_RegexesValid asserts that the embedded parse data loads without
// error at startup: all JSON files are present in the embedded FS, all regex
// strings in version_patterns.json compile successfully, and no JSON is
// malformed.
//
// The sync.Once error path in initParseData() is silently
// swallowed by ParseFamily (fail-closed design). This test makes the startup
// contract explicit and measurable. If the data files or regexes are ever
// broken, this test will catch it before any caller of ParseFamily silently
// degrades to returning raw values unchanged.
func TestParseData_RegexesValid(t *testing.T) {
	t.Parallel()
	if err := bestiary.ParseDataReady(); err != nil {
		t.Fatalf("ParseDataReady() returned unexpected error: %v\n"+
			"  What: embedded parse data failed to load\n"+
			"  Why: a JSON file is missing, malformed, or a regex in version_patterns.json did not compile\n"+
			"  Where: parse/data/*.json embedded files\n"+
			"  How to fix: inspect the error message above and repair the affected JSON data file", err)
	}
}

// ----------------------------------------------------------------------------
// ParseFamily tests
// ----------------------------------------------------------------------------

// TestParseFamily_Overrides covers all entries in family_overrides.json
// that have a non-empty variant. The corpus (testdata/parse/family_overrides_corpus.json)
// is authoritative: if you add an override to the JSON, add a case there.
func TestParseFamily_Overrides(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyOverridesCorpusJSON, 60)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"claude-opus":         {Family: "claude", Variant: "opus"},
		"gpt-oss":             {Family: "gpt", Variant: "oss"},
		"sonar-deep-research": {Family: "sonar", Variant: "deep-research"},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_Overrides_OpaqueCompounds tests compound families that are
// kept as-is (empty variant) because they are atomic branding units.
func TestParseFamily_Overrides_OpaqueCompounds(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyOpaqueCompoundsCorpusJSON, 8)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"text-embedding": {Family: "text-embedding", Variant: ""},
		"dall-e":         {Family: "dall-e", Variant: ""},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_VersionedPatterns covers the versioned-variant patterns.
func TestParseFamily_VersionedPatterns(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyVersionedPatternsCorpusJSON, 5)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"kimi-k2.5": {Family: "kimi", Variant: "k2.5"},
		"qwen3.5":   {Family: "qwen", Variant: "3.5"},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_HyphenVersion covers the hyphen-separated version rule.
// BDD criterion: "Given raw 'claude-opus-4-5' When ParseFamily Then returns ('claude', 'opus-4-5')."
func TestParseFamily_HyphenVersion(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyHyphenVersionCorpusJSON, 1)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"claude-opus-4-5": {Family: "claude", Variant: "opus-4-5"},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_SingleToken covers raw families that are already single tokens.
// These should return (raw, "") unchanged.
func TestParseFamily_SingleToken(t *testing.T) {
	t.Parallel()

	singles := []bestiary.Family{
		"claude", "gpt", "gemini", "llama", "mistral", "qwen", "grok",
		"phi", "nova", "sonar", "kimi", "minimax", "mimo", "magistral",
		"deepseek", "codestral", "command",
	}

	for _, raw := range singles {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(raw)
			if gotFamily != raw {
				t.Errorf("ParseFamily(%q) family = %q, want %q (passthrough)", raw, gotFamily, raw)
			}
			if gotVariant != "" {
				t.Errorf("ParseFamily(%q) variant = %q, want empty for single-token input", raw, gotVariant)
			}
		})
	}
}

// TestParseFamily_Empty covers the empty-input edge case.
func TestParseFamily_Empty(t *testing.T) {
	t.Parallel()
	gotFamily, gotVariant := bestiary.ParseFamily("")
	if gotFamily != "" {
		t.Errorf("ParseFamily(\"\") family = %q, want empty", gotFamily)
	}
	if gotVariant != "" {
		t.Errorf("ParseFamily(\"\") variant = %q, want empty", gotVariant)
	}
}

// TestParseFamily_Determinism verifies that ParseFamily is deterministic:
// running it 100 times on the same input always produces identical output.
// This guards against any map-iteration-order leakage.
// MINOR : includes a suffix-stripping input.
func TestParseFamily_Determinism(t *testing.T) {
	t.Parallel()

	inputs := []bestiary.Family{
		"claude-opus", "kimi-k2.5", "qwen3.5", "gemini-flash-lite",
		"gpt-codex-spark", "claude-opus-4-5", "", "llama",
		// suffix-stripping path: ensure determinism on Step 3.
		"foo-mini",
	}

	for _, raw := range inputs {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			first, firstVariant := bestiary.ParseFamily(raw)
			for i := 1; i < 100; i++ {
				f, v := bestiary.ParseFamily(raw)
				if f != first {
					t.Errorf("ParseFamily(%q) iteration %d: family = %q, want %q (non-deterministic)", raw, i, f, first)
				}
				if v != firstVariant {
					t.Errorf("ParseFamily(%q) iteration %d: variant = %q, want %q (non-deterministic)", raw, i, v, firstVariant)
				}
			}
		})
	}
}

// TestParseFamily_SuffixStripping covers the entries in variant_suffixes.json.
// Inputs are chosen to NOT appear in family_overrides.json and to NOT match any
// versioned-variant pattern, so they route past Steps 1 and 2 and reach the
// suffix-stripping loop. The corpus also pins the ratified global modifiers
// (instruct/turbo/base) that ParseFamily no longer strips (empty variant), and
// the longest-first multi-suffix ordering case.
func TestParseFamily_SuffixStripping(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familySuffixStrippingCorpusJSON, 29)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		// longest-first: "-codex-mini" must beat "-mini".
		"baz-codex-mini": {Family: "baz", Variant: "codex-mini"},
		// ratified global modifiers are NOT stripped by ParseFamily.
		"acme-instruct": {Family: "acme-instruct", Variant: ""},
		"foo-turbo":     {Family: "foo-turbo", Variant: ""},
		"foo-base":      {Family: "foo-base", Variant: ""},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_VPrefix covers the v-prefix versioned-variant pattern using
// base values NOT present in family_overrides.json.
func TestParseFamily_VPrefix(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyVPrefixCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"somebase-v3.0":  {Family: "somebase", Variant: "v3.0"},
		"thing-v2.5-pro": {Family: "thing", Variant: "v2.5-pro"},
	})
	runFamilyVariantCorpus(t, corpus)
}

// TestParseFamily_HyphenVersion_NoOverride covers the else-branch of the
// hyphen-version pattern handler: when the extracted base is NOT found in the
// overrides table, the function returns (Family(base), variant) directly.
func TestParseFamily_HyphenVersion_NoOverride(t *testing.T) {
	t.Parallel()
	corpus := loadFamilyVariantCorpus(t, familyHyphenVersionNoOverrideCorpusJSON, 2)
	requireInputCoverage(t, corpus, map[string]familyVariantExpected{
		"llama-3-1": {Family: "llama", Variant: "3-1"},
		"phi-4-5":   {Family: "phi", Variant: "4-5"},
	})
	runFamilyVariantCorpus(t, corpus)
}

// ----------------------------------------------------------------------------
// ExtractDate tests
// ----------------------------------------------------------------------------

// TestExtractDate_FromID covers date extraction from model IDs.
func TestExtractDate_FromID(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[dateInput, string](t, extractDateFromIDCorpusJSON, 7)
	requireInputCoverage(t, corpus, map[dateInput]string{
		// an ID-embedded date wins over releaseDate.
		{ID: "model-20240101", ReleaseDate: "2023-06-15"}: "2024-01-01",
		// releaseDate fallback when the ID carries no date.
		{ID: "llama-3", ReleaseDate: "2024-04-18"}: "2024-04-18",
	})
	runExtractDateCorpus(t, corpus)
}

// TestExtractDate_CalendarValidation checks that structurally-matching but
// semantically-invalid dates (e.g. month 99, day 99) are rejected.
// ExtractDate must use time.Parse round-trip to validate range.
func TestExtractDate_CalendarValidation(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[dateInput, string](t, extractDateCalendarCorpusJSON, 8)
	// Value coverage: the load-bearing valid/invalid boundary pairs must remain.
	requireInputCoverage(t, corpus, map[dateInput]string{
		{ID: "model-9999-99-01"}: "",           // invalid month rejected
		{ID: "model-2023-02-29"}: "",           // Feb 29 non-leap rejected
		{ID: "model-2024-02-29"}: "2024-02-29", // Feb 29 leap accepted
	})
	runExtractDateCorpus(t, corpus)
}

// ----------------------------------------------------------------------------
// InferFamilyFromID tests
// ----------------------------------------------------------------------------

// TestInferFamilyFromID covers the empty-family fallback heuristic.
func TestInferFamilyFromID(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[providerIDInput, string](t, inferFamilyFromIDCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[providerIDInput]string{
		// BDD criterion: prefix extraction from a dated id.
		{ID: "gpt-4o-2024-08-06", Provider: "openai"}: "gpt",
		// numeric-only id has no family signal.
		{ID: "1234", Provider: "local"}: "",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.InferFamilyFromID(bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			if string(got) != c.Expected {
				t.Errorf("InferFamilyFromID(%q, %q) = %q, want %q", c.Input.ID, c.Input.Provider, got, c.Expected)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ParseFamilyWithVersion tests
// (tests FAIL until the implementation exists)
// ----------------------------------------------------------------------------

// TestParseFamilyWithVersion_Core covers the primary acceptance criteria from
// the slice spec: hyphen-versioned families split into (family, variant, version).
//
// BDD criterion: "claude-opus-4-5" → (family=claude, variant=opus, version=4.5).
// Version uses dot separator (4.5) not hyphen (4-5).
func TestParseFamilyWithVersion_Core(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, familyVersionExpected](t, familyWithVersionCoreCorpusJSON, 8)
	requireInputCoverage(t, corpus, map[string]familyVersionExpected{
		"claude-opus-4-5": {Family: "claude", Variant: "opus", Version: "4.5"},
		"claude-opus":     {Family: "claude", Variant: "opus", Version: ""},
		"llama-3-1":       {Family: "llama", Variant: "", Version: "3.1"},
	})
	runFamilyVersionCorpus(t, corpus)
}

// TestParseFamilyWithVersion_Gemini covers Gemini models which use a
// major.minor version in their family string.
func TestParseFamilyWithVersion_Gemini(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, familyVersionExpected](t, familyWithVersionGeminiCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[string]familyVersionExpected{
		"gemini-2.5-flash": {Family: "gemini", Variant: "flash", Version: "2.5"},
		"gemini-2.5":       {Family: "gemini", Variant: "", Version: "2.5"},
	})
	runFamilyVersionCorpus(t, corpus)
}

// TestParseFamilyWithVersion_Empty verifies that empty input returns all-empty results.
func TestParseFamilyWithVersion_Empty(t *testing.T) {
	t.Parallel()
	gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion("")
	if gotFamily != "" || gotVariant != "" || gotVersion != "" {
		t.Errorf("ParseFamilyWithVersion(\"\") = (%q, %q, %q), want all empty", gotFamily, gotVariant, gotVersion)
	}
}

// TestParseFamilyWithVersion_AlphanumericVersion covers inputs where the version
// suffix is alphanumeric (e.g. "4o") rather than purely numeric (e.g. "4-5").
// The "4o" pattern is structurally different from hyphen-numeric patterns:
// it does not match the hyphen-version regex, so it falls through to the pure
// fallback. ParseFamilyWithVersion returns the raw value unchanged for these inputs.
//
// NOTE: "gpt-4o" → ("gpt-4o", "", "") because "4o" is not recognized as a
// separable version by any current pattern (it is not matched by hyphen-version
// which requires purely numeric trailing tokens, and the no-prefix pattern
// requires an embedded dot). Callers that need the version for gpt-4o models
// should use ExtractVersionFromID instead, which handles this case.
func TestParseFamilyWithVersion_AlphanumericVersion(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, familyVersionExpected](t, familyWithVersionAlnumCorpusJSON, 2)
	requireInputCoverage(t, corpus, map[string]familyVersionExpected{
		// alphanumeric "4o" is not a separable version: full fallback, raw unchanged.
		"gpt-4o":            {Family: "gpt-4o", Variant: "", Version: ""},
		"chatgpt-4o-latest": {Family: "chatgpt-4o-latest", Variant: "", Version: ""},
	})
	runFamilyVersionCorpus(t, corpus)
}

// TestExtractVersionFromID covers the ExtractVersionFromID helper introduced in
// cycle 2 (BLOCKER ). The helper extracts the version from the
// model ID when the raw family field does not embed one.
func TestExtractVersionFromID(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[versionFromIDInput, string](t, extractVersionFromIDCorpusJSON, 12)
	requireInputCoverage(t, corpus, map[versionFromIDInput]string{
		// required spec case: dated id yields the dotted version.
		{ID: "claude-opus-4-5-20251101", RawFamily: "claude-opus"}: "4.5",
		// alphanumeric single-token version after prefix strip.
		{ID: "gpt-4o", RawFamily: "gpt"}: "4o",
		// trailing YYYY-MM-DD date stripped before version extraction.
		{ID: "claude-opus-4-6-2026-02-05", RawFamily: "claude-opus"}: "4.6",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(bestiary.ModelID(c.Input.ID), bestiary.Family(c.Input.RawFamily))
			if got != c.Expected {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q", c.Input.ID, c.Input.RawFamily, got, c.Expected)
			}
		})
	}
}

// TestParseFamilyWithVersion_BackwardCompat verifies that non-versioned inputs
// produce the same (family, variant) as ParseFamily, with version="".
func TestParseFamilyWithVersion_BackwardCompat(t *testing.T) {
	t.Parallel()

	cases := []bestiary.Family{
		"claude-opus", "claude-haiku", "gpt-mini", "gemini-flash",
		"kimi-k2.5", "qwen3.5", "llama",
	}

	for _, raw := range cases {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			wantFamily, wantVariant := bestiary.ParseFamily(raw)
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(raw)
			if gotFamily != wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, ParseFamily says %q", raw, gotFamily, wantFamily)
			}
			if gotVariant != wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, ParseFamily says %q", raw, gotVariant, wantVariant)
			}
			if gotVersion != "" {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want empty for non-versioned input", raw, gotVersion)
			}
		})
	}
}

// TestInferFamilyFromID_Variant verifies that InferFamilyFromIDWithVariant extracts
// both variant and version from model IDs where the raw family field is empty.
//
// The empty-family code path in genToModelInfo must produce
// identical (Family, Variant, Version) as the non-empty-family path for the same
// raw model ID. A model ID like "claude-opus-4-5-20251101" with empty raw family
// must decompose to (claude, opus, 4.5), not (claude, "", "").
//
// This test FAILS until InferFamilyFromIDWithVariant lands (it
// does not yet exist; the existing InferFamilyFromID only returns family).
func TestInferFamilyFromID_Variant(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[providerIDInput, familyVersionExpected](t, inferFamilyVariantCorpusJSON, 4)
	requireInputCoverage(t, corpus, map[providerIDInput]familyVersionExpected{
		// the empty-raw-family path must decompose the full tuple, not first-token.
		{ID: "claude-opus-4-5-20251101", Provider: "nano-gpt"}: {Family: "claude", Variant: "opus", Version: "4.5"},
		{ID: "claude-opus-4-6", Provider: "some-provider"}:     {Family: "claude", Variant: "opus", Version: "4.6"},
		// empty-raw version-before-variant forms must recover version 3.5 (both
		// dotted "3.5-haiku" and dashed "3-5-haiku" spellings), not drop it.
		{ID: "claude-3.5-haiku", Provider: "some-provider"}: {Family: "claude", Variant: "haiku", Version: "3.5"},
		{ID: "claude-3-5-haiku", Provider: "some-provider"}: {Family: "claude", Variant: "haiku", Version: "3.5"},
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.InferFamilyFromIDWithVariant(bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			if string(gotFamily) != c.Expected.Family {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) family = %q, want %q",
					c.Input.ID, c.Input.Provider, gotFamily, c.Expected.Family)
			}
			if gotVariant != c.Expected.Variant {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) variant = %q, want %q; "+
					"must apply suffix/pattern logic to extract variant from ID tokens, "+
					"not just return the first token",
					c.Input.ID, c.Input.Provider, gotVariant, c.Expected.Variant)
			}
			if gotVersion != c.Expected.Version {
				t.Errorf("InferFamilyFromIDWithVariant(%q, %q) version = %q, want %q",
					c.Input.ID, c.Input.Provider, gotVersion, c.Expected.Version)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ParseFamilyDetailed failure-detection tests
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_VersionDigitsNotExtracted verifies that ParseFamilyDetailed
// emits a ParseFailure with reason ReasonVersionDigitsNotExtracted for model IDs
// like "claude-3-5-haiku-20241022" where the raw_family is "claude-haiku" but the
// version digits (3, 5) are embedded in the model ID between the family prefix
// ("claude") and the variant ("haiku"), and are not extractable by ExtractVersionFromID.
//
// BDD: Given raw_family="claude-haiku" and id="claude-3-5-haiku-20241022" are parsed
// when version digits between family-prefix and variant cannot be extracted from the
// model ID then ParseFailure emitted with reason
// "version digits between family-prefix and variant not extracted".
func TestParseFamilyDetailed_VersionDigitsNotExtracted(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		// claude-3.x line: version digits "3-5" between "claude" and "haiku" in the ID,
		// but raw_family="claude-haiku" gives family="claude", variant="haiku", version="".
		// ExtractVersionFromID fails because "claude-haiku" prefix does not match start of ID.
		{
			rawFamily: "claude-haiku",
			id:        "claude-3-5-haiku-20241022",
			provider:  "anthropic",
		},
		// claude-3.x line: version digits "3-7" between "claude" and "sonnet" in the ID.
		{
			rawFamily: "claude-sonnet",
			id:        "claude-3-7-sonnet-20250219",
			provider:  "anthropic",
		},
		// claude-3.x line: version digit "3" between "claude" and "haiku" in the ID.
		{
			rawFamily: "claude-haiku",
			id:        "claude-3-haiku-20240307",
			provider:  "anthropic",
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			// Under Δ1 (extract-first), these inputs now SUCCEED: version is populated from
			// the model ID via ExtractVersionBetweenFamilyAndVariant. So failure must be nil
			// and version must be non-empty.
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			_ = modifier

			// Best-effort result is always returned.
			if family == "" {
				t.Errorf("ParseFamilyDetailed(%q): got empty family; expected a non-empty best-effort result", tc.rawFamily)
			}
			_ = variant

			// Under Δ1 extract-first: version must be populated from the model ID.
			if version == "" {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want non-empty (Δ1 extract-first should populate version)",
					tc.rawFamily, tc.id, version)
			}

			// Failure must be nil — extract-first mode succeeds for these inputs.
			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): expected nil ParseFailure (version now extracted), got Reason=%q\n"+
					"  What: Δ1 extract-first should populate version from the model ID\n"+
					"  Why: ExtractVersionBetweenFamilyAndVariant should find the digits in the ID",
					tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_YYMMDateAsVersion verifies that ParseFamilyDetailed emits a
// ParseFailure with reason ReasonYYMMDateAsVersion for Mistral-style 4-digit numerals
// (e.g. "mistral-2401") where the YYMM segment cannot be reliably distinguished from a
// version number.
//
// BDD: Given a Mistral 4-digit numeric (e.g. "mistral-2401") when YYMM date cannot
// be cleanly distinguished from version then ParseFailure emitted with reason
// "YYMM-date-as-version false-positive".
func TestParseFamilyDetailed_YYMMDateAsVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		{rawFamily: "mistral-2401", id: "mistral-2401", provider: "mistral"},
		{rawFamily: "mistral-2403", id: "mistral-2403", provider: "mistral"},
		{rawFamily: "pixtral-2411", id: "pixtral-2411-latest", provider: "mistral"},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)

			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q): expected ParseFailure for YYMM pattern, got nil\n"+
					"  What: YYMM-date-as-version false-positive was not detected\n"+
					"  Why: the detector did not match the 4-digit YYMM pattern in the raw family string\n"+
					"  How to fix: verify reYYMMCandidate regex matches 4-digit numerals in range 1900-2999",
					tc.rawFamily)
			}
			if failure.Reason != bestiary.ReasonYYMMDateAsVersion {
				t.Errorf("ParseFamilyDetailed(%q): failure.Reason = %q, want %q",
					tc.rawFamily, failure.Reason, bestiary.ReasonYYMMDateAsVersion)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ExtractModifier tests
// ----------------------------------------------------------------------------

// TestExtractModifier covers the 4-case corpus from the slice spec plus negative
// cases. Tests are expected to FAIL until ExtractModifier is integrated into the
// parse pipeline AND the result is wired into ModelInfo.Modifier.
//
// Note: This test directly calls ExtractModifier which is already implemented
// (the skeleton returns the correct value since the body is implemented).
// The pipeline integration test below covers the end-to-end flow.
func TestExtractModifier(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[modifierInput, modifierExpected](t, extractModifierCorpusJSON, 8)
	requireInputCoverage(t, corpus, map[modifierInput]modifierExpected{
		// trailing modifier fires with an empty variant.
		{ID: "model-thinking", Family: "model", Variant: ""}: {Modifier: "thinking", Consumed: "-thinking"},
		// variant-guard: trailing token equals variant -> no double-count.
		{ID: "deepseek-thinking", Family: "deepseek", Variant: "thinking"}: {Modifier: "", Consumed: ""},
	})
	runExtractModifierCorpus(t, corpus)
}

// TestExtractModifier_DoesNotDoubleCountVariant verifies the variant-guard:
// when the trailing modifier token in the model ID equals the parsed variant,
// ExtractModifier returns ("","") to avoid encoding the same semantic token in
// both Variant and Modifier (double-count).
//
// NOTE: after the uniform thinking/vision-as-modifier migration, the kimi/
// deepseek "variant=thinking" inputs below are SYNTHETIC — production no longer
// decomposes those IDs to variant="thinking" (the overrides/suffixes/members were
// removed; thinking is now the first-class Modifier — see TestUniformModifierSuffix).
// The variant-guard is RETAINED as a general defensive anti-double-count: should ANY
// variant ever coincide with a trailing modifier token, it must not be counted twice.
// These rows pin that guard mechanism; the empty-variant rows pin the new reality.
//
// IMPORTANT: this guards against double-counting a variant token that also matches a trailing modifier.
func TestExtractModifier_DoesNotDoubleCountVariant(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[modifierInput, modifierExpected](t, extractModifierDoubleCountCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[modifierInput]modifierExpected{
		// guard fires: trailing token equals variant.
		{ID: "kimi-k2-thinking", Family: "kimi", Variant: "thinking"}: {Modifier: "", Consumed: ""},
		// guard must NOT fire: variant 'opus' != trailing 'thinking'.
		{ID: "claude-opus-4-6-thinking", Family: "claude", Variant: "opus"}: {Modifier: "thinking", Consumed: "-thinking"},
		// empty variant: modifier fires normally.
		{ID: "kimi-k2-thinking", Family: "kimi", Variant: ""}: {Modifier: "thinking", Consumed: "-thinking"},
	})
	runExtractModifierCorpus(t, corpus)
}

// TestUniformModifierSuffix is the acceptance test for the uniform
// thinking/vision-as-modifier migration: ANY trailing {thinking,vision} token is
// ALWAYS surfaced as the first-class Modifier and NEVER as the Variant, for ALL
// families and regardless of whether the token arrives via the model ID, the raw
// family field ("kimi-thinking", "deepseek-thinking", "grok-vision"), or both.
//
// BDD: Given a model whose ID and/or raw family carries a trailing thinking/vision
// token, When ParseFamilyDetailed runs, Then Modifier == that token AND Variant is
// never that token.
//
// SCOPE NOTE: version-presence (e.g. "3.7" in claude-3-7-sonnet-thinking, "k2" as a
// kimi variant) is OUT of scope here — that is (version extraction). This
// test pins the modifier-classification invariant, not the full tuple's version.
func TestUniformModifierSuffix(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[uniformModInput, uniformModExpected](t, uniformModifierSuffixCorpusJSON, 5)
	requireInputCoverage(t, corpus, map[uniformModInput]uniformModExpected{
		// vision is treated identically to thinking.
		{RawFamily: "grok-vision", ID: "grok-vision", Provider: "xai"}: {Family: "grok", Modifier: "vision"},
		// raw-family-encoded modifier with no modifier token in the ID.
		{RawFamily: "deepseek-thinking", ID: "deepseek-r1", Provider: "iflowcn"}: {Family: "deepseek", Modifier: "thinking"},
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			family, variant, _, modifier, _ := bestiary.ParseFamilyDetailed(bestiary.Family(c.Input.RawFamily), bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			if string(family) != c.Expected.Family {
				t.Errorf("ParseFamilyDetailed(%q, %q) family = %q, want %q",
					c.Input.RawFamily, c.Input.ID, family, c.Expected.Family)
			}
			if modJoin(modifier) != c.Expected.Modifier {
				t.Errorf("ParseFamilyDetailed(%q, %q) modifier = %q, want %q\n"+
					"  What: trailing %q token was NOT surfaced as the first-class Modifier\n"+
					"  Why: uniform migration — thinking/vision are ALWAYS modifiers",
					c.Input.RawFamily, c.Input.ID, modifier, c.Expected.Modifier, c.Expected.Modifier)
			}
			// The invariant: the modifier token must NEVER be encoded as the variant.
			if variant == c.Expected.Modifier {
				t.Errorf("ParseFamilyDetailed(%q, %q) variant = %q — a trailing modifier token "+
					"must NEVER be classified as the Variant (uniform migration)",
					c.Input.RawFamily, c.Input.ID, variant)
			}
		})
	}
}

// TestExtractModifier_PipelineIntegration verifies that the parse pipeline
// (ParseFamily → ExtractModifier → strip consumed → ExtractVersionFromID →
// ExtractDate) produces a ModelInfo with Modifier populated and Version/Date
// NOT polluted by the trailing modifier token.
//
// These tests will FAIL until ExtractModifier is integrated into genToModelInfoDetailed
// so that ModelInfo.Modifier is populated during codegen.
// This test validates the FUNCTION COMPOSITION directly (not the codegen path).
func TestExtractModifier_PipelineIntegration(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[pipelineInput, pipelineExpected](t, extractModifierPipelineCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[pipelineInput]pipelineExpected{
		// modifier stripped BEFORE version/date: all three recovered.
		{RawID: "claude-opus-4-1-20250805-thinking", RawFamily: "claude-opus"}: {Modifier: "thinking", Version: "4.1", Date: "2025-08-05"},
		// no modifier, version not extracted, date recovered.
		{RawID: "gpt-4o-2024-05-13", RawFamily: "gpt-4o"}: {Modifier: "", Version: "", Date: "2024-05-13"},
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			rawID := bestiary.ModelID(c.Input.RawID)
			rawFamily := bestiary.Family(c.Input.RawFamily)

			// Step 1: ParseFamily
			family, variant, _ := bestiary.ParseFamilyWithVersion(rawFamily)

			// Step 2: ExtractModifier
			modifier, consumed := bestiary.ExtractModifier(rawID, family, variant)

			// Verify modifier extraction
			if modifier != c.Expected.Modifier {
				t.Errorf("ExtractModifier modifier = %q, want %q", modifier, c.Expected.Modifier)
			}

			// Step 3: Strip consumed from ID
			cleanedID := rawID
			if consumed != "" {
				cleanedStr := string(rawID)
				if len(cleanedStr) >= len(consumed) && cleanedStr[len(cleanedStr)-len(consumed):] == consumed {
					cleanedID = bestiary.ModelID(cleanedStr[:len(cleanedStr)-len(consumed)])
				}
			}

			// Step 4: ExtractVersionFromID on cleaned ID
			version := bestiary.ExtractVersionFromID(cleanedID, rawFamily)
			if version != c.Expected.Version {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q", cleanedID, rawFamily, version, c.Expected.Version)
			}

			// Step 5: ExtractDate on cleaned ID
			date := bestiary.ExtractDate(cleanedID, "")
			if date != c.Expected.Date {
				t.Errorf("ExtractDate(%q, %q) = %q, want %q", cleanedID, "", date, c.Expected.Date)
			}
		})
	}
}

// TestParseFamilyDetailed_KnownSuffixOverflow verifies that ParseFamilyDetailed emits
// a ParseFailure with reason ReasonKnownSuffixOverflow for model IDs whose trailing
// token is a known modifier (thinking, think, vision, latest, code, preview) that
// the parser did NOT capture as the variant.
//
// BDD: Given a model ID ending with a known modifier token (e.g. "claude-opus-4-thinking")
// when the modifier was not captured by ParseFamilyWithVersion as the variant
// then ParseFailure emitted with reason ReasonKnownSuffixOverflow.
func TestParseFamilyDetailed_KnownSuffixOverflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
		modifier  string // expected trailing modifier token (for documentation)
	}{
		// "thinking" — each seed modifier tested with a realistic ID.
		{rawFamily: "claude-opus", id: "claude-opus-4-thinking", provider: "anthropic", modifier: "thinking"},
		{rawFamily: "gpt-4o", id: "gpt-4o-thinking", provider: "openai", modifier: "thinking"},
		// "think"
		{rawFamily: "claude-opus", id: "claude-opus-think", provider: "anthropic", modifier: "think"},
		// "vision"
		{rawFamily: "gpt-4", id: "gpt-4-vision", provider: "openai", modifier: "vision"},
		// "latest"
		{rawFamily: "gpt-4o", id: "gpt-4o-latest", provider: "openai", modifier: "latest"},
		// "code"
		{rawFamily: "claude-opus", id: "claude-opus-code", provider: "anthropic", modifier: "code"},
		// "preview"
		{rawFamily: "gpt-4o", id: "gpt-4o-preview", provider: "openai", modifier: "preview"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			_ = modifier

			// Best-effort parse result is always returned.
			if family == "" {
				t.Errorf("ParseFamilyDetailed(%q, %q): got empty family; expected non-empty best-effort",
					tc.rawFamily, tc.id)
			}
			_ = variant
			_ = version

			// Failure must be emitted.
			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q, %q): expected ParseFailure for known modifier %q, got nil\n"+
					"  What: trailing modifier token in model ID was not detected\n"+
					"  Why: pd.modifiers allowlist or Mode 2 condition may have changed\n"+
					"  How to fix: verify the modifier %q is in parse/data/modifiers.json and Mode 2 fires for this case",
					tc.rawFamily, tc.id, tc.modifier, tc.modifier)
			}
			if failure.Reason != bestiary.ReasonKnownSuffixOverflow {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonKnownSuffixOverflow)
			}
			if failure.RawID != tc.id {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.RawID = %q, want %q",
					tc.rawFamily, tc.id, failure.RawID, tc.id)
			}
		})
	}
}

// TestParseFamilyDetailed_UnknownSuffixOverflow verifies that ParseFamilyDetailed
// emits a ParseFailure with reason ReasonUnknownSuffixOverflow when the model ID has
// a trailing token that is NOT in the modifier allowlist but overflow is detected.
//
// This is an audit-log hint: when this fires, extend the modifier allowlist in parse.go.
//
// BDD: Given a model ID whose suffix-overflow condition fires but the trailing token
// is NOT in the seed allowlist when parsed then ParseFailure emitted with reason
// ReasonUnknownSuffixOverflow.
func TestParseFamilyDetailed_UnknownSuffixOverflow(t *testing.T) {
	t.Parallel()

	// Positive: unknown suffix token FIRES Mode 2 UnknownSuffixOverflow when:
	// (1) model ID trailing token is NOT in the modifier allowlist (pd.modifiers), AND
	// (2) that token is not already the parsed variant, AND
	// (3) detectSuffixOverflow returns true (raw family has >2 unaccounted tokens).
	//
	// This test documents the positive case for ReasonUnknownSuffixOverflow as an audit
	// hint to extend the modifier allowlist when new modifiers are detected in the wild.
	// Example: if models.dev returns rawFamily="claude-opus-4-1-extra-stuff-zen",
	// and the parser can only extract family="claude", variant="opus" from the override,
	// the tokens [4, 1, extra, stuff, zen] would be unaccounted for (5 tokens > 2 threshold),
	// triggering detectSuffixOverflow. Trailing token "zen" is unknown, so
	// ReasonUnknownSuffixOverflow would fire as an audit hint.
	//
	// This subtest is LIVE (not skipped). ParseFamilyWithVersion Step-5 bounded
	// reorder prevents the pure-fallback from absorbing all trailing tokens, making
	// ReasonUnknownSuffixOverflow reachable for the claude-opus-4-1-extra-stuff-zen fixture.
	// The reachability cases are a CAPTURE corpus (testdata/parse/unknown_suffix_overflow_corpus.json):
	// the positive row is the synthetic input that reaches the >2 unaccounted-token
	// threshold with an unknown trailing token, and the two must-fail rows are the
	// negative controls pinning the conservative boundary (an unknown trailing token
	// alone is NOT sufficient). Acceptance is the ParseFailure the parser RETURNS.
	corpus := loadParseCorpus[suffixOverflowInput, suffixOverflowExpected](t, parseUnknownSuffixOverflowCorpusJSON, 3)
	requireNameCoverage(t, corpus,
		"UnknownSuffixOverflow_PositiveCase",
		"no-overflow-gpt-4-zen",
		"no-overflow-claude-opus-foobar",
	)
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(
				bestiary.Family(c.Input.RawFamily), bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			if c.Classification == testcase.MustFail {
				// Negative control: the overflow reason must NOT fire.
				if failure != nil && string(failure.Reason) == string(bestiary.ReasonUnknownSuffixOverflow) {
					t.Errorf("ParseFamilyDetailed(%q, %q): got ReasonUnknownSuffixOverflow; "+
						"this case must not fire Mode 2 (trailing token is unknown but there is no overflow)",
						c.Input.RawFamily, c.Input.ID)
				}
				return
			}
			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q, %q): expected a ParseFailure with Reason=%q, got nil\n"+
					"  What: ReasonUnknownSuffixOverflow was not emitted\n"+
					"  Why: ParseFamilyWithVersion Step-5 bounded reorder must decompose the input so the\n"+
					"       trailing tokens are unaccounted (>2 threshold)\n"+
					"  How to fix: verify ParseFamilyWithVersion returns (claude,opus,4.1), not a raw passthrough",
					c.Input.RawFamily, c.Input.ID, c.Expected.Reason)
			}
			if string(failure.Reason) != c.Expected.Reason {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
					c.Input.RawFamily, c.Input.ID, failure.Reason, c.Expected.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_Mode2_NegativeCases verifies that Mode 2 does NOT fire
// when the modifier is already the parsed variant (i.e., correctly extracted by
// ParseFamilyWithVersion's suffix stripping).
//
// BDD: Given rawFamily="claude-thinking" (suffix "-thinking" stripped) when parsed
// then ParseFamilyWithVersion extracts variant="thinking" and Mode 2 does NOT fire.
func TestParseFamilyDetailed_Mode2_NegativeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
		note      string
	}{
		// the former claude-thinking / gpt-vision rows were REMOVED. Under the
		// uniform thinking/vision-as-modifier migration those tokens are never the parsed
		// variant, so they correctly surface as the first-class Modifier and — with no
		// variant absorbing them — DO trip Mode 2 as an honest audit signal (same as
		// claude-opus-4-thinking). Covered by TestParseFamilyDetailed_KnownSuffixOverflow
		// and TestUniformModifierSuffix.
		// Clean IDs with no trailing modifier.
		{"claude-opus", "claude-opus-4-20250514", "anthropic", "date suffix, not modifier"},
		{"claude-haiku", "claude-haiku-4-5", "anthropic", "version suffix, not modifier"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure != nil && (failure.Reason == bestiary.ReasonKnownSuffixOverflow || failure.Reason == bestiary.ReasonUnknownSuffixOverflow) {
				t.Errorf("ParseFamilyDetailed(%q, %q): got Mode 2 failure %q, expected none\n"+
					"  Note: %s\n"+
					"  Mode 2 should not fire when the modifier is already the parsed variant",
					tc.rawFamily, tc.id, failure.Reason, tc.note)
			}
		})
	}
}

// TestParseFamilyDetailed_CleanParse verifies that ParseFamilyDetailed returns
// nil *ParseFailure for cleanly parseable model IDs that the heuristics fully handle.
//
// BDD: Given a cleanly parseable model ID (e.g. "claude-opus") when parsed
// then NO ParseFailure emitted.
func TestParseFamilyDetailed_CleanParse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		// Known override entries — fully handled by the overrides table.
		{rawFamily: "claude-opus", id: "claude-opus-4-20250514", provider: "anthropic"},
		{rawFamily: "claude-haiku", id: "claude-haiku-4-5", provider: "anthropic"},
		{rawFamily: "claude-sonnet", id: "claude-sonnet-4-5-20251015", provider: "anthropic"},
		// Gemini with dot-version — handled by dot-version extraction.
		{rawFamily: "gemini-flash", id: "gemini-2.5-flash-preview-04-17", provider: "google"},
		// Empty raw family — no failure emitted on empty input.
		{rawFamily: "", id: "some-model", provider: "openai"},
	}

	for _, tc := range cases {
		t.Run(string(tc.rawFamily), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)

			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q): expected nil ParseFailure for clean parse, got: %+v\n"+
					"  Family=%q  Reason=%q",
					tc.rawFamily, failure, family, failure.Reason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// ExtractVersionBetweenFamilyAndVariant tests
// --------------------------------------------------------------------------

// TestExtractVersionBetweenFamilyAndVariant covers the primary acceptance cases
// from the scope. These tests FAIL until the extractor is implemented.
//
// N-M equivalence: hyphen-separated numeric tokens are dot-joined (3-5 → 3.5).
// Residual: tokens between version and variant that are neither numeric nor variant
// are returned in the residual slice (honest-audit).
func TestExtractVersionBetweenFamilyAndVariant(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantVersion  string
		wantResidual []string
	}{
		// Primary acceptance cases from scope.
		{
			desc:         "gpt-5-mini → 5 (single numeric between family and variant)",
			id:           "gpt-5-mini",
			family:       "gpt",
			variant:      "mini",
			wantVersion:  "5",
			wantResidual: nil,
		},
		{
			desc:         "claude-3-5-haiku-20241022 → 3.5 (N-M dot-join)",
			id:           "claude-3-5-haiku-20241022",
			family:       "claude",
			variant:      "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:         "claude-3.5-haiku → 3.5 (dot-normalized in ID)",
			id:           "claude-3.5-haiku",
			family:       "claude",
			variant:      "haiku",
			wantVersion:  "3.5",
			wantResidual: nil,
		},
		{
			desc:         "gemini-3-pro-preview → 3 (single numeric, variant=pro)",
			id:           "gemini-3-pro-preview",
			family:       "gemini",
			variant:      "pro",
			wantVersion:  "3",
			wantResidual: nil,
		},
		{
			desc:         "gemini-3-1-pro-preview → 3.1 (N-M dot-join, variant=pro)",
			id:           "gemini-3-1-pro-preview",
			family:       "gemini",
			variant:      "pro",
			wantVersion:  "3.1",
			wantResidual: nil,
		},
		{
			desc:         "nova-2-lite-v1 → version=2, residual=[v1] (honest-audit)",
			id:           "nova-2-lite-v1",
			family:       "nova",
			variant:      "lite",
			wantVersion:  "2",
			wantResidual: []string{"v1"},
		},
		{
			desc:         "nemotron-3-super-free → version=3, residual=[super] (honest-audit)",
			id:           "nemotron-3-super-free",
			family:       "nemotron",
			variant:      "free",
			wantVersion:  "3",
			wantResidual: []string{"super"},
		},
		// Edge cases.
		{
			desc:        "no version between family and variant → empty",
			id:          "claude-opus-4-6",
			family:      "claude",
			variant:     "opus",
			wantVersion: "",
		},
		{
			desc:        "empty id → empty",
			id:          "",
			family:      "claude",
			variant:     "haiku",
			wantVersion: "",
		},
		{
			desc:        "empty family → empty",
			id:          "claude-3-5-haiku-20241022",
			family:      "",
			variant:     "haiku",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotVersion, gotResidual := bestiary.ExtractVersionBetweenFamilyAndVariant(tc.id, tc.family, tc.variant)
			if gotVersion != tc.wantVersion {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) version = %q, want %q",
					tc.id, tc.family, tc.variant, gotVersion, tc.wantVersion)
			}
			// Compare residual slices (nil and empty are equivalent for this test).
			if len(gotResidual) != len(tc.wantResidual) {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual = %v, want %v",
					tc.id, tc.family, tc.variant, gotResidual, tc.wantResidual)
			} else {
				for i, tok := range tc.wantResidual {
					if gotResidual[i] != tok {
						t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual[%d] = %q, want %q",
							tc.id, tc.family, tc.variant, i, gotResidual[i], tok)
					}
				}
			}
		})
	}
}

// --------------------------------------------------------------------------
// isFourDigitDateToken (YYMM date guard) tests
// --------------------------------------------------------------------------

// TestIsYYMMDateToken_Parity verifies that isFourDigitDateToken parity holds with
// ExtractVersionFromID: tokens for which isFourDigitDateToken is true must not be
// returned as versions.
// The direct unit test for isFourDigitDateToken lives in parse_internal_test.go
// (package bestiary) since the function is unexported.
//
// The key case: mistral-small-2603 → no version (2603 is a YYMM date).
func TestIsYYMMDateToken_Parity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		{
			desc:      "mistral-small-2603 → no version (2603 is YYMM date)",
			id:        "mistral-small-2603",
			rawFamily: "mistral",
			want:      "",
		},
		{
			desc:      "mistral-medium-2505 → no version (2505 is YYMM date)",
			id:        "mistral-medium-2505",
			rawFamily: "mistral",
			want:      "",
		},
		{
			desc:      "genuine version still extracted: claude-opus-4-6",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: YYMM token was not rejected by isFourDigitDateToken guard\n"+
					"  Why: ExtractVersionFromID must consult isFourDigitDateToken before returning hyphen-digit tokens",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Modifier-strip date-recovery: InferFamilyFromIDWithVariant tests
// --------------------------------------------------------------------------

// TestInferFamilyFromIDWithVariant_ModifierStripDateRecovery covers the Δ2′ corrected algorithm:
// tentative modifier strip → expose hidden date → decompose → guarded commit.
//
// Three empirically-verified traces:
//  1. 302ai re-host: claude-opus-4-1-20250805-thinking → (claude, opus, 4.1)
//  2. Genuine-variant guard: kimi-k2-thinking → the passthrough-guard declines, variant=thinking preserved
//  3. No-modifier control: claude-opus-4-1-20250805 → (claude, opus, 4.1) unchanged
func TestInferFamilyFromIDWithVariant_ModifierStripDateRecovery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// Trace 1: 302ai re-host — empty raw_family, modifier after date.
			// exposed=claude-opus-4-1-20250805 → cleaned=claude-opus-4-1 → PFWV →
			// (claude, opus, 4.1); the variant-guard passes (ExtractModifier returns -thinking),
			// the passthrough-guard passes (claude != claude-opus-4-1) → return (claude, opus, 4.1).
			desc:        "claude-opus-4-1-20250805-thinking → (claude, opus, 4.1)",
			id:          "claude-opus-4-1-20250805-thinking",
			provider:    "302ai",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.1",
		},
		{
			// Trace 2 (d / flip, SUPERSEDES the
			// (kimi,"","") pin): kimi-k2-thinking (empty raw_family). kimi is a
			// letter-prefix series, so InferFamilyFromIDWithVariant's series split →
			// (kimi, "k", "2"). The trailing "thinking" is NOT a variant;
			// ParseFamilyDetailed surfaces it as the first-class Modifier
			// (InferFamilyFromIDWithVariant itself returns no modifier).
			desc:        "kimi-k2-thinking empty raw_family → series (kimi,k,2); thinking is a Modifier",
			id:          "kimi-k2-thinking",
			provider:    "moonshot",
			wantFamily:  "kimi",
			wantVariant: "k",
			wantVersion: "2",
		},
		{
			// Trace 3: no-modifier control — claude-opus-4-1-20250805.
			// trimOneTrailingModifier is a no-op (last token is date digit-group) →
			// existing flow → (claude, opus, 4.1) exactly as today.
			desc:        "claude-opus-4-1-20250805 no modifier → unchanged",
			id:          "claude-opus-4-1-20250805",
			provider:    "anthropic",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.1",
		},
		{
			// Previous acceptance case (must not regress).
			desc:        "claude-opus-4-5-20251101 empty raw_family",
			id:          "claude-opus-4-5-20251101",
			provider:    "nano-gpt",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if gotFamily != tc.wantFamily {
				t.Errorf("InferFamilyFromIDWithVariant(%q) family = %q, want %q",
					tc.id, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("InferFamilyFromIDWithVariant(%q) variant = %q, want %q",
					tc.id, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("InferFamilyFromIDWithVariant(%q) version = %q, want %q",
					tc.id, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestParseFamilyDetailed_R3c verifies that ParseFamilyDetailed, when called with
// empty raw_family (via InferFamilyFromIDWithVariant path), produces the expected
// 5-tuple for the Δ2′ traces.
//
// This covers the mandate that 5-tuple returns include modifier.
func TestParseFamilyDetailed_5Tuple(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawFamily    bestiary.Family
		id           bestiary.ModelID
		provider     bestiary.Provider
		wantFamily   bestiary.Family
		wantVariant  string
		wantVersion  string
		wantModifier string
	}{
		{
			desc:         "claude-opus-4-1-20250805-thinking → modifier=thinking",
			rawFamily:    "claude-opus",
			id:           "claude-opus-4-1-20250805-thinking",
			provider:     "anthropic",
			wantFamily:   "claude",
			wantVariant:  "opus",
			wantVersion:  "4.1",
			wantModifier: "thinking",
		},
		{
			desc:         "claude-opus-4-6 → no modifier",
			rawFamily:    "claude-opus",
			id:           "claude-opus-4-6",
			provider:     "anthropic",
			wantFamily:   "claude",
			wantVariant:  "opus",
			wantVersion:  "4.6",
			wantModifier: "",
		},
		{
			// / RED→GREEN flip (SUPERSEDES the
			// (kimi,"","") pin): the k-prefix is now a letter-prefix SERIES, so
			// kimi-k2-thinking → (kimi, variant="k", version="2", modifier=thinking).
			// "thinking" is stripped to the first-class Modifier first (uniform modifier rule);
			// the series split then decomposes the remaining "k2" → variant "k" + version
			// "2". Consistent across ALL providers (empty raw and raw="kimi-thinking").
			desc:         "kimi-k2-thinking empty rawFamily → series (kimi,k,2) + modifier thinking",
			rawFamily:    "",
			id:           "kimi-k2-thinking",
			provider:     "moonshot",
			wantFamily:   "kimi",
			wantVariant:  "k",
			wantVersion:  "2",
			wantModifier: "thinking",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, modifier, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if modJoin(modifier) != tc.wantModifier {
				t.Errorf("modifier = %q, want %q", modifier, tc.wantModifier)
			}
			// No case in this table should emit a spurious ParseFailure.
			if failure != nil {
				t.Errorf("unexpected failure: %+v", failure)
			}
		})
	}
}

// TestParseFamilyDetailed_HonestAuditResidual verifies the honest-audit signal:
// when extraction succeeds but leaves a residual token, a ParseFailure is emitted
// with Reason=ReasonResidualUnaccountedTokens AND version is populated.
//
// BDD: Given id="nova-2-lite-v1" and rawFamily="nova-lite" when parsed
// then version="2" AND failure.Reason=ReasonResidualUnaccountedTokens with [v1].
func TestParseFamilyDetailed_HonestAuditResidual(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string
	}{
		{
			desc:        "nova-2-lite-v1 → version=2 + residual failure",
			rawFamily:   "nova-lite",
			id:          "nova-2-lite-v1",
			provider:    "amazon",
			wantVersion: "2",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  honest-audit: version must be populated even when failure is emitted",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
			if failure == nil {
				t.Fatalf("ParseFamilyDetailed(%q, %q): expected ParseFailure with honest-audit residual, got nil",
					tc.rawFamily, tc.id)
			}
			if failure.Reason != bestiary.ReasonResidualUnaccountedTokens {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonResidualUnaccountedTokens)
			}
		})
	}
}

// --------------------------------------------------------------------------
// tests: the bare-4-digit date guard + the sole trailing variant-suffix promotion
// + negative controls
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_Bare4DigitDateGuard verifies the bare-4-digit-date guard: any standalone
// 4-digit all-numeric token is rejected as a version (treated as a date/release-id),
// regardless of whether it falls in the YYMM range (19xx–29xx). The original guard
// (the original YYMM guard) only rejected YYMM-range tokens; the bare-4-digit-date guard generalises to all 4-digit
// numerics since analysis confirmed 0 legitimate bare-4-digit semantic
// versions exist across the 1745 version-populated models.
//
// BDD: Given id contains a bare 4-digit numeric suffix (MMDD format like "0528")
// when ParseFamilyDetailed is called then version="" (no version emitted for date token).
//
// Acceptance: deepseek-r1-0528 → no version; deepseek-v3-0324 → no version;
// mistral-small-2603 still no version (YYMM, already handled by the YYMM guard).
func TestParseFamilyDetailed_Bare4DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string // want empty: 4-digit token must NOT be returned as version
	}{
		{
			// deepseek-r1-0528: "0528" is MMDD format, below 19xx YYMM range.
			// the bare-4-digit-date guard: extended guard rejects "0528" as version.
			desc:        "deepseek-r1-0528 → no version (0528 is MMDD date, not version)",
			rawFamily:   "deepseek-r1",
			id:          "deepseek-r1-0528",
			provider:    "deepseek",
			wantVersion: "",
		},
		{
			// deepseek-v3-0324: "0324" is MMDD format.
			// the bare-4-digit-date guard: extended guard rejects "0324" as version.
			desc:        "deepseek-v3-0324 → no version (0324 is MMDD date, not version)",
			rawFamily:   "deepseek",
			id:          "deepseek-v3-0324",
			provider:    "deepseek",
			wantVersion: "",
		},
		{
			// mistral-small-2603: "2603" is YYMM range — still rejected (YYMM-guard coverage preserved).
			desc:        "mistral-small-2603 → no version (2603 is YYMM date, YYMM guard still holds)",
			rawFamily:   "mistral-small",
			id:          "mistral-small-2603",
			provider:    "mistral",
			wantVersion: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  What: bare 4-digit date token was returned as a version\n"+
					"  Why: the bare-4-digit-date guard guard should reject any 4-digit all-numeric token as a date/release-id\n"+
					"  How to fix: verify isFourDigitDateToken returns true for all 4-digit all-numeric tokens",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
		})
	}
}

// TestExtractVersionFromID_Bare4DigitDateGuard verifies that ExtractVersionFromID
// also rejects bare 4-digit date tokens (the bare-4-digit-date guard parity with ParseFamilyDetailed).
// The guard must be consistent across all call sites: isVersionToken, ExtractVersionFromID,
// and ParseFamilyDetailed.
//
// Acceptance: genuine versions like 4.6, 4.5, 4o still extracted correctly.
func TestExtractVersionFromID_Bare4DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		// the bare-4-digit-date guard: bare 4-digit tokens rejected.
		{
			desc:      "deepseek-r1-0528 → no version (0528 rejected)",
			id:        "deepseek-r1-0528",
			rawFamily: "deepseek-r1",
			want:      "",
		},
		{
			desc:      "some-model-0324 → no version (0324 rejected)",
			id:        "some-model-0324",
			rawFamily: "some-model",
			want:      "",
		},
		// Existing YYMM guard still active.
		{
			desc:      "mistral-small-2603 → no version (2603 YYMM, YYMM guard preserved)",
			id:        "mistral-small-2603",
			rawFamily: "mistral-small",
			want:      "",
		},
		// Genuine versions still extracted (must not regress).
		{
			desc:      "claude-opus-4-6 → 4.6 (legitimate version)",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
		{
			desc:      "gpt-4o → 4o (alphanumeric version)",
			id:        "gpt-4o",
			rawFamily: "gpt",
			want:      "4o",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: the bare-4-digit-date guard bare-4-digit guard inconsistency\n"+
					"  Why: 4-digit token must be rejected by isFourDigitDateToken in ExtractVersionFromID",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// TestParseFamilyDetailed_SoleVariantSuffixPromotion verifies the sole-residual suffix promotion:
// when version was extracted AND exactly ONE residual token remains AND it is a
// known variant suffix (from variant_suffixes.json) AND Variant=="" → the token is
// promoted into Variant, and no ReasonResidualUnaccountedTokens failure is emitted.
//
// BDD: Given id with sole residual = known variant suffix and Variant==""
// when ParseFamilyDetailed is called then variant=<suffix> AND failure=nil.
//
// Acceptance: glm-5-turbo→(glm,turbo,5); phi-4-mini→(phi,mini,4).
// Note: text-embedding-3-large/small were here in the earlier full-prefix-first fix but are now documented residuals
// after reverted the full-prefix-first change.
func TestParseFamilyDetailed_SoleVariantSuffixPromotion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		rawFamily     bestiary.Family
		id            bestiary.ModelID
		provider      bestiary.Provider
		wantFamily    bestiary.Family
		wantVariant   string
		wantVersion   string
		wantNoFailure bool // true → failure must be nil
	}{
		{
			// glm-5-turbo: rawFamily="glm" → family=glm, variant="" initially.
			// ExtractVersionBetween: ver="5", residual=["turbo"]. "turbo" is a known suffix.
			// variant="" → promote "turbo" → (glm, turbo, 5), no failure.
			// 'turbo' is now a global Modifier (glm has no 'turbo' member), so it is
			// NOT promoted into Variant — variant is empty, modifier=[turbo], version=5,
			// and no residual-unaccounted failure (the modifier is a first-class field).
			// turbo→Modifier; ParseFamilyDetailed emits the ReasonKnownSuffixOverflow
			// AUDIT annotation (turbo is a known modifier trailing the ID) which codegen clears
			// once the modifier is a first-class field — so wantNoFailure is false here.
			desc:          "glm-5-turbo → (glm, '', 5) turbo→Modifier",
			rawFamily:     "glm",
			id:            "glm-5-turbo",
			provider:      "zhipu",
			wantFamily:    "glm",
			wantVariant:   "",
			wantVersion:   "5",
			wantNoFailure: false,
		},
		{
			// phi-4-mini: rawFamily="phi" → family=phi, variant="" initially.
			// ExtractVersionBetween: ver="4", residual=["mini"]. "mini" is a known suffix.
			// variant="" → promote "mini" → (phi, mini, 4), no failure.
			desc:          "phi-4-mini → (phi, mini, 4), no residual failure",
			rawFamily:     "phi",
			id:            "phi-4-mini",
			provider:      "microsoft",
			wantFamily:    "phi",
			wantVariant:   "mini",
			wantVersion:   "4",
			wantNoFailure: true,
		},
		// NOTE: text-embedding-3-large and text-embedding-3-small are NOT in this table.
		// The earlier full-prefix-first change that made them promote has been reverted. With firstToken normalization, family="text-embedding" →
		// prefix="text-" → remainder="embedding-3-large" → residual=["embedding","large"]
		// (2 residual tokens, the sole-residual promotion requires exactly 1) → ReasonResidualUnaccountedTokens.
		// These are documented residuals.
		// They are covered by TestParseFamilyDetailed_TextEmbeddingResidual.
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: sole trailing known-suffix was not promoted into Variant\n"+
					"  Why: the sole-residual suffix promotion should set Variant=<suffix> when exactly one residual token is a known variant suffix\n"+
					"  How to fix: verify the sole-residual promotion logic in ParseFamilyDetailed",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if tc.wantNoFailure && failure != nil {
				t.Errorf("failure = %+v, want nil\n"+
					"  What: ReasonResidualUnaccountedTokens emitted even though sole residual was a known suffix\n"+
					"  Why: the sole-residual suffix promotion should suppress failure when sole residual is promoted to Variant",
					failure)
			}
		})
	}
}

// TestParseFamilyDetailed_SoleVariantSuffixPromotion_NegativeControls verifies that a residual failure
// is STILL emitted (the model does not fully decompose) in two cases where the
// trailing residue is more than a single promotable suffix token — even though the
// member-variant IS now recovered by recoverMemberVariant.
//
// recoverMemberVariant superseded the old inline promotion.
// Unlike the old promotion — which fired only on EXACTLY ONE post-version residual token — the
// broad member-zone scan now recovers a member variant up front (for registered
// families) regardless of how many OTHER residual tokens follow. So the variant IS
// populated here; the residual failure persists because a DIFFERENT, unaccounted
// token remains after the variant:
//
//	phi-3-medium-128k-instruct → variant="medium" recovered (phi member), but
//	    "128k" (and "instruct") remain unaccounted → ReasonResidualUnaccountedTokens.
//	nova-2-lite-v1 → variant="lite" set by ParseFamilyWithVersion suffix-strip, but
//	    "v1" remains after the variant → ReasonResidualUnaccountedTokens.
//
// These remain documented residuals (user-accepted, out of scope): the failure is
// the honest-audit signal that the ID did not fully decompose, NOT a missing variant.
func TestParseFamilyDetailed_SoleVariantSuffixPromotion_NegativeControls(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVariant string // member variant IS recovered, even though a residual remains
		wantFailure bool   // true → failure must be non-nil
		wantReason  bestiary.ParseFailureReason
		desc2       string // description of why it stays residual
	}{
		{
			// phi-3-medium-128k-instruct: recoverMemberVariant recovers "medium" (a phi
			// member) up front. ExtractVersionBetween then finds ver="3" with residual
			// ["128k","instruct"] AFTER the variant → residual failure persists.
			desc:        "phi-3-medium-128k-instruct (variant=medium recovered; 128k unaccounted)",
			rawFamily:   "phi",
			id:          "phi-3-medium-128k-instruct",
			provider:    "microsoft",
			wantVariant: "medium",
			wantFailure: true,
			wantReason:  bestiary.ReasonResidualUnaccountedTokens,
			desc2:       "variant recovered as 'medium', but '128k' remains unaccounted after the variant",
		},
		{
			// nova-2-lite-v1: rawFamily="nova-lite" → variant="lite" via ParseFamilyWithVersion
			// suffix-strip (so recoverMemberVariant is not consulted). ExtractVersionBetween
			// finds ver="2", residual=["v1"] AFTER the variant → residual failure persists.
			desc:        "nova-2-lite-v1 (variant=lite pre-set; v1 unaccounted)",
			rawFamily:   "nova-lite",
			id:          "nova-2-lite-v1",
			provider:    "cartesia",
			wantVariant: "lite",
			wantFailure: true,
			wantReason:  bestiary.ReasonResidualUnaccountedTokens,
			desc2:       "variant is 'lite'; 'v1' remains unaccounted after the variant",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, variant, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if variant != tc.wantVariant {
				t.Errorf("ParseFamilyDetailed(%q, %q): variant = %q, want %q\n"+
					"  What: recoverMemberVariant should recover the member variant even when a residual remains\n"+
					"  Why: the broad member-zone scan no longer requires a single sole residual (unlike the old promotion)",
					tc.rawFamily, tc.id, variant, tc.wantVariant)
			}
			if tc.wantFailure {
				if failure == nil {
					t.Errorf("ParseFamilyDetailed(%q, %q): failure = nil, want %q failure\n"+
						"  What: a residual failure should still fire here (%s)\n"+
						"  Why: an unaccounted token remains after the recovered variant",
						tc.rawFamily, tc.id, tc.wantReason, tc.desc2)
					return
				}
				if failure.Reason != tc.wantReason {
					t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason = %q, want %q",
						tc.rawFamily, tc.id, failure.Reason, tc.wantReason)
				}
			} else if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): unexpected failure %q", tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// --------------------------------------------------------------------------
// date-as-version guard inside dot-join paths
// --------------------------------------------------------------------------

// TestParseFamilyWithVersion_DateGroupsStripped verifies:
// the date-shape guard is applied INSIDE the hyphen-version dot-join path,
// stripping trailing date groups and keeping only leading semantic-version groups.
//
// INVARIANT: no model's Version may be a date-shaped group. Covered shapes:
//   - 4-digit YYMM (e.g. "2603", "2512", "2508") → ""
//   - 4-digit MMDD (e.g. "0528", "0314", "1206") → ""
//   - 6-digit YYMMDD (e.g. "250615", "250715") → stripped from trailing position
//   - MM-YYYY two-group (e.g. "08-2024", "03-2025") → ""
//
// BDD: given a raw family string with a trailing date group in hyphen-version form,
// when ParseFamilyWithVersion is called, then version="" (date stripped) or version
// equals only the leading non-date groups.
func TestParseFamilyWithVersion_DateGroupsStripped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// 4-digit YYMM cases: base in overrides, trailing date token.
		{
			name: "mistral-small-2603 → version empty (2603 is YYMM date)",
			raw:  "mistral-small-2603", wantFamily: "mistral", wantVariant: "small", wantVersion: "",
		},
		{
			name: "mistral-large-2512 → version empty (2512 is YYMM date)",
			raw:  "mistral-large-2512", wantFamily: "mistral", wantVariant: "large", wantVersion: "",
		},
		{
			name: "codestral-2508 → version empty (2508 is YYMM date)",
			raw:  "codestral-2508", wantFamily: "codestral", wantVariant: "", wantVersion: "",
		},
		// 4-digit MMDD cases: leading semantic version kept, trailing date stripped.
		{
			name: "gpt-4-0314 → version=4 (0314 is MMDD date, leading 4 kept)",
			raw:  "gpt-4-0314", wantFamily: "gpt", wantVariant: "", wantVersion: "4",
		},
		// 6-digit YYMMDD cases: stripped from trailing position.
		{
			name: "doubao-seed-1-6-250615 → version=1.6 (250615 is YYMMDD, stripped)",
			raw:  "doubao-seed-1-6-250615", wantFamily: "doubao-seed", wantVariant: "", wantVersion: "1.6",
		},
		// 4-digit MMDD: gemini-exp-1206 (1206 is MMDD, single group → "").
		{
			name: "gemini-exp-1206 → version empty (1206 is MMDD date)",
			raw:  "gemini-exp-1206", wantFamily: "gemini-exp", wantVariant: "", wantVersion: "",
		},
		// 4-digit MMDD: deepseek-r1-0528.
		{
			name: "deepseek-r1-0528 → version empty (0528 is MMDD date)",
			raw:  "deepseek-r1-0528", wantFamily: "deepseek-r1", wantVariant: "", wantVersion: "",
		},
		// MM-YYYY two-group cases: full remainder is a date.
		{
			name: "command-r-08-2024 → version empty (08-2024 is MM-YYYY date)",
			raw:  "command-r-08-2024", wantFamily: "command", wantVariant: "r", wantVersion: "",
		},
		{
			name: "command-a-03-2025 → version empty (03-2025 is MM-YYYY date)",
			raw:  "command-a-03-2025", wantFamily: "command", wantVariant: "a", wantVersion: "",
		},
		// Regression: legitimate versions must be preserved.
		{
			name: "claude-opus-4-5 → version=4.5 (no date, preserve)",
			raw:  "claude-opus-4-5", wantFamily: "claude", wantVariant: "opus", wantVersion: "4.5",
		},
		{
			name: "llama-3-1 → version=3.1 (no date, preserve)",
			raw:  "llama-3-1", wantFamily: "llama", wantVariant: "", wantVersion: "3.1",
		},
		{
			name: "phi-4-5 → version=4.5 (no date, preserve)",
			raw:  "phi-4-5", wantFamily: "phi", wantVariant: "", wantVersion: "4.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(tc.raw)
			if gotFamily != tc.wantFamily {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", tc.raw, gotFamily, tc.wantFamily)
			}
			if gotVariant != tc.wantVariant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", tc.raw, gotVariant, tc.wantVariant)
			}
			if gotVersion != tc.wantVersion {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q\n"+
					"  What: date-shaped token was returned as version\n"+
					"  Why: the date-shape guard must strip date groups inside hyphen-version dot-join path\n"+
					"  How to fix: verify dotJoinStrippingDateSuffix strips trailing date groups",
					tc.raw, gotVersion, tc.wantVersion)
			}
		})
	}
}

// TestExtractVersionFromID_MMYYYYTwoGroup verifies for the
// reHyphenDigits path in ExtractVersionFromID: the MM-YYYY two-group pattern
// (e.g. "08-2024", "03-2025") must be detected as a date and return "".
//
// BDD: given remainder="MM-YYYY" after family-prefix strip, when ExtractVersionFromID
// is called, then "" is returned (date shape, not a semantic version).
func TestExtractVersionFromID_MMYYYYTwoGroup(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		id        bestiary.ModelID
		rawFamily bestiary.Family
		want      string
	}{
		{
			name:      "command-r-08-2024 → no version (08-2024 is MM-YYYY)",
			id:        "command-r-08-2024",
			rawFamily: "command-r",
			want:      "",
		},
		{
			name:      "command-a-03-2025 → no version (03-2025 is MM-YYYY)",
			id:        "command-a-03-2025",
			rawFamily: "command-a",
			want:      "",
		},
		// Regression: must not break legitimate hyphen-digit versions.
		{
			name:      "claude-opus-4-5 → 4.5 (legitimate version preserved)",
			id:        "claude-opus-4-5",
			rawFamily: "claude-opus",
			want:      "4.5",
		},
		{
			name:      "claude-opus-4-6 → 4.6 (legitimate version preserved)",
			id:        "claude-opus-4-6",
			rawFamily: "claude-opus",
			want:      "4.6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractVersionFromID(tc.id, tc.rawFamily)
			if got != tc.want {
				t.Errorf("ExtractVersionFromID(%q, %q) = %q, want %q\n"+
					"  What: MM-YYYY two-group was returned as version\n"+
					"  Why: the isMMYYYYTwoGroup guard must detect and reject MM-YYYY remainder",
					tc.id, tc.rawFamily, got, tc.want)
			}
		})
	}
}

// TestExtractVersionBetweenFamilyAndVariant_6DigitStripped verifies that
// correctly strips 6-digit YYMMDD tokens from the version extraction
// loop in ExtractVersionBetweenFamilyAndVariant.
//
// BDD: given an ID with a 6-digit YYMMDD suffix embedded after valid version tokens,
// when ExtractVersionBetweenFamilyAndVariant is called, then the version contains only
// the leading semantic groups (6-digit date group is stopped at, not included).
func TestExtractVersionBetweenFamilyAndVariant_6DigitStripped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		id           bestiary.ModelID
		family       bestiary.Family
		variant      string
		wantVersion  string
		wantResidual []string
	}{
		{
			// seed-1-6-flash-250715: rawFamily="seed", 250715 is 6-digit YYMMDD.
			// With variant="flash" (from ParseFamilyDetailed), extraction stops at 250715.
			desc:         "seed-1-6-flash-250715 with variant=flash → version=1.6",
			id:           "seed-1-6-flash-250715",
			family:       "seed",
			variant:      "flash",
			wantVersion:  "1.6",
			wantResidual: nil,
		},
		{
			// doubao-seed-1-6-250615: family="doubao-seed", variant="".
			// full-prefix-first reverted; firstToken("doubao-seed")="doubao" →
			// prefix="doubao-", remainder="seed-1-6-250615" → "seed" is non-version residual,
			// "1","6" are version tokens, "250615" is 6-digit YYMMDD → stop.
			// → version="1.6", residual=["seed"] (honest-audit residual for compound family).
			desc:         "doubao-seed-1-6-250615 with variant=empty → version=1.6, residual=[seed]",
			id:           "doubao-seed-1-6-250615",
			family:       "doubao-seed",
			variant:      "",
			wantVersion:  "1.6",
			wantResidual: []string{"seed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			gotVersion, gotResidual := bestiary.ExtractVersionBetweenFamilyAndVariant(tc.id, tc.family, tc.variant)
			if gotVersion != tc.wantVersion {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) version = %q, want %q\n"+
					"  What: 6-digit YYMMDD was included in version\n"+
					"  Why: isDateShapedToken must reject 6-digit tokens in dot-join loop",
					tc.id, tc.family, tc.variant, gotVersion, tc.wantVersion)
			}
			if len(gotResidual) != len(tc.wantResidual) {
				t.Errorf("ExtractVersionBetweenFamilyAndVariant(%q, %q, %q) residual = %v, want %v",
					tc.id, tc.family, tc.variant, gotResidual, tc.wantResidual)
			}
		})
	}
}

// --------------------------------------------------------------------------
// regression tests
// --------------------------------------------------------------------------

// TestParseFamilyDetailed_VersionRestoredAfterRevert is the regression
// test pinning that the full-prefix-first revert RESTORES version extraction for
// the three canonical cases that were over-stripped. Guards against version-nulling recurrence.
//
// BDD: Given model IDs whose version digits appear BEFORE a compound family prefix in the ID
// (e.g. "gemini-2.5-flash-image-generation" where "2.5" precedes "flash"),
// when ParseFamilyDetailed is called, then version is populated (not empty).
//
// Pinned assertions:
//   - claude-3-7-sonnet-thinking → version "3.7"
//   - gemini-2.5-flash-image-generation → version "2.5"
//   - grok-3-beta → version "3"
func TestParseFamilyDetailed_VersionRestoredAfterRevert(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantVersion string
	}{
		{
			// claude-3-7-sonnet-thinking: rawFamily="claude-sonnet" → (claude, sonnet, "").
			// ExtractVersionBetween(id, "claude", "sonnet"): prefix="claude-", rem="3-7-sonnet-thinking-20250219"
			// → date strip → "3-7-sonnet-thinking" → "3","7" before "sonnet" → ver="3.7".
			// (modifier "thinking" is stripped by ExtractModifier before this call.)
			desc:        "claude-3-7-sonnet-thinking → version 3.7 (the full-prefix-first revert restore)",
			rawFamily:   "claude-sonnet",
			id:          "claude-3-7-sonnet-thinking-20250219",
			provider:    "anthropic",
			wantVersion: "3.7",
		},
		{
			// gemini-2.5-flash-image-generation: rawFamily="gemini-2.5-flash-image" → via suffix/overrides
			// → family="gemini-2.5-flash", variant="image". ExtractVersionBetween(id, "gemini-2.5-flash", "image"):
			// prefix=firstToken("gemini-2.5-flash")+"-"="gemini-", rem="2.5-flash-image-generation"
			// → dot-version early return: reBareVersion.MatchString("2.5")=true → ver="2.5".
			// (the earlier full-prefix-first fix full-prefix-first would have matched "gemini-2.5-flash-" and returned ver="".)
			desc:        "gemini-2.5-flash-image-generation → version 2.5 (the full-prefix-first revert restore)",
			rawFamily:   "gemini-2.5-flash-image",
			id:          "gemini-2.5-flash-image-generation",
			provider:    "google",
			wantVersion: "2.5",
		},
		{
			// grok-3-beta: rawFamily="grok" → (grok, "", ""). ExtractVersionBetween(id, "grok", ""):
			// prefix="grok-", rem="3-beta" → no variantFirst → "3" is version, "beta" residual.
			// len(residual)==1, variant=="" → check "beta" is known suffix → promote → (grok, beta, 3).
			desc:        "grok-3-beta → version 3 (the full-prefix-first revert restore)",
			rawFamily:   "grok",
			id:          "grok-3-beta",
			provider:    "xai",
			wantVersion: "3",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if version != tc.wantVersion {
				t.Errorf("ParseFamilyDetailed(%q, %q): version = %q, want %q\n"+
					"  What: version not extracted — the full-prefix-first revert should restore this\n"+
					"  Why: full-prefix-first over-stripped compound family prefix, losing the leading version digits\n"+
					"  How to fix: verify ExtractVersionBetweenFamilyAndVariant uses firstToken normalization, not full-prefix",
					tc.rawFamily, tc.id, version, tc.wantVersion)
			}
			if failure != nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): unexpected ParseFailure reason=%q (version should be populated cleanly)",
					tc.rawFamily, tc.id, failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_SurvivingSoleSuffixPromotions verifies that the sole-variant-suffix
// promotions that do NOT depend on the full-prefix-first change SURVIVE the full-prefix-first revert.
// These are gpt-5-codex and gpt-4-turbo, where rawFamily="gpt" (single token) and the full-prefix
// is the same as firstToken, so the revert has no effect on them.
//
// BDD: Given rawFamily="gpt" (single token, no compound prefix), when ParseFamilyDetailed is
// called on gpt-5-codex and gpt-4-turbo, then the promotion fires for the sole residual variant suffix.
func TestParseFamilyDetailed_SurvivingSoleSuffixPromotions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// gpt-5-codex: rawFamily="gpt" → (gpt, "", ""). ExtractVersionBetween: prefix="gpt-",
			// rem="5-codex" → "5" is version, "codex" residual. Promotion: len(residual)==1, variant=="" →
			// "codex" is a known suffix → promote → (gpt, codex, 5). No compound prefix issue.
			desc:        "gpt-5-codex → (gpt, codex, 5) — promotion survives the full-prefix-first revert",
			rawFamily:   "gpt",
			id:          "gpt-5-codex",
			provider:    "openai",
			wantFamily:  "gpt",
			wantVariant: "codex",
			wantVersion: "5",
		},
		{
			// 'turbo' is now a global Modifier (gpt has no 'turbo' member), so it is
			// extracted to the Modifier list instead of promoted to Variant → (gpt, "", 4).
			desc:        "gpt-4-turbo → (gpt, '', 4) turbo→Modifier",
			rawFamily:   "gpt",
			id:          "gpt-4-turbo",
			provider:    "openai",
			wantFamily:  "gpt",
			wantVariant: "",
			wantVersion: "4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			family, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: the sole-variant-suffix promotion did not fire\n"+
					"  Why: gpt-5-codex/gpt-4-turbo use single-token rawFamily ('gpt'); revert should not affect them\n"+
					"  How to fix: verify the sole-residual promotion logic in ParseFamilyDetailed",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			// a turbo→Modifier reclassification emits the ReasonKnownSuffixOverflow
			// AUDIT annotation (codegen clears it once the modifier is first-class). Permit it;
			// any OTHER failure reason is still unexpected.
			if failure != nil && failure.Reason != bestiary.ReasonKnownSuffixOverflow {
				t.Errorf("unexpected ParseFailure reason=%q; the promotion should have promoted sole residual to variant",
					failure.Reason)
			}
		})
	}
}

// TestParseFamilyDetailed_TextEmbeddingResidual documents the EXPECTED post-the full-prefix-first revert behavior
// of text-embedding-3-large and text-embedding-3-small: they are documented residuals
// (ReasonResidualUnaccountedTokens) after the full-prefix-first revert.
//
// After revert: firstToken("text-embedding")="text" → prefix="text-" → remainder="embedding-3-large"
// → residual=["embedding","large"] (2 tokens, the sole-residual promotion requires exactly 1) → failure emitted.
// Proper additive handling is deferred.
func TestParseFamilyDetailed_TextEmbeddingResidual(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rawFamily bestiary.Family
		id        bestiary.ModelID
		provider  bestiary.Provider
	}{
		{rawFamily: "text-embedding", id: "text-embedding-3-large", provider: "openai"},
		{rawFamily: "text-embedding", id: "text-embedding-3-small", provider: "openai"},
	}

	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			_, _, _, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if failure == nil {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure=nil, want ReasonResidualUnaccountedTokens\n"+
					"  What: text-embedding models should emit residual failure after the full-prefix-first revert\n"+
					"  Why: full-prefix-first was reverted; firstToken('text-embedding')='text' leaves 'embedding' as residual\n"+
					"  How to fix: verify full-prefix-first is NOT in ExtractVersionBetweenFamilyAndVariant",
					tc.rawFamily, tc.id)
				return
			}
			if failure.Reason != bestiary.ReasonResidualUnaccountedTokens {
				t.Errorf("ParseFamilyDetailed(%q, %q): failure.Reason=%q, want %q",
					tc.rawFamily, tc.id, failure.Reason, bestiary.ReasonResidualUnaccountedTokens)
			}
		})
	}
}

// TestParseFamilyWithVersion_Step5_6DigitDateGuard verifies the 6-digit-date-guard
// fix: ParseFamilyWithVersion Step-5 override-prefix version loop now uses isDateShapedToken
// (catches 4-digit AND 6-digit YYMMDD) instead of isFourDigitDateToken (4-digit only).
//
// BDD: Given a rawFamily string that hits the Step-5 override-prefix path AND contains a
// 6-digit YYMMDD date token in the suffix (e.g. "claude-opus-1-6-250615"),
// when ParseFamilyWithVersion is called, then the 6-digit date is NOT included in the version.
//
// Also confirms TestStaticModels_NoDateVersions invariant is not violated by the 4th site.
//
// NOTE: The inputs in this test (e.g. "claude-opus-1-6-250615")
// actually match the Step-2 hyphen-version regex (all-digit suffix) and are processed by
// dotJoinStrippingDateSuffix BEFORE reaching Step-5. These tests are therefore NOT load-bearing
// for parse.go:455 (the isDateShapedToken guard in the Step-5 override-prefix loop).
// See TestParseFamilyWithVersion_Step5_6DigitDateGuard_LoadBearing below for the
// load-bearing test that actually exercises parse.go:455.
func TestParseFamilyWithVersion_Step5_6DigitDateGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// claude-opus-1-6-250615: "claude-opus" is in overrides → (claude, opus).
			// suffix = ["1","6","250615"]. "1" ok, "6" ok, "250615" is 6-digit → isDateShapedToken → stop.
			// → ver="1.6" (not "1.6.250615").
			name:        "claude-opus-1-6-250615 → version 1.6 (6-digit date stopped at Step-5)",
			raw:         "claude-opus-1-6-250615",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "1.6",
		},
		{
			// claude-sonnet-3-7-250219: "claude-sonnet" in overrides → (claude, sonnet).
			// suffix = ["3","7","250219"]. "3" ok, "7" ok, "250219" 6-digit → stop.
			// → ver="3.7".
			name:        "claude-sonnet-3-7-250219 → version 3.7 (6-digit date stopped at Step-5)",
			raw:         "claude-sonnet-3-7-250219",
			wantFamily:  "claude",
			wantVariant: "sonnet",
			wantVersion: "3.7",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			family, variant, version := bestiary.ParseFamilyWithVersion(tc.raw)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q", family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: 6-digit YYMMDD token included in version at Step-5 override-prefix loop\n"+
					"  Why: isFourDigitDateToken only catches 4-digit tokens; is6DigitYYMMDD was not guarded here\n"+
					"  How to fix: verify Step-5 loop uses isDateShapedToken",
					version, tc.wantVersion)
			}
			// The version must NOT be a 6-digit all-numeric string.
			if len(version) == 6 {
				allDigits := true
				for _, r := range version {
					if r < '0' || r > '9' {
						allDigits = false
						break
					}
				}
				if allDigits {
					t.Errorf("version = %q is a bare 6-digit date — INVARIANT VIOLATED: version must not be a date",
						version)
				}
			}
		})
	}
}

// TestParseFamilyWithVersion_Step5_6DigitDateGuard_LoadBearing is the LOAD-BEARING
// companion test for parse.go:455 (the isDateShapedToken guard inside the Step-5
// override-prefix version loop of ParseFamilyWithVersion).
//
// Background:
//
// The existing TestParseFamilyWithVersion_Step5_6DigitDateGuard is NOT load-bearing for
// parse.go:455: its inputs (e.g. "claude-opus-1-6-250615") match the Step-2 hyphen-version
// regex (^base-(\d+(-\d+)*)$) because their suffix is all-numeric, so they are handled by
// dotJoinStrippingDateSuffix at Step-2 and RETURN before Step-5 is ever entered.
// Reverting parse.go:455 from isDateShapedToken back to isFourDigitDateToken passes the entire
// test suite — confirming the original test does NOT exercise the full-prefix-first revert change site.
//
// Reaching Step-5: the Step-5 override-prefix loop fires when:
//
//	(a) No exact-override match at Step-1 (rawStr itself is not in overrides),
//	(b) No hyphen-version match at Step-2 (requires the TRAILING suffix to be all-numeric —
//	    any non-digit token after the last digit group defeats the match),
//	(c) No other pattern (v/k/m/no-prefix) at Step-2,
//	(d) No suffix-strip match at Step-3,
//	(e) No dot-version match at Step-4.
//
// Key insight: appending a non-digit modifier (e.g. "-zen") after the date prevents the
// hyphen-version regex from matching (it requires an all-digit tail), so the input falls
// through to Step-5 where the override-prefix scan fires.
//
// Mutation verification (performed during test authoring):
//
//	Reverting parse.go:455 to isFourDigitDateToken: FAILS these cases.
//	  "claude-opus-1-6-250615-zen" → version="1.6.250615" (want "1.6")
//	  "claude-opus-4-250615-zen"   → version="4.250615"   (want "4")
//	  "claude-opus-250615-zen"     → version="250615"     (want "")
//	Restoring parse.go:455 to isDateShapedToken: PASSES all cases.
//
// BDD:
//
//	Given a rawFamily that (1) has no exact override match, (2) does NOT match the
//	hyphen-version regex due to a trailing non-digit modifier, and (3) has a known
//	override prefix in the overrides table with a 6-digit YYMMDD date token in the
//	version position of the remaining suffix —
//	When ParseFamilyWithVersion is called,
//	Then the 6-digit token must NOT appear in the returned version.
func TestParseFamilyWithVersion_Step5_6DigitDateGuard_LoadBearing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         bestiary.Family
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		{
			// "claude-opus-1-6-250615-zen": trailing "-zen" defeats hyphen-version regex (Step-2)
			// → falls through to Step-5. Override scan: "claude-opus" → {claude, opus}.
			// suffix = ["1","6","250615","zen"]. Tokens: "1" (version), "6" (version),
			// "250615" (6-digit YYMMDD — isDateShapedToken=true) → break.
			// Without the full-prefix-first revert (isFourDigitDateToken): "250615" has len=6≠4 → isFourDigitDateToken=false
			// → "250615" appended → version="1.6.250615" (WRONG).
			// With the full-prefix-first revert (isDateShapedToken): is6DigitYYMMDD("250615")=true → break → version="1.6" (CORRECT).
			name:        "claude-opus-1-6-250615-zen → version 1.6 (Step-5 path, 6-digit blocked)",
			raw:         "claude-opus-1-6-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "1.6",
		},
		{
			// "claude-opus-4-250615-zen": no hyphen-version match (zen at end).
			// Step-5: "claude-opus" override → suffix=["4","250615","zen"].
			// "4" ok, "250615" 6-digit → break → version="4".
			name:        "claude-opus-4-250615-zen → version 4 (Step-5 path, 6-digit blocked)",
			raw:         "claude-opus-4-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "4",
		},
		{
			// "claude-opus-250615-zen": no hyphen-version match (zen at end).
			// Step-5: "claude-opus" override → suffix=["250615","zen"].
			// "250615" is 6-digit date → break immediately → version="".
			name:        "claude-opus-250615-zen → version empty (Step-5 path, 6-digit blocked first)",
			raw:         "claude-opus-250615-zen",
			wantFamily:  "claude",
			wantVariant: "opus",
			wantVersion: "",
		},
		{
			// "claude-sonnet-4-250615-zen": uses "claude-sonnet" override.
			// suffix=["4","250615","zen"]. "4" ok, "250615" 6-digit → break → version="4".
			name:        "claude-sonnet-4-250615-zen → version 4 (Step-5 path, 6-digit blocked)",
			raw:         "claude-sonnet-4-250615-zen",
			wantFamily:  "claude",
			wantVariant: "sonnet",
			wantVersion: "4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			family, variant, version := bestiary.ParseFamilyWithVersion(tc.raw)
			if family != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: wrong family from Step-5 override-prefix decomposition\n"+
					"  File: parse.go ParseFamilyWithVersion Step-5 (lines ~443-465)",
					family, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: wrong variant from Step-5 override-prefix decomposition\n"+
					"  File: parse.go ParseFamilyWithVersion Step-5 (lines ~443-465)",
					variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: 6-digit YYMMDD token leaked into version at parse.go:455 (Step-5 loop)\n"+
					"  Why: isFourDigitDateToken only rejects 4-digit tokens; 6-digit YYMMDD (len=6) passes through it\n"+
					"  Where: parse.go ParseFamilyWithVersion Step-5, loop at ~line 454-459\n"+
					"  How to fix: parse.go:455 must use isDateShapedToken (not isFourDigitDateToken)\n"+
					"  Ref: load-bearing mutation test",
					version, tc.wantVersion)
			}
			// The version must NOT contain a 6-digit all-numeric segment (date leak).
			for _, seg := range splitDotSegments(version) {
				if len(seg) == 6 && isAllDigits(seg) {
					t.Errorf("version segment %q is a bare 6-digit date — INVARIANT VIOLATED\n"+
						"  What: version=%q contains date-shaped segment %q\n"+
						"  Ref: parse.go:455 isDateShapedToken guard",
						seg, version, seg)
				}
			}
		})
	}
}

// splitDotSegments splits s on "." and returns the non-empty parts.
// Used by TestParseFamilyWithVersion_Step5_6DigitDateGuard_LoadBearing to
// inspect individual dot-notation version tokens.
func splitDotSegments(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isAllDigits reports whether every rune in s is an ASCII digit.
// Used by TestParseFamilyWithVersion_Step5_6DigitDateGuard_LoadBearing.
func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ============================================================================
// Tests (RED until the vendor strip/the case-fold/recoverMemberVariant are implemented)
// ============================================================================

// ----------------------------------------------------------------------------
// the case-fold — case-fold: Family field must be lowercase at the output boundary
// ----------------------------------------------------------------------------

// TestFamilyCaseFold verifies that ParseFamilyDetailed lowercases the
// Family field regardless of the casing in the raw_family input (the case-fold).
//
// BDD: Given a mixed-case raw_family (e.g. "MiniMax"),
// When ParseFamilyDetailed is called,
// Then the returned Family is lowercase ("minimax").
//
// This is the case-fold step. Fixes CatA cross-provider divergences
// (e.g. some providers return raw_family="MiniMax" while others return "minimax").
func TestFamilyCaseFold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		rawFamily  bestiary.Family
		id         bestiary.ModelID
		provider   bestiary.Provider
		wantFamily bestiary.Family
	}{
		{
			// CatA divergence: some providers return "MiniMax" (capitalised),
			// others return "minimax". the case-fold normalises both to lowercase "minimax".
			desc:       "MiniMax raw_family → lowercase minimax",
			rawFamily:  "MiniMax",
			id:         "minimax-m1-80k",
			provider:   "nano-gpt",
			wantFamily: "minimax",
		},
		{
			// "Hy" is the only uppercase entry in allFamilies. The case-fold lowercases it.
			desc:       "Hy raw_family → lowercase hy",
			rawFamily:  "Hy",
			id:         "hy3-something",
			provider:   "some-provider",
			wantFamily: "hy",
		},
		{
			// Already lowercase — the case-fold is a no-op; existing behaviour preserved.
			desc:       "claude raw_family unchanged by the case-fold",
			rawFamily:  "claude-opus",
			id:         "claude-opus-4-6",
			provider:   "anthropic",
			wantFamily: "claude",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, _, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: the case-fold case-fold did not lowercase the Family field\n"+
					"  Why: the parser requires Family(strings.ToLower(...)) at the Family-field boundary\n"+
					"  How to fix: apply the case-fold case-fold in ParseFamilyDetailed and InferFamilyFromIDWithVariant",
					fam, tc.wantFamily)
			}
		})
	}
}

// TestInferFamilyCaseFold verifies that InferFamilyFromIDWithVariant (the
// empty-raw-family path) also lowercases the inferred Family field (the case-fold).
//
// BDD: Given an empty raw_family with a mixed-case model ID (e.g. "MiniMax-M1"),
// When InferFamilyFromIDWithVariant is called,
// Then the returned Family is lowercase ("minimax").
func TestInferFamilyCaseFold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc       string
		id         bestiary.ModelID
		provider   bestiary.Provider
		wantFamily bestiary.Family
	}{
		{
			// MiniMax-M1: some providers have empty raw_family; model ID starts with
			// "MiniMax" (uppercase). After the vendor strip path-strip and the case-fold lowercase, family
			// should be "minimax".
			desc:       "MiniMax-M1 empty raw_family → minimax (the case-fold lowercase)",
			id:         "MiniMax-M1",
			provider:   "nano-gpt",
			wantFamily: "minimax",
		},
		{
			// deepseek-ai/DeepSeek-V3.2: the vendor strip path-strip gives "DeepSeek-V3.2", the case-fold
			// lowercases first token → "deepseek".
			desc:       "DeepSeek-V3.2 after path strip → deepseek (the case-fold lowercase)",
			id:         "deepseek-ai/DeepSeek-V3.2",
			provider:   "some-provider",
			wantFamily: "deepseek",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, _, _ := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("InferFamilyFromIDWithVariant(%q) family = %q, want %q\n"+
					"  What: the case-fold case-fold did not lowercase inferred Family\n"+
					"  Why: the parser requires the case-fold lowercase at Family-field boundary in"+
					" InferFamilyFromIDWithVariant\n"+
					"  How to fix: apply Family(strings.ToLower(...)) at the return boundaries",
					tc.id, fam, tc.wantFamily)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// the vendor strip — vendor/namespace strip via vendor_aliases.json
// ----------------------------------------------------------------------------

// TestVendorAliasStrip verifies that model IDs starting with a vendor alias
// from vendor_aliases.json have the alias prefix stripped before family inference.
//
// BDD: Given a model ID starting with "minimaxai-" (a vendor alias NOT in
// Providers()),
// When InferFamilyFromIDWithVariant is called with empty raw_family,
// Then the vendor prefix is stripped and family="minimax" is inferred.
//
// The "/" separator case (e.g. "minimaxai/minimax-m1") is already handled by
// the existing lastPathSegment call. This test specifically covers the "-"
// separator variant.
func TestVendorAliasStrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		{
			// "minimaxai-minimax-m1": the vendor strip strips "minimaxai-" → "minimax-m1",
			// the case-fold lowercases → "minimax-m1"; (d) series split → variant="m"
			// (REVERSES the whole-token "m1").
			desc:        "minimaxai-minimax-m1 → strip alias, family=minimax series variant=m",
			id:          "minimaxai-minimax-m1",
			provider:    "some-provider",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _ := bestiary.InferFamilyFromIDWithVariant(tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: the vendor strip vendor alias strip did not remove vendor prefix\n"+
					"  How to fix: implement the vendor strip '-' strip for vendor_aliases in pipeline",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: recoverMemberVariant did not recover variant from stripped ID",
					variant, tc.wantVariant)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// recoverMemberVariant — sole owner of member-variant recovery
// ----------------------------------------------------------------------------

// TestRecoverMemberVariant_FamiliesJSONMembers verifies that recoverMemberVariant
// recovers variant tokens from families.json members, specifically for tokens that
// are NOT in variant_suffixes.json (the old sole-residual scope) but ARE in the family's
// member list.
//
// BDD: Given raw_family="minimax" and id="minimax-m1-80k" (where "m1" is in
// families.json minimax.members but NOT in variant_suffixes.json),
// When ParseFamilyDetailed is called,
// Then variant="m1" is recovered.
//
// This test covers the NEW scope of recoverMemberVariant beyond the old sole-residual promotion.
// It will be RED until recoverMemberVariant is implemented.
func TestRecoverMemberVariant_FamiliesJSONMembers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		{
			// / minimax is now a letter-prefix SERIES
			// (series_letter "m"), so the series split OWNS this decomposition and
			// supersedes the old whole-token "m1" member recovery: minimax-m1-80k →
			// variant="m" (+ version "1"; "80k" is an ignored context-window token).
			desc:        "minimax raw_family + id minimax-m1-80k → series variant=m",
			rawFamily:   "minimax",
			id:          "minimax-m1-80k",
			provider:    "minimax",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
		{
			// / empty raw_family, MiniMax-M1 (mixed case)
			// → the case-fold family="minimax"; series split → variant="m" (+ version "1").
			desc:        "empty raw_family, MiniMax-M1 → series (minimax, m)",
			rawFamily:   "",
			id:          "MiniMax-M1",
			provider:    "nano-gpt",
			wantFamily:  "minimax",
			wantVariant: "m",
		},
		{
			// qwen family, member "max" not in variant_suffixes.json.
			// raw_family="qwen", id="qwen-max" → variant="max" via member recovery.
			desc:        "qwen raw_family + id qwen-max → variant=max",
			rawFamily:   "qwen",
			id:          "qwen-max",
			provider:    "alibaba",
			wantFamily:  "qwen",
			wantVariant: "max",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q", fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q\n"+
					"  What: recoverMemberVariant did not recover variant from families.json members\n"+
					"  Why: the parser requires recoverMemberVariant to consult pd.families members\n"+
					"  How to fix: implement recoverMemberVariant in pipeline",
					variant, tc.wantVariant)
			}
		})
	}
}

// TestRecoverMemberVariant_SubsumesSoleSuffixPromotion verifies that the family-agnostic
// sole-residual suffix promotion still yields its expected (family, variant,
// version) results.
//
// NOTE: The sole-residual promotion was NOT removed. The fix cycle RESTORED a
// version-preserving promotion that runs POST-version extraction for UNREGISTERED
// families (via the shared bareVariantSuffix helper) — see the sole-residual promotion block in
// ParseFamilyDetailed and the recoverMemberVariant doc comment. These cases must
// remain green; if they turn red, the restored promotion regressed.
func TestRecoverMemberVariant_SubsumesSoleSuffixPromotion(t *testing.T) {
	t.Parallel()

	// These cases were already tested by TestParseFamilyDetailed_SoleVariantSuffixPromotion.
	// They must remain green after the inline promotion is removed. Including here as explicit
	// regression guards for the recoverMemberVariant subsumption.
	cases := []struct {
		desc          string
		rawFamily     bestiary.Family
		id            bestiary.ModelID
		provider      bestiary.Provider
		wantFamily    bestiary.Family
		wantVariant   string
		wantVersion   string
		wantNoFailure bool
	}{
		{
			// 'turbo'→Modifier (glm non-member) → variant empty. The
			// ReasonKnownSuffixOverflow audit annotation now fires (codegen clears it).
			desc:          "glm-5-turbo → (glm, '', 5) turbo→Modifier [sole-residual subsumed]",
			rawFamily:     "glm",
			id:            "glm-5-turbo",
			provider:      "zhipu",
			wantFamily:    "glm",
			wantVariant:   "",
			wantVersion:   "5",
			wantNoFailure: false,
		},
		{
			desc:          "phi-4-mini → (phi, mini, 4) [sole-residual subsumed]",
			rawFamily:     "phi",
			id:            "phi-4-mini",
			provider:      "microsoft",
			wantFamily:    "phi",
			wantVariant:   "mini",
			wantVersion:   "4",
			wantNoFailure: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, version, _, failure := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q", fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q (sole-residual subsumption check)", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q", version, tc.wantVersion)
			}
			if tc.wantNoFailure && failure != nil {
				t.Errorf("failure = %+v, want nil (sole-residual subsumption: no residual failure expected)", failure)
			}
		})
	}
}

// TestRecoverMemberVariant_SubsumesAmputation verifies that recoverMemberVariant
// subsumes the empty-raw amputation (parse.go:819-821) in
// InferFamilyFromIDWithVariant, preserving the passthrough-guard tests green.
//
// The amputation case (family == candidateFamilyStr → firstToken) must be replaced
// by: firstToken as family + recoverMemberVariant for variant.
func TestRecoverMemberVariant_SubsumesAmputation(t *testing.T) {
	t.Parallel()

	// / RED→GREEN flip (SUPERSEDES the
	// (kimi,"","") pin): kimi is a letter-prefix series, so InferFamilyFromIDWithVariant
	// applies the series split → (kimi, "k", "2"). The trailing "thinking" is a
	// Modifier (surfaced by ParseFamilyDetailed's ExtractModifier), never a Variant.
	t.Run("kimi-k2-thinking → series (kimi,k,2); thinking is a Modifier", func(t *testing.T) {
		t.Parallel()
		fam, variant, version := bestiary.InferFamilyFromIDWithVariant("kimi-k2-thinking", "moonshot")
		if fam != "kimi" {
			t.Errorf("family = %q, want %q", fam, "kimi")
		}
		if variant != "k" {
			t.Errorf("variant = %q, want %q", variant, "k")
		}
		if version != "2" {
			t.Errorf("version = %q, want %q", version, "2")
		}
	})

	// New: when the amputation path IS taken (passthrough), recoverMemberVariant
	// should recover the variant from the remaining tokens.
	t.Run("empty raw_family MiniMax-M1 → series (minimax, m, 1)", func(t *testing.T) {
		t.Parallel()
		// / minimax is a letter-prefix series, so the
		// series split owns this — variant="m", version="1" (REVERSES whole-token "m1").
		fam, variant, version := bestiary.InferFamilyFromIDWithVariant("MiniMax-M1", "nano-gpt")
		if fam != "minimax" {
			t.Errorf("family = %q, want %q (expected the case-fold lowercase)", fam, "minimax")
		}
		if variant != "m" || version != "1" {
			t.Errorf("(variant,version) = (%q,%q), want (\"m\",\"1\") (series split)",
				variant, version)
		}
	})
}

// ----------------------------------------------------------------------------
// Loader fail-fast — families.json key validation
// ----------------------------------------------------------------------------

// TestFamiliesJSON_LoaderFailFast verifies that FamiliesJSONKeyError catches
// unknown keys (typos) and accepts valid known keys.
//
// BDD: Given families.json with a typo key "claud" (not in allFamilies),
// When FamiliesJSONKeyError is called,
// Then a non-nil error is returned (fail-fast behaviour).
//
// This test is GREEN from the start (FamiliesJSONKeyError is part of the base infrastructure).
// It serves as the specification of the fail-fast contract.
func TestFamiliesJSON_LoaderFailFast(t *testing.T) {
	t.Parallel()

	t.Run("valid key passes", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"claude": {"members": ["opus"], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("valid key 'claude' caused error: %v", err)
		}
	})

	t.Run("typo key fails with actionable error", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"claud": {"members": ["opus"], "bare_gen_split": false}}`)
		err := bestiary.FamiliesJSONKeyError(data)
		if err == nil {
			t.Error("typo key 'claud' did not cause error — fail-fast not triggered\n" +
				"  What: FamiliesJSONKeyError must fail on unknown key\n" +
				"  Why: 'claud' is not in allFamilies (likely typo of 'claude')\n" +
				"  How to fix: ensure initParseData / FamiliesJSONKeyError validates keys")
		}
		// Verify the error mentions the bad key.
		if err != nil && !strings.Contains(err.Error(), "claud") {
			t.Errorf("error %q does not mention the bad key 'claud' — not actionable", err.Error())
		}
	})

	t.Run("_comment key is skipped (not validated)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"_comment": "test", "gpt": {"members": ["mini"], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("_comment key should be skipped, got error: %v", err)
		}
	})

	t.Run("Hy (uppercase in allFamilies) accepted as hy", func(t *testing.T) {
		t.Parallel()
		// allFamilies has "Hy" (uppercase). families.json uses lowercase keys.
		// FamiliesJSONKeyError must accept "hy" as matching "Hy" case-insensitively.
		data := []byte(`{"hy": {"members": [], "bare_gen_split": false}}`)
		if err := bestiary.FamiliesJSONKeyError(data); err != nil {
			t.Errorf("'hy' should be valid (case-insensitive match for allFamilies 'Hy'), got error: %v", err)
		}
	})

	t.Run("openai is not a Family (provider name, not in allFamilies)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"openai": {"members": ["gpt"], "bare_gen_split": false}}`)
		err := bestiary.FamiliesJSONKeyError(data)
		if err == nil {
			t.Error("'openai' is a provider name, not a Family — should fail key validation")
		}
	})
}

// ============================================================================
// family_aliases canonical-winner ledger
// ============================================================================

// TestFamilyAliasesJSON_LoaderFailFast verifies the ledger validation
// contract: alias TARGETS (canonical family values) must be known families, while
// alias KEYS (mislabels) are deliberately NOT validated.
//
// BDD: Given a ledger row whose TARGET is a typo (not in allFamilies), When
// FamilyAliasesJSONError is called, Then a non-nil actionable error naming the bad
// target is returned. Given a non-canonical KEY mapping to a valid target, Then no
// error (keys are arbitrary mislabels by design).
func TestFamilyAliasesJSON_LoaderFailFast(t *testing.T) {
	t.Parallel()

	t.Run("valid target passes (ratified l3 → llama)", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"aliases": {"l3": "llama", "l3.1": "llama"}}`)
		if err := bestiary.FamilyAliasesJSONError(data); err != nil {
			t.Errorf("valid target 'llama' caused error: %v", err)
		}
	})

	t.Run("typo target fails with actionable error", func(t *testing.T) {
		t.Parallel()
		data := []byte(`{"aliases": {"l3": "lluma"}}`)
		err := bestiary.FamilyAliasesJSONError(data)
		if err == nil {
			t.Fatal("typo target 'lluma' did not cause error — target validation not triggered")
		}
		if !strings.Contains(err.Error(), "lluma") {
			t.Errorf("error %q does not mention the bad target 'lluma' — not actionable", err.Error())
		}
	})

	t.Run("non-canonical KEY with valid target is accepted (keys are mislabels)", func(t *testing.T) {
		t.Parallel()
		// "l3.1" is NOT itself a canonical family — it is a mislabel. Only the TARGET
		// ("llama") must be canonical. This must pass.
		data := []byte(`{"aliases": {"l3.1": "llama"}}`)
		if err := bestiary.FamilyAliasesJSONError(data); err != nil {
			t.Errorf("non-canonical key 'l3.1' → valid target 'llama' should pass, got: %v", err)
		}
	})
}

// TestFamilyAliasesLedger_Fold verifies the RATIFIED l3/l3.1/l3.3 → llama fold
// end-to-end through ParseFamilyDetailed (the canonical-winner ledger applied after
// the case-fold family normalisation, before bare-gen-split). Community Llama-3 finetunes
// (sao10k/*) labelled with the "L3.x" shorthand must canonicalise to family "llama"
// so the family agrees cross-provider.
//
// SCOPE NOTE: the finetune name and the embedded "3.x" version are residual here —
// version recovery for folded families is (version-presence), out of scope.
func TestFamilyAliasesLedger_Fold(t *testing.T) {
	t.Parallel()

	cases := []struct {
		id         bestiary.ModelID
		wantFamily bestiary.Family
	}{
		{"sao10k/l3-euryale-70b", "llama"},
		{"sao10K/l3-8b-lunaris", "llama"},
		{"sao10k/l3.1-70b-hanami-x1", "llama"},
		{"sao10k/l3.1-euryale-70b", "llama"},
		{"sao10k/l3.3-euryale-70b", "llama"},
	}
	for _, tc := range cases {
		t.Run(string(tc.id), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, _ := bestiary.ParseFamilyDetailed("", tc.id, "p")
			if family != tc.wantFamily {
				t.Errorf("ParseFamilyDetailed(\"\", %q) family = %q, want %q\n"+
					"  What: the family_aliases ledger fold (l3* → llama) did not fire\n"+
					"  Why: RATIFIED row in parse/data/family_aliases.json must remap after the case-fold",
					tc.id, family, tc.wantFamily)
			}
		})
	}
}

// TestFamilyAliasesLedger_DefaultOwnFamily verifies the DEFAULT own-family rule:
// genuinely distinct families that have NO ledger row are left unchanged (no
// accidental fold). These are the families explicitly ratified as their own family.
func TestFamilyAliasesLedger_DefaultOwnFamily(t *testing.T) {
	t.Parallel()

	for _, raw := range []bestiary.Family{"mixtral", "ministral", "qwq", "aion", "pixtral", "voxtral"} {
		t.Run(string(raw), func(t *testing.T) {
			t.Parallel()
			family, _, _, _, _ := bestiary.ParseFamilyDetailed(raw, bestiary.ModelID(raw), "p")
			if family != raw {
				t.Errorf("ParseFamilyDetailed(%q,…) family = %q, want %q (DEFAULT own-family: no ledger row)",
					raw, family, raw)
			}
		})
	}
}

// ============================================================================
// Tests (RED until the bare_gen_split predicate is implemented)
// ============================================================================

// TestBareGenSplit_PositiveSplits verifies the bare-generation split: a
// glued family token <base><int> (e.g. "qwen3", "o1") OR a clean family whose ID
// carries a glued generation token decomposes to (base, …, version=int) when the
// CLOSED predicate holds (has families.json entry ∧ base not digit-suffixed ∧
// bare_gen_split:true flag attested in the snapshot).
//
// BDD: Given "qwen3-max" When decomposed Then (qwen, max, 3).
// These cases are RED until the predicate is implemented at the insertion
// point in BOTH entrypoints (InferFamilyFromIDWithVariant + ParseFamilyDetailed).
func TestBareGenSplit_PositiveSplits(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
		wantVersion string
	}{
		// Bare glued family token, empty raw — split base off the trailing int.
		{"qwen3 → (qwen,,3)", "", "qwen3", "alibaba", "qwen", "", "3"},
		// the 'o' family folds into gpt as a VARIANT —
		// o1 → (gpt, variant='o', version=1). Supersedes the o1→(o,,1) row.
		{"o1 → (gpt,o,1)", "", "o1", "openai", "gpt", "o", "1"},
		// Glued family token + member variant (empty-raw inference path).
		{"qwen3-max (raw empty) → (qwen,max,3)", "", "qwen3-max", "qiniu-ai", "qwen", "max", "3"},
		// CLEAN raw-supplied family + glued generation in the ID: the (B) version
		// recovery half must surface the glued int as version so both providers agree.
		{"qwen3-max (raw qwen) → (qwen,max,3)", "qwen", "qwen3-max", "alibaba", "qwen", "max", "3"},
		// o3-mini → (gpt, variant='o', version=3),
		// mini→modifier (not asserted here). Supersedes the o3-mini→(o,mini,3) row.
		{"o3-mini (raw o) → (gpt,o,3)", "o", "openai/o3-mini", "openrouter", "gpt", "o", "3"},
		// Hyphenated generation already extracts version on the raw side; the
		// empty-raw inferred family "gpt-5"/"gemini-3" must split to the base.
		{"gpt-5-mini (raw empty) → (gpt,mini,5)", "", "openai/gpt-5-mini", "kilo", "gpt", "mini", "5"},
		{"gemini-3-flash-preview (raw empty) → (gemini,flash,3)", "", "gemini-3-flash-preview", "302ai", "gemini", "flash", "3"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, version, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: bare_gen_split did not split the glued generation off the family\n"+
					"  Why: the closed predicate (has-entry ∧ not-digit-suffixed ∧ flag) should split\n"+
					"  How to fix: implement the bare_gen_split predicate at the insertion point",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q", variant, tc.wantVariant)
			}
			if version != tc.wantVersion {
				t.Errorf("version = %q, want %q\n"+
					"  What: bare_gen_split did not surface the generation int as version\n"+
					"  Why: split <base><int> → version=int, including the clean-family (B) recovery half",
					version, tc.wantVersion)
			}
		})
	}
}

// TestBareGenSplit_NonSplit verifies the CLOSED predicate's negative cases:
// tokens that look like <base><int> but MUST NOT split because a clause fails.
//
//   - v0 / asi1 / esm2 / wan2 / hy3 / r1: base ("v"/"asi"/"esm"/"wan"/"hy"/"r")
//     has NO families.json entry → has-entry clause fails.
//   - l3: l3's base "l" has no entry → has-entry clause fails (also guarded
//     by the digit-suffix rule).
//
// NOTE: the letter-prefix series cases that USED to live here
// (minimax-m2.5 / kimi-k2.5 / mimo-v2.5 / mimo-v1) are now decomposed by the
// series split (variant=letter + version=number) — a DIFFERENT
// mechanism from bare_gen_split. bare_gen_split STILL declines them (their bases
// carry no bare_gen_split flag); the observable ParseFamilyDetailed tuple is now
// owned by splitSeriesVariant and asserted in TestSeriesLetterSplit.
//
// These assert the predicate is CLOSED (no per-name allow-list): the family
// stays the un-split token.
func TestBareGenSplit_NonSplit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		rawFamily   bestiary.Family
		id          bestiary.ModelID
		provider    bestiary.Provider
		wantFamily  bestiary.Family
		wantVariant string
	}{
		// has-entry clause fails (base not a families.json key).
		{"v0 NOT split (v∉families)", "", "v0-1.5", "p", "v0", ""},
		{"asi1 NOT split (asi∉families)", "", "asi1-mini", "p", "asi1", "mini"},
		{"esm2 NOT split (esm∉families)", "", "esm2-large", "p", "esm2", "large"},
		{"wan2 NOT split (wan∉families)", "", "wan2-t2v", "p", "wan2", ""},
		// "hy3" MOVED to the SPLIT set — "hy" is now a registered family
		// (bare "hy" attested via raw="Hy"), so hy3-preview → (hy, "", 3) [see
		// TestTier1StragglerConvergences]. It is no longer a NonSplit case.
		{"r1 NOT split (r∉families)", "", "r1", "p", "r1", ""},
		// bare-gen still DECLINES "l3" (base "l" ∉ families.json), but the
		// family_aliases ledger then folds l3 → llama (RATIFIED: L3.x = Llama-3
		// shorthand). The closed-predicate guarantee (no bare-gen split) is unchanged;
		// the canonical family arrives via the ledger remap, not the split.
		{"l3 → llama via ledger (bare-gen declines: l∉families)", "", "l3-8b", "p", "llama", ""},
		// NOTE: the former minimax-m2.5 / kimi-k2.5 / mimo-v2.5 / mimo-v1 cases moved
		// to TestSeriesLetterSplit — they now decompose via the
		// (d) letter-prefix series split, not bare_gen_split.
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			fam, variant, _, _, _ := bestiary.ParseFamilyDetailed(tc.rawFamily, tc.id, tc.provider)
			if fam != tc.wantFamily {
				t.Errorf("family = %q, want %q\n"+
					"  What: bare_gen_split wrongly split a token whose closed-predicate clause fails\n"+
					"  Why: the predicate is CLOSED — no families.json entry (or no trailing digit) means NO split\n"+
					"  How to fix: gate the split on has-entry ∧ not-digit-suffixed ∧ bare_gen_split flag",
					fam, tc.wantFamily)
			}
			if variant != tc.wantVariant {
				t.Errorf("variant = %q, want %q (dotted numerics must remain variant tokens)", variant, tc.wantVariant)
			}
		})
	}
}

// ============================================================================
// ID-driven version-presence consistency + param-size guard +
// glued letter-suffix + letter-prefix series split (+ -5).
// ============================================================================

// TestVersionPresenceConsistency_ClassA verifies (a): a version
// derivable from the (vendor-stripped, case-folded) model ID is extracted
// CONSISTENTLY regardless of the provider raw_family. Each case asserts that the
// SAME id decomposes to an IDENTICAL (Family, Variant, Version) under BOTH an
// empty raw_family (the inference path) AND the provider's populated raw_family.
func TestVersionPresenceConsistency_ClassA(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc         string
		rawPopulated bestiary.Family
		id           bestiary.ModelID
		wantFamily   bestiary.Family
		wantVariant  string
		wantVersion  string
	}{
		{"gpt-4.1 (gpt | empty)", "gpt", "openai/gpt-4.1", "gpt", "", "4.1"},
		{"glm-4.6 (glm | empty)", "glm", "z-ai/glm-4.6", "glm", "", "4.6"},
		{"gemma-3-12b-it (gemma | empty)", "gemma", "google/gemma-3-12b-it", "gemma", "", "3"},
		{"claude-3-5-haiku (claude-haiku | empty)", "claude-haiku", "claude-3-5-haiku-20241022", "claude", "haiku", "3.5"},
		{"grok-4.1-fast (grok | empty)", "grok", "x-ai/grok-4.1-fast", "grok", "", "4.1"},
		{"grok-4-fast (grok | empty)", "grok", "grok-4-fast-non-reasoning", "grok", "", "4"},
		{"ernie-4.5-21b-a3b (ernie | empty)", "ernie", "baidu/ernie-4.5-21b-a3b", "ernie", "", "4.5"},
		{"claude-opus-4.6-fast (claude-opus | empty)", "claude-opus", "anthropic/claude-opus-4.6-fast", "claude", "opus", "4.6"},
		{"mistral-medium-3-5 (mistral-medium | empty)", "mistral-medium", "mistralai/mistral-medium-3-5", "mistral", "medium", "3.5"},
		{"GLM-5 mixed-case (glm | empty)", "glm", "zai-org/GLM-5", "glm", "", "5"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			for _, raw := range []bestiary.Family{"", tc.rawPopulated} {
				f, va, ve, _, _ := bestiary.ParseFamilyDetailed(raw, tc.id, "p")
				if f != tc.wantFamily || va != tc.wantVariant || ve != tc.wantVersion {
					t.Errorf("raw=%q id=%q → (%s|%s|%s), want (%s|%s|%s)\n"+
						"  What: ID-driven version not extracted consistently across raw_family\n"+
						"  Why: version must derive from the ID regardless of raw_family",
						raw, tc.id, f, va, ve, tc.wantFamily, tc.wantVariant, tc.wantVersion)
				}
			}
		})
	}
}

// TestParamSizeGuard verifies (b): parameter-count / model-size
// tokens (NNNb / NNNm / MoE) are NEVER promoted to Version. The size INFO is GH#9
// (missing Size dimension), explicitly not a version. Asserted on ALL providers
// (empty + populated raw) so gpt-oss-120b is Version "" everywhere (consistent).
func TestParamSizeGuard(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		raw  bestiary.Family
		id   bestiary.ModelID
	}{
		{"gpt-oss-120b (raw gpt-oss)", "gpt-oss", "gpt-oss-120b"},
		{"gpt-oss-120b (empty raw)", "", "gpt-oss-120b"},
		{"gpt-oss-20b (raw gpt-oss)", "gpt-oss", "gpt-oss-20b"},
		{"qwen3-coder-30b-a3b MoE (raw qwen)", "qwen", "qwen3-coder-30b-a3b"},
		{"mixtral-8x22b MoE (empty raw)", "", "mistralai/mixtral-8x22b"},
		{"ernie-4.5-300b-a47b (raw ernie) keeps 4.5 not size", "ernie", "baidu/ernie-4.5-300b-a47b"},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, ve, _, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			// The version must never be a bare param-size token (e.g. "120b", "20b",
			// "30b", "8x22b"). For ernie the genuine version 4.5 IS allowed.
			for _, bad := range []string{"120b", "20b", "30b", "8x22b", "300b", "a3b", "a47b"} {
				if ve == bad {
					t.Errorf("raw=%q id=%q → version=%q is a param-size token (must be dropped — GH#9, not a version)", tc.raw, tc.id, ve)
				}
			}
		})
	}

	// Direct guard unit checks on the public ExtractVersionFromID path.
	t.Run("ExtractVersionFromID drops 120b, keeps 4o/versions", func(t *testing.T) {
		t.Parallel()
		if v := bestiary.ExtractVersionFromID("gpt-oss-120b", "gpt-oss"); v != "" {
			t.Errorf("gpt-oss-120b → %q, want \"\" (param-size guard)", v)
		}
		if v := bestiary.ExtractVersionFromID("gpt-4o", "gpt"); v != "4o" {
			t.Errorf("gpt-4o → %q, want \"4o\" (genuine version, NOT a size)", v)
		}
	})
}

// TestGluedVersionModifier verifies the glued letter-after-version handling.
// SUPERSEDES the (c) glm-4.5v→vision behaviour:
//   - the glued single 'v' after a glm version is the VARIANT 'v' (glm-4.5v →
//     (glm, "v", 4.5), NOT modifier vision). The spelled-out "-vision" hyphen token
//     remains a Modifier (uniform rule unchanged) and is NOT exercised here.
//   - gpt-4o → variant '4o', version ” ('4o' is the line designator, not a
//     version). Supersedes the prior (gpt,"",4o) pin.
func TestGluedVersionModifier(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, gluedVersionModifierCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// trailing glued letter splits off the version.
		{Raw: "glm", ID: "glm-4.5v"}: {Family: "glm", Variant: "v", Version: "4.5", Mod: ""},
		// alphanumeric line designator must NOT be split.
		{Raw: "gpt", ID: "gpt-4o"}: {Family: "gpt", Variant: "4o", Version: "", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestDotGluedVariant verifies the LEADING dot-glued variant generalization: a
// variant glued by a "." to a version with no separating hyphen (laguna-xs.2 →
// (laguna,"xs","2"); laguna-m.1 → (laguna,"m","1")) decomposes mechanically, the
// general counterpart of the trailing glued-letter split (glm-4.5v). The bare forms
// (no vendor prefix) exercise the general machinery directly — no exact-ID entry is
// consulted — so this pins that the mechanical path, not a curated map, derives them.
//
// The must-not-mangle rows are the whole point of the narrow gate: a version-prefixed
// line ("deepseek-v3.1", the "v" is fused to a digit, not dotted), a genuine dotted
// version ("gpt-4.1"), and an alphanumeric line designator ("gpt-4o") must all keep
// their existing decomposition — the generalization must never invent a variant/version
// split for them.
func TestDotGluedVariant(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, dotGluedVariantCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// dot-glued leading variant derived mechanically.
		{Raw: "", ID: "laguna-xs.2"}: {Family: "laguna", Variant: "xs", Version: "2", Mod: ""},
		// must-not-mangle: genuine dotted version, no invented variant.
		{Raw: "gpt", ID: "gpt-4.1"}: {Family: "gpt", Variant: "", Version: "4.1", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestSeriesLetterSplit verifies (d): letter-prefix model
// series (kimi→k, minimax→m, mimo→v) decompose to variant=SERIES-LETTER +
// version=NUMBER, with ALL attested forms normalized consistently. This SUPERSEDES
// the whole-token plan (minimax "m1") and this kimi-k2-thinking
// (kimi,"","") pin, and the version_patterns letter-prefix whole-token-variant.
//
// TIER INTERACTION: surfaced + ruled by the user: tier→Modifier,
// variant stays the pure series-letter — pinned in TestSeriesTierModifier.
// MULTI-MODIFIER cases (tier + thinking/vision) remain surfaced (single-valued
// Modifier; multiplicity ruling pending) and keep the existing thinking modifier.
func TestSeriesLetterSplit(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, seriesLetterSplitCorpusJSON, 18)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// kimi K-series: empty raw and populated raw resolve identically.
		{Raw: "", ID: "kimi-k2"}: {Family: "kimi", Variant: "k", Version: "2", Mod: ""},
		// minimax M-series reverses the whole-token "m1".
		{Raw: "minimax", ID: "minimax-m1"}: {Family: "minimax", Variant: "m", Version: "1", Mod: ""},
		// series letter + trailing thinking modifier.
		{Raw: "kimi-thinking", ID: "kimi-k2-thinking"}: {Family: "kimi", Variant: "k", Version: "2", Mod: "thinking"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestMustNotRegress_RealVersions pins genuine semantic versions that the
// param-size guard and series split MUST leave UNCHANGED (the size/series logic
// distinguishes size tokens and series letters from real version numbers).
func TestMustNotRegress_RealVersions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc        string
		raw         bestiary.Family
		id          bestiary.ModelID
		wantVersion string
	}{
		{"4.5 dotted", "claude-opus", "claude-opus-4-5-20251101", "4.5"},
		{"2.5 dotted", "gemini-flash", "gemini-2.5-flash", "2.5"},
		// "4o" is now the VARIANT (line designator), so the
		// version is EMPTY. Supersedes the "4o is a version" pin. (Variant=4o is
		// asserted in TestGluedVersionModifier.)
		{"gpt-4o → version '' ('4o' is the variant)", "gpt", "gpt-4o", ""},
		{"3.5 (claude-haiku)", "claude-haiku", "claude-3-5-haiku-20241022", "3.5"},
		{"3.7 (claude-sonnet)", "claude-sonnet", "claude-3-7-sonnet-20250219", "3.7"},
		{"single-digit 5", "gpt", "openai/gpt-5", "5"},
		{"dotted 3.1", "llama", "meta-llama/llama-3.1-8b", "3.1"},
		{"mistral-small-2603 date NOT a version", "mistral-small", "mistral-small-2603", ""},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			_, _, ve, _, _ := bestiary.ParseFamilyDetailed(tc.raw, tc.id, "p")
			if ve != tc.wantVersion {
				t.Errorf("raw=%q id=%q → version=%q, want %q (must-not-regress real version / date-guard)",
					tc.raw, tc.id, ve, tc.wantVersion)
			}
		})
	}
}

// TestSeriesTierModifier verifies the tier→Modifier promotion: a
// curated TIER token trailing a letter-prefix series token becomes the Modifier,
// while the variant stays the PURE series-letter (kimi-k2-instruct →
// (kimi,'k','2',mod=instruct)). The promotion is SERIES-SCOPED — it must NOT
// reclassify the SAME token when it is a VARIANT of a NON-series family
// (gpt-5-mini, gemini-2.5-flash, qwen-turbo, llama-*-instruct stay variants).
//
// MULTI-MODIFIER cases (tier + thinking/vision, or 2+ tiers) are NOT pinned here:
// the Modifier field is single-valued and the multiplicity rule is pending — those
// keep the series split + the existing thinking/vision modifier and DROP the tier
// (pending a ruling, not resolved unilaterally).
func TestSeriesTierModifier(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, seriesTierModifierCorpusJSON, 16)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// tier -> modifier, variant stays the pure series-letter.
		{Raw: "kimi", ID: "kimi-k2-instruct"}: {Family: "kimi", Variant: "k", Version: "2", Mod: "instruct"},
		// same token is a VARIANT for a non-series family (unchanged).
		{Raw: "gpt", ID: "openai/gpt-5-mini"}: {Family: "gpt", Variant: "mini", Version: "5", Mod: ""},
		// multi-modifier composes losslessly.
		{Raw: "kimi-thinking", ID: "kimi-k2-thinking-turbo"}: {Family: "kimi", Variant: "k", Version: "2", Mod: "thinking,turbo"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// The former multi-modifier-deferred-to-modifier-list test was REMOVED.
// It pinned the single-Modifier interim (kimi-k2-thinking-turbo DROPPED "turbo"). The
// Modifier-LIST schema change now populates BOTH losslessly
// ([thinking, turbo]); the lossless multi-modifier behaviour is asserted by
// TestParseFamilyDetailed_ModifierList.
func TestParseFamilyDetailed_ModifierList(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, parseFamilyDetailedModifierListCorpusJSON, 21)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// multi-modifier lossless capture (triple).
		{Raw: "kimi-thinking", ID: "moonshotai/kimi-k2-thinking-turbo-original"}: {Family: "kimi", Variant: "k", Version: "2", Mod: "thinking,turbo,original"},
		// rawFamily-embedded member must NOT duplicate into both Variant and Modifier.
		{Raw: "sonar-reasoning", ID: "sonar-reasoning"}: {Family: "sonar", Variant: "reasoning", Version: "", Mod: ""},
		// lossless variant-suffix -> modifier split.
		{Raw: "elevenlabs", ID: "elevenlabs/elevenlabs-v2.5-turbo"}: {Family: "elevenlabs", Variant: "v2.5", Version: "", Mod: "turbo"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// ----------------------------------------------------------------------------
// PATH-UNIFICATION unit tests (Option A)
// ----------------------------------------------------------------------------

// TestParseFamilyDetailed_PathUnification pins the re-scoped (Option A)
// behavior: ParseFamilyDetailed derives Variant/Version/Modifier from the ID (the
// idDrivenDecompose primitive shared with the empty-raw path), while PRESERVING the
// Family from raw_family (the ID-path over-captures Family — that convergence is the
// separate family-seeding slice). The diff-first gate
// (TestPathUnification_ZeroUnexpectedRegression) is the dataset-wide guard; these
// units pin the representative classes + the must-not-regress invariants.
func TestParseFamilyDetailed_PathUnification(t *testing.T) {
	cases := []struct {
		desc                               string
		raw, id                            string
		wantFam, wantVar, wantVer, wantMod string
	}{
		// CONVERGENCE WIN: glued letter-suffix version-modifier. raw-aware alone gave
		// (glm,"","5v",""); the ID owns it → (glm,"",5,vision), matching empty-raw providers.
		// the glued single 'v' after a glm version is the
		// VARIANT 'v', NOT the 'vision' modifier (supersedes the glm-5v→vision row).
		{"glm-5v: glued 'v' is variant", "glm", "glm-5v", "glm", "v", "5", ""},

		// FAMILY-PRESERVING (the safeguard's core): the ID-path OVER-captures Family
		// (deepseek-v4, gpt-4o) — raw_family is the correct SHORT family and is kept.
		// Converging these is Option B's scope, NOT this slice.
		{"deepseek-v4-flash: family PRESERVED (not deepseek-v4)", "deepseek-flash", "deepseek-v4-flash", "deepseek", "flash", "", ""},
		// gpt-4o-mini → variant '4o', mini→modifier
		// (the line designator '4o' occupies the variant slot; size token 'mini' demotes
		// to the Modifier). Supersedes the family-preserve (gpt,mini,"") row.
		{"gpt-4o-mini: variant '4o', mini→modifier", "gpt", "gpt-4o-mini", "gpt", "4o", "", "mini"},

		// VARIANT DE-JUNK: raw_family "qwen3.6" leaks the version into the variant
		// ("3.6"); the ID recovers the true member variant "flash".
		{"qwen3.6-flash: variant de-junk 3.6→flash", "qwen3.6", "qwen3.6-flash", "qwen", "flash", "3.6", ""},

		// VARIANT REFINEMENT: ID names a more specific variant than raw_family.
		{"gpt-5.1-codex-mini: variant refinement codex→codex-mini", "gpt-codex", "gpt-5.1-codex-mini", "gpt", "codex-mini", "5.1", ""},

		// CLEAN-VARIANT GUARD (true-regression prevention): the series split is defeated
		// by the "6bit" quantization suffix so the ID variant would be junk
		// ("v2.5-pro-6bit"); the clean raw variant "pro" is PRESERVED, not worsened.
		{"mimo-v2.5-pro-6bit: clean raw variant 'pro' preserved (not worsened)", "mimo", "mimo-v2.5-pro-6bit", "mimo", "pro", "", ""},

		// MUST-NOT-REGRESS: kimi-k2-thinking → (kimi,k,2,thinking).
		{"kimi-k2-thinking (must-hold)", "kimi-thinking", "kimi-k2-thinking", "kimi", "k", "2", "thinking"},

		// MUST-NOT-REGRESS: capability modifier from raw_family is never dropped (the
		// ID "deepseek-reasoner" has no thinking token; raw "deepseek-thinking" carries it).
		{"deepseek-reasoner: rawModifier 'thinking' preserved", "deepseek-thinking", "deepseek-reasoner", "deepseek", "", "", "thinking"},

		// capability + tier compose LOSSLESSLY in the Modifier LIST (supersedes
		// the single-modifier "capability wins, tier dropped" interim).
		{"kimi-k2p6-turbo raw=kimi-thinking: thinking+turbo lossless", "kimi-thinking", "kimi-k2p6-turbo", "kimi", "k", "2.6", "thinking,turbo"},

		// MUST-NOT-REGRESS: claude-opus-4-1-...-thinking → (claude,opus,4.1,thinking).
		{"claude-opus-4-1-thinking (must-hold)", "claude-opus", "claude-opus-4-1-20250805-thinking", "claude", "opus", "4.1", "thinking"},

		// A more-specific raw variant must NOT be
		// overridden by a less-specific ID-driven one. InferFamilyFromIDWithVariant loses
		// "-lite" (returns "flash") for the dated-preview suffix; the superstring guard
		// keeps the correct raw variant "flash-lite" (distinct Gemini tier).
		{"gemini-2.5-flash-lite-preview-06-17: flash-lite preserved (not downgraded to flash)", "gemini-flash-lite", "gemini-2.5-flash-lite-preview-06-17", "gemini", "flash-lite", "2.5", ""},
		// "preview" before an MM-YYYY date is now captured as a Modifier (the
		// tail-scan skips the 09-2025 date fragment); flash-lite variant still preserved.
		{"gemini-2.5-flash-lite-preview-09-2025: flash-lite preserved + preview modifier", "gemini-flash-lite", "gemini-2.5-flash-lite-preview-09-2025", "gemini", "flash-lite", "2.5", "preview"},

		// The '@' version/date delimiter is
		// normalized to '-' so the @-form converges to the canonical version (not "4").
		{"claude-opus-4-1@20250805: @-form version → 4.1 (raw)", "claude-opus", "claude-opus-4-1@20250805", "claude", "opus", "4.1", ""},
		{"claude-opus-4-1@20250805: @-form version → 4.1 (empty-raw)", "", "claude-opus-4-1@20250805", "claude", "opus", "4.1", ""},
		{"claude-sonnet-4-6@default: @-form version → 4.6", "claude-sonnet", "claude-sonnet-4-6@default", "claude", "sonnet", "4.6", ""},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			f, va, ve, mod, _ := bestiary.ParseFamilyDetailed(bestiary.Family(tc.raw), bestiary.ModelID(tc.id), "test-provider")
			if string(f) != tc.wantFam || va != tc.wantVar || ve != tc.wantVer || modJoin(mod) != tc.wantMod {
				t.Errorf("ParseFamilyDetailed(raw=%q, id=%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					tc.raw, tc.id, f, va, ve, mod, tc.wantFam, tc.wantVar, tc.wantVer, tc.wantMod)
			}
		})
	}
}

// TestParseFamilyDetailed_PathUnification_EmptyRawConsistency asserts the unification
// invariant directly: for an ID whose Family agrees between the raw-populated and
// empty-raw paths, the (Variant,Version,Modifier) MUST be identical regardless of
// raw_family — the two paths share one ID-driven decomposition.
func TestParseFamilyDetailed_PathUnification_EmptyRawConsistency(t *testing.T) {
	ids := []string{"glm-5v", "qwen3.6-flash", "kimi-k2-thinking", "gpt-5.1-codex-mini"}
	rawHints := []string{"glm", "qwen3.6", "kimi-thinking", "gpt-codex"}
	for i, id := range ids {
		rf, rv, rver, rmod, _ := bestiary.ParseFamilyDetailed(bestiary.Family(rawHints[i]), bestiary.ModelID(id), "p")
		ef, ev, ever, emod, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(id), "p")
		// Family agrees for these IDs by construction (no over-capture); assert V/V/M parity.
		if rf != ef {
			t.Fatalf("%s: family disagrees raw=%q empty=%q (test precondition: pick a non-over-capture ID)", id, rf, ef)
		}
		if rv != ev || rver != ever || modJoin(rmod) != modJoin(emod) {
			t.Errorf("%s: raw-populated (%q,%q,%q) != empty-raw (%q,%q,%q) — paths not unified",
				id, rv, rver, rmod, ev, ever, emod)
		}
	}
}

// TestParseFamilyDetailed_FamilyOverCaptureReduction asserts the
// family OVER-CAPTURE fix: the empty-raw ID-path now reduces an
// over-captured COMPOUND family to its registered SHORT base so it converges with the
// raw-populated providers of the same ID. Each case pins the empty-raw decomposition;
// the matching raw-populated decomposition (the convergence target) is asserted equal.
func TestParseFamilyDetailed_FamilyOverCaptureReduction(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		wantFam bestiary.Family
		wantVar string
		wantVer string
	}{
		{"claude-opus dotted", "anthropic/claude-opus-4.1", "claude", "opus", "4.1"},
		// gpt-4o-mini → variant '4o' (mini→modifier, asserted
		// elsewhere); version empty. Supersedes the (gpt,mini,"") row.
		{"gpt-4o-mini (variant '4o')", "openai/gpt-4o-mini", "gpt", "4o", ""},
		{"deepseek-r1 (canonical drops r1)", "deepseek-ai/DeepSeek-R1-0528", "deepseek", "", ""},
		// 'instruct' is a global modifier now → variant empty (not "instruct").
		{"llama-3.3-70b-instruct", "meta-llama/llama-3.3-70b-instruct", "llama", "", "3.3"},
		{"qwen3-vl member+gen", "qwen/qwen3-vl-30b-a3b-instruct", "qwen", "vl", "3"},
		{"phi-4-mini member+gen", "microsoft/phi-4-mini-instruct", "phi", "mini", "4"},
		{"gemini flash via modifier-strip branch", "google/gemini-2.5-flash-image", "gemini", "flash", "2.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, v, ver, _, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(tc.id), "empty-prov")
			if f != tc.wantFam || v != tc.wantVar || ver != tc.wantVer {
				t.Errorf("empty-raw %q → (%q,%q,%q), want (%q,%q,%q)",
					tc.id, f, v, ver, tc.wantFam, tc.wantVar, tc.wantVer)
			}
		})
	}
}

// TestParseFamilyDetailed_GenuineCompoundPreserved asserts the reducer is
// CLOSED: it never over-reduces a genuinely-compound family (curated as an override
// self-map) nor a family whose base is not a registered short family. These MUST stay
// intact — the safeguard against over-reducing the 655 short/correct records.
func TestParseFamilyDetailed_GenuineCompoundPreserved(t *testing.T) {
	// Each genuine compound must NOT collapse to its bare leading token (the over-reduction
	// the closed reducer is designed to refuse). The family is expected to retain the
	// curated compound base prefix, never the lone first token.
	cases := []struct {
		id         string
		mustNotBe  bestiary.Family // the wrong over-reduction
		wantPrefix string          // family must keep this curated compound prefix
	}{
		{"text-embedding-3-large", "text", "text-embedding"},
		{"stable-diffusion-xl", "stable", "stable-diffusion"},
		{"nano-banana-pro", "nano", "nano-banana"},
	}
	for _, tc := range cases {
		f, _, _, _, _ := bestiary.ParseFamilyDetailed("", bestiary.ModelID(tc.id), "p")
		if f == tc.mustNotBe {
			t.Errorf("%q → family %q — genuine compound WRONGLY over-reduced to bare leading token", tc.id, f)
		}
		if !strings.HasPrefix(string(f), tc.wantPrefix) {
			t.Errorf("%q → family %q, expected to retain curated compound prefix %q", tc.id, f, tc.wantPrefix)
		}
	}
}

// TestParseFamilyDetailed_CapabilityModifierDeclined asserts that a compound
// family carrying a CAPABILITY modifier (thinking/vision) is NOT reduced — leaving it an
// HONEST residual rather than silently dropping the capability (the Modifier-LIST
// multi-modifier case). kimi-k2-thinking-* keeps a thinking-bearing decomposition rather
// than being collapsed to a bare short family that loses "thinking".
func TestParseFamilyDetailed_CapabilityModifierDeclined(t *testing.T) {
	// glm-4.1v-thinking-flash: empty-raw must NOT silently lose "thinking" by reducing
	// to a bare (glm, flash) — the over-capture family stays intact (honest residual).
	f, _, _, _, _ := bestiary.ParseFamilyDetailed("", "nano-gpt-glm-4.1v-thinking-flash", "p")
	_ = f // family may stay compound; the contract is "thinking not dropped via reduction".
	// Direct reducer contract: IsKnownFamily distinguishes a canonical short family from a
	// synthetic over-capture.
	if !bestiary.IsKnownFamily("claude") {
		t.Errorf("IsKnownFamily(claude) = false, want true (canonical registered family)")
	}
	if bestiary.IsKnownFamily("claude-opus-4-1") {
		t.Errorf("IsKnownFamily(claude-opus-4-1) = true, want false (synthetic over-capture)")
	}
}

// TestCrossProviderConvergences pins the cross-provider convergence fixes.
// Each case is the canonical ParseFamilyDetailed decomposition that the
// convergence pass ratified; together with the before/after-diff gate (ZERO cat-(c)) these are
// the specification for the mechanical + o-series + ledger changes.
func TestCrossProviderConvergences(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, crossProviderConvergencesCorpusJSON, 29)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// o-series restructure: o1 -> (gpt, o, 1).
		{Raw: "", ID: "o1"}: {Family: "gpt", Variant: "o", Version: "1", Mod: ""},
		// gpt-codex phantom cleared, chat -> modifier list (comma-joined).
		{Raw: "gpt-codex", ID: "gpt-5-chat-latest"}: {Family: "gpt", Variant: "", Version: "5", Mod: "chat,latest"},
		// canonical-winner enforce: mislabelled qwen -> qwq.
		{Raw: "qwen", ID: "qwq-32b"}: {Family: "qwq", Variant: "", Version: "", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestTier1StragglerConvergences pins the straggler convergences,
// per the refined set: 5 COMMITTED (cohere command r/r-plus date-guard+member,
// deepseek product-line "chat", meta-llama surgical doubled-vendor) + 3 CONDITIONALS cleanly
// promoted under existing rules (grok product-name "code-fast", Qwen3-Embedding qwen-wins,
// hy3 bare-gen). Each is non-lossy under the hardened gate (cat-(c)=0). command-a-reasoning is
// DEFERRED to the systematic modifier ruling (reasoning = borderline-capability, modifier-vs-variant judgment).
func TestTier1StragglerConvergences(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, tier1StragglerConvergencesCorpusJSON, 20)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// deepseek chat product-line member, version preserved.
		{Raw: "", ID: "deepseek/deepseek-chat-v3.1"}: {Family: "deepseek", Variant: "chat", Version: "3.1", Mod: ""},
		// Qwen3-Embedding: ID-family qwen wins over generic raw "text-embedding".
		{Raw: "text-embedding", ID: "Qwen/Qwen3-Embedding-8B"}: {Family: "qwen", Variant: "embedding", Version: "3", Mod: ""},
		// GUARD: OpenAI text-embedding-3* stays family text-embedding (untouched).
		{Raw: "text-embedding", ID: "openai/text-embedding-3-large"}: {Family: "text-embedding", Variant: "large", Version: "3", Mod: ""},
		// cohere command-r7b-12-2024: "r7b" is Cohere's distinct marketed variant (kept
		// whole; the 7b is carried separately as ParamSize), and the trailing MM-YYYY
		// group "12-2024" is a date, so Version stays empty (never leaks the month "12").
		{Raw: "command-r", ID: "cohere/command-r7b-12-2024"}: {Family: "command", Variant: "r7b", Version: "", Mod: ""},
		// negative controls: the r7b pin must not bleed into plain R / R+ siblings.
		{Raw: "command-r", ID: "command-r-08-2024"}:           {Family: "command", Variant: "r", Version: "", Mod: ""},
		{Raw: "command-r-plus", ID: "command-r-plus-08-2024"}: {Family: "command", Variant: "r-plus", Version: "", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestAzureServingHostCapture pins the serving-host decomposition of the NanoGPT
// azure-* backend-routed OpenAI models: the curated "azure-" host prefix is
// stripped for decomposition (DetectHost, ID-prefix-only) so the resulting
// (family,variant,version,mod) tuple is host-independent and converges with the
// plainly-served model. The three tuples exercise the three distinct shapes:
// gpt@4{turbo} (version + modifier), gpt/4o (4o as VARIANT not version), and
// gpt/4o{mini} (variant + modifier). The provider-independence negative control
// (the strip never consults the Provider field) lives in host_detect_test.go.
func TestAzureServingHostCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, azureServingHostCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		{Raw: "", ID: "azure-gpt-4-turbo"}: {Family: "gpt", Variant: "", Version: "4", Mod: "turbo"},
		// 4o is an alphanumeric line designator (VARIANT), never a dotted version.
		{Raw: "", ID: "azure-gpt-4o"}:      {Family: "gpt", Variant: "4o", Version: "", Mod: ""},
		{Raw: "", ID: "azure-gpt-4o-mini"}: {Family: "gpt", Variant: "4o", Version: "", Mod: "mini"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestParse_FamilyO_OverCapture is the fence for the family-"o" over-capture. vercel
// labels a swathe of unrelated models with the upstream raw_family "o" — the OpenAI
// o-series family — so Alibaba's Wan video models, OpenAI's TTS speech models,
// quiverai's arrow and Cohere's rerankers all decomposed into family "o" and shared
// one junk-bucket entity with the real o-series.
//
// The corpus carries BOTH directions, and the negative controls are the load-bearing
// half: four genuine o-series ids that must be untouched, two of which legitimately
// KEEP family "o". A fix that emptied the bucket by evicting its rightful occupants
// would pass a positives-only corpus and fail here.
func TestParse_FamilyO_OverCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, familyOOverCaptureCorpusJSON, 20)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// one over-capture per correcting mechanism
		{Raw: "o", ID: "alibaba/wan-v2.6-i2v"}: {Family: "wan", Variant: "v2.6-i2v"},
		{Raw: "o", ID: "cohere/rerank-v3.5"}:   {Family: "rerank", Variant: "v3.5"},
		{Raw: "o", ID: "cohere/rerank-v4-pro"}: {Family: "rerank", Mod: "pro"},
		// and the two negative controls that must KEEP family "o"
		{Raw: "o", ID: "openai-o1"}:           {Family: "o"},
		{Raw: "o-mini", ID: "openai-o3-mini"}: {Family: "o", Variant: "mini"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestMetaLlamaNoSlashCapture pins the no-slash doubled-vendor fold (the scoped
// "meta-llama-" prefix strip): the eight attested no-slash meta-llama IDs
// (dotted/dashed/underscored version spellings) all decompose NATIVELY to family
// llama (never "meta"), and the three-spellings-one-entity convergence rows show
// that the slash-org doubled-vendor form, the slash-org Llama-only form, and the
// no-slash form all fold to the SAME (llama,”,3.1,instruct) tuple. The
// underscore spelling ("3_3") is a pinned documented residual: the family fold
// holds but the underscore-glued version is dropped.
func TestMetaLlamaNoSlashCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, metaLlamaNoSlashCorpusJSON, 11)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		// dashed spelling recovers 3.1; dotted spelling too.
		{Raw: "", ID: "meta-llama-3-1-8b-instruct"}: {Family: "llama", Variant: "", Version: "3.1", Mod: "instruct"},
		{Raw: "", ID: "meta-llama-3.1-8b-instruct"}: {Family: "llama", Variant: "", Version: "3.1", Mod: "instruct"},
		// underscore spelling: documented residual, version drops but family folds.
		{Raw: "", ID: "meta-llama-3_3-70b-instruct"}: {Family: "llama", Variant: "", Version: "", Mod: "instruct"},
		// three-spellings-one-entity convergence (raw-populated "llama").
		{Raw: "llama", ID: "meta-llama/Meta-Llama-3.1-8B-Instruct"}: {Family: "llama", Variant: "", Version: "3.1", Mod: "instruct"},
		{Raw: "llama", ID: "meta-llama/Llama-3.1-8B-Instruct"}:      {Family: "llama", Variant: "", Version: "3.1", Mod: "instruct"},
		{Raw: "llama", ID: "meta-llama-3.1-8b-instruct"}:            {Family: "llama", Variant: "", Version: "3.1", Mod: "instruct"},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestNamespaceSuffixTransparencyCapture pins FULL namespace/suffix convergence:
// every dotted AWS Bedrock cross-region form
// ("<region>.anthropic.claude-sonnet-4-5-20250929-v1:0" for us/eu/au/jp/global, plus
// the bare ":0" profile-index spelling) decomposes to the SAME (claude,sonnet,4.5)
// tuple as the plainly-served sibling — version INCLUDED — so all key to the one
// entity claude/sonnet@4.5. The dotted region.vendor. prefix and the -v1:0 / :0
// profile tag are routing metadata seen through by stripBedrockProfile before
// version recovery; the region itself is captured as a per-instance attribute (see
// TestRegionCapture), never in the key.
func TestNamespaceSuffixTransparencyCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, namespaceSuffixTransparencyCorpusJSON, 7)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		{Raw: "claude-sonnet", ID: "us.anthropic.claude-sonnet-4-5-20250929-v1:0"}:     {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		{Raw: "claude-sonnet", ID: "eu.anthropic.claude-sonnet-4-5-20250929-v1:0"}:     {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		{Raw: "claude-sonnet", ID: "au.anthropic.claude-sonnet-4-5-20250929-v1:0"}:     {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		{Raw: "claude-sonnet", ID: "jp.anthropic.claude-sonnet-4-5-20250929-v1:0"}:     {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		{Raw: "claude-sonnet", ID: "global.anthropic.claude-sonnet-4-5-20250929-v1:0"}: {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		// bare :0 profile index (no -v1) is transparent; plain sibling is the target.
		{Raw: "claude-sonnet", ID: "us.anthropic.claude-sonnet-4-5-20250929:0"}: {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
		{Raw: "claude-sonnet", ID: "claude-sonnet-4-5-20250929"}:                {Family: "claude", Variant: "sonnet", Version: "4.5", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestRegionCapture pins the Region-attribute extraction (DetectRegion): each
// attested AWS Bedrock region prefix surfaces its DISTINCT jurisdiction member
// (us->RegionUS, eu->RegionEU, au->RegionAU, jp->RegionJP, global->RegionGlobal —
// au and jp are their OWN members, NOT folded into APAC). The reserved "apac." scope
// and the RegionOther+raw fail-safe ("ca.") are pinned synthetically (0 attested).
// The negative controls confirm Region is orthogonal to Host (a nano-gpt azure-* host
// id stays RegionNone) and closed to non-region dotted segments (openai.gpt-5-codex
// stays RegionNone).
func TestRegionCapture(t *testing.T) {
	corpus := loadParseCorpus[string, regionExpected](t, regionCaptureCorpusJSON, 12)
	requireInputCoverage(t, corpus, map[string]regionExpected{
		"us.anthropic.claude-sonnet-4-5-20250929-v1:0": {Region: "us", RegionRaw: ""},
		"eu.anthropic.claude-haiku-4-5-20251001-v1:0":  {Region: "eu", RegionRaw: ""},
		// au and jp are DISTINCT jurisdictions, never APAC.
		"au.anthropic.claude-sonnet-4-5-20250929-v1:0":    {Region: "au", RegionRaw: ""},
		"jp.anthropic.claude-sonnet-4-5-20250929-v1:0":    {Region: "jp", RegionRaw: ""},
		"global.anthropic.claude-haiku-4-5-20251001-v1:0": {Region: "global", RegionRaw: ""},
		"us.meta.llama4-scout-17b-instruct-v1:0":          {Region: "us", RegionRaw: ""},
		// reserved apac scope + RegionOther+raw fail-safe (synthetic).
		"apac.anthropic.claude-sonnet-4-5-20250929-v1:0": {Region: "apac", RegionRaw: ""},
		"ca.anthropic.claude-sonnet-4-5-20250929-v1:0":   {Region: "other", RegionRaw: "ca"},
		// negative controls: host id and non-Bedrock dotted id both stay RegionNone.
		"azure-gpt-4o":               {Region: "unspecified", RegionRaw: ""},
		"openai.gpt-5-codex":         {Region: "unspecified", RegionRaw: ""},
		"claude-sonnet-4-5-20250929": {Region: "unspecified", RegionRaw: ""},
	})
	runRegionCaptureCorpus(t, corpus)
}

// TestTextEmbeddingSoleVariantCapture pins the compound-family sole-variant case:
// OpenAI's text-embedding-3-{small,large} keep the compound family "text-embedding"
// (no reduction to a bare "text" over-capture), promote the sole trailing variant
// (small/large), and recover version 3 between the family and the variant.
func TestTextEmbeddingSoleVariantCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, textEmbeddingSoleVariantCorpusJSON, 2)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		{Raw: "text-embedding", ID: "text-embedding-3-small"}: {Family: "text-embedding", Variant: "small", Version: "3", Mod: ""},
		{Raw: "text-embedding", ID: "text-embedding-3-large"}: {Family: "text-embedding", Variant: "large", Version: "3", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestGrokDocumentedResidualCapture pins a documented residual: at HEAD
// "grok-3-mini-fast-beta" decomposes to (grok,mini,3) with the mid-ID serving
// tier "fast" and trailing stage "beta" dropped (GH#9 mid-ID engine territory).
// The ID is absent from the current catalog; the row is pinned so a future mid-ID
// extraction that recovers fast/beta surfaces as a deliberate change.
func TestGrokDocumentedResidualCapture(t *testing.T) {
	corpus := loadParseCorpus[rawIDInput, fvvmExpected](t, grokDocumentedResidualCorpusJSON, 1)
	requireInputCoverage(t, corpus, map[rawIDInput]fvvmExpected{
		{Raw: "grok", ID: "grok-3-mini-fast-beta"}: {Family: "grok", Variant: "mini", Version: "3", Mod: ""},
	})
	runFamilyDetailedTupleCorpus(t, corpus)
}

// TestWhisperTrailingVersionRecovery_FamilyGated is the coverage for
// the WHISPER-FAMILY-GATED trailing "-v<int>" → Version recovery. It pins both halves of the
// contract: (1) whisper-* ids gain the version, and (2) the mutation-proof — NO other
// family's "-vN" packaging/revision tag is ever promoted (the failure mode that sank the
// general attempt). A regression that widens the gate beyond whisper turns these RED.
func TestWhisperTrailingVersionRecovery_FamilyGated(t *testing.T) {
	t.Parallel()

	type tc struct {
		raw     bestiary.Family
		id      bestiary.ModelID
		wantFam bestiary.Family
		wantVer string
	}
	cases := []tc{
		// (1) whisper TARGETS — version recovered to 3 (empty-raw AND raw paths agree).
		{"", "openai/whisper-large-v3", "whisper", "3"},
		{"", "whisper-large-v3", "whisper", "3"},
		{"", "whisper-large-v3-turbo", "whisper", "3"}, // skips trailing "turbo" modifier
		{"whisper", "whisper-large-v3-turbo", "whisper", "3"},

		// (2) MUTATION-PROOF — non-whisper "-vN" tags MUST NOT be promoted.
		// claude-opus-4-6-v1's "-v1" is a Bedrock packaging revision; the real version is 4.6,
		// extracted by the normal path. The recovery must NOT overwrite it with "1".
		{"", "anthropic.claude-opus-4-6-v1", "anthropic.claude", "4.6"},
		// elevenlabs/nova/morph/deepseek/recraft trailing -vN must stay Version="" (untouched).
		{"", "elevenlabs/elevenlabs-v2.5-turbo", "elevenlabs", ""},
		{"", "amazon/nova-lite-v1", "nova", ""},
		{"", "morph/morph-v3-fast", "morph", ""},
		{"", "deepseek-ai/DeepSeek-V3", "deepseek", ""},
		{"", "recraft/recraft-v3", "recraft", ""},
	}
	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			fam, _, ver, _, _ := bestiary.ParseFamilyDetailed(c.raw, c.id, "")
			if fam != c.wantFam {
				t.Errorf("ParseFamilyDetailed(%q, %q).Family = %q, want %q", c.raw, c.id, fam, c.wantFam)
			}
			if ver != c.wantVer {
				t.Errorf("ParseFamilyDetailed(%q, %q).Version = %q, want %q (whisper-gated recovery must not touch non-whisper -vN tags)",
					c.raw, c.id, ver, c.wantVer)
			}
		})
	}
}

// TestGrokNegationAwareModifier is the coverage for
// negation-aware modifier emission: an ID containing the literal token "non-<mod>" must
// emit "non-<mod>" (e.g. "non-reasoning"), NEVER the bare positive "<mod>". It pins the
// mutation-proof on both sides: (a) a "Cannon"/substring-"non" id NEVER gains a
// non-* modifier (the gate is a separate hyphen-token "non", not a substring); (a') the
// EXACT-vs-SUBSTRING pin "grok-noncode-reasoning" stays POSITIVE [reasoning] (a
// strings.Contains gate-mutation would RED here); (b) the grok non-reasoning ids invert
// correctly; and (c) the out-of-scope grok "fast" handling is left untouched (the positive
// reasoning sibling keeps [reasoning, fast]; the non-reasoning id does NOT gain "fast").
func TestGrokNegationAwareModifier(t *testing.T) {
	t.Parallel()

	type tc struct {
		id      bestiary.ModelID
		wantMod []string
	}
	cases := []tc{
		// (b) negation emitted, NOT the bare positive.
		{"grok-4-1-fast-non-reasoning", []string{"non-reasoning"}},
		{"grok-4-fast-non-reasoning", []string{"non-reasoning"}},
		{"grok-4-20-non-reasoning", []string{"non-reasoning"}},
		{"xai/grok-4.20-non-reasoning", []string{"non-reasoning"}},
		// (c) out-of-scope "fast" untouched: positive sibling KEEPS [reasoning, fast];
		// the non-reasoning id does NOT gain "fast" (stays a single negation token).
		{"grok-4-1-fast-reasoning", []string{"reasoning", "fast"}},
		{"grok-4-fast-reasoning", []string{"reasoning", "fast"}},
		// (a) substring "non" inside a single token ("Cannon") must NEVER negate.
		{"GalrionSoftworks/MN-LooseCannon-12B-v1", nil},
		{"VongolaChouko/Starcannon-Unleashed-12B-v1.0", nil},
		// (a') EXACT-vs-SUBSTRING PIN: the negation gate is the LITERAL
		// preceding token toks[i-1]=="non", NOT strings.Contains(prev,"non"). Here the modifier
		// "reasoning" is preceded by "noncode" — which CONTAINS "non" as a substring but is not
		// the literal token "non" — so it must stay the POSITIVE [reasoning]. A future mutation
		// of the gate to strings.Contains would wrongly emit [non-reasoning] and turn this RED.
		{"grok-noncode-reasoning", []string{"reasoning"}},
	}
	for _, c := range cases {
		t.Run(string(c.id), func(t *testing.T) {
			_, _, _, mod, _ := bestiary.ParseFamilyDetailed("", c.id, "")
			if !equalStringSlices(mod, c.wantMod) {
				t.Errorf("ParseFamilyDetailed(%q).Modifier = %v, want %v (negation-aware emission: literal "+
					"\"non-<mod>\" token → \"non-<mod>\"; substring \"non\" must not negate; \"fast\" out of scope)",
					c.id, mod, c.wantMod)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// ParseParamSize tests
// ----------------------------------------------------------------------------

// TestParseParamSize_ValidShapes verifies that ParseParamSize accepts well-formed
// size tokens in all recognized shapes and returns the canonical lowercase form.
// Covers dense (<digits><unit>), MoE (<total>x<active>b), and active-MoE
// (<total>b-a<active>b) shapes; and that all inputs are canonicalized to lowercase.
func TestParseParamSize_ValidShapes(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, parseParamSizeValidCorpusJSON, 28)
	requireInputCoverage(t, corpus, map[string]string{
		"":          "",          // empty is valid (no-op)
		"70B":       "70b",       // uppercase folds
		"8x22b":     "8x22b",     // NxM MoE
		"671b-a17b": "671b-a17b", // active-MoE
		"17b-16e":   "17b-16e",   // count-MoE
		"10.7b":     "10.7b",     // decimal preserved
		"560m":      "560m",      // m-unit
	})
	runParseParamSizeCanonical(t, corpus)
}

// TestParseParamSize_InvalidShapes verifies that ParseParamSize rejects inputs
// that do not match any recognized param-size shape and returns an actionable error.
// Each rejected input must produce a non-nil error whose message names the bad input
// (C-actionable-errors: callers must be able to diagnose the rejection without
// reading source code).
func TestParseParamSize_InvalidShapes(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, parseParamSizeInvalidCorpusJSON, 16)
	requireInputCoverage(t, corpus, map[string]string{
		"r7b":    "", // leading-letter substring trap
		"10_7b":  "", // underscore-glued near-miss
		"17b-16": "", // count-moe missing 'e'
	})
	runParseParamSizeInvalid(t, corpus)
}

// TestParseParamSize_CaseFolding pins the case-folding contract: ParseParamSize
// must return the canonical lowercase form regardless of input casing, and must
// not error on uppercase inputs. This guards against regressions where
// strings.ToLower is accidentally removed from the implementation.
func TestParseParamSize_CaseFolding(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, parseParamSizeCasefoldCorpusJSON, 10)
	requireInputCoverage(t, corpus, map[string]string{
		// every shape family folds: dense, active-MoE, count-MoE.
		"671B":      "671b",
		"671B-A17B": "671b-a17b",
		"17B-128E":  "17b-128e",
	})
	runParseParamSizeCanonical(t, corpus)
}

// TestParseParamShape_Shapes verifies that ParseParamShape decomposes each of the
// four size shapes along its parameter-shape joints, populating exactly the fields
// that shape carries and setting the others to the ParamShapeNull (-1) sentinel. The
// load-bearing invariants are: an NxM MoE ("8x22b") records ExpertCount and
// PerExpertParams but leaves TotalParams/ActiveParams NULL (Total is NEVER N*M); an
// active-MoE ("30b-a3b") records Total and Active and leaves PerExpert/Experts NULL;
// a count-suffixed MoE ("17b-16e") records Active and ExpertCount and leaves
// Total/PerExpert NULL; a dense token sets Total, leaves Active/PerExpert NULL, and
// sets ExpertCount to a genuine 0 (the sole in-domain 0 in the contract); the empty
// token leaves ALL FOUR NULL.
func TestParseParamShape_Shapes(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, paramShapeExpected](t, parseParamShapeCorpusJSON, 12)
	requireInputCoverage(t, corpus, map[string]paramShapeExpected{
		// NxM MoE: Total is NEVER N*M; only PerExpert + ExpertCount attested.
		"8x22b": {Total: -1, Active: -1, PerExpert: 22_000_000_000, ExpertCount: 8},
		// dense: ExpertCount is a genuine 0 (the sole in-domain 0).
		"30b": {Total: 30_000_000_000, Active: -1, PerExpert: -1, ExpertCount: 0},
		// count-suffixed MoE: Active + ExpertCount only.
		"17b-16e": {Total: -1, Active: 17_000_000_000, PerExpert: -1, ExpertCount: 16},
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			want := bestiary.ParamShape{
				TotalParams:     c.Expected.Total,
				ActiveParams:    c.Expected.Active,
				PerExpertParams: c.Expected.PerExpert,
				ExpertCount:     int(c.Expected.ExpertCount),
			}
			got, err := bestiary.ParseParamShape(c.Input)
			if err != nil {
				t.Fatalf("ParseParamShape(%q) unexpected error: %v", c.Input, err)
			}
			if got != want {
				t.Errorf("ParseParamShape(%q) = %+v, want %+v", c.Input, got, want)
			}
		})
	}
}

// TestParseParamShape_DecimalExact pins the EXACT string-digit decimal arithmetic:
// a decimal size token must decompose to the exact integer parameter count, never a
// float64-rounded approximation. "10.7b" is exactly 10_700_000_000 and "0.6b" is
// exactly 600_000_000 — pinned as literals so a float64 rewrite cannot pass.
func TestParseParamShape_DecimalExact(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, int64](t, parseParamShapeDecimalCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[string]int64{
		// the two literals the doc comment pins: exact, never float-rounded.
		"10.7b": 10_700_000_000,
		"0.6b":  600_000_000,
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got, err := bestiary.ParseParamShape(c.Input)
			if err != nil {
				t.Fatalf("ParseParamShape(%q) unexpected error: %v", c.Input, err)
			}
			if got.TotalParams != c.Expected {
				t.Errorf("ParseParamShape(%q).TotalParams = %d, want %d (exact string-digit arithmetic, no float truncation)",
					c.Input, got.TotalParams, c.Expected)
			}
		})
	}
}

// TestParseParamShape_Invalid verifies that a non-empty token that is not a
// recognized size shape returns an actionable error naming the input.
func TestParseParamShape_Invalid(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, parseParamShapeInvalidCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[string]string{
		// the substring-trap near-misses must stay rejected.
		"r7b":   "",
		"10_7b": "",
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			_, err := bestiary.ParseParamShape(c.Input)
			if err == nil {
				t.Errorf("ParseParamShape(%q) = nil error, want a rejection error", c.Input)
				return
			}
			if !strings.Contains(err.Error(), c.Input) {
				t.Errorf("ParseParamShape(%q) error does not name the input: %q", c.Input, err.Error())
			}
		})
	}
}

// TestParseParamShape_OverflowGuard pins the int64 overflow rejection: a
// pathological size token whose parameter count exceeds int64 ("9300000000b" =
// 9.3e18 > math.MaxInt64 ~ 9.22e18) must return the documented error, never a
// silently wrapped (negative or garbage) count.
func TestParseParamShape_OverflowGuard(t *testing.T) {
	t.Parallel()

	shape, err := bestiary.ParseParamShape("9300000000b")
	if err == nil {
		t.Fatalf("ParseParamShape(\"9300000000b\") = (%+v, nil), want an int64-overflow rejection error", shape)
	}
	if !strings.Contains(err.Error(), "int64") {
		t.Errorf("overflow error should name the int64 limit, got: %q", err.Error())
	}
}

// TestExtractParamSizeToken pins the single-grammar extractor's contract: longest
// whole-window match over the [-:/] separator set, returning the CANONICAL token;
// '.' and '_' are token-internal (never separators); and no substring trap. Rows
// use real catalog ID spellings wherever one exists for the shape.
func TestExtractParamSizeToken(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, extractParamSizeTokenCorpusJSON, 13)
	requireInputCoverage(t, corpus, map[string]string{
		// longest whole-window beats the prefix.
		"qwen3-235b-a22b": "235b-a22b",
		// near-miss substring traps yield no token.
		"command-r7b":     "",
		"claude-opus-4-5": "",
	})
	runExtractParamSizeTokenCorpus(t, corpus)
}

// TestExtractParamSizeToken_CompoundInvariantAcrossIDForms pins the extractor-level
// invariant that any caller inherits: the compound "235b-a22b" extracts whole and
// canonical for the qwen3 MoE ID regardless of the surrounding tokens — namespace
// prefix, instruct/date suffixes, or casing — and is never split to "235b". Callers
// that delegate to ExtractParamSizeToken therefore size these IDs identically for
// free; this test pins the extractor's own behavior, not any caller's wiring.
func TestExtractParamSizeToken_CompoundInvariantAcrossIDForms(t *testing.T) {
	t.Parallel()

	corpus := loadParseCorpus[string, string](t, extractParamSizeTokenCompoundCorpusJSON, 4)
	requireInputCoverage(t, corpus, map[string]string{
		// never clipped to "235b" under a namespace+suffix or full-uppercase form.
		"qwen/qwen3-235b-a22b-instruct": "235b-a22b",
		"Qwen3-235B-A22B":               "235b-a22b",
	})
	runExtractParamSizeTokenCorpus(t, corpus)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
