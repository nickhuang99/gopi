// The read-deadline tests: what a deadline that expires INSIDE a frame means,
// and what the live session must do so that it cannot happen at all.
//
// THE BUG THESE PIN, measured on a live `/voice` dictation session (branch
// task/01a11d7370a3730c84dabcc80db2985a @823305a17):
//
//	streaming asr: read: unknown WebSocket opcode 0x3
//
// readFrame reads a frame with io.ReadFull, so a deadline that lands after the
// first header byte but before the second leaves the framing state HALF
// CONSUMED and unrecoverable — the next readFrame then parses the rest of that
// frame's bytes as a header, and a payload byte (0x03 here) arrives as an
// opcode. readOne mapped EVERY timeout to (nil, nil) = "silence", so readLoop
// read straight past the interruption. The conflation of "a quiet service" with
// "a half-eaten frame" is the root cause; both tests below are its regression
// guards, and each drives a REAL socket (socketPair), never an in-memory buffer.
package doubaostream

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"
)

const (
	// midFrameDeadline is the read budget that lands MID-FRAME: short enough
	// that a test pays it once, long enough that the frame's first byte is
	// certainly readable before it fires.
	midFrameDeadline = 150 * time.Millisecond
	// midFramePause is how long the fake service holds the rest of a frame back.
	midFramePause = 300 * time.Millisecond
	// testFramePayloadLen is the declared length of the test's one small frame.
	testFramePayloadLen = 0x03
	// testFramePayload is that frame's payload; its first byte is the 0x03 that
	// an interrupted parse reports as an opcode.
	testFramePayload = "abc"
	// liveFlowSilence is how long the fake service stays quiet in the live
	// session test — longer than the read budget that used to be armed during
	// the audio flow, and shorter than Finish's own wait.
	liveFlowSilence = 450 * time.Millisecond
	// liveReaderWait bounds how long a test waits for the session's reader to
	// reach its verdict.
	liveReaderWait = 3 * time.Second
)

// TestReadDeadlineMidFrameIsAnErrorNotSilence is THE regression test for the
// live dictation death. It expires a read deadline in the MIDDLE of a frame
// header — one header byte delivered, the rest withheld — and requires the
// timeout to surface as a READ FAILURE.
//
// What the pre-fix code did instead is printed by the failure below: readOne
// answered (nil, nil) = silence, so a session kept reading and the remaining
// bytes of the interrupted frame were parsed as a header. That is the exact
// mechanism that produced "unknown WebSocket opcode 0x3".
func TestReadDeadlineMidFrameIsAnErrorNotSilence(t *testing.T) {
	c, srv := socketPair(t)

	// The service begins a binary frame and stalls: the second header byte does
	// not arrive before the client's deadline.
	if _, err := srv.Write([]byte{finBit | opcodeBinary}); err != nil {
		t.Fatalf("write the first header byte: %v", err)
	}

	r := &Result{ReqID: "reqid-midframe", LogID: "logid-midframe"}
	msg, err := r.readOne(c, time.Now(), midFrameDeadline, nil, r.LogID)

	if err == nil {
		// The message this bug is named for, quoted before it is caused.
		t.Logf("PRE-FIX: the live session reported exactly this = %q", "streaming asr: read: unknown WebSocket opcode 0x3")
		// PRE-FIX BEHAVIOUR — readOne reported silence and the session read on.
		// This is what that next read did: it parsed the rest of the frame as a
		// header. The payload's own first byte became an opcode.
		if _, werr := srv.Write([]byte{testFramePayloadLen}); werr != nil {
			t.Fatalf("write the interrupted frame's length byte: %v", werr)
		}
		if _, werr := srv.Write([]byte(testFramePayload)); werr != nil {
			t.Fatalf("write the interrupted frame's payload: %v", werr)
		}
		_, _, _, desync := c.readFrame()
		t.Fatalf("a read deadline that expired MID-FRAME was reported as silence (msg=%v, err=%v), "+
			"but the framing state is half-consumed and is NOT resumable; the read that followed got %v",
			msg, err, desync)
	}

	// The failure must still be the deadline (not a torn socket, not a close),
	// and it must reach the caller as a READ failure — the op the session
	// reports, so the operator reads "read: ..." and not "unknown opcode".
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("the mid-frame failure must still be a read deadline, got %T: %v", err, err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Op != "read" {
		t.Fatalf("a mid-frame timeout must surface as an *APIError with Op=read, got %T: %v", err, err)
	}
}

// TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible pins the OTHER half
// of the fix, and it is the half that makes the bug IMPOSSIBLE rather than
// merely detected: the audio-flow phase arms NO deadline, so no deadline can
// land inside a frame while the operator is speaking.
//
// A zero duration therefore means "wait indefinitely" — and the socket's own
// deadline must be CLEARED, not merely forgotten: one left standing from an
// earlier phase (Open reads the request's answer under a packet budget) fires
// mid-frame exactly as a per-frame one would.
func TestClearingTheReadDeadlineMakesAMidFrameTimeoutImpossible(t *testing.T) {
	c, srv := socketPair(t)

	// An earlier phase arms a deadline, as Open does (packetReadBudget).
	c.SetReadDeadline(midFrameDeadline)
	c.SetReadDeadline(0)
	if c.readTimeout != 0 {
		t.Fatalf("SetReadDeadline(0) armed %v — a deadline during the audio flow can land inside a frame, "+
			"which is the desync this phase exists to prevent", c.readTimeout)
	}

	if _, err := srv.Write([]byte{finBit | opcodeBinary}); err != nil {
		t.Fatalf("write the first header byte: %v", err)
	}
	go func() {
		time.Sleep(midFramePause)                     //stylegate:allow-sleep holds the second half of the frame back for longer than the read deadline under test — the pause IS the case
		_, _ = srv.Write([]byte{testFramePayloadLen}) //stylegate:ignore writes the frame's length byte from the fake server; the client's deadline behaviour is the assertion
		_, _ = srv.Write([]byte(testFramePayload))    //stylegate:ignore writes the frame payload that completes what the length byte announced
	}()

	_, _, payload, err := c.readFrame()
	if err != nil {
		t.Fatalf("a read with no deadline failed while the peer held the rest of the frame back for %v: %v",
			midFramePause, err)
	}
	if string(payload) != testFramePayload {
		t.Fatalf("payload = %q, want %q", payload, testFramePayload)
	}
}

// TestSilenceAtAFrameBoundaryIsStillSilence guards the distinction the whole fix
// rests on: a deadline with NOTHING consumed is silence — no frame was started,
// the next read begins at the same boundary — and it must stay a state, not a
// failure. The live session counts these to notice the end of the transcript,
// and Finish's bounded wait depends on them.
func TestSilenceAtAFrameBoundaryIsStillSilence(t *testing.T) {
	c, _ := socketPair(t)

	r := &Result{ReqID: "reqid-quiet", LogID: "logid-quiet"}
	msg, err := r.readOne(c, time.Now(), midFrameDeadline, nil, r.LogID)
	if err != nil {
		t.Fatalf("silence at a frame boundary must not be a failure: %v", err)
	}
	if msg != nil {
		t.Fatalf("silence at a frame boundary must not produce a message: %+v", msg)
	}
}

// TestLiveReadLoopSurvivesASlowFrame drives the LIVE session's own reader loop
// (readLoop) against a real socket whose frame arrives LATE and IN PIECES: the
// header begins, the service goes quiet for longer than the flow phase's old
// read budget, and only then does the rest of the frame arrive.
//
// The audio-flow phase must arm NO deadline, so the pause is simply a wait: the
// reader stays parked, the frame is folded when it lands, and the transcript
// arrives. On the pre-fix code the reader woke up at 200 ms with a byte of the
// header already consumed and reported it as silence — this test then observes
// the session either dying on the garbage parse ("unknown WebSocket opcode 0x3")
// or never producing the transcript at all.
func TestLiveReadLoopSurvivesASlowFrame(t *testing.T) {
	c, srv := socketPair(t)

	partials := make(chan Partial, 4)
	s := &Session{
		conn:       c,
		res:        &Result{ReqID: "reqid-live", LogID: "logid-live"},
		start:      time.Now(),
		logID:      "logid-live",
		readerDone: make(chan struct{}),
	}
	s.opts.OnPartial = func(p Partial) { partials <- p }
	go s.readLoop()
	t.Cleanup(func() {
		_ = c.Close() //stylegate:ignore closes the client of the deadline fixture after the read verdict was asserted
		select {
		case <-s.readerDone:
		case <-time.After(liveReaderWait):
			t.Errorf("the reader did not return after the connection was closed")
		}
	})

	doc := `{"reqid":"reqid-live","code":1000,"sequence":1,"duration":10,"result":[{"text":"你好"}]}`
	gz := gzipBytes(t, []byte(doc))
	msg := wsServerFrame(serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gz))}, gz))

	// ONE header byte, then silence for longer than the old flow-phase budget.
	if _, err := srv.Write([]byte{msg[0]}); err != nil {
		t.Fatalf("write the first header byte: %v", err)
	}
	select {
	case p := <-partials:
		t.Fatalf("a partial arrived before the frame was even complete: %+v", p)
	case <-s.readerDone:
		t.Fatalf("the reader stopped on a frame that was merely slow: %v", s.readerErrForTest())
	case <-time.After(liveFlowSilence):
	}

	// The rest of the frame lands. The reader must still be parked on the SAME
	// frame and fold it — nothing about its state was lost.
	if _, err := srv.Write(msg[1:]); err != nil {
		t.Fatalf("write the rest of the frame: %v", err)
	}

	select {
	case p := <-partials:
		if p.Text != "你好" {
			t.Fatalf("partial = %q, want the transcript the service sent", p.Text)
		}
	case <-s.readerDone:
		t.Fatalf("the live reader failed on a frame that arrived in pieces: %v", s.readerErrForTest())
	case <-time.After(liveReaderWait):
		t.Fatalf("the live reader never folded a frame that arrived in two pieces across a %v pause "+
			"(the flow phase must arm NO read deadline)", liveFlowSilence)
	}
	if err := s.readerErrForTest(); err != nil {
		t.Fatalf("the live reader recorded a failure on a frame that was merely slow: %v", err)
	}
	if got := s.res.Text; got != "你好" {
		t.Fatalf("transcript = %q, want the frame the service sent — a half-consumed frame is not resumable, "+
			"so an interrupted parse here is the desync that kills a dictation", got)
	}
}

// wsServerFrame wraps an application-protocol frame in a WebSocket frame the way
// the SERVICE sends one: FIN|binary, no mask, and the length in whichever of the
// escape forms it needs.
func wsServerFrame(app []byte) []byte {
	var out []byte
	switch n := len(app); {
	case n < len16Marker:
		out = append(out, finBit|opcodeBinary, byte(n))
	case n <= 0xFFFF:
		out = append(out, finBit|opcodeBinary, len16Marker, byte(n>>8), byte(n))
	default:
		out = append(out, finBit|opcodeBinary, len64Marker)
		var ext [wsLen64Bytes]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		out = append(out, ext[:]...)
	}
	return append(out, app...)
}

// readerErrForTest reads the failure the reader recorded, if any.
func (s *Session) readerErrForTest() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
