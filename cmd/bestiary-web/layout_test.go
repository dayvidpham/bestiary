package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// layoutSource reads the base layout template through the SAME embed.FS the binary
// serves from, so the guard below inspects the bytes that actually ship — not a
// source file that could drift from the embed glob.
func layoutSource(t *testing.T) string {
	t.Helper()
	b, err := templatesFS.ReadFile("templates/layout.html")
	if err != nil {
		t.Fatalf("read embedded templates/layout.html: %v", err)
	}
	return string(b)
}

// fontSizeDecl matches a `font-size:` declaration and captures its value up to the
// terminating ';' or '}'. It is deliberately value-agnostic so a hard-coded px, rem, em,
// pt, %, or keyword value is all caught by the same pattern.
var fontSizeDecl = regexp.MustCompile(`font-size\s*:\s*([^;}]+)`)

// scaleTokenDef matches a type-scale token DEFINITION (`--text-foo: 1rem;`) in :root.
var scaleTokenDef = regexp.MustCompile(`--fs-([a-z0-9-]+)\s*:\s*([^;]+);`)

// scaleTokenRef matches a type-scale token REFERENCE (`var(--text-foo)`).
var scaleTokenRef = regexp.MustCompile(`var\(\s*(--fs-[a-z0-9-]+)\s*\)`)

// TestTypeScale_NoHardCodedFontSize is the AC-16 grep guard: EVERY font-size declaration
// in the shipped stylesheet must resolve through a type-scale token, so the page has one
// authority for text size and a reader who changes their browser's base size scales the
// whole document with it.
//
// The guard is phrased as "no literal value", not "no value outside the scale block",
// because the scale itself is declared as `--fs-*:` custom properties — those are not
// `font-size:` declarations at all, so the rule needs no carve-out and cannot be widened
// by accident. A regression that reintroduces `font-size: 12px` anywhere reddens here with
// the offending line quoted.
func TestTypeScale_NoHardCodedFontSize(t *testing.T) {
	src := layoutSource(t)

	var offenders []string
	for i, line := range strings.Split(src, "\n") {
		for _, m := range fontSizeDecl.FindAllStringSubmatch(line, -1) {
			value := strings.TrimSpace(m[1])
			if !scaleTokenRef.MatchString(value) {
				offenders = append(offenders, fmt.Sprintf("layout.html:%d: font-size: %s", i+1, value))
			}
		}
	}
	if len(offenders) != 0 {
		t.Errorf("hard-coded font-size values (each must be a var(--fs-*) scale token):\n  %s",
			strings.Join(offenders, "\n  "))
	}

	// The guard is only meaningful if it actually looked at declarations: a layout that
	// lost its stylesheet would otherwise pass vacuously.
	if n := len(fontSizeDecl.FindAllString(src, -1)); n == 0 {
		t.Fatal("no font-size declarations found in layout.html: the guard would pass vacuously")
	}
}

// TestTypeScale_EveryReferencedTokenIsDefined closes the other half of the contract: a
// declaration may only cite a token the scale actually defines. `font-size: var(--fs-tiny)`
// with no `--fs-tiny` definition is silently size-less in the browser (the declaration is
// invalid at computed-value time), which is exactly the failure a "no literals" rule alone
// would let through.
func TestTypeScale_EveryReferencedTokenIsDefined(t *testing.T) {
	src := layoutSource(t)

	defined := map[string]string{}
	for _, m := range scaleTokenDef.FindAllStringSubmatch(src, -1) {
		defined["--fs-"+m[1]] = strings.TrimSpace(m[2])
	}
	if len(defined) == 0 {
		t.Fatal("no --fs-* scale tokens defined in layout.html")
	}

	referenced := map[string]bool{}
	for _, m := range scaleTokenRef.FindAllStringSubmatch(src, -1) {
		referenced[m[1]] = true
	}
	if len(referenced) == 0 {
		t.Fatal("no --fs-* scale tokens referenced in layout.html")
	}

	var undefined []string
	for tok := range referenced {
		if _, ok := defined[tok]; !ok {
			undefined = append(undefined, tok)
		}
	}
	sort.Strings(undefined)
	if len(undefined) != 0 {
		t.Errorf("font-size cites undefined scale token(s) %v; defined tokens are %v",
			undefined, sortedTokenNames(defined))
	}

	// Every defined step is rem-based: a px scale would ignore the reader's own base
	// font-size preference, which is the readability property the scale exists to give.
	for tok, val := range defined {
		if !strings.HasSuffix(val, "rem") {
			t.Errorf("scale token %s = %q is not rem-based; a px step ignores the reader's base font size", tok, val)
		}
	}

	// A defined-but-unused step is dead weight in the scale, not a failure of the page —
	// report it so the scale stays exactly as large as it needs to be.
	for _, tok := range sortedTokenNames(defined) {
		if !referenced[tok] {
			t.Errorf("scale token %s is defined but never used", tok)
		}
	}
}

// sortedTokenNames returns the map's keys ascending, for deterministic failure messages.
func sortedTokenNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestTypeScale_ClassNamesUnchanged pins the class names the OTHER views and their tests
// depend on across the type-scale refactor. The scale changes sizes, never selectors: a
// stylesheet-wide edit that renamed .vram-bar or .partial would leave the VRAM honesty
// convention (hollow/dashed bar for a weights-only lower bound) silently unstyled while
// every other test still passed.
func TestTypeScale_ClassNamesUnchanged(t *testing.T) {
	src := layoutSource(t)
	for _, class := range []string{"theme-toggle", "badge", "warn", "vram-bar", "partial"} {
		if !strings.Contains(src, "."+class) {
			t.Errorf("class selector .%s disappeared from the stylesheet", class)
		}
	}
}
