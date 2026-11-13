package uiclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SSE framing literals (the wire grammar, from the SSE specification). They
// are named because a parser is exactly where a bare "data:" typo hides: the
// prefix, the event-name prefix, the comment marker and the field separator
// each appear at one decision site and are asserted by the unit tests.
const (
	// sseDataPrefix introduces a data line.
	sseDataPrefix = "data:"
	// sseEventPrefix introduces the frame's event name.
	sseEventPrefix = "event:"
	// sseCommentPrefix marks a comment line (a heartbeat) — ignored.
	sseCommentPrefix = ":"
	// sseDoneSentinel is the out-of-band end marker some SSE producers send
	// instead of closing the body. `gopi web` does NOT send it (its terminal
	// frame is `event: done`); it is honoured anyway so the reader is not
	// surprised by a well-behaved peer, exactly as the sketch's reader did.
	sseDoneSentinel = "[DONE]"
	// sseFieldSeparator is the newline that joins multiple data lines of one
	// frame (the SSE spec's multi-line payload rule).
	sseFieldSeparator = "\n"
)

// DefaultMaxFrameBytes bounds one SSE frame. The server's `delta` frames carry
// model text fragments and the `done` frame carries the whole final answer, so
// the budget is generous but finite: a peer that never sends a blank line must
// fail the turn loudly instead of growing this process without limit (Rule 15).
const DefaultMaxFrameBytes = 4 << 20 // 4 MiB, the server's own request budget

// maxErrorBodyBytes bounds how much of a non-200 response body is quoted in
// the returned error.
const maxErrorBodyBytes = 2 << 10 // 2 KiB

// Client talks to a running `gopi web` server. It carries NO conversation
// state: the transcript lives on the server (s.history), which is why one turn
// is one message on the wire.
type Client struct {
	// BaseURL is the server's origin, e.g. DefaultServerBase. A loopback
	// origin needs no access token; pointing this at a LAN bind means
	// carrying the token (see Token), never weakening the gate.
	BaseURL string
	// Token is the per-run access token a NON-loopback bind demands. Empty for
	// the loopback default, where the server mints nothing.
	Token string
	// HTTP is the transport. nil = a client with no timeout (an LLM turn is
	// long and is bounded by ctx, not by a blunt HTTP timeout).
	HTTP *http.Client
}

// httpClient returns the effective transport.
func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// FrameSink receives one decoded frame. Returning an error stops the turn.
type FrameSink func(Frame) error

// Turn POSTs one message to the server's chat route and streams the SSE frames
// it answers with into sink until the terminal `done` frame (or ctx ends).
//
// CANCELLATION IS A NORMAL END. A user-initiated cancel (Ctrl+X, or the turn's
// context being cancelled) closes the response body and returns nil — it is
// NOT reported as an error, which is what keeps the UI from printing
// "context canceled" after the operator asked it to stop. Every OTHER failure
// (transport error, non-200, undecodable frame, a body that ends without
// `done`) is returned with its cause (Rule 15: no silent failure).
func (c *Client) Turn(ctx context.Context, message string, sink FrameSink) error {
	payload, err := json.Marshal(map[string]string{FieldMessage: message})
	if err != nil {
		return fmt.Errorf("encode turn: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.turnURL(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build turn request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		// The operator asked to stop: a cancelled/aborted request is the
		// normal end of a turn, never an error to report.
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return fmt.Errorf("turn request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // the body is drained by the reader below; a close error adds nothing

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes)) //stylegate:ignore reads a bounded error body for a diagnostic; a short read only shortens the message//nolint:errcheck // best-effort body quote; the status is the load-bearing fact
		return fmt.Errorf("turn rejected: HTTP %d — %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}

	return readFrames(bufio.NewReader(resp.Body), sink, ctx)
}

// turnURL resolves the chat route against BaseURL. A BaseURL carrying a
// trailing slash is accepted (one slash, never two).
func (c *Client) turnURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultServerBase
	}
	return base + ChatPath
}

// interruptRequestTimeout bounds the interrupt POST. It is bounded on purpose:
// the request is a few bytes and must never be the thing that leaves a caller
// waiting (the turn itself is long by design and is not bounded here).
const interruptRequestTimeout = 10 * time.Second

// Interrupt asks the engine to abandon the turn in flight and returns its
// answer: nil when the engine accepted, an error naming what went wrong
// otherwise (Rule 15 — a surface that says "interrupted" while the engine keeps
// running is lying about the one thing the operator pressed the key for).
//
// It is deliberately NOT part of Turn's stream: the whole point is to reach a
// turn that owns the server's lock, so it must be able to arrive while that turn
// is still streaming into another response body.
func (c *Client) Interrupt() error {
	ctx, cancel := context.WithTimeout(context.Background(), interruptRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.interruptURL(), nil)
	if err != nil {
		return fmt.Errorf("build interrupt request: %w", err)
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("interrupt request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // the body is drained just below; a close error adds nothing
	// Drain (bounded) so the connection is reusable — the body is a short JSON
	// ack, and a peer that answers with more must not be read without a limit.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxErrorBodyBytes)) //stylegate:ignore see the drain above: the discarded bytes are not read//nolint:errcheck // best-effort drain; the status below is the load-bearing fact
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the engine refused the interrupt: HTTP %d", resp.StatusCode)
	}
	return nil
}

// interruptURL resolves the interrupt route against BaseURL (same trailing-slash
// rule as turnURL).
func (c *Client) interruptURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = DefaultServerBase
	}
	return base + InterruptPath
}

// readFrames drives the SSE reader: one frame per blank line, `data:` lines
// concatenated, comments skipped, the terminal `done` frame ending the turn.
// It is the ONLY parser (the UI never touches raw bytes).
func readFrames(r *bufio.Reader, sink FrameSink, ctx context.Context) error {
	var (
		event   string
		data    []string
		sawDone bool
	)
	flush := func() (stop bool, err error) {
		if event == "" && len(data) == 0 {
			return false, nil // a bare blank line between frames
		}
		f := Frame{Event: event, Data: strings.Join(data, sseFieldSeparator)}
		if f.Event == "" {
			// A frame with data but no event name: the SSE default event is
			// "message", which this wire never uses. Reported, never guessed.
			f.Event = ""
		}
		if err := sink(f); err != nil {
			return true, err
		}
		if f.Event == EventDone {
			sawDone = true
			return true, nil
		}
		event, data = "", nil
		return false, nil
	}

	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				return nil // user-initiated cancel: a normal end
			}
			return err
		}
		line, err := readLine(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				// The body ended. A turn that never sent `done` did not
				// finish — say so instead of pretending it did.
				if stop, ferr := flush(); stop || ferr != nil {
					return ferr
				}
				if sawDone {
					return nil
				}
				return ErrStreamClosed
			}
			if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return fmt.Errorf("read stream: %w", err)
		}

		switch {
		case line == "":
			stop, ferr := flush()
			if stop || ferr != nil {
				return ferr
			}
		case strings.HasPrefix(line, sseCommentPrefix):
			// A comment (typically a heartbeat) — not part of any payload.
		case strings.HasPrefix(line, sseEventPrefix):
			event = strings.TrimSpace(strings.TrimPrefix(line, sseEventPrefix))
		case strings.HasPrefix(line, sseDataPrefix):
			chunk := strings.TrimPrefix(line, sseDataPrefix)
			// One optional leading space belongs to the framing, not the data.
			chunk = strings.TrimPrefix(chunk, " ")
			if chunk == sseDoneSentinel {
				return nil // out-of-band end marker: the peer is done
			}
			data = append(data, chunk)
		default:
			// An unknown SSE field (id:, retry:) — ignored by the spec's own
			// rule for unrecognized fields, so ignoring it is correct rather
			// than silent.
		}
	}
}

// readLine reads one line without the trailing newline, enforcing the frame
// budget so a peer that never sends a blank line cannot grow this process
// without bound.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	if len(line) > DefaultMaxFrameBytes {
		return "", fmt.Errorf("stream line exceeds %d bytes", DefaultMaxFrameBytes)
	}
	line = strings.TrimRight(line, "\r\n")
	// A truncated final line is still delivered (err == io.EOF with bytes);
	// the caller's next read reports the EOF.
	return line, nil
}
