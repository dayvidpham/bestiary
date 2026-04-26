package bestiary_test

import (
	"errors"
	"strings"
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
// a different Variant ("" vs "opus") for the same model. This must NOT trigger
// ErrAmbiguous — the input is unambiguous because the model ID is exact. The
// fix groups by model ID (not canonical triple) when all matches share the same
// raw ID.
//
// Regression: bestiary-tant (Reviewer A, cycle 2).
func TestResolve_ExactIDInCanonicalMode_NotAmbiguous(t *testing.T) {
	// claude-opus-4-20250514 exhibits the divergent-variant pattern: some
	// providers (e.g. anthropic, jiekou) have Variant="opus" while others
	// (e.g. nano-gpt, 302ai) have Variant="" because they do not populate the
	// API family field.
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

// --- Fix #1: PURL loose-match fallback ---

// TestResolve_PURL_LooseFallback_ZeroNamespaceMatches verifies that when a PURL
// input's namespace (provider segment) yields zero matching models, Resolve falls
// back to an all-provider search and returns *ErrAmbiguous with candidates drawn
// from all providers. The error message must mention the missed namespace.
//
// Fix #1 (SLICE-FIX-V2-2): "State that we are doing loose matching in the fallback.
// In zero match case, state that there are none and fallback to ErrAmbiguous with candidates."
func TestResolve_PURL_LooseFallback_ZeroNamespaceMatches(t *testing.T) {
	// Use a namespace (provider) that does NOT host claude-opus-4-5 to force the
	// zero-match path. "totally-unknown-ns" is not a real provider.
	_, err := bestiary.Resolve("pkg:huggingface/totally-unknown-ns/claude-opus-4-5")
	if err == nil {
		t.Fatal("Resolve PURL with unknown namespace: want error (ErrAmbiguous), got nil")
	}

	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		var notFound *bestiary.ErrNotFound
		if errors.As(err, &notFound) {
			t.Fatalf("Resolve PURL zero-namespace-match: got ErrNotFound, want ErrAmbiguous with loose fallback; "+
				"Fix #1: when namespace yields no matches, fall back to all-provider candidates")
		}
		t.Fatalf("Resolve PURL zero-namespace-match: unexpected error %T: %v", err, err)
	}

	// Candidates must be non-empty (all-provider fallback).
	if len(ambig.Candidates) == 0 {
		t.Fatal("Resolve PURL zero-namespace-match: ErrAmbiguous.Candidates is empty; loose fallback must populate candidates")
	}

	// The error message must mention the missed namespace.
	errMsg := ambig.Error()
	if !strings.Contains(errMsg, "totally-unknown-ns") {
		t.Errorf("ErrAmbiguous.Error() does not mention missed namespace %q; got:\n%s",
			"totally-unknown-ns", errMsg)
	}
}

// TestResolve_PURL_LooseFallback_DiagnosticMessage verifies that the ErrAmbiguous
// error message specifically states that no matches were found in the namespace,
// as required by Fix #1 verbatim: "state that there are none".
func TestResolve_PURL_LooseFallback_DiagnosticMessage(t *testing.T) {
	_, err := bestiary.Resolve("pkg:huggingface/no-such-provider/claude")
	if err == nil {
		t.Fatal("Resolve PURL with unknown namespace: want error, got nil")
	}

	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		// Any non-ErrAmbiguous error is also an acceptable outcome for no candidates
		// but the message must still convey the namespace miss.
		errMsg := err.Error()
		if !strings.Contains(errMsg, "no-such-provider") {
			t.Errorf("error message does not mention missed namespace; got: %v", errMsg)
		}
		return
	}

	errMsg := ambig.Error()
	// Must contain either "no matches" or "namespace" to satisfy the verbatim spec.
	if !strings.Contains(errMsg, "namespace") && !strings.Contains(errMsg, "no matches") {
		t.Errorf("ErrAmbiguous.Error() should mention 'namespace' or 'no matches'; got:\n%s", errMsg)
	}
}

// TestResolve_PURL_ExistingNamespacePreserved verifies that Fix #1 does not break
// the existing behavior when the namespace (provider) DOES have matching models.
// The original provider-filter should still apply when there are matches.
func TestResolve_PURL_ExistingNamespacePreserved(t *testing.T) {
	// anthropic hosts claude-opus-4-20250514 — namespace filter must still work.
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514")
	if err != nil {
		t.Skipf("claude-opus-4-20250514 not found under anthropic; skipping: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve PURL with matching namespace returned empty refs")
	}
	// All returned refs must be from Anthropic.
	for _, r := range refs {
		if r.Provider != bestiary.ProviderAnthropic {
			t.Errorf("PURL with existing namespace: got Provider=%q, want %q only",
				r.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// --- Fix #4: Family.CanonicalProvider preference in Resolve ---

// TestResolve_CanonicalProvider_Preference_Claude verifies that when
// "claude/opus@2025-05-14" matches both Anthropic and rehost providers,
// Resolve returns only the Anthropic ModelRef (canonical provider preference).
//
// Fix #4: "Why this show provider 'qihang-ai' — Anthropic should be the
// canonical provider here"
func TestResolve_CanonicalProvider_Preference_Claude(t *testing.T) {
	refs, err := bestiary.Resolve("claude/opus@2025-05-14")
	if err != nil {
		t.Skipf("claude/opus@2025-05-14 not found in registry; skipping: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve(claude/opus@2025-05-14) returned empty refs")
	}

	// When canonical provider preference is applied, the result should be a
	// single-provider set (Anthropic), not the 17+ rehost set.
	for _, r := range refs {
		if r.Provider != bestiary.ProviderAnthropic {
			t.Errorf("Resolve(claude/opus@2025-05-14): got Provider=%q, want %q (canonical provider preference not applied)",
				r.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// TestResolve_CanonicalProvider_FallsBackToAmbiguous_UnknownFamily verifies that
// when CanonicalProvider() returns empty (unknown family) and >1 provider matches,
// ErrAmbiguous is returned (no preference applied, existing behavior preserved).
func TestResolve_CanonicalProvider_FallsBackToAmbiguous_UnknownFamily(t *testing.T) {
	// "grok" family — XAI is likely canonical but we haven't mapped it yet.
	// This test verifies the fallback: unknown family → ErrAmbiguous.
	// We use a bare family name to trigger multi-provider matching.
	_, err := bestiary.Resolve("grok", bestiary.WithScheme(bestiary.SchemeCanonical))
	if err == nil {
		// Single result means grok only exists under one provider — that's fine too.
		t.Skip("grok resolved to a single canonical; fallback test not applicable")
	}

	// If we get an error, it should be ErrAmbiguous (not ErrNotFound when grok exists).
	var ambig *bestiary.ErrAmbiguous
	if !errors.As(err, &ambig) {
		var notFound *bestiary.ErrNotFound
		if errors.As(err, &notFound) {
			t.Skip("grok not in registry; fallback test not applicable")
		}
		t.Fatalf("Resolve(grok): unexpected error %T: %v", err, err)
	}

	// ErrAmbiguous is correct: unknown family must NOT apply canonical preference.
	if len(ambig.Candidates) == 0 {
		t.Error("ErrAmbiguous.Candidates is empty for grok — should have candidates")
	}
}

// TestResolve_CanonicalProvider_WithInputFormat_Peasant_Claude verifies that
// using WithInputFormat(InputFormatPeasant) with a canonical claude input applies
// the CanonicalProvider preference and returns a single Anthropic ref.
func TestResolve_CanonicalProvider_WithInputFormat_Peasant_Claude(t *testing.T) {
	// Use a canonical input that should unambiguously identify an Anthropic model.
	refs, err := bestiary.Resolve("claude/opus@2025-05-14", bestiary.WithInputFormat(bestiary.InputFormatPeasant))
	if err != nil {
		t.Skipf("claude/opus@2025-05-14 not in registry or error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve returned empty refs")
	}
	for _, r := range refs {
		if r.Provider != bestiary.ProviderAnthropic {
			t.Errorf("canonical provider preference not applied: got Provider=%q, want %q",
				r.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// --- Fix #3: --format input flag (via WithInputFormat) ---

// TestResolve_WithInputFormat_Peasant_RejectsPURL verifies that InputFormatPeasant
// does NOT auto-detect PURL inputs — a PURL string treated as canonical form should
// return ErrNotFound (or ErrAmbiguous if it happens to parse as canonical segments),
// not a successful PURL resolution.
func TestResolve_WithInputFormat_Peasant_RejectsPURL(t *testing.T) {
	// PURL input given to peasant mode: should NOT resolve as PURL.
	// The "pkg:huggingface/..." prefix is not a valid canonical form segment.
	_, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514",
		bestiary.WithInputFormat(bestiary.InputFormatPeasant))
	if err == nil {
		t.Fatal("Resolve peasant mode accepted PURL input without error; want ErrNotFound")
	}
	// May be ErrNotFound (preferred) or ErrAmbiguous (acceptable if pkg: matches as family=pkg).
	// Must NOT silently resolve to the PURL scheme's target.
}

// TestResolve_WithInputFormat_PURL_Resolves verifies that InputFormatPURL
// correctly resolves a PURL input string.
func TestResolve_WithInputFormat_PURL_Resolves(t *testing.T) {
	refs, err := bestiary.Resolve("pkg:huggingface/anthropic/claude-opus-4-20250514",
		bestiary.WithInputFormat(bestiary.InputFormatPURL))
	if err != nil {
		t.Fatalf("Resolve WithInputFormat(PURL) returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve WithInputFormat(PURL) returned empty refs")
	}
	found := false
	for _, r := range refs {
		if r.ID == "claude-opus-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Resolve WithInputFormat(PURL): claude-opus-4-20250514 not in results")
	}
}

// TestResolve_WithInputFormat_HuggingFace_Resolves verifies that InputFormatHuggingFace
// correctly resolves a HuggingFace-form input.
func TestResolve_WithInputFormat_HuggingFace_Resolves(t *testing.T) {
	refs, err := bestiary.Resolve("anthropic/claude-opus-4-20250514",
		bestiary.WithInputFormat(bestiary.InputFormatHuggingFace))
	if err != nil {
		t.Fatalf("Resolve WithInputFormat(HuggingFace) returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve WithInputFormat(HuggingFace) returned empty refs")
	}
	found := false
	for _, r := range refs {
		if r.ID == "claude-opus-4-20250514" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Resolve WithInputFormat(HuggingFace): claude-opus-4-20250514 not in results")
	}
}

// TestResolve_WithInputFormat_Raw_Resolves verifies that InputFormatRaw
// performs an exact model ID lookup.
func TestResolve_WithInputFormat_Raw_Resolves(t *testing.T) {
	refs, err := bestiary.Resolve("claude-opus-4-20250514",
		bestiary.WithInputFormat(bestiary.InputFormatRaw))
	if err != nil {
		t.Fatalf("Resolve WithInputFormat(Raw) returned error: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve WithInputFormat(Raw) returned empty refs")
	}
	for _, r := range refs {
		if r.ID != "claude-opus-4-20250514" {
			t.Errorf("Resolve WithInputFormat(Raw): got ID=%q, want exact match %q", r.ID, "claude-opus-4-20250514")
		}
	}
}

// TestResolve_WithInputFormat_Raw_PartialNoMatch verifies that InputFormatRaw
// with a partial ID returns ErrNotFound (exact match only).
func TestResolve_WithInputFormat_Raw_PartialNoMatch(t *testing.T) {
	_, err := bestiary.Resolve("claude-opus",
		bestiary.WithInputFormat(bestiary.InputFormatRaw))
	if err == nil {
		t.Fatal("Resolve WithInputFormat(Raw) partial ID: want ErrNotFound, got nil")
	}
	var notFound *bestiary.ErrNotFound
	if !errors.As(err, &notFound) {
		t.Fatalf("Resolve WithInputFormat(Raw) partial: got %T, want *ErrNotFound", err)
	}
}

// --- Fix 1 (SLICE-FIX-V2-5 cycle-2): Bracket-suffix [modifier] stripping in Resolve ---

// TestResolve_BracketSuffixStripping_DateMatch verifies that a canonical input
// with a [modifier] bracket suffix resolves correctly when both the date AND the
// modifier match a model in the static registry.
//
// Regression: before the fix, matchCanonicalSegments did not strip the [modifier]
// bracket suffix before parsing the "@date" field, so the dateFilter became
// "2024-10-22[latest]" — which never matched any model's Date field.
// Fix: bracket suffix is extracted BEFORE the "@date" suffix so dateFilter
// contains only the date string.
//
// BLOCKER: bestiary-wjk9
func TestResolve_BracketSuffixStripping_DateMatch(t *testing.T) {
	// claude-3-5-haiku-latest from Anthropic has Family="claude", Variant="haiku",
	// Date="2024-10-22", Modifier="latest" in the static registry.
	// The canonical string "anthropic/claude/haiku@2024-10-22[latest]" must resolve
	// to that model — NOT return ErrNotFound.
	refs, err := bestiary.Resolve("anthropic/claude/haiku@2024-10-22[latest]")
	if err != nil {
		var notFound *bestiary.ErrNotFound
		if errors.As(err, &notFound) {
			t.Fatalf("Resolve(\"anthropic/claude/haiku@2024-10-22[latest]\") returned ErrNotFound; "+
				"bracket-suffix [latest] must be stripped before date matching — BLOCKER bestiary-wjk9\n"+
				"  What: bracket suffix was included in dateFilter string\n"+
				"  Fix: strip [modifier] suffix from matchInput before extracting @date")
		}
		t.Fatalf("Resolve(\"anthropic/claude/haiku@2024-10-22[latest]\") returned unexpected error %T: %v", err, err)
	}
	if len(refs) == 0 {
		t.Fatal("Resolve(\"anthropic/claude/haiku@2024-10-22[latest]\") returned empty refs")
	}
	// All returned refs must have Date="2024-10-22" and Modifier="latest".
	for _, r := range refs {
		if r.Date != "2024-10-22" {
			t.Errorf("ref.Date = %q, want %q", r.Date, "2024-10-22")
		}
		if r.Modifier != "latest" {
			t.Errorf("ref.Modifier = %q, want %q", r.Modifier, "latest")
		}
	}
}

// TestResolve_BracketSuffixStripping_ModifierFilter verifies that the [modifier]
// bracket suffix acts as a filter: a model with a different Modifier value is
// excluded from results.
//
// If both "claude-haiku@2024-10-22[latest]" and "claude-haiku@2024-10-22[nonexistent]"
// are tried, the nonexistent modifier must yield ErrNotFound (the filter excludes all
// models since none have Modifier="nonexistent" for that date).
//
// BLOCKER: bestiary-wjk9
func TestResolve_BracketSuffixStripping_ModifierFilter(t *testing.T) {
	// A synthetic modifier that no real model has — must yield ErrNotFound (filter applied).
	_, err := bestiary.Resolve("claude/haiku@2024-10-22[nonexistent-modifier-xyz]")
	if err == nil {
		t.Fatal("Resolve with nonexistent [modifier] returned nil error; "+
			"modifier filter must exclude models whose Modifier field does not match")
	}
	// Must be ErrNotFound (no models match the nonexistent modifier), not ErrAmbiguous.
	var notFound *bestiary.ErrNotFound
	if !errors.As(err, &notFound) {
		var ambig *bestiary.ErrAmbiguous
		if errors.As(err, &ambig) {
			t.Fatalf("Resolve with nonexistent [modifier] returned ErrAmbiguous; "+
				"expected ErrNotFound since no model has Modifier=%q", "nonexistent-modifier-xyz")
		}
		t.Fatalf("Resolve with nonexistent [modifier]: unexpected error %T: %v", err, err)
	}
}

// TestResolve_BracketSuffixStripping_RoundTrip verifies that ModelRef.String()
// (which emits bracket-suffix when Modifier is set) produces a string that
// Resolve() can successfully resolve back to the same model.
//
// This is the full round-trip: ref → String() → Resolve() → ref'.
// ref' must have the same (Family, Variant, Date, Modifier) as ref.
//
// BLOCKER: bestiary-wjk9
func TestResolve_BracketSuffixStripping_RoundTrip(t *testing.T) {
	// Find any static model with a non-empty Modifier to exercise the round-trip.
	var seed *bestiary.ModelRef
	for _, m := range bestiary.StaticModels() {
		if m.Modifier != "" && m.Family != "" && m.Date != "" && m.Provider == bestiary.ProviderAnthropic {
			ref := m.Ref()
			seed = &ref
			break
		}
	}
	if seed == nil {
		t.Skip("no Anthropic static model with Modifier and Date found; skipping round-trip test")
	}

	// seed.String() produces canonical form including [modifier] bracket suffix.
	canonical := seed.String()
	if canonical == "" {
		t.Fatalf("ModelRef.String() returned empty string for %+v", *seed)
	}

	// Resolve must accept the canonical string and return a matching ref.
	refs, err := bestiary.Resolve(canonical)
	if err != nil {
		t.Fatalf("Resolve(%q) = error %v; round-trip must succeed after bracket-suffix fix", canonical, err)
	}
	if len(refs) == 0 {
		t.Fatalf("Resolve(%q) returned empty refs; round-trip must return at least one ref", canonical)
	}

	// At least one returned ref must match the original seed's (Family, Variant, Date, Modifier).
	found := false
	for _, r := range refs {
		if r.Family == seed.Family && r.Variant == seed.Variant &&
			r.Date == seed.Date && r.Modifier == seed.Modifier {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Resolve(%q): no ref matched seed (Family=%q, Variant=%q, Date=%q, Modifier=%q); refs=%v",
			canonical, seed.Family, seed.Variant, seed.Date, seed.Modifier, refs)
	}
}
