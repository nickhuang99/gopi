package main

// Merged-branch cleanup serve-pass wiring tests (bugfix 2026-09-02): the
// env parsing (single-source Rule 2), the serve-pass throttle clock, and
// the pass against a sandbox repo fixture. The ENGINE tests (real git
// fixture: remove/dirty-skip/live-skip/recommit-skip) live in
// internal/tools/task_merged_cleanup_test.go.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMergedCleanupIntervalFromEnv(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		set      bool
		interval time.Duration
		disabled bool
		wantErr  bool
	}{
		{name: "unset defaults 15min", raw: "", set: false, interval: mergedCleanupIntervalDefault},
		{name: "empty defaults 15min", raw: "", set: true, interval: mergedCleanupIntervalDefault},
		{name: "0 defaults 15min", raw: "0", set: true, interval: mergedCleanupIntervalDefault},
		{name: "600 overrides", raw: "600", set: true, interval: 600 * time.Second},
		{name: "negative disables", raw: "-1", set: true, disabled: true},
		{name: "garbage is error", raw: "soon", set: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(mergedCleanupIntervalEnv, tc.raw)
			}
			got, disabled, err := mergedCleanupIntervalFromEnv()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error for %q", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.interval {
				t.Errorf("interval = %v, want %v", got, tc.interval)
			}
			if disabled != tc.disabled {
				t.Errorf("disabled = %v, want %v", disabled, tc.disabled)
			}
		})
	}
}

// TestServeMergedCleanupSweepThrottle: the pass cadence — the first sweep
// advances the process clock, a second sweep inside the window is
// throttled, a disabled cadence never advances the clock.
func TestServeMergedCleanupSweepThrottle(t *testing.T) {
	// The sweep resolves its repo from THIS process's working directory
	// (tools.GitSyncRepoRoot) and then removes the merged branches' worktrees of
	// that repo. Without the pin this test stands in the package directory and
	// drives the close-out against the LIVE checkout, so it pins all three
	// inputs the pass reads — settings, ledger and cwd — first. The throttle
	// assertions below are unaffected: a sandboxed cwd resolves no repository,
	// and the cadence clock advances before the repo is ever resolved.
	SandboxRepoContext(t)
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))
	t.Setenv(mergedCleanupIntervalEnv, "600")
	serveMergedCleanupLastTick = time.Time{} // fresh process clock
	serveMergedCleanupSweep()
	first := serveMergedCleanupLastTick
	if first.IsZero() {
		t.Fatalf("first sweep must advance the sweep clock (throttle baseline)")
	}
	serveMergedCleanupSweep()
	if !serveMergedCleanupLastTick.Equal(first) {
		t.Fatalf("second sweep inside the window must be throttled (clock moved %v → %v)", first, serveMergedCleanupLastTick)
	}
	// A disabled cadence stops the sweep entirely (clock never set).
	t.Setenv(mergedCleanupIntervalEnv, "-1")
	serveMergedCleanupLastTick = time.Time{}
	serveMergedCleanupSweep()
	if !serveMergedCleanupLastTick.IsZero() {
		t.Fatalf("disabled cadence must not advance the clock")
	}
}

// serveCleanupFixtureGit runs git with the sandbox identity.
func serveCleanupFixtureGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

// TestServeMergedCleanupSweepReportsFrozenPair: the FULL serve-pass wiring
// against a sandbox repo — a merged branch parked in a linked worktree is
// discovered by the pass and REPORTED, and NOTHING is taken away.
//
// CONVERTED, not deleted (epic sweep-report-not-act, 2026-09-19). Its
// predecessor asserted "worktree gone, branch gone"; it now asserts the
// opposite, because the reaper's mutating arms are retired: on a healthy system
// every branch has a destination (it merges and is deleted, or it cannot merge,
// a task is filed, and it eventually merges and is deleted), so a reaper's only
// observable effect was removing ground still in use. The guard ladder is
// unchanged — it is the disposal that is gone.
func TestServeMergedCleanupSweepReportsFrozenPair(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(sandbox, "memory.db"))
	repo := filepath.Join(sandbox, "repo")
	wt := filepath.Join(sandbox, "wt")
	git := func(dir string, args ...string) string {
		return serveCleanupFixtureGit(t, dir, args...)
	}
	git(sandbox, "init", repo)
	git(repo, "config", "user.email", "t@t")
	git(repo, "config", "user.name", "t")
	git(repo, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644)
	git(repo, "add", "base.txt")
	git(repo, "commit", "-m", "base")
	git(repo, "checkout", "-b", "feat/frozen")
	os.WriteFile(filepath.Join(repo, "feat.txt"), []byte("feat\n"), 0o644)
	git(repo, "add", "feat.txt")
	git(repo, "commit", "-m", "feat work")
	git(repo, "checkout", "master")
	git(repo, "merge", "--no-ff", "-m", "merge feat/frozen", "feat/frozen")
	git(repo, "worktree", "add", wt, "feat/frozen")
	git(repo, "checkout", "master")

	// The serve pass must find the repo (cwd-based GitSyncRepoRoot) — run
	// from inside the repo.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Chdir(wd)
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Setenv(mergedCleanupIntervalEnv, "600")
	serveMergedCleanupLastTick = time.Time{}
	lines := serveMergedCleanupSweep()
	found := false
	for _, l := range lines {
		if strings.Contains(l, "would-reap branch=feat/frozen") {
			found = true
		}
	}
	if !found {
		t.Fatalf("serve pass must REPORT the frozen pair, lines=%v", lines)
	}
	// The two physical absences the old test asserted as removals are now the
	// assertions that the ground SURVIVED.
	if _, err := os.Stat(wt); err != nil {
		t.Errorf("the reaper's mutating arms are retired — the worktree must SURVIVE the pass: %v", err)
	}
	if out, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "feat/frozen").CombinedOutput(); err != nil {
		t.Errorf("the reaper's mutating arms are retired — the branch must SURVIVE the pass: %s", out)
	}
}

// TestServeMergedCleanupSweepNoLongerReapsDeadRegistrations is the
// anti-regression on the CARRIER MOVE (epic worktree-reap-reachable): the
// dead-registration reap used to be appended inside serveMergedCleanupSweep,
// which has had NO production caller since 2026-09-04 — so this test's
// predecessor ("…AlsoReapsDeadRegistrations") was green while no resident tick
// ever ran the reap. The reap then rode taskLifecyclePass — which itself turned
// out to have no production caller and was DELETED on 2026-10-30 (task
// 01a18ef7) — and its carrier today is `gopi task merged-cleanup`
// (mergedCleanupPassOnce). The reachability proof lives in
// worktree_reap_reach_test.go (it drives that verb's own body and asserts the
// sweep RAN and ACTED).
//
// What this test pins is the other half of the move: the retired carrier must
// NOT quietly grow the reap back. Two carriers would mean two ticks racing over
// one registration, and a future reader would have no way to tell which one the
// host actually uses.
//
// The fixture is unchanged (a real `worktree add` + a dead directory + an
// "initializing" lock aged past the in-flight floor — the leak's own shape).
func TestServeMergedCleanupSweepNoLongerReapsDeadRegistrations(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(sandbox, "memory.db"))
	// The sweep's repo list comes from settings.json "repos" plus the CWD's own
	// repo — point BOTH at the sandbox so the pass can only ever touch the
	// fixture (the alternative is it sweeping whatever repo this test runs in).
	settings := filepath.Join(sandbox, "settings.json")
	if werr := os.WriteFile(settings, []byte(`{"repos":{"`+filepath.Join(sandbox, "repo")+`":{}}}`), 0o644); werr != nil {
		t.Fatal(werr)
	}
	t.Setenv("GOPI_SETTINGS", settings)
	// The pass throttle is a PROCESS-wide clock: a sibling test may have just
	// advanced it, which would make this pass a no-op (nothing to assert on).
	serveMergedCleanupLastTick = time.Time{}
	repo := filepath.Join(sandbox, "repo")
	wt := filepath.Join(sandbox, "wt-dead")
	serveCleanupFixtureGit(t, sandbox, "init", repo)
	serveCleanupFixtureGit(t, repo, "config", "user.email", "t@t")
	serveCleanupFixtureGit(t, repo, "config", "user.name", "t")
	serveCleanupFixtureGit(t, repo, "commit", "-q", "--allow-empty", "-m", "seed")
	serveCleanupFixtureGit(t, repo, "worktree", "add", wt, "-b", "feat/dead-clean")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	admin := filepath.Join(repo, ".git", "worktrees")
	entries, err := os.ReadDir(admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("fixture has %d registrations, want 1", len(entries))
	}
	id := entries[0].Name()
	// The lock is dropped by the (simulated) SIGKILL, then removed by nothing:
	// age it past the floor so it reads as abandoned, as it would in the wild.
	lock := filepath.Join(admin, id, "locked")
	if werr := os.WriteFile(lock, []byte("initializing"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	old := time.Now().Add(-2 * time.Hour)
	if cerr := os.Chtimes(lock, old, old); cerr != nil {
		t.Fatal(cerr)
	}

	lines := serveMergedCleanupSweep()
	if _, serr := os.Stat(filepath.Join(admin, id)); serr != nil {
		t.Fatalf("a registration sweep other than the verb's own leg removed the entry (stat err = %v) — "+
			"the reap's carrier is `gopi task merged-cleanup`; two carriers would race over one "+
			"registration; lines=%v", serr, lines)
	}
}
