package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// datastarJSSHA256 and datastarJSBytes are the pinned provenance recorded in the
// DatastarJSVersion doc comment (assets.go): sha256 + byte count of the vendored
// assets/datastar.js as fetched from the upstream release. This is the vendored-snapshot
// discipline (see "models.dev snapshot refresh" in CLAUDE.md) made executable: a
// tampered, truncated, or silently-re-fetched-to-a-different-version asset must redden
// this test rather than only living as a doc comment nobody re-checks.
const (
	datastarJSSHA256 = "2837d87acf6ee0ba8e4e63765926c25a98d63883b02f88be194a86b81d3fd24a"
	datastarJSBytes  = 34083
)

// TestVendoredDatastarJS_MatchesPinnedHash hashes the go:embed'd asset bytes (the exact
// bytes the binary ships and /assets/datastar.js serves) and compares against the pinned
// sha256 + length. It reads through assetsFS (the same embed.FS the server serves from),
// not the source file directly, so a go:embed misconfiguration would also be caught.
func TestVendoredDatastarJS_MatchesPinnedHash(t *testing.T) {
	b, err := assetsFS.ReadFile("assets/datastar.js")
	if err != nil {
		t.Fatalf("read embedded assets/datastar.js: %v", err)
	}
	if len(b) != datastarJSBytes {
		t.Errorf("assets/datastar.js is %d bytes, want pinned %d (version drift?)", len(b), datastarJSBytes)
	}
	sum := sha256.Sum256(b)
	if got := hex.EncodeToString(sum[:]); got != datastarJSSHA256 {
		t.Errorf("assets/datastar.js sha256 = %s, want pinned %s (vendored asset changed unexpectedly)", got, datastarJSSHA256)
	}
}
