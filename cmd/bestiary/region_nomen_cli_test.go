package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// entityOut is a partial view of the `show --by-entity --output=json` document: the
// fields this slice adds (Regions aggregate + the Nomina projection). Extra fields in
// the output are ignored by the decoder.
type entityOut struct {
	Ref     bestiary.EntityRef
	Regions []bestiary.Region
	Nomina  []bestiary.Nomen
}

// TestRun_ShowByEntity_GrokBetaNomen is the grok-beta end-to-end demonstration: the
// CLI `show --by-entity --output=json` for grok@4.20{reasoning} surfaces the curated
// xAI alias claim as a claim-attributed Nomen (SourceURL = the xAI page) alongside the
// canonical Preferred nomen.
func TestRun_ShowByEntity_GrokBetaNomen(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--output=json", "grok@4.20{reasoning}"})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity grok@4.20{reasoning}: %v", runErr)
	}
	var got entityOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("entity json did not parse: %v\noutput:\n%s", err, out)
	}
	var sawCanonicalPreferred, sawGrokBeta bool
	for _, n := range got.Nomina {
		if n.Scheme == bestiary.NomenSchemeCanonical && n.Status == bestiary.AcceptabilityPreferred {
			sawCanonicalPreferred = true
		}
		if n.Value == "grok-beta" {
			sawGrokBeta = true
			if n.Scheme != bestiary.NomenSchemeAlias {
				t.Errorf("grok-beta scheme = %v, want alias", n.Scheme)
			}
			if !strings.Contains(n.SourceURL, "x.ai") {
				t.Errorf("grok-beta SourceURL = %q, want the xAI claimant page", n.SourceURL)
			}
			if n.Source != bestiary.DataSourceCurated {
				t.Errorf("grok-beta Source = %q, want curated (the honest ingest, distinct from the claimant)", n.Source)
			}
		}
	}
	if !sawCanonicalPreferred {
		t.Error("show --by-entity JSON missing the canonical Preferred nomen")
	}
	if !sawGrokBeta {
		t.Errorf("show --by-entity JSON missing the grok-beta alias claim;\noutput:\n%s", out)
	}
	// Also assert the raw output text carries the claimant URL (a human sees it).
	if !strings.Contains(out, "docs.x.ai") {
		t.Error("CLI output does not surface the xAI claim URL")
	}
}

// TestRun_ShowByEntity_RegionsGolden pins the human-readable and JSON region surfacing
// for a multi-region entity, including the "unspecified" token for the entity's plain
// (no-prefix) instances.
func TestRun_ShowByEntity_RegionsGolden(t *testing.T) {
	// JSON: the Regions aggregate is present and includes unspecified + us.
	var runErr error
	jsonOut := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--output=json", "claude/sonnet@4.5"})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity claude/sonnet@4.5 (json): %v", runErr)
	}
	var got entityOut
	if err := json.Unmarshal([]byte(jsonOut), &got); err != nil {
		t.Fatalf("entity json did not parse: %v", err)
	}
	tokens := map[string]bool{}
	for _, r := range got.Regions {
		tokens[r.String()] = true
	}
	for _, want := range []string{"unspecified", "us", "eu", "global", "au", "jp"} {
		if !tokens[want] {
			t.Errorf("JSON Regions missing %q; got %v", want, got.Regions)
		}
	}

	// Table: the human view renders a Regions line including the "unspecified" token.
	tableOut := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--output=table", "claude/sonnet@4.5"})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity claude/sonnet@4.5 (table): %v", runErr)
	}
	if !strings.Contains(tableOut, "Regions (6): unspecified, us, eu, global, au, jp") {
		t.Errorf("table view missing the expected Regions line;\noutput:\n%s", tableOut)
	}
}
