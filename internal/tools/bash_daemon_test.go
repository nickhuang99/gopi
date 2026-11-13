package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/procname"
)

// Named constants for the daemon-spawn regression tests (Rule 1: no magic
// numbers). These reproduce bug011: `nohup ... &` leaves a background
// GRANDCHILD holding the stdout/stderr pipe write-ends after bash exits.
// Killing only bash leaves the pipes open forever, so the read goroutines
// never see EOF and bash() deadlocks. The process-group kill must take the
// grandchild down, close the pipes, and let bash() return promptly.
const (
	testDaemonSleepSeconds   = 300             // how long the background grandchild sleeps
	testDaemonAbsTimeout     = 5 * time.Second // abs timeout where the IDLE monitor fires first
	testDaemonIdleTimeout    = 1 * time.Second // idle timeout (must be < abs to exercise idle path)
	testDaemonAbsFastTimeout = 2 * time.Second // abs timeout where the ABSOLUTE timeout fires first
	testDaemonReturnWindow   = 5 * time.Second // bash() must return within this window
	testDaemonReapPollWindow = 2 * time.Second // how long to poll for the daemon to be reaped
	testDaemonPollInterval   = 50 * time.Millisecond
	testDaemonPidFileWait    = 1 * time.Second  // how long to wait for the daemon pid file
	testDaemonForegroundAbs  = 10 * time.Second // generous timeouts for the foreground sanity test
	testDaemonForegroundIdle = 10 * time.Second

	// noProcessGroup is the "do not filter by ownership" selector for
	// procsCmdlineMatch: a caller taking a whole-system baseline wants EVERY
	// match, including ones no group of ours contains. 0 is a value no real
	// /proc/<pid>/stat pgrp can hold (0 means "the caller's own group"), so it
	// cannot collide with a group bash() created.
	noProcessGroup = 0
)

// testDaemonSleepArg is the sleep duration as it appears in argv[1] — `300` for
// `sleep 300`. DERIVED from testDaemonSleepSeconds so the integer and the
// spelling can never drift (Rules 1a/13); a var rather than a const because
// strconv.Itoa is not a constant expression.
var testDaemonSleepArg = strconv.Itoa(testDaemonSleepSeconds)

// TestBashDaemonSpawn_ReturnsAfterTimeout reproduces bug011: a command that
// launches a long-lived background grandchild (`nohup sleep 300 &`) must NOT
// hang bash(). The grandchild inherits the stdout/stderr pipe write-ends, so
// a kill that only targets bash leaves the read goroutines blocked on EOF
// forever. The process-group kill must take the grandchild down, close the
// pipes, and let bash() return promptly with a timeout error.
func TestBashDaemonSpawn_ReturnsAfterTimeout(t *testing.T) {
	cases := []struct {
		name string
		abs  time.Duration
		idle time.Duration
	}{
		{"idle timeout fires first", testDaemonAbsTimeout, testDaemonIdleTimeout},
		{"absolute timeout fires first", testDaemonAbsFastTimeout, testDaemonAbsTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			_, err := bash(fmt.Sprintf("nohup sleep %d &", testDaemonSleepSeconds), tc.abs, tc.idle)
			elapsed := time.Since(start)

			if err == nil {
				t.Fatal("expected timeout error for daemon-spawning command, got nil")
			}
			if elapsed > testDaemonReturnWindow {
				t.Errorf("bash() took %v; daemon-spawn deadlock (bug011) not fixed", elapsed)
			}
		})
	}
}

// TestBashDaemonSpawn_DoesNotLeakProcesses verifies the background grandchild
// is actually dead after bash() returns: the daemon writes its pid to a file
// before exec'ing sleep, and we poll until kill(pid, 0) reports the process
// is gone. We also scan /proc to make sure no `sleep 300` process survives.
func TestBashDaemonSpawn_DoesNotLeakProcesses(t *testing.T) {
	// Baseline FIRST: what `sleep 300` processes already existed before this
	// test spawned anything. Only pids that appear afterwards can be ours.
	foreign := procsCmdlineMatch("sleep", testDaemonSleepArg, noProcessGroup)

	pidFile := filepath.Join(t.TempDir(), "daemon.pid")
	// The daemon publishes its own pid before exec'ing sleep. That pid is a
	// process INSIDE the group bash() created for this command (Setpgid makes
	// bash's child the leader, bash.go), so its /proc/<pid>/stat pgrp field IS
	// the ownership token the scan below needs.
	cmd := fmt.Sprintf("nohup bash -c 'echo $$ > %s; exec sleep %d' &", pidFile, testDaemonSleepSeconds)

	// bash() must run CONCURRENTLY with the capture below: it kills the whole
	// group when it returns (that is the behaviour under test), so by the time
	// it has returned the daemon — and with it the only readable copy of the
	// group — is gone. Capturing the token from the daemon while bash() is
	// still waiting is what makes "ours" an ownership fact rather than an
	// argv-spelling guess or a windowed pid-slot subtraction.
	bashErr := make(chan error, 1)
	go func() {
		_, err := bash(cmd, testDaemonAbsTimeout, testDaemonIdleTimeout)
		bashErr <- err
	}()

	// The daemon writes its pid before exec'ing sleep; read it, then read the
	// group off the live process. Both happen well inside bash()'s timeout.
	pid := readDaemonPID(t, pidFile)
	daemonGroup := 0
	if !waitFor(func() bool {
		daemonGroup = procGroupID(pid)
		return daemonGroup != 0
	}, testDaemonPidFileWait) {
		t.Fatalf("daemon pid %d never published a process group; bash() never launched it into its own group", pid)
	}

	if err := <-bashErr; err == nil {
		t.Fatal("expected timeout error for daemon-spawning command, got nil")
	}

	// kill(pid, 0) returns ESRCH once the daemon is fully reaped.
	if !waitFor(func() bool { return syscall.Kill(pid, 0) != nil }, testDaemonReapPollWindow) {
		t.Errorf("background daemon pid %d survived the process-group kill", pid)
	}

	// Independent /proc scan: no `sleep 300` process in THIS command's own
	// group survives the process-group kill. Scoped by OWNERSHIP to that one
	// group — created fresh by bash() for this command, holding nothing but
	// this command's processes — so membership in it IS "ours": no sibling
	// fixture's `sleep 300`, no operator's probe and no recycled pid slot can
	// be inside it, while a grandchild that escaped the group kill is inside
	// it. (argv alone cannot decide this: `sleep 300` is this package's own
	// shared fixture spelling, and the pid-slot subtraction only holds while
	// the snapshot-to-assertion window is short and its holders are stable.)
	if !waitFor(func() bool {
		return len(newProcsCmdlineFamily(foreign, "sleep", testDaemonSleepArg, daemonGroup)) == 0
	}, testDaemonReapPollWindow) {
		t.Errorf("a 'sleep %d' process of this command's own group %d survived the process-group kill (pids %v)",
			testDaemonSleepSeconds, daemonGroup, newProcsCmdlineFamily(foreign, "sleep", testDaemonSleepArg, daemonGroup))
	}
}

// TestBashForeground_StillWorks is a regression guard: process-group
// isolation must not break plain foreground commands.
func TestBashForeground_StillWorks(t *testing.T) {
	start := time.Now()
	output, err := bash("echo hello", testDaemonForegroundAbs, testDaemonForegroundIdle)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(output, "exit=0\n") {
		t.Errorf("expected output to start with 'exit=0\\n', got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %q", output[:min(len(output), bashDiagWindowBytes)])
	}
	if elapsed > testDaemonReturnWindow {
		t.Errorf("foreground echo took too long: %v", elapsed)
	}
}

// readDaemonPID waits for the daemon pid file to appear and returns its pid.
func readDaemonPID(t *testing.T, pidFile string) int {
	t.Helper()
	var pid int
	waitFor(func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && pid > 0
	}, testDaemonPidFileWait)
	if pid == 0 {
		t.Fatalf("daemon pid file %s was never written", pidFile)
	}
	return pid
}

// waitFor polls cond until it returns true or the window elapses.
func waitFor(cond func() bool, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(testDaemonPollInterval) //stylegate:allow-sleep poll interval while the daemon publishes its pid file
	}
	return cond()
}

// anyProcessCmdlineHas reports whether any process on the system has an argv
// whose first two fields match (prog, arg) — e.g. ("sleep", "300"), the
// background grandchild spawned by the daemon tests. Scanning /proc avoids
// depending on pgrep being installed. It is the whole-system question (no
// ownership filter), so it asks procsCmdlineMatch for every group.
func anyProcessCmdlineHas(prog, arg string) bool {
	return len(procsCmdlineMatch(prog, arg, noProcessGroup)) > 0
}

// procsCmdlineMatch returns the PIDs whose argv starts with (prog, arg) AND
// whose process group is pgid — the real /proc reader, narrowed by OWNERSHIP.
//
// argv is not an identity here: `sleep 300` is a globally generic spelling that
// this package's own sibling tests use for their long-lived fixtures
// (reclaim_live_run_veto_test.go, live_claim_worktree_veto_test.go,
// zz_redproof_reclaim_live_runner_test.go, task_holder_resume_test.go), and the
// pid SLOT is recycled (measured: ~1000 pids/second on this host), so a
// snapshot-subtraction alone cannot say whether a match "appeared" during the
// caller's window — a recycled number lands in a baseline whose holder is long
// gone (see newProcsCmdlineFamily). The group is what a caller controls: bash()
// spawns every command with Setpgid (bash.go), so the leader's pid IS the group
// this test caused. pgid <= 0 = no group filter.
func procsCmdlineMatch(prog, arg string, pgid int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var pids []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		fields := strings.Fields(strings.ReplaceAll(string(data), "\x00", " "))
		if len(fields) < 2 || fields[0] != prog || fields[1] != arg {
			continue
		}
		if pgid > 0 && procGroupID(pid) != pgid {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// procGroupID reads /proc/<pid>/stat's pgrp field — the process group a match
// belongs to. It delegates the field numbering to internal/procname, the ONE
// parse of that file (Rules 13/14: comm is parenthesised and may contain
// spaces and parentheses, so index arithmetic here would be a second, driftable
// answer). 0 = unreadable (the process exited mid-scan, procfs absent) — an
// unknown group is never claimed as ours.
func procGroupID(pid int) int {
	raw, ok := procname.StatFieldString(
		procname.StatStatPath(procRootName, strconv.Itoa(pid)), procname.StatFieldPgrp)
	if !ok {
		return 0
	}
	pgid, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return pgid
}

// newProcsCmdlineFamily is the PIDs matching (prog, arg) in process group pgid
// that are NOT in the pre-test baseline — the set this test is responsible for.
// Two independent filters compose, each killing a different false positive:
//   - the GROUP kills the foreign owner: a sibling test's `sleep 300` fixture,
//     an operator's probe, a `gopi job` — none of them is in the group bash()
//     created for this test, so none is ever read as this test's leak.
//   - the BASELINE kills the pid RECYCLE: a number that was already listed at
//     snapshot time names a process that predates this test, so the slot it
//     lands in afterwards cannot be this test's orphan. (The subtraction alone
//     was never enough — it is exact only while the window is short and the
//     holder is stable, and this host allocates ~1000 pids/second.)
//
// A genuine orphan of THIS test is in its group and absent from its baseline,
// so it is still reported.
func newProcsCmdlineFamily(baseline []int, prog, arg string, pgid int) []int {
	seen := make(map[int]bool, len(baseline))
	for _, pid := range baseline {
		seen[pid] = true
	}
	var fresh []int
	for _, pid := range procsCmdlineMatch(prog, arg, pgid) {
		if !seen[pid] {
			fresh = append(fresh, pid)
		}
	}
	return fresh
}

// TestProcsCmdlineMatch_SubtractsTheBaseline is the acceptance proof for the
// scoped scan above, driving it through the REAL /proc reader. It asserts the
// SAME TWO things it always did — a pid listed in the baseline is never
// reported as new, and a live probe whose argv matches is reported — with the
// ownership filter the package-wide run needs.
//
// The probe leads its OWN process group (Setpgid), the way bash() launches
// every command, and that group is the only identity claim made here: this test
// does NOT assert that the group is globally unused, because `sleep 300` is
// deliberately shared with this package's sibling fixtures and asserting
// global exclusivity would fail on a correct machine the moment one of them
// runs concurrently — a false red, which is the whole defect this test exists
// to pin.
func TestProcsCmdlineMatch_SubtractsTheBaseline(t *testing.T) {
	probe := exec.Command("sleep", testDaemonSleepArg)
	// Pdeathsig, measured 2026-10-27 (task 01a179d7): with only a t.Cleanup
	// this probe SURVIVED a SIGKILL of the test binary and stood in the
	// checkout, which is what makes a finished ground read IN USE. Setpgid is
	// this fixture's own subject, so it builds its *exec.Cmd itself — and arms
	// the kernel's counter on it.
	probe.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := probe.Start(); err != nil {
		t.Skipf("cannot spawn the probe process: %v", err)
	}
	// The probe's pid is the pid a Setpgid'd leader's group takes, read from
	// the kernel rather than assumed (bash.go relies on the same identity).
	wantGroup := probe.Process.Pid
	t.Cleanup(func() {
		if err := probe.Process.Kill(); err != nil {
			t.Logf("probe process was already gone: %v", err) // best-effort reap, logged not swallowed
		}
		if _, err := probe.Process.Wait(); err != nil {
			t.Logf("probe process wait: %v", err)
		}
	})

	// Poll briefly: the child must be visible in /proc before the baseline.
	// Baseline over OUR group only — this test's responsibility is its own
	// group, and a same-argv process outside it is never its leak.
	var baseline []int
	if !waitFor(func() bool {
		baseline = procsCmdlineMatch("sleep", testDaemonSleepArg, wantGroup)
		return len(baseline) > 0
	}, testDaemonPidFileWait) {
		t.Skip("probe process never appeared in /proc")
	}
	if fresh := newProcsCmdlineFamily(baseline, "sleep", testDaemonSleepArg, wantGroup); len(fresh) != 0 {
		t.Errorf("a baseline-listed pid was reported as new: %v", fresh)
	}
	// The live probe must be reported against an empty baseline — read on the
	// REAL group, so a probe absent from /proc fails here rather than passing.
	if fresh := newProcsCmdlineFamily(nil, "sleep", testDaemonSleepArg, wantGroup); len(fresh) == 0 {
		t.Error("with an empty baseline the live probe must be reported as new")
	}
	// Red-proof anchor, at no extra process: the probe IS a genuine member of
	// this test's own group, so pretending it appeared after the baseline must
	// make the assertion fire on it. Delete that one pid from the baseline and
	// assert the ONLY thing reported is the probe. Were the group filter made
	// so wide that it swallowed this test's own processes, this fails — the
	// narrowing is red for a genuine orphan of THIS test.
	withoutProbe := make([]int, 0, len(baseline))
	for _, pid := range baseline {
		if pid != probe.Process.Pid {
			withoutProbe = append(withoutProbe, pid)
		}
	}
	if len(withoutProbe) == len(baseline) {
		t.Fatalf("probe pid %d was not in the group-scoped baseline %v", probe.Process.Pid, baseline)
	}
	fresh := newProcsCmdlineFamily(withoutProbe, "sleep", testDaemonSleepArg, wantGroup)
	if len(fresh) != 1 || fresh[0] != probe.Process.Pid {
		t.Fatalf("genuine in-group orphan must be reported: got %v, want [%d] (baseline %v, group %d)",
			fresh, probe.Process.Pid, baseline, wantGroup)
	}
}
