package bestiary

import "testing"

// TestParseCreatorTable_AcceptsValidSeed asserts a well-formed table (a known
// family, a non-empty creator) parses and indexes correctly.
func TestParseCreatorTable_AcceptsValidSeed(t *testing.T) {
	good := `{"schema_version":1,"creators":[{"family":"llama","creator":"meta"},{"family":"claude","creator":"anthropic"}]}`
	tbl, err := parseCreatorTable([]byte(good))
	if err != nil {
		t.Fatalf("parseCreatorTable rejected a valid table: %v", err)
	}
	if got := tbl.byFamily[FamilyLlama]; got != CreatorMeta {
		t.Errorf("byFamily[llama] = %q, want %q", got, CreatorMeta)
	}
	if got := tbl.byFamily[FamilyClaude]; got != CreatorAnthropic {
		t.Errorf("byFamily[claude] = %q, want %q", got, CreatorAnthropic)
	}
}

// TestParseCreatorTable_RejectsUnknownFamily asserts an FK-style guard: a mapping
// naming a family that fails Family.IsKnown is rejected loudly.
func TestParseCreatorTable_RejectsUnknownFamily(t *testing.T) {
	bad := `{"schema_version":1,"creators":[{"family":"not-a-real-family-xyz","creator":"meta"}]}`
	if _, err := parseCreatorTable([]byte(bad)); err == nil {
		t.Fatal("parseCreatorTable accepted an unknown family; want a rejection error")
	}
}

// TestParseCreatorTable_RejectsDuplicateFamily asserts Family → Creator is enforced
// as a function: the same family twice is rejected (BCNF; a family has one creator).
func TestParseCreatorTable_RejectsDuplicateFamily(t *testing.T) {
	bad := `{"schema_version":1,"creators":[{"family":"llama","creator":"meta"},{"family":"llama","creator":"anthropic"}]}`
	if _, err := parseCreatorTable([]byte(bad)); err == nil {
		t.Fatal("parseCreatorTable accepted a duplicate family; want a rejection (Family → Creator is a function)")
	}
}

// TestParseCreatorTable_RejectsEmptyCreator asserts an empty creator is rejected: an
// unmapped family is expressed by OMITTING its row, never by a blank creator (which
// would be indistinguishable from "no mapping" yet occupy the family key).
func TestParseCreatorTable_RejectsEmptyCreator(t *testing.T) {
	bad := `{"schema_version":1,"creators":[{"family":"llama","creator":""}]}`
	if _, err := parseCreatorTable([]byte(bad)); err == nil {
		t.Fatal("parseCreatorTable accepted an empty creator; want a rejection error")
	}
}

// TestParseCreatorTable_RejectsMalformedJSON asserts a syntactically broken file is
// rejected (not silently treated as empty) at the parse seam.
func TestParseCreatorTable_RejectsMalformedJSON(t *testing.T) {
	if _, err := parseCreatorTable([]byte("{not json")); err == nil {
		t.Fatal("parseCreatorTable accepted malformed JSON; want a rejection error")
	}
}

// TestSafeCreatorTable_DegradesToEmpty asserts the runtime degrade seam: a load
// error yields a non-nil EMPTY table (all lookups miss → CreatorNone), never nil and
// never a panic — the lineage.go / datasource.go graceful-degrade twin of the loud
// codegen guard.
func TestSafeCreatorTable_DegradesToEmpty(t *testing.T) {
	tbl := safeCreatorTable(nil, errTestDegrade)
	if tbl == nil {
		t.Fatal("safeCreatorTable returned nil on error; want a non-nil empty table")
	}
	if got := tbl.byFamily[FamilyLlama]; got != CreatorNone {
		t.Errorf("degraded table lookup = %q, want CreatorNone", got)
	}
}

// errTestDegrade is a sentinel error used to drive the degrade seam.
var errTestDegrade = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "test degrade" }
