package bestiary

import "fmt"

// Provider identifies the organization that hosts or publishes an AI model.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderGoogle    Provider = "google"
	ProviderOpenAI    Provider = "openai"
	ProviderLocal     Provider = "local"
)

// knownProviders is the authoritative set of recognized Provider values.
var knownProviders = [...]Provider{
	ProviderAnthropic,
	ProviderGoogle,
	ProviderOpenAI,
	ProviderLocal,
}

// IsKnown reports whether p is one of the four declared Provider constants.
func (p Provider) IsKnown() bool {
	for _, known := range knownProviders {
		if p == known {
			return true
		}
	}
	return false
}

// String returns the string representation of the provider.
func (p Provider) String() string {
	return string(p)
}

// MarshalText implements encoding.TextMarshaler.
func (p Provider) MarshalText() ([]byte, error) {
	return []byte(p), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts any string value; use IsKnown() to validate.
func (p *Provider) UnmarshalText(b []byte) error {
	if p == nil {
		return fmt.Errorf("bestiary: Provider.UnmarshalText: nil receiver")
	}
	*p = Provider(b)
	return nil
}
