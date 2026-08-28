package main

import (
	_ "embed"
	"errors"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/collision_split_retired_keys_corpus.json
var collisionSplitRetiredKeysCorpusJSON []byte

// collisionSplitSurvivingLingKeys is the inclusionAI ling keyspace, which the split must
// leave BYTE-IDENTICAL. Each entry pairs the key with the instance count it carried
// before the split, measured off the pre-lever generated catalog.
var collisionSplitSurvivingLingKeys = map[string]int{
	"ling#1t":             2,
	"ling@2.6#1t":         4,
	"ling/flash@2.0":      2,
	"ling/flash@2.6":      4,
	"ling/flash-free@2.6": 1,
}

// TestRetiredKeys_CollisionSplit_MeasuredSeamSplit pins the retired-key policy for the
// keys the ling/inkling/kling collision split retires, at the two production seams a
// user actually reaches: `bestiary show <key> --by-entity` (the exact-key entity lookup)
// and `bestiary show <key>` (the looser model resolver with its entity fallback).
//
// The policy is a uniform hard 404 — no alias is minted, no redirect is added, and no
// successor is listed. The CHANGELOG old -> new migration table is the only pointer a
// user gets, and the corpus's per-case mutation line carries the same mapping so the two
// records cannot drift apart silently.
//
// This lever is where the policy's ONE anticipated deviation actually lands, so the
// corpus carries a per-key seam expectation rather than one blanket rule. `kling-v2@6`
// 404s on both seams: nothing of it survives. Bare `ling` 404s only on the exact-key
// seam and comes back AMBIGUOUS on the looser one — not because it split, but because
// its FAMILY outlives the key. Five inclusionAI ling entities are still live, so the
// bare family token still names a real set, exactly as `show gpt`, `show claude` and
// `show mimo` do. That reading is measured and pinned; it must never be "corrected" into
// a 404, because doing so would break those three unrelated live-family lookups too.
//
// This is a VERIFICATION pin, not a behaviour the slice implemented: nothing was written
// to make it hold. Its value is that it fails loudly if some later change quietly
// resurrects a retired key — via an alias seed, a nomen claim, or a family-override row
// that re-mints the old tuple.
func TestRetiredKeys_CollisionSplit_MeasuredSeamSplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, collisionSplitRetiredKeysCorpusJSON, 2)

	// Value-based coverage: a count-preserving swap must not be able to drop either key,
	// and they are the two that differ from each other on the `show` seam.
	for _, want := range []string{"ling", "kling-v2@6"} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
		}
	}
	// The seam split is the whole point of this corpus; assert both readings are present
	// so a well-meaning "make them uniform" edit cannot pass while erasing the deviation.
	var notFound, ambiguous int
	for _, c := range corpus.Cases {
		switch c.Expected.Show {
		case "not-found":
			notFound++
		case "ambiguous":
			ambiguous++
		}
	}
	if notFound != 1 || ambiguous != 1 {
		t.Errorf("corpus records %d not-found / %d ambiguous on the `show` seam, want 1 / 1 — "+
			"the measured split IS the record here; do not flatten it in either direction",
			notFound, ambiguous)
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// Seam 1 — the exact-key entity lookup behind `show --by-entity`. It has no
			// ambiguity path at all, so both keys 404 here regardless of the family.
			if _, ok := bestiary.EntityByKey(key); ok {
				t.Errorf("EntityByKey(%q) still resolves; the key was retired and must be a hard 404", key)
			}
			assertRunSeam(t, c.Expected.ByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})

			// Seam 2 — the looser model resolver + entity fallback behind bare `show`.
			assertRunSeam(t, c.Expected.Show, key,
				[]string{"show", key, "--db-path", tmpDB, "--output=table"})

			// Seam 2, at the LIBRARY level. The CLI reformats an ambiguity into
			// user-facing guidance and drops the type, so the typed contract has to be
			// asserted where it survives — otherwise "ambiguous" in this corpus would be
			// pinned only as a string, and a change that turned it into some OTHER
			// untyped error would still read green.
			_, resolveErr := bestiary.Resolve(key)
			var notFound *bestiary.ErrNotFound
			var ambiguous *bestiary.ErrAmbiguous
			switch c.Expected.Show {
			case "ambiguous":
				if !errors.As(resolveErr, &ambiguous) {
					t.Errorf("bestiary.Resolve(%q) = %v, want *bestiary.ErrAmbiguous — the KEY retires "+
						"while its family stays live, so the bare token still names live children",
						key, resolveErr)
				} else if len(ambiguous.Candidates) == 0 {
					t.Errorf("bestiary.Resolve(%q) reported ambiguity with no candidates", key)
				}
			case "not-found":
				if !errors.As(resolveErr, &notFound) {
					t.Errorf("bestiary.Resolve(%q) = %v, want *bestiary.ErrNotFound", key, resolveErr)
				}
			}
		})
	}
}

// TestRetiredKeys_CollisionSplit_InclusionAIKeyspaceUntouched is the negative control:
// the lever moves 15 instances off two wrong keys, and a "re-key the ling family"
// reading of it would take inclusionAI's real models with them.
//
// It is the assertion the acceptance criterion turns on — the five keys survive the
// whole epoch — and it is checked by VALUE, not by count: each key must resolve and hold
// exactly the instances it held before the split. A count-only check would pass if a
// klingai row wrongly landed on one of them while one of its own rows left.
func TestRetiredKeys_CollisionSplit_InclusionAIKeyspaceUntouched(t *testing.T) {
	for key, wantInstances := range collisionSplitSurvivingLingKeys {
		e, ok := bestiary.EntityByKey(key)
		if !ok {
			t.Errorf("EntityByKey(%q) does not resolve; the collision split must leave inclusionAI's "+
				"ling keyspace byte-identical — only the mislabelled Inkling and klingai rows move", key)
			continue
		}
		if got := len(e.Instances); got != wantInstances {
			t.Errorf("%s holds %d instance(s), want %d — the split must neither take an inclusionAI row "+
				"away nor push a mislabelled one onto it", key, got, wantInstances)
		}
		if got := string(e.Ref.Family); got != "ling" {
			t.Errorf("%s reports family %q, want \"ling\"", key, got)
		}
	}

	// The family itself stays live with exactly those five children, which is the reason
	// bare `ling` answers ambiguous rather than not-found on the `show` seam.
	var live []string
	for _, e := range bestiary.Entities() {
		if e.Ref.Family == "ling" {
			live = append(live, e.Ref.String())
		}
	}
	sort.Strings(live)
	want := make([]string, 0, len(collisionSplitSurvivingLingKeys))
	for k := range collisionSplitSurvivingLingKeys {
		want = append(want, k)
	}
	sort.Strings(want)
	if !slices.Equal(live, want) {
		t.Errorf("family ling now holds %v, want exactly %v", live, want)
	}
}

// TestRetiredKeys_CollisionSplit_SplitTargetsAreLive asserts the other side of the move:
// the keys the retired rows landed ON exist and hold the instances the migration record
// says they do. Without it a successor set could name a live-looking key while the rows
// themselves went somewhere else entirely.
func TestRetiredKeys_CollisionSplit_SplitTargetsAreLive(t *testing.T) {
	want := map[string]int{
		"inkling":                  6,
		"kling@2.6":                1,
		"kling/i2v@2.5{turbo}":     1,
		"kling/t2v@2.5{turbo}":     1,
		"kling/i2v@2.6":            1,
		"kling/motion-control@2.6": 1,
		"kling/t2v@2.6":            1,
		"kling/i2v@3.0":            1,
		"kling/motion-control@3.0": 1,
		"kling/t2v@3.0":            1,
	}
	total := 0
	for key, wantInstances := range want {
		e, ok := bestiary.EntityByKey(key)
		if !ok {
			t.Errorf("EntityByKey(%q) does not resolve; the split's successor keys must be live", key)
			continue
		}
		if got := len(e.Instances); got != wantInstances {
			t.Errorf("%s holds %d instance(s), want %d", key, got, wantInstances)
		}
		total += len(e.Instances)
	}
	// Instance conservation across the lever: the 14 rows bare `ling` held plus the one
	// `kling-v2@6` row account for every instance on the new keys, and no more.
	if total != 15 {
		t.Errorf("the split's successor keys hold %d instances in total, want 15 — this lever moves "+
			"instances between keys and creates none, so the two retired keys' 14 + 1 rows are the "+
			"whole population", total)
	}
}

// TestRetiredKeys_CollisionSplit_SuccessorSetsMatchMeasuredRehoming re-derives each
// retired key's successor set from its pinned pre-split instances against the live
// registry and compares it against the recorded set.
//
// The prose in a migration record is the part nothing checks, and this epoch has already
// shipped two wrong successor claims once (the kimi/minimax free rows, recorded as
// folding onto their bare family line when they actually split three ways). The risk is
// higher here, not lower: bare `ling` fans out to NINE successors across two unrelated
// product lines, and "it folds onto its family" is exactly the wrong guess.
//
// The pinned instances are the evidence, not an assumption: an instance is the only
// thing that survives a key retirement, so "where did this key's rows go" is a question
// only its instances can answer. They were read off the pre-lever generated catalog.
func TestRetiredKeys_CollisionSplit_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, collisionSplitRetiredKeysCorpusJSON, 2)
	home := instanceHomes(t)

	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; the migration record for a key that "+
					"held no rows is unfalsifiable — pin the instances it held before the split", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors; this lever conserves the instance "+
					"total, so every retired key's rows re-home somewhere", key)
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					t.Errorf("instance %q (recorded as a pre-split row of retired key %q) is in no "+
						"live entity; either the instance was dropped — this lever conserves the "+
						"instance total, so that is a defect — or the pinned spelling is stale",
						inst, key)
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
					"row from the measured re-homing above (this key SPLITS across several successors "+
					"— do not assume it folds onto its family line) and correct it here, in the "+
					"CHANGELOG migration table, and in the case's mutation description together.",
					key, got, want)
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

// TestRetiredKeys_CollisionSplit_ChangelogTableMatchesCorpus keeps the two copies of the
// migration record honest with each other. The CHANGELOG table is the copy a human
// reads and the corpus is the copy the tests re-derive, so a correction applied to one
// and forgotten in the other would leave the user-facing half wrong while every test
// stayed green.
func TestRetiredKeys_CollisionSplit_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, collisionSplitRetiredKeysCorpusJSON, 2)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	table := parseMigrationTable(t, string(raw), "| retired collision key | instances re-home to |")

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
