package tools

import "fmt"

// WHICH SESSION A RECOVERY RUN CONTINUES — one answer, one place (Rule 14).
//
// THE DEFECT THIS CLOSES (measured on this host, 2026-10-28). Two recovery
// arms resolved their resume target from the row's `session_id` COLUMN:
//
//	internal/tools/task_holder_resume.go:411   target := r.SessionID
//	cmd/agent/cli_tasks.go:3777                targetArg = TaskSessionID(target)
//
// and both then spawned `gopi session resume <that id> --task <row>`. The
// column does NOT mean "the session that ran this row": for a row submitted by
// an agent (`source=agent`) it is the SUBMITTER's session, written at
// task_submit time, and it is shared by every row that session filed. Measured
// on the live ledger: 1511 rows carried a session_id, only 269 of them agreed
// with the session record in their OWN run directory, and one chat session
// (`07d98407-…`) was named by 47 rows it had merely submitted. So the recovery
// run of any such row would have continued ANOTHER conversation — the wrong
// context, replayed into the wrong row, with the ledger recording it as the
// row's own run.
//
// WHY THE COLUMN IS NOT SIMPLY REWRITTEN. Its submitter semantics are load
// bearing: claim eligibility refuses a row whose `session_id` equals the
// claiming session (internal/tools/tasks.go:7584, `session_id != ?` — the
// anti-self-reference guard that stops a session from claiming a row it filed
// itself). Rewriting the column to the executing session would make every
// resumed row claimable by the very session that ran it. The fix therefore
// belongs to the READ side: the COLUMN keeps meaning what it means, and every
// recovery target is resolved here.
//
// THE ORDER, and why each step is where it is:
//
//  1. THE ROW'S OWN RUN DIRECTORY — `sessions/<task-id>/<session-id>.jsonl`.
//     It is keyed by the row id, so it belongs to this row by construction, and
//     it is the address a resume actually needs. taskRunSessionFromRowDir
//     answers it (newest record wins for a row resumed more than once).
//  2. THE ROW ID — `gopi session resume <task-id>` resolves the row's own
//     checkpoint itself (resumeTaskRawLog → tools.TaskResumeTarget reads the run
//     directory too), so the recovery still lands on THE SAME work. This is the
//     step taken while a row has a run directory but no record yet.
//  3. NEVER THE COLUMN. A column value that names no record of this row is the
//     submitter's session; continuing it replays someone else's conversation.
//     It is reported (never silently adopted) through TaskResumeSession.Column,
//     so an operator reading the arm's log line sees why the row did not get a
//     session-named target.

// The resolution sources, spelled once (Rule 1): the arm's log line and the
// tests read the constant, never a re-typed copy.
const (
	taskResumeSessionSourceRunDir = "row-run-dir"
	taskResumeSessionSourceRowID  = "row-id"
)

// TaskResumeSession is the resolved answer: WHICH session a recovery run of a
// row must continue, WHY that is the answer, and what the ledger column said —
// carried so a disagreement is REPORTED rather than silently overridden.
type TaskResumeSession struct {
	// SessionID is the resume target: an id the row's own run directory holds,
	// else the row id (step 2 above — never empty for a real row).
	SessionID string
	// Source names which step produced SessionID (taskResumeSessionSource*).
	Source string
	// Column is the row's `session_id` column verbatim. Equal to SessionID for
	// the ordinary case (the row ran in the session the column names, i.e. a
	// `source=human`/`auto` row); DIFFERENT when the column names a submitter
	// session, which is the defect this file exists for.
	Column string
}

// Differs reports whether the ledger column named a different session than the
// row's own run directory — the condition an operator must see, because the
// column's value is a session that did not run this row.
func (t TaskResumeSession) Differs() bool {
	return t.Column != "" && t.Column != t.SessionID
}

// Note renders the one-line explanation of a disagreement, or "" when there is
// nothing to explain (the ordinary case). One renderer (Rule 14) so the
// dead-holder arm and the CLI face cannot word it differently.
func (t TaskResumeSession) Note() string {
	if !t.Differs() {
		return ""
	}
	return fmt.Sprintf("the row's session_id column names %s, which is NOT a run of this row (it is the session that submitted it) — resuming %s (%s) instead",
		t.Column, t.SessionID, t.Source)
}

// TaskResumeSessionFor resolves the session a recovery run of taskID must
// continue. A row with no run directory at all resolves to its own id with
// taskResumeSessionSourceRowID: `gopi session resume <task-id>` is then the
// caller's ordinary task-mode target, whose own resolution reads the run
// directory (tools.TaskResumeTarget) and reports a definite absence when there
// is none.
func TaskResumeSessionFor(taskID string) TaskResumeSession {
	tid := taskIDNormalize(taskID)
	out := TaskResumeSession{SessionID: tid, Source: taskResumeSessionSourceRowID}
	if tid == "" {
		return out
	}
	// The column is read for the NOTE only — never to choose the target. A
	// read error leaves it empty, which Differs()/Note() treat as "nothing to
	// report" rather than as an agreement (an unread ledger must not manufacture
	// a claim about what it held).
	// The column read for the NOTE is the session that FILED the row — the one
	// identity that is NOT this row's run. Since the split (2026-10-28) that is
	// its own column, and a legacy row answers from `session_id` through the same
	// resolver, so the note still fires for a pre-split row whose column names a
	// submitter. Reading `session_id` directly here would now compare the row's
	// OWN run session against itself and never report anything.
	if sub, err := TaskSubmitterSession(tid); err == nil {
		out.Column = sub
	}
	if dirSession := taskRunSessionFromRowDir(tid); dirSession != "" {
		out.SessionID = dirSession
		out.Source = taskResumeSessionSourceRunDir
	}
	// NO FILER FALLBACK HERE, and the omission is deliberate (2026-10-28): a row
	// that never ran resolves to its OWN id, whose task-mode resolution reads its
	// own directory. Substituting the filer's session would arm a recovery with
	// someone else's conversation — the substitution this whole ladder exists to
	// prevent. The fumbling case that DOES need the filer is `TaskResumeTarget`'s
	// address question (taskProcessSessionOf), which is a different question: not
	// "which transcript belongs to me" but "what session is associated with this
	// row" — and it answers with the row's run session first.
	return out
}
