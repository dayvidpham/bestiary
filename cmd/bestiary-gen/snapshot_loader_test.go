package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/dayvidpham/bestiary"
)

// snapshotDir returns the absolute path to testdata/snapshot/ relative to this
// source file's location. This makes the loader work regardless of the working
// directory the test runner uses.
func snapshotDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		// Fallback: rely on working directory.
		return filepath.Join("testdata", "snapshot")
	}
	dir := filepath.Dir(thisFile)
	return filepath.Join(dir, "testdata", "snapshot")
}

// SnapshotRecord is a single raw API record extracted from the committed snapshot.
// It carries only the fields needed by the divergence analyzer: the provider slug,
// the model ID, and the raw family string from the API.
type SnapshotRecord struct {
	Provider  bestiary.Provider
	ID        bestiary.ModelID
	RawFamily bestiary.Family
}

// LoadSnapshotRecords reads cmd/bestiary-gen/testdata/snapshot/models_api.json
// (the committed real-data snapshot) and returns one SnapshotRecord per
// (provider, model) entry in the API response.
//
// Records are returned sorted by (Provider, ID) for deterministic ordering —
// the same order produced by fetchModelsWithRaw.
//
// This function is intentionally offline-only: it reads the committed file and
// never makes a network request. Use it in tests and analysis harnesses instead
// of fetchModelsWithRaw(..., noFetch=false).
func LoadSnapshotRecords() ([]SnapshotRecord, error) {
	snapshotPath := filepath.Join(snapshotDir(), "models_api.json")
	body, err := os.ReadFile(snapshotPath)
	if err != nil {
		absPath, _ := filepath.Abs(snapshotPath)
		return nil, fmt.Errorf(
			"LoadSnapshotRecords: cannot read committed snapshot at %s: %w\n"+
				"  What: the committed snapshot file is missing or unreadable\n"+
				"  Why: the file may not have been committed or the path is wrong\n"+
				"  How to fix: verify cmd/bestiary-gen/testdata/snapshot/models_api.json exists\n"+
				"  and is tracked by git (run: git status cmd/bestiary-gen/testdata/snapshot/)",
			absPath, err,
		)
	}

	var resp genWireResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf(
			"LoadSnapshotRecords: cannot decode snapshot JSON: %w\n"+
				"  What: the JSON file does not match the expected models.dev API schema\n"+
				"  Why: the snapshot may be corrupt or from an incompatible API version\n"+
				"  How to fix: re-capture the snapshot from the cache:\n"+
				"    cp .bestiary-gen-cache/api_response.json cmd/bestiary-gen/testdata/snapshot/models_api.json",
			err,
		)
	}

	var records []SnapshotRecord
	for provSlug, prov := range resp {
		for _, wm := range prov.Models {
			records = append(records, SnapshotRecord{
				Provider:  bestiary.Provider(provSlug),
				ID:        bestiary.ModelID(wm.ID),
				RawFamily: bestiary.Family(wm.Family),
			})
		}
	}

	// Sort by (Provider, ID) for deterministic output — mirrors fetchModelsWithRaw.
	sort.Slice(records, func(i, j int) bool {
		if records[i].Provider != records[j].Provider {
			return records[i].Provider < records[j].Provider
		}
		return records[i].ID < records[j].ID
	})

	return records, nil
}

// SnapshotMeta mirrors the committed testdata/snapshot/snapshot_meta.json
// provenance sidecar. Only the fields consumed by the analyzer are decoded.
type SnapshotMeta struct {
	UpstreamGitCommit     string `json:"upstream_git_commit"`
	UpstreamSchemaVersion string `json:"upstream_schema_version"`
	CaptureDate           string `json:"capture_date"`
}

// loadSnapshotMeta reads and decodes testdata/snapshot/snapshot_meta.json.
func loadSnapshotMeta() (SnapshotMeta, error) {
	metaPath := filepath.Join(snapshotDir(), "snapshot_meta.json")
	body, err := os.ReadFile(metaPath)
	if err != nil {
		absPath, _ := filepath.Abs(metaPath)
		return SnapshotMeta{}, fmt.Errorf(
			"loadSnapshotMeta: cannot read snapshot meta at %s: %w\n"+
				"  What: the snapshot provenance sidecar is missing or unreadable\n"+
				"  How to fix: verify cmd/bestiary-gen/testdata/snapshot/snapshot_meta.json exists",
			absPath, err,
		)
	}
	var meta SnapshotMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return SnapshotMeta{}, fmt.Errorf(
			"loadSnapshotMeta: cannot decode snapshot_meta.json: %w\n"+
				"  What: the sidecar JSON does not match the expected SnapshotMeta shape\n"+
				"  How to fix: ensure upstream_git_commit/upstream_schema_version/capture_date are present",
			err,
		)
	}
	return meta, nil
}

// loadSnapshotCommit returns the UpstreamGitCommit recorded in
// snapshot_meta.json, failing the test if the sidecar is missing or malformed.
// The analyzer reads this instead of hardcoding the commit hash so a snapshot
// refresh updates the meta in one place and the test/report follow.
func loadSnapshotCommit(t testingTB) string {
	t.Helper()
	meta, err := loadSnapshotMeta()
	if err != nil {
		t.Fatalf("loadSnapshotCommit: %v", err)
	}
	if meta.UpstreamGitCommit == "" {
		t.Fatalf("loadSnapshotCommit: upstream_git_commit is empty in snapshot_meta.json")
	}
	return meta.UpstreamGitCommit
}

// testingTB is the minimal subset of *testing.T used by loadSnapshotCommit,
// kept local so the loader support file does not import "testing" at the top
// level for a single helper signature.
type testingTB interface {
	Helper()
	Fatalf(format string, args ...any)
}
