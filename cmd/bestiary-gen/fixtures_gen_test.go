package main

// Embedded JSON case corpora for the bestiary-gen identifier-builder tables
// (slugToIdentifier / providerConstName / styleSegment / entityConstName / splitComma),
// plus their shared input/expected types and loaders. Each corpus is guarded by an
// exact case-count control, a value-based coverage assertion, and
// testcase.Corpus.Validate non-vacuity. See TESTING.md for the standard.
//
// These corpora live under cmd/bestiary-gen/testdata/gen/ rather than the root
// testdata/ because they are inputs to THIS package's test binary; the embeds sit in a
// _test.go file so the corpus never ships inside the generator binary.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/gen/slug_to_identifier_corpus.json
var genSlugToIdentifierCorpusJSON []byte

//go:embed testdata/gen/slug_to_identifier_digit_leading_corpus.json
var genSlugToIdentifierDigitLeadingCorpusJSON []byte

//go:embed testdata/gen/slug_to_identifier_chatgpt_corpus.json
var genSlugToIdentifierChatGPTCorpusJSON []byte

//go:embed testdata/gen/provider_const_name_corpus.json
var genProviderConstNameCorpusJSON []byte

//go:embed testdata/gen/split_comma_corpus.json
var genSplitCommaCorpusJSON []byte

//go:embed testdata/gen/style_segment_curated_corpus.json
var genStyleSegmentCuratedCorpusJSON []byte

//go:embed testdata/gen/style_segment_default_titlecase_corpus.json
var genStyleSegmentDefaultTitleCaseCorpusJSON []byte

//go:embed testdata/gen/style_segment_digit_leading_corpus.json
var genStyleSegmentDigitLeadingCorpusJSON []byte

//go:embed testdata/gen/entity_const_name_pinned_corpus.json
var genEntityConstNamePinnedCorpusJSON []byte

// genSlugInput is the (slug, name hint) pair the provider/family symbol builders take.
// The hint is the upstream display name; it supplies casing only for tokens the curated
// brand table does not claim.
type genSlugInput struct {
	Slug     string `json:"slug"`
	NameHint string `json:"name_hint"`
}

// genStyleSegmentInput is the (token, preserveDigitSuffix) pair styleSegment takes. The
// bool is the ONLY difference between the Model__/Entity__ segment rule and the slug
// rule for an uncurated single-char alpha suffix, so it is a corpus input, not a
// per-test constant.
type genStyleSegmentInput struct {
	Token    string `json:"token"`
	Preserve bool   `json:"preserve"`
}

// genEntityRefInput is the JSON-decodable projection of a bestiary.EntityRef driven
// through entityConstName by the grammar corpus.
type genEntityRefInput struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

// loadGenCorpus loads a generator corpus under the exact case-count control (wantN, the
// pre-migration inline row count) and the non-vacuity guard.
func loadGenCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load bestiary-gen corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("bestiary-gen corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// genRequireInputCoverage is the value-based coverage guard: it asserts the
// load-bearing inputs are still present BY VALUE with their expected output, catching a
// count-preserving swap the exact-count control cannot see.
func genRequireInputCoverage[I comparable, E comparable](t *testing.T, corpus testcase.Corpus[I, E], probes map[I]E) {
	t.Helper()
	have := make(map[I]E, len(corpus.Cases))
	for _, c := range corpus.Cases {
		have[c.Input] = c.Expected
	}
	for in, want := range probes {
		got, ok := have[in]
		if !ok {
			t.Errorf("value coverage lost: no case with input %v", in)
			continue
		}
		if got != want {
			t.Errorf("value coverage: input %v has expected %v, want %v", in, got, want)
		}
	}
}

// genRequireNameCoverage is the keyed coverage guard for corpora whose input type is
// not comparable (the entity-ref corpus carries a modifier slice): it asserts each
// probed case NAME survives, so a count-preserving swap still reddens.
func genRequireNameCoverage[I any, E any](t *testing.T, corpus testcase.Corpus[I, E], names ...string) {
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

// runGenSlugCorpus drives slugToIdentifier over every case of a (slug, name hint)
// corpus — shared by the three slugToIdentifier corpora.
func runGenSlugCorpus(t *testing.T, corpus testcase.Corpus[genSlugInput, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := slugToIdentifier(c.Input.Slug, c.Input.NameHint); got != c.Expected {
				t.Errorf("slugToIdentifier(%q, %q) = %q, want %q", c.Input.Slug, c.Input.NameHint, got, c.Expected)
			}
		})
	}
}
