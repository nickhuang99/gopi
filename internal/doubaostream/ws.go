// WebSocket client — the transport of the v2 streaming ASR channel,
// hand-written on the standard library (net + crypto/tls + crypto/sha1 +
// encoding/base64 + encoding/binary + crypto/rand).
//
// WHY HAND-WRITTEN: gopi's binary carries ONE third-party module
// (modernc.org/sqlite) on purpose, and the two ways to get a websocket here
// are both dependencies — github.com/gorilla/websocket (the official demo's
// choice) or golang.org/x/net/websocket. The protocol the ASR endpoint needs
// is the small core of RFC 6455: an HTTP/1.1 upgrade, then frames. That is
// what this file implements, and NOTHING else — no compression extension, no
// redirects, no cookie jar, no reconnection. A client for a feature is not a
// general library, and every extra generality is a line that can be wrong
// without a caller.
//
// WHAT IS DELIBERATELY ABSENT: permessage-deflate (the ASR payload is gzipped
// BY THE APPLICATION PROTOCOL, so the transport-level extension would be a
// second compression stage that nothing asked for), fragmentation of OUR
// writes (the largest frame we send is one 200 ms audio packet, far below any
// size where fragmenting helps), and client-side ping (the SERVER pings, and
// answering with a Pong is what keeps the connection alive — see readMessage).
package doubaostream

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// RFC 6455 wire constants. Named because they are the protocol's vocabulary,
// not tunables (Rule 1/13).
const (
	// wsVersion is the only version RFC 6455 defines.
	wsVersion = "13"
	// wsGUID is the RFC 6455 §1.3 magic value for the accept-hash.
	wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

	// opcodeContinuation / opcodeText / opcodeBinary / opcodeClose /
	// opcodePing / opcodePong are the frame opcodes (RFC 6455 §5.2).
	opcodeContinuation = 0x0
	opcodeText         = 0x1
	opcodeBinary       = 0x2
	opcodeClose        = 0x8
	opcodePing         = 0x9
	opcodePong         = 0xA

	// finBit is the FIN flag of the first frame byte; maskBit is the MASK flag
	// of the second (CLIENT → SERVER frames MUST set it, RFC 6455 §5.3 — a
	// client that forgets is closed by the server with "incorrect mask flag",
	// which is the single easiest way to misread this protocol as a business
	// error).
	finBit  = 0x80
	maskBit = 0x80

	// The 7-bit length needs two escape values: 126 means "the next 2 bytes
	// are the length", 127 means "the next 8 bytes are the length".
	len16Marker = 126
	len64Marker = 127

	// Frame header sizes, in bytes: the two fixed bytes, the two possible
	// length escapes, and the mandatory client mask key.
	wsFixedHeaderBytes = 2
	wsLen16Bytes       = 2
	wsLen64Bytes       = 8
	wsMaskKeyBytes     = 4
	// wsMaxHeaderBytes is the longest a frame header can be: the fixed two
	// bytes, the 8-byte length escape, and the 4-byte mask key. It sizes the
	// buffer readFrame resolves the WHOLE header into, so a header can never be
	// half-read across two io.ReadFull calls for a reason of our own making.
	wsMaxHeaderBytes = wsFixedHeaderBytes + wsLen64Bytes + wsMaskKeyBytes

	// The close-code + reason a well-behaved client sends. 1000 = normal
	// closure (RFC 6455 §7.4.1).
	wsCloseNormal      = 1000
	wsClosePayloadSize = 2
)

// wsSchemeTLS / wsSchemePlain are the two URL schemes accepted; anything else
// is refused by name rather than guessed at.
const (
	wsSchemeTLS   = "wss"
	wsSchemePlain = "ws"
	wsTLSPort     = "443"
	wsPlainPort   = "80"
)

// wsConn is one upgraded connection. It is NOT safe for concurrent use as a
// whole: the session reads from one goroutine and writes from another, and the
// socket directions are independent — the READ side's own state (the deadline
// the frame read is armed with, and the byte count of the frame in flight) is
// what two goroutines actually share, and it is guarded below.
//
// THE READ DEADLINE IS ONE PIECE OF STATE WITH ONE WRITER AND ONE READER, so it
// is published rather than shared: SetReadDeadline states what the NEXT FRAME
// should be armed with, and readFrame takes that value at the frame BOUNDARY —
// the only instant at which the framing state is whole. Before this, the frame
// reader and the session's write side (Finish's phase change) both wrote the
// same duration, which is a data race with a consequence: a deadline landing
// between two io.ReadFull calls is the unrecoverable desync ErrFrameDesync
// reports.
type wsConn struct {
	conn net.Conn
	br   *bufio.Reader
	// writeTimeout bounds one frame write. It is written where the write
	// deadline is configured (Open, Close) — the write side's own goroutine —
	// and read by writeFrame; both are the ONE write side the session's calling
	// contract names, so it needs no lock of its own.
	writeTimeout time.Duration

	// readTimeout is what the NEXT frame read is armed with: 0 means NO
	// deadline, which is the audio-flow phase's setting (Session.readLoop passes
	// sessionFlowReadTimeout = 0 to Result.readOne). It is written under readMu
	// by SetReadDeadline and read under readMu at every frame boundary, so the
	// write side can state the phase's budget without ever reaching into a frame
	// the reader is in the middle of.
	readTimeout time.Duration

	// readMu guards the three fields below, and is the ONE lock every touch of
	// the socket's READ deadline passes through (armDeadline). It is held only
	// for the arming and the copying — NEVER across an io.ReadFull — so a read
	// blocked on the socket cannot hold it (a Close that needs to release that
	// read must not wait on this lock).
	readMu sync.Mutex
	// frameBytesIn is how many bytes of the CURRENT frame readFrame has already
	// consumed. It is written by the frame reader and read by the caller that
	// reports a failure (Result.readOne), so it is guarded by the same lock: the
	// race detector cannot see "this happens to be the reader's own goroutine"
	// and must not have to.
	frameBytesIn int
}

// wsDialer carries the knobs wsDial reads (all optional; a zero value is the
// shipped configuration).
type wsDialer struct {
	// header is the upgrade request's header — this is where the ASR
	// credential travels (Authorization: Bearer;<token>).
	header http.Header
	// timeout bounds the TCP connect AND the TLS handshake.
	timeout time.Duration
	// handshakeHeader receives the UPGRADE RESPONSE's header: the vendor's log
	// id (X-Tt-Logid) is only present there, and it is the one identifier the
	// vendor's support asks for when a request misbehaves.
	handshakeHeader http.Header
}

// wsDial performs the HTTP/1.1 upgrade. It returns the connection plus the
// handshake response (whose header carries the vendor log id).
//
// FAILURES ARE LOUD AND TYPED (Rule 15): a status other than 101, a missing or
// mismatched Sec-WebSocket-Accept, a URL whose scheme is neither ws nor wss —
// each is its own error naming what was seen.
func wsDial(ctx context.Context, rawURL string, d wsDialer) (*wsConn, *http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, fmt.Errorf("endpoint %q is not a URL: %w", rawURL, err)
	}
	if u.Scheme != wsSchemeTLS && u.Scheme != wsSchemePlain {
		return nil, nil, fmt.Errorf("endpoint %q must be %s:// or %s://", rawURL, wsSchemeTLS, wsSchemePlain)
	}
	host := u.Host
	if u.Port() == "" {
		port := wsPlainPort
		if u.Scheme == wsSchemeTLS {
			port = wsTLSPort
		}
		host = net.JoinHostPort(u.Hostname(), port)
	}

	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, fmt.Errorf("connect %s: %w", host, err)
	}
	if u.Scheme == wsSchemeTLS {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			discardConn(conn)
			return nil, nil, fmt.Errorf("TLS handshake with %s: %w", u.Hostname(), err)
		}
		conn = tlsConn
	}

	key, err := wsNewKey()
	if err != nil {
		discardConn(conn)
		return nil, nil, err
	}
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        &url.URL{Path: u.Path, RawQuery: u.RawQuery},
		Host:       u.Host,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
	}
	for k, vs := range d.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	// The upgrade header is OURS to write: a caller cannot accidentally replace
	// a handshake field with a business one (Rule 13 — one writer per value).
	req.Header.Set(headerUpgradeName, headerUpgradeValue)
	req.Header.Set(headerConnectionName, headerConnectionValue)
	req.Header.Set(headerSecWebSocketKey, key)
	req.Header.Set(headerSecWebSocketVersion, wsVersion)

	if d.timeout > 0 {
		_ = conn.SetWriteDeadline(time.Now().Add(d.timeout)) //stylegate:ignore arming the upgrade write deadline on a freshly dialled socket cannot fail on a live net.Conn; the req.Write below reports the real failure
	}
	if err := req.Write(conn); err != nil {
		discardConn(conn)
		return nil, nil, fmt.Errorf("write upgrade request to %s: %w", host, err)
	}

	br := bufio.NewReader(conn)
	if d.timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(d.timeout)) //stylegate:ignore same for the upgrade read deadline; http.ReadResponse below is where a dead socket surfaces
	}
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		discardConn(conn)
		return nil, nil, fmt.Errorf("read upgrade response from %s: %w", host, err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body := readSmallBody(resp.Body)
		discardConn(conn)
		return nil, resp, fmt.Errorf("endpoint %s refused the WebSocket upgrade: %s%s",
			rawURL, resp.Status, body)
	}
	if want := wsAccept(key); !strings.EqualFold(resp.Header.Get(headerSecWebSocketAccept), want) {
		discardConn(conn)
		return nil, resp, fmt.Errorf("endpoint %s answered %s but its %s does not match the key sent (got %q, want %q)",
			rawURL, resp.Status, headerSecWebSocketAccept,
			resp.Header.Get(headerSecWebSocketAccept), want)
	}
	return &wsConn{conn: conn, br: br, readTimeout: timeout}, resp, nil
}

// Close sends a normal-closure frame and closes the socket. A close-frame
// write failure is not reported: the socket is being torn down anyway, and the
// caller's own error (if any) is the one worth keeping.
func (c *wsConn) Close() error {
	payload := make([]byte, wsClosePayloadSize)
	binary.BigEndian.PutUint16(payload, wsCloseNormal)
	c.writeTimeout = defaultCloseTimeout
	_ = c.writeFrame(opcodeClose, payload) //stylegate:ignore the close frame is best-effort: Close's contract is to return the SOCKET's close error, and the peer may already be gone
	return c.conn.Close()
}

// SetReadDeadline states the deadline the NEXT FRAME read is armed with. Every
// read in this file is bounded: an unbounded read on a vendor socket turns "the
// service is not answering" into "gopi is stuck", which is the failure mode the
// whole streaming arm must not have.
//
// A ZERO (OR NEGATIVE) DURATION MEANS "NO DEADLINE" — and that is not the same
// as arming a default. It is the audio-flow phase's setting (see
// Session.readLoop), and it has to CLEAR the socket's deadline rather than
// merely forget it: a deadline left standing from an earlier phase fires at an
// arbitrary moment, and a deadline that fires inside a frame is exactly the
// unrecoverable desync ErrFrameDesync reports. So the caller that wants no
// deadline gets none, and the socket agrees.
//
// IT IS PUBLISHED, NOT APPLIED TO THE FRAME IN FLIGHT. The value is stored, and
// the reader arms it at the frame BOUNDARY — a deadline armed in the middle of a
// frame is a corrupted stream, not a setting, and the boundary is the one
// instant where "which deadline is this frame read under?" is a question with a
// safe answer. The caller that flips it (Session.Finish, on the write side)
// therefore reaches the reader at its next frame, which is why the arm below can
// be stated under the same lock the reader copies under without either side ever
// waiting on a socket read (Rule 15: two owners of one value is a defect; one
// owner and one publisher is a design).
func (c *wsConn) SetReadDeadline(d time.Duration) {
	if d < 0 {
		d = defaultReadTimeout
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.readTimeout = d
	c.armDeadline(d)
}

// armDeadline is the ONE place the socket's read deadline is touched, and the
// caller must hold readMu.
//
// The socket's deadline is set in BOTH directions: a positive duration arms it
// for the next read, and a zero (or negative) duration CLEARS it. A deadline
// nobody clears is a deadline that fires.
func (c *wsConn) armDeadline(d time.Duration) {
	_ = c.conn.SetReadDeadline(time.Time{}) //stylegate:ignore clearing the read deadline cannot fail on a live socket; a cleared deadline is the precondition for the positive arm below
	if d > 0 {
		_ = c.conn.SetReadDeadline(time.Now().Add(d)) //stylegate:ignore arming the read deadline cannot fail on a live socket — armDeadline has no error channel and the read that follows reports
	}
}

// readDeadlineForFrame returns the budget the frame about to be read is armed
// with, and arms the socket with it — ONE critical section, so the value the
// reader takes is never a value a publisher is part-way through changing.
func (c *wsConn) readDeadlineForFrame() {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.armDeadline(c.readTimeout)
}

// setFrameBytesIn publishes the current frame's consumed-byte count for the
// caller that reports a read failure (see frameBytesIn).
func (c *wsConn) setFrameBytesIn(n int) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.frameBytesIn = n
}

// addFrameBytesIn adds the bytes one io.ReadFull consumed to the current
// frame's count.
func (c *wsConn) addFrameBytesIn(delta int) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	c.frameBytesIn += delta
}

// frameBytesRead reports how many bytes of the CURRENT frame the frame read has
// consumed — the exact answer (io.ReadFull, no guessing) to "did the last
// failure land at a frame boundary?".
func (c *wsConn) frameBytesRead() int {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	return c.frameBytesIn
}

// readFrame reads ONE frame: its opcode, its FIN flag and its payload
// (unmasked when the server masked it, which it may do but normally does not).
//
// EVERY BYTE IS COUNTED BEFORE IT IS READ. io.ReadFull can return having taken
// part of what it asked for, so an error here says nothing by itself about
// whether the frame boundary survived: the count is what answers that, and a
// caller may consult it after ANY failure (frameBytesIn — see errFrameDesync).
//
// A frame is read in AT MOST TWO io.ReadFull calls: the header (the fixed two
// bytes plus whatever the length escape and the mask key need, resolved without
// reading past the header) and the payload. That is a deliberate part of the
// boundary accounting: the longer the header is, the more of it a partial read
// can leave consumed, and the payload is the only part at which a resumed frame
// could even in principle be worth reasoning about — it never is, which is what
// errFrameDesync reports instead.
//
// THE DEADLINE IS RE-ARMED PER FRAME — never per read call — so a message
// assembled from many frames cannot be starved by one slow frame's budget. It is
// armed from the value the caller last published (readTimeout) at the boundary,
// which is the one instant where both facts hold at once: the previous frame is
// whole, and this one has consumed nothing. The audio-flow phase's budget is 0,
// so the socket's deadline is CLEARED at every frame boundary during it —
// because a deadline that fires inside a frame is the one interruption this
// reader cannot resume from.
func (c *wsConn) readFrame() (opcode byte, fin bool, payload []byte, err error) {
	c.readDeadlineForFrame()
	c.setFrameBytesIn(0)
	var hdr [wsMaxHeaderBytes]byte
	n, err := io.ReadFull(c.br, hdr[:wsFixedHeaderBytes])
	c.setFrameBytesIn(n)
	if err != nil {
		return 0, false, nil, err
	}
	fin = hdr[0]&finBit != 0
	opcode = hdr[0] & 0x0f
	masked := hdr[1]&maskBit != 0
	length := uint64(hdr[1] & 0x7f)

	hdrLen := wsFixedHeaderBytes
	switch length {
	case len16Marker:
		hdrLen += wsLen16Bytes
	case len64Marker:
		hdrLen += wsLen64Bytes
	}
	if masked {
		hdrLen += wsMaskKeyBytes
	}
	if hdrLen > wsFixedHeaderBytes {
		n, err := io.ReadFull(c.br, hdr[wsFixedHeaderBytes:hdrLen])
		c.addFrameBytesIn(n)
		if err != nil {
			return 0, false, nil, err
		}
	}
	switch length {
	case len16Marker:
		length = uint64(binary.BigEndian.Uint16(hdr[wsFixedHeaderBytes : wsFixedHeaderBytes+wsLen16Bytes]))
	case len64Marker:
		length = uint64(binary.BigEndian.Uint64(hdr[wsFixedHeaderBytes : wsFixedHeaderBytes+wsLen64Bytes]))
	}
	if length > maxFrameBytes {
		return 0, false, nil, fmt.Errorf("frame of %d bytes exceeds the %d byte limit", length, maxFrameBytes)
	}
	var maskKey [wsMaskKeyBytes]byte
	if masked {
		copy(maskKey[:], hdr[hdrLen-wsMaskKeyBytes:hdrLen])
	}
	payload = make([]byte, length)
	if length > 0 {
		n, err := io.ReadFull(c.br, payload)
		c.addFrameBytesIn(n)
		if err != nil {
			return 0, false, nil, err
		}
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%wsMaskKeyBytes]
		}
	}
	return opcode, fin, payload, nil
}

// readMessage reads one DATA message, transparently handling the control
// frames interleaved with it — which is not optional here: the ASR server
// sends WebSocket Pings (op 0x9) on the fresh connection, and a client that
// treats a control frame as business bytes parses the ping payload as JSON.
//
// Continuation frames are reassembled (the server may fragment; we do not).
func (c *wsConn) readMessage() ([]byte, error) {
	var (
		msg     []byte
		started bool
	)
	for {
		// The deadline is re-armed per frame, so a message assembled from many
		// frames cannot be starved by one slow frame's budget.
		opcode, fin, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}
		switch opcode {
		case opcodePing:
			if err := c.writeFrame(opcodePong, payload); err != nil {
				return nil, fmt.Errorf("answer server ping: %w", err)
			}
			continue
		case opcodePong:
			continue
		case opcodeClose:
			return nil, &wsCloseError{Payload: payload}
		case opcodeBinary, opcodeText:
			msg = append(msg, payload...)
			started = true
		case opcodeContinuation:
			if !started {
				return nil, fmt.Errorf("continuation frame before any data frame (RFC 6455 §5.4)")
			}
			msg = append(msg, payload...)
		default:
			return nil, fmt.Errorf("unknown WebSocket opcode 0x%x", opcode)
		}
		if fin {
			return msg, nil
		}
	}
}

// writeFrame writes one masked frame (CLIENT → SERVER frames MUST be masked).
func (c *wsConn) writeFrame(opcode byte, payload []byte) error {
	if c.writeTimeout > 0 {
		_ = c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)) //stylegate:ignore arming the write deadline cannot fail on a live socket; the frame write itself returns the failure
	}
	header := make([]byte, 0, wsFixedHeaderBytes+wsLen64Bytes+wsMaskKeyBytes)
	header = append(header, finBit|opcode)
	switch n := len(payload); {
	case n < len16Marker:
		header = append(header, maskBit|byte(n))
	case n <= 0xFFFF:
		header = append(header, maskBit|len16Marker, byte(n>>8), byte(n))
	default:
		header = append(header, maskBit|len64Marker)
		var ext [wsLen64Bytes]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	var key [wsMaskKeyBytes]byte
	if _, err := rand.Read(key[:]); err != nil {
		return fmt.Errorf("mask key: %w", err)
	}
	header = append(header, key[:]...)

	masked := make([]byte, len(payload))
	for i := range payload {
		masked[i] = payload[i] ^ key[i%wsMaskKeyBytes]
	}
	if _, err := c.conn.Write(header); err != nil {
		return fmt.Errorf("write frame header: %w", err)
	}
	if len(masked) > 0 {
		if _, err := c.conn.Write(masked); err != nil {
			return fmt.Errorf("write frame payload: %w", err)
		}
	}
	return nil
}

// wsCloseError is a close frame from the server: the peer ending the
// conversation is a RESULT, and it carries the vendor's own reason — dropping
// it would leave every mid-stream disconnect as "read: connection reset".
type wsCloseError struct {
	Payload []byte
}

func (e *wsCloseError) Error() string {
	code, reason := wsCloseParts(e.Payload)
	if reason == "" {
		return fmt.Sprintf("server closed the WebSocket (code %d)", code)
	}
	return fmt.Sprintf("server closed the WebSocket (code %d): %s", code, reason)
}

// wsCloseParts splits a close payload into its code and its reason text.
func wsCloseParts(payload []byte) (code int, reason string) {
	if len(payload) < wsClosePayloadSize {
		return 0, strings.TrimSpace(string(payload))
	}
	return int(binary.BigEndian.Uint16(payload[:wsClosePayloadSize])),
		strings.TrimSpace(string(payload[wsClosePayloadSize:]))
}

// wsNewKey returns the base64 16-byte nonce the handshake requires.
func wsNewKey() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("websocket key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(nonce[:]), nil
}

// wsAccept computes the response hash for a key (RFC 6455 §4.2.2).
func wsAccept(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// readSmallBody keeps a short prefix of a refusal body for the message: enough
// to say WHAT the endpoint said, bounded so a misdirected endpoint cannot make
// this client read a megabyte of HTML into an error string.
func readSmallBody(body io.ReadCloser) string {
	if body == nil {
		return ""
	}
	defer body.Close()
	buf := make([]byte, errBodyBytes)
	n, _ := io.ReadFull(io.LimitReader(body, errBodyBytes), buf) //stylegate:ignore a truncated refusal body is still worth quoting; the read error would only repeat the status line
	if n == 0 {
		return ""
	}
	return ": " + strings.TrimSpace(string(buf[:n]))
}

// discardConn closes a connection a failed handshake left behind, keeping the
// handshake error as the one the caller sees (Rule 15: the failure that
// matters is never replaced by a cleanup failure).
func discardConn(c net.Conn) {
	if c == nil {
		return
	}
	_ = c.Close() //stylegate:ignore discardConn's whole purpose is to drop a socket whose handshake already failed; that failure is the one the caller sees
}
