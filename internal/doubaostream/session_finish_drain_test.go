// The drain phase's own tests: Finish arms a deadline (the audio flow does not),
// and that deadline is where the mid-frame case is still reachable — so it has to
// be REPORTED there, never read past.
package doubaostream

import (
	"errors"
	"testing"
	"time"
)

// liveTestSession builds a Session over a real socket with its reader running —
// the same construction the live arm uses, minus the handshake (which needs the
// vendor). The reader starts with the flow phase's own setting: NO deadline.
func liveTestSession(t *testing.T) (*Session, *wsConn, netConn) {
	t.Helper()
	c, srv := socketPair(t)
	s := &Session{
		conn:       c,
		res:        &Result{ReqID: "reqid-drain", LogID: "logid-drain"},
		start:      time.Now(),
		logID:      "logid-drain",
		readerDone: make(chan struct{}),
	}
	go s.readLoop()
	t.Cleanup(func() {
		_ = c.Close() //stylegate:ignore closes the fake service's connection after the drain assertion was made
		select {
		case <-s.readerDone:
		case <-time.After(liveReaderWait):
			t.Errorf("the reader did not return after the connection was closed")
		}
	})
	return s, c, srv
}

// netConn is the peer side of a socketPair, named so the fixture's signature
// reads as what it is (the service's end of the connection).
type netConn interface {
	Write(p []byte) (int, error)
}

// TestFinishDrainReportsAMidFrameInterruption is the SAFETY NET's test, and it is
// the acceptance the brief names: "the drain phase still carries a deadline, so
// this case remains reachable there and must never silently build a transcript on
// garbage".
//
// The service begins a frame and stops mid-header while the drain is running.
// The deadline fires INSIDE the frame, so the framing state is half-consumed:
// the read must come back as a desync — a real *APIError{Op:"read"} — and the
// session must not fold one byte of that frame into a transcript.
func TestFinishDrainReportsAMidFrameInterruption(t *testing.T) {
	s, _, srv := liveTestSession(t)

	// The frame is left half-open: one header byte, then silence.
	if _, err := srv.Write([]byte{finBit | opcodeBinary}); err != nil {
		t.Fatalf("write the first header byte: %v", err)
	}

	started := time.Now()
	res, err := s.Finish(nil)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("the drain left a frame half-consumed and reported NO failure (text=%q) — a desynced stream "+
			"must never be presented as a transcript", res.Text)
	}
	if !errors.Is(err, ErrFrameDesync) {
		t.Fatalf("the drain's mid-frame interruption must be the desync error, got %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Op != "read" {
		t.Fatalf("a mid-frame interruption in the drain must surface as *APIError{Op:read}, got %T: %v", err, err)
	}
	if res.Text != "" {
		t.Fatalf("a transcript was built on a desynced stream: %q", res.Text)
	}
	// It came back because the reader TIMED OUT, not because Finish's own wait
	// expired — the distinction the two facts exist to keep apart.
	if s.FinishTimedOut() {
		t.Errorf("the wait expired although the reader returned at %v (a mid-frame read is bounded by the drain deadline)", elapsed)
	}
	if elapsed > sessionFinishWait {
		t.Errorf("the drain took %v, longer than its own bound %v", elapsed, sessionFinishWait)
	}
	t.Logf("drain reported the desync after %v: %v", elapsed, err)
}

// TestFinishStillEndsOnTheServicesSilence keeps the OTHER constraint pinned: a
// service that has simply stopped talking must still end the session through
// sessionQuietFrames, within sessionFinishWait, and WITHOUT claiming a desync —
// this is the behaviour the whole live arm's "did the transcript settle" verdict
// rests on, and it must survive the flow phase losing its deadline.
func TestFinishStillEndsOnTheServicesSilence(t *testing.T) {
	s, _, _ := liveTestSession(t)

	started := time.Now()
	res, err := s.Finish(nil)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("a service that never answered produced a transcript: %q", res.Text)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Op != "final" {
		t.Fatalf("a session that ended without a transcript and without a code must report Op=final, got %T: %v", err, err)
	}
	if errors.Is(err, ErrFrameDesync) {
		t.Fatalf("silence at a frame boundary was reported as a desync: %v", err)
	}
	if s.FinishTimedOut() {
		t.Errorf("the session's own quiet count did not end it: the wait expired after %v", elapsed)
	}
	if elapsed > sessionFinishWait {
		t.Errorf("Finish took %v, longer than its own bound %v", elapsed, sessionFinishWait)
	}
	t.Logf("Finish ended on the service's silence after %v (bound %v): %v", elapsed, sessionFinishWait, err)
}
