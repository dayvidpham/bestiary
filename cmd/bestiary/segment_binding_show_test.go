package main

// The `show`-seam half of the both-seam non-regression sweep for canonical
// segment binding. Its resolver-seam twin lives beside the resolver.
//
// Why both seams are required, and why a resolver-only sweep is NOT sufficient:
// `bestiary show <ref>` resolves a MODEL first and reaches an entity's aggregate
// view only when that resolution MISSES. So a segment-binding rule that makes a
// bare entity key start resolving as a model reads as an improvement at the
// resolver — a ref that was not-found now returns rows — while at this seam the
// key silently stops rendering `Entity: …` and the user loses the aggregate view
// entirely. Measured on the prototype of this repair, an ungated date-to-version
// rebind cost hundreds of keys their entity view with a green resolver sweep.
//
// The guard is a SET IDENTITY over the live entity census rather than a literal
// count: every key that the CLI cannot resolve as a model must render its entity
// view here. A count would go stale the moment the catalog moves; the identity
// does not.

import (
	"errors"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// entityViewPins are members the set identity must always contain: each is a live
// entity key whose `show` view exists only because model resolution misses. They
// are named explicitly so a change that hollows out the swept population — making
// the sweep pass over an empty or tiny set — still reddens.
var entityViewPins = []string{
	"llama@3.3#70b",
	"glm@4.6",
	"claude/opus@4.0",
	"doubao@1.6{vision}",
}

// TestShowSeam_EveryModelUnresolvableEntityKeyRendersItsEntityView sweeps the
// whole entity census at the production CLI seam and asserts the invariant that
// keeps the aggregate view reachable.
func TestShowSeam_EveryModelUnresolvableEntityKeyRendersItsEntityView(t *testing.T) {
	tmpDB := t.TempDir() + "/cache.db"
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("registry minted no entities; the sweep would pass vacuously")
	}

	pinned := map[string]bool{}
	for _, k := range entityViewPins {
		pinned[k] = false
	}

	swept, failures := 0, 0
	for _, e := range entities {
		key := e.Ref.String()
		// The population under guard: keys the model resolver reports as NOT FOUND,
		// which are exactly the keys `show` hands to its entity fallback. A key that
		// resolves renders a model view, and a key that comes back ambiguous renders
		// the candidate table — both are pre-existing, deliberate behaviours of a
		// live family name and neither reaches the aggregate view.
		_, resolveErr := bestiary.Resolve(key, bestiary.WithInputFormat(bestiary.InputFormatPeasant))
		var notFound *bestiary.ErrNotFound
		if !errors.As(resolveErr, &notFound) {
			continue
		}
		swept++
		if _, ok := pinned[key]; ok {
			pinned[key] = true
		}
		out := captureStdout(t, func() {
			if err := run([]string{"show", key, "--db-path", tmpDB, "--output=table"}); err != nil {
				t.Errorf("show %q returned %v; a model-unresolvable entity key must still render its entity view", key, err)
			}
		})
		if !strings.Contains(out, "Entity: "+e.PreferredName()) {
			failures++
			if failures <= 5 {
				t.Errorf("show %q did not render its entity view (want header %q). The model resolver captured "+
					"a bare entity key, so `show` never reached the entity fallback.\noutput:\n%s",
					key, "Entity: "+e.PreferredName(), out)
			}
		}
	}
	if failures > 5 {
		t.Errorf("... and %d further keys lost their entity view (only the first 5 are listed)", failures-5)
	}

	// Non-vacuity: the swept population must be substantial AND must contain every
	// pinned member. A repair that quietly resolved most keys as models would
	// otherwise shrink this sweep toward nothing and still pass.
	if swept < len(entities)/4 {
		t.Errorf("only %d of %d entity keys are model-unresolvable; the entity-fallback population has "+
			"collapsed, which is the very regression this sweep exists to catch", swept, len(entities))
	}
	for key, present := range pinned {
		if !present {
			t.Errorf("pinned entity-view member %q was not swept: it is either no longer a live entity key "+
				"or it now resolves as a model, and in the latter case it has lost its aggregate view", key)
		}
	}
	t.Logf("swept %d of %d entity keys at the show seam (model-unresolvable, entity-fallback dependent)", swept, len(entities))
}
