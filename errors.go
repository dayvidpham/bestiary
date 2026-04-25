package bestiary

import (
	"fmt"
	"strings"
)

// ErrNotFound is returned when a requested resource cannot be located in the
// local store or remote API.
//
// What identifies the resource kind (e.g., "model"), and Key is the lookup
// value that was not found. Use errors.As to extract structured fields.
//
// ErrNotFound has no Unwrap method by design: it is a sentinel-style error with
// no wrapped cause. Use errors.As to match.
type ErrNotFound struct {
	// What describes the resource kind that was not found (e.g., "model").
	What string
	// Key is the identifier that was searched for (e.g., model ID).
	Key string
}

// Error implements the error interface.
// Format: "bestiary: <what> not found: <key>"
func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("bestiary: %s not found: %s", e.What, e.Key)
}

// ErrAPIUnavailable is returned when the models.dev API cannot be reached after
// one or more attempts. Cause holds the underlying network or HTTP error.
//
// Use errors.As to extract structured fields; use Unwrap to inspect the root
// cause via errors.Is.
type ErrAPIUnavailable struct {
	// URL is the endpoint that was contacted.
	URL string
	// Attempts is how many times the request was tried before giving up.
	Attempts int
	// Cause is the last error returned by the HTTP client.
	Cause error
}

// Error implements the error interface.
// Format: "bestiary: API unavailable after <n> attempt(s) at <url>: <cause>"
func (e *ErrAPIUnavailable) Error() string {
	return fmt.Sprintf("bestiary: API unavailable after %d attempt(s) at %s: %v", e.Attempts, e.URL, e.Cause)
}

// Unwrap returns the underlying cause so that errors.Is and errors.As can
// traverse the error chain.
func (e *ErrAPIUnavailable) Unwrap() error {
	return e.Cause
}

// ErrAmbiguous is returned by Resolve when the input string matches models with
// two or more distinct Canonical identities (e.g., "claude" matches claude/opus,
// claude/sonnet, and claude/haiku simultaneously).
//
// It is distinct from cross-provider hosting: if multiple providers host the
// same Canonical, Resolve returns []ModelRef with err == nil.
//
// What: the raw input string that triggered the ambiguity.
// Scheme: the CanonicalScheme used during resolution.
// Candidates: the list of ModelRefs that matched, one per distinct Canonical.
//
// Use errors.As to extract structured fields. The Error() message names all 6
// actionable-error elements per [C-actionable-errors]:
//  1. What went wrong (ambiguous input),
//  2. Why it happened (multiple distinct canonicals matched),
//  3. Where it failed (Resolve),
//  4. When it failed (during model lookup / resolution step),
//  5. What it means for the caller (the query cannot be resolved unambiguously),
//  6. How to fix it (refine input or use --scheme=raw).
type ErrAmbiguous struct {
	// Input is the raw string passed to Resolve.
	Input string
	// Scheme is the CanonicalScheme that was active during resolution.
	Scheme CanonicalScheme
	// Candidates lists all matching ModelRefs, grouped by distinct Canonical.
	Candidates []ModelRef
}

// Error implements the error interface with an actionable message.
// All 6 elements from the [C-actionable-errors] constraint are present:
// What, Why, Where, When, What it means for the caller, and How to fix.
func (e *ErrAmbiguous) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb,
		"bestiary: ambiguous model input %q (scheme=%s) matched %d distinct canonical(s)\n",
		e.Input, e.Scheme, len(e.Candidates),
	)
	sb.WriteString("  What: input matches multiple distinct model canonicals\n")
	sb.WriteString("  Why: the input string is a prefix or substring shared by several models\n")
	sb.WriteString("  Where: Resolve\n")
	sb.WriteString("  When: during model lookup — after scheme detection, before returning results\n")
	sb.WriteString("  What it means for caller: the query is underspecified; no single model can be returned\n")
	if len(e.Candidates) > 0 {
		sb.WriteString("  Candidates:\n")
		for _, c := range e.Candidates {
			fmt.Fprintf(&sb, "    - %s (provider: %s)\n",
				c.Format(SchemeCanonical), c.Provider)
		}
	}
	sb.WriteString("  How to fix: refine the input to a more specific model ID, or use --scheme=raw with an exact API ID")
	return sb.String()
}
