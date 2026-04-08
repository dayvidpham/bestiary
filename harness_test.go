package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestHarness_IsKnown(t *testing.T) {
	known := []bestiary.Harness{
		bestiary.HarnessClaudeCode,
		bestiary.HarnessGeminiCLI,
		bestiary.HarnessCodex,
		bestiary.HarnessOpenCode,
		bestiary.HarnessCursor,
		bestiary.HarnessAntigravity,
	}
	for _, h := range known {
		if !h.IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = false, want true", h)
		}
	}

	unknown := []bestiary.Harness{
		"",
		"CLAUDE-CODE",
		"unknown-harness",
	}
	for _, h := range unknown {
		if h.IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = true, want false", h)
		}
	}
}

func TestHarness_String(t *testing.T) {
	cases := []struct {
		h    bestiary.Harness
		want string
	}{
		{bestiary.HarnessClaudeCode, "claude-code"},
		{bestiary.HarnessGeminiCLI, "gemini-cli"},
		{bestiary.HarnessCodex, "codex"},
		{bestiary.HarnessOpenCode, "opencode"},
		{bestiary.HarnessCursor, "cursor"},
		{bestiary.HarnessAntigravity, "antigravity"},
	}
	for _, tc := range cases {
		if got := tc.h.String(); got != tc.want {
			t.Errorf("Harness(%q).String() = %q, want %q", tc.h, got, tc.want)
		}
	}
}

func TestHarness_MarshalUnmarshalText(t *testing.T) {
	harnesses := []bestiary.Harness{
		bestiary.HarnessClaudeCode,
		bestiary.HarnessGeminiCLI,
		bestiary.HarnessCodex,
		bestiary.HarnessOpenCode,
		bestiary.HarnessCursor,
		bestiary.HarnessAntigravity,
	}
	for _, h := range harnesses {
		b, err := h.MarshalText()
		if err != nil {
			t.Errorf("Harness(%q).MarshalText() error = %v", h, err)
			continue
		}
		var got bestiary.Harness
		if err := got.UnmarshalText(b); err != nil {
			t.Errorf("Harness.UnmarshalText(%q) error = %v", b, err)
			continue
		}
		if got != h {
			t.Errorf("round-trip: got %q, want %q", got, h)
		}
	}
}

func TestHarness_UnmarshalText_Permissive(t *testing.T) {
	var h bestiary.Harness
	if err := h.UnmarshalText([]byte("some-unknown-harness")); err != nil {
		t.Fatalf("UnmarshalText(unknown) error = %v, want nil", err)
	}
	if h.IsKnown() {
		t.Errorf("Harness(%q).IsKnown() = true, want false for unknown value", h)
	}
}

func TestHarness_UnmarshalText_NilReceiver(t *testing.T) {
	var h *bestiary.Harness
	err := h.UnmarshalText([]byte("claude-code"))
	if err == nil {
		t.Fatal("UnmarshalText on nil receiver: got nil error, want error")
	}
}

func TestHarnesses_AllKnown(t *testing.T) {
	for _, h := range bestiary.Harnesses() {
		if !h.IsKnown() {
			t.Errorf("Harnesses() returned %q which IsKnown() = false", h)
		}
	}
}

func TestHarnesses_Count(t *testing.T) {
	if got := len(bestiary.Harnesses()); got != 6 {
		t.Errorf("len(Harnesses()) = %d, want 6", got)
	}
}

func TestHarnesses_DefensiveCopy(t *testing.T) {
	first := bestiary.Harnesses()
	first[0] = "tampered"
	second := bestiary.Harnesses()
	if second[0] == "tampered" {
		t.Error("Harnesses() returned a non-defensive copy: mutation affected subsequent call")
	}
}
