package bestiary

import (
	"strings"
	"testing"
)

// TestParseRating_RoundTrip verifies that parseRating(r.String()) == r
// for every defined AcceptabilityRating constant.
func TestParseRating_RoundTrip(t *testing.T) {
	ratings := []AcceptabilityRating{
		AcceptabilityAdmitted,
		AcceptabilityPreferred,
		AcceptabilityDeprecated,
	}
	for _, r := range ratings {
		got, err := parseRating(r.String())
		if err != nil {
			t.Errorf("parseRating(%q) error: %v", r.String(), err)
			continue
		}
		if got != r {
			t.Errorf("parseRating(%q) = %v, want %v", r.String(), got, r)
		}
	}
}

// TestParseRating_UnknownInput verifies that parseRating returns a non-nil,
// actionable error for unrecognized input strings.
func TestParseRating_UnknownInput(t *testing.T) {
	unknowns := []string{"", "bogus", "ADMITTED", "Preferred", "unknown-rating"}
	for _, s := range unknowns {
		got, err := parseRating(s)
		if err == nil {
			t.Errorf("parseRating(%q) succeeded with %v, want error", s, got)
			continue
		}
		msg := err.Error()
		if msg == "" {
			t.Errorf("parseRating(%q) returned empty error message", s)
		}
		// Error should mention the bad input and how to fix it.
		if !strings.Contains(msg, s) && s != "" {
			t.Errorf("parseRating(%q) error message does not mention the input: %q", s, msg)
		}
	}
}
