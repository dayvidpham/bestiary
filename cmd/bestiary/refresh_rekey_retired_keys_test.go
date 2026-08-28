package main

import (
	_ "embed"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/retired/refresh_2026_08_28_rekey_retired_keys_corpus.json
var refreshRekeyRetiredKeysCorpusJSON []byte

// refreshRekeyRetiredKeyCount is the size of the RE-KEYED subset of the refresh's retired
// set. The refresh retires 87 keys measured against the v0.2.10 release bake; 68 are
// genuine upstream deletions and need no migration record, and these 19 are the rest.
// It is the key diff itself, so it moves only alongside a re-measured diff.
const refreshRekeyRetiredKeyCount = 19

// TestRetiredKeys_RefreshRekey_MeasuredSplit pins the retired-key policy for every key the
// 2026-08-28 catalog refresh retired whose ARTIFACT SURVIVED under a different key.
//
// The 68 deletions in the same refresh need no record: their ids left the catalog, so a
// 404 is the whole truth. These 19 are the other kind. `claude-fable@5` held 24 instances
// and RESOLVED at the released v0.2.10; on this tree it 404s, and the successor
// `claude/fable@5` — which holds every one of those 24 rows — is named nowhere the tool or
// the tests can see. Nothing pinned what a released key now does, so a future change could
// flip any of these silently. That is the gap this corpus closes.
//
// The discriminator was MEASURED, not asserted: for each retired key, the instance ids it
// held at the v0.2.10 bake were intersected with the id set of the refreshed catalog. A
// non-empty intersection is a re-key; an empty one is a deletion.
//
// This is a VERIFICATION pin. Nothing was written to make it hold; its value is that it
// fails loudly if an alias seed, a nomen claim or a family-override row later resurrects a
// retired key, or if a successor silently re-points.
func TestRetiredKeys_RefreshRekey_MeasuredSplit(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, refreshRekeyRetiredKeysCorpusJSON, refreshRekeyRetiredKeyCount)

	// Value-based coverage: a count-preserving swap must not be able to drop the readings a
	// regression reaches first — the released key that RESOLVED at v0.2.10 and its undated
	// sibling, the one key whose looser seam is not a 404, and the one key whose instances
	// did not all survive.
	for _, want := range []string{
		"claude-fable@5", "claude-fable",
		"glm/v",
		"imagen@4.0",
	} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of re-keyed retired key %q", want)
		}
	}

	// The two claude-fable spellings must both be gone AND both point into the claude
	// family, so a corpus edit cannot quietly re-target one of them back onto a compound
	// family of its own. This is the migration a v0.2.10 user is most likely to need.
	for old, target := range map[string]string{
		"claude-fable":   "claude/fable",
		"claude-fable@5": "claude/fable@5",
	} {
		for i := range corpus.Cases {
			if corpus.Cases[i].Input != old {
				continue
			}
			if got := corpus.Cases[i].Expected.Successors; !slices.Equal(got, []string{target}) {
				t.Errorf("retired key %q records successors %v, want exactly [%s] — Fable is an "+
					"Anthropic TIER, the peer of opus/sonnet/haiku, and it must key inside the "+
					"claude family rather than on a compound family of its own", old, got, target)
			}
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key := c.Input
		t.Run(c.Name, func(t *testing.T) {
			// Seam 1 — the exact-key lookup. Every retired key 404s here without exception,
			// including the one whose family stays live: this seam has no short-reference path.
			if _, ok := bestiary.EntityByKey(key); ok {
				t.Errorf("EntityByKey(%q) still resolves; the key was retired and must be a hard 404\n"+
					"  Why it matters: this key's artifact survives under %v, so a resurrection here\n"+
					"    would give the same rows two keys and split every lookup between them",
					key, c.Expected.Successors)
			}
			assertRunSeam(t, c.Expected.ByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})

			// Seam 2 — the looser model resolver behind bare `show`.
			assertRunSeam(t, c.Expected.Show, key,
				[]string{"show", key, "--db-path", tmpDB, "--output=table"})
		})
	}
}

// TestRetiredKeys_RefreshRekey_SuccessorsAreLiveAndReDerived re-derives every recorded
// successor set from the instances the retired key actually held, and compares it against
// the record. The pinned instances are the evidence, not an assumption: an instance is the
// only thing that survives a key retirement, so "where did this key's rows go" is a
// question only its instances can answer.
//
// Unlike the curation-lever corpora beside it, this lever is NOT conservative: upstream can
// delete a row in the same refresh that re-keys its siblings. So an instance that homes
// nowhere is not automatically a defect — but it must be DECLARED in departed_instances,
// which is what stops a silent row loss from being read as a clean migration.
func TestRetiredKeys_RefreshRekey_SuccessorsAreLiveAndReDerived(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, refreshRekeyRetiredKeysCorpusJSON, refreshRekeyRetiredKeyCount)
	home := instanceHomes(t)

	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; a migration record for a key that held "+
					"no rows is unfalsifiable — pin the instances it held at the release bake", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors, but it is in the RE-KEYED corpus — a "+
					"key whose rows all departed belongs in the deletion set, which needs no record", key)
			}

			declaredDeparted := map[string]bool{}
			for _, inst := range exp.DepartedInstances {
				declaredDeparted[inst] = true
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					if declaredDeparted[inst] {
						continue
					}
					t.Errorf("instance %q (a v0.2.10 row of retired key %q) is in no live entity and is "+
						"not declared in departed_instances\n"+
						"  What: the row neither re-homed nor was recorded as removed upstream\n"+
						"  Why it matters: an undeclared disappearance reads as a clean migration; the\n"+
						"    successor set simply does not mention it\n"+
						"  How to fix: confirm against parse/data/modelsdev/catalog.json. If upstream\n"+
						"    removed the id, add it to departed_instances with the others; if it is still\n"+
						"    served, the row was dropped by the pipeline and that is a defect", inst, key)
					continue
				}
				if declaredDeparted[inst] {
					t.Errorf("instance %q is declared departed for retired key %q but IS live on %q — "+
						"delete the stale departed_instances entry rather than leaving a false record",
						inst, key, dest)
					continue
				}
				seen[dest] = true
			}
			got := sortedKeys(seen)

			want := append([]string(nil), exp.Successors...)
			sort.Strings(want)
			if !slices.Equal(got, want) {
				t.Errorf("retired key %q re-homes onto %v, but the migration record says %v\n"+
					"  What: the successor set re-derived from this key's own instances disagrees with\n"+
					"    the committed record\n"+
					"  Why it matters: the record is the only route a v0.2.10 user has from the retired\n"+
					"    key to the artifact; a wrong successor sends them to the wrong model\n"+
					"  How to fix: re-derive from the instances rather than editing the record to match",
					key, got, want)
			}

			// Every named successor must be LIVE. A migration record pointing at a key that
			// does not resolve is worse than none: it reads as a route and is a dead end.
			for _, s := range exp.Successors {
				if _, ok := bestiary.EntityByKey(s); !ok {
					t.Errorf("retired key %q names successor %q, which does not resolve\n"+
						"  How to fix: re-derive the successor from the key's instances; a migration\n"+
						"    record must never point at a key that is itself retired", key, s)
				}
			}
		})
	}
}
