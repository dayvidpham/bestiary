package main

// CLI fences for the `series` entity filters (--provider, --quant, --status).
//
// Each filter narrows the ENTITY list inside each release; releases emptied by a
// filter drop out, and lines whose releases all empty drop out of both views. The
// tests below assert that behaviour on the real registry rather than on a fixture,
// so they exercise exactly what a user runs.
//
// Every test derives its expectation from the library-side taxonomy rather than
// hard-coding a count, so a catalog refresh moves the expectation with the data and
// only a genuine filter regression reddens.

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// entityMatchesFilter is the test-side ORACLE for the filter semantics, written
// independently of the production predicate: an entity survives when ONE instance
// satisfies every active dimension at once.
func entityMatchesFilter(e bestiary.Entity, provider bestiary.Provider, quant bestiary.Quantization, byQuant bool, status bestiary.ModelStatus, byStatus bool) bool {
	for _, in := range e.Instances {
		if provider != "" && in.Provider != provider {
			continue
		}
		if byQuant {
			var has bool
			for _, qv := range in.QuantVRAM {
				if qv.Quant == quant {
					has = true
				}
			}
			if !has {
				continue
			}
		}
		if byStatus {
			m, ok := bestiary.LookupModelByProvider(in.Provider, string(in.ID))
			if !ok || m.Status != status {
				continue
			}
		}
		return true
	}
	return false
}

// expectedSeriesUnderFilter computes, from the library taxonomy, the set of line
// renderings that should survive a filter, plus the per-line entity totals.
func expectedSeriesUnderFilter(provider bestiary.Provider, quant bestiary.Quantization, byQuant bool, status bestiary.ModelStatus, byStatus bool) map[string]int {
	want := map[string]int{}
	for _, s := range bestiary.SeriesAll() {
		total := 0
		for _, r := range bestiary.ReleasesOf(s) {
			for _, e := range bestiary.EntitiesOf(r) {
				if entityMatchesFilter(e, provider, quant, byQuant, status, byStatus) {
					total++
				}
			}
		}
		if total > 0 {
			want[s.String()] = total
		}
	}
	return want
}

// seriesListingUnderFilter runs the listing with the given flags and returns the
// rendered line → entity-count map.
func seriesListingUnderFilter(t *testing.T, args ...string) map[string]int {
	t.Helper()
	var rows []struct {
		Series   string
		Releases int
		Entities int
	}
	runSeriesJSON(t, &rows, args...)
	got := map[string]int{}
	for _, r := range rows {
		got[r.Series] = r.Entities
	}
	return got
}

// TestSeries_ProviderFilter pins --provider: only lines with an entity served by
// that provider survive, with post-filter entity counts.
func TestSeries_ProviderFilter(t *testing.T) {
	const provider = bestiary.Provider("cohere")
	want := expectedSeriesUnderFilter(provider, 0, false, 0, false)
	if len(want) == 0 {
		t.Fatalf("catalog precondition lost: no series has an entity served by %q", provider)
	}
	if len(want) >= len(bestiary.SeriesAll()) {
		t.Fatalf("filter precondition lost: %q appears in every line (%d of %d), so the filter "+
			"cannot be shown to narrow anything", provider, len(want), len(bestiary.SeriesAll()))
	}

	got := seriesListingUnderFilter(t, "--provider", string(provider))
	if len(got) != len(want) {
		t.Errorf("series --provider=%s listed %d lines, want %d\n got: %v\nwant: %v",
			provider, len(got), len(want), got, want)
	}
	for line, n := range want {
		if got[line] != n {
			t.Errorf("line %q reported %d entities under --provider=%s, want %d", line, got[line], provider, n)
		}
	}
}

// TestSeries_StatusFilter pins --status against the same oracle. Status is a
// MODEL-level fact reached through the instance, so this also guards the
// (Provider, ID) lookup the predicate uses.
func TestSeries_StatusFilter(t *testing.T) {
	status, err := bestiary.ParseModelStatus("beta")
	if err != nil {
		t.Fatalf("ParseModelStatus(beta): %v", err)
	}
	want := expectedSeriesUnderFilter("", 0, false, status, true)
	if len(want) == 0 {
		t.Fatal("catalog precondition lost: no entity has a beta-status instance")
	}
	if len(want) >= len(bestiary.SeriesAll()) {
		t.Fatal("filter precondition lost: every line has a beta instance, so --status narrows nothing")
	}

	got := seriesListingUnderFilter(t, "--status", "beta")
	if len(got) != len(want) {
		t.Errorf("series --status=beta listed %d lines, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for line, n := range want {
		if got[line] != n {
			t.Errorf("line %q reported %d entities under --status=beta, want %d", line, got[line], n)
		}
	}
}

// TestSeries_QuantFilter pins --quant. Quantization data is curated and sparse, so
// this is the narrowest of the three filters — which is exactly what makes it a
// good check that the cascade drops lines rather than reporting them with zero.
func TestSeries_QuantFilter(t *testing.T) {
	quant, err := bestiary.ParseQuantization("q4_k_m")
	if err != nil {
		t.Fatalf("ParseQuantization(q4_k_m): %v", err)
	}
	want := expectedSeriesUnderFilter("", quant, true, 0, false)
	if len(want) == 0 {
		t.Fatal("catalog precondition lost: no entity carries a q4_k_m QuantVRAM row")
	}

	got := seriesListingUnderFilter(t, "--quant", "q4_k_m")
	if len(got) != len(want) {
		t.Errorf("series --quant=q4_k_m listed %d lines, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for line, n := range want {
		if got[line] != n {
			t.Errorf("line %q reported %d entities under --quant=q4_k_m, want %d", line, got[line], n)
		}
	}
	// The cascade: a line with no surviving entity must be ABSENT, never present
	// with a zero count.
	for line, n := range got {
		if n == 0 {
			t.Errorf("line %q survived --quant=q4_k_m with 0 entities; an emptied line must drop out", line)
		}
	}
}

// TestSeries_CombinedFilters pins the per-instance conjunction: --provider and
// --quant together must be satisfied by ONE instance. The oracle encodes the same
// rule independently, and the union-vs-intersection distinction is asserted
// directly — a per-dimension reading would keep strictly more lines.
func TestSeries_CombinedFilters(t *testing.T) {
	quant, err := bestiary.ParseQuantization("q4_k_m")
	if err != nil {
		t.Fatalf("ParseQuantization(q4_k_m): %v", err)
	}
	// llmgateway is chosen because it BOTH serves many lines and carries q4_k_m rows
	// on only some of them — the only shape in which a conjunction can be shown to be
	// strictly narrower than either dimension alone.
	const provider = bestiary.Provider("llmgateway")

	want := expectedSeriesUnderFilter(provider, quant, true, 0, false)
	if len(want) == 0 {
		t.Fatalf("catalog precondition lost: no entity has a single %q instance carrying a q4_k_m row; "+
			"this test would pass vacuously — re-pick the provider", provider)
	}
	got := seriesListingUnderFilter(t, "--provider", string(provider), "--quant", "q4_k_m")
	if len(got) != len(want) {
		t.Errorf("series --provider=%s --quant=q4_k_m listed %d lines, want %d\n got: %v\nwant: %v",
			provider, len(got), len(want), got, want)
	}
	for line, n := range want {
		if got[line] != n {
			t.Errorf("line %q reported %d entities under the combined filter, want %d", line, got[line], n)
		}
	}

	// The conjunction must not be looser than each filter alone, and must be STRICTLY
	// narrower than at least one of them — otherwise the combination is untested.
	onlyProvider := seriesListingUnderFilter(t, "--provider", string(provider))
	onlyQuant := seriesListingUnderFilter(t, "--quant", "q4_k_m")
	if len(got) >= len(onlyProvider) && len(got) >= len(onlyQuant) {
		t.Errorf("combined filter kept %d lines while --provider alone kept %d and --quant alone kept %d; "+
			"the conjunction narrows nothing here, so this case cannot detect a union/intersection regression",
			len(got), len(onlyProvider), len(onlyQuant))
	}
	for line := range got {
		if _, ok := onlyProvider[line]; !ok {
			t.Errorf("line %q survived the combined filter but not --provider=%s alone; the "+
				"conjunction must be at least as strict as each dimension", line, provider)
		}
		if _, ok := onlyQuant[line]; !ok {
			t.Errorf("line %q survived the combined filter but not --quant=q4_k_m alone; the "+
				"conjunction must be at least as strict as each dimension", line)
		}
	}
}

// TestSeries_ListingAndDetailAgree is the consistency fence between the two views:
// the entity count the LISTING reports for a line under a filter must equal the
// number of entity keys the DETAIL view of that same line prints under the same
// filter. A drift between them would let the listing advertise lines the detail
// view then refuses to show.
func TestSeries_ListingAndDetailAgree(t *testing.T) {
	for _, flags := range [][]string{
		{},
		{"--provider", "cohere"},
		{"--status", "beta"},
		{"--quant", "q4_k_m"},
	} {
		name := "unfiltered"
		if len(flags) > 0 {
			name = strings.Join(flags, "")
		}
		t.Run(name, func(t *testing.T) {
			listing := seriesListingUnderFilter(t, flags...)
			if len(listing) == 0 {
				t.Fatalf("listing under %v is empty; nothing to cross-check", flags)
			}
			checked := 0
			for line, wantEntities := range listing {
				var details []struct {
					Series   string
					Releases []struct {
						Name     string
						Entities []string
					}
				}
				runSeriesJSON(t, &details, append([]string{line}, flags...)...)
				total := 0
				for _, d := range details {
					if d.Series != line {
						continue
					}
					for _, r := range d.Releases {
						if len(r.Entities) == 0 {
							t.Errorf("line %q detail under %v kept release %q with 0 entities; "+
								"an emptied release must drop out", line, flags, r.Name)
						}
						total += len(r.Entities)
					}
				}
				if total != wantEntities {
					t.Errorf("line %q: listing says %d entities under %v, detail shows %d",
						line, wantEntities, flags, total)
				}
				checked++
				if checked >= 12 { // a representative sample; the whole registry is slow
					return
				}
			}
		})
	}
}

// TestSeries_UnknownFilterValuesRejected is the negative control for both parsed
// filters: an unrecognised value is an actionable error, never a silent empty
// result. A silent empty would be indistinguishable from "nothing matched", which
// is the failure mode parseQuantFilter exists to prevent.
func TestSeries_UnknownFilterValuesRejected(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown quant", []string{"series", "--quant", "not-a-quant"}, "unrecognised quantization"},
		{"unknown status", []string{"series", "--status", "not-a-status"}, "status"},
		{"unknown quant with a selector", []string{"series", "llama-3.3", "--quant", "q9_z_z"}, "unrecognised quantization"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() { runErr = run(tc.args) })
			if runErr == nil {
				t.Fatalf("run %v succeeded; an unknown filter value must be rejected. stdout:\n%s", tc.args, out)
			}
			if !strings.Contains(runErr.Error(), tc.want) {
				t.Errorf("error %q does not mention %q — the rejection must name what was wrong", runErr, tc.want)
			}
			if strings.TrimSpace(out) != "" {
				t.Errorf("run %v printed output before failing; the filter must be parsed before any "+
					"view is computed. stdout:\n%s", tc.args, out)
			}
		})
	}
}

// TestSeries_FilterEmptiesSelectedLine pins the distinct outcome for a GOOD
// selector emptied by a filter: an actionable error naming the filters, not
// ErrNotFound (the line exists) and not a header printed over nothing.
func TestSeries_FilterEmptiesSelectedLine(t *testing.T) {
	// command is a real line; cohere serves it but publishes no GGUF quant rows, so
	// the conjunction is empty.
	args := []string{"series", "command", "--provider", "cohere", "--quant", "q4_k_m"}
	var runErr error
	out := captureStdout(t, func() { runErr = run(args) })
	if runErr == nil {
		t.Fatalf("run %v succeeded; an emptied selection must be an actionable error. stdout:\n%s", args, out)
	}
	msg := runErr.Error()
	for _, want := range []string{"command", "--provider=cohere", "--quant=q4_k_m", "left no entities"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "not found") {
		t.Errorf("error %q reports not-found; the line EXISTS and the filter is what matched nothing", msg)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("run %v printed a view before failing. stdout:\n%s", args, out)
	}
}

// TestSeries_DBPathStillRejectedWithFilters guards the boundary the filters must
// not erode: the view stays registry-static, so --db-path remains rejected even
// when a filter is present.
func TestSeries_DBPathStillRejectedWithFilters(t *testing.T) {
	err := run([]string{"series", "--provider", "cohere", "--db-path", t.TempDir() + "/x.db"})
	if err == nil {
		t.Fatal("series accepted --db-path alongside a filter; it must stay rejected")
	}
	if !strings.Contains(err.Error(), "db-path") {
		t.Errorf("error %q does not name the rejected flag", err)
	}
}
