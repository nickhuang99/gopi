package agent

// reframe_durable_repo_resolver_test.go — the DURABLE-REPO resolution itself
// (epic 01a0d55b), split from the E2E red proof so the latter stays a pure
// pre-fix assertion.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// TestReframeChildRepoIsDurableNotTheGround: standing in a linked worktree (a
// run's ground), the resolved repo must be the DURABLE main checkout, and must
// stay a live work tree after that ground is deleted.
func TestReframeChildRepoIsDurableNotTheGround(t *testing.T) {
	main, ground := reframeGroundFixture(t)
	mainReal := canonicalDir(main)
	t.Chdir(ground)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	repo, err := tools.DurableRepoForSubmit(cwd)
	if err != nil {
		t.Fatalf("durable repo for %s: %v", cwd, err)
	}
	if tools.SameDir(repo, ground) {
		t.Fatalf("the resolved repo is the run's GROUND %s (a linked worktree that dies with the run); want the durable main checkout %s", repo, main)
	}
	if !tools.SameDir(repo, mainReal) {
		t.Fatalf("resolved repo = %q, want the main checkout %q", repo, mainReal)
	}
	if err := tools.VerifyGitWorkTree(repo); err != nil {
		t.Fatalf("the resolved repo %q is not a git work tree: %v", repo, err)
	}

	// The options builder is what dispatchReframeRoadmap submits with: the
	// durable repo must ride the options, not be re-defaulted downstream.
	opts := reframeSubmitOptions("child summary", "child payload", "parent-1",
		tools.ReframeRoadmapTag, "sess", "model", repo)
	if !tools.SameDir(opts.Repo, mainReal) {
		t.Fatalf("child submit options carry repo %q, want the durable %q", opts.Repo, mainReal)
	}
	if opts.ParentTaskID != "parent-1" || opts.Source != taskSourceLLM {
		t.Errorf("the options lost their reframe provenance: %+v", opts)
	}

	// A non-repo directory must REFUSE, never guess (the caller decides between
	// the strict and lenient faces).
	plain := t.TempDir()
	if _, err := tools.DurableRepoForSubmit(plain); err == nil {
		t.Errorf("a non-work-tree directory %s must not resolve to a repo", plain)
	}

	// The ground's death must not touch the durable answer.
	groundGit(t, main, "worktree", "remove", "--force", ground)
	if err := tools.VerifyGitWorkTree(opts.Repo); err != nil {
		t.Fatalf("after the parent ground's death the child's repo %q is no longer a work tree (the child is unrunnable): %v", opts.Repo, err)
	}
}

// TestRepairRepoMovesARowOntoALiveWorkTree: the operator's remedy (epic
// 01a0d55b) — `gopi task update <id> --repo <path>` — re-points a row's
// deliverable.repo, resolves a linked worktree to its durable main checkout, and
// clears a ground pair that belonged to the old repository.
func TestRepairRepoMovesARowOntoALiveWorkTree(t *testing.T) {
	main, ground := reframeGroundFixture(t)
	mainReal := canonicalDir(main)

	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	// A row in the DEFECT shape: deliverable = {"repo":<a directory that is not
	// a work tree>}. The row is created against a real repo (submit verifies it)
	// and then re-pointed at the dead path through the repair surface itself,
	// because that is the only writer.
	seedDir := t.TempDir()
	groundGit(t, seedDir, "init", "-q", filepath.Join(seedDir, "seed"))
	seedRepo := filepath.Join(seedDir, "seed")
	if err := os.WriteFile(filepath.Join(seedRepo, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := tools.TaskSubmit(tools.TaskSubmitOptions{Summary: "repair me", Source: taskSourceHumanTest, Repo: seedRepo})
	if err != nil {
		t.Fatalf("seed row: %v", err)
	}
	id := taskIDFromSubmitOut(t, out)
	if _, err := os.Stat(seedRepo); err != nil {
		t.Fatalf("fixture: the seed repo must exist: %v", err)
	}
	// A ground pair to be CLEARED: the row records the parent's worktree.
	line, err := tools.TaskRepointDeliverableRepo(id, ground)
	if err != nil {
		t.Fatalf("repair to the ground: %v", err)
	}
	// THE FIRST CALL IS A REPO MOVE, NOT A GROUND STAMP: the row carried the
	// SEED repo, so the row's recorded repo CHANGES — and the recorded
	// ground pair belongs to the OLD repository (the row still records the
	// parent's worktree as a dead deliverable.repo), so it is dropped in the
	// same write and the call reports the repo move.
	if !strings.Contains(line, tools.TaskShowDeliverableRepoLabel) {
		t.Errorf("a call that MOVES the row's repo must report the move; got %q", line)
	}
	if !tools.SameDir(deliverableRepoFromShow(t, id), mainReal) {
		t.Fatalf("a linked-worktree argument must resolve to the durable main checkout %q; repair said %q, row says %q",
			mainReal, line, deliverableRepoFromShow(t, id))
	}
	// The stale ground pair is GONE in the same write (it belonged to the seed
	// repo), so the row is ground-less again — the state the submit refusal's
	// door exists for. Asserted through the LABEL's presence, because `task
	// show` renders no deliverable.worktree line at all when the axis is unset
	// (a reader that cannot find its field must not read it as an empty one).
	if show, err := tools.TaskShow(id); err != nil {
		t.Fatalf("TaskShow: %v", err)
	} else if strings.Contains(show, tools.TaskShowDeliverableWorktreeLabel) {
		t.Fatalf("a repo move must DROP the ground pair recorded for the old repo (it names a worktree of a repo the row no longer points at); row still renders:\n%s", show)
	}
	// THE SECOND CALL, same argument: the repo is now the durable one, so the
	// repo axis has nothing to repair — the call reaches the GROUND ARM (epic
	// 01a131f5) and stamps the pair the argument names (worktree + the
	// registration's branch). That is the repair the submit refusal advertises,
	// and it is the answer the pre-arm verb could not give ("already names … —
	// nothing to repair" while the row kept its ground-less blob).
	stamped, err := tools.TaskRepointDeliverableRepo(id, ground)
	if err != nil {
		t.Fatalf("re-repairing the same argument must PERFORM the ground repair, not refuse: %v", err)
	}
	if !strings.Contains(stamped, tools.TaskRepairGroundStampedNote) {
		t.Errorf("the second call must report the GROUND stamp; got %q", stamped)
	}
	if got := deliverableWorktreeFromShow(t, id); !tools.SameDir(got, ground) {
		t.Fatalf("the second call must write the ground the argument names; deliverable.worktree = %q, want %q (verb said %q)", got, ground, stamped)
	}
	if got := deliverableBranchFromShow(t, id); got != "task/fixture-ground" {
		t.Fatalf("the ground stamp must carry the registration's branch; deliverable.branch = %q", got)
	}
	// Same repo a THIRD time is the satisfied state, reported (never a silent
	// ok, and never a refusal — the request already holds).
	again, err := tools.TaskRepointDeliverableRepo(id, ground)
	if err != nil {
		t.Fatalf("idempotent repair: %v", err)
	}
	if !strings.Contains(again, tools.TaskRepairGroundPresentPhrase) {
		t.Errorf("re-repairing the same repo must say so; got %q", again)
	}
	if got := deliverableWorktreeFromShow(t, id); !tools.SameDir(got, ground) {
		t.Errorf("the satisfied no-op must not move the recorded ground; row says %q", got)
	}
	// A non-repo path is refused (Rule 15).
	if _, err := tools.TaskRepointDeliverableRepo(id, t.TempDir()); err == nil {
		t.Errorf("repairing onto a non-work-tree must refuse")
	}
	// And the row is runnable again: a ground can be resolved for it.
	if _, err := tools.TaskRunGroundFor(id); err != nil {
		t.Fatalf("the repaired row must resolve a ground: %v", err)
	}
}

// deliverableWorktreeFromShow / deliverableBranchFromShow read the row's ground
// half back through the same user-facing surface deliverableRepoFromShow uses
// (`gopi task show`'s rendered lines — the text an operator and a script parse).
//
// The LABELS come from tools' own constants (Rule 1b/14), so a renderer change
// cannot leave these readers grepping an orphaned spelling. A missing line is
// the EMPTY value only when the line itself is there: an absent label is a
// failure, never a quiet "" (Rule 15 — a reader that cannot find its field must
// not be indistinguishable from a row that has no field).
func deliverableWorktreeFromShow(t *testing.T, taskID string) string {
	t.Helper()
	return deliverableLineFromShow(t, taskID, tools.TaskShowDeliverableWorktreeLabel)
}

func deliverableBranchFromShow(t *testing.T, taskID string) string {
	t.Helper()
	return deliverableLineFromShow(t, taskID, tools.TaskShowDeliverableBranchLabel)
}

func deliverableLineFromShow(t *testing.T, taskID, label string) string {
	t.Helper()
	show, err := tools.TaskShow(taskID)
	if err != nil {
		t.Fatalf("TaskShow: %v", err)
	}
	for _, line := range strings.Split(show, "\n") {
		if rest, ok := strings.CutPrefix(line, label+" "); ok {
			return strings.TrimSpace(rest)
		}
	}
	t.Fatalf("`task show` renders no %s line for %s (a reader must not read a missing field as an empty one):\n%s", label, taskID, show)
	return ""
}
