package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
//
// CatD is a conservative UPPER BOUND on genuine mislabels: the A/B/C heuristics
// are deliberately narrow and may miss some auto-fixable patterns, so CatD
// over-counts by design — the downstream ledger review filters the residue.
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
	slices.Sort(multiIDs)

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
		slices.Sort(families)

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
		slices.SortFunc(tupleSlice, func(a, b familyTuple) int {
			if a.Family != b.Family {
				return strings.Compare(string(a.Family), string(b.Family))
			}
			if a.Variant != b.Variant {
				return strings.Compare(a.Variant, b.Variant)
			}
			return strings.Compare(a.Version, b.Version)
		})

		divergents = append(divergents, divergentID{
			id:       id,
			tuples:   tupleSlice,
			families: families,
			cat:      cat,
		})
	}
	slices.SortFunc(divergents, func(a, b divergentID) int {
		return strings.Compare(a.id, b.id)
	})

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
	slices.SortFunc(genuinePairs, func(a, b pairEntry) int {
		if a.count != b.count {
			return b.count - a.count // descending by count
		}
		if a.pair[0] != b.pair[0] {
			return strings.Compare(string(a.pair[0]), string(b.pair[0]))
		}
		return strings.Compare(string(a.pair[1]), string(b.pair[1]))
	})

	// ── Assertions ────────────────────────────────────────────────────────────

	totalDivergent := len(divergents)
	totalMulti := len(multiIDs)

	snapshotCommit := loadSnapshotCommit(t)

	// The committed snapshot is a FIXED blob, so the divergence count is a HARD
	// invariant, not a fuzzy band. Refreshing the snapshot intentionally changes
	// this number — update divergenceExact in lockstep with the new blob.
	//
	// SLICE-5's hardened TestStaticDataset_CrossProviderConsistency is the
	// authoritative cross-provider gate; THIS analyzer pins the rc2 empirical
	// baseline (the 388 figure the whole rc2 effort is measured against).
	//
	// SLICE-1 (rc2) updated these constants: M4 (case-fold) resolved all 10 CatA
	// cases; recoverMemberVariant resolved 38 CatC cases. New baseline: 340 / A=0
	// / B=73 / C=210 / D=57. The "Meta <-> llama" mislabel pair became
	// "llama <-> meta" (M4 lowercased the inferred family).
	//
	// SLICE-1 (rc2) FIX CYCLE is divergence-NEUTRAL on this count: restoring
	// recoverMemberVariant's B1 family-agnostic sole-residual promotion (seed→flash,
	// reka→flash, imagen→ultra, voyage→large/lite, …) reduces parse-failure residuals
	// (a separate metric) but only promotes VERSION-bearing IDs — exactly what the
	// original inline B1 did. Those promotions do not change which multi-provider IDs
	// agree, so the divergence figures stay 340 / 0 / 73 / 210 / 57. The jvpa M3
	// helper symmetry fix is likewise non-observable here (version extraction unchanged).
	const (
		divergenceExact = 340
		// Secondary sanity band — guards against a wholesale snapshot/pipeline
		// breakage that happens to coincidentally land on a different exact value.
		divergenceLow  = 280
		divergenceHigh = 500
	)
	if totalDivergent != divergenceExact {
		t.Errorf("divergence count = %d, want exactly %d\n"+
			"  What: the cross-provider (Family,Variant,Version) divergence count drifted\n"+
			"  Why: this is the committed-snapshot baseline (snapshot commit %s); it only\n"+
			"       changes when models_api.json is refreshed OR the parse pipeline changes\n"+
			"  How to fix: if you refreshed the snapshot intentionally, update divergenceExact\n"+
			"       (and the category floors below) to the new figures; otherwise a pipeline\n"+
			"       regression reclassified IDs — investigate ParseFamilyDetailed",
			totalDivergent, divergenceExact, snapshotCommit)
	}
	if totalDivergent < divergenceLow || totalDivergent > divergenceHigh {
		t.Errorf("divergence count %d is outside sanity band [%d, %d]; "+
			"snapshot or parse pipeline broke badly",
			totalDivergent, divergenceLow, divergenceHigh)
	}

	// Pin the per-category counts on the fixed snapshot. These guard against a
	// silent CatD→CatC (or any cross-category) reclassification that would keep
	// the total at 340 while changing the genuine-mislabel ledger candidates.
	// Refreshing the snapshot intentionally changes these — update in lockstep.
	// SLICE-1: CatA → 0 (M4 fixed all case divergences); CatC → 210 (recoverMemberVariant).
	// FIX CYCLE divergence-neutral (see divergenceExact note above): restored B1
	// promotion is version-gated, so it does not move these category counts.
	const (
		catAExact = 0   // vendor-prefix/case (SLICE-1 M4 resolved all)
		catBExact = 73  // bare-gen-split (SLICE-2 scope)
		catCExact = 210 // member-variant recovery (SLICE-1 recoverMemberVariant resolved 38)
		catDExact = 57  // genuine family mislabel (ledger candidates)
	)
	checkCat := func(name string, got, want int) {
		if got != want {
			t.Errorf("%s count = %d, want exactly %d\n"+
				"  What: a divergent ID was reclassified between categories\n"+
				"  Why: committed-snapshot baseline (commit %s); a category-count shift\n"+
				"       with an unchanged total signals silent reclassification (e.g. CatD→CatC)\n"+
				"  How to fix: if the snapshot was refreshed, update the cat*Exact floors;\n"+
				"       otherwise investigate the classification heuristic / parse pipeline",
				name, got, want, snapshotCommit)
		}
	}
	checkCat("CatA (vendor-prefix/case)", catCounts[CatA], catAExact)
	checkCat("CatB (bare-gen-split)", catCounts[CatB], catBExact)
	checkCat("CatC (member-variant)", catCounts[CatC], catCExact)
	checkCat("CatD (genuine-mislabel)", catCounts[CatD], catDExact)

	// ── Log summary ───────────────────────────────────────────────────────────

	t.Logf("=== Cross-Provider Divergence Analysis (snapshot commit %s) ===", snapshotCommit)
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
		SnapshotCommit:        snapshotCommit,
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
