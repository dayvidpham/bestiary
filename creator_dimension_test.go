package bestiary_test

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// creatorsFileJSON is the read-only view of parse/data/creators.json the consistency
// guards below need. It reads the FILE rather than the loaded table so a curation slip
// that the loader would silently tolerate is still caught at the source of truth.
type creatorsFileJSON struct {
	Creators []struct {
		Family  string `json:"family"`
		Creator string `json:"creator"`
	} `json:"creators"`
	Withheld []struct {
		Family string `json:"family"`
		Reason string `json:"reason"`
	} `json:"withheld"`
}

func loadCreatorsFile(t *testing.T) creatorsFileJSON {
	t.Helper()
	raw, err := os.ReadFile("parse/data/creators.json")
	if err != nil {
		t.Fatalf("read parse/data/creators.json: %v", err)
	}
	var f creatorsFileJSON
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("parse parse/data/creators.json: %v", err)
	}
	if len(f.Creators) == 0 {
		t.Fatal("parse/data/creators.json carries no creator rows; the guards below would be vacuous")
	}
	return f
}

// TestCreatorsJSON_EveryCreatorIsKnown closes the consistency gap between the curated
// table and the well-known Creator set: every creator token the data file emits must
// satisfy Creator.IsKnown.
//
// The two are independently authored, and nothing else forces them together — the FK
// gate in parseCreatorTable checks the FAMILY side only, because Creator is an OPEN
// type and an unrecognized token is still a VALID value. That openness is right for
// an ingest-surfaced originator, but a token typed by hand into the curated seed is a
// different thing: if it is not in knownCreators, Family.Creator() returns a creator
// that Creator.IsKnown() denies, and (worse) codegen's creatorExpr has no case for it
// and silently bakes the untyped fallback Creator("token") instead of the constant.
// This is part 3 of the five-part cost documented on Creators; the test is what makes
// omitting it fail loudly.
func TestCreatorsJSON_EveryCreatorIsKnown(t *testing.T) {
	f := loadCreatorsFile(t)
	for _, row := range f.Creators {
		if !bestiary.Creator(row.Creator).IsKnown() {
			t.Errorf("creators.json family %q maps to creator %q, which reports !IsKnown()\n"+
				"  Why: a hand-curated token must also be a well-known Creator, or codegen bakes Creator(%q) instead of the constant\n"+
				"  How to fix: add the Creator constant, its knownCreators entry, and its creatorExpr case, then re-derive the Creators() pin",
				row.Family, row.Creator, row.Creator)
		}
	}
}

// TestCreatorsJSON_EveryKnownCreatorIsUsed is the other direction: every well-known
// Creator constant must be reachable from at least one curated row.
//
// A constant no row references is DEAD curation — it costs a knownCreators entry, a
// creatorExpr case and a slot in the Creators() pin while being unreachable through
// Family.Creator(). Catching it here keeps the five-part cost honest in both
// directions, so the set cannot quietly accumulate tokens nothing maps to.
func TestCreatorsJSON_EveryKnownCreatorIsUsed(t *testing.T) {
	f := loadCreatorsFile(t)
	used := make(map[bestiary.Creator]struct{}, len(f.Creators))
	for _, row := range f.Creators {
		used[bestiary.Creator(row.Creator)] = struct{}{}
	}
	for _, c := range bestiary.Creators() {
		if _, ok := used[c]; !ok {
			t.Errorf("well-known Creator %q is referenced by NO creators.json row (dead curation)\n"+
				"  How to fix: map a family to it, or delete the constant, its knownCreators entry and its creatorExpr case",
				c)
		}
	}
}

// TestCreatorsJSON_DeadRowsDisposition pins the disposition of the six creator rows
// that map a family NO catalog entity currently carries.
//
// The six are compound and retired family spellings that canonicalization no longer
// routes an entity to: claude-haiku / claude-opus / claude-sonnet fold into
// claude/<variant>, command-a / command-r fold into command/<variant>, and the
// o-series folds into gpt with the line designator in the variant slot. Their
// disposition is RETAIN, on exactly the rationale family.go already gives for keeping
// FamilyO in CanonicalProvider: each token is still a real raw_family value the
// upstream catalog emits, so a residual or future row carrying one resolves to its
// lab instead of falling through to CreatorNone.
//
// The test pins BOTH halves of that disposition, so neither can rot silently:
//   - the six rows are still PRESENT and still resolve to their lab (retention), and
//   - they are still DEAD (zero entities) — if canonicalization ever routes an entity
//     to one of them again, this fails and the "dead" claim is re-examined rather
//     than carried forward as folklore.
func TestCreatorsJSON_DeadRowsDisposition(t *testing.T) {
	deadRows := map[bestiary.Family]bestiary.Creator{
		"claude-haiku":  bestiary.CreatorAnthropic,
		"claude-opus":   bestiary.CreatorAnthropic,
		"claude-sonnet": bestiary.CreatorAnthropic,
		"command-a":     bestiary.CreatorCohere,
		"command-r":     bestiary.CreatorCohere,
		"o":             bestiary.CreatorOpenAI,
	}

	entitiesByFamily := make(map[bestiary.Family]int)
	for _, e := range bestiary.Entities() {
		entitiesByFamily[e.Ref.Family]++
	}

	for fam, wantCreator := range deadRows {
		// Retention: the row is still there and still points at the right lab.
		if got := fam.Creator(); got != wantCreator {
			t.Errorf("dead row %q: Creator() = %q, want %q — the row is RETAINED deliberately as "+
				"residual-value insurance for a raw_family the catalog still emits; do not delete it",
				fam, got, wantCreator)
		}
		// Deadness: still zero entities.
		if n := entitiesByFamily[fam]; n != 0 {
			t.Errorf("row %q now carries %d entities, but is documented as DEAD in creators.json\n"+
				"  Why this matters: the retention rationale is 'no entity reaches this family, keep the row for residual rows'.\n"+
				"  How to fix: re-examine the disposition — the family is live now, so it belongs with the live rows",
				fam, n)
		}
	}
}

// TestCreatorsJSON_WithheldIsDisjointAndReported pins the withhold mechanism end to
// end: a withheld family carries a reason, carries NO creator mapping, and is
// re-surfaced by the codegen disagreement derivation on every run.
//
// The last clause is the point of the mechanism. A deferral that is merely absent
// from the table is indistinguishable from an oversight; one that is re-reported
// every regen stays a visible, explained decision.
func TestCreatorsJSON_WithheldIsDisjointAndReported(t *testing.T) {
	f := loadCreatorsFile(t)
	if len(f.Withheld) == 0 {
		t.Skip("no withheld families curated; nothing to check")
	}
	mapped := make(map[string]struct{}, len(f.Creators))
	for _, row := range f.Creators {
		mapped[row.Family] = struct{}{}
	}
	reported := make(map[bestiary.Family]bestiary.CreatorLabClass)
	for _, d := range bestiary.CreatorLabDisagreementsFromBaked() {
		reported[d.Family] = d.Class
	}
	for _, w := range f.Withheld {
		if _, dup := mapped[w.Family]; dup {
			t.Errorf("family %q is both withheld and mapped in creators.json", w.Family)
		}
		if strings.TrimSpace(w.Reason) == "" {
			t.Errorf("withheld family %q carries no reason", w.Family)
		}
		if got := bestiary.Family(w.Family).Creator(); got != bestiary.CreatorNone {
			t.Errorf("withheld family %q resolves to creator %q, want CreatorNone", w.Family, got)
		}
		if class, ok := reported[bestiary.Family(w.Family)]; !ok {
			t.Errorf("withheld family %q is NOT reported by the lab-disagreement derivation; the deferral is invisible", w.Family)
		} else if class != bestiary.CreatorLabClassWithheld {
			t.Errorf("withheld family %q is reported with class %v, want %v", w.Family, class, bestiary.CreatorLabClassWithheld)
		}
	}
}

// TestCreatorLabDisagreements_Pinned pins the exact disagreement set this catalog
// produces, with its classes and its evidence weights.
//
// It is a literal census guard in the TestMetadataCensus_SynthesizedStandalonesPinned
// mould: the derivation is mechanical, so any change to the metadata, the aliases or
// the curated table that adds, drops or re-classifies a disagreement must be a
// DELIBERATE re-pin here rather than a silent drift. Each row's presence is also its
// own justification for why the lab evidence was not auto-applied:
//
//   - glm: the lab spells itself "zhipuai", the curated token is "zhipu" — one
//     organization, two spellings, so applying the lab value would churn the token
//     without changing the fact.
//   - seed: the lab spells itself "bytedance-seed", the curated token is "bytedance" —
//     the same one-organization-two-spellings shape as glm.
//   - llama / mistral: NVIDIA re-publishes both labs' weights under its own "nvidia/"
//     prefix, so the catalog carries two originators for one family and applying
//     either would silently pick a winner.
//   - gemma: AI Singapore re-publishes Google's Gemma weights under an "aisingapore/"
//     prefix, which is the same multi-org shape.
//
// The set was FOUR rows until the ling/inkling/kling collision split. The fourth was
// "ling", withheld: its only lab-scoped row was thinkingmachines/inkling, reaching it
// through a curated alias, so applying the evidence would have credited Thinking
// Machines with inclusionAI's Ling line. The split retargets that alias onto the new
// "inkling" family, where the same evidence is unambiguous and agrees with curation —
// so it produces no row at all — and leaves "ling" with no lab-scoped row to disagree
// about. The withhold list is now empty and the class is unexercised here by design;
// TestCreatorsJSON_WithheldIsDisjointAndReported covers the mechanism itself.
//
// All five surviving rows are mechanical disagreements, and auto-applying any of them
// would have produced a WRONG creator. That is the measured justification for the
// derivation being report-only rather than self-applying.
func TestCreatorLabDisagreements_Pinned(t *testing.T) {
	type want struct {
		labs  []string
		class bestiary.CreatorLabClass
		count int
	}
	// 3 rows -> 5 with the 2026-08-28 models.dev catalog refresh, and every evidence
	// weight moved. The two new rows are the same two mechanical classes the existing
	// rows already document:
	//   - gemma (multi-org): the models view now carries aisingapore-scoped Gemma rows
	//     beside google's, so the catalog itself names two originators for the family.
	//     AI Singapore's SEA-LION Gemma derivatives are re-publications of Google's
	//     weights — the identical shape as llama/meta+nvidia — so applying either lab
	//     would silently pick a winner.
	//   - seed (spelling-variant): the curated creator is "bytedance" and the only lab
	//     prefix reaching the family is "bytedance-seed". One organization, two
	//     spellings, exactly the glm/zhipu case; applying the lab value would churn the
	//     token without changing the fact.
	// Counts: glm 14 -> 17, llama 5 -> 10, mistral 11 -> 12 — evidence weights, which
	// rise with the metadata rows the refresh added (models view 263 -> 361).
	wanted := map[bestiary.Family]want{
		"gemma":   {labs: []string{"aisingapore", "google"}, class: bestiary.CreatorLabClassMultiOrg, count: 5},
		"glm":     {labs: []string{"zhipuai"}, class: bestiary.CreatorLabClassSpellingVariant, count: 17},
		"llama":   {labs: []string{"meta", "nvidia"}, class: bestiary.CreatorLabClassMultiOrg, count: 10},
		"mistral": {labs: []string{"mistral", "nvidia"}, class: bestiary.CreatorLabClassMultiOrg, count: 12},
		"seed":    {labs: []string{"bytedance-seed"}, class: bestiary.CreatorLabClassSpellingVariant, count: 12},
	}

	got := bestiary.CreatorLabDisagreementsFromBaked()
	if len(got) != len(wanted) {
		t.Fatalf("DeriveCreatorLabDisagreements returned %d rows, want %d; got %+v", len(got), len(wanted), got)
	}
	// Sorted ascending by family — the byte-stability contract of the emission.
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Family < got[j].Family }) {
		t.Errorf("disagreement rows are not sorted ascending by family: %+v", got)
	}
	for _, d := range got {
		w, ok := wanted[d.Family]
		if !ok {
			t.Errorf("unexpected disagreement row for family %q (class %v, labs %v)", d.Family, d.Class, d.Labs)
			continue
		}
		if d.Class != w.class {
			t.Errorf("family %q: class = %v, want %v", d.Family, d.Class, w.class)
		}
		if strings.Join(d.Labs, ",") != strings.Join(w.labs, ",") {
			t.Errorf("family %q: labs = %v, want %v", d.Family, d.Labs, w.labs)
		}
		if d.Count != w.count {
			t.Errorf("family %q: count = %d, want %d", d.Family, d.Count, w.count)
		}
		if !sort.StringsAreSorted(d.Labs) {
			t.Errorf("family %q: labs are not sorted: %v", d.Family, d.Labs)
		}
		if strings.TrimSpace(d.Reason) == "" {
			t.Errorf("family %q: empty reason; a report row a curator cannot act on is noise", d.Family)
		}
	}

	// The mechanical (non-withheld) rows are the justification for report-only
	// behaviour: every one of them would have applied a WRONG creator.
	mechanical := 0
	for _, d := range got {
		if d.Class != bestiary.CreatorLabClassWithheld {
			mechanical++
		}
	}
	// 3 -> 5 with the 2026-08-28 catalog refresh: the gemma multi-org and seed
	// spelling-variant rows join (see the note on the wanted table above).
	if mechanical != 5 {
		t.Errorf("mechanical disagreements = %d, want 5 (glm/seed spelling + gemma/llama/mistral multi-org)", mechanical)
	}
	if mechanical != len(got) {
		t.Errorf("%d of %d rows are withheld deferrals; the withhold list is empty, so every "+
			"reported row must be a mechanical disagreement", len(got)-mechanical, len(got))
	}
}

// TestCreatorLabDisagreements_Deterministic asserts the derivation is a pure function
// of its input across repeated calls: same input, byte-identical row sequence. The
// sweep accumulates through Go maps, so an omitted sort in output position would show
// up here (and in the codegen N=100 byte-identity guard) and nowhere else.
func TestCreatorLabDisagreements_Deterministic(t *testing.T) {
	first := bestiary.CreatorLabDisagreementsFromBaked()
	for i := 0; i < 20; i++ {
		got := bestiary.CreatorLabDisagreementsFromBaked()
		if len(got) != len(first) {
			t.Fatalf("iteration %d: %d rows, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Family != first[j].Family || got[j].Class != first[j].Class {
				t.Fatalf("iteration %d row %d: (%q,%v), want (%q,%v)",
					i, j, got[j].Family, got[j].Class, first[j].Family, first[j].Class)
			}
		}
	}
}

// TestCreatorLabDisagreements_EmptyInput asserts the derivation degrades to an empty
// (non-nil) result on empty input rather than panicking or reporting phantom rows.
func TestCreatorLabDisagreements_EmptyInput(t *testing.T) {
	got := bestiary.DeriveCreatorLabDisagreements(nil)
	if got == nil {
		t.Fatal("DeriveCreatorLabDisagreements(nil) returned a nil slice, want empty non-nil")
	}
	if len(got) != 0 {
		t.Errorf("DeriveCreatorLabDisagreements(nil) = %+v, want no rows", got)
	}
}

// TestCreatorLabClass_TokensAreStable pins the JSON encoding of every class, because
// the tokens are the committed report's vocabulary: renaming one silently rewrites an
// artifact a curator reads.
func TestCreatorLabClass_TokensAreStable(t *testing.T) {
	cases := map[bestiary.CreatorLabClass]string{
		bestiary.CreatorLabClassNone:            "none",
		bestiary.CreatorLabClassMultiOrg:        "multi-org",
		bestiary.CreatorLabClassSpellingVariant: "spelling-variant",
		bestiary.CreatorLabClassDivergent:       "divergent",
		bestiary.CreatorLabClassWithheld:        "withheld",
	}
	for class, wantToken := range cases {
		if got := class.String(); got != wantToken {
			t.Errorf("CreatorLabClass(%d).String() = %q, want %q", int(class), got, wantToken)
		}
		b, err := class.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText(%v): %v", class, err)
		}
		if string(b) != wantToken {
			t.Errorf("CreatorLabClass(%d).MarshalText() = %q, want %q", int(class), b, wantToken)
		}
		// Round trip: the committed report must be readable by the program that wrote it.
		var back bestiary.CreatorLabClass
		if err := back.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText(%q): %v", b, err)
		}
		if back != class {
			t.Errorf("round trip of %q = %v, want %v", b, back, class)
		}
	}

	// Strict decode: an unrecognized token is an actionable error, never a silent zero.
	var bad bestiary.CreatorLabClass
	err := bad.UnmarshalText([]byte("not-a-class"))
	if err == nil {
		t.Fatal("UnmarshalText(\"not-a-class\") = nil error; the class vocabulary is closed and must reject unknown tokens")
	}
	if !strings.Contains(err.Error(), "unknown class token") {
		t.Errorf("error message missing \"unknown class token\"; got: %v", err)
	}
	if bad != bestiary.CreatorLabClassNone {
		t.Errorf("failed decode mutated the receiver to %v", bad)
	}
}
