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
	// change deferred to the later Modifier-LIST slice (SLICE-10; grep "multi-modifier"
	// / "Modifier-LIST"). For S8 these keep the series split + the capability modifier
	// (thinking) and DROP the tier.
	// (The other ~150 residual divergences are NON-series family/version mislabels —
	// deepseek-v3.x, magnum, morph, mistral-7b-v0.x, the CatD ledger, qwen3-vl/param —
	// out of SLICE-8's version-path scope.)
	// No previously agreeing ID broke (verified by suite + NoDateVersions=0 +
	// version-only-divergence probe = 0).
	//
	// SLICE-9 (rc2) PATH-UNIFICATION (CLARIFICATION-8, Option A): ParseFamilyDetailed
	// is now ID-driven for Variant/Version/Modifier (raw_family is a HINT; the
	// idDrivenDecompose primitive is shared by the empty-raw and raw-populated paths
	// via reconcileIDDriven). FAMILY is PRESERVED from raw_family — the diff-first
	// safeguard (TestPathUnification_ZeroUnexpectedRegression) proved the ID-path
	// OVER-captures Family for the <family>-<gen><size>-<variant> shape (deepseek-v4,
	// gpt-4o, llama-3.3-70b …), so converging those 107 FAMILY-over-capture divergences
	// is deferred to the dedicated family-seeding slice (Option B). New baseline:
	// 123 / A=0 / B=3 / C=76 / D=44 (was 154/0/3/107/44). All −31 are CatC member-
	// variant/version convergences: empty-raw and raw-populated providers of the same
	// ID now share one ID-driven Variant/Version/Modifier (glm-5v → (glm,"",5,vision);
	// qwen3.6-flash variant de-junk "3.6"→"flash"; gpt-5.1-codex-mini "codex"→
	// "codex-mini"; version-presence agreement). The committed before/after diff
	// (testdata/snapshot/decomp_diff_report.json) categorizes every change a/b/c with
	// ZERO category-(c) unexpected regressions (1 reviewed justified-exception:
	// gemini-2.5-pro-preview-tts raw mislabel). CatD=44 unchanged — the genuine family
	// mislabel ledger is Option B's scope, not SLICE-9's.
	//
	// SLICE-9 fix-cycle-2 (review A/B/C REVISE → fixed): re-pinned 123→126 / CatC 76→79.
	//  - P1 (Reviewer A): the variant-superstring guard restores the correct, more-
	//    specific raw variant "flash-lite" for gemini-2.5-flash-lite-preview-* (the
	//    ID-path returns less-specific "flash"). Those ~3 IDs therefore STAY divergent
	//    (the empty-raw providers still mis-derive "flash") — an HONEST residual surfaced
	//    for a future ID-path/tier-seeding fix, NOT hidden by downgrading the correct
	//    flash-lite data. This is the +3 vs the (unsound) fix-cycle-1 count of 123.
	//  - P2 (Reviewer C): '@'-form version normalization (claude-…-4-1@20250805 → 4.1,
	//    not "4") removes the newly-introduced cross-form version-VALUE divergence; net
	//    of P1+P2 the 3-tuple analyzer lands at 126 / A=0 / B=3 / C=79 / D=44.
	//
	// SLICE-11 (rc2) family OVER-CAPTURE fix (Option B / CLARIFICATION-9): the ID-path
	// family-SEEDING now reduces an over-captured COMPOUND family to its registered SHORT
	// base (claude-opus→claude, gpt-4o→gpt, deepseek-v4→deepseek, llama-3.3-70b→llama,
	// qwen3-vl-*→qwen, phi-4-mini→phi, …) so the empty-raw and raw-populated providers of
	// the same ID converge on the SAME short family + member-recovered variant/version
	// (reduceOverCapturedFamily, CLOSED over override self-maps + families.json + the
	// allFamilies registry, with an ALL-residue guard and a capability-modifier decline).
	// New baseline: 71 / A=0 / B=2 / C=35 / D=34 (was 126/0/3/79/44). The −55 splits:
	// CatC 79→35 (−44, family over-capture reductions that also converged variant/version),
	// CatD 44→34 (−10, over-captures previously mis-bucketed as genuine mislabels now reduce
	// to the correct short family), CatB 3→2 (−1). The remaining D=34 are GENUINE mislabels
	// requiring the IP-5 ledger + user sign-off (aion/llama, hermes/nousresearch namespace
	// leaks, lfm/liquid, ministral/mistral, pixtral/voxtral/mixtral vs mistral, inflection/gpt,
	// intellect/glm, text-embedding/qwen, qwq/qwen, …) — NOT folded here. The before/after
	// diff (decomp_diff_report.json) categorizes every change with ZERO category-(c)
	// unexpected regressions (1 reviewed justified-exception: qwen3.6-plus-free free→plus).
	// HONEST residuals NOT converged (declined by the closed reducer, surfaced not masked):
	// capability/multi-modifier IDs (kimi-k2-thinking-turbo, llama-3.2-11b-vision,
	// phi-4-multimodal — deferred to the SLICE-10 Modifier-LIST), the glued glmv letter-suffix,
	// raw-populated over-captures (qwen3.7-max), and IDs whose canonical short side is itself
	// lossy/inconsistent (deepseek-chat→deepseek drops "chat"; qwen3-next picks suffix
	// "instruct" not "next").
	// SLICE-12 (rc2) cross-provider convergence fix-cycle (bestiary-b4jm): 68 → 18.
	// The −50 came from: o-series taxonomy restructure (bestiary-xdbc Q2: o1/o3/o4→(gpt,
	// variant=o,ver), gpt-4o→(gpt,4o,""), gpt-audio→(gpt,audio); sanctioned via the reviewed
	// allowlist) + gpt-codex ID-wins phantom-variant clear (8) + glm glued-'v' variant
	// (Q1, glm-4.5v→(glm,v,4.5); glmv→glm+v) + canonical-winner ENFORCE set (own-family +
	// org-namespace leak: aion/magnum/hermes/mixtral/pixtral/voxtral/intellect/qwq/weaver/
	// owl/wizardlm/inflection/ministral + nousresearch→hermes/allenai→olmo/liquid→lfm) +
	// dotted bare-gen de-junk (qwen3.5/3.6) + raw-populated over-capture fold (qwen3.7-max) +
	// member-variant suffix re-recovery (codellama/rnj/mixtral/voxtral/lyria) + flash-lite
	// tier (compound-member recovery, gemini-2.5-flash-lite-preview-*). The decomp diff
	// (decomp_diff_report.json) classifies every change with ZERO category-(c) regressions.
	//
	// RESIDUAL = 18 (HONEST, surfaced — NOT masked). Of these, 5 are the SLICE-10-blocked
	// multi-modifier/capability records (llama-3.2-11b-vision-instruct ×2, phi-4-multimodal-
	// instruct, kimi-k2-thinking-turbo ×2 — a tier AND thinking/vision in the single Modifier
	// slot, deferred to the SLICE-10 Modifier-LIST). The other ~13 are GENUINE stragglers
	// that do NOT cleanly converge and are deliberately left rather than force-converged with
	// a lossy/over-broad hack: deepseek-chat-v3* (the canonical short side drops "chat"),
	// qwen3-next-80b-a3b-instruct ×2 ("next" is an unrecognised over-capture token), nvidia
	// llama-3.3-nemotron-super-49b (an EMBEDDED registered family — the ID leads with "llama"),
	// x-ai/grok-code-fast-1 ("fast" unrecognised), llama-4-scout (over-capture "scout"),
	// tencent/hy3-preview ("hy" not a registered family), Qwen3-Embedding ("text-embedding"
	// is a curated self-map override), meta-llama/Meta-Llama-3.1 (empty-raw derives "meta"
	// from the doubled vendor — fixing it introduced cat-(c) collateral on odd-format records,
	// so reverted/surfaced), command-r-plus/r7b/a-reasoning (over-capture + date-as-version),
	// hermes-2-pro-llama (variant 'pro' on one side only). These warrant a follow-up slice or
	// upstream data fix; see the SLICE-12 worker report.
	//
	// SLICE-14 (rc2) straggler fix-cycle (bestiary-vs61), team-lead-refined set: 18 → 10.
	// GOVERNING PRINCIPLE: converge only when unambiguous under EXISTING ratified rules (family/
	// vendor-strip/gen-split/date-guard/product-name member recovery) with NO modifier-vs-variant
	// taxonomy judgment (that is reserved for S10). 5 COMMITTED + 3 CONDITIONALS all cleanly
	// promoted (best-case 18→10), cat-(c)=0 under the hardened (token-aware, ovf6) gate:
	//  - deepseek-chat-v3-0324 / -v3.1 → (deepseek, chat, …) — "chat" is the DeepSeek product
	//    line, recovered as a deepseek member (non-lossy; v3.1 version preserved via v-prefix).
	//  - command-r-plus-08-2024 → (command, r-plus); command-r7b-12-2024 → (command, r) ["r7b" =
	//    member "r" + param-size "7b"]. R-line members; 08 is an MM-YYYY date (date-guarded).
	//  - meta-llama/Meta-Llama-3.1-8B-Instruct → (llama, instruct, 3.1) — SURGICAL doubled-vendor
	//    strip (org "meta-llama/" + repeated "Meta-Llama-…"); scoped so the broad-"meta"-alias
	//    cat-(c) collateral on odd-format IDs CANNOT recur.
	//  - grok-code-fast-1 → (grok, code-fast, 1) — "code-fast" as ONE product-name member unit
	//    (no fast-as-modifier judgment).
	//  - Qwen/Qwen3-Embedding-* → (qwen, embedding, 3) — the ID-derived real family "qwen" wins
	//    over the generic raw "text-embedding" self-map. GUARDED: OpenAI text-embedding-3-large/
	//    small (whose ID literally IS "text-embedding") keep family "text-embedding" (no collateral).
	//  - tencent/hy3-preview → (hy, "", 3, preview) — bare "hy" gen-split ("hy" attested via raw="Hy").
	// command-r7b's version field still carries the date frag "12" (a shared, pre-existing value
	// on both providers — CONVERGED, not a divergence; surfaced for a future date-guard polish).
	//
	// RESIDUAL = 10 (HONEST). 9 are S10-PENDING (converge once S10's systematic modifier ruling +
	// Modifier-LIST land): kimi-k2-thinking-turbo ×2, llama-3.2-11b-vision-instruct ×2, phi-4-
	// multimodal-instruct (the 5 multi-modifier), command-a-reasoning-08-2025 ("reasoning"=
	// borderline-capability), llama-4-scout + qwen3-next ×2 (line-designator + instruct→modifier).
	// 1 is PERMANENT v0.2.2 LEDGER: nvidia/llama-3.3-nemotron-super-49b-v1.5 (embedded-family — the
	// ID leads with "llama", canonical is "nemotron"; GH-followup). The set-equality enumerated-
	// divergence gate is pinned later (post-S10), not in this slice.
	const (
		divergenceExact = 10
		// Secondary sanity band — guards against a wholesale snapshot/pipeline
		// breakage that happens to coincidentally land on a different exact value.
		divergenceLow  = 6
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
	// SLICE-12: CatC 33→12 (member-variant/version convergences: o-series, member re-recovery,
	// flash-lite, dotted-gen, gpt-codex). CatD 34→5 (enforce-set own-family/org corrections +
	// o-series family fold + qwen3.7-max; residual 5 = qwen3-next ×2, nemotron, text-embedding,
	// meta-llama — genuine stragglers). CatB unchanged at 1.
	// SLICE-14: CatC 12→7 (command r/r-plus member+date-guard, deepseek chat, grok code-fast,
	// hy3 bare-gen). CatD 5→3 (meta-llama strip → llama, Qwen3-Embedding qwen-wins; residual D =
	// qwen3-next ×2 + nemotron). CatB 1→0 (hy3 was the last bare-gen-split residual).
	const (
		catAExact = 0 // vendor-prefix/case (SLICE-1 M4 resolved all)
		catBExact = 0 // bare-gen-split (SLICE-14: −1 via hy3 "hy" gen-split)
		catCExact = 7 // member-variant/version (SLICE-14: command r/r-plus, deepseek chat, grok code-fast)
		catDExact = 3 // genuine family mislabel (SLICE-14: −2 via meta-llama + Qwen3-Embedding; residual = qwen3-next×2 + nemotron)
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
