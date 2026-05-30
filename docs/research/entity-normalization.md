# Entity Normalization Research

Domain research for the entity normalization epoch in `bestiary`. Maps ISO
terminology, IFLA LRM, entity-resolution and prior-art conventions onto the
problem of normalizing 4,168 model entries across 110 providers in models.dev.

References to bestiary types use today's scaffolding: `ModelInfo`, `ModelRef`
(Provider, RawFamily, Family, Variant, Date), `Provider`, `Family`. See
`/home/minttea/codebases/dayvidpham/bestiary/main/modelref.go`.

## 1. ISO Terminology Standards

### 1.1 The object / concept / designation triangle (ISO 704:2022 §5, ISO 1087:2019 §3)

The cornerstone of ISO 704 is a three-level model:

- **Object** — anything perceivable or conceivable. A specific set of model
  weights as it exists on a vendor's servers is an object.
- **Concept** — a unit of knowledge abstracted from one or more objects (ISO
  1087 §3.2.7). "Claude Opus 4.5 of 2025-11-01" is a concept.
- **Designation** — the representation of a concept by a sign (ISO 1087
  §3.4.1). The strings `claude-opus-4-5`, `anthropic.claude-opus-4-5-v1:0`,
  and `us.anthropic.claude-opus-4-5-v1:0` are all designations.

Two specializations of designation matter for us:

- **Term** — designation of a *general concept* (ISO 1087 §3.4.2). Example:
  `claude-opus` as a family name, designating the general concept "any model
  in the Claude Opus tier".
- **Appellation** — a term applied to a group of objects whose relevant
  properties are identical (ISO 1087 §3.4.3). The standard's own examples
  ("Adobe Acrobat X Pro", "Nokia 7 Plus") are exactly the genre of "product
  variant" naming we see in `claude-opus-4-5`.
- **Proper name** — designation of an *individual concept* (ISO 1087 §3.4.4).
  A specific dated revision such as `claude-opus-4-5-20251101` is closer to a
  proper name.

The takeaway: **family** behaves as a *term* (general concept), while a
**dated, versioned model release** behaves as an *appellation* / proper name
(individual concept).

### 1.2 Synonymy, polysemy, homonymy (ISO 704:2022 §7.7, ISO 1087:2019 §3.4.23–§3.4.29)

These give us precise vocabulary for the failure modes in the models.dev data:

| ISO term | Definition | bestiary example |
|---|---|---|
| **Synonymy** | Multiple designations for the *same* concept | `gpt-4o-2024-08-06` ↔ `gpt-4o/2024-08-06` ↔ `openai/gpt-4o-2024-08-06` |
| **Polysemy** | One designation, multiple *related* concepts | `claude-3-5-sonnet` (different snapshots over time, related lineage) |
| **Homonymy** | One designation, multiple *unrelated* concepts | A `claude-opus` ID rehosted by an aggregator that maps it to a different snapshot than Anthropic's |
| **Mononymy/monosemy** | One concept, one designation (the goal) | The canonical reference we are designing |

The 684 shared-ID-across-providers and 191 collision pairs we already
documented are mostly synonymy (intended) plus some homonymy (the danger
case). Normalization aims to reach mononymy/monosemy *within* our internal
canonical reference even though the wild data won't.

### 1.3 Acceptability rating (ISO 704:2022 §7.7.7)

ISO standardizes a three-tier rating that maps directly onto what we need:

- **Preferred designation** — the primary term assigned to a concept.
- **Admitted designation** — a synonym, acceptable but not primary.
- **Deprecated designation** — a synonym that is rejected (still recognized
  for lookup, but never emitted).

This is exactly the shape of an alias table: one canonical form, plus a set
of admitted synonyms used for lookup, plus a deprecated set we recognize but
re-emit as the canonical form.

### 1.4 Canonical form (ISO 1087:2019 §3.8.3)

ISO 1087 names this directly: a **canonical form** (`reference form`,
`base form`) is "a word form chosen according to grammatical conventions to
represent the forms of a paradigm". This is the standards-blessed name for
what we are building: one chosen string per concept, against which all
incoming designations are compared and to which they are normalized
(`lemmatization`, §3.8.5).

### 1.5 Harmonization (ISO 704:2022 §7.7.6, ISO 860 referenced)

ISO 704 describes harmonization as the standardization activity triggered
*when* synonymy/polysemy/homonymy is observed across domains. That is the
exact pipeline step we have been calling "harmonize" — it is not a buzzword;
it has a precise standards meaning.

### 1.6 Metadata-registry frame (ISO/IEC TR 20943-6:2013, leaning on ISO/IEC 11179-3)

ISO/IEC 11179-3 (referenced throughout TR 20943-6) gives us a generic
metamodel for metadata registries. The relevant pieces:

- **Concept System** — a namespace; e.g., the `bestiary` model registry.
- **Object Class** — `Model` (the thing being registered).
- **Property** — fields like `Family`, `Variant`, `ReleaseDate`, `Provider`,
  `Modalities`.
- **Conceptual Domain / Value Meanings** — the controlled vocabulary that
  Property values are drawn from (e.g., the closed set of canonical
  `Family` values).

This vocabulary is useful to import: bestiary's `Provider`, `Family`,
`Modality` enums are *conceptual domains* in ISO/IEC 11179 terms; the
forthcoming canonical `Variant` set is another. Code generation (which we
already do) is the natural materialization of a controlled vocabulary.

## 2. IFLA LRM Mapping

### 2.1 The four primary entities

IFLA LRM (2017, ratified replacement for FRBR) defines a four-level WEMI
hierarchy plus the supertype `Res` ("thing") and supporting entities `Agent`,
`Nomen`, `Place`, `Time-span`.

| LRM entity | Library example | AI model analogue |
|---|---|---|
| **Work** | "Hamlet" the play | The abstract trained-model identity: "Claude Opus 4.5" |
| **Expression** | Hamlet in modern English | A specific dated revision: "Claude Opus 4.5 as of 2025-11-01" |
| **Manifestation** | Penguin 2003 hardcover edition | A provider's hosting: that revision served on Anthropic's API |
| **Item** | The specific physical book on my shelf | A specific deployed endpoint, slug, or model-ID string |

This maps cleanly onto bestiary's domain. Same weights served by Anthropic,
Bedrock, and Vertex AI are three Manifestations of one Expression of one Work.
The string `anthropic/claude-opus-4-5-20251101` and the alias
`bedrock/us.anthropic.claude-opus-4-5-v1:0` are two Items that happen to be
two Manifestations.

### 2.2 The Nomen entity

IFLA LRM also introduces **Nomen**: an explicit entity representing "any
designation of any thing". It absorbs FRAD's *name* and *identifier* and
FRSAD's *controlled access point*. Nomen has its own attributes (scheme,
language, status) and its own relationships to the entity it designates.

This is the LRM equivalent of ISO 1087's *designation*, and it is the
strongest single hint for our data model: **identifiers are first-class
entities, not string fields on the model entity.** The same Work has many
Nomens (one preferred, several admitted, several deprecated), each with
provenance.

### 2.3 Mapping to ModelRef

The current `ModelRef{Provider, RawFamily, Family, Variant, Date}` blends
LRM levels. A clean separation:

- **Work identity** = `(Family, Variant)` — the abstract Opus / Sonnet / Haiku
  tier of Claude. Provider-independent, date-independent.
- **Expression identity** = `(Family, Variant, Date)` — a specific dated
  release of the trained weights. Provider-independent.
- **Manifestation identity** = `(Provider, Family, Variant, Date)` — a
  particular hosting of an Expression.
- **Item identity** = `ModelID` (the slug used in the API call) — what an
  end-user actually puts in `model=`.
- **Nomen** = any designation that resolves to a Manifestation or
  Expression: `RawFamily`, the API `id`, aliases discovered during
  normalization, and the canonical form we mint.

The current 5-tuple is essentially a flattened (Manifestation, RawFamily)
record. Restructuring to surface the Work / Expression / Manifestation levels
gives us the cross-provider identity the user explicitly asked for.

### 2.4 Real-world adoption and critiques

- **BIBFRAME 2** (Library of Congress) is the LD/RDF realization of LRM.
- **RDA Toolkit** is the cataloguing rules that operationalize LRM.
- **DCMI OpenWEMI** (2024) is a deliberately *minimally-constrained*
  re-statement of WEMI for the wider digital-resource community: it relaxes
  LRM's library-specific definitions so that WEMI can be applied to digital
  artifacts (datasets, software, models) without needing the rest of the LRM
  ontology. This is the closest precedent for what bestiary needs and worth
  citing as direct prior art.
- The standing critique of LRM for digital-only entities is that
  Manifestation/Item collapse: there is no "physical copy" distinct from "the
  digital file". For us this is fine — Item maps to "the model-ID slug as a
  string", which is genuinely distinct from Manifestation = "the
  provider-hosted endpoint configuration".

## 3. Entity Resolution Techniques

### 3.1 Probabilistic record linkage (Fellegi-Sunter, 1969)

The classic statistical foundation: compare two records field-by-field,
compute per-field `m` (agreement | match) and `u` (agreement | non-match)
probabilities, sum log-likelihood ratios, threshold. Implemented in
**Splink** (UK MoJ) and **Dedupe.io** (active learning). Splink scales to
millions of records.

Relevance to bestiary: **probably overkill.** We have 4,168 records, mostly
deterministic structure (provider prefix, family slug, date suffix). A small
rule-based normalizer plus a manual override table will dominate a
probabilistic matcher. But the framework is useful as a fallback for the
long tail (e.g., aggregator slugs that don't follow obvious patterns) and
for measuring the quality of our deterministic rules — Splink-style m/u
weights can score residual ambiguity.

### 3.2 Deterministic / rule-based linkage

For high-structure, high-coverage domains — which ours is — the standard
move is:

1. **Block** records by a deterministic key (here: candidate Family).
2. **Tokenize** within the block (split on `-`, `_`, `/`, `.`, `:`).
3. **Apply rules** (provider-prefix strip, version suffix extract, date
   pattern recognize).
4. **Manually override** the residual that rules can't crack.

This is what LiteLLM's normalization layer effectively does (see §4).

### 3.3 Knowledge-graph entity resolution

Wikidata's `owl:sameAs` and `schema:sameAs` are the canonical way to
publish "these two URIs designate the same thing". The pattern is to mint
one canonical URI per Work/Expression and link the alternative designations
to it via `sameAs`. For an internal Go API this maps to: one canonical Go
identifier per Work, with the alias table as data — never as a parallel
type.

## 4. Prior Art in AI Model Registries

### 4.1 Hugging Face Hub

The format is `{org}/{repo}` (e.g., `EleutherAI/gpt-neo-1.3B`), with an
optional **revision** (branch name, tag, or git commit hash) to pin a
specific version. The pinning model is git-based; revisions are immutable
and content-addressable. There is no separation between Work and Expression
— the repo is the Work, revisions are Expressions, and there is exactly one
Manifestation (HF Hub itself).

**Lesson**: Identifier = `namespace/name` plus optional pinned revision. The
two-level structure (org, name) is structurally identical to what PURL uses.

### 4.2 LiteLLM

The format is `{provider}/{model-name}`, e.g.,
`anthropic/claude-3-5-sonnet-20241022`,
`vertex_ai/gemini-1.5-pro`,
`bedrock/us.anthropic.claude-3-7-sonnet-20250219-v1:0`.

LiteLLM normalizes *responses* (everything to OpenAI ChatCompletion shape)
but does **not** normalize *identities* — `bedrock/us.anthropic.claude-...`
is treated as its own model id, separate from `anthropic/claude-...`, even
when the underlying weights are identical. So users still have to know which
hosting they want.

**Lesson**: A flat `provider/model` string is the operational lingua franca,
but it does not by itself express cross-provider identity.

### 4.3 OpenRouter

Same `provider/model-name` format (`anthropic/claude-opus-4`,
`google/gemini-2.5-pro-preview`). OpenRouter does add cross-provider
abstraction by routing the same model id to whichever provider endpoint is
healthiest — but this is implicit, the user never sees the
Manifestation-level choice.

**Lesson**: User-facing interfaces tend to expose Work-level (or Expression-
level) names and hide Manifestation. Internally there must still be a
Manifestation table.

### 4.4 Stanford CRFM / HELM

HELM uses `{org}/{model}` model identifiers internally (e.g.,
`openai/gpt2`). No formal canonical-identifier registry. Models are
configured per-run.

### 4.5 Model Cards (Mitchell et al. 2018, schema.org Bib)

Model Cards are the closest thing to LRM Manifestation-level metadata:
each card describes one model version with version history. Schema.org's
Bib extension has a generic `Identifier` type with (value, type, issuing
authority) — directly importable as the shape of a Nomen entity.

## 5. Identifier Schemes

### 5.1 PURL (Package URL, ECMA-427:2025)

`pkg:type/namespace/name@version?qualifiers#subpath`

The `huggingface` PURL type is registered (see PR #201 on
package-url/purl-spec). Examples in the wild:

- `pkg:huggingface/EleutherAI/gpt-neo-1.3B@797174552AE...`
- `pkg:huggingface/hauson-fan/RagRetriever@4404c42...?model_format=pytorch`

PURL is now an ECMA standard and is used in CycloneDX SBOMs and CVE records.

**Match to our problem**:

| PURL slot | bestiary mapping |
|---|---|
| `type` | constant `bestiary` (or follow ML lead: `huggingface`, `bedrock`, etc., per provider) |
| `namespace` | `Provider` |
| `name` | `Family-Variant` (the appellation) |
| `version` | `Date` (or release tag) |
| `qualifiers` | hosting metadata — context window, region, fine-tune slot |

### 5.2 URN / IETF

URN format `urn:nid:nss` is too generic to add value over PURL for our use
case. PURL effectively *is* a URN profile for software packages.

### 5.3 DOI

DOI gives institutional registration, persistence, and cite-ability — but
requires DOI registration for every model release, which only large
publishers do (arXiv now mints DOIs automatically per paper).

### 5.4 arXiv versioning convention

`arXiv:2401.12345v3` — base identifier plus monotonic version suffix. The
unversioned form `arXiv:2401.12345` resolves to the latest. This is a useful
pattern: the *Work-level* canonical reference resolves to "current latest
Expression"; the *Expression-level* reference pins.

### 5.5 Recommendation matrix

| Use case | Recommended scheme |
|---|---|
| External / interop output | PURL (`pkg:bestiary/...` or reuse `pkg:huggingface/...` when origin is HF) |
| Internal Go API | A typed struct, not a string — see §6 |
| User-facing CLI flag | `provider/family-variant[@date]` (LiteLLM/OpenRouter shape) |
| Wire format / JSON | Both: a flat string `id` plus a structured `ref` object |

## 6. Recommended Approach for bestiary

### 6.1 Adopt a four-level type structure (LRM-inspired, ISO-vocabulary-grounded)

```go
// Work — provider-independent, date-independent abstract identity.
// Equivalent to LRM Work, ISO 1087 general concept (term).
type Work struct {
    Family  Family   // controlled vocabulary; e.g. FamilyClaude
    Variant Variant  // controlled vocabulary; e.g. VariantOpus
}

// Expression — a specific dated release of a Work.
// Equivalent to LRM Expression, ISO 1087 individual concept (proper name).
type Expression struct {
    Work
    Date string // ISO 8601 date or empty if undated
}

// Manifestation — an Expression as hosted by a particular Provider.
// Equivalent to LRM Manifestation. This is the unit ModelInfo describes today.
type Manifestation struct {
    Expression
    Provider Provider
}

// Nomen — any designation that resolves to one of the above.
// Carries provenance and acceptability rating (ISO 1087 §3.4.18).
type Nomen struct {
    Value       string             // the literal string, e.g. "us.anthropic.claude-opus-4-5-v1:0"
    Scheme      NomenScheme        // RawAPIID, ProviderSlug, BedrockARN, Canonical, ...
    Status      AcceptabilityRating // Preferred, Admitted, Deprecated, Obsolete
    ResolvesTo  ManifestationRef   // foreign key into the Manifestation table
}
```

Today's `ModelRef` becomes `Manifestation` (with `RawFamily` demoted to a
Nomen of scheme `RawAPIID`). Today's `ProvidersForFamily` becomes
`Manifestations(Work{Family: FamilyClaude, Variant: VariantOpus})`.

### 6.2 Adopt ISO acceptability ratings explicitly

```go
type AcceptabilityRating int

const (
    Preferred  AcceptabilityRating = iota // ISO 1087 §3.4.19
    Admitted                              // ISO 1087 §3.4.20
    Deprecated                            // ISO 1087 §3.4.21
    Obsolete                              // ISO 1087 §3.4.22
)
```

Lookups accept any rating; emission only ever produces `Preferred`. This is
the established pattern from terminology standardization and gives us
free guidance for behavior.

### 6.3 Pipeline (cache → explore → normalize → harmonize)

1. **Cache** the raw models.dev JSON (already planned in UAT-1). Acts as
   the immutable upstream Object record.
2. **Explore** — run analysis to discover candidate (Family, Variant) pairs
   from the conflated `RawFamily`. 25% of records have empty family — these
   need rule-based extraction from `id`.
3. **Normalize** — apply rules + manual overrides to assign each record a
   `(Work, Date, Provider)` triple. Emit Nomens for every encountered
   designation, marked with provenance scheme.
4. **Harmonize** (ISO 704 §7.7.6) — within and across providers, deduplicate
   Nomens that point to the same Manifestation; choose the Preferred per
   concept; flag residual collisions for manual review.

### 6.4 Canonical reference scheme

Mint one Preferred Nomen per Manifestation in the LiteLLM/OpenRouter shape:

```
{provider}/{family}-{variant}[-{date}]
anthropic/claude-opus-4-5-20251101
bedrock/claude-opus-4-5-20251101
```

For PURL interop, also expose:

```
pkg:bestiary/anthropic/claude-opus-4-5@20251101
```

For the user's stated goal of cross-provider identity, expose Work and
Expression resolvers:

```go
func (b *Registry) Work(family Family, variant Variant) []Manifestation
func (b *Registry) Expression(family Family, variant Variant, date string) []Manifestation
func (b *Registry) Resolve(designation string) (Manifestation, AcceptabilityRating, error)
```

`Resolve` takes any Nomen — raw API id, Bedrock ARN, alias, or canonical
form — and returns the Manifestation plus the rating of the input
designation. This is the runtime normalizer CLI from item #4 in
bestiary-n6x.

### 6.5 What to *not* do

- **Do not** model Work as a Go interface with provider-specific
  implementations. The variant explosion the user warns about kicks in
  immediately — every new provider becomes a new type. Work is data, not
  code.
- **Do not** invent a new URN scheme. Reuse PURL with the `huggingface`
  type for HF-origin models and a new (proposed) `bestiary` type for
  cross-provider canonical references.
- **Do not** start with probabilistic linkage. Deterministic rules + manual
  overrides will cover >95% of 4,168 records; reserve probabilistic
  scoring for measuring the residual.
- **Do not** treat `RawFamily` as a permanent field on Manifestation.
  Demote it to a Nomen with scheme `RawAPIID` so its provenance survives
  but it stops competing with the canonical Family.

### 6.6 Adoption order

1. Define types: `Work`, `Expression`, `Manifestation`, `Nomen`,
   `AcceptabilityRating`. (Compatible with current ModelRef as an alias for
   Manifestation during migration.)
2. Define controlled vocabularies for `Variant` — the second axis after
   `Family`. Codegen from a curated YAML, not from the API.
3. Build `Resolve` with deterministic rules first. Test corpus = the 191
   collision pairs.
4. Emit Preferred Nomens for canonical output. Keep raw nomens in the cache
   layer for round-tripping.
5. Add a sync-time auditor that flags Manifestations whose
   `(Work, Date, Provider)` triple was already taken — this catches
   homonymy as the upstream dataset grows.

## Sources

- ISO 704:2022 — Terminology work: principles and methods (§5 concepts,
  §7.7 designation–concept relations, §7.7.6 harmonization, §7.7.7
  acceptability rating).
- ISO 1087:2019 — Terminology work: vocabulary (§3.2.7 concept, §3.4
  designations, §3.8.3 canonical form, §3.8.4 disambiguation).
- ISO/IEC TR 20943-6:2013 — Procedures for achieving metadata registry
  content consistency (§5 framework; references ISO/IEC 11179-3 metamodel).
- IFLA Library Reference Model (2017): https://www.ifla.org/publications/ifla-library-reference-model/
- DCMI OpenWEMI: https://www.dublincore.org/blog/2024/announcing-openwemi/
- PURL spec (ECMA-427:2025): https://github.com/package-url/purl-spec
- PURL `huggingface` type PR #201:
  https://github.com/package-url/purl-spec/pull/201
- Splink (probabilistic record linkage):
  https://moj-analytical-services.github.io/splink/
- Fellegi & Sunter, "A Theory For Record Linkage" (1969).
- Hugging Face Hub model identifiers + revisions:
  https://huggingface.co/docs/hub/index
- LiteLLM provider routing: https://docs.litellm.ai/docs/providers
- OpenRouter model id format: https://openrouter.ai/docs
- Stanford CRFM HELM: https://crfm.stanford.edu/helm/
- Wikidata data model + canonical-identifier discussion:
  https://www.wikidata.org/wiki/Wikidata:Data_model
- arXiv identifier scheme: https://info.arxiv.org/help/arxiv_identifier.html
- Schema.org Bib + Identifier:
  https://www.w3.org/community/schemabibex/wiki/Identifier
