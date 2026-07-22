package bestiary_test

// Embedded JSON case corpora for the parse-package table-driven tests, plus the
// shared input/expected types and corpus-runner helpers. See TESTING.md for the
// corpus standard: each corpus is guarded by an exact case-count control, a
// value-based coverage assertion, and testcase.Corpus.Validate non-vacuity.

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

// ---- ParseFamily decomposition corpora (input: raw family string) ----------

//go:embed testdata/parse/family_overrides_corpus.json
var familyOverridesCorpusJSON []byte

//go:embed testdata/parse/family_opaque_compounds_corpus.json
var familyOpaqueCompoundsCorpusJSON []byte

//go:embed testdata/parse/family_versioned_patterns_corpus.json
var familyVersionedPatternsCorpusJSON []byte

//go:embed testdata/parse/family_hyphen_version_corpus.json
var familyHyphenVersionCorpusJSON []byte

//go:embed testdata/parse/family_suffix_stripping_corpus.json
var familySuffixStrippingCorpusJSON []byte

//go:embed testdata/parse/family_vprefix_corpus.json
var familyVPrefixCorpusJSON []byte

//go:embed testdata/parse/family_hyphen_version_no_override_corpus.json
var familyHyphenVersionNoOverrideCorpusJSON []byte

// ---- ExtractDate / InferFamily / ParseFamilyWithVersion / ExtractVersion ----

//go:embed testdata/parse/extract_date_fromid_corpus.json
var extractDateFromIDCorpusJSON []byte

//go:embed testdata/parse/extract_date_calendar_corpus.json
var extractDateCalendarCorpusJSON []byte

//go:embed testdata/parse/infer_family_fromid_corpus.json
var inferFamilyFromIDCorpusJSON []byte

//go:embed testdata/parse/family_with_version_core_corpus.json
var familyWithVersionCoreCorpusJSON []byte

//go:embed testdata/parse/family_with_version_gemini_corpus.json
var familyWithVersionGeminiCorpusJSON []byte

//go:embed testdata/parse/family_with_version_alnum_corpus.json
var familyWithVersionAlnumCorpusJSON []byte

//go:embed testdata/parse/extract_version_fromid_corpus.json
var extractVersionFromIDCorpusJSON []byte

//go:embed testdata/parse/infer_family_variant_corpus.json
var inferFamilyVariantCorpusJSON []byte

// familyVariantExpected is the expected output of one ParseFamily case: the
// decomposed family and variant.
type familyVariantExpected struct {
	Family  string `json:"family"`
	Variant string `json:"variant"`
}

// familyVersionExpected is the (family, variant, version) triple produced by
// ParseFamilyWithVersion and InferFamilyFromIDWithVariant.
type familyVersionExpected struct {
	Family  string `json:"family"`
	Variant string `json:"variant"`
	Version string `json:"version"`
}

// dateInput is the (id, releaseDate) pair fed to ExtractDate.
type dateInput struct {
	ID          string `json:"id"`
	ReleaseDate string `json:"release_date"`
}

// providerIDInput is the (id, provider) pair fed to InferFamilyFromID and
// InferFamilyFromIDWithVariant.
type providerIDInput struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// versionFromIDInput is the (id, rawFamily) pair fed to ExtractVersionFromID.
type versionFromIDInput struct {
	ID        string `json:"id"`
	RawFamily string `json:"raw_family"`
}

// ---- ExtractModifier corpora ----------------------------------------------

//go:embed testdata/parse/extract_modifier_corpus.json
var extractModifierCorpusJSON []byte

//go:embed testdata/parse/extract_modifier_double_count_corpus.json
var extractModifierDoubleCountCorpusJSON []byte

//go:embed testdata/parse/uniform_modifier_suffix_corpus.json
var uniformModifierSuffixCorpusJSON []byte

//go:embed testdata/parse/extract_modifier_pipeline_corpus.json
var extractModifierPipelineCorpusJSON []byte

// modifierInput is the (id, family, variant) triple fed to ExtractModifier.
type modifierInput struct {
	ID      string `json:"id"`
	Family  string `json:"family"`
	Variant string `json:"variant"`
}

// modifierExpected is the (modifier, consumed) pair ExtractModifier returns.
type modifierExpected struct {
	Modifier string `json:"modifier"`
	Consumed string `json:"consumed"`
}

// uniformModInput is the (rawFamily, id, provider) triple fed to
// ParseFamilyDetailed for the uniform-modifier acceptance test.
type uniformModInput struct {
	RawFamily string `json:"raw_family"`
	ID        string `json:"id"`
	Provider  string `json:"provider"`
}

// uniformModExpected is the (family, modifier) pair asserted by the
// uniform-modifier acceptance test; the runner also asserts variant != modifier.
type uniformModExpected struct {
	Family   string `json:"family"`
	Modifier string `json:"modifier"`
}

// pipelineInput is the (rawID, rawFamily) pair fed to the parse-pipeline
// composition test.
type pipelineInput struct {
	RawID     string `json:"raw_id"`
	RawFamily string `json:"raw_family"`
}

// pipelineExpected is the (modifier, version, date) triple the parse pipeline
// must produce with the trailing modifier stripped before version/date.
type pipelineExpected struct {
	Modifier string `json:"modifier"`
	Version  string `json:"version"`
	Date     string `json:"date"`
}

// runExtractModifierCorpus drives bestiary.ExtractModifier over every case.
func runExtractModifierCorpus(t *testing.T, corpus testcase.Corpus[modifierInput, modifierExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotModifier, gotConsumed := bestiary.ExtractModifier(bestiary.ModelID(c.Input.ID), bestiary.Family(c.Input.Family), c.Input.Variant)
			if gotModifier != c.Expected.Modifier {
				t.Errorf("ExtractModifier(%q, %q, %q) modifier = %q, want %q",
					c.Input.ID, c.Input.Family, c.Input.Variant, gotModifier, c.Expected.Modifier)
			}
			if gotConsumed != c.Expected.Consumed {
				t.Errorf("ExtractModifier(%q, %q, %q) consumed = %q, want %q",
					c.Input.ID, c.Input.Family, c.Input.Variant, gotConsumed, c.Expected.Consumed)
			}
		})
	}
}

// ---- ParseFamilyDetailed tuple corpora (dot-glued, cross-provider) ---------

//go:embed testdata/parse/dot_glued_variant_corpus.json
var dotGluedVariantCorpusJSON []byte

//go:embed testdata/parse/cross_provider_convergences_corpus.json
var crossProviderConvergencesCorpusJSON []byte

//go:embed testdata/parse/glued_version_modifier_corpus.json
var gluedVersionModifierCorpusJSON []byte

//go:embed testdata/parse/series_letter_split_corpus.json
var seriesLetterSplitCorpusJSON []byte

//go:embed testdata/parse/series_tier_modifier_corpus.json
var seriesTierModifierCorpusJSON []byte

//go:embed testdata/parse/parse_family_detailed_modifier_list_corpus.json
var parseFamilyDetailedModifierListCorpusJSON []byte

//go:embed testdata/parse/tier1_straggler_convergences_corpus.json
var tier1StragglerConvergencesCorpusJSON []byte

//go:embed testdata/parse/azure_serving_host_corpus.json
var azureServingHostCorpusJSON []byte

//go:embed testdata/parse/family_o_overcapture_corpus.json
var familyOOverCaptureCorpusJSON []byte

//go:embed testdata/parse/meta_llama_no_slash_corpus.json
var metaLlamaNoSlashCorpusJSON []byte

//go:embed testdata/parse/namespace_suffix_transparency_corpus.json
var namespaceSuffixTransparencyCorpusJSON []byte

//go:embed testdata/parse/text_embedding_sole_variant_corpus.json
var textEmbeddingSoleVariantCorpusJSON []byte

//go:embed testdata/parse/grok_documented_residual_corpus.json
var grokDocumentedResidualCorpusJSON []byte

//go:embed testdata/parse/region_capture_corpus.json
var regionCaptureCorpusJSON []byte

// regionExpected is the (region, region_raw) pair DetectRegion produces: region is
// the Region.String() rendering, region_raw the fail-safe raw carrier (non-empty
// only for RegionOther). The region+profile-suffix strip is owned by
// stripBedrockProfile (exercised end-to-end by the namespace-convergence corpus),
// so DetectRegion returns the attribute pair, not a stripped ID.
type regionExpected struct {
	Region    string `json:"region"`
	RegionRaw string `json:"region_raw"`
}

// runRegionCaptureCorpus drives bestiary.DetectRegion over every case and asserts
// the (Region.String(), RegionRaw) pair.
func runRegionCaptureCorpus(t *testing.T, corpus testcase.Corpus[string, regionExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			region, raw := bestiary.DetectRegion(bestiary.ModelID(c.Input))
			if region.String() != c.Expected.Region {
				t.Errorf("DetectRegion(%q) region = %q, want %q", c.Input, region, c.Expected.Region)
			}
			if raw != c.Expected.RegionRaw {
				t.Errorf("DetectRegion(%q) regionRaw = %q, want %q", c.Input, raw, c.Expected.RegionRaw)
			}
		})
	}
}

// rawIDInput is the (rawFamily, id) pair fed to ParseFamilyDetailed.
type rawIDInput struct {
	Raw string `json:"raw"`
	ID  string `json:"id"`
}

// fvvmExpected is the (family, variant, version, mod) tuple ParseFamilyDetailed
// produces; mod is the modJoin of the returned modifier list.
type fvvmExpected struct {
	Family  string `json:"family"`
	Variant string `json:"variant"`
	Version string `json:"version"`
	Mod     string `json:"mod"`
}

// runFamilyDetailedTupleCorpus drives bestiary.ParseFamilyDetailed(raw, id, "p")
// over every case and asserts the (family, variant, version, modJoin(mod)) tuple.
func runFamilyDetailedTupleCorpus(t *testing.T, corpus testcase.Corpus[rawIDInput, fvvmExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			f, v, ver, mod, _ := bestiary.ParseFamilyDetailed(bestiary.Family(c.Input.Raw), bestiary.ModelID(c.Input.ID), "p")
			if string(f) != c.Expected.Family || v != c.Expected.Variant || ver != c.Expected.Version || modJoin(mod) != c.Expected.Mod {
				t.Errorf("ParseFamilyDetailed(raw=%q,id=%q) = (%q,%q,%q,%q), want (%q,%q,%q,%q)",
					c.Input.Raw, c.Input.ID, f, v, ver, modJoin(mod), c.Expected.Family, c.Expected.Variant, c.Expected.Version, c.Expected.Mod)
			}
		})
	}
}

// ---- ParseParamSize / ParseParamShape / ExtractParamSizeToken corpora ------

//go:embed testdata/parse/parse_param_size_valid_corpus.json
var parseParamSizeValidCorpusJSON []byte

//go:embed testdata/parse/parse_param_size_casefold_corpus.json
var parseParamSizeCasefoldCorpusJSON []byte

//go:embed testdata/parse/parse_param_size_invalid_corpus.json
var parseParamSizeInvalidCorpusJSON []byte

//go:embed testdata/parse/parse_param_shape_corpus.json
var parseParamShapeCorpusJSON []byte

//go:embed testdata/parse/parse_param_shape_decimal_corpus.json
var parseParamShapeDecimalCorpusJSON []byte

//go:embed testdata/parse/parse_param_shape_invalid_corpus.json
var parseParamShapeInvalidCorpusJSON []byte

//go:embed testdata/parse/extract_param_size_token_corpus.json
var extractParamSizeTokenCorpusJSON []byte

//go:embed testdata/parse/extract_param_size_token_compound_corpus.json
var extractParamSizeTokenCompoundCorpusJSON []byte

// paramShapeExpected is the four-joint decomposition of a size token. Each field
// holds a genuine parameter count, a genuine 0 (dense ExpertCount), or -1
// (bestiary.ParamShapeNull) for a joint that shape does not carry.
type paramShapeExpected struct {
	Total       int64 `json:"total"`
	Active      int64 `json:"active"`
	PerExpert   int64 `json:"per_expert"`
	ExpertCount int64 `json:"expert_count"`
}

// runParseParamSizeCanonical drives bestiary.ParseParamSize and asserts the
// canonical (lowercase) result, with no error. Shared by the valid-shapes and
// case-folding corpora.
func runParseParamSizeCanonical(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got, err := bestiary.ParseParamSize(c.Input)
			if err != nil {
				t.Fatalf("ParseParamSize(%q) unexpected error: %v", c.Input, err)
			}
			if got != c.Expected {
				t.Errorf("ParseParamSize(%q) = %q, want %q (canonical lowercase)", c.Input, got, c.Expected)
			}
		})
	}
}

// runParseParamSizeInvalid drives bestiary.ParseParamSize over the reject corpus
// and asserts an actionable error naming the input and carrying "How to fix".
func runParseParamSizeInvalid(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			_, err := bestiary.ParseParamSize(c.Input)
			if err == nil {
				t.Errorf("ParseParamSize(%q) = nil error, want a rejection error for an invalid shape", c.Input)
				return
			}
			msg := err.Error()
			if !strings.Contains(msg, c.Input) {
				t.Errorf("ParseParamSize(%q) error does not mention the rejected input: %q", c.Input, msg)
			}
			if !strings.Contains(msg, "How to fix") {
				t.Errorf("ParseParamSize(%q) error missing 'How to fix' clause: %q", c.Input, msg)
			}
		})
	}
}

// runExtractParamSizeTokenCorpus drives bestiary.ExtractParamSizeToken and
// asserts the canonical token; ok is exactly (token != "").
func runExtractParamSizeTokenCorpus(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotTok, gotOK := bestiary.ExtractParamSizeToken(c.Input)
			wantOK := c.Expected != ""
			if gotTok != c.Expected || gotOK != wantOK {
				t.Errorf("ExtractParamSizeToken(%q) = (%q, %v), want (%q, %v)",
					c.Input, gotTok, gotOK, c.Expected, wantOK)
			}
		})
	}
}

// loadParseCorpus loads a corpus, enforces the exact case-count control (wantN,
// the pre-migration inline row count) and the non-vacuity guard, and returns it
// so the caller can add a value-based coverage assertion before driving the SUT.
func loadParseCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// loadFamilyVariantCorpus is the ParseFamily specialization of loadParseCorpus.
func loadFamilyVariantCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[string, familyVariantExpected] {
	t.Helper()
	return loadParseCorpus[string, familyVariantExpected](t, data, wantN)
}

// runFamilyVariantCorpus drives bestiary.ParseFamily over every case and
// asserts the decomposed (family, variant) against each case's Expected, under
// the same t.Run/t.Parallel shape the inline tables used.
func runFamilyVariantCorpus(t *testing.T, corpus testcase.Corpus[string, familyVariantExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant := bestiary.ParseFamily(bestiary.Family(c.Input))
			if string(gotFamily) != c.Expected.Family {
				t.Errorf("ParseFamily(%q) family = %q, want %q", c.Input, gotFamily, c.Expected.Family)
			}
			if gotVariant != c.Expected.Variant {
				t.Errorf("ParseFamily(%q) variant = %q, want %q", c.Input, gotVariant, c.Expected.Variant)
			}
		})
	}
}

// runExtractDateCorpus drives bestiary.ExtractDate over every case.
func runExtractDateCorpus(t *testing.T, corpus testcase.Corpus[dateInput, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got := bestiary.ExtractDate(bestiary.ModelID(c.Input.ID), c.Input.ReleaseDate)
			if got != c.Expected {
				t.Errorf("ExtractDate(%q, %q) = %q, want %q", c.Input.ID, c.Input.ReleaseDate, got, c.Expected)
			}
		})
	}
}

// requireInputCoverage is the value-based coverage guard: it asserts each probed
// input is still present in the corpus with its expected output. A count-preserving
// swap that drops a load-bearing case (and adds a filler) reddens here even though
// the exact-count control cannot see it. Both I and E must be comparable (the
// all-string input/expected structs used by these corpora are).
func requireInputCoverage[I comparable, E comparable](t *testing.T, corpus testcase.Corpus[I, E], probes map[I]E) {
	t.Helper()
	got := map[I]E{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: case for input %+v is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: case %+v has expected %+v, want %+v", in, have, want)
		}
	}
}

// requireNameCoverage is the keyed sibling of requireInputCoverage for corpora
// whose input or expected type is not comparable (slice-shaped fields, e.g. the
// llama-4 membership id arrays): it asserts each probed case NAME is still
// present. A count-preserving swap that drops a load-bearing case and adds a
// filler (necessarily under a different name) reddens here, giving the same
// swap-detection power without the comparability constraint.
func requireNameCoverage[I any, E any](t *testing.T, corpus testcase.Corpus[I, E], names ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, c := range corpus.Cases {
		have[c.Name] = true
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("value coverage lost: case named %q is missing", n)
		}
	}
}

// runFamilyVersionCorpus drives bestiary.ParseFamilyWithVersion over every case
// and asserts the (family, variant, version) triple.
func runFamilyVersionCorpus(t *testing.T, corpus testcase.Corpus[string, familyVersionExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotFamily, gotVariant, gotVersion := bestiary.ParseFamilyWithVersion(bestiary.Family(c.Input))
			if string(gotFamily) != c.Expected.Family {
				t.Errorf("ParseFamilyWithVersion(%q) family = %q, want %q", c.Input, gotFamily, c.Expected.Family)
			}
			if gotVariant != c.Expected.Variant {
				t.Errorf("ParseFamilyWithVersion(%q) variant = %q, want %q", c.Input, gotVariant, c.Expected.Variant)
			}
			if gotVersion != c.Expected.Version {
				t.Errorf("ParseFamilyWithVersion(%q) version = %q, want %q", c.Input, gotVersion, c.Expected.Version)
			}
		})
	}
}

// ---- unknown-suffix-overflow ParseFailure capture ---------------------------

//go:embed testdata/parse/unknown_suffix_overflow_corpus.json
var parseUnknownSuffixOverflowCorpusJSON []byte

// suffixOverflowInput is the (rawFamily, id, provider) triple ParseFamilyDetailed
// takes. The rawFamily is carried separately from the id because the overflow is
// detected against the DECOMPOSED family, not against the id alone.
type suffixOverflowInput struct {
	RawFamily string `json:"raw_family"`
	ID        string `json:"id"`
	Provider  string `json:"provider"`
}

// suffixOverflowExpected is the ParseFailure reason the parser must RETURN. It is
// empty on the negative controls, which must not fire the overflow reason at all.
type suffixOverflowExpected struct {
	Reason string `json:"reason"`
}
