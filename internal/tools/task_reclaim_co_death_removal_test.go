package tools

// Regression guards for the "the reason claims a result the pass does not hold"
// defect (epic 01a17e1e, second instance of the family fixed in 01a17dd0).
//
// THE DEFECT. reclaimDeadHolderCoDeath writes the row's reason BEFORE the
// removal runs, and that reason ended with "…CLOSED the attempt as failed and
// freed the directory (branch kept for merge)". The removal is a LATER step and
// it can fail — the epic's own instance is a row whose `deliverable.repo` and
// `deliverable.worktree` are the SAME path (the clone ground: `task_submit`
// stamps cwd, GOPI.md §4.8.2), so the disposition judges the clone FREABLE (its
// own `git worktree list` lists itself) and `git worktree remove <the clone>`
// exits 128 `is a main working tree`. The row then carries, durably and
// append-only, "freed the directory" while the directory is still there — which
// misinforms reconcile and the operator about safe-merge gate [c], the very
// gate the reclaim exists to unblock.
//
// WHAT IS PINNED HERE, both through the REAL pass and the REAL ledger:
//
//	① the reason states only what the pass already HOLDS (the row was closed,
//	  the branch is kept) — never the directory's fate;
//	② the removal's OWN outcome is recorded AFTER it ran, under
//	  reclaimGroundOutcomeEvent, which `task show` renders (its name is in
//	  verdictTrailEventNames) — so a failed removal is not a single stderr line
//	  belonging to a process that already exited;
//	③ the co-death GUARD IS UNTOUCHED: the status transition still happens
//	  before the removal, even when the removal then fails (asserted, because a
//	  "fix" that made the removal a precondition of the close would reverse the
//	  2026-10-12 contract instead of correcting its wording).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/devworkspace"
)

// reclaimCoDeathCloneGroundFixture builds the epic's exact shape: a real origin
// repository, a real CLONE of it standing in for the run's ground, and a row
// whose deliverable names that clone as BOTH its repo and its worktree — the
// stamp `task_submit` produces from cwd.
//
// The clone is measured with the repository's ONE shape discriminator
// (devworkspace.GroundModelOf), so the test fails loudly if the fixture ever
// stops being the clone shape it claims to be.
func reclaimCoDeathCloneGroundFixture(t *testing.T, taskID string) (ground, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed — the clone-ground shape cannot be modelled")
	}
	if _, err := os.Stat("/proc"); err != nil {
		t.Skip("/proc is unavailable — the dead-holder shape cannot be modelled")
	}
	t.Setenv(memoryDBEnv, filepath.Join(t.TempDir(), "memory.db"))
	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	ground = filepath.Join(root, "ground")
	gitRun(t, root, "init", origin)
	gitRun(t, origin, "config", "user.email", "t@t")
	gitRun(t, origin, "config", "user.name", "t")
	gitRun(t, origin, "checkout", "-b", IntegrationBranchName)
	if err := os.WriteFile(filepath.Join(origin, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, origin, "add", "a.txt")
	gitRun(t, origin, "commit", "-m", "init")
	gitRun(t, root, "clone", origin, ground)
	branch = IntegrationBranchName
	if got := devworkspace.GroundModelOf(ground); got != devworkspace.GroundModelClone {
		t.Fatalf("fixture: %s must be a CLONE ground (.git is a directory), GroundModelOf said %q", ground, got)
	}
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "co-death reason vs a failed removal", Payload: "p", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	seedDeadHolderRow(t, taskID, taskStatusInProgress, ground, branch, ground, hostPid(t, deadPID(t)))
	return ground, branch
}

// reclaimTrailEvents reads one row's trail rows for ONE event name, oldest
// first, through the ledger resolver the arm itself wrote through (Rule 32/14).
func reclaimTrailEvents(t *testing.T, taskID, event string) []string {
	t.Helper()
	db, err := openTasksDB(t.Context(), os.Getenv(memoryDBEnv))
	if err != nil {
		t.Fatalf("open the ledger: %v", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(t.Context(),
		`SELECT detail FROM task_events WHERE task_id = ? AND event = ? ORDER BY id`, taskID, event)
	if err != nil {
		t.Fatalf("read %s events for %s: %v", event, taskID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatalf("scan %s event for %s: %v", event, taskID, err)
		}
		out = append(out, detail)
	}
	return out
}

// TestCoDeathReasonNeverClaimsAGroundItFailedToFree is the defect itself, at the
// smallest sample the epic measured: the removal is ATTEMPTED and it FAILS (git
// refuses to remove a main working tree), while the row is closed by co-death.
func TestCoDeathReasonNeverClaimsAGroundItFailedToFree(t *testing.T) {
	const taskID = "t-co-death-clone-ground"
	ground, branch := reclaimCoDeathCloneGroundFixture(t, taskID)
	freshReclaimRefusalMemo(t)

	if _, err := os.Stat(ground); err != nil {
		t.Fatalf("fixture: the ground must exist before the pass: %v", err)
	}
	if got := mustTaskStatus(t, taskID); got != taskStatusInProgress {
		t.Fatalf("fixture: the row must start live, got %q", got)
	}

	var lines []string
	_ = captureStderrDuring(t, func() { lines = ReclaimDeadHolderWorktrees(true) })
	joined := strings.Join(lines, "\n")

	// THE PRECONDITION THE ASSERTIONS BELOW DEPEND ON: the removal really was
	// attempted and really failed. A pass that refused this row earlier would
	// make ① and ② true for the wrong reason.
	if _, err := os.Stat(ground); err != nil {
		t.Fatalf("precondition: this fixture measures a FAILED removal, so the clone ground must still exist "+
			"(stat err=%v); pass lines:\n%s", err, joined)
	}
	if got := mustTaskStatus(t, taskID); got != taskStatusFailed {
		t.Fatalf("precondition: co-death must still close the attempt (the guard is not weakened by this fix), "+
			"got status %q; pass lines:\n%s", got, joined)
	}

	// ① THE DEFECT: a durable, append-only sentence claiming a result this pass
	// never obtained. `gopi task show` renders the reason, so an operator reads
	// "freed the directory" while `git worktree list` still names it.
	why := reclaimRowReason(t, taskID)
	if strings.Contains(why, reclaimCoDeathFreedClaim) {
		t.Fatalf("the row's reason must not claim the ground was freed — the removal FAILED and the directory "+
			"is still at %s. The reason may state only what the pass already holds (the row was closed as "+
			"failed, the branch is kept). Got reason:\n%s", ground, why)
	}

	// ② THE RECORD: the removal's own outcome, written AFTER it ran, on the
	// surface `task show` reads (Rule 15 — a failure must not live only in a
	// stderr line belonging to a process that has already exited).
	outcomes := reclaimTrailEvents(t, taskID, reclaimGroundOutcomeEvent)
	if len(outcomes) != 1 {
		t.Fatalf("the failed removal must leave exactly ONE %q record on the row's trail, got %d: %v",
			reclaimGroundOutcomeEvent, len(outcomes), outcomes)
	}
	if !strings.Contains(outcomes[0], ground) || !strings.Contains(outcomes[0], reclaimGroundNotFreedMarker) {
		t.Errorf("the trail record must name the directory that survived and the fact that it was NOT freed; got:\n%s",
			outcomes[0])
	}
	if !taskShowVerdictEventName(reclaimGroundOutcomeEvent) {
		t.Errorf("%q is not in verdictTrailEventNames, so `task show` would not render the removal outcome at all",
			reclaimGroundOutcomeEvent)
	}

	// ③ the report line is honest too: it must not read as a freeing.
	for _, line := range lines {
		if strings.Contains(line, taskID) && !strings.Contains(line, reclaimGroundNotFreedMarker) {
			t.Errorf("the pass reported this row without naming the failed removal: %q", line)
		}
	}

	// The branch survives (the deliverable is not destroyed by a failed removal).
	if out, err := exec.Command("git", "-C", ground, "rev-parse", "--verify", branch).Output(); err != nil {
		t.Errorf("the branch %s must survive a failed removal: %v", branch, err)
	} else if strings.TrimSpace(string(out)) == "" {
		t.Errorf("the branch %s must still resolve after the failed removal", branch)
	}
}

// TestCoDeathReasonOnASuccessfulRemovalNamesTheFreedDirectory is the other half
// of the split: the directory-freed FACT is recorded — but only after the
// removal really ran, and as its own record rather than inside the reason.
func TestCoDeathReasonOnASuccessfulRemovalNamesTheFreedDirectory(t *testing.T) {
	const taskID = "t-co-death-freed-ground"
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "co-death reason vs a successful removal", Payload: "p", TaskID: taskID, Source: "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	seedDeadHolderRow(t, taskID, taskStatusInProgress, repo, branch, wt, hostPid(t, deadPID(t)))
	freshReclaimRefusalMemo(t)

	lines := ReclaimDeadHolderWorktrees(true)
	joined := strings.Join(lines, "\n")

	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("precondition: the ground must be freed by this pass (stat err=%v); pass lines:\n%s", err, joined)
	}
	// The reason still must not be the place the freeing is claimed — it is
	// written before the removal, so it cannot know.
	if why := reclaimRowReason(t, taskID); strings.Contains(why, reclaimCoDeathFreedClaim) {
		t.Errorf("the reason is written BEFORE the removal and may not assert its outcome; got:\n%s", why)
	}
	outcomes := reclaimTrailEvents(t, taskID, reclaimGroundOutcomeEvent)
	if len(outcomes) != 1 {
		t.Fatalf("a successful removal must leave exactly ONE %q record, got %d: %v",
			reclaimGroundOutcomeEvent, len(outcomes), outcomes)
	}
	if !strings.Contains(outcomes[0], wt) || !strings.Contains(outcomes[0], reclaimGroundFreedMarker) {
		t.Errorf("the trail record must name the freed directory and the fact that it is gone; got:\n%s", outcomes[0])
	}
}
