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
const BestiarySchemaVersion = "0.4.0"

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
