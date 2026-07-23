package bestiary

// BestiarySchemaVersion is the semantic version of the bestiary JSON Schema
// (the public CLI JSON output schema in bestiary.schema.json). It follows
// semver (major.minor.patch) and must be incremented whenever the public output
// schema changes.
//
// This is DISTINCT from the SQLite store's currentSchemaVersion (store.go),
// which versions the on-disk cache migrations and bumps on its own cadence. Do
// not conflate the two: the JSON output schema and the SQLite cache schema
// evolve independently.
//
// Changelog:
//   - 0.0.2 → 0.0.3: widened the Modifier field string → []string
//     (backward-INCOMPATIBLE public schema change).
//   - 0.0.3 → 0.1.0: added the v0.2.3 entity-model fields (ModelInfo.Host,
//     ModelInfo.Lineage; ModelRef.Host; new $defs EntityRef, LineageEdge,
//     DerivationKind). Additive and backward-COMPATIBLE: the new fields are
//     optional/zero-value, so 0.0.x records still validate.
//   - 0.1.0 → 0.2.0: added the v0.2.4 VRAM/quantization/provenance fields
//     (ModelInfo.ParamSize, ModelInfo.QuantVRAM, ModelInfo.Source;
//     EntityRef.ParamSize; new $defs Quantization, QuantVRAM, ProviderInstance,
//     CapabilityUnion, Entity, DataSource, DatasetIngested, EntitySource).
//     Additive and backward-COMPATIBLE: every new field is optional/zero-value,
//     so 0.1.x records still validate.
//   - 0.2.0 → 0.3.0: added the v0.2.5 models.dev harmonization fields — the
//     instance-level ModelInfo props (Description, Status, StatusRaw,
//     ReasoningOptions, CostInputAudioPerMTok, CostOutputAudioPerMTok,
//     CostContextOver200k, CostTiers), the Entity.Metadata join projection,
//     ModelRef.ParamSize (the #size identity carrier on the identity tuple), and
//     new $defs ModelStatus, LinkType, ModelLink, BenchmarkResult,
//     ReasoningOption, TierCost, CostTier, EntityMetadata. (Catalog is a parser
//     return container, not a serialized output document, so it is deliberately
//     NOT a $def.) Additive and backward-COMPATIBLE: every new property is
//     optional/zero-value, so 0.2.x records still validate.
//   - 0.3.0 → 0.4.0: stamps the v0.2.6 parameter-shape fields decomposed from
//     ParamSize (ModelInfo.TotalParams, ActiveParams, PerExpertParams,
//     ExpertCount). These are DERIVED presentation facts, never entity-key
//     material, and follow the ParamShapeNull (-1) in-domain NULL sentinel
//     contract: -1 = not populated by parser or curation, a positive value = an
//     attested count, and a genuine 0 is reachable only for ExpertCount (a dense
//     shape attests zero experts). The schema pins minimum -1 on each. Additive and
//     backward-COMPATIBLE: every new property is optional, so 0.3.x records still
//     validate. (The full-bulk #size re-key that lands this epoch changes many
//     EntityRef keys but is a data change, not a schema-shape change — the ParamSize
//     carrier and its grammar are unchanged.)
//   - 0.4.0 → 0.5.0: adds the v0.2.7 naming layer and the Region axis. The naming
//     layer: new $defs Nomen + NomenScheme and an optional Entity.Nomina array (the
//     read projection of an entity's recorded namings — canonical Preferred nomen,
//     provider-ID Admitted nomina, curated third-party alias claims). The Region axis:
//     ModelInfo.Region + ModelInfo.RegionRaw (the AWS Bedrock cross-region
//     inference-profile jurisdiction, a per-instance attribute never part of entity
//     identity), ProviderInstance.Region + RegionRaw projected at registry roll-up,
//     an optional Entity.Regions sorted-unique aggregate, and a new $defs Region
//     closed string enum (which INCLUDES "unspecified" for the RegionNone zero value).
//     Additive and backward-COMPATIBLE: every new property is optional/zero-value
//     (Region defaults to "unspecified", RegionRaw to "", Nomina/Regions absent), so
//     0.4.x records still validate. This is the SOLE schema bump for the naming +
//     region work — do not add another this epoch.
//   - 0.5.0 → 0.6.0: adds the v0.2.8 creator dimension, the multi-attestation naming
//     model, and the OCI external-identifier scheme. The creator dimension: a new
//     $defs Creator (open string type — the SPDX originator / training lab, distinct
//     from Provider the supplier) and the DERIVED join projections ModelInfo.Creator
//     and Entity.Creator (baked / projected from Family via the curated creators.json
//     seed, never a stored column — Family → Creator is a function). The naming model:
//     a new $defs NomenAttestation (the per-claim evidence record) plus the
//     AttestationAuthority and IngestMethod element enums, and an optional
//     Nomen.Attestations array (a name HAS-MANY attestations; provenance is no longer
//     fused onto the name row). The external-identifier scheme: "oci" appended to the
//     $defs.CanonicalScheme enum tail (a per-quant-digest scheme token; ModelRef.Format
//     returns "" for it by design, as a bare ref carries no single OCI identity).
//     Additive and backward-COMPATIBLE: every new property is optional/zero-value
//     (Creator defaults to "", Attestations absent, "oci" a new enum member), and none
//     is added to any required[] array, so 0.5.x records still validate.
const BestiarySchemaVersion = "0.6.0"

// UpstreamSchemaVersion identifies the exact snapshot of the models.dev schema
// that this bestiary schema was derived from. Format: YYYY.MM.DD-sha256
// where sha256 is the full 64 lowercase hex character SHA-256 hash of the
// upstream schema file (packages/core/src/schema.ts).
const UpstreamSchemaVersion = "2026.07.12-a0d8cb006e5c2b848dd96fe622e4a61cf405906a1f4bb9a2dc03fdd890ddaac8"

// UpstreamGitCommit is the short Git commit hash of the models.dev repository
// revision that corresponds to UpstreamSchemaVersion.
const UpstreamGitCommit = "bf55e760"

// UpstreamGitRemote is the canonical Git remote URL for the models.dev
// repository from which the upstream schema was sourced.
const UpstreamGitRemote = "git@github.com:anomalyco/models.dev.git"
