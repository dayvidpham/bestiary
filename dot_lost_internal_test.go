package bestiary

import (
	"testing"

	"github.com/dayvidpham/bestiary/testcase"
)

// TestDotLostVersion_Corpus pins every dot-lost version-spelling repair to the entity it
// now resolves to. A "dot-lost" id spells a minor version without its separating dot —
// dotless ("minimax-m25", "qwen35-122b-a10b") or dash-glued onto a digit-suffixed family
// token ("qwen2-5-7b-instruct", "qwen3-6-plus") — so the decomposition captured only the
// leading integer and lost the minor. The corrected version comes from the curated
// dotLostVersionOverrides table; each row here fences the OUTCOME (the id lands on the
// real dotted entity) that the ruling asked for.
func TestDotLostVersion_Corpus(t *testing.T) {
	corpus := loadInternalCorpus[pNotationInput, pNotationExpected](t, internalDotLostVersionCorpusJSON, 33)

	// Value coverage (by id): one representative of each transform class plus the three
	// RE-KEYs (no same-key sibling) must be present, so a count-preserving swap cannot
	// silently drop one.
	requireEntityKeyCoverage(t, corpus, map[string]string{
		"minimax-m25":                   "minimax/m@2.5",
		"qwen35-397b-a17b":              "qwen@3.5#397b-a17b",
		"mistral-small-31-24b-instruct": "mistral/small@3.1#24b{instruct}",
		"qwen2-5-7b-instruct":           "qwen@2.5#7b{instruct}",
		"qwen2-5-coder-7b-instruct":     "qwen/coder@2.5#7b{instruct}", // RE-KEY
		"qwen2-5-omni-7b":               "qwen@2.5#7b{omni}",           // RE-KEY
		"qwen3-6-35b":                   "qwen@3.6#35b",                // RE-KEY
		"qwen3-7-plus":                  "qwen/plus@3.7",
	})

	runEntityKeyCorpus(t, corpus)
}

// TestParamSize1T_Corpus pins the trillion param-size routing: "1t" (Ling-1T / Ring-1T are
// 1-trillion-parameter models) is a SIZE, not a version, so it re-keys ling@1t → ling#1t
// and rides as the size beside a version (ling@2.6#1t). The two NEGATIVE CONTROLS guard
// the blast radius: the ollama ":1t" tag on kimi-k2:1t (suppress-pinned) and the
// token-internal "r1t2" in deepseek-tng-r1t2-chimera must be unaffected.
func TestParamSize1T_Corpus(t *testing.T) {
	corpus := loadInternalCorpus[pNotationInput, pNotationExpected](t, internalParamSize1TCorpusJSON, 8)

	requireEntityKeyCoverage(t, corpus, map[string]string{
		"Ling-1T":                 "ling#1t",
		"inclusionai/ring-2.6-1t": "ring@2.6#1t",
		"ring-2.6-1t-free":        "ring@2.6#1t",
		// kimi-k2:1t (the ollama :1t-tag negative control) retired with its upstream row, 2026-07-23 refresh.
		"deepseek-tng-r1t2-chimera": "deepseek", // NEGATIVE CONTROL (token-internal r1t2)
	})

	runEntityKeyCorpus(t, corpus)
}

// requireEntityKeyCoverage asserts each probe id is present in the corpus by value (id +
// expected key), independent of which provider row the corpus pinned — the guard the
// exact-count control cannot provide.
func requireEntityKeyCoverage(t *testing.T, corpus testcase.Corpus[pNotationInput, pNotationExpected], probes map[string]string) {
	t.Helper()
	have := make(map[string]string, len(corpus.Cases))
	for _, c := range corpus.Cases {
		have[c.Input.ID] = c.Expected.EntityKey
	}
	for id, wantKey := range probes {
		got, ok := have[id]
		if !ok {
			t.Errorf("value coverage lost: no corpus case for id %q", id)
			continue
		}
		if got != wantKey {
			t.Errorf("value coverage: corpus pins id %q to %q, want %q", id, got, wantKey)
		}
	}
}

// runEntityKeyCorpus drives a (provider,id) -> entity-key corpus: it looks up each pinned
// catalog row, rebuilds its EntityRef the way the registry does, and asserts the key it
// resolves to AND that an entity is really filed under that key (so a row can never pass
// by naming a key nothing exists under). It mirrors the catalog-level half of
// TestPNotationVersion_Corpus.
func runEntityKeyCorpus(t *testing.T, corpus testcase.Corpus[pNotationInput, pNotationExpected]) {
	t.Helper()
	for _, c := range corpus.Cases {
		t.Run(c.Name, func(t *testing.T) {
			m, ok := LookupModelByProvider(Provider(c.Input.Provider), c.Input.ID)
			if !ok {
				t.Fatalf("LookupModelByProvider(%q, %q) = false; the pinned catalog row is gone",
					c.Input.Provider, c.Input.ID)
			}
			ref := EntityRef{
				Family:    m.Family,
				Variant:   m.Variant,
				Version:   m.Version,
				ParamSize: m.ParamSize,
				Modifier:  EntityModifiers(m.Modifier, m.Family),
			}
			if got := ref.String(); got != c.Expected.EntityKey {
				t.Errorf("%q (%s) resolves to entity %q, want %q",
					c.Input.ID, c.Input.Provider, got, c.Expected.EntityKey)
			}
			if _, ok := EntityByKey(c.Expected.EntityKey); !ok {
				t.Errorf("entity %q is absent from the registry", c.Expected.EntityKey)
			}
		})
	}
}
