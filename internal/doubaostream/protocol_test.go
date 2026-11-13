package doubaostream

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

// ── the frame formats (the protocol's whole contract, byte by byte) ─────

// TestHeaderTemplatesAreTheProtocolsBytes pins the three message headers. They
// are the values the official demo sends and the values the service accepted
// (measured); a change here is a change of protocol, not a refactor, so the
// bytes are asserted literally rather than recomputed from the bit fields.
func TestHeaderTemplatesAreTheProtocolsBytes(t *testing.T) {
	cases := []struct {
		name string
		got  [headerBytes]byte
		want [headerBytes]byte
	}{
		{"full client request", headerFullClientRequest, [headerBytes]byte{0x11, 0x10, 0x11, 0x00}},
		{"audio only", headerAudioOnly, [headerBytes]byte{0x11, 0x20, 0x11, 0x00}},
		{"last audio", headerLastAudio, [headerBytes]byte{0x11, 0x22, 0x11, 0x00}},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s header = % x, want % x", c.name, c.got, c.want)
		}
	}
	// The distinctions the three encode, asserted as nibbles so a copy-paste
	// between them cannot pass the literal check above.
	if headerFullClientRequest[1]>>msgTypeShift != 0x1 {
		t.Errorf("the full client request is not message type 0b0001")
	}
	if headerAudioOnly[1]>>msgTypeShift != 0x2 || headerLastAudio[1]>>msgTypeShift != 0x2 {
		t.Errorf("an audio packet is not message type 0b0010")
	}
	if headerAudioOnly[1]&0x0f != 0 {
		t.Errorf("a non-final audio packet must carry flags 0b0000, got 0b%04b", headerAudioOnly[1]&0x0f)
	}
	if headerLastAudio[1]&0x0f != 0x2 {
		t.Errorf("the LAST audio packet must carry the end-of-stream flag 0b0010, got 0b%04b", headerLastAudio[1]&0x0f)
	}
	for _, h := range [][headerBytes]byte{headerFullClientRequest, headerAudioOnly, headerLastAudio} {
		if h[0] != 0x11 {
			t.Errorf("protocol version / header size nibbles = %02x, want 0x11", h[0])
		}
		if h[2] != 0x11 {
			t.Errorf("serialization / compression nibbles = %02x, want 0x11 (JSON + gzip)", h[2])
		}
	}
}

// TestFullClientRequestRoundTrips proves the JSON document survives the wire
// form: header + size + gzip, and that the declared size matches the bytes.
func TestFullClientRequestRoundTrips(t *testing.T) {
	doc := requestDoc{
		App:     appMeta{AppID: "app-1", Token: "tok-1", Cluster: "volcengine_streaming_common"},
		User:    userMeta{UID: "gopi"},
		Audio:   audioMeta{Format: "raw", Rate: 16000, Bits: 16, Channel: 1},
		Request: requestMeta{ReqID: "r-1", Sequence: 1, NBest: 1, Workflow: DefaultWorkflow, ResultType: "full", ShowUtterances: true},
	}
	frame, err := buildFullClientRequest(doc)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !bytes.Equal(frame[:headerBytes], headerFullClientRequest[:]) {
		t.Fatalf("frame does not start with the full-client-request header: % x", frame[:headerBytes])
	}
	size := int(binary.BigEndian.Uint32(frame[headerBytes : headerBytes+sizeFieldBytes]))
	body := frame[headerBytes+sizeFieldBytes:]
	if size != len(body) {
		t.Fatalf("declared size %d, body %d bytes", size, len(body))
	}
	plain, err := gzipDecompress(body)
	if err != nil {
		t.Fatalf("gunzip the request: %v", err)
	}
	var back requestDoc
	if err := json.Unmarshal(plain, &back); err != nil {
		t.Fatalf("the request payload is not JSON: %v", err)
	}
	if back.App != doc.App || back.Request.ReqID != doc.Request.ReqID || back.Audio.Rate != doc.Audio.Rate {
		t.Errorf("round trip lost fields: got %+v", back)
	}
	// The cluster is the one field whose value decides whether the request is
	// served at all, so it is asserted on the WIRE bytes as well as in the doc.
	if !strings.Contains(string(plain), DefaultCluster) {
		t.Errorf("the request does not carry the cluster %q", DefaultCluster)
	}
}

// TestAudioPacketUsesTheLastHeaderOnlyForTheLastPacket is the end-of-stream
// signal's own test: the flag is what tells the service to finish.
func TestAudioPacketUsesTheLastHeaderOnlyForTheLastPacket(t *testing.T) {
	pcm := []byte{1, 0, 2, 0}
	mid, err := buildAudioPacket(pcm, false)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !bytes.Equal(mid[:headerBytes], headerAudioOnly[:]) {
		t.Errorf("a non-final packet carries header % x, want % x", mid[:headerBytes], headerAudioOnly[:])
	}
	last, err := buildAudioPacket(pcm, true)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !bytes.Equal(last[:headerBytes], headerLastAudio[:]) {
		t.Errorf("the final packet carries header % x, want % x", last[:headerBytes], headerLastAudio[:])
	}
	plain, err := gzipDecompress(last[headerBytes+sizeFieldBytes:])
	if err != nil {
		t.Fatalf("gunzip the audio: %v", err)
	}
	if !bytes.Equal(plain, pcm) {
		t.Errorf("audio payload = % x, want % x", plain, pcm)
	}
}

// ── the server side's parsing ──────────────────────────────────────────

// gzipBytes is the test's own compressor (independent of the package's, so a
// bug in gzipCompress cannot make a parse test pass by symmetric error).
func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("gzip: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// serverFrame builds a wire frame the way the SERVICE does: header, then the
// size (or sequence + size) field, then the gzipped payload.
func serverFrame(kind byte, fields []uint32, payload []byte) []byte {
	out := []byte{0x11, kind << msgTypeShift, 0x11, 0x00}
	for _, f := range fields {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], f)
		out = append(out, b[:]...)
	}
	return append(out, payload...)
}

func TestParseFullServerResponse(t *testing.T) {
	doc := `{"reqid":"r-9","code":1000,"message":"Success","sequence":-3,` +
		`"duration":"3696","log_id":"log-9","result":[{"text":"这是字节跳动，","confidence":8,` +
		`"utterances":[{"text":"这是字节跳动，","start_time":0,"end_time":1705,"definite":true}]}]}`
	frame := serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gzipBytes(t, []byte(doc))))}, gzipBytes(t, []byte(doc)))
	msg, err := parseServerMessage(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Kind != msgTypeServerFullResponse || !msg.HasResponse {
		t.Fatalf("kind = %02x, hasResponse = %v", msg.Kind, msg.HasResponse)
	}
	if msg.Response.Code != codeSuccess || msg.Response.ReqID != "r-9" || msg.Response.Sequence != -3 {
		t.Errorf("decoded %+v, want code 1000 / reqid r-9 / sequence -3", msg.Response)
	}
	if got := msg.Response.text(); got != "这是字节跳动，" {
		t.Errorf("text = %q", got)
	}
	if d, err := msg.Response.Duration.Int64(); err != nil || d != 3696 {
		t.Errorf("duration = %v (%v), want 3696 — the service sends it as a STRING here", d, err)
	}
	if msg.Response.LogID != "log-9" {
		t.Errorf("log_id = %q, want log-9", msg.Response.LogID)
	}
	if !definiteOf(msg.Response) {
		t.Errorf("a negative sequence must read as definite")
	}
}

// TestParseDurationAcceptsBothSpellings: the service has been seen to send the
// duration as a number and as a quoted string. A typed int field would make the
// quoted form fail the WHOLE document — losing the transcript along with it —
// so both are pinned here.
func TestParseDurationAcceptsBothSpellings(t *testing.T) {
	for _, spelling := range []string{`"duration":3696`, `"duration":"3696"`} {
		doc := `{"code":1000,"message":"Success",` + spelling + `,"result":[{"text":"x"}]}`
		gz := gzipBytes(t, []byte(doc))
		frame := serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gz))}, gz)
		msg, err := parseServerMessage(frame)
		if err != nil {
			t.Fatalf("%s: parse: %v", spelling, err)
		}
		if d, err := msg.Response.Duration.Int64(); err != nil || d != 3696 {
			t.Errorf("%s: duration = %v (%v)", spelling, d, err)
		}
		if msg.Response.text() != "x" {
			t.Errorf("%s: text lost", spelling)
		}
	}
}

// TestParseAckCarriesSequenceAndNoPayload: the ack is the frame that follows
// every response, and treating it as a result is how a stream reports silence
// while the transcript grows behind it.
func TestParseAckCarriesSequenceAndNoPayload(t *testing.T) {
	frame := serverFrame(msgTypeServerAck, []uint32{7, 0}, nil)
	msg, err := parseServerMessage(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Kind != msgTypeServerAck {
		t.Fatalf("kind = %02x", msg.Kind)
	}
	if msg.Sequence != 7 {
		t.Errorf("sequence = %d, want 7", msg.Sequence)
	}
	if msg.HasResponse || len(msg.JSON) != 0 {
		t.Errorf("an empty ack produced a document: %+v", msg)
	}
}

// TestParseServerErrorKeepsCodeAndReason: an error frame carries the vendor's
// own code and a JSON body with the reason, and both must survive parsing — a
// refusal that loses its reason makes every incident start from zero.
func TestParseServerErrorKeepsCodeAndReason(t *testing.T) {
	doc := `{"code":45000030,"message":"requested resource not granted"}`
	gz := gzipBytes(t, []byte(doc))
	frame := serverFrame(msgTypeServerError, []uint32{uint32(45000030), uint32(len(gz))}, gz)
	msg, err := parseServerMessage(frame)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if msg.Code != 45000030 {
		t.Errorf("code = %d, want 45000030", msg.Code)
	}
	if got := serverErrorMessage(msg); got != "requested resource not granted" {
		t.Errorf("reason = %q", got)
	}
}

// TestParseRefusesTruncatedAndMismatchedFrames: the two ways a frame can lie
// about its own length. Neither may be silently accepted (a declared-larger
// size would decode a partial document; a declared-smaller one would hide bytes
// the service sent).
func TestParseRefusesTruncatedAndMismatchedFrames(t *testing.T) {
	doc := `{"code":1000,"message":"Success"}`
	gz := gzipBytes(t, []byte(doc))
	good := serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gz))}, gz)

	cases := []struct {
		name  string
		frame []byte
	}{
		{"shorter than the header", good[:3]},
		{"declared size larger than the body", serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gz) + 40)}, gz)},
		{"declared size smaller than the body", serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(gz) - 2)}, gz)},
		{"no size field at all", append([]byte{0x11, 0x90, 0x11, 0x00}, gz...)},
		{"unknown message type", serverFrame(0x0c, []uint32{0}, nil)},
		{"gzip flag with non-gzip payload", serverFrame(msgTypeServerFullResponse, []uint32{5}, []byte("hello"))},
	}
	for _, c := range cases {
		if _, err := parseServerMessage(c.frame); err == nil {
			t.Errorf("%s: accepted silently", c.name)
		}
	}
}

// TestParseRefusesNonJSONResponseButKeepsANonJSONError is the asymmetry that
// the vendor's own demo shows: a RESPONSE that is not JSON is a protocol break,
// while an error frame is allowed to carry prose.
func TestParseRefusesNonJSONResponseButKeepsANonJSONError(t *testing.T) {
	raw := []byte("this is not json")
	if _, err := parseServerMessage(serverFrame(msgTypeServerFullResponse, []uint32{uint32(len(raw))}, raw)); err == nil {
		t.Errorf("a non-JSON response was accepted")
	}
	prose := gzipBytes(t, raw)
	msg, err := parseServerMessage(serverFrame(msgTypeServerError, []uint32{9, uint32(len(prose))}, prose))
	if err != nil {
		t.Fatalf("a prose error frame was refused: %v", err)
	}
	if got := serverErrorMessage(msg); got != string(raw) {
		t.Errorf("reason = %q, want the raw body", got)
	}
}

// TestTextFallsBackToUtterances: a response that carries only utterances must
// not read as "no speech" — that is the false negative that would make a real
// transcript look like an empty run.
func TestTextFallsBackToUtterances(t *testing.T) {
	resp := serverResponse{Result: []resultNode{{Utterances: []utteranceN{
		{Text: "第一句。"}, {Text: "第二句。"},
	}}}}
	if got := resp.text(); got != "第一句。第二句。" {
		t.Errorf("text = %q, want the joined utterances", got)
	}
	resp = serverResponse{Result: []resultNode{{Text: "整体文本", Utterances: []utteranceN{{Text: "分句"}}}}}
	if got := resp.text(); got != "整体文本" {
		t.Errorf("text = %q, want the result-level text when present", got)
	}
}

// ── the WebSocket framing (RFC 6455's client half) ─────────────────────

// socketPair returns a connected client/server pair over loopback TCP. NOT
// net.Pipe: that is an unbuffered channel with no write buffer, so a writer
// blocks until the peer reads and a test that writes "a ping, then data" (or
// reads a pong it must first produce) deadlocks against itself. A real socket
// behaves the way the service's end of the connection does, which is the point
// of these tests.
func socketPair(t *testing.T) (*wsConn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }() //stylegate:ignore teardown close of the protocol fixture's listener, after the handshake frames were read
	type accepted struct {
		conn net.Conn
		err  error
	}
	ch := make(chan accepted, 1)
	go func() {
		c, err := ln.Accept()
		ch <- accepted{c, err}
	}()
	cli, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	a := <-ch
	if a.err != nil {
		t.Fatalf("accept: %v", a.err)
	}
	t.Cleanup(func() { cli.Close(); a.conn.Close() }) //stylegate:ignore cleanup closes the accepted pair of the protocol fixture; the frame exchange is the assertion
	return &wsConn{conn: cli, br: bufio.NewReader(cli)}, a.conn
}

// TestWSFrameIsMaskedAndRoundTrips drives the real frame writer against a real
// socket: the MASK bit is set, the payload is masked with the key the header
// carries, and a peer can recover the bytes. Forgetting the mask bit is the
// mistake that gets the connection closed with "incorrect mask flag" — a
// message that reads like a business error.
func TestWSFrameIsMaskedAndRoundTrips(t *testing.T) {
	c, srv := socketPair(t)

	payload := []byte("the quick brown fox jumps over the lazy dog — 中文也要能过")
	done := make(chan error, 1)
	go func() {
		done <- c.writeFrame(opcodeBinary, payload)
	}()

	hdr := make([]byte, 2)
	if _, err := io.ReadFull(srv, hdr); err != nil {
		t.Fatalf("read header: %v", err)
	}
	if hdr[0]&finBit == 0 || hdr[0]&0x0f != opcodeBinary {
		t.Errorf("first byte = %02x, want FIN|binary", hdr[0])
	}
	if hdr[1]&maskBit == 0 {
		t.Fatalf("CLIENT frame is not masked (second byte %02x) — the server closes the connection for this", hdr[1])
	}
	length := int(hdr[1] & 0x7f)
	switch length {
	case len16Marker:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(srv, ext); err != nil {
			t.Fatalf("read length: %v", err)
		}
		length = int(binary.BigEndian.Uint16(ext))
	case len64Marker:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(srv, ext); err != nil {
			t.Fatalf("read length: %v", err)
		}
		length = int(binary.BigEndian.Uint64(ext))
	}
	if length != len(payload) {
		t.Fatalf("length = %d, want %d", length, len(payload))
	}
	key := make([]byte, wsMaskKeyBytes)
	if _, err := io.ReadFull(srv, key); err != nil {
		t.Fatalf("read mask key: %v", err)
	}
	masked := make([]byte, length)
	if _, err := io.ReadFull(srv, masked); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	for i := range masked {
		masked[i] ^= key[i%wsMaskKeyBytes]
	}
	if !bytes.Equal(masked, payload) {
		t.Fatalf("unmasked payload = %q, want %q", masked, payload)
	}
	if err := <-done; err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
}

// TestWSReaderAnswersPingAndSkipsControlFrames: the service pings a fresh
// connection until the first request arrives. A client that hands the ping
// payload to the JSON parser reads garbage; a client that never pongs is
// eventually closed.
func TestWSReaderAnswersPingAndSkipsControlFrames(t *testing.T) {
	c, srv := socketPair(t)

	// A ping, then the data the caller actually wanted. The pong the reader
	// sends back is read on the same connection, from a goroutine that starts
	// FIRST so it is already waiting when the pong is written.
	pongCh := make(chan []byte, 1)
	go func() {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(srv, hdr); err != nil {
			pongCh <- nil
			return
		}
		length := int(hdr[1] & 0x7f)
		key := make([]byte, wsMaskKeyBytes)
		if _, err := io.ReadFull(srv, key); err != nil {
			pongCh <- nil
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(srv, body); err != nil {
			pongCh <- nil
			return
		}
		for i := range body {
			body[i] ^= key[i%wsMaskKeyBytes]
		}
		pongCh <- append([]byte{hdr[0], hdr[1]}, body...)
	}()
	go func() {
		// SERVER → CLIENT frames are NOT masked: 0x04 is length 4, and the
		// mask bit must stay clear or the reader consumes the next frame's
		// first bytes as a mask key.
		_, _ = srv.Write([]byte{finBit | opcodePing, 0x04, 'p', 'i', 'n', 'g'})   //stylegate:ignore writes a canned ping frame from the fake server; a write error only means the client stopped reading
		_, _ = srv.Write([]byte{finBit | opcodeBinary, 0x04, 'd', 'a', 't', 'a'}) //stylegate:ignore writes the canned binary frame the parser case is about
	}()

	c.SetReadDeadline(2 * time.Second)
	msg, err := c.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if string(msg) != "data" {
		t.Fatalf("message = %q, want the data frame after the ping", msg)
	}
	select {
	case pong := <-pongCh:
		if pong == nil {
			t.Fatal("no pong was sent for the server's ping")
		}
		if op := pong[0] & 0x0f; op != opcodePong {
			t.Errorf("answered a ping with opcode %02x, want pong", op)
		}
		if string(pong[2:]) != "ping" {
			t.Errorf("pong payload = %q, want the ping's payload", pong[2:])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the pong")
	}
}

// TestWSReaderReassemblesFragmentedMessages: the server may fragment, and a
// client that returns the first fragment as a whole message decodes a truncated
// JSON document.
func TestWSReaderReassemblesFragmentedMessages(t *testing.T) {
	c, srv := socketPair(t)

	go func() {
		_, _ = srv.Write([]byte{opcodeBinary, 0x03, 'a', 'b', 'c'})         // no FIN //stylegate:ignore writes the unterminated-frame fixture on purpose — the missing FIN is the case
		_, _ = srv.Write([]byte{0x80 | opcodeContinuation, 0x02, 'd', 'e'}) // FIN //stylegate:ignore writes the FIN continuation of that same fixture frame
	}()
	c.SetReadDeadline(2 * time.Second)
	msg, err := c.readMessage()
	if err != nil {
		t.Fatalf("readMessage: %v", err)
	}
	if string(msg) != "abcde" {
		t.Fatalf("message = %q, want the reassembled fragments", msg)
	}
}

// TestWSCloseFrameIsReportedWithThePeersReason: a peer ending the conversation
// mid-stream is a RESULT (quota exhausted, credential expired), and the reason
// must reach the caller rather than arriving as "read: connection reset".
func TestWSCloseFrameIsReportedWithThePeersReason(t *testing.T) {
	c, srv := socketPair(t)

	reason := "quota exhausted"
	body := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(body, wsCloseNormal)
	copy(body[2:], reason)
	go func() {
		_, _ = srv.Write(append([]byte{finBit | opcodeClose, byte(len(body))}, body...)) //stylegate:ignore writes the canned close frame the fixture sends to the client
	}()

	c.SetReadDeadline(2 * time.Second)
	_, err := c.readMessage()
	if err == nil {
		t.Fatal("a close frame was not reported")
	}
	var closeErr *wsCloseError
	if !asClose(err, &closeErr) {
		t.Fatalf("error %v is not a close error", err)
	}
	if code, gotReason := wsCloseParts(closeErr.Payload); code != wsCloseNormal || gotReason != reason {
		t.Errorf("close parts = (%d, %q), want (%d, %q)", code, gotReason, wsCloseNormal, reason)
	}
	if !strings.Contains(err.Error(), reason) {
		t.Errorf("the peer's reason did not reach the message: %q", err.Error())
	}
}

func asClose(err error, target **wsCloseError) bool {
	var c *wsCloseError
	if e, ok := err.(*wsCloseError); ok {
		c = e
	}
	if c == nil {
		return false
	}
	*target = c
	return true
}

// TestWSAcceptMatchesTheRFCsValue: the accept-hash is the handshake's proof
// that the endpoint really speaks WebSocket, and RFC 6455 §1.3 gives one
// worked example — used here so the constant is pinned by the spec, not by
// this implementation's own output.
func TestWSAcceptMatchesTheRFCsValue(t *testing.T) {
	const key = "dGhlIHNhbXBsZSBub25jZQ=="
	const want = "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got := wsAccept(key); got != want {
		t.Errorf("wsAccept(%q) = %q, want the RFC's %q", key, got, want)
	}
}

// TestWSDialRefusesANonWebSocketScheme: a typo'd scheme must be refused by
// name rather than read as a URL that "looked fine".
func TestWSDialRefusesANonWebSocketScheme(t *testing.T) {
	if _, _, err := wsDial(t.Context(), "https://openspeech.bytedance.com/api/v2/asr", wsDialer{}); err == nil {
		t.Fatal("an https:// endpoint was accepted")
	}
}

// TestWSFrameLengthsUseTheEscapeForms: the 7-bit length has three encodings
// (inline, 126+uint16, 127+uint64) and a writer that only ever uses the inline
// one truncates silently at 125 bytes.
func TestWSFrameLengthsUseTheEscapeForms(t *testing.T) {
	for _, n := range []int{0, 1, 125, 126, 127, 70000} {
		c, srv := socketPair(t)
		got := make(chan int, 1)
		go func() {
			hdr := make([]byte, 2)
			if _, err := io.ReadFull(srv, hdr); err != nil {
				got <- -1
				return
			}
			length := int(hdr[1] & 0x7f)
			switch length {
			case len16Marker:
				ext := make([]byte, 2)
				_, _ = io.ReadFull(srv, ext) //stylegate:ignore reads the client's extended-length frame header from the fake server; the decoded length is asserted right below
				length = int(binary.BigEndian.Uint16(ext))
			case len64Marker:
				ext := make([]byte, 8)
				_, _ = io.ReadFull(srv, ext) //stylegate:ignore reads the extended payload after that header for the same comparison
				length = int(binary.BigEndian.Uint64(ext))
			}
			got <- length
		}()
		if err := c.writeFrame(opcodeBinary, make([]byte, n)); err != nil {
			t.Fatalf("writeFrame(%d): %v", n, err)
		}
		if length := <-got; length != n {
			t.Errorf("encoded length = %d, want %d", length, n)
		}
	}
}
