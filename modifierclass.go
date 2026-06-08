package bestiary

import (
	"encoding/json"
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
		global:    map[string]ModifierClass{},
		perFamily: map[Family]map[string]ModifierClass{},
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
	return tbl
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

// attributeModifiers returns the ATTRIBUTE-class subset of mods, de-duplicated and
// in canonical order — the complement of EntityModifiers. It is the projection
// used to build the "[attributes]" segment of a canonical render: identity-class
// modifiers are dropped because they belong in the "{identity-mods}" segment. An
// empty/all-identity input returns nil.
func attributeModifiers(mods []string, fam Family) []string {
	canon := CanonicalizeModifiers(mods)
	if len(canon) == 0 {
		return nil
	}
	out := make([]string, 0, len(canon))
	for _, m := range canon {
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
// attribute. Unknown tokens default to identity and are retained.
func EntityModifiers(mods []string, fam Family) []string {
	canon := CanonicalizeModifiers(mods)
	if len(canon) == 0 {
		return nil
	}
	out := make([]string, 0, len(canon))
	for _, m := range canon {
		if ClassifyModifier(m, fam) == ModifierClassIdentity {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
