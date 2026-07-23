# Concepts

The vocabulary bestiary is built on. Every term here names one precise thing;
the code, the docs, and the beads use them consistently. The ontology is
grounded in **ISO 1087** (terminology work — concepts vs. their designations)
and **IFLA LRM / LRMoo** (the library world's reference model and its
CIDOC-CRM harmonization); see [Grounding](#grounding) for the mapping and
[`research/entity-normalization.md`](research/entity-normalization.md) for the
full derivation.

```
                      the THING                        NAMES for the thing
                ┌───────────────────┐            ┌──────────────────────────┐
                │      Entity       │◄───────────│         Nomen            │
                │ (one model,       │ resolves-to│ (one recorded spelling,  │
                │  one identity)    │            │  with provenance)        │
                └───────┬───────────┘            └──────────────────────────┘
                        │ has-many                    many per entity
                ┌───────▼───────────┐
                │ ProviderInstance  │  one (provider, host, region) serving,
                │ (an offering)     │  with its own price/limits/quants
                └───────────────────┘
```

## The identity layer

### Entity

**One model identity**: same weights, same architecture — however many
providers serve it, under however many spellings. The unit of cross-provider
comparison. An entity is addressed by its **canonical key**:

```
family[/variant][@version][#paramsize]{identity-mods}

llama/scout@4#17b-16e{instruct}
```

Segments that are identity: the family (the model line), the variant (a named
member of the line), the version (distinct from a release *date*), the
parameter size (a 70B and an 8B are different models), and identity-class
modifiers (tokens naming genuinely different artifacts, like `instruct` or
`omni`). Segments that are **not** identity: serving tiers (`fast`,
`realtime`), release stages (`preview`, `beta`), quantization, provider, host,
region — those are per-instance facts.

Versions normalize **dotted-canonical at identity level**: where a family
attests both `4` and `4.0` for the same tuple, they are one entity keyed
`@4.0`, and a bare-version expression resolves to it.

### ProviderInstance

One concrete **offering** of an entity: a `(provider, host, region)` serving
with its own raw ID, pricing, context limits, and quantization rows. Many
instances roll up into one entity; the instance list is the queryable evidence
of the spelling unification. `ModelInfo` is the same granularity in its flat
catalog form (what `list` prints and the store persists).

### Provider · Host · Region

Three orthogonal instance-level axes:

| Axis | Question it answers | Example |
|---|---|---|
| `Provider` | who makes the model available to you (API **or** weight distribution) | `anthropic`, `groq`, `huggingface` |
| `Host` | which upstream backend a reseller routes to | NanoGPT's `azure-*` IDs → `Host: azure` |
| `Region` | which geographic/jurisdictional boundary serves the request | Bedrock's `eu.` profiles → `Region: eu` |

None of the three is identity. A **creator** axis (who trained the weights)
does not yet exist as a typed dimension — it is implicit in the family and the
lab-scoped metadata ID; making it first-class is tracked upstream (GH#26).

### Series and Release

A computed, read-only hierarchy **above** entity keys — never fed back into
them: `Series{Family, Generation}` is the versioned line (`llama-4`,
`claude-4.5`), `Release{Series, Name}` a named member of it (`scout`,
`maverick`). Version sits above variant, per the ratified mental model
"the llama-4 series → scout/maverick releases". Because the relations are
computed, the hierarchy can be reshaped without re-keying anything.

## The naming layer

### Appellation

The terminology-theory concept (ISO 1087; CIDOC CRM's `E41 Appellation`): a
name-as-such — any sign by which a thing is called. Not a data type in
bestiary; the *idea* the data types capture. What an appellation carries in
ISO 1087 is an **acceptability rating**: `Preferred`, `Admitted`,
`Deprecated`, `Obsolete` — bestiary's `AcceptabilityRating`.

### Nomen

The **recorded fact** that a spelling names an entity — the name-to-thing
association reified as data, so it can carry provenance:

```go
Nomen{
    Value      string              // the literal spelling
    Scheme     NomenScheme         // what KIND of name (canonical/provider-id/huggingface/purl/alias)
    Status     AcceptabilityRating // how sanctioned (Preferred/Admitted/...)
    ResolvesTo EntityRef           // the entity it names
    SourceURL  string              // WHO asserts the naming (an archive.org snapshot, by policy)
    Source     DataSourceID        // WHICH ingest we read it from (models.dev / curated / ...)
}
```

One entity carries many nomina: exactly one `Preferred` canonical nomen (its
key), every raw provider spelling as an `Admitted` provider-ID nomen, plus
curated aliases and HuggingFace repo names. **Homonymy is representable**: one
spelling resolving to several entities is simply several nomina sharing a
`Value`, and `NomenLookup` returns them all.

Two provenance levels never share a field: `SourceURL` is *who says so* (the
claimant — xAI's docs, a HuggingFace repo page), `Source` is *where we read
it* (the ingest). Curated claims cite archive.org snapshots so the evidence
cannot rot.

The load-bearing consequence: **repairs to identity never erase the record**.
When two entities merge, or a spelling is re-keyed, the old spellings survive
as Admitted nomina pointing at the surviving entity — the count of provider-ID
nomina is invariant under every identity repair.

### Canonicalized expression

The output of a **pure function** — `EntityRef.String()`,
`ModelRef.Format(scheme)`. A deterministic serialization of an identity into a
grammar: total (every ref renders), computed on demand, carrying no claim and
no provenance. The bridge to the naming layer: at mint time each entity's
canonical expression is enrolled as its one `Preferred` canonical nomen.

### Designation

The ISO-flavored **read projection** (`designation.go`): value + scheme +
rating computed from a `ModelRef`, no storage, no attribution. Historically
the first appellation-shaped type; kept consistent with the Nomen layer by
fence (the canonical designation is `Preferred`, others `Admitted`).

### Suppression

A **naming-status policy, never a key change**: a curated seed entry marks a
modifier redundant for an entity, making the spelling *with* the modifier an
`Admitted` nomen while the `Preferred` value omits it. Fully reversible. Ships
with an **empty seed**; its collision guard rejects any entry whose
"suppressed" spelling would collide with a genuinely distinct entity — which
is how the first attempted entries were correctly routed to attribute-class
demotion (a merge) instead.

## Attestation quality (v0.2.8 extension, beyond LRM)

Multi-attestation (the shift from one `SourceURL`/`Source` pair per Nomen to a
Nomen `HAS-MANY` `NomenAttestation`s) adds a second axis alongside `Status`:
**attestation quality** — whose voice a piece of evidence is, and how it
entered the system. This section documents that axis and the specs it was
checked against; see the v0.2.8 registry-ingest/creator-dimension proposal §3.1
for the shipping types
(`AttestationAuthority`, `IngestMethod`).

Where `Status` (`AcceptabilityRating`) is bestiary's **one** editorial judgment
about a Nomen as a whole — the correction to the Grounding table above is
important here: **LRM-E9 Nomen's own attribute list carries no dedicated
"status" attribute at all.** IFLA LRM (2017-12), Table 4.3, lists Nomen's
attributes as Category, Nomen string, Scheme, Intended audience, Context of
use, Reference source, Language, and Script (+ Script conversion) — nine
attributes, none named "status". LRM represents "preferred" through the
general-purpose **Category** attribute, sub-typed per application (§4.6.3's
worked example literally tags one clustered nomen `Category (preferred form of
access point)` and its siblings as variants). `AcceptabilityRating`'s
four-value scheme (`Preferred`/`Admitted`/`Deprecated`/`Obsolete`) is therefore,
as this document's opening paragraph already states, grounded in **ISO 1087**
— it is not a literal LRM field, and the Grounding table's shorthand ("nomen
`status` attribute") should be read as an analogy to LRM's Category-as-preferred
usage, not a citation of an attribute that exists in the spec.

`AttestationAuthority` (`Primary`/`Secondary`, per-attestation — whose voice
the evidence document is) and `IngestMethod` (`Curated`/`Harvested`/
`SelfMinted`, per-attestation — how the record entered bestiary) go further
than LRM does anywhere: LRM has no notion of grading the reliability or
provenance-class of an attestation at all. The nearest **formal** cousins are
in CIDOC CRM's extension family, not LRM proper:

- **CIDOC CRM `E13 Attribute Assignment`** (CIDOC CRM 7.1.2) — "comprises
  actions of making assertions about one property of an object" (via `P140
  assigned attribute to`, `P141 assigned`, `P177 assigned property of type`).
  A `NomenAttestation` is one instance of this shape: an ingest event
  asserting a property (a naming) of a given type onto an entity.
- **CRMinf** (v1.2, April 2025 — CIDOC CRM's argumentation extension) —
  `I7 Belief Adoption`: *"the action of an E39 Actor adopting [...] propositions
  taken from an interpretation of [...] an [E73] Information Object as being
  true [...] The basis of I7 Belief Adoption is the justification of trust in
  the source of the adopted propositions, rather than the application of
  rules for inferring the respective propositions from logical premises."*
  That is bestiary's ingest posture exactly: a harvested or curated nomen is
  adopted on trust in its source, not derived by inference. `I2 Belief` ("the
  [...] Proposition Set is to have a particular `I6 Belief Value` [...] held by
  a particular Actor") and `I6 Belief Value` (the True/False/Unknown value
  itself) are the surrounding machinery `I7 Belief Adoption` concludes into.
  (Trained-knowledge correction made during this cite-verify pass: the belief
  adoption class is **`I7`**, not `I2` — `I2` is `Belief` itself.)

**Historiographic grounding for Primary/Secondary**: this is source
criticism's standard split — a primary source speaks in its own voice for
itself (a namespace owner's own docs; a Hub repo attesting its own name), a
secondary source relays or aggregates another's claim (an aggregator's
catalog row). **Wikidata's statement-rank model** is the closest production
system to bestiary's shape, useful for calibration rather than as a literal
precedent: each statement carries one rank — `Preferred` ("assigned to the
[...] statement(s) that best represent consensus"), `Normal` (the default,
"no judgement [...] of a value's accuracy"), `Deprecated` ("known to include
errors [...] or [...] outdated knowledge") — attached to the *statement*,
while references attach separately and severally: *"if there are many
references for a claim, each of them makes the claim independently of the
others."* That split mirrors bestiary's: `Status` is the one-per-Nomen
rank-like judgment (Wikidata-rank analogue); `AttestationAuthority` /
`IngestMethod` / `SourceURL` / `Source` live on each independent attestation
(Wikidata-reference analogue) — kept apart because a statement's standing and
an individual reference's provenance answer different questions.

None of `E13`, `I7 Belief Adoption`, or Wikidata ranks is a literal precedent
for `AttestationAuthority` or `IngestMethod` as named types — both are
bestiary-authored vocabulary, sized to its own ingest reality. What the specs
establish is that the **shape** — one editorial judgment per naming fact, many
independently-standing attestations under it, each stamped with whose voice
it is — is a well-trodden pattern across library authority control,
CIDOC-CRM's argumentation extension, and Wikidata, not an ad hoc invention.

**Sources checked directly against their primary texts for this section**
(not from trained recall — see the v0.2.8 ledger-deliverables §9 cite-verify
flag):
IFLA LRM (2017-12 consolidated edition, `ifla-lrm-august-2017_rev201712.pdf`),
§4.2 Table 4.3 (Nomen attribute list) and §4.6.3 (Category-as-preferred
example); CIDOC CRM 7.1.2, class `E13 Attribute Assignment` and properties
`P140`/`P141`/`P177`; CRMinf v1.2 (April 2025), class declarations for `I2
Belief`, `I6 Belief Value`, `I7 Belief Adoption`; Wikidata `Help:Ranking`
(statement rank definitions, current as of this writing).

## External identifiers

| Identifier | Role | Scope |
|---|---|---|
| **IRI** (`EntityRef.IRI(base)`) | the entity's node name in a knowledge graph — RFC 3987, percent-encoded canonical key under a caller-supplied namespace | total: every entity |
| **purl** (`Format(SchemePURL)`) | a foreign key into a package registry, for SBOM/supply-chain tooling | only where the artifact's registry home is known (today: HuggingFace-hosted rows) |
| **HuggingFace nomen** | the entity-level Hub `org/repo` name, provider-independent | curated seeds today; registry ingest planned (GH#25) |

The division of labor: an IRI asserts nothing about retrievability (every
entity has one); a purl is only honest when the weights genuinely live in a
registry (an empty purl beats a spec-invalid one).

## Grounding

| bestiary | ISO 1087 | IFLA LRM / LRMoo | CIDOC CRM |
|---|---|---|---|
| Entity | concept | Res (any thing); WEMI levels collapse to Expression/Manifestation ≈ entity/instance | — |
| Nomen | designation (recorded) | **Nomen**: "an association between an entity and a designation that refers to it" (reified, attribute-bearing) — LRMoo `F12 Nomen` | under `E41 Appellation` |
| Appellation | designation (concept) | the `has appellation` relationship's object | `E41 Appellation` |
| `AcceptabilityRating` | acceptability rating | *no dedicated attribute — LRM subsumes "preferred" under the `Category` attribute* (see [Attestation quality](#attestation-quality-v028-extension-beyond-lrm)) | — |
| Preferred canonical nomen | preferred term | authorized access point | — |
| `Scheme`/`SourceURL` | — | nomen `scheme` / `reference source` attributes | — |

Two documented deviations from LRM: nomina resolve at **entity** level only
(LRM lets every WEMI level carry nomina; instance naming rides the
`ProviderID` scheme), and v1 keeps **single-claim-per-triple** (LRM models
multiple attestations; that is the named extension).

## See also

- [`architecture.md`](architecture.md) — how these concepts are implemented:
  pipelines, layers, storage, and the test architecture.
- [`research/entity-normalization.md`](research/entity-normalization.md) — the
  research that derived this ontology.
- [`../TESTING.md`](../TESTING.md) — the corpus standard that pins the
  vocabulary's behavior.
- [`w3id-runbook.md`](w3id-runbook.md) — how an `EntityRef.IRI(base)` becomes a
  resolvable public identifier (w3id.org registration, content negotiation).
- [`poetools-claude-code.md`](poetools-claude-code.md) — a concrete
  harness-vs-model conflation case in the ingested catalog, and the design
  question it raises for `harness.go`.
