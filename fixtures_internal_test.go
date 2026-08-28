package bestiary

// Embedded JSON case corpora for the internal (package bestiary) unit tests that reach
// unexported seams — the resolve group-key helpers (parseContextN, isBareIdentifier)
// and the parse date guard (isFourDigitDateToken). Like fixtures_midid_internal_test.go
// this file lives in the internal package, so it cannot reuse the loadParseCorpus /
// requireInputCoverage helpers that live in the external bestiary_test package and
// carries neutral twins of them instead. See TESTING.md.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/resolve/parse_context_n_corpus.json
var internalParseContextNCorpusJSON []byte

//go:embed testdata/resolve/is_bare_identifier_corpus.json
var internalIsBareIdentifierCorpusJSON []byte

//go:embed testdata/parse/is_yymm_date_token_corpus.json
var internalIsYYMMDateTokenCorpusJSON []byte

//go:embed testdata/parse/p_notation_version_corpus.json
var internalPNotationVersionCorpusJSON []byte

//go:embed testdata/parse/dot_lost_version_corpus.json
var internalDotLostVersionCorpusJSON []byte

//go:embed testdata/parse/param_size_1t_corpus.json
var internalParamSize1TCorpusJSON []byte

//go:embed testdata/parse/curation_repair_v028_corpus.json
var internalCurationRepairV028CorpusJSON []byte

//go:embed testdata/parse/compound_recovery_corpus.json
var internalCompoundRecoveryCorpusJSON []byte

// pNotationInput is one row of the p-notation corpus. It carries EITHER a bare Token
// (a unit probe of decodePNotationVersion) OR an Id + Provider (a catalog probe of
// the entity a real registry row resolves to) — the two kinds share a corpus because
// they pin one rule at its two levels, and the runner dispatches on which field is set.
type pNotationInput struct {
	Token    string `json:"token"`
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// pNotationExpected is the decoded version for a unit row, or the entity key for a
// catalog row. An empty Decoded on a must-fail unit row means "must not decode".
type pNotationExpected struct {
	Decoded   string `json:"decoded"`
	EntityKey string `json:"entity_key"`
}

// loadInternalCorpus loads a corpus for an internal-package test under the exact
// case-count control (wantN, the pre-migration inline row count) and the non-vacuity
// guard.
func loadInternalCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load internal corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("internal corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// internalRequireInputCoverage is the internal-package twin of requireInputCoverage:
// the value-based coverage guard that catches a count-preserving swap (a load-bearing
// case dropped and a filler added), which the exact-count control cannot see.
func internalRequireInputCoverage[I comparable, E comparable](t *testing.T, corpus testcase.Corpus[I, E], probes map[I]E) {
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
