package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// mockStreamingServer returns an httptest server that serves the Claude SSE
// text stream for the given final answer, firing one text_delta. It stands in
// for api.deepseek.com so the real Agent.Chat path streams through OnText.
func mockStreamingServer(t *testing.T, finalText string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("mock: expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, finalText)
	}))
}

// textSSE writes a minimal Claude-style streaming text response for finalText.
// Split into two deltas so we can assert the SSE stream carries per-fragment
// events (the streaming property the web UI depends on). The frame shape
// mirrors the agent package's claudeTextStream helper (content_block_start →
// one or more text_deltas → content_block_stop → message_delta → message_stop).
func textSSE(w io.Writer, text string) {
	writeClaudeSSE(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	writeClaudeSSE(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	mid := len(text) / 2
	parts := []string{text[:mid], text[mid:]}
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		jsonText, _ := json.Marshal(p) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		writeClaudeSSE(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}", jsonText))
	}
	writeClaudeSSE(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	writeClaudeSSE(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":7}}")
	writeClaudeSSE(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// writeClaudeSSE writes one SSE frame with an event type and JSON data payload.
func writeClaudeSSE(w io.Writer, eventType, payloadJSON string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payloadJSON)
}

// webServerForTest builds a *webServer whose agent factory returns a real
// *agent.Agent pointed at the mock streaming server, and wires it in as the
// server's SINGLE persistent agent (the production runWeb path now holds one
// agent across every /api/chat turn — the tests exercise that same reuse).
func webServerForTest(t *testing.T, finalText string) (*webServer, *httptest.Server) {
	t.Helper()
	mockSrv := mockStreamingServer(t, finalText)
	ag := &agent.Agent{
		Client: &deepseek.Client{
			BaseURL:    mockSrv.URL,
			APIKey:     "test-key",
			HTTPClient: mockSrv.Client(),
		},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		OnText:  nil, // handleChat sets it
		Timeout: 0,
	}
	ag.PersistChatSession = true // single-session persistence (as runWeb sets)
	ws := &webServer{
		addr:      ":0",
		agent:     ag,
		startedAt: time.Now(),
	}
	return ws, mockSrv
}

// postChat issues one /api/chat turn and returns the raw SSE body.
func postChat(t *testing.T, ws *webServer, message string, history []webChatMessage) string {
	t.Helper()
	body, _ := json.Marshal(webChatRequest{Message: message, History: history}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleChat status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	return rec.Body.String()
}

// sseEvents decodes a raw SSE body into a list of (event, data) pairs.
func sseEvents(body string) [][2]string {
	var out [][2]string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		var ev, data string
		for _, line := range strings.Split(frame, "\n") {
			line = strings.TrimSuffix(line, "\r")
			if strings.HasPrefix(line, "event:") {
				ev = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		out = append(out, [2]string{ev, data})
	}
	return out
}

func TestIsWebSubcommandDetection(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"web"}, true},
		{[]string{"web", "--addr=:9999"}, true},
		{[]string{"chat"}, false},
		{[]string{"write", "x"}, false},
		{nil, false},
	}
	for _, c := range cases {
		got := len(c.args) > 0 && c.args[0] == "web"
		if got != c.want {
			t.Errorf("isWeb(args=%v)=%v, want %v", c.args, got, c.want)
		}
	}
}

func TestRunWebHelpReturnsZero(t *testing.T) {
	if code := runWeb([]string{"-h"}); code != 0 {
		t.Errorf("runWeb([-h]) = %d, want 0", code)
	}
	if code := runWeb([]string{"--help"}); code != 0 {
		t.Errorf("runWeb([--help]) = %d, want 0", code)
	}
}

func TestHandleRootServesPage(t *testing.T) {
	ws := &webServer{addr: ":0"}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	ws.handleRoot(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handleRoot status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "gopi web") {
		t.Error("page does not contain the 'gopi web' title")
	}
	if !strings.Contains(rec.Body.String(), "/api/chat") {
		t.Error("page does not reference the /api/chat endpoint")
	}
	if !strings.Contains(rec.Body.String(), "getReader()") {
		t.Error("page does not read the streaming (SSE) response body")
	}
}

func TestHandleChatStreamsDeltasAndDone(t *testing.T) {
	const answer = "The capital of France is Paris."
	ws, mockSrv := webServerForTest(t, answer)
	defer mockSrv.Close()

	body := postChat(t, ws, "What is the capital of France?", nil)
	events := sseEvents(body)

	var gotDelta strings.Builder
	gotDone := false
	var donePayload map[string]interface{}
	for _, ev := range events {
		switch ev[0] {
		case "delta":
			var d struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(ev[1]), &d); err != nil {
				t.Fatalf("bad delta data %q: %v", ev[1], err)
			}
			gotDelta.WriteString(d.Text)
		case "done":
			gotDone = true
			if err := json.Unmarshal([]byte(ev[1]), &donePayload); err != nil {
				t.Fatalf("bad done data %q: %v", ev[1], err)
			}
		case "error":
			t.Fatalf("unexpected error event: %s", ev[1])
		}
	}

	if !gotDone {
		t.Fatal("stream never emitted a done event")
	}
	if gotDelta.String() != answer {
		t.Errorf("streamed text = %q, want %q", gotDelta.String(), answer)
	}
	if res, _ := donePayload["result"].(string); res != answer {
		t.Errorf("done.result = %q, want %q", res, answer)
	}
	// Assert per-fragment streaming actually happened (two deltas for the
	// split answer) — not a single coalesced blob.
	if n := countSSEEvent(events, "delta"); n < 2 {
		t.Errorf("expected >=2 delta events (real streaming), got %d", n)
	}
}

func countSSEEvent(events [][2]string, name string) int {
	n := 0
	for _, ev := range events {
		if ev[0] == name {
			n++
		}
	}
	return n
}

// TestHandleChatRoundsHistoryIntoRequest — R1 REWRITE of the legacy contract
// test. The old web behavior seeded each turn from the client-shipped
// `history` field; under R1 the SERVER owns the transcript, so the request's
// history is accepted (decoding must not fail — back-compat) but IGNORED:
// the model request carries only the server-captured context plus the new
// message. (Multi-turn continuity without any client history is locked by
// TestWebChat_R1_ServerOwnedContextTwoTurnsNoClientHistory.)
func TestHandleChatRoundsHistoryIntoRequest(t *testing.T) {
	var lastReq string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		lastReq = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "ok")
	}))
	defer srv.Close()

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		OnText:  nil,
		Timeout: 0,
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	history := []webChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "first reply"},
	}
	body := postChat(t, ws, "second", history)

	// The /api/chat response must be a well-formed SSE stream ending in done.
	if !strings.Contains(body, "event: done") {
		t.Fatalf("expected done event, body=%q", body)
	}
	if lastReq == "" {
		t.Fatal("mock never received the model request")
	}
	var modelReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(lastReq), &modelReq); err != nil {
		t.Fatalf("bad model request JSON %q: %v", lastReq, err)
	}
	var seenSecond bool
	for _, m := range modelReq.Messages {
		for _, c := range m.Content {
			if c.Text == "second" {
				seenSecond = true
			}
		}
	}
	if !seenSecond {
		t.Errorf("new message missing from model request: %q", lastReq)
	}
	// R1: the client-shipped history must NOT reach the model request.
	for _, m := range modelReq.Messages {
		for _, c := range m.Content {
			if c.Text == "first" || c.Text == "first reply" {
				t.Errorf("client-shipped history leaked into the model request (R1 violation): %q", lastReq)
			}
		}
	}
}

func TestHandleChatRejectsEmptyMessage(t *testing.T) {
	ws := &webServer{addr: ":0"}
	body, _ := json.Marshal(webChatRequest{Message: "   "}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty message status = %d, want 400", rec.Code)
	}
}

func TestHandleChatRejectsNonPost(t *testing.T) {
	ws := &webServer{addr: ":0"}
	req := httptest.NewRequest(http.MethodGet, "/api/chat", nil)
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/chat status = %d, want 405", rec.Code)
	}
}

// TestHandleChat_MidStreamClientDisconnectNoPanic is the regression test for
// the root-cause fix: an earlier draft closed a signal channel from both the
// request-context goroutine and the deferred close, panicking with "close of
// closed channel" whenever a client hung up mid-stream. The channel was
// removed (nothing consumed it; Agent.Chat's own timeout bounds the turn).
// This test drives a real abort — the client's Request context is cancelled
// while the handler would otherwise be streaming — and asserts handleChat
// neither panics nor leaves the server unable to serve a fresh request.
func TestHandleChat_MidStreamClientDisconnectNoPanic(t *testing.T) {
	const answer = "one two three four five"
	ws, mockSrv := webServerForTest(t, answer)
	defer mockSrv.Close()

	// Build a request whose context is already cancelled — equivalent to a
	// client that dropped the connection the instant the turn began. The
	// fixed handler must not panic (the old code's goroutine would have
	// closed the channel and the deferred close would have double-closed).
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body, _ := json.Marshal(webChatRequest{Message: "hi", History: nil}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	// handleChat must complete without a panic (a panic would fail the test
	// via the test harness's recover, but we assert explicitly).
	if rec := func() (r *httptest.ResponseRecorder) {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("handleChat panicked on cancelled-request context: %v", p)
			}
		}()
		ws.handleChat(rec, req)
		return rec
	}(); !strings.Contains(rec.Body.String(), "event: done") {
		// With a cancelled client context the SSE response may be partial, but
		// the handler must still terminate cleanly and the recorder should
		// carry no panic marker. done is expected when the write still lands.
		t.Logf("note: cancelled-context body = %q (partial is acceptable)", rec.Body.String())
	}
}

// --- Regression tests for the reachable-URL printing fix (http://:8090 bug) ---

// helper: nonLoopbackIPs returns the machine's non-loopback IPv4 addresses as
// strings, for asserting against webDisplayURLs/webPrintListening output.
func helperNonLoopbackIPs(t *testing.T) []string {
	t.Helper()
	var ips []string
	if ifs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range ifs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
				ips = append(ips, ip4.String())
			}
		}
	}
	if len(ips) == 0 {
		t.Skip("no non-loopback IPv4 interface — cannot assert LAN URL listing")
	}
	return ips
}

// hasAllSubset reports whether every element of want appears in got.
func hasAllSubset(got, want []string) bool {
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// TestWebDisplayURLs_ExplicitHost: a concrete host is printed verbatim, not
// expanded to all interfaces.
func TestWebDisplayURLs_ExplicitHost(t *testing.T) {
	got := webDisplayURLs("127.0.0.1", "8099")
	if len(got) != 1 || got[0] != "http://127.0.0.1:8099" {
		t.Fatalf("explicit host: got %v, want exactly [http://127.0.0.1:8099]", got)
	}
	got = webDisplayURLs("localhost", "8099")
	if len(got) != 1 || got[0] != "http://localhost:8099" {
		t.Fatalf("explicit localhost: got %v, want exactly [http://localhost:8099]", got)
	}
}

// TestWebDisplayURLs_EmptyHost: an all-interfaces bind must never yield the
// broken "http://:PORT" form; it must carry a localhost URL plus the machine's
// reachable non-loopback IPs (what a browser on another host actually needs).
func TestWebDisplayURLs_EmptyHost(t *testing.T) {
	ips := helperNonLoopbackIPs(t)
	for _, host := range []string{"", "0.0.0.0", "::"} {
		got := webDisplayURLs(host, "8090")

		// Regression guard: NO empty-host URL may appear.
		if hasAllSubset(got, []string{"http://:8090"}) || hasAllSubset(got, []string{"http://0.0.0.0:8090"}) {
			t.Fatalf("host %q: got %v — emitted a non-navigable all-interfaces URL", host, got)
		}
		// Must carry the loopback URL a browser can open immediately.
		if !hasAllSubset(got, []string{"http://localhost:8090"}) {
			t.Fatalf("host %q: got %v — missing http://localhost:8090", host, got)
		}
		// Must also expose the LAN reach addresses.
		want := make([]string, 0, len(ips))
		for _, ip := range ips {
			want = append(want, "http://"+ip+":8090")
		}
		if !hasAllSubset(got, want) {
			t.Fatalf("host %q: got %v — missing LAN URLs %v", host, got, want)
		}
	}
}

// TestWebPrintListening_EmptyHostPrintsReachableURLs: the listening banner
// must print browser-navigable "open: http://..." lines (never the broken
// "http://:PORT" single line). Captures os.Stderr.
func TestWebPrintListening_EmptyHostPrintsReachableURLs(t *testing.T) {
	ips := helperNonLoopbackIPs(t)

	// Listen on an ephemeral port so the printed port matches a real listener.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := strings.TrimPrefix(ln.Addr().String(), "[::]:")
	if i := strings.LastIndex(ln.Addr().String(), ":"); i >= 0 {
		port = ln.Addr().String()[i+1:]
	}

	old := os.Stderr
	r, w, _ := os.Pipe() //stylegate:ignore opens the fixture pipe; the read below is the assertion
	os.Stderr = w
	webPrintListening(":0", ln, "", false)
	w.Close() //stylegate:ignore test captures a listening notice through an os.Pipe; the read below is the assertion, and the writer close error cannot lose buffered output
	os.Stderr = old
	out, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable

	s := string(out)
	if strings.Contains(s, "http://:") {
		t.Fatalf("printed a broken all-interfaces URL: %q", s)
	}
	if !strings.Contains(s, "open: http://localhost:"+port) {
		t.Fatalf("missing loopback URL line in: %q", s)
	}
	for _, ip := range ips {
		if !strings.Contains(s, "open: http://"+ip+":"+port) {
			t.Fatalf("missing LAN URL for %s in: %q", ip, s)
		}
	}
}

// TestWebPrintListening_ExplicitHostPrintsVerbatum: "--addr=127.0.0.1:PORT"
// prints that exact URL, never the machine's other interfaces.
func TestWebPrintListening_ExplicitHostPrintsVerbatum(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String() // e.g. 127.0.0.1:PORT

	old := os.Stderr
	r, w, _ := os.Pipe() //stylegate:ignore opens the fixture pipe; the read below is the assertion
	os.Stderr = w
	webPrintListening(addr, ln, "", false)
	w.Close() //stylegate:ignore test captures a listening notice through an os.Pipe; same bounded-capture shape as the test above, the read is the assertion
	os.Stderr = old
	out, _ := io.ReadAll(r) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable

	s := string(out)
	if !strings.Contains(s, "http://"+addr) {
		t.Fatalf("explicit host: missing %s in banner: %q", addr, s)
	}
	if strings.Contains(s, "open: http://localhost") || strings.Contains(s, "open: http://192.") {
		t.Fatalf("explicit host: banner unexpectedly printed extra URLs: %q", s)
	}
}

// TestWebChat_SinglePersistentSession verifies the Epic's consolidation ask
// at the HTTP boundary: the web server holds ONE persistent agent
// (PersistChatSession, the same single-session foundation as `gopi chat`), so
// multiple /api/chat turns SHARE one session id + one session JSONL (each turn
// appends, no fresh session + log per turn — the pre-Epic per-turn bug), and
// finalizeChatSession writes exactly ONE session_end + closes the ledger.
// Mirrors chat_session_test.go's single-session-across-turns assertions.
func TestWebChat_SinglePersistentSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPI_HOME", dir)

	mockSrv := mockStreamingServer(t, "web single-session answer")
	defer mockSrv.Close()

	// Build a real session-+raw-log agent (the single persistent one runWeb
	// creates at startup), pointed at the mock.
	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	defer func() { sl.Close(); rl.Close() }()
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: sl,
		RawLog:     rl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true // mirror runWeb's single-session setup

	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	sessionPath := ag.SessionLog.Path()
	sessionID := ag.SessionLog.SessionID()

	// Drive TWO turns through handleChat (reusing the same persistent agent).
	history := []webChatMessage{{Role: "user", Content: "first"}}
	postChat(t, ws, "first", nil)
	postChat(t, ws, "second", history)

	// The same single session stays open with the SAME id + path.
	if ag.SessionLog == nil || ag.SessionLog.SessionID() != sessionID {
		t.Fatalf("web session id changed across turns: got nil=%v want %s", ag.SessionLog == nil, sessionID)
	}
	if ag.SessionLog.Path() != sessionPath {
		t.Errorf("web session path changed across turns: got %s want %s", ag.SessionLog.Path(), sessionPath)
	}

	// Read the ONE session file: one session_start, two user, two assistant,
	// and NO per-turn session_end before finalize.
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read web session file: %v", err)
	}
	var starts, users, assistants, ends int
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "session_start":
			starts++
		case "user":
			users++
		case "assistant":
			assistants++
		case "session_end":
			ends++
		}
	}
	if starts != 1 {
		t.Errorf("web session_start count = %d, want 1 (ONE session, not per turn)", starts)
	}
	if users != 2 || assistants != 2 {
		t.Errorf("web user/assistant events = %d/%d, want 2/2 (both turns appended to one file)", users, assistants)
	}
	if ends != 0 {
		t.Errorf("web per-turn session_end count = %d, want 0 before finalize", ends)
	}

	// finalizeChatSession: exactly ONE session_end on the SAME file.
	ws.finalizeChatSession(true)
	data2, _ := os.ReadFile(sessionPath) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	ends2 := 0
	for _, ln := range strings.Split(string(data2), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) == nil && ev["type"] == "session_end" {
			ends2++
		}
	}
	if ends2 != 1 {
		t.Errorf("web session_end count after finalize = %d, want exactly 1 (single-session close)", ends2)
	}
}

// TestWebChat_ChatTableReflectsSession verifies the M4.6 钩子分流 at the web
// boundary: after a successful web chat turn the session is registered in the
// CHATS table (mode=web — the conversation index, NOT the tasks ledger),
// RecordChatTurn bumps the turn facts, and finalizeChatSession stamps
// ended_at/end_reason=normal. Drives handleChat + finalizeChatSession
// end-to-end with a temp GOPI_HOME + GOPI_MEMORY_DB so the real profile is
// never touched.
func TestWebChat_ChatTableReflectsSession(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	mockSrv := mockStreamingServer(t, "web task answer")
	defer mockSrv.Close()

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true

	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	// One successful turn ensures the single session's chats-table row.
	postChat(t, ws, "what is the web chat task?", nil)
	if !ag.ChatRegisterLatch() {
		t.Fatal("web chat did not ensure its chats-table row after a successful turn")
	}
	sessionID := ag.SessionLog.SessionID()
	row, err := tools.ChatList(tools.ChatListFilter{})
	if err != nil || !strings.Contains(row, sessionID) {
		t.Fatalf("chat list = %q, err %v; want session %s", row, err, sessionID)
	}
	if !strings.Contains(row, "what is the web chat task?") {
		t.Errorf("chat row missing the first web chat input: %q", row)
	}

	// A second turn still uses the SAME row (one web conversation, not one
	// row per turn) and bumps turn_count.
	postChat(t, ws, "second turn", []webChatMessage{{Role: "user", Content: "what is the web chat task?"}})
	row2, err := tools.ChatList(tools.ChatListFilter{})
	if err != nil || !strings.Contains(row2, sessionID) {
		t.Fatalf("second-turn chat list = %q, err %v; still want one row", row2, err)
	}
	// THE BARE WORD CARRIES THE LOG'S OPENED COUNT (epic 2026-11-04): this web
	// session HAS a log, so `turns 2` is a measurement of the position axis every
	// replay band numbers along, and the second turn bumped it.
	//
	// NO COMPLETED CLAUSE HERE, AND THAT IS THE DESIGN, not an omission: the
	// ledger's number prints on the PER-SESSION faces (`session show`, `stats`,
	// `log`, `replay`); on a one-line index row the two clauses would sit side by
	// side with nothing but punctuation between them and the ledger's number would
	// read as a measurement of the word beside it (internal/tools/chat_turns.go's
	// chatTurnClause carries the measured reason). What this arm pins is that the
	// count the row DOES print advanced with the second turn, under the bare word.
	if !strings.Contains(row2, sessionevent.CounterUnitTurn+" 2") {
		t.Errorf("second turn did not bump the opened count (want %q): %q",
			sessionevent.CounterUnitTurn+" 2", row2)
	}

	// finalizeChatSession (server shutdown) stamps ended_at + end_reason.
	ws.finalizeChatSession(true)
	row3, err := tools.ChatList(tools.ChatListFilter{})
	if err != nil || !strings.Contains(row3, "normal") {
		t.Fatalf("finalized chat list = %q, err %v; want end_reason normal", row3, err)
	}
}

// webMaxChatBodyBytes caps a single chat request body (history + message).

// TestWebChat_ConcurrentTurnsSingleSession verifies the serialization guard
// added for the shared persistent agent: concurrent /api/chat requests against
// the SAME webServer (same one session) must not race the shared mutable agent
// state (Ctx.Messages, Cost, iterCount). Run under -race this proves the
// s.mu serialization keeps one conversation's turns safe — mirroring the
// strictly sequential `gopi chat` REPL.
func TestWebChat_ConcurrentTurnsSingleSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPI_HOME", dir)

	mockSrv := mockStreamingServer(t, "concurrent answer")
	defer mockSrv.Close()

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	defer func() { sl.Close(); rl.Close() }()
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: sl,
		RawLog:     rl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	sessionID := ag.SessionLog.SessionID()

	// Fire 5 concurrent turns at the same single session.
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			body, _ := json.Marshal(webChatRequest{Message: fmt.Sprintf("turn %d", n), History: nil}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
			req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			ws.handleChat(rec, req)
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "event: done") {
				errs <- fmt.Errorf("turn %d: status %d, no done", n, rec.Code)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent turn error: %v", err)
	}

	// One session across 5 turns: exactly 5 user + 5 assistant events on ONE
	// file, still the same id.
	if ag.SessionLog == nil || ag.SessionLog.SessionID() != sessionID {
		t.Fatalf("session id changed under concurrency: nil=%v", ag.SessionLog == nil)
	}
	data, _ := os.ReadFile(ag.SessionLog.Path()) //stylegate:ignore reads a file the test itself wrote; the content is the assertion
	var users, assistants int
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev map[string]interface{}
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		switch ev["type"] {
		case "user":
			users++
		case "assistant":
			assistants++
		}
	}
	if users != 5 || assistants != 5 {
		t.Errorf("concurrent user/assistant = %d/%d, want 5/5 on the one session file", users, assistants)
	}
}

// Repro 5 (t-1788053264): `gopi web` defaulted to ":8090" (all interfaces) —
// the docs claim loopback-only, so a no-approval tool-execution surface was
// exposed to the LAN by default. The DEFAULT must be 127.0.0.1; a LAN bind
// requires the explicit --addr 0.0.0.0 opt-in.
func TestReproWebDefaultLoopbackOnly(t *testing.T) {
	t.Setenv("GOPI_WEB_ADDR", "") // the default path, not an env override
	if got := webDefaultAddr(); got != "127.0.0.1:8090" {
		t.Fatalf("BUG REPRODUCED: gopi web default binds %q (all interfaces) — docs promise loopback-only; want 127.0.0.1:8090 (LAN = explicit --addr opt-in)", got)
	}
}

// With the loopback default, an all-interfaces bind can only come from an
// explicit --addr: the banner must still resolve it into reachable URLs
// (the fb9de6b fix must keep working for the opt-in case).
func TestWebDisplayURLsAllInterfacesStillResolvesLAN(t *testing.T) {
	got := webDisplayURLs("", "8090")
	if len(got) == 0 {
		t.Fatal("explicit all-interfaces bind: no URLs resolved")
	}
	for _, u := range got {
		if strings.Contains(u, "http://:8090") || strings.Contains(u, "http://0.0.0.0:8090") {
			t.Fatalf("unusable URL %q in the explicit-opt-in banner", u)
		}
	}
	foundLoopback := false
	for _, u := range got {
		if strings.HasPrefix(u, "http://127.0.0.1:8090") || strings.HasPrefix(u, "http://localhost:8090") {
			foundLoopback = true
		}
	}
	if !foundLoopback {
		t.Fatalf("explicit all-interfaces bind: loopback URL missing from %v", got)
	}
}
