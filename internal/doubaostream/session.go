// session.go — the LIVE half of the v2 streaming channel: audio goes in as it
// is SPOKEN, partials come out WHILE it is still being spoken.
//
// WHY THIS EXISTS BESIDE Transcribe. Transcribe answers "one audio file in, one
// transcript out" and it can, because a file has all of its audio up front: it
// paces the packets against the audio timeline and returns when the last one has
// been answered. An INTERACTIVE dictation has no file — the audio comes from a
// microphone at the speed the operator speaks, and the text must appear at that
// same speed. The two sessions differ in exactly one place, and it is the pacing:
//
//   - a FILE is sent against a CLOCK the client owns (Transcribe's
//     `time.Until(audioStart + n*chunkDur)`), because the client holds audio the
//     service has not "heard" yet and sending it early bills a stream longer than
//     the audio;
//   - a LIVE microphone IS the clock. Samples arrive in real time by
//     construction, so there is nothing to pace and no way to run ahead — each
//     packet is written the moment its audio was captured.
//
// So the live session has NO pacing loop: the caller's Say carries the audio, and
// the packets go out immediately. Everything else — the handshake, the credential
// headers, the request document, the frame construction, the response folding and
// the partial callback — is the SAME code Transcribe uses, reached through the
// same helpers, because a second spelling of the protocol is the drift Rule 13/14
// exist to stop.
//
// THE READER IS A GOROUTINE because the write side must never wait for the read
// side: the service answers a packet with a FULL response followed by a bare ACK,
// and the operator's next 200 ms of audio is due whether or not those arrived
// (measured — a client that waited for each answer per packet ran 3x real time and
// delivered its first partial at 3.65 s). The reader therefore drains whatever has
// arrived and the writer never blocks on it.
//
// IT IS NOT AN ALWAYS-ON MICROPHONE. The session lives for ONE utterance: the
// caller opens it, says what it has to say, calls Finish (which sends the
// end-of-stream flag) and closes. That is also what keeps the billing per audio
// second rather than per connection second.
package doubaostream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// sessionFlowReadTimeout is the audio-flow phase's read deadline: NONE.
	//
	// THE WHOLE FIX IS THIS ZERO. A read deadline can expire INSIDE a frame
	// (io.ReadFull takes part of a header, the rest is still in the socket), and
	// the framing state is not resumable — reading on parses the rest of that
	// frame as a header, which is how a live `/voice` dictation died with
	// "unknown WebSocket opcode 0x3" (measured). While the operator is still
	// speaking there is nothing to notice by timing out: the session ends
	// because Finish sent the end-of-stream flag, or because the capture ended,
	// or because the service closed or errored. The reader therefore parks on a
	// frame and stays parked, and the desync is IMPOSSIBLE by construction
	// rather than merely detected. The bound that matters is not lost with it:
	// Finish's wait (sessionFinishWait) is what limits the operator's key, and
	// the drain phase arms its own deadline (sessionDrainReadTimeout).
	sessionFlowReadTimeout = 0
	// sessionDrainReadTimeout is the deadline Finish arms for the drain: the
	// end-of-stream flag is out and the service is producing its final answer,
	// so a quiet service has to be distinguishable from a slow one. It is
	// defaultFinalReadTimeout, the same budget the file arm's drain uses, because
	// it is the same fact about the same service.
	sessionDrainReadTimeout = defaultFinalReadTimeout
	// sessionQuietFrames is how many consecutive read timeouts AFTER the last
	// packet end the session. It is drainQuietFrames — the same number, from the
	// same place, because it is the same fact about the same service (it answers
	// asynchronously, so the end of a transcript is SILENCE, not a terminator).
	sessionQuietFrames = drainQuietFrames
	// sessionFinishWait bounds how long Finish waits for the reader to reach that
	// silence. It is derived from the two numbers above plus one timeout of slack,
	// so a service that answers promptly is never cut off and a service that has
	// stopped talking cannot hold the operator's key for longer than the drain
	// it is owed (Rule 15: the wait is bounded, and its exhaustion is REPORTED).
	sessionFinishWait = sessionDrainReadTimeout*time.Duration(sessionQuietFrames+1) + sessionFlowReadTimeout
	// sessionCloseWait bounds how long Close waits for the reader goroutine to
	// return after the connection is closed under it. It is a teardown bound, not
	// a protocol timeout.
	sessionCloseWait = defaultCloseTimeout
)

// Session is ONE live v2 streaming session. It is created by Open, fed by Say,
// completed by Finish and released by Close.
//
// THE CALLING CONTRACT IS ONE GOROUTINE ON THE WRITE SIDE: Say and Finish are
// called by the goroutine that owns the capture, in order, and never
// concurrently. The READ side is this type's own goroutine, and the only thing
// the two share is the connection (whose read and write directions are
// independent — a bufio reader and a socket write do not touch the same state)
// and the result, which the reader owns until it has returned.
type Session struct {
	opts  Options
	conn  *wsConn
	res   *Result
	start time.Time
	logID string

	mu      sync.Mutex
	closed  bool
	endSent bool
	err     error
	// socketReleased reports that the socket has been RELEASED — by Finish's
	// drain or by Close, whichever arrived first. It is deliberately NOT the same
	// fact as `closed`: `closed` means "Close was CALLED" and it is what
	// isClosing() reads (so a read that fails because WE closed the connection is
	// never mistaken for a broken stream) and what stateErrLocked reports as
	// ErrSessionClosed. Finish's ordinary exit also releases the socket without
	// anyone having called Close, so the release needs a fact of its own — one the
	// two teardown paths share, so the socket is released EXACTLY once (task
	// 01a11dfe: Finish closed the socket, the deferred Close then closed the same
	// descriptor again and reported "use of closed network connection").
	socketReleased bool
	// releaseDone closes when the claimer's release has returned, so the path
	// that did NOT claim the release returns only once the socket is really gone
	// — instead of racing ahead into awaitReader on a connection still open. It
	// is created by the claimer under mu, so a nil value means the release has
	// already completed.
	releaseDone chan struct{}
	// finishTimedOut reports that Finish's bounded wait expired before the reader
	// reached the service's silence — see FinishTimedOut.
	finishTimedOut bool
	// partialsSeen counts the partials this session has delivered to the caller.
	// It is counted HERE (wrapping the caller's callback) rather than read off
	// Result.Partials, whose slice the reader appends to: a count published under
	// the session's own mutex is the whole of what another goroutine needs.
	partialsSeen int

	// readerDone closes when the reader goroutine has returned, so Finish can
	// WAIT for the end-of-stream answer instead of guessing that it arrived.
	readerDone chan struct{}
	// quiet is the reader's consecutive-timeout counter, owned by the reader
	// alone (no lock: only that goroutine reads or writes it).
	quiet int
	// audioMS is how much audio this session has sent. It is maintained by the
	// WRITE side (under mu) and copied into the result at Finish.
	audioMS int
}

// Open dials the streaming channel and performs the handshake: the credential is
// proven, the cluster and the audio geometry are accepted, and the request is
// registered — all BEFORE a second of audio is billed, which is the property that
// makes a wrong credential a one-second fix rather than a failure discovered at
// the end of an utterance.
//
// A refusal here is returned with the vendor's own log id, exactly as Transcribe
// reports it: the upgrade response carries that header even when the upgrade
// failed, and it is the identifier support asks for.
func Open(ctx context.Context, opts Options) (*Session, error) {
	if opts.AppID == "" || opts.Token == "" {
		return nil, errors.New("streaming asr needs an app id and an access token (see LoadCredentials)")
	}
	reqID := newReqID()
	// The credential travels as an UPGRADE header, with the semicolon separator
	// the vendor's own demo uses — see Transcribe's note on the same two lines,
	// which this shares rather than re-spells.
	d := wsDialer{
		timeout: defaultDialTimeout,
		header: http.Header{
			headerAuthorization:      []string{authorizationBearerPrefix + authorizationBearerSep + opts.Token},
			headerAPIRequestIDHeader: []string{reqID},
		},
		handshakeHeader: http.Header{},
	}

	conn, resp, err := wsDial(ctx, endpointOrDefault(opts.Endpoint), d)
	if err != nil {
		logID := ""
		if resp != nil {
			logID = resp.Header.Get(logIDHeader)
		}
		return nil, &APIError{Op: "connect", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	logID := resp.Header.Get(logIDHeader)
	conn.writeTimeout = defaultDialTimeout

	res := &Result{ReqID: reqID, LogID: logID}
	s := &Session{
		opts:       opts,
		conn:       conn,
		res:        res,
		start:      time.Now(),
		logID:      logID,
		readerDone: make(chan struct{}),
	}

	doc := requestDoc{
		App:     appMeta{AppID: opts.AppID, Token: opts.Token, Cluster: clusterOrDefault(opts.Cluster)},
		User:    userMeta{UID: uidOrDefault(opts.UID)},
		Audio:   audioMeta{Format: DefaultFormat, Rate: DefaultSampleRate, Bits: DefaultBits, Channel: DefaultChannel, Language: defaultLanguage},
		Request: requestMeta{ReqID: reqID, Sequence: 1, NBest: 1, Workflow: workflowOrDefault(opts.Workflow), ResultType: resultTypeFull, ShowUtterances: true},
	}
	frame, err := buildFullClientRequest(doc)
	if err != nil {
		_ = conn.Close() //stylegate:ignore the socket is handed back before the returned APIError; its close error must not replace the build failure
		return nil, &APIError{Op: "build-request", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	if err := conn.writeFrame(opcodeBinary, frame); err != nil {
		_ = conn.Close() //stylegate:ignore same hand-back on the send-request failure path — the write error is the returned one
		return nil, &APIError{Op: "send-request", Message: err.Error(), LogID: logID, ReqID: reqID, Err: err}
	}
	// THE REQUEST IS READ SYNCHRONOUSLY, before the reader starts: a refused
	// cluster answers 45000030 at THIS point (the batch arm learned the same
	// thing), and a session that had already started its reader would report that
	// refusal as a mid-stream failure of an utterance nobody has spoken yet.
	for i := 0; i < dialogReadsAfterRequest; i++ {
		if err := res.absorb(conn, s.start, packetReadBudget, s.onPartial, logID); err != nil {
			_ = conn.Close() //stylegate:ignore same hand-back when the request read fails; the absorb error travels to the caller
			return nil, err
		}
	}
	go s.readLoop()
	return s, nil
}

// Say sends ONE packet of the audio the operator just spoke. It never waits for
// an answer — the audio is due now, and the reader is draining the answers in
// parallel (see the file header).
//
// It reports an error only for a session that can no longer carry audio: one
// already finished, closed, or broken. A caller that ignores the error loses the
// packet, so the caller is expected to state it (the chat surface reports it and
// ends the utterance).
func (s *Session) Say(pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.stateErrLocked(); err != nil {
		return err
	}
	packet, err := buildAudioPacket(pcm, false)
	if err != nil {
		return fmt.Errorf("build audio packet: %w", err)
	}
	if err := s.conn.writeFrame(opcodeBinary, packet); err != nil {
		s.failLocked(&APIError{Op: "send-audio", Message: err.Error(), LogID: s.logID, ReqID: s.res.ReqID, Err: err})
		return s.err
	}
	s.audioMS += pcmDurationMS(len(pcm))
	return nil
}

// packetAssembler is this session's packet geometry: the SAME derivation the
// file arm's send loop uses, read from the options the session was opened with
// instead of being spelled out again here (Rule 13/14 — see packetAssembler's
// own note in client.go for the two copies this replaces).
func (s *Session) packetAssembler() PacketAssembler {
	return s.opts.packetAssembler()
}

// Finish ends the utterance: it sends the audio still in hand as the LAST packet
// (the header's end-of-stream flag is the protocol's own "I am done speaking"),
// waits for the service to finish the transcript, and returns what it produced.
//
// tail is the audio that was captured after the last Say, and it may be empty —
// an operator who pressed the finish key between two packets still owes the
// service the end-of-stream flag, so an empty last packet is sent rather than
// nothing (a session that merely stopped sending would be left open until the
// service's own packet timeout, which is the operator watching a frozen line).
//
// THE WAIT IS BOUNDED (sessionFinishWait) and its exhaustion is REPORTED through
// the returned result's Text being whatever had arrived: a caller that needs to
// know the difference asks FinishTimedOut.
func (s *Session) Finish(tail []byte) (*Result, error) {
	s.mu.Lock()
	if err := s.stateErrLocked(); err != nil {
		s.mu.Unlock()
		return s.res, err
	}
	// THE LAST PACKET COMES FROM THE ASSEMBLER (client.go's packetAssembler):
	// the end-of-stream packet is the same construction whichever arm sends it,
	// so "the utterance is over" is stated once rather than once per arm.
	packet, err := s.packetAssembler().FinalPacket(tail)
	if err != nil {
		s.mu.Unlock()
		return s.res, fmt.Errorf("build final audio packet: %w", err)
	}
	if err := s.conn.writeFrame(opcodeBinary, packet); err != nil {
		s.failLocked(&APIError{Op: "send-final", Message: err.Error(), LogID: s.logID, ReqID: s.res.ReqID, Err: err})
		s.mu.Unlock()
		return s.res, s.err
	}
	s.audioMS += pcmDurationMS(len(tail))
	s.endSent = true
	s.mu.Unlock()

	// THE DRAIN DEADLINE IS ARMED ONLY NOW, and only if the reader is still
	// reading. Before this point the audio-flow phase has NO deadline (the reader
	// is parked mid-frame if a frame is in flight — that is what makes the
	// mid-frame desync impossible), so the transition to the drain is
	// DELIBERATE: the end-of-stream flag is out, and from here "the service has
	// stopped talking" is the fact the session waits for. If the reader has
	// already returned (it read the explicit terminator, or the service closed),
	// arming a deadline on a connection nobody reads is not a fact about
	// anything — the wait below returns at once.
	select {
	case <-s.readerDone:
	default:
		s.conn.SetReadDeadline(sessionDrainReadTimeout)
	}

	select {
	case <-s.readerDone:
	case <-time.After(sessionFinishWait):
		s.markFinishTimedOut()
	}
	// THE CONNECTION IS CLOSED HERE, after the reader stopped or the wait
	// expired. The close is what releases a reader still parked on a read the
	// service is not answering. It is the ONE release the two teardown paths
	// share: a Close after this returns nil, because the socket it would close
	// was already released by this line (see releaseSocket).
	s.releaseSocket()
	s.awaitReader()
	return s.finishResult()
}

// Close releases the session WITHOUT ending the utterance: no end-of-stream flag
// is sent, so the service is told nothing it did not already know and simply
// stops being fed. It is the abort path (a capture that failed, a mode that died)
// and it is deliberately distinct from Finish, whose whole job is to send that
// flag.
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

// releaseSocket releases THE socket, once, whichever path reaches teardown first:
// Finish's drain and Close both call it, and the second caller waits for the first
// one's release to return rather than closing the same descriptor again. Its
// answer is that release's outcome — so "already released" is reported as the
// success it is, which is exactly the contract a caller with a deferred Close
// needs (Close after Finish returns nil; task 01a11dfe).
//
// The claim and the release report are made under the session's own mutex, and in
// that order: the claimer is the goroutine that sets socketReleased, so a second
// caller finds the flag set and reads a releaseDone that is non-nil — never a
// connection it then closes to no purpose.
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
	s.mu.Unlock()

	err := s.conn.Close()
	close(done)
	return err
}

// PartialCount reports how many partials have arrived so far. It is a read of the
// session's own progress for a caller that wants to say something about it (a
// status row), and it takes the same lock the reader publishes under.
func (s *Session) PartialCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.partialsSeen
}

// onPartial is the callback the reader hands to the folding code: it counts the
// partial and forwards it to the caller's own callback, in that order (the count
// is this session's bookkeeping and must not depend on what the caller does with
// the text).
func (s *Session) onPartial(p Partial) {
	s.mu.Lock()
	s.partialsSeen++
	s.mu.Unlock()
	if s.opts.OnPartial != nil {
		s.opts.OnPartial(p)
	}
}

// readLoop is the read side: it drains whatever the service has said, folding
// every frame into the shared result and calling Options.OnPartial for each new
// transcript — which is what makes the text grow in the operator's input line
// while they are still speaking.
//
// IT ENDS ON ONE OF FOUR THINGS, and each is a different fact:
//   - the caller CLOSED the session (the connection read fails; closing is not a
//     failure and is not recorded as one);
//   - the service sent an explicit terminator (a full response carrying the
//     negative sequence of the last packet);
//   - the end has been sent and the service has gone QUIET (sessionQuietFrames
//     consecutive timeouts), which is how an asynchronous recogniser says "that
//     was all";
//   - a real error (a peer close, a server-error frame, a broken read, or a
//     read interrupted inside a frame), which is recorded and reported by Finish.
//
// THE DEADLINE CHANGES WITH THE PHASE, and that is deliberate. During the audio
// flow there is NONE (sessionFlowReadTimeout): a deadline that expires inside a
// frame leaves the framing state half-consumed and unrecoverable, and the old
// per-frame budget during this phase is exactly how a live dictation died (see
// sessionFlowReadTimeout). The quiet counter therefore starts counting only once
// the end has been sent — before that, being quiet is not a fact about anything.
// The drain arms sessionDrainReadTimeout, because once the end-of-stream flag is
// out, "the service has stopped talking" is exactly what must be noticed.
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
		msg, err := s.res.readOne(s.conn, s.start, budget, s.onPartial, s.logID)
		if err != nil {
			if s.closeRequested() {
				return
			}
			s.recordErr(err)
			return
		}
		if msg == nil {
			// Silence: the end of the transcript, once the end has been sent.
			if s.endWasSent() {
				s.quiet++
				if s.quiet >= sessionQuietFrames {
					return
				}
			}
			continue
		}
		s.quiet = 0
		if msg.Kind == msgTypeServerFullResponse && msg.Response.Sequence < 0 {
			// The service echoes the last packet's negative sequence on its final
			// response: an explicit terminator, and the same one the file arm's
			// drain stops on.
			return
		}
	}
}

// closeRequested reports whether Close has been called: the flag is set BEFORE
// the connection is closed, so a read that fails because of the close request
// is never mistaken for a broken stream.
//
// Named for the value it answers (PROJECT.md Rule 20 / Google Go Style
// decisions#getters): the accessor is niladic, so an `is` prefix would say
// nothing the return type does not already say.
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

// failLocked records the first failure. Later failures are dropped: the first one
// is the cause, and the ones after it are its consequences (the reader's "use of
// closed connection" behind the write that already failed).
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
// reading. It is a REPORTED fact — the transcript may be short by whatever the
// service had not finished saying — never a silent truncation.
func (s *Session) markFinishTimedOut() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishTimedOut = true
}

// FinishTimedOut reports whether Finish's bounded wait expired.
func (s *Session) FinishTimedOut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finishTimedOut
}

// awaitReader waits for the reader goroutine to return after the connection was
// closed under it. The wait is bounded so a reader that somehow ignored the close
// cannot hold the operator's key.
func (s *Session) awaitReader() {
	select {
	case <-s.readerDone:
	case <-time.After(sessionCloseWait):
	}
}

// finishResult turns the accumulated result into the caller's answer: the elapsed
// time, the audio that was sent, and — this is the part that matters — the two
// veredicts that are NOT a transcript.
//
// THE VERDICTS ARE THE POINT (Rule 15), and they are the same two Transcribe
// reports: code 1013 is the service's CORRECT answer for audio with no speech in
// it (reported as NoSpeechError, never as an empty transcript passed off as a
// recognition result), and an empty transcript WITHOUT that code is a result this
// client cannot explain and says so. A caller that has a failure recorded reports
// that first: the audio was lost, which is the more actionable fact.
func (s *Session) finishResult() (*Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := s.res
	res.Elapsed = time.Since(s.start)
	res.AudioMS = s.audioMS
	if s.err != nil {
		return res, s.err
	}
	if res.Text == "" && res.NoSpeech {
		return res, &NoSpeechError{Message: res.noSpeechMessage, ReqID: res.ReqID, LogID: res.LogID}
	}
	if res.Text == "" {
		return res, &APIError{
			Op: "final", Code: res.lastCode,
			Message: "the session ended without a transcript and without a code saying why",
			LogID:   res.LogID, ReqID: res.ReqID,
		}
	}
	return res, nil
}

// ErrSessionClosed / ErrSessionFinished are the two named states a session
// refuses audio in. They are EXPORTED sentinels so a caller can tell them apart
// from a transport failure with errors.Is — and they are exported because the
// caller that needs the distinction is outside this package: the chat live arm
// feeds audio from one goroutine while another may be finishing the utterance,
// and "this packet arrived after the end was sent" is a RACE IT MUST NOT REPORT
// AS A BROKEN STREAM (Rule 15: the refusal is named, not mistaken for a fault).
var (
	ErrSessionClosed   = errors.New("the streaming session is closed")
	ErrSessionFinished = errors.New("the streaming session has already been finished")
)

// pcmDurationMS is how many milliseconds of audio a byte count of 16 kHz mono
// PCM16 carries. It is derived from the protocol's own geometry constants rather
// than restated, so a change to the sample rate cannot leave the reported audio
// duration describing a different one.
func pcmDurationMS(n int) int {
	return n / bytesPerSample16 * 1000 / DefaultSampleRate
}
