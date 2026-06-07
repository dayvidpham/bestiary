package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// familiesJSONPath locates the committed parse/data/families.json relative to this
// test file (robust to the test's working directory, mirroring snapshotDir()).
func familiesJSONPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("..", "..", "parse", "data", "families.json")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "parse", "data", "families.json")
}

// loadFamiliesJSONMembers reads families.json and returns family → member list,
// skipping the top-level "_comment" string entry.
func loadFamiliesJSONMembers(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(familiesJSONPath())
	if err != nil {
		t.Fatalf("read families.json at %s: %v", familiesJSONPath(), err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("decode families.json: %v", err)
	}
	out := make(map[string][]string, len(top))
	for fam, rawVal := range top {
		if strings.HasPrefix(fam, "_") {
			continue // skip "_comment" and any future metadata keys
		}
		var fi struct {
			Members []string `json:"members"`
		}
		if err := json.Unmarshal(rawVal, &fi); err != nil {
			t.Fatalf("decode families.json entry %q: %v", fam, err)
		}
		out[fam] = fi.Members
	}
	return out
}

// TestFamiliesJSON_MembersReachableInSnapshot is the spec-item-1 guard:
// "Member lists validated against
// snapshot." It asserts that for every (family, member) declared in families.json,
// at least ONE real model ID in the committed snapshot decomposes — via the live
// ParseFamilyDetailed pipeline — to that (family, member-as-variant) pair.
//
// Without this guard, a typo'd or speculative member (e.g. "claude/sonnnet", or a
// non-existent tier) silently no-ops with no test failure. That exact gap let the
// seed regression through in round 1 (recoverMemberVariant returning "" for an
// unregistered family went unnoticed because nothing pinned member reachability).
//
// Known-unreachable members are listed in deferredUnreachableMembers with a reason.
// The ONLY current entry is grok/"vision": "vision" is a MODIFIER (ExtractModifier
// strips it before member recovery), so it can never surface as a variant. Removing
// the thinking/vision member entries is explicitly deferred to the modifier
// migration — until then it is allowlisted here.
func TestFamiliesJSON_MembersReachableInSnapshot(t *testing.T) {
	// deferredUnreachableMembers["<family>"] = set of members allowed to be
	// unreachable in the current snapshot, each with a documented reason.
	// removed the thinking/vision members (deepseek/kimi → thinking, grok →
	// vision) from families.json — they are now uniform Modifiers, not recoverable
	// variants — so the former grok/"vision" allowlist entry is obsolete and gone.
	deferredUnreachableMembers := map[string]map[string]string{
		// the ONLY snapshot model that decomposed to (qwen, "free") was
		// "qwen3.6-plus-free". this family-seed reduction makes the ID-path family
		// agree with raw "qwen-free", so reconcileIDDriven now adopts the more-specific
		// ID-driven capability tier "plus" over the access-tag "free" (the reviewed
		// justified-exception in path_unification_test.go), leaving "free" transiently
		// unreachable. The member is retained (not deleted) because the "qwen-free"
		// override still maps the raw family, and a future qwen "free"-tier ID with no
		// competing tier would re-ground it.
		"qwen": {"free": "sole (qwen,free) model qwen3.6-plus-free now refines to variant 'plus' (justified exception)"},
		// the 'o' family is FOLDED into gpt as a variant —
		// o1/o3/o4[-mini/-pro] now decompose to (gpt, variant='o', version=N, modifier=mini/
		// pro). The "o" families.json entry is RETAINED (its bare_gen_split:true still splits
		// the transient "o1"→o+1 before canonicalizeOpenAILine relabels family→gpt, and its
		// members [mini,pro] are still recovered transiently so the size token survives as the
		// Modifier). But NO FINAL decomposition is (o, mini) / (o, pro) anymore — the members
		// are reachable only mid-pipeline, so they read as unreachable here. Justified.
		"o": {
			"mini": "o-series folded into gpt; 'mini' now surfaces as the Modifier under family=gpt, recovered only transiently under 'o'",
			"pro":  "o-series folded into gpt; 'pro' now surfaces as the Modifier under family=gpt, recovered only transiently under 'o'",
		},
	}

	members := loadFamiliesJSONMembers(t)

	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("LoadSnapshotRecords: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("snapshot is empty — no records loaded")
	}

	// Count, per (lowercase-family|member), how many snapshot IDs decompose to it.
	reach := make(map[string]int)
	for _, r := range records {
		fam, variant, _, _, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		if variant == "" {
			continue
		}
		reach[strings.ToLower(string(fam))+"|"+variant]++
	}

	// Deterministic iteration for stable failure output.
	fams := make([]string, 0, len(members))
	for fam := range members {
		fams = append(fams, fam)
	}
	sort.Strings(fams)

	for _, fam := range fams {
		for _, member := range members[fam] {
			if reach[fam+"|"+member] > 0 {
				continue
			}
			if reason, ok := deferredUnreachableMembers[fam][member]; ok {
				t.Logf("families.json %q/%q: 0 snapshot IDs decompose to this member — ALLOWED (%s)", fam, member, reason)
				continue
			}
			t.Errorf("families.json member %q/%q is UNREACHABLE: no snapshot model ID decomposes to (family=%q, variant=%q)\n"+
				"  What: a families.json member is not grounded in any real model ID in the committed snapshot\n"+
				"  Why: the spec requires member lists be validated against the snapshot;\n"+
				"       an unreachable member is most likely a typo or a speculative tier that does not exist\n"+
				"  How to fix: correct the member spelling, remove the speculative member from parse/data/families.json,\n"+
				"       OR (if intentionally deferred) add it to deferredUnreachableMembers with a documented reason",
				fam, member, fam, member)
		}
	}
}

// familyEnforcePath locates the committed parse/data/family_enforce.json relative to this
// test file (mirrors familiesJSONPath).
func familyEnforcePath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("..", "..", "parse", "data", "family_enforce.json")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "parse", "data", "family_enforce.json")
}

// loadFamilyEnforce reads parse/data/family_enforce.json and returns the families list.
func loadFamilyEnforce(t *testing.T) []string {
	t.Helper()
	body, err := os.ReadFile(familyEnforcePath())
	if err != nil {
		t.Fatalf("loadFamilyEnforce: %v", err)
	}
	var f struct {
		Families []string `json:"families"`
	}
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatalf("loadFamilyEnforce: parse: %v", err)
	}
	return f.Families
}

// TestFamilyEnforce_IntegrityGuard is the integrity guard for the
// canonical-winner ENFORCE set. It asserts that EVERY family_enforce.json entry decomposes
// from >=1 REAL model in the committed snapshot — the analogue of the o-series allowlist's
// ConformsToRatification, and of the families.json member-reachability guard. The set CANNOT
// be validated against allFamilies: 11/18 entries (aion, codellama, inflection, lfm, magnum,
// olmo, owl, qwq, remm, weaver, wizardlm) are DELIBERATELY absent from the upstream registry —
// they are distinct models a provider mislabels as a parent, which is the whole point of the
// enforce set. So the guard grounds each entry in real snapshot data instead.
func TestFamilyEnforce_IntegrityGuard(t *testing.T) {
	enforce := loadFamilyEnforce(t)
	if len(enforce) == 0 {
		t.Fatal("family_enforce.json has no entries")
	}
	records, err := LoadSnapshotRecords()
	if err != nil {
		t.Fatalf("LoadSnapshotRecords: %v", err)
	}
	famCount := make(map[string]int)
	for _, r := range records {
		fam, _, _, _, _ := bestiary.ParseFamilyDetailed(r.RawFamily, r.ID, r.Provider)
		famCount[strings.ToLower(string(fam))]++
	}
	for _, f := range enforce {
		lf := strings.ToLower(f)
		// Each enforce entry must be in the closed set (round-trips through the loader)…
		if !bestiary.IsEnforcedCanonicalFamily(bestiary.Family(f)) {
			t.Errorf("family_enforce.json entry %q is not recognised by IsEnforcedCanonicalFamily", f)
		}
		// …and must ground in >=1 real model (else it is dead/speculative and must be removed).
		if famCount[lf] == 0 {
			t.Errorf("family_enforce.json entry %q maps to ZERO snapshot models — dead/speculative entry\n"+
				"  What: a canonical-winner enforce family is not produced by any committed snapshot record\n"+
				"  Why: the enforce set must be grounded in real data (it cannot be validated vs allFamilies,\n"+
				"       since distinct-model entries are intentionally absent from the upstream registry)\n"+
				"  How to fix: remove the entry from parse/data/family_enforce.json, or correct its spelling",
				f)
		}
	}
	t.Logf("family_enforce integrity: %d entries, all grounded in >=1 snapshot model", len(enforce))
}

// TestIsEnforcedCanonicalFamily_Unit is the direct true/false unit
// for the exported predicate (previously exercised only indirectly via the diff gate).
func TestIsEnforcedCanonicalFamily_Unit(t *testing.T) {
	for _, f := range []string{"aion", "hermes", "mixtral", "qwq", "intellect", "AION", "Hermes"} {
		if !bestiary.IsEnforcedCanonicalFamily(bestiary.Family(f)) {
			t.Errorf("IsEnforcedCanonicalFamily(%q) = false, want true (case-insensitive enforce member)", f)
		}
	}
	for _, f := range []string{"gpt", "llama", "claude", "mistral", "qwen", "", "deepseek"} {
		if bestiary.IsEnforcedCanonicalFamily(bestiary.Family(f)) {
			t.Errorf("IsEnforcedCanonicalFamily(%q) = true, want false (parent/registered family, not enforced)", f)
		}
	}
}
