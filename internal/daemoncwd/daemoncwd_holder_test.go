package daemoncwd

// daemoncwd_holder_test.go — the TEST-ARTIFACT holder predicate's tests (epic
// verify-no-verdict-keeps-merge, 2026-09-14; two-channel revision later in the
// same epic).
//
// WHY THIS FILE EXISTS AT ALL. IsTestScratchHolder is the reclaim arm's escape
// from a deadlock that no amount of producer-side hygiene can clear: a daemon
// from an EARLIER commit's test suite is still alive, pinned to a task worktree,
// and the reclaim arm refuses that worktree forever because the holder never
// exits. The predicate's whole value is its PRECISION — a false positive
// quietly authorises deleting a directory out from under a live process — so
// both directions are pinned here.
//
// THE TWO CHANNELS ARE PINNED IN SEPARATE FILES, because they answer the two
// halves of the measured residue and a single file invites the mistake this
// predicate actually made:
//
//	this file        the CWD channel (a process standing in the scratch tree)
//	daemoncwd_env_holder_test.go   the ENVIRONMENT channel (a process whose
//	                 exec-time env names a scratch path, whatever its cwd is)
//
// The environment channel is the one the measured leftovers needed: their cwd
// is a TASK WORKTREE, not the scratch tree, so a cwd-only predicate classified
// all eight of them as ordinary holders (MEASURED, this host, 2026-09-14).
//
// Every case drives the REAL /proc interface on a REAL child process — its cwd
// and environment are set by the parent, and the verdict is read back from the
// kernel's record of that child.

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/scratch"
	"github.com/nick/gopi/internal/testutil"
)

// TestIsTestScratchHolderRejectsInvalidPids pins the trivial half: a non-process
// cannot be a holder (a directory name that parses as a pid but has no live
// process must never veto or authorise anything).
func TestIsTestScratchHolderRejectsInvalidPids(t *testing.T) {
	for _, pid := range []int{0, -1} {
		if IsTestScratchHolder(pid) {
			t.Errorf("pid %d must never classify as a holder", pid)
		}
	}
}

// TestIsTestScratchCwdClassifiesPaths pins the cwd channel as a PATH question
// (which is what the reclaim arm asks when it holds cwd evidence without a live
// pid), including the two exclusions that keep the predicate from out-voting a
// legitimate holder.
func TestIsTestScratchCwdClassifiesPaths(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host — the predicate has nothing to classify against")
	}
	inside := filepath.Join(home, "holder-predicate-test")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", inside, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(inside) }) //stylegate:ignore test cleanup of the fixture directory it created

	if !IsTestScratchCwd(inside) {
		t.Errorf("%s is inside the scratch home — it must classify as a scratch-test location", inside)
	}
	escape := filepath.Join(home, runtimeDirName, EscapeDirLeaf)
	if IsTestScratchCwd(escape) {
		t.Errorf("%s is the daemon escape root — a legitimate immortal home, never a test location", escape)
	}
	// A path OUTSIDE the scratch home must not classify. It is built from the
	// scratch home's own PARENT rather than from t.TempDir(): this suite pins
	// its temp root into the scratch home (Rule 22), so a t.TempDir() here IS
	// inside the test tree and would prove nothing. No filesystem write happens
	// — the classification is a path question.
	outside := filepath.Join(filepath.Dir(home), "daemoncwd-outside-the-scratch-home")
	if IsTestScratchCwd(outside) {
		t.Errorf("%s is outside the scratch home — it must not classify as a scratch-test location", outside)
	}
	// The scratch home itself IS the test tree (a daemon standing at its root
	// escaped nowhere).
	if !IsTestScratchCwd(home) {
		t.Errorf("%s (the scratch home) must classify as the test tree", home)
	}
}

// TestIsTestScratchHolderClassifiesADaemonByItsCwd is the cwd channel on a REAL
// child: a daemon-shaped process standing inside the scratch test tree is an
// artifact. Its environment is deliberately CLEAN, so the verdict can only come
// from the cwd channel.
func TestIsTestScratchHolderClassifiesADaemonByItsCwd(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	inside := filepath.Join(home, "holder-cwd-channel-test")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", inside, err)
	}
	mp := envHolderChild(t, inside, childBaseEnv(t))

	if !IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d stands in %s with a daemon argv[0] — the cwd channel must classify it as a test artifact",
			mp.Pid(), inside)
	}
}

// TestIsTestScratchHolderKeepsADaemonStandingAtTheEscapeRoot is the
// anti-weakening control, and the reason the escape root is excluded: a daemon
// that DID escape stands there for its whole immortal life, so out-voting it
// would authorise deleting worktrees out from under healthy daemons.
func TestIsTestScratchHolderKeepsADaemonStandingAtTheEscapeRoot(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	escape := filepath.Join(home, runtimeDirName, EscapeDirLeaf)
	if err := os.MkdirAll(escape, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", escape, err)
	}
	mp := envHolderChild(t, escape, childBaseEnv(t))

	if IsTestScratchHolder(mp.Pid()) {
		t.Errorf("pid %d stands in the daemon escape root %s — a correctly escaped daemon is a legitimate holder",
			mp.Pid(), escape)
	}
}

// TestIsTestScratchHolderClassifiesTheCurrentProcess is the SELF control: this
// test binary is a REAL process whose cwd the test can move, and it must never
// classify as a test ARTIFACT — it is not daemon-shaped, so no environment and
// no cwd makes it one. That is the same precision the reclaim arm depends on
// (the suite's own children are evidence about a worktree, not artifacts with
// no interest in it).
func TestIsTestScratchHolderClassifiesTheCurrentProcess(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host")
	}
	pid := os.Getpid()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// t.Chdir/cleanup ordering: restore before the test's own TempDir cleanup
	// runs (a cwd inside a removed directory would break the framework).
	t.Cleanup(func() { _ = os.Chdir(orig) }) //stylegate:ignore test cleanup restoring this process's own cwd

	inside := filepath.Join(home, "holder-predicate-test")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", inside, err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(inside) }) //stylegate:ignore test cleanup of the fixture directory it created
	if err := os.Chdir(inside); err != nil {
		t.Fatalf("chdir %s: %v", inside, err)
	}
	if IsTestScratchHolder(pid) {
		t.Errorf("pid %d must NOT classify as an artifact: it is not daemon-shaped, so a suite's own child is never out-voted", pid)
	}
	// ...while the PATH question still answers true for the directory it stands
	// in: the two questions are deliberately separate.
	if !IsTestScratchCwd(inside) {
		t.Errorf("%s must classify as a scratch-test location", inside)
	}
	if err := os.Chdir(orig); err != nil {
		t.Fatalf("chdir back to %s: %v", orig, err)
	}
}

// TestWorktreeFixtureRootIsUniquePerCase is the REGRESSION GUARD for the
// geometry that reverted this package (Rule 5: a bug fix ships the minimal
// repro). Two cases must never share a fixture root: the shared path is what
// let the first t.Cleanup RemoveAll delete the directory a sibling's live child
// was standing in, which is the measured failure (3 of 48 concurrent runs, all
// reporting "never reported cwd …/daemoncwd-holder-fixtures/worktree-shaped-cwd").
//
// It is deliberately structural — no child process, no /proc — so it fails from
// the geometry alone, and it keeps the Rule 22 criterion honest: the fixture
// root must not be on the boot device (the criterion is measured, never spelled).
func TestWorktreeFixtureRootIsUniquePerCase(t *testing.T) {
	home := scratch.HomeDir()
	if home == "" {
		t.Skip("no scratch home on this host — the fixture root needs its measured parent")
	}
	first := testWorktreeDir(t, daemonEnvHolderWorktree)
	second := testWorktreeDir(t, daemonEnvHolderWorktree)
	if first == second {
		t.Fatalf("two cases share the fixture directory %s — a sibling's cleanup deletes a live child's cwd", first)
	}
	sep := string(filepath.Separator)
	if strings.HasPrefix(first, second+sep) || strings.HasPrefix(second, first+sep) {
		t.Fatalf("fixture roots are nested (%s / %s) — one case's cleanup would remove the other's tree", first, second)
	}
	for _, dir := range []string{first, second} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("fixture directory %s does not exist: %v", dir, err)
		}
		if shares, err := testutil.ScratchRootSharesRootFS(dir); err != nil {
			t.Errorf("cannot measure the fixture root's filesystem (%s): %v", dir, err)
		} else if shares {
			t.Errorf("fixture directory %s is on the `/` partition — Rule 22 requires a MEASURED off-root location", dir)
		}
	}
	if IsTestScratchPath(first) {
		t.Errorf("%s is inside the scratch tree — the cwd channel would not be silent and the case would prove nothing", first)
	}
}

// --- shared re-exec'd-child helpers (used by BOTH channel files) ---

// daemonEnvHolderDefaultHold is the helper's default hold, in seconds: long
// enough for the parent to read /proc, short enough to bound a hanging failure.
const daemonEnvHolderDefaultHold = 120

// envHolderHelperFlag is the argument that identifies the re-exec'd helper on
// the child's COMMAND LINE — the flag the parent passes when it starts the
// helper, and therefore the proof that the child has EXEC'd (see
// procExecLanded). It is a named constant because two call sites must agree on
// it: the spawn and the readiness check (Rule 1b: a bare string compared in two
// places is one typo away from a readiness check that never becomes true).
const envHolderHelperFlag = "-test.run=TestHelperEnvHolder"

// waitForProcCwd blocks until pid's /proc record is BOTH the child's own and
// standing in dir (or fails the test): the measurement the cases rest on must be
// the kernel's answer, and a child that has not chdir'd yet would otherwise be
// classified as its parent's directory.
//
// WHY THE CWD MATCH IS NOT SUFFICIENT (measured, this package's own concurrency
// probe): the kernel does not publish a process's /proc records at one moment.
// /proc/<pid>/cwd is already the child's NEW directory — the fork copied it — while
// /proc/<pid>/cmdline and /proc/<pid>/environ still report the LAUNCHER's records,
// which only execve replaces (atomically). A caller that returned on the cwd match
// alone could therefore classify a child that stands in the fixture but has not
// exec'd: its argv[0] is the launcher's (no daemon prefix ⇒ "not a daemon") and its
// environment is the launcher's (no scratch variable ⇒ "not an artifact"), so every
// positive case is spuriously red.
//
// The window is MEASURED, not assumed (probe: 200 spawns of a child given an
// explicit cwd and an argv[0] marker, cwd read before cmdline every iteration):
// 50/200 spawns observed cwd=new while cmdline still lacked the marker, and 0/200
// the reverse. One spawn in four — enough that a loaded host samples it under
// concurrency, which is how this case's own 1-in-48 failure arose.
func waitForProcCwd(t *testing.T, pid int, dir string) {
	t.Helper()
	want := ResolvePrefix(dir)
	for i := 0; i < procCwdWaitAttempts; i++ {
		if procExecLanded(pid) {
			if cwd, ok := ProcessCwd(pid); ok && ResolvePrefix(cwd) == want {
				return
			}
		}
		time.Sleep(procCwdPollInterval) //stylegate:allow-sleep bounded /proc poll: a child's exec+chdir is immediate, but the kernel publishes the two records asynchronously, so the loop re-reads until both land (procCwdWaitAttempts caps it; the failure path is a Fatalf, never a silent pass)
	}
	got, _ := ProcessCwd(pid)
	t.Fatalf("pid %d never reported cwd %s (got %q, exec landed %t) — the measurement is unavailable",
		pid, want, got, procExecLanded(pid))
}

// procExecLanded reports whether pid has EXEC'd the helper, i.e. whether the
// kernel's record of this process is the CHILD's own and no longer the fork's
// copy of the launcher's. The helper's own flag on the command line is the
// evidence: execve installs the new argv atomically, and the flag is passed by
// the parent (envHolderHelperFlag), so it cannot be present before that moment —
// whereas the launcher's argv and environ ARE what /proc reports in between.
//
// An unreadable or not-yet-installed cmdline is NOT evidence of anything, so it
// answers false and the caller keeps waiting (Rule 15: a missing measurement is
// never a classification).
func procExecLanded(pid int) bool {
	if pid <= 0 {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(procRootDir, strconv.Itoa(pid), procCmdlineName))
	if err != nil {
		return false
	}
	return bytes.Contains(raw, []byte(envHolderHelperFlag))
}

// procCwdWaitAttempts / procCwdPollInterval bound waitForProcCwd's poll: a child
// exec + chdir is immediate, so this is the harness's patience, not a policy
// knob (5 ms × 200 ≈ 1 s).
const (
	procCwdWaitAttempts = 200
	procCwdPollInterval = 5 * time.Millisecond
)
