package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/rawlogfmt"
)

// ---------------------------------------------------------------------------
// RawLog must record THE BYTES THAT WENT ON THE WIRE — not a second
// serialization of the request object.
//
// The two send paths (ClaudeChat, ClaudeChatStream) build the body INSIDE the
// retryDo closure, once per attempt. The raw log used to call json.Marshal(req)
// a SECOND time after the loop returned, shadowing the closure's reqBody with
// `:=`. Those are two different serializations joined only by the accident that
// a caller owning req does not touch it between the send and the log write.
//
// The tests below make that accident NOT hold: a wrapping RoundTripper mutates
// the request object the moment the response headers come back — i.e. exactly
// between the send and the log write — and then assert the logged bytes are
// still the bytes the server received, byte for byte.
// ---------------------------------------------------------------------------

// mutateAfterResponse wraps a RoundTripper and mutates the request object at a
// precisely chosen moment: `sentAfter` picks which attempt (1-based) gets the
// mutation right after its response arrives, and `eofAfter` picks which attempt
// gets it when that response's body reaches EOF — i.e. after the LAST send but
// before the send path's log line. RoundTrip and io.ReadAll are both synchronous
// inside http.Client.Do / the caller, so the mutation happens-before the code it
// is meant to fool: no race, no sleep.
type mutateAfterResponse struct {
	base      http.RoundTripper
	sentAfter int    // 1-based attempt; 0 means 1
	onSent    func() // runs when attempt sentAfter's response arrives
	eofAfter  int    // 1-based attempt; 0 means "never"
	onBodyEOF func() // runs when attempt eofAfter's body hits EOF

	n       int
	once    sync.Once
	eofOnce sync.Once
}

func (m *mutateAfterResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := m.base.RoundTrip(r)
	m.n++
	if err != nil {
		return resp, err
	}
	target := m.sentAfter
	if target == 0 {
		target = 1
	}
	if m.n == target && m.onSent != nil {
		m.once.Do(m.onSent)
	}
	if m.n == m.eofAfter && m.onBodyEOF != nil {
		resp.Body = &eofMutator{rc: resp.Body, on: func() { m.eofOnce.Do(m.onBodyEOF) }}
	}
	return resp, err
}

// eofMutator runs on when the wrapped body reports EOF — the moment the
// response has been fully read, which on the non-streaming path is the last
// instant before the raw log is written.
type eofMutator struct {
	rc  io.ReadCloser
	on  func()
	hit bool
}

func (e *eofMutator) Read(p []byte) (int, error) {
	n, err := e.rc.Read(p)
	if err == io.EOF && !e.hit {
		e.hit = true
		if e.on != nil {
			e.on()
		}
	}
	return n, err
}

func (e *eofMutator) Close() error { return e.rc.Close() }

// rawRecord is one RawLog callback invocation.
type rawRecord struct {
	tag        string
	annotation string
	data       []byte
}

// rawLogRecorder collects every RawLog callback, byte for byte.
type rawLogRecorder struct {
	mu      sync.Mutex
	records []rawRecord
}

func (rr *rawLogRecorder) log(tag, annotation string, data []byte) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	rr.records = append(rr.records, rawRecord{tag: tag, annotation: annotation, data: cp})
}

// bytesFor returns the payload of the last record carrying tag.
func (rr *rawLogRecorder) bytesFor(tag string) []byte {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	for i := len(rr.records) - 1; i >= 0; i-- {
		if rr.records[i].tag == tag {
			return rr.records[i].data
		}
	}
	return nil
}

// wireServer records the request body of every attempt and answers with the
// supplied handler.
type wireServer struct {
	mu       sync.Mutex
	attempts [][]byte
}

func (ws *wireServer) record(r *http.Request) {
	body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.attempts = append(ws.attempts, body)
}

func (ws *wireServer) last() []byte {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if len(ws.attempts) == 0 {
		return nil
	}
	return ws.attempts[len(ws.attempts)-1]
}

func (ws *wireServer) count() int {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return len(ws.attempts)
}

const rawLogTestSSEBody = `event: message_start
data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

// assertLoggedEqualsWire is the whole point of the fix, in one assertion: the
// request bytes the raw log holds are the request bytes the server received.
func assertLoggedEqualsWire(t *testing.T, label string, wire, logged []byte) {
	t.Helper()
	if logged == nil {
		t.Fatalf("%s: no %q record reached RawLog", label, rawlogfmt.TagRequest)
	}
	if len(wire) != len(logged) || !bytes.Equal(wire, logged) {
		t.Fatalf("%s: RawLog bytes != bytes on the wire\n wire (%d): %s\n log  (%d): %s",
			label, len(wire), wire, len(logged), logged)
	}
}

// TestClaudeChatRawLogIsTheSentBytes — non-streaming path (ClaudeChat).
func TestClaudeChatRawLogIsTheSentBytes(t *testing.T) {
	ws := &wireServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer srv.Close()

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages: []ClaudeMessage{
			{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "before"}}},
		},
	}

	rec := &rawLogRecorder{}
	client := &Client{
		BaseURL:     srv.URL,
		APIKey:      "test-key",
		RetryConfig: DefaultRetryConfig(),
		RawLog:      rec.log,
		HTTPClient: &http.Client{Transport: &mutateAfterResponse{
			base: srv.Client().Transport,
			// The caller mutates the request AFTER it was sent — the case the
			// old code silently recorded the WRONG bytes for.
			eofAfter:  1,
			onBodyEOF: func() { req.Messages[0].Content[0].Text = "mutated-after-send" },
		}},
	}

	if _, err := client.ClaudeChat(context.Background(), req); err != nil {
		t.Fatalf("ClaudeChat: %v", err)
	}

	wire := ws.last()
	if !bytes.Contains(wire, []byte("before")) {
		t.Fatalf("wire body does not carry the pre-mutation text: %s", wire)
	}
	assertLoggedEqualsWire(t, "ClaudeChat", wire, rec.bytesFor(rawlogfmt.TagRequest))

	// The mutation really did happen — otherwise this test would pass on the
	// old code by accident, which is the very defect it guards.
	if !bytes.Contains([]byte(mustMarshal(t, req)), []byte("mutated-after-send")) {
		t.Fatalf("test precondition broken: req was not mutated after the send")
	}
}

// TestClaudeChatStreamRawLogIsTheSentBytes — streaming path
// (ClaudeChatStream), plus the `stream` field: the wire body and the logged
// body must BOTH say stream:true.
func TestClaudeChatStreamRawLogIsTheSentBytes(t *testing.T) {
	ws := &wireServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.record(r)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(rawLogTestSSEBody))
	}))
	defer srv.Close()

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages: []ClaudeMessage{
			{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "before"}}},
		},
	}

	rec := &rawLogRecorder{}
	client := &Client{
		BaseURL:     srv.URL,
		APIKey:      "test-key",
		RetryConfig: DefaultRetryConfig(),
		RawLog:      rec.log,
		HTTPClient: &http.Client{Transport: &mutateAfterResponse{
			base:   srv.Client().Transport,
			onSent: func() { req.Messages[0].Content[0].Text = "mutated-after-send" },
		}},
	}

	events, err := client.ClaudeChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ClaudeChatStream: %v", err)
	}
	for range events { // drain: parseClaudeSSE owns the body
	}

	wire := ws.last()
	assertLoggedEqualsWire(t, "ClaudeChatStream", wire, rec.bytesFor(rawlogfmt.TagRequest))

	// The stream flag is part of the same defect: the request body must not
	// disagree with the streaming behaviour the caller asked for.
	var wireReq, loggedReq ClaudeRequest
	if err := json.Unmarshal(wire, &wireReq); err != nil {
		t.Fatalf("wire body is not the request JSON: %v\n%s", err, wire)
	}
	if err := json.Unmarshal(rec.bytesFor(rawlogfmt.TagRequest), &loggedReq); err != nil {
		t.Fatalf("logged body is not the request JSON: %v\n%s", err, rec.bytesFor(rawlogfmt.TagRequest))
	}
	if !bytes.Contains(wire, []byte(`"stream":true`)) {
		t.Fatalf(`wire body is missing "stream":true — the streaming call would not stream: %s`, wire)
	}
	if !bytes.Contains(rec.bytesFor(rawlogfmt.TagRequest), []byte(`"stream":true`)) {
		t.Fatalf(`logged body is missing "stream":true — the raw log does not describe the streaming call it recorded: %s`,
			rec.bytesFor(rawlogfmt.TagRequest))
	}
	if !wireReq.Stream || !loggedReq.Stream {
		t.Fatalf("stream flag decoded false: wire=%v logged=%v", wireReq.Stream, loggedReq.Stream)
	}
}

// TestClaudeChatNonStreamRawLogSaysStreamFalse — the mirror case: the
// non-streaming path must not claim stream:true on the wire or in the log.
func TestClaudeChatNonStreamRawLogSaysStreamFalse(t *testing.T) {
	ws := &wireServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer srv.Close()

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Stream:    true, // a caller that had it set: the path must clear it
		Messages: []ClaudeMessage{
			{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}},
		},
	}

	rec := &rawLogRecorder{}
	client := &Client{
		BaseURL:     srv.URL,
		APIKey:      "test-key",
		RetryConfig: DefaultRetryConfig(),
		RawLog:      rec.log,
		HTTPClient:  srv.Client(),
	}

	if _, err := client.ClaudeChat(context.Background(), req); err != nil {
		t.Fatalf("ClaudeChat: %v", err)
	}

	wire := ws.last()
	assertLoggedEqualsWire(t, "ClaudeChat non-stream", wire, rec.bytesFor(rawlogfmt.TagRequest))
	if bytes.Contains(wire, []byte(`"stream":true`)) {
		t.Fatalf(`non-streaming wire body claims "stream":true: %s`, wire)
	}
	if bytes.Contains(rec.bytesFor(rawlogfmt.TagRequest), []byte(`"stream":true`)) {
		t.Fatalf(`non-streaming raw log claims "stream":true: %s`, rec.bytesFor(rawlogfmt.TagRequest))
	}
}

func mustMarshal(t *testing.T, req *ClaudeRequest) string {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestRawLogRecordsTheLastAttemptAfterARetry — the retry leg.
//
// retryDo RE-MARSHALS per attempt on purpose (each attempt owns its own ctx and
// httpReq), so the correct thing to log is the LAST attempt's bytes, not the
// first serialization hoisted out of the loop. A fix that hoists one
// json.Marshal outside the closure and logs THAT would pass the single-attempt
// tests above and still be wrong here: the raw log would describe a request the
// server refused.
func TestRawLogRecordsTheLastAttemptAfterARetry(t *testing.T) {
	ws := &wireServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws.record(r)
		if ws.count() == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"type":"api_error","message":"overloaded"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg_1","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	defer srv.Close()

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages: []ClaudeMessage{
			{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "attempt-one"}}},
		},
	}

	rec := &rawLogRecorder{}
	client := &Client{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		RetryConfig: RetryConfig{
			MaxRetries:     3,
			InitialDelay:   time.Millisecond,
			MaxDelay:       2 * time.Millisecond,
			JitterFraction: 0,
		},
		RawLog: rec.log,
		HTTPClient: &http.Client{Transport: &mutateAfterResponse{
			base: srv.Client().Transport,
			// Mutate DURING the accepted attempt's response read — after its
			// body left the wire, before the log write. No mutation on attempt
			// 1, so the first body still says "attempt-one".
			eofAfter:  2,
			onBodyEOF: func() { req.Messages[0].Content[0].Text = "attempt-two" },
		}},
	}

	if _, err := client.ClaudeChat(context.Background(), req); err != nil {
		t.Fatalf("ClaudeChat: %v", err)
	}

	if got := ws.count(); got != 2 {
		t.Fatalf("expected exactly 2 attempts (500 then 200), got %d", got)
	}
	wire := ws.last()
	if !bytes.Contains(wire, []byte("attempt-one")) {
		t.Fatalf("test precondition broken: attempt 1's body is not the pre-mutation one: %s", wire)
	}
	logged := rec.bytesFor(rawlogfmt.TagRequest)
	assertLoggedEqualsWire(t, "retried ClaudeChat", wire, logged)
	if bytes.Contains(logged, []byte("attempt-two")) {
		t.Fatalf("raw log recorded a body that was never sent: %s", logged)
	}
}
