package bestiary

// MergeModels merges static and cached model lists.
// Deduplicates by ModelID. When both sources have the same ID,
// the entry with the more recent LastSynced timestamp wins.
// Since LastSynced uses RFC3339 UTC format, lexicographic string
// comparison correctly determines recency.
func MergeModels(static, cached []ModelInfo) []ModelInfo {
	return nil // stub — implemented in L3
}
