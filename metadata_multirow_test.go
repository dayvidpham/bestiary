package bestiary_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// metadata_multirow_test.go pins the MULTI-ROW metadata contract: an entity carries
// EVERY metadata row that decomposes to its identity on MetadataAll (sorted ascending
// by MetadataID) with Metadata as the derived primary (shortest MetadataID, ties
// lexicographic ascending).
//
// The defect this closes: distinct lab ids routinely decompose to one entity key
// ("openai/gpt-5.5" and "openai/gpt-5.5-instant" both key gpt@5.5), and a single
// Metadata pointer kept only whichever row the join happened to visit last —
// silently discarding the other row's benchmark claims and links. These tests
// exercise the EXPORTED join surface (the same code path the registry, the CLI
// overlay and codegen use), never a test-only helper.

// multiRowMeta builds a metadata row carrying one benchmark claim and one link, so a
// dropped row is observable as lost payload and not merely a lost id.
func multiRowMeta(id, name string) bestiary.EntityMetadata {
	return bestiary.EntityMetadata{
		MetadataID:  bestiary.MetadataID(id),
		Name:        name,
		Description: name + " description",
		Benchmarks:  []bestiary.BenchmarkResult{{Name: name + "-bench", Metric: "acc", Score: 1}},
		Links:       []bestiary.ModelLink{{Type: bestiary.LinkDocs, URL: "https://example.test/" + name}},
	}
}

// metadataIDsOf projects an entity's MetadataAll onto its ids, in slice order, so a
// test can assert BOTH membership and the ascending-MetadataID ordering contract.
func metadataIDsOf(e bestiary.Entity) []string {
	out := make([]string, len(e.MetadataAll))
	for i, m := range e.MetadataAll {
		out[i] = string(m.MetadataID)
	}
	return out
}

// --------------------------------------------------------------------------
// The contract: every row kept, sorted, primary derived
// --------------------------------------------------------------------------

// TestJoin_MetadataAll_KeepsEveryRow_SortedAscending asserts the core repair: two
// metadata ids that decompose to ONE entity key both land on MetadataAll, ordered
// ascending by MetadataID, with each row's own benchmark claims and links intact.
//
// Mutation guard: the pre-repair single-pointer join would leave len(MetadataAll)
// at most 1, and a join that inherited incoming order would fail the ordering arm
// because the rows are supplied in DESCENDING id order here.
func TestJoin_MetadataAll_KeepsEveryRow_SortedAscending(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
	meta := []bestiary.EntityMetadata{
		multiRowMeta("openai/gpt-5.5-instant", "Instant"), // supplied SECOND-sorting first
		multiRowMeta("openai/gpt-5.5", "Base"),
	}

	attached, unlinked, standalone := bestiary.JoinEntityMetadata(ents, meta)
	if len(unlinked) != 0 || len(standalone) != 0 {
		t.Fatalf("both rows must attach to the existing entity; unlinked=%v standalone=%d", unlinked, len(standalone))
	}
	if len(attached) != 1 {
		t.Fatalf("attached = %d entities, want 1", len(attached))
	}

	got := metadataIDsOf(attached[0])
	want := []string{"openai/gpt-5.5", "openai/gpt-5.5-instant"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MetadataAll ids = %v, want %v (ascending by MetadataID, every row kept)", got, want)
	}
	// Per-row payload attribution: each row keeps ITS OWN claims, never fused.
	for _, m := range attached[0].MetadataAll {
		if len(m.Benchmarks) != 1 {
			t.Errorf("row %s carries %d benchmark claims, want its own 1 (claims must never be fused across rows)",
				m.MetadataID, len(m.Benchmarks))
		}
		if len(m.Links) != 1 {
			t.Errorf("row %s carries %d links, want its own 1", m.MetadataID, len(m.Links))
		}
	}
}

// TestJoin_Primary_IsShortestID_TieLexicographic pins the primary rule and both of
// its rejected alternatives. The shortest id wins regardless of (a) how many
// benchmark claims a row carries and (b) the order rows arrive in; a length tie
// falls back to lexicographic ascending.
func TestJoin_Primary_IsShortestID_TieLexicographic(t *testing.T) {
	t.Run("shortest id wins over a heavier payload", func(t *testing.T) {
		short := multiRowMeta("openai/gpt-5.5", "Base") // 1 claim
		long := multiRowMeta("openai/gpt-5.5-instant", "Instant")
		// Give the LONGER id a much heavier payload: a payload-count rule would pick it.
		for i := 0; i < 25; i++ {
			long.Benchmarks = append(long.Benchmarks, bestiary.BenchmarkResult{Name: "filler", Metric: "acc"})
		}
		ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
		attached, _, _ := bestiary.JoinEntityMetadata(ents, []bestiary.EntityMetadata{long, short})

		if attached[0].Metadata == nil {
			t.Fatal("primary Metadata is nil after a successful join")
		}
		if got := string(attached[0].Metadata.MetadataID); got != "openai/gpt-5.5" {
			t.Errorf("primary = %q, want %q; the primary is the SHORTEST id — never the heaviest payload and never the incoming order",
				got, "openai/gpt-5.5")
		}
	})

	t.Run("equal-length ids break lexicographic ascending", func(t *testing.T) {
		ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
		// Same length; supplied in DESCENDING order so incoming order would pick "b".
		meta := []bestiary.EntityMetadata{
			multiRowMeta("openai/gpt-5.5-bbb", "B"),
			multiRowMeta("openai/gpt-5.5-aaa", "A"),
		}
		attached, _, _ := bestiary.JoinEntityMetadata(ents, meta)
		if got := string(attached[0].Metadata.MetadataID); got != "openai/gpt-5.5-aaa" {
			t.Errorf("primary on a length tie = %q, want %q (lexicographic ascending)", got, "openai/gpt-5.5-aaa")
		}
	})
}

// TestJoin_Primary_AliasesIntoMetadataAll asserts Metadata is a DERIVED projection of
// MetadataAll and not an independent copy: the primary pointer addresses the matching
// element of the entity's own slice, so the two can never drift apart.
func TestJoin_Primary_AliasesIntoMetadataAll(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
	meta := []bestiary.EntityMetadata{
		multiRowMeta("openai/gpt-5.5", "Base"),
		multiRowMeta("openai/gpt-5.5-instant", "Instant"),
	}
	attached, _, _ := bestiary.JoinEntityMetadata(ents, meta)

	e := attached[0]
	if e.Metadata != &e.MetadataAll[0] {
		t.Errorf("primary Metadata does not address MetadataAll[0]; it must be a derived projection of the entity's own slice, not a separate copy")
	}
}

// --------------------------------------------------------------------------
// Purity
// --------------------------------------------------------------------------

// TestJoin_Purity_InputsUnmutated_NoSharedStorage asserts the join never mutates its
// inputs and that a returned entity shares no storage with them: mutating a returned
// row's benchmark table must not reach back into the caller's metadata slice.
//
// Mutation guard: an element-wise copy is required — append([]EntityMetadata(nil),
// rows...) copies the row structs but leaves each row's Benchmarks slice header
// aliasing the source, which the write-through arm below detects.
func TestJoin_Purity_InputsUnmutated_NoSharedStorage(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
	meta := []bestiary.EntityMetadata{
		multiRowMeta("openai/gpt-5.5", "Base"),
		multiRowMeta("openai/gpt-5.5-instant", "Instant"),
	}
	entsBefore := deepJSON(t, ents)
	metaBefore := deepJSON(t, meta)

	attached, _, _ := bestiary.JoinEntityMetadata(ents, meta)

	if got := deepJSON(t, ents); got != entsBefore {
		t.Errorf("JoinEntityMetadata mutated its entity input;\n got: %s\nwant: %s", got, entsBefore)
	}
	if got := deepJSON(t, meta); got != metaBefore {
		t.Errorf("JoinEntityMetadata mutated its metadata input;\n got: %s\nwant: %s", got, metaBefore)
	}

	// Write through the RETURNED entity's nested payload; the caller's rows must not move.
	attached[0].MetadataAll[0].Benchmarks[0].Name = "MUTATED"
	attached[0].MetadataAll[0].Links[0].URL = "https://mutated.test"
	if got := deepJSON(t, meta); got != metaBefore {
		t.Errorf("mutating a returned entity's metadata row reached back into the caller's slice;\n got: %s\nwant: %s", got, metaBefore)
	}
}

// TestEntities_Clone_MetadataAll_ElementWise asserts the same no-shared-storage
// property across the PUBLIC registry read: two Entities() calls hand out
// independent MetadataAll payloads, so a caller mutating one can never corrupt the
// memoized registry index or another caller's copy.
func TestEntities_Clone_MetadataAll_ElementWise(t *testing.T) {
	first := multiRowEntityFromRegistry(t)
	if len(first.MetadataAll) == 0 || len(first.MetadataAll[0].Benchmarks) == 0 {
		t.Skip("no registry entity with a benchmark-carrying metadata row; nothing to clone-check")
	}
	before := first.MetadataAll[0].Benchmarks[0].Name
	first.MetadataAll[0].Benchmarks[0].Name = "MUTATED"

	second := multiRowEntityFromRegistry(t)
	if got := second.MetadataAll[0].Benchmarks[0].Name; got != before {
		t.Errorf("mutating one Entities() copy changed a later read: got %q, want %q;"+
			" MetadataAll must be cloned ELEMENT-WISE (each row's Benchmarks/Links get fresh backing arrays)", got, before)
	}
}

// --------------------------------------------------------------------------
// Idempotence + no-match preservation
// --------------------------------------------------------------------------

// TestJoin_Idempotent_ReJoinByteIdentical asserts re-joining an already-joined entity
// set over the SAME metadata reproduces a byte-identical result: MetadataAll is
// cleared-then-accumulated per entity, so rows never double up across repeated joins.
//
// Mutation guard: an accumulate-without-clear implementation passes the first arm and
// fails here with two copies of every row.
func TestJoin_Idempotent_ReJoinByteIdentical(t *testing.T) {
	ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", "")}
	meta := []bestiary.EntityMetadata{
		multiRowMeta("openai/gpt-5.5", "Base"),
		multiRowMeta("openai/gpt-5.5-instant", "Instant"),
	}

	once, _, _ := bestiary.JoinEntityMetadata(ents, meta)
	twice, _, standalone := bestiary.JoinEntityMetadata(once, meta)

	if len(standalone) != 0 {
		t.Errorf("re-join synthesized %d standalone entities; an already-attached row must be RE-ATTACHED, never re-created", len(standalone))
	}
	if n := len(twice[0].MetadataAll); n != 2 {
		t.Fatalf("re-join left %d metadata rows, want 2; MetadataAll must be cleared before accumulating", n)
	}
	if got, want := deepJSON(t, twice), deepJSON(t, once); got != want {
		t.Errorf("re-join is not byte-identical;\n got: %s\nwant: %s", got, want)
	}
}

// TestJoin_NoMatch_PreservesOriginalMetadata asserts an entity that NO row lands on
// is left exactly as it arrived — its pre-existing Metadata and MetadataAll survive.
// The clear-then-accumulate step must be scoped to entities touched in THIS call,
// never applied blanket to the whole set.
func TestJoin_NoMatch_PreservesOriginalMetadata(t *testing.T) {
	untouched := entityWithRef("llama", "", "3.3", "70b", "instruct")
	untouched.MetadataAll = []bestiary.EntityMetadata{multiRowMeta("meta/llama-3.3-70b-instruct", "Llama")}
	untouched.Metadata = &untouched.MetadataAll[0]

	ents := []bestiary.Entity{entityWithRef("gpt", "", "5.5", ""), untouched}
	// Metadata that keys ONLY the gpt entity.
	meta := []bestiary.EntityMetadata{multiRowMeta("openai/gpt-5.5", "Base")}

	attached, _, _ := bestiary.JoinEntityMetadata(ents, meta)

	got := attached[1]
	if len(got.MetadataAll) != 1 || string(got.MetadataAll[0].MetadataID) != "meta/llama-3.3-70b-instruct" {
		t.Errorf("an entity no row matched lost its pre-existing MetadataAll: %v", metadataIDsOf(got))
	}
	if got.Metadata == nil || string(got.Metadata.MetadataID) != "meta/llama-3.3-70b-instruct" {
		t.Errorf("an entity no row matched lost its pre-existing Metadata primary")
	}
}

// --------------------------------------------------------------------------
// Corpus identity: the baked catalog
// --------------------------------------------------------------------------

// TestMetadataAll_TotalRows_MatchesVendoredModelsView is the corpus identity guard:
// the number of metadata rows reachable across the whole registry via MetadataAll
// must equal the number of rows in the vendored models.dev models view — every baked
// row is reachable from exactly one entity, none dropped and none duplicated.
//
// UNIT: metadata rows. AXIS: the whole baked registry (Entities()). CONFIGURATION:
// the committed parse/data/modelsdev/catalog.json snapshot, offline, no store overlay.
//
// The expected total is COMPUTED from the vendored view minus the justified-unlinked
// ledger (bakedMetadataRowCount), not copied from a plan: a snapshot refresh moves both
// sides together and this guard keeps measuring "every attachable row is reachable"
// rather than a stale literal. The pre-repair single-pointer join reached 224 of them.
func TestMetadataAll_TotalRows_MatchesVendoredModelsView(t *testing.T) {
	want := bakedMetadataRowCount(t)

	ents := bestiary.Entities()
	got := 0
	seen := make(map[bestiary.MetadataID]string, want)
	for _, e := range ents {
		for _, m := range e.MetadataAll {
			got++
			if prev, dup := seen[m.MetadataID]; dup {
				t.Errorf("metadata id %q is attached to two entities (%s and %s); a row belongs to exactly one identity",
					m.MetadataID, prev, e.Ref.String())
			}
			seen[m.MetadataID] = e.Ref.String()
		}
	}
	if got != want {
		t.Errorf("MetadataAll rows reachable over the registry = %d, want %d"+
			" (every ATTACHABLE row of the vendored models.dev models view — the whole view minus the"+
			" justified-unlinked ledger — none dropped);"+
			" distinct entities carrying metadata = %d", got, want, countEntitiesWithMetadata(ents))
	}
}

// TestMetadataAll_MultiRowEntitiesExist_GPT55Witness is the named witness for the
// repair: gpt@5.5 carries BOTH openai/gpt-5.5 and openai/gpt-5.5-instant, its primary
// is the shorter id, and the base row's benchmark claims — invisible before the
// repair, because the instant row won the single pointer — are reachable.
func TestMetadataAll_MultiRowEntitiesExist_GPT55Witness(t *testing.T) {
	e, ok := bestiary.EntityByTuple("gpt", "", "5.5", "")
	if !ok {
		t.Fatal("entity gpt@5.5 not found in the registry")
	}

	got := metadataIDsOf(e)
	want := []string{"openai/gpt-5.5", "openai/gpt-5.5-instant"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gpt@5.5 MetadataAll = %v, want %v", got, want)
	}
	if e.Metadata == nil || string(e.Metadata.MetadataID) != "openai/gpt-5.5" {
		t.Errorf("gpt@5.5 primary = %v, want openai/gpt-5.5 (shortest id)", e.Metadata)
	}
	// The recovered payload: the base row's claims must be reachable and attributed
	// to their OWN id, never merged into the instant row's (empty) table.
	base := e.MetadataAll[0]
	if len(base.Benchmarks) == 0 {
		t.Errorf("openai/gpt-5.5 carries no benchmark claims; the row the pre-repair join discarded is the one holding them")
	}
	if n := len(e.MetadataAll[1].Benchmarks); n != 0 {
		t.Errorf("openai/gpt-5.5-instant carries %d benchmark claims, want 0;"+
			" claims must stay attributed to the MetadataID the lab reported them under, never fused", n)
	}
}

// TestMetadataAll_MultiRowEntities_AllSortedAndPrimaryConsistent sweeps the WHOLE
// registry: every entity's MetadataAll is sorted ascending by MetadataID and its
// Metadata primary is exactly the shortest-id row of that slice. This is the
// value-based coverage arm — it holds for all ~32 multi-row entities, not just the
// named witness.
func TestMetadataAll_MultiRowEntities_AllSortedAndPrimaryConsistent(t *testing.T) {
	multi := 0
	for _, e := range bestiary.Entities() {
		if len(e.MetadataAll) == 0 {
			if e.Metadata != nil {
				t.Errorf("%s has a Metadata primary but an empty MetadataAll; the primary is derived from the record", e.Ref.String())
			}
			continue
		}
		if len(e.MetadataAll) > 1 {
			multi++
		}
		ids := metadataIDsOf(e)
		if !sort.StringsAreSorted(ids) {
			t.Errorf("%s MetadataAll is not sorted ascending by MetadataID: %v", e.Ref.String(), ids)
		}
		if e.Metadata == nil {
			t.Errorf("%s carries %d metadata rows but no primary", e.Ref.String(), len(e.MetadataAll))
			continue
		}
		wantPrimary := ids[0]
		for _, id := range ids[1:] {
			if len(id) < len(wantPrimary) || (len(id) == len(wantPrimary) && id < wantPrimary) {
				wantPrimary = id
			}
		}
		if got := string(e.Metadata.MetadataID); got != wantPrimary {
			t.Errorf("%s primary = %q, want %q (shortest MetadataID, ties lexicographic ascending)", e.Ref.String(), got, wantPrimary)
		}
	}
	// Non-vacuity: the sweep must actually observe multi-row entities, otherwise it
	// would pass trivially on a corpus where the repair changed nothing.
	if multi == 0 {
		t.Error("no multi-row entity found in the registry; this sweep would be vacuous")
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

// deepJSON marshals v for whole-value comparison, so an assertion covers every
// nested field rather than the handful a hand-written comparison would name.
func deepJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal for deep comparison failed: %v", err)
	}
	return string(b)
}

// countEntitiesWithMetadata reports how many entities carry at least one metadata
// row — the "distinct entities" figure that contrasts with the row total.
func countEntitiesWithMetadata(ents []bestiary.Entity) int {
	n := 0
	for _, e := range ents {
		if len(e.MetadataAll) > 0 {
			n++
		}
	}
	return n
}

// multiRowEntityFromRegistry returns a fresh registry copy of a known multi-row
// entity for the clone checks.
func multiRowEntityFromRegistry(t *testing.T) bestiary.Entity {
	t.Helper()
	e, ok := bestiary.EntityByTuple("gpt", "", "5.5", "")
	if !ok {
		t.Fatal("entity gpt@5.5 not found in the registry")
	}
	return e
}

// bakedMetadataRowCount computes the expected metadata-row total DIRECTLY from the
// vendored codegen input — the "models" view of parse/data/modelsdev/catalog.json,
// the same committed snapshot cmd/bestiary-gen bakes models_metadata_gen.go from —
// MINUS the rows that provably cannot attach.
// Deriving it here (rather than hard-coding a literal) is what makes the corpus
// identity guard survive a snapshot refresh: both sides move together, and the guard
// keeps asserting "every attachable vendored row is reachable" instead of a frozen number.
//
// The subtrahend is a PREMISE change from the 2026-08-28 catalog refresh, not a fudge
// factor. Until that refresh the join drained the disagreement report to zero, so "every
// row of the view attaches" was simply true and the raw view size was the right expected
// value. The refresh produced twelve rows that cannot be aliased honestly — a lab model
// whose family is served but whose exact (variant, version, size) is not — and each one
// is enumerated, with its reason, in unlinkedJustifiedExceptions in metadata_join_test.go.
// Those rows attach to nothing by design.
//
// Subtracting the LEDGER rather than a literal 12 is what keeps this honest: the ledger is
// asserted by SET EQUALITY against the codegen-emitted parse/data/modelsdev_unlinked.json
// in TestModelsdevUnlinked_MatchesJustifiedLedger, so a NEW orphan cannot hide inside this
// subtraction — it fails there first, and until it is justified in the ledger it is still
// counted as attachable here. When a parse-level fix makes one of the twelve joinable, its
// ledger row is deleted and this number rises on its own.
func bakedMetadataRowCount(t *testing.T) int {
	t.Helper()
	path := filepath.Join("parse", "data", "modelsdev", "catalog.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read the vendored models.dev catalog at %s: %v;\n"+
			"  how to fix: the corpus identity guard derives its expected row count from this"+
			" committed codegen input — restore it or refresh it per the snapshot-refresh workflow", path, err)
	}
	var catalog struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("could not parse the vendored models.dev catalog at %s: %v", path, err)
	}
	if len(catalog.Models) == 0 {
		t.Fatalf("the vendored models.dev catalog at %s has an empty \"models\" view;"+
			" the corpus identity guard would be vacuous", path)
	}
	if n := len(unlinkedJustifiedExceptions); n >= len(catalog.Models) {
		t.Fatalf("the justified-unlinked ledger holds %d ids but the vendored models view has only %d rows;"+
			" the expected attach count would be meaningless", n, len(catalog.Models))
	}
	return len(catalog.Models) - len(unlinkedJustifiedExceptions)
}

// --------------------------------------------------------------------------
// Store round-trip — no migration required
// --------------------------------------------------------------------------

// TestStore_RoundTripsEveryMetadataRow_NoMigration asserts the whole baked metadata
// record survives a SQLite round-trip and rejoins into the same MetadataAll shape.
//
// No store migration is needed for the multi-row repair, and this test is what proves
// it: entity_metadata is keyed by the stable metadata_id, one row per lab identifier,
// with no notion of "the entity's metadata". MetadataAll is rebuilt by the join at
// read time, so the multi-row record is a JOIN-layer property the existing v8 schema
// already carries. A schema that had stored one row per entity would fail here by
// losing the non-primary rows.
//
// UNIT: metadata rows. AXIS: the full baked catalog through Upsert then Query.
// CONFIGURATION: in-memory SQLite at the current store schema, offline.
func TestStore_RoundTripsEveryMetadataRow_NoMigration(t *testing.T) {
	ents := bestiary.Entities()
	var baked []bestiary.EntityMetadata
	for _, e := range ents {
		baked = append(baked, e.MetadataAll...)
	}
	if want := bakedMetadataRowCount(t); len(baked) != want {
		t.Fatalf("baked record = %d rows, want %d before the round-trip", len(baked), want)
	}

	store := openMemStore(t)
	ctx := context.Background()
	// entity_metadata.source_id is a real foreign key into data_sources, so the
	// dimension row must exist before the facts are written (the store's documented
	// ordering). Seed every source the baked record attributes rows to.
	var sources []bestiary.DataSource
	seenSource := map[bestiary.DataSourceID]struct{}{}
	for _, m := range baked {
		if _, dup := seenSource[m.Source]; dup || m.Source == "" {
			continue
		}
		seenSource[m.Source] = struct{}{}
		sources = append(sources, bestiary.DataSource{
			ID: m.Source, URI: "https://" + string(m.Source) + "/", CanonicalName: string(m.Source),
		})
	}
	if err := store.UpsertDataSources(ctx, sources, nil); err != nil {
		t.Fatalf("UpsertDataSources (FK parents for entity_metadata): %v", err)
	}
	if err := store.UpsertEntityMetadata(ctx, baked); err != nil {
		t.Fatalf("UpsertEntityMetadata over the full baked record: %v", err)
	}
	got, err := store.QueryEntityMetadata(ctx)
	if err != nil {
		t.Fatalf("QueryEntityMetadata: %v", err)
	}
	if len(got) != len(baked) {
		t.Errorf("store round-trip returned %d metadata rows, want %d (every row persists; none collapse per entity)", len(got), len(baked))
	}

	// Re-joining the round-tripped rows onto a metadata-free entity set must rebuild
	// the SAME MetadataAll record, so a cached read and a baked read agree.
	bare := make([]bestiary.Entity, len(ents))
	for i, e := range ents {
		e.Metadata, e.MetadataAll = nil, nil
		bare[i] = e
	}
	rejoined, _, _ := bestiary.JoinEntityMetadata(bare, got)
	for i := range rejoined {
		if len(rejoined[i].MetadataAll) != len(ents[i].MetadataAll) {
			t.Errorf("%s: rejoined from the store carries %d rows, baked carries %d",
				ents[i].Ref.String(), len(rejoined[i].MetadataAll), len(ents[i].MetadataAll))
		}
	}
}
