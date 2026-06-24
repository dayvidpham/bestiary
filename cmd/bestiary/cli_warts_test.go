package main

import (
	"errors"
	"strings"
	"testing"
)

// TestRenderError_SinglePrefix pins WART 1: the CLI must emit EXACTLY one
// "bestiary: " prefix on every error path. Structured package errors already
// namespace themselves (ErrNotFound, the ParseQuantization error) — rendering
// them must NOT double the prefix into "bestiary: bestiary:". Inline command
// errors (usage, unsupported-output) carry no prefix — rendering them must add
// the sole one. A mutant that reverts main() to the unconditional
// "bestiary: %v" rendering re-introduces the double prefix and dies here.
func TestRenderError_SinglePrefix(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			// ErrNotFound — Error() already carries "bestiary: ".
			name: "not_found_entity",
			args: []string{"show", "--by-entity", "no-such-family/no-variant@no-version"},
		},
		{
			// ParseQuantization error — Error() already carries "bestiary: ".
			name: "unknown_quant",
			args: []string{"providers", "--quant=definitely-not-a-quant", sizedCuratedKey},
		},
		{
			// Inline unsupported-output error — carries NO prefix; main adds one.
			name: "unsupported_output",
			args: []string{"providers", "--output=tabel", sizedCuratedKey},
		},
		{
			// Inline usage error — carries NO prefix; main adds one.
			name: "unknown_command",
			args: []string{"definitely-not-a-command"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args)
			if err == nil {
				t.Fatalf("run(%v) returned nil, want an error to render", tc.args)
			}
			got := renderError(err)
			if !strings.HasPrefix(got, errPrefix) {
				t.Errorf("rendered error %q must start with %q", got, errPrefix)
			}
			if n := strings.Count(got, errPrefix); n != 1 {
				t.Errorf("rendered error %q has %d %q prefixes, want exactly 1", got, n, errPrefix)
			}
		})
	}
}

// runResult captures the observable outcome of a run() invocation: the error (as
// a string for comparison) and stdout. Two argument orderings that produce an
// identical runResult are behaviorally equivalent.
type runResult struct {
	errStr string
	stdout string
}

func invokeRun(t *testing.T, args []string) runResult {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		err = run(args)
	})
	res := runResult{stdout: out}
	if err != nil {
		res.errStr = err.Error()
	}
	return res
}

// TestFlagsPositionIndependent pins WART 2: a flag placed AFTER the positional
// must take effect identically to the same flag placed BEFORE it. Go's flag
// package stops at the first non-flag arg, so without reordering
// `show KEY --by-entity` would silently ignore --by-entity. Each case asserts
// the flags-after-positional ordering produces byte-identical stdout AND the same
// error as the flags-first ordering. A mutant that drops reorderArgs (parsing
// args verbatim) makes the post-positional flag a no-op, diverging the two
// orderings, and dies here.
func TestFlagsPositionIndependent(t *testing.T) {
	cases := []struct {
		name       string
		flagsFirst []string
		flagsAfter []string
	}{
		{
			name:       "show_by_entity_bool",
			flagsFirst: []string{"show", "--by-entity", sizedCuratedKey},
			flagsAfter: []string{"show", sizedCuratedKey, "--by-entity"},
		},
		{
			name:       "providers_quant_joined",
			flagsFirst: []string{"providers", "--quant=q4_k_m", sizedCuratedKey},
			flagsAfter: []string{"providers", sizedCuratedKey, "--quant=q4_k_m"},
		},
		{
			name:       "providers_quant_separated",
			flagsFirst: []string{"providers", "--quant", "q4_k_m", sizedCuratedKey},
			flagsAfter: []string{"providers", sizedCuratedKey, "--quant", "q4_k_m"},
		},
		{
			name:       "sources_output_separated",
			flagsFirst: []string{"sources", "--output", "json", sizedCuratedKey},
			flagsAfter: []string{"sources", sizedCuratedKey, "--output", "json"},
		},
		{
			name:       "show_by_entity_and_output_mixed",
			flagsFirst: []string{"show", "--by-entity", "--output=json", sizedCuratedKey},
			flagsAfter: []string{"show", sizedCuratedKey, "--by-entity", "--output=json"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := invokeRun(t, tc.flagsFirst)
			after := invokeRun(t, tc.flagsAfter)
			if first.errStr != after.errStr {
				t.Errorf("error diverged by flag position:\n flags-first: %q\n flags-after: %q",
					first.errStr, after.errStr)
			}
			if first.stdout != after.stdout {
				t.Errorf("stdout diverged by flag position:\n flags-first:\n%s\n flags-after:\n%s",
					first.stdout, after.stdout)
			}
			// Sanity: the flags-first form must actually succeed for these fixtures,
			// otherwise an equality of two identical failures would pass vacuously.
			if first.errStr != "" {
				t.Errorf("flags-first %v unexpectedly errored: %s", tc.flagsFirst, first.errStr)
			}
		})
	}
}

// TestFlagsByEntityActuallyApplies guards against the vacuous-equality trap in
// TestFlagsPositionIndependent: it confirms `show KEY --by-entity` truly renders
// the aggregate ENTITY view (which the plain `show KEY` model view never does).
// If reorderArgs were removed, --by-entity after the positional would be ignored
// and `show KEY --by-entity` would fall through to the model-resolution path.
func TestFlagsByEntityActuallyApplies(t *testing.T) {
	e := pickMultiProviderEntity(t)
	out := captureStdout(t, func() {
		if err := run([]string{"show", e.Ref.String(), "--by-entity", "--output=table"}); err != nil {
			t.Fatalf("run show KEY --by-entity (flags after positional) errored: %v", err)
		}
	})
	// The entity view is identified by its "Entity: <ref>" header — the per-model
	// `show` renderer never emits this line.
	if !strings.Contains(out, "Entity: "+e.Ref.String()) {
		t.Errorf("show KEY --by-entity did not render the entity view; --by-entity was ignored after the positional.\noutput:\n%s", out)
	}
}

// TestFlagsMalformed_PositionIndependent pins the dukv7 fix: a value-bearing flag
// that is missing its value must raise the SAME "flag needs an argument" error
// whether it appears before or after the positional. A value can only be a token
// that FOLLOWS the flag, so a positional typed before a trailing value-flag (e.g.
// `providers KEY --quant`) leaves that flag dangling — it must NOT silently
// consume reorderArgs' inserted "--" terminator as a bogus value. A mutant that
// lets the dangling flag swallow the "--" (the pre-fix behavior) diverges the two
// orderings — flags-after would mis-error on "--" (for --quant) or silently
// succeed (for --db-path) — and dies here.
func TestFlagsMalformed_PositionIndependent(t *testing.T) {
	cases := []struct {
		name       string
		flagsFirst []string
		flagsAfter []string
	}{
		{
			// Value-flag with no value, flags-first vs after a positional.
			name:       "dangling_quant",
			flagsFirst: []string{"providers", "--quant"},
			flagsAfter: []string{"providers", sizedCuratedKey, "--quant"},
		},
		{
			name:       "dangling_dbpath",
			flagsFirst: []string{"show", "--db-path"},
			flagsAfter: []string{"show", sizedCuratedKey, "--db-path"},
		},
		{
			// A satisfied bool flag followed by a dangling value flag.
			name:       "bool_then_dangling_value",
			flagsFirst: []string{"show", "--by-entity", "--db-path"},
			flagsAfter: []string{"show", sizedCuratedKey, "--by-entity", "--db-path"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first := invokeRun(t, tc.flagsFirst)
			after := invokeRun(t, tc.flagsAfter)
			// Both orderings must FAIL, identically — including no stray stdout from
			// a silently-accepted bogus "--" value.
			if first.errStr == "" {
				t.Fatalf("flags-first %v should error (flag needs an argument) but succeeded", tc.flagsFirst)
			}
			if first.errStr != after.errStr {
				t.Errorf("malformed error diverged by flag position:\n flags-first: %q\n flags-after: %q",
					first.errStr, after.errStr)
			}
			if !strings.Contains(first.errStr, "flag needs an argument") {
				t.Errorf("flags-first %v error = %q; want 'flag needs an argument'", tc.flagsFirst, first.errStr)
			}
			if after.stdout != "" {
				t.Errorf("flags-after %v emitted stdout %q; a dangling value flag must error, not silently succeed",
					tc.flagsAfter, after.stdout)
			}
		})
	}
}

// TestFlagsBoolThenValue_TableEffect pins the hiwl9 gap: it asserts the EFFECT of
// the bool-vs-value distinction that reorderArgs/flagIsBool make. A bool flag
// (--by-entity) must NOT consume the following positional, so a trailing value
// flag (--output=table) is still seen by flag.Parse and selects the table entity
// view in BOTH orderings. A mutant that makes flagIsBool return false treats
// --by-entity as value-bearing, swallowing the positional; flag.Parse then stops
// at the leftover non-flag and never sees --output=table, so the renderer falls
// back to JSON. Asserting the table EFFECT (the "Entity: " header that only the
// table renderer emits) kills that mutant.
func TestFlagsBoolThenValue_TableEffect(t *testing.T) {
	e := pickMultiProviderEntity(t)
	key := e.Ref.String()
	header := "Entity: " + key
	cases := []struct {
		name string
		args []string
	}{
		{"flags_first", []string{"show", "--by-entity", "--output=table", key}},
		{"value_after_positional", []string{"show", "--by-entity", key, "--output=table"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runErr error
			out := captureStdout(t, func() {
				runErr = run(tc.args)
			})
			if runErr != nil {
				t.Fatalf("run(%v) errored: %v", tc.args, runErr)
			}
			// The table renderer prints the "Entity: <ref>" header; the JSON
			// renderer (the flagIsBool=>false fallback) never does.
			if !strings.Contains(out, header) {
				t.Errorf("run(%v) did not render the TABLE entity view (no %q header); --output=table was dropped.\noutput:\n%s",
					tc.args, header, out)
			}
			if strings.HasPrefix(strings.TrimSpace(out), "{") {
				t.Errorf("run(%v) rendered JSON, not the table view; the bool/value distinction was lost.\noutput:\n%s",
					tc.args, out)
			}
		})
	}
}

// TestExplicitDashDash_Passthrough pins the sub4c gap: an explicit "--" input
// terminator must be consumed by reorderArgs and let the positional through
// unchanged, identical to omitting it. A mutant that drops the `arg == "--"`
// input branch turns a trailing "--" into a dangling unknown flag, which drops
// the positional and yields a usage error instead of the entity output — so the
// two forms diverge and the mutant dies.
func TestExplicitDashDash_Passthrough(t *testing.T) {
	plain := invokeRun(t, []string{"providers", sizedCuratedKey})
	withSep := invokeRun(t, []string{"providers", sizedCuratedKey, "--"})
	if plain.errStr != "" {
		t.Fatalf("control `providers KEY` errored: %s", plain.errStr)
	}
	if withSep.errStr != "" {
		t.Errorf("`providers KEY --` errored %q; an explicit terminator must pass the positional through", withSep.errStr)
	}
	if plain.stdout != withSep.stdout {
		t.Errorf("explicit `--` changed output:\n without --:\n%s\n with --:\n%s", plain.stdout, withSep.stdout)
	}
}

// TestDashLeadingPositional_AfterDashDash pins the 5zlvk gap: a positional that
// itself begins with "-", reachable only after an explicit "--", must be treated
// as a positional and not re-parsed as a flag. The output-side "--" terminator
// reorderArgs inserts is what guards it. A mutant that drops that terminator lets
// flag.Parse see the dash-leading positional as an undefined flag — yielding
// "flag provided but not defined" instead of the entity not-found error — and
// dies here.
func TestDashLeadingPositional_AfterDashDash(t *testing.T) {
	err := run([]string{"show", "--by-entity", "--", "-weird-key"})
	if err == nil {
		t.Fatal("run show --by-entity -- -weird-key returned nil; want a not-found error for the positional")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "not found") {
		t.Errorf("error = %q; want the dash-leading token treated as a (not-found) POSITIONAL, not parsed as a flag", err.Error())
	}
	if strings.Contains(msg, "flag provided but not defined") {
		t.Errorf("error = %q; the dash-leading positional after `--` was mis-parsed as a flag", err.Error())
	}
}

// TestRenderError_WrappedSinglePrefix pins the m1ccu + a16j0 hardening: an error
// whose message CONTAINS "bestiary: " somewhere other than the start (a library
// error wrapped with a bare context prefix, e.g. runSync's
// "sync: open store at X: bestiary: OpenStore: …") must still render with EXACTLY
// one leading prefix — never the redundant "bestiary: bestiary:". A mutant that
// reverts renderError to the prefix-only check re-doubles the token and dies.
func TestRenderError_WrappedSinglePrefix(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"wrapped_sync_context", "sync: open store at /tmp/x: " + errPrefix + "OpenStore: not a directory"},
		{"contains_not_prefix", "oops " + errPrefix + "detail"},
		{"two_embedded_tokens", "ctx: " + errPrefix + "inner: " + errPrefix + "deepest"},
		{"bare_no_token", "plain inline failure with no namespace"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderError(errors.New(tc.in))
			if !strings.HasPrefix(got, errPrefix) {
				t.Errorf("renderError(%q) = %q; must start with %q", tc.in, got, errPrefix)
			}
			if n := strings.Count(got, errPrefix); n != 1 {
				t.Errorf("renderError(%q) = %q has %d %q tokens, want exactly 1", tc.in, got, n, errPrefix)
			}
		})
	}
}

// TestUnknownFlagErrorsRegardlessOfPosition pins that reordering does NOT mask an
// unknown flag: it must still error whether placed before or after the
// positional. reorderArgs treats an unknown flag as value-bearing and defers the
// rejection to flag.Parse, so the unknown-flag error survives in both orderings.
func TestUnknownFlagErrorsRegardlessOfPosition(t *testing.T) {
	orderings := [][]string{
		{"show", "--definitely-not-a-flag", sizedCuratedKey},
		{"show", sizedCuratedKey, "--definitely-not-a-flag"},
		{"show", sizedCuratedKey, "--definitely-not-a-flag", "trailing-value"},
	}
	for _, args := range orderings {
		if err := run(args); err == nil {
			t.Errorf("run(%v) returned nil, want an unknown-flag error", args)
		}
	}
}
