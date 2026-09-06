package bestiary

import _ "embed"

// The GH#43 parser-conformance corpus is embedded into the TEST binary only, per
// the rule in TESTING.md: a corpus is test input and must never bloat the shipped
// package.

//go:embed testdata/parse/parser_conformance_corpus.json
var conformanceCorpusJSON []byte
