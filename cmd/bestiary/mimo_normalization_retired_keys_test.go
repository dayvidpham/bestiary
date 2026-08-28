package main

import (
	_ "embed"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/mimo_normalization_retired_keys_corpus.json
var mimoNormalizationRetiredKeysCorpusJSON []byte

// mimoRetiredKeyCount is the size of the retired set this lever produces. It is the key
// diff itself, so it moves only alongside a re-measured diff.
const mimoRetiredKeyCount = 10

// TestRetiredKeys_MimoNormalization_MeasuredSplit pins the retired-key policy for every
// key the keyspace-wide mimo normalization retires, at the two seams a user reaches:
// `bestiary show <key> --by-entity` (an exact match over the store-overlaid entity index
// — entity key, entity preferred name or concrete model id — with no short-reference
// path) and `bestiary show <key>` (the model resolver, which keeps its short-reference
// fallback). The exact-key seams are bestiary.EntityByKey and GET /entity/<key>.
//
// The policy is a uniform hard 404 — no alias is minted, no redirect is added, no
// successor is listed. The corpus pins the MEASURED split rather than that blanket rule,
// because one key in this set does not 404 on the looser seam: bare `mimo`. That is not
// an exception to the policy but a different question being asked. `mimo` retires as a
// KEY (the three tts rows that occupied it move to `mimo@2{tts}`) while the FAMILY stays
// live with nine children, so the looser seam is resolving a bare family token with live
// children — exactly what `show gpt` and `show claude` do. Turning it into a 404 would
// break those three live-family lookups, so the corpus records what it measurably does.
//
// This is a VERIFICATION pin: nothing was written to make it hold. Its value is that it
// fails loudly if a later change quietly resurrects a retired mimo key through an alias
// seed, a nomen claim, or a family-override row that re-mints the old tuple.
func TestRetiredKeys_MimoNormalization_MeasuredSplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, mimoNormalizationRetiredKeysCorpusJSON, mimoRetiredKeyCount)

	// Value-based coverage: a count-preserving swap must not be able to drop the readings
	// a regression reaches first — the keys that had to collapse onto one, the
	// bare junk key with its ambiguous seam, and the two-tier speech key whose second tier
	// only survives because the tier promotion returns a list.
	for _, want := range []string{
		"mimo/v@2.5{pro}", "mimo/pro",
		"mimo", "mimo/v2.5-tts-voiceclone", "mimo/flash",
	} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
		}
	}

	// The three spellings of the 2.5 Pro model must all be gone AND all point at the same
	// successor: that is the whole requirement, and asserting it here means a corpus edit
	// cannot quietly re-target one of them. Two of the three are retired by THIS lever;
	// the third, `mimo/v2.5-pro`, was already retired by the earlier free-tier demotion,
	// so its row lives in that lever's corpus — where this lever re-pointed it at the same
	// target and the cross-check test holds it there.
	const target = "mimo@2.5{pro}"
	for _, old := range []string{"mimo/v@2.5{pro}", "mimo/pro"} {
		for i := range corpus.Cases {
			if corpus.Cases[i].Input != old {
				continue
			}
			if got := corpus.Cases[i].Expected.Successors; !slices.Equal(got, []string{target}) {
				t.Errorf("retired key %q records successors %v, want exactly [%s] — the three "+
					"spellings of the 2.5 Pro model must collapse onto ONE key", old, got, target)
			}
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// Seam 1 — the exact-key lookup bestiary.EntityByKey, and above it
			// `show --by-entity`, which matches the store-overlaid entity index by
			// entity key, entity preferred name or concrete model id. It has no
			// short-reference path, so every retired key 404s here, bare `mimo` included.
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

// TestRetiredKeys_MimoNormalization_SuccessorSetsMatchMeasuredRehoming re-derives every
// recorded successor set from the instances the retired key actually held, and compares
// it against the record. The pinned instances are the evidence, not an assumption: an
// instance is the only thing that survives a key retirement, so "where did this key's
// rows go" is a question only its instances can answer.
//
// It also asserts instance CONSERVATION for the whole lever. Nine of these ten keys are
// pure renames and the tenth is a merge, so nothing may be created or destroyed: the
// instances pinned across the ten retired keys and the instances living on the nine
// surviving mimo keys must be the same set, not merely the same count.
func TestRetiredKeys_MimoNormalization_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, mimoNormalizationRetiredKeysCorpusJSON, mimoRetiredKeyCount)
	home := instanceHomes(t)

	retired := map[string]bool{}
	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; the migration record for a key that "+
					"held no rows is unfalsifiable — pin the instances it held before the lever", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors; every retired key's rows re-home "+
					"somewhere (this lever conserves the instance total exactly)", key)
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					t.Errorf("instance %q (recorded as a pre-lever row of retired key %q) is in no "+
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
					"successor set sends them to a key that does not hold their model. Re-derive "+
					"the row from the measured re-homing above and correct it here, in the "+
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

	for _, c := range corpus.Cases {
		for _, inst := range c.Expected.RetiredInstances {
			retired[inst] = true
		}
	}
	live := map[string]bool{}
	for _, e := range bestiary.Entities() {
		if !isMimoKey(e.Ref.String()) {
			continue
		}
		for _, in := range e.Instances {
			live[string(in.Provider)+"|"+string(in.ID)] = true
		}
	}
	// Instance conservation, re-stated after the 2026-08-28 models.dev catalog refresh.
	//
	// This was SET EQUALITY between the rows pinned across the retired keys and the rows
	// living on the surviving mimo keys, and equality had a premise: that the lever's own
	// population IS the whole mimo keyspace. The refresh made that premise false. Eighteen
	// mimo rows arrived that no retired key ever held (llmgateway-providers' six backend-
	// labelled v2.5 rows, llmtr's two, nano-gpt's four `:thinking` and `-crof` spellings,
	// requesty's two, and digitalocean, impossibl, scnet-token-plan and inferx one apiece),
	// so equality can no longer hold no matter what the lever did, and re-pinning it by
	// adding those rows to the corpus would be inventing a population the retired keys never
	// had.
	//
	// The conservation CLAIM is kept and split into the two halves that are still
	// falsifiable:
	//
	//   - nothing was destroyed: every pinned row must still be live on a mimo key. This is
	//     the half the lever could break, and it is asserted exactly as before.
	//   - nothing foreign was created: every live mimo row the lever did not place must still
	//     be a genuine MiMo id. A mislabelled row swept onto this keyspace — the defect class
	//     the whole normalization exists to prevent — fails here.
	for _, inst := range sortedKeys(retired) {
		if !live[inst] {
			t.Errorf("mimo instance conservation FAILED: pinned row %q is on no surviving mimo key. "+
				"This lever is nine renames and one merge; it may not destroy an instance. If "+
				"upstream deleted the row, re-measure the corpus case and say so there.", inst)
		}
	}
	for _, inst := range sortedKeys(live) {
		if retired[inst] {
			continue
		}
		if !strings.Contains(strings.ToLower(inst), "mimo") {
			t.Errorf("live mimo-keyspace row %q was not placed by this lever and is not a MiMo id; "+
				"a foreign row on this keyspace is the mislabelling the normalization exists to "+
				"prevent", inst)
		}
	}
}

// TestRetiredKeys_MimoNormalization_ChangelogTableMatchesCorpus keeps the two copies of
// the migration record honest with each other. The CHANGELOG table is the copy a human
// reads and the corpus is the copy the tests re-derive, so a correction applied to one
// and forgotten in the other would leave the user-facing half wrong while every test
// stayed green.
func TestRetiredKeys_MimoNormalization_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, mimoNormalizationRetiredKeysCorpusJSON, mimoRetiredKeyCount)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	table := parseMigrationTable(t, string(raw), "| retired mimo key | instances re-home to |")

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

// TestMimoKeyspace_IsExactlyTheSurvivingNine pins the whole surviving mimo keyspace, not
// just the retirements. A retired-key test alone cannot catch a key that was ADDED by
// accident, and the failure mode this lever risks most is minting a residue key (an id
// that declines the series split and lands on a variant spelling of its own).
//
// STILL NINE after the 2026-08-28 models.dev catalog refresh, and that is worth recording
// because the refresh briefly made it ten. A new upstream id, `inferx|mimo-v25`, spells
// MiMo v2.5 with the dot dropped; with no dot to split on, the mechanical path read the
// orphaned digits as the whole version and minted a residue key `mimo@25` — a "MiMo 25"
// line Xiaomi never published, the same defect shape as the cogito `@1` phantom. It was
// repaired with a curated pin in the generated data (the layer this test observes but does
// not own), and the row now sits on `mimo@2.5` with its siblings. The list below is
// therefore unchanged, and it is what caught the residue.
func TestMimoKeyspace_IsExactlyTheSurvivingNine(t *testing.T) {
	want := []string{
		"mimo@2.5",
		"mimo@2.5{pro}",
		"mimo@2.5{tts,voiceclone}",
		"mimo@2.5{tts,voicedesign}",
		"mimo@2.5{tts}",
		"mimo@2{flash}",
		"mimo@2{omni}",
		"mimo@2{pro}",
		"mimo@2{tts}",
	}

	var got []string
	instances := 0
	for _, e := range bestiary.Entities() {
		k := e.Ref.String()
		if !isMimoKey(k) {
			continue
		}
		got = append(got, k)
		instances += len(e.Instances)
	}
	sort.Strings(got)

	if !slices.Equal(got, want) {
		t.Errorf("mimo keyspace = %v,\nwant %v — the surviving set is the lever's published key diff; "+
			"an extra key means an id declined the series split and minted a residue spelling, a "+
			"missing one means a distinction was collapsed", got, want)
	}
	// 93 -> 102 with the 2026-08-28 models.dev catalog refresh. This is a CENSUS of a live
	// keyspace, not the lever's conservation figure: nine of the lever's rows were deleted
	// upstream (kilo's and nano-gpt's `mimo-v2-*` ids, both providers having moved to the
	// v2.5 line only) and eighteen rows arrived that the lever never placed. The conservation
	// claim itself is asserted by SET, in two directions, in
	// TestRetiredKeys_MimoNormalization_SuccessorSetsMatchMeasuredRehoming.
	if instances != 102 {
		t.Errorf("mimo holds %d instances, want 102 — a census of the live mimo keyspace at the "+
			"2026-08-28 refresh, re-measured rather than derived from the lever", instances)
	}

	// No `mimo/v*` key may survive anywhere: the series letter is consumed for the version
	// and must never reappear in a variant slot.
	for _, k := range got {
		if len(k) >= 7 && k[:7] == "mimo/v@" || len(k) >= 6 && k[:6] == "mimo/v" {
			t.Errorf("key %q still carries the series letter in its variant slot", k)
		}
	}
}

// isMimoKey reports whether an entity key belongs to the mimo family. It matches on the
// family segment rather than a raw prefix so a hypothetical `mimolike` family could never
// be swept into these assertions.
func isMimoKey(key string) bool {
	if len(key) < 4 || key[:4] != "mimo" {
		return false
	}
	if len(key) == 4 {
		return true
	}
	switch key[4] {
	case '/', '@', '#', '{', '[':
		return true
	default:
		return false
	}
}
