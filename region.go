package bestiary

import (
	"fmt"
	"strings"
)

// Region is the serving JURISDICTION of a model instance: the AWS Bedrock
// cross-region inference-profile region encoded in a dotted "<region>.<vendor>."
// model-ID prefix (us.*, eu.*, au.*, jp.*, global.*). Like Host, a Region is a
// per-instance ATTRIBUTE and is NEVER part of entity identity — it is excluded
// from EntityRef and from all key rendering, because the same artifact served in
// two regions is one entity, not two.
//
// It is a CLOSED int enum (the Quantization / DerivationKind precedent) with a
// RegionRaw carrier for the fail-safe RegionOther bucket: a recognized-but-unmapped
// region token is never dropped, it rides along on RegionRaw (the
// Quantization/QuantRaw precedent). RegionNone is the zero value (no prefix), and
// it renders as "unspecified" (not "") for external-reader clarity.
type Region int

const (
	// RegionNone is the zero value: the model ID carries no Bedrock region prefix.
	// It renders as "unspecified" so an external reader is never shown a blank.
	RegionNone Region = iota
	// RegionUS is the AWS "us" cross-region inference profile.
	RegionUS
	// RegionEU is the AWS "eu" cross-region inference profile.
	RegionEU
	// RegionAPAC is the AWS Asia-Pacific family: "au" (Australia), "jp" (Japan),
	// and the "ap"/"apac" cross-region profiles.
	RegionAPAC
	// RegionGlobal is the AWS "global" cross-region inference profile.
	RegionGlobal
	// RegionOther is the fail-safe bucket: a Bedrock region token that is recognized
	// as a region but not mapped to a named member. The raw token rides on RegionRaw
	// so it is never lost.
	RegionOther
)

// String renders the Region as a stable lowercase token. RegionNone renders
// "unspecified" (a user ruling for external-reader clarity — never the empty
// string); the concrete regions render their canonical token; RegionOther renders
// "other" (the discriminating raw token lives on RegionRaw, not here).
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
	case "apac", "ap", "au", "jp":
		return RegionAPAC, nil
	case "global":
		return RegionGlobal, nil
	default:
		return RegionNone, fmt.Errorf(
			"bestiary: ParseRegion(%q): unknown region token\n"+
				"  What: the input is not a recognized region\n"+
				"  Why: only the closed set {unspecified, us, eu, apac, ap, au, jp, global} is accepted\n"+
				"  Where: bestiary.ParseRegion (region.go)\n"+
				"  How to fix: pass one of the accepted tokens, or \"unspecified\" for no region",
			s,
		)
	}
}

// regionFromToken maps a lowercase Bedrock region prefix token (as parsed by
// bedrockProfile) to its Region member and, for the fail-safe RegionOther bucket,
// the raw token carrier. The attested catalog set (us/eu/au/jp/global) all map to
// named members with an empty raw; a recognized-but-unmapped region token (e.g. a
// future "ca"/"sa") yields RegionOther plus the raw token so it is never dropped.
func regionFromToken(tok string) (Region, string) {
	switch strings.ToLower(tok) {
	case "us":
		return RegionUS, ""
	case "eu":
		return RegionEU, ""
	case "au", "jp", "ap", "apac":
		return RegionAPAC, ""
	case "global":
		return RegionGlobal, ""
	default:
		return RegionOther, strings.ToLower(tok)
	}
}

// DetectRegion inspects a model ID for an AWS Bedrock cross-region inference-profile
// "<region>.<vendor>." prefix and returns the Region, the raw region token (non-empty
// ONLY for the fail-safe RegionOther), and the region+profile-suffix-stripped ID. An
// ID with no Bedrock prefix returns (RegionNone, "", id unchanged). It mirrors
// DetectHost: the Region is decided from the ID prefix alone and is a per-instance
// attribute; stripping it leaves the (Family,Variant,Version) tuple region-independent
// so a region-routed instance shares its entity with the plainly-served model.
func DetectRegion(id ModelID) (region Region, regionRaw string, stripped ModelID) {
	tok, model, ok := bedrockProfile(string(id))
	if !ok {
		return RegionNone, "", id
	}
	reg, raw := regionFromToken(tok)
	return reg, raw, ModelID(model)
}
