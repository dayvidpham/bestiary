package bestiary

// MergeModels merges static and cached model lists.
// Deduplicates by ModelID. When both sources have the same ID,
// the entry with the more recent LastSynced timestamp wins.
// Since LastSynced uses RFC3339 UTC format, lexicographic string
// comparison correctly determines recency.
func MergeModels(static, cached []ModelInfo) []ModelInfo {
	seen := make(map[ModelID]ModelInfo, len(static)+len(cached))

	for _, m := range static {
		seen[m.ID] = m
	}

	for _, m := range cached {
		if existing, ok := seen[m.ID]; ok {
			// RFC3339 UTC timestamps sort lexicographically — later timestamp wins.
			if m.LastSynced > existing.LastSynced {
				seen[m.ID] = m
			}
		} else {
			seen[m.ID] = m
		}
	}

	out := make([]ModelInfo, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	return out
}
