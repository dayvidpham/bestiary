package main

import (
	"embed"
	"html/template"
)

// DatastarJSVersion is the pinned upstream version of the vendored Datastar browser
// client (cmd/bestiary-web/assets/datastar.js). The client is VENDORED via go:embed and
// served from the same origin at /assets/datastar.js — there is NO CDN for JavaScript
// (proposal F1; the RQ1 ruling permits a CDN for FONTS ONLY). The server-side Go SDK,
// github.com/starfederation/datastar-go v1.2.2, speaks the v1.x SSE protocol that this
// client implements, so the two are version-paired across a major.
//
// Provenance (recorded like the models.dev snapshot refresh, so a reviewer can re-derive
// the vendored bytes exactly):
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
}

// pageFiles maps each page name to the template files that compose it. Parsing per-page
// (rather than one big set) keeps each page's "content" block unambiguous — the layout is
// re-parsed into every set, and the last "content" parsed into a set wins, so a set never
// sees two content blocks. The index page also pulls in results.html, whose
// "entity-results" fragment it renders inline.
var pageFiles = map[string][]string{
	"index":  {"templates/layout.html", "templates/results.html", "templates/index.html"},
	"entity": {"templates/layout.html", "templates/entity.html"},
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

// parseFragments builds the standalone template set used to render SSE fragments (the
// "entity-results" list PatchElements-ed into #entity-results). It carries no layout.
func parseFragments() (*template.Template, error) {
	return template.New("fragments").Funcs(templateFuncs).ParseFS(templatesFS, "templates/results.html")
}
