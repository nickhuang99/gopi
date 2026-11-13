package agent

// reframe_child_repo_inheritance_test.go — THE PARENT ROW IS THE ONLY SOURCE of
// a roadmap child's repo (bug 01a1515c, measured live 2026-10-18).
//
// THE DEFECT. dispatchReframeRoadmap resolved the children's repo from the
// PROCESS CWD (tools.DurableRepoForSubmit(reframeSubmitCwd())). For the pre-fix
// epic 01a0d55b that was a fix: a LINKED WORKTREE's cwd resolves through git's
// common dir to the main checkout. Under the CLONE CHANNEL it is the bug: the
// parent's cwd is /home/nick/workspace/<parent-id>, a STANDALONE CLONE — its own
// repository, not a worktree of the source — so git's common dir answers with
// the clone itself and every child was born naming it:
//
//	parent 01a150cb deliverable.repo = /home/nick/work/gopi   deliverable.worktree = /home/nick/workspace/01a150cb-…
//	child  01a1510e deliverable.repo = /home/nick/workspace/01a150cb-…   ← the parent's WORKSPACE
//
// and the clone channel then refused each child — correctly — with "repo …
// declares no build command in settings repos". Three of three splits died the
// same way (01a1510e, 01a1513b, 01a15103).
//
// THIS TEST IS THE RED PROOF (Rule 10.4): it uses no symbol the fix adds, so it
// compiles and runs on the pre-fix tree, where the child inherits the parent's
// workspace and the assertion below fails.

import (
	"github.com/nick/gopi/internal/testutil"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// TestReframedChildInheritsTheParentsRepoNotItsWorktree is the whole property in
// one place: from the position a clone-channel parent stands in, the roadmap
// children name THE PARENT ROW'S deliverable.repo — never the parent's
// workspace/worktree, and never whatever repository the process happens to be
// standing in.
func TestReframedChildInheritsTheParentsRepoNotItsWorktree(t *testing.T) {
	source, _ := reframeGroundFixture(t) // source = the MAIN checkout (a real repository with a commit)
	sourceReal := canonicalDir(source)

	// THE PARENT'S WORKSPACE: a STANDALONE CLONE of the source — the exact shape
	// the clone channel creates at workspace_root/<task-id>, and the shape whose
	// `git rev-parse --git-common-dir` answers with ITSELF. (In that channel the
	// row's deliverable.worktree IS this directory, so "not the worktree" and
	// "not the cwd" are one assertion here.)
	parentWorkspace := filepath.Join(t.TempDir(), "01a1515c-0000-7000-8000-00000000w0rk")
	cloneGit(t, filepath.Dir(parentWorkspace), "clone", "-q", source, parentWorkspace)
	workspaceReal := canonicalDir(parentWorkspace)

	// A THIRD repository: what the process stands in when the parent row names
	// something else. The fix must read the ROW, so this directory must not
	// appear on any child.
	_, elsewhere := reframeGroundFixture(t)
	elsewhereReal := canonicalDir(elsewhere)

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// THE PARENT RUN'S POSITION: the swap. First it stands in its own workspace
	// (the measured defect's cwd), then in an UNRELATED repository — a child born
	// from either position must still name the parent row's repo. One FRESH
	// parent row per position, so neither sub-test can read the other's children.
	for _, tc := range []struct{ name, cwd string }{
		{"in the parent's workspace", parentWorkspace},
		{"in an unrelated repository", elsewhere},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
			parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{
				Summary: "parent epic", Source: taskSourceHumanTest, Repo: source,
			})
			if err != nil {
				t.Fatalf("seed parent: %v", err)
			}
			parentID := taskIDFromSubmitOut(t, parentOut)
			if got := deliverableRepoFromShow(t, parentID); !tools.SameDir(got, sourceReal) {
				t.Fatalf("fixture: parent row repo = %q, want %q", got, sourceReal)
			}
			for _, status := range []string{tools.TaskStatusClaimed, tools.TaskStatusInProgress, tools.TaskStatusCompleted} {
				if _, err := tools.TaskUpdate(parentID, status); err != nil {
					t.Fatalf("move the parent row to %s: %v", status, err)
				}
			}
			t.Chdir(tc.cwd)
			ag := &Agent{Ctx: &Context{}, autoTaskRowID: parentID, Model: "deepseek-v4-flash"}
			ag.Ctx.AddMessage(deepseek.Message{Role: "user", Content: "[EPIC GOAL]\n" + reframeBigBriefing()})
			out := ag.executeTool(deepseek.ToolCall{Name: toolnames.ToolTask, Arguments: reframeValidArgs(taskverdict.TooBig)})
			if !strings.Contains(out, "child task(s)") {
				t.Fatalf("the roadmap must have been enqueued; got: %s", out)
			}
			children, err := tools.TaskChildren(parentID)
			if err != nil {
				t.Fatalf("TaskChildren: %v", err)
			}
			if len(children) == 0 {
				t.Fatalf("no children enqueued")
			}
			for _, c := range children {
				repo := deliverableRepoFromShow(t, c.TaskID)
				if repo == "" {
					t.Errorf("child %s carries no deliverable.repo", c.TaskID)
					continue
				}
				// THE DEFECT, stated positively: the child must not name the
				// parent's workspace (= its worktree in the clone channel).
				if tools.SameDir(repo, workspaceReal) {
					t.Errorf("child %s names the parent's WORKSPACE %s as its repo — a per-run checkout declares no build and the clone channel refuses it (the 01a150cb/01a1510e defect)", c.TaskID, repo)
				}
				// ...nor whatever repository this process stands in.
				if tools.SameDir(repo, canonicalDir(tc.cwd)) {
					t.Errorf("child %s names the PROCESS CWD %s as its repo — the repo must come from the parent ROW, not from where this run happens to stand", c.TaskID, repo)
				}
				// THE PROPERTY: the parent row's deliverable.repo, verbatim.
				if !tools.SameDir(repo, sourceReal) {
					t.Errorf("child %s repo = %q, want the parent row's deliverable.repo %q", c.TaskID, repo, sourceReal)
				}
			}
		})
	}
	// The unrelated repository is reached through the sub-test position; keep
	// its canonical form used so a reader sees the intent.
	_ = elsewhereReal
}

// cloneGit runs one git command in dir, failing the test on error.
func cloneGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}
