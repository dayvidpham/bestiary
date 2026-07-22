package bestiary_test

// Embedded JSON case corpora for the entity-package table-driven tests, plus the
// shared input/expected types and corpus-runner helpers. See TESTING.md for the
// corpus standard: each corpus is guarded by an exact case-count control, a
// value-based coverage assertion, and testcase.Corpus.Validate non-vacuity.

import (
	_ "embed"
	"reflect"
	"testing"

	"github.com/dayvidpham/bestiary"
	"github.com/dayvidpham/bestiary/testcase"
)

// ---- EntityRef.String() grammar corpora (input: EntityRef fields) ---------

//go:embed testdata/entity/entity_ref_string_contract_corpus.json
var entRefStringContractCorpusJSON []byte

//go:embed testdata/entity/entity_ref_paramsize_distinct_corpus.json
var entRefParamSizeDistinctCorpusJSON []byte

//go:embed testdata/entity/entity_ref_string_paramsize_grammar_corpus.json
var entRefStringParamSizeGrammarCorpusJSON []byte

// ---- DerivationKind text round-trip corpus ---------------------------------

//go:embed testdata/entity/derivation_kind_text_roundtrip_corpus.json
var entDerivationKindTextRoundTripCorpusJSON []byte

// ---- redundant-modifier suppression per-entry fence corpus -----------------

//go:embed testdata/entity/suppression_fence_corpus.json
var suppressionFenceCorpusJSON []byte

// ---- laguna curated-variant three-way split corpus --------------------------

//go:embed testdata/entity/laguna_three_way_split_corpus.json
var entLagunaThreeWaySplitCorpusJSON []byte

// entLagunaInput is the (variant, version) leg of the split probe; the family
// (laguna) is invariant across the corpus, matching the original inline table.
type entLagunaInput struct {
	Variant string `json:"variant"`
	Version string `json:"version"`
}

// entLagunaExpected is the entity key the tuple must render to plus the lab
// metadata row that must attach to THAT entity (the collision-free attach).
type entLagunaExpected struct {
	WantKey    string `json:"want_key"`
	MetadataID string `json:"metadata_id"`
}

// ---- Series/Release display-rendering corpus --------------------------------

//go:embed testdata/entity/series_release_string_corpus.json
var entSeriesReleaseStringCorpusJSON []byte

// entReleaseInput is the JSON-decodable projection of a bestiary.Release: the series
// (family + generation) plus the release name. Empty segments are the load-bearing
// cases, so they are plain strings rather than optional fields.
type entReleaseInput struct {
	Family     string `json:"family"`
	Generation string `json:"generation"`
	Name       string `json:"name"`
}

// ---- llama-4 unified @4 entity membership corpus ---------------------------

//go:embed testdata/entity/llama4_version_pins_corpus.json
var entLlama4VersionPinsCorpusJSON []byte

// entRefInput is the JSON-decodable projection of bestiary.EntityRef's fields
// driven through EntityRef.String() by the grammar corpora above.
type entRefInput struct {
	Family    string   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size"`
	Modifier  []string `json:"modifier"`
}

// toEntityRef builds the bestiary.EntityRef the corpus case describes.
func (in entRefInput) toEntityRef() bestiary.EntityRef {
	return bestiary.EntityRef{
		Family:    bestiary.Family(in.Family),
		Variant:   in.Variant,
		Version:   in.Version,
		ParamSize: in.ParamSize,
		Modifier:  in.Modifier,
	}
}

// entRefProbe pins one load-bearing EntityRef input to its expected rendered
// key, for the value-based coverage guard (a count-preserving case swap
// reddens here even though the exact-count control cannot see it).
type entRefProbe struct {
	input entRefInput
	want  string
}

// entLlama4Input is the (variant, paramSize) leg of an EntityByTuple probe; the
// family ("llama"), version ("4"), and identity modifier ("instruct") are
// invariant across both corpus cases, matching the original inline table.
type entLlama4Input struct {
	Variant   string `json:"variant"`
	ParamSize string `json:"param_size"`
}

// entLlama4Expected is the unified entity's rendered key plus the exact
// instance IDs that must be members of it.
type entLlama4Expected struct {
	WantKey string   `json:"want_key"`
	IDs     []string `json:"ids"`
}

// loadEntRefCorpus is the EntityRef.String() specialization of loadParseCorpus
// (defined in fixtures_parse_test.go, same package).
func loadEntRefCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[entRefInput, string] {
	t.Helper()
	return loadParseCorpus[entRefInput, string](t, data, wantN)
}

// runEntRefStringCorpus drives bestiary.EntityRef.String() over every case.
// The original inline tables used plain t.Run with no t.Parallel(), so the
// migrated runner preserves that shape exactly.
func runEntRefStringCorpus(t *testing.T, corpus testcase.Corpus[entRefInput, string]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := c.Input.toEntityRef().String(); got != c.Expected {
				t.Errorf("EntityRef.String() = %q, want %q", got, c.Expected)
			}
		})
	}
}

// requireEntRefStringCoverage asserts each probe's input is still present in
// the corpus with its expected rendered key. entRefInput carries a []string
// field, so it is not map-keyable; coverage is checked by linear scan with
// reflect.DeepEqual instead of the map-based helpers in fixtures_parse_test.go.
func requireEntRefStringCoverage(t *testing.T, corpus testcase.Corpus[entRefInput, string], probes []entRefProbe) {
	t.Helper()
	for _, p := range probes {
		found := false
		for _, c := range corpus.Cases {
			if reflect.DeepEqual(c.Input, p.input) {
				found = true
				if c.Expected != p.want {
					t.Errorf("value coverage: case %+v has expected %q, want %q", p.input, c.Expected, p.want)
				}
				break
			}
		}
		if !found {
			t.Errorf("value coverage lost: case for input %+v is missing", p.input)
		}
	}
}

// requireEntityProjections is the PROJECTION PROBE shared by the two corpora whose
// expected value is an ENTITY rather than a parse result (the llama-4 version pins and
// the laguna three-way split). Pinning only the rendered key would leave the derived
// read projections unchecked, so this probe drives the SAME entity through them:
//
//   - Nomina() must carry exactly one PREFERRED canonical nomen, and (with an empty
//     suppression seed) its value must be the entity key — the naming layer and the key
//     cannot silently disagree;
//   - every instance ID must also appear as an ADMITTED provider-id nomen, so a
//     membership pin is not satisfied by an entity whose nomina lost the spelling;
//   - Sources must attest models.dev — the derived provenance projection of the
//     entity<->source join.
func requireEntityProjections(t *testing.T, ent bestiary.Entity, wantKey string) {
	t.Helper()
	preferred := 0
	admitted := map[string]bool{}
	for _, n := range ent.Nomina() {
		switch {
		case n.Scheme == bestiary.NomenSchemeCanonical && n.Status == bestiary.AcceptabilityPreferred:
			preferred++
			if n.Value != wantKey {
				t.Errorf("projection: preferred canonical nomen = %q, want the entity key %q", n.Value, wantKey)
			}
		case n.Scheme == bestiary.NomenSchemeProviderID:
			admitted[n.Value] = true
		}
		if got := n.ResolvesTo.String(); got != wantKey {
			t.Errorf("projection: nomen %q resolves to %q, want %q", n.Value, got, wantKey)
		}
	}
	if preferred != 1 {
		t.Errorf("projection: %q has %d preferred canonical nomina, want exactly 1", wantKey, preferred)
	}
	for _, inst := range ent.Instances {
		if !admitted[string(inst.ID)] {
			t.Errorf("projection: instance spelling %q of %q has no admitted provider-id nomen", inst.ID, wantKey)
		}
	}
	attested := false
	for _, s := range ent.Sources {
		if s == bestiary.DataSourceModelsDev {
			attested = true
		}
	}
	if !attested {
		t.Errorf("projection: %q Sources = %v, want the models.dev attestation", wantKey, ent.Sources)
	}
}
