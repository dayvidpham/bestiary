package bestiary

import (
	"bytes"
	"strings"
	"testing"
)

// This file carries ONE assertion per creator-first selection site. The five sites
// share a single preference authority (providerPreferenceScore), so the value of
// testing them individually is not the ranking logic — it is that each site actually
// CONSULTS it. A site that silently kept its old canonical-only rule would still pass
// a test of the authority alone.
//
// Sites (in the order they appear in the code):
//  1. resolve.go — PURL loose-fallback per-ID representative upgrade
//  2. resolve.go — single-group canonical-form provider narrowing
//  3. resolve.go — collectRehostProviders ("Also rehosted by") exclusion
//  4. resolve.go — selectRepresentative group tie-break
//  5. format.go  — FormatAmbiguous both-axes listing
//
// Every site keeps Family.CanonicalProvider as the layer BENEATH the creator axis, so
// each test also pins the fall-through: a family with no creator, no curated
// distribution row, or no creator-hosted candidate behaves exactly as before.

// creatorFirstFixture is the family/provider vocabulary the site tests share.
//
//   - "llama" is the family that separates the two axes: its curated canonical
//     provider is "local" (which serves nothing in the catalog) while "llama" and
//     "meta" are Meta's curated distribution surfaces. It is the family the
//     creator-first preference is about.
//   - "flux" has a creator (blackforestlabs) but NO curated distribution row and no
//     canonical provider — the "creator known, nothing to prefer" fall-through.
//   - "definitely-not-a-family" has neither axis — the untouched-baseline case.
const (
	famCreatorAndCanonicalSplit = Family("llama")
	famCreatorNoSurfaces        = Family("flux")
	famNeitherAxis              = Family("definitely-not-a-family")
	provMetaCreatorSurface      = Provider("llama")
	provLlamaCanonical          = Provider("local")
	provRehostA                 = Provider("deepinfra")
	provRehostB                 = Provider("azure")
)

// creatorFirstPreconditions fails fast if the curated data no longer supports the
// vocabulary above, so a curation change turns these tests into a loud failure rather
// than a set of quietly vacuous assertions.
func creatorFirstPreconditions(t *testing.T) {
	t.Helper()
	if !isCreatorProvider(famCreatorAndCanonicalSplit, provMetaCreatorSurface) {
		t.Fatalf("precondition: %q is no longer a curated distribution surface for %q's creator",
			provMetaCreatorSurface, famCreatorAndCanonicalSplit)
	}
	if got := famCreatorAndCanonicalSplit.CanonicalProvider(); got != provLlamaCanonical {
		t.Fatalf("precondition: Family(%q).CanonicalProvider() = %q, want %q",
			famCreatorAndCanonicalSplit, got, provLlamaCanonical)
	}
	if provLlamaCanonical == provMetaCreatorSurface {
		t.Fatal("precondition: the canonical provider and the creator surface must DIFFER for these tests to separate the axes")
	}
	if len(famCreatorNoSurfaces.Creator().Providers()) != 0 {
		t.Fatalf("precondition: %q's creator now has curated surfaces; pick another no-surface family", famCreatorNoSurfaces)
	}
	if famNeitherAxis.Creator() != CreatorNone || famNeitherAxis.CanonicalProvider() != "" {
		t.Fatalf("precondition: %q now carries an axis; pick another unmapped family", famNeitherAxis)
	}
}

// --- Site 1: PURL loose-fallback per-ID representative upgrade -----------------

// TestCreatorFirst_Site1_PURLLooseFallbackRepresentative asserts the per-ID
// representative chosen on the PURL loose-fallback path is the creator-hosted row,
// even when a rehost row for the same ID is seen FIRST.
//
// Seeing the rehost first is the whole point: the site keeps a running representative
// per ID and must UPGRADE it when a better-ranked row arrives later, so a test that
// fed the creator row first would pass against a site that never upgrades at all.
func TestCreatorFirst_Site1_PURLLooseFallbackRepresentative(t *testing.T) {
	creatorFirstPreconditions(t)
	const id = "meta-llama/Llama-3.3-70B-Instruct"
	models := []ModelInfo{
		// Rehost FIRST in registry order.
		{ID: ModelID(id), Provider: provRehostA, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
		{ID: ModelID(id), Provider: provMetaCreatorSurface, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
		// A second, distinct ID so the loose fallback has >1 candidate and stays ambiguous.
		{ID: "meta-llama/Llama-3.1-8B-Instruct", Provider: provRehostB, Family: famCreatorAndCanonicalSplit, Version: "3.1", ParamSize: "8b"},
	}
	withSyntheticRegistry(t, models, func(t *testing.T) {
		_, err := Resolve("pkg:huggingface/nonexistent/" + id)
		amb, ok := err.(*ErrAmbiguous)
		if !ok {
			t.Fatalf("Resolve PURL loose-fallback: got %T (%v), want *ErrAmbiguous", err, err)
		}
		var rep ModelRef
		var found bool
		for _, c := range amb.Candidates {
			if string(c.ID) == id {
				rep, found = c, true
				break
			}
		}
		if !found {
			t.Fatalf("no candidate for ID %q; candidates: %+v", id, amb.Candidates)
		}
		if rep.Provider != provMetaCreatorSurface {
			t.Errorf("site 1: per-ID representative Provider = %q, want %q (the creator's curated surface, "+
				"which appeared AFTER the rehost — the site must upgrade the stored representative)",
				rep.Provider, provMetaCreatorSurface)
		}
	})
}

// --- Site 2: single-group canonical-form provider narrowing --------------------

// TestCreatorFirst_Site2_SingleGroupNarrowing asserts the single-group narrowing
// prefers the creator's surface, and — the fall-through half — that a family with no
// creator surface among its hosts is narrowed by the canonical-provider preference
// exactly as before.
func TestCreatorFirst_Site2_SingleGroupNarrowing(t *testing.T) {
	creatorFirstPreconditions(t)

	t.Run("creator surface wins over rehosts", func(t *testing.T) {
		const id = "llama-3.3-70b-instruct"
		models := []ModelInfo{
			{ID: ModelID(id), Provider: provRehostA, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
			{ID: ModelID(id), Provider: provRehostB, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
			{ID: ModelID(id), Provider: provMetaCreatorSurface, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
		}
		withSyntheticRegistry(t, models, func(t *testing.T) {
			refs, err := Resolve(id, WithInputFormat(InputFormatPeasant))
			if err != nil {
				t.Fatalf("Resolve(%q): %v", id, err)
			}
			if len(refs) != 1 || refs[0].Provider != provMetaCreatorSurface {
				t.Errorf("site 2: Resolve(%q) = %d refs %v, want exactly the creator surface %q",
					id, len(refs), providerSetOf(refs), provMetaCreatorSurface)
			}
		})
	})

	t.Run("canonical preference still fires when no creator surface is hosted", func(t *testing.T) {
		// Same family, but the creator's surfaces are ABSENT — only the canonical
		// provider and rehosts are present. The layer beneath must narrow to it.
		const id = "llama-3.3-70b-instruct"
		models := []ModelInfo{
			{ID: ModelID(id), Provider: provRehostA, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
			{ID: ModelID(id), Provider: provLlamaCanonical, Family: famCreatorAndCanonicalSplit, Version: "3.3", ParamSize: "70b"},
		}
		withSyntheticRegistry(t, models, func(t *testing.T) {
			refs, err := Resolve(id, WithInputFormat(InputFormatPeasant))
			if err != nil {
				t.Fatalf("Resolve(%q): %v", id, err)
			}
			if len(refs) != 1 || refs[0].Provider != provLlamaCanonical {
				t.Errorf("site 2 fall-through: Resolve(%q) = %d refs %v, want exactly the canonical provider %q; "+
					"the creator layer must not bypass or disable the canonical preference",
					id, len(refs), providerSetOf(refs), provLlamaCanonical)
			}
		})
	})

	t.Run("neither axis leaves the full match set untouched", func(t *testing.T) {
		const id = "nothing-known-3.3"
		models := []ModelInfo{
			{ID: ModelID(id), Provider: provRehostA, Family: famNeitherAxis, Version: "3.3"},
			{ID: ModelID(id), Provider: provRehostB, Family: famNeitherAxis, Version: "3.3"},
		}
		withSyntheticRegistry(t, models, func(t *testing.T) {
			refs, err := Resolve(id, WithInputFormat(InputFormatPeasant))
			if err != nil {
				t.Fatalf("Resolve(%q): %v", id, err)
			}
			if len(refs) != 2 {
				t.Errorf("site 2 baseline: Resolve(%q) = %d refs %v, want the full match set of 2 unchanged",
					id, len(refs), providerSetOf(refs))
			}
		})
	})
}

// --- Site 3: collectRehostProviders exclusion ----------------------------------

// TestCreatorFirst_Site3_CollectRehostProviders asserts "Also rehosted by" excludes
// BOTH originating axes. Before the creator axis existed it excluded only the
// canonical provider, so a lab's own hosting surface was listed as a rehost of the
// lab's own weights.
func TestCreatorFirst_Site3_CollectRehostProviders(t *testing.T) {
	creatorFirstPreconditions(t)
	refs := []ModelRef{
		{Family: famCreatorAndCanonicalSplit, Provider: provMetaCreatorSurface},
		{Family: famCreatorAndCanonicalSplit, Provider: provLlamaCanonical},
		{Family: famCreatorAndCanonicalSplit, Provider: provRehostA},
		{Family: famCreatorAndCanonicalSplit, Provider: provRehostB},
	}
	got := collectRehostProviders(refs)
	inGot := func(p Provider) bool {
		for _, g := range got {
			if g == p {
				return true
			}
		}
		return false
	}
	if inGot(provMetaCreatorSurface) {
		t.Errorf("site 3: creator surface %q listed as a rehost; got %v", provMetaCreatorSurface, got)
	}
	if inGot(provLlamaCanonical) {
		t.Errorf("site 3: canonical provider %q listed as a rehost; got %v", provLlamaCanonical, got)
	}
	if !inGot(provRehostA) || !inGot(provRehostB) {
		t.Errorf("site 3: genuine rehosts missing from %v; want both %q and %q", got, provRehostA, provRehostB)
	}
	if len(got) != 2 {
		t.Errorf("site 3: rehost providers = %v, want exactly the two genuine rehosts", got)
	}
}

// --- Site 4: selectRepresentative group tie-break -------------------------------

// TestCreatorFirst_Site4_SelectRepresentative asserts the group representative is the
// creator surface when one is present, the canonical provider when it is not, and the
// lexicographically-smallest provider when neither axis is available.
//
// Each group deliberately lists the winner LAST, so a site that simply returned the
// first row would fail all three arms.
func TestCreatorFirst_Site4_SelectRepresentative(t *testing.T) {
	creatorFirstPreconditions(t)

	t.Run("creator surface wins", func(t *testing.T) {
		group := []ModelRef{
			{Family: famCreatorAndCanonicalSplit, Provider: provRehostB},
			{Family: famCreatorAndCanonicalSplit, Provider: provLlamaCanonical},
			{Family: famCreatorAndCanonicalSplit, Provider: provMetaCreatorSurface},
		}
		if got := selectRepresentative(group); got.Provider != provMetaCreatorSurface {
			t.Errorf("site 4: representative = %q, want the creator surface %q", got.Provider, provMetaCreatorSurface)
		}
	})

	t.Run("canonical provider wins when no creator surface is present", func(t *testing.T) {
		group := []ModelRef{
			{Family: famCreatorAndCanonicalSplit, Provider: provRehostB},
			{Family: famCreatorAndCanonicalSplit, Provider: provRehostA},
			{Family: famCreatorAndCanonicalSplit, Provider: provLlamaCanonical},
		}
		if got := selectRepresentative(group); got.Provider != provLlamaCanonical {
			t.Errorf("site 4 fall-through: representative = %q, want the canonical provider %q", got.Provider, provLlamaCanonical)
		}
	})

	t.Run("lexicographic tie-break when neither axis is present", func(t *testing.T) {
		group := []ModelRef{
			{Family: famNeitherAxis, Provider: "zzz"},
			{Family: famNeitherAxis, Provider: "mmm"},
			{Family: famNeitherAxis, Provider: "aaa"},
		}
		if got := selectRepresentative(group); got.Provider != Provider("aaa") {
			t.Errorf("site 4 baseline: representative = %q, want the lexicographically-smallest %q", got.Provider, "aaa")
		}
	})
}

// --- Site 5: FormatAmbiguous both-axes listing ----------------------------------

// TestCreatorFirst_Site5_BothAxesListing asserts the ambiguity listing renders a
// Creator section BEFORE a Canonical section, that a row appears in exactly one of
// them (Creator winning when a provider satisfies both), and that each section is
// suppressed INDEPENDENTLY when it has no rows.
func TestCreatorFirst_Site5_BothAxesListing(t *testing.T) {
	creatorFirstPreconditions(t)

	render := func(t *testing.T, candidates []ModelRef, rehosts []Provider) string {
		t.Helper()
		var buf bytes.Buffer
		FormatAmbiguous(&buf, &ErrAmbiguous{
			Input:           "llama",
			Scheme:          SchemeCanonical,
			Candidates:      candidates,
			RehostProviders: rehosts,
		})
		return buf.String()
	}

	t.Run("both sections present, Creator first", func(t *testing.T) {
		out := render(t, []ModelRef{
			{Family: famCreatorAndCanonicalSplit, Provider: provLlamaCanonical, Version: "3.1"},
			{Family: famCreatorAndCanonicalSplit, Provider: provMetaCreatorSurface, Version: "3.3"},
		}, nil)
		creatorPos := strings.Index(out, "Creator:")
		canonicalPos := strings.Index(out, "Canonical:")
		if creatorPos < 0 {
			t.Fatalf("site 5: 'Creator:' section absent;\n%s", out)
		}
		if canonicalPos < 0 {
			t.Fatalf("site 5: 'Canonical:' section absent;\n%s", out)
		}
		if creatorPos > canonicalPos {
			t.Errorf("site 5: 'Creator:' must precede 'Canonical:' (creator=%d canonical=%d);\n%s",
				creatorPos, canonicalPos, out)
		}
		if !strings.Contains(out, "+ "+string(provMetaCreatorSurface)+"/") {
			t.Errorf("site 5: creator row not marked with '+ ';\n%s", out)
		}
		if !strings.Contains(out, "* "+string(provLlamaCanonical)+"/") {
			t.Errorf("site 5: canonical row not marked with '* ';\n%s", out)
		}
	})

	t.Run("Canonical suppressed when only creator rows exist", func(t *testing.T) {
		out := render(t, []ModelRef{
			{Family: famCreatorAndCanonicalSplit, Provider: provMetaCreatorSurface, Version: "3.3"},
		}, nil)
		if !strings.Contains(out, "Creator:") {
			t.Errorf("site 5: 'Creator:' section absent;\n%s", out)
		}
		if strings.Contains(out, "Canonical:") {
			t.Errorf("site 5: bare 'Canonical:' header rendered with no canonical rows;\n%s", out)
		}
		if strings.Contains(out, "* = canonical provider") {
			t.Errorf("site 5: orphaned canonical legend rendered with no canonical rows;\n%s", out)
		}
	})

	t.Run("Creator suppressed when only canonical rows exist", func(t *testing.T) {
		out := render(t, []ModelRef{
			{Family: famCreatorAndCanonicalSplit, Provider: provLlamaCanonical, Version: "3.1"},
		}, nil)
		if !strings.Contains(out, "Canonical:") {
			t.Errorf("site 5: 'Canonical:' section absent;\n%s", out)
		}
		if strings.Contains(out, "Creator:") {
			t.Errorf("site 5: bare 'Creator:' header rendered with no creator rows;\n%s", out)
		}
		if strings.Contains(out, "+ = served by the creating lab") {
			t.Errorf("site 5: orphaned creator legend rendered with no creator rows;\n%s", out)
		}
	})

	t.Run("a provider on both axes is listed once, under Creator", func(t *testing.T) {
		// anthropic is BOTH the claude creator's surface and claude's canonical provider.
		var buf bytes.Buffer
		FormatAmbiguous(&buf, &ErrAmbiguous{
			Input:  "claude",
			Scheme: SchemeCanonical,
			Candidates: []ModelRef{
				{Family: "claude", Provider: ProviderAnthropic, Variant: "opus", Version: "4"},
			},
		})
		out := buf.String()
		if !strings.Contains(out, "Creator:") {
			t.Errorf("site 5: coincident provider not rendered under 'Creator:';\n%s", out)
		}
		if strings.Contains(out, "Canonical:") {
			t.Errorf("site 5: coincident provider rendered under BOTH axes (duplicate row);\n%s", out)
		}
		if n := strings.Count(out, string(ProviderAnthropic)+"/claude"); n != 1 {
			t.Errorf("site 5: anthropic row appears %d times, want exactly 1;\n%s", n, out)
		}
	})

	t.Run("neither axis falls back to the entity-key candidate listing", func(t *testing.T) {
		out := render(t, []ModelRef{
			{Family: famNeitherAxis, Provider: provRehostA, Version: "1"},
			{Family: famNeitherAxis, Provider: provRehostB, Version: "2"},
		}, []Provider{provRehostA, provRehostB})
		if strings.Contains(out, "Creator:") || strings.Contains(out, "Canonical:") {
			t.Errorf("site 5 baseline: an originating section rendered for a family with neither axis;\n%s", out)
		}
		if !strings.Contains(out, "Candidates:") {
			t.Errorf("site 5 baseline: the entity-key 'Candidates:' listing must still fire when neither "+
				"axis produces a row, or 'the matching candidates are listed above' is false;\n%s", out)
		}
	})
}

// providerSetOf renders the providers of refs for assertion messages.
func providerSetOf(refs []ModelRef) []Provider {
	out := make([]Provider, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Provider)
	}
	return out
}
