package main

// worker_drain_signals_test.go — a terminal session teardown must DRAIN a
// foreground worker (and close a chat out), never silently discard the hangup.
//
// THE MEASURED DEFECT (task 01a15bb9, 2026-10-21). A foreground `gopi worker`
// was sent SIGHUP — what a pty teardown / session hangup delivers:
//
//	$ ps -o pid,ppid,pgid,sid,args -p 3353279
//	    PID    PPID    PGID     SID COMMAND
//	3353279   54349 3353278   57142 …/gopi-baseline worker
//	$ kill -HUP 3353279 ; sleep 2 ; ps -p 3353279
//	STILL ALIVE (pid 3353279)
//
// and the worker's own lifecycle log said NOTHING about it, while
// internal/signalrec HAD recorded the delivery:
//
//	{"kind":"signal","signal":"SIGHUP","role":"worker",…,"sourced":false}
//
// The same worker, sent SIGINT, DID drain ("worker: interrupt received —
// draining"). So the hangup was recorded by the provenance recorder and then
// dropped on the floor: a silent drop, which Rule 15 calls a failure and not a
// policy.
//
// WHY IT LANDED THERE. signalrec's `watchedSignals` registers SIGTERM, SIGINT
// and SIGHUP for EVERY gopi verb, which REPLACES the default disposition — so
// SIGHUP no longer kills anything. signalrec then declines to re-raise for a
// mode listed in `modesThatOwnTheirSignalHandlers` ("worker", "chat", …), on
// the premise that those modes installed their own handler. That premise was
// false for SIGHUP: the worker's own set was {SIGTERM, SIGINT, SIGUSR1} and
// chat's was {SIGINT, SIGTERM}. The signal fell in the gap between "the
// recorder will not kill me" and "my handler does not know me" — and the gap is
// silent by construction.
//
// WHAT THESE ARMS PIN:
//  1. the worker's drain set CONTAINS SIGHUP (the fix, on the constant), and
//  2. a real SIGHUP delivered to the installed handler REQUESTS A STOP with the
//     signal named — the observable drain, not just the membership;
//  3. the same for chat, on the set it registers.
//
// The registry entries in signalrec's map are NOT changed by this fix: the
// claim "this mode owns its handlers" becomes TRUE for the hangup, which is the
// point — one writer of the disposition, and the mode's own handler is it.

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// The poll budget for a REAL signal round-trip. Signals are asynchronous, but
// the handler only sets a flag under a mutex, so this is generous by orders of
// magnitude; it exists to fail fast instead of hanging the suite.
const (
	workerDrainSignalTestTimeout = 5 * time.Second
	workerDrainSignalTestPoll    = 10 * time.Millisecond
	// workerDrainSignalName is the spelling the stop record must carry for a
	// hangup. It is the NAME the provenance layer prints (signalrec.SignalName),
	// not a second vocabulary invented by the test.
	workerDrainSignalName = "SIGHUP"
)

// TestWorkerStopSignalsIncludeSIGHUP fails until a session hangup is part of
// the worker's graceful-stop set. Membership is checked by the signal VALUE the
// kernel delivers, not by a spelling (Rule 1: no bare string keywords).
func TestWorkerStopSignalsIncludeSIGHUP(t *testing.T) {
	for _, want := range []os.Signal{syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM} {
		found := false
		for _, got := range workerStopSignals {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("workerStopSignals lacks signal %d — a terminal session teardown sends SIGHUP, and a worker "+
				"that does not handle it has the signal RECORDED by signalrec and then silently dropped "+
				"(measured: task 01a15bb9, worker stayed alive and kept claiming)", int(sigNoForDrainTest(want)))
		}
	}
}

// TestWorkerDrainsOnSIGHUP drives the REAL installed handler through a REAL
// signal: it is the behaviour the set exists for. A membership assertion alone
// would pass on a set the handler never reads (Rule 31: a surface you did not
// execute is not delivered).
func TestWorkerDrainsOnSIGHUP(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)
	uninstall := installWorkerStopSignals()
	defer uninstall()

	// SAFETY NET, and it is part of the MEASUREMENT: without a registration of
	// its own, this test process would die on the SIGHUP it sends itself —
	// measured in the first red run ("signal: hangup", the run aborted with the
	// other two arms never executing). Registering the hangup here only DISABLES
	// the default disposition for the test binary; it does not touch the
	// production handler, so the assertion below still measures whether
	// installWorkerStopSignals asked for a stop. A notify delivers to EVERY
	// registered channel, so this cannot mask the production handler.
	keepAlive := make(chan os.Signal, 1)
	signal.Notify(keepAlive, syscall.SIGHUP)
	defer signal.Stop(keepAlive)

	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatalf("could not deliver SIGHUP to this process: %v", err)
	}
	deadline := time.Now().Add(workerDrainSignalTestTimeout)
	for time.Now().Before(deadline) {
		if workerStopRequested() {
			if got := workerStopSignalSpelling(); got != workerDrainSignalName {
				t.Errorf("stop requested by SIGHUP, but the recorded signal spelling is %q, want %q", got, workerDrainSignalName)
			}
			return
		}
		time.Sleep(workerDrainSignalTestPoll) //stylegate:allow-sleep a poll loop IS the bounded wait here — the assertion tests the production signal handler, which records its stop request from signal context, so there is no completion channel a test could block on without putting a synchronous send inside the production handler it measures
	}
	t.Fatalf("SIGHUP was NOT turned into a stop request within %s — the worker would keep claiming work while "+
		"its terminal session is gone (the measured 01a15bb9 defect)", workerDrainSignalTestTimeout)
}

// TestChatSignalSetIncludesSIGHUP is chat's half of the same gap: the set chat
// registers must carry the hangup, so an open session drains instead of being
// abandoned with its CHATS row left in_progress.
func TestChatSignalSetIncludesSIGHUP(t *testing.T) {
	found := false
	for _, got := range chatGracefulSignals() {
		if got == syscall.SIGHUP {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("chat's registered signal set lacks SIGHUP — a chat whose terminal goes away must close its " +
			"session out, not lose it (measured: signalrec records the SIGHUP, chat never sees it)")
	}
}

// sigNoForDrainTest renders a signal's number for an assertion message without
// depending on a name table (the value IS what the kernel delivers).
func sigNoForDrainTest(sig os.Signal) syscall.Signal {
	if s, ok := sig.(syscall.Signal); ok {
		return s
	}
	return 0
}
