package main

// clone_channel_retired_row_e2e_test.go — the RETIRED-ROW boundary on the REAL
// binary, through the whole `gopi worker` clone-channel path (row 01a18392).
//
// The unit tests prove the loop's arithmetic and the production status reader.
// What neither can prove is the thing an operator actually experiences: a
// long-running `gopi worker` round, whose row is cancelled WHILE the round is
// running, must stop spending deliveries and must not spawn the retry agent
// round. That is a fact about the assembled binary across a process boundary —
// the child is a real gopi run, the ledger is a real sqlite ledger, and the
// cancel is issued from inside the run by the model's own bash tool.
//
// THE MEASURED DEFECT this pins: before the fix the loop never read the row at
// all, so the same scenario spent "delivery attempt 1/3", spawned an agent
// round, "delivery attempt 2/3", another round, "delivery attempt 3/3", and
// finally wrote `clone channel: delivery did not land in 3 attempt(s) …` onto a
// row its owner had already cancelled.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

// cloneRetiredTaskID is a full uuid (the ground-naming rule needs one).
const cloneRetiredTaskID = "01a18392-0000-7000-8000-0000000e2e01"

// cloneRetiredStopLine is the marker a stopped loop prints. Spelled once here so
// the assertion and the reader of this file agree.
const cloneRetiredStopLine = "so the delivery loop stopped after"

// TestWorkerDeliveryLoopStopsWhenTheRunCancelsItsOwnRow is the gate: the model's
// round cancels the row through the real CLI, then leaves its work UNCOMMITTED
// (the dirty blocker, which is what makes the loop want a retry round), and the
// binary must stop at the boundary instead of spawning it.
func TestWorkerDeliveryLoopStopsWhenTheRunCancelsItsOwnRow(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed: the clone channel reads real repositories")
	}
	bin := testutil.GopiBin(t)
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")

	// THE CHILD'S TWO DEEDS, in ONE bash call so the order cannot invert: (1)
	// the run cancels its own row, exactly as an operator's `gopi task update`
	// would, and (2) it leaves uncommitted work, so the delivery attempt has a
	// blocker and the loop reaches the boundary that used to spawn a retry.
	cancelCmd := "printf 'half done\\n' > wip.txt && " +
		bin + " task update " + cloneRetiredTaskID + " --status cancelled --reason 'cancelled by its own operator mid-run'"
	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_cancel",
			Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": cancelCmd},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete."},
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"retired row delivery", "--payload", "do the work", "--task-id", cloneRetiredTaskID,
		"--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	out, errOut, _ := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := out + errOut
	t.Logf("worker --once\nstdout:\n%s\nstderr:\n%s", out, errOut)

	// ① THE LOOP STOPPED AT ITS BOUNDARY, and said which status stopped it.
	if !strings.Contains(both, cloneRetiredStopLine) {
		t.Fatalf("the delivery loop never reported a retirement stop (%q absent):\n%s", cloneRetiredStopLine, both)
	}
	if !strings.Contains(both, "the row is cancelled") {
		t.Fatalf("the stop must name the ledger's own status:\n%s", both)
	}
	// ② IT DID NOT SPEND THE REMAINING ATTEMPTS — one attempt, never two.
	if strings.Contains(both, "delivery attempt 2/") {
		t.Fatalf("a cancelled row must not reach a second delivery attempt:\n%s", both)
	}
	// ③ AND IT WROTE NEITHER TERMINAL VERDICT. `delivery did not land` is the
	// exhausted() sentence a cancelled row used to be branded with.
	if strings.Contains(both, "delivery did not land") {
		t.Fatalf("a cancelled row must not be given the failure verdict:\n%s", both)
	}
	if strings.Contains(both, "nothing to deliver") {
		t.Fatalf("a cancelled row must not be given the nothing-to-deliver verdict:\n%s", both)
	}
	// ④ THE ROW KEEPS THE OPERATOR'S DECISION — the close-out must not rewrite
	// a cancelled row into `failed`.
	show, showErr, showCode := runGopiMapped(t, []string{"task", "show", cloneRetiredTaskID}, "", env, repo)
	if showCode != 0 {
		t.Fatalf("task show: code=%d stderr=%s", showCode, showErr)
	}
	if !cloneClaimStatusIs(t, show, "cancelled") {
		t.Fatalf("the row must still read `cancelled` (the operator's decision is not the loop's to rewrite):\n%s", show)
	}
}
