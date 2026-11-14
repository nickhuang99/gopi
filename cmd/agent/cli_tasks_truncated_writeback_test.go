package main

// cli_tasks_truncated_writeback_test.go — the WRAPPER face of the truncation
// rule. `gopi task run` / a worker round spawns a child and writes the HUMAN
// row's terminal status AFTER cmd.Wait. A child the engine cut short exits 0,
// so that writeback used to stamp `completed` on a half-finished run even
// after the child's own end hook had recorded the truncation — the false
// success, one door later.
//
// The child's record is the authority; the wrapper must read it back rather
// than re-derive the verdict from the exit code.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/runend"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestWritebackOutcomeRefusesCompletedOnTruncatedRow: a row whose run recorded
// an engine truncation must not be closed `completed` by the wrapper's write.
func TestWritebackOutcomeRefusesCompletedOnTruncatedRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")

	const row = "t-trunc-writeback"
	if _, err := tools.TaskNew("run the cap-truncated work", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	// What the child's end hook records — the reason column + the digest.
	if _, err := tools.TaskUpdateReason(row, tools.TaskStatusFailed,
		runend.WallclockCap.ReasonText(),
		" — elapsed=25.0s iter=2 tokens=17 | "+runend.WallclockCap.Detail()); err != nil {
		t.Fatalf("child record: %v", err)
	}

	// The wrapper's own write, with the child's exit code 0 (the defect's
	// exact shape: it would claim `completed` here).
	writebackOutcome(row, tools.TaskStatusCompleted, "child exit 0", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusFailed {
		t.Fatalf("status = %q, want %q — the wrapper must not complete a truncated run", st, tools.TaskStatusFailed)
	}
	show, err := tools.TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if !strings.Contains(show, runend.TruncationMarker) || !strings.Contains(show, runend.WallclockCap.Token()) {
		t.Fatalf("the wrapper's writeback must carry the truncation into the row:\n%s", show)
	}
	// …and the operator gets the recovery pointer, which is the actionable
	// half of a truncated row.
	if !strings.Contains(show, "gopi session resume") {
		t.Fatalf("the truncated writeback must keep the resume pointer:\n%s", show)
	}
}

// TestWritebackOutcomeCompletesAnOrdinaryRow is the control: without a
// recorded truncation the writeback keeps its original behaviour — the rule is
// about what the run recorded, not about being cautious with every row.
func TestWritebackOutcomeCompletesAnOrdinaryRow(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(tools.TaskWrapperEnv, "")

	const row = "t-plain-writeback"
	if _, err := tools.TaskNew("ordinary run", row, "", ""); err != nil {
		t.Fatalf("TaskNew: %v", err)
	}
	writebackOutcome(row, tools.TaskStatusCompleted, "child exit 0", "", "")

	st, err := tools.TaskStatusOf(row)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st != tools.TaskStatusCompleted {
		t.Fatalf("status = %q, want %q — an untruncated row still completes", st, tools.TaskStatusCompleted)
	}
	show, err := tools.TaskShow(row)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	if strings.Contains(show, runend.TruncationMarker) {
		t.Fatalf("no truncation may be invented for an ordinary row:\n%s", show)
	}
}
