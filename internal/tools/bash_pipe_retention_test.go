package tools

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

// bashPipeRetentionWatchdog is how long the test is willing to wait for the
// bash tool to RETURN before declaring it wedged. It is deliberately much
// larger than the command's own budget (a few seconds) and much smaller than
// "forever": a red here must mean "the tool never came back", not "the machine
// was slow".
const bashPipeRetentionWatchdog = 20 * time.Second

// TestBashKillReturnsWhenADescendantHoldsThePipe is the regression proof for the
// wedge this file exists for:
//
//	A bash call whose child SURVIVES the kill must not hold the tool past
//	abbudget + grace.
//
// THE SHAPE. bashInDirCtx kills the child's process TREE and then waits for both
// pipe readers to reach EOF (step 9) BEFORE cmd.Wait() (step 10). EOF only
// arrives when every holder of the pipe's WRITE end has closed it. A descendant
// that leaves the tree — `setsid`/double-fork reparents it to init, so
// bashDescendantPIDs(pid) can no longer find it — keeps the write end open
// forever. At that moment BOTH the absolute budget and the idle net have already
// fired (the budget is what killed the child, and the idle monitor returns
// immediately after its own kill), so NOTHING else is left to fire: the call
// hangs with no deadline at all.
//
// The command below is the minimal sample: bash backgrounds a `setsid` sleeper
// and exits on its own. No timeout is involved on the child's side at all — the
// sleeper simply owns the pipe after bash is gone.
func TestBashKillReturnsWhenADescendantHoldsThePipe(t *testing.T) {
	scratch := t.TempDir()
	pidFile := filepath.Join(scratch, "detached.pid")

	// The sleeper is created by THIS test (we own it), announces its pid through
	// a file, and is collected below by that pid — never by pattern, never by a
	// negative-pid group signal (Rule 42).
	// NO redirection of the detached child's stdio: inheriting this call's pipe
	// write end is precisely the bind being reproduced.
	cmd := "setsid bash -c 'echo $$ > " + pidFile + "; exec sleep 300' & echo launched"

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)

	// A 2s absolute budget: generous for `echo launched`, tiny for a human.
	go func() {
		out, err := bashInDirCtx(t.Context(), cmd, scratch, 2*time.Second, 2*time.Second)
		done <- result{out, err}
	}()

	var got result
	select {
	case got = <-done:
	case <-time.After(bashPipeRetentionWatchdog):
		killDetachedFromPidFile(t, pidFile)
		t.Fatalf("bashInDirCtx never returned: the tool stayed blocked past its absolute budget (%s) "+
			"because a descendant left the process tree and kept the output pipe open "+
			"(watchdog %s)", 2*time.Second, bashPipeRetentionWatchdog)
	}
	killDetachedFromPidFile(t, pidFile)

	if !strings.Contains(got.out, "launched") {
		t.Fatalf("command output lost: %q (err=%v)", got.out, got.err)
	}
	// The release must not be silent (PROJECT.md Rule 15): the result says the
	// pipe was still held after the kill and that the tool released it itself.
	if !strings.Contains(got.out, bashPipeRetainedNoticeFragment) {
		t.Fatalf("a pipe released out from under a surviving descendant was not reported in the result: %q", got.out)
	}
	if got.err != nil {
		return // a killed/timeout error is a legitimate outcome; the point is that it RETURNED.
	}
}

// killDetachedFromPidFile terminates the sleeper this test created, addressing
// the one pid it wrote down. Best effort: the process may already be gone.
func killDetachedFromPidFile(t *testing.T, pidFile string) {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		return
	}
	if pgid, err := syscall.Getpgid(pid); err == nil && pgid == pid {
		killOwned(t, -pgid)
		return
	}
	killOwned(t, pid)
}

// killOwned delivers SIGKILL to a process (or group) this test created and
// holds the id of, and HANDLES the two answers that are not failures: the
// process is already gone (ESRCH, the expected case after a successful kill) or
// the id was never a process (EINVAL). Anything else is reported rather than
// dropped, so a cleanup that silently did nothing cannot pass for clean-up.
func killOwned(t *testing.T, pid int) {
	t.Helper()
	err := syscall.Kill(pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EINVAL) {
		return
	}
	t.Logf("cleanup: kill(%d, SIGKILL) returned %v", pid, err)
}
