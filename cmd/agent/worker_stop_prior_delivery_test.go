// worker_stop_prior_delivery_test.go — the SECOND `gopi worker --stop` must not
// wait for a receipt the protocol can never produce twice.
//
// THE BUG THIS PINS (measured 2026-10-29, this task's briefing): the first
// `--stop` writes the durable request TERM_<pid> and the worker writes its ONE
// receipt ACK_<pid> when it consumes the request; the sender that reads that
// receipt ARCHIVES it (ACK_<pid>.<req_id>.consumed). A second `--stop`
// therefore finds the request still standing, no live receipt to read — and used
// to sit out the whole --wait bound (10m by default) waiting for a reply that
// cannot be produced a second time, printing
//
//	FOREGROUND pid N is already asked to leave (stop requested) — nothing to do
//	pid N has written no receipt yet at ... (5.014s into a 10m0s bound); ...
//
// for ten minutes per worker.
//
// THE TWO HALVES, one assertion each:
//
//	① A PRIOR DELIVERY WITH ITS RECEIPT ON RECORD: the invocation returns at
//	   once, states that the request was already delivered, names the recorded
//	   acknowledgement, and says no further receipt is awaited. Nothing is
//	   rewritten and nothing is signalled (the target is still alive).
//	② A PRIOR DELIVERY WITH NO RECEIPT ANYWHERE: equally prompt, but NOT
//	   reported as stopped — the request stands and this invocation has not
//	   observed the worker acting on it (the safety behaviour that must not be
//	   softened by the fix).
//
// THE RED PROOF: on the pre-fix tree both cases block for the whole bound
// (wfpdBound, 60s here), so ① fails on its elapsed check long before its output
// checks — the assertion is a MEASUREMENT, not a wording match.
//
// Rule 27: no load is generated. The targets are `sleep` children this test owns
// and reaps; the assertions are about files, pids and one elapsed duration.
package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// wfpdBound is the --wait bound the second invocation is given. It is far
	// LONGER than wfpdPromptMax on purpose: the assertion is "the command did not
	// pay the bound", so a bound short enough to be paid quickly would prove
	// nothing.
	wfpdBound = 60 * time.Second
	// wfpdPromptMax is what "returns immediately with the state it already knows"
	// means as a measurement: comfortably under the bound, far above the cost of
	// a stat and a ReadDir.
	wfpdPromptMax = 1 * time.Second
	// wfpdAckSenderPID is the pid named in the receipt this test SEEDS — a sender
	// that is not this test process, i.e. the earlier invocation the line reports.
	wfpdAckSenderPID = 999999
	// wfpdAliveWait bounds the check that a second invocation did NOT signal the
	// target (a load-tolerant window, not a wait for anything to happen).
	wfpdAliveWait = 300 * time.Millisecond
)

// wfpdFixture plants the two-pid procfs shape: a FOREIGN-binary target (so the
// ask is file-only and no signal can land on the test's own child) and this
// process, under a temp profile so the control dir is the test's.
func wfpdFixture(t *testing.T, root, home string, target int) {
	t.Helper()
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))
}

// TestWorkerStopSecondInvocationDoesNotAwaitASecondReceipt is ①: a standing
// request whose receipt is already on record returns the known state at once.
func TestWorkerStopSecondInvocationDoesNotAwaitASecondReceipt(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	root, home := t.TempDir(), t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wfpdFixture(t, root, home, target)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the control dir: %v", err)
	}
	// THE PRIOR INVOCATION's footprint: the request it wrote, and the receipt the
	// worker answered it with (already archived by the sender that read it — the
	// archive is what a second invocation actually meets).
	if err := workerRequestStopFile(dir, target); err != nil {
		t.Fatalf("seed the standing request: %v", err)
	}
	sender := workerStopSender{PID: wfpdAckSenderPID, TS: time.Now().Format(time.RFC3339)}
	if err := workerWriteStopAck(dir, target, workerAckModeDrain, 1, sender); err != nil {
		t.Fatalf("seed the receipt the earlier sender read: %v", err)
	}
	if _, err := archiveWorkerAck(workerAck{Path: workerAckPathText(dir, target), ReqID: wfpdAckSenderPID,
		AckedBy: target, AckedAt: time.Now().Format(time.RFC3339), Mode: workerAckModeDrain}); err != nil {
		t.Fatalf("archive the seeded receipt the way a sender does: %v", err)
	}
	requestPath := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
	before, err := os.Stat(requestPath)
	if err != nil {
		t.Fatalf("stat the seeded request: %v", err)
	}

	var ok bool
	start := time.Now()
	out := captureStdoutAndStderr(t, func() int {
		ok = workerControlStopForeground([]int{target}, wfpdBound, false)
		return 0
	})
	elapsed := time.Since(start)

	// ① THE MEASUREMENT: no bound was paid. On the pre-fix tree this is where the
	// case fails — after wfpdBound, not after a millisecond.
	if elapsed > wfpdPromptMax {
		t.Errorf("a second --stop took %s (bound %s): an already-delivered request must not be waited on again\n%s",
			elapsed.Round(time.Millisecond), wfpdBound, out)
	}
	// ② THE STATE: the request was already delivered, the receipt is on record,
	// and no further receipt is awaited — all three, by name.
	for _, want := range []string{
		"already asked to leave",
		"EARLIER invocation",
		"1 acknowledgement(s) are on record",
		"no further receipt is awaited",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report must state %q:\n%s", want, out)
		}
	}
	// ③ THE REQUEST IS UNTOUCHED: a repeat is a reported no-op, not a rewrite.
	after, err := os.Stat(requestPath)
	if err != nil {
		t.Fatalf("stat the request after the repeat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("the standing request was REWRITTEN (%s → %s); a repeat must write nothing",
			before.ModTime(), after.ModTime())
	}
	// ④ NOTHING WAS SIGNALLED and the end state is the one already known.
	if !ok {
		t.Errorf("the recorded acknowledgement IS the end state — the call must report it as reached:\n%s", out)
	}
	time.Sleep(wfpdAliveWait) //stylegate:allow-sleep bounded check that the repeat left the target alone
	if !tools.PidAlive(target) {
		t.Fatalf("pid %d was signalled by a REPEAT of a delivery already made:\n%s", target, out)
	}
}

// TestWorkerStopDaemonFaceDoesNotAwaitASecondReceipt is the SAME defect on the
// DAEMON face (workerControlStopOne): it printed "already stop requested —
// nothing to do" and then fell through to the claim-loop settle and the exit
// wait, paying the bound for a receipt the earlier sender already consumed.
func TestWorkerStopDaemonFaceDoesNotAwaitASecondReceipt(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_test.go)
	root, home := t.TempDir(), t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wfpdFixture(t, root, home, target)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the control dir: %v", err)
	}
	// The daemon shape: the request stands, and the receipt the earlier sender
	// read is archived — exactly what a live daemon leaves behind.
	if err := workerRequestStopFile(dir, target); err != nil {
		t.Fatalf("seed the standing request: %v", err)
	}
	sender := workerStopSender{PID: wfpdAckSenderPID, TS: time.Now().Format(time.RFC3339)}
	if err := workerWriteStopAck(dir, target, workerAckModeDrain, 1, sender); err != nil {
		t.Fatalf("seed the receipt the earlier sender read: %v", err)
	}
	if _, err := archiveWorkerAck(workerAck{Path: workerAckPathText(dir, target), ReqID: wfpdAckSenderPID,
		AckedBy: target, AckedAt: time.Now().Format(time.RFC3339), Mode: workerAckModeDrain}); err != nil {
		t.Fatalf("archive the seeded receipt: %v", err)
	}
	st := workerStateFile{
		PID:    target,
		Path:   filepath.Join(dir, workerStateDirName+"-"+strconv.Itoa(target)),
		Parsed: true,
		State:  workerControlState{PID: target},
	}

	var ok bool
	start := time.Now()
	out := captureStdoutAndStderr(t, func() int {
		ok = workerControlStopOne(st, wfpdBound, false)
		return 0
	})
	elapsed := time.Since(start)

	if elapsed > wfpdPromptMax {
		t.Errorf("the daemon face took %s (bound %s) for an already-delivered request\n%s",
			elapsed.Round(time.Millisecond), wfpdBound, out)
	}
	if !ok {
		t.Errorf("the recorded acknowledgement IS the end state — the daemon face must report it as reached:\n%s", out)
	}
	for _, want := range []string{"already " + workerDrainVerb, "EARLIER invocation", "no further receipt is awaited"} {
		if !strings.Contains(out, want) {
			t.Errorf("the daemon face must state %q:\n%s", want, out)
		}
	}
}

// TestWorkerStopPriorDeliveryWithoutAckIsNotReportedAsStopped is ②: prompt, but
// honest — a request that was delivered and never acknowledged is NOT "stopped".
func TestWorkerStopPriorDeliveryWithoutAckIsNotReportedAsStopped(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	root, home := t.TempDir(), t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wfpdFixture(t, root, home, target)
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve the control dir: %v", err)
	}
	if err := workerRequestStopFile(dir, target); err != nil {
		t.Fatalf("seed the standing request: %v", err)
	}

	var ok bool
	start := time.Now()
	out := captureStdoutAndStderr(t, func() int {
		ok = workerControlStopForeground([]int{target}, wfpdBound, false)
		return 0
	})
	elapsed := time.Since(start)

	if elapsed > wfpdPromptMax {
		t.Errorf("a second --stop took %s (bound %s) waiting for a receipt that cannot be written twice\n%s",
			elapsed.Round(time.Millisecond), wfpdBound, out)
	}
	if ok {
		t.Errorf("a delivery with no acknowledgement anywhere must NOT be reported as stopped:\n%s", out)
	}
	for _, want := range []string{
		"no acknowledgement is on record",
		"NOT reported as stopped",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report must state %q:\n%s", want, out)
		}
	}
}
