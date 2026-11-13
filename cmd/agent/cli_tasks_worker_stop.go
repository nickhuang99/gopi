package main

// cli_tasks_worker_stop.go — the worker's graceful stop (signal) handling.
//
// WHY IT EXISTS (signal-handling epic; sandbox rig /tmp/self-release-*): the
// producer-side worktree release is a DEFER on the round's close-out
// (workerRoundOwnWorktrees in cli_tasks_worker.go, and the wrapper's own call
// in runTaskRunOnce). Go's default SIGTERM/SIGINT disposition kills the
// process immediately, so a signal that lands mid-round — a supervisor's
// `timeout N`, a service unit's `systemctl stop`, Ctrl-C — unwinds NOTHING:
// the round's terminal writeback and its own-release never ran. Measured on
// the rig: `gopi worker --once` under `timeout 300` exited 143 with
// `worker: claimed <id>` as its last line, the row stayed in_progress and its
// worktree stayed in `git worktree list`, so scripts/safe-merge.sh gate [c]
// kept refusing the branch of a run that was already over. The defect is
// exactly the one the producer release exists to fix, one signal earlier.
//
// THE CONTRACT (the repo's established pattern: cli_chat.go, cli_web.go,
// merger_graceful.go — one flag, the handler does no work):
//
//	① a SIGTERM/SIGINT sets a REQUEST. The handler itself only records it —
//	   no locks, no I/O, no exit — because it runs in signal context.
//	② the round IN FLIGHT is not abandoned: the request is forwarded to the
//	   run child registered by runTaskRunOnce (registerPassChild, the merger's
//	   one registry — Rule 14), so the child winds down through its OWN
//	   session_end/close-out path instead of being orphaned under this
//	   process's death.
//	③ the round's close-out is what releases the ground: the loop returns
//	   only at a claim BOUNDARY, so the deferred release + writeback always
//	   run. Waiting for the child is deliberate — the child may hold the
//	   claim's writeback, and a wrapper that exits first would leave the row
//	   in_progress with nobody to close it.
//	④ the operator's ESCALATION is the explicit force spelling — SIGUSR1, or
//	   the durable FORCE_<pid> request — and NOT "a signal arrived twice": a
//	   repeated SIGTERM/SIGINT re-affirms the same graceful request (see
//	   installWorkerStopSignals). The escalation abandons the wait for the
//	   child immediately (SIGKILL, whole group), so a wedged child can never
//	   make the worker unkillable — while a duplicated graceful request can
//	   never kill the round it was meant to protect. SIGKILL needs no
//	   handling — the dead-holder sweep covers what an uncatchable stop
//	   leaves behind, and that limit is named in the devdoc.
//
// Exit code 0: "you asked it to stop and it stopped cleanly" is SUCCESS, not
// an error (the merger's retired mergerGracefulExitCode contract — the worker
// keeps the same operator-facing shape) — a service unit's
// restart policy keys on it.

import (
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/signalrec"
)

// workerStopSignals is the set of signals meaning "finish the in-flight round
// and exit". It shares its two signal choices with the merger's own loop
// (mergerStopSignals, in the retired loop file) — SIGTERM from a supervisor /
// service unit
// and SIGINT from an operator's terminal — and adds SIGUSR1, a DRAIN signal this
// tool's request-file protocol uses and the merger's loop deliberately does not
// (the merger has no request files to mean anything by it).
//
// SIGHUP IS IN THE SET, AND IT WAS THE MEASURED GAP (task 01a15bb9, 2026-10-21).
// A pty teardown — the operator closing the terminal, an ssh drop, a session
// scope going away — hangups the foreground worker. Before this, the hangup was
// NOT in the set, and the worker neither drained nor died: internal/signalrec's
// watchedSignals registers SIGHUP for every verb (which REPLACES the default
// disposition, so nothing dies by default), and signalrec then declines to
// re-raise for "worker" because that mode is listed as owning its own handlers.
// The signal landed in the seam between those two facts and was dropped in
// silence — recorded as {"signal":"SIGHUP","role":"worker","sourced":false} in
// the provenance ledger while the worker kept claiming rounds against a terminal
// session that no longer existed. Adding it here makes the registry's claim
// TRUE: the mode the recorder defers to is the mode that acts.
//
// It is GRACEFUL, not force (workerForceSignalRequested names SIGUSR1 alone).
//
// AND IT IS A DRAIN, NOT A STOP (measured 2026-10-27). Being in the set made the
// hangup behave EXACTLY like SIGTERM — a bare signal by the contract above, so
// startWorkerStopForwarder → forwardWorkerStopToChild reached into the in-flight
// run child with SIGTERM, and a round that had run 38 minutes closed out
// `failed: run interrupted by signal`. Measured on the ledger: 4 of 165 `failed`
// rows carried exactly that reason, and that count is a FLOOR — signal-provenance
// landed on master only on 2026-10-27, so the rows before it could not be
// attributed at all.
//
// THE SEMANTIC LINE: a terminal tab closing (a `vte-spawn-*.scope` or a
// `session-*.scope` being torn down) says "the window this process was typing
// into is gone". It does NOT say "abandon the work in flight", which is what
// SIGTERM from a supervisor means. So the hangup keeps its membership (Rule 24:
// an unregistered SIGHUP is a silent DROP — the mode owns its handler) but takes
// the DRAIN shape: the in-flight round is allowed to FINISH and the worker claims
// nothing new afterwards. Nothing is ever forwarded to the child on a hangup.
var workerStopSignals = []os.Signal{syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR1}

// workerHangupSignal reports whether sig is the TERMINAL TEARDOWN spelling — the
// one graceful signal whose meaning is "my window is gone", not "stop". It is a
// predicate over the signal VALUE (Rule 1/14): the handler classifies with it and
// nothing spells the signal twice.
func workerHangupSignal(sig os.Signal) bool {
	return sig == syscall.SIGHUP
}

// workerHangupReasonFmt is the recorded REASON of a hangup drain, rendered from
// the caught signal's own name (signalrec's spelling) and stopReasonSignalSuffix,
// so the termination line keeps the one vocabulary ("… received") an operator
// greps for. The clause after it states the SEMANTICS, because the reason is what
// a reader finds a week later when asking "why did this worker leave?".
const workerHangupReasonFmt = "%s%s — terminal teardown is a DRAIN, not a stop: " +
	"the in-flight round is allowed to finish and nothing new is claimed"

// workerLeaveLine is the ONE boundary verdict renderer: it picks the line by the
// SHAPE of the pending request, so no call site has to know which renderer
// applies (the habit that let a hangup inherit the bare-signal story). Escalation
// is passed through because a forced leave must never read like a clean drain.
func workerLeaveLine(reason string, forced bool) string {
	if workerHangupDrainRequested() && !forced {
		return workerHangupLine(reason)
	}
	return workerDrainLine(reason, forced)
}

// workerHangupLine renders the boundary verdict of a hangup drain. It is a
// SEPARATE renderer from workerDrainLine on purpose: that line says the request
// was "consumed at the claim boundary", which is the durable FILE's story, and a
// hangup has no file to consume. Both carry workerStopLinePrefix and the
// provenance suffix, so a reader still greps ONE prefix for every leaving worker.
func workerHangupLine(reason string) string {
	if reason == "" {
		reason = signalrec.SignalName(syscall.SIGHUP) + stopReasonSignalSuffix
	}
	// No second copy of the semantics here: the reason already carries them
	// (workerHangupReasonFmt), and a line that states them twice reads like two
	// different verdicts.
	return fmt.Sprintf(workerStopLinePrefix+"%s | %s", reason, workerTerminationProvenance())
}

// workerStopGraceDefault bounds the wait for the in-flight run child when the
// stop arrived as a BARE signal (no durable request): the sender — a
// supervisor's `timeout N`, a service unit's stop — is about to escalate to
// SIGKILL itself, so a worker that spent its last seconds waiting would lose
// the very close-out (writeback + own-worktree release) this mechanism exists
// to guarantee. The child is told first (SIGTERM, whole group); whatever is
// left after this window is killed so the round can close out.
//
// For a DRAIN (an operator's `--stop`/`--restart`, which wrote the request
// file first) this value is NOT a deadline — it is the report interval of a
// wait with no deadline, because that request means "let the round finish".
// Two shapes, one constant, and the call site says which one it is.
const workerStopGraceDefault = 5 * time.Second

// workerStopGraceEnv overrides that interval, in seconds (Rule 2: one reader,
// workerStopGrace). A malformed/negative value falls back to the default with
// a warning — never to "wait forever".
const workerStopGraceEnv = "GOPI_WORKER_STOP_GRACE"

// workerDrainCloseOutNote is the periodic line a draining worker prints while
// the in-flight round is still closing out: it says the worker is WORKING (not
// wedged) and names the escalation.
const workerDrainCloseOutNote = "worker: drain — the in-flight round is still closing out (waited %s so far); it will be allowed to finish. To interrupt it, repeat the request with the force spelling (%s)\n"

// workerForceTerminateNote is the operator-escalated arm's report: the
// interruption was ASKED FOR, and the round is truncated from here.
const workerForceTerminateNote = "worker: FORCE — the in-flight run child did not exit within %s; terminating %d so the round's close-out (writeback + own-worktree release) can run\n"

// workerSupervisorTerminateNote is the bare-signal arm's report: nobody asked
// this process to wait, so the bounded wind-down ends by terminating the child
// (and the round's close-out still runs). Distinct from the force note on
// purpose — "the operator interrupted it" and "the supervisor's clock ran out"
// are different facts to read in a log.
const workerSupervisorTerminateNote = "worker: stop — the in-flight run child did not wind down within %s; terminating %d so the round's close-out (writeback + own-worktree release) can run\n"

// workerForceEscalationHint names the force spelling in reports (one string, so
// the CLI's --force and the worker's own advice cannot drift).
const workerForceEscalationHint = "--force (or SIGUSR1)"

// workerStopPollInterval is the reporter's sampling period while the stop
// waits — coarse enough to cost nothing, fine enough that an operator sees
// the wait is progressing rather than wedged.
const workerStopPollInterval = time.Second

// workerStopGrace resolves the stop wait (Rule 2: the env is read in ONE
// place; the constant is the default).
func workerStopGrace() time.Duration {
	raw := strings.TrimSpace(os.Getenv(workerStopGraceEnv))
	if raw == "" {
		return workerStopGraceDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "worker: %s=%q is not a non-negative integer (seconds) — using default %s\n",
			workerStopGraceEnv, raw, workerStopGraceDefault)
		return workerStopGraceDefault
	}
	return time.Duration(n) * time.Second
}

// workerStopState is the one module-level stop record per process (the merger's
// mergerStopState shape): the handler sets it, the loop asks it at every claim
// boundary, and the exit path renders it. A mutex, not an atomic, because the
// reason string and the request travel together.
var workerStopState = struct {
	mu     sync.Mutex
	stop   bool
	reason string
	force  bool
	// drain marks a DURABLE-FILE request (the operator's `--stop`/`--restart`):
	// the in-flight round is allowed to finish. A bare signal leaves it false.
	drain bool
	// hangup marks the TERMINAL-TEARDOWN shape (SIGHUP): the in-flight round is
	// allowed to finish and nothing new is claimed, exactly like a durable drain —
	// the difference is only WHERE the request came from (a scope going away, not
	// a file). It is a THIRD field rather than a use of drain because the two are
	// consumed differently: drain is re-read from the durable file at every
	// boundary (a stat), while hangup is a fact about a signal that already
	// happened and has no file to re-read. Both answer workerRoundMustFinish.
	hangup bool
	// signal is the SPELLING recorded for the request (SIGTERM / SIGINT /
	// SIGUSR1): the signal path writes the name of the signal it caught, the
	// file path the signal the REQUESTER recorded in its header. It is empty
	// when the stop was delivered by FILE ALONE with no signal recorded —
	// which is a fact to print, not a gap to fill in.
	signal string
	// sender is the requester identity carried by the durable request file.
	// The zero value means UNSOURCED: a bare signal, a session teardown, a
	// pre-provenance caller, or a request file already cleaned up.
	sender workerStopSender
	// task is the LEDGER ID of the worker's LAST CLAIMED round, latched so the
	// termination line can name what was running. It is deliberately the LAST
	// claim and not "currently in flight": the line is printed AFTER the
	// in-flight round's close-out, so the round it names has finished — naming
	// it is still exactly what an operator needs ("which work was this worker
	// on when it was terminated"), and an empty value honestly says the worker
	// was terminated before it ever claimed anything.
	task string
}{}

// workerRequestStop records a stop request. First writer wins the reason — a
// later signal must not erase "SIGTERM received" with a bare escalation.
//
// The DRAIN flag is the shape of the request (epic worker-graceful-drain): a
// request that arrived through the DURABLE FILE means "finish the in-flight
// round, do not kill it", while a bare signal means "wind down now, my
// supervisor's clock is running". The two are remembered separately because
// the wait at the round's boundary is different for each — see
// awaitWorkerStopChild.
//
// The SIGNAL SPELLING is recorded the same first-writer-wins way as the reason
// (Rule 2: one fact, one writer): the caller that first asked for the stop is
// the one whose "who/what" the termination line must report.
func workerRequestStop(reason string, force bool) {
	workerRequestStopWithSignal(reason, force, "")
}

// workerRequestStopWithSignal is workerRequestStop plus the signal spelling the
// request arrived as. signal is the empty string when the request carried no
// signal at all — a file-only stop — and the termination line then says so
// instead of guessing.
func workerRequestStopWithSignal(reason string, force bool, signal string) {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	if force {
		workerStopState.force = true
	}
	if !workerStopState.stop {
		workerStopState.stop = true
		workerStopState.reason = reason
		workerStopState.signal = signal
	} else if workerStopState.signal == "" && signal != "" {
		// The first writer asked by FILE (no signal); a later signal re-affirms
		// the same request and is the only place the spelling can come from.
		// This is an ADDITION to a missing fact, never a rewrite of one.
		workerStopState.signal = signal
	}
}

// workerRecordStopSender files the requester identity a durable request carried
// (the claim-boundary consume). It is a separate writer from the signal path
// because the two facts arrive by different routes; the FIRST non-empty sender
// wins, so a second request cannot rewrite the identity of the first.
func workerRecordStopSender(s workerStopSender) {
	if !s.Sourced() {
		return
	}
	workerStopState.mu.Lock()
	if !workerStopState.sender.Sourced() {
		workerStopState.sender = s
	}
	workerStopState.mu.Unlock()
}

// workerRecordStopSignal records the signal spelling a request file declared
// (see workerRecordStopSender for why this is not part of workerRequestStop).
func workerRecordStopSignal(signal string) {
	if signal == "" {
		return
	}
	workerStopState.mu.Lock()
	if workerStopState.signal == "" {
		workerStopState.signal = signal
	}
	workerStopState.mu.Unlock()
}

// workerRecordStopTask latches the ledger id of the round just claimed — the
// TARGET field of a termination line. It is never cleared: the last claimed
// round is the honest answer to "what was this worker doing?" after the round
// has closed out (a cleared latch would print "none" for a worker that ran a
// full round before it was terminated).
func workerRecordStopTask(taskID string) {
	if taskID == "" {
		return
	}
	workerStopState.mu.Lock()
	workerStopState.task = taskID
	workerStopState.mu.Unlock()
}

// workerStopSignalSpelling is the recorded signal ("" when the stop carried
// none).
func workerStopSignalSpelling() string {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.signal
}

// workerStopSenderRecord is the recorded sender identity (the zero value when
// no sender record reached this process).
func workerStopSenderRecord() workerStopSender {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.sender
}

// workerStopTargetTask is the ledger id of the last claimed round ("" when the
// worker was terminated before claiming anything).
func workerStopTargetTask() string {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.task
}

// workerMarkDrainRequested records that the pending stop is a DRAIN (it came
// from the durable request file). It is separate from workerRequestStop so the
// signal path cannot accidentally claim the drain shape: a caller that only
// signals leaves the flag false (the bare-signal contract) — with ONE exception,
// workerMarkHangupDrain, because a hangup's shape is decided by WHICH signal it
// was, and that is knowledge only the signal handler has.
func workerMarkDrainRequested() {
	workerStopState.mu.Lock()
	workerStopState.drain = true
	workerStopState.mu.Unlock()
}

// workerMarkHangupDrain records the TERMINAL-TEARDOWN shape (SIGHUP): finish the
// in-flight round, claim nothing new, forward nothing. It is called ONLY from the
// signal handler, where the signal's identity is the one fact available.
func workerMarkHangupDrain() {
	workerStopState.mu.Lock()
	workerStopState.hangup = true
	workerStopState.mu.Unlock()
}

// workerClearHangupDrain drops the terminal-teardown shape when a LATER graceful
// signal takes the request over. It exists because the two facts are decided at
// different instants: the hangup says "my window is gone", and a supervisor's
// SIGTERM that follows says "stop now, my clock is running" — the second is a
// strictly stronger request and must not be swallowed by the first (a worker
// that kept the drain shape would sit in an unbounded wait while the supervisor
// counts down to SIGKILL).
func workerClearHangupDrain() {
	workerStopState.mu.Lock()
	workerStopState.hangup = false
	workerStopState.mu.Unlock()
}

// workerHangupDrainRequested reports whether the pending stop arrived as a
// terminal teardown (false when nothing was requested, when the request was the
// durable file, or when it was a bare SIGTERM/SIGINT).
func workerHangupDrainRequested() bool {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.hangup
}

// workerRoundMustFinish is the ONE predicate the round boundary asks: "may the
// in-flight round run to its own close-out with NO deadline?" Two shapes answer
// yes — the durable request file (an operator's `--stop`/`--restart`) and a
// terminal hangup — and they answer it for the same reason: neither one is a
// supervisor whose SIGKILL is imminent. Everything else (SIGTERM, SIGINT) keeps
// the bare-signal contract and hands the child a bounded wait.
//
// It exists so no call site can pass the file-only predicate by habit (Rules
// 13/14: one reader of a shared decision — that habit IS how the hangup used to
// die in the bare-signal arm).
func workerRoundMustFinish() bool {
	return workerDrainRequestedByFile() || workerHangupDrainRequested()
}

// workerDrainRequestedByFile reports whether the pending stop is the durable
// drain shape (false when nothing was requested, or when the request is a bare
// signal).
func workerDrainRequestedByFile() bool {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.drain
}

// workerStopRequested reports whether a graceful stop is pending.
func workerStopRequested() bool {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.stop
}

// workerStopReason is the recorded reason ("" when none).
func workerStopReason() string {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.reason
}

// workerStopEscalated reports whether the FORCE spelling arrived (SIGUSR1 or a
// FORCE_<pid> request) — the operator's explicit "stop waiting for the child".
// It is deliberately NOT "a signal arrived more than once": since the epic
// worker-graceful-drain, a repeated graceful request is a no-op, so escalation
// happens only when the operator spells it.
func workerStopEscalated() bool {
	workerStopState.mu.Lock()
	defer workerStopState.mu.Unlock()
	return workerStopState.force
}

// workerStopLinePrefix is the fixed head EVERY worker stop/drain verdict shares
// (workerStopLine + workerDrainLine) — the ONE spelling the reader, the
// operator's `--stop` waiter and the tests match on when the question is only
// "did this worker stop, and was it let finish?" (Rule 1b: the prefix is a
// value with meaning, so it has a name).
const workerStopLinePrefix = "worker: "

// workerTerminationProvenance renders the provenance SUFFIX every termination
// line carries: who asked, with what, against which task, at what time. It is
// ONE renderer (Rule 2) so the drain line and the after-close-out line cannot
// describe the same event in two different ways — the defect this closes was
// precisely that the artifact was self-describing enough to be read and not
// enough to be acted on.
//
// The TASK field is the last claimed round's id, or workerStopNoTaskMarker when
// the worker was terminated before claiming anything; the SIGNAL field is the
// recorded spelling or workerStopNoSignalMarker for a file-only stop; the AT
// field is the wallclock of the termination — the moment the line was written,
// on the same clock as the session log's other lines.
func workerTerminationProvenance() string {
	task := workerStopTargetTask()
	if task == "" {
		task = workerStopNoTaskMarker
	}
	signal := workerStopSignalSpelling()
	if signal == "" {
		signal = workerStopNoSignalMarker
	}
	return fmt.Sprintf("%s %s %s %s %s %s %s",
		workerStopFieldSenderLabel, workerStopSenderRecord().Render(),
		workerStopFieldSignalLabel, signal,
		workerStopFieldTaskLabel, task,
		workerStopFieldAtLabel+" "+time.Now().Format(time.RFC3339))
}

// workerStopLine is the ONE operator-facing line a stopped worker prints: the
// reason and the fact that the round was closed out rather than killed. A
// silent exit would make "stopped" indistinguishable from "crashed" (Rule 15).
//
// It carries the PROVENANCE clause (epic signal-provenance-unsourced): the same
// line used to be printed for an operator's drain and for an unannounced kill,
// so the two deaths were byte-identical in the log. The clause is appended by
// the caller through recordStopVerdict, which owns the signalrec verdict — this
// function stays a pure renderer of the reason.
func workerStopLine(reason string) string {
	if reason == "" {
		reason = "stop requested"
	}
	return fmt.Sprintf(workerStopLinePrefix+"%s — stopping after the in-flight round's close-out (own-worktree release included) | %s",
		reason, workerTerminationProvenance())
}

// stopReasonSignalSuffix is the tail every SIGNAL-shaped stop reason ends with
// — the reason a SIGTERM/SIGINT-requested stop is spelled with, for the worker
// (installWorkerStopSignals) AND for the merger (mergerInstallStopSignals):
// "<signal> received". It exists as a value because that phrase is what a
// reader greps for when asking "who stopped this component?" and what the tests
// pin — a second spelling (a bare literal at either signal handler) would
// silently un-pin them (Rule 1b/13).
const stopReasonSignalSuffix = " received"

// workerDrainLine is the line the worker prints when it leaves from the
// CLAIM-BOUNDARY consume (i.e. before claiming anything): the boundary verdict
// an operator's `--stop` is waiting on, and the observable proof that the
// request was honoured at the boundary rather than by the trailing ticker. The
// escalation is named when it happened — a forced leave must never read like a
// clean drain.
//
// BOTH SHAPES CARRY THE PROVENANCE SUFFIX (epic: "terminated received" is
// unattributable). Measured before this work: 31 termination lines on zbook,
// 31/31 reading exactly this text with no sender, no signal, no task and no
// wallclock, and only 3/31 resolvable to a task id at all — so "who killed the
// workers?" had no answer derivable from the artifact the system kept. The
// suffix is on the SAME line on purpose: a reader greps ONE line and has the
// whole event, rather than having to join this log against
// signal-provenance.jsonl (which is where the provenance used to live, in a
// file the log reader never opened).
func workerDrainLine(reason string, forced bool) string {
	if reason == "" {
		reason = workerDrainVerb + " requested"
	}
	if forced {
		return fmt.Sprintf(workerStopLinePrefix+"%s — FORCED: the in-flight round was interrupted at the operator's request; leaving now | %s",
			reason, workerTerminationProvenance())
	}
	return fmt.Sprintf(workerStopLinePrefix+"%s — %s (consumed at the claim boundary: nothing new claimed, leaving) | %s",
		reason, workerDrainVerb, workerTerminationProvenance())
}

// workerProgVersion is the running binary's version, as `gopi version` prints
// it. It is a named helper (not an inline buildVersion() at the call sites) so
// the state file, the restart report and the log line cannot disagree about
// which binary this process is.
func workerProgVersion() string {
	return buildVersion()
}

// installWorkerStopSignals wires the graceful-stop signals for THIS process
// and returns the uninstall function its caller must defer (leaving the
// handler installed would swallow the next signal with nothing left to act on
// it). The returned function is safe to call twice.
//
// The handler does NOTHING but record the request and forward it to the live
// run child: it runs in signal context, where anything heavier (a lock held by
// the interrupted code, an allocation-free predicate over the ledger) is a
// deadlock or a corruption. Forwarding is the one piece of work the child
// needs to see — without it the child keeps running while the wrapper waits.
//
// ESCALATION IS THE OPERATOR'S EXPLICIT SPELLING, NOT A COUNT (epic
// worker-graceful-drain, 2026-09-13). The predecessor rule was "any second
// signal escalates", which made a REPEATED graceful request — `--stop` run
// twice, a service manager's stop+retry, a script's idempotent stop — SIGKILL
// the in-flight round: exactly the outcome the graceful path exists to
// prevent, triggered by a duplicate. Work is now escalated by SIGUSR1 (the
// force spelling) or by the durable FORCE_<pid> request, and a repeated
// SIGTERM/SIGINT re-affirms the SAME graceful request (first writer keeps the
// reason, the flag is already set) and changes nothing.
func installWorkerStopSignals() func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, workerStopSignals...)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				// THE SIGNAL'S MEANING IS DECIDED HERE, and for the hangup it is
				// NOT "stop": a terminal teardown drains the round and forwards
				// nothing (see workerStopSignals' comment and the measured
				// failed-row). Everything else keeps the bare-signal contract.
				hangup := workerHangupSignal(sig)
				reason := fmt.Sprintf("%v%s", sig, stopReasonSignalSuffix)
				if hangup {
					reason = fmt.Sprintf(workerHangupReasonFmt, signalrec.SignalName(sig), stopReasonSignalSuffix)
				}
				// A FORCE signal (SIGUSR1, the merger's interrupt spelling) is
				// the operator's explicit "do not wait" — the ONLY signal that
				// escalates. One question, one flag: every exit path asks the
				// same thing.
				force := workerForceSignalRequested(sig)
				// The signal's own SPELLING is recorded with the request: the
				// termination line must name the signal, and a bare signal is
				// the one case where the victim is its only witness (the
				// sender's identity is unrecoverable by construction — Linux
				// hands no SA_SIGINFO sender pid to a handler — so the line
				// renders the sender as UNSOURCED rather than inventing one).
				workerRequestStopWithSignal(reason, force, signalrec.SignalName(sig))
				// THE SHAPE follows the signal: a hangup is a DRAIN, and any
				// LATER graceful signal is a supervisor taking the request over
				// — it clears the drain flag, so a SIGTERM that arrives after a
				// hangup is still forwarded (the operator's own "stop", spelled
				// the way a supervisor spells it). First writer keeps the
				// reason; only the SHAPE can be re-decided.
				if hangup {
					workerMarkHangupDrain()
				} else if !force {
					workerClearHangupDrain()
				}
				// The forwarder is WOKEN, nothing more. It is the piece that
				// reaches the child while the round is still in flight — a
				// round does not return by itself (runTaskRunOnce blocks in
				// cmd.Wait() on that very child), so a stop that is only
				// handled at the round's boundary is a stop that never lands.
				// The nudge is the one thing a signal handler may do here: a
				// one-slot non-blocking send, no allocation, no lock, no I/O.
				// The forwarding DECISION still belongs to the forwarder
				// goroutine, which can tell a DRAIN (the durable request file
				// is standing — the round must finish) from a bare signal (the
				// supervisor's clock is running) and can do file I/O.
				notifyWorkerStopNudge()
				// The child is NOT touched here for either shape: the
				// forwarding decision belongs to the waiter on the loop's own
				// goroutine (awaitWorkerStopChild), which can tell a DRAIN (the
				// durable request file is standing — the round must finish)
				// from a bare signal (the supervisor's clock is running), and
				// which can do file I/O — this handler runs in signal context,
				// where a stat is already too much.
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// workerStopNudge is the forwarder's wake-up: a ONE-SLOT buffered channel, so
// a nudge can never be lost and can never block its sender. The signal handler
// and the durable-file watcher both write it; the forwarder goroutine is its
// only reader.
var workerStopNudge = make(chan struct{}, 1)

// notifyWorkerStopNudge wakes the round-scoped forwarder without blocking. The
// caller may be a SIGNAL HANDLER, where a blocking send (or any allocation) is
// not allowed — a nudge already pending is simply the same wake-up.
func notifyWorkerStopNudge() {
	select {
	case workerStopNudge <- struct{}{}:
	default: // a nudge is already pending: the forwarder has not drained it yet
	}
}

// workerStopDrainSettle is how long a BARE-signal nudge settles before it
// forwards into the run child, so a `--stop`'s durable request file — written
// BEFORE its signal — is observed first and the drain guarantee holds even when
// the file and the signal arrive in the same instant. It is the daemon's own
// request-file poll interval (workerFileWatchInterval): the settle costs the
// latency that contract already pays, and stays far inside the bare-signal
// grace window (workerStopGraceDefault).
const workerStopDrainSettle = workerFileWatchInterval

// startWorkerStopForwarder starts the loop-lifetime forwarder and returns its
// stop function (a deferred call: it is what releases the goroutine when the
// loop leaves).
func startWorkerStopForwarder() func() {
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-workerStopNudge:
				forwardWorkerStopDuringRound(stop)
			}
		}
	}()
	return func() { close(stop) }
}

// forwardWorkerStopDuringRound delivers a pending stop to the IN-FLIGHT run
// child while the round is still running — the step the boundary wait could
// not perform because it is only reached once the child is already gone.
//
// SHAPE DISCIPLINE (the drain guarantee is preserved): a DURABLE drain
// (`--stop`/`--restart`, which wrote the request file) forwards NOTHING — that
// request means "let the in-flight round finish" (measured 2026-09-13: a plain
// `--restart` that forwarded SIGTERM closed the row out `failed: signal:
// terminated`). A TERMINAL HANGUP is the same shape for a different reason: the
// tab that went away never asked for the work to stop (measured 2026-10-27, a
// 38-minute round closed out `failed: run interrupted by signal`), so it
// forwards nothing either. Both are asked through the ONE predicate,
// workerRoundMustFinish. Only a BARE signal — the supervisor's `timeout N`, a
// service unit's stop — is forwarded, which is that shape's pre-existing
// contract: awaitWorkerStopChild's non-drain arm already forwards at once; it
// was simply unreachable mid-round. Escalation (force) is forwarded immediately
// for either shape: the operator spelled "do not wait".
func forwardWorkerStopDuringRound(stop <-chan struct{}) {
	// ① Read the durable files FIRST (the ONE reader, idempotent): a drain must
	// be recognised before anything is forwarded, because forwarding a drain
	// ends the very round it asked to finish.
	workerCtx.consumeDrainRequests()
	if workerStopEscalated() {
		forwardWorkerStopToChild(true)
		return
	}
	if workerRoundMustFinish() {
		return
	}
	// ② A bare signal: settle, then re-read. The file may still be landing
	// (workerStopDrainSettle) — and once it is there, this stop is a drain and
	// nothing is forwarded.
	select {
	case <-time.After(workerStopDrainSettle):
	case <-stop:
		return
	}
	workerCtx.consumeDrainRequests()
	if workerStopEscalated() {
		forwardWorkerStopToChild(true)
		return
	}
	if workerRoundMustFinish() {
		return
	}
	// ③ Still bare: tell the child now, so the round can end and its close-out
	// (writeback + own-worktree release) can run. Group-directed, so the child's
	// own grandchildren come along (forwardWorkerStopToChild).
	forwardWorkerStopToChild(false)
}

// forwardWorkerStopToChild delivers a FORCED stop to the registered run child,
// if any. It is called ONLY for escalation (the force spelling), never for a
// graceful drain: a graceful request means "let the in-flight round finish",
// and signalling the child would end the very round the operator asked us to
// complete — measured 2026-09-13 on the drain rig, where a plain `--restart`
// forwarded SIGTERM into the run child and the row closed out `failed: signal:
// terminated` (a stop, reported as a failure).
//
// Group-directed (the merger's killPassChildren rule: a child's grandchildren —
// a build, a test binary — must come along, or the "stop" leaves work running
// against the repo). Best-effort and quiet on ESRCH: the child may have exited
// between the registry read and the signal, which is precisely the state the
// caller wants.
func forwardWorkerStopToChild(force bool) {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	for _, c := range livePassChildren() {
		if c.Kind != passChildKindRun {
			continue
		}
		if !signalRunChild(c.PID, sig) {
			fmt.Fprintf(os.Stderr, workerStopUndeliveredNote, c.PID)
		}
	}
}

// signalRunChild delivers sig to the in-flight run child, covering its
// GRANDCHILDREN (the agent grandchild, a bash tool, the tool's own children).
//
// WHY NOT JUST kill(-c.Group) (measured 2026-09-17, devdoc/worker-sigterm-
// futex-hang.md): the registry records Group = the child's pid as an
// ASSUMPTION (child_registry.go: "== its pid whenever the caller set Setpgid"),
// but the task-run wrapper spawns its child WITHOUT Setpgid — so the child's
// real process group is the WORKER's, and `kill(-childPID)` addressed a group
// that does not exist. ESRCH was swallowed as benign, so the forward reported
// success while delivering NOTHING: this was the second reason the worker hung
// (the first was the forward only running after the round).
//
// So the group is not trusted — it is MEASURED: the child's own process group
// id is read from the process table (Getpgid). It is used only when it is the
// child's OWN group (pgid == pid, i.e. the caller really did set Setpgid, as
// internal/tools/bash.go does for a tool child): then a group signal is exact
// and takes the grandchildren along. Otherwise the child is signalled directly
// and its DESCENDANTS are signalled individually — derived from the live
// process table, never assumed from the registry.
//
// THE SECOND HALF OF THE GROUP GUARD (same defect family as task 01a163df,
// measured 2026-10-22 — see internal/tools/bash.go's
// killChildTreeGroupIsTheCallers, whose shape this mirrors). `pgid == pid` asks
// "does the CHILD lead this group?"; it cannot answer the question that makes a
// group signal fatal: "is the group I am about to address the one I MYSELF live
// in?" Those coincide whenever the worker is itself a group leader — the normal
// shape of a chat/worker started from a shell — and a run child that was spawned
// WITHOUT Setpgid then has pgid == the WORKER's pid == the child's own pid only
// when the worker spawned it as a leader. One `kill(-pgid, SIGKILL)` there takes
// the worker, its round and the operator's terminal down together, and SIGKILL
// cannot be caught, so no handler can soften it: the guard is the only defence.
// When the group is ours (or names our pid, i.e. the group we live in), the
// targeted path below still runs — the child AND its enumerated descendants are
// signalled individually, so bug011's deadlock (a pipe write end held by an
// unsignalled descendant) cannot come back.
//
// Reports whether ANY signal was delivered, so the caller can never again let a
// stop look like it succeeded while nothing was sent (Rule 15).
func signalRunChild(pid int, sig syscall.Signal) bool {
	if pid <= 0 {
		return false
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid && !runChildSignalGroupIsTheCallers(pgid) {
		return runChildGroupSignal(pgid, sig)
	}
	// The descendants are enumerated BEFORE the child is signalled: killing the
	// child first REPARENTS its orphaned grandchildren to init, so a walk taken
	// afterwards would no longer find them (measured in
	// TestWorkerStopReachesGrandchildren: the grandchild outlived the stop).
	descendants := descendantPIDs(pid)
	delivered := syscall.Kill(pid, sig) == nil
	for _, d := range descendants {
		if syscall.Kill(d, sig) == nil {
			delivered = true
		}
	}
	return delivered
}

// WHY THESE ARE SEAMS, and not the direct calls they wrap (the same reasoning
// internal/tools/bash.go records for its pair — measured, not stylistic). The
// hazard is a state TWO processes cannot be placed in: a group id names its
// leader's pid, so "a live child leads the group I am in" is a contradiction —
// a fixture can only ever produce "the child leads a group that is NOT mine",
// which is the SAFE branch. The refused branch is therefore made observable by
// lowering the READING of "my group" to the child's group (runChildSignalGroupOf),
// which is exactly the predicate production evaluates when a stray process really
// does share the caller's group. The group SIGNAL itself is a seam for a second
// reason: both branches end with the child dead, so the OUTCOME cannot tell
// "refused the group" from "signalled the group" — only a recorder that sees which
// group was addressed can. Neither seam changes what production does.
var (
	// runChildSignalGroupOf reads the process group this process is in.
	runChildSignalGroupOf = syscall.Getpgrp
	// runChildGroupSignal delivers sig to a whole process group, reporting
	// whether anything was addressed (false on ESRCH: the group is already gone,
	// which is the state the caller wants).
	runChildGroupSignal = func(pgid int, sig syscall.Signal) bool {
		return syscall.Kill(-pgid, sig) == nil
	}
)

// runChildSignalGroupIsTheCallers reports whether a group signal addressed at
// pgid would reach the process that is doing the killing: either the group IS
// this process's group, or the number names this process's own pid (the id of the
// group it lives in when it leads one).
func runChildSignalGroupIsTheCallers(pgid int) bool {
	return pgid == runChildSignalGroupOf() || pgid == os.Getpid()
}

// descendantPIDs returns the live descendants of pid (its children,
// grandchildren, …), read from /proc. Best-effort by contract: an unreadable
// entry is skipped rather than failing the stop, and the walk is bounded by the
// process table itself (a descendant can only have one parent, so the walk
// terminates).
func descendantPIDs(pid int) []int {
	children := map[int][]int{}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue
		}
		ppid := readPPID(p)
		if ppid <= 0 {
			continue
		}
		children[ppid] = append(children[ppid], p)
	}
	var out []int
	queue := append([]int(nil), children[pid]...)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		out = append(out, cur)
		queue = append(queue, children[cur]...)
	}
	return out
}

// readPPID reads a pid's parent from /proc (0 when unreadable).
//
// The stat PARSE is not re-inlined here: `pid (comm) state ppid …` is read
// through internal/procname.StatFieldString — the ONE home of the
// count-from-the-last-')' rule, whose field NUMBERING (field 4 = parent) is
// what makes comm-as-a-process-name safe (Rules 13/14). An unreadable file /
// unusable line reports 0, the "unknown" value this caller's contract uses.
func readPPID(pid int) int {
	return readPPIDAt(procRoot, strconv.Itoa(pid))
}

// readPPIDAt is the read itself, rooted at an explicit procfs mount so the
// parse is testable against a fixture instead of a live process
// (readppid_parenthesised_comm_test.go).
func readPPIDAt(root, pid string) int {
	raw, ok := procname.StatFieldString(procname.StatStatPath(root, pid), procname.StatFieldParent)
	if !ok {
		return 0
	}
	ppid, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return ppid
}

// procRoot and procStatFileName name the kernel's process table (Rule 1b: the
// spelling lives here, not inline at the read).
const (
	procRoot         = "/proc"
	procStatFileName = "stat"
)

// workerStopUndeliveredNote is the honest report of a stop that reached NO
// process: before this existed the ESRCH was swallowed and the operator saw a
// "draining" worker that was never actually told to drain.
const workerStopUndeliveredNote = "worker: stop — the in-flight run child %d received NO signal (pid or process group already gone); it is left to its own close-out\n"

// awaitWorkerStopChild waits for the in-flight run child to finish, and
// TERMINATES it only when the operator has escalated (the force spelling): the
// writeback and the producer's own-worktree release are the guarantee the
// drain exists for, so a graceful drain waits for them instead of killing the
// work it was asked to let finish. The wait is reported, never silent
// (Rule 15) — an operator whose worker is still closing out must be able to
// see that it is working, not wedged — and the escape hatch (force) is named
// in the report.
//
// TWO GRACEFUL SHAPES, ONE DECISION (epic worker-graceful-drain): the caller
// tells us WHICH one this is, because they are not the same request.
//
//   - drain (an operator's `--stop`/`--restart`, which wrote the durable
//     request BEFORE signalling): the in-flight round is allowed to FINISH.
//     No deadline kills it — killing it would end the round as a failure,
//     which is the outcome `--stop` was asked to prevent (measured on the
//     drain rig 2026-09-13: `failed: signal: terminated` on a plain
//     --restart). The force spelling is the only interruption.
//   - wind-down (a BARE signal — a supervisor's `timeout N`, a service unit's
//     stop, Ctrl-C — with no request file): the sender is about to escalate to
//     SIGKILL itself, so the child is told at once and the wait is bounded
//     (workerStopGraceDefault), then terminated so the close-out still runs.
//     This is the pre-existing signal contract, unchanged.
func awaitWorkerStopChild(drain bool) {
	if drain {
		awaitWorkerDrainChild()
		return
	}
	// The bare-signal shape keeps its pre-existing contract: tell the child at
	// once (a supervisor's stop must reach the run child, whose own close-out
	// is still its business), then bound the wait and terminate what is left so
	// THIS round's writeback + own-worktree release still run.
	forwardWorkerStopToChild(false)
	grace := workerStopGrace()
	deadline := time.Now().Add(grace)
	for {
		if workerStopEscalated() {
			break // the operator spelled the escalation: stop waiting
		}
		if left := waitForChildren(workerStopPollInterval); len(left) == 0 {
			return
		}
		if !time.Now().Before(deadline) {
			break
		}
	}
	terminateInFlightChild(grace, workerSupervisorTerminateNote)
}

// awaitWorkerDrainChild waits for the in-flight round to FINISH (no deadline):
// it reports periodically so a long close-out is visibly progressing, and it
// terminates only on the force spelling.
func awaitWorkerDrainChild() {
	grace := workerStopGrace()
	nextReport := time.Now().Add(grace)
	for {
		if workerStopEscalated() {
			terminateInFlightChild(grace, workerForceTerminateNote)
			return
		}
		if left := waitForChildren(workerStopPollInterval); len(left) == 0 {
			return
		}
		if !time.Now().Before(nextReport) {
			fmt.Fprintf(os.Stderr, workerDrainCloseOutNote, grace, workerForceEscalationHint)
			nextReport = time.Now().Add(grace)
		}
	}
}

// terminateInFlightChild is the ESCALATED arm: whatever is still running is
// killed (whole group) so the round's close-out can run. It is reported — a
// truncated round must be distinguishable from a completed one in the log.
// The note is passed in because the two spellings (an operator's force, a
// supervisor's deadline) are different facts and must not share a line.
func terminateInFlightChild(grace time.Duration, note string) {
	left := livePassChildren()
	if len(left) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, note, grace, len(left))
	forwardWorkerStopToChild(true)
}

// workerIdleDrainedWait is the drained-queue wait of the NEVER-SELF-EXIT face
// (GOPI_WORKER_IDLE_EXITS=0). It is a stop-aware sleep, never a raw
// time.Sleep: installWorkerStopSignals REPLACES SIGTERM's default disposition,
// so a raw sleep turns a 1h idle interval into a 1h stop delay — the signal
// sets a flag and the sleeper never looks at it (measured 2026-09-16: SIGTERM
// and `worker --stop` both landed only after the whole interval).
//
// It differs from waitWorkerIdleOrStop in ONE way: it also notices the durable
// request FILE. A `--stop` writes TERM_<pid> and then signals, and the loop
// must still be interrupted when the signal never arrived (a foreign binary, a
// caller that can only write a file) — the control contract's "the signal is
// the nudge, the file is the record, and either alone is sufficient". The file
// is only STATed here, never consumed: exiting is the loop's job at its own
// claim boundary.
//
// A failed stat is reported once per wait, not once per poll (Rule 15b): a
// real error means the stop could not be observed, and a silent scan would
// hide exactly that.
func workerIdleDrainedWait(d time.Duration) {
	dir, pid := workerStopRequestLocation()
	var statErrReported bool
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if workerStopRequested() {
			return
		}
		if dir != "" {
			exists, err := workerStopRequestFileExists(dir, pid)
			switch {
			case err != nil:
				if !statErrReported {
					fmt.Fprintf(os.Stderr, "worker: could not read the durable stop request: %v\n", err)
					statErrReported = true
				}
			case exists:
				// Return WITHOUT flipping the flag: the loop's claim boundary
				// consumes the file (consumeDrainRequests) and renders the
				// drain line, so there is exactly ONE reader of a request
				// (Rule 2) and a wake-up can never invent a stop reason.
				return
			}
		}
		step := workerStopPollInterval
		remaining := time.Until(deadline)
		if remaining < step {
			step = remaining
		}
		time.Sleep(step)
	}
}

// waitWorkerIdleOrStop sleeps for the drained-queue interval but wakes early
// when a stop request arrives, so a service manager's stop does not have to
// wait out the interval. The request is NOT consumed here — the loop's next
// boundary check reads it and exits — which keeps ONE reader of the flag
// (Rule 2).
func waitWorkerIdleOrStop(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if workerStopRequested() {
			return
		}
		step := workerStopPollInterval
		remaining := time.Until(deadline)
		if remaining < step {
			step = remaining
		}
		time.Sleep(step)
	}
}
