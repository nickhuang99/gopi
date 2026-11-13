package tools

// failed-needs-reissue — the ONE mechanism that makes a `failed` row with no
// successor IMPOSSIBLE to silently forget.
//
// THE DEFECT (measured on the operator's live ledger, 2026-10-01): 289 rows
// carry status=failed and are terminal BY DESIGN (taskAllowedTransitions —
// failed has no outgoing edge). 288 of them have NO successor row and no
// `resolved` tag, so nothing in the system will ever look at them again:
// `gopi task list` renders them as one more terminal line among 3,000, the
// reclaim sweep only scans rows whose deliverable carries a repo, and a
// reissue is a MANUAL act (`gopi task submit` / `gopi task clone`) that
// nothing ever asks for. A row can therefore fail and be forgotten forever
// with no alarm, no count and no id — the operator's own framing: "a failed
// row is SILENT forever".
//
// WHAT THIS FILE IS, AND IS NOT. It is VISIBILITY, not retry: it counts and
// names the rows on surfaces an operator already reads, and it fires ONE
// notify event through the existing alarm pipeline. It never flips a status,
// never mints a row, never schedules anything — auto-reissue is explicitly
// out of scope (and the status model stays append-only and untouched: no
// transition is added, no status value is minted).
//
// THE PREDICATE — a row is "failed-needs-reissue" iff ALL THREE hold:
//
//	1. status = 'failed'            — the terminal verdict;
//	2. NO successor row exists      — no row carries it as `parent_task_id`
//	   (a reissue child, a rework child, a re-dispatch all land there);
//	3. it is not tagged `resolved`  — the operator's OWN "handled, stop
//	   counting it" mark (the same tag `task archive` appends, so a handled
//	   failure is retired by the existing archive verb, never by a new one).
//
// Conditions 2 and 3 are the two ways a failure stops being a debt: it was
// REISSUED (a successor exists) or it was ADJUDICATED (the operator tagged it
// solved). Neither is a status change, which is why this mechanism needs no
// schema change and no transition — and why it cannot disturb the 289 rows
// already in the ledger (it reads them; nothing here writes a row).
//
// THE FOURTH CONDITION — and why the count is SPLIT, not merely filtered
// (added 2026-11-01). A row that carries NEITHER a briefing (payload empty) NOR
// a ground (no deliverable.branch/worktree) is not a debt: nothing was ever
// asked of it, so nothing is owed on it. That population is real and large —
// 71 of the 329 counted rows on the operator's live ledger, every one of them a
// bare-prompt/chat registration (`hi`, `list`, `config show`, `replay <id>`)
// whose payload is EMPTY BY CONTRACT (internal/agent/taskregister.go: "Payload
// stays EMPTY for bare auto rows on purpose"). They are NOT dropped: dropping
// them would be the same silence in the other direction (the population would
// exist and nothing would say so), and it would make the printed total
// unreproducible. They are NAMED, COUNTED, and stated to owe nothing — so the
// decisive number (the debt) is not diluted by a population that cannot be paid.
//
// WHERE IT SURFACES — ONE carrier, and it is a live operator verb:
//
//   - the `gopi task list` render (TaskList appends FailedReissueNote, via
//     failedReissueNote in tasks.go). Every `gopi task list` — plain, or with
//     --status failed — replays the count and the ids on its header.
//
// THE RESIDENT/notify LEG IS GONE (2026-10-30, task 01a18ef7). It was
// SweepFailedNeedsReissue, documented as riding "the tick the worker loop and
// the merger already fire". MEASURED: that tick had no production caller (the
// worker round's call was removed by 187d05c, the merger tick was deleted
// 2026-09-19, and `gopi task reconcile` was retired in 01a18799), so this file's
// only caller was that dead pass. A registered alarm nothing fires is the same
// silent-failure class as a duty nothing runs, so the sweep, its per-process
// notify latch and the event registration went with it (Rule 43). The DUTY did
// not go anywhere: it is the `task list` render below, and that is the surface
// the operator already reads.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TaskFailedReissuePrefix opens the rendered count line. It is EXPORTED and
// is the ONE spelling: the render and the tests all reference it, so a reader
// (or a gate) greps one token and finds every surface (Rule 1b/13).
const TaskFailedReissuePrefix = "failed-needs-reissue: "

// taskFailedReissueFormat spells the count line ONCE: the count, the tag that
// retires a row from the count, the ids and — at the END of the line — the two
// verbs that act on them, spelled EXACTLY as the CLI advertises them so the
// line is copy-pasteable (`<id>` is the placeholder). The `%s%s` tail is the id
// list plus the "…and N more" suffix, each rendered by its own const below.
//
// WHY THE VERBS RIDE THE LINE: the note names a debt ("reissue or tag them")
// but an operator reading `gopi task list` had to go look up the spelling of
// the verbs before acting — the alarm stopped one step short of the fix. The
// verbs are VERIFIED against their own help, not recalled:
//
//   - `gopi task submit "<the work, re-briefed>"` — the REISSUE door. There is
//     no operator reissue verb: the append-only reissue ENGINE (a new pending
//     `<id>-r1` row, parent=<id>) is reached through a verify FAIL verdict, and
//     a FAILED row cannot be moved back to pending by `task update` (the
//     transition table admits failed -> verified|completed only), so a fresh
//     submit is the one spelling an operator has.
//   - `gopi task archive <task_id> [--db PATH]` — the ADJUDICATE verb. There is
//     NO `gopi task tag` verb; `task archive <id>` is the one that appends the
//     `resolved` tag this very line counts by, so the second command is the
//     archive verb, not an invented `tag`.
const taskFailedReissueFormat = TaskFailedReissuePrefix +
	"%d failed row(s), no successor row and no %q tag — reissue or tag them; newest first: %s%s" +
	taskFailedReissueNextFmt

// taskFailedReissueNoneFmt is the DEBT-ZERO arm of the same line: a ledger
// whose counted failures are ALL conversation registrations (see the named
// bucket below) must not print "0 … newest first: " with nothing after the
// colon — that reads like a truncated line. The bucket sentence still
// follows, so the reader learns WHY the actionable count is zero instead of
// being told nothing is wrong.
const taskFailedReissueNoneFmt = TaskFailedReissuePrefix +
	"%d failed row(s) with no successor row and no %q tag — nothing to reissue"

// taskFailedReissueBucketFmt is the NAMED SECOND BUCKET, rendered after the
// actionable count and its ids.
//
// WHY IT EXISTS (measured 2026-11-01, this row). The one number mixed two
// populations with OPPOSITE meanings: 329 rows were reported as "reissue or
// tag them", and 71 of those were bare-prompt/chat registrations — `hi`,
// `list`, `config show`, `replay 56507166-…` — the rows the harness mints for
// a run that was never asked for anything. Their payload is EMPTY BY
// CONTRACT (taskregister.go: "Payload stays EMPTY for bare auto rows on
// purpose") and they carry no ground, so no deliverable was ever owed on
// them: "reissue" is not a debt an operator can pay, it is a question that
// does not apply. Left inside one number they made the debt look 27% larger
// than it is, which is exactly how a real alarm teaches a reader to discount
// it.
//
// The split stays on the SAME line (the note is one log line and one webhook
// body — see taskFailedReissueNextFmt's note), and the phrasing says plainly
// that this bucket owes NOTHING, so a reader never has to guess whether the
// ids it does not see are an oversight.
const taskFailedReissueBucketFmt = " | not reissue debt: %d row(s) with no briefing and no ground — bare-prompt/chat registrations (a prompt like `hi`, `config show`, `replay <id>`), which were never asked for a deliverable and carry none, so nothing is owed on them"

// The two bucket NAMES are the Go side's verdict vocabulary — the values the
// row's Bucket field carries, the tests match on, and any future reader keys
// on. Exported and spelled ONCE (Rule 1b/13/14): a second literal spelling of
// "actionable" is how two surfaces drift.
const (
	// FailedReissueBucketActionable — a failed EXECUTION somebody asked for:
	// it carries a briefing and/or a ground, so a reissue is a debt that can
	// be paid.
	FailedReissueBucketActionable = "actionable"
	// FailedReissueBucketNoBriefingNoGround — a bare-prompt/chat registration:
	// no briefing (payload empty by contract) AND no ground (no branch, no
	// worktree), i.e. a row nothing was ever asked of.
	FailedReissueBucketNoBriefingNoGround = "no-briefing-no-ground"
)

// taskFailedReissueNextFmt appends the copy-pasteable verbs to the note.
// It stays on the SAME line (the note is one log line and one webhook body),
// and `<id>` is the placeholder an operator replaces with an id the note just
// printed. Kept as its own const so a help/gate test can assert the exact
// spelling without re-typing it (Rule 13).
const taskFailedReissueNextFmt = ` — next: gopi session resume <id> (continue the same row) | gopi task clone <id> (a clean room) | gopi task archive <id>`

// taskFailedReissueMoreFmt is the tail when the id list is capped: an
// operator reads the newest ids and knows exactly how many it did not see —
// the line never silently truncates (Rule 15).
const taskFailedReissueMoreFmt = " (+%d more)"

// taskFailedReissueMaxIDs caps the id list on the RENDERED line (the notify
// summary reuses it): a ledger with hundreds of orphan rows must not put
// hundreds of ids into one log line or one webhook body, while the COUNT
// stays exact.
const taskFailedReissueMaxIDs = 5

// taskFailedReissueQuery is the predicate, in SQL (the shape the sweep runs).
// `tasks` is the outer table and is referenced BY NAME from the subquery, so
// the successor test is provably correlated to the row under test.
//
// The `resolved` test mirrors isTaskResolved's Go rule — token equality after
// trimming — via the comma-wrapping twin the bug population already uses
// (taskBugPopulationWhere), so a tag that merely CONTAINS "resolved"
// ("sync-resolved") is not read as the archive mark. The LIKE argument is
// built from taskTagResolved itself (below), never re-typed.
//
// THE BUCKET EXPRESSION is the SQL half of failedReissueBucketOf — the SAME
// two facts (a ground named by deliverable.branch/worktree, a briefing in
// payload), asked ONCE and reused by both arms of the mechanism, so a reader
// can reproduce any rendered number with one query (measured 2026-11-01:
// `counted=329, conversation=71, debt=258` on the operator's ledger).
//
// It DECODES the deliverable blob rather than LIKE-matching the key spellings,
// and the difference is measured, not stylistic: the LIKE form over-matched on
// the real ledger (`%branch%` hit 3 rows whose only occurrence is in a value,
// 266 vs the decoder's 263), and json_extract THROWS on the 73 rows whose
// deliverable column is the empty string — the json_valid guard is what makes
// the expression total. The key names are the Deliverable*Key constants bound
// as parameters (Rule 1b/13), never literals inside the statement.
//
// The bucket verdict is joined to the debt test in Go (never correlated here)
// so `FailedReissueNote` and `FailedNeedsReissue` read ONE row set.
var taskFailedReissueQuery = `SELECT task_id,
	  CASE WHEN ` + taskFailedReissueGroundExpr + `
	       THEN 1 ELSE 0 END,
	  CASE WHEN trim(COALESCE(payload, '')) = '' THEN 0 ELSE 1 END
	FROM tasks
	WHERE status = ?
	  AND NOT EXISTS (SELECT 1 FROM tasks c WHERE c.parent_task_id = tasks.task_id)
	  AND (',' || replace(COALESCE(tags, ''), ' ', '') || ',') NOT LIKE ?` +
	taskListOrderNewestFirst

// taskFailedReissueGroundExpr is the ONE spelling of "this row names a
// ground". It is a SQL predicate over a row of `tasks` and it answers the same
// question taskDeliverableParse answers in Go — branch-or-worktree, decoded
// from the blob. `json_valid` first, because json_extract RAISES on a
// malformed (or empty) document and the ledger carries 73 such rows.
const taskFailedReissueGroundExpr = `(json_valid(COALESCE(deliverable, '')) AND (
		COALESCE(json_extract(deliverable, ?), '') <> ''
		OR COALESCE(json_extract(deliverable, ?), '') <> ''))`

// taskFailedReissueQueryArgs is the argument list for taskFailedReissueQuery —
// built from the constants, in ONE place, so the statement and its parameters
// cannot drift (Rule 1b: a positional `?.` list retyped at a second call site
// is how a WHERE clause silently starts comparing the wrong column).
func taskFailedReissueQueryArgs() []any {
	return []any{
		"$." + DeliverableBranchKey,
		"$." + DeliverableWorktreeKey,
		taskStatusFailed,
		taskFailedReissueResolvedLike,
	}
}

// taskFailedReissueResolvedLike is the LIKE argument of the resolved test —
// derived from taskTagResolved so the two spellings cannot drift (Rule 13).
const taskFailedReissueResolvedLike = "%," + taskTagResolved + ",%"

// FailedReissueRow is one row the mechanism reports — the minimum an operator
// (or a notify subscriber) needs to act on it: the id to open, and WHICH
// population it belongs to.
type FailedReissueRow struct {
	TaskID string
	// Bucket is the row's verdict (FailedReissueBucketActionable or
	// FailedReissueBucketNoBriefingNoGround). It rides the row rather than
	// being recomputed by the renderer: the query decided it, so the count, the
	// id list and any future consumer all read the SAME answer instead of
	// three re-derivations that can disagree.
	Bucket string
}

// Actionable reports whether this row is a debt somebody can pay: a failed
// EXECUTION that carried a briefing or a ground.
func (r FailedReissueRow) Actionable() bool {
	return r.Bucket == FailedReissueBucketActionable
}

// taskFailedReissueBucketOf is the Go side of the bucket verdict — the ONE
// predicate, applied to the two facts the SQL selected. A row is NOT a debt
// iff it carries no briefing AND no ground; anything else is actionable.
//
// Both facts are read as selected (integers), never re-queried, so this
// function cannot disagree with the statement that produced the row: the split
// is expressed once per side and the two sides ask the SAME two questions
// (measured identical on 487 real failed rows — 0 mismatches over both axes).
func taskFailedReissueBucketOf(hasGround, hasBriefing bool) string {
	if !hasGround && !hasBriefing {
		return FailedReissueBucketNoBriefingNoGround
	}
	return FailedReissueBucketActionable
}

// FailedNeedsReissue returns every `failed` row that has no successor row and
// carries no `resolved` tag, newest first, each carrying its bucket. It is the
// READ half of the mechanism and is pure over an OPEN ledger — the caller owns
// the handle (Rule 32: a test hands it the scaffold's database, never a
// hand-rolled path). Errors are returned, never swallowed (Rule 15).
//
// EVERY counted row comes back, bucket rows included: the count on the line is
// the sum of the two buckets, so dropping the non-debt rows here would make
// the rendered numbers impossible to re-derive from this function.
func FailedNeedsReissue(ctx context.Context, db *sql.DB) ([]FailedReissueRow, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := db.QueryContext(ctx, taskFailedReissueQuery, taskFailedReissueQueryArgs()...)
	if err != nil {
		return nil, fmt.Errorf("failed-needs-reissue query: %w", err)
	}
	defer rows.Close()
	var out []FailedReissueRow
	for rows.Next() {
		var id string
		var hasGround, hasBriefing int
		if err := rows.Scan(&id, &hasGround, &hasBriefing); err != nil {
			return nil, fmt.Errorf("failed-needs-reissue scan: %w", err)
		}
		out = append(out, FailedReissueRow{
			TaskID: id,
			Bucket: taskFailedReissueBucketOf(hasGround == 1, hasBriefing == 1),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed-needs-reissue rows: %w", err)
	}
	return out, nil
}

// FailedReissueNote renders the counted line for rows, and "" when there are
// none — a ledger with nothing counted must render NOTHING, not a zero line
// (the same contract as TaskListArchiveHiddenNote: a note that renders at zero
// is noise an operator learns to skip, which is how a real alarm stops being
// read). A ledger whose counted rows are ALL bucket rows is NOT that case: it
// renders, with an actionable count of 0 and the bucket sentence saying why.
//
// THE ORDER IS THE MEANING: the actionable count and its ids come first (that
// is the debt), and the named bucket follows. The id list names ONLY
// actionable rows, so pasting the line's ids into `gopi task show` never
// lands on a chat registration.
//
// It is a FUNCTION rather than a format string so the zero case cannot be
// rendered by accident, and it is the ONE renderer: every reader of this
// mechanism — today that is the `task list` render — calls it.
func FailedReissueNote(rows []FailedReissueRow) string {
	if len(rows) == 0 {
		return ""
	}
	var debt, bucket int
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Actionable() {
			debt++
			ids = append(ids, r.TaskID)
			continue
		}
		bucket++
	}
	line := fmt.Sprintf(taskFailedReissueNoneFmt, debt, taskTagResolved)
	if debt > 0 {
		shown, more := ids, ""
		if len(shown) > taskFailedReissueMaxIDs {
			more = fmt.Sprintf(taskFailedReissueMoreFmt, len(ids)-taskFailedReissueMaxIDs)
			shown = shown[:taskFailedReissueMaxIDs]
		}
		line = fmt.Sprintf(taskFailedReissueFormat, debt, taskTagResolved,
			strings.Join(shown, " "), more)
	}
	if bucket > 0 {
		line += fmt.Sprintf(taskFailedReissueBucketFmt, bucket)
	}
	return line + "\n"
}
