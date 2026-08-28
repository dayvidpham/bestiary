package bestiary_test

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// The bake-identity gate.
//
// The path-unification gate reports (c)REGRESSION=0 against decomp_baseline.tsv, and that
// number is correct — but it cannot mean "this refresh introduced no regression".
// decomp_baseline.tsv is RE-CAPTURED whenever the catalog is re-vendored. That is the
// right procedure and it is declared honestly, but it has a structural consequence: a
// decomposition change introduced BY the refresh is frozen into the new baseline before
// the gate measures anything, so the gate can only see what curation levers move AFTER
// the re-capture. Refresh-introduced regressions are invisible to it by construction.
//
// This file supplies the measurement that gap needs. testdata/bake_identity_baseline.tsv
// is the PREVIOUS RELEASE's bake — (ID, Provider) -> (Family, Variant, Version) — and it
// is deliberately NOT re-captured on a refresh. It moves only when a release is tagged.
// Over the rows present in BOTH bakes, a populated Version going empty is a REGRESSION
// unless it is enumerated in bakeIdentityVersionLosses with a reason.
//
// Version is identity here (Opus 4.5 is not 4.6). A row that loses its version folds into
// a coarser, undated bucket, so `show` and `series` can no longer separate generations,
// and a user who pinned a dated key at the previous release finds it retired with its
// instances scattered onto undated successors.

const bakeIdentityBaselinePath = "testdata/bake_identity_baseline.tsv"

type bakeRowKey struct {
	ID       string
	Provider string
}

type bakeTuple struct {
	Family  string
	Variant string
	Version string
}

func (t bakeTuple) String() string {
	return fmt.Sprintf("(family=%q,variant=%q,version=%q)", t.Family, t.Variant, t.Version)
}

// bakeIdentityVersionLosses is the ENUMERATED, REVIEWED ledger of rows that legitimately
// lose a populated Version between the previous release bake and this one. It is the
// twin of cmd/bestiary-gen's justifiedExceptions: the gate fails on any loss NOT listed,
// so a new one is never silently absorbed, and it fails on any listed loss that no longer
// occurs, so the ledger cannot rot into dead curation.
//
// SEEDED from the measured 2026-08-28 refresh: 5,146 rows are present in both bakes; 9
// lose a populated Version, 2 GAIN one, and 0 change value. (The finding that produced
// this gate measured TEN losses; the tenth — nano-gpt's Gemma 4 31B distill, which lost
// its "4" when upstream stamped raw_family "claude" on it — was a real defect and is
// FIXED by an exact-id pin rather than justified here.)
//
// Every remaining loss is one upstream FAMILY RELABEL, and the reason is recorded per row
// rather than as a blanket rule, because each has a different disposition.
var bakeIdentityVersionLosses = map[bakeRowKey]string{
	// The ByteDance Seed fold. Upstream re-labelled these rows from the compound families
	// "doubao-seed" / "doubao" to the short family "seed", and the short family carries no
	// version-bearing token the scan can reach: the version lives in the DASH-SPELLED date
	// suffix ("1-6-250615", "2-0-260215") that the doubao spellings used to expose.
	//
	// DEFERRED, DELIBERATELY, and this ledger is where the cost is now written down. A
	// family_aliases.json row folding seed/doubao-seed/doubao together moves entity keys
	// across three families at once, which is a curation slice with its own ruling — not
	// something to land inside a data refresh. The deferral note recorded the cost as
	// coexisting sibling keys; the measured cost is larger, and it is these six version
	// losses. Recorded here so the next curator inherits the real figure.
	{ID: "doubao-seed-1-6-250615", Provider: "nano-gpt"}:              doubaoSeedFold,
	{ID: "doubao-seed-1-6-flash-250615", Provider: "nano-gpt"}:        doubaoSeedFold,
	{ID: "doubao-seed-2-0-code-preview-260215", Provider: "nano-gpt"}: doubaoSeedFold,
	{ID: "doubao-seed-2-0-lite-260215", Provider: "nano-gpt"}:         doubaoSeedFold,
	{ID: "doubao-seed-2-0-mini-260215", Provider: "nano-gpt"}:         doubaoSeedFold,
	{ID: "doubao-seed-2-0-pro-260215", Provider: "nano-gpt"}:          doubaoSeedFold,

	// LearnLM was absorbed into Gemini upstream: the catalog now labels this row family
	// "gemini", and "1.5" was LearnLM's own release line, not a Gemini one. Carrying it
	// across would assert a Gemini 1.5 Pro that this row is not. The honest reading of the
	// relabel is that the row's own version no longer applies, so the loss is CORRECT
	// rather than merely tolerated. No pin is warranted.
	{ID: "learnlm-1.5-pro-experimental", Provider: "nano-gpt"}: "upstream absorbed LearnLM into Gemini and re-labelled the row family gemini; \"1.5\" was LearnLM's release line, not Gemini's, so carrying it across would assert a generation this row does not belong to. The loss is the correct reading of the relabel.",

	// Poolside's Laguna. Upstream moved the series letter out of the family
	// ("laguna-s" -> "laguna") without moving it into the id, so the "s-2.1" segment is no
	// longer split into a series letter plus a version and the 2.1 is not reached. This is
	// the shape the mimo normalization repairs by separating the series letter from the
	// family — but mimo has ~40 rows across nine keys establishing the reading, and laguna
	// has these two, both on one provider, one of them a free spelling of the other. A
	// two-row lever on a family with no sibling evidence is a curation judgement rather
	// than a mechanical repair, so it is recorded here and left for the curator who has the
	// wider Laguna picture. Named as an OPEN item, not as a settled correctness claim.
	{ID: "poolside/laguna-s-2.1", Provider: "vercel"}:      lagunaSeriesLetter,
	{ID: "poolside/laguna-s-2.1-free", Provider: "vercel"}: lagunaSeriesLetter,
}

const (
	doubaoSeedFold     = "upstream re-labelled the row from the compound family doubao-seed/doubao to the short family seed, and the version lives in the dash-spelled date suffix the compound spelling used to expose. The seed/doubao-seed/doubao fold is DEFERRED to its own curation slice because it moves entity keys across three families; these six version losses are that deferral's real cost, recorded here rather than left unstated."
	lagunaSeriesLetter = "upstream moved the series letter out of the family (laguna-s -> laguna) without moving it into the id, so \"s-2.1\" is no longer split into a series letter plus a version. This is the shape the mimo normalization repairs, but laguna has only these two rows on one provider (one the free spelling of the other) and no sibling evidence for the reading. OPEN: it needs the curator who has the wider Laguna picture, not a two-row pin landed inside a data refresh."
)

func loadBakeIdentityBaseline(t *testing.T) map[bakeRowKey]bakeTuple {
	t.Helper()
	fh, err := os.Open(bakeIdentityBaselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", bakeIdentityBaselinePath, err)
	}
	defer fh.Close()
	out := map[bakeRowKey]bakeTuple{}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Text()
		if strings.HasPrefix(raw, "#") || strings.TrimSpace(raw) == "" {
			continue
		}
		f := strings.Split(raw, "\t")
		if len(f) != 5 {
			t.Fatalf("%s:%d: got %d tab-separated fields, want 5 "+
				"(id, provider, family, variant, version)", bakeIdentityBaselinePath, line, len(f))
		}
		out[bakeRowKey{ID: f[0], Provider: f[1]}] = bakeTuple{Family: f[2], Variant: f[3], Version: f[4]}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", bakeIdentityBaselinePath, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty — the baseline must never be emptied to make this gate pass",
			bakeIdentityBaselinePath)
	}
	return out
}

func liveBakeTuples() map[bakeRowKey]bakeTuple {
	out := map[bakeRowKey]bakeTuple{}
	for _, m := range bestiary.Models() {
		out[bakeRowKey{ID: string(m.ID), Provider: string(m.Provider)}] = bakeTuple{
			Family: string(m.Family), Variant: m.Variant, Version: m.Version,
		}
	}
	return out
}

// TestBakeIdentity_NoUnjustifiedVersionLoss is the gate. Over the rows present in BOTH
// the previous release bake and this one, a populated Version going empty must be
// enumerated in bakeIdentityVersionLosses.
func TestBakeIdentity_NoUnjustifiedVersionLoss(t *testing.T) {
	base := loadBakeIdentityBaseline(t)
	live := liveBakeTuples()

	var both int
	var unjustified []string
	var justified, gained, changed int
	for key, before := range base {
		after, ok := live[key]
		if !ok {
			continue
		}
		both++
		switch {
		case before.Version != "" && after.Version == "":
			if _, ok := bakeIdentityVersionLosses[key]; ok {
				justified++
				continue
			}
			unjustified = append(unjustified, fmt.Sprintf("    %s | %s | %s -> %s",
				key.ID, key.Provider, before, after))
		case before.Version == "" && after.Version != "":
			gained++
		case before.Version != "" && after.Version != "" && before.Version != after.Version:
			changed++
		}
	}
	sort.Strings(unjustified)

	t.Logf("bake identity: %d rows in both bakes; version LOST %d (all justified), GAINED %d, CHANGED %d",
		both, justified+len(unjustified), gained, changed)

	if len(unjustified) > 0 {
		t.Errorf("%d row(s) LOST a populated Version with no ledger entry:\n%s\n"+
			"  What: a row present in both the previous release bake and this one used to carry a\n"+
			"    version and no longer does\n"+
			"  Why it matters: Version is identity in this project (Opus 4.5 is not 4.6). The row folds\n"+
			"    into a coarser, undated bucket, so `show` and `series` stop separating generations and\n"+
			"    a user who pinned the dated key at the previous release finds it retired\n"+
			"  Why the other gates are silent: the path-unification baseline is RE-CAPTURED on a\n"+
			"    catalog refresh, so a regression introduced by the refresh itself is frozen into that\n"+
			"    baseline before it is measured. This gate is the one that can see it\n"+
			"  Where: %s versus the live bake\n"+
			"  How to fix: EITHER pin the id in idFamilyOverrides (parse.go) so the version is recovered\n"+
			"    — the established lever, used for the -crof backend label, the Nemotron tier-before-\n"+
			"    version spellings and mimo-v25 — OR, if the loss is the correct reading of an upstream\n"+
			"    relabel, add a row to bakeIdentityVersionLosses stating WHY, per row, in this file",
			len(unjustified), strings.Join(unjustified, "\n"), bakeIdentityBaselinePath)
	}
}

// TestBakeIdentity_LedgerHasNoDeadRows fails on dead curation: a ledger entry whose loss
// no longer occurs. Without it, a justification outlives the transition it justified and
// would silently absorb a future regression that happened to reproduce the same key.
func TestBakeIdentity_LedgerHasNoDeadRows(t *testing.T) {
	base := loadBakeIdentityBaseline(t)
	live := liveBakeTuples()

	var dead []string
	for key := range bakeIdentityVersionLosses {
		before, inBase := base[key]
		after, inLive := live[key]
		switch {
		case !inBase:
			dead = append(dead, fmt.Sprintf("    %s | %s — absent from the baseline", key.ID, key.Provider))
		case !inLive:
			dead = append(dead, fmt.Sprintf("    %s | %s — no longer in the bake (upstream retired it)", key.ID, key.Provider))
		case before.Version == "" || after.Version != "":
			dead = append(dead, fmt.Sprintf("    %s | %s — no longer loses a version (%s -> %s)",
				key.ID, key.Provider, before, after))
		}
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Errorf("%d ledger row(s) no longer describe a live version loss:\n%s\n"+
			"  Why it matters: a dead row reads as reviewed coverage while justifying nothing, and it\n"+
			"    would absorb a future regression that reproduced the same key\n"+
			"  How to fix: delete the row. If the loss was FIXED by a pin, say so in that commit; if\n"+
			"    upstream retired the id, the row simply goes.", len(dead), strings.Join(dead, "\n"))
	}
}

// TestBakeIdentity_NoFamilyErasure asserts the stronger, unconditional invariant: no row
// present in both bakes may lose its Family outright. Unlike a version loss this has no
// legitimate upstream cause — a relabel replaces a family, it does not empty one — so it
// carries no ledger.
func TestBakeIdentity_NoFamilyErasure(t *testing.T) {
	base := loadBakeIdentityBaseline(t)
	live := liveBakeTuples()
	var erased []string
	for key, before := range base {
		after, ok := live[key]
		if !ok {
			continue
		}
		if before.Family != "" && after.Family == "" {
			erased = append(erased, fmt.Sprintf("    %s | %s | %s -> %s", key.ID, key.Provider, before, after))
		}
	}
	sort.Strings(erased)
	if len(erased) > 0 {
		t.Errorf("%d row(s) LOST their Family:\n%s\n"+
			"  What: a row carried a family at the previous release and now carries none\n"+
			"  Why it matters: a family-less row cannot key an entity at all; it is not a coarser\n"+
			"    reading, it is a lost artifact. There is no legitimate upstream cause — a relabel\n"+
			"    replaces a family, it never empties one\n"+
			"  How to fix: this is a pipeline defect, not a curation judgement. Find the seam that\n"+
			"    declined and repair it; do not add a ledger for this class", len(erased), strings.Join(erased, "\n"))
	}
}
