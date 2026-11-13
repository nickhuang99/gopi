package main

// signal_owning_modes_regression_test.go — one minimal regression per GAP this
// epic closed, each proving the mode's OWN registration (not the recorder's) now
// answers SIGHUP.
//
// THE SHARED DEFECT (measured, task 01a15bb9 + this epic's audit). signalrec
// registers SIGTERM/SIGINT/SIGHUP for EVERY gopi verb, which REPLACES the kernel
// default death, and then declines to re-raise for every mode in
// modesThatOwnTheirSignalHandlers — because those modes are declared to own the
// handlers themselves. Where the declaration was false (worker, chat before
// 01a15bb9; web, resume, the playback, the worker-control command before this
// epic) the signal was recorded in the provenance ledger and then reached
// nothing: no exit, no drain, no line anywhere.
//
// WHAT EACH ARM BELOW IS FOR. The coverage guard
// (signal_owning_modes_guard_test.go) states the invariant over ALL modes; these
// arms are the per-mode proofs that a NEW mode does not inherit the old silence
// — and, for the two faces that can be driven in-process, that a real SIGHUP
// reaches a real handler.
//
// THE SELF-SIGNAL RULE (PROJECT.md Rule 42, and the measured trap recorded in
// worker_drain_signals_test.go): a test that sends itself a signal the test
// binary has no registration for dies by the DEFAULT disposition — the run
// aborts with "signal: hangup" and every later arm silently never executes,
// which reads like a crash rather than a failed assertion. So every arm that
// raises a real signal registers a safety net FIRST, and only then raises.

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// simRegressTimeout bounds the wait for a handler to record the signal it
	// just received. The handler does its work in signal context and hands off
	// through an atomic, so there is no completion channel to block on without
	// putting a synchronous send inside the production handler this measures.
	simRegressTimeout = 5 * time.Second
	// simRegressPoll is the poll interval of that bounded wait.
	simRegressPoll = 10 * time.Millisecond
)

// TestWorkerControlCommandTakesSIGHUP is web/resume's sibling for the
// `worker --stop` barrier: its set used to be {SIGINT, SIGTERM}, so a hangup
// (a supervisor's terminal going away, the exact case a stop command waits
// through) was recorded by the recorder and acted on by nobody — the command
// stayed parked while the worker it was watching never heard a stop.
//
// It drives the PRODUCTION installer and asserts on the PRODUCTION latch, so it
// measures the real handler rather than a restatement of the set.
func TestWorkerControlCommandTakesSIGHUP(t *testing.T) {
	prevAborted := workerControlAborted.Load()
	prevSignal, _ := workerControlAbortSignal.Load().(string)
	t.Cleanup(func() {
		workerControlAborted.Store(prevAborted)
		workerControlAbortSignal.Store(prevSignal)
	})

	uninstall := installWorkerControlSignals()
	defer uninstall()

	// SAFETY NET FIRST (Rule 42 + the measured trap): without a registration of
	// its own, this test binary would die on the SIGHUP it sends itself, and the
	// assertion below would never run. The net only disables the default
	// disposition for the TEST binary; a notify delivers to EVERY registered
	// channel, so the production handler still receives the signal.
	keepAlive := make(chan os.Signal, 1)
	signal.Notify(keepAlive, syscall.SIGHUP)
	defer signal.Stop(keepAlive)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("could not deliver SIGHUP to this process: %v", err)
	}
	deadline := time.Now().Add(simRegressTimeout)
	for time.Now().Before(deadline) {
		if workerControlAborted.Load() {
			if got := workerControlAbortSignalName(); got != simSIGHUPSpelling {
				t.Errorf("the control command aborted on SIGHUP but recorded the signal as %q, want %q — the termination line would name the wrong cause", got, simSIGHUPSpelling)
			}
			return
		}
		time.Sleep(simRegressPoll) //stylegate:allow-sleep the assertion measures the PRODUCTION signal handler, which hands off through an atomic from signal context; a completion channel would mean putting a synchronous send inside the handler under test
	}
	t.Fatalf("SIGHUP did not abort the `worker --stop` wait within %s — the recorder registers SIGHUP for every verb and declines to re-raise for mode \"worker\", so an unhandled hangup here is recorded and DROPPED (the 01a15bca gap)", simRegressTimeout)
}

// TestResumeFaceInstallsItsOwnTerminatingRegistration is the resume arm. The mode
// had NO signal.Notify at all while the recorder's table declared it
// handler-owning, so every terminating signal was recorded and dropped.
//
// The face cannot be driven in-process (it loads a real checkpoint and runs a
// model), so this arm asserts the SOURCE PROPERTIES that make the gap
// unrepeatable: the registration exists, it takes the recorder's set, and the
// claim follows it — because the claim is what tells the recorder to leave the
// signal alone, and a claim WITHOUT a registration is the original defect
// inverted (the process would be unkillable by SIGTERM).
func TestResumeFaceInstallsItsOwnTerminatingRegistration(t *testing.T) {
	src := simReadSource(t, "cli_resume.go")

	notifyAt := strings.Index(src, "signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)")
	if notifyAt < 0 {
		t.Fatalf("cli_resume.go does not register the recorder's terminating set (want `signal.Notify(resumeSigCh, signalrec.GracefulStopSignals()...)`) — mode \"resume\" is declared handler-owning, so with no registration of its own every SIGTERM/SIGINT/SIGHUP is recorded and then dropped")
	}
	claimAt := strings.Index(src, "signalrec.ClaimSignalHandlers()")
	if claimAt < 0 {
		t.Fatalf("cli_resume.go never declares the claim — the recorder would re-raise with the default disposition and kill the resumed run mid-unwind")
	}
	if claimAt < notifyAt {
		t.Errorf("cli_resume.go claims its handlers %d bytes BEFORE its own registration — the window between the two is the only one the recorder can still win, so the claim must follow signal.Notify", notifyAt-claimAt)
	}

	// The unwind must be REACHABLE: a handler that only sets a flag does nothing
	// while the resumed turn runs synchronously on main. The arm requires the
	// two calls that make it real — the cause declared, then the interrupt.
	if !strings.Contains(src, "ag.CloseSignal = signalrec.SignalName(sig)") {
		t.Errorf("cli_resume.go's handler does not declare ag.CloseSignal — the session_end would file a signalled teardown as `unknown`")
	}
	if !strings.Contains(src, "ag.Interrupt()") {
		t.Errorf("cli_resume.go's handler never calls ag.Interrupt() — a flag-only handler cannot unwind a synchronous turn, so the signal would be received and still do nothing")
	}
}

// TestSessionReplayTakesSIGHUP is the playback arm, and it is the one with a
// terminal-state consequence: without the registration a hangup left the
// operator's shell in RAW MODE inside the ALTERNATE SCREEN, because the restore
// is what the handler exists to run.
//
// The registration moved into internal/replayplay (the package that owns that
// terminal), so the assertion is that the package asks the recorder for the set
// rather than carrying its own list.
func TestSessionReplayTakesSIGHUP(t *testing.T) {
	src := simReadSource(t, simReplayLoopFile)
	if !strings.Contains(src, "signal.Notify(s.sigCh, signalrec.GracefulStopSignals()...)") {
		t.Errorf("%s does not register the recorder's terminating set — a SIGHUP during a playback would leave the terminal in raw mode in the alternate screen with the process still running (the recorder records it and re-raises for nobody)", simReplayLoopFile)
	}
	if !strings.Contains(src, "s.Close()") {
		t.Errorf("%s: the signal arm no longer restores the terminal — the restore IS the reason this handler exists", simReplayLoopFile)
	}
}

// simSIGHUPSpelling is the name signalrec files a hangup under, spelled here so
// the assertion compares spellings rather than a `%v` of a syscall.Signal (whose
// String() is "hangup", NOT "SIGHUP" — a measured trap in the 01a15bb9 work).
const simSIGHUPSpelling = "SIGHUP"

// simReplayLoopFile is the playback's registration site, spelled once.
const simReplayLoopFile = "../../internal/replayplay/loop.go"
