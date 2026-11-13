package tools

// PRE-SPAWN SINGLETON GUARD for a task run (epic task-run-singleton-guard,
// 2026-09-15). The epic's defect: two `gopi task run <id>` invocations could
// BOTH execute the same row's briefing. The ledger's status CAS was thought
// sufficient, but the CLAIMED window is not arbitrated for a CLAIMED row — the
// crash-recovery branch (Shape ② of TaskClaimExplicit, and TaskResumeClaimed)
// is a conditional UPDATE on status = claimed, so two runners racing a claimed
// row BOTH win honestly: both read 'claimed' before either writes, both
// re-stamp, then both advance the row and execute the briefing.
//
// THE FIX USES THE THREE-FACTOR IDENTITY THAT ALREADY EXISTS (epic
// task-process-identity, taskproc.go): pid + /proc start ticks + command line.
// But those facts were only captured at the claimed→in_progress transition, so
// a row in the CLAIMED window carried a bare `hostname:pid` stamp — which
// cannot be verified and cannot tell "the claimer died" (recoverable) from
// "the claimer is alive and about to execute" (a double-run). This file
// captures the identity AT CLAIM TIME as well, and decides, BEFORE the row is
// touched, whether a LIVE runner already owns it.
//
// THE RULE, faithfully the epic's: a holder whose three-factor identity names a
// live process that is NOT this one ⇒ this run REFUSES (exit non-zero). A row
// whose fields are missing, unreadable, foreign or recycled ⇒ this run
// PROCEDES and fills them in, so the NEXT runner can be refused. Proceeding is
// safe because the recovery CAS is pinned to the holder state just judged
// (taskRunHolderPin): of N racers on one unjudgeable claim, exactly ONE wins.
//
// THE DECISION IS ONE FUNCTION (taskRunHolderVerdict) with one implementation
// (Rule 14): the claimed-window pin, the pre-spawn door in `gopi task run`, and
// any future reader all call it, so no two of them can disagree about who
// holds a row.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

// taskRunHolder* are the verdicts of the pre-spawn singleton guard. Each name
// states WHO holds the row, not how it was computed — the caller acts on the
// name (proceed / refuse), and the refusal text is rendered from it.
const (
	// taskRunHolderFree: no live process owns the row. Either the row has no
	// usable identity (a legacy claim, a claimer that could not be read, a
	// recycled pid, a blank stamp) or the recorded process is provably gone.
	// The run PROCEEDS — this is the crash-recovery path the epic must not
	// break, and the pin below still makes the recovery single-winner.
	taskRunHolderFree = "free"

	// taskRunHolderSelf: the recorded identity IS this process. The
	// claim-window shape of a runner that claimed the row and is now
	// (re-)entering the run — a worker round passing its own claimed row
	// through, or the same process calling the door twice. Proceeds, and is
	// what keeps a runner from refusing ITS OWN claim.
	taskRunHolderSelf = "self"

	// taskRunHolderLive: a DIFFERENT live process owns the row, verified by
	// the three factors. This is the double-run the epic exists to stop: the
	// run REFUSES and exits non-zero (the shape the operator overrides with
	// --takeover when they have deliberately killed the run's session).
	taskRunHolderLive = "live-other"

	// taskRunHolderUnknown: the row's holder cannot be judged from this host —
	// a foreign claim stamp, a malformed stamp, a stale stamp of a previous
	// run, or an unreadable procfs. It is NOT evidence of a live holder, so the
	// run proceeds (Rule 15: evidence may only add a refusal, never fabricate
	// one) and the guard RECORDS its own identity, which is what makes the
	// next runner's answer definite.
	taskRunHolderUnknown = "unknown"
)

// taskRunHolderName* are the strategies for the holder stamp the guard writes
// when it admits a run. The stamp and the recorded identity must describe the
// SAME process (taskproc's Rule-15 note), so the choice is a value, not an
// inline decision at the call site.
const (
	// taskRunHolderNameChosen: the caller named its identity (the --as flag,
	// the worker's own ownership string) → that name, and its pid part is what
	// the identity must describe.
	taskRunHolderNameChosen = "chosen"

	// taskRunHolderNameSelf: the caller named nothing → this process's own
	// `hostname:pid`, the same default every other claim writer uses.
	taskRunHolderNameSelf = "self"
)

// Exported faces of the guard vocabulary (Rule 14: the CLI and the worker
// re-check compare the SAME constants the engine produces, never a literal).
const (
	// TaskRunHolderFree: no live process owns the row — the run proceeds.
	TaskRunHolderFree = taskRunHolderFree
	// TaskRunHolderSelf: the recorded holder IS this process — the run proceeds.
	TaskRunHolderSelf = taskRunHolderSelf
	// TaskRunHolderLive: a verified-live OTHER process owns the row — refuse.
	TaskRunHolderLive = taskRunHolderLive
	// TaskRunHolderUnknown: the holder is unjudgeable from this host — the run
	// proceeds (never a fabricated refusal) and records its own identity.
	TaskRunHolderUnknown = taskRunHolderUnknown
)

// taskRunGuardAdmits reports whether a verdict lets the run proceed. It is the
// SINGLE implementation of the admit/refuse decision (Rule 14) — the CLI door
// and the claim/recovery paths all read it, so none of them can disagree.
func taskRunGuardAdmits(verdict string) bool { return verdict != taskRunHolderLive }

// TaskRunGuardAdmits is the exported face of the admit decision.
func TaskRunGuardAdmits(verdict string) bool { return taskRunGuardAdmits(verdict) }

// taskRunHolderVerdict judges one row's holder for the run door: is a LIVE
// process already executing this row, and is it someone other than me?
//
// The degradation ladder is taskproc's (TaskProcessStatusFor): definite death
// and definite foreign liveness are answers; anything unjudgeable is "unknown",
// never a fabricated liveness (Rule 15). A row with NO recorded identity but a
// FRESH claim stamp on this host falls back to the pre-existing pid +
// comm-name probe — exactly the evidence the ledger reported before this epic,
// so a claimed row from an older build keeps the protection it had.
func taskRunHolderVerdict(t *Task) string {
	if t == nil {
		return taskRunHolderFree
	}
	// A claim stamp this process minted is this process's own run, whatever
	// the recorded pid says (a wrapper may claim under an inherited --as name
	// whose pid part belongs to its child — the stamp equality is the stronger
	// fact, and refusing here would refuse our own claim).
	if taskRunStampIsSelf(t.ClaimedBy) {
		return taskRunHolderSelf
	}
	_, pid, local := claimHolder(t.ClaimedBy)
	switch {
	case strings.TrimSpace(t.ClaimedBy) == "":
		// No stamp at all: nothing names an executor.
		return taskRunHolderFree
	case !local && !claimHostIsLocal(t.ClaimedBy):
		// Another host minted the stamp. Its pid means nothing here, and the
		// cross-node double-run is the M4.21 claim-lock protocol's business
		// (the run yields when the peer holds the lock) — not a refusal this
		// host would be inventing from absent evidence.
		return taskRunHolderUnknown
	case !local:
		// Our own hostname with an unparsable pid part: a broken stamp.
		return taskRunHolderUnknown
	}
	if t.HasProcIdentity {
		if t.ProcPid != pid || t.ProcPid <= 0 {
			// The recorded identity belongs to a different pid than the stamp
			// names (a re-stamp rewrote claimed_by without the identity), so
			// the record cannot judge this pid.
			return taskRunHolderUnknown
		}
		// The row's recorded process IS this process: its own run entering the
		// door (a worker passing its claimed row through, a second door call
		// in the same process). Checked against the STAMP as well as the pid,
		// so a row that merely happens to name our pid for another reason
		// still reads as ours — never as a stranger to refuse.
		if t.ProcPid == os.Getpid() {
			return taskRunHolderSelf
		}
		return taskRunProcessStateVerdict(taskRowProcessLiveness(t).State)
	}
	// Legacy shape (no identity columns): a stamp that PREDATES this row's
	// in_progress stretch names a previous claimant, so its pid says nothing
	// about the current run — that is the crash-recovery path, and refusing it
	// would break resume.
	if t.FreshnessStale() {
		return taskRunHolderFree
	}
	if pid == os.Getpid() {
		return taskRunHolderSelf
	}
	if !pidAlive(pid) {
		return taskRunHolderFree
	}
	if taskPidIsRunner(pid) {
		return taskRunHolderLive
	}
	// Alive but not a gopi process: a recycled pid. Nothing of ours runs.
	return taskRunHolderFree
}

// taskRunProcessStateVerdict maps the ledger's process-state verdict
// (taskproc.go) onto the guard's door decision. It is a pure translation of an
// already-decided verdict — the guard does NOT re-derive liveness, which is why
// `task status` and `gopi task run` can never disagree about a row (Rule 14).
//
//	dead / recycled → free  (the recorded runner is gone: recoverable)
//	running, pid==me → self (our own run entering the door)
//	running          → live (a LIVE OTHER runner: the door refuses)
//	no-holder        → free (nothing names an executor)
//	foreign/unknown  → unknown (unjudgeable here: proceed and record, never a
//	                          fabricated refusal — the cross-node lock protocol
//	                          and the pinned re-stamp own those shapes)
func taskRunProcessStateVerdict(state string) string {
	switch state {
	case TaskProcessStateDead, TaskProcessStateRecycled, TaskProcessStateNoHolder:
		return taskRunHolderFree
	case TaskProcessStateRunning:
		return taskRunHolderLive
	}
	return taskRunHolderUnknown
}

// taskRunStampIsSelf reports whether a claim stamp names THIS process
// (hostname:pid with our own pid) — the direct self-identification the guard
// uses before any /proc probing.
func taskRunStampIsSelf(claimedBy string) bool {
	if _, pid, ok := claimHolder(claimedBy); ok && pid == os.Getpid() {
		return true
	}
	return false
}

// TaskRunGuardFor is the run door's pre-spawn check for a row that may already
// be held: it returns the holder verdict, the refusal text when the run must
// NOT proceed, and a non-nil error only for a real failure (an absent row, an
// unreadable ledger). A row that is not in a live state (pending/terminal)
// answers "free" — the claim path owns those transitions, not this gate.
//
// It never writes: the door calls it BEFORE mutating the row, so a refused run
// leaves the ledger exactly as it found it (the same contract the claim CAS
// refusal has).
func TaskRunGuardFor(taskID string) (string, string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", "", fmt.Errorf("task: run guard: task_id must not be empty")
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return "", "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", "", fmt.Errorf("task: run guard: %w", err)
	}
	if t == nil {
		return "", "", fmt.Errorf("task: run guard: no task with task_id=%q%s", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	return taskRunGuardRuling(t)
}

// taskRunGuardRuling is the row-image half of TaskRunGuardFor (the
// claim/recovery paths hold a row and must not re-read the ledger).
func taskRunGuardRuling(t *Task) (string, string, error) {
	if !taskRunRowIsLive(t.Status) {
		return taskRunHolderFree, "", nil
	}
	verdict := taskRunHolderVerdict(t)
	if taskRunGuardAdmits(verdict) {
		return verdict, "", nil
	}
	return verdict, taskRunRefusalText(t, verdict), nil
}

// taskRunRowIsLive reports whether a row's status is one a runner can hold
// (claimed or in_progress) — the statuses the singleton guard judges. A
// pending row has no holder yet; a terminal row is finished.
func taskRunRowIsLive(status string) bool {
	return status == taskStatusClaimed || status == taskStatusInProgress
}

// taskRunRefusalText renders the operator-facing refusal: WHICH process holds
// the row, why the guard concluded so, and the ways forward. A refusal that
// does not name the holder would send the operator hunting through the ledger
// — the shape `gopi task status` exists to avoid, so the text points there too.
//
// THE PROGRESS AXIS IS PART OF THE REFUSAL (bug epic 01a1ac0f). A LIVE verdict
// alone reads as "somebody is working on this", and that reading is WRONG for
// the measured shape this epic was filed for: holder 518745 was alive for 5h18m
// with 24s of CPU and a transcript silent since 16:57:18, so an operator
// following the old text ("wait for the holder to finish") waited on a process
// that would never finish. The refusal therefore states the axis whenever it was
// measured, and prints the ONE command that recovers an idle row.
func taskRunRefusalText(t *Task, verdict string) string {
	var b strings.Builder
	if verdict == taskRunHolderLive {
		fmt.Fprintf(&b, "task %s is already RUNNING (holder %s", t.TaskID, orNone(t.ClaimedBy))
		if t.HasProcIdentity {
			fmt.Fprintf(&b, ", pid %d started %s", t.ProcPid, procStartTimeDisplay(t.ProcStartTicks))
		}
		b.WriteString(") — refusing to run it twice")
		b.WriteString(": the row carries a live holder's process identity (pid + start time + command line) verified against /proc, so this is a concurrent run, not a stale marker")
	} else {
		fmt.Fprintf(&b, "task %s is already claimed by %s", t.TaskID, orNone(t.ClaimedBy))
	}
	// The axis, when it could be measured. "live" and "working" are two
	// questions and this is the sentence that separates them.
	if verdict == taskRunHolderLive && t.HasProcIdentity {
		if p := taskProgressAxisFor(t, t.ProcPid, time.Now()); p.State != "" {
			fmt.Fprintf(&b, "; PROGRESS: %s — %s", p.State, p.Why)
		}
	}
	fmt.Fprintf(&b, "; inspect it with `gopi task status %s`, wait for the holder to finish, or pass --takeover to run it deliberately", t.TaskID)
	return b.String()
}

// orNone renders an empty ledger field as "(none)" for refusal text.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return strings.TrimSpace(s)
}

// taskRunHolderName resolves the holder stamp the guard writes when it admits
// a run: the caller's chosen name when it gave one, else this process's own
// hostname:pid. The caller must pair it with the pid the identity describes,
// or the row would claim one process and describe another.
func taskRunHolderName(chosen string) (string, string) {
	if s := strings.TrimSpace(chosen); s != "" {
		return s, taskRunHolderNameChosen
	}
	return taskDefaultClaimHolder(), taskRunHolderNameSelf
}

// taskRunIdentityForHolder captures the three-factor identity of the process a
// holder stamp names — the ONE place the CLAIM-time identity is captured,
// shared by every writer that stamps a claim (Rule 2). It mirrors the
// claimed→in_progress capture in taskUpdateStamped, including its degrade rule:
// a pid whose identity cannot be read records NOTHING rather than a zero-valued
// pseudo-identity, and a FOREIGN holder has no readable identity here
// (Rule 15).
func taskRunIdentityForHolder(holder string) (ident taskProcIdentity, ok bool) {
	pid := claimPidOf(holder)
	if pid <= 0 || !claimHostIsLocal(holder) {
		return taskProcIdentity{}, false
	}
	return taskCaptureProcIdentity(pid)
}

// taskRunIdentityArgs renders the identity as the three SQL argument values for
// the identity columns: a readable identity records pid/start/cmd, an
// unreadable one records NULL/NULL/” — never zeros, which the verdict ladder
// would read as a match against "pid 0" (the Rule-15 degrade rule shared with
// taskSelfIdentityValues).
func taskRunIdentityArgs(ident taskProcIdentity, ok bool) (pid any, start any, cmd any) {
	if !ok {
		return nil, nil, ""
	}
	return ident.Pid, ident.Start, ident.Cmd
}

// taskRunClaimIdentitySet renders the SET fragment and its bind values for the
// three identity columns of the process a holder names — the ONE place the
// claim writers compose that write (Rule 2), so the pending→claimed CAS, the
// claimed re-stamp and the worker's resumption all record the same shape.
func taskRunClaimIdentitySet(holder string) (string, []any) {
	pid, start, cmd := taskRunIdentityArgs(taskRunIdentityForHolder(holder))
	return tasksProcPidColumnName + " = ?, " + tasksProcStartColumnName + " = ?, " + tasksProcCmdColumnName + " = ?",
		[]any{pid, start, cmd}
}

// taskRunHolderPin renders the WHERE fragment and its bind values that pin a
// claim re-stamp to the holder state the guard just JUDGED: the row may only
// change hands while it still names the same holder and carries the same
// recorded identity. Without the pin, two operators recovering the SAME
// unjudgeable claim would both win their conditional UPDATE — both read
// 'claimed' before either wrote — and both execute the row. The pin is what
// makes the recovery a genuine single-winner CAS, closing the claimed window
// for identity-less rows exactly as the three-factor verdict closes it for
// rows that carry an identity.
//
// IFNULL matches the pointer-free comparison the rest of the ledger uses: the
// column is NULL on a row with no recorded identity, and the observed value is
// then 0.
func taskRunHolderPin(taskID string, observed *Task) (string, []any) {
	pinnedPid := 0
	observedBy := ""
	if observed != nil {
		pinnedPid = observed.ProcPid
		observedBy = strings.TrimSpace(observed.ClaimedBy)
	}
	return "task_id = ? AND status = ? AND claimed_by = ? AND IFNULL(" + tasksProcPidColumnName + ", 0) = ?",
		[]any{taskID, taskStatusClaimed, observedBy, pinnedPid}
}

// taskRunRestampClaim re-stamps a CLAIMED row's holder stamp and its
// claim-time process identity, PINNED to the holder state the caller just
// judged (taskRunHolderPin). applied=false with a nil error means the pin
// matched no row — the claim moved between the guard's read and this write, so
// ANOTHER runner recovered it first; the caller must REFUSE the run (proceed =
// double-run), never treat a lost CAS as success (Rule 15). It is the single
// implementation of the recover-a-claimed-row write (Rule 14), shared by
// TaskClaimExplicit's crash-recovery branch and TaskResumeClaimed.
func taskRunRestampClaim(ctx context.Context, db *sql.DB, observed *Task, holder string, now float64) (bool, error) {
	identSet, identArgs := taskRunClaimIdentitySet(holder)
	pinWhere, pinArgs := taskRunHolderPin(observed.TaskID, observed)
	args := append([]any{holder, now, now}, identArgs...)
	args = append(args, pinArgs...)
	res, err := db.ExecContext(ctx,
		`UPDATE tasks SET claimed_by = ?, claimed_at = ?, updated_at = ?, `+identSet+`
		 WHERE `+pinWhere,
		args...)
	if err != nil {
		return false, fmt.Errorf("task: claim recover: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("task: claim recover: %w", err)
	}
	return n == 1, nil
}

// taskRunGuardResolvable resolves the row a claim is about to mutate, so the
// guard can judge the holder BEFORE any write. An absent row is an error here
// (the claim path reports ghosts itself; the guard must not read "no row" as
// "free to run").
func taskRunGuardResolvable(db *sql.DB, taskID string) (*Task, error) {
	ctx := context.Background()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return nil, fmt.Errorf("task: claim: %w", err)
	}
	if t == nil {
		return nil, fmt.Errorf("task: claim: no task with task_id=%q%s — refusing to run a ghost id", taskID, taskIDPrefixRefusal(ctx, db, taskID))
	}
	return t, nil
}
