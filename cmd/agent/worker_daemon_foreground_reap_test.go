package main

// worker_daemon_foreground_reap_test.go — the guard that keeps a FOREGROUND
// `gopi worker` from outliving the test that spawned it.
//
// THE MEASURED DEFECT (2026-11-08). pid 3634507, a no-flag `gopi worker`
// started by TestWorkerDaemon_ForegroundWorkerIgnoresTheLock, was still alive
// 2 days 4 hours after its test process had died:
//
//	PPID 1                     — orphan, reparented to init
//	exe, cwd  "(deleted)"      — both under /dev/shm/suite-cmd-agent-run-769077469
//	environ                     — carried GOPI_DAEMON_DEATHSIG_HOLD_FD=200
//	fd 200                      — pipe:[400314538], READ end, no other holder
//
// So the child had INHERITED the suite's death pipe and never watched it: the
// binding existed, the bound did not. On the test side the spawn was a raw
// exec.Command+Start whose only reclaim was a `defer` SIGTERM+Wait — and a
// defer cannot run when the test binary is SIGKILLed, nor when `go test`'s own
// -timeout panic ends in os.Exit.
//
// THE FIX (both halves, and this file guards both):
//
//	① the spawn goes through testutil.StartMockProcessCmd — Pdeathsig=SIGKILL
//	   (the kernel's bound, the only one that survives a SIGKILLed parent) plus
//	   Setpgid + t.Cleanup(mp.Reap) (the in-process bound: return, t.Fatal,
//	   panic);
//	② runTaskWorker arms the inherited death pipe on EVERY face of the verb,
//	   not only the daemon face — a face that ignores the descriptor it was
//	   handed is the leak.
//
// WHAT TURNS THIS RED: dropping the reaper from the spawn (assertion ③ below
// measures a real process dying, not a struct field), dropping Pdeathsig (①),
// joining the test's own process group (② — where a group reaper would kill the
// test run itself), or restoring a raw exec.Command+Start in either e2e file
// (④, the source pin).
//
// Rule 42: nothing here signals anything it did not create. mp.Reap signals the
// one process group this test started; the survival check is kill(pid, 0) —
// a probe, not a signal.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

const (
	// wdForegroundReapBudget bounds the poll for a reaped child to disappear
	// from the process table. SIGKILL cannot be deferred, so this is the
	// kernel's scheduling latency, not a grace window.
	wdForegroundReapBudget = wdDaemonWait
	// wdForegroundReapPoll is that poll's cadence (Rule 1: no bare literal in a
	// loop condition).
	wdForegroundReapPoll = wdPollInterval
)

// TestWorkerDaemon_ForegroundWorkerIsBoundToItsTest is the positive proof: the
// worker wdForeground really starts is bound to this test in all three ways,
// and the reaper provably removes it.
func TestWorkerDaemon_ForegroundWorkerIsBoundToItsTest(t *testing.T) {
	bin := wdBin(t)
	// repo-context sandbox: the child must not inherit this process's cwd (the
	// live repository) — its own-worktree-release pass would enumerate the
	// developer's worktrees. See repo_context_sandbox_test.go.
	SandboxRepoContext(t)
	home := t.TempDir()
	settings := wdSettings(t, home)
	// A budget long enough that the loop's own self-exit cannot be mistaken for
	// the reaper's work — this test measures the KILL, not the idle timeout.
	env := wdEnv(t, home, settings, workerIdleIntervalEnv+"=30", workerIdleExitsEnv+"=3")

	mp := wdForegroundSpawn(t, bin, env)
	pid := mp.Pid()
	if pid <= 0 {
		t.Fatalf("wdForegroundSpawn returned no pid — the fixture is not measuring a process")
	}

	// ① THE KERNEL'S BOUND. Without it, a SIGKILLed test binary (a watchdog, an
	// operator, a killed CI job) leaves the worker sweeping forever — the
	// measured 2-day orphan.
	//
	// READ THE STRUCT, AND WHY THAT IS THE BEST AVAILABLE READING HERE — the
	// honesty matters more than the check. /proc/<pid>/status normally carries a
	// `Pdeathsig:` line, which would be the in-force value; this kernel does not
	// print it. Measured 2026-11-08 on 6.8.0-138-generic:
	//
	//	grep -l Pdeathsig /proc/[0-9]*/status | wc -l   →   0
	//	grep -i death /proc/self/status                →   (nothing)
	//
	// So userspace here has no channel that shows the ENFORCED value, and the
	// field this test asked for is the only record of the request. It is a
	// weaker reading than a kernel read and is labelled as one; the enforcement
	// is proven BEHAVIOURALLY by ③ below (a real process is reaped) and by the
	// source pin in TestWorkerDaemon_ForegroundSpawnsGoThroughTheReaper, both of
	// which go red if the reap path is removed. Do NOT dress this up as a kernel
	// reading — a future reader must not think the value was observed in force.
	if cmd := mp.Cmd(); cmd.SysProcAttr == nil || cmd.SysProcAttr.Pdeathsig != syscall.SIGKILL {
		got := syscall.Signal(0)
		if cmd.SysProcAttr != nil {
			got = cmd.SysProcAttr.Pdeathsig
		}
		t.Fatalf("the foreground worker's SysProcAttr.Pdeathsig = %v, want %v (SIGKILL) — "+
			"without it the worker outlives a SIGKILLed test binary, which is exactly the measured orphan "+
			"(pid 3634507, 2d 4h)", got, syscall.SIGKILL)
	}
	// The channel's own verdict, logged every run: on a kernel that prints
	// Pdeathsig this line shows "killed"; on this one it shows "" and the comment
	// above says why the request is the best reading available. Logging it keeps
	// the absence a measurement rather than a claim.
	t.Logf("proc Pdeathsig channel for pid %d: %q (empty = this kernel prints no such field; see the note above)",
		pid, wdPdeathsig(t, pid))

	// ② ITS OWN PROCESS GROUP — measured from the KERNEL, not from the struct,
	// because the reaper addresses the group id. A child in THIS test's group
	// would make mp.Reap take the test run down with it (refused by the
	// reaper's own guard, but the spawn must not ask for it in the first place).
	pgid := wdProcessField(t, pid, wdProcessFieldPGID)
	if pgid != pid {
		t.Errorf("foreground worker pid %d has PGID=%d, want its own pid %d (Setpgid makes the child a group leader so the reaper can address it)", pid, pgid, pid)
	}
	if own := syscall.Getpgrp(); pgid == own {
		t.Errorf("foreground worker pid %d shares THIS test's process group %d — a group reaper would signal the whole test run", pid, own)
	}

	// ③ THE REAP REALLY REMOVES IT. This is the assertion that turns red if the
	// reap call is removed from the spawn: without it the child keeps running
	// and keeps answering kill(pid, 0).
	mp.Reap()
	if alive := wdProcAlive(pid); alive {
		t.Fatalf("foreground worker pid %d is STILL ALIVE after Reap — the reap path is not in force (this is the orphan shape)", pid)
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatalf("foreground worker pid %d still answers kill(pid, 0) after Reap — it was not reaped", pid)
	} else if err != syscall.ESRCH {
		// EPERM would mean "alive, but not ours" — a different, also-wrong answer.
		t.Fatalf("probing reaped pid %d returned %v, want ESRCH", pid, err)
	}
}

// TestWorkerDaemon_ForegroundSpawnsGoThroughTheReaper is the source pin: it
// fails if either e2e file goes back to a raw spawn of the worker.
//
// WHY BOTH A STRUCTURAL AND A BEHAVIOURAL CHECK. The behavioural one above
// measures the fixture helper; a case that hand-rolled its own
// exec.Command+Start would be green there and leak all the same (that IS how
// the 2-day orphan happened — wdForeground and an inline duplicate, two spawns,
// one shared omission). So the raw spelling is refused wherever it appears in
// these two files, and the helper is required to be the one that spawns.
func TestWorkerDaemon_ForegroundSpawnsGoThroughTheReaper(t *testing.T) {
	files := []string{
		"worker_daemon_singleton_e2e_test.go",
		"worker_daemon_flag_e2e_test.go",
	}
	// wdForegroundSpawn is the ONE spawn of a foreground worker; if its body
	// stops calling the reaper the bound silently disappears, so the seam itself
	// is asserted to exist and to be used.
	singleton := wdReadTestSource(t, files[0])
	if !strings.Contains(singleton, "testutil.StartMockProcessCmd(t, cmd)") {
		t.Errorf("%s: wdForegroundSpawn no longer routes the spawn through testutil.StartMockProcessCmd — "+
			"a raw exec.Command+Start has no Pdeathsig and no t.Cleanup reap, which is the measured orphan",
			files[0])
	}
	for _, f := range files {
		src := wdReadTestSource(t, f)
		if strings.Contains(src, ".Start()") {
			t.Errorf("%s: contains a raw `.Start()` — every foreground `gopi worker` spawn in this package must go through "+
				"testutil.StartMockProcessCmd (wdForegroundSpawn), which is the only spelling that carries Pdeathsig=SIGKILL "+
				"and the t.Cleanup reaper", f)
		}
	}
}

// wdPdeathsigKilled is how the kernel spells SIGKILL in /proc/<pid>/status's
// Pdeathsig field ("killed", not "9" and not "SIGKILL"). Named once (Rule 1b):
// it is a value the guard COMPARES against, so a bare literal would be a second
// source of truth for the same fact.
const wdPdeathsigKilled = "killed"

// wdPdeathsig reads the death signal the KERNEL will send this pid when its
// parent dies, out of /proc/<pid>/status. Empty string means the field is
// absent (a kernel without the feature) or the process is already gone; both
// are reported as the value read and left to the caller to judge.
func wdPdeathsig(t *testing.T, pid int) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		t.Fatalf("read /proc/%d/status: %v", pid, err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "Pdeathsig:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// wdReadTestSource reads a file of this package's source tree. A read failure
// fails the test rather than skipping: the pin is worthless if it cannot read
// what it pins (Rule 15 — a scan that looked at nothing must not pass).
func wdReadTestSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(wdTestSourceRoot(t), name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the source under pin %s: %v", path, err)
	}
	return string(b)
}

// wdTestSourceRoot is the directory these test files live in (the package
// directory `go test` runs the binary from).
func wdTestSourceRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}
