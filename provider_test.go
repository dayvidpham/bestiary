package bestiary_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestProviders_AllKnown verifies that every value returned by Providers()
// is recognized by IsKnown().
func TestProviders_AllKnown(t *testing.T) {
	for _, p := range bestiary.Providers() {
		if !p.IsKnown() {
			t.Errorf("Providers() contains %q but IsKnown() returns false", p)
		}
	}
}

// TestProviders_IncludesLocal verifies that Providers() always includes ProviderLocal.
func TestProviders_IncludesLocal(t *testing.T) {
	for _, p := range bestiary.Providers() {
		if p == bestiary.ProviderLocal {
			return
		}
	}
	t.Error("Providers() does not include ProviderLocal")
}

// TestProviders_MinimumCount is a regression guard: the provider set must not
// collapse. We expect at least 50 providers (110 API + Local at time of writing).
func TestProviders_MinimumCount(t *testing.T) {
	if n := len(bestiary.Providers()); n < 50 {
		t.Errorf("Providers() returned %d providers, expected at least 50 (regression guard)", n)
	}
}

// TestProviderIsKnown_KnownProviders verifies that core providers are recognized.
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

// TestProviderIsKnown_UnknownProviders verifies that clearly unknown values are rejected.
func TestProviderIsKnown_UnknownProviders(t *testing.T) {
	unknown := []bestiary.Provider{
		"",
		"ANTHROPIC",
		"not-a-real-provider",
	}
	for _, p := range unknown {
		if p.IsKnown() {
			t.Errorf("Provider(%q).IsKnown() = true, want false", p)
		}
	}
}

// TestGeneratedProviders_MatchSlugs verifies that every generated (non-Local)
// provider constant has a valid slug format: lowercase alphanumeric + hyphens + dots.
// Dots are permitted because the upstream models.dev API uses them in provider IDs
// (e.g. "wafer.ai"). This is a codegen output validation guard.
func TestGeneratedProviders_MatchSlugs(t *testing.T) {
	providers := bestiary.Providers()
	for _, p := range providers {
		if p == bestiary.ProviderLocal {
			continue
		}
		s := string(p)
		if s == "" {
			t.Error("empty string in Providers()")
			continue
		}
		for _, c := range s {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
				t.Errorf("Provider %q contains non-slug character %q", s, string(c))
			}
		}
	}
}

// TestProviders_DefensiveCopy verifies that modifying the returned slice does
// not affect subsequent calls.
func TestProviders_DefensiveCopy(t *testing.T) {
	first := bestiary.Providers()
	originalLen := len(first)
	if originalLen == 0 {
		t.Skip("skipping: Providers() returned empty slice")
	}
	first = first[:0]
	second := bestiary.Providers()
	if len(second) != originalLen {
		t.Fatalf("Providers(): defensive copy broken — after truncating first result, second call returned %d entries (expected %d)",
			len(second), originalLen)
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

// TestProviders_GoldenFile snapshots the sorted list of all provider slugs
// against testdata/providers.golden. To regenerate the golden file, set the
// UPDATE_GOLDEN environment variable before running tests:
//
//	UPDATE_GOLDEN=1 go test -run TestProviders_GoldenFile ./...
func TestProviders_GoldenFile(t *testing.T) {
	goldenPath := filepath.Join("testdata", "providers.golden")

	// Collect and sort the current provider slugs.
	providers := bestiary.Providers()
	slugs := make([]string, len(providers))
	for i, p := range providers {
		slugs[i] = string(p)
	}
	sort.Strings(slugs)
	got := strings.Join(slugs, "\n") + "\n"

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("TestProviders_GoldenFile: mkdir %s: %v", filepath.Dir(goldenPath), err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("TestProviders_GoldenFile: write %s: %v — check directory permissions", goldenPath, err)
		}
		t.Logf("golden file updated: %s (%d providers)", goldenPath, len(slugs))
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("TestProviders_GoldenFile: read %s: %v — regenerate with UPDATE_GOLDEN=1 go test -run TestProviders_GoldenFile ./...", goldenPath, err)
	}
	want := string(wantBytes)

	if got != want {
		t.Errorf("TestProviders_GoldenFile: Providers() does not match golden file %s\n"+
			"Got %d providers, want %d providers.\n"+
			"Diff (first mismatch):\n%s",
			goldenPath, len(slugs), len(strings.Split(strings.TrimSuffix(want, "\n"), "\n")),
			firstDiff(got, want))
	}
}

// --------------------------------------------------------------------------
// helpers
// --------------------------------------------------------------------------

// firstDiff returns the first line that differs between got and want.
// It is used for human-readable golden file mismatch messages.
func firstDiff(got, want string) string {
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want, "\n")
	n := len(gotLines)
	if len(wantLines) < n {
		n = len(wantLines)
	}
	for i := 0; i < n; i++ {
		if gotLines[i] != wantLines[i] {
			return "line " + itoa(i+1) + ": got " + gotLines[i] + ", want " + wantLines[i]
		}
	}
	return "lengths differ: got " + itoa(len(gotLines)) + " lines, want " + itoa(len(wantLines)) + " lines"
}

// itoa converts an int to its decimal string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
