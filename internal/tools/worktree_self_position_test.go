package tools

// Regression guard for the MEASURED defect that kept `gopi session resume` from
// reclaiming the worktree it had just finished in (epic 01a0d500, 2026-09-25).
//
// THE SYMPTOM. `gopi session resume <row>` completed the row, released its cwd, ran
// finish-cleanup — and the worktree stayed, with the cleanup saying:
//
//	[gopi] finish-cleanup: skipped: own-ground refusal (arm
//	  guestRowOwnCheckoutRefusal): the worktree <wt> is the checkout THIS
//	  process is standing in …
//
// THE MEASUREMENT (instrumented run, one pid, two reads):
//
//	[selfres] pid=304507 cwd="<wt>" root="<wt>"
//	[ownref]  pid=304507 cwd="/"   memo="<wt>" candidate="<wt>"   ← refused
//	[ownref]  pid=304507 cwd="/"   memo="<wt>" candidate="<wt>"
//
// The process was NOT standing in the worktree any more — it had chdir'd to
// `/`, exactly as the producer obligation requires — while the ACTOR-POSITION
// MEMO still answered "you stand in <wt>". The memo was written once per
// PROCESS (`sync.Once`), and the assumption it rested on ("the pass chdir'ing
// mid-run is not a shape this code has") was false twice over in this very
// flow: `gopi session resume` chdirs INTO the deliverable worktree to work there
// (cli_resume.go resumeChdirToWorktree) and its close-out then hands the cwd
// BACK (worktreerelease.ReleaseReporting) so the directory becomes removable.
// A refused cleanup is therefore the visible half of a stale identity.
//
// THE FIX pinned here: the position is memoized per CWD — re-measured whenever
// the process's working directory changes — so "where do I stand?" is answered
// about NOW. The GATE is untouched: a process that really is standing in the
// candidate directory still resolves that directory and is still refused (the
// sibling test TestWorktreeSweepRefusesToFreeTheRepoItStandsIn and both
// guestRow own-ground tests keep pinning that direction).
//
// WHY A CHILD PROCESS: cwd is process-global, so the only honest way to present
// "this process moved" is to move an actual process — chdir'ing the whole test
// binary would corrupt every sibling test's own cwd reasoning.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// selfPositionChildEnv marks the re-exec'd helper.
	selfPositionChildEnv = "GOPI_TEST_SELFPOSITION_CHILD"
	// selfPositionMainEnv / selfPositionWTEnv hand the child the two places it
	// must stand in, in order: the linked worktree first, then the main
	// checkout. The child derives nothing — a fixture-built expectation is what
	// makes a failure readable.
	selfPositionMainEnv = "GOPI_TEST_SELFPOSITION_MAIN"
	selfPositionWTEnv   = "GOPI_TEST_SELFPOSITION_WT"
)

// TestSelfPositionChild is the re-exec'd body: it stands in the worktree, reads
// the position, moves to the main checkout, and reads it again. The SECOND read
// is the whole assertion — before the fix it still reported the worktree.
func TestSelfPositionChild(t *testing.T) {
	if os.Getenv(selfPositionChildEnv) == "" {
		t.Skip("helper — driven by TestSelfPositionFollowsACwdChange")
	}
	main := os.Getenv(selfPositionMainEnv)
	wt := os.Getenv(selfPositionWTEnv)
	if main == "" || wt == "" {
		t.Fatalf("the helper needs %s and %s", selfPositionMainEnv, selfPositionWTEnv)
	}
	if err := os.Chdir(wt); err != nil {
		t.Fatalf("chdir %s: %v", wt, err)
	}
	first := worktreeSweepSelfPosition()
	t.Logf("IN WORKTREE repo=%q common=%q guest=%v", first.Repo, first.Common, first.Guest)
	if first.Repo == "" {
		t.Fatalf("standing in the linked worktree %s, the position must resolve — got an empty repo", wt)
	}
	if !first.Guest {
		t.Fatalf("a process standing in the linked worktree %s must read as a guest, got guest=false (repo=%q)", wt, first.Repo)
	}
	// The MOVE — the measured flow's own order (the close-out hands the cwd
	// back to the main checkout / the root before the cleanup runs).
	if err := os.Chdir(main); err != nil {
		t.Fatalf("chdir %s: %v", main, err)
	}
	second := worktreeSweepSelfPosition()
	t.Logf("IN MAIN     repo=%q common=%q guest=%v", second.Repo, second.Common, second.Guest)
	if second.Repo == "" {
		t.Fatalf("after moving to %s the position must resolve, got an empty repo", main)
	}
	if second.Repo == first.Repo {
		t.Fatalf("STALE IDENTITY: after chdir to %s the position still reports %q — the memo outlived the move, which is exactly the refusal that kept the resumed run's worktree on disk (the cleanup then calls itself \"the checkout THIS process is standing in\")",
			main, second.Repo)
	}
	if second.Guest {
		t.Fatalf("standing in the MAIN checkout %s the process must not read as a guest (repo=%q)", main, second.Repo)
	}
	// THE OTHER DIRECTION — a released base must not become a stale "I am
	// somewhere else". A process that moves BACK into the worktree is standing
	// there again, so the rung must refuse again: an implementation that
	// released the previous CWD base (dropping the memo key) without
	// re-measuring would leave the identity resolved from the checkout it just
	// left, and `git worktree remove` would delete the directory under the
	// process for real. Measured 2026-09-25: the release of the base is what
	// frees the checkout the process DEMONSTRABLY stood in (the fail-
	// unmeasurable fallback in worktreeSweepSelfPosition); the sequence here is
	// that fallback's own shape, taken in one process.
	if err := os.Chdir(wt); err != nil {
		t.Fatalf("chdir back to %s: %v", wt, err)
	}
	third := worktreeSweepSelfPosition()
	t.Logf("BACK IN WT  repo=%q common=%q guest=%v", third.Repo, third.Common, third.Guest)
	if third.Repo != first.Repo {
		t.Fatalf("returning to %s must re-measure the position, got repo=%q (want %q) — a released base that is never re-measured leaves the rung answering about a checkout the process has left",
			wt, third.Repo, first.Repo)
	}
	if !third.Guest {
		t.Errorf("standing in the linked worktree %s again must read as a guest, got guest=false", wt)
	}
}

// TestSelfPositionFollowsACwdChange is the guard: it builds a real repo with a
// real linked worktree, runs the helper in its own process, and fails when the
// position does not follow the move.
func TestSelfPositionFollowsACwdChange(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	main := filepath.Join(root, "main")
	wt := filepath.Join(root, "wt")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git(root, "init", main)
	git(main, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(main, "add", "a.txt")
	git(main, "commit", "-m", "init")
	git(main, "worktree", "add", wt, "-b", "feat/self-position")

	cmd := exec.Command(os.Args[0], "-test.run=^TestSelfPositionChild$", "-test.v")
	cmd.Env = append(os.Environ(),
		selfPositionChildEnv+"=1",
		selfPositionMainEnv+"="+main,
		selfPositionWTEnv+"="+wt,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the actor-position memo did not follow a cwd change: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "IN MAIN") {
		t.Fatalf("the helper did not report its second read — the guard measured nothing:\n%s", out)
	}
}
