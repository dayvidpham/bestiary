package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// metadata_join_test.go exercises the EXPORTED metadata<->entity join surface
// (MergeEntityMetadata, JoinEntityMetadata, AttachEntityMetadata) through the same
// code path the CLI overlay and codegen use. The shipped modelsdev_aliases.json is
// empty, so these tests observe the MECHANICAL join; alias-precedence and
// file-degradation are covered by the internal test file.

// entityWithRef builds a minimal registry-shaped Entity carrying only the identity
// tuple under test — enough for the join, which keys purely on EntityRef.String().
func entityWithRef(family, variant, version, paramSize string, mods ...string) bestiary.Entity {
	return bestiary.Entity{
		Ref: bestiary.EntityRef{
			Family:    bestiary.Family(family),
			Variant:   variant,
			Version:   version,
			ParamSize: paramSize,
			Modifier:  mods,
		},
	}
}

// --------------------------------------------------------------------------
// MergeEntityMetadata
// --------------------------------------------------------------------------

// TestMergeEntityMetadata_Union pins B-M4: a baked-only metadata row (present only in
// the static set, never re-synced) SURVIVES a merge with a disjoint cached set, and a
// cached-only row is added — union semantics, mirroring MergeModels.
func TestMergeEntityMetadata_Union(t *testing.T) {
	static := []bestiary.EntityMetadata{
		{MetadataID: "lab/baked-only", Name: "Baked", LastSynced: ""},
		{MetadataID: "lab/both", Name: "BakedBoth", LastSynced: ""},
	}
	cached := []bestiary.EntityMetadata{
		{MetadataID: "lab/both", Name: "SyncedBoth", LastSynced: "2026-07-12T00:00:00Z"},
		{MetadataID: "lab/cached-only", Name: "Cached", LastSynced: "2026-07-12T00:00:00Z"},
	}

	got := bestiary.MergeEntityMetadata(static, cached)

	byID := map[bestiary.MetadataID]bestiary.EntityMetadata{}
	for _, m := range got {
		byID[m.MetadataID] = m
	}
	if len(got) != 3 {
		t.Fatalf("MergeEntityMetadata union size = %d, want 3 (baked-only + both + cached-only); got %+v", len(got), got)
	}
	if m, ok := byID["lab/baked-only"]; !ok || m.Name != "Baked" {
		t.Errorf("baked-only row did not survive merge: ok=%v name=%q; want a surviving \"Baked\" row", ok, m.Name)
	}
	if m, ok := byID["lab/cached-only"]; !ok || m.Name != "Cached" {
		t.Errorf("cached-only row missing from merge: ok=%v name=%q", ok, m.Name)
	}
}

// TestMergeEntityMetadata_MostRecentWins pins the recency rule: on a shared
// MetadataID the row with the later LastSynced wins, and a baked row (empty
// LastSynced) always loses to any synced row regardless of argument position.
func TestMergeEntityMetadata_MostRecentWins(t *testing.T) {
	static := []bestiary.EntityMetadata{{MetadataID: "lab/x", Name: "Baked", LastSynced: ""}}
	cached := []bestiary.EntityMetadata{{MetadataID: "lab/x", Name: "Synced", LastSynced: "2026-07-12T00:00:00Z"}}

	got := bestiary.MergeEntityMetadata(static, cached)
	if len(got) != 1 || got[0].Name != "Synced" {
		t.Fatalf("most-recent-wins failed: got %+v, want single \"Synced\" row", got)
	}

	// Older cached must NOT overwrite a newer static — recency, not position, decides.
	newerStatic := []bestiary.EntityMetadata{{MetadataID: "lab/x", Name: "NewBaked", LastSynced: "2026-07-12T09:00:00Z"}}
	olderCached := []bestiary.EntityMetadata{{MetadataID: "lab/x", Name: "OldSynced", LastSynced: "2026-07-12T00:00:00Z"}}
	got = bestiary.MergeEntityMetadata(newerStatic, olderCached)
	if len(got) != 1 || got[0].Name != "NewBaked" {
		t.Fatalf("older cached wrongly overwrote newer static: got %+v, want \"NewBaked\"", got)
	}
}

// --------------------------------------------------------------------------
// Join / Attach — mechanical path
// --------------------------------------------------------------------------

// TestJoinEntityMetadata_MechanicalMatch: a metadata id that decomposes to a present
// entity's identity key attaches its metadata to exactly that entity.
func TestJoinEntityMetadata_MechanicalMatch(t *testing.T) {
	target := entityWithRef("llama", "", "3.3", "70b", "instruct") // key llama@3.3#70b{instruct}
	other := entityWithRef("glm", "", "4.6", "")                   // key glm@4.6
	meta := []bestiary.EntityMetadata{{MetadataID: "meta/llama-3.3-70b-instruct", Name: "Llama"}}

	attached, unlinked, standalone := bestiary.JoinEntityMetadata([]bestiary.Entity{target, other}, meta)
	if len(unlinked) != 0 || len(standalone) != 0 {
		t.Fatalf("clean mechanical match should have no unlinked/standalone; got unlinked=%v standalone=%d", unlinked, len(standalone))
	}
	if attached[0].Metadata == nil || attached[0].Metadata.Name != "Llama" {
		t.Errorf("target entity did not receive metadata: %+v", attached[0].Metadata)
	}
	if attached[1].Metadata != nil {
		t.Errorf("non-matching entity wrongly received metadata: %+v", attached[1].Metadata)
	}
}

// TestJoinEntityMetadata_FamilyKnownMiss pins the two-tier miss upper tier: the
// decomposed family IS present but the tuple mismatches -> the id is reported as
// unlinked and NOT attached, NOT synthesized as a standalone.
func TestJoinEntityMetadata_FamilyKnownMiss(t *testing.T) {
	// Registry has llama@3.3#8b{instruct}; the metadata decomposes to #70b.
	e := entityWithRef("llama", "", "3.3", "8b", "instruct")
	meta := []bestiary.EntityMetadata{{MetadataID: "meta/llama-3.3-70b-instruct", Name: "Llama70"}}

	attached, unlinked, standalone := bestiary.JoinEntityMetadata([]bestiary.Entity{e}, meta)
	if len(standalone) != 0 {
		t.Errorf("family-known miss must NOT synthesize a standalone; got %d", len(standalone))
	}
	if attached[0].Metadata != nil {
		t.Errorf("family-known miss must NOT attach; got %+v", attached[0].Metadata)
	}
	if len(unlinked) != 1 || unlinked[0] != "meta/llama-3.3-70b-instruct" {
		t.Fatalf("family-known miss must be reported as unlinked; got %v", unlinked)
	}
}

// TestJoinEntityMetadata_FamilyAbsentStandalone pins the two-tier miss lower tier: a
// metadata id whose family is absent entirely becomes a metadata-only standalone
// entity (Ref from decomposition, empty Instances, Sources=[models.dev], metadata
// attached) and is NOT reported as unlinked.
func TestJoinEntityMetadata_FamilyAbsentStandalone(t *testing.T) {
	e := entityWithRef("llama", "", "3.3", "70b", "instruct")
	meta := []bestiary.EntityMetadata{{MetadataID: "somelabxyz/frobnik-9-42b", Name: "Frob"}}

	attached, unlinked, standalone := bestiary.JoinEntityMetadata([]bestiary.Entity{e}, meta)
	if len(unlinked) != 0 {
		t.Errorf("family-absent id must NOT be unlinked; got %v", unlinked)
	}
	if attached[0].Metadata != nil {
		t.Errorf("unrelated entity wrongly received metadata: %+v", attached[0].Metadata)
	}
	if len(standalone) != 1 {
		t.Fatalf("family-absent id must synthesize exactly one standalone; got %d", len(standalone))
	}
	s := standalone[0]
	if s.Ref.String() != "frobnik@9#42b" {
		t.Errorf("standalone key = %q, want %q", s.Ref.String(), "frobnik@9#42b")
	}
	if s.Metadata == nil || s.Metadata.MetadataID != "somelabxyz/frobnik-9-42b" {
		t.Errorf("standalone missing its metadata: %+v", s.Metadata)
	}
	if len(s.Instances) != 0 {
		t.Errorf("standalone must have empty Instances; got %d", len(s.Instances))
	}
	if len(s.Sources) != 1 || s.Sources[0] != bestiary.DataSourceModelsDev {
		t.Errorf("standalone Sources = %v, want [%s]", s.Sources, bestiary.DataSourceModelsDev)
	}
}

// TestAttachEntityMetadata_Idempotent pins C-soft-1: feeding a prior Attach result
// (which already contains a synthesized standalone) back through Attach with the same
// metadata RE-ATTACHES onto the existing standalone rather than duplicating it.
func TestAttachEntityMetadata_Idempotent(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("llama", "", "3.3", "70b", "instruct")}
	meta := []bestiary.EntityMetadata{{MetadataID: "somelabxyz/frobnik-9-42b", Name: "Frob"}}

	out1 := bestiary.AttachEntityMetadata(ents, meta)
	if len(out1) != 2 {
		t.Fatalf("first Attach should yield entity + standalone (2); got %d", len(out1))
	}

	out2 := bestiary.AttachEntityMetadata(out1, meta)
	if len(out2) != len(out1) {
		t.Fatalf("second Attach duplicated the standalone: len=%d, want %d", len(out2), len(out1))
	}
	count := 0
	for _, e := range out2 {
		if e.Ref.String() == "frobnik@9#42b" {
			count++
			if e.Metadata == nil {
				t.Error("re-attached standalone lost its metadata")
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one frobnik standalone after re-attach; got %d", count)
	}
}

// TestAttachEntityMetadata_Purity pins the pure-function contract: the input entities
// and metadata are never mutated, and a returned entity's metadata never aliases the
// caller's metadata slice.
func TestAttachEntityMetadata_Purity(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("llama", "", "3.3", "70b", "instruct")}
	meta := []bestiary.EntityMetadata{{
		MetadataID: "meta/llama-3.3-70b-instruct",
		Name:       "Original",
		Links:      []bestiary.ModelLink{{Label: "card", URL: "u"}},
	}}

	out := bestiary.AttachEntityMetadata(ents, meta)

	if ents[0].Metadata != nil {
		t.Errorf("input entity was mutated (Metadata set on the caller's slice): %+v", ents[0].Metadata)
	}
	if out[0].Metadata == nil || out[0].Metadata.Name != "Original" {
		t.Fatalf("output entity did not receive metadata: %+v", out[0].Metadata)
	}

	// Mutating the returned copy must not reach back into the caller's metadata slice.
	out[0].Metadata.Name = "Mutated"
	out[0].Metadata.Links[0].Label = "changed"
	if meta[0].Name != "Original" || meta[0].Links[0].Label != "card" {
		t.Errorf("returned metadata aliased the input slice: input now %+v", meta[0])
	}
}
