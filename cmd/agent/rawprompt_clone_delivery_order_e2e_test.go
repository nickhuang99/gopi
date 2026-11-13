package main

// rawprompt_clone_delivery_order_e2e_test.go — the ORDER between a bare-prompt
// clone run's DELIVERY LEG and its DELIVERY PROOF, on the shipped binary.
//
// THE DEFECT (measured 2026-10-24, `gopi` at the epic's base): the end hook
// wrote the row FIRST and delivered SECOND, while the write choke point's
// delivery proof (tools.deliveryProofRefusal) measures the tip against
// refs/remotes/origin/<branch> — a ref the push itself moves. Both sentences
// therefore appeared on ONE stderr, in this order:
//
//	[task-notify] delivery proof: REFUSED <row> — … the tip … is in NEITHER the
//	              ancestry NOR the patches of refs/remotes/origin/master …
//	clone channel: delivery delivered — pushed master to origin after 1 attempt(s)
//
// i.e. the delivery SUCCEEDED, the source repo's master DID move, and the row
// stayed `in_progress` forever because the claim was refused before the push had
// a chance to make it true.
//
// WHAT IS PINNED HERE, on the real binary, in the epic's own acceptance words:
//
//	① DELIVERED: the source repo's master moves AND the row lands `completed`
//	   AND stderr carries NO `delivery proof: REFUSED`;
//	② BLOCKED: the row does NOT land `completed` (it stays OPEN, with the leg's
//	   own sentence as its reason) and nothing was pushed to the source repo.
//
// The two arms are the SAME run shape with ONE difference — the declared build
// (`true` vs `false`), which is the deterministic way to make AttemptDelivery
// refuse at a real step without touching the network, a device or a clock.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// cloneOrderRowIDPattern pulls the auto-registered row id out of the run's own
// stderr — the identifier the operator reads there, not a second bookkeeping
// channel invented for the test.
var cloneOrderRowIDPattern = regexp.MustCompile(`task ledger: registered ([0-9a-f-]{36})`)

// cloneOrderRun drives ONE bare-prompt clone run against a fresh source repo and
// returns what an operator can observe afterwards: the row status, the source
// repo's tips before/after, and the process's stderr.
func cloneOrderRun(t *testing.T, build string) (status, before, after, stderrText string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	bin := testutil.GopiBin(t)
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("gopi binary unavailable: %v", err)
	}
	assertGopiBinIsFresh(t, bin)

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, repo, "init", "-q", "-b", "master")
	rawPromptGit(t, repo, "config", "receive.denyCurrentBranch", "ignore")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rawPromptGit(t, repo, "add", "a.txt")
	rawPromptGit(t, repo, "commit", "-q", "-m", "init")
	before = rawPromptGit(t, repo, "log", "--oneline", "-1")

	gopiHome := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "memory.db")
	root := filepath.Join(t.TempDir(), "workspace")
	settingsPath := filepath.Join(t.TempDir(), "settings.json")
	body := `{"sync_peers":[],"dev_repo":"` + repo + `","workspace_root":"` + root +
		`","repos":{"` + repo + `":{"build":"` + build + `"}}}`
	if err := os.WriteFile(settingsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := testutil.NewMockLLM(t,
		testutil.Resp{ToolUse: &testutil.ToolCall{ID: "call_commit", Name: toolnames.ToolBash,
			Args: map[string]interface{}{"command": "printf 'delivered by a raw prompt\\n' >> a.txt && " +
				"git add -A && git -c user.email=t@t -c user.name=t commit -q -m 'raw prompt clone delivery'"}}},
		testutil.Resp{Text: "committed in the clone workspace"},
	)
	t.Cleanup(mock.Close)

	env := testutil.MockEnv(t, gopiHome, mock.URL())
	env = append(env,
		"GOPI_MEMORY_DB="+dbPath,
		config.SettingsEnv+"="+settingsPath,
		config.SkipClaimBroadcastEnv+"=1",
		"GOPI_SKIP_TERMINAL_PUSH=1",
		"GOPI_AUTO_TASK_REGISTER=1",
		"GOPI_CRASH_DUMP_SWEEP=off",
		config.DevRepoEnvName+"="+repo,
		config.WorkspaceRootEnvName+"="+root,
	)
	stdout, stderr, code := runGopiMapped(t, []string{"commit a small change"}, "", env, t.TempDir())
	t.Logf("run exit=%d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	if code != 0 {
		t.Fatalf("the raw-prompt run must exit 0 (a delivery verdict is not an exit code): code=%d\nstderr:\n%s", code, stderr)
	}
	m := cloneOrderRowIDPattern.FindStringSubmatch(stderr)
	if m == nil {
		t.Fatalf("the run registered no auto row — the fixture premise is broken:\n%s", stderr)
	}
	rowID := m[1]
	// The ledger read goes through the SAME profile the run used (Rule 32: the
	// path comes from the pinned env, never a hand-rolled one).
	t.Setenv("GOPI_MEMORY_DB", dbPath)
	st, err := tools.TaskRowStatusAt(rowID)
	if err != nil {
		t.Fatalf("read the row %s back: %v", rowID, err)
	}
	after = rawPromptGit(t, repo, "log", "--oneline", "-1")
	return st, before, after, stderr
}

// TestRawPromptCloneDeliveryOrderDeliveredClosesTheRow is acceptance ①.
func TestRawPromptCloneDeliveryOrderDeliveredClosesTheRow(t *testing.T) {
	status, before, after, stderrText := cloneOrderRun(t, "true")

	if !strings.Contains(stderrText, "delivery delivered") {
		t.Fatalf("the push landed but the leg did not say `delivered`:\n%s", stderrText)
	}
	if after == before {
		t.Fatalf("the source repo's master did not move — the run's commit never reached origin\nbefore=%q after=%q\n%s",
			before, after, stderrText)
	}
	// THE DEFECT, ASSERTED BY ITS OWN SENTENCE: a delivered run must not have
	// its claim refused by the delivery proof. This is the assertion the bug
	// fails on — before the reorder it was RED on every delivered run.
	if strings.Contains(stderrText, "delivery proof: REFUSED") {
		t.Fatalf("a run whose push LANDED must not have its `completed` claim refused — the delivery leg ran after the claim:\n%s", stderrText)
	}
	if status != tools.TaskStatusCompleted {
		t.Fatalf("a delivered run must land `%s`, got %q\nstderr:\n%s", tools.TaskStatusCompleted, status, stderrText)
	}
}

// TestRawPromptCloneDeliveryOrderBlockedLandsTerminalFailed is acceptance ②.
func TestRawPromptCloneDeliveryOrderBlockedLandsTerminalFailed(t *testing.T) {
	status, before, after, stderrText := cloneOrderRun(t, "false")

	if !strings.Contains(stderrText, "delivery BLOCKED") {
		t.Fatalf("the declared build fails, so the leg must report BLOCKED:\n%s", stderrText)
	}
	if after != before {
		t.Fatalf("a blocked delivery must not move the source repo's master\nbefore=%q after=%q", before, after)
	}
	// THE NON-LYING HALF: a run whose own push did not land must not wear the
	// success terminal. It lands `failed` — a TERMINAL, resumable status that is
	// not a delivery claim, so the proof never disputes it, `gopi session resume`
	// still reaches the context, and the reissue census lists it.
	//
	// `in_progress` was the old answer and it was WRONG in a way nothing
	// repaired (bug 01a19639, measured 2026-11-01): the arm wrote `in_progress`
	// and returned, waiting for a re-drive whose carrier had been retired
	// 2026-10-30, so a real row of exactly this shape read
	// `status: in_progress / verdict: dead / resumable: no` for 65 minutes until
	// an operator typed the `--status failed` by hand. The assertion below is the
	// one that would have caught it: the row must be TERMINAL.
	if status == tools.TaskStatusCompleted {
		t.Fatalf("a run whose delivery was BLOCKED must not close `%s`:\n%s", tools.TaskStatusCompleted, stderrText)
	}
	if status != tools.TaskStatusFailed {
		t.Fatalf("a run whose delivery was BLOCKED must land a terminal `%s` (a parked `%s` is the shape no dispatcher, no resume and no census can act on):\n%s",
			tools.TaskStatusFailed, tools.TaskStatusInProgress, stderrText)
	}
	if !strings.Contains(stderrText, "LEFT OPEN") {
		t.Fatalf("the run must say the claim was NOT written — a silent non-claim is the failure mode this epic exists to end:\n%s", stderrText)
	}
	if !strings.Contains(stderrText, "delivery did not land") {
		t.Fatalf("the row's reason must carry the leg's own sentence:\n%s", stderrText)
	}
}
