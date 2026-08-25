package main

import (
	"embed"
	"fmt"
	"html/template"

	"github.com/dayvidpham/bestiary"
)

// DatastarJSVersion is the pinned upstream version of the vendored Datastar browser
// client (cmd/bestiary-web/assets/datastar.js). The client is VENDORED via go:embed and
// served from the same origin at /assets/datastar.js — there is NO CDN for JavaScript.
// A CDN is permitted for FONTS ONLY. The server-side Go SDK,
// github.com/starfederation/datastar-go v1.2.2, speaks the v1.x SSE protocol that this
// client implements, so the two are version-paired across a major.
//
// Provenance (recorded like the models.dev snapshot refresh, so the vendored bytes can be
// re-derived exactly):
//
//	source:  https://cdn.jsdelivr.net/gh/starfederation/datastar@1.0.2/bundles/datastar.js
//	version: 1.0.2  (upstream banner: "// Datastar v1.0.2")
//	sha256:  2837d87acf6ee0ba8e4e63765926c25a98d63883b02f88be194a86b81d3fd24a
//	bytes:   34083
//
// Refresh (deliberate, occasional — mirrors the "models.dev snapshot refresh" workflow):
//  1. Choose the Datastar JS release matching the datastar-go SDK major (v1.x today) from
//     https://github.com/starfederation/datastar/releases.
//  2. curl -fsSL the bundle URL above (swap the @<ver>) → assets/datastar.js.
//  3. Update DatastarJSVersion + the sha256/bytes above; confirm with `sha256sum`.
//  4. Commit as a separate vendored-asset bump, apart from feature work.
const DatastarJSVersion = "1.0.2"

// assetsFS holds the vendored, same-origin static assets. datastar.js is the pinned
// Datastar client (see DatastarJSVersion); it is served verbatim, never fetched at
// serve time.
//
//go:embed assets/datastar.js
var assetsFS embed.FS

// templatesFS holds the html/template sources for the server-rendered views. The base
// layout carries the approved "Phosphor Terminal" CSS (both color modes) and loads the
// vendored datastar.js; page templates define the "content" block it renders.
//
//go:embed templates/*.html
var templatesFS embed.FS

// templateFuncs are the helpers page templates may call. Kept minimal and pure (no I/O,
// no network) so template rendering stays offline and deterministic.
var templateFuncs = template.FuncMap{
	// dict builds a map for passing multiple named values into a sub-template.
	"dict": func(pairs ...any) map[string]any {
		m := make(map[string]any, len(pairs)/2)
		for i := 0; i+1 < len(pairs); i += 2 {
			key, _ := pairs[i].(string)
			m[key] = pairs[i+1]
		}
		return m
	},
	// bytesGB renders a byte count as a "NN.N GB" figure (GiB, base-1024), or an
	// em-dash when the value is unknown (<= 0). Used for weights/VRAM cells.
	"bytesGB": func(b int64) string {
		if b <= 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f GB", float64(b)/float64(1<<30))
	},
	// pct clamps n/d to an integer percentage in [0,100] for a mini-bar width. A
	// zero or unknown denominator yields 0 (an empty bar), never a divide-by-zero.
	"pct": func(n, d int64) int {
		if d <= 0 || n <= 0 {
			return 0
		}
		p := int(n * 100 / d)
		if p > 100 {
			p = 100
		}
		return p
	},
	// estVRAMGB recomputes a quant row's VRAM at a caller-chosen context and renders
	// it as a "NN.N GB" figure. It returns "" when ctx is not a positive override or
	// exceeds the row's baked maximum context (VRAMContextTokens) — the same clamp the
	// CLI's TYP column applies: no figure is shown above the context the row supports.
	//
	// This is display-only recompute in the MODEL-FIRST direction: you already have an
	// entity and you ask what it costs at a context. The budget-first direction — you
	// state a budget and ask what fits — is the /calculator page, which reads the same
	// arithmetic through the root package's FitOver. Neither writes anything baked.
	"estVRAMGB": func(q bestiary.QuantVRAM, ctx int) string {
		if ctx <= 0 || q.VRAMContextTokens < ctx {
			return ""
		}
		v := q.EstimateVRAM(ctx)
		if v <= 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f GB", float64(v)/float64(1<<30))
	},
	// priceStr renders a per-MTok price pointer as "$N.NN", or an em-dash when the
	// price is genuinely unknown (nil) — never a guessed zero.
	"priceStr": func(p *float64) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("$%.2f", *p)
	},
	// benchScore renders a benchmark claim's SCORE cell: the verbatim upstream value
	// (ScoreRaw) when the score was non-numeric, otherwise the numeric Score. This
	// mirrors the CLI's benchScoreCell so the cell is never blank — a string score
	// ("pass", an em-dash) rides through on ScoreRaw rather than collapsing to a bare 0.
	"benchScore": func(b bestiary.BenchmarkResult) string {
		if b.ScoreRaw != "" {
			return b.ScoreRaw
		}
		return fmt.Sprintf("%g", b.Score)
	},
	// isPrimaryMetadata reports whether a metadata row is the entity's DERIVED primary
	// (the row Entity.Metadata points at). The entity view marks the primary in the
	// per-row attribution rather than hiding the other rows, so a reader can see both
	// which naming is canonical and that the other rows exist.
	"isPrimaryMetadata": func(m bestiary.EntityMetadata, primary *bestiary.EntityMetadata) bool {
		return primary != nil && m.MetadataID == primary.MetadataID
	},
	// orDash renders an empty string as an em-dash so a blank cell reads as "unknown"
	// rather than a rendering gap (the CLI orDash convention).
	"orDash": func(s string) string {
		if s == "" {
			return "—"
		}
		return s
	},
	// plural renders a count with its noun, choosing the singular form at exactly one:
	// plural 1 "entity" "entities" -> "1 entity". The tree labels every node with a count,
	// and "1 families" on a single-family creator reads as a rendering bug to anyone who
	// notices it, which undermines the figures that are correct.
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return fmt.Sprintf("%d %s", n, one)
		}
		return fmt.Sprintf("%d %s", n, many)
	},
}

// pageFiles maps each page name to the template files that compose it. Parsing per-page
// (rather than one big set) keeps each page's "content" block unambiguous — the layout is
// re-parsed into every set, and the last "content" parsed into a set wins, so a set never
// sees two content blocks. The entities page also pulls in results.html, whose
// "entity-results" fragment it renders inline.
//
// "tree" (the front page) and "families" both pull in seriestree.html, which defines the
// ONE "series-subtree" rendering of a hoisted versioned line. Sharing the partial rather
// than duplicating the markup is what keeps a retired "(base)" node from reappearing on one
// page and not the other.
//
// Every page also pulls in palette.html: the command palette lives in the shared layout, so
// its "palette-prompt" opening state must be defined in every page's set. The SAME file is
// parsed into the fragment set, which is what makes the dialog's initial contents and the
// server's patched contents one rendering rather than two that could drift.
//
// There is deliberately no "series" entry: that page was retired, its content absorbed by
// "families", and /series now 404s.
var pageFiles = map[string][]string{
	// Every page set carries palette.html: layout.html renders the palette dialog on all
	// pages and invokes its "palette-prompt" fragment, so a page whose set omitted it would
	// fail to parse. That includes the calculator, which the palette landed after.
	"tree":     {"templates/layout.html", "templates/palette.html", "templates/seriestree.html", "templates/tree.html"},
	"entities": {"templates/layout.html", "templates/palette.html", "templates/results.html", "templates/entities.html"},
	"entity":   {"templates/layout.html", "templates/palette.html", "templates/entity.html"},
	"families": {"templates/layout.html", "templates/palette.html", "templates/seriestree.html", "templates/families.html"},
	// The calculator pulls in calcresults.html, whose "calc-results" fragment it renders
	// inline on first paint and the SSE seam patches on every budget change -- the
	// entities/results.html arrangement, and for the same reason: one rendering of the
	// table means the initial page and every patch cannot disagree.
	"calculator": {"templates/layout.html", "templates/palette.html", "templates/calcresults.html", "templates/calculator.html"},
}

// parseTemplates builds one template set per page (see pageFiles).
func parseTemplates() (map[string]*template.Template, error) {
	out := make(map[string]*template.Template, len(pageFiles))
	for page, files := range pageFiles {
		t, err := template.New(page).Funcs(templateFuncs).ParseFS(templatesFS, files...)
		if err != nil {
			return nil, err
		}
		out[page] = t
	}
	return out, nil
}

// parseFragments builds the standalone template set used to render SSE fragments: the
// "entity-rows" table PatchElements-ed into #entity-results, the "calc-results" block
// patched into #calc-results, and the "palette-options" list PatchElements-ed into
// #palette-results. It carries no layout.
func parseFragments() (*template.Template, error) {
	return template.New("fragments").Funcs(templateFuncs).ParseFS(templatesFS,
		"templates/results.html", "templates/calcresults.html", "templates/palette.html")
}
