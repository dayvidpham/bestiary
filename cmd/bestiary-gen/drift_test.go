//go:build drift

// Package main — SLICE-5 L2 (bestiary-vbjy) drift smoke.
//
// This file is guarded by the `drift` build tag so it is NEVER compiled or run by the
// default `CGO_ENABLED=0 go test ./...` invocation (which does not pass `-tags drift`).
// Run it explicitly, ON DEMAND, when you want to check whether the committed snapshot has
// drifted from the live models.dev API:
//
//	go test -tags drift ./cmd/bestiary-gen/ -run TestDrift -v
//
// It is a NON-GATING smoke: it requires network access (it fetches the live API), so it is
// deliberately kept out of the hermetic default suite. Unlike a logged-only probe, its
// comparator is ASSERTED (t.Errorf) — a drift FAILS the test so the signal is unmissable:
// when it fails, refresh the committed snapshot and re-run the hardened consistency gate.
package main

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// driftRecord is the (provider, id) identity plus the raw family — the inputs the
// production decomposition consumes. Drift in any of these (added/removed model, or a
// changed raw_family) changes what the snapshot-based gates see, so the snapshot must be
// refreshed.
type driftRecord struct {
	RawFamily bestiary.Family
	Family    bestiary.Family
	Variant   string
	Version   string
	Modifier  string // canonical, order-independent key
}

// TestDrift_SnapshotMatchesLiveAPI fetches the live models.dev API and asserts that the
// committed snapshot's PRODUCTION DECOMPOSITION still matches the live API's, per
// (provider, id). Any added/removed (provider, id) or any changed
// (RawFamily, Family, Variant, Version, Modifier-set) is reported as drift → refresh the
// snapshot. Live decomposition vs committed snapshot, asserted (not just logged).
func TestDrift_SnapshotMatchesLiveAPI(t *testing.T) {
	// ── Committed side: decompose the committed snapshot. ─────────────────────────
	committedRecs, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("drift: load committed snapshot: %v", err)
	}
	type key struct {
		provider bestiary.Provider
		id       bestiary.ModelID
	}
	committed := make(map[key]driftRecord, len(committedRecs))
	for _, r := range committedRecs {
		fam, variant, version, mods, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		committed[key{r.Provider, r.ID}] = driftRecord{r.RawFamily, fam, variant, version, modKey(mods)}
	}

	// ── Live side: fetch the live API and decompose it the same way. ──────────────
	rawJSON, _, _, _, err := fetchModelsWithRaw(context.Background(), t.TempDir(), false /* fetch */)
	if err != nil {
		t.Fatalf("drift: fetch live API (needs network; run with -tags drift online): %v", err)
	}
	var resp genWireResponse
	if err := json.Unmarshal(rawJSON, &resp); err != nil {
		t.Fatalf("drift: parse live API response: %v", err)
	}
	live := make(map[key]driftRecord)
	for provSlug, prov := range resp {
		for _, wm := range prov.Models {
			provider := bestiary.Provider(provSlug)
			id := bestiary.ModelID(wm.ID)
			rawFam := bestiary.Family(wm.Family)
			fam, variant, version, mods, _ := bestiary.ParseFamilyDetailed(rawFam, id, provider)
			live[key{provider, id}] = driftRecord{rawFam, fam, variant, version, modKey(mods)}
		}
	}

	// ── Asserted comparator. ──────────────────────────────────────────────────────
	var added, removed, changed []string
	for k, lr := range live {
		cr, ok := committed[k]
		if !ok {
			added = append(added, string(k.provider)+"/"+string(k.id))
			continue
		}
		if cr != lr {
			changed = append(changed, string(k.provider)+"/"+string(k.id))
		}
	}
	for k := range committed {
		if _, ok := live[k]; !ok {
			removed = append(removed, string(k.provider)+"/"+string(k.id))
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	if len(added)+len(removed)+len(changed) > 0 {
		t.Errorf("SNAPSHOT DRIFT detected vs live models.dev API — refresh the committed snapshot:\n"+
			"  added (in live, not snapshot):   %d  e.g. %s\n"+
			"  removed (in snapshot, not live): %d  e.g. %s\n"+
			"  changed decomposition:           %d  e.g. %s\n"+
			"  Fix: re-capture cmd/bestiary-gen/testdata/snapshot/models_api.json, then re-run the\n"+
			"  hardened gate (TestStaticDataset_CrossProviderConsistency) + update the divergence pins.",
			len(added), head(added), len(removed), head(removed), len(changed), head(changed))
	}
}

// head returns the first few elements for a compact error message.
func head(s []string) string {
	const n = 5
	if len(s) > n {
		s = s[:n]
	}
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}
