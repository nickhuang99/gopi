package main

// worker_idle_drained_stop_e2e_test.go — the REAL-BINARY proof of the
// never-self-exit drained wait's stop-awareness.
//
// THE DEFECT (epic worker-idle-drained-stop): `gopi worker --daemon` started
// with GOPI_WORKER_IDLE_EXITS=0 slept its drained-queue interval with a raw
// time.Sleep. The graceful stop handler had already replaced SIGTERM's default
// disposition, so SIGTERM only set an in-memory flag the sleeper never read:
// the daemon lived the whole interval (an hour at GOPI_WORKER_IDLE_SEC=3600),
// and `worker --stop` — which writes TERM_<pid> and then signals — waited it
// out too. Both documented doors to a stop were effectively closed.
//
// WHY THIS IS AN E2E: the property is about an OS-level process answering an
// OS-level request (a signal, a file, a pid that must actually disappear), and
// the in-process unit cases (worker_idle_drained_stop_test.go) cannot see the
// loop's choice of wait. This case runs the real binary with a ONE-HOUR idle
// interval, so any return of the raw sleep fails it in seconds rather than in
// an hour.
//
// Load discipline (Rule 27): nothing here generates device load. The daemon
// idles on an EMPTY queue and the measurements are process liveness plus an
// elapsed duration.
//
// Sandbox (Rule 6): a private GOPI_HOME with an empty repos table, an empty
// ledger, no sync peers — and the daemon is stopped by its EXPLICIT pid, never
// by a pattern kill (the operator runs a live worker daemon, and merger ticks, on this host).

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/testutil"
)

const (
	// wisIdleSec is the ONE-HOUR idle interval: the defect's own shape, and the
	// reason a raw sleep is unmissable rather than a slow-but-passing test.
	wisIdleSec = "3600"
	// wisIdleExits is the never-self-exit opt-out — the face whose wait was not
	// stop-aware. A worker with self-exit enabled already used the stop-aware
	// wait (waitWorkerIdleOrStop), which is why the default face never showed
	// the bug.
	wisIdleExits = "0"
	// wisStopBudget is the acceptance bound: after the stop is delivered, the
	// daemon must be gone within this window. It is many times smaller than the
	// idle interval, so only a stop-aware wait can satisfy it, and it leaves
	// room for process-table settling.
	wisStopBudget = 5 * time.Second
	// wisReadyBudget bounds how long the daemon may take to reach its drained
	// wait before the case delivers the stop.
	wisReadyBudget = 60 * time.Second
	// wisDrainedLine is the loop's own announcement of the wait under test. The
	// case waits for it so the stop is delivered INTO that wait rather than
	// during start-up (which the signal path also handles, but which would not
	// test this defect).
	wisDrainedLine = "queue drained — waiting 1h0m0s"
	// wisStatusWhy is the sentence a failure prints: the liveness door is the
	// ENUMERATION, and that is what the assertion below measures (see
	// wisAssertGone).
	wisStatusWhy = "a live daemon would still be listed"
)

// wisStartDaemon starts the real binary as a detached daemon with the
// never-self-exit, one-hour-idle configuration, and returns its pid, log path
// and environment.
func wisStartDaemon(t *testing.T) (int, string, string, []string) {
	t.Helper()
	bin := wdBin(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	env := wdEnv(t, home, settings,
		workerIdleIntervalEnv+"="+wisIdleSec,
		workerIdleExitsEnv+"="+wisIdleExits,
	)
	code, out := wdRun(t, bin, env, CliVerbWorker, workerDaemonFlagLong)
	if code != 0 {
		t.Fatalf("`%s %s` must exit 0, got %d:\n%s", CliVerbWorker, workerDaemonFlagLong, code, out)
	}
	pid, logPath := wdParseStart(t, out)
	t.Cleanup(func() { wdReap(t, home) })
	if !wdWaitLogContains(t, logPath, wisDrainedLine, wisReadyBudget) {
		t.Fatalf("the daemon never reached its drained wait (want %q in %s):\n%s",
			wisDrainedLine, logPath, wdLogTail(logPath))
	}
	return pid, logPath, home, env
}

// wisAssertGone asserts the daemon is gone within budget AND that the
// read-only status face agrees — two independent doors, because "kill -0 says
// dead" and "the operator's own query says nothing is running" are different
// facts (a residue state file could make the second disagree with the first).
func wisAssertGone(t *testing.T, pid int, home string, env []string, bin string, start time.Time) {
	t.Helper()
	deadline := time.Now().Add(wisStopBudget)
	for time.Now().Before(deadline) {
		if !wdProcAlive(pid) {
			break
		}
		time.Sleep(wdPollInterval) //stylegate:allow-sleep bounded wait for the probed daemon to exit
	}
	if wdProcAlive(pid) {
		t.Fatalf("worker daemon pid %d is STILL ALIVE %s after the stop — the drained wait does not wake on a "+
			"stop request, so a GOPI_WORKER_IDLE_EXITS=0 daemon serves out its whole %ss idle interval before "+
			"it will die", pid, time.Since(start), wisIdleSec)
	}
	// The second door is the ENUMERATION, read in its MACHINE form and scoped to
	// THIS profile. It used to be a substring over the human page
	// (`containsLine(out, workerStatusNoWorkers)`), which measured the whole
	// host: another run's leaked worker — or a worker-shaped process whose own
	// environment names no profile, which the scan deliberately COUNTS rather
	// than drops (fallbackWorkerPIDs' stated degradation) — renders a worker line
	// and the empty page line never appears, while this daemon really is gone.
	// Measured 2026-10-24 on master 76e6faf: all three cases in this file were
	// red on a host with two such processes, and green when it had none.
	// TestWorkerStatusAssertionMeasuresTheProfileNotThePage pins the trap.
	doc := workerStatusMachineDoc(t, bin, env, CliVerbWorker, workerControlStatus)
	workerStatusAssertOwnProfileEnumeration(t, doc, home)
	workerStatusAssertNoWorkerPID(t, doc, pid, wisStatusWhy)
	// The human page must agree about THIS pid: no line of the anchored worker
	// shape names it. (A pid can appear in a residue row or in another process's
	// command line; neither says this profile is running it.)
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStatus)
	if code != 0 {
		t.Errorf("`%s %s` exited %d after the daemon died:\n%s", CliVerbWorker, workerControlStatus, code, out)
	}
	if line := workerStatusWorkerLineFor(out, pid); line != "" {
		t.Errorf("`%s %s` still renders a worker line for the daemon pid %d after the stop — %s:\n%s",
			CliVerbWorker, workerControlStatus, pid, wisStatusWhy, line)
	}
}

// containsLine reports whether any line of out equals want (a substring test
// would match a daemon's state line that merely quotes the phrase).
func containsLine(out, want string) bool {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	// The status face prefixes its banner with the program name, so match the
	// line's SUFFIX too rather than weakening it to a bare substring.
	for _, line := range lines {
		if len(line) > len(want) && line[len(line)-len(want):] == want {
			return true
		}
	}
	return false
}

// TestWorkerDaemon_IdleDrainedStopLandsOnSIGTERM is the acceptance's first half:
// the SIGNAL door. With GOPI_WORKER_IDLE_SEC=3600 and
// GOPI_WORKER_IDLE_EXITS=0, a SIGTERM must end the daemon in seconds.
func TestWorkerDaemon_IdleDrainedStopLandsOnSIGTERM(t *testing.T) {
	pid, _, home, env := wisStartDaemon(t)
	bin := wdBin(t)

	start := time.Now()
	// `pid` was parsed out of the daemon's success line, so it is a number from
	// a log rather than a handle: read its incarnation and signal only that one
	// (Rule 42 — a recycled pid must never be stopped).
	id, idErr := testutil.ReadProcIdentity(pid)
	if idErr != nil {
		t.Fatalf("read the daemon's identity (pid %d): %v", pid, idErr)
	}
	if err := testutil.KillIfIdentical(id, syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM pid %d: %v", pid, err)
	}
	wisAssertGone(t, pid, home, env, bin, start)
}

// TestWorkerDaemon_IdleDrainedStopLandsOnStopCommand is the acceptance's second
// half: the DURABLE FILE door — `worker --stop` writes TERM_<pid> and signals.
// The file is what must wake the wait even when the signal's meaning is unknown
// (a foreign binary, a caller that can only write a file), and this case proves
// the two together end the daemon in seconds.
func TestWorkerDaemon_IdleDrainedStopLandsOnStopCommand(t *testing.T) {
	pid, _, home, env := wisStartDaemon(t)
	bin := wdBin(t)

	start := time.Now()
	code, out := wdRun(t, bin, env, CliVerbWorker, workerControlStop,
		workerControlWait, wisStopCommandWait)
	if code != 0 {
		t.Fatalf("`%s %s %s %s` exited %d — the stop did not reach its end state:\n%s",
			CliVerbWorker, workerControlStop, workerControlWait, wisStopCommandWait, code, out)
	}
	if elapsed := time.Since(start); elapsed > wisStopBudget+wisStopCommandSlack {
		t.Errorf("`%s %s` took %s (want the daemon gone within %s of the request) — the stop is still waiting "+
			"out the idle interval", CliVerbWorker, workerControlStop, elapsed, wisStopBudget)
	}
	wisAssertGone(t, pid, home, env, bin, start)
}

// TestWorkerDaemon_IdleDrainedStopLeavesNoResidue: a stop that lands must also
// leave the profile CLEAN — the daemon removes its own durable request on the
// way out (cleanupRequests). A request file left behind addresses a recycled
// pid later, which is how an unrelated daemon gets a stop it never asked for.
func TestWorkerDaemon_IdleDrainedStopLeavesNoResidue(t *testing.T) {
	pid, _, home, env := wisStartDaemon(t)
	bin := wdBin(t)

	start := time.Now()
	// `pid` was parsed out of the daemon's success line, so it is a number from
	// a log rather than a handle: read its incarnation and signal only that one
	// (Rule 42 — a recycled pid must never be stopped).
	id, idErr := testutil.ReadProcIdentity(pid)
	if idErr != nil {
		t.Fatalf("read the daemon's identity (pid %d): %v", pid, idErr)
	}
	if err := testutil.KillIfIdentical(id, syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM pid %d: %v", pid, err)
	}
	wisAssertGone(t, pid, home, env, bin, start)

	req := filepath.Join(home, ".gopi", workerStateDirName, workerStopRequestPrefix+itoa(pid))
	if _, err := os.Stat(req); err == nil {
		t.Errorf("the daemon left its durable request %s behind — a request must not outlive the pid it addresses", req)
	}
}

// wisStopCommandWait is the `--wait` bound the case declares: comfortably more
// than the daemon needs (so a slow host does not fail the case) and far less
// than the idle interval (so a stop that waits the interval out cannot pass).
const wisStopCommandWait = "20s"

// wisStopCommandSlack is the tolerance added to wisStopBudget for the CLI's own
// start-up, spawn and reporting around the wait.
const wisStopCommandSlack = 5 * time.Second
