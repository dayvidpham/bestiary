package bestiary

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
// CONTRACT: unknown/uncurated tokens MUST classify as ModifierClassIdentity
// (the fail-safe default) and ClassifyModifier MUST NOT panic for any input —
// rendering and entity keying depend on this graceful-degrade guarantee.
//
// This slice ships the SIGNATURE and a contract-preserving stub; the curated
// modifier-class table (parse/data/modifier_class.json) plus per-family overrides
// that drive the real classification are owned by a later slice. The stub returns
// the documented default for every token, so call sites wire correctly today and
// gain real classification when the table lands behind this same signature.
func ClassifyModifier(token string, fam Family) ModifierClass {
	return ModifierClassIdentity
}

// EntityModifiers returns the IDENTITY-class subset of mods, de-duplicated and in
// canonical order (see CanonicalizeModifiers). It is the projection used to build
// the "{identity-mods}" segment of an entity key: attribute-class modifiers are
// dropped because they do not affect identity. An empty/all-attribute input
// returns nil (the canonical "no identity modifiers" value).
//
// EntityModifiers is implemented in terms of ClassifyModifier, so it is correct
// by construction the moment ClassifyModifier's curated table lands — no rewrite
// needed here. Until then, ClassifyModifier defaults every token to Identity, so
// this returns the full canonicalized set.
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
