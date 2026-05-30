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

	// InputFormatPURL is the Package URL (PURL) form:
	//   pkg:huggingface/<provider>/<raw-model-id>
	InputFormatPURL InputFormat = "purl"

	// InputFormatRaw is the raw API model ID (exact match):
	//   <raw-model-id>
	InputFormatRaw InputFormat = "raw"
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
	writeYAMLString(&sb, indent, "RawFamily", string(m.RawFamily))
	writeYAMLString(&sb, indent, "Family", string(m.Family))
	writeYAMLString(&sb, indent, "Variant", m.Variant)
	writeYAMLString(&sb, indent, "Date", m.Date)
	writeYAMLString(&sb, indent, "Modifier", m.Modifier)
	writeYAMLInt(&sb, indent, "ContextWindow", m.ContextWindow)
	writeYAMLInt(&sb, indent, "MaxOutput", m.MaxOutput)
	writeYAMLBool(&sb, indent, "Reasoning", m.Reasoning)
	writeYAMLBool(&sb, indent, "ToolCall", m.ToolCall)
	writeYAMLBool(&sb, indent, "Attachment", m.Attachment)
	writeYAMLBool(&sb, indent, "Temperature", m.Temperature)
	writeYAMLBool(&sb, indent, "StructuredOutput", m.StructuredOutput)
	writeYAMLCapability(&sb, indent, "Interleaved", m.Interleaved)
	writeYAMLBool(&sb, indent, "OpenWeights", m.OpenWeights)
	writeYAMLFloat64Ptr(&sb, indent, "CostInputPerMTok", m.CostInputPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostOutputPerMTok", m.CostOutputPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostReasoningPerMTok", m.CostReasoningPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostCacheReadPerMTok", m.CostCacheReadPerMTok)
	writeYAMLFloat64Ptr(&sb, indent, "CostCacheWritePerMTok", m.CostCacheWritePerMTok)
	writeYAMLString(&sb, indent, "ReleaseDate", m.ReleaseDate)
	writeYAMLString(&sb, indent, "Knowledge", m.Knowledge)
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
const tableHeader = "%-40s  %-12s  %-16s  %9s  %9s  %6s  %5s  %12s\n"
const tableRow = "%-40s  %-12s  %-16s  %9d  %9d  %6s  %5s  %12s\n"

func costStr(p *float64) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("$%.2f", *p)
}

func boolCol(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func printTableHeader(w io.Writer) {
	fmt.Fprintf(w, tableHeader,
		"ID", "Provider", "Family", "Context", "MaxOutput", "Reason", "Tools", "CostIn/MTok",
	)
	fmt.Fprintf(w, tableHeader,
		strings.Repeat("-", 40),
		strings.Repeat("-", 12),
		strings.Repeat("-", 16),
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

// --- ErrAmbiguous two-section output (SLICE-FIX-V4-1) ---

// ambiguousMaxCanonical is the maximum number of canonical rows displayed in
// Section 1 before truncation with a "+N more" hint. (SLICE-FIX-V4-1)
const ambiguousMaxCanonical = 5

// ambiguousMaxRehosts is the maximum number of distinct rehost provider names
// displayed in Section 2 before truncation with a "+N more" hint. (SLICE-FIX-V4-1)
const ambiguousMaxRehosts = 5

// FormatAmbiguous writes a human-readable two-section disambiguation message for
// e to w (typically os.Stderr).
//
// Output format:
//
//	bestiary: input "<input>" matched multiple canonicals
//	[no matches in namespace "..." — ... (when PURLMissedNamespace is set)]
//
//	* = canonical provider
//
//	Canonical:
//	* <canonical-form>
//	... (up to 5 rows; "+N more" when >5)
//
//	Also rehosted by:           (omitted when RehostProviders is empty)
//	<provider-name>, <provider-name>, ...  (up to 5 names; "+N more" when >5)
//
//	To see all providers/variants: bestiary list   (or: bestiary list --provider <slug>)
//	To resolve an exact model ID:  bestiary show <raw-id> --format=raw
//
// Section 1 (Canonical) shows up to 5 representatives from Candidates where
// the Provider is the canonical/originating provider for the family. Each row
// is prefixed with "* " to visually mark the canonical origin.
//
// Section 2 (Also rehosted by) lists up to 5 distinct provider names taken
// directly from ErrAmbiguous.RehostProviders. The section is omitted entirely
// when RehostProviders is empty.
//
// The function always returns nil; write errors are silently swallowed because
// this is advisory stderr output — a write failure should not mask the real
// ErrAmbiguous that the caller surfaces to the user.
func FormatAmbiguous(w io.Writer, e *ErrAmbiguous) {
	fmt.Fprintf(w, "bestiary: input %q matched multiple canonicals\n", e.Input)

	// PURL missed-namespace note: keep at top, unchanged from Fix 2 (SLICE-FIX-V3-1).
	if e.PURLMissedNamespace != "" {
		fmt.Fprintf(w, "\nno matches in namespace %q — performing loose match across all providers\n",
			e.PURLMissedNamespace)
	}

	// Section 1: Canonical rows — up to ambiguousMaxCanonical, each prefixed with "* ".
	// Filter candidates to only canonical-provider rows (Provider == Family.CanonicalProvider()).
	// Dedup by (Family, Variant, Version) so each model appears once.
	type groupKey struct {
		family  string
		variant string
		version string
	}
	seenGroup := make(map[groupKey]struct{})
	var canonicalRows []ModelRef
	for _, c := range e.Candidates {
		if c.Provider == "" || c.Provider != c.Family.CanonicalProvider() {
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
		canonicalRows = append(canonicalRows, c)
	}

	// Fix (SLICE-FIX-V4-1-FIX2 FOLD): when canonicalRows is empty (unknown canonical
	// provider for this family, e.g. "minimax"), omit the legend and the Canonical
	// section entirely. A bare empty "Canonical:" header with an orphaned legend is
	// misleading — the user sees no canonical rows and no explanation. When canonical
	// rows are present, the legend + section together form a coherent unit.
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
		// Render as a comma-separated list of provider names on one line.
		names := make([]string, len(displayRehosts))
		for i, p := range displayRehosts {
			names[i] = string(p)
		}
		fmt.Fprintf(w, "%s\n", strings.Join(names, ", "))
		if rehostOverflow > 0 {
			fmt.Fprintf(w, "+%d more\n", rehostOverflow)
		}
	}

	// Footer: verified real commands only.
	fmt.Fprintf(w, "\nTo see all providers/variants: bestiary list   (or: bestiary list --provider <slug>)\n")
	fmt.Fprintf(w, "To resolve an exact model ID:  bestiary show <raw-id> --format=raw\n")
}
