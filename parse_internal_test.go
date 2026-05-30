package bestiary

import (
	"testing"
)

// --------------------------------------------------------------------------
// R3b (eq7w): isYYMMDateToken direct unit test (internal)
// --------------------------------------------------------------------------

// TestIsYYMMDateToken verifies the YYMM-date guard: 4-digit tokens matching the
// YYMM range (century prefixes 19xx–29xx) must return true, while genuine version
// tokens (4o, 45, 2.5) must return false.
//
// isYYMMDateToken is unexported; this test lives in the internal package to call
// it directly. Effect-level coverage is in TestIsYYMMDateToken_Parity (parse_test.go).
func TestIsYYMMDateToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tok  string
		want bool
	}{
		// True: YYMM range tokens (should be rejected as versions).
		{"2603", true},  // mistral-small-2603
		{"2512", true},  // YYMM dec 2025
		{"2411", true},  // pixtral-style
		{"2401", true},  // mistral-2401
		{"2503", true},  // another YYMM
		// False: genuine version tokens.
		{"45", false},   // single two-digit (not 4-digit)
		{"46", false},   // single two-digit
		{"4o", false},   // alphanumeric (not pure digits)
		{"2", false},    // single digit
		{"35", false},   // two-digit version
		{"100", false},  // three digits
		// False: 4-digit tokens outside YYMM range.
		{"1234", false}, // 12xx — below 19xx threshold
		{"3000", false}, // 30xx — above 29xx threshold
		// False: empty.
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.tok, func(t *testing.T) {
			t.Parallel()
			got := isYYMMDateToken(tc.tok)
			if got != tc.want {
				t.Errorf("isYYMMDateToken(%q) = %v, want %v", tc.tok, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// R1 parity: detectVersionDigitsInID ⟺ ExtractVersionBetweenFamilyAndVariant
// --------------------------------------------------------------------------

// TestExtractVersionBetweenFamilyAndVariant_Parity enforces the URD R1 parity
// contract: detectVersionDigitsInID fires if and only if
// ExtractVersionBetweenFamilyAndVariant returns a non-empty version OR a
// non-empty residual.
//
// This test is the load-bearing enforcer of the invariant stated in
// ExtractVersionBetweenFamilyAndVariant's doc comment. If the extractor is
// modified so that it fires without the detector also firing (or vice versa),
// this test will fail.
//
// Positive cases (detector MUST fire AND extractor MUST return version or residual):
//   - gpt-5-mini: single numeric between family and variant
//   - claude-3-5-haiku-20241022: N-M dot-join
//   - gemini-3-pro-preview: single numeric, variant=pro
//   - nova-2-lite-v1: version=2, residual=[v1]
//   - nemotron-3-super-free: version=3, residual=[super]
//
// Negative cases (detector MUST NOT fire AND extractor MUST return empty):
//   - claude-opus-4-6: version is AFTER family+variant (no digits between)
//   - empty id / empty family
func TestExtractVersionBetweenFamilyAndVariant_Parity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		id            ModelID
		family        Family
		variant       string
		wantDetector  bool // detectVersionDigitsInID expected result
	}{
		// Positive: detector fires, extractor returns non-empty version or residual.
		{
			desc:         "gpt-5-mini → detector fires (single numeric between family and variant)",
			id:           "gpt-5-mini",
			family:       "gpt",
			variant:      "mini",
			wantDetector: true,
		},
		{
			desc:         "claude-3-5-haiku-20241022 → detector fires (N-M between family and variant)",
			id:           "claude-3-5-haiku-20241022",
			family:       "claude",
			variant:      "haiku",
			wantDetector: true,
		},
		{
			desc:         "gemini-3-pro-preview → detector fires (single numeric, variant=pro)",
			id:           "gemini-3-pro-preview",
			family:       "gemini",
			variant:      "pro",
			wantDetector: true,
		},
		{
			desc:         "nova-2-lite-v1 → detector fires (version=2, residual=[v1])",
			id:           "nova-2-lite-v1",
			family:       "nova",
			variant:      "lite",
			wantDetector: true,
		},
		{
			desc:         "nemotron-3-super-free → detector fires (version=3, residual=[super])",
			id:           "nemotron-3-super-free",
			family:       "nemotron",
			variant:      "free",
			wantDetector: true,
		},
		// Negative: no version digits between family and variant.
		{
			desc:         "claude-opus-4-6 → detector does NOT fire (digits come after variant)",
			id:           "claude-opus-4-6",
			family:       "claude",
			variant:      "opus",
			wantDetector: false,
		},
		{
			desc:         "empty id → detector does NOT fire",
			id:           "",
			family:       "claude",
			variant:      "haiku",
			wantDetector: false,
		},
		{
			desc:         "empty family → detector does NOT fire",
			id:           "claude-3-5-haiku-20241022",
			family:       "",
			variant:      "haiku",
			wantDetector: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			gotDetector := detectVersionDigitsInID(tc.id, tc.family, tc.variant)
			version, residual := ExtractVersionBetweenFamilyAndVariant(tc.id, tc.family, tc.variant)
			extractorFired := version != "" || len(residual) > 0

			// Parity check: detector fires IFF extractor fires (version or residual non-empty).
			if gotDetector != extractorFired {
				t.Errorf(
					"parity violation for id=%q family=%q variant=%q:\n"+
						"  detectVersionDigitsInID = %v\n"+
						"  ExtractVersionBetweenFamilyAndVariant fired = %v (version=%q, residual=%v)\n"+
						"  URD R1 requires: detector fires IFF extractor returns non-empty version or residual",
					tc.id, tc.family, tc.variant,
					gotDetector, extractorFired, version, residual,
				)
			}

			// Also verify the expected detector result matches the test table.
			if gotDetector != tc.wantDetector {
				t.Errorf(
					"detectVersionDigitsInID(%q, %q, %q) = %v, want %v",
					tc.id, tc.family, tc.variant, gotDetector, tc.wantDetector,
				)
			}
		})
	}
}
