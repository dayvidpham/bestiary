package bestiary_test

import (
	"errors"
	"fmt"
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
