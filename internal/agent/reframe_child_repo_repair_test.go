package agent

// reframe_child_repo_repair_test.go — THE CHILD MUST NOT INHERIT A DEAD
// WORKSPACE (epic 01a15629).
//
// THE MEASURED DEFECT (rows 01a154de / 01a1510e / 01a1513b). In the clone
// channel the parent row's deliverable.repo IS its workspace
// (/home/user/workspace/<parent-id>), and that workspace is RECLAIMED when the
// parent's push lands. A reframe/split child then inherited the path through
// reframeChildRepo (which honours the parent row verbatim), and the clone
// channel refused it — correctly — but with the WRONG reason: "declares no build
// command in settings repos", for a directory that no longer existed.
//
// THIS TEST IS THE RED PROOF: it names no symbol the fix adds. It seeds a parent
// row whose repo is a workspace path that is GONE and whose own deliverable
// records the live repository, asks the REAL reframe handler for the children's
// repo, and asserts the two facts the acceptance names:
//
//	① the children's repo is NOT the dead path;
//	② it IS the live repository (and the repair said so, in the agent's log).

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/devworkspace"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// TestReframedChildRepairsAParentRepoThatWasReclaimed is the epic's first red:
// a parent whose repo is a reclaimed workspace must produce children naming the
// LIVE repository its row recorded — never the dead path.
func TestReframedChildRepairsAParentRepoThatWasReclaimed(t *testing.T) {
	repo := reframeRepairFixture(t) // a real repository with one commit
	t.Setenv(config.WorkspaceRootEnvName, filepath.Join(filepath.Dir(repo), "workspace"))
	root, _, err := config.WorkspaceRootConfigured()
	if err != nil {
		t.Fatalf("workspace root: %v", err)
	}
	parentID := "01a15629-0000-7000-8000-00000parent"
	// The parent's workspace: created by the fixture, then RECLAIMED — the shape
	// the channel leaves behind.
	workspace := devworkspace.Dir(root, parentID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	reframeRepairSeed(t, parentID, workspace, repo)
	if err := os.RemoveAll(workspace); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	// THE PARENT RUN'S OWN POSITION: in the clone channel the decomposing run
	// stands in ITS workspace, and that is the value reframeSubmitRepo carries —
	// which is exactly the path a child used to inherit.
	ag := &Agent{Ctx: &Context{}, autoTaskRowID: parentID, Model: "deepseek-v4-flash",
		reframeSubmitRepo: workspace, Logger: log.New(&logs, "", 0)}
	ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
	out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.TooBig)})
	if !strings.Contains(out, "child task(s)") {
		t.Fatalf("the roadmap must have been enqueued; got: %s", out)
	}
	children, cerr := tools.TaskChildren(parentID)
	if cerr != nil {
		t.Fatalf("TaskChildren: %v", cerr)
	}
	if len(children) == 0 {
		t.Fatalf("no children enqueued")
	}
	for _, c := range children {
		got := deliverableRepoFromShow(t, c.TaskID)
		if tools.SameDir(got, workspace) {
			t.Errorf("child %s names the PARENT'S RECLAIMED WORKSPACE %s as its repo — the directory is gone and the clone channel refuses it (the 01a1510e defect)", c.TaskID, got)
		}
		if !tools.SameDir(got, repo) {
			t.Errorf("child %s repo = %q, want the LIVE repository the parent row recorded %q", c.TaskID, got, repo)
		}
	}
	// THE REPAIR IS LOUD (Rule 15): the log names the dead path and its
	// replacement, so an operator reading the run knows the value moved.
	if !strings.Contains(logs.String(), workspace) || !strings.Contains(logs.String(), repo) {
		t.Fatalf("the repair must be printed with both paths; the agent log was:\n%s", logs.String())
	}
}

// reframeRepairFixture builds a real repository with one commit and returns it.
func reframeRepairFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the repo must be a real work tree")
	}
	repo := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cloneGit(t, repo, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cloneGit(t, repo, "add", "a.txt")
	cloneGit(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// reframeRepairSeed inserts a row whose deliverable names `repo` as its
// repository and `worktree` as the checkout it ran in.
func reframeRepairSeed(t *testing.T, taskID, worktree, repo string) {
	t.Helper()
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(config.SkipClaimBroadcastEnv, "1")
	if _, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "parent whose workspace was reclaimed", Source: taskSourceHumanTest,
		TaskID: taskID, Repo: repo,
	}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := tools.TaskStampDeliverableGroundForTest(taskID, worktree); err != nil {
		t.Fatalf("stamp the parent's workspace: %v", err)
	}
	if got, _ := tools.TaskDeliverableWorktreeFor(taskID); !tools.SameDir(got, worktree) {
		t.Fatalf("fixture: parent worktree = %q, want %q", got, worktree)
	}
	for _, status := range []string{tools.TaskStatusClaimed, tools.TaskStatusInProgress} {
		if _, err := tools.TaskUpdate(taskID, status); err != nil {
			t.Fatalf("move the parent row to %s: %v", status, err)
		}
	}
}
