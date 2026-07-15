package bestiary_test

import (
	"strings"
	"testing"

	bestiary "github.com/dayvidpham/bestiary"
)

// modHas reports whether a modifier slice contains tok.
func modHas(mods []string, tok string) bool {
	for _, m := range mods {
		if m == tok {
			return true
		}
	}
	return false
}

// TestStageMigration_SyntheticGrokMiniFastBeta pins the ratified SYNTHETIC exemplar
// grok-3-mini-fast-beta (NOT a catalog ID — a grammar illustration). The stage-axis
// vocabulary rules it demonstrates:
//
//   - Stage = StageBeta (DetectStageFromID finds the standalone "beta" token).
//   - Variant = mini, Version = 3 (unchanged decomposition).
//   - "beta" is NEVER placed in the Modifier list (detect-without-strip: it is
//     detected on the stage axis, and its position in the raw decomposition is left
//     exactly as the pre-S6 pipeline had it — this epoch never consumes/strips beta).
//   - "fast" is IDENTITY-class for grok — NOT demoted to an attribute (R7b survey is
//     report-only; no fast/turbo reclassification this epoch).
//
// NUANCE (documented, not a regression): in this particular synthetic ID the
// mechanical tail scan halts at the non-modifier "beta" boundary token, so "fast" is
// left as decomposition residue rather than captured into the Modifier list. That
// residue disposition is PRE-EXISTING and deliberately UNCHANGED by S6 — making beta
// transparent to the scan (so it reached "fast") would alter the frozen decomposition
// of the real catalog beta IDs, which the contract forbids. The identity CLASSIFICATION
// of "fast" for grok is what is pinned here, exercised directly via ClassifyModifier.
func TestStageMigration_SyntheticGrokMiniFastBeta(t *testing.T) {
	const id = "grok-3-mini-fast-beta" // SYNTHETIC — not present in the catalog.

	fam, variant, version, mods, _ := bestiary.ParseFamilyDetailed("", id, bestiary.ProviderxAI)
	if fam != "grok" {
		t.Errorf("family = %q, want grok", fam)
	}
	if variant != "mini" {
		t.Errorf("variant = %q, want mini (unchanged decomposition)", variant)
	}
	if version != "3" {
		t.Errorf("version = %q, want 3", version)
	}
	if modHas(mods, "beta") {
		t.Errorf("Modifier %v must NOT contain beta (detect-without-strip: beta is a stage, never a modifier)", mods)
	}

	stage, raw := bestiary.DetectStageFromID(id)
	if stage != bestiary.StageBeta {
		t.Errorf("Stage = %v, want StageBeta", stage)
	}
	if raw != "" {
		t.Errorf("StageRaw = %q, want \"\"", raw)
	}

	// fast is an IDENTITY modifier for grok (not demoted): the classification rule the
	// exemplar illustrates. grok has no per-family fast override, so it rides the
	// global identity default.
	if got := bestiary.ClassifyModifier("fast", "grok"); got != bestiary.ModifierClassIdentity {
		t.Errorf("ClassifyModifier(fast, grok) = %v, want ModifierClassIdentity (fast not demoted for grok)", got)
	}
}

// TestStageMigration_BetaKeyFrozen pins the exact catalog exemplar
// grok-4.20-beta-0309-reasoning: Stage=StageBeta is SET while the entity key is
// UNCHANGED — "beta" stays in the Variant (it is entity-key material for grok today),
// so the model still keys to grok/beta@4.20{reasoning}. Re-keying beta out of the
// variant is deferred to a future ruling; this epoch freezes the key.
func TestStageMigration_BetaKeyFrozen(t *testing.T) {
	const id = "grok-4.20-beta-0309-reasoning" // exact catalog ID.

	stage, _ := bestiary.DetectStageFromID(id)
	if stage != bestiary.StageBeta {
		t.Errorf("Stage = %v, want StageBeta", stage)
	}

	// The entity is keyed with beta in the Variant slot (frozen), reachable by tuple.
	ent, ok := bestiary.EntityByTuple("grok", "beta", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok/beta@4.20{reasoning} entity missing — beta must stay in the Variant (key frozen)")
	}
	if got := ent.Ref.String(); got != "grok/beta@4.20{reasoning}" {
		t.Errorf("entity key = %q, want grok/beta@4.20{reasoning} (unchanged by S6)", got)
	}
	if !entityHoldsInstanceContaining(ent, "grok-4.20-beta-0309-reasoning") {
		t.Errorf("grok/beta@4.20{reasoning} missing its beta instance; instances=%v", instIDs(ent))
	}
}

// TestStageMigration_MigratedTokensRetainedButRoutedOut verifies the migration
// mechanics for preview/latest/original: the token STAYS in the Modifier data field
// (so the entity-key constant name and the resolve [attr] filter are byte-stable — no
// codegen churn), while it is routed OUT of BOTH modifier render segments (it renders
// on the separate stage axis) and out of the entity key.
func TestStageMigration_MigratedTokensRetainedButRoutedOut(t *testing.T) {
	// chatgpt-4o-latest: latest retained in Modifier, Stage=latest, key excludes it.
	_, _, _, chatMods, _ := bestiary.ParseFamilyDetailed("", "chatgpt-4o-latest", bestiary.ProviderOpenAI)
	if !modHas(chatMods, "latest") {
		t.Errorf("Modifier %v should RETAIN latest (field kept for constant/resolve stability)", chatMods)
	}
	if stage, _ := bestiary.DetectStageFromID("chatgpt-4o-latest"); stage != bestiary.StageLatest {
		t.Errorf("Stage = %v, want StageLatest", stage)
	}
	// latest is routed out of the identity key (was attribute-class → excluded; now
	// stage-routed → still excluded): zero key change.
	if got := bestiary.EntityModifiers([]string{"latest"}, "chatgpt"); got != nil {
		t.Errorf("EntityModifiers([latest], chatgpt) = %v, want nil (stage-routed out of the key)", got)
	}

	// Canonical render drops the migrated token from "[...]": it belongs to the stage axis.
	ref := bestiary.ModelRef{Provider: "openai", Family: "gpt", Variant: "4o", Modifier: []string{"latest"}}
	if got := ref.Format(bestiary.SchemeCanonical); strings.Contains(got, "latest") {
		t.Errorf("render %q must not contain latest (migrated to the stage axis)", got)
	}

	// kimi ...-thinking-turbo-original: original retained in Modifier, Stage=original,
	// key keeps only the true identity modifier (turbo).
	const kimiID = "moonshotai/kimi-k2-thinking-turbo-original"
	kfam, _, _, kmods, _ := bestiary.ParseFamilyDetailed("", kimiID, "moonshotai")
	if !modHas(kmods, "original") {
		t.Errorf("Modifier %v should RETAIN original", kmods)
	}
	if stage, _ := bestiary.DetectStageFromID(kimiID); stage != bestiary.StageOriginal {
		t.Errorf("Stage = %v, want StageOriginal", stage)
	}
	keyMods := bestiary.EntityModifiers(kmods, kfam)
	if modHas(keyMods, "original") {
		t.Errorf("EntityModifiers(%v, %s) = %v must exclude original (stage-routed)", kmods, kfam, keyMods)
	}
	if !modHas(keyMods, "turbo") {
		t.Errorf("EntityModifiers(%v, %s) = %v must keep turbo (identity for kimi)", kmods, kfam, keyMods)
	}
}
