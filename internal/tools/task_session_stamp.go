package tools

// WHERE A ROW'S session_id MAY COME FROM — and how a value that came from
// somewhere else is repaired (epic 01a17df9).
//
// THE MEASURED DEFECT. Two worker-claimed rows carried the SAME chat session id
// (07d98407-…, the console's own session) while sessions/<task-row>/ held the
// record of the run that really executed them. The write source could fall back
// to "the newest *.jsonl under the profile's sessions ROOT" — the LEGACY FLAT
// layout — and a chat session is written continuously, so inside any run window
// its flat record is newer than the row's own. That id was then stamped into the
// row. The stamp guard was "only while the column is EMPTY", so the wrong value
// became PERMANENT: no later write could correct it, and every session-keyed
// reader (resume, log, heartbeat, model) resolved to a stranger's conversation.
//
// THE FIX HAS THREE PARTS, all keyed on ONE question — "is this session record
// inside the row's OWN run directory?" (`sessions/<task-id>/<session>.jsonl`):
//
//  1. WRITE SOURCE (taskSessionStampRefusal): a session id whose record is not
//     the row's own is REFUSED by name instead of stamped. A refusal is not a
//     failure of the run — the run's artifacts are elsewhere — so it is stated
//     on stderr and the column is left alone (Rule 15: never silent).
//  2. SELF-HEAL (taskSessionRepairValue): the deferred stamp repairs a column
//     whose value PROVABLY belongs to another session (it names that session's
//     own container, its flat record, or a row in the chats index) by replacing
//     it with the row's own record. A value that is merely unverifiable is NOT
//     touched — Rule 5/31 of the epic: only what can be PROVEN foreign.
//  3. REPAIR DOOR (TaskRepairSessionIDs): `gopi task update --repair-session`
//     runs the same predicate over the rows already on the ledger, with a dry
//     run as the default reading.
//
// Rule 14: every predicate here is asked by both halves (the stamp and the
// repair door) — a second copy of "does this belong to the row" is exactly the
// drift that produced the polluted column.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/sessionlayout"
)

// taskSessionRepairEventName is the append-only event a repair leaves on the
// row's trail, so a corrected column can be told apart from one that was always
// right (the audit surface every other repair in this ledger uses).
const taskSessionRepairEventName = "session-repaired"

// taskSessionRefusalSubject names the row a stamp refusal is about, in the ONE
// wording both the early stamp and the writeback seam print.
const taskSessionRefusalSubject = "session_id stamp"

// taskSessionRowDir returns the row's OWN run directory —
// `<profile>/sessions/<task-id>` — and whether it exists. "" when the profile
// cannot be resolved (the ordinary "nothing to say" absence).
func taskSessionRowDir(taskID string) string {
	tid := taskIDNormalize(taskID)
	if tid == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err != nil {
		return ""
	}
	return filepath.Join(gd, config.SessionsDirName, tid)
}

// taskSessionRecordIsRowsOwn reports whether `sessions/<task-id>/<sid>.jsonl`
// exists as a regular file — the ONE test of "this session record is the row's
// own". A record anywhere else (a bare session container, the legacy flat root,
// another row's directory) is by construction a different session's artifact.
func taskSessionRecordIsRowsOwn(taskID, sessionID string) bool {
	tid := taskIDNormalize(taskID)
	sid := strings.TrimSpace(sessionID)
	if tid == "" || sid == "" {
		return false
	}
	dir := taskSessionRowDir(tid)
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, sid+sessionlayout.JSONLSuffix))
	return err == nil && !fi.IsDir()
}

// taskSessionForeignHome answers WHERE a session id lives when it is not the
// row's own — a non-empty evidence string, or "" when the id names no session
// this machine can point at. The three shapes are the three ways a session
// exists on disk/ledger, and each is a CHEAP probe (Rule 22's precedent: no
// whole-root walk for a question three stats can answer):
//
//	<profile>/sessions/<sid>/            a bare session container (a chat or an
//	                                     interactive run that is not a task run)
//	<profile>/sessions/<sid>.jsonl       the legacy FLAT record — the exact
//	                                     source this epic removes from the
//	                                     write path, and the one a chat keeps
//	                                     writing during a task's run window
//	the chats ledger                     a session this node indexed as a chat
//
// A row id is never "foreign evidence" on its own: the question is about
// SESSIONS, and a task id that happens to equal a session id names that row's
// own run (the unified identity) — taskSessionRecordIsRowsOwn decides that case
// first.
func taskSessionForeignHome(sessionID string) string {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return ""
	}
	gd, err := config.GopiDir()
	if err == nil {
		root := filepath.Join(gd, config.SessionsDirName)
		if fi, serr := os.Stat(filepath.Join(root, sid)); serr == nil && fi.IsDir() {
			return filepath.Join(root, sid)
		}
		if fi, serr := os.Stat(filepath.Join(root, sid+sessionlayout.JSONLSuffix)); serr == nil && !fi.IsDir() {
			return filepath.Join(root, sid+sessionlayout.JSONLSuffix)
		}
	}
	return ""
}

// taskSessionIsAChat reports whether this node's chats index carries the id —
// the shape that polluted the two rows this epic was filed for (a console
// session had no container and no flat record left, but the index still named
// it). A ledger that cannot be read answers false, never a guess.
func taskSessionIsAChat(ctx context.Context, q QueryExecer, sessionID string) bool {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || q == nil {
		return false
	}
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM chats WHERE session_id = ?`, sid).Scan(&one)
	return err == nil
}

// taskSessionStampRefusal is the WRITE-SOURCE gate (deliverable ①): a session
// id may be stamped into a row only when the record it names sits in the row's
// own run directory. Returns a refusal sentence naming the id, the directory
// that would have to hold it, and where the id actually lives — or "" when the
// stamp is legitimate.
//
// WHY A REFUSAL AND NOT A SILENT SKIP: the caller (the task-run poll) is the one
// meeting that produced the id, and a quiet skip would look identical to "no
// session yet", so the operator would never learn that the poll's window caught
// someone else's record. The run itself is unaffected — the writeback's
// deferred stamp still fills the column from the row's own directory.
func taskSessionStampRefusal(ctx context.Context, q QueryExecer, taskID, sessionID string) string {
	tid := taskIDNormalize(taskID)
	sid := strings.TrimSpace(sessionID)
	if tid == "" || sid == "" {
		return ""
	}
	if taskSessionRecordIsRowsOwn(tid, sid) {
		return ""
	}
	dir := taskSessionRowDir(tid)
	where := taskSessionForeignHome(sid)
	if where == "" && taskSessionIsAChat(ctx, q, sid) {
		where = "the chats index names it as a chat session"
	}
	if where == "" {
		// NOT AT A SHARED ADDRESS — but it may still be ANOTHER ROW'S run
		// record (sessions/<other>/<sid>.jsonl), which is the executor-stamp
		// pollution in its purest form. One walk of the run directories answers
		// it; the walk happens only on the path that was about to be accepted,
		// so the ordinary stamp pays nothing.
		if owner := taskSessionRunIndex()[sid]; owner != "" && owner != tid {
			where = fmt.Sprintf("the run record of task %s", owner)
		}
	}
	if where == "" {
		// NO HOME AT ALL: the id names no session this machine can point at —
		// the ordinary race where the child's record has not been written yet.
		// Refusing it would turn "nothing on disk yet" into a hard error, which
		// is a different (and false) claim from "this id belongs to someone
		// else"; the fill-only-while-empty guard already keeps such a stamp
		// from overwriting anything. Only a PROVABLE foreign home is refused.
		return ""
	}
	return fmt.Sprintf("%s REFUSED for %s: %q is not this row's session record — "+
		"its record must be %s, and it is %s. Only a record inside the row's OWN run directory is accepted "+
		"(a continuously written chat session is newer than the run's own log inside any run window, "+
		"which is how a foreign id used to be stamped permanently)",
		taskSessionRefusalSubject, tid, sid,
		filepath.Join(dir, sid+sessionlayout.JSONLSuffix), where)
}

// taskSessionRepairValue is the REPAIR decision, and it is deliberately NARROW.
//
// THE COLUMN HAS TWO HONEST MEANINGS, and this predicate exists to respect the
// difference (measured before it was written — internal/tools/
// task_resume_session.go documents it):
//
//	SUBMITTER (a `source=agent` row): the session that FILED the row, written at
//	  task_submit time and shared by every row that session filed. It is load
//	  bearing: the anti-self-reference guard refuses a claim when
//	  `session_id = <claiming session>`, which is what stops a session from
//	  claiming a row it filed itself. A "repair" that rewrote it would break
//	  that guard on every resumed row.
//	EXECUTOR (TaskBackfillSession, the early stamp): the session that RAN the
//	  row. Its record lives in the row's own run directory by construction.
//
// So "the column names a session that is not this row's" is NOT by itself
// evidence of pollution — for an agent-filed row it is the DESIGN. The one
// shape that is provably wrong is an EXECUTOR stamp that names ANOTHER ROW'S
// run: `sessions/<other-task-id>/<id>.jsonl`. A submitter session is never
// another row's run record, so that shape can only be a stamp that came from
// the wrong source (the flat/root fallback this epic removes, or an inherited
// GOPI_EXECUTOR_SESSION_ID), and replacing it with the row's own record cannot
// damage the claim guard (the value it removes names no filer of this row).
//
// Everything else is LEFT ALONE and REPORTED with its reason: a bare chat
// container, a legacy flat record, a chats-index session — each is a plausible
// submitter, and an unprovable value keeps its authority (the epic's own
// constraint: never overwrite the row's own authoritative record; only correct
// values provably not belonging to the row).
func taskSessionRepairValue(ctx context.Context, q QueryExecer, idx map[string]string, taskID, current string) (newSID, reason string) {
	tid := taskIDNormalize(taskID)
	cur := strings.TrimSpace(current)
	if tid == "" || cur == "" {
		return "", ""
	}
	if taskSessionRecordIsRowsOwn(tid, cur) {
		return "", ""
	}
	owner, ok := idx[cur]
	if !ok || owner == tid {
		// Not another row's run record, or this row's own: a submitter-shaped
		// value. Untouched.
		return "", ""
	}
	own := taskRunSessionFromRowDir(tid)
	if own == "" || own == cur {
		return "", ""
	}
	return own, fmt.Sprintf("%q is the run record of task %s (an executor stamp from another row); this row's own run directory holds %q",
		cur, owner, own)
}

// taskSessionRunIndex maps a session id to the ROW whose run directory holds
// its record (`sessions/<task-id>/<sid>.jsonl`) — built ONCE per pass, because
// the alternative (a stat per row per directory) is 1049 × 8737 probes on this
// host's profile. A session that no row directory holds simply misses, which is
// exactly the submitter-shaped value the repair refuses to touch.
//
// ONE WALK, ONE MEANING: the map is the same measurement
// taskSessionRecordIsRowsOwn makes for a single row, applied to the whole tree
// — a second "does this belong to a row" rule would be the drift this epic is
// about (Rule 14).
func taskSessionRunIndex() map[string]string {
	out := map[string]string{}
	gd, err := config.GopiDir()
	if err != nil {
		return out
	}
	root := filepath.Join(gd, config.SessionsDirName)
	ents, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		rowDir := filepath.Join(root, e.Name())
		files, ferr := os.ReadDir(rowDir)
		if ferr != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !sessionlayout.IsJSONL(f.Name()) {
				continue
			}
			sid := strings.TrimSuffix(f.Name(), sessionlayout.JSONLSuffix)
			// FIRST writer wins: a record name is minted per session, so a
			// collision would mean two rows claiming one session, and the
			// repair must not depend on ReadDir's order for it.
			if _, seen := out[sid]; !seen {
				out[sid] = e.Name()
			}
		}
	}
	return out
}

// taskSessionRepairTerminal answers whether a row's status is one the claim
// guard no longer judges — the ONLY states this pass rewrites (see the call
// site). It reuses the exported status values (Rule 1b) rather than spelling
// them.
func taskSessionRepairTerminal(status string) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled, TaskStatusVerified, TaskStatusSuperseded:
		return true
	}
	return false
}

// TaskSessionRepairRow is one row's repair verdict, rendered by the CLI face
// (and by the JSON arm) — never a second measurement in package main.
type TaskSessionRepairRow struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	Current  string `json:"current"`
	Repaired string `json:"repaired,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Applied is true only when the UPDATE for THIS row really landed.
	Applied bool `json:"applied"`
}

// TaskSessionRepairReport is the repair door's whole answer: what was found,
// what WOULD change (dry run) or DID change, and the rows left untouched with
// the reason they were left (Rule 15: "nothing to do" is a verdict, printed).
type TaskSessionRepairReport struct {
	DryRun     bool                   `json:"dry_run"`
	Scanned    int                    `json:"scanned"`
	Candidates []TaskSessionRepairRow `json:"candidates"`
	Skipped    []TaskSessionRepairRow `json:"skipped,omitempty"`
}

// TaskRepairSessionIDs is `gopi task update --repair-session [--dry-run]`
// (deliverable ③): run the ONE provenance predicate over the ledger's rows and,
// when apply is set, WRITE the row's own record into any column proven to carry
// another session's id.
//
// taskID narrows the pass to one row ("" = every row carrying a value). The
// write is a CAS on the observed value (`WHERE session_id = ?`), so a row that
// moved between the read and the write is reported as such rather than
// clobbered — the same discipline the ledger's other repairs use.
func TaskRepairSessionIDs(taskID string, apply bool) (TaskSessionRepairReport, error) {
	rep := TaskSessionRepairReport{DryRun: !apply}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return rep, err
	}
	defer db.Close()

	sel := `SELECT task_id, COALESCE(status,''), COALESCE(session_id,'') FROM tasks WHERE COALESCE(session_id,'') <> ''`
	var args []any
	if tid := taskIDNormalize(taskID); tid != "" {
		sel += ` AND task_id = ?`
		args = append(args, tid)
	}
	sel += ` ORDER BY updated_at DESC`
	rows, err := db.QueryContext(ctx, sel, args...)
	if err != nil {
		return rep, fmt.Errorf("task: repair-session: select: %w", err)
	}
	type rowImage struct{ id, status, sid string }
	var images []rowImage
	for rows.Next() {
		var r rowImage
		if err := rows.Scan(&r.id, &r.status, &r.sid); err != nil {
			rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
			return rep, fmt.Errorf("task: repair-session: scan: %w", err)
		}
		images = append(images, r)
	}
	rowsErr := rows.Err()
	rows.Close() //stylegate:ignore read-only cursor at end of iteration; the error carries no state to recover
	if rowsErr != nil {
		return rep, fmt.Errorf("task: repair-session: %w", rowsErr)
	}
	if taskIDNormalize(taskID) != "" && len(images) == 0 {
		return rep, fmt.Errorf("task: no task with task_id=%q (or it carries no session_id to judge)", taskIDNormalize(taskID))
	}

	rep.Scanned = len(images)
	idx := taskSessionRunIndex()
	for _, r := range images {
		newSID, reason := taskSessionRepairValue(ctx, db, idx, r.id, r.sid)
		if newSID == "" {
			// Every scanned row is REPORTED — a row that was read and left alone
			// is different from a row that was never looked at.
			kept := "row's own record (or nothing provably foreign)"
			if cur := strings.TrimSpace(r.sid); cur != "" && !taskSessionRecordIsRowsOwn(r.id, cur) {
				kept = "unverifiable: no container, no flat record, not in the chats index — left alone"
			}
			rep.Skipped = append(rep.Skipped, TaskSessionRepairRow{TaskID: r.id, Status: r.status, Current: r.sid, Reason: kept})
			continue
		}
		cand := TaskSessionRepairRow{TaskID: r.id, Status: r.status, Current: r.sid, Repaired: newSID, Reason: reason}
		// A REWRITE IS ONLY SAFE WHERE THE CLAIM GUARD IS MOOT. The column also
		// carries the SUBMITTER's session, and the anti-self-reference guard
		// refuses a claim when `session_id` equals the claiming session
		// (task_resume_session.go documents the semantics). Rewriting a LIVE
		// row's column could therefore hand a row to the session that ran it —
		// so an in_progress/pending row is REPORTED and never written: the pass
		// fixes history, it does not move work (epic constraint ⑦).
		if apply && !taskSessionRepairTerminal(r.status) {
			cand.Reason = reason + " — NOT WRITTEN: the row is live, and this column also carries the submitter (the claim guard compares it); repair it after the row is terminal"
			rep.Candidates = append(rep.Candidates, cand)
			continue
		}
		if apply {
			res, uerr := db.ExecContext(ctx,
				`UPDATE tasks SET session_id = ?, updated_at = ? WHERE task_id = ? AND session_id = ?`,
				newSID, float64(time.Now().Unix()), r.id, r.sid)
			if uerr != nil {
				return rep, fmt.Errorf("task: repair-session %s: %w", r.id, uerr)
			}
			if n, aerr := res.RowsAffected(); aerr == nil && n == 1 {
				cand.Applied = true
				taskEventWrite(ctx, db, r.id, taskSessionRepairEventName,
					fmt.Sprintf("session_id %q -> %q: %s", r.sid, newSID, reason))
			} else {
				cand.Reason = reason + " — the row's value moved before the write; nothing was overwritten"
			}
		}
		rep.Candidates = append(rep.Candidates, cand)
	}
	return rep, nil
}

// taskSessionRepairLines renders a report for the operator (the non-JSON arm):
// ONE line per row, naming what was found and what happened, so the pass is
// readable without a JSON reader.
func (r TaskSessionRepairReport) Lines() []string {
	mode := "APPLY"
	if r.DryRun {
		mode = "DRY RUN (nothing written — pass --apply)"
	}
	out := []string{fmt.Sprintf("[%s] repair-session: scanned %d row(s) carrying a session_id",
		taskSessionRepairEventName, r.Scanned)}
	for _, c := range r.Candidates {
		state := "WOULD REPAIR"
		if c.Applied {
			state = "REPAIRED"
		}
		out = append(out, fmt.Sprintf("  %s %s  %s  %q -> %q  (%s)", state, c.TaskID, c.Status, c.Current, c.Repaired, c.Reason))
	}
	for _, s := range r.Skipped {
		out = append(out, fmt.Sprintf("  kept %s  %s  %q  (%s)", s.TaskID, s.Status, s.Current, s.Reason))
	}
	if len(r.Candidates) == 0 {
		out = append(out, "  no row carries a provably foreign session_id — nothing to repair")
	}
	out = append(out, fmt.Sprintf("  mode: %s; candidates %d, kept %d", mode, len(r.Candidates), len(r.Skipped)))
	return out
}
