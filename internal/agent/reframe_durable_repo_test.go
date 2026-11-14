package agent

// reframe_durable_repo_test.go — a TOO_BIG reframe's child rows must carry the
// DURABLE repository, never the parent run's ephemeral GROUND.
//
// THE DEFECT (measured live, epic 01a0d55b): a TOO_BIG reframe enqueues its
// roadmap children through reframeSubmitOptions, which named no repo — so
// tools.TaskSubmit fell back to taskSubmitResolveDefaultRepo: the submitting
// PROCESS CWD. For a worker-run parent that cwd IS the run's ground
// (/home/user/workspace/gopi-<parent-uuid>), a LINKED WORKTREE the worker
// creates for the parent run and DELETES when the parent finishes. The four
// children of 01a0d526 were therefore born naming a directory that died with
// their parent:
//
//	parent 01a0d526 deliverable = {"branch":"task/<uuid>","repo":"/home/user/work/GoPi",
//	                               "worktree":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
//	child  01a0d528 deliverable = {"repo":"/home/user/workspace/gopi-01a0d52643bf701f9dba6746c8a09d8e"}
//	                                   ^ the parent's WORKTREE, stamped as the child's REPO
//
// `gopi worker <child>` then refused — correctly — with "the repo is not a git
// work tree ... refusing to start the run in the repo's main checkout".
//
// THIS TEST IS THE RED PROOF (Rule 10.4): it compiles and runs on the PRE-FIX
// tree (it uses no symbol the fix adds — a copy of this file dropped into a
// pre-fix checkout fails on the ground assertion below), and it is green on the
// fixed tree.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/taskverdict"
	"github.com/nick/gopi/internal/toolnames"
	"github.com/nick/gopi/internal/tools"
)

// reframeGroundFixture makes a real repository plus one LINKED WORKTREE of it —
// the exact position a worker-run parent stands in when it files its TOO_BIG
// verdict. Returns (main checkout, ground).
func reframeGroundFixture(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not installed: the durable-repo resolution needs a real work tree")
	}
	base := t.TempDir()
	main := filepath.Join(base, "reframe-main")
	ground := filepath.Join(base, "reframe-ground")
	groundGit(t, base, "init", "-q", main)
	if err := os.WriteFile(filepath.Join(main, "README.md"), []byte("fixture\n"), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	groundGit(t, main, "add", "README.md")
	groundGit(t, main, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "-m", "fixture")
	groundGit(t, main, "worktree", "add", "-q", "-b", "task/fixture-ground", ground)
	return main, ground
}

// groundGit runs one git command in dir, failing the test on error.
func groundGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// canonicalDir resolves symlinks so two spellings of one directory compare
// equal (the worktree root may live behind a symlink).
func canonicalDir(dir string) string {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return real
}

// TestReframeChildRowsSurviveTheParentGroundsDeath is the END-TO-END half: the
// real ledger rows a TOO_BIG dispatch writes must name the durable repo, and the
// ground resolver must still produce a ground for them after the parent's ground
// is gone — which is exactly what `gopi worker <child-id>` needs to reach its
// spawn.
//
// It drives the PRODUCTION path (the task tool's reframe handler), from the
// position a worker-run parent stands in (its ground), and asserts on the rows
// the ledger actually carries.
func TestReframeChildRowsSurviveTheParentGroundsDeath(t *testing.T) {
	main, ground := reframeGroundFixture(t)
	mainReal := canonicalDir(main)

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// The parent row: an epic run whose own deliverable records the repo it
	// decomposed in — the live shape (row 01a0d526).
	parentOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "parent epic", Source: taskSourceHumanTest, Repo: main,
	})
	if err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	parentID := taskIDFromSubmitOut(t, parentOut)
	// The children are claimable only once the parent is released (Rule 33);
	// walk the legal transition ladder (pending → claimed → in_progress →
	// completed).
	for _, status := range []string{tools.TaskStatusClaimed, tools.TaskStatusInProgress, tools.TaskStatusCompleted} {
		if _, err := tools.TaskUpdate(parentID, status); err != nil {
			t.Fatalf("move the parent row to %s: %v", status, err)
		}
	}

	// THE PARENT RUN'S POSITION: it stands in its GROUND (the linked worktree
	// the worker made for it), not in the main checkout.
	t.Chdir(ground)

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
		if tools.SameDir(repo, ground) {
			t.Errorf("child %s names the parent run's GROUND %s as its repo — it dies with the parent", c.TaskID, repo)
		}
		if !tools.SameDir(repo, mainReal) {
			t.Errorf("child %s repo = %q, want the durable %q", c.TaskID, repo, mainReal)
		}
	}
	if t.Failed() {
		return
	}

	// THE GROUND'S DEATH: the worker removes the parent's ground when the
	// parent finishes. The children must still resolve a runnable ground — the
	// spawn precondition `gopi worker <child-id>` needs.
	groundGit(t, main, "worktree", "remove", "--force", ground)
	if _, statErr := os.Stat(ground); !os.IsNotExist(statErr) {
		t.Fatalf("fixture: the ground %s should be gone (stat err: %v)", ground, statErr)
	}
	for _, c := range children {
		res, err := tools.TaskRunGroundFor(c.TaskID)
		if err != nil {
			t.Errorf("child %s is unrunnable after the parent ground died: %v", c.TaskID, err)
			continue
		}
		if res.Dir == "" {
			t.Errorf("child %s resolved no ground", c.TaskID)
		}
	}
}
