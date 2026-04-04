package bestiary

// BestiarySchemaVersion is the semantic version of the bestiary JSON Schema.
// It follows semver (major.minor.patch) and must be incremented whenever the
// schema changes in a backward-incompatible way.
const BestiarySchemaVersion = "1.0.0"

// UpstreamSchemaVersion identifies the exact snapshot of the models.dev schema
// that this bestiary schema was derived from. Format: YYYY.MM.DD-sha256first12
// where sha256first12 is the first 12 lowercase hex characters (0-9, a-f) of
// the upstream schema file's SHA-256 hash. Uppercase hex is not accepted.
const UpstreamSchemaVersion = "2026.04.04-fd776194f63d"

// UpstreamGitCommit is the short Git commit hash of the models.dev repository
// revision that corresponds to UpstreamSchemaVersion.
const UpstreamGitCommit = "6a41e313"

// UpstreamGitRemote is the canonical Git remote URL for the models.dev
// repository from which the upstream schema was sourced.
const UpstreamGitRemote = "git@github.com:anomalyco/models.dev.git"
