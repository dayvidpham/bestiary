package bestiary

import (
	"fmt"
	"sort"
	"strings"
)

// --------------------------------------------------------------------------
// models.dev LAB-PREFIX derivation for the Family→Creator dimension.
//
// Every models.dev metadata id is lab-scoped ("<lab>/<model>"), so the catalog
// already asserts an originator for each metadata row. Projecting that assertion
// onto the family the JOIN ITSELF decomposes the row to yields a Family→Creator
// candidate that needs no independent guess. The projection is only SAFE where it
// is unambiguous, so this file computes the ambiguity rather than papering over it:
// DeriveCreatorLabDisagreements reports every family whose lab evidence conflicts —
// with more than one lab, with an existing curated row, or with a curated
// withholding — and codegen emits that report as a committed artifact.
//
// The derived rows themselves are materialized INTO parse/data/creators.json by a
// curator, not applied at load time. That keeps ONE source of truth for the
// dimension (the curated file the loud codegen guard validates) instead of a table
// whose contents depend on which metadata happened to be baked.
// --------------------------------------------------------------------------

// CreatorLabClass classifies WHY a family's lab evidence was not auto-applied. It is
// a closed int enum: the set of ways the evidence can conflict is fixed by the
// derivation itself, so callers can switch on it exhaustively.
type CreatorLabClass int

const (
	// CreatorLabClassNone is the zero value: no conflict. It is never carried by a
	// reported row (a family with no conflict produces no row at all) and exists so
	// the zero value of the enum is not a meaningful classification.
	CreatorLabClassNone CreatorLabClass = iota
	// CreatorLabClassMultiOrg means MORE THAN ONE lab prefix reaches this family, so
	// the catalog itself disagrees about the originator. This is the genuine
	// multi-organization case: a lab's weights re-published under another lab's
	// prefix (an NVIDIA-tuned Llama is still filed under "nvidia/…"). No single lab
	// can be applied without silently picking a winner.
	CreatorLabClassMultiOrg
	// CreatorLabClassSpellingVariant means exactly one lab reaches the family, it
	// differs from the curated creator, and one token is a PREFIX of the other — the
	// same organization spelled two ways ("zhipu" curated vs the "zhipuai" lab
	// prefix). Applying it would churn the token without changing the fact.
	CreatorLabClassSpellingVariant
	// CreatorLabClassDivergent means exactly one lab reaches the family, it differs
	// from the curated creator, and the two tokens are NOT prefix-related — the
	// catalog names a materially different organization than the curation does. This
	// is the class that warrants a human look: one of the two is wrong.
	CreatorLabClassDivergent
	// CreatorLabClassWithheld means the family is on the curated withhold list in
	// creators.json. The derivation may be perfectly unambiguous; it is held back
	// deliberately, for the reason the withhold row carries.
	CreatorLabClassWithheld
)

// String returns the machine token for the class, which is also its JSON encoding.
func (c CreatorLabClass) String() string {
	switch c {
	case CreatorLabClassNone:
		return "none"
	case CreatorLabClassMultiOrg:
		return "multi-org"
	case CreatorLabClassSpellingVariant:
		return "spelling-variant"
	case CreatorLabClassDivergent:
		return "divergent"
	case CreatorLabClassWithheld:
		return "withheld"
	default:
		return fmt.Sprintf("CreatorLabClass(%d)", int(c))
	}
}

// MarshalText implements encoding.TextMarshaler so the class encodes as its token
// rather than as an opaque integer in the committed codegen report.
func (c CreatorLabClass) MarshalText() ([]byte, error) {
	return []byte(c.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler, making the committed report
// ROUND-TRIPPABLE. Marshalling without the reverse direction would emit an artifact
// that the emitting program itself cannot read back — the tests that verify the
// report's content, and any future consumer of it, would each have to re-implement
// the token table.
//
// It is STRICT: an unrecognized token is an actionable error rather than a silent
// CreatorLabClassNone, because this decodes a codegen-emitted artifact whose
// vocabulary is fixed. A tolerant decode here would turn a renamed class into a
// report that parses cleanly and means something different.
func (c *CreatorLabClass) UnmarshalText(b []byte) error {
	if c == nil {
		return fmt.Errorf("bestiary: CreatorLabClass.UnmarshalText: nil receiver")
	}
	switch string(b) {
	case "none":
		*c = CreatorLabClassNone
	case "multi-org":
		*c = CreatorLabClassMultiOrg
	case "spelling-variant":
		*c = CreatorLabClassSpellingVariant
	case "divergent":
		*c = CreatorLabClassDivergent
	case "withheld":
		*c = CreatorLabClassWithheld
	default:
		return fmt.Errorf(
			"bestiary: CreatorLabClass.UnmarshalText: unknown class token %q\n"+
				"  What: a creator lab-disagreement class token was not recognized\n"+
				"  Where: decoding parse/data/creators_lab_disagreements.json (or an equivalent payload)\n"+
				"  Why: the class vocabulary is closed — none, multi-org, spelling-variant, divergent, withheld\n"+
				"  How to fix: regenerate the report with `go generate ./...`, or add the new class to CreatorLabClass and this decoder together",
			b,
		)
	}
	return nil
}

// CreatorLabDisagreement is one family whose models.dev lab evidence was NOT
// auto-applied to the Family→Creator dimension, together with everything a curator
// needs to resolve it without re-deriving anything: the labs the catalog offers, how
// many metadata rows back them, what (if anything) the curated table already says,
// and which conflict class it falls in.
type CreatorLabDisagreement struct {
	// Family is the family the join's own decomposition maps the rows to.
	Family Family `json:"family"`
	// CuratedCreator is what parse/data/creators.json already says for this family,
	// or CreatorNone when the family carries no curated row.
	CuratedCreator Creator `json:"curated_creator"`
	// Labs are the distinct models.dev lab prefixes reaching this family, sorted
	// ascending. Always at least one entry.
	Labs []string `json:"labs"`
	// Count is the number of metadata rows behind this family across all its labs —
	// the weight of the evidence, so a one-row curiosity is not mistaken for a
	// systematic disagreement.
	Count int `json:"count"`
	// Class is why the evidence was not applied.
	Class CreatorLabClass `json:"class"`
	// Reason is the human-readable explanation. For a withheld family it is the
	// curated withhold reason verbatim; otherwise it is derived from the class.
	Reason string `json:"reason"`
}

// creatorLabEvidence is the per-family accumulation of lab evidence during the sweep.
type creatorLabEvidence struct {
	labs  map[string]int
	count int
}

// metadataLabPrefix returns the leading "<lab>" segment of a models.dev metadata id
// and whether the id carried one. It is the Creator-axis twin of stripMetadataLab,
// which returns the OTHER half of the same cut, so the two can never disagree about
// where the boundary is.
func metadataLabPrefix(id string) (string, bool) {
	lab, _, found := strings.Cut(id, "/")
	if !found || lab == "" {
		return "", false
	}
	return lab, true
}

// creatorLabFamily resolves the family a metadata row belongs to for the purposes of
// this derivation, using THE JOIN'S OWN decomposition so the seed can never disagree
// with the entity keys it is supposed to describe: a curated modelsdev_aliases.json
// entry is the SOLE identity when one exists (matching the alias-first rule in
// JoinEntityMetadata), and otherwise the row decomposes through metadataEntityRef
// (stripMetadataLab + ParseFamilyDetailed).
func creatorLabFamily(id MetadataID) Family {
	if ref, ok := metadataAliasRef(id); ok {
		return ref.Family
	}
	return metadataEntityRef(id).Family
}

// isSpellingVariant reports whether two organization tokens are the same name spelled
// two ways, under the narrow and mechanical rule that one is a prefix of the other
// ("zhipu" / "zhipuai"). It deliberately recognizes nothing subtler: a broader
// similarity heuristic would start quietly merging genuinely different labs, and the
// cost of a false negative here is only that a row is filed as divergent — which asks
// a human to look, exactly as it should.
func isSpellingVariant(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// DeriveCreatorLabDisagreements sweeps the supplied models.dev entity metadata and
// returns every family whose lab evidence was NOT auto-applied to the Family→Creator
// dimension, sorted ascending by family so the emission is byte-stable.
//
// A family produces NO row when the evidence is unambiguous and already agrees with
// curation — exactly one lab reaches it and either there is no curated row or the
// curated row names that same lab. Everything else is reported:
//
//   - more than one lab reaches the family (CreatorLabClassMultiOrg);
//   - one lab, but it disagrees with the curated row, either as a spelling of the
//     same organization (CreatorLabClassSpellingVariant) or as a materially
//     different one (CreatorLabClassDivergent);
//   - the family is on the curated withhold list (CreatorLabClassWithheld), which
//     takes precedence over the other classes so a deliberate deferral is never
//     re-reported as an accident.
//
// It reads no wall clock and iterates no map in output position, so two runs over the
// same metadata produce byte-identical results.
func DeriveCreatorLabDisagreements(meta []EntityMetadata) []CreatorLabDisagreement {
	evidence := make(map[Family]*creatorLabEvidence)
	for _, m := range meta {
		lab, ok := metadataLabPrefix(string(m.MetadataID))
		if !ok {
			continue
		}
		fam := creatorLabFamily(m.MetadataID)
		if fam == "" {
			continue
		}
		ev := evidence[fam]
		if ev == nil {
			ev = &creatorLabEvidence{labs: map[string]int{}}
			evidence[fam] = ev
		}
		ev.labs[lab]++
		ev.count++
	}

	tbl := loadCreatorTableSafe()
	out := make([]CreatorLabDisagreement, 0, len(evidence))
	for fam, ev := range evidence {
		labs := make([]string, 0, len(ev.labs))
		for lab := range ev.labs {
			labs = append(labs, lab)
		}
		sort.Strings(labs)
		curated := tbl.byFamily[fam]

		class := CreatorLabClassNone
		reason := ""
		switch {
		case tbl.withheldReason(fam) != "":
			class = CreatorLabClassWithheld
			reason = tbl.withheldReason(fam)
		case len(labs) > 1:
			class = CreatorLabClassMultiOrg
			reason = fmt.Sprintf(
				"%d models.dev lab prefixes (%s) reach this family, so the catalog itself disagrees about the originator; applying any one of them would silently pick a winner",
				len(labs), strings.Join(labs, ", "),
			)
		case curated == CreatorNone || string(curated) == labs[0]:
			// Unambiguous and consistent with curation: nothing to report.
			continue
		case isSpellingVariant(string(curated), labs[0]):
			class = CreatorLabClassSpellingVariant
			reason = fmt.Sprintf(
				"the curated creator %q and the lab prefix %q are prefix-related spellings of one organization; applying the lab spelling would churn the token without changing the fact",
				curated, labs[0],
			)
		default:
			class = CreatorLabClassDivergent
			reason = fmt.Sprintf(
				"the curated creator %q and the lab prefix %q name materially different organizations; one of the two is wrong and a human must decide which",
				curated, labs[0],
			)
		}

		out = append(out, CreatorLabDisagreement{
			Family:         fam,
			CuratedCreator: curated,
			Labs:           labs,
			Count:          ev.count,
			Class:          class,
			Reason:         reason,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Family < out[j].Family })
	return out
}

// CreatorLabDisagreementsFromBaked runs DeriveCreatorLabDisagreements over the
// compiled-in models.dev metadata. It is the accessor the runtime and the tests share
// with codegen, so nothing needs a second copy of the sweep.
func CreatorLabDisagreementsFromBaked() []CreatorLabDisagreement {
	return DeriveCreatorLabDisagreements(staticEntityMetadata())
}
