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
