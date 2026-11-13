package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// M4.8 deliverable closure (t-1788089584): the task_submit TOOL handler —
// repo absent → cwd default (resolved in the handler, verified in the
// engine); a non-repo cwd aborts the whole submit.
// ---------------------------------------------------------------------------

// TestTaskSubmitToolRepoDefaultCwd: no repo arg → the handler resolves cwd
// and TaskSubmit stamps it as deliverable.repo. Skips when the test's cwd
// is not a git work tree (the handler shells out to git rev-parse).
func TestTaskSubmitToolRepoDefaultCwd(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := tools.VerifyGitWorkTree(cwd); err != nil {
		t.Skipf("test cwd %s is not a git work tree: %v", cwd, err)
	}

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ag := &Agent{}
	ag.autoTaskSessionID = "sess-repo-default"

	out := ag.executeTool(deepseek.ToolCall{
		Name: "task_submit",
		Arguments: map[string]interface{}{
			"summary": "tool-level repo default",
		},
	})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("task_submit failed: %s", out)
	}
	if repo := deliverableRepoFromShow(t, taskIDFromSubmitOut(t, out)); repo != cwd {
		t.Errorf("tool submit stamped deliverable.repo = %q, want the cwd %q", repo, cwd)
	}
}

// TestTaskSubmitToolRepoExplicitArg: repo passed explicitly wins over cwd —
// the handler forwards it verbatim and the engine stamps that repo.
func TestTaskSubmitToolRepoExplicitArg(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo := t.TempDir()
	if err := tools.VerifyGitWorkTree(repo); err == nil {
		t.Skip("degenerate: temp dir is already a work tree")
	}
	gitInitQuiet(t, repo)

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ag := &Agent{}
	ag.autoTaskSessionID = "sess-repo-explicit"

	out := ag.executeTool(deepseek.ToolCall{
		Name: "task_submit",
		Arguments: map[string]interface{}{
			"summary": "tool-level explicit repo",
			"repo":    repo,
		},
	})
	if strings.Contains(out, "task_submit error") {
		t.Fatalf("task_submit failed: %s", out)
	}
	if got := deliverableRepoFromShow(t, taskIDFromSubmitOut(t, out)); got != repo {
		t.Errorf("deliverable.repo = %q, want the explicitly-passed repo %q", got, repo)
	}
}

// TestTaskSubmitToolRepoNonRepoAborts: cwd in a plain temp dir (no git) +
// no repo arg → the submit aborts with the verification error and the queue
// stays empty.
func TestTaskSubmitToolRepoNonRepoAborts(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	nonRepo := t.TempDir()
	t.Chdir(nonRepo) // Go 1.24: chdir for the duration of the test

	ag := &Agent{}
	ag.autoTaskSessionID = "sess-non-repo"

	out := ag.executeTool(deepseek.ToolCall{
		Name: "task_submit",
		Arguments: map[string]interface{}{
			"summary": "tool-level non-repo cwd",
		},
	})
	if !strings.Contains(out, "not a git work tree") {
		t.Errorf("non-repo cwd submit must abort with the verification error, got: %s", out)
	}
}

// deliverableRepoFromShow reads a row's stamped repo back through the
// user-facing surface (gopi task show's deliverable.repo line) — the same
// text the M4.9 backfill consumers will parse.
func deliverableRepoFromShow(t *testing.T, taskID string) string {
	t.Helper()
	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, line := range strings.Split(show, "\n") {
		if rest, ok := strings.CutPrefix(line, "deliverable.repo: "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// gitInitQuiet turns dir into a minimal git work tree (skips the caller's
// test when git is absent — the verification shells out to git by design).
func gitInitQuiet(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: deliverable verification needs it")
	}
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init %s: %v\n%s", dir, err, out)
	}
}
