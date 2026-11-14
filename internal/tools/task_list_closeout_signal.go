package tools

// task_list_closeout_signal.go — THE LEDGER VIEW'S HALF OF A REFUSED CLOSE-OUT.
//
// INCIDENT (2026-10-11). A tracked dirty file in the MAIN checkout made the
// close-out chain refuse WHOLE: the push never ran, `git branch -d` never ran,
// and the row still read `completed`. Forty-two rows sat in that state for nine
// hours and NOTHING on the surface said so — the refusal was written to the
// row's trail (`finish-cleanup-removal-refused`, task_finish_cleanup.go) and to
// stderr, and stderr belongs to a run that ended hours ago. An operator reading
// `gopi task list` saw a wall of green `completed` rows and had no way to learn
// that some of them were stranded.
//
// WHAT THIS FILE IS, AND WHAT IT IS NOT. It is a READER plus one rendered
// marker. It changes NO gate: the close-out chain still refuses exactly as it
// did (Rule 30's dirty-refusal, Rule 6's step-7 ancestry check, every guard in
// task_finish_cleanup.go stands). The signal is already durable — the chain has
// written it since devdoc/requirements/merge-and-branches.md B2 — so this adds
// no new event, no new column, no new config key and no new file: it makes an
// existing fact visible where the operator already looks.
//
// WHY THE LIST AND NOT ONLY `task show`. `task show` already renders it (the
// row's newest verdict line, tasks.go's taskEventLatestVerdict). That is one
// invocation per row — an operator holding a 41-row backlog has to open 41 of
// them to find the stranded ones, which is the arithmetic that let nine hours
// pass. The list is one render for the whole ledger, so the marker has to be
// there to be worth anything.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// taskListCloseoutMarker is the token the list renders on a row whose LAST
// recorded close-out refused. It is a constant (Rule 1b) so the renderer, the
// tests and a future consumer face share ONE spelling — a reader greps for it.
//
// It deliberately does NOT spell the refusal's own words: the marker says WHICH
// STATE the row is in ("agreed work that the machine declined to collect"), and
// the reason is carried beside it (clipTaskListCloseoutReason) so the row stays
// one line.
const taskListCloseoutMarker = "[closeout-refused]"

// taskListCloseoutReasonLimit bounds the reason quoted on the list line. The
// marker earns its place by being readable at a glance; the FULL sentence is
// already one `task show <id>` away, and the row's own trail keeps every byte
// (Rule 38's split: a rendered figure is not the record).
const taskListCloseoutReasonLimit = 60

// taskListCloseoutTailSeparator sits between the marker and the reason, and it
// is what makes the tail greppable as one token pair rather than as two loose
// words on a crowded line.
const taskListCloseoutTailSeparator = ": "

// closeoutSignalsNewestPerRow is the close-out read's base query, spelled once
// (Rule 1b): the newest event row PER TASK, filtered to the refusal name. It
// groups by task_id and takes MAX(id) rather than matching the event name
// anywhere in the trail — the marker is a STATE, not a scar (a row refused once
// and delivered on a later pass must not keep wearing it).
const closeoutSignalsNewestPerRow = `
		SELECT e.task_id, e.detail
		  FROM task_events e
		  JOIN (SELECT task_id, MAX(id) AS last_id FROM task_events GROUP BY task_id) m
		    ON m.task_id = e.task_id AND m.last_id = e.id
		 WHERE e.event = ?`

// closeoutSignalsViewScopeClause narrows that read to the population a
// `--status` view is about to RENDER — the tasks-table predicate the row walk
// itself applies (`WHERE status = ?`), spelled here as a join so the header
// count and the marked rows come from ONE population. Both spellings live
// beside each other on purpose: they must agree, and a divergence is exactly
// the defect this constant closes.
const closeoutSignalsViewScopeClause = `
		   AND e.task_id IN (SELECT task_id FROM tasks WHERE status = ?)`

// The two SCOPE PHRASES the header can render (Rule 1b): the counted domain is
// part of the sentence, never left for the reader to infer. They are separate
// constants because they are separate facts — one render counted the ledger,
// the other counted the view under it.
const taskListCloseoutScopeLedger = "across the ledger"
const taskListCloseoutScopeView = "in this view"

// The rendered tail's ellipsis, named once (Rule 1b): the same three dots say
// "this is a prefix" everywhere in this render.
const taskListCloseoutEllipsis = "…"

// taskListCloseoutSignals reads, for ONE render, the ids of every row whose
// NEWEST event is a close-out refusal.
//
// THE COUNT HAS A DOMAIN, AND THE DOMAIN IS RENDERED. `--status <st>` walks a
// STATUS-FILTERED population; a count taken over the whole ledger printed above
// it is a different population from the rows under it, and the header used to
// carry no scope clause at all — so it READ as "of the rows on screen"
// (measured 2026-10-12, host-a: `--status failed` header 54 / 0 marked rows
// on screen, `--status cancelled` 54 / 1). Both halves are now the same
// population: the query is scoped by the view's own status predicate and the
// sentence names the domain it counted. Epic 01a12bc0.
//
// ONE QUERY, AHEAD OF THE ROW CURSOR. The ledger openers pin a single
// connection (internal/tools/memory.go SetMaxOpenConns(1) — SQLite serializes
// writers), so a per-row query issued while the row cursor is open would block
// on the connection that cursor holds: a deadlock, not a slow path. This is the
// same constraint failedReissueNote and the parent-status index obey, and the
// same shape: read the whole answer once, then render.
//
// NEWEST, NOT EVER. A row that was refused once and delivered on a later pass
// must NOT keep wearing the marker — the refusal is a state, not a scar. The
// window therefore asks which event is the row's LAST one, not whether the name
// appears anywhere in its trail. (This is why the query groups by task_id and
// takes MAX(id) rather than filtering on the event name alone: 296 rows on this
// host's ledger have the event in their history and only the ones whose trail
// ENDS there are still stranded.)
//
// AN EMPTY OR UNMIGRATED LEDGER ANSWERS "NOTHING TO SAY", never an error: the
// missing-table shape is the same one failedReissueNote tolerates, and a list
// that failed because an optional trail table is absent would trade a silent
// marker for a silent list (Rule 15 — an unreadable trail is reported by the
// absence of the marker, and the list itself still renders).
func taskListCloseoutSignals(ctx context.Context, db *sql.DB, statusFilter string) (map[string]string, error) {
	// THE SCOPE PREDICATE IS THE VIEW'S OWN (Rule 1b — one spelling, here).
	// A FILTERED view joins the tasks table on exactly the status its row walk
	// filters by, so the header count IS the number of marked rows the render
	// is about to print (the property the tests pin). An UNFILTERED render
	// keeps the ledger-global query unchanged — there is no filter to share,
	// and every row the ledger holds is about to be walked.
	query := closeoutSignalsNewestPerRow
	args := []any{finishCleanupRemovalRefusedEvent}
	if statusFilter != "" {
		query += closeoutSignalsViewScopeClause
		args = append(args, statusFilter)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		if isMissingTableErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("task: list: close-out trail: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, detail string
		if err := rows.Scan(&id, &detail); err != nil {
			return nil, fmt.Errorf("task: list: close-out trail: %w", err)
		}
		out[id] = detail
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task: list: close-out trail: %w", err)
	}
	return out, nil
}

// taskListCloseoutSignalSuffix renders the row's close-out tail ("" when the
// row's trail says nothing about a refused close-out).
//
// The tail is the DIAGNOSIS ONLY — it never names a NEXT STEP, and that
// restraint is deliberate: the refusal is one fact with several causes (a dirty
// main checkout, a directory that is gone, a foreign binding), and each has its
// own remedy printed by the arm that refused. A list inventing one remedy for
// all of them would be the same class of defect as the pushback lane's old
// "merge master into it" advice (epic 01a12a54) — a report that is wrong about
// what to do next, printed where the operator is most likely to act on it.
func taskListCloseoutSignalSuffix(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return " " + taskListCloseoutMarker
	}
	// THE ONE STATE A SCANNER MUST NOT HAVE TO CLIP FOR (epic 01a12d5b-bb3a).
	// A refusal whose delivery was WITHHELD has an unfixed answer for the
	// question every operator asks of a refused row — "is my branch on the
	// peer?" — and that answer arrives before the 60-rune clip can cut it. The
	// position is the whole fix: the list line and the row's trail carry the
	// SAME sentence, so a reader who greps either one sees the same words in
	// the same order.
	if i := strings.Index(reason, finishCleanupDeliveryWithheldMarker); i >= 0 {
		return " " + taskListCloseoutMarker + taskListCloseoutTailSeparator +
			finishCleanupDeliveryWithheldMarker + closeoutWithheldStateSeparator +
			clipTaskListCloseoutReason(reason[:i])
	}
	return " " + taskListCloseoutMarker + taskListCloseoutTailSeparator +
		clipTaskListCloseoutReason(reason)
}

// taskListCloseoutNote is the render's ONE header sentence for close-out
// refusals, or "" when there are none — the same shape FailedReissueNote takes,
// and for the same reason: a zero must add NOTHING to the output, so "no
// refusals" and "this render never asked" are told apart by the absence of a
// line rather than by an empty one.
//
// THE COUNT IS IN THE HEADER; THE ROWS CARRY THE MARKER AND THE REASON. An
// operator scanning a 41-row backlog needs the total first (that is the number
// that was invisible for nine hours) and then the rows, and the marker is what
// survives a `grep`.
//
// `scope` is MANDATORY and is one of the two scope phrases above — the header
// never renders a count without saying WHICH population it counted (epic
// 01a12bc0: an unscoped count above a status-filtered row walk read as "of the
// rows below", and on this host it was not).
func taskListCloseoutNote(n int, scope string) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(taskListCloseoutNoteFmt, n, scope, taskListCloseoutMarker) + "\n"
}

// taskListCloseoutNoteFmt is the sentence above, spelled once (Rule 1b). It
// states TWO things it can prove and nothing else:
//
//   - the POPULATION it counted (%d + the scope phrase: this view, or the whole
//     ledger), which is the domain of the number; and
//   - what it means for a row (%s, the marker token, and its reason on the row).
//
// IT DELIBERATELY DOES NOT SAY WHICH HALF OF THE CLOSE-OUT CHAIN STOPPED. The
// sentence used to read "the delivery chain stopped before the push, so the
// branch was NOT removed" — a CONSEQUENCE this reader cannot see, and one the
// ledger contradicts: taskListCloseoutSignals keys on ONE event name
// (finishCleanupRemovalRefusedEvent), written by BOTH the removal-refusal arm
// and the removal-not-reached arm (task_finish_cleanup.go), so this render knows
// exactly one thing — the row's newest recorded verdict is a refusal. WHICH half
// refused, and what survived it, are in the reason text on the row, and each
// arm's own remedy is printed by the arm that refused. Measured on this host:
// 49/49 marked rows carried the SAME reason — the gone-directory one with its
// retired delivery clause (`finishCleanupDeliveryClaimedPrefix` is what that
// clause reads now) — i.e. the push HAD run and the removal was an idempotent
// no-op, so the old clause was false for every row under it.
const taskListCloseoutNoteFmt = "close-out refused on %d row(s) %s — each affected row is marked %s with its reason:"

// clipTaskListCloseoutReason shortens a refusal sentence to the list's budget,
// on a RUNE boundary (a byte-wise cut would split a multi-byte character and
// put invalid UTF-8 on the terminal).
func clipTaskListCloseoutReason(reason string) string {
	reason = strings.Join(strings.Fields(reason), " ")
	r := []rune(reason)
	if len(r) <= taskListCloseoutReasonLimit {
		return reason
	}
	return string(r[:taskListCloseoutReasonLimit]) + taskListCloseoutEllipsis
}
