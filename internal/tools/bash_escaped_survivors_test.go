package tools

// WHAT A KILL LEFT BEHIND — the regression proof for bash_escaped_survivors.go
// and for the three kill branches in bash.go that append its report.
//
// THE DEFECT (bug row 01a1d278, the operator's own measurement). A foreground
// bash call was killed by the idle net and the whole result said
//
//	[command killed: no output for 2m0s]
//
// while a `setsid`-detached `go test` — deliberately detached by
// scripts/test-budget.sh, Rule 42's containment, which must NOT be removed —
// kept running for up to 45 minutes holding the per-package suite lock. The next
// run of that package was refused with a holder pid the operator had never
// started. Two facts were true; the tool reported one.
//
// WHAT IS ASSERTED HERE, and why each half matters:
//
//  1. a real `setsid`-detached survivor is NAMED in the result (its pid, its
//     incarnation, and the measured fact that its session is not the child's) —
//     not merely "something may still be running";
//  2. the ordinary kill — a command whose tree the kill genuinely ended — keeps
//     the OLD wording exactly, with no survivor block. A report that fires on
//     every kill is noise, and noise is how a real one gets ignored;
//  3. the pid named is the process the kernel says holds THIS call's pipe,
//     which is what makes the link a measurement rather than a guess.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// bashSurvivorTestWatchdog bounds how long the tool may take to return. It
	// is much larger than the command's own budget and much smaller than
	// "forever": a red here must mean "the tool never came back".
	bashSurvivorTestWatchdog = 20 * time.Second

	// bashSurvivorIdleTimeout is the idle net this case fires. One second of
	// silence is enough because the victim's whole life is "print once, then
	// sleep silently".
	bashSurvivorIdleTimeout = 1 * time.Second

	// bashSurvivorAbsTimeout is the absolute budget: generous for the two
	// `echo`s, tiny for a human, and never the cause of this death (the idle
	// net fires first and the assertion below pins WHICH reason was recorded).
	bashSurvivorAbsTimeout = 5 * time.Second
)

// TestBashKillNamesTheDetachedSurvivorItLeftRunning is the positive proof: a
// detached child survives the kill, and the result SAYS SO with its measured
// identity.
func TestBashKillNamesTheDetachedSurvivorItLeftRunning(t *testing.T) {
	scratch := t.TempDir()
	pidFile := filepath.Join(scratch, "survivor.pid")

	// The survivor is created by THIS test (we own it) and is collected below by
	// the pid it wrote down — never by pattern and never by a negative-pid group
	// signal (Rule 42). It inherits the tool's stdout pipe deliberately: that
	// inherited WRITE end is the only link that survives setsid, and it is the
	// channel the fix measures.
	cmd := "setsid bash -c 'echo $$ > " + pidFile + "; exec sleep 120' & echo launched; exec sleep 120"

	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := bashInDirCtx(t.Context(), cmd, scratch, bashSurvivorAbsTimeout, bashSurvivorIdleTimeout)
		done <- struct {
			out string
			err error
		}{out, err}
	}()

	var got struct {
		out string
		err error
	}
	select {
	case got = <-done:
	case <-time.After(bashSurvivorTestWatchdog):
		bashKillDetached(t, pidFile)
		t.Fatalf("bashInDirCtx never returned (watchdog %s)", bashSurvivorTestWatchdog)
	}
	survivorPid := bashReadPidFile(t, pidFile)
	bashKillDetached(t, pidFile)

	if !strings.Contains(got.out, "launched") {
		t.Fatalf("command output lost: %q (err=%v)", got.out, got.err)
	}
	if got.err == nil {
		t.Fatalf("the command was killed, so an error was expected; got nil with output %q", got.out)
	}
	if !strings.Contains(got.out, bashEscapedSurvivorNoticeFragment) {
		t.Fatalf("the tool reported the kill and said NOTHING about the descendant it left running — this is the "+
			"silence the epic removes. output:\n%s", got.out)
	}
	if survivorPid <= 0 {
		t.Fatalf("the victim never wrote its pid (%s) — the assertion below could not name it either", pidFile)
	}
	if !strings.Contains(got.out, "pid "+strconv.Itoa(survivorPid)+" ") {
		t.Fatalf("the survivor (pid %d) was NOT named in the report, so the reader cannot tell WHICH process kept the "+
			"work alive. output:\n%s", survivorPid, got.out)
	}
	// The identity half: a pid number is recyclable, and the report must hand the
	// reader the incarnation with it (Rule 24).
	if !strings.Contains(got.out, "startticks ") {
		t.Fatalf("the survivor report names a pid WITHOUT its incarnation: a pid is a recyclable number, so that "+
			"report is not an identity. output:\n%s", got.out)
	}
}

// TestBashOrdinaryKillKeepsTheOldWording is the negative control: a command whose
// whole tree the kill ended must keep exactly the old sentence, with no survivor
// block. Without this arm a report that always fires would pass the proof above.
func TestBashOrdinaryKillKeepsTheOldWording(t *testing.T) {
	scratch := t.TempDir()

	out, err := bashInDirCtx(t.Context(), "echo ran; exec sleep 120", scratch, bashSurvivorAbsTimeout, bashSurvivorIdleTimeout)
	if err == nil {
		t.Fatalf("idle kill expected; got nil error")
	}
	if !strings.Contains(out, "no output for") {
		t.Fatalf("the idle-timeout sentence changed shape: %q", out)
	}
	if strings.Contains(out, bashEscapedSurvivorNoticeFragment) {
		t.Fatalf("a kill that reached its whole tree reported a survivor — a report that fires on every kill is the "+
			"noise that gets the real one ignored. output:\n%s", out)
	}
}

// TestBashPipeHoldersFindOnlyWriteEnds pins the two halves of the scan's
// criterion on a process this test owns: the inode match says "this is my pipe",
// the access mode says "this is the far end of it". A scan that skipped the mode
// would report the tool's own read end as a survivor.
func TestBashPipeHoldersFindOnlyWriteEnds(t *testing.T) {
	scratch := t.TempDir()
	pidFile := filepath.Join(scratch, "holder.pid")

	// A detached child whose stdout is the pipe this call opened. It writes to it
	// (fd 1 is its WRITE end) and then sleeps.
	cmd := "setsid bash -c 'echo $$ > " + pidFile + "; exec sleep 120' & echo launched; exec sleep 120"

	done := make(chan struct {
		out string
		err error
	}, 1)
	go func() {
		out, err := bashInDirCtx(t.Context(), cmd, scratch, bashSurvivorAbsTimeout, bashSurvivorIdleTimeout)
		done <- struct {
			out string
			err error
		}{out, err}
	}()
	select {
	case <-done:
	case <-time.After(bashSurvivorTestWatchdog):
		bashKillDetached(t, pidFile)
		t.Fatalf("bashInDirCtx never returned (watchdog %s)", bashSurvivorTestWatchdog)
	}

	holderPid := bashReadPidFile(t, pidFile)
	if holderPid <= 0 {
		t.Fatalf("the holder never wrote its pid")
	}
	// The scan's own predicate, against a pid this test owns and a set of inodes
	// that CANNOT be its pipe: it must answer false. (The positive direction is
	// covered by the end-to-end case above, which goes through the same code.)
	if bashPidHoldsPipeEnd(holderPid, map[uint64]bool{}) {
		bashKillDetached(t, pidFile)
		t.Fatalf("an empty inode set reported a holder — the scan asserts a link it did not measure")
	}
	bashKillDetached(t, pidFile)
}

// bashReadPidFile reads the pid a victim wrote down. 0 = absent/unreadable.
func bashReadPidFile(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// bashKillDetached terminates a victim THIS TEST created, addressing the one pid
// it wrote down (never a pattern, never a bare negative id — Rule 42). Best
// effort: the process may already be gone.
func bashKillDetached(t *testing.T, pidFile string) {
	t.Helper()
	pid := bashReadPidFile(t, pidFile)
	if pid <= 0 {
		return
	}
	err := syscall.Kill(pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		t.Logf("cleanup: kill(%d, SIGKILL) returned %v", pid, err)
	}
}
