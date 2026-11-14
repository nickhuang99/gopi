package tools

// Claim-holder freshness + dead-worktree reclaim tests (epic 2026-09-12).
//
// TWO coupled defects, one file:
//
//	(1) the claim STAMP — a row's claimed_by ("host:pid") is written by the
//	    claim ladder (pending→claimed CAS, crash-recovery re-stamp), not by
//	    the process executing the row, and TaskStart used to DISCARD its `by`
//	    argument entirely. An in_progress row could therefore carry a dead
//	    holder's pid, and the zombie chain's pid layer read that pid as a LIVE
//	    holder whose veto suppressed the stale event forever.
//	(2) the DEAD WORKTREE HOLDER — the same dead holder leaves its worktree
//	    behind; scripts/safe-merge.sh gate [c] refuses a worktree-occupied
//	    branch and the merger's protected arm skips it silently, so the
//	    finished deliverable never lands.
//
// The tests below are deterministic (no daemon, no LLM): the ledger is a
// sandboxed sqlite file, "dead" pids are real killed children, and the one
// git fixture is a local temp repo.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
)

// holderSeedRow submits a row and moves it to in_progress through the REAL
// start transition, then rewrites the claim stamp with raw SQL to the
// wrong/dead-holder shape under test. Raw SQL is the only way to forge it —
// that is the point of defect (1): the API cannot produce the shape any more,
// so the fixture has to step behind it.
func holderSeedRow(t *testing.T, taskID, claimedBy string, claimedAt, inProgressAt float64) *Task {
	t.Helper()
	if _, err := TaskSubmit(TaskSubmitOptions{
		Summary: "holder fixture " + taskID,
		Payload: "claim-holder fixture",
		TaskID:  taskID,
		Source:  "human",
	}); err != nil {
		t.Fatalf("submit %s: %v", taskID, err)
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+` = ?, updated_at = ? WHERE task_id = ?`,
		taskStatusInProgress, claimedBy, claimedAt, inProgressAt, inProgressAt, taskID); err != nil {
		t.Fatalf("forge %s: %v", taskID, err)
	}
	row, err := taskByID(ctx, db, taskID)
	if err != nil || row == nil {
		t.Fatalf("re-read %s: %v", taskID, err)
	}
	return row
}

// hostPid stamps "hostname:pid" for THIS host — the shape claimHolder can
// resolve locally (a foreign-host stamp degrades to unverified, a different
// axis that already had its own tests).
func hostPid(t *testing.T, pid int) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	return host + ":" + itoa(pid)
}

// TestTaskStartStampsTheExecutingProcess is defect (1)'s primary assertion:
// TaskStart's `by` argument is no longer discarded — the row's holder is the
// process named by the caller, written ATOMICALLY with claimed→in_progress.
// Before the fix the stamp below stayed "the-claim-ladder" (TaskStart called
// TaskUpdate and dropped `by` on the floor).
func TestTaskStartStampsTheExecutingProcess(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ctx := context.Background()

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-start-stamp", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// The claim ladder's stamp — what the row carries before the start.
	if err := TaskClaimExplicit("t-start-stamp", "the-claim-ladder:12345"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-start-stamp", "the-executor:777"); err != nil {
		t.Fatalf("start: %v", err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, "t-start-stamp")
	if err != nil || row == nil {
		t.Fatalf("re-read: %v", err)
	}
	if row.Status != taskStatusInProgress {
		t.Fatalf("status = %q, want in_progress", row.Status)
	}
	if row.ClaimedBy != "the-executor:777" {
		t.Errorf("claimed_by = %q, want the EXECUTOR's stamp (TaskStart used to discard `by`)", row.ClaimedBy)
	}
	if !row.HasInProgressAt {
		t.Fatalf("the start transition must stamp %s — the claim stamp's referent", tasksInProgressAtColumnName)
	}
	if row.HasClaim && row.ClaimedAt < row.InProgressAt-claimStampSkewSec {
		t.Errorf("the holder stamp must be written WITH the transition (claimed_at=%v < in_progress_at=%v)",
			row.ClaimedAt, row.InProgressAt)
	}
	if got := claimStampFreshness(row); got != claimStampFresh {
		t.Errorf("freshness = %q, want %q (the executor's own stamp)", got, claimStampFresh)
	}
}

// TestTaskStartEmptyHolderFallsBackToThisProcess: an empty `by` is the
// auto-register shape — the row must still name the process that will execute
// it, never a blank stamp (a blank stamp is what made auto rows "suspected
// forever" before the self-claim stamp existed).
func TestTaskStartEmptyHolderFallsBackToThisProcess(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ctx := context.Background()
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-start-blank", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if err := TaskClaimExplicit("t-start-blank", ""); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := TaskStart("t-start-blank", ""); err != nil {
		t.Fatalf("start: %v", err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, "t-start-blank")
	if err != nil || row == nil {
		t.Fatalf("re-read: %v", err)
	}
	if _, pid, ok := claimHolder(row.ClaimedBy); !ok || pid != os.Getpid() {
		t.Errorf("claimed_by = %q, want this process (pid %d) as the executor", row.ClaimedBy, os.Getpid())
	}
}

// TestTaskStampSelfClaimMovesTheReferentWithTheStamp: the auto-register
// resume path must not write a holder stamp OLDER than the row's
// in_progress_at — both come from one `now`, so the freshness axis cannot
// misread the row as "a previous claimant".
func TestTaskStampSelfClaimMovesTheReferentWithTheStamp(t *testing.T) {
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	ctx := context.Background()
	// A row born in_progress long ago (the auto-register shape), whose stale
	// referent would classify the fresh self-claim stamp as stale.
	holderSeedRow(t, "t-selfclaim", "old-holder:1", 1000, 1000)
	if err := TaskStampSelfClaim("t-selfclaim"); err != nil {
		t.Fatalf("self-claim: %v", err)
	}
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	row, err := taskByID(ctx, db, "t-selfclaim")
	if err != nil || row == nil {
		t.Fatalf("re-read: %v", err)
	}
	if _, pid, ok := claimHolder(row.ClaimedBy); !ok || pid != os.Getpid() {
		t.Fatalf("claimed_by = %q, want this process", row.ClaimedBy)
	}
	if got := claimStampFreshness(row); got != claimStampFresh {
		t.Errorf("freshness = %q, want %q — the resume path must move the referent with its stamp", got, claimStampFresh)
	}
}

// TestClaimStampFreshness pins the three-way classifier at its boundaries:
// the normal claim→start pair stays FRESH (the claim precedes the start by
// milliseconds — requiring stamp >= referent exactly would call the healthy
// shape stale), a stamp more than the skew older is STALE, and a row without
// a referent (legacy NULL) or without a stamp is UNKNOWN.
func TestClaimStampFreshness(t *testing.T) {
	host := hostPid(t, os.Getpid())
	cases := []struct {
		name string
		row  *Task
		want string
	}{
		{"claim with the start (same instant)", &Task{ClaimedBy: host, ClaimedAt: 1000, HasClaim: true, InProgressAt: 1000, HasInProgressAt: true}, claimStampFresh},
		{"claim inside the skew (normal claim→start race)", &Task{ClaimedBy: host, ClaimedAt: 995, HasClaim: true, InProgressAt: 1000, HasInProgressAt: true}, claimStampFresh},
		{"claim just past the skew", &Task{ClaimedBy: host, ClaimedAt: 994, HasClaim: true, InProgressAt: 1000, HasInProgressAt: true}, claimStampStale},
		{"claim long before the start (the dead wrapper)", &Task{ClaimedBy: host, ClaimedAt: 100, HasClaim: true, InProgressAt: 900_000, HasInProgressAt: true}, claimStampStale},
		{"no referent (legacy row)", &Task{ClaimedBy: host, ClaimedAt: 100, HasClaim: true}, claimStampUnknown},
		{"no claim stamp", &Task{InProgressAt: 900_000, HasInProgressAt: true}, claimStampUnknown},
		{"neither", &Task{}, claimStampUnknown},
	}
	for _, tc := range cases {
		if got := claimStampFreshness(tc.row); got != tc.want {
			t.Errorf("%s: freshness = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestPidIsZombieIsDead pins the Z-state layer on a REAL unreaped child: a
// process that exited but whose parent has not waited is state 'Z', passes
// signal 0, and must still be reported DEAD. Without this, a SIGKILLed
// wrapper (which is exactly its own child's unreaped zombie) reads as a live
// holder and vetoes every stale verdict AND every close-out.
func TestPidIsZombieIsDead(t *testing.T) {
	pid := spawnZombieChild(t)
	if !pidIsZombie(pid) {
		t.Fatalf("pid %d must read as a zombie before it is reaped", pid)
	}
	if pidAlive(pid) {
		t.Fatalf("a zombie-state pid %d must report DEAD (it does no work)", pid)
	}
	reapChild()
	if pidIsZombie(pid) {
		t.Errorf("after the reap pid %d must no longer be a zombie", pid)
	}
}

// TestPidIsZombieOnARunningProcess: the probe must not misreport a WORKING
// process (its own parent, here) as a zombie — a false positive would disable
// the live veto for every healthy run.
func TestPidIsZombieOnARunningProcess(t *testing.T) {
	if pidIsZombie(os.Getpid()) {
		t.Fatalf("this process is running, not a zombie")
	}
	if !pidAlive(os.Getpid()) {
		t.Fatalf("this process must report alive")
	}
	if pidIsZombie(0) || pidIsZombie(-1) || pidIsZombie(1<<30) {
		t.Errorf("garbage pids must not report a zombie")
	}
}

// TestDeadHolderReclaimRemovesTheWorktree is defect (2)'s core acceptance, at
// the tools layer: a TERMINAL row whose holder is provably dead and whose
// worktree is CLEAN is reclaimed by the sweep's reclaim arm — the directory
// goes, the branch STAYS (it is the deliverable that must still reach
// safe-merge).
func TestDeadHolderReclaimRemovesTheWorktree(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-dead-reclaim", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	dead := deadPID(t)
	seedDeadHolderRow(t, "t-dead-reclaim", taskStatusCompleted, repo, branch, wt, hostPid(t, dead))

	proposal := strings.Join(deadHolderReclaimCandidates(), " ")
	if !strings.Contains(proposal, "t-dead-reclaim") || !strings.Contains(proposal, branch) {
		t.Fatalf("the sweep must PROPOSE the dead-holder row AND name its branch: got %q", proposal)
	}
	if got := ReclaimDeadHolderWorktrees(true); len(got) != 1 || !strings.Contains(got[0], "t-dead-reclaim") {
		t.Fatalf("reclaim must report the row it repaired, got %v", got)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the dead holder's clean worktree must be removed (stat err=%v)", err)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", branch).Run(); err != nil {
		t.Errorf("the BRANCH must survive the reclaim — it is the deliverable: %v", err)
	}
}

// TestDeadHolderReclaimLeavesALiveHolderAlone is defect (2)'s negative case —
// the one that must never regress: identical fixture, LIVE holder pid. The
// reclaim must not touch the directory (somebody is working in it).
func TestDeadHolderReclaimLeavesALiveHolderAlone(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-live-holder", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-live-holder", taskStatusCompleted, repo, branch, wt, hostPid(t, os.Getpid()))

	if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
		t.Fatalf("a LIVE holder's worktree must never be reclaimed, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the live holder's worktree must still exist: %v", err)
	}
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", branch).Run(); err != nil {
		t.Errorf("the branch must be untouched: %v", err)
	}
}

// TestDeadHolderReclaimRefusesALiveProcessInside: a DEAD claim pid is not
// enough — if live processes are actually working inside the directory (cwd
// scan), the reclaim must refuse. This is the second half of the "provably
// dead holder" contract, and it is what keeps the reclaim from deleting a
// directory out from under a running session.
func TestDeadHolderReclaimRefusesALiveProcessInside(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-inuse", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-inuse", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// Occupy the directory as a DIFFERENT test process would: the fixture's
	// own cwd is the excluded self-chain, so hold it from a child shell.
	holdDir(t, wt)
	if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
		t.Fatalf("a directory with a live cwd inside must not be reclaimed, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the in-use worktree must still exist: %v", err)
	}
}

// TestDeadHolderReclaimRefusesADirtyWorktree: uncommitted work is work — a
// dead holder's DIRTY worktree is left for a human, never swept away.
func TestDeadHolderReclaimRefusesADirtyWorktree(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	_ = repo

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-dirty", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-dirty", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
	if err := os.WriteFile(filepath.Join(wt, "uncommitted.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
		t.Fatalf("a DIRTY worktree must not be reclaimed, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the dirty worktree must still exist: %v", err)
	}
}

// TestDeadHolderReclaimNeverTouchesALiveRow pinned the PRE-EPIC behaviour: an
// in_progress row (dead pid or not) was never reclaimable, because the scan was
// keyed on TERMINAL rows and taskLiveHolderStatus called any live status a
// holder by design. The ORPHAN EPIC (2026-09-12) deliberately reverses exactly
// that one case — the dead-LOCAL-pid orphan — so this test was replaced by the
// pair below: TestDeadHolderReclaimFreesALiveStatusOrphan (in_progress + dead
// local pid ⇒ freed, branch kept, disposition named) and
// TestDeadHolderReclaimLeavesALiveTwinAlone (the same row with a LIVE pid ⇒
// untouched). Everything else about a live row is unchanged: only a pid THIS
// host can prove dead releases the directory, and every other guard (clean
// worktree, no live process inside, branch match) still applies in full.

// TestDeadHolderReclaimFreesALiveStatusOrphan is the orphan epic's reclaim-side
// acceptance (2026-09-12): the row shape above — an in_progress row whose LOCAL
// claim pid is provably DEAD — is the ONE live-status row whose directory the
// pass must free. Before the fix both paths protected it (the reclaim scan was
// keyed on terminal rows; taskLiveHolderStatus called its status a holder by
// design), so the directory and the branch gate [c] refuses stayed parked until
// a human cleared them BY HAND.
//
// RED-PROOF (mutation): with the pre-fix scan this returns 0 lines and the
// directory survives — the assertions below are the AFTER state, and the report
// must NAME the orphan disposition rather than free it silently.
func TestDeadHolderReclaimFreesALiveStatusOrphan(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	holderSeedRow(t, "t-orphan-row", hostPid(t, deadPID(t)), 1000, 2000)
	seedDeliverable(t, "t-orphan-row", repo, branch, wt)
	got := ReclaimDeadHolderWorktrees(false)
	if len(got) != 1 {
		t.Fatalf("the dry run must PROPOSE the orphan's directory: %v", got)
	}
	if !strings.Contains(got[0], branch) || !strings.Contains(got[0], "t-orphan-row") {
		t.Fatalf("the proposal must name the branch and the row: %v", got)
	}
	if !strings.Contains(got[0], finishCleanupDeadLocalHolderMarker) {
		t.Errorf("the proposal must carry the dead-local-holder disposition, not just a freed line: %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("a dry run must not remove anything: %v", err)
	}

	got = ReclaimDeadHolderWorktrees(true)
	if len(got) != 1 {
		t.Fatalf("the orphan's clean directory must be freed by one pass, got %v", got)
	}
	if !strings.Contains(got[0], finishCleanupDeadLocalHolderMarker) {
		t.Errorf("the freed line must carry the disposition (an operator must see WHY): %v", got)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the orphan's directory must be freed (stat err=%v)", err)
	}
	// The branch is the deliverable: it must survive the release.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", branch).Run(); err != nil {
		t.Errorf("the branch must survive the release: %v", err)
	}
	// Converged: a second pass has nothing left to free.
	if again := ReclaimDeadHolderWorktrees(true); len(again) != 0 {
		t.Errorf("the second pass must find nothing (state repaired, not re-failing): %v", again)
	}
}

// TestDeadHolderReclaimLeavesALiveTwinAlone is the conservative twin of the
// test above: the identical row/directory, but the claim pid is ALIVE (this
// process). Status is in_progress in both — the ONLY difference is death, so
// anything the reclaim does here is attributable to that axis alone.
func TestDeadHolderReclaimLeavesALiveTwinAlone(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	holderSeedRow(t, "t-live-twin", hostPid(t, os.Getpid()), 1000, 2000)
	seedDeliverable(t, "t-live-twin", repo, branch, wt)
	if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
		t.Fatalf("a live holder's directory must never be freed, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the live twin's worktree must still exist: %v", err)
	}
}

// TestDeadHolderReclaimDryRunReportsOnly: dry-run is the operator's probe —
// it must name the branch it WOULD free without freeing it.
func TestDeadHolderReclaimDryRunReportsOnly(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-dry", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-dry", taskStatusFailed, repo, branch, wt, hostPid(t, deadPID(t)))
	if got := ReclaimDeadHolderWorktrees(false); len(got) != 1 {
		t.Fatalf("dry run must report the reclaimable row, got %v", got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("dry run must not remove anything: %v", err)
	}
	if !strings.Contains(strings.Join(ReclaimDeadHolderWorktrees(false), " "), branch) {
		t.Errorf("the report must name the freed branch because it is what gate [c] blocks on")
	}
}

// TestDeadHolderReclaimRepairsAVanishedWorktree is the regression test for the
// failure the worker printed at startup, line after line, pass after pass:
//
//	dead-holder reclaim feat/x (t-…) remove failed: worktree remove <path>:
//	  exit status 128: fatal: '<path>' is not a working tree
//
// The row records a worktree whose DIRECTORY IS GONE (the incident's real
// shape: every one of those paths stat'd as absent). The guard used to read
// "nothing to inspect → nothing refuses → all guards held" and fall through to
// the removal, so a doomed `git worktree remove` was re-attempted on every
// lifecycle pass forever — the registration is dropped only by a successful
// remove or by `git worktree prune`, so the failure could not converge. Worse,
// `git worktree list` (which safe-merge gate [c] greps) keeps printing the
// gone-but-registered worktree, so the vanished directory withheld its own
// branch from merging.
//
// After the fix the pass REPAIRS instead of failing: the stale registration is
// pruned, no removal is attempted, and the row leaves the candidate set (a
// second pass has nothing left to say).
func TestDeadHolderReclaimRepairsAVanishedWorktree(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-vanished", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-vanished", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// The incident's shape: the directory is gone, git still holds the
	// registration, and gate [c]'s own check (`git worktree list`) still names
	// the branch.
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	lit := worktreeList(t, repo)
	if !strings.Contains(lit, branch) {
		t.Fatalf("fixture precondition: the stale registration must still be listed (gate [c] greps this): %s", lit)
	}

	got := ReclaimDeadHolderWorktrees(true)
	if len(got) != 1 || !strings.Contains(got[0], reclaimRepairedMarker) || !strings.Contains(got[0], "t-vanished") {
		t.Fatalf("a vanished worktree must be REPAIRED and reported as such, got %v", got)
	}
	if strings.Contains(strings.Join(got, " "), reclaimRemovedMarker) {
		t.Errorf("nothing was removed (the directory is gone) — the repair must not claim a removal: %v", got)
	}
	// The repair's operator-visible half: git no longer prints the stale
	// worktree, so gate [c] stops blocking the branch.
	if lit := worktreeList(t, repo); strings.Contains(lit, branch) {
		t.Fatalf("the stale registration must be pruned (gate [c] greps this list): %s", lit)
	}
	// The branch is the deliverable — the repair never touches it.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", branch).Run(); err != nil {
		t.Errorf("the branch must survive the repair: %v", err)
	}
	// Convergence: the row is no longer a candidate, so no further pass can
	// print a removal failure for it.
	if again := ReclaimDeadHolderWorktrees(true); len(again) != 0 {
		t.Errorf("a repaired row must leave the candidate set (else the burst never ends), got %v", again)
	}
}

// TestDeadHolderReclaimRefusesAGhostDirectory is the OTHER half of the same
// symptom: the recorded path EXISTS but is not a linked worktree of the row's
// repo — the re-attached / repo-moved shape that git answers with
// `fatal: '<path>' is not a working tree` (exit 128). The directory is
// somebody's ground; a worktree removal there would act in a repo git does not
// relate to this row. Refusing must be a QUIET skip (operator sink only) — no
// line in the repair list, and above all no removal attempt.
func TestDeadHolderReclaimRefusesAGhostDirectory(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-ghost", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-ghost", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// Free the registration (git takes the directory with it), then re-create
	// the path: the shape under test is "the path exists, git has no worktree
	// there" — the repo-moved / path-reused directory git answers with
	// `is not a working tree`.
	gitRun(t, repo, finishCleanupWorktreeVerb, finishCleanupWorktreeRemoveVerb, "--force", wt)
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, "someone-elses-file.txt"), []byte("not mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(worktreeList(t, repo), wt) {
		t.Fatalf("fixture precondition: the registration must be gone: %s", worktreeList(t, repo))
	}

	if got := ReclaimDeadHolderWorktrees(true); len(got) != 0 {
		t.Fatalf("a directory this repo does not own must never be reported as repaired, got %v", got)
	}
	if b, err := os.ReadFile(filepath.Join(wt, "someone-elses-file.txt")); err != nil || string(b) != "not mine" {
		t.Fatalf("a refused directory must be left exactly as it was (err=%v content=%q)", err, b)
	}
}

// TestDeadHolderReclaimUnownedRowIsNotProtected pins the occupancy half on the
// merger's side: a terminal row whose holder is dead but whose recorded
// worktree is NOT a linked worktree of repo does not OCCUPY the branch, so the
// protected arm must not name it — "protected" is a claim about a held
// checkout, and the sweep's reclaim leg will not free a directory the repo
// does not own. (The ledger can drift; git is the occupancy source of truth.)
func TestDeadHolderReclaimUnownedRowIsNotProtected(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-unowned", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-unowned", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	dead := DeadHolderBranches(repo)
	if !dead[branch] {
		t.Fatalf("precondition: an OCCUPIED worktree of a dead holder must be named, got %v", dead)
	}

	// The dead holder's directory vanishes. The branch is no longer held by
	// anything this repo owns.
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	if dead := DeadHolderBranches(repo); dead[branch] {
		t.Errorf("a branch whose only 'occupant' directory is gone is not protected, got %v", dead)
	}
}

// worktreeList returns `git worktree list` for repo — the very output
// scripts/safe-merge.sh gate [c] greps, so a test can assert what the gate
// sees instead of re-implementing it.
func worktreeList(t *testing.T, repo string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, finishCleanupWorktreeVerb, finishCleanupWorktreeListVerb)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree list: %v: %s", err, out)
	}
	return string(out)
}

// --- fixtures ---

// deadHolderReclaimFixture builds a repo + a clean LINKED worktree on a feat
// branch (the shape a killed wrapper leaves behind), and returns them.
func deadHolderReclaimFixture(t *testing.T) (repo, worktree, branch string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "wt")
	branch = "feat/dead-holder"
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "work.txt"), []byte("deliverable"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "work.txt")
	gitRun(t, repo, "commit", "-m", "work")
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, "worktree", "add", worktree, branch)
	return repo, worktree, branch
}

// gitRun runs one git command in dir and fails the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// deadPID returns a pid that is provably dead: a real child, killed and
// reaped (a hand-picked constant could belong to a live process).
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if pidAlive(pid) {
		t.Fatalf("pid %d must be dead after Wait", pid)
	}
	return pid
}

// seedDeadHolderRow writes the incident's row shape directly: a TERMINAL row
// whose deliverable names a worktree, with a claim stamp of a dead pid.
func seedDeadHolderRow(t *testing.T, taskID, status, repo, branch, worktree, claimedBy string) {
	t.Helper()
	seedDeliverable(t, taskID, repo, branch, worktree)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ? WHERE task_id = ?`,
		status, claimedBy, float64(time.Now().Unix()), taskID); err != nil {
		t.Fatalf("stamp %s: %v", taskID, err)
	}
}

// seedDeliverable records repo/branch/worktree on the row's deliverable blob.
func seedDeliverable(t *testing.T, taskID, repo, branch, worktree string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	blob := `{"repo":"` + repo + `","branch":"` + branch + `","worktree":"` + worktree + `"}`
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("deliverable %s: %v", taskID, err)
	}
}

// holdDir starts a child shell whose CWD is dir, so the reclaim's live-cwd
// scan sees a real holder that is NOT this process's self-chain. The child is
// killed on cleanup.
func holdDir(t *testing.T, dir string) {
	t.Helper()
	//fixture-spawn-ok: the child's CWD inside this directory is exactly what is measured (the live-holder veto reads /proc/<pid>/cwd), so it cannot be parked away; a helper would also make it this process's own child, which the self-chain exclusion is testing against
	cmd := exec.Command("sh", "-c", "cd \"$1\" && sleep 30", "sh", dir)
	if err := cmd.Start(); err != nil {
		t.Fatalf("hold dir: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore teardown of the fixture holder; the cwd observation is what the test asserts
		_ = cmd.Wait()         //stylegate:ignore reap of the killed fixture holder; its liveness is read from /proc
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		if cwd, ok := finishCleanupProcCWD(itoa(cmd.Process.Pid)); ok && finishCleanupPathUnder(cwd, dir) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the holder process never entered %s", dir)
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while a just-forked child's cwd appears in /proc — sampling is the only observable
	}
}

// deadHolderReclaimCandidates is the sweep's proposal pass as the reclaim arm
// sees it — the report lines for the branches gate [c] blocks but which are
// reclaimable.
func deadHolderReclaimCandidates() []string {
	return ReclaimDeadHolderWorktrees(false)
}

// spawnZombieChild creates a child that exits but whose parent (this test
// process) deliberately does NOT wait for it — the unreaped-zombie shape.
func spawnZombieChild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	zombieChild = cmd
	deadline := time.Now().Add(5 * time.Second)
	for {
		if pidIsZombie(cmd.Process.Pid) {
			return cmd.Process.Pid
		}
		if time.Now().After(deadline) {
			reapChild()
			t.Fatalf("child %d never reached zombie state", cmd.Process.Pid)
		}
		time.Sleep(20 * time.Millisecond) //stylegate:allow-sleep poll interval while the fixture child transitions to zombie; the state is observable only by sampling
	}
}

// reapChild waits for the deliberately unreaped zombie child, so no test
// leaks a zombie into the rest of the suite.
func reapChild() {
	if zombieChild != nil {
		_ = zombieChild.Wait() //stylegate:ignore reaping the deliberately unreaped fixture child; the test asserts the zombie state, not this exit status
		zombieChild = nil
	}
}

// zombieChild is the single deliberately-unreaped child the Z-state test
// holds (see spawnZombieChild / reapChild).
var zombieChild *exec.Cmd

// TestDeadHolderBranchIsNamedInTheSweepPreview is deliverable (c)'s
// acceptance: the merger's protected arm must SAY a branch is held by a DEAD
// holder rather than folding it into a silent protected count.
func TestDeadHolderBranchIsNamedInTheSweepPreview(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-preview", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-preview", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	preview := MergerSweepPreview(repo)
	if preview.Protected != 1 {
		t.Fatalf("the branch is worktree-protected (protected=%d)", preview.Protected)
	}
	if len(preview.DeadHolder) != 1 || preview.DeadHolder[0] != branch {
		t.Fatalf("the preview must NAME the dead-holder branch, got dead=%v (want [%s])", preview.DeadHolder, branch)
	}
	if !strings.Contains(preview.DescribeProtected(), branch) {
		t.Errorf("the operator line must name %s, got %q", branch, preview.DescribeProtected())
	}
}

// TestLiveHolderBranchIsNotReportedAsDead: the mirror of the above — a LIVE
// holder's branch is protected but must NOT be announced as a dead holder
// (a false "dead holder" line would invite an operator to free a busy
// worktree).
func TestLiveHolderBranchIsNotReportedAsDead(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-preview-live", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-preview-live", taskStatusInProgress, repo, branch, wt, hostPid(t, os.Getpid()))

	preview := MergerSweepPreview(repo)
	if preview.Protected != 1 {
		t.Fatalf("the branch is worktree-protected (protected=%d)", preview.Protected)
	}
	if len(preview.DeadHolder) != 0 {
		t.Errorf("a live holder must not be reported as a dead-holder branch, got %v", preview.DeadHolder)
	}
}

// TestDeadHolderHonestVerdict_RefusesTheReclaimableClaim is the regression test
// for the 2026-09-13 defect: the merger's protected arm told an operator that
// every dead-holder branch was "reclaimable by the finish-cleanup sweep" — a
// promise about a RELEASE, while the reclaim arm's own guards (a clean
// worktree, guard ②) refuse the majority of them by design. Measured live: 7
// branches carried the promise for over an hour, 6 of them DIRTY, and no
// resident process ever ran the arm.
//
// The assertion is a two-sided contract: a DIRTY dead holder's branch must be
// named as still-refused WITH the arm's own reason, and must NOT appear as one
// the arm will free; a CLEAN one must NOT be reported as refused (there the
// release is real, so naming a veto would be the mirror-image lie).
func TestDeadHolderHonestVerdict_RefusesTheReclaimableClaim(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-honest", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-honest", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))

	// CLEAN first: the arm's guards all hold, so the branch is a real release
	// candidate — the verdict must be silent about it.
	clean := MergerSweepPreview(repo)
	if len(clean.DeadHolder) != 1 || clean.DeadHolder[0] != branch {
		t.Fatalf("precondition: the dead holder's branch must be named (got %v)", clean.DeadHolder)
	}
	if len(clean.Held) != 0 {
		t.Fatalf("a CLEAN dead holder's directory is freeable — it must not be reported as still refused: %v", clean.Held)
	}
	if strings.Contains(clean.DescribeProtected(), "still refused") {
		t.Errorf("the operator line must not refuse a branch the arm will free: %q", clean.DescribeProtected())
	}

	// Now the measured shape: the holder's directory carries uncommitted work.
	if err := os.WriteFile(filepath.Join(wt, "uncommitted.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty := MergerSweepPreview(repo)
	if _, ok := dirty.Held[branch]; !ok {
		t.Fatalf("a DIRTY dead holder's branch must carry the arm's verdict, got held=%v", dirty.Held)
	}
	if got := dirty.DescribeProtected(); !strings.Contains(got, "still refused") ||
		!strings.Contains(got, branch) || !strings.Contains(got, "DIRTY") {
		t.Errorf("the operator line must name the still-refused branch and the reason (dirty), got %q", got)
	}
	// The old promise must be gone: no surface may call it reclaimable while
	// the arm's own guard refuses it.
	if strings.Contains(dirty.DescribeProtected(), "reclaimable by the finish-cleanup sweep") {
		t.Errorf("the false release promise must not survive: %q", dirty.DescribeProtected())
	}
}

// TestDeadHolderRowWithNoLedgerEntryIsNotClaimed: a worktree-occupied branch
// with NO row naming it is not attributable to a dead holder — the reclaim
// and the preview must treat it as plain protected work (the ledger is the
// only evidence of ownership; git alone cannot know a holder died).
func TestDeadHolderRowWithNoLedgerEntryIsNotClaimed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	repo, _, _ := deadHolderReclaimFixture(t)

	preview := MergerSweepPreview(repo)
	if preview.Protected != 1 {
		t.Fatalf("the branch is worktree-protected (protected=%d)", preview.Protected)
	}
	if len(preview.DeadHolder) != 0 {
		t.Errorf("no ledger row names this branch — it must not be reported as a dead holder, got %v", preview.DeadHolder)
	}
}

// TestReclaimEnvKillSwitch: the reclaim arm is switchable (the finish-cleanup
// env convention) — off means report-only, never a removal.
func TestReclaimEnvKillSwitch(t *testing.T) {
	repo, wt, branch := deadHolderReclaimFixture(t)
	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "s", Payload: "p", TaskID: "t-envoff", Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	seedDeadHolderRow(t, "t-envoff", taskStatusCompleted, repo, branch, wt, hostPid(t, deadPID(t)))
	t.Setenv(ReclaimDeadHolderEnv, "0")

	got := ReclaimDeadHolderWorktrees(true)
	if len(got) != 1 || !strings.Contains(got[0], branch) {
		// Report-only by contract: the branch is still NAMED (an operator
		// must see what is withheld) — silence would hide it again.
		t.Fatalf("the kill switch must turn the reclaim into a report naming %s, got %v", branch, got)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("the worktree must still exist with the kill switch off: %v", err)
	}
	_ = config.GopiDir
}
