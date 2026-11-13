// Package testutil provides the shared mock-LLM and sandbox infrastructure
// for the gopi testsuite: a scripted Claude-compatible mock server, session
// JSONL assertion helpers, and a subprocess runner for the real bin/gopi
// binary. Nothing in this package (or in the tests built on it) ever touches
// a real LLM endpoint — the mock is an httptest server and the subprocess
// environment strips every real credential (see MockEnv).
//
// The mock pattern here is the testsuite's copy of the two pre-existing
// in-package mock helpers (cmd/agent/cli_web_test.go textSSE +
// mockStreamingServer, internal/agent/chat_ontest_test.go
// claudeTextStreamFragmented + newTestAgent): an httptest server serving
// Claude Messages API SSE frames, reached by pointing deepseek.Client.BaseURL
// (or the ANTHROPIC_BASE_URL/DEEPSEEK_BASE_URL env var for subprocesses) at
// it. Those originals live in *_test.go files of their own packages and are
// not importable; this package is the shared home for the same pattern.
package testutil

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// ToolCall is one scripted tool_use block the mock streams back to the agent.
type ToolCall struct {
	ID   string                 // e.g. "call_01" — must match the tool_use id
	Name string                 // e.g. "write"
	Args map[string]interface{} // tool input JSON, streamed as input_json_delta
}

// Resp is one scripted mock response, matched to the model request by index
// (the first request gets script[0], the second script[1], …; requests beyond
// the script length repeat the LAST entry — confirmation rounds and nudges
// therefore keep getting a benign text answer and the run terminates).
type Resp struct {
	// Text streams a plain text answer with stop_reason end_turn (a normal
	// no-tool round). Empty Text means no text content block.
	Text string
	// ToolUse streams a single tool_use block with stop_reason tool_use.
	ToolUse *ToolCall
	// Fail makes the mock answer HTTP 400 (non-retryable — the client retries
	// only 429/5xx, see internal/deepseek/retry.go), which the agent surfaces
	// as a hard error. This is the scripted error scenario.
	Fail bool
	// TextDeltaBytes is how many BYTES each streamed text fragment carries when
	// Text is served (0 = TextSSE's legacy two-half split). The renderer's
	// behaviour is a function of the stream's CADENCE — one fragment per
	// content_block_delta — so a caller that needs a measured cadence asks for
	// it here and reads the sizes the stub actually streamed back from
	// TextStreamSizes (the proof is the record, not the parameter).
	TextDeltaBytes int
	// TextDeltaDelay is how long the mock waits between two text fragments
	// (0 = none). It exists for the same reason as TextDeltaBytes: with both at
	// their zero value every frame is one burst and the terminal drains the
	// whole answer in one read, so a caller measuring the LIVE band (a partial
	// paint, not the committed rows) must space the fragments.
	TextDeltaDelay time.Duration
	// Hold makes the mock accept the request, write the SSE headers, then
	// block mid-stream (holding the request open) until ReleaseHold is
	// called. Used to keep an agent run IN-FLIGHT at a deterministic point —
	// e.g. so a test can SIGTERM the gopi subprocess mid-LLM-round-trip and
	// assert the graceful-closeout behavior (session_end written by the
	// runEngine defer) rather than a hard process death.
	Hold bool
}

// HoldSignal is the channel the mock blocks on while a Hold response is
// being served. Close it (or send on it) to release the held request.
type HoldSignal chan struct{}

// MockLLM is a scripted Claude-compatible mock server. It answers every POST
// (the real client POSTs {base}/anthropic/messages — see
// deepseek.Client.claudeMessagesURL) with the scripted Claude SSE stream.
type MockLLM struct {
	t    *testing.T
	srv  *httptest.Server
	mu   sync.Mutex
	reqs int // total requests served (1-based)
	// bodies holds each request body (decoded form); protected by mu.
	bodies [][]byte
	// holdReleases are the per-request release channels for Hold responses,
	// indexed by request (1-based); protected by mu.
	holdReleases []HoldSignal
	// textSizes records, per request (1-based, index i = request i+1), the
	// sizes in bytes of the text fragments actually streamed for that request.
	// It is the RECORD a cadence claim is made from (TextStreamSizes), so a
	// harness reports the sizes the stub wrote rather than the sizes it asked
	// for; protected by mu.
	textSizes [][]int
}

// NewMockLLM starts a scripted mock server. The returned server must be
// closed (Close) before the test ends.
func NewMockLLM(t *testing.T, script ...Resp) *MockLLM {
	t.Helper()
	if len(script) == 0 {
		// Rule 1: a mock with no script would serve nothing — refuse loudly.
		t.Fatal("testutil: NewMockLLM requires at least one scripted Resp")
	}
	m := &MockLLM{t: t}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			m.t.Errorf("mock: expected POST, got %s %s", r.Method, r.URL.Path)
			http.Error(w, "mock: POST only", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		m.mu.Lock()
		m.reqs++
		idx := m.reqs - 1
		m.bodies = append(m.bodies, body)
		m.mu.Unlock()

		resp := script[len(script)-1] // beyond the script: repeat the last entry
		if idx < len(script) {
			resp = script[idx]
		}
		if resp.Hold {
			// Register the release channel for this request BEFORE writing
			// any response bytes, so the test's ReleaseHold is never missed.
			release := make(HoldSignal)
			m.mu.Lock()
			m.holdReleases = append(m.holdReleases, release)
			m.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Block until the test releases the hold (or the client's ctx
			// cancel closes this handler's connection — the write below then
			// errors and the handler returns).
			select {
			case <-release:
			case <-r.Context().Done():
			}
			return
		}
		switch {
		case resp.Fail:
			http.Error(w, "mock: scripted failure (bad request)", http.StatusBadRequest)
		case resp.ToolUse != nil:
			w.Header().Set("Content-Type", "text/event-stream")
			ToolUseSSE(w, *resp.ToolUse)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			sizes := textSSEFor(w, resp)
			m.mu.Lock()
			// Pad up to this request's slot: a ToolUse/Fail round streams no text
			// and must still leave the index aligned with the request number, or
			// TextStreamSizes(q) would answer with another round's sizes.
			for len(m.textSizes) < idx {
				m.textSizes = append(m.textSizes, nil)
			}
			m.textSizes = append(m.textSizes, sizes)
			m.mu.Unlock()
		}
	}))
	return m
}

// textSSEFor serves one scripted text Resp and returns the sizes of the text
// fragments it streamed (nil for the two-half legacy shape, whose halves are
// derived — a caller that wants the sizes of THAT shape reads the wire with
// TextStreamSizes' nil fallback, which reports nothing rather than a guess).
func textSSEFor(w io.Writer, resp Resp) []int {
	if resp.TextDeltaBytes <= 0 && resp.TextDeltaDelay <= 0 {
		TextSSE(w, resp.Text)
		return nil
	}
	return TextSSEStream(w, resp.Text, resp.TextDeltaBytes, resp.TextDeltaDelay)
}

func (m *MockLLM) URL() string { return m.srv.URL }

// Close shuts the mock server down. Any still-held requests are released
// first (httptest.Close blocks until outstanding requests complete — a
// never-released hold would deadlock the shutdown).
func (m *MockLLM) Close() {
	m.mu.Lock()
	for _, r := range m.holdReleases {
		select {
		case <-r:
		default:
			close(r)
		}
	}
	m.mu.Unlock()
	m.srv.Close()
}

// TextStreamSizes returns the sizes in bytes of the text fragments the mock
// actually streamed for request n (1-based) — the cadence of the answer the
// workbench received, as the SERVER saw it. It is nil for a request that
// streamed no text, for a round served by the legacy two-half TextSSE (whose
// halves this record does not claim to know), and for a request the mock never
// served.
//
// It exists so a cadence claim is READ OFF THE STUB instead of restated from
// its own parameters: the harness asks for 1..16-byte deltas and then reports
// the sizes this function returns, so a run whose requests were chunked
// differently cannot quietly report the range it intended.
func (m *MockLLM) TextStreamSizes(n int) []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.textSizes) {
		return nil
	}
	out := m.textSizes[n-1]
	return append([]int(nil), out...)
}

// RequestCount returns how many model requests the mock has served.
func (m *MockLLM) RequestCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reqs
}

// RequestBody returns the n-th request body (1-based) as raw JSON, or "" when
// fewer than n requests arrived. Useful for wire-shape assertions (e.g. the
// /tools toggle must put the toolset on the wire).
func (m *MockLLM) RequestBody(n int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.bodies) {
		return ""
	}
	return string(m.bodies[n-1])
}

// RequestsContaining returns every request body that contains want, in arrival
// order. It is the reading a case needs when the question is WHICH turn carried
// something rather than HOW MANY requests arrived: a run whose turn count is not
// fixed by the fixture (a minted turn, a confirmation round) cannot be read from
// an index, and asserting on an index would be asserting the fixture's shape
// instead of the feature's.
func (m *MockLLM) RequestsContaining(want string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, b := range m.bodies {
		if strings.Contains(string(b), want) {
			out = append(out, string(b))
		}
	}
	return out
}

// HeldCount returns how many Hold responses are currently blocked mid-stream// (waiting for their ReleaseHold). A test waits for this to reach 1 before
// signalling the gopi subprocess, so the SIGTERM lands mid-round-trip.
func (m *MockLLM) HeldCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.holdReleases)
}

// ReleaseHold releases the n-th held request (1-based, in arrival order),
// letting the mock finish (empty) streaming so the held HTTP round-trip
// completes. Safe to call after the client already aborted (the handler
// returns on its r.Context().Done()); idempotent.
func (m *MockLLM) ReleaseHold(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 1 || n > len(m.holdReleases) {
		return
	}
	select {
	case <-m.holdReleases[n-1]:
	default:
		close(m.holdReleases[n-1])
	}
}

// --- Claude Messages API SSE frame writers (the wire format the real client
// parses — see internal/deepseek/claude_client.go claudeWireEvent). ---

// WriteClaudeSSE writes one SSE frame with an event type and JSON payload.
func WriteClaudeSSE(w io.Writer, eventType, payloadJSON string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payloadJSON)
}

// TextSSE streams a Claude-style text answer: message_start →
// content_block_start → two text_delta fragments (so per-fragment streaming
// is exercised) → content_block_stop → message_delta(end_turn) → message_stop.
//
// The two-half split is the LEGACY shape and it is kept byte-for-byte (callers
// rely on it, and it is what the mock has always done). It is also a blind
// spot: no cadence finer than "half the answer at once" has ever reached a
// surface through this stub. A caller that needs a measured cadence uses
// TextSSEChunked.
func TextSSE(w io.Writer, text string) {
	writeClaudeTextStart(w)
	mid := len(text) / 2
	for _, p := range []string{text[:mid], text[mid:]} {
		if p == "" {
			continue
		}
		writeClaudeTextDelta(w, p)
	}
	writeClaudeTextEnd(w)
}

// TextSSEChunked streams the same Claude text answer in fragments of at most
// deltaBytes BYTES (deltaBytes <= 0 falls back to TextSSE's two-half split).
// It returns the sizes in bytes of the fragments it actually wrote, in stream
// order — the ONE proof of the cadence that is not the caller's own parameter:
// a harness that reports "deltas 1..16 were streamed" reports it because the
// stub says so, and can therefore report the sizes it really got rather than
// the ones it asked for.
//
// The cut is RUNE-SAFE: an answer is a string of runes, and a fragment that
// split a multi-byte rune would put half a rune in a JSON string — a wire shape
// no real server emits, and one the client's JSON decoder would reject for a
// reason that has nothing to do with the render. So a fragment carrying a wide
// rune may be WIDER than deltaBytes (never narrower): the returned sizes say
// so, which is exactly the fact a reader of a cadence claim needs.
func TextSSEChunked(w io.Writer, text string, deltaBytes int) []int {
	return TextSSEStream(w, text, deltaBytes, 0)
}

// TextSSEStream is TextSSEChunked with a pace BETWEEN fragments: each fragment
// is flushed to the wire and then delayed by pause, so a client's reads are
// spaced rather than drained in one burst.
//
// The pause is a REAL interval between two frames of one answer — not an
// artificially widened payload, not repeated content, not a second request —
// and it exists because the cadence IS the observation: the live band's
// partial-paint path only has a screen to be wrong on while the answer is
// still arriving, and a socket that already holds every frame delivers them in
// one terminal read. The cost is bounded by the caller (text length ÷
// deltaBytes × pause) and is stated in the caller's own report.
func TextSSEStream(w io.Writer, text string, deltaBytes int, pause time.Duration) []int {
	if deltaBytes <= 0 {
		TextSSE(w, text)
		return nil
	}
	fragments := textFragments(text, deltaBytes)
	writeClaudeTextStart(w)
	flushWriter(w)
	sizes := make([]int, 0, len(fragments))
	for i, fragment := range fragments {
		if i > 0 && pause > 0 {
			time.Sleep(pause) //stylegate:allow-sleep the pause IS the measured cadence: the answer must still be arriving for the live band to be observed mid-paint
		}
		writeClaudeTextDelta(w, fragment)
		flushWriter(w)
		sizes = append(sizes, len(fragment))
	}
	writeClaudeTextEnd(w)
	flushWriter(w)
	return sizes
}

// textFragments splits text into rune-aligned fragments of at most deltaBytes
// bytes each. The loop advances one RUNE at a time and closes a fragment as
// soon as the next rune would push it past deltaBytes, so fragment sizes differ
// by at most one rune's width and no rune is ever split.
func textFragments(text string, deltaBytes int) []string {
	if text == "" {
		return nil
	}
	var out []string
	start, size := 0, 0
	for i, r := range text {
		w := utf8.RuneLen(r)
		if size > 0 && size+w > deltaBytes {
			out = append(out, text[start:i])
			start, size = i, 0
		}
		size += w
	}
	if size > 0 {
		out = append(out, text[start:])
	}
	return out
}

// flushWriter pushes a frame to the wire immediately when the writer supports
// it. httptest's ResponseWriter is an http.Flusher; a plain buffer is not, and
// then there is nothing to flush.
func flushWriter(w io.Writer) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// writeClaudeTextStart / writeClaudeTextDelta / writeClaudeTextEnd are the
// frames TextSSE and TextSSEChunked share, so the two shapes cannot drift
// apart in anything except their chunking.
func writeClaudeTextStart(w io.Writer) {
	WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
	WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
}

func writeClaudeTextDelta(w io.Writer, fragment string) {
	j, _ := json.Marshal(fragment) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`, j))
}

func writeClaudeTextEnd(w io.Writer) {
	WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// ToolUseSSE streams a Claude-style single tool_use answer: the block starts
// with an empty input, then the full argument JSON arrives as one
// input_json_delta fragment (the loop accumulates fragments and parses the
// combined string at content_block_stop — see internal/agent/loop.go).
//
// partial_json is a STRING containing the accumulated JSON — not a JSON
// object. Marshaling the args map directly emits a bare object, which the
// loop's fragment accumulator silently reads as no input at all (the tool
// then fails with "Missing required parameter: …").
func ToolUseSSE(w io.Writer, tc ToolCall) {
	argsJSON, _ := json.Marshal(tc.Args)             //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)// the tool input as a JSON object
	partialJSON, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)// …wrapped as a JSON string
	idJSON, _ := json.Marshal(tc.ID)                 //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	nameJSON, _ := json.Marshal(tc.Name)             //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
	WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":%s,"name":%s,"input":{}}}`, idJSON, nameJSON))
	WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, partialJSON))
	WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// ParseSSE decodes a raw SSE body into (event, data) pairs. The web /api/chat
// endpoint emits `event: delta` / `event: done` frames; the Claude stream
// parser in the client is separate (it runs inside the binary).
func ParseSSE(body string) [][2]string {
	var out [][2]string
	for _, frame := range splitSSEFrames(body) {
		var ev, data string
		for _, line := range frame {
			if len(line) >= 6 && line[:6] == "event:" {
				ev = strings.TrimSpace(line[6:])
			} else if len(line) >= 5 && line[:5] == "data:" {
				data += strings.TrimSpace(line[5:])
			}
		}
		if ev == "" && data == "" {
			continue
		}
		out = append(out, [2]string{ev, data})
	}
	return out
}

// CountSSEEvents counts how many frames carry the given event name.
func CountSSEEvents(events [][2]string, name string) int {
	n := 0
	for _, ev := range events {
		if ev[0] == name {
			n++
		}
	}
	return n
}

// splitSSEFrames splits an SSE body into frames (each a slice of lines).
func splitSSEFrames(body string) [][]string {
	var frames [][]string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var lines []string
		for _, l := range strings.Split(frame, "\n") {
			lines = append(lines, strings.TrimSuffix(l, "\r"))
		}
		frames = append(frames, lines)
	}
	return frames
}
