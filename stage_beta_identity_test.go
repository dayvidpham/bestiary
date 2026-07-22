package bestiary_test

// Fences for the beta-always-stage rule: beta is a RELEASE STAGE and never part of a
// model's identity.
//
// The two axes are independent by construction — DetectStageFromID scans the ID
// without stripping — so the interesting failure is the COEXISTENCE: a decomposition
// that ALSO promotes the beta token into the entity key, leaving one record asserting
// beta as both a stage and an identity.

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestBetaNeverInIdentity_Registry is the census sweep: no entity in the shipped
// registry may carry beta in its key. This is the state the codegen guard maintains,
// asserted independently of the guard so a broken guard cannot hide a broken registry.
func TestBetaNeverInIdentity_Registry(t *testing.T) {
	for _, e := range bestiary.Entities() {
		if strings.EqualFold(e.Ref.Variant, "beta") {
			t.Errorf("entity %q has variant \"beta\" — beta is a release stage, never an identity", e.Ref.String())
		}
		for _, m := range e.Ref.Modifier {
			if strings.EqualFold(m, "beta") {
				t.Errorf("entity %q carries beta as an identity modifier — beta is a release stage", e.Ref.String())
			}
		}
	}
}

// TestBetaGuard_FiresOnIdentityBeta is the NEGATIVE CONTROL. A guard that never fires
// is indistinguishable from no guard, so both violating shapes are constructed and
// each must be rejected with an actionable, ID-naming error.
func TestBetaGuard_FiresOnIdentityBeta(t *testing.T) {
	cases := []struct {
		name string
		ent  bestiary.Entity
		want string
	}{
		{
			name: "beta promoted into the variant slot",
			ent: bestiary.Entity{
				Ref:       bestiary.EntityRef{Family: "someLab", Variant: "beta"},
				Instances: []bestiary.ProviderInstance{{ID: "somelab/somelab-beta", Provider: "someprovider"}},
			},
			want: `the variant slot is "beta"`,
		},
		{
			name: "beta as an identity modifier",
			ent: bestiary.Entity{
				Ref:       bestiary.EntityRef{Family: "someLab", Version: "2", Modifier: []string{"beta"}},
				Instances: []bestiary.ProviderInstance{{ID: "somelab-2-beta", Provider: "someprovider"}},
			},
			want: "identity modifier",
		},
		{
			name: "case-folded beta still violates",
			ent: bestiary.Entity{
				Ref:       bestiary.EntityRef{Family: "someLab", Variant: "BETA"},
				Instances: []bestiary.ProviderInstance{{ID: "somelab/SomeLab-BETA", Provider: "someprovider"}},
			},
			want: `the variant slot is "beta"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := bestiary.ValidateNoBetaInIdentity([]bestiary.Entity{tc.ent})
			if err == nil {
				t.Fatalf("ValidateNoBetaInIdentity accepted %q; the guard must abort the bake", tc.ent.Ref.String())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not explain the violation (%q missing): %v", tc.want, err)
			}
			// The error must name the offending model ID, or a curator cannot find the row.
			if !strings.Contains(err.Error(), string(tc.ent.Instances[0].ID)) {
				t.Errorf("error does not name the offending model id %q: %v", tc.ent.Instances[0].ID, err)
			}
		})
	}
}

// TestBetaGuard_DoesNotOverReach pins the other side: the guard must not fire on a
// stage-only beta, nor on a whole-token near-miss. A guard that rejected these would
// make the beta STAGE axis unusable, which is the opposite of the intent.
func TestBetaGuard_DoesNotOverReach(t *testing.T) {
	ok := []bestiary.Entity{
		// Stage-only: the id says beta, the key does not. This is the correct shape and
		// exactly what the interfaze pin produces.
		{Ref: bestiary.EntityRef{Family: "interfaze"}, Instances: []bestiary.ProviderInstance{{ID: "interfaze/interfaze-beta"}}},
		// Near-miss tokens: "betamax" is not "beta".
		{Ref: bestiary.EntityRef{Family: "betamax", Variant: "betamax"}},
		{Ref: bestiary.EntityRef{Family: "someLab", Modifier: []string{"betamax"}}},
		// A family literally named beta is not an identity-MODIFIER violation; only the
		// variant slot and the modifier set are in scope.
		{Ref: bestiary.EntityRef{Family: "beta"}},
	}
	if err := bestiary.ValidateNoBetaInIdentity(ok); err != nil {
		t.Errorf("ValidateNoBetaInIdentity rejected a conforming set: %v", err)
	}
}

// TestInterfaze_BetaIsStageNotIdentity pins the curated resolution end-to-end: the one
// row that used to put beta into a key now decomposes bare, and its beta lives purely
// on the stage axis — the detect-without-strip contract, demonstrated on the exact row
// that motivated the rule.
func TestInterfaze_BetaIsStageNotIdentity(t *testing.T) {
	const id = "interfaze/interfaze-beta"

	var found bool
	for _, m := range bestiary.StaticModels() {
		if string(m.ID) != id {
			continue
		}
		found = true
		if m.Family != "interfaze" || m.Variant != "" {
			t.Errorf("%q decomposes to (%q, %q), want family interfaze with NO variant — "+
				"beta must not reach the identity", id, m.Family, m.Variant)
		}
		if m.Stage != bestiary.StageBeta {
			t.Errorf("%q has stage %v, want StageBeta — the pin must not cost the row its stage", id, m.Stage)
		}
	}
	if !found {
		t.Fatalf("catalog precondition lost: %q is gone; re-check the pin against the current snapshot", id)
	}

	// The entity it lands on is the bare family, and the retired key is not minted.
	if _, ok := bestiary.EntityByKey("interfaze"); !ok {
		t.Error("entity \"interfaze\" is missing — the pin must land the row on the bare family")
	}
	for _, e := range bestiary.Entities() {
		if e.Ref.String() == "interfaze/beta" {
			t.Error("entity key \"interfaze/beta\" is still minted — the curated pin is no longer taking effect")
		}
	}
}
