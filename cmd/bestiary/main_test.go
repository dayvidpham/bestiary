package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// TestRun_NoArgs verifies that calling run with no arguments returns a usage error.
func TestRun_NoArgs(t *testing.T) {
	err := run([]string{})
	if err == nil {
		t.Fatal("run([]string{}) returned nil; expected a usage error")
	}
	msg := err.Error()
	// The usage message should guide the caller toward valid subcommands.
	if !strings.Contains(strings.ToLower(msg), "usage") {
		t.Errorf("run([]string{}) error = %q; expected message to contain %q", msg, "usage")
	}
}

// TestRun_UnknownCommand verifies that an unrecognised subcommand returns a
// descriptive error mentioning the unknown command name.
func TestRun_UnknownCommand(t *testing.T) {
	err := run([]string{"bogus"})
	if err == nil {
		t.Fatal("run([]string{\"bogus\"}) returned nil; expected an error for unknown command")
	}
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "unknown") {
		t.Errorf("run([]string{\"bogus\"}) error = %q; expected message to contain %q", msg, "unknown")
	}
}

// TestRun_List verifies that the list subcommand succeeds and writes table output
// when given an isolated --db-path backed by a temporary directory (so no real
// user state is touched).
func TestRun_List(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	// Capture os.Stdout so we can assert on the output content.
	// Read from the pipe concurrently to avoid deadlock when output
	// exceeds the OS pipe buffer (~64KB) — the static registry is large.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(): %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	runErr := run([]string{"list", "--db-path", tmpDB})

	w.Close()
	os.Stdout = old
	<-done

	output := buf.String()

	if runErr != nil {
		t.Fatalf("run([\"list\", \"--db-path\", %q]) returned unexpected error: %v", tmpDB, runErr)
	}
	// The default format is JSON; static registry is non-empty so the output
	// must contain the "Provider" field key.
	if !strings.Contains(output, "Provider") {
		t.Errorf("run([\"list\"]) output does not contain \"Provider\"; got %q", output)
	}
}

// TestRun_ShowNoID verifies that "bestiary show" without a model ID argument
// returns an error describing the missing argument.
func TestRun_ShowNoID(t *testing.T) {
	err := run([]string{"show"})
	if err == nil {
		t.Fatal("run([]string{\"show\"}) returned nil; expected an error about missing model ID")
	}
	// The error should give enough context to guide the caller toward the
	// correct usage, specifically mentioning usage instructions or model-id.
	msg := err.Error()
	if !strings.Contains(strings.ToLower(msg), "usage") && !strings.Contains(strings.ToLower(msg), "model") {
		t.Errorf("run([]string{\"show\"}) error = %q; expected message to contain \"usage\" or \"model\"", msg)
	}
}

// captureStdout redirects os.Stdout to a pipe, calls fn, then restores
// os.Stdout and returns the accumulated output. Safe for concurrent use
// within a single test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStdout: os.Pipe(): %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stdout = old
	<-done
	return buf.String()
}

// captureStderr redirects os.Stderr to a pipe, calls fn, then restores
// os.Stderr and returns the accumulated output.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("captureStderr: os.Pipe(): %v", err)
	}
	os.Stderr = w

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	fn()

	w.Close()
	os.Stderr = old
	<-done
	return buf.String()
}

// TestShow_SchemeRaw verifies that bestiary show <raw-id> --format raw resolves
// a model by exact model ID and prints its JSON to stdout.
//
// "claude-opus-4-1" is a known model ID in the static registry.
// The --format raw flag is required for exact-ID lookup; without it, the default
// peasant (canonical) mode would treat the ID as a canonical form and may produce
// ErrAmbiguous if multiple canonical groups match.
func TestShow_SchemeRaw(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--format", "raw", "--db-path", tmpDB, "claude-opus-4-1"})
	})

	if runErr != nil {
		t.Fatalf("run show --format raw claude-opus-4-1 returned error: %v", runErr)
	}
	if !strings.Contains(out, "claude-opus-4-1") {
		t.Errorf("show output does not contain model ID %q; got %q", "claude-opus-4-1", out)
	}
}

// TestShow_SchemeHuggingFace verifies that bestiary show <provider>/<raw-id>
// with --format huggingface resolves the model by stripping the provider prefix.
//
// "anthropic/claude-opus-4-1" with --format huggingface should resolve to "claude-opus-4-1".
func TestShow_SchemeHuggingFace(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--format", "huggingface", "--db-path", tmpDB, "anthropic/claude-opus-4-1"})
	})

	if runErr != nil {
		t.Fatalf("run show --format huggingface anthropic/claude-opus-4-1 returned error: %v", runErr)
	}
	if !strings.Contains(out, "claude-opus-4-1") {
		t.Errorf("show output does not contain model ID %q; got %q", "claude-opus-4-1", out)
	}
}

// TestShow_SchemePURL verifies that bestiary show pkg:huggingface/<provider>/<raw-id>
// with --format purl resolves the model by stripping both the "pkg:huggingface/"
// prefix and the provider segment.
//
// "pkg:huggingface/anthropic/claude-opus-4-1" with --format purl should resolve
// to "claude-opus-4-1".
func TestShow_SchemePURL(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--format", "purl", "--db-path", tmpDB, "pkg:huggingface/anthropic/claude-opus-4-1"})
	})

	if runErr != nil {
		t.Fatalf("run show --format purl pkg:huggingface/anthropic/claude-opus-4-1 returned error: %v", runErr)
	}
	if !strings.Contains(out, "claude-opus-4-1") {
		t.Errorf("show output does not contain model ID %q; got %q", "claude-opus-4-1", out)
	}
}

// TestShow_Ambiguous verifies that an under-specified input that matches multiple
// distinct canonical triples produces:
//  1. A candidate table on stderr (header, rows, footer).
//  2. A non-zero exit (non-nil error returned by run).
//  3. Nothing on stdout (table goes to stderr only).
//
// "claude" with default --format peasant matches claude/opus, claude/sonnet, etc.
func TestShow_Ambiguous(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	var runErr error
	var errOut string
	out := captureStdout(t, func() {
		errOut = captureStderr(t, func() {
			runErr = run([]string{"show", "--db-path", tmpDB, "claude"})
		})
	})

	// run must return a non-nil error (non-zero exit in main).
	if runErr == nil {
		t.Fatal("run show --scheme canonical claude returned nil error; expected non-zero exit for ambiguous input")
	}

	// stderr must contain the header with the input name.
	if !strings.Contains(errOut, "claude") {
		t.Errorf("stderr does not contain input %q; got %q", "claude", errOut)
	}
	// stderr must contain the column headers.
	if !strings.Contains(errOut, "Canonical") || !strings.Contains(errOut, "Provider") || !strings.Contains(errOut, "Raw ID") {
		t.Errorf("stderr does not contain expected column headers; got %q", errOut)
	}
	// stderr must contain a remediation hint pointing toward --format raw or refinement.
	if !strings.Contains(errOut, "--format") && !strings.Contains(errOut, "refine") && !strings.Contains(errOut, "raw") {
		t.Errorf("stderr does not contain remediation hint (--format or refine); got %q", errOut)
	}
	// stdout must be empty — the candidate table goes to stderr only.
	if out != "" {
		t.Errorf("stdout should be empty for ambiguous input; got %q", out)
	}
}

// TestShow_NotFound verifies that bestiary show with a model ID that does not
// exist in the static registry returns a non-nil error containing "not found".
func TestShow_NotFound(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	err := run([]string{"show", "--db-path", tmpDB, "definitely-not-a-real-model-id-xyz"})
	if err == nil {
		t.Fatal("run show nonexistent-model returned nil; expected ErrNotFound")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q; expected to contain %q", err.Error(), "not found")
	}
}
