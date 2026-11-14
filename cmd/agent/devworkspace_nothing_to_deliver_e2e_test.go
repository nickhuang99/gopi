package main

// devworkspace_nothing_to_deliver_e2e_test.go — the items=0 run, end to end.
//
// THE DEFECT THIS PINS (row 01a18244, measured on rows 01a17e38 / 01a17e7c /
// 01a17e04): a run that was asked to produce NO change produced none, its clone
// ground carried 0 commits past origin/master, and the channel recorded the
// ledger row `failed` with the sentence "delivery did not land" — the same
// sentence a refused push earns. Two opposite facts, one exit.
//
// The unit test (devworkspace_delivery_loop_test.go) pins the fork's own
// classification. THIS one proves the seam an operator actually reads: a real
// `gopi worker --once` whose agent never commits, judged by the ROW's status and
// the ROW's reason.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// TestCloneChannelNothingToDeliverClosesTheRowAsCompleted is the acceptance for
// the split: an empty ground is a CONCLUSION, so the row lands `completed` with
// a reason that says "nothing to deliver" — and never the failure's sentence.
func TestCloneChannelNothingToDeliverClosesTheRowAsCompleted(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")
	const taskID = "01a150eb-0000-7000-8000-00000000e2e2"

	// THE CHILD'S DEED IS NOTHING: every round answers in prose and touches no
	// file, so the ground stays level with origin/master. One response per
	// delivery round plus the closing report round, and then one spare so a
	// mock that pops an extra answer cannot turn a green into a flake.
	script := make([]testutil.Resp, 0, devworkspace.MaxDeliveryRounds+2)
	for i := 0; i < devworkspace.MaxDeliveryRounds+2; i++ {
		script = append(script, testutil.Resp{
			Text: "This items=0 task needs no change: nothing is committed, on purpose.",
		})
	}
	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		script...)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"nothing to deliver e2e", "--payload", "verify the count is zero; produce no change",
		"--task-id", taskID, "--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := out + errOut
	t.Logf("worker --once exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)

	// ① THE RUN'S OWN ACCOUNT: the exit says which fact happened.
	if !strings.Contains(both, "nothing to deliver") {
		t.Fatalf("the run must report the nothing-to-deliver outcome by name:\n%s", both)
	}

	// ② THE ROW CARRIES IT (Rule 15): a verdict only on stderr is invisible to
	//    the next reader of the ledger. `task show` is the operator's surface.
	show, showErr, showCode := runGopiMapped(t, []string{"task", "show", taskID}, "", env, repo)
	if showCode != 0 {
		t.Fatalf("task show: code=%d stderr=%s", showCode, showErr)
	}
	t.Logf("task show:\n%s", show)
	if !strings.Contains(show, tools.TaskStatusCompleted) {
		t.Fatalf("a run with nothing to deliver must land %q, not %q:\n%s",
			tools.TaskStatusCompleted, tools.TaskStatusFailed, show)
	}
	if !strings.Contains(show, "nothing to deliver") {
		t.Fatalf("the row's reason must say nothing to deliver:\n%s", show)
	}
	if strings.Contains(show, "delivery did not land") {
		t.Fatalf("the row must NOT wear the refused-delivery sentence:\n%s", show)
	}
	// ②b THE IDLE SHAPE IS VISIBLE, and this walk is the end-to-end proof of it
	//    (row 01a1b48a-8746, measured 2026-11-07). The mock above is a run that
	//    answers in prose, touches nothing and writes no report — the idlest
	//    ground the channel can meet. Before this arm it closed `completed` with
	//    nothing but the channel's sentence, which is byte-identical to what a
	//    counter-proof run's row carries; the difference is now ON THE ROW.
	if !strings.Contains(show, tools.NoDeliverableMarker) {
		t.Fatalf("a ground with no commit and no authored report must carry %s on its row — "+
			"otherwise nothing in the ledger separates it from a counter-proof run:\n%s",
			tools.NoDeliverableMarker, show)
	}

	// ③ THE WORKSPACE GOES: with no commit to publish it holds nothing origin
	//    does not already have, so keeping it would leave exactly the clone
	//    residue the channel exists to not produce.
	if _, statErr := os.Stat(devworkspace.Dir(root, taskID)); statErr == nil {
		t.Fatalf("an empty ground must be reclaimed, but %s is still there",
			devworkspace.Dir(root, taskID))
	}
}
