package bestiary_test

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// hfSeedClaims mirrors the curated huggingface claims in parse/data/nomen_claims.json:
// the Hub org/repo path an open-weight entity's weights live at, and the entity key it
// names. Each repo path is a real raw model ID present in the committed catalog.
var hfSeedClaims = []struct {
	repo      string
	entityKey string
}{
	{"meta-llama/Llama-4-Scout-17B-16E-Instruct", "llama/scout@4#17b-16e{instruct}"},
	{"meta-llama/Llama-3.3-70B-Instruct", "llama@3.3#70b{instruct}"},
	{"Qwen/Qwen3-Coder-480B-A35B-Instruct", "qwen/coder@3#480b-a35b{instruct}"},
	{"deepseek-ai/DeepSeek-V3.2", "deepseek/v3.2"},
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
			if n.Source != bestiary.DataSourceCurated {
				t.Errorf("%q ingest source = %q, want the curated claim file", seed.repo, n.Source)
			}
			if want := "https://huggingface.co/" + seed.repo; n.SourceURL != want {
				t.Errorf("%q claimant SourceURL = %q, want %q", seed.repo, n.SourceURL, want)
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
