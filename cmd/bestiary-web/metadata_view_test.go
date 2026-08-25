package main

import (
	"strings"
	"testing"

	"github.com/dayvidpham/bestiary"
)

// metadata_view_test.go pins the entity detail page's provider-agnostic metadata
// section: EVERY joined metadata row is rendered, and each row's benchmark claims are
// attributed to the MetadataID the lab published them under — never fused into one
// table. The defect this closes is a page that showed a single row's claims as though
// they were the entity's whole assessment record.

// multiMetadataFixture builds an entity carrying TWO metadata rows whose payloads are
// disjoint and individually identifiable, so a fused or dropped table is observable.
func multiMetadataFixture() bestiary.Entity {
	all := []bestiary.EntityMetadata{
		{
			MetadataID:  "openai/gpt-5.5",
			Name:        "GPT-5.5",
			Description: "BASE-DESCRIPTION",
			License:     "proprietary",
			Benchmarks: []bestiary.BenchmarkResult{
				{Name: "BASE-CLAIM", Metric: "resolve rate", Score: 58.6, Harness: "Terminus-2", Date: "2026-05-28", SourceURL: "https://example.test/base"},
			},
			Links: []bestiary.ModelLink{{Label: "BASE-LINK", URL: "https://example.test/base-card", Type: bestiary.LinkModelCard}},
		},
		{
			MetadataID:  "openai/gpt-5.5-instant",
			Name:        "GPT-5.5 Instant",
			Description: "INSTANT-DESCRIPTION",
			Benchmarks: []bestiary.BenchmarkResult{
				{Name: "INSTANT-CLAIM", Metric: "acc", ScoreRaw: "PASS-RAW"},
			},
		},
	}
	e := bestiary.Entity{
		Ref:         bestiary.EntityRef{Family: "gpt", Version: "5.5"},
		Sources:     []bestiary.DataSourceID{bestiary.DataSourceModelsDev},
		MetadataAll: all,
	}
	e.Metadata = &e.MetadataAll[0] // primary = shortest MetadataID
	return e
}

// TestDetail_MetadataAll_RendersEveryRow_AttributedPerID asserts the detail page shows
// both metadata rows, labels which is primary, and attributes each benchmark claim to
// its own MetadataID.
//
// Mutation guard: a page rendering only Entity.Metadata would omit INSTANT-CLAIM; a
// page fusing the rows into one table would fail the per-id attribution arm.
func TestDetail_MetadataAll_RendersEveryRow_AttributedPerID(t *testing.T) {
	e := multiMetadataFixture()
	s := newTestServer(t, []bestiary.Entity{e})
	body := get(t, s, e.Ref.IRI(entityRoutePrefix), "text/html").Body.String()

	for _, want := range []string{
		"model facts &amp; reported claims", // the section exists
		"BASE-DESCRIPTION",                  // primary row supplies description/license
		"openai/gpt-5.5",                    // both ids named
		"openai/gpt-5.5-instant",
		"BASE-CLAIM",    // the primary row's claim
		"INSTANT-CLAIM", // the row a single-pointer page would have dropped
		"PASS-RAW",      // non-numeric score rides through on ScoreRaw
		"BASE-LINK",     // per-row links
		"lab-reported claims",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("entity detail page missing %q;\nbody:\n%s", want, body)
		}
	}

	// Attribution: each claim sits under a caption naming ITS OWN MetadataID, and the
	// two captions are distinct — the claims are never presented as one table.
	for _, want := range []string{
		`claim(s) reported under <span class="mono">openai/gpt-5.5</span>`,
		`claim(s) reported under <span class="mono">openai/gpt-5.5-instant</span>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing per-row claim attribution %q;\nbody:\n%s", want, body)
		}
	}

	// The primary is MARKED, not the only one shown.
	// Count the BADGE markup specifically: the word "primary" also appears as an
	// attestation-authority cell elsewhere on the page.
	if n := strings.Count(body, `<span class="badge live">primary</span>`); n != 1 {
		t.Errorf("primary badge appears %d times, want exactly 1 (one row is primary; the rest are still rendered)", n)
	}

	// Ordering: the claims of the primary row precede the second row's, matching the
	// entity's ascending-MetadataID MetadataAll order.
	if i, j := strings.Index(body, "BASE-CLAIM"), strings.Index(body, "INSTANT-CLAIM"); i < 0 || j < 0 || i > j {
		t.Errorf("metadata rows are not rendered in MetadataAll (ascending MetadataID) order: BASE at %d, INSTANT at %d", i, j)
	}
}

// TestDetail_NoMetadata_SectionAbsent asserts an entity with no joined metadata renders
// no metadata section at all — the section is additive and never shows an empty shell.
func TestDetail_NoMetadata_SectionAbsent(t *testing.T) {
	e := bestiary.Entity{Ref: bestiary.EntityRef{Family: "llama", Version: "3.3", ParamSize: "70b"}}
	s := newTestServer(t, []bestiary.Entity{e})
	body := get(t, s, e.Ref.IRI(entityRoutePrefix), "text/html").Body.String()

	if strings.Contains(body, "model facts &amp; reported claims") {
		t.Errorf("entity with no metadata rendered the metadata section;\nbody:\n%s", body)
	}
}

// TestDetail_RealCorpus_GPT55_ShowsBothRows is the production witness over the ACTUAL
// committed registry (not a fixture): gpt@5.5 resolves to two lab identifiers and the
// page shows both, with the base row's claims — invisible before the multi-row repair
// — present and attributed.
func TestDetail_RealCorpus_GPT55_ShowsBothRows(t *testing.T) {
	e, ok := bestiary.EntityByTuple("gpt", "", "5.5", "")
	if !ok {
		t.Skip("gpt@5.5 absent from the registry corpus")
	}
	if len(e.MetadataAll) < 2 {
		t.Fatalf("gpt@5.5 carries %d metadata rows, want the 2 the corpus provides", len(e.MetadataAll))
	}

	s := newTestServer(t, []bestiary.Entity{e})
	body := get(t, s, e.Ref.IRI(entityRoutePrefix), "text/html").Body.String()

	for _, m := range e.MetadataAll {
		if !strings.Contains(body, string(m.MetadataID)) {
			t.Errorf("detail page omits metadata row %q", m.MetadataID)
		}
	}
	// The recovered payload: the primary row's claims must be on the page.
	claims := e.MetadataAll[0].Benchmarks
	if len(claims) == 0 {
		t.Fatal("the corpus row openai/gpt-5.5 carries no claims; this witness would be vacuous")
	}
	if !strings.Contains(body, claims[0].Name) {
		t.Errorf("detail page omits claim %q reported under %q", claims[0].Name, e.MetadataAll[0].MetadataID)
	}
}
