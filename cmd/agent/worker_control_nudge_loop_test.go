// worker_control_nudge_loop_test.go — the regression guard for the DRAINING
// worker's hot loop and its control-file ENOENT storm (measured 2026-10-15,
// host host-a, bug: `gopi worker --stop --pid <pid>` never returns).
//
// TWO DEFECTS, ONE CHAIN, and this file pins both halves.
//
//  1. THE SELF-RE-ARMING NUDGE. consumeDrainRequests is called from three
//     places, and one of them is the forwarder ITSELF (its first act —
//     forwardWorkerStopDuringRound). With the wake-up (`notifyWorkerStopNudge`)
//     sent unconditionally on every consume, the three formed a cycle with no
//     blocking point: the forwarder consumes → the consume nudges → the
//     forwarder's select receives its own nudge immediately → it consumes again
//     → it nudges again. Measured on a real draining daemon: 514 ticks of CPU
//     in 5 seconds — 100% of a core — for a worker whose only job at that
//     moment was to sit still and wait for the in-flight round to end.
//
//  2. THE STATE-FILE ENOENT STORM, which the hot loop made visible. Every round
//     of that loop re-published the worker's state record, and
//     tools.WriteControlText wrote its in-flight bytes to ONE fixed tmp name
//     (`<path>.tmp`) shared by every writer of that file. The watcher goroutine
//     and the loop goroutine flush the SAME record, so two overlapping flushes
//     both wrote the one tmp and the second rename(2) found it already consumed:
//
//     gopi worker: install control file .../worker-state-<pid>: rename
//     .../worker-state-<pid>.tmp .../worker-state-<pid>: no such file or directory
//
//     Measured: 93 such lines in 3m49s on one profile, 51 in another, and the
//     unit arm of the second test below lost 5-6 of 8 concurrent writers per
//     run before the fix.
//
// WHY THIS IS NOT COSMETIC: the state record is the ONLY answer to "is a worker
// running, and what is it doing?" — `--status` reads it, and `--stop`'s
// in-flight-round wait reads it (workerWaitForExit → workerReportStillBusy). A
// lost flush is a record one boundary behind, and the log storm buries the
// lines an operator needs. The stop's own hang is separate: a drain legitimately
// waits for the in-flight round (awaitWorkerDrainChild has no deadline by
// design), but a worker burning a core to re-discover a fact it already
// recorded is not waiting for work — it is spinning, and it starves the very
// round it claims to be waiting on.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// nudgeLoopQuietWindow is how long the drain must stay quiet for the assertion
// to pass. It is generous next to the defect's own cadence (the pre-fix loop
// produced a nudge every few microseconds, i.e. thousands inside this window)
// and far too short for a legitimate consume — a drain is consumed ONCE per
// boundary (2s ticker at the fastest), and a re-consume of a standing request
// writes no new fact.
const nudgeLoopQuietWindow = 1500 * time.Millisecond

// nudgeLoopStateWrites counts how many DISTINCT state-file installations a
// drain produces inside the window. The measurement is taken from the FILESYSTEM
// rather than from a counter inside the code under test: what the defect
// corrupts is the record on disk, so the record on disk is what is read.
func nudgeLoopStateWrites(t *testing.T, dir string, window time.Duration) int {
	t.Helper()
	path := tools.ControlStatePath(dir, workerStateFilePrefix, os.Getpid())
	start := statMtimeNanos(t, path)
	deadline := time.Now().Add(window)
	writes := 0
	for time.Now().Before(deadline) {
		if now := statMtimeNanos(t, path); now != start {
			writes++
			start = now
		}
		time.Sleep(nudgeLoopPollStep) //stylegate:allow-sleep the poll STEP of the window above — the deadline is the bound, this is the sampling rate
	}
	return writes
}

// statMtimeNanos reads a file's modification stamp, or -1 when it is missing.
// A missing record is NOT a zero — it is a lost record, and the caller must be
// able to tell the two apart (Rule 15: never a silent zero).
func statMtimeNanos(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.ModTime().UnixNano()
}

// nudgeLoopPollStep is the sampling cadence. It is far finer than the pre-fix
// loop's own period, so the count it produces under the defect is a LOWER
// bound, not a guess at the true rate.
const nudgeLoopPollStep = 2 * time.Millisecond

// TestWorkerDrainNudgeDoesNotReArmItself is the RED-when-broken half: a standing
// durable request, consumed through the forwarder (the exact path the pre-fix
// cycle ran on), must settle — the forwarder consumes once, forwards nothing
// (this is a DRAIN: the in-flight round is allowed to finish), and then BLOCKS.
//
// The pre-fix code re-armed itself on every consume, so the state record was
// rewritten continuously and this window saw hundreds of installations. The
// assertion is deliberately about SETTLING rather than about a rate: any bounded
// number would let a slower re-arm through, while "the record stopped changing"
// is the property that was actually lost.
func TestWorkerDrainNudgeDoesNotReArmItself(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	workerCtx = drainTestCtx(t, dir)
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	stopForwarder := startWorkerStopForwarder()
	defer stopForwarder()

	// The nudge a durable request delivers (the watcher's tick does exactly this).
	notifyWorkerStopNudge()
	// Let the transition settle before the window opens, so what is measured is
	// the STEADY state of a draining worker — not the one legitimate consume.
	time.Sleep(workerStopDrainSettle + nudgeLoopQuietWindow) //stylegate:allow-sleep lets a delivered nudge settle BEFORE the quiet window opens, so what is measured is the steady state

	writes := nudgeLoopStateWrites(t, dir, nudgeLoopQuietWindow)
	if writes != 0 {
		t.Fatalf("a draining worker rewrote its state record %d time(s) in %s with no new fact to publish — "+
			"the wake-up re-arms itself (consumeDrainRequests nudges, the forwarder consumes, and the cycle has no "+
			"blocking point). Pre-fix measured: 100%% of a core, 93 rename failures in 3m49s.", writes, nudgeLoopQuietWindow)
	}
	if !workerDrainRequestedByFile() {
		t.Error("the durable request was never recognised as a DRAIN")
	}
	if workerStopEscalated() {
		t.Error("a durable drain escalated the stop — escalation is the force spelling only")
	}
}

// TestControlTextWritersDoNotShareOneTmp is the second half: N writers to the
// SAME control path must all succeed. A fixed tmp name makes the second
// rename(2) fail with ENOENT (the name is already consumed) while the FIRST
// writer's bytes are what lands — i.e. the record silently loses a flush it was
// told had been installed.
//
// The concurrent writers are not synthetic: they are the shape the worker
// actually has (a loop goroutine and a request-watcher goroutine flushing one
// state record), and the defect was measured on a live daemon before this test
// existed.
func TestControlTextWritersDoNotShareOneTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker-state-probe")

	const writers = 8
	errs := make(chan error, writers)
	start := make(chan struct{})
	for i := 0; i < writers; i++ {
		go func() {
			<-start // release all writers at once, so their windows overlap
			errs <- tools.WriteControlText(path, []byte("record"))
		}()
	}
	close(start)
	var failed []string
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		t.Fatalf("%d/%d concurrent writers to one control file failed — they share ONE tmp name, so each rename(2) "+
			"after the first finds it already consumed:\n%s",
			len(failed), writers, strings.Join(failed, "\n"))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no record was installed at all: %v", err)
	}
	if string(body) != "record" {
		t.Fatalf("installed record is %q, want %q", body, "record")
	}
	// No in-flight tmp may be left behind: the point of the suffix is that a
	// reader ignores it, not that it accumulates.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(path) {
			t.Errorf("writer left residue behind: %s", e.Name())
		}
	}
}
