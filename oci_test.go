package bestiary_test

import (
	"encoding/json"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestQuantVRAM_OCIPurl_PassesOwnDigest verifies the exported method wraps
// formatOCIPurl with the row's own OCIDigest as the version, and returns "" when the
// row carries no digest (the never-mint-without-a-digest rule at quant altitude).
func TestQuantVRAM_OCIPurl(t *testing.T) {
	q := bestiary.QuantVRAM{QuantRaw: "q4_k_m", OCIDigest: "sha256:abc123"}
	got := q.OCIPurl("library/llama3.1", "70b", "registry.ollama.ai/library")
	want := "pkg:oci/llama3.1@sha256%3Aabc123?repository_url=registry.ollama.ai%2Flibrary&tag=70b"
	if got != want {
		t.Errorf("QuantVRAM.OCIPurl = %q, want %q", got, want)
	}

	// No digest → no purl (MUST-FAIL), regardless of name/tag/registry.
	noDigest := bestiary.QuantVRAM{QuantRaw: "q4_k_m"}
	if got := noDigest.OCIPurl("llama3.1", "70b", "registry.ollama.ai/library"); got != "" {
		t.Errorf("QuantVRAM.OCIPurl with empty OCIDigest = %q, want \"\"", got)
	}
}

// TestModelRefFormat_OCI_EmptyByDesign is the wave-2 A2 fence: SchemeOCI has an
// EXPLICIT "" arm and must never fall through to the default arm (which returns the
// raw ID). A bare ModelRef has no per-quant digest, so it has no OCI identity.
func TestModelRefFormat_OCI_EmptyByDesign(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "llama3.3:70b-instruct-q4_K_M",
		Provider: bestiary.ProviderHuggingFace,
		Family:   "llama",
		Version:  "3.3",
	}
	got := ref.Format(bestiary.SchemeOCI)
	if got != "" {
		t.Errorf("Format(SchemeOCI) = %q, want \"\" (must NOT leak the raw ID via the default arm)", got)
	}
	if got == string(ref.ID) {
		t.Errorf("Format(SchemeOCI) leaked the raw ID %q — the explicit SchemeOCI arm is missing", ref.ID)
	}
}

// TestDesignations_NoOCIEntry verifies the ModelRef read projection emits NO OCI
// designation (the wave-1 A1 fix: OCI lives at quant altitude, dropped from ModelRef).
func TestDesignations_NoOCIEntry(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "meta-llama/Llama-3.3-70B-Instruct",
		Provider: bestiary.ProviderHuggingFace,
		Family:   "llama",
		Version:  "3.3",
	}
	for _, d := range ref.Designations() {
		if d.Scheme == bestiary.SchemeOCI {
			t.Errorf("Designations() emitted an OCI entry %+v; OCI is per-quant, never a ModelRef designation", d)
		}
	}
}

// TestScheme_OCI_TokenRoundTrip pins the scheme-token surface: String, ParseScheme,
// ParseInputFormat, and JSON marshal/unmarshal all agree on "oci".
func TestScheme_OCI_TokenRoundTrip(t *testing.T) {
	if bestiary.SchemeOCI.String() != "oci" {
		t.Errorf("SchemeOCI.String() = %q, want \"oci\"", bestiary.SchemeOCI.String())
	}
	if s, err := bestiary.ParseScheme("oci"); err != nil || s != bestiary.SchemeOCI {
		t.Errorf("ParseScheme(\"oci\") = (%v, %v), want (SchemeOCI, nil)", s, err)
	}
	if f, err := bestiary.ParseInputFormat("oci"); err != nil || f != bestiary.InputFormatOCI {
		t.Errorf("ParseInputFormat(\"oci\") = (%v, %v), want (InputFormatOCI, nil)", f, err)
	}
	// JSON round-trip: marshal to the token, unmarshal back to the enum.
	b, err := json.Marshal(bestiary.SchemeOCI)
	if err != nil || string(b) != `"oci"` {
		t.Fatalf("json.Marshal(SchemeOCI) = (%s, %v), want (\"oci\", nil)", b, err)
	}
	var back bestiary.CanonicalScheme
	if err := json.Unmarshal([]byte(`"OCI"`), &back); err != nil || back != bestiary.SchemeOCI {
		t.Errorf("json.Unmarshal(\"OCI\") = (%v, %v), want (SchemeOCI, nil) — case-insensitive", back, err)
	}
}

// ociTestEntity builds a constructed entity whose single instance carries one
// digest-bearing quant row — the falsifier for the mint path, since shipped data has
// ZERO digests today and so cannot exercise it.
func ociTestEntity(digest string) bestiary.Entity {
	ref := bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b"}
	return bestiary.Entity{
		Ref:     ref,
		Sources: []bestiary.DataSourceID{bestiary.DataSourceModelsDev, bestiary.DataSourceOllama},
		Instances: []bestiary.ProviderInstance{{
			ID:       "ollama/llama3.3",
			Provider: "ollama",
			Source:   bestiary.DataSourceOllama,
			QuantVRAM: []bestiary.QuantVRAM{{
				QuantRaw:     "q4_k_m",
				WeightsBytes: 42_000_000_000,
				OCIDigest:    digest,
			}},
		}},
	}
}

// TestMintNomina_OCI_PerDigestRow proves the mint path: a digest-bearing quant row
// yields exactly ONE NomenSchemeOCI nomen with the ruled Ollama attestation shape
// (§3.2: Source=ollama, Authority=Secondary, Method=Harvested), and a digest-less
// entity yields NONE.
func TestMintNomina_OCI_PerDigestRow(t *testing.T) {
	e := ociTestEntity("sha256:abc123")
	var oci []bestiary.Nomen
	for _, n := range bestiary.MintNomina([]bestiary.Entity{e}) {
		if n.Scheme == bestiary.NomenSchemeOCI {
			oci = append(oci, n)
		}
	}
	if len(oci) != 1 {
		t.Fatalf("digest-bearing entity minted %d OCI nomina, want 1", len(oci))
	}
	n := oci[0]
	wantValue := "pkg:oci/llama3.3@sha256%3Aabc123?repository_url=registry.ollama.ai%2Flibrary"
	if n.Value != wantValue {
		t.Errorf("OCI nomen Value = %q, want %q", n.Value, wantValue)
	}
	if n.Status != bestiary.AcceptabilityAdmitted {
		t.Errorf("OCI nomen Status = %v, want AcceptabilityAdmitted", n.Status)
	}
	if n.ResolvesTo.String() != e.Ref.String() {
		t.Errorf("OCI nomen ResolvesTo = %q, want %q", n.ResolvesTo.String(), e.Ref.String())
	}
	if len(n.Attestations) != 1 {
		t.Fatalf("OCI nomen carries %d attestations, want 1", len(n.Attestations))
	}
	at := n.Attestations[0]
	if at.Source != bestiary.DataSourceOllama {
		t.Errorf("OCI attestation Source = %q, want %q", at.Source, bestiary.DataSourceOllama)
	}
	if at.Authority != bestiary.AuthoritySecondary {
		t.Errorf("OCI attestation Authority = %v, want AuthoritySecondary", at.Authority)
	}
	if at.Method != bestiary.IngestMethodHarvested {
		t.Errorf("OCI attestation Method = %v, want IngestMethodHarvested", at.Method)
	}
	if at.SourceURL != "https://ollama.com/library/llama3.3" {
		t.Errorf("OCI attestation SourceURL = %q, want the ollama library page", at.SourceURL)
	}

	// A digest-less entity mints no OCI nomen.
	none := ociTestEntity("")
	for _, n := range bestiary.MintNomina([]bestiary.Entity{none}) {
		if n.Scheme == bestiary.NomenSchemeOCI {
			t.Errorf("digest-less entity minted an OCI nomen %q — expected none", n.Value)
		}
	}
}

// TestMintNomina_OCI_ShippedDigestCensus pins the OCI leg over the SHIPPED registry.
// It was written when no quant row carried a digest and asserted zero; the deliberate
// offline Ollama refresh that it named as the arrival point has now landed, so the
// scheme carries real data and this is the guard on its size.
//
// The arithmetic, measured over the shipped bake: 19 entities carry at least one
// digest-bearing quant row, holding 262 DISTINCT digests. A nomen is minted per
// (Value, Scheme, ResolvesTo) triple, and 3 digests are published under more than one
// catalog ID, so the count is the 267 (digest, entity) PAIRS rather than the 262
// digests — the two are different numbers on different axes, and neither is a typo
// for the other. Both mint joints must agree: a digest lives on a ModelInfo's quant
// rows, which both the entity and the from-models path read.
func TestMintNomina_OCI_ShippedDigestCensus(t *testing.T) {
	const wantOCI = 267
	countOCI := func(ns []bestiary.Nomen) int {
		n := 0
		for _, x := range ns {
			if x.Scheme == bestiary.NomenSchemeOCI {
				n++
			}
		}
		return n
	}
	if got := countOCI(bestiary.MintNomina(bestiary.Entities())); got != wantOCI {
		t.Errorf("MintNomina(Entities()) OCI nomina = %d, want %d;\n"+
			"  what went wrong: the shipped OCI census moved\n"+
			"  why: parse/data/quant_vram.json's digest inventory changed (an Ollama refresh), or minting regressed\n"+
			"  where: MintNomina / the OCI leg (oci.go)\n"+
			"  how to fix: re-measure from this tree's corpus and re-pin here and in TestNomina_CensusExact together",
			got, wantOCI)
	}
	if got := countOCI(bestiary.MintNominaFromModels(bestiary.StaticModels())); got != wantOCI {
		t.Errorf("MintNominaFromModels(StaticModels()) OCI nomina = %d, want %d (the two joints must agree — a digest lives on the model's quant rows)",
			got, wantOCI)
	}
}
