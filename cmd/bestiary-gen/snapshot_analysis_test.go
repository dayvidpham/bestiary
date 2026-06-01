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
	//
	// SLICE-2 (rc2) bare_gen_split: the closed predicate splits glued <base><int>
	// family tokens (qwen3→qwen+v3, o3→o+v3, gpt-5→gpt+v5, gemini-3→gemini+v3,
	// glm-5→glm+v5) for the 5 flagged families (qwen, o, gpt, gemini, glm) whose
	// split form is attested in this snapshot. New baseline: 281 / A=0 / B=3 / C=221
	// / D=57. Empirically verified (divergent-ID set diff vs the fc51ddc baseline):
	// 59 IDs converged (left the divergent set) and ZERO previously-agreeing IDs
	// broke. CatB fell 73→3; the 3 residual CatB IDs (bases hy / lyria / rnj) are
	// correctly DECLINED by the closed predicate because they have no families.json
	// entry — clearing them would require ADDING speculative single-occurrence family
	// keys (open per-name maintenance), which the closed predicate exists to avoid.
	// CatC rose 210→221 (+11): those 11 are former-CatB IDs that split correctly to
	// the base family but still diverge on a deeper hyphenated-version / param-count /
	// letter-suffix form (e.g. qwen vs qwen-2.5-72b, gpt vs gpt-4o, glm vs glm-5v) —
	// out of bare-gen scope, so they remain honestly divergent rather than masked.
	//
	// SLICE-3 (rc2) family_aliases ledger + uniform thinking/vision-as-modifier
	// migration: New baseline 259 / A=0 / B=3 / C=212 / D=44 (was 281/0/3/221/57).
	//  - PART A ledger: l3/l3.1/l3.3 → llama folds (RATIFIED, community Llama-3
	//    finetunes) cleared the three l3*<->llama CatD pairs — every sao10k/l3* ID now
	//    decomposes to family "llama" instead of the shorthand seed.
	//  - PART B modifier migration: removing the deepseek-thinking/kimi-thinking
	//    /grok-vision overrides + the thinking/vision members/suffixes makes the
	//    thinking-family IDs (kimi-k2-thinking et al.) decompose CONSISTENTLY to the
	//    base family with the token surfaced as the first-class Modifier (not Variant),
	//    converging the family across the empty-raw and raw="<fam>-thinking" providers.
	// Net: total 281→259 (−22), CatD 57→44 (−13), CatC 221→212 (−9). No previously
	// agreeing ID broke (verified by suite + NoDateVersions=0 + cross-provider gate).
	//
	// SLICE-8 (rc2) ID-driven version-presence consistency + param-size guard +
	// glued letter-suffix + letter-prefix series split: New baseline 158 / A=0 / B=3
	// / C=111 / D=44 (was 259/0/3/212/44). All −101 are CatC (member-variant/version)
	// reductions:
	//  - (a) ID-DRIVEN VERSION: ExtractVersionFromID now strips the vendor/path
	//    namespace + case-folds; ExtractVersionBetween handles a bare dot-version
	//    remainder; the empty-raw passthrough + the family+variant-compound prefix now
	//    extract post-variant versions. Cleared ALL 89 version-only divergences
	//    (gpt-4.1, glm-4.x, gemma-N, grok-4.x, claude-*-haiku/sonnet, ernie-4.5,
	//    mistral-medium-3-5, GLM-5, …) — same ID → same version on every provider.
	//  - (b) PARAM-SIZE GUARD: gpt-oss-120b → Version "" on ALL providers (size is
	//    GH#9, not a version), converging the 120b-vs-"" split.
	//  - (c) GLUED letter-suffix: glm-4.5v → (glm,"",4.5,vision) — cleared the
	//    glm↔glmv version/modifier divergence for the glued form.
	//  - (d) SERIES SPLIT (CLARIFICATION-5): kimi-k2/k2.5/…, minimax-m1/m2.x,
	//    mimo-v2.x → (family, letter, number), incl. the empty-raw compound-family
	//    recovery (kimi-k2-0905 → family kimi). Converged the non-tier series IDs.
	// CLARIFICATION-6 (tier→modifier, variant=pure series-letter): the clean
	// single-tier series IDs (mimo-v2.5-pro, minimax-m2.5-fast/highspeed,
	// kimi-k2-instruct, …) now promote the tier to the Modifier and converge to
	// (family, letter, version): 158 → 155 (CatC 111 → 108). The tier set is
	// series-SCOPED (parse.go seriesTierModifiers), NOT added to global modifiers.json
	// — that would reclassify non-series variants (gpt-5-mini, gemini-2.5-flash,
	// qwen-turbo); verified those stay variant-tokens (CLARIFICATION-6 edge-b).
	// "omni" added to the curated series-tier set (CLARIFICATION-6 residual analysis):
	// it was the only remaining UNKNOWN-tier token still causing a series divergence
	// after the initial tier wiring (mimo-v2-omni → (mimo,'v','2',mod=omni)): 155 → 154
	// (CatC 108 → 107).
	// RESIDUAL (honest, surfaced — NOT masked): the only SERIES (kimi/minimax/mimo)
	// IDs still divergent are the MULTI-MODIFIER cases — a tier AND thinking/vision (or
	// 2+ tiers) in the single-valued Modifier field: kimi-k2-thinking-turbo (×2 paths).
	// The user ruled Option 1 (Modifier → LIST, lossless), but that is a PUBLIC SCHEMA
	// change deferred to SLICE-9 (grep "SLICE-9" / "multi-modifier"). For S8 these keep
	// the series split + the capability modifier (thinking) and DROP the tier.
	// (The other ~150 residual divergences are NON-series family/version mislabels —
	// deepseek-v3.x, magnum, morph, mistral-7b-v0.x, the CatD ledger, qwen3-vl/param —
	// out of SLICE-8's version-path scope.)
	// No previously agreeing ID broke (verified by suite + NoDateVersions=0 +
	// version-only-divergence probe = 0).
	const (
		divergenceExact = 154
		// Secondary sanity band — guards against a wholesale snapshot/pipeline
		// breakage that happens to coincidentally land on a different exact value.
		divergenceLow  = 130
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
	// SLICE-2: bare_gen_split predicate resolved 70 of 73 CatB (59 converged, 11
	// reclassified to CatC). CatB → 3 (residual hy/lyria/rnj, no families.json entry).
	// SLICE-3: ledger l3*→llama folds + thinking/vision modifier migration cleared
	// 13 CatD ledger candidates (44 remain) and converged 9 CatC IDs (212 remain).
	// SLICE-8: ID-driven version + param-size guard + glued-suffix + series split
	// (+ CLARIFICATION-6 tier→modifier) converged 104 CatC IDs (212→108). CatA/B/D
	// unchanged (version-path-only slice).
	const (
		catAExact = 0   // vendor-prefix/case (SLICE-1 M4 resolved all)
		catBExact = 3   // bare-gen-split (SLICE-2 cleared 70/73; residual = bases w/o families.json entry)
		catCExact = 107 // member-variant/version (SLICE-8: −105 via ID-driven version + series split + tier→modifier incl. omni)
		catDExact = 44  // genuine family mislabel (SLICE-3: −13, l3*→llama ledger + thinking/vision)
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
