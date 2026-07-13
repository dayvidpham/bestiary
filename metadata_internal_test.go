package bestiary

import "testing"

// TestDetectModelStatus exercises the ingest path for the api.json status field:
// an empty/whitespace token is StatusNone with no raw; a recognized token
// (case-insensitive) is its constant with no raw; and an unknown-but-present
// token is StatusOther with the verbatim raw preserved (never dropped).
func TestDetectModelStatus(t *testing.T) {
	cases := []struct {
		in      string
		want    ModelStatus
		wantRaw string
	}{
		{"", StatusNone, ""},
		{"   ", StatusNone, ""},
		{"alpha", StatusAlpha, ""},
		{"BETA", StatusBeta, ""}, // case-insensitive
		{" deprecated ", StatusDeprecated, ""},
		{"experimental", StatusOther, "experimental"}, // unknown → Other + verbatim raw
		{"other", StatusOther, "other"},               // upstream literal "other" is not a known status
	}
	for _, c := range cases {
		got, raw := detectModelStatus(c.in)
		if got != c.want {
			t.Errorf("detectModelStatus(%q) status = %v, want %v", c.in, got, c.want)
		}
		if raw != c.wantRaw {
			t.Errorf("detectModelStatus(%q) raw = %q, want %q", c.in, raw, c.wantRaw)
		}
	}
}

// TestDetectLinkType exercises the ingest path for a link type tag: recognized
// tokens map to their constant with no raw; the empty token and an unknown token
// both map to the LinkOther fail-safe (raw non-empty only when the token was
// present but unrecognized).
func TestDetectLinkType(t *testing.T) {
	cases := []struct {
		in      string
		want    LinkType
		wantRaw string
	}{
		{"", LinkOther, ""},
		{"announcement", LinkAnnouncement, ""},
		{"MODEL_CARD", LinkModelCard, ""}, // case-insensitive
		{"weights", LinkWeights, ""},
		{"forum", LinkOther, "forum"}, // unknown → Other + verbatim raw
		{"other", LinkOther, "other"},
	}
	for _, c := range cases {
		got, raw := detectLinkType(c.in)
		if got != c.want {
			t.Errorf("detectLinkType(%q) type = %v, want %v", c.in, got, c.want)
		}
		if raw != c.wantRaw {
			t.Errorf("detectLinkType(%q) raw = %q, want %q", c.in, raw, c.wantRaw)
		}
	}
}

// TestDetectReasoningOptionKind exercises the ingest path for a reasoning-option
// kind tag: recognized tokens map to their constant with no raw; the empty token
// and an unknown token both map to the ReasoningOptionOther fail-safe (raw
// non-empty only when the token was present but unrecognized).
func TestDetectReasoningOptionKind(t *testing.T) {
	cases := []struct {
		in      string
		want    ReasoningOptionKind
		wantRaw string
	}{
		{"", ReasoningOptionOther, ""},
		{"toggle", ReasoningToggle, ""},
		{"EFFORT", ReasoningEffort, ""}, // case-insensitive
		{"budget_tokens", ReasoningBudgetTokens, ""},
		{"custom", ReasoningOptionOther, "custom"}, // unknown → Other + verbatim raw
		{"other", ReasoningOptionOther, "other"},
	}
	for _, c := range cases {
		got, raw := detectReasoningOptionKind(c.in)
		if got != c.want {
			t.Errorf("detectReasoningOptionKind(%q) kind = %v, want %v", c.in, got, c.want)
		}
		if raw != c.wantRaw {
			t.Errorf("detectReasoningOptionKind(%q) raw = %q, want %q", c.in, raw, c.wantRaw)
		}
	}
}
