package main

import (
	_ "embed"
	"os"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/cogito_decomposition_retired_keys_corpus.json
var cogitoDecompositionRetiredKeysCorpusJSON []byte

// cogitoRetiredKeyCount is the size of the retired set the cogito repair produces. It is
// the key diff itself, so it moves only alongside a re-measured diff.
const cogitoRetiredKeyCount = 2

// TestRetiredKeys_CogitoDecomposition_MeasuredSplit pins the retired-key policy for both
// keys the cogito repair retires, at the two seams a user reaches: `bestiary show <key>
// --by-entity` (the exact-key entity lookup) and `bestiary show <key>` (the looser model
// resolver with its entity fallback).
//
// The policy is a uniform hard 404 — no alias is minted, no redirect is added, no
// successor is listed. Unlike the ling and mimo sets, this one has no bare-family row: a
// key retires ambiguously on the looser seam only when the token being looked up is a
// bare FAMILY that survives with live children, and both keys here carry a variant or a
// version segment. The corpus records "not-found" on both seams for both keys because
// that is what was measured, not because the blanket rule was assumed to apply.
//
// This is a VERIFICATION pin: nothing was written to make it hold. Its value is that it
// fails loudly if a later change quietly resurrects a retired cogito key through an alias
// seed, a nomen claim, or an override row that re-mints the old tuple.
func TestRetiredKeys_CogitoDecomposition_MeasuredSplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, cogitoDecompositionRetiredKeysCorpusJSON, cogitoRetiredKeyCount)

	// Value-based coverage: a count-preserving swap must not be able to drop either of the
	// two distinct defects this lever repairs — the fused variant that doubled the size,
	// and the phantom version minted from a lost dot.
	for _, want := range []string{"cogito/v2.1-671b#671b", "cogito@1#671b"} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
		}
	}

	// Both retired keys must name the SAME successor: they were two keys for one artifact,
	// and collapsing them onto one key is the entire point of the lever. Asserting it here
	// means a corpus edit cannot quietly re-target one of them.
	const target = "cogito@2.1#671b"
	for i := range corpus.Cases {
		if got := corpus.Cases[i].Expected.Successors; !slices.Equal(got, []string{target}) {
			t.Errorf("retired key %q records successors %v, want exactly [%s] — both spellings name "+
				"one artifact and must collapse onto ONE key", corpus.Cases[i].Input, got, target)
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// Seam 1 — the exact-key entity lookup behind `show --by-entity`.
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

// TestRetiredKeys_CogitoDecomposition_SuccessorSetsMatchMeasuredRehoming re-derives every
// recorded successor set from the instances the retired key actually held, and compares
// it against the record. The pinned instances are the evidence, not an assumption: an
// instance is the only thing that survives a key retirement, so "where did this key's
// rows go" is a question only its instances can answer.
//
// It also asserts instance CONSERVATION for the whole lever. One of these two keys is a
// rename and the other a merge, so nothing may be created or destroyed: the instances
// pinned across the retired keys and the instances living on the surviving cogito
// keyspace must be the same SET, not merely the same count.
func TestRetiredKeys_CogitoDecomposition_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, cogitoDecompositionRetiredKeysCorpusJSON, cogitoRetiredKeyCount)
	home := instanceHomes(t)

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

	retired := map[string]bool{}
	for _, c := range corpus.Cases {
		for _, inst := range c.Expected.RetiredInstances {
			retired[inst] = true
		}
	}
	live := map[string]bool{}
	for _, e := range bestiary.Entities() {
		if !isCogitoKey(e.Ref.String()) {
			continue
		}
		for _, in := range e.Instances {
			live[string(in.Provider)+"|"+string(in.ID)] = true
		}
	}
	if !slices.Equal(sortedKeys(retired), sortedKeys(live)) {
		t.Errorf("cogito instance conservation FAILED: %d instance(s) pinned across the retired keys, "+
			"%d live on the surviving cogito keyspace, and the two sets are not identical. This lever "+
			"is one rename and one merge; it may not create or destroy an instance.",
			len(retired), len(live))
	}
}

// TestRetiredKeys_CogitoDecomposition_ChangelogTableMatchesCorpus keeps the two copies of
// the migration record honest with each other. The CHANGELOG table is the copy a human
// reads and the corpus is the copy the tests re-derive, so a correction applied to one
// and forgotten in the other would leave the user-facing half wrong while every test
// stayed green.
func TestRetiredKeys_CogitoDecomposition_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, cogitoDecompositionRetiredKeysCorpusJSON, cogitoRetiredKeyCount)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	table := parseMigrationTable(t, string(raw), "| retired cogito key | instances re-home to |")

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

// TestCogitoKeyspace_IsExactlyTheOneSurvivingKey pins the whole surviving cogito keyspace,
// not just the retirements. A retired-key test alone cannot catch a key that was ADDED by
// accident, and the failure mode this lever risks most is a residue key: one of the three
// id spellings declining the pin and landing on a variant spelling of its own.
//
// Deep Cogito serves exactly one artifact in this catalog, so the surviving keyspace is a
// single key holding all three instances. The size segment is asserted to appear once —
// that doubling is the defect the decomposition exists to remove.
func TestCogitoKeyspace_IsExactlyTheOneSurvivingKey(t *testing.T) {
	want := []string{"cogito@2.1#671b"}

	var got []string
	instances := 0
	for _, e := range bestiary.Entities() {
		k := e.Ref.String()
		if !isCogitoKey(k) {
			continue
		}
		got = append(got, k)
		instances += len(e.Instances)
	}
	sort.Strings(got)

	if !slices.Equal(got, want) {
		t.Errorf("cogito keyspace = %v,\nwant %v — the surviving set is the lever's published key diff; "+
			"an extra key means an id spelling declined the pin and minted a residue key, a missing "+
			"one means a distinction was collapsed", got, want)
	}
	if instances != 3 {
		t.Errorf("cogito holds %d instances, want 3 — the lever is one rename and one merge, so the "+
			"instance total is conserved exactly", instances)
	}

	// The size must be stated exactly once. Before the repair the key read
	// cogito/v2.1-671b#671b, carrying 671b as an identity token AND as the size segment.
	for _, k := range got {
		if n := countSubstring(k, "671b"); n != 1 {
			t.Errorf("key %q states its parameter size %d time(s), want exactly 1 — the size belongs "+
				"in the #size segment and nowhere else", k, n)
		}
	}
}

// isCogitoKey reports whether an entity key belongs to the cogito family. It matches on
// the family segment rather than a raw prefix so a hypothetical `cogitolike` family could
// never be swept into these assertions.
func isCogitoKey(key string) bool {
	const fam = "cogito"
	if len(key) < len(fam) || key[:len(fam)] != fam {
		return false
	}
	if len(key) == len(fam) {
		return true
	}
	switch key[len(fam)] {
	case '/', '@', '#', '{', '[':
		return true
	default:
		return false
	}
}

// countSubstring counts non-overlapping occurrences of sub in s.
func countSubstring(s, sub string) int {
	n := 0
	for i := 0; i+len(sub) <= len(s); {
		if s[i:i+len(sub)] == sub {
			n++
			i += len(sub)
			continue
		}
		i++
	}
	return n
}
