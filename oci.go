package bestiary

import "strings"

// ociOllamaRegistry is the purl `repository_url` qualifier value for Ollama-registry
// artifacts: the anonymous Docker-Distribution-v2 host plus the `library` namespace
// path. The purl-spec `oci` type PROHIBITS a namespace on the purl itself — a
// namespace that is part of the physical location belongs in repository_url — so the
// `library/` prefix Ollama uses lives here, never in the purl name (research §1B).
const ociOllamaRegistry = "registry.ollama.ai/library"

// formatOCIPurl renders a purl-spec `pkg:oci` package URL for a content-addressed OCI
// artifact (the Ollama registry manifest of one quantization). The `oci` type
// (packageurl.org/types/oci-definition.json, research §1B):
//
//		pkg:oci/<name>@<version>?<qualifiers>
//
//	  - name is REQUIRED, NOT case-sensitive (must be lowercased), and is the LAST
//	    fragment of the repository name (e.g. "library/llama3.1" → "llama3.1").
//	  - version is the "sha256:<hex_lowercase_digest>" of the artifact and is what
//	    UNIQUELY identifies it — so an OCI purl is NEVER minted without a digest: a
//	    digest-less "pkg:oci/<name>" would not identify any artifact. When digest == ""
//	    the function returns "" (an honest empty, never a foreign key that resolves to
//	    nothing) — the falsifiable MUST-FAIL case.
//	  - repository_url and tag are OPTIONAL qualifiers, emitted only when supplied, and
//	    rendered in purl-canonical alphabetical key order (repository_url before tag).
//
// The ':' in the sha256 version is percent-encoded to "%3A" (spec examples show the
// encoded form); reserved characters in qualifier values ('/', ':') are likewise
// percent-encoded so a repository_url path or a port survives round-trip.
func formatOCIPurl(name, digest, tag, registry string) string {
	if digest == "" {
		// Content-addressed identity requires the digest; without it there is no
		// artifact to name. Mint NOTHING rather than a digest-less purl.
		return ""
	}

	// name: purl-spec requires the lowercased LAST path fragment.
	n := strings.ToLower(name)
	if i := strings.LastIndexByte(n, '/'); i >= 0 {
		n = n[i+1:]
	}
	if n == "" {
		return ""
	}

	// version: the sha256:<hex> manifest digest, lowercased, prefix-normalized, with
	// ':' percent-encoded. A caller may pass either "sha256:<hex>" or a bare "<hex>";
	// both normalize to the same "sha256%3A<hex>" version.
	ver := strings.ToLower(digest)
	if !strings.HasPrefix(ver, "sha256:") {
		ver = "sha256:" + ver
	}
	ver = strings.ReplaceAll(ver, ":", "%3A")

	var b strings.Builder
	b.WriteString("pkg:oci/")
	b.WriteString(n)
	b.WriteByte('@')
	b.WriteString(ver)

	// Qualifiers in purl-canonical alphabetical order: repository_url < tag.
	var quals []string
	if registry != "" {
		quals = append(quals, "repository_url="+ociEncodeQualifier(registry))
	}
	if tag != "" {
		quals = append(quals, "tag="+ociEncodeQualifier(tag))
	}
	if len(quals) > 0 {
		b.WriteByte('?')
		b.WriteString(strings.Join(quals, "&"))
	}
	return b.String()
}

// ociEncodeQualifier percent-encodes the reserved characters that would otherwise
// break purl qualifier parsing ('/' path separators, ':' port/scheme markers). It is
// deliberately minimal (stdlib-only, no net/url which would '+'-encode spaces): the
// qualifier values bestiary emits are registry hostnames/paths and tag tokens, which
// contain at most these two reserved characters.
func ociEncodeQualifier(v string) string {
	return ociQualifierReplacer.Replace(v)
}

var ociQualifierReplacer = strings.NewReplacer(
	"/", "%2F",
	":", "%3A",
)
