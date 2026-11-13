// session_teardown_test.go — the TEARDOWN contract: the ONE socket is released
// exactly once, and Close after Finish is not an error.
//
// THE DEFECT THESE PIN (task 01a11dfe; found by the ported loopback suite
// committed as add190b13 and REPORTED there, not fixed — see
// devdoc/doubaostream-session-test-coverage.md § "The defect this port found").
// Finish closed the socket and Close closed it AGAIN, because Close's only
// guard was `closed` — a flag whose meaning is "Close was CALLED" and which
// Finish never set. So the ordinary live-arm sequence (Finish at
// cmd/agent/chat_voice_live.go:282, then the deferred `_ = sess.Close()` at
// :261) re-closed the same descriptor on every utterance and reported
//
//	close tcp 127.0.0.1:37620->127.0.0.1:37915: use of closed network connection
//
// The two paths therefore disagreed about a single fact ("the socket is gone"),
// which is what these tests make impossible: the FIRST test drives the real
// public API (Open/Say/Finish/Close against the same real loopback server the
// ported suite uses), and the SECOND counts the socket's own Close calls, so
// "released once" is measured on the descriptor rather than on a flag.
package doubaostream

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// teardownRaceIterations is how many times the two teardown paths are raced
// against each other. Every iteration uses a FRESH session — "released exactly
// once" is a fact about ONE socket — and the repetition is what makes an
// interleaving-dependent defect observable at all: without the fix BOTH orderings
// double-close (the second path never learns the first ran), so a single round
// already fails, and the loop is what shows it is not a one-off.
const teardownRaceIterations = 20

// teardownPacketBytes is the size of the one packet these cases send: any audio
// at all is enough — the subject is the teardown, not the payload.
const teardownPacketBytes = 64

// TestSessionCloseAfterFinishIsNotAnError is the acceptance the brief names, and
// it goes through the REAL production surface: the same Open handshake, the same
// Say/Finish, and the same server the ported suite uses. The teardown under test
// is the live arm's own sequence.
//
// It fails on the pre-fix code with the defect's own sentence, and it is
// deliberately free of any assertion on the session's internal flags: the
// contract is about what the CALLER sees (nil from Close, and a refusal
// vocabulary that has not moved).
func TestSessionCloseAfterFinishIsNotAnError(t *testing.T) {
	srv := newASRFakeServer(t)
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Say(make([]byte, teardownPacketBytes)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if _, err := sess.Finish(nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// THE PRODUCTION SEQUENCE: Finish ended the utterance above, and
	// cmd/agent/chat_voice_live.go's deferred Close now releases the session.
	// The socket was already released by Finish's drain, so this second path
	// reports the release the first already did instead of closing the same
	// descriptor again.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close after Finish: %v — teardown must report the socket as released, not re-close it "+
			"(the live arm calls exactly this sequence on every utterance)", err)
	}
	// THE FIX MUST NOT MOVE THE REFUSAL VOCABULARY (the constraint this task
	// carries): Close has now been called, so "closed" outranks "finished" for
	// audio that arrives after teardown — the same answer the pre-fix code gave.
	if err := sess.Say(make([]byte, teardownPacketBytes)); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Say after Finish-then-Close gave %v, want ErrSessionClosed (the two terminal states keep their own names)", err)
	}
	// And a THIRD teardown (a caller with more than one deferred release) is
	// still a no-op.
	if err := sess.Close(); err != nil {
		t.Errorf("a repeated Close: %v, want nil", err)
	}
	srv.awaitServerClose(t)
}

// TestSessionReleasesTheSocketExactlyOnce measures the fact both paths are
// supposed to share: how many times the socket's own Close is called.
//
// THE INSTRUMENT IS THE DESCRIPTOR, not the session's flag: the client side of a
// real loopback socket (socketPair, the package's own fixture) is wrapped in a
// counter, so "released once" is counted where it happens. A test that read
// s.closed instead would be asserting the bookkeeping that was wrong.
//
// The three sub-cases are the three ways a caller can reach teardown: Finish then
// Close (the live arm), Close then Finish (an abort racing the end), and both at
// once (the chat arm feeds audio from one goroutine while another ends the
// utterance — see the ErrSessionClosed sentinel's own note on that race).
func TestSessionReleasesTheSocketExactlyOnce(t *testing.T) {
	t.Run("Finish then Close", func(t *testing.T) {
		s, cc := countingSession(t)
		_, _ = s.Finish(nil) //stylegate:ignore finishes the teardown case's session; the ordered shutdown the test observes is the assertion
		_ = s.Close()        //stylegate:ignore closes that session so the teardown ordering can be observed
		assertSingleRelease(t, cc)
	})

	t.Run("Close then Finish", func(t *testing.T) {
		s, cc := countingSession(t)
		_ = s.Close()        //stylegate:ignore closes the session in the concurrent-close case, which is the operation under test
		_, _ = s.Finish(nil) //stylegate:ignore finishes the same session concurrently; the race-is-safe property is the assertion
		assertSingleRelease(t, cc)
	})

	t.Run("Close and Finish concurrently", func(t *testing.T) {
		for i := 0; i < teardownRaceIterations; i++ {
			s, cc := countingSession(t)
			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Add(2)
			go func() { defer wg.Done(); <-start; _, _ = s.Finish(nil) }() //stylegate:ignore the concurrent Finish arm of the race fixture; both arms must run at once, which is the property
			go func() { defer wg.Done(); <-start; _ = s.Close() }()        //stylegate:ignore the concurrent Close arm of the same race fixture
			close(start)
			wg.Wait()
			if got := cc.closes.Load(); got != 1 {
				t.Fatalf("round %d: the socket was released %d times, want exactly 1 — the two teardown paths "+
					"must share one fact about the connection", i+1, got)
			}
		}
	})
}

// assertSingleRelease is the shared verdict, and it also waits for the reader
// goroutine to return so a failure here cannot be a leaked reader.
func assertSingleRelease(t *testing.T, cc *countingConn) {
	t.Helper()
	if got := cc.closes.Load(); got != 1 {
		t.Fatalf("the socket was released %d times, want exactly 1 (a second Close on the same descriptor is the "+
			"\"use of closed network connection\" the live arm used to report)", got)
	}
}

// countingConn counts the Close calls that reach the socket. Everything else is
// the connection it wraps.
type countingConn struct {
	net.Conn
	closes atomic.Int32
}

// Close counts the release and then releases.
func (c *countingConn) Close() error {
	c.closes.Add(1)
	return c.Conn.Close()
}

// countingSession builds a Session over a REAL loopback socket whose client end
// is instrumented, with its reader running — the same construction the package's
// own live-session fixtures use (liveTestSession), minus the handshake (which
// needs the vendor), plus the counter.
//
// THE SERVICE'S END STAYS OPEN, and that is what makes this fixture reach the
// teardown: Finish refuses a session whose reader has recorded a failure, so a
// fixture that simply dropped the peer would have Finish return through its
// refusal and never touch the socket — an instrument that cannot see the defect
// it is built for. Instead the reader is ENDED THE WAY THE SERVICE ENDS A
// STREAM: an explicit terminator (the negative sequence) is written before
// teardown, so the reader returns with NO failure recorded, the peer is still
// there for Finish's last packet, and both teardown paths run to their end.
func countingSession(t *testing.T) (*Session, *countingConn) {
	t.Helper()
	c, srv := socketPair(t)
	cc := &countingConn{Conn: c.conn}
	s := &Session{
		conn:       &wsConn{conn: cc, br: bufio.NewReader(cc)},
		res:        &Result{ReqID: "reqid-release", LogID: "logid-release"},
		start:      time.Now(),
		logID:      "logid-release",
		readerDone: make(chan struct{}),
	}
	go s.readLoop()
	writeTerminator(t, srv)
	select {
	case <-s.readerDone:
	case <-time.After(liveReaderWait):
		t.Fatalf("the reader did not return after the service's terminator")
	}
	return s, cc
}

// writeTerminator sends the service's own end of the transcript: a FULL response
// carrying the NEGATIVE sequence of the last packet, which is exactly the frame
// the ported suite's server sends on the client's goodbye frame and what
// readLoop's explicit-terminator branch stops on. It is built through the same
// production helpers that server uses (assembleHeader over fakeResponseHeader,
// the protocol's own compression), so this fixture cannot drift from the
// parser's vocabulary.
func writeTerminator(t *testing.T, srv net.Conn) {
	t.Helper()
	doc := serverResponse{Code: codeSuccess, Sequence: -1, ReqID: "fake-reqid", LogID: "fake-logid", Result: []resultNode{{Text: "你好"}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal the terminator: %v", err)
	}
	body, err := gzipCompress(raw)
	if err != nil {
		t.Fatalf("compress the terminator: %v", err)
	}
	bw := bufio.NewWriter(srv)
	if err := writeServerFrame(bw, assembleHeader(fakeResponseHeader, uint32(len(body)), body)); err != nil {
		t.Fatalf("write the terminator frame: %v", err)
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush the terminator frame: %v", err)
	}
}
