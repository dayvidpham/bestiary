package bestiary

import "testing"

// TestDetectModelStatus exercises the ingest path for the api.json status field:
// an empty/whitespace token is StatusNone with no raw; a recognized token
// (case-insensitive) is its constant with no raw; and an unknown-but-present
// token is StatusOther with the verbatim raw preserved (never dropped).
// Cases live in testdata/metadata/detect_model_status_corpus.json.
func TestDetectModelStatus(t *testing.T) {
	corpus := loadMetaDetectCorpus(t, metaDetectModelStatusCorpusJSON, 7)
	metaRequireInputCoverage(t, corpus, map[string]metaDetectExpected{
		"":             {Token: StatusNone.String(), Raw: ""},
		"BETA":         {Token: StatusBeta.String(), Raw: ""},
		"experimental": {Token: StatusOther.String(), Raw: "experimental"},
	})
	runMetaDetectCorpus(t, corpus, func(in string) (string, string) {
		got, raw := detectModelStatus(in)
		return got.String(), raw
	})
}

// TestDetectLinkType exercises the ingest path for a link type tag: recognized
// tokens map to their constant with no raw; the empty token and an unknown token
// both map to the LinkOther fail-safe (raw non-empty only when the token was
// present but unrecognized).
// Cases live in testdata/metadata/detect_link_type_corpus.json.
func TestDetectLinkType(t *testing.T) {
	corpus := loadMetaDetectCorpus(t, metaDetectLinkTypeCorpusJSON, 6)
	metaRequireInputCoverage(t, corpus, map[string]metaDetectExpected{
		"":        {Token: LinkOther.String(), Raw: ""},
		"weights": {Token: LinkWeights.String(), Raw: ""},
		"forum":   {Token: LinkOther.String(), Raw: "forum"},
	})
	runMetaDetectCorpus(t, corpus, func(in string) (string, string) {
		got, raw := detectLinkType(in)
		return got.String(), raw
	})
}

// TestDetectReasoningOptionKind exercises the ingest path for a reasoning-option
// kind tag: recognized tokens map to their constant with no raw; the empty token
// and an unknown token both map to the ReasoningOptionOther fail-safe (raw
// non-empty only when the token was present but unrecognized).
// Cases live in testdata/metadata/detect_reasoning_option_kind_corpus.json.
func TestDetectReasoningOptionKind(t *testing.T) {
	corpus := loadMetaDetectCorpus(t, metaDetectReasoningOptionKindCorpusJSON, 6)
	metaRequireInputCoverage(t, corpus, map[string]metaDetectExpected{
		"":       {Token: ReasoningOptionOther.String(), Raw: ""},
		"EFFORT": {Token: ReasoningEffort.String(), Raw: ""},
		"custom": {Token: ReasoningOptionOther.String(), Raw: "custom"},
	})
	runMetaDetectCorpus(t, corpus, func(in string) (string, string) {
		got, raw := detectReasoningOptionKind(in)
		return got.String(), raw
	})
}
