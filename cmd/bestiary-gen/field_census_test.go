package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The upstream field-shape census exists because a snapshot refresh is otherwise
// reviewed on COUNTS alone. Provider and row totals are obvious in any diff; a field
// upstream ADDED (nothing consumes it yet, so nothing fails) or REMOVED (something
// downstream silently stops being populated) is invisible. These tests are the guard:
// the census is recomputed from the vendored catalog and compared to the committed
// emission, and a divergence NAMES the paths rather than reporting a byte mismatch.
//
// Two levels, deliberately separate because they fail for different reasons and want
// different fixes:
//
//   - TestModelsdevFieldCensus_NoDrift compares the PATH SET. It is the semantic guard
//     and its message is the actionable one (added/removed paths, by name).
//   - TestModelsdevFieldCensus_UpToDate compares the BYTES. It also catches a fill
//     count that moved without any path moving — the ordinary case after a refresh —
//     whose fix is simply to re-run codegen and commit the emission.

// fieldCensusPath locates the committed emission from the cmd/bestiary-gen test working
// directory (tests run in their own package dir, the emission path is module-relative).
func fieldCensusPath() string { return filepath.Join("..", "..", modelsdevFieldCensusFile) }

// vendoredCatalogTestPath locates the committed codegen input from the same place.
func vendoredCatalogTestPath() string { return filepath.Join("..", "..", vendoredCatalogPath) }

// loadCommittedFieldCensus reads and decodes the committed emission.
func loadCommittedFieldCensus(t *testing.T) (ModelsdevFieldCensusEnvelope, []byte) {
	t.Helper()
	data, err := os.ReadFile(fieldCensusPath())
	if err != nil {
		t.Fatalf("read committed field census %s: %v\n"+
			"  What: the committed upstream field-shape census is missing or unreadable\n"+
			"  How to fix: run `go generate ./...` from the module root to emit it",
			fieldCensusPath(), err)
	}
	var env ModelsdevFieldCensusEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode committed field census %s: %v", fieldCensusPath(), err)
	}
	return env, data
}

// rebuildFieldCensus recomputes the census from the vendored catalog through the SAME
// pure builder codegen uses — never a reimplementation, so the test cannot drift from
// the emitter.
func rebuildFieldCensus(t *testing.T) ([]byte, ModelsdevFieldCensusEnvelope) {
	t.Helper()
	raw, err := os.ReadFile(vendoredCatalogTestPath())
	if err != nil {
		t.Fatalf("read vendored catalog %s: %v", vendoredCatalogTestPath(), err)
	}
	data, err := buildModelsdevFieldCensus(raw)
	if err != nil {
		t.Fatalf("buildModelsdevFieldCensus over the vendored catalog: %v", err)
	}
	var env ModelsdevFieldCensusEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("decode rebuilt field census: %v", err)
	}
	return data, env
}

// pathSet indexes a census by field path.
func pathSet(env ModelsdevFieldCensusEnvelope) map[string]int {
	m := make(map[string]int, len(env.Fields))
	for _, r := range env.Fields {
		m[r.Path] = r.Fill
	}
	return m
}

// TestModelsdevFieldCensus_NoDrift is the LOUD upstream field-shape guard. It fails
// naming every field path the vendored catalog gained or lost relative to the committed
// census, because "the catalog changed shape" is only actionable once you know WHICH
// field.
func TestModelsdevFieldCensus_NoDrift(t *testing.T) {
	committed, _ := loadCommittedFieldCensus(t)
	_, rebuilt := rebuildFieldCensus(t)

	have := pathSet(committed)
	want := pathSet(rebuilt)

	var added, removed []string
	for p := range want {
		if _, ok := have[p]; !ok {
			added = append(added, p)
		}
	}
	for p := range have {
		if _, ok := want[p]; !ok {
			removed = append(removed, p)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)

	if len(added) == 0 && len(removed) == 0 {
		t.Logf("upstream field-shape census: %d paths, no drift", len(want))
		return
	}
	t.Errorf("upstream models.dev field shape DRIFTED from the committed census\n"+
		"  What: the vendored catalog publishes a different set of field paths than %s records\n"+
		"    ADDED upstream (%d): %s\n"+
		"    REMOVED upstream (%d): %s\n"+
		"  Where: %s measured against %s\n"+
		"  When: recomputing the census from the committed codegen input\n"+
		"  Why: a refresh is otherwise reviewed on row and provider counts alone, which cannot\n"+
		"       show a field appearing or disappearing — an ADDED path is upstream data nothing\n"+
		"       consumes yet, a REMOVED path is a field something downstream may still expect\n"+
		"  What it means for the caller: parsing still succeeds; the risk is silent, not loud —\n"+
		"       a removed path leaves its consumer reading a zero value forever\n"+
		"  How to fix: decide each path deliberately. For an ADDED path, either wire it into\n"+
		"       wire.go/parse.go or accept it as unused. For a REMOVED path, find its consumers\n"+
		"       first. Then re-run `go generate ./...` and commit the updated census in the same\n"+
		"       commit as the catalog refresh that moved it",
		modelsdevFieldCensusFile,
		len(added), strings.Join(added, ", "),
		len(removed), strings.Join(removed, ", "),
		vendoredCatalogPath, modelsdevFieldCensusFile,
	)
}

// TestModelsdevFieldCensus_UpToDate is the byte-level staleness guard: it catches a
// FILL count that moved while the path set held (the ordinary outcome of a refresh),
// which the drift test above deliberately ignores so its message stays about shape.
func TestModelsdevFieldCensus_UpToDate(t *testing.T) {
	_, committedBytes := loadCommittedFieldCensus(t)
	rebuiltBytes, _ := rebuildFieldCensus(t)
	if string(committedBytes) == string(rebuiltBytes) {
		return
	}
	committed, _ := loadCommittedFieldCensus(t)
	_, rebuilt := rebuildFieldCensus(t)
	have, want := pathSet(committed), pathSet(rebuilt)
	var moved []string
	for p, n := range want {
		if old, ok := have[p]; ok && old != n {
			moved = append(moved, p+": "+strconv.Itoa(old)+" -> "+strconv.Itoa(n))
		}
	}
	sort.Strings(moved)
	// A byte mismatch with NO moved fill count means the divergence is in the envelope —
	// schema_version, the _comment, or the path set itself (which the drift test names).
	// Without this arm the message reads "Fill counts that moved (0): " and tells the
	// reader nothing about the one case it cannot explain.
	movedLine := "  Fill counts that moved (" + strconv.Itoa(len(moved)) + "): " + strings.Join(moved, ", ") + "\n"
	if len(moved) == 0 {
		movedLine = "  NO fill count moved: the difference is in the emission ENVELOPE (schema_version,\n" +
			"    the _comment) or in the PATH SET, which TestModelsdevFieldCensus_NoDrift names\n"
	}
	t.Errorf("committed field census is STALE relative to the vendored catalog\n"+
		"  What: %s does not match what buildModelsdevFieldCensus produces from %s\n"+
		"%s"+
		"  When: after a snapshot refresh that changed row counts without changing field shape\n"+
		"  What it means for the caller: the committed census describes an older snapshot\n"+
		"  How to fix: run `go generate ./...` and commit the regenerated census",
		modelsdevFieldCensusFile, vendoredCatalogPath, movedLine)
}

// TestModelsdevFieldCensus_EnvelopeContract pins the committed-emission invariants the
// other reports carry: count agrees with the list, the list is sorted by the explicit
// sort key, the list is empty-not-null, and there is no wall clock.
func TestModelsdevFieldCensus_EnvelopeContract(t *testing.T) {
	env, data := loadCommittedFieldCensus(t)

	if env.Count != len(env.Fields) {
		t.Errorf("census count = %d but the fields list holds %d rows — the envelope is self-inconsistent",
			env.Count, len(env.Fields))
	}
	if env.SchemaVersion != 1 {
		t.Errorf("census schema_version = %d, want 1", env.SchemaVersion)
	}
	if !sort.SliceIsSorted(env.Fields, func(i, j int) bool { return env.Fields[i].Path < env.Fields[j].Path }) {
		t.Error("census fields are not sorted ascending by path — the emitter must sort explicitly, never rely on Go map order")
	}
	if strings.Contains(string(data), `"fields": null`) {
		t.Error(`census emitted "fields": null; an empty census must serialize as [] so the shape is stable`)
	}
	assertNoWallClock(t, data)

	// Non-vacuity: the census must actually measure all three scopes, or a silent
	// collapse to one scope would still satisfy every check above.
	scopes := map[string]bool{}
	for _, r := range env.Fields {
		switch {
		case strings.HasPrefix(r.Path, "providers[].models[]."):
			scopes["model"] = true
		case strings.HasPrefix(r.Path, "providers[]."):
			scopes["provider"] = true
		case strings.HasPrefix(r.Path, "models[]."):
			scopes["models_view"] = true
		default:
			t.Errorf("census row %q has no recognized scope prefix", r.Path)
		}
	}
	for _, want := range []string{"provider", "model", "models_view"} {
		if !scopes[want] {
			t.Errorf("census measured no %s-scope paths — the census went partially vacuous", want)
		}
	}
	if len(env.Fields) < 20 {
		t.Errorf("census holds only %d paths, want >= 20 — a credible floor for this catalog shape", len(env.Fields))
	}
}

// TestBuildModelsdevFieldCensus_DetectsAnAddedField is the non-vacuity guard for the
// DRIFT MECHANISM itself: an emitter that returned a constant path set would satisfy
// every test above. It feeds a MUTATED catalog carrying one synthetic field and proves
// the census reports exactly that path — the falsifier for the whole guard.
func TestBuildModelsdevFieldCensus_DetectsAnAddedField(t *testing.T) {
	raw, err := os.ReadFile(vendoredCatalogTestPath())
	if err != nil {
		t.Fatalf("read vendored catalog: %v", err)
	}
	baseData, err := buildModelsdevFieldCensus(raw)
	if err != nil {
		t.Fatalf("build baseline census: %v", err)
	}
	var base ModelsdevFieldCensusEnvelope
	if err := json.Unmarshal(baseData, &base); err != nil {
		t.Fatalf("decode baseline census: %v", err)
	}

	// Mutate IN MEMORY: add one synthetic field to exactly one model row of one
	// provider, and one nested subkey under it. The committed catalog is never touched.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal catalog: %v", err)
	}
	var providers map[string]json.RawMessage
	if err := json.Unmarshal(doc["providers"], &providers); err != nil {
		t.Fatalf("unmarshal providers: %v", err)
	}
	var provName string
	for k := range providers {
		if provName == "" || k < provName {
			provName = k // deterministic pick: lexicographically first provider
		}
	}
	var prov map[string]json.RawMessage
	if err := json.Unmarshal(providers[provName], &prov); err != nil {
		t.Fatalf("unmarshal provider %q: %v", provName, err)
	}
	var models map[string]json.RawMessage
	if err := json.Unmarshal(prov["models"], &models); err != nil {
		t.Fatalf("unmarshal models of %q: %v", provName, err)
	}
	var modelID string
	for k := range models {
		if modelID == "" || k < modelID {
			modelID = k
		}
	}
	var model map[string]json.RawMessage
	if err := json.Unmarshal(models[modelID], &model); err != nil {
		t.Fatalf("unmarshal model %q: %v", modelID, err)
	}
	model["synthetic_drift_probe"] = json.RawMessage(`{"nested_probe": 1}`)
	remarshal := func(v any) json.RawMessage {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("remarshal: %v", err)
		}
		return b
	}
	models[modelID] = remarshal(model)
	prov["models"] = remarshal(models)
	providers[provName] = remarshal(prov)
	doc["providers"] = remarshal(providers)
	mutated := remarshal(doc)

	mutatedData, err := buildModelsdevFieldCensus(mutated)
	if err != nil {
		t.Fatalf("build census over the mutated catalog: %v", err)
	}
	var after ModelsdevFieldCensusEnvelope
	if err := json.Unmarshal(mutatedData, &after); err != nil {
		t.Fatalf("decode mutated census: %v", err)
	}

	basePaths, afterPaths := pathSet(base), pathSet(after)
	var added []string
	for p := range afterPaths {
		if _, ok := basePaths[p]; !ok {
			added = append(added, p)
		}
	}
	sort.Strings(added)

	want := []string{
		"providers[].models[].synthetic_drift_probe",
		"providers[].models[].synthetic_drift_probe.nested_probe",
	}
	if len(added) != len(want) {
		t.Fatalf("mutated census added %d paths %v, want exactly %v — the drift mechanism does not see a new field",
			len(added), added, want)
	}
	for i := range want {
		if added[i] != want[i] {
			t.Errorf("added path %d = %q, want %q", i, added[i], want[i])
		}
	}
	for _, p := range want {
		if afterPaths[p] != 1 {
			t.Errorf("added path %q has fill %d, want 1 (it was injected into exactly one model row)", p, afterPaths[p])
		}
	}

	// The committed catalog must be untouched by this test.
	reread, err := os.ReadFile(vendoredCatalogTestPath())
	if err != nil {
		t.Fatalf("re-read vendored catalog: %v", err)
	}
	if !bytes.Equal(reread, raw) {
		t.Fatal("the vendored catalog changed on disk during the mutation test — the probe must stay " +
			"in memory.\n" +
			"  Why the comparison is byte-for-byte: this is the FALSIFIER, the one test whose whole job\n" +
			"    is to be believed about what it proves. A length check passes any same-length edit,\n" +
			"    and swapping one JSON field value for another of equal width is exactly that.\n" +
			"  How to fix: restore the committed catalog from git; the mutation must never leave memory")
	}
}
