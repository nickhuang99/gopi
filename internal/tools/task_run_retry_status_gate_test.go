package tools

// task_run_retry_status_gate_test.go — the regression guard for the STATUS
// half of the resume gate (TaskRunChildResumable → TaskRunResumeStatusGate).
//
// THE TWO-LEVEL POLICY THIS PINS (operator ruling, 2026-10-29 — the whole
// statement is the header of task_run_retry.go). RESUME IS LEVEL 1 and it
// includes `failed`: a failure judges an ATTEMPT, and rerunning the same row
// (same task id, same session id, same directory) on a repaired tree is a new
// attempt, not a replay. A COPY (`gopi task clone` → `<id>-c1`) is level 2,
// the remedy after a resume has been tried.
//
// So the status gate REFUSES exactly the FINISHED work, and that is what these
// tests pin. The arm that used to be here asserted the opposite on `failed`
// (a refusal, "its raw log records a JUDGED run") — that reading conflated a
// judged attempt with finished work, and it is retired with the ruling.
//
// THE ARMS. The tests drive the REAL function (TaskRunChildResumable) over a
// real ledger, and every row is prepared so the HOLDER axis is the same in both
// directions — free — which is what makes the status the only variable:
//
//	① failed           + free holder → ADMIT (level 1: the attempt to retry)
//	② in_progress      + free holder → ADMIT (the shape a resume exists for)
//	③ cancelled        + free holder → ADMIT (externally stopped) — with the
//	                                    cross-host caveat asserted on the gate's
//	                                    own vocabulary, since a cancellation is
//	                                    also how a handed-over row is closed
//	④ success terminal + free holder → REFUSE, naming the status and the copy
//	⑤ pending/claimed  + free holder → ADMIT (nothing entered yet)
//	⑥ the LIVE arms are unchanged: running-self admits, running-other refuses,
//	  another host's stamp refuses.
//
// ⑥ is the guard against over-tightening: the status gate must not turn the
// holder arms into a different decision.

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// statusGateRow seeds one row and leaves it at status, with the holder trail in
// the shape that status produces in production (`in_progress` is reached
// through the real claim+start writers so its identity is real; a terminal
// status is reached through the real writeback, which CLEARS that identity —
// that clearing is exactly why a terminal row reads holder-free).
func statusGateRow(t *testing.T, taskID, status string) {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume status gate " + taskID,
		Payload: "fixture row",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	if err := TaskClaimExplicit(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("claim %s: %v", taskID, err)
	}
	if _, err := TaskStart(taskID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("start %s: %v", taskID, err)
	}
	if status == TaskStatusInProgress {
		return
	}
	// The writeback face only accepts completed|failed (it is the RUN's own
	// close-out); every other terminal is reached through the ledger's state
	// machine, which is the writer a cancelled/verified/superseded row really
	// comes from.
	if status == TaskStatusCompleted || status == TaskStatusFailed {
		if _, err := TaskRunWriteback(taskID, status, "fixture: "+status); err != nil {
			t.Fatalf("writeback %s → %s: %v", taskID, status, err)
		}
		return
	}
	if status == TaskStatusVerified {
		// `verified` is reachable only THROUGH completed (the ledger's state
		// machine: in_progress → completed|failed|cancelled|superseded, then
		// completed → verified), so the row walks the real path.
		if _, err := TaskUpdateReason(taskID, TaskStatusCompleted, "fixture: completed", ""); err != nil {
			t.Fatalf("update %s → completed: %v", taskID, err)
		}
	}
	if _, err := TaskUpdateReason(taskID, status, "fixture: "+status, ""); err != nil {
		t.Fatalf("update %s → %s: %v", taskID, status, err)
	}
}

// statusGateForgeHolderlessInProgress is the SHAPE a `gopi worker --resume`
// meets after a crash: the row says in_progress and NOTHING names a live
// holder (no claim stamp, no recorded identity). It is built with the
// production column names (Rule 13) because no writer produces it — it is the
// absence the crash leaves behind.
func statusGateForgeHolderlessInProgress(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = '', claimed_at = NULL, `+tasksInProgressAtColumnName+` = NULL,
		 `+tasksProcPidColumnName+` = NULL, `+tasksProcStartColumnName+` = NULL, `+tasksProcCmdColumnName+` = ''
		 WHERE task_id = ?`,
		taskStatusInProgress, taskID); err != nil {
		t.Fatalf("forge holderless in_progress %s: %v", taskID, err)
	}
}

// TestTaskRunChildResumableAdmitsFailedRow is LEVEL 1 of the policy: a `failed`
// row with no live holder must be ADMITTED, and the row must be prepared so the
// only variable is the status — the holder axis really is free.
func TestTaskRunChildResumableAdmitsFailedRow(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-gate-failed"
	statusGateRow(t, taskID, TaskStatusFailed)

	// The HOLDER axis really is free — otherwise this test would pass for the
	// wrong reason (the pre-existing refusal arms) instead of pinning the new one.
	if st, err := TaskProcessStatusFor(taskID); err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	} else if st.Verdict != taskProcessStateNotApplicable {
		t.Fatalf("fixture precondition: verdict = %q, want %q — the failed row must be holder-free", st.Verdict, taskProcessStateNotApplicable)
	}

	ok, verdict := TaskRunChildResumable(taskID, os.Getpid())
	if !ok {
		t.Fatalf("a failed row with no live holder was REFUSED (%q) — a failure judges one attempt, and level 1 is to resume that attempt", verdict)
	}
	// The admission must still NAME its holder evidence (an admitted verdict is
	// not a silent true: the caller renders it as the handover's reason).
	if verdict != taskProcessStateNotApplicable {
		t.Errorf("verdict = %q, want %q — the ADMITTED answer still names what the holder axis measured", verdict, taskProcessStateNotApplicable)
	}
	// And the status axis must not manufacture a refusal sentence for it.
	if st, err := TaskProcessStatusFor(taskID); err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	} else if st.StatusRefusal != "" {
		t.Errorf("the status axis refused a failed row: %q", st.StatusRefusal)
	}
}

// TestTaskRunChildResumableAdmitsInProgressWithoutALiveHolder is the
// counter-case the fix must not break: the crash-recovery shape — in_progress,
// nobody alive — is exactly what a resume is for, so it stays ADMITTED.
func TestTaskRunChildResumableAdmitsInProgressWithoutALiveHolder(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-gate-in-progress"
	statusGateRow(t, taskID, TaskStatusInProgress)
	statusGateForgeHolderlessInProgress(t, taskID)

	ok, verdict := TaskRunChildResumable(taskID, os.Getpid())
	if !ok {
		t.Fatalf("an in_progress row with no live holder was refused (%q) — that is the crash-recovery path a resume exists for", verdict)
	}
	if verdict != TaskProcessStateNoHolder {
		t.Fatalf("verdict = %q, want %q — the admitted verdict must still NAME the holder evidence", verdict, TaskProcessStateNoHolder)
	}
}

// TestTaskRunChildResumableAdmitsCancelledRow: a CANCELLED row was stopped from
// outside (an operator, a drain) — no run judged the work — so continuing it
// replays no verdict and is admitted.
func TestTaskRunChildResumableAdmitsCancelledRow(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-gate-cancelled"
	statusGateRow(t, taskID, TaskStatusCancelled)

	if ok, verdict := TaskRunChildResumable(taskID, os.Getpid()); !ok {
		t.Fatalf("a cancelled row was refused (%q) — it was stopped externally, and no run of its own finished the work", verdict)
	}
	// AND THE CAVEAT IS ON THE GATE'S OWN VOCABULARY (the one thing an operator
	// must read before resuming a cancelled row): a cancellation is also how a
	// row is closed when its work was HANDED TO ANOTHER NODE, and resuming that
	// one here is a cross-host double run. The gate still ADMITS it — it cannot
	// know which cancellation this is — so the warning has to be in the words the
	// reader reaches, naming the command that answers the question.
	clause := taskRunResumeStatusGateCancelledCaveat
	lower := strings.ToLower(clause)
	for _, want := range []string{"another node", "double run", "gopi task diff --peer"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the cancelled admission's caveat does not carry %q:\n%s", want, clause)
		}
	}
	// And it is the sentence the STATUS VIEW actually renders on an admitted
	// cancelled row — a caveat nobody prints is a comment, not a warning.
	if got := taskRunResumeAdmissionCaveat(TaskStatusCancelled); got != clause {
		t.Errorf("the status view's caveat for cancelled = %q, want the named sentence", got)
	}
	if got := taskRunResumeAdmissionCaveat(TaskStatusFailed); got != "" {
		t.Errorf("a failed row carries a caveat (%q) — its admission is unconditional (level 1)", got)
	}
}

// TestTaskRunChildResumableRefusesEveryOtherStatus pins the refused half of the
// admitted set: every FINISHED terminal (success, or retired by supersession) is
// refused, and every refusal names the status. "Finished" is the criterion, not
// "judged" — a failure is judged too, and it is ADMITTED (level 1), which is the
// distinction the retired framing of this gate got wrong.
func TestTaskRunChildResumableRefusesEveryOtherStatus(t *testing.T) {
	testutil.NewTestDBPath(t)
	for _, status := range []string{
		TaskStatusCompleted, TaskStatusVerified, TaskStatusSuperseded,
	} {
		t.Run(status, func(t *testing.T) {
			taskID := "t-gate-" + status
			statusGateRow(t, taskID, status)
			ok, refusal := TaskRunChildResumable(taskID, os.Getpid())
			if ok {
				t.Fatalf("status %q was ADMITTED — finished work must never be resumed, only copied (gopi task clone)", status)
			}
			if !strings.Contains(refusal, status) {
				t.Errorf("the refusal does not name the status %q:\n%s", status, refusal)
			}
		})
	}
}

// TestTaskRunChildResumableHolderArmsUnchanged is the anti-regression half of
// the epic's constraint: the LIVE / foreign arms decide exactly as they did.
// The status is in_progress on every row here, so the new gate is not the
// variable — a change in any of these outcomes is a change to the old arms.
func TestTaskRunChildResumableHolderArmsUnchanged(t *testing.T) {
	testutil.NewTestDBPath(t)

	// ① a row whose recorded identity IS this process: running-self → ADMIT
	// (the handing-over wrapper), and the verdict says so.
	const selfID = "t-gate-self"
	procIdentitySeedRow(t, selfID, hostPid(t, os.Getpid()))
	ok, verdict := TaskRunChildResumable(selfID, os.Getpid())
	if !ok {
		t.Fatalf("the row's own live holder was refused (%q) — the handover arm must admit", verdict)
	}
	if !strings.Contains(verdict, TaskProcessStateRunning+"-self") {
		t.Fatalf("verdict = %q, want the running-self arm", verdict)
	}

	// ② the same row asked by ANOTHER pid: a verified-live foreign runner →
	// REFUSE, whatever the status says.
	if ok, verdict := TaskRunChildResumable(selfID, os.Getpid()+1); ok {
		t.Fatalf("a live holder that is NOT the caller was admitted (%q) — that is the double-run", verdict)
	} else if verdict != TaskProcessStateRunning {
		t.Fatalf("verdict = %q, want %q (the holder arm must keep naming its own evidence)", verdict, TaskProcessStateRunning)
	}

	// ③ another host's claim stamp: unverifiable here → REFUSE.
	const foreignID = "t-gate-foreign"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume status gate " + foreignID, Payload: "fixture row",
		TaskID: foreignID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	now := float64(1234567890)
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ? WHERE task_id = ?`,
		taskStatusInProgress, "another-host.example:4242", now, now, foreignID); err != nil {
		t.Fatalf("forge foreign stamp: %v", err)
	}
	if ok, verdict := TaskRunChildResumable(foreignID, os.Getpid()); ok {
		t.Fatalf("a foreign-held row was admitted (%q) — this host cannot prove that run is over", verdict)
	} else if verdict != TaskProcessStateUnknown {
		t.Fatalf("verdict = %q, want %q (a foreign stamp is not measurable here)", verdict, TaskProcessStateUnknown)
	}
}

// TestTaskProcessStatusCarriesTheStatusRefusal pins the SECOND surface: the
// `gopi task status` view must answer with the same decision (Rule 14), so an
// operator reading the verb and the mechanism taking the decision cannot
// disagree about one row. Both facts stay visible at once: the holder verdict
// says n/a (the ledger's own answer), and the status refusal says why a resume
// is still not available.
func TestTaskProcessStatusCarriesTheStatusRefusal(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-gate-view"
	statusGateRow(t, taskID, TaskStatusCompleted)

	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		t.Fatalf("status %s: %v", taskID, err)
	}
	if st.Verdict != taskProcessStateNotApplicable {
		t.Errorf("verdict = %q, want %q (the holder answer is unchanged by this fix)", st.Verdict, taskProcessStateNotApplicable)
	}
	if st.Resumable {
		t.Fatalf("resumable = true on a completed row — the verb would tell an operator to resume finished work")
	}
	if !strings.Contains(st.StatusRefusal, TaskStatusCompleted) {
		t.Fatalf("status_refusal does not name the status:\n%q", st.StatusRefusal)
	}
	report := TaskProcessStatusReport(st)
	if !strings.Contains(report, "resumable:   no") {
		t.Errorf("the report does not render the refusal as `resumable: no`:\n%s", report)
	}
	if !strings.Contains(report, "gopi task clone") {
		t.Errorf("the report does not render the way forward:\n%s", report)
	}
	// The JSON face carries it too (a script reads the field, not the prose).
	blob, err := TaskProcessStatusJSON(st)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if !strings.Contains(blob, `"status_refusal"`) {
		t.Errorf("the JSON face drops the status refusal:\n%s", blob)
	}
}

// TestTaskRunChildResumableAdmitsPendingAndClaimed pins the OTHER side of the
// rule: a row with NO run (pending) and a row in the admission window
// (claimed) carry nothing judged, so the status gate leaves them alone. This
// is the arm that keeps the gate narrow — a dispatch surface reading a pending
// row as "unresumable" would tell an operator to re-enter a row that was never
// entered.
func TestTaskRunChildResumableAdmitsPendingAndClaimed(t *testing.T) {
	testutil.NewTestDBPath(t)
	const pendingID = "t-gate-pending"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume status gate " + pendingID, Payload: "fixture row",
		TaskID: pendingID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if ok, verdict := TaskRunChildResumable(pendingID, os.Getpid()); !ok {
		t.Fatalf("a pending row was refused (%q) — it carries nothing judged, and the dispatch surfaces read this answer", verdict)
	}

	const claimedID = "t-gate-claimed"
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "resume status gate " + claimedID, Payload: "fixture row",
		TaskID: claimedID, Source: "human",
	}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := TaskClaimExplicit(claimedID, hostPid(t, os.Getpid())); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if ok, verdict := TaskRunChildResumable(claimedID, os.Getpid()); !ok {
		t.Fatalf("a claimed row was refused (%q) — the claim window is unjudged work", verdict)
	}
}

// TestTaskProcessStatusUnjudgedRowsKeepResumable is the guard for the shape the
// status axis nearly broke: the axis may only ever make the answer STRICTER.
// The early-return arm (a row with no holder to judge) sets Resumable=true as
// the holder axis's own answer, and the status axis runs AFTER it — if it were
// ever written to overwrite the field rather than restrict it, every pending
// row would read `resumable: no` and the dispatch surfaces would start telling
// operators to "re-enqueue" rows that were never enqueued.
func TestTaskProcessStatusUnjudgedRowsKeepResumable(t *testing.T) {
	testutil.NewTestDBPath(t)
	for _, tc := range []struct{ id, status string }{
		{"t-view-pending", TaskStatusPending},
		{"t-view-in-progress", TaskStatusInProgress},
		{"t-view-cancelled", TaskStatusCancelled},
	} {
		tc := tc
		t.Run(tc.status, func(t *testing.T) {
			if _, err := TaskSubmit(TaskSubmitOptions{
				Summary: "unjudged row " + tc.id, Payload: "fixture row", TaskID: tc.id, Source: "human",
			}); err != nil {
				t.Fatalf("submit: %v", err)
			}
			if tc.status != TaskStatusPending {
				// Walk the ledger's legal ladder to the target status.
				steps := []string{TaskStatusInProgress}
				if tc.status != TaskStatusInProgress {
					steps = append(steps, tc.status)
				}
				for _, step := range steps {
					if _, err := TaskUpdateReason(tc.id, step, "fixture", ""); err != nil {
						t.Fatalf("update %s: %v", step, err)
					}
				}
			}
			st, err := TaskProcessStatusFor(tc.id)
			if err != nil {
				t.Fatalf("status %s: %v", tc.id, err)
			}
			if !st.Resumable {
				t.Fatalf("resumable = false on an UNJUDGED row (%s) — the status axis may only restrict a judged row", tc.status)
			}
			if st.StatusRefusal != "" {
				t.Fatalf("status_refusal set on an unjudged row (%s): %q", tc.status, st.StatusRefusal)
			}
		})
	}
}

// TestResumeOfAFailedRowKeepsItsIdentity is LEVEL 1, MEASURED END TO END: the
// resume of a failed row continues THAT row. Same task id, same session id, and
// no `<id>-r1` successor anywhere in the ledger — because the successor is the
// COPY (level 2), and the whole argument for resuming first is that a copy
// throws away the context this row already has.
//
// The two writes it drives are the ones a resume really makes:
//   - the gate ADMITS the failed row (TaskRunChildResumable, the mechanism), and
//   - the ledger's resume edge `failed → in_progress` moves the SAME row
//     (taskAllowedTransitions; a copy would instead be a NEW pending row).
//
// It then reads the ledger back rather than trusting the writes: a status that
// moved, an id that did not, a session that did not, and no `-r` sibling.
func TestResumeOfAFailedRowKeepsItsIdentity(t *testing.T) {
	testutil.NewTestDBPath(t)
	const taskID = "t-resume-failed-identity"
	statusGateRow(t, taskID, TaskStatusFailed)

	// The row carries a SESSION (the run that failed): the resume must keep it.
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	const sessionID = "11111111-2222-3333-4444-555555555555"
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET session_id = ? WHERE task_id = ?`, sessionID, taskID); err != nil {
		t.Fatalf("stamp session: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the fixture handle before the mechanism opens the ledger itself: %v", err)
	}

	// LEVEL 1 is admitted by the mechanism.
	if ok, refusal := TaskRunChildResumable(taskID, os.Getpid()); !ok {
		t.Fatalf("the gate refused a failed row (%q) — level 1 is a resume of the same row", refusal)
	}
	// The LEDGER's resume edge exists, and it is the same row.
	if _, err := TaskUpdateReason(taskID, TaskStatusInProgress, "resume: level 1", ""); err != nil {
		t.Fatalf("failed → in_progress was refused: %v — the resume edge is missing from taskAllowedTransitions", err)
	}

	after, err := taskByID(context.Background(), mustTestDB(t), taskID)
	if err != nil || after == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	if after.Status != TaskStatusInProgress {
		t.Fatalf("status = %q, want %q — the resume continues this row", after.Status, TaskStatusInProgress)
	}
	if after.TaskID != taskID {
		t.Fatalf("task_id = %q, want %q — a resume must NOT mint a new id", after.TaskID, taskID)
	}
	if after.SessionID != sessionID {
		t.Fatalf("session_id = %q, want %q — a resume appends to the SAME session's checkpoint", after.SessionID, sessionID)
	}
	// AND NO COPY WAS MADE: the -rN successor is level 2's spelling, and its
	// absence is what makes "resume" and "reissue" different acts in the ledger.
	rows, err := mustTestDB(t).QueryContext(context.Background(),
		`SELECT task_id FROM tasks WHERE task_id LIKE ?`, taskID+"-%")
	if err != nil {
		t.Fatalf("list successors: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Fatalf("a resume minted the successor row %q — that is the COPY (level 2), and a resume must not create one", id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
}

// mustTestDB opens the test ledger for the READ-BACK above (one opener, so the
// test never hand-rolls a path — Rule 32).
func mustTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTestTasksDB(t, memoryDBPath())
}
