package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

func TestProviderIsKnown_KnownProviders(t *testing.T) {
	known := []bestiary.Provider{
		bestiary.ProviderAnthropic,
		bestiary.ProviderGoogle,
		bestiary.ProviderOpenAI,
		bestiary.ProviderLocal,
	}
	for _, p := range known {
		if !p.IsKnown() {
			t.Errorf("Provider(%q).IsKnown() = false, want true", p)
		}
	}
}

func TestProviderIsKnown_UnknownProviders(t *testing.T) {
	unknown := []bestiary.Provider{
		"openrouter",
		"",
		"ANTHROPIC",
		"mistral",
	}
	for _, p := range unknown {
		if p.IsKnown() {
			t.Errorf("Provider(%q).IsKnown() = true, want false", p)
		}
	}
}

func TestProviderMarshalUnmarshalRoundTrip(t *testing.T) {
	providers := []bestiary.Provider{
		bestiary.ProviderAnthropic,
		bestiary.ProviderGoogle,
		bestiary.ProviderOpenAI,
		bestiary.ProviderLocal,
	}
	for _, p := range providers {
		b, err := p.MarshalText()
		if err != nil {
			t.Errorf("Provider(%q).MarshalText() error = %v", p, err)
			continue
		}
		var got bestiary.Provider
		if err := got.UnmarshalText(b); err != nil {
			t.Errorf("Provider.UnmarshalText(%q) error = %v", b, err)
			continue
		}
		if got != p {
			t.Errorf("round-trip: got %q, want %q", got, p)
		}
	}
}

func TestProviderString(t *testing.T) {
	cases := []struct {
		p    bestiary.Provider
		want string
	}{
		{bestiary.ProviderAnthropic, "anthropic"},
		{bestiary.ProviderGoogle, "google"},
		{bestiary.ProviderOpenAI, "openai"},
		{bestiary.ProviderLocal, "local"},
	}
	for _, tc := range cases {
		if got := tc.p.String(); got != tc.want {
			t.Errorf("Provider(%q).String() = %q, want %q", tc.p, got, tc.want)
		}
	}
}
