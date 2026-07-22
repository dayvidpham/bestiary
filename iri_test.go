package bestiary_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// iriTortureRefs is the round-trip torture set: every special character the entity-key
// grammar can produce, alone and in combination, plus real keys taken from the committed
// catalog. The grammar is family[/variant][@version][#paramsize]{identity-mods}, so the
// characters that MUST survive a mint/unescape cycle are '/', '@', '#', '{', '}', ','
// (the identity-mod separator), '.' and '-'.
var iriTortureRefs = []struct {
	ref bestiary.EntityRef
	key string
}{
	{bestiary.EntityRef{Family: "llama"}, "llama"},
	{bestiary.EntityRef{Family: "llama", Variant: "scout"}, "llama/scout"},
	{bestiary.EntityRef{Family: "llama", Version: "4"}, "llama@4"},
	{bestiary.EntityRef{Family: "llama", ParamSize: "17b-16e"}, "llama#17b-16e"},
	{bestiary.EntityRef{Family: "llama", Modifier: []string{"instruct"}}, "llama{instruct}"},
	{bestiary.EntityRef{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}}, "llama/scout@4#17b-16e{instruct}"},
	{bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}, "llama@3.3#70b{instruct}"},
	{bestiary.EntityRef{Family: "llama", Version: "3.1", ParamSize: "405b", Modifier: []string{"turbo", "instruct"}}, "llama@3.1#405b{turbo,instruct}"},
	{bestiary.EntityRef{Family: "qwen", Variant: "coder", Version: "3", ParamSize: "480b-a35b", Modifier: []string{"instruct"}}, "qwen/coder@3#480b-a35b{instruct}"},
	{bestiary.EntityRef{Family: "deepseek", Variant: "v3.2"}, "deepseek/v3.2"},
	{bestiary.EntityRef{Family: "gemini", Version: "3.0"}, "gemini@3.0"},
}

// iriBase is a stand-in for the consumer's (still-pending) w3id namespace. The base is a
// caller-supplied PARAMETER by design: bestiary never invents a namespace it does not own.
const iriBase = "https://w3id.org/bestiary/entity/"

// TestEntityRef_IRI_RoundTrip is the core fence: for every torture-set key, stripping the
// caller-supplied base off the minted IRI and percent-decoding the remainder returns the
// canonical key BYTE-IDENTICALLY. A mint that lost or mangled a grammar character would
// break here.
func TestEntityRef_IRI_RoundTrip(t *testing.T) {
	for _, tc := range iriTortureRefs {
		if got := tc.ref.String(); got != tc.key {
			// Guard the fixture itself: a torture case whose key drifted would test
			// the wrong string.
			t.Fatalf("EntityRef.String() = %q, want the torture key %q", got, tc.key)
		}
		iri := tc.ref.IRI(iriBase)
		if !strings.HasPrefix(iri, iriBase) {
			t.Fatalf("IRI(%q) = %q, want the caller base as a prefix", tc.key, iri)
		}
		encoded := strings.TrimPrefix(iri, iriBase)
		decoded, err := url.PathUnescape(encoded)
		if err != nil {
			t.Fatalf("PathUnescape(%q) failed: %v", encoded, err)
		}
		if decoded != tc.key {
			t.Errorf("round trip for %q: decoded %q (encoded %q)", tc.key, decoded, encoded)
		}
	}
}

// TestEntityRef_IRI_EncodesGrammarDelimiters pins the ESCAPING CONTRACT itself: every
// character that is structurally significant in an IRI — '/', '@', '#', '{', '}' — must
// be percent-encoded, so the whole key occupies exactly ONE path segment and can never be
// read as a path boundary, a userinfo separator or (worst) a fragment start.
func TestEntityRef_IRI_EncodesGrammarDelimiters(t *testing.T) {
	ref := bestiary.EntityRef{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}}
	iri := ref.IRI(iriBase)
	tail := strings.TrimPrefix(iri, iriBase)
	for _, bad := range []string{"/", "@", "#", "{", "}"} {
		if strings.Contains(tail, bad) {
			t.Errorf("minted IRI tail %q still contains the raw delimiter %q", tail, bad)
		}
	}
	want := iriBase + "llama%2Fscout%404%2317b-16e%7Binstruct%7D"
	if iri != want {
		t.Errorf("IRI = %q, want %q", iri, want)
	}
}

// TestEntityRef_IRI_EmptyBase verifies the ONE validation the mint performs: an empty
// base cannot produce an IRI (the result would be a bare relative reference), so it
// returns "" rather than a half-formed identifier. The base is otherwise unvalidated —
// bestiary does not own the consumer's namespace and will not second-guess its shape.
func TestEntityRef_IRI_EmptyBase(t *testing.T) {
	ref := bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b", Modifier: []string{"instruct"}}
	if got := ref.IRI(""); got != "" {
		t.Errorf("EntityRef.IRI(\"\") = %q, want \"\"", got)
	}
	if got := (bestiary.ModelRef{ID: "x", Provider: "p"}).IRI(""); got != "" {
		t.Errorf("ModelRef.IRI(\"\") = %q, want \"\"", got)
	}
}

// TestEntityRef_IRI_BaseVerbatim verifies the base is appended VERBATIM: bestiary neither
// normalizes it, adds a separator, nor requires a trailing slash — the caller owns the
// namespace shape (a hash-namespace base ending in '#' is as legitimate as a slash one).
func TestEntityRef_IRI_BaseVerbatim(t *testing.T) {
	ref := bestiary.EntityRef{Family: "llama"}
	for _, base := range []string{
		"https://w3id.org/bestiary/entity/",
		"https://example.org/ns#",
		"urn:bestiary:",
	} {
		if got := ref.IRI(base); got != base+"llama" {
			t.Errorf("IRI(%q) = %q, want %q", base, got, base+"llama")
		}
	}
}

// TestModelRef_IRI_RoundTrip is the ref-level twin of the entity fence: a ModelRef mints
// over its CANONICAL rendering (provider-scoped, with the "@date" suffix and the
// "[attributes]" segment), and that string survives the same decode cycle — including the
// square brackets, which the entity-key grammar never produces.
func TestModelRef_IRI_RoundTrip(t *testing.T) {
	refs := []bestiary.ModelRef{
		{ID: "claude-opus-4-20250514", Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "opus", Date: "2025-05-14"},
		{ID: "claude-opus-4-5-fast", Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "opus", Version: "4.5", Modifier: []string{"fast", "thinking"}},
		{ID: "meta-llama/Llama-4-Scout-17B-16E-Instruct", Provider: "togetherai", Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}},
		{ID: "some-opaque-model-id", Provider: "custom-provider"},
	}
	for _, ref := range refs {
		canonical := ref.Format(bestiary.SchemeCanonical)
		iri := ref.IRI(iriBase)
		if !strings.HasPrefix(iri, iriBase) {
			t.Fatalf("IRI(%q) = %q, want the caller base as a prefix", canonical, iri)
		}
		encoded := strings.TrimPrefix(iri, iriBase)
		decoded, err := url.PathUnescape(encoded)
		if err != nil {
			t.Fatalf("PathUnescape(%q) failed: %v", encoded, err)
		}
		if decoded != canonical {
			t.Errorf("round trip for %q: decoded %q (encoded %q)", canonical, decoded, encoded)
		}
	}
}

// TestModelRef_IRI_EncodesAttributeBrackets pins that the ref-level "[attributes]"
// segment's brackets are encoded too: '[' and ']' delimit an IP-literal host in the URI
// grammar and must never appear raw in a path segment.
func TestModelRef_IRI_EncodesAttributeBrackets(t *testing.T) {
	ref := bestiary.ModelRef{ID: "claude-opus-4-5-fast", Provider: bestiary.ProviderAnthropic, Family: "claude", Variant: "opus", Version: "4.5", Modifier: []string{"fast", "thinking"}}
	tail := strings.TrimPrefix(ref.IRI(iriBase), iriBase)
	if !strings.Contains(ref.Format(bestiary.SchemeCanonical), "[thinking,fast]") {
		t.Fatalf("fixture drift: canonical %q no longer carries an [attributes] segment", ref.Format(bestiary.SchemeCanonical))
	}
	for _, bad := range []string{"[", "]"} {
		if strings.Contains(tail, bad) {
			t.Errorf("minted IRI tail %q still contains the raw delimiter %q", tail, bad)
		}
	}
}

// TestIRI_StaticRegistryRoundTrip is the corpus-wide fence: EVERY entity in the committed
// registry, and every model ref in it, mints an IRI that decodes back to its canonical
// string. This catches a grammar character that only some rare key in the catalog produces.
func TestIRI_StaticRegistryRoundTrip(t *testing.T) {
	for _, e := range bestiary.Entities() {
		key := e.Ref.String()
		decoded, err := url.PathUnescape(strings.TrimPrefix(e.Ref.IRI(iriBase), iriBase))
		if err != nil {
			t.Fatalf("entity %q: PathUnescape failed: %v", key, err)
		}
		if decoded != key {
			t.Errorf("entity %q: IRI decoded to %q", key, decoded)
		}
	}
	for _, m := range bestiary.StaticModels() {
		ref := m.Ref()
		canonical := ref.Format(bestiary.SchemeCanonical)
		decoded, err := url.PathUnescape(strings.TrimPrefix(ref.IRI(iriBase), iriBase))
		if err != nil {
			t.Fatalf("model %q: PathUnescape failed: %v", m.ID, err)
		}
		if decoded != canonical {
			t.Errorf("model %q: IRI decoded to %q, want %q", m.ID, decoded, canonical)
		}
	}
}
