package bestiary

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// LineageRecord is the curated lineage of a single model: the set of parent
// derivation edges plus the provenance flags that describe how the record was
// sourced. It is returned by LineageRecordFor.
//
//   - Child is the child model's identity as a node in the derivation DAG (used
//     by Ancestors / Descendants traversal).
//   - Edges holds one LineageEdge per parent. A MERGE carries >= 2 edges.
//   - Real reports whether the child model exists in the models.dev catalog
//     (an attested record). When false the entry is a SYNTHETIC / catalog-absent
//     fixture: the child itself is not in the catalog (only its parents are), and
//     the edge is curated-only. The flag exists so consumers never mistake a
//     synthetic fixture for an attested catalog lineage.
type LineageRecord struct {
	Child EntityRef
	Edges []LineageEdge
	Real  bool
}

// --------------------------------------------------------------------------
// Curated table loading (parse/data/lineage.json) — go:embed + sync.Once,
// mirroring loadParseData / loadHostData determinism.
// --------------------------------------------------------------------------

// lineageParentJSON is one parent derivation edge as curated in lineage.json.
type lineageParentJSON struct {
	Family  Family `json:"family"`
	Variant string `json:"variant"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// lineageRefJSON is the child's entity-ref identity (its DAG node) in lineage.json.
type lineageRefJSON struct {
	Family  Family `json:"family"`
	Variant string `json:"variant"`
	Version string `json:"version"`
}

// lineageEntryJSON is one curated child→parents record in lineage.json.
type lineageEntryJSON struct {
	ChildID  string              `json:"child_id"`
	ChildRef lineageRefJSON      `json:"child_ref"`
	Real     bool                `json:"real"`
	Parents  []lineageParentJSON `json:"parents"`
}

// lineageFileJSON is the top-level shape of parse/data/lineage.json.
type lineageFileJSON struct {
	Comment       string             `json:"_comment"`
	SchemaVersion int                `json:"schema_version"`
	Edges         []lineageEntryJSON `json:"edges"`
}

// lineageTable is the parsed, validated curated lineage DAG.
//
//   - byID maps a lowercase child match key (the full model ID and/or the
//     segment after the last "/") to that child's LineageRecord.
//   - forward maps a child EntityRef key (EntityRef.String()) to its parent
//     edges; it drives transitive Ancestors traversal.
//   - reverse maps a parent EntityRef key to the child refs derived from it; it
//     drives Descendants traversal.
type lineageTable struct {
	byID    map[string]LineageRecord
	forward map[string][]LineageEdge
	reverse map[string][]EntityRef
}

// emptyLineageTable is the degraded (load-failure) value: a non-nil table whose
// lookups all miss, so LineageFor returns nil and traversal returns nil without
// ever panicking.
func emptyLineageTable() *lineageTable {
	return &lineageTable{
		byID:    map[string]LineageRecord{},
		forward: map[string][]LineageEdge{},
		reverse: map[string][]EntityRef{},
	}
}

var (
	lineageOnce sync.Once
	lineageTbl  *lineageTable
	lineageErr  error
)

// loadLineageTable reads and validates parse/data/lineage.json from the embedded
// filesystem exactly once (sync.Once). The cached error is non-nil when the file
// is missing, malformed, or fails parent-family validation; ValidateLineageTable
// surfaces it so codegen can fail loudly on bad curation.
func loadLineageTable() (*lineageTable, error) {
	lineageOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/lineage.json")
		if err != nil {
			lineageErr = fmt.Errorf(
				"bestiary lineage: load lineage.json: %w\n"+
					"  What: cannot read the embedded lineage table\n"+
					"  Where: parse/data/lineage.json\n"+
					"  Why: file missing from the embedded FS (should not happen in a production build)\n"+
					"  How to fix: ensure parse/data/lineage.json is present before building",
				err,
			)
			return
		}
		lineageTbl, lineageErr = parseLineageTable(raw)
	})
	return lineageTbl, lineageErr
}

// loadLineageTableSafe returns the cached table, or an empty (degraded) table
// when loading failed. It never returns nil and never panics — runtime lineage
// lookups degrade to "no lineage" rather than aborting the program.
func loadLineageTableSafe() *lineageTable {
	t, err := loadLineageTable()
	if err != nil || t == nil {
		return emptyLineageTable()
	}
	return t
}

// parseLineageTable parses and validates the curated lineage JSON. It is the
// testable seam behind loadLineageTable: load-time parent validation rejects any
// edge whose parent.family is not a known base family (see Family.IsKnown), and
// any entry with a missing child key, no parents, or an unrecognized derivation
// kind, with an actionable error. On success it returns a fully built table.
func parseLineageTable(raw []byte) (*lineageTable, error) {
	var file lineageFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf(
			"bestiary lineage: parse lineage.json: %w\n"+
				"  What: JSON unmarshal failed\n"+
				"  Where: parse/data/lineage.json\n"+
				"  How to fix: validate the JSON syntax in the data file",
			err,
		)
	}

	tbl := emptyLineageTable()
	for i, e := range file.Edges {
		childID := strings.ToLower(strings.TrimSpace(e.ChildID))
		if childID == "" {
			return nil, fmt.Errorf(
				"bestiary lineage: invalid entry #%d: empty child_id\n"+
					"  What: a lineage entry has no child match key\n"+
					"  Where: parse/data/lineage.json edges[%d]\n"+
					"  Why: child_id keys the entry to a catalog model ID\n"+
					"  How to fix: set a non-empty child_id (lowercase model ID or its post-'/' segment)",
				i, i,
			)
		}
		if len(e.Parents) == 0 {
			return nil, fmt.Errorf(
				"bestiary lineage: invalid entry #%d (child_id=%q): no parents\n"+
					"  What: a derivation edge needs at least one parent\n"+
					"  Where: parse/data/lineage.json edges[%d].parents\n"+
					"  How to fix: list >= 1 parent (>= 2 for a merge)",
				i, childID, i,
			)
		}

		edges := make([]LineageEdge, 0, len(e.Parents))
		for j, p := range e.Parents {
			if !p.Family.IsKnown() {
				return nil, fmt.Errorf(
					"bestiary lineage: invalid parent in entry #%d (child_id=%q): unknown base family %q\n"+
						"  What: a lineage parent names a family that is not a known base family\n"+
						"  Where: parse/data/lineage.json edges[%d].parents[%d].family\n"+
						"  Why: every derivation parent must resolve to a known base family (Family.IsKnown); raw_family is a weak hint and is not trusted here\n"+
						"  How to fix: correct the family, or register it as a base family in family.go (curatedBaseFamilies)",
					i, childID, p.Family, i, j,
				)
			}
			var kind DerivationKind
			if err := kind.UnmarshalText([]byte(p.Kind)); err != nil {
				return nil, fmt.Errorf(
					"bestiary lineage: invalid parent in entry #%d (child_id=%q): %w\n"+
						"  Where: parse/data/lineage.json edges[%d].parents[%d].kind\n"+
						"  How to fix: use a known derivation kind",
					i, childID, err, i, j,
				)
			}
			edges = append(edges, LineageEdge{
				Parent: EntityRef{Family: p.Family, Variant: p.Variant, Version: p.Version},
				Kind:   kind,
			})
		}

		childRef := EntityRef{Family: e.ChildRef.Family, Variant: e.ChildRef.Variant, Version: e.ChildRef.Version}
		rec := LineageRecord{Child: childRef, Edges: edges, Real: e.Real}
		tbl.byID[childID] = rec

		// Forward index: child node -> its parent edges (transitive ancestors).
		childKey := childRef.String()
		tbl.forward[childKey] = append(tbl.forward[childKey], edges...)
		// Reverse index: each parent node -> this child (descendants).
		for _, ed := range edges {
			pk := ed.Parent.String()
			tbl.reverse[pk] = append(tbl.reverse[pk], childRef)
		}
	}
	return tbl, nil
}

// lookup resolves a model ID to its curated LineageRecord, matching the lowercase
// full ID first, then the segment after the last "/". The returned Edges slice is
// a defensive copy so callers cannot mutate the cached table.
func (t *lineageTable) lookup(id ModelID) (LineageRecord, bool) {
	s := strings.ToLower(string(id))
	rec, ok := t.byID[s]
	if !ok {
		if i := strings.LastIndexByte(s, '/'); i >= 0 {
			rec, ok = t.byID[s[i+1:]]
		}
	}
	if !ok {
		return LineageRecord{}, false
	}
	rec.Edges = append([]LineageEdge(nil), rec.Edges...)
	return rec, true
}

// --------------------------------------------------------------------------
// Public API
// --------------------------------------------------------------------------

// ValidateLineageTable loads the curated lineage table and returns any
// load/parse/validation error (nil when the table is well-formed). Codegen calls
// this once and aborts on a non-nil result so bad curation — an unknown parent
// family, a malformed entry — is caught at generation time rather than silently
// dropping lineage at runtime.
func ValidateLineageTable() error {
	_, err := loadLineageTable()
	return err
}

// LineageRecordFor returns the curated LineageRecord for the model identified by
// id, and whether one exists. Matching is by the curated child key (the lowercase
// model ID or its post-'/' segment), so it resolves both bare and namespaced ID
// forms — and SYNTHETIC / catalog-absent children too (their Real field is false).
func LineageRecordFor(id ModelID) (LineageRecord, bool) {
	return loadLineageTableSafe().lookup(id)
}

// LineageFor returns the curated parent derivation edges for the model identified
// by id, or nil when no lineage is curated for it. It is the population entry
// point used by codegen to bake ModelInfo.Lineage, and a convenience over
// LineageRecordFor when the provenance flags are not needed.
func LineageFor(id ModelID) []LineageEdge {
	rec, ok := LineageRecordFor(id)
	if !ok {
		return nil
	}
	return rec.Edges
}

// --------------------------------------------------------------------------
// Cycle-safe DAG traversal
// --------------------------------------------------------------------------

// lineageAncestors returns the transitive set of parent EntityRefs reachable from
// seed, following fwd (child key -> parent edges) for deeper hops. A visited set
// keyed by EntityRef.String() makes it cycle-safe: each ancestor is emitted once
// and a cyclic table terminates instead of looping forever. Order is the
// depth-first discovery order.
func lineageAncestors(seed []LineageEdge, fwd map[string][]LineageEdge) []EntityRef {
	var out []EntityRef
	visited := map[string]bool{}
	var walk func(edges []LineageEdge)
	walk = func(edges []LineageEdge) {
		for _, e := range edges {
			k := e.Parent.String()
			if visited[k] {
				continue
			}
			visited[k] = true
			out = append(out, e.Parent)
			walk(fwd[k])
		}
	}
	walk(seed)
	return out
}

// lineageDescendants returns the transitive set of child EntityRefs derived from
// rootKey, following rev (parent key -> child refs). Like lineageAncestors it is
// cycle-safe via a visited set keyed by EntityRef.String().
func lineageDescendants(rootKey string, rev map[string][]EntityRef) []EntityRef {
	var out []EntityRef
	visited := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		for _, child := range rev[key] {
			ck := child.String()
			if visited[ck] {
				continue
			}
			visited[ck] = true
			out = append(out, child)
			walk(ck)
		}
	}
	walk(rootKey)
	return out
}

// Ancestors returns the transitive set of parent EntityRefs reachable from this
// entity's lineage, via a cycle-safe depth-first traversal of the curated
// derivation DAG. The first hop comes from the entity's own Lineage edges (as
// populated at codegen); deeper hops follow the curated forward index. When the
// entity carries no edges directly, the curated index keyed by its Ref is used as
// the seed so a hand-constructed Entity still resolves. A cycle never loops.
func (e Entity) Ancestors() []EntityRef {
	t := loadLineageTableSafe()
	seed := e.Lineage
	if len(seed) == 0 {
		seed = t.forward[e.Ref.String()]
	}
	return lineageAncestors(seed, t.forward)
}

// Descendants returns the transitive set of child EntityRefs derived (directly or
// indirectly) from this entity, via a cycle-safe depth-first traversal of the
// curated derivation DAG's reverse edges.
func (e Entity) Descendants() []EntityRef {
	t := loadLineageTableSafe()
	return lineageDescendants(e.Ref.String(), t.reverse)
}
