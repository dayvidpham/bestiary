package bestiary

import (
	"strings"
	"testing"
)

// TestParseNomenClaims_Valid exercises the happy path with defaults: a claim with no
// scheme/status/source_id defaults to (alias, admitted, models.dev), and the
// resolves_to tuple decomposes through the identity-class projection.
func TestParseNomenClaims_Valid(t *testing.T) {
	raw := []byte(`{
      "schema_version": 1,
      "claims": [
        {"value": "grok-beta", "resolves_to": {"family": "grok", "version": "4.20", "modifier": ["reasoning"]}, "source_url": "https://docs.x.ai/docs/models"}
      ]
    }`)
	tbl, err := parseNomenClaims(raw)
	if err != nil {
		t.Fatalf("parseNomenClaims: %v", err)
	}
	if len(tbl.claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(tbl.claims))
	}
	c := tbl.claims[0]
	if c.Scheme != NomenSchemeAlias {
		t.Errorf("default scheme = %v, want alias", c.Scheme)
	}
	if c.Status != AcceptabilityAdmitted {
		t.Errorf("default status = %v, want admitted", c.Status)
	}
	if c.Source != DataSourceCurated {
		t.Errorf("default source = %q, want curated", c.Source)
	}
	if c.ResolvesTo.String() != "grok@4.20{reasoning}" {
		t.Errorf("resolves_to = %q, want grok@4.20{reasoning}", c.ResolvesTo.String())
	}
}

// TestParseNomenClaims_Rejects verifies the LOUD codegen-side validation: an empty
// value, a missing claimant (source_url), or an unknown family are actionable errors.
func TestParseNomenClaims_Rejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty value", `{"claims":[{"value":"","resolves_to":{"family":"grok"},"source_url":"https://x"}]}`, "empty value"},
		{"no claimant", `{"claims":[{"value":"grok-beta","resolves_to":{"family":"grok"}}]}`, "source_url"},
		{"unknown family", `{"claims":[{"value":"x","resolves_to":{"family":"not-a-family"},"source_url":"https://x"}]}`, "unknown base family"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseNomenClaims([]byte(tc.raw)); err == nil {
				t.Fatal("parseNomenClaims accepted an invalid claim; want a loud error")
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestLoadNomenClaimsSafe_NeverNil is the runtime graceful-degrade guard: the safe
// accessor returns a non-nil table even if loading fails (the lineage.go precedent).
func TestLoadNomenClaimsSafe_NeverNil(t *testing.T) {
	if loadNomenClaimsSafe() == nil {
		t.Fatal("loadNomenClaimsSafe returned nil; must degrade to a non-nil empty table")
	}
	// The embedded seed must load and contain the grok-beta claim.
	tbl, err := loadNomenClaims()
	if err != nil {
		t.Fatalf("loadNomenClaims (embedded seed): %v", err)
	}
	found := false
	for _, c := range tbl.claims {
		if c.Value == "grok-beta" {
			found = true
		}
	}
	if !found {
		t.Error("embedded nomen_claims.json seed missing the grok-beta claim")
	}
}

// TestParseEntityKey_RoundTrip verifies the store-side key parser is the exact inverse
// of EntityRef.String() across the segment shapes (family, variant, version, size,
// mods).
func TestParseEntityKey_RoundTrip(t *testing.T) {
	keys := []string{
		"grok",
		"grok@4.20",
		"grok@4.20{reasoning}",
		"claude/sonnet@4.5",
		"llama/scout@4#17b-16e{instruct}",
		"llama@4#17b-128e{instruct}",
		"deepseek@3.2",
	}
	for _, k := range keys {
		if got := parseEntityKey(k).String(); got != k {
			t.Errorf("parseEntityKey(%q).String() = %q, want round-trip", k, got)
		}
	}
}
