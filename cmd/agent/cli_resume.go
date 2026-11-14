package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/cliverbs"
	"github.com/nick/gopi/internal/closeouthandoff"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/gopilog"
	"github.com/nick/gopi/internal/runcmd"
	"github.com/nick/gopi/internal/rundir"
	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/sessionlayout"
	"github.com/nick/gopi/internal/signalrec"
	"github.com/nick/gopi/internal/tools"
	"github.com/nick/gopi/internal/worktreerelease"
)

// The cross-guidance fragments of the session resume page (Rules 1b/13/14): the
// command spellings come from internal/cliverbs (the leaf that owns them), and
// the two phrases that are PURE PROSE — they name no command — live here beside
// the page they are printed on.
const (
	// resumeTaskRowAddressSpelled is the worker-layer command this page points
	// at, backticked for the page's own vocabulary. The command itself comes
	// from internal/cliverbs (the leaf that owns the spelling).
	resumeTaskRowAddressSpelled = "`" + cliverbs.ResumeTaskRowAddress + "`"
	// resumeTaskRowAdvice is the paragraph's opening claim: WHICH face a row
	// belongs to. One clause per line, because a help page is measured in
	// DISPLAY columns (cmd/agent/usage_test.go holds every rendered line to the
	// page width) and the clause carries a composed command inside it.
	resumeTaskRowAdvice = "A TASK ROW is resumed with " + resumeTaskRowAddressSpelled +
		", not with this verb:\n" +
		"that face takes over the row's existing ground and runs the close-out\n" +
		"chain that " + resumeReleasePhrase + "\n" +
		resumeOnlyRouteAdvice
	// resumeReleasePhrase names WHAT the worker face does that this one does
	// NOT — the whole reason an operator has to pick (Rule 15).
	resumeReleasePhrase = "RELEASES its worktree; a resume here leaves it registered"
	// resumeChatShapes names the two routes that have NO worker behind them —
	// the reason this verb still exists at all.
	resumeChatShapes = "`gopi chat`, a bare `gopi \"<prompt>\"`"
	// resumeOnlyRouteAdvice is the paragraph's second half, under the same
	// width rule.
	resumeOnlyRouteAdvice = "for good.\n" +
		"Use this one for what has no row (" + resumeChatShapes + "),\n" +
		"or for a row's raw log when no ground is involved."
	// resumeSessionIDNoRowPhrase is the fact a session-id resume must state
	// (Rule 15): the target links no ledger row.
	resumeSessionIDNoRowPhrase = "A resume by SESSION ID names no ledger row — a uuid is not a task id,\n" +
		"and the resume continues session-only and says so. Link it with --task <id>\n" +
		"when the run must be recorded against a row."
)

// resumeUsage is the `gopi session resume -h` text.
//
// THE PAGE'S THIRD PARAGRAPH IS THE CROSS-GUIDANCE (PROJECT.md Rule 15: the choice
// between the two resume faces must be stated where the choice is made). It is
// assembled from the constants below, which are built from internal/cliverbs'
// shared spellings of the two commands — so a verb rename moves this page with
// it (Rules 13/14), and the worker page's mirror sentence and `gopi task show`'s
// advice name the same commands.
const resumeUsage = `Usage: gopi session resume <task-id|raw-log-path|session-id> [continuation-file]

Resume an interrupted session from its raw HTTP log checkpoint.

` + resumeTaskRowAdvice + `

` + resumeSessionIDNoRowPhrase + `

<task-id>           a task id (e.g. from 'gopi task list --status in_progress'):
                    the row's own session checkpoint is located automatically AND
                    the row is re-linked, so recovering a killed run needs no
                    session-id hunting. This is the zombie-recovery path, AND it
                    is LEVEL 1 of the failure policy (internal/tools/
                    task_run_retry.go): a FAILED row is RESUMED, not replaced —
                    same task id, same session id, same directory, checkpoint
                    appended — because a failure judges one ATTEMPT, not the work.
                    A COPY of the row (gopi task clone, which mints a FRESH
                    uuid and gives it a NEW session and workspace — the lineage
                    lives in the row's tags, clone + rework-of:<source id>,
                    never in the id) is LEVEL 2: the remedy after a
                    resume has been tried again, never the first move.

                    Two gates, on two different facts:
                      * the row's PROCESS IDENTITY — a verified-LIVE holder
                        refuses (no double run); a dead/recycled/no-holder row is
                        reclaimed; a holder whose claim stamp names ANOTHER HOST
                        reads UNKNOWN and refuses, because this machine cannot
                        judge that pid;
                      * the row's STATUS — FINISHED work (completed / verified /
                        superseded) refuses, and the refusal names gopi task
                        clone. A CANCELLED row is admitted WITH a printed
                        caveat: a cancellation is also how a row is closed when
                        its work was handed to another node, and resuming that
                        one here is a cross-host double run — check with
                        gopi task diff --peer <alias> first.
                    Inspect first with 'gopi task status <task-id>' (exit 0
                    resumable, 1 running, 2 ground in use, 3 unverifiable,
                    4 no such row, 5 status forbids a resume).

                    WHERE THE CHECKPOINT IS LOOKED UP — the target names a RUN
                    DIRECTORY, <gopi dir>/sessions/<target>/, and the checkpoint
                    is the run log inside it (the raw log that log records, or a
                    co-located <run>.raw.log). Two consequences worth knowing:
                      * the lookup is a FILESYSTEM read, so it does not need the
                        profile's ledger to hold the row: with GOPI_HOME pointed
                        at another profile, a task id whose run directory is
                        there resolves even though no row of that id exists here;
                      * the tag/linkage half (the resumed:<session-id> stamp, the
                        row's terminal status, the worktree resume) DOES need the
                        row — that half is skipped with a warning when the ledger
                        does not hold it, and the resume still runs.

<raw-log-path>      path to a ~/.gopi/raw/gopi-raw-*.log file (or to a raw log
                    re-homed inside its session directory). The LAST
                    "→ REQUEST" line in the log is a byte-exact full-context
                    checkpoint (system prompt + GOPI.md + conversation history
                    + thinking params). That context is rebuilt and the ReAct
                    loop continues from where the interrupted run left off.

<session-id>        a session UUID (e.g. from 'gopi session log --last'): the matching
                    session's raw log is located automatically via the
                    session_start timestamp — and since a session's run
                    directory is NAMED by its uuid, this is the same
                    filesystem address a task id resolves by. A session
                    uuid is not a task id, so it links NO ledger row: the
                    resume continues the work session-only and says so
                    ('names no ledger row (…)'). Link a row explicitly with
                    --task <id> when the run must be recorded against one.

[continuation-file] optional file whose contents become the user's next
                    instruction. Omitted: the model is told to continue from
                    where it left off without repeating completed steps.

Examples:
  gopi session resume 01a096c9-6f7d-7e43-8e46-9ecca58e9cd0
  gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log
  gopi session resume ~/.gopi/sessions/01a096c9-6f7d-7e43-8e46-9ecca58e9cd0/....raw.log
  gopi session resume 4c9a1f3e-8b2d-4a10-9e6f-2c5d7a0b1e33
  gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log next-step.md
  gopi session resume t-1788048877
  gopi session resume t-1788048877 --task t-1788048877
      (the row's own id — the same target, so --task is redundant here; it is
      the explicit form, and the one to use when the TARGET is a raw log and
      the row to link is a different task)
  gopi session resume ~/.gopi/raw/gopi-raw-20260826-120000.log --task t-1788048877
      --task links the continuation to a ledger task: the resumed run's NEW
      session id is stamped into the task row as a "resumed:<session-id>"
      tag, so 'gopi task show <task_id>' reports the resume lineage (the
      failed→resume→re-dispatch dependency). Given a task id as the TARGET it
      is implied — the target IS the task — and the flag is then only a way to
      say so explicitly.
      --task may stand on EITHER side of the target: 'gopi session resume
      <target> --task <id>' and 'gopi session resume --task <id> <target>' are
      the same invocation, and the target is never mistaken for the flag (nor
      the flag for the target) in either spelling. A -flag is never the thing
      to resume; a line with no target at all is refused by name.
      A RUN flag placed BEFORE the verb (e.g. gopi --timeout 3 resume ...)
      is consumed by the run path, which reads the rest of the line as its
      prompt — the verb (and with it --task) is never reached.
`

// isResumeSubcommand reports whether argv begins with the `resume`
// subcommand. When it does it runs the subcommand and returns true (the
// caller must not treat the remaining args as an input file).
func isResumeSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == CliVerbResume {
		os.Exit(runResume(args[1:]))
	}
	return false
}

// resumeGate* are the pre-flight decisions of the resume's admission gates.
const (
	// resumeGateProceed: nothing stands in the way — the resume may run. Covers
	// the reclaim cases (dead / recycled / no-holder) and the no-row case (a
	// plain session resume carries no task).
	resumeGateProceed = iota
	// resumeGateHolderLive: the LEDGER says a process with the row's recorded
	// identity is alive on this host. It is NOT a refusal any more (epic
	// resume-gate-is-the-lock, 2026-11-05) — it is a CLAIM the run-scope kernel
	// lock is asked to arbitrate, because "pid 518745 is in /proc" and "pid
	// 518745 is executing this run directory" are different facts and only the
	// second one is double-running.
	//
	// MEASURED (PA, 2026-11-05 22:12 and 22:17, same evening, verbatim same
	// refusal): a holder that had run 5h18m with 24s of CPU, 36 threads all in
	// futex, session.jsonl frozen at 16:57:18 — zero progress, the refusal was
	// WRONG; and a holder 2m25s into a live round — the refusal was RIGHT. The
	// old gate could not tell them apart because it only asked the first
	// question.
	//
	// SO THE DECISION MOVED TO THE KERNEL: a flock cannot be held by a process
	// that is not running, and it is released the instant that process ends
	// (`kill -9` included) with no sweep. Holding the lock IS "this directory
	// is not being executed", and the caller (resumeSession) reports this claim
	// as OVERRIDDEN with its pid and the reason. When no lock can be taken at
	// all — a checkpoint outside this profile's sessions root — the kernel has
	// said nothing and the claim stands as a refusal (Rule 15: never resolve an
	// unknown in the admitting direction).
	resumeGateHolderLive
	// resumeGateRefuseRunning: the LIVE claim with NOTHING to arbitrate it —
	// the checkpoint is not in a run directory of this profile, so no lock could
	// be taken and the kernel said nothing. The claim then stands as a refusal
	// (resumeDecideWithLock is the ONE place this fold happens). It is also what
	// the gate answer reads when a caller asks about the ledger alone.
	resumeGateRefuseRunning
	// resumeGateRefuseUnknown: the holder cannot be judged from this host
	// (foreign claim stamp, stale stamp, unreadable procfs) → refuse, and
	// tell the operator which command answers the question. UNAFFECTED by the
	// lock: a foreign claim means the run may be live on ANOTHER HOST, whose
	// run directory this process's flock by definition does not cover.
	resumeGateRefuseUnknown
	// resumeGateRefuseStatus: the holder is free, but the row's LEDGER STATUS
	// says the work is FINISHED — refuse, and name the copy (`gopi task
	// clone`). A separate code from the ones above because the remedy is a
	// different act: not "wait", not "the stamp", but "this work is done; a
	// new attempt is a new row". Also unaffected by the lock: the lock answers
	// "is it executing", never "is the work over".
	//
	// `failed` is NOT this code. A failure judges one ATTEMPT, so the resume of
	// a failed row is LEVEL 1 of the policy (internal/tools/task_run_retry.go)
	// and is admitted here — through the SAME gate the worker's resume reads
	// (tools.TaskRunResumeStatusGate), so the two faces cannot disagree.
	resumeGateRefuseStatus
)

// decideResumeByTaskID asks the ledger's tri-factor verdict whether a
// --task resume may proceed. It is deliberately a SEPARATE call from the
// resume itself so the decision is testable without running an agent, and so
// every refusal path prints the same "check with gopi task status" advice.
//
// An EMPTY task id (no --task) and a nonexistent row both proceed: a plain
// session resume has no holder to verify, and an unknown row cannot be
// double-run by this process (the resolution step reports it).
func decideResumeByTaskID(taskID string) int {
	if strings.TrimSpace(taskID) == "" {
		return resumeGateProceed
	}
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil {
		// The row does not exist (or the ledger is unreadable): nothing is
		// verified-live, so the gate does not block. The error surfaces on
		// the ledger-linkage path, where it belongs.
		return resumeGateProceed
	}
	switch st.Verdict {
	case tools.TaskProcessStateRunning:
		// A CLAIM, not a verdict (epic resume-gate-is-the-lock): the caller
		// folds it with the run-scope lock's answer (resumeDecideWithLock).
		return resumeGateHolderLive
	case tools.TaskProcessStateForeign, tools.TaskProcessStateUnknown:
		return resumeGateRefuseUnknown
	}
	// THE SECOND GATE, on a DIFFERENT FACT (devdoc/resume-on-failed.md's
	// "status-blind" posture is retired): the holder being free says nobody is
	// running the row; it does not say the work is unfinished. `TaskProcessStatus`
	// already computes that on the status axis (StatusRefusal) through the ONE
	// predicate the worker's resume reads — this face just has to ACT on it
	// instead of ignoring it, which is what it did while the usage text claimed
	// FINISHED work was refused.
	if st.StatusRefusal != "" {
		return resumeGateRefuseStatus
	}
	return resumeGateProceed
}

// resumeDecideWithLock folds the run-scope lock's verdict into the ledger's
// admission gates. It is the ONE place the two facts meet, so the swarm of
// callers (the switch in resumeSession, and any test that drives the decision)
// cannot each invent the fold.
//
// THE RULE, and it is asymmetric on purpose:
//
//   - the LIVE holder claim is DOWNGRADED to an admission when the kernel
//     ARBITRATED this run directory (resumeTakeRunLock took its flock). The
//     ledger asked whether that pid exists; the lock answers the question a
//     resume actually has — is anything executing this directory — and the
//     kernel cannot be wrong about it. This is the epic's fix.
//   - with NO lock taken, the claim stays a REFUSAL (resumeGateRefuseRunning):
//     the override needs a measurement, and "no directory to lock" is the
//     absence of one. Admitting there would be resolving an unknown in the
//     admitting direction — the shape Rule 15 forbids.
//   - everything else passes through untouched: a FOREIGN/UNKNOWN holder and a
//     FINISHED status are facts the lock cannot measure (another host's run
//     directory is not this process's to lock; a status says whether the WORK is
//     over, not whether a process is running).
//
// CONCURRENCY IS NOT WEAKENED BY THIS, and that is criterion ② of the epic: a
// second process cannot reach this fold at all, because resumeTakeRunLock has
// already refused it at the flock (runlock.Lock → ErrHeld, naming the holder).
// The override only ever fires for a caller that HOLDS the directory.
func resumeDecideWithLock(taskID string, arbitrated bool) int {
	gate := decideResumeByTaskID(taskID)
	if gate == resumeGateHolderLive && !arbitrated {
		return resumeGateRefuseRunning
	}
	return gate
}

// resumeReportHolderOverride is the ADMISSION side of the holder gate (Rule 15):
// the ledger names a live process for this row and the resume proceeds anyway,
// so the operator must see WHICH pid was overridden and on what evidence —
// otherwise an admitted-by-lock resume is indistinguishable from one whose
// holder was simply dead, and the next reader would conclude the gate was
// deleted.
//
// The evidence sentence is the LOCK, stated as the fact it is: this process
// holds the run-scope flock, which the kernel grants to exactly one holder. It
// deliberately does not editorialise about the pid's CPU time or its thread
// state — those are how the measured incident was DIAGNOSED, not the criterion;
// the criterion is the lock.
func resumeReportHolderOverride(w io.Writer, prog, taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil {
		return
	}
	if st.Verdict != tools.TaskProcessStateRunning {
		// Nothing was overridden: the row's holder is not a live identity, so
		// there is no claim to report (the ordinary reclaim path says nothing
		// extra — an announcement there would claim a measurement that decided
		// nothing).
		return
	}
	if strings.TrimSpace(prog) == "" {
		prog = resumeProgName
	}
	fmt.Fprintf(w, "%s: task %s records a LIVE holder (pid %d) and resuming anyway — this process now holds the run-scope lock on its run directory, and the kernel grants that lock to one process, so nothing is executing it (the pid is alive but not running this run; the lock is released the instant a holder dies).\n",
		prog, taskID, st.RecordedPid)
}

// resumeReportDeliveryAdmission prints the SECOND axis' evidence when the gate
// admitted a row whose status said "finished" — the one shape where a resume
// proceeds because the DELIVERY axis overrode the status.
//
// WHY IT IS A SECOND READ rather than a value threaded back out of
// decideResumeByTaskID: that shape keeps the gate's signature (an int code the
// switch reads) and matches resumeStatusWord right above, which already re-reads
// the row for the refusal sentence — one row, one process, one inspection verb,
// and the read is a read-only ledger open (no lock, no write). Threading a struct
// back through the switch would change the gate's contract for every existing
// test that drives it to print one sentence after it.
//
// It prints NOTHING unless the admission really came from the delivery axis: an
// ordinary admitted row (in_progress, failed, cancelled, pending, claimed) has
// no override to report, and a caveat printed there would be noise claiming a
// measurement that never decided anything.
func resumeReportDeliveryAdmission(w io.Writer, prog, taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil {
		return
	}
	if st.DeliveryVerdict != tools.TaskResumeDeliveryUnlanded {
		return
	}
	if strings.TrimSpace(prog) == "" {
		prog = resumeProgName
	}
	fmt.Fprintf(w, "%s: task %s resumed although its status reads %s — %s\n",
		prog, taskID, st.Status, st.StatusCaveat)
}

// resumeProgressNote renders the PROGRESS AXIS as the sentence a refused
// resume deserves (bug epic 01a1ac0f).
//
// WHY THE REFUSAL NEEDS IT. The gate above refuses on a verified-LIVE holder,
// and "live" alone reads as "somebody is working on this — wait". For the
// measured shape this epic was filed for, that reading is FALSE and expensive:
// holder 518745 was alive 5h18m with 24s of CPU and a transcript silent since
// 16:57:18, so an operator following the old text waited on a process that
// would never finish. So when the axis is measured and says `idle`, the refusal
// carries the axis and the ONE command that recovers the row.
//
// It prints NOTHING in the ordinary case (axis running, or unmeasurable): a
// sentence there would be noise, and an unmeasured axis must not be dressed as
// a finding (Rule 15). The empty string is why this returns "" rather than a
// default sentence.
func resumeProgressNote(taskID string) string {
	if strings.TrimSpace(taskID) == "" {
		return ""
	}
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil || !st.Progress.Idle() {
		return ""
	}
	return fmt.Sprintf("  PROGRESS: %s — %s\n", st.Progress.State, st.Progress.Why)
}

// resumeReportRefuseRunning renders the refusal for a verified-LIVE holder that
// NO run lock arbitrated, and the sentence is a UNION of two epics that met on
// this exact line — neither half is optional.
//
//   - CONDITION (epic resume-gate-is-the-lock): "live" alone stopped being a
//     refusal on 2026-11-05, so this case now means the ledger claims a live
//     holder AND no run lock could be taken to arbitrate the claim. The earlier
//     wording would tell the operator the pid decided it, which is the bug that
//     epic closed.
//   - PROGRESS AXIS (bug epic 01a1ac0f, resumeProgressNote): whether the holder
//     is progressing or has been idle for hours. That axis is measured
//     independently of the lock and is what tells an operator whether waiting is
//     even possible — the 5h18m/24s-of-CPU incident that epic was filed for.
//
// Both facts are true at once: the lock answers "is anything EXECUTING this
// directory", the progress axis answers "is it DOING anything". A merge that
// keeps only the first deletes a measured operator affordance; one that keeps
// only the second tells the operator the pid arbitrated a claim it does not
// arbitrate. TestResumeRefuseRunningSentenceCarriesBothAxes is the check that
// fails if either half is dropped (it asserts resumeProgressNote's slot is
// filled by a real marker, so a format-string edit cannot silently lose it).
func resumeReportRefuseRunning(w io.Writer, prog, taskID string) {
	if strings.TrimSpace(prog) == "" {
		prog = resumeProgName
	}
	fmt.Fprintf(w, "%s: refusing — task %s records a verified-LIVE holder and NO run lock could be taken on its run directory, so nothing arbitrated the claim; resuming could run it twice.\n"+
		"%s"+
		"  Check with: gopi task status %s\n  (if that process is not the run you think it is, the row is reclaimable — cancel/close it first)\n",
		prog, taskID, resumeProgressNote(taskID), taskID)
}

// resumeStatusWord names the row's current status for the refusal sentence,

// read from the same call the gate just made (one source — Rule 14). An
// unreadable row yields the generic word rather than an empty one, because a
// refusal that prints "is FINISHED ()" tells the operator nothing.
func resumeStatusWord(taskID string) string {
	st, err := tools.TaskProcessStatusFor(taskID)
	if err != nil || st == nil || strings.TrimSpace(st.Status) == "" {
		return "its status forbids a resume"
	}
	return st.Status
}

// resumeTargetWasTaskID records that args[0] resolved as a TASK id (not a
// path/session id), so runResume can auto-link the row without the operator
// repeating it as --task. Package-level because the resolution happens in a
// helper whose signature the tests drive directly; it is set once per
// process by the single resume entry point.
var resumeTargetWasTaskID bool

// resumeTargetRowID names the ledger ROW a task-mode target resolved to —
// set ONLY on the branch that resolved through the row VIEW
// (tools.TaskResumeTarget), and empty on the run-directory branch, which
// reads a run log off the filesystem WITHOUT ever asking whether a row of
// that id exists.
//
// WHY THE DISTINCTION IS LOAD-BEARING (epic resume-session-target-latch).
// "The target looks like a task id" and "the ledger holds this row" are two
// different facts, and the second one is a PRECONDITION of adopting: adoption
// (adoptResumeLedgerRow → Agent.AdoptLedgerRow) suppresses the run's own auto
// row in favour of the adopted one, which is only sound if that row is really
// there. A run DIRECTORY is named by a session uuid just as often as by a task
// id — every bare/chat session gets
// <gopi dir>/sessions/<session-uuid>/session.jsonl plus its co-located raw log
// (internal/agent/session.go sessionDirForID + rundir.RunDirRawLog) — so
// `gopi session resume <session-uuid>` lands on that branch with a perfectly
// resolvable checkpoint and NO row anywhere in the ledger. Adopting the session
// uuid there is adopting a row that can never exist: it reports success, the
// start hook's latchWrapperRow() then returns true and the auto mirror is
// suppressed, the fresh session is tagged onto the nonexistent row (warning),
// and the end hook closes it (warning) — three [gopi] stderr lines and a
// resumed run with no ledger record at all (Rule 15).
var resumeTargetRowID string

// resumeRowReasonRunDir is the reason reported when a target resolved through
// the run directory rather than the ledger's task-id view — the reason a
// session-only run is what it is, in one spelling (Rule 2).
const resumeRowReasonRunDir = "resolved from its run directory, not from the task-id view"

// resumeNoRowNoteFmt is the ONE spelling of "this target names no ledger row":
// the honest, operator-facing form of the decision — never an error (the
// resume itself is fine: the checkpoint is real and the run proceeds). Rule 15
// forbids the alternative that used to happen here (adopting a row that cannot
// exist, then leaking three warnings and ending ledger-less).
//
// WHAT IT MAY NOT CLAIM — measured, not assumed. "This run keeps its own
// ledger row" would be FALSE on this path: the start hook is documented
// non-resume-only (`if !resume { maybeRegisterSessionTask }`, loop.go) and
// adoption was the only thing that ever set the end hook's gate on a resume, so
// a session-only resume ends with NO row of its own either (verified against
// the sandbox profile: the fixture row is the ONLY row the ledger holds after
// the resumed run exits 0). The note says exactly that, and names the remedy
// (`--task <id>`) instead of implying a row that is not there.
//
// `%s: this target names no ledger row (%s) — continuing session-only: nothing
// is adopted, linked or closed (a resume CONTINUES work; it does not dispatch a
// row). Pass --task <id> to link the resume to a row.`
const resumeNoRowNote = "names no ledger row"
const resumeNoRowNoteFmt = "%s: this target " + resumeNoRowNote + " (%s) — continuing session-only: nothing is adopted, linked or closed (a resume CONTINUES work; it does not dispatch a row). Pass --task <id> to link the resume to a row."

// reportResumeNoRow prints the note above: the target, the ledger's own reason
// when there is one, and what the run DOES instead of latching.
func reportResumeNoRow(target, reason string) {
	line := fmt.Sprintf(resumeNoRowNoteFmt, target, reason)
	fmt.Fprintf(os.Stderr, "[gopi] resume: %s\n", line)
}

// resolveResumeTarget extends resolveResumeLogPath with the task-id target
// (epic task-process-identity): `gopi session resume <task-id>` — the recovery path
// for a killed run, with no session-id hunting. A target that names a task
// row whose run directory holds a checkpoint is resolved HERE; a target that
// names NO row falls through to the pre-existing path/session-id resolution
// untouched (so every existing invocation keeps working byte-for-byte —
// including a CHAT target, which has no ledger row and is resolved by session
// id / bare layout below).
//
// A ROW THAT NAMES NO CHECKPOINT IS A HARD ERROR (2026-10-28, Rule 15). It is
// NOT routed into the fallbacks: they answer about a STRING (a directory name,
// a session uuid), and the target here is a ROW. The measured defect they used
// to produce — the row's `session_id` column naming the SUBMITTER chat, so the
// "recovery" continued an unrelated conversation — is documented on
// tools.TaskResumeTarget. The order is therefore: row view (resolve or refuse),
// run directory, path/session id.
//
// THE TWO TASK-MODE BRANCHES ARE NOT THE SAME FACT (epic
// resume-session-target-latch), and the difference decides whether the run may
// ADOPT a row — see resumeTargetRowID. The ROW VIEW (resumeTaskRawLog) answers
// "this id is a row"; the RUN-DIRECTORY branch (resumeRunLog) answers only
// "this id names a directory holding a checkpoint", which a session uuid does
// too. Only the first sets an adopted-row id; the second reports the
// session-only fact instead.
//
// RUN IDENTITY (epic run-identity-unify, 2026-09-20): the checkpoint comes from
// the ROW's run directory — sessions/<task_id>/, named by the target itself —
// whose run LOGS are named by the run's uuid. Resolving the task id as if it
// were a log FILENAME (sessions/<task_id>.jsonl) walks a path that cannot
// exist; internal/rundir owns the mapping. The row's `session_id` column is
// read only for a row whose directory holds no run log at all (the
// re-homed/pre-layout shape), never as a substitute for a row that ran and kept
// nothing. See tools.TaskResumeTarget.
func resolveResumeTarget(target string) (string, error) {
	resumeTargetWasTaskID = false
	resumeTargetRowID = ""
	if raw, st, ok := resumeTaskRawLog(target); ok {
		if raw != "" {
			// THE CHECKPOINT MUST BE THERE, not merely recorded (measured
			// 2026-11-05). tools.TaskResumeTarget deliberately answers with the
			// run log's RECORDED raw-log path even when the file is gone — its
			// own comment says so, and it does it to fail fast instead of
			// re-searching the whole sessions tree. Returning that path from
			// HERE would move the failure one layer down, where the checkpoint
			// loader can only say "no → REQUEST checkpoint in <path>": a
			// different sentence, and silent about WHICH row and WHICH verdict.
			// A recorded-but-missing checkpoint is exactly the
			// ran-no-checkpoint state, and it is classified here with the rest
			// of the row's evidence.
			if fi, statErr := os.Stat(raw); statErr == nil && !fi.IsDir() {
				resumeTargetWasTaskID = true
				// ROW VIEW: the ledger answered — this target IS a row (tools.
				// TaskResumeTarget returns only for an existing row, and st.TaskID
				// is that row's id). This is the ONE branch on which adoption is
				// sound, and the id adopted is the ROW's, never the operator's
				// spelling of it.
				resumeTargetRowID = st.TaskID
				return raw, nil
			}
			return "", resumeNoCheckpointError(target, st)
		}
		// THE ROW EXISTS AND NAMES NO CHECKPOINT — a HARD ERROR (Rule 15).
		//
		// WHY THIS MAY NOT FALL THROUGH (measured 2026-10-28, epic
		// resume-session-target-latch): the two fallbacks below answer about a
		// STRING, not about the row. `resumeRunLog(target)` reads
		// sessions/<target>/ on the filesystem and proves nothing about the
		// ledger; `resolveResumeLogPath(target)` treats the target as a session
		// UUID and accepts whatever session the id lookup finds. Falling
		// through therefore turned "this row kept no checkpoint" into
		// "continue some other session" — the exact substitution
		// tools.TaskResumeTarget now refuses to make on the row side, and the
		// shape that let a resume open an unrelated conversation instead of
		// failing. A recovery lands on the row's own work or does not run.
		return "", resumeNoCheckpointError(target, st)
	}
	if raw := resumeRunLog(target); raw != "" {
		// RUN-DIRECTORY branch: a checkpoint was found at
		// sessions/<target>/ WITHOUT asking the ledger anything, so it
		// proves nothing about a row of that id. The target IS task-mode
		// (a task id and a session uuid are indistinguishable here), so
		// the caller must say which of the two facts it has — but it may
		// NOT adopt: see resumeTargetRowID, which stays empty on this
		// branch.
		resumeTargetWasTaskID = true
		return raw, nil
	}
	return resolveResumeLogPath(target)
}

// resumeRunLog resolves a target's newest run checkpoint DIRECTLY from its run
// directory — <gopi dir>/sessions/<target>/, addressed by the target itself
// (the run-identity contract). The target is a task id in the common case, but
// the address is the run id, and for a task row those are one and the same
// string (rundir.DirFor).
//
// WHY IT EXISTS (the ledger-context fallback): resumeTaskRawLog answers through
// the row VIEW (tools.TaskResumeTarget → TaskProcessStatusFor), which must read
// the row first — so a target that is a VALID task id becomes resolvable only
// while the ledger answers. Recovery is exactly the situation where that fails
// or has not yet happened: the row was claimed and its session directory minted,
// but nothing has read the VIEW. This fallback keys the SAME directory mapping
// (rundir — one address implementation, Rules 13/14) and asks the SAME question
// (the newest run holding a checkpoint), so a task-id target resolves from the
// filesystem alone and the ledger becomes an enrichment, not a precondition of
// recovery.
//
// THE TWO SHAPES OF "the directory holds a checkpoint" (epic resume-target-
// surface, 2026-09-26): a run directory can hold a session log that RECORDS its
// raw log path (the settled shape, answered by WithCheckpoint/ExistingRawLog),
// or hold the raw log and NO record naming it — a log with .raw.log beside it
// is written by the run path, and an interrupted run (or an isolated profile)
// can have the checkpoint without the record. Only the filesystem answers the
// second shape, and it is the shape the operator's own acceptance probe
// produces (mkdir sessions/<T> + a real <T>.raw.log). Both are asked here, in
// that order: the RECORD is authoritative about WHICH run's checkpoint is
// meant, the co-located file is the address itself.
//
// Fail-soft by contract (a resume must never refuse to run because a run log was
// pruned): no target, no run directory, no run log, or a vanished file all
// return "" — the caller then walks the pre-existing path/session-id
// resolution.
func resumeRunLog(target string) string {
	if strings.TrimSpace(target) == "" {
		return ""
	}
	dir := rundir.DirFor(target)
	if dir == "" {
		return ""
	}
	// An absent directory is an EMPTY list, not an error: "never ran" and
	// "logs gone" are different absences and only the second one is a loss.
	recs := rundir.RecordsDir(dir)
	if rec, ok := rundir.WithCheckpoint(recs); ok {
		return rundir.ExistingRawLog(rec)
	}
	// The directory's OWN raw log (rundir's one resolver): the checkpoint of a
	// run whose session log does not name it, or that wrote no session log at
	// all. It comes AFTER the record-based answer for the reason above, and
	// BEFORE the Latest fallback (which only names a run the record already
	// failed to resolve).
	if raw := rundir.RunDirRawLog(dir); raw != "" {
		return raw
	}
	if rec, ok := rundir.Latest(recs); ok {
		return rundir.ExistingRawLog(rec)
	}
	return ""
}

// resumeTaskRawLog resolves the raw-log checkpoint of a task row — the
// `gopi session resume <task-id>` target without an operator hunting a session id.
// The checkpoint is read from the row's run DIRECTORY (the run that actually
// executed the row), never from the row's session_id column. ok=false when the
// argument names no task row at all (then the normal path/session-id
// resolution runs).
func resumeTaskRawLog(target string) (rawLog string, row *tools.TaskProcessStatus, ok bool) {
	st, raw, err := tools.TaskResumeTarget(target)
	if err != nil || st == nil {
		return "", nil, false
	}
	return raw, st, true
}

// resolveResumeLogPath maps the resume target to a raw log path. A path that
// exists (or looks like a raw log filename) is used as-is; otherwise the
// argument is treated as a session UUID and resolved through the session
// JSONL's session_start timestamp.
//
// CHAT RESUME — MEASURED (epic run-identity-unify, 2026-09-20). The operator's
// question was "why can't a chat resume from its previous run — the raw log is
// there, why not send it as the request and continue?". Answer, with numbers from
// this host:
//
//   - the RESOLUTION is not the blocker: a bare chat dir is named by its session
//     id, and 211 of the 238 identity-matched bare raw logs here HAVE a
//     session.jsonl beside them, so FindSession + SessionRawLogPath/SiblingRawLog
//     answer for the ordinary chat (the remaining 27 are raw-logs whose jsonl is
//     gone — that absence is reported, not guessed at).
//   - what a chat lacks is the LEDGER ROW: resume's --task linkage, the
//     cross-run conclusion carry-forward and the claim/holder gate all key on a
//     task_id, which a bare prompt never has. That is a product gap (a chat
//     cannot be resumed as a ROW), not a resolution defect — and it is the same
//     gap the row-keyed work in tools.TaskResumeTarget closes for tasks.
//   - the RECOVERY that does exist for a chat: `gopi session resume <session-id>` replays
//     its checkpoint, and `gopi session resume <raw-log-path>` takes the raw log
//     directly.
func resolveResumeLogPath(target string) (string, error) {
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	// Not a readable path — try session-id resolution (dual-read across all
	// three layouts: legacy flat <id>.jsonl, task-run <task_id>/<id>.jsonl,
	// bare <id>/session.jsonl).
	dir, err := gopilog.ResolveSessionDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve session dir: %w", err)
	}
	// THE ROW PATH FIRST, the id layouts second — the SAME resolver the other
	// session verbs call (cli_session_resolver.go), so `gopi session resume X`
	// and `gopi session log X` cannot disagree about one operand again. An
	// operand that names a ledger row resolves to the row's own record; every
	// other operand falls through to the id layouts unchanged.
	sessionPath, err := findSessionLogPath(dir, target)
	if err != nil {
		return "", fmt.Errorf("cannot resolve session id %q: %w", target, err)
	}
	if sessionPath == "" {
		return "", errors.New(resumeResolveRefusalLine(target, dir))
	}
	// Prefer the raw log path recorded at session_start (authoritative —
	// works for custom GOPI_RAW_LOG_DIR sessions too). Fall back to the
	// timestamp-based pairing for sessions that predate the field.
	if p := gopilog.SessionRawLogPath(sessionPath); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		// Re-homed raw log: the file moved next to its session (epic
		// task-session-cwd), the RECORD still names it by basename.
		if sib := gopilog.SiblingRawLog(sessionPath, p); sib != "" {
			return sib, nil
		}
		fmt.Fprintf(os.Stderr, "[gopi] resume: recorded raw log %s missing, falling back to timestamp pairing\n", p)
	}
	start, err := gopilog.SessionStartTime(sessionPath)
	if err != nil {
		return "", fmt.Errorf("cannot read session_start from %s: %w", sessionPath, err)
	}
	rawPath, err := gopilog.FindRawLogForSession(start)
	if err != nil {
		return "", fmt.Errorf("cannot locate raw log for session %s: %w", target, err)
	}
	return rawPath, nil
}

// resumeTaskFlag is the `--task` spelling of the resume linkage option (M4.7).
// It is a constant because three places compare it — the target/tail split, the
// flag parser and the usage text — and a rename in one place only would turn
// the option into an unknown flag.
const resumeTaskFlag = "--task"

// resumeNoTargetMessage is the ONE wording of "this verb needs an operand",
// shared by the empty line and the flags-only line: two spellings of one
// refusal let a reader think they faced two different problems.
const resumeNoTargetMessage = "no resume target — pass <task-id|raw-log-path|session-id> (a -flag is never the thing to resume)"

// resumeDumpCheckpointNoticeFormat announces a checkpoint recovered from the
// CRASH DUMP instead of the raw log (row 01a183a8). The recovery is real — the
// dump holds the full-context request the run died on — but it is a DIFFERENT
// artifact, so it is stated: the interrupted run recorded no completed request
// round in its raw log, and an operator reading "resumed" needs to know which
// body the continuation was seeded from (Rule 15).
const resumeDumpCheckpointNoticeFormat = "%s: %s holds no → REQUEST checkpoint (the run recorded no completed request round — no request body reached the wire), so the checkpoint was recovered from the %s [%s]; the continuation is seeded from that request body.\n"

// resumeDumpCheckpointDigest renders the recovered request in the same shape
// the crash-dump pipeline's own digest uses, so the operator can recognise it
// against the dump document: model, message count, wire size.
func resumeDumpCheckpointDigest(req *deepseek.ClaudeRequest) string {
	if req == nil {
		return "request"
	}
	wire, _ := json.Marshal(req) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return fmt.Sprintf("request body (model=%s messages=%d bytes=%d)", req.Model, len(req.Messages), len(wire))
}

// resumeUsageFlagErr is the sentinel error for a malformed --task flag; the
// caller prints the full usage after it (the other arg errors do the same).
var errResumeUsage = fmt.Errorf("malformed %s flag", resumeTaskFlag)

// parseResumeArgs splits the resume argv tail (after the resume target) into
// the --task linkage value and the continuation-file positional. Accepts
// --task VALUE and --task=VALUE; any other -flag is refused (typo
// protection — a silently ignored flag would break the M4.7 linkage without
// a trace). An empty --task value is an error; no --task returns "".
func parseResumeArgs(args []string) (taskID, continuation string, err error) {
	for i := 0; i < len(args); i++ {
		opt := args[i]
		if !strings.HasPrefix(opt, "-") {
			if continuation == "" {
				continuation = opt // first positional = continuation file
			}
			continue
		}
		val := ""
		hasVal := false
		if eq := strings.Index(opt, "="); eq >= 0 {
			val, hasVal = opt[eq+1:], true
			opt = opt[:eq]
		}
		if opt != resumeTaskFlag {
			return "", "", fmt.Errorf("%w: unknown option %q", errResumeUsage, opt)
		}
		if !hasVal {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return "", "", fmt.Errorf("%w: --task requires a task_id argument", errResumeUsage)
			}
			i++
			val = args[i]
		}
		if strings.TrimSpace(val) == "" {
			return "", "", fmt.Errorf("%w: --task requires a non-empty task_id", errResumeUsage)
		}
		taskID = strings.TrimSpace(val)
	}
	return taskID, continuation, nil
}

// runResume handles the `gopi session resume` subcommand. args is the argv
// tail AFTER the "resume" subcommand word.
//
// THE BODY IS ONLY THE ADDRESS PARSER. The resume itself is resumeSession, the
// ONE core below — so this face and the no-verb `gopi --session <id>
// ["<prompt>"]` face are two addresses over one implementation: the checkpoint
// load, the process-identity gate, the row adoption/linkage, the close-out
// chain and every diagnostic after that point are shared, never mirrored.
// What this face keeps to itself is its POSITIONAL contract — target plus an
// optional continuation FILE, `-h` printing its own page — and what it adds to
// the core is nothing: it passes no inline prompt (its positional is a path,
// read by the core), because a prompt text in the continuation position would
// be a filename that does not exist (the measured trap: `gopi --session ID
// "<prompt>"` cannot be spelled by stuffing the prompt here).
func runResume(args []string) int {
	if len(args) == 0 {
		// ONE refusal shape for "no operand", whether the line is empty or
		// carries only flags: the missing input is the TARGET, and saying so is
		// the whole job of the message (Rule 15). The page follows, so the
		// remedy is one scroll away.
		fmt.Fprintf(os.Stderr, "gopi session resume: %s\n%s", resumeNoTargetMessage, resumeUsage)
		return 1
	}
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Print(resumeUsage)
			return 0
		}
	}

	// --task <task_id>: opt-in resume linkage (M4.7). The task row gains a
	// "resumed:<session-id>" tag for THIS run's fresh session, so the ledger
	// can answer "how often was this task resumed". Parsed pre-flight so a
	// malformed --task aborts before the (wasted) checkpoint load.
	//
	// THE TARGET IS THE FIRST POSITIONAL, WHEREVER THE FLAG STANDS (the family
	// criterion, cli_session_flag_arity.go). This used to read args[0] as the
	// target unconditionally, so `gopi session resume --task t-123 <id>` tried
	// to resume a session literally named "--task" and answered "resume target
	// \"--task\" is neither a raw log path nor an existing session id" — a
	// claim about the operand while the operand was on the command line, and
	// the same defect `log`/`stats`/`raw` had on the positional <session-id>
	// (measured 2026-10-27, both orders).
	target, tail := resumeTargetAndTail(args)
	// The tail is parsed FIRST so an unknown option is refused BY NAME
	// (`unknown option "--bogus"`) rather than being reported as a missing
	// target — the more specific statement wins, and a flag is never mistaken
	// for the operand in either direction.
	taskID, continuation, err := parseResumeArgs(tail)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopi session resume: %v\n%s", err, resumeUsage)
		return 1
	}
	if target == "" {
		fmt.Fprintf(os.Stderr, "gopi session resume: %s\n%s", resumeNoTargetMessage, resumeUsage)
		return 1
	}
	return resumeSession(resumeProgName, target, continuation, taskID, "")
}

// resumeTargetAndTail splits `gopi session resume`'s argv into the resume
// TARGET (its one positional operand — a task id, a raw-log path or a session
// id) and the TAIL parseResumeArgs reads (the --task flag and the optional
// continuation-file positional).
//
// The split follows the SAME criterion the rest of the family obeys: a flag's
// position relative to the operand must not change which token is the operand.
// `--task T <id>` and `<id> --task T` therefore name the same target and the
// same task id. `--task`'s VALUE is never a candidate for the target, which is
// the half a naive "first token not starting with -" scan gets wrong.
//
// A missing target is refused by NAME, never by resuming something else: the
// only input this verb cannot do without is the operand, and saying so is the
// whole job of the message.
func resumeTargetAndTail(args []string) (target string, tail []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == resumeTaskFlag:
			// The flag and its value are the TAIL's business (parseResumeArgs
			// owns their refusals — a missing/empty value, a value that is
			// itself a flag). Skipping the value here is what keeps `--task T`
			// from being read as the target.
			if i+1 < len(args) {
				tail = append(tail, a, args[i+1])
				i++
				continue
			}
			tail = append(tail, a)
		case strings.HasPrefix(a, resumeTaskFlag+"="):
			tail = append(tail, a)
		case strings.HasPrefix(a, "-"):
			// Any other -flag belongs to parseResumeArgs, whose refusal names it
			// (typo protection: a silently ignored flag breaks the M4.7 linkage
			// without a trace).
			tail = append(tail, a)
		case target == "":
			target = a
		default:
			tail = append(tail, a)
		}
	}
	return target, tail
}

// resumeSession is the ONE resume implementation: it loads the checkpoint
// addressed by target from its raw log, seeds a fresh agent's context, and
// runs the continued loop.
//
// THE INSTRUCTION HAS TWO CHANNELS AND EXACTLY ONE IS EVER SET, because they
// come from two different argv shapes:
//
//   - continuation — a FILE whose contents become the instruction. It is the
//     `gopi session resume <target> [continuation-file]` positional, and its
//     semantics are unchanged: read the file, trim it, send it.
//   - prompt — INLINE text, the no-verb face's argv tail (`gopi --session
//     <id> "now run the tests"`). There is no file to read.
//
// progName is the face's own spelling ("gopi session resume" / "gopi
// --session"): every diagnostic this core emits on its own behalf carries it,
// so an operator reading one line of stderr knows which command they typed.
func resumeSession(progName, target, continuation, taskID, prompt string) int {
	rawLogPath, err := resolveResumeTarget(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		return 1
	}
	// TASK-MODE TARGET (epic task-process-identity): `gopi session resume <task-id>`
	// resolves the row's own session checkpoint AND auto-links the row, so
	// the operator's recovery path for a killed run is one command with no
	// session-id hunting — the complaint that started this epic.
	//
	// ADOPT ONLY WHAT THE ROW VIEW RESOLVED (epic resume-session-target-latch).
	// The auto-link above needs a row to link TO, and only the ledger view can
	// say one exists (resumeTargetRowID). A run-directory resolution is a
	// FILESYSTEM fact — a bare session's own directory resolves exactly the
	// same way — so there the run is reported SESSION-ONLY: no adopt, no
	// linkage stamp, no close-out of a row nobody named, and the run keeps the
	// auto row every session gets (Rule 15: the decision is stated, never
	// silent). The `--task <id>` explicit form is untouched by this: an
	// operator who names a row asserts it exists.
	targetRowID := resumeTargetRowID
	if taskID == "" && resumeTargetWasTaskID && targetRowID != "" {
		taskID = targetRowID
	}
	// SESSION-ONLY TARGET (epic resume-session-target-latch): a task-mode
	// target that resolved from the run DIRECTORY — the address a bare
	// session's own directory is named by, so `gopi session resume <session-uuid>`
	// and `gopi session resume <task-id>` are the same shape here. Nothing may be
	// adopted (resumeTargetRowID is empty: the ledger was never asked), so
	// say so once, up front, rather than letting three warnings leak later
	// about a row nobody can name. Reported, not refused: the checkpoint is
	// real and the resume is the right thing to do.
	if resumeTargetWasTaskID && targetRowID == "" && strings.TrimSpace(target) != "" {
		reportResumeNoRow(strings.TrimSpace(target), resumeRowReasonRunDir)
	}

	// THE ADMISSION GATES, AND THE ONE WHOSE JUDGE CHANGED (epic
	// resume-gate-is-the-lock, 2026-11-05).
	//
	// ① THE HOLDER CLAIM — a --task resume must never be decided by a status
	// string ALONE, and it must not be decided by pid liveness alone either.
	// `in_progress` cannot tell a live run from a worker killed by SIGKILL, and
	// pid liveness cannot tell "alive" from "executing": the ledger records the
	// holder's pid + start ticks + cmdline, so that verdict answers whether a
	// process with that identity EXISTS on this host — which is NOT the question
	// a resume has to answer. Measured (PA, 2026-11-05, the same evening, the
	// verbatim same refusal): a holder 5h18m old with 24s of CPU and 36 threads
	// in futex (zero progress, the refusal WRONG) and a holder 2m25s into a live
	// round (the refusal RIGHT) were indistinguishable to it. So the LIVE answer
	// is now a CLAIM, arbitrated by the run-scope kernel lock taken just below:
	// holding that lock IS "nothing is executing this run directory", and the
	// override is REPORTED with the pid it overrode (resumeReportHolderOverride,
	// Rule 15). When no lock could be taken the claim stands as a refusal —
	// the kernel said nothing, and an unarbitrated claim is never resolved in
	// the admitting direction.
	//
	// ② LEDGER STATUS — a free holder says nobody is running the row, NOT that
	// the work is unfinished. FINISHED terminal statuses (completed / verified /
	// superseded) are refused, with the copy named. `failed` passes: a failure
	// judges one ATTEMPT, so resuming it is level 1 of the failure policy — the
	// exact opposite of the pre-2026-10-29 posture. Both facts come from
	// TaskProcessStatusFor, so this face, `gopi task status` and `gopi worker
	// --resume` cannot disagree. UNTOUCHED by the lock: a lock answers "is it
	// executing", never "is the work over".
	//
	// ③ DELIVERY — the SECOND axis of gate ②, and the reason it is not enough
	// (2026-10-30): "finished" is a claim about the WORK, and a status column
	// cannot make it alone. Fifteen commits sat in three `completed` rows whose
	// branches were ancestors of NOTHING. TaskProcessStatusFor measures that
	// axis (DeliveryVerdict) and this gate ACTS on it — an UNLANDED verdict
	// admits the row and prints the caveat naming the status it overrode, while
	// an UNKNOWN one keeps the refusal (the admission rule is EVIDENCE of owed
	// work, never the absence of a measurement). The whole design is the header
	// of internal/tools/task_resume_delivery_axis.go.
	//
	// THE ORDER IS THE FIX. Until 2026-11-05 the gate answered ① BEFORE taking
	// the lock, so the weaker measurement refused first and the lock never got
	// to speak. Now the lock is taken first and the gate reads an ARBITRATED
	// verdict (resumeDecideWithLock) instead of asking the raw question.
	//
	// RUN-SCOPE LOCK (epic run-lock-per-run-dir): this process is about to
	// CONTINUE the session living in rawLogPath's run directory, appending to
	// the very JSONL and raw log a worker may be executing right now. The lock
	// is taken HERE — before the checkpoint load, before newAgentResuming and
	// before ag.Resume, i.e. before anything in this process creates or writes
	// a session file — so a refusal leaves the run directory byte-for-byte as
	// it found it. A second holder is refused by the kernel and the refusal
	// names the lock path and the holder's pid/start ticks (Rule 15). See
	// run_lock_resume_seam.go for why the address is the checkpoint's own
	// directory, and why that is the same directory the funnel locks.
	//
	// resumeLockArbitrated is what makes ① usable: true means the kernel
	// granted this process the directory, so no other process is executing it;
	// false means the checkpoint is not in a run directory of this profile at
	// all, so NOTHING arbitrated it and a LIVE claim keeps its refusal.
	resumeLockRelease, resumeLockArbitrated, lerr := resumeTakeRunLock(progName, rawLogPath)
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "%v\n", lerr)
		return 1
	}
	// Released on EVERY exit below, including the error paths: the lock is held
	// by an open descriptor, and a caller that keeps running must not keep the
	// directory.
	defer resumeLockRelease()

	switch resumeDecideWithLock(taskID, resumeLockArbitrated) {
	case resumeGateHolderLive:
		// ADMITTED, AND THE OVERRIDE IS SAID OUT LOUD: the ledger named a live
		// pid and this resume proceeds anyway, because the lock this process
		// now holds is the kernel's answer that the pid is not executing this
		// run directory. Printing nothing here would make this admission
		// indistinguishable from an ordinary one (Rule 15).
		resumeReportHolderOverride(os.Stderr, progName, taskID)
	case resumeGateRefuseRunning:
		// MERGE NOTE (delivery round 2/3, 2026-11-05): this sentence is the UNION
		// of two epics that met on this exact line, and neither half is optional.
		//
		//   - OURS (epic resume-gate-is-the-lock) restates the CONDITION: "live"
		//     alone stopped being a refusal on 2026-11-05, so this case now means
		//     the ledger claims a live holder AND no run lock could be taken to
		//     arbitrate the claim. Keeping master's wording would tell the
		//     operator the pid decided it, which is the bug this epic closed.
		//   - MASTER's (bug epic 01a1ac0f) `"%s"+ resumeProgressNote(taskID)`
		//     appends the PROGRESS AXIS: whether the holder is progressing or has
		//     been idle for hours. That axis is measured independently of the lock
		//     and is what tells an operator whether waiting is even possible — the
		//     5h18m/24s-of-CPU incident that epic was filed for.
		//
		// Both facts are true at once: the lock answers "is anything executing
		// this directory", the progress axis answers "is it doing anything".
		resumeReportRefuseRunning(os.Stderr, progName, taskID)
		return 1
	case resumeGateRefuseUnknown:
		fmt.Fprintf(os.Stderr, "%s: refusing — task %s's holder cannot be verified on this host (foreign host or an unreadable/stale stamp).\n"+
			"  Check with: gopi task status %s\n  Resuming would risk double-running it; resolve the stamp first.\n",
			progName, taskID, taskID)
		return 1
	case resumeGateRefuseStatus:
		fmt.Fprintf(os.Stderr, "%s: refusing — task %s's work is FINISHED (%s); a resume would continue something already delivered.\n"+
			"  Reissue it as a copy: gopi task clone %s   (a new row under a FRESH uuid, parented to this one)\n"+
			"  Inspect first with: gopi task status %s\n",
			progName, taskID, resumeStatusWord(taskID), taskID, taskID)
		return 1
	}
	// THE ADMISSION SIDE OF GATE ③ must be as loud as the refusal, because it
	// is the SURPRISING answer: this row's status says its work is finished and
	// the resume is proceeding anyway. Printing nothing here would make an
	// admitted-by-delivery resume indistinguishable from an ordinary one, and
	// the operator would have no way to see which measurement overrode the
	// status (Rule 15). It goes to STDERR: stdout carries the resumed session's
	// result alone (GOPI.md's stream contract).
	resumeReportDeliveryAdmission(os.Stderr, progName, taskID)

	// Deliverable ground closure (bugfix epic 2026-09-03; CHANNEL-AWARE since
	// the clone channel). The row's recorded ground is resolved through the SAME
	// judge `gopi worker --resume` uses (tools.TaskResumeGroundForTask →
	// devworkspace.ResolveGroundState, the ONE liveness verdict, Rules 13/14),
	// so a CLONE-channel workspace (`.git` a DIRECTORY, under
	// <workspace_root>/<task-id>) is entered exactly like a linked worktree.
	//
	// It is NOT fail-soft any more. Measured: this face chdir'd nowhere on a
	// clone row and ran the whole session in the CALLER's cwd — from the main
	// checkout that is a session whose commits land in the shared tree with no
	// owner, no branch and no rollback point (Rule 37). A ground the row RECORDS
	// but which is not usable is now a REFUSAL with a non-zero exit; the
	// degradation is reachable only through the explicit switch
	// (config.ResumeAllowMissingGround). chdir happens BEFORE newAgent so the
	// fresh session's workdir, its GOPI.md project context and the bash tool's
	// cwd all point at the ground.
	if _, gerr := resumeChdirToWorktree(taskID); gerr != nil {
		fmt.Fprint(os.Stderr, resumeGroundRefusalText(progName, gerr))
		return 1
	}

	// The instruction: the INLINE prompt when the face carried one, else the
	// continuation FILE (unchanged semantics). The two are exclusive by
	// construction — resumeSession reads the file only when prompt is empty.
	var input string
	if prompt != "" {
		input = strings.TrimSpace(prompt)
	}
	if input == "" && continuation != "" {
		data, err := os.ReadFile(continuation)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: error reading continuation file %s: %v\n", progName, continuation, err)
			return 1
		}
		input = strings.TrimSpace(string(data))
	}

	// Load the checkpoint. TWO SOURCES, ONE DECISION (row 01a183a8): the raw
	// log's last "→ REQUEST" is the byte-exact checkpoint, and when the log
	// holds no completed request round (a first request that died at the
	// transport layer writes no body) the crash dump beside it carries the same
	// full-context body. LoadCheckpointWithSource answers which one was used;
	// the fallback is ANNOUNCED here, because a resume that recovered from the
	// dump must not read as one that continued from its own raw log (Rule 15).
	req, history, systemTexts, provenance, err := agent.LoadCheckpointWithSource(rawLogPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", progName, err)
		return 1
	}
	if provenance.Source == agent.CheckpointSourceDump {
		fmt.Fprintf(os.Stderr, resumeDumpCheckpointNoticeFormat, progName, rawLogPath, provenance.Detail(), resumeDumpCheckpointDigest(req))
	}

	// CONTINUE THE SESSION THAT EXISTS (row 01a17dd8). The identity — the
	// session id, the raw log to append to and the JSONL to append to — is
	// resolved from the checkpoint's own run directory. Building a FRESH agent
	// here is the defect: it minted a new uuid, and the run wrote
	// sessions/<new-uuid>/ while the row's own run directory was never touched,
	// i.e. a "resume" that created a session instead of continuing one.
	identity, recordFound := resumeIdentityFor(rawLogPath)
	switch {
	case identity.SessionLogPath == "":
		// NO SESSION TO CONTINUE — the checkpoint named no run directory of
		// THIS profile (a log copied out of a tree, an export, a fixture). The
		// run gets its OWN fresh session id and writes its record under the
		// sessions root; the CONVERSATION still continues from the checkpoint
		// (RawLogPath is appended to). Stated, never silent (Rule 15), and
		// stated for what it is: this is not a rebuilt record and not a
		// continued session — using the checkpoint's FILE NAME as an id is the
		// defect row 01a180f5 removes (the name names no session of any
		// profile, so the row's lineage stamp was refused and never written).
		fmt.Fprintln(os.Stderr, resumeFreshSessionNotice(progName, rawLogPath, identity.SessionID))
	case !recordFound:
		// THE RECORD WAS REBUILT, NOT MINTED (row 01a17df9-99c4). This branch
		// keeps the checkpoint's own run id and its own directory, so the
		// session the operator named is the session that continues — identity
		// is preserved and this is the documented recovery (`gopi session
		// resume <raw-log-path>`). It is NOT the defect: the defect was the
		// chat face minting a NEW uuid directory beside the old one and
		// reporting success (see cli_resume_record_gap.go). What the recovery
		// still owes the operator is the FACT, so it is announced rather than
		// performed in silence (Rule 15) — measured before this line: the run
		// simply continued and nothing said the record had been rebuilt.
		fmt.Fprintf(os.Stderr, resumeRecordRebuiltNoticeFormat,
			progName, filepath.Dir(identity.SessionLogPath))
		// …AND ON THE ROW (epic 01a17df9-99c4). Same reason as the chat face's
		// note: the notice above belongs to a process that will have exited.
		// The rebuild is milder than a mint — the id and directory are kept — but
		// it still means the record a reader consults was written by a RESUME,
		// and `resumed:<session>` alone cannot say that.
		if noteErr := tools.TaskResumeRecordRebuiltNote(taskID,
			tools.TaskResumeRecordRebuiltNoteText(strings.TrimSpace(target), filepath.Dir(identity.SessionLogPath))); noteErr != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", progName, noteErr)
		}
	}
	if identity.SessionLogPath != "" {
		fmt.Fprintf(os.Stderr, "[gopi] resume: continuing session %s in %s\n", identity.SessionID, filepath.Dir(identity.SessionLogPath))
	} else {
		fmt.Fprintf(os.Stderr, "[gopi] resume: new session %s continues the conversation in %s\n", identity.SessionID, rawLogPath)
	}
	// OWNERSHIP HANDOFF (epic task-process-identity), AND IT IS THE RESUME
	// ITSELF (two-level policy, internal/tools/task_run_retry.go): the gate above
	// proved the row has no live holder, so THIS process is now the one executing
	// it. Stamping the row with this process's identity — and moving a `failed`
	// row back to in_progress in the SAME write (TaskStampHolder's WHERE admits
	// failed→in_progress) — is what makes a resume VISIBLE: while this run lives,
	// its successor's gate sees a verified-live holder and refuses; when this run
	// dies, the verdict flips back to dead and the row is resumable again.
	//
	// WHERE THIS SITS IS THE FIX, NOT AN ACCIDENT. It used to run immediately
	// after the gate — BEFORE the checkpoint was resolved — so a resume that then
	// REFUSED (no recoverable checkpoint, an unreadable raw log) had already
	// flipped the row off `failed`, leaving a row reading in_progress with no
	// run behind it. "A refused resume leaves the row exactly as it was" is the
	// acceptance test (TestResumeRefusedLeavesTheRowAlone), and it is a real
	// promise: the row is the operator's evidence. So the handoff happens HERE,
	// at the last moment before the agent is built — everything that can refuse
	// has refused by now, and what follows is a run that will really execute.
	// Best-effort with a warning: a ledger hiccup must not block recovering an
	// interrupted session.
	if taskID != "" {
		if err := tools.TaskStampSelfClaim(taskID); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] resume: warning: could not claim task %s for this run (a concurrent resume may double-run it): %v\n",
				taskID, err)
		}
	}

	ag, err := newAgentResuming("", identity)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent: %v\n", err)
		return 1
	}
	// Row adoption (epic resume-writes-terminal-status): the resumed run
	// CONTINUES the --task row, so hand the agent that identity BEFORE the
	// engine starts — the start hook latches it and the end hook's close-out
	// (the ONE terminal-status writer, shared with every other run) writes
	// this run's real outcome onto it. Without this the row keeps whatever the
	// run that DIED left behind: `failed`, `ended_at` frozen at that death,
	// forever, while the resume's work was in fact committed and its cleanup
	// completed. Best-effort: adopting a row can never fail, and the end hook's
	// own guards leave a nonexistent/already-terminal row alone.
	adoptResumeLedgerRow(ag, taskID)
	printLogPaths(ag)

	// THE RESUME FACE OWNS ITS TERMINATING SIGNALS (epic 01a15bca). Until this
	// block existed the mode had NO registration at all, while
	// modesThatOwnTheirSignalHandlers declares "resume" handler-owning — so the
	// recorder took SIGTERM/SIGINT/SIGHUP on the mode's behalf (which REPLACES
	// their default death) and then declined to re-raise, leaving a signal that
	// neither killed nor drained the resumed run. Measured shape, same family as
	// 01a15bb9: the provenance ledger gets {"signal":"SIGHUP","role":"resume"}
	// and the run continues as if nothing happened.
	//
	// The unwind is the bare-prompt face's, because this IS one: cancellation
	// through the agent's own interrupt root (the graceful path runEngine already
	// honours), and CloseSignal declared BEFORE it so the session_end names the
	// cause instead of filing a terminal teardown as `unknown`.
	resumeSigCh := make(chan os.Signal, 1)
	signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)
	signalrec.ClaimSignalHandlers()
	defer signal.Stop(resumeSigCh)
	go func() {
		if sig, ok := <-resumeSigCh; ok {
			fmt.Fprintf(os.Stderr, "\n[gopi] %v received — finalizing session…\n", sig)
			ag.CloseSignal = signalrec.SignalName(sig)
			ag.Interrupt()
		}
	}()

	// Validate the checkpoint's model against the resolved one (settings drift
	// would silently change the resumed context's behaviour — warn, do not
	// halt, mirroring the feasibility study's risk #5).
	if req != nil && req.Model != "" && req.Model != ag.Model {
		fmt.Fprintf(os.Stderr, "[gopi] resume: checkpoint model %q differs from current %q — context may behave differently\n",
			req.Model, ag.Model)
	}

	// Surface the recovered context size for observability (no repeat work:
	// the user can see exactly how much history was carried over).
	wireBytes, _ := json.Marshal(req) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	fmt.Fprintf(os.Stderr, "[gopi] resume: loaded %d history message(s) [%d system block(s), %d bytes in checkpoint request]\n",
		len(history), len(systemTexts), len(wireBytes))

	// Cross-RUN working-area carry-forward (memory-aware T2): when --task is
	// given, recover the ORIGINAL (crashed/interrupted) session whose
	// write_task_area conclusions must be re-injected — the key under which
	// those conclusions were recorded. RUN IDENTITY (epic run-identity-unify):
	// the candidate list comes from the row's OWN RUNS, newest first (the run
	// that just crashed wrote the conclusions a recovery wants), with the
	// ledger's session_id column last — for a multi-run row that column
	// frequently names a different run, whose conclusions are the wrong ones.
	// Best-effort and non-fatal: no candidates / no conclusions simply means no
	// carry-forward (the recovered history in the checkpoint is still
	// authoritative); a lookup error warns but never blocks the recovery.
	if taskID != "" {
		for _, sid := range tools.TaskCarryForwardSessionIDs(taskID) {
			if tools.CarryForwardHasConclusions(sid) {
				ag.CarryForwardSessionID = sid
				fmt.Fprintf(os.Stderr, "[gopi] resume: armed write_task_area carry-forward from run session %s\n", sid)
				break
			}
		}
	}

	// Opt-in ledger linkage (--task): before resuming, stamp this run's fresh
	// session id into the task row as a resumed:<session-id> tag. Non-fatal —
	// a ledger hiccup must never block recovering an interrupted session
	// (warn on stderr; the lineage gap is visible via task show).
	if taskID != "" {
		if _, err := tools.TaskTagResumed(taskID, ag.SessionLog.SessionID()); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] resume: warning: task linkage failed (%s ← %s): %v\n",
				taskID, ag.SessionLog.SessionID(), err)
		} else {
			fmt.Fprintf(os.Stderr, "[gopi] resume: task %s tagged resumed:%s\n",
				taskID, ag.SessionLog.SessionID())
		}
		// Run provenance (devdoc/run-method-provenance.md): stamp the row
		// method=resume with the re-runnable command line — on a crashed
		// row the run_cmd IS the resume recipe, so the recovery that
		// actually happened records exactly how to reproduce it. rawLogPath
		// is the resolved continuation source (args[0] after resolution);
		// the --task <id> linkage is reconstructed verbatim.
		//
		// ONE RENDERER, TWO HALVES (epic resume-surface convergence step 2).
		// The stamped line goes through runcmd.ResumeArgv — the SAME function
		// the worker's child spawner and the merger's dead-holder arm execute —
		// so the recorded command and the spawned command cannot disagree about
		// the resume face's shape. Before this, the stamp site hand-built the
		// argv and kept spelling the verb face after step 1 had given resumes a
		// second, no-verb address: the field whose whole purpose is to be pasted
		// back into a shell would have taught the retired-by-step-3 spelling to
		// every operator reading `gopi task show`. Rendered by the shared
		// renderer (basename + shell quoting) like every other stamp site, so a
		// spaced raw-log path stays pasteable — and replayable, because the
		// no-verb face resolves a raw-log PATH target through the same
		// resolveResumeTarget the verb face uses. Non-fatal, same posture as the
		// tag above.
		resumeCmd := runcmd.Command(runcmd.ResumeArgv(rawLogPath, taskID))
		if err := tools.TaskStampRun(taskID, runcmd.MethodResume, resumeCmd); err != nil {
			fmt.Fprintf(os.Stderr, "[gopi] resume: warning: run provenance stamp failed (%s): %v\n",
				taskID, err)
		}
	}

	// The close-out owner is declared BEFORE the session starts: the session's
	// own end hook runs FIRST (measured — it is inside runEngine's defer, which
	// fires when Resume returns), so a declaration made after the run would
	// arrive too late and the same chain would be reported twice over the same
	// row. This is what makes the hook's decision a fact rather than a guess.
	resumeDeclareCleanupOwnership(taskID)
	result, err := ag.Resume(input, history)
	if err != nil {
		// Producer obligation, failure half: release the CWD before reporting
		// the error, so a FAILED resume that stood in a deliverable.worktree
		// (resumeChdirToWorktree above chdir'd into it) does not pin the
		// directory's inode for whatever runs next. ONE implementation
		// (Rules 13/14); best-effort and reported (Rule 15).
		resumeWorktreeRelease(os.Stderr, resumeProgName)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	// Producer obligation, success half: the resumed session may have
	// COMPLETED the task whose worktree this process is standing in — its own
	// end hook runs the finish-cleanup chain, which can remove that very
	// directory. Hand the cwd back to the root before returning so the deleted
	// inode is not held by this process (and, before the epic, by nothing at
	// all: it was left to process exit, which the caller cannot control).
	resumeFinishTail(result, taskID)
	return 0
}

// adoptResumeLedgerRow hands the agent the row this resume CONTINUES, before
// the engine starts (epic resume-writes-terminal-status).
//
// It is a named function rather than one inline call so the WIRING can be
// tested without running an agent: the defect being fixed is a MISSING
// LINKAGE, and an inline call is only assertable by reading the file (the
// same reason resumeFinishTail is named).
//
// THE ADOPTION IS GATED ON THE ROW EXISTING (epic resume-session-target-latch).
// Adoption's whole effect is to SUPPRESS this run's own auto row in favour of
// the adopted one, so it is only sound when that row is really there — and two
// of the three ways a task id reaches this function do not establish that: the
// target may have resolved from the run directory (a session uuid looks the
// same), or an explicit `--task` may name a row this ledger does not hold
// (another profile, a typo, a ledger that does not carry the row). Latching
// there is adopting a row that cannot exist: it reports success, the start
// hook then skips auto-registration, the resume tag and the close-out both
// fail against a missing row, and the resumed run ends with NO ledger record —
// three [gopi] warnings and a silent hole in the ledger, which is the shape
// this guard closes. A non-resolving adopt now REPORTS and runs session-only
// (Rule 15) instead of latching.
//
// ONE READER, NO SECOND POLICY: the existence question is asked through the
// same ledger row view the target resolution uses (tools.TaskStatusOf), not
// through a new query, and the answer is the ledger's own error text — so the
// operator sees WHY the row is unknown rather than a bare "not found".
func adoptResumeLedgerRow(ag *agent.Agent, taskID string) {
	if ag == nil || strings.TrimSpace(taskID) == "" {
		// No --task: this resume continues no row, so there is none to
		// close out (a bare continuation has no deliverable). The engine
		// still adopts one row per session — its own auto row — exactly as
		// any other session does.
		return
	}
	id := strings.TrimSpace(taskID)
	if _, err := resumeRowExists(id); err != nil {
		reportResumeNoRow(id, err.Error())
		return
	}
	ag.AdoptLedgerRow(id)
}

// resumeRowExists asks the ledger whether a row exists, through the same row
// view the resume target uses. It is a seam (the staleSweep/resumeFinishCleanup
// convention) so the guard's decision can be driven from a test without a real
// ledger — and so the read stays ONE reader if that view ever changes.
var resumeRowExists = tools.TaskStatusOf

// resumeFinishTail is the SUCCESS tail of a resume, in its one correct order:
// release the process CWD, then run the close-out chain, then print the session
// result. The order is the producer obligation (NOT a preference): the chain's
// job includes removing the directory this process is standing in, and a
// process still inside it pins the inode — which is exactly what the chain's
// own liveness guard reports from the outside. Releasing first is what lets the
// guard stay unweakened.
//
// It is a named function rather than three inline calls so the ORDER can be
// tested (TestResumeReleasesTheCwdBeforeTheChain): inline statements of an
// ordering are only assertable by reading the file.
//
// ONE WRITER PER CHAIN RUN (epic worker-resume-report-once, measured 2026-09-26
// on zpeer: one `gopi worker --resume` run printed its close-out report twice
// and its `skipped:` summary three times — /tmp/strange_python.txt, 66 lines).
//
// The chain itself has always been idempotent; the REPORT was not, because one
// requested close-out grows several writers in one process tree:
//
//	① the resumed session's end hook  (internal/agent, [gopi]-prefixed line)
//	② this flush (the resume tail)    (the operator-facing report line)
//	③ a resuming PARENT's close-out   (cmd/agent cli_worker_resume.go)
//
// ① and ② are one process, moments apart — and MEASURED, the hook runs FIRST:
// the engine's deferred close-out runs when `Ag.Resume` returns, before this
// tail. So the tail DECLARES its report BEFORE the resumed session starts
// (see resumeDeclareCleanupOwnership in Resume below), and the hook — running
// inside that session — consumes the declaration and defers, naming this tail
// as the reporter. What the hook will do is therefore never a GUESS: it is
// decided before the session's first instruction, and it holds for every way
// the session ends.
//
// ③ is a different PROCESS, which no in-process latch can reach, so the
// resuming parent declares that fact in the CHILD's ENVIRONMENT before spawning
// it (closeouthandoff.ParentReportsEnv — the same cross-process convention
// GOPI_TASK_WRAPPER_ID already uses). This tail reads that declaration and
// defers in turn, naming the parent; the parent then makes the one report.
//
// WHY THE ORDERING IS NOT A PREFERENCE. Releasing the process CWD comes first:
// the chain's job includes removing the directory this process stands in, and a
// process still inside it pins the inode, which is exactly what the chain's own
// liveness guard reports from the outside. Releasing first is what lets that
// guard stay unweakened.
func resumeFinishTail(result, taskID string) {
	resumeWorktreeRelease(os.Stderr, resumeProgName)
	// THE CLOSE-OUT CALL IS GONE WITH THE CHAIN (2026-10-30, task 01a18799,
	// slice 3/4). A resumed run used to ask the worktree+branch hand-off chain
	// to collect the row it continued; nothing to hand over exists under Rule
	// 6c, and the mechanism is retired. The CWD release above stays — it is
	// this process's own producer obligation and depends on no collector.
	//
	// taskID is still accepted so the tail's shape (and its ordering contract)
	// is unchanged for every caller.
	_ = taskID
	printResumeResult(result)
}

// printResumeResult writes the resumed session's result to stdout — the one
// answer channel (GOPI.md's stream contract: stdout carries the result and
// nothing else). Named rather than inlined so the stream contract has ONE call
// site to point at, and so a test can pin the answer to this channel alone.
func printResumeResult(result string) {
	fmt.Print(result)
}

// resumeDeclareCleanupOwnership declares, BEFORE the resumed session starts,
// that this face's tail will report the close-out chain for row — so the
// session's own end hook (which runs first, measured) defers instead of
// reporting the same chain over the same row.
//
// It is a named function rather than one inline call because the POSITION is
// the property that matters (TestResumeDeclaresCleanupOwnershipBeforeTheRun):
// a declaration made after the run would arrive too late for the hook that
// already asked, and the position of an inline call is only assertable by
// reading the file.
func resumeDeclareCleanupOwnership(taskID string) {
	if strings.TrimSpace(taskID) == "" {
		return
	}
	closeouthandoff.Declare(taskID, "")
}

// THE RESUME CLOSE-OUT TAIL IS RETIRED (2026-10-30, task 01a18799, slice 3/4).
//
// It exists no more because the mechanism it called does: `gopi session resume`
// used to run tools.TaskFinishCleanupSessionID over the row it continued, as
// the one face that made the worktree+branch hand-off chain reachable from a
// resume. That chain is gone — a dispatched run's delivery is its clone
// ground's own END-hook publish (Rule 6c), where there is no branch to hand
// over and no directory to reclaim — so a resume owes no close-out call, and
// running one would be running nothing under a name that says it did.
//
// The DECLARATION this face used to make (resumeDeclareCleanupOwnership, above)
// went with it: it existed to keep the session's end hook from reporting the
// same chain twice, and the hook no longer reports a chain at all.

// resumeWorktreeRelease is the CWD-release seam (the same package-var
// convention): both halves of the resume path release the process CWD, and the
// success half's release must precede the chain — an ordering only a seam can
// assert. One writer, one reader (Rule 2).
var resumeWorktreeRelease = worktreerelease.ReleaseReporting

// resumeProgName names the resume face in the diagnostics it emits on its own
// behalf (Rule 1: one spelling per face, one grep — the same shape
// taskProgName and workerProgTopLevel use).
const resumeProgName = "gopi session resume"

// resumeChdirToWorktree is the deliverable.worktree resume closure (bugfix
// epic 2026-09-03): when the --task row carries deliverable.worktree (the
// executor's Rule-6 worktree, backfilled at creation), chdir into it so the
// fresh session's workdir / bash-tool cwd / GOPI.md context point at where
// the executor's commits actually live — NOT the main repo the crashed
// session started in. Fail-soft: no task, no worktree, a vanished/stale
// worktree, or a chdir error all fall back to the current dir (a resume
// must never refuse to run because the recorded worktree was recycled).
// Returns the worktree chdir'd into ("" = stayed in place).
// cwdOrUnknown renders this process's working directory for the resume
// fallback warning, with an explicit unknown marker rather than a blank
// (Rule 15: a silent empty string reads as "we did not look"). It reuses
// pwdOrEmpty (cli_worker_resume.go) so the two resume faces answer from ONE
// reading of the cwd rather than two.
func cwdOrUnknown() string {
	wd := pwdOrEmpty()
	if wd == "" {
		return resumeCwdUnknownMarker
	}
	return wd
}

// resumeCwdUnknownMarker is the ONE spelling of "the OS could not report a
// working directory" in this face's fallback warning (Rule 1b).
const resumeCwdUnknownMarker = "(unknown — the OS could not report a working directory)"

// resumeChdirToWorktree resolves the row's recorded ground through the ONE
// channel-aware judge and chdir's into it.
//
// THREE ANSWERS, never a silent third (Rule 15):
//
//	("", nil)   there is nothing to enter — no task id, or a row whose
//	            deliverable records NO ground. Nothing was asked for, so
//	            nothing is refused.
//	(dir, nil)  the process now STANDS IN dir (a linked worktree, or the clone
//	            channel's workspace; WHICH one is the judge's answer, not this
//	            function's).
//	("", err)   the row RECORDS a ground that is not usable, or the OS refused
//	            the chdir. The caller MUST refuse: it renders the typed refusal
//	            (recorded path / model / detail / repair) and exits non-zero.
//	            Only config.ResumeAllowMissingGround turns this into the old
//	            warn-and-stay-put degradation, and that warning is loud.
//
// THE JUDGE IS SHARED, NOT REINVENTED (Rules 13/14): the resolution is
// tools.TaskResumeGroundForTask — the very call `gopi worker --resume <row>`
// makes — and its liveness verdict is devworkspace.ResolveGroundState. Two
// faces, one verdict, so the session face cannot disagree with the worker face
// about whether a directory is a ground.
func resumeChdirToWorktree(taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", nil
	}
	ground, err := tools.TaskResumeGroundForTask(taskID)
	if err != nil {
		if refusal, ok := tools.TaskResumeGroundRefusalOf(err); ok &&
			refusal.Kind == tools.TaskResumeGroundRefusalNoGround {
			// The row records NOTHING: there is no ground to be gone, so the
			// resume runs where the caller stands — stated, never silent (the
			// same sentence the worker face prints).
			fmt.Fprintf(os.Stderr, "[gopi] resume: %s\n", refusal.Message)
			return "", nil
		}
		if !config.ResumeAllowMissingGround() {
			return "", err
		}
		// THE ESCAPE HATCH — impossible to miss, because what it buys is a
		// resume whose commits have no owner, no branch and no rollback point.
		fmt.Fprint(os.Stderr, resumeDegradedNotice(err))
		return "", nil
	}
	if err := os.Chdir(ground.Dir); err != nil {
		chdirErr := fmt.Errorf("resume: the row's ground %s cannot be entered: %w", ground.Dir, err)
		if !config.ResumeAllowMissingGround() {
			return "", chdirErr
		}
		fmt.Fprint(os.Stderr, resumeDegradedNotice(chdirErr))
		return "", nil
	}
	// The process now stands in the row's ground, NOT where it started: a
	// descendant must not be handed the launch cwd as its working context
	// (taskExecutorWorkdir reads this override), and a later restore must not
	// drag the process back out of the ground the resume chose. Rule 2: the
	// same single writer the wrapper uses (task_session_cwd.go).
	if err := os.Setenv(tools.TaskWorkdirEnv, ground.Dir); err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] resume: workdir export: %v\n", err)
	}
	taskWorkdirOverride = ground.Dir
	fmt.Fprintf(os.Stderr, "[gopi] resume: working in %s (branch %s, model %s)\n",
		ground.Dir, ground.Branch, resumeGroundModelOf(ground.Dir))
	return ground.Dir, nil
}

// resumeGroundModelOf renders WHICH model the ground is in the landing notice,
// through the same judge the resolution used — a second reading that could say
// something else is exactly the defect devworkspace/ground.go was written to
// end. The label says the re-read failed rather than guessing when the
// directory cannot be read a moment after it was entered.
func resumeGroundModelOf(dir string) string {
	if st := devworkspace.ResolveGroundState(dir); st.Alive {
		return string(st.Model)
	}
	return resumeGroundModelUnknown
}

// resumeGroundModelUnknown is the notice's spelling for a directory the judge
// cannot name. Named, not blank (Rule 1b): a blank field reads as "we did not
// look".
const resumeGroundModelUnknown = "(re-read failed)"

// resumeGroundRefusalText renders the refusal the caller exits non-zero with.
// The four facts are the operator's whole repair path — WHAT the row records,
// WHAT the judge saw, WHY, and the command — so it is built from the TYPED
// refusal rather than from an error string (Rule 15: a refusal nobody can act
// on is not an explanation).
func resumeGroundRefusalText(progName string, err error) string {
	if refusal, ok := tools.TaskResumeGroundRefusalOf(err); ok {
		return fmt.Sprintf(resumeGroundRefusalFmt, progName, refusal.TaskID, refusal.Path,
			refusal.Model, refusal.Detail, refusal.Branch, refusal.Repo, refusal.Repair)
	}
	// Not a ground verdict (an unreadable ledger, an unknown row, a chdir the OS
	// refused): the sentence is the error's own, and the escape hatch is still
	// named — otherwise an operator whose row cannot be read has no next step.
	return fmt.Sprintf(resumeGroundFailureFmt, progName, err, config.ResumeAllowMissingGroundEnv)
}

// resumeGroundRefusalFmt is the ONE spelling of the ground refusal (Rule 1b):
// the exit path, its test and the report all read it.
const resumeGroundRefusalFmt = "%s: REFUSING to resume — the row's recorded ground is not usable.\n" +
	"  row:             %s\n" +
	"  recorded path:   %s\n" +
	"  model verdict:   %s\n" +
	"  detail:          %s\n" +
	"  recorded branch: %s\n" +
	"  recorded repo:   %s\n" +
	"  repair:          %s\n"

// resumeGroundFailureFmt is the same refusal for an error that is NOT a ground
// verdict (Rule 15: the two must never render alike).
const resumeGroundFailureFmt = "%s: REFUSING to resume — its ground could not be resolved: %v\n" +
	"  (to resume anyway in the CALLER's cwd, run it again with %s=1; such a run's commits " +
	"have no owner, no branch and no rollback point)\n"

// resumeDegradedNoticeFmt is the escape hatch's own warning: an operator who set
// the switch must still SEE what they bought.
const resumeDegradedNoticeFmt = "[gopi] resume: WARNING: DEGRADED — the row's ground is not usable (%v).\n" +
	"  Resuming in the CALLER's cwd %s because %s is set: this session's commits will have no owner, " +
	"no branch and no rollback point (Rule 37).\n"

// resumeDegradedNotice renders the escape hatch's warning.
//
// WHY IT NAMES THE CALLER'S CWD: the degradation is not chosen by this code — it
// is whatever the invoking process stood in. Measured 2026-10-07 (row
// 01a112e3): `gopi worker --resume --branch <name>` was launched from the MAIN
// checkout, so the directory a failed entry degrades to is the shared tree,
// where a commit has no owner, no branch and no rollback point (Rule 37).
// Naming the directory turns a silent degradation into a fact the operator can
// act on. (There used to be a second, one-line spelling of this notice —
// resumeFallbackNotice — kept alive only by its own test after the refusal
// landed; it was dead code proving a sentence the shipped path no longer
// printed.)
func resumeDegradedNotice(err error) string {
	return fmt.Sprintf(resumeDegradedNoticeFmt, err, cwdOrUnknown(), config.ResumeAllowMissingGroundEnv)
}

// resumeIdentityFor is WHAT A RESUME CONTINUES, in the three facts
// agent.RunIdentity needs — resolved from the filesystem the checkpoint lives
// in, never re-derived from an id (row 01a17dd8).
//
// THE DEFECT IT CLOSES, measured 2026-10-27. `gopi worker resume <row>` spawned
// `gopi --session <row> --task <row>`; the resolution was right (the checkpoint
// loaded, 21 messages seeded) but the AGENT was constructed with newAgent, which
// mints a fresh uuid, so the run wrote sessions/<new-uuid>/session.jsonl and a
// matching .raw.log while the row's own run directory — sessions/<row>/ with its
// fa8048d0-….jsonl and 210 MB .raw.log — was not touched. A resume that mints a
// session is not a resume: the operator's ruling is that it must use the OLD
// resources completely, not create a new session.
//
// WHY THE ID ALONE CANNOT BE REUSED (the second half, and why this returns a
// PATH). The record's directory is not a function of the session id: a task run
// keeps sessions/<task-id>/<run-uuid>.jsonl, a bare session keeps
// sessions/<uuid>/session.jsonl, and the task id that selects the first layout
// comes from GOPI_TASK_WRAPPER_ID — which the resume CHILD never carries. So
// "continue session <uuid>" alone re-derives the BARE layout and appends to
// sessions/<uuid>/session.jsonl, i.e. it creates the very second directory this
// fix exists to prevent. Naming the RECORD FILE is how the continued file is the
// file that exists.
//
// THE THREE ANSWERS, in order, each a definite fact:
//
//  1. a record in the checkpoint's own directory whose run id IS the raw log's
//     basename — the ordinary shape, one run's .jsonl beside its .raw.log;
//  2. else the directory's newest record — the serial-re-run shape, where the
//     resumed run's log is the latest one there;
//  3. else no record at all (the interrupted/first-round shape every isolated
//     profile produces): the SEGMENT is new but the RUN DIRECTORY is the one
//     that holds the checkpoint, so the record is named inside it. This is not
//     minting a session — the session id is the checkpoint's own.
//
// The session id always comes from the record (sessionlayout.SessionID: its
// session_start, else its layout), which is what makes the id stable across
// resumes and keeps ONE session to ONE id however many runs it takes.
func resumeIdentityFor(rawLogPath string) (agent.RunIdentity, bool) {
	dir := filepath.Dir(rawLogPath)
	rawBase := strings.TrimSuffix(filepath.Base(rawLogPath), scratch.SessionRawLogFileSuffix)

	recs := rundir.RecordsDir(dir)
	for _, rec := range recs {
		if rec.RunID == rawBase {
			return resumeIdentityOnRecord(rec.Path, rawLogPath), true
		}
	}
	if rec, ok := rundir.Latest(recs); ok {
		return resumeIdentityOnRecord(rec.Path, rawLogPath), true
	}
	// No record in the run directory: name one beside the checkpoint. The
	// session id is the raw log's own run id (they are minted together, see
	// NewAgentResuming), falling back to a fresh uuid for a raw log that is not
	// named like a run.
	//
	// THE SECOND RETURN VALUE IS FALSE when no record was READ — the caller
	// announces that the record was rebuilt from the checkpoint, under the
	// checkpoint's own id, in the checkpoint's own directory. That distinction
	// is the whole point (row 01a17df9-99c4): this branch PRESERVES identity
	// and is the documented recovery; the silent FORK was a different path (the
	// chat face, which had no id to reuse and minted a fresh session directory
	// beside the old one), and only that one is refused.
	// A CHECKPOINT THAT NAMES NO RUN DIRECTORY OF THIS PROFILE (measured
	// 2026-10-28, row 01a180f5). The three answers above are about CONTINUING a
	// session; they all assume the checkpoint's directory is one of THIS
	// profile's run directories (sessions/<run-id>/). When it is not — a raw log
	// copied out of a tree, a fixture's log, an export — there is no record to
	// rebuild and no directory to rebuild it IN, and the basename is NOT an id:
	// it is a FILE NAME. Using it as the session id is what stamped
	// `resumed:gopi-raw-resume-e2e.log` into a row (the id names no session of
	// any profile, so tools.TaskTagResumed — correctly — refused it and nothing
	// was ever linked), and it put the run's record beside the copy, where no
	// session of this profile lives at all.
	//
	// So this branch MINTS the run's own id and leaves SessionLogPath EMPTY: the
	// constructor then writes the record in the layout that id selects, i.e.
	// under the sessions root where a session of this profile belongs. The
	// CONVERSATION still continues from the checkpoint (RawLogPath is appended
	// to), which is exactly what the operator asked for; what does not continue
	// is a session RECORD, because the checkpoint named none.
	//
	// THE SCOPE IS THE SAME PREDICATE the record-gap refusal uses
	// (resumeCheckpointNamesARunDir), so "is this the profile's own run
	// directory" has ONE answer for the resume family (Rules 13/14).
	if !resumeCheckpointNamesARunDir(rawLogPath) {
		return agent.RunIdentity{
			SessionID:  agent.NewRunUUID(),
			RawLogPath: rawLogPath,
		}, false
	}
	sid := rawBase
	if sid == "" {
		sid = agent.NewRunUUID()
	}
	return agent.RunIdentity{
		SessionID:      sid,
		ResumedFrom:    sid,
		RawLogPath:     rawLogPath,
		SessionLogPath: filepath.Join(dir, sid+sessionlayout.JSONLSuffix),
	}, false
}

// resumeIdentityOnRecord is resumeIdentityFor's branch for a run directory that
// DOES hold a record: the record file is the continued log and its own id is
// the session id (sessionlayout.SessionID — the session_start's sessionId, else
// the layout's).
func resumeIdentityOnRecord(recordPath, rawLogPath string) agent.RunIdentity {
	sid := gopilog.FileToID(recordPath)
	return agent.RunIdentity{
		SessionID:      sid,
		ResumedFrom:    sid,
		RawLogPath:     rawLogPath,
		SessionLogPath: recordPath,
	}
}
