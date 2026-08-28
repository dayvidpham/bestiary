package bestiary_test

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// The archived_url invariant.
//
// The 2026-08-28 refresh deviated from the slice brief, which asked for "159+ archived_url
// values before and after". Ten harvested Hub repos were removed because upstream retired
// the entities they named (the whole Phi-3 / Phi-3.5 line and CodeLlama), so a literal
// floor over a shrinking corpus is the wrong rule. The invariant offered in its place was
// ZERO ERASURES AMONG SURVIVORS — and it was asserted only in prose. Nothing in the tree
// enforced it, on either side of the refresh.
//
// This file enforces it. archived_url is the ONLY durable record of a model card that has
// since disappeared from the Hub, and cmd/bestiary-hf rewrites huggingface_nomina.json
// wholesale on every harvest. The failure it must catch is a surviving repo silently
// losing its snapshot inside a large JSON churn, with every other test green.
//
// A COUNT alone cannot catch that: one erasure hiding behind one addition leaves the count
// unmoved. So the SET is pinned too, in testdata/hf_archived_url_survivors.txt, and an
// erasure fails by repo name.

// hfNominaFile is the shape this test reads. It is deliberately a local, minimal struct
// rather than the package loader: the invariant is about the COMMITTED ARTIFACT on disk,
// so the assertion must not travel through the code that could normalise the defect away.
type hfNominaFile struct {
	Nomina []struct {
		Value       string `json:"value"`
		SourceURL   string `json:"source_url"`
		ArchivedURL string `json:"archived_url,omitempty"`
	} `json:"nomina"`
}

const hfNominaPath = "parse/data/huggingface_nomina.json"

func loadHFNominaRaw(t *testing.T) hfNominaFile {
	t.Helper()
	raw, err := os.ReadFile(hfNominaPath)
	if err != nil {
		t.Fatalf("read %s: %v", hfNominaPath, err)
	}
	var f hfNominaFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode %s: %v", hfNominaPath, err)
	}
	return f
}

// TestHFArchivedURL_CensusExact pins the two counts with their arithmetic, the same way
// every other census literal in this repo is pinned.
func TestHFArchivedURL_CensusExact(t *testing.T) {
	// 159 -> 153 at the 2026-08-28 refresh. The first Wayback capture recorded 159
	// snapshots over 184 harvested repos. The refresh removed 10 repos whose entities
	// upstream retired (the Phi-3 / Phi-3.5 line and CodeLlama), 184 -> 174, and 6 of
	// those 10 carried a snapshot: 159 - 6 = 153. The other 4 never had one. NO surviving
	// repo lost its snapshot, which is the invariant the set assertion below enforces.
	const wantRecords = 174
	const wantArchived = 153

	f := loadHFNominaRaw(t)
	if got := len(f.Nomina); got != wantRecords {
		t.Errorf("%s holds %d records, want %d\n"+
			"  How to fix: this literal moves only with a declared cmd/bestiary-hf harvest; re-pin it\n"+
			"    in the same commit as the harvest, with the arithmetic, and re-pin wantHuggingFace",
			hfNominaPath, got, wantRecords)
	}
	archived := 0
	for _, n := range f.Nomina {
		if strings.TrimSpace(n.ArchivedURL) != "" {
			archived++
		}
	}
	if archived != wantArchived {
		t.Errorf("%s carries %d archived_url values, want %d\n"+
			"  What: the count of durable archive.org snapshots moved\n"+
			"  Why it matters: archived_url is the only record of a model card that later leaves the\n"+
			"    Hub, and a harvest rewrites this file wholesale\n"+
			"  How to fix: if a harvest deliberately dropped repos, state the arithmetic here as the\n"+
			"    comment above does; if it did not, the harvest ERASED snapshots and must be re-run",
			hfNominaPath, archived, wantArchived)
	}
}

// TestHFArchivedURL_NoErasureAmongSurvivors is the invariant itself. Every repo in the
// committed survivor set that is STILL PRESENT in huggingface_nomina.json must still carry
// a non-empty archived_url. A repo leaving the corpus entirely is a separate, visible event
// (the census above moves); a repo staying and losing its snapshot is the silent erasure.
func TestHFArchivedURL_NoErasureAmongSurvivors(t *testing.T) {
	const survivorsPath = "testdata/hf_archived_url_survivors.txt"

	fh, err := os.Open(survivorsPath)
	if err != nil {
		t.Fatalf("read %s: %v", survivorsPath, err)
	}
	defer fh.Close()
	var pinned []string
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pinned = append(pinned, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", survivorsPath, err)
	}
	if len(pinned) == 0 {
		t.Fatalf("%s is empty — the survivor set must never be emptied to make this gate pass", survivorsPath)
	}

	live := map[string]string{}
	for _, n := range loadHFNominaRaw(t).Nomina {
		live[n.Value] = strings.TrimSpace(n.ArchivedURL)
	}

	var erased, departed []string
	for _, repo := range pinned {
		got, present := live[repo]
		if !present {
			departed = append(departed, repo)
			continue
		}
		if got == "" {
			erased = append(erased, repo)
		}
	}
	sort.Strings(erased)
	sort.Strings(departed)

	if len(erased) > 0 {
		t.Errorf("%d surviving repo(s) LOST their archive.org snapshot: %s\n"+
			"  What: each repo below is still in %s but its archived_url is now empty\n"+
			"  Why it matters: archived_url is the only durable record of a model card that later\n"+
			"    disappears from the Hub. A Wayback MISS is not a deletion — cmd/bestiary-hf preserves\n"+
			"    a snapshot an earlier run found — so an empty value on a surviving repo means the\n"+
			"    merge-on-refresh contract was broken, not that the archive is missing\n"+
			"  Where: %s, and the pinned survivor set in %s\n"+
			"  How to fix: re-run the harvest; do NOT re-pin the survivor set to make this pass",
			len(erased), strings.Join(erased, ", "), hfNominaPath, hfNominaPath, survivorsPath)
	}
	if len(departed) > 0 {
		t.Errorf("%d pinned survivor(s) left the corpus entirely: %s\n"+
			"  What: these repos are in the pinned survivor set but absent from %s\n"+
			"  Why it matters: a repo leaves only when upstream retires the entity it names. That is a\n"+
			"    legitimate, but REVIEWED, event — it shrinks the durable archive record\n"+
			"  How to fix: confirm each repo's entity is genuinely gone from the catalog, then re-pin\n"+
			"    the survivor set and the census arithmetic in the SAME commit, naming why",
			len(departed), strings.Join(departed, ", "), hfNominaPath)
	}
}

// TestHFArchivedURL_EveryArchivedRecordIsWellFormed asserts the per-record shape the
// deviation claims: a record carrying archived_url carries a NON-EMPTY value that is an
// archive.org snapshot OF that record's own source_url. An empty-string archived_url is
// an erasure wearing the shape of a present field, which the count above cannot see.
func TestHFArchivedURL_EveryArchivedRecordIsWellFormed(t *testing.T) {
	raw, err := os.ReadFile(hfNominaPath)
	if err != nil {
		t.Fatalf("read %s: %v", hfNominaPath, err)
	}
	// Decode into a generic shape so a PRESENT-but-empty archived_url key is visible;
	// the typed struct above cannot distinguish it from an absent key.
	var generic struct {
		Nomina []map[string]any `json:"nomina"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decode %s: %v", hfNominaPath, err)
	}
	for i, n := range generic.Nomina {
		v, present := n["archived_url"]
		if !present {
			continue
		}
		value, _ := n["value"].(string)
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			t.Errorf("nomina[%d] (value=%q) carries an EMPTY archived_url\n"+
				"  What: the key is present but holds no snapshot\n"+
				"  Why it matters: absent means \"none recorded\", which is normal; present-but-empty is\n"+
				"    an erasure that the count and the survivor set both read as a live snapshot\n"+
				"  How to fix: omit the key entirely when there is no snapshot (the field is\n"+
				"    json:\",omitempty\"), or restore the value the earlier harvest recorded", i, value)
			continue
		}
		if !strings.HasPrefix(s, "https://web.archive.org/web/") {
			t.Errorf("nomina[%d] (value=%q) archived_url %q is not an archive.org snapshot URL\n"+
				"  How to fix: record the Wayback snapshot of source_url, or omit the key", i, value, s)
		}
		src, _ := n["source_url"].(string)
		if src != "" && !strings.HasSuffix(s, src) {
			t.Errorf("nomina[%d] (value=%q) archived_url does not snapshot its own source_url\n"+
				"  archived_url: %s\n  source_url:   %s\n"+
				"  How to fix: a snapshot must be OF this record's live repo URL", i, value, s, src)
		}
	}
}
