package bestiary

import (
	"fmt"
	"strings"
)

// Region is the serving JURISDICTION of a model instance: the AWS Bedrock
// cross-region inference-profile region encoded in a dotted "<region>.<vendor>."
// model-ID prefix (us.*, eu.*, au.*, jp.*, global.*). The axis exists for
// international data-protection relevance — Australia's Privacy Act and Japan's
// APPI are DISTINCT jurisdictions, so au and jp are distinct members (never folded
// into a single Asia-Pacific bucket).
//
// Like Host, a Region is a per-instance ATTRIBUTE and is NEVER part of entity
// identity — it is excluded from EntityRef and from all key rendering, because the
// same artifact served in two regions is one entity, not two. It is a CLOSED int
// enum (the Quantization / DerivationKind precedent) with a RegionRaw carrier for
// the fail-safe RegionOther bucket: a recognized-but-unmapped region token is never
// dropped, it rides along on RegionRaw (the Quantization/QuantRaw precedent).
//
// ISO grounding: the member String() values follow ISO 3166-1 alpha-2 where
// applicable — "us", "au", "jp" are country codes and "eu" is the ISO
// exceptionally-reserved code for the European Union; "apac" and "global" are
// provider-defined scopes OUTSIDE ISO. A future ISO-alpha-2 Bedrock prefix earns its
// own member; anything unrecognized falls to RegionOther + RegionRaw. RegionNone is
// the zero value (no prefix) and renders "unspecified" (not "") for external-reader
// clarity.
type Region int

const (
	// RegionNone is the zero value: the model ID carries no Bedrock region prefix.
	// It renders as "unspecified" so an external reader is never shown a blank.
	RegionNone Region = iota
	// RegionUS is the AWS "us" cross-region inference profile (ISO 3166-1 alpha-2 US).
	RegionUS
	// RegionEU is the AWS "eu" cross-region inference profile (ISO exceptionally-reserved EU).
	RegionEU
	// RegionAPAC is the AWS "apac" cross-region scope (provider-defined, outside ISO).
	// RESERVED: Bedrock documents an "apac." profile prefix, but there are zero attested
	// instances in the current catalog — au and jp are their OWN members, never mapped here.
	RegionAPAC
	// RegionGlobal is the AWS "global" cross-region scope (provider-defined, outside ISO).
	RegionGlobal
	// RegionAU is the AWS "au" cross-region inference profile (ISO 3166-1 alpha-2 AU,
	// Australia — a distinct data-protection jurisdiction, the Privacy Act).
	RegionAU
	// RegionJP is the AWS "jp" cross-region inference profile (ISO 3166-1 alpha-2 JP,
	// Japan — a distinct data-protection jurisdiction, the APPI).
	RegionJP
	// RegionOther is the fail-safe bucket: a Bedrock region token that is recognized
	// as a region but not mapped to a named member. The raw token rides on RegionRaw
	// so it is never lost.
	RegionOther
)

// String renders the Region as a stable lowercase token. RegionNone renders
// "unspecified" (a user ruling for external-reader clarity — never the empty
// string); the concrete regions render their canonical ISO/scope token;
// RegionOther renders "other" (the discriminating raw token lives on RegionRaw,
// not here).
func (r Region) String() string {
	switch r {
	case RegionNone:
		return "unspecified"
	case RegionUS:
		return "us"
	case RegionEU:
		return "eu"
	case RegionAPAC:
		return "apac"
	case RegionGlobal:
		return "global"
	case RegionAU:
		return "au"
	case RegionJP:
		return "jp"
	case RegionOther:
		return "other"
	default:
		return "unspecified"
	}
}

// ParseRegion is the inverse of String for the named members: it maps a canonical
// token back to its Region. "unspecified" and "" both round-trip to RegionNone
// (the two spellings of absence). An unknown non-empty token is an actionable
// error rather than a silent RegionOther (the ParseQuantization precedent: the
// CLI/parse path must guide the caller, never swallow a typo). RegionOther is
// reached only through DetectRegion's carrier path, never minted from a bare token
// here.
func ParseRegion(s string) (Region, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "unspecified":
		return RegionNone, nil
	case "us":
		return RegionUS, nil
	case "eu":
		return RegionEU, nil
	case "apac":
		return RegionAPAC, nil
	case "global":
		return RegionGlobal, nil
	case "au":
		return RegionAU, nil
	case "jp":
		return RegionJP, nil
	default:
		return RegionNone, fmt.Errorf(
			"bestiary: ParseRegion(%q): unknown region token\n"+
				"  What: the input is not a recognized region\n"+
				"  Why: only the closed set {unspecified, us, eu, apac, global, au, jp} is accepted\n"+
				"  Where: bestiary.ParseRegion (region.go)\n"+
				"  How to fix: pass one of the accepted tokens, or \"unspecified\" for no region",
			s,
		)
	}
}

// regionFromToken maps a lowercase Bedrock region prefix token (as parsed by
// bedrockProfile) to its Region member and, for the fail-safe RegionOther bucket,
// the raw token carrier. The attested catalog set (us/eu/au/jp/global) and the
// reserved "apac" all map to named members with an empty raw; a
// recognized-but-unmapped region token (e.g. a gated-but-unnamed "ca"/"sa") yields
// RegionOther plus the raw token so it is never dropped.
func regionFromToken(tok string) (Region, string) {
	switch strings.ToLower(tok) {
	case "us":
		return RegionUS, ""
	case "eu":
		return RegionEU, ""
	case "apac":
		return RegionAPAC, ""
	case "global":
		return RegionGlobal, ""
	case "au":
		return RegionAU, ""
	case "jp":
		return RegionJP, ""
	default:
		return RegionOther, strings.ToLower(tok)
	}
}

// DetectRegion inspects a model ID for an AWS Bedrock cross-region inference-profile
// "<region>.<vendor>." prefix and returns the Region plus the raw region token. The
// raw is non-empty ONLY for the fail-safe RegionOther (a gated-but-unnamed region
// token, carried so it is never dropped); for a named member and for RegionNone the
// raw is "". An ID with no Bedrock prefix returns (RegionNone, "").
//
// It mirrors DetectHost's DETECTION pattern (the Region is decided from the ID
// prefix alone, a per-instance attribute), but its second return is the RegionRaw
// carrier rather than a stripped ID: the region+profile-suffix strip that makes the
// (Family,Variant,Version) tuple region-independent is owned by stripBedrockProfile
// (via stripVendorNamespace), so a caller building an instance record populates
// (Region, RegionRaw) here and gets the plain model id from the decomposition path.
func DetectRegion(id ModelID) (Region, string) {
	tok, _, ok := bedrockProfile(string(id))
	if !ok {
		return RegionNone, ""
	}
	return regionFromToken(tok)
}
