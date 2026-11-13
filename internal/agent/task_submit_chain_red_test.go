package agent

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// THE RED-PROOF TEST for epic 01a12e34 — self-contained ON PURPOSE.
//
// It references NO helper that the fix introduced, so it COMPILES AND RUNS
// against the PRE-FIX tree as well as the fixed one: that is what makes it a
// red-then-green witness rather than a restatement of the fix. Run it once with
// internal/agent/loop.go at HEAD (measured: 2 FAILs, quoted in the report's
// evidence) and once with the fix in place (measured: PASS).
//
// It pins the two claims the epic is about, from the LEDGER out — never from
// the receipt prose:
//
//	① the run's FIRST auto-linked submit carries NO parent (the pre-fix code
//	   stamped the run's OWN in_progress row here, parking the head of every
//	   session's chain behind work that is still running);
//	② parent_task_id="independent" leaves the row parentless even when a
//	   previous submission exists to chain to.
// ---------------------------------------------------------------------------

// redProofAgent seeds a fresh ledger whose run row is in_progress — the exact
// precondition of the measured incident.
func redProofAgent(t *testing.T) *Agent {
	t.Helper()
	testutil.NewTestDBPath(t) // Rule 32: the scaffold owns the ledger path + env pin
	const ownRow = "t-red-proof-own-row"
	if _, err := tools.TaskNew("the run's own row", ownRow, "sess-red-proof", "test-model"); err != nil {
		t.Fatalf("seed the run's own row: %v", err)
	}
	if _, err := tools.TaskUpdate(ownRow, tools.TaskStatusInProgress); err != nil {
		t.Fatalf("the run's own row must be in_progress: %v", err)
	}
	return &Agent{autoTaskRowID: ownRow, autoTaskSessionID: "sess-red-proof", Model: "test-model"}
}

// redProofParentOf reads a row's parent edge straight off the ledger.
func redProofParentOf(t *testing.T, taskID string) string {
	t.Helper()
	out, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow(%s): %v", taskID, err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "parent_task_id:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "parent_task_id:"))
			if v == "—" {
				return ""
			}
			return v
		}
	}
	t.Fatalf("TaskShow(%s) carries no parent_task_id line:\n%s", taskID, out)
	return ""
}

func TestTaskSubmitChainRedProof_FirstSubmitIsNeverParentedOnTheRunItsOwnRow(t *testing.T) {
	ag := redProofAgent(t)
	out := ag.executeTool(deepseek.ToolCall{Name: "task_submit", Arguments: map[string]interface{}{
		"summary": "a row submitted FIRST, with nothing before it",
		"payload": "repro steps (a payload is what makes a pending row dispatchable)",
	}})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("submit failed: %s", out)
	}
	id := taskIDFromSubmitOut(t, out)
	if got := redProofParentOf(t, id); got != "" {
		t.Errorf("the FIRST submission of a run has parent_task_id = %q, want NONE: with a parent it is parked behind a row that is still running, and every later item in the fan — each chained to the one before it — is parked with it", got)
	}
}

func TestTaskSubmitChainRedProof_IndependentSentinelYieldsAParentlessRow(t *testing.T) {
	ag := redProofAgent(t)
	head := ag.executeTool(deepseek.ToolCall{Name: "task_submit", Arguments: map[string]interface{}{
		"summary": "chain head",
		"payload": "the row a later submit would chain to",
	}})
	if strings.Contains(head, "task_submit error") {
		t.Fatalf("head submit failed: %s", head)
	}
	headID := taskIDFromSubmitOut(t, head)

	out := ag.executeTool(deepseek.ToolCall{Name: "task_submit", Arguments: map[string]interface{}{
		"summary":        "an item that does NOT depend on the previous one",
		"payload":        "repro steps for the independent item",
		"parent_task_id": "independent",
	}})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("independent submit failed: %s", out)
	}
	id := taskIDFromSubmitOut(t, out)
	if got := redProofParentOf(t, id); got != "" {
		t.Errorf("parent_task_id=\"independent\" produced parent %q (the pre-fix code chained it to the previous submission %q), want a PARENTLESS row", got, headID)
	}
}
