package bestiary

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// OutputFormat specifies how models are rendered for display.
type OutputFormat string

const (
	FormatJSON  OutputFormat = "json"
	FormatYAML  OutputFormat = "yaml"
	FormatTable OutputFormat = "table"
)

// InputFormat specifies the input scheme for parsing a model identity string
// in the bestiary show command.
//
// The default is InputFormatPeasant (bestiary canonical form). Other formats
// must be explicitly selected via --format on the CLI.
type InputFormat string

const (
	// InputFormatPeasant is the bestiary canonical form:
	//   [<provider>/]<family>[/<variant>[/<version>]][@<date>]
	// This is the default input format.
	InputFormatPeasant InputFormat = "peasant"

	// InputFormatHuggingFace is the HuggingFace Hub form:
	//   <provider>/<raw-model-id>
	InputFormatHuggingFace InputFormat = "huggingface"

	// InputFormatPURL is the Package URL (PURL) form. This is an INPUT format, and
	// input stays lenient (Postel): both the registry-accurate
	// "pkg:huggingface/<org>/<repo>" spelling and the legacy
	// "pkg:huggingface/<provider>/<raw-model-id>" spelling this package once emitted
	// are accepted. The corresponding OUTPUT render (SchemePURL) is narrower — it is
	// minted only for HuggingFace-hosted refs (see canonical.go SchemePURL).
	InputFormatPURL InputFormat = "purl"

	// InputFormatRaw is the raw API model ID (exact match):
	//   <raw-model-id>
	InputFormatRaw InputFormat = "raw"

	// InputFormatOCI is the purl-spec `pkg:oci` external-identifier form. It maps to
	// SchemeOCI. OCI identity is per-quant-digest (QuantVRAM.OCIDigest), so a bare
	// ModelRef has no OCI render — the format is recognized for scheme selection and
	// JSON round-trip symmetry, but its ModelRef render is "" by design.
	InputFormatOCI InputFormat = "oci"
)

// FormatModels writes a list of models to w in the specified format.
func FormatModels(w io.Writer, models []ModelInfo, format OutputFormat) error {
	switch format {
	case FormatJSON:
		return formatModelsJSON(w, models)
	case FormatYAML:
		return formatModelsYAML(w, models)
	case FormatTable:
		return formatModelsTable(w, models)
	default:
		return fmt.Errorf(
			"bestiary: FormatModels: unknown output format %q; supported formats: json, yaml, table",
			string(format),
		)
	}
}

// FormatModel writes a single model to w in the specified format.
func FormatModel(w io.Writer, model ModelInfo, format OutputFormat) error {
	switch format {
	case FormatJSON:
		return formatModelJSON(w, model)
	case FormatYAML:
		return formatModelYAML(w, model)
	case FormatTable:
		return formatModelTable(w, model)
	default:
		return fmt.Errorf(
			"bestiary: FormatModel: unknown output format %q; supported formats: json, yaml, table",
			string(format),
		)
	}
}

// --- JSON ---

func formatModelsJSON(w io.Writer, models []ModelInfo) error {
	enc, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return fmt.Errorf("bestiary: FormatModels(JSON): marshal: %w", err)
	}
	_, err = w.Write(enc)
	if err != nil {
		return fmt.Errorf("bestiary: FormatModels(JSON): write: %w", err)
	}
	return nil
}

func formatModelJSON(w io.Writer, model ModelInfo) error {
	enc, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return fmt.Errorf("bestiary: FormatModel(JSON): marshal: %w", err)
	}
	_, err = w.Write(enc)
	if err != nil {
		return fmt.Errorf("bestiary: FormatModel(JSON): write: %w", err)
	}
	return nil
}

// --- YAML (internal minimal serializer, no external dependency) ---
//
// Handles flat struct fields for ModelInfo:
//   - string  → field: "value"
//   - int     → field: 123
//   - bool    → field: true
//   - *float64 nil  → field: null
//   - *float64 non-nil → field: 15.0 (or integer form when whole number)
//   - []Modality → field:\n  - text\n  - image
//   - Modalities (nested) → field:\n  input:\n    - text\n  output:\n    - text

func writeYAMLString(sb *strings.Builder, indent, key, value string) {
	fmt.Fprintf(sb, "%s%s: %q\n", indent, key, value)
}

// writeYAMLStringSlice renders a string slice as an inline YAML flow sequence
// (e.g. "Modifier: [vision, instruct]"). A nil/empty slice renders as "[]".
// Modifier became a list; values are emitted in their stored canonical
// order so the output is deterministic.
func writeYAMLStringSlice(sb *strings.Builder, indent, key string, values []string) {
	if len(values) == 0 {
		fmt.Fprintf(sb, "%s%s: []\n", indent, key)
		return
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	fmt.Fprintf(sb, "%s%s: [%s]\n", indent, key, strings.Join(quoted, ", "))
}

func writeYAMLInt(sb *strings.Builder, indent, key string, value int) {
	fmt.Fprintf(sb, "%s%s: %d\n", indent, key, value)
}

func writeYAMLBool(sb *strings.Builder, indent, key string, value bool) {
	fmt.Fprintf(sb, "%s%s: %t\n", indent, key, value)
}

func writeYAMLFloat64Ptr(sb *strings.Builder, indent, key string, p *float64) {
	if p == nil {
		fmt.Fprintf(sb, "%s%s: null\n", indent, key)
	} else {
		// Use %g to avoid unnecessary trailing zeros but ensure a decimal point.
		formatted := fmt.Sprintf("%g", *p)
		if !strings.Contains(formatted, ".") && !strings.Contains(formatted, "e") {
			formatted += ".0"
		}
		fmt.Fprintf(sb, "%s%s: %s\n", indent, key, formatted)
	}
}

func writeYAMLCapability(sb *strings.Builder, indent, key string, c Capability) {
	if c.Config == nil {
		fmt.Fprintf(sb, "%s%s: %t\n", indent, key, c.Supported)
		return
	}
	// Config present — render as a sub-object.
	fmt.Fprintf(sb, "%s%s:\n", indent, key)
	fmt.Fprintf(sb, "%s  supported: %t\n", indent, c.Supported)
	fmt.Fprintf(sb, "%s  config:\n", indent)
	for k, v := range c.Config {
		fmt.Fprintf(sb, "%s    %s: %q\n", indent, k, v)
	}
}

func writeYAMLModalities(sb *strings.Builder, indent string, mods Modalities) {
	fmt.Fprintf(sb, "%sModalities:\n", indent)
	fmt.Fprintf(sb, "%s  Input:\n", indent)
	for _, m := range mods.Input {
		fmt.Fprintf(sb, "%s    - %s\n", indent, m.String())
	}
	fmt.Fprintf(sb, "%s  Output:\n", indent)
	for _, m := range mods.Output {
		fmt.Fprintf(sb, "%s    - %s\n", indent, m.String())
	}
}

func modelToYAML(m ModelInfo, indent string) string {
	var sb strings.Builder
	writeYAMLString(&sb, indent, "ID", string(m.ID))
	writeYAMLString(&sb, indent, "Provider", string(m.Provider))
	writeYAMLString(&sb, indent, "DisplayName", m.DisplayName)
	writeYAMLString(&sb, indent, "Description", m.Description)
	writeYAMLString(&sb, indent, "RawFamily", string(m.RawFamily))
	writeYAMLString(&sb, indent, "Family", string(m.Family))
	writeYAMLString(&sb, indent, "Variant", m.Variant)
	writeYAMLString(&sb, indent, "Date", m.Date)
	writeYAMLStringSlice(&sb, indent, "Modifier", m.Modifier)
	writeYAMLInt(&sb, indent, "ContextWindow", m.ContextWindow)
	writeYAMLInt(&sb, indent, "MaxOutput", m.MaxOutput)
	writeYAMLBool(&sb, indent, "Reasoning", m.Reasoning)
	writeYAMLBool(&sb, indent, "ToolCall", m.ToolCall)
	writeYAMLBool(&sb, indent, "Attachment", m.Attachment)
	writeYAMLBool(&sb, indent, "Temperature", m.Temperature)
	writeYAMLBool(&sb, indent, "StructuredOutput", m.StructuredOutput)
	writeYAMLCapability(&sb, indent, "Interleaved", m.Interleaved)
	writeYAMLBool(&sb, indent, "OpenWeights", m.OpenWeights)
	// Status is the api.json instance-level release status; it renders as its
	// canonical wire name (e.g. "none", "beta"). Added in v0.2.5 alongside
	// Description as the two flat metadata scalars the minimal serializer carries.
	writeYAMLString(&sb, indent, "Status", m.Status.String())
	writeYAMLFloat64Ptr(&sb, indent, "CostInputPerMTok", m.CostInputPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostOutputPerMTok", m.CostOutputPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostReasoningPerMTok", m.CostReasoningPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostCacheReadPerMTok", m.CostCacheReadPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostCacheWritePerMTok", m.CostCacheWritePerMTok)
	writeYAMLString(&sb, indent, "ReleaseDate", m.ReleaseDate)
	writeYAMLString(&sb, indent, "Knowledge", m.Knowledge)
	// Region is the per-instance serving jurisdiction (AWS Bedrock cross-region
	// inference profile); it renders as its lowercase token ("unspecified" for the
	// RegionNone zero value, never blank). Added in v0.2.7.
	writeYAMLString(&sb, indent, "Region", m.Region.String())
	writeYAMLString(&sb, indent, "LastSynced", m.LastSynced)
	writeYAMLModalities(&sb, indent, m.Modalities)
	return sb.String()
}

func formatModelsYAML(w io.Writer, models []ModelInfo) error {
	var sb strings.Builder
	sb.WriteString("models:\n")
	for _, m := range models {
		sb.WriteString("  - ")
		// First field inlined after "  - ", rest indented by "    ".
		lines := strings.SplitAfter(modelToYAML(m, "    "), "\n")
		if len(lines) > 0 {
			// Replace leading 4-space indent on first line with empty (the "  - " prefix handles it).
			sb.WriteString(strings.TrimPrefix(lines[0], "    "))
		}
		for _, line := range lines[1:] {
			sb.WriteString(line)
		}
	}
	_, err := fmt.Fprint(w, sb.String())
	if err != nil {
		return fmt.Errorf("bestiary: FormatModels(YAML): write: %w", err)
	}
	return nil
}

func formatModelYAML(w io.Writer, model ModelInfo) error {
	_, err := fmt.Fprint(w, modelToYAML(model, ""))
	if err != nil {
		return fmt.Errorf("bestiary: FormatModel(YAML): write: %w", err)
	}
	return nil
}

// --- Table ---

// tableHeader is the format string for the header and separator rows (all %s args).
// The Creator column (the lab/originator that TRAINED the weights, SPDX "originator")
// sits between Family and Context: it is a per-model identity fact distinct from the
// Provider (the SPDX supplier/distributor) already shown to its left.
const tableHeader = "%-40s  %-12s  %-16s  %-12s  %9s  %9s  %6s  %5s  %12s\n"
const tableRow = "%-40s  %-12s  %-16s  %-12s  %9d  %9d  %6s  %5s  %12s\n"

func costStr(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *p)
}

// creatorCol renders a Creator for a table cell, mapping CreatorNone (the empty
// zero value) to a dash so a family with no curated creator mapping reads as an
// honest blank — never an invented "unknown" label.
func creatorCol(c Creator) string {
	if c == CreatorNone {
		return "-"
	}
	return string(c)
}

func boolCol(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func printTableHeader(w io.Writer) {
	fmt.Fprintf(w, tableHeader,
		"ID", "Provider", "Family", "Creator", "Context", "MaxOutput", "Reason", "Tools", "CostIn/MTok",
	)
	fmt.Fprintf(w, tableHeader,
		strings.Repeat("-", 40),
		strings.Repeat("-", 12),
		strings.Repeat("-", 16),
		strings.Repeat("-", 12),
		strings.Repeat("-", 9),
		strings.Repeat("-", 9),
		strings.Repeat("-", 6),
		strings.Repeat("-", 5),
		strings.Repeat("-", 12),
	)
}

func printTableModelRow(w io.Writer, m ModelInfo) {
	fmt.Fprintf(w, tableRow,
		string(m.ID),
		string(m.Provider),
		m.Family,
		creatorCol(m.Creator),
		m.ContextWindow,
		m.MaxOutput,
		boolCol(m.Reasoning),
		boolCol(m.ToolCall),
		costStr(m.CostInputPerMTok),
	)
}

func formatModelsTable(w io.Writer, models []ModelInfo) error {
	printTableHeader(w)
	for _, m := range models {
		printTableModelRow(w, m)
	}
	return nil
}

func formatModelTable(w io.Writer, model ModelInfo) error {
	printTableHeader(w)
	printTableModelRow(w, model)
	return nil
}

// --- ErrAmbiguous two-section output ---

// ambiguousMaxCanonical is the maximum number of canonical rows displayed in
// Section 1 before truncation with a "+N more" hint.
const ambiguousMaxCanonical = 5

// ambiguousMaxRehosts is the maximum number of distinct rehost provider names
// displayed in Section 2 before truncation with a "+N more" hint.
const ambiguousMaxRehosts = 5

// FormatAmbiguous writes a human-readable two-section disambiguation message for
// e to w (typically os.Stderr).
//
// Output format (no leading "bestiary: " prefix — the caller supplies the sole
// one on its wrapped error; see the header comment in the body):
//
//	"<input>" matched several distinct models — candidates below:
//	[no matches in namespace "..." — ... (when PURLMissedNamespace is set)]
//
//	* = canonical provider      (only when Section 1 renders)
//
//	Canonical:                  (Section 1 — only when canonical rows exist)
//	* <canonical-form>
//	... (up to 5 rows; "+N more" when >5)
//
//	Candidates:                 (Section 1' — only when NO canonical rows exist)
//	  <entity-key>
//	... (up to 5 rows; "… and N more" when >5)
//
//	Also rehosted by:           (omitted when RehostProviders is empty)
//	  <provider-name>           (one per line, up to 5; "+N more" when >5)
//	  <provider-name>
//	  ...
//
//	To see all providers/variants: bestiary list   (or: bestiary list --provider <slug>)
//
// The exact-ID escape hatch (--format=raw) is intentionally NOT repeated here:
// the CLI's wrapped ErrAmbiguous message (runShow, cmd/bestiary/main.go) already
// carries that instruction as part of its narrowing list, immediately below this
// output on stderr — one tip, one place, so the two blocks complement rather than
// restate each other.
//
// Section 1 (Canonical) shows up to 5 representatives from Candidates where
// the Provider is the canonical/originating provider for the family. Each row
// is prefixed with "* " to visually mark the canonical origin. When NO candidate
// has a canonical provider, Section 1' (Candidates) lists up to 5 candidate
// entity keys instead, so the guidance's "candidates ... listed above" holds for
// every family class rather than pointing at bare provider slugs.
//
// Section 2 (Also rehosted by) lists up to 5 distinct provider names taken
// directly from ErrAmbiguous.RehostProviders. The section is omitted entirely
// when RehostProviders is empty.
//
// The function always returns nil; write errors are silently swallowed because
// this is advisory stderr output — a write failure should not mask the real
// ErrAmbiguous that the caller surfaces to the user.
func FormatAmbiguous(w io.Writer, e *ErrAmbiguous) {
	// No "bestiary: " prefix here: the caller (runShow) returns a single wrapped
	// error that the CLI prints with the sole "bestiary: " preamble, so this
	// advisory body must NOT open with a second one — two stacked "bestiary: "
	// lines read as two separate failures for one error. This header cues the
	// listing below without restating the distinct-model count the wrapped error
	// already carries.
	fmt.Fprintf(w, "%q matched several distinct models — candidates below:\n", e.Input)

	// PURL missed-namespace note: keep at top, unchanged from Fix 2.
	if e.PURLMissedNamespace != "" {
		fmt.Fprintf(w, "\nno matches in namespace %q — performing loose match across all providers\n",
			e.PURLMissedNamespace)
	}

	// Section 1: the two ORIGINATING axes, rendered as separate sections in
	// preference order — Creator first, then Canonical.
	//
	// The axes are distinct facts and the listing says so rather than collapsing them:
	// Creator answers "which lab made this and where does that lab serve it", Canonical
	// answers "which provider is the curated originating host for this family". They
	// frequently coincide (Anthropic both creates and hosts Claude) and frequently do
	// not (Meta creates Llama; the curated canonical provider for llama is "local").
	// Each section is suppressed INDEPENDENTLY when it has no rows, so a family with a
	// creator but no canonical provider renders only Creator, and vice versa — neither
	// a bare header nor an orphaned legend is ever emitted.
	//
	// A row is assigned to at most ONE section, Creator winning, so a provider that is
	// both the creator's surface and the family's canonical provider is listed once.
	// Dedup by (Family, Variant, Version) so each model appears once, and the dedup is
	// SHARED across both sections for the same reason.
	type groupKey struct {
		family  string
		variant string
		version string
	}
	seenGroup := make(map[groupKey]struct{})
	var creatorRows, canonicalRows []ModelRef
	for _, c := range e.Candidates {
		if isRehostProvider(c.Family, c.Provider) {
			continue
		}
		key := groupKey{
			family:  string(c.Family),
			variant: c.Variant,
			version: c.Version,
		}
		if _, dup := seenGroup[key]; dup {
			continue
		}
		seenGroup[key] = struct{}{}
		if isCreatorProvider(c.Family, c.Provider) {
			creatorRows = append(creatorRows, c)
		} else {
			canonicalRows = append(canonicalRows, c)
		}
	}

	if len(creatorRows) > 0 {
		// Legend line — only shown when there are creator rows to explain.
		fmt.Fprintf(w, "\n+ = served by the creating lab\n")

		fmt.Fprintf(w, "\nCreator:\n")
		displayCreator := creatorRows
		creatorOverflow := 0
		if len(creatorRows) > ambiguousMaxCanonical {
			creatorOverflow = len(creatorRows) - ambiguousMaxCanonical
			displayCreator = creatorRows[:ambiguousMaxCanonical]
		}
		for _, c := range displayCreator {
			fmt.Fprintf(w, "+ %s\n", c.Format(SchemeCanonical))
		}
		if creatorOverflow > 0 {
			// "… and N more", not the Canonical section's "+N more": the Creator rows
			// are themselves prefixed "+ ", so "+3 more" would read as a fourth row.
			fmt.Fprintf(w, "… and %d more\n", creatorOverflow)
		}
	}

	// When canonicalRows is empty (unknown canonical provider for this family, e.g.
	// "minimax"), omit the legend and the Canonical section entirely. A bare empty
	// "Canonical:" header with an orphaned legend is misleading — the user sees no
	// canonical rows and no explanation. When canonical rows are present, the legend +
	// section together form a coherent unit.
	if len(canonicalRows) > 0 {
		// Legend line — only shown when there are canonical rows to explain.
		fmt.Fprintf(w, "\n* = canonical provider\n")

		fmt.Fprintf(w, "\nCanonical:\n")
		displayCanonical := canonicalRows
		canonicalOverflow := 0
		if len(canonicalRows) > ambiguousMaxCanonical {
			canonicalOverflow = len(canonicalRows) - ambiguousMaxCanonical
			displayCanonical = canonicalRows[:ambiguousMaxCanonical]
		}
		for _, c := range displayCanonical {
			fmt.Fprintf(w, "* %s\n", c.Format(SchemeCanonical))
		}
		if canonicalOverflow > 0 {
			fmt.Fprintf(w, "+%d more\n", canonicalOverflow)
		}
	}

	if len(creatorRows) == 0 && len(canonicalRows) == 0 {
		// NEITHER originating axis produced a row: the family has no creator surface
		// among the candidates AND no canonical-provider row. Without this section the
		// only thing rendered above the wrapped error's "the matching candidates are
		// listed above" would be the bare provider slugs of "Also rehosted by:",
		// making that claim FALSE — a slug is not a candidate. List the candidate
		// ENTITY forms directly (deduped by identity key) so "candidates" names actual
		// model identities for every family class, not just first-party-hosted ones.
		seenKey := make(map[string]struct{})
		var candKeys []string
		for _, c := range e.Candidates {
			key := EntityRef{
				Family:    c.Family,
				Variant:   c.Variant,
				Version:   c.Version,
				ParamSize: c.ParamSize,
				Modifier:  EntityModifiers(c.Modifier, c.Family),
			}.String()
			if key == "" {
				continue
			}
			if _, dup := seenKey[key]; dup {
				continue
			}
			seenKey[key] = struct{}{}
			candKeys = append(candKeys, key)
		}
		if len(candKeys) > 0 {
			fmt.Fprintf(w, "\nCandidates:\n")
			displayKeys := candKeys
			keyOverflow := 0
			if len(candKeys) > ambiguousMaxCanonical {
				keyOverflow = len(candKeys) - ambiguousMaxCanonical
				displayKeys = candKeys[:ambiguousMaxCanonical]
			}
			for _, k := range displayKeys {
				fmt.Fprintf(w, "  %s\n", k)
			}
			if keyOverflow > 0 {
				fmt.Fprintf(w, "  … and %d more\n", keyOverflow)
			}
		}
	}

	// Section 2: Rehost provider names — up to ambiguousMaxRehosts.
	// Omit the section entirely when RehostProviders is empty.
	if len(e.RehostProviders) > 0 {
		fmt.Fprintf(w, "\nAlso rehosted by:\n")
		displayRehosts := e.RehostProviders
		rehostOverflow := 0
		if len(e.RehostProviders) > ambiguousMaxRehosts {
			rehostOverflow = len(e.RehostProviders) - ambiguousMaxRehosts
			displayRehosts = e.RehostProviders[:ambiguousMaxRehosts]
		}
		// Render each provider name on its own line, indented by two spaces.
		for _, p := range displayRehosts {
			fmt.Fprintf(w, "  %s\n", string(p))
		}
		if rehostOverflow > 0 {
			fmt.Fprintf(w, "+%d more\n", rehostOverflow)
		}
	}

	// Footer: verified real commands only. The --format=raw exact-ID tip lives
	// solely in the CLI's wrapped ErrAmbiguous message (runShow), not here — see
	// the FormatAmbiguous doc comment for why.
	fmt.Fprintf(w, "\nTo see all providers/variants: bestiary list   (or: bestiary list --provider <slug>)\n")
}
