package bestiary

import "testing"

// TestFormatOCIPurl_Spec pins the purl-spec `pkg:oci` render contract (research §1B):
// lowercased last-fragment name, sha256 version with ':' percent-encoded, optional
// repository_url/tag qualifiers in canonical alphabetical order.
func TestFormatOCIPurl_Spec(t *testing.T) {
	cases := []struct {
		name                  string
		in                    string
		digest, tag, registry string
		want                  string
	}{
		{
			name:     "full-with-both-qualifiers",
			in:       "library/llama3.1",
			digest:   "sha256:abc123",
			tag:      "70b",
			registry: "registry.ollama.ai/library",
			// name = last fragment "llama3.1"; ':' → %3A; repository_url before tag;
			// the registry '/' is percent-encoded so the qualifier value round-trips.
			want: "pkg:oci/llama3.1@sha256%3Aabc123?repository_url=registry.ollama.ai%2Flibrary&tag=70b",
		},
		{
			name:   "no-qualifiers",
			in:     "llama3.1",
			digest: "sha256:deadbeef",
			want:   "pkg:oci/llama3.1@sha256%3Adeadbeef",
		},
		{
			name:   "lowercases-name-and-digest",
			in:     "Library/Llama3.1",
			digest: "sha256:ABCDEF",
			want:   "pkg:oci/llama3.1@sha256%3Aabcdef",
		},
		{
			name:   "bare-hex-digest-normalized-to-sha256-prefix",
			in:     "x",
			digest: "ABC123",
			want:   "pkg:oci/x@sha256%3Aabc123",
		},
		{
			name:   "deep-path-takes-last-fragment",
			in:     "a/b/c",
			digest: "sha256:d",
			want:   "pkg:oci/c@sha256%3Ad",
		},
		{
			name:     "registry-only-qualifier",
			in:       "llama3.1",
			digest:   "sha256:d",
			registry: "registry.ollama.ai/library",
			want:     "pkg:oci/llama3.1@sha256%3Ad?repository_url=registry.ollama.ai%2Flibrary",
		},
		{
			name:   "tag-only-qualifier",
			in:     "llama3.1",
			digest: "sha256:d",
			tag:    "latest",
			want:   "pkg:oci/llama3.1@sha256%3Ad?tag=latest",
		},
		{
			// MUST-FAIL: an OCI purl is content-addressed — no digest, no purl.
			name:     "no-digest-yields-empty",
			in:       "llama3.1",
			digest:   "",
			tag:      "70b",
			registry: "registry.ollama.ai/library",
			want:     "",
		},
		{
			name:   "empty-name-after-strip-yields-empty",
			in:     "trailing/",
			digest: "sha256:d",
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatOCIPurl(tc.in, tc.digest, tc.tag, tc.registry)
			if got != tc.want {
				t.Errorf("formatOCIPurl(%q,%q,%q,%q) = %q, want %q",
					tc.in, tc.digest, tc.tag, tc.registry, got, tc.want)
			}
		})
	}
}

// TestInputFormatToScheme_OCI verifies the InputFormat→CanonicalScheme dispatch arm
// (the internal seam Resolve uses) maps InputFormatOCI to SchemeOCI.
func TestInputFormatToScheme_OCI(t *testing.T) {
	if got := inputFormatToScheme(InputFormatOCI); got != SchemeOCI {
		t.Errorf("inputFormatToScheme(InputFormatOCI) = %v, want SchemeOCI", got)
	}
}
