package bestiary_test

import (
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGofmtClean is the hygiene GATE: every committed .go file in
// the module MUST already be gofmt-clean, so a formatting regression can never recur
// silently. It uses go/format (the same engine as the gofmt binary) rather than shelling
// out, so it runs hermetically under CGO_ENABLED=0 go test ./... with no external tool.
//
// To fix a failure: run `gofmt -w .` from the module root and commit.
func TestGofmtClean(t *testing.T) {
	root := ".." // this test lives in the module root package; walk from there.
	// Resolve the module root robustly relative to this test's working dir (the package dir).
	if _, err := os.Stat(filepath.Join(".", "go.mod")); err == nil {
		root = "."
	}

	var dirty []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip non-source dirs that may contain generated/vendored or fixture files.
			switch d.Name() {
			case ".git", ".direnv", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		formatted, ferr := format.Source(src)
		if ferr != nil {
			t.Errorf("gofmt: %s does not parse: %v", path, ferr)
			return nil
		}
		if string(formatted) != string(src) {
			dirty = append(dirty, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk module tree: %v", err)
	}
	if len(dirty) > 0 {
		t.Errorf("the following %d file(s) are NOT gofmt-clean — run `gofmt -w .` and commit:\n  %s",
			len(dirty), strings.Join(dirty, "\n  "))
	}
}
