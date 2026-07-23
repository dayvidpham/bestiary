package bestiary_test

// The curated-claims ARCHIVE POLICY fence.
//
// Policy: every SourceURL carried by a curated naming claim
// (parse/data/nomen_claims.json) is an archive.org snapshot of the claimant page,
// captured when the claim was created — never the live page. A claim is evidence
// of what a lab published; model cards and docs pages are edited and deleted
// without notice, so a live URL silently stops attesting the claim it was cited
// for. There is deliberately no second archive_url field: the snapshot URL embeds
// the original claimant URL verbatim in its tail.
//
// The per-claim expectations live with their own fences (the huggingface seeds in
// nomen_huggingface_test.go, grok-beta in nomen_test.go). What lives HERE is the
// policy itself, asserted over the whole claim set, so a claim added later with a
// live URL reddens without anyone remembering to add a case.

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// archiveSnapshotPrefix is the archive.org snapshot scheme+host prefix every curated
// claim's SourceURL must carry. Shared with the per-claim fences.
const archiveSnapshotPrefix = "https://web.archive.org/web/"

// archiveSnapshotRe is the full shape: the snapshot prefix, a 14-digit capture
// timestamp, then the original claimant URL retained verbatim.
var archiveSnapshotRe = regexp.MustCompile(`^https://web\.archive\.org/web/\d{14}/(https?://.+)$`)

// TestNomenClaims_SourceURLsAreArchiveSnapshots reads the curator-facing artifact
// itself — the committed claim file — and holds every claim to the policy. This is
// the file a curator edits, so it is the file the policy is enforced on.
func TestNomenClaims_SourceURLsAreArchiveSnapshots(t *testing.T) {
	raw, err := os.ReadFile("parse/data/nomen_claims.json")
	if err != nil {
		t.Fatalf("read curated claim file: %v", err)
	}
	var file struct {
		Comment string `json:"_comment"`
		Claims  []struct {
			Value     string `json:"value"`
			SourceURL string `json:"source_url"`
		} `json:"claims"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse curated claim file: %v", err)
	}
	if len(file.Claims) == 0 {
		t.Fatal("curated claim file has no claims; the policy fence would pass vacuously")
	}

	// The policy must be discoverable where a curator meets it.
	if !strings.Contains(file.Comment, "web.archive.org") {
		t.Errorf("the claim file's _comment no longer documents the archive policy; a curator adding a claim "+
			"would not know to snapshot the page first. _comment: %q", file.Comment)
	}

	for _, c := range file.Claims {
		m := archiveSnapshotRe.FindStringSubmatch(c.SourceURL)
		if m == nil {
			t.Errorf("claim %q has source_url %q, want an archive.org snapshot of the claimant page "+
				"(%s<14-digit timestamp>/<original-url>).\n"+
				"  Why: a curated claim is evidence of what a lab published, and live model cards and docs "+
				"pages are edited and deleted without notice, so a live URL stops attesting the claim.\n"+
				"  How to fix: capture the claimant page at web.archive.org, verify the snapshot loads, "+
				"then use that URL in parse/data/nomen_claims.json.",
				c.Value, c.SourceURL, archiveSnapshotPrefix)
			continue
		}
		// The embedded original must be a real absolute URL — this is what makes a
		// separate archive_url field unnecessary.
		if orig := m[1]; !strings.HasPrefix(orig, "http") || len(orig) < len("http://a.b") {
			t.Errorf("claim %q snapshot %q does not embed a recoverable original URL (got %q)", c.Value, c.SourceURL, orig)
		}
	}
}

// TestNomina_ClaimAttributionIsArchived is the production-path twin: it walks the
// nomina the library actually mints and hands to callers, and holds every claim
// attribution to the same policy. The file test catches a bad edit to the curation;
// this catches a claim that reaches a caller un-snapshotted by any route.
func TestNomina_ClaimAttributionIsArchived(t *testing.T) {
	attributed := 0
	for _, n := range bestiary.Nomina() {
		if n.SourceURL == "" {
			// Bestiary-minted nomina (canonical keys, provider-ID spellings) assert
			// themselves and carry no claimant.
			continue
		}
		attributed++
		if !archiveSnapshotRe.MatchString(n.SourceURL) {
			t.Errorf("nomen %q (scheme %v) carries claimant SourceURL %q, want an archive.org snapshot; "+
				"every curated claim must cite durable evidence", n.Value, n.Scheme, n.SourceURL)
		}
	}
	if attributed == 0 {
		t.Fatal("no minted nomen carries a claimant SourceURL; the curated claims did not load and this fence " +
			"would pass vacuously")
	}
	t.Logf("archive policy verified on %d claim-attributed nomina", attributed)
}
