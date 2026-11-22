package agent

// charsPerToken is the character-to-token FALLBACK divisor — the rule the
// package applies only when the embedded tokenizer is unavailable
// (heuristicTextTokens in context_tokens.go). It is NOT the basis of the
// estimates any more: EstimateTokens, EstimatedTokens, heuristicTokens and
// fixedFloorTokens all count on the BPE tokenizer, which is what the endpoint
// bills and what `gopi tool tokens` reports.
//
// KEPT AS A NAMED CONSTANT, not deleted (PROJECT.md Rule 1/13): the fallback is
// a real code path, and a bare 4 written at that one site would be the same
// defect the rule exists to prevent. Roughly 4 characters per token holds for
// English prose; for Chinese it is ~1, which is the 2.24x under-count this file's
// constant USED to cause when it was the estimate's basis. MEASURED per content class in the two pinned arms — internal/tokenizer's
// heuristic_ratio_test.go and this package's — which is why the figure here is a
// band and not a remembered number: English, the other way, reads ~1.4x OVER.
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

// messageTokenOverhead is the per-message constant added on top of the BPE count
// of a message's wire text (EstimateTokens, context.go). It is not a fudge
// factor for the count — the count is exact — but the template markers the
// endpoint charges around a message (`<｜User｜>`, the role transition, the
// closing sentence marker) that this package does not render, so they cannot be
// counted from the bytes it has.
//
// It is the SAME value the retired chars/4 arms carried (`…/charsPerToken + 1`),
// which keeps the estimate continuous with the suite's calibrated arithmetic and
// with the recorded API measurements: it was the DIVISOR that was wrong for CJK,
// never this overhead.
//
// PROJECT.md Rule 1: named here, referenced from the one call site, so a second
// `+ 1` cannot appear as a bare literal.
const messageTokenOverhead = 1
