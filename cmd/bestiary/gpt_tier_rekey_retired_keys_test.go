package main

import (
	_ "embed"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/gpt_tier_rekey_retired_keys_corpus.json
var gptTierRekeyRetiredKeysCorpusJSON []byte

// gptTierRekeyRetiredKeyCount is the size of the retired set these two levers produce
// together: 12 keys from the gpt 5.6 tier re-key and 14 from the redundant
// leading-token strip. It is the exact-count control for the corpus, and it is the
// retired-key set itself, so it moves only in the same commit as a measured key diff.
const gptTierRekeyRetiredKeyCount = 26

// TestRetiredKeys_GptTierRekey_PolicySplit pins the retired-key policy for every key
// the gpt tier re-key and the redundant leading-token strip retire, at the exact-key
// seam — bestiary.EntityByKey, and the web route GET /entity/<key> that dereferences
// through it — and at the two production seams a user actually reaches:
// `bestiary show <key> --by-entity` and `bestiary show <key>`. NEITHER of the two CLI
// seams is an exact-key seam: both resolve the input through the model resolver first,
// which keeps its short-reference fallback.
//
// The policy is a uniform hard 404 on the exact-key seam: no alias is minted, no
// redirect is added and no successor is listed. That arm holds for all 26. The
// `--by-entity` seam also reports not-found for all 26 here, but that is a MEASURED
// per-key result, not a consequence of exactness.
//
// The looser `show` seam is pinned PER KEY against what it measurably does, not
// against one blanket rule, and this set contains all three outcomes:
//
//   - not-found, for the 14 keys whose string names nothing live any more;
//   - the under-specified error, for the 9 whose FAMILY survives them, so the string
//     still has live children — the same reason bare `gpt`, `claude` and `mimo` have
//     always come back that way;
//   - RESOLVED, for 3 — and that is a measured DEVIATION from the epoch-wide
//     expectation, recorded rather than repaired. `ministral#3b{instruct}`,
//     `mistral/large#675b{instruct}` and `nemotron#120b` each remain a valid
//     UNDER-SPECIFIED reference to exactly one live entity: the successor key carries
//     a version the retired key did not, so a ref that omits the version still names
//     one model. No alias, redirect or successor-listing instrument exists — the
//     ordinary resolver is simply doing its ordinary job. Turning these into 404s
//     would mean making an under-specified ref fail whenever it happens to match a
//     retired spelling, which breaks working lookups for the sake of a slogan.
func TestRetiredKeys_GptTierRekey_PolicySplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, gptTierRekeyRetiredKeysCorpusJSON, gptTierRekeyRetiredKeyCount)

	// Value-based coverage: a count-preserving swap must not be able to drop the
	// readings a regression reaches first — the three seam outcomes, the key that
	// SPLITS because one of its rows is deliberately NOT stripped, the pro key whose
	// path segment becomes an identity modifier, and the two whose FAMILY was wrong.
	for _, want := range []string{
		"gpt-luna", "gpt-luna/pro", "gpt-luna/pro@5.6", "gpt-luna@5.6",
		"gpt/pro", "kimi-k2{code}", "agi",
		"mistral/mini#3b", "mistral/small#24b",
		"ministral#3b{instruct}", "mistral/large#675b{instruct}", "nemotron#120b",
	} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// Seam 1 — the exact-key lookup, bestiary.EntityByKey, and above it the
			// resolver-routed `show --by-entity`. The uniform-404 policy is about the
			// exact-key arm, which admits no per-key exception in this set; the
			// --by-entity not-found for all 26 is measured, not structural.
			if _, ok := bestiary.EntityByKey(key); ok {
				t.Errorf("EntityByKey(%q) still resolves; the key was retired and must be a hard 404", key)
			}
			assertRunSeam(t, c.Expected.ByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})

			// Seam 2 — the looser model resolver + entity fallback behind bare `show`.
			assertRunSeam(t, c.Expected.Show, key,
				[]string{"show", key, "--db-path", tmpDB, "--output=table"})
		})
	}
}

// TestRetiredKeys_GptTierRekey_SuccessorSetsMatchMeasuredRehoming is the falsifier for
// the migration record. Each case pins the instances its key held before the levers;
// the successor set is RE-DERIVED from those instances against the live registry and
// compared with the recorded one, so a row naming a successor none of its rows
// actually landed on fails here rather than misdirecting a user.
//
// Two rows in this set are exactly why the record cannot be written from assumption.
// `gpt-luna` looks like a pure rename onto `gpt/luna@5.6` and is not: gitlab's
// `duo-chat-` prefix is deliberately NOT stripped, so one of its five rows stays on
// the undated key and the row SPLITS. `gpt/pro` splits three ways, onto 5.2, 5.4 and
// 5.5, because its rows were dated by two different mechanisms.
func TestRetiredKeys_GptTierRekey_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, gptTierRekeyRetiredKeysCorpusJSON, gptTierRekeyRetiredKeyCount)
	home := instanceHomes(t)

	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; the migration record for a key that "+
					"held no rows is unfalsifiable — pin the instances it held before the levers", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors; neither lever destroys an instance, "+
					"so every retired key's rows re-home somewhere", key)
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					t.Errorf("instance %q (recorded as a pre-lever row of retired key %q) is in no "+
						"live entity; these levers conserve the instance total, so either that is a "+
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

// TestRetiredKeys_GptTierRekey_ChangelogTableMatchesCorpus keeps the two copies of the
// migration record honest with each other. The CHANGELOG table is the copy a human
// reads and the corpus is the copy the tests re-derive; a correction applied to one and
// forgotten in the other would leave the user-facing half wrong while every test stayed
// green.
func TestRetiredKeys_GptTierRekey_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, gptTierRekeyRetiredKeysCorpusJSON, gptTierRekeyRetiredKeyCount)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	// Each curation lever writes its OWN table and the CHANGELOG accumulates several
	// under one release, so the header must be distinct from every other lever's.
	table := parseMigrationTable(t, string(raw), "| retired key (tier re-key + prefix strip) | instances re-home to |")

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
