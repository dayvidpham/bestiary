package bestiary

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestParseLineageTable_RejectsUnknownParent (VC10) is the negative
// parent-validation gate: a curated edge whose parent.family is not a known base
// family must be REJECTED at load with an actionable error — never silently
// admitted (which would let raw_family-style guesses leak into lineage).
func TestParseLineageTable_RejectsUnknownParent(t *testing.T) {
	const bad = `{
	  "schema_version": 1,
	  "edges": [
	    {
	      "child_id": "made-up-child",
	      "child_ref": {"family": "made-up-child", "variant": "", "version": "1"},
	      "real": false,
	      "parents": [
	        {"family": "not-a-real-base-family", "variant": "", "version": "1", "kind": "finetune"}
	      ]
	    }
	  ]
	}`
	_, err := parseLineageTable([]byte(bad))
	if err == nil {
		t.Fatal("parseLineageTable accepted an unknown parent base family; want a rejection error")
	}
	if !strings.Contains(err.Error(), "unknown base family") {
		t.Errorf("error = %q, want it to name the unknown base family", err.Error())
	}
}

// TestParseLineageTable_RejectsBadKind verifies an unrecognized derivation kind
// is rejected (the curated ledger may not invent kinds outside the enum).
func TestParseLineageTable_RejectsBadKind(t *testing.T) {
	const bad = `{
	  "edges": [
	    {
	      "child_id": "c",
	      "child_ref": {"family": "llama", "variant": "c", "version": "1"},
	      "parents": [{"family": "llama", "variant": "", "version": "1", "kind": "pruned"}]
	    }
	  ]
	}`
	if _, err := parseLineageTable([]byte(bad)); err == nil {
		t.Fatal("parseLineageTable accepted an unknown derivation kind; want a rejection error")
	}
}

// TestParseLineageTable_RejectsEmptyChildOrParents guards the structural
// invariants: a missing child key or an edge with no parents is a curation bug.
func TestParseLineageTable_RejectsEmptyChildOrParents(t *testing.T) {
	noChild := `{"edges":[{"child_id":"","parents":[{"family":"llama","version":"1","kind":"finetune"}]}]}`
	if _, err := parseLineageTable([]byte(noChild)); err == nil {
		t.Error("empty child_id accepted; want rejection")
	}
	noParents := `{"edges":[{"child_id":"x","child_ref":{"family":"llama","variant":"x","version":"1"},"parents":[]}]}`
	if _, err := parseLineageTable([]byte(noParents)); err == nil {
		t.Error("empty parents accepted; want rejection")
	}
}

// TestEmbeddedLineageTable_Valid confirms the shipped curated ledger loads and
// validates cleanly (no unknown base families) — the production-data counterpart
// of the negative test above.
func TestEmbeddedLineageTable_Valid(t *testing.T) {
	if err := ValidateLineageTable(); err != nil {
		t.Fatalf("embedded lineage.json failed validation: %v", err)
	}
}

// TestLineageAncestors_CycleSafe (VC3+ "no cycles") drives the ancestor DFS
// against a deliberately CYCLIC forward index (a→b→a). The visited-set guard must
// make it terminate and emit each node exactly once, never looping forever.
func TestLineageAncestors_CycleSafe(t *testing.T) {
	a := EntityRef{Family: "llama", Variant: "a"}
	b := EntityRef{Family: "llama", Variant: "b"}
	fwd := map[string][]LineageEdge{
		a.String(): {{Parent: b, Kind: DerivationMerge}},
		b.String(): {{Parent: a, Kind: DerivationMerge}}, // cycle back to a
	}
	seed := []LineageEdge{{Parent: a, Kind: DerivationFinetune}}

	done := make(chan []EntityRef, 1)
	go func() { done <- lineageAncestors(seed, fwd) }()
	var got []EntityRef
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lineageAncestors did not terminate on a cyclic DAG (infinite loop)")
	}
	// The visited set must yield EXACTLY {a, b} — each node once, no duplicates —
	// so the cycle-break is observable in the contents, not just the cardinality.
	if want := refSet(a, b); !reflect.DeepEqual(refSet(got...), want) {
		t.Fatalf("ancestors of cyclic a→b→a = %v, want exactly the set {%s, %s}", got, a.String(), b.String())
	}
}

// TestLineageDescendants_CycleSafe mirrors the ancestor cycle test for the
// reverse traversal.
func TestLineageDescendants_CycleSafe(t *testing.T) {
	a := EntityRef{Family: "llama", Variant: "a"}
	b := EntityRef{Family: "llama", Variant: "b"}
	rev := map[string][]EntityRef{
		a.String(): {b}, // a's descendant is b
		b.String(): {a}, // and b's descendant is a — a cycle
	}
	done := make(chan []EntityRef, 1)
	go func() { done <- lineageDescendants(a.String(), rev) }()
	var got []EntityRef
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lineageDescendants did not terminate on a cyclic DAG (infinite loop)")
	}
	if want := refSet(a, b); !reflect.DeepEqual(refSet(got...), want) {
		t.Fatalf("descendants of cyclic a→b→a = %v, want exactly the set {%s, %s}", got, a.String(), b.String())
	}
}

// refSet collapses EntityRefs to a set keyed by their canonical String() form, so
// a cycle-safe traversal's output can be compared by CONTENT independent of order.
func refSet(refs ...EntityRef) map[string]struct{} {
	set := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		set[r.String()] = struct{}{}
	}
	return set
}

// withSyntheticLineage swaps the process-wide curated lineage table for tbl while fn
// runs, then restores it. It first forces loadLineageTable's sync.Once to fire
// (reading the real embedded table) so a later lazy load cannot re-run and clobber
// the injected fixture; the injection then survives until t.Cleanup restores it. This
// lets the PUBLIC Entity.Ancestors/Descendants methods (which read the global table)
// run against a controlled fixture.
//
// Not safe for concurrent use: it mutates package-global lineage state. Call only from
// subtests that do NOT run in parallel.
func withSyntheticLineage(t *testing.T, tbl *lineageTable, fn func()) {
	t.Helper()
	_, _ = loadLineageTable() // fire the sync.Once so a later Do can't overwrite the injection
	origTbl, origErr := lineageTbl, lineageErr
	lineageTbl, lineageErr = tbl, nil
	t.Cleanup(func() {
		lineageTbl, lineageErr = origTbl, origErr
	})
	fn()
}

// TestLineage_SizedFixtureEdge_ResolvesViaAncestorsDescendants is the #size lineage
// regression test. A curated edge whose child_ref AND parent carry a param_size token
// must key the forward/reverse indices by the SIZED entity keys (family@version#size
// {mods}), so a sized entity is linked to its lineage instead of de-linked.
//
// No shipped edge references a sized entity today (all registry entities are unsized),
// so this drives a hand-built fixture edge. It is RED before the param_size threading
// in lineage.go: without it, parseLineageTable builds the UNSIZED keys ("llama@3.3
// {instruct}" / "llama@3.1"), the sized-key lookups miss, and the fallback-seed
// Ancestors()/Descendants() find nothing.
func TestLineage_SizedFixtureEdge_ResolvesViaAncestorsDescendants(t *testing.T) {
	const fixture = `{
	  "schema_version": 2,
	  "edges": [
	    {
	      "child_id": "sized-fixture-child-70b",
	      "child_ref": { "family": "llama", "variant": "", "version": "3.3", "param_size": "70b", "modifier": ["instruct"] },
	      "real": false,
	      "parents": [
	        { "family": "llama", "variant": "", "version": "3.1", "param_size": "70b", "kind": "finetune" }
	      ]
	    }
	  ]
	}`

	const (
		childKey  = "llama@3.3#70b{instruct}" // sized child entity key
		parentKey = "llama@3.1#70b"           // sized parent entity key
	)

	tbl, err := parseLineageTable([]byte(fixture))
	if err != nil {
		t.Fatalf("parseLineageTable(sized fixture) error: %v", err)
	}

	// Forward index: the child node is keyed by its SIZED key, and the edge resolves
	// to the sized parent. The unsized key must be ABSENT — a drop of param_size would
	// re-key the child unsized and de-link the sized entity.
	fwd, ok := tbl.forward[childKey]
	if !ok {
		t.Fatalf("forward index missing sized child key %q (keys: %v) — child_ref param_size not threaded into the DAG key", childKey, forwardKeys(tbl))
	}
	if _, unsized := tbl.forward["llama@3.3{instruct}"]; unsized {
		t.Error("forward index contains the UNSIZED child key \"llama@3.3{instruct}\"; the sized child must key by its #size form only")
	}
	if len(fwd) != 1 || fwd[0].Parent.String() != parentKey {
		t.Fatalf("forward[%q] = %+v, want one edge to sized parent %q", childKey, fwd, parentKey)
	}

	// Reverse index: the parent is keyed by its SIZED key and points at the sized child.
	rev, ok := tbl.reverse[parentKey]
	if !ok {
		t.Fatalf("reverse index missing sized parent key %q — parent param_size not threaded into the DAG key", parentKey)
	}
	if len(rev) != 1 || rev[0].String() != childKey {
		t.Fatalf("reverse[%q] = %+v, want the sized child %q", parentKey, rev, childKey)
	}

	// PUBLIC path: with the fixture injected, Entity.Ancestors()/Descendants() (which
	// read the global table via the Ref-keyed fallback seed) must resolve for the sized
	// entity keys — the exact behavior the de-link broke.
	withSyntheticLineage(t, tbl, func() {
		child := Entity{Ref: EntityRef{Family: FamilyLlama, Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}}
		if child.Ref.String() != childKey {
			t.Fatalf("sized child entity key = %q, want %q", child.Ref.String(), childKey)
		}
		anc := child.Ancestors()
		if _, found := refSet(anc...)[parentKey]; !found {
			t.Fatalf("Ancestors() for sized child %q = %v, want it to include sized parent %q", childKey, anc, parentKey)
		}

		parent := Entity{Ref: EntityRef{Family: FamilyLlama, Version: "3.1", ParamSize: "70b"}}
		if parent.Ref.String() != parentKey {
			t.Fatalf("sized parent entity key = %q, want %q", parent.Ref.String(), parentKey)
		}
		desc := parent.Descendants()
		if _, found := refSet(desc...)[childKey]; !found {
			t.Fatalf("Descendants() for sized parent %q = %v, want it to include sized child %q", parentKey, desc, childKey)
		}
	})
}

// forwardKeys returns the forward-index keys of tbl, for readable failure messages.
func forwardKeys(t *lineageTable) []string {
	keys := make([]string, 0, len(t.forward))
	for k := range t.forward {
		keys = append(keys, k)
	}
	return keys
}

// TestSafeLineageTable_DegradesToNoLineage exercises the runtime
// degrade twin of the codegen ValidateLineageTable hard-fail: when the table
// fails to load (parse error) or is nil, safeLineageTable must fall back to a
// non-nil EMPTY table so lookups miss ("no lineage") and traversal yields nothing
// — never a nil-deref or panic. Mirrors the ClassifyModifier degrade test.
func TestSafeLineageTable_DegradesToNoLineage(t *testing.T) {
	// A malformed table is the load-failure trigger.
	badTable, err := parseLineageTable([]byte("}{ not valid json"))
	if err == nil {
		t.Fatal("parseLineageTable accepted malformed JSON; expected a load error to drive the degrade path")
	}

	for _, tc := range []struct {
		name  string
		table *lineageTable
		err   error
	}{
		{"load error", badTable, err},
		{"nil table, nil error", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeLineageTable(tc.table, tc.err)
			if got == nil {
				t.Fatal("safeLineageTable returned nil; the degrade fallback must be non-nil")
			}
			// Lookups miss on the degraded table even for an otherwise-curated child.
			if rec, ok := got.lookup("gryphe/mythomax-l2-13b"); ok {
				t.Errorf("degraded lookup returned a record %+v, want a miss (no lineage)", rec)
			}
			// Traversal over the degraded indices is empty and never panics.
			if anc := lineageAncestors(nil, got.forward); anc != nil {
				t.Errorf("degraded ancestors = %v, want nil", anc)
			}
			if desc := lineageDescendants("llama@3.1", got.reverse); desc != nil {
				t.Errorf("degraded descendants = %v, want nil", desc)
			}
		})
	}
}

// TestLineage_SizeAgnosticFallback_SizedInheritsUnsizedEdge is falsifiable test (a):
// a curated edge whose child_ref/parent OMIT param_size is SIZE-AGNOSTIC — a sized
// entity inherits it through the size-stripped fallback, via BOTH Ancestors and
// Descendants. Mutation-prove: revert forwardSeed/reverseRootKey to an exact-only
// lookup (drop the sizeStrippedSeedKey retry) and this test goes RED.
func TestLineage_SizeAgnosticFallback_SizedInheritsUnsizedEdge(t *testing.T) {
	const fixture = `{
	  "schema_version": 2,
	  "edges": [
	    {
	      "child_id": "size-agnostic-child",
	      "child_ref": { "family": "llama", "version": "3.3", "modifier": ["instruct"] },
	      "real": false,
	      "parents": [ { "family": "llama", "version": "3.1", "kind": "finetune" } ]
	    }
	  ]
	}`
	const (
		unsizedParentKey = "llama@3.1"
		unsizedChildKey  = "llama@3.3{instruct}"
	)
	tbl, err := parseLineageTable([]byte(fixture))
	if err != nil {
		t.Fatalf("parseLineageTable(unsized fixture): %v", err)
	}
	withSyntheticLineage(t, tbl, func() {
		// A SIZED child (llama@3.3#70b{instruct}) inherits the unsized parent via Ancestors.
		sizedChild := Entity{Ref: EntityRef{Family: FamilyLlama, Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}}
		anc := sizedChild.Ancestors()
		if _, ok := refSet(anc...)[unsizedParentKey]; !ok {
			t.Fatalf("sized child %q Ancestors() = %v, want it to inherit unsized parent %q via the size-stripped fallback",
				sizedChild.Ref.String(), anc, unsizedParentKey)
		}
		// A SIZED parent (llama@3.1#70b) inherits the unsized child via Descendants.
		sizedParent := Entity{Ref: EntityRef{Family: FamilyLlama, Version: "3.1", ParamSize: "70b"}}
		desc := sizedParent.Descendants()
		if _, ok := refSet(desc...)[unsizedChildKey]; !ok {
			t.Fatalf("sized parent %q Descendants() = %v, want it to inherit unsized child %q via the size-stripped fallback",
				sizedParent.Ref.String(), desc, unsizedChildKey)
		}
	})
}

// TestLineage_SizeSpecificEdge_OverridesAndDoesNotLeak is falsifiable test (b): a
// size-specific edge OVERRIDES the size-agnostic one for its own size, and does NOT
// leak to siblings of other sizes (a differently-sized sibling still gets the
// unsized edge). Two edges share the unsized stem "qwen@3": the unsized edge points
// at qwen@2, the 70b edge points at qwen@2.5#70b.
func TestLineage_SizeSpecificEdge_OverridesAndDoesNotLeak(t *testing.T) {
	const fixture = `{
	  "schema_version": 2,
	  "edges": [
	    { "child_id": "qwen3-unsized", "child_ref": { "family": "qwen", "version": "3" }, "real": false,
	      "parents": [ { "family": "qwen", "version": "2", "kind": "finetune" } ] },
	    { "child_id": "qwen3-70b", "child_ref": { "family": "qwen", "version": "3", "param_size": "70b" }, "real": false,
	      "parents": [ { "family": "qwen", "version": "2.5", "param_size": "70b", "kind": "finetune" } ] }
	  ]
	}`
	const (
		unsizedParentKey = "qwen@2"
		sizedParentKey   = "qwen@2.5#70b"
	)
	tbl, err := parseLineageTable([]byte(fixture))
	if err != nil {
		t.Fatalf("parseLineageTable(override fixture): %v", err)
	}
	withSyntheticLineage(t, tbl, func() {
		// The 70b entity: its OWN size-specific edge wins; the unsized parent must NOT appear.
		e70 := Entity{Ref: EntityRef{Family: FamilyQwen, Version: "3", ParamSize: "70b"}}
		anc70 := refSet(e70.Ancestors()...)
		if _, ok := anc70[sizedParentKey]; !ok {
			t.Fatalf("70b entity Ancestors() = %v, missing its size-specific parent %q", e70.Ancestors(), sizedParentKey)
		}
		if _, leaked := anc70[unsizedParentKey]; leaked {
			t.Fatalf("70b entity Ancestors() = %v leaked the unsized parent %q; the exact size-specific edge must win alone",
				e70.Ancestors(), unsizedParentKey)
		}
		// A sibling of a DIFFERENT size (8b, no size-specific edge) gets the unsized edge,
		// NOT the 70b size-specific one.
		e8 := Entity{Ref: EntityRef{Family: FamilyQwen, Version: "3", ParamSize: "8b"}}
		anc8 := refSet(e8.Ancestors()...)
		if _, ok := anc8[unsizedParentKey]; !ok {
			t.Fatalf("8b sibling Ancestors() = %v, missing the size-agnostic parent %q", e8.Ancestors(), unsizedParentKey)
		}
		if _, leaked := anc8[sizedParentKey]; leaked {
			t.Fatalf("8b sibling Ancestors() = %v leaked the 70b size-specific parent %q; a size-specific edge must not leak to other sizes",
				e8.Ancestors(), sizedParentKey)
		}
	})
}

// TestLineage_UnsizedEntity_BehaviorUnchanged is falsifiable test (c): an unsized
// entity resolves EXACTLY as before — the size-stripped fallback never fires for it
// (sizeStrippedSeedKey declines an unsized ref), so its exact-key edges are used
// unchanged for both Ancestors and Descendants.
func TestLineage_UnsizedEntity_BehaviorUnchanged(t *testing.T) {
	// The fallback helper must decline an unsized ref (no redundant second lookup).
	if _, ok := sizeStrippedSeedKey(EntityRef{Family: FamilyPhi, Version: "4"}); ok {
		t.Fatal("sizeStrippedSeedKey(unsized ref) returned ok=true; an unsized entity must not trigger the size-stripped fallback")
	}

	const fixture = `{
	  "schema_version": 2,
	  "edges": [
	    { "child_id": "phi4-unsized", "child_ref": { "family": "phi", "version": "4" }, "real": false,
	      "parents": [ { "family": "phi", "version": "3", "kind": "finetune" } ] }
	  ]
	}`
	tbl, err := parseLineageTable([]byte(fixture))
	if err != nil {
		t.Fatalf("parseLineageTable(unsized-entity fixture): %v", err)
	}
	withSyntheticLineage(t, tbl, func() {
		child := Entity{Ref: EntityRef{Family: FamilyPhi, Version: "4"}}
		if _, ok := refSet(child.Ancestors()...)["phi@3"]; !ok {
			t.Fatalf("unsized child Ancestors() = %v, want exact-key parent phi@3 (unchanged behavior)", child.Ancestors())
		}
		parent := Entity{Ref: EntityRef{Family: FamilyPhi, Version: "3"}}
		if _, ok := refSet(parent.Descendants()...)["phi@4"]; !ok {
			t.Fatalf("unsized parent Descendants() = %v, want exact-key child phi@4 (unchanged behavior)", parent.Descendants())
		}
	})
}

// TestLineage_ExactBeforeFallback_Precedence is falsifiable test (d): an entity
// whose SIZED key AND its size-stripped key both carry curated edges resolves to the
// EXACT (sized) edge; the size-stripped edge must not appear. Pins that the exact
// lookup is tried before the fallback, for both Ancestors and Descendants.
// Mutation-prove: make forwardSeed/reverseRootKey prefer the stripped key (drop the
// exact-first check) and this test goes RED.
func TestLineage_ExactBeforeFallback_Precedence(t *testing.T) {
	const fixture = `{
	  "schema_version": 2,
	  "edges": [
	    { "child_id": "gemma2-unsized", "child_ref": { "family": "gemma", "version": "2" }, "real": false,
	      "parents": [ { "family": "gemma", "version": "1", "kind": "finetune" } ] },
	    { "child_id": "gemma2-9b", "child_ref": { "family": "gemma", "version": "2", "param_size": "9b" }, "real": false,
	      "parents": [ { "family": "gemma", "version": "1", "param_size": "9b", "kind": "finetune" } ] }
	  ]
	}`
	const (
		strippedParentKey = "gemma@1"
		sizedParentKey    = "gemma@1#9b"
	)
	tbl, err := parseLineageTable([]byte(fixture))
	if err != nil {
		t.Fatalf("parseLineageTable(precedence fixture): %v", err)
	}
	withSyntheticLineage(t, tbl, func() {
		// Ancestors: the 9b child has both an exact sized edge and a stripped edge; sized wins.
		e := Entity{Ref: EntityRef{Family: FamilyGemma, Version: "2", ParamSize: "9b"}}
		anc := refSet(e.Ancestors()...)
		if _, ok := anc[sizedParentKey]; !ok {
			t.Fatalf("Ancestors() for %q = %v, missing the exact sized parent %q", e.Ref.String(), e.Ancestors(), sizedParentKey)
		}
		if _, leaked := anc[strippedParentKey]; leaked {
			t.Fatalf("Ancestors() for %q = %v included the size-stripped parent %q; the exact sized edge must win (precedence)",
				e.Ref.String(), e.Ancestors(), strippedParentKey)
		}
		// Descendants: the 9b parent has both an exact sized reverse edge and a stripped one; sized wins.
		p := Entity{Ref: EntityRef{Family: FamilyGemma, Version: "1", ParamSize: "9b"}}
		desc := refSet(p.Descendants()...)
		if _, ok := desc["gemma@2#9b"]; !ok {
			t.Fatalf("Descendants() for %q = %v, missing the exact sized child gemma@2#9b", p.Ref.String(), p.Descendants())
		}
		if _, leaked := desc["gemma@2"]; leaked {
			t.Fatalf("Descendants() for %q = %v included the size-stripped child gemma@2; the exact sized reverse edge must win (precedence)",
				p.Ref.String(), p.Descendants())
		}
	})
}
