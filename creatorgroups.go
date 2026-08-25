package bestiary

import "sort"

// ---------------------------------------------------------------------------
// Creator > Family > Series > entities — the browsable GROUPING projection.
//
// This is a read-only VIEW over relations that already exist: Entity.Creator
// (the curated Family->Creator seed), EntityRef.Family, and the computed
// Series/Release taxonomy. Like the taxonomy itself it is COMPUTED on read and
// can never rename an entity — grouping is a relation, identity is a key.
//
// It exists because the taxonomy alone is flat: SeriesAll() hands back several
// hundred lines with no organizing level above them, which is a list to scroll,
// not a hierarchy to walk. Putting the creator (who trained the weights) at the
// root and the family beneath it gives the reader the two questions they
// actually arrive with — "whose models?" then "which line?" — before the
// generation-level detail.
//
// THE BASE HOIST. A Series' un-named ("bare") release is not a member of the
// line alongside the named ones; it IS the line, un-named. Rendering it as a
// sibling node called "(base)" invented a level that does not exist and, worse,
// buried the entities of a line that has no named releases at all one click
// deeper than the entities of a line that does. So the bare release is HOISTED:
// its entities hang directly off the Series as SeriesGroup.Hoisted, and only
// genuinely named releases become Releases. No "(base)" node is emitted anywhere.
//
// The hoist is a re-parenting, never a filter: every entity of a series appears
// either in Hoisted or in exactly one ReleaseGroup, so the projection is a
// partition of the corpus. That property is what the reachability-identity test
// pins, and it is the reason a delete-instead-of-hoist regression fails loudly
// instead of quietly shrinking the tree.
// ---------------------------------------------------------------------------

// SeriesShape classifies a Series by which kinds of release it actually has. It
// is the one fact a renderer needs in order to lay a series out: a base-only
// line attaches its entities directly with nothing to disclose, while a mixed
// line must show its hoisted entities ABOVE its named-release disclosures and
// visually distinguish the two (they sit at different levels of the hierarchy
// despite being adjacent on screen).
//
// Following the ModelStatus precedent, the zero value is meaningful: an empty
// SeriesGroup is SeriesShapeNone (no entities at all), never a silent
// "named-only". A shape is DERIVED on demand from the group's contents, so it
// cannot drift out of agreement with them.
type SeriesShape int

const (
	// SeriesShapeNone is the zero value: the series holds no entities. The
	// projection never emits such a group; it is the honest answer for a
	// hand-constructed or zero-value SeriesGroup.
	SeriesShapeNone SeriesShape = iota
	// SeriesShapeBaseOnly is a line whose entities are ALL un-named — there is
	// nothing to disclose, so a renderer attaches the entities directly.
	SeriesShapeBaseOnly
	// SeriesShapeMixed is a line with both hoisted (un-named) entities and named
	// releases. The hoisted entities render above the release disclosures and
	// must be visually distinct from them.
	SeriesShapeMixed
	// SeriesShapeNamedOnly is a line whose entities all belong to named releases;
	// there is nothing to hoist.
	SeriesShapeNamedOnly
)

// String renders the shape as a stable lowercase token, for display and for
// legible test failures.
func (s SeriesShape) String() string {
	switch s {
	case SeriesShapeBaseOnly:
		return "base-only"
	case SeriesShapeMixed:
		return "mixed"
	case SeriesShapeNamedOnly:
		return "named-only"
	default:
		return "none"
	}
}

// CreatorGroup is one lab and every family it created, the root level of the
// browsable tree. Creator is CreatorNone for families with no curated creator
// mapping; that group is emitted LAST (see CreatorGroups) so the attributed labs
// lead and the unattributed remainder collects at the bottom.
type CreatorGroup struct {
	Creator Creator
	// Families are the creator's families, ascending by family token.
	Families []FamilyGroup
	// EntityCount is the total number of entities beneath this creator. It is a
	// derived sum, carried so a renderer need not re-walk the subtree to label a
	// collapsed node.
	EntityCount int
}

// FamilyGroup is one family and every Series (versioned line) within it,
// ascending by generation.
type FamilyGroup struct {
	Family      Family
	Series      []SeriesGroup
	EntityCount int
}

// SeriesGroup is one versioned line with the base hoist applied: Hoisted carries
// the un-named release's entities directly, and Releases carries only genuinely
// named releases. There is no "(base)" release node — see the package comment
// above for why.
type SeriesGroup struct {
	Series Series
	// Hoisted are the entities of the line's un-named release, lifted to sit
	// directly under the Series. Empty for a named-only line. Ascending by
	// canonical entity key.
	Hoisted []Entity
	// Releases are the line's NAMED releases only, ascending by release name.
	Releases []ReleaseGroup
	// EntityCount is len(Hoisted) plus every release's entities.
	EntityCount int
}

// Shape derives how this line should be laid out. It is computed from the
// group's own contents rather than stored, so it can never disagree with them.
func (g SeriesGroup) Shape() SeriesShape {
	hasHoisted := len(g.Hoisted) > 0
	var hasNamed bool
	for _, r := range g.Releases {
		if len(r.Entities) > 0 {
			hasNamed = true
			break
		}
	}
	switch {
	case hasHoisted && hasNamed:
		return SeriesShapeMixed
	case hasHoisted:
		return SeriesShapeBaseOnly
	case hasNamed:
		return SeriesShapeNamedOnly
	default:
		return SeriesShapeNone
	}
}

// ReleaseGroup is one NAMED release and its entities, ascending by canonical key.
// The un-named release never appears here; it is hoisted onto its SeriesGroup.
type ReleaseGroup struct {
	Release  Release
	Entities []Entity
}

// SeriesGroups returns every Series in the static registry with the base hoist
// applied, ascending by family then generation — the flat, creator-agnostic view
// of the same substrate CreatorGroups nests.
//
// Together the groups PARTITION the corpus: every entity in Entities() appears
// in exactly one group, either hoisted or under exactly one named release. Each
// returned Entity is a defensive deep copy, as with Entities().
func SeriesGroups() []SeriesGroup {
	all := SeriesAll()
	out := make([]SeriesGroup, 0, len(all))
	for _, ser := range all {
		if g, ok := seriesGroupOf(ser); ok {
			out = append(out, g)
		}
	}
	return out
}

// seriesGroupOf builds one Series' group, hoisting the un-named release. The bool
// reports whether the line has any entities at all; an empty line is dropped
// rather than emitted as a node with nothing under it.
func seriesGroupOf(ser Series) (SeriesGroup, bool) {
	g := SeriesGroup{Series: ser}
	for _, rel := range ReleasesOf(ser) {
		ents := EntitiesOf(rel)
		if len(ents) == 0 {
			continue
		}
		if rel.Name == "" {
			// THE HOIST: the un-named release is the line itself, so its
			// entities attach directly rather than under a "(base)" node.
			g.Hoisted = append(g.Hoisted, ents...)
			continue
		}
		g.Releases = append(g.Releases, ReleaseGroup{Release: rel, Entities: ents})
	}
	g.EntityCount = len(g.Hoisted)
	for _, r := range g.Releases {
		g.EntityCount += len(r.Entities)
	}
	if g.EntityCount == 0 {
		return SeriesGroup{}, false
	}
	// ReleasesOf and EntitiesOf are already sorted; re-assert both orders here so
	// the projection's contract holds on its own terms rather than by inheritance
	// from a relation it does not own.
	sort.SliceStable(g.Hoisted, func(i, j int) bool {
		return g.Hoisted[i].Ref.String() < g.Hoisted[j].Ref.String()
	})
	sort.SliceStable(g.Releases, func(i, j int) bool {
		return g.Releases[i].Release.Name < g.Releases[j].Release.Name
	})
	return g, true
}

// CreatorGroups returns the full Creator > Family > Series > entities tree with
// the base hoist applied.
//
// The creator set is DERIVED from the corpus (each entity's own Entity.Creator
// projection), never from a hard-coded list: when the curated creators.json seed
// grows, this tree grows with it and needs no edit here. Groups are ascending by
// creator token with ONE deliberate exception — CreatorNone (families with no
// curated creator) sorts LAST, so the attributed labs lead and the unattributed
// remainder collects in a single group at the bottom that a renderer can collapse.
//
// The tree PARTITIONS the corpus exactly as SeriesGroups does: every entity in
// Entities() appears exactly once.
//
// A SERIES, NOT AN ENTITY, CARRIES THE FAMILY HERE. A handful of entities are
// curated STRAYS: series.json re-homes a family whose own token folds the
// generation in (gemma4, gemma-4-31b-larkspur) or marks an experimental spelling
// (gemini-exp) onto the line it actually belongs to, so a Series can hold
// entities whose own Ref.Family differs from Series.Family. The tree groups by
// Series.Family and takes its creator from that family, because honouring the
// re-homing is the entire point of the strays table: gemma4 IS gemma at
// generation 4, and burying it under an unattributed "gemma4" family would undo
// the curation in the one view built to display it.
//
// The consequence is deliberate and worth stating plainly: such an entity appears
// under a creator that its own Entity.Creator projection does not name (Creator
// is a function of the entity's OWN family, which knows nothing of the stray
// mapping). That divergence is confined to strays, and a test pins exactly that —
// any entity whose tree creator differs from its own must be a re-homed stray.
func CreatorGroups() []CreatorGroup {
	// Group the flat, hoisted series view by (creator, family). Reusing
	// SeriesGroups is what keeps the two views from drifting: there is one hoist
	// implementation, not two.
	type famKey struct {
		creator Creator
		family  Family
	}
	byFamily := map[famKey][]SeriesGroup{}
	for _, g := range SeriesGroups() {
		key := famKey{creator: g.Series.Family.Creator(), family: g.Series.Family}
		byFamily[key] = append(byFamily[key], g)
	}

	byCreator := map[Creator][]FamilyGroup{}
	for key, groups := range byFamily {
		fg := FamilyGroup{Family: key.family, Series: groups}
		for _, g := range groups {
			fg.EntityCount += g.EntityCount
		}
		sort.SliceStable(fg.Series, func(i, j int) bool {
			return lessSeries(fg.Series[i].Series, fg.Series[j].Series)
		})
		byCreator[key.creator] = append(byCreator[key.creator], fg)
	}

	out := make([]CreatorGroup, 0, len(byCreator))
	for creator, fams := range byCreator {
		cg := CreatorGroup{Creator: creator, Families: fams}
		for _, f := range fams {
			cg.EntityCount += f.EntityCount
		}
		sort.SliceStable(cg.Families, func(i, j int) bool {
			return cg.Families[i].Family < cg.Families[j].Family
		})
		out = append(out, cg)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return lessCreatorGroup(out[i].Creator, out[j].Creator)
	})
	return out
}

// lessCreatorGroup orders creator groups: ascending by token, except that
// CreatorNone always sorts LAST. Unattributed families are a remainder, not a
// lab whose name happens to be the empty string, so they belong at the bottom of
// the tree rather than at the top where an empty token would otherwise put them.
func lessCreatorGroup(a, b Creator) bool {
	if (a == CreatorNone) != (b == CreatorNone) {
		return b == CreatorNone
	}
	return a < b
}
