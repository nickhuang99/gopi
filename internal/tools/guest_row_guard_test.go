package tools

// Regression tests for the GUEST-ROW refusal — the ROW-reconciliation half of
// the worktree-eviction defect (epic 01a0c2b5, 2026-09-21). The sweep half
// (worktreeRuleSelfCheckout, 832794fe) is pinned in
// worktree_self_checkout_refusal_test.go; the criterion implemented here is its
// sibling and the mechanism is documented in guest_row_guard.go.
//
// WHAT THESE TESTS PIN, and why BOTH arms appear. The defect has one shape — a
// process standing in a LINKED worktree of repo R releases worktrees of R that
// are not its to release — reached by two arms:
//
//	ROW arm (ReclaimDeadHolderWorktrees → reclaimDisposition): a PEER row's
//	        checkout is freed by a guest. Measured pre-fix on this host
//	        (2026-09-21): a planted, aged, clean, row-owned registration was
//	        removed by `go test ./cmd/agent/ -run
//	        TestTaskLifecycleTick_ReclaimsDeadHolderWorktree` run FROM a guest
//	        worktree, with NO refusal line anywhere; after the guard the same
//	        run prints the guestRowReleaseRefusal sentence instead. (That test
//	        name is historical: 58b5618b retired its tick call site and the
//	        acceptance now runs as TestMergerPass_ReclaimsDeadHolderWorktree —
//	        the arm this paragraph is about is unchanged.)
//	OWN arm (ReclaimOwnFinishedWorktrees → reclaimSelfRelease): a run releases
//	        the checkout it is ITSELF standing in — what took the sweep fix's
//	        own closing gate run down (getwd: no such file or directory) while a
//	        planted decoy survived.
//
// The arms share ONE criterion (guest_row_guard.go) but answer DIFFERENT
// questions — "may I free a peer's checkout" vs "may I free my own row's" — and
// they carry DISTINCT arm names, so each is asserted separately, END TO END
// (through the exported entry points), never through the predicate alone: a
// criterion test would let either arm's wiring rot.
//
// WHY A CHILD PROCESS. The criterion is a fact about the PROCESS's working
// directory, resolved once per process. A helper run in its own process (with
// cmd.Dir set) is the only honest way to present "this process stands in a
// linked worktree" versus "this process stands in the main checkout", and it
// keeps the suite from chdir'ing the whole test binary — a global mutation that
// would make every sibling test's own cwd reasoning unreliable (the same
// technique the sweep half's test uses).
//
// BOTH DIRECTIONS, because a refusal that fires on everything is not a guard,
// it is a stopped sweep:
//
//	(a) GUEST + peer row      ⇒ kept, refusal NAMES the ROW arm;
//	(b) GUEST + its OWN row   ⇒ kept, refusal NAMES the OWN arm;
//	(c) RESIDENT in the repo  ⇒ the same rows are still reconciled, both arms;
//	(d) GUEST + FOREIGN repo  ⇒ a guest still reconciles another repo's rows.
//
// THE RED PROOF is DOUBLE, because the guard's own source does not exist on the
// pre-fix tree:
//
//	① TestGuestRowGuardIsWiredAndNarrow asserts the wired predicates are
//	  reachable and REFUSE NOTHING on a path they cannot identify — so this file
//	  cannot pass vacuously against a build that never wired the guard.
//	② TestGuestRowGuardRedProof drives cases (a) and (b) with the guard DISABLED
//	  (guestRowGuardOffEnv) — reproducing the PRE-FIX behaviour from the fixed
//	  source, which is how the failure text quoted in the task report was
//	  produced. The same disabling mutation is what the independent pre-fix run
//	  (branch scoped) exercises.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// guestRowChildEnv marks the child-helper run; the other three name the
	// repo whose rows the child reconciles, the ARM it drives, and the row.
	guestRowChildEnv     = "GOPI_TEST_GUESTROW_CHILD"
	guestRowChildRepoEnv = "GOPI_TEST_GUESTROW_REPO"
	guestRowChildModeEnv = "GOPI_TEST_GUESTROW_MODE"
	guestRowChildTaskEnv = "GOPI_TEST_GUESTROW_TASK"
	guestRowChildWTEnv   = "GOPI_TEST_GUESTROW_WORKTREE"

	// guestRowModeRow / guestRowModeOwn select the arm the helper drives.
	guestRowModeRow = "row"
	guestRowModeOwn = "own"

	// guestRowBranchName is the deliverable branch every fixture checks out in
	// its linked worktree.
	guestRowBranchName = "feat/guest-row-guard"

	// guestRowOutcomeFmt is the child's own statement about the path, printed
	// AFTER the arm ran: the evidence a failure quotes.
	guestRowOutcomeFmt = "OUTCOME %s exists=%v"
)

// TestGuestRowGuardHelper is not a test: it is the child-process body of the
// tests below. It runs the REAL arm (exported entry point, real ledger, real
// repo) and prints the arm's own lines plus the resulting state of the path the
// assertions read.
func TestGuestRowGuardHelper(t *testing.T) {
	if os.Getenv(guestRowChildEnv) == "" {
		t.Skip("helper — driven by TestGuestRowRefusesToReconcileFromAGuestWorktree")
	}
	repo := os.Getenv(guestRowChildRepoEnv)
	mode := os.Getenv(guestRowChildModeEnv)
	taskID := os.Getenv(guestRowChildTaskEnv)
	wt := os.Getenv(guestRowChildWTEnv)
	self := worktreeSweepSelfPosition()
	t.Logf("SELF checkout=%q common=%q guest=%v", self.Repo, self.Common, self.Guest)

	switch mode {
	case guestRowModeOwn:
		// The producer arm's candidate set is "claimed by THIS process", so the
		// stamp must be written by the CHILD — the process that will release.
		stampGuestRowOwn(t, taskID)
		for _, line := range ReclaimOwnFinishedWorktrees() {
			t.Logf("OWN %s", line)
		}
	default:
		for _, line := range ReclaimDeadHolderWorktrees(true) {
			t.Logf("ROW %s", line)
		}
	}
	_, err := os.Stat(wt)
	t.Logf(guestRowOutcomeFmt, wt, err == nil)
	_ = repo
}

// stampGuestRowOwn re-stamps the row as THIS process's own live-status claim —
// the exact shape reclaimSelfRowsFrom selects on.
func stampGuestRowOwn(t *testing.T, taskID string) {
	t.Helper()
	stamp := TaskClaimOwner("")
	if stamp == "" {
		t.Skip("no hostname on this machine — the self-claim shape is untestable here")
	}
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, `+tasksInProgressAtColumnName+
			` = ?, updated_at = ? WHERE task_id = ?`,
		taskStatusInProgress, stamp, 1000.0, 2000.0, 2000.0, taskID); err != nil {
		t.Fatalf("stamp own row: %v", err)
	}
}

// guestRowFixture builds one repository with a LINKED worktree holding a
// deliverable commit on its own branch, plus a submitted row whose deliverable
// names repo/branch/worktree and whose holder is a pid this host can prove DEAD.
// It returns the ledger + settings paths too, because the child must be handed
// the same ones (its own process environment cannot inherit the test pins), and
// the row id, so a caller never has to guess it.
func guestRowFixture(t *testing.T, taskID, ledger string) (repo, wt, rowID string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	gitRun(t, root, "init", repo)
	gitRun(t, repo, "config", "user.email", "t@t")
	gitRun(t, repo, "config", "user.name", "t")
	gitRun(t, repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "a.txt")
	gitRun(t, repo, "commit", "-m", "init")
	gitRun(t, repo, "checkout", "-b", guestRowBranchName)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "deliverable.txt")
	gitRun(t, repo, "commit", "-m", "the deliverable")
	gitRun(t, repo, "checkout", "master")
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, guestRowBranchName)

	if _, err := TaskSubmit(TaskSubmitOptions{Summary: "guest row guard", Payload: "p", TaskID: taskID, Source: "human"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	// The row's ground must be whoever git says holds that branch — never a
	// path the fixture merely hoped git picked.
	if got := guestRowGitWorktreePath(t, repo, guestRowBranchName); got != wt {
		t.Fatalf("fixture: git registers branch %s at %q, not %q", guestRowBranchName, got, wt)
	}
	seedGuestRowDeadHolder(t, taskID, repo, guestRowBranchName, wt)
	return repo, wt, taskID
}

// guestRowGitWorktreePath reads git's own answer for where a branch is checked
// out (porcelain `git worktree list`).
func guestRowGitWorktreePath(t *testing.T, repo, branch string) string {
	t.Helper()
	out := gitOut(t, repo, finishCleanupWorktreeVerb, "list", "--porcelain")
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, finishCleanupPorcelainWorktreePrefix):
			path = strings.TrimSpace(strings.TrimPrefix(line, finishCleanupPorcelainWorktreePrefix))
		case strings.TrimSpace(line) == "branch refs/heads/"+branch:
			return path
		}
	}
	return ""
}

// seedGuestRowDeadHolder stamps the row as a TERMINAL row whose holder is a pid
// this host can prove DEAD: a child of this very process, started and REAPED, so
// its pid is demonstrably gone (pidAlive's signal-0 probe fails) while the
// stamp's shape is the production one. `unknown` is never `dead` — the fixture
// asserts the pid really is gone.
func seedGuestRowDeadHolder(t *testing.T, taskID, repo, branch, wt string) {
	t.Helper()
	stamp := guestRowDeadHolderStamp(t)
	ctx := context.Background()
	db := openTestTasksDB(t, memoryDBPath())
	defer db.Close()
	if _, err := db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, claimed_by = ?, claimed_at = ?, updated_at = ?, deliverable = ? WHERE task_id = ?`,
		taskStatusCompleted, stamp, 1000.0, 2000.0,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`, taskID); err != nil {
		t.Fatalf("seed dead-holder row: %v", err)
	}
}

// guestRowDeadHolderStamp renders the claim stamp of a pid this host can prove
// dead.
func guestRowDeadHolderStamp(t *testing.T) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no hostname on this machine — the claim-stamp shape is untestable here")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("reap helper child: %v", err)
	}
	pid := cmd.Process.Pid
	if pidAlive(pid) {
		t.Skipf("pid %d still reports alive; a dead-holder fixture is unavailable here", pid)
	}
	return host + claimHolderHostPidSep + strconv.Itoa(pid)
}

// runGuestRowChild runs the helper in its own process standing in `stand`.
func runGuestRowChild(t *testing.T, stand, repo, mode, taskID, wt, ledger, settings string, guardOff bool) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestGuestRowGuardHelper$", "-test.v")
	cmd.Dir = stand
	env := append(os.Environ(),
		guestRowChildEnv+"=1",
		guestRowChildRepoEnv+"="+repo,
		guestRowChildModeEnv+"="+mode,
		guestRowChildTaskEnv+"="+taskID,
		guestRowChildWTEnv+"="+wt,
		memoryDBEnv+"="+ledger,
		config.SettingsEnv+"="+settings,
	)
	env = append(env, guestRowGuardOffEnv+"="+guestRowGuardSwitchValue(guardOff))
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper in %s (mode %s): %v\n%s", stand, mode, err, out)
	}
	return string(out)
}

// guestRowGuardSwitchValue renders the red-proof switch: the guard is ON unless
// a proof explicitly turns it off (an empty value must not disable a guard by
// accident — Rule 15's direction).
func guestRowGuardSwitchValue(off bool) string {
	if off {
		return "off"
	}
	return "on"
}

// guestRowCase is one direction of the criterion.
type guestRowCase struct {
	name string
	// stand is where the child process's cwd is put.
	standGuest bool
	// repoIsForeign selects the repository the child reconciles: its own (false)
	// or another one entirely (true).
	repoIsForeign bool
	// mode selects the arm.
	mode string
	// wantGone is what must happen to the row's worktree.
	wantGone bool
	// wantArm is the arm name the refusal must carry when !wantGone.
	wantArm string
}

// TestGuestRowRefusesToReconcileFromAGuestWorktree is THE criterion test: four
// directions, both arms, driven end to end through the exported entry points in
// a child process that actually STANDS where the case says it stands.
func TestGuestRowRefusesToReconcileFromAGuestWorktree(t *testing.T) {
	guestRowCriterion(t, false)
}

// TestGuestRowGuardRedProof drives the two REFUSAL directions with the guard
// disabled — the pre-fix behaviour, from the fixed source. It is the red-proof's
// second half (see the file doc): with the guard off the directories go, which
// is exactly what these tests exist to prevent.
func TestGuestRowGuardRedProof(t *testing.T) {
	guestRowCriterion(t, true)
}

// guestRowCriterion runs the case table; guardOff reproduces the pre-fix tree
// (the guard predicates answer ""), so the assertions invert for the cases the
// guard is responsible for.
func guestRowCriterion(t *testing.T, guardOff bool) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// ONE ledger for the whole case set (both fixtures' rows live in it): the
	// arm reconciles whatever rows the LEDGER names, so a per-fixture ledger
	// would make the foreign case untestable — and would be a weaker statement
	// than the real shape, where one process reads one ledger.
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	ledger := filepath.Join(t.TempDir(), "memory.db")
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	// The pins are in-process only: the CHILD gets the same two paths in its env
	// (see runGuestRowChild) — a child cannot inherit a t.Setenv.
	t.Setenv(memoryDBEnv, ledger)
	t.Setenv(config.SettingsEnv, settings)

	// The guest's own repository (repo A + its linked worktree wtA) and a
	// FOREIGN repository (repo B + wtB) — a guest keeps its mandate over B.
	repoA, wtA, taskA := guestRowFixture(t, guestRowTaskID(t, "a"), ledger)
	repoB, wtB, taskB := guestRowFixture(t, guestRowTaskID(t, "b"), ledger)

	cases := []guestRowCase{
		{
			name: "guest frees a PEER row of its own repo", standGuest: true, mode: guestRowModeRow,
			wantGone: false, wantArm: guestRowArmRow,
		},
		{
			name: "guest frees its OWN row of its own repo", standGuest: true, mode: guestRowModeOwn,
			wantGone: false, wantArm: guestRowArmOwn,
		},
		{
			name: "resident frees a PEER row of its own repo", standGuest: false, mode: guestRowModeRow,
			wantGone: true,
		},
		{
			name: "resident frees its OWN row of its own repo", standGuest: false, mode: guestRowModeOwn,
			wantGone: true,
		},
		{
			name: "guest frees a row of a FOREIGN repo", standGuest: true, repoIsForeign: true, mode: guestRowModeRow,
			wantGone: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stand, repo, wt, taskID := repoA, repoA, wtA, taskA
			if !tc.standGuest {
				// The RESIDENT case stands in the repository's main checkout.
				stand = repoA
			} else {
				stand = wtA
			}
			if tc.repoIsForeign {
				repo, wt, taskID = repoB, wtB, taskB
			}
			// A case that removes a worktree must not decide the next case's
			// fixture: re-create BOTH registrations first (the fixture also
			// keeps each repository's ground truthful for the next case).
			restoreGuestRowWorktree(t, repoA, wtA, guestRowBranchName)
			restoreGuestRowWorktree(t, repoB, wtB, guestRowBranchName)
			out := runGuestRowChild(t, stand, repo, tc.mode, taskID, wt, ledger, settings, guardOff)

			_, err := os.Stat(wt)
			gone := os.IsNotExist(err)
			wantGone := tc.wantGone
			if guardOff && !tc.wantGone {
				// The red proof asserts the PRE-FIX outcome for the two refused
				// directions: the directory goes (that is the defect).
				wantGone = true
			}
			if gone != wantGone {
				t.Fatalf("%s\nrow %s of %s: worktree %s gone=%v, want gone=%v\narm output:\n%s",
					tc.name, taskID, repo, wt, gone, wantGone, out)
			}
			if tc.wantArm != "" && !guardOff {
				// Rule 15: the refusal is NAMED, not merely silent — the kept
				// directory must carry the arm and the evidence that decided it.
				if !strings.Contains(out, tc.wantArm) {
					t.Fatalf("the refusal must name the %q arm.\narm output:\n%s", tc.wantArm, out)
				}
			}
			if tc.wantArm == "" && !strings.Contains(out, "OUTCOME") {
				t.Fatalf("the helper must report the outcome it observed:\n%s", out)
			}
		})
	}
}

// guestRowTaskID renders the row id for one fixture slot.
func guestRowTaskID(t *testing.T, slot string) string {
	t.Helper()
	return "t-guest-row-" + slot
}

// restoreGuestRowWorktree re-creates the linked worktree at wt if a previous
// subtest let an arm remove it, so every subtest starts from the same world.
func restoreGuestRowWorktree(t *testing.T, repo, wt, branch string) {
	t.Helper()
	if _, err := os.Stat(wt); err == nil {
		return
	}
	gitRun(t, repo, finishCleanupWorktreeVerb, "add", wt, guestRowBranchName)
}

// guestRowArmRow / guestRowArmOwn are the two arm names the refusals must carry
// (Rule 13: one spelling, referenced by both the guard and its test).
const (
	guestRowArmRow = "arm guestRowReleaseRefusal"
	guestRowArmOwn = "arm guestRowOwnReleaseRefusal"
)

// TestGuestRowGuardIsWiredAndNarrow is the anti-vacuity half of the red proof:
// the wired predicates are REACHABLE and refuse NOTHING on a path they cannot
// identify (a directory that is not a repository at all). A guard that refused
// everywhere would be a stopped release pass, and a guard that was never wired
// would make every case above pass for the wrong reason.
func TestGuestRowGuardIsWiredAndNarrow(t *testing.T) {
	if !strings.Contains(guestRowRefusalFmt, guestRowArmRow) {
		t.Fatalf("the ROW arm's sentence must name its arm: %q", guestRowRefusalFmt)
	}
	if !strings.Contains(guestRowOwnRefusalFmt, guestRowArmOwn) {
		t.Fatalf("the OWN arm's sentence must name its arm: %q", guestRowOwnRefusalFmt)
	}
	// A path that is not a repository cannot be identified as the actor's, so
	// NOTHING is refused (the arms keep their mandate over ground they cannot
	// name as a peer's).
	for _, dir := range []string{t.TempDir(), ""} {
		if why := guestRowReleaseRefusal(dir); why != "" {
			t.Fatalf("guestRowReleaseRefusal(%q) refused: %s — an unidentifiable repo must not refuse", dir, why)
		}
		if why := guestRowOwnRefusal(dir); why != "" {
			t.Fatalf("guestRowOwnRefusal(%q) refused: %s — an unidentifiable repo must not refuse", dir, why)
		}
	}
}
