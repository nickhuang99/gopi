// Package shot implements the `gopi tool shot` screenshot engine: a zero-dependency
// Chrome DevTools Protocol (CDP) client that drives a local headless Chromium
// to navigate a URL and capture a PNG screenshot.
//
// Design (devdoc/shot-design.md, route c): CDP is HTTP (/json/list returns the
// WebSocket debugger URL) plus JSON messages over a WebSocket. Both halves are
// small enough to implement with the standard library only — the WebSocket
// handshake is a SHA-1 keyed exchange (crypto/sha1, per RFC 6455 §4.2.2) and
// client→server frames MUST be masked. No chromedp/rod dependency (Rule 10.7:
// ZERO-DEP default; the gopi tree stays stdlib-only).
//
// Verified end-to-end on HP with playwright's chromium-1228 cache
// (Chrome for Testing 149.0.7827.55): the raw-protocol probe captured a
// 1440x900 PNG that vision_analyze read back correctly.
package shot

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// wsHandshakeTimeout bounds the WebSocket upgrade exchange.
const wsHandshakeTimeout = 10 * time.Second

// wsGUID is the fixed WebSocket handshake GUID from RFC 6455 §1.3.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// frame opcodes used by CDP (text frames only).
const wsOpcodeText = 0x1

// cdpConn is a minimal RFC 6455 WebSocket client connection speaking JSON
// CDP messages. Client→server frames are always masked (per spec, servers
// MUST reject unmasked client frames); server→client frames are unmasked.
type cdpConn struct {
	conn net.Conn
	br   *bufio.Reader
}

// dialWebSocket opens a WebSocket client connection to wsURL (ws://host:port/path).
func dialWebSocket(wsURL string) (*cdpConn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("shot: bad websocket url %q: %w", wsURL, err)
	}
	hostPort := u.Host
	if !strings.Contains(hostPort, ":") {
		hostPort += ":80"
	}
	netConn, err := net.DialTimeout("tcp", hostPort, wsHandshakeTimeout)
	if err != nil {
		return nil, fmt.Errorf("shot: dial %s: %w", hostPort, err)
	}
	if err := netConn.SetDeadline(time.Now().Add(wsHandshakeTimeout)); err != nil {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, err
	}
	// RFC 6455 §4.1: random 16-byte Sec-WebSocket-Key, base64'd.
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	req := "GET " + u.RequestURI() + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := netConn.Write([]byte(req)); err != nil {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, fmt.Errorf("shot: handshake write: %w", err)
	}
	br := bufio.NewReader(netConn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, fmt.Errorf("shot: handshake read: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, fmt.Errorf("shot: websocket handshake got HTTP %d (want 101)", resp.StatusCode)
	}
	want := base64.StdEncoding.EncodeToString(sha1Sum(key + wsGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != want {
		netConn.Close() //stylegate:ignore socket close after the exchange finished; nothing to recover from
		return nil, errors.New("shot: websocket handshake accept-key mismatch")
	}
	// Handshake done; clear the deadline so CDP calls are unbounded here
	// (the caller's context governs overall lifetime via chrome process kill).
	_ = netConn.SetDeadline(time.Time{}) //stylegate:ignore clearing a deadline on a connection we own; a failure resurfaces on the next read
	return &cdpConn{conn: netConn, br: br}, nil
}

// sha1Sum returns the raw SHA-1 digest of s (helper for the accept key).
func sha1Sum(s string) []byte {
	h := sha1.New()
	io.WriteString(h, s) //nolint:errcheck // sha1 never fails on Write
	return h.Sum(nil)
}

// writeTextFrame sends one masked text frame carrying b (RFC 6455 §5.3).
func (c *cdpConn) writeTextFrame(b []byte) error {
	var hdr []byte
	n := len(b)
	switch {
	case n < 126:
		hdr = []byte{0x80 | wsOpcodeText, 0x80 | byte(n)}
	case n < 1<<16:
		hdr = make([]byte, 4)
		hdr[0] = 0x80 | wsOpcodeText
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:], uint16(n))
	default:
		hdr = make([]byte, 10)
		hdr[0] = 0x80 | wsOpcodeText
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:], uint64(n))
	}
	// Masking key: 4 random bytes; payload XORed with the key repeating.
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	payload := make([]byte, n)
	for i, v := range b {
		payload[i] = v ^ mask[i%4]
	}
	if _, err := c.conn.Write(hdr); err != nil {
		return err
	}
	if _, err := c.conn.Write(mask); err != nil {
		return err
	}
	_, err := c.conn.Write(payload)
	return err
}

// readFrame reads one frame, returning its opcode and unmasked payload.
// Handles server frames (never masked) plus extended 16/64-bit lengths.
func (c *cdpConn) readFrame() (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return 0, nil, err
	}
	opcode := h[0] & 0x0f
	masked := h[1]&0x80 != 0
	length := uint64(h[1] & 0x7f)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(ext[:])
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

// call sends one CDP command and blocks until its response (id-matched)
// arrives, discarding any interleaved events. It returns the raw "result"
// object of the response.
func (c *cdpConn) call(id int, method string, params any) (json.RawMessage, error) {
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := c.writeTextFrame(req); err != nil {
		return nil, fmt.Errorf("shot: cdp send %s: %w", method, err)
	}
	for {
		opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, fmt.Errorf("shot: cdp recv %s: %w", method, err)
		}
		if opcode != wsOpcodeText {
			continue // ping/close/fragment: not a CDP text message
		}
		var msg struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Method string `json:"method"` // events carry no id
		}
		if err := json.Unmarshal(payload, &msg); err != nil {
			return nil, fmt.Errorf("shot: cdp decode %s: %w", method, err)
		}
		if msg.ID != id {
			continue // an event (no id) or a response to another call
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("shot: cdp %s error: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

// Close tears down the connection.
func (c *cdpConn) Close() error { return c.conn.Close() }

// cdpTarget is one entry of GET /json/list (a debuggable page target).
type cdpTarget struct {
	ID                   string `json:"id"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	Type                 string `json:"type"`
	URL                  string `json:"url"`
}

// Discovery constants (Rule 1): chrome prints its "DevTools listening on ws://…"
// stderr line roughly 100 ms BEFORE it registers the page target, so the FIRST
// GET of /json/list routinely answers an empty (page-less) list on a perfectly
// healthy chrome. Discovery therefore polls instead of trusting one GET.
const (
	// discoveryPollTimeout bounds the page-target discovery poll. The budget is
	// ~20x the measured registration delay, so a healthy chrome always wins.
	discoveryPollTimeout = 2 * time.Second

	// discoveryPollInterval is the /json/list poll cadence while waiting.
	discoveryPollInterval = 50 * time.Millisecond

	// cdpLoopbackHTTPPrefix is the CDP HTTP endpoint shape for a chrome started
	// with --remote-debugging-port=<port> on loopback.
	cdpLoopbackHTTPPrefix = "http://127.0.0.1:"

	// pageTargetType is the CDP target type of a drivable page (the other
	// entries in /json/list are iframes, workers, etc.).
	pageTargetType = "page"
)

// errNoPageTarget is the discovery failure surfaced once the poll budget
// expires without a page target. The text is part of the contract: callers and
// tests match on it.
var errNoPageTarget = errors.New("shot: no page target in /json/list")

// pageTargetWSURL performs ONE discovery GET and returns the WebSocket
// debugger URL of the first page target, or errNoPageTarget when the list
// carries no page target yet (chrome still registering), and the discovery
// error itself when the endpoint is not serving.
func pageTargetWSURL(httpEndpoint string) (string, error) {
	targets, err := listTargets(httpEndpoint)
	if err != nil {
		return "", err
	}
	for _, t := range targets {
		if t.Type == pageTargetType && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}
	return "", errNoPageTarget
}

// waitPageTarget polls the CDP discovery endpoint until a page target appears,
// bounded by discoveryPollTimeout: both a not-yet-serving endpoint and a served
// but page-less list are retried (the registration race). When the budget
// expires the LAST failure is returned — errNoPageTarget for a list that served
// without a page target, otherwise the discovery error — so the caller's error
// text is unchanged for the pre-existing failure modes.
func waitPageTarget(httpEndpoint string) (string, error) {
	deadline := time.Now().Add(discoveryPollTimeout)
	for {
		wsURL, err := pageTargetWSURL(httpEndpoint)
		if err == nil {
			return wsURL, nil
		}
		if time.Now().After(deadline) {
			return "", err
		}
		time.Sleep(discoveryPollInterval)
	}
}

// listTargets fetches the CDP HTTP discovery endpoint and returns page targets.
func listTargets(httpEndpoint string) ([]cdpTarget, error) {
	client := &http.Client{Timeout: wsHandshakeTimeout}
	resp, err := client.Get(httpEndpoint + "/json/list")
	if err != nil {
		return nil, fmt.Errorf("shot: cdp discovery %s: %w", httpEndpoint, err)
	}
	defer resp.Body.Close()
	var targets []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return nil, fmt.Errorf("shot: cdp discovery decode: %w", err)
	}
	return targets, nil
}
