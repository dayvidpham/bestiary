package bestiary

import (
	"reflect"
	"strings"
	"testing"
)

// syntheticSuppressionSeed is the TEMPLATE seed a real curated entry follows, used to
// drive the production machinery end-to-end. It is parsed by the production
// parseSuppressionSeed and injected into the production mintEntityNominaWith — there is
// no test-only mint path, only a test-supplied table.
const syntheticSuppressionSeed = `{
  "schema_version": 1,
  "entries": [
    {
      "entity": {"family": "llama", "version": "4", "param_size": "17b-16e", "modifier": ["instruct"]},
      "suppress": ["instruct"],
      "reason": "synthetic fence entry: exercises the suppression machinery end to end",
      "source_url": "https://example.invalid/synthetic-fence"
    }
  ]
}`

// TestSuppression_SyntheticEntry_EndToEnd is the NON-VACUITY fence for the whole
// policy. The shipped seed is empty, so every census assertion elsewhere is a no-op
// proof; this test proves the machinery actually does something when an entry exists,
// by walking one synthetic entry through the production functions and pinning the
// literal before/after pair.
func TestSuppression_SyntheticEntry_EndToEnd(t *testing.T) {
	tbl, err := parseSuppressionSeed([]byte(syntheticSuppressionSeed))
	if err != nil {
		t.Fatalf("parseSuppressionSeed(synthetic): unexpected error: %v", err)
	}
	if got := len(tbl.entries); got != 1 {
		t.Fatalf("synthetic seed has %d entries, want exactly 1", got)
	}

	ref := EntityRef{
		Family:    Family("llama"),
		Version:   "4",
		ParamSize: "17b-16e",
		Modifier:  []string{"instruct"},
	}
	const wantKey = "llama@4#17b-16e{instruct}"
	const wantPreferred = "llama@4#17b-16e"

	// (a) BEFORE: with no seed the preferred value IS the key, byte for byte.
	if got := preferredNomenValueWith(ref, emptySuppressionTable()); got != wantKey {
		t.Fatalf("before suppression: preferred = %q, want the key %q", got, wantKey)
	}

	// (b) AFTER: with the entry, the preferred value omits the redundant modifier.
	if got := preferredNomenValueWith(ref, tbl); got != wantPreferred {
		t.Fatalf("after suppression: preferred = %q, want %q", got, wantPreferred)
	}

	// (c) THE KEY IS UNTOUCHED — identity, store keys, lineage and taxonomy all read
	// this string, and the policy must never move it.
	if got := ref.String(); got != wantKey {
		t.Fatalf("entity key changed under suppression: got %q, want %q", got, wantKey)
	}
	if got := len(ref.Modifier); got != 1 || ref.Modifier[0] != "instruct" {
		t.Fatalf("suppression mutated the caller's ref modifiers: %v", ref.Modifier)
	}

	// (d) STATUS PLUMBING: the entity mints two canonical nomina — the shorter
	// spelling Preferred, the key spelling Admitted — and both resolve to the same
	// untouched entity.
	ent := Entity{
		Ref:       ref,
		Instances: []ProviderInstance{{ID: ModelID("llama-4-scout-17b-16e-instruct"), Provider: ProviderLocal}},
		Sources:   []DataSourceID{DataSourceModelsDev},
	}
	nomina := mintEntityNominaWith(ent, tbl)

	var preferred, admittedKey []Nomen
	for _, n := range nomina {
		if n.Scheme != NomenSchemeCanonical {
			continue
		}
		switch n.Status {
		case AcceptabilityPreferred:
			preferred = append(preferred, n)
		case AcceptabilityAdmitted:
			admittedKey = append(admittedKey, n)
		}
	}
	if len(preferred) != 1 || preferred[0].Value != wantPreferred {
		t.Fatalf("canonical Preferred nomina = %+v, want exactly one valued %q", preferred, wantPreferred)
	}
	if len(admittedKey) != 1 || admittedKey[0].Value != wantKey {
		t.Fatalf("canonical Admitted nomina = %+v, want exactly one valued %q (the key spelling is recorded, never dropped)", admittedKey, wantKey)
	}
	if got := preferred[0].ResolvesTo.String(); got != wantKey {
		t.Fatalf("preferred nomen resolves to %q, want the untouched key %q", got, wantKey)
	}
	if got := admittedKey[0].ResolvesTo.String(); got != wantKey {
		t.Fatalf("admitted nomen resolves to %q, want the untouched key %q", got, wantKey)
	}

	// (e) REVERSIBILITY: drop the entry and the preferred value grows the modifier
	// back — the policy is computed, never baked.
	if got := preferredNomenValueWith(ref, emptySuppressionTable()); got != wantKey {
		t.Fatalf("after deleting the entry: preferred = %q, want the key %q back", got, wantKey)
	}
	reverted := mintEntityNominaWith(ent, emptySuppressionTable())
	for _, n := range reverted {
		if n.Scheme == NomenSchemeCanonical && n.Status == AcceptabilityAdmitted {
			t.Fatalf("after deleting the entry an admitted canonical nomen %q survives; the reversal must be total", n.Value)
		}
	}
}

// TestSuppression_SuppressedModifiersLookup pins the seed lookup itself: a hit returns
// the sorted tokens, a miss returns nil, and the returned slice is a copy.
func TestSuppression_SuppressedModifiersLookup(t *testing.T) {
	tbl, err := parseSuppressionSeed([]byte(syntheticSuppressionSeed))
	if err != nil {
		t.Fatalf("parseSuppressionSeed(synthetic): %v", err)
	}
	hit := EntityRef{Family: Family("llama"), Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}}
	got := suppressedModifiersWith(hit, tbl)
	if !reflect.DeepEqual(got, []string{"instruct"}) {
		t.Fatalf("suppressedModifiersWith(hit) = %v, want [instruct]", got)
	}
	got[0] = "mutated"
	if again := suppressedModifiersWith(hit, tbl); again[0] != "instruct" {
		t.Fatalf("suppressedModifiersWith returned an aliased slice: table now reads %v", again)
	}
	miss := EntityRef{Family: Family("llama"), Version: "4", Modifier: []string{"instruct"}}
	if got := suppressedModifiersWith(miss, tbl); got != nil {
		t.Fatalf("suppressedModifiersWith(miss) = %v, want nil (a different #size is a different entity)", got)
	}
}

// TestParseSuppressionSeed_Rejects pins the LOUD curation guards: every rejection is a
// mutation of one valid entry, so no case is vacuous.
func TestParseSuppressionSeed_Rejects(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSub string
	}{
		{
			name:    "unknown family",
			raw:     `{"entries":[{"entity":{"family":"definitely-not-a-family","modifier":["instruct"]},"suppress":["instruct"],"reason":"r"}]}`,
			wantSub: "unknown base family",
		},
		{
			name:    "empty reason",
			raw:     `{"entries":[{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":["instruct"],"reason":"  "}]}`,
			wantSub: "empty reason",
		},
		{
			name:    "modifier not carried by the entity",
			raw:     `{"entries":[{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":["thinking"],"reason":"r"}]}`,
			wantSub: "is not an identity modifier of the entity",
		},
		{
			name:    "no modifiers suppressed",
			raw:     `{"entries":[{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":[],"reason":"r"}]}`,
			wantSub: "no modifiers suppressed",
		},
		{
			name:    "empty suppress token",
			raw:     `{"entries":[{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":[" "],"reason":"r"}]}`,
			wantSub: "empty suppress token",
		},
		{
			name: "duplicate entity",
			raw: `{"entries":[
				{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":["instruct"],"reason":"r"},
				{"entity":{"family":"llama","version":"4","modifier":["instruct"]},"suppress":["instruct"],"reason":"r"}]}`,
			wantSub: "duplicate entity",
		},
		{
			name:    "malformed json",
			raw:     `{"entries":[`,
			wantSub: "JSON unmarshal failed",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseSuppressionSeed([]byte(tc.raw))
			if err == nil {
				t.Fatalf("parseSuppressionSeed accepted an invalid seed; want an error mentioning %q", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestSafeSuppressionTable_DegradesToEmpty pins the graceful-degrade contract (the
// lineage.go precedent): a corrupt seed yields a non-nil EMPTY table, so every
// preferred value falls back to its entity key rather than panicking.
func TestSafeSuppressionTable_DegradesToEmpty(t *testing.T) {
	tbl := emptySuppressionTable()
	if tbl == nil || tbl.byKey == nil {
		t.Fatal("emptySuppressionTable returned a nil table or nil index")
	}
	ref := EntityRef{Family: Family("llama"), Version: "4", Modifier: []string{"instruct"}}
	if got, want := preferredNomenValueWith(ref, tbl), ref.String(); got != want {
		t.Fatalf("degraded preferred value = %q, want the key %q", got, want)
	}
}

// TestValidateSuppression_CatalogGuards pins the two entity-relative codegen guards:
// an entry naming an absent entity, and a suppression that would make two entities
// prefer one spelling.
func TestValidateSuppression_CatalogGuards(t *testing.T) {
	tbl, err := parseSuppressionSeed([]byte(syntheticSuppressionSeed))
	if err != nil {
		t.Fatalf("parseSuppressionSeed(synthetic): %v", err)
	}
	seeded := EntityRef{Family: Family("llama"), Version: "4", ParamSize: "17b-16e", Modifier: []string{"instruct"}}
	plain := EntityRef{Family: Family("llama"), Version: "4", ParamSize: "17b-16e"}

	// Absent entity: the seeded entity is not in the set at all.
	if err := validateSuppressionWith([]Entity{{Ref: plain}}, tbl); err == nil {
		t.Fatal("ValidateSuppression accepted an entry naming an absent entity")
	} else if !strings.Contains(err.Error(), "absent entity") {
		t.Fatalf("error %q does not mention the absent entity", err.Error())
	}

	// Collision: suppressing {instruct} would make the seeded entity prefer the plain
	// entity's key — two entities, one preferred spelling.
	if err := validateSuppressionWith([]Entity{{Ref: seeded}, {Ref: plain}}, tbl); err == nil {
		t.Fatal("ValidateSuppression accepted a preferred-naming collision")
	} else if !strings.Contains(err.Error(), "collision") {
		t.Fatalf("error %q does not mention the collision", err.Error())
	}

	// Clean: the seeded entity alone suppresses without colliding.
	if err := validateSuppressionWith([]Entity{{Ref: seeded}}, tbl); err != nil {
		t.Fatalf("ValidateSuppression rejected a valid seed: %v", err)
	}
}
