package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// nominaCensus tallies a nomen slice by scheme — the shared helper for the census
// pins below.
func nominaCensus(ns []bestiary.Nomen) map[bestiary.NomenScheme]int {
	m := map[bestiary.NomenScheme]int{}
	for _, n := range ns {
		m[n.Scheme]++
	}
	return m
}

// TestNomina_CensusExact pins the EXACT per-scheme census of the minted nomen set
// over the static registry (the "census literal pinned at bake"). The counts are
// derived from the committed models_static_gen.go: 940 canonical (one Preferred nomen
// per distinct entity key), 2834 provider-ID (one Admitted nomen per distinct instance
// ID spelling, deduped within an entity), 1 alias (the grok-beta seed claim) and 179
// huggingface (4 curated Hub seeds + 175 distinct-triple repos harvested by the
// cmd/bestiary-hf live run; the 4 curated coalesce with their harvested twins). On a
// models.dev snapshot refresh, a curated-claim addition, or a re-run of the HF harvest,
// these move consciously, like the other census pins.
//
// provider-ID went 2792 → 2791 when the curated command-a-plus override landed. The
// dedup is per distinct ID spelling WITHIN an entity, and command-a-plus-05-2026 was
// split across two entities by a provider disagreement (cohere's raw_family said
// command/a, nano-gpt's empty raw_family produced the compound family
// "command-a-plus"), so the one ID spelling minted a provider-ID nomen in each. The
// override converges both rows on command/a-plus, where the two spellings dedup to
// one — a duplicate naming removed, not a naming lost. That re-key left canonical
// unmoved: it drops one entity key and adds one.
//
// canonical went 975 → 971 when the curated cortecs pins landed: four phantom
// claude/opus@5…@8 entities (a glued-token mis-parse gave each one cortecs instance)
// merged into the real claude/opus@4.5…@4.8 entities, retiring four entity keys and
// therefore four Preferred nomina. provider-ID is untouched by that merge — the four
// cortecs ID spellings still mint one nomen each, now inside the real entities.
//
// canonical went 971 → 982 with the family-"o" over-capture fix: the junk-bucket entity
// that had held alibaba's video models, openai's speech models, quiverai's arrow and
// cohere's rerankers split into the 15 distinct entities those models always were
// (4 bucket keys retired). One Preferred nomen per entity key, so canonical tracks it
// exactly; provider-ID is again untouched, since no ID spelling was added or removed.
//
// canonical went 982 → 979 with the kimi/minimax turbo demotions: three {turbo}
// entities folded into their plain siblings, retiring three keys and therefore three
// Preferred nomina. provider-ID is untouched AGAIN, and here that is the load-bearing
// part: the turbo ID spellings survive as Admitted provider-ID nomina on the merged
// entities. A demotion changes what is IDENTITY, never what is recorded.
//
// canonical went 979 → 977 with the "p"-as-dot version decode: the phantom glm@5p1 and
// glm@5p2 entities merge into the real glm@5.1 and glm@5.2, retiring two keys and
// therefore two Preferred nomina. provider-ID is untouched a THIRD time, and again that
// is the load-bearing part — the fireworks "5p1"/"5p2" ID spellings survive as Admitted
// provider-ID nomina on the merged entities, so decoding a spelling for IDENTITY never
// erases the spelling from the record.
//
// canonical went 977 → 978 with the tts-1-hd identity split: "hd" becomes an IDENTITY
// modifier so tts@1{hd} splits off from tts@1, adding one Preferred canonical nomen.
// provider-ID is UNCHANGED at 2791 — the split moves an existing instance to a new
// entity, it adds no instance, so no provider-ID nomen is minted or lost.
//
// canonical went 978 → 976 with the o-series dual-identity fix: openai-o1 / openai-o3 /
// openai-o3-mini fold onto the existing gpt/o entities, retiring the two junk family-"o"
// keys and their two Preferred nomina. provider-ID is UNCHANGED at 2791 (the three
// digitalocean ID spellings survive as Admitted provider-ID nomina on the merged
// entities — folding a spelling for IDENTITY never erases it from the record).
//
// canonical went 976 → 955 with the dot-lost version repair + 1t param-size routing: the
// dot-lost merges and the 1t re-keys retire 21 Preferred canonical nomina. provider-ID is
// UNCHANGED at 2791 AGAIN — every repair MOVES instances onto the corrected entity, it
// removes none, so the dotless/dash/1t ID spellings all survive as Admitted provider-ID
// nomina on the merged entities.
//
// canonical went 955 → 947 with the entity-level MERGE-only N→N.0 fold (C4): 8 bare-N
// entities fold onto their N.0 siblings, retiring 8 Preferred canonical nomina. provider-ID
// UNCHANGED at 2791 a THIRD time — the fold merges two entities, moving no instance, so the
// bare-version ID spellings survive as Admitted provider-ID nomina on the merged entities.
//
// canonical went 957 → 940 with the global free demotion: 17 free-tier entity keys retire
// (0 added), so 17 Preferred canonical nomina go with them. provider-ID UNCHANGED at 2834 a
// FOURTH time, same reason as every merge before it — the demoted instances re-home onto a
// surviving sibling and carry their ID spellings across as Admitted provider-ID nomina.
func TestNomina_CensusExact(t *testing.T) {
	const (
		wantCanonical   = 940  // 947 -> 958: 2026-07-23 snapshot refresh (upstream additions); 958 -> 957: v0.2.8 curation slice — command/a{translate} split (+1) minus deepseek@1/@2 dot-lost merges (−2); 957 -> 940: the global free demotion retires 17 entity keys (0 added), and there is exactly one canonical nomen per entity, so the canonical count tracks the entity census exactly
		wantProviderID  = 2834 // 2791 -> 2834: 2026-07-23 refresh, +43 new upstream instance spellings; UNCHANGED by the v0.2.8 slice — the re-keyed instances keep their provider-ID spellings as Admitted nomina on the merged/split entities (the C4-fold precedent)
		wantAlias       = 1
		wantHuggingFace = 179 // 4 -> 179: v0.2.8 HF-bot slice. The cmd/bestiary-hf live run harvested 179 open-weight Hub repos that JOIN a catalog entity. Of those, 4 are the pre-existing curated Hub claims (nomen_claims.json), aliased to their EXACT curated (Value, huggingface-scheme, ResolvesTo) triples, so each harvested attestation COALESCES onto its curated claim — one nomen carrying TWO attestations (curated + huggingface), adding 0 to the nomen count (validation case 3). The other 175 harvested repos are distinct triples: +175. 4 + 175 = 179.
		wantTotal       = wantCanonical + wantProviderID + wantAlias + wantHuggingFace
	)
	// v0.2.8 multi-attestation lift: coalesceNomina groups by the (Value, Scheme,
	// ResolvesTo) triple. The attestation refactor itself is census-NEUTRAL (one row,
	// one attestation), but the HF-bot slice's harvested seed IS a new attester, so the
	// huggingface count moves 4 -> 179 (see wantHuggingFace). The genuinely-new union
	// fires where a harvested repo shares a curated claim's triple: the 4 curated Hub
	// claims each coalesce with their aliased harvested twin into ONE nomen carrying TWO
	// attestations (curated + huggingface), so those 4 add 0 — the count grows only by
	// the 175 distinct-triple harvested repos. Total 940 + 2834 + 1 + 179 = 3954.
	all := bestiary.MintNomina(bestiary.Entities())
	if len(all) != wantTotal {
		t.Errorf("MintNomina total = %d, want %d", len(all), wantTotal)
	}
	c := nominaCensus(all)
	if c[bestiary.NomenSchemeCanonical] != wantCanonical {
		t.Errorf("canonical nomina = %d, want %d", c[bestiary.NomenSchemeCanonical], wantCanonical)
	}
	if c[bestiary.NomenSchemeProviderID] != wantProviderID {
		t.Errorf("provider-id nomina = %d, want %d", c[bestiary.NomenSchemeProviderID], wantProviderID)
	}
	if c[bestiary.NomenSchemeAlias] != wantAlias {
		t.Errorf("alias nomina = %d, want %d", c[bestiary.NomenSchemeAlias], wantAlias)
	}
	if c[bestiary.NomenSchemeHuggingFace] != wantHuggingFace {
		t.Errorf("huggingface nomina = %d, want %d", c[bestiary.NomenSchemeHuggingFace], wantHuggingFace)
	}
	// The registry Nomina() convenience must agree with MintNomina(Entities()).
	if got := len(bestiary.Nomina()); got != wantTotal {
		t.Errorf("Nomina() total = %d, want %d", got, wantTotal)
	}
	// The from-models joint (sync path over fetched models) agrees on the
	// instance-bearing schemes with the from-entities joint: provider-id and alias are
	// IDENTICAL. Canonical differs by exactly the 4 metadata-only standalone entities
	// (the ornith rows synthesized by the metadata join) — those have no instances in
	// StaticModels, so the from-models joint mints no canonical nomen for them. This
	// documents the ONLY divergence between the two shared joints.
	const wantFromModelsCanonical = wantCanonical - 4
	fromModels := nominaCensus(bestiary.MintNominaFromModels(bestiary.StaticModels()))
	if fromModels[bestiary.NomenSchemeProviderID] != wantProviderID ||
		fromModels[bestiary.NomenSchemeAlias] != wantAlias ||
		fromModels[bestiary.NomenSchemeHuggingFace] != wantHuggingFace {
		t.Errorf("MintNominaFromModels provider-id/alias/huggingface census = %v, want %d/%d/%d",
			fromModels, wantProviderID, wantAlias, wantHuggingFace)
	}
	if fromModels[bestiary.NomenSchemeCanonical] != wantFromModelsCanonical {
		t.Errorf("MintNominaFromModels canonical = %d, want %d (the entity census minus the 4 metadata-only standalones)",
			fromModels[bestiary.NomenSchemeCanonical], wantFromModelsCanonical)
	}
}

// TestNomina_DeterministicSortedEmission verifies INV3 for the mint output over the
// REAL shipped bake: the slice is sorted by (Value, Scheme, entity key) and two mint
// calls are byte-order identical (no reliance on map iteration order). It is EXTENDED
// for the v0.2.8 multi-attestation lift to also compare each nomen's Attestations
// slice element-for-element across the two calls — but on shipped data every nomen
// carries at most one attestation (0 of 3797 carry >1, per the current census), so
// this only ever compares slices of length <=1: it catches true cross-call
// nondeterminism (e.g. map-iteration leakage reaching the emitted order) but does
// NOT exercise — and cannot pin — lessAttestation's multi-element sort-key
// correctness. That guard is TestCoalesceNomina_UnionsSameTripleAttestations
// (nomen_coalesce_internal_test.go), which constructs a real 2-attestation union and
// reddens on a dropped-attestation or weakened-sort-key mutation.
func TestNomina_DeterministicSortedEmission(t *testing.T) {
	a := bestiary.MintNomina(bestiary.Entities())
	b := bestiary.MintNomina(bestiary.Entities())
	if len(a) != len(b) {
		t.Fatalf("two mint calls differ in length: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Value != b[i].Value || a[i].Scheme != b[i].Scheme ||
			a[i].ResolvesTo.String() != b[i].ResolvesTo.String() ||
			a[i].Status != b[i].Status {
			t.Fatalf("mint output nondeterministic at %d: %+v vs %+v", i, a[i], b[i])
		}
		// EXTEND: the attestation set must be identical across the two mint calls,
		// element-for-element on every field — the TOTAL sort key is what makes this
		// deterministic (equal-key ⇒ byte-identical ⇒ deduped ⇒ stably ordered).
		if len(a[i].Attestations) != len(b[i].Attestations) {
			t.Fatalf("attestation count nondeterministic at %d (%q): %d vs %d",
				i, a[i].Value, len(a[i].Attestations), len(b[i].Attestations))
		}
		for j := range a[i].Attestations {
			if a[i].Attestations[j] != b[i].Attestations[j] {
				t.Fatalf("attestation nondeterministic at nomen %d (%q) attestation %d: %+v vs %+v",
					i, a[i].Value, j, a[i].Attestations[j], b[i].Attestations[j])
			}
		}
		if i > 0 {
			prev, cur := a[i-1], a[i]
			if prev.Value > cur.Value {
				t.Fatalf("not sorted by value at %d: %q > %q", i, prev.Value, cur.Value)
			}
		}
	}
	// The guard must not pass vacuously: at least one nomen must actually carry an
	// attestation (every minted nomen does, exactly one on shipped single-source data).
	sawAttestation := false
	for i := range a {
		if len(a[i].Attestations) > 0 {
			sawAttestation = true
			break
		}
	}
	if !sawAttestation {
		t.Fatal("no minted nomen carries an attestation; the extended guard would pass vacuously")
	}
}

// TestNomina_ValidateNoConflict asserts the real minted set has no same-triple
// conflict — the positive half of the codegen guard.
func TestNomina_ValidateNoConflict(t *testing.T) {
	if err := bestiary.ValidateNomina(bestiary.MintNomina(bestiary.Entities())); err != nil {
		t.Fatalf("ValidateNomina over the real bake returned a conflict: %v", err)
	}
}

// TestNomina_SameTripleConflict_Loud is the NEGATIVE CONTROL with the v0.2.8 INVERTED
// semantics: a same-triple (value, scheme, entity_key) disagreement is a LOUD conflict
// ONLY on Status (bestiary's single editorial judgment). Two records sharing the triple
// but carrying DIFFERENT attesters are now LEGAL — they union into one nomen's
// multi-attestation set — where the v1 guard rejected them. A crafted duplicate keeps
// the guard non-vacuous.
func TestNomina_SameTripleConflict_Loud(t *testing.T) {
	ref := bestiary.EntityRef{Family: "grok", Version: "4.20", Modifier: []string{"reasoning"}}
	at := func(url string) []bestiary.NomenAttestation {
		return []bestiary.NomenAttestation{{SourceURL: url, Source: bestiary.DataSourceModelsDev, Authority: bestiary.AuthorityPrimary, Method: bestiary.IngestMethodCurated}}
	}
	// Same triple, DIFFERING Status → LOUD (never last-write-wins).
	conflict := []bestiary.Nomen{
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityAdmitted, ResolvesTo: ref, Attestations: at("https://a")},
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityPreferred, ResolvesTo: ref, Attestations: at("https://b")},
	}
	err := bestiary.ValidateNomina(conflict)
	if err == nil {
		t.Fatal("ValidateNomina accepted a same-triple Status conflict; want a loud error")
	}
	if !strings.Contains(err.Error(), "same-triple") {
		t.Errorf("conflict error is not actionable about the triple: %v", err)
	}

	// Same triple, SAME Status, DIFFERENT attester → LEGAL append (the v0.2.8 invert:
	// this pairing was a conflict in v1). ValidateNomina must accept it.
	legal := []bestiary.Nomen{
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityAdmitted, ResolvesTo: ref, Attestations: at("https://a")},
		{Value: "grok-beta", Scheme: bestiary.NomenSchemeAlias, Status: bestiary.AcceptabilityAdmitted, ResolvesTo: ref, Attestations: at("https://b")},
	}
	if err := bestiary.ValidateNomina(legal); err != nil {
		t.Errorf("ValidateNomina rejected a same-triple/same-Status differing-attester pairing (now legal): %v", err)
	}

	// An EXACT-duplicate triple (identical fields) is a harmless idempotent no-op and
	// must NOT error.
	dup := []bestiary.Nomen{conflict[0], conflict[0]}
	if err := bestiary.ValidateNomina(dup); err != nil {
		t.Errorf("ValidateNomina rejected an identical-duplicate triple (should be idempotent): %v", err)
	}
}

// TestNomenLookup_GrokBeta is the grok-beta worked example: the curated xAI alias
// claim resolves to the real grok@4.20{reasoning} entity, is Admitted, and carries a
// single NomenAttestation whose claim attribution (SourceURL = the xAI page) is
// DISTINCT from its Source (the curated ingest we read it from) — the
// SourceURL-vs-Source discipline, now recorded per-attestation, demonstrated
// end-to-end. len==1 is re-pinned CONSCIOUSLY: still ONE Nomen for grok-beta, now
// carrying its one attestation.
func TestNomenLookup_GrokBeta(t *testing.T) {
	matches, ok := bestiary.NomenLookup("grok-beta")
	if !ok || len(matches) != 1 {
		t.Fatalf("NomenLookup(grok-beta) = (%d rows, ok=%v), want exactly 1", len(matches), ok)
	}
	n := matches[0]
	if n.Scheme != bestiary.NomenSchemeAlias {
		t.Errorf("grok-beta scheme = %v, want alias", n.Scheme)
	}
	if n.Status != bestiary.AcceptabilityAdmitted {
		t.Errorf("grok-beta status = %v, want admitted", n.Status)
	}
	if got := n.ResolvesTo.String(); got != "grok@4.20{reasoning}" {
		t.Errorf("grok-beta resolves to %q, want grok@4.20{reasoning}", got)
	}
	// The curated claim mints exactly ONE attestation.
	if len(n.Attestations) != 1 {
		t.Fatalf("grok-beta carries %d attestations, want exactly 1 (the single curated claim)", len(n.Attestations))
	}
	at := n.Attestations[0]
	// Claim attribution is pinned to the exact archive.org SNAPSHOT of the xAI docs
	// page, per the curated-claims archive policy (see NomenAttestation.SourceURL).
	const grokBetaClaimantSnapshot = "https://web.archive.org/web/20260204041847/https://docs.x.ai/docs/models"
	if at.SourceURL != grokBetaClaimantSnapshot {
		t.Errorf("grok-beta SourceURL = %q, want the archived xAI claimant page %q", at.SourceURL, grokBetaClaimantSnapshot)
	}
	// The original claimant address stays recoverable from the snapshot itself —
	// which is why the policy adds no separate archive_url field.
	if !strings.HasSuffix(at.SourceURL, "https://docs.x.ai/docs/models") {
		t.Errorf("grok-beta SourceURL = %q does not end in the original xAI claimant URL", at.SourceURL)
	}
	if at.Source != bestiary.DataSourceCurated {
		t.Errorf("grok-beta Source = %q, want curated (the honest ingest — read from bestiary's own claim file, distinct from the xAI claimant)", at.Source)
	}
	// The curated alias defaults to Primary authority (xAI declaring its own model's
	// naming), Curated method (read from bestiary's own claim file).
	if at.Authority != bestiary.AuthorityPrimary {
		t.Errorf("grok-beta attestation authority = %v, want primary (the lab declaring its own naming)", at.Authority)
	}
	if at.Method != bestiary.IngestMethodCurated {
		t.Errorf("grok-beta attestation method = %v, want curated (from the committed claim seed)", at.Method)
	}
	// The alias target must be a real entity, so the CLI can show it end-to-end.
	if _, exists := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning"); !exists {
		t.Error("grok-beta resolves to grok@4.20{reasoning}, which is not a real entity")
	}
}

// TestNomenLookup_HomonymyPositiveFence is the HOMONYMY POSITIVE FENCE: a spelling
// that names more than one distinct entity returns ALL of its rows (never a single
// "the nomen"). It scans for a real homonym and asserts NomenLookup returns every row
// the index holds for it.
func TestNomenLookup_HomonymyPositiveFence(t *testing.T) {
	idx := map[string]int{}
	entsPerValue := map[string]map[string]bool{}
	for _, n := range bestiary.MintNomina(bestiary.Entities()) {
		idx[n.Value]++
		if entsPerValue[n.Value] == nil {
			entsPerValue[n.Value] = map[string]bool{}
		}
		entsPerValue[n.Value][n.ResolvesTo.String()] = true
	}
	var homonym string
	for v, ents := range entsPerValue {
		if len(ents) > 1 {
			homonym = v
			break
		}
	}
	if homonym == "" {
		t.Skip("no homonymous spelling in the current bake; fence not exercisable")
	}
	matches, ok := bestiary.NomenLookup(homonym)
	if !ok {
		t.Fatalf("NomenLookup(%q) missing; want the homonym rows", homonym)
	}
	if len(matches) != idx[homonym] {
		t.Errorf("NomenLookup(%q) returned %d rows, want all %d persisted rows", homonym, len(matches), idx[homonym])
	}
	distinct := map[string]bool{}
	for _, n := range matches {
		distinct[n.ResolvesTo.String()] = true
	}
	if len(distinct) < 2 {
		t.Errorf("homonym %q resolved to %d distinct entities, want >= 2 (the fence)", homonym, len(distinct))
	}
}

// TestEntityNomina_CanonicalPreferredPlusAlias verifies the per-entity projection: the
// grok@4.20{reasoning} entity's Nomina() carries its canonical key as a Preferred
// nomen AND the grok-beta alias that resolves to it.
func TestEntityNomina_CanonicalPreferredPlusAlias(t *testing.T) {
	e, ok := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok@4.20{reasoning} entity not found")
	}
	var sawCanonicalPreferred, sawAlias bool
	for _, n := range e.Nomina() {
		if n.Scheme == bestiary.NomenSchemeCanonical {
			if n.Value != "grok@4.20{reasoning}" {
				t.Errorf("canonical nomen value = %q, want the entity key", n.Value)
			}
			if n.Status != bestiary.AcceptabilityPreferred {
				t.Errorf("canonical nomen status = %v, want preferred", n.Status)
			}
			sawCanonicalPreferred = true
		}
		if n.Scheme == bestiary.NomenSchemeAlias && n.Value == "grok-beta" {
			sawAlias = true
		}
	}
	if !sawCanonicalPreferred {
		t.Error("entity Nomina() missing its canonical Preferred nomen")
	}
	if !sawAlias {
		t.Error("entity Nomina() missing the grok-beta alias claim that resolves to it")
	}
}

// TestDesignationNomenConsistencyFence pins the Designations↔Nomen consistency fence:
// for a static model, the SchemeCanonical Designation rating equals the
// NomenSchemeCanonical status (both Preferred) — shared schemes agree on rating.
func TestDesignationNomenConsistencyFence(t *testing.T) {
	e, ok := bestiary.EntityByTuple("grok", "", "4.20", "", "reasoning")
	if !ok {
		t.Fatal("grok@4.20{reasoning} entity not found")
	}
	// Canonical nomen status for this entity.
	var nomenStatus bestiary.AcceptabilityRating = -1
	for _, n := range e.Nomina() {
		if n.Scheme == bestiary.NomenSchemeCanonical {
			nomenStatus = n.Status
		}
	}
	if nomenStatus == -1 {
		t.Fatal("no canonical nomen for the entity")
	}
	// Canonical designation rating for an instance of the same entity.
	if len(e.Instances) == 0 {
		t.Fatal("entity has no instances")
	}
	m, ok := bestiary.LookupModel(e.Instances[0].ID)
	if !ok {
		t.Fatalf("LookupModel(%q) missing", e.Instances[0].ID)
	}
	var desigRating bestiary.AcceptabilityRating = -1
	for _, d := range m.Ref().Designations() {
		if d.Scheme == bestiary.SchemeCanonical {
			desigRating = d.Rating
		}
	}
	if desigRating != nomenStatus {
		t.Errorf("consistency fence broken: canonical Designation rating=%v but canonical Nomen status=%v (shared scheme must agree)", desigRating, nomenStatus)
	}
	if nomenStatus != bestiary.AcceptabilityPreferred {
		t.Errorf("canonical scheme rating=%v, want preferred (activation)", nomenStatus)
	}
}
