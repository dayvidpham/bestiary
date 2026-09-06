package bestiary_test

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/enum/harness_required_names.json
var harnessRequiredNamesJSON []byte

var harnessRequiredNames = func() []string {
	var names []string
	if err := json.Unmarshal(harnessRequiredNamesJSON, &names); err != nil {
		panic(err)
	}
	return names
}()

func TestHarness_IsKnown(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		if !bestiary.Harness(c.Input).IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = false, want true", c.Input)
		}
	}

	unknown := []bestiary.Harness{
		"",
		"CLAUDE-CODE",
		"unknown-harness",
	}
	for _, h := range unknown {
		if h.IsKnown() {
			t.Errorf("Harness(%q).IsKnown() = true, want false", h)
		}
	}
}

func TestNewHarness(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		got, err := bestiary.NewHarness(c.Input)
		if err != nil {
			t.Errorf("NewHarness(%q) error = %v", c.Input, err)
			continue
		}
		if got.String() != c.Expected {
			t.Errorf("NewHarness(%q) = %q, want %q", c.Input, got, c.Expected)
		}
	}

	if _, err := bestiary.NewHarness("Pi"); err == nil {
		t.Fatal("NewHarness(Pi) error = nil, want unknown harness rejection")
	}
}

// TestHarness_String drives Harness.String() over every well-known constant, loaded
// from testdata/enum/harness_string_corpus.json. requireHarnessKnown additionally
// pins that each corpus token is still a recognized Harness.
func TestHarness_String(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	requireHarnessKnown(t, corpus)
	runEnumStringCorpus(t, corpus, func(_ *testing.T, in string) string {
		return bestiary.Harness(in).String()
	})
}

func TestHarness_MarshalUnmarshalText(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	for _, c := range corpus.Cases {
		h := bestiary.Harness(c.Input)
		b, err := h.MarshalText()
		if err != nil {
			t.Errorf("Harness(%q).MarshalText() error = %v", h, err)
			continue
		}
		var got bestiary.Harness
		if err := got.UnmarshalText(b); err != nil {
			t.Errorf("Harness.UnmarshalText(%q) error = %v", b, err)
			continue
		}
		if got != h {
			t.Errorf("round-trip: got %q, want %q", got, h)
		}
	}
}

func TestHarness_UnmarshalText_Permissive(t *testing.T) {
	var h bestiary.Harness
	if err := h.UnmarshalText([]byte("some-unknown-harness")); err != nil {
		t.Fatalf("UnmarshalText(unknown) error = %v, want nil", err)
	}
	if h.IsKnown() {
		t.Errorf("Harness(%q).IsKnown() = true, want false for unknown value", h)
	}
}

func TestHarness_UnmarshalText_NilReceiver(t *testing.T) {
	var h *bestiary.Harness
	err := h.UnmarshalText([]byte("claude-code"))
	if err == nil {
		t.Fatal("UnmarshalText on nil receiver: got nil error, want error")
	}
}

func TestHarnesses_AllKnown(t *testing.T) {
	corpus := loadHarnessCorpus(t)
	requireHarnessNames(t, corpus)
	want := make(map[bestiary.Harness]bool, len(corpus.Cases))
	for _, c := range corpus.Cases {
		want[bestiary.Harness(c.Input)] = true
	}
	got := bestiary.Harnesses()
	seen := make(map[bestiary.Harness]bool, len(got))
	for _, h := range got {
		if seen[h] {
			t.Errorf("Harnesses() returned duplicate %q", h)
		}
		seen[h] = true
		if !h.IsKnown() {
			t.Errorf("Harnesses() returned %q which IsKnown() = false", h)
		}
		if !want[h] {
			t.Errorf("Harnesses() returned unexpected %q", h)
		}
		delete(want, h)
	}
	for missing := range want {
		t.Errorf("Harnesses() omitted %q", missing)
	}
}

func TestHarnesses_DefensiveCopy(t *testing.T) {
	first := bestiary.Harnesses()
	first[0] = "tampered"
	second := bestiary.Harnesses()
	if second[0] == "tampered" {
		t.Error("Harnesses() returned a non-defensive copy: mutation affected subsequent call")
	}
}
