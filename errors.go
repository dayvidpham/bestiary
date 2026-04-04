package bestiary

import "fmt"

// ErrNotFound is returned when a requested resource cannot be located in the
// local store or remote API.
//
// What identifies the resource kind (e.g., "model"), and Key is the lookup
// value that was not found. Use errors.As to extract structured fields.
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
