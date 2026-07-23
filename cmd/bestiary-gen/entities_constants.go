package main

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strings"

	"github.com/dayvidpham/bestiary"
)

// --------------------------------------------------------------------------
// Entity_ constants generation (entities_constants_gen.go)
//
// This replaces the removed Model__ surface (one constant per provider-flavored
// (ID, Provider) pair) with ONE Entity__ constant per model ENTITY: the canonical
// decomposed identity, provider-agnostic. Provider access moves to the API
// (registry.ProvidersOf / ProvidersOfModel) — no provider leaks into the constant
// namespace.
// --------------------------------------------------------------------------

// outputEntitiesConstantsPath is the file generateEntitiesConstantsSource writes: one
// Entity__* constant per registry entity, valued by the canonical entity key string.
const outputEntitiesConstantsPath = "entities_constants_gen.go"

// entityConstName renders an EntityRef as its Entity__ Go identifier under the
// ratified word-sentinel grammar (the __Version_/__Size_ word sentinels):
//
//	Entity__ + Pascal(family)
//	         [ + __ + Pascal(variant) ]
//	         [ + __Version_ + sanitize(version) ]
//	         [ + __Size_    + sanitize(paramSize) ]
//	         [ + __ + Pascal(mod) ... ]   (identity modifiers, sorted)
//
// The segment-type sentinels (Version_, Size_) are what keep the grammar INJECTIVE on
// the current catalog: without them deepseek@3.2 (version) and deepseek/v3.2 (variant)
// would both collapse to Entity__Deepseek__V3_2. With them they render distinctly
// (Entity__Deepseek__Version_3_2 vs Entity__Deepseek__V3_2), and qwen@3.5 vs qwen@35
// stay apart (Version_3_5 vs Version_35) because the sanitizer is separator-preserving
// (never camel-folded). The grammar is NOT globally injective — a variant and a
// modifier that spell the same token render the same segment — so buildEntityConstEntries
// enforces injectivity with a loud codegen guard rather than a silent _N ordinal.
//
// Sanitize is the single shared transform: every non-[A-Za-z0-9] rune becomes '_'
// (so '.', '-', '/', spaces, etc. are preserved as separators, not folded away).
// Pascal(segment) uppercases only the FIRST ASCII letter, then sanitizes the rest —
// there is no camel-fold, so "text-embedding" → "Text_embedding", "r7b" → "R7b".
func entityConstName(ref bestiary.EntityRef) string {
	var b strings.Builder
	b.WriteString("Entity__")
	b.WriteString(pascalIdentSegment(string(ref.Family)))
	if ref.Variant != "" {
		b.WriteString("__")
		b.WriteString(pascalIdentSegment(ref.Variant))
	}
	if ref.Version != "" {
		b.WriteString("__Version_")
		b.WriteString(sanitizeIdentSegment(ref.Version))
	}
	if ref.ParamSize != "" {
		b.WriteString("__Size_")
		b.WriteString(sanitizeIdentSegment(ref.ParamSize))
	}
	// Identity modifiers, sorted for a deterministic, order-independent name (the key's
	// own modifier set is already canonical/de-duped; sorting a copy is a bijection on
	// the set, so distinct mod SETS never converge here).
	mods := append([]string(nil), ref.Modifier...)
	sort.Strings(mods)
	for _, m := range mods {
		b.WriteString("__")
		b.WriteString(pascalIdentSegment(m))
	}
	return b.String()
}

// sanitizeIdentSegment maps every non-alphanumeric ([A-Za-z0-9]) rune to '_',
// preserving every separator as a distinct underscore (no collapsing, no camel-fold).
// This is the separator-preserving discipline the injectivity of the grammar rests on:
// "3.2" → "3_2" is DISTINCT from "32" → "32".
func sanitizeIdentSegment(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		rs[i] = '_'
	}
	return string(rs)
}

// pascalIdentSegment uppercases only the first ASCII letter of s (plain Pascal — no
// brand-casing table, no camel-fold), then sanitizes the remainder. A leading digit is
// left as-is (e.g. "4o" → "4o"); the "Entity__" prefix guarantees the overall
// identifier still begins with a letter.
func pascalIdentSegment(s string) string {
	if s == "" {
		return ""
	}
	rs := []rune(s)
	if rs[0] >= 'a' && rs[0] <= 'z' {
		rs[0] -= 'a' - 'A'
	}
	return sanitizeIdentSegment(string(rs))
}

// entityConstEntry is one emitted Entity__ constant: its Go identifier name and the
// canonical entity-key string it is valued by.
type entityConstEntry struct {
	name string
	key  string
}

// buildEntityConstEntries derives the Entity__ constant set from the minted nomina and
// enforces the INJECTIVITY GUARD. It consumes the Preferred (Canonical-scheme) nomina —
// exactly the Preferred designation of each entity — so the constant list is the SAME
// entity index the shared MintNomina path produces (one source of truth, no parallel
// enumeration). Alias/ProviderID nomina are ignored: a constant names an entity, and
// keySet gates out any Canonical claim that resolves to an entity absent from the
// catalog (a constant must reference a real registry entity).
//
// The guard: if two DISTINCT entity keys render to the same Go identifier the bake
// FAILS LOUDLY with an actionable error naming both keys — never a silent _N ordinal.
// The grammar is injective on the current catalog, so this is a defense against a future
// key (e.g. a variant/modifier spelling coincidence) that would collide.
func buildEntityConstEntries(nomina []bestiary.Nomen, keySet map[string]bool) ([]entityConstEntry, error) {
	byName := make(map[string]string, len(keySet))
	entries := make([]entityConstEntry, 0, len(keySet))
	for _, n := range nomina {
		if n.Scheme != bestiary.NomenSchemeCanonical {
			continue
		}
		if keySet != nil && !keySet[n.Value] {
			continue
		}
		name := entityConstName(n.ResolvesTo)
		if prev, dup := byName[name]; dup {
			if prev == n.Value {
				continue // same entity minted twice (defensive) — not a collision
			}
			return nil, fmt.Errorf(
				"entity constant name collision: %q is produced by two DISTINCT entity keys %q and %q\n"+
					"  What: two model entities render to the same Go identifier\n"+
					"  Why: the Entity__ grammar is injective on the current catalog, but these two keys "+
					"coincide under it (e.g. a variant segment and a modifier segment spelling the same token)\n"+
					"  Where: cmd/bestiary-gen entityConstName / buildEntityConstEntries (the entities_constants_gen.go bake)\n"+
					"  When: entity-constants codegen, before writing %s\n"+
					"  How to fix: disambiguate the grammar (introduce a segment-type marker for the colliding "+
					"segment) or curate one of the entities; do NOT paper over it with an opaque _N ordinal",
				name, prev, n.Value, outputEntitiesConstantsPath,
			)
		}
		byName[name] = n.Value
		entries = append(entries, entityConstEntry{name: name, key: n.Value})
	}
	// Deterministic output order: ascending by constant name (the names are unique, so
	// this is a total order).
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries, nil
}

// generateEntitiesConstantsSource renders entities_constants_gen.go: one Entity__
// constant per registry entity (valued by its canonical key string), the
// allEntityConstants backing array, and the EntityKeys() accessor. It groups the
// freshly-decomposed models into the entity index (buildEntitySet — the SAME grouping
// the registry uses) AND folds in the metadata-only standalone entities the runtime
// registry synthesizes (attachBakedMetadataToIndex), so the constant set matches
// Entities() exactly. It then mints the nomina through the shared MintNomina path and
// derives the constants from the canonical nomina. A name collision is a LOUD error
// (see buildEntityConstEntries).
//
// Interaction with redundant-modifier suppression (suppression.go): a seeded entity
// mints TWO canonical nomina — the preferred (modifier-omitting) spelling and the key
// spelling, admitted. buildEntityConstEntries keeps only the nomen whose VALUE is in
// keySet, i.e. the KEY spelling, so a constant's value stays an entity key that
// parseEntityKey round-trips and the registry resolves. Constant NAMES derive from
// ResolvesTo, which suppression never touches. A seed entry therefore leaves this file
// byte-identical — by construction, not by coincidence.
func generateEntitiesConstantsSource(models []bestiary.ModelInfo, metadata []bestiary.EntityMetadata) ([]byte, error) {
	entities := buildEntitySet(models)
	keySet := make(map[string]bool, len(entities))
	for _, e := range entities {
		keySet[e.Ref.String()] = true
	}
	// Fold in the metadata-only standalones the metadata↔entity join synthesizes for
	// rows whose family is genuinely absent from the catalog (the runtime registry
	// appends these in attachBakedMetadataToIndex). A standalone whose key collides with
	// a real entity is dropped — never overwrites — matching the runtime discipline.
	_, _, standalone := bestiary.JoinEntityMetadata(entities, metadata)
	for _, s := range standalone {
		k := s.Ref.String()
		if keySet[k] {
			continue
		}
		keySet[k] = true
		entities = append(entities, s)
	}
	// The full entity set exists exactly here, so this is where the entity-relative
	// suppression guards run: a seed entry naming an absent entity is dead curation,
	// and two entities preferring one spelling would make the preferred naming
	// ambiguous. Both abort the bake rather than degrade.
	if err := bestiary.ValidateSuppression(entities); err != nil {
		return nil, fmt.Errorf("validate suppression seed against the catalog: %w", err)
	}
	// Same reason, same place: the beta-always-stage rule is entity-relative, so it is
	// checked once the full entity set exists and ABORTS the bake rather than degrading.
	if err := bestiary.ValidateNoBetaInIdentity(entities); err != nil {
		return nil, fmt.Errorf("validate the beta-always-stage rule against the catalog: %w", err)
	}
	nomina := bestiary.MintNomina(entities)
	entries, err := buildEntityConstEntries(nomina, keySet)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteString("// Code generated by bestiary-gen. DO NOT EDIT.\n")
	buf.WriteString("\n")
	buf.WriteString("package bestiary\n\n")

	if len(entries) > 0 {
		buf.WriteString("// Entity__* constants provide compile-time references to every model ENTITY in\n")
		buf.WriteString("// the static registry. Each value is the canonical entity key\n")
		buf.WriteString("// (family[/variant][@version][#size]{identity-mods}); names follow the grammar:\n")
		buf.WriteString("//   Entity__<Family>__<Variant>?__Version_<version>?__Size_<size>?__<Mod>?...\n")
		buf.WriteString("// Segments are plain-Pascal (no brand-casing, no camel-fold); the Version_/Size_\n")
		buf.WriteString("// word sentinels and the separator-preserving sanitizer (every non-alphanumeric\n")
		buf.WriteString("// character becomes '_') keep the names injective. Provider access is via the API\n")
		buf.WriteString("// (ProvidersOf / ProvidersOfModel), never the constant namespace.\n")
		buf.WriteString("const (\n")
		for _, e := range entries {
			fmt.Fprintf(&buf, "\t%s = %q\n", e.name, e.key)
		}
		buf.WriteString(")\n\n")
	}

	buf.WriteString("// allEntityConstants is the complete list of generated Entity__* constant values\n")
	buf.WriteString("// (canonical entity keys), in the same ascending-name order as the const block.\n")
	buf.WriteString("var allEntityConstants = [...]string{\n")
	for _, e := range entries {
		fmt.Fprintf(&buf, "\t%s,\n", e.name)
	}
	buf.WriteString("}\n\n")

	buf.WriteString("// EntityKeys returns the canonical entity-key values of every generated Entity__*\n")
	buf.WriteString("// constant. The returned slice is a defensive copy; mutating it does not affect\n")
	buf.WriteString("// future calls. See ProvidersOf in registry.go to enumerate an entity's providers.\n")
	buf.WriteString("func EntityKeys() []string {\n")
	buf.WriteString("\tout := make([]string, len(allEntityConstants))\n")
	buf.WriteString("\tcopy(out, allEntityConstants[:])\n")
	buf.WriteString("\treturn out\n")
	buf.WriteString("}\n")

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf(
			"go/format entities_constants_gen.go: %w\n"+
				"  What: the generated entity-constants source is not syntactically valid\n"+
				"  Why: a codegen template bug produced invalid Go\n"+
				"  Where: cmd/bestiary-gen generateEntitiesConstantsSource\n"+
				"  How to fix: inspect the unformatted buffer for syntax errors\n"+
				"  Raw source (first 2000 bytes):\n%s",
			err, truncate(buf.String(), 2000),
		)
	}
	return formatted, nil
}
