package main

// cli_daemon_cwd_test.go — the /proc-LEVEL acceptance test of epic
// daemon-cwd-escape.
//
// THE CLAIM UNDER TEST (and why it is measured, never asserted structurally):
// a detached gopi daemon spawned from INSIDE a task's linked worktree must not
// keep that worktree as its working directory. A test asserting `cmd.Dir` was
// assigned proves nothing — the bug this epic fixes was not a missing
// assignment, it was a child that inherited (or was handed) a directory whose
// removal guard ③ must then refuse forever. So the test:
//
//	① creates a REAL linked worktree (git worktree add) — the producer shape;
//	② starts a REAL detached daemon (a real gopi binary, `merger --daemon
//	   --every 1h`) with the TEST PROCESS's cwd INSIDE that worktree, so the
//	   child would inherit it by the pre-fix rule;
//	③ reads /proc/<pid>/cwd — the kernel's answer — and asserts it is not the
//	   worktree, not inside the operator's worktree root, and measurably OFF the
//	   `/` partition. The daemon's IDENTITY is then read from
//	   /proc/<pid>/cmdline[0] (its argv[0]): ancestry is deliberately NOT the
//	   check — a detached daemon is reparented the moment its launcher exits, so
//	   a descendant assertion would be flaky for exactly the processes this
//	   epic protects.
//
// The daemon is started with the cgroup escape OFF on purpose: the test must
// exercise gopi's own cwd decision UNPRIVILEGED (PROJECT.md Rule 25 — no
// sudo/root remedy anywhere in this epic) and must not depend on a systemd user
// bus being reachable from a build host. The measured cwd is then the cwd of
// the process gopi exec'd, with no wrapper around it.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/cgroupscope"
	"github.com/nickhuang99/gopi/internal/daemoncwd"
	"github.com/nickhuang99/gopi/internal/scratch"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/worktreeroots"
)

const (
	// daemonCwdProbeWait bounds how long the test waits for the daemon's
	// /proc/<pid>/cwd to become readable (the child execs first; its cwd link
	// exists as soon as it does). It is a BOUND on a healthy machine, not a
	// sleep: the loop polls and returns the moment the measurement lands.
	daemonCwdProbeWait = 30 * time.Second
	// daemonCwdProbePeriod is the poll interval of that wait.
	daemonCwdProbePeriod = 50 * time.Millisecond
	// daemonCwdTestIdleSec / daemonCwdTestIdleExits keep the spawned WORKER
	// daemon resident with a large idle budget: it must still be the process
	// whose cwd is measured when the assertion runs. These are the worker loop's
	// own knobs (environment, like the operator's), replacing the retired
	// merger's --every poll period.
	daemonCwdTestIdleSec   = "5"
	daemonCwdTestIdleExits = "100"
	// daemonCwdTestStopWait bounds how long the teardown waits for the daemon
	// it killed to be reaped.
	daemonCwdTestStopWait = 10 * time.Second
	// daemonCwdTestStopPeriod is the teardown's poll interval.
	daemonCwdTestStopPeriod = 20 * time.Millisecond
	// daemonCwdAgentArgv0 is the argv[0] a detached daemon advertises
	// (daemonArgv0Prefix + CliVerbWorker). It is the daemon's identity marker:
	// the mechanism sets it, no other gopi process carries it.
	//
	// THE FACE IS THE WORKER'S because the MERGER'S daemon face is deleted
	// (epic daemon-machinery-removal, 2026-09-17): the merger is a cronjob — one
	// tick and out — so a resident merger no longer exists to have a cwd
	// measured. This test measures the CWD DECISION of the detach mechanism, and
	// that mechanism is now reached only through `gopi worker --daemon`, so that
	// is the face it starts. The subject is unchanged; only the surviving face
	// running it changed.
	daemonCwdAgentArgv0 = daemonArgv0Prefix + CliVerbWorker
	// daemonCwdProcRoot / daemonCwdCmdlineName are the procfs interfaces this
	// file's identity read uses. The test reads them directly (rather than
	// through daemoncwd) because argv[0] is the DAEMON MECHANISM's notion of
	// identity, and the mechanism lives in this package.
	daemonCwdProcRoot    = "/proc"
	daemonCwdCmdlineName = "cmdline"
)

// TestDaemonSpawnFromWorktreeEscapesMeasuredCwd is the epic's acceptance (a):
// a daemon started from inside a linked worktree reports, through
// /proc/<pid>/cwd, a directory that is not the worktree.
func TestDaemonSpawnFromWorktreeEscapesMeasuredCwd(t *testing.T) {
	if _, ok := daemoncwd.ProcessCwd(os.Getpid()); !ok {
		t.Skip("no readable procfs on this host — the measurement this test rests on is unavailable")
	}
	wt := daemonCwdTestWorktree(t)
	bin := daemonCwdTestBinary(t)

	// A sandboxed gopi profile, so the daemon's log and any state it touches
	// land in this test's own tree.
	sandbox := t.TempDir()
	t.Setenv("GOPI_HOME", sandbox)
	// The cgroup escape is turned OFF rather than exercised: this test measures
	// the CWD decision, and the escape is the subject of its own epic. It also
	// keeps the spawn unprivileged (Rule 25).
	t.Setenv(cgroupscope.EnvOff, "1")
	// The operator override and the worktree-root override are pinned ABSENT,
	// so the assertion is about the DEFAULT decision (an inherited
	// GOPI_DAEMON_CWD from the developer's shell would silently reroute it).
	t.Setenv(daemoncwd.EnvName, "")
	t.Setenv(worktreeroots.WorktreeRootEnvName, "")
	// The worker loop's idle budget: a big one, so the daemon is still resident
	// when the cwd is measured (see the constants' note).
	t.Setenv(workerIdleIntervalEnv, daemonCwdTestIdleSec)
	t.Setenv(workerIdleExitsEnv, daemonCwdTestIdleExits)
	// The WORKER face is operator-only (a launcher inside an agent's shell is
	// refused, by design). This test is the deliberate one-off override, the
	// same one the e2e suite uses to start a real worker daemon: it needs a REAL
	// detached process because the subject is the mechanism's measured cwd.
	t.Setenv("GOPI_ALLOW_AGENT_SPAWN", "1")

	// A worker daemon with a long idle budget, so it is still the resident
	// process when the measurement is taken (the merger's --every spelling went
	// with its daemon face, so the worker loop's idle knobs drive residency here).
	launch := daemonCwdLaunchFrom(t, wt, []string{
		bin, CliVerbWorker, daemonFlagLong,
	})
	if launch.err != nil {
		t.Fatalf("spawning the daemon from inside the worktree failed: %v\nstderr: %s", launch.err, launch.stderr)
	}
	lines := strings.TrimSpace(launch.stderr)
	if !strings.Contains(lines, "daemon cwd") {
		t.Fatalf("the start must REPORT the daemon's measured cwd (Rule 15); stdout=%q stderr=%q", launch.stdout, lines)
	}
	pid := daemonCwdTestPid(t, launch.stdout)
	if pid <= 0 {
		t.Fatalf("cannot read the daemon pid from %q", launch.stdout)
	}
	// The pid is killed EXPLICITLY (never by pattern — the 04:12 incident's
	// lesson, PROJECT.md Rule 24): a resident --every daemon outlives this test,
	// and it is the one holding a directory it no longer stands in.
	t.Cleanup(func() { daemonCwdTestStop(pid) })

	// The measurement is read while the daemon is ALIVE: a pid whose process has
	// exited has no /proc entry left to read, and a stale measurement would make
	// this assertion meaningless.
	measured, ok := daemonCwdTestWaitCwd(t, pid)
	if !ok {
		t.Skipf("daemon pid %d exited before its /proc cwd could be read; the measurement is unavailable for this run", pid)
	}
	// IDENTITY: the measured process must be the detached daemon this test
	// started. Ancestry is deliberately NOT the check — the whole point of a
	// daemon is that its launcher EXITS, which reparents the child (measured
	// here: ppid becomes the session/systemd parent), so a descendant test
	// would fail on a correctly detached daemon. Identity is instead read from
	// the daemon's own argv[0] (daemonArgv0Prefix + component, which the spawn
	// seam sets and nothing else does).
	argv0, ok := daemonCwdTestCmdline(pid)
	if !ok {
		t.Fatalf("cannot read /proc/%d/cmdline — the measured process's identity is unverifiable", pid)
	}
	if !strings.Contains(argv0, daemonCwdAgentArgv0) {
		t.Fatalf("pid %d cmdline[0] %q is not the detached daemon this test started (argv[0] %q expected)", pid, argv0, daemonCwdAgentArgv0)
	}

	if daemoncwd.WithinWorktree(measured, wt) {
		t.Fatalf("the daemon's MEASURED cwd %s is inside the task worktree %s — guard ③ would refuse that worktree's recycle for the daemon's whole lifetime (this is the epic's defect)", measured, wt)
	}
	root, err := worktreeroots.DefaultRoot()
	if err != nil {
		t.Fatalf("resolve the default worktree root: %v", err)
	}
	if daemoncwd.WithinWorktree(measured, root) {
		t.Errorf("the daemon's cwd %s is inside the operator's worktree root %s — a later task worktree could own it and the daemon would veto that task's recycle", measured, root)
	}
	// Rule 22's MEASURED criterion, not a spelling: the cwd must be proven OFF
	// the `/` partition — the SAME predicate production uses
	// (scratch.SharesRootFS is the reverse of the resolver's measurement, and
	// it reports an unanswerable device as an error, never as "safe").
	shared, err := scratch.SharesRootFS(measured)
	if err != nil {
		t.Fatalf("measure the filesystem of the daemon cwd %s: %v", measured, err)
	}
	if shared {
		t.Errorf("the daemon's cwd %s shares the / filesystem — Rule 22 requires a MEASURED off-/ directory, not merely a different spelling from /tmp", measured)
	}
	t.Logf("measured daemon cwd: pid %d → %s (worktree %s escaped, shares /: %v)", pid, measured, wt, shared)
}

// daemonCwdLaunch is one spawned daemon's observable results.
type daemonCwdLaunch struct {
	stdout string
	stderr string
	err    error
}

// daemonCwdLaunchFrom spawns argv with the CHILD's cwd set to dir — the exact
// producer shape under test: the launcher stands in the worktree.
func daemonCwdLaunchFrom(t *testing.T, dir string, argv []string) daemonCwdLaunch {
	t.Helper()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return daemonCwdLaunch{stdout: out.String(), stderr: errBuf.String(), err: err}
}

// daemonCwdTestPid extracts the pid from the daemon's start note — the pid the
// operator is told to inspect, so the test inspects the same one.
func daemonCwdTestPid(t *testing.T, stdout string) int {
	t.Helper()
	const marker = "pid "
	i := strings.Index(stdout, marker)
	if i < 0 {
		return 0
	}
	rest := stdout[i+len(marker):]
	end := strings.IndexAny(rest, ", \n")
	if end < 0 {
		end = len(rest)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0
	}
	return pid
}

// daemonCwdTestWaitCwd polls /proc/<pid>/cwd until it resolves, bounded by
// daemonCwdProbeWait. A daemon that exits before the read lands is REPORTED by
// the caller (Skip), never mistaken for a passing assertion.
func daemonCwdTestWaitCwd(t *testing.T, pid int) (string, bool) {
	t.Helper()
	deadline := time.Now().Add(daemonCwdProbeWait)
	for {
		if cwd, ok := daemoncwd.ProcessCwd(pid); ok {
			return cwd, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(daemonCwdProbePeriod) //stylegate:allow-sleep bounded poll for a just-spawned daemon's cwd to become readable (no event fires when the child lands)
	}
}

// daemonCwdTestCmdline returns pid's argv[0] as the kernel reports it — the
// daemon's own identity marker (its argv[0] is the stable name the spawn seam
// assigns, so it distinguishes gopi's daemon from every other process without
// relying on the pid's ancestry, which a deliberate detach severs).
func daemonCwdTestCmdline(pid int) (string, bool) {
	b, err := os.ReadFile(filepath.Join(daemonCwdProcRoot, strconv.Itoa(pid), daemonCwdCmdlineName))
	if err != nil || len(b) == 0 {
		return "", false
	}
	first := string(b)
	if i := strings.IndexByte(first, 0); i >= 0 {
		first = first[:i]
	}
	return first, true
}

// daemonCwdTestWorktree creates a REAL linked worktree and returns its path —
// the directory whose live holder guard ③ protects.
func daemonCwdTestWorktree(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	wt := filepath.Join(base, "wt-feat-escape")
	daemonCwdGit(t, base, "init", "-q", repo)
	daemonCwdGit(t, repo, "config", "user.email", "test@example.invalid")
	daemonCwdGit(t, repo, "config", "user.name", "gopi test")
	if err := os.WriteFile(filepath.Join(repo, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	daemonCwdGit(t, repo, "add", "seed.txt")
	daemonCwdGit(t, repo, "commit", "-qm", "seed")
	daemonCwdGit(t, repo, "worktree", "add", "-q", "-b", "feat/escape", wt)
	// The property the decision keys on: a LINKED worktree's .git is a file.
	if st, err := os.Lstat(filepath.Join(wt, ".git")); err != nil || st.IsDir() {
		t.Fatalf("fixture worktree %s is not a LINKED worktree (.git file expected): %v", wt, err)
	}
	return daemoncwd.ResolvePrefix(wt)
}

// daemonCwdTestStop stops the daemon the test started, by EXPLICIT pid — never
// by pattern (PROJECT.md Rule 24: a `pgrep -f` probe self-matches the shell that
// issues it, which is how an operator's live daemons were once killed). The
// wait is bounded and the teardown is never an assertion.
//
// The delivery goes through the IDENTITY-checked path, not a bare pid number:
// the handle this pid names is captured by the test and the signal is sent
// later, so a pid that has been recycled in between would be signalled in place
// of this daemon. `os.FindProcess` cannot tell the two apart on Unix (it always
// succeeds), so the incarnation (pid + starttime) is read first and the signal
// is refused unless the process behind the number is still the recorded one —
// the defect that sent SIGINT into `systemd --user` and stopped this host's
// GNOME session five times on 2026-10-17 (cmd/agent/ground_kill_identity_test.go).
func daemonCwdTestStop(pid int) {
	id, err := testutil.ReadProcIdentity(pid)
	if err != nil || id.Pid <= 0 {
		return
	}
	_ = testutil.KillIfIdentical(id, syscall.SIGTERM) //stylegate:ignore teardown of the test's own child daemon, and only while it is the recorded incarnation
	deadline := time.Now().Add(daemonCwdTestStopWait)
	for time.Now().Before(deadline) {
		if _, ok := daemoncwd.ProcessCwd(pid); !ok {
			return
		}
		time.Sleep(daemonCwdTestStopPeriod) //stylegate:allow-sleep bounded wait for the test's own daemon to leave /proc (teardown, never an assertion)
	}
	_ = testutil.KillIfIdentical(id, syscall.SIGKILL) //stylegate:ignore last-resort teardown against the SAME recorded incarnation
}

// daemonCwdTestBinary returns the gopi binary this spawn test drives: the
// package's ONE process-level build slot (gopibin_test.go), which lives in the
// suite scratch root — off `/` (Rule 22), and outliving each test.
//
// It used to keep a slot of its own and rebuild the same package, on the
// stated ground that other files drive the shared slot "with their own build
// flags (a coverage-instrumented build included)". That is not so: the plain
// binary and the instrumented one are two distinct slots in gopibin_test.go
// (gopiBinOnce / gopiCoverBinOnce), so taking the plain one cannot reseat the
// other, and every other file in this package that drives a plain build reads
// the same path the setup.buildGopi helper writes.
func daemonCwdTestBinary(t *testing.T) string {
	t.Helper()
	return gopiTestBinPath(t)
}

// daemonCwdGit runs one git command, failing the test on error.
func daemonCwdGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
