package bestiary_test

// Embedded JSON case corpora for the closed-enum table-driven tests (Modality,
// AcceptabilityRating, Harness, CanonicalScheme) plus their shared runners. See
// TESTING.md for the corpus standard: each corpus is guarded by an exact case-count
// control, a value-based coverage assertion, and testcase.Corpus.Validate non-vacuity.
//
// These tables are all the same SHAPE — an enum member (int value, or the underlying
// token for a string enum) mapped to a rendered wire string, or a JSON literal mapped
// to the token it parses to — so the corpora share two generic runners rather than one
// per enum.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// ---- Modality ---------------------------------------------------------------

//go:embed testdata/enum/modality_string_valid_corpus.json
var enumModalityStringValidCorpusJSON []byte

//go:embed testdata/enum/modality_string_outofrange_corpus.json
var enumModalityStringOutOfRangeCorpusJSON []byte

// ---- AcceptabilityRating ----------------------------------------------------

//go:embed testdata/enum/acceptability_string_corpus.json
var enumAcceptabilityStringCorpusJSON []byte

//go:embed testdata/enum/acceptability_unmarshal_caseinsensitive_corpus.json
var enumAcceptabilityUnmarshalCICorpusJSON []byte

//go:embed testdata/enum/acceptability_unmarshal_reject_corpus.json
var enumAcceptabilityUnmarshalRejectCorpusJSON []byte

// ---- Harness ----------------------------------------------------------------

//go:embed testdata/enum/harness_string_corpus.json
var enumHarnessStringCorpusJSON []byte

// ---- CanonicalScheme --------------------------------------------------------

//go:embed testdata/enum/canonical_scheme_string_corpus.json
var enumCanonicalSchemeStringCorpusJSON []byte

//go:embed testdata/enum/parse_scheme_valid_corpus.json
var enumParseSchemeValidCorpusJSON []byte

//go:embed testdata/enum/canonical_scheme_unmarshal_caseinsensitive_corpus.json
var enumCanonicalSchemeUnmarshalCICorpusJSON []byte

//go:embed testdata/enum/canonical_scheme_unmarshal_reject_corpus.json
var enumCanonicalSchemeUnmarshalRejectCorpusJSON []byte

// ---- Family / Provider (permissive string types) ----------------------------

//go:embed testdata/enum/family_string_corpus.json
var enumFamilyStringCorpusJSON []byte

//go:embed testdata/enum/family_roundtrip_corpus.json
var enumFamilyRoundTripCorpusJSON []byte

//go:embed testdata/enum/family_canonical_provider_corpus.json
var enumFamilyCanonicalProviderCorpusJSON []byte

//go:embed testdata/enum/provider_string_corpus.json
var enumProviderStringCorpusJSON []byte

// loadEnumIntCorpus loads an int-keyed enum corpus (the enum's underlying int value
// mapped to its rendered wire token) under the exact-count and non-vacuity guards.
func loadEnumIntCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[int, string] {
	t.Helper()
	return loadParseCorpus[int, string](t, data, wantN)
}

// loadEnumStringCorpus loads a string-keyed enum corpus (a wire token or a raw JSON
// literal mapped to the token it must render/parse to) under the same guards.
func loadEnumStringCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[string, string] {
	t.Helper()
	return loadParseCorpus[string, string](t, data, wantN)
}

func loadHarnessCorpus(t *testing.T) testcase.Corpus[string, string] {
	t.Helper()
	return loadEnumStringCorpus(t, enumHarnessStringCorpusJSON, len(harnessRequiredNames))
}

// runEnumIntStringCorpus drives a String() method over every int-valued case. render
// is the production call under test; it is passed the case's int input.
func runEnumIntStringCorpus(t *testing.T, corpus testcase.Corpus[int, string], render func(int) string) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := render(c.Input); got != c.Expected {
				t.Errorf("String() over int value %d = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// runEnumStringCorpus drives a string→string production call (a String() over a string
// enum, or a parse-then-render round trip) over every case.
func runEnumStringCorpus(t *testing.T, corpus testcase.Corpus[string, string], render func(*testing.T, string) string) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := render(t, c.Input); got != c.Expected {
				t.Errorf("input %q = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// runEnumRejectCorpus drives a must-fail corpus: every case's input must produce a
// non-nil error carrying a non-empty message. reject returns the production error.
func runEnumRejectCorpus(t *testing.T, corpus testcase.Corpus[string, string], reject func(string) error) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Classification != testcase.MustFail {
				t.Fatalf("case %q is classified %q; this corpus is must-fail only", c.Name, c.Classification)
			}
			err := reject(c.Input)
			if err == nil {
				t.Fatalf("input %s was accepted, want a rejection", c.Input)
			}
			if err.Error() == "" {
				t.Fatalf("input %s was rejected with an EMPTY error message", c.Input)
			}
		})
	}
}

// requireHarnessKnown is the shared guard behind the Harness corpus: every token in the
// corpus must also be a recognized Harness, so the corpus cannot drift into naming a
// constant the package no longer ships.
func requireHarnessKnown(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		if !bestiary.Harness(c.Input).IsKnown() {
			t.Errorf("corpus names harness %q, which is not a known Harness", c.Input)
		}
	}
}

func requireHarnessNames(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	names := make(map[string]bool, len(corpus.Cases))
	for _, c := range corpus.Cases {
		if names[c.Name] {
			t.Errorf("harness corpus contains duplicate required name %q", c.Name)
		}
		names[c.Name] = true
	}
	for _, name := range harnessRequiredNames {
		if !names[name] {
			t.Errorf("harness corpus is missing required name %q", name)
		}
		delete(names, name)
	}
	for name := range names {
		t.Errorf("harness corpus contains unexpected name %q", name)
	}
}
