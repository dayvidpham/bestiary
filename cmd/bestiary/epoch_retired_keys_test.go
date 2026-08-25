package main

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/retired/epoch_retired_keys_corpus.json
var epochRetiredKeysCorpusJSON []byte

// epochRetiredKeyCount is the size of the release's CUMULATIVE retired set, measured as
// the difference between the entity keyspace at the release baseline and the keyspace at
// this tip: 957 keys before, 930 after, 62 retired and 35 added (the two figures do not
// net to the key delta on their own — 957 - 62 + 35 = 930 does). It is the exact-count
// control for the corpus below, and it IS the retired set, so it moves only in the same
// commit as a measured cumulative key diff.
//
// The plan projected 41. That projection was made before four of the levers were
// measured and is superseded by this count, which is derived from the diff rather than
// forecast.
const epochRetiredKeyCount = 62

// epochRetiredKeySeams is the expected outcome for one cumulatively-retired key.
//
// Library is the outcome of bestiary.Resolve — the seam where the error TYPE survives.
// The CLI's ambiguous branch deliberately prints its candidate listing and returns a
// fresh unwrapped error, so the typed *bestiary.ErrAmbiguous is only observable here.
//
// CLIByEntity is the outcome of `bestiary show <key> --by-entity`. It is pinned
// SEPARATELY from the exact-key seam because the two are not the same thing: the CLI
// resolves its input through the model resolver before reaching the entity view, so a
// retired key whose spelling is still a valid under-specified reference can answer there
// while the exact-key lookup 404s.
//
// CoveredBy names the per-lever retired-key corpora that also carry this key. It is the
// reconciliation datum: a key with an EMPTY CoveredBy is recorded here and nowhere else,
// which the reconciliation test asserts is true of exactly the keys it expects.
type epochRetiredKeySeams struct {
	Library     string   `json:"library"`
	CLIByEntity string   `json:"cli_by_entity"`
	CoveredBy   []string `json:"covered_by"`
}

// epochOnlyRetiredKeys enumerates the cumulatively-retired keys that NO per-lever corpus
// covers, with the reason. The list is committed rather than derived so that a key
// silently falling out of a per-lever corpus cannot quietly join it.
//
// There is exactly one. The suppress-pin that retired it landed as a re-derivation
// in-tree and authored no retired-key corpus of its own, so a review pass flagged it as
// a coverage gap that the epoch-wide probe had to close. This corpus is the only
// committed record of its retirement.
var epochOnlyRetiredKeys = map[string]string{
	"qwen/coder@3#1m": "retired by the qwen3-coder-next suppress-pin extended to the unprefixed " +
		"spelling; that landing authored no per-lever retired-key corpus, so this probe is its " +
		"only committed coverage",
}

// TestEpochRetiredKeys_MeasuredPolicySplit is the release-wide retired-key probe. It
// takes every key present in the entity keyspace at the release baseline and absent at
// this tip, and pins what each one measurably does at the seams a user reaches.
//
// ONE invariant holds without exception, and it is the one the uniform-404 policy is
// actually about: the EXACT-key entity lookup, bestiary.EntityByKey, is a hard 404 for
// all 62. No alias is minted, no redirect is added, no successor is listed and no
// nomen claim resurrects a retired key. The CHANGELOG migration tables are the only
// pointer a user gets.
//
// The looser seams are pinned PER KEY against the MEASURED split, not against a blanket
// rule, because a blanket rule is false here in two different directions:
//
//   - 7 keys return the typed under-specified error from bestiary.Resolve rather than
//     not-found, because the string still has LIVE CHILDREN after the key retires. The
//     plan named two of them (`ling`, `mimo`); the measured set is `agi`, `gpt-luna`,
//     `gpt-sol`, `gpt-terra`, `kimi{instruct}`, `ling` and `mimo`. This is live-family
//     behaviour — the same reading `show gpt` and `show claude` have always produced —
//     and it must NEVER be "fixed" into a 404, which would 404 the live family itself.
//     A test asserting not-found on both seams for all 62 would be RED, and making it
//     green would break working lookups.
//
//   - 2 keys, `kimi-k2` and `kimi-k3`, ANSWER on `show --by-entity`, which every other
//     retired key 404s on. They are the upstream raw_family spellings, and after the
//     series-compound recovery reduced them they remain valid UNDER-SPECIFIED references
//     matching exactly one live entity each. The CLI resolves its input through the
//     model resolver before reaching the entity view, so it finds them; the exact-key
//     lookup above still 404s. Recorded as a measured deviation, not repaired.
func TestEpochRetiredKeys_MeasuredPolicySplit(t *testing.T) {
	corpus := loadEpochRetiredKeyCorpus(t)

	// Value-based coverage: the readings a regression reaches first — the invariant's
	// two exception classes, and the key no per-lever corpus covers.
	for _, want := range []string{
		"ling", "mimo", "agi", "kimi{instruct}", // the under-specified class
		"kimi-k2", "kimi-k3", // the by-entity deviation
		"qwen/coder@3#1m", // the epoch-only key
	} {
		if !epochCorpusHas(corpus, want) {
			t.Errorf("epoch retired-key corpus lost coverage of %q", want)
		}
	}

	tmpDB := t.TempDir() + "/cache.db"
	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			// The invariant. It admits no per-key exception in this set.
			if _, ok := bestiary.EntityByKey(key); ok {
				t.Errorf("EntityByKey(%q) still resolves. Every key in this corpus was present in "+
					"the keyspace at the release baseline and absent at this tip, so it is retired "+
					"and the exact-key seam must be a hard 404. A key coming back here means an "+
					"alias seed, a nomen claim or a family-override row re-minted the old tuple.", key)
			}

			// The library seam, where the error TYPE survives.
			_, err := bestiary.Resolve(key)
			var notFound *bestiary.ErrNotFound
			var ambiguous *bestiary.ErrAmbiguous
			switch exp.Library {
			case "not-found":
				if !errors.As(err, &notFound) {
					t.Errorf("bestiary.Resolve(%q) = %v, want *bestiary.ErrNotFound — this retired "+
						"key names nothing live and no bare family survives under it", key, err)
				}
			case "ambiguous":
				if !errors.As(err, &ambiguous) {
					t.Errorf("bestiary.Resolve(%q) = %v, want *bestiary.ErrAmbiguous — this key "+
						"retires while its string keeps LIVE CHILDREN. Do not repair this into a "+
						"not-found: that would 404 the live family, which is the behaviour "+
						"`show gpt` and `show claude` also rely on.", key, err)
				}
			case "resolved":
				if err != nil {
					t.Errorf("bestiary.Resolve(%q) = %v, want a successful resolution — this key is "+
						"recorded as a measured deviation whose retired spelling remains a valid "+
						"under-specified reference to one live entity. If that is no longer true, "+
						"RE-MEASURE the row rather than changing the resolver.", key, err)
				}
			default:
				t.Fatalf("corpus row for %q declares library outcome %q, which is not one of the "+
					"three measured outcomes: \"not-found\", \"ambiguous\" or \"resolved\"", key, exp.Library)
			}

			// The `show --by-entity` CLI seam.
			assertRunSeam(t, exp.CLIByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})
		})
	}
}

// TestEpochRetiredKeys_ReconcileWithPerLeverCorpora is the coverage falsifier for the
// release's retired-key record. The per-lever corpora and this epoch corpus are two
// independent accounts of the same set, and the release is only fully recorded when they
// agree: every key a lever retired must appear here, and every key here must either be
// carried by a per-lever corpus or be declared, with a reason, in epochOnlyRetiredKeys.
//
// The declared exception is what this test exists for. A key retired by a landing that
// authored no corpus of its own is invisible to every per-lever check, and reads as
// covered only because nothing looks for it. Naming it explicitly turns that silence
// into a committed statement.
func TestEpochRetiredKeys_ReconcileWithPerLeverCorpora(t *testing.T) {
	corpus := loadEpochRetiredKeyCorpus(t)

	epoch := map[string]bool{}
	for _, c := range corpus.Cases {
		epoch[c.Input] = true
	}

	union := map[string][]string{}
	for _, f := range perLeverRetiredCorpora(t) {
		for _, k := range f.keys {
			union[k] = append(union[k], f.name)
		}
	}

	// Direction 1: a per-lever corpus may not carry a key the cumulative diff does not
	// name — that would mean a key was recorded as retired and is in fact live, or the
	// cumulative diff was mis-measured.
	for k, files := range union {
		if !epoch[k] {
			t.Errorf("per-lever corpora %v record %q as retired, but it is not in the release's "+
				"cumulative retired set. Either the key is live again or the cumulative diff was "+
				"mis-measured; re-derive both before editing either.", files, k)
		}
	}

	// Direction 2: every cumulatively-retired key is either covered by a per-lever
	// corpus or explicitly declared as epoch-only.
	for _, c := range corpus.Cases {
		k := c.Input
		_, declared := epochOnlyRetiredKeys[k]
		covered := len(union[k]) > 0
		switch {
		case covered && declared:
			t.Errorf("%q is declared epoch-only but IS covered by %v; delete the "+
				"epochOnlyRetiredKeys entry so the declaration keeps meaning something", k, union[k])
		case !covered && !declared:
			t.Errorf("retired key %q is covered by NO per-lever corpus and is not declared in "+
				"epochOnlyRetiredKeys. A retired key no per-lever check looks for reads as covered "+
				"only because nothing looks for it. Either add it to the lever's corpus or declare "+
				"it here with the reason.", k)
		}
		// The corpus's own record of its coverage must match the measured union, so a
		// row cannot claim coverage it does not have.
		got := append([]string(nil), c.Expected.CoveredBy...)
		want := append([]string(nil), union[k]...)
		sort.Strings(got)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Errorf("corpus row for %q records coverage %v, measured coverage is %v", k, got, want)
		}
	}

	// A declaration for a key that is not retired at all is stale.
	for k := range epochOnlyRetiredKeys {
		if !epoch[k] {
			t.Errorf("epochOnlyRetiredKeys declares %q, which is not in the release's cumulative "+
				"retired set; drop the stale declaration", k)
		}
	}
}

type perLeverCorpus struct {
	name string
	keys []string
}

// perLeverRetiredCorpora reads every per-lever retired-key corpus in the shared
// directory EXCEPT this epoch corpus. It globs rather than listing the files, so a new
// lever's corpus joins the reconciliation automatically instead of needing a second
// place to be registered.
func perLeverRetiredCorpora(t *testing.T) []perLeverCorpus {
	t.Helper()
	paths, err := filepath.Glob("testdata/retired/*.json")
	if err != nil {
		t.Fatalf("glob retired-key corpora: %v", err)
	}
	var out []perLeverCorpus
	for _, p := range paths {
		name := filepath.Base(p)
		if name == "epoch_retired_keys_corpus.json" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		c, err := testcase.LoadCorpus[retiredKeyInput, retiredKeySeams](data)
		if err != nil {
			t.Fatalf("load %s: %v", p, err)
		}
		keys := make([]string, 0, len(c.Cases))
		for i := range c.Cases {
			keys = append(keys, c.Cases[i].Input)
		}
		out = append(out, perLeverCorpus{name: name, keys: keys})
	}
	if len(out) == 0 {
		t.Fatal("no per-lever retired-key corpora found; the reconciliation would pass vacuously")
	}
	return out
}

// loadEpochRetiredKeyCorpus loads the epoch corpus under the three-guard discipline: an
// EXACT case count (a floor would let a silent drop pass), non-vacuity via Validate, and
// — at the call site — a value-based coverage assertion.
func loadEpochRetiredKeyCorpus(t *testing.T) testcase.Corpus[retiredKeyInput, epochRetiredKeySeams] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[retiredKeyInput, epochRetiredKeySeams](epochRetiredKeysCorpusJSON)
	if err != nil {
		t.Fatalf("load epoch retired-key corpus: %v", err)
	}
	if got := len(corpus.Cases); got != epochRetiredKeyCount {
		t.Fatalf("epoch retired-key corpus has %d cases, want exactly %d — the count is the "+
			"release's cumulative retired set itself, so update the literal in the same commit as "+
			"the cumulative key diff it records", got, epochRetiredKeyCount)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

func epochCorpusHas(c testcase.Corpus[retiredKeyInput, epochRetiredKeySeams], key string) bool {
	for i := range c.Cases {
		if c.Cases[i].Input == key {
			return true
		}
	}
	return false
}
