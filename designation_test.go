package bestiary_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestAcceptabilityRating_String(t *testing.T) {
	tests := []struct {
		rating bestiary.AcceptabilityRating
		want   string
	}{
		{bestiary.AcceptabilityAdmitted, "admitted"},
		{bestiary.AcceptabilityPreferred, "preferred"},
		{bestiary.AcceptabilityDeprecated, "deprecated"},
		{bestiary.AcceptabilityRating(99), "AcceptabilityRating(99)"},
	}
	for _, tt := range tests {
		got := tt.rating.String()
		if got != tt.want {
			t.Errorf("AcceptabilityRating(%d).String() = %q, want %q", int(tt.rating), got, tt.want)
		}
	}
}

func TestAcceptabilityRating_IotaOrder(t *testing.T) {
	if int(bestiary.AcceptabilityAdmitted) != 0 {
		t.Errorf("AcceptabilityAdmitted = %d, want 0", int(bestiary.AcceptabilityAdmitted))
	}
	if int(bestiary.AcceptabilityPreferred) != 1 {
		t.Errorf("AcceptabilityPreferred = %d, want 1", int(bestiary.AcceptabilityPreferred))
	}
	if int(bestiary.AcceptabilityDeprecated) != 2 {
		t.Errorf("AcceptabilityDeprecated = %d, want 2", int(bestiary.AcceptabilityDeprecated))
	}
}

func TestDesignation_ZeroValue(t *testing.T) {
	var d bestiary.Designation
	// Zero value should be AcceptabilityAdmitted.
	if d.Rating != bestiary.AcceptabilityAdmitted {
		t.Errorf("zero-value Designation.Rating = %v, want AcceptabilityAdmitted", d.Rating)
	}
}

// TestDesignation_JSONRoundTrip verifies that a Designation survives JSON
// marshal → unmarshal with all fields intact. This exercises the observable
// serialization behavior (MarshalJSON on Scheme and Rating) rather than just
// struct field wiring.
func TestDesignation_JSONRoundTrip(t *testing.T) {
	original := bestiary.Designation{
		Value:    "anthropic/claude/opus@2025-05-14",
		Scheme:   bestiary.SchemeCanonical,
		Provider: bestiary.ProviderAnthropic,
		Rating:   bestiary.AcceptabilityAdmitted,
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal(Designation) error: %v", err)
	}

	var got bestiary.Designation
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal(Designation) error: %v", err)
	}

	if got.Value != original.Value {
		t.Errorf("Value: got %q, want %q", got.Value, original.Value)
	}
	if got.Scheme != original.Scheme {
		t.Errorf("Scheme: got %v, want %v", got.Scheme, original.Scheme)
	}
	if got.Provider != original.Provider {
		t.Errorf("Provider: got %q, want %q", got.Provider, original.Provider)
	}
	if got.Rating != original.Rating {
		t.Errorf("Rating: got %v, want %v", got.Rating, original.Rating)
	}

	// Verify that Scheme and Rating are serialized as strings (not numbers).
	s := string(b)
	if !strings.Contains(s, `"canonical"`) {
		t.Errorf("expected Scheme to serialize as %q in JSON; got: %s", "canonical", s)
	}
	if !strings.Contains(s, `"admitted"`) {
		t.Errorf("expected Rating to serialize as %q in JSON; got: %s", "admitted", s)
	}
}

func TestDesignation_AdmittedIsDefault(t *testing.T) {
	// Verify that the zero AcceptabilityRating is Admitted, not Preferred or Deprecated.
	// This is an invariant for all epoch-generated designations.
	var rating bestiary.AcceptabilityRating
	if rating != bestiary.AcceptabilityAdmitted {
		t.Errorf("zero AcceptabilityRating = %v, want AcceptabilityAdmitted (zero iota)", rating)
	}
}

func TestAcceptabilityRating_JSON_RoundTrip(t *testing.T) {
	ratings := []bestiary.AcceptabilityRating{
		bestiary.AcceptabilityAdmitted,
		bestiary.AcceptabilityPreferred,
		bestiary.AcceptabilityDeprecated,
	}
	for _, r := range ratings {
		b, err := json.Marshal(r)
		if err != nil {
			t.Errorf("json.Marshal(%v) error: %v", r, err)
			continue
		}
		var got bestiary.AcceptabilityRating
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) error: %v", b, err)
			continue
		}
		if got != r {
			t.Errorf("round-trip %v: got %v", r, got)
		}
	}
}

func TestAcceptabilityRating_UnmarshalJSON_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  bestiary.AcceptabilityRating
	}{
		{`"ADMITTED"`, bestiary.AcceptabilityAdmitted},
		{`"Admitted"`, bestiary.AcceptabilityAdmitted},
		{`"PREFERRED"`, bestiary.AcceptabilityPreferred},
		{`"Preferred"`, bestiary.AcceptabilityPreferred},
		{`"DEPRECATED"`, bestiary.AcceptabilityDeprecated},
		{`"Deprecated"`, bestiary.AcceptabilityDeprecated},
	}
	for _, tt := range tests {
		var got bestiary.AcceptabilityRating
		if err := json.Unmarshal([]byte(tt.input), &got); err != nil {
			t.Errorf("Unmarshal(%s) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Unmarshal(%s) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestAcceptabilityRating_UnmarshalJSON_RejectsBadInput(t *testing.T) {
	tests := []struct {
		desc  string
		input string
	}{
		{"wrong type: number", `42`},
		{"wrong type: null", `null`},
		{"unknown string value", `"bogus"`},
	}
	for _, tt := range tests {
		var got bestiary.AcceptabilityRating
		err := json.Unmarshal([]byte(tt.input), &got)
		if err == nil {
			t.Errorf("Unmarshal(%s) [%s] succeeded with %v, want error", tt.input, tt.desc, got)
			continue
		}
		msg := err.Error()
		if msg == "" {
			t.Errorf("Unmarshal(%s) [%s] returned empty error message", tt.input, tt.desc)
		}
	}
}
