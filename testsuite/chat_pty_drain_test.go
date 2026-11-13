package testsuite

// chat_pty_drain_test.go — the regression guard for the pty harness's TWO
// read-starvation defects (both fixed 2026-10-30, both measured on this suite's
// own delta=1 case):
//
//	1. ptySession.close() waited on cmd.Wait() alone, so a child whose shutdown
//	   still had output to write parked in write(2) once the pty kernel buffer
//	   filled — and the failure surfaced as "chat did not exit within 1m0s".
//	2. a settle-wait that polls an artifact with time.Sleep reads nothing from
//	   p.rx, so the drain goroutine blocks on its send into the BOUNDED channel
//	   (ptyReadBuf), nobody calls read(master), and the child freezes mid-answer.
//
// WHY THESE TWO TESTS AND NOT A pty CHAT. The defect is in the HARNESS, not in
// the product, so the guard drives a plain child whose output is larger than the
// pty buffer — the smallest sample that reproduces it (Rule 10-4) — and asserts
// the two properties that were missing: close() keeps consuming the stream until
// the child exits, and pumpFor() consumes it while a caller waits for something
// else.
//
// THE CHILD IS THE TEST'S OWN (Rule 42): it is started here, on a pty this file
// opened, and it writes to nobody else's descriptor.

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	// drainChildBytes is comfortably more than a pty's kernel buffer (a few KiB),
	// so an unread child MUST block: that is the reproduction, and a smaller
	// number would pass on the old code by luck.
	drainChildBytes = "262144"
	// drainChildCommand writes that many bytes and exits. No newline, so the pty
	// driver cannot fold the output into an editing line's echo.
	drainChildCommand = "head -c " + drainChildBytes + " /dev/zero | tr '\\0' 'x'; exit 0"
	// drainPumpBudget bounds one pumpFor call in the second test: the child writes
	// for far less than this once the stream is being consumed.
	drainPumpBudget = 30 * time.Second
	// drainPumpTick is the pumpFor step the second test uses: one reader's
	// cadence, short enough that the child is never starved between two steps.
	drainPumpTick = 20 * time.Millisecond
)

// startPtyChild launches an arbitrary child on a fresh pty, in the same shape
// startPTYChat uses for the product (Setsid+Setctty, so the child has a real
// terminal), and returns a ptySession bound to it.
func startPtyChild(t *testing.T, name string, args ...string) *ptySession {
	t.Helper()
	master, slavePath := openPty(t)
	slave, err := os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		_ = master.Close() //stylegate:ignore closing the master on the skip path
		t.Skipf("cannot open the pty slave device: %v", err)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()  //stylegate:ignore closing both ends on the fatal path
		_ = master.Close() //stylegate:ignore closing both ends on the fatal path
		t.Fatalf("start %s on a PTY: %v", name, err)
	}
	_ = slave.Close() //stylegate:ignore the child holds its own descriptor for the slave
	// The ONE constructor: it starts the drain and registers the teardown
	// (kill THIS child, close THIS master, wait for the drain to return). A
	// second hand-rolled cleanup here is how the three harnesses drifted apart
	// in the first place — this file's old version killed the child but left the
	// drain goroutine and the master behind.
	return newPTYSession(t, cmd, master)
}

// TestPtyCloseDrainsWhileTheChildStillHasOutput pins the FIRST defect: a child
// that exits only after its final write completes. On the old close() (wait on
// cmd.Wait() and read nothing) the write parks forever and the call dies on
// ptyQuitTimeout; the drained form returns the child's real exit status.
func TestPtyCloseDrainsWhileTheChildStillHasOutput(t *testing.T) {
	p := startPtyChild(t, "sh", "-c", drainChildCommand)
	started := time.Now()
	code := p.close(t)
	elapsed := time.Since(started)
	t.Logf("DRAIN child exited with code %d after %s (it wrote %s bytes, more than the pty buffer)",
		code, elapsed, drainChildBytes)
	if code != 0 {
		t.Fatalf("child exit = %d, want 0: the harness lost the child's output and its status", code)
	}
	if elapsed >= ptyQuitTimeout {
		t.Fatalf("close() took %s — the child's final write parked because nothing was reading the pty (want well under %s)",
			elapsed, ptyQuitTimeout)
	}
}

// TestPtyPumpForConsumesWhileTheCallerWaits pins the SECOND defect: a wait that
// is about something other than the stream (here, nothing at all — the caller
// only needs the child to finish) must keep the pty flowing. The assertion is
// the same child as above, but the caller never touches p.rx directly: the only
// reader is pumpFor.
func TestPtyPumpForConsumesWhileTheCallerWaits(t *testing.T) {
	p := startPtyChild(t, "sh", "-c", drainChildCommand)
	var buf strings.Builder
	// The child's own status comes out through the harness's ONE Wait (reap()),
	// not a second cmd.Wait(): Go allows exactly one Wait per process, and the
	// teardown registered by the constructor reaps too. The channel close is the
	// happens-before edge for p.waitErr.
	exited := p.reap()
	deadline := time.Now().Add(drainPumpBudget)
	for {
		select {
		case <-exited:
			if waitErr := p.waitErr; waitErr != nil {
				t.Fatalf("the child exited with an error after the pump consumed %d bytes: %v — the wait must return the child's real status, not a lost one",
					buf.Len(), waitErr)
			}
			t.Logf("PUMP child exited after %s; pumpFor consumed %d bytes while the caller waited on the process, not on the stream",
				drainPumpBudget-time.Until(deadline), buf.Len())
			return
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the child never exited within %s: %d bytes consumed and the stream stopped flowing — a wait that does not read the pty freezes the child",
				drainPumpBudget, buf.Len())
		}
		_ = p.pumpFor(&buf, "", drainPumpTick)
	}
}
