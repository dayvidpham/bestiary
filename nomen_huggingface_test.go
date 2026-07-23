package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// hfSeedClaims mirrors the curated huggingface claims in parse/data/nomen_claims.json:
// the Hub org/repo path an open-weight entity's weights live at, the entity key it
// names, and the claimant SourceURL attesting it. Each repo path is a real raw model ID
// present in the committed catalog.
//
// sourceURL is pinned as an archive.org SNAPSHOT of the Hub model card, per the curated
// claims archive policy (see Nomen.SourceURL and the claim file's _comment): a claim is
// evidence of what a lab published, and a Hub model card is edited and deleted without
// notice, so a live URL silently stops attesting the claim. These assertions were
// previously "https://huggingface.co/" + repo — the live page — and were re-pinned when
// the policy landed.
var hfSeedClaims = []struct {
	repo      string
	entityKey string
	sourceURL string
}{
	{
		"meta-llama/Llama-4-Scout-17B-16E-Instruct", "llama/scout@4#17b-16e{instruct}",
		"https://web.archive.org/web/20260715030540/https://huggingface.co/meta-llama/Llama-4-Scout-17B-16E-Instruct",
	},
	{
		"meta-llama/Llama-3.3-70B-Instruct", "llama@3.3#70b{instruct}",
		"https://web.archive.org/web/20260718133241/https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct",
	},
	{
		"Qwen/Qwen3-Coder-480B-A35B-Instruct", "qwen/coder@3#480b-a35b{instruct}",
		"https://web.archive.org/web/20260715051853/https://huggingface.co/Qwen/Qwen3-Coder-480B-A35B-Instruct",
	},
	{
		"deepseek-ai/DeepSeek-V3.2", "deepseek/v3.2",
		"https://web.archive.org/web/20260717140422/https://huggingface.co/deepseek-ai/DeepSeek-V3.2",
	},
}

// TestNomenLookup_HuggingFaceSeeds is the end-to-end fence for the curated Hub claims:
// each seeded repo path loads from the claim file, is classified NomenSchemeHuggingFace,
// carries Admitted status (a third-party naming is never Preferred — only the canonical
// key is), attributes the curated ingest source (distinct from its claimant SourceURL),
// and RESOLVES to the intended entity key.
func TestNomenLookup_HuggingFaceSeeds(t *testing.T) {
	for _, seed := range hfSeedClaims {
		matches, ok := bestiary.NomenLookup(seed.repo)
		if !ok {
			t.Errorf("NomenLookup(%q) found nothing — the curated huggingface claim did not load", seed.repo)
			continue
		}
		var found bool
		for _, n := range matches {
			if n.Scheme != bestiary.NomenSchemeHuggingFace {
				continue
			}
			found = true
			if got := n.ResolvesTo.String(); got != seed.entityKey {
				t.Errorf("%q resolves to %q, want %q", seed.repo, got, seed.entityKey)
			}
			if n.Status != bestiary.AcceptabilityAdmitted {
				t.Errorf("%q status = %v, want admitted", seed.repo, n.Status)
			}
			// v0.2.8: provenance is per-attestation; this curated HF seed carries one,
			// read fully off the NomenAttestation (Source + SourceURL below). The store
			// persists such attestations in the v8 nomen_attestations child table.
			if len(n.Attestations) != 1 {
				t.Fatalf("%q carries %d attestations, want 1", seed.repo, len(n.Attestations))
			}
			at := n.Attestations[0]
			if at.Source != bestiary.DataSourceCurated {
				t.Errorf("%q ingest source = %q, want the curated claim file", seed.repo, at.Source)
			}
			if at.SourceURL != seed.sourceURL {
				t.Errorf("%q claimant SourceURL = %q, want the pinned archive snapshot %q", seed.repo, at.SourceURL, seed.sourceURL)
			}
			// The policy's own justification, asserted rather than assumed: the
			// snapshot is durable AND self-describing — the live Hub model card the
			// claim was read from is recoverable verbatim from the snapshot's tail,
			// which is why no separate archive_url field exists.
			if !strings.HasPrefix(at.SourceURL, archiveSnapshotPrefix) {
				t.Errorf("%q claimant SourceURL = %q, want an %s snapshot: a curated claim must cite durable evidence, not a live page",
					seed.repo, at.SourceURL, archiveSnapshotPrefix)
			}
			if live := "https://huggingface.co/" + seed.repo; !strings.HasSuffix(at.SourceURL, live) {
				t.Errorf("%q snapshot %q does not end in the original claimant URL %q; the live address must stay recoverable from the value itself",
					seed.repo, at.SourceURL, live)
			}
		}
		if !found {
			t.Errorf("NomenLookup(%q) returned no huggingface-scheme nomen (matches: %+v)", seed.repo, matches)
		}
	}
}

// TestEntityNomina_CarriesHuggingFaceSeed demonstrates the point of an ENTITY-level
// identifier: an entity served by many providers carries its Hub name regardless of which
// provider is serving it, alongside its canonical key and its per-provider ID spellings.
func TestEntityNomina_CarriesHuggingFaceSeed(t *testing.T) {
	const key = "llama/scout@4#17b-16e{instruct}"
	e, ok := bestiary.EntityByKey(key)
	if !ok {
		t.Fatalf("entity %q not found in the static registry", key)
	}
	if len(e.Instances) < 2 {
		t.Fatalf("entity %q has %d instances; the multi-provider point needs at least 2", key, len(e.Instances))
	}
	var hubName string
	for _, n := range e.Nomina() {
		if n.Scheme == bestiary.NomenSchemeHuggingFace {
			hubName = n.Value
		}
	}
	if hubName != "meta-llama/Llama-4-Scout-17B-16E-Instruct" {
		t.Errorf("entity %q huggingface nomen = %q, want the curated Hub repo path", key, hubName)
	}
}

// TestHuggingFaceSeeds_RepoPathsExistInCatalog guards the curation against invention: a
// seeded Hub repo path must appear VERBATIM as a raw model ID somewhere in the committed
// catalog. A path nobody serves under that spelling would be an unverifiable claim.
func TestHuggingFaceSeeds_RepoPathsExistInCatalog(t *testing.T) {
	ids := make(map[string]struct{})
	for _, m := range bestiary.StaticModels() {
		ids[string(m.ID)] = struct{}{}
	}
	for _, seed := range hfSeedClaims {
		if _, ok := ids[seed.repo]; !ok {
			t.Errorf("curated huggingface claim %q is not a raw model ID in the committed catalog", seed.repo)
		}
		if !strings.Contains(seed.repo, "/") {
			t.Errorf("curated huggingface claim %q is not an org/repo path", seed.repo)
		}
	}
}
