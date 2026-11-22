package deepseek

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// HTTP transport tuning constants.
	defaultMaxIdleConns        = 32
	defaultMaxIdleConnsPerHost = 16
	defaultIdleConnTimeout     = 90 * time.Second
	defaultTLSHandshakeTimeout = 10 * time.Second

	// defaultResponseHeaderTimeout is the timeout for the first byte of a
	// response. Generous on purpose: prefill of a large prompt is slow
	// (measured ~16s for ~290K tokens, and grows with size) — a short
	// timeout aborts and RETRIES the whole request, doubling its cost.
	defaultResponseHeaderTimeout = 120 * time.Second
	defaultDialTimeout           = 30 * time.Second
	defaultDialKeepAlive         = 30 * time.Second

	// SSE parser buffer size for coalescing micro-chunks.
	defaultSSEReadBufSize = 4096
)

// Client is an HTTP client for the DeepSeek API.
type Client struct {
	BaseURL     string
	APIKey      string
	HTTPClient  *http.Client
	RetryConfig RetryConfig

	// MessagesPath, when non-empty, is the full Claude Messages API request
	// path appended verbatim to BaseURL (see Config.MessagesPath /
	// claudeMessagesURL). Empty means the legacy suffix logic applies.
	MessagesPath string

	// VisionModel is the resolved multimodal model (env > settings > default),
	// captured at construction by NewVisionClient. VisionAnalyze uses it so a
	// settings-only vision_model actually takes effect; the nil-resolve fallback
	// inside VisionAnalyze keeps directly-constructed clients working.
	VisionModel string

	// RawLog is an optional callback for raw HTTP request/response debugging.
	// It receives the record's DIRECTION TAG (rawlogfmt.TagRequest /
	// TagProbeRequest / TagResponse / TagChunk / TagError), an ANNOTATION naming a
	// guard/fail-fast ladder send ("" for the agent's accepted call), and the raw
	// payload bytes.
	//
	// THE TAG REPLACES THE OLD MARKER-LINE STRING, and that is the whole point of
	// the signature: the direction used to arrive as literal marker TEXT
	// ("→ REQUEST\n") that the logger then wrote into the file, so a payload
	// containing the same text was indistinguishable from a real direction. Now the
	// direction is a constant in the record's HEADER (a length-prefixed line) and
	// the payload is never scanned for text. The callback therefore no longer does
	// any formatting: internal/agent's RawLogger.LogFunc decides the framing (a new
	// log is length-prefixed; a log being continued keeps its own).
	RawLog func(tag, annotation string, data []byte)

	// LastErrDetail captures the most recent failed HTTP round-trip for the
	// crash-dump pipeline (session_end.error + <session-id>.dump.json).
	// Written by the non-streaming and streaming send paths on every
	// non-200 (and transport error); read by the agent loop when a round
	// fails terminally. NOT a new tool (Rule 3) — a diagnostics side-channel
	// on the existing client. Zero value = no failure observed yet.
	LastErrDetail LastHTTPFailure
}

// newHTTPClient builds the shared HTTP transport for DeepSeek API clients.
// Tuned for low-latency streaming: connection pooling, TCP_NODELAY, and a
// generous response-header timeout so SSE streams are never cut early.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:          defaultMaxIdleConns,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ResponseHeaderTimeout: defaultResponseHeaderTimeout,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{
					Timeout:   defaultDialTimeout,
					KeepAlive: defaultDialKeepAlive,
				}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				if tcpConn, ok := conn.(*net.TCPConn); ok {
					tcpConn.SetNoDelay(true)
				}
				return conn, nil
			},
		},
	}
}

// scanSSEEvents reads an SSE stream from r and invokes fn for each complete
// event payload — the raw lines between blank lines, with event:/data:/
// comment prefixes preserved. A trailing partial event (EOF without a
// terminating blank line) is delivered best-effort. It returns the read
// error (nil on clean EOF); a non-nil error means no usable trailing event
// was buffered.
func scanSSEEvents(ctx context.Context, r io.Reader, fn func(event []byte)) error {
	rd := bufio.NewReaderSize(r, defaultSSEReadBufSize)
	buffer := make([]byte, 0, defaultSSEReadBufSize)

	for {
		line, err := rd.ReadBytes('\n')
		if err != nil {
			// EOF or read error — flush any buffered partial event as a
			// best-effort final event, then exit the loop.
			if len(buffer) > 0 {
				if len(line) > 0 {
					buffer = append(buffer, line...)
				}
				fn(buffer)
				return nil
			}
			return err
		}

		buffer = append(buffer, line...)

		// Check for end-of-event (double newline).
		if !bytes.HasSuffix(buffer, []byte("\n\n")) {
			continue
		}

		// We have a complete SSE event in buffer.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fn(bytes.TrimSuffix(buffer, []byte("\n\n")))
		buffer = buffer[:0] // reset, retain array
	}
}
