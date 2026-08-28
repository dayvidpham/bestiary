package bestiary

import "fmt"

// CanonicalProvider returns the originating/canonical provider for this Family.
//
// When the family is a well-known model family with a clear original publisher,
// the canonical provider is returned. When the family has no canonical mapping
// (community models, multi-org models, or unmapped families), the empty Provider
// is returned. Resolve falls back to ErrAmbiguous in that case.
//
// The mapping is a static switch populated at source time. Unknown families use
// the empty string sentinel rather than a wrong-but-plausible guess.
//
// TODO: review and fill in additional canonical-provider mappings beyond the
// initial well-known set.
func (f Family) CanonicalProvider() Provider {
	switch f {
	case FamilyClaude, FamilyClaudeHaiku, FamilyClaudeOpus, FamilyClaudeSonnet:
		// Anthropic is the canonical publisher for all claude-family models.
		return ProviderAnthropic
	case FamilyGemini, FamilyGemma:
		// Google is the canonical publisher for gemini and gemma families.
		return ProviderGoogle
	case FamilyGPT, FamilyO:
		// OpenAI is the canonical publisher for gpt-family models. The o-series reasoning
		// line (o1, o3, o4) canonicalizes to Family="gpt" with the line designator in the
		// VARIANT slot (gpt/o@1, gpt/o@3) via canonicalizeOpenAILine — for BOTH the
		// path-segment spelling (openai/o1) and the hyphen-glued spelling (openai-o1) — so
		// no o-series model carries Family="o" any longer. FamilyO is retained as a
		// canonical-provider mapping so any residual or future raw_family="o" row still
		// resolves to OpenAI rather than falling through.
		return ProviderOpenAI
	case FamilyLlama:
		// Meta's Llama models are published under the "local" provider (project decision).
		return ProviderLocal
	case FamilyMistral, FamilyCodestral, FamilyDevstral:
		// Mistral AI is the canonical publisher for mistral, codestral, and devstral families.
		return ProviderMistral
	case FamilyCommand, FamilyCommandA, FamilyCommandR:
		// Cohere is the canonical publisher of the Command line. The two compound
		// spellings are listed alongside the base family because they are real
		// raw_family values the upstream catalog emits, so a row can reach
		// CanonicalProvider carrying either one.
		return ProviderCohere
	case FamilyDeepSeek:
		// DeepSeek is the canonical publisher for deepseek family models.
		return ProviderDeepSeek
	case FamilyQwen:
		// Alibaba is the canonical publisher for qwen family models.
		return ProviderAlibaba
	default:
		// TODO: review canonical provider for this family
		return "" // empty Provider; Resolve falls back to ErrAmbiguous
	}
}

// IsKnown reports whether f is a recognized Family.
// The known set is generated from the models.dev API at codegen time
// (allFamilies, families_gen.go) plus the hand-curated curatedBaseFamilies
// supplement (base families the API omits but that lineage / canonical
// references depend on, e.g. "solar").
func (f Family) IsKnown() bool {
	for _, known := range allFamilies {
		if f == known {
			return true
		}
	}
	for _, known := range curatedBaseFamilies {
		if f == known {
			return true
		}
	}
	return false
}

// FamilySolar is the curated base family for Upstage's SOLAR models. The
// models.dev API never emits a bare "solar" family value — only the
// variant-qualified solar-mini / solar-pro reach allFamilies (families_gen.go) —
// yet the base family is needed as a valid lineage derivation PARENT (a SOLAR
// finetune names "solar" as its base). It is registered here as a hand-curated
// supplement to the generated set; see curatedBaseFamilies.
const FamilySolar Family = "solar"

// FamilyMythologic and FamilyHuginn are the two parent base families of the
// MythoMax-L2-13B merge. MythoMax is a weight merge of MythoLogic-L2 and Huginn,
// so the merge edge carries these as STANDALONE parent families (not as
// llama-variants): the parents are distinct artifacts in their own right, and a
// merge by definition combines >= 2 separate parents. Neither is emitted by the
// API as a base family value, so both are registered here so lineage
// parent-validation recognizes them.
const (
	FamilyMythologic Family = "mythologic"
	FamilyHuginn     Family = "huginn"
)

// FamilyC4AI, FamilyOrnith and FamilyQwQ (with the lowercase "hy" registered as a
// literal below) are the four families that the
// catalog decomposition DOES route real entities to but that the models.dev API
// never emits as a bare family value, so they are absent from the generated
// allFamilies set. Each one is the decomposed head of a lab-scoped id line —
// Cohere's c4ai-command-r, Tencent's hy-*, DeepReinforce's ornith-*, Alibaba's QwQ
// — and each is reached by exactly one models.dev lab prefix, so each has a
// well-determined Creator. They are registered here because the curated
// Family→Creator table's FK gate requires Family.IsKnown: without registration the
// four would have to be dropped from the creator dimension entirely, leaving their
// catalog entities unattributed for a reason that is about the family SET rather
// than about the originator.
const (
	FamilyC4AI   Family = "c4ai"
	FamilyOrnith Family = "ornith"
	FamilyQwQ    Family = "qwq"
)

// curatedBaseFamilies are hand-maintained base families that the models.dev API
// does not surface as a bare family value but which are required as canonical /
// lineage references (e.g. as a derivation parent, or as the subject of a curated
// Family→Creator row). IsKnown consults these in addition to the generated
// allFamilies, so a curated family is a first-class known Family. Keep this list
// minimal: add a base family only when a real reference needs it. The yi base
// family (01.AI) is already present in allFamilies and so is NOT repeated here.
var curatedBaseFamilies = [...]Family{
	FamilySolar,      // base for upstage SOLAR (allFamilies has only solar-mini/solar-pro)
	FamilyMythologic, // MythoMax merge parent
	FamilyHuginn,     // MythoMax merge parent
	FamilyC4AI,       // creator row: cohere (entities present, absent from allFamilies)
	FamilyOrnith,     // creator row: deepreinforce (entities present, absent from allFamilies)
	FamilyQwQ,        // creator row: alibaba (entities present, absent from allFamilies)
	// Tencent's hy-* line, registered as a LITERAL rather than through a constant
	// because the generated set already binds the identifier FamilyHy to a
	// DIFFERENT Family VALUE: allFamilies carries the upstream mixed-case spelling
	// "Hy" (families_gen.go), while every catalog entity the decomposition actually
	// produces carries the lowercase "hy". Family comparison is byte-exact, so the
	// two are distinct families and the generated constant does not make the live
	// one known. Registering the lowercase spelling attributes the served entities;
	// reconciling the two spellings is a family-set repair, not a creator one.
	Family("hy"),
	// The inkling / kling halves of the three-way ling collision, registered as
	// LITERALS rather than constants because no FamilyInkling / FamilyKling constant
	// exists: the upstream catalog ships raw_family "ling" on every one of those rows,
	// so neither token ever reached allFamilies (families_gen.go) and the generated set
	// cannot name them. They become first-class known families here so the curated
	// family-token guards (creator, lineage, nomen-claim, suppression) accept them the
	// moment the split lands, in the same regen pass that creates their entities.
	Family("inkling"), // Thinking Machines' Inkling, 6 instances
	Family("kling"),   // Kuaishou's KlingAI video line, 8 klingai/kling-v* rows + qiniu's kling-v2-6
}

// String returns the string representation of the family.
func (f Family) String() string {
	return string(f)
}

// MarshalText implements encoding.TextMarshaler.
// It is permissive: any Family value (known or unknown) can be marshaled.
func (f Family) MarshalText() ([]byte, error) {
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It is permissive: any byte slice is accepted; use IsKnown() to validate.
func (f *Family) UnmarshalText(b []byte) error {
	if f == nil {
		return fmt.Errorf("bestiary: Family.UnmarshalText: nil receiver")
	}
	*f = Family(b)
	return nil
}

// Provider-preference scoring: the single authority behind every provider-preference
// site in resolution and formatting, so the five sites cannot drift apart.
//
// providerPreferenceScore maps a (family, provider) pair to a total order — LOWER is
// more preferred — across two LAYERED axes. The order is deliberate:
//
//   - A provider that is one of the family CREATOR's curated distribution surfaces
//     scores by its INDEX in that curated list, so the curation itself expresses
//     primacy: Zhipu's row leads with its own "zhipuai" API ahead of the
//     international "zai" brand, and a lexicographic tie-break can never quietly
//     reorder them. This tier is the originator answering for its own weights, so it
//     outranks everything.
//   - A provider that equals Family.CanonicalProvider() scores
//     providerScoreCanonical. This axis is UNCHANGED and still consulted in full; it
//     simply now sits beneath the creator tier rather than at the top. It is never
//     bypassed: a family with no creator, no curated distribution row, or no
//     creator-hosted instance among the candidates resolves exactly as it did before
//     this scoring existed.
//   - Everything else scores providerScoreRehost.
//
// The two axes genuinely differ. Anthropic both creates and hosts Claude, so its
// creator surface and its canonical provider coincide and the scoring changes
// nothing. Meta creates Llama but Family.CanonicalProvider reports "local", which no
// catalog instance is served by — there the creator tier is what makes a first-party
// llama.com row win over an arbitrary rehost.
const (
	// providerScoreCreatorMax bounds the creator tier. A curated distribution list
	// longer than this would collide with the canonical tier; no real lab operates
	// anywhere near this many surfaces, and the index is clamped rather than allowed
	// to wrap, so an implausible list degrades to "least-preferred creator surface"
	// instead of silently outranking nothing.
	providerScoreCreatorMax = 900
	// providerScoreCanonical is the score of Family.CanonicalProvider().
	providerScoreCanonical = 1000
	// providerScoreRehost is the score of every other provider.
	providerScoreRehost = 2000
)

// providerPreferenceScore returns the preference score of provider p for family f.
// Lower is more preferred; see the block comment above for the tiers.
//
// An empty provider always scores providerScoreRehost: it can never be evidence of
// first-party hosting, and treating it as canonical would let an unset field win a
// tie-break.
func providerPreferenceScore(f Family, p Provider) int {
	if p == "" {
		return providerScoreRehost
	}
	for i, cp := range f.Creator().Providers() {
		if p == cp {
			if i >= providerScoreCreatorMax {
				return providerScoreCreatorMax - 1
			}
			return i
		}
	}
	if canon := f.CanonicalProvider(); canon != "" && p == canon {
		return providerScoreCanonical
	}
	return providerScoreRehost
}

// isCreatorProvider reports whether p is one of f's creator's curated distribution
// surfaces.
func isCreatorProvider(f Family, p Provider) bool {
	return providerPreferenceScore(f, p) < providerScoreCreatorMax
}

// isRehostProvider reports whether p hosts f first-party for NEITHER originating
// axis — neither a creator distribution surface nor the family's canonical provider.
func isRehostProvider(f Family, p Provider) bool {
	return providerPreferenceScore(f, p) >= providerScoreRehost
}

// preferredCreatorProvider returns the single most-preferred creator distribution
// surface for family f that is actually present among providers, and whether one was
// found. "Most preferred" is the EARLIEST entry in the curated list, so the answer is
// a curation decision rather than an accident of slice or map order.
//
// It returns false when the family has no creator, its creator has no curated
// distribution row, or none of that creator's surfaces appear among providers — the
// three cases in which resolution must fall through to the canonical-provider
// preference untouched rather than narrow the candidate set to nothing.
func preferredCreatorProvider(f Family, providers []Provider) (Provider, bool) {
	best := Provider("")
	bestScore := providerScoreCreatorMax
	for _, p := range providers {
		score := providerPreferenceScore(f, p)
		if score < bestScore {
			best, bestScore = p, score
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}
