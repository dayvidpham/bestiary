package main

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// The two creator-dimension emissions are covered for byte-identity across N=100 runs
// by the extended reproducibility harness (runFixtureCodegenArtifacts). What that
// cannot show is whether either report DETECTS anything: an emitter that always
// returned an empty list would be perfectly reproducible. These tests supply the
// non-vacuity half — each is fed input that MUST produce a row — plus the
// INV3 contract checks (no wall clock, explicit sort, empty-not-null).

// decodeUnserved parses the creator→provider coverage report bytes.
func decodeUnserved(t *testing.T, data []byte) CreatorProvidersUnservedEnvelope {
	t.Helper()
	var env CreatorProvidersUnservedEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal unserved report: %v\n%s", err, data)
	}
	return env
}

// TestBuildCreatorProvidersUnserved_DetectsAnUnservedPair is the non-vacuity guard.
//
// It feeds a model set that serves a creator's family through ONE of that creator's
// curated surfaces and none of the others, so every other curated surface for that
// creator must be reported. A report that always came back empty — the failure mode
// the first draft of this emitter actually had, because it joined against codegen-side
// entities whose Providers slice is always empty — fails here.
func TestBuildCreatorProvidersUnserved_DetectsAnUnservedPair(t *testing.T) {
	// zhipu curates several surfaces and glm maps to zhipu; serve only the first.
	surfaces := bestiary.CreatorZhipu.Providers()
	if len(surfaces) < 2 {
		t.Skipf("precondition: creator %q needs >=2 curated surfaces to have an unserved one; has %d",
			bestiary.CreatorZhipu, len(surfaces))
	}
	if got := bestiary.Family("glm").Creator(); got != bestiary.CreatorZhipu {
		t.Skipf("precondition: family glm no longer maps to %q (got %q)", bestiary.CreatorZhipu, got)
	}

	models := []bestiary.ModelInfo{
		{ID: "glm-4.6", Provider: surfaces[0], Family: "glm", Version: "4.6"},
	}
	env := decodeUnserved(t, mustBuildUnserved(t, models))

	reported := make(map[bestiary.Provider]bool, len(env.Unserved))
	for _, row := range env.Unserved {
		reported[bestiary.Provider(row.Provider)] = true
	}
	if reported[surfaces[0]] {
		t.Errorf("surface %q IS served by the input but was reported unserved", surfaces[0])
	}
	for _, p := range surfaces[1:] {
		if !reported[p] {
			t.Errorf("curated surface %q serves nothing in the input but was NOT reported;\nreport: %+v", p, env.Unserved)
		}
	}
	if env.Count != len(env.Unserved) {
		t.Errorf("count = %d, want %d (len of the list)", env.Count, len(env.Unserved))
	}
}

// TestBuildCreatorProvidersUnserved_SortedAndTimestampFree pins the INV3 contract:
// rows sorted by (creator, provider) with an EXPLICIT sort, and no wall clock
// anywhere in the bytes. Both are what make the committed report byte-stable across
// regens; the first-seen-order shortcut is explicitly not a pattern to copy here.
func TestBuildCreatorProvidersUnserved_SortedAndTimestampFree(t *testing.T) {
	env := decodeUnserved(t, mustBuildUnserved(t, nil))
	if !sort.SliceIsSorted(env.Unserved, func(i, j int) bool {
		if env.Unserved[i].Creator != env.Unserved[j].Creator {
			return env.Unserved[i].Creator < env.Unserved[j].Creator
		}
		return env.Unserved[i].Provider < env.Unserved[j].Provider
	}) {
		t.Errorf("unserved rows are not sorted by (creator, provider): %+v", env.Unserved)
	}
	assertNoWallClock(t, mustBuildUnserved(t, nil))
}

// TestBuildCreatorProvidersUnserved_EmptyIsListNotNull asserts the healthy steady
// state (nothing unserved) serializes as an empty JSON ARRAY, not null. A null would
// make every consumer special-case the good case.
func TestBuildCreatorProvidersUnserved_EmptyIsListNotNull(t *testing.T) {
	// Serve EVERY curated surface, so nothing can be reported.
	var models []bestiary.ModelInfo
	for _, c := range bestiary.Creators() {
		fam := familyForCreator(c)
		if fam == "" {
			continue
		}
		for _, p := range c.Providers() {
			models = append(models, bestiary.ModelInfo{
				ID: bestiary.ModelID(string(fam) + "-1"), Provider: p, Family: fam, Version: "1",
			})
		}
	}
	data := mustBuildUnserved(t, models)
	env := decodeUnserved(t, data)
	if len(env.Unserved) != 0 {
		t.Fatalf("expected an empty report when every curated surface is served; got %+v", env.Unserved)
	}
	if !strings.Contains(string(data), `"unserved": []`) {
		t.Errorf("empty report did not serialize as an empty array;\n%s", data)
	}
}

// TestBuildCreatorsLabDisagreements_DetectsAMultiOrgFamily is the non-vacuity guard
// for the lab-derivation report: two lab prefixes reaching one family must produce a
// multi-org row naming both.
func TestBuildCreatorsLabDisagreements_DetectsAMultiOrgFamily(t *testing.T) {
	meta := []bestiary.EntityMetadata{
		{MetadataID: "meta/llama-3.3-70b"},
		{MetadataID: "nvidia/llama-3.3-nemotron-super"},
	}
	var env CreatorsLabDisagreementsEnvelope
	data, err := buildCreatorsLabDisagreements(meta)
	if err != nil {
		t.Fatalf("buildCreatorsLabDisagreements: %v", err)
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal disagreement report: %v\n%s", err, data)
	}
	var row *bestiary.CreatorLabDisagreement
	for i := range env.Disagreements {
		if env.Disagreements[i].Family == "llama" {
			row = &env.Disagreements[i]
			break
		}
	}
	if row == nil {
		t.Fatalf("no llama row in the report; two labs reach it so it must be reported;\n%s", data)
	}
	if row.Class != bestiary.CreatorLabClassMultiOrg {
		t.Errorf("llama class = %v, want %v", row.Class, bestiary.CreatorLabClassMultiOrg)
	}
	if strings.Join(row.Labs, ",") != "meta,nvidia" {
		t.Errorf("llama labs = %v, want [meta nvidia] sorted", row.Labs)
	}
	// The class token, not an integer, is what a curator reads.
	if !strings.Contains(string(data), `"class": "multi-org"`) {
		t.Errorf("class did not serialize as its token;\n%s", data)
	}
	if env.Count != len(env.Disagreements) {
		t.Errorf("count = %d, want %d", env.Count, len(env.Disagreements))
	}
}

// TestBuildCreatorsLabDisagreements_SortedTimestampFreeAndNeverFatal pins the rest of
// the emission contract: sorted by family, no wall clock, an empty list rather than
// null, and — the ruled behaviour — a disagreement NEVER fails the build. A catalog
// that disagrees with itself is the normal case; aborting codegen on one would make an
// ordinary upstream re-publication break the build.
func TestBuildCreatorsLabDisagreements_SortedTimestampFreeAndNeverFatal(t *testing.T) {
	meta := []bestiary.EntityMetadata{
		{MetadataID: "nvidia/mistral-nemo"},
		{MetadataID: "mistral/mistral-large"},
		{MetadataID: "nvidia/llama-3.3-nemotron"},
		{MetadataID: "meta/llama-3.3-70b"},
	}
	data, err := buildCreatorsLabDisagreements(meta)
	if err != nil {
		t.Fatalf("buildCreatorsLabDisagreements returned an error for input carrying real disagreements: %v\n"+
			"  Why this matters: a disagreement is a triage row, never a build failure", err)
	}
	var env CreatorsLabDisagreementsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	if !sort.SliceIsSorted(env.Disagreements, func(i, j int) bool {
		return env.Disagreements[i].Family < env.Disagreements[j].Family
	}) {
		t.Errorf("disagreement rows are not sorted by family: %+v", env.Disagreements)
	}
	assertNoWallClock(t, data)

	empty, err := buildCreatorsLabDisagreements(nil)
	if err != nil {
		t.Fatalf("buildCreatorsLabDisagreements(nil): %v", err)
	}
	if !strings.Contains(string(empty), `"disagreements": []`) {
		t.Errorf("empty report did not serialize as an empty array;\n%s", empty)
	}
}

// mustBuildUnserved builds the coverage report or fails the test.
func mustBuildUnserved(t *testing.T, models []bestiary.ModelInfo) []byte {
	t.Helper()
	data, err := buildCreatorProvidersUnserved(models)
	if err != nil {
		t.Fatalf("buildCreatorProvidersUnserved: %v", err)
	}
	return data
}

// familyForCreator returns some family mapped to c, or "" when none is. It reads the
// live curated table so the sweep stays correct as the table grows.
func familyForCreator(c bestiary.Creator) bestiary.Family {
	for _, e := range bestiary.Entities() {
		if e.Ref.Family.Creator() == c {
			return e.Ref.Family
		}
	}
	return ""
}

// assertNoWallClock fails if the emitted bytes carry anything that looks like an
// RFC3339 instant — the INV3 rule that keeps a committed report from churning on
// every regen.
func assertNoWallClock(t *testing.T, data []byte) {
	t.Helper()
	s := string(data)
	// An RFC3339 stamp always carries a "T" between two digits followed by a colon.
	for i := 1; i+3 < len(s); i++ {
		if s[i] != 'T' {
			continue
		}
		if isDigit(s[i-1]) && isDigit(s[i+1]) && isDigit(s[i+2]) && s[i+3] == ':' {
			t.Errorf("emitted report appears to carry a wall-clock timestamp near offset %d: %q\n"+
				"  Why this matters: a timestamp makes the committed artifact churn on every regen",
				i, s[max0(i-24):min(i+16, len(s))])
			return
		}
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func max0(i int) int {
	if i < 0 {
		return 0
	}
	return i
}
