package tools

// task_transfer_eligibility.go — the ONE two-way predicate of a CROSS-LEDGER
// row transfer (epic task-pull, deliverable ②).
//
// WHAT PROBLEM THIS FILE ANSWERS. `gopi task receive --peer A` moves ONE
// queued row off a peer's ledger onto this one and runs it here. Moving a row
// is not a copy: the source ledger loses it. Two things can therefore make a
// move WRONG, and neither is visible from the source ledger alone:
//
//	① THE ROW ITSELF is not work this dispatcher may take. The peer's own
//	   claimability definition answers that (status pending + taskRowClaimable
//	   + taskClaimOrderBy) — and it is REUSED here, never re-derived (Rule 14):
//	   the selection is the peer's, so a row this predicate calls eligible is a
//	   row the peer's own `task next` would have taken.
//
//	② THE ROW'S EDGE. Rule 33 parks a pending row whose parent is not
//	   released FOREVER (no override, no cascade, no timeout: a cancelled or
//	   failed parent never releases). A row that carries a parent cannot
//	   resolve that edge in a ledger that does not hold the parent — so it is
//	   NOT transferable, and the peer-side SELECT never even offers one.
//	   The sharper arm is the REVERSE edge: a row whose CHILD is still live
//	   (pending / claimed / in_progress) in the source ledger. Moving the PARENT
//	   away leaves that child pointing at a row the ledger no longer holds —
//	   `taskParentGateDenied` calls that arm "missing from the ledger" and
//	   parks the child permanently, with no local error and no way back (there
//	   is no override flag to un-park it). So a row is transferable only when
//	   it is BOTH claimable there AND childless-here: the predicate is
//	   two-way, and BOTH halves must hold.
//
// The reverse half is a fact about the SOURCE ledger's children, which is why
// the dependency index is handed in as a value (TaskTransferDependency): the
// caller reads it from the ledger it is moving rows OUT OF, and this file
// never opens a ledger. That keeps the predicate pure over row fields (the
// shape Rule 14 asks for: one predicate, two readers) and lets the same
// predicate run on the initiator or on the peer — it is the SAME judgement
// whichever side asks.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/nick/gopi/internal/tasktags"
)

// taskTransferLiveChildStatuses is the LIVE set of the dependency edge: a row
// in one of these states still waits for its parent, so moving the parent away
// parks it (Rule 33's release set is exactly {completed, verified} — every
// status NOT in it is a row that a pending child can still be waiting for, and
// the searchable, operator-visible subset of those is this one). terminalTaskStatus
// members (completed/verified/failed/cancelled/superseded/reopened) are NOT
// live: a finished child has nothing left to wait for.
//
// Rule 1b/13: the SQL fragment and the Go predicate are the two faces of ONE
// list, so they are DERIVED from these constants rather than typed twice.
var taskTransferLiveChildStatuses = []string{taskStatusPending, taskStatusClaimed, taskStatusInProgress}

// taskTransferLiveChildSQL renders the membership test for the live-child
// query from the SAME list the predicate documents.
func taskTransferLiveChildSQL() string {
	ph := make([]string, 0, len(taskTransferLiveChildStatuses))
	for range taskTransferLiveChildStatuses {
		ph = append(ph, "?")
	}
	return "(" + strings.Join(ph, ", ") + ")"
}

// The rendered reasons — ONE spelling each (Rule 1b), because the peer-side
// selector, the initiator's refusal and the tests all read the same sentence.
const (
	taskTransferWhyNilRow       = "no row was handed in"
	taskTransferWhyNotPending   = "it is %s, not %s — only QUEUED work moves (a row already being worked, or already finished, is not a transfer candidate)"
	taskTransferWhyParented     = "it carries parent_task_id %s — a dependency edge cannot resolve in a ledger that does not hold the parent (Rule 33 parks such a row forever)"
	taskTransferWhyLiveChildFmt = "row %s (%s) still depends on it HERE — moving it away would leave that child pointing at a parent this ledger no longer holds, which Rule 33 parks permanently (releases only on %s); finish or cancel the child first"
	taskTransferWhyUnnameable   = "the claimability predicate refused it and no narrower reason is derivable here (the ROW CHANGED between selection and this check — it was claimable when the candidate list was built)"
)

// TaskTransferDependency is the SOURCE ledger's answer to "does anything still
// have to wait for this row?" — the reverse half of the two-way predicate.
// Zero value = no live child = the row may travel.
type TaskTransferDependency struct {
	// Live reports that a child of this row is still waiting for it.
	Live bool
	// ChildID / ChildStatus name the ONE such child (the youngest by claim
	// order), so the refusal an operator reads names the row to deal with
	// instead of a category (Rule 15b: a failure names itself).
	ChildID     string
	ChildStatus string
}

// TaskTransferEligible is the ONE judgement of whether a row may be handed
// over to another ledger, with the reason it may not ("" when it may). It is
// the push half's gate and the pull half's gate — the CALLER decides which
// direction it is asking about; the predicate does not care.
//
// skipSet is the receiving dispatcher's tag exclusion (the same set the
// candidate's future claimer uses): a row that dispatcher would refuse must
// never be handed over, or the move strands it — flagged and cancelled on the
// source, then unclaimable at the destination (the pa-queue trap this argument
// exists to close). Pass nil to ask the question with no exclusions.
func TaskTransferEligible(t *Task, dep TaskTransferDependency, skipSet map[string]bool) (bool, string) {
	if t == nil {
		return false, taskTransferWhyNilRow
	}
	id := strings.TrimSpace(t.TaskID)
	// ① ORDER AND STATUS are asked here as well as in the SQL, because this is
	// the flag's TOCTOU re-check too (selection is not atomic with the handover
	// write): the row that comes back must still BE the row that was selected.
	if status := strings.TrimSpace(t.Status); status != taskStatusPending {
		return false, fmt.Sprintf(taskTransferWhyNotPending, status, taskStatusPending)
	}
	if parent := strings.TrimSpace(t.ParentTaskID); parent != "" {
		return false, fmt.Sprintf(taskTransferWhyParented, parent)
	}
	// ② THE FORWARD HALF IS THE PEER'S OWN PREDICATE, verbatim (Rule 14) — the
	// nil parent-status index is not a shortcut: a parentED row is refused by it
	// outright, which is the same verdict the arms above state in words. The
	// `why` below is PROSE for a refusal that predicate already decided; it
	// never re-decides (the verdict is the predicate's alone), and every path
	// that does not name a narrower cause reports exactly that (Rule 15: an
	// unnamed refusal is still a refusal, but never an invented reason).
	if !taskRowClaimable(t.TaskID, t.Tags, t.Payload, taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs), nil, skipSet) {
		if why := taskTransferClaimabilityWhy(t, skipSet); why != "" {
			return false, why
		}
		return false, taskTransferWhyUnnameable
	}
	// ③ THE REVERSE HALF: nothing here may still be waiting for it.
	if dep.Live {
		return false, fmt.Sprintf(taskTransferWhyLiveChildFmt, dep.ChildID, dep.ChildStatus, taskParentReleaseSetLabel())
	}
	_ = id
	return true, ""
}

// taskTransferClaimabilityWhy renders the narrowest cause a refusal by
// taskRowClaimable can be traced to, or "" when none of the named arms applies.
//
// IT IS A REPORT, NOT A SECOND GATE: every arm below is the same test
// taskRowClaimable already ran, asked again only to NAME it, and the caller
// uses the answer for PROSE alone. A row the predicate refused with no named
// arm falls through to the honest "no narrower reason" sentence (Rule 15b) —
// never to a guessed cause, and never to "eligible".
func taskTransferClaimabilityWhy(t *Task, skipSet map[string]bool) string {
	if matchesSkipTags(t.Tags, skipSet) {
		return fmt.Sprintf("it is tagged %q, which the receiving dispatcher SKIPS — it would arrive unclaimable and stranded here (the skip set the caller handed in)", t.Tags)
	}
	if strings.TrimSpace(t.Payload) == "" {
		return "it carries no payload — a payload-less row is a ledger entry, not dispatchable work (no dispatcher will ever take it)"
	}
	if resolveRowIsMergerOwned(t.Tags) {
		return "it is a MERGER-RESOLVE row (merge-conflict + auto-spawn) — it belongs to the merger's own lane on the ledger that enqueued it, and is never worker-dispatchable"
	}
	// The fixture arm is asked here too (Rule 14: ONE tag value, every reader):
	// a fixture that travelled to a peer would be claimed THERE instead — the
	// identical LLM round, one host over — so the transfer leg must refuse it
	// for its own reason rather than fall through to "no narrower reason".
	if taskHasTagToken(t.Tags, tasktags.FixtureTag) {
		return "it is a FIXTURE row (the reserved " + tasktags.FixtureTag + " tag): queue decoration, never work — handing it over would move the decoration to a peer's queue, where the same predicate refuses it"
	}
	if _, reason := taskParentGateDenied(t.TaskID, t.Tags, t.Payload, taskParentIDsOf(t.ParentTaskID, t.ParentTaskIDs), nil); reason != "" {
		return strings.TrimSpace(reason)
	}
	return ""
}

// TaskTransferLiveChild finds the ONE live dependent of a row in the ledger
// `db` belongs to — the reverse half of TaskTransferEligible, read where the
// row currently lives (the SOURCE ledger, i.e. the peer's, for a pull).
//
// The query narrows in SQL and the answer is the YOUNGEST live child by the
// claim order, so the refusal names the row an operator would look at first
// rather than an arbitrary one.
func TaskTransferLiveChild(ctx context.Context, db *sql.DB, taskID string) (TaskTransferDependency, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return TaskTransferDependency{}, nil
	}
	args := []any{taskID}
	for _, st := range taskTransferLiveChildStatuses {
		args = append(args, st)
	}
	query := `SELECT task_id, status FROM tasks WHERE parent_task_id = ? AND status IN ` +
		taskTransferLiveChildSQL() + taskClaimOrderBy
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return TaskTransferDependency{}, fmt.Errorf("transfer: live-child scan: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return TaskTransferDependency{}, fmt.Errorf("transfer: live-child scan: %w", err)
		}
		return TaskTransferDependency{}, nil
	}
	dep := TaskTransferDependency{Live: true}
	if err := rows.Scan(&dep.ChildID, &dep.ChildStatus); err != nil {
		return TaskTransferDependency{}, fmt.Errorf("transfer: live-child scan: %w", err)
	}
	return dep, nil
}

// taskTransferParentlessSQL is the candidate pre-filter's edge arm: a
// transferable row never carries a parent (the reverse of the same rule — its
// edge could not resolve in the receiving ledger).
const taskTransferParentlessSQL = `(parent_task_id IS NULL OR parent_task_id = '')`

// ============================================================================
// THE ARRIVAL HALF — the same criterion asked from the receiving side
// ============================================================================
//
// The predicate above answers "may this row LEAVE the ledger it is in". For
// `gopi task pull` / `task receive` the question is a different one, and the
// epic that added this half exists precisely because the two were conflated:
//
//	THE ARRIVAL DIRECTION IS ITS OWN QUESTION, NOT THE SAME ONE MIRRORED. A
//	pull MOVES NOTHING on the peer — it does not cancel the peer's row, and it
//	writes nothing over an existing row's edge (syncDeltaMergeRow backfills an
//	absent edge and never clobbers a present one) — so the push leg's REVERSE
//	half (live dependents) has no counterpart here: there is no local dependent
//	a pull could strand, and asking the question would refuse the designed
//	chain flow (pull the parent, then pull each child). What a pull CAN still
//	do to THIS ledger is land a row carrying an edge that Rule 33 can never
//	release, and then the merge REFUSES it and ROLLS IT BACK: the row is not
//	HERE, the backfill that would have written the edge was rolled back whole,
//	and the pull reports its counters about a row that did not land. `pull T`
//	written that way is a one-shot — re-running it once the parent is released
//	reads the PEER's copy again, which carries no backfill, so asking again
//	cannot close the gap.
//
// The arrival criterion therefore judges the row's OWN edge against THIS
// ledger and demands the edge be writeable, using the family's existing verdict
// (syncParentUnresolvable over syncParentVerdict and taskParentReleaseStatuses)
// so that a criterion, an apply and a rollback can never disagree about one
// edge.
//
// WHAT IS DELIBERATELY NOT HERE. No switch, no env var, no settings key
// (Rule 35): the protection is not optional, and the way past it is to handle
// the parent, not to disable the check. No cascade, no reparent, no override —
// Rule 33's forbidden ground. And no I/O policy of its own: the view below IS
// the ledger read, injected so the caller owns the connection and the ledger
// choice.

// TaskDependencyView is the READ the criterion needs. Both directions are
// asked of the SAME view, so neither half can be judged against a different
// ledger than the other.
type TaskDependencyView interface {
	// TaskByID returns the row with this id, or nil when the ledger holds no
	// such row (absence is an answer, not an error).
	TaskByID(id string) (*Task, error)
	// StatusesByID answers the status of each NAMED parent in ONE read — the
	// arrival half's consultation. A batch arrives as a set of ids, so asking
	// them one at a time would need a second interface. An id the ledger does
	// not hold is simply absent from the map: absence stays an answer.
	StatusesByID(ids []string) (map[string]string, error)
}

// NewTaskLedgerDependencyView returns the view over the ledger the caller
// named ("" = this process's own ledger), resolved through syncRowWithLedger —
// the ONE pin both row legs and their gates already use, so "--db PATH" means
// the same ledger on every half of a verb.
func NewTaskLedgerDependencyView(dbPath string) TaskDependencyView {
	return taskLedgerDependencyView{dbPath: dbPath}
}

// taskLedgerDependencyView is the production view: reads over the pinned ledger.
type taskLedgerDependencyView struct{ dbPath string }

// TaskByID reads one row (nil = absent).
func (v taskLedgerDependencyView) TaskByID(id string) (*Task, error) {
	var out *Task
	err := syncRowWithLedger(v.dbPath, func() error {
		row, rerr := taskStatusDirect(id)
		if rerr != nil {
			return rerr
		}
		out = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// StatusesByID answers each named id's status in ONE statement (the arrival set
// is the handful of ids this criterion asks about, so the SQL list is bounded
// by the argument we were handed — no client-side window). An id this ledger
// does not hold is left out of the map, which is the same "absence is an
// answer" contract TaskByID has.
func (v taskLedgerDependencyView) StatusesByID(ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	named := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		named = append(named, id)
	}
	if len(named) == 0 {
		return out, nil
	}
	args := make([]any, len(named))
	for i, id := range named {
		args[i] = id
	}
	err := syncRowWithLedger(v.dbPath, func() error {
		db, err := openTasksDB(context.Background(), memoryDBPath())
		if err != nil {
			return err
		}
		defer db.Close()
		rows, err := db.Query(
			`SELECT task_id, status FROM tasks WHERE task_id IN (`+sqlPlaceholders(len(named))+`)`, args...)
		if err != nil {
			return err
		}
		//stylegate:ignore the deferred Close covers the error path; rows.Err() below is the read's own verdict
		defer rows.Close()
		for rows.Next() {
			var id, status string
			if err := rows.Scan(&id, &status); err != nil {
				return err
			}
			out[strings.TrimSpace(id)] = strings.TrimSpace(status)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sqlPlaceholders renders `?, ?, …` for n arguments (Rule 1: one spelling of
// the placeholder idiom rather than a format string built inline).
func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat(taskSQLPlaceholder+", ", n), ", ")
}

// taskSQLPlaceholder is the ONE spelling of a positional SQLite argument.
const taskSQLPlaceholder = "?"

// taskArrivalWritesTheEdge mirrors syncDeltaMergeRow's OWN gate on the edge
// backfill: the merge reaches the edge only when it reaches the row at all — on
// a strictly newer anchor, or on a rank upgrade (a forward move). Every other
// shape (an older copy, an idempotent echo) leaves the row alone, so it writes
// NO edge and cannot park anything.
//
// IT IS A MIRROR ON PURPOSE, and it is the reason the criterion can be trusted
// with a row that is already here: a criterion that judged an edge the merge
// would never have written would refuse the ordinary "the peer is behind" pull.
// local may be nil (the row is not in this ledger yet, so the import INSERTs it
// and the edge is written whole — the insert lane's own judgement).
func taskArrivalWritesTheEdge(row SyncTaskRow, local *Task) bool {
	if local == nil {
		return true
	}
	if float64(row.UpdatedAt) > local.UpdatedAt {
		return true
	}
	return statusRankOf(strings.TrimSpace(row.Status)) > statusRankOf(local.Status)
}

// The arrival half's own rendered reasons. The family's sentences above are
// reused verbatim where the condition is the same one (a parent this ledger
// cannot release); these two say what is specific to the ARRIVING row, whose
// words have to work on a ledger the row is not on yet.
const (
	// taskTransferWhyArrivalNoView: no ledger view was handed in. Denied, never
	// assumed satisfied — the same rule syncParentVerdict's nil-lookup arm
	// already follows, and the reason the view is injected rather than defaulted.
	taskTransferWhyArrivalNoView = "the ledger could not be read, and an unreadable dependency graph is never assumed clear"
	// taskTransferWhyNoID: a row with no id cannot be looked up in this ledger,
	// so whether the merge would write its edge is unanswerable.
	taskTransferWhyNoID = "the row carries no task id, so it cannot be looked up in this ledger"
	// taskTransferWhyReadFailedFormat: the lookup itself failed. Same rule as
	// the nil view above — an unreadable graph is never assumed clear.
	taskTransferWhyReadFailedFormat = "the ledger read failed (%v), and an unreadable dependency graph is never assumed clear"
	// taskTransferWhyArrivalParentMissingFormat names the parent for a row that
	// is not here yet: "has to be repaired here before the row travels" (the
	// departing row's phrasing) would describe the wrong ledger.
	taskTransferWhyArrivalParentMissingFormat = "this row names parent %s, which is not in this ledger — a child whose parent cannot be resolved is parked by Rule 33 (it releases only on %s), so landing it would write an edge nothing here can resolve; send or pull the parent first, then re-run this pull"
)

// TaskTransferArrivalEligible is THE criterion for a row ARRIVING on this
// ledger — the pull half (`gopi task pull` / `task receive`). It is a SECOND
// FUNCTION rather than a parameter on TaskTransferEligible because the question
// is not the same one asked from the other side (see the section head above).
//
// WHAT IT JUDGES is the row's OWN edge, against the ledger that will have to
// honour it. The verdict is the FAMILY'S, not a second opinion: an
// unresolvable edge is judged by syncParentUnresolvable — the very predicate
// the import's own apply and its insert arm already run — against the SAME
// release set (taskParentReleaseStatuses). So a criterion refusal and an
// import refusal cannot disagree about one edge, and the criterion cannot start
// refusing rows the merge would have accepted.
func TaskTransferArrivalEligible(row SyncTaskRow, dep TaskDependencyView) (bool, string) {
	taskID := strings.TrimSpace(row.TaskID)
	if taskID == "" {
		return false, taskTransferWhyNoID
	}
	if dep == nil {
		return false, taskTransferWhyArrivalNoView
	}
	parentID := strings.TrimSpace(row.ParentTaskID)
	if parentID == "" {
		// No edge travels: nothing to resolve, and the row arrives in exactly
		// the state it was in before this criterion existed.
		return true, ""
	}
	local, err := dep.TaskByID(taskID)
	if err != nil {
		return false, fmt.Sprintf(taskTransferWhyReadFailedFormat, err)
	}
	if local != nil {
		// The row is HERE already, so the import is a merge; a merge writes an
		// edge only when it moves the row at all (an older copy and an
		// idempotent echo both leave it alone), so an incoming edge the merge
		// would never reach is not this criterion's business — refusing it
		// would refuse the ordinary "the peer is behind" pull.
		if !taskArrivalWritesTheEdge(row, local) {
			return true, ""
		}
		// A local edge is never clobbered: if this row already carries one, the
		// incoming edge is not written and cannot be what parks it.
		if strings.TrimSpace(local.ParentTaskID) != "" {
			return true, ""
		}
	}
	// THE EDGE IS JUDGED BY THE FAMILY'S OWN PREDICATE, consulted once for the
	// one parent this row names: the status comes from the arrival view's read
	// (the ledger as the import will find it), and the verdict — released,
	// dangling, terminal-unreleased, live-but-orderable — is syncParentVerdict's,
	// exactly as the import's own apply asks it. A LIVE parent is NOT refused
	// here: the edge is real and unmet, which is the state Rule 33 models for
	// this ledger (the row lands and the dispatcher parks it until its parent
	// releases) — refusing it would break the designed chain flow, where a
	// child may legitimately arrive ahead of the run that finishes its parent.
	statuses, err := dep.StatusesByID([]string{parentID})
	if err != nil {
		return false, fmt.Sprintf(taskTransferWhyReadFailedFormat, err)
	}
	parentStatus := statuses[parentID]
	if parentStatus == "" {
		return false, fmt.Sprintf(taskTransferWhyArrivalParentMissingFormat, parentID, taskParentReleaseSetLabel())
	}
	if reason, bad := syncParentUnresolvable(parentID, func(string) (string, bool) {
		// Already read: the consultation is one statement for every id the
		// criterion needs, never a lookup per question.
		return parentStatus, true
	}); bad {
		return false, reason
	}
	return true, ""
}
