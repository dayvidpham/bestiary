package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ModifierClass partitions a trailing model-ID modifier token (e.g. "instruct",
// "thinking", "turbo") into one of two roles relative to model IDENTITY:
//
//   - ModifierClassIdentity: the modifier distinguishes a genuinely different
//     model artifact (different weights/behavior), so it is PART of the entity
//     key. Example: "instruct" — meta/llama@3.1{instruct} is a distinct entity
//     from meta/llama@3.1. Identity modifiers render in the "{...}" segment.
//   - ModifierClassAttribute: the modifier describes a per-instance presentation
//     or runtime knob that does NOT change model identity, so it is EXCLUDED from
//     the entity key. Example: "thinking" — claude/opus@4.5[thinking] is the same
//     entity as claude/opus@4.5. Attribute modifiers render in the "[...]" segment.
//
// The zero value is ModifierClassIdentity: an unknown/uncurated token defaults to
// Identity (fail-safe — never silently collapse two artifacts into one entity).
type ModifierClass int

const (
	// ModifierClassIdentity marks a modifier that is part of the entity key.
	// This is the zero value and the default for unknown tokens.
	ModifierClassIdentity ModifierClass = iota
	// ModifierClassAttribute marks a modifier that is excluded from the entity
	// key (a per-instance presentation/runtime attribute).
	ModifierClassAttribute
)

// String returns the lowercase name of the modifier class.
func (c ModifierClass) String() string {
	switch c {
	case ModifierClassIdentity:
		return "identity"
	case ModifierClassAttribute:
		return "attribute"
	default:
		return "identity"
	}
}

// ClassifyModifier returns the ModifierClass of a single modifier token for the
// given family. The classification is family-aware because the same token can be
// identity-bearing for one family and a mere attribute for another (e.g.
// "turbo": identity for gpt-4-turbo, attribute for a speed-tier alias elsewhere).
//
// Resolution order (per-family override BEATS the global table):
//  1. family_overrides[fam][token] in modifier_class.json, if present;
//  2. global[token] in modifier_class.json, if present;
//  3. otherwise ModifierClassIdentity (the fail-safe default).
//
// CONTRACT: unknown/uncurated tokens MUST classify as ModifierClassIdentity
// (the fail-safe default) and ClassifyModifier MUST NOT panic for any input —
// rendering and entity keying depend on this graceful-degrade guarantee. If the
// embedded table fails to load, classification degrades to the unknown->Identity
// default for every token (never a panic).
func ClassifyModifier(token string, fam Family) ModifierClass {
	if token == "" {
		return ModifierClassIdentity
	}
	return loadModifierClassTable().classify(token, fam)
}

// classify resolves a single token against this table (per-family override beats
// global, default unknown->Identity). It is the testable seam behind
// ClassifyModifier: a nil receiver or an empty table degrades every token to
// ModifierClassIdentity, exactly the load-failure contract, and never panics.
func (t *modifierClassTable) classify(token string, fam Family) ModifierClass {
	if t == nil {
		return ModifierClassIdentity
	}
	key := strings.ToLower(token)
	// Per-family override wins.
	if fam != "" {
		if over, ok := t.perFamily[Family(strings.ToLower(string(fam)))]; ok {
			if c, ok := over[key]; ok {
				return c
			}
		}
	}
	if c, ok := t.global[key]; ok {
		return c
	}
	return ModifierClassIdentity
}

// modifierClassTable is the curated global + per-family modifier classification,
// loaded once from the embedded parse/data/modifier_class.json. A nil/empty table
// is a valid (degraded) state: every lookup then falls through to the
// unknown->Identity default in ClassifyModifier.
type modifierClassTable struct {
	global    map[string]ModifierClass
	perFamily map[Family]map[string]ModifierClass
	// seriesTiers is the PER-FAMILY extension of the curated series-tier token set
	// (parse.go's seriesTierModifiers). It maps a lowercase family to the extra
	// tokens that, for THAT family only, count as a tier trailing the series token
	// inside splitSeriesVariant. Scoping the extension per family is what keeps a
	// tier token added for one letter-series family (mimo) from reclassifying the
	// same token for the other letter-series families (kimi, minimax) — the global
	// set is shared by all three and must never grow for a single family's sake.
	// A family with no entry (the common case) behaves exactly as before.
	seriesTiers map[Family]map[string]struct{}
}

var (
	modClassOnce  sync.Once
	modClassTable *modifierClassTable
)

// loadModifierClassTable loads and caches the modifier-class table exactly once
// (sync.Once). On any load/parse error it caches an EMPTY table rather than
// failing — classification then degrades to unknown->Identity for every token.
// It never returns nil and never panics.
func loadModifierClassTable() *modifierClassTable {
	modClassOnce.Do(func() {
		modClassTable = initModifierClassTable()
	})
	return modClassTable
}

// initModifierClassTable reads parse/data/modifier_class.json from the embedded
// filesystem (parseDataFS, declared in parse.go) and builds the lookup maps. Any
// failure yields an empty-but-non-nil table (graceful degrade); unrecognized
// class strings within the file are skipped rather than aborting the whole load.
func initModifierClassTable() *modifierClassTable {
	tbl := &modifierClassTable{
		global:      map[string]ModifierClass{},
		perFamily:   map[Family]map[string]ModifierClass{},
		seriesTiers: map[Family]map[string]struct{}{},
	}

	raw, err := parseDataFS.ReadFile("parse/data/modifier_class.json")
	if err != nil {
		// Embedded file missing (should not happen in a production build):
		// degrade to the empty table so callers still get unknown->Identity.
		return tbl
	}

	var file struct {
		Comment        string                       `json:"_comment"`
		SchemaVer      int                          `json:"schema_version"`
		Global         map[string]string            `json:"global"`
		FamilyOverride map[string]map[string]string `json:"family_overrides"`
		// SeriesTiers MUST be decoded here: encoding/json silently DROPS any key of
		// the data file that has no matching struct field, so a series_tiers block
		// added to modifier_class.json without this field would load as an empty
		// extension and the lever would fail with no error at all.
		SeriesTiers map[string][]string `json:"series_tiers"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return tbl
	}

	for token, cls := range file.Global {
		if c, ok := parseModifierClass(cls); ok {
			tbl.global[strings.ToLower(token)] = c
		}
	}
	for fam, over := range file.FamilyOverride {
		fkey := Family(strings.ToLower(fam))
		for token, cls := range over {
			c, ok := parseModifierClass(cls)
			if !ok {
				continue
			}
			if tbl.perFamily[fkey] == nil {
				tbl.perFamily[fkey] = map[string]ModifierClass{}
			}
			tbl.perFamily[fkey][strings.ToLower(token)] = c
		}
	}
	for fam, toks := range file.SeriesTiers {
		fkey := Family(strings.ToLower(fam))
		for _, tok := range toks {
			tok = strings.ToLower(strings.TrimSpace(tok))
			if tok == "" {
				continue
			}
			if tbl.seriesTiers[fkey] == nil {
				tbl.seriesTiers[fkey] = map[string]struct{}{}
			}
			tbl.seriesTiers[fkey][tok] = struct{}{}
		}
	}
	return tbl
}

// seriesTierTokensFor returns the curated per-family series-tier extension for fam
// (nil when the family has none, which is the common case). A nil receiver or a
// degraded (load-failed) table returns nil, so the caller falls back to the global
// series-tier set alone — the same graceful-degrade contract classify() carries.
func (t *modifierClassTable) seriesTierTokensFor(fam Family) map[string]struct{} {
	if t == nil || fam == "" {
		return nil
	}
	return t.seriesTiers[Family(strings.ToLower(string(fam)))]
}

// isSeriesTierTokenFor reports whether tok counts as a series-tier token for fam:
// the curated GLOBAL set (seriesTierModifiers, shared by every letter-series
// family) UNION the per-family extension declared in modifier_class.json. It is
// the family-scoped replacement for the bare global membership test and is
// consulted only inside splitSeriesVariant.
func isSeriesTierTokenFor(fam Family, tok string) bool {
	if isSeriesTierToken(tok) {
		return true
	}
	if over := loadModifierClassTable().seriesTierTokensFor(fam); over != nil {
		_, ok := over[strings.ToLower(tok)]
		return ok
	}
	return false
}

// parseModifierClass maps a curated class string ("identity"/"attribute") to a
// ModifierClass. The bool result is false for any unrecognized string so the
// caller can skip a malformed entry instead of mis-classifying it.
func parseModifierClass(s string) (ModifierClass, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "identity":
		return ModifierClassIdentity, true
	case "attribute":
		return ModifierClassAttribute, true
	default:
		// Unrecognized class string: the ModifierClass is meaningless here and the
		// caller ignores it when ok==false, so return the zero value.
		return 0, false
	}
}

// isStageToken reports whether a modifier token is a recognized release-stage
// marker that MIGRATED to the ReleaseStage axis (preview/latest/original — see
// stage.go). Such a token belongs to NEITHER modifier segment: it is neither an
// identity modifier (it does not split the entity) nor an attribute modifier (it
// renders on the separate "stage" axis, not in "[...]"). This check MUST run BEFORE
// ClassifyModifier so a migrated token — now absent from modifier_class.json — never
// hits the unknown->Identity fail-safe and gets promoted into the entity key. It is
// the routing rule that makes "no entity key change" hold by construction: preview/
// latest/original were attribute-class (key-excluded) before the migration and are
// stage-routed (still key-excluded) after it.
func isStageToken(token string) bool {
	_, ok := DetectReleaseStage(token)
	return ok
}

// attributeModifiers returns the ATTRIBUTE-class subset of mods, de-duplicated and
// in canonical order — the complement of EntityModifiers. It is the projection
// used to build the "[attributes]" segment of a canonical render: identity-class
// modifiers are dropped because they belong in the "{identity-mods}" segment, and
// stage-axis tokens (preview/latest/original) are dropped because they render on
// the separate "stage" axis. An empty/all-identity input returns nil.
func attributeModifiers(mods []string, fam Family) []string {
	canon := CanonicalizeModifiers(mods)
	if len(canon) == 0 {
		return nil
	}
	out := make([]string, 0, len(canon))
	for _, m := range canon {
		if isStageToken(m) {
			continue // migrated to the ReleaseStage axis; renders as "stage", not "[...]"
		}
		if ClassifyModifier(m, fam) == ModifierClassAttribute {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// EntityModifiers returns the IDENTITY-class subset of mods, de-duplicated and in
// canonical order (see CanonicalizeModifiers). It is the projection used to build
// the "{identity-mods}" segment of an entity key: attribute-class modifiers are
// dropped because they do not affect identity. An empty/all-attribute input
// returns nil (the canonical "no identity modifiers" value).
//
// EntityModifiers is implemented in terms of ClassifyModifier, so it tracks the
// curated table automatically: a token classifies as identity (and is retained
// here) unless the table — global or per-family override — demotes it to
// attribute. Unknown tokens default to identity and are retained — EXCEPT
// migrated stage-axis tokens (preview/latest/original), which are routed out
// FIRST (via isStageToken, before ClassifyModifier) so their absence from the
// curated table never lets the identity fail-safe pull them into the key.
func EntityModifiers(mods []string, fam Family) []string {
	canon := CanonicalizeModifiers(mods)
	if len(canon) == 0 {
		return nil
	}
	out := make([]string, 0, len(canon))
	for _, m := range canon {
		if isStageToken(m) {
			// Stage-axis token (preview/latest/original): routed out BEFORE the
			// ClassifyModifier fail-safe so a token absent from modifier_class.json
			// is never promoted to identity. It was attribute-class (key-excluded)
			// pre-migration and stays key-excluded — the entity key is unchanged.
			continue
		}
		if ClassifyModifier(m, fam) == ModifierClassIdentity {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ─── series_tiers data-integrity gate ────────────────────────────────────────

// validateSeriesTiersData is the LOUD half of the series_tiers lever. The loader
// above is deliberately silent — initModifierClassTable degrades to an empty table
// rather than panicking, because a library consumer must not be taken down by a
// corrupt embedded file — and that silence is exactly the failure mode the Go
// decoder field was added to prevent in the first place: encoding/json drops an
// unmatched key without a word, so a struct-tag typo, a family name that no longer
// exists, or a value that is a string where a list belongs all "work" and simply
// switch the lever off.
//
// So the data contract is enforced HERE instead, at build/test time, over the same
// embedded bytes the loader reads. Every defect is reported with the family, the
// token, what the file actually says, what it means for decomposition, and what to
// change; the caller reports them all rather than stopping at the first, so one run
// lists every problem in the file.
//
// The four invariants, and why each one exists:
//  1. series_tiers must be a JSON object of family -> list of strings. A scalar or
//     an object in the value position means the whole family entry is dropped.
//  2. every family named must exist in families.json AND declare a series_letter.
//     splitSeriesVariant is the only consumer, and it returns early for a family
//     with no series letter, so an entry for any other family is dead curation that
//     reads as if it were live.
//  3. every token must carry a curated modifier class (global or a family override).
//     A tier's class is what decides whether it lands INSIDE the entity key or
//     beside it; an unclassified token falls through to the unknown->identity
//     fail-safe and silently splits the keyspace.
//  4. a non-empty series_tiers block in the file must produce a non-empty decoded
//     table. This is the struct-tag guard: rename the json tag and invariants 1-3
//     still pass over the raw bytes while the lever is completely inert.
func validateSeriesTiersData() []error {
	const path = "parse/data/modifier_class.json"

	raw, err := parseDataFS.ReadFile(path)
	if err != nil {
		return []error{fmt.Errorf("series_tiers validation: cannot read the embedded curated file %s: %w. "+
			"The file is embedded through parseDataFS at build time, so this means it was removed or renamed "+
			"in the source tree; restore it (the per-family series-tier extension and the whole modifier-class "+
			"table are both inert without it)", path, err)}
	}

	// Deliberately tag-free: the whole point of this gate is that a struct tag can
	// silently stop matching, so the validator indexes the raw object by NAME and
	// shares no decoding path with the loader it is checking.
	var file map[string]json.RawMessage
	if err := json.Unmarshal(raw, &file); err != nil {
		return []error{fmt.Errorf("series_tiers validation: %s does not parse as a JSON object: %w. "+
			"Every family's tier extension is dropped while this stands, so splitSeriesVariant sees only "+
			"the global tier set; fix the JSON syntax", path, err)}
	}
	seriesTiers := map[string]json.RawMessage{}
	if rawBlock, ok := file["series_tiers"]; ok {
		if err := json.Unmarshal(rawBlock, &seriesTiers); err != nil {
			return []error{fmt.Errorf("series_tiers validation: the \"series_tiers\" key of %s is %s, not an "+
				"object mapping a family to its list of tier tokens: %w. The loader unmarshals the same bytes "+
				"into map[string][]string and fails the WHOLE file on this, so every family's tier extension "+
				"is lost; write it as {\"<family>\": [\"<token>\", …]}", path, string(rawBlock), err)}
		}
	}

	pd, perr := loadParseData()
	if perr != nil || pd == nil {
		return []error{fmt.Errorf("series_tiers validation: cannot load parse data to check the family "+
			"names in %s's series_tiers block: %v. The family/series-letter invariant cannot be evaluated "+
			"without families.json; fix that load failure first", path, perr)}
	}
	tbl := loadModifierClassTable()

	var errs []error
	decodedTokens := 0
	for fam, rawToks := range seriesTiers {
		fkey := Family(strings.ToLower(strings.TrimSpace(fam)))

		// (1) shape
		var toks []string
		if err := json.Unmarshal(rawToks, &toks); err != nil {
			errs = append(errs, fmt.Errorf("series_tiers[%q] in %s is %s, not a JSON list of tier tokens: %w. "+
				"encoding/json fails the whole file on this, so EVERY family's tier extension is lost, not just "+
				"this one; write it as a list, e.g. \"%s\": [\"flash\"]", fam, path, string(rawToks), err, fam))
			continue
		}

		// (2) the family must be a real letter-series family
		info, known := pd.families[fkey]
		switch {
		case !known:
			errs = append(errs, fmt.Errorf("series_tiers[%q] in %s names a family that does not exist in "+
				"parse/data/families.json. splitSeriesVariant looks the family up there and returns early when "+
				"it is absent, so these %d token(s) are never consulted and the entry reads as live curation "+
				"while doing nothing; either add the family to families.json with a series_letter, or delete "+
				"this entry", fam, path, len(toks)))
		case info.SeriesLetter == "":
			errs = append(errs, fmt.Errorf("series_tiers[%q] in %s names a family that declares no "+
				"\"series_letter\" in parse/data/families.json. The per-family tier extension is consulted "+
				"ONLY inside the letter-prefix series decomposition, which returns early for such a family, so "+
				"these %d token(s) are dead curation; give %q a series_letter or move the tokens to the "+
				"family's \"family_overrides\" modifier-class entry instead", fam, path, len(toks), fam))
		}

		// (3) every token must carry a curated class
		for _, tok := range toks {
			t := strings.ToLower(strings.TrimSpace(tok))
			if t == "" {
				errs = append(errs, fmt.Errorf("series_tiers[%q] in %s contains an empty tier token. An empty "+
					"token can never match an id segment, so it is silently skipped by the loader; remove it "+
					"from the list", fam, path))
				continue
			}
			decodedTokens++
			_, hasGlobal := tbl.global[t]
			_, hasOverride := tbl.perFamily[fkey][t]
			if !hasGlobal && !hasOverride {
				errs = append(errs, fmt.Errorf("series_tiers[%q] in %s promotes the token %q, but %q has no "+
					"curated modifier class — it is absent from both the \"global\" block and the "+
					"\"family_overrides\".%q block of the same file. A promoted tier's CLASS is what decides "+
					"whether it lands inside the entity key ({identity}) or beside it, and an unclassified "+
					"token falls through to the unknown->identity fail-safe, so this silently splits the "+
					"%s keyspace on a token nobody classified; add %q to \"global\" or to "+
					"\"family_overrides\".%q with \"identity\" or \"attribute\"",
					fam, path, t, t, fam, fam, t, fam))
			}
		}
	}

	// (4) struct-tag guard: the raw file declares tokens but the decoded table has none.
	if decodedTokens > 0 {
		loaded := 0
		for _, toks := range tbl.seriesTiers {
			loaded += len(toks)
		}
		if loaded == 0 {
			errs = append(errs, fmt.Errorf("series_tiers in %s declares %d tier token(s) but the decoded "+
				"table holds NONE. encoding/json drops a data-file key with no matching struct field without "+
				"reporting anything, so the whole per-family tier extension is inert: check the `json:\"series_tiers\"` "+
				"tag on the SeriesTiers field of the anonymous struct in initModifierClassTable (modifierclass.go)",
				path, decodedTokens))
		}
	}
	return errs
}
