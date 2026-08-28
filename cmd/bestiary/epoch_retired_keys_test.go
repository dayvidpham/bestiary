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

// epochRetiredKeyLibraryShowDisagreements and epochRetiredKeyAnySeamDisagreements pin
// the two seam-disagreement figures the doc comments above publish, in KEYS out of the
// 62-key cumulative retired set. They are asserted against the committed corpus by
// TestEpochRetiredKeys_MeasuredPolicySplit rather than transcribed into prose alone, so
// a corpus row changing its seam columns fails here instead of silently invalidating the
// documentation.
//
// 14 = 3 (`gpt-luna`, `gpt-sol`, `gpt-terra`: AMBIGUOUS at the bare library call,
// NOT-FOUND at `bestiary show`) + 8 (NOT-FOUND bare, AMBIGUOUS at `show`) + 3
// (NOT-FOUND bare, RESOLVED at `show`: `ministral#3b{instruct}`,
// `mistral/large#675b{instruct}`, `nemotron#120b`).
//
// 18 = those same 14 + 4 keys that agree at library and `show` but diverge at
// `show --by-entity` (`agi`, `ling`, `mimo`, `kimi{instruct}`: AMBIGUOUS at both of
// those, NOT-FOUND by entity).
const (
	epochRetiredKeyLibraryShowDisagreements = 14
	epochRetiredKeyAnySeamDisagreements     = 18
)

// epochRetiredKeySeams is the expected outcome for one cumulatively-retired key at each
// of the three seams it is reachable through. They are pinned SEPARATELY because they
// measurably disagree, in BOTH directions, and collapsing them into one "the retired key
// does X" claim is false for 18 of the 62 keys (epochRetiredKeyAnySeamDisagreements,
// asserted below against the corpus rather than transcribed).
//
// Library is the outcome of a BARE bestiary.Resolve(key) — zero ResolveOptions, so the
// resolver auto-detects the input scheme. This is a library-API reading only: NO shipped
// code path calls Resolve without options, so a row's Library value must never be read as
// "what a user sees". It is pinned because it is the seam where the error TYPE survives —
// the CLI's ambiguous branch deliberately prints its candidate listing and returns a
// fresh unwrapped error, so the typed *bestiary.ErrAmbiguous is only observable here.
//
// CLIShow is the outcome of `bestiary show <key>` — the DEFAULT, production seam, and
// the one a user actually reaches. cmd/bestiary/main.go passes
// WithInputFormat(InputFormatPeasant) whenever --format/--scheme are unset (i.e. almost
// always), which parses the input as a canonical tuple instead of auto-detecting it.
// That reading differs from the bare Library call for 14 keys
// (epochRetiredKeyLibraryShowDisagreements), in both directions:
//
//   - `gpt-luna`, `gpt-sol`, `gpt-terra` are AMBIGUOUS bare and NOT-FOUND here. Bare
//     Resolve takes the variant-aware bare-family fallback (`gpt` is a live Family,
//     `luna` names a Variant in it); Peasant parses the whole string as a Family, which
//     no longer exists after the gpt tier re-key.
//   - 8 keys are NOT-FOUND bare and AMBIGUOUS here, and 3 more are NOT-FOUND bare and
//     RESOLVE here (`ministral#3b{instruct}`, `mistral/large#675b{instruct}`,
//     `nemotron#120b`). Their `#size` / `/variant` punctuation disqualifies the
//     bare-identifier fallback, so auto-detect reads them as raw ids and misses; the
//     Peasant canonical parse finds the live entities.
//
// CLIByEntity is the outcome of `bestiary show <key> --by-entity`. It is pinned
// SEPARATELY from the exact-key seam because the two are not the same lookup: the CLI
// matches its input against the store-overlaid entity index by entity key, entity
// preferred name or concrete model id, so a retired key whose spelling is still a live
// concrete model id can answer there while the exact-key lookup 404s. It has no
// short-reference path, so it never returns the under-specified error.
//
// CoveredBy names the per-lever retired-key corpora that also carry this key. It is the
// reconciliation datum: a key with an EMPTY CoveredBy is recorded here and nowhere else,
// which the reconciliation test asserts is true of exactly the keys it expects.
type epochRetiredKeySeams struct {
	Library     string   `json:"library"`
	CLIShow     string   `json:"cli_show"`
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
// The rule this probe enforces, stated per seam:
//
//	A retired key is not found at the exact-key seams: EntityByKey, GET /entity/<key>.
//	The CLI resolver keeps its short-reference fallback. An old spelling that is still a
//	valid short reference can resolve or show candidates.
//
// So ONE invariant holds without exception, and it is the one the retired-key policy is
// actually about: the EXACT-key lookup — bestiary.EntityByKey, and the web route
// GET /entity/<key> that dereferences through it — is a hard 404 for all 62. No alias is
// minted, no redirect is added, no successor is listed and no nomen claim resurrects a
// retired key. The CHANGELOG migration tables are the only pointer a user gets.
//
// `bestiary show` and `show --by-entity` are neither the exact-key seams nor each other:
// `--by-entity` is an exact match over the store-overlaid entity index (entity key,
// entity preferred name or concrete model id) with no short-reference path, while plain
// `show` runs the input through the model resolver, which keeps that fallback. Both are
// pinned PER KEY below against the measured split.
//
// The looser seams are pinned PER KEY against the MEASURED split, not against a blanket
// rule, because a blanket rule is false here in several directions at once. Each bullet
// below is a measured class, and the seam a claim is about is always named, because the
// bare library call and the shipped `bestiary show` DISAGREE for 14 keys (see the
// epochRetiredKeySeams doc for the two mechanisms).
//
// At the seam a user actually reaches — `bestiary show <key>`, which defaults to
// WithInputFormat(InputFormatPeasant) — the split is 45 not-found, 12 under-specified
// and 5 resolved:
//
//   - 12 keys return the under-specified reading from `bestiary show` rather than a 404.
//     Ten of them are DISTINCT-ENTITY ambiguity: the string still names several live and
//     genuinely different entities (`agi`, `ling`, `mimo`, `gpt/pro`, `devstral#123b`,
//     `gemma#4b`, `gemma#12b`, `gemma#26b-a4b`, `ministral#8b{instruct}`,
//     `mistral/small#24b`, `nemotron#30b-a3b` — eleven, less `kimi{instruct}` below).
//     This is live-family behaviour — the same reading `show gpt` and `show claude` have
//     always produced — and it must NEVER be "fixed" into a 404, which would 404 the
//     live family itself. A test asserting not-found on every seam for all 62 would be
//     RED, and making it green would break working lookups.
//
//   - `kimi{instruct}` is under-specified for a DIFFERENT reason, and is filed apart from
//     the class above on purpose: it is NOT a surviving bare family. All nine candidates
//     share one identical tuple (family=kimi, variant=k, version=2, modifier=[instruct])
//     and differ ONLY by `date`, so the ambiguity is the ordinary single-entity
//     date-fragmentation any multi-instance entity produces. The surviving bare family
//     here is `kimi`; `kimi{instruct}` is a variant+modifier compound.
//
//   - `gpt-luna`, `gpt-sol` and `gpt-terra` are under-specified ONLY at the bare library
//     call and are a plain 404 at `bestiary show`. They are pinned that way in both
//     columns, and gpt_tier_rekey_retired_keys_corpus.json records the same not-found.
//     Nothing about them "reads like `show gpt`" for a user: bare Resolve reaches them
//     through the variant-aware bare-family fallback, and no shipped code path calls
//     Resolve without WithInputFormat.
//
//   - 5 keys RESOLVE at `bestiary show`. Two of them, `kimi-k2` and `kimi-k3`, also
//     answer on `show --by-entity`, which every other retired key 404s on. They are the
//     upstream raw_family spellings, and both are still LIVE concrete model ids, so
//     `--by-entity` finds them through its concrete-model-id arm — which is why these two
//     are its only exceptions in the epoch. The other three (`ministral#3b{instruct}`,
//     `mistral/large#675b{instruct}`, `nemotron#120b`) answer at `show` alone, as
//     under-specified references the model resolver matches to one live entity;
//     `--by-entity` has no short-reference path, so it 404s on them. The exact-key lookup
//     above 404s on all five. Recorded as measured deviations, not repaired.
func TestEpochRetiredKeys_MeasuredPolicySplit(t *testing.T) {
	corpus := loadEpochRetiredKeyCorpus(t)

	// Value-based coverage: the readings a regression reaches first — one key from each
	// measured class, and the key no per-lever corpus covers.
	for _, want := range []string{
		"ling", "mimo", "agi", // distinct-entity under-specified at BOTH seams
		"kimi{instruct}",                   // single-entity date-fragmentation
		"gpt-luna", "gpt-sol", "gpt-terra", // under-specified bare, 404 at `show`
		"gemma#12b", "nemotron#120b", // 404 bare, ambiguous / resolved at `show`
		"kimi-k2", "kimi-k3", // the by-entity deviation
		"qwen/coder@3#1m", // the epoch-only key
	} {
		if !epochCorpusHas(corpus, want) {
			t.Errorf("epoch retired-key corpus lost coverage of %q", want)
		}
	}

	// The published seam-disagreement figures, DERIVED from the corpus. The doc comments
	// above quote both; this is what keeps the prose from drifting away from the data.
	libShow, anySeam := 0, 0
	for _, c := range corpus.Cases {
		e := c.Expected
		if e.Library != e.CLIShow {
			libShow++
		}
		if e.Library != e.CLIShow || e.CLIShow != e.CLIByEntity {
			anySeam++
		}
	}
	if libShow != epochRetiredKeyLibraryShowDisagreements {
		t.Errorf("the bare library call and `bestiary show` disagree for %d of the %d retired keys, "+
			"but the doc comments above publish %d. Re-derive the figure from this corpus and "+
			"correct BOTH the constant and the prose that quotes it.",
			libShow, len(corpus.Cases), epochRetiredKeyLibraryShowDisagreements)
	}
	if anySeam != epochRetiredKeyAnySeamDisagreements {
		t.Errorf("%d of the %d retired keys have at least two of the three seams disagreeing, but "+
			"the doc comments above publish %d. Re-derive the figure from this corpus and correct "+
			"BOTH the constant and the prose that quotes it.",
			anySeam, len(corpus.Cases), epochRetiredKeyAnySeamDisagreements)
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

			// The LIBRARY seam, where the error TYPE survives. This is a bare
			// bestiary.Resolve — auto-detect, zero options — which is a library-API
			// reading only; the seam a user reaches is CLIShow below, and for 14 keys
			// the two disagree. Read a failure here as "the library contract moved",
			// never as "the CLI moved".
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

			// The DEFAULT `bestiary show` seam — the production path, and the reading
			// a user gets when they omit --format/--scheme (i.e. almost always). It runs
			// the real run() entry point, so it exercises main.go's
			// WithInputFormat(InputFormatPeasant) default rather than re-deriving it.
			assertRunSeam(t, exp.CLIShow, key,
				[]string{"show", key, "--db-path", tmpDB, "--output=table"})

			// The `show --by-entity` CLI seam.
			assertRunSeam(t, exp.CLIByEntity, key,
				[]string{"show", key, "--by-entity", "--db-path", tmpDB, "--output=table"})
		})
	}
}

// TestEpochRetiredKeys_ReconcileWithPerLeverCorpora is the coverage falsifier for the
// release's retired-key record. The per-lever corpora and this epoch corpus are two
// independent accounts of the same set, and the release is only fully recorded when they
// agree: every key a lever retired must appear here, every key here must either be
// carried by a per-lever corpus or be declared, with a reason, in epochOnlyRetiredKeys,
// and — where both accounts pin the SAME command — they must give the same answer. That
// last direction is what keeps this corpus's CLI columns honest: they pin `bestiary show`
// and `show --by-entity`, the two commands the per-lever corpora already measured key by
// key, so a column drifting away from the lever record fails here instead of quietly
// generalizing a reading no seam produces.
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
	leverSeams := map[string]map[string]retiredKeySeams{}
	for _, f := range perLeverRetiredCorpora(t) {
		for _, k := range f.keys {
			union[k] = append(union[k], f.name)
		}
		leverSeams[f.name] = f.seams
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

	// Direction 3: the two accounts must agree on WHAT each key does, not merely on which
	// keys retired. The per-lever corpora pin `bestiary show <key>` and
	// `show <key> --by-entity` for the keys their lever retired; the epoch corpus pins the
	// same two commands for all 62. They were authored independently, so a disagreement
	// means one of them is stale — and this is the check that catches a seam column
	// silently drifting away from the lever record it is supposed to generalize.
	for _, c := range corpus.Cases {
		for _, file := range union[c.Input] {
			lever, ok := leverSeams[file][c.Input]
			if !ok {
				continue
			}
			if lever.Show != c.Expected.CLIShow {
				t.Errorf("seam disagreement for %q: %s records `show` = %q, the epoch corpus records "+
					"cli_show = %q. Both pin the SAME command, so one account is stale; re-measure "+
					"before editing either.", c.Input, file, lever.Show, c.Expected.CLIShow)
			}
			if lever.ByEntity != c.Expected.CLIByEntity {
				t.Errorf("seam disagreement for %q: %s records `by_entity` = %q, the epoch corpus "+
					"records cli_by_entity = %q. Both pin the SAME command, so one account is stale; "+
					"re-measure before editing either.", c.Input, file, lever.ByEntity, c.Expected.CLIByEntity)
			}
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
	// seams is the per-lever corpus's own reading of the two CLI seams, keyed by input.
	// It is carried so the reconciliation can falsify the epoch corpus's CLI columns
	// against an INDEPENDENTLY authored measurement of the same commands, rather than
	// only checking that the two accounts name the same keys.
	seams map[string]retiredKeySeams
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
		seams := make(map[string]retiredKeySeams, len(c.Cases))
		for i := range c.Cases {
			keys = append(keys, c.Cases[i].Input)
			seams[c.Cases[i].Input] = c.Cases[i].Expected
		}
		out = append(out, perLeverCorpus{name: name, keys: keys, seams: seams})
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
