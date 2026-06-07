package bestiary_test

import (
	"errors"
	"os/exec"
	"testing"
)

// checkIgnored reports whether git ignores the given repo-relative path. git
// check-ignore exits 0 when the path IS ignored and 1 when it is NOT; any other
// exit (e.g. 128 — not a work tree) surfaces as an error so the caller can skip.
func checkIgnored(t *testing.T, path string) (bool, error) {
	t.Helper()
	cmd := exec.Command("git", "check-ignore", "-q", path)
	err := cmd.Run()
	if err == nil {
		return true, nil // exit 0: ignored
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil // exit 1: not ignored
	}
	return false, err // 128/other: not a usable git work tree
}

// TestGitignore_CmdBestiaryNotIgnored (VC15) guards the .gitignore hygiene fix:
// the binary-ignore rule must be ROOT-ANCHORED (/bestiary) so it ignores only the
// built ./bestiary binary at the repo root — NOT the cmd/bestiary/ source
// directory. An unanchored "bestiary" matches every path component named
// bestiary, silently swallowing new files under cmd/bestiary/ (which previously
// forced `git add -f`). The built root binary must still be ignored.
func TestGitignore_CmdBestiaryNotIgnored(t *testing.T) {
	// A hypothetical NEW source file under cmd/bestiary/ must be trackable.
	ignored, err := checkIgnored(t, "cmd/bestiary/some_new_file_test.go")
	if err != nil {
		t.Skipf("git check-ignore unavailable (not a git work tree?): %v", err)
	}
	if ignored {
		t.Error("cmd/bestiary/some_new_file_test.go is git-ignored; the binary rule must be root-anchored (/bestiary) so cmd/bestiary/ source is never ignored")
	}

	// The built binary at the repo root must STILL be ignored.
	ignored, err = checkIgnored(t, "bestiary")
	if err != nil {
		t.Skipf("git check-ignore unavailable: %v", err)
	}
	if !ignored {
		t.Error("the root ./bestiary binary is no longer ignored; /bestiary must still match the built binary")
	}
}
