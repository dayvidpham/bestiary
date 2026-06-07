package bestiary_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// typeContains reports whether a JSON-Schema "type" value (which may be a bare string
// or an array of strings) includes want.
func typeContains(typeField any, want string) bool {
	switch t := typeField.(type) {
	case string:
		return t == want
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// TestSchemaFile_VersionAndModifierType guards against schema-metadata drift:
// the schema FILE's own metadata was unguarded, so it
// silently drifted from the Go const (version stuck at 0.0.2) and could silently revert
// the ModelRef.Modifier type to "string". This test cross-checks the schema FILE against
// the Go contract so neither class can recur unnoticed.
func TestSchemaFile_VersionAndModifierType(t *testing.T) {
	raw, err := os.ReadFile("bestiary.schema.json")
	if err != nil {
		t.Fatalf("read bestiary.schema.json: %v", err)
	}
	var schema struct {
		Version    string `json:"version"`
		Properties map[string]struct {
			Type any `json:"type"`
		} `json:"properties"`
		Defs map[string]struct {
			Properties map[string]struct {
				Type any `json:"type"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal bestiary.schema.json: %v", err)
	}

	// (a) schema FILE version must equal the Go const.
	if schema.Version != bestiary.BestiarySchemaVersion {
		t.Errorf("bestiary.schema.json \"version\" = %q, want %q (== BestiarySchemaVersion);\n"+
			"  the schema-file metadata drifted from the Go const — bump schema line 6 in lockstep",
			schema.Version, bestiary.BestiarySchemaVersion)
	}

	// (b) ModelInfo top-level Modifier.type must declare "array".
	if mi, ok := schema.Properties["Modifier"]; !ok {
		t.Error("schema properties.Modifier missing")
	} else if !typeContains(mi.Type, "array") {
		t.Errorf("schema properties.Modifier.type = %v, must contain \"array\" (Modifier is []string)", mi.Type)
	}

	// (b) $defs.ModelRef.Modifier.type must ALSO declare "array" (the cycle-1 gap: the
	// Go output check alone did not catch a schema-side revert to "string").
	ref, ok := schema.Defs["ModelRef"]
	if !ok {
		t.Fatal("schema $defs.ModelRef missing")
	}
	if rm, ok := ref.Properties["Modifier"]; !ok {
		t.Error("schema $defs.ModelRef.Modifier missing")
	} else if !typeContains(rm.Type, "array") {
		t.Errorf("schema $defs.ModelRef.Modifier.type = %v, must contain \"array\" (ModelRef.Modifier is []string)", rm.Type)
	}
}

// TestBestiarySchemaVersion_Exact asserts that BestiarySchemaVersion equals
// exactly "0.0.3" — bumped by for the Modifier string→[]string
// public schema change. Update this test when a new schema version is released.
func TestBestiarySchemaVersion_Exact(t *testing.T) {
	const want = "0.0.3"
	if bestiary.BestiarySchemaVersion != want {
		t.Errorf(
			"BestiarySchemaVersion = %q, want %q;\n"+
				"  what went wrong: schema version constant does not match expected value\n"+
				"  why: version.go was not updated for this schema epoch, or was bumped too far\n"+
				"  where: bestiary.BestiarySchemaVersion (version.go)\n"+
				"  how to fix: set BestiarySchemaVersion = %q in version.go",
			bestiary.BestiarySchemaVersion, want, want,
		)
	}
}

// TestBestiarySchemaVersion_Semver verifies that BestiarySchemaVersion matches
// the semver major.minor.patch format (e.g. "0.0.2").
func TestBestiarySchemaVersion_Semver(t *testing.T) {
	re := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !re.MatchString(bestiary.BestiarySchemaVersion) {
		t.Errorf(
			"BestiarySchemaVersion %q does not match semver major.minor.patch format;\n"+
				"  what went wrong: value does not satisfy regexp %q\n"+
				"  why: const was set to a non-semver string in version.go\n"+
				"  where: bestiary.BestiarySchemaVersion (version.go)\n"+
				"  how to fix: set BestiarySchemaVersion to a string like \"0.0.2\"",
			bestiary.BestiarySchemaVersion, re.String(),
		)
	}
}

// TestUpstreamSchemaVersion_Format verifies that UpstreamSchemaVersion matches
// the YYYY.MM.DD-sha256 format (e.g. "2026.04.04-<64 hex chars>").
func TestUpstreamSchemaVersion_Format(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}-[0-9a-f]{64}$`)
	if !re.MatchString(bestiary.UpstreamSchemaVersion) {
		t.Errorf(
			"UpstreamSchemaVersion %q does not match YYYY.MM.DD-sha256 format;\n"+
				"  what went wrong: value does not satisfy regexp %q\n"+
				"  why: const was set to a non-conforming string in version.go\n"+
				"  where: bestiary.UpstreamSchemaVersion (version.go)\n"+
				"  how to fix: set UpstreamSchemaVersion to a string like "+
				"\"2026.04.04-<full 64-char lowercase hex SHA-256>\"",
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
			"UpstreamGitCommit is empty;\n" +
				"  what went wrong: const is an empty string\n" +
				"  why: const was not set in version.go\n" +
				"  where: bestiary.UpstreamGitCommit (version.go)\n" +
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
			"UpstreamGitRemote is empty;\n" +
				"  what went wrong: const is an empty string\n" +
				"  why: const was not set in version.go\n" +
				"  where: bestiary.UpstreamGitRemote (version.go)\n" +
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
