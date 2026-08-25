package bestiary

import (
	"strings"
	"testing"
)

// TestParseCreatorProviderTable_Valid asserts a well-formed relation parses, that
// lookups return the curated providers, and — the load-bearing part — that the
// provider order is preserved VERBATIM from the file rather than sorted.
//
// The order is the lab's primacy order and resolution picks the earliest listed
// surface present among a model's hosts, so a sort here would silently change which
// provider `bestiary show` renders. The fixture deliberately lists providers in
// non-alphabetical order so a re-introduced sort fails.
func TestParseCreatorProviderTable_Valid(t *testing.T) {
	raw := []byte(`{
	  "schema_version": 1,
	  "creator_providers": [
	    { "creator": "zhipu", "providers": ["zhipuai", "zai"] },
	    { "creator": "openai", "providers": ["openai"] }
	  ]
	}`)
	tbl, err := parseCreatorProviderTable(raw)
	if err != nil {
		t.Fatalf("parseCreatorProviderTable: %v", err)
	}
	got := tbl.byCreator[CreatorZhipu]
	want := []Provider{"zhipuai", "zai"}
	if len(got) != len(want) {
		t.Fatalf("zhipu providers = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("zhipu providers = %v, want %v (CURATION ORDER, not sorted — "+
				"the first entry is the surface resolution prefers)", got, want)
		}
	}
}

// TestParseCreatorProviderTable_Rejects covers every validation arm, each with the
// actionable-message fragment a curator would search for. Each case is a distinct
// curation slip that would otherwise degrade silently: an unreachable row, an
// order-dependent lookup, a row that can never match, or an inflated report.
func TestParseCreatorProviderTable_Rejects(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantFrag string
	}{
		{
			name:     "unknown creator",
			raw:      `{"creator_providers":[{"creator":"not-a-lab","providers":["openai"]}]}`,
			wantFrag: "unknown creator",
		},
		{
			name:     "duplicate creator row",
			raw:      `{"creator_providers":[{"creator":"openai","providers":["openai"]},{"creator":"openai","providers":["azure"]}]}`,
			wantFrag: "duplicate creator_providers.json creator",
		},
		{
			name:     "empty provider list",
			raw:      `{"creator_providers":[{"creator":"openai","providers":[]}]}`,
			wantFrag: "empty provider list",
		},
		{
			name:     "unknown provider",
			raw:      `{"creator_providers":[{"creator":"openai","providers":["not-a-provider"]}]}`,
			wantFrag: "unknown provider",
		},
		{
			name:     "duplicate provider within a row",
			raw:      `{"creator_providers":[{"creator":"openai","providers":["openai","openai"]}]}`,
			wantFrag: "duplicate creator_providers.json provider",
		},
		{
			name:     "malformed JSON",
			raw:      `{"creator_providers":`,
			wantFrag: "JSON unmarshal failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCreatorProviderTable([]byte(tc.raw))
			if err == nil {
				t.Fatalf("parseCreatorProviderTable(%s) = nil error, want a LOUD rejection", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantFrag) {
				t.Errorf("error message missing %q; got:\n%v", tc.wantFrag, err)
			}
		})
	}
}

// TestSafeCreatorProviderTable_Degrades asserts the runtime degrade seam: a load
// failure yields a non-nil EMPTY table, so Creator.Providers returns an empty slice
// and creator-first selection becomes a no-op that leaves the canonical-provider
// preference in charge — never a panic, and never a nil map dereference.
func TestSafeCreatorProviderTable_Degrades(t *testing.T) {
	for _, tc := range []struct {
		name string
		tbl  *creatorProviderTable
		err  error
	}{
		{name: "load error", tbl: nil, err: errForTest("boom")},
		{name: "nil table, nil error", tbl: nil, err: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := safeCreatorProviderTable(tc.tbl, tc.err)
			if got == nil {
				t.Fatal("safeCreatorProviderTable returned nil; it must never return nil")
			}
			if got.byCreator == nil {
				t.Fatal("degraded table has a nil map; lookups would still be safe but the invariant is non-nil")
			}
			if len(got.byCreator[CreatorOpenAI]) != 0 {
				t.Errorf("degraded table resolved providers for openai: %v", got.byCreator[CreatorOpenAI])
			}
		})
	}
}

// errForTest is a minimal error value for the degrade-seam table above.
type errForTest string

func (e errForTest) Error() string { return string(e) }

// TestCreatorTable_WithheldRejects covers the withhold-array validation arms added to
// the Family→Creator loader.
func TestCreatorTable_WithheldRejects(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantFrag string
	}{
		{
			name:     "unknown family",
			raw:      `{"withheld":[{"family":"not-a-family","reason":"because"}]}`,
			wantFrag: "unknown family",
		},
		{
			name:     "empty reason",
			raw:      `{"withheld":[{"family":"ling","reason":"   "}]}`,
			wantFrag: "empty reason",
		},
		{
			name:     "mapped and withheld",
			raw:      `{"creators":[{"family":"ling","creator":"meta"}],"withheld":[{"family":"ling","reason":"because"}]}`,
			wantFrag: "both mapped and withheld",
		},
		{
			name:     "duplicate withholding",
			raw:      `{"withheld":[{"family":"ling","reason":"a"},{"family":"ling","reason":"b"}]}`,
			wantFrag: "duplicate creators.json withheld family",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCreatorTable([]byte(tc.raw))
			if err == nil {
				t.Fatalf("parseCreatorTable(%s) = nil error, want a LOUD rejection", tc.raw)
			}
			if !strings.Contains(err.Error(), tc.wantFrag) {
				t.Errorf("error message missing %q; got:\n%v", tc.wantFrag, err)
			}
		})
	}
}

// TestProviderPreferenceScore_Layering asserts the shared preference authority puts
// the two axes in the documented order and separates them from rehosts.
//
// claude/anthropic is the coincident case (Anthropic creates AND hosts), llama/local
// is the canonical-only case (the curated canonical provider is not a Meta surface),
// llama/llama is the creator-only case, and an arbitrary slug is the rehost floor.
func TestProviderPreferenceScore_Layering(t *testing.T) {
	creatorScore := providerPreferenceScore("claude", ProviderAnthropic)
	if creatorScore >= providerScoreCreatorMax {
		t.Errorf("claude/anthropic score = %d, want the creator tier (< %d)", creatorScore, providerScoreCreatorMax)
	}
	if got := providerPreferenceScore("llama", ProviderLocal); got != providerScoreCanonical {
		t.Errorf("llama/local score = %d, want the canonical tier (%d)", got, providerScoreCanonical)
	}
	if got := providerPreferenceScore("llama", Provider("llama")); got >= providerScoreCreatorMax {
		t.Errorf("llama/llama score = %d, want the creator tier (< %d)", got, providerScoreCreatorMax)
	}
	if got := providerPreferenceScore("llama", Provider("deepinfra")); got != providerScoreRehost {
		t.Errorf("llama/deepinfra score = %d, want the rehost tier (%d)", got, providerScoreRehost)
	}
	// An empty provider is never evidence of first-party hosting.
	if got := providerPreferenceScore("llama", Provider("")); got != providerScoreRehost {
		t.Errorf("llama/\"\" score = %d, want the rehost tier (%d)", got, providerScoreRehost)
	}
	// Ordering: creator strictly outranks canonical, which strictly outranks rehost.
	if !(creatorScore < providerScoreCanonical && providerScoreCanonical < providerScoreRehost) {
		t.Errorf("tier ordering broken: creator=%d canonical=%d rehost=%d",
			creatorScore, providerScoreCanonical, providerScoreRehost)
	}
}

// TestPreferredCreatorProvider_UsesCurationOrder asserts the winner among several
// present creator surfaces is the EARLIEST curated one, not the alphabetically first,
// and that the three fall-through cases report "no creator surface" so resolution
// hands off to the canonical-provider preference untouched.
func TestPreferredCreatorProvider_UsesCurationOrder(t *testing.T) {
	// glm's creator (zhipu) curates zhipuai ahead of zai. Alphabetically "zai" would
	// win; the curated order must.
	got, ok := preferredCreatorProvider("glm", []Provider{"zai", "openrouter", "zhipuai"})
	if !ok {
		t.Fatal("preferredCreatorProvider(glm, [zai openrouter zhipuai]) reported no creator surface")
	}
	if got != Provider("zhipuai") {
		t.Errorf("preferredCreatorProvider = %q, want %q (curation order, not alphabetical)", got, "zhipuai")
	}

	for _, tc := range []struct {
		name      string
		family    Family
		providers []Provider
	}{
		{name: "no creator", family: "definitely-not-a-family", providers: []Provider{"openai"}},
		{name: "creator with no curated surfaces", family: "flux", providers: []Provider{"openai"}},
		{name: "creator surfaces absent from the host set", family: "glm", providers: []Provider{"openrouter", "deepinfra"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if p, ok := preferredCreatorProvider(tc.family, tc.providers); ok {
				t.Errorf("preferredCreatorProvider(%q, %v) = (%q, true), want no creator surface so the "+
					"canonical-provider preference stays in charge", tc.family, tc.providers, p)
			}
		})
	}
}
