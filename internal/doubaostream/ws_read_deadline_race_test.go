// The MINIMAL regression guard for the read-deadline race (Rule 5).
//
// THE DEFECT: the frame reader (Result.readOne → wsConn.readFrame) and the
// session's write side (Session.Finish → wsConn.SetReadDeadline) both wrote the
// SAME duration — the deadline the next frame is armed with — with no
// synchronisation. `go test ./internal/doubaostream/ -race` reported it on every
// session test (7 distinct reports, all rooted at the field's write), and the
// suite never saw it because `make test-budget` runs without -race.
//
// WHY IT IS A RACE WITH A CONSEQUENCE, not a cosmetic one: the value decides
// whether a deadline is armed at all, so which write wins decides whether a
// deadline can land INSIDE a frame — the unrecoverable desync ErrFrameDesync
// reports (a payload byte arrives as "unknown WebSocket opcode 0x3").
//
// THE TEST IS TWO GOROUTINES AND NO CLOCK: the reader reads frames as fast as
// the peer writes them, the writer flips the deadline between the flow phase's
// "none" and the drain phase's budget. No load is manufactured (Rule 27) — the
// frames are a handful of bytes, and the fact under test is WHICH GOROUTINE
// writes the value, which the race detector answers at any rate.
package doubaostream

import (
	"testing"
	"time"
)

// raceTestFrames is how many frames the reader folds while the deadline is being
// flipped. Enough for the two goroutines to overlap, small enough to be free.
const raceTestFrames = 32

// TestReadDeadlineFlipIsNotARace pins the ONE thing the fix is about: the frame
// reader and the phase change (Finish's SetReadDeadline) may run concurrently,
// and neither may write a value the other is reading.
//
// On the pre-fix tree this test FAILS with "WARNING: DATA RACE" naming
// ws.go's SetReadDeadline write and readFrame's read of the same field. On the
// fixed tree it passes — and it passes for the RIGHT reason: the arm and the
// copy happen in one critical section, and the deadline the reader uses comes
// from the value the caller published.
func TestReadDeadlineFlipIsNotARace(t *testing.T) {
	c, srv := socketPair(t)

	// The reader: folds real frames off a real socket, arming its deadline at
	// every frame boundary the way Session.readLoop does.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for i := 0; i < raceTestFrames; i++ {
			if _, _, _, err := c.readFrame(); err != nil {
				return
			}
		}
	}()

	// The writer: the phase change Session.Finish performs — the flow phase
	// arms NO deadline, the drain phase arms one. It runs until the reader is
	// done, which is exactly the overlap the defect lived in.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		for {
			select {
			case <-readDone:
				return
			default:
			}
			c.SetReadDeadline(sessionFlowReadTimeout)
			c.SetReadDeadline(sessionDrainReadTimeout)
		}
	}()

	// The service: one small unmasked text frame per read, written from this
	// goroutine so a slow reader cannot stall the writer.
	for i := 0; i < raceTestFrames; i++ {
		if _, err := srv.Write([]byte{finBit | opcodeBinary, 0x01, 'x'}); err != nil {
			break
		}
	}

	select {
	case <-readDone:
	case <-time.After(liveReaderWait):
		t.Fatalf("the reader did not fold %d frames within %v", raceTestFrames, liveReaderWait)
	}
	select {
	case <-writeDone:
	case <-time.After(liveReaderWait):
		t.Fatalf("the deadline flipper did not stop within %v", liveReaderWait)
	}
}
