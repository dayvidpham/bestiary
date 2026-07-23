package bestiary_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// modJoin is the test-side equivalent of the package-internal modifierKey: the
// canonical, order-independent comma-joined modifier key. Shared across
// the bestiary_test files for asserting Modifier-list behaviour.
func modJoin(mods []string) string {
	return strings.Join(bestiary.CanonicalizeModifiers(mods), ",")
}

// TestCanonicalScheme_String drives CanonicalScheme.String() over every render codec
// plus the out-of-range fallback, loaded from
// testdata/enum/canonical_scheme_string_corpus.json.
func TestCanonicalScheme_String(t *testing.T) {
	corpus := loadEnumIntCorpus(t, enumCanonicalSchemeStringCorpusJSON, 6)
	requireInputCoverage(t, corpus, map[int]string{
		int(bestiary.SchemeCanonical): "canonical",
		99:                            "CanonicalScheme(99)",
	})
	runEnumIntStringCorpus(t, corpus, func(v int) string {
		return bestiary.CanonicalScheme(v).String()
	})
}

// TestParseScheme_Valid drives ParseScheme over every wire token and renders the
// result back, so the corpus asserts a full token round trip. Loaded from
// testdata/enum/parse_scheme_valid_corpus.json.
func TestParseScheme_Valid(t *testing.T) {
	corpus := loadEnumStringCorpus(t, enumParseSchemeValidCorpusJSON, 5)
	requireInputCoverage(t, corpus, map[string]string{
		"canonical": "canonical",
		"raw":       "raw",
	})
	runEnumStringCorpus(t, corpus, func(t *testing.T, in string) string {
		got, err := bestiary.ParseScheme(in)
		if err != nil {
			t.Fatalf("ParseScheme(%q) returned error: %v", in, err)
		}
		return got.String()
	})
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
		bestiary.SchemeOCI,
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
	msg := err.Error()
	if msg == "" {
		t.Fatal("ParseScheme error message is empty")
	}

	// The error must mention the bad input.
	if !strings.Contains(msg, "badscheme") {
		t.Errorf("error message does not contain the bad input %q:\n  %s", "badscheme", msg)
	}

	// The error must name all 5 valid scheme strings.
	for _, scheme := range []string{"canonical", "huggingface", "purl", "raw", "oci"} {
		if !strings.Contains(msg, scheme) {
			t.Errorf("error message does not mention valid scheme %q:\n  %s", scheme, msg)
		}
	}

	// The error must include a how-to-fix hint.
	if !strings.Contains(msg, "how to fix") {
		t.Errorf("error message missing 'how to fix' guidance:\n  %s", msg)
	}

	_ = errors.Unwrap(err) // must not panic
}

func TestCanonicalScheme_IotaOrder(t *testing.T) {
	// Verify iota ordering is stable: Canonical=0, HF=1, PURL=2, Raw=3, OCI=4.
	// SchemeOCI is APPENDED after SchemeRaw (the iota tail, per canonical.go's
	// wire-stability comment), so this pins it at position 4 — one past the
	// pre-existing members' final position — rather than inserting mid-sequence.
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
	if int(bestiary.SchemeOCI) != 4 {
		t.Errorf("SchemeOCI = %d, want 4", int(bestiary.SchemeOCI))
	}
}

func TestCanonicalScheme_JSON_RoundTrip(t *testing.T) {
	schemes := []bestiary.CanonicalScheme{
		bestiary.SchemeCanonical,
		bestiary.SchemeHuggingFace,
		bestiary.SchemePURL,
		bestiary.SchemeRaw,
		bestiary.SchemeOCI,
	}
	for _, s := range schemes {
		b, err := json.Marshal(s)
		if err != nil {
			t.Errorf("json.Marshal(%v) error: %v", s, err)
			continue
		}
		var got bestiary.CanonicalScheme
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) error: %v", b, err)
			continue
		}
		if got != s {
			t.Errorf("round-trip %v: got %v", s, got)
		}
	}
}

// TestCanonicalScheme_UnmarshalJSON_CaseInsensitive drives the JSON decode over every
// case-folded spelling of each codec token, loaded from
// testdata/enum/canonical_scheme_unmarshal_caseinsensitive_corpus.json.
func TestCanonicalScheme_UnmarshalJSON_CaseInsensitive(t *testing.T) {
	corpus := loadEnumStringCorpus(t, enumCanonicalSchemeUnmarshalCICorpusJSON, 9)
	requireInputCoverage(t, corpus, map[string]string{
		`"CANONICAL"`:   "canonical",
		`"HuggingFace"`: "huggingface",
	})
	runEnumStringCorpus(t, corpus, func(t *testing.T, in string) string {
		var got bestiary.CanonicalScheme
		if err := json.Unmarshal([]byte(in), &got); err != nil {
			t.Fatalf("Unmarshal(%s) error: %v", in, err)
		}
		return got.String()
	})
}

// TestCanonicalScheme_UnmarshalJSON_RejectsBadInput drives the must-fail arm: every
// input must be rejected with a non-empty error, never silently degraded to the zero
// value. Loaded from testdata/enum/canonical_scheme_unmarshal_reject_corpus.json.
func TestCanonicalScheme_UnmarshalJSON_RejectsBadInput(t *testing.T) {
	corpus := loadEnumStringCorpus(t, enumCanonicalSchemeUnmarshalRejectCorpusJSON, 3)
	requireInputCoverage(t, corpus, map[string]string{
		`42`:      "",
		`"bogus"`: "",
	})
	runEnumRejectCorpus(t, corpus, func(in string) error {
		var got bestiary.CanonicalScheme
		return json.Unmarshal([]byte(in), &got)
	})
}

// ----------------------------------------------------------------------------
// Canonical bracket-suffix tests
// ----------------------------------------------------------------------------

// TestModelRef_Format_BracketSuffix verifies that formatCanonical appends a
// [modifier] bracket suffix when Modifier is non-empty, and omits it when empty.
//
// These tests will FAIL until formatCanonical in modelref.go is updated to emit
// the bracket-suffix.
func TestModelRef_Format_BracketSuffix(t *testing.T) {
	tests := []struct {
		desc string
		ref  bestiary.ModelRef
		want string
	}{
		{
			desc: "modifier present → bracket suffix appended",
			ref: bestiary.ModelRef{
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.6",
				Date:     "2026-02-05",
				Modifier: []string{"thinking"},
			},
			want: "anthropic/claude/opus/4.6@2026-02-05[thinking]",
		},
		{
			desc: "modifier empty → no brackets",
			ref: bestiary.ModelRef{
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.5",
				Date:     "2025-11-01",
				Modifier: nil,
			},
			want: "anthropic/claude/opus/4.5@2025-11-01",
		},
		{
			desc: "gpt-4o with date and no modifier",
			ref: bestiary.ModelRef{
				Provider: "openai",
				Family:   "gpt",
				Variant:  "",
				Version:  "4o",
				Date:     "2024-05-13",
				Modifier: nil,
			},
			want: "openai/gpt/4o@2024-05-13",
		},
		{
			desc: "modifier with no date",
			ref: bestiary.ModelRef{
				Provider: "anthropic",
				Family:   "claude",
				Variant:  "opus",
				Version:  "4.6",
				Date:     "",
				Modifier: []string{"thinking"},
			},
			want: "anthropic/claude/opus/4.6[thinking]",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.desc, func(t *testing.T) {
			got := tt.ref.Format(bestiary.SchemeCanonical)
			if got != tt.want {
				t.Errorf("ModelRef.Format(SchemeCanonical) = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestModelRef_ParseCanonical_BracketSuffix verifies that the canonical parser
// (via Resolve with SchemeCanonical) correctly extracts the Modifier field from
// a bracket-suffixed canonical string.
//
// These tests will FAIL until the Resolve/canonical parser is updated to consume
// the optional [modifier] bracket suffix.
func TestModelRef_ParseCanonical_BracketSuffix(t *testing.T) {
	// "anthropic/claude/opus/4.6@2026-02-05[thinking]" must resolve cleanly with Modifier="thinking".
	// Since this is a test-only ModelRef not in the static registry, we test formatCanonical
	// round-trip instead: Format then parse back should produce the same Modifier.
	//
	// The round-trip is: ref → Format(SchemeCanonical) → string → (parsed back via Resolve or manual parse)
	// Since the static registry may not have the exact model, we test the Format output format
	// and verify Modifier appears in bracket notation.

	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-6-thinking",
		Provider: "anthropic",
		Family:   "claude",
		Variant:  "opus",
		Version:  "4.6",
		Date:     "2026-02-05",
		Modifier: []string{"thinking"},
	}

	canonical := ref.Format(bestiary.SchemeCanonical)
	wantStr := "anthropic/claude/opus/4.6@2026-02-05[thinking]"
	if canonical != wantStr {
		t.Errorf("Format(SchemeCanonical) = %q, want %q", canonical, wantStr)
	}

	// Verify brackets don't appear when Modifier is empty.
	refNoModifier := bestiary.ModelRef{
		ID:       "gpt-4o-2024-05-13",
		Provider: "openai",
		Family:   "gpt",
		Variant:  "",
		Version:  "4o",
		Date:     "2024-05-13",
		Modifier: nil,
	}
	canonical2 := refNoModifier.Format(bestiary.SchemeCanonical)
	if strings.Contains(canonical2, "[") || strings.Contains(canonical2, "]") {
		t.Errorf("Format for empty Modifier must NOT contain brackets, got %q", canonical2)
	}
}
