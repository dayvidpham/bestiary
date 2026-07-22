package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// suppressionFenceInput is the entity a suppression seed entry applies to, spelled as
// the same tuple the seed file uses.
type suppressionFenceInput struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

func (in suppressionFenceInput) toEntityRef() bestiary.EntityRef {
	return bestiary.EntityRef{
		Family:    bestiary.Family(in.Family),
		Variant:   in.Variant,
		Version:   in.Version,
		ParamSize: in.ParamSize,
		Modifier:  in.Modifier,
	}
}

// suppressionFenceExpected is the LITERAL before/after pair a seed entry must produce:
// Key is the entity key (unchanged — suppression is never a key change) and Preferred
// is the naming the entry promotes.
type suppressionFenceExpected struct {
	Key       string `json:"key"`
	Preferred string `json:"preferred"`
}

// TestSuppressionSeed_PerEntryFenceParity is the ENTRY-DELETION-FAILS-A-TEST mechanism.
// Every seed entry must have exactly one literal before/after row in the fence corpus,
// and every row must correspond to a live entry. Deleting a seed entry therefore
// reddens here (parity) AND in the row assertion below (the preferred value reverts to
// the key), so no entry can be removed silently and no entry is decorative.
func TestSuppressionSeed_PerEntryFenceParity(t *testing.T) {
	corpus, err := testcase.LoadCorpus[suppressionFenceInput, suppressionFenceExpected](suppressionFenceCorpusJSON)
	if err != nil {
		t.Fatalf("load suppression fence corpus: %v", err)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("suppression fence corpus is under-populated: %v", err)
	}

	seed := bestiary.SuppressionSeed()
	if len(corpus.Cases) != len(seed) {
		t.Fatalf("suppression fence corpus has %d row(s) but the seed has %d entry/entries;\n"+
			"every seed entry needs exactly one literal before/after fence row in\n"+
			"testdata/entity/suppression_fence_corpus.json (and vice versa)",
			len(corpus.Cases), len(seed))
	}

	seedKeys := make(map[string]bestiary.SuppressionEntry, len(seed))
	for _, e := range seed {
		seedKeys[e.Entity.String()] = e
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ref := c.Input.toEntityRef()
			key := ref.String()
			if key != c.Expected.Key {
				t.Fatalf("fence row input renders key %q, but the row pins %q", key, c.Expected.Key)
			}
			entry, ok := seedKeys[key]
			if !ok {
				t.Fatalf("fence row pins entity %q, which has no entry in parse/data/suppression_seed.json", key)
			}
			if entry.Reason == "" {
				t.Fatalf("seed entry for %q carries no reason", key)
			}
			// AFTER: the preferred naming omits the redundant modifier.
			if got := bestiary.PreferredNomenValue(ref); got != c.Expected.Preferred {
				t.Fatalf("PreferredNomenValue(%q) = %q, want %q", key, got, c.Expected.Preferred)
			}
			// The pair must actually differ, or the entry does nothing.
			if c.Expected.Preferred == c.Expected.Key {
				t.Fatalf("fence row for %q pins an identical before/after pair; the entry suppresses nothing", key)
			}
			// The KEY spelling stays resolvable as an admitted canonical nomen.
			assertCanonicalStatus(t, key, bestiary.AcceptabilityAdmitted)
			assertCanonicalStatus(t, c.Expected.Preferred, bestiary.AcceptabilityPreferred)
		})
	}
}

// assertCanonicalStatus asserts that value is recorded as a canonical nomen carrying
// want — i.e. that both spellings of a suppressed entity really are resolvable, with
// the right acceptability rating.
func assertCanonicalStatus(t *testing.T, value string, want bestiary.AcceptabilityRating) {
	t.Helper()
	matches, ok := bestiary.NomenLookup(value)
	if !ok {
		t.Fatalf("NomenLookup(%q) found nothing; the spelling must stay resolvable", value)
	}
	for _, n := range matches {
		if n.Scheme == bestiary.NomenSchemeCanonical && n.Status == want {
			return
		}
	}
	t.Fatalf("NomenLookup(%q) has no canonical nomen with status %s: %+v", value, want, matches)
}

// TestSuppression_EmptySeedNoOpCensus is the EMPTY-SEED NO-OP CENSUS SWEEP: with the
// shipped (empty) seed, the naming layer must be byte-identical to its pre-suppression
// behaviour across the WHOLE catalog. Three independent readings of "no-op":
//
//  1. every entity's preferred name equals its key, byte for byte (the full-census
//     render diff);
//  2. no entity mints an extra canonical nomen — the canonical nomina count equals the
//     entity count exactly;
//  3. no canonical nomen is Admitted (the admitted-key spelling only ever appears under
//     an active seed entry).
//
// The sweep is non-vacuous by construction: it fails if the catalog is empty, and it
// pins the census size so a collapse cannot pass silently.
//
// WHY IT SKIPS ITSELF ON A NON-EMPTY SEED — this is a deliberate scope boundary, not a
// coverage gap. What this test asserts is a UNIVERSAL no-op ("suppression changes
// nothing, anywhere"), and that proposition is definitionally true only while the seed
// is empty: the first curated entry is *supposed* to change one entity's preferred
// naming, so continuing to demand universal no-op would assert the machinery does not
// work. Weakening the sweep to "no-op except the seeded entities" would also be wrong —
// it would silently stop policing the 974 entities it exists to police the moment one
// entry lands. So it steps aside cleanly, and three sibling fences take over:
//
//   - a MALFORMED entry never reaches here at all: ValidateSuppressionSeed (unknown
//     family, missing reason, a modifier the entity does not carry, duplicate entity)
//     and ValidateSuppression (absent entity, preferred-naming collision) both fail the
//     BAKE, loudly, before any output is produced;
//   - a VALID entry's correctness is TestSuppressionSeed_PerEntryFenceParity's job: it
//     requires one literal before/after fence row per entry and asserts both spellings
//     resolve with the right acceptability — per entry, which is the right granularity
//     for a per-entry policy;
//   - the machinery itself stays covered unconditionally by
//     TestSuppression_SyntheticEntry_EndToEnd, which injects a synthetic seed into the
//     production functions and so never depends on what the shipped seed contains.
//
// The skip is therefore load-bearing information: reaching it means "the seed grew, go
// read the per-entry fence", and the skip message says exactly that.
func TestSuppression_EmptySeedNoOpCensus(t *testing.T) {
	if got := len(bestiary.SuppressionSeed()); got != 0 {
		t.Skipf("suppression seed is no longer empty (%d entries), so the UNIVERSAL no-op this sweep asserts "+
			"no longer holds by design; per-entry correctness is TestSuppressionSeed_PerEntryFenceParity's job, "+
			"malformed entries are caught at the bake by ValidateSuppressionSeed/ValidateSuppression, and the "+
			"machinery stays covered by TestSuppression_SyntheticEntry_EndToEnd", got)
	}

	entities := bestiary.Entities()
	if len(entities) < 900 {
		t.Fatalf("entity census collapsed to %d entities; the sweep would be vacuous", len(entities))
	}

	diffs := 0
	keys := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		key := e.Ref.String()
		keys[key] = struct{}{}
		if got := e.PreferredName(); got != key {
			diffs++
			if diffs <= 5 {
				t.Errorf("empty-seed render diff: entity %q renders preferred name %q", key, got)
			}
		}
		if got := bestiary.SuppressedModifiers(e.Ref); got != nil {
			t.Errorf("empty-seed lookup hit: SuppressedModifiers(%q) = %v, want nil", key, got)
		}
	}
	if diffs > 0 {
		t.Fatalf("%d of %d entity renders differ from their key under an EMPTY seed; suppression must be a total no-op",
			diffs, len(entities))
	}

	canonical, admitted := 0, 0
	for _, n := range bestiary.Nomina() {
		if n.Scheme != bestiary.NomenSchemeCanonical {
			continue
		}
		canonical++
		if n.Status == bestiary.AcceptabilityAdmitted {
			admitted++
			if admitted <= 5 {
				t.Errorf("empty-seed status diff: canonical nomen %q is Admitted", n.Value)
			}
		}
		if _, ok := keys[n.Value]; !ok {
			t.Errorf("empty-seed value diff: canonical nomen %q is not an entity key", n.Value)
		}
	}
	if admitted != 0 {
		t.Fatalf("%d canonical nomina are Admitted under an EMPTY seed; only an active seed entry may demote a key spelling", admitted)
	}
	if canonical != len(entities) {
		t.Fatalf("minted %d canonical nomina for %d entities; an empty seed must mint exactly one per entity",
			canonical, len(entities))
	}
}
