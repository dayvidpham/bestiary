package bestiary

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	"github.com/dayvidpham/bestiary/testcase/assert"
)

// GH#43 — parser conformance sweep for the top-traffic labs.
//
// This file carries TWO guards, and they are deliberately different in kind:
//
//  1. TestParserConformance_CitedStrings drives the AUTHORED corpus
//     (testdata/parse/parser_conformance_corpus.json): every string the issue cites,
//     plus the measured witnesses and the conforming CONTROLS that isolate each
//     defect's cause, each pinned to the entity key the production path produces
//     for it today. It is a fixed, hand-authored case list, so it is a corpus.
//
//  2. TestParserConformance_TokenCensus is the SWEEP itself: a computed census over the
//     whole vendored catalog. Its rows are derived from live data, not authored,
//     so by the rule at the top of TESTING.md it stays inline Go.
//
// NEITHER changes an entity key. The sweep only reports: it drives the production
// parser and records what that parser already does.
//
// The production path is never re-implemented here. A SERVING id is keyed by the
// registry's own entity index (the key an Entity actually carries), and a LAB id or
// an OFF-CATALOG usage spelling by metadataEntityRef — the same function the
// models.dev metadata join uses.

// ---------------------------------------------------------------------------
// Closed sets
// ---------------------------------------------------------------------------

// conformanceKind is the closed set of how a corpus string reaches the production
// parser. The three members take genuinely different code paths, so a case must
// say which one it pins.
type conformanceKind string

const (
	// conformanceServing is a models.dev PROVIDER-view id: a served row that the codegen
	// pipeline turns into a ProviderInstance on an Entity. Its key is read back
	// off the registry's entity index.
	conformanceServing conformanceKind = "serving-id"
	// conformanceLab is a models.dev MODELS-view (metadata) id, decomposed by the
	// metadata join's own id-driven path.
	conformanceLab conformanceKind = "lab-id"
	// conformanceOffCatalog is a spelling that appears in USAGE data but is absent from
	// the vendored catalog. It has no entity of its own; it is driven through the
	// same id-driven decomposition to record what key it would land on.
	conformanceOffCatalog conformanceKind = "off-catalog-id"
)

// IsValid reports whether the kind is one of the known members.
func (k conformanceKind) IsValid() bool {
	switch k {
	case conformanceServing, conformanceLab, conformanceOffCatalog:
		return true
	}
	return false
}

// conformanceVerdict is the closed set of verdicts the sweep may record for a
// measured key.
type conformanceVerdict string

const (
	// conformanceConforming: the measured key is CORRECT. Used both for a cited defect
	// that is refuted at this tip and for a control whose only job is to isolate
	// a neighbouring defect's cause.
	conformanceConforming conformanceVerdict = "conforming"
	// conformanceDefect: the measured key is WRONG, and WantKey states the correct
	// destination.
	conformanceDefect conformanceVerdict = "defect"
	// conformanceUndecided: the measured key is wrong OR right, and the sweep must not
	// say which — the destination is a curation ruling. WantKey carries the
	// EXPECTED_TBD marker and the fix issue frames the decision.
	conformanceUndecided conformanceVerdict = "undecided"
	// conformanceExcluded: the string is NOT a model and does not belong in the
	// keyspace at all (a harness, an agent, a product passthrough). The parser still
	// produces a key for it TODAY, which Key pins as the witness, but the UAT ruling
	// removes it, so WantKey carries the EXCLUDED marker instead of a destination.
	conformanceExcluded conformanceVerdict = "excluded"
)

// IsValid reports whether the verdict is one of the known members.
func (c conformanceVerdict) IsValid() bool {
	switch c {
	case conformanceConforming, conformanceDefect, conformanceUndecided, conformanceExcluded:
		return true
	}
	return false
}

// conformanceExpectedTBD is the marker an undecided case carries in WantKey. It is a
// literal, not an empty string, so an undecided case can never be confused with a
// case whose author forgot to fill the field in.
const conformanceExpectedTBD = "EXPECTED_TBD"

// conformanceExcludedMarker is the marker an excluded case carries in WantKey. Like
// EXPECTED_TBD it is a distinct literal, so an excluded (non-model) case is never
// confused with a defect whose want-key was left blank.
const conformanceExcludedMarker = "EXCLUDED"

// conformanceCaseCount is the EXACT authored case count. An exact control (not a floor)
// catches a drop as well as a stray add.
const conformanceCaseCount = 52

// conformanceCatalogPath is the vendored catalog snapshot every figure in this file and
// in the report is measured against. conformanceReportPath is the prose those figures
// live in: a red test sends the reader there, so the path must resolve.
const (
	conformanceCatalogPath = "parse/data/modelsdev/catalog.json"
	conformanceReportPath  = "docs/research/parser-conformance-sweep.md"
)

// conformanceClassCount is the number of defect classes this corpus records: GH#43's
// original six, plus the two UAT-raised follow-up findings — class 7 (the GLM
// vision 'v' suffix read as a variant instead of a {vision} modifier) and class 8
// (flash/flashx must be a distinct-weight variant uniformly). Every one must be
// covered by at least one case.
const conformanceClassCount = 8

type conformanceInput struct {
	Raw  string          `json:"raw"`
	Kind conformanceKind `json:"kind"`
}

type conformanceExpected struct {
	// Key is the entity key the production path produces for Raw TODAY.
	Key string `json:"key"`
	// Conformance is the sweep's verdict on Key.
	Conformance conformanceVerdict `json:"conformance"`
	// DefectClass is the GH#43 class (1..6) this case belongs to. A conforming
	// control still names the class it controls.
	DefectClass int `json:"defect_class"`
	// WantKey is the CORRECT destination: equal to Key when conforming, the
	// corrected key when a defect, conformanceExpectedTBD when undecided.
	WantKey string `json:"want_key"`
}

// conformanceIndex holds the three catalog-derived sets the corpus needs: the registry's
// own serving-id -> entity-key map, and the id sets of the two catalog views. The
// id sets exist so a case's PREMISE is checked, not only its key: a case that says
// "this is a lab row" or "this string is absent from the catalog" must go red when
// that stops being true, or its verdict rots green.
type conformanceIndex struct {
	// servingKeys maps a LOWERCASED served id to the key of the Entity that
	// carries it. Lowercased because the registry's own instance index is
	// case-insensitive.
	servingKeys map[string]string
	// servedIDs and labIDs hold every id of the provider (served) view and the
	// models (lab) view, lowercased, for the premise checks.
	servedIDs map[string]bool
	labIDs    map[string]bool
}

// conformanceLoadCatalog reads and parses the vendored catalog snapshot. Both tests in
// this file measure against the SAME snapshot, so they load it the same way.
func conformanceLoadCatalog(t *testing.T) Catalog {
	t.Helper()
	raw, err := os.ReadFile(conformanceCatalogPath)
	if err != nil {
		t.Fatalf("read the vendored catalog snapshot: %v", err)
	}
	cat, err := ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("parse the vendored catalog snapshot: %v", err)
	}
	return cat
}

// conformanceBuildIndex builds the serving-id -> entity-key map from the registry's OWN
// entities, so the key a case asserts is the key the entity actually carries, and
// the two view id sets from the catalog snapshot.
func conformanceBuildIndex(cat Catalog) conformanceIndex {
	idx := conformanceIndex{
		servingKeys: make(map[string]string),
		servedIDs:   make(map[string]bool),
		labIDs:      make(map[string]bool),
	}
	for _, e := range Entities() {
		key := e.Ref.String()
		for _, inst := range e.Instances {
			idx.servingKeys[strings.ToLower(string(inst.ID))] = key
		}
	}
	for _, m := range cat.Models {
		idx.servedIDs[strings.ToLower(string(m.ID))] = true
	}
	for _, md := range cat.Metadata {
		idx.labIDs[strings.ToLower(string(md.MetadataID))] = true
	}
	return idx
}

// conformanceProductionKey drives one corpus string through the PRODUCTION path for its
// kind and returns the entity key. It never re-implements a decomposition.
//
// Every kind is premise-guarded. The three kinds make three DIFFERENT factual
// claims about the catalog, and each claim is load-bearing for a verdict in the
// report, so each one fatals with the same message shape when it stops holding.
func conformanceProductionKey(t *testing.T, in conformanceInput, idx conformanceIndex) string {
	t.Helper()
	low := strings.ToLower(in.Raw)
	switch in.Kind {
	case conformanceServing:
		key, ok := idx.servingKeys[low]
		if !ok {
			t.Fatalf("serving id %q holds no entity instance in the registry\n"+
				"  What: the corpus claims this is a served catalog row, but no Entity carries it\n"+
				"  How to fix: re-measure against %s; if the row"+
				" is gone upstream, re-classify the case as off-catalog-id", in.Raw, conformanceCatalogPath)
		}
		return key
	case conformanceLab:
		if !idx.labIDs[low] {
			t.Fatalf("lab id %q is absent from the models (metadata) view\n"+
				"  What: the corpus claims this is a lab row, but the catalog snapshot"+
				" carries no metadata row with this id\n"+
				"  Why it matters: metadataEntityRef answers for ANY string, so without this"+
				" check the case would still produce a key and the premise would rot green\n"+
				"  How to fix: re-measure against %s; if the row"+
				" is gone upstream, re-classify the case as off-catalog-id", in.Raw, conformanceCatalogPath)
		}
		return metadataEntityRef(MetadataID(in.Raw)).String()
	case conformanceOffCatalog:
		if idx.servedIDs[low] || idx.labIDs[low] {
			view := "models (metadata)"
			if idx.servedIDs[low] {
				view = "provider (served)"
			}
			t.Fatalf("off-catalog id %q IS present in the %s view\n"+
				"  What: the corpus claims this spelling appears only in USAGE data and is"+
				" absent from the vendored catalog, and it no longer is\n"+
				"  Why it matters: the class 6 refutation rests on this premise. A string that"+
				" became a catalog row must be measured on the path its view uses, not on the"+
				" id-only decomposition, or the sweep reports agreement it did not measure\n"+
				"  How to fix: re-classify the case as serving-id or lab-id and re-measure the"+
				" key, then re-check the class 6 verdict in %s",
				in.Raw, view, conformanceReportPath)
		}
		return metadataEntityRef(MetadataID(in.Raw)).String()
	default:
		t.Fatalf("case input kind %q is not a member of the closed set", in.Kind)
		return ""
	}
}

func TestParserConformance_CitedStrings(t *testing.T) {
	corpus, err := testcase.LoadCorpus[conformanceInput, conformanceExpected](conformanceCorpusJSON)
	if err != nil {
		t.Fatalf("load GH#43 conformance corpus: %v", err)
	}
	if got := len(corpus.Cases); got != conformanceCaseCount {
		t.Fatalf("GH#43 conformance corpus has %d cases, want exactly %d", got, conformanceCaseCount)
	}
	// Non-vacuity: classification + provenance + mutation on every case.
	assert.RequireValid(t, corpus)

	idx := conformanceBuildIndex(conformanceLoadCatalog(t))

	classSeen := map[int]int{}
	classDefects := map[int]int{}
	rawSeen := map[string]bool{}
	for _, c := range corpus.Cases {
		if !c.Input.Kind.IsValid() {
			t.Errorf("case %q: kind %q is outside the closed set", c.Name, c.Input.Kind)
			continue
		}
		if !c.Expected.Conformance.IsValid() {
			t.Errorf("case %q: conformance %q is outside the closed set", c.Name, c.Expected.Conformance)
			continue
		}
		if c.Expected.DefectClass < 1 || c.Expected.DefectClass > conformanceClassCount {
			t.Errorf("case %q: defect_class %d is not one of the %d cited classes",
				c.Name, c.Expected.DefectClass, conformanceClassCount)
			continue
		}
		if rawSeen[string(c.Input.Kind)+"|"+c.Input.Raw] {
			t.Errorf("case %q: duplicate (kind, raw) pair %q", c.Name, c.Input.Raw)
		}
		rawSeen[string(c.Input.Kind)+"|"+c.Input.Raw] = true
		classSeen[c.Expected.DefectClass]++
		if c.Expected.Conformance == conformanceDefect {
			classDefects[c.Expected.DefectClass]++
		}

		// The verdict must be internally consistent with WantKey. This is what
		// stops a case from being a decoration: a "defect" that wants the key it
		// already has says nothing.
		switch c.Expected.Conformance {
		case conformanceConforming:
			if c.Expected.WantKey != c.Expected.Key {
				t.Errorf("case %q: conforming case wants %q but pins key %q; a conforming"+
					" case must want the key it measures", c.Name, c.Expected.WantKey, c.Expected.Key)
			}
		case conformanceDefect:
			if c.Expected.WantKey == "" || c.Expected.WantKey == c.Expected.Key {
				t.Errorf("case %q: defect case must state a want_key DIFFERENT from the"+
					" measured key %q, got %q", c.Name, c.Expected.Key, c.Expected.WantKey)
			}
		case conformanceUndecided:
			if c.Expected.WantKey != conformanceExpectedTBD {
				t.Errorf("case %q: undecided case must carry want_key %q, got %q",
					c.Name, conformanceExpectedTBD, c.Expected.WantKey)
			}
		case conformanceExcluded:
			if c.Expected.WantKey != conformanceExcludedMarker {
				t.Errorf("case %q: excluded case must carry want_key %q, got %q",
					c.Name, conformanceExcludedMarker, c.Expected.WantKey)
			}
		}

		t.Run(c.Name, func(t *testing.T) {
			got := conformanceProductionKey(t, c.Input, idx)
			if got != c.Expected.Key {
				t.Errorf("%s %q: production key = %q, corpus pins %q\n"+
					"  What: the sweep's measured key for this string MOVED\n"+
					"  Why it matters: this corpus is the GH#43 evidence record; a moved key"+
					" means a defect was fixed, re-shaped, or newly introduced\n"+
					"  How to fix: re-measure the string, then update BOTH the key and the"+
					" conformance verdict in testdata/parse/parser_conformance_corpus.json",
					c.Input.Kind, c.Input.Raw, got, c.Expected.Key)
			}
		})
	}

	// Coverage: every cited class carries at least one case, and every class that
	// the sweep CONFIRMS carries at least one control or refutation beside it, so
	// no class is represented by a lone unfalsifiable row.
	for class := 1; class <= conformanceClassCount; class++ {
		if classSeen[class] == 0 {
			t.Errorf("defect class %d has no case; every cited class must be confirmed or refuted", class)
		}
		if classSeen[class] < 2 {
			t.Errorf("defect class %d has %d case(s); a class needs a witness AND a"+
				" neighbouring control or refutation to be falsifiable", class, classSeen[class])
		}
	}

	// Value coverage: an exact count cannot see a count-preserving swap. These are
	// the load-bearing strings the issue itself cites; they must be present BY VALUE.
	for _, raw := range []string{
		"deepseek/deepseek-v3.2",        // class 1
		"deepseek-v3.1-nex-n1",          // class 1
		"deepseek/deepseek-ocr-2",       // class 2
		"anthropic/claude-fable-5",      // class 2, refuted
		"z-ai-glm-5v-turbo",             // class 3
		"deepseek/deepseek-r1-turbo",    // class 4
		"poetools/claude-code",          // class 4
		"deepseek/deepseek-v4-pro-0813", // class 5
		"anthropic/claude-4.6-sonnet",   // class 6
		"openai/gpt-5-mini-2025-08-07",  // class 6
		"moonshotai/kimi-k2.5-0127",     // class 6
		"anthropic--claude-4.6-sonnet",  // class 6
		"glm-4.5v",                      // class 7, vision suffix
		"glm-5v-turbo",                  // class 7, vision suffix, turbo off-key
		"gemini-2.5-flash",              // class 8, flash variant control
		"qwen3-coder-flash",             // class 8, flash dropped, coder collision
		"glm-4.6v-flash",                // class 8, vision-flash inversion
	} {
		found := false
		for _, c := range corpus.Cases {
			if c.Input.Raw == raw {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cited string %q is missing from the corpus by value", raw)
		}
	}

	// The sweep must CONFIRM at least one defect. A corpus of nothing but
	// conforming controls would pass every check above and report nothing.
	total := 0
	for _, n := range classDefects {
		total += n
	}
	if total == 0 {
		t.Error("the corpus confirms no defect at all; GH#43 exists because defects were observed")
	}
}

// ---------------------------------------------------------------------------
// The sweep: the token census over the whole vendored catalog
// ---------------------------------------------------------------------------

// conformanceLabToken is one seed lab's token group. A lab whose products carry several
// unrelated stems (Mistral, Tencent) is ONE group, so the census counts labs, not
// spellings.
type conformanceLabToken struct {
	Name   string
	Tokens []string
	// Want is the MEASURED match count against the committed catalog snapshot at
	// parse/data/modelsdev/catalog.json. It is snapshot-relative by construction:
	// a re-vendored catalog moves it, and the sweep must then be re-measured.
	Want int
}

// conformanceSeedTokens is the GH#43 seed lab list, in the PRECEDENCE order the census
// uses to attribute a record to exactly ONE lab. Attribution is by first match in
// this order, so the per-lab counts are disjoint and their sum is the distinct
// matched-record total — the accounting the issue's acceptance clause requires.
var conformanceSeedTokens = []conformanceLabToken{
	{Name: "deepseek", Tokens: []string{"deepseek"}, Want: 211},
	{Name: "kimi", Tokens: []string{"kimi"}, Want: 145},
	{Name: "glm", Tokens: []string{"glm"}, Want: 277},
	{Name: "minimax", Tokens: []string{"minimax"}, Want: 96},
	{Name: "mimo", Tokens: []string{"mimo"}, Want: 55},
	{Name: "qwen", Tokens: []string{"qwen"}, Want: 552},
	{Name: "claude", Tokens: []string{"claude"}, Want: 318},
	{Name: "gpt", Tokens: []string{"gpt"}, Want: 415},
	{Name: "llama", Tokens: []string{"llama"}, Want: 195},
	{Name: "muse", Tokens: []string{"muse"}, Want: 22},
	{Name: "laguna", Tokens: []string{"laguna"}, Want: 14},
	{Name: "gemini", Tokens: []string{"gemini"}, Want: 224},
	{Name: "gemma", Tokens: []string{"gemma"}, Want: 93},
	{Name: "grok", Tokens: []string{"grok"}, Want: 122},
	{Name: "nemotron", Tokens: []string{"nemotron"}, Want: 101},
	{Name: "mistral", Tokens: []string{"mistral", "ministral", "devstral", "codestral", "magistral"}, Want: 192},
	{Name: "phi", Tokens: []string{"phi"}, Want: 12},
	{Name: "hy", Tokens: []string{"hy", "hunyuan"}, Want: 30},
	{Name: "step", Tokens: []string{"step"}, Want: 31},
}

// conformanceCensusTotal is the measured total of distinct matched catalog records. The
// sum of every conformanceLabToken.Want must equal it, and the census must match it.
const conformanceCensusTotal = 3105

// ---------------------------------------------------------------------------
// The counting rule, and the per-key record counts the report states
// ---------------------------------------------------------------------------

// THE COUNTING RULE. Everywhere the sweep, the report and the six fix issues say
// "records" for a key, the unit is:
//
//	ONE DISTINCT RAW ID STRING, WITHIN ONE CATALOG VIEW, COMPARED CASE-SENSITIVELY,
//	AMONG THE RECORDS THE SEED-TOKEN CENSUS MATCHED.
//
// Three parts of that sentence are load-bearing and each was measured:
//
//   - PER VIEW. The provider (served) view and the models (lab) view are counted
//     separately, so an id present in both counts twice. A served id is keyed by
//     the registry's entity index; a lab id by metadataEntityRef.
//   - DISTINCT ID, not row. One id served by N providers is ONE record. The
//     universe is 7,791 rows; 6,666 of them match a seed token; they collapse to
//     3,105 distinct (view, id) pairs.
//   - CASE-SENSITIVE. "tencent/hy3" and "tencent/Hy3" are two records even though
//     the registry's serving index is case-INsensitive and both reach one entity.
//
// A different unit gives a very different number for the same key: deepseek/flash
// is 58 records under this rule and 148 provider instances under a row rule. So
// the unit is named beside every figure, in the report and here.

// conformanceKeyRecords pins the record count for ONE entity key under the counting rule
// above. Every key whose count the report or a posted fix issue states appears in
// this table, so a moved count fails HERE first, naming the key.
type conformanceKeyRecords struct {
	Key  string
	Want int
	// Where names the prose the figure appears in, so a red row says what to
	// re-measure and where to correct it.
	Where string
}

// conformanceKeyRecordCounts is the pinned per-key table. It is SNAPSHOT-RELATIVE by
// construction, exactly like the per-lab census: a re-vendored catalog moves these
// counts, the test goes red, and the prose is re-measured instead of going stale.
var conformanceKeyRecordCounts = []conformanceKeyRecords{
	// Class 1, shape A: the version in the variant slot. These eight sum to 31.
	{Key: "deepseek/v3.2", Want: 12, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.2-exp", Want: 6, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.1", Want: 5, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.1-terminus", Want: 4, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.2-speciale", Want: 1, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.2-maas", Want: 1, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.2-251201", Want: 1, Where: "class 1 table; issue #48"},
	{Key: "deepseek/v3.1-maas", Want: 1, Where: "class 1 table; issue #48"},
	// Class 1, the correct sibling, and shape B. 39 + 58 = 97.
	{Key: "deepseek@3.2", Want: 1, Where: "class 1 prose; issue #48"},
	{Key: "deepseek/pro", Want: 39, Where: "class 1 shape B; issue #48"},
	{Key: "deepseek/flash", Want: 58, Where: "class 1 shape B; issue #48"},
	// Class 2.
	{Key: "deepseek-ocr@2", Want: 3, Where: "class 2 table; issue #49"},
	{Key: "claude-mythos@5", Want: 2, Where: "class 2 table; issue #49"},
	{Key: "claude/fable@5", Want: 12, Where: "class 2 prose, refuted"},
	// Class 3.
	{Key: "glm/z", Want: 3, Where: "class 3 prose; issue #50"},
	{Key: "glm/z#9b", Want: 1, Where: "class 3 prose; issue #50"},
	{Key: "glm/v@4.6{flash}", Want: 5, Where: "class 3 prose; issue #50"},
	// Class 4.
	{Key: "deepseek{turbo}", Want: 2, Where: "class 4 table; issue #51"},
	{Key: "claude{code}", Want: 1, Where: "class 4 table; issue #51"},
	// Class 6. These two are the pair the report first stated as 14 and 10.
	{Key: "claude/opus", Want: 19, Where: "class 6 prose; issue #53"},
	{Key: "claude/sonnet", Want: 13, Where: "class 6 prose; issue #53"},
	// Class 7, the GLM vision 'v' line the vision-suffix fix issue re-keys to
	// {vision}. These are the records the misparse currently splits under a v
	// variant.
	{Key: "glm/v@4.6", Want: 9, Where: "class 7 prose; vision-suffix fix issue"},
	{Key: "glm/v@4.5", Want: 7, Where: "class 7 prose; vision-suffix fix issue"},
	{Key: "glm/v@5", Want: 6, Where: "class 7 prose; vision-suffix fix issue"},
	// Class 8, the flash variant line. The two conforming controls show the
	// ruling's target shape is already met where the id is well formed; the qwen
	// coder base is the collision the dropped-flash sibling shares.
	{Key: "gemini/flash@2.5", Want: 24, Where: "class 8 prose; flash-uniformity fix issue"},
	{Key: "qwen/coder@3", Want: 24, Where: "class 8 prose; flash-uniformity fix issue"},
	{Key: "step/flash@3.5", Want: 5, Where: "class 8 prose; flash-uniformity fix issue"},
}

// conformanceDoubledDash is the vendor-prefix spelling class 6 blames. The report states
// how many of the claude/opus and claude/sonnet records carry it, so that figure
// is pinned too: it is the doubled dash's real blast radius, and it is much
// smaller than the key totals above.
const conformanceDoubledDash = "--"

// conformanceDoubledDashCounts pins the doubled-dash subset of two class-6 keys.
var conformanceDoubledDashCounts = []conformanceKeyRecords{
	{Key: "claude/opus", Want: 6, Where: "class 6 prose; issue #53"},
	{Key: "claude/sonnet", Want: 6, Where: "class 6 prose; issue #53"},
}

// conformanceDuoChat is the vendor spelling whose records an earlier draft of class 6
// silently dropped, which is why that draft stated 14 and 10 instead of 19 and 13.
// The report names the size of that omission, so the size is pinned here as well.
const conformanceDuoChat = "duo-chat"

// conformanceDuoChatCounts pins the duo-chat subset of the same two class-6 keys.
var conformanceDuoChatCounts = []conformanceKeyRecords{
	{Key: "claude/opus", Want: 5, Where: "class 6 prose; issue #53"},
	{Key: "claude/sonnet", Want: 3, Where: "class 6 prose; issue #53"},
}

// conformanceClass5Label is the upstream family label class 5 is about. Class 5 counts a
// DIFFERENT population from the census: every row carrying this label, not the
// seed-token matches. Its figures are therefore pinned separately, and the report
// states both units side by side.
const conformanceClass5Label = "deepseek-thinking"

const (
	// conformanceClass5ServedRows is the ROW count: provider rows carrying the label.
	// The issue reported 96; this is the figure comparable with it.
	conformanceClass5ServedRows = 158
	// conformanceClass5ServedIDs is the same population under the counting rule above:
	// distinct case-sensitive served ids.
	conformanceClass5ServedIDs = 63
	// conformanceClass5LabIDs is the models-view side, distinct ids.
	conformanceClass5LabIDs = 6
)

// conformanceClass5Dest pins one destination of the class-5 label in BOTH units, because
// the report prints both and a reader must be able to tell them apart.
type conformanceClass5Dest struct {
	Key  string
	IDs  int
	Rows int
}

// conformanceClass5Destinations is the class-5 destination table.
var conformanceClass5Destinations = []conformanceClass5Dest{
	{Key: "deepseek/pro", IDs: 35, Rows: 106},
	{Key: "deepseek", IDs: 19, Rows: 40},
	{Key: "deepseek#70b", IDs: 3, Rows: 6},
	{Key: "deepseek#32b", IDs: 2, Rows: 2},
	{Key: "deepseek/v3.2-exp", IDs: 1, Rows: 1},
	{Key: "deepseek#8b", IDs: 1, Rows: 1},
	{Key: "deepseek/v3.2", IDs: 1, Rows: 1},
	{Key: "deepseek#14b", IDs: 1, Rows: 1},
}

// conformanceMatchedRows is the ROW-level match count: catalog rows whose id or raw
// family carries a seed token, with no de-duplication. It is pinned beside
// conformanceCensusTotal because the report prints both, and because the gap between
// them (6,666 rows -> 3,105 distinct ids) IS the de-duplication step: a reader who
// re-runs the census without it gets the larger number and concludes the report is
// wrong.
const conformanceMatchedRows = 6666

// conformanceUniverseRows is the whole vendored catalog: provider rows plus models rows.
const conformanceUniverseRows = 7791

// The two views the universe is made of. The report states the SPLIT, not only
// the sum, and the two views reach the parser by two different code paths, so a
// change that moves both views while preserving the sum is a real change and must
// go red.
const (
	// conformanceProviderViewRows is the provider (served) view: cat.Models.
	conformanceProviderViewRows = 7430
	// conformanceLabViewRows is the models (lab) view: cat.Metadata.
	conformanceLabViewRows = 361
)

// conformanceIsLetter reports whether b is an ASCII letter. The census boundary rule is
// stated in terms of LETTERS, not word characters, deliberately: a lab token is
// routinely glued to a digit ("hy3", "gpt-5", "step3", "glm-4.6"), so a digit must
// be an allowed boundary, while a letter must not ("midjourney-steps" is not a
// StepFun model, "codellama" is not attributed to llama by this rule).
func conformanceIsLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// conformanceHasToken reports whether s contains tok delimited by non-letters on both
// sides. s and tok are compared case-insensitively; tok must already be lowercase.
func conformanceHasToken(s, tok string) bool {
	low := strings.ToLower(s)
	for start := 0; start+len(tok) <= len(low); {
		off := strings.Index(low[start:], tok)
		if off < 0 {
			return false
		}
		at := start + off
		end := at + len(tok)
		leftOK := at == 0 || !conformanceIsLetter(low[at-1])
		rightOK := end == len(low) || !conformanceIsLetter(low[end])
		if leftOK && rightOK {
			return true
		}
		start = at + 1
	}
	return false
}

// conformanceAttributeLab returns the index of the FIRST seed lab whose token group
// matches either the raw id or the raw upstream family string, and whether any
// matched. First-match-wins keeps the per-lab counts disjoint.
func conformanceAttributeLab(id string, rawFamily Family) (int, bool) {
	for i, lab := range conformanceSeedTokens {
		for _, tok := range lab.Tokens {
			if conformanceHasToken(id, tok) || conformanceHasToken(string(rawFamily), tok) {
				return i, true
			}
		}
	}
	return 0, false
}

// ---------------------------------------------------------------------------
// The boundary rule's declared COST: what the letter rule drops
// ---------------------------------------------------------------------------

// conformancePlainLab attributes by a BARE SUBSTRING search, with no delimiter test at
// all. It is the naive rule the boundary rule replaces, and it exists only to
// measure the gap between the two rules. That gap is the report's exclusion
// table, which the report bills as a declared cost that was MEASURED.
func conformancePlainLab(id string, rawFamily Family) (int, bool) {
	low, fam := strings.ToLower(id), strings.ToLower(string(rawFamily))
	for i, lab := range conformanceSeedTokens {
		for _, tok := range lab.Tokens {
			if strings.Contains(low, tok) || strings.Contains(fam, tok) {
				return i, true
			}
		}
	}
	return 0, false
}

// conformanceExclusion pins ONE row of the exclusion table, in BOTH units, for the same
// reason the class 5 table is pinned in both: the two units differ here, and a
// row figure printed under a record heading is exactly the error this pin exists
// to stop. A record is dropped when the plain rule attributes at least one of its
// rows and the letter rule attributes none of them.
type conformanceExclusion struct {
	// Lab is the seed lab the plain rule would have attributed the record to.
	Lab string
	// Records is the count under the counting rule: distinct raw id per view.
	Records int
	// Rows is the same population with no de-duplication.
	Rows int
	// Where names the prose that carries the figure.
	Where string
}

// conformanceBoundaryExclusions is the pinned exclusion table. The one row where the two
// units disagree is mistral: "mistralai/mixtral-8x22b-instruct" is served three
// times, twice with the raw family "mistral" (which the letter rule matches, so
// the RECORD is attributed) and once with an empty raw family (that ROW is
// dropped). One record, two units, two numbers.
var conformanceBoundaryExclusions = []conformanceExclusion{
	{Lab: "gpt", Records: 6, Rows: 6, Where: "boundary rule table"},
	{Lab: "mistral", Records: 4, Rows: 5, Where: "boundary rule table"},
	{Lab: "gemma", Records: 3, Rows: 3, Where: "boundary rule table"},
	{Lab: "glm", Records: 2, Rows: 2, Where: "boundary rule table"},
	{Lab: "claude", Records: 1, Rows: 1, Where: "boundary rule table"},
	{Lab: "step", Records: 1, Rows: 1, Where: "boundary rule table"},
}

const (
	// conformanceExcludedRecords is the exclusion table's total in the report's own unit.
	conformanceExcludedRecords = 17
	// conformanceExcludedRows is the same drop set with no de-duplication.
	conformanceExcludedRows = 18
)

// conformanceCheckBoundaryExclusions pins the exclusion table and the "NO record changes
// lab" half of the sentence that carries it. Without this the table is the one
// measured figure in the report that can rot green: renaming a dropped id within
// its own blocked stem moves no other pin.
func conformanceCheckBoundaryExclusions(t *testing.T, cat Catalog) {
	t.Helper()
	type record struct {
		plainLab, letterLab int
		plain, letter       bool
	}
	recs := make(map[string]*record)
	order := make([]string, 0, conformanceUniverseRows)
	dropRows := make(map[string]int)
	visit := func(view, id string, fam Family) {
		key := view + "|" + id
		r, ok := recs[key]
		if !ok {
			r = &record{}
			recs[key] = r
			order = append(order, key)
		}
		plainLab, plainOK := conformancePlainLab(id, fam)
		letterLab, letterOK := conformanceAttributeLab(id, fam)
		if plainOK && !r.plain {
			r.plain, r.plainLab = true, plainLab
		}
		if letterOK && !r.letter {
			r.letter, r.letterLab = true, letterLab
		}
		if plainOK && !letterOK {
			dropRows[conformanceSeedTokens[plainLab].Name]++
		}
	}
	for _, m := range cat.Models {
		visit("serving", string(m.ID), m.Family)
	}
	for _, md := range cat.Metadata {
		visit("lab", string(md.MetadataID), md.RawFamily)
	}

	dropRecords := make(map[string]int)
	labChanged := 0
	for _, key := range order {
		r := recs[key]
		switch {
		case r.plain && !r.letter:
			dropRecords[conformanceSeedTokens[r.plainLab].Name]++
		case r.plain && r.letter && r.plainLab != r.letterLab:
			labChanged++
		}
	}

	fix := "re-measure, then correct this pin and the boundary rule table in " + conformanceReportPath
	records, rows := 0, 0
	for _, ex := range conformanceBoundaryExclusions {
		records += ex.Records
		rows += ex.Rows
		if got := dropRecords[ex.Lab]; got != ex.Records {
			t.Errorf("the letter rule drops %d %s RECORDS, the pinned exclusion table says %d\n"+
				"  What: the boundary rule's declared COST moved for this lab. A dropped record"+
				" is one the plain substring rule attributes and the letter rule does not\n"+
				"  Why it matters: the %s is billed as measured, and it under-counts real"+
				" products of the lab whose token was blocked, so its size is the price of the"+
				" rule\n"+
				"  How to fix: %s", got, ex.Lab, ex.Records, ex.Where, fix)
		}
		if got := dropRows[ex.Lab]; got != ex.Rows {
			t.Errorf("the letter rule drops %d %s ROWS, the pinned exclusion table says %d\n"+
				"  What: the ROW unit of the same drop set moved. It is NOT the record figure:"+
				" one id served by several providers is one record and several rows\n"+
				"  How to fix: %s", got, ex.Lab, ex.Rows, fix)
		}
	}
	if records != conformanceExcludedRecords {
		t.Errorf("the pinned exclusion rows sum to %d records, conformanceExcludedRecords says %d;"+
			" the table and its total must state the same fact", records, conformanceExcludedRecords)
	}
	if rows != conformanceExcludedRows {
		t.Errorf("the pinned exclusion rows sum to %d rows, conformanceExcludedRows says %d;"+
			" the table and its total must state the same fact", rows, conformanceExcludedRows)
	}
	measuredRecords, measuredRows := 0, 0
	for _, n := range dropRecords {
		measuredRecords += n
	}
	for _, n := range dropRows {
		measuredRows += n
	}
	if measuredRecords != conformanceExcludedRecords || measuredRows != conformanceExcludedRows {
		t.Errorf("the letter rule drops %d records / %d rows in total, the report states %d / %d\n"+
			"  What: a lab OUTSIDE the pinned table now loses records to the boundary rule,"+
			" or the totals moved\n"+
			"  How to fix: %s", measuredRecords, measuredRows, conformanceExcludedRecords, conformanceExcludedRows, fix)
	}
	if labChanged != 0 {
		t.Errorf("%d records change lab under the letter rule, the report states NONE\n"+
			"  What: the boundary rule now RE-ATTRIBUTES a record instead of only dropping"+
			" it, so the rule's cost is no longer a pure under-count\n"+
			"  How to fix: %s", labChanged, fix)
	}
}

func TestParserConformance_TokenCensus(t *testing.T) {
	// Arithmetic mirror: the per-lab table and the total are two statements of
	// one fact, so they are checked against each other before any measurement.
	sum := 0
	for _, lab := range conformanceSeedTokens {
		sum += lab.Want
	}
	if sum != conformanceCensusTotal {
		t.Fatalf("per-lab counts sum to %d, conformanceCensusTotal says %d; the table and the"+
			" total must state the same fact", sum, conformanceCensusTotal)
	}

	cat := conformanceLoadCatalog(t)
	idx := conformanceBuildIndex(cat)

	got := make([]int, len(conformanceSeedTokens))
	seen := make(map[string]bool)
	unkeyed := 0
	matchedRows := 0
	// keyRecords counts RECORDS per entity key under the counting rule declared
	// above: one distinct case-sensitive raw id per view, among the census matches.
	keyRecords := make(map[string]int)
	doubledDash := make(map[string]int)
	duoChat := make(map[string]int)
	// Provider view: every SERVED catalog row. ParseCatalogJSON puts the upstream
	// family string in ModelInfo.Family (the codegen pipeline reads it from there
	// as the RAW family), so that is the field the census greps beside the id.
	for _, m := range cat.Models {
		i, ok := conformanceAttributeLab(string(m.ID), m.Family)
		if !ok {
			continue
		}
		matchedRows++
		uk := "serving|" + string(m.ID)
		if seen[uk] {
			continue
		}
		seen[uk] = true
		got[i]++
		key, keyed := idx.servingKeys[strings.ToLower(string(m.ID))]
		if !keyed {
			unkeyed++
			continue
		}
		keyRecords[key]++
		if strings.Contains(string(m.ID), conformanceDoubledDash) {
			doubledDash[key]++
		}
		if strings.Contains(strings.ToLower(string(m.ID)), conformanceDuoChat) {
			duoChat[key]++
		}
	}
	// Models view: every LAB (metadata) row.
	for _, md := range cat.Metadata {
		i, ok := conformanceAttributeLab(string(md.MetadataID), md.RawFamily)
		if !ok {
			continue
		}
		matchedRows++
		uk := "lab|" + string(md.MetadataID)
		if seen[uk] {
			continue
		}
		seen[uk] = true
		got[i]++
		// Every lab row is driven through the production decomposition too, so
		// "no match skipped" means every match reached the parser, not merely
		// that it was counted.
		key := metadataEntityRef(md.MetadataID).String()
		keyRecords[key]++
		if strings.Contains(string(md.MetadataID), conformanceDoubledDash) {
			doubledDash[key]++
		}
		if strings.Contains(strings.ToLower(string(md.MetadataID)), conformanceDuoChat) {
			duoChat[key]++
		}
	}

	measured := 0
	for i, lab := range conformanceSeedTokens {
		measured += got[i]
		if got[i] != lab.Want {
			t.Errorf("lab %q matched %d catalog records, the pinned census says %d\n"+
				"  What: the seed-token census MOVED against the committed catalog snapshot\n"+
				"  Why it matters: GH#43's acceptance clause is a per-lab count whose sum is"+
				" the total; a stale table makes the report a false statement\n"+
				"  How to fix: re-run the sweep after the catalog re-vendor and update BOTH"+
				" this table and conformanceCensusTotal, then re-check the report in"+
				" "+conformanceReportPath, lab.Name, got[i], lab.Want)
		}
		if got[i] == 0 {
			t.Errorf("lab %q matched nothing; a seed lab with no match makes the census vacuous", lab.Name)
		}
	}
	if measured != conformanceCensusTotal {
		t.Errorf("the census matched %d distinct records, conformanceCensusTotal says %d", measured, conformanceCensusTotal)
	}
	if measured != len(seen) {
		t.Errorf("the per-lab counts sum to %d but %d distinct records were attributed;"+
			" attribution must be disjoint and total", measured, len(seen))
	}
	// Every matched SERVED row must reach an entity: an unkeyed match is a match
	// the sweep skipped, which the acceptance clause forbids.
	if unkeyed != 0 {
		t.Errorf("%d matched serving records carry no entity key; no match may be skipped", unkeyed)
	}

	// The two units the report prints must both hold. The gap between them is the
	// de-duplication step, and stating only one of them is what makes the census
	// look wrong to a reader who re-derives it.
	if len(cat.Models) != conformanceProviderViewRows || len(cat.Metadata) != conformanceLabViewRows {
		t.Errorf("the catalog snapshot holds %d provider rows and %d lab rows, the report"+
			" states %d and %d\n"+
			"  What: the VIEW SPLIT moved. The sum alone is not enough: the two views reach"+
			" the parser by two different code paths, so a change that moves both while"+
			" preserving the sum is still a change the sweep must be re-measured against\n"+
			"  How to fix: re-measure and correct the Method section of %s",
			len(cat.Models), len(cat.Metadata), conformanceProviderViewRows, conformanceLabViewRows, conformanceReportPath)
	}
	if conformanceProviderViewRows+conformanceLabViewRows != conformanceUniverseRows {
		t.Errorf("the pinned view split sums to %d, conformanceUniverseRows says %d; the split and"+
			" the total must state the same fact",
			conformanceProviderViewRows+conformanceLabViewRows, conformanceUniverseRows)
	}
	if universe := len(cat.Models) + len(cat.Metadata); universe != conformanceUniverseRows {
		t.Errorf("the catalog snapshot holds %d rows, the report states %d\n"+
			"  How to fix: re-measure and correct the Method section of %s",
			universe, conformanceUniverseRows, conformanceReportPath)
	}
	if matchedRows != conformanceMatchedRows {
		t.Errorf("%d catalog ROWS carry a seed token, the report states %d\n"+
			"  What: the row-level match figure moved. It is NOT the census total:"+
			" the rows collapse to %d distinct (view, id) records\n"+
			"  How to fix: re-measure and correct the Method section of %s",
			matchedRows, conformanceMatchedRows, conformanceCensusTotal, conformanceReportPath)
	}

	// The per-key record counts the report and the six posted fix issues state.
	// Without this pin the prose figures are unguarded, and six of them were
	// measurably wrong before it existed.
	for _, kr := range conformanceKeyRecordCounts {
		if got := keyRecords[kr.Key]; got != kr.Want {
			t.Errorf("key %q holds %d records, the pinned table says %d\n"+
				"  What: a per-key RECORD count the prose states has MOVED. A record is one"+
				" distinct case-sensitive raw id within one catalog view, among the census"+
				" matches; see the counting rule at the top of this section\n"+
				"  Why it matters: this figure sizes and orders the fix work. It is stated in"+
				" %s\n"+
				"  How to fix: re-measure, then correct BOTH this pin and every mirror of the"+
				" figure: %s, the matching draft in docs/research/parser-conformance-fix-drafts/, and the"+
				" POSTED GitHub issue body",
				kr.Key, got, kr.Want, kr.Where, conformanceReportPath)
		}
	}
	for _, kr := range conformanceDoubledDashCounts {
		if got := doubledDash[kr.Key]; got != kr.Want {
			t.Errorf("key %q holds %d records whose raw id carries the doubled dash %q,"+
				" the pinned table says %d\n"+
				"  What: the doubled dash's real BLAST RADIUS moved. It is a SUBSET of the"+
				" key's record count, and the report must not state the key total as if it"+
				" were the blast radius\n"+
				"  How to fix: re-measure, then correct this pin, %s and %s",
				kr.Key, got, conformanceDoubledDash, kr.Want, conformanceReportPath, kr.Where)
		}
	}

	for _, kr := range conformanceDuoChatCounts {
		if got := duoChat[kr.Key]; got != kr.Want {
			t.Errorf("key %q holds %d records whose raw id carries %q, the pinned table says %d\n"+
				"  What: the size of the omission that made an earlier draft state 14 and 10"+
				" for these two keys has moved. It is a SUBSET of the key's record count\n"+
				"  How to fix: re-measure, then correct this pin, %s and %s",
				kr.Key, got, conformanceDuoChat, kr.Want, conformanceReportPath, kr.Where)
		}
	}

	conformanceCheckBoundaryExclusions(t, cat)
	conformanceCheckClass5(t, cat, idx)

	if testing.Verbose() {
		names := make([]string, 0, len(conformanceSeedTokens))
		for i, lab := range conformanceSeedTokens {
			names = append(names, fmt.Sprintf("%-10s %5d", lab.Name, got[i]))
		}
		sort.Strings(names)
		t.Logf("GH#43 census over %d records:\n%s\nTOTAL %d",
			len(cat.Models)+len(cat.Metadata), strings.Join(names, "\n"), measured)
	}
}

// conformanceCheckClass5 pins the class-5 destination table. Class 5 measures a DIFFERENT
// population from the census — every row carrying the upstream family label, not
// the seed-token matches — so it is measured and pinned separately, in BOTH units,
// because the report prints both and a reader must be able to tell them apart.
func conformanceCheckClass5(t *testing.T, cat Catalog, idx conformanceIndex) {
	t.Helper()
	rows := 0
	ids := make(map[string]bool)
	destIDs := make(map[string]int)
	destRows := make(map[string]int)
	for _, m := range cat.Models {
		if !strings.EqualFold(string(m.Family), conformanceClass5Label) {
			continue
		}
		rows++
		key := idx.servingKeys[strings.ToLower(string(m.ID))]
		destRows[key]++
		if ids[string(m.ID)] {
			continue
		}
		ids[string(m.ID)] = true
		destIDs[key]++
	}
	labIDs := make(map[string]bool)
	for _, md := range cat.Metadata {
		if strings.EqualFold(string(md.RawFamily), conformanceClass5Label) {
			labIDs[string(md.MetadataID)] = true
		}
	}

	fix := "re-measure, then correct this pin, the class 5 section of " + conformanceReportPath +
		", docs/research/parser-conformance-fix-drafts/05-distill-destination-ruling.md and the POSTED issue #52"
	if rows != conformanceClass5ServedRows {
		t.Errorf("upstream family %q carries %d provider ROWS, the pin says %d\n  How to fix: %s",
			conformanceClass5Label, rows, conformanceClass5ServedRows, fix)
	}
	if len(ids) != conformanceClass5ServedIDs {
		t.Errorf("upstream family %q carries %d distinct served IDS, the pin says %d\n  How to fix: %s",
			conformanceClass5Label, len(ids), conformanceClass5ServedIDs, fix)
	}
	if len(labIDs) != conformanceClass5LabIDs {
		t.Errorf("upstream family %q carries %d distinct lab IDS, the pin says %d\n  How to fix: %s",
			conformanceClass5Label, len(labIDs), conformanceClass5LabIDs, fix)
	}
	sumIDs, sumRows := 0, 0
	for _, d := range conformanceClass5Destinations {
		sumIDs += d.IDs
		sumRows += d.Rows
		if got := destIDs[d.Key]; got != d.IDs {
			t.Errorf("class 5 destination %q holds %d distinct ids, the pin says %d\n  How to fix: %s",
				d.Key, got, d.IDs, fix)
		}
		if got := destRows[d.Key]; got != d.Rows {
			t.Errorf("class 5 destination %q holds %d provider rows, the pin says %d\n  How to fix: %s",
				d.Key, got, d.Rows, fix)
		}
	}
	// The destination table must be TOTAL: it accounts for every row and every id
	// the label carries, so a destination cannot be quietly dropped from the prose.
	if sumIDs != conformanceClass5ServedIDs {
		t.Errorf("the class 5 destination table sums to %d ids but the label carries %d;"+
			" the table must account for every id\n  How to fix: %s", sumIDs, conformanceClass5ServedIDs, fix)
	}
	if sumRows != conformanceClass5ServedRows {
		t.Errorf("the class 5 destination table sums to %d rows but the label carries %d;"+
			" the table must account for every row\n  How to fix: %s", sumRows, conformanceClass5ServedRows, fix)
	}
}
