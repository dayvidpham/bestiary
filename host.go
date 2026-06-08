package bestiary

// Host identifies the serving host / backend infrastructure that actually runs
// a model instance, as distinct from the Provider (the organization that offers
// or sells access to it). The same logical model+provider may be served from
// different backends (e.g. an OpenAI model served via Azure), and the same host
// may back many providers. Host is therefore a per-instance ATTRIBUTE: it never
// participates in entity identity (see EntityRef) and is rendered in the
// "[attributes]" segment of a canonical string, never in the "{identity-mods}"
// key segment.
//
// Host is a string type (not a closed int enum) for the same reason Provider is:
// the set of serving backends is open and grows over time. Well-known hosts have
// named constants for type-safe call sites; unrecognized hosts are still
// representable verbatim. The curated host table (parse/data/hosts.json) that
// populates ModelInfo.Host from the raw API data is owned by a later slice; this
// slice only fixes the type contract and a seed set of constants.
type Host string

const (
	// HostNone is the zero value: the serving host is unknown or unspecified.
	// A genuine "no distinct host" record (e.g. a provider serving its own
	// model directly) is also represented as HostNone.
	HostNone Host = ""
	// HostAzure is Microsoft Azure (e.g. Azure OpenAI / Azure AI Foundry).
	HostAzure Host = "azure"
	// HostAWS is Amazon Web Services (e.g. Bedrock / SageMaker).
	HostAWS Host = "aws"
	// HostGCP is Google Cloud Platform (e.g. Vertex AI).
	HostGCP Host = "gcp"
	// HostCloudflare is Cloudflare (e.g. Workers AI).
	HostCloudflare Host = "cloudflare"
)

// IsKnown reports whether h is one of the named Host constants (excluding
// HostNone). Unrecognized non-empty hosts return false but remain valid Host
// values — callers that need to accept any backend should not gate on IsKnown.
func (h Host) IsKnown() bool {
	switch h {
	case HostAzure, HostAWS, HostGCP, HostCloudflare:
		return true
	default:
		return false
	}
}
