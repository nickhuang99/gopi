// Package requestusage is the SINGLE definition of WHAT QUANTITY a session
// log's `request` row carries in its four usage counters (prompt_tokens,
// cache_hit, cache_miss, completion_tokens).
//
// WHY THIS IS A PACKAGE AND NOT A LOCAL RULE (PROJECT.md Rule 14). The quantity
// is WRITTEN by internal/agent — two writers, on two execution paths — and READ
// by internal/gopilog, which feeds both summary surfaces (`gopi session stats`
// and `gopi session log`). gopilog imports agent, so agent cannot hold a value
// gopilog must read: it lives in a leaf package with zero project imports,
// exactly like internal/sessionevent and internal/toolnames.
//
// THE DEFECT THIS REPLACES (measured 2026-11-01, epic 01a195b0). One event
// shape carried two different quantities and nothing on the row said which:
//
//   - the TASK path (internal/agent/loop.go, runEngine) snapshots the
//     CostTracker before each send and logs the DIFFERENCE — a per-request
//     increment;
//   - the CHAT path (internal/agent/chat_ops.go, and loop.go's Chat turn)
//     logged the CostTracker's CUMULATIVE totals — the running sum since that
//     run segment began.
//
// Both readers summed the column as if it were increments, so a chat session's
// headline was the sum of its own running totals: session
// d6120d04-98fa-454d-a2f0-51ea7c661956 printed 219,191 prompt tokens against a
// true 109,705, and 34bb9adc-0618-4e05-b849-6bd15757dec6 printed 393,314,088
// against 8,086,768 (48.6x). The raw HTTP log of the same sessions summed to
// the true value in both cases.
//
// TAKING THE SESSION'S LAST READING INSTEAD IS NOT THE FIX EITHER. A resumed
// session restarts its CostTracker from zero, and chat session
// 66243525-16ab-4ce7-9133-84d74c539840 holds THREE run segments whose
// cumulative readings restart at 142,860 and 137,568 after the previous
// segment had reached 44,155,810 and 40,741,990. The counted value of such a
// log is the SUM OVER SEGMENTS of each segment's LAST reading — neither the
// session's last reading (310,935,011 prompt of 395,832,811) nor the column sum
// (356,995,277,119).
package requestusage

// RequestUsageBase is the QUANTITY a `request` row's four counters carry. A
// writer that knows this package DECLARES it on the row (Tagged/Base); a row
// written before the marker existed is classified from its own log by
// Classify, and that fact is reported rather than hidden (Rule 15).
type RequestUsageBase string

const (
	// BaseCumulative means the counters are the writer's RUNNING TOTALS since
	// its run segment began: row i's prompt_tokens already includes rows
	// 0..i-1. Summing such a column double-counts, so every reader folds it
	// with Counted — the LAST reading of each segment counts, and a segment
	// that restarts at a small value contributes the value it reached.
	BaseCumulative RequestUsageBase = "cumulative"

	// BaseDelta means the counters are THIS request's own increment: row i's
	// prompt_tokens counts nothing but request i, so the column sum IS the
	// total. This is the task path's quantity and what the counters have meant
	// on that path since it was written — its readings are unchanged by this
	// package.
	BaseDelta RequestUsageBase = "delta"

	// BaseUnknown means the quantity was NOT established: the row carries no
	// marker and the log offers no evidence (Classify). A reader must SAY so
	// rather than fold an unproven assumption into a headline (Rule 15).
	BaseUnknown RequestUsageBase = ""
)

// Wire keys of the marker written on a `request` row beside the four counters.
// They live here (Rule 14) because the writer (internal/agent) and the reader
// (internal/gopilog) are separate packages.
const (
	// FieldTaggedUsage is the presence marker: true on every row a writer that
	// knows this package produced.
	FieldTaggedUsage = "tagged_usage"
	// FieldUsageBase names the quantity (BaseCumulative / BaseDelta).
	FieldUsageBase = "usage_base"
)

// Reading is one `request` row's four counters as the reader parsed them.
type Reading struct {
	Prompt     int
	CacheHit   int
	CacheMiss  int
	Completion int
}

// Total is a session's counted usage: what the log's rows add up to once each
// row is read under its own quantity, for all four counters (so a render's
// hit/miss split belongs to the same fold as its prompt total and the two never
// disagree).
type Total struct {
	Prompt     int
	CacheHit   int
	CacheMiss  int
	Completion int
	// Segments is how many cumulative RUN segments the fold spanned (0 for a
	// pure-delta log). It is the SPAN of the numbers above, stated because a
	// resumed session's totals are a sum over its runs (Rule 15).
	Segments int
	// Unknown counts the rows that were NOT folded because their quantity could
	// not be established. A render that prints these totals while Unknown > 0
	// must say how many rows it left out.
	Unknown int
}

// Tagged is a reading plus the quantity it is to be read under, and the RUN
// SEGMENT it was accumulated in (Seg >= 0; the reader numbers segments from the
// log's own `session_start` rows, one per run).
//
// SEGMENT AND THE VALUE FALL ARE TWO INDEPENDENT WITNESSES of the same fact — a
// resumed run restarting its counters — and the fold flushes on EITHER: a
// recorded segment change is the writer's own statement, and a fall is what a
// log written before the marker has left as evidence. Using both means a
// restart whose first reading happens to exceed the previous segment's last one
// is still split correctly.
type Tagged struct {
	Reading
	Base RequestUsageBase
	Seg  int
}

// Classify reads an UNTAGGED log's quantity from the log itself. Every session
// log written before this package existed is in that state, so the answer is
// EVIDENCE, in this order — and the evidence is measured, not plausible:
//
//  1. THE RECORDED TOTAL, exactly. A run records `session_end.total_tokens`,
//     and each of the two quantities implies a DIFFERENT total for the same
//     rows: increments sum to the column sum, running totals sum to the sum of
//     each segment's last reading. When exactly one of the two equals the
//     recorded total, that one is the quantity — it cannot be a coincidence
//     (the two sums differ by orders of magnitude on every real chat log: 2x on
//     d6120d04, 48.6x on 34bb9adc, 900x on 66243525). Both readings are taken
//     over ALL FOUR counters, so the hit/miss split is proven with the total.
//  2. A FALL INSIDE A RUN. When neither matches (a log holding several runs
//     where an earlier run's end was never written, or a trimmed log), a
//     running total still cannot go DOWN while an increment rises and falls with
//     each request's own size. The test SKIPS the first row of each run segment
//     (boundary[i] reports that a session_start/session_end row sat between row
//     i-1 and row i), because a resumed run's counters restart low and that
//     restart is not evidence of anything.
//  3. Otherwise CUMULATIVE — the safe direction to guess in, because a
//     multi-row increment column with no fall and no matching total is a shape
//     no writer produced, while a chat log's every other shape is this one. A
//     single-row log is unaffected either way: its only reading IS its total.
func Classify(rows []Reading, boundary []bool, endTotals *int) (RequestUsageBase, string) {
	if len(rows) == 0 {
		return BaseUnknown, ClassifyReasonNoRows
	}
	if endTotals != nil {
		deltaSum := 0
		for _, r := range rows {
			deltaSum += r.Prompt + r.Completion
		}
		cumSum := cumulativeSum(rows, boundary)
		deltaExact := deltaSum == *endTotals
		cumExact := cumSum == *endTotals
		switch {
		case deltaExact && !cumExact:
			return BaseDelta, ClassifyReasonRecordedTotalsDelta
		case cumExact && !deltaExact:
			return BaseCumulative, ClassifyReasonRecordedTotalsCumulative
		}
	}
	if fallsInsideRun(rows, boundary) {
		return BaseDelta, ClassifyReasonDecrease
	}
	return BaseCumulative, ClassifyReasonMonotone
}

// cumulativeSum is the value the rows imply IF they are running totals: the sum
// of each run segment's last reading. It exists so Classify can compare two
// MEASUREMENTS against one recorded number instead of reasoning about shapes.
func cumulativeSum(rows []Reading, boundary []bool) int {
	var (
		total int
		last  Reading
		open  bool
	)
	for i, r := range rows {
		if open && ((i < len(boundary) && boundary[i]) || falls(r, last)) {
			total += last.Prompt + last.Completion
		}
		last = r
		open = true
	}
	if open {
		total += last.Prompt + last.Completion
	}
	return total
}

// fallsInsideRun reports whether any counter drops between two rows of the SAME
// run segment — the one shape a running total cannot produce.
func fallsInsideRun(rows []Reading, boundary []bool) bool {
	for i := 1; i < len(rows); i++ {
		if i < len(boundary) && boundary[i] {
			continue
		}
		if falls(rows[i], rows[i-1]) {
			return true
		}
	}
	return false
}

// falls reports whether cur is lower than prev on any of the four counters.
func falls(cur, prev Reading) bool {
	return cur.Prompt < prev.Prompt || cur.CacheHit < prev.CacheHit ||
		cur.CacheMiss < prev.CacheMiss || cur.Completion < prev.Completion
}

// The reasons Classify's answer rests on, so a surface can print WHY a number
// was read the way it was (Rule 15: a derived quantity carries its ground).
const (
	// ClassifyReasonNoRows is a log with no `request` row at all.
	ClassifyReasonNoRows = "no request rows"
	// ClassifyReasonRecordedTotalsDelta is proof from the log's own session_end:
	// only the increment reading reproduces the total it recorded.
	ClassifyReasonRecordedTotalsDelta = "increments: the rows sum to the recorded session total"
	// ClassifyReasonRecordedTotalsCumulative is the same proof for the other
	// quantity: only the running-total reading reproduces it.
	ClassifyReasonRecordedTotalsCumulative = "running totals: each run's last reading sums to the recorded session total"
	// ClassifyReasonDecrease is a fall inside a run segment — a running total
	// cannot do that.
	ClassifyReasonDecrease = "increments: a counter falls inside a run segment"
	// ClassifyReasonMonotone is the cumulative default.
	ClassifyReasonMonotone = "running totals: no counter falls inside a run segment"
)

// Counted folds the rows into the session's counted Total.
//
// The fold, stated once so no surface re-derives it:
//
//   - a DELTA row contributes its own value;
//   - a CUMULATIVE row REPLACES the pending reading of its segment, so a
//     segment contributes its LAST reading rather than the sum of its running
//     totals. A segment that restarts at a small value contributes the value it
//     reached — not the difference from the previous segment — because the
//     writer's counters restarted and the work in between was already counted
//     by the earlier segment's own last reading.
//
// A backwards move inside a cumulative sequence is NEW EVIDENCE THAT A NEW RUN
// BEGAN (a CostTracker never decreases inside one run), which is exactly how a
// resumed session's three segments are found without a segment id on the row.
func Counted(rows []Tagged) Total {
	var (
		out        Total
		open       bool
		last       Reading
		lastSeg    int
		flushSegmu = func() {
			if !open {
				return
			}
			out.Prompt += last.Prompt
			out.CacheHit += last.CacheHit
			out.CacheMiss += last.CacheMiss
			out.Completion += last.Completion
			out.Segments++
			open = false
			last = Reading{}
		}
	)
	for _, r := range rows {
		switch r.Base {
		case BaseCumulative:
			if open && (r.Seg != lastSeg || r.Prompt < last.Prompt || r.CacheHit < last.CacheHit ||
				r.CacheMiss < last.CacheMiss || r.Completion < last.Completion) {
				flushSegmu()
			}
			open = true
			last = r.Reading
			lastSeg = r.Seg
		case BaseDelta:
			out.Prompt += r.Prompt
			out.CacheHit += r.CacheHit
			out.CacheMiss += r.CacheMiss
			out.Completion += r.Completion
		default:
			// An UNCLASSIFIED row is not folded silently into a headline: a
			// guess is not a measurement, so the row is counted as unknown and
			// the caller reports it (out.Unknown) instead of picking a quantity.
			out.Unknown++
		}
	}
	flushSegmu()
	return out
}

// Deltas returns each row's OWN usage (the per-request increment) together with
// the session's counted Total.
//
// WHY A CALLER WANTS THE PER-REQUEST FORM. Every per-round surface (`session
// stats --rounds`, `session log`, the trim-cliff chain, the per-round cost
// curve) asks about ONE request, and on a cumulative log the recorded reading is
// not that: it is the running total up to and including it. Differencing inside
// a segment recovers the request's own usage — and it is exact, because the
// writer accumulates exactly these four counters. A segment's FIRST row is its
// own usage (a fresh run's counters start at zero), which is also why the deltas
// of a resumed log sum to out.Prompt / out.Completion rather than to the column
// sum. A DELTA row is already in that form and passes through untouched, so a
// task-path log's per-round readings are exactly what they were.
//
// An UNCLASSIFIED row (BaseUnknown) is passed through unchanged and counted in
// out.Unknown: it is printed as recorded, and no headline silently folds it in.
func Deltas(rows []Tagged) ([]Reading, Total) {
	out := make([]Reading, len(rows))
	var (
		tot      Total
		open     bool
		last     Reading
		lastSeg  int
		flushSeg = func() {
			if !open {
				return
			}
			tot.Prompt += last.Prompt
			tot.CacheHit += last.CacheHit
			tot.CacheMiss += last.CacheMiss
			tot.Completion += last.Completion
			tot.Segments++
			open = false
			last = Reading{}
		}
	)
	for i, r := range rows {
		switch r.Base {
		case BaseCumulative:
			if open && (r.Seg != lastSeg || r.Prompt < last.Prompt || r.CacheHit < last.CacheHit ||
				r.CacheMiss < last.CacheMiss || r.Completion < last.Completion) {
				flushSeg()
			}
			prev := Reading{}
			if open {
				prev = last
			}
			out[i] = Reading{
				Prompt:     r.Prompt - prev.Prompt,
				CacheHit:   r.CacheHit - prev.CacheHit,
				CacheMiss:  r.CacheMiss - prev.CacheMiss,
				Completion: r.Completion - prev.Completion,
			}
			open = true
			last = r.Reading
			lastSeg = r.Seg
		case BaseDelta:
			out[i] = r.Reading
			tot.Prompt += r.Prompt
			tot.CacheHit += r.CacheHit
			tot.CacheMiss += r.CacheMiss
			tot.Completion += r.Completion
		default:
			out[i] = r.Reading
			tot.Unknown++
		}
	}
	flushSeg()
	return out, tot
}
