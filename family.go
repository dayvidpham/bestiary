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
		// OpenAI is the canonical publisher for gpt-family models (includes chatgpt-* IDs,
		// which carry Family="gpt") and o-family models (o1, o3, o4 carry Family="o").
		return ProviderOpenAI
	case FamilyLlama:
		// Meta's Llama models are published under the "local" provider (project decision).
		return ProviderLocal
	case FamilyMistral, FamilyCodestral, FamilyDevstral:
		// Mistral AI is the canonical publisher for mistral, codestral, and devstral families.
		return ProviderMistral
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

// curatedBaseFamilies are hand-maintained base families that the models.dev API
// does not surface as a bare family value but which are required as canonical /
// lineage references (e.g. as a derivation parent). IsKnown consults these in
// addition to the generated allFamilies, so a curated family is a first-class
// known Family. Keep this list minimal: add a base family only when a real
// reference (lineage parent, canonical-provider mapping) needs it. The yi base
// family (01.AI) is already present in allFamilies and so is NOT repeated here.
var curatedBaseFamilies = [...]Family{
	FamilySolar,      // base for upstage SOLAR (allFamilies has only solar-mini/solar-pro)
	FamilyMythologic, // MythoMax merge parent
	FamilyHuginn,     // MythoMax merge parent
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
