package bestiary_test

import (
	"strings"
	"testing"

	bestiary "github.com/dayvidpham/bestiary"
)

// instIDs returns the instance ID strings of an entity, for readable failures.
func instIDs(e bestiary.Entity) []string {
	out := make([]string, 0, len(e.Instances))
	for _, in := range e.Instances {
		out = append(out, string(in.ID))
	}
	return out
}

// entityHoldsInstanceContaining reports whether any instance ID of e contains sub.
func entityHoldsInstanceContaining(e bestiary.Entity, sub string) bool {
	for _, in := range e.Instances {
		if strings.Contains(strings.ToLower(string(in.ID)), sub) {
			return true
		}
	}
	return false
}

func modContains(mods []string, want string) bool {
	for _, m := range mods {
		if m == want {
			return true
		}
	}
	return false
}

// TestStageMode_OmniIdentitySplit_Gemini pins the ratified omni IDENTITY split:
// gemini-omni-flash-preview keys to gemini/flash{omni}, a DISTINCT
// entity from the bare gemini/flash that holds gemini-flash-latest. The instances must
// partition — the omni entity holds the omni model, the base entity must NOT.
func TestStageMode_OmniIdentitySplit_Gemini(t *testing.T) {
	base, okBase := bestiary.EntityByTuple(bestiary.FamilyGemini, "flash", "", "")
	omni, okOmni := bestiary.EntityByTuple(bestiary.FamilyGemini, "flash", "", "", "omni")
	if !okBase {
		t.Fatal("gemini/flash entity missing")
	}
	if !okOmni {
		t.Fatal("gemini/flash{omni} entity missing — the omni IDENTITY split collapsed")
	}
	if base.Ref.String() == omni.Ref.String() {
		t.Fatalf("gemini/flash and gemini/flash{omni} are the same key %q — no split", omni.Ref.String())
	}
	if omni.Ref.String() != "gemini/flash{omni}" {
		t.Errorf("omni entity key = %q, want gemini/flash{omni}", omni.Ref.String())
	}
	if !entityHoldsInstanceContaining(omni, "gemini-omni-flash-preview") {
		t.Errorf("gemini/flash{omni} missing its omni-flash-preview instance; instances=%v", instIDs(omni))
	}
	if entityHoldsInstanceContaining(base, "omni") {
		t.Errorf("base gemini/flash wrongly holds an omni instance: %v", instIDs(base))
	}
	if !entityHoldsInstanceContaining(base, "gemini-flash-latest") {
		t.Errorf("base gemini/flash missing gemini-flash-latest: %v", instIDs(base))
	}
	// The metadata for the omni lab model attaches to the omni entity.
	if omni.Metadata == nil || omni.Metadata.MetadataID != "google/gemini-omni-flash-preview" {
		t.Errorf("gemini/flash{omni} metadata = %v, want google/gemini-omni-flash-preview", omni.Metadata)
	}
}

// TestStageMode_RealtimeAttribute_VersionRestored pins the ratified realtime ATTRIBUTE
// treatment: gpt-realtime-2.1 keys to gpt@2.1 — the version is RESTORED (realtime no
// longer swallows it) and realtime is NOT in the entity key. The realtime token rides
// on the instance-level ModelInfo.Modifier (attribute class), not the identity.
func TestStageMode_RealtimeAttribute_VersionRestored(t *testing.T) {
	e, ok := bestiary.EntityByTuple(bestiary.FamilyGPT, "", "2.1", "")
	if !ok {
		t.Fatal("gpt@2.1 entity missing — realtime swallowed the version, or realtime leaked into the key")
	}
	if !entityHoldsInstanceContaining(e, "gpt-realtime-2.1") {
		t.Errorf("gpt@2.1 missing the gpt-realtime-2.1 instance: %v", instIDs(e))
	}
	m, ok := bestiary.LookupModelByProvider(bestiary.Provider("openai"), "gpt-realtime-2.1")
	if !ok {
		t.Fatal("openai/gpt-realtime-2.1 model not found")
	}
	if m.Version != "2.1" {
		t.Errorf("gpt-realtime-2.1 Version = %q, want 2.1 (RESTORED — realtime must not swallow it)", m.Version)
	}
	if m.Family != bestiary.FamilyGPT || m.Variant != "" {
		t.Errorf("gpt-realtime-2.1 = (family %q, variant %q), want (gpt, \"\")", m.Family, m.Variant)
	}
	if !modContains(m.Modifier, "realtime") {
		t.Errorf("gpt-realtime-2.1 Modifier = %v, want it to carry realtime (attribute-class, instance-level)", m.Modifier)
	}
}

// TestStageMode_LagunaThreeWaySplit pins the ratified laguna curated-variant split:
// laguna-xs.2, laguna-xs-2.1, and laguna-m.1 key to THREE distinct
// entities (laguna/xs@2, laguna/xs@2.1, laguna/m@1), and each lab metadata row attaches
// to its own entity — fixing the pre-existing silent m.1 <-> xs.2 metadata collision.
func TestStageMode_LagunaThreeWaySplit(t *testing.T) {
	cases := []struct {
		variant, version, wantKey, metaID string
	}{
		{"xs", "2", "laguna/xs@2", "poolside/laguna-xs.2"},
		{"xs", "2.1", "laguna/xs@2.1", "poolside/laguna-xs-2.1"},
		{"m", "1", "laguna/m@1", "poolside/laguna-m.1"},
	}
	distinct := map[string]bool{}
	for _, c := range cases {
		e, ok := bestiary.EntityByTuple(bestiary.FamilyLaguna, c.variant, c.version, "")
		if !ok {
			t.Fatalf("entity %s missing — the laguna variant split did not land", c.wantKey)
		}
		if e.Ref.String() != c.wantKey {
			t.Errorf("laguna entity key = %q, want %q", e.Ref.String(), c.wantKey)
		}
		distinct[e.Ref.String()] = true
		if e.Metadata == nil || e.Metadata.MetadataID != bestiary.MetadataID(c.metaID) {
			t.Errorf("%s metadata = %v, want %s (collision-free attach)", c.wantKey, e.Metadata, c.metaID)
		}
	}
	if len(distinct) != 3 {
		t.Errorf("expected 3 distinct laguna entities, got %d: %v", len(distinct), distinct)
	}
}

// TestStageMode_LivetranslateIdentity pins livetranslate as an IDENTITY modifier:
// qwen3-livetranslate-flash-realtime keys to qwen/flash@3{livetranslate} (livetranslate
// in the identity, realtime an instance-level attribute — not in the key).
func TestStageMode_LivetranslateIdentity(t *testing.T) {
	e, ok := bestiary.EntityByTuple(bestiary.FamilyQwen, "flash", "3", "", "livetranslate")
	if !ok {
		t.Fatal("qwen/flash@3{livetranslate} entity missing — livetranslate identity did not land")
	}
	if !entityHoldsInstanceContaining(e, "qwen3-livetranslate-flash-realtime") {
		t.Errorf("qwen/flash@3{livetranslate} missing its instance: %v", instIDs(e))
	}
	m, ok := bestiary.LookupModel("qwen3-livetranslate-flash-realtime")
	if !ok {
		t.Fatal("qwen3-livetranslate-flash-realtime model not found")
	}
	if !modContains(m.Modifier, "livetranslate") || !modContains(m.Modifier, "realtime") {
		t.Errorf("qwen3-livetranslate-flash-realtime Modifier = %v, want both livetranslate + realtime", m.Modifier)
	}
}
