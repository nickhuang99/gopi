// worker_explicit_stop_chain_test.go — the regression guard for the NAMED
// worker face's missing stop forwarder (measured 2026-10-27).
//
// THE DEFECT, as observed on the real binary. `gopi worker <task-id>`
// (runTaskWorkerExplicit) installed the graceful-stop HANDLER
// (installWorkerStopSignals) but never started the goroutine that reads that
// handler's wake-up (startWorkerStopForwarder) — the LOOP face does. The
// handler therefore recorded a stop, wrote its signal record, and nudged a
// channel nobody was reading, while the round sat blocked in cmd.Wait() on its
// run child:
//
//	$ kill -INT <worker-pid>
//	… +70s: worker still state=Sl   (the child was never told)
//	…        row written back `completed` — the operator's stop was discarded
//
// WHY A STRUCTURAL ARM AND NOT A BEHAVIOURAL ONE: runTaskWorkerExplicit ends
// in os.Exit, so an in-process test cannot observe its epilogue; the behavioural
// half of the same contract (a bare signal reaches the in-flight child) is
// already pinned by worker_stop_forward_regression_test.go — that file proves
// the CHAIN works, this one proves the NAMED FACE IS ON IT. The two together
// are the contract: a forwarder that works but that this face never starts is
// exactly the field failure.
//
// The assertion is deliberately about CALL MEMBERSHIP of the stop chain, not
// about the text of a comment: a comment saying "the forwarder runs here" is
// what a reader trusts and what the pre-fix file could have carried.

package main

import (
	"os"
	"strings"
	"testing"
)

const (
	// workerExplicitStopChainSource is the file that owns both runner faces.
	workerExplicitStopChainSource = "cli_tasks_worker.go"
	// The two faces' bodies, by their definition prefixes.
	workerExplicitFacePrefix = "func runTaskWorkerExplicit("
	workerLoopFacePrefix     = "func runTaskWorkerLoop("
	// workerStopForwarderCall is the ONE call that starts the goroutine reading
	// workerStopNudge. It is named here as the unit of parity: whatever else a
	// face does about stops, this is the call without which no stop ever leaves
	// the process.
	workerStopForwarderCall = "startWorkerStopForwarder()"
	// workerStopHandlerCall installs the request RECORDING half. Both faces
	// must have it (they did even before the fix) — asserted so a future edit
	// cannot "simplify" a face down to neither half.
	workerStopHandlerCall = "installWorkerStopSignals()"
)

// workerFaceBody returns the source of one function body, by its definition
// prefix. It reuses the suite's ONE extractor (funcBody) rather than a second
// text slicer.
func workerFaceBody(t *testing.T, src, prefix string) string {
	t.Helper()
	return funcBody(t, src, prefix)
}

// TestWorkerExplicitFaceStartsTheStopForwarder is the regression: the NAMED
// face must start the same stop chain the loop does.
//
// RED on the pre-fix tree (the forwarder call is absent from the named face's
// body while the loop's has it, so BOTH assertions below fail — the absence and
// the parity), GREEN after the fix.
func TestWorkerExplicitFaceStartsTheStopForwarder(t *testing.T) {
	src, err := os.ReadFile(workerExplicitStopChainSource)
	if err != nil {
		t.Fatalf("read %s: %v", workerExplicitStopChainSource, err)
	}
	text := string(src)
	explicit := workerFaceBody(t, text, workerExplicitFacePrefix)
	loop := workerFaceBody(t, text, workerLoopFacePrefix)

	// ① the named face starts the forwarder at all.
	if !strings.Contains(explicit, workerStopForwarderCall) {
		t.Errorf("%s never calls %s — a SIGINT/SIGTERM on `gopi worker <task-id>` then only RECORDS the "+
			"request and nudges a channel nobody reads: the in-flight run child is never told, the round "+
			"runs to completion, and the row is written back `completed` (the measured field defect).\n%s",
			workerExplicitFacePrefix[:len(workerExplicitFacePrefix)-1], workerStopForwarderCall, explicit)
	}
	// ② its teardown is DEFERRED: a forwarder started and never stopped parks a
	// goroutine for the rest of the process's life (the file-watcher leak's
	// shape, worker_file_watcher_lifecycle_test.go). Both spellings count —
	// `defer startWorkerStopForwarder()` and the local-then-defer shape the
	// production face uses (`stopForwarder := startWorkerStopForwarder()` …
	// `defer stopForwarder()`), which the loop's file-watcher comment argues for
	// because the one-paren-short variant silently defers the INSTALL.
	if !forwarderStopIsDeferred(explicit) {
		t.Errorf("the named face starts %s without deferring its stop — a caller that returns (or a "+
			"sibling in-process driver) leaves the forwarder goroutine parked.\n%s", workerStopForwarderCall, explicit)
	}
	// ③ PARITY with the loop face: the face an operator names must not be the
	// one face the stop chain does not reach.
	for _, call := range []string{workerStopHandlerCall, workerStopForwarderCall} {
		if strings.Contains(loop, call) && !strings.Contains(explicit, call) {
			t.Errorf("the LOOP face calls %s but the NAMED face does not — the two faces are one runner "+
				"with one stop contract (Rule 14), and the divergence is what made a named run unstoppable",
				call)
		}
		if !strings.Contains(loop, call) {
			t.Errorf("the LOOP face no longer calls %s — this test's parity baseline is gone; the named "+
				"face's own assertion above stands on its own but the comparison no longer proves anything",
				call)
		}
	}
}

// forwarderStopIsDeferred reports whether the forwarder's stop function is
// DEFERRED in the face's body. Two spellings are accepted because they are the
// same contract and the production face deliberately uses the second:
//
//	defer startWorkerStopForwarder()
//	stopForwarder := startWorkerStopForwarder()   …   defer stopForwarder()
//
// Anything else (a bare call whose returned stop func is dropped) leaves the
// goroutine parked after the caller returns.
func forwarderStopIsDeferred(body string) bool {
	if strings.Contains(body, "defer "+workerStopForwarderCall) {
		return true
	}
	// The local-then-defer shape: find the variable the call is assigned to,
	// then look for its own deferred call.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		const assign = " := " + workerStopForwarderCall
		if i := strings.Index(trimmed, assign); i > 0 {
			name := strings.TrimSpace(trimmed[:i])
			if name == "" || strings.ContainsAny(name, " \t.,(") {
				continue // not a plain `x := call` binding
			}
			if strings.Contains(body, "defer "+name+"()") {
				return true
			}
		}
	}
	return false
}
