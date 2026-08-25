package main

import (
	_ "embed"
	"errors"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// retiredKeyInput is one retired entity key, spelled exactly as it appeared in the
// generated constants before the demotion.
type retiredKeyInput = string

// retiredKeySeams is the expected outcome on each of the two user-facing lookup seams.
// The values are the measured split, not an aspiration: "not-found" means the seam
// returns *bestiary.ErrNotFound, "ambiguous" means *bestiary.ErrAmbiguous.
type retiredKeySeams struct {
	ByEntity string `json:"by_entity"`
	Show     string `json:"show"`
}

//go:embed testdata/retired/free_demotion_retired_keys_corpus.json
var freeDemotionRetiredKeysCorpusJSON []byte

// TestRetiredKeys_FreeDemotion_UniformHardNotFound pins the retired-key policy for
// every key the global free-tier demotion retires, at the two production seams a user
// actually reaches: `bestiary show <key> --by-entity` (the exact-key entity lookup) and
// `bestiary show <key>` (the looser model resolver with its entity fallback).
//
// The policy is a uniform hard 404 — no alias is minted, no redirect is added, and no
// successor is listed. The CHANGELOG old -> new migration table is the only pointer a
// user gets, and the corpus's per-case mutation line carries the same mapping so the two
// records cannot drift apart silently.
//
// This is a VERIFICATION pin, not a behaviour the slice implemented: nothing was written
// to make it hold. Its value is that it fails loudly if some later change quietly
// resurrects a retired key — via an alias seed, a nomen claim, or a family-override row
// that re-mints the old tuple.
//
// It is deliberately NOT asserted that the looser `show` seam 404s for EVERY conceivable
// retired key. Two keys elsewhere in this epoch (bare `ling`, bare `mimo`) come back
// ambiguous instead, because the FAMILY survives while the key retires, so the bare
// family token still has live children. Neither is in this lever's set, and neither must
// ever be "fixed" into a 404 — that would break three unrelated live-family lookups. The
// corpus therefore carries a per-key expectation rather than one blanket rule.
func TestRetiredKeys_FreeDemotion_UniformHardNotFound(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, freeDemotionRetiredKeysCorpusJSON, 17)

	// Value-based coverage: a count-preserving swap must not be able to drop the two
	// keys whose whole FAMILY vanished with them (the phantom-family case) or the three
	// mimo keys whose version was fused into the variant slot — those are the readings a
	// regression is most likely to reach first.
	for _, want := range []string{
		"deepseek-flash/free", "minimax-m3/free",
		"mimo/v2.5", "mimo/v2.5-pro", "mimo/omni-free",
		"glm/free", "kimi/free", "minimax/free", "nemotron/free@3",
	} {
		if !corpusHasInput(corpus, want) {
			t.Errorf("corpus lost coverage of retired key %q", want)
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

// TestRetiredKeys_FreeDemotion_PinnedSurvivorStaysLive is the negative control for the
// test above: the demotion is global, but one key is carved out of it by an exact-ID
// pin, and a "retire every free-bearing key" reading would wrongly 404 it. Both seams
// must still resolve it, and its sibling must keep its own instances.
func TestRetiredKeys_FreeDemotion_PinnedSurvivorStaysLive(t *testing.T) {
	const survivor = "ling/flash-free@2.6"
	const sibling = "ling/flash@2.6"

	e, ok := bestiary.EntityByKey(survivor)
	if !ok {
		t.Fatalf("EntityByKey(%q) does not resolve; the exact-ID pin that carves this key out "+
			"of the free demotion is not firing", survivor)
	}
	if got := len(e.Instances); got != 1 {
		t.Errorf("%s has %d instance(s), want 1 (the sole opencode row the pin preserves)", survivor, got)
	}

	sib, ok := bestiary.EntityByKey(sibling)
	if !ok {
		t.Fatalf("EntityByKey(%q) does not resolve", sibling)
	}
	if got := len(sib.Instances); got != 4 {
		t.Errorf("%s has %d instance(s), want 4 — the carved-out row must NOT have folded into it",
			sibling, got)
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, args := range [][]string{
		{"show", survivor, "--by-entity", "--db-path", tmpDB, "--output=table"},
		{"show", survivor, "--db-path", tmpDB, "--output=table"},
	} {
		var runErr error
		out := captureStdout(t, func() { runErr = run(args) })
		if runErr != nil {
			t.Errorf("run %v returned %v; the pinned survivor must still resolve", args, runErr)
			continue
		}
		if !strings.Contains(out, survivor) {
			t.Errorf("run %v did not render %q\noutput:\n%s", args, survivor, out)
		}
	}
}

// assertRunSeam drives one CLI seam and checks the error against the corpus's expected
// outcome for that seam.
func assertRunSeam(t *testing.T, want, key string, args []string) {
	t.Helper()
	var runErr error
	captureStdout(t, func() { runErr = run(args) })

	var notFound *bestiary.ErrNotFound
	var ambiguous *bestiary.ErrAmbiguous
	switch want {
	case "not-found":
		if !errors.As(runErr, &notFound) {
			t.Errorf("run %v returned %v, want *bestiary.ErrNotFound — a retired key is a hard 404 "+
				"on this seam; no alias, redirect or successor listing may resolve %q", args, runErr, key)
		}
	case "ambiguous":
		if !errors.As(runErr, &ambiguous) {
			t.Errorf("run %v returned %v, want *bestiary.ErrAmbiguous — this key retires while its "+
				"FAMILY stays live, so the bare family token still has live children", args, runErr)
		}
	default:
		t.Fatalf("corpus case for %q declares seam expectation %q, want \"not-found\" or \"ambiguous\"", key, want)
	}
}

// loadRetiredKeyCorpus loads a retired-key corpus under the three-guard discipline: an
// EXACT case count (a floor would let a silent drop pass), non-vacuity via Validate, and
// — at the call site — a value-based coverage assertion a count-preserving swap cannot
// slip past.
func loadRetiredKeyCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[retiredKeyInput, retiredKeySeams] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[retiredKeyInput, retiredKeySeams](data)
	if err != nil {
		t.Fatalf("load retired-key corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("retired-key corpus has %d cases, want exactly %d — the count is the retired-key "+
			"set itself, so update the literal in the same commit as the key diff it records", got, wantN)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("retired-key corpus is under-populated: %v", err)
	}
	return corpus
}

// corpusHasInput reports whether the corpus covers the given retired key.
func corpusHasInput(c testcase.Corpus[retiredKeyInput, retiredKeySeams], key string) bool {
	for i := range c.Cases {
		if c.Cases[i].Input == key {
			return true
		}
	}
	return false
}
