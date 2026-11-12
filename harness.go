package bestiary

import "fmt"

// Harness identifies the coding tool or AI-assisted development environment
// that is driving the model interaction.
type Harness string

const (
	HarnessClaudeCode Harness = "claude-code"
	// HarnessGeminiCLI is archived: retained for history, never removed.
	// Gemini CLI was replaced by the Antigravity products. The value stays known so
	// that sessions which already carry it remain valid.
	HarnessGeminiCLI Harness = "gemini-cli"
	HarnessCodex     Harness = "codex"
	HarnessOpenCode  Harness = "opencode"
	HarnessCursor    Harness = "cursor"
	// HarnessAntigravity is archived. It named the Antigravity products as one
	// harness before the command-line agent and the desktop application got their
	// own values. It stays known so that anything which already carries it remains
	// valid.
	//
	// Deprecated: use HarnessAntigravityCLI or HarnessAntigravityIDE. Removal is
	// planned; the conditions and the window are tracked in
	// https://github.com/peasant-labs/schema/issues/232.
	HarnessAntigravity  Harness = "antigravity"
	HarnessStrike       Harness = "strike"
	HarnessPi           Harness = "pi"
	HarnessGrokBuild    Harness = "grok-build"
	HarnessDevinDesktop Harness = "devin-desktop"
	HarnessCline        Harness = "cline"
	HarnessKiro         Harness = "kiro"
	HarnessKiroCLI      Harness = "kiro-cli"
	HarnessKiroCrew     Harness = "kiro-crew"
	// HarnessAntigravityCLI is the Antigravity command-line agent (agy).
	HarnessAntigravityCLI Harness = "antigravity-cli"
	// HarnessAntigravityIDE is the Antigravity desktop application.
	HarnessAntigravityIDE Harness = "antigravity-ide"
)

// archivedHarnesses is the authoritative set of archived Harness values.
var archivedHarnesses = [...]Harness{HarnessGeminiCLI, HarnessAntigravity}

// ArchivedHarnesses returns the known Harness values that are archived: kept so
// that sessions and records which already carry them stay valid, with no
// current product behind the value. Every archived value is known. The set
// does not say whether a value will be removed: HarnessGeminiCLI is never
// removed, and HarnessAntigravity is planned for removal; each constant's
// documentation says which. The returned slice is a copy.
func ArchivedHarnesses() []Harness {
	out := make([]Harness, len(archivedHarnesses))
	copy(out, archivedHarnesses[:])
	return out
}

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
	HarnessGrokBuild,
	HarnessDevinDesktop,
	HarnessCline,
	HarnessKiro,
	HarnessKiroCLI,
	HarnessKiroCrew,
	HarnessAntigravityCLI,
	HarnessAntigravityIDE,
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
