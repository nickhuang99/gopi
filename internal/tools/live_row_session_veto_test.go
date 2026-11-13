package tools

// Regression tests for the 2026-09-17 incident (task 01a0af50): a LIVE session's
// worktree directory and branch were destroyed mid-run, and its only committed
// tip became reachable from no ref (`git branch --contains` and
// `git tag --contains` both empty; the branch answered `fatal: invalid
// reference`). The row ran in TWO sessions at once while its deliverable named
// only the other session's worktree, so every ledger-keyed arm read "nobody owns
// this" and the removal went ahead.
//
// Two properties are pinned here, each with a POSITIVE and a NEGATIVE half so
// the guard cannot degenerate into a blanket refusal:
//
//	SESSION ARM   a live row's standing run protects the registrations its
//	              deliverable does not name — and stops protecting them the
//	              moment the run is gone.
//	ORPHAN GUARD  a force-delete that would leave a tip reachable from no ref is
//	              refused — while an ordinary merged branch is still reaped.

import (
	"github.com/nick/gopi/internal/scratch"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/worktreeroots"
)

// sessionVetoFixtureRoot gives the sweep a declared root on the suite's scratch
// tree (the ONE sandbox hook, so no test can reach a real workspace).
func sessionVetoFixtureRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "declared-root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(worktreeroots.WorktreeRootEnvName, root)
	return root
}

// sessionVetoSeedSessionDir creates the row's session container under the gopi
// profile the test pins, returning its path — the stand-in for the directory an
// adopting worker's process stands in.
//
// It deliberately does NOT move this process's cwd: the fixtures below assert a
// BEFORE (no run standing ⇒ the arm frees) and an AFTER (a run standing ⇒ the
// arm refuses) over the SAME fixture, so the cwd flip has to be theirs to place.
// sessionVetoChdir does that flip.
func sessionVetoSeedSessionDir(t *testing.T, taskID string) string {
	t.Helper()
	gd, err := config.GopiDir()
	if err != nil {
		t.Fatalf("gopi dir: %v", err)
	}
	dir := filepath.Join(gd, taskSessionsRootName, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestSweepKeepsGroundOfALiveRowsSecondSession is the incident's exact shape on
// the DECISION stage: the registration belongs to no row's deliverable, but a
// live row has a run standing in its own session directory — so the registration
// (and with it the branch the run is working on) is kept, under its own rule.
func TestSweepKeepsGroundOfALiveRowsSecondSession(t *testing.T) {
	root := sessionVetoFixtureRoot(t)
	reg := sweepFixtureReg(root, "gopi-second-session", "fix/second-session")
	roots := []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	// The row: LIVE status, demonstrably STANDING run (its own session dir is
	// the CWD of this test process), and — the incident's key detail — a
	// deliverable that names a DIFFERENT worktree of the same row.
	dir := sessionVetoSeedSessionDir(t, "T-two-sessions")
	live := []tasksRowForSweep{{
		TaskID: "T-two-sessions", Status: taskStatusInProgress,
		Worktree: filepath.Join(t.TempDir(), "gopi-other-worktree"), Branch: "fix/second-session",
	}}
	sessionVetoChdir(t, dir)

	f := worktreeSweepJudge(reg, live, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) {
		t.Fatalf("a live row's standing session did not protect the registration: %s (%s)", f.Disposition, f.Reason)
	}
	if f.Rule != string(worktreeRuleSessionLive) {
		t.Fatalf("verdict rule = %q, want %q — the arm that decided must be named (%s)", f.Rule, worktreeRuleSessionLive, f.Reason)
	}
	if !strings.Contains(f.Reason, "T-two-sessions") {
		t.Errorf("the refusal does not name the live row: %s", f.Reason)
	}

	// NEGATIVE HALF — the same fixture with the run GONE (no live process in the
	// session dir, and no recorded runner): the registration is reclaimed and the
	// branch reaped, i.e. the arm protects a STANDING run, not a row status.
	sessionVetoChdir(t, t.TempDir())
	dead := []tasksRowForSweep{{
		TaskID: "T-two-sessions", Status: taskStatusInProgress,
		Worktree: filepath.Join(t.TempDir(), "gopi-other-worktree"), Branch: "fix/second-session",
	}}
	f = worktreeSweepJudge(reg, dead, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition == string(worktreeKeep) {
		t.Fatalf("a live row with NO standing run still vetoed on the session arm: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestReclaimRefusesADirectoryALiveRowsSessionStandsIn is the incident's shape
// on the REMOVAL stage (the arm that deletes the directory): the row's claim pid
// is dead — which is exactly the state the removed-runner window produces — while
// its run is still standing. The refusal must name the row.
func TestReclaimRefusesADirectoryALiveRowsSessionStandsIn(t *testing.T) {
	repo, worktree, branch := deadHolderReclaimFixture(t)
	dir := sessionVetoSeedSessionDir(t, "t-live-session")

	row := reclaimDeadHolderRow{
		TaskID: "t-live-session", Status: taskStatusInProgress,
		ClaimedBy: "zbook:" + strconv.Itoa(deadPID(t)),
		Repo:      repo, Branch: branch, Worktree: worktree,
	}
	// Before the stand-in: dead claim pid, no recorded runner → the arm frees.
	if disp, why := reclaimDisposition(row, nil); disp != reclaimFreable {
		t.Fatalf("baseline disposition = %v (%s), want free", disp, why)
	}
	// With a live process standing in the row's session dir: refused, named.
	sessionVetoChdir(t, dir)
	disp, why := reclaimDisposition(row, nil)
	if disp != reclaimSkip {
		t.Fatalf("a live row's standing session did not refuse the removal: %v (%s)", disp, why)
	}
	if !strings.Contains(why, "t-live-session") || !strings.Contains(why, dir) {
		t.Errorf("the refusal must name the row AND its session directory: %s", why)
	}
	// NEGATIVE HALF: the run gone → the same row is reclaimed again (no blanket
	// refusal), which is what keeps the orphan epic's release working.
	sessionVetoChdir(t, t.TempDir())
	if disp, why := reclaimDisposition(row, nil); disp != reclaimFreable {
		t.Fatalf("the arm stopped freeing once the run was gone: %v (%s)", disp, why)
	}
}

// sessionVetoChdir puts THIS process's working directory where the fixture needs
// it (the /proc cwd channel is the one the predicate reads), restoring it on
// cleanup. The test process's own cwd is the stand-in for a worker that adopted
// the row (measured: that is exactly where an adopting worker stands).
func sessionVetoChdir(t *testing.T, dir string) {
	t.Helper()
	// t.Chdir (not an os.Chdir + manual restore pair): it restores the process
	// cwd at cleanup AND fails the test if the change cannot be made, so the
	// flip this fixture depends on can never silently not happen (stylegate's
	// test-chdir rule states the same preference — this is its recommended form).
	t.Chdir(dir)
}

// TestBranchDeleteGuardRefusesAnOrphaningDelete is the REF half of the incident
// with both poles: a force-delete that would leave the tip reachable from no ref
// is REFUSED and named (positive), while an ordinary merged branch is still
// reaped (negative — the guard is not a blanket refusal).
func TestBranchDeleteGuardRefusesAnOrphaningDelete(t *testing.T) {
	repo := branchGuardFixtureRepo(t)

	// orphaning: a branch whose one commit no other ref reaches.
	gitRun(t, repo, "checkout", "-b", "fix/orphan", "master")
	branchGuardCommit(t, repo, "orphan.txt", "unmerged work")
	gitRun(t, repo, "checkout", "master")

	why := branchDeleteOrphanRefusal(repo, "fix/orphan")
	if why == "" {
		t.Fatal("an unmerged, unreferenced branch was NOT refused — the tip would be orphaned")
	}
	if !strings.Contains(why, branchDeleteNoOtherRefMarker) || !strings.Contains(why, "fix/orphan") {
		t.Errorf("the refusal must carry the marker and name the branch: %s", why)
	}
	// The branch is still there — a refusal is not a deletion.
	if out := rsGit(t, repo, false, "rev-parse", "--verify", "refs/heads/fix/orphan"); strings.TrimSpace(out) == "" {
		t.Fatal("the refused branch is gone — the guard deleted instead of refusing")
	}

	// NEGATIVE: merged into the integration branch → the same delete is safe.
	gitRun(t, repo, "merge", "--no-ff", "-m", "merge orphan", "fix/orphan")
	if why := branchDeleteOrphanRefusal(repo, "fix/orphan"); why != "" {
		t.Fatalf("a MERGED branch was refused: %s — the guard must not block ordinary reaping", why)
	}
	// NEGATIVE: another branch carrying the tip (reachability, not equality).
	gitRun(t, repo, "checkout", "-b", "topic/carrier", "fix/orphan^")
	if why := branchDeleteOrphanRefusal(repo, "fix/orphan"); why != "" {
		t.Fatalf("a tip another branch reaches was refused: %s", why)
	}
	gitRun(t, repo, "checkout", "master")

	// A branch that does not exist is not an orphan (nothing to lose).
	if why := branchDeleteOrphanRefusal(repo, "fix/never-existed"); why != "" {
		t.Errorf("a missing branch was reported as an orphan: %s", why)
	}
}

// branchGuardFixtureRepo builds a throwaway repo with one master commit.
func branchGuardFixtureRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	gitRun(t, root, "init", "-q", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-q", "-b", "master")
	branchGuardCommit(t, repo, "base.txt", "base")
	return repo
}

// branchGuardCommit writes one file and commits it.
func branchGuardCommit(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", name)
	gitRun(t, repo, "commit", "-q", "-m", content)
}

// branchGuardBareWithBranch builds an empty bare repo — the delivery point the
// push under test writes into.
func branchGuardBareWithBranch(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "delivery.git")
	gitRun(t, filepath.Dir(bare), "init", "-q", "--bare", bare)
	return bare
}

// TestBranchDeleteGuardAsksTheDeliveryPoint is the DELIVERY-POINT half of the
// orphan guard (2026-09-18, the finish-cleanup chain's own shape). The chain
// pushes the branch to its target in step ② and then force-deletes the local
// ref; asking the LOCAL repo alone answers "no other ref" for a tip the
// delivery just kept, which refused the chain's healthy close-out (the two
// fixture tests red at master). Both poles, plus the fail-closed direction:
//
//	DELIVERED     the target holds the tip      → NOT refused
//	UNDELIVERED   the target holds nothing      → refused
//	MISMATCHED    the target holds another tip  → refused
//	UNREADABLE    the target cannot be reached  → refused
//	NO POINT      no delivery point named       → refused
//	LOCAL CARRIER local repo holds the tip      → NOT refused (unchanged)
func TestBranchDeleteGuardAsksTheDeliveryPoint(t *testing.T) {
	repo := branchGuardFixtureRepo(t)
	delivery := branchGuardBareWithBranch(t)

	gitRun(t, repo, "checkout", "-q", "-b", "feat/delivered", "master")
	branchGuardCommit(t, repo, "delivered.txt", "delivered work")
	gitRun(t, repo, "checkout", "-q", "master")
	tip := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "refs/heads/feat/delivered"))

	// NO POINT: the local scan is unchanged — an unmerged, unreferenced
	// branch is still refused (the guard's own reason, untouched by this fix).
	if why := branchDeleteOrphanRefusal(repo, "feat/delivered"); why == "" {
		t.Fatal("without a delivery point an unreferenced branch was NOT refused")
	}
	// UNDELIVERED: an existing but empty delivery point is no evidence.
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", delivery); why == "" {
		t.Fatal("a delivery point that does NOT hold the tip was accepted as one")
	}
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", delivery); !strings.Contains(why, branchDeleteDeliveryPointMarker) {
		t.Errorf("the refusal must name the delivery point it asked: %s", why)
	}
	// UNDELIVERED: a target that cannot be reached at all (a missing path).
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", filepath.Join(t.TempDir(), "gone.git")); why == "" {
		t.Fatal("an unreachable delivery point was accepted as evidence")
	}

	// DELIVERED: push the tip → the delivery point holds EXACTLY it.
	gitRun(t, repo, "push", "-q", delivery, "feat/delivered:refs/heads/feat/delivered")
	if got := strings.TrimSpace(rsGit(t, delivery, true, "rev-parse", "refs/heads/feat/delivered")); got != tip {
		t.Fatalf("fixture: the delivery point holds %s, want the pushed tip %s", got, tip)
	}
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", delivery); why != "" {
		t.Fatalf("the DELIVERED tip was refused: %s — the delivery point IS a ref that holds it", why)
	}
	// Two-pole proof: the same predicate, same repo, without the point —
	// refused. The delivery point is the only thing that changed the answer.
	if why := branchDeleteOrphanRefusal(repo, "feat/delivered"); why == "" {
		t.Fatal("the local-only form must still refuse the same branch (the point is what changed)")
	}

	// MISMATCHED: the target holds a DIFFERENT tip (the branch moved on after
	// the push, so the tip the delivery kept is NOT the tip at risk) → no
	// evidence, the local refusal stands. The move must land on the BRANCH —
	// a commit made on master would leave both tips equal and the pole would
	// prove nothing, so the fixture asserts the two tips really differ.
	gitRun(t, repo, "checkout", "-q", "feat/delivered")
	branchGuardCommit(t, repo, "later.txt", "moved on")
	gitRun(t, repo, "checkout", "-q", "master")
	moved := strings.TrimSpace(rsGit(t, repo, true, "rev-parse", "refs/heads/feat/delivered"))
	if got := strings.TrimSpace(rsGit(t, delivery, true, "rev-parse", "refs/heads/feat/delivered")); got != tip || moved == tip {
		t.Fatalf("fixture: want the branch at %s and the delivery point behind at %s, got branch %s / point %s",
			moved, tip, moved, got)
	}
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", delivery); why == "" {
		t.Fatal("a delivery point holding a DIFFERENT tip was accepted as evidence")
	}

	// LOCAL CARRIER: a ref in the judged repo still holds the tip → reaped
	// (the reachability half of the guard is unchanged by the delivery point).
	// The carrier takes the tip the BRANCH currently holds — the branch moved
	// in the pole above, so minting the carrier at the old tip would leave the
	// tip under test unreferenced and the pole would prove nothing.
	gitRun(t, repo, "branch", "topic/carrier", moved)
	if why := branchDeleteOrphanRefusalAt(repo, "feat/delivered", filepath.Join(t.TempDir(), "gone.git")); why != "" {
		t.Fatalf("a tip the LOCAL repo still reaches was refused: %s", why)
	}
}
