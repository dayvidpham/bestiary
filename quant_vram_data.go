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
}

// emptyQuantVRAMTable returns the graceful-degrade value: a non-nil table whose
// lookups all miss, so QuantVRAMFor returns nil and the others return zero values
// without ever panicking.
func emptyQuantVRAMTable() *quantVRAMTable {
	return &quantVRAMTable{
		rows:      map[string][]QuantVRAM{},
		paramSize: map[string]string{},
		source:    map[string]DataSourceID{},
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
		quantVRAMTbl, quantVRAMErr = ParseAndValidateQuantVRAMBytes(raw)
	})
	return quantVRAMTbl, quantVRAMErr
}

// loadQuantVRAMTableSafe returns the cached table, or an empty (degraded) table
// when loading failed. It never returns nil and never panics — runtime lookups
// degrade to "no VRAM data" rather than aborting the program. This mirrors the
// loadLineageTableSafe / safeLineageTable pattern exactly.
func loadQuantVRAMTableSafe() *quantVRAMTable {
	tbl, err := loadQuantVRAMTable()
	if err != nil || tbl == nil {
		return emptyQuantVRAMTable()
	}
	return tbl
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

// ParseAndValidateQuantVRAMBytes parses and validates raw quant_vram.json bytes,
// returning a fully built quantVRAMTable or an actionable error. It is the
// testable seam behind ValidateQuantVRAMTable and loadQuantVRAMTable — callers
// can inject synthetic JSON bytes to exercise validation paths without touching
// the embedded file (see TestValidateQuantVRAMTable_RejectsBadInput).
//
// Validation rules:
//   - JSON unmarshal must succeed.
//   - Each model_id must be non-empty and unique (case-insensitive).
//   - param_size, if non-empty, must pass ParseParamSize.
//   - Each row's quant must unmarshal to a known Quantization (not Other) via
//     UnmarshalText — an unknown quant token in curated data is a curation bug.
//   - Each row's weights_bytes must be > 0.
func ParseAndValidateQuantVRAMBytes(raw []byte) (*quantVRAMTable, error) {
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

	tbl := &quantVRAMTable{
		rows:      make(map[string][]QuantVRAM, len(file.Models)),
		paramSize: make(map[string]string, len(file.Models)),
		source:    make(map[string]DataSourceID, len(file.Models)),
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

		// Parse rows.
		qrows := make([]QuantVRAM, 0, len(m.Rows))
		for j, r := range m.Rows {
			if r.WeightsBytes <= 0 {
				return nil, fmt.Errorf(
					"bestiary quant_vram: invalid weights_bytes %d at entry #%d (model_id=%q) row #%d\n"+
						"  What: weights_bytes must be > 0\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].weights_bytes\n"+
						"  Why: weights_bytes is the ground-truth GGUF file size; zero or negative indicates missing or corrupt data\n"+
						"  How to fix: supply the actual GGUF file size in bytes (> 0)",
					r.WeightsBytes, i, mid, j, i, j,
				)
			}

			var q Quantization
			if err := q.UnmarshalText([]byte(r.Quant)); err != nil {
				return nil, fmt.Errorf(
					"bestiary quant_vram: unknown quant token %q at entry #%d (model_id=%q) row #%d: %w\n"+
						"  What: the quant string does not match any known Quantization name\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].quant\n"+
						"  Why: curated quant tokens must be known enum values; unknown tokens indicate a curation error\n"+
						"  How to fix: use a canonical quant name (e.g. \"q4_k_m\", \"q8_0\", \"f16\"); see Quantization constants",
					r.Quant, i, mid, j, err, i, j,
				)
			}
			// UnmarshalText accepts "other" as a valid wire name; but curated data
			// must not use it — that would be a curation gap, not a lossless escape.
			if q == QuantizationOther {
				return nil, fmt.Errorf(
					"bestiary quant_vram: unknown quant token %q (resolved to QuantizationOther) at entry #%d (model_id=%q) row #%d\n"+
						"  What: the quant string resolved to QuantizationOther\n"+
						"  Where: parse/data/quant_vram.json models[%d].rows[%d].quant\n"+
						"  Why: curated rows must use named quant constants, not the Other escape\n"+
						"  How to fix: replace %q with a canonical quant name (e.g. \"q4_k_m\", \"q8_0\", \"f16\")",
					r.Quant, i, mid, j, i, j, r.Quant,
				)
			}

			qrows = append(qrows, QuantVRAM{
				Quant:        q,
				QuantRaw:     r.Quant,
				WeightsBytes: r.WeightsBytes,
				Layers:       r.Layers,
				KVHeads:      r.KVHeads,
				HeadDim:      r.HeadDim,
				// VRAMBytes, VRAMContextTokens, VRAMEstimatePartial are NOT set here.
				// They are computed and baked by the codegen caller (cmd/bestiary-gen)
				// using EstimateVRAMBytes at the model's max context window. The loader
				// provides only the raw ingested inputs; the codegen caller is responsible
				// for the estimation step.
			})
		}

		tbl.rows[mid] = qrows
		if m.ParamSize != "" {
			tbl.paramSize[mid] = strings.ToLower(m.ParamSize)
		}
		if m.Source != "" {
			tbl.source[mid] = DataSourceID(m.Source)
		}
	}

	return tbl, nil
}

// --------------------------------------------------------------------------
// Public API
// --------------------------------------------------------------------------

// QuantVRAMFor returns the curated per-quantization weight-and-arch rows for
// the model identified by id, or nil when no curated rows exist for it. Matching
// is case-insensitive against the model_id keys in parse/data/quant_vram.json.
//
// The returned rows have Quant/QuantRaw/WeightsBytes/Layers/KVHeads/HeadDim
// populated from the file. VRAMBytes/VRAMContextTokens/VRAMEstimatePartial are
// NOT computed here — the codegen caller (cmd/bestiary-gen) computes and bakes
// them using EstimateVRAMBytes at the model's max context window.
//
// On file or parse failure the function degrades gracefully: it returns nil
// without panicking, and ValidateQuantVRAMTable returns the load error so
// codegen can abort on bad curation.
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

// ValidateQuantVRAMTable loads the curated quant-VRAM table and returns any
// load/parse/validation error (nil when the table is well-formed). Codegen calls
// this once and aborts on a non-nil result so bad curation — an unknown quant
// token, zero weights_bytes, a duplicate model_id, a malformed param_size — is
// caught at generation time rather than silently producing wrong VRAM estimates
// at runtime.
func ValidateQuantVRAMTable() error {
	_, err := loadQuantVRAMTable()
	return err
}
