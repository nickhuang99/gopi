package notifyfeed

// unhandled.go — "which FAILURES has nobody dealt with?" read off the event
// STREAM rather than the ledger.
//
// WHY FROM THE STREAM. The ledger's own answer already exists —
// internal/tools.FailedNeedsReissue asks it of the `tasks` table, and its
// predicate (failed, no successor row, no `resolved` tag) is the ledger's
// verdict. This file answers the CONSUMER's version of the same question: a
// subscriber that only ever sees the event stream (a PA loop, an on-call
// script, a peer node reading a forwarded sink) must be able to reach it too,
// and it must be the SAME answer. So the predicate is not re-invented here: this
// file folds the stream into "what is each row's terminal verdict, and has a
// later event for that row answered it", and the two halves are stated so they
// cannot silently disagree.
//
// THE STREAM'S ANSWER IS DELIBERATELY WEAKER, and the difference is the honest
// part. The ledger predicate counts a row as handled when it has a SUCCESSOR
// ROW or a `resolved` TAG — facts the events never carry (no notify payload
// names a parent_task_id or a tag set... the `tags` field exists on transition
// payloads but a later tag edit fires no event at all). So a stream-only
// consumer can see a failure and can see a LATER success for the same id, and
// it CANNOT see the two ways the operator retires a failure without changing its
// status. It therefore reports what it can prove and names the gap in its own
// output rather than implying a completeness it does not have.

import (
	"fmt"
	"sort"
	"time"
)

// FailedEntry is one row whose LAST terminal event in the stream is a failure,
// with the evidence a subscriber needs to act: which event said so, when, and
// the payload's own failure digest.
type FailedEntry struct {
	// TaskID is the row (or, for a session-level alarm, the session id).
	TaskID string
	// Event is the event that left it failed.
	Event string
	// Timestamp is that event's own delivery time.
	Timestamp string
	// Summary is the row's summary at the time of the event.
	Summary string
	// Result is the payload's `result` — the failure digest the ledger's audit
	// trail carries (an empty one means the row's own detail is the place to
	// look, not that it succeeded).
	Result string
}

// FailedOptions narrows the report.
type FailedOptions struct {
	// SinceUnix drops events older than this (0 = no bound). It is how a
	// consumer asks "what failed since I last looked" without re-deriving the
	// cursor's job.
	SinceUnix int64
	// SessionAlarms includes the session-level alarms (loop-stuck,
	// session-wallclock-exceeded, …) whose `task_id` is a session id. They are
	// real failures of a RUN, not of a ledger row, so a consumer that wants
	// ROWS excludes them.
	SessionAlarms bool
}

// terminalSuccessEvents are the events that CLEAR a row's failure debt: a row
// that later reached one of these has been dealt with (reopened and finished,
// or verified). They are the stream's own vocabulary, not a second invention —
// each is a ledger status whose transition produces exactly this event name.
var terminalSuccessEvents = map[string]bool{
	"completed": true,
	"verified":  true,
}

// failureEvents are the events that PUT a row in the failed column. `failed` is
// the terminal verdict; the two alarms are failures of a RUN rather than of a
// row and are only counted when the caller asks for them.
var failureEvents = map[string]bool{
	"failed": true,
}

// sessionFailureEvents are the run-level failure alarms.
var sessionFailureEvents = map[string]bool{
	"stale":                      true,
	"session-wallclock-exceeded": true,
	"loop-stuck":                 true,
	"loop-stuck-toolful":         true,
	"loop-stuck-observation":     true,
	"build-failed":               true,
}

// UnhandledFailures folds a decoded stream into the rows whose most recent
// terminal event is a failure. The stream is scanned in order, so "most recent"
// is decided by POSITION (the sink is append-ordered) rather than by comparing
// timestamps — the same reason the cursor is an offset and not a clock.
func UnhandledFailures(records []Record, opts FailedOptions) []FailedEntry {
	// state holds, per id, the last terminal-ish event seen and its record.
	type state struct{ entry FailedEntry }
	seen := map[string]state{}
	order := []string{}
	for _, r := range records {
		event := r.Event()
		id := r.TaskID()
		if id == "" {
			continue
		}
		isFailure := failureEvents[event] || (opts.SessionAlarms && sessionFailureEvents[event])
		isSuccess := terminalSuccessEvents[event]
		if !isFailure && !isSuccess {
			continue
		}
		if _, ok := seen[id]; !ok {
			order = append(order, id)
		}
		if isSuccess {
			// The debt is cleared: drop the entry rather than keeping a stale
			// failure whose remedy already happened.
			delete(seen, id)
			continue
		}
		seen[id] = state{entry: FailedEntry{
			TaskID:    id,
			Event:     event,
			Timestamp: r.Timestamp(),
			Summary:   recordString(r.Payload, fieldSummary),
			Result:    recordString(r.Payload, fieldResult),
		}}
	}
	out := make([]FailedEntry, 0, len(seen))
	for _, id := range order {
		s, ok := seen[id]
		if !ok {
			continue // cleared by a later success
		}
		if opts.SinceUnix > 0 && eventUnix(s.entry.Timestamp) < opts.SinceUnix {
			continue
		}
		out = append(out, s.entry)
	}
	// Sink order is append order, which is the order an operator wants to read
	// them in — newest LAST. The report prints newest-first for the same reason
	// `gopi task list` does, and reversing here keeps the fold's own ordering
	// rule in one place.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	return out
}

// FailedReportHead is the report's first line: how many ids the stream says are
// failed with no later success behind them. It says id, not row, because the
// stream's failure column is dominated by ids that are NOT ledger rows —
// measured on the operator's sink 2026-10-01: of 11,685 such ids, 11,508 never
// appear in the `tasks` table at all (years of smoke/fixture runs). Calling them
// rows would be a claim the stream cannot support.
const FailedReportHead = "notify: %d id(s) whose latest terminal event in this stream is a failure"

// FailedReportGapNote is the honesty line (Rule 26's corollary applied to a
// consumer): the stream CANNOT see the two ways an operator retires a failure
// without changing its status, and it also carries ids that were never ledger
// rows at all (years of smoke/fixture runs) — so the report says what it is
// instead of implying it counted the ledger.
const FailedReportGapNote = "notify: this is the STREAM's count, deliberately not the ledger's. The stream cannot see the two ways " +
	"a failure stops being a debt without its status changing — a `resolved` tag and a successor row both fire NO further event — " +
	"and it also carries ids that were never ledger rows (historic smoke/fixture runs). `gopi task list` renders the ledger's own " +
	"authoritative count; this report is what a stream-only consumer (a peer, a forwarded sink) can still prove."

// FormatFailedEntry renders one entry on one line, greppable and copy-pasteable
// (the id first so `awk '{print $2}'` gets it).
func FormatFailedEntry(e FailedEntry) string {
	line := fmt.Sprintf("%s\t%s\t%s\t%s", e.TaskID, e.Event, e.Timestamp, firstLine(e.Summary))
	if e.Result != "" {
		line += "\t" + firstLine(e.Result)
	}
	return line
}

// eventUnix parses a payload's RFC3339 timestamp into unix seconds; an
// unparseable one is 0, which the `SinceUnix` bound then treats as "outside the
// window" only when a bound was asked for — a malformed timestamp never makes
// an entry silently disappear from an unbounded report.
func eventUnix(ts string) int64 {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return 0
	}
	return t.Unix()
}
