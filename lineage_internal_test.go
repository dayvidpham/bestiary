package bestiary

import (
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
	if len(got) != 2 {
		t.Fatalf("ancestors of cyclic a→b→a = %+v, want exactly 2 distinct nodes", got)
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
	if len(got) != 2 {
		t.Fatalf("descendants of cyclic a→b→a = %+v, want exactly 2 distinct nodes", got)
	}
}
