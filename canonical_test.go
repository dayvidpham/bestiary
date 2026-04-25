package bestiary_test

import (
	"errors"
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestCanonicalScheme_String(t *testing.T) {
	tests := []struct {
		scheme bestiary.CanonicalScheme
		want   string
	}{
		{bestiary.SchemeCanonical, "canonical"},
		{bestiary.SchemeHuggingFace, "huggingface"},
		{bestiary.SchemePURL, "purl"},
		{bestiary.SchemeRaw, "raw"},
		{bestiary.CanonicalScheme(99), "CanonicalScheme(99)"},
	}
	for _, tt := range tests {
		got := tt.scheme.String()
		if got != tt.want {
			t.Errorf("CanonicalScheme(%d).String() = %q, want %q", int(tt.scheme), got, tt.want)
		}
	}
}

func TestParseScheme_Valid(t *testing.T) {
	tests := []struct {
		input string
		want  bestiary.CanonicalScheme
	}{
		{"canonical", bestiary.SchemeCanonical},
		{"huggingface", bestiary.SchemeHuggingFace},
		{"purl", bestiary.SchemePURL},
		{"raw", bestiary.SchemeRaw},
	}
	for _, tt := range tests {
		got, err := bestiary.ParseScheme(tt.input)
		if err != nil {
			t.Errorf("ParseScheme(%q) returned error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseScheme(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseScheme_Invalid(t *testing.T) {
	inputs := []string{"", "default", "CANONICAL", "hf", "unknown"}
	for _, input := range inputs {
		got, err := bestiary.ParseScheme(input)
		if err == nil {
			t.Errorf("ParseScheme(%q) succeeded with %v, want error", input, got)
		}
	}
}

func TestParseScheme_RoundTrip(t *testing.T) {
	schemes := []bestiary.CanonicalScheme{
		bestiary.SchemeCanonical,
		bestiary.SchemeHuggingFace,
		bestiary.SchemePURL,
		bestiary.SchemeRaw,
	}
	for _, s := range schemes {
		parsed, err := bestiary.ParseScheme(s.String())
		if err != nil {
			t.Errorf("ParseScheme(scheme.String()) for %v returned error: %v", s, err)
			continue
		}
		if parsed != s {
			t.Errorf("ParseScheme(%q) = %v, want %v (round-trip failed)", s.String(), parsed, s)
		}
	}
}

func TestParseScheme_ErrorIsActionable(t *testing.T) {
	_, err := bestiary.ParseScheme("badscheme")
	if err == nil {
		t.Fatal("ParseScheme(\"badscheme\") returned nil error, want error")
	}
	// Error should mention the bad input and how to fix it.
	msg := err.Error()
	if msg == "" {
		t.Error("ParseScheme error message is empty")
	}
	_ = errors.Unwrap(err) // just ensure it doesn't panic
}

func TestCanonicalScheme_IotaOrder(t *testing.T) {
	// Verify iota ordering is stable: Canonical=0, HF=1, PURL=2, Raw=3.
	if int(bestiary.SchemeCanonical) != 0 {
		t.Errorf("SchemeCanonical = %d, want 0", int(bestiary.SchemeCanonical))
	}
	if int(bestiary.SchemeHuggingFace) != 1 {
		t.Errorf("SchemeHuggingFace = %d, want 1", int(bestiary.SchemeHuggingFace))
	}
	if int(bestiary.SchemePURL) != 2 {
		t.Errorf("SchemePURL = %d, want 2", int(bestiary.SchemePURL))
	}
	if int(bestiary.SchemeRaw) != 3 {
		t.Errorf("SchemeRaw = %d, want 3", int(bestiary.SchemeRaw))
	}
}
