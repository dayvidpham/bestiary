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
	After      string            `json:"after"`
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

	// A divergent ID that converged to a brand-new value (no provider had it BEFORE,
	// not yet fully consistent) but where the change is still a strict enrichment was
	// already caught above. Anything reaching here changed a populated field to a
	// different value without being a convergence — flag as a regression for review.
	return CatRegress, fmt.Sprintf("populated field changed value without converging: before=%s after=%s", before, after)
}
