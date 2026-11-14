// worker_foreground_file_stop_test.go — the FILE channel of a foreground
// `gopi worker --stop`, and the end of the refusal.
//
// THE BUG THIS PINS (measured 2026-10-13, briefing for this task): a foreground
// worker had NO delivery channel at all — it resolved no control dir, watched no
// request file — so the control face's only door was a SIGNAL, and workerForegroundAsk
// refused to signal any target whose executable was not PROVABLY this binary.
// That is the NORMAL state after any rebuild (`/proc/<pid>/exe` →
// ".../bin/gopi (deleted)"), so `--stop` printed
//
//	NOT signalling: … stop it yourself: `kill -SIGTERM <pid>`
//
// on the most ordinary shape of the command — a refusal with no fallback.
//
// THE TWO HALVES, one assertion each:
//
//	① THE DELIVERY (foreign target): a target whose binary differs gets the
//	   durable REQUEST FILE, at the very path consumeDrainRequests reads for
//	   that pid, and NO signal reaches it (the process must still be alive —
//	   that is the observable half of "no signal").
//	② THE SIGNAL (proven target): a target whose executable IS this binary gets
//	   the file AND the signal, and really dies.
//
// Rule 27: no load is generated. The targets are `sleep` children the test owns
// and reaps; the assertions are about files and pids.
package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/procname"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// wffForeignExe / wffSelfExe are the two distinct executable FILES the
	// fixtures point /proc/<pid>/exe at: distinct inodes are what makes a target
	// "a DIFFERENT binary" (workerSameBinary compares identity, not a name).
	wffForeignExe = "gopi-foreign-build"
	wffSelfExe    = "gopi-this-build"
	// wffForeignAliveWait bounds the check that the foreign target really was
	// NOT signalled (it is alive after the ask) — a load-tolerant bound.
	wffForeignAliveWait = 2 * time.Second
	// wffDeathWait bounds the wait for the PROVEN target to die from its signal.
	wffDeathWait = 15 * time.Second
	// wffDeathPoll is that wait's cadence (Rule 1).
	wffDeathPoll = 20 * time.Millisecond
)

// wffFixture plants the procfs shape of pid and pins this test's profile, so the
// ask resolves the control dir under a temp home instead of the operator's.
// exePath is the file /proc/<pid>/exe resolves to.
func wffFixture(t *testing.T, root, home string, pid int, exePath string) {
	t.Helper()
	procFixtureExe(t, root, pid, binaryNameGopi,
		[]string{"gopi", procname.VerbWorker}, wfsForeignSession, exePath)
	t.Setenv(procname.EnvProcRoot, root)
	t.Setenv(config.GopiDirEnv, home)
}

// wffExecFile creates a real file with exec bits — the inode a fixture's
// /proc/<pid>/exe link resolves to.
func wffExecFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("create fixture executable %s: %v", path, err)
	}
	return path
}

// TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers is the bug's own
// repro, at the unit seam: a foreground target running a DIFFERENT binary must
// still be ASKED — by the durable request file — and must NOT be printed as an
// operator's manual job.
func TestWorkerStopForegroundDeliversByFileWhenTheBinaryDiffers(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	// Two DIFFERENT files: the target's image is provably not this process's.
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))
	if rel, _, _ := workerBinaryIdentity(target); rel.workerBinarySignallable() {
		t.Fatal("fixture is wrong: the two fixture executables must NOT be the same file")
	}

	out := captureStdoutAndStderr(t, func() int {
		workerControlStopForeground([]int{target}, 0, false)
		return 0
	})

	// ① THE DELIVERY: the request file exists at the path the worker's own
	// consumeDrainRequests reads for THIS pid — the one channel both faces share.
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	body, err := os.ReadFile(tools.ControlRequestPath(dir, workerStopRequestPrefix, target))
	if err != nil {
		t.Fatalf("no durable stop request for pid %d was delivered (%v):\n%s", target, err, out)
	}
	// …and it carries THIS process as the requester (the sender provenance a
	// termination line renders).
	if got := parseWorkerStopSender(body); got.PID != os.Getpid() {
		t.Errorf("request sender pid = %d, want %d (this process)", got.PID, os.Getpid())
	}
	if !strings.Contains(out, "stop request file") {
		t.Errorf("the ask must name the file as the delivery:\n%s", out)
	}
	// The ask must name the REAL reason the signal is withheld: a different
	// executable FILE, whose identity cannot be proven identical (bug 01a17df2
	// retired the "DIFFERENT binary" phrasing — it collapsed the foreign case and
	// the ordinary rebuild case, where the path is the SAME).
	if !strings.Contains(out, "DIFFERENT executable FILE") {
		t.Errorf("the ask must name the binary mismatch it is compensating for:\n%s", out)
	}

	// ② THE REFUSAL IS GONE: neither the dead-end sentence nor the old
	// "NOT signalling" wording may appear.
	for _, banned := range []string{"stop it yourself", "NOT signalling"} {
		if strings.Contains(out, banned) {
			t.Errorf("the dead-end wording %q is back (the operator is pushed off the graceful path):\n%s", banned, out)
		}
	}

	// ③ NO SIGNAL: the target is still alive. This is the observable half of the
	// conservative direction — a foreign binary's signal contract is UNKNOWN
	// (a pre-drain worker forwards a stop into its in-flight run child).
	deadline := time.Now().Add(wffForeignAliveWait)
	for time.Now().Before(deadline) {
		if !tools.PidAlive(target) {
			t.Fatalf("pid %d was SIGNALLED — the foreign target must get the request file only:\n%s", target, out)
		}
		time.Sleep(wffDeathPoll) //stylegate:allow-sleep bounded check that the foreign target was left alone
	}
}

// TestWorkerStopForegroundSignalsAProvenTarget pins the OTHER arm: when the
// target's executable IS this binary (same device+inode), the signal is added to
// the file — the file survives a missed signal, the signal makes the stop prompt.
func TestWorkerStopForegroundSignalsAProvenTarget(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	selfExe := wffExecFile(t, filepath.Join(root, wffSelfExe))
	// A child this test owns and REAPS: a killed-but-unreaped child stays a
	// zombie with a live /proc entry, which would make the liveness wait below
	// assert the wrong thing.
	cmd := exec.Command("/bin/sleep", "60")
	// Pdeathsig reaches this DIRECT child, so a SIGKILLed test binary leaves no
	// fixture; its cwd is not the subject (the exe identity is).
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Dir = t.TempDir()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start target child: %v", err)
	}
	target := cmd.Process.Pid
	reaped := make(chan struct{})
	go func() {
		_ = cmd.Wait() //stylegate:ignore reaping our own child is the point; its error is uninteresting here
		close(reaped)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //stylegate:ignore best-effort teardown of the test's own child
	})
	wffFixture(t, root, home, target, selfExe)
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), selfExe)
	if rel, _, _ := workerBinaryIdentity(target); !rel.workerBinarySignallable() {
		t.Fatal("fixture is wrong: both fixture pids must resolve to the SAME executable file")
	}

	out := captureStdoutAndStderr(t, func() int {
		if !workerControlStopForeground([]int{target}, wffDeathWait, false) {
			t.Errorf("the stop did not reach its end state for pid %d", target)
		}
		return 0
	})

	// THE EFFECT: the process is gone — the signal really landed.
	select {
	case <-reaped:
	case <-time.After(wffDeathWait):
		t.Fatalf("pid %d was not signalled (nothing reaped it within %s):\n%s", target, wffDeathWait, out)
	}
	if !strings.Contains(out, "FOREGROUND") {
		t.Errorf("the success line must name the pid it ended:\n%s", out)
	}
}

// TestWorkerForegroundStopIsIdempotent: a standing request is a durable FACT, so
// a second `--stop` reports the true state instead of rewriting the file or
// signalling again — the daemon arm's own contract, now on the file channel the
// foreground face shares.
func TestWorkerForegroundStopIsIdempotent(t *testing.T) {
	t.Cleanup(resetWorkerStopState) // hand the process-wide record back (worker_stop_state_leak_gate_test.go)
	root := t.TempDir()
	home := t.TempDir()
	target := liveWorkerChildren(t, 1)[0]
	wffFixture(t, root, home, target, wffExecFile(t, filepath.Join(root, wffForeignExe)))
	procFixtureExe(t, root, os.Getpid(), binaryNameGopi, []string{"gopi", procname.VerbWorker, workerControlStop},
		os.Getpid(), wffExecFile(t, filepath.Join(root, wffSelfExe)))
	dir, err := tools.ControlDir(workerStateDirName)
	if err != nil {
		t.Fatalf("resolve control dir: %v", err)
	}
	if err := workerRequestStopFile(dir, target); err != nil {
		t.Fatalf("seed the standing request: %v", err)
	}
	path := tools.ControlRequestPath(dir, workerStopRequestPrefix, target)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the seeded request: %v", err)
	}

	out := captureStdoutAndStderr(t, func() int {
		workerControlStopForeground([]int{target}, 0, false)
		return 0
	})

	if !strings.Contains(out, "already asked to leave") {
		t.Errorf("a repeated ask must report the standing request as the truth:\n%s", out)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat the request after the repeat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("the standing request was REWRITTEN (%s → %s); a repeat is a reported no-op",
			before.ModTime(), after.ModTime())
	}
}
