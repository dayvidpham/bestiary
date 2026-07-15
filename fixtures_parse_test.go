package bestiary_test

// Embedded JSON case corpora for the parse-package table-driven tests, plus the
// shared input/expected types and corpus-runner helpers. See TESTING.md for the
// corpus standard: each corpus is guarded by an exact case-count control, a
// value-based coverage assertion, and testcase.Corpus.Validate non-vacuity.

import (
	_ "embed"
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

// requireDateCoverage asserts each probed (id,releaseDate) input is still
// present with its expected date. Value-based coverage guard.
func requireDateCoverage(t *testing.T, corpus testcase.Corpus[dateInput, string], probes map[dateInput]string) {
	t.Helper()
	got := map[dateInput]string{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: ExtractDate case for input %+v is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: ExtractDate case %+v has %q, want %q", in, have, want)
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

// requireFamilyVersionCoverage asserts each probed raw input is still present
// with its expected (family, variant, version). Value-based coverage guard.
func requireFamilyVersionCoverage(t *testing.T, corpus testcase.Corpus[string, familyVersionExpected], probes map[string]familyVersionExpected) {
	t.Helper()
	got := map[string]familyVersionExpected{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: case for input %q is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: case %q has expected %+v, want %+v", in, have, want)
		}
	}
}

// requireFamilyVariantCoverage asserts each probed input is still present in the
// corpus with its expected decomposition. It is the value-based coverage guard:
// a count-preserving swap that drops a load-bearing case (and adds a filler)
// reddens here even though the exact-count control cannot see it.
func requireFamilyVariantCoverage(t *testing.T, corpus testcase.Corpus[string, familyVariantExpected], probes map[string]familyVariantExpected) {
	t.Helper()
	got := map[string]familyVariantExpected{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: case for input %q is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: case %q has expected %+v, want %+v", in, have, want)
		}
	}
}
