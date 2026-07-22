package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Redundant-modifier suppression (GH#7).
//
// SUPPRESSION IS A NAMING-STATUS POLICY, NEVER A KEY CHANGE. A curated seed entry
// declares that one identity-class modifier is REDUNDANT for one entity, because the
// capability the modifier names is innate to that entity (a model that is always a
// reasoning model does not need a "thinking" token to say so). The policy has exactly
// three effects, and no others:
//
//  1. The spelling WITH the modifier — the entity key itself — stays recorded as an
//     ADMITTED nomen: it is never dropped and stays resolvable.
//  2. The PREFERRED nomen's VALUE omits the modifier. Human render sites (the `Entity:`
//     headers, the entities table) show the preferred value.
//  3. The ENTITY KEY IS UNTOUCHED. Identity, store keys, lineage edges, EntitySource
//     attestations, taxonomy grouping and the Entity__ constants all key off
//     EntityRef.String(), which this file never changes. Zero identity churn.
//
// The policy is therefore fully REVERSIBLE: delete the seed entry and the preferred
// value grows the modifier back, because the preferred value is computed, never baked.
//
// Interaction with the Entity__ constants (documented, not exercised — the seed ships
// EMPTY): the constants' VALUE is the entity KEY, so a seed entry does not change any
// constant value. Were the constants ever re-derived from preferred nomina instead, a
// seed entry WOULD rename one — and its value would then be a spelling that
// parseEntityKey decomposes to a DIFFERENT ref, so the lookup path would have to
// consult NomenLookup first. That is why the constants stay key-valued, and why the
// end-to-end fence asserts the preferred spelling resolves through NomenLookup.
//
// The loader follows the lineage.go precedent exactly: go:embed + sync.Once, a LOUD
// validated parse used by codegen, and a graceful-degrade runtime accessor that returns
// a non-nil EMPTY table on any missing/corrupt file — so a bad seed degrades to "no
// suppression" (every preferred value equals its key), never a panic.

// suppressionRefJSON is the entity tuple a seed entry applies to. It mirrors
// nomenClaimRefJSON: family/variant/version/param_size plus identity-class modifiers,
// decomposed into the same EntityRef key the registry produces.
type suppressionRefJSON struct {
	Family    Family   `json:"family"`
	Variant   string   `json:"variant"`
	Version   string   `json:"version"`
	ParamSize string   `json:"param_size,omitempty"`
	Modifier  []string `json:"modifier,omitempty"`
}

// suppressionEntryJSON is one curated suppression declaration.
//
//   - Entity is the entity whose preferred naming drops the redundant modifier.
//   - Suppress lists the modifier tokens declared redundant. Every token MUST already
//     be an identity modifier of Entity — a token that is not present would make the
//     entry a silent no-op, which is rejected.
//   - Reason is the curation rationale (WHY the capability is innate). Required: an
//     unexplained naming policy is un-reviewable.
//   - SourceURL is WHO documents the innate capability (the lab page). Optional, and
//     deliberately distinct from the ingest provenance (Nomen.Source): a *who says so*
//     and a *which catalog we read* are different provenance levels.
type suppressionEntryJSON struct {
	Entity    suppressionRefJSON `json:"entity"`
	Suppress  []string           `json:"suppress"`
	Reason    string             `json:"reason"`
	SourceURL string             `json:"source_url,omitempty"`
}

// suppressionFileJSON is the top-level shape of parse/data/suppression_seed.json.
type suppressionFileJSON struct {
	Comment       string                 `json:"_comment"`
	SchemaVersion int                    `json:"schema_version"`
	Entries       []suppressionEntryJSON `json:"entries"`
}

// SuppressionEntry is one validated seed entry: the entity it applies to, the
// identity-class modifier tokens declared redundant for it, and the curation
// provenance. It is exported so the per-entry fence can enumerate the shipped seed and
// require a literal before/after row for every entry.
type SuppressionEntry struct {
	// Entity is the entity whose preferred naming omits the suppressed modifiers. Its
	// key (EntityRef.String()) is NOT changed by the policy.
	Entity EntityRef
	// Suppress holds the redundant identity-modifier tokens, sorted and deduplicated.
	Suppress []string
	// Reason is the curation rationale for calling the capability innate.
	Reason string
	// SourceURL is WHO documents the innate capability (claim attribution). May be empty.
	SourceURL string
}

// suppressionTable is the parsed, validated seed: the entries in stable order plus the
// entity-key → suppressed-token index the render path consults.
type suppressionTable struct {
	entries []SuppressionEntry
	byKey   map[string][]string
}

func emptySuppressionTable() *suppressionTable {
	return &suppressionTable{byKey: map[string][]string{}}
}

var (
	suppressionOnce sync.Once
	suppressionTbl  *suppressionTable
	suppressionErr  error
)

// loadSuppression reads and validates parse/data/suppression_seed.json from the
// embedded FS exactly once. The cached error is non-nil when the file is missing,
// malformed, or fails validation; ValidateSuppressionSeed surfaces it so codegen fails
// loudly on bad curation.
func loadSuppression() (*suppressionTable, error) {
	suppressionOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/suppression_seed.json")
		if err != nil {
			suppressionErr = fmt.Errorf(
				"bestiary suppression: load suppression_seed.json: %w\n"+
					"  What: cannot read the embedded redundant-modifier suppression seed\n"+
					"  Where: parse/data/suppression_seed.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/suppression_seed.json is present before building",
				err,
			)
			return
		}
		suppressionTbl, suppressionErr = parseSuppressionSeed(raw)
	})
	return suppressionTbl, suppressionErr
}

// loadSuppressionSafe returns the cached table, or a non-nil EMPTY (degraded) table
// when loading failed. It never returns nil and never panics — a corrupt seed degrades
// to "no suppression", so every preferred nomen value equals its entity key.
func loadSuppressionSafe() *suppressionTable {
	t, err := loadSuppression()
	if err != nil || t == nil {
		return emptySuppressionTable()
	}
	return t
}

// parseSuppressionSeed parses and validates the curated seed JSON. It is the testable
// seam behind loadSuppression and performs every check that is SELF-CONTAINED (needs
// only the entry itself): a known base family, a non-empty reason, at least one
// non-empty suppressed token, every suppressed token actually present among the
// entity's identity modifiers, and no duplicate entity across entries. The
// entity-relative checks (does the entity exist in the catalog, does the resulting
// preferred value collide) need the entity set and live in ValidateSuppression.
func parseSuppressionSeed(raw []byte) (*suppressionTable, error) {
	var file suppressionFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary suppression: parse suppression_seed.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/suppression_seed.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := &suppressionTable{byKey: make(map[string][]string, len(file.Entries))}
	for i, e := range file.Entries {
		if !e.Entity.Family.IsKnown() {
			return nil, fmt.Errorf(
				"bestiary suppression: invalid entry #%d: unknown base family %q\n"+
					"  What: a suppression entry names a family that is not a known base family\n"+
					"  Where: parse/data/suppression_seed.json entries[%d].entity.family\n"+
					"  Why: every entry must apply to a known base family (Family.IsKnown)\n"+
					"  How to fix: correct the family, or register it in family.go (curatedBaseFamilies)",
				i, e.Entity.Family, i,
			)
		}
		if strings.TrimSpace(e.Reason) == "" {
			return nil, fmt.Errorf(
				"bestiary suppression: invalid entry #%d (family=%q): empty reason\n"+
					"  What: a suppression entry has no curation rationale\n"+
					"  Where: parse/data/suppression_seed.json entries[%d].reason\n"+
					"  Why: suppression is a naming-policy judgement; an unexplained entry cannot be reviewed or reversed with confidence\n"+
					"  How to fix: state why the capability is innate to this entity",
				i, e.Entity.Family, i,
			)
		}

		ref := EntityRef{
			Family:    e.Entity.Family,
			Variant:   e.Entity.Variant,
			Version:   e.Entity.Version,
			ParamSize: e.Entity.ParamSize,
			Modifier:  EntityModifiers(e.Entity.Modifier, e.Entity.Family),
		}
		key := ref.String()

		present := make(map[string]struct{}, len(ref.Modifier))
		for _, m := range ref.Modifier {
			present[m] = struct{}{}
		}

		seen := make(map[string]struct{}, len(e.Suppress))
		tokens := make([]string, 0, len(e.Suppress))
		for _, raw := range e.Suppress {
			tok := strings.TrimSpace(raw)
			if tok == "" {
				return nil, fmt.Errorf(
					"bestiary suppression: invalid entry #%d (entity=%q): empty suppress token\n"+
						"  What: a suppression entry lists an empty modifier token\n"+
						"  Where: parse/data/suppression_seed.json entries[%d].suppress\n"+
						"  How to fix: remove the empty element, or name the redundant modifier",
					i, key, i,
				)
			}
			if _, dup := seen[tok]; dup {
				continue
			}
			if _, ok := present[tok]; !ok {
				return nil, fmt.Errorf(
					"bestiary suppression: invalid entry #%d (entity=%q): modifier %q is not an identity modifier of the entity\n"+
						"  What: the entry declares a modifier redundant that the entity's key does not carry\n"+
						"  Where: parse/data/suppression_seed.json entries[%d].suppress\n"+
						"  Why: suppressing an absent token is a silent no-op, so the entry would be dead curation (identity modifiers of this entity: %v)\n"+
						"  How to fix: name a modifier the entity actually carries, or drop the entry",
					i, key, tok, i, ref.Modifier,
				)
			}
			seen[tok] = struct{}{}
			tokens = append(tokens, tok)
		}
		if len(tokens) == 0 {
			return nil, fmt.Errorf(
				"bestiary suppression: invalid entry #%d (entity=%q): no modifiers suppressed\n"+
					"  What: a suppression entry lists no redundant modifier\n"+
					"  Where: parse/data/suppression_seed.json entries[%d].suppress\n"+
					"  Why: an entry that suppresses nothing changes no naming, so it is dead curation\n"+
					"  How to fix: name at least one redundant identity modifier, or delete the entry",
				i, key, i,
			)
		}
		sort.Strings(tokens)

		if _, dup := tbl.byKey[key]; dup {
			return nil, fmt.Errorf(
				"bestiary suppression: invalid entry #%d: duplicate entity %q\n"+
					"  What: two suppression entries apply to the same entity\n"+
					"  Where: parse/data/suppression_seed.json entries[%d].entity\n"+
					"  Why: one entity has exactly one preferred naming; two entries would race to define it\n"+
					"  How to fix: merge the two entries' suppress lists into one entry",
				i, key, i,
			)
		}
		tbl.byKey[key] = tokens
		tbl.entries = append(tbl.entries, SuppressionEntry{
			Entity:    ref,
			Suppress:  tokens,
			Reason:    strings.TrimSpace(e.Reason),
			SourceURL: strings.TrimSpace(e.SourceURL),
		})
	}

	sort.Slice(tbl.entries, func(i, j int) bool {
		return tbl.entries[i].Entity.String() < tbl.entries[j].Entity.String()
	})
	return tbl, nil
}

// SuppressionSeed returns the shipped, validated suppression seed — one entry per
// entity carrying a redundant modifier — in stable entity-key order. It degrades to an
// empty slice when the seed is missing or corrupt. The returned slice and its token
// slices are copies, so a caller cannot mutate the cached table.
func SuppressionSeed() []SuppressionEntry {
	tbl := loadSuppressionSafe()
	out := make([]SuppressionEntry, 0, len(tbl.entries))
	for _, e := range tbl.entries {
		clone := e
		clone.Entity.Modifier = append([]string(nil), e.Entity.Modifier...)
		clone.Suppress = append([]string(nil), e.Suppress...)
		out = append(out, clone)
	}
	return out
}

// SuppressedModifiers returns the identity-modifier tokens the curated seed declares
// redundant for ref, sorted. It returns nil when no entry applies — the overwhelmingly
// common case, and the only case while the seed ships empty.
func SuppressedModifiers(ref EntityRef) []string {
	return suppressedModifiersWith(ref, loadSuppressionSafe())
}

func suppressedModifiersWith(ref EntityRef, tbl *suppressionTable) []string {
	toks, ok := tbl.byKey[ref.String()]
	if !ok || len(toks) == 0 {
		return nil
	}
	return append([]string(nil), toks...)
}

// PreferredNomenValue returns the VALUE of ref's preferred nomen: the canonical key
// with every seed-suppressed modifier omitted. With no applicable seed entry it is
// exactly ref.String(), byte for byte — so with an empty seed this function is the
// identity on every entity in the catalog.
//
// It never mutates ref (the modifier slice is rebuilt, never sliced in place) and it
// never changes identity: the caller's key is still ref.String().
func PreferredNomenValue(ref EntityRef) string {
	return preferredNomenValueWith(ref, loadSuppressionSafe())
}

func preferredNomenValueWith(ref EntityRef, tbl *suppressionTable) string {
	suppressed := suppressedModifiersWith(ref, tbl)
	if len(suppressed) == 0 {
		return ref.String()
	}
	drop := make(map[string]struct{}, len(suppressed))
	for _, s := range suppressed {
		drop[s] = struct{}{}
	}
	kept := make([]string, 0, len(ref.Modifier))
	for _, m := range ref.Modifier {
		if _, skip := drop[m]; skip {
			continue
		}
		kept = append(kept, m)
	}
	preferred := ref
	preferred.Modifier = kept
	return preferred.String()
}

// PreferredName returns this entity's preferred naming — the canonical key with any
// seed-suppressed redundant modifier omitted. It is what human render sites print. The
// entity's KEY is unchanged and is still Ref.String(); the key spelling remains
// recorded as an admitted nomen (see Nomina).
func (e Entity) PreferredName() string {
	return PreferredNomenValue(e.Ref)
}

// ValidateSuppressionSeed is the LOUD codegen guard over the seed FILE: it surfaces the
// load/parse error the runtime accessor swallows, so a malformed or dead seed entry
// fails the bake instead of silently degrading to "no suppression".
func ValidateSuppressionSeed() error {
	_, err := loadSuppression()
	return err
}

// ValidateSuppression is the LOUD codegen guard over the seed AGAINST THE CATALOG. It
// performs the two checks that need the entity set:
//
//   - EXISTENCE: every seed entry must name an entity that actually exists, otherwise
//     the entry is dead curation that silently protects nothing.
//   - COLLISION: a suppressed preferred value must not equal any other entity's
//     preferred value. Two entities sharing one preferred spelling would make the
//     preferred naming ambiguous — precisely the property preferring a naming is meant
//     to supply — so it is a curation error, never resolved by last-write-wins.
//
// Codegen calls it over the built entity set; the runtime never does (it degrades).
func ValidateSuppression(entities []Entity) error {
	return validateSuppressionWith(entities, loadSuppressionSafe())
}

// validateSuppressionWith is the table-injected core of ValidateSuppression, so the
// guards can be driven over a synthetic seed through the SAME implementation the bake
// runs (dependency injection, not a test-only branch).
func validateSuppressionWith(entities []Entity, tbl *suppressionTable) error {
	if len(tbl.entries) == 0 {
		return nil
	}

	exists := make(map[string]struct{}, len(entities))
	for _, e := range entities {
		exists[e.Ref.String()] = struct{}{}
	}
	for _, entry := range tbl.entries {
		key := entry.Entity.String()
		if _, ok := exists[key]; !ok {
			return fmt.Errorf(
				"bestiary suppression: seed entry names an absent entity %q\n"+
					"  What: a suppression entry applies to an entity that is not in the catalog\n"+
					"  Where: parse/data/suppression_seed.json (entity %q)\n"+
					"  Why: the entry suppresses a modifier on a naming nothing renders, so it is dead curation that will silently rot\n"+
					"  How to fix: correct the entity tuple to a real entity key, or delete the entry",
				key, key,
			)
		}
	}

	owner := make(map[string]string, len(entities))
	for _, e := range entities {
		key := e.Ref.String()
		preferred := preferredNomenValueWith(e.Ref, tbl)
		if prev, clash := owner[preferred]; clash {
			return fmt.Errorf(
				"bestiary suppression: preferred-naming collision on %q\n"+
					"  What: entities %q and %q would both prefer the naming %q\n"+
					"  Where: parse/data/suppression_seed.json\n"+
					"  Why: a preferred naming must denote exactly one entity; a collision makes the preferred spelling ambiguous, defeating the point of preferring it\n"+
					"  How to fix: drop the suppression entry for one of the two entities (the modifier is evidently NOT redundant — it distinguishes them)",
				preferred, prev, key, preferred,
			)
		}
		owner[preferred] = key
	}
	return nil
}
