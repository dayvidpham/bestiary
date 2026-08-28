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
//  1. TestGH43Conformance_CitedStrings drives the AUTHORED corpus
//     (testdata/parse/gh43_conformance_corpus.json): every string the issue cites,
//     plus the measured witnesses and the conforming CONTROLS that isolate each
//     defect's cause, each pinned to the entity key the production path produces
//     for it today. It is a fixed, hand-authored case list, so it is a corpus.
//
//  2. TestGH43Sweep_TokenCensus is the SWEEP itself: a computed census over the
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

// gh43Kind is the closed set of how a corpus string reaches the production
// parser. The three members take genuinely different code paths, so a case must
// say which one it pins.
type gh43Kind string

const (
	// gh43Serving is a models.dev PROVIDER-view id: a served row that the codegen
	// pipeline turns into a ProviderInstance on an Entity. Its key is read back
	// off the registry's entity index.
	gh43Serving gh43Kind = "serving-id"
	// gh43Lab is a models.dev MODELS-view (metadata) id, decomposed by the
	// metadata join's own id-driven path.
	gh43Lab gh43Kind = "lab-id"
	// gh43OffCatalog is a spelling that appears in USAGE data but is absent from
	// the vendored catalog. It has no entity of its own; it is driven through the
	// same id-driven decomposition to record what key it would land on.
	gh43OffCatalog gh43Kind = "off-catalog-id"
)

// IsValid reports whether the kind is one of the known members.
func (k gh43Kind) IsValid() bool {
	switch k {
	case gh43Serving, gh43Lab, gh43OffCatalog:
		return true
	}
	return false
}

// gh43Conformance is the closed set of verdicts the sweep may record for a
// measured key.
type gh43Conformance string

const (
	// gh43Conforming: the measured key is CORRECT. Used both for a cited defect
	// that is refuted at this tip and for a control whose only job is to isolate
	// a neighbouring defect's cause.
	gh43Conforming gh43Conformance = "conforming"
	// gh43Defect: the measured key is WRONG, and WantKey states the correct
	// destination.
	gh43Defect gh43Conformance = "defect"
	// gh43Undecided: the measured key is wrong OR right, and the sweep must not
	// say which — the destination is a curation ruling. WantKey carries the
	// EXPECTED_TBD marker and the fix issue frames the decision.
	gh43Undecided gh43Conformance = "undecided"
)

// IsValid reports whether the verdict is one of the known members.
func (c gh43Conformance) IsValid() bool {
	switch c {
	case gh43Conforming, gh43Defect, gh43Undecided:
		return true
	}
	return false
}

// gh43ExpectedTBD is the marker an undecided case carries in WantKey. It is a
// literal, not an empty string, so an undecided case can never be confused with a
// case whose author forgot to fill the field in.
const gh43ExpectedTBD = "EXPECTED_TBD"

// gh43CaseCount is the EXACT authored case count. An exact control (not a floor)
// catches a drop as well as a stray add.
const gh43CaseCount = 41

// gh43ClassCount is the number of defect classes GH#43 cites. Every one must be
// covered by at least one case.
const gh43ClassCount = 6

type gh43Input struct {
	Raw  string   `json:"raw"`
	Kind gh43Kind `json:"kind"`
}

type gh43Expected struct {
	// Key is the entity key the production path produces for Raw TODAY.
	Key string `json:"key"`
	// Conformance is the sweep's verdict on Key.
	Conformance gh43Conformance `json:"conformance"`
	// DefectClass is the GH#43 class (1..6) this case belongs to. A conforming
	// control still names the class it controls.
	DefectClass int `json:"defect_class"`
	// WantKey is the CORRECT destination: equal to Key when conforming, the
	// corrected key when a defect, gh43ExpectedTBD when undecided.
	WantKey string `json:"want_key"`
}

// gh43ProductionKey drives one corpus string through the PRODUCTION path for its
// kind and returns the entity key. It never re-implements a decomposition.
func gh43ProductionKey(t *testing.T, in gh43Input, servingKeys map[string]string) string {
	t.Helper()
	switch in.Kind {
	case gh43Serving:
		key, ok := servingKeys[strings.ToLower(in.Raw)]
		if !ok {
			t.Fatalf("serving id %q holds no entity instance in the registry\n"+
				"  What: the corpus claims this is a served catalog row, but no Entity carries it\n"+
				"  How to fix: re-measure against parse/data/modelsdev/catalog.json; if the row"+
				" is gone upstream, re-classify the case as off-catalog-id", in.Raw)
		}
		return key
	case gh43Lab, gh43OffCatalog:
		return metadataEntityRef(MetadataID(in.Raw)).String()
	default:
		t.Fatalf("case input kind %q is not a member of the closed set", in.Kind)
		return ""
	}
}

// gh43ServingKeyIndex builds the serving-id -> entity-key map from the registry's
// OWN entities, so the key a case asserts is the key the entity actually carries.
func gh43ServingKeyIndex() map[string]string {
	idx := make(map[string]string)
	for _, e := range Entities() {
		key := e.Ref.String()
		for _, inst := range e.Instances {
			idx[strings.ToLower(string(inst.ID))] = key
		}
	}
	return idx
}

func TestGH43Conformance_CitedStrings(t *testing.T) {
	corpus, err := testcase.LoadCorpus[gh43Input, gh43Expected](gh43ConformanceCorpusJSON)
	if err != nil {
		t.Fatalf("load GH#43 conformance corpus: %v", err)
	}
	if got := len(corpus.Cases); got != gh43CaseCount {
		t.Fatalf("GH#43 conformance corpus has %d cases, want exactly %d", got, gh43CaseCount)
	}
	// Non-vacuity: classification + provenance + mutation on every case.
	assert.RequireValid(t, corpus)

	servingKeys := gh43ServingKeyIndex()

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
		if c.Expected.DefectClass < 1 || c.Expected.DefectClass > gh43ClassCount {
			t.Errorf("case %q: defect_class %d is not one of the %d cited classes",
				c.Name, c.Expected.DefectClass, gh43ClassCount)
			continue
		}
		if rawSeen[string(c.Input.Kind)+"|"+c.Input.Raw] {
			t.Errorf("case %q: duplicate (kind, raw) pair %q", c.Name, c.Input.Raw)
		}
		rawSeen[string(c.Input.Kind)+"|"+c.Input.Raw] = true
		classSeen[c.Expected.DefectClass]++
		if c.Expected.Conformance == gh43Defect {
			classDefects[c.Expected.DefectClass]++
		}

		// The verdict must be internally consistent with WantKey. This is what
		// stops a case from being a decoration: a "defect" that wants the key it
		// already has says nothing.
		switch c.Expected.Conformance {
		case gh43Conforming:
			if c.Expected.WantKey != c.Expected.Key {
				t.Errorf("case %q: conforming case wants %q but pins key %q; a conforming"+
					" case must want the key it measures", c.Name, c.Expected.WantKey, c.Expected.Key)
			}
		case gh43Defect:
			if c.Expected.WantKey == "" || c.Expected.WantKey == c.Expected.Key {
				t.Errorf("case %q: defect case must state a want_key DIFFERENT from the"+
					" measured key %q, got %q", c.Name, c.Expected.Key, c.Expected.WantKey)
			}
		case gh43Undecided:
			if c.Expected.WantKey != gh43ExpectedTBD {
				t.Errorf("case %q: undecided case must carry want_key %q, got %q",
					c.Name, gh43ExpectedTBD, c.Expected.WantKey)
			}
		}

		t.Run(c.Name, func(t *testing.T) {
			got := gh43ProductionKey(t, c.Input, servingKeys)
			if got != c.Expected.Key {
				t.Errorf("%s %q: production key = %q, corpus pins %q\n"+
					"  What: the sweep's measured key for this string MOVED\n"+
					"  Why it matters: this corpus is the GH#43 evidence record; a moved key"+
					" means a defect was fixed, re-shaped, or newly introduced\n"+
					"  How to fix: re-measure the string, then update BOTH the key and the"+
					" conformance verdict in testdata/parse/gh43_conformance_corpus.json",
					c.Input.Kind, c.Input.Raw, got, c.Expected.Key)
			}
		})
	}

	// Coverage: every cited class carries at least one case, and every class that
	// the sweep CONFIRMS carries at least one control or refutation beside it, so
	// no class is represented by a lone unfalsifiable row.
	for class := 1; class <= gh43ClassCount; class++ {
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

// gh43LabToken is one seed lab's token group. A lab whose products carry several
// unrelated stems (Mistral, Tencent) is ONE group, so the census counts labs, not
// spellings.
type gh43LabToken struct {
	Name   string
	Tokens []string
	// Want is the MEASURED match count against the committed catalog snapshot at
	// parse/data/modelsdev/catalog.json. It is snapshot-relative by construction:
	// a re-vendored catalog moves it, and the sweep must then be re-measured.
	Want int
}

// gh43SeedTokens is the GH#43 seed lab list, in the PRECEDENCE order the census
// uses to attribute a record to exactly ONE lab. Attribution is by first match in
// this order, so the per-lab counts are disjoint and their sum is the distinct
// matched-record total — the accounting the issue's acceptance clause requires.
var gh43SeedTokens = []gh43LabToken{
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

// gh43CensusTotal is the measured total of distinct matched catalog records. The
// sum of every gh43LabToken.Want must equal it, and the census must match it.
const gh43CensusTotal = 3105

// gh43IsLetter reports whether b is an ASCII letter. The census boundary rule is
// stated in terms of LETTERS, not word characters, deliberately: a lab token is
// routinely glued to a digit ("hy3", "gpt-5", "step3", "glm-4.6"), so a digit must
// be an allowed boundary, while a letter must not ("midjourney-steps" is not a
// StepFun model, "codellama" is not attributed to llama by this rule).
func gh43IsLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// gh43HasToken reports whether s contains tok delimited by non-letters on both
// sides. s and tok are compared case-insensitively; tok must already be lowercase.
func gh43HasToken(s, tok string) bool {
	low := strings.ToLower(s)
	for start := 0; start+len(tok) <= len(low); {
		off := strings.Index(low[start:], tok)
		if off < 0 {
			return false
		}
		at := start + off
		end := at + len(tok)
		leftOK := at == 0 || !gh43IsLetter(low[at-1])
		rightOK := end == len(low) || !gh43IsLetter(low[end])
		if leftOK && rightOK {
			return true
		}
		start = at + 1
	}
	return false
}

// gh43AttributeLab returns the index of the FIRST seed lab whose token group
// matches either the raw id or the raw upstream family string, and whether any
// matched. First-match-wins keeps the per-lab counts disjoint.
func gh43AttributeLab(id string, rawFamily Family) (int, bool) {
	for i, lab := range gh43SeedTokens {
		for _, tok := range lab.Tokens {
			if gh43HasToken(id, tok) || gh43HasToken(string(rawFamily), tok) {
				return i, true
			}
		}
	}
	return 0, false
}

func TestGH43Sweep_TokenCensus(t *testing.T) {
	// Arithmetic mirror: the per-lab table and the total are two statements of
	// one fact, so they are checked against each other before any measurement.
	sum := 0
	for _, lab := range gh43SeedTokens {
		sum += lab.Want
	}
	if sum != gh43CensusTotal {
		t.Fatalf("per-lab counts sum to %d, gh43CensusTotal says %d; the table and the"+
			" total must state the same fact", sum, gh43CensusTotal)
	}

	raw, err := os.ReadFile("parse/data/modelsdev/catalog.json")
	if err != nil {
		t.Fatalf("read the vendored catalog snapshot: %v", err)
	}
	cat, err := ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("parse the vendored catalog snapshot: %v", err)
	}

	servingKeys := gh43ServingKeyIndex()

	got := make([]int, len(gh43SeedTokens))
	seen := make(map[string]bool)
	unkeyed := 0
	// Provider view: every SERVED catalog row. ParseCatalogJSON puts the upstream
	// family string in ModelInfo.Family (the codegen pipeline reads it from there
	// as the RAW family), so that is the field the census greps beside the id.
	for _, m := range cat.Models {
		i, ok := gh43AttributeLab(string(m.ID), m.Family)
		if !ok {
			continue
		}
		uk := "serving|" + string(m.ID)
		if seen[uk] {
			continue
		}
		seen[uk] = true
		got[i]++
		if _, keyed := servingKeys[strings.ToLower(string(m.ID))]; !keyed {
			unkeyed++
		}
	}
	// Models view: every LAB (metadata) row.
	for _, md := range cat.Metadata {
		i, ok := gh43AttributeLab(string(md.MetadataID), md.RawFamily)
		if !ok {
			continue
		}
		uk := "lab|" + string(md.MetadataID)
		if seen[uk] {
			continue
		}
		seen[uk] = true
		got[i]++
		// Every lab row is driven through the production decomposition too, so
		// "no match skipped" means every match reached the parser, not merely
		// that it was counted.
		_ = metadataEntityRef(md.MetadataID).String()
	}

	measured := 0
	for i, lab := range gh43SeedTokens {
		measured += got[i]
		if got[i] != lab.Want {
			t.Errorf("lab %q matched %d catalog records, the pinned census says %d\n"+
				"  What: the seed-token census MOVED against the committed catalog snapshot\n"+
				"  Why it matters: GH#43's acceptance clause is a per-lab count whose sum is"+
				" the total; a stale table makes the report a false statement\n"+
				"  How to fix: re-run the sweep after the catalog re-vendor and update BOTH"+
				" this table and gh43CensusTotal, then re-check the report in"+
				" docs/research/gh43_parser_conformance_sweep.md", lab.Name, got[i], lab.Want)
		}
		if got[i] == 0 {
			t.Errorf("lab %q matched nothing; a seed lab with no match makes the census vacuous", lab.Name)
		}
	}
	if measured != gh43CensusTotal {
		t.Errorf("the census matched %d distinct records, gh43CensusTotal says %d", measured, gh43CensusTotal)
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

	if testing.Verbose() {
		names := make([]string, 0, len(gh43SeedTokens))
		for i, lab := range gh43SeedTokens {
			names = append(names, fmt.Sprintf("%-10s %5d", lab.Name, got[i]))
		}
		sort.Strings(names)
		t.Logf("GH#43 census over %d records:\n%s\nTOTAL %d",
			len(cat.Models)+len(cat.Metadata), strings.Join(names, "\n"), measured)
	}
}
