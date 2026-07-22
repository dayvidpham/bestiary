package bestiary

import (
	"testing"
)

// TestParseSeriesStrays_RealFile checks the committed parse/data/series.json loads
// into exactly the curated rows, with the target line and the optional release-name
// override read correctly.
func TestParseSeriesStrays_RealFile(t *testing.T) {
	table := loadSeriesStrays()
	if len(table) != 3 {
		t.Fatalf("loadSeriesStrays() = %d rows, want 3 (gemma4, gemma-4-31b-larkspur, gemini-exp)", len(table))
	}
	cases := []struct {
		family      Family
		wantSeries  Series
		wantRelease string
		wantHasName bool
	}{
		{"gemma4", Series{Family: "gemma", Generation: "4"}, "", false},
		{"gemma-4-31b-larkspur", Series{Family: "gemma", Generation: "4"}, "larkspur", true},
		{"gemini-exp", Series{Family: "gemini", Generation: ""}, "exp", true},
	}
	for _, tc := range cases {
		got, ok := table[tc.family]
		if !ok {
			t.Errorf("stray row for %q missing", tc.family)
			continue
		}
		if got.Series != tc.wantSeries || got.Release != tc.wantRelease || got.HasName != tc.wantHasName {
			t.Errorf("stray %q = %+v, want series %+v release %q hasName %v",
				tc.family, got, tc.wantSeries, tc.wantRelease, tc.wantHasName)
		}
	}
}

// TestParseSeriesStrays_GracefulDegrade is the load-failure contract (the
// lineage.json / modifier_class.json precedent): malformed or empty input yields an
// EMPTY but NON-NIL table — the taxonomy then falls back to pure computation — and
// never panics. Unusable individual rows are skipped without costing the good rows.
func TestParseSeriesStrays_GracefulDegrade(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantRows int
	}{
		{"malformed json", `{"strays": [`, 0},
		{"not an object", `["nope"]`, 0},
		{"empty file", ``, 0},
		{"no strays key", `{"schema_version": 1}`, 0},
		{"row without source family", `{"strays":[{"series":{"family":"gemma","generation":"4"}}]}`, 0},
		{"row without target family", `{"strays":[{"family":"gemma4","series":{"generation":"4"}}]}`, 0},
		{
			"one unusable row does not cost the good row",
			`{"strays":[{"family":"","series":{"family":"gemma","generation":"4"}},` +
				`{"family":"gemma4","series":{"family":"gemma","generation":"4"}}]}`,
			1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSeriesStrays([]byte(tc.raw))
			if got == nil {
				t.Fatal("parseSeriesStrays returned nil; the degraded value must be an empty NON-NIL map")
			}
			if len(got) != tc.wantRows {
				t.Errorf("parseSeriesStrays = %d rows, want %d", len(got), tc.wantRows)
			}
		})
	}
}

// TestBuildTaxonomyIndex_NoStrays_PureComputation exercises the computation without
// the static registry: a hand-built entity set must produce the same shape the
// registry does, proving the index is a function of key components only.
func TestBuildTaxonomyIndex_NoStrays_PureComputation(t *testing.T) {
	entities := []Entity{
		{Ref: EntityRef{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e"}},
		{Ref: EntityRef{Family: "llama", Variant: "maverick", Version: "4", ParamSize: "17b-128e"}},
		{Ref: EntityRef{Family: "llama", Version: "4"}},
		{Ref: EntityRef{Family: "llama", Version: "3.3"}},
	}
	idx := buildTaxonomyIndex(entities)
	if len(idx.series) != 2 {
		t.Fatalf("series = %+v, want 2 lines (llama-4, llama-3.3)", idx.series)
	}
	if idx.series[0] != (Series{Family: "llama", Generation: "3.3"}) ||
		idx.series[1] != (Series{Family: "llama", Generation: "4"}) {
		t.Errorf("series = %+v, want [llama-3.3 llama-4] in ascending order", idx.series)
	}
	llama4 := seriesKey(Series{Family: "llama", Generation: "4"})
	if got := len(idx.releases[llama4]); got != 3 {
		t.Errorf("llama-4 has %d releases, want 3 (bare, maverick, scout)", got)
	}
	if names := releaseNamesOf(idx.releases[llama4]); names[0] != "" || names[1] != "maverick" || names[2] != "scout" {
		t.Errorf("llama-4 release names = %v, want [\"\" maverick scout]", names)
	}
}

func releaseNamesOf(in []Release) []string {
	out := make([]string, 0, len(in))
	for _, r := range in {
		out = append(out, r.Name)
	}
	return out
}

// TestBuildGenerationNormalization is the unit fence on the ONE normalization rule:
// a bare "N" folds into "N.0" only when the same family also spells "N.0". The
// negative arms are the point — an unconditional fold would rename llama-4.
func TestBuildGenerationNormalization(t *testing.T) {
	raw := map[Family]map[string]bool{
		"gemini": {"3": true, "3.0": true, "2.5": true, "": true},
		"llama":  {"4": true, "3.3": true},
		"veo":    {"3": true, "3.0": true},
		"odd":    {"3.0": true},              // dotted only: nothing to fold
		"multi":  {"12": true, "12.0": true}, // multi-digit generations fold too
	}
	got := buildGenerationNormalization(raw)

	if got["gemini"]["3"] != "3.0" {
		t.Errorf("gemini 3 -> %q, want %q", got["gemini"]["3"], "3.0")
	}
	if got["veo"]["3"] != "3.0" {
		t.Errorf("veo 3 -> %q, want %q", got["veo"]["3"], "3.0")
	}
	if got["multi"]["12"] != "12.0" {
		t.Errorf("multi 12 -> %q, want %q", got["multi"]["12"], "12.0")
	}
	if _, ok := got["llama"]; ok {
		t.Errorf("llama got a normalization entry (%v); with no dotted sibling nothing may fold", got["llama"])
	}
	if _, ok := got["odd"]; ok {
		t.Errorf("odd got a normalization entry (%v); a dotted-only family has nothing to fold", got["odd"])
	}
	// A dotted generation is never itself a fold SOURCE, and the empty generation is
	// never eligible.
	if _, ok := got["gemini"]["2.5"]; ok {
		t.Error("a dotted generation must never be a fold source")
	}
	if _, ok := got["gemini"][""]; ok {
		t.Error("the empty generation must never be a fold source")
	}
}

// TestIsBareIntegerGeneration pins the eligibility predicate for the fold.
func TestIsBareIntegerGeneration(t *testing.T) {
	cases := map[string]bool{
		"3": true, "12": true, "2025": true,
		"": false, "3.0": false, "v3": false, "3b": false, "3-preview": false,
	}
	for in, want := range cases {
		if got := isBareIntegerGeneration(in); got != want {
			t.Errorf("isBareIntegerGeneration(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestComputeRelease_StrayOverridesComputation verifies the resolution order:
// curated beats mechanical, and a stray without a release name keeps the computed
// variant.
func TestComputeRelease_StrayOverridesComputation(t *testing.T) {
	strays := map[Family]seriesStray{
		"gemma4":   {Series: Series{Family: "gemma", Generation: "4"}},
		"larkspur": {Series: Series{Family: "gemma", Generation: "4"}, Release: "larkspur", HasName: true},
	}
	gens := map[Family]map[string]string{"gemma": {"4": "4.0"}}

	// Named override replaces the computed variant.
	got := computeRelease(EntityRef{Family: "larkspur", Variant: "v0.5", Version: "4"}, strays, gens)
	if got.Series != (Series{Family: "gemma", Generation: "4"}) || got.Name != "larkspur" {
		t.Errorf("stray with release name = %+v, want gemma-4/larkspur", got)
	}
	// Un-named stray keeps the computed variant as the release name.
	got = computeRelease(EntityRef{Family: "gemma4", Variant: "mini"}, strays, gens)
	if got.Series != (Series{Family: "gemma", Generation: "4"}) || got.Name != "mini" {
		t.Errorf("stray without release name = %+v, want gemma-4/mini", got)
	}
	// The curated generation is taken VERBATIM: normalization does not re-touch it
	// (curation is authoritative for a stray).
	if got.Series.Generation != "4" {
		t.Errorf("stray generation = %q, want the curated %q verbatim", got.Series.Generation, "4")
	}
	// A non-stray family goes through the mechanical path, normalization included.
	got = computeRelease(EntityRef{Family: "gemma", Variant: "x", Version: "4"}, strays, gens)
	if got.Series != (Series{Family: "gemma", Generation: "4.0"}) || got.Name != "x" {
		t.Errorf("mechanical path = %+v, want gemma-4.0/x", got)
	}
}
