package bestiary

import (
	"testing"
)

// --------------------------------------------------------------------------
// isFourDigitDateToken direct unit test (internal)
// --------------------------------------------------------------------------

// TestIsYYMMDateToken verifies the bare-4-digit-date guard (a generalization of
// the original YYMM guard): ANY 4-digit all-numeric token must return true
// (rejected as a date/release-id), not just the YYMM century range (19xx–29xx).
//
// isFourDigitDateToken is unexported; this test lives in the internal package to call
// it directly. Effect-level coverage is in TestIsYYMMDateToken_Parity (parse_test.go).
func TestIsYYMMDateToken(t *testing.T) {
	t.Parallel()

	corpus := loadInternalCorpus[string, bool](t, internalIsYYMMDateTokenCorpusJSON, 19)
	internalRequireInputCoverage(t, corpus, map[string]bool{
		"2603": true,
		"0528": true,
		"1234": true,
		"3000": true,
		"4o":   false,
		"":     false,
	})
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			if got := isFourDigitDateToken(c.Input); got != c.Expected {
				t.Errorf("isFourDigitDateToken(%q) = %v, want %v", c.Input, got, c.Expected)
			}
		})
	}
}

// --------------------------------------------------------------------------
// Parity: detectVersionDigitsInID ⟺ ExtractVersionBetweenFamilyAndVariant
// --------------------------------------------------------------------------

// TestExtractVersionBetweenFamilyAndVariant_Parity enforces the parity
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
		desc         string
		id           ModelID
		family       Family
		variant      string
		wantDetector bool // detectVersionDigitsInID expected result
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
						"  parity requires: detector fires IFF extractor returns non-empty version or residual",
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

// assertLengthThenLexOrder verifies that slice s is sorted by descending length,
// then ascending lexicographic on equal-length runs — the TOTAL order the two
// initParseData sorts (suffixes, modifiers) commit to. It pins the exact
// tie-break: for any adjacent pair of equal length, the earlier element must be
// lexicographically smaller. This is the invariant that makes greedy suffix /
// modifier matching deterministic BY CONSTRUCTION rather than by the sort
// implementation's incidental handling of equal-length elements.
func assertLengthThenLexOrder(t *testing.T, label string, s []string) {
	t.Helper()
	for i := 1; i < len(s); i++ {
		prev, cur := s[i-1], s[i]
		if len(prev) < len(cur) {
			t.Fatalf(
				"%s not length-descending at index %d: %q (len %d) precedes %q (len %d)\n"+
					"  What: the load-time sort left a shorter element before a longer one\n"+
					"  Why: greedy longest-first matching requires descending length\n"+
					"  How to fix: restore the length-descending comparator in initParseData",
				label, i, prev, len(prev), cur, len(cur),
			)
		}
		if len(prev) == len(cur) && prev >= cur {
			t.Fatalf(
				"%s tie-break not lexicographic at index %d: %q is not < %q (both len %d)\n"+
					"  What: two equal-length elements are not in ascending lexicographic order\n"+
					"  Why: an equal-length tie must resolve to a FIXED order (lexicographic) so the\n"+
					"       decomposition never depends on the sort algorithm's incidental tie-handling\n"+
					"  How to fix: keep the total-order comparator (length desc, then s[i] < s[j]) in initParseData",
				label, i, prev, cur, len(prev),
			)
		}
	}
}

// TestParseData_SortsAreTotalOrdered pins the tie-break of the two load-time
// sorts (variant suffixes and modifiers). A length-only comparator is not a total
// order: equal-length entries are left in a sort-implementation-defined order that
// is deterministic for a given Go build but is a latent tie-break the greedy
// matchers inherit. The comparators now break ties lexicographically; this test
// asserts the resulting slices honor (length desc, then lex asc) so a regression
// back to a length-only comparator — or any change that reintroduces an
// order-unstable tie — fails here.
func TestParseData_SortsAreTotalOrdered(t *testing.T) {
	t.Parallel()

	pd, err := loadParseData()
	if err != nil {
		t.Fatalf("loadParseData: %v", err)
	}
	assertLengthThenLexOrder(t, "pd.suffixes", pd.suffixes)
	assertLengthThenLexOrder(t, "pd.modifiers", pd.modifiers)
}

// TestMuseSpark_DecompositionStable pins the canonical decomposition of the
// muse-spark-1.1 model across the ID/raw-family forms it appears under in the July
// catalog (dot-form ID, namespaced ID, dash-form ID, and the empty-raw path). The
// stable tuple is (family=muse, variant=spark, version=1.1, no modifier). This is
// the outcome that a prior codegen (0d25c87) captured in a non-canonical
// undecomposed form (variant/version empty); the parse pipeline is deterministic
// on this input (verified across thousands of fresh-process runs), and this test
// guards the committed stable form at the unit level so any regression that
// re-empties the variant/version is caught without a full regen.
func TestMuseSpark_DecompositionStable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc string
		raw  Family
		id   ModelID
		prov Provider
	}{
		{"raw=muse dot-form id", "muse", "muse-spark-1.1", "abacus"},
		{"raw=muse namespaced id", "muse", "meta/muse-spark-1.1", "vercel"},
		{"raw=muse dash-form id", "muse", "muse-spark-1-1", "empiriolabs"},
		{"empty-raw dot-form id", "", "muse-spark-1.1", "abacus"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			fam, variant, version, modifier, _ := ParseFamilyDetailed(tc.raw, tc.id, tc.prov)
			if fam != "muse" || variant != "spark" || version != "1.1" || len(modifier) != 0 {
				t.Errorf(
					"ParseFamilyDetailed(raw=%q, id=%q, prov=%q) = (family=%q, variant=%q, version=%q, modifier=%v)\n"+
						"  want (family=\"muse\", variant=\"spark\", version=\"1.1\", modifier=[])\n"+
						"  What: muse-spark-1.1 must decompose to its canonical (muse/spark/1.1) tuple\n"+
						"  Why: an undecomposed (muse, \"\", \"\") form is the non-canonical variant a prior\n"+
						"       stale regen captured; the committed catalog carries the decomposed form\n"+
						"  How to fix: do not regress the ID-driven variant/version recovery for this family",
					tc.raw, tc.id, tc.prov, fam, variant, version, modifier,
				)
			}
		})
	}
}
