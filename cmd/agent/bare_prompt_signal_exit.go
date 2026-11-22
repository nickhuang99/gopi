package main

// bare_prompt_signal_exit.go — the terminating half of the bare-prompt face's
// signal handling (defect row 01a1e59c).
//
// THE MEASURED DEFECT, so a later reader does not read this file as
// belt-and-braces. A one-shot `gopi submit` run (a bare word that is not a
// verb, so it fell through to the bare-prompt path and bought a real agent run
// — session a59966eb, 23 turns) finished its work on 2026-11-05 and then
// stayed alive for ELEVEN DAYS:
//
//	utime/stime, rchar/wchar, syscr/syscw — two samples 10 s apart: all identical
//	36 of 37 threads wchan=futex_wait_queue, no child process, no socket
//	/proc/<pid>/status SigCgt bit for SIGTERM (15) was SET
//	kill -TERM → still there 3 s later; only SIGKILL ended it
//
// Two mechanisms combined to produce that, and this file is the second:
//
//  1. the process never left (the run returned, and `main` returned from a face
//     whose goroutines were still parked — the recorder's, the console-log
//     fan-out's, the signal watcher's), and
//  2. every terminating signal was RECORDED and then DROPPED, because this face
//     calls signalrec.ClaimSignalHandlers() (it owns the graceful path) while
//     its own handler only cancelled a context that nobody was waiting on.
//
// The kernel's view is the one that matters to an operator: `SigCgt` says a
// handler covers SIGTERM, so the default action is gone — and if the handler
// does not terminate, NOTHING terminates short of SIGKILL. A terminating
// signal must terminate.

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	// signalExitBase is the conventional exit status of a process that ended
	// because of signal N: 128 + N (130 = SIGINT, 143 = SIGTERM, 129 = SIGHUP).
	// It is the SAME convention internal/replayplay and internal/sandbox use,
	// restated here as a named constant rather than a literal (Rule 1) because
	// this package cannot import either of them for one integer.
	signalExitBase = 128

	// signalExitEscalationGrace bounds how long the run's own unwind is given
	// after Interrupt() before this face leaves anyway.
	//
	// WHY A GRACE AT ALL, AND WHY THIS SHORT. The unwind's product is the
	// session's own record (session_end + the auto ledger row), and an immediate
	// os.Exit would throw it away — that is the pre-existing behaviour this
	// handler was installed to fix (t-1788514086/1788514159/1788514437). But the
	// unwind is not bounded anywhere: measured, `gopi web`'s SIGTERM hang left
	// the process waiting on a lock held across a model round trip until the
	// supervisor's own grace expired and SIGKILLed it (internal/agent/loop.go,
	// chatOnceAttempt). 5 s matches internal/config's existing webShutdownGrace
	// and the operator's own patience with a Ctrl+C; past it the record has had
	// its chance and the process's obligation is to be killable.
	signalExitEscalationGrace = 5 * time.Second
)

// barePromptSignalPlan is the DECISION the signal handler makes, lifted out of
// the goroutine so every arm can be driven by a test without delivering a real
// signal to the test binary (Rule 42: a test signals only what it created, and
// the cheapest honest form of that is not signalling at all).
//
// The two arms are the whole defect: a process with nothing in flight must
// LEAVE, and a process with a run in flight must unwind FIRST and leave anyway
// if the unwind does not finish.
type barePromptSignalPlan int

const (
	// planUnwind — a run is in flight: cancel it, let the end-of-run defer write
	// the session record, and arm the bounded exit in case that unwind parks.
	planUnwind barePromptSignalPlan = iota
	// planExitNow — nothing is in flight: there is no record left to write and
	// no work to protect, so the terminating signal terminates. This is the arm
	// whose absence let pid 2680213 outlive its session by eleven days.
	planExitNow
)

// barePromptSignalPlanFor is that decision, and it is the ONLY place a
// terminating signal's meaning is decided on this face.
func barePromptSignalPlanFor(runInFlight bool) barePromptSignalPlan {
	if runInFlight {
		return planUnwind
	}
	return planExitNow
}

// signalExitStatus maps a caught terminating signal to the process exit status
// this face leaves with (128 + N). A signal Go hands over as something other
// than a syscall.Signal cannot be mapped, and answering 1 would claim "the run
// failed" — which is a different fact — so the caller gets the SIGTERM value,
// the signal every one of these stop paths is actually sent as.
func signalExitStatus(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return signalExitBase + int(s)
	}
	return signalExitBase + int(syscall.SIGTERM)
}

// armSignalExitEscalation starts the bounded exit that guarantees this process
// dies if the run's own unwind does not finish the job.
//
// IT IS ARMED ONLY AFTER Interrupt()'s TARGET EXISTS (an in-flight run), and it
// deliberately does NOT wait for the unwind: the timer runs on its own
// goroutine and the handler returns, so the run loop unwinds in parallel with
// it. Whoever gets there first wins — a clean unwind lets main return normally,
// and the timer is simply never observed because the process is gone.
//
// IT REPORTS ITSELF BEFORE IT FIRES. A process killed by this timer is a run
// whose close-out did not finish inside the grace, which is exactly the kind of
// fact Rule 15 says must not be silent: the line names the signal, the grace
// and the consequence, on stderr, while the process can still write it.
func armSignalExitEscalation(sig os.Signal) {
	status := signalExitStatus(sig)
	go func() {
		time.Sleep(signalExitEscalationGrace)
		fmt.Fprintf(os.Stderr,
			"[gopi] %v received and the run is still unwinding %s later — terminating anyway (the session record may be incomplete)\n",
			sig, signalExitEscalationGrace)
		os.Exit(status)
	}()
}
