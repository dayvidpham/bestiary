package bestiary

// metadata_join_internal_test.go covers the parts of the metadata<->entity join that
// need internal access: curated-alias precedence (injecting a synthetic alias map),
// the mechanical decomposition contract, the graceful-degrade alias loader, and the
// registry hook that attaches baked metadata + folds in synthesized standalones.
//
// These tests mutate shared package state (modelsdevAliasMap, staticModels,
// entityIndex, entityIndexOnce, bakedEntityMetadata) and therefore must NOT run in
// parallel with each other or with any test that reads the entity index.

import (
	"sync"
	"testing"
)

// withSyntheticModelsdevAliases replaces the curated alias map for the duration of fn.
// It first forces the load-once to fire (so the real embedded file is read and the
// sync.Once is spent), then overwrites the cached map so metadataAliasRef observes the
// injection; the original map is restored on cleanup.
func withSyntheticModelsdevAliases(t *testing.T, aliases map[string]modelsdevAlias, fn func()) {
	t.Helper()
	loadModelsdevAliases() // spend the sync.Once so our overwrite is authoritative
	orig := modelsdevAliasMap
	modelsdevAliasMap = aliases
	t.Cleanup(func() { modelsdevAliasMap = orig })
	fn()
}

// withMetadataRegistry replaces staticModels and bakedEntityMetadata and resets the
// entity index for the duration of fn, then restores the originals. Mirrors
// withSyntheticRegistry (paramsize_wiring_internal_test.go) but also injects the baked
// metadata the registry hook consumes. Not safe for concurrent use.
func withMetadataRegistry(t *testing.T, models []ModelInfo, meta []EntityMetadata, fn func()) {
	t.Helper()

	origModels := staticModels
	origIndex := entityIndex
	origKeys := entityKeys
	origMeta := bakedEntityMetadata

	staticModels = models
	bakedEntityMetadata = meta
	entityIndexOnce = sync.Once{} // reset — assign a new zero value (go vet allows assign, not copy)
	entityIndex = nil
	entityKeys = nil

	t.Cleanup(func() {
		staticModels = origModels
		bakedEntityMetadata = origMeta
		entityIndexOnce = sync.Once{}
		entityIndex = origIndex
		entityKeys = origKeys
	})

	fn()
}

// TestMetadataEntityRef_Decomposition locks the mechanical decomposition contract:
// the lab prefix is stripped and the remainder decomposes through the production
// pipeline into the same identity key the registry would build.
func TestMetadataEntityRef_Decomposition(t *testing.T) {
	cases := []struct {
		id      MetadataID
		wantKey string
	}{
		{"meta/llama-3.3-70b-instruct", "llama@3.3#70b{instruct}"},
		{"meta/llama-3.3-70b", "llama@3.3#70b"},
		{"zhipuai/glm-4.6", "glm@4.6"},
		{"bare-no-lab-7b", "bare#7b"}, // no slash: whole id decomposes
		// Remainder-pin rule: the stripped remainder "llama-4-scout-17b-instruct" is
		// byte-equal to a curated pin, so the pin's full-shape token wins over the
		// mechanical "17b" and the join key agrees with the served entity's #17b-16e
		// key by construction. Deleting the pin branch in metadataParamSize regresses
		// this row to "...#17b{...}".
		{"meta/llama-4-scout-17b-instruct", "llama/scout@4#17b-16e{instruct}"},
	}
	for _, tc := range cases {
		if got := metadataEntityRef(tc.id).String(); got != tc.wantKey {
			t.Errorf("metadataEntityRef(%q).String() = %q, want %q", tc.id, got, tc.wantKey)
		}
	}
}

// TestMetadataParamSize_ThreeTiers is the focused fence on metadataParamSize's tier
// order over a lab-stripped remainder: (1) a remainder byte-equal to a pinned ID takes
// the pin (whose token diverges from the mechanical "17b", so a deleted pin branch is
// caught, not masked by agreement); (2) an unpinned remainder takes the mechanical
// ExtractParamSizeToken result; (3) a remainder with no pin and no size token yields "".
func TestMetadataParamSize_ThreeTiers(t *testing.T) {
	// Tier 1 — pin: the bare llama-4 scout form is a curated pin ("17b-16e").
	// Precondition: the mechanical result genuinely diverges, so this leg is
	// falsifiable by mutation.
	if mech, ok := ExtractParamSizeToken("llama-4-scout-17b-instruct"); !ok || mech != "17b" {
		t.Fatalf("precondition: mechanical token = (%q,%v), want (\"17b\",true) — the pin must diverge from mechanical", mech, ok)
	}
	if got := metadataParamSize("llama-4-scout-17b-instruct"); got != "17b-16e" {
		t.Errorf("pinned remainder: metadataParamSize = %q, want %q (pin tier)", got, "17b-16e")
	}

	// Tier 2 — mechanical: no pin for this remainder; the compound token is lifted whole.
	if got := metadataParamSize("qwen3-235b-a22b"); got != "235b-a22b" {
		t.Errorf("unpinned remainder: metadataParamSize = %q, want %q (mechanical tier)", got, "235b-a22b")
	}

	// Tier 3 — none: no pin, no size token (a dotted version is never a size).
	if got := metadataParamSize("glm-4.6"); got != "" {
		t.Errorf("token-less remainder: metadataParamSize = %q, want \"\"", got)
	}
}

// TestJoin_AliasBeatsMechanical pins curated > mechanical: with an injected alias that
// re-points a metadata id onto a DIFFERENT present entity than its mechanical
// decomposition, the alias target receives the metadata and the mechanical target does
// not.
func TestJoin_AliasBeatsMechanical(t *testing.T) {
	// mechanical: "meta/llama-3.3-70b-instruct" -> llama@3.3#70b{instruct}
	// alias re-points it onto glm@4.6.
	aliases := map[string]modelsdevAlias{
		"meta/llama-3.3-70b-instruct": {Family: "glm", Version: "4.6"},
	}
	withSyntheticModelsdevAliases(t, aliases, func() {
		llama := Entity{Ref: EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}}
		glm := Entity{Ref: EntityRef{Family: "glm", Version: "4.6"}}
		meta := []EntityMetadata{{MetadataID: "meta/llama-3.3-70b-instruct", Name: "AliasWins"}}

		out := AttachEntityMetadata([]Entity{llama, glm}, meta)
		// out[0]=llama, out[1]=glm (no standalones expected).
		if len(out) != 2 {
			t.Fatalf("expected 2 entities (no standalone); got %d", len(out))
		}
		if out[0].Metadata != nil {
			t.Errorf("mechanical target (llama) wrongly received metadata under an alias override: %+v", out[0].Metadata)
		}
		if out[1].Metadata == nil || out[1].Metadata.Name != "AliasWins" {
			t.Errorf("alias target (glm) did not receive metadata: %+v", out[1].Metadata)
		}
	})
}

// TestParseModelsdevAliases_Corrupt: malformed JSON yields an actionable error (the
// codegen/test seam), never a panic.
func TestParseModelsdevAliases_Corrupt(t *testing.T) {
	if _, err := parseModelsdevAliases([]byte("{ this is not json")); err == nil {
		t.Error("parseModelsdevAliases on corrupt JSON returned nil error; want an actionable error")
	}
}

// TestLoadModelsdevAliases_NeverNil: the runtime loader always returns a non-nil map
// (graceful-degrade), so metadataAliasRef never dereferences nil even when the file is
// missing or malformed.
func TestLoadModelsdevAliases_NeverNil(t *testing.T) {
	if loadModelsdevAliases() == nil {
		t.Error("loadModelsdevAliases() returned a nil map; graceful-degrade requires a non-nil (possibly empty) map")
	}
}

// TestParseModelsdevAliases_LowercasesKeys: MetadataID lookups are case-insensitive,
// so keys are folded to lowercase at load.
func TestParseModelsdevAliases_LowercasesKeys(t *testing.T) {
	raw := []byte(`{"schema_version":1,"aliases":{"Lab/Mixed-Case":{"family":"glm","version":"4.6"}}}`)
	m, err := parseModelsdevAliases(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := m["lab/mixed-case"]; !ok {
		t.Errorf("alias key was not lowercased; got keys %v", m)
	}
}

// TestRegistry_AttachesBakedMetadata pins the registry hook: with baked metadata
// injected, a catalog entity gains the matching Metadata and a family-absent metadata
// row is folded in as a standalone entity — all via the memoized index.
func TestRegistry_AttachesBakedMetadata(t *testing.T) {
	model := syntheticLlamaModel("70b") // key llama@3.3#70b{instruct}
	meta := []EntityMetadata{
		{MetadataID: "meta/llama-3.3-70b-instruct", Name: "LlamaMeta"},
		{MetadataID: "somelabxyz/frobnik-9-42b", Name: "Frob"}, // family absent -> standalone
	}
	withMetadataRegistry(t, []ModelInfo{model}, meta, func() {
		// The catalog entity gains its metadata.
		e, ok := EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if !ok {
			t.Fatal("EntityByTuple(llama 70b instruct) miss; want hit")
		}
		if e.Metadata == nil || e.Metadata.Name != "LlamaMeta" {
			t.Errorf("catalog entity did not gain baked metadata: %+v", e.Metadata)
		}

		// The family-absent metadata is folded in as a standalone entity.
		s, ok := EntityByTuple(Family("frobnik"), "", "9", "42b")
		if !ok {
			t.Fatal("frobnik standalone not present in the index; the registry hook did not synthesize it")
		}
		if s.Metadata == nil || s.Metadata.MetadataID != "somelabxyz/frobnik-9-42b" {
			t.Errorf("standalone missing metadata: %+v", s.Metadata)
		}
		if len(s.Instances) != 0 {
			t.Errorf("standalone must have empty Instances; got %d", len(s.Instances))
		}
		if len(s.Sources) != 1 || s.Sources[0] != DataSourceModelsDev {
			t.Errorf("standalone Sources = %v, want [%s]", s.Sources, DataSourceModelsDev)
		}

		// The standalone participates in the full listing deterministically.
		all := Entities()
		if len(all) != 2 {
			t.Fatalf("Entities() = %d, want 2 (catalog entity + standalone)", len(all))
		}
	})
}

// TestRegistry_MetadataDeepCopyIsolatesCache is the VERIFY-ONLY guard that cloneEntity's
// Metadata deep-copy (landed earlier) interacts correctly with the join: mutating a
// returned entity's Metadata must never corrupt the memoized registry cache, so a
// subsequent read returns the pristine baked metadata.
func TestRegistry_MetadataDeepCopyIsolatesCache(t *testing.T) {
	model := syntheticLlamaModel("70b")
	meta := []EntityMetadata{{
		MetadataID: "meta/llama-3.3-70b-instruct",
		Name:       "Original",
		Links:      []ModelLink{{Label: "card", URL: "u"}},
	}}
	withMetadataRegistry(t, []ModelInfo{model}, meta, func() {
		e1, ok := EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if !ok || e1.Metadata == nil {
			t.Fatalf("entity or its metadata missing: ok=%v meta=%+v", ok, e1.Metadata)
		}
		// Mutate the returned copy's metadata (scalar + a Links element).
		e1.Metadata.Name = "Mutated"
		e1.Metadata.Links[0].Label = "changed"

		// A fresh read must be unaffected — the deep-copy isolates the registry cache.
		e2, _ := EntityByTuple(Family("llama"), "", "3.3", "70b", "instruct")
		if e2.Metadata.Name != "Original" || e2.Metadata.Links[0].Label != "card" {
			t.Errorf("mutating a returned entity's Metadata corrupted the registry cache: %+v", e2.Metadata)
		}
	})
}

// TestJoin_AliasNoFallbackToMechanical pins curated > mechanical at its hardest case:
// when an alias EXISTS but its target entity is ABSENT, the row must NOT fall back to a
// mechanical match against a DIFFERENT present entity — it flows through the two-tier
// miss on the curated family (here: unlinked, because the curated family is present).
// A mechanical fallback would wrongly attach the row to the llama entity, turning this
// test RED — which is the exact wrong-attach an alias exists to prevent.
func TestJoin_AliasNoFallbackToMechanical(t *testing.T) {
	// mechanical: "meta/llama-3.3-70b-instruct" -> llama@3.3#70b{instruct} (PRESENT).
	// alias re-points it onto glm@4.6, but only glm@4.5 is present (alias target ABSENT).
	aliases := map[string]modelsdevAlias{
		"meta/llama-3.3-70b-instruct": {Family: "glm", Version: "4.6"},
	}
	withSyntheticModelsdevAliases(t, aliases, func() {
		mechTarget := Entity{Ref: EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}}
		glmOther := Entity{Ref: EntityRef{Family: "glm", Version: "4.5"}} // glm family present, NOT the alias target
		meta := []EntityMetadata{{MetadataID: "meta/llama-3.3-70b-instruct", Name: "X"}}

		attached, unlinked, standalone := JoinEntityMetadata([]Entity{mechTarget, glmOther}, meta)
		if attached[0].Metadata != nil {
			t.Errorf("alias override must NOT fall back to the mechanical (llama) entity; got %+v", attached[0].Metadata)
		}
		if attached[1].Metadata != nil {
			t.Errorf("glm@4.5 is not the alias target (glm@4.6) and must not be attached; got %+v", attached[1].Metadata)
		}
		if len(standalone) != 0 {
			t.Errorf("glm family is present, so an absent alias target is a disagreement (unlinked), not a standalone; got %d standalones", len(standalone))
		}
		if len(unlinked) != 1 || unlinked[0] != "meta/llama-3.3-70b-instruct" {
			t.Fatalf("absent alias target must flow to unlinked; got %v", unlinked)
		}
	})
}

// TestRegistry_StandaloneOrderingAscending pins the deterministic standalone ordering
// in the registry attach path: two family-absent metadata rows supplied in REVERSE
// MetadataID order must appear in Entities() in ASCENDING MetadataID order. Removing
// the sort.Slice in attachBakedMetadataToIndex leaves them in meta-slice (reverse)
// order, turning this test RED.
func TestRegistry_StandaloneOrderingAscending(t *testing.T) {
	model := syntheticLlamaModel("70b")
	// Two family-absent metadata rows, supplied in DESCENDING MetadataID order.
	meta := []EntityMetadata{
		{MetadataID: "zlab/frobnik-9-42b", Name: "Z"},
		{MetadataID: "alab/wibble-3-7b", Name: "A"},
	}
	withMetadataRegistry(t, []ModelInfo{model}, meta, func() {
		var order []MetadataID
		for _, e := range Entities() {
			if len(e.Instances) == 0 && e.Metadata != nil {
				order = append(order, e.Metadata.MetadataID)
			}
		}
		want := []MetadataID{"alab/wibble-3-7b", "zlab/frobnik-9-42b"}
		if len(order) != len(want) {
			t.Fatalf("standalone count = %d (%v), want %d distinct family-absent standalones", len(order), order, len(want))
		}
		for i := range want {
			if order[i] != want[i] {
				t.Fatalf("standalone order = %v, want ascending %v (registry sort dropped?)", order, want)
			}
		}
	})
}

// TestMetadataCensus_SynthesizedStandalonesPinned is the census guard: it pins the SET
// of synthesized metadata-only standalone entities produced by the production join over
// the real baked metadata to a CURATED expected list. The list is a LITERAL — updated
// consciously on each models.dev refresh — NOT derived from production output, so a
// decomposition or presence-gate regression that invents a standalone for a family the
// catalog actually serves fails here loudly.
//
// Today the only legitimate synthesized standalones are the 4 deepreinforce/ornith rows:
// ornith has ZERO serving entries in the vendored catalog, so its metadata is a genuine
// catalog-absent entity. The four join-failures this fix addressed
// (gemini-omni-flash-preview, gpt-realtime-2.1, laguna-xs-2.1, nemotron-super-49b-v1.5)
// must NOT appear: three route to the unlinked report and one attaches via a curated
// alias.
func TestMetadataCensus_SynthesizedStandalonesPinned(t *testing.T) {
	// CURATED expected set. Update this ONLY with a deliberate curation decision.
	want := map[string]bool{
		"ornith@1.0#9b":   true,
		"ornith@1.0#31b":  true,
		"ornith@1.0#35b":  true,
		"ornith@1.0#397b": true,
	}

	// Reconstruct the served entity set (the join's left input before standalone append)
	// by excluding the metadata-only standalones the registry already folded in, then run
	// the PRODUCTION join over the baked metadata to recompute the standalone set.
	var served []Entity
	for _, e := range Entities() {
		if len(e.Instances) > 0 {
			served = append(served, e)
		}
	}
	_, _, standalone := JoinEntityMetadata(served, staticEntityMetadata())

	got := make(map[string]bool, len(standalone))
	for _, s := range standalone {
		got[s.Ref.String()] = true
	}

	for k := range got {
		if !want[k] {
			t.Errorf(
				"UNEXPECTED synthesized standalone %q\n"+
					"  What: the join invented a metadata-only entity not in the curated expected set\n"+
					"  Why: the decomposition/presence gate likely over-captured a compound family the\n"+
					"       catalog actually serves (the exact class this guard exists to catch)\n"+
					"  How to fix: add a parse/data/modelsdev_aliases.json entry (verify the target is a\n"+
					"       DISTINCT entity — no collision), or correct the decomposition; only widen the\n"+
					"       expected set for a genuine new catalog-absent lab (0 serving entries)",
				k,
			)
		}
	}
	for k := range want {
		if !got[k] {
			t.Errorf(
				"MISSING expected synthesized standalone %q\n"+
					"  What: a curated catalog-absent standalone is no longer synthesized\n"+
					"  Why: the metadata row may have gained serving entries, been re-decomposed, or been\n"+
					"       aliased away\n"+
					"  How to fix: if intended, remove %q from the curated expected set in this test",
				k, k,
			)
		}
	}
}

// TestMetadataAlias_NemotronSuper49bAttaches asserts the ONE clean curated alias lands:
// nvidia/llama-3.3-nemotron-super-49b-v1.5's metadata attaches to the distinct
// provider-backed entity nemotron/v1.5@3.3#49b (deepinfra/kilo/openrouter serve it),
// rather than being synthesized as a spurious standalone, and that its metadata CONTENT
// travels with it. The alias carries param_size "49b" because the full-bulk size re-key
// gave the served entity a #49b segment; the alias target was re-pinned to match. (This
// upstream row carries a description + name but no benchmarks/license, so the content
// check is on Description — the field it actually populates.)
func TestMetadataAlias_NemotronSuper49bAttaches(t *testing.T) {
	e, ok := EntityByTuple(FamilyNemotron, "v1.5", "3.3", "49b")
	if !ok {
		t.Fatalf("entity nemotron/v1.5@3.3#49b not found in the registry; the alias target must be a real served entity")
	}
	if e.Metadata == nil {
		t.Fatalf("entity %q has no attached Metadata; the curated alias for nvidia/llama-3.3-nemotron-super-49b-v1.5 did not land", e.Ref.String())
	}
	if e.Metadata.MetadataID != "nvidia/llama-3.3-nemotron-super-49b-v1.5" {
		t.Errorf("entity %q Metadata.MetadataID = %q, want %q",
			e.Ref.String(), e.Metadata.MetadataID, "nvidia/llama-3.3-nemotron-super-49b-v1.5")
	}
	if e.Metadata.Description == "" {
		t.Errorf("entity %q attached Metadata carries an empty Description; the metadata content did not travel with the alias", e.Ref.String())
	}
}
