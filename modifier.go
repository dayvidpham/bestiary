package bestiary

import (
	"sort"
	"strings"
)

// The Modifier field is a LIST ([]string), so a
// model may carry MULTIPLE trailing qualifiers losslessly (e.g. kimi-k2-thinking-turbo
// → [thinking, turbo]; llama-3.2-…-vision-instruct → [vision, instruct]). This file
// owns the single, deterministic CANONICAL ORDER for that list — the byte-stability
// anchor for codegen (models_static_gen.go) and for ModelRef.Format across all schemes.
//
// RATIFIED ORDER: class rank first, then a
// fixed within-class order (NOT alpha — "thinking" precedes "vision" deliberately),
// with an alphabetical fallback for any token outside the curated table.
//
//	(1) capability: thinking, vision, multimodal, reasoning, non-reasoning
//	(2) speed:      turbo, fast   (+ series-tier speed promotions: highspeed, lightning, precision)
//	(3) format/stage: instruct, chat, base, preview, latest, code
//	(4) residual tier/size (series-scoped promotions), alpha: deep-research, flash, mini, omni, pro
//
// "think" is the legacy short alias of "thinking" and sorts adjacent to it.
var canonicalModifierOrder = []string{
	// (1) capability
	"thinking", "think", "vision", "multimodal", "reasoning", "non-reasoning",
	// (2) speed-tier
	"turbo", "fast", "highspeed", "lightning", "precision",
	// (3) format/stage
	"instruct", "chat", "base", "original", "preview", "latest", "code",
	// (4) residual tier/size promotions (series-scoped), kept in a deterministic slot
	"deep-research", "flash", "mini", "omni", "pro",
}

// modifierRankTable maps each curated modifier token to its canonical rank. Built once
// at package init from canonicalModifierOrder (index == rank).
var modifierRankTable = func() map[string]int {
	m := make(map[string]int, len(canonicalModifierOrder))
	for i, tok := range canonicalModifierOrder {
		m[tok] = i
	}
	return m
}()

// modifierRank returns the canonical sort rank of a modifier token. Curated tokens
// sort by their table index; any uncurated token sorts AFTER all curated tokens
// (rank == len(table)) and ties among uncurated tokens break alphabetically (handled
// by the caller's secondary string compare). This keeps the ordering total and
// deterministic even for tokens not yet enumerated in the table.
func modifierRank(tok string) int {
	if r, ok := modifierRankTable[strings.ToLower(tok)]; ok {
		return r
	}
	return len(canonicalModifierOrder)
}

// modifierLess reports whether modifier token a should sort before b in canonical
// order: primary key is the class/table rank, secondary key is the lowercase token
// (alphabetical) so the ordering is total and stable.
func modifierLess(a, b string) bool {
	ra, rb := modifierRank(a), modifierRank(b)
	if ra != rb {
		return ra < rb
	}
	return strings.ToLower(a) < strings.ToLower(b)
}

// CanonicalizeModifiers returns a NEW slice containing the modifiers de-duplicated
// (case-insensitively, first spelling wins) and sorted into the canonical order. An
// empty or all-empty input returns nil (the canonical "no modifiers" value), so the
// public contract is: empty → nil; single → 1-elem; multiple → canonical-ordered.
// It never mutates the input slice.
func CanonicalizeModifiers(mods []string) []string {
	if len(mods) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(mods))
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		if m == "" {
			continue
		}
		k := strings.ToLower(m)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil
	}
	sort.SliceStable(out, func(i, j int) bool {
		return modifierLess(out[i], out[j])
	})
	return out
}

// modifierKey returns a stable, order-independent string key for a modifier list:
// the canonical-ordered tokens joined by ",". Two lists that are permutations of the
// same set produce the IDENTICAL key (the modifier set-independence guarantee used
// by resolve.go's group key and by the cross-provider divergence comparison).
// An empty/nil list returns "".
func modifierKey(mods []string) string {
	c := CanonicalizeModifiers(mods)
	if len(c) == 0 {
		return ""
	}
	return strings.Join(c, ",")
}
