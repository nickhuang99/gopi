package gopilog

// cost.go — THE COST BLOCK IS NOT PART OF A SESSION LOG.
//
// THE RULING THIS FILE RECORDS (operator, 2026-11-04). `gopi session log <id>`
// used to end with a `COST:` block — Iterations, turns, Passes, Prompt /
// Completion / Total tokens, Elapsed. A reader of a LOG takes a block that sits
// at the log's tail for something the session WROTE, and every one of those
// numbers was CALCULATED by a formatter from the parsed `request` rows. A
// calculated number rendered inside a log is a category error: the log records
// what happened, the block re-derives an opinion about it, and the two live in
// one stream with nothing between them saying which is which.
//
// SO: the numbers move to the view that is backed by a RECORD rather than by
// rows — `gopi session show <id>` — and they are read from what the session
// RECORDED about itself, not re-summed:
//
//	Passes    session_end total_iterations   (summed over run segments: each
//	Total     session_end total_tokens        verdict reports only its own run)
//	Elapsed   session_end elapsed_sec
//	split     the chats row's recorded token columns, for a chat session
//
// The log keeps a POINTER, never the numbers (see CostPointerLine): one line
// naming the verb that does carry them.
//
// WHY THE BLOCK LIVES HERE AND NOT IN cmd/agent. Two surfaces must agree about
// one fact — the show page renders it, the log points at it — and Rule 14 says
// one definition. It also keeps the counter vocabulary in the package that owns
// it (sessionevent.CounterUnit*/CounterSource*), so no surface can print a
// number under another quantity's word.

import (
	"fmt"

	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/sessionevent"
)

// The block's own words. Named (Rule 1) because the renderer, the tests that
// pin the shape and the log's pointer line all read them.
const (
	// CostBlockHeader opens the show page's cost block.
	CostBlockHeader = "COST:"
	// CostBlockBasis is the line under the header: WHAT the numbers are, so a
	// reader cannot mistake them for the log's own content a second time.
	CostBlockBasis = "the session's RECORDED totals (`session_end`), not a per-request sum"
	// CostPassesKey / CostTotalKey / CostElapsedKey / CostSplitKey are the
	// block's key column.
	CostPassesKey  = "Passes"
	CostTotalKey   = "Total"
	CostElapsedKey = "Elapsed"
	CostSplitKey   = "Tokens"
	// CostUnrecorded is the value of a quantity no `session_end` recorded —
	// a live run, or one killed before its end hook. It is a WORD, never a 0:
	// a zero is a measurement and nothing took one (PROJECT.md Rule 15).
	CostUnrecorded = "not recorded — this session wrote no `session_end`"
	// CostSplitUnrecorded is the value of the per-direction split when the
	// session's species records none. It names WHY, because "no numbers" and
	// "this face cannot carry them" are different answers.
	CostSplitUnrecorded = "no per-direction split is recorded for a run — `session_end` records one total"
	// CostMoneyKey is the block's last row: the REAL MONEY (元) is not here.
	//
	// THE POINTER CHAIN HAD A DEAD END. `session log` points at `session show`
	// (CostPointerLine), and the show page pointed at nothing: measured on
	// 3a93ac1c-c0a3-448d-a14f-9eb673b64f9f, `session show` printed "元" 0 times
	// and the word "stats" 0 times, while `session stats <id> --cost` printed
	// "total cost 1.7568 元 @ CNY". A reader following the log's instruction
	// arrived at a page that cannot answer the question the instruction was
	// about — the same "one fact, no authority" defect this epic removes, one
	// hop further along.
	//
	// A POINTER AND NOT A NUMBER, deliberately: the money is priced by
	// internal/pricing (regime windows, cache tiers, the price snapshot of the
	// day), and a second computation here would be the forbidden "写第二份 cost
	// 计算". `session show` prints what the session RECORDED; the price is what
	// the ledger's writer computed, and `session stats --cost` is its one
	// surface.
	CostMoneyKey = "Money"
	// CostMoneyPointer is that row's value. The verb is spelled from
	// internal/cliverbs so a rename of the session family moves the instruction
	// with it (Rule 13), and it carries the selector placeholder rather than a
	// bare address so what a reader pastes is runnable.
	CostMoneyPointer = "not recorded here — the price is calculated, ask " +
		cliverbs.Program + " " + cliverbs.SessionStatsAddress + " " +
		cliverbs.ResumeSelectorPlaceholder + " --cost"
)

// RecordedCost is the block's INPUT: facts a session recorded about itself,
// carried across the package boundary so no renderer has to re-derive them.
//
// EVERY FIELD IS A RECORDING OR An ABSENCE. There is deliberately no
// per-request field: a caller that could fill one would be free to re-sum the
// rows, which is the defect this whole move removes.
type RecordedCost struct {
	// HasEndTotals says a `session_end` was read at all. false means the three
	// totals below are unmeasured, and the block says so instead of printing
	// zeroes.
	HasEndTotals bool
	// Passes is session_end.total_iterations, summed over the log's segments.
	Passes int
	// TotalTokens is session_end.total_tokens, summed over the segments.
	TotalTokens int
	// ElapsedSec is session_end.elapsed_sec, summed over the segments.
	ElapsedSec float64
	// HasTokenColumns says the per-direction split below is a MEASUREMENT —
	// the chats row's recorded columns (a chat session). A task run records
	// none, and the block states that absence rather than inventing a split.
	HasTokenColumns bool
	InputTokens     int
	OutputTokens    int
	CacheTokens     int
}

// FormatRecordedCost renders the block. It prints the header and the basis line
// unconditionally: a block that vanished when unmeasured would leave a reader
// unable to tell "no cost" from "no block here", and the basis line is what
// stops the numbers being read as logged content one more time.
func FormatRecordedCost(c RecordedCost) string {
	out := CostBlockHeader + "\n"
	out += "  " + CostBlockBasis + "\n"
	if !c.HasEndTotals {
		// ONE absence, stated once: three `not recorded` lines would say the
		// same thing three times about one missing event.
		out += fmt.Sprintf("  %-*s%s\n", recordedCostKeyWidth, CostPassesKey, CostUnrecorded)
		out += formatRecordedCostSplit(c)
		out += fmt.Sprintf("  %-*s%s\n", recordedCostKeyWidth, CostMoneyKey, CostMoneyPointer)
		return out
	}
	out += fmt.Sprintf("  %-*s%d  — %s\n", recordedCostKeyWidth, CostPassesKey, c.Passes, sessionevent.CounterSourceSessionEnd)
	out += fmt.Sprintf("  %-*s%s tokens\n", recordedCostKeyWidth, CostTotalKey, formatNum(c.TotalTokens))
	out += fmt.Sprintf("  %-*s%.1fs\n", recordedCostKeyWidth, CostElapsedKey, c.ElapsedSec)
	out += formatRecordedCostSplit(c)
	out += fmt.Sprintf("  %-*s%s\n", recordedCostKeyWidth, CostMoneyKey, CostMoneyPointer)
	return out
}

// recordedCostKeyWidth is the block's label column. Nine cells fits every key this
// block carries with room to spare, and it is a constant so the renderer and
// the tests cannot drift into two widths (Rule 1).
const recordedCostKeyWidth = 9

// formatRecordedCostSplit renders the per-direction line, or the sentence that
// says why there is none.
func formatRecordedCostSplit(c RecordedCost) string {
	if !c.HasTokenColumns {
		return fmt.Sprintf("  %-*s%s\n", recordedCostKeyWidth, CostSplitKey, CostSplitUnrecorded)
	}
	return fmt.Sprintf("  %-*s%s tokens  (the session's recorded columns)\n", recordedCostKeyWidth, CostSplitKey,
		fmt.Sprintf("in=%s out=%s cache=%s", formatNum(c.InputTokens), formatNum(c.OutputTokens), formatNum(c.CacheTokens)))
}

// CostPointerLine is what a LOG prints where the block used to be: one line,
// naming the verb that carries the numbers.
//
// It is a LINE and not a silent omission. Silence would be indistinguishable
// from "this session recorded no cost", and a reader who remembers the block
// would think the log lost it (Rule 15: an absence is STATED). The verb is
// spelled from internal/cliverbs, so a rename of the session family moves this
// instruction with it (Rule 13).
func CostPointerLine() string {
	return "cost is not part of this log — it is the session's RECORDED totals: " +
		cliverbs.Program + " " + cliverbs.Session + " " + cliverbs.Show + " <id>"
}
