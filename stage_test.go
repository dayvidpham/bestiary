package bestiary_test

import (
	"errors"
	"testing"

	bestiary "github.com/dayvidpham/bestiary"
)

// TestReleaseStage_StringRoundTrip pins the canonical wire name of every enum
// member and verifies String/MarshalText/UnmarshalText round-trip.
func TestReleaseStage_StringRoundTrip(t *testing.T) {
	cases := []struct {
		stage bestiary.ReleaseStage
		name  string
	}{
		{bestiary.StageNone, "none"},
		{bestiary.StageStable, "stable"},
		{bestiary.StagePreview, "preview"},
		{bestiary.StageBeta, "beta"},
		{bestiary.StageAlpha, "alpha"},
		{bestiary.StageExperimental, "experimental"},
		{bestiary.StageLatest, "latest"},
		{bestiary.StageOriginal, "original"},
		{bestiary.StageOther, "other"},
	}
	for _, c := range cases {
		if got := c.stage.String(); got != c.name {
			t.Errorf("ReleaseStage(%d).String() = %q, want %q", int(c.stage), got, c.name)
		}
		text, err := c.stage.MarshalText()
		if err != nil {
			t.Errorf("MarshalText(%v) error: %v", c.stage, err)
			continue
		}
		if string(text) != c.name {
			t.Errorf("MarshalText(%v) = %q, want %q", c.stage, text, c.name)
		}
		var back bestiary.ReleaseStage
		if err := back.UnmarshalText([]byte(c.name)); err != nil {
			t.Errorf("UnmarshalText(%q) error: %v", c.name, err)
			continue
		}
		if back != c.stage {
			t.Errorf("UnmarshalText(%q) = %v, want %v", c.name, back, c.stage)
		}
	}
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
func TestParseReleaseStage(t *testing.T) {
	if s, err := bestiary.ParseReleaseStage(""); err != nil || s != bestiary.StageNone {
		t.Errorf("ParseReleaseStage(\"\") = (%v, %v), want (StageNone, nil)", s, err)
	}
	accepted := map[string]bestiary.ReleaseStage{
		"stable":       bestiary.StageStable,
		"preview":      bestiary.StagePreview,
		"beta":         bestiary.StageBeta,
		"alpha":        bestiary.StageAlpha,
		"experimental": bestiary.StageExperimental,
		"latest":       bestiary.StageLatest,
		"original":     bestiary.StageOriginal,
		"BETA":         bestiary.StageBeta, // case-insensitive
	}
	for in, want := range accepted {
		got, err := bestiary.ParseReleaseStage(in)
		if err != nil {
			t.Errorf("ParseReleaseStage(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseReleaseStage(%q) = %v, want %v", in, got, want)
		}
	}
	// The internal "other" sentinel is not user-selectable.
	if _, err := bestiary.ParseReleaseStage("other"); err == nil {
		t.Error("ParseReleaseStage(\"other\") = nil error, want rejection (internal sentinel)")
	}
	// An unknown token is a hard error, never a silent Other.
	if _, err := bestiary.ParseReleaseStage("gamma"); err == nil {
		t.Error("ParseReleaseStage(\"gamma\") = nil error, want actionable error")
	}
}

// TestDetectReleaseStage pins the ID-detection primitive: the four ID-detected
// tokens resolve to their constants; the deferred tokens and the stable-diffusion
// guard resolve to (StageNone, false); nothing ever resolves to StageOther.
func TestDetectReleaseStage(t *testing.T) {
	detected := map[string]bestiary.ReleaseStage{
		"preview":  bestiary.StagePreview,
		"beta":     bestiary.StageBeta,
		"latest":   bestiary.StageLatest,
		"original": bestiary.StageOriginal,
		"Beta":     bestiary.StageBeta, // case-insensitive standalone token
	}
	for tok, want := range detected {
		got, ok := bestiary.DetectReleaseStage(tok)
		if !ok || got != want {
			t.Errorf("DetectReleaseStage(%q) = (%v, %v), want (%v, true)", tok, got, ok, want)
		}
	}
	// Deferred / not-ID-detected this epoch → (StageNone, false).
	for _, tok := range []string{"stable", "alpha", "experimental", "exp", "", "instruct", "frobnicate"} {
		got, ok := bestiary.DetectReleaseStage(tok)
		if ok || got != bestiary.StageNone {
			t.Errorf("DetectReleaseStage(%q) = (%v, %v), want (StageNone, false)", tok, got, ok)
		}
	}
	// Family-member guard: a compound family token is never a standalone stage.
	if got, ok := bestiary.DetectReleaseStage("stable-diffusion"); ok || got != bestiary.StageNone {
		t.Errorf("DetectReleaseStage(stable-diffusion) = (%v, %v), want (StageNone, false)", got, ok)
	}
}

// TestDetectStageFromID drives the shared ID scanner used at every enrichment
// joint over real catalog exemplars (and the deferred / guarded negatives).
func TestDetectStageFromID(t *testing.T) {
	cases := []struct {
		id    string
		stage bestiary.ReleaseStage
	}{
		// MIGRATED tokens, now the stage axis.
		{"chatgpt-4o-latest", bestiary.StageLatest},
		{"claude-3-5-haiku-latest", bestiary.StageLatest},
		{"gemini-2.5-flash-preview-09-2025", bestiary.StagePreview},
		{"gemini-3-pro-preview", bestiary.StagePreview},
		{"moonshotai/kimi-k2-thinking-turbo-original", bestiary.StageOriginal},
		// DETECT-WITHOUT-STRIP beta exemplar (exact catalog ID). Scanner unit
		// exemplar only — the full beta ROW SET is never hand-enumerated here: it is
		// census-derived over the whole catalog by TestStageBeta_CensusDerived
		// (stage_migration_test.go), so a new beta ID can never escape a hand glob.
		{"grok-4.20-beta-0309-reasoning", bestiary.StageBeta},
		// NOT ID-detected this epoch → StageNone.
		{"deepseek-ai/deepseek-v3.2-exp", bestiary.StageNone},          // -exp exemplar
		{"deepseek-ai/deepseek-v3.2-exp-thinking", bestiary.StageNone}, // -exp exemplar
		{"learnlm-1.5-pro-experimental", bestiary.StageNone},
		{"openrouter/owl-alpha", bestiary.StageNone},
		// Family-member guard: "stable" is a family name, never a stage.
		{"stable-diffusion-3.5-large", bestiary.StageNone},
		{"fal-ai/stable-audio-25/text-to-audio", bestiary.StageNone},
		// No stage marker.
		{"meta/llama-4-scout-17b-16e", bestiary.StageNone},
		{"claude-opus-4-5-20251101", bestiary.StageNone},
	}
	for _, c := range cases {
		got, raw := bestiary.DetectStageFromID(bestiary.ModelID(c.id))
		if got != c.stage {
			t.Errorf("DetectStageFromID(%q) stage = %v, want %v", c.id, got, c.stage)
		}
		// StageRaw is reserved for the StageOther path; the ID scan never produces
		// StageOther, so raw is always empty this epoch.
		if raw != "" {
			t.Errorf("DetectStageFromID(%q) raw = %q, want \"\" (reserved for the Other path)", c.id, raw)
		}
	}
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
