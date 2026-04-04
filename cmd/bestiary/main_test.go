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
