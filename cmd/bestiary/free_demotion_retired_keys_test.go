package main

import (
	_ "embed"
	"errors"
	"os"
	"slices"
	"sort"
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
	// RetiredInstances is the MEASURED set of provider instances this key held
	// immediately BEFORE the demotion, each spelled "<provider>|<model id>". It was
	// read off the pre-lever generated catalog, not assumed, and it is the evidence
	// the successor set is re-derived from: an instance is the only thing that
	// actually survives a key retirement, so "where did this key's rows go" is a
	// question only its instances can answer.
	RetiredInstances []string `json:"retired_instances"`
	// Successors is the set of LIVE entity keys those instances now belong to,
	// sorted and de-duplicated. It is NOT "the key's bare parent": two keys in this
	// corpus split across three successors each, because peeling `free` exposed a
	// version the fused suffix had been hiding. Recorded here so the migration
	// record is machine-checkable rather than a prose claim.
	Successors []string `json:"successors"`
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
		// The CLI does NOT propagate *bestiary.ErrAmbiguous: on the ambiguous branch it
		// prints the candidate listing and returns a fresh, unwrapped fmt.Errorf carrying
		// the "narrow it" guidance (cmd/bestiary/main.go). That is deliberate production
		// behaviour — the user-facing text is the point of that branch — so this seam is
		// asserted on what it actually returns: a non-nil error that is specifically NOT a
		// not-found. The typed *bestiary.ErrAmbiguous is asserted at the LIBRARY seam
		// (bestiary.Resolve) by the caller, which is where the type survives.
		if runErr == nil {
			t.Errorf("run %v returned nil, want the under-specified error — this key retires while "+
				"its FAMILY stays live, so the bare family token still has live children", args)
			break
		}
		if errors.As(runErr, &notFound) {
			t.Errorf("run %v returned %v (a not-found), want the under-specified/ambiguous error — "+
				"%q retires as a KEY while its family stays live; turning it into a 404 breaks the "+
				"bare-family lookup that `show gpt`, `show claude` and `show mimo` also rely on",
				args, runErr, key)
			break
		}
		if !strings.Contains(runErr.Error(), "under-specified") {
			t.Errorf("run %v returned %v, want the under-specified error naming the candidates", args, runErr)
		}
		_ = ambiguous
	case "resolved":
		// A retired KEY whose string is still a valid UNDER-SPECIFIED reference to
		// exactly one live entity. The successor carries a field the retired key did
		// not (a version, typically), so a ref omitting that field still names one
		// model and the ordinary resolver answers it. No alias, redirect or
		// successor-listing instrument is involved, and the exact-key seam is still a
		// hard 404 — see the corpus rows that declare this outcome for why making it
		// fail instead would break working lookups.
		if runErr != nil {
			t.Errorf("run %v returned %v, want a successful resolution — %q is recorded as a "+
				"retired key whose string remains a valid under-specified reference to one live "+
				"entity; if that is no longer true, re-measure the row rather than changing the "+
				"resolver", args, runErr, key)
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

// TestRetiredKeys_FreeDemotion_SuccessorSetsMatchMeasuredRehoming is the falsifier for
// the migration record itself. The corpus's prose used to CLAIM where a retired key's
// rows went, and two of the seventeen claims were simply wrong: `kimi/free` and
// `minimax/free` were recorded as folding onto their bare family line, when peeling
// `free` actually exposed three different versions apiece and split each key across
// three successors. Nothing could catch that, because nothing checked it.
//
// This test closes that hole. Each case pins the instances the key held before the
// demotion; the successor set is then RE-DERIVED from those instances against the live
// registry and compared against the recorded one. A row that names the wrong successor
// — a bare parent that none of its instances actually landed on, a stale key, or a set
// that is missing one of a split's branches — fails here rather than misdirecting a user
// who is hunting for the model their key used to name.
//
// The pinned instances are the evidence, not an assumption: an instance is the only
// thing that survives a key retirement, so "where did this key's rows go" is a question
// only its instances can answer. They were read off the pre-lever generated catalog.
func TestRetiredKeys_FreeDemotion_SuccessorSetsMatchMeasuredRehoming(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, freeDemotionRetiredKeysCorpusJSON, 17)
	home := instanceHomes(t)

	for _, c := range corpus.Cases {
		key, exp := c.Input, c.Expected
		t.Run(c.Name, func(t *testing.T) {
			if len(exp.RetiredInstances) == 0 {
				t.Fatalf("retired key %q records no instances; the migration record for a key that "+
					"held no rows is unfalsifiable — pin the instances it held before the demotion", key)
			}
			if len(exp.Successors) == 0 {
				t.Fatalf("retired key %q records no successors; every retired key's rows re-home "+
					"somewhere (the instance total is conserved by this lever)", key)
			}

			seen := map[string]bool{}
			for _, inst := range exp.RetiredInstances {
				dest, ok := home[inst]
				if !ok {
					t.Errorf("instance %q (recorded as a pre-demotion row of retired key %q) is in no "+
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
					"row from the measured re-homing above (a key may SPLIT across several successors "+
					"— do not assume it folds onto its bare parent) and correct it here, in the "+
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

// TestRetiredKeys_FreeDemotion_ChangelogTableMatchesCorpus keeps the two copies of the
// migration record honest with each other. The CHANGELOG table is the copy a human
// reads and the corpus is the copy the tests re-derive, so a correction applied to one
// and forgotten in the other would leave the user-facing half wrong while every test
// stayed green — which is exactly how the kimi/minimax rows survived.
func TestRetiredKeys_FreeDemotion_ChangelogTableMatchesCorpus(t *testing.T) {
	corpus := loadRetiredKeyCorpus(t, freeDemotionRetiredKeysCorpusJSON, 17)

	const changelogPath = "../../CHANGELOG.md"
	raw, err := os.ReadFile(changelogPath)
	if err != nil {
		t.Fatalf("read %s: %v", changelogPath, err)
	}
	table := parseMigrationTable(t, string(raw), "| retired key | instances re-home to |")

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

// parseMigrationTable reads one "old -> new" markdown migration table out of the
// CHANGELOG, returning retired key -> successor keys. Every cell value is a backtick-
// quoted key, so the successor column parses by extracting its quoted tokens, which
// keeps a multi-successor row (a key that SPLITS) readable as a plain comma list.
//
// The table is addressed by its exact header row, because each curation lever writes its
// OWN table and the CHANGELOG accumulates several under one release. Matching on a shared
// prefix would silently bind every lever's test to whichever table happened to come
// first, so each header is required to be distinct and is passed in by the caller.
func parseMigrationTable(t *testing.T, changelog, header string) map[string][]string {
	t.Helper()

	lines := strings.Split(changelog, "\n")
	start := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) == header {
			start = i + 2 // skip the header and its |---|---| separator
			break
		}
	}
	if start < 0 {
		t.Fatalf("CHANGELOG has no migration table headed %q. That table is the record of record "+
			"for the lever that names it; if it moved or was renamed, update the caller in the "+
			"same commit so the record stays checked.", header)
	}

	out := map[string][]string{}
	for _, ln := range lines[start:] {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "|") {
			break
		}
		cells := strings.Split(strings.Trim(ln, "|"), "|")
		if len(cells) != 2 {
			t.Fatalf("migration table row %q has %d cell(s), want 2", ln, len(cells))
		}
		old := backtickedTokens(cells[0])
		if len(old) != 1 {
			t.Fatalf("migration table row %q names %d retired key(s) in its first cell, want exactly 1", ln, len(old))
		}
		news := backtickedTokens(cells[1])
		if len(news) == 0 {
			t.Fatalf("migration table row %q names no successor key", ln)
		}
		out[old[0]] = news
	}
	return out
}

// backtickedTokens returns every `quoted` token in a markdown table cell, in order.
func backtickedTokens(cell string) []string {
	var out []string
	for {
		i := strings.Index(cell, "`")
		if i < 0 {
			return out
		}
		rest := cell[i+1:]
		j := strings.Index(rest, "`")
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		cell = rest[j+1:]
	}
}

// instanceHomes maps every live provider instance, spelled "<provider>|<model id>", to
// the entity key that holds it. It is the measurement seam the successor sets are
// re-derived through.
func instanceHomes(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, e := range bestiary.Entities() {
		key := e.Ref.String()
		for _, in := range e.Instances {
			out[string(in.Provider)+"|"+string(in.ID)] = key
		}
	}
	if len(out) == 0 {
		t.Fatal("no live instances; the registry is empty, so no re-homing can be measured")
	}
	return out
}

// sortedKeys returns the set's members in ascending order.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
