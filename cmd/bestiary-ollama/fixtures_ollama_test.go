package main

// Embedded JSON case corpus for the bestiary-ollama name-normalization table, plus its
// loader and coverage guard. Guarded by an exact case-count control, a value-based
// coverage assertion, and testcase.Corpus.Validate non-vacuity. See TESTING.md.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/ollama/normalize_ollama_name_corpus.json
var ollamaNormalizeNameCorpusJSON []byte

// loadOllamaCorpus loads a corpus under the exact case-count control (wantN, the
// pre-migration inline row count) and the non-vacuity guard.
func loadOllamaCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load bestiary-ollama corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("bestiary-ollama corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// ollamaRequireInputCoverage is the value-based coverage guard: it catches a
// count-preserving swap that drops a load-bearing case and adds a filler.
func ollamaRequireInputCoverage[I comparable, E comparable](t *testing.T, corpus testcase.Corpus[I, E], probes map[I]E) {
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
