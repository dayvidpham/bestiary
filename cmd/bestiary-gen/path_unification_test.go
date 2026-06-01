package main

// SLICE-9 (rc2) PATH-UNIFICATION — before/after decomposition diff harness.
//
// CLARIFICATION-8 (bestiary-l77c): the user chose the ROOT-CAUSE path-unification
// over the surgical patch — make ParseFamilyDetailed FULLY ID-driven (raw_family a
// HINT/FALLBACK only, never overriding an ID-derived value). The risk that choice
// carries is correctness regression: the raw_family="" experiment proved divergence
// drops to 0, but did NOT prove the unified path never WORSENS a currently-correct
// decomposition.
//
// This file is the MANDATORY SAFEGUARD (the de-risking gate):
//
//   L1 (bestiary-5sbg): dumpDecomposition() over ALL snapshot records ×
//      (Family,Variant,Version,Modifier); a committed BEFORE baseline
//      (testdata/snapshot/decomp_baseline.tsv) captured PRE-refactor; and the
//      a/b/c categorizer (classifyDecompChange) that labels every change.
//   L2 (bestiary-68ct): TestPathUnification_ZeroUnexpectedRegression — the GATE:
//      loads the frozen BEFORE baseline, computes the live AFTER decomposition,
//      categorizes every change, and asserts category-(c) (UNEXPECTED REGRESSION)
//      is EMPTY. Pre-refactor the diff is empty (trivially green); post-refactor it
//      is the regression surface the reviewers scrutinize.
//   L3 (bestiary-m0e6): the refactor + the committed categorized diff report
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
	Modifier  string
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
	Modifier string
}

func (r decompRecord) tuple() decompTuple {
	return decompTuple{r.Family, r.Variant, r.Version, r.Modifier}
}

func (t decompTuple) String() string {
	return fmt.Sprintf("(family=%q,variant=%q,version=%q,modifier=%q)", t.Family, t.Variant, t.Version, t.Modifier)
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
	return strings.Join([]string{
		string(r.Provider), string(r.ID), string(r.RawFamily),
		string(r.Family), r.Variant, r.Version, r.Modifier,
	}, "\t")
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
		Modifier:  parts[6],
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
				"  What: the SLICE-9 BEFORE baseline (pre-path-unification decomposition) is missing\n"+
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
// Run this ONCE, PRE-refactor (on the SLICE-8 HEAD), and commit the result.
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
		{before.Modifier, after.Modifier},
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
func classifyDecompChange(before, after decompTuple, beforeByID, afterByID []decompTuple) (changeCategory, string) {
	distinct := func(ts []decompTuple) int {
		seen := map[decompTuple]struct{}{}
		for _, t := range ts {
			seen[t] = struct{}{}
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
		if t == after && t != before {
			matchedExistingBefore = true
			break
		}
	}

	if beforeDivergent && (afterConsistent || matchedExistingBefore) {
		reason := "converged divergent ID toward cross-provider agreement"
		if matchedExistingBefore {
			reason = "now matches a tuple another provider already produced for this ID (BEFORE)"
		} else if afterConsistent {
			reason = "all providers for this ID now agree post-unification"
		}
		return CatFix, reason
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
	if before.Family == after.Family && before.Version == after.Version && before.Modifier == after.Modifier &&
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

	// A divergent ID that converged to a brand-new value (no provider had it BEFORE,
	// not yet fully consistent) but where the change is still a strict enrichment was
	// already caught above. Anything reaching here changed a populated field to a
	// different value without being a convergence — flag as a regression for review.
	return CatRegress, fmt.Sprintf("populated field changed value without converging: before=%s after=%s", before, after)
}

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
	TotalRecords     int            `json:"total_records"`
	ChangedCount     int            `json:"changed_count"`
	CatFixCount      int            `json:"cat_a_divergence_fix_count"`
	CatImprove       int            `json:"cat_b_improvement_count"`
	CatRegress       int            `json:"cat_c_unexpected_regression_count"`
	JustifiedCount   int            `json:"justified_exception_count"`
	DivergenceBefore int            `json:"divergence_before"`
	DivergenceAfter  int            `json:"divergence_after"`
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
		if br.tuple() == ar.tuple() {
			continue
		}
		cat, reason := classifyDecompChange(br.tuple(), ar.tuple(), beforeByID[ar.ID], afterByID[ar.ID])
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
		seen := map[decompTuple]struct{}{}
		for _, t := range ts {
			seen[t] = struct{}{}
		}
		if len(seen) >= 2 {
			n++
		}
	}
	return n
}

// TestPathUnification_ZeroUnexpectedRegression is THE GATE (CLARIFICATION-8 mandatory
// safeguard). It diffs the FROZEN pre-refactor BEFORE baseline against the LIVE AFTER
// decomposition, categorizes every change (a/b/c), writes the committed categorized
// diff report, and asserts ZERO category-(c) UNEXPECTED REGRESSIONS.
//
// Pre-refactor (baseline == live) the diff is empty → trivially green. Post-refactor
// the categorized changes are the regression surface the per-slice reviewers scrutinize.
// justifiedExceptions is the ENUMERATED, REVIEWED ledger of intended decomposition
// changes that the mechanical a/b/c classifier flags as category-(c) but that human
// review has confirmed are genuine fixes/improvements (not regressions). Keyed by
// model ID; each entry carries a one-line justification. This mirrors the snapshot
// gate's "enumerated justified exception ledger" philosophy: the gate fails on any
// category-(c) change NOT in this ledger, so new regressions are never silently
// absorbed. ADDING an entry is a reviewed decision (committed in the diff artifact).
var justifiedExceptions = map[bestiary.ModelID]string{
	// raw_family "gemini-flash" MISLABELS a PRO model (the ID literally contains
	// "pro"). The ID-driven variant "pro" is authoritative and correct; the raw
	// "flash" was a provider data error. Single-provider correctness fix.
	"gemini-2.5-pro-preview-tts": "raw_family 'gemini-flash' mislabels a PRO model (ID says 'pro'); ID-driven variant 'pro' is correct",
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
			if rationale, ok := justifiedExceptions[c.ID]; ok {
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

	// Write the committed categorized diff artifact.
	report := diffReport{
		TotalRecords:     total,
		ChangedCount:     len(changes),
		CatFixCount:      fix,
		CatImprove:       improve,
		CatRegress:       regress,
		JustifiedCount:   justified,
		DivergenceBefore: divBefore,
		DivergenceAfter:  divAfter,
		Changes:          changes,
	}
	reportBytes, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("marshal diff report: %v", err)
	}
	reportPath := filepath.Join(snapshotDir(), "decomp_diff_report.json")
	if err := os.WriteFile(reportPath, append(reportBytes, '\n'), 0o644); err != nil {
		t.Fatalf("write diff report: %v", err)
	}

	t.Logf("=== SLICE-9 path-unification before/after diff ===")
	t.Logf("records=%d  changed=%d  (a)divergence-fix=%d  (b)improvement=%d  (c)REGRESSION=%d  justified-exception=%d",
		total, len(changes), fix, improve, regress, justified)
	t.Logf("divergence: before=%d  after=%d", divBefore, divAfter)
	t.Logf("committed report → %s", reportPath)

	if regress != 0 {
		t.Errorf("GATE FAILED: %d category-(c) UNEXPECTED REGRESSION(s) — must be ZERO.\n"+
			"  What: a populated decomposition field changed to a different value without converging\n"+
			"        a divergent ID (i.e. a currently-correct decomposition was WORSENED)\n"+
			"  Why: SLICE-9 (CLARIFICATION-8) requires zero unexpected regressions; the path-unification\n"+
			"       must be family-preserving and monotonic on Variant/Version/Modifier\n"+
			"  How to fix: add a targeted raw_family fallback for the flagged case in reconcileIDDriven,\n"+
			"       OR (if the change is actually intended) justify it and reclassify\n"+
			"  Regressions:", regress)
		for _, c := range regressions {
			t.Errorf("    %s/%s raw=%q  %s → %s  [%s]", c.Provider, c.ID, c.RawFamily, c.Before, c.After, c.Reason)
		}
	}
}
