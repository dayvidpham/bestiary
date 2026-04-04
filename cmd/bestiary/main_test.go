package main

import (
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

// TestRun_List verifies that the list subcommand succeeds when given an isolated
// --db-path backed by a temporary directory (so no real user state is touched).
func TestRun_List(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	err := run([]string{"list", "--db-path", tmpDB})
	if err != nil {
		t.Fatalf("run([\"list\", \"--db-path\", %q]) returned unexpected error: %v", tmpDB, err)
	}
}

// TestRun_ShowNoID verifies that "bestiary show" without a model ID argument
// returns an error describing the missing argument.
func TestRun_ShowNoID(t *testing.T) {
	err := run([]string{"show"})
	if err == nil {
		t.Fatal("run([]string{\"show\"}) returned nil; expected an error about missing model ID")
	}
	// The error should give enough context to guide the caller.
	msg := err.Error()
	if msg == "" {
		t.Error("run([]string{\"show\"}) returned a non-nil but empty error message")
	}
}
