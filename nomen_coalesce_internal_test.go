package bestiary

import "testing"

// coalesceRef is the shared throwaway entity a coalesce fixture resolves to. Its exact
// identity is irrelevant to the union mechanics under test — only that the fixtures
// share it so they share a triple.
var coalesceRef = EntityRef{Family: "grok", Version: "4.20", Modifier: []string{"reasoning"}}

// TestCoalesceNomina_UnionsSameTripleAttestations is the REQUIRED constructed-input
// falsifier for the v0.2.8 multi-attestation lift (the datasetIngestedFrom single-row
// lesson): shipped single-source data has no duplicate triples, so it can never
// exercise the union path. This drives coalesceNomina over TWO same-triple attestations
// from DISTINCT sources and asserts exactly ONE Nomen with TWO deterministically-sorted
// attestations and no conflict. It also pins determinism: a reversed input order yields
// byte-identical output.
func TestCoalesceNomina_UnionsSameTripleAttestations(t *testing.T) {
	// Same (Value, Scheme, ResolvesTo) triple, same Status, DISTINCT sources — e.g. a
	// huggingface-scheme name asserted by both a curated claim and (a stand-in for) the
	// HF bot. Sources: "curated" and "models.dev".
	mint := func() []Nomen {
		return []Nomen{
			{Value: "org/repo", Scheme: NomenSchemeHuggingFace, Status: AcceptabilityAdmitted, ResolvesTo: coalesceRef,
				Attestations: []NomenAttestation{{SourceURL: "https://hub", Source: DataSourceModelsDev, Authority: AuthorityPrimary, Method: IngestMethodHarvested}}},
			{Value: "org/repo", Scheme: NomenSchemeHuggingFace, Status: AcceptabilityAdmitted, ResolvesTo: coalesceRef,
				Attestations: []NomenAttestation{{SourceURL: "https://claim", Source: DataSourceCurated, Authority: AuthoritySecondary, Method: IngestMethodCurated}}},
		}
	}

	out, err := coalesceNomina(mint())
	if err != nil {
		t.Fatalf("coalesceNomina returned an error on a same-triple/same-Status union: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("coalesced to %d nomina, want exactly 1 (one triple)", len(out))
	}
	if got := len(out[0].Attestations); got != 2 {
		t.Fatalf("coalesced nomen carries %d attestations, want exactly 2 (the union)", got)
	}
	// The TOTAL sort key orders on Source first: "curated" < "models.dev".
	if out[0].Attestations[0].Source != DataSourceCurated || out[0].Attestations[1].Source != DataSourceModelsDev {
		t.Errorf("attestations not sorted by the total key (Source first): got %q then %q, want curated then models.dev",
			out[0].Attestations[0].Source, out[0].Attestations[1].Source)
	}

	// Determinism (INV3): reversing the input order must yield byte-identical output —
	// the union order is fixed by the total sort key, not by input/map iteration order.
	rev := mint()
	rev[0], rev[1] = rev[1], rev[0]
	out2, err := coalesceNomina(rev)
	if err != nil {
		t.Fatalf("coalesceNomina (reversed input) errored: %v", err)
	}
	if len(out2) != 1 || len(out2[0].Attestations) != 2 {
		t.Fatalf("reversed-input coalesce shape differs: %d nomina, %d attestations", len(out2), len(out2[0].Attestations))
	}
	for i := range out[0].Attestations {
		if out[0].Attestations[i] != out2[0].Attestations[i] {
			t.Fatalf("coalesce nondeterministic under input reorder at attestation %d: %+v vs %+v",
				i, out[0].Attestations[i], out2[0].Attestations[i])
		}
	}
}

// TestCoalesceNomina_IdempotentDuplicate pins the idempotent leg: a BYTE-IDENTICAL
// duplicate attestation collapses to one — coalescing the same input twice, or an
// exact duplicate within one call, yields one Nomen with a single attestation (no
// double-count).
func TestCoalesceNomina_IdempotentDuplicate(t *testing.T) {
	at := NomenAttestation{SourceURL: "https://a", Source: DataSourceCurated, Authority: AuthorityPrimary, Method: IngestMethodCurated}
	n := Nomen{Value: "grok-beta", Scheme: NomenSchemeAlias, Status: AcceptabilityAdmitted, ResolvesTo: coalesceRef, Attestations: []NomenAttestation{at}}

	out, err := coalesceNomina([]Nomen{n, n})
	if err != nil {
		t.Fatalf("coalesceNomina errored on an idempotent duplicate: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("idempotent duplicate coalesced to %d nomina, want 1", len(out))
	}
	if got := len(out[0].Attestations); got != 1 {
		t.Fatalf("byte-identical duplicate attestation not deduped: %d attestations, want 1", got)
	}
	if out[0].Attestations[0] != at {
		t.Errorf("deduped attestation = %+v, want the original %+v", out[0].Attestations[0], at)
	}
}

// TestCoalesceNomina_StatusConflictLoud exercises the engine's LOUD path directly: two
// same-triple records disagreeing on Status is a conflict coalesceNomina must reject
// (Status is the single editorial judgment per name), while differing attesters are not.
func TestCoalesceNomina_StatusConflictLoud(t *testing.T) {
	mk := func(status AcceptabilityRating, url string) Nomen {
		return Nomen{Value: "grok-beta", Scheme: NomenSchemeAlias, Status: status, ResolvesTo: coalesceRef,
			Attestations: []NomenAttestation{{SourceURL: url, Source: DataSourceCurated, Authority: AuthorityPrimary, Method: IngestMethodCurated}}}
	}
	_, err := coalesceNomina([]Nomen{mk(AcceptabilityAdmitted, "https://a"), mk(AcceptabilityPreferred, "https://b")})
	if err == nil {
		t.Fatal("coalesceNomina accepted a same-triple Status disagreement; want a loud conflict")
	}
}
