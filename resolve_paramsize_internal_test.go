package bestiary

// resolve_paramsize_internal_test.go — verifies that parameter size participates in
// Resolve()'s ambiguity-group key so sized siblings (distinct registry entities) stay
// distinct candidate groups. These tests live in package bestiary (internal) so they
// can drive Resolve over a synthetic registry via withSyntheticRegistry (defined in
// paramsize_wiring_internal_test.go).
//
// They must NOT run in parallel with each other or with any test that reads entity /
// registry state, because withSyntheticRegistry mutates shared package variables.

import (
	"errors"
	"testing"
)

// TestResolve_SizedSiblings_DistinctAmbiguityGroups verifies that two entities that
// differ ONLY by parameter size (llama@3.3#8b{instruct} vs #70b{instruct}) surface as
// DISTINCT candidates in Resolve()'s ambiguity listing, rather than collapsing into a
// single cross-provider representative.
//
// RED before the groupKey.paramsize field (and the ParamSize threading onto ModelRef):
// without a paramsize discriminator the two siblings share an identical group key,
// byGroup has ONE entry, and Resolve returns them as a non-ambiguous cross-provider
// match (nil error) instead of *ErrAmbiguous.
func TestResolve_SizedSiblings_DistinctAmbiguityGroups(t *testing.T) {
	model8 := syntheticLlamaModel("8b")
	model70 := syntheticLlamaModel("70b")
	withSyntheticRegistry(t, []ModelInfo{model8, model70}, func(t *testing.T) {
		refs, err := Resolve("llama")
		if err == nil {
			t.Fatalf("Resolve(%q) over sized siblings returned refs=%v, nil error; want *ErrAmbiguous "+
				"(the two sizes must NOT collapse into one cross-provider group)", "llama", refs)
		}
		var amb *ErrAmbiguous
		if !errors.As(err, &amb) {
			t.Fatalf("Resolve error = %v (%T), want *ErrAmbiguous", err, err)
		}
		if len(amb.Candidates) != 2 {
			t.Fatalf("ErrAmbiguous.Candidates = %d %+v, want 2 (one per distinct size)", len(amb.Candidates), amb.Candidates)
		}
		got := map[string]bool{}
		for _, c := range amb.Candidates {
			got[c.ParamSize] = true
		}
		if !got["8b"] || !got["70b"] {
			t.Errorf("candidate ParamSizes = %v, want the distinct set {8b, 70b}", got)
		}
	})
}

// TestResolve_SameSizeAcrossProviders_StaysOneGroup is the over-split guard: adding
// paramsize to the group key must NOT split genuine cross-provider hosting of ONE
// sized model into multiple ambiguity groups. Two providers serving the identical
// sized model (same family/version/#size, same ID) stay a single group and resolve
// without ambiguity.
//
// BOTH synthetic providers are deliberately REHOSTS — neither is one of the Meta
// creator's curated distribution surfaces nor llama's canonical provider. The
// provider-preference layer narrows a single group to its most-preferred host, which
// is correct behaviour but would mask the over-split this test exists to catch: one
// returned ref is equally consistent with "one group, narrowed" and with "two groups,
// one dropped". Two rehosts leave the preference a no-op, so the returned count is a
// direct read of the group count.
func TestResolve_SameSizeAcrossProviders_StaysOneGroup(t *testing.T) {
	const id = "meta-llama/Llama-3.3-70b-Instruct"
	meta := ModelInfo{ID: ModelID(id), Provider: "deepinfra", Family: "llama", Version: "3.3", Modifier: []string{"instruct"}, ParamSize: "70b"}
	together := ModelInfo{ID: ModelID(id), Provider: "together", Family: "llama", Version: "3.3", Modifier: []string{"instruct"}, ParamSize: "70b"}
	withSyntheticRegistry(t, []ModelInfo{meta, together}, func(t *testing.T) {
		refs, err := Resolve("llama")
		if err != nil {
			t.Fatalf("Resolve(%q) over one sized model on two providers = error %v; want a non-ambiguous cross-provider match", "llama", err)
		}
		if len(refs) != 2 {
			t.Fatalf("cross-provider refs = %d %+v, want 2 (both providers of the single sized model)", len(refs), refs)
		}
		for _, r := range refs {
			if r.ParamSize != "70b" {
				t.Errorf("ref %q ParamSize = %q, want 70b", r.ID, r.ParamSize)
			}
		}
	})
}
