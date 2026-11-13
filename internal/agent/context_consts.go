package agent

// charsPerToken is the character-to-token heuristic divisor used by
// EstimateTokens and the context-size estimates (roughly 4 characters per
// token for mixed text).
//
// PROJECT.md Rule 1/13: the divide-by-four-plus-one token heuristic was inlined
// at five sites in context.go as a bare arithmetic expression; it is declared
// ONCE here and every site references it.
//
// Why a separate file rather than the top of context.go: devdoc/hippocampus.md
// cites `internal/agent/context.go:171` by LINE NUMBER, and an insertion above
// that line drifts the citation off its symbol. That cost is real and is why
// the constant landed here: a new file adds no line to any existing file, so
// the citation stays where it was. Same reason as internal/tools/limits_rule1.go.
//
// HISTORY, so nobody satisfies a check that does not exist. The citation above
// used to be ENFORCED: the devdoc line-anchor gate failed when a devdoc
// line anchor drifted, and it was deleted with internal/docclass in 5a35f61.
// Nothing resolves that citation now — it is a stale reference nobody checks.
// The census that replaced it over CONFIG.md's rows (`config-anchor-census.go
// --check`, TestMergeConfigAnchorGate* arms) never anchored a line of
// context.go either, and it was deleted with the documentation-citation family
// in 78bdf05a (2026-11-05). No executable anchor check runs on this tree. The
// reason to prefer a new file here is the diff cost of re-keying prose.
const charsPerToken = 4
