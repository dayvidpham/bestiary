package main

import (
	_ "embed"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/compound_recovery_retired_keys_corpus.json
var compoundRecoveryRetiredKeysCorpusJSON []byte

// compoundRecoveryRetiredKeyCount is the size of the retired set the bare-integer
// series-compound family recovery produces. It is the exact-count control for the
// corpus, and it IS the retired-key set, so it moves only in the same commit as a
// measured key diff.
const compoundRecoveryRetiredKeyCount = 4

// TestRetiredKeys_CompoundRecovery_PolicySplit pins the retired-key policy for every
// key the bare-integer series-compound family recovery retires, at the two production
// seams a user actually reaches: `bestiary show <key> --by-entity` (the exact-key
// entity lookup with its resolver front-end) and `bestiary show <key>` (the looser
// model resolver with its entity fallback).
//
// The policy is a hard 404 with no alias, no redirect and no successor listing, and it
// holds unconditionally at the EXACT-key seam — bestiary.EntityByKey — for all four.
// That is the invariant this test opens with, and it is the one the policy is about.
//
// The two CLI seams are pinned PER KEY against what they measurably do. This set
// contains a DEVIATION the epoch's earlier levers did not produce, and it is recorded
// rather than repaired: `kimi-k2` and `kimi-k3` are the upstream raw_family spellings,
// and once the recovery reduces them they remain valid UNDER-SPECIFIED references that
// the ordinary resolver matches to exactly one live entity each (kimi/k@2 and
// kimi/k@3). So they answer on BOTH CLI seams, including `--by-entity`, which every
// earlier retired key 404s on. No alias, redirect or successor-listing instrument is
// involved — the resolver is doing its ordinary job on a string that still names one
// model. Making them fail would mean breaking an under-specified reference for the sake
// of a slogan, and it would break it for every user who types the upstream spelling.
//
// `kimi{instruct}` takes the third outcome, the under-specified error at the looser
// seam — but NOT for the bare-family reason `show gpt` and `show claude` illustrate, and
// the distinction is worth keeping: `kimi{instruct}` never named a family. It is a
// variant+modifier compound (the surviving bare family is `kimi`), and its 9 candidates
// share ONE identical tuple (family=kimi, variant=k, version=2, modifier=[instruct]),
// differing only by `date`. The ambiguity is therefore the ordinary single-entity
// date-fragmentation any multi-instance entity produces once the resolver's grouping key
// takes `date` into account, not a family surviving its bare key.
func TestRetiredKeys_CompoundRecovery_PolicySplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, compoundRecoveryRetiredKeysCorpusJSON, compoundRecoveryRetiredKeyCount)

	// Value-based coverage: a count-preserving swap must not drop the readings a
	// regression reaches first — the lever's target key, the key that SPLITS, the
	// bare-family exception and the modifier-bearing merge.
	for _, want := range []string{"kimi-k3", "kimi-k2", "kimi{instruct}", "kimi-k2{instruct}"} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// The invariant, admitting no per-key exception: the exact-key entity
			// lookup is a hard 404 for every retired key.
			if _, ok := bestiary.EntityByKey(key); ok {
				t.Errorf("EntityByKey(%q) still resolves; the key was retired and must be a hard 404", key)
			}
			assertRunSeam(t, c.Expected.ByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})
			assertRunSeam(t, c.Expected.Show, key,
				[]string{"show", key, "--db-path", tmpDB, "--output=table"})
		})
	}
}

// TestRetiredKeys_CompoundRecovery_SuccessorSetsMatchMeasuredRehoming is the falsifier
// for the migration record. Each case pins the instances its key held immediately
// before the recovery; the successor set is RE-DERIVED from those instances against the
// live registry and compared with the recorded one, so a row naming a successor none of
// its rows actually landed on fails here rather than misdirecting a user.
//
// `kimi-k2` is exactly why the record cannot be written from assumption: it looks like a
// fold onto one successor and is not. Its four rows split two ways, because the two
// umans-coder ids name no series token (so the letter-prefix seam declines and the
// residual token becomes a variant) while the two umans-kimi-k2.7 ids carry a dotted
// 2.7 the seam reads straight off.
func TestRetiredKeys_CompoundRecovery_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, compoundRecoveryRetiredKeysCorpusJSON, compoundRecoveryRetiredKeyCount)
	home := instanceHomes(t)

	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; the migration record for a key that "+
					"held no rows is unfalsifiable — pin the instances it held before the recovery", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors; this lever destroys no instance, so "+
					"every retired key's rows re-home somewhere", key)
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					t.Errorf("instance %q (recorded as a pre-recovery row of retired key %q) is in no "+
						"live entity; this lever conserves the instance total, so either that is a "+
						"defect or the pinned spelling is stale", inst, key)
					continue
				}
				seen[dest] = true
			}
			got := sortedKeys(seen)

			want := append([]string(nil), exp.Successors...)
			sort.Strings(want)
			if !slices.Equal(got, want) {
				t.Errorf("retired key %q re-homes onto %v, but the migration record says %v.\n"+
					"The record is what a user gets instead of an alias or redirect, so a wrong "+
					"successor set sends them to a key that does not hold their model. Re-derive the "+
					"row from the measured re-homing above (a key may SPLIT — do not assume it folds "+
					"onto one successor) and correct it here, in the CHANGELOG migration table, and "+
					"in the case's mutation description together.", key, got, want)
			}

			for _, s := range exp.Successors {
				if _, ok := bestiary.EntityByKey(s); !ok {
					t.Errorf("retired key %q names successor %q, which does not resolve; a migration "+
						"record may only point at a live key", key, s)
				}
			}
		})
	}
}

// TestRetiredKeys_CompoundRecovery_ChangelogTableMatchesCorpus keeps the two copies of
// the migration record honest with each other, in BOTH directions. The CHANGELOG table
// is the copy a human reads and the corpus is the copy the tests re-derive; a correction
// applied to one and forgotten in the other would leave the user-facing half wrong while
// every test stayed green.
func TestRetiredKeys_CompoundRecovery_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, compoundRecoveryRetiredKeysCorpusJSON, compoundRecoveryRetiredKeyCount)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	// Each curation lever writes its OWN table and the CHANGELOG accumulates several
	// under one release, so the header must be distinct from every other lever's.
	table := parseMigrationTable(t, string(raw), "| retired key (series-compound recovery) | instances re-home to |")

	if len(table) != len(corpus.Cases) {
		t.Errorf("CHANGELOG migration table has %d row(s), corpus has %d case(s); the two are the "+
			"same record and must be edited together", len(table), len(corpus.Cases))
	}
	for _, c := range corpus.Cases {
		got, ok := table[c.Input]
		if !ok {
			t.Errorf("CHANGELOG migration table has no row for retired key %q; every retired key "+
				"needs one, because the table is the only pointer the tool gives a user", c.Input)
			continue
		}
		want := append([]string(nil), c.Expected.Successors...)
		sort.Strings(want)
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("CHANGELOG migration table sends %q to %v, the corpus records %v", c.Input, got, want)
		}
	}
	for old := range table {
		if !corpusHasInput(corpus, old) {
			t.Errorf("CHANGELOG migration table carries a row for %q, which the retired-key corpus "+
				"does not cover", old)
		}
	}
}
