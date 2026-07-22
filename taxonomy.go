package bestiary

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// ---------------------------------------------------------------------------
// Series / Release — the COMPUTED hierarchy above stable entity keys.
//
// The taxonomy is a read-only VIEW derived from key components that already
// exist: an entity's family plus its identity version make the Series (the
// versioned line: llama-4, gemini-3.0), and its variant names the Release (a
// named member of that line: scout, maverick, flash). Nesting is
// VERSION-ABOVE-VARIANT — "the llama-4 series has the scout and maverick
// releases" — which is why Series carries the generation and Release carries
// the variant name, and not the other way round.
//
// The hierarchy NEVER feeds back into entity keys. Nothing here can rename an
// entity: Series and Release are computed on read from EntityRef components,
// and the curated strays table (parse/data/series.json) only re-homes an entity
// within the hierarchy — it never touches EntityRef.String(). Grouping is a
// relation; identity is a key. Keeping them separate is what lets the taxonomy
// be re-shaped later without a migration.
// ---------------------------------------------------------------------------

// Series is a versioned model line: one family at one generation (llama-4,
// gemini-3.0, claude-4.5). It is COMPUTED from an entity's Family plus its
// identity Version — never stored, never part of any key.
//
// Generation is the identity version verbatim, with one normalization applied:
// a bare-integer generation "N" folds into "N.0" if and only if the SAME family
// also spells "N.0", so one line is never split in two by a spelling difference
// while a family with no dotted sibling (llama-4) keeps its bare generation.
// Generation is EMPTY for an unversioned
// line (e.g. the bare "gemma" entities) — an empty generation is a real Series,
// not a missing one, so it is included in SeriesAll.
type Series struct {
	Family     Family
	Generation string
}

// String renders the Series as "family-generation" (llama-4, gemini-3.0), or as
// the bare family name when the line carries no generation. It is a DISPLAY
// rendering for humans and CLI output; it is deliberately NOT used as a lookup
// key (see seriesKey), because a family name may itself contain a dash and
// therefore the rendering is not injective.
func (s Series) String() string {
	if s.Generation == "" {
		return string(s.Family)
	}
	return string(s.Family) + "-" + s.Generation
}

// Release is a named member of a Series: llama-4's scout and maverick, gemini-3.0's
// flash and pro. It is COMPUTED from the entity's Variant — never stored, never
// part of any key.
//
// Name is EMPTY for the bare line (an entity with no variant, e.g. deepseek@3.2).
// The empty Release is a real Release — the un-named member of the line — not a
// missing one, so it is returned by ReleasesOf alongside the named members.
type Release struct {
	Series Series
	Name   string
}

// String renders the Release as "series/name" (llama-4/scout), or as the bare
// Series rendering when the release is un-named. Like Series.String it is a
// display rendering, not a lookup key.
func (r Release) String() string {
	if r.Name == "" {
		return r.Series.String()
	}
	return r.Series.String() + "/" + r.Name
}

// seriesKey is the internal, collision-free lookup key for a Series. It joins the
// components with NUL rather than reusing Series.String() because the display
// rendering is not injective: a family whose name contains a dash (e.g.
// "gemma-4-31b-larkspur") could otherwise render the same string as a different
// (family, generation) pair. NUL never occurs in a family or generation token.
func seriesKey(s Series) string {
	return string(s.Family) + "\x00" + s.Generation
}

// releaseKey is the internal, collision-free lookup key for a Release: the
// Series key extended with the release name under the same NUL discipline.
func releaseKey(r Release) string {
	return seriesKey(r.Series) + "\x00" + r.Name
}

// ---------------------------------------------------------------------------
// Curated strays (parse/data/series.json)
// ---------------------------------------------------------------------------

// seriesStrayJSON is one curated stray row: an entity family whose Series the
// computation cannot derive from its own key components, mapped onto the Series
// it actually belongs to.
//
// Strays exist because a handful of catalog spellings fold the generation (or a
// codename) INTO the family token itself — "gemma4" is the gemma family at
// generation 4, "gemma-4-31b-larkspur" is a codenamed gemma-4 release — so their
// decomposed Version is empty (or their variant is a point release) and the
// mechanical family+version computation puts them in a line of their own. Curation
// re-homes exactly those; everything else is computed. Curated > mechanical, the
// parse/ override precedent.
//
// Family is the entity family token to re-home (lowercase, matched case-folded).
// Series is the target line. Release, when non-empty, overrides the computed
// release name (the entity's variant) for every entity of that family.
type seriesStrayJSON struct {
	Comment string `json:"_comment,omitempty"`
	Family  Family `json:"family"`
	Series  struct {
		Family     Family `json:"family"`
		Generation string `json:"generation"`
	} `json:"series"`
	Release string `json:"release,omitempty"`
}

// seriesFileJSON is the top-level shape of parse/data/series.json.
type seriesFileJSON struct {
	Comment       string            `json:"_comment"`
	SchemaVersion int               `json:"schema_version"`
	Strays        []seriesStrayJSON `json:"strays"`
}

// seriesStray is one loaded stray mapping: the target Series plus an optional
// release-name override (Release non-empty iff the row supplied one).
type seriesStray struct {
	Series  Series
	Release string
	HasName bool
}

var (
	seriesStrayOnce  sync.Once
	seriesStrayTable map[Family]seriesStray
)

// loadSeriesStrays returns the curated stray table, loading it exactly once
// (sync.Once) from the embedded parse/data/series.json.
//
// It GRACEFULLY DEGRADES, following the lineage.go / modifier_class.go
// precedent: a missing, unreadable, or malformed file yields an EMPTY but
// non-nil map, so the taxonomy falls back to the pure computation rather than
// panicking or nilling out. Individual rows that are unusable (no source family,
// or no target family) are skipped rather than aborting the whole load, so one
// bad curated row cannot cost the rest of the table.
func loadSeriesStrays() map[Family]seriesStray {
	seriesStrayOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/series.json")
		if err != nil {
			seriesStrayTable = map[Family]seriesStray{}
			return
		}
		seriesStrayTable = parseSeriesStrays(raw)
	})
	return seriesStrayTable
}

// parseSeriesStrays is the testable seam behind loadSeriesStrays: it converts the
// curated JSON into the lookup table under the graceful-degrade contract above.
// It NEVER returns nil and never panics — malformed JSON yields an empty table,
// and an unusable row is skipped.
func parseSeriesStrays(raw []byte) map[Family]seriesStray {
	out := map[Family]seriesStray{}
	var file seriesFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return out
	}
	for _, row := range file.Strays {
		from := Family(strings.ToLower(strings.TrimSpace(string(row.Family))))
		to := Family(strings.ToLower(strings.TrimSpace(string(row.Series.Family))))
		if from == "" || to == "" {
			continue // unusable row: skipped, never fatal
		}
		out[from] = seriesStray{
			Series:  Series{Family: to, Generation: strings.TrimSpace(row.Series.Generation)},
			Release: row.Release,
			HasName: row.Release != "",
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Taxonomy index
// ---------------------------------------------------------------------------

// taxonomyIndex is the memoized Series/Release view over the static registry,
// built once (sync.Once) from the entity index. Every slice it holds is in an
// EXPLICITLY sorted order (never map-iteration or first-seen order), so each
// query returns the same sequence on every run and in every process — the INV3
// determinism discipline the generated emissions follow.
//
//   - series is every distinct Series, sorted by (Family, Generation).
//   - releases maps a series key to that line's Releases, sorted by Name.
//   - members maps a release key to the entity keys of its members, sorted.
//   - ofEntity maps an entity key to the Release it computes to (which carries
//     its Series), so the entity->taxonomy direction needs no recomputation.
//   - generations maps a family to its generation-normalization table (raw
//     identity version -> normalized generation); a version absent from the map
//     normalizes to itself.
type taxonomyIndex struct {
	series      []Series
	releases    map[string][]Release
	members     map[string][]string
	ofEntity    map[string]Release
	generations map[Family]map[string]string
}

var (
	taxonomyOnce sync.Once
	taxonomyIdx  *taxonomyIndex
)

// loadTaxonomyIndex returns the memoized taxonomy index, building the entity
// index first if needed. It never returns nil.
func loadTaxonomyIndex() *taxonomyIndex {
	taxonomyOnce.Do(func() { taxonomyIdx = buildTaxonomyIndex(entityIndexAll()) })
	return taxonomyIdx
}

// buildTaxonomyIndex computes the whole taxonomy from a set of entities. It is the
// testable seam behind loadTaxonomyIndex — a caller may build an index over a
// hand-built entity slice to exercise the computation without the static registry.
//
// Two passes are required, and the order matters: the generation normalization can
// only be decided once EVERY generation a family spells is known (a bare "3" folds
// into "3.0" only if that family also spells "3.0"), so pass 1 collects the raw
// generations per family and pass 2 assigns each entity to its normalized Series.
func buildTaxonomyIndex(entities []Entity) *taxonomyIndex {
	strays := loadSeriesStrays()

	// Pass 1 — collect the raw generation spellings per family, skipping families
	// that curation re-homes wholesale (their generation comes from the stray row,
	// so their own version must not pollute the target family's spelling set).
	rawGens := map[Family]map[string]bool{}
	for _, e := range entities {
		fam := e.Ref.Family
		if _, isStray := strays[fam]; isStray {
			continue
		}
		if rawGens[fam] == nil {
			rawGens[fam] = map[string]bool{}
		}
		rawGens[fam][e.Ref.Version] = true
	}
	generations := buildGenerationNormalization(rawGens)

	idx := &taxonomyIndex{
		releases:    map[string][]Release{},
		members:     map[string][]string{},
		ofEntity:    map[string]Release{},
		generations: generations,
	}

	seenSeries := map[string]Series{}
	seenRelease := map[string]bool{}
	for _, e := range entities {
		rel := computeRelease(e.Ref, strays, generations)
		key := e.Ref.String()
		idx.ofEntity[key] = rel

		sk := seriesKey(rel.Series)
		if _, ok := seenSeries[sk]; !ok {
			seenSeries[sk] = rel.Series
		}
		rk := releaseKey(rel)
		if !seenRelease[rk] {
			seenRelease[rk] = true
			idx.releases[sk] = append(idx.releases[sk], rel)
		}
		idx.members[rk] = append(idx.members[rk], key)
	}

	idx.series = make([]Series, 0, len(seenSeries))
	for _, s := range seenSeries {
		idx.series = append(idx.series, s)
	}
	sort.Slice(idx.series, func(i, j int) bool { return lessSeries(idx.series[i], idx.series[j]) })
	for k := range idx.releases {
		rs := idx.releases[k]
		sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	}
	for k := range idx.members {
		sort.Strings(idx.members[k])
	}
	return idx
}

// lessSeries is the total order on Series: ascending Family, then ascending
// Generation. It is the single ordering used by SeriesAll.
func lessSeries(a, b Series) bool {
	if a.Family != b.Family {
		return a.Family < b.Family
	}
	return a.Generation < b.Generation
}

// buildGenerationNormalization decides, per family, which raw identity versions
// map onto a different generation spelling.
//
// The ONE rule: a bare-integer version "N" normalizes to "N.0" when the SAME
// family also spells "N.0". A catalog that ships both gemini@3 and gemini@3.0 is
// spelling one generation two ways, and a line must not be split in two by a
// spelling difference — so both entities land in Series{gemini, "3.0"} (the
// dotted spelling wins, being the more specific one).
//
// The rule is deliberately CONDITIONAL on the sibling's presence rather than
// unconditional: llama spells only "4", so llama-4 stays "llama-4" (an
// unconditional N->N.0 would rename it to llama-4.0 and contradict the ratified
// naming). And it is deliberately data-driven rather than curated, because the
// bare/dotted split is a spelling accident that recurs across families as the
// catalog grows; curating it would mean chasing every new occurrence.
//
// ENTITY KEYS ARE UNTOUCHED: gemini@3 and gemini@3.0 remain two distinct
// entities with their own byte-identical keys. Only their Series agrees.
func buildGenerationNormalization(rawGens map[Family]map[string]bool) map[Family]map[string]string {
	out := map[Family]map[string]string{}
	for fam, versions := range rawGens {
		for v := range versions {
			if !isBareIntegerGeneration(v) {
				continue
			}
			dotted := v + ".0"
			if !versions[dotted] {
				continue
			}
			if out[fam] == nil {
				out[fam] = map[string]string{}
			}
			out[fam][v] = dotted
		}
	}
	return out
}

// isBareIntegerGeneration reports whether v is a non-empty run of ASCII digits
// ("3", "4", "2025"), the only shape eligible for the ".0" fold.
func isBareIntegerGeneration(v string) bool {
	if v == "" {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return true
}

// computeRelease maps one entity ref onto its Release (which carries its Series).
//
// Resolution order (curated beats mechanical, the parse/ override precedent):
//  1. a curated stray row for this family supplies the Series verbatim, and its
//     release name when it names one;
//  2. otherwise the Series is the entity's family at its normalized generation,
//     and the release name is the entity's variant.
func computeRelease(ref EntityRef, strays map[Family]seriesStray, generations map[Family]map[string]string) Release {
	fam := Family(strings.ToLower(string(ref.Family)))
	if stray, ok := strays[fam]; ok {
		name := ref.Variant
		if stray.HasName {
			name = stray.Release
		}
		return Release{Series: stray.Series, Name: name}
	}
	gen := ref.Version
	if norm, ok := generations[ref.Family][gen]; ok {
		gen = norm
	}
	return Release{Series: Series{Family: ref.Family, Generation: gen}, Name: ref.Variant}
}

// SeriesOf returns the Series the given entity ref belongs to: its family at its
// normalized generation, or the curated target line when the family is a curated
// stray. The result is COMPUTED — calling it can never change an entity key.
//
// The ref need not exist in the static registry: an unknown ref still computes to
// the Series it would belong to (a taxonomy is a function of the key components,
// not of registry membership). Generation normalization is family-scoped and is
// therefore taken from the registry's spelling set.
func SeriesOf(ref EntityRef) Series {
	return ReleaseOf(ref).Series
}

// ReleaseOf returns the Release the given entity ref belongs to: the entity's
// variant as a named member of its Series (Name is empty for the bare line), or
// the curated release when the family is a curated stray. Like SeriesOf it is
// computed and never affects identity.
func ReleaseOf(ref EntityRef) Release {
	idx := loadTaxonomyIndex()
	if rel, ok := idx.ofEntity[ref.String()]; ok {
		return rel
	}
	return computeRelease(ref, loadSeriesStrays(), idx.generations)
}
