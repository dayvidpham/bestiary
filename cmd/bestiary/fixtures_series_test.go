package main

// Embedded JSON case corpora for the `series` selector tables, plus their shared
// input/expected types and the loader helper. See TESTING.md for the corpus
// standard: each corpus is guarded by an exact case-count control, a value-based
// coverage assertion, and testcase.Corpus.Validate non-vacuity.
//
// This is the first corpus area under cmd/bestiary. The rows here are AUTHORED data
// facts (a selector and the lines it must return), which is what puts them in a
// corpus rather than inline; the argv/flag-mechanics vectors in cli_warts_test.go
// stay inline under the same rule, because those rows exercise flag parsing rather
// than pinning catalog facts.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
)

// ---- end-to-end selector resolution ----------------------------------------

//go:embed testdata/series/selector_resolution_corpus.json
var seriesSelectorResolutionCorpusJSON []byte

// seriesResolutionInput is one `bestiary series` invocation: the positional
// selector plus the flags that participate in SELECTION (not the entity filters,
// which are exercised by series_filter_cli_test.go). Empty fields mean "flag not
// given", which is the load-bearing default for every case that omits them.
type seriesResolutionInput struct {
	Selector    string `json:"selector"`
	Version     string `json:"version"`
	InputFormat string `json:"input_format"`
	Provider    string `json:"provider"`
}

// args renders the input as the argv the CLI would receive, so the corpus drives
// run() exactly as a user would invoke it.
func (in seriesResolutionInput) args() []string {
	out := []string{"series", "--output=json", in.Selector}
	if in.Version != "" {
		out = append(out, "--version", in.Version)
	}
	if in.InputFormat != "" {
		out = append(out, "--input-format", in.InputFormat)
	}
	if in.Provider != "" {
		out = append(out, "--provider", in.Provider)
	}
	return out
}

// seriesResolutionExpected is what the invocation must produce. Series is the
// rendered line names in output order. Releases, when non-empty, additionally pins
// the release names EVERY returned line must show — the release-level cut. For a
// must-fail case, ErrorContains lists substrings the error must name.
type seriesResolutionExpected struct {
	Series        []string `json:"series"`
	Releases      []string `json:"releases"`
	ErrorContains []string `json:"error_contains"`
}

// ---- the strict major-union membership rule (unit) --------------------------

//go:embed testdata/series/major_union_membership_corpus.json
var seriesMajorUnionMembershipCorpusJSON []byte

// seriesMembershipInput is one (generation, version) pair probed against the strict
// union rule.
type seriesMembershipInput struct {
	Generation string `json:"generation"`
	Version    string `json:"version"`
}

// ---- --version composition (unit) -------------------------------------------

//go:embed testdata/series/version_flag_compose_corpus.json
var seriesVersionComposeCorpusJSON []byte

// seriesComposeInput is one applyVersionFlag call.
type seriesComposeInput struct {
	Selector    string `json:"selector"`
	Version     string `json:"version"`
	InputFormat string `json:"input_format"`
}

// seriesComposeExpected is the candidate selector spellings the fold must produce,
// or the substrings a rejection must name.
type seriesComposeExpected struct {
	Candidates    []string `json:"candidates"`
	ErrorContains []string `json:"error_contains"`
}

// ---- selectSeries readings over a synthetic universe (unit) -----------------

//go:embed testdata/series/select_series_readings_corpus.json
var seriesSelectReadingsCorpusJSON []byte

// loadSeriesCorpus loads a series-area corpus under the three-guard discipline:
// an EXACT case count (a floor would let a silent drop pass), non-vacuity via
// Validate, and — at the call site — a value-based coverage assertion that a
// count-preserving swap cannot slip past.
func loadSeriesCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load series corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("series corpus has %d cases, want exactly %d — update the literal in the same "+
			"commit if a case was added or retired deliberately", got, wantN)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("series corpus is under-populated: %v", err)
	}
	return corpus
}
