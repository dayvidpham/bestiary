package bestiary_test

import (
	"errors"
	"testing"

	bestiary "github.com/dayvidpham/bestiary"
)

// TestReleaseStage_StringRoundTrip pins the canonical wire name of every enum
// member and verifies String/MarshalText/UnmarshalText round-trip. Corpus:
// testdata/stage/release_stage_names_corpus.json (9 cases, one per named
// ReleaseStage constant including the reserved StageOther).
func TestReleaseStage_StringRoundTrip(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[int, string](t, releaseStageNamesCorpusJSON, 9)
	requireStageNamesCoverage(t, corpus, map[int]string{
		int(bestiary.StageBeta):  "beta",
		int(bestiary.StageOther): "other",
	})
	runStageNamesCorpus(t, corpus)
}

// TestReleaseStage_UnmarshalCaseInsensitive verifies parsing is not spelling-fragile.
func TestReleaseStage_UnmarshalCaseInsensitive(t *testing.T) {
	var s bestiary.ReleaseStage
	if err := s.UnmarshalText([]byte("BETA")); err != nil {
		t.Fatalf("UnmarshalText(BETA) error: %v", err)
	}
	if s != bestiary.StageBeta {
		t.Errorf("UnmarshalText(BETA) = %v, want StageBeta", s)
	}
	if err := s.UnmarshalText([]byte("not-a-stage")); err == nil {
		t.Error("UnmarshalText(not-a-stage) = nil error, want actionable error")
	}
}

// TestReleaseStage_IsKnown verifies IsKnown covers every named member (incl. the
// reserved StageOther) and rejects an out-of-range integer.
func TestReleaseStage_IsKnown(t *testing.T) {
	for s := bestiary.StageNone; s <= bestiary.StageOther; s++ {
		if !s.IsKnown() {
			t.Errorf("IsKnown(%v) = false, want true (named member)", s)
		}
	}
	if bestiary.ReleaseStage(999).IsKnown() {
		t.Error("IsKnown(999) = true, want false (out of range)")
	}
	// An out-of-range value renders defensively rather than panicking.
	if got := bestiary.ReleaseStage(999).String(); got == "" {
		t.Error("String(999) = empty, want a defensive releasestage(999) render")
	}
}

// TestParseReleaseStage pins the CLI parse path: empty → None, every named stage
// accepted (including the ones NOT ID-detected this epoch, since Parse is the
// CLI-completeness half), the "other" sentinel rejected, and an unknown token
// yields an actionable error rather than a silent Other.
// Corpus: testdata/stage/parse_release_stage_corpus.json (11 cases).
func TestParseReleaseStage(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, string](t, parseReleaseStageCorpusJSON, 11)
	requireStageParseCoverage(t, corpus, map[string]string{
		"":     "none",
		"BETA": "beta",
	})
	runStageParseCorpus(t, corpus)
}

// TestDetectReleaseStage pins the ID-detection primitive: the four ID-detected
// tokens resolve to their constants; the deferred tokens and the stable-diffusion
// guard resolve to (StageNone, false); nothing ever resolves to StageOther.
// Corpus: testdata/stage/detect_release_stage_corpus.json (13 cases).
func TestDetectReleaseStage(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, string](t, detectReleaseStageCorpusJSON, 13)
	requireStageDetectTokenCoverage(t, corpus, map[string]string{
		"preview":          "preview",
		"stable":           "none",
		"stable-diffusion": "none",
	})
	runStageDetectTokenCorpus(t, corpus)
}

// TestDetectStageFromID drives the shared ID scanner used at every enrichment
// joint over real catalog exemplars (and the deferred / guarded negatives).
// Corpus: testdata/stage/detect_stage_from_id_corpus.json (14 cases).
func TestDetectStageFromID(t *testing.T) {
	t.Parallel()
	corpus := loadParseCorpus[string, string](t, detectStageFromIDCorpusJSON, 14)
	requireStageDetectFromIDCoverage(t, corpus, map[string]string{
		"grok-4.20-beta-0309-reasoning": "beta",
		"chatgpt-4o-latest":             "latest",
		"stable-diffusion-3.5-large":    "none",
	})
	runStageDetectFromIDCorpus(t, corpus)
}

// TestParseReleaseStage_ErrorActionable checks the error is an ordinary error
// value (used via the standard errors package by CLI callers).
func TestParseReleaseStage_ErrorActionable(t *testing.T) {
	_, err := bestiary.ParseReleaseStage("nope")
	if err == nil {
		t.Fatal("want error for unknown stage")
	}
	if errors.Is(err, nil) {
		t.Fatal("error should be non-nil")
	}
}
