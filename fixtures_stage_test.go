package bestiary_test

// Embedded JSON case corpora for the stage-package table-driven tests, plus the
// corpus-runner helpers. See TESTING.md for the corpus standard: each corpus is
// guarded by an exact case-count control, a value-based coverage assertion, and
// testcase.Corpus.Validate non-vacuity.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// ---- ReleaseStage corpora ---------------------------------------------------

//go:embed testdata/stage/release_stage_names_corpus.json
var releaseStageNamesCorpusJSON []byte

//go:embed testdata/stage/detect_stage_from_id_corpus.json
var detectStageFromIDCorpusJSON []byte

//go:embed testdata/stage/detect_release_stage_corpus.json
var detectReleaseStageCorpusJSON []byte

//go:embed testdata/stage/parse_release_stage_corpus.json
var parseReleaseStageCorpusJSON []byte

// runStageNamesCorpus drives ReleaseStage.String/MarshalText/UnmarshalText over
// every case. Input is the stage's ordinal (int(ReleaseStage)); Expected is its
// canonical wire name.
func runStageNamesCorpus(t *testing.T, corpus testcase.Corpus[int, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			stage := bestiary.ReleaseStage(c.Input)
			if got := stage.String(); got != c.Expected {
				t.Errorf("ReleaseStage(%d).String() = %q, want %q", c.Input, got, c.Expected)
			}
			text, err := stage.MarshalText()
			if err != nil {
				t.Errorf("MarshalText(%d) error: %v", c.Input, err)
				return
			}
			if string(text) != c.Expected {
				t.Errorf("MarshalText(%d) = %q, want %q", c.Input, text, c.Expected)
			}
			var back bestiary.ReleaseStage
			if err := back.UnmarshalText([]byte(c.Expected)); err != nil {
				t.Errorf("UnmarshalText(%q) error: %v", c.Expected, err)
				return
			}
			if int(back) != c.Input {
				t.Errorf("UnmarshalText(%q) = %d, want %d", c.Expected, back, c.Input)
			}
		})
	}
}

// requireStageNamesCoverage asserts each probed ordinal is still present with
// its expected wire name. Value-based coverage guard.
func requireStageNamesCoverage(t *testing.T, corpus testcase.Corpus[int, string], probes map[int]string) {
	t.Helper()
	got := map[int]string{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: ReleaseStage names case for ordinal %d is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: ReleaseStage names case for ordinal %d has %q, want %q", in, have, want)
		}
	}
}

// runStageDetectFromIDCorpus drives bestiary.DetectStageFromID over every case.
// StageRaw is reserved for the StageOther path, which the ID scan never
// produces, so raw is asserted empty for every case (an invariant, not
// per-case data).
func runStageDetectFromIDCorpus(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotStage, raw := bestiary.DetectStageFromID(bestiary.ModelID(c.Input))
			if got := gotStage.String(); got != c.Expected {
				t.Errorf("DetectStageFromID(%q) stage = %q, want %q", c.Input, got, c.Expected)
			}
			if raw != "" {
				t.Errorf("DetectStageFromID(%q) raw = %q, want \"\" (reserved for the Other path)", c.Input, raw)
			}
		})
	}
}

// requireStageDetectFromIDCoverage asserts each probed id is still present
// with its expected stage. Value-based coverage guard.
func requireStageDetectFromIDCoverage(t *testing.T, corpus testcase.Corpus[string, string], probes map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: DetectStageFromID case for id %q is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: DetectStageFromID case %q has %q, want %q", in, have, want)
		}
	}
}

// runStageDetectTokenCorpus drives bestiary.DetectReleaseStage over every case.
// A case's Classification determines the expected ok: MustPass -> true,
// MustFail -> false.
func runStageDetectTokenCorpus(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			gotStage, gotOK := bestiary.DetectReleaseStage(c.Input)
			wantOK := c.Classification == testcase.MustPass
			if gotOK != wantOK {
				t.Errorf("DetectReleaseStage(%q) ok = %v, want %v", c.Input, gotOK, wantOK)
			}
			if got := gotStage.String(); got != c.Expected {
				t.Errorf("DetectReleaseStage(%q) stage = %q, want %q", c.Input, got, c.Expected)
			}
		})
	}
}

// requireStageDetectTokenCoverage asserts each probed token is still present
// with its expected stage. Value-based coverage guard.
func requireStageDetectTokenCoverage(t *testing.T, corpus testcase.Corpus[string, string], probes map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: DetectReleaseStage case for token %q is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: DetectReleaseStage case %q has %q, want %q", in, have, want)
		}
	}
}

// runStageParseCorpus drives bestiary.ParseReleaseStage over every case.
// MustFail cases assert an actionable error; MustPass cases assert the parsed
// stage's wire name against Expected.
func runStageParseCorpus(t *testing.T, corpus testcase.Corpus[string, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			got, err := bestiary.ParseReleaseStage(c.Input)
			if c.Classification == testcase.MustFail {
				if err == nil {
					t.Errorf("ParseReleaseStage(%q) = (%v, nil), want an actionable error", c.Input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseReleaseStage(%q) unexpected error: %v", c.Input, err)
				return
			}
			if gotName := got.String(); gotName != c.Expected {
				t.Errorf("ParseReleaseStage(%q) = %v, want %v", c.Input, gotName, c.Expected)
			}
		})
	}
}

// requireStageParseCoverage asserts each probed input is still present with
// its expected wire name. Value-based coverage guard.
func requireStageParseCoverage(t *testing.T, corpus testcase.Corpus[string, string], probes map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, c := range corpus.Cases {
		got[c.Input] = c.Expected
	}
	for in, want := range probes {
		have, ok := got[in]
		if !ok {
			t.Errorf("value coverage lost: ParseReleaseStage case for input %q is missing", in)
			continue
		}
		if have != want {
			t.Errorf("value coverage: ParseReleaseStage case %q has %q, want %q", in, have, want)
		}
	}
}
