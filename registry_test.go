package bestiary_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// TestStaticModels_NonEmpty verifies that StaticModels returns a non-empty
// slice after codegen has produced models_static_gen.go.
// NOTE: This test will fail until go generate ./... has been run.
func TestStaticModels_NonEmpty(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Fatal("StaticModels: expected non-empty slice; got zero entries — run 'go generate ./...' to produce models_static_gen.go")
	}
}

// TestStaticModels_DefensiveCopy verifies that modifying the slice returned by
// StaticModels does not affect subsequent calls (defensive copy contract).
func TestStaticModels_DefensiveCopy(t *testing.T) {
	first := bestiary.StaticModels()
	if len(first) == 0 {
		t.Skip("skipping defensive-copy test: StaticModels returned empty slice (codegen not yet run)")
	}

	originalLen := len(first)
	// Truncate the returned slice — should not affect the registry.
	first = first[:0]

	second := bestiary.StaticModels()
	if len(second) != originalLen {
		t.Fatalf("StaticModels: defensive copy broken — after truncating first result, second call returned %d entries (expected %d)",
			len(second), originalLen)
	}
}

// TestLookupModel_Known verifies that a model known to be present in the
// static registry can be retrieved by ID.
// NOTE: This test will fail until go generate ./... has been run.
func TestLookupModel_Known(t *testing.T) {
	// Pick a model ID that is stable in the Anthropic catalogue.
	// claude-opus-4-20250514 is the dated release slug for Claude Opus 4.
	const wantID bestiary.ModelID = "claude-opus-4-20250514"

	info, ok := bestiary.LookupModel(wantID)
	if !ok {
		// Provide a more helpful message by listing available IDs if the
		// registry is non-empty but the model wasn't found.
		all := bestiary.StaticModels()
		if len(all) == 0 {
			t.Fatalf("LookupModel(%q): not found — static registry is empty; run 'go generate ./...' first", wantID)
		}
		t.Fatalf("LookupModel(%q): not found in static registry (%d models); check the generated model ID list", wantID, len(all))
	}
	if info.ID != wantID {
		t.Fatalf("LookupModel(%q): returned model has ID %q", wantID, info.ID)
	}
}

// TestLookupModel_Unknown verifies that looking up a non-existent model
// returns the zero value and false.
func TestLookupModel_Unknown(t *testing.T) {
	info, ok := bestiary.LookupModel("nonexistent-model")
	if ok {
		t.Fatalf("LookupModel(\"nonexistent-model\"): expected (zero, false); got (model=%+v, ok=true)", info)
	}
	// ModelInfo contains Modalities which holds slices — not directly comparable.
	// Check the discriminating fields that will be set on any real model.
	if info.ID != "" || info.Provider != "" || info.DisplayName != "" {
		t.Fatalf("LookupModel(\"nonexistent-model\"): expected zero ModelInfo; got non-zero fields: ID=%q Provider=%q DisplayName=%q",
			info.ID, info.Provider, info.DisplayName)
	}
}

// TestModelsByProvider verifies that ModelsByProvider(ProviderAnthropic)
// returns only Anthropic models, and that each result has the correct provider.
// NOTE: This test will fail until go generate ./... has been run.
func TestModelsByProvider(t *testing.T) {
	models := bestiary.ModelsByProvider(bestiary.ProviderAnthropic)
	if len(models) == 0 {
		t.Fatal("ModelsByProvider(ProviderAnthropic): expected non-empty slice; got zero entries — run 'go generate ./...' first")
	}
	for _, m := range models {
		if m.Provider != bestiary.ProviderAnthropic {
			t.Errorf("ModelsByProvider(ProviderAnthropic): got model %q with provider %q (want %q)",
				m.ID, m.Provider, bestiary.ProviderAnthropic)
		}
	}
}

// TestModelsByProvider_Empty verifies that filtering by a truly unknown provider
// returns an empty (nil/zero-length) slice without panicking.
func TestModelsByProvider_Empty(t *testing.T) {
	models := bestiary.ModelsByProvider(bestiary.Provider("definitely-not-a-real-provider-xyz"))
	if len(models) != 0 {
		t.Fatalf("ModelsByProvider(\"definitely-not-a-real-provider-xyz\"): expected empty slice; got %d entries", len(models))
	}
}

// TestModelsByFamily verifies that ModelsByFamily returns only models with the
// given raw family string, and that all results match.
// NOTE: This test will fail until go generate ./... has been run.
func TestModelsByFamily(t *testing.T) {
	// "claude-opus" is a stable raw API family in the Anthropic catalogue.
	const wantFamily = "claude-opus"

	models := bestiary.ModelsByFamily(wantFamily)
	if len(models) == 0 {
		all := bestiary.StaticModels()
		if len(all) == 0 {
			t.Fatalf("ModelsByFamily(%q): static registry is empty; run 'go generate ./...' first", wantFamily)
		}
		t.Fatalf("ModelsByFamily(%q): got zero results from non-empty registry (%d models); check the family name", wantFamily, len(all))
	}
	for _, m := range models {
		if m.RawFamily != wantFamily {
			t.Errorf("ModelsByFamily(%q): got model %q with RawFamily %q", wantFamily, m.ID, m.RawFamily)
		}
	}
}

// TestStaticModels_AllHaveKnownProvider verifies that every model in the static
// registry has a provider that IsKnown() accepts.
func TestStaticModels_AllHaveKnownProvider(t *testing.T) {
	for _, m := range bestiary.StaticModels() {
		if !m.Provider.IsKnown() {
			t.Errorf("model %q has unknown provider %q", m.ID, m.Provider)
		}
	}
}

// TestStaticModels_NoDuplicateKeys verifies that no (ModelID, Provider) pair
// appears more than once in the static registry.
func TestStaticModels_NoDuplicateKeys(t *testing.T) {
	type key struct {
		ID bestiary.ModelID
		P  bestiary.Provider
	}
	seen := make(map[key]struct{})
	for _, m := range bestiary.StaticModels() {
		k := key{m.ID, m.Provider}
		if _, dup := seen[k]; dup {
			t.Errorf("duplicate (ID, Provider): (%q, %q)", m.ID, m.Provider)
		}
		seen[k] = struct{}{}
	}
}

// TestStaticModels_ContainsCoreProviders is a regression guard ensuring the
// three core providers (Anthropic, Google, OpenAI) actually appear in the
// static registry after codegen (Reviewer B-4 requirement).
func TestStaticModels_ContainsCoreProviders(t *testing.T) {
	seen := make(map[bestiary.Provider]bool)
	for _, m := range bestiary.StaticModels() {
		seen[m.Provider] = true
	}
	for _, core := range []bestiary.Provider{
		bestiary.ProviderAnthropic,
		bestiary.ProviderGoogle,
		bestiary.ProviderOpenAI,
	} {
		if !seen[core] {
			t.Errorf("core provider %q not found in static registry", core)
		}
	}
}

// TestStaticModels_HaveLastSynced verifies that every model in the static
// registry has a non-empty LastSynced field.
// NOTE: This test will fail until go generate ./... has been run.
func TestStaticModels_HaveLastSynced(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Fatal("StaticModels: expected non-empty slice; run 'go generate ./...' first")
	}
	for _, m := range models {
		if m.LastSynced == "" {
			t.Errorf("model %q has empty LastSynced — codegen must set LastSynced to the generation timestamp", m.ID)
		}
	}
}

// TestLookupModelByProvider_Found verifies that a model known to exist in the
// static registry can be found by provider and name.
func TestLookupModelByProvider_Found(t *testing.T) {
	const wantName = "claude-opus-4-5"
	info, ok := bestiary.LookupModelByProvider(bestiary.ProviderAnthropic, wantName)
	if !ok {
		all := bestiary.StaticModels()
		if len(all) == 0 {
			t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): not found — static registry is empty; run 'go generate ./...' first", wantName)
		}
		t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): not found in static registry (%d models)", wantName, len(all))
	}
	if string(info.ID) != wantName {
		t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): returned model has ID %q", wantName, info.ID)
	}
	if info.Provider != bestiary.ProviderAnthropic {
		t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): returned model has provider %q", wantName, info.Provider)
	}
}

// TestLookupModelByProvider_WrongProvider verifies that querying with the correct
// name but wrong provider returns the zero value and false.
func TestLookupModelByProvider_WrongProvider(t *testing.T) {
	// "claude-opus-4-5" exists under ProviderAnthropic, not ProviderGoogle.
	const name = "claude-opus-4-5"
	info, ok := bestiary.LookupModelByProvider(bestiary.ProviderGoogle, name)
	if ok {
		t.Fatalf("LookupModelByProvider(ProviderGoogle, %q): expected (zero, false); got (model=%+v, ok=true)", name, info)
	}
	if info.ID != "" || info.Provider != "" || info.DisplayName != "" {
		t.Fatalf("LookupModelByProvider(ProviderGoogle, %q): expected zero ModelInfo; got non-zero fields: ID=%q Provider=%q DisplayName=%q",
			name, info.ID, info.Provider, info.DisplayName)
	}
}

// TestLookupModelByProvider_WrongName verifies that querying with the correct
// provider but a non-existent name returns the zero value and false.
func TestLookupModelByProvider_WrongName(t *testing.T) {
	const name = "nonexistent-model-xyz"
	info, ok := bestiary.LookupModelByProvider(bestiary.ProviderAnthropic, name)
	if ok {
		t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): expected (zero, false); got (model=%+v, ok=true)", name, info)
	}
	if info.ID != "" || info.Provider != "" || info.DisplayName != "" {
		t.Fatalf("LookupModelByProvider(ProviderAnthropic, %q): expected zero ModelInfo; got non-zero fields: ID=%q Provider=%q DisplayName=%q",
			name, info.ID, info.Provider, info.DisplayName)
	}
}

// TestModels_NonEmpty verifies that Models returns a non-empty slice.
func TestModels_NonEmpty(t *testing.T) {
	models := bestiary.Models()
	if len(models) == 0 {
		t.Fatal("Models: expected non-empty slice; got zero entries — run 'go generate ./...' first")
	}
}

// TestModels_SameContentAsStaticModels verifies that Models returns the same
// content as StaticModels (both are defensive copies of the same backing data).
func TestModels_SameContentAsStaticModels(t *testing.T) {
	got := bestiary.Models()
	want := bestiary.StaticModels()
	if len(got) == 0 {
		t.Skip("skipping: Models returned empty slice (codegen not yet run)")
	}
	if len(got) != len(want) {
		t.Fatalf("Models: returned %d entries but StaticModels returned %d", len(got), len(want))
	}
	for i := range got {
		if got[i].ID != want[i].ID || got[i].Provider != want[i].Provider {
			t.Errorf("Models[%d]: got ID=%q Provider=%q, want ID=%q Provider=%q",
				i, got[i].ID, got[i].Provider, want[i].ID, want[i].Provider)
		}
	}
}

// TestModels_DefensiveCopy verifies that modifying the slice returned by
// Models does not affect subsequent calls.
func TestModels_DefensiveCopy(t *testing.T) {
	first := bestiary.Models()
	if len(first) == 0 {
		t.Skip("skipping: Models returned empty slice (codegen not yet run)")
	}
	originalLen := len(first)
	first = first[:0]
	second := bestiary.Models()
	if len(second) != originalLen {
		t.Fatalf("Models: defensive copy broken — after truncating first result, second call returned %d entries (expected %d)",
			len(second), originalLen)
	}
}

// TestStaticModels_NoDateVersions is the SLICE-1-FIX-3 real-data invariant test.
// It iterates ALL models in the static registry and asserts that no model's Version
// field contains a date-shaped value. This permanently guards the class of bugs
// where date tokens (YYMM, MMDD, YYMMDD, MM-YYYY) are silently stored as versions.
//
// INVARIANT: no model's Version may be a date. Date shapes rejected:
//   - 4-digit YYMM or MMDD (any bare 4-digit all-numeric string)
//   - 6-digit YYMMDD (any bare 6-digit all-numeric string)
//   - MM-YYYY: "NN-NNNN" where NN is 01-12 and NNNN is a 19xx or 20xx year
//   - Any embedded 4-digit run that is a calendar year (19xx or 20xx), with or without dashes
//
// This test is deterministic — it reads only from the static registry, no API calls.
// It will fail until go generate ./... has been run to produce the current static data.
func TestStaticModels_NoDateVersions(t *testing.T) {
	models := bestiary.StaticModels()
	if len(models) == 0 {
		t.Skip("TestStaticModels_NoDateVersions: static registry empty — run 'go generate ./...' first")
	}

	// Date shape regexes for version field validation.
	// re4Digit: any bare 4-digit all-numeric version (YYMM or MMDD format).
	re4Digit := regexp.MustCompile(`^\d{4}$`)
	// re6Digit: any bare 6-digit all-numeric version (YYMMDD format).
	re6Digit := regexp.MustCompile(`^\d{6}$`)
	// reMMYYYY: MM-YYYY two-group (e.g. "08-2024", "03-2025").
	reMMYYYY := regexp.MustCompile(`^(0[1-9]|1[0-2])-(19|20)\d{2}$`)
	// reEmbedYear: version string that contains an embedded 4-digit calendar year
	// run (19xx or 20xx), with or without surrounding dots/hyphens, OR a concatenated
	// YYYYMMDD run (e.g. "20240101" where "2024" is concatenated with "0101" — no separator).
	// SLICE-1-FIX-4 (reEmbedYear): trailing boundary hardened to also match a following digit,
	// catching concatenated 8-digit YYYYMMDD strings that have no separator between year and day.
	reEmbedYear := regexp.MustCompile(`(?:^|[.\-])(19|20)\d{2}(?:$|[.\-]|\d)`)

	var failures []string
	for _, m := range models {
		v := m.Version
		if v == "" {
			continue
		}
		reason := ""
		switch {
		case re4Digit.MatchString(v):
			reason = "bare 4-digit date (YYMM or MMDD)"
		case re6Digit.MatchString(v):
			reason = "bare 6-digit date (YYMMDD)"
		case reMMYYYY.MatchString(v):
			reason = "MM-YYYY two-group date"
		case reEmbedYear.MatchString(v):
			reason = "embedded 4-digit year run (19xx or 20xx)"
		}
		if reason != "" {
			failures = append(failures, "  model "+string(m.ID)+" (provider "+string(m.Provider)+"): Version="+v+" — "+reason)
		}
	}

	if len(failures) > 0 {
		t.Errorf("TestStaticModels_NoDateVersions: %d model(s) have date-shaped Version fields (INVARIANT VIOLATED):\n"+
			"%s\n"+
			"  What: Version field contains a date-shaped value\n"+
			"  Why: parse heuristics failed to strip date tokens from the version dot-join path\n"+
			"  How to fix: run SLICE-1-FIX-3 date-group guards or re-run go generate ./... after parse fix",
			len(failures), strings.Join(failures, "\n"))
	}
}
