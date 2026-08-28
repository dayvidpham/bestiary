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

// collisionSplitSurvivingLingKeys is the inclusionAI ling keyspace the split must not
// touch. Each entry pairs a key with the instance count it holds, and the negative control
// this map serves is UNCHANGED: no Inkling row and no klingai row may ever appear on any
// of these keys, and none of inclusionAI's own rows may leave them.
//
// The counts were originally the pre-split census and are now RE-MEASURED at the
// 2026-08-28 models.dev catalog refresh, which moved this family in two ways at once:
//
//   - `ling@2.6#1t` 4 -> 2 and `ling/flash@2.6` 4 -> 2. Upstream retired provider rows from
//     the 2.6 line as it published 3.0; the rows were DELETED, not moved onto another ling
//     key (the 3.0 keys below are all served by 3.0-spelled ids). Nothing about the split
//     took them away.
//   - two keys ADDED: `ling/flash@3.0` (8 rows) and `ling@3.0` (7 rows), from inclusionAI's
//     new Ling 3.0 line. They are inclusionAI's own weights, so they belong in this control
//     rather than beside it — a klingai or Inkling row landing on a ling 3.0 key would be
//     the same defect the split repaired, and only a pinned count catches it.
//
// The count literals are therefore a re-measurement of a live family, not a claim about
// what the lever did. What the lever did is asserted by the identity of the SET, which is
// what the family-membership check below pins.
var collisionSplitSurvivingLingKeys = map[string]int{
	"ling#1t":             2,
	"ling@2.6#1t":         2,
	"ling/flash@2.0":      2,
	"ling/flash@2.6":      2,
	"ling/flash-free@2.6": 1,
	"ling/flash@3.0":      8,
	"ling@3.0":            7,
}

// TestRetiredKeys_CollisionSplit_MeasuredSeamSplit pins the retired-key policy for the
// keys the ling/inkling/kling collision split retires, at the two production seams a
// user actually reaches: `bestiary show <key> --by-entity` (an exact match over the
// store-overlaid entity index — entity key, entity preferred name or concrete model id —
// with no short-reference path) and `bestiary show <key>` (the model resolver, which
// keeps its short-reference fallback). The exact-key seams are bestiary.EntityByKey and
// GET /entity/<key>.
//
// The policy is a uniform hard 404 — no alias is minted, no redirect is added, and no
// successor is listed. The CHANGELOG old -> new migration table is the only pointer a
// user gets, and the corpus's per-case mutation line carries the same mapping so the two
// records cannot drift apart silently.
//
// This lever is where the policy's ONE anticipated deviation actually lands, so the
// corpus carries a per-key seam expectation rather than one blanket rule. `kling-v2@6`
// 404s on both seams: nothing of it survives. Bare `ling` 404s on `--by-entity` (and at
// bestiary.EntityByKey) but comes back AMBIGUOUS at plain `show` — not because it split,
// but because
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
			// Seam 1 — the exact-key lookup bestiary.EntityByKey, and above it
			// `show --by-entity`, which matches the store-overlaid entity index by
			// entity key, entity preferred name or concrete model id. It has no
			// short-reference path, so both keys 404 here regardless of the family.
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
	// `inkling` 6 -> 28 and the total 15 -> 37 with the 2026-08-28 models.dev catalog
	// refresh. The lever is unchanged and still moves rows without creating any; what
	// changed is the POPULATION it moved them into. Thinking Machines' Inkling went from a
	// 6-row model to a widely rehosted one — 28 provider rows, including a new Inkling-Small
	// sibling set and the org-prefixed rename of Thinking Machines' own endpoint — so the
	// arithmetic in the conservation comment below no longer closes on the lever's 14 + 1.
	//
	// The honest statement is the one the assertions now make: the nine successor keys are
	// live, the nine kling keys still hold exactly the one row each the split gave them
	// (which is where a mis-split would show), and `inkling` holds every row the split sent
	// it PLUS whatever upstream has since added. Conservation of the lever's own population
	// is asserted by SET in the companion re-homing test, which is the check that can
	// actually falsify it; these counts are a census.
	want := map[string]int{
		"inkling":                  29,
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
	// The successor-key census. This was pinned as instance CONSERVATION across the lever —
	// bare `ling`'s 14 rows plus the one `kling-v2@6` row, 15 and no more — and that premise
	// is no longer true of the live catalog: the 2026-08-28 refresh grew `inkling` from 6
	// rows to 28 with rows that were never part of the lever's population, so 15 is now a
	// count of a historical population rather than of these keys.
	//
	// 15 -> 37 (28 on `inkling` + 1 on each of the nine kling keys). 37 -> 38 at the round-2
	// review pin on requesty's "inkling-256k": that row had minted the phantom inkling@256k out
	// of a SERVED CONTEXT LENGTH, and pinning it to the bare family moves its one instance onto
	// `inkling`, 28 -> 29. It is an ADDITION to this census, not a re-split — the nine kling
	// keys are untouched. Read it as a census,
	// not as conservation: the conservation claim survives intact in
	// TestRetiredKeys_CollisionSplit_SuccessorSetsMatchMeasuredRehoming, which re-derives
	// each retired key's rows against the live registry by SET and would fail if one had
	// been dropped or duplicated.
	if total != 38 {
		t.Errorf("the split's successor keys hold %d instances in total, want 38 — 29 on `inkling` "+
			"after the 2026-08-28 refresh and the inkling-256k pin, plus one on each of the nine "+
			"kling keys; a kling key holding more or fewer than one row is a mis-split", total)
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
