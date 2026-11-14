package main

// worker_drain_unit_test.go — the unit half of the graceful-drain contract
// (epic worker-graceful-drain, 2026-09-13): the durable request file is
// consumed at the CLAIM BOUNDARY, the consume is IDEMPOTENT, escalation comes
// only from the FORCE spelling, and the state file carries the binary version
// a restart is verified against.
//
// WHY THESE ARE UNIT TESTS (and what they do NOT cover): the E2E half (a real
// daemon, a real long round, a real `--restart`) is
// worker_drain_restart_e2e_test.go. Here the ledger is sandboxed and the
// process is this test binary, so the request/consume/idempotency paths are
// asserted exactly and cheaply.

import (
	"context"
	"encoding/json"
	"github.com/nickhuang99/gopi/internal/config"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/tools"
)

// drainTestDir is the sandbox control directory (one per test, never the real
// profile's): the request and state files live here and nowhere else.
func drainTestDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// drainTestCtx builds a daemon control context for THIS process in dir, with a
// real state writer (so the drain flag it publishes can be read back).
func drainTestCtx(t *testing.T, dir string) workerDaemonCtx {
	t.Helper()
	return workerDaemonCtx{dir: dir, pid: os.Getpid(), writer: newWorkerStateWriter(dir, os.Getpid(), "unit test")}
}

// drainTestState reads back the state file the context published.
func drainTestState(t *testing.T, dir string) workerControlState {
	t.Helper()
	b, err := os.ReadFile(tools.ControlStatePath(dir, workerStateFilePrefix, os.Getpid()))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	var st workerControlState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatalf("parse state file %s: %v", b, err)
	}
	return st
}

// resetWorkerStopState clears the process-wide stop record between tests (the
// record is deliberately module-level: it is the ONE flag every exit path
// reads, and the tests must start from a known state rather than from whatever
// the previous test left behind). The provenance fields are cleared with it —
// a leaked SENDER/TASK from a sibling test would make a termination line look
// attributable without any request having been made, which is exactly the
// class of false record this work exists to prevent.
func resetWorkerStopState() {
	workerStopState.mu.Lock()
	workerStopState.stop = false
	workerStopState.force = false
	workerStopState.reason = ""
	workerStopState.drain = false
	workerStopState.hangup = false
	workerStopState.signal = ""
	workerStopState.sender = workerStopSender{}
	workerStopState.task = ""
	workerStopState.mu.Unlock()
}

// TestWorkerDrainConsumesRequestAtBoundary: the claim-boundary consume turns a
// durable request FILE (no signal involved) into the stop flag, and publishes
// the drain in the state file so an operator asking again is told the truth.
func TestWorkerDrainConsumesRequestAtBoundary(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	ctx := drainTestCtx(t, dir)

	// Before the request: nothing is pending and the consume is a no-op.
	ctx.consumeDrainRequests()
	if workerStopRequested() {
		t.Fatal("a stop was reported with no request standing")
	}

	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	ctx.consumeDrainRequests()
	if !workerStopRequested() {
		t.Fatal("the durable request file was not consumed at the claim boundary")
	}
	if workerStopEscalated() {
		t.Error("a graceful request escalated the stop — it must never interrupt the in-flight round")
	}
	if got := workerStopReason(); got != workerStopReasonForRequest(dir, os.Getpid()) {
		t.Errorf("recorded reason = %q, want %q", got, workerStopReasonForRequest(dir, os.Getpid()))
	}
	st := drainTestState(t, dir)
	if !st.Draining {
		t.Error("the state file does not report the drain — a repeated --stop cannot be answered honestly without it")
	}
	if st.DrainingSince == "" {
		t.Error("the drain was recorded with no timestamp")
	}
}

// TestWorkerDrainConsumeIsIdempotent: consuming the same request again (and a
// second identical request FILE) changes nothing — one flag, one drain
// timestamp. This is what makes a repeated `--stop` an honest no-op instead of
// a re-signal.
func TestWorkerDrainConsumeIsIdempotent(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	ctx := drainTestCtx(t, dir)
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	ctx.consumeDrainRequests()
	first := drainTestState(t, dir).DrainingSince

	// A SECOND request (the repeated `--stop`) re-writes the same file, and the
	// consume runs again: neither may restart the clock.
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("re-write stop request: %v", err)
	}
	ctx.consumeDrainRequests()
	if second := drainTestState(t, dir).DrainingSince; second != first {
		t.Errorf("the drain timestamp was restarted (%q → %q) — the age of the drain is the honest answer to a repeated request", first, second)
	}
	if !workerStopRequested() || workerStopEscalated() {
		t.Errorf("stop=%v force=%v after a repeated request, want stop=true force=false",
			workerStopRequested(), workerStopEscalated())
	}
}

// TestWorkerDrainRequestDetectedForPid: the CLI's idempotency decision is made
// from the request directory (not from a re-signal), so the predicate it uses
// must find a standing request for a LIVE pid and never claim one for a pid
// nobody is waiting on.
func TestWorkerDrainRequestDetectedForPid(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	if workerDrainRequestedFor(dir, os.Getpid()) {
		t.Fatal("a request was reported before one was written")
	}
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	if !workerDrainRequestedFor(dir, os.Getpid()) {
		t.Error("a standing request for a live pid was not detected — a repeated --stop would re-signal")
	}
	// A request for a pid that does not exist is a dead record, not a pending
	// drain (LiveStopRequestPIDs filters on liveness).
	dead := tools.ControlRequestPath(dir, workerStopRequestPrefix, 0)
	if _, err := os.Stat(dead); err == nil {
		t.Fatalf("the pid-0 request path exists (%s) — the encoding is not what this test assumes", dead)
	}
}

// TestWorkerForceSpellingAloneEscalates is the DIRECT regression guard for the
// escalation defect: a REPEATED graceful signal must not be read as force,
// while the force spelling (SIGUSR1) always is.
func TestWorkerForceSpellingAloneEscalates(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	graceful := []os.Signal{syscall.SIGTERM, syscall.SIGINT}
	for _, sig := range graceful {
		if workerForceSignalRequested(sig) {
			t.Fatalf("%v is not the force spelling", sig)
		}
		// Three identical requests in a row: the old rule ("!first ⇒ force")
		// would have escalated the third — and SIGKILLed an in-flight round.
		for i := 0; i < 3; i++ {
			workerRequestStop("repeated graceful signal", workerForceSignalRequested(sig))
		}
		if workerStopEscalated() {
			t.Fatalf("a repeated %v escalated the stop — a duplicate request must never interrupt the round", sig)
		}
	}
	if !workerForceSignalRequested(syscall.SIGUSR1) {
		t.Fatal("SIGUSR1 (the force spelling) does not escalate")
	}
	workerRequestStop("force signal", workerForceSignalRequested(syscall.SIGUSR1))
	if !workerStopEscalated() {
		t.Error("the force spelling did not escalate")
	}
}

// TestWorkerForceRequestFileEscalates: the durable FORCE request escalates too
// (a caller that can only write a file, or a force that raced the signal).
func TestWorkerForceRequestFileEscalates(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	ctx := drainTestCtx(t, dir)
	if err := workerRequestForceFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write force request: %v", err)
	}
	ctx.consumeDrainRequests()
	if !workerStopRequested() {
		t.Error("the force request did not stop the worker at the boundary")
	}
	if !workerStopEscalated() {
		t.Error("the force request did not escalate")
	}
	if !drainTestState(t, dir).Draining {
		t.Error("a forced leave is also a drain (nothing new is claimed)")
	}
}

// TestWorkerStateFileCarriesBinaryVersion: the version is what makes a restart
// VERIFIABLE (the new daemon must be distinguishable from the old one by a
// fact, not by a pid). It is stamped at construction and reported by the state
// renderer.
func TestWorkerStateFileCarriesBinaryVersion(t *testing.T) {
	dir := drainTestDir(t)
	newWorkerStateWriter(dir, os.Getpid(), "version test")
	st := drainTestState(t, dir)
	if st.Version != buildVersion() {
		t.Errorf("state version = %q, want the running binary's %q", st.Version, buildVersion())
	}
	if !contains(st.Render(), "binary "+buildVersion()) {
		t.Errorf("--status render does not name the binary: %q", st.Render())
	}
}

// TestWorkerDrainRenderNamesTheDrain: the operator-facing render must say
// "draining" (the word the idempotency contract is asserted on) and must not
// invent an age for a record that carries none.
func TestWorkerDrainRenderNamesTheDrain(t *testing.T) {
	withAge := workerControlState{PID: 7, Draining: true, DrainingSince: "2026-09-13T00:00:00Z"}
	if !contains(withAge.Render(), workerDrainVerb) {
		t.Errorf("render %q does not use the drain spelling %q", withAge.Render(), workerDrainVerb)
	}
	withoutAge := workerControlState{PID: 7, Draining: true}
	if !contains(withoutAge.Render(), workerDrainUnknownAge) {
		t.Errorf("render %q does not name the unknown age (%q)", withoutAge.Render(), workerDrainUnknownAge)
	}
}

// drainSeedPendingRow writes ONE claimable pending row straight into the
// sandboxed ledger: the shape TaskNextFiltered selects (status pending, plus a
// payload — a payload-less row is filtered out of every dispatcher selection by
// the bug028 guard). A worker test needs a row the CLAIM would really take, or
// "it refused to claim" would be proven by a row that was never claimable.
func drainSeedPendingRow(t *testing.T, taskID string) {
	t.Helper()
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, payload, status, source, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'auto', ?, ?)`,
		taskID, "drain probe "+taskID, "probe payload", tools.TaskStatusPending, 1000.0, 1000.0); err != nil {
		t.Fatalf("seed row %s: %v", taskID, err)
	}
}

// TestWorkerRoundRefusesClaimWhileDraining is the claim-boundary REFUSAL: with
// a request standing, an eligible pending row must stay pending — the worker
// claims nothing more, and the round returns false so the loop exits instead of
// running.
func TestWorkerRoundRefusesClaimWhileDraining(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	// The gopi data root is named in the SAME fixture as the ledger (devdoc/
	// task-child-fail-escape-sandbox.md): the escape's sandbox precondition
	// measures both, and a fixture that lets one of them resolve to the
	// operator's profile is exactly the shape the 2026-09-16 incident had.
	t.Setenv(config.GopiDirEnv, t.TempDir())
	prevCtx := workerCtx
	defer func() { workerCtx = prevCtx }()
	workerCtx = drainTestCtx(t, dir)

	drainSeedPendingRow(t, "drain-refuse-1")
	if err := workerRequestStopFile(dir, os.Getpid()); err != nil {
		t.Fatalf("write stop request: %v", err)
	}
	//stylegate:ignore claims a round against the test's ledger; the settled state is the assertion
	if ran, _ := workerRoundClaiming(); ran {
		t.Fatal("the worker claimed a row while a drain request was standing")
	}
	status, err := tools.TaskStatusOf("drain-refuse-1")
	if err != nil {
		t.Fatalf("read row status after the refused claim: %v", err)
	}
	if status != tools.TaskStatusPending {
		t.Errorf("row status = %q after a refused claim, want %q (nothing may be claimed while draining)",
			status, tools.TaskStatusPending)
	}
}

// TestWorkerRoundClaimsWhenNoRequestStands is the POSITIVE CONTROL for the
// refusal above: with no request standing, the very same setup does NOT stop at
// the drain guard — the row leaves `pending`. Without this control a guard that
// refused unconditionally would look like a pass (the refusal must be CAUSED by
// the request, not by the new check).
func TestWorkerRoundClaimsWhenNoRequestStands(t *testing.T) {
	resetWorkerStopState()
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	dir := drainTestDir(t)
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	// Both roots of the sandbox named together — see the drain-refusal test above.
	t.Setenv(config.GopiDirEnv, t.TempDir())
	// The child is synthetic (taskChildFailTestEnv): no model call, no API key.
	// The round then fails its run and the claim is released — what is asserted
	// is the CLAIM, which is the only thing the drain guard gates.
	t.Setenv(taskChildFailTestEnv, "1")
	prevCtx := workerCtx
	defer func() { workerCtx = prevCtx }()
	workerCtx = drainTestCtx(t, dir)

	drainSeedPendingRow(t, "drain-claim-control-1")
	_, _ = workerRoundClaiming() //stylegate:ignore claims a round against the test's ledger; the settled state is the assertion
	status, err := tools.TaskStatusOf("drain-claim-control-1")
	if err != nil {
		t.Fatalf("read row status: %v", err)
	}
	if status == tools.TaskStatusPending {
		t.Errorf("row is still %q with no drain request standing — the guard refuses unconditionally", status)
	}
}

// contains is the small substring helper these assertions read through.
func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// TestWorkerOtherDaemonLiveIsTheRefusalVerdict pins the predicate that decides
// whether a REFUSED spawn was covered by a concurrent winner (epic
// worker-graceful-drain): a live daemon other than the one this invocation
// replaced means the restart end state holds, so exiting 1 would be a lie. The
// replacee's own stale record must NOT count — that is the same daemon the
// caller just drained, not a replacement.
func TestWorkerOtherDaemonLiveIsTheRefusalVerdict(t *testing.T) {
	dir := drainTestDir(t)
	if _, ok := workerOtherDaemonLive(dir, 0); ok {
		t.Fatal("no state file exists yet, but a live daemon was reported — the refusal would be read as covered")
	}
	// A live daemon (this test process) publishes its state.
	newWorkerStateWriter(dir, os.Getpid(), "refusal verdict test")
	if _, ok := workerOtherDaemonLive(dir, 0); !ok {
		t.Error("a live daemon was not reported for prevPID=0 — a covered refusal would be misreported as a failure")
	}
	if _, ok := workerOtherDaemonLive(dir, os.Getpid()); ok {
		t.Error("the daemon this restart replaced was counted as a REPLACEMENT — the loser could claim a success it did not have")
	}
}

// drainTestParseStamp asserts the drain timestamp is the RFC3339 the state
// schema promises (a stamp an operator's `--status` cannot parse would be a
// silent lie about how long the worker has been leaving).
func drainTestParseStamp(t *testing.T, stamp string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatalf("drain timestamp %q is not RFC3339: %v", stamp, err)
	}
	return parsed
}

// TestWorkerForeignDrainBinaryDecidesTheSignal pins the predicate that decides
// HOW a worker is asked to leave (acceptance v4, 2026-09-13): only a worker
// whose recorded version EQUALS this binary's own is signalled. A different
// version — the pre-drain generation, whose signal handler forwards into the
// in-flight run child — is asked by FILE ONLY, and an unreadable state is
// treated as foreign too (unknowable ≠ same).
func TestWorkerForeignDrainBinaryDecidesTheSignal(t *testing.T) {
	cases := []struct {
		name string
		st   workerStateFile
		want bool
	}{
		{"same version as this binary = signal it", workerStateFile{Parsed: true, State: workerControlState{PID: 4242, Version: workerProgVersion()}}, false},
		{"different version = file only", workerStateFile{Parsed: true, State: workerControlState{PID: 4242, Version: "7c24ea065580"}}, true},
		{"no version = unknown, file only", workerStateFile{Parsed: true, State: workerControlState{PID: 4242}}, true},
		{"unreadable state = unknown, file only", workerStateFile{}, true},
	}
	for _, c := range cases {
		if got := workerForeignDrainBinary(c.st); got != c.want {
			t.Errorf("%s: workerForeignDrainBinary = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestWorkerAlreadyOrDeliveredNamesTheRecordDifference pins the no-wait
// report's two words: a repeated request must READ as a no-op ("already
// standing") and a first request as a delivery — the operator-facing half of
// the idempotency contract.
func TestWorkerAlreadyOrDeliveredNamesTheRecordDifference(t *testing.T) {
	if got := alreadyOrDelivered(true); got != "already standing" {
		t.Errorf("repeat request rendered as %q, want %q", got, "already standing")
	}
	if got := alreadyOrDelivered(false); got != "delivered" {
		t.Errorf("first request rendered as %q, want %q", got, "delivered")
	}
}
