package main

// PATH-UNIFICATION — before/after decomposition diff harness.
//
// the user chose the ROOT-CAUSE path-unification
// over the surgical patch — make ParseFamilyDetailed FULLY ID-driven (raw_family a
// HINT/FALLBACK only, never overriding an ID-derived value). The risk that choice
// carries is correctness regression: the raw_family="" experiment proved divergence
// drops to 0, but did NOT prove the unified path never WORSENS a currently-correct
// decomposition.
//
// This file is the MANDATORY SAFEGUARD (the de-risking gate):
//
//   - dumpDecomposition() over ALL snapshot records ×
//      (Family,Variant,Version,Modifier); a committed BEFORE baseline
//      (testdata/snapshot/decomp_baseline.tsv) captured PRE-refactor; and the
//      a/b/c categorizer (classifyDecompChange) that labels every change.
//   - TestPathUnification_ZeroUnexpectedRegression — the GATE:
//      loads the frozen BEFORE baseline, computes the live AFTER decomposition,
//      categorizes every change, and asserts category-(c) (UNEXPECTED REGRESSION)
//      is EMPTY. Pre-refactor the diff is empty (trivially green); post-refactor it
//      is the regression surface the reviewers scrutinize.
//   - the refactor + the committed categorized diff report
//      (testdata/snapshot/decomp_diff_report.json).
//
// HONESTY CONTRACT: the BEFORE baseline is FROZEN — it is captured once, pre-refactor,
// and is NEVER regenerated to "make the gate pass". Re-capturing it would mask exactly
// the regressions this slice exists to catch. The capture path is env-gated and skips
// by default so a normal `go test ./...` can never overwrite it.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// decompRecord is the full canonical decomposition of a single (Provider, ID)
// snapshot record. (Provider, ID) is the unique key (a provider's model map is
// keyed by ID upstream). RawFamily is retained for diagnostics — it is the HINT
// the unified path may fall back to, and seeing it next to a change explains why
// the ID-driven path moved the tuple.
type decompRecord struct {
	Provider  bestiary.Provider
	ID        bestiary.ModelID
	RawFamily bestiary.Family
	Family    bestiary.Family
	Variant   string
	Version   string
	// Modifier is a LIST, compared as an ORDER-INDEPENDENT SET.
	Modifier []string
}

// modKey is the canonical, order-independent string key for a modifier list (the
// R1 set-independence anchor): permutations collapse to one key. Mirrors the
// production bestiary modifierKey using the exported CanonicalizeModifiers.
func modKey(mods []string) string {
	c := bestiary.CanonicalizeModifiers(mods)
	if len(c) == 0 {
		return ""
	}
	return strings.Join(c, ",")
}

// modSet returns the modifier tokens as a lookup set (lowercased).
func modSet(mods []string) map[string]struct{} {
	s := make(map[string]struct{}, len(mods))
	for _, m := range mods {
		s[strings.ToLower(m)] = struct{}{}
	}
	return s
}

// inModSet reports whether tok (case-insensitive) is in the modifier list.
func inModSet(mods []string, tok string) bool {
	_, ok := modSet(mods)[strings.ToLower(tok)]
	return ok
}

// decompKey is the stable identity of a record for diffing: (Provider, ID).
type decompKey struct {
	Provider bestiary.Provider
	ID       bestiary.ModelID
}

func (r decompRecord) key() decompKey { return decompKey{r.Provider, r.ID} }

// tuple is the (Family,Variant,Version,Modifier) 4-tuple the diff compares.
type decompTuple struct {
	Family   bestiary.Family
	Variant  string
	Version  string
	Modifier []string
}

func (r decompRecord) tuple() decompTuple {
	return decompTuple{r.Family, r.Variant, r.Version, r.Modifier}
}

// decompCmp is the COMPARABLE projection of a decompTuple (the Modifier list is
// reduced to its order-independent canonical key) so tuples can be used as map keys
// and compared with ==. This is the R1 set-independence guarantee applied to
// the categorizer: a permuted modifier list never reads as a distinct tuple.
type decompCmp struct {
	Family   bestiary.Family
	Variant  string
	Version  string
	Modifier string
}

func (t decompTuple) cmp() decompCmp {
	return decompCmp{t.Family, t.Variant, t.Version, modKey(t.Modifier)}
}

// tupleEqual reports SET-equality of two tuples (modifier compared order-independently).
func tupleEqual(a, b decompTuple) bool { return a.cmp() == b.cmp() }

func (t decompTuple) String() string {
	return fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)", t.Family, t.Variant, t.Version, modKey(t.Modifier))
}

// dumpDecomposition runs the LIVE production decomposition (ParseFamilyDetailed)
// over every snapshot record and returns them sorted by (Provider, ID). This is
// the single source the BEFORE-capture and the AFTER-diff both consume, so the
// two sides are guaranteed apples-to-apples (same loader, same field order).
func dumpDecomposition(t testingTB) []decompRecord {
	t.Helper()
	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("dumpDecomposition: LoadSnapshotRecords: %v", err)
	}
	out := make([]decompRecord, 0, len(records))
	for _, r := range records {
		fam, variant, version, modifier, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		out = append(out, decompRecord{
			Provider:  r.Provider,
			ID:        r.ID,
			RawFamily: r.RawFamily,
			Family:    fam,
			Variant:   variant,
			Version:   version,
			Modifier:  modifier,
		})
	}
	slices.SortFunc(out, func(a, b decompRecord) int {
		if a.Provider != b.Provider {
			return strings.Compare(string(a.Provider), string(b.Provider))
		}
		return strings.Compare(string(a.ID), string(b.ID))
	})
	return out
}

// baselinePath is the committed FROZEN BEFORE baseline (pre-refactor decomposition
// of every snapshot record). TSV is used over JSON: it is the most compact and the
// most git-diff-friendly representation, and none of the fields can contain a tab.
func baselinePath() string {
	return filepath.Join(snapshotDir(), "decomp_baseline.tsv")
}

// baselineHeader is the TSV column header. Order is fixed so the file is stable.
const baselineHeader = "provider\tid\traw_family\tfamily\tvariant\tversion\tmodifier"

func formatBaselineLine(r decompRecord) string {
	// the modifier column is the canonical comma-joined key (order-independent).
	// The FROZEN pre-refactor baseline holds single-token modifiers (no commas).
	return strings.Join([]string{
		string(r.Provider), string(r.ID), string(r.RawFamily),
		string(r.Family), r.Variant, r.Version, modKey(r.Modifier),
	}, "\t")
}

// parseModifierColumn turns the TSV modifier column back into a list (empty → nil).
// The committed FROZEN baseline stores single-token values; a future capture would
// store the canonical comma-joined form — both parse correctly here.
func parseModifierColumn(s string) []string {
	if s == "" {
		return nil
	}
	return bestiary.CanonicalizeModifiers(strings.Split(s, ","))
}

func parseBaselineLine(line string) (decompRecord, error) {
	parts := strings.Split(line, "\t")
	if len(parts) != 7 {
		return decompRecord{}, fmt.Errorf("expected 7 tab-separated fields, got %d: %q", len(parts), line)
	}
	return decompRecord{
		Provider:  bestiary.Provider(parts[0]),
		ID:        bestiary.ModelID(parts[1]),
		RawFamily: bestiary.Family(parts[2]),
		Family:    bestiary.Family(parts[3]),
		Variant:   parts[4],
		Version:   parts[5],
		Modifier:  parseModifierColumn(parts[6]),
	}, nil
}

// writeBaseline serializes records to the FROZEN baseline TSV.
func writeBaseline(records []decompRecord) error {
	var b strings.Builder
	b.WriteString(baselineHeader)
	b.WriteByte('\n')
	for _, r := range records {
		b.WriteString(formatBaselineLine(r))
		b.WriteByte('\n')
	}
	return os.WriteFile(baselinePath(), []byte(b.String()), 0o644)
}

// loadBaseline reads the FROZEN BEFORE baseline TSV.
func loadBaseline() ([]decompRecord, error) {
	f, err := os.Open(baselinePath())
	if err != nil {
		abs, _ := filepath.Abs(baselinePath())
		return nil, fmt.Errorf(
			"loadBaseline: cannot read frozen decomposition baseline at %s: %w\n"+
				"  What: the BEFORE baseline (pre-path-unification decomposition) is missing\n"+
				"  Why: it is captured once via TestCaptureDecompositionBaseline (env-gated) and committed\n"+
				"  How to fix: BESTIARY_CAPTURE_BASELINE=1 go test ./cmd/bestiary-gen -run TestCaptureDecompositionBaseline\n"+
				"       (run this ONLY pre-refactor; re-capturing post-refactor would mask regressions)",
			abs, err)
	}
	defer f.Close()

	var out []decompRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			if line != baselineHeader {
				return nil, fmt.Errorf("loadBaseline: unexpected header %q, want %q", line, baselineHeader)
			}
			continue
		}
		if line == "" {
			continue
		}
		rec, perr := parseBaselineLine(line)
		if perr != nil {
			return nil, fmt.Errorf("loadBaseline: %w", perr)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("loadBaseline: scan: %w", err)
	}
	return out, nil
}

// TestCaptureDecompositionBaseline writes the FROZEN BEFORE baseline. It is the ONLY
// path that writes decomp_baseline.tsv and is env-gated so a normal `go test ./...`
// can never overwrite the frozen artifact (which would defeat the regression gate).
//
//	BESTIARY_CAPTURE_BASELINE=1 go test ./cmd/bestiary-gen -run TestCaptureDecompositionBaseline
//
// Run this ONCE, PRE-refactor (on the HEAD), and commit the result.
func TestCaptureDecompositionBaseline(t *testing.T) {
	if os.Getenv("BESTIARY_CAPTURE_BASELINE") != "1" {
		t.Skip("BESTIARY_CAPTURE_BASELINE != 1 — refusing to overwrite the frozen BEFORE baseline " +
			"(set BESTIARY_CAPTURE_BASELINE=1 to capture, PRE-refactor only)")
	}
	records := dumpDecomposition(t)
	if len(records) == 0 {
		t.Fatal("dumpDecomposition returned 0 records — snapshot empty?")
	}
	if err := writeBaseline(records); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	t.Logf("captured BEFORE baseline: %d records → %s", len(records), baselinePath())
}

// ── a/b/c categorizer ───────────────────────────────────────────────────────
//
// changeCategory labels every BEFORE→AFTER decomposition change. The gate is:
// ZERO category-(c).
type changeCategory int

const (
	// CatFix (a): intended divergence-fix. BEFORE, this record's ID was divergent
	// across providers; AFTER, this record's tuple converges to the cross-provider
	// agreement (the ID-driven canonical value other providers already produced).
	CatFix changeCategory = iota + 1
	// CatImprove (b): intended improvement. Not a divergence convergence, but the
	// AFTER tuple is a strict enrichment of the BEFORE tuple — a previously-empty
	// field (version/variant/modifier) is now populated and the populated fields are
	// preserved (no field's non-empty value was replaced by a different value).
	CatImprove
	// CatRegress (c): UNEXPECTED REGRESSION. A change that is neither a convergence
	// nor a strict enrichment — a non-empty field changed to a DIFFERENT value or was
	// cleared. These are the changes the gate forbids; each requires a targeted
	// raw_family fallback (or proof it is actually intended, reclassified by review).
	CatRegress
)

func (c changeCategory) String() string {
	switch c {
	case CatFix:
		return "a:divergence-fix"
	case CatImprove:
		return "b:improvement"
	case CatRegress:
		return "c:UNEXPECTED-REGRESSION"
	default:
		return "unknown"
	}
}

// decompChange records a single BEFORE→AFTER change for the diff report.
type decompChange struct {
	Provider  bestiary.Provider `json:"provider"`
	ID        bestiary.ModelID  `json:"id"`
	RawFamily bestiary.Family   `json:"raw_family"`
	Before    string            `json:"before"`
	After     string            `json:"after"`
	Category  string            `json:"category"`
	Reason    string            `json:"reason"`
}

// isStrictEnrichment reports whether `after` only FILLS empty fields of `before`
// without changing any field that `before` already populated. This is the
// mechanical (b) predicate.
func isStrictEnrichment(before, after decompTuple) bool {
	fieldPairs := [][2]string{
		{string(before.Family), string(after.Family)},
		{before.Variant, after.Variant},
		{before.Version, after.Version},
	}
	changed := false
	for _, fp := range fieldPairs {
		b, a := fp[0], fp[1]
		if b == a {
			continue
		}
		changed = true
		if b != "" {
			// A populated field changed to a different value (or was cleared) — not
			// an enrichment.
			return false
		}
	}
	// modifier as a SET: an enrichment may only ADD modifiers (before ⊆ after),
	// never drop one. A dropped modifier is a populated-field change → not an enrichment.
	if modKey(before.Modifier) != modKey(after.Modifier) {
		changed = true
		afterSet := modSet(after.Modifier)
		for _, b := range before.Modifier {
			if _, ok := afterSet[strings.ToLower(b)]; !ok {
				return false // a modifier present BEFORE is gone AFTER — not enrichment
			}
		}
	}
	return changed
}

// classifyDecompChange labels a single changed record (a/b/c).
//
// Inputs:
//   - before, after: the tuples for THIS record.
//   - beforeByID, afterByID: all tuples (across providers) for this record's ID,
//     BEFORE and AFTER respectively. Used to detect convergence.
//
// Rules (in order):
//  1. (a) divergence-fix: BEFORE the ID was divergent (≥2 distinct tuples across
//     providers) AND AFTER this record's tuple equals the AFTER-consensus for the ID
//     (all providers now agree, or it now matches the majority/another provider's
//     pre-existing value). The defining property: the change reduced cross-provider
//     disagreement for this ID.
//  2. (b) improvement: a strict enrichment (only fills empty fields).
//  3. (c) regression: everything else (a populated field changed to a different
//     value and it was NOT a convergence toward what other providers already had).
func classifyDecompChange(id bestiary.ModelID, before, after decompTuple, beforeByID, afterByID []decompTuple, allow sanctionedAllowlist) (changeCategory, string) {
	// SANCTIONED o-series taxonomy escape,
	// EXPECTED-TUPLE-MATCHED. An allowlisted raw ID is admitted as cat-(a)-sanctioned
	// ONLY IF its observed AFTER tuple EQUALS the ratified target tuple recorded in the
	// allowlist artifact. ANY OTHER delta on an allowlisted ID (a dropped/changed field,
	// a wrong modifier, an unrelated bug riding the sanctioned escape) FALLS THROUGH to
	// the mechanical classifier and stays a HARD cat-(c). A reassignment whose raw ID is
	// NOT on the allowlist likewise gets no escape here. This is the NO-MASKING gate: you
	// physically cannot hide an unrelated regression on an allowlisted ID under the escape,
	// because the escape is keyed to the exact ratified tuple, not the ID alone.
	if exp, ok := allow[id]; ok {
		if tupleEqual(after, exp) {
			return CatFix, "sanctioned-taxonomy: converged to the ratified o-series target tuple " + exp.String()
		}
		// AUTHORITATIVE for allowlisted IDs: any observed AFTER tuple OTHER than the
		// ratified target is a HARD cat-(c) — short-circuit, do NOT fall through to the
		// mechanical classifier (which might otherwise rubber-stamp an o-series-shaped
		// reassignment as a clean convergence and mask a drifted/buggy tuple). This is
		// what makes the escape EXPECTED-TUPLE-MATCHED rather than ID-blanket.
		return CatRegress, fmt.Sprintf("allowlisted o-series ID did NOT converge to its ratified tuple %s (got %s) — sanctioned escape is tuple-matched, not ID-blanket", exp, after)
	}

	distinct := func(ts []decompTuple) int {
		seen := map[decompCmp]struct{}{}
		for _, t := range ts {
			seen[t.cmp()] = struct{}{}
		}
		return len(seen)
	}
	beforeDivergent := distinct(beforeByID) >= 2
	afterConsistent := distinct(afterByID) == 1

	// Did this record's AFTER tuple already exist among the OTHER providers' BEFORE
	// tuples for this ID? If so, the change moved this record INTO agreement with a
	// value the dataset already considered correct for this ID — a divergence-fix.
	matchedExistingBefore := false
	for _, t := range beforeByID {
		if tupleEqual(t, after) && !tupleEqual(t, before) {
			matchedExistingBefore = true
			break
		}
	}

	// G1: a convergence is only a divergence-FIX when it
	// does NOT empty or DOWNGRADE a field THIS record already had populated. A value-
	// blind "matches a prior sibling tuple = fix" rule let real regressions through:
	// converging onto a pre-existing WRONG value (e.g. flash-lite→flash because a
	// sibling's empty-raw path mis-derived "flash") or clearing a populated field (e.g.
	// deepseek-reasoner thinking→"") both reduced disagreement while WORSENING this
	// record. Reject those here so they fall through to CatRegress (and Option B, whose
	// family convergences are exactly this class, cannot fool the gate).
	//
	// (Option B) splits the directionality guard by field class. A family
	// OVER-CAPTURE reduction (claude-opus → claude + variant "opus", deepseek-r1 →
	// deepseek, llama-3.3-70b → llama) shortens a populated Family field and so trips
	// isFieldDowngrade, yet converging it is a genuine FIX: the over-captured COMPOUND
	// family spuriously glued/split fields (the "-r1", "-70b" param-size, the "instruct"
	// suffix) that the dataset's CANONICAL short decomposition — independently produced
	// by the raw-populated sibling providers of the SAME ID — omits. When the change
	// converges onto such a sibling tuple AND the family moved in the REDUCTION direction,
	// trust it. The ONE thing still rejected is a NON-family field losing SPECIFICITY
	// (before a strict superstring of after — the flash-lite → flash class): that is real
	// data loss, not over-capture noise, so it stays a regression.
	// HARDENING — the over-capture-reduction review IMPORTANT.
	// The earlier CatFix family-reduction branch only checked nonFamilyPrefixDowngrade
	// (prefix-only) and IGNORED field CLEARS, so a family reduction that CLEARED a
	// populated variant a same-ID SIBLING retains was admitted as a fix — safe-by-DATA,
	// not safe-by-CONSTRUCTION. The unified guard below (realNonFamilyLoss) rejects ANY
	// non-family field that lost a REAL (ID-present) value not re-surfaced in another
	// AFTER field — whether by clear OR by lateral/prefix change — in BOTH the family-
	// reduction and the ordinary-convergence branch. A PHANTOM value (provider noise that
	// never appears in the model ID, e.g. raw_family "gpt-codex" tagging a chat ID whose
	// text has no "codex") may be dropped on convergence; an ID-PRESENT value may not
	// (that is the flash-lite / instruct-clear data-loss class). This single guard makes
	// the gpt-codex variant-clear AND the A-1 variant-clear safe-by-CONSTRUCTION.
	familyReduced := familyIsReductionOf(after.Family, before.Family)
	if beforeDivergent && (afterConsistent || matchedExistingBefore) {
		if familyReduced {
			if !realNonFamilyLoss(before, after, id) {
				return CatFix, "family over-capture reduced to registered short base, converged onto an independently-produced sibling tuple (hardened: no ID-present non-family field lost)"
			}
		} else if !familyClearedOrDowngraded(before, after) && !realNonFamilyLoss(before, after, id) {
			reason := "converged divergent ID toward cross-provider agreement (hardened: only phantom non-family loss permitted)"
			if matchedExistingBefore {
				reason = "now matches a tuple another provider already produced for this ID (BEFORE); only phantom non-family loss permitted"
			} else if afterConsistent {
				reason = "all providers for this ID now agree post-unification; only phantom non-family loss permitted"
			}
			return CatFix, reason
		}
	}

	if isStrictEnrichment(before, after) {
		return CatImprove, "strict enrichment — only previously-empty field(s) populated"
	}

	// (b) refinement / de-junk: the ONLY non-empty fields that changed are Family-
	// preserving Variant/Version refinements that improve a populated value:
	//   - variant refinement: after-variant is a superstring of before-variant
	//     (e.g. "codex" → "codex-mini" — the ID names a more specific variant);
	//   - variant de-junk: before-variant was version-shaped junk (the version digits
	//     leaked into the variant, e.g. "3.6") and after-variant is a clean word token
	//     (e.g. "flash") — the ID recovered the true member variant.
	// Family/Version/Modifier must be otherwise unchanged for this rule to apply.
	if before.Family == after.Family && before.Version == after.Version && modKey(before.Modifier) == modKey(after.Modifier) &&
		before.Variant != after.Variant {
		refinement := after.Variant != "" && before.Variant != "" &&
			strings.HasPrefix(after.Variant, before.Variant)
		deJunk := isVersionShapedToken(before.Variant) && !isVersionShapedToken(after.Variant) && after.Variant != ""
		if refinement {
			return CatImprove, fmt.Sprintf("variant refinement: %q → %q (ID names a more specific variant)", before.Variant, after.Variant)
		}
		if deJunk {
			return CatImprove, fmt.Sprintf("variant de-junk: version-shaped %q → clean member variant %q", before.Variant, after.Variant)
		}
	}

	// (b) NON-converging family over-capture reduction (single-provider IDs, or
	// multi-provider IDs whose other providers stay an honest residual). Admitted as an
	// improvement ONLY under an INDEPENDENT, data-grounded test: AFTER's family is a
	// CANONICAL registered family (in the upstream-derived allFamilies registry) while
	// BEFORE's is NOT — i.e. BEFORE was a synthetic over-capture and AFTER is the real
	// short family — AND no NON-family field lost specificity (the flash-lite guard). An
	// over-captured family is always wrong, so reducing it to the canonical short base
	// without downgrading variant/version/modifier is a strict improvement. The registry
	// is curated UPSTREAM data, not the reducer's logic, so this cannot rubber-stamp an
	// arbitrary family rewrite: a genuine mislabel (intellect↔glm, ministral↔mistral) is
	// NOT a reduction-direction shortening and a capability-modifier loss (kimi-k2-thinking)
	// is declined upstream, so neither can slip through here.
	// This NON-converging improvement branch must ALSO refuse to bless a
	// reduction that CLEARS or laterally changes an ID-present non-family field that no
	// AFTER field re-surfaces (e.g. rnj-1→rnj dropping the ID-attested "instruct" a sibling
	// keeps). realNonFamilyLoss subsumes the old prefix-only nonFamilyPrefixDowngrade check.
	if familyReduced && !realNonFamilyLoss(before, after, id) &&
		(familySuffixMovedToVariant(before, after) ||
			(bestiary.IsKnownFamily(after.Family) && !bestiary.IsKnownFamily(before.Family))) {
		return CatImprove, "family over-capture reduced to short base — exact family-suffix→variant move (override semantics), OR before ∉ registry & after ∈ registry; no ID-present non-family field lost (hardened)"
	}

	// (b) CANONICAL-WINNER ENFORCE: a LATERAL family change (not a reduction)
	// whose AFTER family is in the CLOSED enforce set (family_enforce.json) is a SANCTIONED
	// ledger correction — the ID-canonical DISTINCT family (aion/magnum/hermes/mixtral/qwq/
	// intellect/…) beat a parent-family or org-namespace mislabel (raw 'llama'/'mistral'/
	// 'gpt'/'glm'/'qwen'/'nousresearch'/'allenai'/'liquid'). Admitted ONLY when no ID-present
	// non-family field was lost (realNonFamilyLoss) — so a correction that also drops a real
	// variant stays a cat-(c). The enforce set is curated data, not the
	// categorizer's logic, so this cannot rubber-stamp an arbitrary
	// family rewrite (only the ID's own distinct family triggers it in the parser).
	if before.Family != after.Family && bestiary.IsEnforcedCanonicalFamily(after.Family) &&
		!realNonFamilyLoss(before, after, id) {
		return CatImprove, "canonical-winner enforce: ID-derived distinct family won over a parent/org mislabel (family_enforce.json ledger)"
	}

	// (b) GLUED family-suffix → variant fold: the
	// dropped family suffix re-surfaces VERBATIM as the variant, with no hyphen between base
	// and suffix (before.Family == after.Family + after.Variant, e.g. "glmv" == "glm"+"v").
	// Information-preserving (the suffix is relocated, not lost) and no ID-present non-family
	// field lost — a strict improvement (the glm raw-family fold ratified in Q1).
	if after.Variant != "" && string(before.Family) == string(after.Family)+after.Variant &&
		!realNonFamilyLoss(before, after, id) {
		return CatImprove, "glued family-suffix moved to variant (e.g. glmv → glm + variant 'v')"
	}

	// (b) the GENERIC "text-embedding" raw self-map descriptor corrected to the
	// ID-derived REAL family (Qwen/Qwen3-Embedding-* → qwen) — a canonical family correction
	// (the ID names the actual family; "text-embedding" was a generic provider descriptor),
	// the same spirit as the canonical-winner enforce set. Admitted only when the AFTER family
	// is a registered family and no ID-present non-family field was lost. Holds even for the
	// non-divergent single-provider embedding sizes (0.6B/4B). OpenAI's own text-embedding-3*
	// keep family "text-embedding" (idFam==rawFam) and never reach here.
	if strings.EqualFold(string(before.Family), "text-embedding") && before.Family != after.Family &&
		bestiary.IsKnownFamily(after.Family) && !realNonFamilyLoss(before, after, id) {
		return CatImprove, "generic 'text-embedding' descriptor corrected to the ID-derived real family"
	}

	// (b) JUNK-VARIANT removal: a populated variant CLEARED with Family/Version/
	// Modifier otherwise preserved is a de-noise improvement when the cleared value is EITHER
	//   - PHANTOM: absent from the model ID (#4 gpt-codex — raw_family "gpt-codex" tagging a
	//     "-chat" ID with no "codex"); OR
	//   - REDUNDANT: a duplicate of the version that is GENUINELY redundant per the ID (#3
	//     dotted bare-gen — raw_family "qwen3.6"/"glm" leaked the generation "3.6"/"4.7" into
	//     BOTH the variant and version slots; the ID carries that token exactly once).
	// This holds even when the ID is NOT cross-provider divergent (these bare IDs are
	// consistent across providers). An ID-present, non-redundant variant cleared would have
	// been a real loss (realNonFamilyLoss / the convergence branch) and never reach here.
	//
	// the REDUNDANT escape now also consults the ID — it fires only
	// when the value is in the ID AND equals the version (a genuine bare-gen duplicate leak),
	// not on bare structural equality, so a future model that legitimately ID-named two
	// distinct-but-equal tokens could not be silently cleared.
	redundant := before.Variant == before.Version && valueInID(before.Variant, id)
	if before.Variant != after.Variant && after.Variant == "" &&
		before.Family == after.Family && before.Version == after.Version && modKey(before.Modifier) == modKey(after.Modifier) &&
		(!valueInID(before.Variant, id) || redundant) {
		return CatImprove, fmt.Sprintf("junk variant %q cleared: phantom (absent from ID) or redundant (== version, ID-confirmed); de-noise", before.Variant)
	}

	// (b) family-preserving DE-NOISE/ENRICH: the FAMILY is unchanged and NO
	// ID-present non-family field was lost (realNonFamilyLoss=false) — every changed field
	// is either an ENRICHMENT (empty→populated, or a superstring extension) or a PHANTOM
	// loss (a value ABSENT from the model ID). This covers compound changes the single-field
	// branches above miss — notably the glm glued-'v' (Q1): glm-4.5v before (glm,"",4.5,
	// modifier="vision") → after (glm,variant="v",4.5,"") simultaneously ENRICHES the variant
	// ("v") and DROPS the phantom "vision" modifier ("vision" is NOT a substring of the ID
	// "glm-4.5v"). An ID-present value lost would have tripped realNonFamilyLoss and never
	// reach here; the family is preserved; so no currently-correct decomposition is worsened.
	if before.Family == after.Family && !realNonFamilyLoss(before, after, id) {
		return CatImprove, "family-preserving de-noise/enrich: only phantom losses and/or enrichments, no ID-present field lost"
	}

	// (b) SANCTIONED org/over-capture family CORRECTION: the BEFORE family is an
	// UNREGISTERED junk/over-capture string (∉ allFamilies — e.g. "azure", "azure-gpt-4",
	// "meta", "meta-llama-3_3-70b") and the AFTER family is a REGISTERED canonical family
	// (∈ allFamilies — gpt, llama) reached via a curated vendor/provider-prefix strip, AND
	// no ID-present non-family field was lost (realNonFamilyLoss=false). An unregistered
	// family is always wrong, so correcting it to a registered one without dropping any
	// variant/version/modifier is a strict improvement. This is safe-by-construction (it
	// CANNOT bless a wrong rewrite): a genuine mislabel between two REAL families is blocked
	// (before would be ∈ allFamilies), and any token loss is blocked by realNonFamilyLoss.
	// Mirrors the reduction branch's registry test, without requiring a prefix
	// REDUCTION (an org-prefix strip is a lateral, not a leading-reduction, family change).
	if before.Family != after.Family &&
		bestiary.IsKnownFamily(after.Family) && !bestiary.IsKnownFamily(before.Family) &&
		!realNonFamilyLoss(before, after, id) {
		return CatImprove, "unregistered org/over-capture family corrected to a registered family via curated vendor/provider-prefix strip; no ID-present field lost"
	}

	// A divergent ID that converged to a brand-new value (no provider had it BEFORE,
	// not yet fully consistent) but where the change is still a strict enrichment was
	// already caught above. Anything reaching here changed a populated field to a
	// different value without being a convergence — flag as a regression for review.
	return CatRegress, fmt.Sprintf("populated field changed value without converging: before=%s after=%s", before, after)
}

// nonFamilyPrefixDowngrade reports whether any NON-family field (Variant/Version/
// Modifier) lost SPECIFICITY in the BEFORE→AFTER change — before is non-empty and after
// is a strict less-specific PREFIX of it (e.g. variant "flash-lite" → "flash", modifier
// "thinking-turbo" → "thinking"). This is the directionality guard for family
// over-capture convergences: clearing an over-capture-noise field is allowed (the
// canonical short sibling does not carry it either), but a populated field collapsing to
// a less-specific prefix is genuine data loss and stays a regression. A field that is
// CLEARED (after == "") is deliberately NOT counted here — that is the over-capture-noise
// case (e.g. mixtral-8x22b-instruct → mixtral, matching the raw-populated sibling that
// also carries no variant); a field changed to a non-prefix LATERAL value is likewise not
// a specificity loss and is governed by the convergence requirement.
func nonFamilyPrefixDowngrade(before, after decompTuple) bool {
	// the Modifier list is governed as a SET by realNonFamilyLoss (a dropped
	// modifier token is a loss only when ID-present and not re-surfaced); prefix-downgrade
	// is a scalar-field notion, so only Variant/Version participate here.
	pairs := [][2]string{
		{before.Variant, after.Variant},
		{before.Version, after.Version},
	}
	for _, p := range pairs {
		b, a := p[0], p[1]
		if b == "" || a == "" || b == a {
			continue
		}
		if strings.HasPrefix(b, a) {
			return true // before is a strict more-specific superstring of after
		}
	}
	return false
}

// familySuffixMovedToVariant reports whether the BEFORE→AFTER change is exactly a
// family-suffix → variant MOVE: before.Family == after.Family + "-" + after.Variant.
// This is the signature of an override-table compound reduction (claude-opus → claude +
// "opus", magistral-small → magistral + "small", gpt-image → gpt + "image") and is the
// case where BEFORE's family is ITSELF a registered family value (models.dev emits
// "claude-opus" for some providers) so the registry test cannot tell it apart from a
// canonical short family. The exact suffix→variant equality is unambiguous: the dropped
// family token re-appears verbatim as the variant, so no information is lost.
func familySuffixMovedToVariant(before, after decompTuple) bool {
	if after.Variant == "" {
		return false
	}
	return string(before.Family) == string(after.Family)+"-"+after.Variant
}

// valueInID reports whether a lost field value `val` actually appears in the model ID
// (the canonical, vendor-stripped, '@'→'-' normalized form). A value PRESENT in the ID
// is REAL data the decomposition must not drop; a value ABSENT from the ID is provider
// PHANTOM noise (e.g. raw_family "gpt-codex" tagging "gpt-5-chat-latest", whose ID has
// no "codex") that may be cleared on convergence. Substring (not token) match so a
// multi-token value like "flash-lite" is detected in "gemini-2.5-flash-lite-preview".
func valueInID(val string, id bestiary.ModelID) bool {
	if val == "" {
		return false
	}
	s := strings.ToLower(string(id))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "@", "-")
	lv := strings.ToLower(val)
	// Fast path: contiguous substring (handles the common single-token + adjacent cases).
	if strings.Contains(s, lv) {
		return true
	}
	// Foundation: dash/dot-form-INSENSITIVE contiguous check. A genuine
	// numeric version is present in the ID as a CONTIGUOUS run even when its separator
	// differs from the value's — "4.5" appears as "claude-opus-4-5" (dash form). Normalize
	// BOTH '.'→'-' and retest the contiguous substring. This CATCHES a dropped dash-form
	// numeric version (valueInID("4.5","...-4-5-...")==true → realNonFamilyLoss → cat-(c))
	// WITHOUT false-positiving on hermes-style glued generation noise: "2.3" → "2-3" is NOT
	// a contiguous substring of "hermes-3-llama-3-1-70b" (the digits 2 and 3 are non-adjacent),
	// so the FP the alphabetic-only fallback guarded against does not recur. Strictly additive
	// (only ever turns MORE losses real), so it cannot mask a regression.
	sNorm := strings.ReplaceAll(s, ".", "-")
	lvNorm := strings.ReplaceAll(lv, ".", "-")
	if (sNorm != s || lvNorm != lv) && strings.Contains(sNorm, lvNorm) {
		return true
	}
	// TOKEN-AWARE fallback: a MULTI-token lost value whose tokens
	// are NON-CONTIGUOUS in the ID still represents REAL data present in the ID and must NOT
	// be treated as phantom. Split the lost value on [-.] and require EVERY sub-token to be
	// present in the ID's token-set (split on any non-alphanumeric). Closes the masking hole
	// Exploit case: id="gemini-flash-2.0-lite", variant "flash-lite"→"flash" — "flash-lite"
	// is not a contiguous substring, but both "flash" and "lite" ARE in the ID, so dropping
	// "lite" is a REAL loss (cat-(c)), not a phantom de-noise. PURELY ADDITIVE: this only ever
	// turns MORE losses real (never fewer), so it cannot mask a regression.
	valToks := strings.FieldsFunc(lv, func(r rune) bool { return r == '-' || r == '.' })
	if len(valToks) < 2 {
		return false // single-token value already handled by the substring path
	}
	// Scope the token-set fallback to WORD-compound values (flash-lite, deep-research):
	// every sub-token must contain a letter. A NUMERIC/dotted version ("2.3") must NOT use
	// it — its digits ("2","3") often appear scattered in the ID (hermes-2…llama-3) without
	// the version "2.3" being present, which would be a false "real loss". Numeric values
	// rely on the contiguous substring check above (a real version is contiguous in the ID).
	for _, vt := range valToks {
		if !hasLetter(vt) {
			return false
		}
	}
	idToks := strings.FieldsFunc(s, func(r rune) bool { return !isAlnum(r) })
	idSet := make(map[string]struct{}, len(idToks))
	for _, t := range idToks {
		idSet[t] = struct{}{}
	}
	for _, vt := range valToks {
		if _, ok := idSet[vt]; !ok {
			return false
		}
	}
	return true
}

func isAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func hasLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return true
		}
	}
	return false
}

// resurfaces reports whether a value re-appears in another AFTER field — the token was
// RELOCATED, not lost (e.g. a family suffix moved into the variant slot). Also recognises a
// VERSION-SHAPED value relocating to the Version slot with its leading version-marker letter
// stripped (e.g. a junk variant "v3.1" de-junked to version "3.1" — the 'v' is a prefix
// marker, not data).
func resurfaces(val string, after decompTuple) bool {
	if val == "" {
		return false
	}
	// a value that relocated INTO the after-Modifier SET counts as re-surfaced
	// (this is the movedToModifier sanctioned-non-loss path: a variant token like
	// "instruct" moving variant→modifier is a lateral move, not a drop).
	if after.Variant == val || after.Version == val || inModSet(after.Modifier, val) || string(after.Family) == val {
		return true
	}
	// Version-shaped value whose numeric part (leading letters stripped) equals the version.
	if after.Version != "" {
		stripped := strings.TrimLeft(strings.ToLower(val), "abcdefghijklmnopqrstuvwxyz")
		if stripped == after.Version {
			return true
		}
	}
	return false
}

// realNonFamilyLoss reports whether the BEFORE→AFTER change LOSES REAL information in a
// NON-family field (Variant/Version/Modifier): a populated old value was CLEARED, or
// replaced by a non-enriching value, AND that old value is PRESENT in the model ID AND
// did not re-surface verbatim in another AFTER field. This is the unified gpt-codex
// guard. An enrichment (AFTER extends BEFORE as a superstring prefix) is never a loss; a
// PHANTOM loss (old value absent from the ID) is never a loss. The FAMILY field is
// governed separately by the over-capture reduction path (familyIsReductionOf), whose
// dropped suffix is canonical over-capture noise — so realNonFamilyLoss deliberately
// inspects only Variant/Version/Modifier.
func realNonFamilyLoss(before, after decompTuple, id bestiary.ModelID) bool {
	type fld struct {
		b, a      string
		isVersion bool
	}
	// Scalar fields (Variant/Version). The Modifier LIST is handled separately below.
	pairs := []fld{
		{before.Variant, after.Variant, false},
		{before.Version, after.Version, true},
	}
	for _, p := range pairs {
		b, a := p.b, p.a
		if b == "" || b == a {
			continue
		}
		if a != "" && strings.HasPrefix(a, b) {
			continue // enrichment: AFTER is a more-specific superstring of BEFORE
		}
		// variant-suffix→modifier SPLIT (not a loss): the before-variant is the
		// after-variant plus one or more trailing hyphen tokens that ALL re-surfaced in the
		// after-Modifier set (e.g. variant "v2.5-turbo" → variant "v2.5" + modifier [turbo]).
		// The dropped suffix relocated, nothing lost.
		if !p.isVersion && a != "" && strings.HasPrefix(b, a+"-") {
			rest := strings.Split(strings.TrimPrefix(b, a+"-"), "-")
			allMoved := true
			for _, tok := range rest {
				if !inModSet(after.Modifier, tok) {
					allMoved = false
					break
				}
			}
			if allMoved {
				continue
			}
		}
		// a CLEARED Version that is the MM of an MM-YYYY DATE in the ID
		// (command-r-plus-08-2024 → "08", command-r7b-12-2024 → "12") is NOT a real version
		// loss — it is a spurious date-fragment leak the parser correctly date-guards to "".
		// (The raw-populated siblings already carry version "" for the same reason.)
		if p.isVersion && a == "" && isDateFragmentInID(b, id) {
			continue
		}
		// b was cleared or replaced by a non-enriching value. resurfaces() now also
		// recognises a token relocated INTO the after-Modifier set (the
		// variant→modifier move), so a sanctioned reclassification is NOT a loss.
		if valueInID(b, id) && !resurfaces(b, after) {
			return true // ID-present value lost without re-surfacing → REAL loss
		}
	}
	// Modifier SET loss: a modifier token present BEFORE but absent from the
	// after-Modifier set is a REAL loss only when it is ID-present AND did not re-surface
	// in another after field. (Phantom modifiers — e.g. the glm "vision" not in the ID —
	// may be dropped; the union semantics mean this normally never fires.)
	afterModSet := modSet(after.Modifier)
	for _, b := range before.Modifier {
		if _, ok := afterModSet[strings.ToLower(b)]; ok {
			continue // still present
		}
		if valueInID(b, id) && !resurfaces(b, after) {
			return true
		}
	}
	return false
}

// movedToModifier reports whether `val` is a SANCTIONED variant/version→modifier lateral
// move: it is absent from after.Variant AND after.Version but PRESENT in the after-Modifier
// SET. This is the R2 predicate that distinguishes a reclassification (cat-a/b,
// non-loss) from a genuine drop. It is ENFORCEMENT-checked by the adversarial mutation test
// (TestPathUnification_ModifierMove_MutationProof): delete the token from after.Modifier and
// the move is no longer sanctioned, so the categorizer must flip the record to cat-(c).
func movedToModifier(val string, after decompTuple) bool {
	if val == "" {
		return false
	}
	if after.Variant == val || after.Version == val {
		return false
	}
	return inModSet(after.Modifier, val)
}

// isDateFragmentInID reports whether ver appears in id as the MM of an MM-YYYY date
// (e.g. "08" in "command-r-plus-08-2024"). Only bare 1-2 digit numeric tokens qualify;
// a real dotted/longer version ("3.5", "2024") never matches this shape.
func isDateFragmentInID(ver string, id bestiary.ModelID) bool {
	if len(ver) == 0 || len(ver) > 2 {
		return false
	}
	for _, r := range ver {
		if r < '0' || r > '9' {
			return false
		}
	}
	s := strings.ToLower(string(id))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	re := regexp.MustCompile(`(?:^|[^0-9])` + ver + `-(?:19|20)\d\d(?:[^0-9]|$)`)
	return re.MatchString(s)
}

// familyClearedOrDowngraded reports whether the FAMILY field was CLEARED (X→"") or
// PREFIX-DOWNGRADED outside the sanctioned over-capture reduction path. A lateral family
// change (mistral→mixtral, the own-family-enforce ledger class) is NOT a downgrade and is
// permitted on convergence; a family CLEAR or a prefix-shortening that is not a registered
// reduction is rejected.
func familyClearedOrDowngraded(before, after decompTuple) bool {
	if before.Family == after.Family {
		return false
	}
	if after.Family == "" {
		return true // cleared
	}
	bf := strings.ToLower(string(before.Family))
	af := strings.ToLower(string(after.Family))
	if len(af) < len(bf) && strings.HasPrefix(bf, af) {
		return true // prefix-shortening (handled as a reduction in the other branch; here = downgrade)
	}
	return false
}

// sanctionedAllowlist maps a raw model ID to its EXPECTED post-restructure (ratified)
// decomposition tuple. It is the reviewed artifact that makes the
// o-series taxonomy restructure safe-by-construction: the ONLY sanctioned escape from
// cat-(c), and only when the observed AFTER tuple EQUALS the expected tuple for that ID.
type sanctionedAllowlist map[bestiary.ModelID]decompTuple

// sanctionedAllowlistEntry is the on-disk JSON shape of one allowlist row.
type sanctionedAllowlistEntry struct {
	Family   string `json:"family"`
	Variant  string `json:"variant"`
	Version  string `json:"version"`
	Modifier string `json:"modifier"`
}

// sanctionedAllowlistFile is the committed allowlist artifact.
type sanctionedAllowlistFile struct {
	Cite    string                              `json:"_cite"`
	Comment string                              `json:"_comment"`
	Entries map[string]sanctionedAllowlistEntry `json:"entries"`
}

func sanctionedAllowlistPath() string {
	return filepath.Join(snapshotDir(), "sanctioned_oseries_allowlist.json")
}

// loadSanctionedAllowlist reads the committed, reviewed o-series allowlist artifact.
func loadSanctionedAllowlist() (sanctionedAllowlist, error) {
	body, err := os.ReadFile(sanctionedAllowlistPath())
	if err != nil {
		return nil, fmt.Errorf("loadSanctionedAllowlist: cannot read %s: %w\n"+
			"  What: the sanctioned o-series taxonomy allowlist (raw ID → ratified tuple) is missing\n"+
			"  Why: it is the reviewed artifact that blesses the o-series restructure\n"+
			"  How to fix: ensure cmd/bestiary-gen/testdata/snapshot/sanctioned_oseries_allowlist.json is committed",
			sanctionedAllowlistPath(), err)
	}
	var f sanctionedAllowlistFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("loadSanctionedAllowlist: parse %s: %w", sanctionedAllowlistPath(), err)
	}
	out := make(sanctionedAllowlist, len(f.Entries))
	for id, e := range f.Entries {
		out[bestiary.ModelID(id)] = decompTuple{bestiary.Family(e.Family), e.Variant, e.Version, parseModifierColumn(e.Modifier)}
	}
	return out, nil
}

// familyIsReductionOf reports whether `short` is a strict LEADING reduction of `long`
// — i.e. long is "short" plus a trailing member/generation token. Covers the hyphen
// form ("claude-opus" ⊃ "claude") and the glued form ("qwen3" ⊃ "qwen", "gpt4o" ⊃
// "gpt"). The token-loss check in isFamilyReductionPreserving is the real guard; this
// only enforces the family moved in the reduction DIRECTION (short ⊂ long), so a
// sideways family swap (mistral→ministral) can never qualify.
func familyIsReductionOf(short, long bestiary.Family) bool {
	s := strings.ToLower(string(short))
	l := strings.ToLower(string(long))
	if s == "" || s == l || !strings.HasPrefix(l, s) {
		return false
	}
	// The char after the prefix must be a boundary (hyphen) or a digit (glued
	// generation), never a continuing letter (so "command" is not a reduction of
	// "commander").
	c := l[len(s)]
	return c == '-' || (c >= '0' && c <= '9')
}

// isFamilyReductionPreserving reports whether a BEFORE→AFTER change is an
// INFORMATION-PRESERVING family OVER-CAPTURE reduction: the family
// became a less-specific REGISTERED short base (after.Family ⊂ before.Family) AND no
// information was lost — every sub-token present in BEFORE still appears in AFTER (the
// dropped family suffix re-surfaces as the variant/version/modifier).
//
// isVersionShapedToken reports whether a token is version-shaped (digits with
// optional '.'/'-' separators and embedded/trailing letters). Used to detect a
// version digit that leaked into the Variant field (e.g. "3.6").
func isVersionShapedToken(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case r == '.' || r == '-':
		case r >= 'a' && r <= 'z':
		default:
			return false
		}
	}
	return hasDigit
}

// diffReport is the committed categorized BEFORE→AFTER artifact.
type diffReport struct {
	TotalRecords   int `json:"total_records"`
	ChangedCount   int `json:"changed_count"`
	CatFixCount    int `json:"cat_a_divergence_fix_count"`
	CatImprove     int `json:"cat_b_improvement_count"`
	CatRegress     int `json:"cat_c_unexpected_regression_count"`
	JustifiedCount int `json:"justified_exception_count"`
	// DivergenceBefore/After count cross-provider divergent IDs over the 4-TUPLE INCLUDING
	// Modifier (Family,Variant,Version,Modifier): 'before' = frozen baseline,
	// 'after' = current live snapshot. This is a DIFFERENT, broader metric than the
	// authoritative cross-provider gate divergenceExact (snapshot_analysis_test.go), which is
	// the 3-TUPLE EXCLUDING Modifier on the current snapshot (=0). The explicit json keys +
	// DivergenceMetric descriptor below exist so a reader can never conflate the two.
	DivergenceBefore int            `json:"id_tuple4_incl_modifier_divergence_frozen_before"`
	DivergenceAfter  int            `json:"id_tuple4_incl_modifier_divergence_current_after"`
	DivergenceMetric string         `json:"divergence_metric"`
	Changes          []decompChange `json:"changes"`
}

// computeDiff loads the frozen BEFORE baseline, computes the live AFTER decomposition,
// and returns the categorized change list plus before/after divergence counts.
func computeDiff(t *testing.T) ([]decompChange, int, int, int) {
	t.Helper()
	before, err := loadBaseline()
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}
	after := dumpDecomposition(t)

	allow, err := loadSanctionedAllowlist()
	if err != nil {
		t.Fatalf("computeDiff: %v", err)
	}

	beforeByKey := make(map[decompKey]decompRecord, len(before))
	beforeByID := make(map[bestiary.ModelID][]decompTuple)
	for _, r := range before {
		beforeByKey[r.key()] = r
		beforeByID[r.ID] = append(beforeByID[r.ID], r.tuple())
	}
	afterByKey := make(map[decompKey]decompRecord, len(after))
	afterByID := make(map[bestiary.ModelID][]decompTuple)
	for _, r := range after {
		afterByKey[r.key()] = r
		afterByID[r.ID] = append(afterByID[r.ID], r.tuple())
	}

	var changes []decompChange
	for _, ar := range after {
		br, ok := beforeByKey[ar.key()]
		if !ok {
			// Record present in AFTER but not BEFORE — snapshot drift, not expected.
			t.Errorf("computeDiff: record %s/%s present in AFTER but missing from frozen baseline "+
				"(snapshot changed since capture?)", ar.Provider, ar.ID)
			continue
		}
		if tupleEqual(br.tuple(), ar.tuple()) {
			continue
		}
		cat, reason := classifyDecompChange(ar.ID, br.tuple(), ar.tuple(), beforeByID[ar.ID], afterByID[ar.ID], allow)
		changes = append(changes, decompChange{
			Provider:  ar.Provider,
			ID:        ar.ID,
			RawFamily: ar.RawFamily,
			Before:    br.tuple().String(),
			After:     ar.tuple().String(),
			Category:  cat.String(),
			Reason:    reason,
		})
	}
	slices.SortFunc(changes, func(a, b decompChange) int {
		if a.Category != b.Category {
			return strings.Compare(a.Category, b.Category)
		}
		if a.ID != b.ID {
			return strings.Compare(string(a.ID), string(b.ID))
		}
		return strings.Compare(string(a.Provider), string(b.Provider))
	})

	divBefore := countDivergentIDs(beforeByID)
	divAfter := countDivergentIDs(afterByID)
	return changes, divBefore, divAfter, len(after)
}

// countDivergentIDs counts multi-provider IDs whose tuples are not all identical.
func countDivergentIDs(byID map[bestiary.ModelID][]decompTuple) int {
	n := 0
	for _, ts := range byID {
		if len(ts) < 2 {
			continue
		}
		seen := map[decompCmp]struct{}{}
		for _, t := range ts {
			seen[t.cmp()] = struct{}{}
		}
		if len(seen) >= 2 {
			n++
		}
	}
	return n
}

// TestPathUnification_ZeroUnexpectedRegression is THE GATE (mandatory
// safeguard). It diffs the FROZEN pre-refactor BEFORE baseline against the LIVE AFTER
// decomposition, categorizes every change (a/b/c), writes the committed categorized
// diff report, and asserts ZERO category-(c) UNEXPECTED REGRESSIONS.
//
// Pre-refactor (baseline == live) the diff is empty → trivially green. Post-refactor
// the categorized changes are the regression surface the per-slice reviewers scrutinize.
// exceptionKey identifies a SPECIFIC intended decomposition change by the exact
// (ID, before-tuple, after-tuple). Keying on all three
// rather than ID alone means the ledger justifies ONLY the precise change that was
// reviewed — if a future pipeline change makes the same ID transition to a DIFFERENT
// (and unreviewed) tuple, the ledger no longer absorbs it and the gate fails.
type exceptionKey struct {
	ID     bestiary.ModelID
	Before string
	After  string
}

// justifiedExceptions is the ENUMERATED, REVIEWED ledger of intended decomposition
// changes that the mechanical a/b/c classifier flags as category-(c) but that human
// review has confirmed are genuine fixes/improvements (not regressions). Each entry
// carries a one-line justification. This mirrors the snapshot gate's "enumerated
// justified exception ledger" philosophy: the gate fails on any category-(c) change
// NOT in this ledger, so new regressions are never silently absorbed. ADDING an entry
// is a reviewed decision (committed in the diff artifact).
var justifiedExceptions = map[exceptionKey]string{
	// The 2 earlier DORMANT keys
	// (gemini-2.5-pro-preview-tts, qwen3.6-plus-free) were PRUNED — they no longer fire a
	// live change record against the current snapshot, so they were dead ledger weight.
	// The map now holds ONLY the live entries.

	// ── modifier-taxonomy reclassification collateral ──────────────
	// The ratified taxonomy moves {instruct, turbo, base} from variant_suffixes.json to
	// global modifiers (modifiers.json). For these NON-divergent, single-/few-provider IDs
	// on UNREGISTERED or over-captured families, the reclassification surfaces a residual
	// the mechanical classifier flags as cat-(c). Each is REVIEWED below; NONE is one of the
	// 9 convergence stragglers and NONE introduces a cross-provider divergence (the
	// divergent set is exactly {nvidia/llama-3.3-nemotron-super-49b-v1.5}).
	//
	// USER-RATIFIED — the final ledger is exactly the user-
	// sanctioned NON-defects. Each is non-divergent and honest; resolving it is disproportionate
	// (∉ allFamilies with no fold target, or a structural Variant-LIST relaxation), so the user
	// ratified them as documented non-defects for v0.2.2. 'instruct' re-surfaces in the Modifier
	// list (no token loss); the categorizer flags only the family-root / variant-slot residual.
	{
		ID:     "abacusai/dracarys-llama-3_1-70b-instruct",
		Before: `(family="dracarys-llama-3_1-70b",variant="instruct",version="",modifier="")`,
		After:  `(family="dracarys",variant="",version="",modifier="instruct")`,
	}: "USER-RATIFIED non-defect (GH#11 model-lineage): dracarys is a llama fine-tune with its own lineage, ∉ allFamilies; 'instruct'→Modifier re-surfaces (no token loss). Family-root precision is a taxonomy call, not a regression.",
	{
		ID:     "upstage/solar-10_7b-instruct",
		Before: `(family="solar-10_7b",variant="instruct",version="",modifier="")`,
		After:  `(family="solar",variant="",version="",modifier="instruct")`,
	}: "USER-RATIFIED non-defect (future taxonomy): solar ∉ allFamilies and has no fold target (only attested as solar-mini/solar-pro); 'instruct'→Modifier re-surfaces (no token loss). 10.7b=size GH#9.",
	{
		ID:     "grok-3-mini-fast-beta",
		Before: `(family="grok-3-mini-fast",variant="beta",version="3",modifier="")`,
		After:  `(family="grok",variant="mini",version="3",modifier="")`,
	}: "USER-RATIFIED non-defect (GH#13 release-stage dimension): family corrected to grok + real tier 'mini' (+version 3); the release-stage token 'beta' AND the speed-tier token 'fast' are BOTH dropped — neither can co-occupy the single Variant slot with 'mini' (variant-multiplicity, the variant analogue of the Modifier-LIST, deferred). MECHANISM (corrected) for the related xai/grok-4.20-non-reasoning-beta, which stays Modifier=nil (nil both BEFORE and AFTER — no regression, no gate trip): the trailing 'beta' is the TAIL-ORDER MODIFIER-SCAN BOUNDARY — the scan starts at the tail and halts at 'beta' (a non-collected variant/boundary token) before reaching the inner 'non-reasoning', so the negation branch never executes. Contrast (verified): grok-4.20-non-reasoning [tail='reasoning' preceded by 'non']→[non-reasoning]; grok-4-20-beta-0309-non-reasoning ['beta' MID-string, tail still 'reasoning']→[non-reasoning]; xai/grok-4.20-non-reasoning-beta [tail='beta']→nil. Same release-stage multiplicity tracked under GH#13.",
	{
		ID:     "azure-gpt-4-turbo",
		Before: `(family="azure-gpt-4",variant="turbo",version="4",modifier="")`,
		After:  `(family="azure-gpt",variant="",version="4",modifier="turbo")`,
	}: "NanoGPT reseller id: the leading 'azure-' is a backend-host label (NanoGPT routes to Azure-hosted OpenAI models), NOT a redundant provider prefix — the genuine Azure provider is the separate azure-cognitive-services namespace. The earlier provider-prefix strip that forced these to the gpt family was removed because it deleted the azure-host signal. These now decompose natively to an imperfect 'azure-*' family, pending a serving-host/backend dimension (GH#16). The before/after differs only in how that imperfect family splits 'turbo'/'4'; neither tuple is a meaningful model decomposition, so this is not a model-identity regression.",
	// RESOLVED & de-ledgered:
	//  • nvidia/llama-3.3-nemotron-super-49b-v1.5 — folded to nemotron via idFamilyOverrides
	//    (cross-provider divergence 1→0).
	//  • meta-llama-3_3-70b-instruct + Meta-Llama-3-1-…-FP8 — no-slash doubled-vendor strip →
	//    llama (version preserved); native family-correction (cat-(b)).
	// Also RESOLVED & de-ledgered:
	//  • whisper-large-v3-turbo / seed-oss-36b-instruct — registered whisper(large)/seed(oss)
	//    families (∈ allFamilies, attested) → lossless variants, now cat-(b).
	//  • elevenlabs/elevenlabs-v2.5-turbo — lossless variant-suffix split (v2.5-turbo →
	//    variant v2.5 + modifier [turbo]); realNonFamilyLoss now recognises the split, cat-(b).
}

func TestPathUnification_ZeroUnexpectedRegression(t *testing.T) {
	changes, divBefore, divAfter, total := computeDiff(t)

	var fix, improve, regress, justified int
	var regressions []decompChange
	for i := range changes {
		c := &changes[i]
		switch c.Category {
		case CatFix.String():
			fix++
		case CatImprove.String():
			improve++
		case CatRegress.String():
			if rationale, ok := justifiedExceptions[exceptionKey{c.ID, c.Before, c.After}]; ok {
				// Reviewed & justified — reclassify so it is not counted as a regression.
				c.Category = "justified-exception"
				c.Reason = "JUSTIFIED: " + rationale
				justified++
			} else {
				regress++
				regressions = append(regressions, *c)
			}
		}
	}

	report := diffReport{
		TotalRecords:     total,
		ChangedCount:     len(changes),
		CatFixCount:      fix,
		CatImprove:       improve,
		CatRegress:       regress,
		JustifiedCount:   justified,
		DivergenceBefore: divBefore,
		DivergenceAfter:  divAfter,
		DivergenceMetric: "4-tuple INCL Modifier (Family,Variant,Version,Modifier); before=frozen baseline, after=current snapshot. DISTINCT from the authoritative cross-provider divergenceExact gate (3-tuple, EXCL Modifier, current snapshot, =0).",
		Changes:          changes,
	}

	t.Logf("=== path-unification before/after diff ===")
	t.Logf("records=%d  changed=%d  (a)divergence-fix=%d  (b)improvement=%d  (c)REGRESSION=%d  justified-exception=%d",
		total, len(changes), fix, improve, regress, justified)
	t.Logf("divergence: before=%d  after=%d", divBefore, divAfter)

	// M2: only persist the committed artifact when the gate
	// PASSES. A failing run must NOT leave a dirty/mismatched report in the working tree
	// (which would pollute git status and could mask the failure under a re-commit).
	reportPath := filepath.Join(snapshotDir(), "decomp_diff_report.json")
	if regress == 0 {
		reportBytes, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatalf("marshal diff report: %v", err)
		}
		if err := os.WriteFile(reportPath, append(reportBytes, '\n'), 0o644); err != nil {
			t.Fatalf("write diff report: %v", err)
		}
		t.Logf("committed report → %s", reportPath)
	}

	if regress != 0 {
		t.Errorf("GATE FAILED: %d category-(c) UNEXPECTED REGRESSION(s) — must be ZERO.\n"+
			"  What: a populated decomposition field changed to a different value without converging\n"+
			"        a divergent ID (i.e. a currently-correct decomposition was WORSENED)\n"+
			"  Why: the path-unification requires zero unexpected regressions; it\n"+
			"       must be family-preserving and monotonic on Variant/Version/Modifier\n"+
			"  How to fix: add a targeted raw_family fallback for the flagged case in reconcileIDDriven,\n"+
			"       OR (if the change is actually intended) justify it and reclassify\n"+
			"  Regressions:", regress)
		for _, c := range regressions {
			t.Errorf("    %s/%s raw=%q  %s → %s  [%s]", c.Provider, c.ID, c.RawFamily, c.Before, c.After, c.Reason)
		}
	}
}

// TestClassifyDecompChange_RejectsDowngrade is the G1 unit: a
// convergence that EMPTIES or DOWNGRADES a populated field must classify as
// CatRegress, NOT CatFix — even when the worse value matches a sibling provider's
// prior tuple (the value-blind blind spot the review exploited).
func TestClassifyDecompChange_RejectsDowngrade(t *testing.T) {
	cases := []struct {
		desc       string
		id         bestiary.ModelID
		before     decompTuple
		after      decompTuple
		beforeByID []decompTuple
		afterByID  []decompTuple
		want       changeCategory
	}{
		{
			desc: "thinking modifier CLEARED while the ID DOES carry 'thinking', converging onto a sibling's empty-modifier tuple → REGRESS (ID-present value lost)",
			// The distinguishing signal is whether the lost value is in
			// the ID. Here 'thinking' IS in the ID, so clearing it is REAL data loss.
			id:         "deepseek-v3-thinking",
			before:     decompTuple{"deepseek", "", "", []string{"thinking"}},
			after:      decompTuple{"deepseek", "", "", nil},
			beforeByID: []decompTuple{{"deepseek", "", "", []string{"thinking"}}, {"deepseek", "", "", nil}},
			afterByID:  []decompTuple{{"deepseek", "", "", nil}, {"deepseek", "", "", nil}},
			want:       CatRegress,
		},
		{
			desc:       "flash-lite DOWNGRADED to less-specific flash, converging onto a sibling's flash tuple → REGRESS (lite is in the ID)",
			id:         "gemini-2.5-flash-lite-preview-09-2025",
			before:     decompTuple{"gemini", "flash-lite", "2.5", nil},
			after:      decompTuple{"gemini", "flash", "2.5", nil},
			beforeByID: []decompTuple{{"gemini", "flash-lite", "2.5", nil}, {"gemini", "flash", "2.5", nil}},
			afterByID:  []decompTuple{{"gemini", "flash", "2.5", nil}, {"gemini", "flash", "2.5", nil}},
			want:       CatRegress,
		},
		{
			// ADVERSARIAL exploit — the lost value
			// "flash-lite" is NOT a contiguous substring of "gemini-flash-2.0-lite", but both
			// "flash" and "lite" ARE present (non-contiguous). The pre-S14 substring valueInID
			// returned false → masked the dropped "lite" as cat-(b) de-noise. The token-aware
			// valueInID detects both tokens → REAL loss → cat-(c).
			desc:       "NON-CONTIGUOUS multi-token loss (flash-lite→flash, tokens split by '2.0' in ID) → REGRESS (token-aware ovf6)",
			id:         "gemini-flash-2.0-lite",
			before:     decompTuple{"gemini", "flash-lite", "2.0", nil},
			after:      decompTuple{"gemini", "flash", "2.0", nil},
			beforeByID: []decompTuple{{"gemini", "flash-lite", "2.0", nil}, {"gemini", "flash", "2.0", nil}},
			afterByID:  []decompTuple{{"gemini", "flash", "2.0", nil}, {"gemini", "flash", "2.0", nil}},
			want:       CatRegress,
		},
		{
			desc: "gpt-codex PHANTOM variant CLEARED (codex absent from the chat ID), converging onto a sibling that already had variant='' → FIX",
			// 'codex' is provider phantom noise — it is
			// NOT in 'gpt-5-chat-latest', so dropping it loses no real information. This is
			// the case the absent-from-ID construction rule blesses (vs flash-lite above).
			id:         "gpt-5-chat-latest",
			before:     decompTuple{"gpt", "codex", "5", []string{"latest"}},
			after:      decompTuple{"gpt", "", "5", []string{"latest"}},
			beforeByID: []decompTuple{{"gpt", "codex", "5", []string{"latest"}}, {"gpt", "", "5", []string{"latest"}}},
			afterByID:  []decompTuple{{"gpt", "", "5", []string{"latest"}}, {"gpt", "", "5", []string{"latest"}}},
			want:       CatFix,
		},
		{
			desc:       "genuine version-presence convergence (empty→4.1, no downgrade) → FIX",
			id:         "claude-opus-4-1",
			before:     decompTuple{"claude", "opus", "", nil},
			after:      decompTuple{"claude", "opus", "4.1", nil},
			beforeByID: []decompTuple{{"claude", "opus", "", nil}, {"claude", "opus", "4.1", nil}},
			afterByID:  []decompTuple{{"claude", "opus", "4.1", nil}, {"claude", "opus", "4.1", nil}},
			want:       CatFix,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, reason := classifyDecompChange(tc.id, tc.before, tc.after, tc.beforeByID, tc.afterByID, nil)
			if got != tc.want {
				t.Errorf("classifyDecompChange = %s (%s), want %s", got, reason, tc.want)
			}
		})
	}
}

// TestPathUnification_CrossIDFormConsistency is the G2 cross-ID-FORM probe:
// the per-exact-ID gate never compares the '@'-delimited
// form of a model against its '-' form, so a same-model version-VALUE divergence
// across ID-forms is invisible to it. This probe decomposes every snapshot record
// whose ID contains '@' BOTH as-is and with '@'→'-' and asserts the canonical
// (Family,Variant,Version) agree — i.e. the '@'-form converges to the canonical form.
func TestPathUnification_CrossIDFormConsistency(t *testing.T) {
	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("LoadSnapshotRecords: %v", err)
	}
	atForms := 0
	for _, r := range records {
		if !strings.Contains(string(r.ID), "@") {
			continue
		}
		atForms++
		hyphen := bestiary.ModelID(strings.ReplaceAll(string(r.ID), "@", "-"))
		af, av, aver, _, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		hf, hv, hver, _, _ := bestiary.ParseFamilyDetailed(r.RawFamily, hyphen, r.Provider)
		if af != hf || av != hv || aver != hver {
			t.Errorf("cross-ID-form divergence for %s (provider %s, raw=%q):\n"+
				"  '@'-form  %q → (family=%q,variant=%q,version=%q)\n"+
				"  '-'-form  %q → (family=%q,variant=%q,version=%q)\n"+
				"  the '@'-form must converge to the canonical '-'-form decomposition",
				r.ID, r.Provider, r.RawFamily, r.ID, af, av, aver, hyphen, hf, hv, hver)
		}
	}
	if atForms == 0 {
		t.Skip("no '@'-form IDs in snapshot — probe vacuous")
	}
	t.Logf("cross-ID-form probe: %d '@'-form records, all converge to canonical '-'-form", atForms)
}

// TestSLICE11_CategorizerPredicates unit-tests the categorizer extensions in
// isolation: the family-reduction direction guard, the non-family prefix-downgrade guard
// (which still catches the flash-lite class), and the exact family-suffix→variant move.
func TestSLICE11_CategorizerPredicates(t *testing.T) {
	t.Run("familyIsReductionOf", func(t *testing.T) {
		yes := [][2]string{{"claude", "claude-opus"}, {"qwen", "qwen3-vl-72b"}, {"gpt", "gpt-4o"}, {"llama", "llama-3.3-70b"}}
		no := [][2]string{{"claude", "claude"}, {"mistral", "ministral"}, {"command", "commander"}, {"gpt", "deepseek"}}
		for _, c := range yes {
			if !familyIsReductionOf(bestiary.Family(c[0]), bestiary.Family(c[1])) {
				t.Errorf("familyIsReductionOf(%q,%q) = false, want true", c[0], c[1])
			}
		}
		for _, c := range no {
			if familyIsReductionOf(bestiary.Family(c[0]), bestiary.Family(c[1])) {
				t.Errorf("familyIsReductionOf(%q,%q) = true, want false", c[0], c[1])
			}
		}
	})

	t.Run("nonFamilyPrefixDowngrade catches flash-lite, allows clears/laterals", func(t *testing.T) {
		// flash-lite → flash: variant lost specificity → TRUE (still a regression).
		if !nonFamilyPrefixDowngrade(decompTuple{"gemini", "flash-lite", "2.5", nil}, decompTuple{"gemini", "flash", "2.5", nil}) {
			t.Error("flash-lite→flash must be a non-family prefix downgrade")
		}
		// instruct → "" (cleared as over-capture noise) → FALSE (allowed in family reduction).
		if nonFamilyPrefixDowngrade(decompTuple{"mixtral-8x22b", "instruct", "", nil}, decompTuple{"mixtral", "", "", nil}) {
			t.Error("clearing a variant must NOT count as a prefix downgrade")
		}
		// image → flash (lateral, not a prefix) → FALSE.
		if nonFamilyPrefixDowngrade(decompTuple{"gemini-2.5-flash", "image", "2.5", nil}, decompTuple{"gemini", "flash", "2.5", nil}) {
			t.Error("lateral variant change must NOT count as a prefix downgrade")
		}
	})

	t.Run("familySuffixMovedToVariant", func(t *testing.T) {
		if !familySuffixMovedToVariant(decompTuple{"claude-opus", "", "4.1", nil}, decompTuple{"claude", "opus", "4.1", nil}) {
			t.Error("claude-opus → claude+opus must be a suffix→variant move")
		}
		if !familySuffixMovedToVariant(decompTuple{"kat-coder", "pro", "", nil}, decompTuple{"kat", "coder", "", nil}) {
			t.Error("kat-coder → kat+coder must be a suffix→variant move")
		}
		// NOT an exact move: variant does not reconstruct the family suffix.
		if familySuffixMovedToVariant(decompTuple{"llama-3.3-70b", "instruct", "3.3", nil}, decompTuple{"llama", "instruct", "3.3", nil}) {
			t.Error("llama-3.3-70b → llama+instruct is NOT an exact suffix→variant move")
		}
	})
}

// TestSLICE11_ClassifyFamilyReduction asserts classifyDecompChange's end-to-end verdicts
// for the family over-capture cases — converging reductions are FIXES, single-
// provider information-preserving reductions are IMPROVEMENTS, and a non-family specificity
// LOSS (flash-lite) converging onto a sibling is STILL a regression (the G1 guard holds).
func TestSLICE11_ClassifyFamilyReduction(t *testing.T) {
	cases := []struct {
		desc       string
		id         bestiary.ModelID
		before     decompTuple
		after      decompTuple
		beforeByID []decompTuple
		afterByID  []decompTuple
		want       changeCategory
	}{
		{
			desc:       "claude-opus → claude+opus, converges onto raw sibling → FIX",
			id:         "claude-opus-4-1",
			before:     decompTuple{"claude-opus", "", "4.1", nil},
			after:      decompTuple{"claude", "opus", "4.1", nil},
			beforeByID: []decompTuple{{"claude-opus", "", "4.1", nil}, {"claude", "opus", "4.1", nil}},
			afterByID:  []decompTuple{{"claude", "opus", "4.1", nil}, {"claude", "opus", "4.1", nil}},
			want:       CatFix,
		},
		{
			desc:       "deepseek-r1 → deepseek (drops r1), converges onto raw sibling → FIX",
			id:         "deepseek-r1",
			before:     decompTuple{"deepseek-r1", "", "", nil},
			after:      decompTuple{"deepseek", "", "", nil},
			beforeByID: []decompTuple{{"deepseek-r1", "", "", nil}, {"deepseek", "", "", nil}},
			afterByID:  []decompTuple{{"deepseek", "", "", nil}, {"deepseek", "", "", nil}},
			want:       CatFix,
		},
		{
			desc:       "single-provider jamba-large-1.6 → jamba+large, no sibling → IMPROVE",
			id:         "jamba-large-1.6",
			before:     decompTuple{"jamba-large", "", "1.6", nil},
			after:      decompTuple{"jamba", "large", "1.6", nil},
			beforeByID: []decompTuple{{"jamba-large", "", "1.6", nil}},
			afterByID:  []decompTuple{{"jamba", "large", "1.6", nil}},
			want:       CatImprove,
		},
		{
			desc:       "flash-lite → flash converging onto a sibling's flash is STILL a regression (G1; lite in ID)",
			id:         "gemini-2.5-flash-lite-preview",
			before:     decompTuple{"gemini", "flash-lite", "2.5", nil},
			after:      decompTuple{"gemini", "flash", "2.5", nil},
			beforeByID: []decompTuple{{"gemini", "flash-lite", "2.5", nil}, {"gemini", "flash", "2.5", nil}},
			afterByID:  []decompTuple{{"gemini", "flash", "2.5", nil}, {"gemini", "flash", "2.5", nil}},
			want:       CatRegress,
		},
		{
			// ADVERSARIAL: a family reduction that ALSO CLEARS a populated
			// variant THE ID CARRIES and that NO sibling re-surfaces must fall to cat-(c).
			// Earlier this slipped through (the reduction branch ignored field clears).
			desc:       "family reduction clearing an ID-PRESENT variant that the converged-to sibling does NOT carry → REGRESS",
			id:         "essentialai/rnj-1-instruct",
			before:     decompTuple{"rnj-1", "instruct", "1", nil},
			after:      decompTuple{"rnj", "", "1", nil},
			beforeByID: []decompTuple{{"rnj-1", "instruct", "1", nil}, {"rnj", "instruct", "1", nil}},
			afterByID:  []decompTuple{{"rnj", "", "1", nil}, {"rnj", "instruct", "1", nil}},
			want:       CatRegress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, reason := classifyDecompChange(tc.id, tc.before, tc.after, tc.beforeByID, tc.afterByID, nil)
			if got != tc.want {
				t.Errorf("classifyDecompChange = %s (%s), want %s", got, reason, tc.want)
			}
		})
	}
}

// TestSLICE12_SanctionedAllowlistGate is the NO-MASKING adversarial unit (,
// the supervisor refinement that OVERRIDES the handoff): the o-series sanctioned escape is
// EXPECTED-TUPLE-MATCHED, not ID-blanket. It proves four properties:
//
//	(1) an allowlisted ID whose observed AFTER tuple EQUALS its ratified target → cat-(a).
//	(2) an allowlisted ID mutated to a WRONG (non-ratified) tuple → cat-(c) (no masking).
//	(3) an o-series-SHAPED reassignment whose ID is NOT on the allowlist → cat-(c).
//	(4) an UNRELATED bug riding an allowlisted ID (a tuple unrelated to the ratified one)
//	    → cat-(c) — you cannot hide a regression on an allowlisted ID under the escape.
func TestSLICE12_SanctionedAllowlistGate(t *testing.T) {
	// A small, explicit allowlist standing in for the committed artifact: o1-mini's
	// ratified target tuple per (Q2a/Q2b).
	allow := sanctionedAllowlist{
		"o1-mini": decompTuple{"gpt", "o", "1", []string{"mini"}},
		"gpt-4o":  decompTuple{"gpt", "4o", "", nil},
	}
	cases := []struct {
		desc   string
		id     bestiary.ModelID
		before decompTuple
		after  decompTuple
		// bID/aID describe the cross-provider tuples for this ID; per-case so the
		// convergence shape is realistic (same parser → same after on every provider).
		bID  []decompTuple
		aID  []decompTuple
		want changeCategory
	}{
		{
			desc:   "(1) allowlisted o1-mini converged to its RATIFIED tuple → sanctioned cat-(a)",
			id:     "o1-mini",
			before: decompTuple{"o", "mini", "1", nil},
			after:  decompTuple{"gpt", "o", "1", []string{"mini"}},
			bID:    []decompTuple{{"o", "mini", "1", nil}, {"gpt", "o", "1", []string{"mini"}}},
			aID:    []decompTuple{{"gpt", "o", "1", []string{"mini"}}, {"gpt", "o", "1", []string{"mini"}}},
			want:   CatFix,
		},
		{
			desc:   "(2) allowlisted o1-mini mutated to a WRONG tuple (mini left in variant, no ratified 'o') → cat-(c)",
			id:     "o1-mini",
			before: decompTuple{"o", "mini", "1", nil},
			after:  decompTuple{"gpt", "mini", "1", nil}, // NOT the ratified (gpt,o,1,mini)
			// Even though the parser would produce this on every provider (consistent) and
			// the mechanical classifier would call it a clean family-convergence fix, the
			// allowlist is AUTHORITATIVE → hard cat-(c). This is the no-masking property.
			bID:  []decompTuple{{"o", "mini", "1", nil}, {"gpt", "mini", "1", nil}},
			aID:  []decompTuple{{"gpt", "mini", "1", nil}, {"gpt", "mini", "1", nil}},
			want: CatRegress,
		},
		{
			desc:   "(3) LOSSY gpt-4o-shaped change (version '4o' DROPPED, not relocated) on a NON-allowlisted ID → cat-(c); you MUST allowlist it",
			id:     "zzz-4o", // not in the allowlist; '4o' is in the ID and is NOT re-surfaced
			before: decompTuple{"gpt", "", "4o", nil},
			after:  decompTuple{"gpt", "", "", nil}, // '4o' dropped entirely → real loss
			// A LOSSLESS 4o relocation (version→variant) is correctly a de-noise improvement
			// and needs no allowlist; but a LOSSY drop of the ID-present '4o' (not re-surfaced)
			// is a real regression unless the exact ratified tuple is allowlisted.
			bID:  []decompTuple{{"gpt", "", "4o", nil}},
			aID:  []decompTuple{{"gpt", "", "", nil}},
			want: CatRegress,
		},
		{
			desc:   "(4) UNRELATED bug riding an allowlisted ID (gpt-4o mangled to a foreign tuple) → cat-(c)",
			id:     "gpt-4o",
			before: decompTuple{"gpt", "", "4o", nil},
			after:  decompTuple{"deepseek", "", "", nil}, // unrelated to the ratified (gpt,4o,"")
			bID:    []decompTuple{{"gpt", "", "4o", nil}, {"deepseek", "", "", nil}},
			aID:    []decompTuple{{"deepseek", "", "", nil}, {"deepseek", "", "", nil}},
			want:   CatRegress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			got, reason := classifyDecompChange(tc.id, tc.before, tc.after, tc.bID, tc.aID, allow)
			if got != tc.want {
				t.Errorf("classifyDecompChange = %s (%s), want %s", got, reason, tc.want)
			}
		})
	}
}

// TestSLICE12_AllowlistConformsToRatification asserts the committed o-series allowlist
// artifact conforms to the ratified rule: every entry is family="gpt" with
// the line designator in the VARIANT slot, version follows the designator rule (o→digits,
// 4o/audio→empty), and the ID actually carries the designator. This is the reviewable
// integrity check tying the artifact to the ratification (no stray/padded entries).
func TestSLICE12_AllowlistConformsToRatification(t *testing.T) {
	allow, err := loadSanctionedAllowlist()
	if err != nil {
		t.Fatalf("%v", err)
	}
	if len(allow) == 0 {
		t.Fatal("allowlist is empty — expected the o-series entries")
	}
	digits := func(s string) bool {
		if s == "" {
			return false
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	for id, tup := range allow {
		idl := strings.ToLower(string(id))
		if tup.Family != "gpt" {
			t.Errorf("%s: family=%q, want gpt (all OpenAI under gpt)", id, tup.Family)
		}
		switch tup.Variant {
		case "o":
			if !digits(tup.Version) {
				t.Errorf("%s: variant 'o' must have a digit version, got %q", id, tup.Version)
			}
			if !strings.Contains(idl, "o"+tup.Version) {
				t.Errorf("%s: ID does not carry o%s designator", id, tup.Version)
			}
		case "4o":
			if tup.Version != "" {
				t.Errorf("%s: variant '4o' must have empty version, got %q", id, tup.Version)
			}
			if !strings.Contains(idl, "4o") {
				t.Errorf("%s: ID does not contain '4o'", id)
			}
		case "audio":
			if tup.Version != "" {
				t.Errorf("%s: variant 'audio' must have empty version, got %q", id, tup.Version)
			}
			if !strings.Contains(idl, "audio") {
				t.Errorf("%s: ID does not contain 'audio'", id)
			}
		default:
			t.Errorf("%s: variant %q is not a ratified line designator (o/4o/audio)", id, tup.Variant)
		}
	}
}

// oseriesMultiModifierCompromise lists the allowlisted o-series IDs that carry 2+ tokens
// competing for the SINGLE Modifier slot. does NOT rule on which wins (the
// Modifier-LIST is deferred to ), so their Modifier is a parser-determined
// single-slot COMPROMISE — NOT independently rule-derivable. They are excluded from the
// strict rule-authored Modifier assertion below (their designator+version IS still asserted).
var oseriesMultiModifierCompromise = map[bestiary.ModelID]bool{
	"gpt-4o-mini-search-preview": true, "gpt-4o-search-preview": true,
	"openai/gpt-4o-mini-search-preview": true, "openai/gpt-4o-search-preview": true,
	"openai/gpt-4o-audio-preview": true, "openai/gpt-4o-mini-search": true,
	"openai/gpt-4o-search":  true,
	"o4-mini-deep-research": true, "openai/o4-mini-deep-research": true,
	"openai/o3-mini-high": true, "openai/o3-mini-low": true,
}

// ratifiedOSeriesTuple INDEPENDENTLY authors the expected (family,variant,version,modifier)
// tuple for an o-series ID DIRECTLY from the deterministic rule — a pure
// function of the ID, written WITHOUT consulting the parser (guardrail-1: the allowlist is
// the SPEC, the parser conforms to it, not the other way around). Returns ok=false for a
// non-o-series ID or a multi-modifier compromise (whose Modifier the rule does not fix).
func ratifiedOSeriesTuple(id bestiary.ModelID) (decompTuple, bool) {
	s := strings.ToLower(string(id))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "@", "-")
	toks := strings.Split(s, "-")
	if len(toks) == 0 {
		return decompTuple{}, false
	}
	var variant, version string
	switch {
	case reOSeriesLineTest.MatchString(toks[0]):
		variant, version = "o", reOSeriesLineTest.FindStringSubmatch(toks[0])[1]
	case contains(toks, "4o"):
		variant, version = "4o", ""
	case contains(toks, "audio") && (toks[0] == "gpt" || toks[0] == "chatgpt"):
		variant, version = "audio", ""
	default:
		return decompTuple{}, false
	}
	// Modifier (independent rule): the single size/finetune designator. mini/pro/nano are
	// SIZE tokens demoted to Modifier; latest/preview are capability modifiers; deep-research
	// is a finetune modifier. If 2+ are present it is a single-slot compromise (ok=false).
	recognized := map[string]bool{"mini": true, "pro": true, "nano": true, "latest": true, "preview": true}
	var mods []string
	joined := "-" + s + "-"
	if strings.Contains(joined, "-deep-research-") {
		mods = append(mods, "deep-research")
	}
	for _, t := range toks {
		if recognized[t] {
			mods = append(mods, t)
		}
	}
	if len(mods) > 1 {
		return decompTuple{}, false // multi-modifier compromise — rule does not fix it
	}
	return decompTuple{"gpt", variant, version, bestiary.CanonicalizeModifiers(mods)}, true
}

var reOSeriesLineTest = regexp.MustCompile(`^o([0-9]+)$`)

// TestSLICE12_AllowlistMatchesIndependentRule is guardrail-1 (supervisor checkpoint-1): every
// allowlist entry's tuple must EQUAL the tuple INDEPENDENTLY authored from the
// rule (ratifiedOSeriesTuple, written without consulting the parser) — so a subtle wrong tuple
// copied from parser output cannot self-pass. Multi-modifier compromise IDs (whose Modifier the
// rule does not fix) only have their designator+version checked; their Modifier is documented.
func TestSLICE12_AllowlistMatchesIndependentRule(t *testing.T) {
	allow, err := loadSanctionedAllowlist()
	if err != nil {
		t.Fatalf("%v", err)
	}
	for id, got := range allow {
		want, clean := ratifiedOSeriesTuple(id)
		if !clean {
			// Compromise (or non-derivable): assert only the designator+version match the rule.
			if oseriesMultiModifierCompromise[id] {
				// designator+version independently checked via the family/variant/version rule:
				ruleVarVer, _ := ratifiedOSeriesTupleDesignatorOnly(id)
				if got.Family != "gpt" || got.Variant != ruleVarVer.Variant || got.Version != ruleVarVer.Version {
					t.Errorf("%s: designator/version %q/%q != rule %q/%q", id, got.Variant, got.Version, ruleVarVer.Variant, ruleVarVer.Version)
				}
				continue
			}
			t.Errorf("%s: not derivable by the o-series rule and not a documented compromise — investigate", id)
			continue
		}
		if !tupleEqual(got, want) {
			t.Errorf("%s: allowlist tuple %s != INDEPENDENTLY rule-authored %s (guardrail-1: allowlist must match the rule, not parser output)", id, got, want)
		}
	}
}

// ratifiedOSeriesTupleDesignatorOnly returns just the rule-derived family/variant/version
// (ignoring the Modifier slot) for the compromise IDs.
func ratifiedOSeriesTupleDesignatorOnly(id bestiary.ModelID) (decompTuple, bool) {
	s := strings.ToLower(string(id))
	if i := strings.LastIndexByte(s, '/'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.ReplaceAll(s, "@", "-")
	toks := strings.Split(s, "-")
	switch {
	case reOSeriesLineTest.MatchString(toks[0]):
		return decompTuple{"gpt", "o", reOSeriesLineTest.FindStringSubmatch(toks[0])[1], nil}, true
	case contains(toks, "4o"):
		return decompTuple{"gpt", "4o", "", nil}, true
	case contains(toks, "audio") && (toks[0] == "gpt" || toks[0] == "chatgpt"):
		return decompTuple{"gpt", "audio", "", nil}, true
	}
	return decompTuple{}, false
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// R1 set-independence + R2(i) adversarial modifier-move proof
// ─────────────────────────────────────────────────────────────────────────────

// TestPathUnification_Slice10_ModifierSetIndependence (R1) asserts the categorizer
// compares the Modifier list as an ORDER-INDEPENDENT SET: a permuted modifier list
// across two providers is NOT a divergence and NOT a change.
func TestPathUnification_Slice10_ModifierSetIndependence(t *testing.T) {
	a := decompTuple{"kimi", "k", "2", []string{"thinking", "turbo"}}
	b := decompTuple{"kimi", "k", "2", []string{"turbo", "thinking"}} // permuted

	if !tupleEqual(a, b) {
		t.Errorf("permuted modifier lists must be SET-equal: %s vs %s", a, b)
	}
	if a.cmp() != b.cmp() {
		t.Errorf("cmp() keys differ for permuted modifiers: %v vs %v", a.cmp(), b.cmp())
	}
	// Cross-provider divergence: two providers carrying permuted lists must collapse to
	// ONE distinct tuple (no divergence).
	seen := map[decompCmp]struct{}{a.cmp(): {}, b.cmp(): {}}
	if len(seen) != 1 {
		t.Errorf("permuted modifier lists counted as %d distinct tuples across providers, want 1", len(seen))
	}
	// A pure reorder is never a real non-family loss.
	if realNonFamilyLoss(a, b, "kimi-k2-thinking-turbo") {
		t.Errorf("a pure modifier reorder was flagged as realNonFamilyLoss")
	}
}

// TestPathUnification_Slice10_ModifierMove_MutationProof (R2-i) is the adversarial
// mutation-proof test: a value CLAIMED "moved variant→modifier" is asserted to be
// ACTUALLY IN after.Modifier; deleting it from the after-modifier list MUST flip the
// record to cat-(c) — you cannot launder a real drop as a sanctioned move.
func TestPathUnification_Slice10_ModifierMove_MutationProof(t *testing.T) {
	id := bestiary.ModelID("meta-llama/llama-3.3-70b-instruct")
	before := decompTuple{"llama", "instruct", "3.3", nil}
	after := decompTuple{"llama", "", "3.3", []string{"instruct"}} // instruct moved variant→modifier
	mutated := decompTuple{"llama", "", "3.3", nil}                // ADVERSARIAL: token deleted

	// The sanctioned move: instruct is absent from after.Variant/Version but PRESENT in
	// the after-Modifier set.
	if !movedToModifier("instruct", after) {
		t.Fatal("movedToModifier(instruct, after) = false, want true (instruct is in after.Modifier)")
	}
	if realNonFamilyLoss(before, after, id) {
		t.Error("sanctioned variant→modifier move wrongly flagged as realNonFamilyLoss")
	}
	allow := sanctionedAllowlist{}
	if cat, _ := classifyDecompChange(id, before, after, []decompTuple{before}, []decompTuple{after}, allow); cat == CatRegress {
		t.Error("sanctioned variant→modifier move classified as cat-(c) — should be a fix/improvement")
	}

	// MUTATION: delete instruct from the after-modifier list. It is now a GENUINE drop.
	if movedToModifier("instruct", mutated) {
		t.Error("after deleting instruct from the modifier list, movedToModifier MUST be false")
	}
	if !realNonFamilyLoss(before, mutated, id) {
		t.Error("after deleting the moved token, the record MUST be a real non-family loss")
	}
	if cat, _ := classifyDecompChange(id, before, mutated, []decompTuple{before}, []decompTuple{mutated}, allow); cat != CatRegress {
		t.Errorf("mutated (token-dropped) record MUST be cat-(c), got %v — a real drop laundered as a move", cat)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Foundation — valueInID numeric dash-form completeness
// ─────────────────────────────────────────────────────────────────────────────

// TestValueInID_Gp9y_NumericDashForm proves the safety-predicate is now COMPLETE for the
// numeric dash-form (the pre-existing alphabetic-only blind spot) AND still does NOT
// false-positive on glued generation noise.
func TestValueInID_Gp9y_NumericDashForm(t *testing.T) {
	// (i) CAUGHT: a genuine version present only as scattered hyphen-digits.
	caught := []struct {
		val string
		id  bestiary.ModelID
	}{
		{"4.5", "claude-opus-4-5-20251101"}, // dash form of 4.5
		{"4.5", "anthropic/claude-opus-4-5"},
		{"3.5", "openai/gpt-3-5-turbo"},
		{"2.5", "gemini-2.5-flash"}, // dotted form (already contiguous) — still true
	}
	for _, c := range caught {
		if !valueInID(c.val, c.id) {
			t.Errorf("valueInID(%q,%q) = false, want true (dash/dot-form numeric version must be CAUGHT)", c.val, c.id)
		}
	}

	// (ii) NO FALSE POSITIVE: glued generation noise where the digits are NON-adjacent.
	noFP := []struct {
		val string
		id  bestiary.ModelID
	}{
		{"2.3", "nousresearch/hermes-3-llama-3.1-70b"}, // 2 absent; 3s non-adjacent to any 2
		{"2.3", "hermes-2-pro-llama-3"},                // 2 and 3 present but NON-adjacent
		{"1.5", "qwen-1-pro-mixtral-5"},                // 1 and 5 non-adjacent
	}
	for _, c := range noFP {
		if valueInID(c.val, c.id) {
			t.Errorf("valueInID(%q,%q) = true, want false (non-adjacent digits must NOT FP as a version)", c.val, c.id)
		}
	}

	// (i') A record DROPPING a genuine scattered-numeric version → cat-(c).
	allow := sanctionedAllowlist{}
	beforeC := decompTuple{"claude", "opus", "4.5", nil}
	afterC := decompTuple{"claude", "opus", "", nil}
	if cat, _ := classifyDecompChange("claude-opus-4-5-20251101", beforeC, afterC,
		[]decompTuple{beforeC}, []decompTuple{afterC}, allow); cat != CatRegress {
		t.Errorf("dropping scattered-numeric version 4.5 → got %v, want CatRegress (real numeric loss must be cat-(c))", cat)
	}

	// (ii') A record clearing a PHANTOM numeric (non-adjacent digits, not a real version)
	// → NOT cat-(c) (clean de-noise).
	beforeP := decompTuple{"hermes", "", "2.3", nil}
	afterP := decompTuple{"hermes", "", "", nil}
	if cat, _ := classifyDecompChange("nousresearch/hermes-3-llama-3.1-70b", beforeP, afterP,
		[]decompTuple{beforeP}, []decompTuple{afterP}, allow); cat == CatRegress {
		t.Error("clearing phantom non-adjacent '2.3' → got CatRegress, want non-regress (must NOT FP)")
	}
}
