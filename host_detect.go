package bestiary

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// hostPrefixEntry is one curated host-prefix row from parse/data/hosts.json:
// a leading model-ID token (prefix) that marks a host-routed instance, paired
// with the Host value it maps to.
type hostPrefixEntry struct {
	Prefix string `json:"prefix"`
	Host   string `json:"host"`
}

var (
	hostOnce     sync.Once
	hostPrefixes []hostPrefixEntry
	hostErr      error
)

// loadHostData reads and parses parse/data/hosts.json from the embedded
// filesystem exactly once (sync.Once), mirroring loadParseData's determinism
// contract. Entries are sorted longest-prefix-first so greedy matching prefers
// the most specific host token. On any load/parse error the returned slice is
// nil and DetectHost degrades to a no-op (HostNone), never panicking.
func loadHostData() ([]hostPrefixEntry, error) {
	hostOnce.Do(func() {
		raw, err := parseDataFS.ReadFile("parse/data/hosts.json")
		if err != nil {
			hostErr = fmt.Errorf(
				"bestiary parse: load hosts.json: %w\n"+
					"  What: cannot read embedded host-prefix table\n"+
					"  Where: parse/data/hosts.json\n"+
					"  Why: file missing from embedded FS (should not happen in production build)\n"+
					"  How to fix: ensure parse/data/hosts.json is present before building",
				err,
			)
			return
		}
		var file struct {
			Comment       string            `json:"_comment"`
			SchemaVersion int               `json:"schema_version"`
			Prefixes      []hostPrefixEntry `json:"prefixes"`
		}
		if err := json.Unmarshal(raw, &file); err != nil {
			hostErr = fmt.Errorf(
				"bestiary parse: parse hosts.json: %w\n"+
					"  What: JSON unmarshal failed\n"+
					"  Where: parse/data/hosts.json\n"+
					"  How to fix: validate JSON syntax in the data file",
				err,
			)
			return
		}
		entries := make([]hostPrefixEntry, 0, len(file.Prefixes))
		for _, e := range file.Prefixes {
			if e.Prefix == "" || e.Host == "" {
				// A row missing either side cannot route; skip defensively.
				continue
			}
			entries = append(entries, hostPrefixEntry{
				Prefix: strings.ToLower(e.Prefix),
				Host:   e.Host,
			})
		}
		// Greedy longest-first so a more specific host token wins when two
		// curated prefixes share a leading substring.
		sort.Slice(entries, func(i, j int) bool {
			return len(entries[i].Prefix) > len(entries[j].Prefix)
		})
		hostPrefixes = entries
	})
	return hostPrefixes, hostErr
}

// DetectHost inspects a model ID for a curated serving-host PREFIX and, on a
// match, returns the corresponding Host plus the host-stripped ID. For example
// "azure-gpt-4o" → (HostAzure, "gpt-4o"). When no curated prefix matches it
// returns (HostNone, id) unchanged.
//
// Detection is intentionally curated and ID-PREFIX-ONLY:
//
//   - It NEVER consults the Provider field. A genuine host-as-provider (e.g.
//     provider "azure-cognitive-services" serving a plain "gpt-4o" ID) carries
//     no host prefix on its ID, so Host stays HostNone — the host is implied by
//     the Provider, not duplicated as an instance attribute. This is the guard
//     against the v0.2.2 blanket provider-name strip that erased a backend label.
//   - Namespaced IDs (org/model, containing "/") are never split, so an org
//     token that merely begins with a host word (e.g.
//     "azure-cognitive-services/gpt-4o") is left untouched and Host stays
//     HostNone.
//
// Host is a per-instance ATTRIBUTE: stripping it makes the remaining
// (Family,Variant,Version) tuple host-independent, so a host-routed instance
// shares its entity identity with the plainly-served model.
func DetectHost(id ModelID) (Host, ModelID) {
	entries, err := loadHostData()
	if err != nil || len(entries) == 0 {
		return HostNone, id
	}
	s := strings.ToLower(string(id))
	// Namespaced IDs keep the host implied by the org segment; never split here.
	if strings.Contains(s, "/") {
		return HostNone, id
	}
	for _, e := range entries {
		marker := e.Prefix + "-"
		// Require a non-empty remainder after the "<prefix>-" so a bare host
		// token (e.g. "azure" alone) is never treated as a model.
		if strings.HasPrefix(s, marker) && len(s) > len(marker) {
			// Slice the ORIGINAL id by the matched byte length to preserve the
			// remainder's original case; the prefix region is ASCII so the
			// lowercased and original byte lengths are identical.
			return Host(e.Host), ModelID(string(id)[len(marker):])
		}
	}
	return HostNone, id
}
