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
		// Meta's Llama models are published under the "local" provider per team-lead direction.
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
// and is stored in allFamilies (families_gen.go).
func (f Family) IsKnown() bool {
	for _, known := range allFamilies {
		if f == known {
			return true
		}
	}
	return false
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
