package agent

// cloneground_owner_test.go — the ADOPTION seam's arm of ONE-GROUND-ONE-OWNING-ROW.
//
// THE DEFECT THESE TESTS CLOSE (measured 2026-11-01, rows 01a19648-4fbe →
// 01a196bf-8cbb). A read-only question-and-answer run was started from the bash
// tool of a RUNNING run. `gopi --prompt` inherits the caller's environment, so
// the child saw the parent's GOPI_RUN_GROUND, treated it as "the runner
// announced this ground to me", chdir'd into the parent's workspace, and had
// the parent's directory and branch stamped onto its own row:
//
//	$ grep -o '"workdir":"[^"]*"' ~/.gopi/sessions/d55a93dc-…/session.jsonl
//	"workdir":"/home/nick/workspace/01a19648-4fbe-72ea-b3e9-02b219aaa4bf"
//	$ head -2 ~/.gopi/sessions/d55a93dc-…/console.log | tail -1
//	[task-notify] task 01a196bf-8cbb-…: deliverable.worktree backfilled to
//	"/home/nick/workspace/01a19648-4fbe-…" (branch "task/01a196484fbe…") at claim
//
// Its close-out then reported `clone channel: delivery did not land` against a
// dirty tree its neighbour was still editing — a tree it never touched.
//
// THE FIX THESE ARMS PIN: the announcement is a CLAIM, and a claim is checked
// against the ledger BEFORE any chdir. A ground another LIVE row names is that
// row's; the child reports it and opens its own workspace instead.
//
// The positive control matters as much as the refusal: a run handed a ground
// whose previous holder FINISHED must still adopt it (the shape
// TestBarePromptCloneChannelGroundsTheRunInTheCloneWorkspace pins from the other
// side), so the guard is about OWNERSHIP, never about announcements in general.

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

// cloneOwnerGit runs one git command against dir and returns its combined
// output; a failure is the TEST's failure (a broken fixture must never be read
// as a product verdict).
func cloneOwnerGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// testutil.GitEnv keeps the fixture's child from inheriting the real
	// repository routing (Rule 13/15; TestGate9GitEnvRedProof).
	cmd.Env = testutil.GitEnv(
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=ground owner test", "GIT_AUTHOR_EMAIL=owner@test.invalid",
		"GIT_COMMITTER_NAME=ground owner test", "GIT_COMMITTER_EMAIL=owner@test.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// cloneOwnerWorkspace clones src into <root>/<taskID> and stands it on the
// branch TaskGroundBranchName derives for that id — the shape the clone channel
// produces for a run.
func cloneOwnerWorkspace(t *testing.T, src, root, taskID string) (dir, branch string) {
	t.Helper()
	dir = filepath.Join(root, taskID)
	cloneOwnerGit(t, root, "clone", "-q", src, dir)
	branch = tools.TaskGroundBranchName(taskID)
	cloneOwnerGit(t, dir, "checkout", "-q", "-b", branch)
	return dir, branch
}

// cloneOwnerLogAgent is newAutoTaskAgent with its channel lines CAPTURED, so a
// test can assert what the run told its operator (Rule 15: a refusal that is
// not reported is indistinguishable from a run that never considered the
// question).
func cloneOwnerLogAgent(t *testing.T) (*Agent, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	a := newAutoTaskAgent(t)
	a.Logger = log.New(buf, "", 0)
	return a, buf
}

// TestAnnouncedGroundHeldByAnotherLiveRowIsNotAdopted is the RED→GREEN arm.
//
// Pre-fix this failed at the cwd assertion: the run chdir'd into the other
// row's workspace and the ledger stamped it as this run's ground.
func TestAnnouncedGroundHeldByAnotherLiveRowIsNotAdopted(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	a, chanLog := cloneOwnerLogAgent(t)

	// THE OTHER RUN: a live row that already owns a clone workspace. Built
	// through the REAL writer, so the fixture cannot describe a shape the
	// product cannot produce.
	ownerOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "a live run editing the tree", Source: taskSourceHumanTest, Repo: src,
	})
	if err != nil {
		t.Fatalf("seed the owning row: %v", err)
	}
	ownerID := taskIDFromSubmitOut(t, ownerOut)
	announced, ownerBranch := cloneOwnerWorkspace(t, src, root, ownerID)
	if _, err := tools.TaskRunGroundForClone(ownerID, announced); err != nil {
		t.Fatalf("the owner records its own ground: %v", err)
	}

	// THE INHERITING RUN: announced a ground that is NOT its own.
	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, announced)
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.plainPrompt = true

	a.maybeRegisterSessionTask("谁写的这几行？只回答结论，不要改文件。")
	if !a.autoTaskRegistered {
		t.Fatalf("the start hook did not register a row")
	}
	if a.autoTaskRowID == ownerID {
		t.Fatalf("fixture collapsed: the run registered the owner's own id %s", ownerID)
	}
	a.adoptCloneGround(a.autoTaskRowID)

	// ① THE RUN DOES NOT STAND IN THE OTHER ROW'S TREE. That chdir is what made
	// its close-out publish from a tree it did not own.
	if cwd, err := os.Getwd(); err != nil || tools.SameDir(cwd, announced) {
		t.Fatalf("cwd = %q (err %v) — a run must not stand in a workspace another LIVE row owns (%q)", cwd, err, announced)
	}
	// ② THE LEDGER DOES NOT NAME IT. Two rows naming one directory is the state
	// that blocks both rows' recycle and puts two owners on one tree.
	if wt, _ := tools.TaskDeliverableWorktreeFor(a.autoTaskRowID); tools.SameDir(wt, announced) {
		t.Fatalf("row ground = %q, want a workspace of its own — %q belongs to row %s", wt, announced, ownerID)
	}
	// ③ THE OWNER KEEPS ITS GROUND. A refusal must not cost the other run the
	// directory it is working in.
	if wt, br := tools.TaskDeliverableWorktreeFor(ownerID); !tools.SameDir(wt, announced) || br != ownerBranch {
		t.Fatalf("the owner's ground became (%q, %q) — a refused adoption must leave the owner untouched", wt, br)
	}
	// ④ AND IT SAYS SO, naming the row that holds it (Rule 15: no silent failure).
	if !strings.Contains(chanLog.String(), ownerID) {
		t.Fatalf("the refusal must name the owning row %s so a reader can follow it:\n%s", ownerID, chanLog.String())
	}
}

// TestAnnouncedGroundOfAFinishedRowIsStillAdoptable is the direction the rule
// must NOT over-reach in: a ground whose previous holder FINISHED is not
// "another live row's", and a run handed it must take it. Without this arm a
// guard that refused any named directory would break every retry round.
func TestAnnouncedGroundOfAFinishedRowIsStillAdoptable(t *testing.T) {
	src := cloneGroundFixtureRepo(t)
	root := t.TempDir()
	before, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cloneGroundRestoreCWD(t, before)

	a, _ := cloneOwnerLogAgent(t)

	ownerOut, err := tools.TaskSubmit(tools.TaskSubmitOptions{
		Summary: "the run that already finished here", Source: taskSourceHumanTest, Repo: src,
	})
	if err != nil {
		t.Fatalf("seed the finished row: %v", err)
	}
	ownerID := taskIDFromSubmitOut(t, ownerOut)
	announced, _ := cloneOwnerWorkspace(t, src, root, ownerID)
	if _, err := tools.TaskRunGroundForClone(ownerID, announced); err != nil {
		t.Fatalf("the previous holder records its ground: %v", err)
	}
	for _, status := range []string{tools.TaskStatusClaimed, tools.TaskStatusInProgress, tools.TaskStatusCompleted} {
		if _, err := tools.TaskUpdate(ownerID, status); err != nil {
			t.Fatalf("close the previous holder (%s): %v", status, err)
		}
	}

	t.Setenv(config.DevRepoEnvName, src)
	t.Setenv(config.WorkspaceRootEnvName, root)
	t.Setenv(tools.TaskGroundDirEnv, announced)
	t.Setenv(tools.TaskGroundBranchEnv, "")
	a.plainPrompt = true

	a.maybeRegisterSessionTask("继续在这块地上干")
	if !a.autoTaskRegistered {
		t.Fatalf("the start hook did not register a row")
	}
	a.adoptCloneGround(a.autoTaskRowID)

	if workspace, _ := a.CloneRunGround(); !tools.SameDir(workspace, announced) {
		t.Fatalf("ground = %q, want the announced %q — a ground whose holder FINISHED is free for the next run", workspace, announced)
	}
	if wt, _ := tools.TaskDeliverableWorktreeFor(a.autoTaskRowID); !tools.SameDir(wt, announced) {
		t.Fatalf("row ground = %q, want %q", wt, announced)
	}
}
