package bestiary_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestErrNotFound_ErrorMessage(t *testing.T) {
	err := &bestiary.ErrNotFound{What: "model", Key: "foo"}
	got := err.Error()
	want := "bestiary: model not found: foo"
	if got != want {
		t.Errorf("ErrNotFound.Error() = %q, want %q", got, want)
	}
}

func TestErrNotFound_ErrorsAs(t *testing.T) {
	err := &bestiary.ErrNotFound{What: "model", Key: "claude-3"}
	var target *bestiary.ErrNotFound
	if !errors.As(err, &target) {
		t.Fatal("errors.As(*ErrNotFound) = false, want true")
	}
	if target.What != "model" {
		t.Errorf("target.What = %q, want %q", target.What, "model")
	}
	if target.Key != "claude-3" {
		t.Errorf("target.Key = %q, want %q", target.Key, "claude-3")
	}
}

func TestErrNotFound_WrappedErrorsAs(t *testing.T) {
	inner := &bestiary.ErrNotFound{What: "provider", Key: "openrouter"}
	wrapped := fmt.Errorf("lookup failed: %w", inner)
	var target *bestiary.ErrNotFound
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As on wrapped ErrNotFound = false, want true")
	}
	if target.Key != "openrouter" {
		t.Errorf("target.Key = %q, want %q", target.Key, "openrouter")
	}
}

func TestErrAPIUnavailable_ErrorMessage(t *testing.T) {
	cause := errors.New("connection refused")
	err := &bestiary.ErrAPIUnavailable{
		URL:      "https://api.models.dev/v1/models",
		Attempts: 3,
		Cause:    cause,
	}
	got := err.Error()
	// Should mention attempts and URL
	if got == "" {
		t.Error("ErrAPIUnavailable.Error() returned empty string")
	}
}

func TestErrAPIUnavailable_ErrorsAs(t *testing.T) {
	cause := errors.New("timeout")
	err := &bestiary.ErrAPIUnavailable{
		URL:      "https://api.models.dev/v1/models",
		Attempts: 2,
		Cause:    cause,
	}
	var target *bestiary.ErrAPIUnavailable
	if !errors.As(err, &target) {
		t.Fatal("errors.As(*ErrAPIUnavailable) = false, want true")
	}
	if target.Attempts != 2 {
		t.Errorf("target.Attempts = %d, want %d", target.Attempts, 2)
	}
	if target.URL != "https://api.models.dev/v1/models" {
		t.Errorf("target.URL = %q, want %q", target.URL, "https://api.models.dev/v1/models")
	}
}

func TestErrAPIUnavailable_Unwrap(t *testing.T) {
	cause := errors.New("no route to host")
	err := &bestiary.ErrAPIUnavailable{
		URL:      "https://api.models.dev/v1/models",
		Attempts: 1,
		Cause:    cause,
	}
	if !errors.Is(err, cause) {
		t.Error("errors.Is(ErrAPIUnavailable, cause) = false, want true (Unwrap must return Cause)")
	}
}

func TestErrAPIUnavailable_WrappedErrorsAs(t *testing.T) {
	cause := errors.New("dns failure")
	inner := &bestiary.ErrAPIUnavailable{
		URL:      "https://api.models.dev/v1/models",
		Attempts: 5,
		Cause:    cause,
	}
	wrapped := fmt.Errorf("sync failed: %w", inner)
	var target *bestiary.ErrAPIUnavailable
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As on wrapped ErrAPIUnavailable = false, want true")
	}
	if target.Attempts != 5 {
		t.Errorf("target.Attempts = %d, want 5", target.Attempts)
	}
}

// TestErrAmbiguous_Format verifies the Error() message format for various
// candidate list sizes: empty, single, and multi-candidate.
func TestErrAmbiguous_Format(t *testing.T) {
	t.Run("empty_candidates", func(t *testing.T) {
		err := &bestiary.ErrAmbiguous{
			Input:      "claude",
			Scheme:     bestiary.SchemeCanonical,
			Candidates: nil,
		}
		msg := err.Error()
		if msg == "" {
			t.Fatal("ErrAmbiguous.Error() returned empty string")
		}
		// Must mention the input and convey ambiguity.
		if !containsAll(msg, "claude", "ambiguous") {
			t.Errorf("ErrAmbiguous.Error() = %q; must mention input and 'ambiguous'", msg)
		}
	})

	t.Run("single_candidate", func(t *testing.T) {
		err := &bestiary.ErrAmbiguous{
			Input:  "claude",
			Scheme: bestiary.SchemeCanonical,
			Candidates: []bestiary.ModelRef{
				{
					Provider: bestiary.ProviderAnthropic,
					Family:   "claude",
					Variant:  "opus",
				},
			},
		}
		msg := err.Error()
		if !containsAll(msg, "claude", "anthropic") {
			t.Errorf("ErrAmbiguous.Error() = %q; must mention candidate family/provider", msg)
		}
	})

	t.Run("multi_candidate", func(t *testing.T) {
		err := &bestiary.ErrAmbiguous{
			Input:  "claude",
			Scheme: bestiary.SchemeCanonical,
			Candidates: []bestiary.ModelRef{
				{Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "opus"},
				{Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "sonnet"},
				{Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "haiku"},
			},
		}
		msg := err.Error()
		// Must mention how-to-fix guidance.
		if !containsAll(msg, "How to fix", "refine") {
			t.Errorf("ErrAmbiguous.Error() = %q; must include how-to-fix guidance", msg)
		}
		// Must list all 3 variants.
		if !containsAll(msg, "opus", "sonnet", "haiku") {
			t.Errorf("ErrAmbiguous.Error() = %q; must list all candidate variants", msg)
		}
	})
}

func TestErrAmbiguous_ErrorsAs(t *testing.T) {
	err := &bestiary.ErrAmbiguous{
		Input:  "gpt",
		Scheme: bestiary.SchemeRaw,
		Candidates: []bestiary.ModelRef{
			{Provider: bestiary.ProviderOpenAI, Family: "gpt", Variant: "4o"},
		},
	}
	var target *bestiary.ErrAmbiguous
	if !errors.As(err, &target) {
		t.Fatal("errors.As(*ErrAmbiguous) = false, want true")
	}
	if target.Input != "gpt" {
		t.Errorf("target.Input = %q, want %q", target.Input, "gpt")
	}
	if target.Scheme != bestiary.SchemeRaw {
		t.Errorf("target.Scheme = %v, want SchemeRaw", target.Scheme)
	}
	if len(target.Candidates) != 1 {
		t.Errorf("len(target.Candidates) = %d, want 1", len(target.Candidates))
	}
}

func TestErrAmbiguous_WrappedErrorsAs(t *testing.T) {
	inner := &bestiary.ErrAmbiguous{
		Input:  "gemini",
		Scheme: bestiary.SchemeCanonical,
	}
	wrapped := fmt.Errorf("resolution failed: %w", inner)
	var target *bestiary.ErrAmbiguous
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As on wrapped ErrAmbiguous = false, want true")
	}
	if target.Input != "gemini" {
		t.Errorf("target.Input = %q, want %q", target.Input, "gemini")
	}
}

// containsAll reports whether s contains every substring in subs.
func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
