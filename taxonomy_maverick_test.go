package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// The maverick MEMBER-IZE re-key fences.
//
// maverick is a curated member of the llama family (parse/data/families.json), so
// llama-4's maverick instances key to llama/maverick@4#<size> as siblings of scout
// rather than collapsing into the bare llama@4 line. This is the epoch's ONE
// deliberate entity re-key; the tests below are its falsifiable evidence — the new
// keys exist with their full instance census, the old keys are GONE (a move, never a
// copy), and the taxonomy places both releases under the same llama-4 series.

// TestReleasesOf_Llama4 is the ratified worked example: the llama-4 series has the
// scout and maverick releases (plus the bare, un-named line), and each release's
// entities are exactly its sized siblings.
//
// The maverick membership is the epoch's ONE deliberate re-key: maverick is a
// curated llama family member, so its instances key to llama/maverick@4#... as
// siblings of scout rather than collapsing into the bare llama@4 line.
func TestReleasesOf_Llama4(t *testing.T) {
	line := bestiary.Series{Family: "llama", Generation: "4"}
	if !seriesSet(bestiary.SeriesAll())[line] {
		t.Fatalf("SeriesAll() does not contain %+v", line)
	}
	if got, want := line.String(), "llama-4"; got != want {
		t.Errorf("Series.String() = %q, want %q", got, want)
	}

	wantNames := []string{"", "maverick", "scout"}
	if got := releaseNames(bestiary.ReleasesOf(line)); !equalStrings(got, wantNames) {
		t.Errorf("ReleasesOf(llama-4) names = %v, want %v (ascending; the bare line first)", got, wantNames)
	}

	cases := []struct {
		name     string
		wantKeys []string
	}{
		{"scout", []string{"llama/scout@4#17b-16e", "llama/scout@4#17b-16e{instruct}"}},
		{"maverick", []string{"llama/maverick@4#17b-128e", "llama/maverick@4#17b-128e{instruct}"}},
	}
	for _, tc := range cases {
		rel := bestiary.Release{Series: line, Name: tc.name}
		if got, want := rel.String(), "llama-4/"+tc.name; got != want {
			t.Errorf("Release.String() = %q, want %q", got, want)
		}
		if got := entityKeysOf(bestiary.EntitiesOf(rel)); !equalStrings(got, tc.wantKeys) {
			t.Errorf("EntitiesOf(llama-4/%s) = %v, want %v", tc.name, got, tc.wantKeys)
		}
	}
}

// TestReleasesOf_Llama4Maverick_InstanceCensus pins the maverick re-key at the
// instance level: the two re-keyed entities carry exactly the 23 instance rows they
// carried under their old bare-llama keys (8 + 15), and NO instance was gained or
// lost by the re-key. It is the member-ize ruling's falsifiable evidence.
func TestReleasesOf_Llama4Maverick_InstanceCensus(t *testing.T) {
	const (
		wantBase     = 8
		wantInstruct = 15
	)
	rel := bestiary.Release{Series: bestiary.Series{Family: "llama", Generation: "4"}, Name: "maverick"}
	got := map[string]int{}
	for _, e := range bestiary.EntitiesOf(rel) {
		got[e.Ref.String()] = len(e.Instances)
	}
	want := map[string]int{
		"llama/maverick@4#17b-128e":           wantBase,
		"llama/maverick@4#17b-128e{instruct}": wantInstruct,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("entity %q has %d instances, want %d", k, got[k], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("llama-4/maverick has %d entities (%v), want %d", len(got), got, len(want))
	}
	// The old bare-llama keys MUST be gone: the re-key is a move, not a copy.
	for _, stale := range []string{"llama@4#17b-128e", "llama@4#17b-128e{instruct}"} {
		if _, ok := bestiary.EntityByKey(stale); ok {
			t.Errorf("stale pre-re-key entity %q still present; maverick member-ize must MOVE the key, not duplicate it", stale)
		}
	}
}
