package bestiary_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// --------------------------------------------------------------------------
// Families() property tests (migrated from provider_test.go — bestiary-49xa)
// --------------------------------------------------------------------------

// TestFamilies_AllNonEmpty verifies no empty string in Families().
func TestFamilies_AllNonEmpty(t *testing.T) {
	for _, f := range bestiary.Families() {
		if f == "" {
			t.Error("Families() contains an empty string")
		}
	}
}

// TestFamilies_MinimumCount is a regression guard: the family set must not
// collapse. We expect at least 50 families (~159 at time of writing).
func TestFamilies_MinimumCount(t *testing.T) {
	if n := len(bestiary.Families()); n < 50 {
		t.Errorf("Families() returned %d families, want >= 50", n)
	}
}

// TestFamilies_DefensiveCopy verifies that modifying the returned slice does
// not affect subsequent calls (allFamilies is an array; copy must be returned).
func TestFamilies_DefensiveCopy(t *testing.T) {
	a := bestiary.Families()
	b := bestiary.Families()
	if len(a) == 0 {
		t.Fatal("Families() returned empty")
	}
	a[0] = "MODIFIED"
	if b[0] == "MODIFIED" {
		t.Error("Families() returned a reference to internal state")
	}
}

// TestFamily_IsKnown verifies positive and negative cases for Family.IsKnown().
func TestFamily_IsKnown(t *testing.T) {
	t.Run("known families are recognized", func(t *testing.T) {
		known := []bestiary.Family{
			bestiary.FamilyClaude,
			bestiary.FamilyGemini,
			bestiary.FamilyGPT,
			bestiary.FamilyLlama,
			bestiary.FamilyMistral,
		}
		for _, f := range known {
			if !f.IsKnown() {
				t.Errorf("Family(%q).IsKnown() = false, want true", f)
			}
		}
	})

	t.Run("all Families() values are known", func(t *testing.T) {
		for _, f := range bestiary.Families() {
			if !f.IsKnown() {
				t.Errorf("Families() contains %q but IsKnown() returns false", f)
			}
		}
	})

	t.Run("unknown families are rejected", func(t *testing.T) {
		unknown := []bestiary.Family{
			"",
			"CLAUDE",
			"not-a-real-family",
			"gpt-5-ultra",
		}
		for _, f := range unknown {
			if f.IsKnown() {
				t.Errorf("Family(%q).IsKnown() = true, want false", f)
			}
		}
	})
}

// TestFamily_String verifies that String() returns the underlying string value.
func TestFamily_String(t *testing.T) {
	cases := []struct {
		f    bestiary.Family
		want string
	}{
		{bestiary.FamilyClaude, "claude"},
		{bestiary.FamilyGemini, "gemini"},
		{bestiary.FamilyGPT, "gpt"},
		{bestiary.FamilyLlama, "llama"},
		{bestiary.Family("custom-value"), "custom-value"},
		{bestiary.Family(""), ""},
	}
	for _, tc := range cases {
		if got := tc.f.String(); got != tc.want {
			t.Errorf("Family(%q).String() = %q, want %q", tc.f, got, tc.want)
		}
	}
}

// TestFamily_RoundTrip verifies that MarshalText → UnmarshalText is idempotent
// for both known and unknown Family values.
func TestFamily_RoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		family bestiary.Family
	}{
		{"claude", bestiary.FamilyClaude},
		{"gemini", bestiary.FamilyGemini},
		{"gpt", bestiary.FamilyGPT},
		{"llama", bestiary.FamilyLlama},
		{"mistral", bestiary.FamilyMistral},
		{"deepseek", bestiary.FamilyDeepseek},
		// Unknown family: permissive contract must round-trip unknown values.
		{"unknown", bestiary.Family("totally-unknown-family-xyz")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b, err := tc.family.MarshalText()
			if err != nil {
				t.Fatalf("Family(%q).MarshalText() error = %v", tc.family, err)
			}
			var got bestiary.Family
			if err := got.UnmarshalText(b); err != nil {
				t.Fatalf("Family.UnmarshalText(%q) error = %v", b, err)
			}
			if got != tc.family {
				t.Errorf("round-trip: got %q, want %q", got, tc.family)
			}
		})
	}
}

// TestFamily_UnmarshalUnknownAccepted verifies the permissive contract:
// UnmarshalText accepts any string, including unknown family names.
// Callers are expected to use IsKnown() separately for validation.
func TestFamily_UnmarshalUnknownAccepted(t *testing.T) {
	unknown := []string{
		"totally-unknown-family",
		"",
		"UPPER-CASE",
		"spaces in name",
		"unicode-漢字",
	}
	for _, s := range unknown {
		var f bestiary.Family
		if err := f.UnmarshalText([]byte(s)); err != nil {
			t.Errorf("Family.UnmarshalText(%q) returned error = %v, want nil (permissive contract)", s, err)
			continue
		}
		if string(f) != s {
			t.Errorf("Family.UnmarshalText(%q): got %q, want %q", s, f, s)
		}
	}
}

// TestFamily_UnmarshalText_NilReceiver verifies the nil-receiver guard in
// UnmarshalText. The guard `if f == nil` protects against a direct call on
// a typed nil *Family pointer. A nil interface value can never dispatch to
// a method in Go, so this test exercises the concrete-pointer nil path.
//
// The intent of the guard is defensive: it produces an actionable error instead
// of a nil-pointer dereference when a caller passes a nil pointer directly.
func TestFamily_UnmarshalText_NilReceiver(t *testing.T) {
	var f *bestiary.Family // typed nil concrete pointer
	err := f.UnmarshalText([]byte("claude"))
	if err == nil {
		t.Error("Family.UnmarshalText on nil receiver: want error, got nil")
	}
	msg := err.Error()
	if msg == "" {
		t.Error("Family.UnmarshalText on nil receiver: error message is empty")
	}
}

// --- Fix #4: Family.CanonicalProvider method ---

// TestFamily_CanonicalProvider_WellKnown verifies that CanonicalProvider returns
// the correct canonical provider for well-known model families.
//
// Fix #4 (SLICE-FIX-V2-2): "For now, we can just determine the canonical providers
// for the most popular models and stub the rest with a placeholder value."
func TestFamily_CanonicalProvider_WellKnown(t *testing.T) {
	cases := []struct {
		family   bestiary.Family
		wantProv bestiary.Provider
	}{
		{bestiary.FamilyClaude, bestiary.ProviderAnthropic},
		{bestiary.FamilyClaudeOpus, bestiary.ProviderAnthropic},
		{bestiary.FamilyClaudeSonnet, bestiary.ProviderAnthropic},
		{bestiary.FamilyClaudeHaiku, bestiary.ProviderAnthropic},
		{bestiary.FamilyGemini, bestiary.ProviderGoogle},
		{bestiary.FamilyGemma, bestiary.ProviderGoogle},
		{bestiary.FamilyGPT, bestiary.ProviderOpenAI},
		{bestiary.FamilyO, bestiary.ProviderOpenAI},  // o1, o3, o4 carry Family="o"
		{bestiary.FamilyLlama, bestiary.ProviderLocal},
		{bestiary.FamilyMistral, bestiary.ProviderMistral},
		{bestiary.FamilyDeepseek, bestiary.ProviderDeepSeek},
		{bestiary.FamilyQwen, bestiary.ProviderAlibaba},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.family), func(t *testing.T) {
			got := tc.family.CanonicalProvider()
			if got != tc.wantProv {
				t.Errorf("Family(%q).CanonicalProvider() = %q, want %q",
					tc.family, got, tc.wantProv)
			}
		})
	}
}

// TestFamily_CanonicalProvider_UnknownReturnsEmpty verifies that unknown families
// return empty Provider (not a wrong-but-plausible guess, not panic).
//
// Fix #4 spec: "Placeholder MUST NOT be a wrong-but-plausible guess. Empty Provider only."
func TestFamily_CanonicalProvider_UnknownReturnsEmpty(t *testing.T) {
	unknowns := []bestiary.Family{
		bestiary.Family(""),
		bestiary.Family("totally-unknown-family"),
		bestiary.Family("grok"),  // not mapped in Wave 3
		bestiary.Family("nova"),
		bestiary.Family("sonar"),
		bestiary.Family("kimi"),
	}

	for _, f := range unknowns {
		f := f
		t.Run(string(f), func(t *testing.T) {
			got := f.CanonicalProvider()
			if got != "" {
				t.Errorf("Family(%q).CanonicalProvider() = %q, want empty string for unknown family",
					f, got)
			}
		})
	}
}

// TestFamily_CanonicalProvider_NeverPanics verifies that CanonicalProvider never
// panics for any family value, including edge cases.
func TestFamily_CanonicalProvider_NeverPanics(t *testing.T) {
	edgeCases := []bestiary.Family{
		"",
		"a",
		"UPPER-CASE",
		"with spaces",
		"unicode-漢字",
		bestiary.Family("claude"),
	}
	for _, f := range edgeCases {
		// Should not panic.
		_ = f.CanonicalProvider()
	}
}

// TestFamily_CanonicalProvider_AllKnownFamiliesHandled verifies that all families
// in Families() either return a known Provider or empty string (never a random value).
func TestFamily_CanonicalProvider_AllKnownFamiliesHandled(t *testing.T) {
	knownProviders := make(map[bestiary.Provider]bool)
	for _, p := range bestiary.Providers() {
		knownProviders[p] = true
	}
	knownProviders[""] = true // empty is valid sentinel for "unknown"

	for _, f := range bestiary.Families() {
		got := f.CanonicalProvider()
		if !knownProviders[got] {
			t.Errorf("Family(%q).CanonicalProvider() = %q, which is not a known Provider or empty",
				f, got)
		}
	}
}

// TestFamilyType_NamedNotAlias is a regression guard against families_gen.go
// accidentally defining Family as a type alias (type Family = string) instead
// of a named type (type Family string). A type alias would break type safety
// at call sites — e.g., bare string literals would satisfy Family parameters
// without any compiler check.
//
// It reads families_gen.go directly from disk and asserts:
//   - "type Family string"   is present (named type declaration)
//   - "type Family = string" is absent  (alias declaration — must never appear)
func TestFamilyType_NamedNotAlias(t *testing.T) {
	// Locate families_gen.go relative to this test file using runtime.Caller.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("TestFamilyType_NamedNotAlias: runtime.Caller(0) failed — cannot locate source file")
	}
	genFile := filepath.Join(filepath.Dir(thisFile), "families_gen.go")

	data, err := os.ReadFile(genFile)
	if err != nil {
		t.Fatalf("TestFamilyType_NamedNotAlias: read %s: %v — ensure families_gen.go is present (run go generate ./...)", genFile, err)
	}
	content := string(data)

	const namedDecl = "type Family string"
	const aliasDecl = "type Family = string"

	if !strings.Contains(content, namedDecl) {
		t.Errorf("TestFamilyType_NamedNotAlias: %q not found in families_gen.go — Family must be a named type, not an alias", namedDecl)
	}
	if strings.Contains(content, aliasDecl) {
		t.Errorf("TestFamilyType_NamedNotAlias: %q found in families_gen.go — Family must NOT be a type alias (use named type instead)", aliasDecl)
	}
}
