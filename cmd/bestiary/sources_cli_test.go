package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// sizedCuratedKey is the curated, param-sized entity baked by codegen from
// parse/data/quant_vram.json. It is attested by BOTH models.dev (the catalog
// origin) and ollama (the curated quant/VRAM enrichment), so its provenance view
// has two joined source rows. The key is #size-aware: it only resolves because
// the sized identity carrier round-trips through parseEntityTuple → EntityByTuple.
const sizedCuratedKey = "llama@3.3#70b{instruct}"

// requireSizedEntity fetches the curated sized entity or fails loudly with a
// regen hint — the whole slice's behaviour rides on this baked data.
func requireSizedEntity(t *testing.T) bestiary.Entity {
	t.Helper()
	e, ok := lookupEntity(sizedCuratedKey)
	if !ok {
		t.Fatalf("lookupEntity(%q) missed; the curated sized entity must be baked\n"+
			"  How to fix: run 'go run ./cmd/bestiary-gen --no-fetch' to regenerate static data", sizedCuratedKey)
	}
	return e
}

// TestRun_Sources_JSON_SortedJoined drives `sources <sized-key>` (default json)
// end-to-end. It asserts: exactly one record per attesting source; records sorted
// ascending by source id; and that every record's uri/canonical-name/ingested-at
// is the value reached by the BCNF FK join (DataSourceByID / DatasetIngestedFor) —
// never a duplicated or hardcoded value. This kills both the "drop the sort" and
// "hardcode the uri (drop the DataSourceByID join)" mutants at the CLI boundary.
func TestRun_Sources_JSON_SortedJoined(t *testing.T) {
	ent := requireSizedEntity(t)
	if len(ent.Sources) < 2 {
		t.Fatalf("curated sized entity %q has %d sources, want >=2 (models.dev + ollama)",
			sizedCuratedKey, len(ent.Sources))
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"sources", sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run sources %q returned error: %v", sizedCuratedKey, runErr)
	}

	var got []struct {
		Source        string `json:"source"`
		URI           string `json:"uri"`
		CanonicalName string `json:"canonical_name"`
		IngestedAt    string `json:"ingested_at"`
		ParserSchema  int    `json:"parser_schema"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("sources json did not parse: %v\noutput:\n%s", err, out)
	}

	if len(got) != len(ent.Sources) {
		t.Fatalf("sources json has %d records, want %d (one per attesting source)", len(got), len(ent.Sources))
	}

	// Sorted ascending by source id (kills the drop-the-sort mutant).
	order := make([]string, len(got))
	for i, r := range got {
		order[i] = r.Source
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i].Source < got[j].Source }) {
		t.Errorf("sources records are not sorted ascending by source id; got order: %v", order)
	}

	// Each record's join fields must equal the values reached via the FK join —
	// this is what catches a hardcoded/duplicated uri.
	for _, r := range got {
		ds, ok := bestiary.DataSourceByID(bestiary.DataSourceID(r.Source))
		if !ok {
			t.Errorf("source %q in output does not resolve via DataSourceByID (FK join broken)", r.Source)
			continue
		}
		if r.URI != ds.URI {
			t.Errorf("source %q uri = %q, want %q (must come from the DataSourceByID join, not a hardcoded value)",
				r.Source, r.URI, ds.URI)
		}
		if r.CanonicalName != ds.CanonicalName {
			t.Errorf("source %q canonical_name = %q, want %q", r.Source, r.CanonicalName, ds.CanonicalName)
		}
		di, ok := bestiary.DatasetIngestedFor(bestiary.DataSourceID(r.Source))
		if !ok {
			t.Errorf("source %q has no DatasetIngested (join broken)", r.Source)
			continue
		}
		if r.IngestedAt != di.IngestedAt {
			t.Errorf("source %q ingested_at = %q, want %q", r.Source, r.IngestedAt, di.IngestedAt)
		}
		if r.ParserSchema != di.ParserSchema {
			t.Errorf("source %q parser_schema = %d, want %d", r.Source, r.ParserSchema, di.ParserSchema)
		}
	}
}

// TestRun_Sources_Table drives `sources <sized-key> --output=table` and asserts
// the entity header, the SOURCE|URI|INGESTED|PARSER column header, and that each
// attesting source id together with its FK-joined uri renders.
func TestRun_Sources_Table(t *testing.T) {
	ent := requireSizedEntity(t)

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"sources", "--output=table", sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run sources --output=table %q returned error: %v", sizedCuratedKey, runErr)
	}

	if !strings.Contains(out, "Entity: "+sizedCuratedKey) {
		t.Errorf("table output missing entity header; got:\n%s", out)
	}
	for _, want := range []string{"SOURCE", "URI", "INGESTED", "PARSER"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing column header %q; got:\n%s", want, out)
		}
	}
	for _, id := range ent.Sources {
		if !strings.Contains(out, string(id)) {
			t.Errorf("table output missing source id %q; got:\n%s", id, out)
		}
		ds, ok := bestiary.DataSourceByID(id)
		if ok && ds.URI != "" && !strings.Contains(out, ds.URI) {
			t.Errorf("table output missing FK-joined uri %q for source %q; got:\n%s", ds.URI, id, out)
		}
	}
}

// TestRun_Sources_SingleSource verifies a models.dev-only entity yields exactly
// one provenance record. This complements the dual-source case and guards against
// the projection collapsing or duplicating single-source attestations.
func TestRun_Sources_SingleSource(t *testing.T) {
	var single bestiary.Entity
	found := false
	for _, e := range bestiary.Entities() {
		if len(e.Sources) == 1 && e.Sources[0] == bestiary.DataSourceModelsDev {
			single = e
			found = true
			break
		}
	}
	if !found {
		t.Skip("no models.dev-only entity in the registry")
	}

	key := single.Ref.String()
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"sources", key})
	})
	if runErr != nil {
		t.Fatalf("run sources %q returned error: %v", key, runErr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("sources json did not parse: %v\noutput:\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("models.dev-only entity %q produced %d source records, want 1", key, len(got))
	}
	if got[0]["source"] != string(bestiary.DataSourceModelsDev) {
		t.Errorf("single source = %v, want %q", got[0]["source"], bestiary.DataSourceModelsDev)
	}
}

// TestRun_Sources_NotFound verifies an unknown key returns an actionable
// *ErrNotFound naming the entity and key.
func TestRun_Sources_NotFound(t *testing.T) {
	const bogus = "no-such-family/no-variant@no-version#404b"
	err := run([]string{"sources", bogus})
	if err == nil {
		t.Fatal("run sources <bogus> returned nil error, want a not-found error")
	}
	var nf *bestiary.ErrNotFound
	if !errors.As(err, &nf) {
		t.Fatalf("error %T is not *ErrNotFound; want a structured not-found error", err)
	}
	if nf.What != "entity" || nf.Key != bogus {
		t.Errorf("ErrNotFound = {What:%q, Key:%q}, want {entity, %q}", nf.What, nf.Key, bogus)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

// TestRun_Sources_SizeAware proves the subcommand resolves the SIZED key and that
// the unsized variant resolves to a DIFFERENT entity — i.e. the #size carrier is
// honoured rather than collapsed away. A wrong size must miss.
func TestRun_Sources_SizeAware(t *testing.T) {
	// The sized key resolves.
	if _, ok := lookupEntity(sizedCuratedKey); !ok {
		t.Fatalf("sized key %q did not resolve; #size-aware lookup broken", sizedCuratedKey)
	}
	// A wrong size for the same family/version/mods must miss.
	const wrongSize = "llama@3.3#999b{instruct}"
	if err := run([]string{"sources", wrongSize}); err == nil {
		t.Errorf("run sources %q returned nil, want a not-found error (wrong #size must not resolve)", wrongSize)
	}
}

// TestRun_Sources_NArg verifies the dispatch rejects a missing argument with a
// usage error before attempting any lookup.
func TestRun_Sources_NArg(t *testing.T) {
	err := run([]string{"sources"})
	if err == nil {
		t.Fatal("run([\"sources\"]) returned nil, want a usage error for the missing argument")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "usage") {
		t.Errorf("error %q should contain 'usage'", err.Error())
	}
}

// TestSourceProvenanceRows_SortsAndJoins is the direct unit guard on the builder:
// given a deliberately REVERSE-ordered source slice, the output must be sorted
// ascending by source id, and each row's uri/ingest must equal the FK-joined
// value. This is the falsifying test for the explicit sort.Slice and the
// DataSourceByID/DatasetIngestedFor joins independent of any baked entity order.
func TestSourceProvenanceRows_SortsAndJoins(t *testing.T) {
	// ollama sorts AFTER models.dev; pass them reversed so a missing sort shows.
	in := []bestiary.DataSourceID{bestiary.DataSourceOllama, bestiary.DataSourceModelsDev}
	rows := sourceProvenanceRows(in)
	if len(rows) != 2 {
		t.Fatalf("sourceProvenanceRows returned %d rows, want 2", len(rows))
	}
	if rows[0].Source != bestiary.DataSourceModelsDev || rows[1].Source != bestiary.DataSourceOllama {
		t.Fatalf("rows not sorted ascending: got [%s, %s], want [models.dev, ollama]",
			rows[0].Source, rows[1].Source)
	}
	for _, r := range rows {
		ds, _ := bestiary.DataSourceByID(r.Source)
		if r.URI != ds.URI || r.CanonicalName != ds.CanonicalName {
			t.Errorf("row %q join mismatch: uri=%q/%q name=%q/%q",
				r.Source, r.URI, ds.URI, r.CanonicalName, ds.CanonicalName)
		}
		di, _ := bestiary.DatasetIngestedFor(r.Source)
		if r.IngestedAt != di.IngestedAt || r.ParserSchema != di.ParserSchema {
			t.Errorf("row %q ingest mismatch: at=%q/%q schema=%d/%d",
				r.Source, r.IngestedAt, di.IngestedAt, r.ParserSchema, di.ParserSchema)
		}
	}
}

// TestRun_Providers_QuantFilter_Match drives `providers --quant=<q> <sized-key>`
// (json) and asserts every returned instance carries a matching QuantVRAM row and
// that the count equals the number of instances that genuinely carry that quant.
func TestRun_Providers_QuantFilter_Match(t *testing.T) {
	ent := requireSizedEntity(t)
	const q = bestiary.QuantQ4_K_M
	const qFlag = "q4_k_m"

	want := 0
	for _, in := range ent.Instances {
		if instanceHasQuant(in, q) {
			want++
		}
	}
	if want == 0 {
		t.Fatalf("curated sized entity has no instance carrying %s; fixture assumption broken", qFlag)
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", "--quant=" + qFlag, sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run providers --quant=%s returned error: %v", qFlag, runErr)
	}
	var insts []bestiary.ProviderInstance
	if err := json.Unmarshal([]byte(out), &insts); err != nil {
		t.Fatalf("providers json did not parse: %v\noutput:\n%s", err, out)
	}
	if len(insts) != want {
		t.Errorf("quant filter kept %d instances, want %d", len(insts), want)
	}
	for _, in := range insts {
		if !instanceHasQuant(in, q) {
			t.Errorf("instance %q retained but carries no %s row (filter let a non-match through)", in.ID, qFlag)
		}
	}
}

// TestRun_Providers_QuantFilter_Drop verifies that filtering by a quantization no
// instance carries drops every instance (empty result, no error).
func TestRun_Providers_QuantFilter_Drop(t *testing.T) {
	ent := requireSizedEntity(t)
	// Find a known quant that NO instance of this entity carries.
	const absent = bestiary.QuantQ5_K_M
	const absentFlag = "q5_k_m"
	for _, in := range ent.Instances {
		if instanceHasQuant(in, absent) {
			t.Skipf("entity unexpectedly carries %s; cannot test the drop-all path", absentFlag)
		}
	}

	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", "--quant=" + absentFlag, sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run providers --quant=%s returned error: %v", absentFlag, runErr)
	}
	var insts []bestiary.ProviderInstance
	if err := json.Unmarshal([]byte(out), &insts); err != nil {
		t.Fatalf("providers json did not parse: %v\noutput:\n%s", err, out)
	}
	if len(insts) != 0 {
		t.Errorf("filtering by an absent quant kept %d instances, want 0", len(insts))
	}
}

// TestRun_Providers_QuantFilter_UnknownErrors verifies an unrecognised --quant is
// rejected with an actionable error — NEVER silently mapped to QuantizationOther
// (which would match nothing and mislead the user). This kills the "use
// DetectQuantization instead of ParseQuantization" mutant.
func TestRun_Providers_QuantFilter_UnknownErrors(t *testing.T) {
	err := run([]string{"providers", "--quant=definitely-not-a-quant", sizedCuratedKey})
	if err == nil {
		t.Fatal("run providers --quant=<bogus> returned nil, want an actionable error")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "quantization") {
		t.Errorf("error %q should explain the quantization was unrecognised", err.Error())
	}
}

// TestRun_Providers_QuantColumns asserts the instance table renders the per-quant
// VRAM columns (QUANT|WEIGHTS|VRAM|CTX|PARTIAL) and a real quant name for an
// entity that carries quant data.
func TestRun_Providers_QuantColumns(t *testing.T) {
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"providers", "--output=table", sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run providers --output=table returned error: %v", runErr)
	}
	for _, want := range []string{"QUANT", "WEIGHTS", "VRAM", "CTX", "PARTIAL", "q4_k_m"} {
		if !strings.Contains(out, want) {
			t.Errorf("instance table missing quant column/value %q; got:\n%s", want, out)
		}
	}
}

// TestRun_ShowByEntity_QuantFilter verifies the --quant filter also applies to the
// `show --by-entity` instance view (json), retaining only matching instances.
func TestRun_ShowByEntity_QuantFilter(t *testing.T) {
	ent := requireSizedEntity(t)
	const q = bestiary.QuantQ4_K_M
	want := 0
	for _, in := range ent.Instances {
		if instanceHasQuant(in, q) {
			want++
		}
	}
	if want == 0 {
		t.Skip("no q4_k_m instances to assert against")
	}
	var runErr error
	out := captureStdout(t, func() {
		runErr = run([]string{"show", "--by-entity", "--quant=q4_k_m", sizedCuratedKey})
	})
	if runErr != nil {
		t.Fatalf("run show --by-entity --quant returned error: %v", runErr)
	}
	var got bestiary.Entity
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("entity json did not parse: %v\noutput:\n%s", err, out)
	}
	if len(got.Instances) != want {
		t.Errorf("show --by-entity --quant kept %d instances, want %d", len(got.Instances), want)
	}
}

// instanceHasQuant reports whether inst carries a QuantVRAM row of quantization q.
func instanceHasQuant(inst bestiary.ProviderInstance, q bestiary.Quantization) bool {
	for _, qv := range inst.QuantVRAM {
		if qv.Quant == q {
			return true
		}
	}
	return false
}
