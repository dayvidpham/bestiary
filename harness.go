package bestiary

import "fmt"

// Harness identifies the coding tool or AI-assisted development environment
// that is driving the model interaction.
type Harness string

const (
	HarnessClaudeCode  Harness = "claude-code"
	HarnessGeminiCLI   Harness = "gemini-cli"
	HarnessCodex       Harness = "codex"
	HarnessOpenCode    Harness = "opencode"
	HarnessCursor      Harness = "cursor"
	HarnessAntigravity Harness = "antigravity"
	HarnessStrike      Harness = "strike"
	HarnessPi          Harness = "pi"
)

// knownHarnesses is the authoritative set of recognized Harness values.
var knownHarnesses = [...]Harness{
	HarnessClaudeCode,
	HarnessGeminiCLI,
	HarnessCodex,
	HarnessOpenCode,
	HarnessCursor,
	HarnessAntigravity,
	HarnessStrike,
	HarnessPi,
}

// NewHarness validates a wire value and returns its canonical Harness.
func NewHarness(value string) (Harness, error) {
	h := Harness(value)
	if !h.IsKnown() {
		return "", fmt.Errorf("bestiary: NewHarness: harness %q is not recognized; use one of %v", value, Harnesses())
	}
	return h, nil
}

// Harnesses returns all known Harness values. The returned slice is a
// defensive copy — modifying it does not affect the package state.
func Harnesses() []Harness {
	out := make([]Harness, len(knownHarnesses))
	copy(out, knownHarnesses[:])
	return out
}

// IsKnown reports whether h is one of the declared Harness constants.
func (h Harness) IsKnown() bool {
	for _, known := range knownHarnesses {
		if h == known {
			return true
		}
	}
	return false
}

// String returns the string representation of the harness.
func (h Harness) String() string {
	return string(h)
}

// MarshalText implements encoding.TextMarshaler.
func (h Harness) MarshalText() ([]byte, error) {
	return []byte(h), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts any string value; use IsKnown() to validate.
func (h *Harness) UnmarshalText(b []byte) error {
	if h == nil {
		return fmt.Errorf("bestiary: Harness.UnmarshalText: nil receiver")
	}
	*h = Harness(b)
	return nil
}
