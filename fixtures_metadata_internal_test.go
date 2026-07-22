package bestiary

// Embedded JSON case corpora for the internal models.dev metadata ingest-detector
// tests (detectModelStatus / detectLinkType / detectReasoningOptionKind), plus their
// shared input/expected types and runner. This lives in package bestiary (internal) so
// it can reach the unexported detectors; it therefore cannot reuse the external
// bestiary_test loaders and carries its own thin ones, exactly as
// fixtures_midid_internal_test.go does. See TESTING.md.

import (
	_ "embed"
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
	tcassert "github.com/dayvidpham/bestiary/testcase/assert"
)

//go:embed testdata/metadata/detect_model_status_corpus.json
var metaDetectModelStatusCorpusJSON []byte

//go:embed testdata/metadata/detect_link_type_corpus.json
var metaDetectLinkTypeCorpusJSON []byte

//go:embed testdata/metadata/detect_reasoning_option_kind_corpus.json
var metaDetectReasoningOptionKindCorpusJSON []byte

// metaDetectExpected is the (enum, raw) pair every ingest detector returns. Token is
// the enum member's wire token — comparing on the token rather than the int keeps the
// corpus readable and makes an accidental iota renumbering visible. Raw is the verbatim
// upstream token, non-empty ONLY when the value landed in the fail-safe Other bucket
// while actually being present.
type metaDetectExpected struct {
	Token string `json:"token"`
	Raw   string `json:"raw"`
}

// loadMetaDetectCorpus loads a detector corpus under the exact case-count control
// (wantN, the pre-migration inline row count) and the non-vacuity guard.
func loadMetaDetectCorpus(t *testing.T, data []byte, wantN int) testcase.Corpus[string, metaDetectExpected] {
	t.Helper()
	corpus, err := testcase.LoadCorpus[string, metaDetectExpected](data)
	if err != nil {
		t.Fatalf("load metadata detector corpus: %v", err)
	}
	if got := len(corpus.Cases); got != wantN {
		t.Fatalf("metadata detector corpus has %d cases, want exactly %d", got, wantN)
	}
	tcassert.RequireValid(t, corpus)
	return corpus
}

// runMetaDetectCorpus drives one detector over every case. detect returns the enum's
// wire token plus the preserved raw, so all three detectors share this runner despite
// returning three different enum types.
func runMetaDetectCorpus(t *testing.T, corpus testcase.Corpus[string, metaDetectExpected], detect func(string) (string, string)) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			token, raw := detect(c.Input)
			if token != c.Expected.Token {
				t.Errorf("detect(%q) token = %q, want %q", c.Input, token, c.Expected.Token)
			}
			if raw != c.Expected.Raw {
				t.Errorf("detect(%q) raw = %q, want %q", c.Input, raw, c.Expected.Raw)
			}
		})
	}
}

// metaRequireInputCoverage is the internal-package value-coverage guard: it asserts the
// load-bearing inputs are still present BY VALUE with their expected result, catching a
// count-preserving swap the exact-count control cannot see.
func metaRequireInputCoverage(t *testing.T, corpus testcase.Corpus[string, metaDetectExpected], probes map[string]metaDetectExpected) {
	t.Helper()
	have := make(map[string]metaDetectExpected, len(corpus.Cases))
	for _, c := range corpus.Cases {
		have[c.Input] = c.Expected
	}
	for in, want := range probes {
		got, ok := have[in]
		if !ok {
			t.Errorf("value coverage lost: no case with input %q", in)
			continue
		}
		if got != want {
			t.Errorf("value coverage: input %q has expected %+v, want %+v", in, got, want)
		}
	}
}
