package bestiary

import (
	"net/url"
	"strings"
)

// This file mints IRIs (RFC 3987) for the two identity types: an entity key
// (EntityRef) and a per-provider ref (ModelRef). It is a PURE RENDERING layer over the
// canonical strings those types already produce — it adds no new identity, no new
// stored field, and no schema surface. The canonical string stays the identity; the IRI
// is that identity expressed as a dereferenceable name inside a namespace the CALLER
// owns.

// mintIRI is the single shared mint behind EntityRef.IRI and ModelRef.IRI: it appends
// the percent-encoded canonical string to the caller-supplied base VERBATIM.
//
// The base is deliberately NOT defaulted and NOT validated beyond emptiness. bestiary
// does not own a public namespace, so inventing one (or rejecting a caller's) would be
// a claim it cannot back: the consumer supplies its own (a w3id entry, an internal
// https namespace, a urn: prefix). Emptiness is the ONE rejected case, because an empty
// base yields a bare relative reference rather than an IRI — returning "" there is the
// same honest-empty discipline the restricted PURL render uses, never a half-formed
// identifier a caller might publish.
func mintIRI(base, canonical string) string {
	if base == "" {
		return ""
	}
	return base + escapeIRISegment(canonical)
}

// escapeIRISegment percent-encodes a canonical identity string so it occupies exactly
// ONE path segment of an IRI.
//
// Escaping variant, and why: url.PathEscape is the PATH-SEGMENT escaper (the IRI's
// actual context here) — it encodes '/', '#', '{', '}', '[', ']' and every other
// character that is not legal inside a segment, and, crucially, it leaves a space as
// "%20". url.QueryEscape is the wrong tool despite encoding more: it renders a space as
// '+', which is a query-string convention that does NOT decode back through
// url.PathUnescape, so it would silently break the round-trip fence in a path position.
//
// PathEscape alone is not sufficient, though: '@' is a legal pchar (it is the userinfo
// delimiter only in an authority), so PathEscape leaves it raw — while the canonical
// grammar uses '@' as the version/date delimiter. It is therefore encoded explicitly
// afterwards. The replacement is safe and cannot double-encode: PathEscape has already
// converted every '%' in the input to "%25", so the only literal '@' bytes remaining in
// its output are the ones that came from the input.
//
// The result decodes back byte-identically through url.PathUnescape — the round-trip
// fence (iri_test.go) asserts exactly that over a torture set and over the whole
// committed registry. The space rationale above is not merely defensive prose: no key
// the grammar produces today contains a space, so the torture set carries a SYNTHETIC
// space-bearing case whose only job is to pin this escaper choice
// (TestIRI_SpacePinsEscaperChoice reddens if this line becomes url.QueryEscape).
func escapeIRISegment(canonical string) string {
	return strings.ReplaceAll(url.PathEscape(canonical), "@", "%40")
}

// IRI returns an IRI (RFC 3987) naming this ENTITY inside the caller-supplied base
// namespace: base + the percent-encoded entity key.
//
// The entity key (EntityRef.String(), e.g. "llama/scout@4#17b-16e{instruct}") is the
// identity; this method only renders it as a dereferenceable name. Every delimiter the
// key grammar produces — '/', '@', '#', '{', '}' — is percent-encoded, so the key can
// never be misread as IRI structure. '#' matters most: left raw it would start a URI
// FRAGMENT, silently truncating the identifier at the parameter-size segment
// ("…/llama#17b" would name the entity "llama" with a fragment).
//
// base is a parameter by design (see mintIRI) and is used verbatim: no separator is
// inserted and no trailing slash is required, so a hash namespace
// ("https://example.org/ns#") works as well as a slash one. An empty base returns "".
//
//	EntityRef{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e",
//	          Modifier: []string{"instruct"}}.IRI("https://w3id.org/bestiary/entity/")
//	  → "https://w3id.org/bestiary/entity/llama%2Fscout%404%2317b-16e%7Binstruct%7D"
func (r EntityRef) IRI(base string) string {
	return mintIRI(base, r.String())
}

// IRI returns an IRI (RFC 3987) naming this per-provider REF inside the caller-supplied
// base namespace: base + the percent-encoded canonical rendering
// (Format(SchemeCanonical), e.g. "anthropic/claude/opus@2025-05-14").
//
// It is the instance-level twin of EntityRef.IRI and differs only in WHAT is named: a
// ModelRef IRI names one provider's serving of a model (it carries the provider segment,
// the release date and the "[attributes]" segment), while an EntityRef IRI names the
// provider-agnostic entity. A consumer building a graph normally wants the entity IRI as
// the subject and the ref IRIs as its per-provider instances.
//
// The same escaping and base rules apply (see escapeIRISegment / mintIRI); an empty base
// returns "".
func (r ModelRef) IRI(base string) string {
	return mintIRI(base, r.Format(SchemeCanonical))
}
