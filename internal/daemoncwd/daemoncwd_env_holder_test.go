package daemoncwd

// daemoncwd_env_holder_test.go — the SECOND evidence channel of the
// test-artifact holder classification (epic verify-no-verdict-keeps-merge,
// 2026-09-14).
//
// WHY THIS FILE EXISTS. The cwd-only predicate could not see the residue it was
// written for. MEASURED on this host (2026-09-14, the eight live leftovers of an
// earlier commit's cmd/agent suite):
//
//	cwd      = /home/nick/workspace/gopi-<task>/cmd/agent   ← a TASK WORKTREE
//	GOPI_HOME= /dev/shm/gopi-test-scratch/suite-cmd-agent-run-…/001
//	TMPDIR   = /dev/shm/gopi-test-scratch/suite-cmd-agent-run-…
//
// The cwd is NOT in the scratch tree, so cwd-only classification read every one
// of them as an ordinary holder and the reclaim arm stayed deadlocked on exactly
// the case this epic dispatched. The exec-time ENVIRONMENT is the channel that
// still sees them.
//
// Every test below drives the REAL /proc path on a REAL child process: the child
// is started with an explicit, fully controlled environment and cwd, and the
// predicate's verdict is read from the kernel's record of that child —
// /proc/<pid>/environ and /proc/<pid>/cwd. Nothing is simulated.
//
// Tests in this package run sequentially (Go runs a package's tests one at a
// time unless t.Parallel is called), which is what lets a test decorate THIS
// process's own environment and watch that the verdict for a CLEAN candidate
// stays false (t.Setenv, restored by the framework), and because the child is
// spawned with an explicit environment, never os.Environ() inheritance, so
// nothing leaks either way.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

const (
	// daemonEnvHolderMarker wakes the re-exec'd helper; without it the helper
	// skips, so a normal `go test` run never blocks.
	daemonEnvHolderMarker = "DAEMONCWD_TEST_ENV_HOLDER_MARKER"
	// daemonEnvHolderHold is how long the helper stays alive — long enough for
	// the parent to read its /proc files, short enough to bound a failure.
	daemonEnvHolderHold = "DAEMONCWD_TEST_ENV_HOLDER_HOLD"
	// daemonEnvHolderWorktree is the cwd of channel-②'s children: a directory
	// that is NOT in the scratch tree, i.e. the measured incident shape (a task
	// worktree) where the cwd channel is silent by construction.
	daemonEnvHolderWorktree = "worktree-shaped-cwd"
)

// TestHelperEnvHolder is NOT a test: re-exec'd by the cases below, it blocks so
// the parent can classify a live process. Its cwd and environment are set by the
// PARENT (cmd.Dir / cmd.Env) — never inherited, so the parent's own environment
// cannot accidentally satisfy the predicate.
func TestHelperEnvHolder(t *testing.T) {
	if os.Getenv(daemonEnvHolderMarker) == "" {
		t.Skip("env-holder helper — only runs as the re-exec'd child of the classification tests")
	}
	hold := daemonEnvHolderDefaultHold
	if v := os.Getenv(daemonEnvHolderHold); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			hold = secs
		}
	}
	time.Sleep(time.Duration(hold) * time.Second) //stylegate:allow-sleep this process IS the observation: the parent reads its /proc record while it stays alive
}

// envHolderChild starts the helper with EXACTLY the given environment and cwd —
// no os.Environ() inheritance, so the predicate's verdict is attributable to the
// values this test chose.
func envHolderChild(t *testing.T, workdir string, env []string) *testutil.MockProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], envHolderHelperFlag)
	cmd.Dir = workdir
	cmd.Args[0] = Argv0Prefix + "merger" // the DAEMON identity channel
	cmd.Env = env
	mp := testutil.StartMockProcessCmd(t, cmd)
	t.Cleanup(mp.Reap)
	waitForProcCwd(t, mp.Pid(), workdir)
	return mp
}

// testFixtureRootPrefix names the per-case fixture root testWorktreeDir creates
// as a SIBLING of the scratch home (the trailing "-" is MkdirTemp's seam for the
// random suffix that makes the root unique).
const testFixtureRootPrefix = "daemoncwd-holder-fixtures-"

// testWorktreeDir makes a WORKTREE-SHAPED fixture directory that is NOT inside
// the scratch test tree. It is deliberately not t.TempDir(): this suite pins its
// temp root INTO the scratch home (Rule 22 / devdoc/tmp-ban-scratch-root.md), so
// every t.TempDir() here IS the test tree, and a case whose whole point is "the
// cwd channel must be silent" cannot be built from it.
//
// THE ROOT MUST BE UNIQUE PER CASE, and that is the one property this helper's
// first version got wrong (it reverted the branch). The fixtures must live
// OUTSIDE the scratch tree, so its first version rooted them at the scratch
// home's own parent — a HOST-GLOBAL directory every concurrent run of this
// package shares — and gave five cases the same leaf. Two runs (or two cases)
// therefore built the SAME path, and the first t.Cleanup RemoveAll deleted the
// directory a sibling's live child was standing in: /proc/<pid>/cwd then answers
// an unreadable cwd (an empty path, ok=false), the case reports "the measurement itself is broken" / "never
// reported cwd", and the package is flaky exactly where it must not be.
//
// os.MkdirTemp is what makes the root unique (per case, and per concurrent run),
// and its parent is the scratch home's own parent — a directory the suite has
// ALREADY certified as off `/` at TestMain time (RunSuiteMain refuses a scratch
// root on the boot device, and the parent is on the same filesystem), so the
// Rule 22 criterion is measured, never spelled. A t.Cleanup removes this case's
// own root only; no sibling can be standing in it.
func testWorktreeDir(t *testing.T, leaf string) string {
	t.Helper()
	home := strings.TrimSpace(scratch.HomeDir())
	if home == "" {
		t.Skip("no scratch home on this host — the fixture root needs its measured parent")
	}
	root, err := os.MkdirTemp(filepath.Dir(home), testFixtureRootPrefix)
	if err != nil {
		t.Fatalf("mkdir fixture root under %s: %v", filepath.Dir(home), err)
	}
	dir := filepath.Join(root, leaf)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) }) //stylegate:ignore test cleanup of the fixture directory it created
	return dir
}

// childBaseEnv is the minimum a Go test binary needs to start, and it carries
// NO scratch variable at all — the clean baseline both directions are measured
// against. HOME is set to this package's own directory: a REAL directory that is
// outside the scratch tree (the suite pins its temp root INTO the scratch home,
// Rule 22, so t.TempDir() can no longer stand for "elsewhere"), and HOME is not
// one of the two classified channels anyway.
func childBaseEnv(t *testing.T) []string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + wd,
	}
}

// envWith appends one NAME=value pair to the baseline.
func envWith(base []string, pair string) []string {
	return append(append([]string{}, base...), pair)
}

// TestIsTestScratchHolderClassifiesADaemonByItsEnvironment is the case the
// cwd-only predicate could not see, and the reason channel ② exists: the child
// stands in a WORKTREE-shaped directory (so the cwd channel reports "not a test
// tree") while its exec-time TMPDIR names the scratch tree — the measured
// residue shape. The predicate must classify it as a test artifact.
func TestIsTestScratchHolderClassifiesADaemonByItsEnvironment(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host — the predicate has nothing to classify against")
	}
	scratchDir := filepath.Join(home, "env-holder-test")
	workdir := testWorktreeDir(t, daemonEnvHolderWorktree)
	for _, dir := range []string{scratchDir, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	mp := envHolderChild(t, workdir,
		envWith(childBaseEnv(t), scratch.TmpDirEnvName+"="+scratchDir))
	pid := mp.Pid()

	// The negative control that makes this test non-vacuous: the CWD channel
	// must be silent for this child (its cwd is not in the scratch tree), so the
	// verdict below can only come from the environment.
	cwd, ok := ProcessCwd(pid)
	if !ok {
		t.Fatalf("pid %d: cwd unreadable — the measurement itself is broken", pid)
	}
	if IsTestScratchPath(cwd) {
		t.Fatalf("this case is only meaningful when the cwd is OUTSIDE the scratch tree, got %s", cwd)
	}
	if !IsTestScratchHolder(pid) {
		t.Errorf("pid %d (cwd %s, TMPDIR %s) must classify as a test artifact via its environment — a cwd-only predicate missed exactly this shape",
			pid, cwd, scratchDir)
	}
}

// TestIsTestScratchHolderRejectsATestTreeLessDaemon is the PRECISION control:
// a daemon that was NOT spawned by a suite (no scratch variable in its
// environment, cwd outside the test tree) is an ordinary holder. Without this,
// the predicate would out-vote every daemon on the host.
func TestIsTestScratchHolderRejectsATestTreeLessDaemon(t *testing.T) {
	workdir := testWorktreeDir(t, daemonEnvHolderWorktree)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", workdir, err)
	}
	mp := envHolderChild(t, workdir, childBaseEnv(t))

	if IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d carries no scratch variable and stands outside the scratch tree — it must NOT classify as a test artifact",
			mp.Pid())
	}
}

// TestIsTestScratchHolderGatesOnTheDaemonShape pins the OTHER half of the
// precision: a process that DID inherit the suite's environment but is not a
// gopi daemon is the suite's own child — the test that spawned it is evidence
// ABOUT the worktree, not an artifact with no interest in it. Out-voting those
// would delete the directory a live test is proving is protected.
func TestIsTestScratchHolderGatesOnTheDaemonShape(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	scratchDir := filepath.Join(home, "env-holder-test-nondaemon")
	workdir := testWorktreeDir(t, daemonEnvHolderWorktree)
	for _, dir := range []string{scratchDir, workdir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	cmd := exec.Command(os.Args[0], envHolderHelperFlag)
	cmd.Dir = workdir
	// argv[0] stays the real test binary — NOT a daemon name.
	cmd.Env = envWith(childBaseEnv(t), scratch.TmpDirEnvName+"="+scratchDir)
	mp := testutil.StartMockProcessCmd(t, cmd)
	t.Cleanup(mp.Reap)
	waitForProcCwd(t, mp.Pid(), workdir)

	if IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d is not daemon-shaped (argv[0] %q) — a suite's own child must never be classified as an artifact",
			mp.Pid(), os.Args[0])
	}
}

// TestIsTestScratchHolderExcludesTheEscapeRoot is the anti-weakening control:
// a daemon standing in the process root DID its job, so it stays a LEGITIMATE
// immortal holder even though that directory is inside the scratch home and its
// environment carries scratch variables. A predicate that out-voted it would
// authorise deleting worktrees out from under healthy daemons.
func TestIsTestScratchHolderExcludesTheEscapeRoot(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	escape := filepath.Join(home, runtimeDirName, EscapeDirLeaf)
	scratchDir := filepath.Join(home, "env-holder-test-escape")
	for _, dir := range []string{escape, scratchDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	mp := envHolderChild(t, escape,
		envWith(childBaseEnv(t), scratch.TmpDirEnvName+"="+scratchDir))

	if IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d stands in the daemon escape root %s — a correctly escaped daemon is a legitimate holder, never an artifact",
			mp.Pid(), escape)
	}
}

// TestIsTestScratchHolderSeesTheSuiteHomeVariable pins the second environment
// signal: GOPI_TEST_SCRATCH_HOME present at all identifies a suite-spawned
// process, whatever its value resolves to (only a test run publishes it).
func TestIsTestScratchHolderSeesTheSuiteHomeVariable(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	workdir := testWorktreeDir(t, daemonEnvHolderWorktree)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", workdir, err)
	}
	mp := envHolderChild(t, workdir,
		envWith(childBaseEnv(t), scratch.HomeEnvName+"="+home))

	if !IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d carries %s — it was spawned by a test run and must classify as an artifact",
			mp.Pid(), scratch.HomeEnvName)
	}
}

// TestIsTestScratchHolderCannotBeFooledByItsOwnEnvironment is the AUTHORITY
// control, and the bug this file was written after. The predicate must answer
// from the CANDIDATE's /proc record, never from the classifying process's own
// environment: a long-lived gopi process (the merger daemon, the CLI) runs with
// scratch variables set all the time in a test session, and an implementation
// that read its OWN TMPDIR would classify every daemon on the host as a test
// artifact — on this host /dev/shm/gopi-test-scratch in TMPDIR is exactly how
// gopi's children are pinned.
//
// The gate is injected via finish-agnostic indirection: this test makes THIS
// process look like a suite-spawned one and re-reads the verdict for the clean
// child, which must stay false.
//
// t.Setenv is exactly right here (Rule 17): the mark must be in place AT the
// assertion and gone after it, which is the testing package's own scope — no
// per-call flip, so this is not the case t.Setenv cannot serve.
func TestIsTestScratchHolderCannotBeFooledByItsOwnEnvironment(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	workdir := testWorktreeDir(t, daemonEnvHolderWorktree)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", workdir, err)
	}
	// The candidate's environment carries NO scratch variable (childBaseEnv's
	// baseline): this suite pins its temp root INTO the scratch home, so a
	// TMPDIR/GOPI_HOME derived from t.TempDir() would hand the child the very
	// mark the assertion is about. HOME is not one of the two channels.
	mp := envHolderChild(t, workdir, childBaseEnv(t))
	if IsTestScratchHolder(mp.Pid()) {
		t.Fatalf("pid %d is clean of the scratch roots but the classifier already called it an artifact — a fixture that short-circuits the assertion", mp.Pid())
	}

	t.Setenv(scratch.HomeEnvName, home)
	t.Setenv(scratch.TmpDirEnvName, filepath.Join(home, "env-holder-test-self"))

	if IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d is clean; the verdict flipped only because THIS process's environment names the scratch tree — the predicate must read the candidate's /proc record",
			mp.Pid())
	}
}
