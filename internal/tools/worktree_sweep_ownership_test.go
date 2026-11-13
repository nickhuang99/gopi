package tools

import (
	"context"
	"github.com/nick/gopi/internal/scratch"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/worktreeroots"
)

// sweepOwnershipRoot declares a root for the ownership-rung tests.
func sweepOwnershipRoot(t *testing.T) (string, []worktreeroots.Root) {
	t.Helper()
	root := sweepFixtureRoot(t)
	return root, []worktreeroots.Root{{Path: root, Role: worktreeroots.RoleWorktree, Source: worktreeroots.SourceSettingsKey}}
}

// TestSweepBranchNameCollisionDoesNotProtectAForeignTree is the guard-③ red
// proof (epic: worktree stay, deliverable 1). A NON-TERMINAL row that merely
// NAMES this registration's branch, while recording a DIFFERENT worktree, is
// working somewhere else: the branch names collide (both tasks minted
// fix/<same-name>), the directory does not belong to that row, and the rung
// must not turn a name collision into ownership. Before the fix the row
// vetoed reclamation for as long as it stayed open; after it, a clean aged
// registration is reclaimed.
func TestSweepBranchNameCollisionDoesNotProtectAForeignTree(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-collided", "fix/shared-name")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	collision := []tasksRowForSweep{{
		TaskID:   "T-collide",
		Status:   taskStatusInProgress,
		Branch:   reg.Branch,               // the same name …
		Worktree: root + "/somewhere-else", // … but a different directory
		// no proof of death recorded → the row keeps its ground
	}}
	f := worktreeSweepJudge(reg, collision, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("a branch-name collision protected a foreign tree: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepUnstampedRowOfThisBranchStillProtects is the other half of the same
// rung: the collision above is only a collision because the row records ANOTHER
// directory. A row that names this branch and records NO directory (the
// submit-time {"repo":...} placeholder, or a tilde spelling that never got
// backfilled) is the owner whose proof is missing — it must keep protecting.
func TestSweepUnstampedRowOfThisBranchStillProtects(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-unstamped", "fix/unstamped")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	rows := []tasksRowForSweep{{
		TaskID: "T-unstamped",
		Status: taskStatusInProgress,
		Branch: reg.Branch,
		// no proof of death recorded → the row keeps its ground
	}}
	f := worktreeSweepJudge(reg, rows, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeKeep) || !strings.Contains(f.Reason, "T-unstamped") {
		t.Fatalf("an owner without a stamped path lost its protection: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepDistrustsAStampWhoseRunnerIsGone is the deliverable-2 red proof: the
// PERSISTED false deliverable.worktree stamp (written when a run merely
// inspected another task's worktree) still named this registration from a
// NON-TERMINAL row — but the row's own recorded runner is provably gone, so the
// row is an ownership RECORD, not an owner. The sweep must distrust the stamp
// and reclaim; before the fix the stamp alone vetoed it forever.
func TestSweepDistrustsAStampWhoseRunnerIsGone(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-false-stamp", "fix/false-stamp")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	stale := []tasksRowForSweep{{
		TaskID:           "T-false-stamp",
		Status:           taskStatusInProgress,
		Worktree:         reg.Path, // the persisted stamp — path arm would match
		Branch:           reg.Branch,
		HolderProvenDead: true, // this host can prove its recorded runner is gone
	}}
	f := worktreeSweepJudge(reg, stale, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("a false stamp with no living runner still owned the tree: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepAgeFloorDoesNotShieldAnEndedOwner is the guard-⑤ red proof
// (deliverable 3): a registration created TODAY (age well under the 24 h
// floor) whose owning row the LEDGER has already stamped as ended. The floor
// exists to spare a registration something just created; when the ledger's own
// evidence says the run is over, there is nothing left for it to protect — so
// the same-day orphan is reclaimable the same day, without an operator
// hand-edit and without waiting out the clock.
func TestSweepAgeFloorDoesNotShieldAnEndedOwner(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-same-day-orphan", "fix/same-day")
	opts := sweepOptionsNow(true)
	// Created ten minutes ago — far inside the default 24 h floor.
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-10 * time.Minute)}

	ended := []tasksRowForSweep{{
		TaskID:   "T-ended",
		Status:   taskStatusInProgress, // status never went terminal …
		Worktree: reg.Path,
		Branch:   reg.Branch,
		// no proof of death recorded → the row keeps its ground
		EndedAt: float64(opts.Now.Add(-6 * time.Hour).Unix()), // … but the ledger stamped its end
	}}
	f := worktreeSweepJudge(reg, ended, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("the age floor shielded a registration whose owner had ended: %s (%s)", f.Disposition, f.Reason)
	}
	if !strings.Contains(f.Reason, "T-ended") {
		t.Errorf("the reclaim reason must name the row that licensed it, got %q", f.Reason)
	}
}

// TestSweepYoungRegistrationWithoutOwnerEvidenceIsStillKept pins the floor's
// remaining ground: a young registration with NO ended-owner evidence is kept,
// exactly as before the change. Without this the guard-⑤ change would be a
// licence to reclaim anything fresh.
func TestSweepYoungRegistrationWithoutOwnerEvidenceIsStillKept(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-fresh", "fix/fresh")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-10 * time.Minute)}

	if f := worktreeSweepJudge(reg, nil, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a fresh unowned registration was %s (%s), want keep", f.Disposition, f.Reason)
	}
	// An ended row that ends INSIDE the grace window (a run that submitted a
	// child as it closed out) must also stay protected.
	within := []tasksRowForSweep{{
		TaskID: "T-just-ended", Status: taskStatusInProgress, Worktree: reg.Path, Branch: reg.Branch,
		// no proof of death recorded → the row keeps its ground
		// ended ONE MINUTE ago — inside worktreeSweepEndedGrace, the window in
		// which a closing run may still be enqueuing a child
		EndedAt: float64(opts.Now.Add(-time.Minute).Unix()),
	}}
	if f := worktreeSweepJudge(reg, within, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("a just-ended row inside the grace window lost its protection: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepLoaderCarriesABranchOnlyRow is the loader-level red proof for the
// evidence set. The rung-③ branch arm can only protect a row the loader
// actually delivered, and the loader used to scan for the "worktree" needle
// alone — so a live row whose deliverable records ONLY a branch (the
// {"repo":...,"branch":...} blob a run writes when its worktree was never
// backfilled, or the branch-only placeholder a submitter stamps) was invisible
// to the arm that exists to protect it. MEASURED on a real probe ledger: a row
// naming only a branch was absent from the evidence set while every direct-row
// test kept passing. This test drives the REAL loader, so it fails on the
// needle and passes on the wide scan.
func TestSweepLoaderCarriesABranchOnlyRow(t *testing.T) {
	hintFixtureLedger(t) // isolates the DB the loader reads
	seedSubmitterRow(t, "t-sweep-branch-only", "55555555-6666-4777-8888-000000000001")
	seedDeliverableBranchOnly(t, "t-sweep-branch-only", "/fixture/repo", "fix/branch-only")
	seedSubmitterRow(t, "t-sweep-full", "55555555-6666-4777-8888-000000000002")
	seedDeliverable(t, "t-sweep-full", "/fixture/repo", "feat/full", "/fixture/wt-full")

	rows, err := worktreeSweepTaskRows(context.Background())
	if err != nil {
		t.Fatalf("ledger read: %v", err)
	}
	var sawBranchOnly, sawFull bool
	for _, r := range rows {
		switch r.TaskID {
		case "t-sweep-branch-only":
			sawBranchOnly = true
			if r.Branch != "fix/branch-only" {
				t.Errorf("branch-only row loaded with branch %q", r.Branch)
			}
		case "t-sweep-full":
			sawFull = true
		}
	}
	if !sawFull {
		t.Fatalf("the loader lost an ordinary worktree row (evidence set = %d rows)", len(rows))
	}
	if !sawBranchOnly {
		t.Fatalf("the loader dropped a branch-only row: the branch arm cannot protect evidence it never receives")
	}
}

// seedDeliverableBranchOnly writes a deliverable that names repo+branch and NO
// worktree — the shape a run leaves when its tree was never backfilled.
func seedDeliverableBranchOnly(t *testing.T, taskID, repo, branch string) {
	t.Helper()
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	blob := `{"repo":"` + repo + `","branch":"` + branch + `"}`
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET deliverable = ? WHERE task_id = ?`, blob, taskID); err != nil {
		t.Fatalf("deliverable %s: %v", taskID, err)
	}
}

// TestSweepKeepsRegistrationOfAGenuinelyLiveRun is the safety half of the
// whole change: the sharper predicate must not free ground a live run stands
// on. A live-status row with a live holder and no ended stamp protects both by
// path and by branch — the behaviour every other rung depends on.
func TestSweepKeepsRegistrationOfAGenuinelyLiveRun(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-truly-live", "fix/truly-live")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-72 * time.Hour)}

	live := []tasksRowForSweep{{
		TaskID: "T-live", Status: taskStatusInProgress, Worktree: reg.Path, Branch: reg.Branch,
		// no proof of death recorded → the row keeps its ground;
		// no ended stamp → the ledger has not declared the row's life over
	}}
	if f := worktreeSweepJudge(reg, live, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) ||
		!strings.Contains(f.Reason, "T-live") {
		t.Fatalf("a genuinely live run's worktree was not kept: %s (%s)", f.Disposition, f.Reason)
	}
	// With NO identity recorded at all, an open row keeps its protection
	// (unknowable ≠ dead) — the fail-closed direction, re-asserted here so a
	// future tightening of the holder proof cannot silently free live ground.
	unknowable := []tasksRowForSweep{{
		TaskID: "T-nostamp", Status: taskStatusInProgress, Branch: reg.Branch,
		// no proof of death recorded → the row keeps its ground
	}}
	if f := worktreeSweepJudge(reg, unknowable, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("an open row with no readable claim lost its protection: %s (%s)", f.Disposition, f.Reason)
	}
}

// TestSweepAgeFloorDoesNotShieldATerminalOwner is the OTHER half of the
// guard-⑤ red proof (deliverable 3), and the half that needs no
// self-contradiction to be read: the registration was created TODAY (well
// inside the 24 h floor) and its owning row is TERMINAL — the ordinary way a
// finished run leaves a same-day orphan. On the pre-change code the floor was
// measured on the registration record alone, so this registration reported
// age=0h and could not be reclaimed until the next day, no matter how plainly
// the ledger said the run was over. MEASURED on the live ledger: 671 rows
// carry a deliverable and 665 of the terminal ones carry a populated ended_at.
func TestSweepAgeFloorDoesNotShieldATerminalOwner(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-same-day-terminal", "fix/same-day-terminal")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-10 * time.Minute)}

	owner := []tasksRowForSweep{{
		TaskID:   "T-finished",
		Status:   taskStatusCompleted,
		Worktree: reg.Path,
		Branch:   reg.Branch,
		EndedAt:  float64(opts.Now.Add(-3 * time.Hour).Unix()),
	}}
	f := worktreeSweepJudge(reg, owner, scratch.HolderScan{}, roots, opts, ho)
	if f.Disposition != string(worktreeReclaim) {
		t.Fatalf("the age floor shielded a same-day registration whose owner is terminal: %s (%s)", f.Disposition, f.Reason)
	}
	if !strings.Contains(f.Reason, "T-finished") {
		t.Errorf("the reclaim reason must name the row that licensed it, got %q", f.Reason)
	}
}

// TestSweepEndedRowElsewhereDoesNotLicenseTheBypass is the safety twin of the
// widening above. Because terminal rows now count as ended, a branch-name match
// ALONE would let any finished task elsewhere license the bypass — so the tie
// must hold: a terminal row that names this branch but records a DIFFERENT
// worktree is working somewhere else, and it says nothing about this
// registration. The registration must stay inside the floor.
func TestSweepEndedRowElsewhereDoesNotLicenseTheBypass(t *testing.T) {
	root, roots := sweepOwnershipRoot(t)
	reg := sweepFixtureReg(root, "gopi-untied", "fix/untied")
	opts := sweepOptionsNow(true)
	ho := worktreeSweepHostObservations{CreatedAt: opts.Now.Add(-10 * time.Minute)}

	elsewhere := []tasksRowForSweep{{
		TaskID:   "T-elsewhere",
		Status:   taskStatusCompleted,
		Worktree: root + "/another-tree",
		Branch:   reg.Branch, // the same name, a different directory
		EndedAt:  float64(opts.Now.Add(-3 * time.Hour).Unix()),
	}}
	if f := worktreeSweepJudge(reg, elsewhere, scratch.HolderScan{}, roots, opts, ho); f.Disposition != string(worktreeKeep) {
		t.Fatalf("an ended row working elsewhere licensed the age-floor bypass: %s (%s)", f.Disposition, f.Reason)
	}
}
