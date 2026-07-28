package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestInstanceTable_TruncatesLongProviderAndCaps pins the two instance-table
// readability fixes: (5) a provider slug longer than its column is truncated with an
// ellipsis so it can no longer overrun and knock the numeric columns out of
// alignment, and (6) the row set is capped with a "… and N more (use --output json)"
// footer, mirroring the nomina/benchmark convention.
func TestInstanceTable_TruncatesLongProviderAndCaps(t *testing.T) {
	// One row with an over-wide provider slug, plus enough rows to trip the cap.
	total := instanceTableLimit + 5
	insts := make([]bestiary.ProviderInstance, total)
	insts[0] = bestiary.ProviderInstance{ID: "m0", Provider: "azure-cognitive-services-really-long"}
	for i := 1; i < total; i++ {
		insts[i] = bestiary.ProviderInstance{ID: bestiary.ModelID(fmt.Sprintf("m%d", i)), Provider: bestiary.Provider(fmt.Sprintf("p%d", i))}
	}
	statuses := make([]bestiary.ModelStatus, total)
	stages := make([]bestiary.ReleaseStage, total)

	var buf strings.Builder
	writeInstanceTableWithStatus(&buf, insts, statuses, stages)
	out := buf.String()

	// (5) The over-wide provider slug must be truncated (ellipsis), never rendered in full.
	if strings.Contains(out, "azure-cognitive-services-really-long") {
		t.Errorf("over-wide provider slug must be truncated to its column width;\nGot:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("truncated provider cell must carry an ellipsis;\nGot:\n%s", out)
	}

	// (6) The header reports the true total, the body caps at instanceTableLimit, and
	// a "… and N more (use --output json)" footer names the omitted count.
	if !strings.Contains(out, fmt.Sprintf("Instances (%d):", total)) {
		t.Errorf("header must report the true instance total %d;\nGot:\n%s", total, out)
	}
	wantFooter := fmt.Sprintf("… and %d more (use --output json)", total-instanceTableLimit)
	if !strings.Contains(out, wantFooter) {
		t.Errorf("capped instance table missing footer %q;\nGot:\n%s", wantFooter, out)
	}
	// The (instanceTableLimit+1)-th instance's ID must NOT be rendered.
	if strings.Contains(out, fmt.Sprintf("m%d ", instanceTableLimit)) {
		t.Errorf("instance beyond the cap (index %d) must not be rendered;\nGot:\n%s", instanceTableLimit, out)
	}
}

// extractShowGuidanceExample pulls the concrete command the ambiguity guidance
// prints on its "show one directly:" line — everything AFTER the literal
// "bestiary show " token — so a test can run the tool's OWN suggestion back
// through the CLI verbatim.
func extractShowGuidanceExample(t *testing.T, msg string) string {
	t.Helper()
	const marker = "bestiary show "
	for _, line := range strings.Split(msg, "\n") {
		if !strings.Contains(line, "show one directly:") {
			continue
		}
		i := strings.Index(line, marker)
		if i < 0 {
			t.Fatalf("guidance line missing %q token: %q", marker, line)
		}
		return strings.TrimSpace(line[i+len(marker):])
	}
	t.Fatalf("no 'show one directly:' guidance line found in ambiguity error:\n%s", msg)
	return ""
}

// TestShow_AmbiguityGuidance_ExampleResolves is the round-trip pin for the F1
// derived-example fix: the "show one directly:" command the ambiguity error prints
// must actually resolve when pasted back — the exact end-to-end explicability the
// user's Impl-UAT complaint was about. It is table-driven over four families that
// each ambiguate for a DIFFERENT structural reason (llama: no canonical provider,
// Variant-empty + #size; claude: canonical provider + variant + version; gpt:
// high-fanout entity whose key is itself model-ambiguous; gemini: variant +
// version, no size), so a derivation that only works for one shape reddens here.
//
// The example is derived as the candidate's ENTITY KEY shown via `show --by-entity`
// (see runShow's ErrAmbiguous branch): the entity view renders an entity key
// directly, without the model-first resolution that produced the ambiguity, so a
// candidate's own key resolves BY CONSTRUCTION for every family class — including
// gpt/4o, whose key names 20 date-differentiated model rows and would re-ambiguate
// under a plain `show <key>`.
func TestShow_AmbiguityGuidance_ExampleResolves(t *testing.T) {
	for _, family := range []string{"llama", "claude", "gpt", "gemini"} {
		t.Run(family, func(t *testing.T) {
			tmpDB := t.TempDir() + "/test.db"

			var runErr error
			captureStdout(t, func() {
				captureStderr(t, func() {
					runErr = run([]string{"show", "--db-path", tmpDB, family})
				})
			})
			if runErr == nil {
				t.Fatalf("show %q: expected an ambiguity error to derive a suggestion from; got nil", family)
			}
			if !strings.Contains(runErr.Error(), "under-specified") {
				t.Fatalf("show %q: expected the under-specified ambiguity error; got: %v", family, runErr)
			}

			example := extractShowGuidanceExample(t, runErr.Error())
			// Run the tool's OWN suggestion back through the CLI, verbatim.
			args := append([]string{"show", "--db-path", tmpDB}, strings.Fields(example)...)
			var back error
			out := captureStdout(t, func() {
				captureStderr(t, func() {
					back = run(args)
				})
			})
			if back != nil {
				t.Fatalf("derived example %q did NOT resolve when pasted back: %v", example, back)
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("derived example %q resolved (exit 0) but rendered an EMPTY view", example)
			}
		})
	}
}

// TestShow_AmbiguousInput_KeepsAmbiguityPath_NotEntityFallback pins the F2
// precedence rule the reviewer's mutation probe found unpinned: an input that is
// BOTH model-ambiguous AND a resolvable entity key must take the ambiguity path,
// never the entity fallback. "gpt/4o" is exactly that dual case — it names ~20
// date-differentiated gpt-4o model rows (ambiguous) yet is a valid entity key
// (`show --by-entity gpt/4o` renders). Model resolution stays FIRST, so the
// ambiguity error is returned and the entity fallback is NEVER reached; wiring the
// fallback into the ErrAmbiguous branch would render a single aggregate entity view
// on stdout and swallow the ambiguity, which this test forbids.
func TestShow_AmbiguousInput_KeepsAmbiguityPath_NotEntityFallback(t *testing.T) {
	tmpDB := t.TempDir() + "/test.db"

	var runErr error
	out := captureStdout(t, func() {
		captureStderr(t, func() {
			runErr = run([]string{"show", "--db-path", tmpDB, "gpt/4o"})
		})
	})

	if runErr == nil {
		t.Fatal("show gpt/4o: expected an ambiguity error; got nil (the entity fallback shadowed the ambiguity)")
	}
	if !strings.Contains(runErr.Error(), "under-specified") {
		t.Fatalf("show gpt/4o: expected the under-specified ambiguity error; got: %v", runErr)
	}
	// The entity view (if wrongly reached) prints "Entity:" to stdout; the ambiguity
	// path keeps stdout empty (candidates go to stderr, the error is returned).
	if strings.Contains(out, "Entity:") {
		t.Errorf("show gpt/4o rendered an entity view on stdout; the ambiguity path must win:\n%s", out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("show gpt/4o: stdout must be empty on the ambiguity path; got:\n%s", out)
	}
}

// TestShow_EntityView_DefaultsToTable_NoOutputFlag pins F5's human-readable default
// on BOTH entity-view code paths, with NO --output flag supplied — the exact gap the
// reviewer's mutation probe found (every existing test passed an explicit --output).
// A human running either path with no flags must get a readable table, not a JSON
// blob (the user's literal complaint). Flipping either default back to json reddens
// this: the output would open with JSON's '{' and omit the table's "Entity:" header.
func TestShow_EntityView_DefaultsToTable_NoOutputFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		// Path 1: explicit `show --by-entity <key>`.
		{"by-entity", []string{"show", "--by-entity", "llama@3.3#70b"}},
		// Path 2: the plain-show F2 entity fallback (`show <entity-key>`, no --by-entity).
		{"plain-fallback", []string{"show", "llama@3.3#70b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDB := t.TempDir() + "/test.db"
			args := make([]string, 0, len(tc.args)+2)
			args = append(args, tc.args...)
			args = append(args, "--db-path", tmpDB)

			var runErr error
			out := captureStdout(t, func() {
				captureStderr(t, func() {
					runErr = run(args)
				})
			})
			if runErr != nil {
				t.Fatalf("%v: %v", args, runErr)
			}
			trimmed := strings.TrimSpace(out)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				t.Errorf("%v: default output looks like JSON, want a table:\n%s", args, out)
			}
			if !strings.Contains(out, "Entity: llama@3.3#70b") {
				t.Errorf("%v: default output missing the table 'Entity:' header:\n%s", args, out)
			}
		})
	}
}
