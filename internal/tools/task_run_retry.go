package tools

// task_run_retry.go — the LEDGER half of the wrapped-executor-child retry
// (epic: bounded automatic retry of an abnormally-exited child, 4/5).
//
// THE POLICY: TWO LEVELS, RESUME FIRST (operator ruling, 2026-10-29). A run
// that did not deliver is handled in TWO steps, and the order is the ruling —
// a copy is the EXCEPTION, never the reflex:
//
//	       a run ended without delivering
//	                    │
//	                    ▼
//	 ┌──────────────────────────────────────────────────────────────┐
//	 │ LEVEL 1 — RESUME  (the default, and it includes a `failed` row)│
//	 │   same task id · same session id · same directory              │
//	 │   the CHECKPOINT IS APPENDED TO, never replaced                │
//	 │   `gopi session resume <task-id>`  /  `gopi worker --resume`   │
//	 └──────────────────────────────────────────────────────────────┘
//	                    │  it ran and STILL did not deliver
//	                    ▼
//	 ┌──────────────────────────────────────────────────────────────┐
//	 │ LEVEL 2 — COPY  (the remedy, and it says so)                  │
//	 │   NEW task id (a FRESH uuid) · NEW session · NEW workspace     │
//	 │   `gopi task clone <id>`  (a clean room carrying the work)     │
//	 └──────────────────────────────────────────────────────────────┘
//
// WHY LEVEL 1 IS THE DEFAULT, INCLUDING FOR `failed`. A failure is a fact about
// ONE run, not a proof about the task: the binary was stale, the bug it hit has
// since been fixed, the network dropped, the model read the wrong file. Rerunning
// the SAME row with today's tree is not a replay of the old verdict — it is a
// new attempt at the same work, and it costs nothing but the attempt. The old
// rule ("a resume would replay a judged run") mistook `failed` for `finished`:
// the verdict judged that RUN, and the work it was about is exactly what has not
// been delivered yet.
//
// WHY A COPY IS THE SECOND LEVEL AND NOT THE FIRST. `clone` does not continue
// anything — it mints a FRESH uuid, a new row with a new session and a new
// directory — and it records what it copies in the row's own tags (`clone` +
// `rework-of:<source id>`), not in the id. (Before operator ruling 2026-11-05,
// memory #27273, it minted `<id>-c1` and each further attempt `<id>-c1-c1`; a
// suffix is not an identity, and those rows are read, never written.) Do that
// reflexively
// and the history of one piece of work becomes a chain of ids nobody can
// enumerate, while the context that already exists (the checkpoint, the
// half-written fix, the commits) is abandoned every time. A copy is right in
// exactly ONE situation: level 1 has been TRIED and has failed again, so the
// workspace itself is now evidence that it is dirty — then a clean one is worth
// its cost.
//
// WHAT THIS FILE ACTUALLY GATES (and the two arms that make level 1 safe): a
// resumed run must never race a second run of the same row (the holder arm,
// below), and it must never start on a row another HOST may be running right
// now (the host arm, taskproc.go). Neither arm cares whether the work was
// judged; both ask whether somebody else is already doing it.
//
// THE GAP THIS CLOSES. `gopi task run` waitpids its executor child and, on a
// non-zero exit, writes the row failed and walks away. That is right when the
// child JUDGED ITS TASK and failed it, and wrong when the child died
// ABNORMALLY — killed mid-run, or bailing before its own close-out ran —
// because then the row's failure is only the WRAPPER's report of a crash and
// the work in flight (a half-written fix, a worktree full of commits, a session
// checkpoint holding the whole context) is abandoned by default. The wrapper
// resumes the SAME session through the existing recovery entry point; THIS file
// holds the facts that make that resume BOUNDED, SURVIVABLE and SAFE:
//
//   - the attempt counter is an APPEND-ONLY ORDINAL TAG on the row
//     (`run-retry:<n>`), the precedent the dead-holder arm set with
//     `dead-holder-resume:<n>`: never a mutable column, so a wrapper that
//     crashes between two spawns cannot outrun its own cap by forgetting how
//     many times it already tried — the count lives in the ledger, not in the
//     process that died.
//   - the cap is a named value with a NAMED handover reason, so the ladder ends
//     in an operator decision instead of an unbounded restart loop.
//   - the double-run question is asked BEFORE the spawn, so a retry that would
//     race a live run is refused with its reason named rather than discovered
//     inside the child.
//
// WHY A SECOND TAG NAMESPACE and not the dead-holder one: the two counters
// count DIFFERENT acts (the merger resuming a provably-dead holder vs a run
// wrapper resuming its own crashed child). Sharing a prefix would let one arm's
// attempts consume the other's budget and would hide which mechanism ran out.
// Two namespaces, two caps, one implementation shape.

import (
	"context"
	"fmt"
	"strings"
)

const (
	// TaskRunRetryCap is how many times the `gopi task run` wrapper may
	// automatically resume the SAME row after an ABNORMAL child exit. Three
	// matches the other bounded ladders in this package (the merger's
	// dead-holder arm, the verify-retry lane): one attempt for a transient
	// death plus the retries a genuinely recoverable run needs, and no more.
	TaskRunRetryCap = 3

	// taskRunRetryTagPrefix labels the attempt counter tags. It must NOT be a
	// prefix of any other counter (`dead-holder-resume:`) — see the header.
	taskRunRetryTagPrefix = "run-retry:"

	// taskRunRetryTagScanLimit bounds how many tag entries a single read
	// scans: a row's tag string is operator/model-writable, so the reader is
	// bounded by construction rather than by trust (Rule 1: a named limit,
	// never an inline one).
	taskRunRetryTagScanLimit = 512

	// taskRunRetryCapReasonFmt is the NAMED handover recorded on the row when
	// the ladder is exhausted. It is a format (not a literal) so the number an
	// operator reads is the cap itself — a reworded message can never drift
	// from the constant that bounded the loop.
	taskRunRetryCapReasonFmt = "abnormal-child retry cap reached (%d attempts spent): the wrapped executor child exited abnormally on every attempt " +
		"without closing the row itself — no further automatic resume was started. The run's own record (the child's session log) holds the cause; " +
		"this row is handed over for an operator decision (re-dispatch it as a fresh task, or resume it deliberately with `gopi session resume`)."
)

// TaskRunRetryCapReason renders the cap handover reason with the cap folded in
// (one renderer, so the row's reason and any test that asserts it agree).
func TaskRunRetryCapReason() string {
	return fmt.Sprintf(taskRunRetryCapReasonFmt, TaskRunRetryCap)
}

// TaskRowStatusAt reports the row's ledger status — the fact the retry
// classifier reads at the child's exit INSTANT (before the wrapper's own
// writeback lands). A read failure is returned, never flattened into a wrong
// status: the caller must be able to tell "open" from "could not ask".
func TaskRowStatusAt(taskID string) (string, error) {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task: row status: task_id must not be empty")
	}
	ctx := context.Background()
	// READ-ONLY OPENER — this face is a PURE READ: its SELECTs are the whole of
	// what it does, and it calls no writer (openTasksDBReadOnly skips the tasks DDL
	// batch and takes no BEGIN IMMEDIATE, so a reader never queues on the writer
	// that holds the ledger).
	db, err := openTasksDBReadOnly(ctx, memoryDBPath())
	if err != nil {
		return "", err
	}
	defer db.Close()
	t, err := taskByID(ctx, db, taskID)
	if err != nil {
		return "", fmt.Errorf("task: row status: %w", err)
	}
	if t == nil {
		return "", fmt.Errorf("task: row status: no task with task_id=%q", taskID)
	}
	return t.Status, nil
}

// TaskRowTerminalAt reports whether the row the child just exited from was
// ALREADY TERMINAL at that moment, and which status closed it.
//
// THIS IS THE DISCRIMINATOR. The child that JUDGED its task closes the row
// itself on its way out (its END hook / its own writeback), so a non-zero exit
// on a row that is already terminal is a VERDICT — a real task failure, whose
// retry would repeat a judgement rather than recover work. A child that died
// ABNORMALLY never got that far: the row is still in_progress/claimed, and the
// wrapper's writeback below is the only reason it will read `failed` at all.
// The distinction therefore needs no new marker in the ledger: it is the
// difference between "the row was closed before I looked" and "I am the one
// closing it".
func TaskRowTerminalAt(taskID string) (terminal bool, status string, err error) {
	st, err := TaskRowStatusAt(taskID)
	if err != nil {
		return false, "", err
	}
	return taskTerminalStatus(st) || st == taskStatusCancelled || st == taskStatusSuperseded, st, nil
}

// TaskRunRetryCounts reads the row's append-only retry trail and returns the
// highest ordinal recorded (how many attempts the ledger knows about). The
// MAXIMUM is what bounds the ladder, deliberately: an unparsable or duplicated
// ordinal must never authorise one more retry.
func TaskRunRetryCounts(taskID string) (int, error) {
	tags, err := TaskTags(taskID)
	if err != nil {
		return 0, err
	}
	return taskRunRetryCountsFromTags(tags), nil
}

// taskRunRetryCountsFromTags is the pure half (Rule 14: the reader and the
// tests share ONE parse).
func taskRunRetryCountsFromTags(tags string) int {
	spent, scanned := 0, 0
	for _, tg := range strings.Split(tags, ",") {
		scanned++
		if scanned > taskRunRetryTagScanLimit {
			break
		}
		tg = strings.TrimSpace(tg)
		if !strings.HasPrefix(tg, taskRunRetryTagPrefix) {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(strings.TrimPrefix(tg, taskRunRetryTagPrefix), "%d", &n); err != nil {
			continue // an unparsable ordinal is not evidence of an attempt
		}
		if n > spent {
			spent = n
		}
	}
	return spent
}

// TaskRunRetryNote appends ONE attempt tag carrying the next ordinal, so the
// trail reads in order rather than as N identical entries. Idempotent per
// ordinal: a wrapper that dies after spawning but before this write re-does
// that ordinal harmlessly, and the ladder still advances because the NEXT
// attempt's ordinal is higher.
func TaskRunRetryNote(taskID string) error {
	taskID = taskIDNormalize(taskID)
	if taskID == "" {
		return fmt.Errorf("task: retry note: task_id must not be empty")
	}
	spent, err := TaskRunRetryCounts(taskID)
	if err != nil {
		return err
	}
	ctx := context.Background()
	db, err := openTasksDB(ctx, memoryDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = taskAddTag(ctx, db, taskID, taskRunRetryTagPrefix+fmt.Sprint(spent+1))
	return err
}

// TaskRunChildResumable reports whether a resume of this row would be ADMITTED
// by the double-run gate, naming the holder verdict it judged. The wrapper asks
// BEFORE the spawn (the pre-flight shape the dead-holder arm uses).
//
// selfPID IS THE CALLER'S OWN PID and it is load-bearing, not a convenience:
// the wrapper about to quit is, by construction, the row's recorded HOLDER (it
// claimed and started the row minutes ago). A verdict of "running" on that
// stamp is therefore not a competing run — it is the handover itself, and
// judging it live would make the retry refuse in exactly the situation it
// exists for. So: a live holder that IS this process → admit (this is the
// handoff); a live holder that is ANOTHER process, or a foreign/unreadable
// stamp → refuse, because a resume could then race a run that really exists.
func TaskRunChildResumable(taskID string, selfPID int) (bool, string) {
	st, err := TaskProcessStatusFor(taskID)
	if err != nil {
		return false, fmt.Sprintf("holder verdict unreadable (%v)", err)
	}
	switch st.Verdict {
	case TaskProcessStateRunning:
		if selfPID > 0 && st.HolderPid == selfPID {
			return true, st.Verdict + "-self (the handing-over wrapper)"
		}
		return false, st.Verdict
	case TaskProcessStateForeign, TaskProcessStateUnknown:
		return false, st.Verdict
	}
	// THE HOLDER IS FREE — and that is only HALF the question. The arms above
	// answer "may a second process enter this row"; this one answers "is there
	// any UNFINISHED work here for a resume to continue". A free holder is true
	// of every finished row (the terminal transition clears the identity), so
	// without this gate a `completed` row was admitted by `gopi worker --resume`
	// and rerun as if it were an interrupted one. Note the asymmetry with the
	// original defect: the status this gate REFUSES is `completed`/`verified`,
	// not `failed` — a failed attempt is exactly the work still to be done
	// (the two-level policy in the file header).
	if admitted, refusal := TaskRunResumeStatusGate(taskID, st.Status); !admitted {
		// THE SECOND AXIS, and it is asked HERE for the same reason it is asked
		// on the status view: a status column states what the ROW claims, and
		// "finished" is a claim about the WORK that a status cannot make alone.
		// TaskProcessStatus already carries the verdict (taskResumeDeliveryAxis
		// fills DeliveryVerdict/DeliveryWhy), so this wrapper reads the ONE
		// computation instead of running the git probes a second time — the
		// two faces that admit a resume therefore cannot disagree.
		//
		// An UNKNOWN verdict keeps the refusal: the admission rule is EVIDENCE
		// of owed work, never the absence of a measurement (see
		// task_resume_delivery_axis.go's header).
		if taskResumeDeliveryStatusMayBeOverridden(st.Status) && st.DeliveryVerdict == TaskResumeDeliveryUnlanded {
			return true, st.Verdict + " delivery=" + st.DeliveryVerdict
		}
		return false, refusal
	}
	return true, st.Verdict
}

// TaskRunResumeStatusGate is the STATUS half of the resume gate: given the
// row's ledger status (with the holder already known to be free), may a
// resume be admitted, and if not, WHY — in a sentence that names the status,
// the reason a resume is wrong for it, and the documented way forward.
//
// It is ONE function so the two surfaces that ask the question cannot
// disagree (Rule 14): TaskRunChildResumable asks it before a wrapper resumes
// its crashed child, and taskProcessStatus (the `gopi task status` view)
// folds its answer into `resumable:` — the operator reading the verb and the
// mechanism taking the decision must be reading the same decision.
//
// THE ADMITTED SET IS EVERY STATUS WHOSE WORK IS NOT FINISHED, and each has a
// reason:
//
//   - in_progress — the shape a resume exists for: a run was interrupted
//     mid-flight (killed, crashed, host went down) and its session checkpoint
//     holds work nobody has finished.
//
//   - failed — LEVEL 1 OF THE TWO-LEVEL POLICY (see the file header): a failure
//     is a verdict on ONE run, and rerunning the SAME row on a repaired tree is
//     a new attempt, not a replay. It is admitted DELIBERATELY, and the
//     remedy it names when it is finally exhausted is a COPY (level 2), not a
//     second resume.
//
//   - cancelled — an EXTERNAL stop (an operator, a drain, a superseding
//     decision): no run of its own judged the work. **BUT READ THE WARNING
//     BEFORE RESUMING ONE**: "cancelled" is also how a row is closed when its
//     work was HANDED TO ANOTHER NODE — pulled, pushed, or re-dispatched there,
//     so that the local copy stops competing for the ground. Continuing THAT
//     row here is not a recovery, it is a SECOND RUN of the same work on two
//     hosts, and nothing in it will ever converge (two ledgers, two branches,
//     two reports for one task id). Check before you resume:
//
//     gopi task diff --peer <alias>          # does the peer's ledger hold this row, moved on?
//
//     A peer copy in claimed/in_progress/completed is that handover; resume the
//     row on the host that owns it instead (see `gopi task status --peer`).
//
//   - pending / claimed — no run exists yet. There is nothing to replay and
//     nothing to duplicate, so a resume here is not the defect this gate closes; it is kept
//     admitted DELIBERATELY, because the dispatch surfaces read this answer (a
//     pending row that read unresumable would tell an operator to "re-enter" a
//     row that was never entered).
//
// Everything else is refused, and the reason is the same one for all of them:
// the work IS finished (completed/verified) or has been retired (superseded), so
// there is nothing left to resume and a copy would duplicate delivered work.
// THE RULE IN ONE SENTENCE: a row may be resumed while its work is UNFINISHED —
// `failed` included, because an attempt that failed is not work that is over.
func TaskRunResumeStatusGate(taskID, status string) (bool, string) {
	if taskRunResumeStatusAdmits(status) {
		return true, ""
	}
	return false, taskRunResumeRefusalText(taskID, status)
}

// taskRunResumeStatusAdmits is the predicate alone (the status view needs the
// answer without the sentence): every status whose work is not finished.
func taskRunResumeStatusAdmits(status string) bool {
	switch status {
	case TaskStatusInProgress, TaskStatusFailed, TaskStatusCancelled, TaskStatusPending, TaskStatusClaimed:
		return true
	}
	return false
}

// taskRunResumeStatusGateCancelledCaveat is the warning printed beside an
// ADMITTED cancelled row — the one status this gate admits whose honest reason
// may be the opposite of "safe to resume".
//
// The gate CANNOT decide this itself: "cancelled" carries no reason (the ledger
// stores the operator's free text, if any), so the same status covers "an
// operator stopped it, continue it here" and "this work was handed to another
// node, stop competing for the ground". The second one resumed here is a
// CROSS-HOST DOUBLE RUN — two agents, two workspaces, one task id, and no
// convergence afterwards (two ledgers, two branches, two reports) — so the
// warning is not optional prose: it is printed on the row's own status view, and
// it names the command that answers the question.
const taskRunResumeStatusGateCancelledCaveat = "cancelled is ADMITTED, but check FIRST: a row is also cancelled when its work was " +
	"HANDED TO ANOTHER NODE (pushed/pulled/re-dispatched there) — resuming one of those here is a cross-host DOUBLE RUN of the same task id. " +
	"Ask before you resume: gopi task diff --peer <alias>  (a peer copy in claimed/in_progress/completed IS that handover)"

// taskRunResumeAdmissionCaveat is the caveat a status carries WHEN ADMITTED
// ("" for the statuses that carry none), so both resume surfaces can print the
// one sentence the same way instead of retyping it (Rule 1b).
func taskRunResumeAdmissionCaveat(status string) string {
	if status == TaskStatusCancelled {
		return taskRunResumeStatusGateCancelledCaveat
	}
	return ""
}

// taskRunResumeRefusalFmt is the NAMED refusal of the status gate. It is a
// format because the row id and the status are the facts the operator needs
// (Rule 1: no assembled prose at the call site): the row id is the operand of
// BOTH ways forward, so a reader can copy either line verbatim.
//
// WHO SEES THIS AT ALL, NOW THAT `failed` IS ADMITTED: only a row whose work is
// already finished (completed/verified) or retired (superseded), plus a status
// this vocabulary does not know. So the remedies are the LEVEL-2 ones on
// purpose — a fresh named attempt at new work — and never "resume it again",
// because for these rows there is nothing left to resume.
//
// THE REMEDIES ARE MEASURED, NOT ASSUMED. `gopi task clone` is the COPY: a NEW
// task id (a FRESH uuid — never a suffix of the source id), a NEW session, a NEW
// workspace, carrying the work and the
// row's verdict in its briefing — the ledger's append-only shape (a decided row
// keeps its decision). It is LEVEL 2 of the two-level policy in the file header:
// reach for it when a RESUME has already been tried, or when the recorded
// workspace is itself the problem. `gopi task submit` is the same act for work
// that is not a continuation of this row at all.
//
// (For a `failed` row the LEVEL-1 remedy is not printed here — it is simply
// admitted: `gopi session resume <id>` continues the same row, same session,
// same directory, appending to its checkpoint. The ledger spelling of the same
// move is `gopi task update <id> --status in_progress`, which the state machine
// now permits from `failed` for exactly this reason.)
const taskRunResumeRefusalFmt = "task %[1]s is %[2]s — %[3]s. Two ways forward:\n" +
	"  gopi task clone %[1]s                    (level 2 — COPY: a fresh pending row under a NEW uuid, new session, new workspace)\n" +
	"  gopi task submit \"...\"                  (a fresh task of your own briefing)"

// The per-status reason clauses of the refusal. They are separate names so the
// vocabulary lives in one place and any other surface (the usage text, a test)
// quotes the same words instead of retyping them (Rule 1b).
const (
	// taskRunResumeRefusalFinished: the work landed (or was retired). This is
	// the whole of the refusal set now that `failed` is level 1.
	taskRunResumeRefusalFinished = "this work is FINISHED, not interrupted — there is no unfinished attempt for a resume to continue, " +
		"and continuing it would duplicate delivered work"
	// taskRunResumeRefusalUnknownStatus is the fallback clause for a status this
	// vocabulary does not know (a row written by a newer build, a hand-forged
	// value): the refusal still states the rule instead of guessing which side
	// of it the status is on.
	taskRunResumeRefusalUnknownStatus = "a resume continues work that is not finished, " +
		"and this status is not one this verb may resume — inspect the row before re-entering it"
)

// taskRunResumeRefusalClause picks the reason clause for a refused status.
func taskRunResumeRefusalClause(status string) string {
	switch status {
	case TaskStatusCompleted, TaskStatusVerified, TaskStatusSuperseded:
		return taskRunResumeRefusalFinished
	}
	return taskRunResumeRefusalUnknownStatus
}

// taskRunResumeRefusalText renders the full refusal for one row.
func taskRunResumeRefusalText(taskID, status string) string {
	return fmt.Sprintf(taskRunResumeRefusalFmt, taskID, status, taskRunResumeRefusalClause(status))
}
