package bestiary_test

import (
	"encoding/json"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestEntityRef_String_Contract locks the comparable-key contract for
// EntityRef.String():
//
//		family[/variant][@version]{identity-mods}
//
//	  - "/variant" only when Variant is non-empty
//	  - "@version" only when Version is non-empty (identity version, NOT a date)
//	  - "{identity-mods}" only when at least one identity modifier is present;
//	    tokens in canonical order, comma-separated; braces OMITTED when empty
//	  - the "[attributes]" segment is NEVER emitted (attributes are not identity)
func TestEntityRef_String_Contract(t *testing.T) {
	cases := []struct {
		name string
		ref  bestiary.EntityRef
		want string
	}{
		{
			name: "family only",
			ref:  bestiary.EntityRef{Family: "llama"},
			want: "llama",
		},
		{
			name: "family + variant",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus"},
			want: "claude/opus",
		},
		{
			name: "family + version",
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.1"},
			want: "llama@3.1",
		},
		{
			name: "family + variant + version",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"},
			want: "claude/opus@4.5",
		},
		{
			name: "identity modifier renders in braces and is keyed",
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.1", Modifier: []string{"instruct"}},
			want: "llama@3.1{instruct}",
		},
		{
			name: "multiple identity modifiers in canonical order, comma-separated",
			// Input deliberately out of canonical order: thinking ranks before turbo.
			ref:  bestiary.EntityRef{Family: "kimi", Version: "k2", Modifier: []string{"turbo", "thinking"}},
			want: "kimi@k2{thinking,turbo}",
		},
		{
			name: "empty modifier list omits braces entirely",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5", Modifier: []string{}},
			want: "claude/opus@4.5",
		},
		{
			name: "nil modifier omits braces entirely",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5", Modifier: nil},
			want: "claude/opus@4.5",
		},
		{
			name: "all-empty modifier tokens collapse to no braces",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Modifier: []string{"", ""}},
			want: "claude/opus",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ref.String(); got != tc.want {
				t.Errorf("EntityRef.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEntityRef_String_VersionIsNotDate guards the IP-1 clarification: @version
// renders the identity Version, and EntityRef has no Date field to leak into the
// key. A ref with version "4.5" must key on @4.5 regardless of any release date
// (which is not part of the type).
func TestEntityRef_String_VersionIsNotDate(t *testing.T) {
	ref := bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"}
	if got := ref.String(); got != "claude/opus@4.5" {
		t.Fatalf("EntityRef.String() = %q, want %q (@version must be identity Version, not a date)", got, "claude/opus@4.5")
	}
}

// TestEntityRef_String_IsComparableKey verifies String() behaves as a comparable
// map key: identity-mod permutations collapse to one key, an identity modifier
// makes a DISTINCT key from the base, and the key can index a map.
func TestEntityRef_String_IsComparableKey(t *testing.T) {
	base := bestiary.EntityRef{Family: "meta", Variant: "llama", Version: "3.1"}
	withInstruct := bestiary.EntityRef{Family: "meta", Variant: "llama", Version: "3.1", Modifier: []string{"instruct"}}

	if base.String() == withInstruct.String() {
		t.Fatalf("identity modifier must produce a DISTINCT key: base=%q instruct=%q", base.String(), withInstruct.String())
	}

	// Permutations of the same identity-mod set must yield the identical key.
	a := bestiary.EntityRef{Family: "kimi", Version: "k2", Modifier: []string{"thinking", "turbo"}}
	b := bestiary.EntityRef{Family: "kimi", Version: "k2", Modifier: []string{"turbo", "thinking"}}
	if a.String() != b.String() {
		t.Fatalf("permuted identity-mod sets must yield identical key: %q != %q", a.String(), b.String())
	}

	index := map[string]int{}
	index[base.String()]++
	index[withInstruct.String()]++
	index[a.String()]++
	index[b.String()]++ // same key as a
	if index[a.String()] != 2 {
		t.Errorf("permuted refs should map to the same key bucket; got count %d, want 2", index[a.String()])
	}
	if len(index) != 3 {
		t.Errorf("expected 3 distinct keys (base, instruct, kimi), got %d", len(index))
	}
}

// TestDerivationKind_TextRoundTrip locks the lossless MarshalText/UnmarshalText
// round-trip for every DerivationKind constant, the wire names, and JSON
// embedding (DerivationKind must serialize as a string, not an integer).
func TestDerivationKind_TextRoundTrip(t *testing.T) {
	cases := []struct {
		kind bestiary.DerivationKind
		wire string
	}{
		{bestiary.DerivationNone, "none"},
		{bestiary.DerivationFinetune, "finetune"},
		{bestiary.DerivationMerge, "merge"},
		{bestiary.DerivationDistillation, "distillation"},
		{bestiary.DerivationQuantized, "quantized"},
		{bestiary.DerivationAdapter, "adapter"},
	}
	for _, tc := range cases {
		t.Run(tc.wire, func(t *testing.T) {
			// String() matches the wire name.
			if got := tc.kind.String(); got != tc.wire {
				t.Errorf("String() = %q, want %q", got, tc.wire)
			}
			// MarshalText emits the wire name.
			b, err := tc.kind.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error: %v", err)
			}
			if string(b) != tc.wire {
				t.Errorf("MarshalText() = %q, want %q", string(b), tc.wire)
			}
			// UnmarshalText round-trips back to the same kind.
			var got bestiary.DerivationKind
			if err := got.UnmarshalText([]byte(tc.wire)); err != nil {
				t.Fatalf("UnmarshalText(%q) error: %v", tc.wire, err)
			}
			if got != tc.kind {
				t.Errorf("UnmarshalText(%q) = %v, want %v", tc.wire, got, tc.kind)
			}
		})
	}
}

// TestDerivationKind_UnmarshalText_Unknown verifies an unrecognized token yields
// an error (not a silent default).
func TestDerivationKind_UnmarshalText_Unknown(t *testing.T) {
	var k bestiary.DerivationKind
	if err := k.UnmarshalText([]byte("pruned")); err == nil {
		t.Error("UnmarshalText(\"pruned\") = nil error, want an error for an unknown derivation kind")
	}
}

// TestDerivationKind_JSON_AsString confirms DerivationKind embeds in JSON as its
// text wire value (via encoding.TextMarshaler), e.g. inside a LineageEdge.
func TestDerivationKind_JSON_AsString(t *testing.T) {
	edge := bestiary.LineageEdge{
		Parent: bestiary.EntityRef{Family: "llama", Version: "3"},
		Kind:   bestiary.DerivationFinetune,
	}
	enc, err := json.Marshal(edge)
	if err != nil {
		t.Fatalf("json.Marshal(LineageEdge) error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(enc, &out); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if out["Kind"] != "finetune" {
		t.Errorf("LineageEdge.Kind JSON = %v (%T), want string \"finetune\"", out["Kind"], out["Kind"])
	}
}

// TestModifierClass_DefaultsToIdentity guards the fail-safe default: an
// unknown/uncurated modifier token classifies as Identity (never silently merge
// two artifacts into one entity), and ClassifyModifier never panics.
func TestModifierClass_DefaultsToIdentity(t *testing.T) {
	if bestiary.ModifierClassIdentity != 0 {
		t.Errorf("ModifierClassIdentity must be the zero value (0), got %d", bestiary.ModifierClassIdentity)
	}
	if got := bestiary.ClassifyModifier("totally-unknown-token", "no-such-family"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(unknown) = %v, want ModifierClassIdentity (fail-safe default)", got)
	}
}
