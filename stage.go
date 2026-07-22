package bestiary

import (
	"fmt"
	"sort"
	"strings"
)

// ReleaseStage classifies the release stage a model instance advertises IN ITS
// ID (preview / beta / latest / …). It is a closed int enum, following the
// DerivationKind / Quantization / ModelStatus precedent: the set of stage tokens
// is small and well-understood, so a closed enum gives type safety at call sites
// without a stringly-typed axis.
//
// ReleaseStage is DELIBERATELY separate from ModelStatus even though the two
// overlap on tokens like "beta". They are two different provenance levels:
//
//   - ModelStatus is upstream-DECLARED lifecycle metadata (the api.json `status`
//     field): the provider tells us "this is beta".
//   - ReleaseStage is ID-token-DERIVED (an instance fact read out of the model ID
//     by DetectStageFromID): bestiary infers "this ID carries a `-beta` marker".
//
// A model can carry one, both, or neither. `show` renders them under distinct
// labels ("stage" vs "status") so the two provenance levels never blur together.
//
// The zero value is StageNone: on this SCALAR field absence is meaningful — a
// model whose ID carries no stage marker is a normal, generally-available release
// — so None sits at zero. This None-at-zero convention matches ModelStatus (a
// scalar where absence carries information) and deliberately differs from the
// Other-at-zero convention on the models.dev element enums (LinkType,
// ReasoningOptionKind), whose every element always arrives with a discriminating
// tag.
//
// Wire names are lowercase ASCII strings; MarshalText / UnmarshalText implement
// encoding.TextMarshaler / encoding.TextUnmarshaler so a ReleaseStage serializes
// as a JSON/YAML string rather than an integer.
type ReleaseStage int

const (
	// StageNone is the zero value: the ID carries no recognized stage marker
	// (a normal, generally-available release).
	StageNone ReleaseStage = iota
	// StageStable: an explicitly stable/GA release. Enum member exists for CLI
	// and parse completeness; "stable" is NOT ID-detected this epoch (a bare
	// "stable" token in an ID is far more often a family name — see the
	// stable-diffusion guard in DetectReleaseStage — so detecting it would be
	// unsafe). ID-detection of "stable" is deferred (GH#13).
	StageStable
	// StagePreview: a preview / early-access release (ID-detected).
	StagePreview
	// StageBeta: a beta release (ID-detected). Detected without stripping — the
	// `beta` token stays in the ID's decomposition (it is entity-key material for
	// some families today, e.g. grok-4.20-beta-*), and re-keying it is deferred.
	StageBeta
	// StageAlpha: an early, unstable alpha release. Enum member exists for CLI
	// and parse completeness; "alpha" is NOT ID-detected this epoch (deferred,
	// GH#13).
	StageAlpha
	// StageExperimental: an experimental release. Enum member exists for CLI and
	// parse completeness; neither "experimental" nor its "-exp" short form is
	// ID-detected this epoch (deferred, GH#13).
	StageExperimental
	// StageLatest: a rolling "latest" alias (ID-detected). NOTE: "latest" (like
	// "original") NAMES A MOVING TARGET, not a fixed property of one artifact —
	// the model an id like "…-latest" resolves to changes over time. It is
	// recorded as a stage so the alias is visible, not because the artifact is
	// intrinsically "latest".
	StageLatest
	// StageOriginal: a rolling "original" alias (ID-detected). Like StageLatest,
	// this NAMES A MOVING TARGET (the pinned-original counterpart of a rolling
	// alias), not a fixed artifact property.
	StageOriginal
	// StageOther is the RESERVED fail-safe bucket for a stage token that is
	// present but not covered by the named constants above. Following the
	// Quantization / ModelStatus reserved-member precedent, it exists so the wire
	// format is stable when a FUTURE non-ID stage feeder lands (e.g. a metadata
	// field). It is UNUSED by the ID-detection path this epoch: DetectReleaseStage
	// returns known members only and NEVER StageOther, and ParseReleaseStage
	// rejects it as an internal sentinel. Its verbatim companion is StageRaw on
	// ModelInfo (populated only when Stage is StageOther, mirroring StatusRaw).
	StageOther
)

// releaseStageNames is the canonical lowercase wire name for each ReleaseStage,
// index-aligned with the iota constants. It is the single source of truth for
// String / MarshalText / UnmarshalText / ParseReleaseStage.
var releaseStageNames = [...]string{
	StageNone:         "none",
	StageStable:       "stable",
	StagePreview:      "preview",
	StageBeta:         "beta",
	StageAlpha:        "alpha",
	StageExperimental: "experimental",
	StageLatest:       "latest",
	StageOriginal:     "original",
	StageOther:        "other",
}

// idDetectedStages is the CLOSED subset of stage tokens that DetectReleaseStage
// recognizes when read out of a model ID this epoch. It is deliberately smaller
// than releaseStageNames: only tokens that actually appear as reliable, unambiguous
// stage markers in catalog IDs are detected. Notably ABSENT (→ StageNone):
//
//   - "stable" — far more often a family name ("stable-diffusion", "stable-audio")
//     than a stage marker; detecting it would misclassify those families.
//   - "alpha" / "experimental" / "exp" — appear in IDs but their ID-detection is
//     deferred to a dedicated release-stage curation pass (GH#13), pending a ruling
//     on the "-exp" short form and family collisions.
//
// The map is the family-member guard in practice: a token not present here is never
// promoted to a stage from an ID, so "stable-diffusion" (tokenized to "stable"
// + "diffusion") can never match.
var idDetectedStages = map[string]ReleaseStage{
	"preview":  StagePreview,
	"beta":     StageBeta,
	"latest":   StageLatest,
	"original": StageOriginal,
}

// String returns the canonical lowercase wire name of the stage. An out-of-range
// value renders as "releasestage(<n>)" so logs never silently drop an unexpected
// value.
func (s ReleaseStage) String() string {
	if int(s) < 0 || int(s) >= len(releaseStageNames) {
		return fmt.Sprintf("releasestage(%d)", int(s))
	}
	return releaseStageNames[s]
}

// MarshalText implements encoding.TextMarshaler, emitting the canonical wire name
// (so ReleaseStage serializes as a JSON string, not an integer). An out-of-range
// value is a programming error and yields an actionable error.
func (s ReleaseStage) MarshalText() ([]byte, error) {
	if int(s) < 0 || int(s) >= len(releaseStageNames) {
		return nil, fmt.Errorf(
			"bestiary: cannot marshal ReleaseStage: value %d is out of range [0,%d);"+
				" why: an invalid enum value was constructed"+
				" (only the StageNone..StageOther constants are valid);"+
				" where: ReleaseStage.MarshalText;"+
				" how to fix: assign one of the exported ReleaseStage constants",
			int(s), len(releaseStageNames),
		)
	}
	return []byte(releaseStageNames[s]), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, parsing a canonical wire name
// back into a ReleaseStage. Parsing is case-insensitive so a mixed-case token
// round-trips. An unrecognized token yields an actionable error.
func (s *ReleaseStage) UnmarshalText(text []byte) error {
	lower := strings.ToLower(string(text))
	for i, name := range releaseStageNames {
		if name == lower {
			*s = ReleaseStage(i)
			return nil
		}
	}
	return fmt.Errorf(
		"bestiary: cannot unmarshal ReleaseStage from %q;"+
			" why: the token does not match any known release stage;"+
			" where: ReleaseStage.UnmarshalText;"+
			" how to fix: use one of %v",
		string(text), releaseStageNames,
	)
}

// IsKnown reports whether s is a named constant in this package (i.e. not an
// out-of-range integer). StageOther is considered known — it is a named member of
// the enum; only truly out-of-range integers return false.
func (s ReleaseStage) IsKnown() bool {
	return int(s) >= 0 && int(s) < len(releaseStageNames)
}

// ParseReleaseStage parses a ReleaseStage from a string using a case-insensitive
// exact match against the canonical wire names. It is the CLI / configuration path
// (the analogue of ParseModelStatus / ParseQuantization): the empty string returns
// StageNone with no error, a recognized name returns its constant, and any other
// non-empty string returns an actionable error naming what was received and the
// valid values. It rejects the internal "other" sentinel (StageOther is not a
// user-selectable stage) and never silently buckets an unknown token.
//
// NOTE: there is intentionally NO CLI caller for ParseReleaseStage this epoch
// (`list` gains no stage filter — that is deferred). It is the public parse half
// of the stage axis, defined now per the final-interface principle (define the API
// you know the axis needs) rather than added later; it is NOT dead code and must
// not be removed as such.
func ParseReleaseStage(s string) (ReleaseStage, error) {
	if s == "" {
		return StageNone, nil
	}
	lower := strings.ToLower(s)
	for i, name := range releaseStageNames {
		if i == int(StageOther) {
			continue // "other" is an internal sentinel, never user-selectable
		}
		if name == lower {
			return ReleaseStage(i), nil
		}
	}
	return StageNone, fmt.Errorf(
		"bestiary: ParseReleaseStage: unrecognized release stage %q;"+
			" why: the input does not match any known release stage (case-insensitive);"+
			" where: ParseReleaseStage;"+
			" valid values: none, stable, preview, beta, alpha, experimental, latest, original;"+
			" how to fix: pass one of the valid values listed above",
		s,
	)
}

// DetectReleaseStage reports the ReleaseStage a single, standalone ID token denotes
// and whether it is a recognized ID-detected stage. It is the ID-detection primitive
// (the analogue of detectModelStatus, but exported because the enrichment and codegen
// both consume it):
//
//   - a recognized token (preview / beta / latest / original) → (its constant, true);
//   - any other token, including "stable" / "alpha" / "experimental" / "exp" whose
//     ID-detection is deferred this epoch → (StageNone, false).
//
// It is known-members-only: it NEVER returns StageOther (that reserved bucket is for
// a future non-ID feeder, not the ID path — returning it here would fabricate an
// unreachable state). It matches STANDALONE tokens only (exact, case-insensitive; no
// substring matching) and is family-member-guarded by construction: "stable" is not in
// the detected set, so "stable-diffusion" — whether passed whole or tokenized to
// "stable" + "diffusion" — can never resolve to a stage.
func DetectReleaseStage(tok string) (ReleaseStage, bool) {
	if tok == "" {
		return StageNone, false
	}
	s, ok := idDetectedStages[strings.ToLower(tok)]
	if !ok {
		return StageNone, false
	}
	return s, true
}

// DetectStageFromID scans a model ID for a recognized release-stage marker and
// returns (stage, raw) — mirroring detectModelStatus's return contract:
//
//   - a recognized standalone stage token → (its constant, "");
//   - no recognized token → (StageNone, "").
//
// The second return is the verbatim-token slot reserved for the StageOther path
// (populated only when a future non-ID feeder produces StageOther); the ID path
// never yields StageOther, so it is always "" this epoch — the caller assigns it to
// ModelInfo.StageRaw, which therefore mirrors StatusRaw (non-empty only for the Other
// bucket).
//
// It is a PURE function of the ID and the embedded stage table, so it is applied
// identically at every enrichment joint (codegen bake, wire decode, store read) — the
// same ID always yields the same stage, so a live-sync row and its baked static row
// never disagree. Any provider/path prefix is dropped (up to the last "/") before
// tokenizing so a path segment can never masquerade as a stage; the remaining name is
// split on every non-alphanumeric byte and each token is checked via
// DetectReleaseStage (exact, standalone). The FIRST recognized token (scanning left to
// right) wins — catalog IDs carry at most one stage marker, so the tie-break is
// documentation, not a real ambiguity.
func DetectStageFromID(id ModelID) (ReleaseStage, string) {
	s := string(id)
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		s = s[idx+1:]
	}
	start := -1
	for i := 0; i <= len(s); i++ {
		alnum := i < len(s) && isAlnumByte(s[i])
		if alnum {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			if stage, ok := DetectReleaseStage(s[start:i]); ok {
				// Known ID-detected stage: raw is "" (StageRaw is reserved for the
				// StageOther path, which the ID scan never produces).
				return stage, ""
			}
			start = -1
		}
	}
	return StageNone, ""
}

// isAlnumByte reports whether b is an ASCII letter or digit (the token boundary for
// DetectStageFromID's split). Any other byte — '-', '.', '/', ':', '@', '_', space —
// is a separator.
func isAlnumByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// ValidateNoBetaInIdentity is the LOUD codegen guard for the beta-always-stage rule:
// beta is a RELEASE STAGE, never part of a model's identity.
//
// The two axes are independent by design — DetectStageFromID scans the ID without
// stripping, so a row can carry Stage=StageBeta while its entity key says nothing
// about beta. The failure this guard catches is the COEXISTENCE: a decomposition that
// also promotes the beta token into the key, so one record asserts beta as both a
// stage and an identity. That is not merely redundant, it is contradictory — it splits
// a lab's beta and non-beta spellings of one artifact into two entities that the stage
// axis simultaneously claims are the same model at different maturities.
//
// Scope is exactly two shapes, because those are the two ways a token reaches a key:
//
//   - Variant == "beta"        (the token promoted into the variant slot)
//   - "beta" among the entity's IDENTITY modifiers (the token in the {…} segment)
//
// An ATTRIBUTE-class beta would not reach the key and is not a violation; neither is a
// family or version that merely contains the substring (only whole-token equality
// counts, case-folded). The error names the offending entity key and every model ID
// that landed on it, so a curator can pin the row the way interfaze/interfaze-beta is
// pinned rather than hunting for it.
//
// Codegen calls this over the assembled entity set and ABORTS the bake; the runtime
// never calls it. There is deliberately NO allowlist: the last exception
// (interfaze/beta) was resolved by curation rather than exempted, and an allowlist
// would let the next one accumulate silently.
func ValidateNoBetaInIdentity(entities []Entity) error {
	for _, e := range entities {
		var why string
		switch {
		case strings.EqualFold(e.Ref.Variant, "beta"):
			why = `the variant slot is "beta"`
		case hasFoldedToken(e.Ref.Modifier, "beta"):
			why = `"beta" is an identity modifier (it renders in the {…} key segment)`
		default:
			continue
		}
		ids := make([]string, 0, len(e.Instances))
		for _, in := range e.Instances {
			ids = append(ids, string(in.ID))
		}
		sort.Strings(ids)
		return fmt.Errorf(
			"bestiary stage: entity %q puts beta into its IDENTITY — %s\n"+
				"  What: a decomposition promoted a beta token into the entity key\n"+
				"  Where: the entity key %q, minted from model id(s): %s\n"+
				"  When: validating the assembled entity set at codegen, before any constant is emitted\n"+
				"  Why: beta is a RELEASE STAGE, never an identity. Stage is detected from the ID\n"+
				"       without stripping, so this record now asserts beta on BOTH axes — splitting the\n"+
				"       beta and non-beta spellings of one artifact into separate entities while the\n"+
				"       stage axis says they are the same model at different maturities\n"+
				"  What it means for the caller: the bake is ABORTED; no constants are emitted\n"+
				"  How to fix: add a curated exact-id override in parse.go mapping the offending id to\n"+
				"       its bare identity (see the interfaze/interfaze-beta entry). Stage is unaffected:\n"+
				"       DetectStageFromID keeps reading beta off the id",
			e.Ref.String(), why, e.Ref.String(), strings.Join(ids, ", "),
		)
	}
	return nil
}

// hasFoldedToken reports whether toks contains want under case-folded WHOLE-token
// equality. Substring matching would be wrong here: a family or variant that merely
// contains "beta" (say a hypothetical "betamax") is not a beta release.
func hasFoldedToken(toks []string, want string) bool {
	for _, t := range toks {
		if strings.EqualFold(t, want) {
			return true
		}
	}
	return false
}
