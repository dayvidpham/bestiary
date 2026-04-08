package bestiary

import "fmt"

// Provider identifies the organization that hosts or publishes an AI model.
type Provider string

// ProviderLocal is a bestiary-specific provider for locally-hosted models.
// It is not derived from the models.dev API and is always included in
// knownProviders (generated in providers_gen.go, appended last).
const ProviderLocal Provider = "local"

// All other Provider constants (ProviderAnthropic, ProviderGoogle, ProviderOpenAI,
// and ~107 more) are generated in providers_gen.go by cmd/bestiary-gen.
// knownProviders is also generated in providers_gen.go.

// IsKnown reports whether p is a recognized Provider.
// The known set is generated from the models.dev API at codegen time and includes
// ProviderLocal as the final entry.
func (p Provider) IsKnown() bool {
	for _, known := range knownProviders {
		if p == known {
			return true
		}
	}
	return false
}

// Providers returns all known Provider values as a defensive copy.
// The returned slice includes all API-derived providers plus ProviderLocal.
// Modifying the returned slice does not affect the package state.
func Providers() []Provider {
	out := make([]Provider, len(knownProviders))
	copy(out, knownProviders[:])
	return out
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
