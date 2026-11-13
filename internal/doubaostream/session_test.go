package doubaostream

// session_test.go — the LIVE session (Open / Say / Finish / Close) driven
// against a REAL loopback WebSocket server that speaks the vendor's own frame
// shapes.
//
// WHY A FAKE SERVER AND NOT A UNIT-TESTED SEAM. The properties this file is
// accepted on are SCHEDULE properties — "a partial reaches OnPartial while the
// audio is still being sent", "Finish's wait is bounded and its exhaustion is
// reported", "teardown releases the reader" — and none of them can be observed
// by inspecting arguments: they are facts about what happened on the wire over
// time, and about which goroutine observed what. So the server below is a real
// `net/http` handler upgraded by hand with the package's own header constants,
// and the assertions read the FRAMES it received (their count, their cadence,
// their header nibble) rather than the caller's intent.
//
// PROVENANCE (Rule 5: this is a PORT, not a new test). The shapes below are
// ported from the eight real-loopback assertions measured green on the
// parallel branch `task/01a11b5d48887a4a9b13ff18a2c83673`, file
// `internal/doubaostream/session_test.go` (commit `96a2f812f`), which targeted
// THAT branch's Session API (`Push`/`End`/`Wait`/`SessionOptions`). The type
// landed on master (`823305a17`) is a DIFFERENT design — `Options` with the
// batch arm's fields, `Say`/`Finish`/`Close`, and deliberately NO client-side
// pacing loop (see session.go's header: "a LIVE microphone IS the clock") — so
// the PORTED assertions are rewritten against the real API and the ones that
// were about that branch's self-pacing sender are replaced by the shape THIS
// design actually has (documented per test). The fake server, its
// header-constant derivation and `readClientFrame` are kept whole.
//
// The server is deliberately generous about what it accepts and strict about
// what it records: it answers the full client request with the code-1000
// response the real service sends, answers every audio packet with a growing
// partial, and — on the packet whose header carries the end-of-stream flag —
// answers with the negative sequence that terminates the stream.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClientFullRequest / fakeClientAudio are the message-type nibbles the
// server half must recognise, DERIVED from the production header templates (so
// a change to the protocol's own bytes cannot leave this server reading a
// vocabulary nothing sends). fakeEndFlag is the end-of-stream flag, read the
// same way.
var (
	fakeClientFullRequest = headerFullClientRequest[1] >> msgTypeShift
	fakeClientAudio       = headerAudioOnly[1] >> msgTypeShift
	fakeEndFlag           = headerLastAudio[1] & 0x0f
)

// asrFakeServer is one scripted stand-in for the v2 channel.
type asrFakeServer struct {
	// ln is the loopback listener; the endpoint handed to Open is ws:// + its
	// address (wsDial accepts the plain scheme, which is what makes this test
	// need no certificate).
	ln net.Listener

	// mu guards everything below: the handler goroutine writes, the test reads.
	mu sync.Mutex
	// packets is every AUDIO packet received, in order, with the header byte
	// that says whether it was flagged last and the payload length.
	packets []fakePacket
	// requests is how many full client requests arrived (must be exactly 1).
	requests int
	// partialsIn is how many partial responses the server has sent.
	partialsIn int
	// lastText is the text of the most recent response, so the FINAL response
	// can close the transcript with one more clause (see the terminator in
	// dialog).
	lastText string
	// closed records that the server side stopped having a connection: the
	// dialog returned, or its read failed.
	closed bool
	// speech is the application code the server answers a stream with.
	// codeSuccess is the ordinary recogniser; CodeNoSpeech is the service's
	// verdict on audio that holds no speech.
	speech int
	// serve the dialog, overridable so a case can drive another script (a
	// stream whose responses carry no text at all).
	serve func(s *asrFakeServer, br *bufio.Reader, bw *bufio.Writer)
}

// fakePacket is one audio packet as the server saw it.
type fakePacket struct {
	header byte
	bytes  int
	// payload is the packet's body, gunzipped back into the PCM the client
	// sent. It is kept because the frame's own length is a COMPRESSED length:
	// two packets of different audio can carry the same byte count (all-zero
	// PCM compresses to nearly the same few bytes whatever its size), so a
	// byte-length comparison is not a comparison of audio.
	payload []byte
	// at is when the packet arrived, measured from the FIRST packet's arrival —
	// the observation the cadence assertion is made of.
	at time.Duration
}

func newASRFakeServer(t *testing.T) *asrFakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &asrFakeServer{ln: ln, speech: codeSuccess}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/asr", srv.handle)
	hs := &http.Server{Handler: mux}
	go func() { _ = hs.Serve(ln) }() //stylegate:ignore the fixture's fake HTTP server; a serve error means the test already finished with the listener closed
	t.Cleanup(func() {
		_ = hs.Close() //stylegate:ignore closes that fake server once the fixture's exchange is over
		_ = ln.Close() //stylegate:ignore closes its listener, the last act of the fixture's teardown
	})
	return srv
}

// endpoint is the ws:// URL Open should be pointed at.
func (s *asrFakeServer) endpoint() string {
	return "ws://" + s.ln.Addr().String() + "/api/v2/asr"
}

// fakeServerCloseGrace bounds how long an assertion waits for the SERVER side
// of the connection to observe the client's close.
//
// WHY A WAIT IS REQUIRED, and not a defect being papered over: the handler runs
// on its own goroutine (net/http's accept loop spawns it), and it records the
// close only once its OWN read of the socket fails. So "the server saw the
// connection close" is a fact that becomes TRUE, not a fact that is already
// true by the time Close returns — an assertion that reads the snapshot on the
// next line is asserting the scheduler's timing, which is exactly what made
// this case fail under load (measured 2026-10-09 on the parallel branch: 3 of
// 5 runs red, while the same case passed 5 of 5 on an idle machine).
const fakeServerCloseGrace = 5 * time.Second

// fakeServerClosePollInterval is the re-read cadence of awaitServerClose: short
// enough to be invisible next to the grace, long enough not to spin a core.
const fakeServerClosePollInterval = 2 * time.Millisecond

// awaitServerClose waits for the server to have seen the connection close, or
// reports what it saw when the grace ran out.
func (s *asrFakeServer) awaitServerClose(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(fakeServerCloseGrace)
	for {
		if _, _, _, closed := s.snapshot(); closed {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("the server never saw the connection close within %s", fakeServerCloseGrace)
			return
		}
		time.Sleep(fakeServerClosePollInterval) //stylegate:allow-sleep bounded re-read cadence of a grace poll; the subject is which goroutine wrote the state, not the sleep
	}
}

// snapshot returns the recorded state under the lock.
func (s *asrFakeServer) snapshot() (packets []fakePacket, requests, partials int, closed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fakePacket, len(s.packets))
	copy(out, s.packets)
	return out, s.requests, s.partialsIn, s.closed
}

// handle performs the upgrade by hand (the package's own wsDialer is the
// CLIENT half; the server half here is the RFC's echo of the key, which is all
// the client verifies) and then runs the dialog.
//
// THE HANDLER OWNS THE CONNECTION, so its return IS the moment the server side
// stopped having one — recorded unconditionally, because a dialog that ended on
// the client's own terminator (see dialog's negative sequence) leaves this
// function by RETURNING, not by failing a read. Without the deferred record,
// "did the server side see the connection go?" would answer false for every
// session that finished correctly, which is the instrument lying rather than
// the client being wrong.
func (s *asrFakeServer) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(headerSecWebSocketKey)
	if key == "" {
		http.Error(w, "no key", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer func() {
		_ = conn.Close() //stylegate:ignore closes the fake service connection after the refused-cluster response was asserted
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		headerUpgradeName + ": " + headerUpgradeValue + "\r\n" +
		headerConnectionName + ": " + headerConnectionValue + "\r\n" +
		headerSecWebSocketAccept + ": " + wsAccept(key) + "\r\n" +
		logIDHeader + ": fake-logid\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	if s.serve != nil {
		s.serve(s, rw.Reader, rw.Writer)
		return
	}
	s.dialog(rw.Reader, rw.Writer)
}

// dialog is the ordinary script: one full client request, then audio packets
// until the end-of-stream flag, then the final response.
func (s *asrFakeServer) dialog(br *bufio.Reader, bw *bufio.Writer) {
	s.serveScript(br, bw, s.speech)
}

// serveScript is dialog with an explicit application code, so a case can make
// the service answer the recogniser's own "no speech in this audio" (1013).
func (s *asrFakeServer) serveScript(br *bufio.Reader, bw *bufio.Writer, speechCode int) {
	var start time.Time
	for {
		payload, opcode, err := readClientFrame(br)
		if err != nil {
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
			return
		}
		if opcode != opcodeBinary || len(payload) < headerBytes {
			if opcode == opcodeClose {
				// THE VENDOR'S TERMINATOR IS THE GOODBYE FRAME: the stream ends
				// with a NEGATIVE-sequence response, and the client sends its
				// Close frame only once it has finished reading. So the final
				// response is sent HERE, on the goodbye — which is what makes
				// the client's own explicit-terminator path (session.go's
				// readLoop) the path this suite actually exercises, and what
				// stops every case from paying the quiet-timer's full bound.
				s.mu.Lock()
				text := s.lastText + "。"
				s.mu.Unlock()
				_ = s.writeResponse(bw, -1, text, speechCode) //stylegate:ignore writes the canned refusal the case feeds the client; the parse result below is the assertion
				return
			}
			continue
		}
		kind := payload[1] >> msgTypeShift
		switch kind {
		case fakeClientFullRequest:
			s.mu.Lock()
			s.requests++
			s.mu.Unlock()
			if err := s.writeResponse(bw, 0, "你好", speechCode); err != nil {
				return
			}
		case fakeClientAudio:
			last := payload[1]&0x0f == fakeEndFlag
			at := time.Duration(0)
			if start.IsZero() {
				start = time.Now()
			} else {
				at = time.Since(start)
			}
			// THE PAYLOAD IS GUNZIPPED BACK INTO PCM: the frame's length is the
			// COMPRESSED length, so it is not a reading of the audio.
			pcm, err := gunzipBytes(payload[headerBytes+sizeFieldBytes:])
			if err != nil {
				return
			}
			s.mu.Lock()
			s.packets = append(s.packets, fakePacket{header: payload[1], bytes: len(payload), payload: pcm, at: at})
			s.partialsIn++
			n := s.partialsIn
			s.mu.Unlock()
			// EVERY packet is answered with a partial, which is exactly what
			// the measured v2 channel does — and it is what makes "OnPartial
			// fires while the audio is still being sent" observable here. A
			// stream the service has judged speechless gets the code-1013
			// answer with NO text instead.
			text := strings.Repeat("字", n)
			if speechCode == CodeNoSpeech {
				text = ""
			}
			if err := s.writeResponse(bw, 0, text, speechCode); err != nil {
				return
			}
			// The end-of-stream packet gets an ACKNOWLEDGING response and
			// nothing more: the terminator is owed on the goodbye frame above,
			// exactly as the real service times it.
			_ = last
		}
	}
}

// gunzipBytes reverses the protocol's own body compression, so the server half
// can read the PCM a packet really carried rather than the size of its
// compressed form.
func gunzipBytes(body []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// writeResponse renders one FULL server response frame.
func (s *asrFakeServer) writeResponse(bw *bufio.Writer, seq int, text string, code int) error {
	if code == 0 {
		code = codeSuccess
	}
	doc := serverResponse{Code: code, Message: "", Sequence: seq, ReqID: "fake-reqid", LogID: "fake-logid"}
	if code == CodeNoSpeech {
		doc.Message = "no valid speech"
	}
	doc.Result = []resultNode{{Text: text}}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.lastText = text
	s.mu.Unlock()
	body, err := gzipCompress(raw)
	if err != nil {
		return err
	}
	frame := assembleHeader(fakeResponseHeader, uint32(len(body)), body)
	if err := writeServerFrame(bw, frame); err != nil {
		return err
	}
	return bw.Flush()
}

// fakeResponseHeader is the full-server-response template's own bytes, built
// through the production constant so this server cannot drift from the client's
// parser.
var fakeResponseHeader = [headerBytes]byte{headerFullClientRequest[0], msgTypeServerFullResponse << msgTypeShift, headerFullClientRequest[2], headerFullClientRequest[3]}

// writeServerFrame writes one unmasked server-to-client WebSocket frame.
func writeServerFrame(bw *bufio.Writer, payload []byte) error {
	hdr := []byte{0x80 | opcodeBinary}
	switch {
	case len(payload) < len16Marker:
		hdr = append(hdr, byte(len(payload)))
	case len(payload) <= 0xffff:
		hdr = append(hdr, len16Marker, byte(len(payload)>>8), byte(len(payload)))
	default:
		hdr = append(hdr, len64Marker)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		hdr = append(hdr, ext[:]...)
	}
	if _, err := bw.Write(hdr); err != nil {
		return err
	}
	_, err := bw.Write(payload)
	return err
}

// readClientFrame reads one MASKED client frame (the client's own framing, read
// back by the server half).
func readClientFrame(br *bufio.Reader) ([]byte, byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return nil, 0, err
	}
	opcode := hdr[0] & 0x0f
	masked := hdr[1]&maskBit != 0
	length := uint64(hdr[1] & 0x7f)
	switch length {
	case len16Marker:
		var ext [2]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, 0, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case len64Marker:
		var ext [8]byte
		if _, err := io.ReadFull(br, ext[:]); err != nil {
			return nil, 0, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	var key [wsMaskKeyBytes]byte
	if masked {
		if _, err := io.ReadFull(br, key[:]); err != nil {
			return nil, 0, err
		}
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(br, payload); err != nil {
			return nil, 0, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%wsMaskKeyBytes]
		}
	}
	return payload, opcode, nil
}

// ── the tests ────────────────────────────────────────────────────────────

// sessionOptions is the credential + endpoint every case hands Open, so no case
// can accidentally drive a session with the real default endpoint.
func sessionOptions(srv *asrFakeServer) Options {
	return Options{AppID: "app", Token: "tok", Endpoint: srv.endpoint(), ChunkMillis: 40}
}

// TestSessionSendsExactlyWhatTheCallerSaid is the core schedule property of
// THIS design, and it is the deliberate OPPOSITE of the branch this file ports
// from: master's live session has NO client-side pacing loop. session.go's own
// header says why — "a LIVE microphone IS the clock. Samples arrive in real
// time by construction, so there is nothing to pace and no way to run ahead —
// each packet is written the moment its audio was captured".
//
// So the assertion is not "packets go out when the caller pushes nothing" (that
// was the source branch's self-pacing sender, and it would be FALSE here): it is
// that "Say writes now" — a session that sat on the caller's audio waiting for a
// tick of its own would fail this, and so would one that silently swallowed a
// packet. The counterweight is the shape of the LAST packet, which is the
// protocol's end-of-stream flag and is what makes the service close the stream
// rather than time out waiting for it.
func TestSessionSendsExactlyWhatTheCallerSaid(t *testing.T) {
	srv := newASRFakeServer(t)
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const pushes = 5
	for i := 0; i < pushes; i++ {
		if err := sess.Say(make([]byte, 1280)); err != nil {
			t.Fatalf("Say %d: %v", i, err)
		}
	}
	res, err := sess.Finish(nil)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	defer sess.Close()

	packets, requests, _, _ := srv.snapshot()
	if requests != 1 {
		t.Errorf("full client requests = %d, want exactly 1", requests)
	}
	// `pushes` says + the end-of-stream packet Finish owes, and not one more:
	// the session must not manufacture audio of its own.
	if len(packets) != pushes+1 {
		t.Fatalf("audio packets = %d, want %d (%d Say + the end-of-stream packet) — a live session sends what it was handed, and Finish always owes the flag",
			len(packets), pushes+1, pushes)
	}
	last := packets[len(packets)-1]
	if last.header&0x0f != fakeEndFlag {
		t.Errorf("the last packet is not flagged end-of-stream (header %02x, want nibble %#02x)", last.header, fakeEndFlag)
	}
	for i, p := range packets[:len(packets)-1] {
		if p.header&0x0f == fakeEndFlag {
			t.Errorf("packet %d was flagged end-of-stream but %d packets followed it", i+1, len(packets)-i-1)
		}
	}
	// AudioMS is the session's own reading of what it sent: 6 packets × 1280
	// bytes of 16 kHz mono PCM16 = 40 ms each.
	if want := pushes * pcmDurationMS(1280); res.AudioMS != want {
		t.Errorf("Result.AudioMS = %d, want %d — the reported audio must describe what was actually sent", res.AudioMS, want)
	}
}

// TestSessionPushesPartialWhileAudioIsStillGoing is the acceptance property in
// its smallest form: OnPartial fires BEFORE Finish is called, i.e. text exists
// while the speaker is still speaking. That is the whole reason the live arm
// exists, and it is observable only here — a unit seam could assert that the
// callback was installed, never that it fired early.
//
// The reader is a goroutine precisely so this can be true while the write side
// is still running (session.go: "the write side must never wait for the read
// side").
func TestSessionPushesPartialWhileAudioIsStillGoing(t *testing.T) {
	srv := newASRFakeServer(t)
	type arrival struct {
		index int
		text  string
		at    time.Duration
	}
	var mu sync.Mutex
	var got []arrival
	began := time.Now()
	opts := sessionOptions(srv)
	opts.OnPartial = func(p Partial) {
		mu.Lock()
		got = append(got, arrival{index: p.Index, text: p.Text, at: time.Since(began)})
		mu.Unlock()
	}
	sess, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// THE PARTIALS MUST ARRIVE WHILE THIS LOOP IS STILL RUNNING — the loop IS
	// the "still speaking" state, and the assertion below reads how many
	// partials had arrived before it ended. Each Say is answered by the server
	// before the next one is written, so the count is deterministic, not a
	// race against a clock.
	const pushes = 5
	for i := 0; i < pushes; i++ {
		if err := sess.Say(make([]byte, 1280)); err != nil {
			t.Fatalf("Say %d: %v", i, err)
		}
		// The SERVER's own receipt is the clock this waits on. It is not a
		// sleep used to give the reader "time to catch up": the i-th partial
		// CANNOT have arrived before the i-th packet did, and the server writes
		// that response before it reads the next frame, so waiting here makes
		// the observation deterministic instead of racy.
		srv.awaitAudioPackets(t, i+1)
	}
	// THE HANDSHAKE'S OWN PARTIAL IS THE CLIENT'S FACT, so the wait for it is on
	// the CLIENT'S OWN counter. The server's receipt of packet i+1 says nothing
	// about whether the reader has folded the response to packet i — those are
	// two different goroutines and two different facts, and reading the second on
	// the first one's clock is asserting the scheduler's timing (measured
	// 2026-10-09: red on 4 of 5 runs with the committed product files, and 2 of 3
	// with the read-deadline fix in place — it is independent of that change).
	// awaitPartials returns AT ONCE when the partial has arrived, so the loop
	// above stays the "still speaking" state the assertion is about.
	sess.awaitPartials(t, pushes+1)
	mu.Lock()
	beforeFinish := len(got)
	firstAt := time.Duration(0)
	if beforeFinish > 0 {
		firstAt = got[0].at
	}
	mu.Unlock()
	// pushes + 1: the FULL CLIENT REQUEST is answered with a response too (the
	// measured v2 channel answers the handshake with the first transcript), so
	// by the time the caller has said five packets, six partials are owed.
	if want := pushes + 1; beforeFinish != want {
		t.Fatalf("OnPartial fired %d times while the caller was still sending audio, want %d (%d packets + the handshake's own response) — the callback is not live", beforeFinish, want, pushes)
	}
	if firstAt > 2*time.Second {
		t.Errorf("the first partial arrived %s after Open — too late to be text 'while you speak'", firstAt)
	}
	if cnt := sess.PartialCount(); cnt != beforeFinish {
		t.Errorf("PartialCount() = %d while %d partials had arrived — the session's own progress reading disagrees with the callback", cnt, beforeFinish)
	}

	res, err := sess.Finish(nil)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	defer sess.Close()
	mu.Lock()
	total := len(got)
	mu.Unlock()
	// pushes + 2: the FULL CLIENT REQUEST is answered with a response too (the
	// measured v2 channel answers the handshake with the first transcript) and
	// the terminator the goodbye frame draws closes the transcript with one
	// more clause, so a five-packet utterance owes seven partials.
	if want := pushes + 2; total != want {
		t.Errorf("partials total %d, want %d — the final response's text must arrive too", total, want)
	}
	if res.Text == "" {
		t.Errorf("the final transcript is empty although the server answered every packet")
	}
	if len(res.Partials) != total {
		t.Errorf("Result.Partials = %d, OnPartial saw %d — the two readings of one stream disagree", len(res.Partials), total)
	}
	if res.FirstPartialAt <= 0 {
		t.Errorf("Result.FirstPartialAt = %s, want the first-token latency of a stream that did produce partials", res.FirstPartialAt)
	}
	// The indexes are the arrival order the caller is promised.
	for i, a := range got {
		if a.index != i+1 {
			t.Errorf("partial %d carries Index %d, want %d", i, a.index, i+1)
		}
	}
}

// awaitAudioPackets waits until the server has RECEIVED n audio packets.
//
// It waits on the server's own counter rather than on a sleep: the test's
// subject is which goroutine observed what, and a fixed sleep would either be
// slower than needed or flaky under load (Rule 27: no load is manufactured
// here). The cadence of these cases is CALLER-DRIVEN — the live session has no
// pacing loop — so waiting on the server's receipt is waiting on the thing
// under test, not on a clock.
func (s *asrFakeServer) awaitAudioPackets(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(fakeServerCloseGrace)
	for {
		if packets, _, _, _ := s.snapshot(); len(packets) >= n {
			return
		}
		if time.Now().After(deadline) {
			packets, _, _, _ := s.snapshot()
			t.Fatalf("the server received %d audio packets within %s, want %d", len(packets), fakeServerCloseGrace, n)
		}
		time.Sleep(fakeServerClosePollInterval) //stylegate:allow-sleep bounded re-read cadence of a grace poll (see awaitServerClose)
	}
}

// awaitPartials waits until the session's own partial count has reached n, or
// reports what it saw when the grace ran out.
//
// IT WAITS ON THE SESSION'S OWN READING (PartialCount), never on the server's
// receipt of a packet: which partials the reader has folded is a fact about the
// reader's goroutine, and asserting it from the server's counter is asserting
// the scheduler's timing. The wait is bounded by the same grace the close wait
// uses, and it RETURNS IMMEDIATELY when the partial has already arrived — the
// caller's loop is the "still speaking" state, so a satisfied wait costs
// nothing and a failing one is loud.
func (s *Session) awaitPartials(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(fakeServerCloseGrace)
	for {
		if s.PartialCount() >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the session had folded %d partials within %s, want %d — the callback is not live",
				s.PartialCount(), fakeServerCloseGrace, n)
		}
		time.Sleep(fakeServerClosePollInterval) //stylegate:allow-sleep bounded re-read cadence of a grace poll (see awaitServerClose)
	}
}

// TestFinishFlushesTheTailAndFlagsIt: the audio captured after the last Say
// reaches the wire, and the packet carrying it is the flagged one. The tail is
// not padded into a wall of silence — the protocol's own end-of-stream nibble
// is what tells the service the utterance is over, and a session that merely
// stopped sending would be left open until the service's packet timeout (the
// operator watching a frozen line).
func TestFinishFlushesTheTailAndFlagsIt(t *testing.T) {
	srv := newASRFakeServer(t)
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	const tail = 640 // 20 ms of audio: shorter than one 40 ms chunk
	if err := sess.Say(make([]byte, tail)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	res, err := sess.Finish(make([]byte, tail))
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	defer sess.Close()
	packets, _, _, _ := srv.snapshot()
	if len(packets) != 2 {
		t.Fatalf("audio packets = %d, want 2 (the Say and the Finish that carries its own tail)", len(packets))
	}
	last := packets[len(packets)-1]
	if last.header&0x0f != fakeEndFlag {
		t.Errorf("the packet carrying Finish's tail is not flagged end-of-stream (header %02x)", last.header)
	}
	// THE PCM IS COMPARED, NOT THE FRAME LENGTH: a frame's length is a
	// COMPRESSED length, so two packets of different audio can be the same few
	// bytes long. What this asserts is that Finish sent the tail AS IT WAS —
	// not zero-padded into a full chunk, which is what a session that treated
	// the tail as "one more packet to fill" would put on the wire.
	if got := len(last.payload); got != tail {
		t.Errorf("the packet carrying Finish's tail holds %d bytes of audio, want the %d bytes handed to Finish — the tail was padded instead of sent as it was", got, tail)
	}
	if got := len(packets[0].payload); got != tail {
		t.Errorf("the first packet holds %d bytes of audio, want the %d bytes handed to Say", got, tail)
	}
	if want := 2 * pcmDurationMS(tail); res.AudioMS != want {
		t.Errorf("Result.AudioMS = %d, want %d (both the Say and Finish's tail are audio that was sent)", res.AudioMS, want)
	}
}

// TestFinishReportsTheServicesOwnVerdictOnSpeechlessAudio ports the source
// branch's empty-transcript case onto the API master actually has. The branch
// called a free function (`emptyTranscriptVerdict`); this design renders the
// verdict from INSIDE Finish (`finishResult`), whose comment states the rule:
// "code 1013 is the service's CORRECT answer for audio with no speech in it
// (reported as NoSpeechError, never as an empty transcript passed off as a
// recognition result), and an empty transcript WITHOUT that code is a result
// this client cannot explain and says so."
//
// THE SERVICE'S OWN VERDICT: when it answers 1013, Finish returns the
// NoSpeechError — and it does so through a real stream, because the fact being
// tested is that a WHOLE session (Open + Say + Finish) surfaces it that way.
func TestFinishReportsTheServicesOwnVerdictOnSpeechlessAudio(t *testing.T) {
	srv := newASRFakeServer(t)
	srv.speech = CodeNoSpeech
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Say(make([]byte, 1280)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	res, err := sess.Finish(nil)
	defer sess.Close()
	var noSpeech *NoSpeechError
	if !errors.As(err, &noSpeech) {
		t.Fatalf("a stream the service answered %d gave %v, want a NoSpeechError", CodeNoSpeech, err)
	}
	if res == nil || !res.NoSpeech {
		t.Fatalf("the result does not carry NoSpeech — a caller cannot tell the service's verdict from a failed run")
	}
	if res.Text != "" {
		t.Errorf("Result.Text = %q, want empty: a speechless stream has no transcript", res.Text)
	}
}

// TestFinishRefusesAnEmptyTranscriptWithNoCode is the other half of the same
// rule, and it is the half a "did it return an error?" test cannot see: when
// the transcript is empty and the service gave NO code saying why, that is a
// result this client cannot explain — and it says so, in those words, rather
// than handing the caller an empty string that looks like a successful run of
// silent audio.
func TestFinishRefusesAnEmptyTranscriptWithNoCode(t *testing.T) {
	srv := newASRFakeServer(t)
	// A stream whose responses carry no text and the ordinary success code: the
	// service answered, and did not say why there is nothing to show.
	srv.serve = func(s *asrFakeServer, br *bufio.Reader, bw *bufio.Writer) {
		s.serveEmptyDialog(br, bw)
	}

	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Say(make([]byte, 1280)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	res, err := sess.Finish(nil)
	defer sess.Close()
	if err == nil {
		t.Fatalf("Finish returned no error for a stream with no transcript and no code — an unexplained empty transcript must be reported")
	}
	var noSpeech *NoSpeechError
	if errors.As(err, &noSpeech) {
		t.Errorf("an empty transcript with code %d was reported as no-speech", codeSuccess)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("the failure is %T (%v), want an *APIError", err, err)
	}
	if !strings.Contains(err.Error(), "without a code saying why") {
		t.Errorf("the failure text %q does not say why the stream was empty", err.Error())
	}
	if res == nil {
		t.Fatalf("Finish returned no result beside the error — the caller loses the ids the vendor's support asks for")
	}
}

// serveEmptyDialog answers every packet with a code-1000 response that carries
// NO text — the shape this case needs: the service answered, and said nothing
// about why there is nothing to show.
func (s *asrFakeServer) serveEmptyDialog(br *bufio.Reader, bw *bufio.Writer) {
	for {
		payload, opcode, err := readClientFrame(br)
		if err != nil {
			s.mu.Lock()
			s.closed = true
			s.mu.Unlock()
			return
		}
		if opcode != opcodeBinary || len(payload) < headerBytes {
			if opcode == opcodeClose {
				// Same terminator timing as dialog: the final response is drawn
				// by the client's goodbye frame.
				_ = s.writeResponse(bw, -1, "", codeSuccess) //stylegate:ignore writes the canned success frame of the second case, consumed by the decoder under test
				return
			}
			continue
		}
		kind := payload[1] >> msgTypeShift
		switch kind {
		case fakeClientFullRequest:
			s.mu.Lock()
			s.requests++
			s.mu.Unlock()
			if err := s.writeResponse(bw, 0, "", codeSuccess); err != nil {
				return
			}
		case fakeClientAudio:
			s.mu.Lock()
			s.packets = append(s.packets, fakePacket{header: payload[1], bytes: len(payload)})
			s.partialsIn++
			s.mu.Unlock()
			if err := s.writeResponse(bw, 0, "", codeSuccess); err != nil {
				return
			}
		}
	}
}

// TestSessionRefusesAudioAfterFinishAndAfterClose: the two named states, one
// refusal each, and never a silent drop. This is the design's own guarantee
// (ErrSessionClosed / ErrSessionFinished are exported precisely because the
// chat arm feeds audio from one goroutine while another may end the utterance),
// and it is asserted here against the REAL session rather than against the
// sentinels themselves.
func TestSessionRefusesAudioAfterFinishAndAfterClose(t *testing.T) {
	srv := newASRFakeServer(t)
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := sess.Finish(nil); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// FINISH FIRST: the end-of-stream flag has gone out, so more audio is not a
	// stale packet, it is audio the service will never hear.
	if err := sess.Say(make([]byte, 64)); !errors.Is(err, ErrSessionFinished) {
		t.Errorf("Say after Finish gave %v, want ErrSessionFinished", err)
	}
	// The refusal is on Finish too, through the same one predicate
	// (stateErrLocked), so the two cannot disagree about what "finished" means.
	if _, err := sess.Finish(nil); !errors.Is(err, ErrSessionFinished) {
		t.Errorf("Finish twice gave %v, want ErrSessionFinished", err)
	}
	_ = sess.Close() //stylegate:ignore closes the fixture session once its frames were drained
	if err := sess.Say(make([]byte, 64)); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Say after Close gave %v, want ErrSessionClosed (closed outranks finished: the session is gone)", err)
	}
	// THE SECOND TEARDOWN IS THE LIVE ARM's own sequence, and its return value IS
	// now asserted. When this suite was ported (commit add190b13) it was not: the
	// defect this file's comment used to describe — `Close()` after `Finish`
	// reporting `use of closed network connection`, because Finish closed the
	// socket without setting the flag Close guarded on — was REPORTED there, not
	// fixed, and a test asserting the broken value would have had to go red the
	// day it was fixed. Task 01a11dfe fixed it (the release is claimed once and
	// shared), so the assertion moves to the contract: nil.
	if err := sess.Close(); err != nil {
		t.Errorf("Close after Finish: %v, want nil — teardown must report the socket as already released", err)
	}
	// THE SERVER SIDE IS POLLED, NOT READ ON THE NEXT LINE — the handler
	// records its end of the connection when its OWN read fails or its dialog
	// returns, on its own goroutine, so a snapshot taken one line later would
	// be asserting the scheduler's timing. This is the ported fix from the
	// source branch (measured there: 3 of 5 runs red under load without it).
	srv.awaitServerClose(t)
}

// TestSessionCloseIsIdempotent: teardown is safe from every exit path, so a
// caller with two of them (a normal return and a deferred release) cannot make
// the second one an error.
func TestSessionCloseIsIdempotent(t *testing.T) {
	srv := newASRFakeServer(t)
	sess, err := Open(context.Background(), sessionOptions(srv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("second Close: %v, want nil — teardown must be safe to repeat", err)
	}
	srv.awaitServerClose(t)
}

// TestOpenRefusesMissingCredentialsBeforeDialing: a session with no credential
// is refused with a sentence, not with a handshake that fails for an
// unrelated-looking reason. The endpoint names a listener that answers
// nothing, so the refusal can only have come from the check — a session that
// DIALED would have failed with a connect error instead.
func TestOpenRefusesMissingCredentialsBeforeDialing(t *testing.T) {
	for _, tc := range []struct {
		name     string
		appID    string
		token    string
		wantWord string
	}{
		{"no credential at all", "", "", "app id and an access token"},
		{"token only", "", "tok", "app id and an access token"},
		{"app id only", "app", "", "app id and an access token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(context.Background(), Options{AppID: tc.appID, Token: tc.token, Endpoint: "ws://127.0.0.1:1/api/v2/asr"})
			if err == nil {
				t.Fatalf("Open with %s succeeded", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantWord) {
				t.Errorf("the refusal %q does not name the credential", err.Error())
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				t.Errorf("the refusal is an *APIError (%v) — a missing credential is a caller mistake, not a wire failure, and the two must not look alike", err)
			}
		})
	}
}

// TestOpenReportsARefusedHandshakeWithTheLogID: the upgrade response's log id
// is the identifier the vendor's support asks for, and it must survive onto the
// error line of a session that never opened. It is the one assertion in this
// file that deliberately does NOT use the fake server: the case needs a 403,
// which the fake server's handshake cannot produce.
func TestOpenReportsARefusedHandshakeWithTheLogID(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		for {
			line, err := br.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		_, _ = fmt.Fprintf(c, "HTTP/1.1 403 Forbidden\r\n%s: refused-logid\r\nContent-Length: 0\r\n\r\n", logIDHeader) //stylegate:ignore the fake server answers with a 403 carrying the log id; the header that reaches the client is the assertion
	}()
	_, err = Open(context.Background(), Options{
		AppID: "app", Token: "tok",
		Endpoint: "ws://" + ln.Addr().String() + "/api/v2/asr",
	})
	if err == nil {
		t.Fatalf("a 403 handshake was reported as a successful open")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("the refusal is %T (%v), want an *APIError", err, err)
	}
	if apiErr.Op != "connect" {
		t.Errorf("the refusal names op %q, want the step that failed (`connect`)", apiErr.Op)
	}
	if apiErr.LogID != "refused-logid" {
		t.Errorf("the refusal carries logid %q, want the one the handshake response sent", apiErr.LogID)
	}
	if apiErr.ReqID == "" {
		t.Errorf("the refusal carries no reqid — the caller's own correlation id must survive a handshake that never opened")
	}
}

//
// ── INTEGRATION (union resolution, Rule 23) ─────────────────────────────
// This file is the UNION of two independently written suites: the landed one
// ABOVE (master's, unchanged) and the branch section BELOW (task 01a11b5d,
// reworked by 01a11dd0). Four top-level names were added by BOTH sides —
// fakeServerCloseGrace, fakeServerClosePollInterval, writeServerFrame and
// readClientFrame — so they are declared exactly ONCE, above, on master's
// side, and this section uses them by name. The two constants are
// byte-identical on both sides; the two functions are one codec whose branch
// copy differed on a single body line (`finBit` where master writes `0x80`,
// the same value — ws.go:62) and on one doc line. No assertion, fake-server
// body or test in either suite was dropped, renamed or rewritten.
//

// session_test.go — the LIVE half's TEARDOWN, driven against a REAL loopback
// WebSocket server that speaks the vendor's own frame shapes.
//
// PROVENANCE, because this file has one and it matters (task 01a11b5d, reworked
// by 01a11dd0): the properties below were found and fixed on the branch that
// FIRST built a live session, whose Session had a different shape
// (Open/Push/End/Wait over SessionOptions) and whose code was never mergeable
// with the landed one — master independently shipped this feature as
// Open/Say/Finish/Close. What is PORTED here is the HARDENING, not the shape:
// the two bugs were properties of the CHANNEL, and they are properties of the
// landed session exactly as they were of the branch's.
//
// WHY A REAL SERVER AND NOT A UNIT SEAM. Every assertion below is a fact about
// the ORDER of two events on ONE connection — what the service had already read
// when Close RETURNED, and how much audio had reached it before the
// end-of-stream flag — and neither can be observed by inspecting arguments or a
// stub's call log. So the server is a real net/http handler upgraded by hand
// (the RFC's echo of the key, which is all ws.go's client half verifies), and
// the assertions read what THAT HANDLER actually received, on its own goroutine.
//
// WHAT IS MEASURED, AND WHY EACH ONE IS A LOSS THE OPERATOR WOULD FEEL:
//
//   - Close must be a JOIN, not a request to stop. The client's close frame and
//     the socket's teardown race the server's OWN read of them; a close that
//     returns before that read has failed asserts the scheduler's timing, not
//     the protocol's, and the session is still holding a socket the service is
//     still talking on.
//   - Finish must not drop audio between the last Say and the end-of-stream
//     flag. The dangerous spelling of "finish" is to close the connection and
//     let the tail go with it; the flag is what tells the service the utterance
//     is over, and a packet lost in front of it is the operator's last words.
//
// The audit that would make the FIRST property pass for the wrong reason is
// counted too (see TestSessionCloseWaitsForTheServicesOwnReadOfTheClose): the
// fake server accounts for every byte it read as either a whole packet or an
// open prefix of one, and a run whose close truncated the client's last frame
// fails on that count instead of passing on the write returning nil.

// fakeServerCloseGrace bounds how long an assertion waits for the SERVER side of
// the connection to observe the client's close.
//
// WHY A WAIT IS REQUIRED, and not a defect being papered over: the handler runs
// on its own goroutine (net/http's accept loop spawns it), so "the server saw
// the connection close" is a fact that BECOMES true, not one that is already
// true when Close returns. Awaiting it is how the assertion stops being a
// statement about the scheduler; the await is BOUNDED, and its exhaustion is
// reported rather than waited out (Rule 15).

// fakeServerClosePollInterval is the re-read cadence of awaitClientClose: short
// enough to be invisible next to the grace, long enough not to spin a core.

// liveFakeServer is one scripted stand-in for the v2 channel: it upgrades by
// hand, records every AUDIO packet it reads, answers every packet with the
// service's own bare ACK (the frame the real channel sends beside each
// response), and counts the bytes it had read when the connection went away.
type liveFakeServer struct {
	// ln is the loopback listener; the endpoint handed to Open is ws:// + its
	// address (wsDial accepts the plain scheme, which is what makes this case
	// need no certificate).
	ln net.Listener

	mu sync.Mutex
	// packets is every AUDIO packet received, in order, PARSED from the header —
	// the size it declares, the framing flag, and how much audio the payload
	// unfolds to. Parsing is deliberate: a byte count would accept a frame whose
	// header was truncated under it.
	packets []liveFakePacket
	// requests is how many full client requests arrived.
	requests int
	// shortFrames counts the frames the server had read ONLY PART OF when the
	// connection went away. It is the audit that keeps the close assertion
	// honest: a close that returns before the service read what the client sent
	// leaves its last frame here.
	shortFrames int
	// closed records that the client's connection went away, and closedAtRead is
	// how many bytes the handler had consumed by then.
	closed       bool
	closedAtRead int
	// c is the hijacked connection, kept so the case can close it at teardown: an
	// hijacked conn belongs to no http.Server and would otherwise outlive the
	// test.
	c net.Conn
}

// liveFakePacket is one audio packet as the server read it.
type liveFakePacket struct {
	size     int
	last     bool
	audioLen int
}

// newLiveFakeServer starts the listener and its handler.
func newLiveFakeServer(t *testing.T) *liveFakeServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &liveFakeServer{ln: ln}
	hs := &http.Server{Handler: http.HandlerFunc(srv.handle)}
	go func() { _ = hs.Serve(ln) }() //stylegate:ignore second fake server of the same fixture file; a serve error follows the listener's teardown
	t.Cleanup(func() {
		srv.mu.Lock()
		c := srv.c
		srv.mu.Unlock()
		if c != nil {
			_ = c.Close() //stylegate:ignore closes the fixture's client connection after the exchange
		}
		_ = hs.Close() //stylegate:ignore closes the second fake server during teardown
		_ = ln.Close() //stylegate:ignore closes its listener, ending the fixture
	})
	return srv
}

// endpoint is the ws:// URL Open should be pointed at.
func (s *liveFakeServer) endpoint() string {
	return "ws://" + s.ln.Addr().String() + "/api/v2/asr"
}

// snapshot returns the recorded state under the lock.
func (s *liveFakeServer) snapshot() (packets []liveFakePacket, requests, short int, closed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]liveFakePacket, len(s.packets))
	copy(out, s.packets)
	return out, s.requests, s.shortFrames, s.closed
}

// awaitClientClose waits for the server to have seen the connection go away, or
// reports what it saw when the grace ran out. It is the ONLY way this file reads
// the `closed` flag: reading it inline is what made the branch's version pass on
// an idle machine and fail under load.
func (s *liveFakeServer) awaitClientClose(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(fakeServerCloseGrace)
	for {
		if _, _, _, closed := s.snapshot(); closed {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("%s: the server never saw the client's connection close within %s", what, fakeServerCloseGrace)
			return
		}
		time.Sleep(fakeServerClosePollInterval) //stylegate:allow-sleep the bounded poll inside awaitClientClose; the wait itself is capped by fakeServerCloseGrace
	}
}

// handle performs the upgrade by hand — the same RFC echo of the client's key
// that ws.go's own dialer verifies — and then runs the dialog.
func (s *liveFakeServer) handle(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get(headerSecWebSocketKey)
	if key == "" {
		http.Error(w, "no key", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "cannot hijack", http.StatusInternalServerError)
		return
	}
	conn, rw, err := hj.Hijack()
	if err != nil {
		return
	}
	s.mu.Lock()
	s.c = conn
	s.mu.Unlock()
	defer conn.Close()
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		headerUpgradeName + ": " + headerUpgradeValue + "\r\n" +
		headerConnectionName + ": " + headerConnectionValue + "\r\n" +
		headerSecWebSocketAccept + ": " + wsAccept(key) + "\r\n" +
		logIDHeader + ": live-fake-logid\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	s.serve(rw.Reader, rw.Writer)
}

// serve is the dialog: the client's full request, then audio packets until the
// end-of-stream flag, then the negative-sequence terminator the service sends.
//
// THE READ IS DELIBERATELY FRAME-BY-FRAME, through this package's own parser, so
// what is recorded is what the protocol carried rather than what a byte count
// suggests. The buffer is kept for the same reason: on the way out, whatever is
// still in it was sent by the client and never became a packet.
func (s *liveFakeServer) serve(br *bufio.Reader, bw *bufio.Writer) {
	for {
		payload, opcode, err := readClientFrame(br)
		if err != nil {
			s.recordClose(br)
			return
		}
		if opcode != opcodeBinary || len(payload) < headerBytes {
			continue
		}
		if livePartitionOf(payload) != liveAudioPartition {
			s.mu.Lock()
			s.requests++
			s.mu.Unlock()
			// THE ACK THE REAL CHANNEL SENDS beside each response. It is what
			// makes the response/ACK pairing visible to a reader draining the
			// socket, and it is deliberately NOT a full response: an ACK is
			// progress (it resets the client's quiet counter), not a transcript.
			if err := s.writeAck(bw); err != nil {
				return
			}
			continue
		}
		pkt, perr := liveParsePacket(payload)
		if perr != nil {
			s.recordClose(br)
			return
		}
		s.mu.Lock()
		s.packets = append(s.packets, pkt)
		s.mu.Unlock()
		if err := s.writeAck(bw); err != nil {
			return
		}
		if !pkt.last {
			continue
		}
		// THE TERMINATOR: the service echoes the last packet's negative sequence
		// on its final response, which is the stream's end (see
		// Result.drain's reading of the same fact).
		if err := s.writeResponse(bw, -1, "你好。"); err != nil {
			return
		}
		s.recordClose(br)
		return
	}
}

// recordClose notes that the client's connection went away, together with what
// was still unread at that moment.
func (s *liveFakeServer) recordClose(br *bufio.Reader) {
	s.mu.Lock()
	s.closed = true
	s.closedAtRead = br.Buffered()
	if br.Buffered() > 0 {
		s.shortFrames++
	}
	s.mu.Unlock()
}

// writeAck renders the bare ACK the v2 channel answers every packet with: the
// header, the sequence field and an empty inner payload. It is built here rather
// than through assembleHeader because an ACK carries NO outer size field — the
// parser reads its sequence straight after the header (parseServerMessage), which
// is the vendor's own shape for this message type.
func (s *liveFakeServer) writeAck(bw *bufio.Writer) error {
	frame := make([]byte, 0, headerBytes+seqFieldBytes+sizeFieldBytes)
	frame = append(frame, liveAckHeader[:]...)
	frame = append(frame, make([]byte, seqFieldBytes+sizeFieldBytes)...)
	if err := writeServerFrame(bw, frame); err != nil {
		return err
	}
	return bw.Flush()
}

// writeResponse renders one FULL server response frame carrying a transcript.
func (s *liveFakeServer) writeResponse(bw *bufio.Writer, seq int, text string) error {
	doc := serverResponse{Code: codeSuccess, Message: "", Sequence: seq, ReqID: "live-fake-reqid", LogID: "live-fake-logid"}
	doc.Result = []resultNode{{Text: text}}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	body, err := gzipCompress(raw)
	if err != nil {
		return err
	}
	frame := assembleHeader(liveResponseHeader, uint32(len(body)), body)
	if err := writeServerFrame(bw, frame); err != nil {
		return err
	}
	return bw.Flush()
}

// liveAckHeader / liveResponseHeader are built through the PRODUCTION constants,
// so a change to the protocol's own bytes cannot leave this server answering a
// vocabulary nothing sends — the same reason the branch's fake server derived its
// nibbles from the headers it had to read.
var (
	liveAckHeader      = [headerBytes]byte{headerFullClientRequest[0], msgTypeServerAck << msgTypeShift, headerFullClientRequest[2], headerFullClientRequest[3]}
	liveResponseHeader = [headerBytes]byte{headerFullClientRequest[0], msgTypeServerFullResponse << msgTypeShift, headerFullClientRequest[2], headerFullClientRequest[3]}
)

// liveAudioPartition is the message-type nibble of an AUDIO-ONLY packet, read the
// way the client writes it (through the production template).
var liveAudioPartition = headerAudioOnly[1] >> msgTypeShift

// livePartitionOf reads a client message's type nibble.
func livePartitionOf(payload []byte) byte { return payload[1] >> msgTypeShift }

// liveParsePacket reads one recorded audio packet from the CLIENT's bytes: the
// header, the big-endian body size, and — the part a byte count cannot see —
// whether the payload is inflated by exactly what the size field declared.
func liveParsePacket(payload []byte) (liveFakePacket, error) {
	out := liveFakePacket{last: payload[1]&0x0f == headerLastAudio[1]&0x0f}
	body := payload[headerBytes:]
	if len(body) < sizeFieldBytes {
		return out, fmt.Errorf("audio packet carries %d body bytes, need at least %d", len(body), sizeFieldBytes)
	}
	out.size = int(binary.BigEndian.Uint32(body[:sizeFieldBytes]))
	compressed := body[sizeFieldBytes:]
	if out.size != len(compressed) {
		return out, fmt.Errorf("audio packet declares %d payload bytes but carries %d", out.size, len(compressed))
	}
	pcm, err := gzipDecompress(compressed)
	if err != nil {
		return out, fmt.Errorf("gunzip an audio packet: %w", err)
	}
	out.audioLen = len(pcm)
	return out, nil
}

// writeServerFrame writes one unmasked server-to-client WebSocket frame.

// readClientFrame reads one MASKED client frame — the client's own framing, read
// back by the server half (see wsConn.writeFrame for the producing side).

// ── the tests ────────────────────────────────────────────────────────────

// TestSessionCloseWaitsForTheServicesOwnReadOfTheClose is the TEARDOWN JOIN as
// far as a behavioural case can see it: the assertion is about what the SERVER
// had read when Close returned, and it is made on the server's goroutine, not on
// the client's call stack.
//
// THE PROPERTY: Close is the abort path — it releases the session without ending
// the utterance — and the session's contract is that the socket is gone and the
// reader has stopped using it. This case pins the half of that contract which IS
// observable from inside the process: the close frame reached the service, the
// client's own frames were fully read, and the client's last packet was not
// truncated by the teardown.
//
// WHAT IT CANNOT CATCH, MEASURED RATHER THAN ASSUMED (the honest half, Rule 15):
// deleting the await from Close leaves THIS CASE GREEN (mutation run, 2026-10-09)
// — a fake server runs inside the same test binary, so it stops reading at the
// same instant the client stops waiting and the two orders converge before the
// assertion is made. The absence of the join is a fact about the SOURCE, and it
// is guarded where that can be decided: session_teardown_guard_test.go, which
// reads this package's own session.go with go/ast and fails red on exactly that
// mutation.
func TestSessionCloseWaitsForTheServicesOwnReadOfTheClose(t *testing.T) {
	srv := newLiveFakeServer(t)
	sess, err := Open(context.Background(), Options{
		AppID: "app", Token: "tok", Endpoint: srv.endpoint(), ChunkMillis: 20,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := sess.Say(make([]byte, 2*DefaultSampleRate*bytesPerSample16/10)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// THE ASSERTION, ONE LINE AFTER Close RETURNED — and it is the SERVER's own
	// observation, so a client that returned early fails here.
	srv.awaitClientClose(t, "Close")

	packets, requests, short, closed := srv.snapshot()
	if !closed {
		t.Fatalf("the server never saw the connection close — Close returned before the service's own read of it failed")
	}
	if requests != 1 {
		t.Errorf("the server saw %d full client requests, want exactly 1", requests)
	}
	if len(packets) != 1 {
		t.Errorf("the server recorded %d audio packets, want the 1 that Say sent", len(packets))
	}
	// THE AUDIT THAT STOPS THIS CASE PASSING FOR THE WRONG REASON: bytes the
	// client wrote and the server never parsed are a close that truncated the
	// client's own last frame. A nil return from a write is not delivery, and
	// this is where that is measured.
	if short != 0 {
		t.Errorf("the connection carried %d frame(s) the server never read to the end — the close truncated what the client had already written", short)
	}
}

// TestSessionFinishSendsEveryPacketItWasHanded is the LOSSLESSNESS of the FINISH
// path: the audio handed to Say and the tail handed to Finish reach the service
// IN ORDER, and the end-of-stream flag rides the LAST of them.
//
// WHY IT IS WORTH A CASE. Finish is the one call that both sends audio and tears
// the connection down, so it is the call where a packet can be lost with nothing
// to show for it: a "finish" that closes the connection and lets whatever is in
// hand go with it produces a transcript that is SHORT, and short transcripts are
// indistinguishable from a recogniser that did not hear the speaker. The audio
// geometry is checked with the packet, because a packet the service cannot bill
// is a packet it did not receive.
func TestSessionFinishSendsEveryPacketItWasHanded(t *testing.T) {
	srv := newLiveFakeServer(t)
	sess, err := Open(context.Background(), Options{
		AppID: "app", Token: "tok", Endpoint: srv.endpoint(), ChunkMillis: 20,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore cleanup close of the live-drill session, whose verdict the case already reported

	// ONE FULL PACKET PER Say, so the arithmetic is the protocol's own geometry
	// rather than a number this case invented.
	packet := make([]byte, DefaultSampleRate*bytesPerSample16*DefaultChunkMillis/1000)
	for i := range packet {
		packet[i] = byte(i % 251)
	}
	if err := sess.Say(packet); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if err := sess.Say(packet); err != nil {
		t.Fatalf("Say (second): %v", err)
	}
	// THE TAIL: a partial packet — the audio the operator spoke after the last
	// full one, which is what Finish exists to carry.
	tail := packet[:len(packet)/3]
	res, err := sess.Finish(tail)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if res.Text == "" {
		t.Errorf("Finish returned no transcript although the service answered one")
	}
	if got, want := res.AudioMS, 2*pcmDurationMS(len(packet))+pcmDurationMS(len(tail)); got != want {
		t.Errorf("the session reports %d ms of audio sent, want %d (two packets plus the tail)", got, want)
	}

	srv.awaitClientClose(t, "Finish")
	packets, _, short, closed := srv.snapshot()
	if !closed {
		t.Fatalf("the server never saw the connection close after Finish")
	}
	if len(packets) != 3 {
		t.Fatalf("the server recorded %d audio packets, want 3 (two Say packets and the tail) — audio was lost between the last Say and the end-of-stream flag", len(packets))
	}
	wantAudio := []int{len(packet), len(packet), len(tail)}
	for i, want := range wantAudio {
		if got := packets[i].audioLen; got != want {
			t.Errorf("packet %d carried %d bytes of audio, want %d — the order or the payload of the packets the caller handed over was not preserved", i, got, want)
		}
		if i < len(packets)-1 && packets[i].last {
			t.Errorf("packet %d was flagged end-of-stream although audio followed it", i)
		}
	}
	if !packets[len(packets)-1].last {
		t.Errorf("the LAST packet was not flagged end-of-stream — the service would be left waiting on a stream the operator has finished")
	}
	if short != 0 {
		t.Errorf("the connection carried %d frame(s) the server never read to the end", short)
	}
}

// TestSessionCloseSendsNoEndOfStreamFlag pins the half of the teardown contract
// that Close would otherwise be free to break silently: Close is the ABORT path,
// so the utterance is not ended — the service is told nothing it did not already
// know, and no packet is invented to say it.
//
// It is asserted on the packets the server READ: a Close that sent an
// end-of-stream over the audio it had (the plausible "tidy" mistake) would show
// up as an extra flagged packet, and would bill the operator for an utterance
// they abandoned.
func TestSessionCloseSendsNoEndOfStreamFlag(t *testing.T) {
	srv := newLiveFakeServer(t)
	sess, err := Open(context.Background(), Options{
		AppID: "app", Token: "tok", Endpoint: srv.endpoint(), ChunkMillis: 20,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := sess.Say(make([]byte, 64)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	srv.awaitClientClose(t, "Close")

	packets, _, _, _ := srv.snapshot()
	for i, p := range packets {
		if p.last {
			t.Errorf("packet %d carried the end-of-stream flag on the ABORT path — Close must not tell the service the utterance is over", i)
		}
	}
}

// TestLiveFakeServerSpeaksTheProtocolsOwnShapes is the SELF-CHECK of the server
// above: a fake that is wrong in the same direction as the client would make
// every case in this file pass for a reason that has nothing to do with the
// client. It drives one request and one packet and asserts the server saw the
// FULL CLIENT REQUEST (partition derived from the production header) and answered
// the packet with an ACK that the client's own parser folds as progress.
func TestLiveFakeServerSpeaksTheProtocolsOwnShapes(t *testing.T) {
	srv := newLiveFakeServer(t)
	sess, err := Open(context.Background(), Options{
		AppID: "app", Token: "tok", Endpoint: srv.endpoint(), ChunkMillis: 20,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore cleanup close of the second drill session on the same path
	if err := sess.Say(make([]byte, 64)); err != nil {
		t.Fatalf("Say: %v", err)
	}
	// The wait is for the SERVER to have read the packet, and it polls the same
	// snapshot the assertions read — so a failure here says "nothing arrived",
	// never "it had not arrived yet".
	deadline := time.Now().Add(fakeServerCloseGrace)
	for {
		packets, requests, _, _ := srv.snapshot()
		if len(packets) == 1 && requests == 1 {
			if got := packets[0].audioLen; got != 64 {
				t.Errorf("the server unfolded %d bytes of audio from the packet, want the 64 that were sent", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server read %d request(s) and %d packet(s) within %s, want 1 and 1 — the fake speaks a vocabulary this client does not", requests, len(packets), fakeServerCloseGrace)
		}
		time.Sleep(fakeServerClosePollInterval) //stylegate:allow-sleep the bounded poll of a real connection's arrival; capped by fakeServerCloseGrace
	}
}
