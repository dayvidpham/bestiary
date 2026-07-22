---
title: "v0.2.7 FOLLOWUP — entity-keyed constants + ProvidersOf, alias data model, Series→Release taxonomy, redundant-modifier suppression — Domain Research"
date: "2026-07-17"
depth: "deep-dive"
request: "bestiary-nz9ab (REQUEST); epic bestiary-c82y6; design children 3dkun/k8p8s/n187/z8w3; parse-fix children dmi4(9k7c/bep2/ibtb)/9ax7/ptr9/602y(wi36/e9pi)"
---

## Executive Summary

The v0.2.7 design half has four items. Research finds that **three of the four are already anchored by an authoritative standard bestiary partially implements** — the design work is mostly to *finish wiring* precedent, not to invent:

1. **Entity-keyed constants (3dkun, headline)** — the generated constant surface today is **5,654 `Model__<Provider>__…` constants encoding only 2,790 distinct model IDs across 162 providers** (each ID duplicated ~2× as a provider flavor). This *is* the provider-leak the user named. The correct external analogue is the **AWS SDK for Go v2 / smithy-go enum pattern**: one `TypeNameValue` PascalCase constant per member, a `Values()` enumerator, all in one generated file. bestiary already owns the two hard pieces — a canonical entity key (`EntityRef.String()`: `family[/variant][@version][#size]{mods}`) and a natural reverse index (`Entity.Instances[].Provider`). Recommend: generate **~976 entity constants + keep 162 provider constants**, add a `ProvidersOf(EntityRef) []Provider` reverse-lookup, and **retire the provider suffix** (the `__<Provider>__` prefix and the route-suffix echo that leaks today, e.g. `…Instruct__2507__3__Instruct`). **Adapt** the smithy grammar; the collision/versioned-identifier encoding is the real design work. **BREAKING** Go-API change (dwarfs r66e's 58 renames) — belongs behind the schema/version bump the epic already anticipates.

2. **Alias data model (k8p8s, user-committed)** — the ratified intent ("these aliases … are documented and linked to be referring to THE SAME entity … we need provenance and traceability") maps **almost exactly onto SKOS** (`skos:prefLabel` / `skos:altLabel` / `skos:hiddenLabel` + `skos:exactMatch`) crossed with **Wikidata's aliases-with-references** provenance discipline. bestiary already has the two halves: `Designation{Value, Scheme, Provider, Rating}` with `AcceptabilityRating{preferred, admitted, deprecated}` (ISO 1087, `designation.go`) is the label-with-status half; the `DataSource`/`EntitySource` BCNF core + the metadata `SourceURL`-vs-`Source` split (claimant vs ingest attestation) is the provenance half. Recommend an **alias EDGE** = `{spelling, target EntityKey, Rating, ClaimantURL, Source}` — a first-class relational table joining the two, not a new `_comment` convention. **Adopt** SKOS as the vocabulary anchor; **adapt** the edge into the existing BCNF + `Designation` types.

3. **Series→Release taxonomy (n187 / GH#8)** — the current flat `(Family, Variant, Version)` conflates *release number* with *semantic version* for Gemini/Phi-style lines. Industry (HF collections, models.dev `family.ts`) does **not** carry a formal Series→Release→Family hierarchy — they use flat family strings + human-curated collection pages. So there is **no external model to adopt**; the concern is real but the prior art says "don't over-engineer." Recommend **Defer the structural change**, and in v0.2.7 add only a **derived, non-identity `Series` grouping projection** (Gemini-3-Pro and Gemini-3-Flash roll up under Series=Gemini, Release=3) computed from the existing tuple — no re-keying, no migration. **Adapt lightly / mostly Defer.**

4. **Redundant-modifier suppression (z8w3 / GH#7)** — "opus-4-6-thinking ≡ opus-4-6 because Opus 4.6 is innately a thinking model." This is precisely the **ISO 1087 preferred-vs-admitted decision at the modifier level**, and bestiary *already has the rating type* (`AcceptabilityRating`) and *already extracts* `thinking` as a first-class modifier. The blocker is the user-named "enormous scope": a curated **innate-capability index** keyed `(family,variant,version)→{reasoning,…}`. Recommend scoping v0.2.7 to a **small, curated, high-confidence seed** (the Claude Opus/Sonnet 4.x reasoning-innate set) wired through `AcceptabilityRating` (preferred form drops the redundant modifier; `-thinking` spelling kept as an *admitted* alias edge — composes directly with item 2). **Adapt**; keep the index tiny and curated, never inferred.

The parse-fix children (quick local scan) **all still reproduce conceptually** against current `parse.go`, with one caveat worth a URE check (Mistral YYMM may already be partly date-guarded). Details in the final section.

---

## Topic Area 1 (bestiary-3dkun): provider-agnostic entity-keyed constants + `ProvidersOf` API

### Current state (local) — the surface, measured

`models_constants_gen.go` is one generated file with two blocks:

```go
// models_constants_gen.go:6-14 (header)
// Model__* constants … Names follow the pattern:
//   Model__<Provider>__<Family>__<Variant>?__<Version>?__<Date>?
const (
	Model__302AI__Claude__Opus__4_6          ModelID = "claude-opus-4-6"
	Model__OpenRouter__Claude__Opus__4_6     ModelID = "claude-opus-4-6"   // same ID, different provider
	…
)
// models_constants_gen.go:5672
// allModelConstants is the complete list of generated Model__* constants.
var allModelConstants = []ModelID{ Model__302AI__ChatGPT__4o__Latest, … }
```

Hard counts (2026-07-17, committed snapshot):

| Metric | Count |
|---|---|
| `Model__` constant declarations | **5,654** (one per `(provider, ID)` pair; == `StaticModels()` len) |
| Distinct `ModelID` **string values** | **2,790** |
| Distinct providers (leading segment) | **162** |
| `allModelConstants` slice references | 5,654 |
| Entity census (v0.2.6 F1) | **~976** entities |

> **Correction for the bead:** bestiary-3dkun quotes "11,308 Model__ ID-string constants." That figure is a **double-count** — 5,654 `const` declarations **plus** the 5,654-entry `allModelConstants` slice (5,654 + 5,654 + header ≈ 11,308). The accurate surface is **5,654 constants for 2,790 distinct IDs**, i.e. each model ID is emitted ~2.03× as a provider-flavored constant. This *is* the redundancy the user flagged.

Two concrete failure modes visible in the sample:

- **Provider leak (the headline complaint):** `claude-opus-4-6` → `Model__302AI__Claude__Opus__4_6`, `Model__OpenRouter__Claude__Opus__4_6`, … one per host. The constant identity is `(provider, id)` when the user wants it to be the *entity*.
- **Route-suffix / version-echo leakage in the name:** the name grammar appends a decomposed echo that duplicates tokens already in the name — e.g. `Model__302AI__Qwen3__235b__A22b__Instruct__2507__3__Instruct`, `Model__302AI__Doubao__Seed__1__6__Thinking__250715__1_6__Thinking`, `Model__302AI__Gemini__3__Pro__3__Preview` (the `3` and `Preview`/`Instruct`/`Thinking` appear twice). The name encodes both the raw-ID tokenization **and** the canonical decomposition, so they collide.

### External prior art: how large Go codebases expose generated constant surfaces

#### AWS SDK for Go v2 / smithy-go — the closest analogue

The canonical Go pattern for a large generated enum surface. Grammar is **`TypeName` + `MemberValue`, both PascalCase, concatenated**, string value preserved verbatim:

```go
// aws-sdk-go-v2 service/ec2/types/enums.go (single file, ~13.4k lines / 475 KB)
const (
	AcceleratorManufacturerAmazonWebServices AcceleratorManufacturer = "amazon-web-services"
	AcceleratorNameA100                      AcceleratorName          = "a100"
)
func (AcceleratorManufacturer) Values() []AcceleratorManufacturer { … } // full-member enumerator
```

Takeaways that map directly onto bestiary:
- **One file per generated surface** is fine at 5–13k lines (`unicode/tables.go`, EC2 `enums.go` both prove Go tolerates this; see the sharding caveat below).
- **A `Values()` enumerator method** is the idiomatic "full list of X" API — bestiary's `Entities()` / `Providers()` already fill this role; keep them.
- Special characters (hyphens, dots) are **dropped/cased-away** in the identifier while the value literal preserves them — exactly bestiary's `4.5 → 4_5`, `4o → 4o` rule.
- Smithy does **not** publish a documented collision policy — it relies on the service model being collision-free. bestiary **cannot** assume that (its keys are derived, not authored), so collision policy is genuinely new design (below).

#### `golang.org/x/text/unicode`, `unicode/tables.go`, k8s generated APIs — the sharding caveat

Large generated tables (`x/text`) teach one cost lesson: **init-time and binary-size cost scales with the table**, and importing a big generated package "immediately bloats a binary by ~100k" and can alloc tens of KB in `init` whether or not used (golang/go#26752). Mitigation the ecosystem uses: **lazy init** and **package-split so callers import only what they need**. For bestiary this argues for keeping *constants* (compile-time, zero runtime cost, tree-shaken if unreferenced) over runtime maps where possible, and — if the entity constant file ever dominates compile time — the option to **shard by first letter / by family** (k8s shards generated deepcopy/conversion per-group-version). **Not needed at ~976 entity constants; note as a scaling escape hatch.**

#### Reverse-lookup (`ProvidersOf`) prior art

The user's ask — "access the providers of a model via the Golang API" — is a **value→keys reverse index**. Registry-shaped Go libs (the SDK service registries, protobuf's `protoregistry`, `database/sql`'s driver registry) all expose a forward map plus a helper that inverts it. bestiary already has the data: `Entity.Instances[].Provider` is the forward projection; `ProvidersOf` is a one-line reverse over it. This is not novel — it's an **API-surface** addition, and the deduped `Entity.Providers []Provider` field already exists (`entity.go:195`).

### What identifier grammar can encode the canonical entity key?

The entity key is `family[/variant][@version][#paramsize]{mods}` (`entity.go:115`). A Go-identifier encoding must injectively map the five sentinels (`/ @ # {} ,`) to identifier-legal separators. Proposed grammar (mirrors the smithy `Type+Value` shape, but the "type" is the whole entity):

```
Entity__<Family>[__V_<Variant>][__At_<Version>][__Sz_<ParamSize>][__Mod_<mod1>[_<mod2>…]]
  family    "/variant"      "@version"        "#paramsize"        "{mod,mod}"
```

- `.`→`_` inside a component (existing rule); `-`→`_` or camel-fold (existing rule).
- The **sentinel-tagged segments** (`V_`, `At_`, `Sz_`, `Mod_`) make the grammar *invertible* and prevent the version-echo collision that plagues the current names (a version can never be mistaken for a variant because it is prefixed `At_`).
- Drop `__<Provider>__` entirely — provider moves to `ProvidersOf()`.

**Collision policy (the real design work).** Smithy assumes uniqueness; bestiary must guarantee it. Two entities can only collide if their `EntityRef.String()` collides — which by construction they cannot (the key *is* the identity). So **the identifier is collision-free iff the sentinel encoding is injective**. The one residual risk: two *distinct* keys folding to the same identifier because the encoding is lossy (e.g. a variant literally named `at-4` vs `@4`). The sentinel-prefix scheme (`V_at_4` vs `At_4`) resolves this. Follow the v0.2.6 determinism discipline: emit via explicit `sort.Slice` on `EntityRef.String()`, and add a **codegen guard that asserts identifier injectivity** (no two entity keys produce the same constant name) — the analogue of the existing `_N` collision test.

### Assessment & recommendation (3dkun)

| Aspect | Keep `Model__<Provider>__…` | **Entity__ + ProvidersOf (recommended)** |
|---|---|---|
| Constant count | 5,654 (2× dup) | **~976 entity + 162 provider** |
| Provider leak | Yes (in identity) | No (moved to reverse API) |
| Version-echo collision | Present in names | Eliminated by sentinel tags |
| Reverse lookup | Indirect (`Instances` filter) | `ProvidersOf(ref) []Provider` |
| Go-API break | — | **Yes, large** (schema+version bump) |
| Precedent | none clean | smithy-go `Type+Value` + `Values()` |

**Adopt** the smithy grammar shape and `Values()`-style enumeration. **Adapt** it with sentinel-tagged segments to encode the 5-part key injectively. **New design:** the injectivity codegen guard + the deprecation window for the old `Model__` names (consider keeping `Model__` as a thin generated alias block for one release to soften the break). This is the epic headline and the largest breaking change since r66e — sequence it first, behind the version bump.

---

## Topic Area 2 (bestiary-k8p8s): first-class alias data model with claim attribution

### Ratified intent (local) — UAT bestiary-2v3zg §C4, verbatim

> "It's also important that these aliases or accepted expressions or nomens … are documented and linked to be referring to THE SAME entity. we need provenance and traceability." … user: "I thought we already had a sort of alias data model? We have canonical vs. others?" → (explained: derivation-based unification exists; **missing: alias EDGES with claim attribution**) → "Okay. This alias data model should be in the next release."

So the gap is explicit: today unification is **derivation-based** (mechanical decomposition + curated exact-ID pins, provenance living only in `_comment` fields of `parse/data/*.json`). What's missing is a **machine-readable edge**: *this spelling* → *this entity*, **asserted by** *this claimant*, **read from** *this source*.

### Existing bestiary halves to join

- **Label-with-status half** — `designation.go` already models it:
  ```go
  type Designation struct { Value string; Scheme CanonicalScheme; Provider Provider; Rating AcceptabilityRating }
  // AcceptabilityRating ∈ {AcceptabilityPreferred, AcceptabilityAdmitted, AcceptabilityDeprecated}  (ISO 1087)
  ```
  Today **all** generated designations default to `Admitted`; promotion is deferred — i.e. the type exists but the *edges* and *ratings* are unused.
- **Provenance half** — the BCNF core (`datasource.go`): `DataSource(ID, URI, CanonicalName)` dimension + `EntitySource(EntityKey, SourceID)` join, and the metadata module's **`SourceURL`-vs-`Source` discipline** (`metadata.go`): `SourceURL` = *who reported it* (claimant, e.g. a model-card URL), `Source` (`DataSourceID`) = *which ingest we read it from*. That is exactly the two provenance levels an alias edge needs.

### External prior art: alias/synonym modeling with attribution

| Standard | Alias primitive | Canonical/target | Provenance/attribution | Fit for bestiary |
|---|---|---|---|---|
| **SKOS** (W3C) | `skos:altLabel`, `skos:hiddenLabel` (misspellings/obsolete) | `skos:prefLabel` (≤1 per lang) | `skos:exactMatch` links concepts "interchangeable"; via reification/notes | **Vocabulary anchor.** prefLabel↔`Preferred`, altLabel↔`Admitted`, hiddenLabel↔`Deprecated`/misspelling maps 1:1 onto `AcceptabilityRating`. |
| **Wikidata** | `aliases` (many, per-lang) | `label` (1 per lang) | **statements carry `references`** (≥1 source per claim); qualifiers refine | **Provenance discipline anchor.** "every alias claim cites a source" == bestiary's `SourceURL`+`Source`. |
| **schema.org** | `sameAs` (URL to a reference identifying the thing) | the entity itself | implicit (the URL is the reference) | Lightweight; weaker status vocabulary than SKOS. Skip as primary. |
| **PURL** | qualifiers (`?k=v`), namespace | `type/namespace/name@version` hierarchy | none (pure identifier) | Good **identity-grammar** reference (most-significant-left), no attribution model. Informs item 1 more than item 2. |
| **npm dist-tags** | mutable string tag → 1 version (`latest`,`next`) | the version (immutable) | none | Models the *mutable-alias → immutable-target* shape (`latest`→digest). Reinforces: alias is a **pointer**, target is the stable key. |
| **OCI tags→digest** | mutable tag | immutable digest (sha256) | annotations (lifecycle) | Same mutable-pointer/immutable-target lesson as npm; digest≈`EntityRef.String()`. |

**Synthesis:** SKOS gives the **status vocabulary** (which bestiary already half-owns via `AcceptabilityRating`); Wikidata gives the **attribution discipline** (every alias edge cites its source — which bestiary already half-owns via `SourceURL`/`Source`). npm/OCI confirm the **pointer→stable-target** shape (the alias is mutable/curated; the entity key is the immutable target).

### Recommended direction (k8p8s)

A first-class **alias edge**, BCNF, joining the two existing halves:

```go
// AliasEdge: one asserted "this spelling denotes this entity" claim.
type AliasEdge struct {
    Spelling   string               // the alt/raw expression, e.g. "grok-4.20-beta-0309-reasoning"
    Target     string               // EntityRef.String() — the canonical entity it denotes (FK to entity)
    Rating     AcceptabilityRating  // preferred (canonical) | admitted (accepted alias) | deprecated
    ClaimantURL string              // WHO asserts it (model page / provider docs) — the SourceURL analogue
    Source     DataSourceID         // WHICH ingest we read it from — FK to DataSource
}
```

- **Curated JSON codegen input** (`parse/data/aliases.json`) following the `datasources.json`/`ollama_aliases.json` precedent (embedded glob, graceful-degrade loader, `_comment` for human notes — but the *machine* provenance now lives in `ClaimantURL`/`Source`, not the comment).
- **Store table** (schema bump) mirroring `EntitySource`: PK `(Spelling, Target, Source)`; real FK to `DataSource` and to the entity key; round-trippable via a `--export` union like `sources --export`.
- **`Designation` becomes the read projection** of alias edges for an entity (the `Rating` finally gets used): `preferred` = canonical spelling, `admitted` = the accepted aliases, `deprecated` = retired spellings.
- **Composes with item 1** (the alias `Target` is the same entity key the constants encode) and **item 4** (`-thinking` redundant spelling becomes an *admitted* alias edge, not a distinct entity).

**Adopt** SKOS as the naming/status anchor and Wikidata's cite-every-alias rule. **Adapt** into the `AliasEdge` + BCNF table; do **not** invent a bespoke vocabulary. This is user-committed for v0.2.7 — highest-priority after the constants headline, and it de-risks item 4.

---

## Topic Area 3 (bestiary-n187 / GH#8): Series → Release → Family/variant taxonomy

### The concern (local) — GH#8, verbatim

> "a model 'family' is typically associated with its Model + Release number (e.g. Gemini 3), and then {Flash, Pro} are variants … we have a Model Series (Gemini), with Gemini 3 being the latest Release … the 'version' is relevant when there is a `-preview` or a `-<date>`." The flat `(family=gemini, variant=pro, version=3)` **conflates the release number (3) with a semantic version** and loses Series→Release→Variant. `gpt-5-codex` is a second oddity — `codex` is a *harness*, not a tier.

### External prior art — and the null result

- **Hugging Face** organizes by **Collections** — a *curated, ordered, human-authored grouping page* ("Llama 4 series: Scout, Maverick") and by **Organizations**. Critically, HF carries **no formal Series→Release→Family schema in the model metadata**; the hierarchy is editorial (a collection is just a named list) plus an informal **phylogeny/family-tree** (base→derivative edges) — which bestiary *already* models as `LineageEdge`/`DerivationKind`.
- **models.dev** (bestiary's own upstream) uses a **hand-maintained flat `family.ts` enum** + substring matching in `describe.ts`. No release/series layer at all.
- **LMSYS / arena naming** is flat display strings (`gemini-1.5-pro-002`), no structured hierarchy.

**Null result:** there is **no external standard** to adopt for a Series→Release→Family hierarchy. The industry deliberately keeps model taxonomy flat + curated-collection + lineage-edges. This is a strong signal **against** a structural re-key.

### Assessment & recommendation (n187)

| Option | Cost | Risk | Verdict |
|---|---|---|---|
| Full Series→Release→Family re-key | High (new identity axis; migration; every key changes) | High (breaks item 1's just-stabilized constants) | **Defer** |
| Derived `Series` grouping projection (no re-key) | Low (compute from existing `Family`/`Version`) | Low (additive, non-identity) | **Adapt (light)** |
| Curated `harness` attribute for `codex`-style tokens | Low | Low | Fold into item-4 modifier-class work |

**Recommend Defer the structural change** (aligns with the bead's own "concern to flag, not a change to make now" and with the null external result). For v0.2.7, ship at most a **derived, non-identity `Series` roll-up** — a read projection that groups `gemini@3/pro` and `gemini@3/flash` under `{Series: gemini, Release: 3}` computed from the existing tuple, exposed like the existing `Entity.Providers` denormalized view. No new identity, no migration, no constant churn. The `codex`-is-a-harness case is better handled by the existing `Harness` type (`harness.go`) + item-4 modifier classification than by a taxonomy rework. **Defer** (structure) / **Adapt** (optional derived projection).

---

## Topic Area 4 (bestiary-z8w3 / GH#7): redundant-modifier suppression (preferred vs admitted)

### The insight (local) — GH#7, verbatim

> "opus-4-6-thinking, but **Opus 4.6 IS a thinking model** — the modifier is redundant. `-thinking` only makes sense if the base model IS NOT a thinking model (Haiku, some Gemini Flash) … the '-thinking' modifier would be another **admitted appellation**; the **preferred** term would *not* have it. However … we would need to maintain a separate index for the model's innate features/capabilities."

### Direct precedent already in-tree

This is the **ISO 1087 preferred-vs-admitted designation decision at the modifier level** — and bestiary already has three of the four pieces:

1. **The rating type** — `AcceptabilityRating{Preferred, Admitted, Deprecated}` (`designation.go`), currently defaulting everything to `Admitted`.
2. **First-class modifier extraction** — `thinking`/`think` is already parsed as a modifier (Epoch 2 delivered this; `parse.go` `ExtractModifier`).
3. **The alias-edge machinery** (item 2) — the redundant `-thinking` spelling becomes an *admitted `AliasEdge`* pointing at the preferred (modifier-stripped) entity.

The **one missing piece** is the user-named "enormous scope": a curated **innate-capability index** `(family,variant,version) → {reasoning: bool, …}`.

### External standard: ISO 1087 (confirmed)

ISO 1087:2019 defines the acceptability rating exactly as bestiary's enum: **preferred term** (used in the main body), **admitted term** (accepted synonym, may be several), **deprecated term** (discouraged/obsolete). The canonical example ("terminology science" preferred, "terminology studies" admitted, "terminology" deprecated) is structurally identical to `opus-4-6` (preferred) / `opus-4-6-thinking` (admitted). No new standard needed — bestiary's `designation.go` is *already* the ISO 1087 implementation; it just needs edges + a capability oracle to decide `preferred` vs `admitted`.

### Scope-control recommendation (z8w3)

The user's deferral reason is scope, not doubt. Contain it:

- **Do NOT infer innateness.** Keep the index **small, curated, high-confidence** — seed only the families where curation is certain (Claude Opus/Sonnet 4.x reasoning-innate; explicitly *exclude* Haiku/Flash where `-thinking` is load-bearing). A `parse/data/innate_capabilities.json` file on the graceful-degrade loader precedent; an absent/uncertain entry means **keep the modifier** (fail-safe = never wrongly collapse).
- **Normalization pass:** if `Modifier ∈ {thinking, think}` AND `innate.reasoning(family,variant,version)` is true → preferred `EntityRef` drops the modifier; the `-thinking` spelling is emitted as an **admitted `AliasEdge`** (item 2) so traceability is preserved and no ID is dropped.
- **Wire the rating:** this is the first real user of `AcceptabilityPreferred` — the promotion path `designation.go` explicitly deferred.

| Aspect | Full innate-capability index | **Curated seed (recommended)** |
|---|---|---|
| Data surface | Huge (every family/tier/version) | Tiny (a handful of certain families) |
| Risk of wrong collapse | High | ~none (fail-safe keeps modifier) |
| Reuses `AcceptabilityRating` | Yes | Yes |
| Depends on item 2 | Yes (alias edge for the admitted spelling) | Yes |

**Adapt** — implement the mechanism (rating + normalization + alias edge) but **seed the index conservatively**. Sequence **after** item 2 (it consumes the alias-edge type). Keep the index curated forever; the full auto-inferred version stays deferred.

---

## Parse-fix children — quick local scan (no web research)

All cases confirmed reproducing **conceptually** against the current generated constants / `parse.go` (grep-level confirmation on the committed snapshot; URE should re-confirm with a live parse run):

| Bead | Documented failing case | Current state (confirmed) | Notes |
|---|---|---|---|
| **9k7c** (dmi4) | empty-`raw_family` re-host `claude-3(.\|-)5-haiku` → `(claude,'','')` | Reproduces: `anthropic-claude-3.5-haiku` → `Model__…__Anthropic__Claude__3__5__Haiku` (provider-prefix `anthropic-` read as family; namespaced/empty-raw rehosts still mis-decompose) | Blocks cross-provider r66e collapse; fix = extract variant+version on the empty-raw path for both dot/hyphen forms |
| **ibtb** (dmi4) | additive sole-variant-suffix promotion without nulling versions | Still open — `text-embedding-3-*` residual class persists | ADDITIVE only; guard with a test asserting the version-populated set does not shrink |
| **bep2** (dmi4) | dead-code + `isYYMMDateToken` rename batch | Cleanup batch, non-behavioral | Bundle the `isBareFourDigitDateToken` rename + dead `reYYMMCandidate` removal |
| **9ax7** | cohere `command-r7b-12-2024` → version `12` (MM of MM-YYYY) | Reproduces: `Model__Cohere__Command__R7b__12__20241202` (`12` captured as version) | Thread date-guard through the `r7b`-glued token so MM-YYYY reads as Date |
| **ptr9-1** | `meta-llama-3_3-70b-instruct` (no-slash) → `meta` (family-fold) | Slash-form converges (`meta-llama/Meta-Llama-3.3-70B-Instruct` OK); the single no-slash malformed id needs USER `family_aliases` sign-off for `meta→llama` | May be absent from current catalog — verify at URE |
| **ptr9-2** | `azure-gpt-4-turbo` → `azure-gpt` | Reproduces: `Model__NanoGPT__Azure__GPT__4__Turbo` (`azure` leaks as pseudo-family) | `azure` IS a Provider — needs a provider-prefix-strip mechanism, not a vendor-alias (would collaterally break `azure-gpt-4o`) |
| **ptr9-3** | `grok-3-mini-fast-beta` — mid-family `fast`, tail `beta` blocks scan | Mid-ID modifier extraction (defers to the general mid-ID engine, GH#9/p0w6f territory) | Overlaps v0.2.6's deferred mid-ID work |
| **wi36** (602y) | Mistral YYMM false-positive versions (`large-2512`→`2512`) + namespaced-ID coverage | **Partial caveat:** `mistral-large-2512` now renders `Model__…__Mistral__Large__2512` with **no version-echo** — suggests `2512` is already read as a Date, i.e. the YYMM guard may be **partly landed**. `mistral-small-2603` absent from sample. | **Flag for URE:** re-measure the empty-`NormalizedVersion` YYMM set before scoping — the bead's numbers predate later date-guard work |
| **e9pi** (602y, P2) | `ReasonUnknownSuffixOverflow` unreachable by construction (Step-5 greedy absorb) | Structural: Step-5 fallback absorbs all trailing tokens → `extra=0`, threshold never met; positive test is `t.Skip`/regression-blind | Reorder `ParseFamilyWithVersion` so Step-5 leaves truly-unknown tokens unaccounted; re-enable the skipped positive test with `t.Errorf` |

**Cross-cutting parse observation:** ptr9-2 (`azure` provider-prefix) connects to the **MEMORY note "Provider-prefix strip was wrong"** (azure-* strip once deleted NanoGPT's backend-host label) — the fix must strip the prefix from the *ID decomposition* while preserving the `Provider`/`Host` fields, not blanket-delete the token. Verify Provider field + upstream before stripping.

---

## Summary

| Topic Area | Recommendation | Rationale |
|---|---|---|
| 1 — Entity-keyed constants + `ProvidersOf` (3dkun) | **Adopt** smithy grammar + **Adapt** sentinel-tagged key encoding | 5,654→~976 constants; provider leak → reverse API; injectivity is the new design + a codegen guard. Headline, breaking. |
| 2 — Alias data model (k8p8s) | **Adopt** SKOS vocab + Wikidata attribution; **Adapt** into `AliasEdge` BCNF | Both halves already in-tree (`AcceptabilityRating` + `SourceURL`/`Source`); user-committed; de-risks item 4. |
| 3 — Series→Release taxonomy (n187) | **Defer** structure; optional **Adapt** derived `Series` projection | No external standard exists; industry stays flat+curated+lineage; a re-key would churn item-1's constants. |
| 4 — Redundant-modifier suppression (z8w3) | **Adapt** with a **curated seed** index | ISO 1087 already implemented (`designation.go`); mechanism is small, scope is the risk → keep the index tiny & fail-safe. |
| Parse fixes (dmi4/9ax7/ptr9/602y) | **Fix** — capture currently-wrong decompositions as validation cases | All reproduce conceptually; Mistral YYMM may be partly fixed → re-measure at URE. |

## Key Takeaways

### Adopt
- **smithy-go enum grammar** (`TypeName+Value` PascalCase, single file, `Values()` enumerator) as the shape for the new entity constants (item 1).
- **SKOS** (`prefLabel`/`altLabel`/`hiddenLabel`/`exactMatch`) as the alias status vocabulary and **Wikidata's cite-every-alias** provenance rule (item 2).
- **ISO 1087** as the ratified basis for redundant-modifier suppression — it is *already* implemented as `AcceptabilityRating` (item 4).

### Adapt
- Entity-key → Go-identifier encoding with **sentinel-tagged segments** (`V_`/`At_`/`Sz_`/`Mod_`) to kill the version-echo collision, + an **injectivity codegen guard** (item 1).
- An `AliasEdge{Spelling, Target, Rating, ClaimantURL, Source}` first-class BCNF table joining `Designation` + the `DataSource`/`EntitySource` core; `Designation` becomes its read projection (item 2).
- A **conservatively-seeded** `innate_capabilities.json` on the graceful-degrade loader precedent; absent ⇒ keep the modifier (item 4).

### Defer
- The full **Series→Release→Family re-key** (n187) — no external standard, high churn against item 1; ship at most a derived non-identity `Series` roll-up.
- The **auto-inferred** innate-capability index (item 4 full version) — keep only the curated seed for v0.2.7.
- **e9pi** parser Step-5 reorder is P2 but structural — scope carefully so it doesn't destabilize the parse fixes landing alongside it.

### Skip
- **schema.org `sameAs`** as the primary alias primitive — weaker status vocabulary than SKOS; only useful as a secondary `exactMatch`-style external link.
- Computing MoE-style `Total = N×M` or treating a **release number as a semantic version** — both are known-wrong (n187 conflation; the v0.2.6 MoE nominal-count trap).
- **Provider-prefix blanket-delete** for ptr9-2 (`azure-*`) — MEMORY confirms it deletes real backend-host labels; strip from ID decomposition only, preserve Provider/Host.
