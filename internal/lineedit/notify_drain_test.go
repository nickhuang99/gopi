package lineedit

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// drainBound bounds how long a drain may take. A drain is a NON-BLOCKING
// operation by contract (poll has already reported readiness), so the bound is
// pure failure detection: a blocking drain never returns at all.
const drainBound = 2 * time.Second

// pipeCapacityProbeMax bounds the "fill the pipe" probe below. The kernel's pipe
// buffer is 64 KiB here, so this is an order of magnitude of headroom while
// still turning a non-filling pipe into a failure instead of a hang.
const pipeCapacityProbeMax = 1 << 20

// TestDrainPipeNeverBlocks pins the contract this editor froze on. drainPipe
// reads until the pipe reports "nothing left", which a BLOCKING descriptor can
// never report: the proving read parks instead of returning 0. On a real
// terminal the first notification therefore hung the reader forever (typed
// keystrokes were never read — the child sat in pipe_read while its PTY filled
// up), while every scripted-source test stayed green because those never touch
// the real pipe. Both shapes that occur in production are driven here: an EMPTY
// pipe (drain arriving after the notification was already consumed) and a pipe
// holding several notifications (a publish/resize burst).
func TestDrainPipeNeverBlocks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		bytes int
	}{
		{name: "empty pipe", bytes: 0},
		{name: "one notification", bytes: 1},
		{name: "a burst of notifications", bytes: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := newWakePipe()
			if err != nil {
				t.Fatalf("newWakePipe: %v", err)
			}
			defer p.close()
			for i := 0; i < tc.bytes; i++ {
				p.signal()
			}
			done := make(chan struct{})
			go func() {
				defer close(done)
				drainPipe(p.readFD)
			}()
			select {
			case <-done:
			case <-time.After(drainBound):
				t.Fatalf("drainPipe(%s) did not return within %s — the read end is blocking", tc.name, drainBound)
			}
		})
	}
}

// TestWakePipeIsNonBlockingOnBothEnds is the mechanism behind the contract
// above, asserted on the descriptor flags the kernel actually holds (not on a
// copied constant): the read end must be O_NONBLOCK for drainPipe, and the
// write end too, so a signaller can never park on a full pipe.
func TestWakePipeIsNonBlockingOnBothEnds(t *testing.T) {
	p, err := newWakePipe()
	if err != nil {
		t.Fatalf("newWakePipe: %v", err)
	}
	defer p.close()
	for name, fd := range map[string]int{"read": p.readFD, "write": p.writeFD} {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		if err != nil {
			t.Fatalf("F_GETFL(%s end): %v", name, err)
		}
		if flags&unix.O_NONBLOCK == 0 {
			t.Errorf("%s end flags = %#x, want O_NONBLOCK set: a blocking %s end is the freeze this pipe must not have", name, flags, name)
		}
	}
}

// TestWakeSignalNeverBlocksWhenThePipeIsFull proves the write half: once the
// pipe is full (nobody drained it yet), another signal must still return
// immediately — it is called from the completion worker, which must never be
// parked by a reader that is slow to wake.
func TestWakeSignalNeverBlocksWhenThePipeIsFull(t *testing.T) {
	p, err := newWakePipe()
	if err != nil {
		t.Fatalf("newWakePipe: %v", err)
	}
	defer p.close()
	// Fill the pipe completely, then signal well past capacity.
	var scratch [winchPipeBytes]byte
	for {
		n, err := unix.Read(p.readFD, scratch[:])
		if n <= 0 || err != nil {
			break // drained: the buffer was empty again
		}
	}
	// Fill by writing directly (a byte at a time) until the kernel refuses.
	filled := 0
	for {
		_, err := unix.Write(p.writeFD, scratch[:])
		if err != nil {
			break
		}
		filled++
		if filled > pipeCapacityProbeMax {
			t.Fatalf("pipe never filled after %d writes — the probe cannot establish a full pipe", filled)
		}
	}
	if filled == 0 {
		t.Fatal("could not write even one byte into the notification pipe")
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.signal() // must be a no-op-ish EAGAIN, never a park
	}()
	select {
	case <-done:
	case <-time.After(drainBound):
		t.Fatalf("signal() blocked on a full pipe (%d bytes queued) — the notification must be droppable", filled)
	}
}
