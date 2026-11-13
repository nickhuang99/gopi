package doubaostream

// ws_exports.go — THE EXPORTED WebSocket TRANSPORT, plus the one audio decoder
// a second channel needs.
//
// WHY THIS FILE EXISTS. RFC 6455 framing is implemented ONCE in this package
// (ws.go), hand-written on the standard library because gopi's binary carries
// one third-party module on purpose (see ws.go's header, and the checked
// promise in zerodep_test.go). A SECOND channel that speaks a DIFFERENT
// application protocol over the SAME transport — the v3 SAUC binary protocol
// (internal/doubaosauc) — must therefore not carry a second handshake, a second
// masked-frame writer or a second message reassembler. That is the copy Rule
// 13/14 exist to stop, and a frame bug in a copy is invisible to the suite that
// tests the original (measured on this channel: stepping a BYTE buffer by a
// SAMPLE count halved every packet's audio and doubled the stream's length).
//
// WHAT IS EXPORTED, AND WHAT IS NOT. Only the TRANSPORT — dial (with the
// caller's own upgrade headers, because each channel carries its own credential
// there), write one binary message, read one message, state a read deadline,
// close, and the log id the handshake response carried. The v2 channel's
// APPLICATION protocol (its JSON envelope, its packet cadence, its partial
// handling, its ack ordering) stays unexported and untouched: a v3 caller has
// none of those concepts, and exporting them would invite a second user of a
// protocol that has exactly one.
//
// THE HEADER ORDER IS CARRIED, NOT NORMALISED, which is why WsHeader is a SLICE
// rather than an http.Header: this vendor's endpoint was measured to answer the
// SAME credentials differently depending on their SHAPE (X-Api-App-Key +
// X-Api-Access-Key → 101 Switching Protocols; the single X-Api-Key UUID → 403
// "requested resource not granted"), and a caller debugging that must be able
// to state the exact fields it sent. Go's http.Header plus Write sorts by key,
// which is reproducible but not what the caller wrote; a slice says what was
// written.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// WsCloseCodeNormal is RFC 6455 §7.4.1's normal-closure code. It is exported so
// a caller can tell "the peer said goodbye" (a result) from "the peer vanished"
// (a failure) without importing this package's internals or matching a
// message's phrasing.
const WsCloseCodeNormal = wsCloseNormal

// WsHeader is one upgrade-request header field, in the order the caller wrote
// it. Set (not Add) semantics: a name repeated in the slice keeps its FIRST
// spelling and position, because http.Header.Set replaces in place.
type WsHeader struct {
	// Name / Value are the field name and its value, verbatim.
	Name  string
	Value string
}

// WsDialOptions carries the two things a caller owns about the handshake: the
// headers its own protocol needs, and the bound on connect + TLS + upgrade.
type WsDialOptions struct {
	// Header is the caller's own header list. The RFC 6455 fields (Upgrade,
	// Connection, Sec-WebSocket-Key, Sec-WebSocket-Version) are written by the
	// dialer and CANNOT be replaced from here (Rule 13 — one writer per value).
	Header []WsHeader
	// Timeout bounds the TCP connect, the TLS handshake, the upgrade write and
	// the upgrade read. Zero means defaultDialTimeout.
	Timeout time.Duration
}

// WsConn is one upgraded connection, plus the handshake response so the
// vendor's log id (present ONLY there, never in a later frame) stays reachable
// even when the upgrade is REFUSED.
type WsConn struct {
	c    *wsConn
	resp *http.Response
}

// WsDial performs the HTTP/1.1 upgrade and returns the connection. The errors
// are the transport's own and are LOUD (Rule 15): a status other than 101 names
// the status, the vendor's reason body and — when the service got far enough to
// answer — the log id.
func WsDial(ctx context.Context, rawURL string, opts WsDialOptions) (*WsConn, error) {
	h := http.Header{}
	for _, kv := range opts.Header {
		h.Set(kv.Name, kv.Value)
	}
	respHeader := http.Header{}
	c, resp, err := wsDial(ctx, rawURL, wsDialer{
		header:          h,
		timeout:         opts.Timeout,
		handshakeHeader: respHeader,
	})
	if err != nil {
		// The log id is the ONE identifier the vendor's support asks for, and a
		// refused handshake is exactly when it is needed: attach it when the
		// dial got far enough for the service to answer at all.
		if id := responseLogID(resp, respHeader); id != "" {
			return nil, fmt.Errorf("%w (logid %s)", err, id)
		}
		return nil, err
	}
	return &WsConn{c: c, resp: resp}, nil
}

// responseLogID reads the vendor's log id from whichever header the dialer
// filled — the response itself, or the copy passed in through wsDialer.
func responseLogID(resp *http.Response, header http.Header) string {
	if resp != nil {
		if id := resp.Header.Get(logIDHeader); id != "" {
			return id
		}
	}
	return header.Get(logIDHeader)
}

// LogID is the vendor's request id from the upgrade response, or "" when the
// service sent none. It is the only value from that response a caller needs.
func (c *WsConn) LogID() string {
	if c == nil || c.resp == nil {
		return ""
	}
	return c.resp.Header.Get(logIDHeader)
}

// SetReadDeadline states the deadline the NEXT message read is armed with (0 =
// no deadline). It is the transport's own PHASE contract rather than a
// per-read argument: the value is taken at a frame BOUNDARY, because a deadline
// landing between two io.ReadFull calls is the unrecoverable desync
// ErrFrameDesync reports.
func (c *WsConn) SetReadDeadline(d time.Duration) {
	if c == nil || c.c == nil {
		return
	}
	c.c.SetReadDeadline(d)
}

// ReadMessage returns the next message's payload, reassembled across
// continuation frames, answering pings as it goes.
//
// A CLOSE FRAME FROM THE PEER IS AN ERROR TYPE (WsCloseError), never a bare
// io.EOF: the vendor's own goodbye reason is the most useful sentence in the
// exchange — a mid-stream disconnect carrying "quota exceeded" reads very
// differently from one with no reason — and a caller that cannot see it starts
// every incident from zero.
func (c *WsConn) ReadMessage() ([]byte, error) {
	msg, err := c.c.readMessage()
	var closeErr *wsCloseError
	if errors.As(err, &closeErr) {
		code, reason := wsCloseParts(closeErr.Payload)
		return nil, &WsCloseError{Code: code, Reason: reason}
	}
	return msg, err
}

// WsCloseError is the peer ending the conversation, with its own close code and
// reason. Code 0 means the frame carried no payload.
type WsCloseError struct {
	// Code is the RFC 6455 §7.4 close code (WsCloseCodeNormal = a clean
	// goodbye), 0 when the frame carried none.
	Code int
	// Reason is the peer's own text, "" when it sent none.
	Reason string
}

func (e *WsCloseError) Error() string {
	switch {
	case e.Code == 0:
		return "the peer closed the WebSocket (no close code)"
	case e.Reason == "":
		return fmt.Sprintf("the peer closed the WebSocket (code %d)", e.Code)
	}
	return fmt.Sprintf("the peer closed the WebSocket (code %d: %s)", e.Code, e.Reason)
}

// WriteBinary writes one masked binary message.
func (c *WsConn) WriteBinary(payload []byte) error {
	return c.c.writeFrame(opcodeBinary, payload)
}

// Close sends a normal-closure frame and closes the socket.
func (c *WsConn) Close() error { return c.c.Close() }

// ── the audio decoder, exported ────────────────────────────────────────
//
// WHY THIS IS EXPORTED AND FILE I/O IS NOT. Two channels of the same feature
// take the same audio (`gopi tool voice_input <file>`), and each must hand the
// VENDOR the same geometry — 16 kHz, mono, PCM16 — or the service transcribes
// bytes it is misreading, which produces a plausible-looking wrong transcript
// rather than an error. The decoder that does that (RIFF/WAVE chunk walk, tag
// dispatch, downmix, resample) is therefore a SHARED value, not a per-channel
// one. What stays inside this package is what the v2 channel DOES with the
// samples — its pacing, its packet geometry, its partial handling.

// DecodeAudio decodes one audio file into the 16 kHz mono PCM16 samples every
// channel of this feature sends, plus the audio's own duration in milliseconds.
//
// It is the decoder Transcribe uses (`loadAudio`), exported rather than
// duplicated: a second channel with its own RIFF parser is a second opinion
// about a container format, and the two would drift on the first exotic
// `fmt ` chunk (the measured case: an EXTENSIBLE chunk reading its sub-format
// GUID at the wrong offset refuses an ordinary 32-bit float WAV).
//
// The errors name the PATH and the reason; the audio's bytes never appear in
// one.
func DecodeAudio(path string) ([]byte, int, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, 0, fmt.Errorf("read the audio %q: %w", path, err)
	}
	return loadAudio(Options{AudioFile: path})
}
