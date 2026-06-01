package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// familyTuple is the normalized (Family, Variant, Version) triple produced by
// ParseFamilyDetailed. Two providers diverge when their tuples differ for the
// same model ID.
type familyTuple struct {
	Family  bestiary.Family
	Variant string
	Version string
}

// divergenceCategory classifies each divergent multi-provider model ID into one
// of four buckets:
//
//   - CatA (vendor-prefix/case): families differ only by letter-case
//     (e.g. "MiniMax" vs "minimax"). Fixable by M3 lowercase normalisation.
//   - CatB (bare-gen-split): families share a common base when stripped of
//     trailing version tokens (e.g. "qwen" vs "qwen3.6"). These arise when
//     some providers embed the generation number in the family slug.
//   - CatC (member-variant recovery): one family string is a direct hyphen-
//     prefix of another (e.g. "gpt" vs "gpt-mini"). The sub-family should be
//     recovered as a variant.
//   - CatD (genuine family mislabel): none of A/B/C apply; the families are
//     genuinely inconsistent across providers. These are the ledger candidates
//     that require manual curation to resolve.
type divergenceCategory int

const (
	CatA divergenceCategory = iota + 1 // vendor-prefix / case
	CatB                               // bare-generation split
	CatC                               // member-variant recovery
	CatD                               // genuine family mislabel (ledger candidate)
)

func (c divergenceCategory) String() string {
	switch c {
	case CatA:
		return "A:vendor-case"
	case CatB:
		return "B:bare-gen-split"
	case CatC:
		return "C:member-variant"
	case CatD:
		return "D:genuine-mislabel"
	default:
		return "unknown"
	}
}

// classifyDivergence returns the category for a set of distinct families
// observed across providers for a single model ID.
//
// Precondition: len(families) >= 2 (caller filters for actual divergences).
func classifyDivergence(families []bestiary.Family) divergenceCategory {
	// CatA: all families are identical after case-folding.
	lowers := make([]string, len(families))
	for i, f := range families {
		lowers[i] = strings.ToLower(string(f))
	}
	allSameCase := true
	for _, l := range lowers[1:] {
		if l != lowers[0] {
			allSameCase = false
			break
		}
	}
	if allSameCase {
		return CatA
	}

	// CatB: all families share a common non-empty base when trailing version
	// components (digits, dots, hyphens-with-digits) are stripped.
	stripped := make([]string, len(families))
	for i, f := range families {
		stripped[i] = strings.ToLower(strings.TrimRight(string(f), "0123456789.-"))
	}
	allSameStripped := stripped[0] != ""
	for _, s := range stripped[1:] {
		if s != stripped[0] {
			allSameStripped = false
			break
		}
	}
	if allSameStripped {
		return CatB
	}

	// CatC: at least one pair of families where one is a hyphen-prefix of the
	// other (e.g. "gpt" is a prefix of "gpt-mini").
	for i, f1 := range families {
		for _, f2 := range families[i+1:] {
			s1 := strings.ToLower(string(f1))
			s2 := strings.ToLower(string(f2))
			if strings.HasPrefix(s1, s2+"-") || strings.HasPrefix(s2, s1+"-") {
				return CatC
			}
		}
	}

	// CatD: genuine mislabel — none of the above patterns explain the divergence.
	return CatD
}

// divergenceReport is the committed artifact structure written to
// testdata/snapshot/divergence_report.json.
type divergenceReport struct {
	// TotalMultiProviderIDs is the count of model IDs hosted by ≥2 providers.
	TotalMultiProviderIDs int `json:"total_multi_provider_ids"`
	// TotalDivergentIDs is the count of model IDs where (Family,Variant,Version)
	// is not identical across all providers. This is the ~388 baseline.
	TotalDivergentIDs int `json:"total_divergent_ids"`
	// CatACounts through CatDCounts are per-category model ID counts.
	CatACounts int `json:"cat_a_vendor_case_count"`
	CatBCounts int `json:"cat_b_bare_gen_split_count"`
	CatCCounts int `json:"cat_c_member_variant_count"`
	CatDCounts int `json:"cat_d_genuine_mislabel_count"`
	// GenuineMislabelPairs is the list of distinct (family_a, family_b) pairs
	// found in CatD divergences, with occurrence counts. This is the headline
	// deliverable for ledger sign-off.
	GenuineMislabelPairs []mislabelPair `json:"genuine_mislabel_pairs"`
	// SnapshotCommit records the upstream git commit for traceability.
	SnapshotCommit string `json:"snapshot_commit"`
}

// mislabelPair records a single (FamilyA, FamilyB) disagreement and how many
// model IDs exhibit it.
type mislabelPair struct {
	FamilyA string `json:"family_a"`
	FamilyB string `json:"family_b"`
	Count   int    `json:"count"`
}

// TestSnapshotAnalysis_CrossProviderDivergences loads the committed snapshot,
// runs every multi-provider model ID through the current production parse
// pipeline, and verifies that the cross-provider (Family,Variant,Version)
// divergence count reproduces the expected ~388 baseline.
//
// The test also categorizes every divergent ID and writes a committed report
// artifact to testdata/snapshot/divergence_report.json.
//
// This test is intentionally OFFLINE: it reads only the committed snapshot file
// and never makes a network request.
func TestSnapshotAnalysis_CrossProviderDivergences(t *testing.T) {
	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("LoadSnapshotRecords: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("snapshot is empty — no records loaded")
	}

	// Group records by model ID.
	type entry struct {
		provider bestiary.Provider
		rawFam   bestiary.Family
		tuple    familyTuple
	}
	byID := make(map[string][]entry)
	for _, r := range records {
		fam, variant, version, _, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		byID[string(r.ID)] = append(byID[string(r.ID)], entry{
			provider: r.Provider,
			rawFam:   r.RawFamily,
			tuple:    familyTuple{fam, variant, version},
		})
	}

	// Collect multi-provider IDs.
	var multiIDs []string
	for id, entries := range byID {
		if len(entries) >= 2 {
			multiIDs = append(multiIDs, id)
		}
	}
	sort.Strings(multiIDs)

	// Identify divergent IDs (those where tuples are not all identical).
	type divergentID struct {
		id       string
		tuples   []familyTuple // distinct tuples seen
		families []bestiary.Family
		cat      divergenceCategory
	}
	var divergents []divergentID

	for _, id := range multiIDs {
		entries := byID[id]
		seenTuples := make(map[familyTuple]struct{})
		for _, e := range entries {
			seenTuples[e.tuple] = struct{}{}
		}
		if len(seenTuples) <= 1 {
			continue // all providers agree
		}

		// Collect distinct family strings (non-empty only, for categorization).
		familySet := make(map[bestiary.Family]struct{})
		for t := range seenTuples {
			if t.Family != "" {
				familySet[t.Family] = struct{}{}
			}
		}
		families := make([]bestiary.Family, 0, len(familySet))
		for f := range familySet {
			families = append(families, f)
		}
		sort.Slice(families, func(i, j int) bool { return families[i] < families[j] })

		var cat divergenceCategory
		if len(families) <= 1 {
			// Divergence is in Variant/Version only (same family — treat as CatC).
			cat = CatC
		} else {
			cat = classifyDivergence(families)
		}

		var tupleSlice []familyTuple
		for tup := range seenTuples {
			tupleSlice = append(tupleSlice, tup)
		}
		sort.Slice(tupleSlice, func(i, j int) bool {
			if tupleSlice[i].Family != tupleSlice[j].Family {
				return tupleSlice[i].Family < tupleSlice[j].Family
			}
			if tupleSlice[i].Variant != tupleSlice[j].Variant {
				return tupleSlice[i].Variant < tupleSlice[j].Variant
			}
			return tupleSlice[i].Version < tupleSlice[j].Version
		})

		divergents = append(divergents, divergentID{
			id:       id,
			tuples:   tupleSlice,
			families: families,
			cat:      cat,
		})
	}
	sort.Slice(divergents, func(i, j int) bool { return divergents[i].id < divergents[j].id })

	// Count per category.
	catCounts := map[divergenceCategory]int{}
	for _, d := range divergents {
		catCounts[d.cat]++
	}

	// Collect genuine mislabel pairs (CatD only).
	genuinePairCounts := make(map[[2]bestiary.Family]int)
	for _, d := range divergents {
		if d.cat != CatD {
			continue
		}
		for i, f1 := range d.families {
			for _, f2 := range d.families[i+1:] {
				pair := [2]bestiary.Family{f1, f2}
				if f1 > f2 {
					pair = [2]bestiary.Family{f2, f1}
				}
				genuinePairCounts[pair]++
			}
		}
	}

	// Sort genuine pairs by count desc, then alphabetically.
	type pairEntry struct {
		pair  [2]bestiary.Family
		count int
	}
	var genuinePairs []pairEntry
	for p, c := range genuinePairCounts {
		genuinePairs = append(genuinePairs, pairEntry{p, c})
	}
	sort.Slice(genuinePairs, func(i, j int) bool {
		if genuinePairs[i].count != genuinePairs[j].count {
			return genuinePairs[i].count > genuinePairs[j].count
		}
		if genuinePairs[i].pair[0] != genuinePairs[j].pair[0] {
			return genuinePairs[i].pair[0] < genuinePairs[j].pair[0]
		}
		return genuinePairs[i].pair[1] < genuinePairs[j].pair[1]
	})

	// ── Assertions ────────────────────────────────────────────────────────────

	totalDivergent := len(divergents)
	totalMulti := len(multiIDs)

	// The divergence count must be in the expected band (>300, <=500).
	// The exact baseline is 388 as of snapshot commit 6a41e313.
	const (
		divergenceLow  = 300
		divergenceHigh = 500
		divergenceExact = 388
	)
	if totalDivergent < divergenceLow || totalDivergent > divergenceHigh {
		t.Errorf("divergence count %d is outside expected band [%d, %d]; "+
			"snapshot or parse pipeline may have changed",
			totalDivergent, divergenceLow, divergenceHigh)
	}
	// Warn (not fatal) if the exact baseline drifts.
	if totalDivergent != divergenceExact {
		t.Logf("NOTE: divergence count %d differs from exact baseline %d; "+
			"acceptable if snapshot or pipeline was updated intentionally",
			totalDivergent, divergenceExact)
	}

	// ── Log summary ───────────────────────────────────────────────────────────

	t.Logf("=== Cross-Provider Divergence Analysis (snapshot commit 6a41e313) ===")
	t.Logf("Multi-provider model IDs : %d", totalMulti)
	t.Logf("Divergent IDs (total)    : %d", totalDivergent)
	t.Logf("  A vendor-prefix/case   : %d", catCounts[CatA])
	t.Logf("  B bare-gen-split       : %d", catCounts[CatB])
	t.Logf("  C member-variant       : %d", catCounts[CatC])
	t.Logf("  D genuine-mislabel     : %d", catCounts[CatD])
	t.Logf("")
	t.Logf("Genuine mislabel family pairs (%d distinct):", len(genuinePairs))
	for _, pe := range genuinePairs {
		t.Logf("  %q <-> %q : %d occurrence(s)", pe.pair[0], pe.pair[1], pe.count)
	}

	// ── Write committed report artifact ───────────────────────────────────────

	reportPairs := make([]mislabelPair, 0, len(genuinePairs))
	for _, pe := range genuinePairs {
		reportPairs = append(reportPairs, mislabelPair{
			FamilyA: string(pe.pair[0]),
			FamilyB: string(pe.pair[1]),
			Count:   pe.count,
		})
	}

	report := divergenceReport{
		TotalMultiProviderIDs: totalMulti,
		TotalDivergentIDs:     totalDivergent,
		CatACounts:            catCounts[CatA],
		CatBCounts:            catCounts[CatB],
		CatCCounts:            catCounts[CatC],
		CatDCounts:            catCounts[CatD],
		GenuineMislabelPairs:  reportPairs,
		SnapshotCommit:        "6a41e313",
	}

	reportBytes, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		t.Fatalf("marshal divergence report: %v", marshalErr)
	}

	reportPath := filepath.Join(snapshotDir(), "divergence_report.json")
	if writeErr := os.WriteFile(reportPath, append(reportBytes, '\n'), 0o644); writeErr != nil {
		t.Fatalf("write divergence report to %s: %v", reportPath, writeErr)
	}
	t.Logf("Committed report written to: %s", reportPath)

	// Print a concise summary as a final test log line for CI visibility.
	pairs := make([]string, 0, len(genuinePairs))
	for _, pe := range genuinePairs {
		pairs = append(pairs, fmt.Sprintf("%s<->%s(%d)", pe.pair[0], pe.pair[1], pe.count))
	}
	t.Logf("SUMMARY: divergent=%d A=%d B=%d C=%d D=%d genuine-pairs=[%s]",
		totalDivergent,
		catCounts[CatA], catCounts[CatB], catCounts[CatC], catCounts[CatD],
		strings.Join(pairs, ", "))
}
