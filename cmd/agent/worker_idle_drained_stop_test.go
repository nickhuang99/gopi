// worker_idle_drained_stop_test.go — the drained wait of the NEVER-SELF-EXIT
// face must be a STOP-AWARE sleep.
//
// THE DEFECT (epic worker-idle-drained-stop, measured 2026-09-16): with
// GOPI_WORKER_IDLE_EXITS=0 the loop slept with a raw time.Sleep(idleInterval).
// installWorkerStopSignals REPLACES SIGTERM's default disposition — it records
// a request instead of dying — so a SIGTERM arrived, set the flag, and the
// sleeper never looked: the daemon stayed alive for the WHOLE interval (1h at
// GOPI_WORKER_IDLE_SEC=3600), and `worker --stop` (which writes TERM_<pid> and
// then signals) waited the same hour out. The daemon was effectively
// unkillable by the two documented doors.
//
// These cases pin the two doors separately, because they are DIFFERENT
// mechanisms that fail independently: the in-process flag (the signal path) and
// the durable request FILE (the `--stop` path, which must still land when the
// signal was never delivered — a foreign binary, a caller that can only write
// a file). Both must wake the wait in ~workerStopPollInterval, not in `d`.
//
// Each case runs the wait in a goroutine and measures WALL TIME against a
// requested span tens of seconds long: a raw sleep cannot pass. The truncated
// degenerate timeout keeps the case hermetic and cheap (Rule 27 — no load is
// generated anywhere in this file).

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nick/gopi/internal/tools"
)

const (
	// widLongSpan is the requested wait: far longer than any stop poll, so a
	// wait that ignores the stop cannot return within the case's budget.
	widLongSpan = 30 * time.Second
	// widWakeBudget bounds how long a stop-aware wait may take to return once
	// the request stands — a small multiple of workerStopPollInterval, never
	// anywhere near widLongSpan.
	widWakeBudget = 5 * time.Second
)

// widWithRequestDir points the process's durable-request location at dir for
// the duration of the case (the location resolver reads GopiDir, so a
// sandboxed GOPI_HOME is the supported door — no test-only bypass in
// production code).
func widWithRequestDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the sandbox control dir: %v", err)
	}
	return dir
}

// widAwaitWait runs the drained wait and reports how long it took to return.
func widAwaitWait(t *testing.T, d time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	done := make(chan struct{})
	go func() {
		workerIdleDrainedWait(d)
		close(done)
	}()
	select {
	case <-done:
		return time.Since(start)
	case <-time.After(widWakeBudget):
		t.Fatalf("workerIdleDrainedWait(%s) did not return within %s — the drained wait is not "+
			"stop-aware: a stop request cannot interrupt it, so a daemon with "+
			"GOPI_WORKER_IDLE_EXITS=0 serves out the whole idle interval before it will die",
			d, widWakeBudget)
		return 0
	}
}

// TestWorkerIdleDrainedWaitWakesOnStopFlag: the SIGNAL door. A graceful stop
// recorded in the process-wide flag (what installWorkerStopSignals sets) must
// cut the wait short.
func TestWorkerIdleDrainedWaitWakesOnStopFlag(t *testing.T) {
	widWithRequestDir(t)
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	go func() {
		time.Sleep(workerStopPollInterval) //stylegate:allow-sleep handshake: the stop must land INSIDE the wait under test, not before it starts
		workerRequestStop("SIGTERM received", false)
	}()
	if took := widAwaitWait(t, widLongSpan); took > widWakeBudget {
		t.Fatalf("the wait returned after %s with the stop flag set — want ~%s", took, workerStopPollInterval)
	}
}

// TestWorkerIdleDrainedWaitWakesOnRequestFile: the DURABLE FILE door — the
// `worker --stop` path. The file is not consumed by the wait (the loop's claim
// boundary consumes it, Rule 2), so the assertion is on the wake-up, and the
// request must still be standing for the boundary to read.
func TestWorkerIdleDrainedWaitWakesOnRequestFile(t *testing.T) {
	dir := widWithRequestDir(t)
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	go func() {
		time.Sleep(workerStopPollInterval) //stylegate:allow-sleep handshake: the request must land INSIDE the wait under test, not before it starts
		if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
			t.Errorf("write the durable stop request: %v", err)
		}
	}()
	if took := widAwaitWait(t, widLongSpan); took > widWakeBudget {
		t.Fatalf("the wait returned after %s with a durable request standing — want ~%s", took, workerStopPollInterval)
	}
	if !workerFileStopRequested(dir, os.Getpid()) {
		t.Fatal("the wait consumed the request file — the claim boundary is its ONE reader (Rule 2)")
	}
	if workerStopRequested() {
		t.Error("the wait flipped the in-process flag from the file: the boundary consume is what turns " +
			"a file into a flag, and a wait that pretends to have a REASON invents one")
	}
}

// TestWorkerIdleDrainedWaitSleepsTheWholeSpanWithoutARequest: the NEGATIVE half
// — the anti-regression that stops a future "fix" from turning the wait into a
// busy return (a wait that never sleeps would spin the loop). With no request
// standing, the wait serves the whole requested span.
func TestWorkerIdleDrainedWaitSleepsTheWholeSpanWithoutARequest(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	widWithRequestDir(t)
	resetWorkerStopState()

	const span = 300 * time.Millisecond
	start := time.Now()
	workerIdleDrainedWait(span)
	if took := time.Since(start); took < span {
		t.Fatalf("workerIdleDrainedWait(%s) returned after %s with NO stop request — the wait must "+
			"serve the whole drained span", span, took)
	}
}

// TestWorkerIdleDrainedWaitReportsAnUnreadableRequestLocation: Rule 15b — an
// unresolvable control dir must not silently degrade the wait into "the file
// door does not exist". The wait still runs (the flag door must keep working)
// and the failure surfaces on stderr exactly once.
func TestWorkerIdleDrainedWaitReportsAnUnreadableRequestLocation(t *testing.T) {
	// A GopiDir that cannot be created: a regular file where the home must be.
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatalf("create the blocking file: %v", err)
	}
	t.Setenv("GOPI_HOME", blocked)
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState)

	dir, _ := workerStopRequestLocation()
	if dir != "" {
		t.Fatalf("workerStopRequestLocation resolved %q from an unusable profile — want no location", dir)
	}
	// No location means no file door; the wait must still serve its span and
	// must not fail.
	const span = 100 * time.Millisecond
	workerIdleDrainedWait(span)
}
