package tools

// task_reopen_evidence_test.go — the TERMINAL-STATE GUARD's regression AT THE
// SHARED WRITER (defect A, epic 01a18f3f).
//
// WHY THIS ARM EXISTS, ON TOP OF THE cmd/agent ONE. The end hook's arms
// (cmd/agent/cli_tasks_terminal_overwrite_test.go) drive writebackOutcome,
// which is ONE caller. The guard itself sits in taskUpdateStampedCore — the
// single state gate EVERY write face passes through — and nothing outside
// that one caller was measuring it. The gap was found the honest way: the
// counter-arm in cmd/agent/cli_resume_failed_row_test.go wrote
// `failed → completed` with a bare TaskUpdateReason to arrange a fixture, and
// the guard refused it. That refusal was RIGHT (it is the exact verdict
// overwrite the guard exists to stop) and the fixture was wrong — but the
// incident also proved no test here pinned the shared writer's behaviour, so
// a future edit to taskUpdateStampedCore could have removed the guard and only
// one cmd/agent test would have noticed.
//
// THE TWO ARMS, and they are deliberately opposite:
//
//	① REFUSAL — `failed → completed` with no authorization is refused, the row
//	   keeps its verdict, and the refusal leaves a record NAMING the evidence
//	   that would release it. (Rule 15: the reason reaches the trail, not only
//	   the returned error — a caller that discards the error must still be
//	   readable afterwards.)
//	② The CONTROL that keeps the guard from becoming a wall: the same write
//	   LANDS once an authorization newer than the verdict is on the row. A
//	   guard with no door is a different defect.
//
// The resume path the fixture should have used in the first place —
// `failed → in_progress` (the LEVEL-1 edge) → `completed` — crosses no
// terminal pair, so it never reaches this guard; that is pinned by
// cmd/agent's TestResumeOnFailedRowMovesItBackToInProgress and by
// TestResumeOfAFailedRowKeepsItsIdentity here.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
)

// reopenEvidenceSeedRow drives a row to the `failed` verdict through the real
// state machine (in_progress → failed), so the fixture carries the trail a real
// failed row carries. Rule 32: the ledger path comes from the test scaffold.
func reopenEvidenceSeedRow(t *testing.T, taskID string) {
	t.Helper()
	if _, err := TaskNew("a row whose attempt was judged FAIL", taskID, "", ""); err != nil {
		t.Fatalf("TaskNew %s: %v", taskID, err)
	}
	if _, err := TaskUpdateReason(taskID, TaskStatusFailed,
		"judged FAIL downstream (post-merge verify of the delivered branch)", ""); err != nil {
		t.Fatalf("the failed verdict: %v", err)
	}
}

// TestTerminalGuardRefusesAnUnauthorizedVerdictOverwrite is arm ① — the red
// proof, at the writer every face shares.
func TestTerminalGuardRefusesAnUnauthorizedVerdictOverwrite(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	const row = "t-reopen-refused"
	reopenEvidenceSeedRow(t, row)

	_, err := TaskUpdateReason(row, TaskStatusCompleted, "delivered", "")
	if err == nil {
		t.Fatalf("failed → completed was ALLOWED with no authorization on the row — a terminal "+
			"verdict is a claim about work that already has an answer; overwriting it without "+
			"evidence is the defect (%s) this guard exists to refuse", TaskReopenRefusedEvent)
	}
	if !strings.Contains(err.Error(), "already reached a terminal verdict") {
		t.Fatalf("the refusal must name the shape it refused, got: %v", err)
	}
	if !strings.Contains(err.Error(), TaskReopenAuthorizedEvent) {
		t.Fatalf("the refusal must NAME the evidence that would release it (%s), got: %v",
			TaskReopenAuthorizedEvent, err)
	}

	// The verdict survived: a refused write changes nothing.
	st, err := TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != TaskStatusFailed {
		t.Fatalf("status = %q after a refused write, want %q — a refusal that still lands the "+
			"write is the silent rewrite", st, TaskStatusFailed)
	}

	// The refusal is a RECORD on the row, not only a return value.
	note, err := TaskReopenEvidence(row, TaskStatusFailed)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if strings.TrimSpace(note) != "" {
		t.Fatalf("a refused write must leave NO authorization behind, got %q", note)
	}
}

// TestTerminalGuardReleasesTheWriteWithEvidence is arm ② — the control. The
// same write lands once someone records WHY the verdict is superseded. Without
// this arm the guard could be "fixed" into a wall and arm ① would still pass.
func TestTerminalGuardReleasesTheWriteWithEvidence(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", testutil.NewTestDBPath(t))

	const row = "t-reopen-evidenced"
	const why = "the failed verdict came from a stale binary; the work was re-run and its " +
		"delivery is the tip named on the row — reopening is deliberate, on review"

	reopenEvidenceSeedRow(t, row)
	if err := TaskReopenRecord(row, TaskReopenAuthorizedEvent, why); err != nil {
		t.Fatalf("authorization record: %v", err)
	}
	if _, err := TaskUpdateReason(row, TaskStatusCompleted, "delivered", ""); err != nil {
		t.Fatalf("an authorization newer than the verdict must release the write: %v", err)
	}
	st, err := TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", st, TaskStatusCompleted)
	}
}
