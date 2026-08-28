package bestiary_test

// Fences for the canonical-provider preference on provider-UNQUALIFIED lookups.
//
// The rule under test: when a canonical-form (peasant/SchemeCanonical) lookup
// collapses to a single cross-provider group, Resolve returns only the rows of
// the family's curated CanonicalProvider() when that provider is non-empty AND
// present in the match set; otherwise it returns the full match set unchanged.
//
// The preference deliberately applies to EXACT-ID inputs too. It previously did
// not: an exact raw ID short-circuited the preference, so the representative a
// single-model consumer renders was whichever row the static registry listed
// first — the alphabetically-first provider — and `show claude-sonnet-4-5-20250929`
// reported a rehost as the provider of a first-party Anthropic model.

import (
	_ "embed"
	"errors"
	"sort"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/resolve/canonical_provider_preference_corpus.json
var resolveCanonicalPrefCorpusJSON []byte

// canonicalPrefExpected is the observable outcome of one provider-unqualified
// exact-ID lookup: the provider heading the returned slice (the representative
// `bestiary show` renders), and whether the preference collapsed the slice to
// that provider alone.
//
// Both fields are plain comparable scalars so the corpus can be guarded by the
// value-based coverage check as well as the exact-count control.
type canonicalPrefExpected struct {
	Provider string `json:"provider"`
	Sole     bool   `json:"sole"`
}

// catalogHosts returns the providers hosting the exact model ID in the built
// static catalog, sorted. It is the independent measurement the corpus runner
// checks Resolve against: without it a "sole == true" assertion could pass
// vacuously on an ID that only ever had one host.
func catalogHosts(id string) []bestiary.Provider {
	var out []bestiary.Provider
	for _, m := range bestiary.StaticModels() {
		if string(m.ID) == id {
			out = append(out, m.Provider)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func containsProvider(ps []bestiary.Provider, want bestiary.Provider) bool {
	for _, p := range ps {
		if p == want {
			return true
		}
	}
	return false
}

// TestResolve_CanonicalProviderPreference_ExactID_Corpus drives the corpus of
// real multi-provider catalog IDs through the production Resolve entry point in
// its default CLI configuration (InputFormatPeasant — exactly what
// `bestiary show <id>` passes) and asserts both arms of the rule:
//
//   - sole == true  → the curated canonical provider hosts the ID, so Resolve
//     returns ONLY that provider's row even though the catalog has other hosts;
//   - sole == false → the canonical is empty or absent from the match set, so
//     the FULL match set comes back and the head is the alphabetically-first
//     provider.
//
// Every case additionally requires the ID to still be multi-provider in the
// built catalog: a case that quietly became single-host is no longer exercising
// the preference and fails loudly instead of passing vacuously.
func TestResolve_CanonicalProviderPreference_ExactID_Corpus(t *testing.T) {
	corpus := loadParseCorpus[string, canonicalPrefExpected](t, resolveCanonicalPrefCorpusJSON, 14)

	requireInputCoverage(t, corpus, map[string]canonicalPrefExpected{
		// the reported defect, pinned by value
		"claude-sonnet-4-5-20250929": {Provider: "anthropic", Sole: true},
		// the command→cohere mapping, pinned by value
		"command-a-plus-05-2026": {Provider: "cohere", Sole: true},
		// one fall-through control of each kind
		"codestral-2501": {Provider: "azure", Sole: false},
		"MiniMax-M1":     {Provider: "302ai", Sole: false},
	})

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			hosts := catalogHosts(c.Input)
			if len(hosts) < 2 {
				t.Fatalf("catalog precondition lost: id %q is hosted by %v, want >= 2 providers "+
					"(a single-host id cannot exercise the canonical-provider preference; "+
					"re-pick the case against the current catalog)", c.Input, hosts)
			}

			refs, err := bestiary.Resolve(c.Input, bestiary.WithInputFormat(bestiary.InputFormatPeasant))
			if err != nil {
				t.Fatalf("Resolve(%q, peasant) returned error: %v", c.Input, err)
			}
			if len(refs) == 0 {
				t.Fatalf("Resolve(%q, peasant) returned no refs", c.Input)
			}
			for _, r := range refs {
				if string(r.ID) != c.Input {
					t.Errorf("Resolve(%q) returned ref with ID %q; every ref must carry the exact input ID", c.Input, r.ID)
				}
			}

			want := bestiary.Provider(c.Expected.Provider)
			if refs[0].Provider != want {
				t.Errorf("Resolve(%q)[0].Provider = %q, want %q (the provider `bestiary show` renders); full set: %v",
					c.Input, refs[0].Provider, want, providersOf(refs))
			}

			// Non-vacuity: a `sole` case must be won by a CURATED axis, never by an
			// accident of registry ordering. There are two such axes and the winner
			// may come from either — the creator's curated distribution surfaces
			// (checked first, as resolution checks them first) or the family's
			// canonical provider.
			canon := refs[0].Family.CanonicalProvider()
			creatorSurfaces := refs[0].Family.Creator().Providers()
			wonByCreator := containsProvider(creatorSurfaces, want)
			wonByCanonical := canon == want

			if c.Expected.Sole {
				if !wonByCreator && !wonByCanonical {
					t.Errorf("family %q: expected provider %q is neither one of creator %q's curated surfaces %v "+
						"nor the curated canonical %q — the case asserts a PREFERENCE, so the winner must be curated "+
						"on one of the two axes, not an accident of ordering",
						refs[0].Family, want, refs[0].Family.Creator(), creatorSurfaces, canon)
				}
				if len(refs) != 1 {
					t.Errorf("Resolve(%q) returned %d refs (%v), want exactly 1 (the preferred provider %q); "+
						"the preference must drop the %d other host(s)", c.Input, len(refs), providersOf(refs), want, len(hosts)-1)
				}
			} else {
				// Fall-through cases must have NEITHER axis available among the hosts,
				// or the preference should have fired and the case is misclassified.
				for _, cp := range creatorSurfaces {
					if containsProvider(hosts, cp) {
						t.Errorf("case asserts fall-through, but family %q's creator %q has curated surface %q and it IS "+
							"among the hosts %v — the creator preference should have fired; re-classify this case",
							refs[0].Family, refs[0].Family.Creator(), cp, hosts)
					}
				}
				if canon != "" && containsProvider(hosts, canon) {
					t.Errorf("case asserts fall-through, but family %q has curated canonical %q and it IS among the "+
						"hosts %v — the preference should have fired; re-classify this case", refs[0].Family, canon, hosts)
				}
				if len(refs) != len(hosts) {
					t.Errorf("Resolve(%q) returned %d refs (%v), want the full match set of %d (%v)",
						c.Input, len(refs), providersOf(refs), len(hosts), hosts)
				}
			}
		})
	}
}

// providersOf projects the provider of each ref, for error messages.
func providersOf(refs []bestiary.ModelRef) []bestiary.Provider {
	out := make([]bestiary.Provider, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Provider)
	}
	return out
}

// TestResolve_CanonicalProviderPreference_CatalogSweep is the census twin of the
// corpus: it walks EVERY multi-provider model ID in the built catalog and, for
// each one whose family carries a curated CanonicalProvider() that is among the
// hosts, asserts the provider-unqualified lookup returns that provider alone.
//
// The corpus pins named, hand-explained cases; this sweep guarantees the rule
// has no exceptions anywhere in the shipped data — including families the corpus
// cannot cover today (llama, o) if a catalog refresh ever gives them a
// canonically-hosted multi-provider ID.
func TestResolve_CanonicalProviderPreference_CatalogSweep(t *testing.T) {
	byID := map[string][]bestiary.ModelInfo{}
	var ids []string
	for _, m := range bestiary.StaticModels() {
		id := string(m.ID)
		if _, seen := byID[id]; !seen {
			ids = append(ids, id)
		}
		byID[id] = append(byID[id], m)
	}
	sort.Strings(ids)

	swept := 0
	for _, id := range ids {
		rows := byID[id]
		if len(rows) < 2 {
			continue
		}
		hosts := catalogHosts(id)
		// The representative family is the one Resolve itself consults: the family of
		// the first match, which is the alphabetically-first provider's row because
		// the static catalog is sorted by (Provider, ID).
		canon := hosts0Family(rows).CanonicalProvider()
		if canon == "" || !containsProvider(hosts, canon) {
			continue
		}
		swept++
		refs, err := bestiary.Resolve(id, bestiary.WithInputFormat(bestiary.InputFormatPeasant))
		if err != nil {
			// A multi-group input (genuinely distinct models sharing an id shape) is not
			// what this sweep is about; only clean single-group resolutions are graded.
			var ambig *bestiary.ErrAmbiguous
			if errors.As(err, &ambig) {
				continue
			}
			t.Errorf("Resolve(%q, peasant) returned error: %v", id, err)
			continue
		}
		if len(refs) != 1 || refs[0].Provider != canon {
			t.Errorf("Resolve(%q, peasant) = %v, want exactly [%s] — the curated canonical provider hosts this id "+
				"(hosts: %v), so the preference must collapse the set to it", id, providersOf(refs), canon, hosts)
		}
	}

	// A sweep that graded nothing is a green badge over zero coverage.
	if swept < 100 {
		t.Fatalf("sweep graded only %d canonically-hosted multi-provider ids, want >= 100 — "+
			"the catalog or the family→canonical mapping shrank drastically; re-check before lowering this floor", swept)
	}
	t.Logf("canonical-provider preference verified on %d canonically-hosted multi-provider ids", swept)
}

// hosts0Family returns the family of the alphabetically-first provider's row —
// the row Resolve's match set starts with.
func hosts0Family(rows []bestiary.ModelInfo) bestiary.Family {
	best := rows[0]
	for _, m := range rows[1:] {
		if m.Provider < best.Provider {
			best = m
		}
	}
	return best.Family
}

// TestResolve_CanonicalProviderPreference_InertFamilies documents the two
// curated-canonical families the preference cannot reach in the shipped catalog,
// so the gap is a recorded fact rather than silent absence of coverage:
//
//   - llama's canonical is the synthetic `local` provider, which serves NO rows
//     at all, so no llama lookup can ever prefer it;
//   - the `o` family (o1/o3-style ids) has no multi-provider id, so the
//     preference is a no-op there.
//
// If a catalog refresh changes either fact, this test fails and the corpus above
// gains a real case for that family (the sweep will already be grading it).
func TestResolve_CanonicalProviderPreference_InertFamilies(t *testing.T) {
	localRows := 0
	multiProviderO := 0

	byID := map[string][]bestiary.ModelInfo{}
	for _, m := range bestiary.StaticModels() {
		if m.Provider == bestiary.ProviderLocal {
			localRows++
		}
		byID[string(m.ID)] = append(byID[string(m.ID)], m)
	}
	for _, rows := range byID {
		if len(rows) >= 2 && hosts0Family(rows) == bestiary.FamilyO {
			multiProviderO++
		}
	}

	if localRows != 0 {
		t.Errorf("provider %q now serves %d model rows; the llama family's curated canonical is no longer inert — "+
			"add a llama case to testdata/resolve/canonical_provider_preference_corpus.json",
			bestiary.ProviderLocal, localRows)
	}
	if multiProviderO != 0 {
		t.Errorf("the %q family now has %d multi-provider id(s); the preference is no longer a no-op there — "+
			"add an o-family case to testdata/resolve/canonical_provider_preference_corpus.json",
			bestiary.FamilyO, multiProviderO)
	}
}

// TestResolve_ProviderQualified_Unaffected fences the blast radius: the
// canonical-provider preference applies ONLY to canonical-form,
// provider-unqualified lookups. A caller who pins the provider — through a PURL
// namespace or the (ID, Provider) registry lookup — must still get exactly the
// row they asked for, rehost or not.
//
// The non-canonical schemes are pinned here too, because their behavior is
// deliberately NOT changed by this fix:
//
//   - a HuggingFace-style "<namespace>/<id>" prefix is STRIPPED, never filtered
//     (long-standing behavior), so the lookup returns the full cross-provider set;
//   - --format=raw is the "exactly what the upstream API says" escape hatch and
//     stays on the full match set, in registry order.
//
// Both are recorded as observed facts so a later decision to extend the
// preference across schemes is a visible, deliberate re-cut of these assertions
// rather than an accident.
func TestResolve_ProviderQualified_Unaffected(t *testing.T) {
	const id = "claude-sonnet-4-5-20250929"
	const rehost = bestiary.Provider("302ai")

	hosts := catalogHosts(id)
	if !containsProvider(hosts, rehost) {
		t.Fatalf("catalog precondition lost: %q is no longer hosted by %q; re-pick the rehost", id, rehost)
	}

	t.Run("huggingface-namespace-strips-not-filters", func(t *testing.T) {
		refs, err := bestiary.Resolve(string(rehost)+"/"+id, bestiary.WithInputFormat(bestiary.InputFormatHuggingFace))
		if err != nil {
			t.Fatalf("Resolve(hf %q/%q) returned error: %v", rehost, id, err)
		}
		if len(refs) != len(hosts) {
			t.Errorf("hf lookup returned %d refs (%v), want the full cross-provider set of %d (%v): the hf namespace "+
				"is stripped, not applied as a provider filter, and the canonical-provider preference does not run on SchemeHuggingFace",
				len(refs), providersOf(refs), len(hosts), hosts)
		}
	})

	t.Run("raw-format-returns-full-set", func(t *testing.T) {
		refs, err := bestiary.Resolve(id, bestiary.WithInputFormat(bestiary.InputFormatRaw))
		if err != nil {
			t.Fatalf("Resolve(raw %q) returned error: %v", id, err)
		}
		if len(refs) != len(hosts) {
			t.Errorf("raw lookup returned %d refs (%v), want the full match set of %d (%v): --format=raw is the "+
				"unfiltered upstream-id escape hatch and the canonical-provider preference does not run on SchemeRaw",
				len(refs), providersOf(refs), len(hosts), hosts)
		}
	})

	t.Run("purl-namespace", func(t *testing.T) {
		refs, err := bestiary.Resolve("pkg:huggingface/"+string(rehost)+"/"+id, bestiary.WithInputFormat(bestiary.InputFormatPURL))
		if err != nil {
			t.Fatalf("Resolve(purl %q/%q) returned error: %v", rehost, id, err)
		}
		for _, r := range refs {
			if r.Provider != rehost {
				t.Errorf("provider-qualified purl lookup returned provider %q, want only %q", r.Provider, rehost)
			}
		}
	})

	t.Run("lookup-by-provider", func(t *testing.T) {
		m, ok := bestiary.LookupModelByProvider(rehost, id)
		if !ok {
			t.Fatalf("LookupModelByProvider(%q, %q) = not found; the rehost row must stay reachable", rehost, id)
		}
		if m.Provider != rehost {
			t.Errorf("LookupModelByProvider(%q, %q).Provider = %q, want %q", rehost, id, m.Provider, rehost)
		}
	})
}

// TestResolve_Ambiguity_Unaffected fences the other boundary: the preference
// runs only on the single-group path. An input matching two or more distinct
// canonical groups must still return *ErrAmbiguous with its candidate list —
// the preference must never collapse genuinely distinct models into one answer.
func TestResolve_Ambiguity_Unaffected(t *testing.T) {
	refs, err := bestiary.Resolve("claude", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err == nil {
		t.Fatalf("Resolve(\"claude\", canonical) = %v, want *ErrAmbiguous: the bare family spans opus/sonnet/haiku", providersOf(refs))
	}
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("Resolve(\"claude\", canonical) returned %T (%v), want *ErrAmbiguous", err, err)
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf("ErrAmbiguous.Candidates = %d, want >= 2 distinct canonicals for the bare family \"claude\"", len(ambig.Candidates))
	}
}
