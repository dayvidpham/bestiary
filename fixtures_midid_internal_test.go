package bestiary

// Embedded JSON case corpora for the internal mid-ID token engine tests, plus
// the input/expected types and a local loader. This lives in package bestiary
// (internal) so it can reach the unexported extractModifiers and idFamilyOverrides;
// it therefore cannot reuse the loadParseCorpus helper in the external
// bestiary_test package, so it carries its own thin loader. See TESTING.md.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/midid/stage_override_equivalence_corpus.json
var mididStageOverrideEquivalenceCorpusJSON []byte

//go:embed testdata/midid/realtime_modifier_only_corpus.json
var mididRealtimeModifierOnlyCorpusJSON []byte

//go:embed testdata/midid/mid_id_harvest_corpus.json
var mididMidIDHarvestCorpusJSON []byte

// mididDecompExpected is the retained decomposition tuple a retired stage/mode
// override must reproduce mechanically. Mods is the pre-canonicalization list.
type mididDecompExpected struct {
	Family  string   `json:"family"`
	Variant string   `json:"variant"`
	Version string   `json:"version"`
	Mods    []string `json:"mods"`
}

// mididHarvestInput is the (id, family, variant) triple fed to extractModifiers.
type mididHarvestInput struct {
	ID      string `json:"id"`
	Family  string `json:"family"`
	Variant string `json:"variant"`
}

// mididHarvestExpected is the (mods, consumed) result of the phase-B harvest.
type mididHarvestExpected struct {
	Mods     []string `json:"mods"`
	Consumed string   `json:"consumed"`
}

// loadMididCorpus loads a corpus, enforces the exact case-count control (wantN,
// the pre-migration inline row count) and the non-vacuity guard.
func loadMididCorpus[I any, E any](t *testing.T, data []byte, wantN int) testcase.Corpus[I, E] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[I, E](data)
	if err != nil {
		t.Fatalf("load midid corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("midid corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// mididRequireNames is the internal-package twin of requireNameCoverage (the
// external bestiary_test package cannot be imported from here, mirroring the
// loadParseCorpus/loadMididCorpus split): the keyed value-coverage guard for
// corpora whose input or expected type is not comparable (the mods slices). It
// asserts each probed case NAME is still present, so a count-preserving swap
// that drops a load-bearing case and adds a filler (necessarily under a
// different name) reddens even at a fixed count.
func mididRequireNames[I any, E any](t *testing.T, corpus testcase.Corpus[I, E], names ...string) {
	t.Helper()
	have := map[string]bool{}
	for _, c := range corpus.Cases {
		have[c.Name] = true
	}
	for _, n := range names {
		if !have[n] {
			t.Errorf("value coverage lost: midid case named %q is missing", n)
		}
	}
}
