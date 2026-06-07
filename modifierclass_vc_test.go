package bestiary_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// modJoinCanon renders a modifier slice in canonical comma-joined form using the
// exported CanonicalizeModifiers, for order-independent comparison in tests.
func modJoinCanon(mods []string) string {
	return strings.Join(bestiary.CanonicalizeModifiers(mods), ",")
}

// ----------------------------------------------------------------------------
// VC6 — every modifier in the curated inventory classifies to its pinned class
// ----------------------------------------------------------------------------

// TestVC6_InventoryTokensPinned pins the class of every token in the curated
// 24-token inventory (global, family-agnostic). ATTRIBUTE tokens are per-instance
// presentation/runtime knobs; IDENTITY tokens distinguish the model artifact. The
// AMBIGUOUS tokens (turbo/fast/chat/pro/precision) default to IDENTITY globally —
// the safe over-split — and are demoted to ATTRIBUTE only by a per-family override
// (see VC7). mini/flash stay IDENTITY-class this epoch (size axis deferred).
func TestVC6_InventoryTokensPinned(t *testing.T) {
	attribute := []string{
		"thinking", "think", "preview", "latest", "original", "highspeed", "lightning",
	}
	identity := []string{
		// curated identity
		"instruct", "non-reasoning", "vision", "code", "omni", "multimodal",
		"deep-research", "base",
		// ambiguous tokens — global default is identity (safe over-split)
		"turbo", "fast", "chat", "pro", "precision",
		// stay-identity / extras
		"mini", "flash", "reasoning", "distill",
	}

	if total := len(attribute) + len(identity); total != 24 {
		t.Fatalf("inventory size = %d, want 24 pinned tokens", total)
	}

	for _, tok := range attribute {
		if got := bestiary.ClassifyModifier(tok, ""); got != bestiary.ModifierClassAttribute {
			t.Errorf("ClassifyModifier(%q, \"\") = %v, want ModifierClassAttribute", tok, got)
		}
	}
	for _, tok := range identity {
		if got := bestiary.ClassifyModifier(tok, ""); got != bestiary.ModifierClassIdentity {
			t.Errorf("ClassifyModifier(%q, \"\") = %v, want ModifierClassIdentity", tok, got)
		}
	}

	// Case-insensitivity: classification is not spelling-fragile.
	if got := bestiary.ClassifyModifier("Thinking", ""); got != bestiary.ModifierClassAttribute {
		t.Errorf("ClassifyModifier(%q) = %v, want ModifierClassAttribute (case-insensitive)", "Thinking", got)
	}
	// Graceful-degrade default: an uncurated token is IDENTITY and never panics.
	if got := bestiary.ClassifyModifier("totally-unknown", "no-such-family"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(unknown) = %v, want ModifierClassIdentity (fail-safe)", got)
	}
}

// ----------------------------------------------------------------------------
// VC7 — ambiguous token resolves both arms via per-family override
// ----------------------------------------------------------------------------

// TestVC7_AmbiguousBothArms verifies the canonical ambiguous case: "turbo" is an
// IDENTITY product-line token for gpt (gpt-4-turbo is a distinct model) but a mere
// speed ATTRIBUTE for glm (glm-5-turbo is the same artifact, faster serving). The
// gpt arm rides the global default (identity); the glm arm is demoted by a
// per-family override.
func TestVC7_AmbiguousBothArms(t *testing.T) {
	if got := bestiary.ClassifyModifier("turbo", "gpt"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(turbo, gpt) = %v, want ModifierClassIdentity", got)
	}
	if got := bestiary.ClassifyModifier("turbo", "glm"); got != bestiary.ModifierClassAttribute {
		t.Errorf("ClassifyModifier(turbo, glm) = %v, want ModifierClassAttribute", got)
	}

	// The override is family-scoped: it must NOT leak to other families.
	if got := bestiary.ClassifyModifier("turbo", "kimi"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(turbo, kimi) = %v, want ModifierClassIdentity (override is glm-only)", got)
	}

	// Projected into the identity key: gpt keeps turbo, glm drops it.
	if got := bestiary.EntityModifiers([]string{"turbo"}, "gpt"); modJoinCanon(got) != "turbo" {
		t.Errorf("EntityModifiers([turbo], gpt) = %v, want [turbo]", got)
	}
	if got := bestiary.EntityModifiers([]string{"turbo"}, "glm"); got != nil {
		t.Errorf("EntityModifiers([turbo], glm) = %v, want nil (turbo is an attribute for glm)", got)
	}
}

// ----------------------------------------------------------------------------
// VC4 — identity modifiers split a sibling entity; attribute modifiers link to base
// ----------------------------------------------------------------------------

// TestVC4_IdentitySplit_AttributeLink verifies the entity-keying consequence of
// classification: instruct/distill (IDENTITY) yield a DISTINCT sibling entity key;
// thinking/preview (ATTRIBUTE) collapse onto the base entity; an unknown modifier
// over-splits (distinct) by the fail-safe default.
func TestVC4_IdentitySplit_AttributeLink(t *testing.T) {
	base := bestiary.EntityRef{Family: "llama", Version: "3.1"}
	baseKey := base.String()
	if baseKey != "llama@3.1" {
		t.Fatalf("base key = %q, want %q", baseKey, "llama@3.1")
	}

	// IDENTITY modifiers → distinct sibling entity (modifier survives into the key).
	for _, tok := range []string{"instruct", "distill"} {
		idMods := bestiary.EntityModifiers([]string{tok}, "llama")
		if modJoinCanon(idMods) != tok {
			t.Errorf("EntityModifiers([%s], llama) = %v, want [%s] (identity)", tok, idMods, tok)
		}
		sib := bestiary.EntityRef{Family: "llama", Version: "3.1", Modifier: idMods}
		if sib.String() == baseKey {
			t.Errorf("entity key for %q must differ from base %q, got %q", tok, baseKey, sib.String())
		}
		if want := "llama@3.1{" + tok + "}"; sib.String() != want {
			t.Errorf("entity key for %q = %q, want %q", tok, sib.String(), want)
		}
	}

	// ATTRIBUTE modifiers → no identity mod → SAME key as base (links to base entity).
	for _, tok := range []string{"thinking", "preview"} {
		idMods := bestiary.EntityModifiers([]string{tok}, "llama")
		if idMods != nil {
			t.Errorf("EntityModifiers([%s], llama) = %v, want nil (attribute)", tok, idMods)
		}
		linked := bestiary.EntityRef{Family: "llama", Version: "3.1", Modifier: idMods}
		if linked.String() != baseKey {
			t.Errorf("attribute %q must link to base %q, got %q", tok, baseKey, linked.String())
		}
	}

	// UNKNOWN modifier → identity (over-split) → distinct.
	unk := bestiary.EntityModifiers([]string{"frobnicate"}, "llama")
	if modJoinCanon(unk) != "frobnicate" {
		t.Errorf("EntityModifiers([frobnicate], llama) = %v, want [frobnicate] (unknown→identity)", unk)
	}
}

// ----------------------------------------------------------------------------
// VC11 — class-aware {}/[] rendering, keying, and CLI round-trip
// ----------------------------------------------------------------------------

// TestVC11_Rendering_Keying verifies the {}/[] rendering split and its keying
// consequence: an identity-modifier model renders "{mod}" and keys by it (≠ base);
// an attribute-modifier model renders "[mod]" and keys to the base (== base).
func TestVC11_Rendering_Keying(t *testing.T) {
	// Identity modifier → "{instruct}" in the canonical render.
	idRef := bestiary.ModelRef{
		Provider: "meta", Family: "llama", Version: "3.1",
		Date: "2024-07-23", Modifier: []string{"instruct"},
	}
	gotID := idRef.Format(bestiary.SchemeCanonical)
	if want := "meta/llama/3.1@2024-07-23{instruct}"; gotID != want {
		t.Errorf("identity render = %q, want %q", gotID, want)
	}
	if strings.Contains(gotID, "[instruct]") {
		t.Errorf("identity modifier must NOT render in [], got %q", gotID)
	}
	// Its entity key carries {instruct} and differs from the bare base.
	idEntity := bestiary.EntityRef{Family: "llama", Version: "3.1",
		Modifier: bestiary.EntityModifiers([]string{"instruct"}, "llama")}
	if idEntity.String() != "llama@3.1{instruct}" {
		t.Errorf("identity entity key = %q, want %q", idEntity.String(), "llama@3.1{instruct}")
	}
	if idEntity.String() == (bestiary.EntityRef{Family: "llama", Version: "3.1"}).String() {
		t.Errorf("identity entity must be DISTINCT from base llama@3.1")
	}

	// Attribute modifier → "[thinking]" in the canonical render.
	attrRef := bestiary.ModelRef{
		Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4.5",
		Modifier: []string{"thinking"},
	}
	gotAttr := attrRef.Format(bestiary.SchemeCanonical)
	if want := "anthropic/claude/opus/4.5[thinking]"; gotAttr != want {
		t.Errorf("attribute render = %q, want %q", gotAttr, want)
	}
	if strings.Contains(gotAttr, "{") {
		t.Errorf("attribute modifier must NOT render in {}, got %q", gotAttr)
	}
	// Its entity key has NO identity modifier and equals the base entity.
	attrEntity := bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5",
		Modifier: bestiary.EntityModifiers([]string{"thinking"}, "claude")}
	baseEntity := bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"}
	if attrEntity.String() != baseEntity.String() {
		t.Errorf("attribute entity %q must EQUAL base %q", attrEntity.String(), baseEntity.String())
	}

	// Mixed: identity in {}, attribute in [], identity-first.
	mixed := bestiary.ModelRef{
		Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4.5",
		Modifier: []string{"thinking", "instruct"},
	}
	if want := "anthropic/claude/opus/4.5{instruct}[thinking]"; mixed.Format(bestiary.SchemeCanonical) != want {
		t.Errorf("mixed render = %q, want %q", mixed.Format(bestiary.SchemeCanonical), want)
	}
}

// TestVC11_CLIRoundTrip verifies that the "{identity-mods}" canonical segment
// round-trips through Resolve (the path the CLI `show` uses): a real catalog model
// carrying an identity modifier, rendered to canonical and resolved back, returns a
// ref matching the original (Family, Variant, Version, Date, Modifier) tuple.
func TestVC11_CLIRoundTrip(t *testing.T) {
	// Discover a static model with an identity-class modifier and a non-empty
	// Variant + Date (so the segment parser is unambiguous).
	var seed *bestiary.ModelRef
	for _, m := range bestiary.StaticModels() {
		if m.Family == "" || m.Variant == "" || m.Date == "" {
			continue
		}
		if len(bestiary.EntityModifiers(m.Modifier, m.Family)) == 0 {
			continue
		}
		r := m.Ref()
		seed = &r
		break
	}
	if seed == nil {
		t.Skip("no static model with identity modifier + variant + date found")
	}

	canonical := seed.Format(bestiary.SchemeCanonical)
	if !strings.Contains(canonical, "{") {
		t.Fatalf("seed canonical %q lacks a {identity-mods} segment", canonical)
	}

	matched := func(refs []bestiary.ModelRef) bool {
		for _, r := range refs {
			if r.Family == seed.Family && r.Variant == seed.Variant &&
				r.Version == seed.Version && r.Date == seed.Date &&
				modJoinCanon(r.Modifier) == modJoinCanon(seed.Modifier) {
				return true
			}
		}
		return false
	}

	refs, err := bestiary.Resolve(canonical)
	if err != nil {
		// Multiple providers may host the same canonical → ErrAmbiguous still
		// proves the {} segment parsed; the candidate set must contain the seed.
		var ambig *bestiary.ErrAmbiguous
		if errors.As(err, &ambig) {
			if !matched(ambig.Candidates) {
				t.Fatalf("Resolve(%q) ambiguous but seed tuple not among candidates", canonical)
			}
			return
		}
		t.Fatalf("Resolve(%q) = error %v; {} round-trip must succeed", canonical, err)
	}
	if !matched(refs) {
		t.Errorf("Resolve(%q): no ref matched seed (Family=%q Variant=%q Version=%q Date=%q Modifier=%q)",
			canonical, seed.Family, seed.Variant, seed.Version, seed.Date, seed.Modifier)
	}
}

// ----------------------------------------------------------------------------
// VC12 — backward-compat: attribute-only canonical byte-identical; identity changed
// ----------------------------------------------------------------------------

// TestVC12_BackwardCompat verifies the bounded backward-compatibility contract:
// a render whose modifiers are ALL attribute-class is BYTE-IDENTICAL to the
// pre-class (v0.2.2) single-bracket form, while a render carrying an identity
// modifier changes (the identity token moves [] → {}). The attribute-only golden
// below is the exact string asserted by the pre-existing canonical bracket-suffix
// tests, re-asserted here to lock the byte-identity guarantee.
func TestVC12_BackwardCompat(t *testing.T) {
	// Attribute-only ("thinking") — must match the legacy golden byte-for-byte.
	attrOnly := bestiary.ModelRef{
		Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4.6",
		Date: "2026-02-05", Modifier: []string{"thinking"},
	}
	const legacyGolden = "anthropic/claude/opus/4.6@2026-02-05[thinking]"
	if got := attrOnly.Format(bestiary.SchemeCanonical); got != legacyGolden {
		t.Errorf("attribute-only render = %q, want %q (must be byte-identical to v0.2.2)", got, legacyGolden)
	}

	// Multiple attribute tokens stay comma-joined in a single [] bracket (legacy form).
	multiAttr := bestiary.ModelRef{
		Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4.6",
		Modifier: []string{"latest", "thinking"},
	}
	if got := multiAttr.Format(bestiary.SchemeCanonical); got != "anthropic/claude/opus/4.6[thinking,latest]" {
		t.Errorf("multi-attribute render = %q, want %q", got, "anthropic/claude/opus/4.6[thinking,latest]")
	}

	// Identity modifier ("instruct") — render CHANGES: token moves into {} (the
	// documented, golden-updated divergence from v0.2.2's [instruct]).
	idMod := bestiary.ModelRef{
		Provider: "meta", Family: "llama", Version: "3.1",
		Date: "2024-07-23", Modifier: []string{"instruct"},
	}
	got := idMod.Format(bestiary.SchemeCanonical)
	if !strings.Contains(got, "{instruct}") {
		t.Errorf("identity render %q must contain {instruct}", got)
	}
	if strings.Contains(got, "[instruct]") {
		t.Errorf("identity render %q must NOT use the legacy [instruct] form", got)
	}
}
