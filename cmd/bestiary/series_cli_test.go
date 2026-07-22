package main

import (
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
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
	// which moved 422 → 421 when the curated eva and command-a-plus overrides retired
	// two compound-family lines and created one.
	if want := "Series (421):"; !strings.Contains(out, want) {
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
// generation fold: the CLI shows ONE gemini-3.0 line holding both the "@3" and
// "@3.0" spellings' entities, and no gemini-3 line exists to select.
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
	if want := []string{"gemini/flash@3", "gemini/flash@3.0"}; !equalStringSlices(flash, want) {
		t.Errorf("gemini-3.0/flash entities = %v, want %v (both spellings under one line)", flash, want)
	}
	// The un-normalized rendering is NOT selectable.
	var runErr error
	captureStdout(t, func() { runErr = run([]string{"series", "--output=json", "gemini-3"}) })
	var notFound *bestiary.ErrNotFound
	if !errors.As(runErr, &notFound) {
		t.Errorf("selecting the folded line 'gemini-3' returned %v, want ErrNotFound", runErr)
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

// TestSelectSeries_UnitReadings unit-tests the selector resolution directly,
// including the union case where a family name equals a bare line's rendering.
func TestSelectSeries_UnitReadings(t *testing.T) {
	all := []bestiary.Series{
		{Family: "gemma"},
		{Family: "gemma", Generation: "4"},
		{Family: "llama", Generation: "4"},
	}
	cases := []struct {
		selector string
		want     []bestiary.Series
	}{
		{"llama-4", []bestiary.Series{{Family: "llama", Generation: "4"}}},
		{"gemma-4", []bestiary.Series{{Family: "gemma", Generation: "4"}}},
		{"gemma", []bestiary.Series{{Family: "gemma"}, {Family: "gemma", Generation: "4"}}},
		{"  LLAMA-4  ", []bestiary.Series{{Family: "llama", Generation: "4"}}},
		{"nope", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := selectSeries(all, tc.selector)
		if len(got) != len(tc.want) {
			t.Errorf("selectSeries(%q) = %v, want %v", tc.selector, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("selectSeries(%q)[%d] = %+v, want %+v", tc.selector, i, got[i], tc.want[i])
			}
		}
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
