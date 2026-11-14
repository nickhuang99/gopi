package main

// clone_channel_delivery_before_claim_e2e_test.go — THE ORDER between a
// `gopi worker` clone run's DELIVERY LEG and its `completed` CLAIM, on the real
// binary.
//
// THE DEFECT (measured 2026-10-25 on the live ledger, row 01a16f9d, and in that
// run's own output.log): in runTaskRunOnce the close-out ran
//
//	① writebackOutcome(completed)      ← the CLAIM
//	② taskRunCloseoutLanded(...)       ← refused, returns early
//	③ cloneRun.finish(...)             ← the PUSH, never reached
//
// and ② is decided by tools.deliveryProofRefusal, which measures the tip the
// run produced against refs/remotes/origin/<branch> — the ref the push itself
// moves. So the gate that only the push can open sat IN FRONT OF the push. Both
// halves of the defect are in that run's own record:
//
//	$ grep -c 'delivery attempt' ~/.gopi/sessions/01a16f9d-…/output.log
//	0                    # the delivery leg never executed a single attempt
//	$ gopi task show 01a16f9d-… | grep -A3 '^result:'
//	result: … the tip 52db940ee320 (the ground's HEAD) is in NEITHER the
//	        ancestry NOR the patches of refs/remotes/origin/master … Deliver it
//	        … and write back again, or leave the row OPEN
//
// — a refusal naming its own remedy ("deliver it … and write back again") while
// the code runs delivering AFTER it. The row stayed open, and the work existed
// only in a ground nobody revisits.
//
// WHAT IS PINNED HERE, in one run and on the shipped binary:
//
//	① the delivery leg RAN — "clone channel delivered on delivery attempt" is on
//	   the run's account with at least ONE attempt;
//	② the source repo's master DID move (the work reached its destination);
//	③ the row landed `completed` — the claim follows a true state instead of
//	   being refused by a destination the delivery had not reached;
//	④ stderr carries NO `delivery proof: REFUSED` — the two duties no longer
//	   disagree on one stream.
//
// THE RED FORM of this test (the order reverted) is the state this epic was
// filed for: ① and ② fail because the leg never runs, ③ fails because the row
// is left open, ④ fails with the refusal sentence. The test does not need a
// mutated binary to be honest about that — it asserts the OUTCOME that only the
// correct order produces, and the failure this fixes was measured on the very
// row named above.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/toolnames"
)

// cloneClaimTaskID is a full uuid (the ground-naming rule needs one).
const cloneClaimTaskID = "01a16fe2-0000-7000-8000-00000000c1a1"

// cloneClaimDeliveryLine is the marker the leg prints when a push lands. It is
// spelled once here so the assertion and the reader of this file agree.
const cloneClaimDeliveryLine = "clone channel delivered on delivery attempt"

// TestWorkerCloneDeliveryRunsBeforeTheCompletedClaim is the gate.
func TestWorkerCloneDeliveryRunsBeforeTheCompletedClaim(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed: the clone channel reads real repositories")
	}
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")
	before := cloneClaimGitLog(t, repo)

	// THE CHILD'S ONE DEED: commit real work in the workspace. It must be a
	// COMMIT — an uncommitted edit is the delivery loop's blocked arm (a
	// different test); what this one needs is a finished deliverable sitting
	// where only the push can move it.
	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		testutil.Resp{ToolUse: &testutil.ToolCall{
			ID:   "call_commit",
			Name: toolnames.ToolBash,
			Args: map[string]interface{}{
				"command": "printf 'delivered by the clone channel\\n' > delivered.txt && " +
					"git add -A && git -c user.email=t@t -c user.name=t commit -q -m 'clone delivery before claim'",
			},
		}},
		testutil.Resp{Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nTask complete."},
	)
	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"delivery before claim", "--payload", "do the work", "--task-id", cloneClaimTaskID,
		"--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := out + errOut
	t.Logf("worker --once exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)

	// ① THE LEG RAN, AND IT SPENT AT LEAST ONE ATTEMPT.
	if !strings.Contains(both, cloneClaimDeliveryLine) {
		t.Fatalf("the clone channel's delivery leg did not run (no %q line): a run whose work is only in its ground was closed out without a push\n%s",
			cloneClaimDeliveryLine, both)
	}
	if !strings.Contains(both, "delivery attempt 1/") {
		t.Fatalf("the delivery leg must record its attempt counter:\n%s", both)
	}

	// ② THE WORK REACHED ITS DESTINATION.
	after := cloneClaimGitLog(t, repo)
	if before == after {
		t.Fatalf("the source repo's master did not move: the delivery leg reported a push the repository does not show\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !cloneClaimRepoHas(t, repo, "delivered.txt") {
		t.Fatalf("the delivered file is absent from the source repo's master:\n%s", after)
	}

	// ③ THE ROW LANDED `completed` — the claim follows the delivery.
	show, showErr, showCode := runGopiMapped(t, []string{"task", "show", cloneClaimTaskID}, "", env, repo)
	if showCode != 0 {
		t.Fatalf("task show: code=%d stderr=%s", showCode, showErr)
	}
	if !cloneClaimStatusIs(t, show, "completed") {
		t.Fatalf("the row must land `completed` after its delivery landed (the claim must follow the push):\n%s", show)
	}

	// ④ THE TWO DUTIES NO LONGER DISAGREE ON ONE STREAM: a delivery-proof
	//    refusal here would mean the claim was measured against a destination
	//    the push had not reached — the exact defect, in its exact words.
	if strings.Contains(both, "delivery proof: REFUSED") {
		t.Fatalf("the delivery proof refused a claim whose push had not run yet — the order is wrong:\n%s", both)
	}
}

// cloneClaimGitLog is the source repo's master tip line — the "did the work
// arrive" measurement, read from the repository itself rather than from the
// run's own account.
func cloneClaimGitLog(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "log", "--oneline", "-1", "master")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log in %s: %v (%s)", repo, err, out)
	}
	return strings.TrimSpace(string(out))
}

// cloneClaimRepoHas reports whether a path exists in the repo's master tree.
func cloneClaimRepoHas(t *testing.T, repo, path string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "cat-file", "-e", "master:"+path)
	return cmd.Run() == nil
}

// cloneClaimStatusIs reads the row's status WORD out of `task show`'s own
// rendering (the operator's view, not a second ledger read this test would have
// to keep in step with the CLI).
//
// THE WORD, not the whole field: the `status:` line chains the render-time
// MEASURED holder facts after it (` + holder dead (pid N gone)`,
// internal/tools/taskholder_gone.go), so an equality on the raw field would
// report "not completed" for a row that IS completed and lost its holder —
// the false success/failure this epic was filed for. The split lives once, in
// statusWord (cli_tasks_statusword_split_test.go).
func cloneClaimStatusIs(t *testing.T, show, want string) bool {
	t.Helper()
	for _, line := range strings.Split(show, "\n") {
		if strings.HasPrefix(line, "status:") {
			return statusWord(strings.TrimSpace(strings.TrimPrefix(line, "status:"))) == want
		}
	}
	return false
}
