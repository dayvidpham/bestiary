package bestiary_test

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
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

// TestMergeEntityMetadata_Union pins the union property: a baked-only metadata row
// (present only in the static set, never re-synced) SURVIVES a merge with a disjoint
// cached set, and a cached-only row is added — union semantics, mirroring MergeModels.
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

// TestAttachEntityMetadata_Idempotent pins re-attach idempotency: feeding a prior
// Attach result (which already contains a synthesized standalone) back through Attach
// with the same metadata RE-ATTACHES onto the existing standalone rather than
// duplicating it.
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

// TestJoinEntityMetadata_FamilyAbsentStandalone_AccumulatesRows pins the standalone
// arm of the "one entity, many rows" rule: TWO metadata rows whose absent-family ids
// decompose to the SAME entity key produce exactly ONE synthesized standalone that
// carries BOTH rows on MetadataAll (sorted ascending by MetadataID, each row keeping
// its own claims), with Metadata derived as the shortest id — never two duplicate
// entities and never a silent overwrite of the first row.
//
// Mutation guard: dropping the standaloneByKey index (synthesizing per row) fails the
// len(standalone) == 1 arm; keeping the index but replacing the entity instead of
// appending fails the MetadataAll length/ordering arm.
func TestJoinEntityMetadata_FamilyAbsentStandalone_AccumulatesRows(t *testing.T) {
	e := entityWithRef("llama", "", "3.3", "70b", "instruct")
	meta := []bestiary.EntityMetadata{
		multiRowMeta("somelabxyz/frobnik-9-42b-instant", "Instant"), // supplied SECOND-sorting first
		multiRowMeta("somelabxyz/frobnik-9-42b", "Base"),
	}

	attached, unlinked, standalone := bestiary.JoinEntityMetadata([]bestiary.Entity{e}, meta)
	if len(unlinked) != 0 {
		t.Errorf("family-absent ids must NOT be unlinked; got %v", unlinked)
	}
	if attached[0].Metadata != nil || len(attached[0].MetadataAll) != 0 {
		t.Errorf("unrelated entity wrongly received metadata: %+v", attached[0].MetadataAll)
	}
	if len(standalone) != 1 {
		t.Fatalf("two rows sharing one absent-family key must synthesize exactly ONE standalone; got %d", len(standalone))
	}
	s := standalone[0]
	if s.Ref.String() != "frobnik@9#42b" {
		t.Fatalf("standalone key = %q, want %q", s.Ref.String(), "frobnik@9#42b")
	}
	gotIDs := metadataIDsOf(s)
	wantIDs := []string{"somelabxyz/frobnik-9-42b", "somelabxyz/frobnik-9-42b-instant"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("standalone MetadataAll ids = %v, want %v (both rows kept, ascending by MetadataID)", gotIDs, wantIDs)
	}
	if s.Metadata == nil || s.Metadata.MetadataID != "somelabxyz/frobnik-9-42b" {
		t.Errorf("standalone primary = %+v, want the shortest id %q", s.Metadata, "somelabxyz/frobnik-9-42b")
	}
	// Per-row payload attribution: each row keeps ITS OWN claims, never fused.
	for _, m := range s.MetadataAll {
		if len(m.Benchmarks) != 1 {
			t.Fatalf("row %s = %d benchmarks, want 1 (claims must not be fused across rows)", m.MetadataID, len(m.Benchmarks))
		}
		wantBench := map[bestiary.MetadataID]string{
			"somelabxyz/frobnik-9-42b":         "Base-bench",
			"somelabxyz/frobnik-9-42b-instant": "Instant-bench",
		}[m.MetadataID]
		if m.Benchmarks[0].Name != wantBench {
			t.Errorf("row %s benchmark = %q, want %q", m.MetadataID, m.Benchmarks[0].Name, wantBench)
		}
	}
}

// modelsdevUnlinkedFileJSON is the read-only view of the codegen-emitted
// parse/data/modelsdev_unlinked.json report. It mirrors loadCreatorsFile's discipline:
// read the committed FILE, because the file IS the artifact under guard.
type modelsdevUnlinkedFileJSON struct {
	Count    int      `json:"count"`
	Unlinked []string `json:"unlinked"`
}

// unlinkedJustifiedExceptions is the CLOSED ledger of models.dev metadata ids that are
// KNOWN to be unjoinable against this catalog snapshot, each with the reason it cannot
// be aliased honestly. It is asserted by SET EQUALITY, not as a count: a row appearing
// that is not listed here fails, and a listed row that JOINS again also fails, so the
// ledger can neither absorb a new orphan silently nor outlive the condition it records.
//
// This replaces a drained-to-zero gate. That gate was correct while every orphan had an
// honest alias target; the 2026-08-28 catalog refresh produced twelve that do not. The
// shape of the problem is the same in every row: models.dev's models VIEW is a LAB
// catalogue — what a lab published — while the provider rows are a SERVING catalogue —
// what someone will sell you today. A lab model no provider serves has no entity to
// attach to, and the join reports it here because its FAMILY is served even though that
// exact (variant, version, size) is not.
//
// Aliasing such a row anyway is the failure this ledger exists to prevent: it would
// point a lab's description, license and benchmarks at a DIFFERENT artifact. The
// modelsdev_aliases.json collision-hazard rule says the same thing from the other side.
//
// Each row is therefore a deliberate NON-alias, and the fix for most of them is upstream
// of the join — a parse-level change that mints the finer entity key the serving rows
// deserve (see the per-row notes). When that lands, the row joins mechanically and must
// be deleted from this ledger.
var unlinkedJustifiedExceptions = map[string]string{
	// Lab publishes a size/version tier nobody serves.
	"arcee-ai/trinity-nano-preview": "Arcee ships Trinity Nano; the catalog serves only trinity/large and trinity/mini. " +
		"Aliasing onto either would attribute Nano's card to a different tier.",
	"deepreinforce/ornith-1.0-31b": "DeepReinforce publishes Ornith 1.0 at 9B/31B/397B; the catalog serves Ornith 1.0 only " +
		"at 35B (inferx) and Ornith 1.5 at 9B/397B. Before this refresh these three rows were metadata-only " +
		"STANDALONES, because no catalog entity carried the ornith family at all; upstream now emits a bare " +
		"ornith family and real serving rows, so the presence gate reclassifies them standalone -> unlinked " +
		"without any of them becoming joinable.",
	"deepreinforce/ornith-1.0-397b": "See deepreinforce/ornith-1.0-31b: Ornith 1.0 397B is published but not served " +
		"(only ornith@1.5#397b is).",
	"deepreinforce/ornith-1.0-9b": "See deepreinforce/ornith-1.0-31b: Ornith 1.0 9B is published but not served " +
		"(only ornith@1.5#9b is).",
	"swiss-ai/apertus-8b": "The lab row is the BASE Apertus 8B; the only served 8B is publicai/apertus-8b-instruct " +
		"(apertus#8b{instruct}). Base and instruct are distinct identities on the modifier axis, so aliasing the " +
		"base card onto the instruct entity would assert an identity the catalog does not.",
	"minimax/image-01": "MiniMax publishes image-01; no provider serves it. The nearest key, minimax@01, is " +
		"MiniMax-01 / minimax-text-01 — a TEXT model, a different artifact.",

	// Served, but only under a COARSE key that already holds other lab models. The honest
	// fix is a parse-level version/identity lift, not an alias onto the coarse bucket.
	"bytedance-seed/seed-2.1-pro": "Doubao Seed 2.1 Pro IS served (volcengine, ofox, nano-gpt) but folds into the coarse " +
		"seed/pro key, which also holds seedance-v1.0-pro, seedance-v1.5-pro, seedream-5.0-pro and doubao-seed-2.0-pro. " +
		"Aliasing there would misattribute. Needs a parse-level lift minting seed/pro@2.1.",
	"xai/grok-imagine-image-2.0": "Grok Imagine Image 2.0 IS served but folds into grok/image alongside grok-imagine-image " +
		"and grok-imagine-image-quality — three distinct products on one key. Needs a version lift minting grok/image@2.0.",
	"xai/grok-imagine-video-1.5": "Grok Imagine Video 1.5 is served but no grok video key exists at all; its rows fold into " +
		"a coarse grok bucket. Needs a parse-level identity for the video line.",
	"google/gemini-2.5-computer-use-preview-10-2025": "The Computer Use preview folds into the ordinary gemini/pro@2.5 " +
		"entity, which is a different product. Needs a computer-use identity modifier.",
	"google/gemini-robotics-er-1.6-preview": "Gemini Robotics-ER folds into the bare gemini catch-all. Needs a robotics " +
		"identity before it can carry its own card.",
	"google/deep-research-max-preview-04-2026": "The non-Max Deep Research preview is aliased onto gemini/deep-research " +
		"(one instance, poe). MAX is a distinct tier with no serving row of its own, so pointing it at the same entity " +
		"would collapse two tiers onto one card.",
}

// TestModelsdevUnlinked_MatchesJustifiedLedger is the join-disagreement gate.
//
// A row in the codegen-emitted report is a models.dev metadata id whose decomposed
// family IS present in the catalog but whose full identity tuple matched no entity — so
// the row's description, license, benchmarks and links attach to NOTHING and silently
// vanish from `bestiary show`. Nothing else holds that: the file is codegen-emitted, so
// a curation change that re-orphans a row just regenerates a bigger report.
//
// A curation slice that re-keys an entity breaks this in BOTH directions: re-keying an
// entity out from under an alias orphans that alias's row, and splitting a family
// without retargeting its alias orphans the alias itself.
//
// The assertion is SET EQUALITY against unlinkedJustifiedExceptions. It is NOT strictly
// stronger than the drained-to-zero count it replaced: a zero gate rejects all twelve rows
// this ledger admits, so on the admit-an-orphan axis the old gate was stricter, and the two
// are otherwise incomparable. What is true, and is the reason for the change: given that a
// non-empty justified set IS accepted, set equality is the strongest gate available over it,
// and it adds a direction a count never had — a NEW orphan fails because it is not listed,
// and a listed row that starts joining again fails as dead curation. Neither can hide behind
// an unchanged total.
//
// Reading the count field AND the array length independently is deliberate: the count is
// what a human skims and the array is what is true, and a hand-edit that disagrees with
// itself is its own defect (the file is marked DO NOT EDIT for that reason).
func TestModelsdevUnlinked_MatchesJustifiedLedger(t *testing.T) {
	raw, err := os.ReadFile("parse/data/modelsdev_unlinked.json")
	if err != nil {
		t.Fatalf("read parse/data/modelsdev_unlinked.json: %v", err)
	}
	var f modelsdevUnlinkedFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse parse/data/modelsdev_unlinked.json: %v", err)
	}

	reported := make(map[string]bool, len(f.Unlinked))
	for _, id := range f.Unlinked {
		reported[id] = true
	}

	var unjustified []string
	for id := range reported {
		if _, ok := unlinkedJustifiedExceptions[id]; !ok {
			unjustified = append(unjustified, id)
		}
	}
	var stale []string
	for id := range unlinkedJustifiedExceptions {
		if !reported[id] {
			stale = append(stale, id)
		}
	}
	sort.Strings(unjustified)
	sort.Strings(stale)

	if len(unjustified) > 0 {
		t.Errorf("parse/data/modelsdev_unlinked.json carries %d UNJUSTIFIED unlinked metadata id(s): %v\n"+
			"  What: those models.dev rows decomposed to a family the catalog serves, but matched no entity,\n"+
			"        so their description/license/benchmarks/links attach to nothing and disappear from `show`.\n"+
			"  Why now: a curation re-key moved an entity out from under a curated alias, a family was split\n"+
			"        without retargeting the alias that pointed into it, or a catalog refresh introduced a lab\n"+
			"        row no provider serves.\n"+
			"  How to fix: EITHER add a parse/data/modelsdev_aliases.json entry — verifying the target is a\n"+
			"        DISTINCT entity first, per that file's collision-hazard rule — OR, if no honest target\n"+
			"        exists, add the id to unlinkedJustifiedExceptions in this file WITH the reason it cannot\n"+
			"        be aliased. Do NOT hand-edit the report; it is codegen-emitted.",
			len(unjustified), unjustified)
	}
	if len(stale) > 0 {
		t.Errorf("unlinkedJustifiedExceptions holds %d id(s) that are no longer unlinked: %v\n"+
			"  What: the ledger claims these cannot be joined, but the report no longer lists them.\n"+
			"  Why: a parse-level fix or a new alias made them joinable — which is the outcome each entry asks for.\n"+
			"  How to fix: delete those entries from unlinkedJustifiedExceptions; a ledger that outlives its\n"+
			"        condition is dead curation and would let a future re-orphan pass unnoticed.",
			len(stale), stale)
	}
	if f.Count != len(f.Unlinked) {
		t.Errorf("parse/data/modelsdev_unlinked.json count field = %d but the unlinked array holds %d entries; "+
			"the file disagrees with itself — re-run `go generate ./...` rather than hand-editing it",
			f.Count, len(f.Unlinked))
	}
}
