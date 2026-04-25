package bestiary_test

import (
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

func TestDesignation_Fields(t *testing.T) {
	d := bestiary.Designation{
		Value:    "anthropic/claude/opus@2025-05-14",
		Scheme:   bestiary.SchemeCanonical,
		Provider: bestiary.ProviderAnthropic,
		Rating:   bestiary.AcceptabilityAdmitted,
	}
	if d.Value != "anthropic/claude/opus@2025-05-14" {
		t.Errorf("Designation.Value = %q, want %q", d.Value, "anthropic/claude/opus@2025-05-14")
	}
	if d.Scheme != bestiary.SchemeCanonical {
		t.Errorf("Designation.Scheme = %v, want SchemeCanonical", d.Scheme)
	}
	if d.Provider != bestiary.ProviderAnthropic {
		t.Errorf("Designation.Provider = %q, want ProviderAnthropic", d.Provider)
	}
	if d.Rating != bestiary.AcceptabilityAdmitted {
		t.Errorf("Designation.Rating = %v, want AcceptabilityAdmitted", d.Rating)
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
