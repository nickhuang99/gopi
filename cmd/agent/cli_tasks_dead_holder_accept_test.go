package main

// Shell-level acceptance for the dead-holder reclaim (epic 2026-09-12,
// defect 2): the reclaim arm must take a branch from "refused by
// scripts/safe-merge.sh gate [c]" to "mergeable", WITHOUT touching the branch
// (the commit is the deliverable).
//
// WHAT DRIVES IT, and why the fixture lives here: the test drives the reclaim
// at its LIVE face — the MERGER pass's mergeDeadHolderReclaimLeg, reached
// through mergerSerialFaceLegs (see TestMergerPass_ReclaimsDeadHolderWorktree
// below) — over a sandboxed ledger + a local git repo, then runs the REAL
// scripts/safe-merge.sh, so the two halves the epic couples — ledger staleness
// and worktree occupancy — are verified end to end rather than in isolation.
//
// The file is shared by two test packages' worth of concerns on purpose: the
// FIXTURE (a repo + a linked worktree + a terminal row with a provably dead
// holder + the real safe-merge gate) is the expensive half and is
// mechanism-agnostic, which is why it survived the 58b5618b retirement of the
// sweep it was first written for, while the behavioural test that named that
// sweep's tick moved here to name the MERGER pass, which is what runs it now.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/tools"
)

// deadHolderRepoRoot is the repo the test builds its fixture in — a temp dir,
// never the developer's checkout.
// deadHolderRepoRoot is the repo the test builds its fixture in — a temp dir,
// never the developer's checkout.
func deadHolderModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			return root
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Skip("go.mod not found above the test dir")
		}
		root = parent
	}
}

// deadHolderAcceptanceFixture builds a repo with a linked worktree on a feat
// branch, and a sandboxed ledger + settings holding a TERMINAL row whose dead
// claim pid still "holds" it. Returns (repo, worktree, branch, tip).
// deadHolderAcceptanceFixture builds a repo with a linked worktree on a feat
// branch, and a sandboxed ledger + settings holding a TERMINAL row whose dead
// claim pid still "holds" it. Returns (repo, worktree, branch, tip).
func deadHolderAcceptanceFixture(t *testing.T, taskID string) (repo, wt, branch string, tip string) {
	t.Helper()
	return deadHolderAcceptanceFixtureStatus(t, taskID, tools.TaskStatusCompleted)
}

// deadHolderAcceptanceFixtureStatus is the same fixture with the ROW's status
// as a parameter: the orphan epic (2026-09-12) needs the identical world with
// an in_progress row — the shape the terminal-keyed scans never reached. The
// in_progress_at referent is stamped with the claim so the row is a healthy
// fresh-stamp row (the case the epic is about: a REACHABLE orphan, not a
// stale-stamp one, which stays unverifiable by design).
// deadHolderAcceptanceFixtureStatus is the same fixture with the ROW's status
// as a parameter: the orphan epic (2026-09-12) needs the identical world with
// an in_progress row — the shape the terminal-keyed scans never reached. The
// in_progress_at referent is stamped with the claim so the row is a healthy
// fresh-stamp row (the case the epic is about: a REACHABLE orphan, not a
// stale-stamp one, which stays unverifiable by design).
func deadHolderAcceptanceFixtureStatus(t *testing.T, taskID, status string) (repo, wt, branch string, tip string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	// The ledger is a file of ITS OWN PROFILE: this fixture drives a merger pass
	// whose stale/failed reports go through deliverTaskNotify, and a ledger beside
	// the profile makes every one of them FOREIGN — recorded and reported, never
	// broadcast (measured 2026-11-04, task 01a1a69f).
	_, _ = sandboxProfileLedger(t)
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte(`{"sync_peers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.SettingsEnv, settings)
	t.Setenv(config.SyncPeersEnv, "")

	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	wt = filepath.Join(root, "wt")
	branch = "feat/dead-holder-accept"
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", repo)
	run(repo, "config", "user.email", "t@t")
	run(repo, "config", "user.name", "t")
	run(repo, "checkout", "-b", "master")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "a.txt")
	run(repo, "commit", "-m", "init")
	run(repo, "checkout", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "deliverable.txt"), []byte("the delivery"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(repo, "add", "deliverable.txt")
	run(repo, "commit", "-m", "the deliverable")
	tip = run(repo, "rev-parse", branch)
	run(repo, "checkout", "master")
	run(repo, "worktree", "add", wt, branch)

	dead := deadHolderAcceptancePID(t)
	ctx := context.Background()
	db, err := tools.OpenTasksDBForSweep(ctx)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer db.Close()
	now := float64(time.Now().Unix())
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tasks (task_id, summary, status, deliverable, source, claimed_by, claimed_at, created_at, updated_at, in_progress_at) VALUES (?,?,?,?,'auto',?,?,?,?,?)`,
		taskID, "acceptance", status,
		`{"repo":"`+repo+`","branch":"`+branch+`","worktree":"`+wt+`"}`,
		deadHolderStamp(t, dead), now, now, now, now); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	return repo, wt, branch, tip
}

// deadHolderAcceptancePID returns a provably dead pid (a real child, reaped).
// deadHolderAcceptancePID returns a provably dead pid (a real child, reaped).
func deadHolderAcceptancePID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	return pid
}

// deadHolderStamp renders the "host:pid" stamp the way the ledger writes it.
// deadHolderStamp renders the "host:pid" stamp the way the ledger writes it.
func deadHolderStamp(t *testing.T, pid int) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Skipf("no hostname: %v", err)
	}
	return host + ":" + strconv.Itoa(pid)
}

// safeMerge runs the REAL scripts/safe-merge.sh in repo and returns
// (exitCode, combinedOutput).
// safeMerge runs the REAL scripts/safe-merge.sh in repo and returns
// (exitCode, combinedOutput).
func safeMerge(t *testing.T, repo, branch string) (int, string) {
	t.Helper()
	script := filepath.Join(deadHolderModuleRoot(t), "scripts", "safe-merge.sh")
	cmd := exec.Command("bash", script, branch)
	cmd.Dir = repo
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("safe-merge: %v: %s", err, out)
		}
	}
	return code, string(out)
}

// TestAcceptance_DeadHolderReclaimUnblocksSafeMerge is the epic's end-to-end
// acceptance: BEFORE the sweep runs, scripts/safe-merge.sh REFUSES the branch
// (gate [c]: still occupied by a worktree); AFTER one sweep pass, the SAME
// gate exits 0 with the branch tip untouched.

// TestAcceptance_LiveHolderKeepsSafeMergeRefused is the negative end-to-end:
// the SAME fixture, except a live process is working INSIDE the directory (its
// cwd is the worktree). Two things must hold together — the sweep's reclaim
// leaves the directory alone, and gate [c] keeps refusing. Note the row is
// terminal and its claim pid IS dead here: the ONLY thing protecting the
// directory is the live-cwd guard, which is exactly the guard this test
// exists to pin (a pid-only reclaim would delete a running session's cwd).

// TestAcceptance_VanishedWorktreeLeavesNoFailureLine is the regression test for
// the operator-visible symptom the worker printed at startup, pass after pass —
// the capture this epic started from (session 01a09569):
//
//	[task-notify] dead-holder reclaim feat/x (t-…) remove failed: worktree
//	  remove <path>: exit status 128: fatal: '<path>' is not a working tree
//
// Every path in that capture stat'd as ABSENT. The sweep must therefore
// (a) never attempt a removal for a directory that is not there — no
// "remove failed" line, however many passes run, (b) PRUNE the stale
// registration so gate [c] stops blocking the branch, which is the whole reason
// the file exists, and (c) report nothing on a second pass: the state is
// repaired, not re-failing.

// mustGit runs git in dir and returns stdout, failing the test on error.
// mustGit runs git in dir and returns stdout, failing the test on error.
func mustGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}

// TestAcceptance_DeadLocalHolderOrphanReleasesItsDirectory is the ORPHAN epic's
// end-to-end acceptance (2026-09-12): the row is IN_PROGRESS (not terminal) and
// its claim pid is provably DEAD, in a repo whose branch is parked in a clean
// linked worktree. Before the fix BOTH arms protected that directory forever —
// the reclaim scan was keyed on terminal rows, and the ledger-liveness gate
// called the live status a holder by design — so gate [c] refused the branch
// until a human cleared it BY HAND (no automatic disposer exists since
// 2026-10-26: devdoc/zombie-sweep-removed.md).
//
// The acceptance is the epic's contract, driven through the REAL sweep and the
// REAL scripts/safe-merge.sh: ONE lifecycle pass frees the directory, gate [c]
// flips refuse → exit 0, the branch tip is byte-identical, and the deliverable
// file is still on the branch.

// TestAcceptance_DeadLocalHolderLiveTwinStaysRefused is the conservative twin,
// end to end: the SAME in_progress shape and the SAME parked directory, with the
// only difference being that the claim pid is ALIVE (this test process). The
// sweep must leave the directory alone — gate [c] keeps refusing — so the
// release can never be attributed to anything but provable death on this host.

// TestMergerPass_ReclaimsDeadHolderWorktree covers the dead-holder reclaim at
// the seam it LIVES on since 2026-10-30 (task 01a18799, slice 3/4; 58b5618b
// retired the worker-tick sweep that used to run it): the MERGER pass.
//
// WHAT IT PINS, and why that is the real value: a row whose SIGKILLed holder
// left a clean linked worktree behind must have that directory freed so the
// branch stops being withheld from merging — the REAL scripts/safe-merge.sh
// gate [c] flips refuse → exit 0, and the branch tip is untouched (the branch
// IS the deliverable).
//
// It is driven through mergerSerialFaceLegs, the pass's non-tick legs
// definition, and NOT through mergerPassOnce: the tick classification (is this
// pass due?) belongs to the other tests in this package, and a behavioural
// assertion that also had to satisfy it would be measuring the clock.
//
// Nothing about a stuck row's ledger history is rewritten here: this arm frees
// a DIRECTORY left by a dead process, which is a resource leak and a gate-[c]
// blocker, not a ledger verdict — the reason the leg survived the retirement
// that deleted the row-rewriting sweep around it.
func TestMergerPass_ReclaimsDeadHolderWorktree(t *testing.T) {
	repo, wt, branch, tip := deadHolderAcceptanceFixture(t, "t-zw-reclaim")
	// The merger's own face reads its repos from settings (ResolveMergerRepos),
	// so the fixture — which writes a peer-less settings document — gets its
	// repo DECLARED here. Without it the serialize-half of the pass would run
	// over an empty table and the legs would have nothing of this fixture's to
	// look at. An EMPTY build makes it a pure merge target (Rule 12).
	settingsPath := os.Getenv(config.SettingsEnv)
	if err := os.WriteFile(settingsPath, []byte(`{"sync_peers":[],"repos":{"`+repo+`":{}}}`), 0o644); err != nil {
		t.Fatalf("declare the fixture repo: %v", err)
	}
	// A PRIVATE PROFILE: the merger's close loop resolves its durable-mark
	// directory through config.GopiDir(), which without GOPI_HOME is the
	// operator's live ~/.gopi — a pass must never create directories there.
	// (the profile is already pinned by sandboxProfileLedger above — re-pinning
	// GOPI_HOME HERE would un-pin the ledger from it and re-open the foreign shape)
	sink := filepath.Join(t.TempDir(), "notify.log")
	t.Setenv("GOPI_TASK_NOTIFY_CMD", "cat >> "+sink)
	t.Setenv("GOPI_TASK_NOTIFY_EVENTS", "stale,failed")

	if code, out := safeMerge(t, repo, branch); code == 0 {
		t.Fatalf("fixture precondition: gate [c] must refuse the dead holder's branch:\n%s", out)
	}

	out := captureStdout(t, func() { mergerSerialFaceLegs(&tasksOptions{}) })
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("one merger pass must free the dead holder's directory (stat err=%v, pass output=%s)", err, out)
	}
	code, mergeOut := safeMerge(t, repo, branch)
	if code != 0 {
		t.Fatalf("safe-merge must exit 0 once the pass freed the directory:\n%s", mergeOut)
	}
	if now := strings.TrimSpace(string(mustGit(t, repo, "rev-parse", branch))); now != tip {
		t.Fatalf("the branch tip must be untouched (the branch IS the deliverable): %s → %s", tip, now)
	}
	if err := exec.Command("git", "-C", repo, "cat-file", "-e", branch+":deliverable.txt").Run(); err != nil {
		t.Fatalf("the deliverable file must still be on the branch: %v", err)
	}
}
