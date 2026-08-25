package bestiary_test

import (
	"encoding/json"
	"errors"
	"os"
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

// TestVC6_InventoryTokensPinned asserts that every token in the curated global
// modifier-class inventory classifies to the class the curation file gives it, with the
// inventory DERIVED from parse/data/modifier_class.json rather than restated here.
// ATTRIBUTE tokens are per-instance presentation/runtime/pricing knobs; IDENTITY tokens
// distinguish the model artifact. The AMBIGUOUS tokens (turbo/fast/chat/pro/precision)
// default to IDENTITY globally — the safe over-split — and are demoted to ATTRIBUTE only
// by a per-family override (see VC7). mini/flash stay IDENTITY-class this epoch (size
// axis deferred).
//
// Single source of truth, enforced rather than described: parse/data/modifier_class.json
// is canonical for the classification, and this test reads it. Adding, removing or
// re-classifying a global token needs NO edit here and cannot leave the guard stale —
// which is exactly what the previous hand-maintained token list did (it sat at 21 while
// the file held 25, gated by nothing). Two things are still stated in code, because a
// derivation cannot catch them: the load-bearing floor below (tokens whose curation is
// itself the invariant, so silently deleting a row is caught) and the migrated stage
// tokens, which are DELIBERATELY absent from the file and must still be routed out of
// the entity key before the unknown->Identity fail-safe would promote them.
func TestVC6_InventoryTokensPinned(t *testing.T) {
	raw, err := os.ReadFile("parse/data/modifier_class.json")
	if err != nil {
		t.Fatalf("read curated modifier-class table: %v\n"+
			"  What: the inventory this test derives from could not be read\n"+
			"  Where: parse/data/modifier_class.json, read from the package directory\n"+
			"  How to fix: run the test from the module root, or restore the curated file", err)
	}
	var file struct {
		Global map[string]string `json:"global"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse curated modifier-class table: %v\n"+
			"  What: parse/data/modifier_class.json is not valid JSON in the expected shape\n"+
			"  How to fix: validate the file — the loader degrades silently on this, the test does not", err)
	}
	if len(file.Global) == 0 {
		t.Fatal("curated global modifier-class inventory is empty — the file loaded but classified nothing")
	}

	// Set equality, both directions: every curated token classifies to its curated class,
	// and the classification of a curated token is never the other class. Running the
	// comparison over the file's own key set is what makes the inventory self-updating.
	wantClass := map[string]bestiary.ModifierClass{}
	for tok, cls := range file.Global {
		switch cls {
		case "attribute":
			wantClass[strings.ToLower(tok)] = bestiary.ModifierClassAttribute
		case "identity":
			wantClass[strings.ToLower(tok)] = bestiary.ModifierClassIdentity
		default:
			t.Errorf("curated token %q carries class %q, want \"identity\" or \"attribute\" — "+
				"the loader SKIPS an unrecognized class string, so this row would silently "+
				"fall through to the unknown->Identity fail-safe", tok, cls)
		}
	}
	for tok, want := range wantClass {
		if got := bestiary.ClassifyModifier(tok, ""); got != want {
			t.Errorf("ClassifyModifier(%q, \"\") = %v, want %v (curated global class)", tok, got, want)
		}
	}

	// Load-bearing floor: these tokens must remain curated, with these classes. This is
	// NOT the inventory (it never grows with the file) — it is the subset whose presence
	// is itself an invariant, so a deletion cannot pass the set-equality check above by
	// simply removing the row. Each entry names why it is load-bearing.
	floor := map[string]bestiary.ModifierClass{
		"thinking":  bestiary.ModifierClassAttribute, // reasoning-mode knob; identity would split every thinking pair
		"free":      bestiary.ModifierClassAttribute, // pricing/serving tier; identity would re-split the free-tier keys
		"realtime":  bestiary.ModifierClassAttribute, // serving mode, not a distinct artifact
		"instruct":  bestiary.ModifierClassIdentity,  // distinct post-trained artifact
		"base":      bestiary.ModifierClassIdentity,  // distinct pre-instruct artifact
		"turbo":     bestiary.ModifierClassIdentity,  // AMBIGUOUS: global default is the safe over-split
		"fast":      bestiary.ModifierClassIdentity,  // AMBIGUOUS: demoted per-family only (see VC7)
		"chat":      bestiary.ModifierClassIdentity,  // AMBIGUOUS
		"pro":       bestiary.ModifierClassIdentity,  // AMBIGUOUS
		"precision": bestiary.ModifierClassIdentity,  // AMBIGUOUS
		"mini":      bestiary.ModifierClassIdentity,  // stays identity this epoch (size axis deferred)
		"flash":     bestiary.ModifierClassIdentity,  // stays identity this epoch (size axis deferred)
	}
	for tok, want := range floor {
		got, ok := wantClass[tok]
		if !ok {
			t.Errorf("load-bearing token %q is absent from the curated global inventory — "+
				"removing it drops the token to the unknown->Identity fail-safe", tok)
			continue
		}
		if got != want {
			t.Errorf("load-bearing token %q is curated %v, want %v", tok, got, want)
		}
	}

	// Migrated stage tokens: preview/latest/original are recognized on the stage
	// axis (DetectReleaseStage) and, although absent from modifier_class.json, are
	// routed OUT of the identity key by EntityModifiers (via isStageToken) BEFORE
	// the fail-safe — so they never split an entity. They are also excluded from the
	// attribute-render subset (verified by VC12 + the resolve backward-compat test).
	for _, tok := range []string{"preview", "latest", "original"} {
		if _, ok := bestiary.DetectReleaseStage(tok); !ok {
			t.Errorf("DetectReleaseStage(%q) not recognized — migrated stage token must be on the stage axis", tok)
		}
		if got := bestiary.EntityModifiers([]string{tok}, "llama"); got != nil {
			t.Errorf("EntityModifiers([%s], llama) = %v, want nil (stage-routed out of the key before the identity fail-safe)", tok, got)
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

	// The override is family-scoped: it must NOT leak to families without one.
	// llama is the probe because it carries no turbo override; kimi used to play this
	// role and can no longer, since kimi gained its own curated turbo demotion — a
	// scoping fence has to be anchored on a family that is genuinely un-overridden,
	// or it stops testing scoping at all.
	if got := bestiary.ClassifyModifier("turbo", "llama"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(turbo, llama) = %v, want ModifierClassIdentity (no override for llama)", got)
	}
	// …and the families that DO carry one are demoted, each for its own curated reason.
	for _, fam := range []bestiary.Family{"glm", "kimi", "minimax"} {
		if got := bestiary.ClassifyModifier("turbo", fam); got != bestiary.ModifierClassAttribute {
			t.Errorf("ClassifyModifier(turbo, %s) = %v, want ModifierClassAttribute (curated per-family demotion)", fam, got)
		}
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

// TestVC11_UnionRoundTrip_MixedModifier drives the NEW matchCanonicalSegments
// arms through Resolve for a MIXED-modifier model (one identity + one attribute
// modifier): the "[attributes]" strip arm and the "union of {} and [] tokens"
// matcher. Both the class-aware split form ("…{identity}[attribute]") AND the
// legacy combined form ("…[identity,attribute]") must resolve to the SAME ref,
// and a render with a WRONG attribute token must NOT match that ref. Without this,
// a regression in the attributeFilter branch or the union logic would slip past
// the identity-only round-trip in TestVC11_CLIRoundTrip.
func TestVC11_UnionRoundTrip_MixedModifier(t *testing.T) {
	// Discover a static model whose modifier set is genuinely MIXED: at least one
	// identity-class token (EntityModifiers non-empty) AND at least one
	// attribute-class token (some token dropped from the identity projection),
	// with a non-empty Variant + Date so segment parsing is unambiguous.
	var seed *bestiary.ModelRef
	for _, m := range bestiary.StaticModels() {
		if m.Family == "" || m.Variant == "" || m.Date == "" {
			continue
		}
		all := bestiary.CanonicalizeModifiers(m.Modifier)
		id := bestiary.EntityModifiers(m.Modifier, m.Family)
		if len(id) == 0 || len(all) <= len(id) {
			continue // not mixed: no identity token, or nothing demoted to attribute
		}
		r := m.Ref()
		seed = &r
		break
	}
	if seed == nil {
		t.Skip("no static model with a mixed identity+attribute modifier set found")
	}

	classAware := seed.Format(bestiary.SchemeCanonical)
	brace := strings.Index(classAware, "{")
	if brace < 0 || !strings.Contains(classAware, "[") {
		t.Fatalf("mixed seed canonical %q lacks both {} and [] segments", classAware)
	}
	base := classAware[:brace]
	legacyCombined := base + "[" + strings.Join(bestiary.CanonicalizeModifiers(seed.Modifier), ",") + "]"

	matchesSeed := func(refs []bestiary.ModelRef) bool {
		for _, r := range refs {
			if r.Family == seed.Family && r.Variant == seed.Variant &&
				r.Version == seed.Version && r.Date == seed.Date &&
				modJoinCanon(r.Modifier) == modJoinCanon(seed.Modifier) {
				return true
			}
		}
		return false
	}
	// resolveMatches returns whether the seed tuple is present among Resolve's
	// results, tolerating ErrAmbiguous (multiple providers host the same canonical)
	// as a positive signal that the segments parsed and matched.
	resolveMatches := func(input string) bool {
		refs, err := bestiary.Resolve(input)
		if err != nil {
			var ambig *bestiary.ErrAmbiguous
			if errors.As(err, &ambig) {
				return matchesSeed(ambig.Candidates)
			}
			return false
		}
		return matchesSeed(refs)
	}

	// Both the class-aware split form and the legacy combined form resolve to the
	// same model — the union matcher is class-agnostic on input.
	if !resolveMatches(classAware) {
		t.Errorf("class-aware form %q did not resolve to seed (Family=%q Variant=%q Version=%q Date=%q Modifier=%q)",
			classAware, seed.Family, seed.Variant, seed.Version, seed.Date, seed.Modifier)
	}
	if !resolveMatches(legacyCombined) {
		t.Errorf("legacy combined form %q did not resolve to seed; union matcher must accept the all-bracket form too",
			legacyCombined)
	}

	// Negative: a render with a WRONG attribute token (not in the model's set)
	// must NOT match the seed — the attributeFilter/union branch is doing real work.
	wrongAttr := base + "{" + modJoinCanon(bestiary.EntityModifiers(seed.Modifier, seed.Family)) + "}[definitely-not-a-real-modifier]"
	if resolveMatches(wrongAttr) {
		t.Errorf("form with a bogus attribute %q must NOT match the seed", wrongAttr)
	}
}

// ----------------------------------------------------------------------------
// VC16 — "fast" is a per-family speed ATTRIBUTE for tiered families, but stays
// an IDENTITY token globally (distinct-model families like grok/imagen/veo)
// ----------------------------------------------------------------------------

// TestVC16_FastPerFamilyDemotion pins the both-arm contract for the "fast"
// modifier. For families where "fast" is a SPEED-TIER knob over the same artifact
// (claude, glm, kimi, deepseek, minimax — each has a non-fast sibling with the
// same weights), a per-family override demotes "fast" to ATTRIBUTE so a
// "…-fast" record folds onto the non-fast entity. For families where "fast"
// names a genuinely DIFFERENT model (grok-4-fast ≠ grok-4; Google's imagen/veo
// "fast" media variants), "fast" rides the global default and stays IDENTITY so
// the fast artifact remains a distinct entity. The global table is untouched:
// only family_overrides demote.
func TestVC16_FastPerFamilyDemotion(t *testing.T) {
	// DEMOTED arm: "fast" is an attribute and drops out of the identity key, so a
	// "…-fast" record folds onto its non-fast sibling entity.
	demoted := []bestiary.Family{"claude", "glm", "kimi", "deepseek", "minimax"}
	for _, fam := range demoted {
		if got := bestiary.ClassifyModifier("fast", fam); got != bestiary.ModifierClassAttribute {
			t.Errorf("ClassifyModifier(fast, %q) = %v, want ModifierClassAttribute", fam, got)
		}
		if got := bestiary.EntityModifiers([]string{"fast"}, fam); got != nil {
			t.Errorf("EntityModifiers([fast], %q) = %v, want nil (fast is an attribute → dropped from key)", fam, got)
		}
	}

	// Representative fold: a claude-opus-fast record keys to the SAME entity as the
	// non-fast claude/opus sibling (the speed tier does not split identity).
	fastOpus := bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5",
		Modifier: bestiary.EntityModifiers([]string{"fast"}, "claude")}
	plainOpus := bestiary.EntityRef{Family: "claude", Variant: "opus", Version: "4.5"}
	if fastOpus.String() != plainOpus.String() {
		t.Errorf("claude-opus-fast key %q must EQUAL the non-fast sibling %q (fast folds)",
			fastOpus.String(), plainOpus.String())
	}

	// glm already demotes "turbo"; "fast" is now demoted ALONGSIDE it — both drop.
	if got := bestiary.EntityModifiers([]string{"turbo", "fast"}, "glm"); got != nil {
		t.Errorf("EntityModifiers([turbo,fast], glm) = %v, want nil (both demoted for glm)", got)
	}

	// RETAINED arm: "fast" stays an IDENTITY token where it names a distinct model.
	// grok is the pinned representative (grok-4-fast ≠ grok-4).
	if got := bestiary.ClassifyModifier("fast", "grok"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(fast, grok) = %v, want ModifierClassIdentity (distinct model)", got)
	}
	if got := bestiary.EntityModifiers([]string{"fast"}, "grok"); modJoinCanon(got) != "fast" {
		t.Errorf("EntityModifiers([fast], grok) = %v, want [fast] (identity → retained)", got)
	}
	fastGrok := bestiary.EntityRef{Family: "grok", Version: "4",
		Modifier: bestiary.EntityModifiers([]string{"fast"}, "grok")}
	plainGrok := bestiary.EntityRef{Family: "grok", Version: "4"}
	if fastGrok.String() == plainGrok.String() {
		t.Errorf("grok-4-fast key %q must DIFFER from grok-4 %q (fast is identity for grok)",
			fastGrok.String(), plainGrok.String())
	}
	if want := "grok@4{fast}"; fastGrok.String() != want {
		t.Errorf("grok-4-fast key = %q, want %q", fastGrok.String(), want)
	}

	// The demotion is family-scoped: it must NOT leak to a retained family.
	if got := bestiary.ClassifyModifier("fast", "imagen"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(fast, imagen) = %v, want ModifierClassIdentity (override is per-family)", got)
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
	// RE-PINNED for the ReleaseStage migration: "latest" is no longer an attribute
	// modifier — it migrated to the stage axis and is routed out of BOTH modifier
	// segments (attributeModifiers drops it), so the render carries only "thinking".
	// The token stays in the Modifier field (for constant-name / resolve-filter
	// stability); it simply no longer renders in "[...]". The stage itself surfaces on
	// the separate "stage" axis (ModelInfo.Stage), which ModelRef does not render.
	multiAttr := bestiary.ModelRef{
		Provider: "anthropic", Family: "claude", Variant: "opus", Version: "4.6",
		Modifier: []string{"latest", "thinking"},
	}
	if got := multiAttr.Format(bestiary.SchemeCanonical); got != "anthropic/claude/opus/4.6[thinking]" {
		t.Errorf("multi-attribute render = %q, want %q (latest migrated to stage axis)", got, "anthropic/claude/opus/4.6[thinking]")
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

// TestTurboPerFamilyDemotion_KimiMinimax pins the curated turbo demotions for kimi
// and minimax, with the literal before/after entity keys each one moves.
//
// Turbo is IDENTITY by global default, and rightly so: gpt-4-turbo is a different
// artifact from gpt-4. It is demoted per-family only where curation established the
// token names a serving speed tier over the SAME artifact. Evidence differs in
// strength between the two families and the corpus records that honestly:
//
//   - kimi: moonshot serves kimi-k2-thinking and kimi-k2-thinking-turbo from the
//     IDENTICAL Kimi-K2-Thinking HuggingFace repo — same weights, so the turbo
//     spelling cannot denote a different artifact. Repo-identity evidence.
//   - minimax: no repo-identity proof. The rev-2 URL census resolves the M2.7 and
//     M2.5-highspeed serving names back to the plain repos, and minimax markets
//     turbo the way it markets highspeed (already an attribute). Inference, graded
//     lower — flagged in the curated entry so it is the first row to revisit.
//
// The three entities that merged are asserted by key, because a demotion that
// silently failed to merge them would leave the classification "correct" while the
// registry still carried the split.
func TestTurboPerFamilyDemotion_KimiMinimax(t *testing.T) {
	// Classification arm.
	for _, fam := range []bestiary.Family{"kimi", "minimax"} {
		if got := bestiary.ClassifyModifier("turbo", fam); got != bestiary.ModifierClassAttribute {
			t.Errorf("ClassifyModifier(turbo, %s) = %v, want ModifierClassAttribute", fam, got)
		}
		if got := bestiary.EntityModifiers([]string{"turbo"}, fam); got != nil {
			t.Errorf("EntityModifiers([turbo], %s) = %v, want nil — an attribute must not reach the key", fam, got)
		}
	}

	// Merge arm: the {turbo} keys are GONE and their instances sit on the plain
	// sibling, whose key never changed.
	for _, tc := range []struct{ gone, survivor string }{
		{"kimi/k@2{turbo}", "kimi/k@2"},
		{"kimi/k@2.6{turbo}", "kimi/k@2.6"},
		{"minimax/m@2.7{turbo}", "minimax/m@2.7"},
	} {
		// The retired key must be absent from the registry's own key set. EntityByKey is
		// deliberately NOT used for this: it applies the identity-class projection to its
		// argument, so it resolves the {turbo} spelling onto the survivor — useful for a
		// caller, useless as an absence check.
		for _, e := range bestiary.Entities() {
			if e.Ref.String() == tc.gone {
				t.Errorf("entity key %q is still minted; the turbo demotion must fold it into %q", tc.gone, tc.survivor)
			}
		}
		// The projection is what a caller sees: looking the old spelling up now lands on
		// the survivor rather than 404ing, so a consumer holding the pre-demotion key is
		// carried across instead of broken.
		if folded, ok := bestiary.EntityByKey(tc.gone); !ok || folded.Ref.String() != tc.survivor {
			t.Errorf("EntityByKey(%q) = (%q, ok=%v), want it to resolve onto %q",
				tc.gone, folded.Ref.String(), ok, tc.survivor)
		}
		e, ok := bestiary.EntityByKey(tc.survivor)
		if !ok {
			t.Fatalf("survivor entity %q is missing", tc.survivor)
		}
		// The turbo SPELLING is not lost — it stays an Admitted provider-ID nomen on
		// the merged entity. Demotion changes what is IDENTITY, never what is recorded.
		var sawTurboNomen bool
		for _, n := range e.Nomina() {
			if n.Scheme != bestiary.NomenSchemeProviderID || !strings.Contains(strings.ToLower(n.Value), "turbo") {
				continue
			}
			sawTurboNomen = true
			if n.Status != bestiary.AcceptabilityAdmitted {
				t.Errorf("turbo nomen %q on %q has status %v, want admitted", n.Value, tc.survivor, n.Status)
			}
		}
		if !sawTurboNomen {
			t.Errorf("entity %q carries no turbo-spelled provider-ID nomen; the merge must PRESERVE the "+
				"spelling as an admitted naming, not discard it", tc.survivor)
		}
	}

	// Scoping control: the demotion is per-family and must not leak. gpt keeps turbo
	// in its key, which is the whole reason the global default is identity.
	if got := bestiary.ClassifyModifier("turbo", "gpt"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(turbo, gpt) = %v, want ModifierClassIdentity", got)
	}
	if _, ok := bestiary.EntityByKey("gpt@4{turbo}"); !ok {
		t.Error("entity gpt@4{turbo} is missing — the kimi/minimax demotion must not leak to gpt")
	}
}
