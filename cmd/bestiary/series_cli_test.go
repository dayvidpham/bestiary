package main

import (
	"encoding/json"
	"errors"
	"flag"
	"reflect"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// runSeriesJSON drives `series [selector] --output=json` end-to-end and decodes
// stdout into v. It fails the test on a run error or a decode error, so each test
// below asserts on content only.
func runSeriesJSON(t *testing.T, v any, args ...string) {
	t.Helper()
	full := append([]string{"series", "--output=json"}, args...)
	var runErr error
	out := captureStdout(t, func() { runErr = run(full) })
	if runErr != nil {
		t.Fatalf("run %v returned error: %v", full, runErr)
	}
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("run %v produced undecodable JSON: %v\noutput:\n%s", full, err, out)
	}
}

// TestRun_Series_ListTable drives the bare `series --output=table` listing and
// asserts the census header, the column header, and a known line render.
func TestRun_Series_ListTable(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() { runErr = run([]string{"series", "--output=table"}) })
	if runErr != nil {
		t.Fatalf("run series returned error: %v", runErr)
	}
	// Census header tracks the library-side Series pin (TestSeriesAll_CensusExact),
	// which moved 419 → 418 (o-series dual-identity) then 418 → 411 (dot-lost version
	// repair + 1t param-size routing folding dotless/dash qwen lines and re-keying
	// ling@1t/ring@1t to size-only #1t entities).
	if want := "Series (417):"; /* 411 -> 419: 2026-07-23 refresh; 419 -> 417: v0.2.8 slice, deepseek gen-1/gen-2 lines retire via the dot-lost merges; 417 -> 415, the global free demotion empties the deepseek-flash and minimax-m3 bare lines; 415 -> 417, the ling/inkling/kling collision split adds the bare inkling and kling lines while kling@2.6 replaces the phantom kling-v2@6 versioned line one-for-one */ !strings.Contains(out, want) {
		t.Errorf("listing missing the census header %q; got first line:\n%s", want, firstLine(out))
	}
	for _, want := range []string{"SERIES", "FAMILY", "GENERATION", "RELEASES", "ENTITIES"} {
		if !strings.Contains(out, want) {
			t.Errorf("listing missing column header %q", want)
		}
	}
	if !strings.Contains(out, "llama-4") {
		t.Error("listing missing the llama-4 line")
	}
	// An un-versioned line renders "-" rather than blank space.
	if !strings.Contains(out, " - ") {
		t.Error("listing never renders the '-' placeholder for an un-versioned generation")
	}
}

// TestRun_Series_ListJSON asserts the listing's JSON shape and that its census and
// per-line counts agree with the library API (the CLI is a view, never its own
// source of truth).
func TestRun_Series_ListJSON(t *testing.T) {
	var rows []seriesSummary
	runSeriesJSON(t, &rows)

	if got, want := len(rows), len(bestiary.SeriesAll()); got != want {
		t.Errorf("series listing has %d rows, want %d (SeriesAll census)", got, want)
	}
	var llama4 *seriesSummary
	for i := range rows {
		if rows[i].Series == "llama-4" {
			llama4 = &rows[i]
			break
		}
	}
	if llama4 == nil {
		t.Fatal("series listing has no llama-4 row")
	}
	if llama4.Family != "llama" || llama4.Generation != "4" {
		t.Errorf("llama-4 row = family %q generation %q, want llama / 4", llama4.Family, llama4.Generation)
	}
	// bare + maverick + scout, each with two sized entities.
	if llama4.Releases != 3 || llama4.Entities != 6 {
		t.Errorf("llama-4 row = %d releases / %d entities, want 3 / 6", llama4.Releases, llama4.Entities)
	}
	// Ordering is SeriesAll's: ascending by family, then generation.
	for i := 1; i < len(rows); i++ {
		prev, cur := rows[i-1], rows[i]
		if prev.Family > cur.Family || (prev.Family == cur.Family && prev.Generation >= cur.Generation) {
			t.Fatalf("listing not in SeriesAll order at %d: %+v then %+v", i, prev, cur)
		}
	}
}

// TestRun_Series_DetailTable drives `series llama-4 --output=table` — the ratified
// worked example — and asserts both named releases and the bare-line label render
// with their entity keys.
func TestRun_Series_DetailTable(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() { runErr = run([]string{"series", "--output=table", "llama-4"}) })
	if runErr != nil {
		t.Fatalf("run series llama-4 returned error: %v", runErr)
	}
	for _, want := range []string{
		"Series llama-4 (3 releases):",
		"(bare line)",
		"maverick",
		"scout",
		"llama/maverick@4#17b-128e",
		"llama/maverick@4#17b-128e{instruct}",
		"llama/scout@4#17b-16e{instruct}",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail output missing %q; got:\n%s", want, out)
		}
	}
}

// TestRun_Series_DetailJSON asserts the detail JSON shape for the same line: three
// releases in ReleasesOf order, each carrying its member entity keys.
func TestRun_Series_DetailJSON(t *testing.T) {
	var details []seriesDetail
	runSeriesJSON(t, &details, "llama-4")

	if len(details) != 1 {
		t.Fatalf("series llama-4 returned %d lines, want 1", len(details))
	}
	d := details[0]
	if d.Series != "llama-4" || d.Family != "llama" || d.Generation != "4" {
		t.Errorf("detail = %+v, want llama-4 / llama / 4", d)
	}
	wantNames := []string{"", "maverick", "scout"}
	if len(d.Releases) != len(wantNames) {
		t.Fatalf("detail has %d releases, want %d", len(d.Releases), len(wantNames))
	}
	for i, want := range wantNames {
		if d.Releases[i].Name != want {
			t.Errorf("release %d name = %q, want %q", i, d.Releases[i].Name, want)
		}
	}
	// The bare release renders its display form even with an empty Name.
	if d.Releases[0].Release != "llama-4" {
		t.Errorf("bare release rendering = %q, want %q", d.Releases[0].Release, "llama-4")
	}
	if got, want := d.Releases[1].Entities, []string{
		"llama/maverick@4#17b-128e",
		"llama/maverick@4#17b-128e{instruct}",
	}; !equalStringSlices(got, want) {
		t.Errorf("maverick entities = %v, want %v", got, want)
	}
}

// TestRun_Series_FamilySelectorUnion pins the union selector reading: a family name
// selects EVERY generation of that family, including the un-versioned line that
// shares the family's rendering — so "gemma" can never hide a gemma line.
func TestRun_Series_FamilySelectorUnion(t *testing.T) {
	var details []seriesDetail
	runSeriesJSON(t, &details, "gemma")

	got := map[string]bool{}
	for _, d := range details {
		got[d.Series] = true
		if d.Family != "gemma" {
			t.Errorf("family selector returned a non-gemma line: %+v", d)
		}
	}
	for _, want := range []string{"gemma", "gemma-2", "gemma-3", "gemma-4"} {
		if !got[want] {
			t.Errorf("family selector 'gemma' missing line %q (got %v)", want, keysOf(got))
		}
	}
}

// TestRun_Series_SelectorIsCaseFolded verifies the selector matches case-insensitively.
func TestRun_Series_SelectorIsCaseFolded(t *testing.T) {
	var upper, lower []seriesDetail
	runSeriesJSON(t, &upper, "LLAMA-4")
	runSeriesJSON(t, &lower, "llama-4")
	if len(upper) != 1 || len(lower) != 1 || upper[0].Series != lower[0].Series {
		t.Errorf("case-folded selector disagreed: %+v vs %+v", upper, lower)
	}
}

// TestRun_Series_GeminiNormalizationVisible is the end-to-end fence on the ruled
// generation fold, now realized at ENTITY IDENTITY level: the CLI shows ONE gemini-3.0
// line whose flash release holds the SINGLE merged gemini/flash@3.0 entity (the "@3" and
// "@3.0" spellings merged into it — they are no longer two entities), and gemini-3 is not
// a line of its own. It also draws the distinction the version selectors introduce —
// "gemini-3" is not a LINE but is a valid major SELECTOR, and those are different things.
func TestRun_Series_GeminiNormalizationVisible(t *testing.T) {
	var details []seriesDetail
	runSeriesJSON(t, &details, "gemini-3.0")
	if len(details) != 1 {
		t.Fatalf("series gemini-3.0 returned %d lines, want 1", len(details))
	}
	var flash []string
	for _, r := range details[0].Releases {
		if r.Name == "flash" {
			flash = r.Entities
		}
	}
	if want := []string{"gemini/flash@3.0"}; !equalStringSlices(flash, want) {
		t.Errorf("gemini-3.0/flash entities = %v, want %v (the two spellings merged into one entity)", flash, want)
	}
	// The un-normalized rendering is still not a LINE — the fold is structural and
	// SeriesAll never lists "gemini-3"...
	for _, s := range bestiary.SeriesAll() {
		if s.Family == "gemini" && s.Generation == "3" {
			t.Error("SeriesAll() exposes the folded line gemini-3; the N -> N.0 fold must still apply")
		}
	}
	// ...but it IS a valid MAJOR selector, which is a selection path rather than a
	// line: it unions every gemini 3.x line, the folded gemini-3.0 among them.
	var union []seriesDetail
	runSeriesJSON(t, &union, "gemini-3")
	got := make([]string, 0, len(union))
	for _, d := range union {
		got = append(got, d.Series)
	}
	if want := []string{"gemini-3.0", "gemini-3.1", "gemini-3.5", "gemini-3.6"}; !equalStringSlices(got, want) { // +3.6: new upstream line, 2026-07-23 refresh
		t.Errorf("series gemini-3 = %v, want the major union %v", got, want)
	}
}

// seriesNamesFor runs `series <args…> --output=json` and returns the rendered line
// names of the detail view, in output order.
func seriesNamesFor(t *testing.T, args ...string) []string {
	t.Helper()
	var details []seriesDetail
	runSeriesJSON(t, &details, args...)
	out := make([]string, 0, len(details))
	for _, d := range details {
		out = append(out, d.Series)
	}
	return out
}

// TestRun_Series_SpecificityLadder is the ruled selector semantics end to end: each
// rung of family → family-MAJOR → family-MAJOR.MINOR is strictly narrower than the
// one above, and the major rung returns every 4.x line as a UNION rather than
// re-grouping anything.
//
// This is the user's ask in its own words — asking for `claude-4` returns all of the
// 4.x series, while the more specific `4.8` or `4.0` returns the narrower selection.
func TestRun_Series_SpecificityLadder(t *testing.T) {
	family := seriesNamesFor(t, "claude")
	major := seriesNamesFor(t, "claude-4")
	minor := seriesNamesFor(t, "claude-4.0")

	// The per-rung MEMBERSHIP is pinned as authored data in the resolution corpus
	// (TestRun_Series_SelectorResolutionCorpus). What is asserted here is the
	// structural PROPERTY between rungs, which is computed from those results rather
	// than authored, and so stays inline per TESTING.md.

	// Strictly narrowing, and every rung a subset of the one above — the property that
	// makes the ladder a ladder rather than three unrelated lookups.
	if !(len(family) > len(major) && len(major) > len(minor)) {
		t.Errorf("rungs are not strictly narrowing: family %d, major %d, minor %d",
			len(family), len(major), len(minor))
	}
	inFamily := map[string]bool{}
	for _, s := range family {
		inFamily[s] = true
	}
	inMajor := map[string]bool{}
	for _, s := range major {
		inMajor[s] = true
		if !inFamily[s] {
			t.Errorf("major rung returned %q, which the family rung does not", s)
		}
	}
	for _, s := range minor {
		if !inMajor[s] {
			t.Errorf("minor rung returned %q, which the major rung does not", s)
		}
	}

	// The major rung selects; it does NOT re-group. Every returned line keeps its own
	// exact generation, and the entities stay under the line they were already in.
	var details []seriesDetail
	runSeriesJSON(t, &details, "claude-4")
	for _, d := range details {
		if d.Family != "claude" || d.Generation == "4" {
			t.Errorf("major union altered a line's identity: %+v (generations must stay exact)", d)
		}
	}
}

// TestRun_Series_SelectorResolutionCorpus is the end-to-end fence over the whole
// selector surface: the specificity ladder, the canonical grammar readings (version,
// release-level cut, provider qualification), --version, --input-format, and the
// disagreement errors. Each case drives run() with the argv a user would type.
//
// The rows are AUTHORED data facts — a selector and the exact lines it must return —
// so they live in a JSON corpus under the three-guard discipline (exact count,
// value coverage, non-vacuity) rather than inline. See TESTING.md.
func TestRun_Series_SelectorResolutionCorpus(t *testing.T) {
	corpus := loadSeriesCorpus[seriesResolutionInput, seriesResolutionExpected](
		t, seriesSelectorResolutionCorpusJSON, 28)

	// Value coverage: a count-preserving swap that dropped one of the ruled examples
	// and added a filler would survive the exact-count guard, so the load-bearing
	// selectors are asserted present by value.
	requireSeriesCorpusCoverage(t, corpus, []seriesResolutionInput{
		{Selector: "claude"},
		{Selector: "claude-4"},
		{Selector: "claude", Version: "4"},
		{Selector: "claude-4.0"},
		{Selector: "claude@4"},
		{Selector: "claude/opus"},
		{Selector: "anthropic/claude@4"},
		{Selector: "claude-sonnet-4-5-20250929", InputFormat: "models.dev"},
	})

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() { runErr = run(c.Input.args()) })

			if c.Classification == testcase.MustFail {
				if runErr == nil {
					t.Fatalf("run %v succeeded; the case requires a rejection. stdout:\n%s", c.Input.args(), out)
				}
				for _, want := range c.Expected.ErrorContains {
					if !strings.Contains(runErr.Error(), want) {
						t.Errorf("error %q does not mention %q", runErr, want)
					}
				}
				if strings.TrimSpace(out) != "" {
					t.Errorf("run %v wrote a view before failing:\n%s", c.Input.args(), out)
				}
				return
			}

			if runErr != nil {
				t.Fatalf("run %v returned error: %v", c.Input.args(), runErr)
			}
			var details []seriesDetail
			if err := json.Unmarshal([]byte(out), &details); err != nil {
				t.Fatalf("run %v produced undecodable JSON: %v\noutput:\n%s", c.Input.args(), err, out)
			}
			got := make([]string, 0, len(details))
			for _, d := range details {
				got = append(got, d.Series)
			}
			if !equalStringSlices(got, c.Expected.Series) {
				t.Errorf("run %v returned lines %v, want %v", c.Input.args(), got, c.Expected.Series)
			}
			// A release-level cut additionally pins the releases every returned line
			// must show — without this, `claude/opus` would pass by returning the right
			// lines with all their releases intact.
			if len(c.Expected.Releases) > 0 {
				for _, d := range details {
					names := make([]string, 0, len(d.Releases))
					for _, r := range d.Releases {
						names = append(names, r.Name)
					}
					if !equalStringSlices(names, c.Expected.Releases) {
						t.Errorf("line %q shows releases %v, want %v (the release cut did not apply)",
							d.Series, names, c.Expected.Releases)
					}
				}
			}
		})
	}
}

// requireSeriesCorpusCoverage asserts each probe input is still present in the
// corpus by value — the guard an exact case count cannot provide.
func requireSeriesCorpusCoverage(t *testing.T, corpus testcase.Corpus[seriesResolutionInput, seriesResolutionExpected], probes []seriesResolutionInput) {
	t.Helper()
	for _, p := range probes {
		found := false
		for _, c := range corpus.Cases {
			if c.Input == p {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("value coverage lost: no corpus case for input %+v", p)
		}
	}
}

// TestRun_Series_VersionFlagEquivalence pins `--version` as sugar for the same
// selection. Equivalence is asserted on the FULL decoded output, not just the line
// names, so the flag cannot diverge in the detail it renders either.
//
// The pairs are COMPUTED against each other rather than against an authored expected
// value — the assertion is "these two invocations agree", which has no fixed table —
// so this stays inline while the per-selector membership lives in the corpus.
func TestRun_Series_VersionFlagEquivalence(t *testing.T) {
	cases := []struct{ selector, family, version string }{
		{"claude-4", "claude", "4"},           // major union
		{"claude-4.8", "claude", "4.8"},       // one line
		{"llama-4", "llama", "4"},             // a bare-integer line with no dotted siblings
		{"mistral-0", "mistral", "0"},         // sub-1.0 lines union under 0 like any other
		{"claude/opus@4", "claude/opus", "4"}, // the canonical grammar, version supplied by flag
	}
	for _, tc := range cases {
		var viaSelector, viaFlag []seriesDetail
		runSeriesJSON(t, &viaSelector, tc.selector)
		runSeriesJSON(t, &viaFlag, tc.family, "--version", tc.version)
		if !reflect.DeepEqual(viaSelector, viaFlag) {
			t.Errorf("`series %s` and `series %s --version %s` disagree:\n  selector: %+v\n      flag: %+v",
				tc.selector, tc.family, tc.version, viaSelector, viaFlag)
		}
		if len(viaFlag) == 0 {
			t.Errorf("`series %s --version %s` returned nothing; the case is vacuous", tc.family, tc.version)
		}
	}

	// An unknown version is the SAME error class as an unknown selector — a normal
	// negative naming what was asked for, never silence or a panic.
	var runErr error
	captureStdout(t, func() { runErr = run([]string{"series", "--output=json", "claude", "--version", "9"}) })
	var notFound *bestiary.ErrNotFound
	if !errors.As(runErr, &notFound) {
		t.Fatalf("`series claude --version 9` returned %v, want *bestiary.ErrNotFound", runErr)
	}
	if notFound.What != "series" {
		t.Errorf("ErrNotFound = %+v, want what=series", notFound)
	}
}

// TestRun_Series_VersionFlagRequiresFamily is the negative control on the flag's
// scope: --version selects WITHIN a family, so without one it is rejected with an
// actionable error rather than silently ignored or silently widened to the registry.
func TestRun_Series_VersionFlagRequiresFamily(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			"no family",
			[]string{"series", "--output=json", "--version", "4"},
			[]string{"--version", "without a family", "bestiary series claude --version 4"},
		},
		{
			"no value",
			[]string{"series", "--output=json", "--version="},
			[]string{"--version", "no value"},
		},
		{
			"blank value",
			[]string{"series", "--output=json", "--version", "   ", "claude"},
			[]string{"--version", "no value"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() { runErr = run(tc.args) })
			if runErr == nil {
				t.Fatalf("run %v succeeded; it must be rejected. stdout:\n%s", tc.args, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(runErr.Error(), want) {
					t.Errorf("error %q does not mention %q", runErr, want)
				}
			}
			if strings.TrimSpace(out) != "" {
				t.Errorf("run %v wrote a view before failing:\n%s", tc.args, out)
			}
		})
	}
}

// TestRun_Series_StrictMembershipNegatives is the end-to-end negative control on the
// membership rule: a version that matches NO line must be a normal not-found, never
// an accidental match. "ling-1" must not reach ling@1t, which a bare-prefix rule
// would have allowed.
//
// The POSITIVE membership rows (which lines each union returns) are authored data and
// live in the resolution corpus; these three are computed negatives over the built
// catalog, so they stay inline.
func TestRun_Series_StrictMembershipNegatives(t *testing.T) {
	for _, selector := range []string{"ling-1", "ring-1", "text-embedding-0"} {
		var runErr error
		captureStdout(t, func() { runErr = run([]string{"series", "--output=json", selector}) })
		var notFound *bestiary.ErrNotFound
		if !errors.As(runErr, &notFound) {
			t.Errorf("series %s returned %v, want ErrNotFound (the strict rule must not match "+
				"a non-dotted spelling)", selector, runErr)
		}
	}
}

// TestGenerationInMajorUnion unit-tests the strict membership rule directly, over the
// authored corpus of generation/version pairs — including the real catalog spellings
// (p-notation, 1t, leading zeros, dot-lost tokens) a looser rule would mis-admit.
func TestGenerationInMajorUnion(t *testing.T) {
	corpus := loadSeriesCorpus[seriesMembershipInput, bool](t, seriesMajorUnionMembershipCorpusJSON, 21)

	// Value coverage: the load-bearing exclusions must still be present by value.
	// The two second-of-a-kind rows (0.3/0, 35/3) are pinned here too so the 1:1
	// restoration of the pre-migration inline table cannot silently regress again.
	wantPresent := []seriesMembershipInput{
		{Generation: "4.8", Version: "4"},
		{Generation: "42", Version: "4"},
		{Generation: "4p1", Version: "4"},
		{Generation: "1t", Version: "1"},
		{Generation: "001", Version: "1"},
		{Generation: "0.1", Version: "0"},
		{Generation: "0.3", Version: "0"},
		{Generation: "25", Version: "2"},
		{Generation: "35", Version: "3"},
		{Generation: "4.0", Version: "4.8"},
	}
	for _, want := range wantPresent {
		found := false
		for _, c := range corpus.Cases {
			if c.Input == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("value coverage lost: no corpus case for %+v", want)
		}
	}

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := generationInMajorUnion(c.Input.Generation, c.Input.Version)
			if got != c.Expected {
				t.Errorf("generationInMajorUnion(%q, %q) = %v, want %v",
					c.Input.Generation, c.Input.Version, got, c.Expected)
			}
			// The classification and the expected value must agree, so a row cannot
			// claim to be a negative control while asserting membership.
			if wantMember := c.Classification == testcase.MustPass; wantMember != c.Expected {
				t.Errorf("case %q is classified %s but expects %v", c.Name, c.Classification, c.Expected)
			}
		})
	}
}

// TestApplyVersionFlag unit-tests the sugar: --version folds onto the positional to
// produce the candidate spellings, which is what makes the two spellings one query
// rather than two implementations that must be kept in agreement.
func TestApplyVersionFlag(t *testing.T) {
	corpus := loadSeriesCorpus[seriesComposeInput, seriesComposeExpected](t, seriesVersionComposeCorpusJSON, 10)

	sawBothGrammars, sawCanonicalOnly, sawRejection := false, false, false
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			format, err := parseSeriesInputFormat(c.Input.InputFormat)
			if err != nil {
				t.Fatalf("corpus case names an unparseable --input-format %q: %v", c.Input.InputFormat, err)
			}
			got, err := applyVersionFlag(c.Input.Selector, c.Input.Version, format)

			if c.Classification == testcase.MustFail {
				if err == nil {
					t.Fatalf("applyVersionFlag(%q, %q, %v) = %v, want a rejection",
						c.Input.Selector, c.Input.Version, format, got)
				}
				for _, want := range c.Expected.ErrorContains {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not mention %q", err, want)
					}
				}
				sawRejection = true
				return
			}
			if err != nil {
				t.Fatalf("applyVersionFlag(%q, %q, %v) returned error: %v",
					c.Input.Selector, c.Input.Version, format, err)
			}
			if !equalStringSlices(got, c.Expected.Candidates) {
				t.Errorf("applyVersionFlag(%q, %q, %v) = %v, want %v",
					c.Input.Selector, c.Input.Version, format, got, c.Expected.Candidates)
			}
			if len(got) == 2 {
				sawBothGrammars = true
			}
			if format == seriesInputCanonical && len(got) == 1 && c.Input.Version != "" {
				sawCanonicalOnly = true
			}
		})
	}
	// Non-vacuity beyond Validate: the corpus must actually exercise each behaviour
	// the function has, not just the easy arm.
	if !sawBothGrammars || !sawCanonicalOnly || !sawRejection {
		t.Errorf("corpus does not cover every behaviour: bothGrammars=%v canonicalOnly=%v rejection=%v",
			sawBothGrammars, sawCanonicalOnly, sawRejection)
	}
}

// TestRun_Series_UnknownSelector asserts an unmatched selector is an actionable
// ErrNotFound carrying the selector — a normal negative, never a panic or silence.
func TestRun_Series_UnknownSelector(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"series", "--output=table", "definitely-not-a-family"})
	})
	var notFound *bestiary.ErrNotFound
	if !errors.As(runErr, &notFound) {
		t.Fatalf("unknown selector returned %v, want *bestiary.ErrNotFound", runErr)
	}
	if notFound.What != "series" || notFound.Key != "definitely-not-a-family" {
		t.Errorf("ErrNotFound = %+v, want what=series key=definitely-not-a-family", notFound)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("a failed lookup still wrote to stdout:\n%s", out)
	}
}

// TestRun_Series_UnsupportedOutput asserts the entity-command output discipline
// (json/table only) applies to series too.
func TestRun_Series_UnsupportedOutput(t *testing.T) {
	err := run([]string{"series", "--output=yaml"})
	if err == nil {
		t.Fatal("series --output=yaml returned nil error, want an unsupported-format error")
	}
	if !strings.Contains(err.Error(), "unsupported --output") {
		t.Errorf("error = %v, want an unsupported --output message", err)
	}
}

// TestRun_Series_RejectsExplicitDBPath asserts that --db-path is REJECTED rather
// than silently ignored. The flag parses (it is registered on the shared flagset
// every subcommand uses), but series computes from the compiled-in registry and
// never opens the cache, so accepting it would let a caller believe they had
// scoped the view to a synced database. The message must name the cache-aware
// alternative so the user knows where to go.
func TestRun_Series_RejectsExplicitDBPath(t *testing.T) {
	for _, args := range [][]string{
		{"series", "--db-path=/tmp/does-not-exist.db"},
		{"series", "--db-path", "/tmp/does-not-exist.db", "llama-4"},
		{"series", "llama-4", "--db-path=/tmp/does-not-exist.db"}, // flag after the positional
	} {
		var runErr error
		out := captureStdout(t, func() { runErr = run(args) })
		if runErr == nil {
			t.Errorf("run %v returned nil error; --db-path must be rejected, not ignored", args)
			continue
		}
		msg := runErr.Error()
		for _, want := range []string{"--db-path", "does not read the cache", "entities"} {
			if !strings.Contains(msg, want) {
				t.Errorf("run %v error %q missing %q", args, msg, want)
			}
		}
		if strings.TrimSpace(out) != "" {
			t.Errorf("run %v wrote to stdout despite failing:\n%s", args, out)
		}
	}
}

// TestRun_Series_DefaultDBPathIsNotRejected is the other half of the contract: the
// flag is only rejected when EXPLICITLY set. A plain invocation inherits the shared
// flagset's default and must still work — a rejection keyed on the value rather
// than on explicit setting would break every normal call.
func TestRun_Series_DefaultDBPathIsNotRejected(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() { runErr = run([]string{"series", "--output=table", "llama-4"}) })
	if runErr != nil {
		t.Fatalf("plain series invocation returned error: %v", runErr)
	}
	if !strings.Contains(out, "Series llama-4") {
		t.Errorf("plain series invocation produced no output:\n%s", out)
	}
}

// TestFlagWasSet unit-tests the explicit-vs-default distinction the rejection
// keys off.
func TestFlagWasSet(t *testing.T) {
	newFS := func() (*flag.FlagSet, *string) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		v := fs.String("db-path", "", "")
		return fs, v
	}
	fs, _ := newFS()
	if err := fs.Parse([]string{}); err != nil {
		t.Fatal(err)
	}
	if flagWasSet(fs, "db-path") {
		t.Error("flagWasSet = true for an unset flag")
	}
	fs, _ = newFS()
	if err := fs.Parse([]string{"--db-path=/x"}); err != nil {
		t.Fatal(err)
	}
	if !flagWasSet(fs, "db-path") {
		t.Error("flagWasSet = false for an explicitly set flag")
	}
	// Explicitly set to the DEFAULT value still counts as set.
	fs, _ = newFS()
	if err := fs.Parse([]string{"--db-path="}); err != nil {
		t.Fatal(err)
	}
	if !flagWasSet(fs, "db-path") {
		t.Error("flagWasSet = false for a flag explicitly set to its default value")
	}
	if flagWasSet(fs, "no-such-flag") {
		t.Error("flagWasSet = true for an unregistered flag")
	}
}

// TestSelectSeries_UnitReadings unit-tests the selector resolution directly: the
// family, major-union and line readings, the case where a family name equals a bare
// line's rendering, and the dashed-family case that a "split on the last dash" parse
// would get wrong.
func TestSelectSeries_UnitReadings(t *testing.T) {
	// The synthetic line universe the corpus rows are authored against. It is shared
	// SETUP rather than a case table: every row is resolved against this same
	// universe, chosen to carry the three shapes the readings must distinguish — a
	// family name that is also a bare line's rendering (gemma), a bare-integer line
	// with a dotted sibling (gemma-4 / gemma-4.1), and a family whose NAME contains
	// the ladder separator (grok-build).
	all := []bestiary.Series{
		{Family: "gemma"},
		{Family: "gemma", Generation: "4"},
		{Family: "gemma", Generation: "4.1"},
		{Family: "grok-build", Generation: "0.1"},
		{Family: "llama", Generation: "4"},
	}

	corpus := loadSeriesCorpus[string, []string](t, seriesSelectReadingsCorpusJSON, 12)
	for _, want := range []string{"gemma", "gemma-4", "gemma-4.1", "grok-build-0", "gemma-41"} {
		found := false
		for _, c := range corpus.Cases {
			if c.Input == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("value coverage lost: no corpus case for selector %q", want)
		}
	}

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			got := make([]string, 0, 4)
			for _, s := range selectSeries(all, c.Input) {
				got = append(got, s.String())
			}
			if !equalStringSlices(got, c.Expected) {
				t.Errorf("selectSeries(%q) = %v, want %v", c.Input, got, c.Expected)
			}
			// A must-fail row is one that must select NOTHING; a must-pass row must
			// select something. This keeps a negative control from silently becoming
			// a positive one when the expected list is edited.
			if wantEmpty := c.Classification == testcase.MustFail; wantEmpty != (len(got) == 0) {
				t.Errorf("case %q is classified %s but selected %d line(s)", c.Name, c.Classification, len(got))
			}
		})
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
