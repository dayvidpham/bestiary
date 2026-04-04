package bestiary

// modelKey is a composite key used to deduplicate models across providers.
// A model with the same ID may exist under multiple providers with different
// pricing and capabilities; each (ID, Provider) pair is a distinct entry.
type modelKey struct {
	ID       ModelID
	Provider Provider
}

// MergeModels merges static and cached model lists.
// Deduplicates by (ModelID, Provider) pair. When both sources have the same
// (ID, Provider), the entry with the more recent LastSynced timestamp wins.
// Models with the same ID but different providers are kept as distinct entries.
// Since LastSynced uses RFC3339 UTC format, lexicographic string comparison
// correctly determines recency.
func MergeModels(static, cached []ModelInfo) []ModelInfo {
	seen := make(map[modelKey]ModelInfo, len(static)+len(cached))

	for _, m := range static {
		seen[modelKey{m.ID, m.Provider}] = m
	}

	for _, m := range cached {
		key := modelKey{m.ID, m.Provider}
		if existing, ok := seen[key]; ok {
			// RFC3339 UTC timestamps sort lexicographically — later timestamp wins.
			if m.LastSynced > existing.LastSynced {
				seen[key] = m
			}
		} else {
			seen[key] = m
		}
	}

	out := make([]ModelInfo, 0, len(seen))
	for _, m := range seen {
		out = append(out, m)
	}
	return out
}
