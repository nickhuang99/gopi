package doubaosauc

// session_test.go — the LIVE session's tests, over a fake service that speaks
// THIS package's own envelope (the frame builders above are the same ones the
// client writes, so a test that agreed with a hand-rolled copy of the protocol
// would be a test that agreed with nothing).
//
// WHY A FAKE SERVICE RATHER THAN A UNIT OF EACH FUNCTION. The live session's
// obligations are all CROSS-FRAME facts — the transcript grows while the audio is
// still going, the end-of-stream flag is the NEGATED sequence, the flow phase arms
// NO read deadline (a deadline that expires inside a frame desynchronises the
// reader: see sessionFlowReadTimeout) — and none of them is visible in a single
// function's return value. They are visible in an exchange.

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── the fake service ───────────────────────────────────────────────────

// wsMagicGUID is RFC 6455 §1.3's constant: the key the handshake's accept value
// is derived from. It is a protocol constant, not a tunable, so it is named once.
const wsMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// handshake headers the fake mirrors.
const (
	headerSecWebSocketKey    = "Sec-WebSocket-Key"
	headerSecWebSocketAccept = "Sec-WebSocket-Accept"
	headerUpgrade            = "Upgrade"
	headerConnection         = "Connection"
	headerLogIDFake          = "X-Tt-Logid"
	// opcodeClose / opcodeBinary are RFC 6455's frame opcodes. The transport
	// package keeps its own copies (they are the same two numbers), and they are
	// spelled here because the FAKE is the other end of the wire: a fake that
	// borrowed the client's constants could not disagree with it, and the point of
	// this fake is that it is written from the protocol.
	opcodeClose  = 0x8
	opcodeBinary = 0x2
)

// fakeService is one scripted v3 endpoint. It records what it was sent (so a case
// can assert the WIRE, not the client's intention) and answers on its own terms.
type fakeService struct {
	srv *httptest.Server

	mu sync.Mutex
	// requests counts the full-client-request frames received.
	requests int
	// audioSeqs is every audio packet's sequence number, in arrival order — the
	// END-OF-STREAM assertion reads it (the last one is NEGATED).
	audioSeqs []int32
	// audioSizes is every audio packet's decompressed payload size.
	audioSizes []int
	// lastTail is the payload of the packet whose sequence was negative.
	lastTail []byte
	// closed reports that the handler returned (the server saw the connection go).
	closed bool

	// partials is the text script: packet N (1-based) is answered with
	// partials[N-1] when it exists. A text is REWRITTEN, never appended.
	partials []string
	// finalText is the text the last-package response carries.
	finalText string
	// flowStall is how long the service waits before answering ONE mid-stream
	// packet: it is how a case proves the flow phase arms no deadline.
	flowStall time.Duration
	// stallAfterPacket is the 1-based packet index the stall precedes.
	stallAfterPacket int
	// stayQuiet makes the service answer nothing after the end-of-stream flag
	// (the quiet path). A case that uses it pays sessionFinishWait.
	stayQuiet bool
}

func (s *fakeService) URL() string { return "ws" + strings.TrimPrefix(s.srv.URL, "http") }

func (s *fakeService) record(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// handle performs the upgrade and then the scripted dialog.
func (s *fakeService) handle(w http.ResponseWriter, r *http.Request) {
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
		_ = conn.Close() //stylegate:ignore the fake service closes its own socket at the end of the script
		s.record(func() { s.closed = true })
	}()
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		headerUpgrade + ": websocket\r\n" +
		headerConnection + ": Upgrade\r\n" +
		headerSecWebSocketAccept + ": " + wsAccept(key) + "\r\n" +
		headerLogIDFake + ": fake-logid\r\n\r\n"
	if _, err := rw.WriteString(resp); err != nil {
		return
	}
	if err := rw.Flush(); err != nil {
		return
	}
	s.dialog(rw.Reader, rw.Writer)
}

// dialog reads client frames until the connection ends, answering each audio
// packet with the script's next partial and the negated-sequence packet with the
// final response.
func (s *fakeService) dialog(br *bufio.Reader, bw *bufio.Writer) {
	for {
		opcode, payload, err := wsReadFrame(br)
		if err != nil {
			return
		}
		if opcode == opcodeClose {
			_ = wsWriteFrame(bw, opcodeClose, nil) //stylegate:ignore the goodbye is the script's own end; the client's reply is what the case asserts
			return
		}
		if opcode != opcodeBinary || len(payload) < headerSize4Bytes {
			continue
		}
		switch msgType := payload[1] >> 4; msgType {
		case msgTypeFullClientRequest:
			s.record(func() { s.requests++ })
		case msgTypeAudioOnlyRequest:
			seq := int32(binary.BigEndian.Uint32(payload[headerSize4Bytes:]))
			// The audio frame's payload starts AFTER the header, the sequence
			// field and the length field — the same three offsets the client's own
			// builder wrote, read back with the protocol's own constants.
			body := headerSize4Bytes + sequenceFieldBytes + lengthFieldBytes
			pcm, _ := gunzipBytes(payload[body:]) //stylegate:ignore the frame was written by this test's own builder; the recorded payload length is the assertion, so a decode failure shows up as a wrong size
			s.record(func() {
				s.audioSeqs = append(s.audioSeqs, seq)
				s.audioSizes = append(s.audioSizes, len(pcm))
				if seq < 0 {
					s.lastTail = append([]byte(nil), pcm...)
				}
			})
			index := len(s.audioSeqs)
			if seq < 0 {
				if s.stayQuiet {
					// NOTHING is answered: the case is measuring the quiet path.
					continue
				}
				if err := s.writeResponse(bw, seq, s.finalText, true); err != nil {
					return
				}
				continue
			}
			if s.flowStall > 0 && index == s.stallAfterPacket {
				time.Sleep(s.flowStall) //stylegate:allow-sleep the fixture's SCRIPTED mid-stream flow stall, not a test wait
			}
			text := ""
			if index-1 < len(s.partials) {
				text = s.partials[index-1]
			}
			if text == "" && index == 1 && len(s.partials) == 0 {
				// The service's first answer is a bare acknowledgement (no text):
				// the real one sends `result.additions` and nothing else, and a case
				// must not depend on text it did not script.
				if err := s.writeResponse(bw, seq, "", false); err != nil {
					return
				}
				continue
			}
			if err := s.writeResponse(bw, seq, text, false); err != nil {
				return
			}
		}
	}
}

// writeResponse sends one server full response in the package's own envelope.
func (s *fakeService) writeResponse(bw *bufio.Writer, seq int32, text string, last bool) error {
	doc := responseDoc{Code: 0, Result: responseResult{Text: text}}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	gz, err := gzipBytes(body)
	if err != nil {
		return err
	}
	flags := byte(flagSequencePos)
	if last {
		flags |= flagLastPacket
	}
	frame := saucHeader(msgTypeServerFullRespond, flags)
	frame = putUint32(frame, seq)
	frame = putUint32(frame, int32(len(gz)))
	frame = append(frame, gz...)
	return wsWriteFrame(bw, opcodeBinary, frame)
}

// startFakeService starts one scripted service and stops it when the test ends.
func startFakeService(t *testing.T, svc *fakeService) *fakeService {
	t.Helper()
	svc.srv = httptest.NewServer(http.HandlerFunc(svc.handle))
	t.Cleanup(svc.srv.Close)
	return svc
}

func (s *fakeService) snapshot() (requests int, seqs []int32, sizes []int, tail []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests, append([]int32(nil), s.audioSeqs...), append([]int(nil), s.audioSizes...), append([]byte(nil), s.lastTail...)
}

// liveOpts is the credential + endpoint a live session is opened with: the fake
// service's URL, and the legacy pair the v3 endpoint accepts.
func liveOpts(t *testing.T, svc *fakeService, onPartial func(Partial)) Options {
	t.Helper()
	return Options{
		AppKey:    "fake-app-key",
		AccessKey: "fake-access-key",
		Endpoint:  svc.URL(),
		OnPartial: onPartial,
	}
}

// ── the RFC 6455 helpers the fake needs ────────────────────────────────

func wsAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+wsMagicGUID) //stylegate:ignore writes to the test's own sink; the buffered content is the assertion
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// wsWriteFrame writes one unmasked (server-side) text/binary frame.
func wsWriteFrame(w *bufio.Writer, opcode byte, payload []byte) error {
	hdr := []byte{0x80 | opcode}
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return w.Flush()
}

// wsReadFrame reads one client frame, unmasking its payload.
func wsReadFrame(r *bufio.Reader) (byte, []byte, error) {
	b0, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	b1, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	opcode := b0 & 0x0F
	masked := b1&0x80 != 0
	n := int(b1 & 0x7F)
	switch n {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return 0, nil, err
		}
		n = int(binary.BigEndian.Uint64(ext[:]))
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, n)
	if n > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

// ── the cases ──────────────────────────────────────────────────────────

// TestSessionDeliversPartialsWhileTheAudioIsStillGoing is the whole point of this
// session type: the text arrives on the READER's goroutine as the service answers
// each packet, i.e. BEFORE the caller has said it was done.
func TestSessionDeliversPartialsWhileTheAudioIsStillGoing(t *testing.T) {
	svc := startFakeService(t, &fakeService{
		partials:  []string{"能听到", "能听到吗", "能听到吗？不知道"},
		finalText: "能听到吗？不知道能不能听到声音呢？",
	})
	got := make(chan string, 8)
	sess, err := Open(context.Background(), liveOpts(t, svc, func(p Partial) {
		got <- fmt.Sprintf("%d:%s", p.Index, p.Text)
	}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore the deferred close is the test's teardown; Finish already released the socket

	packet := make([]byte, 3200) // 100 ms of 16 kHz mono PCM16
	for i := 0; i < 3; i++ {
		if err := sess.Say(packet); err != nil {
			t.Fatalf("say %d: %v", i+1, err)
		}
		// THE PARTIAL MUST ARRIVE BEFORE THE NEXT PACKET IS SENT — that is what
		// "while the audio is still going" means, and it is asserted rather than
		// assumed.
		select {
		case line := <-got:
			want := fmt.Sprintf("%d:%s", i+1, []string{"能听到", "能听到吗", "能听到吗？不知道"}[i])
			if line != want {
				t.Fatalf("partial %d = %q, want %q", i+1, line, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no partial arrived before packet %d was sent — the transcript is not streaming", i+2)
		}
	}
	if n := sess.PartialCount(); n != 3 {
		t.Fatalf("PartialCount = %d, want 3", n)
	}
	res, err := sess.Finish(packet)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if res.Text != "能听到吗？不知道能不能听到声音呢？" {
		t.Fatalf("final text = %q", res.Text)
	}
	if res.LogID != "fake-logid" {
		t.Fatalf("log id = %q, want the handshake's own (the vendor's ONE identifier)", res.LogID)
	}
	if res.AudioMS == 0 {
		t.Fatalf("the result reports no audio: nothing was counted as sent")
	}
	if res.FirstTextAt <= 0 {
		t.Fatalf("FirstTextAt = %v, want the first text's arrival", res.FirstTextAt)
	}
}

// TestSessionFinishSendsTheTailAsTheNegatedSequence is the end-of-stream
// obligation: the audio in hand goes out with a NEGATED sequence (the protocol's
// own "I am done speaking"), byte for byte, and nothing before it is lost.
func TestSessionFinishSendsTheTailAsTheNegatedSequence(t *testing.T) {
	svc := startFakeService(t, &fakeService{finalText: "你好"})
	sess, err := Open(context.Background(), liveOpts(t, svc, nil))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore the teardown close; Finish already released the socket

	first := []byte{1, 2, 3, 4}
	tail := []byte{9, 9, 9, 9, 9, 9}
	if err := sess.Say(first); err != nil {
		t.Fatalf("say: %v", err)
	}
	if _, err := sess.Finish(tail); err != nil {
		t.Fatalf("finish: %v", err)
	}

	requests, seqs, sizes, gotTail := svc.snapshot()
	if requests != 1 {
		t.Fatalf("the service saw %d request frames, want 1 (the session's parameters)", requests)
	}
	if len(seqs) != 2 {
		t.Fatalf("the service saw %d audio packets, want 2 (the packet and the tail)", len(seqs))
	}
	if seqs[0] != firstAudioSequence {
		t.Errorf("the first audio packet's sequence = %d, want %d", seqs[0], firstAudioSequence)
	}
	if seqs[1] >= 0 {
		t.Errorf("the last packet's sequence = %d, want it NEGATED (the end-of-stream flag)", seqs[1])
	}
	if sizes[0] != len(first) || sizes[1] != len(tail) {
		t.Errorf("packet sizes = %v, want [%d %d]", sizes, len(first), len(tail))
	}
	if string(gotTail) != string(tail) {
		t.Errorf("the last packet carried %v, want the tail %v", gotTail, tail)
	}
}

// TestSessionRefusesAudioAfterFinishAndAfterClose pins the two named states — the
// chat live arm feeds audio from one goroutine while another may be finishing the
// utterance, and that race must be DISTINGUISHABLE from a broken transport.
func TestSessionRefusesAudioAfterFinishAndAfterClose(t *testing.T) {
	svc := startFakeService(t, &fakeService{finalText: "好"})
	sess, err := Open(context.Background(), liveOpts(t, svc, nil))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := sess.Finish(nil); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if err := sess.Say(make([]byte, 32)); !errors.Is(err, ErrSessionFinished) {
		t.Errorf("Say after Finish = %v, want ErrSessionFinished", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("Close after Finish = %v, want nil (the socket was already released)", err)
	}
	if err := sess.Close(); err != nil {
		t.Errorf("the second Close = %v, want nil (Close is idempotent)", err)
	}
	if err := sess.Say(make([]byte, 32)); !errors.Is(err, ErrSessionClosed) {
		t.Errorf("Say after Close = %v, want ErrSessionClosed", err)
	}
}

// TestSessionCloseSendsNoEndOfStreamFlag: Close is the ABORT path. It must not
// tell the service the utterance ended, because it did not.
func TestSessionCloseSendsNoEndOfStreamFlag(t *testing.T) {
	svc := startFakeService(t, &fakeService{finalText: "好"})
	sess, err := Open(context.Background(), liveOpts(t, svc, nil))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := sess.Say(make([]byte, 320)); err != nil {
		t.Fatalf("say: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, seqs, _, _ := svc.snapshot()
	for _, seq := range seqs {
		if seq < 0 {
			t.Fatalf("Close sent a NEGATED sequence (%d): it ended the utterance instead of abandoning it", seq)
		}
	}
}

// TestSessionFlowPhaseArmsNoReadDeadline is the frame-boundary regression guard,
// and it is the measured defect it prevents (doubaostream's session.go records the
// same one): a read deadline that expires INSIDE a frame leaves the framing state
// half-consumed, reading on parses the rest of that frame as a header, and a live
// dictation dies with an unreadable protocol error.
//
// THE CASE STALLS THE SERVICE FOR LONGER THAN THE DRAIN BUDGET, MID-STREAM. If the
// flow phase armed that budget, the reader would time out inside the response
// frame and lose the partial that followed; because the flow phase arms NO
// deadline, the stall is uneventful and the transcript still arrives.
func TestSessionFlowPhaseArmsNoReadDeadline(t *testing.T) {
	if sessionFlowReadTimeout != 0 {
		t.Fatalf("the flow phase arms %v as a read deadline; it must arm NONE (see sessionFlowReadTimeout)", sessionFlowReadTimeout)
	}
	stall := sessionDrainReadTimeout + 500*time.Millisecond
	svc := startFakeService(t, &fakeService{
		partials:         []string{"第一段", "第二段"},
		finalText:        "第一段，第二段。",
		flowStall:        stall,
		stallAfterPacket: 2,
	})
	got := make(chan Partial, 4)
	sess, err := Open(context.Background(), liveOpts(t, svc, func(p Partial) { got <- p }))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore the teardown close; Finish already released the socket

	packet := make([]byte, 3200)
	for i := 0; i < 2; i++ {
		if err := sess.Say(packet); err != nil {
			t.Fatalf("say %d: %v", i+1, err)
		}
	}
	// The first answer is immediate; the SECOND is late by the stall, and it must
	// still arrive rather than being lost to a read deadline inside its frame.
	select {
	case p := <-got:
		if p.Text != "第一段" {
			t.Fatalf("the first partial = %q, want the service's own text", p.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("the first partial never arrived")
	}
	select {
	case p := <-got:
		if p.Text != "第二段" {
			t.Fatalf("the partial after the stall = %q, want the service's own text", p.Text)
		}
	case <-time.After(stall + 3*time.Second):
		t.Fatalf("the partial after a %v stall never arrived: the flow phase timed out inside a frame", stall)
	}
	res, err := sess.Finish(packet)
	if err != nil {
		t.Fatalf("finish after the stall: %v", err)
	}
	if res.Text == "" {
		t.Fatalf("the final answer is empty after a mid-stream stall")
	}
}

// TestSessionFinishReportsTheServicesQuietEnd is the other half of the drain: a
// service that answers NOTHING after the end-of-stream flag is the asynchronous
// recogniser saying "that is all", and the session ends on that silence rather
// than holding the operator's key forever.
func TestSessionFinishReportsTheServicesQuietEnd(t *testing.T) {
	svc := startFakeService(t, &fakeService{stayQuiet: true, partials: []string{"片段"}})
	sess, err := Open(context.Background(), liveOpts(t, svc, nil))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = sess.Close() }() //stylegate:ignore the teardown close; Finish already released the socket

	if err := sess.Say(make([]byte, 3200)); err != nil {
		t.Fatalf("say: %v", err)
	}
	start := time.Now()
	res, err := sess.Finish(make([]byte, 32))
	elapsed := time.Since(start)
	// THE TEXT THAT DID ARRIVE IS THE ANSWER, and the exhaustion of the wait is
	// REPORTED rather than silently truncating.
	if err == nil && res.Text != "片段" {
		t.Fatalf("text = %q, want the partial the service did send", res.Text)
	}
	if elapsed > sessionFinishWait+sessionCloseWait+2*time.Second {
		t.Fatalf("Finish waited %v, above its own bound (%v): the wait is not bounded",
			elapsed, sessionFinishWait)
	}
}

// TestSessionExposesTheProtocolsOwnSequenceNumbers documents the one numbering
// fact the service refuses confusingly: the full request consumes sequence 1 and
// the audio continues at 2.
func TestSessionExposesTheProtocolsOwnSequenceNumbers(t *testing.T) {
	if firstAudioSequence != 2 || fullRequestSequence != 1 {
		t.Fatalf("sequences = request %d / audio %d, want 1 / 2 (an audio packet repeating the request's sequence is refused)",
			fullRequestSequence, firstAudioSequence)
	}
}
