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

// TestFamiliesJSON_MembersReachableInSnapshot is the SLICE-1 spec-item-1 guard
// (Reviewer B finding B-IMPORTANT-1): "Member lists validated against SLICE-0
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
// the thinking/vision member entries is explicitly deferred to SLICE-3 (modifier
// migration; Reviewer A finding ro2w-A2) — until then it is allowlisted here.
func TestFamiliesJSON_MembersReachableInSnapshot(t *testing.T) {
	// deferredUnreachableMembers["<family>"] = set of members allowed to be
	// unreachable in the current snapshot, each with a documented reason.
	// SLICE-3 removed the thinking/vision members (deepseek/kimi → thinking, grok →
	// vision) from families.json — they are now uniform Modifiers, not recoverable
	// variants — so the former grok/"vision" allowlist entry is obsolete and gone.
	deferredUnreachableMembers := map[string]map[string]string{
		// SLICE-11 (rc2): the ONLY snapshot model that decomposed to (qwen, "free") was
		// "qwen3.6-plus-free". SLICE-11's family-seed reduction makes the ID-path family
		// agree with raw "qwen-free", so reconcileIDDriven now adopts the more-specific
		// ID-driven capability tier "plus" over the access-tag "free" (the reviewed
		// justified-exception in path_unification_test.go), leaving "free" transiently
		// unreachable. The member is retained (not deleted) because the "qwen-free"
		// override still maps the raw family, and a future qwen "free"-tier ID with no
		// competing tier would re-ground it.
		"qwen": {"free": "SLICE-11: sole (qwen,free) model qwen3.6-plus-free now refines to variant 'plus' (justified exception)"},
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
				"  Why: SLICE-1 spec item 1 requires member lists be validated against the SLICE-0 snapshot;\n"+
				"       an unreachable member is most likely a typo or a speculative tier that does not exist\n"+
				"  How to fix: correct the member spelling, remove the speculative member from parse/data/families.json,\n"+
				"       OR (if intentionally deferred) add it to deferredUnreachableMembers with a documented reason",
				fam, member, fam, member)
		}
	}
}
