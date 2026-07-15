package bestiary_test

import (
	"encoding/json"
	"strings"
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

// TestDerivationKind_OutOfRange covers the defensive out-of-range paths so a
// corrupt/invalid enum value never panics or silently serializes as garbage:
// String() returns a diagnostic form and MarshalText returns an actionable error.
func TestDerivationKind_OutOfRange(t *testing.T) {
	bad := bestiary.DerivationKind(99)
	if got := bad.String(); got != "derivationkind(99)" {
		t.Errorf("DerivationKind(99).String() = %q, want %q", got, "derivationkind(99)")
	}
	if _, err := bad.MarshalText(); err == nil {
		t.Error("DerivationKind(99).MarshalText() = nil error, want an out-of-range error")
	}
	// Negative values take the same guarded path.
	neg := bestiary.DerivationKind(-1)
	if got := neg.String(); got != "derivationkind(-1)" {
		t.Errorf("DerivationKind(-1).String() = %q, want %q", got, "derivationkind(-1)")
	}
	if _, err := neg.MarshalText(); err == nil {
		t.Error("DerivationKind(-1).MarshalText() = nil error, want an out-of-range error")
	}
}

// TestHost_IsKnown pins the known/unknown Host classification: named constants
// (except HostNone) are known; HostNone and arbitrary backends are not — but the
// latter remain valid Host values.
func TestHost_IsKnown(t *testing.T) {
	known := []bestiary.Host{bestiary.HostAzure, bestiary.HostAWS, bestiary.HostGCP, bestiary.HostCloudflare}
	for _, h := range known {
		if !h.IsKnown() {
			t.Errorf("Host(%q).IsKnown() = false, want true", h)
		}
	}
	if bestiary.HostNone.IsKnown() {
		t.Error("HostNone.IsKnown() = true, want false (the zero value is not a known backend)")
	}
	if bestiary.Host("some-future-backend").IsKnown() {
		t.Error("Host(\"some-future-backend\").IsKnown() = true, want false (unrecognized backend)")
	}
}

// TestModifierClass_String covers the stringer for both members and the guarded
// default for an out-of-range class value.
func TestModifierClass_String(t *testing.T) {
	if got := bestiary.ModifierClassIdentity.String(); got != "identity" {
		t.Errorf("ModifierClassIdentity.String() = %q, want %q", got, "identity")
	}
	if got := bestiary.ModifierClassAttribute.String(); got != "attribute" {
		t.Errorf("ModifierClassAttribute.String() = %q, want %q", got, "attribute")
	}
	if got := bestiary.ModifierClass(99).String(); got != "identity" {
		t.Errorf("ModifierClass(99).String() = %q, want %q (fail-safe default)", got, "identity")
	}
}

// TestEntityModifiers covers the identity-projection used to build the entity key.
// With the curated modifier-class table loaded, the projection retains only
// IDENTITY-class tokens (dropping ATTRIBUTE-class tokens such as "thinking") and
// pins the canonicalization + dedup + empty-collapse behavior that the key
// construction depends on.
func TestEntityModifiers(t *testing.T) {
	// Empty / all-empty inputs collapse to nil (canonical "no modifiers").
	if got := bestiary.EntityModifiers(nil, "llama"); got != nil {
		t.Errorf("EntityModifiers(nil) = %v, want nil", got)
	}
	if got := bestiary.EntityModifiers([]string{"", ""}, "llama"); got != nil {
		t.Errorf("EntityModifiers(all-empty) = %v, want nil", got)
	}
	// ATTRIBUTE-class tokens are dropped from the identity projection: "thinking"
	// is attribute, "turbo" is identity (global default; kimi has no override), so
	// the de-duplicated projection keeps only "turbo".
	got := bestiary.EntityModifiers([]string{"turbo", "thinking", "turbo"}, "kimi")
	want := []string{"turbo"}
	if len(got) != len(want) {
		t.Fatalf("EntityModifiers dedup/class-filter = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("EntityModifiers[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// The projection feeds EntityRef.String(): keying on the projected mods must
	// match rendering them directly. "thinking" never reaches the key.
	ref := bestiary.EntityRef{Family: "kimi", Version: "k2", Modifier: got}
	if ref.String() != "kimi@k2{turbo}" {
		t.Errorf("EntityRef keyed on EntityModifiers = %q, want %q", ref.String(), "kimi@k2{turbo}")
	}
}

// TestEntityRef_ParamSizeDistinct verifies that two ModelInfo rows identical in
// every identity field except ParamSize produce DISTINCT entity keys via the
// registry grouping path (#size carrier), and that each key contains the
// expected "#<size>" segment.
func TestEntityRef_ParamSizeDistinct(t *testing.T) {
	// Build the two EntityRefs directly (the registry grouping path is tested via
	// registry_test and entity_aggregate_test; here we lock the key shape).
	ref70 := bestiary.EntityRef{
		Family:    "llama",
		Version:   "3.3",
		ParamSize: "70b",
		Modifier:  []string{"instruct"},
	}
	ref8 := bestiary.EntityRef{
		Family:    "llama",
		Version:   "3.3",
		ParamSize: "8b",
		Modifier:  []string{"instruct"},
	}

	key70 := ref70.String()
	key8 := ref8.String()

	// Keys must be distinct.
	if key70 == key8 {
		t.Fatalf("70b and 8b refs produced identical key %q; expected distinct keys", key70)
	}

	// Each key must contain the expected #size segment.
	if !strings.Contains(key70, "#70b") {
		t.Errorf("key70 = %q; expected it to contain \"#70b\"", key70)
	}
	if !strings.Contains(key8, "#8b") {
		t.Errorf("key8 = %q; expected it to contain \"#8b\"", key8)
	}

	// Full expected key forms.
	wantKey70 := "llama@3.3#70b{instruct}"
	wantKey8 := "llama@3.3#8b{instruct}"
	if key70 != wantKey70 {
		t.Errorf("key70 = %q, want %q", key70, wantKey70)
	}
	if key8 != wantKey8 {
		t.Errorf("key8 = %q, want %q", key8, wantKey8)
	}
}

// TestEntityRef_NoMigrationDrift is the registry-level successor invariant for the
// full-bulk #size re-key. Before v0.2.6 only three curated quant_vram.json IDs were
// sized; the shared enrichment now sizes every ID that carries a mechanical size
// token, so the guard shifts from "no catalog entity may bear a #" to a bounded,
// consistency-checked census. It enforces four invariants:
//
//	(a) CENSUS-LITERAL sized-entity count: the number of '#'-bearing CATALOG entities
//	    (with provider instances) and metadata-only STANDALONE entities (no instances)
//	    are each pinned to an exact literal. These literals change ONCE, intentionally,
//	    with the full-bulk re-key; any later drift signals a silently added or dropped
//	    sized entity.
//
//	(b) PER-SHAPE EXEMPLAR KEY PINS: one representative of each param-shape family must
//	    exist with the exact #size segment — dense (#8b), decimal dense (#0.6b), active
//	    MoE (#30b-a3b), NxM MoE (#8x22b), and count-suffixed MoE (#17b-16e, a curated
//	    llama-4 scout pin) — so a regression in any shape's carrier is caught.
//
//	(c) ENRICHMENT-CONSISTENCY SWEEP (forward AND inverse in one pass): every CATALOG
//	    entity's baked ParamSize must equal the LIVE EnrichedParamSize of each of its
//	    instance IDs. Forward — a #size is present only when the ID genuinely resolves
//	    one (no invented size); inverse — an unsized key means NO instance resolves a
//	    size, which is exactly how the suppress-pin (qwen3-coder-next-fp8-1m, where
//	    "1m" is a context-tier marker) is verified to hold: its entity must stay
//	    unsized. The sweep re-derives from the ID rather than reading the baked value,
//	    so it is not a tautology on production output.
//
//	(d) ENTITY-COUNT FLOOR (retained): the registry must hold at least wantMinEntities,
//	    so an inadvertent truncation of the catalog is caught.
func TestEntityRef_NoMigrationDrift(t *testing.T) {
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("static registry is empty; cannot check migration drift")
	}

	// A metadata-only standalone entity has NO provider instances (it is synthesized
	// from a models.dev metadata row whose family is absent from the catalog). Every
	// real catalog entity carries at least one instance, so len(Instances)==0 uniquely
	// identifies a standalone.
	isStandalone := func(e bestiary.Entity) bool { return len(e.Instances) == 0 }

	// (a) Census literals — pinned to the full-bulk re-key snapshot. A change here is
	// an intentional re-key event, not incidental drift.
	const (
		wantSizedCatalog    = 335
		wantSizedStandalone = 4
	)

	// (b) Per-shape exemplar keys that must be present after the re-key.
	wantExemplars := []string{
		"llama@3.1#8b{instruct}",          // dense
		"qwen/embedding@3#0.6b",           // decimal dense — never "6b"
		"qwen@3#30b-a3b",                  // active MoE
		"wizardlm@2#8x22b",                // NxM MoE (ExpertCount + PerExpertParams, no total)
		"llama/scout@4#17b-16e{instruct}", // count-suffixed MoE via the curated llama-4 pin (@4 after the version unification)
	}
	keyIndex := make(map[string]bestiary.Entity, len(entities))
	for _, e := range entities {
		keyIndex[e.Ref.String()] = e
	}
	for _, want := range wantExemplars {
		if _, ok := keyIndex[want]; !ok {
			t.Errorf("per-shape exemplar sized key %q missing from the registry\n"+
				"  What: a representative #size entity for one param-shape is absent\n"+
				"  Why: a regression in the size carrier, the shape grammar, or a curated pin\n"+
				"  How to fix: run 'go run ./cmd/bestiary-gen --no-fetch'; if the re-key intentionally moved this key, update the exemplar",
				want)
		}
	}

	// (a)+(c): count '#'-bearing entities and run the enrichment-consistency sweep.
	sizedCatalog, sizedStandalone := 0, 0
	for _, e := range entities {
		if strings.Contains(e.Ref.String(), "#") {
			if isStandalone(e) {
				sizedStandalone++
			} else {
				sizedCatalog++
			}
		}
		if isStandalone(e) {
			continue // standalones carry no instance IDs to re-derive the size from.
		}
		for _, inst := range e.Instances {
			got, _ := bestiary.EnrichedParamSize(string(inst.ID))
			if got != e.Ref.ParamSize {
				t.Errorf("instance %q: live EnrichedParamSize=%q but entity %q baked ParamSize=%q\n"+
					"  What: the baked static size and the re-derived size disagree\n"+
					"  How to fix: run 'go run ./cmd/bestiary-gen --no-fetch' to re-bake, or fix the enrichment precedence",
					inst.ID, got, e.Ref.String(), e.Ref.ParamSize)
			}
		}
	}
	if sizedCatalog != wantSizedCatalog {
		t.Errorf("sized CATALOG entity count = %d, want %d (census literal)\n"+
			"  What: the number of '#'-bearing catalog entities changed\n"+
			"  How to fix: if the re-key intentionally shifted this, update wantSizedCatalog; otherwise regen",
			sizedCatalog, wantSizedCatalog)
	}
	if sizedStandalone != wantSizedStandalone {
		t.Errorf("sized STANDALONE entity count = %d, want %d (census literal)", sizedStandalone, wantSizedStandalone)
	}

	// (c) explicit suppress-pin guard: the ONE suppress-pinned ID must resolve to NO
	// size, so it never keys #1m and stays grouped with its unsized siblings.
	if got, _ := bestiary.EnrichedParamSize("qwen/qwen3-coder-next-fp8-1m"); got != "" {
		t.Errorf("suppress-pin failed: EnrichedParamSize(qwen/qwen3-coder-next-fp8-1m) = %q, want \"\" (\"1m\" is a 1M-context tier marker, not params)", got)
	}

	// (d) entity-count floor.
	const wantMinEntities = 600
	if len(entities) < wantMinEntities {
		t.Errorf("entity count = %d, want >= %d; a large drop signals registry truncation", len(entities), wantMinEntities)
	}

	t.Logf("checked %d entities: %d sized (catalog), %d sized (standalone), %d unsized",
		len(entities), sizedCatalog, sizedStandalone, len(entities)-sizedCatalog-sizedStandalone)
}

// TestParamSizePins_Llama4CensusBothLegs is the automated guard for the dual-method
// llama-4 pin census. Every catalog llama-4 scout/maverick ID — identified by BOTH
// the decomposition leg (family=llama, version=4, variant scout|maverick) AND the
// literal-substring leg ("llama4"/"llama-4") — must key its FULL expert-shape size:
// scout = 17b-16e, maverick = 17b-128e; never a bare 17b and never unsized.
//
// Neither leg alone is sufficient, which is the whole point: the maverick spellings
// key llama@4 with an EMPTY variant ("maverick" is not a curated family member), so
// they ESCAPE the (variant scout|maverick) decomposition leg and are reached only by
// the substring sweep plus their explicit curated pins. A purely-decomposition census
// would silently leave those bare #17b — this guard fails if that regresses. (The
// version-less scout/maverick spellings now carry a curated @4 version pin, so they no
// longer split on version presence; the size pins guarded here are independent of that
// and unchanged.)
func TestParamSizePins_Llama4CensusBothLegs(t *testing.T) {
	checked := 0
	for _, m := range bestiary.StaticModels() {
		id := strings.ToLower(string(m.ID))
		substrLeg := strings.Contains(id, "llama4") || strings.Contains(id, "llama-4")
		decompLeg := m.Family == bestiary.FamilyLlama && m.Version == "4" &&
			(m.Variant == "scout" || m.Variant == "maverick")
		if !substrLeg && !decompLeg {
			continue
		}
		isScout := strings.Contains(id, "scout")
		isMaverick := strings.Contains(id, "maverick")
		if !isScout && !isMaverick {
			continue // a llama-4 that is neither scout nor maverick carries no pin.
		}
		checked++
		want := "17b-16e"
		if isMaverick {
			want = "17b-128e"
		}
		if m.ParamSize != want {
			t.Errorf("llama-4 %q keys ParamSize=%q, want %q\n"+
				"  What: a llama-4 scout/maverick ID is not carrying its full expert shape\n"+
				"  Why: a curated pin is missing (census gap) — a bare #17b splits the artifact from its siblings\n"+
				"  How to fix: add the ID to parse/data/param_size_overrides.json with the full-shape token",
				m.ID, m.ParamSize, want)
		}
	}
	if checked == 0 {
		t.Fatal("no llama-4 scout/maverick IDs found — the census guard is vacuous; the vendored catalog changed")
	}
	t.Logf("llama-4 census guard checked %d scout/maverick IDs (both legs)", checked)
}

// TestLlama4VersionPins_UnifiedEntityMembership directly asserts that every
// version-less llama-4 scout/maverick spelling carrying a curated @4 version pin
// (the exact-ID overrides in parse.go) lands INSIDE the unified @4 entity —
// llama/scout@4#17b-16e{instruct} for scout, llama@4#17b-128e{instruct} for
// maverick (maverick is not a curated llama variant member, so its official
// entity carries no /maverick segment). The sized-entity census literal fences
// this only indirectly (a dropped pin shifts a count somewhere); this test names
// the exact spelling that regressed. Membership is checked by EXACT instance-ID
// match, not substring, because the dotted Bedrock spellings nest
// ("meta.llama4-…" is a substring of "us.meta.llama4-…").
func TestLlama4VersionPins_UnifiedEntityMembership(t *testing.T) {
	holdsExact := func(e bestiary.Entity, id string) bool {
		for _, inst := range e.Instances {
			if string(inst.ID) == id {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name      string
		variant   string
		paramSize string
		wantKey   string
		ids       []string
	}{
		{
			name:      "scout",
			variant:   "scout",
			paramSize: "17b-16e",
			wantKey:   "llama/scout@4#17b-16e{instruct}",
			ids: []string{
				"meta.llama4-scout-17b-instruct-v1:0",
				"us.meta.llama4-scout-17b-instruct-v1:0",
				"cerebras-llama-4-scout-17b-16e-instruct",
			},
		},
		{
			name:      "maverick",
			variant:   "",
			paramSize: "17b-128e",
			wantKey:   "llama@4#17b-128e{instruct}",
			ids: []string{
				"meta.llama4-maverick-17b-instruct-v1:0",
				"us.meta.llama4-maverick-17b-instruct-v1:0",
				"cerebras-llama-4-maverick-17b-128e-instruct",
				"groq-llama-4-maverick-17b-128e-instruct",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ent, ok := bestiary.EntityByTuple("llama", tc.variant, "4", tc.paramSize, "instruct")
			if !ok {
				t.Fatalf("unified entity %q missing from the registry — the @4 target of the version pins does not exist", tc.wantKey)
			}
			if got := ent.Ref.String(); got != tc.wantKey {
				t.Fatalf("entity key = %q, want %q", got, tc.wantKey)
			}
			for _, id := range tc.ids {
				if !holdsExact(ent, id) {
					t.Errorf("pinned spelling %q is not an instance of %q\n"+
						"  Why: its curated @4 version pin (exact-ID override) is missing or was dropped, re-splitting the spelling into a version-less entity\n"+
						"  How to fix: restore the ID's idFamilyOverrides entry in parse.go and regen",
						id, tc.wantKey)
				}
			}
		})
	}
}

// TestEntityRef_String_ParamSizeGrammar locks the full #size grammar for all
// combinations: present/absent paramsize, with/without variant, version, mods.
func TestEntityRef_String_ParamSizeGrammar(t *testing.T) {
	cases := []struct {
		name string
		ref  bestiary.EntityRef
		want string
	}{
		{
			name: "paramsize only (no version no mods)",
			ref:  bestiary.EntityRef{Family: "llama", ParamSize: "70b"},
			want: "llama#70b",
		},
		{
			name: "paramsize after version",
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b"},
			want: "llama@3.3#70b",
		},
		{
			name: "paramsize before mods",
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}},
			want: "llama@3.3#70b{instruct}",
		},
		{
			name: "full tuple: family/variant@version#size{mods}",
			ref:  bestiary.EntityRef{Family: "qwen", Variant: "coder", Version: "2.5", ParamSize: "7b", Modifier: []string{"instruct"}},
			want: "qwen/coder@2.5#7b{instruct}",
		},
		{
			name: "active-MoE shape token renders verbatim",
			ref:  bestiary.EntityRef{Family: "qwen", Version: "3", ParamSize: "30b-a3b"},
			want: "qwen@3#30b-a3b",
		},
		{
			name: "NxM MoE shape token renders verbatim",
			ref:  bestiary.EntityRef{Family: "wizardlm", Version: "2", ParamSize: "8x22b"},
			want: "wizardlm@2#8x22b",
		},
		{
			name: "count-suffixed MoE shape token renders verbatim",
			ref:  bestiary.EntityRef{Family: "llama", Variant: "scout", ParamSize: "17b-16e", Modifier: []string{"instruct"}},
			want: "llama/scout#17b-16e{instruct}",
		},
		{
			name: "decimal dense shape token renders verbatim",
			ref:  bestiary.EntityRef{Family: "qwen", Variant: "embedding", Version: "3", ParamSize: "0.6b"},
			want: "qwen/embedding@3#0.6b",
		},
		{
			name: "empty paramsize omits # entirely (backward compat)",
			ref:  bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"},
			want: "claude/opus@4.5",
		},
		{
			name: "zero-value paramsize (empty string) omits # entirely",
			ref:  bestiary.EntityRef{Family: "llama", Version: "3.3"},
			want: "llama@3.3",
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
