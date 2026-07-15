package testcase_test

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
)

//go:embed testdata/example.json
var exampleCorpusJSON []byte

// TestLoadCorpus_Example loads the fully-populated example corpus and drives a
// trivial "input is a non-empty string" validator over it, confirming the
// generic Case[I, E] round-trips through encoding/json and that both the
// must-pass and must-fail arms behave.
func TestLoadCorpus_Example(t *testing.T) {
	corpus, err := testcase.LoadCorpus[string, bool](exampleCorpusJSON)
	if err != nil {
		t.Fatalf("LoadCorpus(testdata/example.json): %v", err)
	}
	if got := len(corpus.Cases); got != 2 {
		t.Fatalf("example corpus has %d cases, want exactly 2", got)
	}
	if err := corpus.Validate(); err != nil {
		t.Fatalf("example corpus is under-populated: %v", err)
	}
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			accepted := c.Input != ""
			if accepted != c.Expected {
				t.Errorf("accept(%q) = %v, want %v", c.Input, accepted, c.Expected)
			}
			wantPass := c.Classification == testcase.MustPass
			if accepted != wantPass {
				t.Errorf("classification %q disagrees with expected %v", c.Classification, c.Expected)
			}
		})
	}
}

// TestCheckMin_Floor is the negative control for the size floor: a short corpus
// trips CheckMin and a corpus at or above the floor passes. Proving the guard
// actually fires keeps assert.RequireMin honest without a *testing.T.
func TestCheckMin_Floor(t *testing.T) {
	full, err := testcase.LoadCorpus[string, bool](exampleCorpusJSON)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	if err := full.CheckMin(2); err != nil {
		t.Errorf("CheckMin(2) on a 2-case corpus should pass, got: %v", err)
	}
	if err := full.CheckMin(3); err == nil {
		t.Errorf("CheckMin(3) on a 2-case corpus should fail, got nil")
	}
	var empty testcase.Corpus[string, bool]
	if err := empty.CheckMin(1); err == nil {
		t.Errorf("CheckMin(1) on an empty corpus should fail, got nil")
	}
}

// TestValidate_NonVacuity is the negative control for Corpus.Validate: each way
// a case can be vacuous (out-of-set classification, out-of-set or absent
// provenance source, empty ref, empty mutation description) must be rejected,
// and a fully-populated case must pass.
func TestValidate_NonVacuity(t *testing.T) {
	good := testcase.Case[string, bool]{
		Name:           "ok",
		Classification: testcase.MustPass,
		Provenance:     testcase.Provenance{Source: testcase.SourceRequirement, Ref: "why"},
		Mutation:       testcase.Mutation{Description: "what"},
	}
	if err := good.Validate(); err != nil {
		t.Fatalf("a fully-populated case should validate, got: %v", err)
	}

	mutations := []struct {
		name string
		mut  func(c *testcase.Case[string, bool])
	}{
		{"bad classification", func(c *testcase.Case[string, bool]) { c.Classification = "maybe" }},
		{"bad provenance source", func(c *testcase.Case[string, bool]) { c.Provenance.Source = "guess" }},
		{"empty provenance ref", func(c *testcase.Case[string, bool]) { c.Provenance.Ref = "" }},
		{"empty mutation description", func(c *testcase.Case[string, bool]) { c.Mutation.Description = "" }},
	}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			c := good
			m.mut(&c)
			if err := c.Validate(); err == nil {
				t.Errorf("a %s case should be rejected by Validate, got nil", m.name)
			}
		})
	}

	// Corpus.Validate surfaces the offending case index.
	bad := testcase.Corpus[string, bool]{Cases: []testcase.Case[string, bool]{good, {Name: "vacuous"}}}
	if err := bad.Validate(); err == nil {
		t.Errorf("a corpus containing a vacuous case should fail Validate, got nil")
	}
}
