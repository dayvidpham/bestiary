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

// The harvested layer accepts a NON-base family at parse (a community finetune's
// repo legitimately names a real catalog entity whose family is not in
// curatedBaseFamilies — e.g. codellama/hermes/qwq). Only an EMPTY family is a
// parse-level reject; the real-entity guard is the codegen entity FK check below.
func TestParseHFNomina_EmptyFamily_MustFail(t *testing.T) {
	const repo = "some-org/some-model"
	raw := hfSeedBytes(repo, "", "", hfLiveURLPrefix+repo)
	if _, err := parseHFNomina(raw); err == nil {
		t.Fatalf("want rejection: resolves_to.family must be non-empty (an entity key needs a family)")
	}
}

func TestParseHFNomina_NonBaseFamily_ParsesButFKCatchesOrphan(t *testing.T) {
	const repo = "some-org/some-model"
	// A non-base family parses (harvested layer is not restricted to base families).
	raw := hfSeedBytes(repo, "codellama", `, "param_size": "7b"`, hfLiveURLPrefix+repo)
	tbl, err := parseHFNomina(raw)
	if err != nil {
		t.Fatalf("parseHFNomina rejected a non-base family; the harvested layer must accept it: %v", err)
	}
	// The entity FK guard rejects a nomen resolving to no real entity (constructed
	// orphan: nothing resolves).
	if err := validateHFEntityFKs(tbl.nomina, func(string) bool { return false }); err == nil {
		t.Fatalf("want rejection: a harvested nomen resolving to no entity is an orphan")
	}
	// ... and accepts it when the entity exists.
	if err := validateHFEntityFKs(tbl.nomina, func(string) bool { return true }); err != nil {
		t.Errorf("entity FK check must accept a nomen that resolves: %v", err)
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

// hfSeedBytesArchived builds a harvested-seed document carrying an archived_url.
func hfSeedBytesArchived(value, family, sourceURL, archivedURL string) []byte {
	return []byte(`{
  "schema_version": 1,
  "nomina": [
    {"value": "` + value + `", "resolves_to": {"family": "` + family + `"}, "source_url": "` + sourceURL + `", "archived_url": "` + archivedURL + `"}
  ]
}`)
}

// The carrier chain's middle link: a seed archived_url reaches
// NomenAttestation.ArchivedURL, and it sits BESIDE the live source_url rather than
// replacing it. Without this the bot could write the field and nothing would read it.
func TestParseHFNomina_ArchivedURL_CarriedOntoAttestation(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	live := hfLiveURLPrefix + repo
	snap := "https://web.archive.org/web/20260715030540/" + live
	tbl, err := parseHFNomina(hfSeedBytesArchived(repo, "llama", live, snap))
	if err != nil {
		t.Fatalf("parseHFNomina: %v", err)
	}
	if len(tbl.nomina) != 1 || len(tbl.nomina[0].Attestations) != 1 {
		t.Fatalf("got %+v, want 1 nomen with 1 attestation", tbl.nomina)
	}
	at := tbl.nomina[0].Attestations[0]
	if at.ArchivedURL != snap {
		t.Errorf("attestation ArchivedURL = %q, want %q (the seed's archived_url must reach the attestation)", at.ArchivedURL, snap)
	}
	if at.SourceURL != live {
		t.Errorf("attestation SourceURL = %q, want the UNCHANGED live repo URL %q — ArchivedURL is additive, never a replacement", at.SourceURL, live)
	}
}

// archived_url is OPTIONAL: the overwhelmingly common harvested row has no snapshot,
// and its absence must load cleanly as an empty ArchivedURL, never an error.
func TestParseHFNomina_ArchivedURL_AbsentIsValid(t *testing.T) {
	const repo = "BAAI/bge-m3"
	tbl, err := parseHFNomina(hfSeedBytes(repo, "bge", "", hfLiveURLPrefix+repo))
	if err != nil {
		t.Fatalf("parseHFNomina with no archived_url: %v", err)
	}
	if got := tbl.nomina[0].Attestations[0].ArchivedURL; got != "" {
		t.Errorf("ArchivedURL = %q, want \"\" when archived_url is absent", got)
	}
}

// A PRESENT archived_url that is not an archive.org snapshot is LOUD: the field
// exists to be durable evidence, so shipping a malformed one would present an
// unusable citation as a durable one. The check must be the SHARED shape validator,
// which is what keeps the harvested and curated layers from drifting apart.
func TestParseHFNomina_ArchivedURL_MalformedIsLoud(t *testing.T) {
	const repo = "meta-llama/Llama-3.3-70B-Instruct"
	live := hfLiveURLPrefix + repo
	for name, bad := range map[string]string{
		"live page, not a snapshot": live,
		"no capture timestamp":      "https://web.archive.org/web/" + live,
		"short timestamp":           "https://web.archive.org/web/2026/" + live,
		"http scheme on the prefix": "http://web.archive.org/web/20260715030540/" + live,
		"wrong host":                "https://archive.ph/20260715030540/" + live,
	} {
		t.Run(name, func(t *testing.T) {
			if IsArchiveSnapshotURL(bad) {
				t.Fatalf("test premise broken: %q is accepted by the shared shape validator", bad)
			}
			_, err := parseHFNomina(hfSeedBytesArchived(repo, "llama", live, bad))
			if err == nil {
				t.Fatalf("want rejection for archived_url %q", bad)
			}
			if !strings.Contains(err.Error(), "archived_url") {
				t.Errorf("rejection message does not name the offending field: %v", err)
			}
		})
	}
}

// The shared-validator contract itself: the curated fence and the harvested
// archived_url check accept EXACTLY the same shape, because they are the same
// function. A copy of the regex in either place would let the two drift.
func TestIsArchiveSnapshotURL_SharedShape(t *testing.T) {
	const orig = "https://huggingface.co/meta-llama/Llama-3.3-70B-Instruct"
	if !IsArchiveSnapshotURL("https://web.archive.org/web/20260715030540/" + orig) {
		t.Error("a well-formed archive.org snapshot was rejected")
	}
	if !IsArchiveSnapshotURL("https://web.archive.org/web/20260715030540/http://docs.x.ai/models") {
		t.Error("a snapshot of an http:// original was rejected")
	}
	if IsArchiveSnapshotURL("") {
		t.Error("the empty string must not be an archive snapshot")
	}
	if IsArchiveSnapshotURL(orig) {
		t.Error("a live URL must not be an archive snapshot")
	}
	// It is the same function the curated fence applies.
	if IsArchiveSnapshotURL(orig) != archiveSnapshotURL.MatchString(orig) {
		t.Error("IsArchiveSnapshotURL disagrees with the curated fence's regexp; they must be one implementation")
	}
}
