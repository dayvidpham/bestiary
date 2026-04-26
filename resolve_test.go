package bestiary_test

import (
	"errors"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestResolve_ExactID_CrossProviderHosting verifies that an exact raw model ID
// shared by multiple providers returns []ModelRef with err==nil (cross-provider hosting).
func TestResolve_ExactID_CrossProviderHosting(t *testing.T) {
	// claude-opus-4-20250514 is hosted by multiple providers in the static registry.
	refs, err := bestiary.Resolve("claude-opus-4-20250514")
	if err != nil {
		t.Fatalf("Resolve(\"claude-opus-4-20250514\") returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve(\"claude-opus-4-20250514\") returned empty slice, want >=1")
	}
	// All returned refs must have the same model ID.
	for _, r := range refs {
		if r.ID != "claude-opus-4-20250514" {
			t.Errorf("Resolve returned ref with ID=%q, want %q", r.ID, "claude-opus-4-20250514")
		}
	}
	// Multiple providers should be present.
	providersSeen := make(map[bestiary.Provider]struct{})
	for _, r := range refs {
		providersSeen[r.Provider] = struct{}{}
	}
	if len(providersSeen) < 2 {
		t.Errorf("Resolve(\"claude-opus-4-20250514\") returned only 1 provider, want cross-provider hosting (>=2)")
	}
	// Anthropic must be among them.
	if _, ok := providersSeen[bestiary.ProviderAnthropic]; !ok {
		t.Errorf("Resolve(\"claude-opus-4-20250514\"): ProviderAnthropic not in results; providers=%v", providersSeen)
	}
}

// TestResolve_Ambiguous_MultipleCanonicals verifies that an input matching
// multiple distinct canonical triples returns *ErrAmbiguous.
func TestResolve_Ambiguous_MultipleCanonicals(t *testing.T) {
	// "claude" as a canonical family name matches claude/opus, claude/sonnet,
	// claude/haiku, etc. — multiple distinct canonical triples.
	_, err := bestiary.Resolve("claude", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err == nil {
		t.Fatal("Resolve(\"claude\", SchemeCanonical) returned nil error, want *ErrAmbiguous")
	}
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("Resolve(\"claude\", SchemeCanonical) returned error %T, want *ErrAmbiguous", err)
	}
	if ambig.Input != "claude" {
		t.Errorf("ErrAmbiguous.Input = %q, want %q", ambig.Input, "claude")
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf("ErrAmbiguous.Candidates len=%d, want >=2 distinct canonicals", len(ambig.Candidates))
	}
	// Candidates must be distinct canonical triples.
	seen := make(map[string]struct{})
	for _, c := range ambig.Candidates {
		key := string(c.Family) + "/" + c.Variant + "@" + c.Date
		if _, dup := seen[key]; dup {
			t.Errorf("ErrAmbiguous.Candidates contains duplicate canonical %q", key)
		}
		seen[key] = struct{}{}
	}
}

// TestResolve_NotFound verifies that an unknown model ID returns *ErrNotFound.
func TestResolve_NotFound(t *testing.T) {
	_, err := bestiary.Resolve("totally-unknown-model-xyz-99999")
	if err == nil {
		t.Fatal("Resolve(unknown) returned nil error, want *ErrNotFound")
	}
	var notFound *bestiary.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("Resolve(unknown) returned error %T, want *ErrNotFound", err)
	}
	if notFound.What != "model" {
		t.Errorf("ErrNotFound.What = %q, want %q", notFound.What, "model")
	}
}

// TestResolve_WithSchemeRaw_ExactMatch verifies that WithScheme(SchemeRaw)
// performs an exact model ID match.
func TestResolve_WithSchemeRaw_ExactMatch(t *testing.T) {
	refs, err := bestiary.Resolve("claude-opus-4-20250514", bestiary.WithScheme(bestiary.SchemeRaw))
	if err != nil {
		t.Fatalf("Resolve with SchemeRaw returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve with SchemeRaw returned empty slice")
	}
	for _, r := range refs {
		if r.ID != "claude-opus-4-20250514" {
			t.Errorf("Resolve SchemeRaw: ref.ID = %q, want %q", r.ID, "claude-opus-4-20250514")
		}
	}
}

// TestResolve_WithSchemeRaw_NoMatch verifies that SchemeRaw with a partial ID
// returns ErrNotFound (exact match only, no substring).
func TestResolve_WithSchemeRaw_NoMatch(t *testing.T) {
	// Partial prefix should not match under SchemeRaw.
	_, err := bestiary.Resolve("claude-opus", bestiary.WithScheme(bestiary.SchemeRaw))
	if err == nil {
		t.Fatal("Resolve with SchemeRaw partial ID returned nil error, want ErrNotFound")
	}
	var notFound *bestiary.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("Resolve SchemeRaw partial: error %T, want *ErrNotFound", err)
	}
}

// TestResolve_AutoDetect_RawID verifies that auto-detection treats a plain model
// ID without slashes as SchemeRaw.
func TestResolve_AutoDetect_RawID(t *testing.T) {
	refs, err := bestiary.Resolve("claude-opus-4-20250514")
	if err != nil {
		t.Fatalf("Resolve auto-detect returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve auto-detect returned empty slice")
	}
}

// TestResolve_AutoDetect_HuggingFaceForm verifies that "provider/id" two-segment
// form is treated as SchemeHuggingFace (strips provider prefix).
func TestResolve_AutoDetect_HuggingFaceForm(t *testing.T) {
	refs, err := bestiary.Resolve("anthropic/claude-opus-4-20250514")
	if err != nil {
		t.Fatalf("Resolve auto-detect HuggingFace form returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve auto-detect HuggingFace form returned empty slice")
	}
	// At least Anthropic should be in results.
	found := false
	for _, r := range refs {
		if r.ID == "claude-opus-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Resolve auto-detect HuggingFace form: claude-opus-4-20250514 not in results")
	}
}

// TestResolve_AutoDetect_PURLForm verifies that "pkg:huggingface/provider/id"
// form is treated as SchemePURL (strips pkg:huggingface/ prefix).
func TestResolve_AutoDetect_PURLForm(t *testing.T) {
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514")
	if err != nil {
		t.Fatalf("Resolve auto-detect PURL form returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve auto-detect PURL form returned empty slice")
	}
	found := false
	for _, r := range refs {
		if r.ID == "claude-opus-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Resolve auto-detect PURL form: claude-opus-4-20250514 not in results")
	}
}

// TestResolve_WithSchemeHuggingFace verifies that WithScheme(SchemeHuggingFace)
// strips the provider prefix and matches by raw ID.
// When the model is absent from the static registry the error must be *ErrNotFound
// (not *ErrAmbiguous). When it is present every returned ref must carry the expected
// raw model ID.
func TestResolve_WithSchemeHuggingFace(t *testing.T) {
	refs, err := bestiary.Resolve("openai/gpt-4o-2024-08-06", bestiary.WithScheme(bestiary.SchemeHuggingFace))
	if err != nil {
		// Model may be absent; assert the error is ErrNotFound, not ErrAmbiguous.
		var notFound *bestiary.ErrNotFound
		if !errors.As(err, &notFound) {
			var ambig *bestiary.ErrAmbiguous
			if errors.As(err, &ambig) {
				t.Fatalf("Resolve SchemeHuggingFace returned ErrAmbiguous, want ErrNotFound when model absent")
			}
			t.Fatalf("Resolve SchemeHuggingFace returned unexpected error type %T: %v", err, err)
		}
		return
	}
	for _, r := range refs {
		if r.ID != "gpt-4o-2024-08-06" {
			t.Errorf("Resolve SchemeHuggingFace: ref.ID = %q, want %q", r.ID, "gpt-4o-2024-08-06")
		}
	}
}

// TestResolve_WithSchemePURL verifies that WithScheme(SchemePURL) strips the
// PURL prefix and matches by raw ID.
func TestResolve_WithSchemePURL(t *testing.T) {
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514",
		bestiary.WithScheme(bestiary.SchemePURL))
	if err != nil {
		t.Fatalf("Resolve SchemePURL returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve SchemePURL returned empty slice")
	}
	found := false
	for _, r := range refs {
		if r.ID == "claude-opus-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Resolve SchemePURL: claude-opus-4-20250514 not in results")
	}
}

// TestResolve_AllStaticModels_ByRawID verifies that every static model can be
// resolved by its exact ID without error.
func TestResolve_AllStaticModels_ByRawID(t *testing.T) {
	// Collect unique model IDs to avoid N^2 resolve calls.
	seen := make(map[bestiary.ModelID]struct{})
	for _, m := range bestiary.StaticModels() {
		if _, already := seen[m.ID]; already {
			continue
		}
		seen[m.ID] = struct{}{}
		refs, err := bestiary.Resolve(string(m.ID), bestiary.WithScheme(bestiary.SchemeRaw))
		if err != nil {
			t.Errorf("Resolve(SchemeRaw, %q) returned error: %v", m.ID, err)
			continue
		}
		if len(refs) == 0 {
			t.Errorf("Resolve(SchemeRaw, %q) returned empty slice", m.ID)
		}
	}
}

// TestResolve_CanonicalFamily_ReturnsErrAmbiguous verifies that resolving by
// canonical family name without a specific variant triggers ErrAmbiguous when
// multiple distinct canonicals match.
func TestResolve_CanonicalFamily_ReturnsErrAmbiguous(t *testing.T) {
	// "gpt" is a shared family across many GPT model variants.
	_, err := bestiary.Resolve("gpt", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err == nil {
		t.Fatal("Resolve(\"gpt\", SchemeCanonical) returned nil error, want *ErrAmbiguous")
	}
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("Resolve(\"gpt\", SchemeCanonical) returned %T, want *ErrAmbiguous", err)
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf("ErrAmbiguous.Candidates = %d, want >=2 for family 'gpt'", len(ambig.Candidates))
	}
}

// TestResolve_ExactIDInCanonicalMode_NotAmbiguous is a regression test for the
// false-ambiguity bug: when an exact model ID is resolved with
// WithScheme(SchemeCanonical), providers that omit the family field can produce
// a different NormalizedVariant ("" vs "opus") for the same model. This must
// NOT trigger ErrAmbiguous — the input is unambiguous because the model ID is
// exact. The fix groups by model ID (not canonical triple) when all matches
// share the same raw ID.
//
// Regression: bestiary-tant (Reviewer A, cycle 2).
func TestResolve_ExactIDInCanonicalMode_NotAmbiguous(t *testing.T) {
	// claude-opus-4-20250514 exhibits the divergent-variant pattern: some
	// providers (e.g. anthropic, jiekou) have NormalizedVariant="opus" while
	// others (e.g. nano-gpt, 302ai) have NormalizedVariant="" because they do
	// not populate the API family field.
	refs, err := bestiary.Resolve("claude-opus-4-20250514", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err != nil {
		t.Fatalf("Resolve(\"claude-opus-4-20250514\", SchemeCanonical) returned error (false ambiguity): %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve(\"claude-opus-4-20250514\", SchemeCanonical) returned empty slice")
	}
	// Every returned ref must have the exact input ID.
	for _, r := range refs {
		if r.ID != "claude-opus-4-20250514" {
			t.Errorf("ref.ID = %q, want \"claude-opus-4-20250514\"", r.ID)
		}
	}
	// Multiple providers must be present (cross-provider hosting).
	providers := make(map[bestiary.Provider]struct{})
	for _, r := range refs {
		providers[r.Provider] = struct{}{}
	}
	if len(providers) < 2 {
		t.Errorf("Resolve returned only %d provider(s), want cross-provider hosting (>=2)", len(providers))
	}
	// Anthropic must be among them.
	if _, ok := providers[bestiary.ProviderAnthropic]; !ok {
		t.Errorf("ProviderAnthropic not in results; providers seen: %v", providers)
	}
}

// TestResolve_SchemeCanonical_NeverAutoDetected_detectScheme documents that
// detectScheme never returns SchemeCanonical for a plain family-name input (no
// slashes, no @ separator). The scheme detection boundary is unchanged: only
// inputs with 1–3 "/" separators AND an "@" date or versioned token auto-detect
// as SchemeCanonical. "claude" has no slashes, so detectScheme returns SchemeRaw.
//
// However, Resolve itself applies a bare-family fallback: when SchemeRaw returns
// zero matches, it retries with SchemeCanonical family-only matching and returns
// *ErrAmbiguous when multiple distinct family entries match. This is distinct from
// auto-detecting SchemeCanonical in detectScheme — it is a post-match fallback.
//
// B4 (SLICE-FIX-2): Resolve("claude") must return *ErrAmbiguous (not ErrNotFound).
func TestResolve_SchemeCanonical_NeverAutoDetected_detectScheme(t *testing.T) {
	// "claude" has no slashes — detectScheme returns SchemeRaw (unchanged).
	// But Resolve's bare-family fallback applies, so the result is ErrAmbiguous.
	_, err := bestiary.Resolve("claude")
	if err == nil {
		t.Fatal("Resolve(\"claude\") returned nil error; want *ErrAmbiguous (bare-family fallback)")
	}
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("Resolve(\"claude\") returned error type %T (%v); want *ErrAmbiguous", err, err)
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf("ErrAmbiguous.Candidates len=%d, want >=2 (bare-family fallback must find multiple claude variants)", len(ambig.Candidates))
	}
}

// TestResolve_CanonicalAutoDetect verifies that detectScheme recognizes
// "provider/family/variant@date" form and auto-detects SchemeCanonical.
//
// B1/B2 (SLICE-FIX-2): Resolve("claude/opus@2025-11-01") must return non-empty
// refs (or *ErrAmbiguous when multi-provider) — NEVER ErrNotFound.
func TestResolve_CanonicalAutoDetect(t *testing.T) {
	refs, err := bestiary.Resolve("claude/opus@2025-11-01")
	if err == nil {
		// Single-group result: refs returned without error.
		if len(refs) == 0 {
			t.Fatal("Resolve(\"claude/opus@2025-11-01\") returned empty refs with nil error")
		}
		return
	}
	// *ErrAmbiguous is acceptable when multiple distinct canonical triples match.
	var ambig *bestiary.ErrAmbiguous
	if errors.As(err, &ambig) {
		if len(ambig.Candidates) == 0 {
			t.Fatal("Resolve(\"claude/opus@2025-11-01\") returned ErrAmbiguous with empty candidates")
		}
		return
	}
	// ErrNotFound is the bug — must not be returned.
	var notFound *bestiary.ErrNotFound
	if errors.As(err, &notFound) {
		t.Fatalf("Resolve(\"claude/opus@2025-11-01\") returned ErrNotFound; "+
			"detectScheme must recognize canonical form (family/variant@date) and NOT fall back to raw-ID lookup")
	}
	t.Fatalf("Resolve(\"claude/opus@2025-11-01\") returned unexpected error %T: %v", err, err)
}

// TestResolve_PURLProviderFilter verifies that the provider segment in a PURL
// input is retained as a filter hint and applied to match results.
//
// B3 (SLICE-FIX-2): Resolve("pkg:huggingface/anthropic/claude-opus-4-5") must
// return ONLY refs where Provider == "anthropic". It must never return refs from
// other providers that also host the same model ID.
func TestResolve_PURLProviderFilter(t *testing.T) {
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-5")
	if err != nil {
		// If the model doesn't exist under anthropic exactly, ErrNotFound is
		// acceptable. What is NOT acceptable is zero-Anthropic results in a non-nil refs.
		var notFound *bestiary.ErrNotFound
		if errors.As(err, &notFound) {
			t.Skipf("claude-opus-4-5 not in static registry under anthropic; skipping provider-filter assertion")
		}
		t.Fatalf("Resolve PURL provider filter returned unexpected error %T: %v", err, err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve(\"pkg:huggingface/anthropic/claude-opus-4-5\") returned empty refs")
	}
	// Every returned ref must be from Anthropic — the PURL provider filter must apply.
	for _, r := range refs {
		if r.Provider != bestiary.ProviderAnthropic {
			t.Errorf("Resolve PURL provider filter: got ref with Provider=%q, want %q only; "+
				"PURL provider segment must filter results",
				r.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// TestResolve_BareFamilyAmbiguous verifies that Resolve("claude") returns
// *ErrAmbiguous with a non-empty candidate list — not ErrNotFound.
//
// B4 (SLICE-FIX-2): bare family names should fall back to SchemeCanonical
// family-only matching and surface ErrAmbiguous when multiple variants match.
func TestResolve_BareFamilyAmbiguous(t *testing.T) {
	_, err := bestiary.Resolve("claude")
	if err == nil {
		t.Fatal("Resolve(\"claude\") returned nil error; want *ErrAmbiguous")
	}
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		var notFound *bestiary.ErrNotFound
		if errors.As(err, &notFound) {
			t.Fatalf("Resolve(\"claude\") returned ErrNotFound; "+
				"bare-family fallback must return *ErrAmbiguous when multiple claude variants exist, "+
				"not ErrNotFound — fix: add SchemeCanonical fallback in Resolve after SchemeRaw returns empty")
		}
		t.Fatalf("Resolve(\"claude\") returned unexpected error type %T: %v", err, err)
	}
	if ambig.Input != "claude" {
		t.Errorf("ErrAmbiguous.Input = %q, want %q", ambig.Input, "claude")
	}
	if len(ambig.Candidates) < 2 {
		t.Errorf("ErrAmbiguous.Candidates len=%d, want >=2 distinct claude variants", len(ambig.Candidates))
	}
}

// TestResolve_ErrAmbiguous_SchemePropagated verifies that ErrAmbiguous.Scheme
// reflects the scheme used during resolution.
func TestResolve_ErrAmbiguous_SchemePropagated(t *testing.T) {
	_, err := bestiary.Resolve("claude", bestiary.WithScheme(bestiary.SchemeCanonical))
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		t.Fatalf("expected *ErrAmbiguous, got %T", err)
	}
	if ambig.Scheme != bestiary.SchemeCanonical {
		t.Errorf("ErrAmbiguous.Scheme = %v, want SchemeCanonical", ambig.Scheme)
	}
}
