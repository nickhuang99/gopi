package main

// cli_web_state_test.go — the regression guard for the web surface's
// SERVER-SIDE STATUS (cli_web_state.go).
//
// THE DEFECT THIS PINS. The page used to have only its own `busy` flag: true
// from the moment it POSTed until the stream ended. That flag cannot name a
// tool, cannot say whether the turn can be interrupted, and is silent for a
// turn the SERVER started by itself (cli_web_notice.go). The fix is a server
// state machine whose transitions ride the callbacks the agent loop already
// fires, streamed as `state` frames and readable on demand from /api/state.
//
// WHAT THE TEST DRIVES — the REAL surfaces, not a hand-assembled document:
//
//   - a REAL tool turn through the agent's own ReAct loop (the mock model asks
//     for a tool on round 1 and answers text on round 2, and the tool's OWN
//     executor is a stub this test installs), so the `tool` frames — and
//     therefore the status transitions — come from the loop;
//   - a real GET /api/state while that turn is parked inside the tool, which is
//     the reading the epic's acceptance criterion asks for;
//   - a standing /api/events subscriber, to prove the transitions are PUSHED
//     over the channel that already existed rather than polled;
//   - the notice carrier, to prove a server-started turn is visible too.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
)

// webStateTestToolName is the tool the mock model asks for. It is a real tool
// name (`read`) so the digest path is exercised, but the EXECUTOR is stubbed —
// this test is about the status, not about the tool.
const webStateTestToolName = "read"

// webStateTestToolDelay is how long the stubbed tool BLOCKS. It is what makes
// the middle state observable at all: the assertions below query /api/state
// while the turn is parked here, which is only possible because the transition
// fires before the tool executes.
var webStateTestToolDelay = 400 * time.Millisecond

// webStateTestStreamWindow bounds the standing-stream subscriber in the tests:
// long enough to hold the turn's transitions, short enough that the read ends
// by itself (a test that waits for the stream's own keepalive spends 25 s
// proving nothing).
const webStateTestStreamWindow = 3 * time.Second

// webStateTestClockSkew is how long the assertions wait for a state they
// expect to appear asynchronously (the turn runs on its own goroutine).
const webStateTestClockSkew = 5 * time.Second

// webStateTurnServer builds a server whose model asks for ONE tool call and
// then answers, with the tool's executor blocking for webStateTestToolDelay.
// It returns the server (so a test can install a state observer) and the bare
// handler.
func webStateTurnServer(t *testing.T, onToolStart func()) (*webServer, *httptest.Server) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	const (
		callID = "call_state_1"
		args   = `{"path":"/etc/hostname"}`
	)

	var mu sync.Mutex
	round := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		round++
		n := round
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			toolUseSSE(w, callID, webStateTestToolName, args)
			return
		}
		textSSE(w, "done")
	}))
	t.Cleanup(srv.Close)

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	// The tool's OWN executor: the loop fires the `call` event BEFORE this runs,
	// so a status that is true only after it returns would be invisible here.
	ag.SetToolExecutor(func(tc deepseek.ToolCall) string {
		if onToolStart != nil {
			onToolStart()
		}
		// Not a poll: this duration IS the subject. The state assertions read
		// /api/state WHILE the turn is parked inside the tool, which is only
		// possible if the tool keeps running for a measurable window
		// (TestWebStateToolTransitionPrecedesTheResult drives it with zero).
		time.Sleep(webStateTestToolDelay) //stylegate:allow-sleep the stubbed executor stays parked so the concurrent /api/state reading lands inside the tool turn
		return "hostname-file-body"
	})

	ws := &webServer{addr: ":0", agent: ag, tools: true, startedAt: time.Now()}
	httpSrv := httptest.NewServer(ws.webChatHandler())
	t.Cleanup(httpSrv.Close)
	return ws, httpSrv
}

// readState GETs the snapshot endpoint and decodes it. The endpoint is read
// OVER HTTP (not by calling the method), because the criterion is about the
// surface a reader has: a handler that answers correctly but is not routed is
// still a badge nobody can check.
func readState(t *testing.T, base string) webStateSnapshot {
	t.Helper()
	res, err := http.Get(base + webStatePath)
	if err != nil {
		t.Fatalf("GET %s: %v", webStatePath, err)
	}
	defer res.Body.Close() //stylegate:ignore the close of a status response has no assertion riding on it
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", webStatePath, res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var snap webStateSnapshot
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatalf("decode %s: %v", webStatePath, err)
	}
	return snap
}

// TestWebStateTransitionsAcrossAToolTurn is the epic's minimal reproduction: a
// message that certainly triggers a tool call drives the status
// responding → tool_running(<name>) → idle, and the MIDDLE state is observable
// while the turn is still running (that is the half a state drawn only at the
// end would fail).
func TestWebStateTransitionsAcrossAToolTurn(t *testing.T) {
	ws, httpSrv := webStateTurnServer(t, nil)

	// BEFORE the turn the server is idle — the zero value of the machine, with
	// no constructor having run.
	if got := readState(t, httpSrv.URL); got.State != webStateIdle {
		t.Fatalf("state before any turn = %q, want %q", got.State, webStateIdle)
	}

	// A standing subscriber, exactly as the page holds one open — bounded by a
	// CONTEXT so the read below ends at a deadline instead of blocking until the
	// stream's own 25 s keepalive (an unbounded read here is what made an earlier
	// version of this test report "produced nothing" while holding the frames).
	ctx, cancel := context.WithTimeout(context.Background(), webStateTestStreamWindow)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpSrv.URL+webEventsPath, nil)
	if err != nil {
		t.Fatalf("build GET %s: %v", webEventsPath, err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", webEventsPath, err)
	}
	defer res.Body.Close() //stylegate:ignore the close of the standing stream has no assertion riding on it
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", webEventsPath, res.StatusCode)
	}

	// The turn, on its own goroutine: the handler holds s.mu for the whole turn,
	// so it must not be the goroutine doing the reading below.
	chatDone := make(chan struct{})
	go func() {
		defer close(chatDone)
		rec := newStreamRecorder()
		ws.handleChat(rec, chatRequestForTest(t, "read the hostname file"))
	}()

	// FACT 1 — the MIDDLE state is observable WHILE the tool runs. The tool
	// blocks for webStateTestToolDelay, so this reading lands inside it. A
	// machine that only reported at the end could never answer here.
	waitFor(t, webStateTestClockSkew, "the status to reach tool_running("+webStateTestToolName+")", func() bool {
		got := readState(t, httpSrv.URL)
		return got.State == webStateToolRunning && got.Tool == webStateTestToolName
	})
	mid := readState(t, httpSrv.URL)
	if len(mid.Tools) != 1 || mid.Tools[0] != webStateTestToolName {
		t.Errorf("outstanding tools = %v, want exactly [%s]", mid.Tools, webStateTestToolName)
	}
	if !mid.Interruptible {
		t.Errorf("interruptible = false while a turn is in flight; the badge would hide that the turn can be stopped")
	}
	if mid.Turn != 1 {
		t.Errorf("turn counter = %d, want 1", mid.Turn)
	}

	// FACT 2 — the transition was PUSHED on the standing stream, not merely
	// readable: the page learns of it without asking (no polling loop exists,
	// Rule 35). The frames are read AFTER the fact from the same connection.
	var sawToolFrame bool
	frames := collectStateFrames(t, res.Body)
	for _, f := range frames {
		if f.State == webStateToolRunning && f.Tool == webStateTestToolName {
			sawToolFrame = true
		}
	}
	if !sawToolFrame {
		t.Errorf("the standing stream carried no tool_running(%s) state frame; frames=%+v", webStateTestToolName, frames)
	}

	// Let the turn finish, then FACT 3 — it comes back to idle by itself.
	<-chatDone
	waitFor(t, webStateTestClockSkew, "the status to return to idle", func() bool {
		return readState(t, httpSrv.URL).State == webStateIdle
	})
	final := readState(t, httpSrv.URL)
	if final.Tool != "" {
		t.Errorf("tool = %q after the turn, want empty (an idle server names no tool)", final.Tool)
	}
	if final.Interruptible {
		t.Errorf("interruptible = true with no run in flight; the badge would offer a stop that cannot happen")
	}
}

// TestWebStateToolTransitionPrecedesTheResult pins the ORDER that makes the
// middle state worth having: the CALL event moves the machine into
// tool_running BEFORE the tool executes, so the state is on screen for the
// whole of the tool's blocking duration. The observation is taken INSIDE the
// tool's own executor — i.e. at the one instant where "before the tool ran"
// and "already reported" are the same moment.
func TestWebStateToolTransitionPrecedesTheResult(t *testing.T) {
	prev := webStateTestToolDelay
	webStateTestToolDelay = 0
	t.Cleanup(func() { webStateTestToolDelay = prev })

	var (
		mu       sync.Mutex
		observed []webStateSnapshot
		wsRef    *webServer
	)
	ws, _ := webStateTurnServer(t, func() {
		if wsRef == nil {
			return
		}
		mu.Lock()
		observed = append(observed, wsRef.webStateSnapshotFor())
		mu.Unlock()
	})
	wsRef = ws

	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "read the hostname file"))

	mu.Lock()
	got := append([]webStateSnapshot(nil), observed...)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatalf("the tool executor never ran through the loop's own call path (no observation)")
	}
	if got[0].State != webStateToolRunning {
		t.Errorf("when the tool STARTED the state was %q, want %q (the call event must fire before the tool runs; %s)",
			got[0].State, webStateToolRunning, webStateReportLine(got[0]))
	}
	if got[0].Tool != webStateTestToolName {
		t.Errorf("tool while running = %q, want %q", got[0].Tool, webStateTestToolName)
	}
	// After the turn the machine is idle again, and the call stack is empty.
	if after := ws.webStateSnapshotFor(); after.State != webStateIdle || len(after.Tools) != 0 {
		t.Errorf("after the turn %s, want idle with no outstanding tool", webStateReportLine(after))
	}
}

// TestWebStatePageCarriesTheBadge pins the PAGE half: the resident element
// exists, carries the server's own attribute vocabulary, and the page's copies
// of the state names / route / event name are the server's spellings (a page
// inside a Go raw string cannot import them, so the pair is pinned here rather
// than trusted to a comment — Rule 13).
func TestWebStatePageCarriesTheBadge(t *testing.T) {
	ws := &webServer{addr: ":0"}
	rec := httptest.NewRecorder()
	ws.handleRoot(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()

	for _, want := range []string{
		`id="status"`,                       // the resident node
		`data-state="idle"`,                 // ...initialised to the default state
		`data-tool=""`,                      // ...and to no tool
		`data-interruptible="false"`,        // ...and to "nothing to stop"
		"const API_STATE='" + webStatePath + "'",
		"const EV_STATE='" + webStateFrame + "'",
		"const ST_IDLE='" + webStateIdle + "'",
		"const ST_RESPONDING='" + webStateResponding + "'",
		"const ST_TOOL_RUNNING='" + webStateToolRunning + "'",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry %q — the badge and the server would disagree", want)
		}
	}
	// The route must actually be ROUTED (a page constant pointing at a 404 is
	// the shape of a badge that never leaves its default).
	mux := ws.webChatHandler()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, webStatePath, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s through the real mux = %d, want 200", webStatePath, w.Code)
	}
	if !strings.Contains(w.Body.String(), `"state":`) {
		t.Errorf("the snapshot body carries no state field: %s", w.Body.String())
	}
}

// TestWebStateSnapshotIsCompleteNotOmitEmpty pins Rule 15a on the wire: every
// field is present in the idle document, and the tools list serialises as []
// rather than vanishing. A consumer that decoded an absent field as "no
// information" would read the wrong thing.
func TestWebStateSnapshotIsCompleteNotOmitEmpty(t *testing.T) {
	ws := &webServer{addr: ":0"}
	rec := httptest.NewRecorder()
	ws.handleWebState(rec, httptest.NewRequest(http.MethodGet, webStatePath, nil))
	body := rec.Body.String()
	for _, want := range []string{`"state":"`, `"tool":""`, `"tools":[]`, `"interruptible":`, `"turn":`, `"since":`} {
		if !strings.Contains(body, want) {
			t.Errorf("the snapshot does not carry %s: %s", want, body)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != webStateNoCache {
		t.Errorf("Cache-Control = %q, want %q", got, webStateNoCache)
	}
}

// TestWebStateRefusesNonGet pins the endpoint's ONE verb. The snapshot is a
// READING of the server's state; a POST that answered 200 would invite a caller
// to believe it had changed something, and `no-store` would be pointless on a
// surface that also accepted writes.
func TestWebStateRefusesNonGet(t *testing.T) {
	ws := &webServer{addr: ":0"}
	mux := ws.webChatHandler()
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, webStatePath, nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want %d (%d = it took a write)", method, webStatePath, w.Code, http.StatusMethodNotAllowed, http.StatusOK)
		}
	}
}

// TestWebStateNoticeTurnIsVisible proves a turn the SERVER started is visible
// in the badge too. It is the case the page's own `busy` flag structurally
// cannot report, so it is the one that shows the badge is the server's.
func TestWebStateNoticeTurnIsVisible(t *testing.T) {
	ws, _ := webStateTurnServer(t, nil)

	// Drive a server-started turn directly (the carrier's own entry point), with
	// the tool's executor blocking, and read the state WHILE it is parked.
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		close(started)
		ws.runNoticeTurnForTest(t)
	}()
	<-started

	waitFor(t, webStateTestClockSkew, "the notice turn to register as running", func() bool {
		return ws.webStateSnapshotFor().State != webStateIdle
	})
	<-done
	waitFor(t, webStateTestClockSkew, "the notice turn to return the machine to idle", func() bool {
		return ws.webStateSnapshotFor().State == webStateIdle
	})
}

// runNoticeTurnForTest runs ONE model turn through the same bookkeeping the
// notice carrier uses, without depending on a delivered inbox file — the
// subject here is the STATUS, and going through the inbox would make the test
// measure two mechanisms at once.
func (s *webServer) runNoticeTurnForTest(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.enterTurn(webNowUnix())
	s.publishWebState()
	defer func() {
		s.state.leaveTurn(webNowUnix())
		s.publishWebState()
	}()
	prevOnTool := s.agent.OnToolEvents
	s.agent.OnToolEvents = func(ev agent.ToolEvent) { s.noteToolState(ev) }
	defer func() { s.agent.OnToolEvents = prevOnTool }()
	if _, err := s.agent.ChatOpsOnCarrier(s.webChatCarrier(), "notice turn", nil); err != nil {
		t.Logf("notice turn returned %v (the status assertions do not depend on it)", err)
	}
}

// collectStateFrames drains a standing /api/events connection and returns every
// state document it carried. The connection itself is bounded by its request
// CONTEXT (the caller's), so this read ends when that context does — a window
// checked inside the loop would never be reached, because a Read on an SSE
// stream blocks until the next frame or the keepalive.
func collectStateFrames(t *testing.T, body io.Reader) []webStateSnapshot {
	t.Helper()
	type res struct{ frames []webStateSnapshot }
	ch := make(chan res, 1)
	go func() {
		var out []webStateSnapshot
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					idx := strings.Index(string(buf), "\n\n")
					if idx < 0 {
						break
					}
					frame := string(buf[:idx])
					buf = buf[idx+2:]
					ev, data := parseSSEFrame(frame)
					if ev != webStateFrame || data == "" {
						continue
					}
					var snap webStateSnapshot
					if err := json.Unmarshal([]byte(data), &snap); err != nil {
						continue
					}
					out = append(out, snap)
				}
			}
			if err != nil {
				break
			}
		}
		ch <- res{frames: out}
	}()
	select {
	case r := <-ch:
		return r.frames
	case <-time.After(webStateTestClockSkew):
		t.Fatalf("the standing stream did not end within %v", webStateTestClockSkew)
		return nil
	}
}

// parseSSEFrame splits one raw SSE frame into its event name and joined data.
func parseSSEFrame(frame string) (string, string) {
	event := ""
	var data strings.Builder
	for _, line := range strings.Split(frame, "\n") {
		if after, ok := strings.CutPrefix(line, "event:"); ok {
			event = strings.TrimSpace(after)
			continue
		}
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimSpace(after))
		}
	}
	return event, data.String()
}

// webStateReportLine is used only by the failure messages above; keeping it a
// function of the snapshot means a future field is not forgotten in a message.
func webStateReportLine(s webStateSnapshot) string {
	return fmt.Sprintf("state=%s tool=%q tools=%v interruptible=%v turn=%d", s.State, s.Tool, s.Tools, s.Interruptible, s.Turn)
}

// TestWebStateInterruptRouteIsNegativeBetweenTurns is the ROUTE half of task
// 01a1b4a2's fix, and it is measured at the two surfaces a reader has: the
// snapshot and the interrupt ack.
//
// The defect: `Agent.runCtx` was published at the start of a run and never
// retired when the run returned, so on an idle server `RunInFlight()` stayed
// true and `POST /api/chat/interrupt` answered `ok:true` for a turn that no
// longer existed. The busy badge only looked right because
// `agentRunInterruptible` ANDed the probe with this surface's own in-turn bit —
// the route had no such guard and told the lie directly.
func TestWebStateInterruptRouteIsNegativeBetweenTurns(t *testing.T) {
	ws, httpSrv := webStateTurnServer(t, nil)

	// Drive a REAL turn through the handler (the same path a POST takes).
	rec := newStreamRecorder()
	ws.handleChat(rec, chatRequestForTest(t, "read the hostname file"))

	// ① The snapshot, over HTTP: idle and nothing to stop.
	if snap := readState(t, httpSrv.URL); snap.Interruptible {
		t.Errorf("interruptible = true with no run in flight; %s", webStateReportLine(snap))
	}

	// ② The route answers NEGATIVELY — this is the reading that was a lie.
	body := postInterruptForTest(t, ws)
	if body.OK || body.Interrupted {
		t.Errorf("POST %s between turns acked an interruption: %+v", webChatInterruptPath, body)
	}

	// ③ And the snapshot's own interruptible flag still agrees afterwards (an
	// ack that quietly left a latch behind would show up here).
	if snap := readState(t, httpSrv.URL); snap.Interruptible {
		t.Errorf("interruptible = true after a negative interrupt ack; %s", webStateReportLine(snap))
	}
}

// postInterruptForTest POSTs the interrupt route and decodes its ack.
func postInterruptForTest(t *testing.T, ws *webServer) struct {
	OK          bool `json:"ok"`
	Interrupted bool `json:"interrupted"`
} {
	t.Helper()
	rec := httptest.NewRecorder()
	ws.handleChatInterrupt(rec, httptest.NewRequest(http.MethodPost, webChatInterruptPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200 (body %q)", webChatInterruptPath, rec.Code, rec.Body.String())
	}
	var body struct {
		OK          bool `json:"ok"`
		Interrupted bool `json:"interrupted"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the interrupt ack is not JSON: %v (body %q)", err, rec.Body.String())
	}
	return body
}
