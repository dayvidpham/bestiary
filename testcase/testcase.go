// Package testcase is the pure-data foundation for bestiary's canonical
// test-case corpus: a typed, generic Case/Corpus model with closed-set
// classification and provenance metadata, a pure JSON loader, and pure
// validators. It carries NO testing dependency.
//
// The testing-side helpers (which take *testing.T) live in the sibling
// subpackage github.com/dayvidpham/bestiary/testcase/assert. Splitting them out
// keeps this data/leaf package free of the testing import: testcase is pure
// data, testcase/assert is the testing seam.
//
// This mirrors the peasant-labs/schema testcase standard, with one deliberate
// adaptation to bestiary's dependency discipline (stdlib + zombiezen/sqlite
// only): corpora are authored as JSON and decoded with encoding/json, never
// YAML. No external dependency is introduced.
//
// This package is bestiary's canonical standard for a case corpus. The idiom:
//
//   - author a table of {input, expected} rows as a JSON file in the Corpus
//     schema, each case naming its Classification (must-pass or must-fail), its
//     Provenance (source category plus a concrete ref), and the Mutation under
//     test;
//   - load it with LoadCorpus[I, E];
//   - assert every case is non-vacuous with Corpus.Validate;
//   - guard coverage: assert.RequireMin for a growable floor, or an exact
//     "len == N" control (catching a drop or a stray add) plus a lean
//     value-based coverage assertion (catching a count-preserving swap that
//     drops a real case and adds a filler, which an exact-count check cannot
//     detect) when migrating a fixed corpus;
//   - drive the system under test over each case and assert its result against
//     the case's Expected and Classification.
//
// New case corpora should adopt this shape. The parse/entity/quantization
// grammar corpora under testdata/ are worked migrations onto it.
package testcase

import (
	"encoding/json"
	"fmt"
)

// Classification is the closed set of a case's expectation against the system
// under test: an input the SUT must accept, or one it must reject.
type Classification string

const (
	// MustPass marks a valid input the system under test must accept.
	MustPass Classification = "must-pass"
	// MustFail marks an invalid input the system under test must reject.
	MustFail Classification = "must-fail"
)

// IsValid reports whether the classification is one of the known members.
func (c Classification) IsValid() bool {
	switch c {
	case MustPass, MustFail:
		return true
	}
	return false
}

func (c Classification) String() string { return string(c) }

// ProvenanceSource is the closed set of why a case earns a place in the corpus:
// the category of evidence it came from.
type ProvenanceSource string

const (
	// SourceRequirement: the case pins a stated requirement, spec clause, or
	// BDD acceptance criterion.
	SourceRequirement ProvenanceSource = "requirement"
	// SourceBug: the case is a regression captured from a real defect or a
	// coverage-gap audit finding.
	SourceBug ProvenanceSource = "bug"
	// SourceEnum: the case is generated from a closed enum's members or an
	// authoritative curated table (e.g. every variant_suffixes.json entry).
	SourceEnum ProvenanceSource = "enum"
	// SourceBoundary: the case probes an edge of an allowed range or format.
	SourceBoundary ProvenanceSource = "boundary"
	// SourceManual: the case was authored by hand for a reason recorded in Ref
	// (a real catalog ID it pins, or a hand-computed arithmetic derivation).
	SourceManual ProvenanceSource = "manual"
)

// IsValid reports whether the provenance source is one of the known members.
func (s ProvenanceSource) IsValid() bool {
	switch s {
	case SourceRequirement, SourceBug, SourceEnum, SourceBoundary, SourceManual:
		return true
	}
	return false
}

func (s ProvenanceSource) String() string { return string(s) }

// Provenance records why a case exists: its source category plus a concrete,
// non-empty reference (a requirement id, a bug link, an enum name, a real
// catalog id, or the arithmetic derivation of a hand-computed literal).
type Provenance struct {
	Source ProvenanceSource `json:"source"`
	Ref    string           `json:"ref"`
}

// Mutation records how a case was derived: the single change under test. For a
// must-fail case it is the mutation that makes a valid input invalid, so a
// negative case is never vacuous; for a must-pass case it names what the case
// exercises.
type Mutation struct {
	Description string `json:"description"`
}

// Case is one generic test case: a named input with its expected output, a
// pass/fail classification, and the provenance + mutation metadata that keep
// the corpus traceable and non-vacuous. I is the input type, E the
// expected-output type; both are decoded from JSON by the caller-chosen
// instantiation.
type Case[I any, E any] struct {
	Name           string         `json:"name"`
	Input          I              `json:"input"`
	Expected       E              `json:"expected"`
	Classification Classification `json:"classification"`
	Provenance     Provenance     `json:"provenance"`
	Mutation       Mutation       `json:"mutation"`
}

// Corpus is an ordered collection of cases sharing an input/expected type.
type Corpus[I any, E any] struct {
	Cases []Case[I, E] `json:"cases"`
}

// LoadCorpus parses a JSON corpus document into a typed Corpus. It is pure: it
// returns an error rather than failing a test, mirroring bestiary's production
// fixture loaders so this package stays free of the testing dependency.
func LoadCorpus[I any, E any](data []byte) (Corpus[I, E], error) {
	var c Corpus[I, E]
	if err := json.Unmarshal(data, &c); err != nil {
		return Corpus[I, E]{}, err
	}
	return c, nil
}

// CheckMin is the pure minimum-size meta-guard: it returns an error when the
// corpus holds fewer than n cases, and nil otherwise. It is a floor
// (len >= n), not an exact-count guard, so a corpus may grow without tripping
// it.
//
// It is exported (rather than an unexported helper) because the testing seam
// assert.RequireMin, in the sibling testcase/assert package, wraps it: keeping
// the size check here as one pure, package-crossable function means the loud
// *testing.T wrapper and its own negative-control test share a single source
// of truth instead of re-deriving the floor.
func (c Corpus[I, E]) CheckMin(n int) error {
	if len(c.Cases) < n {
		return fmt.Errorf("corpus has %d case(s), want at least %d", len(c.Cases), n)
	}
	return nil
}

// Validate reports the first way a case is under-populated. Every case must
// carry an in-set classification, a valid provenance source, a non-empty
// provenance ref (why it exists), and a non-empty mutation description (what it
// changes). An empty ref or description is treated as a vacuous case and
// rejected.
func (c Case[I, E]) Validate() error {
	if !c.Classification.IsValid() {
		return fmt.Errorf("case %q: classification %q is not one of must-pass/must-fail", c.Name, c.Classification)
	}
	if !c.Provenance.Source.IsValid() {
		return fmt.Errorf("case %q: provenance source %q is not a known source", c.Name, c.Provenance.Source)
	}
	if c.Provenance.Ref == "" {
		return fmt.Errorf("case %q: provenance ref is empty (a case must cite why it exists)", c.Name)
	}
	if c.Mutation.Description == "" {
		return fmt.Errorf("case %q: mutation description is empty (a case must describe the change under test)", c.Name)
	}
	return nil
}

// Validate runs Case.Validate over every case, returning the first failure with
// the offending case's index for locality.
func (c Corpus[I, E]) Validate() error {
	for i := range c.Cases {
		if err := c.Cases[i].Validate(); err != nil {
			return fmt.Errorf("corpus case %d: %w", i, err)
		}
	}
	return nil
}
