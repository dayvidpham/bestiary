package bestiary_test

import (
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestFormatPURL_HuggingFaceProvider_RepoPath pins the RESTRICTED purl render against a
// REAL catalog row: for a huggingface-provider instance the raw model ID already IS the
// org/repo path, so the purl is "pkg:huggingface/<org>/<repo>" — the purl-spec shape
// (type/namespace/name), with no invented segment.
func TestFormatPURL_HuggingFaceProvider_RepoPath(t *testing.T) {
	const rawID = "meta-llama/Llama-3.3-70B-Instruct"
	m, ok := bestiary.LookupModelByProvider(bestiary.ProviderHuggingFace, rawID)
	if !ok {
		t.Fatalf("catalog drift: %q is no longer a huggingface-provider row", rawID)
	}
	got := m.Ref().Format(bestiary.SchemePURL)
	want := "pkg:huggingface/" + rawID
	if got != want {
		t.Errorf("Format(SchemePURL) = %q, want %q", got, want)
	}
}

// TestFormatPURL_NonHuggingFaceProvider_Empty is the RESTRICT fence: a purl is a foreign
// key into someone else's registry, so bestiary only mints one where the artifact's
// registry home is actually known. An anthropic-served ref has no HuggingFace repo, so
// the render is EMPTY rather than the old (spec-invalid) "pkg:huggingface/anthropic/...".
func TestFormatPURL_NonHuggingFaceProvider_Empty(t *testing.T) {
	ref := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	if got := ref.Format(bestiary.SchemePURL); got != "" {
		t.Errorf("Format(SchemePURL) for a non-HuggingFace provider = %q, want \"\"", got)
	}
}

// TestFormatPURL_NoDoubleHuggingFace is the specific regression guard for the bug the
// restrict fixes: the old render interpolated the PROVIDER as the purl namespace, which
// for HuggingFace's own rows produced "pkg:huggingface/huggingface/<org>/<repo>".
func TestFormatPURL_NoDoubleHuggingFace(t *testing.T) {
	for _, m := range bestiary.ModelsByProvider(bestiary.ProviderHuggingFace) {
		got := m.Ref().Format(bestiary.SchemePURL)
		if got == "" {
			t.Errorf("model %q: huggingface-provider row rendered an empty purl", m.ID)
			continue
		}
		if want := "pkg:huggingface/" + string(m.ID); got != want {
			t.Errorf("model %q: purl = %q, want %q", m.ID, got, want)
		}
	}
}

// TestDesignations_DropPURLForNonHuggingFace verifies the read projection follows the
// render: a designation with an empty value would be a lie ("this model is named ”"),
// so the purl entry is DROPPED entirely for a non-HuggingFace ref (3 designations) and
// PRESENT for a HuggingFace one (4). The canonical designation stays Preferred either way.
func TestDesignations_DropPURLForNonHuggingFace(t *testing.T) {
	nonHF := bestiary.ModelRef{
		ID:       "claude-opus-4-20250514",
		Provider: bestiary.ProviderAnthropic,
		Family:   "claude",
		Variant:  "opus",
		Date:     "2025-05-14",
	}
	ds := nonHF.Designations()
	if len(ds) != 3 {
		t.Errorf("non-HuggingFace Designations() = %d entries, want 3 (raw, canonical, huggingface — no purl)", len(ds))
	}
	for _, d := range ds {
		if d.Scheme == bestiary.SchemePURL {
			t.Errorf("non-HuggingFace Designations() still carries a purl entry: %+v", d)
		}
		if d.Value == "" {
			t.Errorf("Designations() carries an empty-valued entry: %+v", d)
		}
	}

	hf, ok := bestiary.LookupModelByProvider(bestiary.ProviderHuggingFace, "meta-llama/Llama-3.3-70B-Instruct")
	if !ok {
		t.Fatal("catalog drift: meta-llama/Llama-3.3-70B-Instruct is no longer a huggingface-provider row")
	}
	hfDs := hf.Ref().Designations()
	if len(hfDs) != 4 {
		t.Errorf("HuggingFace Designations() = %d entries, want 4 (raw, canonical, huggingface, purl)", len(hfDs))
	}
	var sawPURL bool
	for _, d := range hfDs {
		if d.Scheme == bestiary.SchemePURL {
			sawPURL = true
			if d.Rating != bestiary.AcceptabilityAdmitted {
				t.Errorf("purl designation rating = %v, want admitted", d.Rating)
			}
		}
		if d.Scheme == bestiary.SchemeCanonical && d.Rating != bestiary.AcceptabilityPreferred {
			t.Errorf("canonical designation rating = %v, want preferred", d.Rating)
		}
	}
	if !sawPURL {
		t.Error("HuggingFace Designations() is missing the purl entry")
	}
}

// TestDesignations_NeverEmptyValued is the corpus-wide fence for the drop rule: across
// the whole committed registry no designation is ever emitted with an empty value.
func TestDesignations_NeverEmptyValued(t *testing.T) {
	for _, m := range bestiary.StaticModels() {
		for _, d := range m.Ref().Designations() {
			if d.Value == "" {
				t.Fatalf("model %q emitted an empty-valued designation for scheme %v", m.ID, d.Scheme)
			}
		}
	}
}

// TestResolve_PURLInputStaysLenient guards the Postel split ratified for this change: the
// RENDER side is restricted to HuggingFace, but the INPUT side is untouched — a
// "pkg:huggingface/<provider>/<id>" string (the shape bestiary itself used to emit, and
// the shape a downstream SBOM may still hold) must keep resolving.
func TestResolve_PURLInputStaysLenient(t *testing.T) {
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-1", bestiary.WithScheme(bestiary.SchemePURL))
	if err != nil {
		t.Fatalf("Resolve of a legacy provider-namespaced purl failed: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve of a legacy provider-namespaced purl returned no refs")
	}
	if refs[0].ID != "claude-opus-4-1" {
		t.Errorf("Resolve returned ID %q, want claude-opus-4-1", refs[0].ID)
	}
}
