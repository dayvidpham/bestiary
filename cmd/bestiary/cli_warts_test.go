package main

import (
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
