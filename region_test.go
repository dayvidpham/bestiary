package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestParseRegionRoundTrip pins the ratified S1 deliverable "ParseRegion round-trip
// incl 'unspecified'": every named Region member round-trips through its String()
// rendering back to the same member (RegionNone <-> "unspecified" in BOTH
// directions), the empty string is the second spelling of absence (-> RegionNone),
// and an unknown non-empty token yields a non-nil actionable error that names the
// offending token. RegionOther is deliberately NOT round-trippable: it is the
// DetectRegion carrier bucket, never minted from a bare token, so ParseRegion("other")
// must reject rather than resurrect it.
func TestParseRegionRoundTrip(t *testing.T) {
	t.Parallel()

	// (1) String() -> ParseRegion -> same member, for every named member.
	roundTrip := []bestiary.Region{
		bestiary.RegionNone, // renders "unspecified"
		bestiary.RegionUS,
		bestiary.RegionEU,
		bestiary.RegionAPAC,
		bestiary.RegionGlobal,
		bestiary.RegionAU,
		bestiary.RegionJP,
	}
	for _, want := range roundTrip {
		token := want.String()
		got, err := bestiary.ParseRegion(token)
		if err != nil {
			t.Errorf("ParseRegion(%q) unexpected error: %v", token, err)
			continue
		}
		if got != want {
			t.Errorf("round-trip: ParseRegion(%q) = %v, want %v", token, got, want)
		}
	}

	// (2) Both spellings of absence -> RegionNone; and String(RegionNone) == "unspecified".
	if s := bestiary.RegionNone.String(); s != "unspecified" {
		t.Errorf("RegionNone.String() = %q, want %q", s, "unspecified")
	}
	for _, absent := range []string{"", "unspecified", "  UNSPECIFIED  "} {
		got, err := bestiary.ParseRegion(absent)
		if err != nil {
			t.Errorf("ParseRegion(%q) unexpected error: %v", absent, err)
			continue
		}
		if got != bestiary.RegionNone {
			t.Errorf("ParseRegion(%q) = %v, want RegionNone", absent, got)
		}
	}

	// (3) Case-insensitive acceptance of a named token (the parser lowercases/trims).
	if got, err := bestiary.ParseRegion(" US "); err != nil || got != bestiary.RegionUS {
		t.Errorf("ParseRegion(\" US \") = (%v, %v), want (RegionUS, nil)", got, err)
	}

	// (4) Unknown non-empty tokens -> non-nil actionable error that names the token.
	//     "other" is included deliberately: RegionOther.String() is "other" but it is
	//     NOT a parseable input (carrier-only), so it must reject.
	for _, bad := range []string{"bogus", "other", "usa", "eu-west-1"} {
		_, err := bestiary.ParseRegion(bad)
		if err == nil {
			t.Errorf("ParseRegion(%q) = nil error, want an actionable rejection", bad)
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("ParseRegion(%q) error does not name the offending token: %q", bad, err.Error())
		}
	}
}
