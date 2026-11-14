// cli_web_m1_test.go — regression tests for the M1 web increments B1/B2/B3
// (devdoc/gui-chat-roadmap.md §3 Table B, §4 M1.4/M1.5).
//
//	B1 — `POST /api/chat/reset` clears the SERVER-OWNED conversation
//	     (s.history + per-turn run stats) strictly under s.mu, and `/new`
//	     (the shared command table's word) does the same inside a turn.
//	B2 — the 200 + SSE headers and one `event: queued` frame are written
//	     BEFORE s.mu is acquired, so a second concurrent POST is answered
//	     promptly while the first turn holds the lock.
//	B3 — the page's follow-bottom gate: a named FOLLOW_THRESHOLD const, the
//	     at-bottom predicate that gates every streamed delta, and a
//	     "jump to latest" affordance.
//
// Mock discipline (unchanged from cli_web_test.go / cli_web_align_test.go):
// the LLM is an httptest fake Claude SSE server; app state is real and isolated
// in t.TempDir + GOPI_HOME/GOPI_MEMORY_DB.
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
)

const (
	// webQueuedRacePollInterval is the B2 test's poll step while the first turn
	// holds s.mu: the test polls the real condition (the pre-lock queued frame
	// appearing on the wire), never a fixed wait.
	webQueuedRacePollInterval = 5 * time.Millisecond
	// webQueuedRaceBudget bounds that poll. A hang guard, not a performance
	// assertion: the frame is written before the lock is attempted, so it is
	// observable almost immediately.
	webQueuedRaceBudget = 3 * time.Second
	// webTestTurnModel is the model name the m1 tests drive (one declaration;
	// the mock answers any model).
	webTestTurnModel = "deepseek-v4-pro"
)

// m1Server builds a webServer whose single persistent agent points at the mock
// streaming server. The returned func reports every model request body recorded
// so far (mutex-guarded, so -race sees a clean handoff).
func m1Server(t *testing.T, answer string) (*webServer, *httptest.Server, func() [][]byte) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen [][]byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		mu.Lock()
		seen = append(seen, b)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, answer)
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
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	return ws, srv, func() [][]byte {
		mu.Lock()
		defer mu.Unlock()
		out := make([][]byte, len(seen))
		copy(out, seen)
		return out
	}
}

// turnContextTexts flattens a model request's message texts (user + assistant).
func turnContextTexts(t *testing.T, raw []byte) []string {
	t.Helper()
	var req struct {
		Messages []struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("bad model request JSON: %v", err)
	}
	var out []string
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Text != "" {
				out = append(out, c.Text)
			}
		}
	}
	return out
}

// postReset drives the B1 endpoint through the real route table (so the route
// registration is under test too, not just the handler).
func postReset(t *testing.T, ws *webServer) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, webChatResetPath, nil)
	ws.webChatHandler().ServeHTTP(rec, req)
	return rec
}

// TestWebM1B1_ResetClearsHistoryUnderLockTurnNPlusOneCannotSeeTurnOne is the B1
// acceptance test: after a reset the NEXT turn's model request must have no
// knowledge of the conversation before it — while a pre-reset turn
// demonstrably did. It also pins the structural properties: the reset drops the
// whole server-owned transcript (s.history under s.mu) and the per-turn run
// stats, as O(1) work on the same mutex handleChat uses.
//
// Codeword mirror of the live recipe (devdoc/gopi-web-report.md's ZEBRA trick):
// turn 1 asks the model to remember a codeword, turn 2 (pre-reset) carries it
// back in context, turn 3 (post-reset) must not.
func TestWebM1B1_ResetClearsHistoryUnderLockTurnNPlusOneCannotSeeTurnOne(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	ws, _, requests := m1Server(t, "acknowledged")
	const codeword = "ZEBRA-CODESWORD-7"

	postChat(t, ws, "please remember the codeword "+codeword, nil)
	if len(ws.history) == 0 {
		t.Fatal("turn 1 did not capture the server-owned transcript")
	}
	// Make the per-turn run stats non-zero so the reset's clearing of them is
	// observable (not vacuously true on zero values).
	ws.iterCount = 3
	ws.totalTokens = 1234

	// Turn 2 BEFORE the reset: the codeword is in the model context — the
	// control that makes the post-reset assertion meaningful.
	postChat(t, ws, "what codeword did I ask you to remember?", nil)
	reqs := requests()
	if len(reqs) < 2 {
		t.Fatalf("mock saw %d model requests, want >= 2", len(reqs))
	}
	if texts := turnContextTexts(t, reqs[1]); !containsText(texts, codeword) {
		t.Fatalf("turn 2 (pre-reset) model request has no codeword in context: %v", texts)
	}

	// B1: the reset.
	rec := postReset(t, ws)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s status = %d, want 200 (body %q)", webChatResetPath, rec.Code, rec.Body.String())
	}
	var resetBody struct {
		OK              bool   `json:"ok"`
		Message         string `json:"message"`
		HistoryMessages int    `json:"history_messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resetBody); err != nil {
		t.Fatalf("reset body %q is not JSON: %v", rec.Body.String(), err)
	}
	if !resetBody.OK || resetBody.HistoryMessages != 4 {
		t.Errorf("reset body = %+v, want ok + history_messages 4 (the two turns captured before the reset)", resetBody)
	}
	if resetBody.Message != webResetBodyMessage {
		t.Errorf("reset message = %q, want %q", resetBody.Message, webResetBodyMessage)
	}

	// The reset dropped the transcript and the per-turn stats, under the lock
	// every other reader of them takes.
	ws.mu.Lock()
	historyLen, iter, tokens := len(ws.history), ws.iterCount, ws.totalTokens
	ws.mu.Unlock()
	if historyLen != 0 {
		t.Errorf("s.history length after reset = %d, want 0", historyLen)
	}
	if iter != 0 || tokens != 0 {
		t.Errorf("per-turn state after reset = iter %d / tokens %d, want 0/0", iter, tokens)
	}

	// Turn N+1: a fresh model context — no codeword, none of the pre-reset
	// exchange, just the new user message.
	postChat(t, ws, "what codeword did I ask you to remember?", nil)
	reqs = requests()
	texts := turnContextTexts(t, reqs[len(reqs)-1])
	if containsText(texts, codeword) {
		t.Errorf("turn N+1 still sees the pre-reset codeword — the reset did not clear the seed: %v", texts)
	}
	if len(texts) != 1 {
		t.Errorf("turn N+1 context has %d texts, want exactly the new user message: %v", len(texts), texts)
	}
	if got := len(ws.history); got != 2 {
		t.Errorf("s.history after turn N+1 = %d, want 2 (only the post-reset turn)", got)
	}
}

// TestWebM1B1_SlashNewResetsUnderLockViaSharedCommandTable drives the BROWSER
// affordance: `/new` arrives as a message, must be answered as ONE
// `event: command` frame (R4 — never a model turn), must actually clear the
// conversation under the lock, and — because the word lives in the shared
// command table — must be discoverable in /help with a -h-answering usage
// block. `/clear`, the chat REPL's spelling of the same reset, behaves
// identically.
func TestWebM1B1_SlashNewResetsUnderLockViaSharedCommandTable(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	ws, _, requests := m1Server(t, "acknowledged")

	// The word is in the SHARED table (one registry, both carriers) and its
	// help contract holds: -h answers the command's own usage byte-for-byte.
	c, ok := lookupCommand(webClearCommandName)
	if !ok {
		t.Fatalf("/%s is not in the shared command table", webClearCommandName)
	}
	for _, help := range []string{"-h", "--help", "help"} {
		out, err := execSlashCommand("/"+webClearCommandName+" "+help, CommandContext{})
		if err != nil {
			t.Fatalf("/%s %s: %v", webClearCommandName, help, err)
		}
		if out != c.Usage {
			t.Errorf("/%s %s output != the command's Usage (help contract)", webClearCommandName, help)
		}
	}
	if !strings.Contains(slashHelpText(), "/"+webClearCommandName) {
		t.Errorf("/help does not list /%s:\n%s", webClearCommandName, slashHelpText())
	}

	for _, word := range []string{"/" + webClearCommandName, "/" + webClearAliasName} {
		postChat(t, ws, "seed the conversation before "+word, nil)
		before := len(requests())

		body := postChat(t, ws, word, nil)
		data, ok := sseDataOf(body, webEventCommand)
		if !ok {
			t.Fatalf("%s: no command frame (body %q)", word, body)
		}
		if strings.Contains(body, "event: "+webEventDelta) || strings.Contains(body, "event: "+webEventDone) {
			t.Errorf("%s: the reset must not run a model turn (body %q)", word, body)
		}
		// The frame's text is the shared table's own output for the word.
		var frame struct {
			Command string `json:"command"`
			Output  string `json:"output"`
			Cleared int    `json:"cleared"`
		}
		if err := json.Unmarshal([]byte(data), &frame); err != nil {
			t.Fatalf("%s: bad command frame payload %q: %v", word, data, err)
		}
		if frame.Output != webResetCleared {
			t.Errorf("%s: frame output = %q, want the shared table's reset text %q", word, frame.Output, webResetCleared)
		}
		if frame.Cleared != 2 {
			t.Errorf("%s: frame cleared = %d, want 2 (the seeded turn)", word, frame.Cleared)
		}
		if got := len(ws.history); got != 0 {
			t.Errorf("%s: s.history = %d after the slash reset, want 0", word, got)
		}
		// A reset is local: the model was never asked anything by it.
		if after := len(requests()); after != before {
			t.Errorf("%s: the reset produced %d model request(s)", word, after-before)
		}
	}
}

// TestWebM1B2_SecondConcurrentPostGetsQueuedFrameWhileFirstHoldsLock is the B2
// acceptance test: while turn 1 is inside the model call holding s.mu, a second
// concurrent POST must be answered with the SSE headers and one `event: queued`
// frame — promptly, and while the lock is still held — instead of hanging
// byte-less until turn 1 finishes.
//
// Mechanism: the mock LLM blocks on a channel, so turn 1 provably holds s.mu
// for the whole poll; the second POST runs on its own goroutine against a
// response writer that exposes what has been written so far.
func TestWebM1B2_SecondConcurrentPostGetsQueuedFrameWhileFirstHoldsLock(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	var modelCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&modelCalls, 1) == 1 {
			entered <- struct{}{}
			<-release // hold turn 1 inside the model call, hence inside s.mu
		}
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "answered")
	}))
	defer srv.Close()

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   webTestTurnModel,
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	// Turn 1: a real HTTP request so the response is written incrementally.
	first := newStreamRecorder()
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		ws.handleChat(first, chatRequestForTest(t, "first turn"))
	}()
	select {
	case <-entered:
	case <-time.After(webQueuedRaceBudget):
		t.Fatal("turn 1 never reached the model call")
	}

	// Turn 2 while turn 1 holds s.mu.
	second := newStreamRecorder()
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		ws.handleChat(second, chatRequestForTest(t, "second turn"))
	}()

	sawQueued := false
	deadline := time.Now().Add(webQueuedRaceBudget)
	for time.Now().Before(deadline) {
		if strings.Contains(second.body(), "event: "+webEventQueued) {
			sawQueued = true
			break
		}
		// Poll the real condition (the pre-lock frame appearing), never a
		// fixed wait: the loop exits the moment it is on the wire.
		time.Sleep(webQueuedRacePollInterval) //stylegate:allow-sleep polls a real condition — the pre-lock queued frame appearing on the wire — and exits as soon as it does
	}
	if !sawQueued {
		close(release)
		<-firstDone
		<-secondDone
		t.Fatalf("the second POST got no queued frame while s.mu was held; body so far: %q", second.body())
	}
	// The frame's shape: typed placement + the notice the page renders, written
	// BEFORE the lock (so it precedes any turn payload).
	data, _ := sseDataOf(second.body(), webEventQueued)
	var queued struct {
		Placement string `json:"placement"`
		Notice    string `json:"notice"`
	}
	if err := json.Unmarshal([]byte(data), &queued); err != nil {
		t.Fatalf("queued frame payload %q: %v", data, err)
	}
	if queued.Placement != webPlacementQueued {
		t.Errorf("queued placement = %q, want %q", queued.Placement, webPlacementQueued)
	}
	if queued.Notice != webBusyWaitNotice {
		t.Errorf("queued notice = %q, want %q", queued.Notice, webBusyWaitNotice)
	}
	if ct := second.header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("second POST Content-Type = %q, want text/event-stream (headers flushed pre-lock)", ct)
	}
	if n := atomic.LoadInt32(&modelCalls); n != 1 {
		t.Errorf("the queued turn reached the model before the first finished (%d calls)", n)
	}

	// Release turn 1: BOTH turns must then complete normally (queueing must not
	// have swallowed or broken turn 2), and the second body must carry the full
	// turn payload after the queued frame.
	close(release)
	<-firstDone
	<-secondDone
	for _, s := range []struct {
		name string
		rec  *streamRecorder
	}{{"first", first}, {"second", second}} {
		body := s.rec.body()
		if !strings.Contains(body, "event: "+webEventDone) {
			t.Errorf("%s turn has no done frame: %q", s.name, body)
		}
		if !strings.Contains(body, "event: "+webEventDelta) {
			t.Errorf("%s turn streamed no delta: %q", s.name, body)
		}
	}
	// The queued frame is a PRE-lock frame: the uncontended turn never sees it.
	if strings.Contains(first.body(), "event: "+webEventQueued) {
		t.Errorf("the uncontended turn got a queued frame: %q", first.body())
	}
}

// TestWebM1B3_FollowThresholdGatesStreamScrollAndJumpAffordance pins the B3
// page contract by reading the embedded page: the follow-bottom threshold is a
// NAMED const (matching the server-side webFollowThreshold, ported from dsh
// ChatView.tsx FOLLOW_THRESHOLD), the streamed-frame paths consult it through
// the at-bottom predicate instead of scrolling unconditionally, and a
// "jump to latest" affordance exists.
func TestWebM1B3_FollowThresholdGatesStreamScrollAndJumpAffordance(t *testing.T) {
	page := webChatPage

	// 1. The named constant, with the ported value.
	m := regexp.MustCompile(`const\s+FOLLOW_THRESHOLD\s*=\s*(\d+)\s*;`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the page declares no FOLLOW_THRESHOLD const")
	}
	got, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("FOLLOW_THRESHOLD %q is not a number: %v", m[1], err)
	}
	if got != webFollowThreshold {
		t.Errorf("page FOLLOW_THRESHOLD = %d, server webFollowThreshold = %d — they must be one value", got, webFollowThreshold)
	}

	// 2. The at-bottom predicate exists and is threshold-based (never a bare
	//    scrollHeight comparison).
	atBottom := regexp.MustCompile(`(?s)function atBottom\(\)\{[^}]*\}`).FindString(page)
	if atBottom == "" {
		t.Fatal("the page has no atBottom() predicate")
	}
	if !strings.Contains(atBottom, "FOLLOW_THRESHOLD") {
		t.Errorf("atBottom() does not use the threshold: %q", atBottom)
	}

	// 3. The streamed paths follow CONDITIONALLY: the delta and tool handlers
	//    must EACH route through the gate (the pre-B3 defect was an
	//    unconditional scroll on every delta/tool frame). Each branch body is
	//    extracted and checked, so gating one branch and not the other fails.
	for _, branch := range []struct {
		name string
		re   *regexp.Regexp
	}{
		{"delta", regexp.MustCompile(`(?s)ev==='delta'\)\{(.*?)\n      \}else if`)},
		{"tool", regexp.MustCompile(`(?s)ev==='tool'\)\{(.*?)\n      \}else if`)},
	} {
		m := branch.re.FindStringSubmatch(page)
		if m == nil {
			t.Fatalf("could not locate the %s handler in the page", branch.name)
		}
		if !strings.Contains(m[1], "follow(false)") {
			t.Errorf("the %s stream path does not gate its follow: %s", branch.name, m[1])
		}
		if strings.Contains(m[1], "scrollTop=logEl.scrollHeight") {
			t.Errorf("B3 regression: the %s stream path still scrolls unconditionally: %s", branch.name, m[1])
		}
	}
	// The only unconditional scrolls left are the gate's own primitive and the
	// explicit "a new message arrived" follow(true) calls.
	for _, line := range strings.Split(page, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "scrollTop=logEl.scrollHeight") {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "function scrollToBottom()"):
			// the gate's primitive: correct by construction
		case strings.HasPrefix(trimmed, "function follow("),
			strings.HasPrefix(trimmed, "jumpBtn"):
			// the gate itself / the explicit jump affordance
		case strings.Contains(trimmed, "follow(true)"):
			// an explicit follow (new user turn / command node)
		default:
			t.Errorf("B3 regression: unconditional scroll outside the follow gate: %q", trimmed)
		}
	}

	// 4. The "jump to latest" affordance: element + click handler + the
	//    visibility toggles the gate drives.
	for _, want := range []string{
		`id="jump"`,
		"jumpBtn.addEventListener('click'",
		"jumpBtn.classList.add('show')",
		"jumpBtn.classList.remove('show')",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the jump-to-latest affordance is missing %q", want)
		}
	}

	// 5. B1 + B2 page wiring (the two frames the server now emits).
	//    B1's clear is asserted by BEHAVIOUR, not by one spelling: the reset
	//    branch must empty #log. The M2 rework routes that through the shared
	//    clearNode() (the same append-only discipline B8 uses — the page keeps
	//    exactly two deliberate clears and no content interpolation), so either
	//    the helper or the direct clear satisfies B1.
	if !strings.Contains(page, "RESET_NOTICE") ||
		(!strings.Contains(page, "clearNode(logEl)") && !strings.Contains(page, "logEl.innerHTML=''")) {
		t.Error("the page does not wipe the display on the server's reset notice (B1)")
	}
	if !strings.Contains(page, "ev==='queued'") {
		t.Error("the page's frames() parser does not render the queued frame (B2)")
	}
	// The page's reset sentinel is the SERVER's constant — no second spelling.
	if !strings.Contains(page, "conversation reset — the transcript and per-turn state were cleared") {
		t.Error("the page's RESET_NOTICE is not the server's webResetBodyMessage text")
	}
}

// --- helpers ---------------------------------------------------------------

// chatRequestForTest builds a POST /api/chat request carrying one message.
func chatRequestForTest(t *testing.T, message string) *http.Request {
	t.Helper()
	body, err := json.Marshal(webChatRequest{Message: message})
	if err != nil {
		t.Fatalf("marshal chat request: %v", err)
	}
	return httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(string(body)))
}

// sseDataOf returns the data payload of the FIRST frame with the given event
// name in an SSE response body, and whether such a frame exists.
func sseDataOf(body, event string) (string, bool) {
	for _, frame := range strings.Split(body, "\n\n") {
		lines := strings.Split(frame, "\n")
		if len(lines) == 0 || strings.TrimSpace(lines[0]) != "event: "+event {
			continue
		}
		for _, l := range lines[1:] {
			if strings.HasPrefix(l, "data: ") {
				return strings.TrimPrefix(l, "data: "), true
			}
		}
	}
	return "", false
}

// containsText reports whether a message-text list carries the given needle.
func containsText(texts []string, needle string) bool {
	for _, s := range texts {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// streamRecorder is an http.ResponseWriter that keeps what has been written so
// far readable WHILE the handler is still running (httptest.ResponseRecorder
// only exposes the body after the fact). Reads and writes are mutex-guarded
// because the test polls from another goroutine.
type streamRecorder struct {
	mu     sync.Mutex
	header http.Header
	code   int
	buf    strings.Builder
}

func newStreamRecorder() *streamRecorder {
	return &streamRecorder{header: http.Header{}}
}

func (r *streamRecorder) Header() http.Header { return r.header }

func (r *streamRecorder) WriteHeader(code int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = code
	}
}

func (r *streamRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.buf.Write(p)
}

// Flush makes the recorder an http.Flusher (handleChat requires one, and
// flushing must be observable-free: the body is readable as it is written).
func (r *streamRecorder) Flush() {}

// body returns everything written so far.
func (r *streamRecorder) body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}
