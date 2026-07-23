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
| `AcceptabilityRating` | acceptability rating | nomen `status` attribute | — |
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
