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
)

// knownHarnesses is the authoritative set of recognized Harness values.
var knownHarnesses = [...]Harness{
	HarnessClaudeCode,
	HarnessGeminiCLI,
	HarnessCodex,
	HarnessOpenCode,
	HarnessCursor,
	HarnessAntigravity,
}

// IsKnown reports whether h is one of the six declared Harness constants.
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
