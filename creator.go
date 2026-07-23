package bestiary

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Creator identifies the lab / organization that TRAINED or created a model's
// weights — the SPDX "originator". It is DISTINCT from Provider (the SPDX
// "supplier"/distributor): a model created by one lab is frequently hosted by
// many providers, and a single provider hosts models from many creators. Meta
// creates Llama (Provider is often "local", "together", "fireworks", …); Anthropic
// both creates AND hosts Claude, so its Creator and one of its Providers coincide,
// but the two axes remain conceptually separate.
//
// Like Provider, Creator is an OPEN string type: there are many labs and the set
// grows as the huggingface ingest surfaces new originators, so a closed int enum
// cannot scale. Well-known constants give type safety at call sites while the type
// stays extensible. CreatorNone (the empty string, the zero value) is the honest
// value for an unmapped family — bestiary emits nothing rather than a
// wrong-but-plausible guess.
type Creator string

// CreatorNone is the zero-value Creator: no creator is known for the family. It is
// the honest "unmapped" sentinel — Family.Creator returns it when the curated seed
// carries no row for the family, and it renders as the empty string.
const CreatorNone Creator = ""

// Well-known Creator constants (the initial curated seed set, mirroring the Provider
// constants in providers_gen.go). Values are lowercase machine tokens matching the
// Provider token convention ("anthropic", "openai", …); a Creator token and the
// same lab's Provider token deliberately coincide where a lab both creates and
// distributes, but the two are separate axes and must not be conflated. The set is
// extended by the huggingface ingest (HF-org-derived creators) in a later slice.
const (
	CreatorMeta      Creator = "meta"
	CreatorOpenAI    Creator = "openai"
	CreatorAnthropic Creator = "anthropic"
	CreatorGoogle    Creator = "google"
	CreatorMistral   Creator = "mistral"
	CreatorCohere    Creator = "cohere"
	CreatorDeepSeek  Creator = "deepseek"
	CreatorAlibaba   Creator = "alibaba"
	CreatorZhipu     Creator = "zhipu"
)

// knownCreators is the well-known Creator set consulted by IsKnown and returned by
// Creators. It is the hand-curated seed of recognized originators; an unmapped-but-
// present creator token (surfaced by a future ingest) is still a valid Creator value
// — IsKnown is a recognition check, not a validity gate, exactly as Provider.IsKnown.
var knownCreators = [...]Creator{
	CreatorMeta,
	CreatorOpenAI,
	CreatorAnthropic,
	CreatorGoogle,
	CreatorMistral,
	CreatorCohere,
	CreatorDeepSeek,
	CreatorAlibaba,
	CreatorZhipu,
}

// IsKnown reports whether c is one of the well-known Creator constants.
// The set is the hand-curated seed above; like Provider.IsKnown it is a recognition
// check (use it to validate a call-site literal), not a gate on the open type.
func (c Creator) IsKnown() bool {
	for _, known := range knownCreators {
		if c == known {
			return true
		}
	}
	return false
}

// Creators returns all well-known Creator values as a defensive copy.
// Modifying the returned slice does not affect package state.
func Creators() []Creator {
	out := make([]Creator, len(knownCreators))
	copy(out, knownCreators[:])
	return out
}

// String returns the string representation of the creator.
func (c Creator) String() string {
	return string(c)
}

// MarshalText implements encoding.TextMarshaler.
// It is permissive: any Creator value (known or unknown) marshals to its token.
func (c Creator) MarshalText() ([]byte, error) {
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts any string value; use IsKnown() to validate against the well-known set.
func (c *Creator) UnmarshalText(b []byte) error {
	if c == nil {
		return fmt.Errorf("bestiary: Creator.UnmarshalText: nil receiver")
	}
	*c = Creator(b)
	return nil
}

// --------------------------------------------------------------------------
// Curated creator table loading (parse/data/creators.json) — go:embed +
// sync.Once, mirroring loadDataSourceTable / loadLineageTable determinism.
// The mapping is DATA-DRIVEN (a curated JSON seed), never an in-code switch, so
// the same table feeds codegen (baked ModelInfo.Creator), the runtime projection
// (Entity.Creator), and the persisted store creators dimension by construction.
// --------------------------------------------------------------------------

// creatorJSON is one Family→Creator mapping row as curated in creators.json.
type creatorJSON struct {
	Family  Family  `json:"family"`
	Creator Creator `json:"creator"`
}

// creatorFileJSON is the top-level shape of parse/data/creators.json:
// {_comment, schema_version, creators:[{family, creator}]}.
type creatorFileJSON struct {
	Comment       string        `json:"_comment"`
	SchemaVersion int           `json:"schema_version"`
	Creators      []creatorJSON `json:"creators"`
}

// creatorTable is the parsed, validated Family→Creator dimension.
//
//   - byFamily maps a Family to its Creator for O(1) resolution.
//
// The mapping obeys Family → Creator: a Family has exactly one Creator (BCNF), so
// storing Creator per-model or per-entity would be a transitive dependency. This
// table is the single in-memory source of truth for the mapping.
type creatorTable struct {
	byFamily map[Family]Creator
}

// emptyCreatorTable is the degraded (load-failure) value: a non-nil table whose
// lookups all miss, so Family.Creator returns CreatorNone without ever panicking.
func emptyCreatorTable() *creatorTable {
	return &creatorTable{byFamily: map[Family]Creator{}}
}

var (
	creatorOnce sync.Once
	creatorTbl  *creatorTable
	creatorErr  error
)

// loadCreatorTable reads and validates parse/data/creators.json from the embedded
// filesystem exactly once (sync.Once). The cached error is non-nil when the file is
// missing, malformed, or fails table validation (an unknown family, a duplicate
// family, or an empty creator); ValidateCreatorTable surfaces it so codegen fails
// loudly on bad curation.
func loadCreatorTable() (*creatorTable, error) {
	creatorOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/creators.json")
		if err != nil {
			creatorErr = fmt.Errorf(
				"bestiary creator: load creators.json: %w\n"+
					"  What: cannot read the embedded creator table\n"+
					"  Where: parse/data/creators.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/creators.json is present before building",
				err,
			)
			return
		}
		creatorTbl, creatorErr = parseCreatorTable(raw)
	})
	return creatorTbl, creatorErr
}

// loadCreatorTableSafe returns the cached table, or an empty (degraded) table when
// loading failed. It never returns nil and never panics — a runtime creator lookup
// degrades to CreatorNone rather than aborting the program (the lineage.go / datasource.go
// graceful-degrade precedent).
func loadCreatorTableSafe() *creatorTable {
	return safeCreatorTable(loadCreatorTable())
}

// safeCreatorTable is the testable degrade seam behind loadCreatorTableSafe: it
// returns t when loading succeeded, or a non-nil EMPTY table when err is non-nil or
// t is nil. It is the runtime-degrade twin of the codegen ValidateCreatorTable
// hard-fail — at runtime a bad/missing table yields "no creator", never a panic.
func safeCreatorTable(t *creatorTable, err error) *creatorTable {
	if err != nil || t == nil {
		return emptyCreatorTable()
	}
	return t
}

// parseCreatorTable parses and validates the curated creator JSON. It is the
// testable seam behind loadCreatorTable: it rejects a row whose family is not a
// recognized Family (Family.IsKnown), a duplicate family (Family → Creator must be a
// function), or an empty creator — each with an actionable error. On success it
// returns a fully built table.
func parseCreatorTable(raw []byte) (*creatorTable, error) {
	var file creatorFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary creator: parse creators.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/creators.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := emptyCreatorTable()
	for i, row := range file.Creators {
		if !row.Family.IsKnown() {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creators.json row #%d: unknown family %q\n"+
					"  What: a creator mapping names a family that is not a recognized Family\n"+
					"  Where: parse/data/creators.json creators[%d].family\n"+
					"  Why: every mapped family must satisfy Family.IsKnown so the dimension stays FK-consistent with the family set\n"+
					"  How to fix: correct the family token, or register it in the generated family set / curatedBaseFamilies",
				i, row.Family, i,
			)
		}
		if row.Creator == CreatorNone {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creators.json row #%d (family %q): empty creator\n"+
					"  What: a creator mapping has no creator value\n"+
					"  Where: parse/data/creators.json creators[%d].creator\n"+
					"  Why: an unmapped family is expressed by OMITTING its row (Family.Creator returns CreatorNone), never by a row with an empty creator\n"+
					"  How to fix: set a non-empty creator, or delete the row",
				i, row.Family, i,
			)
		}
		if _, dup := tbl.byFamily[row.Family]; dup {
			return nil, fmt.Errorf(
				"bestiary creator: duplicate creators.json family %q (row #%d)\n"+
					"  What: the same family appears in more than one creator mapping\n"+
					"  Where: parse/data/creators.json creators[].family\n"+
					"  Why: Family → Creator is a function (BCNF); a family has exactly one creator\n"+
					"  How to fix: remove the duplicate row",
				row.Family, i,
			)
		}
		tbl.byFamily[row.Family] = row.Creator
	}
	return tbl, nil
}

// ValidateCreatorTable is the LOUD codegen guard for the curated creator dimension:
// it loads and validates parse/data/creators.json and returns a non-nil error when
// the file is missing, malformed, or carries an unknown family, a duplicate family,
// or an empty creator. cmd/bestiary-gen calls it before baking so a bad seed fails
// codegen with an actionable message rather than silently degrading. (The runtime
// path uses loadCreatorTableSafe, which degrades to CreatorNone instead.)
func ValidateCreatorTable() error {
	_, err := loadCreatorTable()
	return err
}

// Creator resolves a Family to the lab / organization that creates that family's
// models (the SPDX originator), via the curated seed parse/data/creators.json. It is
// DATA-DRIVEN (never an in-code switch): the same seed feeds codegen's baked
// ModelInfo.Creator emission, the registry's Entity.Creator projection, and the
// store's creators dimension, so all three agree by construction.
//
// It returns CreatorNone for a family with no curated mapping (community models,
// multi-org models, or a family the seed has not yet covered) — an honest empty,
// never a wrong-but-plausible guess. At runtime a missing/corrupt seed degrades
// gracefully to CreatorNone (loadCreatorTableSafe); codegen catches such a seed
// loudly via ValidateCreatorTable.
//
// Creator is a SEPARATE axis from Family.CanonicalProvider (which answers a
// Provider-typed resolution-preference question). Both coexist; neither replaces the
// other.
func (f Family) Creator() Creator {
	return loadCreatorTableSafe().byFamily[f]
}
