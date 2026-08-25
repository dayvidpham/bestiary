package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// metadata_multirow_cli_test.go pins the CLI half of the multi-row metadata contract:
// `show --by-entity` lists EVERY metadata row joined to the entity and attributes each
// benchmark table to the MetadataID that reported it, and the sync overlay's baked base
// layer is rebuilt from the full row set rather than one row per entity.

// vendoredMetadataRowCount computes the expected metadata-row total directly from the
// vendored codegen input — the "models" view of parse/data/modelsdev/catalog.json —
// so the overlay-base guard survives a snapshot refresh instead of pinning a literal
// that goes stale the next time the snapshot moves.
func vendoredMetadataRowCount(t *testing.T) int {
	t.Helper()
	path := filepath.Join("..", "..", "parse", "data", "modelsdev", "catalog.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read the vendored models.dev catalog at %s: %v;\n"+
			"  how to fix: this guard derives its expected row count from that committed"+
			" codegen input — restore it or refresh it per the snapshot-refresh workflow", path, err)
	}
	var catalog struct {
		Models map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("could not parse the vendored models.dev catalog at %s: %v", path, err)
	}
	return len(catalog.Models)
}

// TestOverlayBase_IsEveryBakedRow_NotOnePerEntity asserts the sync overlay's baked base
// layer is the FULL set of baked metadata rows — every row of the vendored models view
// — not one row per entity.
//
// This is what makes a sync non-destructive for multi-row entities: MergeEntityMetadata
// unions the base against the synced rows per MetadataID, so a row missing from the base
// layer could not survive as a baked-only row. Reading the derived Metadata primary
// instead of MetadataAll would produce the entity count here.
func TestOverlayBase_IsEveryBakedRow_NotOnePerEntity(t *testing.T) {
	ents := bestiary.Entities()
	base := bakedEntityMetadataFromEntities(ents)

	want := vendoredMetadataRowCount(t)
	if len(base) != want {
		t.Errorf("overlay base layer = %d metadata rows, want %d (every vendored models.dev row)", len(base), want)
	}

	// Non-vacuity + the distinguishing arm: the base must exceed the number of
	// entities carrying metadata, otherwise this guard could not tell the full row
	// set apart from a one-row-per-entity reconstruction.
	entitiesWithMetadata := 0
	for _, e := range ents {
		if len(e.MetadataAll) > 0 {
			entitiesWithMetadata++
		}
	}
	if entitiesWithMetadata == 0 {
		t.Fatal("no entity carries metadata; this guard would be vacuous")
	}
	if len(base) <= entitiesWithMetadata {
		t.Errorf("overlay base (%d) does not exceed the entities carrying metadata (%d);"+
			" a one-row-per-entity reconstruction would be indistinguishable here", len(base), entitiesWithMetadata)
	}

	// Every id is distinct: the base layer must not double-count a row.
	seen := make(map[bestiary.MetadataID]struct{}, len(base))
	for _, m := range base {
		if _, dup := seen[m.MetadataID]; dup {
			t.Errorf("overlay base layer contains metadata id %q twice", m.MetadataID)
		}
		seen[m.MetadataID] = struct{}{}
	}
}

// TestShow_ByEntity_GPT55_ListsBothRowsAttributed is the CLI production witness: over
// the real committed registry, `show gpt@5.5 --by-entity` names both lab identifiers,
// marks the primary, and heads the benchmark table with the identifier the claims were
// reported under.
//
// Before the multi-row repair the entity kept only openai/gpt-5.5-instant, so its
// 31 claims — all reported under openai/gpt-5.5 — rendered nowhere.
func TestShow_ByEntity_GPT55_ListsBothRowsAttributed(t *testing.T) {
	e, ok := bestiary.EntityByTuple("gpt", "", "5.5", "")
	if !ok {
		t.Skip("gpt@5.5 absent from the registry corpus")
	}
	if len(e.MetadataAll) < 2 {
		t.Fatalf("gpt@5.5 carries %d metadata rows, want the 2 the corpus provides", len(e.MetadataAll))
	}

	var out string
	_ = captureStderr(t, func() {
		out = captureStdout(t, func() {
			if err := run([]string{"show", "gpt@5.5", "--by-entity"}); err != nil {
				t.Fatalf("show gpt@5.5 --by-entity: %v", err)
			}
		})
	})

	if !strings.Contains(out, "Metadata rows (2): openai/gpt-5.5 (primary), openai/gpt-5.5-instant") {
		t.Errorf("show did not enumerate both metadata rows with the primary marked;\ngot:\n%s", out)
	}
	if !strings.Contains(out, "Claims reported under openai/gpt-5.5:") {
		t.Errorf("benchmark table is not attributed to the MetadataID that reported it;\ngot:\n%s", out)
	}
	// The recovered claims themselves.
	claims := e.MetadataAll[0].Benchmarks
	if len(claims) == 0 {
		t.Fatal("the corpus row openai/gpt-5.5 carries no claims; this witness would be vacuous")
	}
	if !strings.Contains(out, "Benchmarks (") {
		t.Errorf("no benchmark table rendered for gpt@5.5;\ngot:\n%s", out)
	}
	// The row with zero claims contributes no table — an empty one would be noise.
	if strings.Contains(out, "Claims reported under openai/gpt-5.5-instant:") {
		t.Errorf("a metadata row with no claims must not render an empty benchmark table;\ngot:\n%s", out)
	}
}

// TestShow_ByEntity_SingleRow_StillAttributed asserts the attribution is unconditional:
// an entity with exactly one metadata row still names that row, so a reader never has
// to guess which identifier a claim was published under.
func TestShow_ByEntity_SingleRow_StillAttributed(t *testing.T) {
	var single bestiary.Entity
	found := false
	for _, e := range bestiary.Entities() {
		if len(e.MetadataAll) == 1 && len(e.MetadataAll[0].Benchmarks) > 0 && len(e.Instances) > 0 {
			single, found = e, true
			break
		}
	}
	if !found {
		t.Skip("no single-row metadata entity with claims in the corpus")
	}

	var out string
	_ = captureStderr(t, func() {
		out = captureStdout(t, func() {
			if err := run([]string{"show", single.Ref.String(), "--by-entity"}); err != nil {
				t.Fatalf("show %s --by-entity: %v", single.Ref.String(), err)
			}
		})
	})

	want := "Metadata rows (1): " + string(single.MetadataAll[0].MetadataID) + " (primary)"
	if !strings.Contains(out, want) {
		t.Errorf("single-row entity %s is missing its attribution line %q;\ngot:\n%s", single.Ref.String(), want, out)
	}
}

// TestEntitiesTable_BenchmarkCount_SumsAllRows asserts the entities table's BENCHMARKS
// column is the sum of the claims across EVERY joined row, not just the primary's.
// Counting only the primary would under-report a multi-row entity.
func TestEntitiesTable_BenchmarkCount_SumsAllRows(t *testing.T) {
	e := bestiary.Entity{
		Ref: bestiary.EntityRef{Family: "gpt", Version: "5.5"},
		MetadataAll: []bestiary.EntityMetadata{
			{MetadataID: "openai/gpt-5.5", Benchmarks: []bestiary.BenchmarkResult{{Name: "a"}, {Name: "b"}}},
			{MetadataID: "openai/gpt-5.5-instant", Benchmarks: []bestiary.BenchmarkResult{{Name: "c"}}},
		},
	}
	e.Metadata = &e.MetadataAll[0]

	var sb strings.Builder
	writeEntitiesTable(&sb, []bestiary.Entity{e})
	fields := entitiesRowFields(sb.String(), "gpt@5.5")
	if fields == nil {
		t.Fatalf("entities table missing the gpt@5.5 row; got:\n%s", sb.String())
	}
	// ENTITY KEY | PROVIDERS | METADATA | BENCHMARKS
	if len(fields) != 4 || fields[2] != "yes" || fields[3] != "3" {
		t.Errorf("row = %v, want [gpt@5.5 0 yes 3] (2 claims + 1 claim summed across both rows)", fields)
	}
}
