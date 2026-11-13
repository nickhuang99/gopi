package main

// cli_tasks_terminal_overwrite_test.go — the TERMINAL-STATE GUARD's regression
// (defect A, epic 01a18f3f).
//
// THE MEASURED DEFECT. The wrapper's writebackOutcome downgrades `completed` to
// `failed` on ONE signal (runend.Truncated()). A run that died on a network
// error exits 1, which is not in that set, so the wrapper wrote `completed` over
// a row the ledger already held as `failed` — without ever looking at the
// previous terminal status. Two rows on this host carry the shape, both read
// read-only from ~/.gopi/memory.db (2026-10-31):
//
//	01a18799-3304-…  #12005 failed → #12049 delivery-proof-owed →
//	                 #12050 delivery-blocked ("writeback refused, the row is
//	                 still open") → #12065 completed
//	01a18ea5-8925-…  #12007 failed → #12059 completed
//
// These tests drive the REAL surface (`writebackOutcome`, the end hook's own
// call) against a REAL ledger, because a predicate test would pass on the
// unfixed tree as long as nothing called the predicate.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// failedRowWithNetworkDeath builds the fixture shape: a row the ledger already
// holds as `failed`, closed by a run that died on a network error (exit status
// 1 — the shape runend does NOT classify as truncated).
func failedRowWithNetworkDeath(t *testing.T, row string) {
	t.Helper()
	if _, err := tools.TaskNew("a run that died on the network", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusFailed,
		"gopi run failed: exit status 1 — child stderr: read tcp 192.168.1.12:52098->121.204.231.156:443: read: connection timed out "+
			"— task run failed (abnormal child exit)",
		""); err != nil {
		t.Fatalf("the failed verdict: %v", err)
	}
}

// TestWritebackOutcomeDoesNotOverwriteAFailedRow is the round-1 defect: the end
// hook's `completed` write lands on a row whose verdict is already `failed`,
// with no evidence, and the test asserts it does NOT.
func TestWritebackOutcomeDoesNotOverwriteAFailedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")

	const row = "t-overwrite-failed"
	failedRowWithNetworkDeath(t, row)

	// The wrapper's own write — exactly what the success arm calls when the
	// child exited 0 after a retry, or on any path that re-reaches close-out.
	writebackOutcome(row, tools.TaskStatusCompleted, "gopi run completed", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusFailed {
		t.Fatalf("status = %q, want %q — a wrapper write must not overwrite a terminal verdict "+
			"it did not make; the ledger held `failed` and nothing on the row authorized reopening it",
			st, tools.TaskStatusFailed)
	}
	// The refusal must be a RECORD, not only a return value: the wrapper's
	// stderr belongs to a process that has already exited (the measured shape
	// of the writeback-lost defect). `task show` renders the newest trail entry
	// as `result:`, so the refusal's own sentence is the observable.
	show, err := tools.TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, "refused: this row already reached a terminal verdict") {
		t.Fatalf("the refusal must leave a %s record on the row's trail:\n%s",
			tools.TaskReopenRefusedEvent, show)
	}
	if !strings.Contains(show, tools.TaskReopenAuthorizedEvent) {
		t.Fatalf("the refusal's reason must NAME the evidence that would release it (%s):\n%s",
			tools.TaskReopenAuthorizedEvent, show)
	}
}

// TestWritebackOutcomeReopensWithEvidence is the control, and it is what keeps
// the guard from becoming a wall: a row whose failure was WRONG can still be
// closed, provided someone recorded why. The evidence must be an event — not a
// tag: both measured rows carry `resumed:×1`, so lineage distinguishes nothing.
func TestWritebackOutcomeReopensWithEvidence(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")

	const row = "t-reopen-authorized"
	const why = "the failed verdict came from a dead network link; the work was re-run and its " +
		"delivery is the tip named on the row — reopening is deliberate, on review"

	failedRowWithNetworkDeath(t, row)
	if err := tools.TaskReopenRecord(row, tools.TaskReopenAuthorizedEvent, why); err != nil {
		t.Fatalf("authorization record: %v", err)
	}
	writebackOutcome(row, tools.TaskStatusCompleted, "gopi run completed", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusCompleted {
		t.Fatalf("status = %q, want %q — an authorization newer than the failure releases the "+
			"reopen", st, tools.TaskStatusCompleted)
	}
}

// TestWritebackOutcomeIgnoresAStaleAuthorization pins the POSITION half of the
// criterion: an authorization that predates the failure authorized a DIFFERENT
// verdict. Without this, any row that was ever reopened once could be reopened
// forever, which is the "append-only verdict" property this guard exists for.
func TestWritebackOutcomeIgnoresAStaleAuthorization(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")

	const row = "t-reopen-stale"
	if _, err := tools.TaskNew("a row that was authorized BEFORE it failed", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// Authorization FIRST…
	if err := tools.TaskReopenRecord(row, tools.TaskReopenAuthorizedEvent,
		"an authorization that belongs to the run before this one"); err != nil {
		t.Fatalf("authorization record: %v", err)
	}
	// …then the failure the guard must protect.
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusFailed,
		"gopi run failed: exit status 1 — read: connection timed out", ""); err != nil {
		t.Fatalf("the failed verdict: %v", err)
	}

	writebackOutcome(row, tools.TaskStatusCompleted, "gopi run completed", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusFailed {
		t.Fatalf("status = %q, want %q — an authorization OLDER than the failure authorized a "+
			"different verdict and must not release this one", st, tools.TaskStatusFailed)
	}
}

// TestOperatorForceIsADoorNotABypass pins the operator's route out of the
// guard: `--force` means "I have looked at this verdict and I am superseding
// it", so it must (a) be refused with no reason — an override with no sentence
// behind it is the same silent rewrite, with the operator's fingerprint on it —
// and (b) leave the SAME authorization record a reviewer writes by hand, so the
// wrapper (which never passes --force) stays fixed and the two paths cannot
// drift.
func TestOperatorForceIsADoorNotABypass(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	const row = "t-operator-force"
	failedRowWithNetworkDeath(t, row)

	// (a) no reason → refused, and the row keeps its verdict.
	if _, err := tools.TaskUpdateReasonForced(row, tools.TaskStatusCompleted, "", "", true); err == nil {
		t.Fatalf("--force with no --reason must be refused: the reason IS the evidence")
	}
	if st, err := tools.TaskStatusOf(row); err != nil || st != tools.TaskStatusFailed {
		t.Fatalf("status = %q (%v), want %q — a refused override changes nothing", st, err, tools.TaskStatusFailed)
	}

	// (b) with a reason → the write lands AND the authorization is on the trail.
	const why = "probe: I looked at the failed verdict; the work was re-run and delivered"
	if _, err := tools.TaskUpdateReasonForced(row, tools.TaskStatusCompleted, why, "", true); err != nil {
		t.Fatalf("--force --reason must release the reopen: %v", err)
	}
	if st, err := tools.TaskStatusOf(row); err != nil || st != tools.TaskStatusCompleted {
		t.Fatalf("status = %q (%v), want %q", st, err, tools.TaskStatusCompleted)
	}
	note, err := tools.TaskReopenEvidence(row, tools.TaskStatusFailed)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if strings.TrimSpace(note) == "" {
		t.Fatalf("the override must leave a %s record — an unrecorded override is the defect",
			tools.TaskReopenAuthorizedEvent)
	}
}
