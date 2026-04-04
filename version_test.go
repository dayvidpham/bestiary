package bestiary_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestBestiarySchemaVersion_Semver verifies that BestiarySchemaVersion matches
// the semver major.minor.patch format (e.g. "1.0.0").
func TestBestiarySchemaVersion_Semver(t *testing.T) {
	re := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !re.MatchString(bestiary.BestiarySchemaVersion) {
		t.Errorf(
			"BestiarySchemaVersion %q does not match semver major.minor.patch format;\n"+
				"  what went wrong: value does not satisfy regexp %q\n"+
				"  why: const was set to a non-semver string in version.go\n"+
				"  where: bestiary.BestiarySchemaVersion (version.go)\n"+
				"  how to fix: set BestiarySchemaVersion to a string like \"1.0.0\"",
			bestiary.BestiarySchemaVersion, re.String(),
		)
	}
}

// TestUpstreamSchemaVersion_Format verifies that UpstreamSchemaVersion matches
// the YYYY.MM.DD-hex12 format (e.g. "2026.04.04-fd776194f63d").
func TestUpstreamSchemaVersion_Format(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}-[0-9a-f]{12}$`)
	if !re.MatchString(bestiary.UpstreamSchemaVersion) {
		t.Errorf(
			"UpstreamSchemaVersion %q does not match YYYY.MM.DD-hex12 format;\n"+
				"  what went wrong: value does not satisfy regexp %q\n"+
				"  why: const was set to a non-conforming string in version.go\n"+
				"  where: bestiary.UpstreamSchemaVersion (version.go)\n"+
				"  how to fix: set UpstreamSchemaVersion to a string like \"2026.04.04-fd776194f63d\""+
				" (12 lowercase hex characters 0-9, a-f only; uppercase is rejected)",
			bestiary.UpstreamSchemaVersion, re.String(),
		)
	}
}

// TestUpstreamGitCommit_NonEmpty verifies that UpstreamGitCommit is a non-empty
// hex string (short commit hash).
func TestUpstreamGitCommit_NonEmpty(t *testing.T) {
	v := bestiary.UpstreamGitCommit
	if v == "" {
		t.Errorf(
			"UpstreamGitCommit is empty;\n"+
				"  what went wrong: const is an empty string\n"+
				"  why: const was not set in version.go\n"+
				"  where: bestiary.UpstreamGitCommit (version.go)\n"+
				"  how to fix: set UpstreamGitCommit to the short hex commit hash (e.g. \"6a41e313\")",
		)
		return
	}
	re := regexp.MustCompile(`^[0-9a-f]+$`)
	if !re.MatchString(v) {
		t.Errorf(
			"UpstreamGitCommit %q contains non-hex characters;\n"+
				"  what went wrong: value does not satisfy regexp %q\n"+
				"  why: const contains non-lowercase-hex characters in version.go\n"+
				"  where: bestiary.UpstreamGitCommit (version.go)\n"+
				"  how to fix: use only lowercase hex characters (0-9, a-f)",
			v, re.String(),
		)
	}
}

// TestUpstreamGitRemote_NonEmpty verifies that UpstreamGitRemote is a non-empty
// string starting with "git@" or "https://".
func TestUpstreamGitRemote_NonEmpty(t *testing.T) {
	v := bestiary.UpstreamGitRemote
	if v == "" {
		t.Errorf(
			"UpstreamGitRemote is empty;\n"+
				"  what went wrong: const is an empty string\n"+
				"  why: const was not set in version.go\n"+
				"  where: bestiary.UpstreamGitRemote (version.go)\n"+
				"  how to fix: set UpstreamGitRemote to the git remote URL (e.g. \"git@github.com:org/repo.git\")",
		)
		return
	}
	if !strings.HasPrefix(v, "git@") && !strings.HasPrefix(v, "https://") {
		t.Errorf(
			"UpstreamGitRemote %q does not start with \"git@\" or \"https://\";\n"+
				"  what went wrong: remote URL has unexpected scheme or format\n"+
				"  why: const was set to a non-standard remote URL in version.go\n"+
				"  where: bestiary.UpstreamGitRemote (version.go)\n"+
				"  how to fix: use a URL starting with \"git@\" (SSH) or \"https://\" (HTTPS)",
			v,
		)
	}
}
