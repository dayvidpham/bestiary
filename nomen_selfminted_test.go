package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// canonicalAttestationCensus walks a minted nomen slice and reports, for the
// Canonical scheme only, how many nomina were seen and how many carried each
// distinct attestation Source / Authority / Method. Counting rather than
// short-circuiting on the first row is what makes the assertions below universal:
// a single un-flipped mint site shows up as a non-zero models.dev bucket rather
// than being masked by the majority.
type canonicalAttestationCensus struct {
	canonical  int
	bySource   map[bestiary.DataSourceID]int
	authority  map[bestiary.AttestationAuthority]int
	method     map[bestiary.IngestMethod]int
	withURL    int
	otherBySrc map[bestiary.DataSourceID]int
}

func censusCanonicalAttestations(nomina []bestiary.Nomen) canonicalAttestationCensus {
	c := canonicalAttestationCensus{
		bySource:   map[bestiary.DataSourceID]int{},
		authority:  map[bestiary.AttestationAuthority]int{},
		method:     map[bestiary.IngestMethod]int{},
		otherBySrc: map[bestiary.DataSourceID]int{},
	}
	for _, n := range nomina {
		if n.Scheme != bestiary.NomenSchemeCanonical {
			for _, at := range n.Attestations {
				c.otherBySrc[at.Source]++
			}
			continue
		}
		c.canonical++
		for _, at := range n.Attestations {
			c.bySource[at.Source]++
			c.authority[at.Authority]++
			c.method[at.Method]++
			if at.SourceURL != "" {
				c.withURL++
			}
		}
	}
	return c
}

// assertSelfMintedCanonical is the shared oracle for one mint path: every canonical
// nomen it produces must be attributed to the self-referential bestiary source, with
// its Authority/Method unchanged (Primary / SelfMinted) and no SourceURL — a
// self-minted key has no third-party claimant to cite.
func assertSelfMintedCanonical(t *testing.T, path string, nomina []bestiary.Nomen) {
	t.Helper()
	c := censusCanonicalAttestations(nomina)
	if c.canonical == 0 {
		t.Fatalf("%s: minted 0 canonical nomina; the oracle would be vacuous", path)
	}
	if got := c.bySource[bestiary.DataSourceBestiary]; got != c.canonical {
		t.Errorf("%s: %d/%d canonical attestations carry Source=%q; the full source census is %v",
			path, got, c.canonical, bestiary.DataSourceBestiary, c.bySource)
	}
	if got := c.bySource[bestiary.DataSourceModelsDev]; got != 0 {
		t.Errorf("%s: %d canonical attestations still carry Source=%q — a bestiary-authored key must not be credited to an upstream",
			path, got, bestiary.DataSourceModelsDev)
	}
	// Authority/Method are explicitly NOT changed by the FK flip.
	if got := c.authority[bestiary.AuthorityPrimary]; got != c.canonical {
		t.Errorf("%s: %d/%d canonical attestations are AuthorityPrimary; census %v", path, got, c.canonical, c.authority)
	}
	if got := c.method[bestiary.IngestMethodSelfMinted]; got != c.canonical {
		t.Errorf("%s: %d/%d canonical attestations are IngestMethodSelfMinted; census %v", path, got, c.canonical, c.method)
	}
	if c.withURL != 0 {
		t.Errorf("%s: %d canonical attestations carry a SourceURL; a self-minted key asserts itself", path, c.withURL)
	}
	// The self-referential source is for AUTHORED claims only: no harvested or
	// curated nomen may borrow it.
	if got := c.otherBySrc[bestiary.DataSourceBestiary]; got != 0 {
		t.Errorf("%s: %d NON-canonical attestations carry Source=%q; only self-minted canonical keys are bestiary-authored",
			path, got, bestiary.DataSourceBestiary)
	}
}

// TestCanonicalAttestation_SelfReferentialSource_BothMintPaths is the oracle for the
// shared mint joints: BOTH of them attribute their self-minted canonical nomina to
// the self-referential bestiary source. The from-entities joint (MintNomina) and the
// from-models joint (MintNominaFromModels, the sync path) are separate call sites
// that previously hard-coded different sources, so each is asserted independently —
// a flip applied to only one of them fails here.
func TestCanonicalAttestation_SelfReferentialSource_BothMintPaths(t *testing.T) {
	assertSelfMintedCanonical(t, "MintNomina(Entities())", bestiary.MintNomina(bestiary.Entities()))
	assertSelfMintedCanonical(t, "MintNominaFromModels(StaticModels())", bestiary.MintNominaFromModels(bestiary.StaticModels()))
}

// TestCanonicalAttestation_SourceResolvesViaFK guards the provenance FK itself: the
// Source every canonical attestation names must resolve to a real DataSource
// dimension row. This is what would have caught a constant added without its
// datasources.json seed row — the store's nomen_attestations FK rejects such a row
// at sync time, and the codegen guard rejects it at generation time.
func TestCanonicalAttestation_SourceResolvesViaFK(t *testing.T) {
	ds, ok := bestiary.DataSourceByID(bestiary.DataSourceBestiary)
	if !ok {
		t.Fatalf("DataSourceByID(%q) missed; the self-referential dimension row must be seeded in parse/data/datasources.json",
			bestiary.DataSourceBestiary)
	}
	if ds.URI == "" {
		t.Error("the bestiary source has an empty uri; uri is a candidate key of the dimension")
	}
	// URI is a second candidate key: the self-referential row must not collide with
	// the curated claim-files row, which points at a path INSIDE the same repo.
	cur, ok := bestiary.DataSourceByID(bestiary.DataSourceCurated)
	if ok && cur.URI == ds.URI {
		t.Errorf("bestiary and curated share the uri %q; uri is a candidate key and must be unique", ds.URI)
	}
	if _, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceBestiary); !ok {
		t.Errorf("DatasetIngestedFor(%q) missed; the seeded source needs an ingest row so the sources view can join it",
			bestiary.DataSourceBestiary)
	}
}
