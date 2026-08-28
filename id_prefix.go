package bestiary

import "strings"

// IDPrefixClass classifies the LEADING token of a model ID by what that token
// NAMES. It exists to answer one question with a typed value rather than a
// per-vendor exception list: is this token a fact the record already carries on
// another axis, or is it the only place that fact appears?
//
// The distinction is the whole safety property. A leading token may be dropped
// from the decomposition input ONLY when a DIFFERENT carrier already holds the
// same fact, because then the identity tuple loses nothing:
//
//   - IDPrefixSelfProvider — the token is the serving provider's own slug. The
//     Provider field carries it, so repeating it in the id says nothing new.
//   - IDPrefixCreator — the token names the lab that TRAINED the model, and the
//     family the remainder decomposes to declares that same lab on the Creator
//     axis. The Creator axis carries it.
//   - IDPrefixNone — nothing else carries the token, so removing it would DELETE
//     information. This is the answer for a backend-host label (a token naming a
//     provider that is NOT the serving one), for a product-surface namespace, and
//     for the ordinary case of an id that simply begins with its family.
//
// Two carriers already had their own machinery before this classifier existed and
// keep it: a Bedrock cross-region routing prefix is carried by the Region axis
// (bedrockProfile / DetectRegion) and a curated backend-host prefix is carried by
// the Host axis (DetectHost). Those are the same rule applied earlier, not
// exceptions to it.
type IDPrefixClass uint8

const (
	// IDPrefixNone is the honest default: no other axis holds this token, so the
	// decomposition input is left exactly as the upstream catalog spelled it.
	IDPrefixNone IDPrefixClass = iota
	// IDPrefixSelfProvider — the leading token equals the serving provider's slug.
	IDPrefixSelfProvider
	// IDPrefixCreator — the leading token names the creating lab that the
	// remainder's family already declares on the Creator axis.
	IDPrefixCreator
)

// String renders the class for diagnostics and test failure messages.
func (c IDPrefixClass) String() string {
	switch c {
	case IDPrefixSelfProvider:
		return "self-provider"
	case IDPrefixCreator:
		return "creator"
	default:
		return "none"
	}
}

// splitLeadingIDToken splits an id at its first "-" or "." separator, returning
// the lowercased leading token and the remainder. ok is false when there is no
// separator, when the token is empty, or when nothing follows the separator — a
// bare token is a model name, not a namespace.
func splitLeadingIDToken(id string) (tok, rest string, ok bool) {
	i := strings.IndexAny(id, "-.")
	if i <= 0 || i == len(id)-1 {
		return "", id, false
	}
	return strings.ToLower(id[:i]), id[i+1:], true
}

// remainderFamily returns the family the remainder of a prefix split would
// decompose to, using only its own leading token, and reports whether that family
// is known. This is deliberately a CHEAP, NON-RECURSIVE probe rather than a full
// re-decomposition: the classifier runs inside the decomposition entrypoints, so a
// recursive probe would re-enter them.
//
// It is also the non-destruction guard, and it is what makes the two strip rules
// safe on ids that merely BEGIN with their own provider's or lab's name. Measured:
// provider "minimax" serves "minimax-m2" and provider "deepseek" serves
// "deepseek-v3"; the leading token equals the provider slug in both, but the
// remainders ("m2", "v3") name no family, so stripping would destroy the identity
// rather than de-duplicate it. Requiring the remainder to still name a KNOWN
// family rejects both.
func remainderFamily(rest string) (Family, bool) {
	tok, _, ok := splitLeadingIDToken(rest)
	if !ok {
		tok = strings.ToLower(rest)
	}
	f := Family(tok)
	return f, f.IsKnown()
}

// isCreatorToken reports whether tok is a value in the curated Creator vocabulary.
// Membership alone never licenses a strip — ClassifyIDPrefix additionally requires
// the remainder's family to declare that exact creator, which is the carrier check.
func isCreatorToken(tok string) bool {
	for _, c := range Creators() {
		if string(c) == tok {
			return true
		}
	}
	return false
}

// ClassifyIDPrefix classifies the leading token of id given the provider serving
// it, and returns the id the decomposition should read. On IDPrefixNone the id
// comes back byte-identical; on any other class the leading token and its
// separator are removed.
//
// Namespaced ids (containing "/") are never split here: their org segment is
// already the vendor/namespace strip's job (lastPathSegment), and a token that
// merely begins with a vendor word inside an org segment must not be touched —
// the same guard DetectHost carries.
//
// The rules, in order, each stated as the carrier that makes it safe:
//
//  1. SELF-PROVIDER. The token equals the serving provider's slug, so the
//     Provider field already holds it. Measured: databricks prefixes all 30 of its
//     own ids with "databricks-".
//
//  2. CREATOR. The token is in the Creator vocabulary AND the family the remainder
//     names declares that same creator, so the Creator axis already holds it.
//     Measured: "openai-gpt-5.6-luna" (snowflake-cortex, digitalocean, venice) and
//     "openai.gpt-5.6-luna" (amazon-bedrock's region-less profile form) both
//     decompose to family gpt, whose curated creator IS openai.
//
// Both rules are gated on the remainder still naming a known family, so a strip
// can never leave a stub behind (see remainderFamily).
//
// WHAT IS DELIBERATELY NOT STRIPPED, because no carrier holds it:
//
//   - A backend-host label — a token naming a provider OTHER than the serving one.
//     nano-gpt's "azure-gpt-4o" is served by nano-gpt, not by Azure, so "azure"
//     is the only place that routing fact appears; it fails rule 1 (wrong
//     provider) and rule 2 (not a creator) and is left alone. The Host axis is
//     where it is carried when curated. A blanket provider-name strip erased
//     exactly this label once, and the rule above is what makes that a
//     consequence of the definition rather than a remembered exception.
//   - A product-surface namespace. Measured: gitlab prefixes all 22 of its ids
//     with "duo-chat-". "duo-chat" is neither the provider slug ("gitlab") nor a
//     creating lab, and no axis in the entity model records which of a provider's
//     surfaces served a model, so stripping it would delete a fact nothing else
//     holds. The constancy of the prefix across a provider's ids is NOT on its own
//     a carrier test: measured, 28 providers prefix every one of their ids with a
//     single token, and for most of them (anthropic's "claude-", xai's "grok-",
//     zai's "glm-", moonshotai's "kimi-", xiaomi's "mimo-", upstage's "solar-")
//     that token is the FAMILY. Constancy would strip identity.
//   - A lab token the Creator axis spells differently. Measured: amazon-bedrock
//     serves "zai.glm-…" and "moonshot.kimi-…", but the curated creators of glm
//     and kimi are "zhipu" and "moonshotai". The carrier does not hold the value
//     the id spells, so rule 2 correctly declines; a creator-alias vocabulary is
//     the honest fix and is not in this change.
func ClassifyIDPrefix(id ModelID, p Provider) (IDPrefixClass, ModelID) {
	s := string(id)
	if s == "" || strings.Contains(s, "/") {
		return IDPrefixNone, id
	}
	tok, rest, ok := splitLeadingIDToken(s)
	if !ok {
		return IDPrefixNone, id
	}
	dotted := len(tok) < len(s) && s[len(tok)] == '.'
	fam, known := remainderFamily(rest)
	if !known {
		return IDPrefixNone, id
	}
	switch {
	case tok == strings.ToLower(string(p)):
		return IDPrefixSelfProvider, ModelID(finishStrip(rest, dotted))
	case isCreatorToken(tok) && string(fam.Creator()) == tok:
		return IDPrefixCreator, ModelID(finishStrip(rest, dotted))
	}
	return IDPrefixNone, id
}

// finishStrip completes a leading-token strip. When the token was DOT-separated the
// id is in the Bedrock inference-profile grammar "[<region>.]<vendor>.<model>[-v<N>:<M>]",
// whose trailing routing tag must go with the prefix: bedrockProfile already removes
// it on the region-ful arm, and leaving it on the region-less arm swallows the
// release date sitting behind it, so a dated id like
// "anthropic.claude-haiku-4-5-20251001-v1:0" reads its DATE as part of the version.
// Dash-separated strips are left alone — "-v<N>" is a meaningful trailing token in
// plainly-served ids (nemotron-super-49b-v1) and only the dotted namespace licenses
// reading it as routing.
func finishStrip(rest string, dotted bool) string {
	if !dotted {
		return rest
	}
	return stripBedrockRoutingTail(rest)
}
