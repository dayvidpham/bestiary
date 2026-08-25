package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestEntityConstants_Unique verifies runtime properties of the generated Entity__*
// constants (the provider-agnostic successor to the removed Model__* surface).
//
// Uniqueness of constant NAMES is a compile-time Go guarantee (a duplicate const
// declaration is a compile error) AND a codegen-time guarantee (the injectivity guard
// fails the bake on any duplicate name). This test verifies the runtime shape:
//  1. EntityKeys() returns a non-empty slice (constants exist).
//  2. No empty-string values are present (each constant maps to a real entity key).
//
// Unlike the old Model__ surface, Entity__ VALUES are unique: one constant per entity,
// no provider flavoring. The value-uniqueness itself is pinned in
// TestEntityConstants_ValuesAreCanonicalKeys.
func TestEntityConstants_Unique(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Fatal("EntityKeys() returned an empty slice; entities_constants_gen.go may not have been generated")
	}

	for i, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys()[%d]: empty entity key (should never occur in generated constants)", i)
		}
	}

	// Exact census: the hard-cut bake emits one Entity__ constant per registry entity.
	// At this bake the registry holds 971 entities. This is an EXACT pin (not a floor):
	// a change to the entity count is a deliberate act that must move this literal in the
	// same commit, so a silent drift is caught.
	//
	// 975 → 971 when the curated cortecs pins landed: four phantom claude/opus@5…@8
	// entities (one cortecs instance each, created by a glued-token mis-parse) merged
	// into the real claude/opus@4.5…@4.8 entities they always belonged to.
	//
	// 971 → 982 when the family-"o" over-capture was corrected: vercel labels a swathe
	// of unrelated models raw_family "o", so alibaba's video models, openai's speech
	// models, quiverai's arrow and cohere's rerankers all shared one junk-bucket entity
	// with the real o-series. Splitting them into wan / tts / arrow / rerank ADDS
	// entities (15 new keys, 4 retired) because a bucket holding many distinct models
	// becomes many distinct entities.
	//
	// 982 → 979 with the curated kimi/minimax turbo demotions: turbo leaves the key for
	// those families, so kimi/k@2{turbo}, kimi/k@2.6{turbo} and minimax/m@2.7{turbo} fold
	// into their plain siblings. Three constants are REMOVED and none is renamed — the
	// surviving siblings' keys never changed.
	//
	// 979 → 977 with the "p"-as-dot version decode: fireworks publishes GLM 5.1/5.2 with
	// a "p" where the dot belongs, which minted phantom glm@5p1 / glm@5p2 entities beside
	// the real ones. Decoding the spelling merges those two rows into the existing
	// glm@5.1 and glm@5.2, so two constants are REMOVED and none is renamed — again the
	// surviving siblings' keys never changed.
	//
	// 977 → 978 with the tts-1-hd identity split: OpenAI documents tts-1-hd as a distinct
	// higher-quality product, so "hd" is now peeled as an IDENTITY modifier and tts@1{hd}
	// splits off from tts@1 (one constant ADDED, none renamed).
	//
	// 978 → 976 with the o-series dual-identity fix: the digitalocean openai-o1 / openai-o3
	// / openai-o3-mini rows (hyphen-glued vendor spelling) now canonicalize onto the
	// EXISTING gpt/o@1, gpt/o@3, gpt/o@3{mini} entities, vacating the two junk family-"o"
	// keys (o and o/mini). Two constants REMOVED, none renamed.
	//
	// 976 → 955 with the dot-lost version repair + 1t param-size routing: the dot-lost
	// exact-id overrides fold the dotless (minimax-m25, qwen35-…) and dash-glued
	// (qwen2-5-…, qwen3-6-…) spellings onto their real dotted entities (mostly merges, a
	// few re-keys), and 1t routing re-keys ling@1t/ring@1t to ling#1t/ring#1t and merges
	// ring-2.6-1t-free into ring@2.6#1t. Net −21 (measured; merges dominate).
	//
	// 955 → 947 with the entity-level MERGE-only N→N.0 fold (the C4 ruling): a family that
	// spells both a bare N and N.0 for the SAME (variant, size, modifiers) folds the bare
	// entity onto the dotted one — 8 pairs (claude/opus@4, claude/sonnet@4, gemini/flash@3,
	// gemini/pro@3, imagen@4, imagen@4{fast}, imagen/ultra@4, veo@3). A pure MERGE: 8 bare
	// keys retired, none renamed; llama@4 (no 4.0 sibling) is untouched.
	// 947 -> 958 with the 2026-07-23 snapshot refresh (upstream additions; no repair moved).
	// 958 -> 957 with the v0.2.8 curation slice: command/a{translate} splits out of the coarse
	// command/a key (+1, "translate" now a peeled identity modifier) and the two phantom
	// deepseek dash-glued entities deepseek@1 / deepseek@2 merge onto deepseek/v3.1 and
	// deepseek/v3.2-exp (−2). Net −1.
	const wantEntityCount = 945 // 957 -> 940: the global free demotion retires 17 keys (0 added); the demoted instances re-home onto their surviving siblings. 940 -> 939: the qwen3-coder-next-fp8-1m suppress-pin extended to the unprefixed spelling retires qwen/coder@3#1m (1 key, 0 added) — its '1m' was a 1M-context tier marker, not a parameter size, so the InferX instance rejoins qwen/coder@3. 939 -> 947 with the ling/inkling/kling collision split: the upstream catalog labels all 14 Inkling + klingai rows raw_family "ling", so both product lines were folded onto inclusionAI's Ling family. Splitting them retires bare `ling` (it held only the 6 mislabelled Inkling instances, so it empties) and the phantom `kling-v2@6`, and adds `inkling`, `kling@2.6` and the 8 `kling/v*` video keys: -2 +10 = +8. 947 -> 946 with the keyspace-wide mimo normalization: the mimo series letter leaves the entity key (its family record now declares series_letter_in_key false) and the four speech/tier tokens are curated as a per-family series-tier extension, so the ten mimo keys collapse to nine — mimo, mimo/flash, mimo/pro, mimo/v2.5-tts, mimo/v2.5-tts-voiceclone, mimo/v2.5-tts-voicedesign, mimo/v@2.5, mimo/v@2.5{pro}, mimo/v@2{omni}, mimo/v@2{pro} retire and mimo@2.5, mimo@2.5{pro}, mimo@2.5{tts}, mimo@2.5{tts,voiceclone}, mimo@2.5{tts,voicedesign}, mimo@2{flash}, mimo@2{omni}, mimo@2{pro}, mimo@2{tts} take their place: -10 +9 = -1. All 93 instances are conserved; nine of the ten retirements are pure renames and the tenth (mimo/pro, 1 instance) merges onto mimo@2.5{pro}. 946 -> 945 with the cogito variant pin: the dash-glued togetherai spelling deepcogito/cogito-v2-1-671b had minted a phantom one-instance "Cogito v1" line, and pinning it to the (variant "v", version "2.1") pair its dotted siblings carry merges it onto cogito/v@2.1#671b — 1 key retired, 0 added. (The decomposition commit that precedes it renames cogito/v2.1-671b#671b to cogito/v@2.1#671b and moves no count.)
	if len(keys) != wantEntityCount {
		t.Errorf("EntityKeys() returned %d constants; expected exactly %d — "+
			"re-run go generate ./... and update this census literal if the entity count changed intentionally",
			len(keys), wantEntityCount)
	}

	// Floor guard (defense-in-depth against a silent codegen collapse that also edits the
	// census literal): the count must never fall below a credible floor.
	const minExpected = 800
	if len(keys) < minExpected {
		t.Errorf("EntityKeys() returned only %d constants; expected at least %d — "+
			"re-run go generate ./... to regenerate entities_constants_gen.go", len(keys), minExpected)
	}
}

// TestEntityConstants_RoundTrip verifies that every Entity__* constant value is a real
// entity key: it appears among the entities the static registry exposes via Entities().
// This is the entity-level analog of the old Model__/LookupModel round-trip (the
// constant set is derived from the SAME registry index, so every value must round-trip).
func TestEntityConstants_RoundTrip(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	registryKeys := make(map[string]bool)
	for _, e := range bestiary.Entities() {
		registryKeys[e.Ref.String()] = true
	}

	for _, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys() contains empty value")
			continue
		}
		if !registryKeys[k] {
			t.Errorf("Entity constant value %q not found among registry entities (Entities())", k)
		}
	}
}

// TestEntityConstants_EntityByKeyRoundTrip is the migration fence: EVERY Entity__* value
// must round-trip through the exported string-keyed lookup EntityByKey — ok=true and the
// returned Entity's Ref.String() equal to the constant's value. This proves the
// enumerate-then-lookup idiom the CHANGELOG points migrating consumers at
// (`for _, key := range EntityKeys() { e, _ := EntityByKey(key) }`) actually works for all
// 975 constants, closing the gap where the values are canonical keys with no lookup entrypoint.
func TestEntityConstants_EntityByKeyRoundTrip(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}
	for _, k := range keys {
		e, ok := bestiary.EntityByKey(k)
		if !ok {
			t.Errorf("EntityByKey(%q) = ok=false; every Entity__ constant value must resolve", k)
			continue
		}
		if got := e.Ref.String(); got != k {
			t.Errorf("EntityByKey(%q).Ref.String() = %q; want the same canonical key (round-trip)", k, got)
		}
	}

	// Negative + composition: an unknown key returns ok=false (no panic), and EntityByKey's
	// Ref composes into ProvidersOf.
	if _, ok := bestiary.EntityByKey("definitely/not@a#real{key}"); ok {
		t.Error("EntityByKey on an unknown key returned ok=true; want false")
	}
	scout, ok := bestiary.EntityByKey(bestiary.Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct)
	if !ok {
		t.Fatal("EntityByKey did not resolve the scout constant")
	}
	if provs := bestiary.ProvidersOf(scout.Ref); len(provs) != 11 {
		t.Errorf("ProvidersOf(EntityByKey(scout).Ref) = %d providers; want 11", len(provs))
	}
}

// TestProvidersOf_ScoutCensus pins the ProvidersOf census for the scout entity and
// exercises a generated Entity__ constant end-to-end (the BDD case: the constant
// compiles, resolves to an entity, and ProvidersOf returns 11 distinct providers across
// its 13 instances — no provider-flavored entity constant remains).
func TestProvidersOf_ScoutCensus(t *testing.T) {
	// The generated constant compiles and carries the canonical key.
	const scoutKey = bestiary.Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct
	if scoutKey != "llama/scout@4#17b-16e{instruct}" {
		t.Fatalf("Entity__Llama__Scout__Version_4__Size_17b_16e__Instruct = %q; want %q", scoutKey, "llama/scout@4#17b-16e{instruct}")
	}

	ent, ok := bestiary.EntityByTuple("llama", "scout", "4", "17b-16e", "instruct")
	if !ok {
		t.Fatalf("EntityByTuple did not find the scout entity %q", scoutKey)
	}
	if len(ent.Instances) != 13 {
		t.Errorf("scout instances = %d; want 13", len(ent.Instances))
	}

	provs := bestiary.ProvidersOf(ent.Ref)
	if len(provs) != 11 {
		t.Errorf("ProvidersOf(scout) = %d providers; want 11 (across 13 instances)", len(provs))
	}
	// The result must be sorted ascending and de-duplicated.
	for i := 1; i < len(provs); i++ {
		if !(provs[i-1] < provs[i]) {
			t.Errorf("ProvidersOf(scout) not strictly ascending/deduped at index %d: %q then %q", i, provs[i-1], provs[i])
		}
	}
}

// TestEntityConstants_ValuesAreCanonicalKeys verifies that the constant VALUES are
// canonical entity-key strings (e.g. "llama/scout@4#17b-16e{instruct}"), NOT the Go
// identifier names — a value must never start with "Entity_". It also pins value
// uniqueness (one entity per constant). This is the successor to the old
// ValuesAreRawIDs check, renamed because the values are now canonical keys.
func TestEntityConstants_ValuesAreCanonicalKeys(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			t.Errorf("EntityKeys() contains empty value")
			continue
		}
		if strings.HasPrefix(k, "Entity_") {
			t.Errorf("EntityKeys(): value %q looks like a constant name, not an entity key; "+
				"EntityKeys() must return canonical key string values, not Go identifier strings", k)
		}
		if _, dup := seen[k]; dup {
			t.Errorf("EntityKeys(): duplicate value %q — Entity__ constants must be one-per-entity (no provider flavoring)", k)
		}
		seen[k] = struct{}{}
	}
}

// TestEntityKeys_DefensiveCopy verifies that the slice returned by EntityKeys() is a
// defensive copy: mutating it does not affect subsequent calls.
func TestEntityKeys_DefensiveCopy(t *testing.T) {
	k1 := bestiary.EntityKeys()
	if len(k1) == 0 {
		t.Skip("EntityKeys() returned empty; skipping — run go generate ./... first")
	}

	original := k1[0]
	k1[0] = "mutated"

	k2 := bestiary.EntityKeys()
	if len(k2) == 0 {
		t.Fatal("EntityKeys() returned empty on second call")
	}
	if k2[0] != original {
		t.Errorf("EntityKeys(): not a defensive copy; second call returned %q, want %q", k2[0], original)
	}
}

// TestEntityConstants_NoFreeIdentityModifier is the standing guard for the free-tier
// demotion: `free` names a pricing/serving tier a provider offers for an existing model,
// not a different weights artifact, so it is an ATTRIBUTE and must never appear as an
// identity modifier — the `{...}` segment of an entity key.
//
// It is derived from the generated key set rather than a literal list, so it keeps
// holding as the catalog grows. Without it the invariant was only ever a one-time manual
// grep, and any of three later changes could reintroduce a `{…free…}` key silently: a new
// upstream ID whose free tier is fused into a compound token, a `modifier_class.json` row
// that loses `global.free = "attribute"` and falls back through the unknown→IDENTITY
// fail-safe, or a regression in the tail scan that stops peeling the token.
//
// Three key values legitimately carry the token OUTSIDE the identity-modifier segment and
// are deliberately untouched by this guard: the standalone `free` and `cobuddy:free`
// entities (where it is the family token itself, not a modifier) and `ling/flash-free@2.6`
// (carved out of the demotion by an exact-ID pin). Matching on the raw key string would
// wrongly condemn all three, which is why the check parses the segment.
func TestEntityConstants_NoFreeIdentityModifier(t *testing.T) {
	keys := bestiary.EntityKeys()
	if len(keys) == 0 {
		t.Fatal("EntityKeys() returned an empty slice; the guard would be vacuous")
	}

	for _, key := range keys {
		for _, mod := range identityModifiersOf(key) {
			if !strings.Contains(mod, "free") {
				continue
			}
			t.Errorf("entity key %q carries the identity modifier %q; `free` is a pricing/serving "+
				"tier, not a distinct weights artifact, so it must render in the [attributes] "+
				"segment and never inside the key's {identity} segment. Check that "+
				"parse/data/modifier_class.json still classifies `free` as an attribute (an absent "+
				"row falls through the unknown->IDENTITY fail-safe) and that parse/data/modifiers.json "+
				"still lists it so the tail scan peels it.", key, mod)
		}
	}
}

// identityModifiersOf returns the comma-separated tokens of an entity key's trailing
// {identity-mods} segment, or nil when the key has none. The segment is always last in
// EntityRef.String(), so it is read off the end of the key.
func identityModifiersOf(key string) []string {
	if !strings.HasSuffix(key, "}") {
		return nil
	}
	open := strings.LastIndex(key, "{")
	if open < 0 {
		return nil
	}
	inner := key[open+1 : len(key)-1]
	if inner == "" {
		return nil
	}
	return strings.Split(inner, ",")
}
