package bestiary

// BestiarySchemaVersion is the semantic version of the bestiary JSON Schema.
// It follows semver (major.minor.patch) and must be incremented whenever the
// schema changes.
//
// Changelog:
//   - 0.0.2 → 0.0.3: widened the Modifier field string → []string
//     (backward-INCOMPATIBLE public schema change).
//   - 0.0.3 → 0.1.0: added the v0.2.3 entity-model fields (ModelInfo.Host,
//     ModelInfo.Lineage; ModelRef.Host; new $defs EntityRef, LineageEdge,
//     DerivationKind). Additive and backward-COMPATIBLE: the new fields are
//     optional/zero-value, so 0.0.x records still validate.
const BestiarySchemaVersion = "0.1.0"

// UpstreamSchemaVersion identifies the exact snapshot of the models.dev schema
// that this bestiary schema was derived from. Format: YYYY.MM.DD-sha256
// where sha256 is the full 64 lowercase hex character SHA-256 hash of the
// upstream schema file (packages/core/src/schema.ts).
const UpstreamSchemaVersion = "2026.04.04-fd776194f63d717cce255cdfcff5ceaf18dccfe404a54f824a4b00afd354a8c6"

// UpstreamGitCommit is the short Git commit hash of the models.dev repository
// revision that corresponds to UpstreamSchemaVersion.
const UpstreamGitCommit = "6a41e313"

// UpstreamGitRemote is the canonical Git remote URL for the models.dev
// repository from which the upstream schema was sourced.
const UpstreamGitRemote = "git@github.com:anomalyco/models.dev.git"
