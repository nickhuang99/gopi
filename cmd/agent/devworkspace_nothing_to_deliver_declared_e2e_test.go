package main

// devworkspace_nothing_to_deliver_declared_e2e_test.go — the row that closed
// `completed` while its own report said the work was never done.
//
// THE MEASURED DEFECT (row 01a186b9-064c, 2026-10-29). The clone channel
// measured the ground as empty ("carries no commit to deliver … nothing can be
// delivered or compared") and the close-out wrote `completed` — while the run's
// OWN report opened `**Verdict: BLOCKED**` and stated "本轮没有删除任何代码、没有提交、
// 没有移动 ref". Two facts from one close-out, contradicting each other, and
// nothing read the report.
//
// The unit tests pin the decision (internal/tools/task_nothing_to_deliver_test.go)
// and the predicate (internal/taskreport/declared_test.go). THIS one proves the
// seam an operator actually reads: a real `gopi worker --once` whose agent
// commits nothing and writes a declaring report, judged by the ROW's status and
// the ROW's reason.
//
// The neighbour file (devworkspace_nothing_to_deliver_e2e_test.go) is the other
// half of the same rule and must keep passing: the same empty ground with NO
// admission still closes `completed` (row 01a18244's counter-proof shape).

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/devworkspace"
	"github.com/nickhuang99/gopi/internal/taskreport"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// ntdDeclaredReportBody is the report the scripted agent writes: the same
// declaring shape as the measured row, trimmed to the two lines that matter —
// the verdict token and the deferred-work statement Rule 5 asks for.
const ntdDeclaredReportBody = "# Task Report: the prerequisites never landed\n\n" +
	"**Verdict: BLOCKED** — upstream is not on master, so this run did nothing.\n\n" +
	"## Completed\n\n" +
	"1. Gathered the evidence.\n\n" +
	"## Not Done\n\n" +
	"- The deletion itself: not a single line changed, because the precondition is unmet.\n"

// TestCloneChannelNothingToDeliverButReportDeclaresFailed is the
// acceptance for this defect: the empty-ground exit does NOT claim `completed`
// when the run's own report declares it did not finish, and the row's reason
// carries BOTH sentences.
func TestCloneChannelNothingToDeliverButReportDeclaresFailed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := cloneGateRepo(t)
	root := filepath.Join(t.TempDir(), "workspace")
	const taskID = "01a186b9-0000-7000-8000-00000000e2e3"

	// THE CHILD'S DEED: write the declaring report and commit NOTHING. Every
	// following round answers in prose, so the ground stays level with
	// origin/master — the empty-ground exit is reached for real.
	script := make([]testutil.Resp, 0, devworkspace.MaxDeliveryRounds+2)
	script = append(script, testutil.Resp{ToolUse: &testutil.ToolCall{
		ID:   "call_report",
		Name: "bash",
		Args: map[string]interface{}{
			// The report path is the HARNESS's export, never a model guess
			// (taskreport.PathEnv) — the shell expands it inside the child.
			"command": "cat > \"$" + taskreport.PathEnv + "\" <<'REPORT'\n" + ntdDeclaredReportBody + "REPORT\n",
		},
	}})
	for i := 0; i < devworkspace.MaxDeliveryRounds+2; i++ {
		script = append(script, testutil.Resp{
			Text: "[TASK_LIST_BEGIN]\n[]\n[TASK_LIST_END]\nVerdict: BLOCKED — nothing was changed, on purpose.",
		})
	}
	env := cloneGateEnv(t, repo,
		`"dev_repo":"`+repo+`","workspace_root":"`+root+`"`,
		script...)

	if _, stderr, code := runGopiMapped(t, []string{"task", "submit",
		"nothing to deliver declared e2e", "--payload", "the prerequisite is not in place; do not act",
		"--task-id", taskID, "--repo", repo}, "", env, ""); code != 0 {
		t.Fatalf("submit: code=%d stderr=%s", code, stderr)
	}

	out, errOut, code := runGopiMapped(t, []string{"worker", "--once"}, "", env, repo)
	both := out + errOut
	t.Logf("worker --once exit=%d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)

	// ① THE RUN'S OWN ACCOUNT names the disagreement.
	if !strings.Contains(both, "nothing to deliver") {
		t.Fatalf("the run must report the nothing-to-deliver outcome by name:\n%s", both)
	}

	// ② THE ROW. `task show` is the operator's surface: a verdict only on stderr
	//    is invisible to the next reader of the ledger (Rule 15).
	show, showErr, showCode := runGopiMapped(t, []string{"task", "show", taskID}, "", env, repo)
	if showCode != 0 {
		t.Fatalf("task show: code=%d stderr=%s", showCode, showErr)
	}
	t.Logf("task show:\n%s", show)
	if !strings.Contains(show, tools.TaskStatusFailed) {
		t.Fatalf("a run whose own report declares it did not finish must land %q, not %q:\n%s",
			tools.TaskStatusFailed, tools.TaskStatusCompleted, show)
	}
	if !strings.Contains(show, "BLOCKED") {
		t.Fatalf("the row's reason must quote the author's verdict verbatim:\n%s", show)
	}
	if !strings.Contains(show, "nothing to deliver") {
		t.Fatalf("the row's reason must also carry the channel's own measurement:\n%s", show)
	}

	// ③ THE TRAIL carries the attributed record, so a reader of the row learns
	//    WHY it is not `completed` without opening the report.
	if !strings.Contains(both, tools.NothingToDeliverDeclaredEvent) {
		t.Logf("the trail record %q was not printed on stderr — it is written to the ledger, read via `task show`",
			tools.NothingToDeliverDeclaredEvent)
	}
}
