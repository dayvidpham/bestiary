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

// TestEntityProviderOrder_CreatorThenCanonicalThenAlphabetical pins the three-group
// display order of the entity view's provider line: the creator's own hosted surfaces
// in CURATION order, then the family's canonical provider, then everything else
// alphabetically. The synthetic entity below is deliberately built with an already
// alphabetical Providers slice so a failure means the reorder did not happen, not that
// the input happened to be sorted.
//
// zhipu is used because its curated surface list is longer than one and is
// deliberately NOT alphabetical (zhipuai before zai), so this also pins that the
// curation order survives — a sort applied "for tidiness" anywhere in the chain turns
// this red.
func TestEntityProviderOrder_CreatorThenCanonicalThenAlphabetical(t *testing.T) {
	creatorSurfaces := bestiary.Creator("zhipu").Providers()
	if len(creatorSurfaces) < 2 {
		t.Skipf("the zhipu creator row no longer carries two or more surfaces (%v); "+
			"re-pin this test on a creator that does before assuming the order is unpinned",
			creatorSurfaces)
	}

	e := bestiary.Entity{
		Ref:     bestiary.EntityRef{Family: bestiary.Family("claude"), Version: "4.6"},
		Creator: bestiary.Creator("zhipu"),
		Providers: []bestiary.Provider{
			"anthropic", "openrouter", "zai", "zhipuai",
		},
	}

	ordered, preferred := entityProviderOrder(e)

	// Group 1 is curation order, not alphabetical: zhipuai leads zai.
	// Group 2 is the family's canonical provider (claude → anthropic).
	want := []bestiary.Provider{"zhipuai", "zai", "anthropic", "openrouter"}
	if len(ordered) != len(want) {
		t.Fatalf("entityProviderOrder returned %d providers, want %d — the result must be a "+
			"PERMUTATION of Entity.Providers, never a set that drops or invents one.\ngot:  %v\nwant: %v",
			len(ordered), len(want), ordered, want)
	}
	for i := range want {
		if ordered[i] != want[i] {
			t.Fatalf("entityProviderOrder position %d = %q, want %q. Expected order is "+
				"creator surfaces in curation order, then the family's canonical provider, "+
				"then the rest alphabetically.\ngot:  %v\nwant: %v",
				i, ordered[i], want[i], ordered, want)
		}
	}
	if preferred != 3 {
		t.Errorf("preferred group length = %d, want 3 (two creator surfaces + the canonical "+
			"provider). This is where the %q separator is drawn.", preferred, providerGroupSeparator)
	}
}

// TestEntityProviderOrder_NeverInventsAProvider pins that a creator surface or
// canonical provider that does NOT serve this entity is not hoisted into the line.
// Without the membership guard the printed count (which stays len(e.Providers)) would
// disagree with the printed list.
func TestEntityProviderOrder_NeverInventsAProvider(t *testing.T) {
	e := bestiary.Entity{
		Ref:       bestiary.EntityRef{Family: bestiary.Family("claude"), Version: "4.6"},
		Creator:   bestiary.Creator("zhipu"), // hosts zhipuai/zai; neither serves this entity
		Providers: []bestiary.Provider{"openrouter", "bedrock"},
	}
	ordered, preferred := entityProviderOrder(e)
	if preferred != 0 {
		t.Errorf("preferred group length = %d, want 0 — neither the creator surfaces nor the "+
			"canonical provider serve this entity, so nothing may be hoisted", preferred)
	}
	want := []bestiary.Provider{"bedrock", "openrouter"}
	for i := range want {
		if ordered[i] != want[i] {
			t.Fatalf("entityProviderOrder = %v, want %v (alphabetical, no hoist)", ordered, want)
		}
	}
}

// TestJoinProviderGroups_SeparatorOnlyBetweenGroups pins that the visual separator is
// drawn only when there is something on BOTH sides of it, so an entity with no
// preferred surfaces (or with nothing but preferred surfaces) renders a plain list.
func TestJoinProviderGroups_SeparatorOnlyBetweenGroups(t *testing.T) {
	names := []string{"a", "b", "c"}
	for _, tc := range []struct {
		name      string
		preferred int
		want      string
	}{
		{"no_preferred_group", 0, "a, b, c"},
		{"all_preferred", 3, "a, b, c"},
		{"split", 1, "a | b, c"},
		{"split_two", 2, "a, b | c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinProviderGroups(names, tc.preferred); got != tc.want {
				t.Errorf("joinProviderGroups(%v, %d) = %q, want %q", names, tc.preferred, got, tc.want)
			}
		})
	}
}

// TestOrderInstancesByProvider_FollowsProviderLineAndIsStable pins that the instance
// table follows the SAME order as the provider line — the reason the reorder exists is
// that the table truncates at instanceTableLimit, so an unordered table can cut the
// lab's own offering while showing twenty rehosts — and that rows sharing a provider
// keep their incoming (id-sorted) order.
func TestOrderInstancesByProvider_FollowsProviderLineAndIsStable(t *testing.T) {
	insts := []bestiary.ProviderInstance{
		{ID: "m-a", Provider: "openrouter"},
		{ID: "m-b", Provider: "openrouter"},
		{ID: "m-c", Provider: "zhipuai"},
		{ID: "m-d", Provider: "anthropic"},
	}
	order := []bestiary.Provider{"zhipuai", "anthropic", "openrouter"}

	got := orderInstancesByProvider(insts, order)

	wantIDs := []bestiary.ModelID{"m-c", "m-d", "m-a", "m-b"}
	for i, want := range wantIDs {
		if got[i].ID != want {
			t.Fatalf("instance row %d = %q (%s), want %q. Rows must follow the provider line's "+
				"order, with ties keeping the incoming order.", i, got[i].ID, got[i].Provider, want)
		}
	}
	// The caller's slice must be untouched: the JSON output reads the entity's own
	// Instances projection and must not inherit a display-only reorder.
	if insts[0].ID != "m-a" {
		t.Errorf("orderInstancesByProvider mutated the caller's slice (insts[0] = %q); it must "+
			"sort a COPY so the JSON projection keeps the registry order", insts[0].ID)
	}
}

// TestEntityView_ProviderLineAndInstanceTableAgree walks the PRODUCTION formatter end
// to end on a real registry entity and asserts the two renderings agree: the first
// provider named on the "Providers (N):" line is also the provider of the first
// instance row. This is the property the fix is actually about, and it is checked
// against live data rather than a synthetic entity so a curation change that empties a
// creator row surfaces here.
func TestEntityView_ProviderLineAndInstanceTableAgree(t *testing.T) {
	ent, ok := bestiary.EntityByKey("glm@5")
	if !ok {
		t.Skip("glm@5 is not in this keyspace; re-pin this walk on a live multi-provider entity")
	}
	if len(ent.Providers) < 2 || len(ent.Instances) < 2 {
		t.Skipf("glm@5 no longer has multiple providers/instances (%d/%d)",
			len(ent.Providers), len(ent.Instances))
	}

	var buf strings.Builder
	writeEntityView(&buf, ent)
	out := buf.String()

	ordered, preferred := entityProviderOrder(ent)
	if preferred == 0 {
		t.Fatalf("glm@5 rendered no preferred provider group; its creator is %q with surfaces %v, "+
			"and at least one of those should serve it", ent.Creator, ent.Creator.Providers())
	}

	// The line carries the separator exactly once, with the preferred group ahead of it.
	line := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Providers (") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("entity view printed no \"Providers (N):\" line")
	}
	head, _, found := strings.Cut(line, providerGroupSeparator)
	if !found {
		t.Fatalf("provider line carries no %q separator between the preferred group and the "+
			"rest:\n%s", providerGroupSeparator, line)
	}
	if !strings.HasSuffix(head, string(ordered[preferred-1])) {
		t.Errorf("the provider named immediately before the separator is not the last preferred "+
			"provider (%q):\n%s", ordered[preferred-1], line)
	}

	// The first instance row belongs to the first provider on the line.
	first := string(ordered[0])
	if !strings.Contains(line, "): "+first) {
		t.Errorf("provider line does not lead with %q:\n%s", first, line)
	}
	rows := strings.Split(out, "\n")
	idx := -1
	for i, l := range rows {
		if strings.HasPrefix(l, "Instances (") {
			idx = i + 2 // skip the header row
			break
		}
	}
	if idx < 0 || idx >= len(rows) {
		t.Fatal("entity view printed no instance table")
	}
	if !strings.Contains(rows[idx], first) {
		t.Errorf("first instance row does not belong to %q, the first provider on the "+
			"provider line — the table and the line must use ONE order:\n%s", first, rows[idx])
	}
}
