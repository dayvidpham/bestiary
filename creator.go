package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
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

// Well-known Creator constants, mirroring the Provider constants in providers_gen.go.
// Values are the lab's own short machine token, following the same lowercase
// convention as Provider ("anthropic", "openai", …).
//
// A Creator token and the same lab's Provider token OFTEN coincide when a lab both
// creates and distributes (e.g. "anthropic"/"anthropic"), but they are NOT
// guaranteed to: CreatorZhipu ("zhipu") diverges from ProviderZhipuai ("zhipuai") —
// the lab's short name and its provider-hosting token are independently curated and
// may drift apart. The two are separate axes and must not be conflated. Nor is every
// creator a provider: CreatorDeepReinforce, CreatorBlackForestLabs and CreatorUpstage
// name labs that operate no hosting surface of their own in this catalog at all.
//
// The set has three provenance groups, marked below: the original hand-curated seed
// labs, the tokens derived from the models.dev metadata LAB PREFIX, and the tokens
// hand-curated for families the metadata join never reaches. The set is extended
// further by the huggingface ingest (HF-org-derived creators) in a later slice.
const (
	// Seed labs curated by hand before the lab-prefix derivation existed. Their
	// rows in creators.json WIN over any lab-derived value (curated-over-derived),
	// which is why CreatorZhipu keeps the lab's short name "zhipu" even though the
	// models.dev lab prefix for the same organization spells itself "zhipuai".
	CreatorMeta      Creator = "meta"
	CreatorOpenAI    Creator = "openai"
	CreatorAnthropic Creator = "anthropic"
	CreatorGoogle    Creator = "google"
	CreatorMistral   Creator = "mistral"
	CreatorCohere    Creator = "cohere"
	CreatorDeepSeek  Creator = "deepseek"
	CreatorAlibaba   Creator = "alibaba"
	CreatorZhipu     Creator = "zhipu"

	// Tokens introduced by the models.dev LAB-PREFIX derivation: the leading
	// "<lab>/" segment of a metadata id is the originator the upstream catalog
	// itself asserts, so a family reached by exactly one lab seeds that lab as its
	// Creator. Every token here is a lab prefix verbatim — it is NOT re-spelled to
	// match a Provider slug, because Creator and Provider are independent axes
	// (CreatorMoonshotAI and ProviderMoonshotAI coincide; CreatorDeepReinforce has
	// no provider counterpart at all).
	CreatorDeepReinforce Creator = "deepreinforce"
	CreatorMeituan       Creator = "meituan"
	CreatorMicrosoft     Creator = "microsoft"
	CreatorMiniMax       Creator = "minimax"
	CreatorMoonshotAI    Creator = "moonshotai"
	CreatorNvidia        Creator = "nvidia"
	CreatorPerplexity    Creator = "perplexity"
	CreatorPoolside      Creator = "poolside"
	CreatorSakana        Creator = "sakana"
	CreatorSarvam        Creator = "sarvam"
	CreatorStepFun       Creator = "stepfun"
	CreatorTencent       Creator = "tencent"
	CreatorXAI           Creator = "xai"
	CreatorXiaomi        Creator = "xiaomi"

	// Tokens introduced by HAND-CURATED rows for families the models.dev metadata
	// join does not reach (a family with catalog entities but no models.json row
	// has no lab prefix to derive from). These are the originators of the largest
	// otherwise-unattributed families; each one is priced at the full five-part
	// cost documented on Creators.
	Creator01AI            Creator = "01ai"
	CreatorAI21            Creator = "ai21"
	CreatorAmazon          Creator = "amazon"
	CreatorBAAI            Creator = "baai"
	CreatorBaichuan        Creator = "baichuan"
	CreatorBaidu           Creator = "baidu"
	CreatorBlackForestLabs Creator = "blackforestlabs"
	CreatorByteDance       Creator = "bytedance"
	CreatorElevenLabs      Creator = "elevenlabs"
	CreatorIBM             Creator = "ibm"
	CreatorIdeogram        Creator = "ideogram"
	CreatorNousResearch    Creator = "nousresearch"
	CreatorRecraft         Creator = "recraft"
	CreatorReka            Creator = "reka"
	CreatorRunway          Creator = "runway"
	CreatorStabilityAI     Creator = "stabilityai"
	CreatorUpstage         Creator = "upstage"
	CreatorVoyageAI        Creator = "voyageai"
)

// knownCreators is the well-known Creator set consulted by IsKnown and returned by
// Creators. It is the hand-curated seed of recognized originators; an unmapped-but-
// present creator token (surfaced by a future ingest) is still a valid Creator value
// — IsKnown is a recognition check, not a validity gate, exactly as Provider.IsKnown.
var knownCreators = [...]Creator{
	// Hand-curated seed labs.
	CreatorMeta,
	CreatorOpenAI,
	CreatorAnthropic,
	CreatorGoogle,
	CreatorMistral,
	CreatorCohere,
	CreatorDeepSeek,
	CreatorAlibaba,
	CreatorZhipu,
	// Lab-prefix-derived tokens.
	CreatorDeepReinforce,
	CreatorMeituan,
	CreatorMicrosoft,
	CreatorMiniMax,
	CreatorMoonshotAI,
	CreatorNvidia,
	CreatorPerplexity,
	CreatorPoolside,
	CreatorSakana,
	CreatorSarvam,
	CreatorStepFun,
	CreatorTencent,
	CreatorXAI,
	CreatorXiaomi,
	// Hand-curated tokens for families the metadata join does not reach.
	Creator01AI,
	CreatorAI21,
	CreatorAmazon,
	CreatorBAAI,
	CreatorBaichuan,
	CreatorBaidu,
	CreatorBlackForestLabs,
	CreatorByteDance,
	CreatorElevenLabs,
	CreatorIBM,
	CreatorIdeogram,
	CreatorNousResearch,
	CreatorRecraft,
	CreatorReka,
	CreatorRunway,
	CreatorStabilityAI,
	CreatorUpstage,
	CreatorVoyageAI,
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
//
// Adding a NEW creator token costs FIVE authoring parts, all of which must move
// together or the dimension silently degrades:
//
//  1. the parse/data/creators.json row (or rows) that reference the token — without
//     one the constant is dead;
//  2. the Creator constant in the block above;
//  3. the knownCreators entry — omit it and Creator.IsKnown reports false for a token
//     the curated table does happily emit, breaking the creators.json↔IsKnown
//     consistency guard;
//  4. the Creators() length pin in the exported tests, re-derived from the emitting
//     run rather than copied forward;
//  5. the creatorExpr case in cmd/bestiary-gen — omit it and codegen silently emits
//     the untyped fallback Creator("token") instead of the constant name.
//
// Mapping an ADDITIONAL family onto a token that already exists costs only part 1.
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

// creatorWithheldJSON is one deliberately-NOT-applied family as curated in the
// creators.json "withheld" array: a family the models.dev lab derivation reaches but
// whose lab must not be applied, with the reason a curator can act on.
type creatorWithheldJSON struct {
	Family Family `json:"family"`
	Reason string `json:"reason"`
}

// creatorFileJSON is the top-level shape of parse/data/creators.json:
// {_comment, schema_version, withheld:[{family, reason}], creators:[{family, creator}]}.
type creatorFileJSON struct {
	Comment       string                `json:"_comment"`
	SchemaVersion int                   `json:"schema_version"`
	Withheld      []creatorWithheldJSON `json:"withheld"`
	Creators      []creatorJSON         `json:"creators"`
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
	// withheld maps a family to the curated reason its lab-derived creator must NOT
	// be applied. A withheld family is DISTINCT from an unmapped one: the unmapped
	// family is simply absent everywhere, while a withheld family is a known,
	// explained deferral that the codegen disagreement report re-surfaces on every
	// regen so it cannot decay into an unexplained gap.
	withheld map[Family]string
}

// withheldReason returns the curated reason family f is withheld from lab-derived
// application, or "" when it is not withheld. It is nil-safe on the map so a
// degraded (empty) table answers "not withheld" rather than panicking.
func (t *creatorTable) withheldReason(f Family) string {
	if t == nil {
		return ""
	}
	return t.withheld[f]
}

// emptyCreatorTable is the degraded (load-failure) value: a non-nil table whose
// lookups all miss, so Family.Creator returns CreatorNone without ever panicking.
func emptyCreatorTable() *creatorTable {
	return &creatorTable{
		byFamily: map[Family]Creator{},
		withheld: map[Family]string{},
	}
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

	for i, row := range file.Withheld {
		if !row.Family.IsKnown() {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creators.json withheld row #%d: unknown family %q\n"+
					"  What: a withholding names a family that is not a recognized Family\n"+
					"  Where: parse/data/creators.json withheld[%d].family\n"+
					"  Why: a withholding is a statement ABOUT a family, so the same FK gate applies as to a mapping row\n"+
					"  How to fix: correct the family token, or register it in the generated family set / curatedBaseFamilies",
				i, row.Family, i,
			)
		}
		if strings.TrimSpace(row.Reason) == "" {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creators.json withheld row #%d (family %q): empty reason\n"+
					"  What: a withholding gives no reason\n"+
					"  Where: parse/data/creators.json withheld[%d].reason\n"+
					"  Why: an unexplained withholding is indistinguishable from an oversight the next curator will silently undo\n"+
					"  How to fix: state why the lab-derived creator must not be applied, and what would resolve it",
				i, row.Family, i,
			)
		}
		if _, mapped := tbl.byFamily[row.Family]; mapped {
			return nil, fmt.Errorf(
				"bestiary creator: creators.json family %q is both mapped and withheld (withheld row #%d)\n"+
					"  What: the same family carries a creator mapping AND a withholding\n"+
					"  Where: parse/data/creators.json creators[] and withheld[]\n"+
					"  Why: withholding means the creator is deliberately NOT recorded yet; a mapping says it is\n"+
					"  How to fix: delete whichever of the two no longer holds",
				row.Family, i,
			)
		}
		if _, dup := tbl.withheld[row.Family]; dup {
			return nil, fmt.Errorf(
				"bestiary creator: duplicate creators.json withheld family %q (row #%d)\n"+
					"  What: the same family is withheld more than once\n"+
					"  Where: parse/data/creators.json withheld[].family\n"+
					"  Why: two reasons for one withholding means one of them is stale\n"+
					"  How to fix: merge them into a single row",
				row.Family, i,
			)
		}
		tbl.withheld[row.Family] = row.Reason
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

// --------------------------------------------------------------------------
// Curated creator→provider distribution relation (parse/data/creator_providers.json)
// — the same go:embed + sync.Once discipline as the Family→Creator table above.
// --------------------------------------------------------------------------

// creatorProviderJSON is one Creator→[]Provider distribution row as curated in
// creator_providers.json.
type creatorProviderJSON struct {
	Creator   Creator    `json:"creator"`
	Providers []Provider `json:"providers"`
}

// creatorProviderFileJSON is the top-level shape of parse/data/creator_providers.json:
// {_comment, schema_version, creator_providers:[{creator, providers}]}.
type creatorProviderFileJSON struct {
	Comment          string                `json:"_comment"`
	SchemaVersion    int                   `json:"schema_version"`
	CreatorProviders []creatorProviderJSON `json:"creator_providers"`
}

// creatorProviderTable is the parsed, validated Creator→[]Provider relation.
//
//   - byCreator maps a Creator to its curated distribution providers, each slice
//     stored in CURATION ORDER — the order the JSON file lists them in, which is
//     the lab's own primacy order (its primary API first, regional and plan-scoped
//     variants after). That order is load-bearing: resolution picks the earliest
//     listed surface present among a model's hosts, so re-sorting here would replace
//     a curation decision with an alphabetical accident (Zhipu's international "zai"
//     brand would outrank its own "zhipuai" API). The file is fixed, so the order is
//     deterministic without a sort.
//
// Unlike Family→Creator this is a genuine many-to-many relation, so the value is a
// slice, not a scalar.
type creatorProviderTable struct {
	byCreator map[Creator][]Provider
}

// emptyCreatorProviderTable is the degraded (load-failure) value: a non-nil table
// whose lookups all miss, so Creator.Providers returns an empty (non-nil) slice
// without ever panicking.
func emptyCreatorProviderTable() *creatorProviderTable {
	return &creatorProviderTable{byCreator: map[Creator][]Provider{}}
}

var (
	creatorProviderOnce sync.Once
	creatorProviderTbl  *creatorProviderTable
	creatorProviderErr  error
)

// loadCreatorProviderTable reads and validates parse/data/creator_providers.json from
// the embedded filesystem exactly once (sync.Once). The cached error is non-nil when
// the file is missing, malformed, or fails table validation (an unknown creator, an
// unknown provider, a duplicate creator, a duplicate provider within a row, or an
// empty provider list); ValidateCreatorProviderTable surfaces it so codegen fails
// loudly on bad curation.
func loadCreatorProviderTable() (*creatorProviderTable, error) {
	creatorProviderOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/creator_providers.json")
		if err != nil {
			creatorProviderErr = fmt.Errorf(
				"bestiary creator: load creator_providers.json: %w\n"+
					"  What: cannot read the embedded creator→provider distribution table\n"+
					"  Where: parse/data/creator_providers.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/creator_providers.json is present before building",
				err,
			)
			return
		}
		creatorProviderTbl, creatorProviderErr = parseCreatorProviderTable(raw)
	})
	return creatorProviderTbl, creatorProviderErr
}

// loadCreatorProviderTableSafe returns the cached table, or an empty (degraded) table
// when loading failed. It never returns nil and never panics — a runtime distribution
// lookup degrades to "no curated providers" rather than aborting the program, which
// makes creator-first selection fall back to the CanonicalProvider preference instead
// of failing the resolution.
func loadCreatorProviderTableSafe() *creatorProviderTable {
	return safeCreatorProviderTable(loadCreatorProviderTable())
}

// safeCreatorProviderTable is the testable degrade seam behind
// loadCreatorProviderTableSafe: it returns t when loading succeeded, or a non-nil
// EMPTY table when err is non-nil or t is nil.
func safeCreatorProviderTable(t *creatorProviderTable, err error) *creatorProviderTable {
	if err != nil || t == nil {
		return emptyCreatorProviderTable()
	}
	return t
}

// parseCreatorProviderTable parses and validates the curated distribution JSON. It is
// the testable seam behind loadCreatorProviderTable and rejects, each with an
// actionable error: a row whose creator is not a well-known Creator, a duplicate
// creator row, an empty provider list, a provider that is not a recognized Provider
// (Provider.IsKnown — the LOUD codegen guard), and a provider repeated
// within one row. On success it returns a table whose provider slices are sorted.
func parseCreatorProviderTable(raw []byte) (*creatorProviderTable, error) {
	var file creatorProviderFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary creator: parse creator_providers.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/creator_providers.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := emptyCreatorProviderTable()
	for i, row := range file.CreatorProviders {
		if !row.Creator.IsKnown() {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creator_providers.json row #%d: unknown creator %q\n"+
					"  What: a distribution row names a creator that is not a well-known Creator\n"+
					"  Where: parse/data/creator_providers.json creator_providers[%d].creator\n"+
					"  Why: the relation is an FK onto the Creator set; an unrecognized creator here can never be reached by Family.Creator, so the row is dead curation\n"+
					"  How to fix: correct the creator token, or add the Creator constant and its knownCreators entry",
				i, row.Creator, i,
			)
		}
		if _, dup := tbl.byCreator[row.Creator]; dup {
			return nil, fmt.Errorf(
				"bestiary creator: duplicate creator_providers.json creator %q (row #%d)\n"+
					"  What: the same creator appears in more than one distribution row\n"+
					"  Where: parse/data/creator_providers.json creator_providers[].creator\n"+
					"  Why: a creator's distribution surfaces are one set; two rows would make Creator.Providers order-dependent\n"+
					"  How to fix: merge the two rows' providers into a single row",
				row.Creator, i,
			)
		}
		if len(row.Providers) == 0 {
			return nil, fmt.Errorf(
				"bestiary creator: invalid creator_providers.json row #%d (creator %q): empty provider list\n"+
					"  What: a distribution row lists no providers\n"+
					"  Where: parse/data/creator_providers.json creator_providers[%d].providers\n"+
					"  Why: a creator with no first-party hosting surface is expressed by OMITTING its row (Creator.Providers returns an empty slice), never by an empty list\n"+
					"  How to fix: list at least one provider, or delete the row",
				i, row.Creator, i,
			)
		}
		seen := make(map[Provider]struct{}, len(row.Providers))
		provs := make([]Provider, 0, len(row.Providers))
		for j, p := range row.Providers {
			if !p.IsKnown() {
				return nil, fmt.Errorf(
					"bestiary creator: invalid creator_providers.json row #%d (creator %q): unknown provider %q\n"+
						"  What: a distribution row names a provider that is not a recognized Provider\n"+
						"  Where: parse/data/creator_providers.json creator_providers[%d].providers[%d]\n"+
						"  Why: the relation is an FK onto the Provider set; an unrecognized slug can never match a served instance, so creator-first selection would silently never fire for it\n"+
						"  How to fix: correct the provider slug to one of Providers(), or drop it from the row",
					i, row.Creator, p, i, j,
				)
			}
			if _, dup := seen[p]; dup {
				return nil, fmt.Errorf(
					"bestiary creator: duplicate creator_providers.json provider %q for creator %q (row #%d)\n"+
						"  What: the same provider is listed twice within one distribution row\n"+
						"  Where: parse/data/creator_providers.json creator_providers[%d].providers\n"+
						"  Why: a row is a SET of surfaces; a repeat is a curation slip that inflates the emitted report\n"+
						"  How to fix: remove the duplicate entry",
					p, row.Creator, i, i,
				)
			}
			seen[p] = struct{}{}
			provs = append(provs, p)
		}
		tbl.byCreator[row.Creator] = provs
	}
	return tbl, nil
}

// ValidateCreatorProviderTable is the LOUD codegen guard for the curated
// Creator→[]Provider distribution relation: it loads and validates
// parse/data/creator_providers.json and returns a non-nil error when the file is
// missing, malformed, or carries an unknown creator, a duplicate creator, an empty
// provider list, an unknown provider, or a provider repeated within a row.
// cmd/bestiary-gen calls it before baking so bad curation fails codegen with an
// actionable message rather than silently degrading. (The runtime path uses
// loadCreatorProviderTableSafe, which degrades to an empty relation instead.)
func ValidateCreatorProviderTable() error {
	_, err := loadCreatorProviderTable()
	return err
}

// Providers returns the curated hosting surfaces this creator operates or brands for
// its OWN models — the Creator axis's counterpart to Family.CanonicalProvider — as a
// defensive copy in CURATION ORDER. Modifying the returned slice does not affect
// package state.
//
// The order is the lab's PRIMACY order, not an alphabetical one: the first entry is
// the surface a caller should prefer when several are available, and resolution
// relies on exactly that (Zhipu leads with its own "zhipuai" API ahead of the
// international "zai" brand). Callers that need a stable display order should sort a
// copy rather than assume one.
//
// It returns an EMPTY (non-nil) slice for a creator with no first-party surface in
// this catalog, which is the honest answer for a weights-only lab whose models reach
// users solely through third-party hosts. The result is deliberately NOT the set of
// every provider that rehosts the lab's weights: resolution ranks a curated
// distribution provider ABOVE a rehost, so folding rehosts in would erase the
// distinction the relation exists to draw.
//
// At runtime a missing or corrupt relation degrades to the empty slice
// (loadCreatorProviderTableSafe), which makes creator-first selection a no-op and
// leaves the CanonicalProvider preference in charge; codegen catches such a table
// loudly via ValidateCreatorProviderTable.
func (c Creator) Providers() []Provider {
	provs := loadCreatorProviderTableSafe().byCreator[c]
	out := make([]Provider, len(provs))
	copy(out, provs)
	return out
}
