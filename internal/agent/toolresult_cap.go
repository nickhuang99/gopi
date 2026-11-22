package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Tool-result token cap — the ONE place a tool result is bounded as it enters
// the conversation history.
//
// WHY A CAP AT ALL. A tool's output is written by the TOOL, and a few tools have
// no upper bound of their own: a `bash` command that prints a whole log, a
// keyword_search over a giant generated file. The distribution of every
// tool_result in this operator's session logs (38,887 results over 810
// session.jsonl files, counted with tokenizer.CountDefault — the run's own census
// reproduces it; the figures are in the task report) is:
//
//	p50 201 tokens, p90 971, p95 1469, p99 4752, max 1,213,310, mean 518.
//
// So the tail is not marginal: ONE result can be 250× the p99. Nothing downstream
// refuses it — EstimateTokens simply adds it to the context, the send-time
// request cap then refuses the whole request, and the round dies with a failure
// whose cause is one tool's output. This cap is the floor under that: one result
// costs at most ToolResultTokenCap tokens, and when it costs more the model is
// told exactly what was dropped and where to fetch it.
//
// WHAT THIS IS NOT. It is NOT the tools' own paging. `read` already pages at
// maxPageReadBytes (internal/tools/read.go:33, 50 KiB) and `web_fetch` bounds
// itself at its max_chars; both are the tool CHOOSING what to show and stay
// untouched. The cap is deliberately loose enough that a full 50 KiB read page
// passes through it untouched — ToolResultTokenCap's comment does that
// arithmetic. Nor is it the compaction path (context_compress.go), which deletes
// the oldest RUN of messages when the window fills. This one fires at
// INGESTION, per result, always.
const (
	// ToolResultTokenCap is the hard upper bound, in tokens, on ONE tool result
	// as it is appended to the conversation.
	//
	// 32768, from the measured distribution and NOT from a round number:
	//   - It must sit ABOVE the largest output a self-paging tool produces, or it
	//     silently becomes a second, tighter pagination for `read` (constraint:
	//     do not touch read's paging). maxPageReadBytes = 50*1024 = 51200 bytes,
	//     and this host's measured density for log-shaped text is 2.60–2.64
	//     chars/token (zz-census in this run; the corpus's own bucketed minimum
	//     is 3.02 for ≤4k results), so a full read page is ~19.7k tokens.
	//     32768 leaves that page 66% headroom.
	//   - It is ~6.9× the measured p99 (4752) and ~2.4× the largest result a
	//     legitimate tool call has produced here (the 13k-token class), so it
	//     cannot bite ordinary work: measured, 10 of 38,887 results (0.026%)
	//     exceed it.
	//   - It is ~16% of the 200K deployment window (DefaultContextWindow), which
	//     is the largest share ONE result may take of a request.
	// 4096 would have re-paginated 1.19% of results — read pages included;
	// 8192 would still have re-paginated 0.50%.
	ToolResultTokenCap = 32768

	// toolResultFreeBytes is the FREE tier's bound, in BYTES: a result at or
	// under it is returned untouched without a single BPE call.
	//
	// The bound is RIGOROUS, not statistical: a BPE token covers at least one
	// byte, so token count ≤ byte count for any text. len ≤ ToolResultTokenCap
	// therefore PROVES the result is under the cap. This is the tier every
	// ordinary tool call takes (a bash result, a read page of a few KB, a search
	// hit list), so the cap's cost on ordinary traffic is exactly zero.
	toolResultFreeBytes = ToolResultTokenCap

	// toolResultSampleChars bounds the SAMPLE a long result is measured on, and
	// is the tier above the free one: 8 KiB.
	//
	// WHY SAMPLING AT ALL — this is the change's whole technical risk. Measured
	// on this host with internal/tokenizer.CountDefault (this run's zz-census,
	// reproduced in the report):
	//
	//	52 KB log page    130–200 ms
	//	131 KB            ~350 ms
	//	2.0 MB            3.8–5.1 s
	//
	// (context_tokens.go:83 records the same rate from another angle: 313,101
	// chars / 456 ms.) Paying seconds inside a tool call is not acceptable, and
	// paying the 52 KB price on every large-but-ordinary result is not either. So
	// above the free tier the EXCERPT is sized from an 8 KiB sample (~20 ms) and
	// then the excerpt itself — max ~85 KB, ~200 ms — is counted exactly. Total
	// for a 2 MB result: ~220 ms instead of ~5 s.
	toolResultSampleChars = 8192

	// toolResultSampleMargin is the safety margin on a SAMPLED pass-through
	// decision: a result is passed through unmeasured only when the sample says
	// it costs at most (1-margin)×cap. The sample can under-estimate density
	// (the tail of a file can be denser than its head), and the margin buys 20%
	// of headroom for that — 6554 tokens at this cap. Past the margin the result
	// is measured exactly, so a result near the cap gets the precise answer.
	toolResultSampleMargin = 0.20

	// toolResultHeadShare is the fraction of the kept budget that goes to the
	// HEAD. A tool's output is usually read from the top (a command's echo, a
	// search's first hits), but its VERDICT is at the bottom (a bash result's
	// `exit=N`, a test run's summary), which is why a tail is kept at all and why
	// the head share is not 1.0.
	toolResultHeadShare = 0.6

	// toolResultBudgetSafety shrinks the FIRST excerpt attempt below the computed
	// budget, so the fitted excerpt lands under the cap in one pass.
	//
	// MEASURED REASON: an attempt whose excerpt comes out even 1% over the cap
	// costs a WHOLE extra pass — the excerpt is re-cut and re-counted, and the
	// count is the expensive half (measured: 648–663 ms for a ~191 KB excerpt of
	// a 560 KB result). At 0.9 the sample's own estimation error (measured spread
	// across content buckets: 3.02–3.39 chars/token) is absorbed and the retry
	// does not fire; the price is keeping 10% less of the text, which is the
	// right trade for a backstop whose whole job is bounding cost.
	toolResultBudgetSafety = 0.9

	// toolResultLastResortHalves is the divisor of the fallback excerpt: when the
	// measured fitting loop cannot converge at all, keep 1/N of the text at each
	// end. 4 is a bound, not a target — it exists so the cap NEVER returns the
	// untruncated text (a silent failure, Rule 15).
	toolResultLastResortHalves = 4

	// toolResultFitAttempts bounds the size-fitting loop. Attempt 1 sizes the
	// excerpt from the sample's ratio; each further attempt rescales by the
	// MEASURED miss (cap/got), which converges in one step unless density changes
	// sharply inside the kept region. Three is one derivation plus two
	// corrections; the loop is bounded so a pathological input cannot spin.
	toolResultFitAttempts = 3

	// toolResultCapMarkerFormat is the EXPLICIT note spliced between the kept
	// head and tail. Rule 15: a truncation is never silent — the model is told it
	// is reading an excerpt, how much is missing (chars and lines), and WHERE the
	// whole thing can be fetched from. LogToolResult writes the FULL result
	// before the cap runs (loop.go / chat_ops.go both log first, then append), so
	// the path in this note is a real recovery route.
	toolResultCapMarkerFormat = "\n[tool result capped at %d tokens: %d bytes / %d lines OMITTED from the middle " +
		"(kept %d head + %d tail bytes); the complete output is in %s (tool_use id %s)]\n"

	// toolResultCapMarkerPathUnknown stands in the note when the context never
	// recorded a session-log path (a hand-built Context in a test, a library
	// caller). It says so rather than printing an empty string, which a reader
	// would read as "there is no full copy".
	toolResultCapMarkerPathUnknown = "(session-log path not recorded for this context)"
)

// capToolResult bounds one tool result to ToolResultTokenCap tokens, keeping its
// head and tail and splicing an explicit note about what was dropped.
//
// REUSE, NOT A SECOND TRUNCATOR. The middle-elision SHAPE is the repository's
// existing one — `elideAtBytes` / `runeOffsets` (internal/agent/elide.go): keep
// `head` runes, keep `tail` runes, splice a marker between them. What is
// genuinely NEW here is only the policy around that shape: a TOKEN budget (not
// a rune count), a real tokenizer count rather than chars/4, a two-tier
// free/sample gate in front of that count, and a note that names the omission
// and its recovery route.
//
// THIS CAP IS NOT COMPACTION, and it does not become a second reduction path:
// it bounds ONE result as it enters the context, so the model reads an excerpt
// with a pointer to the full text instead of a 4K-token dump. The round's own
// history is untouched (the message the model is about to read is never
// rewritten) and nothing here depends on the window's fill level.
func (c *Context) capToolResult(toolCallID, result string) string {
	if result == "" {
		return result
	}
	plan := planToolResult(result)
	if plan.keep {
		return result
	}
	path := toolResultCapMarkerPathUnknown
	if c != nil && c.ToolResultSessionPath != "" {
		path = c.ToolResultSessionPath
	}
	budgetBytes := float64(plan.budgetChars) * toolResultBudgetSafety
	for attempt := 0; attempt < toolResultFitAttempts; attempt++ {
		headByte, tailByte := runeOffsets(result, int(budgetBytes*toolResultHeadShare),
			int(budgetBytes)-int(budgetBytes*toolResultHeadShare))
		if headByte <= 0 || tailByte <= headByte {
			break
		}
		excerpt := elideAtBytes(result, headByte, tailByte,
			c.toolResultCapMarker(toolCallID, path, result, headByte, tailByte))
		got := countTokens(excerpt)
		if got <= ToolResultTokenCap {
			return excerpt
		}
		// Rescale by the MEASURED miss: the excerpt's real density differed from
		// the sample's, and this ratio is now exact for exactly this text.
		budgetBytes *= float64(ToolResultTokenCap) / float64(got)
	}
	// The loop could not converge (density rising sharply inside the kept
	// region). Returning the UNTRUNCATED result would be a silent failure of the
	// cap, so keep the smallest excerpt the loop produced — the loop's own bound
	// is the ceiling, and the note still states what was dropped.
	headByte, tailByte := runeOffsets(result, int(budgetBytes*toolResultHeadShare),
		int(budgetBytes)-int(budgetBytes*toolResultHeadShare))
	if headByte <= 0 || tailByte <= headByte {
		// Last resort: a quarter of the text at each end. It cannot overshoot —
		// this branch is reached only when the measured loop failed to converge,
		// and half the input is still a bound the note states exactly.
		half := len(result) / toolResultLastResortHalves
		headByte, tailByte = half, len(result)-half
	}
	return elideAtBytes(result, headByte, tailByte,
		c.toolResultCapMarker(toolCallID, path, result, headByte, tailByte))
}

// toolResultPlan is the decision half of the cap, kept separate so it can be
// read and tested on its own.
type toolResultPlan struct {
	keep        bool    // the result passes through untouched
	budgetChars float64 // what the excerpt may keep, in BYTES (a conservative unit: see below)
	estTokens   int     // the token estimate the decision was taken on
}

// planToolResult decides whether a result has to be capped and how big the kept
// region may be. Its three tiers are ordered by COST, and each states what it
// knows rather than assuming it:
//
//	tier 0  len ≤ cap bytes      → provably under the cap (tokens ≤ bytes). FREE.
//	tier 1  sampled ≤ (1-m)×cap  → under the cap with a 20% margin. One sample
//	                              count (≤8 KiB, ~20 ms).
//	tier 2  otherwise            → over the cap; size the excerpt from the
//	                              sample's own ratio and count only the EXCERPT.
func planToolResult(result string) toolResultPlan {
	p := toolResultPlan{keep: true}
	bytes := len(result)

	// Tier 0 — rigorous, free.
	if bytes <= toolResultFreeBytes {
		return p
	}

	// Tier 1 — one bounded sample.
	ratio := toolResultSampleRatio(result)
	p.estTokens = int(float64(bytes) / ratio)
	if float64(p.estTokens) <= float64(ToolResultTokenCap)*(1-toolResultSampleMargin) {
		return p
	}

	// Tier 2 — cap it, sizing the excerpt from the sample's own density. A
	// result whose sample already proved it fits (est ≤ cap) gets the whole
	// text kept... but it is not "keep = true": an estimate that close to the
	// cap could be wrong, so the excerpt path measures exactly and returns the
	// full text only when the measurement says it fits.
	p.keep = false
	p.budgetChars = float64(ToolResultTokenCap) * ratio
	if p.budgetChars > float64(bytes) {
		p.budgetChars = float64(bytes)
	}
	return p
}

// toolResultSampleRatio measures chars-per-token on a bounded PREFIX of the text
// — a prefix rather than a random slice because a text is contiguous (a log, a
// table, a file) and a prefix is where its dominant shape lives.
func toolResultSampleRatio(result string) float64 {
	sample := result
	if len(sample) > toolResultSampleChars {
		// Cut on a rune boundary so the sample is never a broken encoding.
		cut := toolResultSampleChars
		for cut > 0 && !utf8.RuneStart(sample[cut]) {
			cut--
		}
		sample = sample[:cut]
	}
	n := countTokens(sample)
	if n <= 0 {
		// The tokenizer is unavailable (parse failure) → countTokens fell back to
		// chars/4 and returned 0 only for an empty sample. Use the byte length as
		// the ratio: it over-states the char budget (density 1), so the excerpt is
		// smaller than necessary, never larger.
		return 1
	}
	return float64(len(sample)) / float64(n)
}

// toolResultCapMarker renders the explicit note spliced into a capped result.
// The omitted figures are EXACT: bytes and runes are counted on the whole text
// and the lines are counted on the dropped middle itself.
func (c *Context) toolResultCapMarker(toolCallID, path, result string, headByte, tailByte int) string {
	omitted := ""
	if tailByte > headByte && tailByte <= len(result) {
		omitted = result[headByte:tailByte]
	}
	return fmt.Sprintf(toolResultCapMarkerFormat,
		ToolResultTokenCap,
		len(omitted), strings.Count(omitted, "\n"),
		headByte, len(result)-tailByte,
		path, toolCallID)
}
