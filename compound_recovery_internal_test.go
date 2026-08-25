package bestiary

import "testing"

// compoundRecoveryInput is one row of the compound-family recovery corpus. It carries
// a raw API family ALWAYS, and OPTIONALLY the (Provider, ID) of a real catalog row.
// The two kinds share a corpus because they pin one rule at its two levels: the
// unit rows pin which compound families the recovery accepts and which it declines,
// and the catalog rows pin the entity a real registry row consequently resolves to.
// The runner dispatches on whether ID is set.
type compoundRecoveryInput struct {
	Raw      string `json:"raw"`
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// compoundRecoveryExpected is the decomposition the raw family must produce. EntityKey
// is set only on catalog rows (an empty EntityKey means "unit row, no registry probe").
type compoundRecoveryExpected struct {
	Family    string `json:"family"`
	Variant   string `json:"variant"`
	Version   string `json:"version"`
	EntityKey string `json:"entity_key"`
}

// TestCompoundSeriesFamilyRecovery_Corpus pins the general recovery of a COMPOUND
// series family token on the raw-populated parse path.
//
// The defect it guards: the version-pattern table matches only a DOTTED series number
// ("kimi-k2.7" -> kimi + "k2.7"), so a BARE-INTEGER series compound ("kimi-k2",
// "kimi-k3") fell through to passthrough and kept the compound as its family. Models
// tagged that way were stranded on compound-family keys of their own, split off from
// the short-family siblings carrying the same series. The recovery reuses the SAME
// closed predicate the empty-raw inference already used, so it needs no per-spelling
// curated row: a new series number recovers automatically.
//
// The negative controls are the point of the corpus. They pin that the recovery
// DECLINES a curated genuine compound (a family_overrides.json self-map), a base with
// no curated series letter, a wrong series letter, a series letter with no number, and
// any compound carrying an extra unrecognised token — and that an under-specified raw
// family is never used to ASSERT a version the model ID does not state.
func TestCompoundSeriesFamilyRecovery_Corpus(t *testing.T) {
	corpus := loadInternalCorpus[compoundRecoveryInput, compoundRecoveryExpected](
		t, internalCompoundRecoveryCorpusJSON, 14)

	// Value-based coverage: catches a count-preserving swap the exact-count control
	// cannot see. Keyed by raw family + id so a unit row and a catalog row on the same
	// raw family stay distinguishable.
	internalRequireInputCoverage(t, corpus, map[compoundRecoveryInput]compoundRecoveryExpected{
		{Raw: "kimi-k3", ID: "k3", Provider: "kimi-for-coding"}: {
			Family: "kimi", Variant: "k", Version: "3", EntityKey: "kimi/k@3"},
		{Raw: "kimi-k2", ID: "umans-coder", Provider: "umans-ai"}: {
			Family: "kimi", Variant: "coder", Version: "", EntityKey: "kimi/coder"},
		{Raw: "kimi-k2", ID: "moonshotai/Kimi-K2.5-TEE", Provider: "chutes"}: {
			Family: "kimi", Variant: "", Version: "", EntityKey: "kimi"},
		{Raw: "kimi-k3"}:          {Family: "kimi"},
		{Raw: "kimi-k2"}:          {Family: "kimi"},
		{Raw: "text-embedding"}:   {Family: "text-embedding"},
		{Raw: "stable-diffusion"}: {Family: "stable-diffusion"},
		{Raw: "kimi-v2"}:          {Family: "kimi-v2"},
		{Raw: "kimi-k"}:           {Family: "kimi-k"},
		{Raw: "kimi-k2-nitro"}:    {Family: "kimi-k2-nitro"},
	})

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Input.ID == "" {
				// Unit row: the raw family alone drives the decomposition.
				fam, variant, version, _, _ := ParseFamilyDetailed(Family(c.Input.Raw), "", "")
				if string(fam) != c.Expected.Family || variant != c.Expected.Variant || version != c.Expected.Version {
					t.Errorf("ParseFamilyDetailed(%q, \"\", \"\") = (%q, %q, %q), want (%q, %q, %q)",
						c.Input.Raw, fam, variant, version,
						c.Expected.Family, c.Expected.Variant, c.Expected.Version)
				}
				return
			}

			// Catalog row: probe the real registry row this corpus pins.
			m, ok := LookupModelByProvider(Provider(c.Input.Provider), c.Input.ID)
			if !ok {
				t.Fatalf("LookupModelByProvider(%q, %q) = false; the pinned catalog row is gone",
					c.Input.Provider, c.Input.ID)
			}
			if string(m.RawFamily) != c.Input.Raw {
				t.Fatalf("catalog row %q (%s) reports raw family %q, but the corpus pins the recovery for raw %q",
					c.Input.ID, c.Input.Provider, m.RawFamily, c.Input.Raw)
			}
			if string(m.Family) != c.Expected.Family || m.Variant != c.Expected.Variant || m.Version != c.Expected.Version {
				t.Errorf("%q (%s) decomposes to (%q, %q, %q), want (%q, %q, %q)",
					c.Input.ID, c.Input.Provider, m.Family, m.Variant, m.Version,
					c.Expected.Family, c.Expected.Variant, c.Expected.Version)
			}
			ref := EntityRef{
				Family:    m.Family,
				Variant:   m.Variant,
				Version:   m.Version,
				ParamSize: m.ParamSize,
				Modifier:  EntityModifiers(m.Modifier, m.Family),
			}
			if got := ref.String(); got != c.Expected.EntityKey {
				t.Errorf("%q (%s) resolves to entity %q, want %q",
					c.Input.ID, c.Input.Provider, got, c.Expected.EntityKey)
			}
			if _, ok := EntityByKey(c.Expected.EntityKey); !ok {
				t.Errorf("no entity is filed under %q, so the corpus row would pass vacuously",
					c.Expected.EntityKey)
			}
		})
	}
}

// TestCuratedGenuineCompound_DeclinesSeriesShapedSelfMap makes the self-map guard
// FALSIFIABLE. No family_overrides.json row currently self-maps a family that also has
// the "<base>-<letter><number>" series shape, so removing the guard would leave the
// corpus above green. This test scratch-mutates the loaded override table to add such a
// row and asserts the predicate reports it — so a future curated self-map of a
// series-shaped compound (kimi-k9, minimax-m4) is protected before it is ever written.
func TestCuratedGenuineCompound_DeclinesSeriesShapedSelfMap(t *testing.T) {
	pd, err := loadParseData()
	if err != nil {
		t.Fatalf("loadParseData: %v", err)
	}

	const compound = Family("kimi-k9")

	// Precondition: the series predicate WOULD accept this shape, so a decline can only
	// come from the self-map guard.
	if base, ok := seriesBaseFamily(pd, compound); !ok || base != "kimi" {
		t.Fatalf("seriesBaseFamily(%q) = (%q, %v); the falsifier needs a shape the series predicate accepts",
			compound, base, ok)
	}
	if isCuratedGenuineCompound(pd, compound) {
		t.Fatalf("%q is already curated as a genuine compound; pick a shape that is not", compound)
	}

	scratch := &parseData{overrides: map[Family]familyOverride{compound: {Family: compound}}}
	if !isCuratedGenuineCompound(scratch, compound) {
		t.Errorf("isCuratedGenuineCompound(self-mapped %q) = false, want true — the guard that keeps "+
			"a curated genuine compound out of the automatic series recovery is not load-bearing", compound)
	}

	// A REDUCING override (not a self-map) is not a genuine compound: it names a
	// decomposition, so the guard must not fire on it.
	reducing := &parseData{overrides: map[Family]familyOverride{compound: {Family: "kimi", Variant: "k9"}}}
	if isCuratedGenuineCompound(reducing, compound) {
		t.Errorf("isCuratedGenuineCompound(reducing override %q) = true, want false — only a SELF-map "+
			"marks a curated genuine compound", compound)
	}
}
