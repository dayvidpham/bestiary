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

// escapeIRISegment percent-encodes a canonical identity string for use as the path
// TAIL of an IRI, keeping '/' LITERAL so the key renders as a MULTI-SEGMENT path.
//
// RQ1 amendment (v0.2.8): this is a DELIBERATE output change from the v0.2.7 behavior,
// which percent-encoded '/' as "%2F" and packed the whole key into one opaque segment.
// The key grammar's '/' (family/variant) is now emitted as a real path boundary so an
// entity IRI is a walkable path — e.g. base + "llama/scout%404" rather than base +
// "llama%2Fscout%404". This is the SAME grammar the cmd/bestiary-web /entity/<key>
// routes dereference: the mint and the route share one grammar, so EntityRef.IRI(root)
// equals the route path for the same entity (pinned by TestEntityRef_IRI_MatchesRoute).
// Every OTHER structural delimiter — '@', '#', '{', '}', and the ref-level '[', ']' —
// is still percent-encoded so it can never be misread as IRI structure; '#' matters
// most (left raw it would start a URI fragment and silently truncate the identifier).
//
// Mechanism: split on '/', escape each segment with the per-segment escaper
// (url.PathEscape + explicit '@'→"%40"), then rejoin with a LITERAL '/'. Within a
// segment the output is byte-identical to the pre-amendment escaper; only the segment
// separator changes from "%2F" to "/". Two facts make this correct:
//
//   - Escaping variant: url.PathEscape is the PATH-SEGMENT escaper — it encodes '#',
//     '{', '}', '[', ']' and every other non-segment character and, crucially, leaves a
//     space as "%20". url.QueryEscape is the wrong tool despite encoding more: it renders
//     a space as '+', a query-string convention that does NOT decode back through
//     url.PathUnescape, so it would silently break the round-trip fence. PathEscape alone
//     is not sufficient, though: '@' is a legal pchar (userinfo delimiter only in an
//     authority) so PathEscape leaves it raw, while the grammar uses '@' as the
//     version/date delimiter — it is therefore encoded explicitly afterwards. That
//     replacement cannot double-encode: PathEscape has already turned every '%' into
//     "%25", so the only literal '@' bytes left are the ones from the input.
//   - Round-trip: a whole-string url.PathUnescape still recovers the key byte-identically
//     because a literal '/' passes through unchanged and every "%40"/"%23"/"%7B"/"%7D"/
//     "%5B"/"%5D" decodes back. The round-trip fences (iri_test.go) assert exactly that
//     over a torture set and the whole committed registry, and stay green under this
//     change — only the two golden-string assertions were re-pinned for the literal '/'.
//
// The space rationale above is not merely defensive prose: no key the grammar produces
// today contains a space, so the torture set carries a SYNTHETIC space-bearing case whose
// only job is to pin this escaper choice (TestIRI_SpacePinsEscaperChoice reddens if this
// line becomes url.QueryEscape).
func escapeIRISegment(canonical string) string {
	parts := strings.Split(canonical, "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(p), "@", "%40")
	}
	return strings.Join(parts, "/")
}

// IRI returns an IRI (RFC 3987) naming this ENTITY inside the caller-supplied base
// namespace: base + the percent-encoded entity key.
//
// The entity key (EntityRef.String(), e.g. "llama/scout@4#17b-16e{instruct}") is the
// identity; this method only renders it as a dereferenceable name. Every structural
// delimiter the key grammar produces EXCEPT '/' — '@', '#', '{', '}' — is
// percent-encoded, so it can never be misread as IRI structure. '#' matters most: left
// raw it would start a URI FRAGMENT, silently truncating the identifier at the
// parameter-size segment ("…/llama#17b" would name the entity "llama" with a fragment).
// The '/' is kept LITERAL by design (RQ1 amendment, see escapeIRISegment) so the key
// renders as a multi-segment path that the cmd/bestiary-web /entity/<key> routes
// dereference under the same grammar.
//
// base is a parameter by design (see mintIRI) and is used verbatim: no separator is
// inserted and no trailing slash is required, so a hash namespace
// ("https://example.org/ns#") works as well as a slash one. An empty base returns "".
//
//	EntityRef{Family: "llama", Variant: "scout", Version: "4", ParamSize: "17b-16e",
//	          Modifier: []string{"instruct"}}.IRI("https://w3id.org/bestiary/entity/")
//	  → "https://w3id.org/bestiary/entity/llama/scout%404%2317b-16e%7Binstruct%7D"
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
