package tools

// main_checkout_clone_ground_test.go — the regression gate for the close-out
// arm that MOVED A LIVE RUN'S GROUND (measured 2026-10-28).
//
// THE DEFECT, in the shape it was measured: a clone-channel run works in a
// workspace the channel opened for it (`~/workspace/<row-id>`, `.git` a
// DIRECTORY — devworkspace.GroundModelClone), on the branch `task/<row-id>`. Its
// auto-registered row names that workspace as its `deliverable.repo` and records
// NO `deliverable.worktree`. At close-out, FinishCleanupRestoreMainCheckout
// therefore:
//
//	guard ① (a branch held by a LINKED worktree)  → no match: a clone is
//	                                               registered nowhere;
//	guard ② (the row records a ground ≠ main)     → no match: worktree is "";
//	→ finishCleanupMainRepoDir(repo) = the clone itself
//	→ git checkout master        ← INSIDE THE LIVE RUN'S OWN GROUND.
//
// Measured on this host before the fix: the ground's own reflog carried four
// `checkout: moving from task/… to master` entries, and the reproducer is
// `go test -run TestAutoTaskRegisterOnAndEndHook ./internal/agent` run while
// standing in a clone ground (the close-out runs inside that same process).
//
// THE CRITERION (and why it is TWO facts): "is this directory a clone?" is
// `devworkspace.GroundModelOf` — the ONE discriminator — but being a clone does
// NOT disqualify a directory, because a repo's SHARED MAIN CHECKOUT is an
// ordinary clone too. What disqualifies it is being the ground the runner
// ANNOUNCED to this run (`TaskGroundAnnounced`). Hence two arms below: the
// declared ground is left alone, and a clone that is nobody's announced ground
// is still restored (the compatibility contract — the real main checkout keeps
// its repair).

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/devworkspace"
)

// mainCheckoutCloneGroundFixture builds THIS RUN'S CLONE GROUND: a standalone repository
// (`.git` is a DIRECTORY, so the ONE discriminator says CLONE) parked on the
// run's own task branch, with a clean tracked tree — i.e. exactly the state the
// pre-fix arm happily moved to the integration branch.
//
// It also pins the measurement itself: a fixture that was NOT a clone would let
// the regression test pass for the wrong reason.
func mainCheckoutCloneGroundFixture(t *testing.T) (ground, taskBranch string) {
	t.Helper()
	ground, taskBranch = mainCheckoutCloseoutFixture(t)
	if got := devworkspace.GroundModelOf(ground); got != devworkspace.GroundModelClone {
		t.Fatalf("fixture model = %q, want %q — the arm under test is keyed on this measurement",
			got, devworkspace.GroundModelClone)
	}
	return ground, taskBranch
}

// TestFinishCleanupRestoreMainCheckoutLeavesTheRunsCloneGroundAlone is the RED
// PROOF of the fix, in its minimal form: the directory handed to the close-out
// IS this run's announced clone ground, so the arm must decline — the ground
// stays on its task branch, and the refusal says why.
//
// BEFORE THE FIX this test fails at `headBranch`: the checkout read `master`
// (the arm ran `git checkout master` in the run's own live workspace).
func TestFinishCleanupRestoreMainCheckoutLeavesTheRunsCloneGroundAlone(t *testing.T) {
	ground, taskBranch := mainCheckoutCloneGroundFixture(t)
	t.Setenv(TaskGroundDirEnv, ground)
	t.Setenv(TaskGroundBranchEnv, taskBranch)

	// The measured call shape: repo = the ground, worktree = "" (the auto row
	// records no worktree), branch = "" (it records no branch either).
	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), ground, "", "")
	if !ok {
		t.Fatalf("the arm refused the clone ground outright: %s — a decline is not a refusal, "+
			"the directory is simply not its question", detail)
	}
	if got := headBranch(t, ground); got != taskBranch {
		t.Fatalf("the clone ground was moved to %q (want it left on %q) — the close-out switched a "+
			"LIVE run off its task branch", got, taskBranch)
	}
	if !strings.Contains(detail, MainCheckoutCloneGroundMarker) {
		t.Errorf("the decline is not named: %q (want the %q marker)", detail, MainCheckoutCloneGroundMarker)
	}
	if !strings.Contains(detail, TaskGroundDirEnv) {
		t.Errorf("the decline does not name the announcement it decided on (Rule 15): %q", detail)
	}
	if !strings.Contains(detail, string(devworkspace.GroundModelClone)) {
		t.Errorf("the decline does not state the measured ground model: %q", detail)
	}
}

// TestFinishCleanupRestoreMainCheckoutStillRepairsTheSharedCheckout is the
// REVERSE ARM, and it is what keeps the fix from becoming a blanket refusal: a
// SHARED main checkout is a clone by the same measurement (`.git` is a
// directory), so the criterion cannot be "is it a clone" alone. With no ground
// announced to this process — the ordinary sweep / main-checkout shape — the
// repair still happens, exactly as before.
func TestFinishCleanupRestoreMainCheckoutStillRepairsTheSharedCheckout(t *testing.T) {
	repo, branch := mainCheckoutCloneGroundFixture(t)
	// An announced ground that is SOMEBODY ELSE'S directory: the shared checkout
	// below is not it, so the new arm has nothing to say about it.
	t.Setenv(TaskGroundDirEnv, t.TempDir())

	ok, detail := FinishCleanupRestoreMainCheckout(context.Background(), repo, "", "")
	if !ok {
		t.Fatalf("the shared checkout was not restored: %s", detail)
	}
	if strings.Contains(detail, MainCheckoutCloneGroundMarker) {
		t.Fatalf("the shared checkout was declined as a clone ground: %q — a clone alone is not the criterion", detail)
	}
	if !strings.Contains(detail, MainCheckoutRestoredMarker) {
		t.Errorf("the repair is not reported: %q", detail)
	}
	if got := headBranch(t, repo); got != masterBranchName {
		t.Errorf("the shared checkout is on %q, want %s", got, masterBranchName)
	}
	_ = branch
}

// TestFinishCleanupPlanPredictsNothingForACloneGround binds the dry run to the
// apply: the apply declines a clone ground, so the plan must predict nothing for
// one — a plan promising a move the apply refuses is the plan/apply disagreement
// this step exists to prevent.
func TestFinishCleanupPlanPredictsNothingForACloneGround(t *testing.T) {
	ground, taskBranch := mainCheckoutCloneGroundFixture(t)
	t.Setenv(TaskGroundDirEnv, ground)
	t.Setenv(TaskGroundBranchEnv, taskBranch)

	if step := finishCleanupMainCheckoutPlannedStep(context.Background(), ground); step != nil {
		t.Fatalf("the plan predicted %+v for a clone ground the apply declines", *step)
	}
	if got := headBranch(t, ground); got != taskBranch {
		t.Errorf("the PLAN moved the clone ground to %q — a dry run acts on nothing", got)
	}
}

// cloneGroundRestoreFixture builds a per-run clone ground: a repository whose
// `.git` is a DIRECTORY, created as a DIRECT child of a throwaway workspace
// root, standing on the row's ledger branch with one commit on top of master.
// It returns (cloneDir, branch).
//
// It is the fixture the TWO BELOW arms need and the announcement arm above does
// not: those two are about the processes that never see the announcement pair.
func cloneGroundRestoreFixture(t *testing.T) (clone, branch string) {
	t.Helper()
	root := t.TempDir()
	// The workspace root is CONFIGURATION, never a path this test invents
	// (Rule 34's shape rule, applied to the clone channel's root): the
	// production reader decides which directory is the channel's.
	t.Setenv(config.WorkspaceRootEnvName, root)

	clone = filepath.Join(root, "01a17e4b-2b86-7691-9cd1-61c898c61863")
	branch = TaskGroundBranchPrefix + "01a17e4b2b8676919cd161c898c61863"

	gitRun(t, root, "init", "-q", "-b", masterBranchName, clone)
	gitRun(t, clone, "config", "user.email", "test@example.invalid")
	gitRun(t, clone, "config", "user.name", "test")
	writeRepoFile(t, clone, "f.txt", "base\n")
	gitRun(t, clone, "add", "f.txt")
	gitRun(t, clone, "commit", "-q", "-m", "base")

	gitRun(t, clone, "checkout", "-q", "-b", branch)
	writeRepoFile(t, clone, "work.txt", "this run's work\n")
	gitRun(t, clone, "add", "work.txt")
	gitRun(t, clone, "commit", "-q", "-m", "the run's work")
	return clone, branch
}

// TestFinishCleanupMainCheckoutRestoreLeavesTheRunsOwnCloneGroundAlone is the
// SECOND-BYPASS regression, and it exists because the announcement arm above
// (which is the close-out FACE, FinishCleanupRestoreMainCheckout) passed its own
// test and the ground still moved (measured the same day, one hour later).
//
// WHAT WAS MEASURED. After the face-only fix landed, a whole-package
// `make test-budget PKGS='./cmd/agent'` moved this run's ground to master AGAIN.
// The ground's post-checkout hook recorded the mover:
//
//	2026-10-28T19:30:01+08:00 pid=2049361 cwd=/home/user/workspace/01a17e4b…
//	  prev=b52e00c… new=b7e575c… flag=1
//	  2049334: git checkout master
//	  2049284: …/test-budget/run-…/test-budget …/gopi --prompt RP1-BOUNDARY-PLAIN-PROMPT-4f2a …
//	  2008494: …/agent.test -test.run=TestRunPathPlainPromptIsVerbatim
//
// — a CHILD gopi started by an unrelated cmd/agent test. It never touches the
// close-out face; it reaches the move through FinishCleanupMainCheckoutRestore,
// which the sweep tick and the merge executor also call directly. A guard at one
// caller is a guard every other caller routes around.
//
// THE ASSERTION IS MADE ON THE IMPLEMENTATION ITSELF, not on a caller: that is
// what makes it survive the next caller someone adds.
func TestFinishCleanupMainCheckoutRestoreLeavesTheRunsOwnCloneGroundAlone(t *testing.T) {
	clone, branch := cloneGroundRestoreFixture(t)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), clone, masterBranchName)
	if !ok {
		t.Fatalf("the restore refused a clone ground: %s", detail)
	}
	if detail != "" {
		t.Errorf("the restore reported acting on a clone ground: %q", detail)
	}
	if got := headBranch(t, clone); got != branch {
		t.Fatalf("the restore moved the run's own ground from %q to %q — every caller of this ONE "+
			"implementation (the sweep tick, the merge executor, the close-out) must leave a per-run "+
			"clone ground alone", branch, got)
	}
	if body, err := os.ReadFile(filepath.Join(clone, "work.txt")); err != nil || string(body) != "this run's work\n" {
		t.Errorf("the run's deliverable is gone from the ground: body=%q err=%v", body, err)
	}
}

// TestFinishCleanupMainRestoreRecognisesAGroundByItsLedgerAddress is the SECOND
// discriminator's regression, and it exists because the PATH test alone was
// measured insufficient (2026-10-28T19:50:56).
//
// The fixture is a checkout that is NOT a direct child of the configured
// workspace root (so finishCleanupRepoIsACloneGround answers false, exactly as
// it does inside a cmd/agent test whose child gopi carries a throwaway
// GOPI_HOME), but which STANDS ON task/<id>. That branch is the row's ledger
// address (Rule 6a), so the checkout is somebody's ground and the restore must
// leave it alone.
func TestFinishCleanupMainRestoreRecognisesAGroundByItsLedgerAddress(t *testing.T) {
	// A root the checkout is NOT a child of: the path test cannot see it.
	t.Setenv(config.WorkspaceRootEnvName, t.TempDir())

	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", masterBranchName, ".")
	gitRun(t, repo, "config", "user.email", "test@example.invalid")
	gitRun(t, repo, "config", "user.name", "test")
	writeRepoFile(t, repo, "f.txt", "base\n")
	gitRun(t, repo, "add", "f.txt")
	gitRun(t, repo, "commit", "-q", "-m", "base")
	branch := TaskGroundBranchPrefix + "01a17e4b2b8676919cd161c898c61863"
	gitRun(t, repo, "checkout", "-q", "-b", branch)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, masterBranchName)
	if !ok || detail != "" {
		t.Fatalf("the restore acted on a checkout standing on a ledger address: ok=%v detail=%q", ok, detail)
	}
	if got := headBranch(t, repo); got != branch {
		t.Fatalf("the restore moved a ground recognised only by its ledger address: %q → %q", branch, got)
	}
}

// TestFinishCleanupPlanPredictsNothingWithoutTheAnnouncement binds the dry run
// to the apply for the IMPLEMENTATION-LEVEL predicates: a clone ground that was
// never announced to this process (the shape a sweep tick or a suite child sees)
// is declined by the apply, so the plan must predict nothing for it either.
func TestFinishCleanupPlanPredictsNothingWithoutTheAnnouncement(t *testing.T) {
	clone, branch := cloneGroundRestoreFixture(t)

	if step := finishCleanupMainCheckoutPlannedStep(context.Background(), clone); step != nil {
		t.Fatalf("the plan predicted %+v for a clone ground the apply declines", *step)
	}
	if got := headBranch(t, clone); got != branch {
		t.Errorf("the PLAN moved the clone ground to %q — a dry run acts on nothing", got)
	}
}

// TestFinishCleanupMainRestoreLeavesACloneGroundOnAFeatureBranchAlone is the
// PATH predicate's own arm, and the reason it is not redundant with the ledger
// address above: a row whose deliverable NAMES a branch gets a ground standing
// on that branch, not on task/<id> (devworkspace Config.GroundBranch). The
// ledger test cannot see that shape, and the shape test — a clone that is a
// DIRECT child of the CONFIGURED workspace root — can.
func TestFinishCleanupMainRestoreLeavesACloneGroundOnAFeatureBranchAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv(config.WorkspaceRootEnvName, root)

	ground := filepath.Join(root, "01a17e4b-2b86-7691-9cd1-61c898c61863")
	gitRun(t, root, "init", "-q", "-b", masterBranchName, ground)
	gitRun(t, ground, "config", "user.email", "test@example.invalid")
	gitRun(t, ground, "config", "user.name", "test")
	writeRepoFile(t, ground, "f.txt", "base\n")
	gitRun(t, ground, "add", "f.txt")
	gitRun(t, ground, "commit", "-q", "-m", "base")
	// NOT a ledger address: the row declared its own branch, so the second
	// discriminator answers false and only the path test can catch this.
	branch := "feat/declared-by-the-row"
	gitRun(t, ground, "checkout", "-q", "-b", branch)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), ground, masterBranchName)
	if !ok || detail != "" {
		t.Fatalf("the restore acted on a per-run clone ground: ok=%v detail=%q", ok, detail)
	}
	if got := headBranch(t, ground); got != branch {
		t.Fatalf("the restore moved a clone ground parked on %q to %q — the workspace-root shape "+
			"test is the only guard that sees this one", branch, got)
	}
}

// TestFinishCleanupMainRestoreLeavesAProbeBlindCloneGroundAlone is the THIRD
// bypass regression, and it is the one this run MEASURED on the shipped guard
// set (96a853d + 7925bfd): a clone ground that BOTH existing discriminators are
// blind to.
//
// THE SHAPE, and why each guard answers false — neither is a bug in the guard,
// they are two facts a per-run ground does not always carry:
//
//	① finishCleanupRepoIsACloneGround   → the CONFIGURED workspace root is
//	   per-process. A suite child carrying a throwaway GOPI_HOME resolves a
//	   temporary root, so the real ground is not a DIRECT child of it.
//	② finishCleanupCheckoutIsAGroundsLedgerAddress → the box only holds for a
//	   row whose branch IS `task/<id>`. A row that declared its own branch
//	   (devworkspace Config.Branch, e.g. `feat/x`) gets a ground standing on
//	   THAT name, and devworkspace.Open re-checks it out on every reuse.
//
// With both false, the pre-fix implementation reached `git checkout master`
// INSIDE the live run's own workspace (measured this run, evidence
// 03-residual-hole-red.txt: `restored the main checkout:
// feat/declared-by-the-row → master`).
//
// THE THIRD DISCRIMINATOR is the directory's own NAME, and it is the only fact
// of the three that lives on the FILESYSTEM rather than in a profile: the
// clone channel materialises a ground at `devworkspace.Dir(root, taskID)`, i.e.
// `<root>/<the row's full id>`, so the leaf of a per-run ground IS a canonical
// task id. No profile can rename the directory.
func TestFinishCleanupMainRestoreLeavesAProbeBlindCloneGroundAlone(t *testing.T) {
	root := t.TempDir()
	// The configured root does NOT contain the ground: ① cannot see it.
	t.Setenv(config.WorkspaceRootEnvName, t.TempDir())

	// The name the channel gives a ground, built by the channel's own join.
	const rowID = "01a17e4b-2b86-7691-9cd1-61c898c61863"
	ground := devworkspace.Dir(root, rowID)
	gitRun(t, root, "init", "-q", "-b", masterBranchName, ground)
	// The fixture pins the MEASUREMENT it is built on: a directory that is not a
	// standalone clone would let the arm below pass for the wrong reason.
	if got := devworkspace.GroundModelOf(ground); got != devworkspace.GroundModelClone {
		t.Fatalf("devworkspace.Dir(%q, %q) is not a clone ground: %q", root, rowID, got)
	}
	gitRun(t, ground, "config", "user.email", "test@example.invalid")
	gitRun(t, ground, "config", "user.name", "test")
	writeRepoFile(t, ground, "f.txt", "base\n")
	gitRun(t, ground, "add", "f.txt")
	gitRun(t, ground, "commit", "-q", "-m", "base")
	// ② cannot see it either: the row declared its own branch.
	branch := "feat/declared-by-the-row"
	gitRun(t, ground, "checkout", "-q", "-b", branch)
	writeRepoFile(t, ground, "work.txt", "this run's work\n")
	gitRun(t, ground, "add", "work.txt")
	gitRun(t, ground, "commit", "-q", "-m", "the run's work")

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), ground, masterBranchName)
	if !ok || detail != "" {
		t.Fatalf("the restore acted on a ground both shipped guards are blind to: ok=%v detail=%q", ok, detail)
	}
	if got := headBranch(t, ground); got != branch {
		t.Fatalf("the restore moved a live ground %q → %q — the name is the only fact of the three "+
			"that survives a throwaway GOPI_HOME", branch, got)
	}
	if body, err := os.ReadFile(filepath.Join(ground, "work.txt")); err != nil || string(body) != "this run's work\n" {
		t.Errorf("the run's deliverable is gone from the ground: body=%q err=%v", body, err)
	}
}

// TestFinishCleanupMainRestoreStillRepairsACheckoutThatIsNotAGroundName is the
// REVERSE ARM of the third discriminator: an ordinary checkout whose directory
// name is NOT a task id (a repo's shared main checkout — `/home/user/work/gopi`) is still
// repaired. Without this, "the name looks like a ground" would silently become
// "never restore anything", and a parked main checkout would stop being fixed.
func TestFinishCleanupMainRestoreStillRepairsACheckoutThatIsNotAGroundName(t *testing.T) {
	t.Setenv(config.WorkspaceRootEnvName, t.TempDir())
	repo, branch := mainCheckoutCloneGroundFixture(t)

	ok, detail := FinishCleanupMainCheckoutRestore(context.Background(), repo, masterBranchName)
	if !ok {
		t.Fatalf("the shared checkout was not restored: %s", detail)
	}
	if !strings.Contains(detail, MainCheckoutRestoredMarker) {
		t.Errorf("the repair is not reported: %q (came from %s)", detail, branch)
	}
	if got := headBranch(t, repo); got != masterBranchName {
		t.Errorf("the shared checkout is on %q, want %s — the name test must not become a blanket refusal", got, masterBranchName)
	}
}
