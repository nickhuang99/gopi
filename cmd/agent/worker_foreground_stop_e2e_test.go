// worker_foreground_stop_e2e_test.go — the regression guard for bug
// 01a131c8-0500: `gopi worker --stop` asserted "no worker daemon is running …
// a FOREGROUND `gopi worker` holds no lock and is unaffected" (exit 0) while the
// operator's FOREGROUND worker was alive and visible in `ps`.
//
// The two defects the guard pins, one assertion each:
//
//	(a) THE CLAIM (unit half, synthetic procfs): a worker-shaped process that is
//	    NOT detached is found by foregroundWorkerPIDs — and the caller (whose own
//	    argv has the same shape) is not, or the command would signal itself.
//	(b) THE EFFECT (e2e half, real processes): a REAL foreground `gopi worker`
//	    is listed by `--status`, is stopped by `--stop`, and its pid is GONE
//	    afterwards — while no run of this command may print the empty-state
//	    assertion (a state file's absence is not a worker's absence).
//
// Rule 27: nothing here generates load. The worker is one real process doing
// what it always does (poll a drained queue); the assertions are about pids.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/procname"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/tools"
)

const (
	// wfsShapedWait bounds the wait for a freshly spawned `gopi worker` to reach
	// the shape the enumeration reads (comm = gopi, argv[1] = worker). Before
	// exec the pid is still `go`-spawned machinery, so the scan cannot see it —
	// a race the assertion must not turn into a flake.
	wfsShapedWait = 30 * time.Second
	// wfsPoll is that wait's cadence (Rule 1).
	wfsPoll = 50 * time.Millisecond
	// wfsForeignSession is a session id that is NOT the pid, i.e. what a
	// process that never called setsid reports: the FOREGROUND signature.
	wfsForeignSession = 1000
	// wfsStopWait is the operator bound the stop is run with — generous, because
	// the worker answers at its next claim boundary and this host's prologue is
	// a measurement of the machine, not of the case.
	wfsStopWait = "60s"
)

// --- (a) the CLAIM: the enumeration itself --------------------------------

// TestWorkerForegroundPIDsFindsTheNonDetachedWorker pins the predicate's two
// exclusions: a DETACHED worker-shaped process is the daemon half's business
// (fallbackWorkerPIDs), and THIS process — whose argv is `gopi worker --stop`
// /`--status`, the very shape a foreground worker has — must never be returned,
// or the stop would signal its own invocation.
func TestWorkerForegroundPIDsFindsTheNonDetachedWorker(t *testing.T) {
	root := t.TempDir()
	self := os.Getpid()
	// REAL live pids: tools.PidAlive is not test-seamable, so a fabricated pid
	// would be rejected on liveness and the fixture would assert nothing.
	pids := liveWorkerChildren(t, 2)
	foregroundPID, detachedPID := pids[0], pids[1]

	// A bare `gopi worker`: same shape as a daemon, but it did NOT setsid.
	procFixture(t, root, foregroundPID, binaryNameGopi,
		[]string{"gopi", procname.VerbWorker}, wfsForeignSession)
	// A detached worker: session == pid (the daemon signature).
	procFixture(t, root, detachedPID, binaryNameGopi,
		[]string{"gopi", procname.VerbWorker}, detachedPID)
	// THIS process, wearing the control face's argv — the self-match the
	// predicate must exclude.
	procFixture(t, root, self, binaryNameGopi,
		[]string{"gopi", procname.VerbWorker, workerControlStop}, self)

	t.Setenv(procname.EnvProcRoot, root)
	// No environment file for any fixture pid: attribution degrades to "count
	// it as this profile's", which is the arm under test.
	t.Setenv(config.GopiDirEnv, filepath.Join(t.TempDir(), "home"))

	got := foregroundWorkerPIDs(workerStatusOwnProfile())
	if !containsPid(got, foregroundPID) {
		t.Fatalf("the non-detached worker pid %d was not found; got %v", foregroundPID, got)
	}
	if containsPid(got, detachedPID) {
		t.Fatalf("a DETACHED worker (pid %d) was listed as foreground — that pid belongs to the daemon half", detachedPID)
	}
	if containsPid(got, self) {
		t.Fatalf("the CALLER (pid %d) was listed as a foreground worker — the stop would signal its own invocation", self)
	}
}

// TestWorkerForegroundShapeInvertsOnlyTheDetachment is the property behind the
// case above, stated once so a future edit to either predicate must keep it:
// the two shapes disagree ONLY on detachment, never on comm or argv.
func TestWorkerForegroundShapeInvertsOnlyTheDetachment(t *testing.T) {
	root := t.TempDir()
	pids := liveWorkerChildren(t, 2)
	detachedPID, foregroundPID := pids[0], pids[1]
	procFixture(t, root, detachedPID, binaryNameGopi, []string{"gopi", procname.VerbWorker}, detachedPID)
	t.Setenv(procname.EnvProcRoot, root)
	if !workerProcessShape(detachedPID) {
		t.Fatal("the detached fixture must satisfy the daemon-shape predicate")
	}
	if workerForegroundShape(detachedPID) {
		t.Fatal("the detached fixture must NOT satisfy the foreground-shape predicate")
	}
	procFixture(t, root, foregroundPID, binaryNameGopi, []string{"gopi", procname.VerbWorker}, wfsForeignSession)
	if workerProcessShape(foregroundPID) {
		t.Fatal("the foreground fixture must NOT satisfy the daemon-shape predicate")
	}
	if !workerForegroundShape(foregroundPID) {
		t.Fatal("the foreground fixture must satisfy the foreground-shape predicate")
	}
}

// TestWorkerForegroundShapeAcceptsTheTargetsOwnBinaryName pins the FIRST finding
// of this run's own real-run evidence (2026-10-13): the fixed binary was built as
// /tmp/gopi-fix, the live worker's comm was `gopi-fix`, and a single-name test
// (comm == "gopi") reported "no FOREGROUND worker in the process table" while the
// pid was alive — the same false absence this bug is about, one level down. The
// identity of a worker is self-consistent (comm and argv[0] come from one exec),
// so the predicate must accept a process whose comm names the binary ITS OWN argv
// names — and still refuse a name that is neither.
func TestWorkerForegroundShapeAcceptsTheTargetsOwnBinaryName(t *testing.T) {
	root := t.TempDir()
	pids := liveWorkerChildren(t, 2)
	renamed, foreign := pids[0], pids[1]
	procFixtureExe(t, root, renamed, "gopi-probe", []string{"/tmp/gopi-probe", procname.VerbWorker},
		wfsForeignSession, "/tmp/gopi-probe")
	// The image is what decides: an unrelated program that happens to take a
	// `worker` subcommand is NOT ours, however self-consistent its names are.
	procFixtureExe(t, root, foreign, "nginx", []string{"/usr/sbin/nginx", procname.VerbWorker},
		wfsForeignSession, "/usr/sbin/nginx")
	t.Setenv(procname.EnvProcRoot, root)

	if !workerForegroundShape(renamed) {
		t.Fatal("a worker of a DIFFERENTLY-NAMED build of this program must be seen: its IMAGE is the gopi program")
	}
	if workerForegroundShape(foreign) {
		t.Fatal("a process whose IMAGE is not the gopi program must NOT be taken for a worker, whatever its argv[1] says")
	}
}

// procFixtureExe is procFixture plus the process's IMAGE (/proc/<pid>/exe), which
// workerGopiImage reads before falling back to argv[0].
func procFixtureExe(t *testing.T, root string, pid int, comm string, argv []string, session int, exe string) {
	t.Helper()
	procFixture(t, root, pid, comm, argv, session)
	// The fixture the plain procFixture already planted is REPLACED: this
	// variant exists to name the image explicitly (a differently-named build, a
	// foreign program), which is the whole point of the case it serves.
	link := filepath.Join(root, strconv.Itoa(pid), procExeName)
	if err := os.Remove(link); err != nil {
		t.Fatalf("clear the default exe link: %v", err)
	}
	if err := os.Symlink(exe, link); err != nil {
		t.Fatalf("plant exe link: %v", err)
	}
}

// --- (b) the EFFECT: the real binary, a real foreground worker -------------

// wfsEnv is the environment a probe foreground worker runs in: a sandbox
// profile, no peers, no sync, and the loop's idle handling tuned so the worker
// STAYS (idle-exits 0 = the resident wait, which is stop-aware) and polls
// quickly.
func wfsEnv(t *testing.T, home, settings string) []string {
	t.Helper()
	env := wdEnv(t, home, settings,
		workerIdleIntervalEnv+"=1",
		workerIdleExitsEnv+"=0")
	// The guard override must ride AFTER wdEnv: stripAgentSignature (which wdEnv
	// runs over the whole vector) removes it by name, so an extra passed in
	// would be silently dropped.
	//
	// Its reason: this is a TEST spawning the worker loop (the guard's ruling is
	// about an AGENT doing it). The override is the documented operator escape,
	// and it is scoped to this child only.
	return append(env, config.AgentSpawnGuardOverrideEnv+"=1")
}

// wfsStartForeground starts a bare `gopi worker` — the operator's most common
// shape — as our own child and waits until the enumeration can SEE it.
func wfsStartForeground(t *testing.T, bin, dir string, env []string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(bin, CliVerbWorker)
	// The repo-context pin, for the CHILD: the worker's own-worktree-release pass
	// resolves a repo from its cwd, so the child is handed the sandbox directory
	// instead of inheriting this process's cwd (the live checkout). The caller's
	// SandboxRepoContext pins the same directory for itself; this is the child's
	// half, and TestSpawnersPinTheRepoContext fails the build without it.
	cmd.Dir = dir
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatalf("start foreground worker: %v", err)
	}
	t.Cleanup(func() {
		// TEARDOWN BY IDENTITY, not by number (Rule 42). This child is OURS, so
		// ownership is established — but `cmd.Process` names the NUMBER, and by
		// teardown time the fixture may already be gone, in which case a signal
		// reaches whoever owns the number now (measured 2026-10-27/28: five
		// SIGINTs delivered to live `gopi` processes, sender `agent.test`).
		if cmd.Process != nil {
			if id, err := testutil.AwaitProcIdentity(cmd.Process.Pid); err == nil {
				_ = testutil.KillIfIdentical(id, syscall.SIGINT) //stylegate:ignore best-effort teardown of the probe child, delivered only while it is the recorded incarnation
			}
			_ = cmd.Process.Kill()    //stylegate:ignore best-effort teardown of the probe child if it ignored the signal
			_, _ = cmd.Process.Wait() //stylegate:ignore reap of the probe child on the teardown path
		}
	})
	pid := cmd.Process.Pid
	deadline := time.Now().Add(wfsShapedWait)
	for time.Now().Before(deadline) {
		if workerForegroundShape(pid) {
			return cmd
		}
		time.Sleep(wfsPoll) //stylegate:allow-sleep bounded wait for the child to exec a gopi-named worker argv
	}
	t.Fatalf("the foreground worker pid %d never reached the `%s %s` shape within %s",
		pid, binaryNameGopi, procname.VerbWorker, wfsShapedWait)
	return cmd
}

// TestWorkerStatusListsAForegroundWorkerAndStopEndsIt is the bug's own repro,
// run against the real binary: with a live foreground worker the stop must NOT
// claim nothing is running, must name the pid, and must really end it.
func TestWorkerStatusListsAForegroundWorkerAndStopEndsIt(t *testing.T) {
	bin := gopiTestBinPath(t)
	// repo-context sandbox: the ledger is pinned by this fixture, but the CHILD
	// would otherwise inherit this process's cwd — the live repository — and its
	// own-worktree-release pass would enumerate (and free) the developer's
	// worktrees. See repo_context_sandbox_test.go. The returned directory is
	// handed to the child as ITS cwd below (both halves of the pin).
	sb := SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wfsEnv(t, home, settings)

	cmd := wfsStartForeground(t, bin, sb.Dir, env)
	pid := cmd.Process.Pid
	pidText := strconv.Itoa(pid)

	// ① THE READ: `--status` must see it. Before the fix it printed
	// "no workers running" while this pid was alive.
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` exited %d, want 0:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}
	if strings.Contains(out, workerStatusNoWorkers) {
		t.Fatalf("`--status` claimed no workers while the FOREGROUND pid %s was alive:\n%s", pidText, out)
	}
	if !strings.Contains(out, "pid "+pidText+": FOREGROUND") {
		t.Fatalf("`--status` must list the foreground worker by pid %s:\n%s", pidText, out)
	}

	// ② THE ACT: `--stop` must reach it — and it must not print the sentence
	// this bug is about.
	code, out = wdRun(t, bin, env, CliVerbWorker, workerControlStop, workerControlWait, wfsStopWait)
	if strings.Contains(out, "no worker daemon is running") ||
		strings.Contains(out, "holds no lock and is unaffected") {
		t.Fatalf("`--stop` asserted no worker was running while pid %s was alive:\n%s", pidText, out)
	}
	if !strings.Contains(out, "FOREGROUND") || !strings.Contains(out, pidText) {
		t.Fatalf("`--stop` must name the FOREGROUND pid %s it acted on:\n%s", pidText, out)
	}
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` must exit 0 once the foreground worker left, got %d:\n%s",
			CliVerbWorker, workerControlStop, code, out)
	}
	// ③ THE EFFECT: the pid is gone — the claim above is checked against the
	// process table, not against the command's own word.
	if !wdWaitPIDGone(t, pid, wdDaemonWait) {
		t.Fatalf("pid %d is still alive after a successful `--stop`:\n%s", pid, out)
	}
	// ④ and the read agrees afterwards (the enumeration no longer lists it).
	_, out = wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if strings.Contains(out, "pid "+pidText+": FOREGROUND") {
		t.Fatalf("after `--stop` the status still lists the foreground pid %s:\n%s", pidText, out)
	}
}

// TestWorkerStopForegroundNoneIsAnObservation pins the OTHER half of Rule 15:
// with no worker at all the command must still say something MEASURED (no live
// daemon state AND no foreground pid), never the old assertion.
func TestWorkerStopForegroundNoneIsAnObservation(t *testing.T) {
	bin := gopiTestBinPath(t)
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wfsEnv(t, home, settings)

	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStop)
	if code != workerControlExitOK {
		t.Fatalf("`%s %s` with nothing running exited %d, want 0:\n%s", CliVerbWorker, workerControlStop, code, out)
	}
	if strings.Contains(out, "no worker daemon is running") {
		t.Fatalf("the empty case must not assert the absence of a WORKER from the absence of a state file:\n%s", out)
	}
	for _, want := range []string{"no DETACHED worker daemon is running", "no FOREGROUND"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the empty case must state %q:\n%s", want, out)
		}
	}
	// And the enumeration it read was empty: no pid of this sandbox's own
	// profile is a worker.
	dir, derr := tools.ControlDirReadOnly(workerStateDirName)
	if derr != nil {
		t.Fatalf("resolve worker state dir: %v", derr)
	}
	if pids := foregroundWorkerPIDs(home + "/.gopi"); len(pids) != 0 {
		t.Fatalf("the sandbox profile has no worker, but the scan returned %v (state dir %s)", pids, dir)
	}
}
