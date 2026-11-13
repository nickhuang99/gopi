//go:build linux

package lineedit

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

// inputpending_pty_test.go — the DIRECT test of the shipped
// (*Terminal).InputPending() over a REAL pty.
//
// Why it has to be a pty: InputPending is the only production implementation of
// the editor's inputBacklog seam (raw.go), and until this file no test anywhere
// used it as a SUBJECT. Every other source in the tree is a test double, so the
// shipped code path — poll(2) on stdin's captured descriptor — could be broken
// outright and the suite would stay green: the cmd/agent guard rides on its own
// scriptSource, and the package's scripted sources do not implement the seam at
// all. This test therefore measures the REAL descriptor through the REAL
// terminal object, and it uses no double anywhere.
//
// The three honest shapes (Rule 31 — the surface is executed, not reasoned
// about):
//
//	nothing written  → FALSE   (an idle terminal must never make the read loop
//	                            defer a publish)
//	byte written     → TRUE    (input the source has not read is pending; this is
//	                            the answer the ordering gate acts on)
//	byte read        → FALSE   (once read, the gate must stop deferring)
//
// plus the HANGUP arm: a pty whose peer has gone reads POLLHUP immediately, and
// it reports TRUE — the readiness probe answers instantly instead of parking on
// the poll's own slice budget.
//
// A host without /dev/ptmx skips: the descriptor IS the subject, so a substitute
// stream would assert nothing.

const (
	// inputPendingProbeWait bounds a wait for the kernel to make a written byte
	// readable. It is a synchronization bound (the write is synchronous; the
	// poll's visibility is a scheduling detail), never a measured duration, and
	// it is never reached on a healthy host.
	inputPendingProbeWait = time.Second
	// inputPendingProbeStep is how often such a wait re-asks the probe.
	inputPendingProbeStep = time.Millisecond
	// inputPendingPtyBytes is the payload written through the pty. A byte is all
	// the question needs, and the byte is a printable one so a stray echo (there
	// is none in raw mode) would be readable in a failure dump.
	inputPendingPtyBytes = "x"
)

// openInputPendingPTY opens a fresh pty pair, skipping the test when the host
// cannot provide one.
func openInputPendingPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx on this host (%v): the descriptor InputPending polls cannot be exercised", err)
	}
	fd := int(master.Fd())
	n, err := PtyNumber(fd)
	if err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("TIOCGPTN unavailable (%v): cannot resolve the pty slave", err)
	}
	if err := PtyUnlock(fd); err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("TIOCSPTLCK unavailable (%v): cannot open the pty slave", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		master.Close() //stylegate:ignore the harness's own fixture teardown
		t.Skipf("cannot open the pty slave (%v)", err)
	}
	return master, slave
}

// waitInputPending polls the probe until it answers want, reporting what it
// observed. The loop is a bound on a scheduling detail, never a delay the
// assertion depends on: with want=true it can only turn a false negative into a
// pass, and with want=false it is reached only if the probe really is stuck.
func waitInputPending(t *testing.T, term *Terminal, want bool) bool {
	t.Helper()
	deadline := time.Now().Add(inputPendingProbeWait)
	var got bool
	for {
		got = term.InputPending()
		if got == want || time.Now().After(deadline) {
			return got
		}
		time.Sleep(inputPendingProbeStep) //stylegate:allow-sleep bounded synchronization wait on the kernel's poll(2) visibility (see inputPendingProbeWait)
	}
}

// TestTerminalInputPending drives the SHIPPED probe over a real pty: false with
// nothing written, true once a byte is on the wire, false again once it has been
// read, true when the peer hangs up.
func TestTerminalInputPending(t *testing.T) {
	master, slave := openInputPendingPTY(t)
	defer slave.Close()  //stylegate:ignore the harness's own pty teardown
	defer master.Close() //stylegate:ignore the harness's own pty teardown
	term, err := Open(slave, io.Discard)
	if err != nil {
		t.Fatalf("lineedit.Open on a pty slave: %v", err)
	}
	defer term.Close()
	// The editor asks for the seam by a type assertion on the source — pin that
	// the REAL terminal is the object it finds, so this test measures the surface
	// production consults and not a lookalike.
	ed := NewEditor(term, io.Discard, promptForTest, nil)
	if ed.backlog == nil {
		t.Fatal("the editor installed no input-backlog seam over a real *Terminal — InputPending would never be consulted")
	}

	// Shape 1: nothing written yet.
	if got := waitInputPending(t, term, false); got {
		t.Fatalf("InputPending on an idle pty = true, want false (nothing has been written to the master)")
	}

	// Shape 2: a byte is waiting to be read.
	if _, err := master.Write([]byte(inputPendingPtyBytes)); err != nil {
		t.Fatalf("write to the pty master: %v", err)
	}
	if !waitInputPending(t, term, true) {
		t.Fatal("InputPending = false after a byte was written to the master, want true — the readiness gate would draw over queued input")
	}

	// Shape 3: the byte is read, so nothing is pending again.
	buf := make([]byte, len(inputPendingPtyBytes))
	n, err := slave.Read(buf)
	if err != nil {
		t.Fatalf("read the pty slave: %v", err)
	}
	if got := string(buf[:n]); got != inputPendingPtyBytes {
		t.Fatalf("read %q from the pty slave, want %q (raw mode must not transform it)", got, inputPendingPtyBytes)
	}
	if got := waitInputPending(t, term, false); got {
		t.Fatal("InputPending = true after the pending byte was read, want false — the gate would defer a publish with nothing queued")
	}

	// Shape 4: the peer hangs up. POLLHUP is immediate, and the probe reports it
	// as pending so the read loop reads the EOF instead of parking.
	if err := master.Close(); err != nil {
		t.Fatalf("close the pty master: %v", err)
	}
	if !waitInputPending(t, term, true) {
		t.Fatal("InputPending = false after the pty peer hung up, want true — the EOF the read loop must read would wait for another wake")
	}
}

// TestTerminalInputPending_UnknownDescriptorIsFalse pins the degraded arm: a
// Terminal whose input descriptor was never captured answers FALSE rather than
// guessing, which is the same arm a source without the seam takes (the editor
// keeps the unread drawing order). It is not a pty test — it asserts the guard
// the probe itself carries.
func TestTerminalInputPending_UnknownDescriptorIsFalse(t *testing.T) {
	term := &Terminal{inputFD: -1}
	if term.InputPending() {
		t.Fatal("InputPending with no captured input descriptor = true, want false (an unanswerable probe must not claim input is pending)")
	}
	// The probe's own boundary: no captured descriptor is BELOW fdMinValid, so
	// the guard above is the arm that runs (it must not reach poll at all with a
	// descriptor it never captured).
	if !(-1 < fdMinValid) {
		t.Fatalf("fdMinValid = %d: the probe's guard (inputFD < fdMinValid) does not cover the uncaptured descriptor -1, so the case above exercised something else", fdMinValid)
	}
}
