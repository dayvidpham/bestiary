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

// familyVariantExpected is the expected output of one ParseFamily case: the
// decomposed family and variant.
type familyVariantExpected struct {
	Family  string `json:"family"`
	Variant string `json:"variant"`
}

// loadFamilyVariantCorpus loads a ParseFamily corpus, enforces the exact
// case-count control (wantN, the pre-migration inline row count), and the
// non-vacuity guard. It returns the loaded corpus so the caller can add a
// value-based coverage assertion before driving ParseFamily.
func loadFamilyVariantCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[string, familyVariantExpected] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[string, familyVariantExpected](data)
	if err != nil {
		t.Fatalf("load ParseFamily corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("ParseFamily corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
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
