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

// TestGrokBetaUnification_KeyMerged pins the exact catalog exemplar
// grok-4.20-beta-0309-reasoning after the curated grok beta-alias unification:
// Stage=StageBeta is still SET (detect-without-strip is independent of the key), but
// the exact-ID override now maps the beta spelling onto the NON-beta decomposition, so
// it keys grok@4.20{reasoning} — the SAME entity as the official grok-4.20-0309-reasoning
// name. The old split grok/beta@4.20{reasoning} entity must NO LONGER exist. This is a
// grok-only unification; the general beta freeze stays for non-grok names.
func TestGrokBetaUnification_KeyMerged(t *testing.T) {
	const id = "grok-4.20-beta-0309-reasoning" // exact catalog ID.

	// Stage is still detected from the ID, independent of the (now-unified) key.
	stage, _ := bestiary.DetectStageFromID(id)
	if stage != bestiary.StageBeta {
		t.Errorf("Stage = %v, want StageBeta (detection is independent of the key)", stage)
	}

	// The beta spelling now keys the unified non-beta entity, reachable by tuple.
	ent, ok := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok@4.20{reasoning} entity missing — the beta spelling must unify onto the non-beta key")
	}
	if got := ent.Ref.String(); got != "grok@4.20{reasoning}" {
		t.Errorf("entity key = %q, want grok@4.20{reasoning} (beta unified out of the Variant)", got)
	}
	if !entityHoldsInstanceContaining(ent, "grok-4.20-beta-0309-reasoning") {
		t.Errorf("grok@4.20{reasoning} missing its beta instance; instances=%v", instIDs(ent))
	}

	// The old split entity must be gone: no grok/beta@4.20 spelling survives the unification.
	if _, gone := bestiary.EntityByTuple("grok", "beta", "4.20", "", "reasoning"); gone {
		t.Error("grok/beta@4.20{reasoning} still present — the beta-alias unification must remove the split entity")
	}
}

// betaTokenInName reports whether the name part of a model ID (after any path
// prefix) carries a STANDALONE "beta" token. It is the census test's OWN selector —
// deliberately independent of DetectStageFromID, so the census is not a tautology of
// the production scanner asserting against itself: the selector picks the rows by raw
// tokenization, the assertion checks the BAKED Stage field on the static row.
func betaTokenInName(id string) bool {
	s := strings.ToLower(id)
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		s = s[idx+1:]
	}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if tok == "beta" {
			return true
		}
	}
	return false
}

// TestStageBeta_CensusDerived is the automated census guard for the
// detect-without-strip beta set (the dual-leg llama-4 census precedent applied to the
// stage axis). The row set is SELF-DERIVED from StaticModels(), never hand-enumerated
// — a hand glob is exactly what the census rule exists to prevent (a new beta ID
// added by a catalog refresh must be caught without a curator editing a test):
//
//   - forward leg: every catalog row whose ID carries a standalone beta token (per
//     the test's own independent tokenizer) must bake Stage == StageBeta;
//   - inverse leg: every baked StageBeta row must carry a standalone beta token (no
//     beta stage can appear without an ID marker while the ID path is the sole feeder);
//   - key-direction leg (self-deriving over the same census): NO beta row may key with
//     beta in the Variant slot, whatever its family. beta is ALWAYS a release stage and
//     never an identity, so a beta Variant means a curated pin is missing or was dropped
//     — for grok, one of the beta-alias unification entries; for any other family, the
//     bare-identity pin the interfaze row established. This leg previously CONTRASTED the
//     two families (grok unified, non-grok frozen with beta as key material); the ruling
//     that beta is always a stage collapsed the contrast into one universal rule, and the
//     ValidateNoBetaInIdentity codegen guard now enforces the same invariant at bake time;
//   - vacuity guard: the census must find at least 9 distinct beta IDs (the count at
//     the time this guard was cut — the grok-4.20 spellings + interfaze-beta), so a
//     catalog refresh that silently empties the census fails loudly.
func TestStageBeta_CensusDerived(t *testing.T) {
	distinct := map[string]bool{}
	for _, m := range bestiary.StaticModels() {
		hasBetaTok := betaTokenInName(string(m.ID))
		if hasBetaTok {
			distinct[strings.ToLower(string(m.ID))] = true
			if m.Stage != bestiary.StageBeta {
				t.Errorf("catalog row %q (provider %q) carries a standalone beta token but bakes Stage=%v, want StageBeta\n"+
					"  Why: the beta row set is census-derived — every beta-token row must carry the stage",
					m.ID, m.Provider, m.Stage)
			}
			if strings.EqualFold(m.Variant, "beta") {
				t.Errorf("beta row %q (provider %q, family %q) keys with Variant=\"beta\" — beta is a release STAGE, never an identity\n"+
					"  Why: the stage axis already carries beta for this row (detect-without-strip), so a beta Variant asserts it twice and splits one artifact line in two\n"+
					"  How to fix: add/restore the ID's idFamilyOverrides entry mapping it to its bare identity (grok: the beta-alias unification entries; others: the interfaze/interfaze-beta precedent)",
					m.ID, m.Provider, m.Family)
			}
		}
		if m.Stage == bestiary.StageBeta && !hasBetaTok {
			t.Errorf("catalog row %q (provider %q) bakes StageBeta but its ID carries no standalone beta token\n"+
				"  Why: the ID scan is the only stage feeder this epoch — a beta stage without an ID marker is a bake bug",
				m.ID, m.Provider)
		}
	}
	if len(distinct) < 9 {
		t.Fatalf("beta census found only %d distinct beta IDs, want >= 9 — the census went vacuous (a catalog refresh dropped the beta rows, or the selector regressed); IDs: %v",
			len(distinct), distinct)
	}
	t.Logf("beta census: %d distinct beta IDs, all baked StageBeta and none keying beta into its identity", len(distinct))
}

// TestStageMigration_NoStageTokenInAnyEntityKey is the catalog-wide permanent fence
// for the migration invariant: NO migrated stage token (preview/latest/original) may
// appear in ANY baked entity key's {mods} segment. VC6 pins the realistic mutation
// (the classify/route seam); this sweep covers every entity the registry actually
// builds, so any FUTURE alternate path into the identity projection that bypasses the
// stage routing is caught against real data, not just the unit seam.
func TestStageMigration_NoStageTokenInAnyEntityKey(t *testing.T) {
	migrated := map[string]bool{"preview": true, "latest": true, "original": true}
	entities := bestiary.Entities()
	if len(entities) == 0 {
		t.Fatal("Entities() returned no entities — the sweep is vacuous")
	}
	for _, e := range entities {
		for _, mod := range e.Ref.Modifier {
			if migrated[strings.ToLower(mod)] {
				t.Errorf("entity key %q carries migrated stage token %q in its {mods} segment\n"+
					"  Why: preview/latest/original migrated to the Stage axis and must never be identity-key material\n"+
					"  Where: the identity projection (EntityModifiers) must route stage tokens out before the fail-safe",
					e.Ref.String(), mod)
			}
		}
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
	// turbo is EXCLUDED for kimi as well, by the curated per-family demotion — but for
	// a different reason than original, and the distinction is the point of asserting
	// both: original is routed out because it is a release STAGE, turbo because it is
	// a serving speed tier rather than a distinct artifact. This assertion previously
	// required turbo to be KEPT; it was re-cut when the demotion landed.
	if modHas(keyMods, "turbo") {
		t.Errorf("EntityModifiers(%v, %s) = %v must exclude turbo (attribute for kimi by curated demotion)", kmods, kfam, keyMods)
	}
	// The retained-token guarantee still holds: the instance-level Modifier keeps both.
	if !modHas(kmods, "turbo") {
		t.Errorf("Modifier %v should RETAIN turbo at instance level even though the key drops it", kmods)
	}
}
