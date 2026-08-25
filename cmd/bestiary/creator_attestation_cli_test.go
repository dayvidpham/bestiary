package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

const (
	// unmappedFamily / unmappedEntityKey name a family that is deliberately NOT in
	// parse/data/creators.json, used by the unmapped-render cases below.
	unmappedFamily    = "unsloth"
	unmappedEntityKey = "unsloth#27b"
)

// creatorOut is a partial view of the `show --by-entity --output=json` document,
// projecting only the Creator field this slice surfaces. Extra fields are ignored.
type creatorOut struct {
	Creator string
}

// TestRun_Creator_Surfaced is the CLI-observable proof of creator queryability
// (Entity.Creator / ModelInfo.Creator queryable): a mapped family renders its
// curated Creator (llama→meta, claude→anthropic) while an unmapped family renders an
// honest blank ("-" in the table, "" in JSON) — never an invented "unknown" label,
// and never colliding with the Providers list (a Creator is the SPDX originator, a
// distinct axis from the SPDX suppliers). It sweeps both the aggregate entity view
// and the per-model table (which share no rendering path: writeEntityView vs
// format.go printTableModelRow).
func TestRun_Creator_Surfaced(t *testing.T) {
	// Non-vacuity: the unmapped case is only a test of the empty-render path while its
	// family genuinely carries no curated creator. Assert that here rather than
	// trusting the comment, so a later curation slice that maps this family turns the
	// case into a loud failure instead of a silently tautological one.
	if got := bestiary.Family(unmappedFamily).Creator(); got != bestiary.CreatorNone {
		t.Fatalf("family %q now maps to creator %q; the unmapped-render cases below need a "+
			"different family (one that is deliberately unattributed)", unmappedFamily, got)
	}

	// Entity-view (table) sweep: mapped families show their Creator, unmapped shows "-".
	entityCases := []struct {
		name     string
		argv     []string
		wantLine string
	}{
		{
			name:     "llama entity creator is meta",
			argv:     []string{"show", "--by-entity", "--output=table", "llama@3.3#70b{instruct}"},
			wantLine: "Creator:       meta",
		},
		{
			name:     "claude entity creator is anthropic",
			argv:     []string{"show", "--by-entity", "--output=table", "claude/sonnet@4.5"},
			wantLine: "Creator:       anthropic",
		},
		{
			// unsloth is deliberately absent from creators.json and stays that way:
			// the token is a decomposition artifact of "unsloth/<repo>" ids, and
			// Unsloth is a fine-tuning/quantization toolkit, not a model ORIGINATOR —
			// so there is no honest creator to record. An unmapped family renders the
			// honest empty, never a guessed value.
			name:     "unmapped family creator is a dash, not 'unknown'",
			argv:     []string{"show", "--by-entity", "--output=table", unmappedEntityKey},
			wantLine: "Creator:       -",
		},
	}
	for _, tc := range entityCases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() { runErr = run(tc.argv) })
			if runErr != nil {
				t.Fatalf("run %v: %v", tc.argv, runErr)
			}
			if !strings.Contains(out, tc.wantLine) {
				t.Errorf("entity view missing %q;\noutput:\n%s", tc.wantLine, out)
			}
			// The unmapped case must never print the invented "unknown" text.
			if strings.Contains(strings.ToLower(out), "creator:       unknown") {
				t.Errorf("entity view invented an 'unknown' Creator label; output:\n%s", out)
			}
		})
	}

	// Per-model table: the shared show/list table gains a Creator column.
	t.Run("per-model table shows Creator column", func(t *testing.T) {
		var runErr error
		out := captureStdout(t, func() {
			runErr = run([]string{"show", "--format", "raw", "--output", "table", "claude-opus-4-1"})
		})
		if runErr != nil {
			t.Fatalf("run show --format raw --output table claude-opus-4-1: %v", runErr)
		}
		if !strings.Contains(out, "Creator") {
			t.Errorf("per-model table missing the Creator header column;\noutput:\n%s", out)
		}
		if !strings.Contains(out, "anthropic") {
			t.Errorf("per-model table missing the claude→anthropic Creator value;\noutput:\n%s", out)
		}
	})

	// JSON entity view carries Creator directly (mapped) / empty (unmapped).
	jsonCases := []struct {
		name        string
		argv        []string
		wantCreator string
	}{
		{
			name:        "llama JSON creator is meta",
			argv:        []string{"show", "--by-entity", "--output=json", "llama@3.3#70b{instruct}"},
			wantCreator: "meta",
		},
		{
			name:        "unmapped family JSON creator is empty",
			argv:        []string{"show", "--by-entity", "--output=json", unmappedEntityKey},
			wantCreator: "",
		},
	}
	for _, tc := range jsonCases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() { runErr = run(tc.argv) })
			if runErr != nil {
				t.Fatalf("run %v: %v", tc.argv, runErr)
			}
			var got creatorOut
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatalf("entity json did not parse: %v\noutput:\n%s", err, out)
			}
			if got.Creator != tc.wantCreator {
				t.Errorf("JSON Creator = %q, want %q", got.Creator, tc.wantCreator)
			}
		})
	}
}

// TestRun_DualAttestation_BothLegsVisible is the CLI-observable proof of the
// dual-attestation visibility guarantee: a single (Value, Scheme, ResolvesTo)
// name attested by TWO distinct sources coalesces to ONE Nomen carrying BOTH
// attestations, and the CLI shows both legs.
// The real-data instance is the huggingface-scheme name meta-llama/Llama-3.3-70B-Instruct
// on the llama@3.3#70b{instruct} entity: a curated nomen_claims.json claim AND the
// harvested huggingface_nomina.json seed both assert it, so it MUST surface with a
// curated leg (Source=curated) and a huggingface leg (Source=huggingface), each with
// its Authority/Method — never a same-triple conflict and never a dropped attester.
func TestRun_DualAttestation_BothLegsVisible(t *testing.T) {
	const (
		dualName  = "meta-llama/Llama-3.3-70B-Instruct"
		entityArg = "llama@3.3#70b{instruct}"
	)

	// JSON: the coalesced Nomen carries exactly two attestations from distinct sources.
	t.Run("json shows both attestations", func(t *testing.T) {
		var runErr error
		out := captureStdout(t, func() {
			runErr = run([]string{"show", "--by-entity", "--output=json", entityArg})
		})
		if runErr != nil {
			t.Fatalf("run show --by-entity json %s: %v", entityArg, runErr)
		}
		var got entityOut // reuses region_nomen_cli_test.go's Nomina projection view
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("entity json did not parse: %v", err)
		}
		var found bool
		for _, n := range got.Nomina {
			if n.Value != dualName || n.Scheme != bestiary.NomenSchemeHuggingFace {
				continue
			}
			found = true
			if len(n.Attestations) != 2 {
				t.Fatalf("%s carries %d attestations, want 2 (curated + huggingface);\nnomen: %+v",
					dualName, len(n.Attestations), n)
			}
			sources := map[bestiary.DataSourceID]bestiary.AttestationAuthority{}
			for _, at := range n.Attestations {
				sources[at.Source] = at.Authority
			}
			if _, ok := sources[bestiary.DataSourceCurated]; !ok {
				t.Errorf("%s missing the curated attestation leg; sources=%v", dualName, sources)
			}
			if _, ok := sources[bestiary.DataSourceHuggingFace]; !ok {
				t.Errorf("%s missing the huggingface attestation leg; sources=%v", dualName, sources)
			}
		}
		if !found {
			t.Fatalf("show --by-entity JSON missing the dually-attested %s (huggingface scheme);\noutput:\n%s",
				dualName, out)
		}
	})

	// Table: the Nomina section renders the name with both attestation legs beneath it.
	t.Run("table shows both attestation legs", func(t *testing.T) {
		var runErr error
		out := captureStdout(t, func() {
			runErr = run([]string{"show", "--by-entity", "--output=table", entityArg})
		})
		if runErr != nil {
			t.Fatalf("run show --by-entity table %s: %v", entityArg, runErr)
		}
		if !strings.Contains(out, "Nomina (") {
			t.Fatalf("entity table missing the Nomina section;\noutput:\n%s", out)
		}
		// Isolate the block for the dually-attested huggingface-scheme name so the
		// curated/huggingface assertions read THAT name's legs, not another nomen's.
		// A nomen header line is 2-space indented ("  <value>  (scheme, status)");
		// its attestation sub-rows are 6-space indented. Collect ONLY the 6-space
		// rows that follow the header line, stopping at the next 2-space header
		// line — the header itself is deliberately EXCLUDED from the rows slice.
		// It must not feed the leg assertions below: the nomen's SCHEME is
		// literally "huggingface" too (writeNominaTable's "(%s, %s)" prints
		// n.Scheme.String()), so a substring check against a blockStr that still
		// included the header line would read "huggingface" off the scheme name
		// and coincidentally pass even when the huggingface attestation ROW was
		// dropped, silently missing the loss of that leg.
		header := "  " + dualName + "  (huggingface,"
		lines := strings.Split(out, "\n")
		var rows []string
		for i := 0; i < len(lines); i++ {
			if !strings.HasPrefix(lines[i], header) {
				continue
			}
			for j := i + 1; j < len(lines) && strings.HasPrefix(lines[j], "      "); j++ {
				rows = append(rows, lines[j])
			}
			break
		}
		if len(rows) == 0 {
			t.Fatalf("entity table missing the %q (huggingface) nomen block;\noutput:\n%s", dualName, out)
		}
		// rows[0] is the column header ("SOURCE  AUTHORITY  METHOD  SOURCE-URL");
		// every row after it is one attestation. Assert the DATA ROW COUNT
		// directly — a dropped leg must shrink this count, not just a substring.
		dataRows := rows[1:]
		if len(dataRows) != 2 {
			t.Fatalf("%s has %d attestation rows, want 2 (curated + huggingface);\nrows:\n%s",
				dualName, len(dataRows), strings.Join(rows, "\n"))
		}
		// Read the SOURCE token — the first whitespace-delimited field — off each
		// data row rather than substring-matching the whole block: a curated leg's
		// SOURCE-URL can itself contain "huggingface.co", which would let a
		// substring check pass even with the huggingface SOURCE row missing.
		gotSources := map[string]bool{}
		for _, row := range dataRows {
			fields := strings.Fields(row)
			if len(fields) == 0 {
				t.Fatalf("%s attestation row has no fields;\nrow: %q", dualName, row)
			}
			gotSources[fields[0]] = true
		}
		if !gotSources["curated"] {
			t.Errorf("%s missing the curated attestation ROW (SOURCE column);\nrows:\n%s",
				dualName, strings.Join(rows, "\n"))
		}
		if !gotSources["huggingface"] {
			t.Errorf("%s missing the huggingface attestation ROW (SOURCE column);\nrows:\n%s",
				dualName, strings.Join(rows, "\n"))
		}
		// The curated leg's claimant archive URL must be visible (a human sees WHO asserts).
		blockStr := strings.Join(dataRows, "\n")
		if !strings.Contains(blockStr, "web.archive.org") {
			t.Errorf("%s curated leg missing its claimant SourceURL;\nblock:\n%s", dualName, blockStr)
		}
	})
}

// TestRun_SchemeOCI_ActionableEmpty pins deliverable (3): requesting the `oci` scheme
// on a bare ref (which has no ModelRef-altitude render, "" BY DESIGN) is an EXPLAINED
// empty, not a failure. The CLI prints nothing to stdout, returns success (nil), and
// writes an actionable message to stderr that (a) explains the emptiness, (b) covers
// both the bare-ref and no-digest-yet situations, and (c) directs the user to the
// quant-level view — never a silent or confusing blank. It sweeps both flag spellings
// (--scheme oci and --format oci) since both route to SchemeOCI.
func TestRun_SchemeOCI_ActionableEmpty(t *testing.T) {
	cases := []struct {
		name string
		argv []string
	}{
		{name: "legacy --scheme oci", argv: []string{"show", "--scheme", "oci", "claude/opus@4.5"}},
		{name: "--format oci", argv: []string{"show", "--format", "oci", "claude/opus@4.5"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			var stdout string
			stderr := captureStderr(t, func() {
				stdout = captureStdout(t, func() { runErr = run(tc.argv) })
			})
			if runErr != nil {
				t.Fatalf("run %v: expected nil error (explained empty, not a failure), got %v", tc.argv, runErr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Errorf("run %v: expected empty stdout, got %q", tc.argv, stdout)
			}
			// Actionable-message content: the directive to the quant-level view and the
			// two situations must both be present.
			for _, want := range []string{"--by-entity", "OCIDigest", "digest"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("run %v: stderr missing %q;\nstderr:\n%s", tc.argv, want, stderr)
				}
			}
		})
	}
}
