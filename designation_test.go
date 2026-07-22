package bestiary_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestAcceptabilityRating_String drives AcceptabilityRating.String() over every
// ISO-1087 member plus the out-of-range fallback, loaded from
// testdata/enum/acceptability_string_corpus.json.
func TestAcceptabilityRating_String(t *testing.T) {
	corpus := loadEnumIntCorpus(t, enumAcceptabilityStringCorpusJSON, 4)
	requireInputCoverage(t, corpus, map[int]string{
		int(bestiary.AcceptabilityPreferred): "preferred",
		99:                                   "AcceptabilityRating(99)",
	})
	runEnumIntStringCorpus(t, corpus, func(v int) string {
		return bestiary.AcceptabilityRating(v).String()
	})
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

// TestAcceptabilityRating_UnmarshalJSON_CaseInsensitive drives the JSON decode over
// every case-folded spelling of each wire token, loaded from
// testdata/enum/acceptability_unmarshal_caseinsensitive_corpus.json.
func TestAcceptabilityRating_UnmarshalJSON_CaseInsensitive(t *testing.T) {
	corpus := loadEnumStringCorpus(t, enumAcceptabilityUnmarshalCICorpusJSON, 6)
	requireInputCoverage(t, corpus, map[string]string{
		`"ADMITTED"`:   "admitted",
		`"Deprecated"`: "deprecated",
	})
	runEnumStringCorpus(t, corpus, func(t *testing.T, in string) string {
		var got bestiary.AcceptabilityRating
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Fatalf("Unmarshal(%s) error: %v", in, err)
		}
		return got.String()
	})
}

// TestAcceptabilityRating_UnmarshalJSON_RejectsBadInput drives the must-fail arm:
// every input must be rejected with a non-empty error, never silently degraded to the
// zero value. Loaded from testdata/enum/acceptability_unmarshal_reject_corpus.json.
func TestAcceptabilityRating_UnmarshalJSON_RejectsBadInput(t *testing.T) {
	corpus := loadEnumStringCorpus(t, enumAcceptabilityUnmarshalRejectCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[string]string{
		`null`:    "",
		`"bogus"`: "",
	})
	runEnumRejectCorpus(t, corpus, func(in string) error {
		var got bestiary.AcceptabilityRating
		return json.Unmarshal([]byte(in), &got)
	})
}
