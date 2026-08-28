package bestiary

// The resolver half of the both-seam non-regression sweep for canonical segment
// binding. It reaches the unexported pass seam, so it lives in the internal
// package; the `show`-seam half lives beside the CLI, and the peasant-seam
// candidate-set corpus lives in the external test package.
//
// Why a whole-registry sweep and not a handful of cases: the repair's two failure
// modes are both SET-shaped and neither is visible from any single input.
//
//  1. A repair rule that fires per-model instead of on a globally empty match set
//     widens refs that already resolved, turning unique resolutions into
//     ambiguities. TestSegmentBinding_RepairPassesNeverWidenAResolvedRef sweeps
//     every entity key and every catalog id for that.
//  2. A date-to-version rebind without its provider-strip gate swallows bare
//     entity keys into model resolution. That looks harmless at the resolver —
//     the ref starts resolving, which reads like an improvement — while at the
//     CLI it silently costs the key its aggregate entity view, because `show`
//     reaches that view only when model resolution MISSES. Measured on the
//     prototype, an ungated rule cost hundreds of keys their entity view while a
//     resolver-only sweep stayed green.
//     TestSegmentBinding_FallbackNeverClaimsALiveEntityKey is that guard,
//     expressed as an invariant rather than as a before/after count so it cannot
//     go stale as the census moves.

import "testing"

// TestSegmentBinding_FallbackNeverClaimsALiveEntityKey asserts, over EVERY entity
// key the registry mints, that a key the strict pass cannot resolve is not
// resolved by either repair pass either. Such a key must fall through to
// model-not-found so the CLI's entity fallback renders its aggregate view.
//
// Deleting the providerStripped gate on the date-to-version rebind reddens this
// test immediately and by the hundreds.
func TestSegmentBinding_FallbackNeverClaimsALiveEntityKey(t *testing.T) {
	entities := Entities()
	if len(entities) == 0 {
		t.Fatal("registry minted no entities; the sweep would pass vacuously")
	}
	// Non-vacuity: the sweep is only meaningful if a substantial share of entity
	// keys are genuinely UNRESOLVABLE as models — those are the keys that depend on
	// the entity fallback and that an ungated rebind would capture.
	unresolvable := 0
	for _, e := range entities {
		key := e.Ref.String()
		if len(matchModelsPass(key, SchemeCanonical, matchPassStrict)) > 0 {
			continue
		}
		unresolvable++
		for _, pass := range []matchPass{matchPassRebindBase, matchPassRebindVariant} {
			if got := matchModelsPass(key, SchemeCanonical, pass); len(got) > 0 {
				t.Errorf("entity key %q is not a model ref in the strict pass, but repair pass %d claims %d row(s) "+
					"(first: %s/%s). A key captured here stops reaching the CLI's entity fallback and silently "+
					"loses its `Entity:` view — the date-to-version rebind must stay gated on a stripped provider segment.",
					key, pass, len(got), got[0].Provider, got[0].ID)
				break
			}
		}
	}
	if unresolvable == 0 {
		t.Fatalf("every one of the %d entity keys resolves as a model in the strict pass, so this sweep "+
			"exercises nothing; the entity-fallback population has changed and the guard needs rethinking",
			len(entities))
	}
	t.Logf("swept %d entity keys, %d of them model-unresolvable and therefore entity-fallback dependent",
		len(entities), unresolvable)
}

// TestSegmentBinding_RepairPassesNeverWidenAResolvedRef asserts the set-level
// gate from the other direction: whenever the strict pass matches at least one
// row, that match set is what Resolve sees — no repair rule contributes.
//
// It sweeps every entity key and every catalog model id, and checks the property
// at matchModels (the gate's own site) rather than by re-deriving it, so removing
// the `len(out) == 0` condition reddens the test rather than silently changing
// which rows a caller gets.
func TestSegmentBinding_RepairPassesNeverWidenAResolvedRef(t *testing.T) {
	seen := map[string]bool{}
	var probes []string
	addProbe := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			probes = append(probes, s)
		}
	}
	for _, e := range Entities() {
		addProbe(e.Ref.String())
	}
	for _, m := range staticModels {
		addProbe(string(m.ID))
		addProbe(string(m.Family))
	}
	if len(probes) < 1000 {
		t.Fatalf("sweep collected only %d probes; the catalog is far larger and the sweep would be vacuous", len(probes))
	}

	widened := 0
	for _, in := range probes {
		strict := matchModelsPass(in, SchemeCanonical, matchPassStrict)
		if len(strict) == 0 {
			continue
		}
		got := matchModels(in, SchemeCanonical)
		if len(got) != len(strict) {
			widened++
			if widened <= 5 {
				t.Errorf("matchModels(%q) returned %d row(s) but the strict pass alone returns %d — a repair "+
					"pass ran on a ref that already resolved. The fallback gate is a property of the whole "+
					"match SET, never of one model.", in, len(got), len(strict))
			}
			continue
		}
		for i := range got {
			if got[i].ID != strict[i].ID || got[i].Provider != strict[i].Provider {
				t.Fatalf("matchModels(%q) row %d = %s/%s, strict pass = %s/%s — a repair pass replaced a "+
					"strict-pass row", in, i, got[i].Provider, got[i].ID, strict[i].Provider, strict[i].ID)
			}
		}
	}
	if widened > 5 {
		t.Errorf("... and %d further widened refs (only the first 5 are listed)", widened-5)
	}
	t.Logf("swept %d distinct refs at the resolver seam", len(probes))
}
