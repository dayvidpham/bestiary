package main

import (
	_ "embed"
	"slices"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

//go:embed testdata/rehome/nemotron_rehome_corpus.json
var nemotronRehomeCorpusJSON []byte

// nemotronRehomeKeyCount is the number of LIVE keys this lever touches. Both existed
// before it and both exist after, which is the point: the count is 2 and it does not
// move, because no key is retired.
const nemotronRehomeKeyCount = 2

// nemotronRehomeMembership is the exact instance membership one live entity key must
// hold, each instance spelled "<provider>|<model id>" with the id's upstream casing.
type nemotronRehomeMembership struct {
	Instances []string `json:"instances"`
}

// TestNemotronRehome_BothKeysHoldExactlyTheirMeasuredInstances pins the nemotron
// Super-49B re-home as MEMBERSHIP on two live keys, which is the only shape that can
// state what this lever actually does.
//
// nano-gpt spells the v1.5 artifact with underscores
// (nvidia/Llama-3_3-Nemotron-Super-49B-v1_5). Underscores are not a separator the
// decomposition splits on, so that row arrived with an empty variant and version and
// keyed the bare `nemotron#49b` line — which already held the genuinely different
// Super-49B v1. Pinning the id to the tuple its dotted siblings carry moves it onto
// `nemotron/v1.5@3.3#49b`.
//
// BOTH keys already existed at the baseline, so this lever moves ONE INSTANCE AND RETIRES
// NO KEY. It is NOT a split and it carries no migration table: `nemotron#49b` survives,
// holding v1 alone. Asserting only the arrival on the v1.5 key would leave a retirement
// of the bare key indistinguishable from a re-home, so the surviving key's membership is
// pinned just as tightly as the receiving key's.
func TestNemotronRehome_BothKeysHoldExactlyTheirMeasuredInstances(t *testing.T) {
	corpus, err := testcase.LoadCorpus[string, nemotronRehomeMembership](nemotronRehomeCorpusJSON)
	if err != nil {
		t.Fatalf("load nemotron re-home corpus: %v", err)
	}
	if got := len(corpus.Cases); got != nemotronRehomeKeyCount {
		t.Fatalf("nemotron re-home corpus has %d cases, want exactly %d — the count is the set of "+
			"live keys this lever touches; a floor would let a silently dropped key pass",
			got, nemotronRehomeKeyCount)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("nemotron re-home corpus is under-populated: %v", err)
	}

	// Value-based coverage: a count-preserving swap must not be able to drop either half of
	// the statement — the key that SURVIVES, the key that RECEIVES, or the instance that
	// actually moved between them.
	byKey := map[string][]string{}
	for i := range corpus.Cases {
		byKey[corpus.Cases[i].Input] = corpus.Cases[i].Expected.Instances
	}
	const (
		bareKey  = "nemotron#49b"
		v15Key   = "nemotron/v1.5@3.3#49b"
		movedRow = "nano-gpt|nvidia/Llama-3_3-Nemotron-Super-49B-v1_5"
	)
	for _, k := range []string{bareKey, v15Key} {
		if _, ok := byKey[k]; !ok {
			t.Fatalf("corpus lost coverage of live key %q", k)
		}
	}
	if slices.Contains(byKey[bareKey], movedRow) {
		t.Errorf("the corpus still records %s on %q; that instance is the one the lever moves, "+
			"and leaving it here would describe the lever as a no-op", movedRow, bareKey)
	}
	// The arrival assertion used to be unconditional: movedRow HAD to be recorded on the
	// v1.5 key, because that arrival IS the lever. Its premise died at the 2026-08-28
	// models.dev catalog refresh — nano-gpt deleted the underscore-spelled row, and no
	// provider in the catalog publishes an underscore-spelled Super-49B id any more, so
	// there is no arrival left to record. Demanding one would demand a row that does not
	// exist; recording one anyway would be inventing an instance.
	//
	// So the claim is restated CONDITIONALLY, over the live registry rather than over the
	// corpus, and it still fails loudly the moment the spelling comes back: wherever
	// movedRow lives, it must live on the v1.5 key. That is the exact-ID pin's whole job,
	// and it is why the pin is kept even while it has nothing to act on.
	home := instanceHomes(t)
	if dest, live := home[movedRow]; live {
		if dest != v15Key {
			t.Errorf("%s is live and homes to %q, want %q — the exact-ID pin exists to keep the "+
				"underscore spelling off the bare 49B key", movedRow, dest, v15Key)
		}
		if !slices.Contains(byKey[v15Key], movedRow) {
			t.Errorf("%s is live again but the corpus does not record it on %q; that arrival IS "+
				"the lever, so re-measure both membership rows", movedRow, v15Key)
		}
	} else if slices.Contains(byKey[v15Key], movedRow) {
		t.Errorf("the corpus records %s on %q, but no provider serves that id any more; pin the "+
			"membership that upstream actually publishes", movedRow, v15Key)
	}

	total := 0
	for _, c := range corpus.Cases {
		key, want := c.Input, append([]string(nil), c.Expected.Instances...)
		total += len(want)
		t.Run(c.Name, func(t *testing.T) {
			e, ok := bestiary.EntityByKey(key)
			if !ok {
				t.Fatalf("EntityByKey(%q) does not resolve; BOTH keys in this set existed before the "+
					"lever and must exist after it — this lever retires no key, so a missing key here "+
					"means the re-home was implemented as a split", key)
			}
			var got []string
			for _, in := range e.Instances {
				got = append(got, string(in.Provider)+"|"+string(in.ID))
			}
			sort.Strings(got)
			sort.Strings(want)
			if !slices.Equal(got, want) {
				t.Errorf("%q holds instances %v, want %v — membership is the whole statement of this "+
					"lever, so a difference here is either a row that failed to move, a row that moved "+
					"too far, or a spelling that drifted upstream", key, got, want)
			}
		})
	}

	// Conservation across the pair: four instances before the lever, four after. A re-home
	// may not create or destroy a row, and counting the union rather than the two lists
	// separately means a duplicated instance fails here too.
	//
	// The 2026-08-28 models.dev catalog refresh left the total at four by coincidence, not
	// by conservation: two rows were deleted upstream (kilo's dotted v1.5, nano-gpt's
	// underscore v1.5) and two were added (NVIDIA's own dotted v1 and v1.5). Read the 4
	// below as a re-measured census of the pair; what conserves is the MEMBERSHIP asserted
	// per key above, which is where a row moving too far or failing to move shows up.
	union := map[string]bool{}
	for _, c := range corpus.Cases {
		for _, in := range c.Expected.Instances {
			if union[in] {
				t.Errorf("instance %s is recorded on more than one key; an instance lives in exactly "+
					"one entity", in)
			}
			union[in] = true
		}
	}
	if len(union) != 4 || total != 4 {
		t.Errorf("the two keys record %d instance(s) over %d row(s), want 4 and 4 — this lever moves "+
			"one instance between two live keys, so the total is conserved exactly", len(union), total)
	}
}
