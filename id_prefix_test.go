package bestiary_test

import (
	_ "embed"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

//go:embed testdata/parse/id_prefix_class_corpus.json
var idPrefixClassCorpusJSON []byte

// idPrefixInput is one (id, provider) pair, which is the whole input to the
// classification: the same token means different things under different providers, so
// neither field alone decides.
type idPrefixInput struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// idPrefixExpected pins BOTH halves of the answer — the class the token was given and
// the id the decomposition will actually read. Pinning only the class would let a rule
// fire correctly and then strip the wrong number of characters; pinning only the
// stripped id would let a token be removed for the wrong reason, which is the failure
// mode that erased a backend-host label once.
type idPrefixExpected struct {
	Class    string `json:"class"`
	Stripped string `json:"stripped"`
}

// idPrefixClassCaseCount is the exact-count control. A floor would let a silent case
// drop pass, and the negative rows are the ones a careless edit removes first.
const idPrefixClassCaseCount = 19

// TestClassifyIDPrefix_Corpus drives the production classifier — the same exported
// function both decomposition entrypoints call — over the measured corpus.
//
// The rule under test: a leading id token may be dropped from the decomposition input
// ONLY when a DIFFERENT carrier already holds the fact it names. Two carriers license a
// strip (the Provider field, for a provider's repetition of its own slug; the Creator
// axis, for a lab token the remainder's family already declares), and everything else
// is left byte-identical — including the two cases that look identical from the outside
// and are not: a backend-host label naming a provider that is NOT serving the model,
// and a product-surface namespace that no axis records at all.
func TestClassifyIDPrefix_Corpus(t *testing.T) {
	corpus := loadParseCorpus[idPrefixInput, idPrefixExpected](t, idPrefixClassCorpusJSON, idPrefixClassCaseCount)

	// Value-based coverage: a count-preserving swap must not be able to drop the
	// negative controls, which are what separate this rule from the blanket
	// provider-name strip it replaces, nor the two carrier-positive controls the
	// deferral record named by id. The two glm rows are a MINIMAL PAIR over one family
	// (glm, whose curated creator is zhipu): both leading tokens are in the Creator
	// vocabulary, and only the one the family actually declares licenses a strip. Vocabulary
	// membership alone is therefore not enough, and dropping the carrier conjunct turns this
	// corpus red on its own rather than only through a downstream curated fixture.
	requireInputCoverage(t, corpus, map[idPrefixInput]idPrefixExpected{
		{ID: "azure-gpt-4o", Provider: "nano-gpt"}:                {Class: "none", Stripped: "azure-gpt-4o"},
		{ID: "duo-chat-gpt-5-6-luna", Provider: "gitlab"}:         {Class: "none", Stripped: "duo-chat-gpt-5-6-luna"},
		{ID: "minimax-m2", Provider: "minimax"}:                   {Class: "none", Stripped: "minimax-m2"},
		{ID: "claude-sonnet-4-5", Provider: "anthropic"}:          {Class: "none", Stripped: "claude-sonnet-4-5"},
		{ID: "openai-gpt-5.6-luna", Provider: "snowflake-cortex"}: {Class: "creator", Stripped: "gpt-5.6-luna"},
		{ID: "gpt-5-6-luna", Provider: "kenari"}:                  {Class: "none", Stripped: "gpt-5-6-luna"},
		{ID: "databricks-gpt-5-6-luna", Provider: "databricks"}:   {Class: "self-provider", Stripped: "gpt-5-6-luna"},
		{ID: "zhipu-glm-5", Provider: "nano-gpt"}:                 {Class: "creator", Stripped: "glm-5"},
		{ID: "microsoft-glm-5", Provider: "nano-gpt"}:             {Class: "none", Stripped: "microsoft-glm-5"},
	})

	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			class, stripped := bestiary.ClassifyIDPrefix(bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			if class.String() != c.Expected.Class {
				t.Errorf("ClassifyIDPrefix(%q, %q) class = %q, want %q — the class is the REASON the "+
					"token may or may not go, and a right answer for the wrong reason is how a "+
					"backend-host label gets deleted",
					c.Input.ID, c.Input.Provider, class, c.Expected.Class)
			}
			if string(stripped) != c.Expected.Stripped {
				t.Errorf("ClassifyIDPrefix(%q, %q) id = %q, want %q",
					c.Input.ID, c.Input.Provider, stripped, c.Expected.Stripped)
			}
			if class == bestiary.IDPrefixNone && string(stripped) != c.Input.ID {
				t.Errorf("ClassifyIDPrefix(%q, %q) classified none but returned %q; an unclassified "+
					"token must leave the id byte-identical", c.Input.ID, c.Input.Provider, stripped)
			}
		})
	}
}

// TestClassifyIDPrefix_IsIdempotent fences the property both call sites depend on:
// ParseFamilyDetailed applies the rule and then reaches InferFamilyFromIDWithVariant,
// which applies it again. A rule that kept biting would eat the family on the second
// pass, so every corpus case's OUTPUT must be a fixed point.
func TestClassifyIDPrefix_IsIdempotent(t *testing.T) {
	corpus := loadParseCorpus[idPrefixInput, idPrefixExpected](t, idPrefixClassCorpusJSON, idPrefixClassCaseCount)
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			p := bestiary.Provider(c.Input.Provider)
			_, once := bestiary.ClassifyIDPrefix(bestiary.ModelID(c.Input.ID), p)
			class, twice := bestiary.ClassifyIDPrefix(once, p)
			if twice != once {
				t.Errorf("ClassifyIDPrefix is not idempotent on %q (%s): first pass gave %q, second "+
					"gave %q as %s — both decomposition entrypoints apply this rule, so a second "+
					"bite would remove the family", c.Input.ID, p, once, twice, class)
			}
		})
	}
}

// TestClassifyIDPrefix_StripIsAlwaysAPrefix is the structural guard the per-case
// expectations cannot give on their own: whatever the rule decides, the id it hands
// back must be a SUFFIX of the id it was given. A rule that rewrote the middle of an id
// — or normalized it — would be doing something other than removing a leading token,
// and every downstream extractor reads the result as if it were the vendor's own
// spelling.
//
// The one licensed exception is the Bedrock routing tail on a DOT-separated strip,
// which is removed from the end; those cases are checked by prefix instead.
func TestClassifyIDPrefix_StripIsAlwaysAPrefix(t *testing.T) {
	corpus := loadParseCorpus[idPrefixInput, idPrefixExpected](t, idPrefixClassCorpusJSON, idPrefixClassCaseCount)
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			_, got := bestiary.ClassifyIDPrefix(bestiary.ModelID(c.Input.ID), bestiary.Provider(c.Input.Provider))
			s, in := string(got), c.Input.ID
			if len(s) > len(in) {
				t.Fatalf("ClassifyIDPrefix(%q) returned a LONGER id %q; the rule only removes", in, s)
			}
			at := strings.Index(in, s)
			if at < 0 {
				t.Fatalf("ClassifyIDPrefix(%q) returned %q, which is not a contiguous run of the "+
					"input; the rule must REMOVE text, never rewrite or normalize it — every "+
					"downstream extractor reads the result as the vendor's own spelling", in, s)
			}
			// What was removed from the front must be a whole token: it ends exactly at
			// the separator that followed it. A rule that cut mid-token would leave a
			// stub that decomposes to a family nobody publishes.
			if at > 0 && in[at-1] != '-' && in[at-1] != '.' {
				t.Errorf("ClassifyIDPrefix(%q) cut at offset %d, which is not a token boundary "+
					"(preceding byte %q); the rule removes whole leading tokens only",
					in, at, string(in[at-1]))
			}
			// Anything removed from the END is the Bedrock routing tail, which only a
			// DOT-separated strip licenses.
			if tail := in[at+len(s):]; tail != "" && !strings.Contains(in[:at], ".") {
				t.Errorf("ClassifyIDPrefix(%q) also removed the trailing %q, but its leading token "+
					"was not dot-separated; only the Bedrock inference-profile grammar licenses "+
					"dropping a trailing routing tag", in, tail)
			}
		})
	}
}
