package tools

// The BINDING half of Rule 30's close-out check (epic "a dirty close-out IS not
// completed", 2026-09-16).
//
// Motivating measurement — task 01a0af50, whose close-out said the opposite of
// its own report, FOUR times:
//
//	[task-notify] run 01a0af50-5029-75ba-8bac-368ae86a3351 closed out with
//	  UNCOMMITTED TRACKED WORK in /home/user/workspace/gopi-merger-takeover-detach:
//	  worktree is DIRTY (uncommitted work) — left for a human — entries:
//	  M cmd/agent/merger_takeover.go
//	  M cmd/agent/merger_tick_no_child_gate_test.go
//	?? cmd/agent/merger_takeover_scope.go
//	?? cmd/agent/merger_takeover_scope_test.go
//	?? devdoc/merger-takeover-scope-escape.md
//	  the deliverable was never committed (commit it before reporting ready —
//	  a dirty close-out makes the branch invisible to the merge queue;
//	  the outcome stands, nothing deleted)
//
// while the same run's report read "`git status --porcelain` empty in both
// worktrees (Rule 30)". ~1000 lines of finished work (398-line impl, 332-line
// test file, 177-line devdoc) sat uncommitted and a human had to rescue it.
//
// The contract under test:
//
//   - the run's OWN recorded worktree holding a TRACKED change ⇒ the
//     `completed` writeback is REFUSED, the row stays open, and the reason names
//     the worktree and the exact entries;
//   - committing the work clears the refusal (the honest path out);
//   - a CLEAN worktree completes exactly as before;
//   - UNTRACKED-only residue completes (operator's switch unchanged) and is
//     still recorded;
//   - a worktree another row's LIVE claim holds is NEVER this run's refusal;
//   - the operator's override lets one writeback through and says so on the
//     trail;
//   - nothing is committed, stashed or deleted by the arm.

import (
	"context"
	"github.com/nickhuang99/gopi/internal/heavytest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// dirtyCloseoutFixture is the shared arrange step: a row that claimed a worktree
// (its OWN recorded ground) whose branch is checked out there.
func dirtyCloseoutFixture(t *testing.T, taskID string) (repo, wt, branch string) {
	t.Helper()
	repo, wt, branch = deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s " + taskID, Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, taskID, repo, branch, wt)
	if _, err := TaskStart(taskID, "worker-1"); err != nil {
		t.Fatal(err)
	}
	return repo, wt, branch
}

// taskRowStatus reads the row's committed status straight from the ledger (the
// writeback's return value is not the evidence — the ROW is).
func taskRowStatus(t *testing.T, taskID string) string {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id = ?`, taskID).Scan(&status); err != nil {
		t.Fatalf("read status of %s: %v", taskID, err)
	}
	return status
}

// gitStatusPorcelainFixture runs `git status --porcelain` in dir and returns its
// combined output — the Rule 30 self-check, as the test's own eyes on the tree.
func gitStatusPorcelainFixture(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git status --porcelain in %s: %v: %s", dir, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

// TestDirtyCloseoutRefusesTrackedWorkInRunsOwnWorktree is the measured case:
// a run whose OWN worktree holds an uncommitted tracked deliverable is refused
// the `completed` writeback, the row stays OPEN, the reason names the worktree
// and the exact entries, and the bytes are untouched.
func TestDirtyCloseoutRefusesTrackedWorkInRunsOwnWorktree(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-bind")
	// work.txt is COMMITTED on the fixture branch: editing it is an uncommitted
	// deliverable (the 398-line impl in the measured case). next.txt is the
	// untracked half of the same close-out (the devdoc/test files).
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "next.txt"), []byte("new file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := TaskRunWriteback("t-bind", TaskStatusCompleted, "done — all green")
	if err == nil {
		t.Fatalf("UNCOMMITTED DELIVERABLE: the run closed out `%s` with a tracked change still "+
			"uncommitted in its OWN worktree (%s) — the branch is then invisible to every merge path",
			TaskStatusCompleted, wt)
	}
	// Acceptance 4: the refusal names the worktree path AND the exact file list.
	for _, want := range []string{reclaimSkipDirtyReason, wt, "work.txt", "next.txt", TaskStatusCompleted} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q — got %q", want, err.Error())
		}
	}
	// The row stays OPEN: "work exists but was not committed" is exactly the
	// state a human must be able to see in the queue.
	if got := taskRowStatus(t, "t-bind"); got == TaskStatusCompleted {
		t.Fatalf("a refused writeback must leave the row open, got %q", got)
	}
	// Nothing is deleted, nothing is committed: the deliverable is still on disk,
	// byte for byte, and git still reports it as uncommitted work.
	if b, rerr := os.ReadFile(filepath.Join(wt, "work.txt")); rerr != nil || !strings.Contains(string(b), "real edit") {
		t.Fatalf("the refused arm must not touch the work: %v %q", rerr, string(b))
	}
	if out := gitStatusPorcelainFixture(t, wt); !strings.Contains(out, "work.txt") {
		t.Fatalf("the work must still read as uncommitted (no auto-commit): %q", out)
	}
	// The trail carries BOTH facts in distinct, greppable events.
	if taskEventDetailFor(t, "t-bind", reclaimRunResidueDirtyEvent) == "" {
		t.Errorf("the shape must still be RECORDED under %q", reclaimRunResidueDirtyEvent)
	}
	refusal := taskEventDetailFor(t, "t-bind", reclaimRunTrackedDirtRefusalEvent)
	if !strings.Contains(refusal, wt) || !strings.Contains(refusal, "work.txt") {
		t.Errorf("the refusal must be a ledger fact naming the ground and the entries, got %q", refusal)
	}
}

// TestDirtyCloseoutRefusalIsClearedByCommittingTheWork is the honest way out and
// the normal path: the SAME row, the SAME worktree, one commit later, closes out
// `completed` with no gate firing.
func TestDirtyCloseoutRefusalIsClearedByCommittingTheWork(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-committed")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("deliverable\nreal edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-committed", TaskStatusCompleted, "premature"); err == nil {
		t.Fatal("the gate must fire before the work is committed (otherwise this test proves nothing)")
	}
	gitRun(t, wt, "add", "work.txt")
	gitRun(t, wt, "commit", "-m", "the deliverable, committed by its author")
	if _, err := TaskRunWriteback("t-committed", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("a committed worktree must close out cleanly, got %v", err)
	}
	if got := taskRowStatus(t, "t-committed"); got != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", got, TaskStatusCompleted)
	}
}

// TestDirtyCloseoutCleanWorktreeStillCompletes is acceptance 2: the gate must not
// fire on the ordinary path.
func TestDirtyCloseoutCleanWorktreeStillCompletes(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-clean")
	if out := gitStatusPorcelainFixture(t, wt); strings.TrimSpace(out) != "" {
		t.Fatalf("the fixture ground must start clean, got %q", out)
	}
	if _, err := TaskRunWriteback("t-clean", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("a CLEAN worktree must still report %s, got %v", TaskStatusCompleted, err)
	}
	if got := taskRowStatus(t, "t-clean"); got != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", got, TaskStatusCompleted)
	}
}

// TestDirtyCloseoutUntrackedResidueStillCompletes is acceptance 3's residue
// class: the tracked gate must not swallow the untracked shape, whose handling
// stays exactly as it was (recorded; refusal only under the operator's switch).
func TestDirtyCloseoutUntrackedResidueStillCompletes(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-residue")
	if err := os.WriteFile(filepath.Join(wt, "build.out"), []byte("test residue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-residue", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("untracked-only residue must keep today's handling (record, not the tracked gate): %v", err)
	}
	if got := taskRowStatus(t, "t-residue"); got != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", got, TaskStatusCompleted)
	}
	if taskEventDetailFor(t, "t-residue", reclaimRunResidueEvent) == "" {
		t.Errorf("the untracked shape must still be RECORDED under %q", reclaimRunResidueEvent)
	}
}

// TestDirtyCloseoutNeverFiresForASiblingRunsLiveWork is the epic's sibling class:
// a worktree another row's LIVE claim holds is that row's work, so the closing
// run is not refused for it (and its honest `completed` stands).
func TestDirtyCloseoutNeverFiresForASiblingRunsLiveWork(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionDeviceExec)
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "sibling", Payload: "p", TaskID: "t-sib", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-sib", repo, branch, wt)
	// A LIVE claim on the SAME directory: this process is provably alive, and it
	// stands in for a sibling run that is still working there.
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET claimed_by = ? WHERE task_id = ?`,
		hostPid(t, os.Getpid()), "t-sib"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the seed ledger: %v", err)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "closing", Payload: "p", TaskID: "t-close", Source: "human"}); err != nil {
		t.Fatal(err)
	}
	seedDeliverable(t, "t-close", repo, branch, wt)
	if _, err := TaskStart("t-close", "worker-1"); err != nil {
		t.Fatal(err)
	}
	// An uncommitted tracked change that is the SIBLING's, not ours.
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("sibling's live edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskRunWriteback("t-close", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("another row's LIVE work must never be this run's refusal, got %v", err)
	}
	if got := taskRowStatus(t, "t-close"); got != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", got, TaskStatusCompleted)
	}
}

// TestDirtyCloseoutOverrideLetsTheWritebackThroughAndSaysSo: the operator's
// reviewed escape hatch — the writeback proceeds AND the trail distinguishes
// "recorded" from "recorded and deliberately let through".
func TestDirtyCloseoutOverrideLetsTheWritebackThroughAndSaysSo(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-override")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("reviewed by a human\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(reclaimEnvDirtyCloseout, reclaimEnvDirtyCloseoutOn)
	if _, err := TaskRunWriteback("t-override", TaskStatusCompleted, "done"); err != nil {
		t.Fatalf("the operator's override must let this writeback through, got %v", err)
	}
	if got := taskRowStatus(t, "t-override"); got != TaskStatusCompleted {
		t.Fatalf("status = %q, want %q", got, TaskStatusCompleted)
	}
	if taskEventDetailFor(t, "t-override", reclaimRunTrackedDirtOverrideEvent) == "" {
		t.Fatalf("an overridden dirty close-out must say so on the trail (event %q)", reclaimRunTrackedDirtOverrideEvent)
	}
	if taskEventDetailFor(t, "t-override", reclaimRunResidueDirtyEvent) == "" {
		t.Errorf("the shape is still RECORDED even when the gate is lifted")
	}
}

// TestDirtyCloseoutGateNeverCommitsStashesOrDeletes closes the boundary the epic
// states first: the gate refuses the STATUS claim, it never launders the bytes.
//
// The predicate is the ROOT-CAUSE shape of the ban, not an English-word scan: a
// git mutation (commit / stash / push / reset / checkout / restore / add) or any
// file deletion REQUIRES the arm to act on the world — to spawn a process or to
// unlink a path. The arm contains NEITHER (it measures through the shared
// classifier's own helper, defined outside this region), so "no `exec.Command`,
// no `os.Remove*`, no `--force`" is the invariant that cannot be satisfied while
// auto-committing, and it cannot be tripped by the arm's own prose telling the
// operator to commit their work (a wording scan would have to either miss the
// real risk or fire on the remedy's name — Rule 13 wants the check that closes
// the hole, not one that argues about vocabulary).
func TestDirtyCloseoutGateNeverCommitsStashesOrDeletes(t *testing.T) {
	b, err := os.ReadFile("task_reclaim.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(b)
	start := strings.Index(src, reclaimRunResidueArmMarker)
	end := strings.Index(src, "// reclaimGitRemove runs the ordinary")
	if start < 0 || end <= start {
		t.Fatalf("the enforcement arm's boundaries moved — update this gate rather than deleting it")
	}
	arm := stripGoLineComments(src[start:end])
	for _, banned := range []string{
		"exec.Command",    // every git mutation (and every other side effect) needs a subprocess
		"os.StartProcess", // the same, without exec.Command
		"os.Remove",       // no deletion of a run's scratch or work
		"os.RemoveAll",    // (subsumed by os.Remove, kept for the reader)
		"--force",         // the guard this whole epic may never weaken
	} {
		if strings.Contains(arm, banned) {
			t.Errorf("the close-out arm must never carry %q — it REFUSES the status claim, it never "+
				"touches the tree (Rule 13)", banned)
		}
	}
	// The gate is not vacuous: the region really does hold the binding arm.
	if !strings.Contains(arm, "reclaimRunTrackedDirtRefusal") {
		t.Fatalf("the scanned region must contain the binding gate — the marker moved")
	}
}

// TestDirtyCloseoutGateGuardsTheVerifierStampToo: the SECOND delivery claim is
// the verifier's `verified`. The gate reads ONE list of delivery claims
// (taskTerminalClaimStatuses), so a dirty row cannot be handed a clean bill of
// health by the verify loop after having escaped the writeback door.
func TestDirtyCloseoutGateGuardsTheVerifierStampToo(t *testing.T) {
	_, wt, _ := dirtyCloseoutFixture(t, "t-verified")
	if err := os.WriteFile(filepath.Join(wt, "work.txt"), []byte("never committed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The row is already `completed` (e.g. a legacy row, or an operator's manual
	// move that predates the gate) — the peer move completed→verified is legal,
	// and it is the move the gate must refuse.
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE task_id = ?`,
		TaskStatusCompleted, "t-verified"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close the seed ledger: %v", err)
	}
	_, err := TaskUpdate("t-verified", TaskStatusVerified)
	if err == nil {
		t.Fatalf("the %s claim must be refused for a dirty own worktree too — a clean bill of "+
			"health for uncommitted work is the same defect one door later", TaskStatusVerified)
	}
	if !strings.Contains(err.Error(), reclaimSkipDirtyReason) || !strings.Contains(err.Error(), wt) {
		t.Errorf("the refusal must name the classifier's wording and the worktree, got %q", err.Error())
	}
	if got := taskRowStatus(t, "t-verified"); got != TaskStatusCompleted {
		t.Errorf("the refused move must leave the row where it was, got %q", got)
	}
}
