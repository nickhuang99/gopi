package doubaosauc

// session.go — the LIVE half of the v3 SAUC channel: audio goes in as it is
// SPOKEN, and the transcript comes back WHILE the sentence is still being
// spoken.
//
// WHY THIS EXISTS BESIDE Transcribe. Transcribe answers "one audio file in, one
// transcript out", and it can because a file has all of its audio up front: it
// paces the packets against the audio timeline and returns when the last one
// has been answered. An INTERACTIVE dictation has no file — the audio arrives at
// the speed the operator speaks, so the caller's own capture IS the clock and
// there is nothing for this package to pace.
//
// THE V2 CHANNEL ALREADY HAS THIS SHAPE (doubaostream.Session), and this is its
// v3 sibling rather than a flag on it: the two channels share the TRANSPORT
// (doubaostream.WsDial/WriteBinary/ReadMessage) and share NOTHING of the
// application protocol, which is the rule protocol.go's package doc records —
// a "shared" implementation would be two implementations behind one name.
//
// MEASURED (2026-10-13, `bigmodel_async`, /tmp/rec1.wav, the probe in
// devdoc/doubao-asr-eval/minclient): the bidirectional endpoint answers every
// packet with a response whose `result.text` is the LATEST FULL TEXT, so the
// transcript GROWS while the audio is being sent — 能听到 → 能听到吗 → … — and
// the last response carries the low-nibble last-packet flag. That is the whole
// protocol difference this session rests on: the same envelope, folded the same
// way, with the text arriving many times instead of once.
//
// THE RESPONSE SHAPE IS NOT WHAT THE VENDOR'S SDK SNAPSHOT SUGGESTS. The
// snapshot in devdoc/doubao-asr-eval/sdk-snapshot wraps the payload in a
// `payload_msg` object; the endpoint measured above does NOT — the body is
// `{"audio_info":…,"result":{"text":…}}` directly, which is exactly what
// client.go's parseResponse already decodes. So no second envelope is
// implemented here: a `payload_msg` wrapper would have been a field nothing
// sends.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nick/gopi/internal/doubaostream"
)

// ── the read budgets of ONE live session ───────────────────────────────
const (
	// sessionFlowReadTimeout is the audio-flow phase's read deadline: NONE.
	//
	// THE WHOLE POINT IS THIS ZERO, and it is the same fact doubaostream's
	// session records: a read deadline can expire INSIDE a frame (part of a
	// header read, the rest still in the socket) and the framing state is not
	// resumable — reading on parses the rest of that frame as a header. While
	// the operator is still speaking there is nothing a timeout could notice,
	// so the reader parks on a frame and stays parked: the desync is impossible
	// by construction rather than merely detected.
	sessionFlowReadTimeout = 0
	// sessionDrainReadTimeout is the deadline the reader arms once the
	// end-of-stream flag is out: from there "the service has stopped talking" is
	// exactly what must be noticed. It is drainReadBudget — the SAME number the
	// file arm's drain uses, from the same place and for the same reason (it is
	// a fact about the service, not about a caller).
	sessionDrainReadTimeout = drainReadBudget
	// sessionQuietReads is how many consecutive timeouts AFTER the last packet
	// end the session — drainQuietReads from the same place, because it is the
	// same fact about the same service.
	sessionQuietReads = drainQuietReads
	// sessionFinishWait bounds how long Finish waits for the reader to reach that
	// silence, derived from the two numbers above plus one timeout of slack: a
	// service that answers promptly is never cut off, and one that has stopped
	// talking cannot hold the operator's key longer than the drain it is owed.
	sessionFinishWait = sessionDrainReadTimeout*time.Duration(sessionQuietReads+1) + sessionFlowReadTimeout
	// sessionCloseWait bounds the wait for the reader goroutine after the socket
	// is closed under it. A teardown bound, not a protocol timeout.
	sessionCloseWait = drainReadBudget
	// maxSayPacketBytes bounds ONE packet a caller may hand to Say. It is a
	// sanity bound rather than a protocol limit: a caller that hands over a whole
	// file would have the service wait out a stream it cannot pace, and the
	// refusal names the fact instead of failing in the service's own words.
	maxSayPacketBytes = 1 << 20
)

// Partial is one v3 partial transcript: the text known SO FAR (the service
// rewrites rather than appends — see the file header), and whether the service
// marked that sentence closed.
//
// IT IS THIS PACKAGE'S OWN TYPE rather than a reuse of doubaostream.Partial, for
// the same reason Result is: the two channels share the transport and nothing of
// the application protocol, and a shared type would be the first thread of the
// shared implementation the package doc refuses. The caller that needs one shape
// adapts at its own seam, where the difference is visible.
type Partial struct {
	// Index counts from 1 in arrival order.
	Index int
	// Text is the latest full text from the service.
	Text string
	// Definite is the service's own verdict that this utterance is closed.
	Definite bool
	// At is how long after the session's start this partial arrived — the
	// first-token latency when Index is 1.
	At time.Duration
}

// Session is ONE live v3 streaming session. It is created by Open, fed by Say,
// completed by Finish and released by Close.
//
// THE CALLING CONTRACT IS ONE GOROUTINE ON THE WRITE SIDE: Say and Finish are
// called by the goroutine that owns the capture, in order, never concurrently.
// The read side is this type's own goroutine.
type Session struct {
	opts  Options
	conn  *doubaostream.WsConn
	res   *Result
	start time.Time

	mu      sync.Mutex
	closed  bool
	endSent bool
	err     error
	// socketReleased reports that the socket has been RELEASED — by Finish's
	// drain or by Close, whichever arrived first. It is deliberately not the same
	// fact as `closed`: Finish's ordinary exit also releases the socket without
	// anyone having called Close, and the two callers must not close the same
	// descriptor twice.
	socketReleased bool
	// releaseDone closes when the claimer's release has returned, so the path
	// that did NOT claim it waits for the real release instead of racing ahead.
	releaseDone chan struct{}
	// finishTimedOut reports that Finish's bounded wait expired before the reader
	// reached the service's silence — see FinishTimedOut.
	finishTimedOut bool
	// lastSeen reports the service's own last-package flag.
	lastSeen bool
	// partialsSeen counts the partials delivered to the caller. It is counted
	// HERE (under the session's mutex) rather than read off the result, so a count
	// another goroutine publishes is one value and not a slice in motion.
	partialsSeen int
	// audioMS / nextSeq are the WRITE side's own bookkeeping, under mu.
	audioMS int
	nextSeq int32

	// onPartial is Options.OnPartial, kept so the reader does not re-read a
	// caller's struct.
	onPartial func(Partial)

	// readerDone closes when the reader goroutine has returned, so Finish can
	// WAIT for the end-of-stream answer instead of guessing that it arrived.
	readerDone chan struct{}
	// quiet is the reader's consecutive-timeout counter, owned by the reader
	// alone (no lock: only that goroutine reads or writes it).
	quiet int
}

// Open dials the v3 endpoint and performs the handshake for a LIVE session: the
// credential is proven and the audio geometry is accepted BEFORE the operator
// says a word, which is what makes a wrong credential a one-second fix instead
// of a failure discovered at the end of an utterance.
//
// THE REQUEST FRAME GOES OUT HERE, exactly as in Transcribe, because the v3
// service will not accept audio before the session's parameters. The response to
// it is NOT awaited synchronously (Transcribe's own reader would have to run
// first for that): the reader below starts immediately and every refusal the
// service sends — an application code, an error message — is folded into the
// result and reported by Finish's verdict, with the vendor log id.
func Open(ctx context.Context, opts Options) (*Session, error) {
	cred, err := ResolveCredentials(opts)
	if err != nil {
		return nil, err
	}
	reqID, err := newRequestID()
	if err != nil {
		return nil, &APIError{Op: opCredential, Message: "generate the request id", Err: err}
	}
	endpoint := endpointOrDefault(opts)
	resourceID := resourceIDOrDefault(opts)

	conn, err := doubaostream.WsDial(ctx, endpoint, doubaostream.WsDialOptions{
		Header: cred.UpgradeHeaders(resourceID, reqID),
	})
	if err != nil {
		return nil, &APIError{Op: opConnect, Message: err.Error(), ReqID: reqID, Err: err}
	}

	s := &Session{
		opts:       opts,
		conn:       conn,
		res:        &Result{ReqID: reqID, LogID: conn.LogID()},
		start:      time.Now(),
		readerDone: make(chan struct{}),
		nextSeq:    firstAudioSequence,
		onPartial:  opts.OnPartial,
	}
	doc := requestDoc{
		User:  userMeta{UID: uidOrDefault(opts.UID)},
		Audio: audioMeta{Format: audioFormatRaw, Codec: audioCodecRaw, Rate: audioSampleRate, Bits: audioBits, Channel: audioChannels},
		Request: requestMeta{
			ModelName:      modelName,
			EnableITN:      true,
			EnablePUNC:     true,
			ShowUtterances: true,
		},
	}
	body, err := json.Marshal(doc)
	if err != nil {
		s.releaseSocketNow(conn)
		return nil, &APIError{Op: opRequest, Message: "marshal the request document", LogID: s.res.LogID, ReqID: reqID, Err: err}
	}
	frame, err := fullClientRequestFrame(body)
	if err != nil {
		s.releaseSocketNow(conn)
		return nil, &APIError{Op: opRequest, Message: err.Error(), LogID: s.res.LogID, ReqID: reqID, Err: err}
	}
	if err := conn.WriteBinary(frame); err != nil {
		s.releaseSocketNow(conn)
		return nil, &APIError{Op: opRequest, Message: err.Error(), LogID: s.res.LogID, ReqID: reqID, Err: err}
	}
	go s.readLoop()
	return s, nil
}

// Say sends ONE packet of audio the caller just captured. It never waits for an
// answer: the audio is due now, and the reader is draining the answers in
// parallel.
//
// A REFUSAL HERE MEANS THE SESSION CAN NO LONGER CARRY AUDIO — it was finished,
// closed, or broken — and the two states have their own exported sentinels so a
// caller can tell a race from a failure with errors.Is (Rule 15).
func (s *Session) Say(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if len(pcm) > maxSayPacketBytes {
		return fmt.Errorf("say %d bytes: above the %d-byte packet bound (a live session carries capture-sized packets, not a file)",
			len(pcm), maxSayPacketBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.stateErrLocked(); err != nil {
		return err
	}
	frame, err := audioFrame(s.nextSeq, pcm)
	if err != nil {
		return fmt.Errorf("build audio frame: %w", err)
	}
	if err := s.conn.WriteBinary(frame); err != nil {
		s.failLocked(&APIError{Op: opAudio, Message: err.Error(), LogID: s.res.LogID, ReqID: s.res.ReqID, Err: err})
		return s.err
	}
	s.nextSeq++
	s.audioMS += pcmDurationMSV3(len(pcm))
	return nil
}

// Finish ends the utterance: the audio still in hand goes out as the LAST packet
// (the protocol's own "I am done speaking" — a NEGATED sequence), the reader is
// given the time it is owed to reach the service's silence, and the accumulated
// result is returned with its verdict.
//
// tail may be EMPTY, and an empty last packet is still sent: an operator who
// pressed the finish key between two packets owes the service the end-of-stream
// flag anyway, and a session that merely stopped sending would be left open
// until the service's own packet timeout — the operator watching a frozen line.
func (s *Session) Finish(tail []byte) (*Result, error) {
	s.mu.Lock()
	if err := s.stateErrLocked(); err != nil {
		s.mu.Unlock()
		return s.res, err
	}
	frame, err := audioFrame(-s.nextSeq, tail)
	if err != nil {
		s.mu.Unlock()
		return s.res, fmt.Errorf("build final audio frame: %w", err)
	}
	if err := s.conn.WriteBinary(frame); err != nil {
		s.failLocked(&APIError{Op: opAudio, Message: err.Error(), LogID: s.res.LogID, ReqID: s.res.ReqID, Err: err})
		s.mu.Unlock()
		return s.res, s.err
	}
	s.audioMS += pcmDurationMSV3(len(tail))
	s.endSent = true
	lastSeen := s.lastSeen
	s.mu.Unlock()

	// THE DRAIN DEADLINE IS ARMED ONLY NOW, and only if the reader is still
	// reading: before this point the audio-flow phase has NO deadline, and a
	// deadline set there would land mid-frame. If the reader has already returned
	// (the service sent its last package, or closed), arming a deadline on a
	// connection nobody reads is not a fact about anything.
	if !lastSeen {
		select {
		case <-s.readerDone:
		default:
			s.conn.SetReadDeadline(sessionDrainReadTimeout)
		}
	}

	select {
	case <-s.readerDone:
	case <-time.After(sessionFinishWait):
		s.markFinishTimedOut()
	}
	// THE SOCKET IS RELEASED HERE, after the reader stopped or the wait expired:
	// the close is what releases a reader still parked on a read the service is
	// not answering, and it is the ONE release both teardown paths share.
	s.releaseSocket()
	s.awaitReader()
	return s.finishResult()
}

// Close releases the session WITHOUT ending the utterance: no end-of-stream flag
// is sent, so the service simply stops being fed. It is the abort path (a
// capture that failed, a mode that died), deliberately distinct from Finish.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	err := s.releaseSocket()
	s.awaitReader()
	return err
}

// PartialCount reports how many partials have arrived so far — the session's own
// progress, published under the mutex the reader counts under.
func (s *Session) PartialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.partialsSeen
}

// FinishTimedOut reports whether Finish's bounded wait expired, i.e. whether the
// reader was still reading when the wait ran out. The transcript is then short by
// whatever the service had not finished saying — a REPORTED fact, never a silent
// truncation.
func (s *Session) FinishTimedOut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishTimedOut
}

// ── the read side ──────────────────────────────────────────────────────

// readLoop drains whatever the service has said, folding every frame into the
// shared result and calling the caller's OnPartial for each new text — which is
// what makes the transcript grow in the operator's line while they speak.
//
// IT ENDS ON ONE OF FOUR THINGS, each a different fact:
//   - the caller CLOSED the session (the read fails; closing is not a failure
//     and is not recorded as one);
//   - the service sent its LAST PACKAGE (the envelope's last-packet flag);
//   - the end has been sent and the service has gone QUIET (sessionQuietReads
//     consecutive timeouts — how an asynchronous recogniser says "that was all");
//   - a real error, recorded and reported by Finish.
//
// The deadline changes with the phase for the reason sessionFlowReadTimeout
// records; the quiet counter therefore starts counting only once the end has been
// sent, because before that being quiet is not a fact about anything.
func (s *Session) readLoop() {
	defer close(s.readerDone)
	for {
		if s.closeRequested() {
			return
		}
		budget := sessionDrainReadTimeout
		if !s.endWasSent() {
			budget = sessionFlowReadTimeout
		}
		s.conn.SetReadDeadline(budget)
		msg, err := s.conn.ReadMessage()
		if err != nil {
			if s.closeRequested() {
				return
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				// Silence. It ends the session only once the end has been sent —
				// see the loop's own note.
				if s.endWasSent() {
					s.quiet++
					if s.quiet >= sessionQuietReads {
						return
					}
				}
				s.mu.Lock()
				// A timeout that is not read as silence still means the reader is
				// parked at a FRAME BOUNDARY, which is the only place a deadline
				// may be armed: the next iteration arms it again.
				s.mu.Unlock()
				continue
			}
			var closeErr *doubaostream.WsCloseError
			if errors.As(err, &closeErr) && closeErr.Code == doubaostream.WsCloseCodeNormal {
				// A clean goodbye: the service has said everything it means to.
				return
			}
			s.recordErr(&APIError{Op: opRead, Message: err.Error(), LogID: s.res.LogID, ReqID: s.res.ReqID, Err: err})
			return
		}
		s.quiet = 0
		s.apply(msg)
		if s.lastReached() {
			return
		}
	}
}

// apply decodes one server message and folds it in: the code, the last-package
// flag, the text (REPLACED, never appended — the service sends the latest full
// text, so appending would repeat the transcript once per packet) and the
// caller's partial callback.
func (s *Session) apply(msg []byte) {
	code, last, text, errMsg, definite := parseResponseDoc(msg)
	s.mu.Lock()
	if code != 0 {
		s.res.Codes = append(s.res.Codes, code)
	}
	if last {
		s.res.last = true
		s.lastSeen = true
	}
	if errMsg != "" {
		s.res.errMsg = errMsg
	}
	if text != "" {
		if s.res.FirstTextAt == 0 {
			s.res.FirstTextAt = time.Since(s.start)
		}
		s.res.Text = text
		s.partialsSeen++
		index := s.partialsSeen
		s.mu.Unlock()
		if s.onPartial != nil {
			s.onPartial(Partial{Index: index, Text: text, Definite: definite, At: time.Since(s.start)})
		}
		return
	}
	s.mu.Unlock()
}

// lastReached reports whether the service marked a package as the last one.
func (s *Session) lastReached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}

// ── teardown ───────────────────────────────────────────────────────────

// closeRequested reports whether Close has been called. The flag is set BEFORE
// the socket is closed, so a read that fails because of the close request is
// never mistaken for a broken stream.
func (s *Session) closeRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// endWasSent reports whether Finish has sent the end-of-stream flag.
func (s *Session) endWasSent() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.endSent
}

// stateErrLocked is the ONE refusal for a session that cannot carry audio, so Say
// and Finish cannot disagree about what "finished" means.
func (s *Session) stateErrLocked() error {
	if s.err != nil {
		return s.err
	}
	if s.closed {
		return ErrSessionClosed
	}
	if s.endSent {
		return ErrSessionFinished
	}
	return nil
}

// failLocked records the first failure. Later ones are dropped: the first is the
// cause and the rest are its consequences.
func (s *Session) failLocked(err error) {
	if s.err == nil {
		s.err = err
	}
}

// recordErr records a failure the READER found.
func (s *Session) recordErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failLocked(err)
}

// markFinishTimedOut records that Finish's wait expired with the reader still
// reading (see FinishTimedOut).
func (s *Session) markFinishTimedOut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishTimedOut = true
}

// releaseSocket releases THE socket, once, whichever path reaches teardown first.
// Its answer is that release's outcome, so "already released" is reported as the
// success it is — which is the contract a caller with a deferred Close needs.
func (s *Session) releaseSocket() error {
	s.mu.Lock()
	if s.socketReleased {
		done := s.releaseDone
		s.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	s.socketReleased = true
	done := make(chan struct{})
	s.releaseDone = done
	conn := s.conn
	s.mu.Unlock()

	err := conn.Close()
	close(done)
	return err
}

// releaseSocketNow closes a socket the session never took ownership of: the
// construction failed after the handshake, so there is no reader and no second
// caller to coordinate with.
func (s *Session) releaseSocketNow(conn *doubaostream.WsConn) {
	_ = conn.Close() //stylegate:ignore the construction already failed; the close releases the socket and its error must not replace the returned failure
}

// awaitReader waits for the reader goroutine to return after the socket was
// closed under it. The wait is bounded so a reader that somehow ignored the close
// cannot hold the operator's key.
func (s *Session) awaitReader() {
	select {
	case <-s.readerDone:
	case <-time.After(sessionCloseWait):
	}
}

// finishResult turns the accumulated state into the caller's answer. The verdict
// itself is Result.finish — the SAME function the file arm reports through, so
// the two arms cannot describe an empty transcript differently (Rule 13/14).
func (s *Session) finishResult() (*Result, error) {
	s.mu.Lock()
	res := s.res
	res.Elapsed = time.Since(s.start)
	res.AudioMS = s.audioMS
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return res, err
	}
	return res, res.finish(res.ReqID)
}

// ErrSessionClosed / ErrSessionFinished are the two named states a session
// refuses audio in. They are EXPORTED sentinels so a caller can tell them apart
// from a transport failure with errors.Is — and they are exported because the
// caller that needs the distinction is outside this package: the chat live arm
// feeds audio from one goroutine while another may be finishing the utterance,
// and "this packet arrived after the end was sent" is a RACE IT MUST NOT REPORT
// AS A BROKEN STREAM (Rule 15).
var (
	ErrSessionClosed   = errors.New("the sauc v3 session is closed")
	ErrSessionFinished = errors.New("the sauc v3 session has already been finished")
)

// pcmDurationMSV3 is how many milliseconds of audio a byte count of 16 kHz mono
// PCM16 carries, derived from the protocol's own geometry constants rather than
// restated.
func pcmDurationMSV3(n int) int {
	return n / bytesPerSample16 * milliPerSecond / audioSampleRate
}
