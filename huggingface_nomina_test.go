package bestiary

import (
	"strings"
	"testing"
)

// parseHFNomina and coalesceNomina are unexported, so these are INTERNAL
// (package bestiary) tests: they drive the harvested-seed loader and the
// multi-attestation union over CONSTRUCTED bytes, so the harvested path is fully
// falsifiable without any live run and independent of whatever the committed
// huggingface_nomina.json currently contains.

// hfSeedBytes builds a minimal valid harvested-seed document from one entry.
func hfSeedBytes(value, family, extra, sourceURL string) []byte {
	return []byte(`{
  "schema_version": 1,
  "nomina": [
    {"value": "` + value + `", "resolves_to": {"family": "` + family + `"` + extra + `}, "source_url": "` + sourceURL + `"}
  ]
}`)
}

func TestParseHFNomina_Valid_HarvestedAttestation(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	raw := hfSeedBytes(repo, "llama", `, "version": "3.3", "param_size": "70b", "modifier": ["instruct"]`, hfLiveURLPrefix+repo)
	tbl, err := parseHFNomina(raw)
	if err != nil {
		t.Fatalf("parseHFNomina: %v", err)
	}
	if len(tbl.nomina) != 1 {
		t.Fatalf("got %d nomina, want 1", len(tbl.nomina))
	}
	n := tbl.nomina[0]
	if n.Value != repo {
		t.Errorf("value = %q, want case-preserved %q", n.Value, repo)
	}
	if n.Scheme != NomenSchemeHuggingFace {
		t.Errorf("scheme = %v, want huggingface", n.Scheme)
	}
	if n.Status != AcceptabilityAdmitted {
		t.Errorf("status = %v, want admitted (a third-party naming is never Preferred)", n.Status)
	}
	if len(n.Attestations) != 1 {
		t.Fatalf("got %d attestations, want 1", len(n.Attestations))
	}
	at := n.Attestations[0]
	if at.Source != DataSourceHuggingFace {
		t.Errorf("attestation Source = %q, want huggingface", at.Source)
	}
	if at.Method != IngestMethodHarvested {
		t.Errorf("attestation Method = %v, want harvested (a live Hub observation, NOT a curated claim)", at.Method)
	}
	if at.Authority != AuthorityPrimary {
		t.Errorf("attestation Authority = %v, want primary (the Hub is authoritative for the huggingface scheme)", at.Authority)
	}
	if at.SourceURL != hfLiveURLPrefix+repo {
		t.Errorf("attestation SourceURL = %q, want the LIVE repo URL %q", at.SourceURL, hfLiveURLPrefix+repo)
	}
	if want := "llama@3.3#70b{instruct}"; n.ResolvesTo.String() != want {
		t.Errorf("resolves_to = %q, want %q", n.ResolvesTo.String(), want)
	}
}

// Lowercasing an HF org/repo is a MUST-FAIL: the loader cross-checks source_url ==
// live-prefix + value, so a value whose case disagrees with its (case-significant)
// source_url is REJECTED rather than silently minting a case-mangled Hub name.
func TestParseHFNomina_CaseMismatch_MustFail(t *testing.T) {
	// value carries the true mixed case; source_url was lowercased — a mismatch.
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	raw := hfSeedBytes(repo, "llama", `, "version": "3.3"`, hfLiveURLPrefix+strings.ToLower(repo))
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: a lowercased source_url for a mixed-case value must fail (case preservation)")
	}
}

// The harvested layer must NOT accept an archive.org snapshot URL — that fence
// binds the CURATED layer only. A harvested nomen cites the LIVE observation.
func TestParseHFNomina_ArchiveURL_MustFail(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	archive := "https://web.archive.org/web/20260715030540/https://huggingface.co/" + repo
	raw := hfSeedBytes(repo, "llama", `, "version": "3.3"`, archive)
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: a harvested nomen must cite the live repo URL, not an archive snapshot")
	}
}

func TestParseHFNomina_NonOrgRepo_MustFail(t *testing.T) {
	raw := hfSeedBytes("just-a-model-no-slash", "llama", "", hfLiveURLPrefix+"just-a-model-no-slash")
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: an HF id must be org/repo (contain '/')")
	}
}

func TestParseHFNomina_UnknownFamily_MustFail(t *testing.T) {
	const repo = "some-org/some-model"
	raw := hfSeedBytes(repo, "not-a-real-family-xyz", "", hfLiveURLPrefix+repo)
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: resolves_to.family must be a known base family")
	}
}

func TestParseHFNomina_Duplicate_MustFail(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	raw := []byte(`{
  "schema_version": 1,
  "nomina": [
    {"value": "` + repo + `", "resolves_to": {"family": "llama"}, "source_url": "` + hfLiveURLPrefix + repo + `"},
    {"value": "` + repo + `", "resolves_to": {"family": "llama"}, "source_url": "` + hfLiveURLPrefix + repo + `"}
  ]
}`)
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: a duplicate org/repo in the fetch-owned set is a bot bug")
	}
}

// §11 case-3: ONE (Value, Scheme, ResolvesTo) triple attested by BOTH a curated
// nomen_claims.json claim AND the HF bot coalesces into ONE Nomen carrying TWO
// NomenAttestations with distinct Source (curated + huggingface) and Method
// (curated + harvested). This is the genuinely-new multi-attestation union the
// harvested layer enables, proven here on constructed input.
func TestHFNomina_CuratedPlusHarvested_CoalesceUnion(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"

	// Harvested side: minted by the real loader over a synthetic seed.
	tbl, err := parseHFNomina(hfSeedBytes(repo, "llama", `, "version": "3.3", "param_size": "70b", "modifier": ["instruct"]`, hfLiveURLPrefix+repo))
	if err != nil {
		t.Fatalf("parseHFNomina: %v", err)
	}
	harvested := tbl.nomina[0]

	// Curated side: same triple (value/scheme/entity), a distinct attester (an
	// archive snapshot read from the curated ingest, Method=Curated).
	curated := Nomen{
		Value:      harvested.Value,
		Scheme:     NomenSchemeHuggingFace,
		Status:     AcceptabilityAdmitted, // SAME Status — differing Status would be a LOUD conflict, not a union
		ResolvesTo: harvested.ResolvesTo,
		Attestations: []NomenAttestation{{
			SourceURL: "https://web.archive.org/web/20260718133241/https://huggingface.co/" + repo,
			Source:    DataSourceCurated,
			Authority: AuthorityPrimary,
			Method:    IngestMethodCurated,
		}},
	}

	coalesced, err := coalesceNomina([]Nomen{curated, harvested})
	if err != nil {
		t.Fatalf("coalesceNomina: %v", err)
	}
	if len(coalesced) != 1 {
		t.Fatalf("coalesced to %d nomina, want 1 (same triple unions, never duplicates)", len(coalesced))
	}
	n := coalesced[0]
	if len(n.Attestations) != 2 {
		t.Fatalf("coalesced nomen carries %d attestations, want 2 (curated + harvested union)", len(n.Attestations))
	}
	var sawCurated, sawHarvested bool
	for _, at := range n.Attestations {
		if at.Source == DataSourceCurated && at.Method == IngestMethodCurated {
			sawCurated = true
		}
		if at.Source == DataSourceHuggingFace && at.Method == IngestMethodHarvested {
			sawHarvested = true
		}
	}
	if !sawCurated || !sawHarvested {
		t.Errorf("union attestations missing a leg: sawCurated=%v sawHarvested=%v (%+v)", sawCurated, sawHarvested, n.Attestations)
	}
	// Idempotent: re-coalescing the same set is byte-stable (no duplicate attestation).
	again, err := coalesceNomina([]Nomen{curated, harvested})
	if err != nil {
		t.Fatalf("coalesceNomina (again): %v", err)
	}
	if len(again[0].Attestations) != 2 {
		t.Errorf("re-coalesce changed the attestation count to %d, want 2 (idempotent dedup)", len(again[0].Attestations))
	}
}

// The empty committed seed loads cleanly (graceful) and mints no HF nomina — the
// inert-until-populated state the ValidateHFNomina guard also accepts.
func TestLoadHFNomina_EmbeddedSeedLoads(t *testing.T) {
	if err := ValidateHFNomina(); err != nil {
		t.Fatalf("ValidateHFNomina over the committed seed: %v", err)
	}
	// Every embedded harvested nomen (if any) is huggingface-scheme + harvested.
	for _, n := range hfNominaClaims() {
		if n.Scheme != NomenSchemeHuggingFace {
			t.Errorf("embedded HF nomen %q has scheme %v, want huggingface", n.Value, n.Scheme)
		}
		if len(n.Attestations) == 0 || n.Attestations[0].Method != IngestMethodHarvested {
			t.Errorf("embedded HF nomen %q must carry a harvested attestation", n.Value)
		}
	}
}
