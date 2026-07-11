package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// quantVRAMTable is the parsed curated table of per-model per-quant weights
// and architecture facts loaded from parse/data/quant_vram.json.
type quantVRAMTable struct {
	// rows maps a lowercase model_id to its QuantVRAM rows.
	rows map[string][]QuantVRAM
	// paramSize maps a lowercase model_id to its canonical param-size token.
	paramSize map[string]string
	// source maps a lowercase model_id to its DataSourceID.
	source map[string]DataSourceID
	// contextWindow maps a lowercase model_id to the curated context window in
	// tokens. 0 means no context_window was curated for this entry. Codegen uses
	// this as the model-max override when computing VRAMBytes.
	contextWindow map[string]int
	// baseRef maps a lowercase model_id to its curated base model reference string
	// (the base_ref field in the JSON). Empty means no base_ref was curated. Codegen
	// uses this to infer a DerivationFinetune lineage edge for community finetunes.
	baseRef map[string]string
}

// emptyQuantVRAMTable returns the graceful-degrade value: a non-nil table whose
// lookups all miss, so QuantVRAMFor returns nil and the others return zero
// values without ever panicking.
func emptyQuantVRAMTable() *quantVRAMTable {
	return &quantVRAMTable{
		rows:          map[string][]QuantVRAM{},
		paramSize:     map[string]string{},
		source:        map[string]DataSourceID{},
		contextWindow: map[string]int{},
		baseRef:       map[string]string{},
	}
}

var (
	quantVRAMOnce sync.Once
	quantVRAMTbl  *quantVRAMTable
	quantVRAMErr  error
)

// loadQuantVRAMTable reads and validates parse/data/quant_vram.json from the
// embedded filesystem exactly once (sync.Once). On any load or parse error the
// cached error is non-nil; ValidateQuantVRAMTable surfaces it so codegen can
// abort on bad curation. The runtime lookups degrade to empty-table (no data)
// rather than panicking.
func loadQuantVRAMTable() (*quantVRAMTable, error) {
	quantVRAMOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/quant_vram.json")
		if err != nil {
			quantVRAMErr = fmt.Errorf(
				"bestiary quant_vram: load quant_vram.json: %w\n"+
					"  What: cannot read the embedded quant-VRAM table\n"+
					"  Where: parse/data/quant_vram.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/quant_vram.json is present before building",
				err,
			)
			return
		}
		quantVRAMTbl, quantVRAMErr = parseQuantVRAMTable(raw)
	})
	return quantVRAMTbl, quantVRAMErr
}

// safeQuantVRAMTable is the testable degrade seam behind loadQuantVRAMTableSafe:
// it returns t when loading succeeded, or a non-nil EMPTY table when err is
// non-nil or t is nil. It is the runtime-degrade twin of the codegen
// ValidateQuantVRAMTable hard-fail — at runtime a bad or missing table yields
// "no VRAM data", never a panic. Mirrors safeLineageTable exactly.
func safeQuantVRAMTable(t *quantVRAMTable, err error) *quantVRAMTable {
	if err != nil || t == nil {
		return emptyQuantVRAMTable()
	}
	return t
}

// loadQuantVRAMTableSafe returns the cached table, or an empty (degraded) table
// when loading failed. It never returns nil and never panics — runtime lookups
// degrade to "no VRAM data" rather than aborting the program.
func loadQuantVRAMTableSafe() *quantVRAMTable {
	return safeQuantVRAMTable(loadQuantVRAMTable())
}

// --------------------------------------------------------------------------
// JSON wire types for parse/data/quant_vram.json
// --------------------------------------------------------------------------

// quantVRAMRowJSON is one row within a model's "rows" array.
type quantVRAMRowJSON struct {
	Quant        string `json:"quant"`
	WeightsBytes int64  `json:"weights_bytes"`
	Layers       int    `json:"layers,omitempty"`
	KVHeads      int    `json:"kv_heads,omitempty"`
	HeadDim      int    `json:"head_dim,omitempty"`
}

// quantVRAMModelJSON is one model entry in the "models" array.
type quantVRAMModelJSON struct {
	Comment       string             `json:"_comment,omitempty"`
	ModelID       string             `json:"model_id"`
	ParamSize     string             `json:"param_size,omitempty"`
	Source        string             `json:"source"`
	BaseRef       string             `json:"base_ref,omitempty"`
	ContextWindow int                `json:"context_window,omitempty"`
	Rows          []quantVRAMRowJSON `json:"rows"`
}

// quantVRAMFileJSON is the top-level shape of parse/data/quant_vram.json.
type quantVRAMFileJSON struct {
	Comment       string               `json:"_comment,omitempty"`
	SchemaVersion int                  `json:"schema_version"`
	Models        []quantVRAMModelJSON `json:"models"`
}

// knownQuantVRAMSchemaVersions is the set of schema_version values this code
// understands. Bump when the JSON shape changes incompatibly. Only versions
// listed here are accepted; any other value yields an actionable error from
// parseQuantVRAMTable.
var knownQuantVRAMSchemaVersions = map[int]bool{
	1: true,
}

// parseQuantVRAMTable parses and validates raw quant_vram.json bytes, returning
// a fully built quantVRAMTable or an actionable error. It is the testable seam
// behind loadQuantVRAMTable and ValidateQuantVRAMTable — callers can inject
// synthetic JSON bytes to exercise validation paths without touching the
// embedded file (see quant_vram_internal_test.go).
//
// Validation rules:
//   - JSON unmarshal must succeed.
//   - schema_version must be a known version (currently: 1).
//   - Each model_id must be non-empty and unique (case-insensitive).
//   - param_size, if non-empty, must pass ParseParamSize.
//   - source must be a known, non-empty DataSourceID (DataSourceModelsDev or
//     DataSourceOllama); DataSourceNone (empty string) is rejected.
//   - Each row's quant must parse to a known Quantization constant that is not
//     QuantizationOther via UnmarshalText — an unknown quant token in curated
//     data is a curation bug. Matching is case-insensitive (the loader
//     normalises Ollama's mixed-case file_type values, e.g. "Q4_K_M").
//   - Each row's weights_bytes must be > 0.
//   - Each row's layers, kv_heads, head_dim must be >= 0.
func parseQuantVRAMTable(raw []byte) (*quantVRAMTable, error) {
	var file quantVRAMFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary quant_vram: unmarshal quant_vram.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/quant_vram.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	if !knownQuantVRAMSchemaVersions[file.SchemaVersion] {
		return nil, fmt.Errorf(
			"bestiary quant_vram: unsupported schema_version %d in quant_vram.json\n"+
				"  What: the file declares a schema_version this code does not understand\n"+
				"  Where: parse/data/quant_vram.json schema_version\n"+
				"  Why: only schema versions %v are supported by this build\n"+
				"  How to fix: either update the code to handle the new version, or"+
				" restore schema_version to a supported value",
			file.SchemaVersion, supportedVersionList(knownQuantVRAMSchemaVersions),
		)
	}

	tbl := &quantVRAMTable{
		rows:          make(map[string][]QuantVRAM, len(file.Models)),
		paramSize:     make(map[string]string, len(file.Models)),
		source:        make(map[string]DataSourceID, len(file.Models)),
		contextWindow: make(map[string]int, len(file.Models)),
		baseRef:       make(map[string]string, len(file.Models)),
	}

	for i, m := range file.Models {
		mid := strings.ToLower(strings.TrimSpace(m.ModelID))
		if mid == "" {
			return nil, fmt.Errorf(
				"bestiary quant_vram: invalid entry #%d: empty model_id\n"+
					"  What: a quant_vram entry has no model ID\n"+
					"  Where: parse/data/quant_vram.json models[%d].model_id\n"+
					"  Why: model_id keys the entry to a catalog model\n"+
					"  How to fix: set a non-empty model_id",
				i, i,
			)
		}
		if _, dup := tbl.rows[mid]; dup {
			return nil, fmt.Errorf(
				"bestiary quant_vram: duplicate model_id %q at entry #%d\n"+
					"  What: two models share the same model_id (case-insensitive)\n"+
					"  Where: parse/data/quant_vram.json models[%d].model_id\n"+
					"  Why: model_id must be unique within the table\n"+
					"  How to fix: remove or merge the duplicate entry",
				mid, i, i,
			)
		}

		// Validate param_size if present.
		if m.ParamSize != "" {
			if _, err := ParseParamSize(m.ParamSize); err != nil {
				return nil, fmt.Errorf(
					"bestiary quant_vram: invalid param_size %q at entry #%d (model_id=%q)\n"+
						"  What: param_size does not pass ParseParamSize validation\n"+
						"  Where: parse/data/quant_vram.json models[%d].param_size\n"+
						"  How to fix: use a valid param-size token (e.g. \"70b\", \"3b\", \"0.5b\"): %w",
					m.ParamSize, i, mid, i, err,
				)
			}
		}

		// Validate source: must be a known, non-empty DataSourceID.
		if !isKnownDataSourceID(DataSourceID(m.Source)) {
			return nil, fmt.Errorf(
				"bestiary quant_vram: unknown source %q at entry #%d (model_id=%q)\n"+
					"  What: the source field does not name a known data source\n"+
					"  Where: parse/data/quant_vram.json models[%d].source\n"+
					"  Why: curated entries must declare a known data source so provenance"+
					" can be tracked; the empty string and unknown IDs are not allowed\n"+
					"  How to fix: set source to one of the known values: %q, %q",
				m.Source, i, mid, i, string(DataSourceModelsDev), string(DataSourceOllama),
			)
		}

		// Parse rows.
		qrows := make([]QuantVRAM, 0, len(m.Rows))
		for j, r := range m.Rows {
			if r.WeightsBytes <= 0 {
				return nil, fmt.Errorf(
					"bestiary quant_vram: invalid weights_bytes %d at entry #%d (model_id=%q) row #%d\n"+
						"  What: weights_bytes must be > 0\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].weights_bytes\n"+
						"  Why: weights_bytes is the ground-truth GGUF file size; zero or negative"+
						" indicates missing or corrupt data\n"+
						"  How to fix: supply the actual GGUF file size in bytes (> 0)",
					r.WeightsBytes, i, mid, j, i, j,
				)
			}

			if r.Layers < 0 || r.KVHeads < 0 || r.HeadDim < 0 {
				return nil, fmt.Errorf(
					"bestiary quant_vram: negative arch fact at entry #%d (model_id=%q) row #%d:"+
						" layers=%d kv_heads=%d head_dim=%d\n"+
						"  What: layers, kv_heads, and head_dim must be >= 0 (0 means unknown)\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d]\n"+
						"  Why: negative values are physically nonsensical and indicate a curation error\n"+
						"  How to fix: set the value to 0 (unknown) or a positive integer",
					i, mid, j, r.Layers, r.KVHeads, r.HeadDim, i, j,
				)
			}

			var q Quantization
			if err := q.UnmarshalText([]byte(r.Quant)); err != nil {
				return nil, fmt.Errorf(
					"bestiary quant_vram: unknown quant token %q at entry #%d (model_id=%q) row #%d: %w\n"+
						"  What: the quant string does not match any known Quantization name\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].quant\n"+
						"  Why: curated quant tokens must be known enum values; unknown tokens indicate"+
						" a curation error\n"+
						"  How to fix: use a canonical quant name (e.g. \"q4_k_m\", \"q8_0\", \"f16\");"+
						" see Quantization constants",
					r.Quant, i, mid, j, err, i, j,
				)
			}
			// UnmarshalText accepts "other" as a valid wire name, but curated data
			// must not use it — that would be a curation gap, not a lossless escape.
			if q == QuantizationOther {
				return nil, fmt.Errorf(
					"bestiary quant_vram: unknown quant token %q (resolved to QuantizationOther)"+
						" at entry #%d (model_id=%q) row #%d\n"+
						"  What: the quant string resolved to QuantizationOther\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].quant\n"+
						"  Why: curated rows must use named quant constants, not the Other escape\n"+
						"  How to fix: replace %q with a canonical quant name"+
						" (e.g. \"q4_k_m\", \"q8_0\", \"f16\")",
					r.Quant, i, mid, j, i, j, r.Quant,
				)
			}

			qrows = append(qrows, QuantVRAM{
				Quant: q,
				// QuantRaw is always the verbatim curated token from the JSON file,
				// preserving the original casing (e.g. "q4_k_m" exactly as written).
				// It is populated for every row, not only for QuantizationOther.
				// Consumers needing display-safe or round-trip-safe text use QuantRaw;
				// consumers needing a canonical enum use Quant.
				QuantRaw:     r.Quant,
				WeightsBytes: r.WeightsBytes,
				Layers:       r.Layers,
				KVHeads:      r.KVHeads,
				HeadDim:      r.HeadDim,
				// VRAMBytes, VRAMContextTokens, VRAMEstimatePartial are NOT set here.
				// They are computed and baked by the codegen caller (cmd/bestiary-gen)
				// using EstimateVRAMBytes at the model's max context window. The loader
				// provides only the raw ingested inputs; the codegen caller is responsible
				// for the estimation step. Callers must verify these fields are zero
				// when reading rows directly from QuantVRAMFor.
			})
		}

		tbl.rows[mid] = qrows
		if m.ParamSize != "" {
			tbl.paramSize[mid] = strings.ToLower(m.ParamSize)
		}
		if m.Source != "" {
			tbl.source[mid] = DataSourceID(m.Source)
		}
		if m.ContextWindow < 0 {
			return nil, fmt.Errorf(
				"bestiary quant_vram: negative context_window %d at entry #%d (model_id=%q)\n"+
					"  What: context_window must be >= 0 (0 means not curated)\n"+
					"  Where: parse/data/quant_vram.json models[%d].context_window\n"+
					"  Why: a negative context window is physically nonsensical and indicates"+
					" a curation error\n"+
					"  How to fix: set context_window to 0 (omit it) or a positive integer"+
					" representing the model's maximum context length in tokens",
				m.ContextWindow, i, mid, i,
			)
		}
		if m.ContextWindow > 0 {
			tbl.contextWindow[mid] = m.ContextWindow
		}
		if m.BaseRef != "" {
			tbl.baseRef[mid] = m.BaseRef
		}
	}

	return tbl, nil
}

// isKnownDataSourceID reports whether id is one of the well-known, non-empty
// DataSourceID constants. DataSourceNone (empty string) is explicitly rejected
// here because curated entries must always declare a provenance source.
func isKnownDataSourceID(id DataSourceID) bool {
	switch id {
	case DataSourceModelsDev, DataSourceOllama:
		return true
	default:
		return false
	}
}

// supportedVersionList formats the keys of a version-support map as a sorted
// slice for inclusion in error messages.
func supportedVersionList(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	// Sort small slice inline to avoid importing sort for a one-liner.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// --------------------------------------------------------------------------
// Public API
// --------------------------------------------------------------------------

// QuantVRAMFor returns the curated per-quantization weight-and-arch rows for
// the model identified by id, or nil when no curated rows exist for it.
// Matching is case-insensitive against the model_id keys in
// parse/data/quant_vram.json.
//
// The returned rows have Quant/QuantRaw/WeightsBytes/Layers/KVHeads/HeadDim
// populated from the file. VRAMBytes, VRAMContextTokens, and VRAMEstimatePartial
// are always zero — they are computed and baked by the codegen caller
// (cmd/bestiary-gen) using EstimateVRAMBytes at the model's max context window.
// Callers must not treat a zero VRAMBytes as meaning "no VRAM data"; it means
// "not yet computed by codegen". Use WeightsBytes as the weights footprint.
//
// On file or parse failure the function degrades gracefully: it returns nil
// without panicking. ValidateQuantVRAMTable returns the load error so codegen
// can abort on bad curation.
func QuantVRAMFor(id ModelID) []QuantVRAM {
	tbl := loadQuantVRAMTableSafe()
	rows := tbl.rows[strings.ToLower(string(id))]
	if len(rows) == 0 {
		return nil
	}
	// Defensive copy so callers cannot mutate the cached table.
	out := make([]QuantVRAM, len(rows))
	copy(out, rows)
	return out
}

// ParamSizeFor returns the canonical parameter-size token (e.g. "70b", "3b",
// "0.5b") for the model identified by id, or the empty string when no
// param_size is curated for it. Matching is case-insensitive.
//
// The returned value is the lowercased canonical form (validated by
// ParseParamSize at load time) suitable for EntityRef.ParamSize.
func ParamSizeFor(id ModelID) string {
	tbl := loadQuantVRAMTableSafe()
	return tbl.paramSize[strings.ToLower(string(id))]
}

// SourceFor returns the DataSourceID for the model identified by id, or
// DataSourceNone (the empty string) when the model is not in the curated table.
// Matching is case-insensitive. For curated Ollama data the value is
// DataSourceOllama.
func SourceFor(id ModelID) DataSourceID {
	tbl := loadQuantVRAMTableSafe()
	src, ok := tbl.source[strings.ToLower(string(id))]
	if !ok {
		return DataSourceNone
	}
	return src
}

// ContextWindowFor returns the curated maximum context window in tokens for the
// model identified by id, or 0 when no context_window is curated for it.
// Matching is case-insensitive.
//
// Codegen uses this as the model-max override when computing VRAMBytes: a
// non-zero value from this function takes precedence over ModelInfo.ContextWindow
// from the models.dev catalog.
func ContextWindowFor(id ModelID) int {
	tbl := loadQuantVRAMTableSafe()
	return tbl.contextWindow[strings.ToLower(string(id))]
}

// BaseRefFor returns the curated base model reference string for the model
// identified by id, or the empty string when no base_ref is curated for it.
// Matching is case-insensitive.
//
// Codegen uses this to infer a DerivationFinetune lineage edge for community
// finetunes whose base model is known from Ollama metadata. An empty result
// means the model is not a curated finetune (or its base is not known).
func BaseRefFor(id ModelID) string {
	tbl := loadQuantVRAMTableSafe()
	return tbl.baseRef[strings.ToLower(string(id))]
}

// ValidateQuantVRAMTable loads the curated quant-VRAM table and returns any
// load/parse/validation error (nil when the table is well-formed). Codegen
// calls this once and aborts on a non-nil result so bad curation — an unknown
// quant token, zero weights_bytes, a duplicate model_id, a malformed
// param_size, an unknown source — is caught at generation time rather than
// silently producing wrong VRAM estimates at runtime.
func ValidateQuantVRAMTable() error {
	_, err := loadQuantVRAMTable()
	return err
}
