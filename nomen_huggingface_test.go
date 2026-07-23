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
// claims archive policy (see NomenAttestation.SourceURL and the claim file's _comment):
// a claim is evidence of what a lab published, and a Hub model card is edited and deleted
// without notice, so a live URL silently stops attesting the claim. These assertions were
// previously "https://huggingface.co/" + repo — the live page — and were re-pinned when
// the policy landed. NOTE the archive-pin fence binds the CURATED layer (nomen_claims.json)
// ONLY: the harvested HF layer (huggingface_nomina.json, cmd/bestiary-hf) cites the LIVE
// repo URL and is Method=Harvested — see TestHuggingFaceNomina_* in huggingface_nomina_test.go.
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

// TestNomenLookup_HuggingFaceSeeds is the end-to-end fence for the Hub claims AND the
// §11 case-3 union on REAL data: each seeded repo path is classified
// NomenSchemeHuggingFace, carries Admitted status (a third-party naming is never
// Preferred — only the canonical key is), RESOLVES to the intended entity key, and —
// because the cmd/bestiary-hf live run harvested these same four repos (aliased to their
// exact curated triples in hf_aliases.json) — coalesces to ONE nomen carrying TWO
// attestations: the CURATED claim (archive-snapshot SourceURL, Source=curated,
// Method=Curated) AND the HARVESTED observation (live Hub URL, Source=huggingface,
// Method=Harvested). This is the genuinely-new same-triple multi-attestation union the
// whole attestation model exists for (exact case match is what makes the triples
// collide). Provenance is read FULLY off the NomenAttestation (the v0.2.8 HF-bot EXTEND).
func TestNomenLookup_HuggingFaceSeeds(t *testing.T) {
	for _, seed := range hfSeedClaims {
		matches, ok := bestiary.NomenLookup(seed.repo)
		if !ok {
			t.Errorf("NomenLookup(%q) found nothing — the huggingface nomen did not load", seed.repo)
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
			// Case-3 union: exactly TWO attestations (curated claim + harvested
			// observation), located by their distinct Source/Method and each fully
			// asserted off the NomenAttestation.
			if len(n.Attestations) != 2 {
				t.Fatalf("%q carries %d attestations, want 2 (curated + harvested same-triple union)", seed.repo, len(n.Attestations))
			}
			var cur, harv *bestiary.NomenAttestation
			for i := range n.Attestations {
				switch n.Attestations[i].Source {
				case bestiary.DataSourceCurated:
					cur = &n.Attestations[i]
				case bestiary.DataSourceHuggingFace:
					harv = &n.Attestations[i]
				}
			}
			if cur == nil || harv == nil {
				t.Fatalf("%q attestations missing a leg (curated=%v harvested=%v): %+v", seed.repo, cur != nil, harv != nil, n.Attestations)
			}

			// CURATED leg: Method=Curated, Authority=Primary, an archive-snapshot
			// SourceURL that is durable AND self-describing (the live Hub URL is
			// recoverable verbatim from the snapshot's tail — why no archive_url field).
			if cur.Method != bestiary.IngestMethodCurated {
				t.Errorf("%q curated leg Method = %v, want curated", seed.repo, cur.Method)
			}
			if cur.Authority != bestiary.AuthorityPrimary {
				t.Errorf("%q curated leg Authority = %v, want primary", seed.repo, cur.Authority)
			}
			if cur.SourceURL != seed.sourceURL {
				t.Errorf("%q curated SourceURL = %q, want the pinned archive snapshot %q", seed.repo, cur.SourceURL, seed.sourceURL)
			}
			if !strings.HasPrefix(cur.SourceURL, archiveSnapshotPrefix) {
				t.Errorf("%q curated SourceURL = %q, want an %s snapshot (durable evidence, not a live page)", seed.repo, cur.SourceURL, archiveSnapshotPrefix)
			}
			live := "https://huggingface.co/" + seed.repo
			if !strings.HasSuffix(cur.SourceURL, live) {
				t.Errorf("%q snapshot %q does not end in the original claimant URL %q", seed.repo, cur.SourceURL, live)
			}

			// HARVESTED leg: Method=Harvested, Authority=Primary (the Hub is
			// authoritative for the huggingface scheme), and the LIVE repo URL
			// (case-preserved) — NOT an archive snapshot (that fence binds the curated
			// layer only).
			if harv.Method != bestiary.IngestMethodHarvested {
				t.Errorf("%q harvested leg Method = %v, want harvested", seed.repo, harv.Method)
			}
			if harv.Authority != bestiary.AuthorityPrimary {
				t.Errorf("%q harvested leg Authority = %v, want primary", seed.repo, harv.Authority)
			}
			if harv.SourceURL != live {
				t.Errorf("%q harvested SourceURL = %q, want the live repo URL %q (case-preserved, not an archive snapshot)", seed.repo, harv.SourceURL, live)
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
