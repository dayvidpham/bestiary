package bestiary

import "fmt"

// ModelRef represents the canonical identity of a model.
//
// The tuple (ID, Provider, RawFamily, Family, Variant, Version, ParamSize, Date,
// Modifier) is the stable anchor for cross-provider queries, canonical formatting,
// and the normalization pipeline. ID is the original API model identifier (e.g.
// "claude-opus-4-20250514"). Family, Variant, Version, ParamSize, and Modifier are
// populated at codegen time by the normalization pipeline in cmd/bestiary-gen.
//
// ParamSize is part of the canonical entity identity (mirroring EntityRef): a 70B
// and an 8B of one family are distinct models, so the size participates in
// identity-level bucketing (e.g. Resolve's ambiguity grouping). It is empty when the
// model's size is unknown. Host is a per-instance attribute and is NEVER part of
// identity.
type ModelRef struct {
	ID        ModelID  // Original API model ID (e.g. "claude-opus-4-20250514")
	Provider  Provider // Hosting provider
	RawFamily Family   // API family field verbatim (e.g., "claude-opus")
	Family    Family   // Canonical family (e.g., "claude"); empty if not yet normalized
	Variant   string   // Canonical variant (e.g., "opus"); empty if no variant
	Version   string   // Model version extracted from family (e.g., "4.5", "2.5"); empty if none
	ParamSize string   // Canonical parameter-size token (e.g. "70b", "8b"); empty when unknown. Part of identity.
	Date      string   // Release date in YYYY-MM-DD format; empty if none
	Modifier  []string // Known trailing tokens in canonical order (e.g., ["vision","instruct"]); nil if none
	Host      Host     // Serving host/backend (per-instance attribute, never part of identity); HostNone if unknown
	// Region is the serving jurisdiction / AWS Bedrock region (per-instance attribute,
	// never part of identity; RegionNone if unspecified). RegionRaw is the fail-safe
	// carrier for RegionOther. Both are json:"-" for now: the field exists (the parse
	// half), but exposing Region on the public JSON contract is a schema change deferred
	// to the schema-0.5.0 bump — until then it is excluded from serialization so it
	// causes no schema drift, exactly as it is excluded from all key rendering.
	Region    Region `json:"-"`
	RegionRaw string `json:"-"`
}

// Ref returns a ModelRef for this ModelInfo.
// Every field is populated: ID from the API model ID, RawFamily from the raw API
// family field, and Family, Variant, Version, ParamSize, Date, Modifier, Host from
// the codegen-baked normalization. ParamSize carries the canonical parameter-size
// token so identity-level consumers (e.g. Resolve's ambiguity grouping) can keep
// distinct sizes of one family apart.
func (m ModelInfo) Ref() ModelRef {
	return ModelRef{
		ID:        m.ID,
		Provider:  m.Provider,
		RawFamily: m.RawFamily,
		Family:    m.Family,
		Variant:   m.Variant,
		Version:   m.Version,
		ParamSize: m.ParamSize,
		Date:      m.Date,
		Modifier:  m.Modifier,
		Host:      m.Host,
	}
}

// Format serializes the ModelRef according to the given CanonicalScheme.
//
//   - SchemeCanonical: "<provider>/<family>/<variant>@<date>" — the variant
//     segment is included only when non-empty; the "@<date>" suffix is included
//     only when date is non-empty. Falls back to "<provider>/<raw-id>" when both
//     Family and Variant are empty (e.g., provider-specific representation).
//   - SchemeHuggingFace: "<provider>/<raw-id>" (HuggingFace Hub form).
//   - SchemePURL: "pkg:huggingface/<provider>/<raw-id>" (purl-spec + ECMA-427).
//   - SchemeRaw: string(r.ID) — the original API model identifier verbatim.
func (r ModelRef) Format(s CanonicalScheme) string {
	switch s {
	case SchemeCanonical:
		return r.formatCanonical()
	case SchemeHuggingFace:
		return fmt.Sprintf("%s/%s", r.Provider, r.ID)
	case SchemePURL:
		return fmt.Sprintf("pkg:huggingface/%s/%s", r.Provider, r.ID)
	case SchemeRaw:
		return string(r.ID)
	default:
		// Unrecognized scheme: fall back to raw ID for safety.
		return string(r.ID)
	}
}

// formatCanonical produces the SchemeCanonical string.
//
// When Family is populated the form is built from the non-empty segments:
//
//	<provider>/<family>[/<variant>][/<version>][@<date>]{identity-mods}[attributes]
//
// Segment inclusion rules:
//   - Family empty: fall back to "<provider>/<raw-id>"
//   - Variant only appended when non-empty
//   - Version only appended when non-empty (requires Variant to precede it, or
//     placed directly after Family when Variant is empty)
//   - Date only appended as "@<date>" suffix when non-empty
//   - Modifiers rendered class-aware: IDENTITY-class tokens in a "{identity-mods}"
//     segment, ATTRIBUTE-class tokens in an "[attributes]" segment, in that order,
//     after the date suffix (if any). Each token is routed by
//     ClassifyModifier(token, family); within a segment tokens are de-duplicated
//     and comma-joined in CanonicalizeModifiers order.
//
// Backward-compatibility: ONLY identity modifiers moved out of the legacy "[mod]"
// bracket into "{mod}"; attribute modifiers stay in "[mod]". A render whose
// modifiers are ALL attribute-class is therefore BYTE-IDENTICAL to the pre-class
// canonical form. Classification depends on the embedded table; if it fails to
// load, every token degrades to IDENTITY (never a panic).
//
// Full example matrix (p = provider, f = family, v = variant, ver = version,
// d = date, i = identity-mod, a = attribute-mod):
//
//	(f)                          → p/f
//	(f,d)                        → p/f@d
//	(f,v)                        → p/f/v
//	(f,v,d)                      → p/f/v@d
//	(f,ver)                      → p/f/ver
//	(f,ver,d)                    → p/f/ver@d
//	(f,v,ver)                    → p/f/v/ver
//	(f,v,ver,d)                  → p/f/v/ver@d
//	(f,v,ver,d,a)                → p/f/v/ver@d[a]
//	(f,v,ver,i)                  → p/f/v/ver{i}
//	(f,v,ver,d,i,a)              → p/f/v/ver@d{i}[a]
func (r ModelRef) formatCanonical() string {
	if r.Family == "" {
		// Fall back to provider-specific representation.
		return fmt.Sprintf("%s/%s", r.Provider, r.ID)
	}

	// Build path segments after family.
	// Variant (if any) comes first, then Version (if any).
	path := string(r.Family)
	if r.Variant != "" {
		path += "/" + r.Variant
	}
	if r.Version != "" {
		path += "/" + r.Version
	}

	var base string
	if r.Date != "" {
		base = fmt.Sprintf("%s/%s@%s", r.Provider, path, r.Date)
	} else {
		base = fmt.Sprintf("%s/%s", r.Provider, path)
	}

	// Route modifiers by class: identity-mods into "{...}", attribute-mods into
	// "[...]", in that order. Attribute-only renders stay byte-identical to the
	// legacy single-bracket form.
	if idKey := modifierKey(EntityModifiers(r.Modifier, r.Family)); idKey != "" {
		base += "{" + idKey + "}"
	}
	if attrKey := modifierKey(attributeModifiers(r.Modifier, r.Family)); attrKey != "" {
		base += "[" + attrKey + "]"
	}
	return base
}

// String implements fmt.Stringer.
// It returns Format(SchemeCanonical), the canonical slash-separated form.
func (r ModelRef) String() string {
	return r.Format(SchemeCanonical)
}

// Designations returns all string designations for this ModelRef.
//
// Acceptability is now ACTIVATED (this is the epoch that promotes it): the
// SchemeCanonical designation carries AcceptabilityPreferred — the canonical
// decomposed key IS the preferred designation of the identity — while the raw ID,
// HuggingFace, and PURL forms stay AcceptabilityAdmitted (recognized, usable, not
// preferred). This is the READ-PROJECTION half of the naming layer: a Designation is
// the ref-level rendering of the same fact a Nomen records at the entity level, so
// the SchemeCanonical designation's Preferred rating agrees by construction with the
// NomenSchemeCanonical nomen's AcceptabilityPreferred status (the Designations↔Nomen
// consistency fence: shared schemes agree on rating).
//
// The returned slice contains:
//  1. A SchemeRaw designation using the original API model ID (Admitted).
//  2. A SchemeCanonical designation, the canonical slash-separated form (Preferred).
//  3. A SchemeHuggingFace designation (Admitted).
//  4. A SchemePURL designation (Admitted).
func (r ModelRef) Designations() []Designation {
	return []Designation{
		{
			Value:    r.Format(SchemeRaw),
			Scheme:   SchemeRaw,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
		{
			Value:    r.Format(SchemeCanonical),
			Scheme:   SchemeCanonical,
			Provider: r.Provider,
			Rating:   AcceptabilityPreferred,
		},
		{
			Value:    r.Format(SchemeHuggingFace),
			Scheme:   SchemeHuggingFace,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
		{
			Value:    r.Format(SchemePURL),
			Scheme:   SchemePURL,
			Provider: r.Provider,
			Rating:   AcceptabilityAdmitted,
		},
	}
}

// ProvidersForFamily returns the set of providers that host models with
// the given raw API family string (e.g., "claude-opus", "gemini-flash").
// The family parameter matches the RawFamily field (verbatim API value).
// The returned slice contains no duplicates. If no models match, a nil slice
// is returned.
func ProvidersForFamily(family Family) []Provider {
	seen := make(map[Provider]struct{})
	var out []Provider
	for _, m := range staticModels {
		if m.RawFamily == family {
			if _, ok := seen[m.Provider]; !ok {
				seen[m.Provider] = struct{}{}
				out = append(out, m.Provider)
			}
		}
	}
	return out
}
